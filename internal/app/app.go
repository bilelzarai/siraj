// Package app is the one place where the pool, services, modules and router are wired.
package app

import (
	"context"

	"github.com/gin-gonic/gin"

	"siraj/internal/config"
	"siraj/internal/db"
	sirajhttp "siraj/internal/http"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/modules/admin"
	"siraj/internal/modules/auth"
	"siraj/internal/modules/catalog"
	"siraj/internal/modules/challenges"
	"siraj/internal/modules/content"
	"siraj/internal/modules/duel"
	"siraj/internal/modules/games"
	livemod "siraj/internal/modules/live"
	"siraj/internal/modules/messages"
	"siraj/internal/modules/nodes"
	"siraj/internal/modules/notifications"
	"siraj/internal/modules/perm"
	"siraj/internal/modules/progress"
	"siraj/internal/modules/questions"
	"siraj/internal/modules/rooms"
	"siraj/internal/modules/sets"
	"siraj/internal/modules/settings"
	"siraj/internal/modules/social"
	"siraj/internal/modules/support"
	"siraj/internal/platform/live"
	"siraj/internal/platform/mailer"
	"siraj/internal/platform/sms"
)

type App struct {
	Cfg    config.Config
	DB     *db.DB
	Router *gin.Engine
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		return nil, err
	}
	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	bundle, err := i18n.Load()
	if err != nil {
		return nil, err
	}
	assets := render.NewAssets(cfg.ViteDevServer, cfg.IsDev())

	authSvc := auth.NewService(d, auth.Deps{
		SessionTTL:     cfg.SessionTTL,
		GuestTTL:       cfg.GuestTTL,
		Mailer:         mailer.New(cfg.SMTPAddr, cfg.MailFrom),
		SMS:            sms.New(cfg.SMSProvider),
		GoogleClientID: cfg.GoogleClientID,
		AppleClientID:  cfg.AppleClientID,
		BaseURL:        cfg.BaseURL,
		// No gateway in development: the code comes back to the screen rather
		// than being stuck in a log nobody is watching.
		DevCodes: cfg.IsDev() && cfg.SMSProvider == "",
	})
	// One event stream for the whole app: services announce on it, screens listen.
	hub := live.NewHub()
	catalogSvc := catalog.NewService(d)
	settingsSvc := settings.NewService(d, authSvc, catalogSvc)
	gamesSvc := games.NewService(d, catalogSvc)
	questionsSvc := questions.NewService(d)
	progressSvc := progress.NewService(d)
	// Progress writes XP, mastery, streaks and the leaderboard inside the finish
	// transaction, so a finished game and its progress never disagree.
	gamesSvc.AddFinishHook(progressHook{progressSvc})
	gamesWeb := games.NewWeb(gamesSvc, catalogSvc, nil)
	notifySvc := notifications.NewService(d)
	notifySvc.SetLive(hub)
	socialSvc := social.NewService(d, notifySvc)
	setsSvc := sets.NewService(d)
	supportSvc := support.NewService(d)
	adminSvc := admin.NewService(d)
	permSvc := perm.NewService(d)
	nodesSvc := nodes.NewService(d, permSvc)
	contentSvc := content.NewService(d, permSvc)
	progressWeb := progress.NewWeb(progressSvc, gamesWeb)
	gamesWeb.SetProgress(progressWeb)
	extras := homeExtras{questions: questionsSvc, progress: progressSvc, social: socialSvc, catalog: catalogSvc}

	// Challenges and pass-the-device rounds share the game engine and the topic
	// picker; the finish hook writes each player's score back onto the challenge.
	challengesSvc := challenges.NewService(d, gamesSvc, notifySvc)
	gamesSvc.AddFinishHook(challengesSvc)
	socialWeb := social.NewWeb(socialSvc, cfg.BaseURL)
	setsWeb := sets.NewWeb(setsSvc, catalogSvc)
	challengesSvc.SetLive(hub)
	challengesWeb := challenges.NewWeb(challengesSvc, gamesSvc, gamesWeb, socialWeb, setsWeb)
	extras.challenges = challengesSvc

	messagesSvc := messages.NewService(d, socialSvc)
	messagesSvc.SetPeople(peopleForMessages{social: socialSvc})
	messagesSvc.SetUploadsDir(cfg.UploadsDir)
	messagesSvc.SetLive(hub)
	messagesWeb := messages.NewWeb(messagesSvc)
	messagesWeb.SetBlocks(blockForMessages{social: socialSvc})
	duelSvc := duel.NewService(d, gamesSvc, hub)
	gamesSvc.AddFinishHook(duelSvc)
	duelWeb := duel.NewWeb(duelSvc)
	roomsSvc := rooms.NewService(d, gamesSvc)
	roomsSvc.SetLive(hub)
	roomsSvc.SetFriends(socialSvc)
	// The entry screen's "Join a room" is open to a visitor, who becomes a guest in
	// the act of joining: that is what the auth web module is handed over for.
	authWeb := auth.NewWeb(authSvc, extras)
	roomsWeb := rooms.NewWeb(roomsSvc, gamesWeb, authWeb, cfg.BaseURL)
	// The Rooms tab of Messages reads the live rooms through this adapter (MS-01).
	messagesSvc.SetRooms(roomsForMessages{rooms: roomsSvc})
	// A challenge can be posted into a thread, and tapped from there (MS-02).
	messagesSvc.SetChallenges(challengesForMessages{challenges: challengesSvc})
	gamesWeb.SetRooms(roomsWeb)

	catalogWeb := catalog.NewWeb(catalogSvc, extras)
	catalogWeb.SetDomainExtras(extras)

	router := sirajhttp.NewRouter(sirajhttp.Options{
		DB:          d,
		Resolver:    authSvc,
		Shell:       shell{auth: authSvc, catalog: catalogSvc, challenges: challengesSvc, messages: messagesSvc},
		Bundle:      bundle,
		Assets:      assets,
		Dev:         cfg.IsDev(),
		CORSOrigins: cfg.CORSOrigins,
		Modules: []sirajhttp.Module{
			auth.NewAPI(authSvc),
			catalog.NewAPI(catalogSvc),
			settings.NewAPI(settingsSvc),
			games.NewAPI(gamesSvc),
			questions.NewAPI(questionsSvc),
			progress.NewAPI(progressSvc),
			social.NewAPI(socialSvc, cfg.BaseURL),
			notifications.NewAPI(notifySvc),
			sets.NewAPI(setsSvc),
			support.NewAPI(supportSvc),
			rooms.NewAPI(roomsSvc),
			duel.NewAPI(duelSvc),
			challenges.NewAPI(challengesSvc),
			messages.NewAPI(messagesSvc),
			livemod.NewAPI(hub),
			// The admin console is staff-only and web-only (AD-01), so it has no
			// entry here: every other module answers on /api/v1.
		},
		Web: []sirajhttp.WebModule{
			authWeb,
			catalogWeb,
			gamesWeb,
			progressWeb,
			socialWeb,
			notifications.NewWeb(notifySvc),
			setsWeb,
			challengesWeb,
			roomsWeb,
			duelWeb,
			messagesWeb,
			livemod.NewWeb(hub),
			support.NewWeb(supportSvc),
			admin.NewWeb(adminSvc, supportSvc, nodesSvc),
			nodes.NewWeb(nodesSvc, contentSvc, permSvc, socialSvc),
			questions.NewWeb(questionsSvc),
			settings.NewWeb(settingsSvc),
		},
	})
	return &App{Cfg: cfg, DB: d, Router: router}, nil
}

func (a *App) Close() { a.DB.Close() }

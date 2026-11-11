// Package tests holds the integration and API contract tests: a real PostgreSQL 18,
// the real router, the real services — only the clock and the mailer are fakes.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/db"
	"siraj/internal/db/dbtest"
	"siraj/internal/db/sqlc"
	sirajhttp "siraj/internal/http"
	"siraj/internal/http/middleware"
	"siraj/internal/http/render"
	"siraj/internal/i18n"
	"siraj/internal/modules/admin"
	"siraj/internal/modules/auth"
	"siraj/internal/modules/catalog"
	"siraj/internal/modules/challenges"
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
	"siraj/internal/platform/authctx"
	"siraj/internal/platform/live"
	"siraj/internal/platform/mailer"
	"siraj/internal/view"
)

// app is everything a test needs: the router and the services behind it.
type app struct {
	t       *testing.T
	DB      *db.DB
	Router  *gin.Engine
	Auth    *auth.Service
	Catalog *catalog.Service
	Mail    *fakeMailer
	// Content is the domain, category and questions this test wrote for itself. No
	// migration seeds a bank any more, so a test that plays a game needs one.
	Content dbtest.Content
}

type fakeMailer struct{ sent []mailer.Mail }

func (f *fakeMailer) Send(_ context.Context, m mailer.Mail) error {
	f.sent = append(f.sent, m)
	return nil
}

func newApp(t *testing.T) *app { return newAppWithOrigins(t, nil) }

// newAppWithOrigins is newApp with the CORS allow-list a test wants to exercise.
func newAppWithOrigins(t *testing.T, origins []string) *app {
	t.Helper()
	gin.SetMode(gin.TestMode)
	d := dbtest.Open(t)
	content := dbtest.Seed(t, d, 12)
	bundle, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	mail := &fakeMailer{}
	authSvc := auth.NewService(d, auth.Deps{
		SessionTTL: 720 * time.Hour, GuestTTL: 24 * time.Hour,
		Mailer: mail, BaseURL: "http://test.local",
		// No gateway in a test either: the one-time code comes back in the answer,
		// which is how a test can go on to use it.
		DevCodes: true,
	})
	catalogSvc := catalog.NewService(d)
	settingsSvc := settings.NewService(d, authSvc, catalogSvc)
	gamesSvc := games.NewService(d, catalogSvc)
	questionsSvc := questions.NewService(d)
	progressSvc := progress.NewService(d)
	gamesSvc.AddFinishHook(testProgressHook{progressSvc})
	notifySvc := notifications.NewService(d)
	socialSvc := social.NewService(d, notifySvc)
	setsSvc := sets.NewService(d)
	supportSvc := support.NewService(d)
	adminSvc := admin.NewService(d)
	gamesWeb := games.NewWeb(gamesSvc, catalogSvc, nil)
	progressWeb := progress.NewWeb(progressSvc, gamesWeb)
	gamesWeb.SetProgress(progressWeb)

	// The modes that need the live hub, wired as the app wires them, so the contract
	// tests run against the real services (NF-04).
	hub := live.NewHub()
	notifySvc.SetLive(hub)
	challengesSvc := challenges.NewService(d, gamesSvc, notifySvc)
	challengesSvc.SetLive(hub)
	gamesSvc.AddFinishHook(challengesSvc)
	duelSvc := duel.NewService(d, gamesSvc, hub)
	gamesSvc.AddFinishHook(duelSvc)
	roomsSvc := rooms.NewService(d, gamesSvc)
	roomsSvc.SetLive(hub)
	roomsSvc.SetFriends(socialSvc)
	messagesSvc := messages.NewService(d, socialSvc)
	messagesSvc.SetLive(hub)
	messagesSvc.SetChallenges(testChallenges{challengesSvc})
	messagesWeb := messages.NewWeb(messagesSvc)
	// Attachments (#86) write to a directory of the test's own.
	messagesSvc.SetUploadsDir(t.TempDir())

	authWeb := auth.NewWeb(authSvc, nil)

	router := sirajhttp.NewRouter(sirajhttp.Options{
		DB: d, Resolver: authSvc, Shell: testShell{authSvc, catalogSvc},
		Bundle: bundle, Assets: render.NewAssets("", false), Dev: true,
		CORSOrigins: origins,
		Modules: []sirajhttp.Module{
			auth.NewAPI(authSvc), catalog.NewAPI(catalogSvc), settings.NewAPI(settingsSvc),
			games.NewAPI(gamesSvc), questions.NewAPI(questionsSvc), progress.NewAPI(progressSvc),
			social.NewAPI(socialSvc, "http://test.local"), notifications.NewAPI(notifySvc),
			sets.NewAPI(setsSvc), support.NewAPI(supportSvc),
			rooms.NewAPI(roomsSvc), duel.NewAPI(duelSvc),
			challenges.NewAPI(challengesSvc), messages.NewAPI(messagesSvc),
			livemod.NewAPI(hub),
		},
		Web: []sirajhttp.WebModule{
			authWeb, catalog.NewWeb(catalogSvc, nil), gamesWeb, progressWeb,
			// The rooms screens, wired as internal/app wires them: the entry
			// screen's "Join a room" needs the auth module to make the guest.
			rooms.NewWeb(roomsSvc, gamesWeb, authWeb, "http://test.local"),
			questions.NewWeb(questionsSvc), settings.NewWeb(settingsSvc),
			social.NewWeb(socialSvc, "http://test.local"), notifications.NewWeb(notifySvc),
			sets.NewWeb(setsSvc, catalogSvc), support.NewWeb(supportSvc), admin.NewWeb(adminSvc, supportSvc, nodes.NewService(d, perm.NewService(d))),
			messagesWeb,
		},
	})
	return &app{t: t, DB: d, Router: router, Auth: authSvc, Catalog: catalogSvc, Mail: mail, Content: content}
}

// testChallenges is the wiring that lets the composer post a challenge card,
// exactly as internal/app does it.
type testChallenges struct{ svc *challenges.Service }

func (t testChallenges) Shareable(ctx context.Context, who *authctx.Principal, lang string) ([]messages.ChallengeCard, error) {
	rows, err := t.svc.Shareable(ctx, who, lang)
	if err != nil {
		return nil, err
	}
	out := make([]messages.ChallengeCard, len(rows))
	for i, r := range rows {
		out[i] = messages.ChallengeCard{ID: r.ID, Title: r.Title, Questions: r.Questions, ScoreToBeat: r.ScoreToBeat}
	}
	return out, nil
}

func (t testChallenges) Card(ctx context.Context, who *authctx.Principal, id uuid.UUID, lang string) (messages.ChallengeCard, error) {
	r, err := t.svc.Card(ctx, who, id, lang)
	if err != nil {
		return messages.ChallengeCard{}, err
	}
	return messages.ChallengeCard{ID: r.ID, Title: r.Title, Questions: r.Questions, ScoreToBeat: r.ScoreToBeat}, nil
}

// testProgressHook is the same seam as the production one: progress writes inside the
// game-finish transaction.
type testProgressHook struct{ p *progress.Service }

func (h testProgressHook) OnGameFinished(ctx context.Context, q *sqlc.Queries, r games.FinishResult) error {
	return h.p.Apply(ctx, q, progress.Finished{
		GameID: r.Game.ID, PlayerID: r.PlayerID, UserID: r.UserID, Mode: string(r.Game.Mode),
		Score: r.Score, Correct: r.Correct, Total: r.Total, XP: r.XP,
	})
}

// testShell mirrors the production shell loader closely enough for page tests.
type testShell struct {
	auth    *auth.Service
	catalog *catalog.Service
}

func (s testShell) Load(ctx context.Context, p *authctx.Principal, lang string) (middleware.ShellData, error) {
	out := middleware.ShellData{}
	user, err := s.auth.Me(ctx, p.ID)
	if err != nil {
		return out, err
	}
	out.DisplayName = user.DisplayName
	domains, err := s.catalog.MyDomains(ctx, p, lang)
	if err != nil {
		return out, err
	}
	for _, d := range domains {
		out.MyDomains = append(out.MyDomains, view.Domain{ID: d.ID, Slug: d.Slug, Name: d.Name, Color: d.Color, Family: string(d.Family), Icon: d.Icon, Motif: d.Motif, IsFeatured: d.IsFeatured})
	}
	return out, nil
}

// reply is one HTTP answer, with helpers for the JSON envelope.
type reply struct {
	t    *testing.T
	Code int
	Body []byte
	Hdr  http.Header
}

func (r reply) data() map[string]any {
	r.t.Helper()
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(r.Body, &out); err != nil {
		r.t.Fatalf("body is not the {\"data\":…} envelope: %s", r.Body)
	}
	return out.Data
}

func (r reply) list() []any {
	r.t.Helper()
	var out struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(r.Body, &out); err != nil {
		r.t.Fatalf("body is not a list envelope: %s", r.Body)
	}
	return out.Data
}

func (r reply) errorCode() string {
	r.t.Helper()
	var out struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Body, &out); err != nil {
		r.t.Fatalf("body is not an error envelope: %s", r.Body)
	}
	return out.Error.Code
}

func (r reply) expect(code int) reply {
	r.t.Helper()
	if r.Code != code {
		r.t.Fatalf("want status %d, got %d: %s", code, r.Code, r.Body)
	}
	return r
}

// do sends a request. token != "" becomes a Bearer header (the mobile client path).
func (a *app) do(method, path string, body any, token string) reply {
	a.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			a.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	return reply{t: a.t, Code: rec.Code, Body: rec.Body.Bytes(), Hdr: rec.Header()}
}

func (a *app) get(path, token string) reply { return a.do(http.MethodGet, path, nil, token) }
func (a *app) post(path string, body any, token string) reply {
	return a.do(http.MethodPost, path, body, token)
}

// guest starts a guest session and returns its token (#4).
func (a *app) guest(name string) string {
	a.t.Helper()
	r := a.post("/api/v1/auth/guest", map[string]any{"name": name, "client": "ios"}, "").expect(http.StatusCreated)
	token, _ := r.data()["token"].(string)
	if token == "" {
		a.t.Fatalf("no token in %s", r.Body)
	}
	return token
}

// member registers an account and returns its token (#1).
func (a *app) member(username string) string {
	a.t.Helper()
	r := a.post("/api/v1/auth/register", map[string]any{
		"display_name": username, "username": username,
		"email": username + "@example.com", "password": "averysafe1",
		"accept_terms": true, "client": "ios",
	}, "").expect(http.StatusCreated)
	token, _ := r.data()["token"].(string)
	if token == "" {
		a.t.Fatalf("no token in %s", r.Body)
	}
	return token
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// decode is json.Unmarshal under a name the sweep can read.
func decode(body []byte, into any) error { return json.Unmarshal(body, into) }

// page reads the "page" block a list endpoint carries beside its data. A list that
// has none cannot be paged through by a client, which is what §17 forbids.
func (r reply) page() map[string]any {
	r.t.Helper()
	var out struct {
		Page map[string]any `json:"page"`
	}
	if err := json.Unmarshal(r.Body, &out); err != nil {
		return nil
	}
	return out.Page
}

// rewriteEveryQuestion is an author changing their mind after a game has been dealt.
// It writes straight to the database because the point is what the *engine* does
// afterwards, not how the edit was made.
func (a *app) rewriteEveryQuestion(t *testing.T, body string) {
	t.Helper()
	if _, err := a.DB.Pool.Exec(context.Background(),
		`UPDATE question_translations SET body = $1`, body); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
}

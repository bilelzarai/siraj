package handlers

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/bilelzarai/siraj/internal/assets"
	"github.com/bilelzarai/siraj/internal/handlers/api"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Routes builds the full request tree. Public pages sit at the root, every
// authenticated area is grouped behind RequireAuth.
func (h *Handlers) Routes(staticFS fs.FS) http.Handler {
	// Hash the assets once at boot so every page links a versioned URL.
	h.assetV = assetVersion(staticFS)

	// Where the built assets are, or the dev server if one is pointed at us. A
	// tree nobody has built resolves nothing and the pages still answer.
	manifest := assets.Load(staticFS, assetBase)
	devOrigin := ""
	if !h.cfg.IsProduction() {
		devOrigin = h.cfg.ViteDevServer
	}
	h.links = assets.NewLinks(manifest, devOrigin, h.assetV, assetBase)
	switch {
	case devOrigin != "":
		slog.Info("linking assets from the dev server", "origin", devOrigin)
	case manifest.Loaded():
		slog.Info("linking built assets", "manifest", manifest.Source())
	default:
		slog.Warn("no asset build found and no dev server configured; " +
			"pages will render without styles or script — run `npm run build`")
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)

	// Only rewrite the peer address from X-Forwarded-For when the operator has
	// said how many proxies are in front. Trusting the header unconditionally
	// lets any caller pick their own identity, which is what the login and
	// password-reset limiters key on.
	if h.cfg.TrustedProxyHops > 0 {
		r.Use(RealIP(h.cfg.TrustedProxyHops))
	}

	// Logger before Recover, so the panic handler is looking at the status
	// recorder Logger installs. The other way round, its "have the headers
	// already gone out?" check can never see one and always answers no.
	r.Use(Logger)
	r.Use(h.Recover)
	r.Use(SecureHeaders(contentSecurityPolicy(h.cfg)))
	r.Use(middleware.Compress(5, "text/html", "text/css", "application/javascript", "application/json"))

	r.NotFound(h.NotFound)

	// Static assets are served before the session middleware so they never
	// touch the database.
	r.Handle("/static/*", staticHandler(staticFS, !h.cfg.IsProduction()))
	r.Get("/healthz", h.Health)

	// The public surface, mounted above the session layer on purpose: it reads
	// no cookie and pairs no cross-site token, because a bearer key is a
	// different kind of credential and the two must never be interchangeable
	// (D2). Nothing below this line can be reached with a key, and nothing
	// above it with a session.
	r.Mount("/api/v1", api.NewV1(h.repo).Routes())

	r.Group(func(r chi.Router) {
		r.Use(h.Session)
		// Who the next move belongs to, on a device more than one person is
		// using. Always resolved against whoever is signed in, so a seat
		// cookie can never name somebody they did not create.
		r.Use(h.Seat)
		r.Use(h.CSRF)

		// ---- public
		r.Get("/", h.Landing)

		r.Group(func(r chi.Router) {
			r.Use(h.RequireGuest)
			r.Get("/login", h.LoginForm)
			r.Post("/login", h.Login)
			r.Get("/register", h.RegisterForm)
			r.Post("/register", h.Register)
			r.Get("/forgot", h.ForgotForm)
			r.Post("/forgot", h.Forgot)
			r.Get("/reset", h.ResetForm)
			r.Post("/reset", h.Reset)
		})

		r.Post("/settings/locale", h.SetLocale)

		// Playing without an account. One press, no form: it makes a
		// temporary player and signs the browser in as it. Everything that
		// player does is swept when their time is up.
		r.Post("/guest", h.StartGuest)

		// ---- authenticated
		r.Group(func(r chi.Router) {
			r.Use(h.RequireAuth)

			r.Post("/logout", h.Logout)
			r.Get("/app", h.Dashboard)

			// Several people, one device. A guest here is a player and
			// nothing else: they take a turn, they get a score, and they are
			// deleted with everything they did.
			r.Post("/players", h.AddLocalPlayer)
			r.Post("/players/seat", h.TakeSeat)
			r.Post("/players/{id}/remove", h.RemoveLocalPlayer)

			// Who this player may reach, for the picker dialogs. Friends and
			// the room they are standing in — which is the whole of it, on
			// purpose.
			//
			// Under /ui and not /api: this is the interface fetching for
			// itself, inside the session chain, behind the cookie and the
			// cross-site token. /api/v1 is mounted above that chain with a
			// bearer key and reads no cookie, and one prefix cannot mean both.
			r.Get("/ui/people", h.People)

			// Looking back over the round that just finished. The list of
			// past rounds is history and needs an account; one round's own
			// answers are part of that round, which a temporary player is
			// entitled to read for as long as it exists. Review refuses any
			// session but the reader's own, so opening it here widens who may
			// look at their own round and nothing else.
			r.Get("/history/{id}", h.Review)

			r.Get("/questions/{id}/comments", h.QuestionComments)
			r.Post("/questions/{id}/comments", h.PostQuestionComment)

			r.Route("/play", func(r chi.Router) {
				r.Get("/", h.PlaySetup)
				r.Post("/start", h.PlayStart)
				// One shared set per day, one attempt each. The mode existed in
				// the schema and the history filter with nothing to create it.
				r.Post("/daily", h.PlayDaily)
				r.Get("/round", h.PlayRound)
				r.Post("/answer", h.PlayAnswer)
				r.Post("/pause", h.PlayPauseClock)
				r.Post("/comment", h.PlayComment)
				r.Post("/rate", h.PlayRate)
				r.Post("/finish", h.PlayFinish)
				r.Post("/quit", h.PlayQuit)
				// Taking the phone in a match played round one device. It is
				// a press rather than a redirect on purpose: it is what stops
				// one person answering two questions in a row.
				r.Post("/seat", h.TakeTurn)
				r.Get("/result/{id}", h.PlayResult)
			})

			r.Route("/challenges", func(r chi.Router) {
				r.Get("/", h.Challenges)
				r.Get("/new", h.NewChallengeForm)
				r.Post("/new", h.CreateChallenge)
				// Somebody picked out of the room, and out of nowhere else.
				r.Post("/random", h.RandomChallenge)
				r.Post("/{id}/accept", h.AcceptChallenge)
				r.Post("/{id}/decline", h.DeclineChallenge)
				// The host ending their own match. Separate from declining
				// because the consequence is different: this one is for
				// everybody in it.
				r.Post("/{id}/cancel", h.CancelChallenge)
				// The host setting it going. One press opens a round for
				// everybody who accepted, at the same moment — which is what
				// makes a remote match a match rather than two people
				// answering the same questions on different days.
				r.Post("/{id}/start", h.StartChallenge)
			})

			// Rooms, which a temporary player may use and the rest of
			// messaging is not.
			//
			// A room is the one place in this application where strangers
			// meet, and meeting somebody is the whole of what anonymous play
			// was missing: a guest could play, and had nobody to play with.
			// So these routes sit outside RequireAccount, and the separation
			// that keeps them safe is a different one — a temporary room
			// holds temporary players only, enforced in the repository and
			// again by a trigger. Everything else about a room is unchanged,
			// including that every read and write below is gated on being a
			// member of the thread.
			r.Route("/messages", func(r chi.Router) {
				r.Get("/", h.Messages)
				r.Post("/new", h.CreateThread)
				r.Post("/{id}/join", h.JoinRoom)
				r.Post("/{id}/leave", h.LeaveThread)
				r.Get("/{id}/members", h.ThreadMembers)
				// The panel's numbers and previews on their own, for a page
				// that has heard something changed and does not want to throw
				// away a half-written message to find out what.
				r.Get("/summary", h.ConversationSummary)
				r.Get("/{id}", h.Messages)
				r.Post("/{id}", h.SendMessage)
				r.Get("/{id}/poll", h.PollMessages)
				// Reading is its own act with its own route, so nothing can
				// mark a message read as a side effect of fetching it.
				r.Post("/{id}/read", h.MarkThreadRead)
				r.Get("/{id}/receipts", h.Receipts)
				r.Post("/{id}/m/{message}/withdraw", h.WithdrawMessage)
				r.Post("/{id}/m/{message}/hide", h.HideMessage)

				// The parts of a thread that only an account has: a private
				// thread with one person, a group built out of friends, and
				// the per-thread settings that go with keeping either.
				r.Group(func(r chi.Router) {
					r.Use(h.RequireAccount)
					r.Get("/with/{username}", h.MessagesWith)
					r.Get("/new/group", h.NewGroup)
					r.Post("/{id}/mute", h.MuteConversation)
					r.Post("/{id}/delete", h.DeleteConversation)
				})
			})

			// Everything below needs an account. Anonymous play is a complete
			// game and deliberately nothing more: no saved progress, no
			// friends, no history. Enforced here rather than by leaving links
			// off a page, because a link is not a lock.
			// What people sent each other. Guarded by who is in the
			// conversation rather than by having an account, so a guessed id
			// reaches nothing and a voice note in a guest's room still plays.
			r.Get("/files/{id}", h.Files)

			// The live stream and the badge counts. Both are per-person —
			// the hub publishes to one subscriber and the counts are counted
			// over that person's own threads — so a guest listening on them
			// hears their own room and nothing else. Without this a room
			// would be live for an account and silent for a guest, which is
			// the same room behaving two ways.
			r.Get("/events", h.Events)
			r.Get("/ui/counts", h.UnreadCounts)

			r.Group(func(r chi.Router) {
				r.Use(h.RequireAccount)

				// What happened while they were away. Seven places write these;
				// nothing read them back before this.
				r.Get("/notifications", h.Notifications)
				r.Post("/notifications/read", h.MarkNotificationsRead)
				r.Get("/notifications/{id}/open", h.OpenNotification)

				r.Route("/friends", func(r chi.Router) {
					r.Get("/", h.Friends)
					r.Post("/{action}", h.FriendAction)
				})

				// A player's own questions: written by them, played only by the
				// friends they invite, never in the bank.
				r.Get("/my/questions", h.MyQuestions)
				r.Post("/my/questions", h.CreateQuestionSet)
				r.Get("/my/questions/{id}", h.MyQuestions)
				r.Post("/my/questions/{id}/add", h.AddQuestionsToSet)
				r.Post("/my/questions/{id}/delete", h.DeleteQuestionSet)
				r.Post("/my/questions/{id}/{question}/remove", h.RemoveFromSet)
				r.Post("/my/questions/{id}/{question}/edit", h.UpdateSetQuestion)
				r.Post("/my/questions/{id}/{question}/copy", h.CopySetQuestion)
				r.Post("/challenges/{id}/keep", h.KeepMatchQuestions)

				r.Get("/leaderboard", h.Leaderboard)

				r.Route("/support", func(r chi.Router) {
					r.Get("/", h.Support)
					r.Get("/new", h.SupportNewForm)
					r.Post("/new", h.SupportCreate)
					r.Get("/{id}", h.SupportThread)
					r.Post("/{id}/reply", h.SupportReply)
					r.Post("/{id}/close", h.SupportClose)
				})

				r.Get("/profile", h.MyProfile)
				r.Get("/profile/edit", h.EditProfileForm)
				r.Post("/profile/edit", h.EditProfile)
				r.Post("/profile/photo", h.UploadAvatar)
				r.Post("/profile/photo/remove", h.RemoveAvatar)
				r.Get("/u/{username}", h.Profile)

				r.Get("/history", h.History)

				// Changing a password, revoking sessions and deleting an
				// account are all about an account. A temporary player has
				// none of the three.
				r.Route("/settings", func(r chi.Router) {
					r.Get("/", h.Settings)
					r.Post("/password", h.ChangePassword)
					r.Post("/sessions/revoke", h.RevokeSessions)
					r.Post("/delete", h.DeleteAccount)
				})
			}) // end of the account-only group

			// ---- admin. RequireModerator covers content work; user
			// management and the audit trail need full admin.
			r.Route("/admin", func(r chi.Router) {
				r.Use(h.RequireModerator)

				r.Get("/", h.AdminDashboard)

				// The top bar's one search field. It answers a fragment for
				// the drop-down under the field, not a page.
				r.Get("/search", h.AdminSearch)

				// The level above a category. Admin-only, every one of them:
				// reshaping the taxonomy is structural, while writing a
				// category inside it is content work and stays with
				// moderators (D11). {id}/{action} is registered last or it
				// swallows the named routes above it.
				r.With(h.RequireAdmin).Get("/domains", h.AdminDomains)
				r.With(h.RequireAdmin).Get("/domains/new", h.AdminDomainForm)
				r.With(h.RequireAdmin).Get("/domains/{id}/edit", h.AdminDomainForm)
				r.With(h.RequireAdmin).Post("/domains/save", h.AdminDomainSave)
				r.With(h.RequireAdmin).Post("/domains/{id}/delete", h.AdminDomainDelete)
				r.With(h.RequireAdmin).Post("/domains/{id}/{action}", h.AdminDomainAction)

				// The taxonomy. Content work, so a moderator may add a
				// category and correct its names; deleting one is not, because
				// the cascade reaches the questions filed under it and every
				// answer ever given to one — the same reasoning that put bulk
				// question deletion behind full admin.
				r.Get("/categories", h.AdminCategories)
				r.Get("/categories/new", h.AdminCategoryForm)
				r.Get("/categories/{id}/edit", h.AdminCategoryForm)
				r.Post("/categories/save", h.AdminCategorySave)
				// Before the {id}/{action} route below, which would otherwise
				// read "reorder" as a category id.
				r.Post("/categories/reorder", h.AdminCategoryReorder)
				r.With(h.RequireAdmin).Post("/categories/{id}/delete", h.AdminCategoryDelete)
				r.Post("/categories/{id}/{action}", h.AdminCategoryAction)

				r.Get("/questions", h.AdminQuestions)
				// The same filter the listing reads, written to a file. A
				// literal segment, so it is matched ahead of {id} below.
				r.Get("/questions/export.csv", h.AdminExportQuestions)
				// Authoring. Until these existed the only ways to add or correct
				// a question were editing the bundled JSON and restarting, or
				// preparing a bulk import.
				r.Get("/questions/new", h.AdminQuestionForm)
				r.Get("/questions/{id}/edit", h.AdminQuestionForm)
				r.Post("/questions/save", h.AdminQuestionSave)
				r.Get("/questions/import", h.AdminImportForm)
				r.Post("/questions/import", h.AdminImportUpload)
				r.Get("/questions/import/template.csv", h.AdminImportTemplate)
				// Before the {id} route, or "bulk" would be read as an id.
				//
				// Admin, not moderator: with scope=filter and an empty search
				// this selects every question in the bank, and deleting cascades
				// into game_answers — every player's history with it. Managing
				// one user needs full admin; destroying all content should not
				// need less.
				r.With(h.RequireAdmin).Post("/questions/bulk", h.AdminQuestionBulk)
				r.Post("/questions/{id}/{action}", h.AdminQuestionAction)
				r.Get("/review", h.AdminReview)
				// Registered before the {action} catch-all so it is not
				// swallowed by it.
				r.Post("/review/{id}/translate", h.AdminTranslate)
				r.Post("/review/{id}/{action}", h.AdminReviewAction)
				r.Get("/rated", h.AdminRated)
				r.Post("/rated/{id}/dismiss", h.AdminRatingsDismiss)
				r.Get("/integrity", h.AdminIntegrity)
				r.Post("/integrity/verdict", h.AdminDuplicateVerdict)

				// Player remarks on questions. The schema has carried a hidden
				// flag for these since migration 0006 with nothing to set it.
				r.Get("/comments", h.AdminComments)
				r.Post("/comments/{id}/{action}", h.AdminCommentAction)

				r.Get("/support", h.AdminSupport)
				r.Get("/support/{id}", h.AdminSupportThread)
				r.Post("/support/{id}/reply", h.AdminSupportReply)
				r.Post("/support/{id}/update", h.AdminSupportUpdate)

				r.Group(func(r chi.Router) {
					r.Use(h.RequireAdmin)
					r.Get("/users", h.AdminUsers)
					// Email addresses and last-seen times leave the building
					// in this file, so it sits with the directory behind
					// RequireAdmin rather than with the content exports.
					r.Get("/users/export.csv", h.AdminExportUsers)
					// The drawer the directory's eye opens, fetched when it
					// is opened rather than rendered fifty times with the list.
					r.Get("/users/{id}/panel", h.AdminUserPanel)
					r.Get("/users/new", h.AdminNewUserForm)
					r.Post("/users/new", h.AdminCreateUser)
					r.Post("/users/{id}/{action}", h.AdminUserAction)
					r.Get("/audit", h.AdminAudit)
					r.Get("/audit/export.csv", h.AdminExportAudit)
				})
			})
		})
	})

	return r
}

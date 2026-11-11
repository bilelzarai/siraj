# Sirāj v2 — notes for Claude Code

Sirāj is a trilingual (Arabic / English / French) quiz app: Islamic knowledge is the flagship domain, next to general domains (sport, geography, business, science…). One Go binary serves the player website, the staff admin (templ + htmx) and a JSON API for the mobile app, on one PostgreSQL 18 database.

## The source of truth is the dev kit in `siraj-v2-dev-kit/`

Read it in this order — it overrides anything you remember:

1. `siraj-v2-dev-kit/docs/01-REQUIREMENTS.md` — what to build, with requirement IDs (BR, DM, LG, RL, AU, HM, GP, RS, RM, DU, CH, DV, MS, FR, LB, MQ, OT, AD, NF).
2. `siraj-v2-dev-kit/docs/04-TODO.md` — the task list, in order, with "Needs" and "Done when". Ticked boxes are merged.
3. `siraj-v2-dev-kit/docs/backend-architecture.md` + `03-BACKEND-CHANGES.md` — tables and the 119 endpoints (★ items in 03 override the architecture doc).
4. `siraj-v2-dev-kit/docs/02-SCREENS.md` — which prototype screen becomes which templ page.
5. `siraj-v2-dev-kit/docs/05-DESIGN-SYSTEM.md` + `docs/design/tokens/theme.css` — tokens and rules.
6. `siraj-v2-dev-kit/docs/06-DECISIONS.md` — every decision is answered (D-01…D-17). If a new one is needed, write it there.
7. `siraj-v2-dev-kit/docs/07-QA-CHECKLIST.md` — the pass before shipping.

The clickable prototypes are `siraj-v2-dev-kit/docs/design/player/index.html` (add `#route`) and `siraj-v2-dev-kit/docs/design/admin/*.html`. The prototype shows the look and the flow; its JavaScript uses sample data and fake timers — build the real logic from the requirements. When several prototype files touch one screen, the later file wins: `brand.js` → `arena.js` → `rounds.js`.

## Product rules the code must respect

1. **Your domains.** The player app works inside `user_domains`: Home, Play, setup, search, mastery, leaderboard filters and the question of the day. Only Explore, Add a domain and onboarding list every domain. Every player, guests included, keeps at least one. Playing in a domain that isn't yours adds it first (`source = played`).
2. **Islamic content only** (`user_settings.islamic_only`): general domains are hidden everywhere and can't be added; the rows stay in `user_domains` (D-05).
3. **Neutral shell, themed domains.** A domain's `color`, `icon` and `motif` theme its screens through `data-dom` / `--d`; the geometric pattern belongs to `family = islamic` only.
4. **Three languages for every text a player reads**, in `*_translations`; the API language comes from `Accept-Language` (or `?lang=`), the website from `users.lang` → cookie → header.
5. **Guests are real users** (`users.kind = 'guest'`, 24 h). Upgrading keeps the same id, so points carry over.
6. A quick game is always 5 questions; a player-made set needs 5+ questions.
7. **Scores, XP, ratings and timers are the server's** (`internal/modules/games/scoring.go` is the one scoring function). Device rounds are re-scored on save.
8. **Admin is staff-only and web-only**; every staff write also writes `audit_log` in the same transaction.
9. **Hidden challenge scores:** until a challenge closes, other players' scores never leave the server (one DTO builder, with a test).

## Commands

| What | Command |
| --- | --- |
| First run (tools, .env, assets) | `make setup` |
| **Everything up** (db, codegen, assets, server :8080, worker — background) | `make up` |
| **Everything down** (server, worker, watchers, db; `keep=1` keeps the db) | `make down` |
| Restart the app, keeping the database | `make restart` |
| What is running + health | `make status` (logs: `make logs`, `make logs who=worker`) |
| Start PostgreSQL 18 (project `siraj-v2`, host port 5442) | `make db-up` |
| Run the server on :8080 (migrations first, built assets) | `make run` |
| Live reload: templ + Vite on :5173 + air | `make dev` |
| Regenerate after editing SQL or templ | `make gen` |
| Build the front-end only | `make assets` |
| Tests (needs the database) | `make test` |
| **Everything a change must pass** (vet, types, generated, tests) | `make check` |
| Type-check the front-end (Vite strips types without checking) | `make typecheck` |
| First staff account (real credentials, no samples) | `make admin email=you@example.com username=you password='…'` |
| Empty a database of content and players (keeps the schema) | `make db-clear` |
| Development content to work against (`clear=1` removes it) | `make dev-seed` |
| Check the OpenAPI document / build its HTML reference | `make api-lint` · `make api-docs` |
| Layout check (every screen, 360–1440 px) | `make ui-check login=… password=…` |
| New migration | `make migrate-new name=progress` |

Each test binary creates and migrates a database of its own (`siraj_test_<package>`) and skips itself when PostgreSQL is not running. **No migration seeds content:** a test that needs something to play with writes it through `dbtest.Seed`.

## How the code is organised

```
cmd/server, cmd/worker      main + scheduled jobs
cmd/createuser              create the first staff account (make admin)
internal/app                wiring: pool, services, modules, router (one place)
internal/config             env vars → Config (.env is read, walking up directories)
internal/db                 pool.go, migrate.go (embedded), migrations/ (19 so far), queries/, sqlc/ (generated), dbtest/
internal/http               router.go, middleware/ (lang, auth, csrf, view), render/ (templ → Gin), apiutil/ (JSON envelope)
internal/i18n               loader + locales/{ar,en,fr}.json  (one key set, enforced by a test)
internal/modules/<feature>  service.go (rules) + api.go (JSON) + web.go (pages) + *_test.go
internal/platform           apperr, authctx, mailer
internal/view               view.Ctx: the one view model every page receives
web/templates               layouts/, components/, pages/, partials/  (*.templ, generated *_templ.go)
web/assets                  css/app.css (design tokens + the kit's component classes), js/main.ts, js/alpine/
web/public                  icons.svg (sprite generated from the kit), lamp.svg
tests/                      integration and API contract tests against a real PostgreSQL
```

Rules:
- **Direction:** handler → service → `sqlc.Queries`. Handlers never import sqlc queries; services never import Gin or templ (`authctx.Principal` and `view.Domain` are plain structs).
- `web.go` and `api.go` of a module call the same service (NF-04).
- **Pages never import a module** (it would be a cycle): a module maps its structs onto the page's own types.
- **Responses:** `{"data": …}`, lists add `"page"`, errors `{"error": {code, message, fields}}` — always through `apiutil`.
- **Errors:** services return `apperr.*`; anything else becomes a logged 500.
- **Multi-row changes run in `db.WithTx`;** lock the user row (`q.LockUser`) when a rule depends on a count.
- **Never edit generated code** (`internal/db/sqlc`, `*_templ.go`); run `make gen`. Never edit a merged migration.
- **Every user-visible string** goes into the three locale files in the same change; no hex colour in a template.

## Status (2026-10-09)

Every screen of `docs/design/screens/player` and `docs/design/screens/admin` now has a
page behind it. Merged, with tests: phases 0–5 of `04-TODO.md`.

- **T-00** every decision answered (D-01…D-17).
- **Skeleton (T-01…T-06):** Vite + Tailwind v4 + templ, design tokens, layouts,
  navigation, icon sprite, i18n (ar/en/fr, 930 keys), theme switch, CSRF, embedded assets.
- **Identity and catalog (T-07…T-15):** auth, guests and their locks, Your domains,
  Islamic-only, welcome / sign in / sign up / guest / onboarding (with its search and
  category chips), domain theming, the domain switch, and O01–O03 as the drawer and
  modal the design shows (htmx over the screen, full pages as the no-JS fallback).
- **Solo game (T-16…T-23):** questions and games migrations, the engine (scope,
  scoring, lifelines, server timer, finish transaction), play hub, the full topic
  picker on setup, game, results with review, ratings, comments, reports, question of
  the day, domain page.
- **Progress (T-24…T-27):** XP and levels, mastery, streaks, daily goal, achievements,
  leaderboards; Home's player card, P24, P25 (mastery sort, 12-week heatmap), P26.
- **Player content and account:** friends, requests, blocks, invite links, people
  search; notifications; My questions with the 5-question rule; Settings; Contact us.
- **Live rooms (T-28…T-33):** `internal/modules/rooms` — host (P12), join by code
  (P13), lobby with ready and start (P14), one shared game for the whole room (P15)
  and the podium (P16).
- **Challenges and device rounds (T-34…T-39):** `internal/modules/challenges` — the
  three boxes (P19), "Set up your round" on the shared topic picker (P20/P20b), and
  pass-the-device with its hand-off (P21). Scores stay hidden until a challenge closes:
  `challenges/view.go` is the one DTO builder that reads them.
- **Duels (T-17/T-18 screens):** `internal/modules/duel` — the queue, the pairing
  transaction, P17's searching state and P18's score bar, with Elo-style settling.
- **Messages (T-42/T-43):** `internal/modules/messages` — inbox, thread, send,
  unread badge.
- **Admin console (T-47…T-50):** its own Vite entry and layout, A01 rebuilt to the
  design (4 KPIs, games chart, category table, needs attention, players by domain and
  language, activity) and the eleven screens on one shared table with the design's
  toolbar, avatar cell, badges and result range. Every staff write records `audit_log`.

**`/api/v1` is the external contract**, and `api/openapi.yaml` is that contract
written down (OpenAPI 3.1, 132 operations). The binary serves it at
`GET /api/v1/openapi.yaml`; `tests/openapi_test.go` fails when the document and the
router disagree in either direction, so adding an endpoint means adding its lines. A mobile app or any other client sends
`Authorization: Bearer <token>` (from `/auth/guest` or `/auth/login`) and
`Accept-Language`; it never sees a cookie and never needs a CSRF token — `CSRF()`
leaves `/api/` calls that carry no session alone. Cross-origin browsers are allowed
only through `CORS_ORIGINS`; the wrong verb on a real path answers 405 with `Allow`,
and an unknown path answers the JSON error envelope, never an HTML page.

**Every module answers on `/api/v1` too.** Rooms (36–46, 119), duels (47–50),
challenges and pass-the-device rounds (51–62, 118), messages and uploads (75–86), the
category and search endpoints (20, 21) and the live stream now have JSON handlers over
the same services as their screens (NF-04). `tests/api_routes_test.go` fails if one of
them stops being mounted. Only the admin console stays web-only, by rule.

**The two ways in are not the same form.** Signing in asks for a number or an
address and sends a code. Creating an account is "1 · Your details → 2 · Verify":
the display name and a password (all five checks — 8 characters, A-Z, a-z, a digit,
a symbol) are given first, and the button stays locked until they pass, in the page
and again in `auth.signupDetails`. They wait on the `auth_codes` row — the name as
itself, the password already hashed — so verifying the six digits creates the
account in one step, with no plain password travelling back through a form. A
number nobody uses that arrives from the *sign in* tab still gets the old "name it"
step (edge case 24), and a number that already has an account signs in whichever tab
it was typed on. `/rooms/join` is open to visitors: typing a real code makes the
guest (`auth.Web.EnsureGuest`), a wrong one costs nothing.

**Liveness is polling, not websockets.** The lobby, the live board, the duel search and
an open message thread re-fetch themselves through htmx every 2–5 s. `/ws` is still
unbuilt; swapping the poll for a socket is a change inside those four templates.

Migrations, in the order they were created: 1 identity, 2 catalog, 3 (was the catalog
seed, now empty), 4 questions, 5 (was the question seed, now empty), 6 games + rooms,
7 `game_questions.served_at`, 8 progress, 9 challenges, 10 social, 11 sets, 12 messages,
13 notifications + support + audit_log, 14 duel queue, 15–16 room chat and its files,
17 the unique indexes the leaderboard views need to refresh concurrently, 18 phone
sign-in and `auth_codes`, 19 the name and password a create-account code carries.

`make dev-seed` is the exception that proves the rule below: a tool, not content
the product ships with. It writes a catalog and a bank whose slugs all start with
`dev-`, so `make dev-seed clear=1` takes back exactly what it wrote; it refuses to
run against a production database, and neither the server nor the worker calls it.
It exists so the admin console's screens have rows on them while they are built.

**No content ships with the code.** Migrations 3 and 5 used to insert the prototype's
six domains and its sample bank; they are empty files now (the version numbers stay,
because existing databases already applied them). Domains, categories and questions are
written by staff in the admin console, and `make db-clear` empties a database that was
filled before. A fresh install therefore shows empty states and refuses to start a game
with `no_questions` until the first questions are published — that is the intended
behaviour, not a bug.

Next, in `04-TODO.md` order: **T-52** the rest of the contract tests, **T-53** the QA
pass and **T-55** deploy.

**Every CSV import reads the same size of file.** `internal/platform/csvlimit` holds
the one pair of numbers — 5 000 rows, 24 MB — and the admin importer, a player's own
set importer, the staff hint and the "too big" message all read it from there, so the
screens can never promise a size the reader refuses. It lives outside the modules
because a page may not import one.

Known gaps: the admin still needs the A/R/E/S shortcuts and the per-user domains
panel; the duplicates job runs on demand; `/ws` (endpoint 111) is still an SSE stream
(`GET /api/v1/events`) rather than a socket. **Before a deploy:** no SMS gateway is
implemented (`internal/platform/sms` logs the code), and an empty `SMTP_ADDR` logs the
mail — with both unset nobody can sign in, because the six-digit code never leaves the
server.

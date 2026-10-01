# Sirāj — سِراج

*by Peecso*

**لعبة إسلامية تجمع بين المتعة والتعلم**
اختبر معلوماتك، تحدَّ أصحابك وعائلتك، واكتشفوا الإسلام بطريقة مختلفة وممتعة.

A multiplayer Islamic quiz game by **Peecso**: **Go + templ + PostgreSQL**, server-rendered,
trilingual (العربية / English / Français) with full RTL support.

---

## What it does

| Area | Details |
|---|---|
| **Play without an account** | One press on the landing page starts a round — no sign-up, no email. A guest can play solo, open matches and take invitations; they cannot save progress, add friends or send messages, and everything they did is deleted a day after they stop. An account unlocks the rest |
| **One device, several players** | Add the people around you by name and pass the phone. A match on one device is a **hot seat**: everybody answers question one, then everybody answers question two, and so on. The answer and the explanation are held back until the last player at the device has committed, and a named handover screen stands between two turns so nobody answers somebody else's question. Each player keeps their own clock, their own score and their own place on the scoreboard, and none of them needs an account |
| **Quiz** | 8 categories (Qur'an, Sīrah, Prophets, Fiqh, Hadith, History, Ramadan, Akhlāq), 3 difficulties, 25s timer, speed bonus + streak multiplier, an explanation after every answer. The setup screen shows how many questions each choice can actually draw and disables the ones the bank cannot fill |
| **Daily round** | One shared question set per day, drawn from the date so everyone gets the same one, one attempt each |
| **Matches** | One-to-one, free-for-all up to ten, or teams whose scores are added by side. Everyone plays the *same* question set independently. Several invitations can wait at once; a player is inside one match at a time, which the database enforces rather than the screens. A challenge round cannot be paused or resumed — a question left unanswered is lost and the round moves on |
| **Rooms** | Open spaces anybody may create and join, one at a time. The people in your room are reachable for a message or a challenge without being friends, and stop being so the moment either of you leaves. "Random opponent" draws from the room and from nowhere else |
| **Messenger** | Private 1:1 threads, group threads with friends, and room threads. You can write to friends and to the people in your room — never to the whole site. Unread counts, live delivery via SSE + polling fallback |
| **Friends** | Fuzzy user search, request / accept / decline / remove, presence indicator |
| **History** | Every round stored; per-question review showing your answer, the correct one, and why |
| **Profile** | Level curve from XP, accuracy per category, 7-day activity chart, 9 unlockable badges, public profiles at `/u/{username}` |
| **Leaderboard** | Global and friends-only, ranked by XP |
| **Support** | Players open categorised conversations (suggestion, question, bug, account, abuse). Staff triage by kind, status, priority and assignee, reply with saved responses, and keep internal notes the player never sees |
| **Admin** | Roles (player / moderator / admin), user CRUD with role assignment, suspension, a question editor with near-duplicate warning, bulk import, translation review queue (approve, reject, or machine-translate a missing language), comment moderation, content-integrity report, and an audit log of every privileged action |
| **Notifications** | Friend requests, duel invitations and outcomes, replies — with a history page, not only a live badge |
| **i18n** | 3 locales, RTL-native layout, per-locale question content, content direction scoped independently of page direction |
| **Accounts** | bcrypt, DB-backed sessions, CSRF, login rate limiting, session management, account deletion |

## Stack

- **Go 1.25+** — stdlib `net/http` behind [chi](https://github.com/go-chi/chi) for routing
- **[templ](https://templ.guide)** — type-safe compiled HTML templates
- **PostgreSQL 16** via [pgx/v5](https://github.com/jackc/pgx)
- **No frontend framework, no CDN.** ~25 KB of hand-written CSS and vanilla JS,
  all embedded in the binary.

## Quick start

```bash
cp .env.example .env          # defaults point at the docker-compose database
make db-up                    # PostgreSQL on :5434
make tools                    # installs the templ CLI
make run                      # http://localhost:8080
```

The first boot applies migrations and seeds the 64-question bank automatically.
Press **Play without an account** to start straight away, or register to keep
what you play.

```bash
make check                    # go vet + tests
make build                    # single self-contained binary in bin/
```

Deploying it — dev, test and production, the database, and what must be set
before production will start — is in **[DEPLOY.md](DEPLOY.md)**.

### Configuration

All settings come from the environment (see `.env.example`):

| Variable | Default | Notes |
|---|---|---|
| `DATABASE_URL` | — | **required** |
| `SESSION_SECRET` | random in dev | **required** in production; signs the flash cookie |
| `APP_ADDR` | `:8080` | listen address |
| `APP_ENV` | `development` | `production` switches to JSON logs and enforces the two rules below |
| `SECURE_COOKIES` | `false` | set `true` behind HTTPS; **required** in production |
| `BASE_URL` | derived | absolute URL used in links; **required**, and must be `https://`, in production |
| `TRUSTED_PROXY_HOPS` | `0` | how many reverse proxies are in front. `0` ignores `X-Forwarded-For` entirely |
| `SESSION_LIFETIME` | `720h` | how long a session cookie lasts |
| `DEFAULT_LOCALE` | `ar` | last-resort locale |
| `SEED_ON_START` | `true` | upserts the bundled question bank on boot, leaving locally edited translations alone |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_USERNAME` / `SMTP_PASSWORD` / `SMTP_FROM_EMAIL` / `SMTP_FROM_NAME` / `SMTP_STARTTLS` | *(unset)* | outbound mail; with no host the mailer logs instead of sending |
| `TRANSLATE_PROVIDER` | `none` | `claude` (needs `ANTHROPIC_API_KEY`, optionally `ANTHROPIC_MODEL`) or `libretranslate` (needs `LIBRETRANSLATE_URL`, optionally `LIBRETRANSLATE_API_KEY`) |
| `STATIC_DIR` | *(unset)* | serve CSS/JS from disk instead of the embedded copy |

## Layout

```
cmd/server/          entrypoint: config, wiring, graceful shutdown, janitor, -healthcheck
cmd/sirajctl/        admin CLI: the first admin, password resets, platform totals
assets.go            embeds static/ (go:embed cannot reach above its package)
internal/
  config/            environment parsing
  database/          pool, migrator, question seeder
    migrations/      *.sql, applied once in filename order
    seed/            questions.json — the bundled question bank
  models/            domain types + pure logic (level curve, accuracy, outcomes)
  repository/        all SQL, grouped by aggregate
  service/           auth, game rules & scoring, social, players/guests, SSE hub, presence
  handlers/          HTTP: routing, middleware, request/response mapping
  i18n/              catalogs + negotiation
    locales/         ar.json · en.json · fr.json
  views/             templ templates + view helpers
static/              css, js, favicon
```

The dependency direction is one-way: `handlers → service → repository → database`.
`models` is pure and depends on nothing.

## Design notes

**RTL is structural, not a skin.** The stylesheet uses logical properties
throughout (`margin-inline-start`, `inset-inline-end`, `border-start-start-radius`),
so `dir="rtl"` mirrors the entire application with no mirrored rules.

**Content direction is separate from page direction.** A round keeps the language
it started in, so an English question can appear inside an Arabic interface. The
question card carries its own `lang`/`dir`, which stops the bidi algorithm from
visually reordering the sentence.

**The client is never trusted with scoring.** The submitted question position must
match the server's cursor, and points are computed server-side. A duplicate
submission hits a unique index on `(session_id, position)` and returns 409.

The clock is the server's too. `game_sessions.served_at` is stamped the first
time a position is rendered and cleared when the round advances, so the elapsed
time used for the speed bonus is never smaller than what the server observed,
less a grace for the round trip. A client reporting `timeMs: 0` on every answer
gets the server's figure, not the full bonus. A refresh does not restart the
clock, because the stamp is only written once per position.

**One active round per player,** enforced by a partial unique index rather than
application logic, so a double-submit cannot fork someone's progress.

**Locale-aware content, with graceful fallback.** Questions, categories and badges
are all translated in their own tables, and every query `COALESCE`s to Arabic so a
missing translation degrades instead of rendering blank.

**The SSE stream carries no content** — only a "something changed" nudge. Clients
re-fetch through the normal authorised endpoints, so nothing sensitive rides the
event stream. The hub is process-local; running more than one instance means
swapping it for Postgres `LISTEN/NOTIFY`.

## Adding content

Questions live in `internal/database/seed/questions.json`:

```json
{
  "id": 109, "category": "quran", "difficulty": 2, "correct": 1,
  "t": {
    "ar": { "prompt": "…", "choices": ["…","…","…","…"], "explanation": "…" },
    "en": { "prompt": "…", "choices": ["…","…","…","…"], "explanation": "…" },
    "fr": { "prompt": "…", "choices": ["…","…","…","…"], "explanation": "…" }
  }
}
```

`correct` is the 0-based index into `choices`. Restart the server and the seeder
upserts it — editing an existing `id` updates the row in place. A translation
that has since been edited through the admin editor, an import, or an approved
machine translation is left alone: its `source` is no longer `seed`, and the
bundled file does not outrank a human.

The bulk importer reads the same shape and treats `id` the same way, so a file
re-imported after an edit updates the rows it names rather than inserting copies.

Or use `/admin/questions/new`, which writes the same thing one language at a
time and warns before saving a prompt that already reads almost the same as an
existing one.

Adding a **language** means: append to `i18n.Supported`, add
`internal/i18n/locales/<code>.json`, add the locale to each question and to the
`category_translations` / `badge_translations` tables. `go test ./internal/i18n`
fails if any catalog is missing a key or has mismatched format verbs.

## Testing

```bash
go test ./...            # unit tests
python3 scripts/check-i18n.py   # every T() call matches its key's arity
./scripts/smoke.sh       # 240 end-to-end HTTP checks against a running server
./scripts/reset-test-data.sh    # clear player data, keep the content bank
```

> `scripts/reset-test-data.sh` uses `DELETE`, not `TRUNCATE ... CASCADE`.
> `questions.created_by` references `users`, and `TRUNCATE users CASCADE`
> follows that edge regardless of `ON DELETE SET NULL` — it wipes the entire
> question bank.

Covers the scoring model (speed bonus, streak caps, and the server-side clock
that stops a client understating its own time), the XP level curve, catalog
completeness across all three locales, CSRF issue/verify, flash-cookie signing
and forgery rejection, login rate limiting, forwarding-header parsing from the
right, open-redirect rejection on `?next=` and on a crafted `Referer`, and the
import parser including the byte-order mark its own CSV template carries.

## Deployment

```bash
SESSION_SECRET=$(openssl rand -hex 32) \
BASE_URL=https://siraj.example \
docker compose --profile full up -d
```

Both variables are required: the `app` service runs as `APP_ENV=production`, and
production refuses to start without an `https://` `BASE_URL` and
`SECURE_COOKIES=true`. It expects a TLS terminator in front of it — session
cookies without `Secure` travel in clear over any plain-HTTP hop, and one
forgotten variable must not silently downgrade every session on the deployment.

Set `TRUSTED_PROXY_HOPS` to the number of proxies ahead of the process (`1` for a
single reverse proxy, `2` for a CDN in front of that). It defaults to `0`, which
ignores `X-Forwarded-For` altogether — the login and password-reset limiters key
on the peer address, and a header anyone can set must not decide who is being
throttled. The forwarding chain is read from the *right*, because everything left
of our own proxies was written by someone we do not control.

Health check: `GET /healthz`, which also backs the container's own
`HEALTHCHECK` — the binary is its own probe (`server -healthcheck`), so the image
needs no curl and no shell.

**Content-Security-Policy.** Scripts are `'self'` only; there is no inline
script anywhere, including the pre-paint theme boot, which is why it lives in
`static/js/boot.js`. Styles keep `'unsafe-inline'` because templ emits style
attributes.

## Administration

The first admin cannot be created through the web UI, because `role` defaults
to `player` and nothing in the app can grant privilege from nothing. Use the
CLI:

`make build` produces both `bin/server` and `bin/sirajctl`, and the Docker image
carries both at `/app/server` and `/app/sirajctl`.

```bash
make build
./bin/sirajctl users                  # every account with its role
./bin/sirajctl promote <username>     # grant admin
./bin/sirajctl admins                 # list them
./bin/sirajctl stats                  # platform totals
./bin/sirajctl whois <username>       # inspect one account
```

`sirajctl demote` refuses to remove the last admin, and so does the web UI.

Passwords are bcrypt hashes; nothing can read one back — not this tool, not an
admin, not a database query. Setting a new one is the only way into a locked-out
account, and it signs out every device that account had open:

```bash
read -rs NEWPASS && printf '%s' "$NEWPASS" | ./bin/sirajctl passwd <username>
```

Piping it keeps the password out of your shell history and out of `ps`, where
any other user on the machine could read it from the argument list.

**Roles.** `player` is the default. `moderator` reaches the content half of the
admin area (questions, import, translation review, integrity report).
`admin` additionally manages accounts and sees the audit log. An unprivileged
account gets a 404 rather than a 403, so the admin area does not advertise
its own existence.

## Bulk question import

`/admin/questions/import` accepts JSON (the same shape as the bundled seed
file) or CSV (one row per language, rows sharing an `id` merge into one
question). Download the filled-in template from that screen.

Every upload runs as a **dry run first**: the preview reports what would be
added, updated, skipped and rejected, with a reason per row, and writes
nothing. Re-select the file and confirm to apply it. A question whose prompt is
≥85% similar to an existing one is skipped rather than silently duplicated.

## Machine translation

Set `TRANSLATE_PROVIDER` to `claude` or `libretranslate` to enable it. **Both
need an API key** — with neither configured the admin UI offers the question
editor and bulk import only, which is the default.

`/admin/review` has two halves: translations awaiting review, each with approve
and reject per language, and questions missing a language entirely. When a
provider is configured, each missing language gets a translate button; the source
is the Arabic text where there is one, and never a translation that is itself
still awaiting review.

Machine output is written with `needs_review = true`, and **every player-facing
query filters unreviewed translations out**. A machine rendering of a Qur'anic
verse or a hadith therefore cannot reach a player until a human approves it in
`/admin/review`. That gate is deliberate: a plausible-but-wrong translation of
scripture is a real harm, not a cosmetic defect.

# Sirāj — سِراج

*by Peecso*

**لعبة إسلامية تجمع بين المتعة والتعلم**
اختبر معلوماتك، تحدَّ أصحابك وعائلتك، واكتشفوا الإسلام بطريقة مختلفة وممتعة.

A multiplayer Sirāj quiz game by **Peecso**: **Go 1.26 + templ + PostgreSQL 16**,
server-rendered, trilingual (العربية / English / Français) with full RTL
support. Alpine for local state and Vite for the build; no framework rendering
the page, no CDN, one self-contained binary carrying the server, the admin CLI
and the built assets.

---

## Where everything is written down

One fact lives in exactly one file. This README answers *what is this* and *how
do I run it*; everything else is one click away.

| File | Answers |
|---|---|
| **README.md** *(you are here)* | What it is, how to run it, how to add content and administer it |
| **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** | What must be true: principles, the domain and its invariants, layers, routes, the directory layout, guarantees, decisions |
| **[STACK.md](docs/STACK.md)** | What it runs on: every piece with its version and its boundary, every command, every environment variable, containers, release, CI gates |
| **[TODO.md](docs/TODO.md)** | What is left to do: 24 prompts in order, each with the files, the code and the checks that close it |
| **[DEPLOY.md](docs/DEPLOY.md)** | The operator runbook: dev, test and production, and what must be set before production starts |

---

## What it does

| Area | Details |
|---|---|
| **Play without an account** | One press on the landing page starts a round — no sign-up, no email. A guest can play solo, open matches and take invitations; they cannot save progress, add friends or send messages, and everything they did is deleted a day after they stop. An account unlocks the rest |
| **One device, several players** | Add the people around you by name and pass the phone. A match on one device is a **hot seat**: everybody answers question one, then everybody answers question two, and so on. The answer and the explanation are held back until the last player at the device has committed, and a named handover screen stands between two turns so nobody answers somebody else's question. Each player keeps their own clock, their own score and their own place on the scoreboard, and none of them needs an account |
| **Quiz** | 8 categories (Qur'an, Sīrah, Prophets, Fiqh, Hadith, History, Ramadan, Akhlāq), 3 difficulties, 25s timer, speed bonus + streak multiplier, an explanation after every answer. The setup screen shows how many questions each choice can actually draw and disables the ones the bank cannot fill. **Subject areas sit above the categories** — an admin creates Sport or History beside Islamic from `/admin/domains`, and a round can be drawn from a whole area or one category |
| **Daily round** | One shared question set per day, drawn from the date so everyone gets the same one, one attempt each |
| **Matches** | One-to-one, free-for-all up to ten, or teams whose scores are added by side. Everyone plays the *same* question set independently. Several invitations can wait at once; a player is inside one match at a time, which the database enforces rather than the screens. A challenge round cannot be paused or resumed — a question left unanswered is lost and the round moves on |
| **Rooms** | Open spaces anybody may create and join, one at a time. The people in your room are reachable for a message or a challenge without being friends, and stop being so the moment either of you leaves. "Random opponent" draws from the room and from nowhere else |
| **Messenger** | Private 1:1 threads, group threads with friends, and room threads. You can write to friends and to the people in your room — never to the whole site. Unread counts, live delivery via SSE + polling fallback |
| **Friends** | Fuzzy user search, request / accept / decline / remove, presence indicator |
| **History** | Every round stored; per-question review showing your answer, the correct one, and why |
| **Profile** | Level curve from XP, accuracy per category, 7-day activity chart, 9 unlockable badges, public profiles at `/u/{username}` |
| **Leaderboard** | Global and friends-only, ranked by XP |
| **Support** | Players open categorised conversations (suggestion, question, bug, account, abuse). Staff triage by kind, status, priority and assignee, reply with saved responses, and keep internal notes the player never sees |
| **Admin** | Roles (player / moderator / admin), user CRUD with role assignment, suspension, a category editor, a question editor with near-duplicate warning, bulk import, translation review queue (approve, reject, or machine-translate a missing language), comment moderation, content-integrity report, and an audit log of every privileged action |
| **Notifications** | Friend requests, duel invitations and outcomes, replies — with a history page, not only a live badge |
| **i18n** | 3 locales, RTL-native layout, per-locale question content, content direction scoped independently of page direction |
| **Accounts** | bcrypt, DB-backed sessions, CSRF, login rate limiting, session management, account deletion |

---

## Run it

```bash
./run.sh                      # http://localhost:8080
```

One command from a clean clone: it writes `.env` from `.env.example` with a
generated `SESSION_SECRET`, brings up PostgreSQL in Docker (container
`siraj-game-db`, database `siraj-db` on :5434 — the handle is
[STACK.md §4](docs/STACK.md)), installs the templ CLI if it is missing, regenerates
the templates and serves from source, so a CSS or JS edit lands on reload.
`PORT=9090 ./run.sh` moves the port.

Equivalently, by hand:

```bash
make db-up                    # PostgreSQL on :5434
make tools                    # installs the templ CLI, pinned to go.mod
make run                      # http://localhost:8080
```

**Migrations apply on boot. Content and privilege do not.** Two steps are
deliberately a command somebody runs:

```bash
make build
./bin/sirajctl seed                 # loads the bundled 64-question bank
./bin/sirajctl promote <username>   # the first admin
```

`SEED_ON_START` stays `false`: content arrives when somebody asks for it, not on
every restart. Nothing in the application can grant privilege from nothing.

`make help` lists every target — **[STACK.md §4](docs/STACK.md)** has the full table
and **[STACK.md §5](docs/STACK.md)** every environment variable with its default.

## Test it

```bash
make check                      # go vet + the full suite, against a real database
python3 scripts/check-i18n.py   # every T() call matches its key's arity
./scripts/smoke.sh              # end-to-end HTTP walk against a running server
./scripts/reset-test-data.sh    # clear player data, keep the content bank
```

The suite never touches the development database
([ARCHITECTURE.md §10](docs/ARCHITECTURE.md)). The smoke script, by contrast, drives
exactly one — the local `deploy/compose.yaml` database — through psql, through the
server and through `sirajctl`, so the server it is aimed at has to be that same
one:

```bash
make db-up
DATABASE_URL=$(grep -m1 '^DATABASE_URL=' .env | cut -d= -f2-) \
  STATIC_DIR=./static go run ./cmd/server &
./scripts/smoke.sh
```

Pointing the server anywhere else stops the run after registration with the URL
it expected, rather than reporting the whole admin half as missing pages.
Override the target with `SMOKE_DATABASE_URL`, and the container it resets with
`DB_CONTAINER` / `DB_USER` / `DB_NAME` / `DB_PORT`.

> `scripts/reset-test-data.sh` uses `DELETE`, not `TRUNCATE ... CASCADE`.
> `questions.created_by` references `users`, and `TRUNCATE users CASCADE`
> follows that edge regardless of `ON DELETE SET NULL` — it wipes the entire
> question bank.

What each layer of the suite is for is **[ARCHITECTURE.md §10](docs/ARCHITECTURE.md)**.

## Deploy it

**[DEPLOY.md](docs/DEPLOY.md)** is the runbook. The short version: the image carries
the server, the CLI and the assets, migrates on boot and probes itself
(`server -healthcheck` behind `GET /healthz`), so it needs no curl and no shell.

```bash
docker compose -f deploy/compose.yaml up -d          # development: one command, no configuration

SESSION_SECRET=$(openssl rand -hex 32) \
BASE_URL=https://siraj.example \
docker compose -f deploy/compose.yaml -f deploy/compose.prod.yaml up -d
```

The base file is what a clean clone runs — database and application, no
configuration, no secret to supply. The overlay is production, and both
variables are required there: it sets `APP_ENV=production`, and production
refuses to start without an `https://` `BASE_URL` and `SECURE_COOKIES=true`. It expects a TLS terminator in front of it — a session
cookie without `Secure` travels in clear over any plain-HTTP hop, and one
forgotten variable must not silently downgrade every session on the deployment.
Set `TRUSTED_PROXY_HOPS` to the number of proxies ahead of the process — it
defaults to `0`, which ignores `X-Forwarded-For` altogether
([STACK.md §5](docs/STACK.md)).

**Content-Security-Policy.** Scripts are `'self'` with no inline script
anywhere — which is why even the pre-paint theme boot is its own file. Styles
still keep `'unsafe-inline'`, because the templates emit style attributes; what
it would take to drop it is [ARCHITECTURE.md §8.1](docs/ARCHITECTURE.md) and
[TODO.md](docs/TODO.md) prompt 9c.

## The public endpoint

One read-only, credentialed, versioned route over the question bank — the only
way into this system that is not a browser session.

```bash
./bin/sirajctl apikey new "mirror for the mobile app"   # shown once, never recoverable
curl -H "Authorization: Bearer siraj_…" \
  "http://localhost:8080/api/v1/questions?locale=en&domain=islamic&limit=50"
```

It serves what the game draws from, under the same five conditions — so
nothing awaiting review ever leaves it. The contract is
**[docs/api.md](docs/api.md)** in prose and
**[api/openapi.yaml](api/openapi.yaml)** for a machine.

## Administer it

The first admin cannot be created through the web interface, because `role`
defaults to `player`. `make build` produces both `bin/server` and
`bin/sirajctl`, and the image carries both at `/app/server` and `/app/sirajctl`:

```bash
./bin/sirajctl users                  # every account with its role
./bin/sirajctl promote <username>     # grant admin
./bin/sirajctl demote <username>      # refuses to remove the last admin
./bin/sirajctl admins                 # list them
./bin/sirajctl whois <username>       # inspect one account
./bin/sirajctl stats                  # platform totals
./bin/sirajctl seed                   # load or upsert the bundled bank
```

Passwords are bcrypt hashes; nothing can read one back — not this tool, not an
admin, not a database query. Setting a new one signs out every device that
account had open:

```bash
read -rs NEWPASS && printf '%s' "$NEWPASS" | ./bin/sirajctl passwd <username>
```

Piping it keeps the password out of your shell history and out of `ps`, where
any other user on the machine could read it from the argument list.

**Roles.** `player` is the default. `moderator` reaches the content half of the
admin area; `admin` additionally manages accounts, destroys content and sees the
audit log. An unprivileged account gets a 404 rather than a 403, so the admin
area does not advertise its own existence
([ARCHITECTURE.md §2.6](docs/ARCHITECTURE.md)).

## Add content

Three ways in, all writing the same rows:

- **`/admin/questions/new`** — one language at a time, with a warning before saving a prompt that already reads almost the same as an existing one.
- **`/admin/questions/import`** — JSON or CSV (one row per language; rows sharing an `id` merge into one question). Every upload is a **dry run first**: the preview reports what would be added, updated, skipped and rejected, with a reason per row, and writes nothing. Re-select the file and confirm to apply it. A prompt ≥85% similar to an existing one is skipped rather than silently duplicated.
- **`internal/database/seed/questions.json`** — the bundled bank, loaded by `sirajctl seed`:

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

`correct` is the 0-based index into `choices`. Editing an existing `id` updates
that row in place — but a translation since edited through the admin editor, an
import, or an approved machine translation is left alone: its `source` is no
longer `seed`, and the bundled file does not outrank a human.

**Machine translation is held, never published.** Set `TRANSLATE_PROVIDER` to
`claude` or `libretranslate` to enable it; with neither configured the admin
area offers the editor and the importer only, which is the default. Machine
output is written with `needs_review = true`, and **every player-facing query
filters unreviewed translations out**, so a machine rendering of a Qur'anic verse
or a hadith cannot reach a player until a human approves it in `/admin/review`.
That gate is deliberate: a plausible-but-wrong translation of scripture is a real
harm, not a cosmetic defect.

Adding a **language** means one line in `i18n.Supported`, a
`internal/i18n/locales/<code>.json` catalogue, and a locale row in each
translated table. `go test ./internal/i18n` fails if any catalogue is missing a
key, carries an orphan, or has a mismatched format verb.

---

## Working on it

[TODO.md](docs/TODO.md) is the order of work, one prompt per branch. Landed so
far: one command from a clean clone, the Vite and Alpine toolchain with the
frontend sources out of the served tree, subject areas above the categories end
to end, the public endpoint, and the repository tidy-up. What is left is the
remaining Alpine conversions and the design-system debt, both listed there with
what each one is for.

Branches, commits and pushes are the author's — nothing is staged, committed or
pushed automatically, and no branch is opened on anybody's behalf
([TODO.md](docs/TODO.md), *Rules for every prompt*).

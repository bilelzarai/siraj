# Deploying Sirāj

Three environments, the same binary in each, told apart only by what is in the
environment. There is no build flag that changes behaviour — `APP_ENV` switches
log format and turns two safety checks from warnings into refusals to start, and
that is the whole of it.

| | Database | `APP_ENV` | Who reaches it |
|---|---|---|---|
| **dev** | `docker compose up db`, port 5434 | `development` | you, on your machine |
| **test** | its own database on the staging server | `production` | the team, behind a password or a private network |
| **prod** | its own database, backed up | `production` | players |

Test runs with `APP_ENV=production` deliberately. An environment that is
configured more loosely than production is an environment that cannot tell you
whether production will start.

---

## What the binary needs

Everything is an environment variable. `.env.example` lists every one the server
reads; these are the ones without a usable default:

| Variable | Required | Notes |
|---|---|---|
| `DATABASE_URL` | always | `postgres://user:pass@host:5432/db?sslmode=require` outside dev |
| `SESSION_SECRET` | production | `openssl rand -hex 32`. Rotating it signs every in-flight flash cookie out |
| `SECURE_COOKIES` | production | `true`. The server refuses to start in production without it — a session cookie without `Secure` travels in clear over any plain-HTTP hop |
| `BASE_URL` | production | Must be `https://`. Used in password-reset links |
| `TRUSTED_PROXY_HOPS` | behind a proxy | `1` behind one reverse proxy, `2` with a CDN in front. `0` ignores `X-Forwarded-For` entirely, which is right when nothing is in front |

`SEED_ON_START` is `false` and stays that way. The bundled question bank is
real content, but it arrives by somebody asking for it once:

```bash
./bin/sirajctl seed
```

Idempotent, and it will not overwrite a translation that an import, the admin
editor or an approved review has touched since — so re-run it after editing
`internal/database/seed/questions.json`. Boot-time seeding is the thing to avoid:
a restart is not a decision about content.

**Secrets never go in the repository.** `.env` is git-ignored; put the real
values in the host's secret store or the unit file's `EnvironmentFile`.

---

## Database

Migrations run automatically at boot, in filename order, each in its own
transaction, recorded in `schema_migrations`. There is no separate migrate step
and no rollback: a migration that must be undone is undone by a later migration.

So a deployment is **migrate-then-serve in one process**, which has one
consequence worth planning for: during a rolling deploy the old binary can
briefly be running against the new schema. Every migration here has been written
to be safe that way — new columns are nullable or defaulted, and nothing is
dropped in the same release that stops writing it. Migration `0031` dropped the
duel columns only after a release in which nothing read them.

**Before deploying to prod, take a backup.**

```bash
pg_dump "$DATABASE_URL" --format=custom --file="siraj-$(date +%F).dump"
```

Check it restores somewhere else before you rely on it.

---

## Dev

```bash
cp .env.example .env     # defaults point at the compose database
make db-up               # PostgreSQL 16 on :5434
make tools               # the templ CLI
make run                 # http://localhost:8080
go run ./cmd/sirajctl seed   # once, to put the question bank in
```

`make check` runs vet and the tests. The suite builds and drops its own
database, so it never touches your development data.

Without `make` installed, the targets are thin enough to run by hand:

```bash
docker compose up -d db
go run github.com/a-h/templ/cmd/templ@latest generate
STATIC_DIR=./static go run ./cmd/server
go vet ./... && go test ./... -count=1
```

---

## Test server

A single host is enough. Postgres and the app, both from compose:

```bash
git clone https://github.com/bilelzarai/siraj.git && cd siraj

# compose substitutes these from .env, and the app service refuses to start
# without them — both are marked required in docker-compose.yml on purpose.
cat > .env <<EOF
SESSION_SECRET=$(openssl rand -hex 32)
BASE_URL=https://test.example
EOF

# --profile full is not optional: the app service is behind that profile, so
# a plain `docker compose up` starts the database and nothing else.
docker compose --profile full up -d --build

# once the app has booted and migrated, put the question bank in
docker compose exec app /app/sirajctl seed
```

The `app` service already sets `APP_ENV=production`, `SECURE_COOKIES=true` and
`TRUSTED_PROXY_HOPS=1`, so it expects **one TLS-terminating proxy in front of
it**. Put one there. With nothing forwarding, the hop count is wrong and the
login and password-reset limiters end up keyed on an address the caller chose
for themselves; with two proxies, raise it to `2`.

`docker compose up db` alone is the dev database on `:5434` — the `app` service
is the deployable one, and it keeps uploads on a named `uploads` volume so
attachments survive `up --build`. That volume is the one thing in this stack
worth backing up besides the database.

To refresh it from a prod backup:

```bash
pg_restore --clean --if-exists --no-owner -d "$DATABASE_URL" siraj-2026-10-01.dump
```

Scrub anything personal before it leaves production.

---

## Production

Same image. Differences are operational, not in the code:

- **Postgres is managed separately** — not in the app's compose file. A database
  whose lifetime is tied to an application deployment is a database you will one
  day delete by redeploying.
- **`UPLOAD_DIR` is a persistent volume.** Photos and voice notes are written to
  disk with only their metadata in the database; a container filesystem loses
  them on every deploy.
- **`/healthz` pings the database** and answers `503` when it cannot reach it.
  That makes it a readiness check: point the load balancer at it to decide
  whether to send traffic, but do not use it as a liveness probe — a database
  blip would otherwise have the orchestrator restart healthy app containers.
- **Logs are JSON** when `APP_ENV=production`; ship them somewhere.

The janitor runs in-process every 15 minutes: expired sessions, stale matches,
temporary players whose time is up, spent password resets, old request keys. It
is not a cron job and needs no scheduling, but it does mean **a single instance
does this work** — run more than one and they will each sweep, which is
harmless but wasteful.

### First run

Two things a fresh database does not have and the UI cannot give it:

```bash
./bin/sirajctl seed              # the question bank
./bin/sirajctl promote <username>   # the first admin
```

Both are idempotent. `seed` is also how a content edit reaches production:
edit the JSON, deploy, run it again.

### First administrator

The admin area is role-gated and the first account has no way to promote itself
through the UI. Use the bundled CLI on the server:

```bash
./bin/sirajctl promote <username>
```

`sirajctl` reads `DATABASE_URL` from the same environment the server does, and
refuses to demote the last admin.

---

## Scaling past one instance

Two things are process-local today and would need addressing first:

1. **The SSE hub** (`service.Hub`) fans events out to connections held in memory.
   With two instances, a player connected to one never hears an event published
   on the other. Swap it for PostgreSQL `LISTEN`/`NOTIFY` — the publish sites are
   already funnelled through one method.
2. **The rate limiters** (`service.WriteLimits`, the login limiter) count in
   memory, so N instances means N times the allowance.

Neither matters at one instance, and both are noted where they live.

---

## Checklist before a production deploy

- [ ] `make check` green, CI green on the commit being deployed
- [ ] Backup taken **and** test-restored
- [ ] `SESSION_SECRET` set and not the dev value
- [ ] `SECURE_COOKIES=true`, `BASE_URL` is `https://`
- [ ] `TRUSTED_PROXY_HOPS` matches the real number of proxies
- [ ] `UPLOAD_DIR` on a volume that survives the deploy
- [ ] An admin account exists
- [ ] `sirajctl seed` has been run, so there are questions to play

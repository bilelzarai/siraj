# Sirāj v2

A trilingual (Arabic / English / French) quiz app: Islamic knowledge is the flagship
domain, next to general domains (sport, geography, business, science…). One Go binary
serves the player website, the staff admin and a JSON API for the mobile app, on one
PostgreSQL 18 database.

Everything that defines the product lives in **`siraj-v2-dev-kit/`**: requirements,
to-do list, backend architecture, screen map, design system, decisions, QA checklist and
the clickable prototypes (`siraj-v2-dev-kit/docs/design/player/index.html` and
`.../design/admin/index.html`). `CLAUDE.md` is the short version for day-to-day work.

## Stack

| Concern | Choice |
| --- | --- |
| HTTP | Gin |
| Database | PostgreSQL 18 via pgx/v5 |
| Queries | sqlc (type-safe, generated into `internal/db/sqlc`) |
| Migrations | golang-migrate, embedded, run on startup |
| HTML templates | templ |
| Frontend build | Vite 8 (Rolldown) via Bun |
| CSS | Tailwind CSS v4 (`@tailwindcss/vite`) |
| Interactivity | htmx (partials) + Alpine.js (UI state) |

## Install once

- [Go 1.24+](https://go.dev/dl/)
- [Bun](https://bun.sh/) — runs Vite and installs the front-end packages
- [Docker](https://www.docker.com/products/docker-desktop/) — runs PostgreSQL 18
- [VS Code](https://code.visualstudio.com/) (optional) and [Git](https://git-scm.com/)

`make setup` installs the Go tools it needs itself (`air`, `templ`, `sqlc`) and the
front-end packages.

## Run it

```bash
make setup     # .env, Go modules, air + templ + sqlc, bun install, first asset build
make up        # the whole app in the background: PostgreSQL, codegen, assets, server, worker
make down      # stop all of it again (make down keep=1 leaves PostgreSQL running)
make status    # what runs, and whether /api/v1/health answers   (make logs follows the log)
make run       # the server in the foreground instead (PostgreSQL + assets + migrations)
make dev       # the same with live reload: templ --watch, Vite on :5173, air
make test      # unit + integration tests (each package migrates a database of its own)
make gen       # regenerate after editing internal/db/queries or a .templ file

make admin email=you@example.com username=you password='a real password'
               # the first staff account — nothing is seeded, so this is how you get in
make db-clear  # empty a database of content and players, keeping the schema
```

**The database starts empty.** No migration inserts domains, categories or questions:
the catalog and the question bank are real content, written by staff in the admin
console (`/admin`). Until the first questions are published, the player app shows its
empty states and a game refuses to start with `no_questions`.

PostgreSQL listens on **5442** by default (`POSTGRES_PORT` in `.env`), in the Docker
compose project `siraj-v2`, so it never clashes with another PostgreSQL on 5432.

In VS Code: **Terminal → Run Task…** has the same steps numbered 1 to 8, and F5 debugs
the server.

Try the API with `api.http` (REST Client extension), or read the contract:
**[`api/openapi.yaml`](api/openapi.yaml)** — OpenAPI 3.1, every endpoint, served by the
server itself at `GET /api/v1/openapi.yaml`. `make api-lint` checks it, `make api-docs`
writes a single-page HTML reference, and a test fails if it ever stops matching the
routes the server mounts.

## How it fits together

```
browser ── HTML + htmx ─┐
                        ├─ Gin ── module service ── sqlc ── PostgreSQL 18
mobile app ── JSON ─────┘            (one service behind both)
```

- Pages are templ components rendered server-side; htmx swaps partials, Alpine holds
  local UI state only (timer, pagers, drawers, star rater).
- The front-end is built by Vite into `web/dist` and embedded in the binary, so
  production ships one file. In development the server reads `web/dist` from disk, or
  the Vite dev server when `VITE_DEV_SERVER` is set (that is what `make dev` does).
- Migrations are embedded and applied before the server listens; a dirty state stops it.

## What works today

Phases 0–3 of `siraj-v2-dev-kit/docs/04-TODO.md` (see `CLAUDE.md → Status` for detail):

- Welcome, sign in, sign up, guest mode, password reset, onboarding (3 steps)
- Home with the question of the day, Your domains, start-a-game tiles, categories
- Explore / Add / Manage domains, domain pages themed by their own colour
- Play hub, game setup, the solo game (timer, 50/50, skip, feedback) and the results
  screen with the answer review, star ratings, comments and problem reports
- JSON API: every documented endpoint (1–119, bar the WebSocket), in the
  `{"data": …}` envelope — identity, catalog and search, games, questions, rooms,
  duels, challenges and pass-the-device rounds, friends, messages and uploads,
  notifications, progress and leaderboards, sets, support. It is built for an
  external consumer: `Authorization: Bearer`, no cookies and no CSRF token for a
  token client, `Accept-Language` per request, JSON for every 404/405, and CORS for
  the origins listed in `CORS_ORIGINS`
- Arabic / English / French everywhere, light / dark / device themes, 390 px to desktop

Also built since: progress and leaderboards, live rooms, duels, challenges and
pass-the-device rounds, friends, messages, notifications, my questions, support and the
admin back office. Not built yet: the WebSocket (endpoint 111 — the app reads the same
signals from `GET /api/v1/events`, server-sent), the admin CSV import, and the
duplicate-question job, which runs on demand.

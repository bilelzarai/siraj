#!/usr/bin/env bash
# One command to bring the whole development stack up: database, templates,
# server. Written because `make` is not installed everywhere this repo is
# cloned — it does exactly what `make db-up` + `make run` would do.
#
#   ./run.sh              start everything, serve on http://localhost:8080
#   PORT=9090 ./run.sh    same, on another port
set -euo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$REPO"

CONTAINER=${DB_CONTAINER:-islamic-game-db}
PORT=${PORT:-8080}
export PATH="$PATH:$(go env GOPATH)/bin"

say() { printf '\033[36m==\033[0m %s\n' "$1"; }

# --- 1. configuration -------------------------------------------------------
if [ ! -f .env ]; then
  say "no .env — copying .env.example"
  cp .env.example .env
  # SESSION_SECRET is blank in the example and required outside development.
  if command -v openssl >/dev/null; then
    sed -i "s|^SESSION_SECRET=$|SESSION_SECRET=$(openssl rand -hex 32)|" .env
  fi
fi

# --- 2. database ------------------------------------------------------------
# The container may already exist outside this compose project (started by hand
# with `docker run`), in which case `docker compose up` fails on the name. Reuse
# whatever is already answering rather than fighting it for the name.
if [ -n "$(docker ps -q -f "name=^/${CONTAINER}$")" ]; then
  say "database already running"
elif [ -n "$(docker ps -aq -f "name=^/${CONTAINER}$")" ]; then
  say "starting existing database container"
  docker start "$CONTAINER" >/dev/null
else
  say "creating database container"
  docker compose up -d db
fi

say "waiting for PostgreSQL"
for _ in $(seq 1 30); do
  docker exec "$CONTAINER" pg_isready -U islamic -d islamic_game >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$CONTAINER" pg_isready -U islamic -d islamic_game >/dev/null

# --- 3. stop a previous server ---------------------------------------------
# A previous run still holding the port would make this one die on bind. Match
# on the resolved executable rather than the command line, because the same
# binary shows up as `./bin/server` or an absolute path depending on how it was
# started — and so an unrelated process merely named `server` is left alone.
for pid in $(pgrep -f 'bin/server' 2>/dev/null || true); do
  if [ "$(readlink -f "/proc/$pid/exe" 2>/dev/null)" = "$REPO/bin/server" ]; then
    say "stopping the previous server (pid $pid)"
    kill "$pid" 2>/dev/null || true
    sleep 1
  fi
done

# --- 4. templates -----------------------------------------------------------
if ! command -v templ >/dev/null; then
  say "installing the templ CLI"
  go install github.com/a-h/templ/cmd/templ@latest
fi
say "generating templates"
templ generate

# --- 5. server --------------------------------------------------------------
# STATIC_DIR serves css/js from disk, so edits land without a rebuild.
say "serving on http://localhost:${PORT}  (ctrl-c to stop)"
exec env STATIC_DIR=./static APP_ADDR=":${PORT}" go run ./cmd/server

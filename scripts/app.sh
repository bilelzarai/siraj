#!/usr/bin/env bash
# One command for the whole stack: PostgreSQL + built assets + server + worker.
#
#   scripts/app.sh up        start everything (idempotent: restarts what already runs)
#   scripts/app.sh down      stop everything, database included (--keep-db keeps PostgreSQL)
#   scripts/app.sh restart   down, then up
#   scripts/app.sh status    what is running, and whether /api/v1/health answers
#   scripts/app.sh logs      follow the server log (logs worker for the worker's)
#
# State lives in tmp/ (gitignored): tmp/server.pid, tmp/server.log, tmp/worker.{pid,log}.
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root" || exit 1

# The tools are installed per user, not on a non-login shell's PATH.
export PATH="$PATH:$HOME/go/bin:$HOME/.bun/bin"

run=tmp
mkdir -p "$run"

say()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m  ! \033[0m%s\n' "$*"; }
die()  { printf '\033[1;31m  x \033[0m%s\n' "$*" >&2; exit 1; }

port() { # HTTP port from .env / environment, default 8080
  local addr=${HTTP_ADDR:-}
  [ -n "$addr" ] || addr=$(sed -n 's/^HTTP_ADDR=//p' .env 2>/dev/null | tail -1)
  [ -n "$addr" ] || addr=:8080
  echo "${addr##*:}"
}

alive() { [ -n "${1:-}" ] && kill -0 "$1" 2>/dev/null; }

stop_pidfile() { # name
  local name=$1 pid
  pid=$(cat "$run/$name.pid" 2>/dev/null)
  if alive "$pid"; then
    kill "$pid" 2>/dev/null
    for _ in $(seq 1 40); do alive "$pid" || break; sleep 0.25; done
    alive "$pid" && kill -9 "$pid" 2>/dev/null
    say "stopped $name (pid $pid)"
  fi
  rm -f "$run/$name.pid"
}

stop_strays() { # any siraj-server / siraj-worker left over from an earlier run
  local pid name
  for name in siraj-server siraj-worker; do
    for pid in $(pgrep -x "$name" 2>/dev/null); do
      kill "$pid" 2>/dev/null
      for _ in $(seq 1 20); do alive "$pid" || break; sleep 0.25; done
      alive "$pid" && kill -9 "$pid" 2>/dev/null
      say "stopped stray $name (pid $pid)"
    done
  done
}

stop_watchers() { # air / vite / templ --watch started by `make dev` in this directory
  local pid cwd cmd
  for pid in $(pgrep -f 'air|vite|templ generate' 2>/dev/null); do
    [ "$pid" = "$$" ] && continue
    cwd=$(readlink "/proc/$pid/cwd" 2>/dev/null) || continue
    [ "$cwd" = "$root" ] || continue
    cmd=$(tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null)
    case $cmd in *app.sh*) continue;; esac
    kill "$pid" 2>/dev/null && say "stopped dev watcher (pid $pid)"
  done
}

build_assets() {
  local manifest=web/dist/.vite/manifest.json
  if [ "${ASSETS:-auto}" = skip ]; then warn "assets: skipped (ASSETS=skip)"; return 0; fi
  if [ "${ASSETS:-auto}" = auto ] && [ -f "$manifest" ] &&
     [ -z "$(find web/assets web/public package.json bun.lock vite.config.ts -newer "$manifest" 2>/dev/null | head -1)" ]; then
    say "assets: already up to date"
    return 0
  fi
  command -v bun >/dev/null || die "bun is not installed (run make setup)"
  say "assets: building (bun run build)"
  bun install >/dev/null 2>&1
  bun run build >"$run/assets.log" 2>&1 || { tail -20 "$run/assets.log"; die "asset build failed (tmp/assets.log)"; }
}

generate_if_stale() { # templ -> Go and SQL -> Go, only when a source changed since the last run
  [ "${GEN:-auto}" = skip ] && { warn "codegen: skipped (GEN=skip)"; return 0; }
  local f out stale=0 force=0
  [ "${GEN:-auto}" = force ] && force=1

  # templ. The stamp is what we compare against: templ leaves a *_templ.go untouched when
  # its output does not change, so comparing file to file would regenerate on every call.
  if [ "$force" = 1 ] || [ ! -f "$run/.gen-templ" ]; then
    stale=1
  else
    while IFS= read -r f; do
      out="${f%.templ}_templ.go"
      if [ ! -f "$out" ] || [ "$f" -nt "$run/.gen-templ" ]; then stale=1; break; fi
    done < <(find web/templates -name '*.templ' 2>/dev/null)
  fi
  if [ "$stale" = 1 ]; then
    command -v templ >/dev/null || die "templ is not installed (run make setup)"
    say "codegen: templ generate"
    templ generate >"$run/gen.log" 2>&1 || { tail -20 "$run/gen.log"; die "templ generate failed (tmp/gen.log)"; }
    touch "$run/.gen-templ"
  fi

  # sqlc: the queries, and the migrations it reads the schema from.
  if [ "$force" = 1 ] || [ ! -f "$run/.gen-sqlc" ] ||
     [ -n "$(find internal/db/queries internal/db/migrations sqlc.yaml -newer "$run/.gen-sqlc" 2>/dev/null | head -1)" ]; then
    say "codegen: sqlc generate"
    { sqlc generate || docker run --rm -v "$root:/src" -w /src sqlc/sqlc:1.31.1 generate; } >>"$run/gen.log" 2>&1 ||
      { tail -20 "$run/gen.log"; die "sqlc generate failed (tmp/gen.log)"; }
    touch "$run/.gen-sqlc"
  fi
}

start() { # name, binary
  local name=$1 bin=$2
  : >"$run/$name.log"
  setsid nohup "$bin" >>"$run/$name.log" 2>&1 < /dev/null &
  echo $! >"$run/$name.pid"
  say "$name started (pid $(cat "$run/$name.pid")) -> tmp/$name.log"
}

cmd_up() {
  command -v docker >/dev/null || die "docker is not installed"
  [ -f .env ] || { cp .env.example .env && say ".env created from .env.example"; }

  say "database: docker compose up -d --wait"
  docker compose up -d --wait >/dev/null || die "PostgreSQL did not start (docker compose logs postgres)"

  generate_if_stale
  build_assets

  say "building cmd/server and cmd/worker"
  go build -o "$run/siraj-server" ./cmd/server || die "go build ./cmd/server failed"
  go build -o "$run/siraj-worker" ./cmd/worker || die "go build ./cmd/worker failed"

  stop_pidfile server; stop_pidfile worker; stop_strays

  start server "$root/$run/siraj-server"
  local p; p=$(port)
  for _ in $(seq 1 60); do
    curl -sf "http://localhost:$p/api/v1/health" >/dev/null && break
    alive "$(cat "$run/server.pid" 2>/dev/null)" || { tail -20 "$run/server.log"; die "the server exited on start (tmp/server.log)"; }
    sleep 0.5
  done
  curl -sf "http://localhost:$p/api/v1/health" >/dev/null ||
    { tail -20 "$run/server.log"; die "/api/v1/health did not answer (tmp/server.log)"; }

  start worker "$root/$run/siraj-worker"
  say "up: http://localhost:$p  (admin /admin, API /api/v1)"
}

cmd_down() {
  local keep_db=0
  [ "${1:-}" = "--keep-db" ] && keep_db=1
  stop_pidfile worker; stop_pidfile server; stop_strays; stop_watchers
  if [ "$keep_db" = 1 ]; then
    say "database: left running (--keep-db)"
  else
    say "database: docker compose down"
    docker compose down >/dev/null 2>&1 || warn "docker compose down reported an error"
  fi
  say "down"
}

cmd_status() {
  local name pid p
  for name in server worker; do
    pid=$(cat "$run/$name.pid" 2>/dev/null)
    if alive "$pid"; then printf '%-8s running (pid %s)\n' "$name" "$pid"
    else printf '%-8s stopped\n' "$name"; fi
  done
  for pid in $(pgrep -x siraj-server 2>/dev/null) $(pgrep -x siraj-worker 2>/dev/null); do
    grep -qx "$pid" "$run/server.pid" "$run/worker.pid" 2>/dev/null || printf 'stray    pid %s\n' "$pid"
  done
  p=$(port)
  if curl -sf "http://localhost:$p/api/v1/health" >/dev/null; then printf 'health   ok (http://localhost:%s)\n' "$p"
  else printf 'health   no answer on :%s\n' "$p"; fi
  docker compose ps 2>/dev/null | tail -n +1
}

case "${1:-}" in
  up)      shift; cmd_up "$@";;
  down)    shift; cmd_down "$@";;
  restart) shift; cmd_down --keep-db; cmd_up "$@";;
  status)  cmd_status;;
  logs)    tail -f "$run/${2:-server}.log";;
  *) sed -n '2,12p' "$0"; exit 1;;
esac

# macOS / Linux / WSL shortcuts. On Windows use the VS Code tasks (Terminal → Run Task), they do the same.
.PHONY: setup up down restart status logs db-up db-down db-reset db-clear db-ui db-ui-down dev run worker admin dev-seed gen assets templ check-generated test typecheck check ui-check vet migrate-new psql api-lint api-docs

setup:            ## first run: copy .env, download modules, install the tools, build the assets
	@test -f .env || cp .env.example .env
	go mod tidy
	go install github.com/air-verse/air@latest
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	bun install

up:               ## start everything: PostgreSQL, built assets, server on :8080, worker
	./scripts/app.sh up

down:             ## stop everything, database included (make down keep=1 leaves PostgreSQL up)
	./scripts/app.sh down $(if $(keep),--keep-db)

restart:          ## stop and start the app again (the database stays up)
	./scripts/app.sh restart

status:           ## what is running, and whether /api/v1/health answers
	./scripts/app.sh status

logs:             ## follow the server log (make logs who=worker for the worker's)
	./scripts/app.sh logs $(or $(who),server)

db-up:            ## start PostgreSQL 18 in Docker
	docker compose up -d --wait

db-down:
	docker compose down

db-reset:         ## wipe the local database (migrations re-run on next start)
	docker compose down -v && docker compose up -d --wait

db-ui: db-up       ## browse the database at http://localhost:8081 (Adminer; make db-ui-down stops it)
	docker compose --profile tools up -d adminer
	@echo "  http://localhost:$${ADMINER_PORT:-8081}  —  System: PostgreSQL · Server: postgres · User: siraj · Password: secret · Database: siraj"

db-ui-down:       ## stop the database browser
	docker compose --profile tools rm -sf adminer

db-clear: db-up   ## empty content and players, keep the schema (removes data entered earlier)
	docker exec -i siraj-v2-postgres psql -U siraj -d siraj -v ON_ERROR_STOP=1 -f - < scripts/db-clear.sql

dev: db-up        ## server with live reload on :8080 and Vite on :5173
	templ generate --watch --proxy=http://localhost:8080 & \
	bunx vite & \
	VITE_DEV_SERVER=http://localhost:5173 air

run: db-up assets ## server without live reload (serves the built assets)
	go run ./cmd/server

worker:
	go run ./cmd/worker

admin: db-up      ## create the first staff account (make admin email=you@x.com username=you password=…)
	@test -n "$(email)" -a -n "$(username)" || (echo "usage: make admin email=you@example.com username=you password='…' [role=admin]" && exit 1)
	go run ./cmd/createuser -email='$(email)' -username='$(username)' $(if $(password),-password='$(password)') $(if $(name),-name='$(name)') $(if $(role),-role='$(role)') $(if $(lang),-lang='$(lang)')

dev-seed: db-up   ## development content to work against: a catalog and a bank (clear=1 removes it)
	go run ./cmd/devseed $(if $(clear),-clear)

gen:              ## regenerate everything generated: SQL -> Go (sqlc) and templ -> Go
	sqlc generate || docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc:1.28.0 generate
	templ generate

check-generated:  ## fail when sqlc/templ output is stale (what CI runs)
	$(MAKE) gen
	@git diff --quiet -- internal/db/sqlc ':(glob)web/templates/**/*_templ.go' || \
	  (echo "generated code is stale: run make gen and commit the result" && git --no-pager diff --stat -- internal/db/sqlc ':(glob)web/templates/**/*_templ.go' && exit 1)

api-lint:         ## check api/openapi.yaml against the OpenAPI 3.1 rules
	bunx @redocly/cli@latest lint api/openapi.yaml

api-docs:         ## write a single-page HTML reference next to the spec
	bunx @redocly/cli@latest build-docs api/openapi.yaml -o var/api-docs.html
	@echo "  var/api-docs.html"

ui-check:         ## open every screen at 360–1440 px and fail on sideways scroll or cut content
	@command -v google-chrome >/dev/null || (echo "google-chrome is needed for make ui-check" && exit 1)
	@google-chrome --headless=new --remote-debugging-port=$${CDP_PORT:-9222} --no-sandbox --disable-gpu \
	  --user-data-dir=$$(mktemp -d) about:blank >/dev/null 2>&1 & echo $$! > tmp/ui-chrome.pid; sleep 3
	@UI_LOGIN=$(login) UI_PASSWORD=$(password) bun scripts/ui-check.js; status=$$?; \
	  kill $$(cat tmp/ui-chrome.pid) 2>/dev/null; rm -f tmp/ui-chrome.pid; exit $$status

test:
	go test ./...

vet:
	go vet ./...

typecheck:        ## type-check the front-end (Vite strips types without checking them)
	bunx tsc --noEmit

check: vet typecheck check-generated test  ## everything a change has to pass before it ships

migrate-new:      ## make migrate-new name=games
	@test -n "$(name)" || (echo "usage: make migrate-new name=games" && exit 1)
	@n=$$(ls internal/db/migrations/*.up.sql | wc -l); n=$$(printf "%06d" $$((n+1))); \
	touch internal/db/migrations/$${n}_$(name).up.sql internal/db/migrations/$${n}_$(name).down.sql; \
	echo "created internal/db/migrations/$${n}_$(name).{up,down}.sql"

psql:
	docker exec -it siraj-v2-postgres psql -U siraj -d siraj

assets:           ## build the front-end (Vite + Tailwind) into web/dist
	bun install --frozen-lockfile
	bun run build

templ:            ## regenerate *_templ.go from the templ templates
	templ generate

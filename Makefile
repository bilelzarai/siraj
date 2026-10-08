GO      ?= go
BIN     := bin/server
CTL     := bin/sirajctl
TEMPL   := $(shell $(GO) env GOPATH)/bin/templ

# The suite builds its own database and drops it again, so it must never be
# aimed at a deployment. repository_test falls back to .env when this is unset,
# and .env holds whatever the last deploy was pointed at. Override to use
# another server: make test TEST_DATABASE_URL=...
TEST_DATABASE_URL ?= postgres://siraj:siraj@localhost:5434/siraj-db?sslmode=disable

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: tools
tools: ## Install the templ CLI
	$(GO) install github.com/a-h/templ/cmd/templ@$$($(GO) list -m -f '{{.Version}}' github.com/a-h/templ)

.PHONY: generate
generate: ## Regenerate *_templ.go from *.templ
	$(TEMPL) generate

.PHONY: assets
assets: ## Build the frontend assets into static/dist
	@command -v npm >/dev/null 2>&1 || { \
		echo "npm not found — skipping the asset build; pages will have no styles"; \
		exit 0; }
	@test -d node_modules || npm install
	npm run build

.PHONY: build
build: generate assets ## Build the server and the admin CLI
	$(GO) build -o $(BIN) ./cmd/server
	$(GO) build -o $(CTL) ./cmd/sirajctl

.PHONY: run
run: generate assets ## Run the server (assets served from disk)
	STATIC_DIR=./static $(GO) run ./cmd/server

.PHONY: db-up
db-up: ## Start PostgreSQL in Docker
	docker compose -f deploy/compose.yaml up -d db

.PHONY: db-down
db-down: ## Stop PostgreSQL
	docker compose -f deploy/compose.yaml down

.PHONY: db-reset
db-reset: ## Drop and recreate the database volume
	docker compose -f deploy/compose.yaml down -v && docker compose -f deploy/compose.yaml up -d db

.PHONY: db-shell
db-shell: ## Open psql against the dev database
	docker compose -f deploy/compose.yaml exec db psql -U siraj -d siraj-db

.PHONY: test
test: generate ## Run the test suite
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test ./... -count=1

.PHONY: vet
vet: generate ## Run go vet
	$(GO) vet ./...

.PHONY: check
check: vet test ## Vet and test

.PHONY: fmt
fmt: ## Format Go and templ sources
	$(GO) fmt ./...
	$(TEMPL) fmt .

.PHONY: clean
clean:
	rm -rf bin

.PHONY: smoke
smoke: ## Run the end-to-end HTTP smoke test against a running server
	./scripts/smoke.sh

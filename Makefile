GO      ?= go
BIN     := bin/server
CTL     := bin/sirajctl
TEMPL   := $(shell $(GO) env GOPATH)/bin/templ

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: tools
tools: ## Install the templ CLI
	$(GO) install github.com/a-h/templ/cmd/templ@latest

.PHONY: generate
generate: ## Regenerate *_templ.go from *.templ
	$(TEMPL) generate

.PHONY: build
build: generate ## Build the server and the admin CLI
	$(GO) build -o $(BIN) ./cmd/server
	$(GO) build -o $(CTL) ./cmd/sirajctl

.PHONY: run
run: generate ## Run the server (assets served from disk)
	STATIC_DIR=./static $(GO) run ./cmd/server

.PHONY: db-up
db-up: ## Start PostgreSQL in Docker
	docker compose up -d db

.PHONY: db-down
db-down: ## Stop PostgreSQL
	docker compose down

.PHONY: db-reset
db-reset: ## Drop and recreate the database volume
	docker compose down -v && docker compose up -d db

.PHONY: db-shell
db-shell: ## Open psql against the dev database
	docker compose exec db psql -U islamic -d islamic_game

.PHONY: test
test: generate ## Run the test suite
	$(GO) test ./... -count=1

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

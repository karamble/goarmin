# goarmin: self-hosted Garmin sync and health dashboard
#
# make help lists every target. The server runs on this machine, not in Docker: the sync role
# needs the host's Bluetooth (BlueZ over D-Bus).
SHELL := /bin/bash

# Colors for output (tput for portability)
RED    := $(shell tput -Txterm setaf 1 2>/dev/null)
GREEN  := $(shell tput -Txterm setaf 2 2>/dev/null)
YELLOW := $(shell tput -Txterm setaf 3 2>/dev/null)
BLUE   := $(shell tput -Txterm setaf 4 2>/dev/null)
NC     := $(shell tput -Txterm sgr0 2>/dev/null)

BIN     := bin/goarmin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Every outbound request carries a regular desktop browser User-Agent and nothing that
# identifies us. Keep this in sync with pkg/httpx.BrowserUA.
BROWSER_UA := Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36

# Builds and tests never download modules: the go command would send its own identifying
# User-Agent ("GoCommand/1 (+https://go.dev/cmd/go)"). Fetch dependencies with `make deps`.
GO := GOPROXY=off GOTOOLCHAIN=local go

# git carries the browser User-Agent through per-command config; the user's git config is untouched.
GIT_UA := GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=http.userAgent GIT_CONFIG_VALUE_0="$(BROWSER_UA)"

# fetch downloads modules with git only (GOPROXY=direct) and fails if the go command made an
# HTTP request of its own, which `go mod download -x` logs as "# get https://...".
define fetch
	@log=$$(mktemp); \
	$(GIT_UA) GOPROXY=direct GOTOOLCHAIN=local go mod download -x $(1) 2>"$$log"; rc=$$?; \
	if grep -q '^# get http' "$$log"; then echo "go made its own HTTP request, see $$log"; exit 1; fi; \
	if [ $$rc -ne 0 ]; then cat "$$log"; rm -f "$$log"; exit $$rc; fi; \
	rm -f "$$log"
endef
TIDY := GOPROXY="file://$$(go env GOMODCACHE)/cache/download" GOTOOLCHAIN=local go mod tidy

# Local settings and secrets that never go into the repository (.gitignore): .env holds
# KEY=value lines, e.g. GOARMIN_TILE_URL=jawg-matrix and GOARMIN_TILE_TOKEN=…; the server
# gets them from the environment.
-include .env
export GOARMIN_TILE_URL GOARMIN_TILE_TOKEN GOARMIN_TILE_DARK GOARMIN_TILE_ATTRIBUTION

# Server configuration. DATA holds the database, the raw files and the watch spool; the
# Bluetooth bond with the watch lives in BlueZ (/var/lib/bluetooth), not in DATA.
LISTEN     ?= $(or $(GOARMIN_LISTEN),127.0.0.1:8642)
DATA       ?= $(or $(GOARMIN_DATA_DIR),$(or $(XDG_DATA_HOME),$(HOME)/.local/share)/goarmin)
SERVE_ARGS ?= -with-sync
STATE_DIR  ?= $(or $(XDG_STATE_HOME),$(HOME)/.local/state)/goarmin
URL        := http://localhost:$(lastword $(subst :, ,$(LISTEN)))
SQLITE     ?= sqlite3
DB         := $(DATA)/goarmin.db
SERVER     := LISTEN="$(LISTEN)" STATE_DIR="$(STATE_DIR)" scripts/server.sh
START      := ./$(BIN) -data "$(DATA)" $(SERVE_ARGS)

# Backups hold health data, so they live outside the repository, readable only by you.
BACKUP_DIR ?= $(HOME)/goarmin-backups
KEEP_DAYS  ?= 30
KEEP_MIN   ?= 3
BACKUPS    := DATA="$(DATA)" BACKUP_DIR="$(BACKUP_DIR)" SQLITE="$(SQLITE)" KEEP_DAYS="$(KEEP_DAYS)" \
	KEEP_MIN="$(KEEP_MIN)" LISTEN="$(LISTEN)" STATE_DIR="$(STATE_DIR)" scripts/backup.sh

# The account the inspection targets show (default: the first one).
EMAIL ?= $(shell $(SQLITE) "file:$(DB)?mode=ro" "SELECT email FROM users ORDER BY id LIMIT 1;" 2>/dev/null)

.DEFAULT_GOAL := help

# Default target
.PHONY: help
help: ## Show this help message
	@echo "$(BLUE)goarmin$(NC): self-hosted Garmin sync and health dashboard"
	@echo "===================================================="
	@echo "$(BLUE)Server:$(NC) $(URL)    $(BLUE)Data:$(NC) $(DATA)"
	@echo "$(BLUE)Backups:$(NC) $(BACKUP_DIR)"
	@awk 'BEGIN {FS = ":.*?## "} \
		/^##@ / { printf "\n$(GREEN)%s$(NC)\n", substr($$0, 5) } \
		/^[a-zA-Z_-]+:.*?## / { printf "  $(YELLOW)%-16s$(NC) %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""

# ============================================
# Server
# ============================================
##@ Server

.PHONY: start
start: build ## Build, then start the server in the background (log: make logs)
	@$(SERVER) start $(START)

.PHONY: stop
stop: ## Stop the server; a running sync ends cleanly first (FORCE=1 kills it after 60 s)
	@$(SERVER) stop $(if $(FORCE),force)

.PHONY: restart
restart: build ## Rebuild, stop and start the server
	@$(SERVER) stop && $(SERVER) start $(START)

.PHONY: run
run: build ## Build, then run the server in the foreground (Ctrl+C stops it)
	@echo "$(GREEN)goarmin on $(URL), data in $(DATA) (Ctrl+C stops it)$(NC)"
	./$(BIN) serve -listen $(LISTEN) -data "$(DATA)" $(SERVE_ARGS)

.PHONY: status
status: ## Show whether the server runs, where, and its last log lines
	@$(SERVER) status || true

.PHONY: logs
logs: ## Show the last 100 lines of the server log
	@tail -n 100 "$(STATE_DIR)/serve.log" 2>/dev/null || echo "no log yet at $(STATE_DIR)/serve.log (make start writes it)"

.PHONY: logs-follow
logs-follow: ## Follow the server log
	@$(SERVER) logs

.PHONY: health
health: ## Check that the server answers
	@curl -fs -m 3 -A "$(BROWSER_UA)" -o /dev/null "$(URL)/api/health" && \
		echo "$(GREEN)✓ goarmin answers on $(URL)$(NC)" || \
		{ echo "$(RED)✗ goarmin does not answer on $(URL)$(NC)"; exit 1; }

.PHONY: open
open: ## Open the dashboard in the browser
	@xdg-open "$(URL)" >/dev/null 2>&1 &

# ============================================
# Backup and Restore
# ============================================
##@ Backup and restore

.PHONY: backup
backup: ## Back up the database, raw files and watch spool (safe while the server runs)
	@$(BACKUPS) backup

.PHONY: backup-list
backup-list: ## List all backups
	@$(BACKUPS) list

.PHONY: backup-clean
backup-clean: ## Remove backups older than 30 days (KEEP_DAYS), always keeping the newest 3 (KEEP_MIN)
	@$(BACKUPS) clean

.PHONY: restore
restore: build-go ## Restore a backup (BACKUP=goarmin_backup_….tar.gz); the current data is kept aside
	@$(BACKUPS) restore "$(BACKUP)" -- $(START)

# ============================================
# Inspection (read-only)
# ============================================
##@ Inspection (read-only)

.PHONY: info
info: ## Show version, server, data and backup information
	@echo "$(BLUE)goarmin$(NC) $$(./$(BIN) version 2>/dev/null || echo '(not built)')"
	@$(SERVER) status 2>/dev/null | head -1 || true
	@echo ""
	@echo "$(GREEN)Data$(NC) $(DATA)"
	@du -sh "$(DATA)"/* 2>/dev/null | sed 's|$(DATA)/||; s/^/  /' || echo "  none yet"
	@if [ -s "$(DB)" ]; then $(SQLITE) "file:$(DB)?mode=ro" \
		"SELECT '  accounts ' || (SELECT count(*) FROM users) || ', watches ' || (SELECT count(*) FROM devices) || \
		', raw files ' || (SELECT count(*) FROM raw_files) || ', days ' || (SELECT count(*) FROM daily_summary) || \
		', activities ' || (SELECT count(*) FROM activities) || ', samples ' || (SELECT count(*) FROM samples);"; fi
	@echo ""
	@echo "$(GREEN)Backups$(NC) $(BACKUP_DIR)"
	@n=$$(ls -1 "$(BACKUP_DIR)"/goarmin_backup_*.tar.gz 2>/dev/null | wc -l); \
		last=$$(ls -1t "$(BACKUP_DIR)"/goarmin_backup_*.tar.gz 2>/dev/null | head -1); \
		echo "  $$n backup(s)$${last:+, newest $$(basename "$$last")}"

.PHONY: list-users
list-users: ## List the accounts
	@$(SQLITE) -header -column "file:$(DB)?mode=ro" "SELECT id, email, display_name AS name, \
		CASE is_admin WHEN 1 THEN 'yes' ELSE '' END AS admin, timezone, units, \
		datetime(created_at, 'unixepoch', 'localtime') AS created FROM users ORDER BY id;"

.PHONY: list-watches
list-watches: ## List the watches with their owner and after-fetch mode
	@$(SQLITE) -header -column "file:$(DB)?mode=ro" "SELECT d.id, d.name, d.unit_id, d.address, d.firmware, \
		d.after_fetch, (SELECT u.email FROM device_assignments a JOIN users u ON u.id = a.user_id \
		WHERE a.device_id = d.id AND a.valid_to IS NULL ORDER BY a.valid_from DESC LIMIT 1) AS owner FROM devices d ORDER BY d.id;"

.PHONY: list-syncs
list-syncs: ## List the last 20 syncs
	@$(SQLITE) -header -column "file:$(DB)?mode=ro" "SELECT datetime(r.started_at, 'unixepoch', 'localtime') AS started, \
		(r.finished_at - r.started_at) || ' s' AS took, CASE r.ok WHEN 1 THEN 'ok' ELSE 'FAILED' END AS result, \
		r.pulled, r.imported, r.archived, r.deleted, substr(r.error, 1, 60) AS error \
		FROM sync_runs r ORDER BY r.started_at DESC, r.id DESC LIMIT 20;"

.PHONY: days
days: build-go ## Show the last 14 days (EMAIL=… for another account, N=… days)
	@./$(BIN) days -data "$(DATA)" -user "$(EMAIL)" -n $(or $(N),14)

.PHONY: activities
activities: build-go ## Show the newest 20 activities (EMAIL=… for another account, N=…)
	@./$(BIN) activities -data "$(DATA)" -user "$(EMAIL)" -n $(or $(N),20)

.PHONY: db-shell
db-shell: ## Open the database read-only in sqlite3
	$(SQLITE) -header -column "file:$(DB)?mode=ro"

# ============================================
# Build
# ============================================
##@ Build

.PHONY: build
build: build-frontend build-go ## Build the dashboard, then the binary with it embedded

.PHONY: build-frontend
build-frontend: ## Build the React dashboard into internal/webui/dist (offline from the lockfile)
	@if [ ! -d web ]; then echo "no web/ directory yet; skipping the frontend build"; exit 0; fi; \
	cd web && npm ci --prefer-offline --no-audit --no-fund --user-agent "$(BROWSER_UA)" && npm run build

.PHONY: build-go
build-go: ## Build only the Go binary into bin/goarmin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/goarmin

.PHONY: tools
tools: ## Build the research tools (snoopdecode, fitinspect) into bin/
	CGO_ENABLED=0 $(GO) build -trimpath -o bin/ ./tools/...

.PHONY: clean
clean: ## Remove build output (never touches data or backups)
	rm -rf bin
	find internal/webui/dist -mindepth 1 -not -name .gitkeep -delete

# ============================================
# Development
# ============================================
##@ Development

.PHONY: dev-frontend
dev-frontend: ## Run the Vite dev server on :5173 against the server on :8642 (sign in with the password there)
	cd web && npx vite

.PHONY: check
check: vet test web-test ## Everything: gofmt, go vet, Go tests, dashboard tests

.PHONY: test
test: ## Run the unit tests (offline; fixtures and httptest only)
	$(GO) test ./...

.PHONY: race
race: ## Run the unit tests with the race detector
	CGO_ENABLED=1 $(GO) test -race ./...

.PHONY: vet
vet: ## gofmt check and go vet
	@unformatted=$$(gofmt -l $$(find . -name '*.go' -not -path './web/*')); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	$(GO) vet ./...

.PHONY: web-test
web-test: ## Type-check and test the dashboard
	cd web && npx tsc -b && npx vitest run

.PHONY: fuzz
fuzz: ## Run the fuzz targets for 30 s each
	$(GO) test -run='^$$' -fuzz=FuzzDecode -fuzztime=30s ./pkg/cobs
	$(GO) test -run='^$$' -fuzz=FuzzDatasets -fuzztime=30s ./pkg/mtp
	$(GO) test -run='^$$' -fuzz=FuzzSession -fuzztime=30s ./pkg/mtp

# ============================================
# Dependencies
# ============================================
##@ Dependencies

.PHONY: web-deps
web-deps: ## Install the dashboard's dependencies, fetching what the cache lacks (browser UA)
	cd web && npm install --no-audit --no-fund --user-agent "$(BROWSER_UA)"

.PHONY: deps
deps: ## Download every module in go.mod via git with the browser UA, then tidy offline
	$(call fetch,)
	$(TIDY)

.PHONY: dep
dep: ## Fetch one module for a new import: make dep MOD=module@version (then import it and run make deps)
	@test -n "$(MOD)" || { echo "usage: make dep MOD=module@version"; exit 2; }
	$(call fetch,$(MOD))

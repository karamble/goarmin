# goarmin: self-hosted Garmin sync and health dashboard
#
# make help lists every target. make start runs the server on this machine, with the sync role
# (it needs the host's Bluetooth, BlueZ over D-Bus). docker/ runs the server alone in a container
# (the Docker targets below, docker/README.md).
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
# A Gadgetbridge clone, read by make fitprofile (the community's description of FIT).
GADGETBRIDGE ?= $(HOME)/go/src/codeberg.org/Freeyourgadget/Gadgetbridge
# The exercise figures (make figures): Blender, and the mannequin from Blender Studio's Human Base
# Meshes (CC0), trimmed into the repository (make figures-asset BUNDLE=… writes it again).
BLENDER ?= blender
FIGURES_BLEND ?= tools/figures/blender/mannequin.blend
SERVE_ARGS ?= -with-sync
STATE_DIR  ?= $(or $(XDG_STATE_HOME),$(HOME)/.local/state)/goarmin
URL        := http://localhost:$(lastword $(subst :, ,$(LISTEN)))
SQLITE     ?= sqlite3
# GNU time (not the shell keyword): wall time and peak memory for make bench-import.
TIME       ?= /usr/bin/time
DB         := $(DATA)/goarmin.db
SERVE_SH   := LISTEN="$(LISTEN)" STATE_DIR="$(STATE_DIR)" scripts/server.sh
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
	@$(SERVE_SH) start $(START)

.PHONY: stop
stop: ## Stop the server; a running sync ends cleanly first (FORCE=1 kills it after 60 s)
	@$(SERVE_SH) stop $(if $(FORCE),force)

.PHONY: restart
restart: build ## Rebuild, stop and start the server
	@$(SERVE_SH) stop && $(SERVE_SH) start $(START)

.PHONY: run
run: build ## Build, then run the server in the foreground (Ctrl+C stops it)
	@echo "$(GREEN)goarmin on $(URL), data in $(DATA) (Ctrl+C stops it)$(NC)"
	./$(BIN) serve -listen $(LISTEN) -data "$(DATA)" $(SERVE_ARGS)

.PHONY: status
status: ## Show whether the server runs, where, and its last log lines
	@$(SERVE_SH) status || true

.PHONY: logs
logs: ## Show the last 100 lines of the server log
	@tail -n 100 "$(STATE_DIR)/serve.log" 2>/dev/null || echo "no log yet at $(STATE_DIR)/serve.log (make start writes it)"

.PHONY: logs-follow
logs-follow: ## Follow the server log
	@$(SERVE_SH) logs

.PHONY: health
health: ## Check that the server answers
	@curl -fs -m 3 -A "$(BROWSER_UA)" -o /dev/null "$(URL)/api/health" && \
		echo "$(GREEN)✓ goarmin answers on $(URL)$(NC)" || \
		{ echo "$(RED)✗ goarmin does not answer on $(URL)$(NC)"; exit 1; }

.PHONY: open
open: ## Open the dashboard in the browser
	@xdg-open "$(URL)" >/dev/null 2>&1 &

# ============================================
# Sync node (this computer syncs its watches for a server elsewhere)
# ============================================
##@ Sync node (goarmin sync -server; no service: after a reboot, make node-start)

# The server this computer syncs for: SERVER=… on the command line, or NODE_SERVER in .env.
NODE_SERVER ?=
SERVER      ?= $(NODE_SERVER)
NODE_ARGS   ?=
NODE        := STATE_DIR="$(STATE_DIR)" scripts/node.sh

.PHONY: node-login
node-login: build-go ## Sign this computer in to a server: make node-login SERVER=http://host:8642 [EMAIL=…] (asks the password)
	@test -n "$(SERVER)" || { echo "usage: make node-login SERVER=http://host:8642 [EMAIL=you@example.com]"; exit 2; }
	./$(BIN) node login -data "$(DATA)" -server "$(SERVER)" $(if $(EMAIL),-email "$(EMAIL)")

.PHONY: node-start
node-start: build-go ## Start the sync node in the background (log: make node-logs); NODE_ARGS=-no-archive keeps files on the watch
	@$(NODE) start ./$(BIN) "$(SERVER)" -data "$(DATA)" $(NODE_ARGS)

.PHONY: node-stop
node-stop: ## Stop the sync node; a running sync ends cleanly first (FORCE=1 kills it after 60 s)
	@$(NODE) stop $(if $(FORCE),force)

.PHONY: node-restart
node-restart: build-go ## Rebuild, stop and start the sync node
	@$(NODE) stop && $(NODE) start ./$(BIN) "$(SERVER)" -data "$(DATA)" $(NODE_ARGS)

.PHONY: node-status
node-status: build-go ## Show whether the sync node runs, the server, and each watch's state
	@$(NODE) status || true
	@./$(BIN) node status -data "$(DATA)" || true

.PHONY: node-logs
node-logs: ## Follow the sync node's log
	@$(NODE) logs

.PHONY: node-logout
node-logout: build-go ## Forget this computer's sign-in (revoke it in the dashboard too)
	./$(BIN) node logout -data "$(DATA)"

# ============================================
# Backup and Restore
# ============================================
##@ Backup and restore

.PHONY: backup
backup: build-go ## Back up the database, raw files and watch spool (safe while the server runs)
	@./$(BIN) backup -data "$(DATA)" -out "$(BACKUP_DIR)"

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
	@$(SERVE_SH) status 2>/dev/null | head -1 || true
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

.PHONY: bench-import
bench-import: build-go ## Time an import on a throwaway copy of the data: make bench-import FILE=export.zip (the data is not touched)
	@test -n "$(FILE)" || { echo "usage: make bench-import FILE=path/to/export.zip [EMAIL=…]"; exit 2; }
	@test -x "$(TIME)" || { echo "GNU time is missing at $(TIME): install it (Arch: pacman -S time), see make check-tools"; exit 1; }
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	echo "$(BLUE)Copying the data to $$tmp$(NC) (deleted afterwards)"; \
	$(SQLITE) "file:$(DB)?mode=ro" ".backup '$$tmp/goarmin.db'" && cp -r "$(DATA)/raw" "$$tmp/" 2>/dev/null; \
	$(TIME) -f "$(GREEN)wall %e s, user %U s, system %S s, peak memory %M KB$(NC)" \
		./$(BIN) import -data "$$tmp" -user "$(EMAIL)" -source gc_export "$(FILE)" | grep -v '@'

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
# Docker
# ============================================
##@ Docker (the server in a container; docker/README.md)

# The local test runs on a scratch copy of DATA, kept outside the repository, on DOCKER_DEV_PORT.
DOCKER_DEV      ?= $(STATE_DIR)/docker-dev
DOCKER_DEV_PORT ?= 18642
DOCKER_DEV_ENV  := GOARMIN_VERSION="$(VERSION)" GOARMIN_DATA="$(DOCKER_DEV)/data" GOARMIN_BACKUP="$(DOCKER_DEV)/backup" \
	GOARMIN_ENV_FILE="$(DOCKER_DEV)/goarmin.env" GOARMIN_UID="$$(id -u)" GOARMIN_GID="$$(id -g)" GOARMIN_DEV_PORT="$(DOCKER_DEV_PORT)"
DOCKER_DEV_COMPOSE := $(DOCKER_DEV_ENV) docker compose -p goarmin-dev -f docker/compose.yaml -f docker/compose.dev.yaml

.PHONY: docker-image
docker-image: ## Build the image goarmin:$(VERSION) (also tagged goarmin:local)
	docker build -f docker/Dockerfile --build-arg VERSION="$(VERSION)" -t goarmin:"$(VERSION)" -t goarmin:local .

.PHONY: docker-dev-up
docker-dev-up: build-go docker-image ## Run the image on a scratch copy of your data at http://localhost:18642
	@if [ ! -s "$(DOCKER_DEV)/data/goarmin.db" ]; then \
		echo "$(BLUE)Copying the data to $(DOCKER_DEV)$(NC) (make docker-dev-clean removes it)"; \
		mkdir -p "$(DOCKER_DEV)/data" "$(DOCKER_DEV)/backup" && chmod 700 "$(DOCKER_DEV)" && \
		./$(BIN) backup -data "$(DATA)" -out "$(DOCKER_DEV)/backup" && \
		tar -xzf "$$(ls -1t "$(DOCKER_DEV)"/backup/goarmin_backup_*.tar.gz | head -1)" --strip-components=1 \
			-C "$(DOCKER_DEV)/data" --wildcards '*/goarmin.db' '*/raw' && \
		rm -f "$(DOCKER_DEV)"/backup/goarmin_backup_*.tar.gz; \
	fi
	@umask 077; { [ -n "$(GOARMIN_TILE_URL)" ] && echo "GOARMIN_TILE_URL=$(GOARMIN_TILE_URL)"; \
		[ -n "$(GOARMIN_TILE_TOKEN)" ] && echo "GOARMIN_TILE_TOKEN=$(GOARMIN_TILE_TOKEN)"; true; } > "$(DOCKER_DEV)/goarmin.env"
	@$(DOCKER_DEV_COMPOSE) up -d
	@echo "$(GREEN)goarmin in Docker on http://localhost:$(DOCKER_DEV_PORT)$(NC) (make docker-dev-logs, docker-dev-down)"

.PHONY: docker-dev-down
docker-dev-down: ## Stop the local test container (the scratch data stays)
	@$(DOCKER_DEV_COMPOSE) down

.PHONY: docker-dev-status
docker-dev-status: ## Show the local test container and its health
	@$(DOCKER_DEV_COMPOSE) ps

.PHONY: docker-dev-logs
docker-dev-logs: ## Show the local test container's log
	@$(DOCKER_DEV_COMPOSE) logs --tail 100

.PHONY: docker-dev-health
docker-dev-health: ## Check that the local test container answers
	@curl -fs -m 3 -o /dev/null "http://localhost:$(DOCKER_DEV_PORT)/healthz" && \
		echo "$(GREEN)✓ goarmin in Docker answers on http://localhost:$(DOCKER_DEV_PORT)$(NC)" || \
		{ echo "$(RED)✗ goarmin in Docker does not answer on http://localhost:$(DOCKER_DEV_PORT)$(NC)"; exit 1; }

.PHONY: docker-dev-backup
docker-dev-backup: ## Write a backup from inside the local test container (as a nightly job would)
	@$(DOCKER_DEV_COMPOSE) exec -T goarmin goarmin backup -out /backup -keep-days 7 -keep-min 3

.PHONY: docker-dev-clean
docker-dev-clean: docker-dev-down ## Stop the local test container and delete its scratch data
	rm -rf "$(DOCKER_DEV)"

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

.PHONY: figures
figures: ## Rebuild the exercise figures (mannequin + every exercise's poses) with Blender (docs/figures.md; BLENDER=…)
	@mkdir -p bin
	$(GO) run ./tools/figures/exercises > bin/figures-exercises.json
	$(BLENDER) -b --factory-startup --python-exit-code 1 -P tools/figures/blender/build.py -- \
		--bundle "$(FIGURES_BLEND)" --exercises bin/figures-exercises.json \
		--data internal/figures/data --mesh web/src/figures/assets/mannequin.glb

.PHONY: figures-asset
figures-asset: ## Write the mannequin asset again from the Human Base Meshes bundle: make figures-asset BUNDLE=…/human_base_meshes_bundle.blend
	@test -f "$(BUNDLE)" || { echo "BUNDLE: the Human Base Meshes v1.4.1 bundle .blend (Blender Studio, CC0)"; exit 1; }
	$(BLENDER) -b --factory-startup --python-exit-code 1 -P tools/figures/blender/extract.py -- \
		--bundle "$(BUNDLE)" --out "$(FIGURES_BLEND)"

.PHONY: fitprofile
fitprofile: ## Regenerate pkg/fitprofile (FIT messages, fields, file types, devices) from the FIT profile and Gadgetbridge's (GADGETBRIDGE=…)
	$(GO) run ./tools/fitprofilegen -gadgetbridge "$(GADGETBRIDGE)"

# ============================================
# Dependencies
# ============================================
##@ Dependencies

.PHONY: check-tools
check-tools: ## Check the tools goarmin needs on this machine (go, node, npm, sqlite3, GNU time; optional: Blender, ImageMagick)
	@ok=1; for t in go node npm $(SQLITE) $(TIME); do \
		if command -v "$$t" >/dev/null 2>&1; then printf "  $(GREEN)ok$(NC)       %s\n" "$$t"; \
		else printf "  $(RED)missing$(NC)  %s\n" "$$t"; ok=0; fi; done; \
	if [ -x "$(TIME)" ] && ! $(TIME) --version >/dev/null 2>&1; then echo "  $(RED)$(TIME) is not GNU time$(NC)"; ok=0; fi; \
	if [ ! -x "$(TIME)" ]; then echo "  install GNU time (Arch: pacman -S time)"; fi; \
	for t in $(BLENDER) montage; do \
		if command -v "$$t" >/dev/null 2>&1; then printf "  $(GREEN)ok$(NC)       %s (optional: exercise figures)\n" "$$t"; \
		else printf "  optional %s missing (only for make figures and contact sheets, docs/figures.md)\n" "$$t"; fi; done; \
	[ $$ok = 1 ] || exit 1

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

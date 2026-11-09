# Run from monitor-central, or use: make -C monitor-central <target>
.DEFAULT_GOAL := help
.DELETE_ON_ERROR:

GO           ?= go
GOFMT        ?= gofmt
BIN_DIR      ?= $(CURDIR)/bin
ENV_FILE     ?= .env
COMPOSE_FILE := deployments/compose.yaml
COMPOSE      ?= docker compose
DC           = CENTRAL_ENV_FILE="$(abspath $(ENV_FILE))" $(COMPOSE) --env-file "$(ENV_FILE)" -f "$(COMPOSE_FILE)"

# Central has no dependency on its sibling modules or the workspace file.
export GOWORK := off

.PHONY: help audit backend/test backend/test/integration backend/test/cover \
        backend/upgradeable backend/tidy backend/build backend/run key-secret \
        db/migrate seed/admin seed/demo seed/demo-clean docker/build docker/up \
        docker/up/build docker/db/up docker/down docker/stop/backend docker/logs \
        docker/status docker/restart/backend docker/migrate docker/seed-admin \
        docker/seed-demo docker/seed-demo-clean dev

## help: show available commands
help:
	@printf 'Usage: make <target>\n\n'
	@awk '/^## / {sub(/^## /, ""); split($$0, parts, ": "); printf "  %-26s %s\n", parts[1], parts[2]}' $(MAKEFILE_LIST)

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## audit: run tests, module checks, formatting checks, and vet
audit: backend/test
	$(GO) mod tidy -diff
	$(GO) mod verify
	@files="$$($(GOFMT) -l .)"; if [ -n "$$files" ]; then printf 'Unformatted Go files:\n%s\n' "$$files"; exit 1; fi
	$(GO) vet ./...

## backend/test: run central tests with the race detector
backend/test:
	$(GO) test -race -count=1 ./...

## backend/test/integration: run SQL tests using an explicit isolated MONITOR_TEST_DSN
backend/test/integration:
	@test -n "$$MONITOR_TEST_DSN" || { printf 'Set MONITOR_TEST_DSN to an isolated test database; application data must not be used.\n'; exit 1; }
	$(GO) test -race -tags integration -count=1 -v ./internal/integration

## backend/test/cover: write bin/coverage.out and print statement coverage
backend/test/cover:
	@mkdir -p "$(BIN_DIR)"
	$(GO) test -race -count=1 -coverprofile="$(BIN_DIR)/coverage.out" ./...
	$(GO) tool cover -func="$(BIN_DIR)/coverage.out"

## backend/upgradeable: list module versions and available updates
backend/upgradeable:
	$(GO) list -mod=readonly -u -m all

# ==================================================================================== #
# LOCAL BACKEND AND ADMINISTRATION (requires Go)
# ==================================================================================== #

## backend/tidy: tidy module files and format Go sources
backend/tidy:
	$(GO) mod tidy -v
	$(GO) fmt ./...

## backend/build: build all central command binaries into bin/
# Always rebuild so source and embedded SQL changes are picked up.
backend/build:
	@mkdir -p "$(BIN_DIR)"
	$(GO) build -trimpath -o "$(BIN_DIR)/" ./cmd/...

## backend/run: start central locally using its .env database settings
backend/run:
	$(GO) run ./cmd/app

## key-secret: configure HOST_API_KEY_SECRET in the private local .env
key-secret:
	$(GO) run ./cmd/key-secret

## db/migrate: initialize or upgrade the database using local .env settings
db/migrate:
	$(GO) run ./cmd/migrate

## seed/admin: provision the administrator using local .env settings
seed/admin:
	$(GO) run ./cmd/seed

## seed/demo: create marked demo data using local .env settings
seed/demo:
	$(GO) run ./cmd/seed-demo

## seed/demo-clean: delete only marked demo data using local .env settings
seed/demo-clean:
	$(GO) run ./cmd/seed-demo-clean

# ==================================================================================== #
# DOCKER COMPOSE
# ==================================================================================== #

## docker/build: build backend and migration images without starting services
docker/build:
	$(DC) build backend migrate

## docker/up: start PostgreSQL, run migrations, then start the backend
docker/up:
	$(DC) up -d backend

## docker/up/build: rebuild and start PostgreSQL, migrations, and the backend
docker/up/build:
	$(DC) up --build -d backend

## docker/db/up: start only PostgreSQL for local Go development
docker/db/up:
	$(DC) up -d --wait postgres

## docker/down: stop the Compose stack and preserve database volumes
docker/down:
	$(DC) down

## docker/stop/backend: stop only the backend before a database upgrade
docker/stop/backend:
	$(DC) stop backend

## docker/logs: follow backend and migration logs
docker/logs:
	$(DC) logs -f backend migrate

## docker/status: show Compose service status
docker/status:
	$(DC) ps -a

## docker/restart/backend: rebuild and recreate backend with dependency checks
docker/restart/backend:
	$(DC) up --build --force-recreate -d backend

## docker/migrate: run the migration command in a fresh one-shot container
docker/migrate:
	$(DC) run --rm --build migrate

# These commands require an already initialized/running database. Compose run
# does not publish the backend port, and --no-deps avoids starting the web app.
## docker/seed-admin: provision the administrator against Compose PostgreSQL
docker/seed-admin:
	$(DC) run --rm --no-deps backend /app/bin/seed

## docker/seed-demo: create marked demo data against Compose PostgreSQL
docker/seed-demo:
	$(DC) run --rm --no-deps backend /app/bin/seed-demo

## docker/seed-demo-clean: delete marked demo data against Compose PostgreSQL
docker/seed-demo-clean:
	$(DC) run --rm --no-deps backend /app/bin/seed-demo-clean

## dev: build and start the complete central development stack
dev: docker/up/build

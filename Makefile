GO ?= go
PSQL ?= psql
GO_DIR := backend/go
GOVULNCHECK_VERSION := v1.9.0
SQLC_VERSION := v1.31.1
COMPOSE ?= docker compose -f compose.yaml -f compose.dev.yaml
DOCKER_GO_IMAGE ?= cyberguard-go:local
DOCKER_WEB_IMAGE ?= cyberguard-web:local
AI_VM_CONFIG ?= /etc/cyberguard/ai/config.env
APP_VM_CONFIG ?= /etc/cyberguard/app/config.env

.DEFAULT_GOAL := help
.PHONY: help go-run go-build go-fmt go-fmt-check go-test go-test-race go-vet go-vuln go-check go-test-integration
.PHONY: db-bootstrap-identity db-bootstrap-incidents
.PHONY: go-sqlc go-sqlc-check db-migrate-identity db-status-identity db-seed-demo db-migrate-incidents db-status-incidents
.PHONY: docker-secrets docker-build docker-db-up docker-bootstrap docker-migrate docker-status docker-seed-demo docker-up docker-down docker-logs
.PHONY: docker-config-check docker-test-secrets
.PHONY: docker-caddy-check
.PHONY: docker-smoke
.PHONY: docker-test-readiness
.PHONY: docker-test-web
.PHONY: docker-build-go docker-build-web frontend-test-packaged
.PHONY: frontend-install frontend-dev frontend-check frontend-test-e2e frontend-test-e2e-live
.PHONY: frontend-test-incidents-live
.PHONY: ai-vm-config ai-vm-build ai-vm-models ai-vm-up ai-vm-check ai-vm-status ai-vm-logs ai-vm-stop
.PHONY: ai-vm-test ai-vm-test-bootstrap ai-vm-smoke
.PHONY: app-vm-config app-vm-build app-vm-create-secrets app-vm-db-up app-vm-bootstrap app-vm-migrate app-vm-migration-status app-vm-bootstrap-ai app-vm-seed-demo
.PHONY: app-vm-up app-vm-check app-vm-check-ai app-vm-status app-vm-logs app-vm-stop app-vm-maintenance-on app-vm-maintenance-off app-vm-quiesce
.PHONY: app-vm-test app-vm-test-bootstrap app-vm-smoke

help:
	@printf '%s\n' \
	  'go-run              Run the API with the current environment' \
	  'go-build            Build API, migration and seed binaries' \
	  'go-fmt              Format Go source files' \
	  'go-fmt-check        Check formatting without modifying files' \
	  'go-test             Run unit and HTTP tests (no PostgreSQL required)' \
	  'go-test-race        Run tests with the race detector' \
	  'go-vet              Run go vet' \
	  'go-vuln             Run pinned govulncheck (requires network access)' \
	  'go-check            Check formatting, vet, race tests and vulnerabilities' \
	  'go-test-integration Run database tests against two dedicated PostgreSQL instances'
	@printf '%s\n' \
	  'frontend-install    Install locked frontend dependencies with npm ci' \
	  'frontend-dev        Run Vite at http://127.0.0.1:5173' \
	  'frontend-check      Check types, lint, formatting, unit tests and build' \
	  'frontend-test-e2e   Run isolated Firefox and Chromium browser tests' \
	  'frontend-test-e2e-live Run real-backend browser auth tests (E2E_PASSWORD_FILE required)' \
	  'frontend-test-incidents-live Run browser create/read/isolation against explicit disposable fixture'
	@printf '%s\n' \
	  'db-bootstrap-identity  Create identity database and restricted roles (admin connection)' \
	  'db-bootstrap-incidents Create incident database and restricted roles (admin connection)'
	@printf '%s\n' \
	  'go-sqlc             Regenerate committed identity query code' \
	  'go-sqlc-check       Check generated code without editing it' \
	  'db-migrate-identity Apply pending identity migrations (migration connection)' \
	  'db-status-identity  Report current and target identity migration versions' \
	  'db-migrate-incidents Apply incident schema migrations (migration connection)' \
	  'db-status-incidents Report current and target incident migration versions' \
	  'db-seed-demo        Explicitly create the four development accounts'
	@printf '%s\n' \
	  'docker-secrets      Generate local secret files without overwriting existing ones' \
	  'docker-build        Build separate Go and frontend/Caddy images' \
	  'docker-build-go     Build only the Go API/migrate/seed image' \
	  'docker-build-web    Build only the static frontend/Caddy image' \
	  'docker-db-up        Start both local databases and wait for health' \
	  'docker-bootstrap    Explicitly bootstrap database roles with one-off tools' \
	  'docker-migrate      Explicitly migrate both databases' \
	  'docker-status       Report migration versions in both databases' \
	  'docker-seed-demo    Explicitly seed local demo accounts' \
	  'docker-up           Start the local application without running setup' \
	  'docker-down         Remove containers/networks, preserving data volumes' \
	  'docker-logs         Show the last 100 service log lines' \
	  'docker-config-check Validate Compose configuration, including one-off tools' \
	  'docker-caddy-check  Validate Caddy configuration in its runtime image' \
	  'docker-smoke        Build and verify a fresh disposable local Docker environment' \
	  'docker-test-secrets Test local credential generation (requires openssl)' \
	  'docker-test-readiness Test PostgreSQL healthchecks during first initialization' \
	  'docker-test-web     Test packaged HTTP assertions with synthetic responses'
	@printf '%s\n' \
	  'ai-vm-config       Validate the standalone AI VM Compose configuration' \
	  'ai-vm-build        Explicitly build/pull AI VM runtime images (no weights)' \
	  'ai-vm-models       Explicitly download and verify pinned model artifacts' \
	  'ai-vm-up           Check prerequisites and start the prepared AI VM stack' \
	  'ai-vm-check        Probe AI VM storage/services/TLS without claiming jobs' \
	  'ai-vm-status       Show standalone AI VM services' \
	  'ai-vm-logs         Show bounded AI VM service logs' \
	  'ai-vm-stop         Stop AI VM services, retaining all models and secrets' \
	  'ai-vm-test         Run non-privileged deployment safety tests (ShellCheck)' \
	  'ai-vm-test-bootstrap Test bootstrap in disposable Ubuntu with mocked apt/systemd' \
	  'ai-vm-smoke        Run disposable real-service deployment checks with synthetic models'
	@printf '%s\n' 'frontend-test-packaged Run real browser flows through disposable Caddy (explicit E2E settings)'
	@printf '%s\n' \
	  'app-vm-config/build Validate/build the application VM deployment (no startup)' \
	  'app-vm-create-secrets Generate initial TLS URLs/passwords without overwrites' \
	  'app-vm-db-up/bootstrap/migrate Explicit application database provisioning' \
	  'app-vm-migration-status Require current schema and migration image versions' \
	  'app-vm-bootstrap-ai Provision the restricted AI role (not ordinary startup)' \
	  'app-vm-seed-demo Explicit demo-only accounts; requires acknowledgement' \
	  'app-vm-up/check    Start/check the prepared application (AI may be offline)' \
	  'app-vm-check-ai    Check remote AI TLS and local worker safety grants' \
	  'app-vm-maintenance-on/off Pause/resume public writes without breaking private reads' \
	  'app-vm-quiesce     Pause writes and stop/block Go for explicit migrations' \
	  'app-vm-status/logs/stop Inspect/stop services, retaining all persistent state' \
	  'app-vm-test        Run deployment configuration/credential safety checks' \
	  'app-vm-test-bootstrap Test preparation in disposable Ubuntu with mocks' \
	  'app-vm-smoke       Run disposable application deployment/TLS checks'

go-run:
	cd $(GO_DIR) && $(GO) run ./cmd/api

go-build:
	cd $(GO_DIR) && $(GO) build -o bin/api ./cmd/api
	cd $(GO_DIR) && $(GO) build -o bin/migrate ./cmd/migrate
	cd $(GO_DIR) && $(GO) build -o bin/seed ./cmd/seed

go-fmt:
	cd $(GO_DIR) && gofmt -w cmd internal

go-fmt-check:
	@cd $(GO_DIR) && files="$$(gofmt -l cmd internal)" && \
	  if [ -n "$$files" ]; then printf 'Run make go-fmt:\n%s\n' "$$files"; exit 1; fi

go-test:
	cd $(GO_DIR) && $(GO) test ./...

go-test-race:
	cd $(GO_DIR) && $(GO) test -race ./...

go-vet:
	cd $(GO_DIR) && $(GO) vet ./...

go-vuln:
	cd $(GO_DIR) && $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

go-check: go-fmt-check go-sqlc-check go-vet go-test-race go-vuln

go-test-integration:
	cd $(GO_DIR) && $(GO) test -race -tags=integration -count=1 ./...

db-bootstrap-identity:
	@test -n "$$IDENTITY_ADMIN_DATABASE_URL" || { printf '%s\n' 'IDENTITY_ADMIN_DATABASE_URL is required'; exit 1; }
	@PGCONNECT_TIMEOUT=5 $(PSQL) -X --no-password --dbname "$$IDENTITY_ADMIN_DATABASE_URL" --file db/bootstrap/identity.sql

db-bootstrap-incidents:
	@test -n "$$INCIDENT_ADMIN_DATABASE_URL" || { printf '%s\n' 'INCIDENT_ADMIN_DATABASE_URL is required'; exit 1; }
	@PGCONNECT_TIMEOUT=5 $(PSQL) -X --no-password --dbname "$$INCIDENT_ADMIN_DATABASE_URL" --file db/bootstrap/incidents.sql

go-sqlc:
	cd $(GO_DIR) && $(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

go-sqlc-check:
	cd $(GO_DIR) && $(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) diff

db-migrate-identity:
	cd $(GO_DIR) && $(GO) run ./cmd/migrate -dir ../../db/migrations/identity

db-status-identity:
	cd $(GO_DIR) && $(GO) run ./cmd/migrate -dir ../../db/migrations/identity -status

db-migrate-incidents:
	cd $(GO_DIR) && $(GO) run ./cmd/migrate -database incidents -dir ../../db/migrations/incidents

db-status-incidents:
	cd $(GO_DIR) && $(GO) run ./cmd/migrate -database incidents -dir ../../db/migrations/incidents -status

db-seed-demo:
	cd $(GO_DIR) && $(GO) run ./cmd/seed -demo

docker-secrets:
	sh docker/create-secrets.sh

docker-build: docker-build-go docker-build-web

docker-build-go:
	docker build -f backend/go/Dockerfile -t "$(DOCKER_GO_IMAGE)" .

docker-build-web:
	docker build -f frontend/Dockerfile -t "$(DOCKER_WEB_IMAGE)" .

docker-db-up:
	$(COMPOSE) up -d --wait --wait-timeout 60 identity-db incident-db

docker-bootstrap:
	$(COMPOSE) run --rm bootstrap-identity
	$(COMPOSE) run --rm bootstrap-incidents

docker-migrate:
	$(COMPOSE) run --rm migrate-identity
	$(COMPOSE) run --rm migrate-incidents

docker-status:
	$(COMPOSE) run --rm migrate-identity /app/migrate -dir /app/migrations/identity -status
	$(COMPOSE) run --rm migrate-incidents /app/migrate -database incidents -dir /app/migrations/incidents -status

docker-seed-demo:
	$(COMPOSE) run --rm seed-demo

docker-up:
	$(COMPOSE) up -d --no-build --wait --wait-timeout 60

docker-down:
	$(COMPOSE) down

docker-logs:
	$(COMPOSE) logs --tail 100

docker-config-check:
	$(COMPOSE) --profile tools config --quiet

docker-test-secrets:
	sh docker/test-secrets.sh

docker-test-readiness:
	$(COMPOSE) config --format json | bash docker/test-readiness.sh

docker-test-web:
	bash docker/test-web-regressions.sh

docker-caddy-check:
	@image="$$($(COMPOSE) config --format json | jq -er '.services.web.image')" && \
	  docker run --rm --read-only --network none --user 10001:10001 \
	  "$$image" caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
	@image="$$($(COMPOSE) config --format json | jq -er '.services.web.image')" && \
	  docker run --rm --read-only --network none --user 10001:10001 -e AI_HANDLER=Caddyfile.ai \
	  "$$image" caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile

docker-smoke: docker-build
	bash docker/smoke.sh

ai-vm-config ai-vm-build ai-vm-up ai-vm-check ai-vm-status ai-vm-logs ai-vm-stop:
	bash deploy/ai-vm/manage.sh --config "$(AI_VM_CONFIG)" "$(@:ai-vm-%=%)"

ai-vm-models:
	bash deploy/ai-vm/prepare-models.sh --config "$(AI_VM_CONFIG)"

ai-vm-test:
	bash deploy/ai-vm/test.sh

ai-vm-test-bootstrap:
	docker run --rm --network none --volume "$(CURDIR):/repo:ro" \
	  ubuntu:26.04@sha256:6a06111c3b0a8600feadb270aec771dc239bf407b25625f93f44ab00e11f4d3c \
	  bash /repo/deploy/ai-vm/tests/bootstrap.sh

ai-vm-smoke: docker-build-go
	docker build -f backend/python/Dockerfile -t cyberguard-ai:azure .
	bash deploy/ai-vm/smoke.sh

app-vm-config app-vm-build app-vm-create-secrets app-vm-db-up app-vm-bootstrap app-vm-migrate app-vm-migration-status app-vm-bootstrap-ai app-vm-seed-demo app-vm-up app-vm-check app-vm-check-ai app-vm-status app-vm-logs app-vm-stop app-vm-maintenance-on app-vm-maintenance-off app-vm-quiesce:
	bash deploy/app-vm/manage.sh --config "$(APP_VM_CONFIG)" "$(@:app-vm-%=%)"

app-vm-test:
	bash deploy/app-vm/test.sh

app-vm-test-bootstrap:
	docker run --rm --network none --volume "$(CURDIR):/repo:ro" \
	  ubuntu:26.04@sha256:6a06111c3b0a8600feadb270aec771dc239bf407b25625f93f44ab00e11f4d3c \
	  bash /repo/deploy/app-vm/tests/bootstrap.sh

app-vm-smoke: docker-build
	bash deploy/app-vm/smoke.sh

frontend-install:
	cd frontend && npm ci

frontend-dev:
	cd frontend && npm run dev

frontend-check:
	cd frontend && npm run check

frontend-test-e2e:
	cd frontend && npm run test:e2e

frontend-test-e2e-live:
	cd frontend && npm run test:e2e:live

frontend-test-incidents-live:
	cd frontend && npm run test:e2e:incidents-live

frontend-test-packaged:
	cd frontend && npm run test:e2e:packaged

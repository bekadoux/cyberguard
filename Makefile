GO ?= go
GO_DIR := backend/go
GOVULNCHECK_VERSION := v1.9.0

.DEFAULT_GOAL := help
.PHONY: help go-run go-build go-fmt go-fmt-check go-test go-test-race go-vet go-vuln go-check go-test-integration

help:
	@printf '%s\n' \
	  'go-run              Run the API with the current environment' \
	  'go-build            Build backend/go/bin/api' \
	  'go-fmt              Format Go source files' \
	  'go-fmt-check        Check formatting without modifying files' \
	  'go-test             Run unit and HTTP tests (no PostgreSQL required)' \
	  'go-test-race        Run tests with the race detector' \
	  'go-vet              Run go vet' \
	  'go-vuln             Run pinned govulncheck (requires network access)' \
	  'go-check            Check formatting, vet, race tests and vulnerabilities' \
	  'go-test-integration Run database tests against two dedicated PostgreSQL instances'

go-run:
	cd $(GO_DIR) && $(GO) run ./cmd/api

go-build:
	cd $(GO_DIR) && $(GO) build -o bin/api ./cmd/api

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

go-check: go-fmt-check go-vet go-test-race go-vuln

go-test-integration:
	cd $(GO_DIR) && $(GO) test -race -tags=integration -count=1 ./...

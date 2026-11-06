.DEFAULT_GOAL := help

# Linters run through `go run` at pinned versions, so nothing needs
# installing. Keep them in step with .github/workflows/ci.yml.
#
# v0.8.2 is staticcheck 2026.2.2, the newest release as of 2026-11-06,
# and its notes say it supports the export data format of Go 1.27.2.
# The `(compile)` errors this target reported on untouched files are
# therefore not a question of an older version being pinned; CI is the
# first place that failure is reproducible on a clean machine.
STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.8.2
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0

.PHONY: help
help: ## List available commands
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: migrate
migrate: ## Apply pending database migrations
	go run ./cmd/migrate

.PHONY: status
status: ## Show what is in the pantry
	go run ./cmd/despensa status

.PHONY: build
build: ## Build every binary into bin/
	go build -trimpath -o bin/ ./cmd/...

.PHONY: mcp
mcp: ## Serve the pantry as MCP tools over stdio
	go run ./cmd/mcp

.PHONY: api
api: ## Serve the WhatsApp webhook
	go run ./cmd/api

.PHONY: digest
digest: ## Send the weekly shopping list once and exit
	go run ./cmd/digest

.PHONY: test
test: ## Run tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Generate the coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "report written to coverage.html"

.PHONY: check
check: fmt-check ## Run everything CI runs
	go vet ./...
	go run $(STATICCHECK) ./...
	go test -race ./...
	go run $(GOVULNCHECK) ./...

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-clean
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
	  echo "Files not gofmt-clean:"; echo "$$out"; exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin coverage.out coverage.html

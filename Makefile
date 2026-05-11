.PHONY: help dev dev-fixtures prod lint lint-go lint-frontend lint-prettier-root format format-go format-frontend format-prettier-root test test-go test-frontend test-e2e clean-test-tmp

SHELL := bash
FRONTEND_DIR := frontend
# Force pure-Go builds for all `go` invocations below (matches the `-tags
# netgo` builds used in test/dev/prod recipes).
export CGO_ENABLED := 0

# Scenario loaders source the dotenv-style files directly so `make` targets and
# `wa --env ...` share the same environment naming.
LOAD_TEST_ENV = \
	if [ "$${WA_ENV:-}" = "production" ]; then \
		printf '%s\n' 'make test refuses WA_ENV=production inherited from the shell.' >&2; \
		exit 1; \
	fi; \
	if [ -n "$${WA_RESULTS_DB_PATH:-}" ]; then \
		printf 'make test refuses WA_RESULTS_DB_PATH=%s inherited from the shell.\n' "$${WA_RESULTS_DB_PATH}" >&2; \
		exit 1; \
	fi; \
	if [ -f ./.env ]; then set -a; . ./.env; set +a; fi; \
	[ -f ./.env.test ] || { printf '%s\n' '.env.test is required for make test.' >&2; exit 1; }; \
	set -a; . ./.env.test; [ -f ./.env.test.local ] && . ./.env.test.local; set +a

LOAD_DEVELOPMENT_ENV = \
	if [ "$${WA_ENV:-}" = "production" ]; then \
		printf '%s\n' 'make dev refuses WA_ENV=production inherited from the shell.' >&2; \
		exit 1; \
	fi; \
	if [ -f ./.env ]; then set -a; . ./.env; set +a; fi; \
	[ -f ./.env.development ] || { printf '%s\n' '.env.development is required for make dev.' >&2; exit 1; }; \
	set -a; . ./.env.development; [ -f ./.env.local ] && . ./.env.local; [ -f ./.env.development.local ] && . ./.env.development.local; set +a

LOAD_PRODUCTION_ENV = \
	for var in WA_TEST_FRONTEND_PORT WA_TEST_RESULTS_PORT WA_TEST_SEQMETA_PORT WA_DEV_FRONTEND_PORT WA_DEV_RESULTS_PORT WA_DEV_SEQMETA_PORT; do \
		if [ -n "$${!var:-}" ]; then \
			printf 'make prod refuses %s=%s inherited from the shell.\n' "$$var" "$${!var}" >&2; \
			exit 1; \
		fi; \
	done; \
	if [ -f ./.env ]; then set -a; . ./.env; set +a; fi; \
	[ -f ./.env.production ] || { printf '%s\n' '.env.production is required for make prod.' >&2; exit 1; }; \
	set -a; . ./.env.production; [ -f ./.env.local ] && . ./.env.local; [ -f ./.env.production.local ] && . ./.env.production.local; set +a

# Convenience flag for `make dev FIXTURES=1`.
DEV_FIXTURES_FLAG := $(if $(filter 1 yes true,$(FIXTURES)),--fixtures,)

help:
	@printf 'Usage: make <target>\n\n'
	@printf 'Bring-up:\n'
	@printf '  dev               Run the dev stack (no fixtures). Persistent DB from .env.development + .env.development.local.\n'
	@printf '  dev FIXTURES=1    Same as above but seed demo fixtures.\n'
	@printf '  dev-fixtures      Alias for `make dev FIXTURES=1`.\n'
	@printf '  prod              Run the production stack with .env.production + .env.production.local.\n\n'
	@printf 'Quality:\n'
	@printf '  lint              Run Go and frontend linters.\n'
	@printf '  format            Apply Go and frontend formatters.\n'
	@printf '  test              Run Go + Vitest + Playwright tests under .env.test.\n'

# ---- Test scenario --------------------------------------------------------
# Hermetic: ephemeral DBs, throwaway ports, never touches dev/prod state.
# `clean-test-tmp` runs whether the sub-targets pass or fail so .tmp/ never
# accumulates the built `wa` binary, the playwright port-allocation cache, or
# stray SQLite DBs from a killed run-dev.sh.
test:
	@rc=0; \
	$(MAKE) --no-print-directory test-go test-frontend test-e2e || rc=$$?; \
	$(MAKE) --no-print-directory clean-test-tmp; \
	exit $$rc

test-go:
	@$(LOAD_TEST_ENV); go test -tags netgo --count 1 ./...

test-frontend:
	@$(LOAD_TEST_ENV); cd $(FRONTEND_DIR) && pnpm test

test-e2e:
	@$(LOAD_TEST_ENV); cd $(FRONTEND_DIR) && pnpm exec playwright test

# Remove only the artefacts `make test` itself produces under .tmp/. Leaves
# unrelated entries (e.g. agent scratch in .tmp/agent/) untouched.
clean-test-tmp:
	@rm -f .tmp/wa .tmp/playwright-ports.json
	@rm -f .tmp/results-dev.*.sqlite

# ---- Dev scenario ---------------------------------------------------------
dev:
	@$(LOAD_DEVELOPMENT_ENV); ./run-dev.sh --mode dev $(DEV_FIXTURES_FLAG)

dev-fixtures:
	$(MAKE) dev FIXTURES=1

# ---- Production scenario --------------------------------------------------
prod:
	@$(LOAD_PRODUCTION_ENV); ./run-dev.sh --mode prod

# ---- Lint and format (no scenario binding) -------------------------------
lint: lint-go lint-frontend lint-prettier-root

lint-go:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...; \
	fi

lint-frontend:
	cd $(FRONTEND_DIR) && pnpm lint

# Prettier --check on root-level files (markdown, JSON, etc.) using the same
# config and binary as the frontend, mirroring what VS Code applies on save.
lint-prettier-root:
	$(FRONTEND_DIR)/node_modules/.bin/prettier --check .

format: format-go format-frontend format-prettier-root

format-go:
	git ls-files '*.go' | xargs -r gofmt -w
	git ls-files '*.go' | xargs -r cleanorder -min-diff

format-frontend:
	cd $(FRONTEND_DIR) && pnpm format

# Prettier --write on root-level files. Excludes governed by .prettierignore.
format-prettier-root:
	$(FRONTEND_DIR)/node_modules/.bin/prettier --write .

.PHONY: help dev dev-fixtures prod lint lint-go lint-frontend lint-prettier-root format format-go format-frontend format-prettier-root test test-go test-frontend test-e2e

FRONTEND_DIR := frontend
# Force pure-Go builds for all `go` invocations below (matches the `-tags
# netgo` builds used in test/dev/prod recipes).
export CGO_ENABLED := 0

# Each scenario sources exactly one env file via scripts/wa-env.sh and is
# isolated from the others. See DEVELOPING.md for details.
WA_ENV_TEST  := scripts/wa-env.sh test --
WA_ENV_DEV   := scripts/wa-env.sh dev --
WA_ENV_PROD  := scripts/wa-env.sh prod --

# Convenience flag for `make dev FIXTURES=1`.
DEV_FIXTURES_FLAG := $(if $(filter 1 yes true,$(FIXTURES)),--fixtures,)

help:
	@printf 'Usage: make <target>\n\n'
	@printf 'Bring-up:\n'
	@printf '  dev               Run the dev stack (no fixtures). Persistent DB from .env.dev.\n'
	@printf '  dev FIXTURES=1    Same as above but seed demo fixtures.\n'
	@printf '  dev-fixtures      Alias for `make dev FIXTURES=1`.\n'
	@printf '  prod              Run the production stack. Requires .env.prod with WA_ENV=production.\n\n'
	@printf 'Quality:\n'
	@printf '  lint              Run Go and frontend linters.\n'
	@printf '  format            Apply Go and frontend formatters.\n'
	@printf '  test              Run Go + Vitest + Playwright tests under .env.test.\n'

# ---- Test scenario --------------------------------------------------------
# Hermetic: ephemeral DBs, throwaway ports, never touches dev/prod state.
test: test-go test-frontend test-e2e

test-go:
	$(WA_ENV_TEST) go test -tags netgo --count 1 ./...

test-frontend:
	$(WA_ENV_TEST) bash -c 'cd $(FRONTEND_DIR) && pnpm test'

test-e2e:
	$(WA_ENV_TEST) bash -c 'cd $(FRONTEND_DIR) && pnpm exec playwright test'

# ---- Dev scenario ---------------------------------------------------------
dev:
	$(WA_ENV_DEV) ./run-dev.sh --mode dev $(DEV_FIXTURES_FLAG)

dev-fixtures:
	$(MAKE) dev FIXTURES=1

# ---- Production scenario --------------------------------------------------
prod:
	$(WA_ENV_PROD) ./run-dev.sh --mode prod

# ---- Lint and format (no scenario binding) -------------------------------
lint: lint-go lint-frontend lint-prettier-root

lint-go:
	golangci-lint run ./...

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

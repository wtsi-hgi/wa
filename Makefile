.PHONY: run lint lint-go lint-frontend format format-go format-frontend test test-go test-frontend test-e2e

-include .env

.EXPORT_ALL_VARIABLES:

FRONTEND_DIR := frontend
WA_TEST_FRONTEND_PORT ?= 3000
WA_TEST_RESULTS_PORT ?= 8090
WA_TEST_SEQMETA_PORT ?= 8091

run:
	./run-dev.sh --frontend-port $(WA_TEST_FRONTEND_PORT) --results-port $(WA_TEST_RESULTS_PORT) --seqmeta-port $(WA_TEST_SEQMETA_PORT)

lint: lint-go lint-frontend

lint-go:
	golangci-lint run ./...

lint-frontend:
	cd $(FRONTEND_DIR) && pnpm lint

format: format-go format-frontend

format-go:
	git ls-files '*.go' | xargs -r gofmt -w
	git ls-files '*.go' | xargs -r cleanorder -min-diff

format-frontend:
	cd $(FRONTEND_DIR) && pnpm exec prettier --write .

test: test-go test-frontend test-e2e

test-go:
	CGO_ENABLED=1 go test -tags netgo --count 1 ./...

test-frontend:
	cd $(FRONTEND_DIR) && pnpm test

test-e2e:
	cd $(FRONTEND_DIR) && pnpm exec playwright test
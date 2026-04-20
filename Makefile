.PHONY: run lint lint-go lint-frontend format format-go format-frontend test test-go test-frontend

FRONTEND_DIR := frontend

run:
	./run-dev.sh

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

test: test-go test-frontend

test-go:
	CGO_ENABLED=1 go test -tags netgo --count 1 ./...

test-frontend:
	cd $(FRONTEND_DIR) && pnpm test
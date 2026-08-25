.PHONY: gen gen-openapi gen-sdk gen-ts build api cli web docs lint test cover dev fmt clean

# --- Code generation ---

gen: gen-openapi gen-sdk gen-ts

gen-openapi:
	cd go/api && make gen-openapi

gen-sdk:
	cd go/sdk && make gen

gen-ts:
	cd ts && pnpm --filter @sunred/api-client gen

# --- Go ---

api:
	cd go/api && make build

cli:
	cd go/cli && make build

# --- TypeScript ---

web:
	cd ts && pnpm --filter web build

docs:
	cd ts && pnpm --filter docs build

# --- Lint & Test ---

lint: lint-go lint-ts

lint-go:
	cd go/api && make lint
	cd go/cli && go vet ./...

lint-ts:
	cd ts && pnpm lint

test: test-go test-ts

test-go:
	cd go/api && make test
	cd go/relay && make test

test-ts:
	cd ts && pnpm typecheck

# --- Coverage ---

cover: cover-go

cover-go:
	cd go/api && make cover
	cd go/relay && make cover

# --- Dev ---

dev:
	@echo "Starting API and web dev servers..."
	@cd go/api && make run &
	@cd ts && pnpm dev

# --- Formatting ---

fmt: fmt-go fmt-ts

fmt-go:
	cd go/api && make fmt

fmt-ts:
	cd ts && pnpm format

# --- Clean ---

clean:
	cd go/api && make clean
	rm -f go/cli/sunred go/cli/sunred-tui

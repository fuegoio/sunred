.PHONY: build run migrate gen-openapi test cover fmt fmt-check vet lint clean db-up db-down db-reset

build:
	go build -o sunred ./cmd/sunred/

run: build
	./sunred

migrate: build
	./sunred --migrate

gen-openapi: build
	./sunred --openapi 2>/dev/null | python3 -m json.tool > openapi.json
	cp openapi.json ../../ts/apps/docs/openapi/openapi.json
	cd ../../ts/packages/api-client && pnpm gen

test: build
	go test ./... -count=1
cover: build
	go test ./... -count=1 -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1
	@echo "coverage.out written; run 'go tool cover -html=coverage.out' for a detailed view"

fmt:
	@gofmt -w -s $$(find . -name '*.go')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go'))" || { echo "Go files need formatting. Run: make fmt"; exit 1; }

vet:
	go vet ./...

lint:
	go tool golangci-lint run ./...

clean:
	rm -f sunred

db-up:
	docker compose up -d

db-down:
	docker compose down

db-reset:
	docker exec sunred-postgres psql -U sunred -d postgres -c "DROP DATABASE IF EXISTS sunred;" -c "CREATE DATABASE sunred;"

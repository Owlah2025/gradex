-include .env
export

.PHONY: up down migrate-up migrate-down migrate-version run-api run-worker test test-integration seed smoke build fmt ci

up:
	docker compose up -d

down:
	docker compose down

# Migrations run through the repository's own command, so no globally
# installed migrate binary is required. See cmd/migrate.
migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down 1

migrate-version:
	go run ./cmd/migrate version

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

build:
	go build ./...

fmt:
	gofmt -w .

# Mirrors the Backend and Guards CI jobs, so a red pipeline can be reproduced
# without pushing. Migrations and frontend have their own jobs; see
# .github/workflows/ci.yml.
ci:
	@test -z "$$(gofmt -l .)" || (echo "not gofmt-formatted:"; gofmt -l .; exit 1)
	go build ./...
	go vet ./...
	go vet -tags=integration ./...
	go test -race ./...
	../scripts/docs-guard.sh
	../scripts/expose-guard.sh

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

seed:
	docker compose exec -T postgres psql -U gradex -d gradex < scripts/seed.sql

smoke:
	./scripts/smoke_test.sh

-include .env
export

.PHONY: up down migrate-up migrate-down migrate-version run-api run-worker test test-integration seed smoke build

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

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

seed:
	docker compose exec -T postgres psql -U gradex -d gradex < scripts/seed.sql

smoke:
	./scripts/smoke_test.sh

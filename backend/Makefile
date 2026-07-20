include .env
export

MIGRATE := $(shell go env GOPATH)/bin/migrate
MIGRATIONS_DIR := internal/db/migrations

.PHONY: up down migrate-up migrate-down migrate-force run-api run-worker test test-integration seed smoke build

up:
	docker compose up -d

down:
	docker compose down

migrate-up:
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down:
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

migrate-force:
	$(MIGRATE) -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" force $(version)

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

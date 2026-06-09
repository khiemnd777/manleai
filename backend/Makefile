.PHONY: run-api run-worker test tidy seed-local

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

test:
	go test ./...

tidy:
	go mod tidy

seed-local:
	psql "$$DATABASE_URL" -f seed/local.sql


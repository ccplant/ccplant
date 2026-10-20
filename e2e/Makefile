.PHONY: test run-api run-worker frontend-install frontend-build
test:
	go test ./...
run-api:
	go run ./cmd/api
run-worker:
	go run ./cmd/worker
frontend-install:
	cd frontend && bun install
frontend-build:
	cd frontend && bun run build

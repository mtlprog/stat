.PHONY: build test fmt vet lint clean up down logs generate

# Build
build:
	go build -o stat ./cmd/stat

# Run tests
test:
	go test ./... -v

# Format and vet
fmt:
	go fmt ./...

vet:
	go vet ./...

lint: fmt vet

# Clean build artifacts
clean:
	rm -f stat stat_bin

# Docker Compose
up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f app

# Trigger snapshot generation (requires running server)
generate:
	curl -s -X POST http://localhost:$${HTTP_PORT:-8080}/api/v1/snapshots/generate | jq .

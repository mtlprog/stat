.PHONY: build test fmt vet lint clean up down logs

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

.PHONY: build test fmt vet lint clean up down logs docs

# Build
build:
	go build -o stat ./cmd/stat

# Regenerate Swagger/OpenAPI spec from handler annotations.
docs:
	go run github.com/swaggo/swag/cmd/swag@latest init \
		-g cmd/stat/main.go \
		-o docs \
		--parseDependency \
		--parseInternal

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

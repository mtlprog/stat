.PHONY: build test fmt vet lint clean

# Build
build:
	go build -o stat ./...

# Run tests
test:
	go test ./... -v

# Format code
fmt:
	go fmt ./...

# Vet code
vet:
	go vet ./...

# Lint (format + vet)
lint: fmt vet

# Clean build artifacts
clean:
	rm -f stat

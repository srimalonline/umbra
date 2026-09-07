# umbra — a single static Go binary that is both the CLI and the MCP server.
BINARY  := umbra
PREFIX  ?= /usr/local
VERSION := 0.1.0

# CGO off => a fully static binary with zero runtime deps (no libc link).
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"

.PHONY: build test vet fmt install clean release

build: ## Build the umbra binary for this platform
	$(GOBUILD) -o $(BINARY) .

test: ## Run the test suite (mock executor + filestore, no Docker)
	go test ./...

vet: ## go vet
	go vet ./...

fmt: ## Format all Go sources
	gofmt -w .

install: build ## Build and install to $(PREFIX)/bin (needs write access)
	install -m 0755 $(BINARY) $(PREFIX)/bin/$(BINARY)

clean: ## Remove build artifacts
	rm -f $(BINARY) $(BINARY)-*-*

# Cross-compiled static binaries for the common server targets, for a GitHub release.
release: ## Build release binaries into ./ (linux/darwin, amd64/arm64)
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o $(BINARY)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o $(BINARY)-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o $(BINARY)-darwin-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o $(BINARY)-darwin-arm64 .

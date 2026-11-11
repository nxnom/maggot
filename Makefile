VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/nxnom/maggot/internal/cli.Version=$(VERSION)

.PHONY: build test vet
build:
	go build -ldflags "$(LDFLAGS)" -o bin/maggot ./cmd/maggot
test:
	go test ./...
vet:
	go vet ./...

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/nxnom/maggot/internal/cli.Version=$(VERSION)

GOBIN ?= $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: build install uninstall test vet
build:
	go build -ldflags "$(LDFLAGS)" -o bin/maggot ./cmd/maggot
# Install from this checkout (no GitHub download) into $(GOBIN).
install:
	GOBIN="$(GOBIN)" go install -ldflags "$(LDFLAGS)" ./cmd/maggot
	@echo "installed $(GOBIN)/maggot ($(VERSION))"
	@case ":$$PATH:" in *":$(GOBIN):"*) ;; *) echo "note: $(GOBIN) is not on PATH; add: export PATH=\"$(GOBIN):\$$PATH\"";; esac
uninstall:
	rm -f "$(GOBIN)/maggot"
test:
	go test ./...
vet:
	go vet ./...

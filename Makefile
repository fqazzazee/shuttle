BINARY  := shuttle
PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Pinned to match CI. `go run` builds it on first use, so no separate install.
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

.PHONY: build test lint fmt-check install uninstall smoke clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) ./cmd/shuttle

test:
	go test -race ./...

lint: fmt-check
	go vet ./...
	$(GOLANGCI_LINT) run

fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "not gofmt'd:"; echo "$$unformatted"; exit 1; fi

install: build
	install -Dm755 bin/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BINARY)

# Runs the installed copy from outside the source tree, so a binary that only
# works from a checkout fails here.
smoke:
	cd / && "$(DESTDIR)$(PREFIX)/bin/$(BINARY)" version
	cd / && "$(DESTDIR)$(PREFIX)/bin/$(BINARY)" help >/dev/null

clean:
	rm -rf bin/ dist/ coverage.out

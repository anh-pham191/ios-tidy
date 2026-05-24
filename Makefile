.PHONY: test test-device test-smoke test-cover lint build build-mcp clean

GO            ?= go
PKG            = ./...
BIN_DIR        = bin
BIN            = $(BIN_DIR)/ios-tidy
BIN_MCP        = $(BIN_DIR)/ios-tidy-mcp
GIT_DESCRIBE  := $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
LDFLAGS        = -s -w -X main.Version=$(GIT_DESCRIBE)

test:
	$(GO) test -race $(PKG)

test-device:
	@if [ -z "$$IOS_TIDY_TEST_UDID" ]; then \
	  echo "IOS_TIDY_TEST_UDID must be set for device tests"; exit 2; \
	fi
	$(GO) test -tags=device ./internal/iosbackend/...

test-smoke: build-mcp
	$(GO) test -tags=smoke ./cmd/ios-tidy-mcp/...

test-cover:
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -n 1

lint:
	$(GO) vet $(PKG)
	@if command -v staticcheck >/dev/null 2>&1; then \
	  staticcheck $(PKG); \
	else \
	  echo "staticcheck not installed — skipping (install: go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
	fi

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/ios-tidy

build-mcp:
	mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_MCP) ./cmd/ios-tidy-mcp

clean:
	rm -rf $(BIN_DIR) coverage.out

# ios-tidy build glue. Targets are deliberately small and obvious.
# Why a Makefile rather than only `go` commands: it codifies the
# integration-test gate (IOS_TIDY_TEST_UDID) so it can never be
# skipped accidentally.

.PHONY: test test-device lint build fmt

# Unit tests: stdlib testing only, no build tag.
test:
	go test ./...

# Device integration tests: require the //go:build device tag AND
# an explicitly-set UDID env var so they can never run against the
# wrong phone by mistake (see SHARED_CONTEXT.md §5).
test-device:
	@if [ -z "$$IOS_TIDY_TEST_UDID" ]; then \
		echo "IOS_TIDY_TEST_UDID is not set; refusing to run device tests."; \
		exit 1; \
	fi
	go test -tags=device -count=1 ./internal/iosbackend/...

lint:
	go vet ./...
	gofmt -l . | tee /tmp/ios-tidy-gofmt.out
	@test ! -s /tmp/ios-tidy-gofmt.out

build:
	go build -trimpath -ldflags="-s -w" -o bin/ios-tidy ./cmd/ios-tidy

fmt:
	gofmt -w .

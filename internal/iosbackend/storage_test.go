package iosbackend

import (
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/storage"
)

// Compile-time interface conformance assertion. Mirrors the M1 device adapter
// convention in device_test.go: any constructor-signature drift surfaces at
// `go test` time rather than at first user run.
func TestNewStorage_returnsStorageClientInterface(t *testing.T) {
	var _ storage.Client = NewStorage()
}

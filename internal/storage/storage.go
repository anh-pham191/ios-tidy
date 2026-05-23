// internal/storage/storage.go

// Package storage defines the seam interface for querying overall iOS device
// filesystem usage. The only production implementation lives in
// internal/iosbackend/storage.go. Tests in consuming packages use FakeClient
// from fake.go.
package storage

import "context"

// DeviceInfo mirrors the data AFC's deviceInfo opcode returns. The numbers are
// AFC-reported and may differ from iOS Settings by a few hundred MB — see
// RESEARCH.md §10 caveat 4. JSON tags are AFC-prefixed (afcTotalBytes etc.) so
// machine consumers can see at a glance that these are AFC numbers, not
// Settings numbers. If you reverse the naming choice, also update the decoder
// struct in cmd/ios-tidy/storage_test.go step 5a (Task 11) and the explanatory
// comment in this file's header.
type DeviceInfo struct {
	Model      string `json:"model"`
	TotalBytes uint64 `json:"afcTotalBytes"`
	FreeBytes  uint64 `json:"afcFreeBytes"`
	BlockSize  uint64 `json:"afcBlockSize"`
}

// Client returns filesystem-level information about a single connected device.
type Client interface {
	DeviceInfo(ctx context.Context, udid string) (DeviceInfo, error)
}

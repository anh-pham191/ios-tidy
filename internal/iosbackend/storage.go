// internal/iosbackend/storage.go
//
// Adapter for storage.Client. The only M2 file (besides apps.go) that imports
// go-ios's afc and ios packages.
//
// Verified signatures (WebFetch against go-ios main on 2026-05-24):
//   afc.New(d ios.DeviceEntry) (*afc.Client, error)
//   (c *afc.Client) DeviceInfo() (afc.DeviceInfo, error)
//   (c *afc.Client) Close() error  ← returns error; we log via defer + intentional drop
//   afc.DeviceInfo{Model string; TotalBytes uint64; FreeBytes uint64; BlockSize uint64}
//   ios.GetDevice(udid string) (ios.DeviceEntry, error)
//
// Sources:
//   https://raw.githubusercontent.com/danielpaulus/go-ios/main/ios/afc/client.go

package iosbackend

import (
	"context"
	"fmt"

	"github.com/anh-pham191/ios-tidy/internal/storage"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/afc"
)

// NewStorage returns a production storage.Client backed by go-ios's AFC client.
func NewStorage() storage.Client { return &storageClient{} }

type storageClient struct{}

func (s *storageClient) DeviceInfo(ctx context.Context, udid string) (storage.DeviceInfo, error) {
	// We honour ctx via pre-flight Err() checks before each blocking go-ios
	// call. go-ios does not expose the underlying socket on this code path, so
	// once afc.DeviceInfo() is running we cannot interrupt it mid-flight; the
	// pre-flight check is the best we can do without forking go-ios.
	if err := ctx.Err(); err != nil {
		return storage.DeviceInfo{}, err
	}
	entry, err := ios.GetDevice(udid)
	if err != nil {
		return storage.DeviceInfo{}, fmt.Errorf("get device %q: %w", udid, err)
	}
	client, err := afc.New(entry)
	if err != nil {
		return storage.DeviceInfo{}, fmt.Errorf("open afc on %q: %w", udid, err)
	}
	// Close returns an error; we intentionally drop it because the primary
	// operation's result is what the caller cares about, and a close-error
	// after a successful DeviceInfo is a connection-teardown warning at most.
	defer func() { _ = client.Close() }()

	if err := ctx.Err(); err != nil {
		return storage.DeviceInfo{}, err
	}
	info, err := client.DeviceInfo()
	if err != nil {
		return storage.DeviceInfo{}, fmt.Errorf("afc deviceInfo on %q: %w", udid, err)
	}
	return storage.DeviceInfo{
		Model:      info.Model,
		TotalBytes: info.TotalBytes,
		FreeBytes:  info.FreeBytes,
		BlockSize:  info.BlockSize,
	}, nil
}

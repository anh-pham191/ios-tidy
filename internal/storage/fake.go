// internal/storage/fake.go
package storage

import "context"

// FakeClient is a hand-written fake for unit-testing consumers of Client.
// It records every udid passed to DeviceInfo in Calls and returns either
// Info or Err. Exported so cross-package tests can reuse it (see §5 of
// SHARED_CONTEXT.md).
type FakeClient struct {
	Info  DeviceInfo
	Err   error
	Calls []string
}

func (f *FakeClient) DeviceInfo(_ context.Context, udid string) (DeviceInfo, error) {
	f.Calls = append(f.Calls, udid)
	if f.Err != nil {
		return DeviceInfo{}, f.Err
	}
	return f.Info, nil
}

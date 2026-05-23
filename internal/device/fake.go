package device

import "context"

// FakeLister is an exported test double that returns Devices (or Err) and
// counts the number of List calls. Reused by tests in cmd/ios-tidy.
type FakeLister struct {
	Devices []Device
	Err     error
	Calls   int
}

func (f *FakeLister) List(_ context.Context) ([]Device, error) {
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Devices, nil
}

// FakeTrustChecker is an exported test double that returns a per-UDID
// canned bool and records every UDID it was asked about. If a UDID is
// not in Trusts, the zero value (false) is returned — matching the
// "default-untrusted" stance of the real implementation.
type FakeTrustChecker struct {
	Trusts  map[string]bool
	Err     error
	Queried []string
}

func (f *FakeTrustChecker) Trusted(_ context.Context, udid string) (bool, error) {
	f.Queried = append(f.Queried, udid)
	if f.Err != nil {
		return false, f.Err
	}
	return f.Trusts[udid], nil
}

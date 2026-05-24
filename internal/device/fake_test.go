package device

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestFakeLister_returnsCannedDevices(t *testing.T) {
	want := []Device{
		{UDID: "AAAA", Name: "iPhone One", Model: "iPhone15,2", IOSVersion: "18.4"},
		{UDID: "BBBB", Name: "iPhone Two", Model: "iPhone14,5", IOSVersion: "17.6"},
	}
	f := &FakeLister{Devices: want}

	got, err := f.List(context.Background())
	if err != nil {
		t.Fatalf("List returned unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("List returned %d devices, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("device %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if f.Calls != 1 {
		t.Errorf("FakeLister.Calls = %d, want 1", f.Calls)
	}
}

func TestFakeLister_ListFnTakesPrecedenceOverCannedFields(t *testing.T) {
	want := []Device{{UDID: "ZZZ", Name: "dyn"}}
	f := &FakeLister{
		Devices: []Device{{UDID: "AAA"}}, // should be ignored
		Err:     errors.New("canned err — should be ignored"),
		ListFn: func(_ context.Context) ([]Device, error) {
			return want, nil
		},
	}
	got, err := f.List(context.Background())
	if err != nil {
		t.Fatalf("List err = %v, want nil (ListFn path)", err)
	}
	if len(got) != 1 || got[0].UDID != "ZZZ" {
		t.Fatalf("List = %+v, want %+v", got, want)
	}
	if f.Calls != 1 {
		t.Fatalf("Calls = %d, want 1", f.Calls)
	}
}

func TestFakeLister_returnsCannedError(t *testing.T) {
	sentinel := errors.New("usbmuxd unreachable")
	f := &FakeLister{Err: sentinel}

	got, err := f.List(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("List error = %v, want %v", err, sentinel)
	}
	if got != nil {
		t.Errorf("List devices = %v on error, want nil", got)
	}
	if f.Calls != 1 {
		t.Errorf("FakeLister.Calls = %d, want 1", f.Calls)
	}
}

func TestFakeTrustChecker_returnsPerUDIDAnswerAndRecordsCalls(t *testing.T) {
	f := &FakeTrustChecker{Trusts: map[string]bool{"AAAA": true, "BBBB": false}}
	ctx := context.Background()

	trustedA, err := f.Trusted(ctx, "AAAA")
	if err != nil {
		t.Fatalf("Trusted(AAAA) err = %v", err)
	}
	if !trustedA {
		t.Errorf("Trusted(AAAA) = false, want true")
	}

	trustedB, err := f.Trusted(ctx, "BBBB")
	if err != nil {
		t.Fatalf("Trusted(BBBB) err = %v", err)
	}
	if trustedB {
		t.Errorf("Trusted(BBBB) = true, want false")
	}

	// Unknown UDID defaults to false (the safe stance).
	trustedC, err := f.Trusted(ctx, "CCCC")
	if err != nil {
		t.Fatalf("Trusted(CCCC) err = %v", err)
	}
	if trustedC {
		t.Errorf("Trusted(CCCC) = true, want false (unknown UDID)")
	}

	if got, want := f.Queried, []string{"AAAA", "BBBB", "CCCC"}; !slices.Equal(got, want) {
		t.Errorf("Queried = %v, want %v", got, want)
	}
}

func TestFakeTrustChecker_returnsCannedError(t *testing.T) {
	sentinel := errors.New("lockdown unreachable")
	f := &FakeTrustChecker{Err: sentinel}

	trusted, err := f.Trusted(context.Background(), "AAAA")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Trusted error = %v, want %v", err, sentinel)
	}
	if trusted {
		t.Errorf("Trusted on error = true, want false")
	}
}

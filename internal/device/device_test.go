package device

import "testing"

func TestDevice_zeroValueHasEmptyStringFields(t *testing.T) {
	var d Device
	if d.UDID != "" || d.Name != "" || d.Model != "" || d.IOSVersion != "" {
		t.Fatalf("Device{} zero value should have empty string fields, got %+v", d)
	}
}

// internal/iosbackend/crashlogs_test.go
package iosbackend

import (
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
)

func TestNewCrashLogs_returnsCrashLogsClient(t *testing.T) {
	var c crashlogs.Client = NewCrashLogs()
	if c == nil {
		t.Fatal("NewCrashLogs() returned nil")
	}
}

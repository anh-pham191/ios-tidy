// internal/iosbackend/sandbox_test.go
//
// Host-side unit tests for the afc.FileInfo → sandbox.FileInfo mapping
// helper. The full AFC round-trip lives in sandbox_device_test.go (build
// tag `device`) — these tests only exercise the pure conversion.
//
// afc.FileInfo at the pinned go-ios SHA has fields {Name, Type, Mode,
// Size, LinkTarget} (no ModTime, no IsDir field — IsDir() is a method
// derived from Type == afc.S_IFDIR). The conversion therefore leaves
// sandbox.FileInfo.ModTime zero and derives IsDir via the method.
package iosbackend

import (
	"testing"

	"github.com/danielpaulus/go-ios/ios/afc"
)

func TestConvertFileInfo_copiesAllFields(t *testing.T) {
	in := afc.FileInfo{
		Name: "thing.cache",
		Size: 4096,
		Type: "", // non-dir
	}
	got := convertFileInfo("/Library/Caches/thing.cache", in)

	if got.Name != "thing.cache" {
		t.Errorf("Name = %q, want %q", got.Name, "thing.cache")
	}
	if got.Path != "/Library/Caches/thing.cache" {
		t.Errorf("Path = %q, want %q", got.Path, "/Library/Caches/thing.cache")
	}
	if got.Size != 4096 {
		t.Errorf("Size = %d, want %d", got.Size, 4096)
	}
	if got.IsDir {
		t.Errorf("IsDir = true, want false")
	}
	if !got.ModTime.IsZero() {
		t.Errorf("ModTime = %v, want zero (afc.FileInfo carries no ModTime at the pinned SHA)", got.ModTime)
	}
}

func TestConvertFileInfo_dirFlagPreserved(t *testing.T) {
	in := afc.FileInfo{Name: "Caches", Type: afc.S_IFDIR}
	got := convertFileInfo("/Library/Caches", in)
	if !got.IsDir {
		t.Errorf("IsDir = false, want true (Type=S_IFDIR)")
	}
	if got.Name != "Caches" {
		t.Errorf("Name = %q, want %q", got.Name, "Caches")
	}
	if got.Path != "/Library/Caches" {
		t.Errorf("Path = %q, want %q", got.Path, "/Library/Caches")
	}
}

func TestConvertFileInfo_fallsBackToPathBasenameWhenNameEmpty(t *testing.T) {
	in := afc.FileInfo{Size: 1, Type: afc.S_IFDIR}
	got := convertFileInfo("/Library/Caches", in)
	if got.Name != "Caches" {
		t.Errorf("Name = %q, want %q (basename of path)", got.Name, "Caches")
	}
}

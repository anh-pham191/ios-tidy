package sandbox

import (
	"context"
	"errors"
	"testing"
)

func TestTargetTmp_isTmp(t *testing.T) {
	if TargetTmp.Name != "tmp" {
		t.Errorf("TargetTmp.Name = %q, want %q", TargetTmp.Name, "tmp")
	}
	if TargetTmp.Root != "tmp" {
		t.Errorf("TargetTmp.Root = %q, want %q", TargetTmp.Root, "tmp")
	}
}

func TestTargetCaches_isLibraryCaches(t *testing.T) {
	if TargetCaches.Name != "Library/Caches" {
		t.Errorf("TargetCaches.Name = %q, want %q", TargetCaches.Name, "Library/Caches")
	}
	if TargetCaches.Root != "Library/Caches" {
		t.Errorf("TargetCaches.Root = %q, want %q", TargetCaches.Root, "Library/Caches")
	}
}

func TestTargetDocuments_isDocuments(t *testing.T) {
	if TargetDocuments.Name != "Documents" {
		t.Errorf("TargetDocuments.Name = %q, want %q", TargetDocuments.Name, "Documents")
	}
	if TargetDocuments.Root != "Documents" {
		t.Errorf("TargetDocuments.Root = %q, want %q", TargetDocuments.Root, "Documents")
	}
}

func TestBuildPlan_emptyTargetReturnsZeroPlan(t *testing.T) {
	fs := &FakeFS{} // no WalkResults entry → Walk delivers nothing
	plan, err := BuildPlan(context.Background(), fs, TargetTmp)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(plan.Files) != 0 {
		t.Errorf("Files len = %d, want 0", len(plan.Files))
	}
	if plan.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", plan.TotalBytes)
	}
	if plan.Target != TargetTmp {
		t.Errorf("Target = %+v, want %+v", plan.Target, TargetTmp)
	}
}

func TestBuildPlan_sumsNonDirFiles(t *testing.T) {
	fs := &FakeFS{
		WalkResults: map[string][]FileInfo{
			"tmp": {
				{Name: "tmp", Path: "tmp", IsDir: true},
				{Name: "a.tmp", Path: "tmp/a.tmp", Size: 100},
				{Name: "sub", Path: "tmp/sub", IsDir: true},
				{Name: "b.tmp", Path: "tmp/sub/b.tmp", Size: 250},
				{Name: "c.tmp", Path: "tmp/sub/c.tmp", Size: 50},
			},
		},
	}
	plan, err := BuildPlan(context.Background(), fs, TargetTmp)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if got, want := len(plan.Files), 3; got != want {
		t.Errorf("Files len = %d, want %d", got, want)
	}
	if got, want := plan.TotalBytes, int64(400); got != want {
		t.Errorf("TotalBytes = %d, want %d", got, want)
	}
}

func TestBuildPlan_propagatesWalkError(t *testing.T) {
	bang := errors.New("transport boom")
	fs := &FakeFS{WalkErr: bang}
	_, err := BuildPlan(context.Background(), fs, TargetCaches)
	if !errors.Is(err, bang) {
		t.Fatalf("BuildPlan err = %v, want %v", err, bang)
	}
}

func TestExecute_tmpRemovesAllAndCountsPlanFiles(t *testing.T) {
	fs := &FakeFS{}
	plan := CleanPlan{
		Target:     TargetTmp,
		Files:      []FileInfo{{Path: "tmp/a", Size: 10}, {Path: "tmp/b", Size: 20}},
		TotalBytes: 30,
	}
	res := Execute(context.Background(), fs, plan)
	if got, want := fs.RemoveAllCalls, []string{"tmp"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("RemoveAllCalls = %v, want %v", got, want)
	}
	if len(fs.RemoveCalls) != 0 {
		t.Errorf("Remove was called per-file but should not be: %v", fs.RemoveCalls)
	}
	if res.Removed != 2 || res.Bytes != 30 {
		t.Errorf("res = %+v, want Removed=2 Bytes=30", res)
	}
	if len(res.Failures) != 0 {
		t.Errorf("Failures = %+v, want none", res.Failures)
	}
}

func TestExecute_tmpRecordsFailureOnRemoveAllError(t *testing.T) {
	bang := errors.New("device disconnected")
	fs := &FakeFS{RemoveAllErr: bang}
	plan := CleanPlan{
		Target:     TargetCaches,
		Files:      []FileInfo{{Path: "Library/Caches/x", Size: 1}},
		TotalBytes: 1,
	}
	res := Execute(context.Background(), fs, plan)
	if res.Removed != 0 || res.Bytes != 0 {
		t.Errorf("res = %+v, want Removed=0 Bytes=0", res)
	}
	if len(res.Failures) != 1 {
		t.Fatalf("Failures len = %d, want 1", len(res.Failures))
	}
	if res.Failures[0].Path != "Library/Caches" {
		t.Errorf("Failures[0].Path = %q, want %q", res.Failures[0].Path, "Library/Caches")
	}
	if !errors.Is(res.Failures[0].Err, bang) {
		t.Errorf("Failures[0].Err = %v, want %v", res.Failures[0].Err, bang)
	}
}

func TestExecute_documentsPerFileRecordsEachFailure(t *testing.T) {
	bang := errors.New("permission denied")
	fs := &FakeFS{
		RemoveErrByPath: map[string]error{
			"Documents/b.txt": bang,
		},
	}
	plan := CleanPlan{
		Target: TargetDocuments,
		Files: []FileInfo{
			{Path: "Documents/a.txt", Size: 100},
			{Path: "Documents/b.txt", Size: 200},
			{Path: "Documents/c.txt", Size: 300},
		},
		TotalBytes: 600,
	}
	res := Execute(context.Background(), fs, plan)

	if got, want := fs.RemoveCalls, []string{"Documents/a.txt", "Documents/b.txt", "Documents/c.txt"}; !equalStrings(got, want) {
		t.Fatalf("RemoveCalls = %v, want %v", got, want)
	}
	if len(fs.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll was called for Documents but should not be: %v", fs.RemoveAllCalls)
	}
	if res.Removed != 2 {
		t.Errorf("Removed = %d, want 2", res.Removed)
	}
	if res.Bytes != 400 {
		t.Errorf("Bytes = %d, want 400", res.Bytes)
	}
	if len(res.Failures) != 1 || res.Failures[0].Path != "Documents/b.txt" {
		t.Errorf("Failures = %+v, want one failure for b.txt", res.Failures)
	}
}

func TestExecute_documentsAllSuccess(t *testing.T) {
	fs := &FakeFS{}
	plan := CleanPlan{
		Target:     TargetDocuments,
		Files:      []FileInfo{{Path: "Documents/x", Size: 5}},
		TotalBytes: 5,
	}
	res := Execute(context.Background(), fs, plan)
	if res.Removed != 1 || res.Bytes != 5 || len(res.Failures) != 0 {
		t.Errorf("res = %+v, want Removed=1 Bytes=5 no failures", res)
	}
}

func TestExecute_documentsAllFail(t *testing.T) {
	bang := errors.New("nope")
	fs := &FakeFS{RemoveErr: bang}
	plan := CleanPlan{
		Target:     TargetDocuments,
		Files:      []FileInfo{{Path: "Documents/x", Size: 5}, {Path: "Documents/y", Size: 6}},
		TotalBytes: 11,
	}
	res := Execute(context.Background(), fs, plan)
	if res.Removed != 0 || res.Bytes != 0 {
		t.Errorf("res counts = %+v, want zeroes", res)
	}
	if len(res.Failures) != 2 {
		t.Fatalf("Failures len = %d, want 2", len(res.Failures))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

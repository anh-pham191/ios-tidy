package sandbox

import "context"

// Target names a sandbox subtree we know how to clean. The Root is the
// path WITHIN the app container (so always POSIX, never including the
// container prefix — the FS is rooted at the container itself).
type Target struct {
	Name string
	Root string
}

// Built-in targets matching iOS app container layout. See Apple's
// "File System Basics" — every iOS app sees tmp/, Library/Caches/,
// and Documents/ as subdirectories of its container root.
var (
	TargetTmp       = Target{Name: "tmp", Root: "tmp"}
	TargetCaches    = Target{Name: "Library/Caches", Root: "Library/Caches"}
	TargetDocuments = Target{Name: "Documents", Root: "Documents"}
)

// Failure is one entry that did not delete cleanly.
// Mirrors crashlogs.Failure intentionally; defined locally to avoid a
// sideways package dependency.
type Failure struct {
	Path string
	Err  error
}

// CleanPlan describes what BuildPlan would delete for a single target.
type CleanPlan struct {
	Target     Target
	Files      []FileInfo // non-dir entries only
	TotalBytes int64
}

// CleanResult is what Execute returns for one target.
type CleanResult struct {
	Target   Target
	Removed  int
	Bytes    int64
	Failures []Failure
}

// BuildPlan walks fs from target.Root and counts non-dir files and their bytes.
// Walk errors during traversal are returned as the function's error; per-entry
// errors are surfaced via Failures on CleanResult during Execute, not here.
func BuildPlan(ctx context.Context, fs FS, target Target) (CleanPlan, error) {
	plan := CleanPlan{Target: target}
	err := fs.Walk(ctx, target.Root, func(info FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir {
			return nil
		}
		plan.Files = append(plan.Files, info)
		plan.TotalBytes += info.Size
		return nil
	})
	if err != nil {
		return CleanPlan{}, err
	}
	return plan, nil
}

// Execute deletes the planned files. For tmp and Library/Caches it walks one
// level into the target root, then issues a RemoveAll for each top-level
// child — the target directory node ITSELF is preserved because some iOS
// apps assume tmp/ and Library/Caches/ exist at launch and crash if the
// node is missing (tmp/ is recreated by iOS on reboot per Apple's File
// System Basics, but Caches/ regeneration is app-managed; we preserve both
// to be safe). For Documents — which holds user data — Execute deletes
// file-by-file so the caller can report which specific files failed.
func Execute(ctx context.Context, fs FS, plan CleanPlan) CleanResult {
	res := CleanResult{Target: plan.Target}
	if plan.Target == TargetDocuments {
		executePerFile(ctx, fs, plan, &res)
		return res
	}
	executeRemoveAll(ctx, fs, plan, &res)
	return res
}

// executeRemoveAll enumerates the immediate children of plan.Target.Root and
// RemoveAlls each one. The target node itself is never removed. Per-child
// RemoveAll failures are reported individually so partial-success cleanup
// is observable.
func executeRemoveAll(ctx context.Context, fs FS, plan CleanPlan, res *CleanResult) {
	children, err := fs.List(ctx, plan.Target.Root)
	if err != nil {
		// Can't list the target → report a single failure against the
		// target node so the caller knows nothing was attempted.
		res.Failures = append(res.Failures, Failure{Path: plan.Target.Root, Err: err})
		return
	}
	// Bucket the plan's file sizes by which top-level child they live
	// under, so the per-child Removed/Bytes accounting reflects the
	// children that successfully RemoveAll'd.
	bytesByChild := make(map[string]int64, len(children))
	filesByChild := make(map[string]int, len(children))
	for _, fi := range plan.Files {
		child := topLevelChild(plan.Target.Root, fi.Path)
		if child == "" {
			continue
		}
		bytesByChild[child] += fi.Size
		filesByChild[child]++
	}
	for _, c := range children {
		childPath := c.Path
		if childPath == "" {
			// Some FS implementations may return Name without Path;
			// reconstruct defensively. Production afcFS always sets Path,
			// but the FakeFS is permissive.
			childPath = plan.Target.Root + "/" + c.Name
		}
		if err := fs.RemoveAll(ctx, childPath); err != nil {
			res.Failures = append(res.Failures, Failure{Path: childPath, Err: err})
			continue
		}
		// On success, credit the planned bytes for files under this child.
		// For non-dir children (a single file at the top level), the bucket
		// equals the file's own size; for dir children the bucket sums
		// everything underneath that was walked.
		if c.IsDir {
			res.Removed += filesByChild[childPath]
			res.Bytes += bytesByChild[childPath]
		} else {
			// Top-level file: count it as one removal and credit its bytes
			// from the plan if we walked it.
			res.Removed += filesByChild[childPath]
			res.Bytes += bytesByChild[childPath]
		}
	}
}

// topLevelChild returns the path to the top-level child of root that
// contains entryPath, or "" if entryPath is not under root. For
// root="tmp", entryPath="tmp/sub/a.txt" returns "tmp/sub"; for
// entryPath="tmp/a.txt" returns "tmp/a.txt".
func topLevelChild(root, entryPath string) string {
	if !pathHasPrefix(entryPath, root+"/") {
		return ""
	}
	rest := entryPath[len(root)+1:]
	if i := indexByte(rest, '/'); i >= 0 {
		return root + "/" + rest[:i]
	}
	return root + "/" + rest
}

func pathHasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func executePerFile(ctx context.Context, fs FS, plan CleanPlan, res *CleanResult) {
	for _, fi := range plan.Files {
		if err := fs.Remove(ctx, fi.Path); err != nil {
			res.Failures = append(res.Failures, Failure{Path: fi.Path, Err: err})
			continue
		}
		res.Removed++
		res.Bytes += fi.Size
	}
}

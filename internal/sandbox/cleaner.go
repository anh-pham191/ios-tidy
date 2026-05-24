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

// Execute deletes the planned files. For tmp and Library/Caches it issues a
// single RemoveAll on the target root (cheap, atomic-from-the-CLI's-view).
// For Documents — which holds user data — it deletes file-by-file so the
// caller can report which specific files failed.
func Execute(ctx context.Context, fs FS, plan CleanPlan) CleanResult {
	res := CleanResult{Target: plan.Target}
	if plan.Target == TargetDocuments {
		executePerFile(ctx, fs, plan, &res)
		return res
	}
	executeRemoveAll(ctx, fs, plan, &res)
	return res
}

func executeRemoveAll(ctx context.Context, fs FS, plan CleanPlan, res *CleanResult) {
	if err := fs.RemoveAll(ctx, plan.Target.Root); err != nil {
		res.Failures = append(res.Failures, Failure{Path: plan.Target.Root, Err: err})
		return
	}
	// Trust the plan's accounting: the walk we did to build it is the closest
	// estimate of what's gone. If the device's view changed between Walk and
	// RemoveAll, the integration test will catch a mismatch, but the unit
	// path proceeds with the planned numbers.
	res.Removed = len(plan.Files)
	res.Bytes = plan.TotalBytes
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

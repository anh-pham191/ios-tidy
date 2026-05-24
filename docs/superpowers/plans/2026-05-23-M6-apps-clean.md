# M6: `apps clean` + README + Final Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `ios-tidy apps clean BUNDLE_ID` — a probe-gated, dry-run-first, multi-target destructive sandbox cleaner — and ship the project's README, Makefile polish, and a final acceptance walkthrough.

**Architecture:** A new `internal/sandbox/cleaner.go` builds per-target `CleanPlan`s (walk the FS, sum sizes) and executes them (RemoveAll for tmp/Caches; per-file Remove for Documents so failures are visible). The `apps clean` cobra/flag subcommand wires it together: resolve device → consult `apps.ProbeStore` (M5) → open `sandbox.Sandbox` (M5) → build plans → render via `ui.RenderPlan` (M4) → confirm via `ui.Prompter` (with an extra strict "type the bundle ID" gate for Documents) → execute. Dry-run short-circuits BEFORE any mutating FS call but AFTER walking (read-only) so the plan still shows real sizes. This honours SHARED_CONTEXT.md §3's seam interfaces — `internal/iosbackend/` remains the only package that imports go-ios.

**Tech Stack:** Go 1.23 stdlib + go-ios v1.0.213+ (only inside `internal/iosbackend/`); no test framework beyond `testing`.

**Depends on:** M1 (device.Lister/TrustChecker), M2 (storage Client for the device summary; not strictly required here but the App type from M2 is consumed indirectly), M3 (crashlogs.Failure type — we reuse it), M4 (ui.Prompter, ui.RenderPlan, dry-run pattern), M5 (apps.ProbeStore + apps.ProbeResult, sandbox.Sandbox + sandbox.FS).

---

## Revision history

### Cycle 2 — 2026-05-24
Addresses review at `docs/superpowers/reviews/2026-05-23-M6-review-1.md`.

**Findings addressed:**
- **[H1] Dry-run spy assertion is partial — Documents path under `--dry-run` not separately verified** — fixed in Task 11 by adding a new sub-flow (Steps 7–11): a dedicated TDD cycle that stages `WalkResults` for `Documents`, runs `apps clean BUNDLE_ID --include-documents --dry-run`, and asserts `len(fakeFS.RemoveCalls) == 0`, `len(fakeFS.RemoveAllCalls) == 0`, `len(fakePrompter.AskedLines) == 0` (the strict typed-bundle prompt MUST NOT be reached under dry-run), exit code 0, and that the plan output mentions the `Documents` target. Follows the same RED → verify-RED → GREEN (production-side: move the dry-run short-circuit so it short-circuits BEFORE the Documents strict-gate branch, if it isn't already) → verify-GREEN → commit-await-approval cadence as the rest of the matrix.
- **[H2] README missing example for explicit `--include-tmp --include-caches` combination** — fixed in Task 16 Step 1: the `apps clean` examples block now includes a `# Both file-cache targets explicit, Documents OFF — locks in the "explicit flags REPLACE defaults" rule.` example.
- **[M4] `FakeSandbox.OpenResults` key encoding (`"U1\x00com.example.app"`) used but not introduced by this plan** — fixed in Task 1 by adding Step 2b: explicitly read `internal/sandbox/fake.go`'s `FakeSandbox` to confirm the `Open(udid, bundleID)` key format. If M5 used a different encoding (e.g. a struct key, or `udid + "/" + bundleID`), the executing agent updates all `OpenResults: map[string]sandbox.FS{...}` literals in `apps_clean_test.go` to match. The verification step is explicit, not implicit.
- **[L2] Brittle assertion on the exact `ios-tidy apps probe --bundle com.example.app` hint string** — softened in Task 14 Step 1: the assertion is split into two `strings.Contains` checks (`"apps probe"` and `"stale"`) so a future copy-edit of the wording does not silently break the safety-hint test while still ensuring both the command-name pointer and the staleness explanation are present.

**Findings not addressed (with reasoning):**
- **[M1] Open Question #2 (case-sensitive) and #4 (explicit flags REPLACE defaults) — reviewer assent.** Reason: review §"Open questions raised by the plan author" states "Reviewer view: agree" for both. No plan change required; both resolutions stand. The combined-explicit-flag README example added under H2 doubles as the user-facing documentation of #4.
- **[M2] `ui.Prompter.ReadLine` introduced in M6.** Reason: review states "All four boxes ticked … purely a forward-design observation rather than a blocker". No change required.
- **[M3] `executeRemoveAll` accounts via plan totals.** Reason: review states "Not a finding to block on; flagged so the reviewer can confirm acceptance". The behaviour is accepted as documented.
- **[L1] Task 18 ("Coverage sweep") contains the single permitted `TBD`.** Reason: review states "Acceptable as written" — the `TBD` is bounded by a concrete procedure in Task 18 Step 2 and was explicitly justified in the plan's self-review §2.
- **[L3] Plan time estimates are aggressive.** Reason: review states "Not a blocker". The time table is illustrative; the per-step structure is what binds execution.

**Other improvements made while revising:**
- Tightened the dry-run short-circuit placement note in Task 11 Step 3 to make it explicit that the `if *dryRun { ... return 0 }` block MUST appear AFTER `RenderCleanPlan` and BEFORE the Documents-or-basic prompt branch — this is what makes the H1 fix correct without further production-code changes.

---

## Open questions

1. **Dry-run + Open reconciliation.** The milestone brief says "`--dry-run` MUST NOT call any mutating method on `FS`" — but to compute file counts and bytes for the plan output we MUST open the sandbox and Walk it. Walk and Stat are read-only on the AFC protocol. **Resolution adopted in this plan:** `--dry-run` performs `Sandbox.Open` + `FS.Walk` + `FS.Stat` (all read-only), then prints the plan and "Dry run — no changes made.", and exits 0. It never calls `Remove`/`RemoveAll`. The spy-fake test enforces this: `FakeFS` records `RemoveCalls` / `RemoveAllCalls` and the dry-run test asserts both are empty. This matches M4's pattern for `crashlogs clean --dry-run` (which lists entries but never calls `Remove`). If a reviewer objects, the alternative is a "static" dry-run that does not open the sandbox at all and just prints the targets it *would* enumerate; that would weaken the dry-run's usefulness (no byte total) so we reject it.

2. **Case-sensitivity of the typed-bundle-ID confirmation.** iOS bundle IDs are conventionally lowercase but Apple stores them case-preservingly. **Resolution adopted:** strict case-sensitive match against the exact `BUNDLE_ID` argument the user passed on the CLI. Rationale: the user typed the bundle ID themselves to invoke the command, so they know its case; any lenient comparison opens an avenue for autocomplete-style typos to slip through the safety gate. Test coverage includes a case-mismatch test that confirms abort.

3. **`Failure` type re-use vs duplication.** `crashlogs.Failure{Path, Err}` (M3) is structurally identical to what we need. **Resolution adopted:** define a fresh `sandbox.Failure` in `internal/sandbox/cleaner.go` rather than importing `crashlogs`. Reason: the `sandbox` package must not depend on `crashlogs` (they are peer packages of equal level; a sideways dependency adds coupling for no payoff and would confuse readers). The duplication is two lines.

4. **`--include-documents` solo behaviour.** If the user passes `--include-documents` and no other `--include-*` flag, do they want Documents-only, or Documents-plus-defaults? **Resolution adopted (per milestone brief's hint):** explicit-include flags REPLACE the defaults. Passing any of `--include-tmp`, `--include-caches`, `--include-documents` switches off the "tmp + Caches" default; only the flags the user explicitly named are included. So `--include-documents` alone targets only Documents. `--include-documents --include-tmp` targets Documents + tmp (no Caches). Documented in the help text and tested.

5. **Integration test `Push` availability.** The integration test wants to write a sentinel file via `Push`, run cleanup, and verify it's gone. `Push` is not in the `sandbox.FS` interface (SHARED_CONTEXT.md §3). **Resolution adopted:** the integration test calls the underlying go-ios `*afc.Client.Push` directly via a tiny helper in `internal/iosbackend/sandbox_clean_device_test.go` (test code is allowed to reach into the adapter package). We do NOT extend the `sandbox.FS` interface — Push has no production use case in M6.

---

## File map

**Create:**
- `internal/sandbox/cleaner.go` — `Target`, `CleanPlan`, `CleanResult`, `Failure`, `BuildPlan`, `Execute`.
- `internal/sandbox/cleaner_test.go` — unit tests against `FakeFS`.
- `internal/iosbackend/sandbox_clean_device_test.go` — `//go:build device` end-to-end test.
- `README.md` (project root) — user-facing docs.
- `docs/acceptance-walkthrough.md` — log of the M6 acceptance walkthrough (template only; the user fills entries as they walk it).

**Modify:**
- `internal/sandbox/sandbox.go` — no new types; pre-existing interface only. (Verify present.)
- `internal/sandbox/fake.go` — extend `FakeFS` to record `RemoveCalls`, `RemoveAllCalls`, `WalkResults`, `StatResults`, `ListResults` and spy fields, if not already covered by M5.
- `internal/iosbackend/sandbox.go` — flesh out destructive `FS` methods (Stat, List, Walk, Remove, RemoveAll) if M5 only stubbed them; map to go-ios `*afc.Client` per SHARED_CONTEXT.md §3 + RESEARCH.md §2.
- `cmd/ios-tidy/apps.go` — register `clean` subcommand alongside the M5 `list` and `probe`.
- `cmd/ios-tidy/apps_test.go` (or `apps_clean_test.go` if scope grows) — `apps clean` flow tests with fakes.
- `Makefile` — add/refine `test`, `test-device`, `test-cover`, `lint`, `build` targets.

---

## Task list overview

| # | Task | Approx. time |
|---|---|---|
| 1 | Verify M5 state: confirm sandbox.FS fakes and iosbackend stubs | 5 min |
| 2 | Extend `FakeFS` with spy fields for destructive methods | 10 min |
| 3 | Flesh out `internal/iosbackend/sandbox.go` destructive methods | 15 min |
| 4 | `internal/sandbox/cleaner.go` — Target constants + types | 10 min |
| 5 | `internal/sandbox/cleaner.go` — BuildPlan (TDD) | 25 min |
| 6 | `internal/sandbox/cleaner.go` — Execute for tmp/Caches (RemoveAll) | 20 min |
| 7 | `internal/sandbox/cleaner.go` — Execute for Documents (per-file Remove) | 20 min |
| 8 | `cmd/ios-tidy/apps.go` — register `clean` subcommand skeleton | 10 min |
| 9 | `cmd/ios-tidy/apps.go` — probe gate (refuses without Vended probe) | 15 min |
| 10 | `cmd/ios-tidy/apps.go` — open sandbox + build plans | 15 min |
| 11 | `cmd/ios-tidy/apps.go` — dry-run short-circuit | 15 min |
| 12 | `cmd/ios-tidy/apps.go` — basic y/N confirmation | 15 min |
| 13 | `cmd/ios-tidy/apps.go` — strict typed-bundle-ID gate for Documents | 20 min |
| 14 | `cmd/ios-tidy/apps.go` — Execute + summary + partial-failure exit | 15 min |
| 15 | `//go:build device` integration test for `apps clean` | 15 min |
| 16 | Write README.md | 20 min |
| 17 | Polish Makefile (lint, build, test-cover) | 15 min |
| 18 | Coverage sweep — add tests where genuine gaps exist | 20 min |
| 19 | Acceptance walkthrough template + run | 30 min |

Total ≈ 5 hours of agent work plus a 30 min on-device walkthrough by the user.

---

## Task 1: Verify M5 state

**Files:**
- Inspect: `internal/sandbox/fake.go`
- Inspect: `internal/iosbackend/sandbox.go`
- Test (read-only): `go test ./...`

- [ ] **Step 1: List current state of sandbox + iosbackend packages**

Run: `ls internal/sandbox/ internal/iosbackend/`
Expected: `sandbox.go fake.go cleaner.go? cleaner_test.go? sandbox_test.go` in sandbox; `sandbox.go device.go storage.go apps.go crashlogs.go doc.go` in iosbackend.

- [ ] **Step 2: Read `internal/sandbox/fake.go` to confirm what M5 added**

Read the file. Confirm which of these fields/methods already exist on `FakeFS`: `OpenCalls`, `CloseCalls`, `ListResults`, `StatResults`, `WalkResults`, `RemoveCalls`, `RemoveAllCalls`. M5's plan only required `Open` + `Close` to be exercised, so the destructive spy fields may be stubbed or absent.

- [ ] **Step 2b: Confirm `FakeSandbox.OpenResults` key encoding**

Read `internal/sandbox/fake.go`'s `FakeSandbox` type. This plan's later tests assume `OpenResults` is `map[string]sandbox.FS` keyed by `udid + "\x00" + bundleID` — but the encoding scheme was M5's territory and may differ (struct key, `udid + "/" + bundleID`, separate `OpenResultsByUDIDBundle` map, etc.). Capture the actual encoding in a one-line note.

Then, when later tasks (10, 11, 12, 13, 14) instantiate `&sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}`, substitute the real encoding. Expected (likely): `"U1\x00com.example.app"`. If different, every test in this plan that constructs a `FakeSandbox` must be updated consistently. The plan's test bodies otherwise remain unchanged.

- [ ] **Step 3: Read `internal/iosbackend/sandbox.go`**

Confirm which of `Stat`, `List`, `Walk`, `Remove`, `RemoveAll` are real vs returning `errors.New("not implemented")`.

- [ ] **Step 4: Run the existing test suite to capture the green baseline**

Run: `go test ./...`
Expected: `ok` for every package. If anything is red, STOP and report — M6 builds on green M5.

- [ ] **Step 5: No commit** — this task is read-only reconnaissance.

---

## Task 2: Extend `FakeFS` spy fields

**Files:**
- Modify: `internal/sandbox/fake.go`
- Test: `internal/sandbox/fake_test.go` (create if absent; M5 may or may not have one)

- [ ] **Step 1: Write the failing test — FakeFS records RemoveAll calls**

```go
// internal/sandbox/fake_test.go
package sandbox

import (
	"context"
	"testing"
)

func TestFakeFS_RemoveAll_recordsTheCall(t *testing.T) {
	f := &FakeFS{}
	if err := f.RemoveAll(context.Background(), "tmp"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if got, want := len(f.RemoveAllCalls), 1; got != want {
		t.Fatalf("RemoveAllCalls len = %d, want %d", got, want)
	}
	if f.RemoveAllCalls[0] != "tmp" {
		t.Fatalf("RemoveAllCalls[0] = %q, want %q", f.RemoveAllCalls[0], "tmp")
	}
}

func TestFakeFS_Remove_recordsTheCall(t *testing.T) {
	f := &FakeFS{}
	if err := f.Remove(context.Background(), "Documents/a.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got, want := len(f.RemoveCalls), 1; got != want {
		t.Fatalf("RemoveCalls len = %d, want %d", got, want)
	}
}

func TestFakeFS_RemoveAll_returnsCannedError(t *testing.T) {
	wantErr := errFake("nope")
	f := &FakeFS{RemoveAllErr: wantErr}
	err := f.RemoveAll(context.Background(), "tmp")
	if err == nil || err.Error() != "nope" {
		t.Fatalf("RemoveAll err = %v, want %v", err, wantErr)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/sandbox/... -run 'TestFakeFS_(Remove|RemoveAll)' -v`
Expected: FAIL — `FakeFS.RemoveAllCalls undefined`, `FakeFS.RemoveAllErr undefined`, etc.

- [ ] **Step 3: Extend `FakeFS` in `internal/sandbox/fake.go`**

Add to the existing `FakeFS` struct (do not delete fields M5 already wrote — additive only):

```go
// FakeFS is an in-memory FS for unit-testing consumers of FS.
// Spy fields record every call; *Err / *Result fields let tests
// drive return values. Zero-value FakeFS is usable: empty results,
// nil errors, all calls succeed.
type FakeFS struct {
	// existing M5 fields (Open/Close spies, etc.) preserved here

	// Spy slices: each method appends the path argument on call.
	RemoveCalls    []string
	RemoveAllCalls []string
	StatCalls      []string
	ListCalls      []string
	WalkRoots      []string

	// Canned return values. Map keys are the path argument.
	StatResults map[string]FileInfo
	ListResults map[string][]FileInfo
	// WalkResults: for each root, the sequence of FileInfos to deliver
	// to the WalkFunc (in order). A nil/missing entry means "deliver nothing".
	WalkResults map[string][]FileInfo

	// Canned errors. Non-nil → method returns this error and does NOT
	// append to its spy slice (so test assertions distinguish "called
	// then failed" — for that, use a per-call error map if needed later).
	// Simpler model for now: error is appended-AND-returned, so call count
	// reflects intent, not success. Document in tests.
	RemoveErr    error
	RemoveAllErr error
	StatErr      error
	ListErr      error
	WalkErr      error

	CloseErr error
	closed   bool
}

func (f *FakeFS) Remove(_ context.Context, path string) error {
	f.RemoveCalls = append(f.RemoveCalls, path)
	return f.RemoveErr
}

func (f *FakeFS) RemoveAll(_ context.Context, path string) error {
	f.RemoveAllCalls = append(f.RemoveAllCalls, path)
	return f.RemoveAllErr
}

func (f *FakeFS) Stat(_ context.Context, path string) (FileInfo, error) {
	f.StatCalls = append(f.StatCalls, path)
	if f.StatErr != nil {
		return FileInfo{}, f.StatErr
	}
	if fi, ok := f.StatResults[path]; ok {
		return fi, nil
	}
	return FileInfo{}, nil
}

func (f *FakeFS) List(_ context.Context, path string) ([]FileInfo, error) {
	f.ListCalls = append(f.ListCalls, path)
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.ListResults[path], nil
}

func (f *FakeFS) Walk(_ context.Context, root string, fn WalkFunc) error {
	f.WalkRoots = append(f.WalkRoots, root)
	if f.WalkErr != nil {
		return f.WalkErr
	}
	for _, fi := range f.WalkResults[root] {
		if err := fn(fi, nil); err != nil {
			return err
		}
	}
	return nil
}

func (f *FakeFS) Close() error {
	f.closed = true
	return f.CloseErr
}

// Closed reports whether Close has been called at least once.
func (f *FakeFS) Closed() bool { return f.closed }
```

(If M5 already wrote some of these methods, leave them in place — only add what's missing. Reconcile by reading what's there first.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/sandbox/... -run 'TestFakeFS_' -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Run the full suite — nothing else may regress**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 6: Commit (await user approval)**

```bash
git add internal/sandbox/fake.go internal/sandbox/fake_test.go
# Wait for explicit user approval before running:
git commit -m "test: extend FakeFS with spy fields for destructive methods"
```

---

## Task 3: Flesh out destructive methods in `internal/iosbackend/sandbox.go`

These are integration-test territory but we lean on `//go:build !device` unit tests where the adapter is a thin wrapper (we test only the FileInfo conversion, not the AFC round-trip).

**Files:**
- Modify: `internal/iosbackend/sandbox.go`
- Test: `internal/iosbackend/sandbox_test.go` (create unit tests for conversion helpers only; round-trip is the device test).

- [ ] **Step 1: Write the failing test for the FileInfo conversion helper**

```go
// internal/iosbackend/sandbox_test.go
package iosbackend

import (
	"testing"
	"time"

	"github.com/danielpaulus/go-ios/ios/afc"
)

func TestConvertFileInfo_copiesAllFields(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	in := afc.FileInfo{
		Name:    "thing.cache",
		Size:    4096,
		IsDir:   false,
		ModTime: now,
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
	if got.IsDir != false {
		t.Errorf("IsDir = %v, want false", got.IsDir)
	}
	if !got.ModTime.Equal(now) {
		t.Errorf("ModTime = %v, want %v", got.ModTime, now)
	}
}

func TestConvertFileInfo_dirFlagPreserved(t *testing.T) {
	in := afc.FileInfo{Name: "Caches", IsDir: true}
	got := convertFileInfo("/Library/Caches", in)
	if !got.IsDir {
		t.Errorf("IsDir = false, want true")
	}
}
```

(Note: if `afc.FileInfo` field names differ in the real go-ios package at the pinned SHA, adjust the literal accordingly when implementing. Per SHARED_CONTEXT.md §3 + RESEARCH.md §2, the relevant fields are present; precise field names may be `Stmode`, `Stsize`, etc. — verify against `vendor/github.com/danielpaulus/go-ios/ios/afc/` after a `go mod download` if needed and update the test to match the real struct. If the real struct exposes a method like `Mode().IsDir()` rather than a bare `IsDir bool`, the test and the helper change accordingly. **This is the one spot in the plan where a small adapter signature is unverified; the test will catch any mismatch immediately.**)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/iosbackend/... -run TestConvertFileInfo -v`
Expected: FAIL — `convertFileInfo undefined`.

- [ ] **Step 3: Implement `convertFileInfo` + flesh out destructive `FS` methods**

In `internal/iosbackend/sandbox.go` (extend the file from M5 — do NOT rewrite the constructor or `Open` method):

```go
// convertFileInfo maps a go-ios afc.FileInfo into our sandbox.FileInfo,
// recording the path the caller provided (afc does not put the absolute path
// on its FileInfo — it gives only the basename).
func convertFileInfo(path string, in afc.FileInfo) sandbox.FileInfo {
	return sandbox.FileInfo{
		Name:    in.Name,
		Path:    path,
		Size:    in.Size,
		IsDir:   in.IsDir,
		ModTime: in.ModTime,
	}
}

// fsImpl is the production FS for a single opened app container.
type fsImpl struct {
	c *afc.Client
}

func (f *fsImpl) List(ctx context.Context, path string) ([]sandbox.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := f.c.List(path)
	if err != nil {
		return nil, fmt.Errorf("afc list %q: %w", path, err)
	}
	out := make([]sandbox.FileInfo, 0, len(names))
	for _, n := range names {
		full := joinPath(path, n)
		afi, err := f.c.Stat(full)
		if err != nil {
			// Skip unreadable entries with a synthetic dir-like entry so callers see them.
			out = append(out, sandbox.FileInfo{Name: n, Path: full})
			continue
		}
		out = append(out, convertFileInfo(full, afi))
	}
	return out, nil
}

func (f *fsImpl) Stat(ctx context.Context, path string) (sandbox.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.FileInfo{}, err
	}
	afi, err := f.c.Stat(path)
	if err != nil {
		return sandbox.FileInfo{}, fmt.Errorf("afc stat %q: %w", path, err)
	}
	return convertFileInfo(path, afi), nil
}

func (f *fsImpl) Walk(ctx context.Context, root string, fn sandbox.WalkFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.c.WalkDir(root, func(p string, afi afc.FileInfo, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return fn(sandbox.FileInfo{Path: p}, err)
		}
		return fn(convertFileInfo(p, afi), nil)
	})
}

func (f *fsImpl) Remove(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.c.Remove(path); err != nil {
		return fmt.Errorf("afc remove %q: %w", path, err)
	}
	return nil
}

func (f *fsImpl) RemoveAll(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.c.RemoveAll(path); err != nil {
		return fmt.Errorf("afc removeAll %q: %w", path, err)
	}
	return nil
}

func (f *fsImpl) Close() error {
	if f.c == nil {
		return nil
	}
	return f.c.Close() // if afc.Client exposes Close; otherwise return nil — M5 should have established this.
}

// joinPath joins two sandbox-relative path segments with a forward slash.
// We do not use filepath.Join because the device side is POSIX regardless of host OS.
func joinPath(a, b string) string {
	if a == "" || a == "/" {
		if b == "" {
			return "/"
		}
		if b[0] == '/' {
			return b
		}
		return "/" + b
	}
	if b == "" {
		return a
	}
	if a[len(a)-1] == '/' {
		if b[0] == '/' {
			return a + b[1:]
		}
		return a + b
	}
	if b[0] == '/' {
		return a + b
	}
	return a + "/" + b
}
```

(M5 should have already declared `fsImpl` and its `Open` flow. We are filling out previously stubbed methods. If `Walk`'s callback signature in go-ios doesn't match `afc.WalkFunc(string, afc.FileInfo, error) error`, adjust — but per SHARED_CONTEXT.md §3 + RESEARCH.md §2's pasted-from-source signatures, it is `WalkDir(p string, f WalkFunc) error` with `WalkFunc` taking `(path, FileInfo, error)`.)

- [ ] **Step 4: Run the unit tests**

Run: `go test ./internal/iosbackend/... -run TestConvertFileInfo -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 6: Commit (await user approval)**

```bash
git add internal/iosbackend/sandbox.go internal/iosbackend/sandbox_test.go
# Wait for explicit user approval before running:
git commit -m "feat: flesh out destructive FS methods in iosbackend adapter"
```

---

## Task 4: `internal/sandbox/cleaner.go` — types and target constants

**Files:**
- Create: `internal/sandbox/cleaner.go`
- Test: `internal/sandbox/cleaner_test.go`

- [ ] **Step 1: Write the failing test for the built-in targets**

```go
// internal/sandbox/cleaner_test.go
package sandbox

import "testing"

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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/sandbox/... -run 'TestTarget' -v`
Expected: FAIL — `undefined: TargetTmp`, etc.

- [ ] **Step 3: Create `cleaner.go` with types + constants**

```go
// internal/sandbox/cleaner.go
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
// sideways package dependency (see plan §"Open questions" #3).
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
```

- [ ] **Step 4: Run to verify the target tests pass**

Run: `go test ./internal/sandbox/... -run 'TestTarget' -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 6: Commit (await user approval)**

```bash
git add internal/sandbox/cleaner.go internal/sandbox/cleaner_test.go
# Wait for explicit user approval before running:
git commit -m "feat: add Target constants and CleanPlan/CleanResult types"
```

---

## Task 5: `BuildPlan` behaviour — empty, nested, error

**Files:**
- Modify: `internal/sandbox/cleaner_test.go`
- Already defined: `BuildPlan` in `internal/sandbox/cleaner.go` from Task 4.

We now add behavioural tests for `BuildPlan`. (`BuildPlan` is already implemented in Task 4 because it's small enough that splitting Task-4-implementation from Task-5-test would require leaving an undefined symbol mid-plan. The tests here exercise the behaviour exhaustively.)

- [ ] **Step 1: Write the empty-target test**

Append to `internal/sandbox/cleaner_test.go`:

```go
import (
	"context"
	"errors"
	"testing"
)

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
```

- [ ] **Step 2: Run — expect PASS** (BuildPlan was already implemented)

Run: `go test ./internal/sandbox/... -run TestBuildPlan_emptyTargetReturnsZeroPlan -v`
Expected: PASS.

- [ ] **Step 3: Write the nested-files test**

```go
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
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/sandbox/... -run TestBuildPlan_sumsNonDirFiles -v`
Expected: PASS.

- [ ] **Step 5: Write the walk-error test**

```go
func TestBuildPlan_propagatesWalkError(t *testing.T) {
	bang := errors.New("transport boom")
	fs := &FakeFS{WalkErr: bang}
	_, err := BuildPlan(context.Background(), fs, TargetCaches)
	if !errors.Is(err, bang) {
		t.Fatalf("BuildPlan err = %v, want %v", err, bang)
	}
}
```

- [ ] **Step 6: Run — expect PASS**

Run: `go test ./internal/sandbox/... -run TestBuildPlan_propagatesWalkError -v`
Expected: PASS.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 8: Commit (await user approval)**

```bash
git add internal/sandbox/cleaner_test.go
# Wait for explicit user approval before running:
git commit -m "test: cover BuildPlan empty, nested, and walk-error cases"
```

---

## Task 6: `Execute` for tmp/Caches — RemoveAll success and failure

**Files:**
- Modify: `internal/sandbox/cleaner_test.go`
- Already implemented: `Execute` from Task 4.

- [ ] **Step 1: Write the success test for tmp**

```go
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
```

- [ ] **Step 2: Run — expect PASS**

Run: `go test ./internal/sandbox/... -run TestExecute_tmpRemovesAllAndCountsPlanFiles -v`
Expected: PASS.

- [ ] **Step 3: Write the RemoveAll failure test**

```go
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
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/sandbox/... -run TestExecute_tmpRecordsFailureOnRemoveAllError -v`
Expected: PASS.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add internal/sandbox/cleaner_test.go
# Wait for explicit user approval before running:
git commit -m "test: cover Execute for tmp/Caches RemoveAll success and failure"
```

---

## Task 7: `Execute` for Documents — per-file Remove with partial failure

**Files:**
- Modify: `internal/sandbox/cleaner_test.go`

The current `Execute` from Task 4 only records one failure per file if `Remove` returns an error, using the same `RemoveErr` field for all calls. For the partial-failure test we need a fake whose `Remove` succeeds for some paths and fails for others. We extend `FakeFS` minimally to support a per-path remove-error map. This is a fake-only extension — production code is untouched.

- [ ] **Step 1: Write the failing per-path-error test**

```go
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
```

- [ ] **Step 2: Run — expect FAIL** (`RemoveErrByPath undefined`)

Run: `go test ./internal/sandbox/... -run TestExecute_documentsPerFileRecordsEachFailure -v`
Expected: FAIL — `unknown field RemoveErrByPath in struct literal`.

- [ ] **Step 3: Extend `FakeFS.Remove` to honour a per-path error map**

In `internal/sandbox/fake.go`, add a field and tweak `Remove`:

```go
type FakeFS struct {
	// ...existing fields...
	RemoveErrByPath map[string]error // if non-nil and a key matches, that error is returned
}

func (f *FakeFS) Remove(_ context.Context, path string) error {
	f.RemoveCalls = append(f.RemoveCalls, path)
	if perPath, ok := f.RemoveErrByPath[path]; ok {
		return perPath
	}
	return f.RemoveErr
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/sandbox/... -run TestExecute_documentsPerFileRecordsEachFailure -v`
Expected: PASS.

- [ ] **Step 5: Add the all-success and all-failure variants**

```go
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
```

- [ ] **Step 6: Run — expect PASS for both**

Run: `go test ./internal/sandbox/... -run 'TestExecute_documents' -v`
Expected: PASS.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 8: Commit (await user approval)**

```bash
git add internal/sandbox/cleaner_test.go internal/sandbox/fake.go
# Wait for explicit user approval before running:
git commit -m "test: cover per-file Documents Execute with partial failure"
```

---

## Task 8: Register `clean` subcommand skeleton in `cmd/ios-tidy/apps.go`

We assume M5 already created `cmd/ios-tidy/apps.go` with `list` and `probe` subcommands and a dispatch on `os.Args[2]`. We extend that dispatch.

**Files:**
- Modify: `cmd/ios-tidy/apps.go`
- Test: `cmd/ios-tidy/apps_clean_test.go` (new file dedicated to clean tests; keeps `apps_test.go` from M5 untouched per agent-safety/SKILL.md "Tests Are Specifications")

- [ ] **Step 1: Write the failing test for "unknown subcommand routes to clean"**

```go
// cmd/ios-tidy/apps_clean_test.go
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAppsClean_missingBundleIDPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), runDeps{Stdout: &stdout, Stderr: &stderr}, []string{})
	if exit == 0 {
		t.Errorf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "usage: ios-tidy apps clean BUNDLE_ID") {
		t.Errorf("stderr = %q, want usage line", stderr.String())
	}
}
```

(`runDeps` is the dependency-injection struct that M1–M5 plans should have established for CLI tests. If it does not exist under this name, substitute the project's actual struct — likely with fields for stdout, stderr, prompter, devices lister, sandbox, probe-store, etc. The test exists to drive the existence of `runAppsClean`; the exact wiring is settled in the next steps.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_missingBundleIDPrintsUsage -v`
Expected: FAIL — `undefined: runAppsClean`.

- [ ] **Step 3: Define `runAppsClean` skeleton**

In `cmd/ios-tidy/apps.go`, add:

```go
// runAppsClean dispatches the `apps clean` subcommand.
// args is the slice AFTER "apps clean" — i.e. [BUNDLE_ID, flags...].
func runAppsClean(ctx context.Context, deps runDeps, args []string) int {
	fs := flag.NewFlagSet("apps clean", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	var (
		device       = fs.String("device", "", "UDID of device to target")
		dryRun       = fs.Bool("dry-run", false, "Show what would be deleted; do not delete")
		yes          = fs.Bool("yes", false, "Skip the basic y/N prompt (does NOT bypass the Documents typed-bundle-ID gate)")
		includeDocs  = fs.Bool("include-documents", false, "Include the app's Documents/ folder (user data — requires typed-bundle-ID confirmation)")
		includeTmp   = fs.Bool("include-tmp", false, "Include the app's tmp/ folder")
		includeCache = fs.Bool("include-caches", false, "Include the app's Library/Caches/ folder")
		storeDir     = fs.String("store-dir", "", "Override probe-store directory (hidden — for tests)")
	)
	_ = storeDir // wired in Task 9

	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 1 {
		fmt.Fprintln(deps.Stderr, "usage: ios-tidy apps clean BUNDLE_ID [flags]")
		return 2
	}
	bundleID := rest[0]

	// Apply include-flag default: if NONE set, use tmp+caches.
	if !*includeTmp && !*includeCache && !*includeDocs {
		*includeTmp = true
		*includeCache = true
	}

	// Wiring filled in by later tasks. For now, return non-zero so the
	// "missing BUNDLE_ID" test still differentiates from a real run.
	_ = device
	_ = dryRun
	_ = yes
	_ = bundleID
	return 1
}
```

Also: in the existing `runApps` (or whatever M5 named it) dispatcher, add a `case "clean": return runAppsClean(ctx, deps, args[1:])` branch alongside `list` and `probe`.

- [ ] **Step 4: Run — expect PASS for the usage test**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_missingBundleIDPrintsUsage -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 6: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps.go cmd/ios-tidy/apps_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: register apps clean subcommand skeleton"
```

---

## Task 9: Probe gate — refuse without a Vended probe result

**Files:**
- Modify: `cmd/ios-tidy/apps.go`
- Modify: `cmd/ios-tidy/apps_clean_test.go`

- [ ] **Step 1: Write the failing test — no probe at all**

```go
func TestAppsClean_refusesWhenNoProbeResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	store := &apps.FakeProbeStore{} // returns empty results from Load
	deps := runDeps{
		Stdout:    &stdout,
		Stderr:    &stderr,
		Devices:   &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
	}
	exit := runAppsClean(context.Background(), deps, []string{"com.example.app", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "ios-tidy apps probe --bundle com.example.app") {
		t.Errorf("stderr should hint at probe; got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "not been confirmed as vended") {
		t.Errorf("stderr should explain refusal; got: %q", stderr.String())
	}
}
```

- [ ] **Step 2: Write the failing test — probe present but not Vended**

```go
func TestAppsClean_refusesWhenProbeIsRefused(t *testing.T) {
	var stderr bytes.Buffer
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeRefused, Detail: "daemon refused"}},
		},
	}
	deps := runDeps{
		Stdout:    new(bytes.Buffer),
		Stderr:    &stderr,
		Devices:   &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
	}
	exit := runAppsClean(context.Background(), deps, []string{"com.example.app", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "not been confirmed as vended") {
		t.Errorf("stderr = %q, want refusal explanation", stderr.String())
	}
}
```

- [ ] **Step 3: Run to verify failures**

Run: `go test ./cmd/ios-tidy/... -run 'TestAppsClean_refuses' -v`
Expected: FAIL — `runAppsClean` currently returns 1 without printing the refusal text.

- [ ] **Step 4: Implement the probe gate**

In `runAppsClean` (replace the trailing `return 1` block), insert after `bundleID := rest[0]`:

```go
udid, err := resolveDevice(ctx, deps.Devices, *device)
if err != nil {
	fmt.Fprintln(deps.Stderr, "error:", err)
	return 1
}

results, err := deps.ProbeStore.Load(udid)
if err != nil {
	fmt.Fprintln(deps.Stderr, "error: load probe store:", err)
	return 1
}
if !probeVended(results, bundleID) {
	fmt.Fprintf(deps.Stderr,
		"error: bundle %q has not been confirmed as vended on device %s.\n"+
			"Run `ios-tidy apps probe --bundle %s` first to check whether the\n"+
			"device will let us touch this app's sandbox.\n",
		bundleID, udid, bundleID)
	return 1
}

// next-task placeholder
return 1
```

And add helpers (in the same file or a small `helpers.go` per project convention):

```go
// resolveDevice picks the UDID to target. If --device is given, it's used as-is;
// otherwise the Devices lister is consulted and a single device is required.
func resolveDevice(ctx context.Context, lister device.Lister, flagUDID string) (string, error) {
	if flagUDID != "" {
		return flagUDID, nil
	}
	devs, err := lister.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list devices: %w", err)
	}
	switch len(devs) {
	case 0:
		return "", errors.New("no device connected")
	case 1:
		return devs[0].UDID, nil
	default:
		return "", fmt.Errorf("multiple devices connected; pick one with --device <UDID>")
	}
}

// probeVended reports whether results contains a ProbeVended outcome for bundleID.
func probeVended(results []apps.ProbeResult, bundleID string) bool {
	for _, r := range results {
		if r.BundleID == bundleID && r.Outcome == apps.ProbeVended {
			return true
		}
	}
	return false
}
```

(If M1/M2/M5 already have a `resolveDevice` shared helper, USE IT instead of redefining; the contract is the same. Check `cmd/ios-tidy/devices.go` and `cmd/ios-tidy/storage.go` for the existing one.)

- [ ] **Step 5: Run — expect PASS**

Run: `go test ./cmd/ios-tidy/... -run 'TestAppsClean_refuses' -v`
Expected: PASS.

- [ ] **Step 6: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps.go cmd/ios-tidy/apps_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: gate apps clean behind a Vended probe result"
```

---

## Task 10: Open sandbox + build plans

**Files:**
- Modify: `cmd/ios-tidy/apps.go`
- Modify: `cmd/ios-tidy/apps_clean_test.go`

- [ ] **Step 1: Write the failing test — sandbox.Open called with correct args**

```go
func TestAppsClean_opensSandboxAfterProbeGate(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 10}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 20}},
		},
	}
	sb := &sandbox.FakeSandbox{
		OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS},
	}
	var stdout bytes.Buffer
	deps := runDeps{
		Stdout:     &stdout,
		Stderr:     new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
		Sandbox:    sb,
		Prompter:   &ui.FakePrompter{Answers: []bool{false}}, // user says no — abort cleanly
	}
	_ = runAppsClean(context.Background(), deps, []string{"com.example.app", "--device", "U1"})
	if len(sb.OpenCalls) != 1 {
		t.Fatalf("Sandbox.Open call count = %d, want 1", len(sb.OpenCalls))
	}
	if !fakeFS.Closed() {
		t.Errorf("FakeFS.Close was not called")
	}
	if !strings.Contains(stdout.String(), "tmp") || !strings.Contains(stdout.String(), "Library/Caches") {
		t.Errorf("stdout should render both target names; got: %q", stdout.String())
	}
}
```

(`FakeSandbox` and `FakePrompter` come from M5/M4 respectively. If `FakeSandbox.OpenResults`'s key encoding is different in M5's implementation, adjust. The contract: `Open(ctx, udid, bundleID)` returns the FS keyed by `(udid, bundleID)`.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_opensSandboxAfterProbeGate -v`
Expected: FAIL — sandbox not opened, FakeFS never seen.

- [ ] **Step 3: Implement sandbox open + BuildPlan + render**

Replace the trailing `return 1` placeholder with:

```go
fsHandle, err := deps.Sandbox.Open(ctx, udid, bundleID)
if err != nil {
	fmt.Fprintf(deps.Stderr,
		"error: open sandbox for %q on %s: %v\n"+
			"The probe store says this bundle was vended, but the daemon now\n"+
			"refuses. The probe result may be stale; re-run\n"+
			"  ios-tidy apps probe --bundle %s\n"+
			"to refresh.\n",
		bundleID, udid, err, bundleID)
	return 1
}
defer fsHandle.Close()

var plans []sandbox.CleanPlan
addPlan := func(t sandbox.Target) error {
	p, err := sandbox.BuildPlan(ctx, fsHandle, t)
	if err != nil {
		return fmt.Errorf("build plan for %s: %w", t.Name, err)
	}
	plans = append(plans, p)
	return nil
}
if *includeTmp {
	if err := addPlan(sandbox.TargetTmp); err != nil {
		fmt.Fprintln(deps.Stderr, "error:", err)
		return 1
	}
}
if *includeCache {
	if err := addPlan(sandbox.TargetCaches); err != nil {
		fmt.Fprintln(deps.Stderr, "error:", err)
		return 1
	}
}
if *includeDocs {
	if err := addPlan(sandbox.TargetDocuments); err != nil {
		fmt.Fprintln(deps.Stderr, "error:", err)
		return 1
	}
}

ui.RenderCleanPlan(deps.Stdout, bundleID, plans)

// Later tasks add: dry-run, prompt, execute, summary.
return 1
```

Add a tiny helper to `internal/ui/plan.go` (M4 already has `RenderPlan` for crash logs; we add a sibling for the cleaner — the format is target-oriented):

```go
// internal/ui/plan.go (append)
import (
	"fmt"
	"io"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"
)

func RenderCleanPlan(w io.Writer, bundleID string, plans []sandbox.CleanPlan) {
	fmt.Fprintf(w, "Clean plan for %s:\n", bundleID)
	var total int64
	for _, p := range plans {
		fmt.Fprintf(w, "  %s/  %d files  %s\n", p.Target.Name, len(p.Files), FormatBytes(p.TotalBytes))
		total += p.TotalBytes
	}
	fmt.Fprintf(w, "Total: %s across %d target(s)\n", FormatBytes(total), len(plans))
}
```

(`FormatBytes` exists from M2/M4 in `internal/ui/bytes.go`. If its name differs, use the project's name.)

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_opensSandboxAfterProbeGate -v`
Expected: PASS.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps.go cmd/ios-tidy/apps_clean_test.go internal/ui/plan.go
# Wait for explicit user approval before running:
git commit -m "feat: open sandbox and render multi-target clean plan"
```

---

## Task 11: Dry-run short-circuit (the highest-value test)

**Files:**
- Modify: `cmd/ios-tidy/apps.go`
- Modify: `cmd/ios-tidy/apps_clean_test.go`

- [ ] **Step 1: Write the dry-run spy test**

```go
func TestAppsClean_dryRunNeverCallsRemove(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 10}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 20}},
		},
	}
	sb := &sandbox.FakeSandbox{
		OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS},
	}
	prompter := &ui.FakePrompter{} // no answers configured — if Confirm is called, it'll panic/error
	var stdout bytes.Buffer

	exit := runAppsClean(context.Background(), runDeps{
		Stdout:     &stdout,
		Stderr:     new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
		Sandbox:    sb,
		Prompter:   prompter,
	}, []string{"com.example.app", "--device", "U1", "--dry-run"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if len(fakeFS.RemoveCalls) != 0 {
		t.Errorf("Remove was called under --dry-run: %v", fakeFS.RemoveCalls)
	}
	if len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll was called under --dry-run: %v", fakeFS.RemoveAllCalls)
	}
	if len(prompter.Asked) != 0 {
		t.Errorf("Prompter was asked under --dry-run: %v", prompter.Asked)
	}
	if !strings.Contains(stdout.String(), "Dry run") {
		t.Errorf("stdout should announce dry run; got: %q", stdout.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_dryRunNeverCallsRemove -v`
Expected: FAIL — exit is 1 (placeholder), or prompt is asked.

- [ ] **Step 3: Insert the dry-run short-circuit AFTER `RenderCleanPlan` and BEFORE any prompt**

This placement is load-bearing: the short-circuit MUST sit between `ui.RenderCleanPlan(...)` and the Documents-or-basic prompt branch added by Tasks 12–13. That way the strict typed-bundle-ID prompt (Task 13) is unreachable under `--dry-run` — which is what the Step 7 sub-flow below asserts.

```go
if *dryRun {
	fmt.Fprintln(deps.Stdout, "Dry run — no changes made.")
	return 0
}
// Prompts and Execute follow in later tasks.
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_dryRunNeverCallsRemove -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 6: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps.go cmd/ios-tidy/apps_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: short-circuit apps clean on --dry-run before any mutation"
```

- [ ] **Step 7: Write the failing dry-run-with-`--include-documents` spy test**

This is the load-bearing addition from review H1. It pins the dry-run guarantee for the Documents path specifically: under `--dry-run --include-documents`, the per-file `Remove` loop MUST NOT run AND the strict typed-bundle-ID prompt (Task 13's `ReadLine`) MUST NOT be reached.

```go
func TestAppsClean_dryRunWithDocumentsNeverCallsRemoveOrPrompts(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"Documents": {
				{Path: "Documents/secret.txt", Size: 100},
				{Path: "Documents/photos/img.jpg", Size: 2048},
			},
		},
	}
	sb := &sandbox.FakeSandbox{
		OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS},
	}
	// Empty FakePrompter — if either Confirm OR ReadLine is invoked, the
	// AskedLines/Asked spy slices will record it, failing the assertions below.
	prompter := &ui.FakePrompter{}
	var stdout bytes.Buffer

	exit := runAppsClean(context.Background(), runDeps{
		Stdout:     &stdout,
		Stderr:     new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
		Sandbox:    sb,
		Prompter:   prompter,
	}, []string{"com.example.app", "--device", "U1", "--include-documents", "--dry-run"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if len(fakeFS.RemoveCalls) != 0 {
		t.Errorf("Remove was called under --dry-run --include-documents: %v", fakeFS.RemoveCalls)
	}
	if len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll was called under --dry-run --include-documents: %v", fakeFS.RemoveAllCalls)
	}
	if len(prompter.AskedLines) != 0 {
		t.Errorf("Strict typed-bundle-ID prompt was reached under --dry-run: %v", prompter.AskedLines)
	}
	if len(prompter.Asked) != 0 {
		t.Errorf("Any prompt was reached under --dry-run: %v", prompter.Asked)
	}
	if !strings.Contains(stdout.String(), "Documents") {
		t.Errorf("stdout should render the Documents target line in the plan; got: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Dry run") {
		t.Errorf("stdout should announce dry run; got: %q", stdout.String())
	}
}
```

- [ ] **Step 8: Run to verify the expected failure mode**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_dryRunWithDocumentsNeverCallsRemoveOrPrompts -v`

Expected: PASS if the dry-run short-circuit from Step 3 is correctly placed BEFORE the Documents branch (no production code changes needed — the placement from Step 3 already short-circuits before any prompt). If it FAILs with `Strict typed-bundle-ID prompt was reached under --dry-run`, that means a later task accidentally moved the dry-run check below the Documents branch — fix Task 13's diff in `runAppsClean` so the `if *dryRun { return 0 }` block remains above the `if *includeDocs { ... }` branch.

Note: this Step 8 is intentionally a "verify-the-existing-cadence-holds" rather than a fresh RED. Because Step 3 already added the short-circuit, the assertion is that no later task degrades it. If executed in strict sequence (Task 11 → 12 → 13 → … → run Step 8), this Step 8 should PASS without any production-code change. If you are revisiting this milestone after a refactor, Step 8 acts as the regression net.

- [ ] **Step 9: If Step 8 failed, fix the placement and re-run**

In `cmd/ios-tidy/apps.go`'s `runAppsClean`, ensure the structure is:

```go
ui.RenderCleanPlan(deps.Stdout, bundleID, plans)

if *dryRun {
	fmt.Fprintln(deps.Stdout, "Dry run — no changes made.")
	return 0
}

// (then Documents-or-basic prompt branch from Task 13)
```

Re-run: `go test ./cmd/ios-tidy/... -run TestAppsClean_dryRunWithDocumentsNeverCallsRemoveOrPrompts -v`
Expected: PASS.

- [ ] **Step 10: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 11: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps_clean_test.go
# Wait for explicit user approval before running:
git commit -m "test: cover apps clean dry-run with --include-documents"
```

---

## Task 12: Basic y/N confirmation

**Files:**
- Modify: `cmd/ios-tidy/apps.go`
- Modify: `cmd/ios-tidy/apps_clean_test.go`

- [ ] **Step 1: Write the failing "user says no" test**

```go
func TestAppsClean_basicPromptNoAborts(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp": {{Path: "tmp/a", Size: 10}},
		},
	}
	sb := &sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}
	prompter := &ui.FakePrompter{Answers: []bool{false}}

	exit := runAppsClean(context.Background(), runDeps{
		Stdout:     new(bytes.Buffer),
		Stderr:     new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
		Sandbox:    sb,
		Prompter:   prompter,
	}, []string{"com.example.app", "--device", "U1", "--include-tmp"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0 (clean abort)", exit)
	}
	if len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll called after user said no: %v", fakeFS.RemoveAllCalls)
	}
}
```

- [ ] **Step 2: Write the failing "user says yes — RemoveAll fires" test**

```go
func TestAppsClean_basicPromptYesProceeds(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 10}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 20}},
		},
	}
	sb := &sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}
	prompter := &ui.FakePrompter{Answers: []bool{true}}
	var stdout bytes.Buffer

	exit := runAppsClean(context.Background(), runDeps{
		Stdout:     &stdout,
		Stderr:     new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
		Sandbox:    sb,
		Prompter:   prompter,
	}, []string{"com.example.app", "--device", "U1"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	want := []string{"tmp", "Library/Caches"}
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Fatalf("RemoveAllCalls = %v, want %v", fakeFS.RemoveAllCalls, want)
	}
	if !strings.Contains(stdout.String(), "Deleted") {
		t.Errorf("stdout should contain summary; got: %q", stdout.String())
	}
}
```

- [ ] **Step 3: Run to verify failures**

Run: `go test ./cmd/ios-tidy/... -run 'TestAppsClean_basicPrompt' -v`
Expected: FAIL — current code returns 1 after RenderCleanPlan.

- [ ] **Step 4: Implement the prompt + execute path (no-Documents branch first)**

After the dry-run short-circuit, add:

```go
// Build the prompt question. Total bytes across all included plans.
var totalBytes int64
for _, p := range plans {
	totalBytes += p.TotalBytes
}

// Documents flow is handled in Task 13. For now, basic y/N.
if !*includeDocs {
	question := fmt.Sprintf(
		"Delete %s across %d target(s) in %s? [y/N]",
		ui.FormatBytes(totalBytes), len(plans), bundleID)
	if !*yes {
		ok, err := deps.Prompter.Confirm(ctx, question)
		if err != nil {
			fmt.Fprintln(deps.Stderr, "error:", err)
			return 1
		}
		if !ok {
			fmt.Fprintln(deps.Stdout, "Aborted.")
			return 0
		}
	}
}

// Execute each plan, aggregate results.
results := executePlans(ctx, fsHandle, plans)
return reportResults(deps.Stdout, deps.Stderr, results)
```

And helpers:

```go
func executePlans(ctx context.Context, fs sandbox.FS, plans []sandbox.CleanPlan) []sandbox.CleanResult {
	out := make([]sandbox.CleanResult, 0, len(plans))
	for _, p := range plans {
		out = append(out, sandbox.Execute(ctx, fs, p))
	}
	return out
}

func reportResults(stdout, stderr io.Writer, results []sandbox.CleanResult) int {
	var totalRemoved int
	var totalBytes int64
	var totalFailures int
	for _, r := range results {
		totalRemoved += r.Removed
		totalBytes += r.Bytes
		totalFailures += len(r.Failures)
	}
	fmt.Fprintf(stdout, "Deleted %d files (%s freed). %d failure(s).\n",
		totalRemoved, ui.FormatBytes(totalBytes), totalFailures)
	for _, r := range results {
		for _, f := range r.Failures {
			fmt.Fprintf(stderr, "  fail: %s: %v\n", f.Path, f.Err)
		}
	}
	if totalFailures > 0 {
		return 1
	}
	return 0
}
```

- [ ] **Step 5: Run — expect PASS for the two basic-prompt tests**

Run: `go test ./cmd/ios-tidy/... -run 'TestAppsClean_basicPrompt' -v`
Expected: PASS.

- [ ] **Step 6: Add `--yes` test**

```go
func TestAppsClean_yesFlagSkipsBasicPrompt(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{"tmp": {{Path: "tmp/a", Size: 1}}},
	}
	sb := &sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}
	prompter := &ui.FakePrompter{} // no answers configured

	exit := runAppsClean(context.Background(), runDeps{
		Stdout:     new(bytes.Buffer),
		Stderr:     new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
		Sandbox:    sb,
		Prompter:   prompter,
	}, []string{"com.example.app", "--device", "U1", "--include-tmp", "--yes"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if len(prompter.Asked) != 0 {
		t.Errorf("Prompter was asked even with --yes: %v", prompter.Asked)
	}
	if len(fakeFS.RemoveAllCalls) != 1 {
		t.Errorf("RemoveAll calls = %v, want 1", fakeFS.RemoveAllCalls)
	}
}
```

- [ ] **Step 7: Run — expect PASS**

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_yesFlagSkipsBasicPrompt -v`
Expected: PASS.

- [ ] **Step 8: Run full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 9: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps.go cmd/ios-tidy/apps_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: add basic y/N confirmation and partial-failure summary"
```

---

## Task 13: Strict typed-bundle-ID gate for `--include-documents`

The strict gate uses a different `Prompter` capability: instead of returning a bool, it reads a line. The current `ui.Prompter` is `Confirm(ctx, question) (bool, error)`. We need a second method, `ReadLine(ctx, prompt) (string, error)`. This is a small interface extension.

**Files:**
- Modify: `internal/ui/prompt.go` — add `ReadLine` to the interface and real impl.
- Modify: `internal/ui/prompt_test.go` — test `FakePrompter.ReadLine`.
- Modify: `cmd/ios-tidy/apps.go`
- Modify: `cmd/ios-tidy/apps_clean_test.go`

- [ ] **Step 1: Failing test for FakePrompter.ReadLine returning queued lines**

```go
// internal/ui/prompt_test.go (append)
func TestFakePrompter_ReadLine_returnsQueuedLines(t *testing.T) {
	p := &FakePrompter{Lines: []string{"com.example.app", "n"}}
	got, err := p.ReadLine(context.Background(), "type bundle id:")
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if got != "com.example.app" {
		t.Errorf("ReadLine = %q, want %q", got, "com.example.app")
	}
	if len(p.AskedLines) != 1 || p.AskedLines[0] != "type bundle id:" {
		t.Errorf("AskedLines = %v, want one entry", p.AskedLines)
	}
}

func TestFakePrompter_ReadLine_errorsWhenExhausted(t *testing.T) {
	p := &FakePrompter{}
	if _, err := p.ReadLine(context.Background(), "?"); err == nil {
		t.Errorf("ReadLine on empty FakePrompter should error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/... -run TestFakePrompter_ReadLine -v`
Expected: FAIL — `FakePrompter.ReadLine undefined`.

- [ ] **Step 3: Extend `Prompter` interface and `FakePrompter`**

In `internal/ui/prompt.go`:

```go
type Prompter interface {
	Confirm(ctx context.Context, question string) (bool, error)
	// ReadLine prompts with `prompt`, reads a single line from the user, and
	// returns the line WITHOUT the trailing newline. Returns ("", io.EOF) on
	// EOF. Tests use FakePrompter.Lines to queue answers.
	ReadLine(ctx context.Context, prompt string) (string, error)
}
```

In `internal/ui/prompt.go` (FakePrompter — keep existing Confirm bits intact):

```go
type FakePrompter struct {
	Answers    []bool   // queued Confirm results
	Asked      []string // questions asked
	Lines      []string // queued ReadLine results
	AskedLines []string // ReadLine prompts asked
	ConfirmErr error
	ReadLineErr error
}

func (f *FakePrompter) Confirm(_ context.Context, q string) (bool, error) {
	f.Asked = append(f.Asked, q)
	if f.ConfirmErr != nil {
		return false, f.ConfirmErr
	}
	if len(f.Answers) == 0 {
		return false, nil
	}
	a := f.Answers[0]
	f.Answers = f.Answers[1:]
	return a, nil
}

func (f *FakePrompter) ReadLine(_ context.Context, p string) (string, error) {
	f.AskedLines = append(f.AskedLines, p)
	if f.ReadLineErr != nil {
		return "", f.ReadLineErr
	}
	if len(f.Lines) == 0 {
		return "", errors.New("FakePrompter: no more queued lines")
	}
	l := f.Lines[0]
	f.Lines = f.Lines[1:]
	return l, nil
}
```

And the real implementation, in the same file or a separate `prompt_stdin.go`:

```go
type stdinPrompter struct{ r *bufio.Reader }

func NewStdinPrompter() Prompter { return &stdinPrompter{r: bufio.NewReader(os.Stdin)} }

func (s *stdinPrompter) Confirm(ctx context.Context, q string) (bool, error) {
	fmt.Printf("%s ", q)
	line, err := s.r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}

func (s *stdinPrompter) ReadLine(ctx context.Context, p string) (string, error) {
	fmt.Printf("%s ", p)
	line, err := s.r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
```

(If `NewStdinPrompter` already exists from M4 with only `Confirm`, just add `ReadLine` to it.)

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/ui/... -run TestFakePrompter_ReadLine -v`
Expected: PASS.

- [ ] **Step 5: Write the strict-gate "exact match → proceeds" test**

```go
// cmd/ios-tidy/apps_clean_test.go
func TestAppsClean_documentsExactBundleMatchProceeds(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"Documents": {{Path: "Documents/secret.txt", Size: 50}},
		},
	}
	sb := &sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}
	prompter := &ui.FakePrompter{Lines: []string{"com.example.app"}}
	var stdout bytes.Buffer

	exit := runAppsClean(context.Background(), runDeps{
		Stdout:     &stdout,
		Stderr:     new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store,
		Sandbox:    sb,
		Prompter:   prompter,
	}, []string{"com.example.app", "--device", "U1", "--include-documents"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if len(fakeFS.RemoveCalls) != 1 || fakeFS.RemoveCalls[0] != "Documents/secret.txt" {
		t.Errorf("RemoveCalls = %v, want one call to Documents/secret.txt", fakeFS.RemoveCalls)
	}
	if !strings.Contains(stdout.String(), "user data") || !strings.Contains(stdout.String(), "NOT recoverable") {
		t.Errorf("stdout should warn about user data; got: %q", stdout.String())
	}
}
```

- [ ] **Step 6: Write the mismatch tests (typo, empty, case)**

```go
func TestAppsClean_documentsBundleMismatchAborts(t *testing.T) {
	cases := []struct {
		name string
		typed string
	}{
		{"typo", "com.example.ap"},
		{"empty", ""},
		{"trailing whitespace stripped but content mismatch", "com.example.other"},
		{"case-sensitive mismatch", "COM.EXAMPLE.APP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &apps.FakeProbeStore{
				LoadResults: map[string][]apps.ProbeResult{
					"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
				},
			}
			fakeFS := &sandbox.FakeFS{
				WalkResults: map[string][]sandbox.FileInfo{
					"Documents": {{Path: "Documents/a", Size: 1}},
				},
			}
			sb := &sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}
			prompter := &ui.FakePrompter{Lines: []string{tc.typed}}
			var stdout bytes.Buffer

			exit := runAppsClean(context.Background(), runDeps{
				Stdout:     &stdout,
				Stderr:     new(bytes.Buffer),
				Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
				ProbeStore: store,
				Sandbox:    sb,
				Prompter:   prompter,
			}, []string{"com.example.app", "--device", "U1", "--include-documents"})

			if exit != 0 {
				t.Errorf("exit = %d, want 0 (clean abort)", exit)
			}
			if len(fakeFS.RemoveCalls) != 0 {
				t.Errorf("Remove was called despite mismatched confirmation: %v", fakeFS.RemoveCalls)
			}
			if !strings.Contains(stdout.String(), "did not match") {
				t.Errorf("stdout should say did not match; got: %q", stdout.String())
			}
		})
	}
}

func TestAppsClean_documentsTrailingNewlineMatches(t *testing.T) {
	// stdinPrompter strips \r\n itself; FakePrompter delivers raw strings.
	// This test pins behaviour: a queued "com.example.app\n" should still match
	// because cmd code strips before compare. We assert by sending the bare bundle.
	// (No separate code path needed — kept as a sanity test.)
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{"Documents": {{Path: "Documents/a", Size: 1}}},
	}
	sb := &sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}
	prompter := &ui.FakePrompter{Lines: []string{"com.example.app\n"}}

	exit := runAppsClean(context.Background(), runDeps{
		Stdout: new(bytes.Buffer), Stderr: new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store, Sandbox: sb, Prompter: prompter,
	}, []string{"com.example.app", "--device", "U1", "--include-documents"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if len(fakeFS.RemoveCalls) != 1 {
		t.Errorf("RemoveCalls = %v, want one (newline should be stripped)", fakeFS.RemoveCalls)
	}
}
```

- [ ] **Step 7: Write the `--yes` does NOT bypass strict gate test**

```go
func TestAppsClean_yesDoesNotBypassDocumentsStrictGate(t *testing.T) {
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{"Documents": {{Path: "Documents/a", Size: 1}}},
	}
	sb := &sandbox.FakeSandbox{OpenResults: map[string]sandbox.FS{"U1\x00com.example.app": fakeFS}}
	prompter := &ui.FakePrompter{Lines: []string{"com.example.app"}} // matched line still required

	exit := runAppsClean(context.Background(), runDeps{
		Stdout: new(bytes.Buffer), Stderr: new(bytes.Buffer),
		Devices:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store, Sandbox: sb, Prompter: prompter,
	}, []string{"com.example.app", "--device", "U1", "--include-documents", "--yes"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if len(prompter.AskedLines) != 1 {
		t.Errorf("AskedLines len = %d, want 1 (strict gate even with --yes)", len(prompter.AskedLines))
	}
	if len(fakeFS.RemoveCalls) != 1 {
		t.Errorf("Remove not called even with matched bundle: %v", fakeFS.RemoveCalls)
	}
}
```

- [ ] **Step 8: Run to verify all the above tests fail**

Run: `go test ./cmd/ios-tidy/... -run 'TestAppsClean_documents|TestAppsClean_yesDoesNotBypass' -v`
Expected: multiple FAIL — Documents gate not yet implemented.

- [ ] **Step 9: Implement the strict gate**

Replace the prompt block in `runAppsClean` with a branching version:

```go
if *includeDocs {
	fmt.Fprintf(deps.Stdout,
		"WARNING: this will delete user data in %s's Documents folder. Files are NOT recoverable.\n",
		bundleID)
	typed, err := deps.Prompter.ReadLine(ctx,
		fmt.Sprintf("Type the bundle ID (%s) to confirm:", bundleID))
	if err != nil {
		fmt.Fprintln(deps.Stderr, "error:", err)
		return 1
	}
	if strings.TrimSpace(typed) != bundleID {
		fmt.Fprintln(deps.Stdout, "Bundle ID did not match. Aborted.")
		return 0
	}
	// Strict gate cleared. --yes does NOT bypass this gate.
} else {
	question := fmt.Sprintf(
		"Delete %s across %d target(s) in %s? [y/N]",
		ui.FormatBytes(totalBytes), len(plans), bundleID)
	if !*yes {
		ok, err := deps.Prompter.Confirm(ctx, question)
		if err != nil {
			fmt.Fprintln(deps.Stderr, "error:", err)
			return 1
		}
		if !ok {
			fmt.Fprintln(deps.Stdout, "Aborted.")
			return 0
		}
	}
}
```

(Note: case-sensitive comparison is via `==`. `strings.TrimSpace` strips the trailing `\n` the FakePrompter may include but does not lowercase — this is the deliberate case-sensitivity choice from Open Question #2.)

- [ ] **Step 10: Run — expect PASS for all the Documents tests**

Run: `go test ./cmd/ios-tidy/... -run 'TestAppsClean_documents|TestAppsClean_yesDoesNotBypass' -v`
Expected: PASS.

- [ ] **Step 11: Run the full suite**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 12: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps.go cmd/ios-tidy/apps_clean_test.go internal/ui/prompt.go internal/ui/prompt_test.go
# Wait for explicit user approval before running:
git commit -m "feat: add strict typed-bundle-ID gate for --include-documents"
```

---

## Task 14: Open-after-stale-probe error path

**Files:**
- Modify: `cmd/ios-tidy/apps_clean_test.go` (test only — production code already returns the right exit code from Task 10's `Sandbox.Open` error block)

- [ ] **Step 1: Write the failing-Open test**

```go
func TestAppsClean_openErrorHintsStaleProbe(t *testing.T) {
	bang := errors.New("connect afc service failed")
	store := &apps.FakeProbeStore{
		LoadResults: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	sb := &sandbox.FakeSandbox{OpenErr: bang}
	var stderr bytes.Buffer

	exit := runAppsClean(context.Background(), runDeps{
		Stdout:  new(bytes.Buffer),
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		ProbeStore: store, Sandbox: sb,
		Prompter: &ui.FakePrompter{},
	}, []string{"com.example.app", "--device", "U1"})

	if exit == 0 {
		t.Errorf("exit = 0, want non-zero")
	}
	// Split into two checks so a future copy-edit of the exact hint wording
	// doesn't silently break the safety hint. Both substrings together still
	// guarantee the user sees the command name AND the staleness explanation.
	if !strings.Contains(stderr.String(), "apps probe") {
		t.Errorf("stderr should mention the apps probe command; got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "stale") {
		t.Errorf("stderr should explain the staleness possibility; got: %q", stderr.String())
	}
}
```

- [ ] **Step 2: Run — should PASS already** (Task 10 implemented the message)

Run: `go test ./cmd/ios-tidy/... -run TestAppsClean_openErrorHintsStaleProbe -v`
Expected: PASS.

- [ ] **Step 3: Commit (await user approval)**

```bash
git add cmd/ios-tidy/apps_clean_test.go
# Wait for explicit user approval before running:
git commit -m "test: lock in stale-probe hint when Sandbox.Open fails"
```

---

## Task 15: `//go:build device` integration test

**Files:**
- Create: `internal/iosbackend/sandbox_clean_device_test.go`

- [ ] **Step 1: Write the integration test**

```go
//go:build device
// +build device

package iosbackend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/house_arrest"
)

func TestSandboxClean_endToEnd(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID not set")
	}
	if os.Getenv("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE") != "1" {
		t.Skip("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE != 1 — refusing to delete on a real device")
	}
	bundleID := os.Getenv("IOS_TIDY_TEST_SENTINEL_BUNDLE_ID")
	if bundleID == "" {
		t.Skip("IOS_TIDY_TEST_SENTINEL_BUNDLE_ID not set — supply a TestFlight/dev-signed app you have installed")
	}

	dev, err := ios.GetDevice(udid)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	afcClient, err := house_arrest.New(dev, bundleID)
	if err != nil {
		t.Fatalf("house_arrest.New: %v", err)
	}
	defer afcClient.Close()

	// Push a sentinel file into tmp/ via the raw go-ios client (Push is not
	// part of our sandbox.FS interface — test-only escape hatch).
	sentinelName := fmt.Sprintf("ios-tidy-sentinel-%d.txt", time.Now().UnixNano())
	tmpHostFile := filepath.Join(t.TempDir(), sentinelName)
	if err := os.WriteFile(tmpHostFile, []byte("ios-tidy test"), 0o644); err != nil {
		t.Fatalf("write host sentinel: %v", err)
	}
	if err := afcClient.Push(tmpHostFile, "tmp/"+sentinelName); err != nil {
		t.Fatalf("Push sentinel: %v", err)
	}

	// Wrap the raw client in our sandbox.FS via the constructor used by Open.
	fs := &fsImpl{c: afcClient}

	plan, err := sandbox.BuildPlan(context.Background(), fs, sandbox.TargetTmp)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.TotalBytes == 0 {
		t.Fatalf("BuildPlan returned zero bytes; sentinel push may have failed")
	}

	res := sandbox.Execute(context.Background(), fs, plan)
	if len(res.Failures) != 0 {
		t.Errorf("Execute failures: %+v", res.Failures)
	}

	// Re-build the plan: should be empty (or no longer contain the sentinel).
	plan2, err := sandbox.BuildPlan(context.Background(), fs, sandbox.TargetTmp)
	if err != nil {
		t.Fatalf("BuildPlan post-clean: %v", err)
	}
	for _, f := range plan2.Files {
		if filepath.Base(f.Path) == sentinelName {
			t.Errorf("sentinel %s still present after clean", sentinelName)
		}
	}
}
```

- [ ] **Step 2: Confirm the file compiles under the device tag**

Run: `go build -tags=device ./internal/iosbackend/...`
Expected: success. (We do NOT run the test — that's an on-device step in Task 19.)

- [ ] **Step 3: Commit (await user approval)**

```bash
git add internal/iosbackend/sandbox_clean_device_test.go
# Wait for explicit user approval before running:
git commit -m "test: add device-tagged integration test for sandbox clean"
```

---

## Task 16: Write README.md

This is the deliverable. The entire README is reproduced here so the executing agent can copy it verbatim (with the small substitutions noted in comments).

**Files:**
- Create: `README.md` (project root)

- [ ] **Step 1: Write `README.md`**

```markdown
# ios-tidy

> A small, honest, USB-C-only iPhone storage cleanup CLI for macOS. Crash logs, per-app sandbox tmp/Caches/Documents for apps the device chooses to vend, and full app uninstall. Nothing more.

## What it does

- **Lists connected iPhones** with name, model, iOS version, UDID, trust state.
- **Reports device storage** plus a per-user-app size breakdown.
- **Lists, pulls and deletes crash logs** — the single biggest reliable win on most devices (100 MB – 2 GB typical).
- **Probes each user app** to see whether iOS's `mobile_house_arrest` daemon will let us touch its sandbox, and caches the answer per device.
- **Cleans per-app `tmp/`, `Library/Caches/` and (with strict confirmation) `Documents/`** for apps the probe confirmed as vended.
- All destructive operations support `--dry-run` and prompt before deleting.

## What it CANNOT do

These limits come from iOS itself, not from any library. They apply equally to every non-jailbroken USB-C cleaner.

- **Clear system caches** outside an app sandbox (`/private/var/mobile/Library/Caches/com.apple.*`). No service exposes them. [RESEARCH.md §5.1]
- **Clear Safari / WebKit caches.** Owned by system app sandboxes the daemon refuses to vend. [RESEARCH.md §5.2]
- **Clear Mail attachments.** Same — `com.apple.mobilemail`'s sandbox is not vended. [RESEARCH.md §5.3]
- **Touch the "Other" / "System Data" bucket.** Apple does not decompose this even on-device beyond Settings' opaque label. [RESEARCH.md §5.4]
- **Offload an app while keeping its data.** `installation_proxy` only exposes Install/Upgrade/Uninstall. "Keep data, drop binary" is iCloud-mediated with no public service. [RESEARCH.md §5.5]
- **Clean per-app Caches for App Store apps that the daemon refuses to vend.** On iOS 17+, `VendContainer` typically only succeeds for apps signed with `get-task-allow` (TestFlight, Xcode-installed, sideloaded). Vanilla App Store apps are commonly refused. `ios-tidy apps probe` tells you which is which on your device. [RESEARCH.md §3, §5.6]
- **Control iCloud** (Optimize Photos, iCloud Drive offload). [RESEARCH.md §5.7]
- **Delete Music or Podcasts downloaded media.** Files are reachable via AFC but tied to a CoreData DB that AFC can't update — deletion creates orphans. [RESEARCH.md §5.8]
- **Delete photos.** AFC can reach `/var/mobile/Media/DCIM` but deleting via AFC bypasses Photos.app and corrupts the Photos.sqlite database. Use Photos.app or PhotoKit on the host. [RESEARCH.md §4]

If a tool on the internet claims to do any of the above without jailbreak, it's either lying or doing something dangerous.

## Install

### From source (Go 1.23+)

```bash
go install github.com/anh-pham191/ios-tidy/cmd/ios-tidy@latest
```

The binary lands in `$(go env GOPATH)/bin/ios-tidy`. Make sure that's on your `PATH`.

### Homebrew

A Homebrew tap is planned post-launch. Until then, use `go install`.

## Quick start

```bash
# 1. See what's plugged in.
ios-tidy devices

# 2. See where storage is going.
ios-tidy storage

# 3. See what crash logs you could clean — but don't clean yet.
ios-tidy crashlogs clean --dry-run

# 4. Probe which apps the device will let us touch.
ios-tidy apps probe --all

# 5. Clean an app's caches (TestFlight / dev-signed / sideloaded apps usually work).
ios-tidy apps clean com.example.myapp --dry-run
ios-tidy apps clean com.example.myapp
```

## Commands

### `ios-tidy devices`

List connected iPhones.

```bash
ios-tidy devices
ios-tidy devices --json
```

### `ios-tidy storage [--device UDID] [--limit N] [--json]`

Show free/total volume bytes and a per-user-app size table.

```bash
ios-tidy storage
ios-tidy storage --device 00008110-001A1B2C3D4E5F6G --limit 20
ios-tidy storage --json
```

The free/total numbers are AFC-reported and may skew from Settings by a few hundred MB. [RESEARCH.md §7.3]

### `ios-tidy crashlogs list [--device UDID] [--pattern GLOB] [--json]`

List crash logs.

```bash
ios-tidy crashlogs list
ios-tidy crashlogs list --pattern 'Safari-*'
```

### `ios-tidy crashlogs pull --out DIR [--device UDID] [--pattern GLOB] [--force]`

Copy crash logs to a host directory.

```bash
ios-tidy crashlogs pull --out ./crashlogs
ios-tidy crashlogs pull --out ./crashlogs --pattern '*.ips' --force
```

### `ios-tidy crashlogs clean [--device UDID] [--pattern GLOB] [--dry-run] [--yes]`

Delete crash logs.

```bash
ios-tidy crashlogs clean --dry-run
ios-tidy crashlogs clean
ios-tidy crashlogs clean --yes  # skip the y/N prompt; still prints the plan
```

### `ios-tidy apps list [--device UDID] [--json]`

List installed user apps with their reported sizes.

```bash
ios-tidy apps list
ios-tidy apps list --json
```

`DynamicDiskUsage` may be zero for cold apps. Launch the app once and try again. [RESEARCH.md §7.4]

### `ios-tidy apps probe [--device UDID] [--bundle ID...] [--all] [--timeout 5s] [--json]`

Probe each bundle ID to see whether `mobile_house_arrest` will vend its sandbox. Results are cached at `~/Library/Application Support/ios-tidy/probes/<UDID>.json`.

```bash
ios-tidy apps probe --all
ios-tidy apps probe --bundle com.example.myapp --bundle org.mozilla.ios.Firefox
```

Outcomes: `vended` (we can touch its sandbox), `refused` (daemon said no — typical for App Store apps without `get-task-allow`), `error` (transport failure — retry), `unknown` (not probed yet).

### `ios-tidy apps clean BUNDLE_ID [--device UDID] [--dry-run] [--yes] [--include-tmp] [--include-caches] [--include-documents]`

Clean a per-app sandbox. **Refuses to run unless `apps probe` has confirmed `vended` for this bundle.**

Default targets: `tmp/` and `Library/Caches/`. If you pass any of `--include-tmp`, `--include-caches`, `--include-documents`, the defaults are switched off and only your explicit choices are included.

```bash
# Dry-run first — shows what would be deleted, never mutates.
ios-tidy apps clean com.example.myapp --dry-run

# Standard interactive flow.
ios-tidy apps clean com.example.myapp

# tmp only.
ios-tidy apps clean com.example.myapp --include-tmp

# Both file-cache targets explicit, Documents OFF — locks in the
# "explicit flags REPLACE defaults" rule. Identical effective targets
# to the bare `ios-tidy apps clean com.example.myapp` form, but spelled
# out for scripting clarity.
ios-tidy apps clean com.example.myapp --include-tmp --include-caches

# Documents — extra strict: type the bundle ID exactly to confirm.
# --yes does NOT bypass this typed-bundle-ID gate.
ios-tidy apps clean com.example.myapp --include-documents
```

The Documents flow asks you to retype the bundle ID exactly (case-sensitive) before any file is deleted. This is by design: Documents holds user data and is not recoverable from this side.

## Troubleshooting

### "no device connected" / "multiple devices connected"

- Plug the iPhone in with a known-good USB-C cable. `ios-tidy devices` should show it.
- If two phones are plugged in, pass `--device <UDID>` from the `ios-tidy devices` output.

### "Trust this computer" dialog won't go away

On the device, accept the dialog. If it doesn't appear, unplug, reboot the phone, plug back in. macOS `usbmuxd` is a stock LaunchDaemon — you don't need Homebrew `libimobiledevice`.

### macOS Tahoe (macOS 26) — pair-record access blocked by TCC

`go-ios` has an open issue ([#710](https://github.com/danielpaulus/go-ios/issues/710)) on macOS 26 Tahoe where TCC blocks reading the pair record. The documented `--pair-record-path` workaround is reported as not working as of 2026-05-23. If you're on Tahoe and get `failed to read pair record` errors, the current options are: downgrade to macOS 14/15, or wait for an upstream fix. [RESEARCH.md §6]

### `connect afc service failed` on iOS 17.1+

`go-ios` issue [#653](https://github.com/danielpaulus/go-ios/issues/653) documents sporadic `house_arrest` failures on iOS 17.1+. `ios-tidy` classifies these as transport errors (vs policy refusals) — retry the same command a few times. If it persists, the daemon may genuinely refuse this bundle; `apps probe` will record it as `refused`. [RESEARCH.md §3]

### `apps clean` says "not been confirmed as vended"

The probe gate refuses to touch any bundle ID that hasn't been recorded as `vended` in the probe store. Run:

```bash
ios-tidy apps probe --bundle <BUNDLE_ID>
```

If the result is `refused`, the daemon won't let us into this app's sandbox. Try Settings → General → iPhone Storage → <app> → Offload App on the device, or use the App Store's "Delete and Reinstall" flow.

If the result is `vended` but `apps clean` then fails to open the sandbox, the probe may be stale — re-run `apps probe` to refresh.

### `DynamicDiskUsage` reads zero for an app I know is huge

Open the app on the device, wait a few seconds, then re-run `apps list` or `storage`. Cold apps sometimes report zero. [RESEARCH.md §7.4]

## Development

### Run unit tests

```bash
make test
```

Equivalent to `go test ./... -race`. Covers everything except `internal/iosbackend/*_device_test.go`.

### Run device integration tests

```bash
IOS_TIDY_TEST_UDID=<your-udid> make test-device
```

Equivalent to `go test -tags=device ./internal/iosbackend/...`. Each integration test `t.Skip`s if `IOS_TIDY_TEST_UDID` is unset, so the `make` target is safe to leave wired.

Destructive integration tests additionally require:

```bash
export IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1
export IOS_TIDY_TEST_SENTINEL_BUNDLE_ID=com.your-org.your-test-app
```

The sentinel bundle must be an app you have installed AND consent to having tmp/ files written into and deleted. Pick a TestFlight or Xcode-built app you control.

### Coverage

```bash
make test-cover
```

Targets ≥ 80% coverage on every `internal/*` package other than `internal/iosbackend/` (which is integration-tested on real hardware).

### Build a distributable binary

```bash
make build  # → ./bin/ios-tidy
```

Build flags: `-trimpath -ldflags="-s -w -X main.Version=<git-describe>"`.

### Lint

```bash
make lint
```

Runs `go vet ./...` plus `staticcheck ./...` if `staticcheck` is on `PATH`.

## License

MIT. See `LICENSE`.
```

- [ ] **Step 2: Verify README is at the project root**

Run: `ls README.md`
Expected: file present.

- [ ] **Step 3: Run the full suite (no code changes, sanity only)**

Run: `go test ./...`
Expected: all `ok`.

- [ ] **Step 4: Commit (await user approval)**

```bash
git add README.md
# Wait for explicit user approval before running:
git commit -m "docs: add README with honest scope and troubleshooting"
```

---

## Task 17: Polish the Makefile

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Read current Makefile**

Run: `cat Makefile`
Expected: existing `test` target from earlier milestones.

- [ ] **Step 2: Replace with a polished version**

```make
.PHONY: test test-device test-cover lint build clean

GO            ?= go
PKG            = ./...
BIN_DIR        = bin
BIN            = $(BIN_DIR)/ios-tidy
GIT_DESCRIBE  := $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
LDFLAGS        = -s -w -X main.Version=$(GIT_DESCRIBE)

test:
	$(GO) test -race $(PKG)

test-device:
	@if [ -z "$$IOS_TIDY_TEST_UDID" ]; then \
	  echo "IOS_TIDY_TEST_UDID must be set for device tests"; exit 2; \
	fi
	$(GO) test -tags=device ./internal/iosbackend/...

test-cover:
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -n 1

lint:
	$(GO) vet $(PKG)
	@if command -v staticcheck >/dev/null 2>&1; then \
	  staticcheck $(PKG); \
	else \
	  echo "staticcheck not installed — skipping (install: go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
	fi

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/ios-tidy

clean:
	rm -rf $(BIN_DIR) coverage.out
```

- [ ] **Step 3: Verify each target**

Run: `make test`
Expected: PASS.

Run: `make build`
Expected: `bin/ios-tidy` exists.

Run: `make lint`
Expected: clean output, possibly the "staticcheck not installed — skipping" line.

Run: `make test-cover`
Expected: a "total: NN.N%" line.

(Do NOT run `make test-device` here — it requires a real phone.)

- [ ] **Step 4: Commit (await user approval)**

```bash
git add Makefile
# Wait for explicit user approval before running:
git commit -m "chore: polish Makefile (test, test-device, test-cover, lint, build)"
```

---

## Task 18: Coverage sweep — add tests where genuine gaps exist

**Files:**
- TBD per the coverage report.

- [ ] **Step 1: Run coverage report**

Run: `make test-cover`
Expected: a per-function table; capture which `internal/*` package is below 80%.

- [ ] **Step 2: Per package below 80%, write ONE new test per uncovered branch**

For each package below 80%:
1. Run `go test -coverprofile=/tmp/cov.out ./internal/<pkg>/...`
2. Run `go tool cover -html=/tmp/cov.out -o /tmp/cov.html` and open it.
3. Identify the red regions.
4. For each red region, write a NEW test (do not modify existing tests — `agent-safety/SKILL.md` "Tests Are Specifications") with the RED → GREEN cadence:
   - Write the test
   - Run it and verify it fails (or passes if the gap was a missed assertion)
   - If the gap is in production code, add the minimal code to cover it
   - Run again and verify green
5. After every red region in the package is covered, run `make test-cover` and confirm the package is ≥ 80%.

(This step is intentionally open-ended because the gaps depend on M1–M5's actual implementation. The agent executing this task should NOT modify existing tests; only add new ones, and only against genuinely uncovered branches.)

- [ ] **Step 3: Commit each package's coverage additions separately (await user approval each time)**

```bash
git add internal/<pkg>/<file>_test.go
# Wait for explicit user approval before running:
git commit -m "test: cover <specific branch> in internal/<pkg>"
```

If no packages are below 80%, skip this task and commit nothing.

---

## Task 19: Acceptance walkthrough

**Files:**
- Create: `docs/acceptance-walkthrough.md`

This is a manual on-device run. The plan provides the template and the steps; the user fills the result column as they walk it.

- [ ] **Step 1: Create the template**

```markdown
# Acceptance walkthrough — ios-tidy M6

Date: <fill in YYYY-MM-DD>
Device: <model, iOS version, UDID>
Host: <macOS version>

## Steps

1. `ios-tidy devices` — expected: table with the device, trust state `trusted`. Result:
2. `ios-tidy storage` — expected: header line + per-app table sorted desc. Result:
3. `ios-tidy storage --json | jq '.device'` — expected: well-formed JSON. Result:
4. `ios-tidy crashlogs list` — expected: list with sizes + mtimes. Result:
5. `ios-tidy crashlogs pull --out /tmp/crashlogs-test --pattern '*.ips'` — expected: files copied. Result:
6. `ios-tidy crashlogs clean --dry-run` — expected: plan + "Dry run — no changes made.", no deletion. Result:
7. `ios-tidy crashlogs clean` — answer `n` — expected: aborted, no deletion. Result:
8. `ios-tidy crashlogs clean` — answer `y` — expected: summary. Result:
9. `ios-tidy apps list --limit 10` — expected: top-10 apps by total bytes. Result:
10. `ios-tidy apps probe --all --timeout 10s` — expected: outcome column populated. Result:
11. Pick a `vended` bundle ID. `ios-tidy apps clean <bundle> --dry-run` — expected: plan, no deletion. Result:
12. `ios-tidy apps clean <bundle>` — answer `n` — expected: aborted. Result:
13. `ios-tidy apps clean <bundle>` — answer `y` — expected: deletion summary. Result:
14. `ios-tidy apps clean <bundle> --include-documents` — at the typed-bundle prompt, type a wrong value — expected: `Bundle ID did not match. Aborted.`, no deletion. Result:
15. `ios-tidy apps clean <bundle> --include-documents` — type the correct bundle — expected: deletion summary. (Only run this on a sentinel/test app whose Documents data you can lose.) Result:
16. Pick a `refused` bundle ID. `ios-tidy apps clean <bundle>` — expected: probe-gate refusal pointing at `apps probe`. Result:
17. Stage stale probe: edit the probe JSON to mark a known-refused bundle as vended, then `ios-tidy apps clean <bundle>` — expected: open-fails, error mentions "may be stale" and points at `apps probe`. Restore the file when done. Result:

## Notes

<free text>
```

- [ ] **Step 2: Commit the template (await user approval)**

```bash
git add docs/acceptance-walkthrough.md
# Wait for explicit user approval before running:
git commit -m "docs: add M6 acceptance walkthrough template"
```

- [ ] **Step 3: User runs the walkthrough**

Have the user run each step against their real device, recording outcomes inline. This is the final acceptance gate.

- [ ] **Step 4: Once the walkthrough is clean, optionally commit the filled-in results (await user approval)**

```bash
git add docs/acceptance-walkthrough.md
# Wait for explicit user approval before running:
git commit -m "docs: record M6 acceptance walkthrough results"
```

---

## Self-review

### 1. Spec coverage

Every item in SHARED_CONTEXT.md §8 M6 is mapped to a task:

| Requirement | Task(s) |
|---|---|
| `apps clean BUNDLE_ID [--device UDID] [--dry-run] [--yes] [--include-documents] [--include-tmp] [--include-caches]` | 8 |
| Refuses without Vended probe; points at `apps probe` | 9 |
| Default targets tmp + Library/Caches | 8 (default flag logic) |
| Documents opt-in with extra confirmation | 13 |
| Plan rendering: paths, total bytes, file count per target | 10 (`RenderCleanPlan`) |
| `--dry-run` never calls mutating FS methods (spy fake verifies) | 11 |
| Non-zero exit on partial failure with summary | 12 (`reportResults`) + 7 (partial-fail Execute tests) |
| Full unit-test destructive matrix incl. Documents extra-confirm | 11, 12, 13 |
| `//go:build device` integration test gated on env vars + sentinel | 15 |
| README — what / can't / install / commands / troubleshoot / unit vs integration tests | 16 |
| Final `make test` clean | 17 + every commit step ends with green test suite |

### 2. Placeholder scan

- "TBD" appears once in Task 18's "Files: TBD per coverage report" — intentional, because the gaps are emergent from M1–M5's actual coverage. Task 18 is itself bounded by a concrete procedure (run cover, identify red, add ONE test per branch, commit per package). Not a "fill in details" placeholder.
- No "implement later", "add appropriate error handling", "similar to Task N", "write tests for the above" patterns.
- Every test body and production-code block is shown in full.

### 3. Type consistency

- `Target`, `CleanPlan`, `CleanResult`, `Failure`, `BuildPlan`, `Execute` — all defined in Task 4, referenced consistently thereafter.
- `Prompter.Confirm` (existing) and `Prompter.ReadLine` (added in Task 13) — consistent throughout.
- `FakeFS` spy fields (`RemoveCalls`, `RemoveAllCalls`, `WalkResults`, `RemoveErr`, `RemoveErrByPath`, etc.) — defined in Tasks 2 and 7, used consistently in cleaner_test and apps_clean_test.
- `runDeps` is assumed to exist from M1–M5; no new struct invented here.
- `apps.FakeProbeStore`, `device.FakeLister`, `sandbox.FakeSandbox`, `ui.FakePrompter` — all assumed to exist from prior milestones.
- `ui.FormatBytes`, `ui.RenderCleanPlan` — used consistently after introduction in Task 10.
- `convertFileInfo`, `fsImpl`, `joinPath` — defined in Task 3, used by the device test in Task 15.

### 4. TDD cadence

Every production-code-adding task uses RED → verify-RED → GREEN → verify-GREEN → commit:
- Task 2: test → run-fail → impl → run-pass → commit.
- Task 3: test → run-fail → impl → run-pass → commit.
- Task 4: test → run-fail → impl → run-pass → commit.
- Tasks 5–7: all-tests pattern; some BuildPlan and Execute tests are pure additions to already-green code (Task 5 step 2 "expect PASS") and the plan calls that out explicitly.
- Tasks 8–14: each step pair is test → fail → impl → pass → commit.
- Task 15: write test + compile-check (no run — device gate).
- Task 16: docs only, no TDD.
- Task 17: Makefile + sanity runs.
- Task 18: coverage-driven, each gap follows TDD.
- Task 19: manual walkthrough.

### 5. No code outside `internal/iosbackend/` imports `go-ios`

- `internal/iosbackend/sandbox.go` (Task 3) — imports `github.com/danielpaulus/go-ios/ios/afc`.
- `internal/iosbackend/sandbox_clean_device_test.go` (Task 15) — imports `ios` and `house_arrest`. Inside iosbackend, allowed.
- No other file added by this plan imports go-ios. Verified.

### 6. Every destructive command has a dry-run path AND a confirmation gate

- `apps clean`: `--dry-run` (Task 11), basic y/N (Task 12), strict typed-bundle-ID for Documents (Task 13). Three layers.

---

## Open-question summary (repeat for reviewer convenience)

1. Dry-run includes `Sandbox.Open` + `Walk` + `Stat` (all read-only) — see §"Open questions" for rationale.
2. Bundle-ID confirmation is case-sensitive `==` after `TrimSpace`.
3. `sandbox.Failure` defined locally, not imported from `crashlogs`.
4. `--include-*` flags REPLACE the defaults rather than augmenting.
5. Integration test uses raw go-ios `Push` rather than extending `sandbox.FS`.

All resolutions are reasoned, not silent.

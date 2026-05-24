# Shared context for ios-tidy planning subagents

This file is the single source of truth for module path, directory layout, seam
interfaces, test conventions, and commit conventions. Every milestone plan must
be written against this. If you (planning subagent) think a deviation is
warranted, surface it as an "Open question" at the top of your plan rather than
silently diverging — a peer-reviewer subagent will catch silent divergences and
your plan will go back through the review cycle.

**Today's date is 2026-05-23.** Use it for plan filenames.

---

## 1. Project orientation

- Project root: `/Users/anhpham/Documents/Projects/script/ios-tidy/`
- Go module path: `github.com/anh-pham191/ios-tidy`
- Go version: 1.23 (use modern stdlib idioms — `slices`, `maps`, `cmp`)
- Primary library: `github.com/danielpaulus/go-ios` pinned at v1.0.213+
- Target platform: `darwin/arm64` (Apple Silicon Mac); `darwin/amd64` should also build
- Distribution: single static binary (`go build -trimpath -ldflags="-s -w"`)
- License: MIT

## 2. Directory layout

Every file has one responsibility. Plans MUST use these exact paths.

```
ios-tidy/
├── go.mod
├── go.sum
├── LICENSE
├── Makefile                       — test / lint / build / build-device-test targets
├── README.md                      — written in M6 only; do not draft earlier
├── RESEARCH.md                    — already exists; do not modify
├── cmd/
│   └── ios-tidy/
│       ├── main.go                — CLI entry; wires subcommands via flag.FlagSet groups
│       ├── devices.go             — `devices` subcommand wiring (M1)
│       ├── storage.go             — `storage` subcommand wiring (M2)
│       ├── crashlogs.go           — `crashlogs {list,pull,clean}` subcommand wiring (M3, M4)
│       ├── apps.go                — `apps {list,probe,clean}` subcommand wiring (M5, M6)
│       └── version.go             — version + build info
├── internal/
│   ├── device/
│   │   ├── device.go              — Device type, Lister + TrustChecker interfaces
│   │   ├── device_test.go         — unit tests against fakes
│   │   └── fake.go                — exported FakeLister / FakeTrustChecker for cross-pkg tests
│   ├── storage/
│   │   ├── storage.go             — DeviceInfo type, Client interface
│   │   ├── storage_test.go
│   │   └── fake.go
│   ├── apps/
│   │   ├── apps.go                — App type, Lister + Uninstaller interfaces
│   │   ├── apps_test.go
│   │   ├── fake.go
│   │   ├── probe.go               — ProbeResult, Prober interface, ProbeStore (M5)
│   │   └── probe_test.go
│   ├── crashlogs/
│   │   ├── crashlogs.go           — Entry, PullResult, RemoveResult, Failure, Client interface
│   │   ├── crashlogs_test.go
│   │   └── fake.go
│   ├── sandbox/
│   │   ├── sandbox.go             — Sandbox + FS + FileInfo + WalkFunc
│   │   ├── sandbox_test.go
│   │   ├── fake.go                — in-memory FS for unit tests of consumers
│   │   ├── cleaner.go             — Cleaner: planning + executing per-app cleanup (M6)
│   │   └── cleaner_test.go
│   ├── ui/
│   │   ├── prompt.go              — Prompter interface; real impl reads stdin
│   │   ├── prompt_test.go
│   │   ├── table.go               — table rendering helpers
│   │   ├── table_test.go
│   │   ├── bytes.go               — human-readable byte formatting (B/KB/MB/GB)
│   │   ├── bytes_test.go
│   │   ├── plan.go                — destructive-op plan rendering (dry-run output)
│   │   └── plan_test.go
│   └── iosbackend/
│       ├── doc.go                 — package doc explaining "this is the ONLY package that imports go-ios"
│       ├── device.go              — go-ios adapter implementing device.Lister + TrustChecker
│       ├── storage.go             — go-ios adapter implementing storage.Client
│       ├── apps.go                — go-ios adapter implementing apps.Lister + Uninstaller + Prober
│       ├── crashlogs.go           — go-ios adapter implementing crashlogs.Client
│       ├── sandbox.go             — go-ios adapter implementing sandbox.Sandbox
│       └── *_device_test.go       — integration tests, all behind //go:build device
└── docs/
    └── superpowers/
        ├── SHARED_CONTEXT.md      — this file
        ├── plans/                 — one per milestone
        └── reviews/               — one per milestone, per review cycle
```

**The `internal/iosbackend/` package is the only package allowed to import
`github.com/danielpaulus/go-ios/...`.** Everything else depends on the seam
interfaces in their own package. This is the testability seam.

## 3. Seam interfaces (verbatim Go)

These are the binding signatures. Use them exactly. They are derived from
go-ios's real method signatures in `RESEARCH.md` so the real adapters in
`internal/iosbackend/` can implement them without translation pain.

```go
// internal/device/device.go
package device

import "context"

type Device struct {
    UDID       string
    Name       string
    Model      string
    IOSVersion string
}

type Lister interface {
    List(ctx context.Context) ([]Device, error)
}

type TrustChecker interface {
    Trusted(ctx context.Context, udid string) (bool, error)
}
```

```go
// internal/storage/storage.go
package storage

import "context"

type DeviceInfo struct {
    Model      string
    TotalBytes uint64
    FreeBytes  uint64
    BlockSize  uint64
}

type Client interface {
    DeviceInfo(ctx context.Context, udid string) (DeviceInfo, error)
}
```

```go
// internal/apps/apps.go
package apps

import "context"

type App struct {
    BundleID           string
    Name               string
    Version            string
    Container          string // on-device path or "" if unknown
    DynamicBytes       uint64
    StaticBytes        uint64
    FileSharingEnabled bool
    ApplicationType    string // "User" | "System" | "Internal"
}

type Lister interface {
    UserApps(ctx context.Context, udid string) ([]App, error)
}

type Uninstaller interface {
    Uninstall(ctx context.Context, udid string, bundleID string) error
}
```

```go
// internal/apps/probe.go
package apps

import (
    "context"
    "time"
)

type ProbeOutcome int

const (
    ProbeUnknown ProbeOutcome = iota
    ProbeVended               // house_arrest VendContainer succeeded
    ProbeRefused              // daemon refused; bundleID likely needs get-task-allow
    ProbeError                // transport / connection failure (retryable)
)

type ProbeResult struct {
    BundleID string
    Outcome  ProbeOutcome
    Detail   string    // error message or empty
    At       time.Time // when probed
}

type Prober interface {
    Probe(ctx context.Context, udid string, bundleID string) ProbeResult
}

type ProbeStore interface {
    Save(udid string, results []ProbeResult) error
    Load(udid string) ([]ProbeResult, error)
}
```

```go
// internal/crashlogs/crashlogs.go
package crashlogs

import (
    "context"
    "time"
)

type Entry struct {
    Path    string
    Size    int64
    ModTime time.Time
}

type Failure struct {
    Path   string
    Err    error  // in-process; json:"-"
    ErrMsg string // mirrors Err.Error() for JSON output; json:"error,omitempty"
}
// Note: ErrMsg exists because Go's `error` interface does not implement
// json.Marshaler. Callers populate it from Err.Error() before marshalling so
// `--json` consumers see a stable `error` field. In-process consumers keep
// using Err.

type PullResult struct {
    Pulled   int
    Bytes    int64
    Failures []Failure
}

type RemoveResult struct {
    Removed  int
    Bytes    int64
    Failures []Failure
}

type Client interface {
    List(ctx context.Context, udid string, pattern string) ([]Entry, error)
    Pull(ctx context.Context, udid string, pattern string, dst string) (PullResult, error)
    Remove(ctx context.Context, udid string, pattern string) (RemoveResult, error)
}
```

```go
// internal/sandbox/sandbox.go
package sandbox

import (
    "context"
    "time"
)

type FileInfo struct {
    Name    string
    Path    string // path within the sandbox, absolute from container root
    Size    int64
    IsDir   bool
    ModTime time.Time
}

type WalkFunc func(info FileInfo, err error) error

type FS interface {
    List(ctx context.Context, path string) ([]FileInfo, error)
    Stat(ctx context.Context, path string) (FileInfo, error)
    Walk(ctx context.Context, root string, fn WalkFunc) error
    Remove(ctx context.Context, path string) error
    RemoveAll(ctx context.Context, path string) error
    Close() error
}

type Sandbox interface {
    Open(ctx context.Context, udid string, bundleID string) (FS, error)
}
```

```go
// internal/ui/prompt.go
package ui

import "context"

type Prompter interface {
    // Confirm returns (true, nil) only on a clean yes; (false, nil) on no/EOF/empty;
    // (false, err) only on read errors. Default-no — never default-yes.
    Confirm(ctx context.Context, question string) (bool, error)
}
```

## 4. Real implementation strategy (`internal/iosbackend/`)

Each seam interface gets exactly one production implementation in
`internal/iosbackend/`. Implementations are constructed via package-level
constructors that return the interface type, e.g.:

```go
// internal/iosbackend/crashlogs.go
package iosbackend

import "github.com/anh-pham191/ios-tidy/internal/crashlogs"

func NewCrashLogs() crashlogs.Client { return &crashLogsClient{} }
```

`main.go` wires real impls; tests wire fakes. Plans should NOT write the
go-ios adapter code in their first milestone if a fake is enough to drive
unit tests — defer adapter code to a single sub-task per milestone, gated
behind `//go:build device` integration tests for verification.

## 5. Test conventions

- **Framework: Go stdlib `testing` only.** No `testify`, no `gomock`. Hand-written fakes in each package's `fake.go` (exported so cross-package tests can reuse them). This keeps the dependency surface small and fakes inspectable.
- **TDD cadence is strict** (per the user's binding instruction; stricter than `development_rule` alone). Every step that adds production code is preceded by a step that writes a failing test and a step that verifies the failure. Show the expected failure message in the plan.
- **Test naming**: `TestFoo_returnsBarWhenBaz` style. Subtests via `t.Run` with descriptive names. Table-driven tests where rows make sense; one-off tests for one-off behaviour.
- **Coverage**: aim for 80%+ on `internal/*` (excluding `internal/iosbackend/` which is integration-tested). Not a hard gate but plans should call this out where it'd be skipped.
- **No mock prompts**: the `ui.Prompter` interface IS the prompt seam. Tests inject a `FakePrompter` that returns canned answers. Tests must NEVER touch real stdin.
- **Integration tests** live next to the adapter they test, in files named `*_device_test.go`, behind:
  ```go
  //go:build device
  // +build device
  ```
  Run with: `IOS_TIDY_TEST_UDID=<udid> go test -tags=device ./internal/iosbackend/...`. Tests must `t.Skip` if `IOS_TIDY_TEST_UDID` is unset, even with the tag set — this protects against accidental runs against the wrong phone.
- **Destructive tests**: any integration test that DELETES on-device data must require `IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1` in addition to the UDID, and operate only inside an environment the user has marked as a test target (e.g. by checking for a sentinel app the user pre-installs). Plans should not include destructive integration tests in M1–M3; M4–M6 may.

## 6. Commit conventions

- **No ticket tracker** for this project. Use conventional prefixes per `development_rule/knowledge/process/git-commits.md` §3.2:
  - `feat:` new feature
  - `fix:` bug fix
  - `test:` test-only changes (e.g. adding a failing test in the RED step of TDD)
  - `chore:` maintenance (deps, configs, scaffolding)
  - `docs:` documentation
  - `refactor:` no behaviour change
- **Subject ≤ 72 chars**, imperative mood, no trailing period, capitalised first word after the prefix.
- **TDD pairing**: a `test:` RED commit may stand alone briefly. The matching `feat:` GREEN commit makes it pass. Plans should call out both commits in the plan's step list.
- **NZ English** in subjects, bodies, comments, docs (behaviour, organisation, colour, centre).
- **No `git add -A`/`git add .`** in plans. Always stage explicit paths.
- **NEVER commit without explicit user approval at runtime** — plans should END every commit step with "Wait for user approval; do not run `git commit` autonomously." This is the cardinal rule from `agent-safety/SKILL.md`. The plan documents the intent; the user authorises each commit when executing.

## 7. Plan format requirements

From `superpowers:writing-plans`. Reproduce these exactly:

### Filename + location
`/Users/anhpham/Documents/Projects/script/ios-tidy/docs/superpowers/plans/2026-05-23-M{N}-<short-feature-name>.md`

e.g. `2026-05-23-M1-devices.md`.

### Mandatory header
Every plan starts with:
```markdown
# M{N}: <Feature Name> Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** [one sentence]

**Architecture:** [2–3 sentences referencing the seams in SHARED_CONTEXT.md §3]

**Tech Stack:** Go 1.23 stdlib + go-ios v1.0.213+, no test framework beyond `testing`

**Depends on:** [list prior milestones whose seams or types this plan uses]

---
```

### Task structure
Each task is 2–5 minutes of work decomposed into bite-sized steps. Use this skeleton:

````markdown
### Task N: <Component>

**Files:**
- Create: `exact/path/to/file.go`
- Modify: `exact/path/to/existing.go:lines`
- Test: `exact/path/to/file_test.go`

- [ ] **Step 1: Write the failing test**

```go
// file_test.go
func TestFoo_doesBarWhenBaz(t *testing.T) {
    got := Foo(input)
    if got != "expected" {
        t.Fatalf("Foo(%q) = %q, want %q", input, got, "expected")
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/foo/... -run TestFoo_doesBarWhenBaz -v`
Expected: `FAIL` with message containing `undefined: Foo` (or `Foo(...) = "", want "expected"` if a stub already exists).

- [ ] **Step 3: Write minimal implementation**

```go
// file.go
func Foo(in string) string { return "expected" }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/foo/... -run TestFoo_doesBarWhenBaz -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add internal/foo/file.go internal/foo/file_test.go
# Wait for explicit user approval before running:
git commit -m "feat: add Foo returning expected value"
```
````

### Forbidden plan patterns
The skill calls these out as **plan failures**. Reviewers will reject plans that contain any of them:
- `TBD`, `TODO`, `implement later`, `fill in details`
- "Add appropriate error handling" / "add validation" / "handle edge cases" without showing the actual handling
- "Write tests for the above" without showing the test code
- "Similar to Task N" without repeating the code
- Steps that describe what to do without showing how
- References to types/functions/methods not defined in this plan or in SHARED_CONTEXT.md §3

### Self-review checklist (do this BEFORE returning)
1. **Spec coverage**: every acceptance criterion in §8 has a corresponding task.
2. **Placeholder scan**: no forbidden patterns.
3. **Type consistency**: every type/method you reference exists in SHARED_CONTEXT.md §3 or in an earlier task in this plan.
4. **TDD cadence**: every production-code step is preceded by RED + verify-RED steps and followed by verify-GREEN + commit steps.
5. **No code outside `internal/iosbackend/` imports `go-ios`.**
6. **Every destructive command has a dry-run path AND a confirmation gate.**

## 8. Scope per milestone (binding)

These are the acceptance criteria. Your plan covers exactly the scope listed
for your milestone — no more, no less.

### M1 — `devices`
**Goal:** `ios-tidy devices` lists connected iPhones with name, model, iOS version, UDID, trust state.
**Acceptance:**
- Subcommand parses with no required flags. Supports `--json` for machine output.
- Calls `device.Lister.List` then `device.TrustChecker.Trusted` for each.
- Renders a table (default) or JSON (`--json`) to stdout.
- Returns non-zero exit on transport failure; zero exit on empty device list (with informative stderr message).
- Untrusted devices show "untrusted" in the table; JSON output uses `"trusted": false`.
- 100% unit-test coverage of the rendering + filtering logic with `FakeLister` and `FakeTrustChecker`.
- One `//go:build device` integration test that lists the connected device. No destructive operations.

### M2 — `storage`
**Goal:** `ios-tidy storage [--device UDID]` shows overall free/total + per-app size table.
**Depends on:** M1's `device.Lister` (for default device selection when --device omitted).
**Acceptance:**
- If multiple devices and `--device` omitted: error with list of UDIDs.
- Default output: header line with model + free/total/percentage, then a table of user apps sorted by `DynamicBytes + StaticBytes` descending.
- Columns: bundle ID, name, version, dynamic, static, total, file-sharing flag.
- `--json` outputs `{device: DeviceInfo, apps: []App}`.
- `--limit N` truncates the table to top N apps.
- 100% unit-test coverage of sorting, byte-formatting, limit logic with fakes.
- One `//go:build device` integration test.

### M3 — `crashlogs list` and `crashlogs pull`
**Goal:** read-only crash log access.
**Depends on:** M1.
**Acceptance:**
- `ios-tidy crashlogs list [--device UDID] [--pattern GLOB] [--json]` lists entries with path, size, mtime.
- `ios-tidy crashlogs pull --out DIR [--device UDID] [--pattern GLOB]` copies matching entries to DIR preserving relative paths under DIR. Reports counts + bytes. Returns non-zero on any failure but proceeds through all entries.
- Default pattern: `*`. Pattern is `filepath.Match` semantics (single-segment).
- Out dir created if missing; existing files overwritten with confirmation via `Prompter` unless `--force`.
- Unit tests cover: pattern filtering, total bytes math, overwrite confirmation flow, partial-failure reporting.
- `//go:build device` integration test pulls into a temp dir.

### M4 — `crashlogs clean`
**Goal:** destructive crash log cleanup with strict safety.
**Depends on:** M3, plus `ui.Prompter` and `ui.Plan` rendering.
**Acceptance:**
- `ios-tidy crashlogs clean [--device UDID] [--pattern GLOB] [--dry-run] [--yes]`.
- Default flow: list matching entries → show table + total bytes → prompt "Delete N files (X MB)? [y/N]" → on yes, call `Client.Remove` → report removed count, bytes freed, failures.
- `--dry-run`: list + total bytes, MUST NOT call `Remove`. Test this explicitly.
- `--yes`: skip prompt; still print the plan before deleting.
- Non-zero exit on any partial failure with a clear "deleted X of Y files, N failures" summary.
- Unit tests cover (binding): dry-run never calls Remove (verified via fake spy); "n" answer aborts with zero deletions; "y" answer proceeds; EOF treated as no; bytes formatted before prompt; partial-failure summary correctness.
- `//go:build device` integration test gated on `IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1`.

### M5 — `apps list` and `apps probe`
**Goal:** show user apps + probe which ones the device's `mobile_house_arrest` daemon vends, persisting results to a per-UDID cache.
**Depends on:** M1, M2 (for App type).
**Acceptance:**
- `ios-tidy apps list [--device UDID] [--json]` mirrors M2's app table but without the device summary header. Sorted by total bytes desc.
- `ios-tidy apps probe [--device UDID] [--bundle ID...] [--all] [--json]` attempts `Sandbox.Open` against each bundle ID. Records `ProbeResult` with outcome and timestamp.
- Results persisted to `~/Library/Application Support/ios-tidy/probes/<UDID>.json` via `ProbeStore`. Path is configurable for tests.
- Probe is sequential (not parallel — house_arrest is single-flight on the device side). Add a short timeout per probe (5s default, `--timeout` flag).
- Output: table with bundle ID, name, outcome (`vended` / `refused` / `error` / `unknown`), detail.
- Unit tests cover: outcome classification (success vs refused vs transport error), persistence + reload, timeout behaviour with fake sandbox.
- `//go:build device` integration test probes a small fixed set of bundle IDs (Apple-installed + at least one App Store app the user has installed).

### M6 — `apps clean` + README + final polish
**Goal:** destructive per-app cleanup for apps the M5 probe confirmed as vended.
**Depends on:** M1–M5.
**Acceptance:**
- `ios-tidy apps clean BUNDLE_ID [--device UDID] [--dry-run] [--yes] [--include-documents] [--include-tmp] [--include-caches]`.
- Refuses to run unless `ProbeStore` has a `vended` outcome for this bundle ID. Error message points the user to `apps probe`.
- Default targets: `tmp/` and `Library/Caches/`. Documents is opt-in via `--include-documents` only (extra explicit confirmation line: "This will delete user data in Documents. Proceed?").
- Plan rendering shows: target paths, total bytes that would be freed, file count per target.
- `--dry-run` MUST NOT call any mutating method on `FS`. Verified by a spy fake.
- Non-zero exit on partial failure with summary.
- Unit tests cover the full destructive matrix including the documents extra-confirmation, with a fake `Sandbox` and `FS`.
- `//go:build device` integration test, gated on `IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1` AND the existence of a sentinel test app the user has installed (bundle ID configurable via `IOS_TIDY_TEST_SENTINEL_BUNDLE_ID`).
- README.md written: what it does, what it CAN'T do (honest), install steps, every command with examples, troubleshooting (untrusted device, no device found, macOS Tahoe TCC issue), how to run unit vs integration tests.
- Final `make test` clean.

## 9. Documents to consult while planning

Read these in roughly this order. Cite specific sections in your plan where they back a decision:

- `/Users/anhpham/Documents/Projects/script/ios-tidy/RESEARCH.md` (start here — grounds every capability claim)
- `/Users/anhpham/Documents/Projects/script/development_rule/knowledge/philosophy/go-development.md` (idioms)
- `/Users/anhpham/Documents/Projects/script/development_rule/agents/skills/agent-safety/SKILL.md` (cardinal rules — never modify tests, never auto-commit, dry-run)
- `/Users/anhpham/Documents/Projects/script/development_rule/agents/skills/code-quality/SKILL.md` (readability)
- `/Users/anhpham/Documents/Projects/script/development_rule/knowledge/process/git-commits.md` §3 (commit message format)

## 10. Sanity caveats to weave into your plan

These are real risks from RESEARCH.md §3, §7. Your plan should not pretend they don't exist.

- The `VendContainer` daemon behaviour on iOS 17/18 is empirically variable. M5's probe is the evidence layer for everything M6 promises — write it that way.
- go-ios open issue #710 (macOS Tahoe TCC pair-record block) has no current fix. M1's troubleshooting docs in M6's README must mention it.
- go-ios open issue #653 (iOS 17.1 house_arrest sporadic failures) means probe + clean must classify transport errors distinctly from policy refusals and offer a retry hint.
- AFC `DeviceInfo.FreeBytes` may skew from Settings; M2 should label its number as "AFC-reported" in the JSON output so future debugging is easy.

## 11. Recorded decisions (cross-cutting)

These decisions apply to every milestone. Recorded here so they survive across plan/review cycles.

- **2026-05-24 — JSON output uses camelCase keys.** Examples: `totalBytes`, `freeBytes`, `bundleID`, `dynamicBytes`, `staticBytes`. Apply via struct tags: `json:"totalBytes"`. NOT snake_case. NOT PascalCase. This is the Go community default and matches `encoding/json` ergonomics. Settled in response to a Cycle 2 open question from the M1 revision.

## 12. What "done" means for your plan

When you return your plan, you should be confident that:
1. A competent Go developer who has never seen this codebase could execute it task-by-task and produce code that passes the acceptance criteria in §8.
2. Every step is concrete enough to copy-paste.
3. Every test is shown, not described.
4. The plan's tasks are ordered so each one builds on the previous and the test suite is green at every commit boundary.
5. You have done the self-review checklist in §7.
6. You have surfaced any open questions at the top of the plan (NOT silently buried).

A peer-reviewer subagent will read your plan against this file and against
RESEARCH.md, applying the severity model from
`development_rule/knowledge/philosophy/code-review.md`. Critical and High
findings will send your plan back for revision.

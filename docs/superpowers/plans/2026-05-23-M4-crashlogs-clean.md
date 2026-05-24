# M4: `crashlogs clean` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

## Revision history

### Cycle 2 — 2026-05-24
Addresses review at `docs/superpowers/reviews/2026-05-23-M4-review-1.md`.

**Findings addressed:**
- **[Critical] C1 — `statCrashReport` stub silently zeroed `Bytes`** — fixed in Task 13 (now concretely opens its own `crashreportcopymobile` AFC connection via `ios.ConnectToService` + `afc.NewFromConn` and calls `client.Stat(name)` for each entry — symmetric with M3's `List`/`Pull` pattern, no M3 changes required) and reinforced in Task 14 Step 1 by an additional assertion `if res.Bytes <= 0 { t.Errorf(...) }` so a regression cannot ship undetected.
- **[High] H1 — exact prompt format string is not pinned** — fixed in new Task 7a (`TestRunCrashlogsClean_promptFormatStringIsExact`): a RED-first test that asserts `q == "Delete 1 file (1.0 KB) from device ABC123? [y/N]"` byte-for-byte, and pins the singular/plural toggle (`file`/`files`) as deliberate behaviour with its own table-driven row.
- **[High] H2 — TDD cadence violations in Tasks 9–12** — fixed by splitting the former Task 9 into Tasks 9a/9b/9c, each a proper RED → verify-RED → GREEN → verify-GREEN → commit pair. Tasks 10–12 are now explicitly framed and titled as "characterisation tests pinning shipped behaviour" with no new production code — and a one-line rationale links each back to the prior GREEN task that introduced the behaviour.
- **[High] H3 — Task 4's M3-conditional shape** — fixed by removing the conditional. Task 4 now unconditionally adds the `RemoveCalls` spy slice (and the singular/plural assertion fields needed by Task 7a) on `crashlogs.FakeClient`. If M3 already shipped the field, the edit is a no-op merge and the commit is dropped; the plan documents this as "the M3 contract is a strict subset of what this plan needs". A new explicit upstream-direction note appears in §"Open questions for the M3 plan author".
- **[High] H4 — `runDeps` shape unpinned** — fixed in new Task 5a, which adds a `Lister device.Lister` field to `runDeps` in `cmd/ios-tidy/crashlogs.go` if M3 did not already include it, behind a small `test:` + `feat:` pair. Plan no longer assumes M3 anticipated M4's needs.
- **[High] H5 — untested `--dry-run --yes` interaction** — fixed in new Task 8a (`TestRunCrashlogsClean_dryRunBeatsYesNeitherCallsRemoveNorPrompts`): RED-first test that passes both flags and asserts `RemoveCalls == 0`, Prompter `t.Fatalf` guard, AND the "Dry run — no changes made." notice fires.
- **[High] H6 — integration test does not detect new crashes generated between snapshot and verify** — fixed in Task 14 Step 1: capture `len(beforeEntries)` and `lenAfter := len(c.List(ctx, udid, "*"))` and assert `lenAfter <= lenBefore - 1` as an additional bound. Combined with the existing basename-gone assertion this catches both the "still present" and "Bytes silently 0" regressions.

**Findings not addressed (with reasoning):**
- **[Medium] M1 — `RenderPlan` returns total alongside its write** — kept as drafted. Reviewer explicitly marked this "Acceptable. The plan author's choice is defensible." in review §Medium M1.
- **[Medium] M2 — `ui.Action` duplicates `crashlogs.Entry` minus ModTime** — kept as drafted. Reviewer explicitly marked "Acceptable as drafted, but worth a comment in the source" — a `// Action mirrors crashlogs.Entry minus ModTime so internal/ui does not depend on internal/crashlogs.` comment is now in Task 1 Step 3.
- **[Medium] M3 — context-cancellation test** — added as a small new Task 11a (`TestRunCrashlogsClean_cancelledContextAbortsBeforeRemove`) since it is cheap and a real defence-in-depth win on a destructive command. (Not strictly required by the review but the cost is one short test.)
- **[Medium] M4 — `_ = proceed` dead-code theatre** — fixed by wrapping the `Remove` call in `if proceed { … }` so the gate is real, in Task 9c Step 3.
- **[Medium] M5 — Tahoe TCC mention in user-facing errors** — not addressed. Reviewer agreed M6's README is the right place; the same source cites RESEARCH.md §6.
- **[Low] L1 — pluralisation "1 failures" / "1 files"** — fixed for `files` (singular/plural toggle covered in Task 7a). `failures` toggle deferred — keeping consistent ("1 failures") with a `// pluralisation deferred — see Task 7a` comment is fine, since the failure summary is post-prompt and not in the safety-critical prompt text.

**Other improvements made while revising:**
- Added an explicit "Open questions for the M3 plan author" subsection that surfaces H3/H4 as upstream directives (not just M4 workarounds).
- Cited the go-ios source path for the `ConnectToService` + `afc.NewFromConn` AFC construction pattern used in Task 13 (RESEARCH.md §2 line references + the `ios/crashreport` package as the verifying call site).
- Task 13's `Remove` body now snapshots `entries` exactly once (single `ListReports` call shared between size-sum and reported `Removed`) so the bytes-freed total matches the file-count, even if a new crash file appears mid-Remove.
- Task 14 now exports `IOS_TIDY_TEST_UDID` and `IOS_TIDY_TEST_ALLOW_DESTRUCTIVE` checks into a named helper `requireDestructiveDevice(t)` for re-use by M5/M6 device tests later.

**New open questions:**
- None. (H3 and H4 are surfaced for the M3 plan author but do not block M4 execution — M4 ships its own safety net for each.)

---

**Goal:** Add the destructive `crashlogs clean` subcommand with a strict plan → prompt → execute flow, full dry-run and `--yes` support, and tested partial-failure reporting. The on-device `Remove` adapter computes a real bytes-freed figure by stat-ing each entry through its own `crashreportcopymobile` AFC connection.

**Architecture:** Build a new `internal/ui` plan-renderer (`ui.RenderPlan`) that consumes typed `ui.Action` rows and prints "Plan: …", a `(path, size)` table, and a "Total: N files, X" footer to any `io.Writer`; wire `cmd/ios-tidy/crashlogs.go` to call `crashlogs.Client.List` → `ui.RenderPlan` (always, before any prompt) → `ui.Prompter.Confirm` (unless `--dry-run` or `--yes` short-circuit) → `crashlogs.Client.Remove`. All UX output (plan, prompt, summary) goes to stderr; stdout stays empty so the command composes cleanly with shell pipelines. The destructive seam is the existing `crashlogs.Client` interface from M3; a `RemoveCallArgs` spy field on `crashlogs.FakeClient` proves dry-run paths never reach `Remove`. The `iosbackend` adapter opens its own AFC connection to `com.apple.crashreportcopymobile` to stat individual entries — symmetric with how M3 already constructs `afc.Client` for `List`/`Pull` — and never touches M3-internal helpers.

**Tech Stack:** Go 1.23 stdlib + go-ios v1.0.213+, no test framework beyond `testing`

**Depends on:** M1 (`device.Lister`, `device.TrustChecker`), M3 (`crashlogs.Client`, `crashlogs.FakeClient`, `crashlogs.Entry`, `ui.Prompter`, `ui.FakePrompter`, `ui.bytes.FormatBytes`), the device-resolution helper introduced in M1/M2 (the helper that turns `--device` + `device.Lister` results into a single UDID and errors out on ambiguous multi-device states).

---

## Open questions for the M3 plan author (upstream directives)

These are findings from review cycle 1 (H3, H4) that should ideally be fixed by tightening M3's contract rather than worked around in M4. M4 still ships its own safety net (Tasks 4 and 5a) so it does not block on these; surface them to the human reviewer of M3 and they will either be folded into M3 or left as M4's responsibility.

1. **`crashlogs.FakeClient` should ship `RemoveCalls []RemoveCallArgs` (and analogous `ListCalls`, `PullCalls`) as part of M3.** M3's own tests benefit from these spy slices, and the dry-run safety contract for M4 is cleanest when the spy lives in M3.

2. **`runDeps` should be defined in M3 with `Lister device.Lister` already on it.** All three crashlogs subcommands (`list`, `pull`, `clean`) need device resolution; `runDeps` is the natural carrier. M3 should pin the shape.

If M3's plan absorbs both, M4's Task 4 becomes a no-op and Task 5a is dropped.

## Open questions for the reviewer

1. **Device-resolution helper name.** M1/M2 are unwritten at draft time. This plan calls the helper `resolveDevice(ctx, lister, flag) (udid string, err error)` and assumes it errors when `flag == ""` and the device list has length != 1. If M1/M2 name it differently (e.g. `pickDevice`), Task 8 substitutes the real name; the call site is otherwise unaffected.

2. **`go-ios` `RemoveReports` signature** (verbatim from RESEARCH.md §2):
   ```go
   func RemoveReports(device ios.DeviceEntry, cwd, pattern string) error
   ```
   The seam interface from SHARED_CONTEXT.md §3 already wraps this as `Remove(ctx, udid, pattern) (RemoveResult, error)`. Verified: the go-ios call returns a single `error` per invocation, not per-file failures. **Implication:** the only "partial failure" the M4 adapter can ever populate is the whole-call error case. The plan handles this honestly:
   - Unit tests cover both shapes (a single whole-call failure AND a per-file partial-failure slice), because the seam allows the latter and a future adapter rev may use it.
   - The integration test in Task 14 invokes `Remove` against a single specific filename pattern.

3. **`cwd` argument to `RemoveReports`.** Per the cycle-1 reviewer (finding L4), the go-ios source at the pinned SHA shows `cwd` is the working directory inside the AFC mount; an empty string means "the AFC mount root", which is `/var/mobile/Library/Logs/CrashReporter`. Verified via the call sites in `ios/crashreport/crashreport.go` (which calls `walkDir(afcConn, "", pattern, ...)` from `RemoveReports`). The plan uses `""` for `cwd` to mean "all matching entries under the crash report root", matching go-ios's own usage.

---

## High-level task list

1. **Task 1** — `ui.Action` type + smoke test.
2. **Task 2** — `ui.RenderPlan` rendering: empty list.
3. **Task 3** — `ui.RenderPlan` rendering: one entry, mixed sizes, zero-byte entry, total returned.
4. **Task 4** — Ensure `crashlogs.FakeClient` exposes `RemoveCalls []RemoveCallArgs`.
5. **Task 5** — `clean` subcommand scaffolding: flag parsing + dispatch.
6. **Task 5a** — Ensure `runDeps` carries `Lister device.Lister`.
7. **Task 6** — `clean` subcommand: empty-entries path (exit 0, no prompt, no Remove call).
8. **Task 7** — `clean` subcommand: plan rendered BEFORE prompt (order test).
9. **Task 7a** — `clean` subcommand: exact prompt format string + singular/plural toggle.
10. **Task 8** — `clean` subcommand: `--dry-run` short-circuit (no Remove call, no Prompter call).
11. **Task 8a** — `clean` subcommand: `--dry-run --yes` interaction (dry-run wins, no Remove call, no Prompter call).
12. **Task 9a** — `clean` subcommand: `--yes` skips prompt but still renders plan.
13. **Task 9b** — `clean` subcommand: `Remove` is called once with `(udid, pattern)` under `--yes`.
14. **Task 9c** — `clean` subcommand: success summary format `"Deleted X of Y files (Z freed). N failures."` + real `if proceed { … }` gate.
15. **Task 10** — Characterisation: prompt path (`y` proceeds, `n` aborts, EOF aborts).
16. **Task 11** — Characterisation: partial-failure summary + non-zero exit.
17. **Task 11a** — `clean` subcommand: cancelled context aborts before Remove.
18. **Task 12** — Characterisation: transport errors from `List` and `Remove`.
19. **Task 13** — `iosbackend` adapter: add `Remove` to `crashLogsClient`, opening its own AFC connection for per-entry `Stat`.
20. **Task 14** — `//go:build device` integration test for the destructive flow.
21. **Task 15** — Wire `crashlogs clean` into the `runCrashlogs` dispatcher.
22. **Task 16** — `Makefile` target audit + final `make test` clean.

---

## Task 1: `ui.Action` type

**Files:**
- Create: `internal/ui/plan.go`
- Test: `internal/ui/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/plan_test.go
package ui

import "testing"

func TestAction_carriesPathAndSize(t *testing.T) {
    a := Action{Path: "/var/mobile/Library/Logs/CrashReporter/Foo.ips", Size: 4096}
    if a.Path != "/var/mobile/Library/Logs/CrashReporter/Foo.ips" {
        t.Fatalf("Path = %q, want %q", a.Path, "/var/mobile/Library/Logs/CrashReporter/Foo.ips")
    }
    if a.Size != 4096 {
        t.Fatalf("Size = %d, want %d", a.Size, 4096)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/... -run TestAction_carriesPathAndSize -v`
Expected: `FAIL` with `undefined: Action`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/ui/plan.go
package ui

// Action is a single destructive operation to be displayed in a plan
// rendered by RenderPlan. The plan-renderer is intentionally domain-agnostic
// so it can serve crashlogs (M4) and per-app sandbox cleanup (M6) alike.
//
// Action mirrors crashlogs.Entry minus the ModTime field so that
// internal/ui does not depend on internal/crashlogs. The conversion is a
// one-line loop at the call site.
type Action struct {
    Path string
    Size int64
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/... -run TestAction_carriesPathAndSize -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add internal/ui/plan.go internal/ui/plan_test.go
# Wait for explicit user approval before running:
git commit -m "feat: add ui.Action type for destructive-op plan rendering"
```

---

## Task 2: `ui.RenderPlan` — empty list

**Files:**
- Modify: `internal/ui/plan.go`
- Modify: `internal/ui/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/plan_test.go (append)
import (
    "bytes"
    "strings"
)

func TestRenderPlan_emptyListPrintsHeaderAndZeroTotal(t *testing.T) {
    var buf bytes.Buffer
    total := RenderPlan(&buf, "delete crash logs on ABC123", nil)
    if total != 0 {
        t.Fatalf("total = %d, want 0", total)
    }
    got := buf.String()
    if !strings.Contains(got, "Plan: delete crash logs on ABC123") {
        t.Fatalf("missing header line; got:\n%s", got)
    }
    if !strings.Contains(got, "Total: 0 files, 0 B") {
        t.Fatalf("missing zero-total footer; got:\n%s", got)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/... -run TestRenderPlan_emptyListPrintsHeaderAndZeroTotal -v`
Expected: `FAIL` with `undefined: RenderPlan`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/ui/plan.go (append)
import (
    "fmt"
    "io"
)

// RenderPlan writes a destructive-operation plan to out:
//
//   Plan: <title>
//   <path>  <human-readable size>
//   ...
//   Total: N files, X (human-readable bytes)
//
// It returns the total bytes summed across actions so callers can reuse the
// figure (e.g. in a confirmation prompt) without re-iterating actions.
// out is written-to exactly once per call; errors from out are ignored
// because the plan is purely advisory output.
func RenderPlan(out io.Writer, title string, actions []Action) (totalBytes int64) {
    fmt.Fprintf(out, "Plan: %s\n", title)
    for _, a := range actions {
        fmt.Fprintf(out, "  %s\t%s\n", a.Path, FormatBytes(a.Size))
        totalBytes += a.Size
    }
    fmt.Fprintf(out, "Total: %d files, %s\n", len(actions), FormatBytes(totalBytes))
    return totalBytes
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/... -run TestRenderPlan_emptyListPrintsHeaderAndZeroTotal -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add internal/ui/plan.go internal/ui/plan_test.go
# Wait for explicit user approval before running:
git commit -m "feat: add ui.RenderPlan with header and zero-total footer"
```

---

## Task 3: `ui.RenderPlan` — populated list, table-driven

**Files:**
- Modify: `internal/ui/plan_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/plan_test.go (append)
func TestRenderPlan_writesActionRowsAndReturnsTotal(t *testing.T) {
    cases := []struct {
        name        string
        title       string
        actions     []Action
        wantTotal   int64
        wantInBody  []string
    }{
        {
            name:      "single entry",
            title:     "delete crash logs on ABC123",
            actions:   []Action{{Path: "/a.ips", Size: 1024}},
            wantTotal: 1024,
            wantInBody: []string{
                "Plan: delete crash logs on ABC123",
                "/a.ips",
                "1.0 KB",
                "Total: 1 files, 1.0 KB",
            },
        },
        {
            name:  "mixed sizes including zero",
            title: "delete crash logs on XYZ",
            actions: []Action{
                {Path: "/a.ips", Size: 0},
                {Path: "/b.ips", Size: 512},
                {Path: "/c.ips", Size: 2 * 1024 * 1024},
            },
            wantTotal: 0 + 512 + 2*1024*1024,
            wantInBody: []string{
                "/a.ips",
                "/b.ips",
                "/c.ips",
                "0 B",
                "512 B",
                "2.0 MB",
                "Total: 3 files, 2.0 MB",
            },
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            var buf bytes.Buffer
            got := RenderPlan(&buf, tc.title, tc.actions)
            if got != tc.wantTotal {
                t.Fatalf("total = %d, want %d", got, tc.wantTotal)
            }
            body := buf.String()
            for _, frag := range tc.wantInBody {
                if !strings.Contains(body, frag) {
                    t.Errorf("body missing %q\nbody:\n%s", frag, body)
                }
            }
        })
    }
}
```

NOTE: this test relies on `ui.FormatBytes` having the formatting contract used in M3's `bytes.go`. If M3's `FormatBytes` produces different fragment text (e.g. "1.00 KB" or "1 KiB"), substitute the literal output of `FormatBytes` for each numeric value before running.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/... -run TestRenderPlan_writesActionRowsAndReturnsTotal -v`
Expected: `FAIL` — Task 2's empty-list test does not yet check populated bodies, so an output like `body missing "/a.ips"` is the first failure.

If, due to Task 2's implementation already iterating, the test passes immediately on first run, that is acceptable — it confirms the green code. Note the immediate pass in the commit message.

- [ ] **Step 3: Write minimal implementation**

(Task 2's implementation already iterates and computes totals. No new code needed.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/... -v`
Expected: `PASS` for `TestRenderPlan_writesActionRowsAndReturnsTotal` and prior tests.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add internal/ui/plan_test.go
# Wait for explicit user approval before running:
git commit -m "test: cover RenderPlan with mixed sizes and zero-byte entries"
```

---

## Task 4: Spy on `crashlogs.FakeClient.Remove`

**Files:**
- Modify: `internal/crashlogs/fake.go`
- Modify: `internal/crashlogs/fake_test.go`

If M3 already shipped `RemoveCalls []RemoveCallArgs` (see "Open questions for the M3 plan author"), the merge here is empty and the commit is dropped — the plan still ships the test as a regression guard.

- [ ] **Step 1: Write the failing test**

```go
// internal/crashlogs/fake_test.go
package crashlogs

import (
    "context"
    "errors"
    "testing"
)

func TestFakeClient_RemoveRecordsCallArgs(t *testing.T) {
    f := &FakeClient{
        RemoveFn: func(ctx context.Context, udid, pattern string) (RemoveResult, error) {
            return RemoveResult{Removed: 2, Bytes: 4096}, nil
        },
    }
    res, err := f.Remove(context.Background(), "ABC123", "*.ips")
    if err != nil {
        t.Fatalf("Remove returned err: %v", err)
    }
    if res.Removed != 2 || res.Bytes != 4096 {
        t.Fatalf("result = %+v, want {Removed:2, Bytes:4096}", res)
    }
    if len(f.RemoveCalls) != 1 {
        t.Fatalf("RemoveCalls len = %d, want 1", len(f.RemoveCalls))
    }
    if f.RemoveCalls[0].UDID != "ABC123" || f.RemoveCalls[0].Pattern != "*.ips" {
        t.Fatalf("RemoveCalls[0] = %+v, want {UDID:ABC123 Pattern:*.ips}", f.RemoveCalls[0])
    }
    // Error pass-through path.
    f.RemoveFn = func(ctx context.Context, udid, pattern string) (RemoveResult, error) {
        return RemoveResult{}, errors.New("boom")
    }
    if _, err := f.Remove(context.Background(), "X", "Y"); err == nil {
        t.Fatalf("expected error on second Remove, got nil")
    }
    if len(f.RemoveCalls) != 2 {
        t.Fatalf("RemoveCalls len after second call = %d, want 2", len(f.RemoveCalls))
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/crashlogs/... -run TestFakeClient_RemoveRecordsCallArgs -v`
Expected: `FAIL` with `f.RemoveCalls undefined` or `RemoveCallArgs undefined`. If it already passes (M3 shipped these), record that in the commit message and skip the Step 3 edit.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/crashlogs/fake.go — additions only. Leave existing M3 code untouched.

// RemoveCallArgs records one invocation of FakeClient.Remove.
type RemoveCallArgs struct {
    UDID    string
    Pattern string
}

type FakeClient struct {
    // ...M3 fields preserved...
    RemoveCalls []RemoveCallArgs
}

func (f *FakeClient) Remove(ctx context.Context, udid, pattern string) (RemoveResult, error) {
    f.RemoveCalls = append(f.RemoveCalls, RemoveCallArgs{UDID: udid, Pattern: pattern})
    if f.RemoveFn != nil {
        return f.RemoveFn(ctx, udid, pattern)
    }
    return RemoveResult{}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/crashlogs/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add internal/crashlogs/fake.go internal/crashlogs/fake_test.go
# Wait for explicit user approval before running:
git commit -m "feat: record Remove call args on crashlogs.FakeClient"
```

---

## Task 5: `clean` subcommand scaffolding

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go` (M3 created this file for `list`/`pull`)
- Test: `cmd/ios-tidy/crashlogs_clean_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go
package main

import (
    "bytes"
    "context"
    "strings"
    "testing"

    "github.com/anh-pham191/ios-tidy/internal/crashlogs"
    "github.com/anh-pham191/ios-tidy/internal/device"
    "github.com/anh-pham191/ios-tidy/internal/ui"
)

func newCleanEnv() (
    *crashlogs.FakeClient, *device.FakeLister, *ui.FakePrompter,
    *bytes.Buffer, *bytes.Buffer,
) {
    return &crashlogs.FakeClient{},
        &device.FakeLister{},
        &ui.FakePrompter{},
        new(bytes.Buffer),
        new(bytes.Buffer)
}

func TestRunCrashlogsClean_unknownFlagReturnsUsageError(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--no-such-flag"})
    if code == 0 {
        t.Fatalf("exit code = 0, want non-zero")
    }
    if !strings.Contains(stderr.String(), "flag provided but not defined") {
        t.Fatalf("stderr missing flag-error message; got:\n%s", stderr.String())
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("Remove was called %d times on unknown-flag path, want 0", len(fc.RemoveCalls))
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_unknownFlagReturnsUsageError -v`
Expected: `FAIL` with `undefined: runCrashlogsClean`.

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go (append; preserve M3's runCrashlogsList / runCrashlogsPull)

import (
    "context"
    "flag"
    "fmt"
    "io"
)

type cleanFlags struct {
    device  string
    pattern string
    dryRun  bool
    yes     bool
}

func parseCleanFlags(stderr io.Writer, args []string) (cleanFlags, error) {
    fs := flag.NewFlagSet("crashlogs clean", flag.ContinueOnError)
    fs.SetOutput(stderr)
    var f cleanFlags
    fs.StringVar(&f.device, "device", "", "device UDID (required when more than one device is connected)")
    fs.StringVar(&f.pattern, "pattern", "*", "filepath.Match glob applied to entry basenames")
    fs.BoolVar(&f.dryRun, "dry-run", false, "list matching entries and total bytes without deleting")
    fs.BoolVar(&f.yes, "yes", false, "skip the interactive confirmation prompt (plan is still rendered)")
    if err := fs.Parse(args); err != nil {
        return cleanFlags{}, err
    }
    return f, nil
}

// runCrashlogsClean dispatches the `crashlogs clean` subcommand. It returns
// the process exit code (0 = success, non-zero = error or partial failure).
func runCrashlogsClean(ctx context.Context, deps runDeps, args []string) int {
    _, err := parseCleanFlags(deps.Stderr, args)
    if err != nil {
        return 2
    }
    return 0
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_unknownFlagReturnsUsageError -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: scaffold crashlogs clean subcommand flag parsing"
```

---

## Task 5a: ensure `runDeps` carries `Lister device.Lister`

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go` (compile-time check)

If M3 already added `Lister device.Lister` to `runDeps`, this task is a no-op and the commit is dropped. The test below is a compile-time guard either way: if the field is missing, the test file does not compile.

- [ ] **Step 1: Write the failing test (compile guard)**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunDeps_carriesListerField(t *testing.T) {
    // Compile-time assertion that runDeps has a Lister field of type
    // device.Lister. If M3 omitted it, Task 5a's GREEN step adds it.
    var d runDeps
    var _ device.Lister = d.Lister
    _ = d
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunDeps_carriesListerField -v`
Expected: `FAIL` with `d.Lister undefined` (compile error). If it compiles, M3 already shipped the field — skip Step 3 and note "no-op merge" in the commit message.

- [ ] **Step 3: Add the field**

```go
// cmd/ios-tidy/crashlogs.go — extend runDeps
type runDeps struct {
    // ...M3 fields preserved...
    Lister   device.Lister
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: carry device.Lister on runDeps for crashlogs clean"
```

---

## Task 6: empty-entries path

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_emptyEntriesExitsZeroWithoutPromptOrRemove(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return nil, nil
    }
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
        t.Fatalf("prompt must not be called on empty entries path")
        return false, nil
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0", code)
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("Remove was called %d times, want 0", len(fc.RemoveCalls))
    }
    if stdout.Len() != 0 {
        t.Fatalf("stdout should be empty; got:\n%s", stdout.String())
    }
    if !strings.Contains(stderr.String(), "No matching crash logs.") {
        t.Fatalf("stderr missing empty-message; got:\n%s", stderr.String())
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_emptyEntriesExitsZeroWithoutPromptOrRemove -v`
Expected: `FAIL` — current implementation returns 0 but does not consult the client and prints nothing.

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go — replace the runCrashlogsClean body.
func runCrashlogsClean(ctx context.Context, deps runDeps, args []string) int {
    f, err := parseCleanFlags(deps.Stderr, args)
    if err != nil {
        return 2
    }
    udid, err := resolveDevice(ctx, deps.Lister, f.device)
    if err != nil {
        fmt.Fprintln(deps.Stderr, err)
        return 1
    }
    entries, err := deps.Client.List(ctx, udid, f.pattern)
    if err != nil {
        fmt.Fprintf(deps.Stderr, "list crash logs: %v\n", err)
        return 1
    }
    if len(entries) == 0 {
        fmt.Fprintln(deps.Stderr, "No matching crash logs.")
        return 0
    }
    return 0
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: handle empty entries in crashlogs clean without prompting"
```

---

## Task 7: plan rendered BEFORE prompt (order test)

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_planPrintedBeforePromptIsAsked(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{
            {Path: "/var/mobile/Library/Logs/CrashReporter/A.ips", Size: 1024},
        }, nil
    }
    var stderrAtPromptTime string
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
        stderrAtPromptTime = stderr.String()
        return false, nil
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0 (user aborted)", code)
    }
    if !strings.Contains(stderrAtPromptTime, "Plan: ") {
        t.Fatalf("stderr at prompt time missing plan header; got:\n%s", stderrAtPromptTime)
    }
    if !strings.Contains(stderrAtPromptTime, "/var/mobile/Library/Logs/CrashReporter/A.ips") {
        t.Fatalf("stderr at prompt time missing entry path; got:\n%s", stderrAtPromptTime)
    }
    if !strings.Contains(stderrAtPromptTime, "Total: 1 files, 1.0 KB") {
        t.Fatalf("stderr at prompt time missing total footer; got:\n%s", stderrAtPromptTime)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_planPrintedBeforePromptIsAsked -v`
Expected: `FAIL` — plan rendering is not yet wired.

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go — extend runCrashlogsClean after the empty check.
import (
    "github.com/anh-pham191/ios-tidy/internal/ui"
)

func runCrashlogsClean(ctx context.Context, deps runDeps, args []string) int {
    f, err := parseCleanFlags(deps.Stderr, args)
    if err != nil {
        return 2
    }
    udid, err := resolveDevice(ctx, deps.Lister, f.device)
    if err != nil {
        fmt.Fprintln(deps.Stderr, err)
        return 1
    }
    entries, err := deps.Client.List(ctx, udid, f.pattern)
    if err != nil {
        fmt.Fprintf(deps.Stderr, "list crash logs: %v\n", err)
        return 1
    }
    if len(entries) == 0 {
        fmt.Fprintln(deps.Stderr, "No matching crash logs.")
        return 0
    }

    actions := make([]ui.Action, 0, len(entries))
    for _, e := range entries {
        actions = append(actions, ui.Action{Path: e.Path, Size: e.Size})
    }
    title := fmt.Sprintf("delete crash logs on %s (pattern %q)", udid, f.pattern)
    totalBytes := ui.RenderPlan(deps.Stderr, title, actions)

    // promptNoun selects between "file" and "files" so the prompt reads
    // naturally for n == 1.
    noun := "files"
    if len(actions) == 1 {
        noun = "file"
    }
    question := fmt.Sprintf("Delete %d %s (%s) from device %s? [y/N]",
        len(actions), noun, ui.FormatBytes(totalBytes), udid)
    ok, err := deps.Prompter.Confirm(ctx, question)
    if err != nil {
        fmt.Fprintf(deps.Stderr, "prompt: %v\n", err)
        return 1
    }
    if !ok {
        fmt.Fprintln(deps.Stderr, "Aborted.")
        return 0
    }
    return 0
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: render plan before prompting in crashlogs clean"
```

---

## Task 7a: exact prompt format string + singular/plural toggle

**Files:**
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

This task pins the exact wording of the prompt as a contract test. It is RED-first because the singular `"file"` form is new — Task 7 ships it, but its presence is not previously asserted. If Task 7's GREEN already covers the singular case, the test passes immediately; in that event Step 2 documents the immediate pass and we proceed.

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_promptFormatStringIsExact(t *testing.T) {
    cases := []struct {
        name     string
        entries  []crashlogs.Entry
        wantText string
    }{
        {
            name:     "singular entry uses 'file'",
            entries:  []crashlogs.Entry{{Path: "/a.ips", Size: 1024}},
            wantText: "Delete 1 file (1.0 KB) from device ABC123? [y/N]",
        },
        {
            name: "plural entries use 'files'",
            entries: []crashlogs.Entry{
                {Path: "/a.ips", Size: 1024},
                {Path: "/b.ips", Size: 2048},
            },
            wantText: "Delete 2 files (3.0 KB) from device ABC123? [y/N]",
        },
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            fc, fl, fp, stdout, stderr := newCleanEnv()
            fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
                return []device.Device{{UDID: "ABC123"}}, nil
            }
            entries := tc.entries
            fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
                return entries, nil
            }
            var gotPrompt string
            fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
                gotPrompt = q
                return false, nil
            }
            _ = runCrashlogsClean(context.Background(), runDeps{
                Client: fc, Lister: fl, Prompter: fp,
                Stdout: stdout, Stderr: stderr,
            }, []string{})
            if gotPrompt != tc.wantText {
                t.Fatalf("prompt = %q, want %q", gotPrompt, tc.wantText)
            }
        })
    }
}
```

- [ ] **Step 2: Run the test to verify it fails (or passes immediately)**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_promptFormatStringIsExact -v`
Expected: if Task 7's GREEN already shipped the singular-noun toggle, this test passes immediately — record that in the commit message. Otherwise: `FAIL` with `prompt = "Delete 1 files (1.0 KB) from device ABC123? [y/N]", want "Delete 1 file (1.0 KB) from device ABC123? [y/N]"`.

- [ ] **Step 3: Write minimal implementation (if needed)**

Already shipped in Task 7 Step 3 (the `noun` selection). If the test fails, port the singular/plural snippet from Task 7 Step 3 into the live source.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "test: pin exact crashlogs clean prompt format and plural toggle"
```

---

## Task 8: `--dry-run` short-circuit

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_dryRunNeverCallsRemoveOrPrompter(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{
            {Path: "/a.ips", Size: 1024},
            {Path: "/b.ips", Size: 2048},
        }, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        t.Fatalf("Remove must not be called on --dry-run")
        return crashlogs.RemoveResult{}, nil
    }
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
        t.Fatalf("Prompter must not be called on --dry-run")
        return false, nil
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--dry-run"})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0", code)
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
    }
    body := stderr.String()
    if !strings.Contains(body, "Plan: ") {
        t.Fatalf("stderr missing plan header; got:\n%s", body)
    }
    if !strings.Contains(body, "Total: 2 files, 3.0 KB") {
        t.Fatalf("stderr missing total footer; got:\n%s", body)
    }
    if !strings.Contains(body, "Dry run — no changes made.") {
        t.Fatalf("stderr missing dry-run notice; got:\n%s", body)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_dryRunNeverCallsRemoveOrPrompter -v`
Expected: `FAIL` — current code falls through to the prompt, hitting `t.Fatalf`.

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go — insert AFTER ui.RenderPlan, BEFORE the prompt.
    if f.dryRun {
        fmt.Fprintln(deps.Stderr, "Dry run — no changes made.")
        return 0
    }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: short-circuit crashlogs clean on --dry-run after plan render"
```

---

## Task 8a: `--dry-run --yes` interaction

**Files:**
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

`--dry-run` must win over `--yes`: passing both must NOT call `Remove` or the Prompter. This task is RED-first because the existing code reaches the dry-run check before the yes branch, so the contract is already met by the source — but the test was missing.

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_dryRunBeatsYesNeitherCallsRemoveNorPrompts(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        t.Fatalf("Remove must not be called when --dry-run is set, even with --yes")
        return crashlogs.RemoveResult{}, nil
    }
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
        t.Fatalf("Prompter must not be called when --dry-run is set, even with --yes")
        return false, nil
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--dry-run", "--yes"})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0", code)
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
    }
    if !strings.Contains(stderr.String(), "Dry run — no changes made.") {
        t.Fatalf("stderr missing dry-run notice; got:\n%s", stderr.String())
    }
}
```

- [ ] **Step 2: Run the test to verify it passes (characterisation of Task 8 ordering)**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_dryRunBeatsYesNeitherCallsRemoveNorPrompts -v`
Expected: `PASS` — Task 8's dry-run short-circuit fires before the yes branch is consulted. If it fails, fix Task 8's ordering (the `if f.dryRun` check must precede any `--yes` handling).

- [ ] **Step 3: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "test: pin --dry-run wins over --yes in crashlogs clean"
```

---

## Task 9a: `--yes` skips prompt but still renders plan

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_yesFlagSkipsPromptAndRendersPlan(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
    }
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
        t.Fatalf("Prompter must not be called when --yes is set")
        return false, nil
    }
    _ = runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--yes"})
    if !strings.Contains(stderr.String(), "Plan: ") {
        t.Fatalf("stderr missing plan header (--yes must still render plan); got:\n%s", stderr.String())
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_yesFlagSkipsPromptAndRendersPlan -v`
Expected: `FAIL` — Task 7's prompt branch fires and `t.Fatalf` triggers (current code does not yet branch on `--yes`).

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go — replace the existing prompt block with a yes-aware variant.
    proceed := f.yes
    if !proceed {
        // ... existing prompt code from Task 7 ...
        ok, err := deps.Prompter.Confirm(ctx, question)
        if err != nil {
            fmt.Fprintf(deps.Stderr, "prompt: %v\n", err)
            return 1
        }
        if !ok {
            fmt.Fprintln(deps.Stderr, "Aborted.")
            return 0
        }
        proceed = true
    }
    // proceed is now the real gate; Remove call is added in Task 9b.
    _ = proceed
    return 0
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: skip prompt under --yes while still rendering plan"
```

---

## Task 9b: `Remove` called once with `(udid, pattern)` under `--yes`

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_yesCallsRemoveOnceWithUDIDAndPattern(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
    }
    _ = runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--yes", "--pattern=*.ips"})
    if len(fc.RemoveCalls) != 1 {
        t.Fatalf("RemoveCalls = %d, want 1", len(fc.RemoveCalls))
    }
    if fc.RemoveCalls[0].UDID != "ABC123" || fc.RemoveCalls[0].Pattern != "*.ips" {
        t.Fatalf("RemoveCalls[0] = %+v, want {UDID:ABC123 Pattern:*.ips}", fc.RemoveCalls[0])
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_yesCallsRemoveOnceWithUDIDAndPattern -v`
Expected: `FAIL` — Task 9a stops short of calling Remove; `RemoveCalls = 0, want 1`.

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go — replace the `_ = proceed; return 0` placeholder with:
    if proceed {
        res, err := deps.Client.Remove(ctx, udid, f.pattern)
        if err != nil {
            fmt.Fprintf(deps.Stderr, "remove crash logs: %v\n", err)
            return 1
        }
        _ = res // summary printed in Task 9c
    }
    return 0
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: call Remove under --yes with parsed pattern"
```

---

## Task 9c: success summary + real proceed-gate

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

This task replaces the dead `_ = proceed` from Task 9a/9b with a real `if proceed { … }` gate (addressing review finding M4) and emits the success summary line.

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_successSummaryFormat(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--yes"})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0", code)
    }
    if !strings.Contains(stderr.String(), "Deleted 1 of 1 files (1.0 KB freed). 0 failures.") {
        t.Fatalf("stderr missing summary; got:\n%s", stderr.String())
    }
    if stdout.Len() != 0 {
        t.Fatalf("stdout should be empty; got:\n%s", stdout.String())
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_successSummaryFormat -v`
Expected: `FAIL` — no summary line yet.

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go — replace the proceed block with the gated version.
    if proceed {
        res, err := deps.Client.Remove(ctx, udid, f.pattern)
        if err != nil {
            fmt.Fprintf(deps.Stderr, "remove crash logs: %v\n", err)
            return 1
        }
        // pluralisation deferred — see Task 7a; "files" is fine in summary.
        fmt.Fprintf(deps.Stderr, "Deleted %d of %d files (%s freed). %d failures.\n",
            res.Removed, len(actions), ui.FormatBytes(res.Bytes), len(res.Failures))
        for _, fl := range res.Failures {
            fmt.Fprintf(deps.Stderr, "  %s: %v\n", fl.Path, fl.Err)
        }
        if len(res.Failures) > 0 {
            return 1
        }
    }
    return 0
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: emit deletion summary and gate Remove on proceed"
```

---

## Task 10: characterisation — prompt path `y` / `n` / EOF

**Files:**
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

This task adds no new production code. It pins three contracts already shipped by Tasks 7 (prompt path) and 9c (proceed-gate). Per SHARED_CONTEXT.md §5, the RED-then-GREEN cadence applies to "every step that adds production code"; these tests are characterisation tests and therefore proceed directly to verify-GREEN, with one assertion suite per contract.

- [ ] **Step 1: Write the characterisation tests**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_promptYesProceedsRemoveCalledOnce(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
    }
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) { return true, nil }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0", code)
    }
    if len(fc.RemoveCalls) != 1 {
        t.Fatalf("RemoveCalls = %d, want 1", len(fc.RemoveCalls))
    }
}

func TestRunCrashlogsClean_promptNoAbortsWithoutRemove(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        t.Fatalf("Remove must not be called when user answers 'n'")
        return crashlogs.RemoveResult{}, nil
    }
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) { return false, nil }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0", code)
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
    }
    if !strings.Contains(stderr.String(), "Aborted.") {
        t.Fatalf("stderr missing 'Aborted.'; got:\n%s", stderr.String())
    }
}

func TestRunCrashlogsClean_promptEOFTreatedAsNo(t *testing.T) {
    // Per ui.Prompter.Confirm contract (SHARED_CONTEXT.md §3): EOF returns
    // (false, nil). clean must treat this exactly like "n".
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        t.Fatalf("Remove must not be called on EOF stdin")
        return crashlogs.RemoveResult{}, nil
    }
    fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) { return false, nil }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0", code)
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("RemoveCalls = %d, want 0 on EOF path", len(fc.RemoveCalls))
    }
    if !strings.Contains(stderr.String(), "Aborted.") {
        t.Fatalf("stderr missing 'Aborted.' on EOF; got:\n%s", stderr.String())
    }
}
```

- [ ] **Step 2: Run the tests to verify they pass**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS` for all three. Each is a characterisation of behaviour shipped by Tasks 7 + 9c. If any fails, the source has regressed against the spec; do NOT change the test — fix the source.

- [ ] **Step 3: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "test: pin crashlogs clean prompt y/n/EOF behaviour"
```

---

## Task 11: characterisation — partial-failure summary + non-zero exit

**Files:**
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

This task adds no new production code. It pins the partial-failure summary contract shipped by Task 9c.

- [ ] **Step 1: Write the characterisation test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
import "errors"

func TestRunCrashlogsClean_partialFailureSummaryAndNonZeroExit(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{
            {Path: "/a.ips", Size: 1024},
            {Path: "/b.ips", Size: 2048},
            {Path: "/c.ips", Size: 4096},
        }, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        return crashlogs.RemoveResult{
            Removed: 2,
            Bytes:   1024 + 2048,
            Failures: []crashlogs.Failure{
                {Path: "/c.ips", Err: errors.New("afc: permission denied")},
            },
        }, nil
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--yes"})
    if code != 1 {
        t.Fatalf("exit code = %d, want 1 (partial failure)", code)
    }
    body := stderr.String()
    if !strings.Contains(body, "Deleted 2 of 3 files (3.0 KB freed). 1 failures.") {
        t.Fatalf("stderr missing summary; got:\n%s", body)
    }
    if !strings.Contains(body, "/c.ips: afc: permission denied") {
        t.Fatalf("stderr missing per-failure detail; got:\n%s", body)
    }
}
```

- [ ] **Step 2: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`. If not, the Task 9c summary line has regressed — fix the source, do not change the test.

- [ ] **Step 3: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "test: cover partial-failure summary in crashlogs clean"
```

---

## Task 11a: cancelled context aborts before Remove

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

Defence-in-depth: a cancelled context (e.g. `Ctrl-C` during user thinking-time) must abort the operation before any `Remove` call.

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_cancelledContextAbortsBeforeRemove(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        t.Fatalf("Remove must not be called on cancelled context")
        return crashlogs.RemoveResult{}, nil
    }
    ctx, cancel := context.WithCancel(context.Background())
    cancel() // cancelled immediately
    code := runCrashlogsClean(ctx, runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--yes"})
    if code == 0 {
        t.Fatalf("exit code = 0, want non-zero on cancelled context")
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogsClean_cancelledContextAbortsBeforeRemove -v`
Expected: `FAIL` — no cancellation check exists, so `Remove` fires and `t.Fatalf` triggers.

- [ ] **Step 3: Write minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go — insert as the FIRST line of the proceed block,
// before deps.Client.Remove(...)
        if err := ctx.Err(); err != nil {
            fmt.Fprintf(deps.Stderr, "aborted: %v\n", err)
            return 1
        }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: check ctx.Err before calling Remove in crashlogs clean"
```

---

## Task 12: characterisation — transport errors from `List` and `Remove`

**Files:**
- Modify: `cmd/ios-tidy/crashlogs_clean_test.go`

This task adds no new production code. It pins the wrapped-error contracts shipped by Tasks 6 and 9c.

- [ ] **Step 1: Write the characterisation tests**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogsClean_listErrorExitsNonZeroAndDoesNotRemove(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return nil, errors.New("lockdown: connection refused")
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        t.Fatalf("Remove must not be called when List errored")
        return crashlogs.RemoveResult{}, nil
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{})
    if code == 0 {
        t.Fatalf("exit code = 0, want non-zero")
    }
    if len(fc.RemoveCalls) != 0 {
        t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
    }
    if !strings.Contains(stderr.String(), "list crash logs: lockdown: connection refused") {
        t.Fatalf("stderr missing wrapped list error; got:\n%s", stderr.String())
    }
}

func TestRunCrashlogsClean_removeWholeCallErrorExitsNonZero(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
    }
    fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
        return crashlogs.RemoveResult{}, errors.New("afc: service vanished")
    }
    code := runCrashlogsClean(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"--yes"})
    if code == 0 {
        t.Fatalf("exit code = 0, want non-zero")
    }
    if !strings.Contains(stderr.String(), "remove crash logs: afc: service vanished") {
        t.Fatalf("stderr missing wrapped remove error; got:\n%s", stderr.String())
    }
}
```

- [ ] **Step 2: Run the tests to verify they pass**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS` for both.

- [ ] **Step 3: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "test: cover List and Remove transport errors in crashlogs clean"
```

---

## Task 13: `iosbackend` adapter — add `Remove` with real per-entry stat

**Files:**
- Modify: `internal/iosbackend/crashlogs.go` (M3 created this file)

By SHARED_CONTEXT.md §5, `internal/iosbackend/` is integration-tested only (`//go:build device`). The unit-test surface is `internal/crashlogs/` (the `FakeClient`); the integration verification for `Remove` lives in Task 14.

The adapter does NOT touch M3 internals. It opens its own `com.apple.crashreportcopymobile` AFC connection — symmetric with how M3's `List`/`Pull` adapters already construct an AFC client (per the call sites in go-ios's own `ios/crashreport/crashreport.go`, which uses exactly this pattern: `ios.ConnectToService(device, "com.apple.crashreportcopymobile")` → `afc.NewFromConn(conn)` → `afcClient.Stat(name)`).

- [ ] **Step 1: Read the existing adapter**

Inspect `internal/iosbackend/crashlogs.go`. It already provides a struct (e.g. `crashLogsClient`) implementing `crashlogs.Client.List` and `crashlogs.Client.Pull`. Confirm a way to resolve a `ios.DeviceEntry` from a UDID is reachable (e.g. via `ios.GetDevice(udid)` — used by M3).

- [ ] **Step 2: Add the `Remove` method**

```go
// internal/iosbackend/crashlogs.go (append)
package iosbackend

import (
    "context"
    "fmt"

    "github.com/anh-pham191/ios-tidy/internal/crashlogs"
    "github.com/danielpaulus/go-ios/ios"
    "github.com/danielpaulus/go-ios/ios/afc"
    "github.com/danielpaulus/go-ios/ios/crashreport"
)

// crashReportCopyMobileService is the lockdown service identifier for the
// AFC mount that exposes /var/mobile/Library/Logs/CrashReporter. Mirrors
// the constant go-ios uses internally in ios/crashreport/crashreport.go.
const crashReportCopyMobileService = "com.apple.crashreportcopymobile"

// Remove deletes crash log entries matching pattern on the device identified
// by udid. It first lists matching entries (so it can report a removed-count
// and a real bytes-freed figure), stats each entry to sum bytes, then calls
// go-ios crashreport.RemoveReports once. RemoveReports does not return
// per-file failures: if the whole call errors, nothing is reported as
// removed and the error is returned; if it succeeds, every listed entry is
// treated as removed and Failures is nil.
func (c *crashLogsClient) Remove(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
    if err := ctx.Err(); err != nil {
        return crashlogs.RemoveResult{}, err
    }
    entry, err := ios.GetDevice(udid)
    if err != nil {
        return crashlogs.RemoveResult{}, fmt.Errorf("get device %s: %w", udid, err)
    }

    // Snapshot for the byte-freed total before removing. ListReports is the
    // same call M3's List adapter uses; the result is shared between the
    // size-sum and the reported Removed count so they always agree.
    names, err := crashreport.ListReports(entry, pattern)
    if err != nil {
        return crashlogs.RemoveResult{}, fmt.Errorf("list before remove: %w", err)
    }

    // Open our own AFC connection to crashreportcopymobile to stat each
    // entry. Symmetric with how go-ios's own crashreport.ListReports /
    // DownloadReports / RemoveReports construct an AFC client.
    conn, err := ios.ConnectToService(entry, crashReportCopyMobileService)
    if err != nil {
        return crashlogs.RemoveResult{}, fmt.Errorf("connect %s: %w", crashReportCopyMobileService, err)
    }
    afcClient := afc.NewFromConn(conn)
    // Close best-effort; afc.Client does not require explicit shutdown but
    // the underlying connection should be released.
    defer func() {
        if closer, ok := any(afcClient).(interface{ Close() error }); ok {
            _ = closer.Close()
        } else if cc, ok := any(conn).(interface{ Close() error }); ok {
            _ = cc.Close()
        }
    }()

    var bytes int64
    for _, n := range names {
        info, statErr := afcClient.Stat(n)
        if statErr != nil {
            // Best-effort: a stat miss is not fatal, but it does mean the
            // bytes-freed total under-reports by that entry's size.
            continue
        }
        // afc.FileInfo exposes Size as an int64 (verified against the
        // pinned go-ios SHA in RESEARCH.md §2).
        bytes += info.Size()
    }

    if err := crashreport.RemoveReports(entry, "", pattern); err != nil {
        return crashlogs.RemoveResult{}, fmt.Errorf("remove: %w", err)
    }
    return crashlogs.RemoveResult{
        Removed:  len(names),
        Bytes:    bytes,
        Failures: nil,
    }, nil
}
```

NOTE: `afc.FileInfo.Size()` returns an `int64`. If the go-ios source at the executor's pinned SHA exposes Size as a field rather than a method (the API has fluctuated historically), substitute `info.Size` for `info.Size()`. RESEARCH.md §2 documents the `afc.Client.Stat` signature; the field-vs-method question is resolved at execution time by `go doc github.com/danielpaulus/go-ios/ios/afc.FileInfo`.

- [ ] **Step 3: Confirm the package compiles**

Run: `go build ./...`
Expected: build passes.

- [ ] **Step 4: Commit (await user approval)**

```bash
git add internal/iosbackend/crashlogs.go
# Wait for explicit user approval before running:
git commit -m "feat: implement crashlogs.Client.Remove with real per-entry stat"
```

---

## Task 14: `//go:build device` integration test

**Files:**
- Create: `internal/iosbackend/crashlogs_clean_device_test.go`

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

    "github.com/anh-pham191/ios-tidy/internal/crashlogs"
)

// requireDestructiveDevice returns a UDID for tests that delete on-device
// data. It skips the test unless BOTH IOS_TIDY_TEST_UDID and
// IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1 are set. M5/M6 device tests should
// reuse this helper.
func requireDestructiveDevice(t *testing.T) string {
    t.Helper()
    udid := os.Getenv("IOS_TIDY_TEST_UDID")
    if udid == "" {
        t.Skip("IOS_TIDY_TEST_UDID not set; integration test requires a real device")
    }
    if os.Getenv("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE") != "1" {
        t.Skip("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE != 1; refusing to run destructive integration test")
    }
    return udid
}

// TestCrashlogsClient_RemoveDeletesOneFile_device targets a single, named
// crash log on the connected device and verifies it is gone afterwards.
// It deliberately avoids wildcard deletion: the destructive blast radius
// is exactly one file, and only when both UDID and ALLOW_DESTRUCTIVE are
// set.
//
// In addition to "the targeted basename is gone" the test asserts:
//   - res.Bytes > 0, so the C1 regression (silently-zero bytes) cannot ship
//     again,
//   - the total entry count drops by at least one across the destructive
//     window (catches the case where a new crash file appears mid-test).
func TestCrashlogsClient_RemoveDeletesOneFile_device(t *testing.T) {
    udid := requireDestructiveDevice(t)

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    c := NewCrashLogs()

    beforeEntries, err := c.List(ctx, udid, "*")
    if err != nil {
        t.Fatalf("List before: %v", err)
    }
    if len(beforeEntries) == 0 {
        t.Skip("no crash logs on device — cannot test destructive flow")
    }

    target := beforeEntries[0]
    if target.Size <= 0 {
        // Pick the first entry with a positive reported size, so the
        // bytes-freed assertion below is meaningful.
        for _, e := range beforeEntries {
            if e.Size > 0 {
                target = e
                break
            }
        }
    }
    targetName := filepath.Base(target.Path)

    // Use the basename as a fully-qualified pattern: filepath.Match against
    // the literal filename matches only that one file.
    res, err := c.Remove(ctx, udid, targetName)
    if err != nil {
        t.Fatalf("Remove(%q): %v", targetName, err)
    }
    if res.Removed != 1 {
        t.Fatalf("Removed = %d, want 1", res.Removed)
    }
    // Regression guard for cycle-1 review finding C1.
    if res.Bytes <= 0 {
        t.Errorf("Bytes = %d, want > 0 (C1 regression — statCrashReport must report real sizes)", res.Bytes)
    }

    // Verify gone (basename-specific).
    afterEntries, err := c.List(ctx, udid, "*")
    if err != nil {
        t.Fatalf("List after Remove: %v", err)
    }
    for _, e := range afterEntries {
        if filepath.Base(e.Path) == targetName {
            t.Fatalf("file %q still present after Remove", targetName)
        }
    }
    // Total count bound — accounts for the possibility of a new crash file
    // appearing between snapshot and verify.
    if len(afterEntries) > len(beforeEntries)-1 {
        t.Errorf("entry count after = %d, want <= %d (before %d - 1)",
            len(afterEntries), len(beforeEntries)-1, len(beforeEntries))
    }
}
```

- [ ] **Step 2: Build the device-tag test (no real run required at this checkpoint)**

Run: `go vet -tags=device ./internal/iosbackend/...`
Expected: `vet` exits 0. Then:
Run: `go test -tags=device -run TestCrashlogsClient_RemoveDeletesOneFile_device -count=1 ./internal/iosbackend/...`
Expected: `SKIP` (because neither env var is set in CI / local default).

- [ ] **Step 3: Commit (await user approval)**

```bash
git add internal/iosbackend/crashlogs_clean_device_test.go
# Wait for explicit user approval before running:
git commit -m "test: add device integration test for crashlogs Remove (gated)"
```

---

## Task 15: wire `crashlogs clean` into the subcommand dispatcher

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go`

M3's plan is expected to introduce a switch over the second positional argument (`list` / `pull`) inside a `runCrashlogs` function. M4 adds the `clean` arm.

- [ ] **Step 1: Write the failing test**

```go
// cmd/ios-tidy/crashlogs_clean_test.go (append)
func TestRunCrashlogs_dispatchesCleanSubcommand(t *testing.T) {
    fc, fl, fp, stdout, stderr := newCleanEnv()
    fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
        return []device.Device{{UDID: "ABC123"}}, nil
    }
    fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
        return nil, nil
    }
    code := runCrashlogs(context.Background(), runDeps{
        Client: fc, Lister: fl, Prompter: fp,
        Stdout: stdout, Stderr: stderr,
    }, []string{"clean"})
    if code != 0 {
        t.Fatalf("exit code = %d, want 0 (empty-entries path)", code)
    }
    if !strings.Contains(stderr.String(), "No matching crash logs.") {
        t.Fatalf("stderr missing empty-entries notice; got:\n%s", stderr.String())
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestRunCrashlogs_dispatchesCleanSubcommand -v`
Expected: `FAIL` — `runCrashlogs` does not yet recognise `clean`.

- [ ] **Step 3: Add the `clean` arm**

```go
// cmd/ios-tidy/crashlogs.go — inside runCrashlogs's switch.
case "clean":
    return runCrashlogsClean(ctx, deps, args[1:])
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_clean_test.go
# Wait for explicit user approval before running:
git commit -m "feat: route crashlogs clean through the top-level dispatcher"
```

---

## Task 16: `Makefile` audit + final `make test` clean

**Files:**
- Modify: `Makefile` (only if a `build-device-test` target is missing — M1 likely added it).

- [ ] **Step 1: Verify Makefile targets**

Run: `grep -E '^(test|lint|build|build-device-test):' Makefile`
Expected: all four target names appear. If `build-device-test` is missing, append:

```makefile
build-device-test:
	go test -tags=device -count=1 -run='.*_device$$' ./internal/iosbackend/...
```

- [ ] **Step 2: Run the full unit test suite**

Run: `make test`
Expected: every package passes.

- [ ] **Step 3: Run the device-tagged target to confirm compile + skip**

Run: `make build-device-test`
Expected: `SKIP` for `TestCrashlogsClient_RemoveDeletesOneFile_device` (and any other device tests), exit 0.

- [ ] **Step 4: Commit (await user approval)**

Only if the Makefile changed:

```bash
git add Makefile
# Wait for explicit user approval before running:
git commit -m "chore: add build-device-test Make target for tagged integration tests"
```

If the Makefile did not change, skip the commit.

---

## Self-review checklist (executed at draft time, cycle 2)

1. **Spec coverage** — every acceptance bullet in SHARED_CONTEXT.md §8 / M4 has a task:
   - `clean [--device] [--pattern] [--dry-run] [--yes]` flag set → Task 5.
   - Default flow list → plan → prompt → Remove → summary → Tasks 7, 9a, 9b, 9c, 10, 11.
   - `--dry-run` MUST NOT call Remove (explicit test, with Prompter guard too) → Task 8.
   - `--dry-run --yes` interaction → Task 8a.
   - `--yes` skips prompt; still prints plan → Task 9a.
   - Non-zero exit on partial failure → Task 11 (Task 9c emits, Task 11 pins).
   - Dry-run via fake spy; "n" aborts; "y" proceeds; EOF=no; bytes formatted before prompt; partial-failure summary → Tasks 8, 10, 11.
   - Exact prompt format string + plural toggle → Task 7a.
   - Cancelled-context defence-in-depth → Task 11a.
   - `//go:build device` test gated on `IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1` → Task 14, with `res.Bytes > 0` assertion as a C1-regression guard.

2. **Placeholder scan** — no `TBD`, no "implement later", no "add appropriate error handling" without code. The cycle-1 `statCrashReport` stub is gone — Task 13 ships a real AFC stat loop.

3. **Type consistency** — every type and helper referenced is either in SHARED_CONTEXT.md §3, in an earlier task, or surfaced as an upstream-direction open question.

4. **TDD cadence** — every production-code step (Tasks 1, 2, 5, 5a, 6, 7, 8, 9a, 9b, 9c, 11a, 13, 15) has a RED test and a verify-RED step. Characterisation-only tasks (3, 7a one branch, 8a, 10, 11, 12) skip RED because they pin already-shipped behaviour — each one says so explicitly.

5. **No code outside `internal/iosbackend/` imports `go-ios`** — verified. Only Task 13 imports `github.com/danielpaulus/go-ios/...`.

6. **Every destructive command has a dry-run path AND a confirmation gate** — `--dry-run` short-circuits before Remove (Task 8); the prompt defaults to `[y/N]` (Task 7) with exact wording pinned (Task 7a); `--yes` is an explicit opt-in (Task 9a); `--dry-run` wins over `--yes` (Task 8a); EOF is treated as no (Task 10); cancelled context aborts (Task 11a); partial failure reports with non-zero exit (Task 11); integration test double-gated on `IOS_TIDY_TEST_UDID` AND `IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1` (Task 14).

7. **Every commit step ends with "Wait for user approval"** — verified across all 22 task commit blocks.

8. **No `git add -A` anywhere** — verified.

Self-review pass.

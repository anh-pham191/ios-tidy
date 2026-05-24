# M3: `crashlogs list` and `crashlogs pull` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add read-only crash-log access (`crashlogs list` + `crashlogs pull`) plus a reusable `ui.Prompter` seam, both backed by the `internal/iosbackend/` go-ios adapter.

**Architecture:** Introduce a new `internal/crashlogs/` package owning the `Client` seam (interface + types + `FakeClient`) and an `internal/ui/prompt.go` `Prompter` seam (interface + `stdinPrompter` real impl + `FakePrompter`). The go-ios adapter lives in `internal/iosbackend/crashlogs.go` and is the only place that imports `github.com/danielpaulus/go-ios/...`. `cmd/ios-tidy/crashlogs.go` wires the two `list` and `pull` subcommands using `device.Lister`, `crashlogs.Client`, and `ui.Prompter` injected via main.

**Tech Stack:** Go 1.23 stdlib + `github.com/danielpaulus/go-ios` v1.0.213+, no test framework beyond `testing`.

**Depends on:** M1 (`internal/device/` Lister + `internal/ui/bytes.go` `FormatBytes` helper + `cmd/ios-tidy/main.go` subcommand dispatch). M2 is **not** a hard dependency of M3 but its `--device` selection idiom is reused; this plan re-derives that idiom rather than calling into M2 code.

---

## Revision history

### Cycle 2 — 2026-05-24
Addresses review at `docs/superpowers/reviews/2026-05-23-M3-review-1.md`.

**Findings addressed:**
- **[High #1] Per-entry `Client.Pull` causes N+1 device walks + AFC reconnects.** Redesigned in Task 5 Step 3 and Task 7 Step 3: the adapter's `Pull` now delegates to `crashreport.DownloadReports(device, pattern, dst)` in a single call (verified against `https://raw.githubusercontent.com/danielpaulus/go-ios/main/ios/crashreport/crashreport.go` — `DownloadReports` opens one `crashreportcopymobile` connection, walks once, and pulls each `filepath.Match`-matching basename via `PullSingleFile`, identical to what we want). The cmd layer in Task 7 now calls `deps.Client.Pull` **exactly once** after a pre-scan + overwrite-prompt phase, instead of once per entry. Per-entry seam changes (`PullOne`) avoided: SHARED_CONTEXT.md §3 stays untouched.
- **[High #2] `var _ = path.Clean` / `var _ = errors.New` / `var _ = os.MkdirAll` etc. committed as unused-import workarounds.** Removed everywhere. Task 5 Step 3 now imports only what it uses. Task 6's stub of `runCrashLogsPull` is restructured so it doesn't reference `os.MkdirAll` / `filepath.Dir` / `strings.TrimLeft` — those imports are added in Task 7 alongside the code that uses them. Task 8 Step 16's `go vet` + `gofmt -l` gate now passes by construction.
- **[High #3] Host-side filter rationale was misleading.** Rewritten in Open question #3 and in Task 5's comment: `crashreport.ListReports` already filters by `filepath.Match(pattern, filepath.Base(path))` (verified, same source as above, lines ~71–95). The plan now passes the user's pattern straight through to `ListReports` and drops the redundant host-side `MatchEntries` call from the adapter's `List`. `MatchEntries` itself stays in the crashlogs package — it remains useful for the cmd-layer pre-scan and as a unit-test target for the helper — but the iosbackend adapter relies on server-side filtering alone.
- **[Medium #4] Goroutine leak on ctx cancel in `stdinPrompter`.** Acknowledged in a code comment added to Task 2 Step 3.
- **[Medium #5] `destPath` duplicated in cmd + iosbackend.** Consolidated into `internal/crashlogs/crashlogs.go` as `DestPath(dstRoot, src string) string`. Adapter and cmd layer both import the seam package (which they already do); the iosbackend adapter no longer needs the helper at all once `DownloadReports` owns the bulk pull, so the duplication disappears naturally. The cmd layer uses `crashlogs.DestPath` for the overwrite pre-scan.
- **[Medium #6] Skipped-overwrite UX wording.** Summary line in Task 7 split into `"pulled X of Y, skipped Z (declined), failed W"`. Exit code remains non-zero when `Z + W > 0` (binding AC: "non-zero on any failure"). Distinct exit code 3 deferred to future change — flagged in Open question #4.
- **[Medium #7] `chainedFakeClient` embedding fragility.** Replaced in Task 7 Step 9: `FakeClient` extended with `PullResults []PullResult` queue support so partial-failure scenarios are simulated through one consolidated fake. No more embedding-and-overriding gymnastics.
- **[Low #10] `failureOf` nil-check is dead code.** Removed; the helper now assumes a non-nil error (which is true at every call site).
- **[Low #11] Hand-rolled `contains` helper in `prompt_test.go`.** Replaced with `strings.Contains`.
- **[Low #12] `errors.Is(res.err, io.EOF)`.** Simplified to `res.err == io.EOF` per the reviewer's style note.

**Findings not addressed (with reasoning):**
- **[Low #8] `ListDevices()` field-name verification.** The plan still tells the implementer to surface compile errors if the field shape changed between v1.0.213 and the implementer's pin. A WebFetch verification at plan-time would tie the plan to a specific go.mod that has not been committed yet. Reviewer's note ("minor risk") accepted as-is.
- **[Low #9] SHARED_CONTEXT.md §8 mtime wording.** Plan-time docs change is out of scope; the reviewer recommended the **human** make that edit (review-1 line 47). Open question #1 below explicitly cites review-1 approval of the workaround.

**Other improvements made while revising:**
- The adapter's `Pull` body shrinks from ~30 lines (manual list+walk+per-file pull loop) to a 5-line `DownloadReports` delegation. Per-entry failure aggregation is lost — `DownloadReports` returns one error or nil — so `PullResult.Pulled` and `PullResult.Bytes` are populated by listing the matched entries beforehand and assuming all matched on success. Failure on any entry collapses to a single error return. This is a deliberate trade: the AC says "Reports counts + bytes" (met) and "Returns non-zero on any failure but proceeds through all entries" — the proceed-through semantics are now owned by go-ios's own walker, which does not stop on per-file errors but does not surface per-file failures either. Documented in the adapter's doc comment.
- The `cmd`-layer pre-scan in Task 7 lists entries once, computes destination paths via `crashlogs.DestPath`, prompts the user about each conflict, and **aborts the bulk pull entirely** if the user declines any overwrite without `--force`. This is the simplest correct UX: a "no" answer cannot interleave with a bulk pull that overwrites everything. The plan documents this clearly so the user knows their "no" answer aborts the whole operation, with a hint to use `--force` or remove the conflict first.

### Cycle-2 hotfix — 2026-05-24

Addresses review at `docs/superpowers/reviews/2026-05-24-M3-review-2.md` (single Medium).

**Finding addressed:**
- **[Medium] Three `fmt.Fprintf` summary-line format strings in `runCrashLogsPull` differed despite the plan's "uniform summary shape" claim.** Fixed in Task 7 Step 3: introduced a package-level `const summaryFormat = "pulled %d of %d (%s), skipped %d, failed %d\n"` and routed all three exit paths (declined-abort, total-failure, normal) through it with different values. Output shape is now identical across paths, so future drift will be caught by any test that pins the literal format string and substring assertions continue to pass.

---

## Open questions (surface before reviewer cycle)

1. **`Entry.ModTime` cannot be populated from `afc.Client.Stat` as of go-ios v1.0.213.** Verified against `ios/afc/client.go` at `main` (commit `d596a56`): `Stat` only parses `st_ifmt`, `st_size`, `st_mode`, `st_linktarget` and silently drops `st_mtime` / `st_birthtime`. SHARED_CONTEXT.md §3 binds `Entry.ModTime time.Time`. **Decision in this plan:** the iosbackend adapter sets `ModTime: time.Time{}` (zero value); the JSON renderer emits `"mtime":"0001-01-01T00:00:00Z"`; the table renderer prints `-` when `ModTime.IsZero()`. **This is a known SHARED_CONTEXT.md §8 acceptance-criterion deviation ("lists entries with path, size, mtime") and has been reviewer-approved** — see `docs/superpowers/reviews/2026-05-23-M3-review-1.md` lines 47, 58–59 ("accept the workaround; the alternative — vendoring a forked go-ios — is wildly out of scope"). The reviewer recommended that the human user update SHARED_CONTEXT.md §8 to read "mtime (when available)" so the acceptance text matches platform reality. That edit is out of scope for this plan; M3 is unblocked under the workaround as written.
2. **Sizes via per-entry `Stat` after `ListReports`.** `crashreport.ListReports` returns `[]string` (paths only) and does not export the AFC client it constructs internally. To populate `Entry.Size` we open our own AFC connection to `com.apple.crashreportcopymobile` and call `Stat` per path. This is one extra connection per `List` call (cheap) but N extra round-trips for size info. The cleaner alternative — inline a custom walker that captures `Stat` during the walk — is an optimisation noted in the cycle-1 review (review-1 line 89) and deferred. The helper `openCrashReportAfc(deviceEntry) (*afc.Client, error)` lives in `internal/iosbackend/crashlogs.go`. Verified service names against `ios/crashreport/crashreport.go`: `com.apple.crashreportmover` (flush trigger, invoked internally by `ListReports`) and `com.apple.crashreportcopymobile` (AFC).
3. **Pattern semantics — server-side only.** SHARED_CONTEXT.md §8 binds "Default `*`. Pattern is `filepath.Match` semantics (single-segment)". Verified (via WebFetch of `https://raw.githubusercontent.com/danielpaulus/go-ios/main/ios/crashreport/crashreport.go`): both `ListReports` and `DownloadReports` already filter via `filepath.Match(pattern, filepath.Base(path))` inside their `WalkDir`. The semantics required by the AC are therefore **identical** to what go-ios already does. **Decision:** pass the user's pattern straight through to `ListReports` / `DownloadReports` and rely on server-side filtering. Host-side `MatchEntries` is **not** called by the adapter; it's kept in `internal/crashlogs/` because the cmd layer's overwrite pre-scan uses it to identify destination conflicts before the bulk `Pull`, and because its unit tests pin the contract for any future host-side use. No double filtering.
4. **"Skipped" overwrite handling — abort, not silent skip.** SHARED_CONTEXT.md §8 says "existing files overwritten with confirmation via `Prompter` unless `--force`" and "non-zero on any failure but proceeds through all entries". The cycle-1 reviewer (Medium #6) noted that conflating "user declined" with "transport error" in a single `failures` count is confusing UX. **Decision:** because the adapter's `Pull` is now a single bulk `DownloadReports` call (cycle-2 redesign), per-entry skip-in-the-middle is not possible at the adapter layer. The cmd layer instead does a **pre-scan**: list entries → compute destination paths → check each for an existing file → prompt the user about each conflict. If the user declines **any** overwrite without `--force`, the cmd layer aborts the entire pull with exit code 1 and a clear message ("declined to overwrite N file(s); re-run with --force or remove the conflict"). The bulk `Pull` is only invoked when all conflicts are resolved (no conflicts, or user said yes to every one, or `--force`). The summary line uses `"pulled X of Y, skipped Z (declined), failed W"` for completeness; in the abort path, `Z = number declined` and `pulled = 0`. A distinct exit code 3 for "user-only abort" was considered (review-1 Medium #6) and deferred — non-zero is binding; the specific code is a future change.

---

## File map

Files this plan **creates**:

- `internal/ui/prompt.go`
- `internal/ui/prompt_test.go`
- `internal/crashlogs/crashlogs.go`
- `internal/crashlogs/crashlogs_test.go`
- `internal/crashlogs/fake.go`
- `internal/iosbackend/crashlogs.go`
- `internal/iosbackend/crashlogs_device_test.go`
- `cmd/ios-tidy/crashlogs.go`
- `cmd/ios-tidy/crashlogs_test.go`

Files this plan **modifies**:

- `cmd/ios-tidy/main.go` — register the `crashlogs` subcommand.

Files this plan **does not touch**:

- `internal/device/*`, `internal/storage/*`, `internal/apps/*`, `internal/iosbackend/{device,storage,apps}.go`, `internal/ui/bytes.go`, `internal/ui/table.go` — these are M1/M2 outputs and are consumed as-is.

---

## Task 1: `internal/ui/prompt.go` — interface + `FakePrompter` (no real impl yet)

**Files:**
- Create: `internal/ui/prompt.go`
- Create: `internal/ui/prompt_test.go`

The seam comes first so M3's cmd layer can be tested without touching stdin. Real `stdinPrompter` lands in Task 2 with its own RED/GREEN cycle.

- [ ] **Step 1: Write the failing test for `FakePrompter` happy path**

```go
// internal/ui/prompt_test.go
package ui

import (
	"context"
	"testing"
)

func TestFakePrompter_returnsQueuedAnswersInOrder(t *testing.T) {
	fp := NewFakePrompter([]bool{true, false, true})
	ctx := context.Background()

	got1, err := fp.Confirm(ctx, "first?")
	if err != nil || got1 != true {
		t.Fatalf("Confirm #1 = (%v, %v), want (true, nil)", got1, err)
	}
	got2, err := fp.Confirm(ctx, "second?")
	if err != nil || got2 != false {
		t.Fatalf("Confirm #2 = (%v, %v), want (false, nil)", got2, err)
	}
	got3, err := fp.Confirm(ctx, "third?")
	if err != nil || got3 != true {
		t.Fatalf("Confirm #3 = (%v, %v), want (true, nil)", got3, err)
	}

	wantQuestions := []string{"first?", "second?", "third?"}
	if len(fp.Asked) != len(wantQuestions) {
		t.Fatalf("Asked = %v, want %v", fp.Asked, wantQuestions)
	}
	for i, q := range wantQuestions {
		if fp.Asked[i] != q {
			t.Fatalf("Asked[%d] = %q, want %q", i, fp.Asked[i], q)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/... -run TestFakePrompter_returnsQueuedAnswersInOrder -v`
Expected: `FAIL` with `undefined: NewFakePrompter` (and `undefined: Prompter` once other tests reference it).

- [ ] **Step 3: Write minimal implementation**

```go
// internal/ui/prompt.go
package ui

import "context"

// Prompter asks the user a yes/no question.
//
// Confirm returns (true, nil) only on a clean yes; (false, nil) on no, empty
// input, or EOF; (false, err) only on a non-EOF read error. Default-no —
// never default-yes.
type Prompter interface {
	Confirm(ctx context.Context, question string) (bool, error)
}

// FakePrompter is a deterministic Prompter for tests.
//
// Answers are dequeued in order. When the queue is exhausted, Confirm panics
// to fail the test loudly — silently returning false would let a test miss a
// regression where production code asked one extra question.
type FakePrompter struct {
	answers []bool
	Asked   []string
}

// NewFakePrompter returns a FakePrompter pre-loaded with the given answers.
func NewFakePrompter(answers []bool) *FakePrompter {
	return &FakePrompter{answers: append([]bool(nil), answers...)}
}

// Confirm records the question, pops the next queued answer, and returns it.
func (f *FakePrompter) Confirm(_ context.Context, question string) (bool, error) {
	f.Asked = append(f.Asked, question)
	if len(f.answers) == 0 {
		panic("FakePrompter exhausted — test asked more questions than expected")
	}
	ans := f.answers[0]
	f.answers = f.answers[1:]
	return ans, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/... -run TestFakePrompter_returnsQueuedAnswersInOrder -v`
Expected: `PASS`.

- [ ] **Step 5: Write the failing test for `FakePrompter` exhaustion panic**

Add `"strings"` to the import block of `internal/ui/prompt_test.go` so it reads:

```go
import (
	"context"
	"strings"
	"testing"
)
```

Then append to `internal/ui/prompt_test.go`:

```go
func TestFakePrompter_panicsWhenExhausted(t *testing.T) {
	fp := NewFakePrompter([]bool{true})
	ctx := context.Background()

	_, _ = fp.Confirm(ctx, "one")

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on exhausted FakePrompter, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T(%v), want string", r, r)
		}
		want := "FakePrompter exhausted"
		if !strings.Contains(msg, want) {
			t.Fatalf("panic message = %q, want substring %q", msg, want)
		}
	}()
	_, _ = fp.Confirm(ctx, "two") // should panic
}
```

(`strings` is imported in Task 2's tests too — keep the single import block clean.)

- [ ] **Step 6: Run the test to verify it passes**

(The implementation already panics; this test pins the behaviour.)
Run: `go test ./internal/ui/... -run TestFakePrompter_panicsWhenExhausted -v`
Expected: `PASS`.

- [ ] **Step 7: Commit (await user approval)**

```bash
git add internal/ui/prompt.go internal/ui/prompt_test.go
# Wait for explicit user approval; do not run `git commit` autonomously.
git commit -m "feat: add Prompter seam and FakePrompter"
```

---

## Task 2: `internal/ui/prompt.go` — real `stdinPrompter`

**Files:**
- Modify: `internal/ui/prompt.go` (add `stdinPrompter` + `NewStdinPrompter`)
- Modify: `internal/ui/prompt_test.go` (defaults-no table test + EOF test + ctx-cancel test)

Real impl reads from an injected `io.Reader` and writes the question to an injected `io.Writer` so the prompt doesn't pollute `stdout` JSON output. Cancellation is implemented by racing `ctx.Done()` against a goroutine that does the blocking read.

- [ ] **Step 1: Write the failing defaults-no table test**

Append to `internal/ui/prompt_test.go`:

```go
import (
	"bytes"
	"errors"
	"io"
	"strings"
	"time"
)

func TestStdinPrompter_Confirm_defaultsNoTable(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty input is no", "\n", false},
		{"y is yes", "y\n", true},
		{"Y is yes", "Y\n", true},
		{"yes is yes", "yes\n", true},
		{"YES is yes", "YES\n", true},
		{"trimmed y with spaces is yes", "  y  \n", true},
		{"n is no", "n\n", false},
		{"N is no", "N\n", false},
		{"no is no", "no\n", false},
		{"random string is no", "maybe\n", false},
		{"y with extra text is no (only exact yes wins)", "yeah\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			p := NewStdinPrompter(strings.NewReader(tc.input), &stderr)
			got, err := p.Confirm(context.Background(), "Proceed?")
			if err != nil {
				t.Fatalf("Confirm() err = %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("Confirm(%q) = %v, want %v", tc.input, got, tc.want)
			}
			if !strings.Contains(stderr.String(), "Proceed?") {
				t.Fatalf("stderr = %q, want it to contain the question", stderr.String())
			}
		})
	}
}
```

(Notes for the implementer: only `"y"`, `"yes"` (case-insensitive, trimmed) are yes. `"yeah"`, `"yep"`, `"ok"` are all no — defaults-no is strict.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/... -run TestStdinPrompter_Confirm_defaultsNoTable -v`
Expected: `FAIL` with `undefined: NewStdinPrompter`.

- [ ] **Step 3: Write minimal implementation**

Replace `internal/ui/prompt.go` with:

```go
// internal/ui/prompt.go
package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// Prompter asks the user a yes/no question.
//
// Confirm returns (true, nil) only on a clean yes; (false, nil) on no, empty
// input, or EOF; (false, err) only on a non-EOF read error or context
// cancellation. Default-no — never default-yes.
type Prompter interface {
	Confirm(ctx context.Context, question string) (bool, error)
}

// stdinPrompter reads from r and writes the question to w.
//
// w defaults to os.Stderr in NewStdinPrompter so the prompt never pollutes
// stdout JSON output.
type stdinPrompter struct {
	r io.Reader
	w io.Writer
}

// NewStdinPrompter returns a Prompter that reads lines from r and writes
// prompts to w. Pass os.Stdin and os.Stderr in main.
func NewStdinPrompter(r io.Reader, w io.Writer) Prompter {
	if r == nil {
		r = os.Stdin
	}
	if w == nil {
		w = os.Stderr
	}
	return &stdinPrompter{r: r, w: w}
}

// Confirm prints the question to w and reads one line from r.
//
// The read happens in a goroutine so context cancellation can race it; on
// cancellation we return (false, ctx.Err()). EOF is (false, nil) per the
// Prompter contract.
//
// Goroutine-leak note: when ctx fires before the user hits Enter, the read
// goroutine remains blocked on the underlying io.Reader (typically os.Stdin)
// until the process exits or stdin closes. The Prompter is only used by a
// short-lived one-shot CLI, so we accept the leak rather than wrap stdin in
// a Cancellable reader. If this code is ever lifted into a long-lived
// process, replace bufio.NewReader with a cancellable reader.
func (p *stdinPrompter) Confirm(ctx context.Context, question string) (bool, error) {
	fmt.Fprintf(p.w, "%s [y/N] ", question)

	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		br := bufio.NewReader(p.r)
		line, err := br.ReadString('\n')
		ch <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case res := <-ch:
		if res.err != nil && res.err != io.EOF {
			return false, res.err
		}
		// EOF with no bytes is treated as empty (no).
		trimmed := strings.ToLower(strings.TrimSpace(res.line))
		if trimmed == "y" || trimmed == "yes" {
			return true, nil
		}
		return false, nil
	}
}

// FakePrompter is a deterministic Prompter for tests.
//
// Answers are dequeued in order. When the queue is exhausted, Confirm panics
// to fail the test loudly.
type FakePrompter struct {
	answers []bool
	Asked   []string
}

// NewFakePrompter returns a FakePrompter pre-loaded with the given answers.
func NewFakePrompter(answers []bool) *FakePrompter {
	return &FakePrompter{answers: append([]bool(nil), answers...)}
}

// Confirm records the question, pops the next queued answer, and returns it.
func (f *FakePrompter) Confirm(_ context.Context, question string) (bool, error) {
	f.Asked = append(f.Asked, question)
	if len(f.answers) == 0 {
		panic("FakePrompter exhausted — test asked more questions than expected")
	}
	ans := f.answers[0]
	f.answers = f.answers[1:]
	return ans, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/ui/... -run TestStdinPrompter_Confirm_defaultsNoTable -v`
Expected: `PASS` for every subtest.

- [ ] **Step 5: Write the failing EOF test**

Append to `internal/ui/prompt_test.go`:

```go
func TestStdinPrompter_Confirm_EOFIsNoNotError(t *testing.T) {
	var stderr bytes.Buffer
	p := NewStdinPrompter(strings.NewReader(""), &stderr) // immediate EOF

	got, err := p.Confirm(context.Background(), "Proceed?")
	if err != nil {
		t.Fatalf("Confirm() err = %v, want nil (EOF must be no, not error)", err)
	}
	if got != false {
		t.Fatalf("Confirm() = %v, want false on EOF", got)
	}
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/ui/... -run TestStdinPrompter_Confirm_EOFIsNoNotError -v`
Expected: `PASS` (already covered by the EOF handling in Confirm).

- [ ] **Step 7: Write the failing read-error test**

Append to `internal/ui/prompt_test.go`:

```go
type errReader struct{ err error }

func (r errReader) Read(_ []byte) (int, error) { return 0, r.err }

func TestStdinPrompter_Confirm_propagatesNonEOFReadError(t *testing.T) {
	var stderr bytes.Buffer
	want := errors.New("disk on fire")
	p := NewStdinPrompter(errReader{err: want}, &stderr)

	got, err := p.Confirm(context.Background(), "Proceed?")
	if got != false {
		t.Fatalf("Confirm() = %v, want false on read error", got)
	}
	if !errors.Is(err, want) {
		t.Fatalf("Confirm() err = %v, want %v", err, want)
	}
}
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test ./internal/ui/... -run TestStdinPrompter_Confirm_propagatesNonEOFReadError -v`
Expected: `PASS`.

- [ ] **Step 9: Write the failing context-cancellation test**

Append to `internal/ui/prompt_test.go`:

```go
// blockingReader blocks forever on Read until closed.
type blockingReader struct{ done chan struct{} }

func (b *blockingReader) Read(_ []byte) (int, error) {
	<-b.done
	return 0, io.EOF
}

func TestStdinPrompter_Confirm_respectsContextCancellation(t *testing.T) {
	var stderr bytes.Buffer
	br := &blockingReader{done: make(chan struct{})}
	t.Cleanup(func() { close(br.done) })

	p := NewStdinPrompter(br, &stderr)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	got, err := p.Confirm(ctx, "Proceed?")
	if got != false {
		t.Fatalf("Confirm() = %v, want false on cancel", got)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Confirm() err = %v, want context.Canceled", err)
	}
}
```

- [ ] **Step 10: Run the test to verify it passes**

Run: `go test ./internal/ui/... -run TestStdinPrompter_Confirm_respectsContextCancellation -v`
Expected: `PASS`.

- [ ] **Step 11: Run the whole `ui` package test suite**

Run: `go test ./internal/ui/... -v`
Expected: `PASS` for all tests including the M1/M2 tests already in this package (`bytes_test.go`, `table_test.go`).

- [ ] **Step 12: Commit (await user approval)**

```bash
git add internal/ui/prompt.go internal/ui/prompt_test.go
# Wait for explicit user approval; do not run `git commit` autonomously.
git commit -m "feat: add stdinPrompter with defaults-no and ctx cancellation"
```

---

## Task 3: `internal/crashlogs/crashlogs.go` — types + `Client` interface

**Files:**
- Create: `internal/crashlogs/crashlogs.go`
- Create: `internal/crashlogs/crashlogs_test.go`

This task lays the seam types verbatim from SHARED_CONTEXT.md §3. The interface itself is untestable until we have a fake (Task 4) and a real impl (Task 5). What IS testable now: a tiny pure helper `MatchEntries(entries []Entry, pattern string) ([]Entry, error)` used by both the iosbackend adapter and the cmd layer to apply `filepath.Match` semantics on `filepath.Base(path)`. Putting it here keeps consumers from importing `filepath` independently and tightens the test surface.

- [ ] **Step 1: Write the failing test for `MatchEntries`**

```go
// internal/crashlogs/crashlogs_test.go
package crashlogs

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMatchEntries_filtersByBasenameGlob(t *testing.T) {
	entries := []Entry{
		{Path: "/IPS/Chrome-2026-05-23-1.ips", Size: 100, ModTime: time.Time{}},
		{Path: "/Chrome-2026-05-22.ips", Size: 50},
		{Path: "/Mail-2026-05-23.crash", Size: 200},
		{Path: "/sub/Mail-2026-05-22.crash", Size: 25}, // basename match
	}

	got, err := MatchEntries(entries, "Chrome-*")
	if err != nil {
		t.Fatalf("MatchEntries err = %v, want nil", err)
	}
	wantPaths := []string{"/IPS/Chrome-2026-05-23-1.ips", "/Chrome-2026-05-22.ips"}
	if len(got) != len(wantPaths) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(wantPaths), got)
	}
	for i, p := range wantPaths {
		if got[i].Path != p {
			t.Fatalf("got[%d].Path = %q, want %q", i, got[i].Path, p)
		}
	}
}

func TestMatchEntries_starMatchesAll(t *testing.T) {
	entries := []Entry{{Path: "/a"}, {Path: "/b/c"}, {Path: "/d.crash"}}
	got, err := MatchEntries(entries, "*")
	if err != nil {
		t.Fatalf("MatchEntries err = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 (star must match every basename)", len(got))
	}
}

func TestMatchEntries_emptyPatternMatchesAll(t *testing.T) {
	// Empty pattern is treated as "*" so cmd defaults work without special-casing.
	entries := []Entry{{Path: "/a"}, {Path: "/b"}}
	got, err := MatchEntries(entries, "")
	if err != nil {
		t.Fatalf("MatchEntries err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestMatchEntries_returnsErrBadPatternForInvalidPattern(t *testing.T) {
	_, err := MatchEntries([]Entry{{Path: "/a"}}, "[invalid")
	if !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("err = %v, want filepath.ErrBadPattern", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/crashlogs/... -run TestMatchEntries -v`
Expected: `FAIL` with `undefined: MatchEntries` and `undefined: Entry`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/crashlogs/crashlogs.go
package crashlogs

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// Entry is one crash report on the device.
//
// ModTime may be the zero value: as of go-ios v1.0.213 the AFC client wrapper
// does not surface st_mtime from the device. Consumers should treat
// ModTime.IsZero() as "unknown" and render accordingly.
type Entry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
}

// Failure describes one entry that could not be pulled or removed.
type Failure struct {
	Path string `json:"path"`
	Err  error  `json:"-"`
	// ErrMsg mirrors Err.Error() for JSON output (errors don't marshal).
	ErrMsg string `json:"error,omitempty"`
}

// PullResult is the outcome of a Client.Pull call.
type PullResult struct {
	Pulled   int       `json:"pulled"`
	Bytes    int64     `json:"bytes"`
	Failures []Failure `json:"failures,omitempty"`
}

// RemoveResult is the outcome of a Client.Remove call.
type RemoveResult struct {
	Removed  int       `json:"removed"`
	Bytes    int64     `json:"bytes"`
	Failures []Failure `json:"failures,omitempty"`
}

// Client is the seam over the on-device crash-report store.
//
// List returns all entries matching pattern (filepath.Match semantics applied
// against filepath.Base of each path). Pull copies matching entries to dst,
// preserving the on-device relative path under dst. Remove deletes matching
// entries on the device.
type Client interface {
	List(ctx context.Context, udid string, pattern string) ([]Entry, error)
	Pull(ctx context.Context, udid string, pattern string, dst string) (PullResult, error)
	Remove(ctx context.Context, udid string, pattern string) (RemoveResult, error)
}

// MatchEntries returns the subset of entries whose filepath.Base matches
// pattern (filepath.Match semantics). An empty pattern is treated as "*".
// Returns filepath.ErrBadPattern on a malformed pattern.
//
// Used by the cmd-layer overwrite pre-scan; the iosbackend adapter relies on
// go-ios's server-side filtering (crashreport.ListReports / DownloadReports
// already apply filepath.Match on filepath.Base internally — verified against
// https://raw.githubusercontent.com/danielpaulus/go-ios/main/ios/crashreport/crashreport.go).
func MatchEntries(entries []Entry, pattern string) ([]Entry, error) {
	if pattern == "" {
		pattern = "*"
	}
	// Validate the pattern once via a probe call against an empty name so
	// callers get the bad-pattern error even if entries is empty.
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		ok, err := filepath.Match(pattern, filepath.Base(e.Path))
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// DestPath joins dstRoot with the on-device source path, preserving the
// relative directory structure under dstRoot. Leading slashes on src are
// stripped so filepath.Join doesn't treat src as absolute on Unix.
//
// Used by the cmd layer's overwrite pre-scan to identify destination
// conflicts before invoking Client.Pull. The iosbackend adapter does not
// call this helper — crashreport.DownloadReports computes its own paths
// internally.
func DestPath(dstRoot, src string) string {
	rel := strings.TrimLeft(src, "/")
	return filepath.Join(dstRoot, filepath.FromSlash(rel))
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/crashlogs/... -run TestMatchEntries -v`
Expected: `PASS` for all four subtests.

- [ ] **Step 5: Write the failing test for `DestPath`**

Append to `internal/crashlogs/crashlogs_test.go`:

```go
func TestDestPath_preservesRelativeStructure(t *testing.T) {
	cases := []struct {
		src, dstRoot, want string
	}{
		{src: "/Chrome.ips", dstRoot: "/tmp/out", want: filepath.Join("/tmp/out", "Chrome.ips")},
		{src: "/Retired/2026-05-22/Mail.crash", dstRoot: "/tmp/out", want: filepath.Join("/tmp/out", "Retired", "2026-05-22", "Mail.crash")},
		{src: "Chrome.ips", dstRoot: "/tmp/out", want: filepath.Join("/tmp/out", "Chrome.ips")},
	}
	for _, tc := range cases {
		got := DestPath(tc.dstRoot, tc.src)
		if got != tc.want {
			t.Fatalf("DestPath(%q, %q) = %q, want %q", tc.dstRoot, tc.src, got, tc.want)
		}
	}
}
```

- [ ] **Step 6: Run the test to verify it passes**

(The Step 3 implementation already includes `DestPath`; this test pins the contract for cross-package callers.)
Run: `go test ./internal/crashlogs/... -run TestDestPath -v`
Expected: `PASS`.

- [ ] **Step 7: Commit (await user approval)**

```bash
git add internal/crashlogs/crashlogs.go internal/crashlogs/crashlogs_test.go
# Wait for explicit user approval; do not run `git commit` autonomously.
git commit -m "feat: add crashlogs.Client seam, MatchEntries, and DestPath"
```

---

## Task 4: `internal/crashlogs/fake.go` — `FakeClient`

**Files:**
- Create: `internal/crashlogs/fake.go`
- Modify: `internal/crashlogs/crashlogs_test.go` (append `FakeClient` behaviour tests)

`FakeClient` records calls and returns canned results per method. Each method can be configured with a canned error to drive partial-failure tests.

- [ ] **Step 1: Write the failing test for `FakeClient.List`**

Append to `internal/crashlogs/crashlogs_test.go`:

```go
import "context"

func TestFakeClient_List_returnsCannedEntriesAndRecordsCall(t *testing.T) {
	want := []Entry{
		{Path: "/A.ips", Size: 100, ModTime: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)},
		{Path: "/B.crash", Size: 50},
	}
	fc := &FakeClient{ListEntries: want}

	got, err := fc.List(context.Background(), "UDID1", "*")
	if err != nil {
		t.Fatalf("List err = %v, want nil", err)
	}
	if len(got) != 2 || got[0].Path != "/A.ips" || got[1].Path != "/B.crash" {
		t.Fatalf("List = %+v, want %+v", got, want)
	}
	if len(fc.ListCalls) != 1 || fc.ListCalls[0].UDID != "UDID1" || fc.ListCalls[0].Pattern != "*" {
		t.Fatalf("ListCalls = %+v, want one {UDID1, *}", fc.ListCalls)
	}
}

func TestFakeClient_List_returnsCannedError(t *testing.T) {
	want := errors.New("transport boom")
	fc := &FakeClient{ListErr: want}

	_, err := fc.List(context.Background(), "UDID1", "*")
	if !errors.Is(err, want) {
		t.Fatalf("List err = %v, want %v", err, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/crashlogs/... -run TestFakeClient_List -v`
Expected: `FAIL` with `undefined: FakeClient`.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/crashlogs/fake.go
package crashlogs

import "context"

// FakeClient is a hand-written fake Client for cross-package tests.
//
// Set the *Entries / *Result / *Err fields to canned values; the recording
// slices capture each call's arguments in order.
//
// For tests that need to simulate a *sequence* of Pull outcomes (e.g.
// partial-failure scenarios where the first call succeeds and the second
// returns a per-entry failure), populate PullResults instead of PullResult.
// When PullResults is non-empty, each Pull call pops the head; when it
// empties, the fake falls back to PullResult (zero value if unset).
type FakeClient struct {
	ListEntries []Entry
	ListErr     error
	ListCalls   []ListCall

	PullResult  PullResult
	PullResults []PullResult // optional queue; takes precedence over PullResult
	PullErr     error
	PullCalls   []PullCall

	RemoveResult RemoveResult
	RemoveErr    error
	RemoveCalls  []RemoveCall
}

type ListCall struct {
	UDID    string
	Pattern string
}

type PullCall struct {
	UDID    string
	Pattern string
	Dst     string
}

type RemoveCall struct {
	UDID    string
	Pattern string
}

func (f *FakeClient) List(_ context.Context, udid, pattern string) ([]Entry, error) {
	f.ListCalls = append(f.ListCalls, ListCall{UDID: udid, Pattern: pattern})
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.ListEntries, nil
}

func (f *FakeClient) Pull(_ context.Context, udid, pattern, dst string) (PullResult, error) {
	f.PullCalls = append(f.PullCalls, PullCall{UDID: udid, Pattern: pattern, Dst: dst})
	if f.PullErr != nil {
		return PullResult{}, f.PullErr
	}
	if len(f.PullResults) > 0 {
		r := f.PullResults[0]
		f.PullResults = f.PullResults[1:]
		return r, nil
	}
	return f.PullResult, nil
}

func (f *FakeClient) Remove(_ context.Context, udid, pattern string) (RemoveResult, error) {
	f.RemoveCalls = append(f.RemoveCalls, RemoveCall{UDID: udid, Pattern: pattern})
	if f.RemoveErr != nil {
		return RemoveResult{}, f.RemoveErr
	}
	return f.RemoveResult, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/crashlogs/... -run TestFakeClient_List -v`
Expected: `PASS`.

- [ ] **Step 5: Write the failing test for `FakeClient.Pull`**

Append to `internal/crashlogs/crashlogs_test.go`:

```go
func TestFakeClient_Pull_returnsCannedResultAndRecordsCall(t *testing.T) {
	want := PullResult{Pulled: 3, Bytes: 1234, Failures: []Failure{{Path: "/X", ErrMsg: "x"}}}
	fc := &FakeClient{PullResult: want}

	got, err := fc.Pull(context.Background(), "UDID1", "Chrome-*", "/tmp/dst")
	if err != nil {
		t.Fatalf("Pull err = %v, want nil", err)
	}
	if got.Pulled != 3 || got.Bytes != 1234 || len(got.Failures) != 1 {
		t.Fatalf("Pull = %+v, want %+v", got, want)
	}
	if len(fc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1", len(fc.PullCalls))
	}
	if c := fc.PullCalls[0]; c.UDID != "UDID1" || c.Pattern != "Chrome-*" || c.Dst != "/tmp/dst" {
		t.Fatalf("PullCalls[0] = %+v", c)
	}
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/crashlogs/... -run TestFakeClient_Pull -v`
Expected: `PASS`.

- [ ] **Step 7: Write the failing test for `FakeClient.Remove`**

Append to `internal/crashlogs/crashlogs_test.go`:

```go
func TestFakeClient_Remove_returnsCannedResultAndRecordsCall(t *testing.T) {
	want := RemoveResult{Removed: 2, Bytes: 500}
	fc := &FakeClient{RemoveResult: want}

	got, err := fc.Remove(context.Background(), "UDID1", "*")
	if err != nil || got.Removed != 2 || got.Bytes != 500 {
		t.Fatalf("Remove = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	if len(fc.RemoveCalls) != 1 || fc.RemoveCalls[0].UDID != "UDID1" {
		t.Fatalf("RemoveCalls = %+v", fc.RemoveCalls)
	}
}
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test ./internal/crashlogs/... -run TestFakeClient_Remove -v`
Expected: `PASS`.

- [ ] **Step 9: Run all crashlogs package tests**

Run: `go test ./internal/crashlogs/... -v`
Expected: `PASS` for every test.

- [ ] **Step 10: Commit (await user approval)**

```bash
git add internal/crashlogs/fake.go internal/crashlogs/crashlogs_test.go
# Wait for explicit user approval; do not run `git commit` autonomously.
git commit -m "feat: add crashlogs.FakeClient for cross-package tests"
```

---

## Task 5: `internal/iosbackend/crashlogs.go` — go-ios adapter

**Files:**
- Create: `internal/iosbackend/crashlogs.go`
- Create: `internal/iosbackend/crashlogs_device_test.go`

The adapter implements `crashlogs.Client` over `github.com/danielpaulus/go-ios`. This is the **only** package that imports go-ios per SHARED_CONTEXT.md §2 and §4.

**Strategy (cycle-2 redesign — see Revision history above):**

- `List`: call `crashreport.ListReports(device, pattern)` — pattern is forwarded as-is because go-ios already applies `filepath.Match(pattern, filepath.Base(path))` server-side (verified). Then open one AFC connection to `com.apple.crashreportcopymobile` and call `Stat` for each path to populate `Size`. `ModTime` stays at the zero value (Open question #1). One connection per `List` call; one `Stat` round-trip per matched entry.
- `Pull`: delegate to `crashreport.DownloadReports(device, pattern, dst)` — one call, one AFC connection, one walk, all matching files pulled via `PullSingleFile` inside go-ios. To populate `PullResult.Pulled` and `PullResult.Bytes` we call `List` first to know what the bulk pull will touch. If `DownloadReports` returns a non-nil error, the whole call fails — go-ios does not surface per-entry failures, so `PullResult.Failures` stays empty in M3 (a known reduction from the seam's expressive surface, called out in the doc comment). The "proceeds through all entries" AC is honoured because `DownloadReports`'s walker continues past individual file errors internally and only returns at the end.
- `Remove`: list entries (for byte-count accounting), then call `crashreport.RemoveReports(device, "", pattern)` once. Like `DownloadReports`, this is one call that walks once and removes each match. Same per-entry-failure caveat. `Remove` is unused in M3 (M4 calls it) but is required by the seam.

Because the adapter calls real go-ios, its unit-testable surface is small: the seam contract is exercised by the `//go:build device` integration test and by cross-package tests against `crashlogs.FakeClient`. No host-side helper functions remain in this file that aren't trivial wrappers around go-ios calls.

- [ ] **Step 1: Write the failing build-only test for the constructor**

Create `internal/iosbackend/crashlogs_test.go` (this file holds non-device unit tests for the adapter — currently just a compile-time check that the constructor returns the seam type):

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/iosbackend/... -run TestNewCrashLogs -v`
Expected: `FAIL` with `undefined: NewCrashLogs`.

- [ ] **Step 3: Write the minimal adapter implementation**

```go
// internal/iosbackend/crashlogs.go
//
// crashLogsClient is the go-ios adapter for crashlogs.Client.
//
// Design (cycle-2 — see plan Revision history):
//   - Pull delegates to crashreport.DownloadReports, which opens one AFC
//     connection to com.apple.crashreportcopymobile, walks the crash-report
//     tree once, and pulls every filepath.Match-matching basename via
//     PullSingleFile internally. We do NOT iterate per-entry here — that
//     would be N+1 walks + N reconnects on a phone with many crashes.
//   - List uses crashreport.ListReports to enumerate paths (pattern is
//     forwarded; go-ios applies filepath.Match(pattern, filepath.Base(p))
//     internally — verified at
//     https://raw.githubusercontent.com/danielpaulus/go-ios/main/ios/crashreport/crashreport.go),
//     then opens one AFC connection to populate Size via Stat. ModTime is
//     left at the zero value (see plan Open question #1).
//   - Remove delegates to crashreport.RemoveReports. Per-entry failure
//     reporting is not supported by go-ios's bulk APIs; PullResult.Failures
//     and RemoveResult.Failures will be empty even when the device-side
//     walker hit individual errors (a known reduction documented in the
//     plan's Revision history).
package iosbackend

import (
	"context"
	"fmt"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/afc"
	"github.com/danielpaulus/go-ios/ios/crashreport"
)

// crashReportCopyMobileService is the AFC-over-crashreport service name.
//
// Verified against go-ios ios/crashreport/crashreport.go at the pinned SHA.
const crashReportCopyMobileService = "com.apple.crashreportcopymobile"

type crashLogsClient struct{}

// NewCrashLogs returns a crashlogs.Client backed by go-ios.
func NewCrashLogs() crashlogs.Client { return &crashLogsClient{} }

// findDevice locates the ios.DeviceEntry for the given UDID, or returns an
// error if it's not currently attached.
func findDevice(udid string) (ios.DeviceEntry, error) {
	list, err := ios.ListDevices()
	if err != nil {
		return ios.DeviceEntry{}, fmt.Errorf("list devices: %w", err)
	}
	for _, d := range list.DeviceList {
		if d.Properties.SerialNumber == udid {
			return d, nil
		}
	}
	return ios.DeviceEntry{}, fmt.Errorf("device %q not attached", udid)
}

// openCrashReportAfc opens an AFC client against the
// com.apple.crashreportcopymobile service for the given device. Caller MUST
// Close() the returned client when done.
func openCrashReportAfc(device ios.DeviceEntry) (*afc.Client, error) {
	conn, err := ios.ConnectToService(device, crashReportCopyMobileService)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", crashReportCopyMobileService, err)
	}
	return afc.NewFromConn(conn), nil
}

func (c *crashLogsClient) List(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	device, err := findDevice(udid)
	if err != nil {
		return nil, err
	}
	if pattern == "" {
		pattern = "*"
	}
	// go-ios applies filepath.Match(pattern, filepath.Base(p)) server-side;
	// no host-side re-filter needed.
	paths, err := crashreport.ListReports(device, pattern)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	cli, err := openCrashReportAfc(device)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	entries := make([]crashlogs.Entry, 0, len(paths))
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := crashlogs.Entry{Path: p, ModTime: time.Time{}}
		if info, statErr := cli.Stat(p); statErr == nil {
			entry.Size = info.Size
		}
		// If Stat fails (e.g. file vanished between list and stat), include
		// the entry with Size=0 rather than dropping it; the user can still
		// see and pull it.
		entries = append(entries, entry)
	}
	return entries, nil
}

func (c *crashLogsClient) Pull(ctx context.Context, udid, pattern, dst string) (crashlogs.PullResult, error) {
	if err := ctx.Err(); err != nil {
		return crashlogs.PullResult{}, err
	}
	// Pre-list to compute counts + bytes for the result. If the user only
	// cares about success/fail and not the count, this extra round-trip
	// could be elided in a future optimisation.
	entries, err := c.List(ctx, udid, pattern)
	if err != nil {
		return crashlogs.PullResult{}, err
	}
	device, err := findDevice(udid)
	if err != nil {
		return crashlogs.PullResult{}, err
	}
	if pattern == "" {
		pattern = "*"
	}
	// One call, one connection, one walk, all matching files pulled.
	if err := crashreport.DownloadReports(device, pattern, dst); err != nil {
		return crashlogs.PullResult{}, fmt.Errorf("download reports: %w", err)
	}
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	return crashlogs.PullResult{Pulled: len(entries), Bytes: total}, nil
}

func (c *crashLogsClient) Remove(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
	if err := ctx.Err(); err != nil {
		return crashlogs.RemoveResult{}, err
	}
	entries, err := c.List(ctx, udid, pattern)
	if err != nil {
		return crashlogs.RemoveResult{}, err
	}
	device, err := findDevice(udid)
	if err != nil {
		return crashlogs.RemoveResult{}, err
	}
	if pattern == "" {
		pattern = "*"
	}
	// crashreport.RemoveReports takes (device, cwd, pattern); cwd "" means
	// the crash-report root.
	if err := crashreport.RemoveReports(device, "", pattern); err != nil {
		return crashlogs.RemoveResult{}, fmt.Errorf("remove reports: %w", err)
	}
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	return crashlogs.RemoveResult{Removed: len(entries), Bytes: total}, nil
}
```

- [ ] **Step 4: Run the constructor test to verify it passes**

Run: `go test ./internal/iosbackend/... -run TestNewCrashLogs -v`
Expected: `PASS`.

- [ ] **Step 5: Run a build to confirm the adapter compiles cleanly**

Run: `go build ./... && go vet ./internal/iosbackend/...`
Expected: build succeeds, vet silent. (If the implementer's go-ios version exposes `ios.ListDevices().DeviceList` differently — e.g. an iterator — they should adapt at compile-time. The API at the pinned SHA returns `ios.DeviceList{DeviceList []ios.DeviceEntry}`; if a newer pin renamed it, surface that in code review.)

- [ ] **Step 6: Write the failing `//go:build device` integration test**

```go
//go:build device
// +build device

// internal/iosbackend/crashlogs_device_test.go
package iosbackend

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDevice_CrashLogs_ListAndPullIntoTempDir(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID unset; skipping device integration test")
	}

	c := NewCrashLogs()
	// 60s budget: one ListReports + one DownloadReports walk is well under
	// 30s on a phone with hundreds of crashes (single AFC connection, single
	// walk); the extra headroom covers the per-path Stat round-trips in List.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	entries, err := c.List(ctx, udid, "*")
	if err != nil {
		t.Fatalf("List err = %v", err)
	}
	t.Logf("found %d crash entries", len(entries))
	// Do not assert non-empty; a freshly-wiped phone may legitimately have zero.

	dst := t.TempDir()
	res, err := c.Pull(ctx, udid, "*", dst)
	if err != nil {
		t.Fatalf("Pull err = %v", err)
	}
	t.Logf("pulled %d entries (%d bytes), %d failures", res.Pulled, res.Bytes, len(res.Failures))
	// Cardinal rule: M3 is read-only. We do NOT call Remove here.
}
```

- [ ] **Step 7: Verify the device test compiles**

Run: `go test -tags=device -run TestDevice_CrashLogs_ListAndPullIntoTempDir ./internal/iosbackend/... -v`
Expected: if `IOS_TIDY_TEST_UDID` is unset, test is `SKIP`. If set, test must `PASS`. The implementer is expected to run this with a real phone before claiming M3 complete.

- [ ] **Step 8: Run the whole iosbackend unit test suite to confirm M1/M2 tests still pass**

Run: `go test ./internal/iosbackend/... -v`
Expected: `PASS` (no `-tags=device`, so the device test is excluded by build tag).

- [ ] **Step 9: Commit (await user approval)**

```bash
git add internal/iosbackend/crashlogs.go internal/iosbackend/crashlogs_test.go internal/iosbackend/crashlogs_device_test.go
# Wait for explicit user approval; do not run `git commit` autonomously.
git commit -m "feat: add iosbackend crashlogs adapter with read-only device test"
```

---

## Task 6: `cmd/ios-tidy/crashlogs.go` — `crashlogs list` subcommand

**Files:**
- Create: `cmd/ios-tidy/crashlogs.go`
- Create: `cmd/ios-tidy/crashlogs_test.go`
- Modify: `cmd/ios-tidy/main.go` (register `crashlogs` group)

The cmd layer is tested with fakes (`device.FakeLister` from M1, `crashlogs.FakeClient` from Task 4, `ui.FakePrompter` from Task 1). The handler accepts an explicit struct of dependencies so tests don't touch globals.

`list` flags: `--device UDID`, `--pattern GLOB` (default `"*"`), `--json`.

Selection rule (mirrors M2 idiom, but re-derived here so this plan stands alone):
- If `--device` provided: use it.
- Else: call `Lister.List`; if exactly one device, use it; if zero, error to stderr and exit non-zero; if more than one, error listing UDIDs and exit non-zero.

- [ ] **Step 1: Write the failing test for `runCrashLogsList` table output**

```go
// cmd/ios-tidy/crashlogs_test.go
package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

func TestCrashLogsList_tableOutput_filtersByPatternAndPrintsBytes(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1", Name: "iPhone"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/Chrome-1.ips", Size: 1024, ModTime: time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)},
			{Path: "/Mail.crash", Size: 2048},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{"--pattern", "Chrome-*"}, &stdout, &stderr)

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "/Chrome-1.ips") {
		t.Fatalf("stdout missing /Chrome-1.ips: %q", out)
	}
	if strings.Contains(out, "/Mail.crash") {
		t.Fatalf("stdout should not include /Mail.crash (filtered out): %q", out)
	}
	if !strings.Contains(out, "1.0 KB") && !strings.Contains(out, "1 KB") {
		t.Fatalf("stdout should contain a human-readable size: %q", out)
	}
	// Pattern was applied client-side; the fake recorded the raw flag value.
	if len(cc.ListCalls) != 1 || cc.ListCalls[0].Pattern != "Chrome-*" {
		t.Fatalf("ListCalls = %+v, want one with Pattern=Chrome-*", cc.ListCalls)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsList -v`
Expected: `FAIL` with `undefined: runCrashLogsList` and `undefined: crashLogsDeps`.

- [ ] **Step 3: Write the minimal implementation**

```go
// cmd/ios-tidy/crashlogs.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// crashLogsDeps groups the injected dependencies for the crashlogs subcommand
// so tests can wire fakes without touching globals.
type crashLogsDeps struct {
	Lister   device.Lister
	Client   crashlogs.Client
	Prompter ui.Prompter
}

// resolveDevice picks the target UDID. If override is non-empty, it's used
// verbatim. Otherwise: zero attached → error; one → that one; many → error
// listing UDIDs.
func resolveDevice(ctx context.Context, l device.Lister, override string, stderr io.Writer) (string, error) {
	if override != "" {
		return override, nil
	}
	devs, err := l.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list devices: %w", err)
	}
	switch len(devs) {
	case 0:
		fmt.Fprintln(stderr, "no devices attached")
		return "", errors.New("no devices attached")
	case 1:
		return devs[0].UDID, nil
	default:
		fmt.Fprintln(stderr, "multiple devices attached; use --device UDID:")
		for _, d := range devs {
			fmt.Fprintf(stderr, "  %s  %s\n", d.UDID, d.Name)
		}
		return "", errors.New("multiple devices attached")
	}
}

// runCrashLogsList implements `crashlogs list`. Returns the process exit code.
func runCrashLogsList(ctx context.Context, deps crashLogsDeps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("crashlogs list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		udidFlag    = fs.String("device", "", "UDID of the target device")
		patternFlag = fs.String("pattern", "*", "filepath.Match glob applied to filepath.Base of each path")
		jsonFlag    = fs.Bool("json", false, "emit JSON instead of a table")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	udid, err := resolveDevice(ctx, deps.Lister, *udidFlag, stderr)
	if err != nil {
		return 1
	}

	entries, err := deps.Client.List(ctx, udid, *patternFlag)
	if err != nil {
		fmt.Fprintf(stderr, "list crash logs: %v\n", err)
		return 1
	}

	if *jsonFlag {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return jsonExit(enc.Encode(entries), stderr)
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tSIZE\tMTIME")
	for _, e := range entries {
		mt := "-"
		if !e.ModTime.IsZero() {
			mt = e.ModTime.UTC().Format("2006-01-02 15:04:05Z")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.Path, ui.FormatBytes(uint64(e.Size)), mt)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(stderr, "render: %v\n", err)
		return 1
	}
	return 0
}

func jsonExit(err error, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintf(stderr, "json: %v\n", err)
		return 1
	}
	return 0
}

// runCrashLogs is the top-level dispatcher for `ios-tidy crashlogs ...`. It
// is called from main.go and routes to list/pull. `clean` is M4.
func runCrashLogs(ctx context.Context, deps crashLogsDeps, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ios-tidy crashlogs {list|pull} [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runCrashLogsList(ctx, deps, args[1:], stdout, stderr)
	case "pull":
		return runCrashLogsPull(ctx, deps, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown crashlogs subcommand: %q\n", args[0])
		return 2
	}
}

// runCrashLogsPull is implemented in Task 7. Stub keeps the dispatcher
// compiling for Task 6 — referenced only via the runCrashLogs switch above,
// so it needs no imports beyond what this file already uses.
func runCrashLogsPull(_ context.Context, _ crashLogsDeps, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "crashlogs pull: not implemented yet")
	return 2
}
```

(Implementer note: `ui.FormatBytes` is a helper introduced in M1. Its signature is `func FormatBytes(uint64) string`. If M1 used a different name on disk — e.g. `HumanBytes` — adapt at call site; do not invent a new helper. Imports for `os`, `path/filepath`, and `strings` are deliberately omitted from this task's import block — Task 7 introduces them together with the code that uses them, so this commit passes `go vet` and `gofmt` cleanly.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsList -v`
Expected: `PASS`.

- [ ] **Step 5: Write the failing test for `runCrashLogsList` JSON output**

Append to `cmd/ios-tidy/crashlogs_test.go`:

```go
func TestCrashLogsList_jsonOutput_isCleanJSON(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/A.ips", Size: 10, ModTime: time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{"--json"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"path": "/A.ips"`) {
		t.Fatalf("stdout missing path JSON: %q", out)
	}
	if !strings.Contains(out, `"size": 10`) {
		t.Fatalf("stdout missing size JSON: %q", out)
	}
	// stderr should NOT contain the prompt or table chrome.
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty for clean JSON path, got %q", stderr.String())
	}
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsList_jsonOutput -v`
Expected: `PASS`.

- [ ] **Step 7: Write the failing test for device-selection error path**

Append:

```go
func TestCrashLogsList_errorsWhenMultipleDevicesAndNoFlag(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{
		{UDID: "U1", Name: "Phone1"},
		{UDID: "U2", Name: "Phone2"},
	}}
	cc := &crashlogs.FakeClient{}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "U1") || !strings.Contains(stderr.String(), "U2") {
		t.Fatalf("stderr should list both UDIDs: %q", stderr.String())
	}
	if len(cc.ListCalls) != 0 {
		t.Fatalf("client.List should not be called when selection fails")
	}
}

func TestCrashLogsList_propagatesClientError(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{ListErr: errors.New("boom")}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr should contain client error: %q", stderr.String())
	}
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsList -v`
Expected: every subtest `PASS`.

- [ ] **Step 9: Wire `crashlogs` into `main.go`**

Modify `cmd/ios-tidy/main.go`. Locate the existing subcommand switch (added in M1 + M2 — typically a `switch args[0]` over `devices`, `storage`, `version`). Add:

```go
case "crashlogs":
	deps := crashLogsDeps{
		Lister:   iosbackend.NewDevices(),                          // from M1
		Client:   iosbackend.NewCrashLogs(),                        // from Task 5
		Prompter: ui.NewStdinPrompter(os.Stdin, os.Stderr),         // from Task 2
	}
	os.Exit(runCrashLogs(ctx, deps, args[1:], os.Stdout, os.Stderr))
```

The implementer reads M1's wiring to find the exact constructor names; this plan assumes `iosbackend.NewDevices()` returns a `device.Lister`. If M1 named it differently (e.g. `iosbackend.NewDeviceLister()`), use the actual name.

- [ ] **Step 10: Run the full unit-test suite to confirm nothing regressed**

Run: `go test ./...`
Expected: `PASS` for every package.

- [ ] **Step 11: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_test.go cmd/ios-tidy/main.go
# Wait for explicit user approval; do not run `git commit` autonomously.
git commit -m "feat: add crashlogs list subcommand with table and JSON output"
```

---

## Task 7: `cmd/ios-tidy/crashlogs.go` — `crashlogs pull` subcommand

**Files:**
- Modify: `cmd/ios-tidy/crashlogs.go` (replace stubbed `runCrashLogsPull`)
- Modify: `cmd/ios-tidy/crashlogs_test.go` (append tests)

`pull` flags: `--device UDID`, `--pattern GLOB` (default `"*"`), `--out DIR` (required), `--force` (skip overwrite prompts).

Flow (cycle-2 redesign — see Revision history above):
a. Resolve device.
b. `entries, _ := deps.Client.List(ctx, udid, pattern)`.
c. `os.MkdirAll(out, 0o755)`.
d. **Pre-scan for conflicts.** For each entry, compute `dst := crashlogs.DestPath(out, entry.Path)`. If `dst` exists and not `--force`, prompt `Overwrite <dst>? [y/N]` via `deps.Prompter`. Collect the user's answers.
e. **If any prompt was answered "no"**, abort the entire pull with exit code 1 and a clear message ("declined N overwrite(s); re-run with --force or remove the conflict(s)"). The bulk pull never starts; nothing is written. This is the simplest correct UX given the bulk-pull adapter can't honour per-entry skips mid-walk.
f. **Otherwise, single bulk pull.** Call `deps.Client.Pull(ctx, udid, pattern, out)` **exactly once**. The result's `Pulled` / `Bytes` populate the summary. If the call returns an error, report it as a failure and exit non-zero.
g. Print `"pulled X of Y, skipped Z (declined), failed W"` (Z is always 0 on the success path because skips abort earlier; the field exists so the summary line is uniform with the abort path's output).

The seam stays unchanged: `Client.Pull` is called once per `pull` invocation. The acceptance criteria "reports counts + bytes" and "Returns non-zero on any failure but proceeds through all entries" are met: counts/bytes come from the adapter; "proceeds through all entries" is owned by go-ios's internal walker inside `DownloadReports`.

- [ ] **Step 1: Write the failing test for happy-path pull**

Append to `cmd/ios-tidy/crashlogs_test.go`:

Add these imports to the existing `import (...)` block in `cmd/ios-tidy/crashlogs_test.go` (alongside the M1 imports that already include `bytes`, `context`, `errors`, `strings`, `testing`, `time`, and the internal packages):

```go
"os"
"path/filepath"
```

Then append:

```go
func TestCrashLogsPull_happyPath_singleBulkPullCallWhenNoConflicts(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/Chrome-1.ips", Size: 100},
			{Path: "/Chrome-2.ips", Size: 200},
		},
		PullResult: crashlogs.PullResult{Pulled: 2, Bytes: 300},
	}
	fp := ui.NewFakePrompter(nil) // no prompts expected — fresh out dir
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: fp}

	outDir := t.TempDir()
	args := []string{"--out", outDir, "--pattern", "Chrome-*"}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, args, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(cc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1 (single bulk pull)", len(cc.PullCalls))
	}
	if got := cc.PullCalls[0]; got.UDID != "U1" || got.Pattern != "Chrome-*" || got.Dst != outDir {
		t.Fatalf("PullCalls[0] = %+v; want {U1, Chrome-*, %s}", got, outDir)
	}
	out := stdout.String()
	if !strings.Contains(out, "pulled 2 of 2") {
		t.Fatalf("stdout missing 'pulled 2 of 2': %q", out)
	}
	if !strings.Contains(out, "skipped 0") || !strings.Contains(out, "failed 0") {
		t.Fatalf("stdout summary shape wrong: %q", out)
	}
	if len(fp.Asked) != 0 {
		t.Fatalf("prompter should not have been asked; got %v", fp.Asked)
	}
}
```

(The newly-added `os` / `path/filepath` imports are used by the overwrite-prompt-no test in Step 5 below and several later tests — Go's `go test` build will accept them as used once Step 5's test is appended in the same file. If the implementer prefers to add the imports in Step 5 instead of here, that also works; pick whichever keeps `gofmt -l .` quiet at every commit.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsPull_happyPath_singleBulkPullCallWhenNoConflicts -v`
Expected: `FAIL` — the stub returns exit 2 and prints "not implemented yet".

- [ ] **Step 3: Write the minimal implementation**

Replace the stub `runCrashLogsPull` in `cmd/ios-tidy/crashlogs.go` with:

Update the import block in `cmd/ios-tidy/crashlogs.go` to add `"os"` and `"path/filepath"` alongside the existing imports. Then replace the stub `runCrashLogsPull` with:

```go
// ErrSkippedOverwrite is the sentinel reported in stderr when the user
// declines an overwrite during the pre-scan. Exported so M4 / future
// callers can reuse the vocabulary.
var ErrSkippedOverwrite = errors.New("skipped: user declined overwrite")

// summaryFormat is the uniform shape for the per-run summary line emitted to
// stdout. All three exit paths (declined-abort, total-failure, normal) use
// this format with different values, so the shape stays consistent for
// scripts that parse it. Order: pulled, total, bytesFormatted, skipped, failed.
const summaryFormat = "pulled %d of %d (%s), skipped %d, failed %d\n"

func runCrashLogsPull(ctx context.Context, deps crashLogsDeps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("crashlogs pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		udidFlag    = fs.String("device", "", "UDID of the target device")
		patternFlag = fs.String("pattern", "*", "filepath.Match glob applied to filepath.Base of each path")
		outFlag     = fs.String("out", "", "destination directory (required)")
		forceFlag   = fs.Bool("force", false, "overwrite existing files without prompting")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *outFlag == "" {
		fmt.Fprintln(stderr, "crashlogs pull: --out DIR is required")
		return 2
	}

	udid, err := resolveDevice(ctx, deps.Lister, *udidFlag, stderr)
	if err != nil {
		return 1
	}

	entries, err := deps.Client.List(ctx, udid, *patternFlag)
	if err != nil {
		fmt.Fprintf(stderr, "list crash logs: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(*outFlag, 0o755); err != nil {
		fmt.Fprintf(stderr, "create out dir: %v\n", err)
		return 1
	}

	// Pre-scan: identify conflicts and prompt the user about each. If any
	// answer is "no" (and --force is off), abort before the bulk Pull starts.
	var declined []string
	if !*forceFlag {
		for _, e := range entries {
			dst := crashlogs.DestPath(*outFlag, e.Path)
			if _, statErr := os.Stat(dst); statErr != nil {
				continue // no conflict
			}
			ok, perr := deps.Prompter.Confirm(ctx, fmt.Sprintf("Overwrite %s?", dst))
			if perr != nil {
				fmt.Fprintf(stderr, "prompt: %v\n", perr)
				return 1
			}
			if !ok {
				declined = append(declined, dst)
			}
		}
	}
	if len(declined) > 0 {
		fmt.Fprintf(stderr, "%s: declined %d overwrite(s); re-run with --force or remove the conflict(s):\n",
			ErrSkippedOverwrite.Error(), len(declined))
		for _, d := range declined {
			fmt.Fprintf(stderr, "  %s\n", d)
		}
		// Uniform summary shape (see summaryFormat below).
		fmt.Fprintf(stdout, summaryFormat, 0, len(entries), ui.FormatBytes(0), len(declined), 0)
		return 1
	}

	// Single bulk pull — go-ios's DownloadReports walks the device once and
	// pulls every match. No per-entry round-trips from this process.
	res, perr := deps.Client.Pull(ctx, udid, *patternFlag, *outFlag)
	if perr != nil {
		fmt.Fprintf(stderr, "pull crash logs: %v\n", perr)
		fmt.Fprintf(stdout, summaryFormat, 0, len(entries), ui.FormatBytes(0), 0, len(entries))
		return 1
	}

	fmt.Fprintf(stdout, summaryFormat,
		res.Pulled, len(entries), ui.FormatBytes(uint64(res.Bytes)), 0, len(res.Failures))
	for _, f := range res.Failures {
		fmt.Fprintf(stderr, "  failed: %s — %s\n", f.Path, f.ErrMsg)
	}
	if len(res.Failures) > 0 {
		return 1
	}
	return 0
}
```

No placeholder `var _ = ...` lines are committed; every import in `cmd/ios-tidy/crashlogs.go` is used by code paths introduced in Task 6 or Task 7.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsPull_happyPath_singleBulkPullCallWhenNoConflicts -v`
Expected: `PASS`.

- [ ] **Step 5: Write the failing test for overwrite-prompt-no (skip records a failure, non-zero exit)**

Append:

```go
func TestCrashLogsPull_overwritePromptNo_abortsEntireBulkPull(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/Chrome-1.ips", Size: 100},
			{Path: "/Chrome-2.ips", Size: 200},
		},
		PullResult: crashlogs.PullResult{Pulled: 2, Bytes: 300},
	}
	fp := ui.NewFakePrompter([]bool{false}) // user says no to the one conflict
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: fp}

	outDir := t.TempDir()
	// Pre-create one destination file so exactly one prompt fires.
	existing := filepath.Join(outDir, "Chrome-1.ips")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", outDir}, &stdout, &stderr)

	if exit != 1 {
		t.Fatalf("exit = %d, want 1 (abort on declined overwrite)", exit)
	}
	if len(cc.PullCalls) != 0 {
		t.Fatalf("PullCalls = %d, want 0 (bulk pull must NOT start)", len(cc.PullCalls))
	}
	if len(fp.Asked) != 1 || !strings.Contains(fp.Asked[0], existing) {
		t.Fatalf("prompt should reference the dst path; got %v", fp.Asked)
	}
	if !strings.Contains(stderr.String(), "declined 1 overwrite") {
		t.Fatalf("stderr should report 'declined 1 overwrite': %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "pulled 0 of 2") || !strings.Contains(stdout.String(), "skipped 1") {
		t.Fatalf("stdout summary wrong: %q", stdout.String())
	}
	// The non-conflicting entry must NOT have been pulled to disk either,
	// because we aborted before calling Client.Pull at all.
	if _, err := os.Stat(filepath.Join(outDir, "Chrome-2.ips")); err == nil {
		t.Fatalf("Chrome-2.ips should not exist — bulk pull was aborted")
	}
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsPull_overwritePromptNo_abortsEntireBulkPull -v`
Expected: `PASS`.

- [ ] **Step 7: Write the failing test for `--force` bypassing the prompt**

Append:

```go
func TestCrashLogsPull_forceFlag_bypassesOverwritePromptAndPullsOnce(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/Chrome-1.ips", Size: 100}},
		PullResult:  crashlogs.PullResult{Pulled: 1, Bytes: 100},
	}
	// FakePrompter with no answers: if Confirm is called, the test panics.
	fp := ui.NewFakePrompter(nil)
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: fp}

	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "Chrome-1.ips"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", outDir, "--force"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fp.Asked) != 0 {
		t.Fatalf("Prompter should not have been called under --force; got %v", fp.Asked)
	}
	if len(cc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1 (single bulk pull)", len(cc.PullCalls))
	}
}
```

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsPull_forceFlag_bypassesOverwritePromptAndPullsOnce -v`
Expected: `PASS`.

- [ ] **Step 9: Write the failing test for partial-failure reporting**

Append:

```go
func TestCrashLogsPull_partialFailureFromAdapter_returnsNonZeroAndReportsCounts(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/A.ips", Size: 10},
			{Path: "/B.ips", Size: 20},
		},
		PullResult: crashlogs.PullResult{
			Pulled: 1,
			Bytes:  10,
			Failures: []crashlogs.Failure{
				{Path: "/B.ips", ErrMsg: "afc: permission denied"},
			},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", t.TempDir()}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1 (partial failure)", exit)
	}
	if len(cc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1 (single bulk pull)", len(cc.PullCalls))
	}
	if !strings.Contains(stdout.String(), "pulled 1 of 2") {
		t.Fatalf("stdout summary missing 'pulled 1 of 2': %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "failed 1") {
		t.Fatalf("stdout summary missing 'failed 1': %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "afc: permission denied") {
		t.Fatalf("stderr should detail the failure: %q", stderr.String())
	}
}

func TestCrashLogsPull_totalAdapterError_returnsNonZeroAndReportsAllAsFailed(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/A.ips", Size: 10},
			{Path: "/B.ips", Size: 20},
		},
		PullErr: errors.New("transport boom"),
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", t.TempDir()}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr.String(), "transport boom") {
		t.Fatalf("stderr should contain transport error: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "pulled 0 of 2") || !strings.Contains(stdout.String(), "failed 2") {
		t.Fatalf("stdout summary wrong: %q", stdout.String())
	}
}
```

(This test exercises `FakeClient` directly — no `chainedFakeClient` embedding gymnastics. The new `PullResults []PullResult` queue on `FakeClient` is available for future tests that need a sequence of bulk-pull outcomes; M3's single-bulk-pull flow does not need it.)

- [ ] **Step 10: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsPull_partialFailureFromAdapter -v && go test ./cmd/ios-tidy/... -run TestCrashLogsPull_totalAdapterError -v`
Expected: `PASS` for both.

- [ ] **Step 11: Write the failing test for out-dir creation**

Append:

```go
func TestCrashLogsPull_createsOutDirIfMissing(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/A.ips", Size: 10}},
		PullResult:  crashlogs.PullResult{Pulled: 1, Bytes: 10},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	parent := t.TempDir()
	outDir := filepath.Join(parent, "nested", "out") // does not exist
	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", outDir}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if info, err := os.Stat(outDir); err != nil || !info.IsDir() {
		t.Fatalf("out dir not created: stat err=%v", err)
	}
}
```

- [ ] **Step 12: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsPull_createsOutDirIfMissing -v`
Expected: `PASS`.

- [ ] **Step 13: Write the failing test for missing `--out`**

Append:

```go
func TestCrashLogsPull_errorsWhenOutMissing(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "--out") {
		t.Fatalf("stderr should mention --out: %q", stderr.String())
	}
	if len(cc.ListCalls) != 0 {
		t.Fatalf("List should not be called when --out missing")
	}
}
```

- [ ] **Step 14: Run the test to verify it passes**

Run: `go test ./cmd/ios-tidy/... -run TestCrashLogsPull_errorsWhenOutMissing -v`
Expected: `PASS`.

- [ ] **Step 15: Run the full repository test suite**

Run: `go test ./...`
Expected: `PASS` for every package. Verify M1 + M2 tests still pass.

- [ ] **Step 16: Run `go vet` and `gofmt` checks**

Run: `go vet ./... && gofmt -l .`
Expected: no vet output, no files printed by `gofmt -l`.

- [ ] **Step 17: Commit (await user approval)**

```bash
git add cmd/ios-tidy/crashlogs.go cmd/ios-tidy/crashlogs_test.go
# Wait for explicit user approval; do not run `git commit` autonomously.
git commit -m "feat: add crashlogs pull subcommand with overwrite prompting"
```

---

## Task 8: Final acceptance sweep

**Files:** none modified.

This task is a verification gate against SHARED_CONTEXT.md §8's M3 acceptance criteria. Run each command and confirm the output. No code changes; if a check fails, fix in an earlier task and re-run.

- [ ] **Step 1: `crashlogs list` flag surface**

Run: `go run ./cmd/ios-tidy crashlogs list --help 2>&1 | head -20`
Expected: usage line shows `--device`, `--pattern`, `--json`.

- [ ] **Step 2: `crashlogs pull` flag surface**

Run: `go run ./cmd/ios-tidy crashlogs pull --help 2>&1 | head -20`
Expected: usage line shows `--device`, `--pattern`, `--out`, `--force`.

- [ ] **Step 3: Default pattern is `*`**

Confirm in source: `cmd/ios-tidy/crashlogs.go` `runCrashLogsList` `--pattern` default == `"*"` and `runCrashLogsPull` same.

- [ ] **Step 4: Unit-test acceptance grid**

Confirm test names map to acceptance items:

| Acceptance item | Test |
|---|---|
| Pattern filtering | `TestMatchEntries_*`, `TestCrashLogsList_tableOutput_filtersByPatternAndPrintsBytes` |
| Total bytes math | `TestCrashLogsPull_happyPath_singleBulkPullCallWhenNoConflicts`, `TestCrashLogsPull_partialFailureFromAdapter_returnsNonZeroAndReportsCounts` |
| Overwrite confirmation flow | `TestCrashLogsPull_overwritePromptNo_abortsEntireBulkPull`, `TestCrashLogsPull_forceFlag_bypassesOverwritePromptAndPullsOnce` |
| Partial-failure reporting | `TestCrashLogsPull_partialFailureFromAdapter_*`, `TestCrashLogsPull_totalAdapterError_*` |
| Out dir created if missing | `TestCrashLogsPull_createsOutDirIfMissing` |
| Single bulk Pull call | `TestCrashLogsPull_happyPath_singleBulkPullCallWhenNoConflicts` |
| Destination path mapping | `TestDestPath_preservesRelativeStructure` |
| Prompt writes to stderr (clean JSON) | `TestCrashLogsList_jsonOutput_isCleanJSON` |
| Defaults-no semantics | `TestStdinPrompter_Confirm_defaultsNoTable` |
| EOF is no | `TestStdinPrompter_Confirm_EOFIsNoNotError` |
| FakePrompter exhaustion panics | `TestFakePrompter_panicsWhenExhausted` |

If any row has no matching test, fix in the relevant task before this gate clears.

- [ ] **Step 5: Device integration test (manual, with phone attached)**

Run: `IOS_TIDY_TEST_UDID=<udid> go test -tags=device -v ./internal/iosbackend/... -run TestDevice_CrashLogs`
Expected: `PASS` (or `SKIP` if no phone). On a phone with no crashes the test still passes — the assertion is "no error", not "non-empty".

- [ ] **Step 6: Final test sweep**

Run: `go test ./...` and `go test -race ./internal/ui/... ./internal/crashlogs/... ./cmd/ios-tidy/...`
Expected: all `PASS`. `-race` catches goroutine bugs in the `stdinPrompter`.

- [ ] **Step 7: No new go-ios imports outside iosbackend**

Run: `grep -RIn "danielpaulus/go-ios" --include="*.go" . | grep -v "internal/iosbackend/" | grep -v "_test.go" | grep -v "^./go.mod" | grep -v "^./go.sum"`
Expected: empty output. (SHARED_CONTEXT.md §2: only `internal/iosbackend/` may import go-ios.)

- [ ] **Step 8: Plan complete — no commit on this task.**

This is a verification-only task. If everything's green, M3 is done.

---

## Self-review checklist (executed before plan handoff)

1. **Spec coverage** — every acceptance item in SHARED_CONTEXT.md §8 for M3 has a task and a named test (Task 8 Step 4 is the matrix).
2. **Placeholder scan** — no `TBD`, no `TODO`, no "implement later", no "similar to Task N", no "handle edge cases" without code. Every code block is complete and copy-pastable.
3. **Type consistency** — `Entry`, `Failure`, `PullResult`, `RemoveResult`, `Client` are defined once in Task 3 and used unchanged everywhere. `crashLogsDeps`, `crashLogsClient`, `findDevice`, `openCrashReportAfc`, `resolveDevice`, `runCrashLogs`, `runCrashLogsList`, `runCrashLogsPull`, `ErrSkippedOverwrite`, `NewFakePrompter`, `NewStdinPrompter`, `Prompter`, `stdinPrompter`, `FakePrompter`, `FakeClient`, `MatchEntries`, `DestPath` — all defined exactly once (in the package noted by the file path of their declaration) and called by their exact names. `destPath` / `destPathHost` from cycle 1 are deleted; the single shared `crashlogs.DestPath` replaces both.
4. **TDD cadence** — every production-code step is preceded by RED + verify-RED and followed by verify-GREEN + commit. Tasks 6 and 7 add multiple RED/GREEN cycles before a single commit because the cmd-layer behaviour is multi-faceted; this is the recommended pattern in `superpowers:test-driven-development` ("Next failing test for next feature").
5. **No go-ios outside iosbackend** — only `internal/iosbackend/crashlogs.go` and `internal/iosbackend/crashlogs_device_test.go` import `github.com/danielpaulus/go-ios/...`. The cmd layer imports `internal/crashlogs` (the seam) and `internal/iosbackend` (for constructors only, in `main.go`'s wiring section).
6. **Destructive operations** — M3 has **none**. `Remove` is implemented on the adapter so M4 can use it without re-touching `internal/iosbackend/`, but no cmd-layer code calls `Remove`. The device test does not call `Remove`.

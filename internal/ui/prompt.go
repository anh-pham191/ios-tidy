// internal/ui/prompt.go
package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Prompter asks the user a question.
//
// Confirm returns (true, nil) only on a clean yes; (false, nil) on no, empty
// input, or EOF; (false, err) only on a non-EOF read error or context
// cancellation. Default-no — never default-yes.
//
// ReadLine reads one full line and returns it with trailing CR/LF stripped.
// EOF with no bytes returns ("", nil). Context cancellation returns ("",
// ctx.Err()). Used by the strict typed-bundle-ID gate for
// `apps clean --include-documents`.
type Prompter interface {
	Confirm(ctx context.Context, question string) (bool, error)
	ReadLine(ctx context.Context, prompt string) (string, error)
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

// ReadLine prints prompt+" " to w and reads one line from r, applying the
// same context-cancellable goroutine pattern as Confirm. Trailing CR/LF is
// stripped. EOF with no bytes returns ("", nil); cancellation returns
// ("", ctx.Err()).
func (p *stdinPrompter) ReadLine(ctx context.Context, prompt string) (string, error) {
	fmt.Fprintf(p.w, "%s ", prompt)

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
		return "", ctx.Err()
	case res := <-ch:
		if res.err != nil && res.err != io.EOF {
			return "", res.err
		}
		// EOF with no bytes → "" + nil. Otherwise strip trailing CR/LF.
		return strings.TrimRight(res.line, "\r\n"), nil
	}
}

// FakePrompter is a deterministic Prompter for tests.
//
// There are two modes:
//   - Function mode: set ConfirmFn and Confirm delegates to it. The recording
//     slice Asked is still populated.
//   - Queue mode (default): construct with NewFakePrompter and answers are
//     dequeued in order. When the queue is exhausted, Confirm panics to fail
//     the test loudly.
//
// Function mode takes precedence when ConfirmFn is non-nil.
type FakePrompter struct {
	answers []bool
	Asked   []string
	// ConfirmFn, when non-nil, supersedes the queued-answer path and is
	// invoked per call. Tests that need to assert "Confirm must not be
	// called" can set this to a function that calls t.Fatalf.
	ConfirmFn func(ctx context.Context, question string) (bool, error)
	// Lines is the FIFO queue of ReadLine return values, recorded prompts
	// land in AskedLines. ReadLineErr is returned (with "" as the value)
	// for every ReadLine call when non-nil. ReadLineFn, when set, takes
	// precedence over Lines and ReadLineErr.
	Lines       []string
	AskedLines  []string
	ReadLineErr error
	ReadLineFn  func(ctx context.Context, prompt string) (string, error)
}

// NewFakePrompter returns a FakePrompter pre-loaded with the given answers.
func NewFakePrompter(answers []bool) *FakePrompter {
	return &FakePrompter{answers: append([]bool(nil), answers...)}
}

// Confirm records the question and dispatches to ConfirmFn (if set) or the
// queued-answer path.
func (f *FakePrompter) Confirm(ctx context.Context, question string) (bool, error) {
	f.Asked = append(f.Asked, question)
	if f.ConfirmFn != nil {
		return f.ConfirmFn(ctx, question)
	}
	if len(f.answers) == 0 {
		panic("FakePrompter exhausted — test asked more questions than expected")
	}
	ans := f.answers[0]
	f.answers = f.answers[1:]
	return ans, nil
}

// ReadLine records the prompt and dispatches to ReadLineFn (if set), then
// ReadLineErr (if set), then the queued-line path. Unlike Confirm, which
// panics on exhaustion, ReadLine returns an error so the typed-bundle-ID
// gate tests can assert the cmd path printed the right "did not match"
// message rather than the test crashing on a panic.
func (f *FakePrompter) ReadLine(ctx context.Context, prompt string) (string, error) {
	f.AskedLines = append(f.AskedLines, prompt)
	if f.ReadLineFn != nil {
		return f.ReadLineFn(ctx, prompt)
	}
	if f.ReadLineErr != nil {
		return "", f.ReadLineErr
	}
	if len(f.Lines) == 0 {
		return "", errors.New("FakePrompter: no more queued lines")
	}
	line := f.Lines[0]
	f.Lines = f.Lines[1:]
	return line, nil
}

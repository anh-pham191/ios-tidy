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

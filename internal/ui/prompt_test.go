package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
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

func TestFakePrompter_ConfirmFnTakesPrecedenceOverQueue(t *testing.T) {
	fp := NewFakePrompter([]bool{true}) // queue should be ignored
	fp.ConfirmFn = func(_ context.Context, q string) (bool, error) {
		if q != "go?" {
			t.Fatalf("ConfirmFn q = %q, want %q", q, "go?")
		}
		return false, errors.New("nope")
	}
	got, err := fp.Confirm(context.Background(), "go?")
	if got != false {
		t.Fatalf("Confirm = %v, want false", got)
	}
	if err == nil || err.Error() != "nope" {
		t.Fatalf("Confirm err = %v, want 'nope'", err)
	}
	if len(fp.Asked) != 1 || fp.Asked[0] != "go?" {
		t.Fatalf("Asked = %v, want [go?]", fp.Asked)
	}
}

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

func TestFakePrompter_ReadLine_returnsQueuedLines(t *testing.T) {
	fp := &FakePrompter{Lines: []string{"first", "second"}}
	ctx := context.Background()

	got1, err := fp.ReadLine(ctx, "prompt1?")
	if err != nil || got1 != "first" {
		t.Fatalf("ReadLine #1 = (%q, %v), want (\"first\", nil)", got1, err)
	}
	got2, err := fp.ReadLine(ctx, "prompt2?")
	if err != nil || got2 != "second" {
		t.Fatalf("ReadLine #2 = (%q, %v), want (\"second\", nil)", got2, err)
	}
	wantPrompts := []string{"prompt1?", "prompt2?"}
	if len(fp.AskedLines) != len(wantPrompts) {
		t.Fatalf("AskedLines = %v, want %v", fp.AskedLines, wantPrompts)
	}
	for i, p := range wantPrompts {
		if fp.AskedLines[i] != p {
			t.Fatalf("AskedLines[%d] = %q, want %q", i, fp.AskedLines[i], p)
		}
	}
}

func TestFakePrompter_ReadLine_errorsWhenExhausted(t *testing.T) {
	fp := &FakePrompter{}
	got, err := fp.ReadLine(context.Background(), "prompt?")
	if got != "" {
		t.Errorf("ReadLine = %q, want empty", got)
	}
	if err == nil {
		t.Fatalf("ReadLine err = nil, want error when queue exhausted")
	}
	if !strings.Contains(err.Error(), "no more queued lines") {
		t.Errorf("err = %v, want 'no more queued lines'", err)
	}
	// Even on error, the call should be recorded so tests can assert that
	// the gate ran.
	if len(fp.AskedLines) != 1 || fp.AskedLines[0] != "prompt?" {
		t.Errorf("AskedLines = %v, want [prompt?]", fp.AskedLines)
	}
}

func TestFakePrompter_ReadLine_ReadLineErrTakesPrecedence(t *testing.T) {
	want := errors.New("queued err")
	fp := &FakePrompter{Lines: []string{"unused"}, ReadLineErr: want}
	got, err := fp.ReadLine(context.Background(), "prompt?")
	if got != "" {
		t.Errorf("ReadLine = %q, want empty when ReadLineErr set", got)
	}
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestStdinPrompter_ReadLine_trimsCRLF(t *testing.T) {
	var stderr bytes.Buffer
	p := NewStdinPrompter(strings.NewReader("hello\r\n"), &stderr)
	got, err := p.ReadLine(context.Background(), "Type bundle:")
	if err != nil {
		t.Fatalf("ReadLine err = %v, want nil", err)
	}
	if got != "hello" {
		t.Fatalf("ReadLine = %q, want %q", got, "hello")
	}
	if !strings.Contains(stderr.String(), "Type bundle:") {
		t.Fatalf("stderr = %q, want it to contain the prompt", stderr.String())
	}
}

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

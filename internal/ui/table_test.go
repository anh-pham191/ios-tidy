package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderTable_emptyRowsWritesHeaderOnly(t *testing.T) {
	var buf bytes.Buffer
	header := []string{"UDID", "NAME"}

	if err := RenderTable(&buf, header, nil); err != nil {
		t.Fatalf("RenderTable err = %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "UDID") {
		t.Errorf("output should start with header, got %q", got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("empty rows should write exactly one (header) line, got %d lines: %q", len(lines), got)
	}
}

func TestRenderTable_columnsAlignToWidestCell(t *testing.T) {
	var buf bytes.Buffer
	header := []string{"UDID", "NAME"}
	rows := [][]string{
		{"AAAA", "iPhone"},
		{"BBBBBBBB", "X"},
	}

	if err := RenderTable(&buf, header, rows); err != nil {
		t.Fatalf("RenderTable err = %v", err)
	}

	// Column 1 width = len("BBBBBBBB") = 8. Header line should be:
	//   "UDID    " + "  " + "NAME" + "\n"
	want := "UDID      NAME\nAAAA      iPhone\nBBBBBBBB  X\n"
	if buf.String() != want {
		t.Errorf("RenderTable output mismatch.\n got: %q\nwant: %q", buf.String(), want)
	}
}

func TestRenderTable_shortRowsPadWithEmptyCells(t *testing.T) {
	var buf bytes.Buffer
	header := []string{"A", "B", "C"}
	rows := [][]string{
		{"1", "2"}, // missing third column
	}

	if err := RenderTable(&buf, header, rows); err != nil {
		t.Fatalf("RenderTable err = %v", err)
	}

	// Trace of writeRow for header [A B C] widths=[1 1 1]:
	//   i=0 (not last): Fprintf("%-1s%s", "A", "  ") -> "A  "
	//   i=1 (not last): Fprintf("%-1s%s", "B", "  ") -> "B  "
	//   i=2 (last col): Fprint("C")                  -> "C"
	//   newline -> "A  B  C\n"
	// Trace for row ["1","2"] (third cell empty), widths=[1 1 1]:
	//   i=0 (not last): Fprintf("%-1s%s", "1", "  ") -> "1  "
	//   i=1 (not last): Fprintf("%-1s%s", "2", "  ") -> "2  "
	//   i=2 (last col): Fprint("")                   -> ""
	//   newline -> "1  2  \n"
	want := "A  B  C\n1  2  \n"
	if buf.String() != want {
		t.Errorf("RenderTable output mismatch.\n got: %q\nwant: %q", buf.String(), want)
	}
}

func TestDashIfEmpty_returnsDashForEmpty(t *testing.T) {
	if got := DashIfEmpty(""); got != "-" {
		t.Errorf("DashIfEmpty(\"\") = %q, want %q", got, "-")
	}
}

func TestDashIfEmpty_passesNonEmptyThrough(t *testing.T) {
	if got := DashIfEmpty("iPhone One"); got != "iPhone One" {
		t.Errorf("DashIfEmpty(non-empty) = %q, want unchanged input", got)
	}
}

func TestTable_Render_rightAlignsNumericColumns(t *testing.T) {
	tbl := NewTable("bundle", "name", "size")
	tbl.AddRow("com.a", "App A", "1.2 GB")
	tbl.AddRow("com.bbbbb", "B", "12 KB")

	got := tbl.Render([]Alignment{AlignLeft, AlignLeft, AlignRight})

	// Header line, separator, two rows.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", len(lines), got)
	}
	// The size column header "size" is 4 chars; the widest cell "1.2 GB" is 6.
	// Right-aligned means "1.2 GB" sits flush right and "12 KB" is padded on
	// the left with one space.
	if !strings.HasSuffix(lines[2], "1.2 GB") {
		t.Fatalf("row 1 last column not right-aligned:\n%q", lines[2])
	}
	if !strings.HasSuffix(lines[3], " 12 KB") {
		t.Fatalf("row 2 last column not right-aligned with leading space:\n%q", lines[3])
	}
}

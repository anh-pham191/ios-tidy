// Package ui contains plain-text rendering helpers for ios-tidy's CLI
// output. The functions here never touch stdin or stdout directly —
// callers pass in an io.Writer so tests can capture output with a
// bytes.Buffer.
package ui

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// RenderTable writes a left-aligned, space-padded table to w. Column
// widths are computed by taking the longest cell (header or row) per
// column. Cells must not contain newlines — the caller is responsible
// for sanitising input.
//
// Why so simple: we want grep-friendly output, not a TUI. Anything
// fancier (colour, box-drawing) would block scripting and add a dep.
func RenderTable(w io.Writer, header []string, rows [][]string) error {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i := 0; i < len(widths) && i < len(r); i++ {
			if l := utf8.RuneCountInString(r[i]); l > widths[i] {
				widths[i] = l
			}
		}
	}

	if err := writeRow(w, header, widths); err != nil {
		return err
	}
	for _, r := range rows {
		if err := writeRow(w, r, widths); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(w io.Writer, cells []string, widths []int) error {
	for i, width := range widths {
		var cell string
		if i < len(cells) {
			cell = cells[i]
		}
		// Last column: write cell as-is (no padding, no separator) so
		// the line has no trailing whitespace — keeps output
		// grep-friendly.
		if i == len(widths)-1 {
			if _, err := fmt.Fprint(w, cell); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "%-*s%s", width, cell, "  "); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// DashIfEmpty returns "-" for blank cells. Used for untrusted devices
// whose lockdown values we can't read, and for any cold/uninitialised
// values future milestones might encounter (cold-app metadata, etc.).
// Printing an empty string would collapse the column visually and make
// grepping by column position unreliable.
func DashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Alignment controls per-column padding direction for the Table struct's
// Render method. Tests assert on prefix/suffix substrings rather than golden
// files so failures pinpoint the misaligned column directly.
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

// Table is a builder-style text-table renderer. Distinct from the simpler
// RenderTable function above: Table supports per-column alignment and a
// header/body separator line, used by the storage subcommand. RenderTable
// is kept for the devices subcommand's simpler output.
//
// Every cell is a string the caller has already formatted (via FormatBytes,
// fmt.Sprintf, etc.); the type is intentionally not generic.
type Table struct {
	header []string
	rows   [][]string
}

func NewTable(header ...string) *Table {
	return &Table{header: append([]string(nil), header...)}
}

func (t *Table) AddRow(cells ...string) {
	t.rows = append(t.rows, append([]string(nil), cells...))
}

// String renders with all columns left-aligned.
func (t *Table) String() string {
	aligns := make([]Alignment, len(t.header))
	// All AlignLeft by default — zero value.
	return t.Render(aligns)
}

// Render formats the table with per-column alignment. len(aligns) must equal
// len(header); if shorter, missing columns default to AlignLeft.
func (t *Table) Render(aligns []Alignment) string {
	widths := make([]int, len(t.header))
	for i, h := range t.header {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) && utf8.RuneCountInString(c) > widths[i] {
				widths[i] = utf8.RuneCountInString(c)
			}
		}
	}

	getAlign := func(i int) Alignment {
		if i < len(aligns) {
			return aligns[i]
		}
		return AlignLeft
	}

	var b strings.Builder
	writeOneRow := func(cells []string) {
		for i, c := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			pad := widths[i] - utf8.RuneCountInString(c)
			if pad < 0 {
				pad = 0
			}
			if getAlign(i) == AlignRight {
				b.WriteString(strings.Repeat(" ", pad))
				b.WriteString(c)
			} else {
				b.WriteString(c)
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		b.WriteString("\n")
	}

	writeOneRow(t.header)
	// Separator line.
	for i, w := range widths {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(strings.Repeat("-", w))
	}
	b.WriteString("\n")
	for _, r := range t.rows {
		// Pad short rows to header length so widths stay consistent.
		if len(r) < len(t.header) {
			pad := make([]string, len(t.header)-len(r))
			r = append(r, pad...)
		}
		writeOneRow(r)
	}
	return b.String()
}

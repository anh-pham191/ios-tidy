// Package ui contains plain-text rendering helpers for ios-tidy's CLI
// output. The functions here never touch stdin or stdout directly —
// callers pass in an io.Writer so tests can capture output with a
// bytes.Buffer.
package ui

import (
	"fmt"
	"io"
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
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i := 0; i < len(widths) && i < len(r); i++ {
			if l := len(r[i]); l > widths[i] {
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

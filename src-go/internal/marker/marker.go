// Package marker reads the tab-separated marker lines the agent-bus wrapper, its installer
// and declare.py print on stdout (SEAT, MEMBER, TEAM, STATUS, ...). A line of any other kind
// is skipped; a line of a wanted kind with the wrong field count is an error, so a changed
// helper fails loudly instead of yielding half a row.
package marker

import (
	"fmt"
	"strings"
)

// Fields splits each line of out that starts with marker into its tab fields (the marker
// itself is field 0) and checks the count is want.
func Fields(out, marker string, want int) ([][]string, error) {
	var rows [][]string
	for line := range strings.Lines(out) {
		text := strings.TrimRight(line, "\r\n")
		if text != marker && !strings.HasPrefix(text, marker+"\t") {
			continue
		}
		f := strings.Split(text, "\t")
		if len(f) != want {
			return nil, fmt.Errorf("%s line has %d fields, want %d: %q", marker, len(f), want, text)
		}
		rows = append(rows, f)
	}
	return rows, nil
}

// DashEmpty is "" for the "-" a helper prints for an absent value, else s.
func DashEmpty(s string) string {
	if s == "-" {
		return ""
	}
	return s
}

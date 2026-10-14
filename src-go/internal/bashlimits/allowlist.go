package bashlimits

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Finding kinds.
const (
	KindBreach  = "breach"  // an unlisted file breaks a limit
	KindGrew    = "grew"    // a listed file passed its record
	KindTighten = "tighten" // a listed file is under its record
	KindWithin  = "within"  // a listed file is now within every limit
	KindStale   = "stale"   // a listed path is not a checked bash file
	KindAdded   = "added"   // an entry the base ref does not have
	KindRaised  = "raised"  // a record higher than the base ref's
)

// Finding is one thing the gate objects to.
type Finding struct {
	Path string
	Kind string
	Msg  string
}

// Allowlist maps each over-limit file to the measurement it may not exceed: the ratchet. Only
// the code-line and function counts and the two flags are held; they are the limits.
type Allowlist map[string]Measurement

// allowlistHeader leads a written file; ParseAllowlist skips comment lines.
const allowlistHeader = `# Ratchet allowlist for the bash limits (scripts/qa-bash-limits.bash, src-go/CLAUDE.md).
#
# Every bash file over a limit, at its current measurement. A file may shrink and be ported to
# Go; it may never grow or gain a flag, and a new over-limit file does not belong here.
# Records only move down: ` + "`scripts/qa-bash-limits.bash --update`" + ` lowers them and removes
# entries that are within limits or gone. Never edit a number up or add a line by hand; the
# gate compares this file with the base ref's and fails on either.
#
# path <TAB> code lines <TAB> functions <TAB> grandfathered flags (assoc, ui, or -)
`

// ParseAllowlist reads the allowlist file's text.
func ParseAllowlist(text string) (Allowlist, error) {
	list := Allowlist{}
	for n, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		path, m, err := parseRecord(line)
		if err != nil {
			return nil, fmt.Errorf("allowlist line %d: %w", n+1, err)
		}
		if _, dup := list[path]; dup {
			return nil, fmt.Errorf("allowlist line %d: %s is listed twice", n+1, path)
		}
		list[path] = m
	}
	return list, nil
}

func parseRecord(line string) (string, Measurement, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 4 || fields[0] == "" {
		return "", Measurement{}, fmt.Errorf("want 4 tab-separated fields (path, code lines, functions, flags), got %q", line)
	}
	code, err := parseCount(fields[1])
	if err != nil {
		return "", Measurement{}, fmt.Errorf("code lines: %w", err)
	}
	funcs, err := parseCount(fields[2])
	if err != nil {
		return "", Measurement{}, fmt.Errorf("functions: %w", err)
	}
	m := Measurement{Code: code, Funcs: funcs}
	if err := parseFlags(fields[3], &m); err != nil {
		return "", Measurement{}, err
	}
	return fields[0], m, nil
}

func parseCount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a count", s)
	}
	return n, nil
}

func parseFlags(s string, m *Measurement) error {
	if s == "-" {
		return nil
	}
	for flag := range strings.SplitSeq(s, ",") {
		switch flag {
		case FlagAssoc:
			m.Assoc = true
		case FlagUI:
			m.UI = true
		default:
			return fmt.Errorf("unknown flag %q (want %s, %s or -)", flag, FlagAssoc, FlagUI)
		}
	}
	return nil
}

// Format is the file's text: the header, then one sorted line per entry.
func (a Allowlist) Format() string {
	var b strings.Builder
	b.WriteString(allowlistHeader)
	for _, path := range slices.Sorted(maps.Keys(a)) {
		m := a[path]
		flags := "-"
		if f := m.Flags(); len(f) > 0 {
			flags = strings.Join(f, ",")
		}
		fmt.Fprintf(&b, "%s\t%d\t%d\t%s\n", path, m.Code, m.Funcs, flags)
	}
	return b.String()
}

// grewPast names what in m exceeds the record, or "" when nothing does.
func grewPast(m, rec Measurement) string {
	var over []string
	if m.Code > rec.Code {
		over = append(over, fmt.Sprintf("code lines %d > %d", m.Code, rec.Code))
	}
	if m.Funcs > rec.Funcs {
		over = append(over, fmt.Sprintf("functions %d > %d", m.Funcs, rec.Funcs))
	}
	if m.Assoc && !rec.Assoc {
		over = append(over, "gained an associative array")
	}
	if m.UI && !rec.UI {
		over = append(over, "gained an interactive UI")
	}
	return strings.Join(over, ", ")
}

// shrankBelow names what in m is under the record, or "" when nothing is.
func shrankBelow(m, rec Measurement) string {
	var under []string
	if m.Code < rec.Code {
		under = append(under, fmt.Sprintf("code lines %d < %d", m.Code, rec.Code))
	}
	if m.Funcs < rec.Funcs {
		under = append(under, fmt.Sprintf("functions %d < %d", m.Funcs, rec.Funcs))
	}
	if rec.Assoc && !m.Assoc {
		under = append(under, "no longer uses an associative array")
	}
	if rec.UI && !m.UI {
		under = append(under, "no longer runs an interactive UI")
	}
	return strings.Join(under, ", ")
}

// Evaluate checks every scanned (non-exempt) file against the limits and the allowlist.
func Evaluate(list Allowlist, scanned map[string]Measurement) []Finding {
	var out []Finding
	for _, path := range slices.Sorted(maps.Keys(scanned)) {
		rec, listed := list[path]
		out = append(out, evaluateFile(path, scanned[path], rec, listed)...)
	}
	for _, path := range slices.Sorted(maps.Keys(list)) {
		if _, ok := scanned[path]; ok {
			continue
		}
		out = append(out, Finding{path, KindStale, path + ": allowlisted, but not a checked bash file (deleted, renamed or exempt): remove the entry (scripts/qa-bash-limits.bash --update)"})
	}
	return out
}

func evaluateFile(path string, m, rec Measurement, listed bool) []Finding {
	if !listed {
		if breaches := m.Breaches(); len(breaches) > 0 {
			return []Finding{{path, KindBreach, fmt.Sprintf("%s: %s; tooling is Go, bash only for very small helpers: write it in Go (src-go/CLAUDE.md)", path, strings.Join(breaches, "; "))}}
		}
		return nil
	}
	if m.WithinLimits() {
		return []Finding{{path, KindWithin, path + ": now within every limit: remove its allowlist entry (scripts/qa-bash-limits.bash --update)"}}
	}
	if over := grewPast(m, rec); over != "" {
		return []Finding{{path, KindGrew, fmt.Sprintf("%s: grew past its allowlist record (%s); the ratchet only shrinks, so cut it back or port it to Go (src-go/CLAUDE.md)", path, over)}}
	}
	if under := shrankBelow(m, rec); under != "" {
		return []Finding{{path, KindTighten, fmt.Sprintf("%s: under its allowlist record (%s): tighten the record (scripts/qa-bash-limits.bash --update)", path, under)}}
	}
	return nil
}

// Tighten is the allowlist after a downward-only update: each record lowered to the file's
// measurement where that is lower, entries within limits or not scanned dropped, nothing
// raised or added.
func Tighten(list Allowlist, scanned map[string]Measurement) Allowlist {
	out := Allowlist{}
	for path, rec := range list {
		m, ok := scanned[path]
		if !ok || m.WithinLimits() {
			continue
		}
		out[path] = Measurement{
			Code:  min(rec.Code, m.Code),
			Funcs: min(rec.Funcs, m.Funcs),
			Assoc: rec.Assoc && m.Assoc,
			UI:    rec.UI && m.UI,
		}
	}
	return out
}

// Seed is a first allowlist: every scanned file over a limit, at its current measurement.
func Seed(scanned map[string]Measurement) Allowlist {
	out := Allowlist{}
	for path, m := range scanned {
		if !m.WithinLimits() {
			out[path] = m
		}
	}
	return out
}

// CompareBase finds what cur gained over base, the same file at the base ref: an entry base
// lacks, or a record higher than base's. Lowering and removing are what the ratchet is for.
func CompareBase(base, cur Allowlist) []Finding {
	var out []Finding
	for _, path := range slices.Sorted(maps.Keys(cur)) {
		old, had := base[path]
		switch {
		case !had:
			out = append(out, Finding{path, KindAdded, path + ": a new allowlist entry; the allowlist only shrinks, so write the file in Go or bring it within the limits (src-go/CLAUDE.md)"})
		case grewPast(cur[path], old) != "":
			out = append(out, Finding{path, KindRaised, fmt.Sprintf("%s: allowlist record raised (%s); records only go down", path, grewPast(cur[path], old))})
		}
	}
	return out
}

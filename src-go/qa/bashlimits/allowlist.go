package bashlimits

import (
	"fmt"
	"maps"
	"path"
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
	KindStale   = "stale"   // a listed file is gone
	KindOrphan  = "orphan"  // a listed file exists but is not a checked bash file
	KindAdded   = "added"   // an entry the base ref does not have
	KindRaised  = "raised"  // a record higher than the base ref's
	KindDropped = "dropped" // an entry the base ref has, removed while its file lives on
	KindMeasure = "measure" // the measurer version went backwards
)

// Finding is one thing the gate objects to.
type Finding struct {
	Path string
	Kind string
	Msg  string
}

// Allowlist maps each over-limit file to the measurement it may not exceed: the ratchet.
type Allowlist map[string]Measurement

// File is a parsed allowlist file: the measurer version its records were taken with, and the
// records. When Version is not MeasurerVersion the records hold only their paths: another
// measure's numbers mean nothing here.
type File struct {
	Version int
	Records Allowlist
}

const measurerPrefix = "# measurer: "

// allowlistHeader leads a written file; the parser skips comment lines.
const allowlistHeader = `# Ratchet allowlist for the bash limits (scripts/qa-bash-limits.bash, src-go/CLAUDE.md).
#
# Every bash file over a limit, at its current measurement. A file may shrink and be ported to
# Go; it may never grow or gain a flag, and a new over-limit file does not belong here.
# Records only move down: ` + "`scripts/qa-bash-limits.bash --update`" + ` lowers them and removes
# entries that are within limits or whose file is gone; it refuses to drop one whose file still
# exists. Never edit a number up or add a line by hand; the gate compares this file with the
# merge-base with the default branch and fails on either.
#
# The one exception is a new measurer: when the way files are measured changes, its version
# rises and ` + "`--remeasure`" + ` rewrites every record at the new measure, in the commit that
# changes the measurer. The version below is the measurer these records were taken with.
#
# path <TAB> code lines <TAB> functions <TAB> statements <TAB> grandfathered flags (assoc, ui, data or -)
`

// Format is the file's text: the header, the measurer version, then one sorted line per entry.
func (a Allowlist) Format() string {
	var b strings.Builder
	b.WriteString(allowlistHeader)
	fmt.Fprintf(&b, "%s%d\n", measurerPrefix, MeasurerVersion)
	for _, p := range slices.Sorted(maps.Keys(a)) {
		m := a[p]
		flags := "-"
		if f := m.Flags(); len(f) > 0 {
			flags = strings.Join(f, ",")
		}
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%s\n", p, m.Code, m.Funcs, m.Stmts, flags)
	}
	return b.String()
}

// ParseAllowlist reads the allowlist file's text. A file with no measurer line was written by
// the first measurer, version 1.
func ParseAllowlist(text string) (File, error) {
	f := File{Version: versionOf(text), Records: Allowlist{}}
	for n, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, measurerPrefix); ok {
			if _, err := strconv.Atoi(strings.TrimSpace(rest)); err != nil {
				return File{}, fmt.Errorf("allowlist line %d: %q is not a measurer version", n+1, line)
			}
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, m, err := parseRecord(line, f.Version)
		if err != nil {
			return File{}, fmt.Errorf("allowlist line %d: %w", n+1, err)
		}
		if _, dup := f.Records[p]; dup {
			return File{}, fmt.Errorf("allowlist line %d: %s is listed twice", n+1, p)
		}
		f.Records[p] = m
	}
	return f, nil
}

// parseRecord reads one record. Another measurer's records may have another shape, so for
// them only the path is read and the numbers are left zero.
func parseRecord(line string, version int) (string, Measurement, error) {
	fields := strings.Split(line, "\t")
	if fields[0] == "" {
		return "", Measurement{}, fmt.Errorf("a record with no path: %q", line)
	}
	if version != MeasurerVersion {
		return fields[0], Measurement{}, nil
	}
	if len(fields) != 5 {
		return "", Measurement{}, fmt.Errorf("want 5 tab-separated fields (path, code lines, functions, statements, flags), got %q", line)
	}
	var counts [3]int
	for i, name := range []string{"code lines", "functions", "statements"} {
		n, err := strconv.Atoi(fields[i+1])
		if err != nil || n < 0 {
			return "", Measurement{}, fmt.Errorf("%s: %q is not a count", name, fields[i+1])
		}
		counts[i] = n
	}
	m := Measurement{Code: counts[0], Funcs: counts[1], Stmts: counts[2]}
	if err := parseFlags(fields[4], &m); err != nil {
		return "", Measurement{}, err
	}
	return fields[0], m, nil
}

// versionOf is the measurer version a text declares, 1 when it declares none.
func versionOf(text string) int {
	for line := range strings.SplitSeq(text, "\n") {
		if rest, ok := strings.CutPrefix(line, measurerPrefix); ok {
			if v, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && v > 0 {
				return v
			}
		}
	}
	return 1
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
		case FlagData:
			m.Data = true
		default:
			return fmt.Errorf("unknown flag %q (want %s, %s, %s or -)", flag, FlagAssoc, FlagUI, FlagData)
		}
	}
	return nil
}

// flagChange is one boolean finding as it stands in a measurement and in a record.
type flagChange struct {
	now, was bool
	gained   string
	lost     string
}

func flagChanges(m, rec Measurement) []flagChange {
	return []flagChange{
		{m.Assoc, rec.Assoc, "gained an associative array", "no longer uses an associative array"},
		{m.UI, rec.UI, "gained an interactive UI", "no longer runs an interactive UI"},
		{m.Data, rec.Data, "gained data parsing", "no longer parses data"},
	}
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
	if m.Stmts > rec.Stmts {
		over = append(over, fmt.Sprintf("statements %d > %d", m.Stmts, rec.Stmts))
	}
	for _, c := range flagChanges(m, rec) {
		if c.now && !c.was {
			over = append(over, c.gained)
		}
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
	if m.Stmts < rec.Stmts {
		under = append(under, fmt.Sprintf("statements %d < %d", m.Stmts, rec.Stmts))
	}
	for _, c := range flagChanges(m, rec) {
		if c.was && !c.now {
			under = append(under, c.lost)
		}
	}
	return strings.Join(under, ", ")
}

// removalProblem says why a listed path may not simply disappear from the allowlist, or ""
// when it may: its file was measured (it is within limits, or is judged on its own), or it is
// gone and nothing exempt looks like its new home. A file that still exists but is not a
// checked bash file, or that went away next to an exempt file of the same name, has been
// moved out of the gate's sight, not ported. The one excuse is a deployed Go command of the
// file's name (src-go/cmd/<name>/main.go) whose row in scripts/go-binary-suites.tsv names that
// exempt file as its suite: the table is where a ported tool's bash suite is declared to drive
// the binary, so a stub command and a renamed script do not qualify on their own.
func removalProblem(p string, scan Scan) string {
	if _, measured := scan.Measured[p]; measured {
		return ""
	}
	switch {
	case scan.Exempt[p]:
		return p + " still exists and is exempt"
	case scan.Present[p]:
		return p + " still exists but is not a bash file the gate checks"
	}
	twin := exemptTwin(p, scan)
	if twin == "" {
		return ""
	}
	name := strings.TrimSuffix(path.Base(p), path.Ext(p))
	if scan.Present["src-go/cmd/"+name+"/main.go"] && scan.Suites[name][twin] {
		return "" // ported: the Go command carries the name and the table gives it this suite
	}
	return p + " is gone but " + twin + " is exempt and has its name: a file moved into an exempt path is not ported (a Go command src-go/cmd/" + name + " with " + twin + " in " + suitesTable + " would be)"
}

// exemptTwin is an exempt bash file named like p, with or without a "test-" prefix, or "".
func exemptTwin(p string, scan Scan) string {
	stem := func(q string) string { return strings.TrimSuffix(path.Base(q), path.Ext(q)) }
	name := stem(p)
	for _, e := range slices.Sorted(maps.Keys(scan.Exempt)) {
		if other := stem(e); other == name || other == "test-"+name || "test-"+other == name {
			return e
		}
	}
	return ""
}

// Evaluate checks every scanned (non-exempt) file against the limits and the allowlist.
func Evaluate(list Allowlist, scan Scan) []Finding {
	var out []Finding
	for _, p := range slices.Sorted(maps.Keys(scan.Measured)) {
		rec, listed := list[p]
		out = append(out, evaluateFile(p, scan.Measured[p], rec, listed)...)
	}
	for _, p := range slices.Sorted(maps.Keys(list)) {
		if _, ok := scan.Measured[p]; ok {
			continue
		}
		if why := removalProblem(p, scan); why != "" {
			out = append(out, Finding{p, KindOrphan, "allowlisted, but " + why + ": an entry stays until its file is ported or within limits"})
			continue
		}
		out = append(out, Finding{p, KindStale, p + ": allowlisted, but the file is gone: remove the entry (scripts/qa-bash-limits.bash --update)"})
	}
	return out
}

func evaluateFile(p string, m, rec Measurement, listed bool) []Finding {
	if !listed {
		if breaches := m.Breaches(); len(breaches) > 0 {
			return []Finding{{p, KindBreach, fmt.Sprintf("%s: %s; tooling is Go, bash only for very small helpers: write it in Go (src-go/CLAUDE.md)", p, strings.Join(breaches, "; "))}}
		}
		return nil
	}
	if m.WithinLimits() {
		return []Finding{{p, KindWithin, p + ": now within every limit: remove its allowlist entry (scripts/qa-bash-limits.bash --update)"}}
	}
	if over := grewPast(m, rec); over != "" {
		return []Finding{{p, KindGrew, fmt.Sprintf("%s: grew past its allowlist record (%s); the ratchet only shrinks, so cut it back or port it to Go (src-go/CLAUDE.md)", p, over)}}
	}
	if under := shrankBelow(m, rec); under != "" {
		return []Finding{{p, KindTighten, fmt.Sprintf("%s: under its allowlist record (%s): tighten the record (scripts/qa-bash-limits.bash --update)", p, under)}}
	}
	return nil
}

// dropProblems are the entries of list that a rewrite would drop although their file lives on
// outside the gate's sight.
func dropProblems(list Allowlist, scan Scan) []Finding {
	var out []Finding
	for _, p := range slices.Sorted(maps.Keys(list)) {
		if why := removalProblem(p, scan); why != "" {
			out = append(out, Finding{p, KindOrphan, "refusing to drop the entry: " + why})
		}
	}
	return out
}

// Tighten is the allowlist after a downward-only update: each record lowered to the file's
// measurement where that is lower, entries within limits or whose file is gone dropped,
// nothing raised or added. The findings are entries it must not drop; with any, the result is
// not to be written.
func Tighten(list Allowlist, scan Scan) (Allowlist, []Finding) {
	out := Allowlist{}
	for p, rec := range list {
		m, ok := scan.Measured[p]
		if !ok || m.WithinLimits() {
			continue
		}
		out[p] = Measurement{
			Code:  min(rec.Code, m.Code),
			Funcs: min(rec.Funcs, m.Funcs),
			Stmts: min(rec.Stmts, m.Stmts),
			Assoc: rec.Assoc && m.Assoc,
			UI:    rec.UI && m.UI,
			Data:  rec.Data && m.Data,
		}
	}
	return out, dropProblems(list, scan)
}

// Seed is an allowlist of every scanned file over a limit, at its current measurement.
func Seed(scanned map[string]Measurement) Allowlist {
	out := Allowlist{}
	for p, m := range scanned {
		if !m.WithinLimits() {
			out[p] = m
		}
	}
	return out
}

// Remeasure is the allowlist rewritten at the current measure: Seed, unless it would drop an
// entry whose file lives on outside the gate's sight.
func Remeasure(old Allowlist, scan Scan) (Allowlist, []Finding) {
	return Seed(scan.Measured), dropProblems(old, scan)
}

// CompareBase finds what cur gained over base, the same file at the base ref. Within one
// measurer an entry base lacks, or a record higher than base's, is a finding; across a newer
// measurer the records are rewritten at the new measure, so neither is. Either way an entry
// that vanished while its file lives on is a finding: lowering and removing are what the
// ratchet is for, hiding a file is not.
func CompareBase(base, cur File, scan Scan) []Finding {
	var out []Finding
	switch {
	case cur.Version < base.Version:
		out = append(out, Finding{"", KindMeasure, fmt.Sprintf("allowlist measurer version %d is older than the base's %d", cur.Version, base.Version)})
	case cur.Version == base.Version:
		for _, p := range slices.Sorted(maps.Keys(cur.Records)) {
			old, had := base.Records[p]
			switch {
			case !had:
				out = append(out, Finding{p, KindAdded, p + ": a new allowlist entry; the allowlist only shrinks, so write the file in Go or bring it within the limits (src-go/CLAUDE.md)"})
			case grewPast(cur.Records[p], old) != "":
				out = append(out, Finding{p, KindRaised, fmt.Sprintf("%s: allowlist record raised (%s); records only go down", p, grewPast(cur.Records[p], old))})
			}
		}
	}
	for _, p := range slices.Sorted(maps.Keys(base.Records)) {
		if _, kept := cur.Records[p]; kept {
			continue
		}
		if why := removalProblem(p, scan); why != "" {
			out = append(out, Finding{p, KindDropped, "allowlist entry removed, but " + why})
		}
	}
	return out
}

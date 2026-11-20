package watchlist

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

// The values of a manifest row's mode and status.
const (
	modeBaseline  = "baseline"
	modeDiff      = "diff"
	statusPending = "pending"
	statusDone    = "done"
)

const manifestName = "manifest.tsv"

const manifestHeader = `# A watchlist study run (CLAUDE/Watchlist.md): one row per repository studied.
# Written by watchlist run new and run mark; read strictly, a malformed row is an error.
#
# repo <TAB> group <TAB> mode <TAB> from_sha <TAB> to_sha <TAB> status
#   mode      baseline (the whole repository) or diff (from_sha..to_sha)
#   from_sha  the commit of the repository's newest done study in an earlier run, or -
#   to_sha    the commit this run studies (the clone's HEAD when the run was made)
#   status    pending, or done once the group's report records the study (run mark)
`

var (
	runIDPattern = regexp.MustCompile(`^(baseline|diff)-([0-9]{8}-[0-9]{4})$`)
	shaPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// RunRow is one repository in a run's manifest.
type RunRow struct {
	Repo    string
	Group   string
	Mode    string
	FromSHA string
	ToSHA   string
	Status  string
}

// RunsDir holds one directory per run under root.
func RunsDir(root string) string { return filepath.Join(root, "CLAUDE", "WatchlistRuns") }

// parseRunID is R1: <mode>-<yyyymmdd>-<hhmm>, a real date and time. It returns the mode and the
// stamp, whose text orders as its time does.
func parseRunID(id string) (string, string, error) {
	m := runIDPattern.FindStringSubmatch(id)
	if m == nil {
		return "", "", fmt.Errorf("run id %q is not <baseline|diff>-<yyyymmdd>-<hhmm>", id)
	}
	if _, err := time.Parse("20060102-1504", m[2]); err != nil {
		return "", "", fmt.Errorf("run id %q has no real date and time: %w", id, err)
	}
	return m[1], m[2], nil
}

func checkRunRow(r RunRow) error {
	var errs []error
	errs = append(errs, checkRepo(r.Repo))
	if r.Group == none {
		errs = append(errs, errors.New("a run row needs a group"))
	} else {
		errs = append(errs, checkGroup(r.Group))
	}
	if r.Mode != modeBaseline && r.Mode != modeDiff {
		errs = append(errs, fmt.Errorf("mode %q is not baseline or diff", r.Mode))
	}
	if r.FromSHA != none && !shaPattern.MatchString(r.FromSHA) {
		errs = append(errs, fmt.Errorf("from_sha %q is not 40 lowercase hex digits or -", r.FromSHA))
	}
	if r.Mode == modeDiff && r.FromSHA == none {
		errs = append(errs, errors.New("a diff row needs a from_sha"))
	}
	if !shaPattern.MatchString(r.ToSHA) {
		errs = append(errs, fmt.Errorf("to_sha %q is not 40 lowercase hex digits", r.ToSHA))
	}
	if r.Status != statusPending && r.Status != statusDone {
		errs = append(errs, fmt.Errorf("status %q is not pending or done", r.Status))
	}
	return errors.Join(errs...)
}

// ParseManifest reads a run's manifest strictly; file names it in the errors.
func ParseManifest(data []byte, file string) ([]RunRow, error) {
	var rows []RunRow
	seen := map[string]int{}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			return nil, fmt.Errorf("%s:%d: want 6 tab-separated fields (repo, group, mode, from_sha, to_sha, status), got %d", file, i+1, len(f))
		}
		r := RunRow{Repo: f[0], Group: f[1], Mode: f[2], FromSHA: f[3], ToSHA: f[4], Status: f[5]}
		if err := checkRunRow(r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		key := strings.ToLower(r.Repo)
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s:%d: %s is already in the run on line %d", file, i+1, r.Repo, first)
		}
		seen[key] = i + 1
		rows = append(rows, r)
	}
	return rows, nil
}

func sortRunRows(rows []RunRow) {
	slices.SortFunc(rows, func(a, b RunRow) int {
		return cmp.Or(strings.Compare(a.Group, b.Group), strings.Compare(a.Repo, b.Repo))
	})
}

// FormatManifest is the manifest text: the header, then the rows sorted by group and repo.
func FormatManifest(rows []RunRow) []byte {
	sorted := slices.Clone(rows)
	sortRunRows(sorted)
	var b strings.Builder
	b.WriteString(manifestHeader)
	for _, r := range sorted {
		b.WriteString(strings.Join([]string{r.Repo, r.Group, r.Mode, r.FromSHA, r.ToSHA, r.Status}, "\t") + "\n")
	}
	return []byte(b.String())
}

// run is one run read from disk.
type run struct {
	id    string
	stamp string
	rows  []RunRow
}

func (r run) dir(root string) string      { return filepath.Join(RunsDir(root), r.id) }
func (r run) manifest(root string) string { return filepath.Join(r.dir(root), manifestName) }

func readRun(root, id string) (run, error) {
	_, stamp, err := parseRunID(id)
	if err != nil {
		return run{}, err
	}
	r := run{id: id, stamp: stamp}
	path := filepath.Clean(r.manifest(root))
	data, err := os.ReadFile(path)
	if err != nil {
		return run{}, fmt.Errorf("cannot read the manifest of run %s: %w", id, err)
	}
	if r.rows, err = ParseManifest(data, path); err != nil {
		return run{}, err
	}
	return r, nil
}

// loadRuns is every run, oldest first. No runs directory is no runs; anything in it that is not
// a run is an error.
func loadRuns(root string) ([]run, error) {
	entries, err := os.ReadDir(RunsDir(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list the runs: %w", err)
	}
	var runs []run
	for _, e := range entries {
		if !e.IsDir() {
			return nil, fmt.Errorf("%s is not a run directory", filepath.Join(RunsDir(root), e.Name()))
		}
		r, readErr := readRun(root, e.Name())
		if readErr != nil {
			return nil, readErr
		}
		runs = append(runs, r)
	}
	slices.SortFunc(runs, func(a, b run) int { return strings.Compare(a.stamp, b.stamp) })
	for i := 1; i < len(runs); i++ {
		if runs[i].stamp == runs[i-1].stamp {
			return nil, fmt.Errorf("runs %s and %s share a timestamp, so their order is unknown", runs[i-1].id, runs[i].id)
		}
	}
	return runs, nil
}

// lastStudied is R4: the to_sha of repo's newest done row in a run stamped before before (every
// run when before is empty), or "".
func lastStudied(runs []run, repo, before string) string {
	for i := len(runs) - 1; i >= 0; i-- {
		if before != "" && runs[i].stamp >= before {
			continue
		}
		for _, r := range runs[i].rows {
			if strings.EqualFold(r.Repo, repo) && r.Status == statusDone {
				return r.ToSHA
			}
		}
	}
	return ""
}

// reportGap is M1 for one row: why the report does not record its study, or "" when it does.
func reportGap(report string, r RunRow) string {
	lines := strings.Split(report, "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t\r") == "## "+r.Repo })
	if start < 0 {
		return "the report has no heading ## " + r.Repo
	}
	for _, l := range lines[start+1:] {
		if strings.HasPrefix(l, "## ") {
			break
		}
		if strings.HasPrefix(l, "Studied: ") && strings.Contains(l, r.ToSHA) {
			return ""
		}
	}
	return "no line beginning 'Studied: ' with " + r.ToSHA + " under its heading"
}

// runCommand is the run verbs' dispatch and command-line grammar.
func (a *App) runCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageMistake("run takes new, rows, mark or status")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "new":
		return a.runNewArgs(ctx, rest)
	case "rows":
		if len(rest) < 1 || len(rest) > 2 {
			return usageMistake("run rows takes <run-id> [group]")
		}
		group := ""
		if len(rest) == 2 {
			group = rest[1]
		}
		return a.runRows(rest[0], group)
	case "mark":
		if len(rest) != 2 {
			return usageMistake("run mark takes <run-id> <group>")
		}
		return a.runMark(rest[0], rest[1])
	case "status":
		if len(rest) > 1 {
			return usageMistake("run status takes at most one run id")
		}
		id := ""
		if len(rest) == 1 {
			id = rest[0]
		}
		return a.runStatus(id)
	default:
		return usageMistake("unknown run command " + verb)
	}
}

// selection is run new's filter: the groups and repositories asked for, empty for every one.
type selection struct {
	mode   string
	groups []string
	repos  []string
}

func (a *App) runNewArgs(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageMistake("run new takes <run-id> --mode baseline|diff")
	}
	id, sel := args[0], selection{}
	for rest := args[1:]; len(rest) > 0; rest = rest[2:] {
		if len(rest) < 2 {
			return usageMistake("run new: " + rest[0] + " needs a value")
		}
		switch rest[0] {
		case "--mode":
			if sel.mode != "" {
				return usageMistake("run new: --mode is given twice")
			}
			sel.mode = rest[1]
		case "--group":
			sel.groups = append(sel.groups, rest[1])
		case "--repo":
			sel.repos = append(sel.repos, rest[1])
		default:
			return usageMistake("run new: unknown argument " + rest[0])
		}
	}
	if sel.mode == "" {
		return usageMistake("run new needs --mode baseline|diff")
	}
	return a.runNew(ctx, id, sel)
}

// selected is R3: the study rows the selection names, every one when it names none. A group or
// repository named that matches no study row is an error.
func selected(entries []Entry, sel selection) ([]Entry, error) {
	var study []Entry
	for _, e := range entries {
		if e.State == stateStudy {
			study = append(study, e)
		}
	}
	if len(sel.groups) == 0 && len(sel.repos) == 0 {
		return study, nil
	}
	for _, g := range sel.groups {
		if !slices.ContainsFunc(study, func(e Entry) bool { return e.Group == g }) {
			return nil, fmt.Errorf("no study row is in group %s", g)
		}
	}
	for _, r := range sel.repos {
		if !slices.ContainsFunc(study, func(e Entry) bool { return strings.EqualFold(e.Repo, r) }) {
			return nil, fmt.Errorf("%s is not a study row", r)
		}
	}
	return slices.DeleteFunc(study, func(e Entry) bool {
		return !slices.Contains(sel.groups, e.Group) &&
			!slices.ContainsFunc(sel.repos, func(r string) bool { return strings.EqualFold(e.Repo, r) })
	}), nil
}

// checkNewRun is R1 and R2: a well-formed id of the mode, a new run, and no row left to triage.
func checkNewRun(id, mode string, runs []run, entries []Entry) (string, error) {
	idMode, stamp, err := parseRunID(id)
	if err != nil {
		return "", err
	}
	if idMode != mode {
		return "", fmt.Errorf("run id %s is not a %s run", id, mode)
	}
	for _, r := range runs {
		if r.id == id || r.stamp == stamp {
			return "", fmt.Errorf("run %s already exists at that time (%s)", id, r.id)
		}
	}
	var untriaged []string
	for _, e := range entries {
		if e.State == stateNew {
			untriaged = append(untriaged, e.Repo)
		}
	}
	if len(untriaged) > 0 {
		return "", fmt.Errorf("triage first: %d row(s) are new: %s", len(untriaged), strings.Join(untriaged, ", "))
	}
	return stamp, nil
}

// runNew is R1 to R7.
func (a *App) runNew(ctx context.Context, id string, sel selection) error {
	entries, err := a.load()
	if err != nil {
		return err
	}
	runs, err := loadRuns(a.root)
	if err != nil {
		return err
	}
	stamp, err := checkNewRun(id, sel.mode, runs, entries)
	if err != nil {
		return err
	}
	chosen, err := selected(entries, sel)
	if err != nil {
		return err
	}
	var rows []RunRow
	var shallow []string
	for _, e := range chosen {
		r, isShallow, rowErr := a.runRow(ctx, e, sel.mode, runs, stamp)
		if rowErr != nil {
			return fmt.Errorf("%s: %w", e.Repo, rowErr)
		}
		if isShallow {
			shallow = append(shallow, e.Repo)
		}
		if r != nil {
			rows = append(rows, *r)
		}
	}
	if len(shallow) > 0 {
		return fmt.Errorf("shallow clone(s), which a study would read only in part: %s; run watchlist sync first", strings.Join(shallow, ", "))
	}
	if len(rows) == 0 {
		a.progress("nothing to study: no selected repository has moved since its last study; no run written")
		return nil
	}
	return a.writeNewRun(id, rows)
}

// runRow is R3 to R5 for one study row: its manifest row, nil when a diff run leaves it out, and
// whether its clone is shallow (R7).
func (a *App) runRow(ctx context.Context, e Entry, mode string, runs []run, stamp string) (*RunRow, bool, error) {
	dir := e.CloneDir(a.root)
	present, err := cloneExists(dir)
	if err != nil {
		return nil, false, err
	}
	if !present {
		return nil, false, fmt.Errorf("no clone at %s; run watchlist sync %s", dir, e.Repo)
	}
	shallow, err := a.git.IsShallow(ctx, dir)
	if err != nil {
		return nil, false, fmt.Errorf("shallow: %w", err)
	}
	head, err := a.git.Head(ctx, dir)
	if err != nil {
		return nil, false, fmt.Errorf("head: %w", err)
	}
	r := RunRow{Repo: e.Repo, Group: e.Group, Mode: modeBaseline, FromSHA: none, ToSHA: head, Status: statusPending}
	if from := lastStudied(runs, e.Repo, stamp); from != "" {
		r.FromSHA = from
		if mode == modeDiff {
			if from == head {
				return nil, shallow, nil
			}
			r.Mode = modeDiff
		}
	}
	return &r, shallow, nil
}

// writeNewRun is R6: the manifest in a new run directory, and each group with its count.
func (a *App) writeNewRun(id string, rows []RunRow) error {
	r := run{id: id}
	if err := os.MkdirAll(RunsDir(a.root), 0o750); err != nil {
		return fmt.Errorf("cannot create the runs directory: %w", err)
	}
	if err := os.Mkdir(r.dir(a.root), 0o750); err != nil {
		return fmt.Errorf("cannot create run %s: %w", id, err)
	}
	if err := fsx.WriteAtomic(r.manifest(a.root), "run manifest", FormatManifest(rows), 0o644); err != nil {
		return err
	}
	for _, g := range groupCounts(rows) {
		a.event("%s %d", g.name, g.pending+g.done)
	}
	return nil
}

type groupCount struct {
	name    string
	pending int
	done    int
}

func groupCounts(rows []RunRow) []groupCount {
	var counts []groupCount
	for _, r := range rows {
		i := slices.IndexFunc(counts, func(c groupCount) bool { return c.name == r.Group })
		if i < 0 {
			counts = append(counts, groupCount{name: r.Group})
			i = len(counts) - 1
		}
		if r.Status == statusDone {
			counts[i].done++
		} else {
			counts[i].pending++
		}
	}
	slices.SortFunc(counts, func(a, b groupCount) int { return strings.Compare(a.name, b.name) })
	return counts
}

// groupRows is the run's rows in group, sorted; a group the run does not hold is an error.
func groupRows(r run, group string) ([]RunRow, error) {
	rows := slices.Clone(r.rows)
	sortRunRows(rows)
	if group == "" {
		return rows, nil
	}
	rows = slices.DeleteFunc(rows, func(row RunRow) bool { return row.Group != group })
	if len(rows) == 0 {
		return nil, fmt.Errorf("run %s has no group %s", r.id, group)
	}
	return rows, nil
}

// runRows is W1.
func (a *App) runRows(id, group string) error {
	r, err := readRun(a.root, id)
	if err != nil {
		return err
	}
	entries, err := a.load()
	if err != nil {
		return err
	}
	rows, err := groupRows(r, group)
	if err != nil {
		return err
	}
	var lines []string
	for _, row := range rows {
		i, findErr := find(entries, row.Repo)
		if findErr != nil {
			return fmt.Errorf("run %s: %w", id, findErr)
		}
		e := entries[i]
		lines = append(lines, strings.Join([]string{row.Repo, e.Licence, e.Reuse, row.Mode, row.FromSHA, row.ToSHA, row.Status, e.CloneDir(a.root)}, "\t")+"\n")
	}
	a.p.Print(a.out, strings.Join(lines, ""))
	return nil
}

// runMark is M1 and M2: every pending row of the group passes, or nothing changes.
func (a *App) runMark(id, group string) error {
	if err := checkGroup(group); err != nil || group == none {
		return fmt.Errorf("group %q is not a lower-case kebab name", group)
	}
	r, err := readRun(a.root, id)
	if err != nil {
		return err
	}
	rows, err := groupRows(r, group)
	if err != nil {
		return err
	}
	path := filepath.Join(r.dir(a.root), group+".md")
	report, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("cannot read the report %s: %w", path, err)
	}
	var gaps []string
	for _, row := range rows {
		if row.Status != statusPending {
			continue
		}
		if gap := reportGap(string(report), row); gap != "" {
			gaps = append(gaps, row.Repo+": "+gap)
		}
	}
	if len(gaps) > 0 {
		a.progress("%s", strings.Join(gaps, "\n"))
		return fmt.Errorf("%d row(s) of group %s in run %s are not recorded in %s; nothing was marked", len(gaps), group, id, path)
	}
	var marked []string
	for i := range r.rows {
		if r.rows[i].Group == group && r.rows[i].Status == statusPending {
			r.rows[i].Status = statusDone
			marked = append(marked, r.rows[i].Repo)
		}
	}
	if err := fsx.WriteAtomic(r.manifest(a.root), "run manifest", FormatManifest(r.rows), 0o644); err != nil {
		return err
	}
	slices.Sort(marked)
	for _, repo := range marked {
		a.event("done %s", repo)
	}
	return nil
}

// runStatus is U1.
func (a *App) runStatus(id string) error {
	var r run
	if id == "" {
		runs, err := loadRuns(a.root)
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			return fmt.Errorf("there are no runs yet in %s", RunsDir(a.root))
		}
		r = runs[len(runs)-1]
	} else {
		var err error
		if r, err = readRun(a.root, id); err != nil {
			return err
		}
	}
	a.event("run %s", r.id)
	for _, g := range groupCounts(r.rows) {
		a.event("%s pending %d done %d", g.name, g.pending, g.done)
	}
	rows, err := groupRows(r, "")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Status == statusPending {
			a.event("pending %s %s", row.Repo, row.Group)
		}
	}
	return nil
}

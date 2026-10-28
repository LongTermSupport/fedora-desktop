package vmtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Retention is the lab's own disk policy: what a run, a member and a rebuild need of the host
// (§10), which run overlays may go (E61) and what the sweep evicts from runs/ and bases/ (E51).
// The bridge spool on the shared mount is the Python's (helpers.vmtest.retention sweep-spool),
// because it is opened through spool.py's pinned descriptors, which the bridge helpers share.

const (
	// ramCeilingPercent is the share of the host's memory a guest may be given.
	ramCeilingPercent = 75
	// defaultKeepRuns is how many passing runs the sweep keeps; every run that did not pass stays.
	defaultKeepRuns = 10
	desktopPrefix   = "desktop-"
	buildSuffix     = ".build"
	responseFile    = "response.json"
	// runIDLayout is the UTC stamp a run id starts with: <stamp>-<scenario> (E34).
	runIDLayout = "20060102T150405Z"
	notARunID   = "not a run directory name (<UTC stamp>-<scenario>)"
)

var runIDRE = regexp.MustCompile(`^([0-9]{8}T[0-9]{6}Z)-(.+)$`)

// runFacts is what the prune rule reads of one run directory.
type runFacts struct {
	name     string
	started  time.Time
	isRun    bool // the name is a run id with a real date and time
	scenario string
	passed   bool
}

// pruneDecision is whether one run's overlay may go, and why.
type pruneDecision struct {
	id     string
	action string
	reason string
}

func factsOf(name string, passed bool) runFacts {
	f := runFacts{name: name, passed: passed}
	m := runIDRE.FindStringSubmatch(name)
	if m == nil {
		return f
	}
	started, err := time.Parse(runIDLayout, m[1])
	if err != nil { // the shape of a stamp with no such date or time: not a run id, so kept
		return f
	}
	f.started, f.isRun, f.scenario = started, true, m[2]
	return f
}

// planPrune decides each run in name order: its overlay may go when it passed, a later run of
// its scenario passed, or it is older than days (E61).
func planPrune(runs []runFacts, now time.Time, days int) []pruneDecision {
	sorted := append([]runFacts(nil), runs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
	out := make([]pruneDecision, 0, len(sorted))
	for _, r := range sorted {
		d := pruneDecision{id: r.name, action: actionKeep}
		switch later := latestPassAfter(sorted, r); {
		case !r.isRun:
			d.reason = notARunID
		case r.passed:
			d.action, d.reason = actionPrune, "passed"
		case later != "":
			d.action, d.reason = actionPrune, "a later run of "+r.scenario+" passed: "+later
		case now.Sub(r.started) > time.Duration(days)*24*time.Hour:
			d.action, d.reason = actionPrune, fmt.Sprintf("did not pass, and is older than %d days", days)
		default:
			d.reason = fmt.Sprintf("did not pass; kept for diagnosis until a later run of %s passes or it is older than %d days", r.scenario, days)
		}
		out = append(out, d)
	}
	return out
}

// latestPassAfter is the newest passing run of r's scenario started after r, or "".
func latestPassAfter(sorted []runFacts, r runFacts) string {
	for _, o := range slices.Backward(sorted) {
		if o.isRun && o.passed && o.scenario == r.scenario && o.started.After(r.started) {
			return o.name
		}
	}
	return ""
}

// runPassed is whether a run was judged a pass. A run with no response, or one that is not a
// finished pass (a run killed while writing it), did not pass and is kept for diagnosis.
func runPassed(dir string) (bool, error) {
	path := filepath.Join(dir, responseFile)
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, failf("cannot read %s: %v", path, err)
	}
	if !json.Valid(body) {
		return false, nil
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return false, failf("cannot read %s: %v", path, err)
	}
	fields, ok := doc.(map[string]any)
	return ok && fields["state"] == "finished" && fields["verdict"] == "pass", nil
}

// runDir is one directory under runs/, with its time and whether it passed.
type runDir struct {
	runFacts

	mtime time.Time
}

// runDirs is every directory under runs/ (not a symlink, not a file); none is an empty lab.
func (l *lab) runDirs() ([]runDir, error) {
	entries, err := os.ReadDir(l.at(runsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, failf("cannot list %s: %v", l.at(runsDir), err)
	}
	var out []runDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, failf("cannot read %s: %v", l.at(runsDir, e.Name()), err)
		}
		passed, err := runPassed(l.at(runsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, runDir{runFacts: factsOf(e.Name(), passed), mtime: info.ModTime()})
	}
	return out, nil
}

// hostMiB is the host's memory, from /proc/meminfo.
func hostMiB() (int, error) {
	body, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	return memTotalMiB(string(body))
}

func memTotalMiB(meminfo string) (int, error) {
	for line := range strings.SplitSeq(meminfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "MemTotal:" {
			continue
		}
		if len(fields) < 2 {
			break
		}
		kib, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, fmt.Errorf("/proc/meminfo has a MemTotal that is not a number: %s", line)
		}
		return kib / 1024, nil
	}
	return 0, errors.New("/proc/meminfo has no MemTotal line")
}

// ramFloor refuses a guest of more than ramCeilingPercent of the host's memory (§10).
func (c *cli) ramFloor(guestMiB int) error {
	host, err := c.env.HostMiB()
	if err != nil {
		return fmt.Errorf("cannot read the host's memory: %w", err)
	}
	if ceiling := host * ramCeilingPercent / 100; guestMiB > ceiling {
		return fmt.Errorf("guest %d MiB exceeds the %d MiB ceiling of a %d MiB host", guestMiB, ceiling, host)
	}
	return nil
}

// rebuildFloor refuses a rebuild with no room beside the old base for a new one of its size
// and the margin, or no RAM for its guest (E16). The disk guard (E64) has already held the
// build to its worst case; this holds it to the base it replaces.
func (c *cli) rebuildFloor(bases string, size int64, guestMiB int) error {
	free, err := c.env.FreeBytes(bases)
	if err != nil {
		return fmt.Errorf("cannot read the free space under %s: %w", bases, err)
	}
	if need := 2*size + diskMarginBytes; free < need {
		return fmt.Errorf("only %s free under %s; a rebuild needs %s (the old and the new base, %s each, and a %s margin)",
			humanBytes(free), bases, humanBytes(need), humanBytes(size), humanWhole(diskMarginBytes))
	}
	return c.ramFloor(guestMiB)
}

// sweep applies the retention policy: runs, then failed builds, then the spool; the evictions
// it prints are its payload (E51).
func (c *cli) sweep(ctx context.Context, l *lab) error {
	checkout, err := l.needCheckout()
	if err != nil {
		return err
	}
	if err := c.sweepRuns(l); err != nil {
		return err
	}
	if err := c.sweepBuilds(l); err != nil {
		return err
	}
	if _, err := c.helper(ctx, helperCall{module: retentionModule, stdout: c.env.Stdout, args: []string{
		"sweep-spool", "--checkout", checkout, flagLog, l.at(retentionLog),
	}}); err != nil {
		return ctxErr(ctx, failf("the spool sweep failed: %v", err))
	}
	c.logf("every eviction is recorded in %s", l.at(retentionLog))
	return nil
}

// sweepRuns removes the passing runs beyond the newest defaultKeepRuns.
func (c *cli) sweepRuns(l *lab) error {
	runs, err := l.runDirs()
	if err != nil {
		return err
	}
	var passed []runDir
	for _, r := range runs {
		if r.passed {
			passed = append(passed, r)
		}
	}
	sort.SliceStable(passed, func(i, j int) bool { return passed[i].mtime.After(passed[j].mtime) })
	for _, r := range passed[min(defaultKeepRuns, len(passed)):] {
		if err := c.evict(l, "run", r.name, "passing run beyond the retention cap", l.at(runsDir, r.name)); err != nil {
			return err
		}
	}
	return nil
}

// sweepBuilds removes a failed fast-base build directory (minutes to redo) and keeps a desktop
// one (hours, and the diagnosis).
func (c *cli) sweepBuilds(l *lab) error {
	entries, err := os.ReadDir(l.at(basesDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return failf("cannot list %s: %v", l.at(basesDir), err)
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasSuffix(name, buildSuffix) {
			continue
		}
		if strings.HasPrefix(name, desktopPrefix) {
			if err := c.record(l, "kept build "+name+" failed desktop build kept for diagnosis"); err != nil {
				return err
			}
			continue
		}
		if err := c.evict(l, "build", name, "failed fast-base build directory", l.at(basesDir, name)); err != nil {
			return err
		}
	}
	return nil
}

// evict logs an eviction, then removes the directory and says so.
func (c *cli) evict(l *lab, kind, name, why, dir string) error {
	if err := c.record(l, "evicted "+kind+" "+name+" "+why); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return failf("cannot remove %s: %v", dir, err)
	}
	c.sayf("VMTEST-EVICTED %s %s", kind, name)
	return nil
}

// record appends `<epoch> <event>` to the retention log, before the change it records.
func (c *cli) record(l *lab, event string) error {
	path := l.at(retentionLog)
	log, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return failf("cannot open %s: %v", path, err)
	}
	_, werr := fmt.Fprintf(log, "%d %s\n", c.env.Now().Unix(), event)
	if err := errors.Join(werr, log.Close()); err != nil {
		return failf("cannot record in %s: %v", path, err)
	}
	return nil
}

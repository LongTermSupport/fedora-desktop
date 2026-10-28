package vmtest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// A run's overlay is its one large file: up to the guest disk's size, kept when the run did not
// pass so its disk can be read. prune-runs removes the overlays the prune rule (planPrune) says
// may go (E61-E63); everything else in the run directory (the logs, the response, the check
// results) stays, and the bases are never touched. The disk guard refuses a run or a build the
// lab's filesystem cannot carry at its worst, and a member-up, before anything is done (E64).

const (
	gib = int64(1) << 30
	// diskMarginBytes is the headroom kept below a step that fits.
	diskMarginBytes = 2 * gib
	// installMediaBytes is room for the media a full base is installed from (the netinst ISO,
	// and a desktop's Live ISO beside it), which the build downloads into images/.
	installMediaBytes = 4 * gib
	// runDiskNeed is a run at its worst: an overlay grown to the whole guest disk, and the margin.
	runDiskNeed = buildDiskGiB*gib + diskMarginBytes
	// buildDiskNeed is a build at its worst: its disk grown to full size, the margin, the media.
	buildDiskNeed = runDiskNeed + installMediaBytes
	// defaultPruneDays is how long a run that did not pass keeps its overlay with no later pass.
	defaultPruneDays = 7
	// maxPruneDays is the largest --older-than taken: a century, far inside time.Duration.
	maxPruneDays = 36500
	runLockFile  = "run.lock"
	actionPrune  = "prune"
	actionKeep   = "keep"
	actionLive   = "live"
	lockHeld     = runLockFile + " is held, the run is in progress"
)

var (
	runNeed = diskNeed{runDiskNeed, fmt.Sprintf("a run needs %s (a guest disk of up to %d GiB and a %s margin)",
		humanBytes(runDiskNeed), buildDiskGiB, humanWhole(diskMarginBytes))}
	// memberNeed is a run's need: a member boots the same overlay of the same guest disk.
	memberNeed = diskNeed{runDiskNeed, fmt.Sprintf("a member needs %s (a guest disk of up to %d GiB and a %s margin)",
		humanBytes(runDiskNeed), buildDiskGiB, humanWhole(diskMarginBytes))}
	buildNeed = diskNeed{buildDiskNeed, fmt.Sprintf("a base build needs %s (a guest disk of up to %d GiB, a %s margin and %s of install media)",
		humanBytes(buildDiskNeed), buildDiskGiB, humanWhole(diskMarginBytes), humanWhole(installMediaBytes))}
)

// diskNeed is what a step needs free, and the sentence that says so.
type diskNeed struct {
	bytes int64
	what  string
}

// pruneEntry is one run holding an overlay, and what prune-runs does with it.
type pruneEntry struct {
	pruneDecision

	files []string // base names of the overlays, sorted
	bytes int64
}

// freed is what the prunable entries of a plan add up to.
type freed struct {
	bytes int64
	runs  int
}

func freeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	return freeOf(path, &st)
}

// freeOf is the space statfs says an unprivileged user can still write under path: f_bavail
// counts fragments (f_frsize), which POSIX distinguishes from the preferred I/O size.
func freeOf(path string, st *syscall.Statfs_t) (int64, error) {
	if st.Frsize <= 0 {
		return 0, fmt.Errorf("statfs %s: fragment size %d", path, st.Frsize)
	}
	if st.Bavail > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	blocks := int64(st.Bavail)
	if blocks > math.MaxInt64/st.Frsize {
		return math.MaxInt64, nil
	}
	return blocks * st.Frsize, nil
}

// allocated is the space a file takes on disk: an overlay is sparse once the guest discards.
func allocated(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512, nil
	}
	return info.Size(), nil
}

// humanBytes is n in binary units, rounded down, so a shortfall never reads as enough.
func humanBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	v, i := float64(n)/1024, 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", math.Floor(v*10)/10, units[i])
}

// humanWhole is a whole number of GiB, as the constants are written.
func humanWhole(n int64) string { return strconv.FormatInt(n/gib, 10) + " GiB" }

// holdRun takes runs/<id>/run.lock for the life of a run, from its directory to the end of its
// teardown. It waits: only a prune holds the lock otherwise, and only while it removes.
func holdRun(dir string) (func() error, error) {
	f, err := os.OpenFile(filepath.Join(dir, runLockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, failf("cannot create the run lock: %v", err)
	}
	if err := flock(f, syscall.LOCK_EX); err != nil {
		return nil, errors.Join(failf("cannot lock %s: %v", f.Name(), err), f.Close())
	}
	return f.Close, nil
}

// tryRun takes a run's lock without waiting; locked is false when a run holds it. A directory
// with no lock file is a run from before the lock (a run makes it before anything else), so
// nothing holds it.
func tryRun(dir string) (func() error, bool, error) {
	none := func() error { return nil }
	f, err := os.OpenFile(filepath.Join(dir, runLockFile), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return none, true, nil
	}
	if err != nil {
		return none, false, failf("cannot open the run lock in %s: %v", dir, err)
	}
	err = flock(f, syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return none, false, f.Close()
	}
	if err != nil {
		return none, false, errors.Join(failf("cannot lock %s: %v", f.Name(), err), f.Close())
	}
	return f.Close, true, nil
}

func flock(f *os.File, how int) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return fmt.Errorf("lock %s: %w", f.Name(), err)
	}
	var lockErr error
	if err := conn.Control(func(fd uintptr) { lockErr = syscall.Flock(int(fd), how) }); err != nil {
		return fmt.Errorf("lock %s: %w", f.Name(), err)
	}
	return lockErr
}

// overlays is the overlay files in a run directory and the space they take.
func overlays(dir string) ([]string, int64, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.qcow2"))
	if err != nil {
		return nil, 0, failf("cannot list %s: %v", dir, err)
	}
	names := make([]string, 0, len(paths))
	var total int64
	for _, p := range paths {
		n, err := allocated(p)
		if err != nil {
			return nil, 0, failf("%v", err)
		}
		names = append(names, filepath.Base(p))
		total += n
	}
	return names, total, nil
}

// prunePlan is every run holding an overlay, decided by planPrune and checked against the
// runs still live: libvirt lists their domain, or they hold their lock.
func (c *cli) prunePlan(ctx context.Context, l *lab, days int) ([]pruneEntry, error) {
	domains, err := c.listDomains(ctx)
	if err != nil {
		return nil, err
	}
	facts, err := l.runDirs()
	if err != nil {
		return nil, err
	}
	var plan []pruneEntry
	for _, d := range planPrune(facts, c.env.Now(), days) {
		e := pruneEntry{pruneDecision: d}
		dir := l.at(runsDir, e.id)
		if e.files, e.bytes, err = overlays(dir); err != nil {
			return nil, err
		}
		if len(e.files) == 0 {
			continue
		}
		if err := markLive(&e, domains, dir); err != nil {
			return nil, err
		}
		plan = append(plan, e)
	}
	return plan, nil
}

// markLive makes a run whose guest or lock is still held live, whatever the plan says.
func markLive(e *pruneEntry, domains map[string]bool, dir string) error {
	if domain := runDomain(e.id); domains[domain] {
		e.action, e.reason = actionLive, "libvirt has the domain "+domain
		return nil
	}
	release, locked, err := tryRun(dir)
	if err != nil {
		return err
	}
	if !locked {
		e.action, e.reason = actionLive, lockHeld
	}
	return release()
}

func prunable(plan []pruneEntry) freed {
	var f freed
	for _, e := range plan {
		if e.action == actionPrune {
			f.bytes += e.bytes
			f.runs++
		}
	}
	return f
}

func parsePruneArgs(args []string) (int, bool, error) {
	days, yes, seenDays := defaultPruneDays, false, false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--yes" && !yes:
			yes = true
		case args[i] == "--older-than" && !seenDays:
			if i+1 >= len(args) {
				return 0, false, usagef("--older-than needs a number of days")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 0 {
				return 0, false, usagef("--older-than takes a whole number of days, not %s", args[i+1])
			}
			// The age is compared as a time.Duration, which a count much past this overflows to a
			// negative age that every failed run would exceed.
			if n > maxPruneDays {
				return 0, false, usagef("--older-than takes at most %d days, not %d", maxPruneDays, n)
			}
			days, seenDays = n, true
			i++
		default:
			return 0, false, usagef("usage: vmtest prune-runs [--older-than DAYS] [--yes]")
		}
	}
	return days, yes, nil
}

func (c *cli) cmdPruneRuns(ctx context.Context, args []string) error {
	days, yes, err := parsePruneArgs(args)
	if err != nil {
		return err
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	plan, err := c.prunePlan(ctx, l, days)
	if err != nil {
		return err
	}
	for i := range plan {
		if err := c.pruneOne(l, &plan[i], yes); err != nil {
			return err
		}
	}
	if err := c.listBases(l); err != nil {
		return err
	}
	f := prunable(plan)
	runs, them := "runs", "them"
	if f.runs == 1 {
		runs, them = "run", "it"
	}
	if yes {
		c.sayf("VMTEST-PRUNE-DONE freed %s from %d %s", humanBytes(f.bytes), f.runs, runs)
		return nil
	}
	c.sayf("VMTEST-PRUNE-DONE would free %s from %d %s; dry run, vmtest prune-runs --yes removes %s", humanBytes(f.bytes), f.runs, runs, them)
	return nil
}

// pruneOne reports one run and, with yes, removes its overlays under its lock, logged first.
func (c *cli) pruneOne(l *lab, e *pruneEntry, yes bool) error {
	verb := map[string]string{actionPrune: "would-remove", actionKeep: actionKeep, actionLive: actionLive}[e.action]
	if yes && e.action == actionPrune {
		var err error
		if verb, err = c.removeLocked(l, e); err != nil {
			return err
		}
	}
	c.sayf("VMTEST-PRUNE-RUN %s %s %s %s: %s", e.id, verb, strings.Join(e.files, ","), humanBytes(e.bytes), e.reason)
	return nil
}

// removeLocked removes a prunable run's overlays under its lock, or marks it live when a run
// took the lock since the plan was made; it returns the word the run's line says.
func (c *cli) removeLocked(l *lab, e *pruneEntry) (string, error) {
	dir := l.at(runsDir, e.id)
	release, locked, err := tryRun(dir)
	if err != nil {
		return "", err
	}
	if !locked {
		e.action, e.reason = actionLive, lockHeld
		return actionLive, nil
	}
	if err := errors.Join(c.removeOverlays(l, dir, e), release()); err != nil {
		return "", err
	}
	return "removed", nil
}

func (c *cli) removeOverlays(l *lab, dir string, e *pruneEntry) error {
	if err := c.record(l, "pruned overlay "+e.id+" "+strings.Join(e.files, ",")+": "+e.reason); err != nil {
		return err
	}
	var removed []string
	return c.removeDisks(l, dir, e.files, &removed)
}

// removeDisks removes the named files of dir one by one. The first that fails stops it, logged
// as failed (the log already recorded the removal) and named with what went before it.
func (c *cli) removeDisks(l *lab, dir string, files []string, removed *[]string) error {
	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.Remove(path); err != nil {
			so := "nothing"
			if len(*removed) > 0 {
				so = strings.Join(*removed, ", ")
			}
			return errors.Join(failf("cannot remove %s: %v; removed so far: %s", path, err, so),
				c.record(l, "failed to remove "+path+": "+err.Error()))
		}
		*removed = append(*removed, path)
	}
	return nil
}

// listBases names every base and its size: they are what prune-runs never touches.
func (c *cli) listBases(l *lab) error {
	disks, err := filepath.Glob(l.at(basesDir, "*", "base.qcow2"))
	if err != nil {
		return failf("cannot list %s: %v", l.at(basesDir), err)
	}
	for _, disk := range disks {
		n, err := allocated(disk)
		if err != nil {
			return failf("%v", err)
		}
		c.sayf("VMTEST-PRUNE-BASE %s %s: bases are never touched", filepath.Base(filepath.Dir(disk)), humanBytes(n))
	}
	return nil
}

// diskGuard refuses a step the lab's filesystem cannot carry at its worst, before anything is
// done, and says what prune-runs would free (E64).
func (c *cli) diskGuard(ctx context.Context, l *lab, need diskNeed) error {
	free, err := c.env.FreeBytes(l.home)
	if err != nil {
		return failf("cannot read the free space under %s: %v", l.home, err)
	}
	if free >= need.bytes {
		return nil
	}
	short := fmt.Sprintf("only %s free under %s; %s", humanBytes(free), l.home, need.what)
	plan, err := c.prunePlan(ctx, l, defaultPruneDays)
	if err != nil {
		return ctxErr(ctx, failf("%s. vmtest prune-runs lists the run overlays that can go, but working out what it would free failed: %v", short, err))
	}
	return failf("%s. vmtest prune-runs would free %s of run overlays; vmtest prune-runs lists them and vmtest prune-runs --yes removes them",
		short, humanBytes(prunable(plan).bytes))
}

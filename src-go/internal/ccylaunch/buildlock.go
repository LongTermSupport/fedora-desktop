package ccylaunch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// buildlock.go is F14, `ccy --build-lock`: the image builds in progress, read from the locks
// lib/image-build-lock.bash takes (one per image, $XDG_RUNTIME_DIR/ccy/image-builds/*.lock,
// each holding while a build runs the record "<pid> <epoch the build began> <image>"), and a
// hung one stopped after a yes.

const (
	buildLockFlag = "--build-lock"
	// userHZ is the unit of a process's start time in /proc/<pid>/stat (USER_HZ, 100 on Linux).
	userHZ = 100
	// termWait and killWait are how long a stop waits for the lock to be let go after SIGTERM,
	// and then after SIGKILL; pollEvery is how often it looks.
	termWait  = 10 * time.Second
	killWait  = 5 * time.Second
	pollEvery = 200 * time.Millisecond
)

// buildRecord is a lock file's record.
type buildRecord struct {
	pid   int
	began int64
	image string
}

var recordLine = regexp.MustCompile(`\A([1-9][0-9]*) ([0-9]+) (\S+)\n\z`)

// parseBuildRecord reads a record, written whole by the holder: one line, three fields.
func parseBuildRecord(text string) (buildRecord, bool) {
	m := recordLine.FindStringSubmatch(text)
	if m == nil {
		return buildRecord{}, false
	}
	pid, pidErr := strconv.Atoi(m[1])
	began, beganErr := strconv.ParseInt(m[2], 10, 64)
	if pidErr != nil || beganErr != nil {
		return buildRecord{}, false
	}
	return buildRecord{pid: pid, began: began, image: m[3]}, true
}

// buildLock is the flag: report every lock, then offer to stop each build whose holder it can
// vouch for (a running process that started before its build began).
func (f Front) buildLock(ctx context.Context) term.ExitCode {
	if len(f.Args) != 1 {
		return f.failWith(term.ExitUsage, "ccy: --build-lock takes no other argument.")
	}
	dir := filepath.Join(playlock.RuntimeDir(f.getenv, os.Getuid()), "ccy", "image-builds")
	names, err := lockFiles(dir)
	if err != nil {
		return f.fail("ccy: " + err.Error())
	}
	var p term.Printer
	var live []heldBuild
	anyHeld := false
	for _, name := range names {
		build, held, err := f.reportLock(&p, filepath.Join(dir, name))
		if err != nil {
			return f.fail("ccy: " + err.Error())
		}
		anyHeld = anyHeld || held
		if build != nil {
			live = append(live, *build)
		}
	}
	if !anyHeld {
		p.Print(f.Stdout, "No image build is running.\n")
	}
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the answer: " + err.Error())
	}
	in := f.Stdin
	if in == nil {
		in = strings.NewReader("")
	}
	// One reader for every question: piped answers are read a line at a time, in order.
	lines := &ui.Lines{In: in, Out: f.Stderr}
	for _, build := range live {
		if code, done := f.offerStop(ctx, lines, build.rec, build.path); done {
			return code
		}
	}
	return term.ExitOK
}

// heldBuild is a build whose holder the front can vouch for, and its lock file.
type heldBuild struct {
	rec  buildRecord
	path string
}

// reportLock says what one lock holds: whether it is held, and the build when its holder can
// be vouched for.
func (f Front) reportLock(p *term.Printer, path string) (*heldBuild, bool, error) {
	held, text, err := probeLock(path)
	if err != nil {
		return nil, false, err
	}
	rec, ok := parseBuildRecord(text)
	switch {
	case !held && ok:
		p.Print(f.Stdout, fmt.Sprintf("%s: the record of process %d is stale (nothing holds the lock, so that build has ended); ignored.\n", rec.image, rec.pid))
		return nil, false, nil
	case !held:
		return nil, false, nil
	case !ok:
		p.Print(f.Stdout, imageOfLock(filepath.Base(path))+": a build holds the lock but has not written its record yet; nothing to stop. Ask again in a moment.\n")
		return nil, true, nil
	}
	why, err := f.unvouched(rec)
	if err != nil {
		return nil, true, err
	}
	if why != "" {
		p.Print(f.Stdout, fmt.Sprintf("%s: the lock is held, but its record names process %d, %s: that record is stale and is ignored; nothing to stop.\n", rec.image, rec.pid, why))
		return nil, true, nil
	}
	p.Print(f.Stdout, fmt.Sprintf("%s is being built by process %d, started %s ago.\n", rec.image, rec.pid, playlock.Duration(f.clock().Now().Sub(time.Unix(rec.began, 0)))))
	return &heldBuild{rec: rec, path: path}, true, nil
}

// offerStop asks about one build and stops it on yes; done is true when the run ends here.
func (f Front) offerStop(ctx context.Context, lines *ui.Lines, rec buildRecord, path string) (term.ExitCode, bool) {
	h := ui.Here("ccy", "Image builds").About(rec.image + " · process " + strconv.Itoa(rec.pid))
	yes, err := lines.YesNo(ctx, h, fmt.Sprintf("Stop the build of %s by process %d, and every process it started?", rec.image, rec.pid), false)
	var p term.Printer
	switch {
	case errors.Is(err, ui.ErrCancelled):
		p.Print(f.Stdout, "Nothing was stopped.\n")
		return term.ExitCancelled, true
	case err != nil:
		return f.fail("ccy: " + err.Error()), true
	case !yes:
		p.Print(f.Stdout, "Nothing was stopped.\n")
	default:
		stopped, err := f.stopBuild(rec, path)
		if err != nil {
			return f.fail("ccy: " + err.Error()), true
		}
		if stopped {
			p.Print(f.Stdout, fmt.Sprintf("The build of %s by process %d was stopped.\n", rec.image, rec.pid))
		} else {
			p.Print(f.Stdout, fmt.Sprintf("The build of %s by process %d ended while you answered; nothing was stopped.\n", rec.image, rec.pid))
		}
	}
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the answer: " + err.Error()), true
	}
	return term.ExitOK, false
}

// treeProc is a process of the build's tree as first seen: a pid is signalled only while it is
// still that process (the same start tick), never a later process given the number.
type treeProc struct {
	pid   int
	start int64
}

// stopBuild stops the build the person said yes to, if it is still that build: the lock is
// still held with the same record by the same process, checked again after the answer, since
// the build may have ended (and its launcher gone on to run a session) while the question was
// open. SIGTERM to the holder and every process under it, holder first; when the lock is not
// let go, or any of them is left, within termWait, SIGKILL to those still the same processes
// and to any the holder has started since. stopped is false when the build had already ended.
func (f Front) stopBuild(rec buildRecord, path string) (bool, error) {
	if still, err := f.stillHolds(rec, path); err != nil || !still {
		return false, err
	}
	tree, err := f.snapshotTree(rec.pid, nil)
	if err != nil {
		return false, err
	}
	for _, round := range []struct {
		signal syscall.Signal
		wait   time.Duration
	}{{syscall.SIGTERM, termWait}, {syscall.SIGKILL, killWait}} {
		if round.signal == syscall.SIGKILL {
			if tree, err = f.snapshotTree(rec.pid, tree); err != nil {
				return false, err
			}
		}
		if err := f.signalTree(tree, round.signal); err != nil {
			return false, err
		}
		done, err := f.waitDone(rec, path, tree, round.wait)
		if err != nil || done {
			return done, err
		}
	}
	return false, fmt.Errorf("could not stop the build of %s: after SIGTERM and SIGKILL to process %d and every process it started, the lock %s is still held or one of them still runs", rec.image, rec.pid, path)
}

// stillHolds is whether the lock is still held with rec as its record, by a process vouched for.
func (f Front) stillHolds(rec buildRecord, path string) (bool, error) {
	held, text, err := probeLock(path)
	if err != nil || !held {
		return false, err
	}
	if now, ok := parseBuildRecord(text); !ok || now != rec {
		return false, nil
	}
	why, err := f.unvouched(rec)
	return why == "", err
}

// snapshotTree is the holder and every process under it, each with its start tick, after the
// processes of earlier that are still the same; the holder's new children only while the
// holder is itself still the same process.
func (f Front) snapshotTree(holder int, earlier []treeProc) ([]treeProc, error) {
	var tree []treeProc
	for _, p := range earlier {
		same, err := f.sameProcess(p)
		if err != nil {
			return nil, err
		}
		if same {
			tree = append(tree, p)
		}
	}
	if len(earlier) > 0 && (len(tree) == 0 || tree[0].pid != holder) {
		return tree, nil
	}
	pids, err := f.processTree(holder)
	if err != nil {
		return nil, err
	}
	for _, pid := range pids {
		if slices.ContainsFunc(tree, func(p treeProc) bool { return p.pid == pid }) {
			continue
		}
		st, alive, err := f.procStat(pid)
		if err != nil {
			return nil, err
		}
		if alive {
			tree = append(tree, treeProc{pid: pid, start: st.start})
		}
	}
	return tree, nil
}

// signalTree sends sig to every process of the tree that is still the same process.
func (f Front) signalTree(tree []treeProc, sig syscall.Signal) error {
	for _, p := range tree {
		same, err := f.sameProcess(p)
		if err != nil {
			return err
		}
		if !same {
			continue
		}
		if err := f.signal(p.pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("could not send %s to process %d: %w", sig, p.pid, err)
		}
	}
	return nil
}

// waitDone is whether, within wait, the build's record left the lock and every process of the
// tree ended.
func (f Front) waitDone(rec buildRecord, path string, tree []treeProc, wait time.Duration) (bool, error) {
	for tries := int(wait / pollEvery); ; tries-- {
		done, err := f.buildDone(rec, path, tree)
		if err != nil || done {
			return done, err
		}
		if tries == 0 {
			return false, nil
		}
		f.pause(pollEvery)
	}
}

func (f Front) buildDone(rec buildRecord, path string, tree []treeProc) (bool, error) {
	held, text, err := probeLock(path)
	if err != nil {
		return false, err
	}
	if now, ok := parseBuildRecord(text); held && ok && now == rec {
		return false, nil
	}
	for _, p := range tree {
		same, err := f.sameProcess(p)
		if err != nil || same {
			return false, err
		}
	}
	return true, nil
}

// sameProcess is whether p's pid is still the process first seen.
func (f Front) sameProcess(p treeProc) (bool, error) {
	st, alive, err := f.procStat(p.pid)
	return alive && st.start == p.start, err
}

// unvouched is why the record's process is not its build's holder: gone, or a later process
// given the same pid. Empty when it is.
func (f Front) unvouched(rec buildRecord) (string, error) {
	st, alive, err := f.procStat(rec.pid)
	if err != nil || !alive {
		return "which has gone", err
	}
	boot, err := f.bootTime()
	if err != nil {
		return "", err
	}
	if boot+st.start/userHZ > rec.began {
		return "but that process started after the build began, so it is another process given the same number", nil
	}
	return "", nil
}

// procStat is a process's parent and start tick, from <proc>/<pid>/stat (fields 4 and 22,
// counted after the command name's closing bracket); alive is false when it has gone, and for a
// process that has exited and only waits to be reaped (state Z or X, field 3).
type procInfo struct {
	ppid  int
	start int64
}

func (f Front) procStat(pid int) (procInfo, bool, error) {
	path := filepath.Join(f.procRoot(), strconv.Itoa(pid), "stat")
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return procInfo{}, false, nil
	}
	if err != nil {
		return procInfo{}, false, fmt.Errorf("could not read process %d: %w", pid, err)
	}
	line := string(raw)
	cut := strings.LastIndex(line, ") ")
	const ppidField, startField = 1, 19
	var fields []string
	if cut >= 0 {
		fields = strings.Fields(line[cut+2:])
	}
	if len(fields) <= startField {
		return procInfo{}, false, fmt.Errorf("process %d: unreadable %s", pid, path)
	}
	ppid, ppidErr := strconv.Atoi(fields[ppidField])
	start, startErr := strconv.ParseInt(fields[startField], 10, 64)
	if ppidErr != nil || startErr != nil {
		return procInfo{}, false, fmt.Errorf("process %d: unreadable %s", pid, path)
	}
	exited := fields[0] == "Z" || fields[0] == "X"
	return procInfo{ppid: ppid, start: start}, !exited, nil
}

// bootTime is the machine's boot time in epoch seconds (btime in <proc>/stat).
func (f Front) bootTime() (int64, error) {
	path := filepath.Join(f.procRoot(), "stat")
	fh, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, fmt.Errorf("could not read the boot time: %w", err)
	}
	scan := bufio.NewScanner(fh)
	boot := int64(-1)
	for scan.Scan() {
		if v, ok := strings.CutPrefix(scan.Text(), "btime "); ok {
			if n, parseErr := strconv.ParseInt(strings.TrimSpace(v), 10, 64); parseErr == nil {
				boot = n
			}
		}
	}
	if err := errors.Join(scan.Err(), fh.Close()); err != nil {
		return 0, fmt.Errorf("could not read the boot time from %s: %w", path, err)
	}
	if boot < 0 {
		return 0, fmt.Errorf("%s has no btime line", path)
	}
	return boot, nil
}

// processTree is pid and every process under it, parents before children.
func (f Front) processTree(pid int) ([]int, error) {
	entries, err := os.ReadDir(f.procRoot())
	if err != nil {
		return nil, fmt.Errorf("could not list the processes: %w", err)
	}
	children := map[int][]int{}
	for _, e := range entries {
		child, convErr := strconv.Atoi(e.Name())
		if convErr != nil || !e.IsDir() {
			continue
		}
		st, alive, statErr := f.procStat(child)
		if statErr != nil {
			return nil, statErr
		}
		if alive {
			children[st.ppid] = append(children[st.ppid], child)
		}
	}
	tree := []int{pid}
	for i := 0; i < len(tree); i++ {
		tree = append(tree, children[tree[i]]...)
	}
	return tree, nil
}

// lockFiles is every *.lock in dir, in name order; none when dir does not exist.
func lockFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read the image build locks in %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".lock") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// probeLock is whether another open of the lock holds it, and its record. The probe takes a
// shared lock for a moment and lets it go: it never waits.
func probeLock(path string) (bool, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, "", fmt.Errorf("could not read the image build lock %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, "", fmt.Errorf("the image build lock %s is not a regular file; remove it", path)
	}
	fh, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false, "", fmt.Errorf("could not open the image build lock %s: %w", path, err)
	}
	flockErr := syscall.Flock(int(fh.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	held := errors.Is(flockErr, syscall.EWOULDBLOCK)
	if flockErr != nil && !held {
		return false, "", errors.Join(fmt.Errorf("could not probe the image build lock %s: %w", path, flockErr), fh.Close())
	}
	text, readErr := os.ReadFile(filepath.Clean(path))
	if readErr != nil {
		readErr = fmt.Errorf("could not read the image build lock %s: %w", path, readErr)
	}
	return held, string(text), errors.Join(readErr, fh.Close())
}

// imageOfLock is the image a lock file is named for: the launcher writes the image's ':' as
// '_', and every ccy image is claude-yolo:<tag>, so the first '_' was the ':'.
func imageOfLock(name string) string {
	return strings.Replace(strings.TrimSuffix(name, ".lock"), "_", ":", 1)
}

func (f Front) procRoot() string {
	if f.ProcRoot == "" {
		return "/proc"
	}
	return f.ProcRoot
}

func (f Front) signal(pid int, sig syscall.Signal) error {
	if f.Signal == nil {
		return syscall.Kill(pid, sig)
	}
	return f.Signal(pid, sig)
}

func (f Front) pause(d time.Duration) {
	if f.Pause == nil {
		time.Sleep(d)
		return
	}
	f.Pause(d)
}

func (f Front) failWith(code term.ExitCode, msg string) term.ExitCode {
	var p term.Printer
	p.Print(f.Stderr, msg+"\n")
	return code
}

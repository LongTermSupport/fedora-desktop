// Package watch is the Lightning diagnostics watchdog, agent-bus-lightning watch (Plan 00268
// Task 1.7). A Lightning instance launched with diagnostics on (internal/lightning L16) writes a
// memory line to its diagnostic log every 5 s from its GUI thread, so a log that falls silent is
// a GUI thread that stopped. Lightning's own stall tracer reports a stall only when it ends, and
// on 2026-11-25 the stall never ended: the instance grew by about 74 GB in at most 2,760 s
// until systemd-oomd killed it. This watchdog, outside the process, records what such a thread
// is doing while it is still doing it.
//
// It only reads Lightning: /proc, and the diagnostic log's modification time. It never opens the
// log or any other file of the instance, never signals or kills the process, and installs
// nothing. Its one write is its record beside the log. The backtrace tool attaches with ptrace,
// which pauses the process while it reads the stacks; the instance is already lost by then.
//
// Expectations (each has a test whose name starts with its number):
//
//	W1 An instance whose log has been silent for Silence while its RSS has risen by GrowthKiB or
//	   more since the log last moved is recorded once: a header naming the process (pid and start
//	   time), the silence and the growth; each thread's id, name, state, user and system CPU
//	   ticks and kernel wait channel, from /proc/<pid>/task; and a backtrace of every thread.
//	   A line on Err names the record.
//	W2 A silent log with flat memory, or climbing memory with a moving log, records nothing and
//	   runs nothing.
//	W3 The growth is counted from the RSS read when the log last moved (or when the watchdog
//	   first saw the process): growth while the log was still written does not count.
//	W4 A process is looked at only when its comm is Lightning's; its argv is read only then. It
//	   is watched only when it is lightning-matrix, owned by UID, with --log-file <path> (or
//	   --log-file=<path>) where path is exactly <Instances>/<team>/cache/<LogName>.
//	W5 The backtrace is gdb's; without gdb, eu-stack's; with neither, the record says so and
//	   nothing is installed. A tool that runs and fails is recorded with what it said.
//	W6 gdb prints no frame argument (one could be a message's text), reads no init file and asks
//	   no debuginfod server; each tool is bounded by backtraceBound.
//	W7 The record file is at most RecordCap bytes: a record that would pass it rolls the file
//	   once to RecordName.1, and a single record over it is cut and says so.
//	W8 The record is written only to a regular file, mode 0600, created if absent; a link or
//	   anything else there is an error, never followed.
//	W9 A process whose /proc files vanish between reads has ended and is skipped.
//	W10 A process already named in the record (its pid and start time) is not recorded again
//	   after the watchdog restarts.
//	W11 Lightning's log is never written, and its cache folder gains only the record.
package watch

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	// RecordName is the watchdog's record, beside the diagnostic log.
	RecordName = "lightning-watchdog.log"
	// RecordCap is the most the record file holds before it rolls once to RecordName.1.
	RecordCap = 1 << 20
	// Interval is how often the instances are looked at.
	Interval = 15 * time.Second
	// Silence is how long a log must be silent: twelve of the 5 s memory lines missed.
	Silence = 60 * time.Second
	// GrowthKiB is the rise in RSS, since the log last moved, that marks a runaway rather than a
	// pause: 256 MiB.
	GrowthKiB = 256 * 1024

	// command is Lightning's program; comm is the kernel's 15-byte cut of it.
	command = "lightning-matrix"
	comm    = "lightning-matri"
	// backtraceBound is the longest a backtrace tool may take; gdb loading symbols for a large
	// process is slow, and the instance is already lost when it runs.
	backtraceBound = 120 * time.Second
	recordMode     = 0o600
	cutNote        = "\n(cut at the cap)\n"
	headPrefix     = "=== lightning-watchdog "
)

// Watcher is the watchdog's state across ticks.
type Watcher struct {
	Proc fs.FS        // /proc
	Run  execx.Runner // the backtrace tools
	Err  io.Writer    // a line per record
	UID  int          // only this user's processes are watched
	// Instances is the folder every team's instance is in; a log anywhere else is not watched.
	Instances string
	// LogName is the diagnostic log's file name in an instance's cache folder.
	LogName string

	seen map[process]*instance
}

// process is one run of a program: a pid is reused, its start time with it is not.
type process struct{ pid, start string }

// instance is what the watchdog remembers of a watched process.
type instance struct {
	logTime  time.Time // the log's modification time when last read
	baseline int       // RSS in KiB when the log last moved
	recorded bool
}

// Tick looks at every process once, at now.
func (w *Watcher) Tick(ctx context.Context, now time.Time) error {
	entries, err := fs.ReadDir(w.Proc, ".")
	if err != nil {
		return fmt.Errorf("listing /proc: %w", err)
	}
	if w.seen == nil {
		w.seen = map[process]*instance{}
	}
	alive := map[process]bool{}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		p, err := w.look(ctx, e.Name(), now)
		switch {
		case gone(err):
			continue
		case err != nil:
			return err
		}
		if p != (process{}) {
			alive[p] = true
		}
	}
	for p := range w.seen {
		if !alive[p] {
			delete(w.seen, p)
		}
	}
	return nil
}

// look is one process at now: the process when it is a watched instance, zero otherwise.
func (w *Watcher) look(ctx context.Context, pid string, now time.Time) (process, error) {
	logPath, err := w.diagnosticLog(pid)
	if err != nil || logPath == "" {
		return process{}, err
	}
	owner, rss, err := w.status(pid)
	if err != nil || owner != w.UID {
		return process{}, err
	}
	fields, err := w.stat(pid + "/stat")
	if err != nil {
		return process{}, err
	}
	p := process{pid: pid, start: fields.start}
	info, err := os.Lstat(logPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return p, nil
	case err != nil:
		return p, fmt.Errorf("reading the time of %s: %w", logPath, err)
	case !info.Mode().IsRegular():
		// Lightning refuses to log through a link, so there is nothing to watch.
		return p, nil
	}
	in := w.seen[p]
	if in == nil {
		in = &instance{logTime: info.ModTime(), baseline: rss}
		w.seen[p] = in
	}
	if !info.ModTime().Equal(in.logTime) {
		in.logTime, in.baseline = info.ModTime(), rss
	}
	silent := now.Sub(info.ModTime())
	if in.recorded || silent < Silence || rss-in.baseline < GrowthKiB {
		return p, nil
	}
	in.recorded = true
	return p, w.record(ctx, p, filepath.Join(filepath.Dir(logPath), RecordName), finding{
		now: now, logTime: info.ModTime(), silent: silent, rss: rss, growth: rss - in.baseline,
	})
}

// diagnosticLog is the process's diagnostic log when it is a Lightning instance launched with
// one under Instances, "" otherwise. Its argv is read only when its comm is Lightning's (W4).
func (w *Watcher) diagnosticLog(pid string) (string, error) {
	name, err := fs.ReadFile(w.Proc, pid+"/comm")
	if err != nil || strings.TrimSpace(string(name)) != comm {
		return "", err
	}
	raw, err := fs.ReadFile(w.Proc, pid+"/cmdline")
	if err != nil {
		return "", err
	}
	args := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if filepath.Base(args[0]) != command {
		return "", nil
	}
	var logPath string
	for i, a := range args[1:] {
		switch {
		case a == "--log-file" && i+2 < len(args):
			logPath = args[i+2]
		case strings.HasPrefix(a, "--log-file="):
			logPath = strings.TrimPrefix(a, "--log-file=")
		}
	}
	if !w.isInstanceLog(logPath) {
		return "", nil
	}
	return logPath, nil
}

// isInstanceLog is whether p is exactly <Instances>/<team>/cache/<LogName>.
func (w *Watcher) isInstanceLog(p string) bool {
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p || filepath.Base(p) != w.LogName {
		return false
	}
	cache := filepath.Dir(p)
	team := filepath.Dir(cache)
	return filepath.Base(cache) == "cache" && filepath.Dir(team) == filepath.Clean(w.Instances)
}

// status is the process's real uid and RSS in KiB.
func (w *Watcher) status(pid string) (int, int, error) {
	data, err := fs.ReadFile(w.Proc, pid+"/status")
	if err != nil {
		return 0, 0, err
	}
	owner, rss := -1, -1
	scan := bufio.NewScanner(bytes.NewReader(data))
	for scan.Scan() {
		key, value, found := strings.Cut(scan.Text(), ":")
		words := strings.Fields(value)
		if !found || len(words) == 0 {
			continue
		}
		switch key {
		case "Uid":
			owner, err = strconv.Atoi(words[0])
		case "VmRSS":
			rss, err = strconv.Atoi(words[0])
		}
		if err != nil {
			return 0, 0, fmt.Errorf("reading %s/status: %w", pid, err)
		}
	}
	if owner < 0 || rss < 0 {
		// A process with no RSS line is a zombie: it has ended.
		return 0, 0, fs.ErrNotExist
	}
	return owner, rss, nil
}

// statFields are the fields of a /proc stat line the record uses.
type statFields struct{ id, name, state, utime, stime, start string }

// stat reads a /proc stat line. The name is in parentheses and may hold anything, so the fields
// are counted from the last ")".
func (w *Watcher) stat(file string) (statFields, error) {
	data, err := fs.ReadFile(w.Proc, file)
	if err != nil {
		return statFields{}, err
	}
	line := strings.TrimSpace(string(data))
	open, closing := strings.Index(line, "("), strings.LastIndex(line, ")")
	if open < 0 || closing < open {
		return statFields{}, fmt.Errorf("reading %s: not a stat line", file)
	}
	rest := strings.Fields(line[closing+1:])
	// rest[0] is field 3 (state); utime, stime and starttime are fields 14, 15 and 22.
	const state, utime, stime, start = 0, 11, 12, 19
	if len(rest) <= start {
		return statFields{}, fmt.Errorf("reading %s: too few fields", file)
	}
	return statFields{
		id: strings.TrimSpace(line[:open]), name: line[open+1 : closing],
		state: rest[state], utime: rest[utime], stime: rest[stime], start: rest[start],
	}, nil
}

// gone is whether err is a process (or thread) that ended between two reads.
func gone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// finding is why a process is recorded.
type finding struct {
	now, logTime time.Time
	silent       time.Duration
	rss, growth  int
}

// record writes p's record to file, unless file already names p (W10).
func (w *Watcher) record(ctx context.Context, p process, file string, f finding) error {
	key := "pid=" + p.pid + " start=" + p.start + " "
	named, err := recordNames(file, key)
	if err != nil || named {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s %s===\n", headPrefix, f.now.UTC().Format(time.RFC3339), key)
	fmt.Fprintf(&b, "log silent for %d s (last written %s); RSS %d KiB, up %d KiB since the log last moved\n",
		int(f.silent/time.Second), f.logTime.UTC().Format(time.RFC3339), f.rss, f.growth)
	b.WriteString("threads (tid name state utime stime wchan; CPU in clock ticks):\n")
	if err := w.threads(&b, p.pid); err != nil {
		return err
	}
	w.backtrace(ctx, &b, p.pid)
	b.WriteString("=== end ===\n")
	if err := appendCapped(file, b.String()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w.Err, "agent-bus-lightning watch: recorded pid %s in %s\n", p.pid, file); err != nil {
		return fmt.Errorf("naming the record: %w", err)
	}
	return nil
}

// threads writes a line for each of pid's threads.
func (w *Watcher) threads(b *strings.Builder, pid string) error {
	tasks, err := fs.ReadDir(w.Proc, pid+"/task")
	if err != nil {
		return fmt.Errorf("listing %s's threads: %w", pid, err)
	}
	for _, t := range tasks {
		dir := path.Join(pid, "task", t.Name())
		fields, err := w.stat(dir + "/stat")
		if gone(err) {
			continue
		}
		if err != nil {
			return err
		}
		wchan, err := fs.ReadFile(w.Proc, dir+"/wchan")
		if gone(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading %s/wchan: %w", dir, err)
		}
		fmt.Fprintf(b, "%s %s %s %s %s %s\n", fields.id, fields.name, fields.state, fields.utime, fields.stime,
			strings.TrimSpace(string(wchan)))
	}
	return nil
}

// backtrace writes every thread's backtrace from gdb, else eu-stack, else why there is none.
// A tool's failure is part of the record, not the watchdog's.
func (w *Watcher) backtrace(ctx context.Context, b *strings.Builder, pid string) {
	ctx, cancel := context.WithTimeout(ctx, backtraceBound)
	defer cancel()
	tools := []execx.Cmd{
		{Name: "gdb", Args: []string{
			"-batch", "-nx", "-p", pid,
			"-iex", "set debuginfod enabled off",
			"-ex", "set print frame-arguments none",
			"-ex", "set print entry-values no",
			"-ex", "thread apply all bt",
		}, Unset: []string{"DEBUGINFOD_URLS"}},
		{Name: "eu-stack", Args: []string{"-p", pid}, Unset: []string{"DEBUGINFOD_URLS"}},
	}
	for _, c := range tools {
		res, err := w.Run.Run(ctx, c)
		switch {
		case execx.IsNotInstalled(err):
			continue
		case err != nil:
			fmt.Fprintf(b, "backtrace (%s) failed: %v\n", c.Name, err)
		default:
			fmt.Fprintf(b, "backtrace (%s):\n", c.Name)
		}
		b.Write(res.Stdout)
		b.Write(res.Stderr)
		return
	}
	b.WriteString("backtrace: neither gdb nor eu-stack is installed; the watchdog installs nothing\n")
}

// recordNames is whether file, or its rolled copy, already holds a record headed with key.
func recordNames(file, key string) (bool, error) {
	for _, f := range []string{file, file + ".1"} {
		info, err := os.Lstat(f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("%s is not a regular file", f)
		}
		data, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			return false, err
		}
		if slices.ContainsFunc(strings.Split(string(data), "\n"), func(line string) bool {
			return strings.HasPrefix(line, headPrefix) && strings.Contains(line, " "+key)
		}) {
			return true, nil
		}
	}
	return false, nil
}

// appendCapped appends text to file, rolling it once to file.1 first when the two would pass
// RecordCap, and cutting text that alone would.
func appendCapped(file, text string) error {
	if len(text) > RecordCap {
		text = text[:RecordCap-len(cutNote)] + cutNote
	}
	info, err := os.Lstat(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", file)
	case info.Size()+int64(len(text)) > RecordCap:
		if err := os.Rename(file, file+".1"); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(filepath.Clean(file), os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, recordMode)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(f, text)
	return errors.Join(writeErr, f.Close())
}

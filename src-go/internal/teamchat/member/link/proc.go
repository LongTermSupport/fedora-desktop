package link

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Proc is the link's reading of /proc: a process's parent and start time, for LN7's chain,
// and its resident cost, for status (B40).
type Proc interface {
	PPid(pid int) (int, error)
	// StartTicks is field 22 of the process's stat, so a reused pid is never taken for it.
	StartTicks(pid int) (int64, error)
	Cost(pid int) (Cost, error)
}

// Cost is one process's resident cost; its uptime is the caller's, from StartTicks and the
// boot clock.
type Cost struct {
	RSSKiB, CPUMS, FDs, Wakeups int64
	StartTicks                  int64
}

// ticksPerSecond is the kernel's USER_HZ, which is 100 on every Linux architecture.
const ticksPerSecond = 100

// OSProc reads a /proc tree: "/proc" in production.
type OSProc struct{ Dir string }

// stat fields, numbered as proc(5) numbers them.
const (
	fieldPPid       = 4
	fieldUTime      = 14
	fieldSTime      = 15
	fieldStartTicks = 22
)

// field is stat field n as a number.
func field(fields []string, n int) (int64, error) {
	v, err := strconv.ParseInt(fields[n-3], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("link: stat field %d: %w", n, err)
	}
	return v, nil
}

// PPid is the process's parent.
func (p OSProc) PPid(pid int) (int, error) {
	fields, err := p.stat(pid)
	if err != nil {
		return 0, err
	}
	v, err := field(fields, fieldPPid)
	return int(v), err
}

// StartTicks is the process's start time in clock ticks since boot.
func (p OSProc) StartTicks(pid int) (int64, error) {
	fields, err := p.stat(pid)
	if err != nil {
		return 0, err
	}
	return field(fields, fieldStartTicks)
}

// Cost is the process's resident set, its CPU time, its open descriptors and its wakeups
// (every context switch, voluntary or not).
func (p OSProc) Cost(pid int) (Cost, error) {
	fields, err := p.stat(pid)
	if err != nil {
		return Cost{}, err
	}
	var c Cost
	var utime, stime int64
	for _, f := range []struct {
		n   int
		out *int64
	}{{fieldUTime, &utime}, {fieldSTime, &stime}, {fieldStartTicks, &c.StartTicks}} {
		if *f.out, err = field(fields, f.n); err != nil {
			return Cost{}, err
		}
	}
	c.CPUMS = (utime + stime) * 1000 / ticksPerSecond
	status, err := p.status(pid)
	if err != nil {
		return Cost{}, err
	}
	rss, ok := status["VmRSS"]
	vol, okVol := status["voluntary_ctxt_switches"]
	nonvol, okNon := status["nonvoluntary_ctxt_switches"]
	if !ok || !okVol || !okNon {
		return Cost{}, fmt.Errorf("link: the status of %d lacks its rss or switches", pid)
	}
	c.RSSKiB, c.Wakeups = rss, vol+nonvol
	fds, err := os.ReadDir(p.path(pid, "fd"))
	if err != nil {
		return Cost{}, err
	}
	c.FDs = int64(len(fds))
	return c, nil
}

// status is the numeric lines of the process's status file, a "kB" unit dropped.
func (p OSProc) status(pid int) (map[string]int64, error) {
	data, err := os.ReadFile(p.path(pid, "status"))
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			return nil, fmt.Errorf("link: a status line of %d has no key", pid)
		}
		switch key {
		case "VmRSS", "voluntary_ctxt_switches", "nonvoluntary_ctxt_switches":
			n, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "kB")), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("link: status %s of %d: %w", key, pid, err)
			}
			out[key] = n
		}
	}
	return out, sc.Err()
}

// stat is the numbered fields of the process's stat line, from field 3 (the state) on: the
// command name, field 2, may hold spaces and ")", so the line is cut at its last ")".
func (p OSProc) stat(pid int) ([]string, error) {
	data, err := os.ReadFile(p.path(pid, "stat"))
	if err != nil {
		return nil, err
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return nil, fmt.Errorf("link: the stat of %d has no command name", pid)
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < fieldStartTicks-2 {
		return nil, fmt.Errorf("link: the stat of %d is short", pid)
	}
	return fields, nil
}

func (p OSProc) path(pid int, name string) string {
	return p.Dir + "/" + strconv.Itoa(pid) + "/" + name
}

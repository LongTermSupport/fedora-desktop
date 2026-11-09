// Package procend says whether a process has ended as a whole, for a test that waits for a
// process it does not reap (a killed link whose keeper is frozen, a grandchild).
//
// The zombie state of /proc/<pid>/stat is the thread-group leader's alone: it turns Z as soon as
// the leader thread exits, while a killed multi-threaded program's other threads may still be
// exiting and still hold its files (a flock, a socket, a pipe end), which are let go only when
// the last of them drops the file table. /proc/<pid>/status counts those threads until each is
// released, after it dropped the table, so State Z (or X) with Threads 1 holds only once the
// whole group has ended (issue #182; project_zombie_exit).
package procend

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Ended is whether process pid has ended as a whole: no such process, or a zombie whose every
// thread but the leader has been released. A status that cannot be read for another reason, or
// that does not say both, is an error.
func Ended(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("procend: pid %d is no process", pid)
	}
	status, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if vanished(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("procend: process %d: %w", pid, err)
	}
	ended, err := endedIn(status)
	if err != nil {
		return false, fmt.Errorf("procend: process %d: %w", pid, err)
	}
	return ended, nil
}

// vanished is whether a read of /proc/<pid> failed because the process has been reaped: the
// directory is gone (ENOENT), or it went between the open and the read (ESRCH).
func vanished(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// endedIn is whether a /proc/<pid>/status text says the whole group has ended.
func endedIn(status []byte) (bool, error) {
	var state, threads string
	scan := bufio.NewScanner(bytes.NewReader(status))
	for scan.Scan() {
		if v, ok := strings.CutPrefix(scan.Text(), "State:"); ok {
			state = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(scan.Text(), "Threads:"); ok {
			threads = strings.TrimSpace(v)
		}
	}
	if err := scan.Err(); err != nil {
		return false, err
	}
	if state == "" {
		return false, errors.New("no State line in its status")
	}
	n, err := strconv.Atoi(threads)
	if err != nil {
		return false, fmt.Errorf("no thread count in its status: %q", threads)
	}
	zombie := state[0] == 'Z' || state[0] == 'X'
	return zombie && n == 1, nil
}

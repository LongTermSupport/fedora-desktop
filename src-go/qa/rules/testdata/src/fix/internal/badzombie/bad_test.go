package badzombie

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The originating helper (teamchat wiring, issue #182): the leader's Z taken as the process's
// end, while the link's other threads still held link.lock.
func gone(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	i := bytes.LastIndexByte(stat, ')')
	return i < 0 || bytes.HasPrefix(stat[i+1:], []byte(" Z")) // want `" Z" is the zombie state of the thread-group leader`
}

// The other spellings the sweep found: a Sprintf path and a trimmed prefix.
func goneSprintf(pid int) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	rest := string(data[bytes.LastIndexByte(data, ')')+1:])
	return strings.HasPrefix(strings.TrimSpace(rest), "Z") // want `"Z" is the zombie state of the thread-group leader`
}

// A joined path and the first field compared.
func alive(pid int) bool {
	raw, err := fs.ReadFile(os.DirFS("/proc"), filepath.Join(strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(raw[bytes.LastIndexByte(raw, ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z" // want `"Z" is the zombie state of the thread-group leader`
}

// A byte compared, inside a function literal.
func TestWaitsForTheEnd(t *testing.T) {
	ended := func(pid int) bool {
		stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return true
		}
		i := bytes.LastIndexByte(stat, ')')
		return stat[i+2] == 'Z' // want `'Z' is the zombie state of the thread-group leader`
	}
	if !ended(os.Getpid()) {
		t.Log("still running")
	}
}

// Clean: another state letter read from stat (stopped is the one thread's state that matters).
func stopped(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(stat, ')')
	return i >= 0 && i+2 < len(stat) && stat[i+2] == 'T'
}

// Clean: the whole group's end, from status: State Z and no thread left but the leader.
func exited(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return true
	}
	var state, threads string
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "State:"); ok {
			state = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "Threads:"); ok {
			threads = strings.TrimSpace(v)
		}
	}
	return strings.HasPrefix(state, "Z") && threads == "1"
}

// Clean: a Z that is a letter, in a function that reads no stat.
func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// Clean: a stat fixture that holds Z as data, compared with nothing.
func TestFixture(t *testing.T) {
	line := "4242 (bash) Z 1 0 0 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 50000 0 0\n"
	if err := os.WriteFile(filepath.Join(t.TempDir(), "stat"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if !stopped(1) && !exited(1) && isLetter('q') {
		t.Log(gone(1), goneSprintf(1), alive(1))
	}
}

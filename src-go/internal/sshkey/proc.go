package sshkey

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// procParent is pid's parent, read from /proc/<pid>/stat (E14).
func procParent(pid int) (int, error) {
	if pid <= 0 {
		return 0, fmt.Errorf("no process %d", pid)
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	return parentFromStat(string(stat))
}

// parentFromStat is the parent pid of a /proc/<pid>/stat line: the second field after the
// command name, which is in parentheses and may itself hold spaces and parentheses.
func parentFromStat(line string) (int, error) {
	end := strings.LastIndexByte(line, ')')
	if end < 0 {
		return 0, errors.New("a stat line with no command name")
	}
	fields := strings.Fields(line[end+1:])
	if len(fields) < 2 {
		return 0, errors.New("a stat line with no parent")
	}
	return strconv.Atoi(fields[1])
}

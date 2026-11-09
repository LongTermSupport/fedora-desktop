// Package badzombie is the fixture of project_zombie_exit.
package badzombie

import (
	"os"
	"strconv"
	"strings"
)

// Reaped is production code, judged the same: the ccy launcher's build lock had this shape.
func Reaped(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	return strings.Fields(string(data))[2] == "Z" // want `"Z" is the zombie state of the thread-group leader`
}

// Ended is clean: the stat state is believed only with the thread count of status beside it.
func Ended(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	if strings.Fields(string(data))[2] != "Z" {
		return false
	}
	status, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return true
	}
	for line := range strings.SplitSeq(string(status), "\n") {
		if v, ok := strings.CutPrefix(line, "Threads:"); ok {
			return strings.TrimSpace(v) == "1"
		}
	}
	return false
}

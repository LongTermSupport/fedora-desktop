package ccylaunch

import (
	"regexp"
	"strconv"
)

// The pids limit of every container ccy runs, as lib/container-limits.bash settles it as soon as
// the bash launcher has loaded its libraries, before it answers --version or --help;
// TestPidsLimitIsTheLibrarys holds these to the library.
const (
	pidsLimitEnv     = "CCY_PIDS_LIMIT"
	pidsLimitDefault = 4096
	pidsLimitMin     = 256
	pidsLimitMax     = 32768
)

var pidsLimitForm = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)

// pidsLimit is the limit ccy_container_limits settles value to (the default for unset or
// empty), and false where the library refuses it: then the front hands over and the bash
// prints its own refusal.
func pidsLimit(value string) (string, bool) {
	if value == "" {
		return strconv.Itoa(pidsLimitDefault), true
	}
	if !pidsLimitForm.MatchString(value) {
		return "", false
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < pidsLimitMin || n > pidsLimitMax {
		return "", false
	}
	return value, true
}

package ccylaunch

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
)

// The pids limit of every container ccy runs, as lib/container-limits.bash settles it as soon as
// the bash launcher has loaded its libraries, before it answers --version or --help;
// TestPidsLimitIsTheLibrarys holds these to the library.
const (
	pidsLimitEnv     = "CCY_PIDS_LIMIT"
	pidsLimitDefault = engine.PidsLimitDefault
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

// settledPidsLimit is the --pids-limit of a container the bash launcher asks the front to run
// (F17-F19), or why there is none. The bash has settled the limit before it asks, so a refused
// value here is one exported between the two, and is refused as the library refuses it.
func (f Front) settledPidsLimit() (string, string) {
	value := f.getenv(pidsLimitEnv)
	limit, ok := pidsLimit(value)
	if !ok {
		return "", fmt.Sprintf("%s=%s is not a pids limit: a whole number from %d to %d", pidsLimitEnv, value, pidsLimitMin, pidsLimitMax)
	}
	return limit, ""
}

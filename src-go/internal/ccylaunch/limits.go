package ccylaunch

import (
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylimits"
)

// limits is the limit set of every container ccy runs, as lib/container-limits.bash settles it
// as soon as the bash launcher has loaded its libraries, before it answers --version or --help
// (internal/ccylimits, held to the library by its tests): the pids limit, the memory caps from
// the agents slice's MemoryMax within this machine's /proc/meminfo, and the slice. The reason is non-empty where the library
// refuses: then the front hands over and the bash prints its own refusal.
func (f Front) limits() (ccylimits.Limits, string) {
	meminfo, err := os.ReadFile(filepath.Join(f.procRoot(), "meminfo"))
	if err != nil {
		return ccylimits.Limits{}, "could not read this machine's memory: " + err.Error()
	}
	lim, err := ccylimits.Settle(f.getenv, meminfo)
	if err != nil {
		return ccylimits.Limits{}, err.Error()
	}
	return lim, ""
}

//go:build busfailpoints

package failpoint

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
)

// envName is the variable that arms points: a comma list of names.
const envName = "TEAMCHAT_FAILPOINTS"

var errUnknown = errors.New("failpoint: " + envName + " names no known point")

var known = map[Name]bool{
	KeeperLinkWriting: true, LinkInRemove: true, LinkAfterRemoveAnswered: true, LinkAfterHandOut: true,
	LinkWhileHanded: true, KeeperBeforeStoreDelete: true, LinkMidEndSends: true, KeeperBeforeEnd: true,
	LinkAfterJudgeBeforePersist: true,
}

// armed is the process's armed points, read once.
var armed = sync.OnceValues(func() (map[Name]bool, error) { return parse(os.Getenv(envName)) })

func parse(list string) (map[Name]bool, error) {
	out := map[Name]bool{}
	if list == "" {
		return out, nil
	}
	for n := range strings.SplitSeq(list, ",") {
		if !known[Name(n)] {
			return nil, fmt.Errorf("%w: %q", errUnknown, n)
		}
		out[Name(n)] = true
	}
	return out, nil
}

// Hit ends the process with SIGKILL when name is armed. A list naming an unknown point panics
// here, since a misspelt point would never fire and its test would pass without its crash.
func Hit(name Name) {
	points, err := armed()
	if fire(name, points, err, syscall.Kill) {
		select {} // the signal is on its way; nothing after the point may run
	}
}

// fire sends this process SIGKILL through kill when name is armed, and says whether it did.
func fire(name Name, points map[Name]bool, armErr error, kill func(pid int, sig syscall.Signal) error) bool {
	if armErr != nil {
		panic(armErr)
	}
	if !points[name] {
		return false
	}
	if err := kill(os.Getpid(), syscall.SIGKILL); err != nil {
		panic("failpoint: kill: " + err.Error())
	}
	return true
}

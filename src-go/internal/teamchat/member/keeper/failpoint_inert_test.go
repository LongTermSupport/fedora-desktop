//go:build !busfailpoints

package keeper

import (
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

// Without busfailpoints a point named in the environment is inert: the keeper in its own
// process ends the session and removes its run.
func TestFailpointsInert(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	c := reexec.Command(t, "keeper", w.dir, string(failpoint.KeeperBeforeEnd))
	c.Env = append(c.Env, "TEAMCHAT_FAILPOINTS="+string(failpoint.KeeperBeforeEnd))
	res, err := execx.OS{}.Run(t.Context(), c)
	if err != nil {
		t.Fatalf("%v: %s", err, res.Stderr)
	}
	entries, err := os.ReadDir(w.layout.RunRoot())
	mustDo(t, err)
	if len(entries) != 0 {
		t.Fatalf("the run root holds %d entries", len(entries))
	}
}

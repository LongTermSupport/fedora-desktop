//go:build busfailpoints

package keeper

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

// An armed point kills the keeper where a crash would: before the end, or while the link
// writes a team's state. Its run is left with its logs, and the next keeper's sweep removes it
// once its keeper.lock is free and its keeper and session are gone (T8).
func TestFailpointsKillTheKeeper(t *testing.T) {
	for _, point := range []failpoint.Name{failpoint.KeeperBeforeEnd, failpoint.KeeperLinkWriting} {
		t.Run(string(point), func(t *testing.T) {
			w := newWorld(t, plainLaunch...)
			c := reexec.Command(t, "keeper", w.dir, string(point))
			c.Env = append(c.Env, "TEAMCHAT_FAILPOINTS="+string(point))
			res, err := execx.OS{}.Run(t.Context(), c)
			var killed *execx.ExitError
			if !errors.As(err, &killed) || killed.Signal != syscall.SIGKILL {
				t.Fatalf("%v: %s", err, res.Stderr)
			}
			entries, err := os.ReadDir(w.layout.RunRoot())
			mustDo(t, err)
			if len(entries) != 1 {
				t.Fatalf("the run root holds %d entries", len(entries))
			}
			left := w.layout.RunRoot() + "/" + entries[0].Name()
			p := w.startPlain(w.config())
			if exists(t, left) || !contains(w.logOf(p.run), "removed") {
				t.Fatalf("the crashed run was not swept: %s", w.logOf(p.run))
			}
			p.finish(0)
		})
	}
}

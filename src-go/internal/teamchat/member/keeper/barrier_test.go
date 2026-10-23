package keeper

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
)

// The harness's guard (rule keeper-log-barrier-turn): logged is a barrier on the loop's turn,
// not on the line. A turn that logs a line and then does more (the ready turn logs "the link
// is ready" and only then starts CMD) is still under way when the line reaches keeper.log, so
// logged must not see it until the turn has ended, or a test asserting the turn's later effect
// races it.
func TestLoggedWaitsForTheTurnThatWroteTheLine(t *testing.T) {
	w := newWorld(t, claudeLaunch...)
	w.writeDesired(desiredJSON(1, state.InboundAccept))
	midTurn := make(chan bool, 1)
	w.starter.OnStart([]string{w.launch.Cmd[0]}, func(c execx.Cmd) (execx.Started, error) {
		midTurn <- w.seen(w.runDir(), "the link is ready", 1)
		return w.startCmd(c)
	})
	res := w.start()
	l := w.up(0)
	if <-midTurn {
		t.Fatal("logged saw the ready line while its turn was still starting CMD")
	}
	c := w.cmd()
	l.expect(t, wire.KindPID)
	w.sessionStarted(l)
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
}

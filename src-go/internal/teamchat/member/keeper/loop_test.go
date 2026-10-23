package keeper

import (
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
)

// RN2: a link that is never ready is killed at 30 s, and the launch fails with 78 naming
// link.log, the run kept so the log can be read; CMD is never started.
func TestNeverReadyFailsTheLaunch(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	res := w.start()
	l := w.link()
	l.expect(t, wire.KindLaunch)
	w.advance(readyWait)
	if s := l.signal(); s != syscall.SIGKILL {
		t.Fatalf("the link was sent %v", s)
	}
	exitOf(t, res, exits.ExitConfig)
	if len(w.starter.Starts()) != 1 {
		t.Fatal("CMD was started")
	}
	run := w.runDir()
	if !exists(t, run.LinkLog()) {
		t.Fatal("link.log is gone")
	}
	if want := run.LinkLog(); !contains(w.stderr.String(), want) {
		t.Fatalf("stderr %q does not name %s", w.stderr.String(), want)
	}
}

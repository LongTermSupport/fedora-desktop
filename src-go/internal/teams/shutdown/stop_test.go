package shutdown

import (
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SD10: a busy session is interrupted once, then sent /exit when its prompt is back.
func TestABusySessionIsInterruptedFirst(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.sessions[0]
	s.pane = paneBusy
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if s.escapes != 1 || !slices.Equal(m.typedInto("ccy-proj"), []string{"/exit"}) {
		t.Fatalf("escapes %d typed %q", s.escapes, m.typedInto("ccy-proj"))
	}
}

// SD10: unsent text is never typed over, and the session is not stopped.
func TestUnsentTextIsLeftAlone(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].pane = paneTyped
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.typedInto("ccy-proj")) != 0 || m.killed() || m.sessions[0].enters != 0 {
		t.Fatal("the session was touched")
	}
	m.errHas(t, "ccy-proj: failed: its input box holds unsent text; attach it (ccy-sessions), send or clear it, and run shutdown again",
		"alpha: 0 exited cleanly, 0 stopped, 1 failed.")
}

// SD10: the completion menu took the Enter: one more Enter.
func TestExitLeftInTheBoxGetsOneMoreEnter(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.sessions[0]
	s.ignoreExit = true
	s.afterExit = paneTyped
	m.run("alpha", "--yes")
	if s.enters != 2 {
		t.Fatalf("enters %d", s.enters)
	}
}

// SD10, SD12: a prompt that never draws is waited for, then the session is stopped.
func TestAPromptThatNeverDrawsIsStopped(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].pane = paneStart
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(m.typedInto("ccy-proj")) != 0 || !m.killed() {
		t.Fatal("not stopped untyped")
	}
	m.errHas(t, "ccy-proj: stopped: its prompt was not ready within 30 s, so its tmux session was killed")
}

// SD10: at a launcher question claude is not running: stopped at once.
func TestALauncherQuestionIsStoppedAtOnce(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].pane = paneSSHKey
	m.run("alpha", "--yes")
	if !m.killed() || len(m.sleeps) > 2 {
		t.Fatalf("killed %v sleeps %v", m.killed(), m.sleeps)
	}
	m.errHas(t, "ccy-proj: stopped: it was at the launcher's ssh-key question, so claude was not running")
}

// SD11: claude ended and the launcher asks about compose services: exited, left at the question.
func TestTheComposeQuestionAfterExitIsACleanExit(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].afterExit = paneCompose
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if m.killed() {
		t.Fatal("the compose question was killed")
	}
	m.errHas(t, "ccy-proj: exited cleanly; its launcher asks whether to stop compose services: attach it (ccy-sessions) to answer")
}

// SD11: claude ended and the launcher reported an error: the window is closed.
func TestAnEndedWindowIsClosed(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].afterExit = paneEnded
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if !m.killed() {
		t.Fatal("the window was left")
	}
	m.errHas(t, "ccy-proj: exited cleanly; claude ended and its launcher reported an error, so the window was closed")
}

// SD14: a seat still held after the session ended fails it.
func TestASeatStillHeldFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.sessions[0].keepSeat = true
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "ccy-proj: failed: seat 1@alpha is still held 30 s after: something of the session still runs (podman ps)")
}

// SD12, SD13: a pane that cannot be read fails the session and kills nothing: it may hold
// unsent text.
func TestAnUnreadablePaneFailsTheSessionAndKillsNothing(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.OnErr("tmux -L ccy capture-pane", "server exited unexpectedly", 1)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if m.killed() || len(m.typedInto("ccy-proj")) != 0 {
		t.Fatal("the session was touched")
	}
	m.errHas(t, "ccy-proj: failed: the screen of ccy-proj could not be read: server exited unexpectedly; nothing was typed or stopped")
}

// SD13: a tmux failure fails that session, with tmux's words.
func TestATmuxFailureFailsTheSession(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.OnErr("tmux -L ccy send-keys", "server exited unexpectedly", 1)
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "ccy-proj: failed: typing into ccy-proj failed: server exited unexpectedly")
}

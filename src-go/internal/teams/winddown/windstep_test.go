package winddown

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// W7: a pane that cannot be read fails, typing nothing.
func TestAPaneThatCannotBeReadFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{ExitCode: 1, Stderr: []byte("server exited unexpectedly")}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	if len(m.typed()) != 0 {
		t.Fatal("something was typed")
	}
	m.errHas(t, "✗ ccy-proj: the screen of ccy-proj could not be read: server exited unexpectedly; nothing was typed")
}

// W6: a box that is never drawn is waited for up to the bound.
func TestABoxNeverDrawnFailsAtTheBound(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.proj().pane = "Building image...\n"
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: its input box was not drawn within "+secs(300)+"; nothing was typed")
}

// W7: a session gone before anything was typed fails.
func TestASessionGoneBeforeAnythingWasTypedFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	ended := false
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "list-sessions"}, func(execx.Cmd) (execx.Result, error) {
		if ended {
			return execx.Result{}, nil
		}
		return execx.Result{Stdout: []byte("ccy-proj 0 " + m.Checkout() + "\n")}, nil
	})
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane"}, func(execx.Cmd) (execx.Result, error) {
		ended = true
		return execx.Result{ExitCode: 1, Stderr: []byte("can't find pane: ccy-proj")}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: the session ended before anything was typed")
}

// W7: a keystroke that fails stops the job.
func TestAKeystrokeThatFailsStopsTheJob(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{ExitCode: 1, Stderr: []byte("no space left")}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: typing into ccy-proj failed: no space left")
}

// W5: a line slow to leave the box gets one poll of grace, so it is not submitted twice.
func TestALineSlowToClearIsNotEnteredTwice(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	s.slowClear = true
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if s.enters != 2 || len(s.entered) != 2 {
		t.Fatalf("enters %d entered %q: a slow line was entered twice", s.enters, s.entered)
	}
}

// W5: a session busy after /goal clear is waited for before the wind-down is typed.
func TestBusyAfterGoalClearIsWaitedFor(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	s.busyAfterGoal = 4
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
	if len(s.entered) != 2 || s.escapes != 0 {
		t.Fatalf("entered %q escapes %d", s.entered, s.escapes)
	}
}

func TestBusyPastTheBoundAfterGoalClearFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.proj().busyAfterGoal = 100000
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear went in, but it stayed busy for "+secs(300))
}

// W5: a session that ends once the wind-down is in has had it.
func TestASessionEndingAfterTheWindDownIsDone(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.proj().endAfterText = true
	if got := m.run("alpha", "--yes"); got != term.ExitOK {
		t.Fatalf("exit %d\n%s", got, m.ErrOut.String())
	}
}

func TestABoxNotDrawnAgainAfterTheWindDownFails(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	m.proj().paneAfterText = "Building image...\n"
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: /goal clear went in, but its input box was not drawn again within "+secs(30)+" of the wind-down")
}

// W7: an Enter that fails says the line was typed and not entered.
func TestAnEnterThatFailsSaysTheLineIsInTheBox(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	s := m.proj()
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Args[len(c.Args)-1] == "Enter" {
			return execx.Result{ExitCode: 1, Stderr: []byte("lost")}, nil
		}
		m.press(s, c.Args[len(c.Args)-2:])
		return execx.Result{}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "✗ ccy-proj: pressing Enter in ccy-proj failed: lost (the line was typed, not entered)")
}

// W7: a failed listing of the sessions stops the watch.
func TestSessionsThatCannotBeListedStopTheWatch(t *testing.T) {
	m := newMachine(t)
	m.noLxc()
	lists := 0
	m.Fake.OnFunc([]string{"tmux", "-L", "ccy", "list-sessions"}, func(execx.Cmd) (execx.Result, error) {
		lists++
		if lists > 1 {
			return execx.Result{ExitCode: 1, Stderr: []byte("server gone")}, nil
		}
		return execx.Result{Stdout: []byte("ccy-proj 0 " + m.Checkout() + "\n")}, nil
	})
	if got := m.run("alpha", "--yes"); got != term.ExitFailure {
		t.Fatalf("exit %d", got)
	}
	m.errHas(t, "the sessions could not be watched any further; nothing was typed")
}

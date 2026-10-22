package hot

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// An add stops with a reason at each step that cannot be read, and changes nothing.
func TestAddSaysWhichStepCouldNotBeRead(t *testing.T) {
	cases := []struct {
		name string
		fail func(r *rig)
		say  string
	}{
		{"systemctl cannot run", func(r *rig) {
			r.Fake.OnFunc([]string{"systemctl", "is-active", "agent-bus-hs@alpha.service"}, func(execx.Cmd) (execx.Result, error) {
				return execx.Result{}, errors.New("no systemctl")
			})
		}, "systemctl could not say whether its homeserver is up"},
		{"the roster cannot be read", func(r *rig) {
			r.OnErr(teamtest.SudoBus+"list alpha", "no route", 2)
		}, "its members could not be read"},
		{"the seat list cannot be read", func(r *rig) {
			r.CrashSeat(t, r.Checkout(), "alpha", "9")
		}, "seat: agent-bus seat list in "},
	}
	for _, c := range cases {
		r := newRig(t)
		c.fail(r)
		if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
			t.Errorf("%s: exit %d:\n%s", c.name, code, r.ErrOut.String())
			continue
		}
		if got := r.ErrOut.String(); !strings.Contains(got, c.say) {
			t.Errorf("%s: stderr lacks %q:\n%s", c.name, c.say, got)
		}
		if len(r.desired(t).Seats) != 0 {
			t.Errorf("%s: the membership changed", c.name)
		}
	}
}

// A seat list the in-process reader cannot read stops the add before a seat is taken, naming the
// checkout and the reader's reason.
func TestAddSaysWhyTheSeatListCannotBeRead(t *testing.T) {
	r := newRig(t)
	r.CrashSeat(t, r.Checkout(), "alpha", "9")
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	got := r.ErrOut.String()
	if want := "seat: agent-bus seat list in " + r.Checkout() + ": agent-bus seat list: "; !strings.Contains(got, want) || !strings.Contains(got, "urlsplit") {
		t.Fatalf("stderr lacks %q and the reason:\n%s", want, got)
	}
	if r.Fake.Called("agent-bus seat take") || len(r.desired(t).Seats) != 0 {
		t.Fatalf("a seat was taken or the membership changed:\n%s", got)
	}
}

// A seat list that cannot be read once seat take has made the seat stops the add: the new seat
// cannot be shown free, so the membership is not changed.
func TestAddSaysWhenTheSeatListCannotBeReadAfterTheSeatIsMade(t *testing.T) {
	r := newRig(t)
	r.m.heldBy["3@alpha"] = true
	r.Fake.OnFunc([]string{"agent-bus", "seat", "take"}, func(c execx.Cmd) (execx.Result, error) {
		r.m.mu.Lock()
		r.m.exists[c.Args[2]] = "proj." + seatOfRef(c.Args[2]) + "+local.podman"
		r.m.mu.Unlock()
		r.CrashSeat(t, r.Checkout(), "alpha", "9")
		return execx.Result{}, nil
	})
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if want := "✗ seat 4: agent-bus seat take ran, but the seat list cannot be read: agent-bus seat list in " + r.Checkout() + ": "; !strings.Contains(r.ErrOut.String(), want) {
		t.Fatalf("stderr lacks %q:\n%s", want, r.ErrOut.String())
	}
	if len(r.desired(t).Seats) != 0 {
		t.Fatalf("the membership changed:\n%s", r.ErrOut.String())
	}
}

// A seat take that agent-bus refuses stops the add with its reason; a busy one is exitBusy.
func TestAddSaysWhySeatTakeRefused(t *testing.T) {
	for _, c := range []struct {
		code int
		want term.ExitCode
	}{{75, exitBusy}, {1, term.ExitFailure}} {
		r := newRig(t)
		r.m.heldBy["3@alpha"] = true
		r.OnErr("agent-bus seat take", "seat 4@alpha is taken ("+strconv.Itoa(c.code)+")\n", c.code)
		if code := r.run(t, "add", session, "alpha", "--yes"); code != c.want {
			t.Fatalf("exit %d for %d:\n%s", code, c.code, r.ErrOut.String())
		}
		wantLines(t, r.ErrOut.String(), "✗ seat 4: agent-bus seat take: seat 4@alpha is taken ("+strconv.Itoa(c.code)+")")
		if len(r.desired(t).Seats) != 0 {
			t.Fatalf("%d: the membership changed", c.code)
		}
	}
}

// With every seat agent-bus offers held, an add takes the next number past them; with that one
// held too, it stops and says so.
func TestAddTakesTheNextSeatPastEveryOneOffered(t *testing.T) {
	r := newRig(t)
	r.m.heldBy["3@alpha"] = true
	r.On("agent-bus seat choose alpha --type=podman", "CHOICE\t3\theld\tproj.3+local.podman\tw\nDEFAULT\t3\n", 0)
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✓ seat 4: created (proj.4+local.podman)")

	r = newRig(t)
	r.m.heldBy["3@alpha"], r.m.heldBy["4@alpha"] = true, true
	r.m.exists["4@alpha"] = "proj.4+local.podman"
	r.On("agent-bus seat choose alpha --type=podman", "CHOICE\t3\theld\tproj.3+local.podman\tw\nDEFAULT\t3\n", 0)
	if code := r.run(t, "add", session, "alpha", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ seat 3: every seat of alpha in this checkout is held or named by another session")
}

// An add of a team the session is already in, whose seat list cannot be read, cannot show the
// seat held: it waits for it, then fails, and does not say there is nothing to do.
func TestAddOfATeamAlreadyInSaysWhenItsSeatCannotBeShownHeld(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.CrashSeat(t, r.Checkout(), "alpha", "9")
	if code := r.run(t, "add", session, "alpha/3", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.ErrOut.String(); !strings.Contains(got, "✗ session: seat 3 not held after ") ||
		!strings.Contains(got, "; the seat list cannot be read: agent-bus seat list in ") || strings.Contains(got, "Nothing to do") {
		t.Fatalf("stderr:\n%s", got)
	}
}

// session list says, in the session's STATE, that its checkout's seats cannot be read.
func TestListSaysWhenASessionsSeatsCannotBeRead(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	r.CrashSeat(t, r.Checkout(), "alpha", "9")
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.Out.String(); !strings.Contains(got, "its seats cannot be read: agent-bus seat list: ") || !strings.Contains(got, "urlsplit") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", got, r.ErrOut.String())
	}
}

// A change to a running session stops, with the reason, where its session cannot be found.
func TestAChangeSaysWhyTheSessionCannotBeChanged(t *testing.T) {
	cases := []struct {
		name string
		fail func(r *rig)
		say  string
	}{
		{"no runtime directory", func(r *rig) { r.Env["XDG_RUNTIME_DIR"] = "run" }, "XDG_RUNTIME_DIR is not an absolute path"},
		{"no record", func(r *rig) {
			if err := r.store.Remove(session); err != nil {
				t.Fatal(err)
			}
		}, "only a session ccy-sessions knows"},
		{"no membership directory", func(r *rig) {
			if err := os.RemoveAll(r.dir()); err != nil {
				t.Fatal(err)
			}
		}, "launched without the agent team bus kit"},
		{"a membership that is another kind's", func(r *rig) {
			d := r.desired(t)
			d.Kind = membership.KindCC
			if err := membership.Write(r.dir(), d); err != nil {
				t.Fatal(err)
			}
		}, "its membership is a cc session's"},
	}
	for _, c := range cases {
		r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
		c.fail(r)
		if code := r.run(t, "remove", session, "alpha", "--yes"); code == term.ExitOK {
			t.Errorf("%s: exit %d:\n%s", c.name, code, r.ErrOut.String())
		} else if got := r.ErrOut.String(); !strings.Contains(got, c.say) {
			t.Errorf("%s: stderr lacks %q:\n%s", c.name, c.say, got)
		}
	}
}

func TestAChangeSaysWhenTheRecordsOrPodmanCannotBeRead(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.Fake.OnFunc([]string{"podman", "ps"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stderr: []byte("cannot connect"), ExitCode: 125}, nil
	})
	if code := r.run(t, "remove", session, "alpha", "--yes"); code == term.ExitOK || !strings.Contains(r.ErrOut.String(), "podman could not list its container") {
		t.Fatalf("podman: exit %d:\n%s", code, r.ErrOut.String())
	}
	r = newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.x.Host.Registry = func() (*registry.Store, error) { return nil, errors.New("records unreadable") }
	if code := r.run(t, "remove", session, "alpha", "--yes"); code == term.ExitOK || !strings.Contains(r.ErrOut.String(), "session "+session+": records unreadable") {
		t.Fatalf("records: exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestRoleSaysWhyARoleCouldNotBeChanged(t *testing.T) {
	cases := []struct {
		name string
		fail func(r *rig)
		say  string
	}{
		{"not a member any more", func(r *rig) {
			reads := 0
			r.Fake.OnFunc(strings.Fields(teamtest.SudoBus+"list alpha"), func(execx.Cmd) (execx.Result, error) {
				reads++
				if reads > 1 {
					return execx.Result{}, nil
				}
				return r.roster("alpha"), nil
			})
		}, "is not a member of alpha"},
		{"sudo is not ready", func(r *rig) { r.On("sudo -n -v", "", 1) }, "sudo is not ready"},
		{"agent-bus refuses", func(r *rig) { r.OnErr(setRoleCmd, "refused", 1) }, "role: proj.3+local.podman was not made a coordinator"},
	}
	for _, c := range cases {
		r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
		c.fail(r)
		if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitFailure {
			t.Errorf("%s: exit %d:\n%s", c.name, code, r.ErrOut.String())
		} else if got := r.ErrOut.String(); !strings.Contains(got, c.say) {
			t.Errorf("%s: stderr lacks %q:\n%s", c.name, c.say, got)
		}
	}
}

func TestAddSaysWhenNoSeatCanBeChosen(t *testing.T) {
	r := newRig(t)
	r.OnErr("agent-bus seat choose alpha", "no seats", 2)
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), "seat: agent-bus seat choose alpha: agent-bus exited 2: no seats") {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestAChangeToACCSessionSaysWhenTmuxCannotBeAsked(t *testing.T) {
	r := newRigAs(t, membership.KindCC, "cc.4242", membership.Seat{Team: "alpha", Seat: "3"})
	r.Fake.OnFunc([]string{"tmux", "-L", "ccy", "list-panes"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stderr: []byte("no server"), ExitCode: 1}, nil
	})
	if code := r.run(t, "remove", "cc.4242", "alpha", "--yes"); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), "session cc.4242: ") || !strings.Contains(r.ErrOut.String(), "no server") {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestListSaysWhenTheCCSessionsCannotBeListed(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.labelled("abc123\trunning\t" + session + "\n")
	r.Fake.OnFunc([]string{"tmux", "-L", "ccy", "list-panes"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stderr: []byte("no server"), ExitCode: 1}, nil
	})
	if code := r.run(t, "list"); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), "the cc sessions could not be listed") {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestRoleSaysWhenTheRosterFailsOnItsSecondRead(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	reads := 0
	r.Fake.OnFunc(strings.Fields(teamtest.SudoBus+"list alpha"), func(execx.Cmd) (execx.Result, error) {
		reads++
		if reads > 1 {
			return execx.Result{Stderr: []byte("no route"), ExitCode: 2}, nil
		}
		return r.roster("alpha"), nil
	})
	if code := r.run(t, "role", session, "alpha", coord, "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if got := r.ErrOut.String(); !strings.Contains(got, "role: the members of alpha could not be read") {
		t.Fatalf("stderr:\n%s", got)
	}
}

func TestRemoveAndRoleSayWhenTheTeamCannotBeRead(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	r.OnErr(teamtest.SudoBus+"list alpha", "no route", 2)
	cases := []struct {
		args []string
		say  string
	}{
		{[]string{"remove", session, "alpha", "--yes"}, "the members of alpha could not be read: no route"},
		{[]string{"role", session, "alpha", worker, "--yes"}, "team alpha: its members could not be read: no route"},
	}
	for _, c := range cases {
		r.ErrOut.Reset()
		if code := r.run(t, c.args...); code == term.ExitOK {
			t.Errorf("%v: exit %d:\n%s", c.args, code, r.ErrOut.String())
		}
		if got := r.ErrOut.String(); !strings.Contains(got, c.say) {
			t.Errorf("%v: stderr lacks %q:\n%s", c.args, c.say, got)
		}
	}
}

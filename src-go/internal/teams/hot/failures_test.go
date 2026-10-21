package hot

import (
	"errors"
	"os"
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
			r.OnErr("agent-bus seat list", "no checkout", 2)
		}, "agent-bus seat list in"},
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
		}, session},
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
		{"agent-bus refuses", func(r *rig) { r.OnErr(setRoleCmd, "refused", 1) }, "role: "},
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
	if code := r.run(t, "add", session, "alpha", "--yes"); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), "seat: ") {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
}

func TestAChangeToACCSessionSaysWhenTmuxCannotBeAsked(t *testing.T) {
	r := newRigAs(t, membership.KindCC, "cc.4242", membership.Seat{Team: "alpha", Seat: "3"})
	r.Fake.OnFunc([]string{"tmux", "-L", "ccy", "list-panes"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stderr: []byte("no server"), ExitCode: 1}, nil
	})
	if code := r.run(t, "remove", "cc.4242", "alpha", "--yes"); code != term.ExitFailure || !strings.Contains(r.ErrOut.String(), "session cc.4242: ") {
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

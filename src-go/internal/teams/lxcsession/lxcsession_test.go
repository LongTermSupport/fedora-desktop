package lxcsession

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// rig is a machine with team alpha's LXC member proj.box+role.lxc, agent in container box,
// recorded joined by the last apply, and the Starter over it.
type rig struct {
	*teamtest.World
	t       *testing.T
	s       *Starter
	running bool
	// unitFails is what systemctl start says when it fails; "" starts the unit.
	unitFails string
}

const handle = "proj.box+role.lxc"

func newRig(t *testing.T, status string) *rig {
	t.Helper()
	r := &rig{World: teamtest.Populated(t), t: t, running: true}
	teamtest.MustWrite(t, filepath.Join(r.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [{"container": "box", "user": "agent", "state": "present", `+
			`"status": "`+status+`", "handle": "`+handle+`", "checkout": "/srv/proj", "why": "", "since": 1}]}`)
	r.On("sudo -n -v", "", 0)
	r.On("sudo -n lxc-config lxc.lxcpath", "/var/lib/lxc\n", 0)
	r.On("sudo -n lxc-ls -1", "box\nother\n", 0)
	r.Fake.OnFunc([]string{"sudo", "-n", "lxc-ls", "-1", "--running"}, func(execx.Cmd) (execx.Result, error) {
		if r.running {
			return execx.Result{Stdout: []byte("box\n")}, nil
		}
		return execx.Result{}, nil
	})
	r.Fake.OnFunc([]string{"sudo", "-n", "lxc-attach"}, func(execx.Cmd) (execx.Result, error) {
		if r.unitFails != "" {
			return execx.Result{ExitCode: 5, Stderr: []byte(r.unitFails)}, nil
		}
		return execx.Result{}, nil
	})
	h := r.Host
	h.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	r.s = New(h, flow.New(h))
	return r
}

func (r *rig) row() board.Row {
	return board.Row{Team: "alpha", Member: handle, Kind: "lxc", Handle: handle, State: "joined"}
}

// started is every lxc-attach call that ran systemctl, as its argv.
func (r *rig) started() [][]string {
	var got [][]string
	for _, c := range r.Fake.Calls() {
		if c.Name == "sudo" && slices.Contains(c.Args, "lxc-attach") {
			got = append(got, c.Args)
		}
	}
	return got
}

func (r *rig) errHas(wants ...string) {
	r.t.Helper()
	for _, want := range wants {
		if !strings.Contains(r.ErrOut.String(), want) {
			r.t.Fatalf("stderr lacks %q:\n%s", want, r.ErrOut.String())
		}
	}
}

// The one start of a member's session: a plain systemctl start of its unit inside the running
// container, as root, which works with the unit not enabled (nothing enables it).
func TestStartItsSessionStartsTheUnitInItsContainer(t *testing.T) {
	r := newRig(t, "joined")
	if _, err := r.s.Start(t.Context(), r.row()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-n", "lxc-attach", "-P", "/var/lib/lxc", "-n", "box", "--clear-env", "--",
		"env", "-i", "PATH=/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin",
		"systemctl", "start", "--", "agent-bus-claude-agent.service",
	}
	if got := r.started(); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Fatalf("lxc-attach calls %q", got)
	}
	r.errHas("Started agent-bus-claude-agent.service in container box (agent@box).",
		"sudo lxc-attach -n box -- runuser -u agent -- tmux -L agent-bus-claude attach")
}

// A member the last apply skipped because its container was stopped is a member all the same.
func TestAMemberRecordedStoppedIsStartedOnceItsContainerRuns(t *testing.T) {
	r := newRig(t, "stopped")
	if _, err := r.s.Start(t.Context(), r.row()); err != nil {
		t.Fatal(err)
	}
	if len(r.started()) != 1 {
		t.Fatalf("calls %v", r.Fake.Calls())
	}
}

func TestAStoppedContainerIsSaidAndNothingIsStarted(t *testing.T) {
	r := newRig(t, "joined")
	r.running = false
	if _, err := r.s.Start(t.Context(), r.row()); err != nil {
		t.Fatal(err)
	}
	if len(r.started()) != 0 {
		t.Fatalf("started in a stopped container: %v", r.started())
	}
	r.errHas("Nothing was started: container box is not running.")
}

func TestAMemberNoRecordNamesIsSaidAndNothingIsStarted(t *testing.T) {
	for _, status := range []string{"deferred", "left"} {
		t.Run(status, func(t *testing.T) {
			r := newRig(t, status)
			if _, err := r.s.Start(t.Context(), r.row()); err != nil {
				t.Fatal(err)
			}
			if len(r.started()) != 0 {
				t.Fatalf("started: %v", r.started())
			}
			r.errHas("Nothing was started: no LXC member record of this host names " + handle + " as a member")
		})
	}
}

func TestAStartSystemdRefusesIsSaidWithItsWords(t *testing.T) {
	r := newRig(t, "joined")
	r.unitFails = "Failed to start agent-bus-claude-agent.service: Unit agent-bus-claude-agent.service not found.\n"
	if _, err := r.s.Start(t.Context(), r.row()); err != nil {
		t.Fatal(err)
	}
	r.errHas("Its session was not started: systemctl start agent-bus-claude-agent.service in container box failed",
		"Unit agent-bus-claude-agent.service not found.",
		"service: true")
}

func TestUnitIsTheSessionUnitThePlayWrites(t *testing.T) {
	if got := Unit("agent"); got != "agent-bus-claude-agent.service" {
		t.Fatal(got)
	}
}

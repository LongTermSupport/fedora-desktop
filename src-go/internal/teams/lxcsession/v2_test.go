package lxcsession

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

const v2Handle = "proj.1+desk.lxc"

// v2Rig is a machine with the v2 team's LXC seat v2Handle, agent in container box, and the
// unit's state systemd gives inside it.
type v2Rig struct {
	*rig
	load, active string
}

func newV2Rig(t *testing.T) *v2Rig {
	t.Helper()
	r := &v2Rig{rig: newRig(t, "joined"), load: "loaded", active: "inactive"}
	r.WriteV2Lxc(t, teamtest.V2Team, teamtest.V2Seat(t, "box", "agent", "/srv/proj", 1, v2Handle, lxcstate.StatusJoined, ""))
	r.Fake.OnFunc([]string{"sudo", "-n", "lxc-attach"}, func(c execx.Cmd) (execx.Result, error) {
		if !slices.Contains(c.Args, "show") {
			return execx.Result{ExitCode: 1, Stderr: []byte("not asked")}, nil
		}
		return execx.Result{Stdout: []byte("LoadState=" + r.load + "\nActiveState=" + r.active + "\n")}, nil
	})
	r.On("sudo -n "+paths.AdminBinaryPath+" lxc start", "", 0)
	h := r.Host
	r.s = New(h, flow.New(h))
	return r
}

func v2Row() board.Row {
	return board.Row{Team: teamtest.V2Team, Member: v2Handle, Kind: "lxc", Handle: v2Handle, State: "enrolled, offline", V2: true}
}

// v2Target is the seat's --member, <container>/<seat key>.
func v2Target(t *testing.T) string {
	t.Helper()
	team, err := paths.ParseTeamID(teamtest.V2Team)
	if err != nil {
		t.Fatal(err)
	}
	return "box/" + paths.LXCSeatKey(team, "/srv/proj", 1).String()
}

// TC16: offered only where it can work: the seat in the LXC record, its container running, its
// unit file placed (service: true) and its session not running.
func TestAV2SeatsStartIsOfferedOnlyWhereItCanWork(t *testing.T) {
	for _, tc := range []struct {
		name, load, active string
		running            bool
		want               bool
	}{
		{"placed and down", "loaded", "inactive", true, true},
		{"failed", "loaded", "failed", true, true},
		{"running", "loaded", "active", true, false},
		{"service false", "not-found", "inactive", true, false},
		{"container stopped", "loaded", "inactive", false, false},
	} {
		r := newV2Rig(t)
		r.load, r.active, r.running = tc.load, tc.active, tc.running
		if got := r.s.Offers(t.Context(), v2Row()); got != tc.want {
			t.Errorf("%s: offered %v", tc.name, got)
		}
	}
	r := newV2Rig(t)
	other := v2Row()
	other.Handle = "proj.7+desk.lxc"
	if r.s.Offers(t.Context(), other) {
		t.Error("a seat no LXC record names is offered")
	}
}

// What cannot be asked without a password keeps the line, so choosing it says why.
func TestAV2SeatsStartIsKeptWhenRootCannotBeAsked(t *testing.T) {
	r := newV2Rig(t)
	r.OnErr("sudo -n lxc-ls", "sudo: a password is required\n", 1)
	if !r.s.Offers(t.Context(), v2Row()) {
		t.Fatal("the line is dropped when root's LXC could not be asked")
	}
}

// TC16: the start is teamchat-admin lxc start --member <container>/<seat key> as root (SU4,
// systemd's start, which leaves a running unit as it is; never lxc restart), and it says what it
// ran and how to attach.
func TestAV2SeatsStartRunsTeamchatAdminLxcStart(t *testing.T) {
	r := newV2Rig(t)
	if _, err := r.s.Start(t.Context(), v2Row()); err != nil {
		t.Fatal(err)
	}
	want := "sudo -n " + paths.AdminBinaryPath + " lxc start --member " + v2Target(t)
	if !r.Fake.Called(want) {
		t.Fatalf("did not run %q: %v", want, r.Fake.Calls())
	}
	r.errHas("Ran sudo "+paths.AdminBinaryPath+" lxc start --member "+v2Target(t), "tmux -S ")
	if r.Fake.Called("sudo -n " + paths.AdminBinaryPath + " lxc restart") {
		t.Fatal("the start ran lxc restart, which ends a session that runs")
	}
}

// A session already running is said so and nothing is run; the start itself would leave it
// running anyway (SU4), so this check is a courtesy, not the guarantee.
func TestAV2SeatsStartRefusesARunningSession(t *testing.T) {
	r := newV2Rig(t)
	r.active = "active"
	if _, err := r.s.Start(t.Context(), v2Row()); err != nil {
		t.Fatal(err)
	}
	if r.Fake.Called("sudo -n " + paths.AdminBinaryPath) {
		t.Fatal("a running session was restarted")
	}
	r.errHas("Nothing was started: its session already runs")
}

// A refusal of teamchat-admin is said with its own words.
func TestAV2SeatsStartSaysTeamchatAdminsRefusal(t *testing.T) {
	r := newV2Rig(t)
	r.OnErr("sudo -n "+paths.AdminBinaryPath+" lxc start", "lxc start: the guest's systemd is unreachable\n", 69)
	if _, err := r.s.Start(t.Context(), v2Row()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ErrOut.String(), "the guest's systemd is unreachable") {
		t.Fatalf("stderr:\n%s", r.ErrOut.String())
	}
}

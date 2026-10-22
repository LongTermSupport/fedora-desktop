package units

import (
	"errors"
	"maps"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func alphaTeam(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func stateProps(over map[string]any) map[string]any {
	p := map[string]any{
		unitIface + ".LoadState":    "loaded",
		unitIface + ".ActiveState":  "active",
		unitIface + ".InvocationID": []byte{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 0xff},
		serviceIface + ".NRestarts": uint32(2),
		serviceIface + ".Result":    "success",
	}
	maps.Copy(p, over)
	return p
}

// SD10: the state read gives the load and active states, the invocation as hex, the restarts
// and the result.
func TestState(t *testing.T) {
	r := newRig(t)
	r.propertyAnswers(stateProps(nil))
	st, err := r.m.State(t.Context(), testUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	want := State{Load: "loaded", Active: "active", InvocationID: "deadbeef000102030405060708090aff", NRestarts: 2, Result: "success"}
	if st != want {
		t.Fatalf("state = %+v, want %+v", st, want)
	}
}

// SD10: a unit with no invocation (never started, or collected) has an empty id.
func TestStateOfAStoppedUnit(t *testing.T) {
	r := newRig(t)
	r.propertyAnswers(stateProps(map[string]any{
		unitIface + ".ActiveState": "failed", unitIface + ".InvocationID": []byte{},
		serviceIface + ".Result": "exit-code",
	}))
	st, err := r.m.State(t.Context(), testUnit(t))
	if err != nil || st.Active != "failed" || st.InvocationID != "" || st.Result != "exit-code" {
		t.Fatalf("state = %+v, %v", st, err)
	}
}

// SD10: a unit systemd cannot load is not loaded and inactive, with nothing else read.
func TestStateOfAnUnloadableUnit(t *testing.T) {
	r := newRig(t)
	r.conn.on(managerLoadUnit, func(fakeCall) ([]any, error) {
		return nil, dbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit"}
	})
	st, err := r.m.State(t.Context(), testUnit(t))
	if err != nil || st != (State{Load: "not-found", Active: "inactive"}) {
		t.Fatalf("state = %+v, %v", st, err)
	}
}

// SD10: each field is decoded strictly.
func TestStateWrongValuesAreErrors(t *testing.T) {
	cases := map[string]map[string]any{
		"load type":    {unitIface + ".LoadState": uint32(1)},
		"load token":   {unitIface + ".LoadState": "Loaded!"},
		"active":       {unitIface + ".ActiveState": int32(1)},
		"invocation":   {unitIface + ".InvocationID": "deadbeef"},
		"short id":     {unitIface + ".InvocationID": []byte{1, 2, 3}},
		"restarts":     {serviceIface + ".NRestarts": int64(3)},
		"result type":  {serviceIface + ".Result": []byte("success")},
		"result token": {serviceIface + ".Result": "SUCCESS"},
		"no result":    {serviceIface + ".Result": nil},
		"no restarts":  {serviceIface + ".NRestarts": nil},
	}
	for name, over := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			props := stateProps(nil)
			for k, v := range over {
				if v == nil {
					delete(props, k)
				} else {
					props[k] = v
				}
			}
			r.propertyAnswers(props)
			if _, err := r.m.State(t.Context(), testUnit(t)); err == nil {
				t.Fatal("no error")
			}
		})
	}
	t.Run("dial", func(t *testing.T) {
		r := newRig(t)
		r.dialErr = errors.New("no bus")
		if _, err := r.m.State(t.Context(), testUnit(t)); !errors.Is(err, ErrUnreachable) {
			t.Fatalf("err = %v", err)
		}
	})
}

// SD10: Kill is KillUnit(unit, whom, signal) on the manager, with nothing to wait for.
func TestKillIsKillUnit(t *testing.T) {
	r := newRig(t)
	r.conn.on(managerKillUnit, func(fakeCall) ([]any, error) { return nil, nil })
	if err := r.m.Kill(t.Context(), testUnit(t), "main", 12); err != nil {
		t.Fatal(err)
	}
	c := r.conn.call(t, managerKillUnit)
	if c.dest != destination || c.path != managerPath || len(c.args) != 3 || c.args[0] != testUnit(t) || c.args[1] != "main" || c.args[2] != int32(12) {
		t.Fatalf("KillUnit = %+v", c)
	}
}

// SD10: Kill refuses a whom systemd does not name and a signal out of range before any call,
// and its refusal by systemd is classified.
func TestKillRefusals(t *testing.T) {
	for _, c := range []struct {
		whom   string
		signal int32
	}{{"", 12}, {"everyone", 12}, {"main", 0}, {"main", -1}, {"main", 65}} {
		r := newRig(t)
		if err := r.m.Kill(t.Context(), testUnit(t), c.whom, c.signal); err == nil || r.dials != 0 {
			t.Errorf("Kill(%q, %d) = %v after %d dials", c.whom, c.signal, err, r.dials)
		}
	}
	r := newRig(t)
	if err := r.m.Kill(t.Context(), "sshd.service", "main", 12); !errors.Is(err, ErrUnitName) || r.dials != 0 {
		t.Fatalf("Kill of another unit = %v", err)
	}
	r.conn.on(managerKillUnit, func(fakeCall) ([]any, error) {
		return nil, dbus.Error{Name: "org.freedesktop.DBus.Error.AccessDenied"}
	})
	if err := r.m.Kill(t.Context(), testUnit(t), "all", 15); !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("Kill refused = %v", err)
	}
}

// SD8 extended: Start and Stop on the Manager are the host's jobs.
func TestStartAndStopOnTheManager(t *testing.T) {
	r := newRig(t)
	unit := paths.RestoreUnit(alphaTeam(t))
	r.jobAnswers(managerStopUnit, unit, jobDone, false)
	if err := r.m.Stop(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	r.jobAnswers(managerStartUnit, unit, jobDone, false)
	if err := r.m.Start(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
}

// SD10: checkUnit accepts the server and the two restore units, and nothing else.
func TestTheRestoreUnitsAreAccepted(t *testing.T) {
	team := alphaTeam(t)
	for _, unit := range []string{paths.ServerUnit(team), paths.RestoreUnit(team), paths.RestoreCtlUnit(team)} {
		if err := checkUnit(unit); err != nil {
			t.Errorf("%s: %v", unit, err)
		}
	}
	for _, unit := range []string{
		"teamchat-restore@.service", "teamchat-restore-ctl@.service", "teamchat-restore@a.socket",
		"teamchat-restorex@a.service", "teamchat-restore-ctlx@a.service", "teamchat-restore@a b.service",
		paths.FenceUnit(team), paths.HealthService, paths.BackupService,
	} {
		if err := checkUnit(unit); !errors.Is(err, ErrUnitName) {
			t.Errorf("%s accepted: %v", unit, err)
		}
	}
}

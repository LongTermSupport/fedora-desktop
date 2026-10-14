package systemd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestRebootRunsSystemctlRebootStreamingToTheGivenWriter(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl", "reboot"}, execx.Result{})
	var sink strings.Builder
	if err := (&Systemd{Runner: f}).Reboot(context.Background(), &sink); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Line() != "systemctl reboot" || calls[0].Stdout != &sink || calls[0].Stderr != &sink {
		t.Fatalf("calls %v", calls)
	}
}

func TestRebootReturnsTheRunnersErrorUnwrappedForItsExitCode(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl", "reboot"}, execx.Result{ExitCode: 4})
	err := (&Systemd{Runner: f}).Reboot(context.Background(), &strings.Builder{})
	if exit, ok := errors.AsType[*execx.ExitError](err); !ok || exit.Code != 4 {
		t.Fatalf("got %v", err)
	}
}

const listPrefix = "systemctl list-units --all --plain --no-legend --type=service agent-bus-hs@*"

func TestHomeserverUnitsParsesListUnits(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl", "list-units"}, execx.Result{Stdout: []byte(
		"agent-bus-hs@alpha.service loaded active running Agent bus homeserver alpha\n" +
			"\n" +
			"agent-bus-hs@beta.service loaded inactive dead Agent bus homeserver beta\n" +
			"other.service loaded active running Not ours\n")})
	units, err := (&Systemd{Runner: f}).HomeserverUnits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Unit{
		{Name: "agent-bus-hs@alpha.service", Team: "alpha", Load: "loaded", Active: "active", Sub: "running", Description: "Agent bus homeserver alpha"},
		{Name: "agent-bus-hs@beta.service", Team: "beta", Load: "loaded", Active: "inactive", Sub: "dead", Description: "Agent bus homeserver beta"},
	}
	if !slices.Equal(units, want) {
		t.Errorf("got %+v", units)
	}
	if !f.Called(listPrefix) {
		t.Errorf("argv: %v", f.Calls())
	}
}

func TestHomeserverUnitsShortLineKeepsTeam(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl"}, execx.Result{Stdout: []byte("agent-bus-hs@alpha.service\n")})
	units, err := (&Systemd{Runner: f}).HomeserverUnits(context.Background())
	if err != nil || len(units) != 1 || units[0].Team != "alpha" {
		t.Fatalf("units %+v err %v", units, err)
	}
}

func TestHomeserverUnitsFailure(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl"}, execx.Result{ExitCode: 1})
	if _, err := (&Systemd{Runner: f}).HomeserverUnits(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestHomeserverStateUp(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl", "is-active", "agent-bus-hs@alpha.service"}, execx.Result{Stdout: []byte("active\n")})
	state, err := (&Systemd{Runner: f}).HomeserverState(context.Background(), "alpha")
	if err != nil || state != "up" {
		t.Fatalf("state %q err %v", state, err)
	}
}

func TestHomeserverStateDownNamesSystemdsWord(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl", "is-active"}, execx.Result{Stdout: []byte("inactive\n"), ExitCode: 3})
	state, err := (&Systemd{Runner: f}).HomeserverState(context.Background(), "alpha")
	if err != nil || state != "down (inactive)" {
		t.Fatalf("state %q err %v", state, err)
	}
}

func TestHomeserverStateUnknownWhenSilent(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemctl", "is-active"}, execx.Result{ExitCode: 4})
	state, err := (&Systemd{Runner: f}).HomeserverState(context.Background(), "alpha")
	if err != nil || state != "down (unknown)" {
		t.Fatalf("state %q err %v", state, err)
	}
}

func TestHomeserverStateCouldNotRun(t *testing.T) {
	if _, err := (&Systemd{Runner: &execx.Fake{}}).HomeserverState(context.Background(), "alpha"); !errors.Is(err, execx.ErrUnexpected) {
		t.Fatalf("got %v", err)
	}
}

func TestElementLogPath(t *testing.T) {
	if got := ElementLogPath("", "/home/u", "alpha"); got != "/home/u/.local/state/ccy-teams/element-alpha.log" {
		t.Errorf("default: %q", got)
	}
	if got := ElementLogPath("/state", "/home/u", "alpha"); got != "/state/ccy-teams/element-alpha.log" {
		t.Errorf("XDG: %q", got)
	}
}

func launchSpec(t *testing.T) Launch {
	t.Helper()
	return Launch{Team: "alpha", Desktop: "/home/u/.local/share/applications/agent-bus-alpha-element.desktop", PID: 42, Log: filepath.Join(t.TempDir(), "state", "element-alpha.log")}
}

func TestLaunchElementArgvAndLog(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemd-run"}, execx.Result{Stdout: []byte("electron noise\n")})
	l := launchSpec(t)
	if err := (&Systemd{Runner: f}).LaunchElement(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	want := "systemd-run --user --scope --quiet --collect --unit ccy-teams-element-alpha-42 " +
		"--description Element for agent team alpha -- setsid -w gio launch " + l.Desktop
	c := f.Calls()[0]
	if c.Line() != want {
		t.Errorf("got  %q\nwant %q", c.Line(), want)
	}
	if c.Stdin != nil {
		t.Error("stdin must be empty")
	}
	data, err := os.ReadFile(l.Log)
	if err != nil || string(data) != "electron noise\n" {
		t.Errorf("log %q err %v", data, err)
	}
}

func TestLaunchElementFailureReturnsLogTail(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemd-run"}, execx.Result{Stdout: []byte("1\n2\n3\n4\n5\n6\n7\n"), ExitCode: 1})
	l := launchSpec(t)
	err := (&Systemd{Runner: f}).LaunchElement(context.Background(), l)
	var le *LaunchError
	if !errors.As(err, &le) {
		t.Fatalf("got %v", err)
	}
	if le.Log != l.Log || le.Tail != "3\n4\n5\n6\n7" {
		t.Errorf("log %q tail %q", le.Log, le.Tail)
	}
	if !strings.Contains(le.Error(), l.Log) {
		t.Errorf("message %q", le.Error())
	}
}

// logSwapper stands in for a launcher that leaves a directory where the log was.
type logSwapper struct{ log string }

func (s logSwapper) Run(context.Context, execx.Cmd) (execx.Result, error) {
	if err := os.Remove(s.log); err != nil {
		return execx.Result{}, err
	}
	if err := os.Mkdir(s.log, 0o750); err != nil {
		return execx.Result{}, err
	}
	return execx.Result{}, &execx.ExitError{Code: 1}
}

func TestLaunchElementUnreadableLogSurfacesTheReason(t *testing.T) {
	l := launchSpec(t)
	err := (&Systemd{Runner: logSwapper{log: l.Log}}).LaunchElement(context.Background(), l)
	var le *LaunchError
	if !errors.As(err, &le) {
		t.Fatalf("got %v", err)
	}
	if le.Tail != "" || le.TailErr == nil {
		t.Fatalf("tail %q tailErr %v", le.Tail, le.TailErr)
	}
	if !strings.Contains(le.Error(), "could not be read") || !strings.Contains(le.Error(), "directory") {
		t.Errorf("message %q", le.Error())
	}
}

func TestLaunchElementAppendsToExistingLog(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"systemd-run"}, execx.Result{Stdout: []byte("new\n")})
	l := launchSpec(t)
	if err := os.MkdirAll(filepath.Dir(l.Log), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Log, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Systemd{Runner: f}).LaunchElement(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.Log)
	if err != nil || string(data) != "old\nnew\n" {
		t.Errorf("log %q err %v", data, err)
	}
}

func TestLaunchElementCannotCreateLogDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := Launch{Team: "alpha", Desktop: "d", PID: 1, Log: filepath.Join(blocker, "sub", "x.log")}
	if err := (&Systemd{Runner: &execx.Fake{}}).LaunchElement(context.Background(), l); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLaunchElementCouldNotStartSystemdRun(t *testing.T) {
	err := (&Systemd{Runner: &execx.Fake{}}).LaunchElement(context.Background(), launchSpec(t))
	var le *LaunchError
	if !errors.As(err, &le) || !errors.Is(err, execx.ErrUnexpected) {
		t.Fatalf("got %v", err)
	}
}

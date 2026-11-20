package lxcfreeze

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// A tmux command run inside a container goes through sudo lxc-attach, as the container's user,
// with a cleared environment, and its output and exit come back as tmux's own.
func TestInContainerRunsTmuxAsTheUserThroughLxcAttach(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, execx.Result{Stdout: []byte("pane\n")})
	r := newLister(f).InContainer("/srv/lxc", "box", "agent")
	res, err := r.Run(context.Background(), execx.Cmd{Name: "tmux", Args: []string{"-L", "agent-bus-claude", "capture-pane", "-p"}})
	if err != nil || string(res.Stdout) != "pane\n" {
		t.Fatalf("res %q err %v", res.Stdout, err)
	}
	want := []string{
		"sudo", "-n", "lxc-attach", "-P", "/srv/lxc", "-n", "box", "--clear-env", "--",
		"runuser", "-u", "agent", "--", "env", "-i", "PATH=" + guestPath,
		"tmux", "-L", "agent-bus-claude", "capture-pane", "-p",
	}
	calls := f.Calls()
	if len(calls) != 1 || !slices.Equal(append([]string{calls[0].Name}, calls[0].Args...), want) {
		t.Fatalf("ran %v", calls)
	}
}

func TestInContainerKeepsTmuxsRefusal(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, exit1("no server running on /tmp/tmux-1000/agent-bus-claude\n"))
	_, err := newLister(f).InContainer("/srv/lxc", "box", "agent").Run(context.Background(), execx.Cmd{Name: "tmux", Args: []string{"list-sessions"}})
	exitErr, ok := errors.AsType[*execx.ExitError](err)
	if !ok || !strings.Contains(exitErr.Stderr, "no server running") {
		t.Fatalf("err %v", err)
	}
}

// Only tmux runs inside a container this way.
func TestInContainerRefusesAnyOtherProgram(t *testing.T) {
	f := &execx.Fake{}
	_, err := newLister(f).InContainer("/srv/lxc", "box", "agent").Run(context.Background(), execx.Cmd{Name: "sh", Args: []string{"-c", "true"}})
	if err == nil || len(f.Calls()) != 0 {
		t.Fatalf("err %v calls %v", err, f.Calls())
	}
}

// A session unit is started inside a container as root through sudo lxc-attach, with a cleared
// environment: a plain systemctl start, which needs the unit loaded, not enabled.
func TestStartUnitStartsTheUnitAsRootThroughLxcAttach(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, execx.Result{})
	if err := newLister(f).StartUnit(context.Background(), "/srv/lxc", "box", "agent-bus-claude-agent.service"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"sudo", "-n", "lxc-attach", "-P", "/srv/lxc", "-n", "box", "--clear-env", "--",
		"env", "-i", "PATH=" + guestPath, "systemctl", "start", "--", "agent-bus-claude-agent.service",
	}
	calls := f.Calls()
	if len(calls) != 1 || !slices.Equal(append([]string{calls[0].Name}, calls[0].Args...), want) {
		t.Fatalf("ran %v", calls)
	}
}

func TestStartUnitKeepsSystemdsRefusal(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, exit1("Unit agent-bus-claude-agent.service not found.\n"))
	err := newLister(f).StartUnit(context.Background(), "/srv/lxc", "box", "agent-bus-claude-agent.service")
	if err == nil || !strings.Contains(err.Error(), "Unit agent-bus-claude-agent.service not found.") {
		t.Fatalf("err %v", err)
	}
}

// A unit's load and active states inside a container are asked of systemd as root through sudo
// lxc-attach: what a v2 LXC seat's start is offered by (its unit file placed, its session down).
func TestUnitStateReadsLoadAndActiveStateAsRoot(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, execx.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")})
	load, active, err := newLister(f).UnitState(context.Background(), "/srv/lxc", "box", "teamchat-lxc-x.service")
	if err != nil || load != "loaded" || active != "inactive" {
		t.Fatalf("%q %q %v", load, active, err)
	}
	want := []string{
		"sudo", "-n", "lxc-attach", "-P", "/srv/lxc", "-n", "box", "--clear-env", "--",
		"env", "-i", "PATH=" + guestPath, "systemctl", "show", "--property=LoadState,ActiveState", "--", "teamchat-lxc-x.service",
	}
	calls := f.Calls()
	if len(calls) != 1 || !slices.Equal(append([]string{calls[0].Name}, calls[0].Args...), want) {
		t.Fatalf("ran %v", calls)
	}
}

func TestUnitStateFailsOnAnAnswerItCannotRead(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, execx.Result{Stdout: []byte("LoadState=loaded\n")})
	if _, _, err := newLister(f).UnitState(context.Background(), "/srv/lxc", "box", "u.service"); err == nil {
		t.Fatal("an answer with no ActiveState was read")
	}
	f = &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, exit1("lxc-attach: failed to get init pid\n"))
	if _, _, err := newLister(f).UnitState(context.Background(), "/srv/lxc", "box", "u.service"); err == nil ||
		!strings.Contains(err.Error(), "failed to get init pid") {
		t.Fatalf("err %v", err)
	}
}

// Every unit of a container is asked in one systemctl show; its blocks come back in argument
// order, and an answer with another number of blocks is refused.
func TestUnitStatesAsksEveryUnitInOneCall(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, execx.Result{Stdout: []byte(
		"LoadState=loaded\nActiveState=inactive\n\nLoadState=not-found\nActiveState=inactive\n")})
	got, err := newLister(f).UnitStates(context.Background(), "/srv/lxc", "box", "a.service", "b.service")
	if err != nil || len(got) != 2 || got[0] != (UnitStatus{Load: "loaded", Active: "inactive"}) ||
		got[1] != (UnitStatus{Load: "not-found", Active: "inactive"}) {
		t.Fatalf("%v %v", got, err)
	}
	calls := f.Calls()
	if len(calls) != 1 || !slices.Equal(calls[0].Args[len(calls[0].Args)-3:], []string{"--", "a.service", "b.service"}) {
		t.Fatalf("ran %v", calls)
	}
	f = &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach"}, execx.Result{Stdout: []byte("LoadState=loaded\nActiveState=inactive\n")})
	if _, err = newLister(f).UnitStates(context.Background(), "/srv/lxc", "box", "a.service", "b.service"); err == nil {
		t.Fatal("one block read for two units")
	}
}

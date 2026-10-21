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

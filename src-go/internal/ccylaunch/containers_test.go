package ccylaunch

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// containersRun is the front run with args on podman, every podman call answered by on.
func containersRun(t *testing.T, on func(r *run), args ...string) *run {
	t.Helper()
	r := newRun(install(t), args...)
	on(r)
	return r
}

func noRules(*run) {}

func TestListContainersPrintsTheNamesOnePerLine(t *testing.T) {
	r := containersRun(t, func(r *run) {
		r.runner.On([]string{"podman", "ps"}, execx.Result{Stdout: []byte("a_yolo\nb_yolo\n")})
	}, listContainersFlag, "podman", "--all", "name=_yolo", "status=exited", "status=created")
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "a_yolo\nb_yolo\n" || r.stderr.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	want := []string{"ps", "-a", "--filter", "name=_yolo", "--filter", "status=exited", "--filter", "status=created", "--format", "{{.Names}}"}
	if c := r.runner.Calls()[0]; !slices.Equal(c.Args, want) {
		t.Fatalf("ran %q, want %q", c.Args, want)
	}
}

func TestListContainersOfNoneIsEmptyAndExitZero(t *testing.T) {
	r := containersRun(t, func(r *run) {
		r.runner.On([]string{"podman", "ps"}, execx.Result{})
	}, listContainersFlag, "podman", "name=_yolo")
	if code := r.start(t); code != term.ExitOK || r.stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, r.stdout)
	}
	if c := r.runner.Calls()[0]; slices.Contains(c.Args, "-a") {
		t.Fatalf("ran %q: without --all only the running ones are asked for", c.Args)
	}
}

// An engine that cannot list is a failure with its cause, never "no containers".
func TestListContainersThatFailIsNeverNoContainers(t *testing.T) {
	cases := []struct {
		name string
		res  execx.Result
		err  error
		word string
	}{
		{"engine refused", execx.Result{ExitCode: 125, Stderr: []byte("Error: database is locked\n")}, nil, "database is locked"},
		{"engine did not start", execx.Result{}, errors.New("exec: podman: not found"), "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := containersRun(t, func(r *run) {
				r.runner.OnFunc([]string{"podman", "ps"}, func(execx.Cmd) (execx.Result, error) { return tc.res, tc.err })
			}, listContainersFlag, "podman", "name=_yolo")
			said(t, r, term.ExitFailure, "could not be listed", tc.word)
		})
	}
}

// stopRun is --stop-containers podman with names, each stop and exists answered by the rules.
func stopRun(t *testing.T, on func(r *run), names ...string) *run {
	t.Helper()
	return containersRun(t, on, append([]string{stopContainersFlag, "podman"}, names...)...)
}

func TestStoppedContainersAreTickedAndExitZero(t *testing.T) {
	r := stopRun(t, func(r *run) {
		r.runner.On([]string{"podman", "stop"}, execx.Result{})
	}, "a_yolo", "b_yolo")
	said(t, r, term.ExitOK, "Stopping a_yolo... ✓", "Stopping b_yolo... ✓")
	if n := len(r.runner.Calls()); n != 2 {
		t.Fatalf("ran %v, want only the two stops", r.runner.Calls())
	}
}

// A stop that failed of a container the store no longer has is "already gone": asked of the
// engine, not read from the stop's words.
func TestAContainerTheStoreNoLongerHasIsAlreadyGone(t *testing.T) {
	r := stopRun(t, func(r *run) {
		r.runner.On([]string{"podman", "stop"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: whatever\n")})
		r.runner.On([]string{"podman", "container", "exists", "--external", "a_yolo"}, execx.Result{ExitCode: 1})
	}, "a_yolo")
	said(t, r, term.ExitOK, "Stopping a_yolo... already gone")
}

// A stop that failed of a container still there is a failure with the engine's words; every
// container is still tried.
func TestAStopThatFailedIsAFailureWithItsCause(t *testing.T) {
	r := stopRun(t, func(r *run) {
		r.runner.On([]string{"podman", "stop"}, execx.Result{})
		r.runner.On([]string{"podman", "stop", "a_yolo"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: permission denied\n")})
		r.runner.On([]string{"podman", "container", "exists"}, execx.Result{})
	}, "a_yolo", "b_yolo")
	said(t, r, term.ExitFailure, "Stopping a_yolo... ✗", "permission denied", "Stopping b_yolo... ✓")
	if strings.Contains(r.stderr.String(), "already") {
		t.Fatalf("stderr %q guesses it was already stopped", r.stderr)
	}
}

// A store that cannot be asked is a failure naming both causes, never "already gone".
func TestAStopWhoseStoreCannotBeAskedIsAFailure(t *testing.T) {
	r := stopRun(t, func(r *run) {
		r.runner.On([]string{"podman", "stop"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: timeout\n")})
		r.runner.On([]string{"podman", "container", "exists"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: database is locked\n")})
	}, "a_yolo")
	said(t, r, term.ExitFailure, "✗", "timeout", "database is locked")
}

// removeRun is --remove-network docker net, its rm and the list answered by the rules.
func removeRun(t *testing.T, on func(r *run)) *run {
	t.Helper()
	return containersRun(t, on, removeNetworkFlag, "docker", "net")
}

func TestARemovedNetworkIsExitZeroAndSilent(t *testing.T) {
	r := removeRun(t, func(r *run) {
		r.runner.On([]string{"docker", "network", "rm", "net"}, execx.Result{Stdout: []byte("net\n")})
	})
	said(t, r, term.ExitOK)
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr %q", r.stderr)
	}
}

func TestANetworkTheEngineNoLongerHasIsGone(t *testing.T) {
	r := removeRun(t, func(r *run) {
		r.runner.On([]string{"docker", "network", "rm"}, execx.Result{ExitCode: 1, Stderr: []byte("Error: no such network\n")})
		r.runner.On([]string{"docker", "network", "ls"}, execx.Result{Stdout: []byte("net_other\n")})
	})
	said(t, r, ExitGone, "net", "not there any more")
}

// A network still there after a failed rm is a failure with the engine's words, never a guess
// that it "may still have containers".
func TestANetworkThatCouldNotBeRemovedSaysWhy(t *testing.T) {
	for name, ls := range map[string]execx.Result{
		"still there":         {Stdout: []byte("net\n")},
		"list cannot be read": {ExitCode: 1, Stderr: []byte("Cannot connect to the Docker daemon\n")},
	} {
		t.Run(name, func(t *testing.T) {
			r := removeRun(t, func(r *run) {
				r.runner.On([]string{"docker", "network", "rm"}, execx.Result{ExitCode: 1, Stderr: []byte("Error: network net has active endpoints\n")})
				r.runner.On([]string{"docker", "network", "ls"}, ls)
			})
			said(t, r, term.ExitFailure, "net could not be removed", "active endpoints")
			if strings.Contains(r.stderr.String(), "may still") {
				t.Fatalf("stderr %q guesses", r.stderr)
			}
		})
	}
}

func TestContainerFlagsRefuseWhatTheyCannotRun(t *testing.T) {
	for _, args := range [][]string{
		{listContainersFlag},
		{stopContainersFlag, "podman"},
		{removeNetworkFlag, "docker"},
		{removeNetworkFlag, "docker", "a", "b"},
	} {
		r := containersRun(t, noRules, args...)
		if code := r.start(t); code != term.ExitUsage {
			t.Errorf("%q: exit %d, want usage", args, code)
		}
	}
	for _, args := range [][]string{
		{listContainersFlag, "lxc"},
		{stopContainersFlag, "lxc", "a"},
		{removeNetworkFlag, "lxc", "net"},
	} {
		r := containersRun(t, noRules, args...)
		if code := r.start(t); code != term.ExitFailure || len(r.runner.Calls()) != 0 {
			t.Errorf("%q: exit %d after %v, want a failure that runs nothing", args, code, r.runner.Calls())
		}
	}
}

func TestContainerFlagsAreAnsweredUnderLegacy(t *testing.T) {
	r := stopRun(t, func(r *run) {
		r.runner.On([]string{"podman", "stop"}, execx.Result{})
	}, "a_yolo").env(LegacyEnv + "=1")
	said(t, r, term.ExitOK, "✓")
	r = containersRun(t, func(r *run) {
		r.runner.On([]string{"podman", "ps"}, execx.Result{Stdout: []byte("a_yolo\n")})
	}, listContainersFlag, "podman").env(LegacyEnv + "=1")
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "a_yolo\n" {
		t.Fatalf("list under legacy: exit %d, stdout %q", code, r.stdout)
	}
	r = removeRun(t, func(r *run) {
		r.runner.On([]string{"docker", "network", "rm"}, execx.Result{})
	}).env(LegacyEnv + "=1")
	said(t, r, term.ExitOK)
}

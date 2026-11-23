package ccylaunch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The launcher settles the limits as soon as it has loaded its libraries, before --version and
// --help, so the front's refusals (F4) are the bash's at every answered flag.
func TestTheLauncherSettlesTheLimitsFirst(t *testing.T) {
	launcher := checkoutFile(t, launcherRel)
	if !strings.Contains(launcher, "\nsource \"$SCRIPT_DIR/lib/container-limits.bash\"\nccy_container_limits || exit 1\n") {
		t.Error("the launcher no longer settles the limits as soon as it is loaded, before --version and --help")
	}
}

// withLimits is the start of the argv of a container the front runs under the limit set lim.
func withLimits(lim []string, rest ...string) []string {
	return slices.Concat([]string{"run", "--rm"}, lim, rest)
}

// limitsOf is the limit set with the pids limit and the memory caps given.
func limitsOf(pids, memory, reservation string) []string {
	return []string{
		"--cgroup-parent", "agents.slice", "--pids-limit", pids,
		"--memory", memory, "--memory-reservation", reservation,
	}
}

// refusedLimits are environments whose limits the library refuses.
var refusedLimits = []string{
	"CCY_PIDS_LIMIT=0", "CCY_PIDS_LIMIT=-1", "CCY_PIDS_LIMIT=max", "CCY_PIDS_LIMIT=00", "CCY_PIDS_LIMIT=255", "CCY_PIDS_LIMIT=32769",
	"CCY_MEMORY_MAX=0", "CCY_MEMORY_MAX=-1", "CCY_MEMORY_MAX=max", "CCY_MEMORY_MAX=3g", "CCY_MEMORY_MAX=65g",
}

// F4 and F8: limits the library refuses hand every answered flag over (the bash refuses them,
// exit 1); ones it accepts are claude --help's container's.
func TestTheLimits(t *testing.T) {
	dir := install(t)
	for _, kv := range refusedLimits {
		for _, flag := range []string{"--version", "--help"} {
			r := newRun(dir, flag).env(kv)
			r.start(t)
			if len(r.execer.Execs()) != 1 || r.stdout.Len() != 0 {
				t.Fatalf("%s with %s: answered %q, not handed over", flag, kv, r.stdout)
			}
		}
	}
	for kv, want := range map[string][]string{
		"CCY_PIDS_LIMIT=":     defaultLimits,
		"CCY_PIDS_LIMIT=8192": limitsOf("8192", "49152m", "47104m"),
		"CCY_MEMORY_MAX=8g":   limitsOf("16384", "8192m", "6144m"),
	} {
		r := newRun(dir, "--help").env(kv)
		r.runner.On([]string{"podman", "image", "inspect", "claude-yolo:latest"}, execx.Result{})
		r.runner.On([]string{"podman", "run"}, execx.Result{Stdout: []byte("Usage: claude\n")})
		if code := r.start(t); code != term.ExitOK || len(r.execer.Execs()) != 0 {
			t.Fatalf("--help with %s: exit %d, stderr %q", kv, code, r.stderr)
		}
		run := withLimits(want, "--entrypoint", "claude", "claude-yolo:latest", "--help")
		if got := r.runner.Calls()[1].Args; !slices.Equal(got, run) {
			t.Fatalf("--help with %s ran %q, want %q", kv, got, run)
		}
	}
}

// F4: a machine the library refuses (no slice unit, too little RAM, no meminfo) hands over.
func TestTheMachineTheLimitsNeed(t *testing.T) {
	for name, spoil := range map[string]func(dir string) error{
		"no slice": func(dir string) error { return os.Remove(filepath.Join(dir, machineSlice)) },
		"16 GiB": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, machineMeminfo), []byte("MemTotal: 16777216 kB\n"), 0o600)
		},
		"no memory": func(dir string) error { return os.Remove(filepath.Join(dir, machineMeminfo)) },
	} {
		dir := install(t)
		if err := spoil(dir); err != nil {
			t.Fatal(err)
		}
		r := newRun(dir, "--version")
		r.start(t)
		if len(r.execer.Execs()) != 1 || r.stdout.Len() != 0 {
			t.Fatalf("%s: answered %q, not handed over", name, r.stdout)
		}
	}
}

// E14 and F17-F19: the containers the bash launcher asks the front to run (the token check,
// the network check, the version probe) are started with the settled limit set; limits the
// library refuses run nothing and are exit 1, naming the variable.
func TestTheLauncherChecksRunUnderTheLimits(t *testing.T) {
	checks := map[string]func(t *testing.T) *run{
		checkTokenFlag: func(t *testing.T) *run {
			t.Helper()
			return checkRun(t, checkValue).answer(execx.Result{Stdout: []byte("200")}, nil)
		},
		checkNetworkFlag: func(t *testing.T) *run {
			t.Helper()
			return netRun(t, execx.Result{}, nil)
		},
		claudeVersionsFlag: func(t *testing.T) *run {
			t.Helper()
			return versionsRun(t, imageSays, registrySays, nil)
		},
	}
	for flag, start := range checks {
		for kv, want := range map[string][]string{
			"CCY_PIDS_LIMIT=":     defaultLimits,
			"CCY_PIDS_LIMIT=8192": limitsOf("8192", "49152m", "47104m"),
			"CCY_MEMORY_MAX=8g":   limitsOf("16384", "8192m", "6144m"),
		} {
			r := start(t).env(kv)
			if code := r.start(t); code != term.ExitOK {
				t.Fatalf("%s with %s: exit %d, stderr %q", flag, kv, code, r.stderr)
			}
			args := r.runner.Calls()[0].Args
			if len(args) < 2+len(want) || !slices.Equal(args[:2+len(want)], withLimits(want)) {
				t.Fatalf("%s with %s ran %q, want the limit set %q", flag, kv, args, want)
			}
		}
		for _, kv := range refusedLimits {
			r := start(t).env(kv)
			name := strings.SplitN(kv, "=", 2)[0]
			if code := r.start(t); code != term.ExitFailure || len(r.runner.Calls()) != 0 || !strings.Contains(r.stderr.String(), name) {
				t.Fatalf("%s with %s: exit %d, ran %d, stderr %q", flag, kv, code, len(r.runner.Calls()), r.stderr)
			}
		}
	}
}

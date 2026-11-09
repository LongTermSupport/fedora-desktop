package ccylaunch

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The front's bounds and form are lib/container-limits.bash's: a value the library refused and
// the front accepted would be answered where the bash exits 1.
func TestPidsLimitIsTheLibrarys(t *testing.T) {
	text := checkoutFile(t, "files/var/local/claude-yolo/lib/container-limits.bash")
	for name, want := range map[string]int{
		"CCY_PIDS_LIMIT_DEFAULT": pidsLimitDefault, "CCY_PIDS_LIMIT_MIN": pidsLimitMin, "CCY_PIDS_LIMIT_MAX": pidsLimitMax,
	} {
		m := regexp.MustCompile(`(?m)^` + name + `=([0-9]+)$`).FindStringSubmatch(text)
		if m == nil || m[1] != strconv.Itoa(want) {
			t.Errorf("the library's %s is %v; the front has %d", name, m, want)
		}
	}
	if !strings.Contains(text, `CCY_PIDS_LIMIT="${CCY_PIDS_LIMIT:-$CCY_PIDS_LIMIT_DEFAULT}"`) {
		t.Error("the library no longer settles an unset or empty CCY_PIDS_LIMIT to its default")
	}
	if !strings.Contains(text, `=~ `+pidsLimitForm.String()+` ]]`) {
		t.Errorf("the library's form is not %s", pidsLimitForm)
	}
	launcher := checkoutFile(t, launcherRel)
	if !strings.Contains(launcher, "\nsource \"$SCRIPT_DIR/lib/container-limits.bash\"\nccy_container_limits || exit 1\n") {
		t.Error("the launcher no longer settles the limit as soon as it is loaded, before --version and --help")
	}
}

func TestPidsLimit(t *testing.T) {
	for value, want := range map[string]string{
		"": "4096", "256": "256", "4096": "4096", "8192": "8192", "32768": "32768",
		"0": "", "00": "", "-1": "", "max": "", "255": "", "32769": "", "99999": "", "100000": "",
		"04096": "", "4096 ": "", " 4096": "", "4096x": "", "1e4": "", "+4096": "",
	} {
		got, ok := pidsLimit(value)
		if got != want || ok != (want != "") {
			t.Errorf("pidsLimit(%q) = %q, %v; want %q", value, got, ok, want)
		}
	}
}

// F4 and F8: a limit the library refuses hands every answered flag over (the bash refuses it,
// exit 1); one it accepts is the --pids-limit of claude --help's container.
func TestThePidsLimit(t *testing.T) {
	dir := install(t)
	for _, value := range []string{"0", "-1", "max", "00", "255", "32769"} {
		for _, flag := range []string{"--version", "--help"} {
			r := newRun(dir, flag).env(pidsLimitEnv + "=" + value)
			r.start(t)
			if len(r.execer.Execs()) != 1 || r.stdout.Len() != 0 {
				t.Fatalf("%s with %s=%s: answered %q, not handed over", flag, pidsLimitEnv, value, r.stdout)
			}
		}
	}
	for value, want := range map[string]string{"": "4096", "8192": "8192"} {
		r := newRun(dir, "--help").env(pidsLimitEnv + "=" + value)
		r.runner.On([]string{"podman", "image", "inspect", "claude-yolo:latest"}, execx.Result{})
		r.runner.On([]string{"podman", "run"}, execx.Result{Stdout: []byte("Usage: claude\n")})
		if code := r.start(t); code != term.ExitOK || len(r.execer.Execs()) != 0 {
			t.Fatalf("--help with %s=%q: exit %d, stderr %q", pidsLimitEnv, value, code, r.stderr)
		}
		run := []string{"run", "--rm", "--pids-limit", want, "--entrypoint", "claude", "claude-yolo:latest", "--help"}
		if got := r.runner.Calls()[1].Args; !slices.Equal(got, run) {
			t.Fatalf("--help with %s=%q ran %q, want %q", pidsLimitEnv, value, got, run)
		}
	}
}

// E14 and F17-F19: the containers the bash launcher asks the front to run (the token check,
// the network check, the version probe) are started with the settled --pids-limit; a limit the
// library refuses runs nothing and is exit 1, naming the variable.
func TestTheLauncherChecksRunUnderThePidsLimit(t *testing.T) {
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
		for value, want := range map[string]string{"": "4096", "8192": "8192"} {
			r := start(t).env(pidsLimitEnv + "=" + value)
			if code := r.start(t); code != term.ExitOK {
				t.Fatalf("%s with %s=%q: exit %d, stderr %q", flag, pidsLimitEnv, value, code, r.stderr)
			}
			args := r.runner.Calls()[0].Args
			if i := slices.Index(args, "--pids-limit"); args[0] != "run" || i < 0 || i+1 >= len(args) || args[i+1] != want {
				t.Fatalf("%s with %s=%q ran %q, want --pids-limit %s", flag, pidsLimitEnv, value, args, want)
			}
		}
		for _, value := range []string{"0", "-1", "max", "255", "32769"} {
			r := start(t).env(pidsLimitEnv + "=" + value)
			if code := r.start(t); code != term.ExitFailure || len(r.runner.Calls()) != 0 || !strings.Contains(r.stderr.String(), pidsLimitEnv) {
				t.Fatalf("%s with %s=%s: exit %d, ran %d, stderr %q", flag, pidsLimitEnv, value, code, len(r.runner.Calls()), r.stderr)
			}
		}
	}
}

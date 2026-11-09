package ccylaunch

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// checkValue stands in for a token: the grammar's characters only.
var checkValue = "placeholder-" + strings.Repeat("Ab3_-", 18)

// checkRun is --check-token podman claude-yolo:latest with the token in its environment.
func checkRun(t *testing.T, value string, kv ...string) *run {
	t.Helper()
	r := newRun(install(t), checkTokenFlag, "podman", "claude-yolo:latest")
	return r.env(append([]string{checkEnvName + "=" + value}, kv...)...)
}

// answer makes the container run answer res, or fail to start with err.
func (r *run) answer(res execx.Result, err error) *run {
	r.runner.OnFunc([]string{"podman", "run"}, func(execx.Cmd) (execx.Result, error) { return res, err })
	return r
}

// checked asserts the run's exit code and that its stderr holds each of words; stdout is
// never written.
func checked(t *testing.T, r *run, want term.ExitCode, words ...string) {
	t.Helper()
	if code := r.start(t); code != want {
		t.Fatalf("exit %d, want %d; stderr %q", code, want, r.stderr)
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("stdout %q: the check's answer is its exit code", r.stdout)
	}
	for _, w := range words {
		if !strings.Contains(r.stderr.String(), w) {
			t.Fatalf("stderr %q does not say %q", r.stderr, w)
		}
	}
	if strings.Contains(r.stderr.String(), checkValue) {
		t.Fatal("stderr shows the token")
	}
}

func TestATokenTheAPIAcceptsIsExitZeroAndSilent(t *testing.T) {
	for _, code := range []string{"200", "429"} {
		r := checkRun(t, checkValue).answer(execx.Result{Stdout: []byte(code)}, nil)
		checked(t, r, term.ExitOK)
		if r.stderr.Len() != 0 {
			t.Fatalf("HTTP %s: stderr %q", code, r.stderr)
		}
		c := r.runner.Calls()[0]
		if !slices.Contains(c.Args, "--pull=never") || !slices.Contains(c.Args, checkEnvName) {
			t.Fatalf("ran %q: the token goes by name to an image that is never pulled", c.Args)
		}
		if len(c.Secrets) != 1 || !c.Secrets[0].Is(checkValue) || c.Secrets[0].Name() != checkEnvName {
			t.Fatalf("secrets %v", c.Secrets)
		}
		if !slices.Contains(c.Args, defaultUsageEndpoint) {
			t.Fatalf("ran %q: the check asks the API the usage reader asks", c.Args)
		}
	}
}

func TestATokenTheAPIRefusesIsTheRecoveryExit(t *testing.T) {
	for _, code := range []string{"401", "403"} {
		r := checkRun(t, checkValue).answer(execx.Result{Stdout: []byte(code)}, nil)
		checked(t, r, ExitTokenRejected, "refused the token", "HTTP "+code)
	}
}

func TestATokenNoTokenCouldBeIsRejectedWithoutARun(t *testing.T) {
	r := checkRun(t, `placeholder-a"b`)
	checked(t, r, ExitTokenRejected, "not a token")
	if len(r.runner.Calls()) != 0 {
		t.Fatal("a malformed token was sent")
	}
}

func TestACheckThatCouldNotRunIsNeverAnInvalidToken(t *testing.T) {
	cases := []struct {
		name  string
		res   execx.Result
		err   error
		words []string
	}{
		{"engine did not start", execx.Result{}, errors.New("exec: podman: permission denied"), []string{"permission denied"}},
		{"image missing", execx.Result{ExitCode: 125, Stderr: []byte("Error: claude-yolo:latest: image not known\n")}, nil, []string{"is not built", "claude-yolo:latest"}},
		{"engine failure", execx.Result{ExitCode: 125, Stderr: []byte("Error: cannot connect to podman socket\n")}, nil, []string{"cannot connect to podman socket"}},
		{"network fault", execx.Result{ExitCode: 6, Stderr: []byte("curl: (6) Could not resolve host: api.anthropic.com\n")}, nil, []string{"Could not resolve host"}},
		{"API fault", execx.Result{Stdout: []byte("529")}, nil, []string{"HTTP 529"}},
		{"no answer", execx.Result{Stdout: []byte("000")}, nil, []string{"HTTP 000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := checkRun(t, checkValue).answer(tc.res, tc.err)
			checked(t, r, term.ExitFailure, append([]string{"could not be checked"}, tc.words...)...)
			if strings.Contains(r.stderr.String(), "refused") {
				t.Fatalf("stderr %q reads a failed check as a refused token", r.stderr)
			}
		})
	}
}

func TestACheckWithNothingToCheckWithIsRefused(t *testing.T) {
	r := newRun(install(t), checkTokenFlag, "podman", "img")
	checked(t, r, term.ExitFailure, "could not be checked", checkEnvName)

	r = checkRun(t, checkValue)
	r.front.Args = []string{checkTokenFlag, "lxc", "img"}
	checked(t, r, term.ExitFailure, "could not be checked", "lxc")

	r = checkRun(t, checkValue, "CCY_USAGE_MODEL=a\tb")
	checked(t, r, term.ExitFailure, "could not be checked", "CCY_USAGE_")
	if len(r.runner.Calls()) != 0 {
		t.Fatal("a refused check ran something")
	}
}

func TestCheckTokenArgumentCountIsUsage(t *testing.T) {
	for _, args := range [][]string{{checkTokenFlag}, {checkTokenFlag, "podman"}, {checkTokenFlag, "podman", "img", "x"}} {
		r := newRun(install(t), args...)
		if code := r.start(t); code != term.ExitUsage || !strings.Contains(r.stderr.String(), "usage:") {
			t.Fatalf("%q: exit %d, stderr %q", args, code, r.stderr)
		}
	}
}

func TestCheckTokenIsAnsweredUnderLegacy(t *testing.T) {
	r := checkRun(t, checkValue, LegacyEnv+"=1").answer(execx.Result{Stdout: []byte("200")}, nil)
	checked(t, r, term.ExitOK)
	if len(r.execer.Execs()) != 0 {
		t.Fatal("handed to the bash, which asks the front itself")
	}
}

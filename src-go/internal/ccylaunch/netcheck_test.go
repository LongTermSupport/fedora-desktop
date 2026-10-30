package ccylaunch

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// netRun is --check-network podman appnet, its probe run answering res, or failing to start with err.
func netRun(t *testing.T, res execx.Result, err error) *run {
	t.Helper()
	r := newRun(install(t), checkNetworkFlag, "podman", "appnet")
	r.runner.OnFunc([]string{"podman", "run"}, func(execx.Cmd) (execx.Result, error) { return res, err })
	return r
}

// said asserts the run's exit code, that stdout is empty and that stderr holds each of words.
func said(t *testing.T, r *run, want term.ExitCode, words ...string) {
	t.Helper()
	if code := r.start(t); code != want {
		t.Fatalf("exit %d, want %d; stderr %q", code, want, r.stderr)
	}
	if r.stdout.Len() != 0 {
		t.Fatalf("stdout %q: the answer is the exit code", r.stdout)
	}
	for _, w := range words {
		if !strings.Contains(r.stderr.String(), w) {
			t.Fatalf("stderr %q does not say %q", r.stderr, w)
		}
	}
}

func TestANetworkThatReachesTheInternetIsExitZeroAndSilent(t *testing.T) {
	r := netRun(t, execx.Result{Stdout: []byte("<html></html>")}, nil)
	said(t, r, term.ExitOK)
	if r.stderr.Len() != 0 {
		t.Fatalf("stderr %q", r.stderr)
	}
	want := []string{"run", "--rm", "--network", "appnet", "alpine", "wget", "-q", "-O-", "--timeout=10", "http://google.com"}
	if c := r.runner.Calls()[0]; !slices.Equal(c.Args, want) {
		t.Fatalf("ran %q, want %q", c.Args, want)
	}
}

func TestAProbeThatRanAndFailedIsNoInternetWithWgetsWords(t *testing.T) {
	cases := []struct{ name, stderr, word string }{
		{"dns", "wget: bad address 'google.com'\n", "bad address"},
		{"timeout", "wget: download timed out\n", "timed out"},
		{"silent", "", "wget exited 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := netRun(t, execx.Result{ExitCode: 1, Stderr: []byte(tc.stderr)}, nil)
			said(t, r, ExitNoInternet, "appnet", "reaches no internet", tc.word)
		})
	}
}

func TestAProbeTheEngineCouldNotRunIsNeverNoInternet(t *testing.T) {
	cases := []struct {
		name string
		res  execx.Result
		err  error
		word string
	}{
		{"engine did not start", execx.Result{}, errors.New("exec: podman: permission denied"), "permission denied"},
		{"pull failed", execx.Result{ExitCode: 125, Stderr: []byte("Error: initializing source docker.io/library/alpine:latest: no route\n")}, nil, "initializing source"},
		{"no wget in the image", execx.Result{ExitCode: 127, Stderr: []byte("wget: not found\n")}, nil, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := netRun(t, tc.res, tc.err)
			said(t, r, term.ExitFailure, "appnet", "could not be checked", tc.word)
			if strings.Contains(r.stderr.String(), "no internet") {
				t.Fatalf("stderr %q reads an engine fault as the network's", r.stderr)
			}
		})
	}
}

func TestCheckNetworkRefusesWhatItCannotRun(t *testing.T) {
	r := newRun(install(t), checkNetworkFlag, "lxc", "appnet")
	said(t, r, term.ExitFailure, "could not be checked", "lxc")
	if len(r.runner.Calls()) != 0 {
		t.Fatal("an unknown engine ran something")
	}
	for _, args := range [][]string{{checkNetworkFlag}, {checkNetworkFlag, "podman"}, {checkNetworkFlag, "podman", "n", "x"}} {
		u := newRun(install(t), args...)
		if code := u.start(t); code != term.ExitUsage || !strings.Contains(u.stderr.String(), "usage:") {
			t.Fatalf("%q: exit %d, stderr %q", args, code, u.stderr)
		}
	}
}

func TestCheckNetworkIsAnsweredUnderLegacy(t *testing.T) {
	r := netRun(t, execx.Result{}, nil).env(LegacyEnv + "=1")
	said(t, r, term.ExitOK)
}

// versionsRun is --claude-versions podman claude-yolo:latest, the image answering image and
// the registry answering registry (or failing to start with regErr).
func versionsRun(t *testing.T, image, registry execx.Result, regErr error) *run {
	t.Helper()
	r := newRun(install(t), claudeVersionsFlag, "podman", "claude-yolo:latest")
	r.runner.OnFunc([]string{"podman", "run"}, func(execx.Cmd) (execx.Result, error) { return image, nil })
	r.runner.OnFunc([]string{"curl"}, func(execx.Cmd) (execx.Result, error) { return registry, regErr })
	return r
}

var (
	imageSays    = execx.Result{Stdout: []byte("2.0.0 (Claude Code)\n")}
	registrySays = execx.Result{Stdout: []byte(`{"name":"@anthropic-ai/claude-code","version":"2.1.3"}`)}
)

func TestBothVersionsKnownArePrintedInstalledThenLatest(t *testing.T) {
	r := versionsRun(t, imageSays, registrySays, nil)
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "2.0.0 2.1.3\n" || r.stderr.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, r.stdout, r.stderr)
	}
	calls := r.runner.Calls()
	if want := []string{"run", "--rm", "--entrypoint", "claude", "claude-yolo:latest", "--version"}; !slices.Equal(calls[0].Args, want) {
		t.Fatalf("ran %q, want %q", calls[0].Args, want)
	}
	if want := []string{"-fsSL", npmLatestURL}; !slices.Equal(calls[1].Args, want) {
		t.Fatalf("ran %q, want %q", calls[1].Args, want)
	}
}

func TestAVersionThatCouldNotBeReadSaysWhy(t *testing.T) {
	cases := []struct {
		name            string
		image, registry execx.Result
		regErr          error
		words           []string
	}{
		{"image would not run", execx.Result{ExitCode: 125, Stderr: []byte("Error: claude-yolo:latest: image not known")}, registrySays, nil, []string{"claude --version in claude-yolo:latest", "image not known"}},
		{"image printed no version", execx.Result{Stdout: []byte("Segmentation fault\n")}, registrySays, nil, []string{"printed no version", "Segmentation fault"}},
		{"registry unreachable", imageSays, execx.Result{ExitCode: 6, Stderr: []byte("curl: (6) Could not resolve host: registry.npmjs.org")}, nil, []string{"could not be reached", "Could not resolve host"}},
		{"registry error answer", imageSays, execx.Result{ExitCode: 22, Stderr: []byte("curl: (22) The requested URL returned error: 503")}, nil, []string{"answered with an error", "503"}},
		{"curl missing", imageSays, execx.Result{}, &execx.NotInstalledError{Name: "curl"}, []string{"curl is not installed"}},
		{"registry answer with no version", imageSays, execx.Result{Stdout: []byte(`{"error":"x"}`)}, nil, []string{"names no version"}},
		{"registry answer not json", imageSays, execx.Result{Stdout: []byte(`<html>`)}, nil, []string{"names no version"}},
		{"registry version not a version", imageSays, execx.Result{Stdout: []byte(`{"version":"1 2"}`)}, nil, []string{"names no version"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := versionsRun(t, tc.image, tc.registry, tc.regErr)
			said(t, r, term.ExitFailure, append([]string{"could not be read"}, tc.words...)...)
			if strings.Contains(r.stderr.String(), "offline") {
				t.Fatalf("stderr %q guesses offline", r.stderr)
			}
		})
	}
}

func TestClaudeVersionsRefusesWhatItCannotRun(t *testing.T) {
	r := newRun(install(t), claudeVersionsFlag, "lxc", "img")
	said(t, r, term.ExitFailure, "could not be read", "lxc")
	for _, args := range [][]string{{claudeVersionsFlag}, {claudeVersionsFlag, "podman"}, {claudeVersionsFlag, "podman", "img", "x"}} {
		u := newRun(install(t), args...)
		if code := u.start(t); code != term.ExitUsage || !strings.Contains(u.stderr.String(), "usage:") {
			t.Fatalf("%q: exit %d, stderr %q", args, code, u.stderr)
		}
	}
	r = versionsRun(t, imageSays, registrySays, nil).env(LegacyEnv + "=1")
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "2.0.0 2.1.3\n" {
		t.Fatalf("under legacy: exit %d, stdout %q", code, r.stdout)
	}
}

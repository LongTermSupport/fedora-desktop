package guest

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var errKernel = errors.New("not in this kernel")

type fakeKernel struct {
	pidfd, inotify error
	watched        []string
}

func (k *fakeKernel) Pidfd() error { return k.pidfd }

func (k *fakeKernel) Inotify(dir string) error {
	k.watched = append(k.watched, dir)
	return k.inotify
}

// guestWorld is a guest whose user `agent` has gh, tmux and claude on the unit's PATH, a gh
// logged in, a kernel with pidfd and inotify, and a homeserver that answers.
type guestWorld struct {
	home   string
	system []string
	sys    *fakeSystem
	run    *execx.Fake
	kernel *fakeKernel
	srv    *fakehs.Server
	// attachPath, when set, is the PATH the attach itself runs with.
	attachPath string
}

func fixedNow() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }

func newGuestWorld(t *testing.T) *guestWorld {
	t.Helper()
	w := &guestWorld{home: home(t), system: []string{dir(t, "usr-local-bin"), dir(t, "usr-bin"), dir(t, "bin")}}
	w.sys = agentSystem(w.home)
	w.run = &execx.Fake{}
	w.kernel = &fakeKernel{}
	executable(t, filepath.Join(w.system[1], "gh"))
	executable(t, filepath.Join(w.system[1], "tmux"))
	// Claude Code's own installer links ~/.local/bin/claude to a versioned file.
	versioned := filepath.Join(w.home, ".local", "share", "claude", "versions", "1.0.0")
	executable(t, versioned)
	if err := os.MkdirAll(filepath.Join(w.home, ".local", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versioned, filepath.Join(w.home, ".local", "bin", "claude")); err != nil {
		t.Fatal(err)
	}
	w.run.On([]string{filepath.Join(w.system[1], "gh"), "auth", "status"}, execx.Result{Stdout: []byte("Logged in to github.com account someone\n")})
	w.srv = fakehs.New(fakehs.Options{ServerName: "bus.example.com", Now: fixedNow, Quirks: fakehs.TuwunelQuirks()})
	if err := w.srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.srv.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func dir(t *testing.T, name string) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func executable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (w *guestWorld) checker() Checker {
	return Checker{Sys: w.sys, Run: w.run, Kernel: w.kernel, SystemDirs: w.system}
}

func (w *guestWorld) stdin() string {
	return `{"v":1,"team":"alpha","base_url":"` + w.srv.URL() + `","plain_http_hosts":["127.0.0.1"]}`
}

func alpha(t *testing.T) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func (w *guestWorld) check(t *testing.T, stdin string) error {
	t.Helper()
	return w.checker().Check(t.Context(), agentUser(t), alpha(t), strings.NewReader(stdin))
}

// GU2: every check passes; gh is run once, at the path found on the unit's PATH, with that
// PATH and the home; tmux is only looked at.
func TestCheckPasses(t *testing.T) {
	w := newGuestWorld(t)
	if err := w.check(t, w.stdin()); err != nil {
		t.Fatal(err)
	}
	calls := w.run.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls %v: only gh auth status runs", calls)
	}
	gh := calls[0]
	if gh.Name != filepath.Join(w.system[1], "gh") || !slices.Equal(gh.Args, []string{"auth", "status", "--hostname", "github.com"}) {
		t.Errorf("gh ran as %s", gh.Line())
	}
	wantPath := "PATH=" + w.home + "/.local/bin:" + strings.Join(w.system, ":")
	if !slices.Equal(gh.Env, []string{wantPath, "HOME=" + w.home}) {
		t.Errorf("gh env %v, want %s and the home", gh.Env, wantPath)
	}
	if gh.Stdout != nil || gh.Stderr != nil {
		t.Error("gh's output would reach the op's own streams")
	}
	if !slices.Equal(w.kernel.watched, []string{w.home}) {
		t.Errorf("inotify watched %v", w.kernel.watched)
	}
}

func TestUnitPath(t *testing.T) {
	if got := UnitPath("/srv/guest-home", SystemDirs); got != "/srv/guest-home/.local/bin:/usr/local/bin:/usr/bin:/bin" {
		t.Errorf("unit PATH = %s", got)
	}
}

func TestNewChecker(t *testing.T) {
	c := NewChecker()
	if c.Sys != (OS{}) || c.Kernel != (LinuxKernel{}) || !slices.Equal(c.SystemDirs, SystemDirs) {
		t.Errorf("checker = %+v", c)
	}
	if _, ok := c.Run.(execx.OS); !ok {
		t.Errorf("runner %T", c.Run)
	}
}

func TestOpCheckChangesNothing(t *testing.T) {
	changes, ok := ChangesOf("guest check")
	if !ok || len(changes) != 0 || OpCheck.Busy {
		t.Errorf("guest check: %v, %v, busy %v", changes, ok, OpCheck.Busy)
	}
}

func TestCheckRefusesTheCallerFirst(t *testing.T) {
	w := newGuestWorld(t)
	w.sys.uid = 0
	wantCode(t, w.check(t, "not even json"), 78, exits.CodeIdentity)
	if len(w.run.Calls()) != 0 {
		t.Error("gh ran for a refused caller")
	}
}

// The stdin contract (n3-D6): one strict object, its team --team; any miss is 64 usage.
func TestCheckRefusesItsStdin(t *testing.T) {
	cases := map[string]string{
		"not json":      `{"v":1`,
		"not an object": `[1]`,
		"extra key":     `{"v":1,"team":"alpha","base_url":"http://127.0.0.1:1","plain_http_hosts":[],"x":1}`,
		"missing key":   `{"v":1,"team":"alpha","base_url":"http://127.0.0.1:1"}`,
		"twin key":      `{"v":1,"team":"alpha","team":"alpha","base_url":"http://127.0.0.1:1","plain_http_hosts":[]}`,
		"v 2":           `{"v":2,"team":"alpha","base_url":"http://127.0.0.1:1","plain_http_hosts":[]}`,
		"another team":  `{"v":1,"team":"beta","base_url":"http://127.0.0.1:1","plain_http_hosts":[]}`,
		"bad team":      `{"v":1,"team":"Alpha!","base_url":"http://127.0.0.1:1","plain_http_hosts":[]}`,
		"url a number":  `{"v":1,"team":"alpha","base_url":1,"plain_http_hosts":[]}`,
		"hosts a str":   `{"v":1,"team":"alpha","base_url":"http://127.0.0.1:1","plain_http_hosts":"127.0.0.1"}`,
		"host a number": `{"v":1,"team":"alpha","base_url":"http://127.0.0.1:1","plain_http_hosts":[1]}`,
		"oversized":     `{"v":1,"team":"alpha","base_url":"http://127.0.0.1:1","plain_http_hosts":["` + strings.Repeat("1", stdinCap) + `"]}`,
	}
	for name, stdin := range cases {
		t.Run(name, func(t *testing.T) {
			w := newGuestWorld(t)
			err := w.check(t, stdin)
			wantCode(t, err, 64, exits.CodeUsage)
			if strings.Contains(err.Error(), "beta") || strings.Contains(err.Error(), "127.0.0.1") {
				t.Errorf("message quotes stdin: %s", err)
			}
		})
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("closed") }

func TestCheckRefusesAnUnreadableStdin(t *testing.T) {
	w := newGuestWorld(t)
	wantCode(t, w.checker().Check(t.Context(), agentUser(t), alpha(t), failReader{}), 64, exits.CodeUsage)
}

func TestCheckRefusesAHomeOfAnother(t *testing.T) {
	w := newGuestWorld(t)
	w.sys.accounts[0].Home = filepath.Join(w.home, "absent")
	wantCode(t, w.check(t, w.stdin()), 78, exits.CodeHome)
}

// GU2: each program is an executable regular file on exactly the unit's PATH.
func TestCheckFindsEachProgram(t *testing.T) {
	cases := map[string]struct {
		change func(w *guestWorld) error
		code   string
	}{
		"no gh": {func(w *guestWorld) error { return os.Remove(filepath.Join(w.system[1], "gh")) }, exits.CodeGh},
		"gh not executable": {func(w *guestWorld) error {
			return os.Chmod(filepath.Join(w.system[1], "gh"), 0o644)
		}, exits.CodeGh},
		"gh a directory": {func(w *guestWorld) error {
			return errors.Join(os.Remove(filepath.Join(w.system[1], "gh")), os.Mkdir(filepath.Join(w.system[1], "gh"), 0o755))
		}, exits.CodeGh},
		"gh only on the attach's PATH": {func(w *guestWorld) error {
			elsewhere := filepath.Join(w.home, "elsewhere")
			w.attachPath = elsewhere
			return errors.Join(os.Mkdir(elsewhere, 0o755), os.Rename(filepath.Join(w.system[1], "gh"), filepath.Join(elsewhere, "gh")))
		}, exits.CodeGh},
		"no tmux":   {func(w *guestWorld) error { return os.Remove(filepath.Join(w.system[1], "tmux")) }, exits.CodeTmux},
		"no claude": {func(w *guestWorld) error { return os.Remove(filepath.Join(w.home, ".local", "bin", "claude")) }, exits.CodeClaude},
		"claude a dangling link": {func(w *guestWorld) error {
			return os.Remove(filepath.Join(w.home, ".local", "share", "claude", "versions", "1.0.0"))
		}, exits.CodeClaude},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newGuestWorld(t)
			if err := c.change(w); err != nil {
				t.Fatal(err)
			}
			if w.attachPath != "" {
				t.Setenv("PATH", w.attachPath)
			}
			wantCode(t, w.check(t, w.stdin()), 78, c.code)
			if len(w.run.Calls()) != 0 {
				t.Errorf("ran %v before every program was found", w.run.Calls())
			}
		})
	}
}

// The first executable on the PATH is the one run, as a shell would; a non-executable earlier
// one is passed over.
func TestCheckRunsTheFirstGhOnThePath(t *testing.T) {
	w := newGuestWorld(t)
	early := filepath.Join(w.home, ".local", "bin", "gh")
	if err := os.WriteFile(early, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	executable(t, filepath.Join(w.system[2], "gh"))
	if err := w.check(t, w.stdin()); err != nil {
		t.Fatal(err)
	}
	if calls := w.run.Calls(); len(calls) != 1 || calls[0].Name != filepath.Join(w.system[1], "gh") {
		t.Errorf("ran %v", calls)
	}
}

func TestCheckNeedsGhLoggedIn(t *testing.T) {
	gh := func(w *guestWorld) []string { return []string{filepath.Join(w.system[1], "gh")} }
	t.Run("refused", func(t *testing.T) {
		w := newGuestWorld(t)
		w.run.On(gh(w), execx.Result{ExitCode: 1, Stderr: []byte("You are not logged into any GitHub hosts\n")})
		err := w.check(t, w.stdin())
		wantCode(t, err, 78, exits.CodeGh)
		if strings.Contains(err.Error(), "logged into") {
			t.Errorf("message quotes gh: %s", err)
		}
	})
	t.Run("not started", func(t *testing.T) {
		w := newGuestWorld(t)
		w.run.OnFunc(gh(w), func(execx.Cmd) (execx.Result, error) { return execx.Result{}, errors.New("exec format error") })
		wantCode(t, w.check(t, w.stdin()), 78, exits.CodeGh)
	})
}

func TestCheckNeedsTheKernel(t *testing.T) {
	for name, change := range map[string]func(*fakeKernel){
		"pidfd":   func(k *fakeKernel) { k.pidfd = errKernel },
		"inotify": func(k *fakeKernel) { k.inotify = errKernel },
	} {
		t.Run(name, func(t *testing.T) {
			w := newGuestWorld(t)
			change(w.kernel)
			wantCode(t, w.check(t, w.stdin()), 78, exits.CodeKernel)
		})
	}
}

// reach: the member client's URL rule, then GET /_matrix/client/versions within 3 s.
func TestCheckReachesTheHomeserver(t *testing.T) {
	if reachTimeout != 3*time.Second {
		t.Errorf("reach waits %v, B says 3 s", reachTimeout)
	}
	t.Run("a URL the hosts do not allow", func(t *testing.T) {
		w := newGuestWorld(t)
		wantCode(t, w.check(t, strings.Replace(w.stdin(), `["127.0.0.1"]`, `[]`, 1)), 78, exits.CodeReach)
	})
	t.Run("asks versions without a token", func(t *testing.T) {
		w := newGuestWorld(t)
		if err := w.check(t, w.stdin()); err != nil {
			t.Fatal(err)
		}
		reqs := w.srv.Requests()
		if len(reqs) != 1 || !strings.HasSuffix(reqs[0].Path, "/_matrix/client/versions") {
			t.Errorf("requests %+v", reqs)
		}
	})
	t.Run("down", func(t *testing.T) {
		w := newGuestWorld(t)
		stdin := w.stdin()
		if err := w.srv.Stop(); err != nil {
			t.Fatal(err)
		}
		wantCode(t, w.check(t, stdin), 78, exits.CodeReach)
	})
}

func TestLinuxKernel(t *testing.T) {
	var k LinuxKernel
	if err := k.Pidfd(); err != nil {
		t.Errorf("pidfd: %v", err)
	}
	if err := k.Inotify(t.TempDir()); err != nil {
		t.Errorf("inotify: %v", err)
	}
	if err := k.Inotify(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a watch on nothing")
	}
}

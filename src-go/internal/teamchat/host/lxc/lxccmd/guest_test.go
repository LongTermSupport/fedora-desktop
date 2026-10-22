package lxccmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const token = "tok-SECRET-1234"

func attach(t *testing.T) Attach {
	t.Helper()
	u, err := paths.ParseGuestUser("agent")
	if err != nil {
		t.Fatal(err)
	}
	d, err := paths.ParseDigest("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return Attach{Container: "c1", User: u, UID: 1001, GID: 1001, Home: "/home/<user>", Kit: paths.HostKitDir(d)}
}

// errorLine is the error object the member binary writes as stderr's last line for code.
func errorLine(t *testing.T, code string) []byte {
	t.Helper()
	var b bytes.Buffer
	exits.Write(&b, exits.New(code, nil, "refused"))
	return b.Bytes()
}

func TestGuestArgv(t *testing.T) {
	f := &execx.Fake{}
	a := attach(t)
	var stdin []byte
	f.OnFunc([]string{"lxc-attach"}, func(c execx.Cmd) (execx.Result, error) {
		b, err := io.ReadAll(c.Stdin)
		stdin = b
		return execx.Result{Stdout: []byte("CHANGED\tseat\nCHANGED\ttoken\n")}, err
	})
	res, err := LXC{Runner: f, DesktopUID: 1000}.Guest(t.Context(), a, guest.OpPlaceBundle, []string{"--team", "t1"}, []byte(token))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Changed, []guest.What{guest.WhatSeat, guest.WhatToken}) {
		t.Errorf("changed %v", res.Changed)
	}
	if string(stdin) != token {
		t.Errorf("stdin %q", stdin)
	}
	calls := f.Calls()
	want := []string{
		"-n", "c1", "-u", "1001", "-g", "1001", "--clear-env",
		"--set-var", "HOME=/home/<user>", "--set-var", "USER=agent", "--set-var", "LOGNAME=agent",
		"--set-var", "PATH=/usr/local/bin:/usr/bin:/bin",
		"--", a.Kit.Binary(), "guest", "place-bundle", "--user", "agent", "--team", "t1",
	}
	if len(calls) != 1 || calls[0].Name != "lxc-attach" || !slices.Equal(calls[0].Args, want) {
		t.Fatalf("%v", calls)
	}
	if strings.Contains(calls[0].Line(), token) {
		t.Error("the token is in the argv")
	}
}

func TestGuestHasADeadline(t *testing.T) {
	r := &deadlineRunner{}
	if _, err := (LXC{Runner: r, DesktopUID: 1000}).Guest(t.Context(), attach(t), guest.OpCheck, nil, nil); err == nil || !r.hadDeadline {
		t.Fatalf("deadline %v, %v", r.hadDeadline, err)
	}
	// A deadline already passed stands for GuestWait running out.
	expired, cancel := context.WithDeadline(t.Context(), time.Time{})
	defer cancel()
	_, err := LXC{Runner: r, DesktopUID: 1000}.Guest(expired, attach(t), guest.OpCheck, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "guest check") {
		t.Fatalf("got %v", err)
	}
	if ae, ok := errors.AsType[*AttachError](err); ok {
		t.Errorf("a timeout is not an attach failure: %v", ae)
	}
}

// deadlineRunner records whether the command's context had a deadline and answers as a
// command a cancelled context ended.
type deadlineRunner struct{ hadDeadline bool }

func (r *deadlineRunner) Run(ctx context.Context, _ execx.Cmd) (execx.Result, error) {
	_, r.hadDeadline = ctx.Deadline()
	return execx.Result{}, &execx.ExitError{Name: "lxc-attach", Code: -1, Signal: syscall.SIGTERM}
}

func TestGuestRefusesBeforeRunning(t *testing.T) {
	cases := map[string]func(a *Attach){
		"uid 0":           func(a *Attach) { a.UID = 0 },
		"gid 0":           func(a *Attach) { a.GID = 0 },
		"a negative uid":  func(a *Attach) { a.UID = -1 },
		"the desktop uid": func(a *Attach) { a.UID = 1000 },
		"a bad container": func(a *Attach) { a.Container = "a b" },
		"no kit":          func(a *Attach) { a.Kit = paths.Kit{} },
		"the current kit": func(a *Attach) { a.Kit = paths.HostKitCurrent() },
		"the ccy kit":     func(a *Attach) { a.Kit = paths.CCYKitDir() },
		"no user":         func(a *Attach) { a.User = paths.GuestUser{} },
		"a relative home": func(a *Attach) { a.Home = "home/<user>" },
		"an unclean home": func(a *Attach) { a.Home = "/home/../agent" },
		"a control":       func(a *Attach) { a.Home = "/home/a\nb" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := &execx.Fake{}
			a := attach(t)
			mutate(&a)
			if _, err := (LXC{Runner: f, DesktopUID: 1000}).Guest(t.Context(), a, guest.OpCheck, nil, nil); !errors.Is(err, ErrRefused) {
				t.Fatalf("got %v", err)
			}
			if len(f.Calls()) != 0 {
				t.Fatal("ran")
			}
		})
	}
	f := &execx.Fake{}
	if _, err := (LXC{Runner: f, DesktopUID: 1000}).Guest(t.Context(), attach(t), guest.Op{Verb: "guest evil"}, nil, nil); !errors.Is(err, ErrRefused) {
		t.Fatalf("an unknown op: %v", err)
	}
	if _, err := (LXC{Runner: f, DesktopUID: 1000}).Guest(t.Context(), attach(t), guest.OpCheck, []string{"a\x00b"}, nil); !errors.Is(err, ErrRefused) {
		t.Fatalf("a NUL in an argument: %v", err)
	}
	if len(f.Calls()) != 0 {
		t.Fatal("ran")
	}
}

func TestGuestStdoutGrammar(t *testing.T) {
	bad := map[string]string{
		"an empty line":          "CHANGED\tseat\n\n",
		"a what outside the set": "CHANGED\tdesired.json\n",
		"a carriage return":      "CHANGED\tseat\r\n",
		"an ANSI escape":         "CHANGED\t\x1b[31mseat\n",
		"a long line":            "CHANGED\t" + strings.Repeat("s", 300) + "\n",
		"a forged summary":       "LXC-SUMMARY\tok\n",
		"a forged team line":     "CHANGED\tt1\tseat\tok\n",
		"no newline at the end":  "CHANGED\tseat",
		"a space for the tab":    "CHANGED seat\n",
		"a guest byte":           "GUESTBYTES\n",
	}
	for name, out := range bad {
		t.Run(name, func(t *testing.T) {
			f := &execx.Fake{}
			f.On([]string{"lxc-attach"}, execx.Result{Stdout: []byte(out)})
			_, err := LXC{Runner: f, DesktopUID: 1000}.Guest(t.Context(), attach(t), guest.OpPlaceBundle, nil, []byte(token))
			if !errors.Is(err, ErrOutput) || !strings.Contains(err.Error(), "guest place-bundle") {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(err.Error(), "GUESTBYTES") || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "\x1b") {
				t.Fatalf("quoted the guest: %q", err)
			}
		})
	}
	f := &execx.Fake{}
	f.On([]string{"lxc-attach"}, execx.Result{Stdout: []byte("CHANGED\tseat\n")})
	if _, err := (LXC{Runner: f, DesktopUID: 1000}).Guest(t.Context(), attach(t), guest.OpCheck, nil, nil); !errors.Is(err, ErrOutput) {
		t.Fatalf("guest check printing: %v", err)
	}
	f.On([]string{"lxc-attach"}, execx.Result{})
	if res, err := (LXC{Runner: f, DesktopUID: 1000}).Guest(t.Context(), attach(t), guest.OpCheck, nil, nil); err != nil || len(res.Changed) != 0 {
		t.Fatalf("guest check silent: %v %v", res, err)
	}
}

func TestGuestOutputIsCapped(t *testing.T) {
	big := bytes.Repeat([]byte("CHANGED\tseat\n"), MaxGuestOutput/12)
	for name, res := range map[string]execx.Result{
		"stdout": {Stdout: big},
		"stderr": {Stderr: append(bytes.Repeat([]byte("x"), MaxGuestOutput), errorLine(t, exits.CodeHome)...), ExitCode: 78},
	} {
		f := &execx.Fake{}
		f.On([]string{"lxc-attach"}, res)
		if _, err := (LXC{Runner: f, DesktopUID: 1000}).Guest(t.Context(), attach(t), guest.OpPlaceBundle, nil, nil); !errors.Is(err, ErrOutput) {
			t.Errorf("%s over the cap: %v", name, err)
		}
	}
}

func TestGuestErrors(t *testing.T) {
	cases := []struct {
		name   string
		op     guest.Op
		res    execx.Result
		exit   int
		code   string
		undeco bool
	}{
		{"78 home", guest.OpCheck, execx.Result{ExitCode: 78, Stderr: append([]byte("a diagnostic\n"), errorLine(t, exits.CodeHome)...)}, 78, exits.CodeHome, false},
		{"64 usage", guest.OpMembership, execx.Result{ExitCode: 64, Stderr: errorLine(t, exits.CodeUsage)}, 64, exits.CodeUsage, false},
		{"1 internal", guest.OpMembership, execx.Result{ExitCode: 1, Stderr: errorLine(t, exits.CodeInternal)}, 1, exits.CodeInternal, false},
		{"75 from a busy op", guest.OpPlaceBundle, execx.Result{ExitCode: 75, Stderr: errorLine(t, exits.CodeSeatHeld)}, 75, exits.CodeSeatHeld, false},
		{"75 from an op that takes no lock", guest.OpMembership, execx.Result{ExitCode: 75, Stderr: errorLine(t, exits.CodeSeatHeld)}, 75, "", true},
		{"an object for another exit", guest.OpCheck, execx.Result{ExitCode: 64, Stderr: errorLine(t, exits.CodeHome)}, 64, "", true},
		{"an exit no op makes", guest.OpCheck, execx.Result{ExitCode: 23, Stderr: errorLine(t, exits.CodeNotFound)}, 23, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &execx.Fake{}
			f.On([]string{"lxc-attach"}, c.res)
			_, err := LXC{Runner: f, DesktopUID: 1000}.Guest(t.Context(), attach(t), c.op, nil, []byte(token))
			var ge *GuestError
			if !errors.As(err, &ge) {
				t.Fatalf("got %v", err)
			}
			if int(ge.Exit) != c.exit || ge.Code != c.code || ge.Undecodable != c.undeco || ge.Op != c.op.Verb {
				t.Fatalf("%+v", ge)
			}
			if !strings.Contains(err.Error(), c.op.Verb) || strings.Contains(err.Error(), "diagnostic") || strings.Contains(err.Error(), token) {
				t.Fatalf("%q", err)
			}
		})
	}
}

func TestGuestAttachFailures(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]func(f *execx.Fake){
		"lxc-attach's own failure": func(f *execx.Fake) {
			f.On([]string{"lxc-attach"}, execx.Result{ExitCode: 1, Stderr: []byte("lxc-attach: c1: attach.c: Failed to get init pid\n")})
		},
		"no stderr at all": func(f *execx.Fake) { f.On([]string{"lxc-attach"}, execx.Result{ExitCode: 1}) },
		"a signal": func(f *execx.Fake) {
			f.OnError([]string{"lxc-attach"}, &execx.ExitError{Name: "lxc-attach", Code: -1, Signal: syscall.SIGKILL})
		},
		"a start failure": func(f *execx.Fake) { f.OnError([]string{"lxc-attach"}, boom) },
		"an undecodable 78": func(f *execx.Fake) {
			f.On([]string{"lxc-attach"}, execx.Result{ExitCode: 78, Stderr: []byte("{\"kind\":\"error\"}\n")})
		},
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			f := &execx.Fake{}
			script(f)
			_, err := LXC{Runner: f, DesktopUID: 1000}.Guest(t.Context(), attach(t), guest.OpMembership, nil, []byte(token))
			var ae *AttachError
			if !errors.As(err, &ae) || ae.Container != "c1" || ae.Op != "guest membership" {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(err.Error(), "init pid") || strings.Contains(err.Error(), token) {
				t.Fatalf("quoted stderr: %q", err)
			}
		})
	}
	f := &execx.Fake{}
	f.OnError([]string{"lxc-attach"}, boom)
	if _, err := (LXC{Runner: f, DesktopUID: 1000}).Guest(t.Context(), attach(t), guest.OpCheck, nil, nil); !errors.Is(err, boom) {
		t.Errorf("the start failure is not wrapped: %v", err)
	}
}

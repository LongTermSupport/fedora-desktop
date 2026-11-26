package chatclientinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/matrixclients"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// clients is the list the command is built with; the tests never spell an app id, so the
// matrix-client-named-by-hand rule holds for them too.
func clients(t *testing.T) []matrixclients.Client {
	t.Helper()
	list, err := matrixclients.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Clients) == 0 {
		t.Fatal("the embedded client list is empty")
	}
	return list.Clients
}

func newApp(t *testing.T, runner execx.Runner) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return &App{Runner: runner, Clients: clients(t), Home: "/home/someone", UID: 1000, Stdout: &out, Stderr: &errOut}, &out, &errOut
}

func TestEveryCallRunsTheFixedFlatpakInAFixedEnvironment(t *testing.T) {
	fake := healthy(t)
	app, _, errOut := newApp(t, fake)
	if code := app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	want := []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "HOME=/home/someone", "XDG_RUNTIME_DIR=/run/user/1000"}
	for _, c := range fake.Calls() {
		if c.Name != "/usr/bin/flatpak" {
			t.Errorf("%s: runs %q, not the full path: the caller's PATH would choose it", c.Line(), c.Name)
		}
		if !c.CleanEnv || !slices.Equal(c.Env, want) {
			t.Errorf("%s: CleanEnv %v, Env %q; want only %q", c.Line(), c.CleanEnv, c.Env, want)
		}
	}
}

func TestEnvironmentValuesInThePermissionsAreHidden(t *testing.T) {
	fake := healthy(t)
	cs := clients(t)
	perms := "[Context]\nshared=network;\n\n[Environment]\nSSL_CERT_FILE=/etc/x.pem\nAPI_TOKEN=s3cr3t\n\n[Session Bus Policy]\norg.example=talk\n"
	fake.On([]string{"/usr/bin/flatpak", "info", "--show-permissions", cs[0].AppID}, execx.Result{Stdout: []byte(perms)})
	app, out, errOut := newApp(t, fake)
	if code := app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	text := out.String()
	if strings.Contains(text, "s3cr3t") || strings.Contains(text, "/etc/x.pem") {
		t.Errorf("an environment value reached stdout:\n%s", text)
	}
	for _, want := range []string{"API_TOKEN=<hidden>", "SSL_CERT_FILE=<hidden>", "shared=network;", "org.example=talk"} {
		if !strings.Contains(text, want) {
			t.Errorf("stdout lacks %q:\n%s", want, text)
		}
	}
}

// healthy answers every command in the table: each client installed, the first one running.
func healthy(t *testing.T) *execx.Fake {
	t.Helper()
	fake := &execx.Fake{}
	cs := clients(t)
	running := "inst-1\t4242\t" + cs[0].AppID + "\n" + "inst-2\t4343\tsome.other.App\n"
	fake.On([]string{"/usr/bin/flatpak", "ps"}, execx.Result{Stdout: []byte(running)})
	for _, c := range cs {
		fake.On([]string{"/usr/bin/flatpak", "info", c.AppID}, execx.Result{Stdout: []byte("Ref: app/" + c.AppID + "\nVersion: 1.2.3\n")})
		fake.On([]string{"/usr/bin/flatpak", "info", "--show-permissions", c.AppID}, execx.Result{Stdout: []byte("[Context]\nshared=network;\n")})
	}
	return fake
}

func lines(cmds []execx.Cmd) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Line())
	}
	return out
}

func TestTheArgvTableIsExact(t *testing.T) {
	cs := clients(t)
	want := [][]string{{"/usr/bin/flatpak", "ps", "--columns=instance,pid,application"}}
	for _, c := range cs {
		want = append(want,
			[]string{"/usr/bin/flatpak", "info", c.AppID},
			[]string{"/usr/bin/flatpak", "info", "--show-permissions", c.AppID})
	}
	got := Table(cs)
	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("argv table\n got %q\nwant %q", got, want)
	}
}

func TestARunMakesExactlyTheTableCalls(t *testing.T) {
	fake := healthy(t)
	app, out, errOut := newApp(t, fake)
	if code := app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	var want []string
	for _, argv := range Table(clients(t)) {
		want = append(want, strings.Join(argv, " "))
	}
	if got := lines(fake.Calls()); !slices.Equal(got, want) {
		t.Fatalf("calls\n got %q\nwant %q", got, want)
	}
	cs := clients(t)
	text := out.String()
	for _, c := range cs {
		for _, part := range []string{c.Name + " (" + c.AppID + ")", "Ref: app/" + c.AppID} {
			if !strings.Contains(text, part) {
				t.Errorf("stdout lacks %q:\n%s", part, text)
			}
		}
	}
	if !strings.Contains(text, "inst-1\t4242\t"+cs[0].AppID) {
		t.Errorf("stdout lacks the running instance of %s:\n%s", cs[0].AppID, text)
	}
	if strings.Contains(text, "some.other.App") {
		t.Errorf("stdout shows an app that is not a chat client:\n%s", text)
	}
	if !strings.Contains(text, "[Context]") {
		t.Errorf("stdout lacks the permissions:\n%s", text)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr %q on a clean run", errOut)
	}
}

func TestAnyArgumentIsRefused(t *testing.T) {
	cs := clients(t)
	for _, args := range [][]string{{"--help"}, {"x"}, {cs[0].AppID}, {"run"}, {""}} {
		fake := &execx.Fake{}
		app, out, errOut := newApp(t, fake)
		if code := app.Run(context.Background(), args); code != term.ExitUsage {
			t.Errorf("%q: exit %d, want %d", args, code, term.ExitUsage)
		}
		if n := len(fake.Calls()); n != 0 {
			t.Errorf("%q: ran %d commands", args, n)
		}
		if out.Len() != 0 {
			t.Errorf("%q: stdout %q", args, out)
		}
		if !strings.Contains(errOut.String(), "takes no arguments") {
			t.Errorf("%q: stderr %q does not say why", args, errOut)
		}
	}
}

// refusedVerbs are flatpak verbs that reach into a client's sandbox or change its install.
var refusedVerbs = []string{
	"run", "enter", "override", "kill", "update", "uninstall", "permission-set", "make-current",
	"install", "remove", "permission-reset", "permission-remove", "document-export", "spawn",
}

func TestNoArgvHoldsARefusedVerb(t *testing.T) {
	for _, argv := range Table(clients(t)) {
		if argv[0] != "/usr/bin/flatpak" {
			t.Errorf("%q runs %s, not flatpak", argv, argv[0])
		}
		if argv[1] != "info" && argv[1] != "ps" {
			t.Errorf("%q: verb %s is neither info nor ps", argv, argv[1])
		}
		for _, word := range argv[1:] {
			for _, verb := range refusedVerbs {
				if strings.Contains(word, verb) {
					t.Errorf("%q holds %q", argv, verb)
				}
			}
			if strings.Contains(word, ".var/app") || strings.Contains(word, "/") {
				t.Errorf("%q names a path: %q", argv, word)
			}
		}
	}
}

func TestAnAppNotInstalledIsReportedNotFailed(t *testing.T) {
	fake := healthy(t)
	cs := clients(t)
	missing := cs[len(cs)-1].AppID
	fake.On([]string{"/usr/bin/flatpak", "info", missing}, execx.Result{
		ExitCode: 1, Stderr: []byte("error: " + missing + "/*unspecified*/*unspecified* not installed\n"),
	})
	app, out, errOut := newApp(t, fake)
	if code := app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if fake.Called("flatpak info --show-permissions " + missing) {
		t.Error("asked for the permissions of an app that is not installed")
	}
	if !strings.Contains(out.String(), "not installed") {
		t.Errorf("stdout does not say the app is not installed:\n%s", out)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr %q: an app not installed is an answer, not an error", errOut)
	}
}

func TestAFlatpakFailureFails(t *testing.T) {
	cs := clients(t)
	for name, setup := range map[string]func(*execx.Fake){
		"info refuses": func(f *execx.Fake) {
			f.On([]string{"/usr/bin/flatpak", "info", cs[0].AppID}, execx.Result{ExitCode: 1, Stderr: []byte("error: bus gone\n")})
		},
		"permissions refuse": func(f *execx.Fake) {
			f.On([]string{"/usr/bin/flatpak", "info", "--show-permissions", cs[0].AppID}, execx.Result{ExitCode: 1, Stderr: []byte("error: bus gone\n")})
		},
		"ps refuses": func(f *execx.Fake) {
			f.On([]string{"/usr/bin/flatpak", "ps"}, execx.Result{ExitCode: 1, Stderr: []byte("error: bus gone\n")})
		},
		"flatpak missing": func(f *execx.Fake) {
			f.OnFunc([]string{"/usr/bin/flatpak"}, func(execx.Cmd) (execx.Result, error) {
				return execx.Result{}, &execx.NotInstalledError{Name: "flatpak"}
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := healthy(t)
			setup(fake)
			app, _, errOut := newApp(t, fake)
			if code := app.Run(context.Background(), nil); code != term.ExitFailure {
				t.Fatalf("exit %d, want %d", code, term.ExitFailure)
			}
			text := errOut.String()
			carried := strings.Contains(text, "bus gone") || strings.Contains(text, "not installed")
			if !strings.HasPrefix(text, "chat-client-info: ") || !carried {
				t.Errorf("stderr %q does not carry flatpak's cause", text)
			}
		})
	}
}

// deadlines records whether every call it passes on had a deadline, and how far off it was.
type deadlines struct {
	inner   execx.Runner
	missing int
	longest time.Duration
}

func (d *deadlines) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	when, ok := ctx.Deadline()
	if !ok {
		d.missing++
	} else if left := time.Until(when); left > d.longest {
		d.longest = left
	}
	res, err := d.inner.Run(ctx, c)
	if err != nil {
		return res, fmt.Errorf("deadlines: %w", err)
	}
	return res, nil
}

func TestEveryCallHasATimeBound(t *testing.T) {
	rec := &deadlines{inner: healthy(t)}
	app, _, errOut := newApp(t, rec)
	if code := app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if rec.missing != 0 {
		t.Errorf("%d calls ran with no deadline", rec.missing)
	}
	if rec.longest > CallBound {
		t.Errorf("a call's deadline was %s away, past the bound %s", rec.longest, CallBound)
	}
}

func TestAnErrorFromTheRunnerIsNotSwallowed(t *testing.T) {
	fake := healthy(t)
	fake.OnFunc([]string{"/usr/bin/flatpak", "ps"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, errors.New("cannot fork")
	})
	app, out, errOut := newApp(t, fake)
	if code := app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Errorf("stdout %q after a failure before anything was read", out)
	}
	if !strings.Contains(errOut.String(), "cannot fork") {
		t.Errorf("stderr %q", errOut)
	}
}

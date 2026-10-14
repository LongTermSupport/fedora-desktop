package busadmin

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

type rig struct {
	a      *Admin
	f      *execx.Fake
	passed *strings.Builder
}

func newRig() rig {
	f := &execx.Fake{}
	passed := &strings.Builder{}
	return rig{
		a:      &Admin{Runner: f, Tool: "/usr/local/bin/agent-bus", Stdout: passed, Stderr: &strings.Builder{}},
		f:      f,
		passed: passed,
	}
}

func stdinOf(t *testing.T, c execx.Cmd) string {
	t.Helper()
	if c.Stdin == nil {
		return ""
	}
	data, err := io.ReadAll(c.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCheckPipesTheTeamFileInAndReturnsTheCanonicalJSON(t *testing.T) {
	r := newRig()
	r.f.On([]string{"/usr/local/bin/agent-bus", "render", "check"}, execx.Result{Stdout: []byte(`{"team":"alpha"}`)})
	got, err := r.a.Check(context.Background(), []byte("raw"), "")
	if err != nil || string(got) != `{"team":"alpha"}` {
		t.Fatalf("%q, %v", got, err)
	}
	call := r.f.Calls()[0]
	if !slices.Equal(call.Args, []string{"render", "check"}) || stdinOf(t, call) != "raw" {
		t.Fatalf("argv %v", call.Args)
	}
	if call.Stderr != r.a.Stderr {
		t.Fatal("the admin tool's stderr must reach the caller's stderr")
	}
}

func TestCheckPassesThePreviousInstalledFile(t *testing.T) {
	r := newRig()
	r.f.On([]string{"/usr/local/bin/agent-bus", "render", "check"}, execx.Result{})
	if _, err := r.a.Check(context.Background(), []byte("raw"), "/var/lib/agent-bus/alpha/team.json"); err != nil {
		t.Fatal(err)
	}
	want := []string{"render", "check", "--previous=/var/lib/agent-bus/alpha/team.json"}
	if got := r.f.Calls()[0].Args; !slices.Equal(got, want) {
		t.Fatalf("argv %v", got)
	}
}

func TestCheckKeepsTheExitStatusOfARefusal(t *testing.T) {
	r := newRig()
	r.f.On([]string{"/usr/local/bin/agent-bus"}, execx.Result{ExitCode: 78})
	_, err := r.a.Check(context.Background(), []byte("raw"), "")
	if code, ok := execx.ExitOf(err); !ok || code != 78 {
		t.Fatalf("got %v", err)
	}
}

func TestTomlDropinAndStampRenderFromTheCanonicalJSON(t *testing.T) {
	r := newRig()
	r.f.On([]string{"/usr/local/bin/agent-bus", "render", "toml"}, execx.Result{Stdout: []byte("toml")})
	r.f.On([]string{"/usr/local/bin/agent-bus", "render", "dropin"}, execx.Result{Stdout: []byte("dropin")})
	r.f.On([]string{"/usr/local/bin/agent-bus", "render", "stamp"}, execx.Result{Stdout: []byte("stamp")})
	ctx := context.Background()
	if got, err := r.a.Toml(ctx, []byte("T")); err != nil || string(got) != "toml" {
		t.Fatalf("toml %q, %v", got, err)
	}
	if got, err := r.a.Dropin(ctx, []byte("T"), []string{"192.0.2.10=agentbus0", "2001:db8::1=wg0"}); err != nil || string(got) != "dropin" {
		t.Fatalf("dropin %q, %v", got, err)
	}
	if got, err := r.a.Stamp(ctx, []byte("T"), "/lib/agent-bus.pyz", "1.9.3", "abc"); err != nil || string(got) != "stamp" {
		t.Fatalf("stamp %q, %v", got, err)
	}
	wants := [][]string{
		{"render", "toml"},
		{"render", "dropin", "--interface=192.0.2.10=agentbus0", "--interface=2001:db8::1=wg0"},
		{"render", "stamp", "--zipapp", "/lib/agent-bus.pyz", "--tuwunel", "1.9.3", "--installer-sha256", "abc"},
	}
	for i, call := range r.f.Calls() {
		if !slices.Equal(call.Args, wants[i]) || stdinOf(t, call) != "T" {
			t.Errorf("call %d: argv %v", i, call.Args)
		}
	}
}

func TestRenderFailuresNameTheCommand(t *testing.T) {
	r := newRig()
	r.f.On([]string{"/usr/local/bin/agent-bus"}, execx.Result{ExitCode: 1})
	ctx := context.Background()
	if _, err := r.a.Toml(ctx, nil); err == nil || !strings.Contains(err.Error(), "render toml") {
		t.Errorf("toml: %v", err)
	}
	if _, err := r.a.Dropin(ctx, nil, nil); err == nil || !strings.Contains(err.Error(), "render dropin") {
		t.Errorf("dropin: %v", err)
	}
	if _, err := r.a.Stamp(ctx, nil, "z", "v", "h"); err == nil || !strings.Contains(err.Error(), "render stamp") {
		t.Errorf("stamp: %v", err)
	}
}

func TestBootstrapPassesItsStdoutThroughAndKeepsItsStatus(t *testing.T) {
	r := newRig()
	r.f.On([]string{"/usr/local/bin/agent-bus", "bootstrap", "alpha"}, execx.Result{Stdout: []byte("CHANGED\tbootstrap\n")})
	if err := r.a.Bootstrap(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if r.passed.String() != "CHANGED\tbootstrap\n" {
		t.Fatalf("passed through %q", r.passed.String())
	}
	r.f.On([]string{"/usr/local/bin/agent-bus", "bootstrap"}, execx.Result{ExitCode: 69})
	err := r.a.Bootstrap(context.Background(), "alpha")
	if code, ok := execx.ExitOf(err); !ok || code != 69 {
		t.Fatalf("got %v", err)
	}
}

func TestBuildZipappRunsTheBundlerFromTheSourceWithACleanPythonEnvironment(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"python3"}, execx.Result{Stdout: []byte("BUNDLE-CHANGED /out/agent-bus.pyz\n")})
	b := &Bundler{Runner: f, Stderr: &strings.Builder{}}
	marker, err := b.Build(context.Background(), "/src", "/out/agent-bus.pyz", "agent-bus")
	if err != nil || marker != "BUNDLE-CHANGED /out/agent-bus.pyz" {
		t.Fatalf("%q, %v", marker, err)
	}
	call := f.Calls()[0]
	want := []string{"-B", "-s", "-m", "helpers.pingbus.bundle", "--source", "/src", "--out", "/out/agent-bus.pyz", "agent-bus"}
	if !slices.Equal(call.Args, want) || call.Dir != "/src" {
		t.Fatalf("argv %v, dir %q", call.Args, call.Dir)
	}
	for _, name := range []string{"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP", "PYTHONUSERBASE", "PYTHONPLATLIBDIR"} {
		if !slices.Contains(call.Unset, name) {
			t.Errorf("%s is not removed from the bundler's environment", name)
		}
	}
	f.On([]string{"python3"}, execx.Result{ExitCode: 1})
	if _, err := b.Build(context.Background(), "/src", "/out/x.pyz", "x"); err == nil || !strings.Contains(err.Error(), "helpers.pingbus.bundle") {
		t.Fatalf("got %v", err)
	}
}

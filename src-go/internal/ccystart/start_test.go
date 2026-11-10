package ccystart

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// v2 is an environment of a v2 session: off v1, on the v2 membership mount, with the pairs.
func v2(kv ...string) map[string]string {
	return with(append([]string{envTeamchat, "{d}/run-teamchat-membership"}, kv...)...)
}

func (f fixture) goRun(t *testing.T, env map[string]string, args []string) outcome {
	t.Helper()
	expanded := map[string]string{}
	for k, v := range env {
		expanded[k] = f.expand(v)
	}
	return f.runGo(t, expanded, args)
}

func TestV2ExecsTeamchatRunAroundTheSupervisor(t *testing.T) {
	f := newFixture(t)
	got := f.goRun(t, v2(envWrapper, supArmed), []string{"claude", "--dangerously-skip-permissions"})
	want := []string{
		f.paths.TeamchatLink, "run", "--claude", "--kind", "ccy", "--desired", f.paths.TeamchatDesired, "--",
		"python3", f.expand("{d}/ws/claude-supervise.py"), "--arm",
		"--plugin", "ccy-lifecycle=" + filepath.Join(f.paths.SupervisorPlugins, "ccy_lifecycle.py"), "--",
		"claude", "--dangerously-skip-permissions",
	}
	if got.code != 0 || !slices.Equal(got.argv, want) {
		t.Fatalf("got %d %q, want %q\n%s", got.code, got.argv, want, got.stderr)
	}
	if link := readLink(f.paths.TeamchatLink); link != filepath.Join(f.paths.Teamchat, "bin/teamchat") {
		t.Errorf("teamchat link %q", link)
	}
	if got.link != "none" {
		t.Errorf("a v2 session linked pingbus: %q", got.link)
	}
	if !slices.Contains(got.execs[0].Unset, envTeamchat) {
		t.Errorf("%s reaches the session: unset %q", envTeamchat, got.execs[0].Unset)
	}
}

func TestV2WithoutSupervisor(t *testing.T) {
	f := newFixture(t)
	got := f.goRun(t, v2(envNoSup, "1"), []string{"claude", "--x"})
	want := []string{f.paths.TeamchatLink, "run", "--claude", "--kind", "ccy", "--desired", f.paths.TeamchatDesired, "--", "claude", "--x"}
	if got.code != 0 || !slices.Equal(got.argv, want) {
		t.Fatalf("got %d %q, want %q\n%s", got.code, got.argv, want, got.stderr)
	}
}

func TestV2Refusals(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		args  []string
		setup func(*testing.T, fixture)
		says  string
	}{
		{name: "both systems", env: v2("PINGBUS_MEMBERSHIP", "{d}/run-ccy-membership", envNoSup, "1"), args: []string{"claude"}, says: "both"},
		{name: "a membership path ccy never passes", env: with(envTeamchat, "/elsewhere", envNoSup, "1"), args: []string{"claude"}, says: "TEAMCHAT_MEMBERSHIP is /elsewhere"},
		{name: "not claude", env: v2(envNoSup, "1"), args: []string{"bash"}, says: "'bash'"},
		{name: "no kit", env: v2(envNoSup, "1"), args: []string{"claude"}, setup: remove("image/teamchat/bin/teamchat"), says: "ccy --rebuild"},
		{name: "no desired membership", env: v2(envNoSup, "1"), args: []string{"claude"}, setup: remove("run-teamchat-membership/desired.json"), says: "desired.json is missing"},
		{name: "a leftover v1 variable", env: v2("PINGBUS_SEATS", "a@b", envNoSup, "1"), args: []string{"claude"}, says: "PINGBUS_SEATS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			if c.setup != nil {
				c.setup(t, f)
			}
			got := f.goRun(t, c.env, c.args)
			if got.code != int(term.ExitFailure) || got.argv != nil {
				t.Fatalf("not refused: %+v", got)
			}
			if !strings.Contains(got.stderr, f.expand(c.says)) {
				t.Errorf("stderr %q does not say %q", got.stderr, c.says)
			}
			if readLink(f.paths.TeamchatLink) != "none" {
				t.Error("a refused launch linked teamchat")
			}
		})
	}
}

func TestExecFailureIsReported(t *testing.T) {
	f := newFixture(t)
	var stderr bytes.Buffer
	fake := &execx.Fake{}
	fake.ExecError(errors.New("no such program"))
	code := Start(context.Background(), Session{
		Env: func(k string) (string, bool) {
			v, ok := map[string]string{"PINGBUS_MEMBERSHIP": "off", envNoSup: "1"}[k]
			return v, ok
		},
		Args:   []string{"claude"},
		Paths:  f.paths,
		Stderr: &stderr,
		Runner: execx.OS{},
		Execer: fake,
	})
	if code != term.ExitFailure || !strings.Contains(stderr.String(), "no such program") {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
}

func TestLinkFailureIsReported(t *testing.T) {
	f := newFixture(t)
	if err := os.RemoveAll(filepath.Dir(f.paths.TeamchatLink)); err != nil {
		t.Fatal(err)
	}
	got := f.goRun(t, v2(envNoSup, "1"), []string{"claude"})
	if got.code != int(term.ExitFailure) || !strings.Contains(got.stderr, "teamchat") {
		t.Fatalf("got %+v", got)
	}
}

func TestImagePaths(t *testing.T) {
	p := ImagePaths()
	if p.TeamchatDesired != "/run/teamchat/membership" || p.Teamchat != "/opt/claude-yolo/optional/teamchat" ||
		p.Membership != "/run/ccy/membership" || p.TeamchatLink != "/usr/local/bin/teamchat" {
		t.Fatalf("%+v", p)
	}
}

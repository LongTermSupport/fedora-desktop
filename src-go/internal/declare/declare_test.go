package declare

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const prefix = "env PYTHONPATH=/root python3 -P -m helpers.agent_bus.declare"

func newClient(f *execx.Fake) *Client {
	return &Client{Runner: f, Root: "/root", File: "/hv.yml"}
}

func TestNewClientDefaultsErrToTheProcessStderr(t *testing.T) {
	c := NewClient(&execx.Fake{}, "/root", "/hv.yml")
	if c.Err != os.Stderr || c.Root != "/root" || c.File != "/hv.yml" {
		t.Fatalf("got %+v", c)
	}
}

func TestSuccessfulRunStderrIsForwardedEvenWithNoErrWriter(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	processStderr := os.Stderr
	os.Stderr = pw
	f := &execx.Fake{}
	f.On([]string{"env"}, execx.Result{Stdout: []byte("CHANGED\tx\n"), Stderr: []byte("kept at /hv.yml.backup.1\n")})
	_, runErr := newClient(f).Remove(context.Background(), "alpha", false)
	os.Stderr = processStderr
	if closeErr := pw.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	got, readErr := io.ReadAll(pr)
	if readErr != nil || string(got) != "kept at /hv.yml.backup.1\n" {
		t.Fatalf("stderr %q err %v", got, readErr)
	}
}

func script(f *execx.Fake, sub, stdout string) {
	f.On([]string{"env", "PYTHONPATH=/root", "python3", "-P", "-m", "helpers.agent_bus.declare", sub},
		execx.Result{Stdout: []byte(stdout)})
}

func lastLine(t *testing.T, f *execx.Fake) string {
	t.Helper()
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("nothing ran")
	}
	last := calls[len(calls)-1]
	if last.Dir != "" {
		t.Fatalf("declare must run in the caller's directory, ran in %q", last.Dir)
	}
	return last.Line()
}

func TestDeclared(t *testing.T) {
	f := &execx.Fake{}
	script(f, "declared", "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\n"+
		"TEAM\told\t-\tabsent\told.agent-bus.internal\t-\n")
	teams, err := newClient(f).Declared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Team{
		{Name: "alpha", Port: 6167, Present: true, ServerName: "alpha.agent-bus.internal", URL: "http://192.0.2.10:6167"},
		{Name: "old", ServerName: "old.agent-bus.internal"},
	}
	if !reflect.DeepEqual(teams, want) {
		t.Fatalf("got %+v", teams)
	}
	if got := lastLine(t, f); got != prefix+" declared --file /hv.yml" {
		t.Fatalf("ran %q", got)
	}
}

func TestDeclaredRejectsAMalformedLine(t *testing.T) {
	for _, line := range []string{
		"TEAM\talpha\t6167\tpresent\tsrv",
		"TEAM\talpha\tport\tpresent\tsrv\t-",
		"TEAM\talpha\t6167\tgone\tsrv\t-",
	} {
		f := &execx.Fake{}
		script(f, "declared", line+"\n")
		if _, err := newClient(f).Declared(context.Background()); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestStatusSortsInstalledAndParsesRows(t *testing.T) {
	f := &execx.Fake{}
	script(f, "status", "STATUS\talpha\tcurrent\t-\nSTATUS\tbeta\toutdated\tconfig,software\n")
	got, err := newClient(f).Status(context.Background(), "/src", "/applied", []string{"beta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Status{{Team: "alpha", State: "current"}, {Team: "beta", State: "outdated", Detail: "config,software"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	line := prefix + " status --file /hv.yml --source /src --applied /applied --installed alpha --installed beta"
	if got := lastLine(t, f); got != line {
		t.Fatalf("ran %q", got)
	}
}

func TestStatusRejectsAShortLine(t *testing.T) {
	f := &execx.Fake{}
	script(f, "status", "STATUS\talpha\tcurrent\n")
	if _, err := newClient(f).Status(context.Background(), "/s", "/a", nil); err == nil {
		t.Fatal("accepted a short STATUS line")
	}
}

func TestLimitedKeepsTheScopeWordsField(t *testing.T) {
	f := &execx.Fake{}
	script(f, "limited", "LIMITED\talpha\tdocs/, CLAUDE/Plan/\tany repository of example-org or example-org/proj\n")
	got, err := newClient(f).Limited(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Limit{{Team: "alpha", Prefixes: "docs/, CLAUDE/Plan/", Trusts: "any repository of example-org or example-org/proj"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	script(f, "limited", "LIMITED\talpha\tdocs/\n")
	if _, err := newClient(f).Limited(context.Background()); err == nil {
		t.Fatal("accepted a LIMITED line with no scope words")
	}
}

func TestElementTeams(t *testing.T) {
	f := &execx.Fake{}
	script(f, "element-teams", "ELEMENT\talpha\thttp://192.0.2.10:6167\n")
	got, err := newClient(f).ElementTeams(context.Background())
	if err != nil || !reflect.DeepEqual(got, []Profile{{Team: "alpha", BaseURL: "http://192.0.2.10:6167"}}) {
		t.Fatalf("%+v %v", got, err)
	}
	script(f, "element-teams", "ELEMENT\talpha\n")
	if _, err := newClient(f).ElementTeams(context.Background()); err == nil {
		t.Fatal("accepted a short ELEMENT line")
	}
}

func TestTeamsFor(t *testing.T) {
	f := &execx.Fake{}
	script(f, "teams-for", "TEAM\talpha\nTEAM\tbeta\n")
	got, err := newClient(f).TeamsFor(context.Background(), "example-org/proj")
	if err != nil || !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("%v %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" teams-for --file /hv.yml --repo example-org/proj" {
		t.Fatalf("ran %q", line)
	}
	script(f, "teams-for", "TEAM\n")
	if _, err := newClient(f).TeamsFor(context.Background(), "example-org/proj"); err == nil {
		t.Fatal("accepted a TEAM line with no name")
	}
}

func TestPlainValueSubcommands(t *testing.T) {
	f := &execx.Fake{}
	script(f, "github-remote", "example-org/proj\n")
	script(f, "address", "192.0.2.10\n")
	script(f, "suggest-team", "example-org\n")
	script(f, "next-steps", "Team alpha is up.\n  Sign in\n")
	c := newClient(f)
	ctx := context.Background()
	if got, err := c.GithubRemote(ctx, "git@github.com:Example-Org/proj.git"); err != nil || got != "example-org/proj" {
		t.Fatalf("remote %q %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" github-remote --url git@github.com:Example-Org/proj.git" {
		t.Fatalf("ran %q", line)
	}
	if got, err := c.Address(ctx); err != nil || got != "192.0.2.10" {
		t.Fatalf("address %q %v", got, err)
	}
	if got, err := c.SuggestTeam(ctx, "example-org"); err != nil || got != "example-org" {
		t.Fatalf("suggest %q %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" suggest-team --file /hv.yml --name example-org" {
		t.Fatalf("ran %q", line)
	}
	if got, err := c.NextSteps(ctx, "alpha"); err != nil || got != "Team alpha is up.\n  Sign in\n" {
		t.Fatalf("next-steps %q %v", got, err)
	}
}

func TestEmptyPlainValueIsNothing(t *testing.T) {
	f := &execx.Fake{}
	script(f, "address", "")
	if got, err := newClient(f).Address(context.Background()); err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestChecks(t *testing.T) {
	f := &execx.Fake{}
	script(f, "check-address", "")
	script(f, "check", "")
	c := newClient(f)
	ctx := context.Background()
	if err := c.CheckAddress(ctx, "192.0.2.10"); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" check-address --address 192.0.2.10" {
		t.Fatalf("ran %q", line)
	}
	e := Entry{
		Team: "alpha", Humans: []string{"a", "b"}, Org: "example-org",
		Repos: []string{"example-org/x", "example-org/y"}, AllowFrom: []string{"192.0.2.0/24"},
	}
	if err := c.Check(ctx, e); err != nil {
		t.Fatal(err)
	}
	want := prefix + " check --file /hv.yml --team alpha --human a --human b --org example-org" +
		" --repo example-org/x --repo example-org/y --allow-from 192.0.2.0/24"
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
	if err := c.Check(ctx, Entry{Repos: []string{"example-org/x"}}); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" check --file /hv.yml --repo example-org/x" {
		t.Fatalf("ran %q", line)
	}
}

func TestCreate(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"env"}, execx.Result{
		Stdout: []byte("CHANGED\tteam alpha, port 6167, in /hv.yml\n"),
		Stderr: []byte("Team alpha is declared in /hv.yml; the file as it was is kept at /hv.yml.backup.1.\n"),
	})
	errw := &bytes.Buffer{}
	c := newClient(f)
	c.Err = errw
	e := Entry{Team: "alpha", Humans: []string{"owner"}, Org: "example-org", Address: "192.0.2.10"}
	got, err := c.Create(context.Background(), e)
	if err != nil || !got.Changed || got.Text != "team alpha, port 6167, in /hv.yml" {
		t.Fatalf("%+v %v", got, err)
	}
	want := prefix + " create --file /hv.yml --team alpha --human owner --org example-org --address 192.0.2.10"
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
	if !strings.Contains(errw.String(), "kept at /hv.yml.backup.1") {
		t.Fatalf("the backup note was not passed on: %q", errw.String())
	}
}

func TestCreateDryRunReturnsTheEntryText(t *testing.T) {
	f := &execx.Fake{}
	script(f, "create", "  - team: alpha\n    port: 6167\n")
	e := Entry{Team: "alpha", Humans: []string{"o"}, Repos: []string{"example-org/x"}}
	got, err := newClient(f).CreateDryRun(context.Background(), e)
	if err != nil || got != "  - team: alpha\n    port: 6167\n" {
		t.Fatalf("%q %v", got, err)
	}
	want := prefix + " create --file /hv.yml --team alpha --human o --repo example-org/x --dry-run"
	if line := lastLine(t, f); line != want {
		t.Fatalf("ran %q", line)
	}
}

func TestElementChangedAndUnchanged(t *testing.T) {
	f := &execx.Fake{}
	script(f, "element", "CHANGED\tElement profile for alpha, in /hv.yml\n")
	c := newClient(f)
	got, err := c.Element(context.Background(), "alpha")
	if err != nil || !got.Changed || got.Text != "Element profile for alpha, in /hv.yml" {
		t.Fatalf("%+v %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" element --file /hv.yml --team alpha" {
		t.Fatalf("ran %q", line)
	}
	script(f, "element", "UNCHANGED\talpha\n")
	got, err = c.Element(context.Background(), "alpha")
	if err != nil || got.Changed || got.Text != "alpha" {
		t.Fatalf("%+v %v", got, err)
	}
	script(f, "element", "nothing useful\n")
	if _, err = c.Element(context.Background(), "alpha"); err == nil {
		t.Fatal("accepted output with neither CHANGED nor UNCHANGED")
	}
}

func TestDropLimitAndRemove(t *testing.T) {
	f := &execx.Fake{}
	script(f, "drop-limit", "CHANGED\tteam alpha may name any path, in /hv.yml\n")
	script(f, "remove", "CHANGED\tteam alpha absent, its data kept (purge: false), in /hv.yml\n")
	c := newClient(f)
	ctx := context.Background()
	got, err := c.DropLimit(ctx, "alpha")
	if err != nil || !got.Changed {
		t.Fatalf("%+v %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" drop-limit --file /hv.yml --team alpha" {
		t.Fatalf("ran %q", line)
	}
	if _, err = c.Remove(ctx, "alpha", false); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" remove --file /hv.yml --team alpha" {
		t.Fatalf("ran %q", line)
	}
	if _, err = c.Remove(ctx, "alpha", true); err != nil {
		t.Fatal(err)
	}
	if line := lastLine(t, f); line != prefix+" remove --file /hv.yml --team alpha --purge" {
		t.Fatalf("ran %q", line)
	}
}

func TestExitCodesBecomeTypedErrors(t *testing.T) {
	cases := []struct {
		code int
		kind error
	}{
		{64, ErrUsage}, {78, ErrRefused}, {130, ErrInterrupted}, {3, ErrFailed},
	}
	for _, tc := range cases {
		f := &execx.Fake{}
		f.On([]string{"env"}, execx.Result{ExitCode: tc.code, Stderr: []byte("declare: --team: nope\n")})
		err := newClient(f).CheckAddress(context.Background(), "x")
		if !errors.Is(err, tc.kind) {
			t.Fatalf("exit %d: %v", tc.code, err)
		}
		failure, ok := errors.AsType[*Error](err)
		if !ok || failure.Code != tc.code || failure.Message() != "--team: nope" || failure.Error() != "declare: --team: nope" {
			t.Fatalf("exit %d: %+v", tc.code, failure)
		}
	}
	if (&Error{Code: 3}).Error() != "declare exited 3" {
		t.Fatal("empty stderr text")
	}
}

func TestACommandThatCannotRunIsNotAnError(t *testing.T) {
	_, err := newClient(&execx.Fake{}).Declared(context.Background())
	if !errors.Is(err, execx.ErrUnexpected) {
		t.Fatalf("got %v", err)
	}
	if declErr, ok := errors.AsType[*Error](err); ok {
		t.Fatalf("a command that did not run is not a declare Error: %v", declErr)
	}
}

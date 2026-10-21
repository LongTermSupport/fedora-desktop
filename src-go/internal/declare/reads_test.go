package declare

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	busdeclare "github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The read methods answer from the host_vars file in-process (Plan 00209 SPEC-view.md V10-V23):
// each test writes a real file, and the fake runner, which answers nothing, proves no process ran.

// fileClient is a client over a host_vars file holding text, running commands with f.
func fileClient(t *testing.T, f *execx.Fake, text string) *Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "localhost.yml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Client{Runner: f, Root: "/root", File: path}
}

func noProcess(t *testing.T, f *execx.Fake) {
	t.Helper()
	if calls := f.Calls(); len(calls) != 0 {
		t.Fatalf("a read started %d processes, first %q", len(calls), calls[0].Line())
	}
}

// refusal checks err is a declare *Error of the kind and code, with v1's "declare: " line.
func refusal(t *testing.T, err error, kind error, code int, stderr string) {
	t.Helper()
	failure, ok := errors.AsType[*Error](err)
	if !ok || !errors.Is(err, kind) || failure.Code != code || failure.Error() != stderr ||
		failure.Message() != stderr[len("declare: "):] {
		t.Fatalf("got %#v (%v), want code %d %q", failure, err, code, stderr)
	}
}

const teamsText = "agent_bus_address: 192.0.2.10\nagent_bus_teams:\n" +
	"  - team: alpha\n    port: 6167\n    description: \"Builds: the \\\"site\\\"\"\n" +
	"    org: example-org\n    path_prefixes: [\"docs/\", \"CLAUDE/Plan/\"]\n" +
	"    repos:\n      - repo: example-org/proj\n        branches: [\"main\"]\n" +
	"  - team: old\n    state: absent\n" +
	"  - team: dash\n    state: absent\n    description: \"-\"\n" +
	"  - team: safe\n    state: absent\n    description: !vault |\n      $ANSIBLE_VAULT;1.1;AES256\n" +
	"  - team: bad\n    state: absent\n    description: \"\"\n"

func TestDeclared(t *testing.T) {
	f := &execx.Fake{}
	teams, err := fileClient(t, f, teamsText).Declared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Team{
		{
			Name: "alpha", Port: 6167, Present: true, ServerName: "alpha.agent-bus.internal", URL: "http://192.0.2.10:6167",
			Description: `Builds: the "site"`,
		},
		{Name: "old", ServerName: "old.agent-bus.internal"},
		{Name: "dash", ServerName: "dash.agent-bus.internal", Description: "-"},
		{Name: "safe", ServerName: "safe.agent-bus.internal", DescriptionState: DescriptionVaulted},
		{Name: "bad", ServerName: "bad.agent-bus.internal", DescriptionState: DescriptionInvalid},
	}
	if !reflect.DeepEqual(teams, want) {
		t.Fatalf("got %+v", teams)
	}
	noProcess(t, f)
}

// A file v1 refuses is refused with v1's exit class and words, as the run of declare.py was.
func TestDeclaredRefusesWhatV1Refuses(t *testing.T) {
	for name, tc := range map[string]struct{ text, stderr string }{
		"a port that is not a number": {
			"agent_bus_teams:\n  - team: alpha\n    port: port\n",
			"declare: line 3: the port is not a number",
		},
		"a state that is neither": {
			"agent_bus_teams:\n  - team: alpha\n    state: gone\n",
			"declare: line 3: state must be one of present, absent",
		},
		"a team twice": {
			"agent_bus_teams:\n  - team: alpha\n  - team: alpha\n",
			"declare: team alpha is declared twice in agent_bus_teams",
		},
	} {
		_, err := fileClient(t, &execx.Fake{}, tc.text).Declared(context.Background())
		t.Run(name, func(t *testing.T) { refusal(t, err, ErrRefused, 78, tc.stderr) })
	}
}

func TestAMissingHostVarsIsRefusedNamingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yml")
	_, err := (&Client{Runner: &execx.Fake{}, File: path}).Declared(context.Background())
	refusal(t, err, ErrRefused, 78, "declare: "+path+" does not exist (copy localhost.yml.dist to make it)")
	if !errors.Is(err, busdeclare.ErrRefused) {
		t.Fatalf("the reader's refusal is not kept as the cause: %v", err)
	}
}

// Where v1 crashed with a traceback, the reader's crash class is exit 1: ErrFailed.
func TestAFileThatIsNotUTF8IsTheCrashClass(t *testing.T) {
	c := fileClient(t, &execx.Fake{}, "agent_bus_teams:\n  - team: \xff\n")
	_, err := c.Declared(context.Background())
	refusal(t, err, ErrFailed, 1, "declare: "+c.File+" is not UTF-8 text")
}

func TestOverviewIsTheDeclaredTeamsAndTheStatesOfOneStatusRun(t *testing.T) {
	f := &execx.Fake{}
	c := fileClient(t, f, "agent_bus_address: 192.0.2.10\nagent_bus_teams:\n  - team: alpha\n    port: 6167\n")
	script(f, "status", "STATUS\talpha\tcurrent\t-\nSTATUS\tzeta\tdecide\tundeclared\n")
	teams, states, err := c.Overview(context.Background(), "/src", "/applied", []string{"beta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	wantTeams := []Team{{Name: "alpha", Port: 6167, Present: true, ServerName: "alpha.agent-bus.internal", URL: "http://192.0.2.10:6167"}}
	wantStates := []Status{{Team: "alpha", State: "current"}, {Team: "zeta", State: "decide", Detail: "undeclared"}}
	if !reflect.DeepEqual(teams, wantTeams) || !reflect.DeepEqual(states, wantStates) {
		t.Fatalf("got %+v %+v", teams, states)
	}
	line := prefix + " status --file " + c.File + " --source /src --applied /applied --installed alpha --installed beta"
	if got := lastLine(t, f); got != line || len(f.Calls()) != 1 {
		t.Fatalf("ran %q in %d calls", got, len(f.Calls()))
	}
}

func TestOverviewRefusesAMalformedStatusLineOrAFileV1Refuses(t *testing.T) {
	f := &execx.Fake{}
	script(f, "status", "STATUS\talpha\tcurrent\n")
	if _, _, err := fileClient(t, f, "agent_bus_teams:\n  - team: alpha\n").Overview(context.Background(), "/s", "/a", nil); err == nil {
		t.Fatal("accepted a short STATUS line")
	}
	g := &execx.Fake{}
	_, _, err := fileClient(t, g, "agent_bus_teams:\n  - team: alpha\n    port: port\n").Overview(context.Background(), "/s", "/a", nil)
	refusal(t, err, ErrRefused, 78, "declare: line 3: the port is not a number")
	noProcess(t, g)
}

func TestLimitedKeepsTheScopeWordsField(t *testing.T) {
	f := &execx.Fake{}
	got, err := fileClient(t, f, teamsText).Limited(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Limit{{Team: "alpha", Prefixes: "docs/, CLAUDE/Plan/", Trusts: "any repository of example-org or example-org/proj"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	noProcess(t, f)
	_, err = fileClient(t, f, "agent_bus_teams:\n  - team: alpha\n    path_prefixes: !vault x\n").Limited(context.Background())
	refusal(t, err, ErrRefused, 78,
		"declare: alpha's path_prefixes on line 3 is tagged (vault-encrypted?); ccy-teams reads only plain values")
}

func TestUndescribedKeepsTheScopeWordsField(t *testing.T) {
	f := &execx.Fake{}
	got, err := fileClient(t, f, "agent_bus_teams:\n  - team: alpha\n    org: example-org\n"+
		"  - team: beta\n    repos:\n      - repo: example-org/proj\n"+
		"  - team: gamma\n    org: !vault x\n"+
		"  - team: described\n    description: Builds the site\n  - team: gone\n    state: absent\n").Undescribed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Undescribed{
		{Team: "alpha", Trusts: "any repository of example-org"},
		{Team: "beta", Trusts: "example-org/proj"},
		{Team: "gamma", Trusts: "a scope ccy-teams cannot read"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	noProcess(t, f)
}

func TestElementTeams(t *testing.T) {
	f := &execx.Fake{}
	got, err := fileClient(t, f, "agent_bus_element_teams:\n  - team: alpha\n    base_url: http://192.0.2.10:6167\n  - team: beta\n").
		ElementTeams(context.Background())
	want := []Profile{{Team: "alpha", BaseURL: "http://192.0.2.10:6167"}, {Team: "beta", BaseURL: "None"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
	noProcess(t, f)
}

const choicesText = "agent_bus_teams:\n" +
	"  - team: alpha\n    org: example-org\n    description: Builds the site\n" +
	"    repos:\n      - repo: example-org/proj\n" +
	"  - team: beta\n    org: other-org\n" +
	"  - team: gamma\n    org: example-org\n    exclude: [\"proj\"]\n    description: !vault x\n" +
	"  - team: delta\n    repos: !vault x\n    description: \"\"\n" +
	"  - team: whole\n    org: example-org\n" +
	"  - team: gone\n    state: absent\n    org: example-org\n"

// A team whose scope is vaulted ends teams-for (v1 refused it), so delta is left out here.
func TestTeamsFor(t *testing.T) {
	f := &execx.Fake{}
	c := fileClient(t, f, strings.Replace(choicesText, "    repos: !vault x\n", "", 1))
	got, err := c.TeamsFor(context.Background(), "example-org/proj")
	if err != nil || !slices.Equal(got, []string{"alpha", "whole"}) {
		t.Fatalf("%v %v", got, err)
	}
	noProcess(t, f)
	_, err = c.TeamsFor(context.Background(), "Example-Org/proj")
	refusal(t, err, ErrUsage, 64, "declare: --repo: 'Example-Org/proj' is not a lower-case OWNER/REPO")
}

func TestChoicesParsesStandingAndDescription(t *testing.T) {
	f := &execx.Fake{}
	got, err := fileClient(t, f, choicesText).Choices(context.Background(), "example-org/proj")
	if err != nil {
		t.Fatal(err)
	}
	want := []Choice{
		{Team: "alpha", Standing: StandingCovers, Description: "Builds the site"},
		{Team: "beta", Standing: StandingWiden},
		{Team: "gamma", Standing: StandingExcluded, DescriptionState: DescriptionVaulted},
		{Team: "delta", Standing: StandingUnreadable, DescriptionState: DescriptionInvalid},
		{Team: "whole", Standing: StandingCovers},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	noProcess(t, f)
}

func TestPlainValueReads(t *testing.T) {
	f := &execx.Fake{}
	c := fileClient(t, f, "agent_bus_address: 192.0.2.10\nagent_bus_teams:\n  - team: alpha\n    port: 6167\n    org: example-org\n")
	ctx := context.Background()
	if got, err := c.GithubRemote(ctx, "git@github.com:Example-Org/proj.git"); err != nil || got != "example-org/proj" {
		t.Fatalf("remote %q %v", got, err)
	}
	if got, err := c.GithubRemote(ctx, "https://example.com/x/y"); err != nil || got != "" {
		t.Fatalf("a remote not on GitHub gave %q %v", got, err)
	}
	if got, err := c.Address(ctx); err != nil || got != "192.0.2.10" {
		t.Fatalf("address %q %v", got, err)
	}
	steps := "Team alpha is up. Later, in ccy-teams:\n" +
		"  Sign in      Enter on your name under the team, then choose Sign in: it sets a password and shows it.\n" +
		"  Element      Enter on the team, then choose Open in Element.\n" +
		"  Agents       in a checkout of any repository of example-org, run ccy --teams alpha\n" +
		"               (ccy-teams run there offers it; Enter on the team, then Start a ccy session, does it too).\n"
	if got, err := c.NextSteps(ctx, "alpha"); err != nil || got != steps {
		t.Fatalf("next-steps %q %v", got, err)
	}
	noProcess(t, f)
	_, err := c.NextSteps(ctx, "beta")
	refusal(t, err, ErrUsage, 64, "declare: --team: team beta is not declared present, with a port, in agent_bus_teams")
}

func TestNoAddressIsNothing(t *testing.T) {
	if got, err := fileClient(t, &execx.Fake{}, "agent_bus_teams:\n  - team: alpha\n").Address(context.Background()); err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestSuggestTeamStaysAPythonRun(t *testing.T) {
	f := &execx.Fake{}
	script(f, "suggest-team", "example-org\n")
	if got, err := newClient(f).SuggestTeam(context.Background(), "example-org"); err != nil || got != "example-org" {
		t.Fatalf("suggest %q %v", got, err)
	}
	if line := lastLine(t, f); line != prefix+" suggest-team --file /hv.yml --name example-org" {
		t.Fatalf("ran %q", line)
	}
}

// A command that could not be started is the runner's error, not a declare Error.
func TestACommandThatCannotRunIsNotAnError(t *testing.T) {
	_, err := newClient(&execx.Fake{}).Status(context.Background(), "/s", "/a", nil)
	if !errors.Is(err, execx.ErrUnexpected) {
		t.Fatalf("got %v", err)
	}
	if declErr, ok := errors.AsType[*Error](err); ok {
		t.Fatalf("a command that did not run is not a declare Error: %v", declErr)
	}
}

// SPEC-many-teams.md S-F2: a setting per key; a key the entry does not set has no value.
func TestSettingsReadsEachKeyAndItsJSONValue(t *testing.T) {
	f := &execx.Fake{}
	got, err := fileClient(t, f, "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    humans: !vault x\n"+
		"    repos:\n      - repo: a/b\n        branches: [\"main\"]\n    org: [\"unclosed\"\n").Settings(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	want := []Setting{
		{Key: "team", Value: []byte(`"alpha"`)},
		{Key: "description"},
		{Key: "humans", State: SettingVaulted},
		{Key: "org", State: SettingInvalid},
		{Key: "repos", Value: []byte(`[{"repo": "a/b", "branches": ["main"]}]`)},
		{Key: "exclude"},
		{Key: "path_prefixes"},
		{Key: "allow_from"},
		{Key: "port", Value: []byte(`6167`)},
		{Key: "listen"},
		{Key: "server_name", Value: []byte(`"alpha.agent-bus.internal"`)},
		{Key: "forge_api"},
		{Key: "removed_humans"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	noProcess(t, f)
}

// json.dumps writes NaN and Infinity, which are not JSON: a Setting's Value must be JSON, so the
// value is refused, as the SETTING line carrying it was.
func TestSettingsRefusesAValueThatIsNotJSON(t *testing.T) {
	_, err := fileClient(t, &execx.Fake{}, "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    humans: [NaN]\n").
		Settings(context.Background(), "alpha")
	if err == nil || err.Error() != `SETTING humans: the value is not JSON: "[NaN]"` {
		t.Fatalf("a value that is not JSON: %v", err)
	}
}

func TestLxcMembersReadsTheEntries(t *testing.T) {
	f := &execx.Fake{}
	got, err := fileClient(t, f, "agent_bus_teams:\n  - team: alpha\n    port: 6167\nagent_bus_lxc_members:\n"+
		"  - team: alpha\n    container: container-a\n    user: agent\n    host: worker\n    role: coordinator\n"+
		"    checkout: /srv/agent/one\n    seat: dev\n"+
		"  - team: alpha\n    container: container-b\n    user: agent\n    host: other\n    state: absent\n"+
		"    checkout: /srv/agent/two\n").LxcMembers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []LxcMember{
		{
			Team: "alpha", Container: "container-a", User: "agent", Host: "worker", Role: "coordinator", State: "present",
			Checkout: "/srv/agent/one", Seat: "dev",
		},
		{Team: "alpha", Container: "container-b", User: "agent", Host: "other", Role: "worker", State: "absent", Checkout: "/srv/agent/two"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	noProcess(t, f)
}

func TestNoLxcMembersIsNone(t *testing.T) {
	if got, err := fileClient(t, &execx.Fake{}, "agent_bus_teams:\n  - team: alpha\n").LxcMembers(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

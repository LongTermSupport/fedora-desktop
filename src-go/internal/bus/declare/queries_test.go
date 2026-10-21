package declare

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const board = `agent_bus_address: 2001:db8::10
agent_bus_teams:
  - team: alpha
    port: 6167
    description: "Builds the example site"
    org: example-org
    exclude: ["legacy"]
    repos:
      - repo: other-org/tool
    path_prefixes: ["docs/", "CLAUDE/Plan/"]
    humans: ["owner"]
  - team: beta
    port: 6168
    listen: ["192.0.2.20"]
    server_name: beta.example.com
    repos: [{"repo": "example-org/proj"}, {"repo": "example-org/legacy"}]
    path_prefixes: docs
  - team: gamma
    description: !vault |
      $ANSIBLE_VAULT;1.1;AES256
    org: !vault |
      $ANSIBLE_VAULT;1.1;AES256
  - team: old
    state: absent
    port: 6170
agent_bus_element_teams:
  - team: alpha
    base_url: "http://[2001:db8::10]:6167"
  - team: beta
`

func TestDeclared(t *testing.T) {
	teams, err := HostVars(board).Declared()
	if err != nil {
		t.Fatal(err)
	}
	want := []Team{
		{Name: "alpha", Port: 6167, Present: true, ServerName: "alpha.agent-bus.internal", URL: "http://[2001:db8::10]:6167", Description: "Builds the example site"},
		{Name: "beta", Port: 6168, Present: true, ServerName: "beta.example.com", URL: "http://192.0.2.20:6168"},
		{Name: "gamma", Present: true, ServerName: "gamma.agent-bus.internal", DescriptionState: DescriptionVaulted},
		{Name: "old", Port: 6170, ServerName: "old.agent-bus.internal", URL: "http://[2001:db8::10]:6170"},
	}
	if !reflect.DeepEqual(teams, want) {
		t.Fatalf("got  %+v\nwant %+v", teams, want)
	}
}

func TestDeclaredDescriptionInvalid(t *testing.T) {
	for _, desc := range []string{`" padded "`, "[1]", `"a\tb"`, "{"} {
		teams, err := HostVars("agent_bus_teams:\n  - team: a\n    description: " + desc + "\n").Declared()
		if err != nil || len(teams) != 1 || teams[0].DescriptionState != DescriptionInvalid || teams[0].Description != "" {
			t.Errorf("%s: %+v, %v", desc, teams, err)
		}
	}
}

func TestDeclaredCrashesWherePrintingWould(t *testing.T) {
	for _, text := range []string{
		"agent_bus_teams:\n  - team: a\n    description: \"\\ud800\"\n",
		"agent_bus_teams:\n  - team: a\n    port: 1\n    listen: [\"\\ud800\"]\n",
		"agent_bus_teams:\n  - team: a\n    description: " + strings.Repeat("1", 4301) + "\n",
	} {
		_, err := HostVars(text).Declared()
		if !errors.Is(err, ErrCrash) {
			t.Errorf("%q: %v, want the crash class", text, err)
		}
	}
}

func TestLimited(t *testing.T) {
	limits, err := HostVars(board).Limited()
	if err != nil {
		t.Fatal(err)
	}
	want := []Limit{
		{Team: "alpha", Prefixes: "docs/, CLAUDE/Plan/", Trusts: "any repository of example-org or other-org/tool"},
		{Team: "beta", Prefixes: "d, o, c, s", Trusts: "example-org/proj or example-org/legacy"},
	}
	if !reflect.DeepEqual(limits, want) {
		t.Fatalf("got %+v", limits)
	}
	_, err = HostVars("agent_bus_teams:\n  - team: a\n    path_prefixes: [x]\n").Limited()
	if !errors.Is(err, ErrRefused) || err.Error() != "a's path_prefixes on line 3 is a flow value ccy-teams cannot read (write it as JSON)" {
		t.Errorf("an unreadable value is refused: %v", err)
	}
	_, err = HostVars("agent_bus_teams:\n  - team: a\n    path_prefixes: [\"x\"]\n    org: !vault x\n").Limited()
	if !errors.Is(err, ErrVaulted) {
		t.Errorf("an unreadable scope is refused: %v", err)
	}
	for _, v := range []string{"5", "true", "-3"} {
		_, err = HostVars("agent_bus_teams:\n  - team: a\n    path_prefixes: " + v + "\n").Limited()
		if !errors.Is(err, ErrCrash) {
			t.Errorf("%s is not iterable (B-S3): %v", v, err)
		}
	}
}

func TestUndescribed(t *testing.T) {
	text := board + "  - team: delta\n    org: !vault x\n"
	text = strings.Replace(text, "agent_bus_element_teams:\n  - team: alpha\n    base_url: \"http://[2001:db8::10]:6167\"\n  - team: beta\n", "", 1)
	text += "  - team: eps\n    repos: [{\"repo\": 5}]\n"
	_, err := HostVars(text).Undescribed()
	if !errors.Is(err, ErrCrash) {
		t.Fatalf("a repo that is not a string cannot be joined (B-S3): %v", err)
	}
	got, err := HostVars(strings.TrimSuffix(text, "  - team: eps\n    repos: [{\"repo\": 5}]\n")).Undescribed()
	if err != nil {
		t.Fatal(err)
	}
	want := []Undescribed{
		{Team: "beta", Trusts: "example-org/proj or example-org/legacy"},
		{Team: "delta", Trusts: "a scope ccy-teams cannot read"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

const gammaBlock = `  - team: gamma
    description: !vault |
      $ANSIBLE_VAULT;1.1;AES256
    org: !vault |
      $ANSIBLE_VAULT;1.1;AES256
`

func TestTeamsForAndChoices(t *testing.T) {
	hv := HostVars(strings.Replace(board, gammaBlock, "", 1))
	if _, err := HostVars(board).TeamsFor("example-org/proj"); !errors.Is(err, ErrVaulted) {
		t.Errorf("teams-for refuses a team whose org is vaulted: %v", err)
	}
	for repo, want := range map[string][]string{
		"example-org/proj":   {"alpha", "beta"},
		"example-org/legacy": {"beta"},
		"other-org/tool":     {"alpha"},
		"other-org/x":        {},
	} {
		got, err := hv.TeamsFor(repo)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("TeamsFor(%s) = %v, %v; want %v", repo, got, err, want)
		}
	}
	choices, err := HostVars(board).Choices("example-org/legacy")
	if err != nil {
		t.Fatal(err)
	}
	want := []Choice{
		{Team: "alpha", Standing: StandingExcluded, Description: "Builds the example site"},
		{Team: "beta", Standing: StandingCovers},
		{Team: "gamma", Standing: StandingUnreadable, DescriptionState: DescriptionVaulted},
	}
	if !reflect.DeepEqual(choices, want) {
		t.Fatalf("got %+v", choices)
	}
	choices, err = HostVars(board).Choices("other-org/x")
	if err != nil || choices[0].Standing != StandingWiden {
		t.Fatalf("got %+v, %v", choices, err)
	}
}

func TestRepoFlagIsJudgedAfterTheFile(t *testing.T) {
	_, err := HostVars("agent_bus_teams: []\n").TeamsFor("Bad")
	if !errors.Is(err, ErrRefused) {
		t.Errorf("the file is read first: %v", err)
	}
	for _, repo := range []string{"Bad/Repo", "a/b.git", "a\nb/c"} {
		_, err = HostVars(board).Choices(repo)
		if !errors.Is(err, ErrUsage) || !strings.HasPrefix(err.Error(), "--repo: '") || !strings.HasSuffix(err.Error(), "' is not a lower-case OWNER/REPO") {
			t.Errorf("%q: %v", repo, err)
		}
	}
	_, err = HostVars(board).TeamsFor("a\nb/c")
	if err == nil || err.Error() != `--repo: 'a\nb/c' is not a lower-case OWNER/REPO` {
		t.Errorf("the repo is shown as python's repr: %v", err)
	}
}

func TestStandingCrashesOnValuesV1CouldNotIterate(t *testing.T) {
	for _, text := range []string{
		"agent_bus_teams:\n  - team: a\n    repos: 5\n",
		"agent_bus_teams:\n  - team: a\n    org: example-org\n    exclude: 5\n",
	} {
		if _, err := HostVars(text).TeamsFor("example-org/proj"); !errors.Is(err, ErrCrash) {
			t.Errorf("TeamsFor %q: %v", text, err)
		}
		if _, err := HostVars(text).Choices("example-org/proj"); !errors.Is(err, ErrCrash) {
			t.Errorf("Choices %q: %v", text, err)
		}
	}
	got, err := HostVars("agent_bus_teams:\n  - team: a\n    org: example-org\n    exclude: legacy-and-more\n").TeamsFor("example-org/legacy")
	if err != nil || len(got) != 0 {
		t.Errorf("a string exclude is python's substring test: %v, %v", got, err)
	}
	got, err = HostVars("agent_bus_teams:\n  - team: a\n    org: example-org\n    exclude: {\"proj\": 1}\n").TeamsFor("example-org/proj")
	if err != nil || len(got) != 0 {
		t.Errorf("a mapping exclude is its keys: %v, %v", got, err)
	}
}

func TestElementTeams(t *testing.T) {
	got, err := HostVars(board).ElementTeams()
	if err != nil {
		t.Fatal(err)
	}
	want := []Profile{{Team: "alpha", BaseURL: "http://[2001:db8::10]:6167"}, {Team: "beta", BaseURL: "None"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	got, err = HostVars("agent_bus_element_teams:\n  - team: a\n    base_url: [1, \"x\", true]\n").ElementTeams()
	if err != nil || got[0].BaseURL != "[1, 'x', True]" {
		t.Fatalf("base_url is python's str(): %+v, %v", got, err)
	}
}

func TestAddress(t *testing.T) {
	if got, err := HostVars(board).Address(); err != nil || got != "2001:db8::10" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := HostVars("x: 1\n").Address(); err != nil || got != "" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := HostVars("agent_bus_teams:\n  - team: X\n").Address(); !errors.Is(err, ErrRefused) {
		t.Errorf("the whole file is read: %v", err)
	}
}

func TestSettings(t *testing.T) {
	text := strings.Replace(board, "    humans: [\"owner\"]\n", "    humans: [\"owner\", \"\\u00e9\"]\n    allow_from: !vault x\n    listen: [nope]\n    forge_api: https://api.github.com\n", 1)
	got, err := HostVars(text).Settings("alpha")
	if err != nil {
		t.Fatal(err)
	}
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	want := []Setting{
		{Key: "team", Value: raw(`"alpha"`)},
		{Key: "description", Value: raw(`"Builds the example site"`)},
		{Key: "humans", Value: raw("[\"owner\", \"\xc3\xa9\"]")},
		{Key: "org", Value: raw(`"example-org"`)},
		{Key: "repos", Value: raw(`[{"repo": "other-org/tool"}]`)},
		{Key: "exclude", Value: raw(`["legacy"]`)},
		{Key: "path_prefixes", Value: raw(`["docs/", "CLAUDE/Plan/"]`)},
		{Key: "allow_from", State: SettingVaulted},
		{Key: "port", Value: raw("6167")},
		{Key: "listen", State: SettingInvalid},
		{Key: "server_name", Value: raw(`"alpha.agent-bus.internal"`)},
		{Key: "forge_api", Value: raw(`"https://api.github.com"`)},
		{Key: "removed_humans"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	for _, team := range []string{"gamma", "old", "nobody"} {
		_, err = HostVars(board).Settings(team)
		if !errors.Is(err, ErrUsage) || err.Error() != "--team: team "+team+" is not declared present, with a port, in agent_bus_teams" {
			t.Errorf("%s: %v", team, err)
		}
	}
	got, err = HostVars("agent_bus_teams:\n  - team: a\n    port: 1\n    server_name: \"\xc3\xa9\"\n").Settings("a")
	if err != nil || string(got[10].Value) != `"\u00e9"` {
		t.Errorf("server_name is json.dumps with ensure_ascii: %+v, %v", got, err)
	}
	_, err = HostVars("agent_bus_teams:\n  - team: a\n    port: 1\n    humans: [\"\\udc00\"]\n").Settings("a")
	if !errors.Is(err, ErrCrash) {
		t.Errorf("a lone surrogate cannot be printed: %v", err)
	}
}

func TestNextSteps(t *testing.T) {
	got, err := HostVars(board).NextSteps("beta")
	if err != nil {
		t.Fatal(err)
	}
	want := "Team beta is up. Later, in ccy-teams:\n" +
		"  Sign in      Enter on your name under the team, then choose Sign in: it sets a password and shows it.\n" +
		"  Element      Enter on the team, then choose Open in Element.\n" +
		"  Agents       in a checkout of example-org/proj or example-org/legacy, run ccy --teams beta\n" +
		"               (ccy-teams run there offers it; Enter on the team, then Start a ccy session, does it too).\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if _, err := HostVars(board).NextSteps("gamma"); !errors.Is(err, ErrUsage) {
		t.Errorf("gamma has no port: %v", err)
	}
	text := "agent_bus_teams:\n  - team: a\n    port: 1\n    org: !vault x\n"
	if _, err := HostVars(text).NextSteps("a"); !errors.Is(err, ErrVaulted) {
		t.Errorf("an unreadable scope is refused: %v", err)
	}
	text = "agent_bus_teams:\n  - team: a\n    port: 1\n"
	if got, err := HostVars(text).NextSteps("a"); err != nil || !strings.Contains(got, "a checkout of no repository, run") {
		t.Errorf("no scope: %q, %v", got, err)
	}
}

func TestValuesWithATabReachTheCallerWhole(t *testing.T) {
	// B-S4: v1's marker line split such a value; the reader returns it as it is.
	text := "agent_bus_teams:\n  - team: a\n    port: 1\n    server_name: 'a\tb'\n    path_prefixes: [\"x\\ty\"]\n"
	teams, err := HostVars(text).Declared()
	if err != nil || teams[0].ServerName != "a\tb" {
		t.Fatalf("%+v, %v", teams, err)
	}
	limits, err := HostVars(text).Limited()
	if err != nil || limits[0].Prefixes != "x\ty" {
		t.Fatalf("%+v, %v", limits, err)
	}
}

func TestPortBeyondAnIntIsRefused(t *testing.T) {
	// B-S6: v1 printed it; the Go client refused it on parse.
	_, err := HostVars("agent_bus_teams:\n  - team: a\n    port: 99999999999999999999\n").Declared()
	if !errors.Is(err, ErrRefused) || err.Error() != "line 3: the port is not a number" {
		t.Fatalf("got %v", err)
	}
}

func TestPortZeroIsAPort(t *testing.T) {
	teams, err := HostVars("agent_bus_address: 192.0.2.1\nagent_bus_teams:\n  - team: a\n    port: 0\n").Declared()
	if err != nil || teams[0].Port != 0 || teams[0].URL != "http://192.0.2.1:0" {
		t.Fatalf("%+v, %v", teams, err)
	}
}

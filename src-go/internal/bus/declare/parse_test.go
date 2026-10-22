package declare

import (
	"reflect"
	"strings"
	"testing"
)

const twoTeams = `---
user_login: someone
agent_bus_address: 192.0.2.10 # the bus
agent_bus_teams:
  - team: alpha
    port: 6167
    listen: ["192.0.2.11"]
    repos:
      - repo: example-org/proj
  # a comment inside the list

  - team: old
    state: absent
    server_name: 'old.example.com'
other_key: 1
`

func TestParseReadsTheEntriesInFileOrder(t *testing.T) {
	hv, err := parse(twoTeams, teamsKey)
	if err != nil {
		t.Fatal(err)
	}
	if hv.address != "192.0.2.10" {
		t.Errorf("address %q", hv.address)
	}
	if len(hv.teams) != 2 {
		t.Fatalf("teams %+v", hv.teams)
	}
	alpha, old := hv.teams[0], hv.teams[1]
	if got := (entry{team: alpha.team, port: alpha.port, hasPort: alpha.hasPort, state: alpha.state, serverName: alpha.serverName, listen: alpha.listen}); !reflect.DeepEqual(got, entry{team: "alpha", port: 6167, hasPort: true, state: "present", serverName: "alpha.agent-bus.internal", listen: "192.0.2.11"}) {
		t.Errorf("alpha %+v", alpha)
	}
	want := []rawKey{
		{key: "team", rest: " alpha", index: 4},
		{key: "port", rest: " 6167", index: 5},
		{key: "listen", rest: ` ["192.0.2.11"]`, index: 6},
		{key: "repos", rest: "", index: 7, nested: []string{"      - repo: example-org/proj"}},
	}
	if !reflect.DeepEqual(alpha.raw, want) {
		t.Errorf("alpha raw %+v", alpha.raw)
	}
	if old.team != "old" || old.hasPort || old.state != "absent" || old.serverName != "old.example.com" || old.listen != "" {
		t.Errorf("old %+v", old)
	}
}

func TestParseWithoutTheKeyOrItems(t *testing.T) {
	for _, text := range []string{"", "user_login: x\n", "agent_bus_teams:\nother: 1\n", "agent_bus_teams: # none yet\n", "agent_bus_teams: ''\n"} {
		hv, err := parse(text, teamsKey)
		if err != nil || len(hv.teams) != 0 {
			t.Errorf("%q: %+v, %v", text, hv, err)
		}
	}
}

func TestParseRefusals(t *testing.T) {
	item := "agent_bus_teams:\n  - team: alpha\n"
	for _, c := range []struct{ text, msg string }{
		{"  agent_bus_teams:\n", "line 1 names agent_bus_teams in a form ccy-teams does not edit"},
		{"x: 1 # agent_bus_teams\n", "line 1 names agent_bus_teams in a form ccy-teams does not edit"},
		{"agent_bus_teams:\nagent_bus_teams :\n", "agent_bus_teams is set more than once (lines 1, 2)"},
		{"agent_bus_address: 192.0.2.1\nagent_bus_address: 192.0.2.2\n", "agent_bus_address is set more than once"},
		{"agent_bus_teams: []\n", "agent_bus_teams on line 1 is written inline; ccy-teams adds only to a block list (as in localhost.yml.dist)"},
		{"agent_bus_teams: !vault |\n", "agent_bus_teams is tagged (vault-encrypted?); ccy-teams reads only plain values"},
		{"agent_bus_teams:\n\t- team: a\n", "line 2 is indented with a tab"},
		{"agent_bus_teams:\n  - team: a\n \t\n", "line 3 is indented with a tab"},
		{item + "  -   team: b\n", "line 3: the agent_bus_teams items are not indented alike"},
		{item + "   - team: b\n", "line 3: not the agent_bus_teams shape ccy-teams edits (a block list of mappings)"},
		{"agent_bus_teams:\n  team: a\n", "line 2: not the agent_bus_teams shape ccy-teams edits (a block list of mappings)"},
		{item + " - team: b\n", "line 3: not the agent_bus_teams shape ccy-teams edits (a block list of mappings)"},
		{"agent_bus_teams:\n  - [a]\n", "line 2: each agent_bus_teams item must be a block mapping of key: value lines"},
		{item + "    team: b\n", "line 3: team is set twice in one agent_bus_teams item"},
		{"agent_bus_teams:\n  - port: 1\n", "the agent_bus_teams item at line 2 has no team"},
		{item + "  - team: alpha\n", "team alpha is declared twice in agent_bus_teams"},
		{"agent_bus_teams:\n  - team: Alpha\n", "line 2: the team name does not match [a-z][a-z0-9-]{0,23}"},
		{item + "    port: 61x\n", "line 3: the port is not a number"},
		{item + "    state: gone\n", "line 3: state must be one of present, absent"},
		{item + "    state: !vault |\n", "the state on line 3 is tagged (vault-encrypted?); ccy-teams reads only plain values"},
		{"agent_bus_teams:\n  - team: !vault x\n", "the team on line 2 is tagged (vault-encrypted?); ccy-teams reads only plain values"},
	} {
		_, err := parse(c.text, teamsKey)
		if e, ok := asError(err); !ok || e.Kind != KindRefused || e.Message != c.msg {
			t.Errorf("%q: got %v, want %q", c.text, err, c.msg)
		}
	}
}

func TestParseAllowsATeamTwiceAmongLxcMembers(t *testing.T) {
	text := "agent_bus_lxc_members:\n  - team: alpha\n  - team: alpha\n"
	hv, err := parse(text, lxcKey)
	if err != nil || len(hv.teams) != 2 {
		t.Fatalf("%+v, %v", hv, err)
	}
}

func TestParseAddressIsPythonsCanonicalFormOrNone(t *testing.T) {
	for text, want := range map[string]string{
		"agent_bus_address: 2001:DB8::0:1\n":                 "2001:db8::1",
		"agent_bus_address:  '192.0.2.1' # c\n":              "192.0.2.1",
		"agent_bus_address: fe80::1%eth0\n":                  "fe80::1%eth0",
		"agent_bus_address: !vault |\n":                      "",
		"agent_bus_address: example.com\n":                   "",
		"agent_bus_address: 192.0.2.01\n":                    "",
		"  agent_bus_address: 192.0.2.1\n":                   "",
		"agent_bus_address \t: 192.0.2.1\n":                  "192.0.2.1",
		"# agent_bus_address: 192.0.2.1\n":                   "",
		"agent_bus_addresses: 192.0.2.1\n":                   "",
		"agent_bus_address: 192.0.2.1\r\n":                   "192.0.2.1",
		strings.Repeat("x: 1\n", 3) + "agent_bus_address:\n": "",
	} {
		hv, err := parse(text, teamsKey)
		if err != nil || hv.address != want {
			t.Errorf("%q: address %q, %v; want %q", text, hv.address, err, want)
		}
	}
}

func TestParseRefusesStructureBeforeTeams(t *testing.T) {
	// The second item lacks a team, but line 5's shape is judged first, as v1 judges it.
	text := "agent_bus_teams:\n  - team: Bad\n  - port: 1\n  - team: ok\n -x\n"
	_, err := parse(text, teamsKey)
	if err == nil || err.Error() != "line 5: not the agent_bus_teams shape ccy-teams edits (a block list of mappings)" {
		t.Fatalf("got %v", err)
	}
}

func TestFirstListen(t *testing.T) {
	for rest, want := range map[string]string{
		` ["192.0.2.1", "x"]`: "192.0.2.1", ` '["2001:db8::1"]'`: "2001:db8::1", ` []`: "", ` [1]`: "",
		` "192.0.2.1"`: "", ` [`: "", ` !vault x`: "", ` ["a\tb"]`: "a\tb",
	} {
		got, err := firstListen([]rawKey{{key: "listen", rest: rest}})
		if err != nil || got != want {
			t.Errorf("firstListen(%q) = %q, %v; want %q", rest, got, err, want)
		}
	}
	if got, err := firstListen(nil); err != nil || got != "" {
		t.Errorf("no listen: %q, %v", got, err)
	}
	_, err := firstListen([]rawKey{{key: "listen", rest: " [" + strings.Repeat("1", 4301) + "]"}})
	if e, ok := asError(err); !ok || e.Kind != KindCrash {
		t.Errorf("an int past python's digit limit is a ValueError json.loads raises: %v", err)
	}
}

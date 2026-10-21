package declare

import (
	"errors"
	"reflect"
	"testing"
)

const members = `agent_bus_lxc_members:
  - team: alpha
    container: web-1
    user: agent
    checkout: /srv/agent/proj
    host: Work Laptop
    seat: 2
    role: orchestrator
  - team: alpha
    container: web-2
    user: agent
    repo: Proj.Name
    state: absent
  - team: beta
    container: web-1
    user: agent
    checkout: "/srv/agent/proj"
    human_text: false
`

func TestLxcMembers(t *testing.T) {
	got, err := HostVars(members).LxcMembers()
	if err != nil {
		t.Fatal(err)
	}
	want := []LxcMember{
		{Team: "alpha", Container: "web-1", User: "agent", Host: "work-laptop", Role: "coordinator", State: "present", Checkout: "/srv/agent/proj", Seat: "2"},
		{Team: "alpha", Container: "web-2", User: "agent", Role: "worker", State: "absent"},
		{Team: "beta", Container: "web-1", User: "agent", Role: "worker", State: "present", Checkout: "/srv/agent/proj"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestLxcMembersRefusals(t *testing.T) {
	head := "agent_bus_lxc_members:\n  - team: alpha\n    container: c\n    user: u\n"
	entry := func(extra string) string { return head + "    checkout: /x\n" + extra }
	for _, c := range []struct{ text, msg string }{
		{head, "agent_bus_lxc_members entry 1: a present member needs checkout, where suggest-handle runs"},
		{"agent_bus_lxc_members:\n  - team: alpha\n    port: 1\n", "agent_bus_lxc_members entry 1: missing container, user; unknown port"},
		{entry("    zed: 1\n    listen: []\n"), "agent_bus_lxc_members entry 1: missing nothing; unknown listen, zed"},
		{entry("    state: \"gone\"\n"), "line 6: state must be one of present, absent"},
		{entry("    state: '\"absent\"'\n"), "line 6: state must be one of present, absent"},
		{"agent_bus_lxc_members:\n  - team: \"alpha\"\n    container: c\n    user: u\n    checkout: /x\n    state: [\"absent\"]\n", "line 6: state must be one of present, absent"},
		{"agent_bus_lxc_members:\n  - team: alpha\n    container: -c\n    user: u\n", "agent_bus_lxc_members entry 1: container must be an LXC container name"},
		{"agent_bus_lxc_members:\n  - team: alpha\n    container: c\n    user: U\n", "agent_bus_lxc_members entry 1: user must be a user name"},
		{entry("    host: 5\n"), "agent_bus_lxc_members entry 1: host must be a string"},
		{entry("    host: \"\"\n"), "agent_bus_lxc_members entry 1: host: no role is set for this install: pass --host"},
		{entry("    host: -bad-\n"), "agent_bus_lxc_members entry 1: host: host '-bad-' does not fit [a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])? once normalised"},
		{head + "    checkout: x\n", "agent_bus_lxc_members entry 1: checkout must be an absolute path inside the container"},
		{entry("    repo: 5\n"), "agent_bus_lxc_members entry 1: repo must be a string"},
		{entry("    repo: '__'\n"), "agent_bus_lxc_members entry 1: repo: repository name '__' is empty once normalised"},
		{head + "    state: absent\n", "agent_bus_lxc_members entry 1: an absent member needs checkout or repo, to know its handle"},
		{entry("    seat: 0\n"), "agent_bus_lxc_members entry 1: seat must match [1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}"},
		{entry("    seat: true\n"), "agent_bus_lxc_members entry 1: seat must match [1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}"},
		{entry("    role: boss\n"), "agent_bus_lxc_members entry 1: role must be one of coordinator, worker"},
		{entry("    human_text: \"no\"\n"), "agent_bus_lxc_members entry 1: human_text must be true or false"},
		{entry("    service: 1\n"), "agent_bus_lxc_members entry 1: service must be true or false"},
		{entry("    role: [x\n"), "alpha's role on line 6 is a flow value ccy-teams cannot read (write it as JSON)"},
		{entry("    user: v\n"), "line 6: user is set twice in one agent_bus_lxc_members item"},
		{entry("") + "  - team: alpha\n    container: c\n    user: u\n    checkout: /y\n", "agent_bus_lxc_members entries 1 and 2 both declare u@c in team alpha"},
		{entry("    host: a\n") + "  - team: beta\n    container: c\n    user: u\n    checkout: /x\n    host: b\n", "agent_bus_lxc_members gives u@c two hosts; its env file sets one HOOKS_DAEMON_HOSTNAME"},
		{entry("    host: a\n    seat: 1\n") + "  - team: alpha\n    container: d\n    user: u\n    checkout: /x\n    host: a\n    seat: 1\n", "agent_bus_lxc_members gives seat 1 of team alpha to two containers; set seat on one"},
		{entry("    role: coordinator\n") + "  - team: alpha\n    container: d\n    user: u\n    checkout: /x\n    role: orchestrator\n", "agent_bus_lxc_members entries 1 and 2 both declare the coordinator of team alpha; a team has one"},
		{entry("    service: true\n") + "  - team: beta\n    container: c\n    user: u\n    checkout: /x\n", "agent_bus_lxc_members: the entries of u@c differ on service; the user runs one session for all its teams, so set it alike on each"},
		{entry("    service: true\n") + "  - team: beta\n    container: c\n    user: u\n    checkout: /y\n    service: true\n", "agent_bus_lxc_members: the entries of u@c with service: true name two checkouts; its one supervised session runs in one checkout"},
	} {
		_, err := HostVars(c.text).LxcMembers()
		if e, ok := asError(err); !ok || e.Kind != KindRefused || e.Message != c.msg {
			t.Errorf("%q:\n got  %v\n want %q", c.text, err, c.msg)
		}
	}
}

func TestLxcSeatsClashOnlyInOneRepo(t *testing.T) {
	two := func(repoA, repoB string) string {
		return "agent_bus_lxc_members:\n" +
			"  - team: alpha\n    container: c\n    user: u\n    checkout: /x\n    host: h\n    seat: 1\n" + repoA +
			"  - team: alpha\n    container: d\n    user: u\n    checkout: /x\n    host: h\n    seat: 1\n" + repoB
	}
	if _, err := HostVars(two("    repo: one\n", "    repo: two\n")).LxcMembers(); err != nil {
		t.Errorf("seat 1 in two repos: %v", err)
	}
	if _, err := HostVars(two("    repo: one\n", "    repo: One\n")).LxcMembers(); err == nil {
		t.Error("seat 1 twice in one repo is refused")
	}
	if _, err := HostVars(two("    repo: one\n", "")).LxcMembers(); err == nil {
		t.Error("an unknown repo may be the same one")
	}
}

func TestLxcMembersCrashWherePrintingWould(t *testing.T) {
	text := "agent_bus_lxc_members:\n  - team: alpha\n    container: c\n    user: u\n    checkout: \"/x\\ud800\"\n"
	if _, err := HostVars(text).LxcMembers(); !errors.Is(err, ErrCrash) {
		t.Fatalf("got %v", err)
	}
}

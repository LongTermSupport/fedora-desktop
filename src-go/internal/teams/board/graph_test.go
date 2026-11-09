package board

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// graphTeam is a team whose name, description and person carry spaces and punctuation Mermaid
// would read as syntax, so the golden proves ids are safe and labels quoted.
const graphTeam = `team a/b "x"`

// graphSnapshot is one team with every kind of member the graph draws, each live state from the
// bus (its LIVE line): a coordinator seat listening, a worker seat whose status lapsed, a free seat
// with no status, an LXC member whose status lapsed, a parked one, a deferred one with no handle
// yet, a member of another machine that is listening, and a person.
func graphSnapshot() *Snapshot {
	s := NewSnapshot()
	s.Teams = []string{graphTeam}
	s.Status[graphTeam] = declare.Status{Team: graphTeam, State: declare.ApplyCurrent}
	s.Live["ccy-proj"] = 1
	s.Members[graphTeam] = agentbus.Roster{}
	s.LXC[graphTeam] = lxcrecord.Record{Version: 1, Team: graphTeam, Members: []lxcrecord.Item{
		{Container: "box one", User: "agent", Status: lxcrecord.StatusStopped, Handle: "site.1+local.lxc", Checkout: "/srv/site", Why: "stopped"},
		{Container: "box one", User: "ops", Status: lxcrecord.StatusJoined, Handle: "site.2+local.lxc", Checkout: "/srv/site2"},
		{Container: "box-2", User: "agent", State: "present", Status: lxcrecord.StatusDeferred, Checkout: "/srv/c", Why: "LXC container box-2 is STOPPED: start it"},
	}}
	seat := func(member, handle, role, record string, live agentbus.Liveness) Row {
		session := record
		if session == "" {
			session = "free"
		}
		return Row{
			Team: graphTeam, Member: member, Kind: KindCcy, State: liveWord(live), Applied: "-", Session: session, Restore: "-",
			Where: "~/proj", Handle: handle, Dir: "/home/user/proj", Record: record, Role: role, Live: live,
		}
	}
	s.Rows = []Row{
		{
			Team: graphTeam, Member: `Does "things" & <stuff> #1`, Kind: KindTeam, State: "up", Applied: "up to date",
			Session: "-", Restore: "-", Where: "port 6167", Handle: "-", Dir: "-",
		},
		seat("dev", "proj.dev+local.podman", agentbus.RoleCoordinator, "ccy-proj", agentbus.LiveListening),
		seat("1", "proj.1+local.podman", agentbus.RoleWorker, "ccy-old", agentbus.LiveStale),
		seat("2", "proj.2+local.podman", agentbus.RoleWorker, "", agentbus.LiveNoStatus),
		{Team: graphTeam, Member: "site.1+local.lxc", Kind: "lxc", State: "stale", Handle: "site.1+local.lxc", Dir: "-", Role: agentbus.RoleWorker, Live: agentbus.LiveStale},
		{Team: graphTeam, Member: "site.2+local.lxc", Kind: "lxc", State: "parked", Handle: "site.2+local.lxc", Dir: "-", Role: agentbus.RoleWorker, Live: agentbus.LiveStale},
		{Team: graphTeam, Member: "agent@box-2", Kind: "lxc", State: lxcrecord.StatusDeferred, Handle: "-", Dir: "-"},
		{Team: graphTeam, Member: "web.1+other.vm", Kind: "vm", State: "listening", Handle: "web.1+other.vm", Dir: "-", Role: agentbus.RoleWorker, Live: agentbus.LiveListening},
		{Team: graphTeam, Member: `Ann "Q" O'Neil`, Kind: KindHuman, State: "in room", Session: "1 signed in", Handle: "-", Dir: "-"},
	}
	return s
}

const graphGolden = `flowchart TB
  classDef listening fill:#dff5e1,stroke:#2e7d32,color:#000
  classDef offline fill:#eeeeee,stroke:#757575,color:#000
  classDef unknown fill:#fff3c4,stroke:#b8860b,color:#000
  subgraph t0_team_a_b__x_["team team a/b #quot;x#quot;: Does #quot;things#quot; #amp; #lt;stuff#gt; #35;1<br/>homeserver up, applied: up to date<br/>port 6167"]
    direction TB
    t0_h0["person Ann #quot;Q#quot; O'Neil<br/>in room, 1 signed in"]
    t0_m0["proj.dev+local.podman (coordinator)<br/>kind ccy, seat dev, role coordinator<br/>checkout ~/proj<br/>listening: a session of it is listening on the bus<br/>session ccy-proj"]:::listening
    t0_m1["proj.1+local.podman<br/>kind ccy, seat 1, role worker<br/>checkout ~/proj<br/>offline: its status on the bus has lapsed (stale)<br/>session ccy-old"]:::offline
    t0_m2["proj.2+local.podman<br/>kind ccy, seat 2, role worker<br/>checkout ~/proj<br/>offline: no status on the bus: no session of it has listened<br/>session free"]:::offline
    subgraph t0_c0["LXC container box one"]
      direction TB
      t0_m3["site.1+local.lxc<br/>kind lxc, seat 1, role worker<br/>user agent, checkout /srv/site in the container<br/>offline: its status on the bus has lapsed (stale)"]:::offline
      t0_m4["site.2+local.lxc<br/>kind lxc, seat 2, role worker<br/>user ops, checkout /srv/site2 in the container<br/>offline: parked"]:::offline
    end
    subgraph t0_c1["LXC container box-2"]
      direction TB
      t0_m5["agent@box-2<br/>kind lxc, seat -, role -<br/>user agent, checkout /srv/c in the container<br/>offline: deferred: LXC container box-2 is STOPPED: start it"]:::offline
    end
    t0_m6["web.1+other.vm<br/>kind vm, seat 1, role worker<br/>checkout not known here<br/>listening: a session of it is listening on the bus"]:::listening
    t0_m0 -->|dispatches to| t0_m1
    t0_m0 -->|dispatches to| t0_m2
    t0_m0 -->|dispatches to| t0_m3
    t0_m0 -->|dispatches to| t0_m4
    t0_m0 -->|dispatches to| t0_m6
  end
`

func TestMermaidDrawsEveryTeamMemberAndPersonWithSafeIdsAndQuotedLabels(t *testing.T) {
	if got := Mermaid(graphSnapshot()); got != graphGolden {
		t.Fatalf("got:\n%s\nwant:\n%s", got, graphGolden)
	}
}

// A member agent-bus list gave no LIVE line for (an agent-bus older than LIVE lines), or that it
// does not name at all, is unknown, with why, never left out and never called listening.
func TestMermaidShowsAnUnreportedMemberAsUnknownWithWhy(t *testing.T) {
	s := graphSnapshot()
	s.Members[graphTeam] = agentbus.Roster{Members: []agentbus.Member{{Handle: "site.1+local.lxc", Role: agentbus.RoleWorker}}}
	s.Rows[4].State, s.Rows[4].Live = declare.WordUnknown, agentbus.LiveUnreported
	s.Rows[1].State, s.Rows[1].Live = declare.WordUnknown, agentbus.LiveUnreported
	got := Mermaid(s)
	for _, want := range []string{
		`user agent, checkout /srv/site in the container<br/>unknown: agent-bus list reports no liveness (the agent-bus play updates it)"]:::unknown`,
		`checkout ~/proj<br/>unknown: agent-bus list names no member proj.dev+local.podman<br/>session ccy-proj"]:::unknown`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// A team whose members could not be read keeps its seats, each with its session's state, its
// role unknown, and the reason on the team.
func TestMermaidWithMembersUnreadableSaysWhy(t *testing.T) {
	s := graphSnapshot()
	s.MembersUnknown[graphTeam] = WordNeedSudo
	s.Rows[0].Where = "port 6167, members: need sudo"
	s.Rows[1].Role, s.Rows[1].State, s.Rows[1].Live = "", WordNeedSudo, agentbus.LiveUnreported
	got := Mermaid(s)
	for _, want := range []string{
		"<br/>port 6167, members: need sudo\"]",
		`t0_m0["proj.dev+local.podman<br/>kind ccy, seat dev, role unknown (need sudo)<br/>checkout ~/proj<br/>unknown: members: need sudo<br/>session ccy-proj"]:::unknown`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// Ids are numbered by place, so two names that slug alike never share an id, and "end" (a
// Mermaid keyword) is never an id on its own.
func TestMermaidIdsNeverCollide(t *testing.T) {
	s := NewSnapshot()
	s.Teams = []string{"a b", "a-b", "end"}
	for _, team := range s.Teams {
		s.Rows = append(s.Rows, Row{Team: team, Member: NoDescription, Kind: KindTeam, State: "up", Where: "port 1"})
	}
	got := Mermaid(s)
	for _, want := range []string{"subgraph t0_a_b[", "subgraph t1_a_b[", "subgraph t2_end["} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestGraphPrintsTheFlowchartOfTheMachine(t *testing.T) {
	h := populated(t)
	if code := h.c.Graph(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	out := h.Out.String()
	for _, want := range []string{
		"flowchart TB\n",
		`subgraph t0_alpha["team alpha: (no description)<br/>homeserver up, applied: up to date<br/>port 6167"]`,
		"proj.1+local.podman<br/>kind ccy, seat 1, role dev<br/>checkout ",
		"listening: a session of it is listening on the bus<br/>session ccy-proj",
		"person owner<br/>in room, never signed in",
		"proj.box+role.lxc<br/>kind lxc, seat box, role ops<br/>checkout not known here<br/>offline: no status on the bus: no session of it has listened",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if h.ErrOut.String() != "" {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

// As --list: members that need sudo are drawn as unknown, the reason said on stderr, and the
// run fails after printing; sudo -v is never run.
func TestGraphNeedingSudoDrawsTheRestAndFails(t *testing.T) {
	h := populated(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	if code := h.c.Graph(t.Context()); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "The members of alpha are not shown: reading them needs root.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if !strings.Contains(h.Out.String(), "members: need sudo") || !strings.Contains(h.Out.String(), "role unknown (need sudo)") {
		t.Fatalf("stdout %q", h.Out.String())
	}
	for _, c := range h.Fake.Calls() {
		if c.Line() == "sudo -v" {
			t.Fatal("--graph never runs sudo -v")
		}
	}
}

func TestGraphWithNoTeamsPrintsNothing(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "")
	h.OnDeclare("status", "")
	h.On("systemctl list-units", "", 0)
	if code := h.c.Graph(t.Context()); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if want := "No agent teams on this machine yet. ccy-teams create starts one.\n"; h.ErrOut.String() != want || h.Out.String() != "" {
		t.Fatalf("stdout %q stderr %q", h.Out.String(), h.ErrOut.String())
	}
}

func TestGraphCollectionFailureExitsOne(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "agent_bus_teams:\n  - team: alpha\n    port: port\n")
	if code := h.c.Graph(t.Context()); code != term.ExitFailure || h.Out.String() != "" {
		t.Fatalf("exit %d, stdout %q", code, h.Out.String())
	}
}

func TestGraphWriteFailureIsAnError(t *testing.T) {
	h := populated(t)
	h.c.Stdout = failingWriter{}
	if code := h.c.Graph(t.Context()); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

// A cc session seat (a host seat, KIND cc from its handle) is drawn as a seat, as a ccy one is:
// its own seat and its session line.
func TestMermaidDrawsACcSeatWithItsSession(t *testing.T) {
	s := graphSnapshot()
	s.Rows = append(s.Rows, Row{
		Team: graphTeam, Member: "3", Kind: KindOf("proj.3+local.host"), State: "listening", Applied: "-", Session: "cc-proj", Restore: "-",
		Where: "~/proj", Handle: "proj.3+local.host", Dir: "/home/user/proj", Record: "cc-proj", Role: agentbus.RoleWorker, Live: agentbus.LiveListening,
	})
	got := Mermaid(s)
	want := `proj.3+local.host<br/>kind cc, seat 3, role worker<br/>checkout ~/proj<br/>`
	if !strings.Contains(got, want) || !strings.Contains(got, `<br/>session cc-proj"]`) {
		t.Fatalf("missing the cc seat %q with its session line in:\n%s", want, got)
	}
}

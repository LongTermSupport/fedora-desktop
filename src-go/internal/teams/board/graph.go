package board

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Graph is ccy-teams --graph: the same pass as --list, drawn as a Mermaid flowchart on stdout
// (Mermaid). It fails as --list does: a part that could not be read is drawn with why, and the
// run exits 1 after printing; it never runs sudo -v.
func (c *Collector) Graph(ctx context.Context) term.ExitCode {
	s, err := c.Collect(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if !c.sayUnshown(s) {
		return term.ExitOK
	}
	if _, err := fmt.Fprint(c.Stdout, Mermaid(s)); err != nil {
		c.Term.PrintError("the graph could not be written: " + err.Error())
		return term.ExitFailure
	}
	return verdict(s)
}

// The live states a member is drawn in, each a Mermaid class: listening on the bus (its LIVE
// line), offline (stale, no status, parked, deferred), or unknown with why.
const (
	stateListening = string(agentbus.LiveListening)
	stateOffline   = "offline"
	stateUnknown   = "unknown"
)

// classDefs colour the live states.
const classDefs = "  classDef " + stateListening + " fill:#dff5e1,stroke:#2e7d32,color:#000\n" +
	"  classDef " + stateOffline + " fill:#eeeeee,stroke:#757575,color:#000\n" +
	"  classDef " + stateUnknown + " fill:#fff3c4,stroke:#b8860b,color:#000\n"

// Mermaid is the snapshot as a Mermaid flowchart: each team a subgraph titled with what it is
// for and its homeserver, holding its people and its members; each member's kind, seat, role,
// checkout and live state (stateOf), LXC members inside a subgraph of their container, and the
// coordinator's edges to the workers it dispatches to. Every row of the snapshot is drawn, so
// nothing --list shows is left out. Ids are numbered by place ([A-Za-z0-9_] only, never a
// keyword) and every label is quoted and escaped (label).
func Mermaid(s *Snapshot) string {
	var b strings.Builder
	b.WriteString("flowchart TB\n" + classDefs)
	for i, team := range s.Teams {
		(&teamGraph{s: s, team: team, id: "t" + strconv.Itoa(i), b: &b}).draw(slug(team))
	}
	return b.String()
}

// teamGraph draws one team's subgraph.
type teamGraph struct {
	s     *Snapshot
	team  string
	id    string
	b     *strings.Builder
	nodes int
	boxes int
}

func (g *teamGraph) draw(slugged string) {
	var members []Row
	var humans []Row
	var title Row
	for _, r := range g.s.Rows {
		switch {
		case r.Team != g.team:
		case r.Kind == KindTeam:
			title = r
		case r.Kind == KindHuman:
			humans = append(humans, r)
		default:
			members = append(members, r)
		}
	}
	g.line(1, "subgraph "+g.id+"_"+slugged+label("team "+g.team+": "+title.Member,
		"homeserver "+title.State+", applied: "+title.Applied, title.Where))
	g.line(2, "direction TB")
	for i, h := range humans {
		g.line(2, g.id+"_h"+strconv.Itoa(i)+label("person "+h.Member, h.State+", "+h.Session))
	}
	ids := g.members(members)
	g.edges(members, ids)
	g.line(1, "end")
}

// members draws every member, an LXC container's all together where its first one comes, and
// returns each member's id by its index in members.
func (g *teamGraph) members(members []Row) []string {
	ids := make([]string, len(members))
	drawn := map[string]bool{}
	for i, r := range members {
		item, inRecord := g.lxcItem(r)
		switch {
		case !inRecord:
			ids[i] = g.node(2, r, nil)
		case !drawn[item.Container]:
			drawn[item.Container] = true
			g.box(item.Container, members, ids)
		}
	}
	return ids
}

// box draws the subgraph of an LXC container with each of its members.
func (g *teamGraph) box(container string, members []Row, ids []string) {
	g.line(2, "subgraph "+g.id+"_c"+strconv.Itoa(g.boxes)+label("LXC container "+container))
	g.boxes++
	g.line(3, "direction TB")
	for i, r := range members {
		if item, ok := g.lxcItem(r); ok && item.Container == container {
			ids[i] = g.node(3, r, &item)
		}
	}
	g.line(2, "end")
}

// node draws one member and returns its id.
func (g *teamGraph) node(depth int, r Row, item *lxcrecord.Item) string {
	id := g.id + "_m" + strconv.Itoa(g.nodes)
	g.nodes++
	name := r.Label()
	if r.Handle == "" || r.Handle == "-" {
		name = r.Crumb()
	}
	where := "checkout not known here"
	switch {
	case item != nil:
		where = "user " + item.User + ", checkout " + item.Checkout + " in the container"
	case r.Dir != "-" && r.Dir != "":
		where = "checkout " + r.Where
	}
	state, why := stateOf(g.s, r, item)
	lines := []string{name, "kind " + r.kindWord() + ", seat " + g.seatOf(r) + ", role " + g.roleOf(r), where, state + ": " + why}
	if r.Kind == KindCcy && r.Dir != "-" && r.Dir != "" {
		lines = append(lines, "session "+r.Session)
	}
	g.line(depth, id+label(lines...)+":::"+state)
	return id
}

// edges draws the coordinator's edge to each other member with a handle.
func (g *teamGraph) edges(members []Row, ids []string) {
	from := -1
	for i, r := range members {
		if r.Role == agentbus.RoleCoordinator {
			from = i
			break
		}
	}
	if from < 0 {
		return
	}
	for i, r := range members {
		if i != from && r.Handle != "" && r.Handle != "-" {
			g.line(2, ids[from]+" -->|dispatches to| "+ids[i])
		}
	}
}

func (g *teamGraph) line(depth int, text string) {
	g.b.WriteString(strings.Repeat("  ", depth) + text + "\n")
}

// lxcItem is the LXC member record's item for an LXC member's row: by its handle, or, for one
// deferred before it ever joined, by user@container.
func (g *teamGraph) lxcItem(r Row) (lxcrecord.Item, bool) {
	if r.Kind != kindLxc {
		return lxcrecord.Item{}, false
	}
	for _, item := range g.s.LXC[g.team].Members {
		if r.Handle != "-" && r.Handle != "" && item.Handle == r.Handle {
			return item, true
		}
		if (r.Handle == "-" || r.Handle == "") && item.Handle == "" && item.Label() == r.Member {
			return item, true
		}
	}
	return lxcrecord.Item{}, false
}

// seatOf is a seat row's seat, or the seat in a member's handle (<repo>.<seat>+<host>.<type>).
func (g *teamGraph) seatOf(r Row) string {
	if r.Kind == KindCcy && r.Dir != "-" && r.Dir != "" {
		return r.Member
	}
	_, rest, ok := strings.Cut(r.Handle, ".")
	seat, _, plus := strings.Cut(rest, "+")
	if !ok || !plus || seat == "" {
		return "-"
	}
	return seat
}

// roleOf is the member's role, unknown with why when the team's members could not be read.
func (g *teamGraph) roleOf(r Row) string {
	switch {
	case r.Role != "":
		return r.Role
	case g.s.MembersUnknown[g.team] != "":
		return "unknown (" + g.s.MembersUnknown[g.team] + ")"
	}
	return "-"
}

// stateOf is a member's live state and why, as --list's STATE says it: listening while its status
// on the bus runs past now (its LIVE line, what pingbus status reads), offline once that has lapsed
// or with none, and offline when parked or deferred. Whatever its kind or machine, a member is
// judged the same way; one agent-bus list did not report is unknown, with why.
func stateOf(s *Snapshot, r Row, item *lxcrecord.Item) (string, string) {
	switch {
	case r.State == wordParked:
		return stateOffline, wordParked
	case r.State == lxcrecord.StatusDeferred && item != nil:
		return stateOffline, item.Words()
	case r.State == lxcrecord.StatusDeferred:
		return stateOffline, lxcrecord.StatusDeferred
	}
	switch r.Live {
	case agentbus.LiveListening:
		return stateListening, "a session of it is listening on the bus"
	case agentbus.LiveStale:
		return stateOffline, "its status on the bus has lapsed (stale)"
	case agentbus.LiveNoStatus:
		return stateOffline, "no status on the bus: no session of it has listened"
	case agentbus.LiveUnreported:
	}
	switch why := s.MembersUnknown[r.Team]; {
	case why != "":
		return stateUnknown, "members: " + why
	case r.Handle == "" || r.Handle == "-":
		return stateUnknown, "no member yet"
	}
	if _, listed := s.Members[r.Team].Member(r.Handle); !listed {
		return stateUnknown, "agent-bus list names no member " + r.Handle
	}
	return stateUnknown, "agent-bus list reports no liveness (the agent-bus play updates it)"
}

// escapes are the characters a quoted Mermaid label would read as syntax, as Mermaid's entity
// codes; a line break in a value would end the statement.
var escapes = strings.NewReplacer("#", "#35;", `"`, "#quot;", "&", "#amp;", "<", "#lt;", ">", "#gt;", "`", "#96;",
	"\n", " ", "\r", " ", "\t", " ")

// label is a node's text in quotes, each line escaped, joined by Mermaid's line break.
func label(lines ...string) string {
	for i, l := range lines {
		lines[i] = escapes.Replace(l)
	}
	return `["` + strings.Join(lines, "<br/>") + `"]`
}

// slug is name with every character outside [A-Za-z0-9] made "_": an id's readable part, made
// unique by the place number before it.
func slug(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
}

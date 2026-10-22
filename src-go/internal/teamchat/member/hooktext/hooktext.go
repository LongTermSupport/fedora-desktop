// Package hooktext is every text a teamchat hook answers with (Plan 00209 B 2.8, SPEC-n3
// HK1): fixed templates whose fills are counts and grammar-checked names only (H14), and the
// three answer shapes Claude Code reads, each one compact all-ASCII JSON object. A fill that
// fails its grammar shows as "?", so nothing a sender or a file wrote reaches a session's
// context through a hook. The link builds its answers from it, and the hook its own when the
// link does not answer; it imports neither.
package hooktext

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// template is a text with one slot per fill.
type template string

const slot = "{}"

// unshown is a fill that failed its grammar.
const unshown = "?"

// The texts. Each names a teamchat command as `teamchat <verb>` with its flags as B 2.2.1
// spells them.
const (
	tmplTeam              template = "team {}: you are {}, the {} (coordinator: {})."
	tmplTeamNoCoordinator template = "team {}: you are {}, the {}; the team has no coordinator now."
	tmplRoleCoordinator   template = "In team {} you direct the team's agents; ask the humans with teamchat ask --team {}, and follow up with teamchat questions --team {}."
	tmplRoleWorker        template = "In team {} take direction from the team's humans and its coordinator; report with teamchat report --team {}."
	tmplRoleProbe         template = "In team {} this session is the team's acceptance probe."
	tmplModeSocket        template = "Deliveries arrive in this conversation by themselves."
	tmplModeWait          template = "Keep teamchat wait running in the background: deliveries reach this session only through it."
	tmplModeNone          template = "Deliveries do not arrive by themselves in this session: run teamchat recv to read them."
	tmplBreakdown         template = "{} from humans, {} from agents, {} pings, {} notices, {} timeouts"
	tmplPending           template = "teamchat: {} pending ({}): run teamchat recv"
	tmplMissing           template = "teamchat: a delivery did not reach the conversation; {} items are pending again: run teamchat recv"
	tmplMissingOne        template = "teamchat: a delivery did not reach the conversation; 1 item is pending again: run teamchat recv"
	tmplStalled           template = "teamchat: deliveries have failed for 2 minutes; {} pending ({}): run teamchat recv"
	tmplStopPending       template = "{} pending ({}). Run teamchat recv before stopping."
	tmplStopNoWaker       template = "Delivery here is by teamchat wait, and none is running: start teamchat wait in the background before stopping."
	tmplTeamFailed        template = "teamchat: team {} stopped ({}); run teamchat status --team {} and tell a human"
	tmplTeamDown          template = "teamchat: team {} cannot reach its server; its messages wait there"
	tmplDesiredRefused    template = "teamchat: this session's membership file could not be read ({}); the last good membership stays; tell a human"
	tmplAddRefused        template = "teamchat: team {}'s seat is held by another session; tell a human"
	tmplRecoveryUnknown   template = "teamchat's link does not answer and why could not be read; tell a human"
)

// maxRecovery bounds a recovery message, which is the verbs' own exit 69 text.
const maxRecovery = 512

// roleNone is the frame's word for a role outside the roster's set.
const roleNone = "member"

var codeGrammar = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// fill is t with fills in its slots; a count of fills that differs from its slots is a
// mistake in this package, so it panics.
func fill(t template, fills ...string) string {
	parts := strings.Split(string(t), slot)
	if len(parts) != len(fills)+1 {
		panic("hooktext: template " + strconv.Quote(string(t)) + " is given " + strconv.Itoa(len(fills)) + " fills")
	}
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(p)
		if i < len(fills) {
			b.WriteString(fills[i])
		}
	}
	return b.String()
}

func count(n int64) string { return strconv.FormatInt(n, 10) }

// handle is a roster handle, full or short, else unshown.
func handle(s string) string {
	if _, ok := proto.ParseHandle(s); ok || proto.ValidShortName(s) {
		return s
	}
	return unshown
}

// oneOf is s when it is in set, else unshown.
func oneOf(s string, set ...string) string {
	if slices.Contains(set, s) {
		return s
	}
	return unshown
}

// Answer is one hook answer: a compact JSON object, all ASCII, without its newline.
type Answer struct{ Line []byte }

// Empty is {}: nothing to tell.
func Empty() Answer { return Answer{Line: []byte("{}")} }

// Context is text as additional context for event, SessionStart or UserPromptSubmit, the two
// events that take it; any other event, or no text, is Empty.
func Context(event, text string) Answer {
	if event != "SessionStart" && event != "UserPromptSubmit" || text == "" {
		return Empty()
	}
	return Answer{Line: []byte(`{"hookSpecificOutput":{"hookEventName":` + proto.EscapeASCII(event) +
		`,"additionalContext":` + proto.EscapeASCII(text) + `}}`)}
}

// Block is Stop's block with reason; no reason is Empty.
func Block(reason string) Answer {
	if reason == "" {
		return Empty()
	}
	return Answer{Line: []byte(`{"decision":"block","reason":` + proto.EscapeASCII(reason) + `}`)}
}

// FrameTeam is one team's block of the frame: the session's handle and role in it, the
// coordinator's handle ("" for none) and the team's description.
type FrameTeam struct {
	Team        paths.TeamID
	Handle      string
	Role        string
	Coordinator string
	Description string
}

// Frame is SessionStart's text: A 13.5's frame, each team's block (who the session is, the
// coordinator, the description, and how its role works), and the delivery mode's line.
func Frame(teams []FrameTeam, mode string) string {
	lines := []string{proto.FrameText, ""}
	for _, t := range teams {
		lines = append(lines, teamLines(t)...)
	}
	switch mode {
	case state.DeliverySocket:
		lines = append(lines, string(tmplModeSocket))
	case state.DeliveryWait:
		lines = append(lines, string(tmplModeWait))
	default:
		lines = append(lines, string(tmplModeNone))
	}
	return strings.Join(lines, "\n")
}

func teamLines(t FrameTeam) []string {
	id := t.Team.String()
	role := oneOf(t.Role, proto.RoleCoordinator, proto.RoleWorker, proto.RoleProbe)
	if role == unshown {
		role = roleNone
	}
	head := fill(tmplTeamNoCoordinator, id, handle(t.Handle), role)
	if t.Coordinator != "" {
		head = fill(tmplTeam, id, handle(t.Handle), role, handle(t.Coordinator))
	}
	if proto.ValidDescription(t.Description) && t.Description != "" {
		head += " " + t.Description
	}
	lines := []string{head}
	switch role {
	case proto.RoleCoordinator:
		lines = append(lines, fill(tmplRoleCoordinator, id, id, id))
	case proto.RoleWorker:
		lines = append(lines, fill(tmplRoleWorker, id, id))
	case proto.RoleProbe:
		lines = append(lines, fill(tmplRoleProbe, id))
	}
	return lines
}

// Counts are pending items by kind: humans' HUMAN, agents' PEER, PING, NOTICE and TIMEOUT.
type Counts struct {
	Total    int64
	Humans   int64
	Agents   int64
	Pings    int64
	Notices  int64
	Timeouts int64
}

func breakdown(c Counts) string {
	return fill(tmplBreakdown, count(c.Humans), count(c.Agents), count(c.Pings), count(c.Notices), count(c.Timeouts))
}

// Pending is the prompt's pending line, when delivery is not live or items are held as
// missing.
func Pending(c Counts) string { return fill(tmplPending, count(c.Total), breakdown(c)) }

// Missing is B 2.7.4's line for a delivery that did not reach the conversation: n items are
// pending again.
func Missing(n int64) string {
	if n == 1 {
		return string(tmplMissingOne)
	}
	return fill(tmplMissing, count(n))
}

// Stalled is the prompt's line while every delivery write has failed for 2 minutes.
func Stalled(c Counts) string { return fill(tmplStalled, count(c.Total), breakdown(c)) }

// StopPending is Stop's block for items that will not be pushed.
func StopPending(c Counts) string { return fill(tmplStopPending, count(c.Total), breakdown(c)) }

// StopNoWaker is Stop's block for a socketless session with no wait running.
func StopNoWaker() string { return string(tmplStopNoWaker) }

// TeamFailed is the line for a team stopped by its server's refusal (status.json's failed).
func TeamFailed(t paths.TeamID, failed string) string {
	why := oneOf(failed, state.FailedUnauthorized, state.FailedForbidden, state.FailedTrust, state.FailedBundle)
	return fill(tmplTeamFailed, t.String(), why, t.String())
}

// TeamDown is the line for a team whose server cannot be reached (OF1: shown, never a block).
func TeamDown(t paths.TeamID) string { return fill(tmplTeamDown, t.String()) }

// DesiredRefused is the line for a membership file the link refused, with its error code.
func DesiredRefused(code string) string {
	if !codeGrammar.MatchString(code) || len(code) > 32 {
		code = unshown
	}
	return fill(tmplDesiredRefused, code)
}

// AddRefused is the line for a team whose seat another session holds.
func AddRefused(t paths.TeamID) string { return fill(tmplAddRefused, t.String()) }

// Membership is the MEMBERSHIP notice's lines as the prompt's fallback (DV6): each line kept
// only when it holds no control character.
func Membership(lines []string) string {
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if clean(l) {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

// Recovery is the hook's own line when the link does not answer: message, the verbs' exit 69
// text for the reading of keeper.json and keeper.lock, when it is one clean line.
func Recovery(message string) string {
	if !clean(message) || len(message) > maxRecovery {
		return string(tmplRecoveryUnknown)
	}
	return message
}

// clean is one non-empty line of UTF-8 with no control character.
func clean(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

// All is every text filled from fixed samples, for the tests that check every agent-facing
// text (PK2).
func All() []string {
	alpha, beta, gamma := sampleTeam("alpha"), sampleTeam("beta"), sampleTeam("gamma")
	const coordinator = "repo.1+desk.podman"
	c := Counts{Total: 6, Humans: 1, Agents: 2, Pings: 1, Notices: 1, Timeouts: 1}
	teams := []FrameTeam{
		{Team: alpha, Handle: coordinator, Role: proto.RoleCoordinator, Coordinator: coordinator, Description: "The sample team."},
		{Team: beta, Handle: "repo.2+desk.podman", Role: proto.RoleWorker},
		{Team: gamma, Handle: "probe.1+desk.podman", Role: proto.RoleProbe, Coordinator: coordinator},
	}
	return []string{
		Frame(teams, state.DeliverySocket), Frame(teams[1:2], state.DeliveryWait), Frame(nil, state.DeliveryNone),
		Pending(c), Missing(1), Missing(3), Stalled(c), StopPending(c), StopNoWaker(),
		TeamFailed(alpha, state.FailedUnauthorized), TeamDown(beta), DesiredRefused("schema"), AddRefused(gamma),
		Membership([]string{"MEMBERSHIP sample"}), Recovery(""),
	}
}

func sampleTeam(s string) paths.TeamID {
	t, err := paths.ParseTeamID(s)
	if err != nil {
		panic("hooktext: sample team " + s + ": " + err.Error())
	}
	return t
}

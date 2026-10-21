package proto

import (
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The room purposes of A 4.1.
const (
	PurposeMain      = "main"
	PurposeReports   = "reports"
	PurposeQuestions = "questions"
)

// The state types this package writes or reads (A 4, 5).
const (
	LinkType        = "teamchat.room"
	CreateType      = "m.room.create"
	PowerLevelsType = "m.room.power_levels"
	JoinRulesType   = "m.room.join_rules"
	MemberType      = "m.room.member"
	MessageType     = "m.room.message"
	RedactionType   = "m.room.redaction"
	nameType        = "m.room.name"
	topicType       = "m.room.topic"
	historyType     = "m.room.history_visibility"
	guestType       = "m.room.guest_access"
)

// RoomVersion is the room version of every team room (A 4.1).
const RoomVersion = "12"

// The fixed topics of A 4.4.
const (
	defaultMainTopic = "Agent team: the team's agents and the humans who direct them. Mention an agent to reach it."
	reportsTopic     = "Reports from the team's agents, for you to read. To reach an agent, write in the main room; " +
		"answer questions in the questions room."
	closedTopic   = "This team has no coordinator now, so this room is closed."
	maxTopicRunes = 120
)

// The power levels of A 4.2: everything closed at 100, `m.room.message` opened alone.
const (
	closedLevel        = 100
	humanMainLevel     = 50
	agentReportsLevel  = 10
	humanReportsLevel  = 0
	agentMainLevel     = 0
	mainRoomMentionLvl = 50
)

// RoomIDs are the IDs of the rooms that exist; empty for one not yet created.
type RoomIDs struct {
	Main, Reports, Questions string
}

// StateEvent is one state event: what a spec wants in a room, or what a room's state holds.
// Sender and EventID are read only by verification.
type StateEvent struct {
	Type, StateKey, Sender, EventID string
	Content                         Value
}

// RoomSpec is one room as C's rooms engine must make it (A 4, 5.2; seam 13). RoomID empty
// means create it, at RoomVersion, with State as its initial state and no encryption, alias
// or extra creator. Members is everyone who belongs in the room; Invite is whom the admin
// invites, nil until the roster is written (A 5.2). C kicks anyone joined outside Members.
type RoomSpec struct {
	Purpose     string
	RoomID      string
	RoomVersion string
	State       []StateEvent
	Members     []string
	Invite      []string
}

// Desired is the team's rooms as specs, in A 5.2's creation order: main with no roster, then
// each side room with its link, and only once all three exist the roster in main and the
// invitations. C asks again after each room it creates, so the specs come back in that order.
// description is the main topic's team description; one that fails ValidDescription is
// replaced by the default topic.
func Desired(server string, r Roster, ids RoomIDs, description string) []RoomSpec {
	full := ids.Main != "" && ids.Reports != "" && ids.Questions != ""
	r.Rooms = SideRooms{Reports: ids.Reports, Questions: ids.Questions}
	specs := []RoomSpec{mainSpec(r, ids.Main, description, full)}
	if ids.Main == "" {
		return specs
	}
	specs = append(specs, reportsSpec(r, ids.Reports, ids.Main, full))
	if ids.Reports == "" {
		return specs
	}
	return append(specs, questionsSpec(server, r, ids.Questions, ids.Main, full))
}

func mainSpec(r Roster, id, description string, full bool) RoomSpec {
	topic := defaultMainTopic
	if ValidDescription(description) {
		topic = description
	}
	s := RoomSpec{
		Purpose: PurposeMain, RoomID: id, RoomVersion: RoomVersion,
		State:   commonState(r.Name, topic, Obj(M("join_rule", Str("invite"))), openPowerLevels(r.Humans, agentMainLevel)),
		Members: sortedUnion(r.Humans, slices.Collect(maps.Keys(r.Members))),
	}
	if full {
		s.State = append(s.State, StateEvent{Type: RosterType, Content: r.Encode()})
		s.Invite = s.Members
	}
	return s
}

func reportsSpec(r Roster, id, mainID string, full bool) RoomSpec {
	join := Obj(M("join_rule", Str("restricted")), M("allow", Arr(Obj(
		M("type", Str("m.room_membership")), M("room_id", Str(mainID)),
	))))
	s := RoomSpec{
		Purpose: PurposeReports, RoomID: id, RoomVersion: RoomVersion,
		State:   append(commonState(r.Name+" · reports", reportsTopic, join, reportsPowerLevels(r.Humans)), link(r.Team, PurposeReports, mainID)),
		Members: sortedUnion(r.Humans, slices.Collect(maps.Keys(r.Members))),
	}
	if full {
		s.Invite = sortedUnion(r.Humans, nil)
	}
	return s
}

// questionsSpec is open, with the coordinator invited, while the roster names a non-parked
// coordinator, and closed otherwise (A 4.1, 4.3).
func questionsSpec(server string, r Roster, id, mainID string, full bool) RoomSpec {
	topic, level := closedTopic, int64(closedLevel)
	var wanted []string
	if coordinator, open := r.Coordinator(); open {
		if handle, ok := Localpart(coordinator, server); ok {
			topic = "Questions the coordinator " + handle +
				" could not answer itself. Answer here; the coordinator reads every message in this room."
		}
		level, wanted = agentMainLevel, []string{coordinator}
	}
	join := Obj(M("join_rule", Str("invite")))
	s := RoomSpec{
		Purpose: PurposeQuestions, RoomID: id, RoomVersion: RoomVersion,
		State:   append(commonState(r.Name+" · questions", topic, join, openPowerLevels(r.Humans, level)), link(r.Team, PurposeQuestions, mainID)),
		Members: sortedUnion(r.Humans, wanted),
	}
	if full {
		s.Invite = s.Members
	}
	return s
}

// link is a side room's `teamchat.room` (A 5.3).
func link(team, purpose, mainID string) StateEvent {
	return StateEvent{Type: LinkType, Content: Obj(
		M("v", Int(WireMajor)), M("team", Str(team)), M("purpose", Str(purpose)), M("main", Str(mainID)),
	)}
}

func commonState(name, topic string, join, power Value) []StateEvent {
	return []StateEvent{
		{Type: nameType, Content: Obj(M("name", Str(name)))},
		{Type: topicType, Content: Obj(M("topic", Str(topic)))},
		{Type: JoinRulesType, Content: join},
		{Type: PowerLevelsType, Content: power},
		{Type: historyType, Content: Obj(M("history_visibility", Str("shared")))},
		{Type: guestType, Content: Obj(M("guest_access", Str("forbidden")))},
	}
}

// openPowerLevels is A 4.2's main-room form, humans at 50; messageLevel 0 is main and open
// questions, 100 closed questions.
func openPowerLevels(humans []string, messageLevel int64) Value {
	return powerLevels(humans, humanMainLevel, agentMainLevel, mainRoomMentionLvl, messageLevel)
}

func reportsPowerLevels(humans []string) Value {
	return powerLevels(humans, humanReportsLevel, agentReportsLevel, closedLevel, agentReportsLevel)
}

func powerLevels(humans []string, humanLevel, usersDefault, roomMention, messageLevel int64) Value {
	users := make([]Member, 0, len(humans))
	for _, h := range sortedUnion(humans, nil) {
		users = append(users, M(h, Int(humanLevel)))
	}
	return Obj(
		M("users", Obj(users...)), M("users_default", Int(usersDefault)),
		M("events_default", Int(closedLevel)), M("state_default", Int(closedLevel)),
		M("invite", Int(closedLevel)), M("kick", Int(closedLevel)), M("ban", Int(closedLevel)),
		M("redact", Int(closedLevel)), M("notifications", Obj(M("room", Int(roomMention)))),
		M("events", Obj(M(MessageType, Int(messageLevel)))),
	)
}

func sortedUnion(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}

// MembershipDiff is whom to invite (in desired, not current) and whom to kick (in current, not
// desired), the admin never in either.
func MembershipDiff(desired, current []string, server string) ([]string, []string) {
	admin := AdminID(server)
	var invite, kick []string
	for _, id := range sortedUnion(desired, nil) {
		if id != admin && !slices.Contains(current, id) {
			invite = append(invite, id)
		}
	}
	for _, id := range sortedUnion(current, nil) {
		if id != admin && !slices.Contains(desired, id) {
			kick = append(kick, id)
		}
	}
	return invite, kick
}

// ValidDescription is P16's team description (A 4.4): 1-120 code points, no control or format
// character, equal to its trimmed self.
func ValidDescription(s string) bool {
	if !utf8.ValidString(s) || s != strings.TrimSpace(s) {
		return false
	}
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return false
		}
		n++
	}
	return n >= 1 && n <= maxTopicRunes
}

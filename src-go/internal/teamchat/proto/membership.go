package proto

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// The reasons a session leaves a team (B 2.10, wire.Remove's farewell): removed or parked, the
// member left the team; released, the owner took the seat out of this session and the member
// stays on the team; none, no reason was given.
const (
	FarewellRemoved  = "removed"
	FarewellReleased = "released"
	FarewellParked   = "parked"
	FarewellNone     = ""
)

// farewellText is each reason's words in the left line (n3e-D6).
var farewellText = map[string]string{
	FarewellRemoved:  "removed by the owner",
	FarewellReleased: "released from this session by the owner; you are still on the team",
	FarewellParked:   "parked by the owner",
	FarewellNone:     "taken out of this session",
}

// slotCoordinator is the coordinator slot's name in a refusal.
const slotCoordinator = "coordinator"

// MembershipLeft is a team this session left, and why.
type MembershipLeft struct{ Team, Reason string }

// MembershipTeam is a team this session joined (Handle, Description), whose role for it
// changed, or whose coordinator changed, with its role there; Coordinator is the team's
// coordinator for a worker, empty when it has none. Current is whether Role and Coordinator
// come from a team record verified current; the zero value is not, so a line built without
// one never states an absence.
type MembershipTeam struct {
	Team, Handle, Role, Coordinator, Description string
	Current                                      bool
}

// CoordinatorClaim is what a session may be told of its team's coordinator.
type CoordinatorClaim int

// The claims: not known (the record is not verified current), a coordinator named, or none.
const (
	CoordinatorNotKnown CoordinatorClaim = iota
	CoordinatorNamed
	CoordinatorAbsent
)

// ClaimCoordinator is the one decision of what a session is told of its team's coordinator
// (issue 192): from a record verified current (current), the coordinator it names, or an
// absence when it names none; from any other record (not verified yet, stopped, untrusted),
// not known. Every coordinator clause a session reads, SessionStart's frame and this notice,
// is decided here.
func ClaimCoordinator(current bool, coordinator string) CoordinatorClaim {
	switch {
	case !current:
		return CoordinatorNotKnown
	case coordinator == "":
		return CoordinatorAbsent
	}
	return CoordinatorNamed
}

// Membership is B's MEMBERSHIP notice (A 13.3, 00203 H12): what changed in this session's
// teams, and Teams, the teams it is on now: each once, every joined team and every team whose
// role or coordinator changed among them, no left team. B says what changed; proto writes
// every line. A role change's Handle and Description are not shown. Coordinators are the
// teams where this session is still a worker and the coordinator changed (issue 192).
type Membership struct {
	Left                        []MembershipLeft
	Joined, Roles, Coordinators []MembershipTeam
	Teams                       []string
}

// Render is the notice as its own delivery: H12's first line and one entry of the template
// lines, left teams, then joined ones, then role changes, then coordinator changes, then the
// teams now. A slot failing its grammar is refused by name, nothing changed is `membership`,
// and a line failing A 13.3's line rule (badMembershipLine) is `membership line`.
func (m Membership) Render(nonce string) (string, error) {
	if !nonceRe.MatchString(nonce) {
		return "", &SlotError{Slot: "nonce"}
	}
	if len(m.Left)+len(m.Joined)+len(m.Roles)+len(m.Coordinators) == 0 {
		return "", &SlotError{Slot: "membership"}
	}
	lines, err := m.changeLines()
	if err != nil {
		return "", err
	}
	if !m.teamsAgree() {
		return "", &SlotError{Slot: "teams"}
	}
	if len(m.Teams) == 0 {
		lines = append(lines, "teamchat: this session is on no team now; nothing arrives until the owner adds it to one.")
	} else {
		lines = append(lines, "teamchat: this session's teams are now: "+strings.Join(m.Teams, ", ")+".")
	}
	if slices.ContainsFunc(lines, badMembershipLine) {
		return "", &SlotError{Slot: "membership line"}
	}
	marker := nonce + " 1/1>>"
	out := []string{
		"teamchat delivery " + nonce + ": a membership notice from teamchat: " + m.headline(),
		"<<teamchat " + marker, "kind: " + kindMembership, "from: teamchat", "to: this session",
	}
	out = append(append(out, lines...), "authority: "+authorityMembership, "<<end teamchat "+marker)
	return strings.Join(out, "\n"), nil
}

// headline is the first line's last words: H12's "this session's teams changed", or, when the
// only change is a coordinator's, which team's (or teams') coordinator changed. The slots were
// checked as team ids by changeLines before it is asked.
func (m Membership) headline() string {
	if len(m.Left)+len(m.Joined)+len(m.Roles) > 0 || len(m.Coordinators) == 0 {
		return "this session's teams changed"
	}
	teams := make([]string, 0, len(m.Coordinators))
	for _, c := range m.Coordinators {
		teams = append(teams, c.Team)
	}
	if len(teams) == 1 {
		return "the coordinator of team " + teams[0] + " changed"
	}
	return "the coordinators of teams " + strings.Join(teams, ", ") + " changed"
}

// changeLines are the left, joined, role-change and coordinator-change lines, in that order.
func (m Membership) changeLines() ([]string, error) {
	var lines []string
	for _, l := range m.Left {
		why, ok := farewellText[l.Reason]
		switch {
		case !ValidTeamID(l.Team):
			return nil, &SlotError{Slot: slotTeam}
		case !ok:
			return nil, &SlotError{Slot: "reason"}
		}
		lines = append(lines, "teamchat: you left team "+l.Team+": "+why+". Commands for it are refused from now on.")
	}
	for _, j := range m.Joined {
		if !validHandle(j.Handle) {
			return nil, &SlotError{Slot: "handle"}
		}
		role, err := roleLine(j)
		if err != nil {
			return nil, err
		}
		lines = append(lines, "teamchat: you joined team "+j.Team+" as "+j.Handle+".")
		// A description failing P16 or the line rule is no description (A 4.4).
		if about := "teamchat: team " + j.Team + " is for: " + j.Description; ValidDescription(j.Description) && !badMembershipLine(about) {
			lines = append(lines, about)
		}
		lines = append(lines, role)
	}
	for _, r := range m.Roles {
		role, err := roleLine(r)
		if err != nil {
			return nil, err
		}
		lines = append(lines, role)
	}
	for _, c := range m.Coordinators {
		line, err := coordinatorLine(c)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// teamsAgree is whether Teams are team IDs, each once, holding every joined team, every role
// and coordinator change's, and no left team.
func (m Membership) teamsAgree() bool {
	on := map[string]bool{}
	for _, team := range m.Teams {
		if !ValidTeamID(team) || on[team] {
			return false
		}
		on[team] = true
	}
	for _, l := range m.Left {
		if on[l.Team] {
			return false
		}
	}
	for _, t := range slices.Concat(m.Joined, m.Roles, m.Coordinators) {
		if !on[t.Team] {
			return false
		}
	}
	return true
}

// roleLine is a team's role line: the coordinator's, or a worker's with its coordinator clause
// as ClaimCoordinator decides it.
func roleLine(t MembershipTeam) (string, error) {
	switch {
	case !ValidTeamID(t.Team):
		return "", &SlotError{Slot: slotTeam}
	case t.Role != RoleCoordinator && t.Role != RoleWorker:
		return "", &SlotError{Slot: "role"}
	case t.Coordinator != "" && (t.Role == RoleCoordinator || !validHandle(t.Coordinator)):
		return "", &SlotError{Slot: slotCoordinator}
	case t.Role == RoleCoordinator:
		return "teamchat: in team " + t.Team + " you are the coordinator.", nil
	}
	worker := "teamchat: in team " + t.Team + " you are a worker; "
	switch ClaimCoordinator(t.Current, t.Coordinator) {
	case CoordinatorNamed:
		return worker + "the coordinator is " + t.Coordinator + ".", nil
	case CoordinatorAbsent:
		return worker + "the team has no coordinator now.", nil
	case CoordinatorNotKnown:
	}
	return worker + "the coordinator is not known yet: run teamchat status --team " + t.Team + ".", nil
}

// coordinatorLine is a worker's line for its team's coordinator changed: the one named now, or
// none. A change not known (a record not verified current) is never told, so it is refused.
func coordinatorLine(t MembershipTeam) (string, error) {
	switch {
	case !ValidTeamID(t.Team):
		return "", &SlotError{Slot: slotTeam}
	case t.Role != RoleWorker:
		return "", &SlotError{Slot: "role"}
	case t.Coordinator != "" && !validHandle(t.Coordinator):
		return "", &SlotError{Slot: slotCoordinator}
	}
	switch ClaimCoordinator(t.Current, t.Coordinator) {
	case CoordinatorNamed:
		return "teamchat: in team " + t.Team + " the coordinator is now " + t.Coordinator + ".", nil
	case CoordinatorAbsent:
		return "teamchat: in team " + t.Team + " there is no coordinator now.", nil
	case CoordinatorNotKnown:
	}
	return "", &SlotError{Slot: slotCoordinator}
}

// badMembershipLine is A 13.3's line rule failed: empty, over MaxMembershipLine characters, not
// valid UTF-8, or holding a control (C0, DEL, C1), U+2028, U+2029, `<<` or `>>`, so no line can
// end the entry or start another.
func badMembershipLine(line string) bool {
	if line == "" || !utf8.ValidString(line) || utf8.RuneCountInString(line) > MaxMembershipLine ||
		strings.Contains(line, "<<") || strings.Contains(line, ">>") {
		return true
	}
	return strings.ContainsFunc(line, func(r rune) bool { return r < ' ' || r >= 0x7F && r <= 0x9F || r == 0x2028 || r == 0x2029 })
}

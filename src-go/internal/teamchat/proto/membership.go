package proto

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// FarewellRemoved is the one reason a session leaves a team (v1 membership.FAREWELL_REASONS):
// the owner took it off the team on the host.
const FarewellRemoved = "removed"

var farewellText = map[string]string{FarewellRemoved: "removed by the owner"}

// MembershipLeft is a team this session left, and why.
type MembershipLeft struct{ Team, Reason string }

// MembershipTeam is a team this session joined (Handle, Description) or whose role for it
// changed, with its role there; Coordinator is the team's coordinator for a worker, empty when
// it has none.
type MembershipTeam struct{ Team, Handle, Role, Coordinator, Description string }

// Membership is B's MEMBERSHIP notice (A 13.3, 00203 H12): what changed in this session's
// teams, and Teams, the teams it is on now: each once, every joined team and every team whose
// role changed among them, no left team. B says what changed; proto writes every line. A role
// change's Handle and Description are not shown.
type Membership struct {
	Left          []MembershipLeft
	Joined, Roles []MembershipTeam
	Teams         []string
}

// Render is the notice as its own delivery: H12's first line and one entry of the template
// lines, left teams, then joined ones, then role changes, then the teams now. A slot failing
// its grammar is refused by name, nothing changed is `membership`, and a line failing A 13.3's
// line rule (badMembershipLine) is `membership line`.
func (m Membership) Render(nonce string) (string, error) {
	if !nonceRe.MatchString(nonce) {
		return "", &SlotError{Slot: "nonce"}
	}
	if len(m.Left)+len(m.Joined)+len(m.Roles) == 0 {
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
		"teamchat delivery " + nonce + ": a membership notice from teamchat: this session's teams changed",
		"<<teamchat " + marker, "kind: " + kindMembership, "from: teamchat", "to: this session",
	}
	out = append(append(out, lines...), "authority: "+authorityMembership, "<<end teamchat "+marker)
	return strings.Join(out, "\n"), nil
}

// changeLines are the left, joined and role-change lines, in that order.
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
	return lines, nil
}

// teamsAgree is whether Teams are team IDs, each once, holding every joined team and every
// role change's and no left team.
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
	for _, t := range slices.Concat(m.Joined, m.Roles) {
		if !on[t.Team] {
			return false
		}
	}
	return true
}

// roleLine is a team's role line: the coordinator's, or a worker's naming its coordinator.
func roleLine(t MembershipTeam) (string, error) {
	switch {
	case !ValidTeamID(t.Team):
		return "", &SlotError{Slot: slotTeam}
	case t.Role != RoleCoordinator && t.Role != RoleWorker:
		return "", &SlotError{Slot: "role"}
	case t.Coordinator != "" && (t.Role == RoleCoordinator || !validHandle(t.Coordinator)):
		return "", &SlotError{Slot: "coordinator"}
	case t.Role == RoleCoordinator:
		return "teamchat: in team " + t.Team + " you are the coordinator.", nil
	case t.Coordinator == "":
		return "teamchat: in team " + t.Team + " you are a worker; the team has no coordinator now.", nil
	}
	return "teamchat: in team " + t.Team + " you are a worker; the coordinator is " + t.Coordinator + ".", nil
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

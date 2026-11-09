// Package proto holds the one decision of a coordinator clause and the places it is written.
package proto

// CoordinatorClaim is what a session may be told of its team's coordinator.
type CoordinatorClaim int

const (
	CoordinatorNotKnown CoordinatorClaim = iota
	CoordinatorAbsent
	CoordinatorNamed
)

// ClaimCoordinator decides the clause.
func ClaimCoordinator(current bool, coordinator string) CoordinatorClaim {
	switch {
	case !current:
		return CoordinatorNotKnown
	case coordinator == "":
		return CoordinatorAbsent
	}
	return CoordinatorNamed
}

// closedTopic is named in the rule's table: room state, not a claim made to a session.
const closedTopic = "This team has no coordinator now, so this room is closed."

func roleLine(current bool, coordinator string) string {
	switch ClaimCoordinator(current, coordinator) {
	case CoordinatorAbsent:
		return "the team has no coordinator now."
	case CoordinatorNamed:
		return "the coordinator is " + coordinator
	case CoordinatorNotKnown:
	}
	return closedTopic + " not known yet"
}

// bareClaim switches on a claim someone else decided: the clause is not ClaimCoordinator's.
func bareClaim(claim CoordinatorClaim) string {
	switch claim {
	case CoordinatorAbsent:
		return "there is no coordinator now." // want `an absence clause`
	}
	return ""
}

// beforeTheCase writes the clause outside the case that decides it.
func beforeTheCase(current bool, coordinator string) string {
	clause := "the team has no coordinator now." // want `an absence clause`
	switch ClaimCoordinator(current, coordinator) {
	case CoordinatorAbsent:
		return clause
	}
	return ""
}

// inTheWrongCase tells an absence where the claim names a coordinator.
func inTheWrongCase(current bool, coordinator string) string {
	switch ClaimCoordinator(current, coordinator) {
	case CoordinatorNamed:
		return "the team has no coordinator now." // want `an absence clause`
	}
	return ""
}

// untableTopic is a constant the table does not name.
const untableTopic = "no coordinator, so this room is closed" // want `an absence clause`

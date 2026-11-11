// Package badabsence writes an absence clause with no ClaimCoordinator deciding it.
package badabsence

const tellsAbsence = "the team has no coordinator now." // want `an absence clause`

func frame(current bool) string {
	if !current {
		return "your role is not known yet"
	}
	return "team alpha: coordinator: none" // want `an absence clause`
}

func closed() string {
	return "Without a coordinator the room is closed" // want `an absence clause`
}

func fine() string {
	return "the coordinator is repo.1+site.podman"
}

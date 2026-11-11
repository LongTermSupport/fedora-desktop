// Package hooktext holds the frame's templates.
package hooktext

// tmplTeamNoCoordinator is named in the rule's table.
const tmplTeamNoCoordinator = "team {}: you are {}; the team has no coordinator now."

// tmplOther is not.
const tmplOther = "team {}: you are {}; no coordinator." // want `an absence clause`

func head() string { return tmplTeamNoCoordinator + tmplOther }

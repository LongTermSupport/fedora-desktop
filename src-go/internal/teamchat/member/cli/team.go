package cli

import (
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ActiveTeam is one team the session holds, with its description for the ambiguous-team list.
type ActiveTeam struct {
	ID          paths.TeamID
	Description string
}

const (
	msgTeamDiffers  exits.Template = "the event named is in team {}, not --team {}"
	msgNoTeam       exits.Template = "this session holds no team, so no verb can act on one; tell a human if it should"
	msgAmbiguous    exits.Template = "this session holds {} teams; give --team with one of: {}"
	msgTeamAndAbout exits.Template = "{} ({})"
)

// ResolveTeam is a single-team verb's team (CL6, B5): the --team flag, or the team the ledger
// knows an event id by (reply, verify), or the only active team. A flag and a ledger that
// differ, no active team, or more than one with neither given, is 64.
func ResolveTeam(flag, ledger *paths.TeamID, active []ActiveTeam) (paths.TeamID, error) {
	switch {
	case flag != nil && ledger != nil && *flag != *ledger:
		return paths.TeamID{}, usage(msgTeamDiffers, exits.Team(*ledger), exits.Team(*flag))
	case flag != nil:
		return *flag, nil
	case ledger != nil:
		return *ledger, nil
	case len(active) == 1:
		return active[0].ID, nil
	case len(active) == 0:
		return paths.TeamID{}, usage(msgNoTeam)
	}
	choices := make([]exits.Fill, len(active))
	for i, a := range active {
		choices[i] = exits.Format(msgTeamAndAbout, exits.Team(a.ID), exits.Description(a.Description))
	}
	return paths.TeamID{}, exits.New(exits.CodeAmbiguousTeam, nil, msgAmbiguous,
		exits.Count(int64(len(active))), exits.Join("; ", choices...))
}

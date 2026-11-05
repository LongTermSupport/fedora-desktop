package cli

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// CL6: every single-team verb with two active teams and no --team is 64 ambiguous-team, the
// message listing the teams with their descriptions, a description failing its grammar as ?.
func TestEverySingleTeamVerbWithTwoTeamsAndNoTeamIsAmbiguous(t *testing.T) {
	two := []ActiveTeam{
		{ID: mustTeam(t, "alpha"), Description: "Release work for the desktop"},
		{ID: mustTeam(t, "beta"), Description: "bad\x1b[31m"},
	}
	n := 0
	for _, v := range Verbs() {
		if v.Team != TeamOne {
			continue
		}
		n++
		inv, err := Parse(singleTeamLine(v.Name))
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		_, err = ResolveTeam(inv.Team, nil, two)
		e, ok := errors.AsType[*exits.Error](err)
		if !ok || e.Exit != exits.ExitUsage || e.Code != exits.CodeAmbiguousTeam {
			t.Errorf("%s: %v", v.Name, err)
			continue
		}
		want := "this session holds 2 teams; give --team with one of: alpha (Release work for the desktop); beta (?)"
		if e.Message != want {
			t.Errorf("%s: %q", v.Name, e.Message)
		}
	}
	if n != 7 {
		t.Errorf("%d single-team verbs", n)
	}
}

// singleTeamLine is a valid command line of a single-team verb with no --team; say's is SD2's
// `--to` case.
func singleTeamLine(verb string) []string {
	switch verb {
	case "reply", "verify":
		return []string{verb, eventID}
	case "say":
		return []string{verb, "--to", "repo.2"}
	case "ping":
		return []string{verb, "review", "pr:o/r#1@abc", "--to", "repo.2"}
	case "thread":
		return []string{verb, "Q1"}
	}
	return []string{verb}
}

func TestResolveTeam(t *testing.T) {
	a, b := mustTeam(t, "alpha"), mustTeam(t, "beta")
	one := []ActiveTeam{{ID: a, Description: "d"}}
	two := []ActiveTeam{{ID: a, Description: "d"}, {ID: b, Description: "e"}}
	for _, c := range []struct {
		name         string
		flag, ledger *paths.TeamID
		active       []ActiveTeam
		want         paths.TeamID
	}{
		{"the flag", new(b), nil, two, b},
		{"the flag, no team active", new(b), nil, nil, b},
		{"the ledger", nil, new(b), two, b},
		{"flag and ledger agree", new(b), new(b), two, b},
		{"the only active team", nil, nil, one, a},
	} {
		got, err := ResolveTeam(c.flag, c.ledger, c.active)
		if err != nil || got != c.want {
			t.Errorf("%s: %v %v", c.name, got, err)
		}
	}
}

func TestResolveTeamRefusals(t *testing.T) {
	a, b := mustTeam(t, "alpha"), mustTeam(t, "beta")
	for _, c := range []struct {
		name         string
		flag, ledger *paths.TeamID
		active       []ActiveTeam
		code         string
		msg          string
	}{
		{"flag and ledger differ", new(a), new(b), nil, exits.CodeUsage, "the event named is in team beta, not --team alpha"},
		{"no team", nil, nil, nil, exits.CodeUsage, "this session holds no team, so no verb can act on one; tell a human if it should"},
	} {
		_, err := ResolveTeam(c.flag, c.ledger, c.active)
		e, ok := errors.AsType[*exits.Error](err)
		if !ok || e.Exit != exits.ExitUsage || e.Code != c.code || e.Message != c.msg {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

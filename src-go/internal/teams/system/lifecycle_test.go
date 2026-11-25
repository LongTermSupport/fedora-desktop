package system

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// lifeOf is a Life over a status record found or not and the host_vars' word on the team.
func lifeOf(found bool, declared Declared) Life {
	return Life{
		Status:   func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, found, nil },
		Declared: func(paths.TeamID) (Declared, error) { return declared, nil },
		HostVars: "/checkout/environment/localhost/host_vars/localhost.yml",
	}
}

// Register R59: a pool seat is judged by its team's lifecycle on this machine, never by the pool
// folder alone. A team installed here (a status record) and a team this machine does not declare
// (served on another machine, docs/agent-bus-cross-network.md) can be taken; a team the host_vars
// declare absent was removed, and one they declare present with no record is not installed, so
// neither seat can log in and each is refused naming why and the way on.
func TestLifeJudgesATeamByItsRecordAndItsDeclaration(t *testing.T) {
	team := mustTeam(t, "gone")
	for name, c := range map[string]struct {
		life Life
		gone bool
		want []string
	}{
		"installed here":                  {life: lifeOf(true, NotDeclared)},
		"installed, declared present":     {life: lifeOf(true, DeclaredPresent)},
		"installed, declared absent":      {life: lifeOf(true, DeclaredAbsent)},
		"served on another machine":       {life: lifeOf(false, NotDeclared)},
		"removed: declared absent":        {life: lifeOf(false, DeclaredAbsent), gone: true, want: []string{"team gone was removed from this machine", "declares it absent", "localhost.yml", paths.StatusFile(team), "ccy-teams seats abandon gone --reason"}},
		"declared present, not installed": {life: lifeOf(false, DeclaredPresent), gone: true, want: []string{"team gone is declared", "not installed on this machine", paths.StatusFile(team), "play-teamchat.yml"}},
	} {
		err := c.life.Check(team)
		if !c.gone {
			if err != nil {
				t.Errorf("%s: refused: %v", name, err)
			}
			continue
		}
		if !errors.Is(err, ErrTeamGone) {
			t.Errorf("%s: want ErrTeamGone, got %v", name, err)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %q does not say %q", name, err, w)
			}
		}
	}
}

// A source that cannot be read, or one not wired, never lets a seat through as takeable.
func TestLifeFailsWhenASourceCannotBeRead(t *testing.T) {
	team := mustTeam(t, "gone")
	broken := errors.New("permission denied")
	noStatus := lifeOf(false, NotDeclared)
	noStatus.Status = func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, broken }
	noDecl := lifeOf(false, NotDeclared)
	noDecl.Declared = func(paths.TeamID) (Declared, error) { return NotDeclared, broken }
	unwired := Life{}
	for name, l := range map[string]Life{"status": noStatus, "declaration": noDecl, "unwired": unwired} {
		err := l.Check(team)
		if err == nil || errors.Is(err, ErrTeamGone) {
			t.Errorf("%s: want a read failure, got %v", name, err)
		}
	}
	if err := noDecl.Check(team); !strings.Contains(err.Error(), "localhost.yml") {
		t.Errorf("a declaration that cannot be read names the file: %v", err)
	}
	if err := unwired.Check(team); !strings.Contains(err.Error(), "cannot be judged") {
		t.Errorf("an unwired Life says so: %v", err)
	}
}

// Register R59: the chooser's pool and team list never offer a removed team's seats; a live
// team beside it is unaffected, and a Cmd with no lifecycle wired refuses.
func TestCmdNeverOffersARemovedTeam(t *testing.T) {
	w := world(t)
	checkout := w.home + "/repo"
	placeSeat(t, w.home, checkout, "gone")
	placeSeat(t, w.home, checkout, "live")
	w.cmd.Sessions = restorable{}
	w.cmd.Life = func(team paths.TeamID) error {
		if team.String() == "gone" {
			return lifeOf(false, DeclaredAbsent).Check(team)
		}
		return nil
	}
	if _, err := w.cmd.PoolState("gone", "podman"); !errors.Is(err, ErrTeamGone) {
		t.Errorf("PoolState offered a removed team's seats: %v", err)
	}
	if got, err := w.cmd.PoolState("live", "podman"); err != nil || len(got) != 1 {
		t.Errorf("PoolState of a live team: got %+v %v", got, err)
	}
	if got, err := w.cmd.PoolTeams(); err != nil || strings.Join(got, ",") != "live" {
		t.Errorf("PoolTeams: got %v %v, want [live]", got, err)
	}
	w.cmd.Life = func(paths.TeamID) error { return errors.New("permission denied") }
	if _, err := w.cmd.PoolTeams(); err == nil {
		t.Error("PoolTeams passed a team whose lifecycle could not be read")
	}
	w.cmd.Life = nil
	if _, err := w.cmd.PoolState("live", "podman"); err == nil {
		t.Error("PoolState with no lifecycle wired answered")
	}
	if _, err := w.cmd.PoolTeams(); err == nil {
		t.Error("PoolTeams with no lifecycle wired answered")
	}
}

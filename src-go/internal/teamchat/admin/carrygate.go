package admin

import (
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// flagDrill is the drill export's and import's flag; --resume and --discard are restore's
// words, which team import shares.
const flagDrill = "drill"

// carryGate is SG1: before a verb runs, the carry records of the team its --team names are
// read, and while any is present the verb refuses (70), naming the record and the one way on,
// unless every present record admits it. Then SG2: a drill import refuses every verb but its
// check and its removal. A verb whose --team names a v1 team (namesV1Team) is not judged here:
// the records are a v2 team's. A run with no --team, or a --team that is not a team
// id (the verb's own usage says so), is not judged here. The lock's guard (SG3) judges again
// once a verb holds the lock.
func carryGate(e Env, name string, args []string) error {
	team, ok := namedTeam(args)
	if !ok || namesV1Team[name] {
		return nil
	}
	sys, err := e.wired()
	if err != nil {
		return err
	}
	found, err := carry.Recorded(sys.Host.Root, team)
	if err != nil {
		return err
	}
	for _, r := range found {
		if !admits(r, name, args) {
			return r.Refusal(sys.Host.Root, team)
		}
	}
	if drillAdmits[name] {
		return nil
	}
	drill, err := carry.DrillImported(sys.Host.Root, sys.Host.StateOwnerUID, team)
	if err != nil {
		return fmt.Errorf("cannot tell whether team %s is a drill import: %w", team, err)
	}
	if drill {
		return carry.DrillImportRefusal(team)
	}
	return nil
}

// drillAdmits are the verbs a drill import admits (SG2, sc-D16): its check and its removal.
var drillAdmits = map[string]bool{"team carry-check": true, verbTeamRemove: true}

// verbTeamRemove is the remove verb's name, which a seal and a drill import each admit.
const verbTeamRemove = "team remove"

// namedTeam is the team id after a --team in args.
func namedTeam(args []string) (paths.TeamID, bool) {
	i := slices.Index(args, "--"+flagTeam)
	if i < 0 || i+1 >= len(args) {
		return paths.TeamID{}, false
	}
	team, err := paths.ParseTeamID(args[i+1])
	return team, err == nil
}

// verbTeamExport is the export verb's name, which a seal and a drill record each admit.
const verbTeamExport = "team export"

// admits is whether the record lets the verb run: a seal admits team remove, team export
// without --drill and team seal; a drill record only team export --drill; an import record
// only team import --resume or --discard.
func admits(r carry.Record, name string, args []string) bool {
	has := func(flag string) bool { return slices.Contains(args, "--"+flag) }
	switch r {
	case carry.RecordSeal:
		return name == verbTeamRemove || name == "team seal" || (name == verbTeamExport && !has(flagDrill))
	case carry.RecordDrill:
		return name == verbTeamExport && has(flagDrill)
	case carry.RecordImport:
		return name == "team import" && (has(flagResume) || has(flagDiscard))
	}
	return false
}

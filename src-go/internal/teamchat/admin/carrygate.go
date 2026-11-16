package admin

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// flagDrill is the drill export's and import's flag; --resume and --discard are restore's
// words, which team import shares.
const flagDrill = "drill"

// carryGate is SG1: before a verb runs, the carry records of the team its --team names are
// read, and while any is present the verb refuses (70), naming the record and the one way on,
// unless every present record admits it. A run with no --team, or a --team that is not a team
// id (the verb's own usage says so), is not judged here. The lock's guard (SG3) judges again
// once a verb holds the lock.
func carryGate(e Env, name string, args []string) error {
	team, ok := namedTeam(args)
	if !ok {
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
	return nil
}

// namedTeam is the team id after a --team in args.
func namedTeam(args []string) (paths.TeamID, bool) {
	i := slices.Index(args, "--"+flagTeam)
	if i < 0 || i+1 >= len(args) {
		return paths.TeamID{}, false
	}
	team, err := paths.ParseTeamID(args[i+1])
	return team, err == nil
}

// admits is whether the record lets the verb run: a seal admits team remove, team export
// without --drill and team seal; a drill record only team export --drill; an import record
// only team import --resume or --discard.
func admits(r carry.Record, name string, args []string) bool {
	has := func(flag string) bool { return slices.Contains(args, "--"+flag) }
	switch r {
	case carry.RecordSeal:
		return name == "team remove" || name == "team seal" || (name == "team export" && !has(flagDrill))
	case carry.RecordDrill:
		return name == "team export" && has(flagDrill)
	case carry.RecordImport:
		return name == "team import" && (has(flagResume) || has(flagDiscard))
	}
	return false
}

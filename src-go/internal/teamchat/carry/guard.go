package carry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// ErrSealed is a verb refused because a carry record of its team is present: the seal, a drill
// export under way or an import under way (SG1, SG3); root exits 70 on it.
var ErrSealed = errors.New("carry: a carry record of the team refuses the verb")

// Record is one of the three carry records SG1 reads.
type Record int

// The carry records, in the order SG1 reads them.
const (
	RecordSeal Record = iota + 1
	RecordDrill
	RecordImport
)

var records = []Record{RecordSeal, RecordDrill, RecordImport}

func (r Record) String() string {
	switch r {
	case RecordSeal:
		return "seal"
	case RecordDrill:
		return "drill"
	case RecordImport:
		return "import"
	}
	return fmt.Sprintf("record %d", int(r))
}

// Path is the record's path for the team on a host (CN1, CN2).
func (r Record) Path(team paths.TeamID) string {
	switch r {
	case RecordSeal:
		return paths.SealRecord(team)
	case RecordDrill:
		return paths.DrillRecord(team)
	case RecordImport:
		return paths.CarryImportRecord(team)
	}
	return ""
}

// RecordedError is ErrSealed for one team and record, naming the record under the host prefix
// and the one way on.
type RecordedError struct {
	Team   paths.TeamID
	Record Record
	Path   string
}

func (e *RecordedError) Error() string {
	t := e.Team.String()
	switch e.Record {
	case RecordSeal:
		return "team " + t + " is sealed (" + e.Path + "): it is carried, and only team export --team " + t +
			", team seal --team " + t + " and team remove --team " + t + " --purge run on it"
	case RecordDrill:
		return "a drill export of team " + t + " is under way (" + e.Path + "): finish it with team export --team " + t + " --drill"
	case RecordImport:
	}
	return "an import of team " + t + " is under way (" + e.Path + "): finish it with team import --team " + t +
		" --resume, or undo it with team import --team " + t + " --discard"
}

// Is makes a RecordedError ErrSealed.
func (e *RecordedError) Is(target error) bool { return target == ErrSealed }

// Recorded is the carry records present for the team under the host prefix root ("" on a host),
// by Lstat, so a link in a record's place counts (SG1).
func Recorded(root string, team paths.TeamID) ([]Record, error) {
	var found []Record
	for _, r := range records {
		_, err := os.Lstat(root + r.Path(team))
		switch {
		case err == nil:
			found = append(found, r)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("cannot tell whether team %s has a carry %s record: %w", team, r, err)
		}
	}
	return found, nil
}

// Refusal is the record's refusal for the team under the host prefix root.
func (r Record) Refusal(root string, team paths.TeamID) error {
	return &RecordedError{Team: team, Record: r, Path: root + r.Path(team)}
}

// refuseRecords is the guard that refuses while any of want is recorded.
func refuseRecords(root string, want ...Record) teamlock.Guard {
	return func(team paths.TeamID) error {
		found, err := Recorded(root, team)
		if err != nil {
			return err
		}
		for _, r := range found {
			if slices.Contains(want, r) {
				return r.Refusal(root, team)
			}
		}
		return nil
	}
}

// Guard is the carry's half of every lock-taking verb's guard under the host prefix root: it
// refuses while any carry record is present (SG3). wire joins it to restore.Guard with
// teamlock.All.
func Guard(root string) teamlock.Guard {
	return refuseRecords(root, records...)
}

// DrillImported is whether the team under the host prefix root was imported from a drill
// export: its carried.json, read by the trust rule as a file of ownerUID's (0 on a host),
// decodes and is of kind drill. No carried.json is false; one that cannot be read or decoded
// is an error, never false.
func DrillImported(root string, ownerUID uint32, team paths.TeamID) (bool, error) {
	raw, found, err := state.ReadOwnedFile(root+paths.CarriedRecord(team), maxRecord, ownerUID)
	if err != nil || !found {
		return false, err
	}
	rec, err := DecodeCarriedRecord(raw, team)
	if err != nil {
		return false, err
	}
	return rec.Kind == KindDrill, nil
}

// DrillImportRefusal is SG2's refusal (70) of every verb but team carry-check and team remove
// on a drill import.
func DrillImportRefusal(team paths.TeamID) error {
	return refusedf("team %s was imported from a drill export: a drill import never joins a real team; "+
		"only team carry-check --team %s and team remove --team %s --purge run on it", team, team, team)
}

// DrillImportGuard is SG2 under the lock (SG3): a drill import refuses. wire joins it to every
// lock-taking verb's guard but team remove's and team carry-check's, so an import that lands
// while a verb waits for the lock still refuses it.
func DrillImportGuard(root string, ownerUID uint32) teamlock.Guard {
	return func(team paths.TeamID) error {
		drill, err := DrillImported(root, ownerUID, team)
		if err != nil {
			return fmt.Errorf("cannot tell whether team %s is a drill import: %w", team, err)
		}
		if drill {
			return DrillImportRefusal(team)
		}
		return nil
	}
}

// RemoveGuard is `team remove`'s whole guard (SG4): restore.Guard, the drill record and the
// import record. Only the seal is admitted, so a sealed team can be removed; a drill or an
// import under way refuses it, as SG1 does.
func RemoveGuard(root string) teamlock.Guard {
	return teamlock.All(restore.Guard(root), refuseRecords(root, RecordDrill, RecordImport))
}

// SealedGuard is `team export`'s guard (SG4): RemoveGuard's refusals, and the seal record,
// read by the trust rule as a file of ownerUID's (0 on a host), at step sealed.
func SealedGuard(root string, ownerUID uint32) teamlock.Guard {
	refuse := RemoveGuard(root)
	return func(team paths.TeamID) error {
		if err := refuse(team); err != nil {
			return err
		}
		_, err := ReadSeal(root, ownerUID, team)
		return err
	}
}

// ReadSeal is the team's seal record at step sealed under the host prefix root; a team not
// sealed, or sealed only to step sealing, is refused (ErrRefused).
func ReadSeal(root string, ownerUID uint32, team paths.TeamID) (SealRecord, error) {
	path := root + paths.SealRecord(team)
	raw, found, err := state.ReadOwnedFile(path, maxRecord, ownerUID)
	switch {
	case err != nil:
		return SealRecord{}, err
	case !found:
		return SealRecord{}, refusedf("team %s is not sealed: run team seal --team %s first", team, team)
	}
	rec, err := DecodeSealRecord(raw, team)
	switch {
	case err != nil:
		return SealRecord{}, err
	case rec.Step != StepSealed:
		return SealRecord{}, refusedf("the seal of team %s stopped at step %s: run team seal --team %s again", team, rec.Step, team)
	}
	return rec, nil
}

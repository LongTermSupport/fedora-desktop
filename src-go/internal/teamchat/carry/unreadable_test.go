package carry

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// unreadable is a file a verb reads, made unreadable: removed, or replaced by bytes its decoder
// refuses; want is what the failure names.
type unreadable struct {
	path func(paths.TeamID) string
	gone bool
	want string
}

func (u unreadable) apply(w *sealWorld) {
	w.t.Helper()
	if u.gone {
		mustRemove(w, u.path(w.team))
		return
	}
	w.put(u.path(w.team), "not the file it should be")
}

// SC5, SC7: a seal resumed at step sealing reads its facts after the stop; a fact it cannot read
// fails the run naming the file, and the seal stays at step sealing with the stamp in place, so
// the next run, once the file is put right, finishes it.
func TestSealResumeNeedsItsFacts(t *testing.T) {
	for name, u := range map[string]unreadable{
		"roster missing":            {path: paths.RosterFile, gone: true, want: "roster.json is missing"},
		"roster undecodable":        {path: paths.RosterFile, want: "roster"},
		"epoch floor undecodable":   {path: paths.RestoreEpochFile, want: "restore"},
		"units.json missing":        {path: paths.UnitsRecord, gone: true, want: "units.json is missing"},
		"units.json undecodable":    {path: paths.UnitsRecord, want: "units"},
		"server-name missing":       {path: paths.ServerNameFile, gone: true, want: "server-name is missing"},
		"the record cannot be read": {path: paths.SealRecord, want: "sealed"},
	} {
		w := newSealWorld(t)
		w.put(paths.SealRecord(w.team), string(EncodeSealRecord(SealRecord{Team: w.team, Step: StepSealing, StartedMS: sealMS, Tail: w.tail.Rooms})))
		u.apply(w)
		_, err := w.seal()
		if err == nil || !strings.Contains(err.Error(), u.want) {
			t.Errorf("%s: the seal answered %v, not a failure naming %q", name, err, u.want)
			continue
		}
		if !w.has(paths.StampFile(w.team)) {
			t.Errorf("%s: the stamp was removed by a seal that failed", name)
		}
		if name != "the record cannot be read" && w.record().Step != StepSealing {
			t.Errorf("%s: the seal is at step %s", name, w.record().Step)
		}
	}
}

// SI10: a resume at step staged reads the record and the staged manifest, and checks every staged
// member against it; one it cannot read, or a member that is not what the manifest lists, fails
// the resume naming it, with nothing placed.
func TestImportResumeAtStagedNeedsTheStagedTree(t *testing.T) {
	staging := func(name string) func(paths.TeamID) string {
		return func(team paths.TeamID) string { return paths.CarryStaging(team) + "/" + name }
	}
	for name, u := range map[string]unreadable{
		"record undecodable":    {path: paths.CarryImportRecord, want: "import"},
		"manifest missing":      {path: staging(manifestName), gone: true, want: manifestName},
		"manifest undecodable":  {path: staging(manifestName), want: "the staged"},
		"a directory as a file": {path: staging("admin/tls"), gone: true, want: "admin/tls"},
	} {
		w := stagedRunWorld(t)
		if name == "a directory as a file" {
			if err := os.RemoveAll(w.root + u.path(w.team)); err != nil {
				t.Fatal(err)
			}
			w.put(u.path(w.team), "a file")
		} else {
			u.apply(w.sealWorld)
		}
		_, err := w.resume()
		if err == nil || !strings.Contains(err.Error(), u.want) {
			t.Errorf("%s: the resume answered %v, not a failure naming %q", name, err, u.want)
		}
		if w.has(paths.AdminState(w.team)) {
			t.Errorf("%s: the resume placed the team", name)
		}
	}
}

// Section 6: the check reads carried.json, team.json and units.json before it starts anything;
// one it cannot read fails the check naming it, and nothing is started.
func TestCarryCheckNeedsItsFiles(t *testing.T) {
	for name, u := range map[string]unreadable{
		"carried.json undecodable": {path: paths.CarriedRecord, want: "carried"},
		"team.json missing":        {path: paths.TeamDeclaration, gone: true, want: "team.json is missing"},
		"units.json missing":       {path: paths.UnitsRecord, gone: true, want: "units.json is missing"},
		"units.json undecodable":   {path: paths.UnitsRecord, want: "units"},
	} {
		w := newCheckWorld(t, KindSealed)
		u.apply(w.sealWorld)
		mark := len(w.calls)
		_, err := w.check()
		if err == nil || !strings.Contains(err.Error(), u.want) {
			t.Errorf("%s: the check answered %v, not a failure naming %q", name, err, u.want)
		}
		if slices.Contains(w.calls[mark:], "loopback") {
			t.Errorf("%s: the check started the homeserver", name)
		}
	}
}

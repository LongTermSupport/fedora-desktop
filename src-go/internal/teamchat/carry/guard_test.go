package carry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// CN1, CN2, SG1: each record has its name and path; a value outside the three has neither.
func TestRecordNamesAndPaths(t *testing.T) {
	team := sampleTeam(t)
	for r, want := range map[Record][2]string{
		RecordSeal:   {"seal", paths.SealRecord(team)},
		RecordDrill:  {"drill", paths.DrillRecord(team)},
		RecordImport: {"import", paths.CarryImportRecord(team)},
		Record(9):    {"record 9", ""},
	} {
		if r.String() != want[0] || r.Path(team) != want[1] {
			t.Errorf("record %d: %q at %q", int(r), r.String(), r.Path(team))
		}
	}
}

// SX4: a name over 100 bytes splits at a slash into ustar's prefix and name; one with no such
// slash does not.
func TestSplitName(t *testing.T) {
	long := "hs/" + strings.Repeat("d", 60) + "/" + strings.Repeat("f", 60)
	prefix, base, ok := splitName(long)
	if !ok || prefix+"/"+base != long || len(base) > 100 || len(prefix) > 155 {
		t.Errorf("%q: %q %q %v", long, prefix, base, ok)
	}
	if _, _, ok := splitName("hs/" + strings.Repeat("f", 120)); ok {
		t.Error("a last segment over 100 bytes was split")
	}
}

// guardRoot is a host prefix with the team's admin state and the carry directory made.
func guardRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{paths.AdminState(sampleTeam(t)), paths.CarryDir} {
		if err := os.MkdirAll(root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func place(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// recordPaths are the three records SG1 reads, each with what its refusal must name as the way on.
func recordPaths(team paths.TeamID) map[Record]struct{ path, way string } {
	return map[Record]struct{ path, way string }{
		RecordSeal:   {paths.SealRecord(team), "team remove --team ops --purge"},
		RecordDrill:  {paths.DrillRecord(team), "team export --team ops --drill"},
		RecordImport: {paths.CarryImportRecord(team), "team import --team ops --resume"},
	}
}

// SG1, SG3: each carry record refuses every lock-taking verb's guard, a link in its place too,
// naming the record and the one way on.
func TestGuardRefusesEachCarryRecord(t *testing.T) {
	team := sampleTeam(t)
	for rec, c := range recordPaths(team) {
		for _, link := range []bool{false, true} {
			root := guardRoot(t)
			if link {
				if err := os.Symlink("/nonexistent", root+c.path); err != nil {
					t.Fatal(err)
				}
			} else {
				place(t, root+c.path, []byte("{}"))
			}
			guardRefuses(t, root, team, rec, c.path, c.way)
		}
	}
}

// guardRefuses holds that Guard refuses team under root for rec alone, naming path and way.
func guardRefuses(t *testing.T, root string, team paths.TeamID, rec Record, path, way string) {
	t.Helper()
	err := Guard(root)(team)
	if !errors.Is(err, ErrSealed) {
		t.Fatalf("%v: Guard = %v, want ErrSealed", rec, err)
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), way) {
		t.Errorf("%v: the refusal %q does not name %s and %q", rec, err, path, way)
	}
	found, err := Recorded(root, team)
	if err != nil || len(found) != 1 || found[0] != rec {
		t.Errorf("%v: Recorded = %v, %v", rec, found, err)
	}
}

// selfUID is the test's own uid, the owner of every tree the tests make.
func selfUID(t *testing.T) uint32 {
	t.Helper()
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no stat")
	}
	return st.Uid
}

// SG1: a team with no carry record passes, and is recorded as having none.
func TestGuardPassesAnUncarriedTeam(t *testing.T) {
	root := guardRoot(t)
	if err := Guard(root)(sampleTeam(t)); err != nil {
		t.Fatal(err)
	}
	if found, err := Recorded(root, sampleTeam(t)); err != nil || len(found) != 0 {
		t.Fatalf("Recorded = %v, %v", found, err)
	}
}

// SG1: a record whose presence cannot be told fails the guard, never passes it, and is no
// carry refusal.
func TestGuardFailsWhenItCannotTell(t *testing.T) {
	root := t.TempDir()
	team := sampleTeam(t)
	if err := os.MkdirAll(filepath.Dir(root+paths.AdminState(team)), 0o700); err != nil {
		t.Fatal(err)
	}
	place(t, root+paths.AdminState(team), nil)
	err := Guard(root)(team)
	if err == nil || errors.Is(err, ErrSealed) {
		t.Fatalf("Guard over an unreadable record = %v, want a failure that is not ErrSealed", err)
	}
}

// SG4: team remove's guard admits a sealed team, and refuses a drill or an import under way
// and a restore record (RG2) as every other guard does.
func TestRemoveGuardAdmitsOnlyTheSeal(t *testing.T) {
	team := sampleTeam(t)
	for rec, c := range recordPaths(team) {
		root := guardRoot(t)
		place(t, root+c.path, []byte("{}"))
		err := RemoveGuard(root)(team)
		switch {
		case rec == RecordSeal && err != nil:
			t.Errorf("RemoveGuard of a sealed team = %v, want nil", err)
		case rec != RecordSeal && !errors.Is(err, ErrSealed):
			t.Errorf("%v: RemoveGuard = %v, want ErrSealed", rec, err)
		}
	}
	root := guardRoot(t)
	place(t, root+paths.RestoreRecord(team), []byte("{}"))
	if err := RemoveGuard(root)(team); !errors.Is(err, restore.ErrPending) {
		t.Fatalf("RemoveGuard with a restore recorded = %v, want restore.ErrPending", err)
	}
}

// SG4: team export's guard requires the seal at step sealed; a team not sealed, or sealed only
// to step sealing, is refused; a restore record, a drill or an import record still refuse.
func TestSealedGuard(t *testing.T) {
	team := sampleTeam(t)
	uid := selfUID(t)
	sealed := EncodeSealRecord(SealRecord{Team: team, Step: StepSealed, Seal: sampleSeal(t)})
	sealing := EncodeSealRecord(SealRecord{Team: team, Step: StepSealing, StartedMS: 1, Tail: sampleSeal(t).Tail})
	for name, c := range map[string]struct {
		files map[string][]byte
		owner uint32
		want  error
	}{
		"not sealed":       {files: nil, owner: uid, want: ErrRefused},
		"sealing":          {files: map[string][]byte{paths.SealRecord(team): sealing}, owner: uid, want: ErrRefused},
		"sealed":           {files: map[string][]byte{paths.SealRecord(team): sealed}, owner: uid},
		"restore recorded": {files: map[string][]byte{paths.SealRecord(team): sealed, paths.RestoreRecord(team): []byte("{}")}, owner: uid, want: restore.ErrPending},
		"drill recorded":   {files: map[string][]byte{paths.SealRecord(team): sealed, paths.DrillRecord(team): []byte("{}")}, owner: uid, want: ErrSealed},
		"import recorded":  {files: map[string][]byte{paths.SealRecord(team): sealed, paths.CarryImportRecord(team): []byte("{}")}, owner: uid, want: ErrSealed},
		"not decodable":    {files: map[string][]byte{paths.SealRecord(team): []byte(`{"v":1}`)}, owner: uid},
		"another owner":    {files: map[string][]byte{paths.SealRecord(team): sealed}, owner: uid + 1},
	} {
		root := guardRoot(t)
		for path, data := range c.files {
			place(t, root+path, data)
		}
		err := SealedGuard(root, c.owner)(team)
		switch {
		case name == "sealed":
			if err != nil {
				t.Errorf("%s: SealedGuard = %v, want nil", name, err)
			}
		case c.want != nil:
			if !errors.Is(err, c.want) {
				t.Errorf("%s: SealedGuard = %v, want %v", name, err, c.want)
			}
		default:
			if err == nil || errors.Is(err, ErrRefused) || errors.Is(err, ErrSealed) {
				t.Errorf("%s: SealedGuard = %v, want a failure that is no refusal", name, err)
			}
		}
	}
}

package admin

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// carriedWorld is a bare world where team alpha has the record at path.
func carriedWorld(t *testing.T, path func(paths.TeamID) string) *world {
	t.Helper()
	w := bareWorld(t)
	for _, dir := range []string{paths.AdminState(alpha(t)), paths.CarryDir} {
		if err := os.MkdirAll(w.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(w.root+path(alpha(t)), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

// runAs runs teamchat-admin over the world as a caller of the verb's own class.
func (w *world) runAs(class Class, args ...string) outcome {
	w.t.Helper()
	proc := procFor(class)
	if class == ClassAny {
		proc = rootProc()
	}
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: proc, Digest: digest, Sys: w.sys(),
	})
	return outcome{code, stdout.String(), stderr.String()}
}

// gateRow is one record's SG1 row: the record, and the verbs (with the flags after --team)
// it admits.
type gateRow struct {
	record  func(paths.TeamID) string
	way     string
	admits  map[string]bool
	flagged map[string][]string
}

func carryGates() map[string]gateRow {
	return map[string]gateRow{
		"seal": {
			record: paths.SealRecord, way: "team remove --team alpha --purge",
			admits:  map[string]bool{"team remove": true, "team export": true, "team seal": true},
			flagged: map[string][]string{"team export": {"--drill"}},
		},
		"drill": {
			record: paths.DrillRecord, way: "team export --team alpha --drill",
			admits:  map[string]bool{"team export --drill": true},
			flagged: map[string][]string{"team export": {"--drill"}},
		},
		"import": {
			record: paths.CarryImportRecord, way: "team import --team alpha --resume",
			admits:  map[string]bool{"team import --resume": true, "team import --discard": true},
			flagged: map[string][]string{"team import": {"--resume", "--discard"}},
		},
	}
}

// SC8, SG1: on a team with each carry record, the whole verb table is run with --team; every
// root verb refuses (70), naming the record and the one way on, but those the record admits: a
// sealed team runs only team remove, team export (no --drill) and team seal; a drill only
// team export --drill; an import only team import --resume or --discard. A verb of another
// class is never judged: it cannot read root's records, and none of them changes a team's state.
func TestTheVerbTableOnACarriedTeam(t *testing.T) {
	for state, g := range carryGates() {
		w := carriedWorld(t, g.record)
		record := w.root + g.record(alpha(t))
		for name, class := range specVerbs() {
			forms := [][]string{nil}
			for _, flag := range g.flagged[name] {
				forms = append(forms, []string{flag})
			}
			for _, extra := range forms {
				args := append(append(strings.Fields(name), "--team", "alpha"), extra...)
				form := strings.Join(append([]string{name}, extra...), " ")
				got := w.runAs(class, args...)
				refused := got.code == ExitRefused && strings.Contains(got.stderr, record) && strings.Contains(got.stderr, g.way)
				admitted := g.admits[form] || class != ClassRoot
				if admitted == refused {
					t.Errorf("%s: %s = %+v, admitted %v", state, form, got, admitted)
				}
				if admitted && strings.Contains(got.stderr, record) {
					t.Errorf("%s: %s, admitted, still names the record: %q", state, form, got.stderr)
				}
			}
		}
	}
}

// drillImported is a bare world where team alpha was imported from an export of kind.
func drillImported(t *testing.T, kind carry.Kind) *world {
	t.Helper()
	w := carriedWorld(t, paths.CarriedRecord)
	raw := carry.EncodeCarriedRecord(carry.CarriedRecord{
		Team: alpha(t), Kind: kind, SHA256: strings.Repeat("a", 64), SealedMS: 1, ImportedMS: 2,
		Tail: []carry.TailEntry{{Room: "main", EventID: "$event-1", TsMS: 1}},
	})
	if err := os.WriteFile(w.root+paths.CarriedRecord(alpha(t)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

// SG2: on a team a drill export was imported as, the whole verb table is run with --team; every
// root verb refuses (70: "a drill import never joins a real team") but team carry-check and
// team remove; a verb of another class is not judged. A sealed import is not judged by SG2.
func TestTheVerbTableOnADrillImport(t *testing.T) {
	const never = "a drill import never joins a real team"
	admitted := map[string]bool{"team carry-check": true, "team remove": true}
	for _, kind := range []carry.Kind{carry.KindDrill, carry.KindSealed} {
		w := drillImported(t, kind)
		for name, class := range specVerbs() {
			got := w.runAs(class, append(strings.Fields(name), "--team", "alpha")...)
			refused := got.code == ExitRefused && strings.Contains(got.stderr, never)
			if want := kind == carry.KindDrill && !admitted[name] && class == ClassRoot; refused != want {
				t.Errorf("%s: %s = %+v, refusal wanted %v", kind, name, got, want)
			}
			if !refused && strings.Contains(got.stderr, never) {
				t.Errorf("%s: %s names the drill import: %q", kind, name, got.stderr)
			}
		}
	}
}

// SG1: a verb given no --team, or a team with no carry record, is not judged by the gate.
func TestTheCarryGateJudgesOnlyTheNamedTeam(t *testing.T) {
	w := carriedWorld(t, paths.SealRecord)
	if got := w.runAs(ClassAny, "version"); got.code != 0 {
		t.Errorf("version on a host with a sealed team = %+v", got)
	}
	if got := w.runAs(ClassRoot, "backups", "--team", "beta"); strings.Contains(got.stderr, "sealed") {
		t.Errorf("backups of another team = %+v, refused by alpha's seal", got)
	}
}

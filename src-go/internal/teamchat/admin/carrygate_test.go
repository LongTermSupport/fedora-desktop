package admin

import (
	"bytes"
	"os"
	"strings"
	"testing"

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
// verb refuses (70), naming the record and the one way on, but those the record admits: a
// sealed team runs only team remove, team export (no --drill) and team seal; a drill only
// team export --drill; an import only team import --resume or --discard.
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
				if g.admits[form] == refused {
					t.Errorf("%s: %s = %+v, admitted %v", state, form, got, g.admits[form])
				}
				if g.admits[form] && strings.Contains(got.stderr, record) {
					t.Errorf("%s: %s, admitted, still names the record: %q", state, form, got.stderr)
				}
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

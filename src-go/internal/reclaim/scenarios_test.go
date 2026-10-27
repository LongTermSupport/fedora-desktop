package reclaim

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// table is testdata/scenarios.json (SPEC-reclaim.md section 6), the one table the Go and the
// bash are both judged by.
type table struct {
	Changing  []string   `json:"changing"`
	Scenarios []scenario `json:"scenarios"`
}

type scenario struct {
	Name      string        `json:"name"`
	Args      []string      `json:"args"`
	NoTTY     bool          `json:"noTty"`
	Installed []string      `json:"installed"`
	Programs  []programJSON `json:"programs"`
	Home      []string      `json:"home"`
	Typed     []string      `json:"typed"`
	Exit      int           `json:"exit"`
	Changes   []string      `json:"changes"`
	HomeAfter []string      `json:"homeAfter"`
	// BashDiffers is the departure that makes the bash differ here; the Go is judged anyway.
	BashDiffers string `json:"bashDiffers"`
}

type programJSON struct {
	Argv   []string `json:"argv"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Exit   int      `json:"exit"`
}

// defaultChanging is the table's list of changing commands, for the expectation tests.
var defaultChanging = mustReadTable().Changing

func readTable() (table, error) {
	var tb table
	raw, err := os.ReadFile(filepath.Join("testdata", "scenarios.json"))
	if err != nil {
		return tb, err
	}
	err = json.Unmarshal(raw, &tb)
	return tb, err
}

func mustReadTable() table {
	tb, err := readTable()
	if err != nil {
		panic(err)
	}
	return tb
}

func loadTable(t *testing.T) table {
	t.Helper()
	tb, err := readTable()
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func TestScenarios(t *testing.T) {
	tb := loadTable(t)
	if len(tb.Scenarios) < 20 {
		t.Fatalf("only %d scenarios", len(tb.Scenarios))
	}
	for _, sc := range tb.Scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			w := newWorld(t)
			w.installed = map[string]bool{}
			w.install(sc.Installed...)
			for _, p := range sc.Programs {
				w.answerErr(p.Argv, p.Stdout, p.Stderr, p.Exit)
			}
			for _, f := range sc.Home {
				w.file(f, "x")
			}
			w.noTTY, w.stdinTTY = sc.NoTTY, !sc.NoTTY
			w.typed(sc.Typed...)
			if len(sc.Typed) == 0 {
				w.tty = ""
			}
			wantCode(t, w.run(sc.Args...), term.ExitCode(sc.Exit))
			if got := w.changes(tb.Changing); !slices.Equal(got, sc.Changes) {
				t.Errorf("changes %q, want %q", got, sc.Changes)
			}
			if got := homeFiles(t, w.home); !slices.Equal(got, sc.HomeAfter) {
				t.Errorf("home after %q, want %q", got, sc.HomeAfter)
			}
		})
	}
}

// homeFiles is every file under home, relative and sorted.
func homeFiles(t *testing.T, home string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(home, p)
			if relErr != nil {
				return relErr
			}
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// TestScenarioTableCanFail proves the judgement catches a wrong command list: a scenario whose
// expected changes are altered no longer matches what the Go ran.
func TestScenarioTableCanFail(t *testing.T) {
	tb := loadTable(t)
	i := slices.IndexFunc(tb.Scenarios, func(s scenario) bool { return s.Name == "packages-yes" })
	sc := tb.Scenarios[i]
	w := newWorld(t)
	w.install(sc.Installed...)
	for _, p := range sc.Programs {
		w.answerErr(p.Argv, p.Stdout, p.Stderr, p.Exit)
	}
	w.typed("3", "n", "q")
	w.run()
	if slices.Equal(w.changes(tb.Changing), sc.Changes) {
		t.Fatal("answering no still matched the yes scenario's changes")
	}
}

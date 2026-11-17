package admin

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// importDigest is a --sha256 of the grammar.
const importDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func (f *fakeCarrier) Import(_ context.Context, team paths.TeamID, flags carry.ImportFlags) ([]byte, error) {
	f.imports = append(f.imports, importCall{team.String(), flags})
	return f.out, f.err
}

// importCall is one import the engine was asked.
type importCall struct {
	team  string
	flags carry.ImportFlags
}

func (w *sealVerbWorld) importVerb(args ...string) outcome {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: append([]string{"team", "import"}, args...), Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: w.proc, Digest: digest, Sys: w.sys,
	})
	return outcome{code, stdout.String(), stderr.String()}
}

// SI11, SI10: team import runs the engine's import with its flags and prints its answer.
func TestTeamImportRunsTheImport(t *testing.T) {
	for name, c := range map[string]struct {
		args []string
		want carry.ImportFlags
	}{
		"sealed":  {nil, carry.ImportFlags{SHA256: importDigest}},
		"drill":   {[]string{"--drill"}, carry.ImportFlags{SHA256: importDigest, Drill: true}},
		"resume":  {[]string{"--resume"}, carry.ImportFlags{SHA256: importDigest, Resume: true}},
		"discard": {[]string{"--discard", "--drill"}, carry.ImportFlags{SHA256: importDigest, Discard: true, Drill: true}},
	} {
		w := newSealVerbWorld(t)
		w.engine.out = []byte(`{"v":1,"team":"alpha","kind":"sealed"}`)
		got := w.importVerb(append([]string{"--team", "alpha", "--sha256", importDigest}, c.args...)...)
		if got.code != term.ExitOK || got.stdout != string(w.engine.out)+"\n" || got.stderr != "" {
			t.Fatalf("%s: %+v", name, got)
		}
		if !slices.Equal(w.engine.imports, []importCall{{"alpha", c.want}}) {
			t.Errorf("%s: imports %+v", name, w.engine.imports)
		}
	}
}

// SE: --sha256 missing or not 64 lower-case hex digits, --resume with --discard, and --team
// missing are 64, with nothing asked of the engine.
func TestTeamImportUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"no sha256":         {"--team", "alpha"},
		"a short sha256":    {"--team", "alpha", "--sha256", importDigest[1:]},
		"an upper sha256":   {"--team", "alpha", "--sha256", strings.ToUpper(importDigest)},
		"a non-hex sha256":  {"--team", "alpha", "--sha256", strings.Repeat("g", 64)},
		"resume discard":    {"--team", "alpha", "--sha256", importDigest, "--resume", "--discard"},
		"no team":           {"--sha256", importDigest},
		"an unknown flag":   {"--team", "alpha", "--sha256", importDigest, "--force"},
		"a path":            {"--team", "alpha", "--sha256", importDigest, "/tmp/x.tar"},
		"sha256 with none":  {"--team", "alpha", "--sha256"},
		"drill with a word": {"--team", "alpha", "--sha256", importDigest, "--drill", "yes"},
	} {
		w := newSealVerbWorld(t)
		got := w.importVerb(args...)
		if got.code != term.ExitUsage || got.stdout != "" || len(w.engine.imports) != 0 {
			t.Errorf("%s: %+v, imports %v", name, got, w.engine.imports)
		}
	}
}

// SE: the import's failures by their class: a refusal or a record 70, a busy lock 75, a failed
// step 1, the setting 78.
func TestTeamImportExitCodes(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want term.ExitCode
	}{
		"refused":  {&carry.RefusedError{Problem: "team alpha has state on this guest"}, ExitRefused},
		"a record": {&carry.RecordedError{Team: alpha(t), Record: carry.RecordImport}, ExitRefused},
		"busy":     {&teamlock.BusyError{Path: "/run/x.lock", Holder: "apply alpha pid=1 since=1"}, ExitBusy},
		"not done": {errors.New("import not done: the archive's sha256 is x"), term.ExitFailure},
		"setting":  {&configError{"there is no " + paths.CarryMaxArchiveFile}, ExitConfig},
	} {
		w := newSealVerbWorld(t)
		w.engine.err = c.err
		got := w.importVerb("--team", "alpha", "--sha256", importDigest)
		if got.code != c.want || got.stderr == "" || len(w.engine.imports) != 1 {
			t.Errorf("%s: %+v, want %d", name, got, c.want)
		}
	}
}

// BV4: team import is root's only (77).
func TestTeamImportIsRootsOnly(t *testing.T) {
	w := newSealVerbWorld(t)
	w.proc = ownerProc()
	if got := w.importVerb("--team", "alpha", "--sha256", importDigest); got.code != ExitClass || len(w.engine.imports) != 0 {
		t.Fatalf("%+v", got)
	}
}

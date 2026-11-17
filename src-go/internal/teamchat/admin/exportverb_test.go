package admin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func (f *fakeCarrier) Export(_ context.Context, team paths.TeamID) ([]byte, error) {
	f.exports = append(f.exports, team.String())
	return f.out, f.err
}

func (f *fakeCarrier) Drill(_ context.Context, team paths.TeamID) ([]byte, error) {
	f.drills = append(f.drills, team.String())
	return f.out, f.err
}

func (w *sealVerbWorld) export(args ...string) outcome {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: append([]string{"team", "export"}, args...), Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: w.proc, Digest: digest, Sys: w.sys,
	})
	return outcome{code, stdout.String(), stderr.String()}
}

// SX6: team export runs the engine's export for the team and prints its answer.
func TestTeamExportPrintsTheAnswer(t *testing.T) {
	w := newSealVerbWorld(t)
	w.engine.out = []byte(`{"v":1,"team":"alpha","kind":"sealed"}`)
	got := w.export("--team", "alpha")
	if got.code != term.ExitOK || got.stdout != string(w.engine.out)+"\n" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
	if !slices.Equal(w.engine.exports, []string{"alpha"}) || len(w.engine.seals) != 0 {
		t.Errorf("exports %v, seals %v", w.engine.exports, w.engine.seals)
	}
}

// SD3: team export --drill runs the engine's drill, not the sealed export, and prints its answer.
func TestTeamExportDrillRunsTheDrill(t *testing.T) {
	w := newSealVerbWorld(t)
	w.engine.out = []byte(`{"v":1,"team":"alpha","kind":"drill"}`)
	got := w.export("--team", "alpha", "--drill")
	if got.code != term.ExitOK || got.stdout != string(w.engine.out)+"\n" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
	if !slices.Equal(w.engine.drills, []string{"alpha"}) || len(w.engine.exports) != 0 || len(w.engine.seals) != 0 {
		t.Errorf("drills %v, exports %v, seals %v", w.engine.drills, w.engine.exports, w.engine.seals)
	}
}

// SE, SD4, SD5: the drill's failures by their class: a sealed team or another refusal 70, the
// team left down by a failed Serve 1, a busy lock 75.
func TestTeamExportDrillExitCodes(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want term.ExitCode
	}{
		"sealed":  {&carry.RecordedError{Team: mustTeam(t, "alpha"), Record: carry.RecordSeal}, ExitRefused},
		"refused": {&carry.RefusedError{Problem: "team alpha is of migration origin"}, ExitRefused},
		"down":    {errors.New("the team is down: team export --team alpha --drill again after fixing x"), term.ExitFailure},
		"busy":    {&teamlock.BusyError{Path: "/run/x.lock", Holder: "apply alpha pid=1 since=1"}, ExitBusy},
	} {
		w := newSealVerbWorld(t)
		w.engine.err = c.err
		got := w.export("--team", "alpha", "--drill")
		if got.code != c.want || got.stderr == "" || len(w.engine.drills) != 1 {
			t.Errorf("%s: %+v, want %d", name, got, c.want)
		}
	}
}

// SE: --team missing or bad, and any other flag or argument, are 64 with nothing asked of the
// engine; --drill takes no value.
func TestTeamExportUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":            nil,
		"a bad team":         {"--team", "Alpha!"},
		"drill with a value": {"--team", "alpha", "--drill", "yes"},
		"drill twice":        {"--team", "alpha", "--drill", "--drill"},
		"drill alone":        {"--drill"},
		"an unknown flag":    {"--team", "alpha", "--force"},
		"an argument":        {"--team", "alpha", "now"},
		"team twice":         {"--team", "alpha", "--team", "alpha"},
	} {
		w := newSealVerbWorld(t)
		got := w.export(args...)
		if got.code != term.ExitUsage || got.stdout != "" || len(w.engine.exports) != 0 || len(w.engine.drills) != 0 {
			t.Errorf("%s: %+v, exports %v, drills %v", name, got, w.engine.exports, w.engine.drills)
		}
	}
}

// BV4: team export is root's only (77).
func TestTeamExportIsRootsOnly(t *testing.T) {
	w := newSealVerbWorld(t)
	w.proc = ownerProc()
	if got := w.export("--team", "alpha"); got.code != ExitClass || len(w.engine.exports) != 0 {
		t.Fatalf("%+v", got)
	}
}

// SE, CN5: the export's failures by their class: a refusal (not sealed, a record, a mode, a cap)
// 70, a busy lock 75, a failed step or a changed export 1; the archive cap's setting absent or
// bad, a wiring without the carry, or whose carry seam fails, 78.
func TestTeamExportExitCodes(t *testing.T) {
	for name, c := range map[string]struct {
		err, seamErr error
		noCarry      bool
		want         term.ExitCode
	}{
		"refused":      {err: &carry.RefusedError{Problem: "team alpha is not sealed"}, want: ExitRefused},
		"a record":     {err: &carry.RecordedError{Team: mustTeam(t, "alpha"), Record: carry.RecordImport}, want: ExitRefused},
		"busy":         {err: &teamlock.BusyError{Path: "/run/x.lock", Holder: "apply alpha pid=1 since=1"}, want: ExitBusy},
		"a step":       {err: errors.New("the existing export differs"), want: term.ExitFailure},
		"the setting":  {err: &configError{"there is no " + paths.CarryMaxArchiveFile}, want: ExitConfig},
		"no carry":     {noCarry: true, want: ExitConfig},
		"seam failure": {seamErr: errors.New("no ctl account"), want: ExitConfig},
	} {
		w := newSealVerbWorld(t)
		w.engine.err, w.seamErr = c.err, c.seamErr
		if c.noCarry {
			w.sys.Carry = nil
		}
		got := w.export("--team", "alpha")
		if got.code != c.want || got.stderr == "" {
			t.Errorf("%s: %+v, want %d", name, got, c.want)
		}
	}
}

// CN5: the archive cap is paths.CarryMaxArchiveFile under root, root's, a positive decimal number
// of bytes with at most one newline; absent, unreadable, empty, zero, negative or anything else is
// 78 naming the setting.
func TestCarryMaxArchiveFile(t *testing.T) {
	for _, content := range []string{"1073741824", "1073741824\n"} {
		root := t.TempDir()
		hostFile(t, root, paths.CarryMaxArchiveFile, content, 0o644)
		got, err := carryMaxArchive(root, self(t))()
		if err != nil || got != 1073741824 {
			t.Fatalf("%q: %d, %v", content, got, err)
		}
	}
	for name, c := range map[string]struct {
		content string
		mode    os.FileMode
	}{
		"empty":          {"", 0o644},
		"zero":           {"0", 0o644},
		"negative":       {"-1", 0o644},
		"a plus":         {"+5", 0o644},
		"not a number":   {"1 GiB", 0o644},
		"two newlines":   {"5\n\n", 0o644},
		"over int64":     {"99999999999999999999", 0o644},
		"group writable": {"5", 0o664},
	} {
		root := t.TempDir()
		hostFile(t, root, paths.CarryMaxArchiveFile, c.content, c.mode)
		_, err := carryMaxArchive(root, self(t))()
		if ExitCodeOf(err) != ExitConfig || !strings.Contains(err.Error(), paths.CarryMaxArchiveVar) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := carryMaxArchive(t.TempDir(), self(t))(); ExitCodeOf(err) != ExitConfig || !strings.Contains(err.Error(), paths.CarryMaxArchiveVar) {
		t.Fatalf("missing: %v", err)
	}
	root := t.TempDir()
	hostFile(t, root, paths.CarryMaxArchiveFile, "5", 0o644)
	if _, err := carryMaxArchive(root, self(t)+1)(); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("another owner: %v", err)
	}
}

package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeDriller is the drill: it records each call and answers out, err and cleaned.
type fakeDriller struct {
	calls    []string
	out      drill.Outcome
	err      error
	cleaned  []paths.TeamID
	cleanErr error
}

func (f *fakeDriller) Run(_ context.Context, port int, where restore.Where) (drill.Outcome, error) {
	f.calls = append(f.calls, fmt.Sprintf("run %d %s", port, where))
	return f.out, f.err
}

func (f *fakeDriller) Clean(context.Context) ([]paths.TeamID, error) {
	f.calls = append(f.calls, "clean")
	return f.cleaned, f.cleanErr
}

type drillWorld struct {
	t       *testing.T
	sys     *System
	driller *fakeDriller
	seams   int
	seamErr error
}

func newDrillWorld(t *testing.T) *drillWorld {
	t.Helper()
	w := &drillWorld{t: t, sys: &System{}, driller: &fakeDriller{}}
	w.sys.Drill = func(context.Context) (Driller, error) {
		w.seams++
		if w.seamErr != nil {
			return nil, w.seamErr
		}
		return w.driller, nil
	}
	return w
}

func (w *drillWorld) verb(proc *fakeProc, args ...string) outcome {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: append([]string{"restore-drill"}, args...), Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: proc, Digest: digest, Sys: w.sys,
	})
	return outcome{code, stdout.String(), stderr.String()}
}

func scratchTeam(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("rd-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The grammar: --port P (1024 to 65535) runs the drill, --clean removes the leftovers, both
// clean first; anything else is 64, with nothing asked of the drill.
func TestRestoreDrillUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":             nil,
		"no port value":       {"--port"},
		"a port too low":      {"--port", "80"},
		"a port too high":     {"--port", "65536"},
		"a port not a number": {"--port", "84x"},
		"a padded port":       {"--port", "08470"},
		"twice":               {"--port", "8470", "--port", "8471"},
		"an unknown flag":     {"--team", "alpha"},
		"an argument":         {"8470"},
		"a valued clean":      {"--clean", "yes"},
		"an unknown where":    {"--port", "8470", "--where", "lab"},
		"an empty where":      {"--port", "8470", "--where", ""},
		"a where twice":       {"--port", "8470", "--where", "host", "--where", "vm-lab"},
		"a where to clean":    {"--clean", "--where", "vm-lab"},
	} {
		w := newDrillWorld(t)
		got := w.verb(rootProc(), args...)
		if got.code != term.ExitUsage || got.stdout != "" || w.seams != 0 {
			t.Errorf("%s: %+v, %d seams", name, got, w.seams)
		}
	}
	// The port is the operator's, given on the command line: nothing here reads the play's
	// teamchat_restore_drill_port, so the refusal does not call it declared (batch 26 review).
	w := newDrillWorld(t)
	got := w.verb(rootProc(), "--port", "80")
	if !strings.Contains(got.stderr, "--port is the port the drill's scratch team listens on") || strings.Contains(got.stderr, "declared") {
		t.Errorf("a port too low: %q", got.stderr)
	}
}

// A drill that passes prints its team, each clause's verdict and that the proof was written.
func TestRestoreDrillRuns(t *testing.T) {
	w := newDrillWorld(t)
	w.driller.out = drill.Outcome{Team: scratchTeam(t), Written: true, Checks: map[restore.Clause]restore.Verdict{
		restore.ClauseHC5: restore.VerdictPass, restore.ClausePoolSeat: restore.VerdictSkipped,
	}}
	got := w.verb(rootProc(), "--port", "8470")
	if got.code != term.ExitOK || !slices.Equal(w.driller.calls, []string{"run 8470 host"}) {
		t.Fatalf("%+v, calls %q", got, w.driller.calls)
	}
	want := `{"v":1,"cleaned":[],"team":"rd-0123456789ab","checks":{"H-C5":"pass","H-C15.pool-seat":"skipped"},"failed":{},"proof":true}` + "\n"
	if got.stdout != want {
		t.Errorf("stdout = %q\nwant     %q", got.stdout, want)
	}
}

// --clean alone removes the leftovers and runs no drill; with --port it cleans first.
func TestRestoreDrillCleans(t *testing.T) {
	w := newDrillWorld(t)
	w.driller.cleaned = []paths.TeamID{scratchTeam(t)}
	got := w.verb(rootProc(), "--clean")
	if got.code != term.ExitOK || !slices.Equal(w.driller.calls, []string{"clean"}) || got.stdout != `{"v":1,"cleaned":["rd-0123456789ab"]}`+"\n" {
		t.Fatalf("%+v, calls %q", got, w.driller.calls)
	}
	w = newDrillWorld(t)
	if got = w.verb(rootProc(), "--clean", "--port", "8470"); got.code != term.ExitOK || !slices.Equal(w.driller.calls, []string{"clean", "run 8470 host"}) {
		t.Fatalf("%+v, calls %q", got, w.driller.calls)
	}
}

// A failed clean-up is its error, and no drill runs after it.
func TestRestoreDrillCleanFails(t *testing.T) {
	w := newDrillWorld(t)
	w.driller.cleanErr = errors.New("cleaning rd-0123456789ab: the purge failed")
	got := w.verb(rootProc(), "--clean", "--port", "8470")
	if got.code != term.ExitFailure || !strings.Contains(got.stderr, "the purge failed") || !slices.Equal(w.driller.calls, []string{"clean"}) {
		t.Fatalf("%+v, calls %q", got, w.driller.calls)
	}
}

// A drill refused (beside a team, a leftover, a port in use) is 70; a failed check is 1 with the
// outcome printed, naming the clause; any other failure is its own code.
func TestRestoreDrillFailures(t *testing.T) {
	failed := drill.Outcome{Team: scratchTeam(t), Failed: map[restore.Clause][]string{restore.ClauseEpoch: {"did not change"}}}
	for name, c := range map[string]struct {
		out    drill.Outcome
		err    error
		code   term.ExitCode
		stdout string
	}{
		"refused": {err: fmt.Errorf("%w: this machine holds team ops", drill.ErrRefused), code: ExitRefused},
		"a failed check": {
			out: failed, err: fmt.Errorf("%w: H-C15.epoch (did not change)", drill.ErrFailed), code: term.ExitFailure,
			stdout: `{"v":1,"cleaned":[],"team":"rd-0123456789ab","checks":{},"failed":{"H-C15.epoch":["did not change"]},"proof":false}` + "\n",
		},
		"a failed operation": {err: errors.New("the backup: no space"), code: term.ExitFailure},
	} {
		w := newDrillWorld(t)
		w.driller.out, w.driller.err = c.out, c.err
		got := w.verb(rootProc(), "--port", "8470")
		if got.code != c.code || got.stdout != c.stdout || !strings.Contains(got.stderr, c.err.Error()) {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// A wiring with no drill is 78; the seam's own failure is its code.
func TestRestoreDrillSeam(t *testing.T) {
	w := newDrillWorld(t)
	w.sys.Drill = nil
	if got := w.verb(rootProc(), "--port", "8470"); got.code != ExitConfig {
		t.Errorf("no seam: %+v", got)
	}
	w = newDrillWorld(t)
	w.seamErr = fmt.Errorf("%w: the drill port 8470 is in use", drill.ErrRefused)
	if got := w.verb(rootProc(), "--port", "8470"); got.code != ExitRefused || !strings.Contains(got.stderr, "in use") {
		t.Errorf("seam refused: %+v", got)
	}
}

// --help is the verb's text on stdout, alone; it says the record is never copied and the two
// clauses skipped.
func TestRestoreDrillHelp(t *testing.T) {
	w := newDrillWorld(t)
	got := w.verb(rootProc(), "--help")
	if got.code != term.ExitOK || got.stderr != "" || !strings.Contains(got.stdout, "never copied") || !strings.Contains(got.stdout, "skipped") ||
		!strings.Contains(got.stdout, "--where host|vm-lab") || w.seams != 0 {
		t.Fatalf("%+v", got)
	}
	if got = w.verb(rootProc(), "--help", "--clean"); got.code != term.ExitUsage {
		t.Fatalf("--help --clean: %+v", got)
	}
}

// Only root runs the drill.
func TestRestoreDrillIsRoots(t *testing.T) {
	for _, proc := range []*fakeProc{childProc(), ownerProc()} {
		w := newDrillWorld(t)
		if got := w.verb(proc, "--port", "8470"); got.code != ExitClass || got.stdout != "" || w.seams != 0 {
			t.Errorf("uid %d: %+v", proc.uid, got)
		}
	}
}

// --where says where the drill runs, recorded in the proof: host by default, or vm-lab.
func TestRestoreDrillWhere(t *testing.T) {
	for want, args := range map[string][]string{
		"run 8470 host":   {"--port", "8470"},
		"run 8470 host ":  {"--port", "8470", "--where", "host"},
		"run 8470 vm-lab": {"--where", "vm-lab", "--port", "8470"},
	} {
		w := newDrillWorld(t)
		if got := w.verb(rootProc(), args...); got.code != term.ExitOK || !slices.Equal(w.driller.calls, []string{strings.TrimSpace(want)}) {
			t.Errorf("%q: %+v, calls %q", args, got, w.driller.calls)
		}
	}
}

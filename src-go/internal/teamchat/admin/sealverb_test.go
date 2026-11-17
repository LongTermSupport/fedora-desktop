package admin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeCarrier is the carry engine: it records each seal, export and drill and answers out and err.
type fakeCarrier struct {
	seals, exports, drills, checks []string
	imports                        []importCall
	out                            []byte
	err                            error
}

func (f *fakeCarrier) Seal(_ context.Context, team paths.TeamID) ([]byte, error) {
	f.seals = append(f.seals, team.String())
	return f.out, f.err
}

type sealVerbWorld struct {
	t       *testing.T
	sys     *System
	engine  *fakeCarrier
	seamErr error
	proc    *fakeProc
}

func newSealVerbWorld(t *testing.T) *sealVerbWorld {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{paths.AdminState(alpha(t)), paths.AdminRunDir} {
		if err := os.MkdirAll(root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w := &sealVerbWorld{t: t, engine: &fakeCarrier{out: []byte(`{"v":1,"team":"alpha","step":"sealed","changed":[]}`)}, proc: rootProc()}
	w.sys = &System{Host: host.Deps{Root: root, LockDir: root + paths.AdminRunDir, Clock: &stepClock{now: time.UnixMilli(t0ms)}, StateOwnerUID: selfUID(t)}}
	w.sys.Carry = func() (Carrier, error) {
		if w.seamErr != nil {
			return nil, w.seamErr
		}
		return w.engine, nil
	}
	return w
}

func (w *sealVerbWorld) verb(args ...string) outcome {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: append([]string{"team", "seal"}, args...), Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: w.proc, Digest: digest, Sys: w.sys,
	})
	return outcome{code, stdout.String(), stderr.String()}
}

// SC7: team seal runs the engine for the team and prints its answer, the sealed document.
func TestTeamSealPrintsTheSealedDocument(t *testing.T) {
	w := newSealVerbWorld(t)
	got := w.verb("--team", "alpha")
	if got.code != term.ExitOK || got.stdout != string(w.engine.out)+"\n" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
	if !slices.Equal(w.engine.seals, []string{"alpha"}) {
		t.Errorf("seals = %v", w.engine.seals)
	}
}

// SE: --drill on team seal, --team missing or bad, and any other flag or argument are 64, with
// nothing asked of the engine.
func TestTeamSealUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":         nil,
		"a bad team":      {"--team", "Alpha!"},
		"drill":           {"--team", "alpha", "--drill"},
		"an unknown flag": {"--team", "alpha", "--force"},
		"an argument":     {"--team", "alpha", "now"},
		"team twice":      {"--team", "alpha", "--team", "alpha"},
	} {
		w := newSealVerbWorld(t)
		got := w.verb(args...)
		if got.code != term.ExitUsage || got.stdout != "" || len(w.engine.seals) != 0 {
			t.Errorf("%s: %+v, seals %v", name, got, w.engine.seals)
		}
	}
	if got := newSealVerbWorld(t).verb("--team", "alpha", "--drill"); !strings.Contains(got.stderr, "team export --team alpha --drill") {
		t.Errorf("the --drill refusal does not name the drill export: %q", got.stderr)
	}
}

// BV4: team seal is root's only (77).
func TestTeamSealIsRootsOnly(t *testing.T) {
	w := newSealVerbWorld(t)
	w.proc = ownerProc()
	if got := w.verb("--team", "alpha"); got.code != ExitClass || len(w.engine.seals) != 0 {
		t.Fatalf("%+v", got)
	}
}

// SE: the engine's failures by their class: a refusal 70, a busy lock 75, a lost server 69, a
// failed step 1; a wiring without the carry, or whose carry seam fails, 78. A document answered
// beside a failure is still printed.
func TestTeamSealExitCodes(t *testing.T) {
	for name, c := range map[string]struct {
		err, seamErr error
		noCarry      bool
		want         term.ExitCode
	}{
		"refused":      {err: &carry.RefusedError{Problem: "the roster event and roster.json disagree"}, want: ExitRefused},
		"sealed":       {err: &carry.RecordedError{Team: mustTeam(t, "alpha"), Record: carry.RecordDrill}, want: ExitRefused},
		"busy":         {err: &teamlock.BusyError{Path: "/run/x.lock", Holder: "apply alpha pid=1 since=1"}, want: ExitBusy},
		"lost":         {err: &ChildError{Class: ClassUnreachable, Message: "the homeserver was lost"}, want: ExitUnreachable},
		"a step":       {err: errors.New("the stop failed"), want: term.ExitFailure},
		"no carry":     {noCarry: true, want: ExitConfig},
		"seam failure": {seamErr: errors.New("no ctl account"), want: ExitConfig},
	} {
		w := newSealVerbWorld(t)
		w.engine.err, w.seamErr = c.err, c.seamErr
		if c.noCarry {
			w.sys.Carry = nil
		}
		got := w.verb("--team", "alpha")
		if got.code != c.want || got.stderr == "" {
			t.Errorf("%s: %+v, want %d", name, got, c.want)
		}
	}
}

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

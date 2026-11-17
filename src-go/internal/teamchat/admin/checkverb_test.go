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

func (f *fakeCarrier) Check(_ context.Context, team paths.TeamID) ([]byte, error) {
	f.checks = append(f.checks, team.String())
	return f.out, f.err
}

func (w *sealVerbWorld) checkVerb(args ...string) outcome {
	w.t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{
		Args: append([]string{"team", "carry-check"}, args...), Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Proc: w.proc, Digest: digest, Sys: w.sys,
	})
	return outcome{code, stdout.String(), stderr.String()}
}

// SPEC-carry section 6: team carry-check runs the engine's check for the team and prints its
// answer.
func TestTeamCarryCheckRunsTheCheck(t *testing.T) {
	w := newSealVerbWorld(t)
	w.engine.out = []byte(`{"v":1,"team":"alpha","kind":"drill","rooms":[],"ok":true}`)
	got := w.checkVerb("--team", "alpha")
	if got.code != term.ExitOK || got.stdout != string(w.engine.out)+"\n" || got.stderr != "" {
		t.Fatalf("%+v", got)
	}
	if !slices.Equal(w.engine.checks, []string{"alpha"}) {
		t.Errorf("checks %v", w.engine.checks)
	}
}

// SE: a copy that is not whole is 1, with the answer still printed beside the failure.
func TestTeamCarryCheckPrintsTheAnswerOfACopyNotWhole(t *testing.T) {
	w := newSealVerbWorld(t)
	w.engine.out = []byte(`{"v":1,"team":"alpha","ok":false}`)
	w.engine.err = errors.New("room reports lacks its sealed newest event")
	got := w.checkVerb("--team", "alpha")
	if got.code != term.ExitFailure || got.stdout != string(w.engine.out)+"\n" || !strings.Contains(got.stderr, "reports") {
		t.Fatalf("%+v", got)
	}
}

// SE: --team missing, a flag the check does not take, or a word is 64, with nothing asked.
func TestTeamCarryCheckUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"no team": nil,
		"a drill": {"--team", "alpha", "--drill"},
		"a keep":  {"--team", "alpha", "--keep-running"},
		"a word":  {"--team", "alpha", "now"},
	} {
		w := newSealVerbWorld(t)
		got := w.checkVerb(args...)
		if got.code != term.ExitUsage || got.stdout != "" || len(w.engine.checks) != 0 {
			t.Errorf("%s: %+v, checks %v", name, got, w.engine.checks)
		}
	}
}

// SE: the check's failures by their class: a refusal or a record 70, a busy lock 75, a step 1.
func TestTeamCarryCheckExitCodes(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		want term.ExitCode
	}{
		"refused":  {&carry.RefusedError{Problem: "team alpha was not imported"}, ExitRefused},
		"a record": {&carry.RecordedError{Team: alpha(t), Record: carry.RecordImport}, ExitRefused},
		"busy":     {&teamlock.BusyError{Path: "/run/x.lock", Holder: "apply alpha pid=1 since=1"}, ExitBusy},
		"a step":   {errors.New("the stop failed"), term.ExitFailure},
	} {
		w := newSealVerbWorld(t)
		w.engine.out = nil
		w.engine.err = c.err
		got := w.checkVerb("--team", "alpha")
		if got.code != c.want || got.stderr == "" || len(w.engine.checks) != 1 {
			t.Errorf("%s: %+v, want %d", name, got, c.want)
		}
	}
}

// BV4: team carry-check is root's only (77).
func TestTeamCarryCheckIsRootsOnly(t *testing.T) {
	w := newSealVerbWorld(t)
	w.proc = ownerProc()
	if got := w.checkVerb("--team", "alpha"); got.code != ExitClass || len(w.engine.checks) != 0 {
		t.Fatalf("%+v", got)
	}
}

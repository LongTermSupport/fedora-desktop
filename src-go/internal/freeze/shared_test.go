package freeze

// What podfreeze and lxcfreeze each wrote and the second port lifted here: the vanished-group
// error (said from the menu, fatal from the command line), the engine's stderr notes, the text of
// a failed command, and the hook that readies an engine before a batch of acts.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestGoneIsAnErrorWithItsOwnWords(t *testing.T) {
	err := Gone("no such network: '%s'", "nosuchnet")
	if err.Error() != "no such network: 'nosuchnet'" {
		t.Fatalf("got %q", err.Error())
	}
	if _, ok := errors.AsType[*GoneError](err); !ok {
		t.Fatal("Gone returns a *GoneError")
	}
}

func TestSelectGoneSaysItOnStderrAndReturnsErrGroupGone(t *testing.T) {
	var w bytes.Buffer
	err := SelectGone(&w, "testfreeze", Gone("no such thing"))
	if !errors.Is(err, ErrGroupGone) {
		t.Fatalf("got %v", err)
	}
	if w.String() != "testfreeze: no such thing\n" {
		t.Fatalf("said %q", w.String())
	}
}

func TestSelectGonePassesEverythingElseThrough(t *testing.T) {
	var w bytes.Buffer
	boom := errors.New("boom")
	if got := SelectGone(&w, "testfreeze", boom); !errors.Is(got, boom) || w.Len() != 0 {
		t.Fatalf("got %v, said %q", got, w.String())
	}
	if got := SelectGone(&w, "testfreeze", nil); got != nil || w.Len() != 0 {
		t.Fatalf("nil stays nil: %v", got)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestSelectGoneReturnsAFailedWriteRatherThanLosingIt(t *testing.T) {
	err := SelectGone(brokenWriter{}, "testfreeze", Gone("x"))
	if err == nil || errors.Is(err, ErrGroupGone) {
		t.Fatalf("a note that could not be written is an error: %v", err)
	}
}

func TestDieGoneMakesAVanishedGroupFatalWithTheSameWords(t *testing.T) {
	err := DieGone(Gone("no such bridge"))
	if dieMsg(err) != "no such bridge" {
		t.Fatalf("got %v", err)
	}
	boom := errors.New("boom")
	if !errors.Is(DieGone(boom), boom) || DieGone(nil) != nil {
		t.Fatal("anything else passes through")
	}
}

func TestSayWritesAFormattedNoteAndReturnsAFailedWrite(t *testing.T) {
	var w bytes.Buffer
	if err := Say(&w, "%s: %d\n", "t", 2); err != nil || w.String() != "t: 2\n" {
		t.Fatalf("got %q, %v", w.String(), err)
	}
	if err := Say(brokenWriter{}, "x"); err == nil {
		t.Fatal("a failed write is returned")
	}
}

func TestFailureText(t *testing.T) {
	boom := errors.New("exec failed")
	cases := []struct {
		name string
		res  execx.Result
		want string
	}{
		{"stderr first, then stdout", execx.Result{Stderr: []byte("bad\n"), Stdout: []byte("partial\n")}, "bad\npartial"},
		{"stderr only", execx.Result{Stderr: []byte("  bad  \n")}, "bad"},
		{"stdout only", execx.Result{Stdout: []byte("said this\n")}, "said this"},
		{"neither: the error itself", execx.Result{}, "exec failed"},
	}
	for _, c := range cases {
		if got := FailureText(c.res, boom); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// preparingEngine is an engine that wants readying before a batch of acts.
type preparingEngine struct {
	fakeEngine
	prepared int
	prepErr  error
}

func (p *preparingEngine) BeforeAct(context.Context) error {
	p.prepared++
	return p.prepErr
}

func newPreparing(t *testing.T) (*Session, *preparingEngine) {
	t.Helper()
	cfg := testConfig(vocabs[0])
	eng := &preparingEngine{fakeEngine: fakeEngine{entries: fixtureEntries(vocabs[0]), cfg: cfg}}
	s := NewSession(cfg, Env{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}, eng, &scriptedChooser{})
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, eng
}

func TestAnEngineIsReadiedOnceBeforeAnActBatch(t *testing.T) {
	s, eng := newPreparing(t)
	if err := s.DoAction(context.Background(), VerbFreeze, []string{"charlie", "echo"}); err != nil {
		t.Fatal(err)
	}
	if eng.prepared != 1 || len(eng.acts) != 2 {
		t.Fatalf("prepared %d times for %d acts", eng.prepared, len(eng.acts))
	}
}

func TestAnEngineThatCannotBeReadiedActsOnNothing(t *testing.T) {
	s, eng := newPreparing(t)
	eng.prepErr = Die("sudo was refused")
	err := s.DoAction(context.Background(), VerbFreeze, []string{"charlie"})
	if dieMsg(err) != "sudo was refused" || len(eng.acts) != 0 {
		t.Fatalf("got %v, acts %v", err, eng.acts)
	}
}

func TestNothingToDoAndADryRunDoNotReadyTheEngine(t *testing.T) {
	s, eng := newPreparing(t)
	if err := s.DoAction(context.Background(), VerbFreeze, []string{"alpha"}); err != nil { // already frozen
		t.Fatal(err)
	}
	s.DryRun = true
	if err := s.DoAction(context.Background(), VerbFreeze, []string{"charlie"}); err != nil {
		t.Fatal(err)
	}
	if eng.prepared != 0 {
		t.Fatalf("readied %d times though nothing was done", eng.prepared)
	}
}

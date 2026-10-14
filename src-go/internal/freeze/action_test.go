package freeze

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDoActionEmptySelectionIsRefusedBeforeAnythingHappens(t *testing.T) {
	h := newHarness(t, vocabs[0])
	err := h.s.DoAction(context.Background(), VerbFreeze, nil)
	msg := dieMsg(err)
	if msg == "" {
		t.Fatalf("an empty selection is fatal: %v", err)
	}
	mustContain(t, "and says how to see what there is", msg, "testfreeze list")
	mustEq(t, "and the engine was never called", len(h.eng.acts), 0)
}

func TestDoActionActsOnWhatItShouldAndReportsTheRest(t *testing.T) {
	h := newHarness(t, vocabs[0])
	if err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie", "alpha", "nosuch"}); err != nil {
		t.Fatal(err)
	}
	got := h.err.String()
	mustEq(t, "the engine is called once, for the one running container", h.eng.actsLine(), "freeze charlie;")
	mustContain(t, "the container already frozen is reported as skipped", got, "Skipped — not currently running: alpha\n")
	mustContain(t, "the absent one is reported as gone", got, "Skipped — no longer present (gone since the inventory was read): nosuch\n")
	// Skipped and vanished must not share a sentence: "already in that state" is a claim about
	// a container nobody can find.
	mustEq(t, "the two skips are reported separately", strings.Count(got, "Skipped"), 2)
	mustContain(t, "a success is ticked off by name", got, "  ✓ charlie\n")
	mustContain(t, "the heading counts what is acted on", got, "1 container(s) being FROZEN:\n")
	// After a freeze, the command that undoes it names exactly what was acted on, not the whole
	// selection, which would thaw containers this run never touched.
	mustContain(t, "and the undo command is offered", got, "  Thaw them with: testfreeze thaw charlie\n")
	mustLack(t, "naming only what was acted on", got, "thaw charlie alpha")
	mustEq(t, "nothing on stdout for a real run", h.out.String(), "")
}

// The freeze-time note is an OPTIONAL slot with no default: the library carries the slot and
// neither the text nor the assumption that there is one.
func TestDoActionFreezeNote(t *testing.T) {
	t.Run("a declared note is printed at freeze time", func(t *testing.T) {
		h := newHarness(t, vocabs[0])
		h.s.Cfg.FreezeNote = "  While frozen it answers nothing. Reconnect after thawing."
		if err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie"}); err != nil {
			t.Fatal(err)
		}
		mustContain(t, "note", h.err.String(), "Reconnect after thawing")
		// Beside the thaw instruction, which is the moment the cost is still avoidable.
		mustContain(t, "and it comes with the thaw instruction", h.err.String(), "Thaw them with:")
	})
	// Thaw is not where the warning belongs: the cost has already been paid by then.
	t.Run("a thaw does not warn about freezing", func(t *testing.T) {
		h := newHarness(t, vocabs[0])
		h.s.Cfg.FreezeNote = "  While frozen it answers nothing. Reconnect after thawing."
		if err := h.s.DoAction(context.Background(), VerbThaw, []string{"alpha"}); err != nil {
			t.Fatal(err)
		}
		mustLack(t, "note", h.err.String(), "Reconnect after thawing")
	})
	// The claim "an empty note prints nothing, not a blank line" is checked as a line COUNT:
	// two extra lines with a note (the blank separator and the note), none without.
	t.Run("an empty note adds exactly nothing", func(t *testing.T) {
		empty := newHarness(t, vocabs[0])
		set := newHarness(t, vocabs[0])
		set.s.Cfg.FreezeNote = "  A note."
		for _, h := range []*harness{empty, set} {
			if err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie"}); err != nil {
				t.Fatal(err)
			}
		}
		mustEq(t, "extra lines", strings.Count(set.err.String(), "\n")-strings.Count(empty.err.String(), "\n"), 2)
	})
}

func TestDoActionThawIsTheMirrorImage(t *testing.T) {
	h := newHarness(t, vocabs[0])
	if err := h.s.DoAction(context.Background(), VerbThaw, []string{"charlie", "alpha"}); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "the engine is called for the frozen one only", h.eng.actsLine(), "thaw alpha;")
	mustContain(t, "and the running one is skipped", h.err.String(), "not currently paused")
	mustContain(t, "heading", h.err.String(), "1 container(s) being THAWED:\n")
	// After a thaw, running the same command again would freeze them, which undoes nothing.
	mustLack(t, "no undo hint after a thaw", h.err.String(), "testfreeze thaw alpha")
}

func TestDoActionNothingToDoIsANoOpNotARun(t *testing.T) {
	h := newHarness(t, vocabs[0])
	err := h.s.DoAction(context.Background(), VerbFreeze, []string{"alpha", "bravo"})
	mustEq(t, "the engine is not called at all", len(h.eng.acts), 0)
	mustEq(t, "and it succeeds", err, "<nil>")
	mustContain(t, "saying so plainly", h.err.String(), "Nothing to do — every selected container is already in the requested state.\n")
}

func TestDoActionDryRunChangesNothingAndPrintsToStdout(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.s.DryRun = true
	if err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie", "echo"}); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "the engine is never called", len(h.eng.acts), 0)
	mustContain(t, "the preview says what would happen", h.out.String(), "DRY RUN — 2 container(s) would be FROZEN:\n")
	mustContain(t, "and names the containers", h.out.String(), "charlie")
	// The dry-run table IS the payload (-n exists to be read or piped), so it goes to stdout
	// while the running commentary goes to stderr.
	mustContain(t, "the table is on stdout", h.out.String(), "NAME")
	mustLack(t, "and the commentary does not repeat it", h.err.String(), "DRY RUN")
}

func TestDoActionAFailingEngineIsNamedCountedAndFailsTheRun(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.eng.actErr = errors.New("exit status 1")
	h.eng.actOutput = "cgroup freezer unavailable\n\n"
	err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie", "echo"})
	if !errors.Is(err, ErrSomeFailed) {
		t.Fatalf("a failing engine fails the run: %v", err)
	}
	got := h.err.String()
	mustContain(t, "each failure is named, with what the engine actually said, trailing newlines stripped", got,
		"  ✗ charlie — cgroup freezer unavailable\n")
	mustContain(t, "each failure is named", got, "✗ echo")
	// Every target is attempted: a failure part-way through must not abandon the rest.
	mustEq(t, "and the batch is not abandoned after the first failure", h.eng.actsLine(), "freeze charlie;freeze echo;")
	mustContain(t, "the count of failures is reported", got, "testfreeze: 2 of 2 container(s) failed — see above.\n")
}

// A failure with no output of its own still says why, from the error.
func TestDoActionAFailureWithNoOutputFallsBackToTheError(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.eng.actErr = errors.New("podman not found")
	if err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie"}); !errors.Is(err, ErrSomeFailed) {
		t.Fatal(err)
	}
	mustContain(t, "fallback", h.err.String(), "✗ charlie — podman not found")
}

// Bug 7: the bash printed "Thaw them with" and the freeze note for attempts, not successes.
func TestDoActionHintsDescribeSuccessesOnly(t *testing.T) {
	t.Run("every target failed: neither hint nor note", func(t *testing.T) {
		h := newHarness(t, vocabs[0])
		h.s.Cfg.FreezeNote = "  A note."
		h.eng.actErr = errors.New("boom")
		if err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie", "echo"}); !errors.Is(err, ErrSomeFailed) {
			t.Fatal(err)
		}
		mustLack(t, "no thaw hint for nothing frozen", h.err.String(), "Thaw them with")
		mustLack(t, "no note for nothing frozen", h.err.String(), "A note.")
	})
	t.Run("a partial freeze lists only what was frozen", func(t *testing.T) {
		h := newHarness(t, vocabs[0])
		failing := &failOn{fakeEngine: h.eng, fail: "echo"}
		h.s.Engine = failing
		err := h.s.DoAction(context.Background(), VerbFreeze, []string{"charlie", "echo"})
		if !errors.Is(err, ErrSomeFailed) {
			t.Fatal(err)
		}
		mustContain(t, "only the success", h.err.String(), "  Thaw them with: testfreeze thaw charlie\n")
		mustContain(t, "the failure count is of the batch", h.err.String(), "1 of 2 container(s) failed")
	})
}

// failOn fails the Act of one named container.
type failOn struct {
	*fakeEngine

	fail string
}

func (f *failOn) Act(ctx context.Context, verb Verb, name string) (string, error) {
	out, err := f.fakeEngine.Act(ctx, verb, name)
	if name == f.fail {
		return "refused", errors.New("exit status 1")
	}
	return out, err
}

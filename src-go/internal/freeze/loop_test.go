package freeze

import (
	"context"
	"errors"
	"testing"
)

// loopHarness scripts the group menu with keys and answers the drill-down with "all". The
// engine's Select understands the keys all, gone, broke and empty, exactly as the bash suite's
// engine did.
func loopHarness(t *testing.T, keys ...string) *harness {
	t.Helper()
	h := newHarness(t, vocabs[0])
	h.chooser.keys = keys
	h.eng.selectFn = func(key string) ([]string, error) {
		switch key {
		case "all":
			return h.s.Inventory().SelectAll(), nil
		case "gone":
			return nil, ErrGroupGone
		case "broke":
			return nil, errors.New("exit status 1")
		case "empty":
			return nil, nil
		}
		return nil, Die("internal error: unknown target key '%s'.", key)
	}
	return h
}

// Ctrl-C during an act in the menu ends the session as a cancellation: no refresh on the dead
// context, no second menu.
func TestLoopCancelledDuringActEndsAsACancellationWithoutRefreshing(t *testing.T) {
	h := loopHarness(t, "all", "all")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.eng.onAct = func(int) error {
		cancel()
		return ctx.Err()
	}
	err := h.s.Loop(ctx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrSomeFailed) {
		t.Fatalf("want a bare cancellation, got %v", err)
	}
	mustEq(t, "no refresh after the interrupt", h.eng.loads, 1)
	mustEq(t, "no second menu", h.chooser.groups, 1)
}

func TestLoopCancelledBeforeARefreshDoesNotRunIt(t *testing.T) {
	h := loopHarness(t, "all")
	ctx, cancel := context.WithCancel(context.Background())
	h.s.fresh = false
	cancel()
	if err := h.s.Loop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	mustEq(t, "the engine was not asked", h.eng.loads, 1)
}

func TestLoopQuittingLeavesWithSuccess(t *testing.T) {
	h := loopHarness(t)
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatalf("quitting leaves with a success status: %v", err)
	}
}

// A group that went away is RECOVERABLE: the engine has already explained it, and the loop must
// come back to the menu rather than end the session.
func TestLoopAGroupThatWentAwayRePromptsRatherThanEndingTheSession(t *testing.T) {
	h := loopHarness(t, "gone")
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "menus drawn", h.eng.loads, 2)
	mustEq(t, "a blank line follows the engine's explanation", h.err.String(), "\n")
}

// ...but an engine that BROKE is not that, and must not be treated as it. Both are non-zero;
// before the typed ErrGroupGone existed both re-prompted and a failing hook redrew the menu
// with no explanation.
func TestLoopASelectThatBrokeIsFatalNotARePrompt(t *testing.T) {
	h := loopHarness(t, "broke", "all")
	err := h.s.Loop(context.Background())
	if err == nil {
		t.Fatal("interactive loop returned nil and carried on")
	}
	msg := dieMsg(err)
	mustContain(t, "the failure names the engine's error and the key", msg, "exit status 1")
	mustContain(t, "and the key", msg, "'broke'")
	mustContain(t, "and does not claim the group went away", msg, "select hook failed")
	mustEq(t, "no second menu", h.chooser.groups, 1)
}

// An engine's own fatal error (a die) is not re-wrapped.
func TestLoopAnEnginesFatalErrorPassesThroughUnchanged(t *testing.T) {
	h := loopHarness(t, "nosuchkey")
	err := h.s.Loop(context.Background())
	mustEq(t, "msg", dieMsg(err), "internal error: unknown target key 'nosuchkey'.")
}

func TestLoopAnEmptyGroupSaysSoAndAsksAgain(t *testing.T) {
	h := loopHarness(t, "empty")
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "an empty group says so", h.err.String(), "Nothing in that group.\n\n")
	mustEq(t, "and asks again", h.eng.loads, 2)
}

// Backing out of the drill-down returns to the GROUP menu: one level up, not out.
func TestLoopBackingOutOfTheDrillDownReturnsToTheMenu(t *testing.T) {
	h := loopHarness(t, "all")
	h.chooser.members = func(MemberScreen) (Choice, error) { return Choice{}, ErrBack }
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "menus drawn", h.eng.loads, 2)
	mustEq(t, "nothing acted", len(h.eng.acts), 0)
}

// The inventory is re-read on EVERY pass, so the counts describe the machine as it is now.
func TestLoopRereadsTheInventoryOnEveryPass(t *testing.T) {
	h := loopHarness(t, "all", "all")
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "the inventory is re-read on every pass", h.eng.loads, 3)
	mustEq(t, "and the engine acted on both passes", h.eng.actsLine(),
		"freeze charlie;freeze echo;freeze charlie;freeze echo;")
}

// An empty machine ends the session without asking anything: there is no menu to draw.
func TestLoopAnEmptyMachineIsReportedNotPromptedAbout(t *testing.T) {
	h := loopHarness(t, "all")
	h.eng.entries = nil
	if err := h.s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustContain(t, "reported", h.err.String(), "No running or frozen containers.\n")
	mustEq(t, "and nothing was acted on", len(h.eng.acts), 0)
	mustEq(t, "no menu was drawn", h.chooser.groups, 0)
}

// A failed action is reported and the session CARRIES ON, but the status it leaves with is
// still non-zero, or a script wrapping the tool reads a failure as a clean run.
func TestLoopAFailedActionStillFailsTheSessionButKeepsGoing(t *testing.T) {
	h := loopHarness(t, "all")
	h.eng.actErr = errors.New("exit status 1")
	err := h.s.Loop(context.Background())
	if !errors.Is(err, ErrSomeFailed) {
		t.Fatalf("a failed action still fails the session: %v", err)
	}
	mustEq(t, "and it kept going to the next menu", h.eng.loads, 2)
}

func TestLoopASessionWithNoFailureReturnsSuccess(t *testing.T) {
	h := loopHarness(t, "all")
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatalf("a session with no failure returns success: %v", err)
	}
}

// Bug 8: the bash returned 0 for "No running or frozen containers." even after an earlier
// failed act.
func TestLoopAnEmptiedMachineStillReturnsTheCarriedStatus(t *testing.T) {
	h := loopHarness(t, "all", "all")
	h.eng.actErr = errors.New("exit status 1")
	h.chooser.members = func(MemberScreen) (Choice, error) {
		h.eng.entries = nil // the machine empties after the act
		return Choice{All: true}, nil
	}
	err := h.s.Loop(context.Background())
	if !errors.Is(err, ErrSomeFailed) {
		t.Fatalf("the carried failure is lost: %v", err)
	}
	mustContain(t, "said so", h.err.String(), "No running or frozen containers.\n")
}

// Bug 26: the startup load serves the first pass, so the menu does not cost a second round trip.
func TestLoopReusesTheStartupLoadForItsFirstPass(t *testing.T) {
	h := loopHarness(t)
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "one load in all", h.eng.loads, 1)

	// Without a prior load the loop fetches its own.
	h2 := loopHarness(t)
	h2.s = NewSession(testConfig(vocabs[0]), Env{Out: h2.out, Err: h2.err}, h2.eng, h2.chooser)
	h2.eng.loads = 0
	if err := h2.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "loop loads itself", h2.eng.loads, 1)
}

func TestLoopHonoursTheExplicitVerbAndTheDryRun(t *testing.T) {
	h := loopHarness(t, "all")
	h.s.Action = VerbThaw
	h.s.DryRun = true
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "nothing acted on a dry run", len(h.eng.acts), 0)
	mustContain(t, "thaw preview on stdout", h.out.String(), "DRY RUN — 3 container(s) would be THAWED:")
}

// The drill-down is offered the group's rows: an "all" label first, then one line per member
// that resolves, each ending in its row verb.
func TestLoopOffersTheDrillDownTheGroupsRows(t *testing.T) {
	h := loopHarness(t, "all")
	h.eng.selectFn = func(string) ([]string, error) { return []string{"charlie", "vanished", "alpha"}, nil }
	h.chooser.members = func(MemberScreen) (Choice, error) { return Choice{}, ErrBack }
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := h.chooser.screens[0]
	// A member that no longer resolves is skipped and must not shift the numbering.
	mustEq(t, "names", s.Names, "[charlie alpha]")
	mustEq(t, "lines", len(s.Lines), 3)
	mustContain(t, "all row", s.Lines[0], "*all*")
	mustContain(t, "all row effect", s.Lines[0], "FREEZE 1")
	mustContain(t, "member row", s.Lines[1], "charlie")
	mustContain(t, "member row verb", s.Lines[1], "FREEZE")
	mustContain(t, "frozen member verb", s.Lines[2], "THAW")
}

// Choosing the "all" row means the whole group, in group order, including a member that has
// gone from the inventory (which the action then reports as vanished).
func TestLoopAllMeansTheWholeGroupIncludingAVanishedMember(t *testing.T) {
	h := loopHarness(t, "all")
	h.eng.selectFn = func(string) ([]string, error) { return []string{"echo", "gone", "charlie"}, nil }
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "group order", h.eng.actsLine(), "freeze echo;freeze charlie;")
	mustContain(t, "vanished is reported", h.err.String(), "Skipped — no longer present (gone since the inventory was read): gone\n")
}

func TestLoopSpecificMeansJustThose(t *testing.T) {
	h := loopHarness(t, "all")
	h.chooser.members = func(MemberScreen) (Choice, error) { return Choice{Names: []string{"echo"}}, nil }
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mustEq(t, "specific", h.eng.actsLine(), "freeze echo;")
}

func TestLoopInterruptedIsReturnedAsCancellation(t *testing.T) {
	h := loopHarness(t, "all")
	h.chooser.members = func(MemberScreen) (Choice, error) { return Choice{}, context.Canceled }
	if err := h.s.Loop(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestLoopMenuWithNoRowsIsAnInternalError(t *testing.T) {
	h := loopHarness(t, "all")
	h.eng.groupsFn = func(Inventory[extraOf]) []Group { return nil }
	err := h.s.Loop(context.Background())
	mustEq(t, "msg", dieMsg(err), "internal error: the menu has no rows to offer.")
}

// Giving up on the drill-down's replies is said, then the menu comes back: in the bash tool the
// give-up there was a die inside a command substitution, which ended only that step. The group
// menu's own give-up stays fatal.
func TestLoopGivingUpInTheDrillDownSaysSoAndReturnsToTheMenu(t *testing.T) {
	h := loopHarness(t, "all")
	h.chooser.members = func(MemberScreen) (Choice, error) { return Choice{}, giveUp() }
	if err := h.s.Loop(context.Background()); err != nil {
		t.Fatalf("giving up in the drill-down ends the step, not the session: %v", err)
	}
	mustEq(t, "the menu is drawn again", h.eng.loads, 2)
	mustEq(t, "said once, then a blank line", h.err.String(), "testfreeze: giving up after 3 attempts — nothing changed.\n\n")
	mustEq(t, "nothing acted", len(h.eng.acts), 0)
}

func TestLoopAFatalErrorFromTheDrillDownIsStillFatal(t *testing.T) {
	h := loopHarness(t, "all")
	h.chooser.members = func(MemberScreen) (Choice, error) { return Choice{}, Die("no terminal") }
	if err := h.s.Loop(context.Background()); dieMsg(err) != "no terminal" {
		t.Fatalf("want the fatal error, got %v", err)
	}
}

func TestGiveUpIsAFatalErrorOfItsOwnKind(t *testing.T) {
	err := giveUp()
	mustEq(t, "msg", dieMsg(err), "giving up after 3 attempts — nothing changed.")
	if gaveUp, ok := errors.AsType[*GaveUpError](err); !ok || gaveUp == nil {
		t.Fatalf("want a *GaveUpError, got %T", err)
	}
}

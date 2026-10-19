package freeze

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

type fzfAnswer struct {
	lines []string
	err   error
}

// fakeFzf records the screens it was asked for and answers from a script.
type fakeFzf struct {
	specs    []ui.ChooseSpec
	headings []ui.Heading
	answers  []fzfAnswer
}

func (f *fakeFzf) Choose(_ context.Context, h ui.Heading, s ui.ChooseSpec) ([]string, error) {
	f.specs = append(f.specs, s)
	f.headings = append(f.headings, h)
	if len(f.answers) == 0 {
		return nil, errors.New("fakeFzf: unscripted")
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	return a.lines, a.err
}

// ttyHarness builds a TTYChooser reading `input` from a fake /dev/tty, with a terminal.
type ttyHarness struct {
	c   *TTYChooser
	err *bytes.Buffer
	fzf *fakeFzf
}

func newTTY(t *testing.T, input string) *ttyHarness {
	t.Helper()
	h := &ttyHarness{err: &bytes.Buffer{}, fzf: &fakeFzf{answers: []fzfAnswer{{err: ui.ErrFzfMissing}, {err: ui.ErrFzfMissing}}}}
	env := Env{
		Err:        h.err,
		IsTerminal: func(any) bool { return true },
		OpenTTY:    func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(input)), nil },
	}
	h.c = &TTYChooser{Cfg: testConfig(vocabs[0]), Env: env, Fzf: h.fzf}
	return h
}

var (
	menuKeys   = []string{"ccy", "all", "net:a"}
	menuLabels = []string{"all CCY", "all containers", "network: a"}
)

func TestChoosersRefuseWithoutATerminalAndNameTheTargets(t *testing.T) {
	// pick_target and drill_into_group both die without a TTY rather than reading whatever is on
	// stdin: a menu answered by a pipe would act on a group nobody chose.
	noTerm := func() *TTYChooser {
		c := newTTY(t, "").c
		c.Env.IsTerminal = func(any) bool { return false }
		return c
	}
	_, err := noTerm().Group(context.Background(), menuKeys, menuLabels)
	msg := dieMsg(err)
	if msg == "" {
		t.Fatalf("Group refuses without a terminal: %v", err)
	}
	mustContain(t, "and names the targets the CLI accepts", msg, "NAME..., or --all")
	mustContain(t, "and points at --help", msg, "testfreeze --help")
	mustEq(t, "exact", msg, "no terminal to ask on — name a target explicitly: NAME..., or --all.\n  See 'testfreeze --help'.")

	_, err = noTerm().Members(context.Background(), MemberScreen{Names: []string{"alpha"}, Lines: []string{"all", "alpha"}})
	msg = dieMsg(err)
	if msg == "" {
		t.Fatalf("Members refuses without a terminal: %v", err)
	}
	mustContain(t, "and names the targets the CLI accepts", msg, "NAME..., or --all")
}

// Bug 12: -r /dev/tty is true for the device node even with no controlling terminal, and the
// later read failed and was read as EOF, so the tool quit silently with exit 0.
func TestNewChoosersOpenTheTerminalAtTheGuard(t *testing.T) {
	c := newTTY(t, "").c
	c.Env.OpenTTY = func() (io.ReadCloser, error) { return nil, errors.New("no such device or address") }
	_, err := c.Group(context.Background(), menuKeys, menuLabels)
	mustContain(t, "no-terminal message", dieMsg(err), "no terminal to ask on")
}

// An EMPTY group is "back" before any terminal is looked for, and says nothing.
func TestAnEmptyGroupReturnsBackBeforeLookingForATerminal(t *testing.T) {
	h := newHarness(t, vocabs[0])
	h.s.Chooser = dieChooser{}
	_, err := h.s.drill(context.Background(), "all CCY", nil)
	if !errors.Is(err, ErrBack) {
		t.Fatalf("got %v", err)
	}
	mustEq(t, "and says nothing on the way out", h.err.String(), "")
}

type dieChooser struct{}

func (dieChooser) Group(context.Context, []string, []string) (string, error) {
	return "", errors.New("chooser must not be asked")
}

func (dieChooser) Members(context.Context, MemberScreen) (Choice, error) {
	return Choice{}, errors.New("chooser must not be asked")
}

func TestGroupNumberedPrompt(t *testing.T) {
	cases := []struct {
		what, input, wantKey string
		wantErr              error
	}{
		{"a number picks that row", "2\n", "all", nil},
		{"the last row", "3\n", "net:a", nil},
		{"leading zeros are the number", "01\n", "ccy", nil},
		{"whitespace around the reply is ignored", "  2 \n", "all", nil},
		{"q quits", "q\n", "", ErrQuit},
		{"Q quits", "Q\n", "", ErrQuit},
		{"quit quits", "quit\n", "", ErrQuit},
		{"end of input quits", "", "", ErrQuit},
		{"a typo then a good answer recovers", "9\nx\n1\n", "ccy", nil},
		{"zero then a good answer recovers", "0\n2\n", "all", nil},
		{"empty then a good answer recovers", "\n3\n", "net:a", nil},
		{"an absurdly long number is a typo, not a crash", "99999999999999999999\n1\n", "ccy", nil},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			h := newTTY(t, c.input)
			key, err := h.c.Group(context.Background(), menuKeys, menuLabels)
			if !errors.Is(err, c.wantErr) || key != c.wantKey {
				t.Fatalf("got %q, %v", key, err)
			}
		})
	}
}

func TestGroupNumberedPromptText(t *testing.T) {
	h := newTTY(t, "q\n")
	if _, err := h.c.Group(context.Background(), menuKeys, menuLabels); !errors.Is(err, ErrQuit) {
		t.Fatal(err)
	}
	want := "What to act on:\n   1) all CCY\n   2) all containers\n   3) network: a\nChoice [1-3, q to quit]: "
	mustEq(t, "stderr", h.err.String(), want)
}

func TestGroupEndOfInputPrintsANewlineThenQuits(t *testing.T) {
	h := newTTY(t, "")
	if _, err := h.c.Group(context.Background(), menuKeys, menuLabels); !errors.Is(err, ErrQuit) {
		t.Fatal(err)
	}
	mustContain(t, "newline after the prompt", h.err.String(), "Choice [1-3, q to quit]: \n")
}

// The retry budget: three bad replies and it gives up, naming the budget.
func TestGroupThreeBadRepliesGiveUp(t *testing.T) {
	h := newTTY(t, "x\n0\n99\n1\n")
	key, err := h.c.Group(context.Background(), menuKeys, menuLabels)
	if key != "" {
		t.Fatalf("key %q", key)
	}
	mustEq(t, "msg", dieMsg(err), "giving up after 3 attempts — nothing changed.")
	mustEq(t, "each bad reply was explained", strings.Count(h.err.String(),
		"  Not one of the options — enter a number between 1 and 3, or q.\n"), 3)
	mustEq(t, "the third prompt was the last", strings.Count(h.err.String(), "Choice [1-3, q to quit]: "), 3)
}

func TestGroupFzfScreenAndMatchByWholeLabel(t *testing.T) {
	h := newTTY(t, "")
	h.fzf.answers = []fzfAnswer{{lines: []string{"all containers"}}}
	key, err := h.c.Group(context.Background(), menuKeys, menuLabels)
	if err != nil || key != "all" {
		t.Fatalf("got %q, %v", key, err)
	}
	want := ui.ChooseSpec{
		Prompt: "testfreeze: ", Header: "ENTER acts on the chosen group — ESC to quit",
		Height: "50%", Rows: menuLabels,
	}
	got := h.fzf.specs[0]
	if got.Prompt != want.Prompt || got.Header != want.Header || got.Height != want.Height || got.Multi ||
		!slices.Equal(got.Rows, want.Rows) {
		t.Fatalf("spec %+v", got)
	}
	mustEq(t, "no numbered prompt was drawn", h.err.String(), "")
}

func TestGroupFzfLeaveIsQuit(t *testing.T) {
	h := newTTY(t, "")
	h.fzf.answers = []fzfAnswer{{err: ui.ErrLeave}}
	if _, err := h.c.Group(context.Background(), menuKeys, menuLabels); !errors.Is(err, ErrQuit) {
		t.Fatal(err)
	}
}

// Bug 2: any non-zero fzf status used to be a quiet quit with exit 0.
func TestGroupFzfFailureIsAFailureNotAQuit(t *testing.T) {
	h := newTTY(t, "")
	boom := errors.New("fzf crashed")
	h.fzf.answers = []fzfAnswer{{err: boom}}
	_, err := h.c.Group(context.Background(), menuKeys, menuLabels)
	if !errors.Is(err, boom) || errors.Is(err, ErrQuit) {
		t.Fatalf("got %v", err)
	}
}

func TestGroupFzfMissingFallsBackToTheNumberedPrompt(t *testing.T) {
	h := newTTY(t, "3\n")
	key, err := h.c.Group(context.Background(), menuKeys, menuLabels)
	if err != nil || key != "net:a" {
		t.Fatalf("got %q, %v", key, err)
	}
}

func TestGroupFzfAnEntryNotInTheMenuIsAnInternalError(t *testing.T) {
	h := newTTY(t, "")
	h.fzf.answers = []fzfAnswer{{lines: []string{"something else"}}}
	_, err := h.c.Group(context.Background(), menuKeys, menuLabels)
	mustEq(t, "msg", dieMsg(err), "internal error: chosen entry not found.")
	h.fzf.answers = []fzfAnswer{{}}
	_, err = h.c.Group(context.Background(), menuKeys, menuLabels)
	mustEq(t, "an empty answer is the same", dieMsg(err), "internal error: chosen entry not found.")
}

var screen = MemberScreen{
	Lines: []string{"*all*       FREEZE 2", "alpha  x", "bravo  y", "charlie  z"},
	Names: []string{"alpha", "bravo", "charlie"},
}

func TestMembersNumberedPrompt(t *testing.T) {
	cases := []struct {
		what, input string
		want        Choice
		wantErr     error
	}{
		{"ENTER is all", "\n", Choice{All: true}, nil},
		{"1 is all", "1\n", Choice{All: true}, nil},
		{"a is all", "a\n", Choice{All: true}, nil},
		{"A is all", "A\n", Choice{All: true}, nil},
		{"all is all", "all\n", Choice{All: true}, nil},
		{"b goes back", "b\n", Choice{}, ErrBack},
		{"B goes back", "B\n", Choice{}, ErrBack},
		{"back goes back", "back\n", Choice{}, ErrBack},
		{"q goes back", "q\n", Choice{}, ErrBack},
		{"Q goes back", "Q\n", Choice{}, ErrBack},
		{"quit goes back", "quit\n", Choice{}, ErrBack},
		{"end of input goes back", "", Choice{}, ErrBack},
		{"specific numbers", "2,4\n", Choice{Names: []string{"alpha", "charlie"}}, nil},
		{"a typo then a good answer recovers", "9\n3\n", Choice{Names: []string{"bravo"}}, nil},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			h := newTTY(t, c.input)
			got, err := h.c.Members(context.Background(), screen)
			if !errors.Is(err, c.wantErr) || got.All != c.want.All || !slices.Equal(got.Names, c.want.Names) {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestMembersNumberedPromptTextAndRetryMessages(t *testing.T) {
	h := newTTY(t, "9\nb\n")
	if _, err := h.c.Members(context.Background(), screen); !errors.Is(err, ErrBack) {
		t.Fatal(err)
	}
	want := "In this group:\n" +
		"   1) *all*       FREEZE 2\n   2) alpha  x\n   3) bravo  y\n   4) charlie  z\n" +
		"Choice [ENTER or 1 for all, numbers for specific, b to go back]: " +
		"  Not a valid choice — 1 (or ENTER) for all, 2-4 for\n  specific containers, or 'b' to go back.\n" +
		"Choice [ENTER or 1 for all, numbers for specific, b to go back]: "
	mustEq(t, "stderr", h.err.String(), want)
}

func TestMembersThreeBadRepliesGiveUp(t *testing.T) {
	h := newTTY(t, "x\n9\n0\n\n")
	_, err := h.c.Members(context.Background(), screen)
	mustEq(t, "msg", dieMsg(err), "giving up after 3 attempts — nothing changed.")
}

func TestMembersEndOfInputPrintsANewline(t *testing.T) {
	h := newTTY(t, "")
	if _, err := h.c.Members(context.Background(), screen); !errors.Is(err, ErrBack) {
		t.Fatal(err)
	}
	mustContain(t, "newline", h.err.String(), "b to go back]: \n")
}

func TestMembersFzfScreen(t *testing.T) {
	h := newTTY(t, "")
	h.fzf.answers = []fzfAnswer{{lines: []string{"alpha  x", "charlie  z"}}}
	got, err := h.c.Members(context.Background(), screen)
	if err != nil || got.All || !slices.Equal(got.Names, []string{"alpha", "charlie"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	spec := h.fzf.specs[0]
	if !spec.Multi || spec.Height != "60%" || spec.Prompt != "In this group: " ||
		spec.Header != "ENTER on the first row acts on all — TAB picks specific ones, ESC goes back" ||
		!slices.Equal(spec.Rows, screen.Lines) {
		t.Fatalf("spec %+v", spec)
	}
}

func TestMembersFzfAllRowWinsWhereverItIs(t *testing.T) {
	h := newTTY(t, "")
	h.fzf.answers = []fzfAnswer{{lines: []string{"alpha  x", "*all*       FREEZE 2"}}}
	got, err := h.c.Members(context.Background(), screen)
	if err != nil || !got.All {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMembersFzfBackAndFailure(t *testing.T) {
	h := newTTY(t, "")
	h.fzf.answers = []fzfAnswer{{err: ui.ErrLeave}, {lines: nil}, {lines: []string{"", ""}}}
	for i := range 3 {
		if _, err := h.c.Members(context.Background(), screen); !errors.Is(err, ErrBack) {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	// Bug 2: a crashed fzf is not "back".
	boom := errors.New("fzf crashed")
	h.fzf.answers = []fzfAnswer{{err: boom}}
	if _, err := h.c.Members(context.Background(), screen); !errors.Is(err, boom) || errors.Is(err, ErrBack) {
		t.Fatalf("got %v", err)
	}
}

func TestMembersFzfMissingFallsBackToTheNumberedPrompt(t *testing.T) {
	h := newTTY(t, "3\n")
	got, err := h.c.Members(context.Background(), screen)
	if err != nil || !slices.Equal(got.Names, []string{"bravo"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// Ctrl-C while a numbered prompt waits: the run is cancelled with a newline after the prompt
// (the shell died by the signal with none).
func TestANumberedPromptInterruptedByCancellation(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Error(err)
		}
	})
	h := newTTY(t, "")
	h.c.Env.OpenTTY = func() (io.ReadCloser, error) { return pr, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.c.Group(ctx, menuKeys, menuLabels)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	mustContain(t, "newline", h.err.String(), "q to quit]: \n")
}

// After Ctrl-C the abandoned read is still blocked on the terminal; releasing the terminal then
// must not race with it (found by the podfreeze Ctrl-C test under -race).
func TestNewClosingAfterAnInterruptedPromptDoesNotRaceTheAbandonedRead(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Error(err)
		}
	})
	h := newTTY(t, "")
	h.c.Env.OpenTTY = func() (io.ReadCloser, error) { return pr, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.c.Group(ctx, menuKeys, menuLabels); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if err := h.c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseIsSafeWhetherOrNotATerminalWasOpened(t *testing.T) {
	h := newTTY(t, "q\n")
	if err := h.c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.c.Group(context.Background(), menuKeys, menuLabels); !errors.Is(err, ErrQuit) {
		t.Fatal(err)
	}
	if err := h.c.Close(); err != nil {
		t.Fatal(err)
	}
}

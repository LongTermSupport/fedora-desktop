package ui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

var lxcStep = Here("Teams", "alpha", "Add LXC member").AtStep(3, 4, "Checkout").About("container-a · user agent")

// measuredHeaderWidth is the widest header line fzf drew whole, by terminal columns, on a
// pseudo-terminal with the house frame (Fable's re-review of Task 1.0, fzf 0.38): one cell more
// was cut to "..".
// unwrapped is a width at which lxcStep fits on one line: any screen at the common 80 columns.
const unwrapped = fallbackColumns - framedMargin

var measuredHeaderWidth = map[int]int{40: 26, 80: 66}

// fakeColumns is a runner whose tput cols answers cols ("" for a tput that fails), and whose
// fzf leaves and gum answers nothing.
func fakeColumns(cols string) *execx.Fake {
	f := &execx.Fake{}
	if cols != "" {
		f.On([]string{"tput", "cols"}, execx.Result{Stdout: []byte(cols + "\n")})
	} else {
		f.On([]string{"tput", "cols"}, execx.Result{ExitCode: 1})
	}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	f.On([]string{"gum"}, execx.Result{})
	return f
}

// drawn is what the runner was asked to draw: every call but the width reader's tput cols.
func drawn(f *execx.Fake) []execx.Cmd {
	var out []execx.Cmd
	for _, c := range f.Calls() {
		if c.Name != "tput" {
			out = append(out, c)
		}
	}
	return out
}

// nihongo is three wide (Han) runes, two cells each.
const nihongo = "\u65e5\u672c\u8a9e"

// callOf is the first call to name.
func callOf(t *testing.T, f *execx.Fake, name string) execx.Cmd {
	t.Helper()
	for _, c := range f.Calls() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s call in %v", name, drawn(f))
	return execx.Cmd{}
}

// fzf truncates a header line wider than its window (the owner's rule: wrap, never truncate),
// so a heading is broken at its separators into lines that fit: on a 40-column terminal the
// framed picker draws 26 cells whole.
func TestALongHeadingWrapsAtItsSeparatorsToTheWidth(t *testing.T) {
	want := "Teams › alpha\n› Add LXC member\n· step 3/4 Checkout\ncontainer-a · user agent\n" + strings.Repeat("─", 24)
	if got := lxcStep.render(false, measuredHeaderWidth[40]); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// Nothing is cut off, lost or reordered at any width, in display cells: a wide (CJK) rune or an
// emoji takes two, and a grapheme cluster is never split.
func TestAHeadingFitsEveryWidthInCellsAndLosesNothing(t *testing.T) {
	for _, h := range []Heading{
		Here("Teams", "alpha", "Running sessions", "dev-session").Push("Move coordinator").
			About("session dev-session \u00b7 team alpha \u00b7 seat 2 \u00b7 now worker \u00b7 coordinator agent@container-b (running)"),
		Here("abcd", strings.Repeat(nihongo, 6)).About(nihongo + " \U0001F680 \u00b7 \U0001F469\u200d\U0001F4BB caf\u00e9 \u00b7 e\u0301e\u0301e\u0301"),
	} {
		for width := 2; width <= 80; width++ {
			fitsAndKeepsEverything(t, h, width)
		}
	}
}

// fitsAndKeepsEverything fails the test when h drawn at width has a line wider than width
// cells, a line that starts inside a grapheme cluster, lost or reordered text, or an ellipsis.
func fitsAndKeepsEverything(t *testing.T, h Heading, width int) {
	t.Helper()
	got := h.render(false, width)
	for l := range strings.SplitSeq(got, "\n") {
		if n := ansi.StringWidth(l); n > width {
			t.Fatalf("width %d: a line of %d cells: %q", width, n, l)
		}
		if r, _ := utf8.DecodeRuneInString(l); r == '\u0301' || r == '\u200d' {
			t.Fatalf("width %d: a grapheme cluster was split: %q", width, l)
		}
	}
	bare := strings.NewReplacer(" ", "", "\n", "", "\u203a", "", "\u00b7", "", "\u2500", "")
	if bare.Replace(got) != bare.Replace(h.Trail()+h.Context) {
		t.Fatalf("width %d lost text:\n%s", width, got)
	}
	if strings.Contains(got, "\u2026") || strings.Contains(got, "..") {
		t.Fatalf("width %d elides:\n%s", width, got)
	}
}

func TestAWideRuneHeadingIsWrappedByCellsNotRunes(t *testing.T) {
	got := Here("abcd", strings.Repeat(nihongo, 6)).render(false, measuredHeaderWidth[40])
	want := "abcd\n› " + strings.Repeat(nihongo, 4) + "\n" + strings.Repeat(nihongo, 2) + "\n" + strings.Repeat("─", 26)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestACrumbLongerThanTheWidthIsHardWrappedNotCut(t *testing.T) {
	got := Here("abcdefghijklmnopqrstuvwxyz0123456789").render(false, 16)
	want := "abcdefghijklmnop\nqrstuvwxyz012345\n6789\n" + strings.Repeat("─", 16)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A piece that fits a line but not with its mark keeps the mark, at the end of the line before.
func TestAMarkThatWouldPushAPieceOntoTwoLinesEndsTheLineBefore(t *testing.T) {
	got := Here("Teams", "abcdefghijklmnop").render(false, 16)
	want := "Teams ›\nabcdefghijklmnop\n" + strings.Repeat("─", 16)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := Here("A").About("a ·  · b").render(false, 40); !strings.HasPrefix(got, "A\na · b\n") {
		t.Fatalf("an empty part of the context draws a stray mark: %q", got)
	}
}

func TestTheRuleIsAsLongAsTheLongestLine(t *testing.T) {
	h := Here("Teams").About("a context line of forty characters, long")
	lines := strings.Split(h.render(false, 100), "\n")
	if rule := lines[len(lines)-1]; utf8.RuneCountInString(rule) != 40 {
		t.Fatalf("rule %q", rule)
	}
	if rule := strings.Split(Here("A").render(false, 100), "\n")[1]; utf8.RuneCountInString(rule) != shortestRule {
		t.Fatalf("a short heading still has a rule of %d: %q", shortestRule, rule)
	}
}

func TestAWrappedHeadingColoursOnlyItsCrumbs(t *testing.T) {
	lines := strings.Split(lxcStep.render(true, measuredHeaderWidth[40]), "\n")
	if lines[0] != headingColour+"Teams › alpha"+colourReset || lines[1] != headingColour+"› Add LXC member"+colourReset ||
		lines[2] != "· step 3/4 Checkout" {
		t.Fatalf("coloured %q", lines)
	}
}

// The framed picker wraps to what fzf was measured to draw whole; the width comes from the one
// reader, tput cols through the runner, and an unreadable width is the common 80 columns.
func TestThePickerWrapsToTheMeasuredWidthOfItsTerminal(t *testing.T) {
	ctx := context.Background()
	for cols, width := range measuredHeaderWidth {
		f := fakeColumns(strconv.Itoa(cols))
		p := &Picker{Runner: f, Plain: true}
		if _, err := p.Pick(ctx, lxcStep, PickSpec{Title: "t", Header: "legend", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
			t.Fatal(err)
		}
		if head := lxcStep.render(false, width) + "\n"; !slices.Contains(callOf(t, f, "fzf").Args, "--header="+head+"legend\n") {
			t.Fatalf("%d columns: pick header %q", cols, callOf(t, f, "fzf").Args)
		}
	}
	f := fakeColumns("")
	if _, err := (&Picker{Runner: f, Plain: true}).Pick(ctx, lxcStep, PickSpec{Title: "t", Header: "legend", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	if head := lxcStep.render(false, measuredHeaderWidth[fallbackColumns]) + "\n"; !slices.Contains(callOf(t, f, "fzf").Args, "--header="+head+"legend\n") {
		t.Fatalf("an unreadable width: pick header %q", callOf(t, f, "fzf").Args)
	}
}

// With WrapHeader, a header line under the heading wider than the window is wrapped at its
// spaces like the heading, never left for fzf to cut: on 40 columns the framed picker draws 26
// cells whole.
func TestAHeaderLineWiderThanTheWindowIsWrappedNotCut(t *testing.T) {
	f := fakeColumns("40")
	long := "Held by play-claude-yolo.yml (pid 123456), held since 12:00:05 UTC, for 3 minutes."
	if _, err := (&Picker{Runner: f, Plain: true}).Pick(context.Background(), lxcStep, PickSpec{Title: "t", Header: "legend\n" + long, Rows: []string{"r"}, WrapHeader: true}); !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	want := "--header=" + lxcStep.render(false, 26) + "\nlegend\nHeld by\nplay-claude-yolo.yml (pid\n123456), held since\n12:00:05 UTC, for 3\nminutes.\n"
	if got := callOf(t, f, "fzf").Args; !slices.Contains(got, want) {
		t.Fatalf("pick header %q\nwant %q", got, want)
	}
}

func TestGumAndTheUnframedChooseWrapToTheirOwnMargins(t *testing.T) {
	ctx := context.Background()
	f := fakeColumns("40")
	if _, err := (&Gum{Runner: f, Plain: true}).Confirm(ctx, lxcStep, "Add it?", true); err != nil {
		t.Fatal(err)
	}
	if head := lxcStep.render(false, 40-gumMargin) + "\n"; !slices.Contains(callOf(t, f, "gum").Args, head+"Add it?") {
		t.Fatalf("confirm %q", callOf(t, f, "gum").Args)
	}
	f = fakeColumns("40")
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("r\n")})
	if _, err := (&Picker{Runner: f, Plain: true}).Choose(ctx, lxcStep, ChooseSpec{Header: "h", Height: "50%", Rows: []string{"r"}}); err != nil {
		t.Fatal(err)
	}
	if head := lxcStep.render(false, 40-plainMargin) + "\n"; !slices.Contains(callOf(t, f, "fzf").Args, "--header="+head+"h") {
		t.Fatalf("choose %q", callOf(t, f, "fzf").Args)
	}
}

func TestHeadingTrailNamesThePathAndTheStep(t *testing.T) {
	if got := lxcStep.Trail(); got != "Teams › alpha › Add LXC member · step 3/4 Checkout" {
		t.Fatalf("trail %q", got)
	}
	if got := Here("CCY sessions").Trail(); got != "CCY sessions" {
		t.Fatalf("a heading with no step is its crumbs only: %q", got)
	}
}

func TestHeadingRendersTrailContextAndARulePlainOrInColour(t *testing.T) {
	plain := lxcStep.render(false, unwrapped)
	lines := strings.Split(plain, "\n")
	if len(lines) != 3 || lines[0] != lxcStep.Trail() || lines[1] != "container-a · user agent" {
		t.Fatalf("plain heading %q", plain)
	}
	if strings.Trim(lines[2], "─") != "" || len([]rune(lines[2])) < len([]rune(lines[0])) {
		t.Fatalf("the third line is a rule under the trail: %q", lines[2])
	}
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("plain has no colour: %q", plain)
	}
	coloured := lxcStep.render(true, unwrapped)
	if !strings.HasPrefix(coloured, "\x1b[1;33mTeams › alpha › Add LXC member\x1b[0m") {
		t.Fatalf("the crumbs are bold in the accent: %q", coloured)
	}
	if got := Here("A").render(false, unwrapped); strings.Count(got, "\n") != 1 {
		t.Fatalf("no context line when there is no context: %q", got)
	}
}

func TestDrawIsTheHeadingWrappedToAWidthAndRefusesOneWithNoPlace(t *testing.T) {
	got, err := lxcStep.Draw(false, 30)
	if err != nil || got != lxcStep.render(false, 30) {
		t.Fatalf("draw %q %v", got, err)
	}
	if _, err := (Heading{}).Draw(false, 30); !errors.Is(err, ErrNoHeading) {
		t.Fatalf("an empty heading drew: %v", err)
	}
}

func TestPushCopiesSoSiblingsDoNotShareCrumbs(t *testing.T) {
	base := Here("Teams")
	a, b := base.Push("alpha"), base.Push("beta")
	if a.Trail() != "Teams › alpha" || b.Trail() != "Teams › beta" || base.Trail() != "Teams" {
		t.Fatalf("a %q b %q base %q", a.Trail(), b.Trail(), base.Trail())
	}
	if got := base.Push("x").AtStep(1, 2, "One").Push("y").Trail(); got != "Teams › x › y" {
		t.Fatalf("a pushed screen starts with no step of its own: %q", got)
	}
}

// Every question and pick refuses to draw without a heading: a screen that does not say where
// the person is, is the defect this type exists to stop.
func TestEveryScreenRefusesToDrawWithoutAHeading(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	g, p := newGum(f), &Picker{Runner: f}
	for name, call := range map[string]func(Heading) error{
		"gum confirm": func(h Heading) error { _, err := g.Confirm(ctx, h, "q", true); return err },
		"gum input":   func(h Heading) error { _, err := g.Input(ctx, h, "h", "p", ""); return err },
		"gum choose":  func(h Heading) error { _, err := g.Choose(ctx, h, "h", "a", []string{"a"}); return err },
		"pick":        func(h Heading) error { _, err := p.Pick(ctx, h, PickSpec{Title: "t", Rows: []string{"r"}}); return err },
		"confirm":     func(h Heading) error { _, err := p.Confirm(ctx, h, "q", "go", true); return err },
		"choose": func(h Heading) error {
			_, err := p.Choose(ctx, h, ChooseSpec{Height: "50%", Rows: []string{"r"}})
			return err
		},
	} {
		for _, h := range []Heading{{}, {Crumbs: []string{"Teams", ""}}, {Crumbs: []string{"Teams", " \t"}}} {
			if err := call(h); !errors.Is(err, ErrNoHeading) {
				t.Errorf("%s with heading %+v: got %v, want ErrNoHeading", name, h, err)
			}
		}
	}
	if len(drawn(f)) != 0 {
		t.Fatalf("nothing is drawn without a heading: %v", drawn(f))
	}
}

func TestTheHeadingIsOnTopOfEveryGumQuestion(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	f.On([]string{"gum"}, execx.Result{Stdout: []byte("a\n")})
	g := &Gum{Runner: f, Plain: true}
	head := lxcStep.render(false, unwrapped) + "\n"
	if _, err := g.Confirm(ctx, lxcStep, "Add it?", true); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Input(ctx, lxcStep, "Checkout path", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Choose(ctx, lxcStep, "Role", "a", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	calls := drawn(f)
	if !slices.Contains(calls[0].Args, head+"Add it?") {
		t.Errorf("confirm prompt %q", calls[0].Args)
	}
	if !slices.Contains(calls[1].Args, head+"Checkout path") {
		t.Errorf("input header %q", calls[1].Args)
	}
	if !slices.Contains(calls[2].Args, head+"Role") {
		t.Errorf("choose header %q", calls[2].Args)
	}
}

func TestTheHeadingIsOnTopOfEveryFzfHeader(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	p := &Picker{Runner: f, Plain: true}
	head := lxcStep.render(false, unwrapped) + "\n"
	if _, err := p.Pick(ctx, lxcStep, PickSpec{Title: "t", Header: "legend", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	if !slices.Contains(drawn(f)[0].Args, "--header="+head+"legend\n") {
		t.Errorf("pick header %q", drawn(f)[0].Args)
	}
	_, err := p.Pick(ctx, lxcStep, PickSpec{Title: "t", Header: "base", Rows: []string{"a"}, RowHeaders: []string{"for a"}})
	if !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	args := drawn(f)[1].Args
	if !slices.Contains(args, "--header="+head+"for a\n") {
		t.Errorf("the first row's header keeps the heading: %q", args)
	}
	at := slices.IndexFunc(args, func(a string) bool { return strings.HasPrefix(a, "--bind=focus:transform-header") })
	if at < 0 {
		t.Fatalf("no header bind: %q", args)
	}
	if bind := args[at]; strings.Count(bind, lxcStep.Trail()) != 2 {
		t.Errorf("every header the cursor brings keeps the heading: %q", bind)
	}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("r\n")})
	if _, err := p.Choose(ctx, lxcStep, ChooseSpec{Header: "h", Height: "50%", Rows: []string{"r"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(drawn(f)[2].Args, "--header="+head+"h") {
		t.Errorf("choose header %q", drawn(f)[2].Args)
	}
}

// A row's menu is headed by its own Heading when it has one, else by the list's heading with
// the menu's title as one more crumb.
func TestARowMenuIsHeadedByItsOwnHeadingOrTheListsPlusItsTitle(t *testing.T) {
	for _, c := range []struct {
		menu Heading
		want string
	}{
		{Heading{}, "--header=Teams › team alpha\n"},
		{Here("Teams", "alpha").About("team alpha · up"), "--header=Teams › alpha\nteam alpha · up\n"},
	} {
		f := &execx.Fake{}
		fzfSays(f, "\nalpha\t0\n", "\nShut down alpha\t1\n")
		spec := teamsSpec()
		spec.Menus[0].Heading = c.menu
		if _, err := (&Picker{Runner: f, Plain: true}).Pick(context.Background(), Here("Teams"), spec); err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(drawn(f)[1].Args, func(a string) bool { return strings.HasPrefix(a, c.want) }) {
			t.Errorf("menu args %q, want a header starting %q", drawn(f)[1].Args, c.want)
		}
	}
}

func TestPickerHeadingIsColouredUnlessPlain(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	if _, err := (&Picker{Runner: f}).Pick(context.Background(), lxcStep, PickSpec{Title: "t", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(drawn(f)[0].Args, func(a string) bool { return strings.HasPrefix(a, "--header=\x1b[1;33m") }) {
		t.Fatalf("args %q", drawn(f)[0].Args)
	}
}

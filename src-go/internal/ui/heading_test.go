package ui

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

var lxcStep = Here("Teams", "alpha", "Add LXC member").AtStep(3, 4, "Checkout").About("container-a · user agent")

// TestMain stands in a terminal whose width cannot be read, so a picker or gum made with no
// Columns wraps its heading to fallbackHeadingWidth whatever terminal runs the tests.
func TestMain(m *testing.M) {
	terminalColumns = func() int { return 0 }
	os.Exit(m.Run())
}

// fzf and gum truncate a header line that is too wide (the owner's rule: wrap, never
// truncate), so a heading is broken at its separators into lines that fit: a 40-column
// terminal, less the picker's frame, leaves 30.
func TestALongHeadingWrapsAtItsSeparatorsToTheWidth(t *testing.T) {
	want := "Teams › alpha › Add LXC member\n· step 3/4 Checkout\ncontainer-a · user agent\n" + strings.Repeat("─", 30)
	if got := lxcStep.render(false, 30); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	long := Here("Teams", "alpha", "Running sessions", "dev-session").Push("Move coordinator").
		About("session dev-session · team alpha · seat 2 · now worker · coordinator agent@container-b (running)")
	for width := narrowestHeading; width <= 80; width++ {
		got := long.render(false, width)
		for l := range strings.SplitSeq(got, "\n") {
			if n := utf8.RuneCountInString(l); n > width {
				t.Fatalf("width %d: a line of %d: %q", width, n, l)
			}
		}
		// Nothing is lost or reordered: the text, less spaces and separator marks, is the same.
		bare := strings.NewReplacer(" ", "", "\n", "", "›", "", "·", "", "─", "")
		if bare.Replace(got) != bare.Replace(long.Trail()+long.Context) {
			t.Fatalf("width %d lost text:\n%s", width, got)
		}
		if strings.Contains(got, "…") || strings.Contains(got, "..") {
			t.Fatalf("width %d elides:\n%s", width, got)
		}
	}
}

func TestACrumbLongerThanTheWidthIsHardWrappedNotCut(t *testing.T) {
	got := Here("abcdefghijklmnopqrstuvwxyz0123456789").render(false, 16)
	want := "abcdefghijklmnop\nqrstuvwxyz012345\n6789\n" + strings.Repeat("─", 16)
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
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
	lines := strings.Split(lxcStep.render(true, 30), "\n")
	if lines[0] != headingColour+"Teams › alpha › Add LXC member"+colourReset || lines[1] != "· step 3/4 Checkout" {
		t.Fatalf("coloured %q", lines)
	}
}

func TestTheHeadingWidthIsTheTerminalsLessTheFrame(t *testing.T) {
	if got := headingWidth(40); got != 30 {
		t.Fatalf("40 columns: %d", got)
	}
	if got := headingWidth(12); got != narrowestHeading {
		t.Fatalf("12 columns: %d", got)
	}
	if got := headingWidth(0); got != fallbackHeadingWidth {
		t.Fatalf("an unreadable width: %d", got)
	}
	terminalColumns = func() int { return 120 }
	t.Cleanup(func() { terminalColumns = func() int { return 0 } })
	if got := headingWidth(0); got != 110 {
		t.Fatalf("a 120-column terminal: %d", got)
	}
}

func TestAPickerAndGumWrapTheHeadingToTheirColumns(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	f.On([]string{"gum"}, execx.Result{})
	head := lxcStep.render(false, 30) + "\n"
	p := &Picker{Runner: f, Plain: true, Columns: 40}
	if _, err := p.Pick(ctx, lxcStep, PickSpec{Title: "t", Header: "legend", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	if !slices.Contains(f.Calls()[0].Args, "--header="+head+"legend\n") {
		t.Fatalf("pick header %q", f.Calls()[0].Args)
	}
	g := &Gum{Runner: f, Plain: true, Columns: 40}
	if _, err := g.Confirm(ctx, lxcStep, "Add it?", true); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.Calls()[1].Args, head+"Add it?") {
		t.Fatalf("confirm %q", f.Calls()[1].Args)
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
	plain := lxcStep.render(false, fallbackHeadingWidth)
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
	coloured := lxcStep.render(true, fallbackHeadingWidth)
	if !strings.HasPrefix(coloured, "\x1b[1;33mTeams › alpha › Add LXC member\x1b[0m") {
		t.Fatalf("the crumbs are bold in the accent: %q", coloured)
	}
	if got := Here("A").render(false, fallbackHeadingWidth); strings.Count(got, "\n") != 1 {
		t.Fatalf("no context line when there is no context: %q", got)
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
	if len(f.Calls()) != 0 {
		t.Fatalf("nothing is drawn without a heading: %v", f.Calls())
	}
}

func TestTheHeadingIsOnTopOfEveryGumQuestion(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	f.On([]string{"gum"}, execx.Result{Stdout: []byte("a\n")})
	g := &Gum{Runner: f, Plain: true}
	head := lxcStep.render(false, fallbackHeadingWidth) + "\n"
	if _, err := g.Confirm(ctx, lxcStep, "Add it?", true); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Input(ctx, lxcStep, "Checkout path", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Choose(ctx, lxcStep, "Role", "a", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
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
	head := lxcStep.render(false, fallbackHeadingWidth) + "\n"
	if _, err := p.Pick(ctx, lxcStep, PickSpec{Title: "t", Header: "legend", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	if !slices.Contains(f.Calls()[0].Args, "--header="+head+"legend\n") {
		t.Errorf("pick header %q", f.Calls()[0].Args)
	}
	_, err := p.Pick(ctx, lxcStep, PickSpec{Title: "t", Header: "base", Rows: []string{"a"}, RowHeaders: []string{"for a"}})
	if !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	args := f.Calls()[1].Args
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
	if !slices.Contains(f.Calls()[2].Args, "--header="+head+"h") {
		t.Errorf("choose header %q", f.Calls()[2].Args)
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
		if !slices.ContainsFunc(f.Calls()[1].Args, func(a string) bool { return strings.HasPrefix(a, c.want) }) {
			t.Errorf("menu args %q, want a header starting %q", f.Calls()[1].Args, c.want)
		}
	}
}

func TestPickerHeadingIsColouredUnlessPlain(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	if _, err := (&Picker{Runner: f}).Pick(context.Background(), lxcStep, PickSpec{Title: "t", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.Calls()[0].Args, func(a string) bool { return strings.HasPrefix(a, "--header=\x1b[1;33m") }) {
		t.Fatalf("args %q", f.Calls()[0].Args)
	}
}

package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

var lxcStep = Here("Teams", "alpha", "Add LXC member").AtStep(3, 4, "Checkout").About("container-a · user agent")

func TestHeadingTrailNamesThePathAndTheStep(t *testing.T) {
	if got := lxcStep.Trail(); got != "Teams › alpha › Add LXC member · step 3/4 Checkout" {
		t.Fatalf("trail %q", got)
	}
	if got := Here("CCY sessions").Trail(); got != "CCY sessions" {
		t.Fatalf("a heading with no step is its crumbs only: %q", got)
	}
}

func TestHeadingRendersTrailContextAndARulePlainOrInColour(t *testing.T) {
	plain := lxcStep.render(false)
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
	coloured := lxcStep.render(true)
	if !strings.HasPrefix(coloured, "\x1b[1;33mTeams › alpha › Add LXC member\x1b[0m") {
		t.Fatalf("the crumbs are bold in the accent: %q", coloured)
	}
	if got := Here("A").render(false); strings.Count(got, "\n") != 1 {
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
		for _, h := range []Heading{{}, {Crumbs: []string{"Teams", ""}}} {
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
	head := lxcStep.render(false) + "\n"
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
	head := lxcStep.render(false) + "\n"
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
	bind := args[len(args)-1]
	if strings.Count(bind, lxcStep.Trail()) != 2 {
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

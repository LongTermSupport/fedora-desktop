package ui

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// drains is whether each prompt run so far asked for the keys typed before it to be discarded.
func drains(f *execx.Fake) []bool {
	var out []bool
	for _, c := range f.Calls() {
		if c.Name == "gum" && len(c.Args) > 0 && c.Args[0] == "style" || c.Name == "tput" {
			continue
		}
		out = append(out, c.DrainInput)
	}
	return out
}

func wantDrains(t *testing.T, f *execx.Fake, want ...bool) {
	t.Helper()
	got := drains(f)
	if len(got) != len(want) {
		t.Fatalf("ran %d prompts, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("prompt %d drained %t, want %t (all %v)", i+1, got[i], want[i], got)
		}
	}
}

// Plan 00177 Task 12.3: in ccy-teams remove, Esc answered "Back up first?" and a Down typed 20 ms
// later moved the next screen's cursor. A screen after one the person left starts on no key they
// typed before seeing it; a screen after an answer keeps them (ttyecho's type-ahead).
func TestTheGumPromptAfterALeaveDrainsAndTheOneAfterAnAnswerDoesNot(t *testing.T) {
	f := noTerminal()
	f.On([]string{"gum", "confirm"}, execx.Result{ExitCode: 1}) // Esc, or No: gum confirm exits 1
	f.On([]string{"gum", "choose"}, execx.Result{Stdout: []byte("a\n")})
	ctx := context.Background()
	g := newGum(f)
	yes, err := g.Confirm(ctx, testHead, "q", true)
	if err != nil || yes {
		t.Fatalf("confirm %t %v", yes, err)
	}
	for range 2 {
		if _, err := g.Choose(ctx, testHead, "h", "a", []string{"a", "b"}); err != nil {
			t.Fatal(err)
		}
	}
	wantDrains(t, f, false, true, false)
}

func TestACancelledInputDrainsTheNextPrompt(t *testing.T) {
	f := noTerminal()
	f.On([]string{"gum", "input"}, execx.Result{ExitCode: 130})
	f.On([]string{"gum", "confirm"}, execx.Result{})
	ctx := context.Background()
	g := newGum(f)
	if _, err := g.Input(ctx, testHead, "h", "p", ""); !errors.Is(err, ErrCancelled) {
		t.Fatalf("input %v", err)
	}
	if _, err := g.Confirm(ctx, testHead, "q", true); err != nil {
		t.Fatal(err)
	}
	wantDrains(t, f, false, true)
}

// A destructive flow drains every prompt, its first included, until it ends.
func TestEveryPromptOfADestructiveFlowDrains(t *testing.T) {
	f := noTerminal()
	f.On([]string{"gum", "confirm"}, execx.Result{})
	f.On([]string{"gum", "choose"}, execx.Result{Stdout: []byte("a\n")})
	ctx := context.Background()
	g := newGum(f)
	end := g.Destructive()
	if _, err := g.Confirm(ctx, testHead, "q", true); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Choose(ctx, testHead, "h", "a", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	end()
	if _, err := g.Choose(ctx, testHead, "h", "a", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	wantDrains(t, f, true, true, false)
}

// A Gum and a Picker of one command share a Drain: leaving a gum question drains the picker
// drawn next, and leaving the picker the gum question after it.
func TestASharedDrainCarriesALeaveAcrossGumAndThePicker(t *testing.T) {
	f := noTerminal()
	f.On([]string{"gum", "confirm"}, execx.Result{ExitCode: 1})
	f.On([]string{"fzf"}, execx.Result{ExitCode: fzfInterrupted})
	f.On([]string{"gum", "choose"}, execx.Result{Stdout: []byte("a\n")})
	ctx := context.Background()
	d := &Drain{}
	g := &Gum{Runner: f, Stderr: &bytes.Buffer{}, Plain: true, Drain: d}
	p := &Picker{Runner: f, Stderr: &bytes.Buffer{}, Plain: true, Drain: d}
	if _, err := g.Confirm(ctx, testHead, "q", true); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pick(ctx, testHead, PickSpec{Rows: []string{"a"}}); !errors.Is(err, ErrLeave) {
		t.Fatalf("pick %v", err)
	}
	if _, err := g.Choose(ctx, testHead, "h", "a", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	wantDrains(t, f, false, true, true)
}

func TestAPickerConfirmLeftOrAnsweredNoDrainsTheNextScreen(t *testing.T) {
	f := noTerminal()
	f.On([]string{"fzf"}, execx.Result{ExitCode: fzfInterrupted})
	ctx := context.Background()
	p := &Picker{Runner: f, Stderr: &bytes.Buffer{}, Plain: true}
	if yes, err := p.Confirm(ctx, testHead, "q", "go", false); err != nil || yes {
		t.Fatalf("confirm %t %v", yes, err)
	}
	if _, err := p.Choose(ctx, testHead, ChooseSpec{Rows: []string{"a"}, Height: "5", Prompt: "> "}); !errors.Is(err, ErrLeave) {
		t.Fatalf("choose %v", err)
	}
	end := p.Destructive()
	if _, err := p.Pick(ctx, testHead, PickSpec{Rows: []string{"a"}}); !errors.Is(err, ErrLeave) {
		t.Fatalf("pick %v", err)
	}
	end()
	wantDrains(t, f, false, true, true)
}

// Draining is the screen's own business: the flow's Destructive nests, and ends only with the
// outermost.
func TestDestructiveNests(t *testing.T) {
	f := noTerminal()
	f.On([]string{"gum", "confirm"}, execx.Result{})
	ctx := context.Background()
	g := newGum(f)
	outer := g.Destructive()
	inner := g.Destructive()
	inner()
	if _, err := g.Confirm(ctx, testHead, "q", true); err != nil {
		t.Fatal(err)
	}
	outer()
	if _, err := g.Confirm(ctx, testHead, "q", true); err != nil {
		t.Fatal(err)
	}
	wantDrains(t, f, true, false)
}

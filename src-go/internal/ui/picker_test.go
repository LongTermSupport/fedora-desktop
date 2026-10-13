package ui

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestPickReturnsKeyAndRowAndTreatsExitAsLeave(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("ctrl-e\nalpha   up\n")})
	p := &Picker{Runner: f}
	got, err := p.Pick(context.Background(), PickSpec{
		Title: "Agent teams", Header: "legend", Keys: []string{"ctrl-e", "?"},
		Rows: []string{"alpha   up"},
	})
	if err != nil || got.Key != "ctrl-e" || got.Row != "alpha   up" {
		t.Fatalf("got %+v, %v", got, err)
	}
	c := f.Calls()[0]
	if !slices.Contains(c.Args, "--expect=ctrl-e,?") || !slices.Contains(c.Args, "--border-label= Agent teams ") {
		t.Fatalf("args %v", c.Args)
	}
	if slices.Contains(c.Args, "--bind=start:last") {
		t.Fatal("cursor starts on the first row unless asked")
	}
	in, err := io.ReadAll(c.Stdin)
	if err != nil || string(in) != "alpha   up\nExit\n" {
		t.Fatalf("rows fed %q, %v", in, err)
	}

	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	if _, err := p.Pick(context.Background(), PickSpec{Title: "t", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatalf("the Exit row leaves, got %v", err)
	}
	f.On([]string{"fzf"}, execx.Result{ExitCode: 130})
	if _, err := p.Pick(context.Background(), PickSpec{Title: "t", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatalf("Esc or q leaves, got %v", err)
	}
}

func TestConfirmInPickerDefaultsToExitUnlessAskedForYes(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Yes, copy the password\n")})
	p := &Picker{Runner: f}
	yes, err := p.Confirm(context.Background(), "Copy the password?", "Put it on the clipboard.", "copy the password", true)
	if err != nil || !yes {
		t.Fatalf("got %v, %v", yes, err)
	}
	if slices.Contains(f.Calls()[0].Args, "--bind=start:last") {
		t.Fatal("a yes-default question starts on Yes")
	}
	if _, err := p.Confirm(context.Background(), "t", "q", "do it", false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.Calls()[1].Args, "--bind=start:last") {
		t.Fatal("the safe default starts on Exit")
	}
}

func TestHeaderJoinsLegendAndContextDroppingEmpty(t *testing.T) {
	if got := Header("Enter choose", "", "line two"); got != "↑↓ choose   Enter choose\nline two" {
		t.Fatalf("got %q", got)
	}
}

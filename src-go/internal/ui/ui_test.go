package ui

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// testHead is the heading of the screens these tests draw; plain, it adds testHeadText above.
var testHead = Here("Test")

const testHeadText = "Test\n" + "────────────────────────" + "\n"

func newGum(f *execx.Fake) *Gum {
	return &Gum{Runner: f, Stderr: &bytes.Buffer{}, Plain: true}
}

func TestConfirmMapsGumExitCodes(t *testing.T) {
	cases := []struct {
		code int
		want bool
		err  error
	}{
		{0, true, nil},
		{1, false, nil},
		{130, false, ErrCancelled},
		{2, false, ErrCancelled},
	}
	for _, c := range cases {
		f := &execx.Fake{}
		f.On([]string{"gum", "confirm"}, execx.Result{ExitCode: c.code})
		got, err := newGum(f).Confirm(context.Background(), testHead, "Create one now?", false)
		if got != c.want || !errors.Is(err, c.err) {
			t.Fatalf("exit %d: got %v, %v", c.code, got, err)
		}
		if line := f.Calls()[0].Line(); line != "gum confirm --default=false "+testHeadText+"Create one now?" {
			t.Fatalf("argv %q", line)
		}
	}
}

func TestGumEnvironmentUsesTheTerminalsColoursAndOneAccent(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gum"}, execx.Result{})
	if _, err := newGum(f).Confirm(context.Background(), testHead, "q", true); err != nil {
		t.Fatal(err)
	}
	env := f.Calls()[0].Env
	for _, want := range []string{
		"GUM_CONFIRM_SELECTED_BACKGROUND=3", "GUM_INPUT_HEADER_FOREGROUND=",
		"GUM_CONFIRM_SHOW_HELP=false", "GUM_CHOOSE_CURSOR_FOREGROUND=3",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("env lacks %s", want)
		}
	}
	for _, kv := range env {
		val := kv[strings.IndexByte(kv, '=')+1:]
		if len(val) > 1 && val[0] >= '0' && val[0] <= '9' {
			t.Errorf("a 256-colour value: %s", kv)
		}
	}
}

func TestInputTrimsAndCancels(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gum", "input"}, execx.Result{Stdout: []byte("  example-org/proj \n")})
	g := newGum(f)
	got, err := g.Input(context.Background(), testHead, "Repositories", "e.g. x/y", "example-org/proj")
	if err != nil || got != "example-org/proj" {
		t.Fatalf("got %q, %v", got, err)
	}
	want := "gum input --header " + testHeadText + "Repositories --placeholder e.g. x/y --value example-org/proj --width 0"
	if line := f.Calls()[0].Line(); line != want {
		t.Fatalf("argv %q", line)
	}
	f.On([]string{"gum", "input"}, execx.Result{ExitCode: 1})
	if _, err := g.Input(context.Background(), testHead, "h", "p", ""); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Esc (1) is a cancel, got %v", err)
	}
}

func TestChoose(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gum", "choose"}, execx.Result{Stdout: []byte("B\n")})
	got, err := newGum(f).Choose(context.Background(), testHead, "Trust", "A", []string{"A", "B"})
	if err != nil || got != "B" {
		t.Fatalf("got %q, %v", got, err)
	}
	if line := f.Calls()[0].Line(); line != "gum choose --header "+testHeadText+"Trust --selected A A B" {
		t.Fatalf("argv %q", line)
	}
}

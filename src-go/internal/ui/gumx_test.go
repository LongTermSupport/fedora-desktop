package ui

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestSpinRunsTheCommandBehindTheDoubleDash(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gum", "spin"}, execx.Result{})
	err := newGum(f).Spin(context.Background(), "Looking up example-org on GitHub…", "gh", "api", "users/example-org", "--silent")
	if err != nil {
		t.Fatal(err)
	}
	want := "gum spin --title Looking up example-org on GitHub… -- gh api users/example-org --silent"
	if line := f.Calls()[0].Line(); line != want {
		t.Fatalf("argv %q", line)
	}
	if len(f.Calls()[0].Env) == 0 {
		t.Fatal("spin must carry the gum environment")
	}
}

func TestSpinReportsTheCommandsFailureAsAnExitError(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gum", "spin"}, execx.Result{ExitCode: 1})
	err := newGum(f).Spin(context.Background(), "t", "gh")
	if exit, ok := errors.AsType[*execx.ExitError](err); !ok || exit.Code != 1 {
		t.Fatalf("got %v", err)
	}
}

func TestBoxIsABorderedStyleWithOptionalBold(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gum", "style"}, execx.Result{})
	g := newGum(f)
	if err := g.Box(context.Background(), true, "New agent team"); err != nil {
		t.Fatal(err)
	}
	if err := g.Box(context.Background(), false, "Team proj", "", "body"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if got, want := calls[0].Line(), "gum style --bold --border rounded --padding 0 1 New agent team"; got != want {
		t.Fatalf("got %q", got)
	}
	if got, want := calls[1].Line(), "gum style --border rounded --padding 0 1 Team proj  body"; got != want {
		t.Fatalf("got %q", got)
	}
	if calls[0].Stdout == nil {
		t.Fatal("a box is drawn on the terminal, not captured")
	}
}

func TestExplainWrapsToTheTerminalWidthLessFour(t *testing.T) {
	cases := []struct {
		tput   execx.Result
		indent int
		want   string
	}{
		{execx.Result{Stdout: []byte("100\n")}, 5, "gum style --width 96 --padding 0 0 0 5 hint"},
		// A narrow terminal is believed: an explanation wider than it would be cut.
		{execx.Result{Stdout: []byte("43\n")}, 0, "gum style --width 39 --padding 0 0 0 0 hint"},
		{execx.Result{Stdout: []byte("0\n")}, 0, "gum style --width 76 --padding 0 0 0 0 hint"},
		{execx.Result{Stdout: []byte("wide\n")}, 0, "gum style --width 76 --padding 0 0 0 0 hint"},
		{execx.Result{ExitCode: 1}, 0, "gum style --width 76 --padding 0 0 0 0 hint"},
	}
	for _, c := range cases {
		f := &execx.Fake{}
		f.On([]string{"tput", "cols"}, c.tput)
		f.On([]string{"gum", "style"}, execx.Result{})
		g := &Gum{Runner: f, Stderr: &bytes.Buffer{}}
		if err := g.Explain(context.Background(), "hint", c.indent); err != nil {
			t.Fatal(err)
		}
		calls := f.Calls()
		if got := calls[len(calls)-1].Line(); got != c.want {
			t.Errorf("tput %q: got %q want %q", c.tput.Stdout, got, c.want)
		}
	}
}

func TestExplainWithNoTputStillExplainsAtTheCommonWidth(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gum", "style"}, execx.Result{})
	g := &Gum{Runner: f, Stderr: &bytes.Buffer{}}
	if err := g.Explain(context.Background(), "hint", 0); err != nil {
		t.Fatal(err)
	}
	if !f.Called("gum style --width 76") {
		t.Fatalf("calls %v", f.Calls())
	}
}

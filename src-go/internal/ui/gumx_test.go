package ui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

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

// A tput that ran and could not say is the common width (above); one that could not run is a
// fault, returned with its cause and nothing drawn.
func TestExplainWithATputThatCannotRunIsTheError(t *testing.T) {
	f := &execx.Fake{}
	missing := &execx.NotInstalledError{Name: "tput"}
	f.OnError([]string{"tput"}, missing)
	f.On([]string{"gum", "style"}, execx.Result{})
	g := &Gum{Runner: f, Stderr: &bytes.Buffer{}}
	err := g.Explain(context.Background(), "hint", 0)
	if !errors.Is(err, missing) || !strings.Contains(err.Error(), "the terminal's width: ") {
		t.Fatalf("got %v", err)
	}
	if f.Called("gum") {
		t.Fatalf("drew with no width: %v", f.Calls())
	}
}

// Every question's heading reads the width the same way: a tput that cannot run stops it.
func TestAQuestionWithATputThatCannotRunIsTheError(t *testing.T) {
	f := &execx.Fake{}
	f.OnError([]string{"tput"}, errors.New("exec format error"))
	g := &Gum{Runner: f, Stderr: &bytes.Buffer{}, Plain: true}
	if _, err := g.Confirm(context.Background(), testHead, "sure?", false); err == nil || !strings.Contains(err.Error(), "exec format error") {
		t.Fatalf("confirm: %v", err)
	}
	p := &Picker{Runner: f, Plain: true}
	if _, err := p.Choose(context.Background(), testHead, ChooseSpec{Rows: []string{"a"}}); err == nil || !strings.Contains(err.Error(), "exec format error") {
		t.Fatalf("choose: %v", err)
	}
	if f.Called("gum") || f.Called("fzf") {
		t.Fatalf("asked with no width: %v", f.Calls())
	}
}

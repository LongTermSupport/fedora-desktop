package ui

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestChooseSingleEmitsTheLegacyArgvAndFeedsRowsOnStdin(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("beta\n")})
	p := &Picker{Runner: f}
	got, err := p.Choose(context.Background(), ChooseSpec{
		Prompt: "podfreeze: ", Header: "ENTER acts — ESC to quit", Height: "50%",
		Rows: []string{"alpha", "beta"},
	})
	if err != nil || !slices.Equal(got, []string{"beta"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	c := f.Calls()[0]
	want := []string{"--height=50%", "--reverse", "--prompt=podfreeze: ", "--header=ENTER acts — ESC to quit"}
	if !slices.Equal(c.Args, want) {
		t.Fatalf("argv %q, want %q", c.Args, want)
	}
	in, err := io.ReadAll(c.Stdin)
	if err != nil || string(in) != "alpha\nbeta\n" {
		t.Fatalf("stdin %q, %v", in, err)
	}
}

func TestChooseMultiAddsMultiFirstAndReturnsEveryLine(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("a  1\nb  2\n")})
	got, err := (&Picker{Runner: f}).Choose(context.Background(), ChooseSpec{
		Prompt: "In this group: ", Header: "h", Height: "60%", Multi: true, Rows: []string{"a  1", "b  2"},
	})
	if err != nil || !slices.Equal(got, []string{"a  1", "b  2"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	want := []string{"--multi", "--height=60%", "--reverse", "--prompt=In this group: ", "--header=h"}
	if !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("argv %q, want %q", f.Calls()[0].Args, want)
	}
}

func TestChooseEmptyOutputIsNoSelection(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{})
	got, err := (&Picker{Runner: f}).Choose(context.Background(), ChooseSpec{Rows: []string{"a"}})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

// The legacy shell treated every non-zero fzf status as a leave; only 1 (no match) and 130
// (Esc, Ctrl-C) are one (spec bug 2).
func TestChooseLeavesOnlyOnNoMatchOrInterrupt(t *testing.T) {
	for _, code := range []int{1, 130} {
		f := &execx.Fake{}
		f.On([]string{"fzf"}, execx.Result{ExitCode: code})
		if _, err := (&Picker{Runner: f}).Choose(context.Background(), ChooseSpec{}); !errors.Is(err, ErrLeave) {
			t.Fatalf("exit %d must leave, got %v", code, err)
		}
	}
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{ExitCode: 2, Stderr: []byte("bad option")})
	_, err := (&Picker{Runner: f}).Choose(context.Background(), ChooseSpec{})
	if err == nil || errors.Is(err, ErrLeave) {
		t.Fatalf("exit 2 is a failure, got %v", err)
	}
}

func TestChooseMissingFzfIsErrFzfMissing(t *testing.T) {
	f := &execx.Fake{}
	f.OnError([]string{"fzf"}, &execx.NotInstalledError{Name: "fzf"})
	if _, err := (&Picker{Runner: f}).Choose(context.Background(), ChooseSpec{}); !errors.Is(err, ErrFzfMissing) {
		t.Fatalf("got %v", err)
	}
}

package ui

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestPickReturnsKeyAndRowAndTreatsExitAsLeave(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("ctrl-e\nalpha   up\n")})
	p := &Picker{Runner: f}
	got, err := p.Pick(context.Background(), testHead, PickSpec{
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
	if _, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatalf("the Exit row leaves, got %v", err)
	}
	f.On([]string{"fzf"}, execx.Result{ExitCode: 130})
	if _, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"r"}}); !errors.Is(err, ErrLeave) {
		t.Fatalf("Esc or q leaves, got %v", err)
	}
}

func TestConfirmInPickerDefaultsToExitUnlessAskedForYes(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Yes, copy the password\n")})
	p := &Picker{Runner: f}
	yes, err := p.Confirm(context.Background(), Here("Teams", "Copy the password?"), "Put it on the clipboard.", "copy the password", true)
	if err != nil || !yes {
		t.Fatalf("got %v, %v", yes, err)
	}
	if slices.Contains(f.Calls()[0].Args, "--bind=start:last") {
		t.Fatal("a yes-default question starts on Yes")
	}
	if !slices.Contains(f.Calls()[0].Args, "--border-label= Teams ") {
		t.Fatalf("the frame is titled with the heading's first crumb: %q", f.Calls()[0].Args)
	}
	if _, err := p.Confirm(context.Background(), testHead, "q", "do it", false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.Calls()[1].Args, "--bind=start:last") {
		t.Fatal("the safe default starts on Exit")
	}
}

func TestTaggedRowsCarryAHiddenTailThatComesBack(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("alpha   up\t1\n")})
	p := &Picker{Runner: f}
	got, err := p.Pick(context.Background(), testHead, PickSpec{
		Title: "t", Rows: []string{"alpha   up", "alpha   up"}, Tags: []string{"0", "1"},
	})
	if err != nil || got.Row != "alpha   up" || got.Tag != "1" {
		t.Fatalf("got %+v, %v", got, err)
	}
	c := f.Calls()[0]
	if !slices.Contains(c.Args, "--delimiter=\t") || !slices.Contains(c.Args, "--with-nth=1") {
		t.Fatalf("args %v", c.Args)
	}
	in, err := io.ReadAll(c.Stdin)
	if err != nil || string(in) != "alpha   up\t0\nalpha   up\t1\nExit\n" {
		t.Fatalf("rows fed %q, %v", in, err)
	}

	if _, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"a", "b"}, Tags: []string{"0"}}); err == nil {
		t.Fatal("tags must match the rows one for one")
	}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	if _, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"a"}, Tags: []string{"0"}}); !errors.Is(err, ErrLeave) {
		t.Fatalf("the untagged Exit row still leaves, got %v", err)
	}
}

func TestPickSurfacesAFzfThatFailedAndOneThatIsMissing(t *testing.T) {
	spec := PickSpec{Title: "t", Rows: []string{"r"}}
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{ExitCode: 1})
	if _, err := (&Picker{Runner: f}).Pick(context.Background(), testHead, spec); !errors.Is(err, ErrLeave) {
		t.Fatalf("fzf exit 1 (no match) leaves, got %v", err)
	}
	// Spec bug 9.2: an fzf that crashed or was given a bad option is not the person pressing Esc.
	f.On([]string{"fzf"}, execx.Result{ExitCode: 2, Stderr: []byte("unknown option")})
	_, err := (&Picker{Runner: f}).Pick(context.Background(), testHead, spec)
	if err == nil || errors.Is(err, ErrLeave) {
		t.Fatalf("fzf exit 2 is a failure, not a leave: %v", err)
	}
	// Spec bug 9.1: a missing fzf is a failure with the install hint, not a leave.
	f.OnError([]string{"fzf"}, &execx.NotInstalledError{Name: "fzf"})
	_, err = (&Picker{Runner: f}).Pick(context.Background(), testHead, spec)
	if !errors.Is(err, ErrFzfMissing) || errors.Is(err, ErrLeave) {
		t.Fatalf("a missing fzf is ErrFzfMissing, got %v", err)
	}
	want := "fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it."
	if err.Error() != want {
		t.Fatalf("message %q", err.Error())
	}
}

func TestUntaggedPicksPassNoDelimiter(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("r\n")})
	if _, err := (&Picker{Runner: f}).Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"r"}}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.Calls()[0].Args, "--with-nth=1") {
		t.Fatalf("args %v", f.Calls()[0].Args)
	}
}

func TestRowHeadersFollowTheCursorAndTheExitRowKeepsTheBaseHeader(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Exit\n")})
	p := &Picker{Runner: f, Plain: true}
	_, err := p.Pick(context.Background(), testHead, PickSpec{
		Title: "t", Header: "base", Rows: []string{"a", "b"},
		RowHeaders: []string{"for a (it's)", "for b"},
	})
	if !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	args := f.Calls()[0].Args
	if !slices.Contains(args, "--header="+testHeadText+"for a (it's)\n") {
		t.Fatalf("the first row's header is shown at the start: %q", args)
	}
	want := "--bind=focus:transform-header^case {n} in 0) printf '%s\\n' '" + testHeadText + "for a (it'\\''s)';; " +
		"1) printf '%s\\n' '" + testHeadText + "for b';; *) printf '%s\\n' '" + testHeadText + "base';; esac^"
	if !slices.Contains(args, want) {
		t.Fatalf("args %q lack %q", args, want)
	}
	if !slices.Contains(f.Calls()[0].Env, "SHELL=/bin/sh") {
		t.Fatalf("the binding is sh, whatever the login shell is: %q", f.Calls()[0].Env)
	}
	if _, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"a"}, RowHeaders: []string{"x", "y"}}); err == nil {
		t.Fatal("headers that do not pair up with rows are refused")
	}
}

func TestRowHeadersTooLargeForOneArgumentAreRefusedNotTruncated(t *testing.T) {
	p := &Picker{Runner: &execx.Fake{}}
	rows := make([]string, 200)
	headers := make([]string, 200)
	for i := range rows {
		rows[i], headers[i] = "r", strings.Repeat("x", 1000)
	}
	_, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: rows, RowHeaders: headers})
	if !errors.Is(err, errHeadersTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestExitLabelNamesTheLastRow(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"fzf"}, execx.Result{Stdout: []byte("Back to the list\n")})
	p := &Picker{Runner: f}
	_, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"a"}, ExitLabel: "Back to the list"})
	if !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	in, readErr := io.ReadAll(f.Calls()[0].Stdin)
	if readErr != nil || string(in) != "a\nBack to the list\n" {
		t.Fatalf("rows fed %q, %v", in, readErr)
	}
}

func TestHeaderJoinsLegendAndContextDroppingEmpty(t *testing.T) {
	if got := Header("Enter choose", "", "line two"); got != "↑↓ choose   Enter choose\nline two" {
		t.Fatalf("got %q", got)
	}
}

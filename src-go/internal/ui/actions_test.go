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

// teamsSpec is a list with screen actions and a menu per row, as ccy-teams draws it.
func teamsSpec() PickSpec {
	return PickSpec{
		Title: "Agent teams", Header: "legend", Rows: []string{"alpha", "beta"},
		Actions: []Action{{ID: "sessions", Label: "Running sessions", Key: "tab"}, {ID: "help", Label: "Help"}},
		Menus: []Menu{
			{Title: "team alpha", Header: "What to do with alpha:", Actions: []Action{
				{ID: "element", Label: "Open alpha in Element", Key: "ctrl-e"}, {ID: "shutdown", Label: "Shut down alpha"},
			}},
			{Title: "team beta", Actions: []Action{{ID: "remove", Label: "Take beta down", Key: "ctrl-x"}}},
		},
	}
}

// fzfFails is an answer that is fzf failing (exit 2), not a choice.
const fzfFails = "<exit 2>"

// fzfSays queues fzf's answers, one per run, in order.
func fzfSays(f *execx.Fake, outs ...string) {
	f.OnFunc([]string{"fzf"}, func(execx.Cmd) (execx.Result, error) {
		if len(outs) == 0 {
			return execx.Result{ExitCode: 2}, nil
		}
		o := outs[0]
		outs = outs[1:]
		if o == fzfFails {
			return execx.Result{ExitCode: 2}, nil
		}
		return execx.Result{Stdout: []byte(o)}, nil
	})
}

func TestEveryActionIsAVisibleRowOrMenuLineWithItsKeyBesideIt(t *testing.T) {
	f := &execx.Fake{}
	p := &Picker{Runner: f}
	fzfSays(f, "\nalpha\t0\n", "\nShut down alpha\t1\n")
	got, err := p.Pick(context.Background(), teamsSpec())
	if err != nil || got.Action != "shutdown" || got.Row != "alpha" {
		t.Fatalf("Enter on a row then a menu line: got %+v, %v", got, err)
	}
	calls := f.Calls()
	list, menu := calls[0], calls[1]
	if !slices.Contains(list.Args, "--expect=tab,ctrl-e,ctrl-x") {
		t.Fatalf("the list takes exactly the declared keys: %v", list.Args)
	}
	in, err := io.ReadAll(list.Stdin)
	if err != nil || string(in) != "alpha\t0\nbeta\t1\nRunning sessions   (Tab)\t2\nHelp\t3\nExit\n" {
		t.Fatalf("list rows fed %q, %v", in, err)
	}
	in, err = io.ReadAll(menu.Stdin)
	if err != nil || string(in) != "Open alpha in Element   (Ctrl-E)\t0\nShut down alpha\t1\nBack to the list\n" {
		t.Fatalf("menu rows fed %q, %v", in, err)
	}
	if !slices.Contains(menu.Args, "--border-label= team alpha ") || !slices.Contains(menu.Args, "--header=What to do with alpha:\n") {
		t.Fatalf("menu args %v", menu.Args)
	}
	if !slices.Contains(menu.Args, "--expect=ctrl-e") {
		t.Fatalf("a menu's line keys choose its lines: %v", menu.Args)
	}
}

func TestAKeyChoosesOnlyWhatTheScreenOrTheRowOffers(t *testing.T) {
	cases := []struct {
		out, action, row string
		notOffered       bool
	}{
		{"ctrl-e\nalpha\t0\n", "element", "alpha", false},
		{"tab\nbeta\t1\n", "sessions", "", false},
		{"tab\nExit\n", "sessions", "", false},
		{"ctrl-x\nalpha\t0\n", "", "alpha", true},
		{"ctrl-e\nRunning sessions   (Tab)\t2\n", "", "", true},
		{"ctrl-x\nExit\n", "", "", true},
		{"\nRunning sessions   (Tab)\t2\n", "sessions", "", false},
		{"\nHelp\t3\n", "help", "", false},
	}
	for _, c := range cases {
		f := &execx.Fake{}
		fzfSays(f, c.out)
		got, err := (&Picker{Runner: f}).Pick(context.Background(), teamsSpec())
		no, isNo := errors.AsType[*NotOfferedError](err)
		if isNo != c.notOffered || got.Action != c.action || got.Row != c.row {
			t.Errorf("%q: got %+v, %v", c.out, got, err)
		}
		if isNo && (no.Key == "" || !strings.Contains(no.Error(), KeyName(no.Key))) {
			t.Errorf("%q: the refusal names the key: %v", c.out, err)
		}
	}
}

func TestLeavingARowMenuIsBackNotLeave(t *testing.T) {
	f := &execx.Fake{}
	fzfSays(f, "\nbeta\t1\n", "\nBack to the list\n")
	_, err := (&Picker{Runner: f}).Pick(context.Background(), teamsSpec())
	if !errors.Is(err, ErrBack) || errors.Is(err, ErrLeave) {
		t.Fatalf("got %v", err)
	}
	f = &execx.Fake{}
	fzfSays(f, "\nbeta\t1\n", "ctrl-x\nBack to the list\n")
	got, err := (&Picker{Runner: f}).Pick(context.Background(), teamsSpec())
	if err != nil || got.Action != "remove" || got.Row != "beta" {
		t.Fatalf("a menu's own key chooses its line wherever the cursor is: %+v, %v", got, err)
	}
	f = &execx.Fake{}
	fzfSays(f, "\nbeta\t1\n", fzfFails)
	if _, err := (&Picker{Runner: f}).Pick(context.Background(), teamsSpec()); err == nil || errors.Is(err, ErrBack) {
		t.Fatalf("a failing menu is a failure: %v", err)
	}
}

func TestActionsThatCannotBeShownAreRefused(t *testing.T) {
	bad := map[string]PickSpec{
		"key with no label":     {Rows: []string{"a"}, Actions: []Action{{ID: "x", Key: "ctrl-x"}}},
		"no id":                 {Rows: []string{"a"}, Actions: []Action{{Label: "X"}}},
		"menu key, no label":    {Rows: []string{"a"}, Menus: []Menu{{Actions: []Action{{ID: "x", Key: "ctrl-x"}}}}},
		"menus not one per row": {Rows: []string{"a", "b"}, Menus: []Menu{{}}},
		"key twice on screen":   {Rows: []string{"a"}, Actions: []Action{{ID: "x", Label: "X", Key: "ctrl-x"}, {ID: "y", Label: "Y", Key: "ctrl-x"}}},
		"key in screen and menu": {
			Rows: []string{"a"}, Actions: []Action{{ID: "x", Label: "X", Key: "ctrl-x"}},
			Menus: []Menu{{Actions: []Action{{ID: "y", Label: "Y", Key: "ctrl-x"}}}},
		},
		"navigation key":  {Rows: []string{"a"}, Actions: []Action{{ID: "x", Label: "X", Key: "enter"}}},
		"q is leave":      {Rows: []string{"a"}, Actions: []Action{{ID: "x", Label: "X", Key: "q"}}},
		"menu key twice":  {Rows: []string{"a"}, Menus: []Menu{{Actions: []Action{{ID: "x", Label: "X", Key: "?"}, {ID: "y", Label: "Y", Key: "?"}}}}},
		"headers per row": {Rows: []string{"a"}, Actions: []Action{{ID: "x", Label: "X"}}, RowHeaders: []string{"1", "2", "3"}},
	}
	for name, spec := range bad {
		spec.Title = "t"
		if _, err := (&Picker{Runner: &execx.Fake{}}).Pick(context.Background(), spec); !errors.Is(err, ErrBadAction) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestActionRowsCanHaveHeadersOfTheirOwn(t *testing.T) {
	f := &execx.Fake{}
	fzfSays(f, "\nExit\n")
	_, err := (&Picker{Runner: f}).Pick(context.Background(), PickSpec{
		Title: "t", Header: "base", Rows: []string{"a"}, Actions: []Action{{ID: "s", Label: "S", Key: "tab"}},
		RowHeaders: []string{"for a", "for S"},
	})
	if !errors.Is(err, ErrLeave) {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.Calls()[0].Args, func(a string) bool { return strings.Contains(a, "1) printf '%s\\n' 'for S'") }) {
		t.Fatalf("args %q", f.Calls()[0].Args)
	}
}

func TestKeyNameIsHowAPersonReadsTheKey(t *testing.T) {
	for key, want := range map[string]string{"ctrl-e": "Ctrl-E", "tab": "Tab", "?": "?", "alt-x": "Alt-X", "f5": "F5", "ctrl-alt-x": "Ctrl-Alt-X"} {
		if got := KeyName(key); got != want {
			t.Errorf("KeyName(%q) = %q, want %q", key, got, want)
		}
	}
	if got := (Action{Label: "Help", Key: "?"}).Line(); got != "Help   (?)" {
		t.Errorf("Line = %q", got)
	}
	if got := (Action{Label: "Attach"}).Line(); got != "Attach" {
		t.Errorf("Line = %q", got)
	}
}

func TestAnAnswerThatIsNotInTheListIsAnError(t *testing.T) {
	for _, out := range []string{"ctrl-e", "\nalpha\t9\n", "\nalpha\tx\n", "\nnot a row\n"} {
		f := &execx.Fake{}
		fzfSays(f, out)
		if _, err := (&Picker{Runner: f}).Pick(context.Background(), teamsSpec()); err == nil || errors.Is(err, ErrLeave) {
			t.Errorf("%q: want an error, got %v", out, err)
		}
	}
}

func TestAPlainScreenActionNeedsNoMenus(t *testing.T) {
	f := &execx.Fake{}
	fzfSays(f, "?\nr\t0\n")
	got, err := (&Picker{Runner: f}).Pick(context.Background(), PickSpec{
		Title: "t", Rows: []string{"r"}, Tags: []string{"row-r"}, Actions: []Action{{ID: "help", Label: "Help", Key: "?"}},
	})
	if err != nil || got.Action != "help" {
		t.Fatalf("got %+v, %v", got, err)
	}
	fzfSays(f, "\nr\t0\n")
	got, err = (&Picker{Runner: f}).Pick(context.Background(), PickSpec{
		Title: "t", Rows: []string{"r"}, Tags: []string{"row-r"}, Actions: []Action{{ID: "help", Label: "Help", Key: "?"}},
	})
	if err != nil || got.Row != "r" || got.Tag != "row-r" || got.Action != "" {
		t.Fatalf("Enter on a row with no menu is the row: %+v, %v", got, err)
	}
}

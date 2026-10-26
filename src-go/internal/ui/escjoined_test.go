package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// fzf waits up to 100 ms after an Esc for the rest of an escape sequence, and reads an Esc and the
// key after it, in one read or within that wait, as that key with Alt: Esc then Down is alt-down,
// Esc then Enter is ctrl-alt-m (alt-enter in a newer fzf), Esc then Ctrl-R is ctrl-alt-r. tmux,
// before 3.5, does the same within its escape-time (500 ms) and hands the pane the joined bytes.
// A screen with no meaning for those keys dropped the Esc: the person stayed on a menu they
// thought they had left, and their next keys acted on it (Plan 00177, the host agent's finding:
// Down within about 50 ms of Esc, or both in one tmux send-keys).

// wordKeys are fzf's default word editing keys (backward-word, forward-word, kill-word,
// backward-kill-word). They keep that meaning: bound to abort they would throw away the query.
var wordKeys = []string{"alt-b", "alt-f", "alt-d", "alt-bs"}

// joinedKeys are the keys fzf hands over for an Esc and the key after it: the arrows, Enter,
// Space, every letter and digit, and every Ctrl+letter, less wordKeys.
func joinedKeys() []string {
	keys := []string{"alt-up", "alt-down", "alt-left", "alt-right", "alt-enter", "alt-space"}
	for c := 'a'; c <= 'z'; c++ {
		keys = append(keys, "alt-"+string(c), "ctrl-alt-"+string(c))
	}
	for c := '0'; c <= '9'; c++ {
		keys = append(keys, "alt-"+string(c))
	}
	return slices.DeleteFunc(keys, func(k string) bool { return slices.Contains(wordKeys, k) })
}

// boundTo is what each key is bound to in fzf's --bind arguments: "key:action" pairs, comma
// separated. Bindings with arguments (focus:transform-header^...^) are skipped.
func boundTo(args []string) map[string]string {
	bound := map[string]string{}
	for _, a := range args {
		spec, ok := strings.CutPrefix(a, "--bind=")
		if !ok || strings.ContainsAny(spec, "^()") {
			continue
		}
		for pair := range strings.SplitSeq(spec, ",") {
			key, action, found := strings.Cut(pair, ":")
			if found {
				bound[key] = action
			}
		}
	}
	return bound
}

// expected is the keys of fzf's --expect argument.
func expected(args []string) []string {
	for _, a := range args {
		if keys, ok := strings.CutPrefix(a, "--expect="); ok {
			return strings.Split(keys, ",")
		}
	}
	return nil
}

// leavesOnEveryJoinedKey fails unless each key fzf can hand over for an Esc joined to the next key
// leaves the screen, as Esc does: bound to abort, or expected so the caller reads it as Esc.
func leavesOnEveryJoinedKey(t *testing.T, screen string, args []string) {
	t.Helper()
	bound, exp := boundTo(args), expected(args)
	for _, k := range joinedKeys() {
		if bound[k] != "abort" && !slices.Contains(exp, k) {
			t.Errorf("%s: %s (an Esc joined to the next key) is bound to %q, not abort: the Esc is lost", screen, k, bound[k])
		}
	}
	for _, k := range wordKeys {
		if bound[k] == "abort" {
			t.Errorf("%s: %s is fzf's word editing, bound to abort: the typed query is thrown away", screen, k)
		}
	}
}

func TestAnEscJoinedToTheNextKeyLeavesEveryFzfScreen(t *testing.T) {
	ctx := context.Background()
	f := &execx.Fake{}
	fzfSays(f, "\nalpha\t0\n", "\nBack to the list\n", "Exit\n", "r\n", "\nr\t0\n")
	p := &Picker{Runner: f, Plain: true}
	if _, err := p.Pick(ctx, testHead, teamsSpec()); !errors.Is(err, ErrBack) {
		t.Fatalf("the list and a row's menu: %v", err)
	}
	if _, err := p.Confirm(ctx, testHead, "Sure?", "do it", false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Choose(ctx, testHead, ChooseSpec{Prompt: "> ", Height: "50%", Rows: []string{"r"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pick(ctx, testHead, PickSpec{Title: "t", Rows: []string{"r"}, Actions: []Action{{ID: "h", Label: "Help", Key: "?"}}}); err != nil {
		t.Fatal(err)
	}
	screens := []string{"the list", "a row's menu", "a yes/no", "a plain choice", "a screen with an action"}
	calls := drawn(f)
	if len(calls) != len(screens) {
		t.Fatalf("%d fzf screens, want %d", len(calls), len(screens))
	}
	for i, c := range calls {
		leavesOnEveryJoinedKey(t, screens[i], c.Args)
	}
}

// Esc then Down in a row's menu is Esc, then Down on the list: the menu closes and the row it was
// opened on comes back with the cursor, so the list can move it. Esc then Up the same.
func TestEscThenAnArrowInARowMenuLeavesItForTheRowItWasOpenedOn(t *testing.T) {
	for _, arrow := range []string{"alt-down", "alt-up"} {
		f := &execx.Fake{}
		fzfSays(f, "\nbeta\t1\n", arrow+"\nTake beta down   (Ctrl-X)\t0\n")
		got, err := (&Picker{Runner: f, Plain: true}).Pick(context.Background(), testHead, teamsSpec())
		if !errors.Is(err, ErrBack) || errors.Is(err, ErrLeave) {
			t.Fatalf("%s in the menu: want back to the list, got %+v, %v", arrow, got, err)
		}
		if got.Action != "" || got.Row != "beta" {
			t.Fatalf("%s: nothing is chosen and the row is the menu's: %+v", arrow, got)
		}
		if menu := drawn(f)[1]; !slices.Contains(expected(menu.Args), arrow) {
			t.Fatalf("the menu hands back %s so the list can move: %q", arrow, menu.Args)
		}
	}
}

// The list says how far its cursor moves after Esc then an arrow: up -1, down 1, else 0.
func TestMovedByIsTheArrowAfterTheEsc(t *testing.T) {
	for arrow, want := range map[string]int{"alt-down": 1, "alt-up": -1} {
		f := &execx.Fake{}
		fzfSays(f, "\nbeta\t1\n", arrow+"\nTake beta down   (Ctrl-X)\t0\n")
		_, err := (&Picker{Runner: f, Plain: true}).Pick(context.Background(), testHead, teamsSpec())
		if got := MovedBy(err); got != want {
			t.Errorf("%s: moved %d, want %d", arrow, got, want)
		}
	}
	if MovedBy(ErrBack) != 0 || MovedBy(nil) != 0 || MovedBy(ErrLeave) != 0 {
		t.Fatal("only Esc then an arrow moves the cursor")
	}
	if (&BackError{Move: 1}).Error() != ErrBack.Error() {
		t.Fatal("a BackError reads as ErrBack")
	}
}

// A list given a start row starts its cursor there, over CursorLast; a list with none starts as
// before.
func TestAListCanStartOnAGivenRow(t *testing.T) {
	f := &execx.Fake{}
	fzfSays(f, "r\n", "r\n")
	p := &Picker{Runner: f, Plain: true}
	if _, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"q", "r"}, StartRow: 2, CursorLast: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pick(context.Background(), testHead, PickSpec{Title: "t", Rows: []string{"q", "r"}}); err != nil {
		t.Fatal(err)
	}
	calls := drawn(f)
	if !slices.Contains(calls[0].Args, "--bind=start:pos(2)") || slices.Contains(calls[0].Args, "--bind=start:last") {
		t.Fatalf("start row 2: %q", calls[0].Args)
	}
	if slices.ContainsFunc(calls[1].Args, func(a string) bool { return strings.HasPrefix(a, "--bind=start:") }) {
		t.Fatalf("no start row: %q", calls[1].Args)
	}
}

// A plain Esc from a row's menu (fzf exits 130) is back to the list on the same row.
func TestLeavingARowMenuNamesTheRowItWasOpenedOn(t *testing.T) {
	f := &execx.Fake{}
	runs := 0
	f.OnFunc([]string{"fzf"}, func(execx.Cmd) (execx.Result, error) {
		runs++
		if runs == 1 {
			return execx.Result{Stdout: []byte("\nbeta\t1\n")}, nil
		}
		return execx.Result{ExitCode: 130}, nil
	})
	got, err := (&Picker{Runner: f}).Pick(context.Background(), testHead, teamsSpec())
	if !errors.Is(err, ErrBack) || got.Row != "beta" || got.Action != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

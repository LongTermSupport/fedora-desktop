package fzfbind

import (
	"slices"
	"strings"
	"testing"
)

func TestEscJoinedIsTheKeysAnEscAndTheNextKeyBecome(t *testing.T) {
	keys := EscJoined()
	if len(keys) != 65 {
		t.Fatalf("%d keys, want 65: the arrows, Enter, Space, a-z, Ctrl+a-z, 0-9, less b, f, d", len(keys))
	}
	for _, k := range []string{"alt-down", "alt-up", "alt-enter", "alt-q", "ctrl-alt-m", "ctrl-alt-r", "ctrl-alt-b", "alt-0"} {
		if !slices.Contains(keys, k) {
			t.Errorf("%s is missing", k)
		}
	}
	for _, k := range WordEditing() {
		if slices.Contains(keys, k) {
			t.Errorf("%s edits fzf's query by word: it must not be required to abort", k)
		}
	}
}

// fzf's word editing keys keep their meaning: bound to abort they lose the typed query.
func TestAbortsWordEditingIsTheWordKeysBoundToAbort(t *testing.T) {
	if !slices.Equal(WordEditing(), []string{"alt-b", "alt-f", "alt-d", "alt-bs"}) {
		t.Fatalf("word editing keys %v", WordEditing())
	}
	if got := AbortsWordEditing([]string{"alt-b:abort,alt-f:forward-word", "alt-bs:abort", "alt-d:abort+clear-query"}); !slices.Equal(got, []string{"alt-b", "alt-bs"}) {
		t.Fatalf("alt-b and alt-bs bound to abort: got %v", got)
	}
	if got := AbortsWordEditing([]string{"alt-b:abort", "alt-b:backward-word"}); len(got) != 0 {
		t.Fatalf("a later binding replaces abort: got %v", got)
	}
	all := make([]string, 0, len(EscJoined()))
	for _, k := range EscJoined() {
		all = append(all, k+":abort")
	}
	if got := AbortsWordEditing([]string{strings.Join(all, ",")}); len(got) != 0 {
		t.Fatalf("the Esc-joined bindings abort no word key: got %v", got)
	}
}

func TestDrawsNoScreenIsTheOptionsThatReadNoKey(t *testing.T) {
	for _, w := range []string{"--version", "--help", "-h", "--bash", "--zsh", "--fish", "--man", "--filter", "-f", "--filter=x"} {
		if !DrawsNoScreen(w) {
			t.Errorf("%s draws no screen", w)
		}
	}
	for _, w := range []string{"--height=40%", "--read0", "--bind=q:abort", "--filterx", "-m"} {
		if DrawsNoScreen(w) {
			t.Errorf("%s draws a screen", w)
		}
	}
}

func TestNotLeavingIsTheJoinedKeysNoSpecBindsToAbort(t *testing.T) {
	all := make([]string, 0, len(EscJoined()))
	for _, k := range EscJoined() {
		all = append(all, k+":abort")
	}
	whole := strings.Join(all, ",")
	if got := NotLeaving([]string{whole}); len(got) != 0 {
		t.Fatalf("every key bound to abort, still missing %v", got)
	}
	half := len(all) / 2
	split := []string{strings.Join(all[:half], ","), "q:abort", strings.Join(all[half:], ",")}
	if got := NotLeaving(split); len(got) != 0 {
		t.Fatalf("bound across several --bind specs, still missing %v", got)
	}
	if got := NotLeaving([]string{"q:abort"}); len(got) != len(EscJoined()) {
		t.Fatalf("only q bound: %d missing, want all %d", len(got), len(EscJoined()))
	}
	moved := strings.Replace(whole, "alt-down:abort", "alt-down:down", 1)
	if got := NotLeaving([]string{moved}); !slices.Equal(got, []string{"alt-down"}) {
		t.Fatalf("alt-down bound to down: want only it missing, got %v", got)
	}
	chained := strings.Replace(whole, "alt-j:abort", "alt-j:abort+execute(x)", 1)
	if got := NotLeaving([]string{chained}); !slices.Equal(got, []string{"alt-j"}) {
		t.Fatalf("alt-j chained past abort: want only it missing, got %v", got)
	}
	if got := NotLeaving([]string{"not a binding"}); len(got) != len(EscJoined()) {
		t.Fatalf("an unreadable spec binds nothing: %d missing", len(got))
	}
}

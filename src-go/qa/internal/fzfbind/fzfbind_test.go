package fzfbind

import (
	"slices"
	"testing"
)

func TestRead(t *testing.T) {
	cases := []struct {
		spec     string
		bindings []Binding
		open     string
	}{
		{"q:abort", []Binding{{Trigger: "q", Actions: []string{"abort"}}}, ""},
		{"tab:toggle+down", []Binding{{Trigger: "tab", Actions: []string{"toggle", "down"}}}, ""},
		{"ctrl-j:down,ctrl-k:up", []Binding{{Trigger: "ctrl-j", Actions: []string{"down"}}, {Trigger: "ctrl-k", Actions: []string{"up"}}}, ""},
		{
			"ctrl-x:execute(rm {}),ctrl-a:accept",
			[]Binding{{Trigger: "ctrl-x", Actions: []string{"execute"}}, {Trigger: "ctrl-a", Actions: []string{"accept"}}},
			"",
		},
		{"focus:transform-header^case {n} in ", []Binding{{Trigger: "focus", Actions: []string{"transform-header"}}}, "^"},
		{"ctrl-o:execute:vim {}", []Binding{{Trigger: "ctrl-o", Actions: []string{"execute"}}}, ""},
		{",:accept", []Binding{{Trigger: ",", Actions: []string{"accept"}}}, ""},
		{"bspace:backward-delete-char/eof", []Binding{{Trigger: "bspace", Actions: []string{"backward-delete-char/eof"}}}, ""},
		{"%s:accept", []Binding{{Trigger: "%s", Actions: []string{"accept"}}}, ""},
		{"start:transform-query:printf %s \"$Q\"", []Binding{{Trigger: "start", Actions: []string{"transform-query"}}}, ""},
		{"ctrl-x:execute(rm)+accept", []Binding{{Trigger: "ctrl-x", Actions: []string{"execute", "accept"}}}, ""},
	}
	for _, c := range cases {
		got, open, err := Read(c.spec)
		if err != nil || open != c.open || len(got) != len(c.bindings) {
			t.Errorf("Read(%q) = %+v open=%q err=%v, want %+v open=%q", c.spec, got, open, err, c.bindings, c.open)
			continue
		}
		for i := range got {
			if got[i].Trigger != c.bindings[i].Trigger || !slices.Equal(got[i].Actions, c.bindings[i].Actions) {
				t.Errorf("Read(%q)[%d] = %+v, want %+v", c.spec, i, got[i], c.bindings[i])
			}
		}
	}
}

func TestReadRefusesWhatItCannotSee(t *testing.T) {
	for _, bad := range []string{"", "ctrl-a", "ctrl-a:", ":accept", "ctrl-a:Accept", "ctrl-a:accept,"} {
		if got, _, err := Read(bad); err == nil {
			t.Errorf("Read(%q) = %+v, want an error", bad, got)
		}
	}
}

func TestHiddenAction(t *testing.T) {
	cases := []struct {
		b    Binding
		want string
	}{
		{Binding{Trigger: "q", Actions: []string{"abort"}}, ""},
		{Binding{Trigger: "tab", Actions: []string{"toggle", "down"}}, ""},
		{Binding{Trigger: "ctrl-r", Actions: []string{"toggle-sort"}}, ""},
		{Binding{Trigger: "enter", Actions: []string{"accept"}}, ""},
		{Binding{Trigger: "double-click", Actions: []string{"accept"}}, ""},
		{Binding{Trigger: "start", Actions: []string{"last"}}, ""},
		{Binding{Trigger: "focus", Actions: []string{"transform-header"}}, ""},
		{Binding{Trigger: "change", Actions: []string{"reload"}}, ""},
		// Without --expect fzf cannot say which key accepted: any key accepting the highlighted
		// row is Enter.
		{Binding{Trigger: "ctrl-a", Actions: []string{"accept"}}, ""},
		{Binding{Trigger: "tab", Actions: []string{"toggle", "accept"}}, ""},
		{Binding{Trigger: "ctrl-j", Actions: []string{"accept-non-empty"}}, ""},
		// Display only: how the same rows are shown.
		{Binding{Trigger: "ctrl-w", Actions: []string{"change-preview-window"}}, ""},
		{Binding{Trigger: "ctrl-t", Actions: []string{"toggle-track"}}, ""},
		{Binding{Trigger: "ctrl-v", Actions: []string{"preview"}}, ""},
		{Binding{Trigger: "alt-p", Actions: []string{"change-preview", "change-prompt", "change-header", "change-border-label"}}, ""},
		{Binding{Trigger: "alt-t", Actions: []string{"track", "untrack", "toggle-search"}}, ""},
		{Binding{Trigger: "ctrl-o", Actions: []string{"accept", "become"}}, "become"},
		// A query edit or a select-all before accepting is read back by the caller
		// (--print-query, --multi): the key carries an action out (Fable's re-check, F1).
		{Binding{Trigger: "ctrl-s", Actions: []string{"put", "accept"}}, "put+accept"},
		{Binding{Trigger: "enter", Actions: []string{"clear-query", "accept"}}, "clear-query+accept"},
		{Binding{Trigger: "ctrl-a", Actions: []string{"select-all", "accept"}}, "select-all+accept"},
		{Binding{Trigger: "ctrl-t", Actions: []string{"toggle-all", "down", "accept-non-empty"}}, "toggle-all+accept-non-empty"},
		{Binding{Trigger: "ctrl-y", Actions: []string{"accept", "yank"}}, ""},
		{Binding{Trigger: "ctrl-d", Actions: []string{"put"}}, ""},
		{Binding{Trigger: "tab", Actions: []string{"toggle", "accept"}}, ""},
		{Binding{Trigger: "ctrl-x", Actions: []string{"execute"}}, "execute"},
		{Binding{Trigger: "ctrl-h", Actions: []string{"transform-header"}}, "transform-header"},
		{Binding{Trigger: "ctrl-z", Actions: []string{"some-future-action"}}, "some-future-action"},
	}
	for _, c := range cases {
		if got := HiddenAction(c.b); got != c.want {
			t.Errorf("HiddenAction(%+v) = %q, want %q", c.b, got, c.want)
		}
	}
}

func TestClosesThenBinds(t *testing.T) {
	if !ClosesThenBinds("^", "x^,ctrl-a:accept") {
		t.Error("a closing caret followed by a comma is a second binding")
	}
	if !ClosesThenBinds("(", "x)+accept") {
		t.Error("a closing paren followed by + is a further action")
	}
	if ClosesThenBinds("^", ";; esac^") {
		t.Error("a closing caret at the end adds nothing")
	}
	if ClosesThenBinds("^", "no closer here") {
		t.Error("no closer adds nothing")
	}
	if ClosesThenBinds("", "x^,y") {
		t.Error("no open argument adds nothing")
	}
}

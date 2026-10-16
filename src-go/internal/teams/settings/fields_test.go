package settings

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
)

func TestShownValues(t *testing.T) {
	cases := []struct {
		s    declare.Setting
		want string
	}{
		{declare.Setting{Key: "description", Value: json.RawMessage(`"Builds it"`)}, "Builds it"},
		{declare.Setting{Key: "humans", Value: json.RawMessage(`["a", "b"]`)}, "a, b"},
		{declare.Setting{Key: "allow_from", Value: json.RawMessage(`[]`)}, "(none)"},
		{declare.Setting{Key: "org"}, "(none)"},
		{declare.Setting{Key: "port", Value: json.RawMessage(`6167`)}, "6167"},
		{
			declare.Setting{Key: "repos", Value: json.RawMessage(`[{"repo": "a/b", "branches": ["main", "dev"]}, {"repo": "c/d"}]`)},
			"a/b (branches main, dev), c/d",
		},
		{declare.Setting{Key: "humans", State: declare.SettingVaulted}, "(vault-encrypted)"},
		{declare.Setting{Key: "humans", State: declare.SettingInvalid}, "(not readable)"},
	}
	for _, c := range cases {
		if got := shown(c.s); got != c.want {
			t.Errorf("%+v: %q, want %q", c.s, got, c.want)
		}
	}
}

func TestEditTextIsWhatTheInputStartsFrom(t *testing.T) {
	if got := editText(json.RawMessage(`[{"repo": "a/b", "branches": ["main"]}, {"repo": "c/d"}]`)); got != "a/b, c/d" {
		t.Fatalf("repos %q", got)
	}
	if got := editText(nil); got != "" {
		t.Fatalf("none %q", got)
	}
	if got := editText(json.RawMessage(`"x"`)); got != "x" {
		t.Fatalf("string %q", got)
	}
}

func TestInputJSON(t *testing.T) {
	people := fieldOf("humans")
	if got := inputJSON(people, " owner ,, second , "); got != `["owner","second"]` {
		t.Fatalf("list %q", got)
	}
	if got := inputJSON(people, ""); got != `[]` {
		t.Fatalf("empty list %q", got)
	}
	if got := inputJSON(fieldOf("description"), `Says "hi"`); got != `"Says \"hi\""` {
		t.Fatalf("string %q", got)
	}
}

// Every key declare.py settings prints has a field, editable exactly when set changes it.
func TestEveryKeyHasAFieldAndOnlySetsKeysAreEditable(t *testing.T) {
	keys := []string{
		"team", "description", "humans", "org", "repos", "exclude", "path_prefixes", "allow_from",
		"port", "listen", "server_name", "forge_api", "removed_humans",
	}
	editable := []string{"description", "humans", "org", "repos", "exclude", "path_prefixes", "allow_from"}
	if len(fields) != len(keys) {
		t.Fatalf("%d fields for %d keys", len(fields), len(keys))
	}
	for i, f := range fields {
		if f.key != keys[i] {
			t.Fatalf("field %d is %s, want %s", i, f.key, keys[i])
		}
		if (f.fixed == "") != slices.Contains(editable, f.key) {
			t.Errorf("%s: fixed %q", f.key, f.fixed)
		}
		if f.fixed == "" && (f.about == "" || f.applies == "") {
			t.Errorf("%s says neither what it is nor what applying it does", f.key)
		}
	}
}

package proto

import (
	"strings"
	"testing"
)

// A foreign producer's object: the keys read are parsed by Parse's rules, every other member is
// skipped whatever its form, so a float, a large integer, a repeated key, a lone surrogate or a
// deep value nobody reads refuses nothing (fedora-desktop#394).
func TestForeignObjectSkipsWhatItDoesNotRead(t *testing.T) {
	data := `{"cost":1.5,"big":9007199254740993,"neg":-1e400,"nested":{"a":[2.5e300,{"b":null}],"z":-0.0},` +
		`"dup":1,"dup":2,"lone":"\ud800","deep":` + strings.Repeat("[", 200) + strings.Repeat("]", 200) + `,` +
		`"id":"abc","flag":false}`
	got, err := ForeignObject([]byte(data), "id", "flag", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["id"].Kind() != KindString || got["id"].Str() != "abc" || got["flag"].Kind() != KindBool || got["flag"].Bool() {
		t.Fatalf("%v", got)
	}
}

// What is refused: not one JSON object in valid UTF-8, a key read twice, and a key read whose
// value breaks Parse's rules.
func TestForeignObjectRefuses(t *testing.T) {
	for name, data := range map[string]string{
		"array":             `[]`,
		"string":            `"x"`,
		"empty":             ``,
		"two objects":       `{}{}`,
		"junk after":        `{} junk`,
		"cut short":         `{"id":"a"`,
		"bad member":        `{"id" "a"}`,
		"invalid utf-8":     "{\"x\":\"\xff\",\"id\":\"a\"}",
		"repeated read key": `{"id":"a","id":"b"}`,
		"read float":        `{"id":1.5}`,
		"read lone":         `{"id":"\ud800"}`,
		"read big integer":  `{"id":9007199254740993}`,
	} {
		if got, err := ForeignObject([]byte(data), "id"); err == nil {
			t.Errorf("%s: %v, want refused", name, got)
		}
	}
}

// An object with none of the keys read, and whitespace around it, is an empty reading.
func TestForeignObjectWithNothingRead(t *testing.T) {
	got, err := ForeignObject([]byte(" \n{\"other\":[1.5]}\n"), "id")
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

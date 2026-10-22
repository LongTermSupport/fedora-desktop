package proto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseAcceptsStrictJSON(t *testing.T) {
	cases := map[string]string{
		"object":        `{"a":1,"b":"x","c":true,"d":false,"e":null,"f":[1,2],"g":{}}`,
		"whitespace":    " \t\r\n{ \"a\" : [ 1 , -2 ] }\n",
		"max int":       `9007199254740991`,
		"min int":       `-9007199254740991`,
		"minus zero":    `-0`,
		"pair":          `"😀"`,
		"escapes":       `"\" \\ \/ \b \f \n \r \t \u0000 é"`,
		"raw non-ascii": `"é😀"`,
		"empty":         `""`,
		"nested":        `[[[]]]`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err != nil {
				t.Fatalf("Parse(%q) = %v, want accepted", input, err)
			}
		})
	}
}

func TestParseRefusesWhatStrictJSONForbids(t *testing.T) {
	cases := map[string]string{
		"lone high surrogate":     `"\ud800"`,
		"lone low surrogate":      `"\udc00"`,
		"high then non-low":       `"\ud800A"`,
		"high then text":          `"\ud800x"`,
		"reversed pair":           `"\ude00\ud83d"`,
		"repeated key":            `{"a":1,"a":2}`,
		"repeated nested key":     `{"x":{"a":1,"a":1}}`,
		"repeated key escaped":    `{"a":1,"a":2}`,
		"fraction":                `1.0`,
		"exponent":                `1e3`,
		"capital exponent":        `1E3`,
		"two to the 53":           `9007199254740992`,
		"minus two to the 53":     `-9007199254740992`,
		"huge":                    `123456789012345678901234567890`,
		"leading zero":            `01`,
		"plus":                    `+1`,
		"bare minus":              `-`,
		"invalid utf-8 in str":    "\"\xff\"",
		"invalid utf-8 outside":   "\xff",
		"overlong utf-8":          "\"\xc0\xaf\"",
		"encoded surrogate":       "\"\xed\xa0\x80\"",
		"raw control in string":   "\"a\nb\"",
		"bad escape":              `"\x"`,
		"short unicode escape":    `"\u12"`,
		"trailing value":          `{} {}`,
		"trailing comma array":    `[1,]`,
		"trailing comma object":   `{"a":1,}`,
		"missing colon":           `{"a" 1}`,
		"unquoted key":            `{a:1}`,
		"single quotes":           `'a'`,
		"empty input":             ``,
		"only space":              `   `,
		"unterminated string":     `"abc`,
		"unterminated array":      `[1`,
		"unterminated object":     `{"a":1`,
		"bad literal":             `tru`,
		"capital literal":         `True`,
		"nan":                     `NaN`,
		"bom":                     "\xef\xbb\xbf{}",
		"non-json whitespace":     "\v{}",
		"comment":                 `{} // x`,
		"key not string":          `{1:2}`,
		"lone high at end of key": `{"\ud800":1}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if v, err := Parse([]byte(input)); err == nil {
				t.Fatalf("Parse(%q) = %v, want refused", input, v.Kind())
			}
		})
	}
}

func TestParseRefusesDeepNesting(t *testing.T) {
	deep := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	if _, err := Parse([]byte(deep)); err == nil {
		t.Fatal("nesting past MaxDepth accepted")
	}
	ok := strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth)
	if _, err := Parse([]byte(ok)); err != nil {
		t.Fatalf("nesting at MaxDepth refused: %v", err)
	}
}

func TestParseErrorNamesNoInput(t *testing.T) {
	_, err := Parse([]byte(`{"secret-value":1.5}`))
	syntax, ok := errors.AsType[*SyntaxError](err)
	if !ok || syntax.Offset != len(`{"secret-value":`) {
		t.Fatalf("error %v is not a *SyntaxError at the number", err)
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error %q echoes the input", err)
	}
}

// member is v's value under key, failing the test when it is absent.
func member(t *testing.T, v Value, key string) Value {
	t.Helper()
	got, ok := v.Get(key)
	if !ok {
		t.Fatalf("no key %q", key)
	}
	return got
}

func TestParseValues(t *testing.T) {
	v, err := Parse([]byte(`{"s":"aé😀","i":-42,"b":true,"n":null,"a":["x",2],"o":{"k":"v"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind() != KindObject || len(v.Members()) != 6 {
		t.Fatalf("not an object of six members: %v", v.Kind())
	}
	checkScalars(t, v)
	if a := member(t, v, "a"); a.Kind() != KindArray || len(a.Items()) != 2 || a.Items()[1].Int() != 2 {
		t.Fatal("a is not [x, 2]")
	}
	if o := member(t, v, "o"); o.Kind() != KindObject || member(t, o, "k").Str() != "v" {
		t.Fatal("o is not {k: v}")
	}
	if _, ok := v.Get("missing"); ok {
		t.Fatal("a missing key was found")
	}
	if _, ok := Str("x").Get("k"); ok {
		t.Fatal("Get on a string found a key")
	}
	if Str("x").Members() != nil || Str("x").Items() != nil {
		t.Fatal("a string has members or items")
	}
}

func checkScalars(t *testing.T, v Value) {
	t.Helper()
	if s := member(t, v, "s"); s.Str() != "aé😀" || s.Kind() != KindString {
		t.Fatalf("s = %q", s.Str())
	}
	if i := member(t, v, "i"); i.Kind() != KindInt || i.Int() != -42 {
		t.Fatalf("i = %d", i.Int())
	}
	if b := member(t, v, "b"); b.Kind() != KindBool || !b.Bool() {
		t.Fatal("b is not true")
	}
	if member(t, v, "n").Kind() != KindNull {
		t.Fatal("n is not null")
	}
}

func TestCompactIsSizeOfCompactUTF8(t *testing.T) {
	cases := map[string]string{
		`{ "a" : [ 1, true, null, "é" ] }`: `{"a":[1,true,null,"é"]}`,
		`"é😀"`:                             `"é😀"`,
		`"\u0001\b\f\n\r\t\"\\\/"`:         `"\u0001\b\f\n\r\t\"\\/"`,
		`"\u007f<>&"`:                      "\"\x7f<>&\"",
		`"\` + `u2028"`:                    "\"" + string(rune(0x2028)) + "\"",
		`-0`:                               `0`,
		`{"b":1,"a":2}`:                    `{"b":1,"a":2}`,
	}
	for input, want := range cases {
		v, err := Parse([]byte(input))
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if got := string(v.Compact()); got != want {
			t.Errorf("Compact(%q) = %q, want %q", input, got, want)
		}
		if v.Size() != len(want) {
			t.Errorf("Size(%q) = %d, want %d", input, v.Size(), len(want))
		}
		back, err := Parse(v.Compact())
		if err != nil || string(back.Compact()) != want {
			t.Errorf("Compact(%q) does not parse back to itself: %v", input, err)
		}
	}
}

func TestBuilders(t *testing.T) {
	v := Obj(
		M("s", Str("x")), M("i", Int(3)), M("b", Bool(false)), M("n", Null()),
		M("a", Arr(Str("y"))), M("e", Arr()),
	)
	if got := string(v.Compact()); got != `{"s":"x","i":3,"b":false,"n":null,"a":["y"],"e":[]}` {
		t.Fatalf("builders gave %s", got)
	}
	same := v
	if !v.Equal(same) || v.Equal(Obj()) || Int(1).Equal(Str("1")) {
		t.Fatal("Equal is wrong")
	}
	if !Obj(M("a", Int(1)), M("b", Int(2))).Equal(Obj(M("b", Int(2)), M("a", Int(1)))) {
		t.Fatal("Equal depends on key order")
	}
}

func mustParse(t *testing.T, s string) Value {
	t.Helper()
	v, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func TestFieldsReadsTypedMembers(t *testing.T) {
	f := readFields(mustParse(t, `{"i":7,"s":"x","b":false,"o":{},"l":["a","b"]}`), "shape")
	if f.integer("i", true, 0, "i") != 7 || f.integer("absent", false, 3, "i") != 3 {
		t.Fatal("integer wrong")
	}
	if f.str("s", true, "s") != "x" || f.str("absent", false, "s") != "" {
		t.Fatal("str wrong")
	}
	if f.boolean("b", true, "b") || !f.boolean("absent", true, "b") {
		t.Fatal("boolean wrong")
	}
	if _, ok := f.object("o", true, "o"); !ok {
		t.Fatal("object missing")
	}
	if got := f.strs("l", true, "l"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("strs = %v", got)
	}
	if got := f.strs("absent", false, "l"); got != nil {
		t.Fatalf("absent strs = %v", got)
	}
	if !f.has("i") || f.has("absent") || f.rule != "" {
		t.Fatalf("has wrong or rule %q set", f.rule)
	}
}

func TestFieldsKeepsTheFirstBrokenRule(t *testing.T) {
	cases := map[string]func(f *fields){
		"i": func(f *fields) { f.integer("s", false, 0, "i") },
		"s": func(f *fields) { f.str("i", false, "s") },
		"b": func(f *fields) { f.boolean("i", false, "b") },
		"o": func(f *fields) { f.object("i", false, "o") },
		"l": func(f *fields) { f.strs("i", false, "l") },
		"e": func(f *fields) { f.strs("ln", false, "e") },
		"r": func(f *fields) { f.integer("absent", true, 0, "r") },
	}
	for want, read := range cases {
		f := readFields(mustParse(t, `{"i":1,"s":"x","ln":[1]}`), "shape")
		read(f)
		f.integer("absent", true, 0, "later")
		if f.rule != want {
			t.Errorf("rule = %q, want %q", f.rule, want)
		}
	}
	if f := readFields(Int(1), "shape"); f.rule != "shape" {
		t.Fatalf("a non-object gave rule %q", f.rule)
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{`{}`, `{"a":[1,"😀",null]}`, `"\ud800"`, `1.5`, `{"a":1,"a":1}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := Parse(data)
		if err != nil {
			return
		}
		if !json.Valid(data) || !utf8.Valid(data) {
			t.Fatal("accepted a document the standard library calls invalid")
		}
		back, err := Parse(v.Compact())
		if err != nil {
			t.Fatalf("compact form of an accepted value is refused: %v", err)
		}
		if !back.Equal(v) {
			t.Fatal("compact form does not round-trip")
		}
	})
}

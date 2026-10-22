package exits

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func team(t *testing.T) *paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return &id
}

// B 2.2.4's table, written out once more by hand: the data must say the same.
func TestTableIsBsTable(t *testing.T) {
	want := map[term.ExitCode]struct {
		codes []string
		class Class
	}{
		1:  {[]string{"internal"}, ClassTell},
		4:  {[]string{"timed-out"}, ClassWait},
		5:  {[]string{"partial"}, ClassRead},
		10: {[]string{"size", "schema", "secret", "target", "verb", "ref", "re", "body", "empty"}, ClassFix},
		11: {[]string{"role"}, ClassFix},
		13: {[]string{"forge", "provenance"}, ClassFix},
		14: {[]string{"rate", "server-rate", "report-rate"}, ClassWait},
		15: {[]string{"duplicate"}, ClassFix},
		20: {[]string{"unreachable"}, ClassWait},
		21: {[]string{"token"}, ClassTell},
		22: {[]string{"untrusted", "forbidden"}, ClassTell},
		23: {[]string{"not-found"}, ClassFix},
		64: {[]string{"usage", "ambiguous-team", "unknown-member", "ambiguous-member"}, ClassFix},
		69: {[]string{"no-session", "no-link", "link-failed"}, ClassTell},
		75: {[]string{"seat-held", "keeper-running"}, ClassLifecycle},
		78: {[]string{"bundle", "membership", "store", "forge-login", "runtime", "launch"}, ClassTell},
	}
	rows := Table()
	if len(rows) != len(want)+1 {
		t.Fatalf("%d rows, want %d", len(rows), len(want)+1)
	}
	if rows[0].Exit != term.ExitOK || rows[0].Codes != nil || rows[0].Class != ClassNone {
		t.Errorf("row 0 = %+v", rows[0])
	}
	for _, r := range rows[1:] {
		w, ok := want[r.Exit]
		if !ok {
			t.Errorf("exit %d is not B's", r.Exit)
			continue
		}
		if !slices.Equal(r.Codes, w.codes) || r.Class != w.class {
			t.Errorf("exit %d: %v %q, want %v %q", r.Exit, r.Codes, r.Class, w.codes, w.class)
		}
		if r.Meaning == "" || r.Next == "" {
			t.Errorf("exit %d has no meaning or next step", r.Exit)
		}
	}
}

func TestEveryCodeHasOneExit(t *testing.T) {
	seen := map[string]term.ExitCode{}
	for _, r := range Table() {
		for _, c := range r.Codes {
			if prev, dup := seen[c]; dup {
				t.Errorf("code %q is in exits %d and %d", c, prev, r.Exit)
			}
			seen[c] = r.Exit
			if got, ok := ExitOf(c); !ok || got != r.Exit {
				t.Errorf("ExitOf(%q) = %d %v", c, got, ok)
			}
		}
	}
	if _, ok := ExitOf("nope"); ok {
		t.Error("an unknown code has an exit")
	}
}

func TestTableIsACopy(t *testing.T) {
	rows := Table()
	rows[1].Codes[0] = "changed"
	if Table()[1].Codes[0] != CodeInternal {
		t.Error("the table was changed through its copy")
	}
}

func TestClassesNameEveryAgentExit(t *testing.T) {
	for _, r := range Table()[1:] {
		if r.Class == ClassNone {
			t.Errorf("exit %d has no class", r.Exit)
		}
	}
	if ClassFix.String() != "fix" || ClassTell.String() != "tell a human" || ClassNone.String() != "-" {
		t.Error("class names")
	}
}

func TestEveryClassSaysWhatToDo(t *testing.T) {
	for _, c := range append(slices.Clone(Classes), ClassLifecycle) {
		if c.Does() == "-" || c.Does() == "" {
			t.Errorf("class %s does nothing", c)
		}
	}
	if ClassNone.Does() != "-" {
		t.Error("none")
	}
}

func TestNewFillsTheTemplateWithCheckedNamesOnly(t *testing.T) {
	cases := []struct {
		name  string
		fill  Fill
		shown string
	}{
		{"team", Team(*team(t)), "alpha"},
		{"name", Name("$abc_DEF-123"), "$abc_DEF-123"},
		{"handle", Name("repo.2+site.podman"), "repo.2+site.podman"},
		{"path", Name("/run/user/1000/teamchat/run/0123456789abcdef/link.log"), "/run/user/1000/teamchat/run/0123456789abcdef/link.log"},
		{"name with a space", Name("a b"), "?"},
		{"name with a quote", Name(`a"b`), "?"},
		{"name with a newline", Name("a\nb"), "?"},
		{"empty name", Name(""), "?"},
		{"long name", Name(strings.Repeat("a", 256)), "?"},
		{"words", Words("seat set-aside"), "seat set-aside"},
		{"words with a capital", Words("Seat"), "?"},
		{"words with two spaces", Words("a  b"), "?"},
		{"description", Description("Release work for the desktop"), "Release work for the desktop"},
		{"description with a control", Description("bad\x07"), "?"},
		{"count", Count(12), "12"},
	}
	for _, c := range cases {
		e := New(CodeUsage, nil, "x {} y", c.fill)
		if e.Message != "x "+c.shown+" y" {
			t.Errorf("%s: %q", c.name, e.Message)
		}
		if e.Exit != ExitUsage || e.Code != CodeUsage || e.Team != nil || e.Error() != e.Message {
			t.Errorf("%s: %+v", c.name, e)
		}
	}
}

func TestFormatAndJoinComposeAList(t *testing.T) {
	list := Join("; ", Format("{} ({})", Name("alpha"), Description("Release work")), Format("{} ({})", Name("beta"), Description("x\ny")))
	e := New(CodeAmbiguousTeam, nil, "pass --team with one of: {}", list)
	if e.Message != "pass --team with one of: alpha (Release work); beta (?)" || e.Exit != ExitUsage {
		t.Errorf("%+v", e)
	}
	if Join(", ").text != "" {
		t.Error("an empty join")
	}
}

func TestNewPanicsOnAProgrammingError(t *testing.T) {
	for name, f := range map[string]func(){
		"unknown code":   func() { t.Log(New("nope", nil, "x")) },
		"too few fills":  func() { t.Log(New(CodeUsage, nil, "{} {}", Name("a"))) },
		"too many fills": func() { t.Log(New(CodeUsage, nil, "{}", Name("a"), Name("b"))) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			f()
		}()
	}
}

func TestWriteIsTheLastStderrLineKindFirst(t *testing.T) {
	var buf bytes.Buffer
	e := New(CodeRole, team(t), "only the coordinator of {} asks its humans", Team(*team(t)))
	code := Write(&buf, fmt.Errorf("wrapped: %w", e))
	if code != ExitRole {
		t.Errorf("exit %d", code)
	}
	want := `{"kind":"error","v":1,"exit":11,"code":"role","team":"alpha","message":"only the coordinator of alpha asks its humans"}` + "\n"
	if buf.String() != want {
		t.Errorf("got  %s\nwant %s", buf.String(), want)
	}
}

func TestWriteWithNoTeamIsNull(t *testing.T) {
	var buf bytes.Buffer
	Write(&buf, New(CodeUsage, nil, "a <b> & c"))
	want := `{"kind":"error","v":1,"exit":64,"code":"usage","team":null,"message":"a <b> & c"}` + "\n"
	if buf.String() != want {
		t.Errorf("got %s", buf.String())
	}
}

func TestWriteNilIsOKAndSilent(t *testing.T) {
	var buf bytes.Buffer
	if Write(&buf, nil) != term.ExitOK || buf.Len() != 0 {
		t.Error("nil error")
	}
}

func TestWriteOfAnyOtherErrorIsInternalWithADiagnosticFirst(t *testing.T) {
	var buf bytes.Buffer
	code := Write(&buf, errors.New("disk on fire\n{\"kind\":\"error\",\"exit\":0}"))
	if code != term.ExitFailure {
		t.Errorf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines: %q", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "teamchat: disk on fire ") {
		t.Errorf("diagnostic %q", lines[0])
	}
	got, err := Decode([]byte(lines[1]))
	if err != nil || got.Exit != term.ExitFailure || got.Code != CodeInternal {
		t.Errorf("last line %q: %+v %v", lines[1], got, err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestWriteKeepsTheExitWhenStderrFails(t *testing.T) {
	if Write(failWriter{}, New(CodeNotFound, nil, "x")) != ExitNotFound {
		t.Error("exit lost")
	}
}

func TestDecodeRoundTrips(t *testing.T) {
	for _, e := range []*Error{
		New(CodeSeatHeld, team(t), "seat {} is held", Name("alpha/1")),
		New(CodeBundle, nil, "bundle"),
	} {
		var buf bytes.Buffer
		Write(&buf, e)
		got, err := Decode(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
		if err != nil {
			t.Fatal(err)
		}
		if got.Exit != e.Exit || got.Code != e.Code || got.Message != e.Message || (got.Team == nil) != (e.Team == nil) {
			t.Errorf("got %+v want %+v", got, e)
		}
		if e.Team != nil && *got.Team != *e.Team {
			t.Errorf("team %v", got.Team)
		}
	}
}

func TestDecodeRefusesAnythingButTheErrorObject(t *testing.T) {
	ok := `{"kind":"error","v":1,"exit":75,"code":"seat-held","team":null,"message":"m"}`
	if _, err := Decode([]byte(ok)); err != nil {
		t.Fatal(err)
	}
	for name, line := range map[string]string{
		"not json":         `CHANGED	bundle`,
		"array":            `[]`,
		"extra key":        `{"kind":"error","v":1,"exit":75,"code":"seat-held","team":null,"message":"m","x":1}`,
		"missing key":      `{"kind":"error","v":1,"exit":75,"code":"seat-held","team":null}`,
		"duplicate key":    `{"kind":"error","v":1,"v":1,"exit":75,"code":"seat-held","team":null,"message":"m"}`,
		"other kind":       `{"kind":"human","v":1,"exit":75,"code":"seat-held","team":null,"message":"m"}`,
		"version 2":        `{"kind":"error","v":2,"exit":75,"code":"seat-held","team":null,"message":"m"}`,
		"version string":   `{"kind":"error","v":"1","exit":75,"code":"seat-held","team":null,"message":"m"}`,
		"unknown code":     `{"kind":"error","v":1,"exit":75,"code":"bogus","team":null,"message":"m"}`,
		"code of another":  `{"kind":"error","v":1,"exit":75,"code":"bundle","team":null,"message":"m"}`,
		"exit 0":           `{"kind":"error","v":1,"exit":0,"code":"seat-held","team":null,"message":"m"}`,
		"exit a float":     `{"kind":"error","v":1,"exit":75.0,"code":"seat-held","team":null,"message":"m"}`,
		"exit a string":    `{"kind":"error","v":1,"exit":"75","code":"seat-held","team":null,"message":"m"}`,
		"code a number":    `{"kind":"error","v":1,"exit":75,"code":1,"team":null,"message":"m"}`,
		"bad team":         `{"kind":"error","v":1,"exit":75,"code":"seat-held","team":"Bad Team","message":"m"}`,
		"team a number":    `{"kind":"error","v":1,"exit":75,"code":"seat-held","team":1,"message":"m"}`,
		"message a number": `{"kind":"error","v":1,"exit":75,"code":"seat-held","team":null,"message":1}`,
		"kind a number":    `{"kind":1,"v":1,"exit":75,"code":"seat-held","team":null,"message":"m"}`,
	} {
		if _, err := Decode([]byte(line)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

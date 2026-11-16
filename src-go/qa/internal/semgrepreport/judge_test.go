package semgrepreport

import (
	"strings"
	"testing"
)

func decode(t *testing.T, doc string) Report {
	t.Helper()
	rep, err := Decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestDecodeRefusesWhatIsNotAReport(t *testing.T) {
	for _, doc := range []string{"", "garbage", `{"results":[]}`} {
		if _, err := Decode(doc); err == nil {
			t.Errorf("%q decoded", doc)
		}
	}
}

func TestJudgeAcceptsAReportThatReadEveryFile(t *testing.T) {
	rep := decode(t, `{"results":[],"errors":[],"paths":{"scanned":["./a.py","b.py"]}}`)
	if err := rep.Judge("Python", []string{"a.py", "b.py"}); err != nil {
		t.Fatal(err)
	}
	if rep.ScannedCount() != 2 {
		t.Fatalf("count %d", rep.ScannedCount())
	}
}

func TestJudgeRefusesErrorsAndUnreadFiles(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"errors":  {`{"errors":[{"path":"a.py","message":"Syntax error"}],"paths":{"scanned":["a.py"]}}`, "could not parse some Python files, so no rule ran on them:\n    a.py: Syntax error"},
		"no path": {`{"errors":[{"message":"boom"}],"paths":{"scanned":["a.py"]}}`, "    ?: boom"},
		"lost":    {`{"paths":{"scanned":["b.py"]}}`, "did not scan Python files it was handed:\n    a.py\n  A pass reported over files the analyser never opened is not a pass"},
	}
	for name, c := range cases {
		err := decode(t, c.doc).Judge("Python", []string{"a.py", "b.py"})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

func TestScannedCountFoldsDuplicates(t *testing.T) {
	rep := decode(t, `{"paths":{"scanned":["a.py","./a.py","b.py"]}}`)
	if rep.ScannedCount() != 2 {
		t.Fatalf("count %d", rep.ScannedCount())
	}
}

// partialParsing is semgrep 1.177's report on a shell fixture it read as Python: the error's type
// is an array, the kind followed by the locations it gave up on.
const partialParsing = `{"results":[],"errors":[{"code":3,"level":"warn","type":["PartialParsing",[{"path":"shell.fixture","start":{"line":2,"col":6,"offset":0},"end":{"line":2,"col":9,"offset":3}}]],"message":"Syntax error at line shell.fixture:2:\n ` + "`euo`" + ` was unexpected","path":"shell.fixture","spans":[{"file":"shell.fixture","start":{"line":2,"col":6,"offset":0},"end":{"line":2,"col":9,"offset":3}}]}],"paths":{"scanned":["shell.fixture"]}}`

func TestAPartialParsingReportIsReadAndItsErrorNamesTheFileAndLine(t *testing.T) {
	err := decode(t, partialParsing).Judge("fixture", []string{"shell.fixture"})
	want := "    shell.fixture:2: Syntax error at line shell.fixture:2: `euo` was unexpected (PartialParsing)"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("got %v\nwant %q", err, want)
	}
}

// A field shaped otherwise than expected must not cost the whole report: the error still counts,
// and what its type said is shown as semgrep wrote it.
func TestAnErrorTypeOfAnyShapeDecodes(t *testing.T) {
	cases := map[string]string{
		`"Timeout"`:              "Timeout",
		`["PartialParsing", []]`: "PartialParsing",
		`["Other"]`:              "Other",
		`[]`:                     "[]",
		`7`:                      "7",
		`{"kind": "x"}`:          `{"kind": "x"}`,
		`[["nested"], "second"]`: `[["nested"], "second"]`,
		`null`:                   "",
	}
	for raw, want := range cases {
		rep := decode(t, `{"errors":[{"type":`+raw+`,"path":"a.py","message":"m"}],"paths":{"scanned":["a.py"]}}`)
		if got := rep.Errors[0].Type.Kind(); got != want {
			t.Errorf("type %s: kind %q, want %q", raw, got, want)
		}
		if err := rep.Judge("Python", []string{"a.py"}); err == nil {
			t.Errorf("type %s: the error was not counted", raw)
		}
	}
}

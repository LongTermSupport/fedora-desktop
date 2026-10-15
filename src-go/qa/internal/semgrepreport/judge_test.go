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

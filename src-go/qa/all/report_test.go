package all

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	bashStage   = `{"type":"bash","status":"fail","summary":{"total":3,"passed":2,"failed":1},"results":[{"file":"a<b>.sh","status":"fail"}],"failures":[{"file":"a<b>.sh","errors":["x & y"]}]}`
	pythonStage = `{"type":"python","status":"pass","summary":{"total":2,"passed":2,"failed":0},"failures":[]}`
)

// wantMerged is what `jq -s` produced over the same two documents (the bash's merge), byte for
// byte: key order, two-space indent, no HTML escaping of a stage's own text.
const wantMerged = `{
  "status": "fail",
  "summary": {
    "total": 5,
    "passed": 4,
    "failed": 1
  },
  "failures": [
    {
      "file": "a<b>.sh",
      "errors": [
        "x & y"
      ]
    }
  ],
  "checks": {
    "bash": {
      "type": "bash",
      "status": "fail",
      "summary": {
        "total": 3,
        "passed": 2,
        "failed": 1
      },
      "results": [
        {
          "file": "a<b>.sh",
          "status": "fail"
        }
      ],
      "failures": [
        {
          "file": "a<b>.sh",
          "errors": [
            "x & y"
          ]
        }
      ]
    },
    "python": {
      "type": "python",
      "status": "pass",
      "summary": {
        "total": 2,
        "passed": 2,
        "failed": 0
      },
      "failures": []
    }
  }
}
`

func mustParse(t *testing.T, body string) *stageReport {
	t.Helper()
	r, err := parseStageReport([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestMergeReportsMatchesWhatJqProduced(t *testing.T) {
	merged, totals, err := mergeReports("fail", []namedReport{
		{key: "bash", report: mustParse(t, bashStage)},
		{key: "python", report: mustParse(t, pythonStage)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != wantMerged {
		t.Fatalf("got:\n%s", merged)
	}
	if totals.Total != 5 || totals.Passed != 4 || totals.Failed != 1 {
		t.Fatalf("totals %+v", totals)
	}
}

// Bug B4: an unusable report is that stage's null, never another stage's data.
func TestMergeReportsKeepsEachStageInItsOwnSlot(t *testing.T) {
	merged, totals, err := mergeReports("fail", []namedReport{
		{key: "bash", report: nil},
		{key: "ansible_syntax", report: mustParse(t, pythonStage)},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(merged)
	if !strings.Contains(text, `"bash": null`) || !strings.Contains(text, `"ansible_syntax": {`) || totals.Total != 2 {
		t.Fatalf("got:\n%s", text)
	}
	if !strings.Contains(text, `"failures": []`) {
		t.Fatalf("no failures must be an empty list, not null:\n%s", text)
	}
}

func TestParseStageReportRefusesWhatIsNotAReport(t *testing.T) {
	cases := []struct{ name, body, mentions string }{
		{"empty", "", "empty"},
		{"not json", "not json", "JSON"},
		{"an array", "[]", "JSON"},
		{"no summary", `{"failures":[]}`, "summary"},
		{"a count missing", `{"summary":{"total":1,"passed":1},"failures":[]}`, "failed"},
		{"a negative count", `{"summary":{"total":-1,"passed":0,"failed":0},"failures":[]}`, "total"},
		{"no failures list", `{"summary":{"total":1,"passed":1,"failed":0}}`, "failures"},
		{"failures not a list", `{"summary":{"total":1,"passed":1,"failed":0},"failures":{}}`, "failures"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseStageReport([]byte(c.body))
			if err == nil || !strings.Contains(err.Error(), c.mentions) {
				t.Fatalf("got %v; want a refusal mentioning %q", err, c.mentions)
			}
		})
	}
}

func TestWriteReportIsAtomicAndReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeReport(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "new\n" {
		t.Fatalf("got %q, %v", body, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, %v: another user and the CI uploader must be able to read it", info.Mode(), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("a temporary file was left behind: %v, %v", entries, err)
	}
}

func TestWriteReportFailsLoudlyWhenTheDirectoryIsGone(t *testing.T) {
	if err := writeReport(filepath.Join(t.TempDir(), "no", "such", "dir", "r.json"), []byte("x")); err == nil {
		t.Fatal("an unwritable report must be an error")
	}
}

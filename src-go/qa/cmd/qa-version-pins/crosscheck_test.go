package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/versionpins"
)

// The manifest's shape is stated twice: in Go for this gate and in helpers/version_pins/manifest.py
// for the host-side consumers (check_pins.py, check-pinned-versions.bash). This test pins the two to
// the same verdicts, so a rule added to one and not the other fails here rather than at login.

const pythonVerdict = "import sys\n" +
	"from helpers.version_pins import manifest\n" +
	"sys.exit(manifest.main(open(sys.argv[1])))\n"

func rowWith(edit func(string) string) string { return "version_pins:\n" + edit(row) }

func untouched(s string) string { return s }

// asJSON is what the host consumers hand the Python validator: the manifest decoded and re-encoded.
func asJSON(t *testing.T, text string) (string, error) {
	t.Helper()
	var doc any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded), nil
}

// pythonAccepts runs the manifest through the Python validator.
func pythonAccepts(t *testing.T, root, jsonText string) (bool, string) {
	t.Helper()
	dir := t.TempDir()
	scratch, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := scratch.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if err = scratch.WriteFile("manifest.json", []byte(jsonText), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "manifest.json")
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{"python3", "-c", pythonVerdict, file},
		Dir:  root,
	})
	if err != nil {
		t.Fatalf("python3 manifest validator: %v", err)
	}
	return res.Code == 0 && strings.HasPrefix(res.Stdout, "VERSION-PINS-OK"), res.Stdout + res.Stderr
}

// goAccepts runs the manifest through this gate over a tree whose playbook resolves.
func goAccepts(t *testing.T, manifest string) (bool, string) {
	t.Helper()
	code, stdout, stderr := realRun(t, t.TempDir(), map[string]string{
		"vars/version-pins.yml": manifest,
		"playbooks/a.yml":       "a_version: \"1\"\n",
	})
	return code == versionpins.ExitOK, stdout + stderr
}

func TestGoAndPythonAgreeOnTheShapeOfTheManifest(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	replace := func(old, repl string) func(string) string {
		return func(s string) string { return strings.Replace(s, old, repl, 1) }
	}
	cases := []struct {
		name string
		text string
		// goOnly names the rule only the Go gate enforces; the Python must accept the manifest.
		goOnly string
		accept bool
	}{
		{name: "good", text: rowWith(untouched), accept: true},
		{name: "unknown row key", text: rowWith(replace("github:", "githug:"))},
		{name: "playbook empty", text: rowWith(replace("playbooks/a.yml", `""`))},
		{name: "var not a string", text: rowWith(replace("a_version", "12"))},
		{name: "delimiter in a field", text: rowWith(untouched) + "    note: \"a|b\"\n"},
		{name: "github not owner/repo", text: rowWith(replace("o/r", "justname"))},
		{name: "no github and no note", text: rowWith(replace("    github: o/r\n", ""))},
		{name: "pair declared twice", text: rowWith(untouched) + row},
		{name: "installed missing", text: rowWith(replace("    installed:\n      kind: command\n      name: a\n", ""))},
		{name: "installed a scalar", text: rowWith(replace("installed:\n      kind: command\n      name: a\n", "installed: command\n"))},
		{name: "installed kind unknown", text: rowWith(replace("command", "apt"))},
		{name: "untracked without why", text: rowWith(replace("kind: command\n      name: a\n", "kind: untracked\n"))},
		{name: "tracked without name", text: rowWith(replace("      name: a\n", ""))},
		{name: "unknown installed key", text: rowWith(untouched) + "      nam: b\n"},
		{name: "row not a mapping", text: "version_pins:\n  - nope\n"},
		{name: "pins not a list", text: "version_pins:\n  a: 1\n"},
		{name: "no pins", text: "version_pins: []\n"},
		{name: "top level not a mapping", text: "- a\n"},
		// Go-only rules (SPEC-qa-version-pins B9 and V16): the Python must accept these, so a rule
		// later added to the Python is noticed here too.
		{name: "other top-level key", text: rowWith(untouched) + "extra: 1\n", goOnly: "B9"},
		{
			name: "no tracked pin", goOnly: "V16",
			text: rowWith(replace("kind: command\n      name: a\n", "kind: untracked\n      why: not established\n")),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := asJSON(t, tc.text)
			if err != nil {
				t.Fatalf("the case must decode: %v", err)
			}
			pyOK, pyOut := pythonAccepts(t, root, encoded)
			goOK, goOut := goAccepts(t, tc.text)
			if tc.goOnly != "" {
				if !pyOK || goOK {
					t.Fatalf("Go-only rule %s: python accepts %v (want true), go accepts %v (want false)\n%s\n%s", tc.goOnly, pyOK, goOK, pyOut, goOut)
				}
				return
			}
			if pyOK != tc.accept || goOK != tc.accept {
				t.Fatalf("python accepts %v, go accepts %v, want both %v\n%s\n%s", pyOK, goOK, tc.accept, pyOut, goOut)
			}
		})
	}
}

// A repeated key is Go-only by construction: the YAML library will not decode it into JSON, so the
// Python is never handed it. The gate still rejects it.
func TestARepeatedKeyIsRejectedByGoAndCannotReachPython(t *testing.T) {
	text := rowWith(untouched) + "    var: other\n"
	if _, err := asJSON(t, text); err == nil {
		t.Fatal("the YAML library decoded a repeated key; the case no longer shows it cannot reach the Python")
	}
	if ok, out := goAccepts(t, text); ok {
		t.Errorf("the gate accepted a repeated key:\n%s", out)
	}
}

// The live manifest is accepted by both definitions.
func TestTheLiveManifestIsAcceptedByThePython(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	text, err := fs.ReadFile(os.DirFS(root), "vars/version-pins.yml")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := asJSON(t, string(text))
	if err != nil {
		t.Fatal(err)
	}
	if ok, out := pythonAccepts(t, root, encoded); !ok {
		t.Fatalf("python rejects the live manifest:\n%s", out)
	}
}

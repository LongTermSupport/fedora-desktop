// Package lintconfig tests the golangci-lint configuration of src-go (.golangci.yml, which the qa
// module links to) by running the real golangci-lint over fixtures with it.
package lintconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// bareConfig runs govet's fieldalignment alone with no exclusion: the control that shows the
// fixture draws both diagnostics, so their treatment under the project configuration is the
// configuration's doing.
const bareConfig = `version: "2"
linters:
  default: none
  enable: [govet]
  settings:
    govet:
      enable: [fieldalignment]
`

const (
	sizeWaste       = "fieldalignment: Wasteful has size 24 but the optimal size is 16"
	pointerOrdering = "fieldalignment: PointerLate has 16 leading bytes of pointer data but optimal value is 8"
)

// issue is the part of one golangci-lint JSON issue the test reads. golangci-lint's keys are
// FromLinter and Text; encoding/json matches a key to its tag without regard to case.
type issue struct {
	FromLinter string `json:"fromLinter"`
	Text       string `json:"text"`
}

// fixtureModule copies the fieldalignment fixture into a module of its own and returns its
// directory.
func fixtureModule(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fixture")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "fieldalignment"))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// lint runs golangci-lint's govet over the module in dir with the configuration at config and
// returns the texts of the fieldalignment issues it reported.
func lint(t *testing.T, dir, config string) []string {
	t.Helper()
	res, err := proc.OS{}.Run(context.Background(), proc.Cmd{
		Argv: []string{
			"golangci-lint", "run", "--allow-parallel-runners", "--config", config,
			"--enable-only", "govet", "--output.json.path", "stdout", "./...",
		},
		Dir:  dir,
		Drop: func(key string) bool { return key == "GOFLAGS" || key == "GOWORK" },
		Env:  []string{"GOWORK=off"},
	})
	if err != nil {
		t.Fatalf("golangci-lint could not run (play-python.yml installs it; go-qa puts its pinned copy on PATH): %v", err)
	}
	// 0 is no issue, 1 is issues found; anything else is golangci-lint failing.
	if res.Code != 0 && res.Code != 1 {
		t.Fatalf("golangci-lint exit %d\nstdout:\n%s\nstderr:\n%s", res.Code, res.Stdout, res.Stderr)
	}
	var report struct {
		Issues []issue `json:"issues"`
	}
	if err := json.NewDecoder(strings.NewReader(res.Stdout)).Decode(&report); err != nil {
		t.Fatalf("golangci-lint's JSON report: %v\nstdout:\n%s\nstderr:\n%s", err, res.Stdout, res.Stderr)
	}
	var texts []string
	for _, is := range report.Issues {
		if is.FromLinter == "govet" && strings.HasPrefix(is.Text, "fieldalignment: ") {
			texts = append(texts, is.Text)
		}
	}
	return texts
}

func hasPrefix(texts []string, prefix string) bool {
	for _, text := range texts {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// TestFieldalignmentControl shows the fixture draws both diagnostics when nothing excludes either.
func TestFieldalignmentControl(t *testing.T) {
	dir := fixtureModule(t)
	config := filepath.Join(t.TempDir(), "golangci.yml")
	if err := os.WriteFile(config, []byte(bareConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	texts := lint(t, dir, config)
	for _, want := range []string{sizeWaste, pointerOrdering} {
		if !hasPrefix(texts, want) {
			t.Errorf("the bare fieldalignment run did not report %q; it reported %q", want, texts)
		}
	}
}

// TestFieldalignmentProjectConfig holds the owner's choice: fieldalignment is off, both halves.
// govet's enable-all would otherwise turn it back on, so the test fails if the disable goes.
func TestFieldalignmentProjectConfig(t *testing.T) {
	config, err := filepath.Abs(filepath.Join("..", "..", ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if texts := lint(t, fixtureModule(t), config); len(texts) != 0 {
		t.Errorf(".golangci.yml runs fieldalignment, which is off by the owner's decision: it reported %q", texts)
	}
}

package toolchain

import (
	"fmt"
	"go/version"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"
)

// go-minimum-above-provisioned-go (CLAUDE/QA.md): every Go build the IaC or a script runs on the
// host uses GOTOOLCHAIN=local, so it can never fetch a newer Go. A go.mod go directive, or a
// playbook's *_go_min record of a built module's go directive, newer than the golang_min_version
// play-golang.yml guarantees fails the build on every host provisioned to that Go (issue #249).
// A toolchain line is not judged: under GOTOOLCHAIN=local it is never enforced.

const (
	provisionedGoRule = "go-minimum-above-provisioned-go"
	golangVarsFile    = "vars/golang.yml"
)

// goMinVar is a playbook variable that records the go directive of a module the play builds.
var goMinVar = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+_go_min):\s*["']?([^"'\s#]+)["']?`)

// notBuiltOnTheHost are trees no host build reads: git's own, vendored modules (their go lines
// are bounded by the vendoring module's), test fixtures, and trees git does not publish.
var notBuiltOnTheHost = []string{".git", "vendor", "testdata", "untracked", "node_modules", ".claude"}

// goMinimumsAboveProvisioned is one finding per go minimum newer than the provisioned Go.
func goMinimumsAboveProvisioned(fsys fs.FS) ([]string, error) {
	provisioned, err := provisionedGo(fsys)
	if err != nil {
		return nil, err
	}
	j := &goMinimumJudge{provisioned: provisioned}
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && slices.Contains(notBuiltOnTheHost, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		return j.file(fsys, p)
	})
	return j.findings, err
}

// goMinimumJudge collects each go minimum above the provisioned Go.
type goMinimumJudge struct {
	provisioned string
	findings    []string
}

func (j *goMinimumJudge) file(fsys fs.FS, p string) error {
	isMod := path.Base(p) == "go.mod"
	if !isMod && !strings.HasSuffix(p, ".yml") && !strings.HasSuffix(p, ".yaml") {
		return nil
	}
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return err
	}
	if !isMod {
		for _, m := range goMinVar.FindAllStringSubmatch(string(data), -1) {
			j.judge(p, m[1], m[2])
		}
		return nil
	}
	mod, err := modfile.ParseLax(p, data, nil)
	if err != nil {
		return fmt.Errorf("cannot parse %s: %w", p, err)
	}
	if mod.Go != nil {
		j.judge(p, "the go directive", mod.Go.Version)
	}
	return nil
}

func (j *goMinimumJudge) judge(file, what, want string) {
	switch {
	case !version.IsValid("go" + want):
		j.findings = append(j.findings, fmt.Sprintf("%s: %s: %s names %q, which is not a Go version",
			provisionedGoRule, file, what, want))
	case version.Compare("go"+want, "go"+j.provisioned) > 0:
		j.findings = append(j.findings, fmt.Sprintf("%s: %s: %s demands go %s, but %s guarantees go %s (golang_min_version)",
			provisionedGoRule, file, what, want, golangVarsFile, j.provisioned))
	}
}

// provisionedGo is golang_min_version: play-golang.yml fails any host whose Go is older.
func provisionedGo(fsys fs.FS) (string, error) {
	data, err := fs.ReadFile(fsys, golangVarsFile)
	if err != nil {
		return "", fmt.Errorf("cannot read %s, which holds the provisioned Go: %w", golangVarsFile, err)
	}
	var vars map[string]string
	if err := yaml.Unmarshal(data, &vars); err != nil {
		return "", fmt.Errorf("cannot parse %s: %w", golangVarsFile, err)
	}
	minimum := vars["golang_min_version"]
	if !version.IsValid("go" + minimum) {
		return "", fmt.Errorf("%s: golang_min_version %q is not a Go version", golangVarsFile, minimum)
	}
	return minimum, nil
}

func TestNoGoMinimumAboveTheProvisionedGo(t *testing.T) {
	findings, err := goMinimumsAboveProvisioned(os.DirFS(filepath.Join("..", "..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f + "; see CLAUDE/QA.md#" + provisionedGoRule)
	}
}

func provisionedGoWorld(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{golangVarsFile: {Data: []byte("---\ngolang_min_version: \"1.26.10\"\n")}}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

func TestProvisionedGoRuleFiresOnAGoDirectiveAboveTheProvisionedGo(t *testing.T) {
	findings, err := goMinimumsAboveProvisioned(provisionedGoWorld(map[string]string{
		"src-go/qa/go.mod": "module m\n\ngo 1.26.11\n\ntoolchain go1.26.11\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "src-go/qa/go.mod: the go directive demands go 1.26.11") {
		t.Fatalf("findings = %q", findings)
	}
}

func TestProvisionedGoRuleFiresOnAPlaybookGoMinAboveTheProvisionedGo(t *testing.T) {
	findings, err := goMinimumsAboveProvisioned(provisionedGoWorld(map[string]string{
		"playbooks/imports/play-x.yml": "- hosts: desktop\n  vars:\n    x_go_min: \"1.27.0\"\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "x_go_min demands go 1.27.0") {
		t.Fatalf("findings = %q", findings)
	}
}

// A toolchain line ahead of the provisioned Go is allowed (GOTOOLCHAIN=local never enforces it),
// so are minimums at or below it, and trees no host build reads.
func TestProvisionedGoRuleAllowsWhatAProvisionedHostBuilds(t *testing.T) {
	findings, err := goMinimumsAboveProvisioned(provisionedGoWorld(map[string]string{
		"src-go/go.mod":            "module m\n\ngo 1.26.0\n\ntoolchain go1.26.11\n",
		"src-go/qa/go.mod":         "module m\n\ngo 1.26.10\n",
		"playbooks/play-y.yml":     "y_go_min: \"1.26.9\"\n",
		"src-go/vendor/x/go.mod":   "module x\n\ngo 1.99\n",
		"src-go/a/testdata/go.mod": "module t\n\ngo 1.99\n",
		"untracked/go.mod":         "module u\n\ngo 1.99\n",
		".claude/w/go.mod":         "module w\n\ngo 1.99\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %q", findings)
	}
}

func TestProvisionedGoRuleRefusesAnUnreadableProvisionedGo(t *testing.T) {
	fsys := fstest.MapFS{golangVarsFile: {Data: []byte("golang_min_version: \"latest\"\n")}}
	if _, err := goMinimumsAboveProvisioned(fsys); err == nil {
		t.Fatal("an unreadable golang_min_version was accepted")
	}
}

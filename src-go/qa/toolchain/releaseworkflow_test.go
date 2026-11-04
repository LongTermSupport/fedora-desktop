package toolchain

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"
)

// The release workflow (Plan 00153 Task 5.3) signs and pushes what servers trust, so what it
// promises is held here, not left to review: every action pinned by commit SHA, the least
// permissions per job, no pull_request_target, no workflow expression inside a shell script
// (a commit message or a title would be code), and the release key's secret only in a job of
// the protected environment.

const (
	releaseWorkflow = ".github/workflows/release.yml"
	actionsDir      = ".github/actions/*/action.yml"
)

// pinnedAction is owner/repo[/path]@<full commit sha>.
var pinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)

type workflowStep struct {
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

type workflowJob struct {
	Permissions map[string]string `yaml:"permissions"`
	Environment any               `yaml:"environment"`
	Steps       []workflowStep    `yaml:"steps"`
}

type workflowDoc struct {
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]workflowJob
	Runs        struct {
		Steps []workflowStep `yaml:"steps"`
	} `yaml:"runs"`
}

// releaseWorkflowFindings is every promise the release workflow and its local actions break.
func releaseWorkflowFindings(root fs.FS) ([]string, error) {
	var findings []string
	data, err := fs.ReadFile(root, releaseWorkflow)
	if err != nil {
		return nil, err
	}
	var wf workflowDoc
	if err = yaml.Unmarshal(data, &wf); err != nil {
		return nil, err
	}
	if _, bad := wf.On["pull_request_target"]; bad {
		findings = append(findings, "triggers on pull_request_target")
	}
	if !maps.Equal(wf.Permissions, map[string]string{"contents": "read"}) {
		findings = append(findings, "the workflow's own permissions are not exactly contents: read")
	}
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		job := wf.Jobs[name]
		if len(job.Permissions) == 0 {
			findings = append(findings, "job "+name+" declares no permissions of its own")
		}
		findings = append(findings, stepFindings("job "+name, job.Steps, job.Environment != nil)...)
	}
	actions, err := fs.Glob(root, actionsDir)
	if err != nil {
		return nil, err
	}
	for _, file := range actions {
		action, readErr := fs.ReadFile(root, file)
		if readErr != nil {
			return nil, readErr
		}
		var doc workflowDoc
		if parseErr := yaml.Unmarshal(action, &doc); parseErr != nil {
			return nil, parseErr
		}
		findings = append(findings, stepFindings(file, doc.Runs.Steps, true)...)
	}
	return findings, nil
}

// stepFindings judges the steps of one job (or composite action). inProtected is whether a
// secret may be referenced there at all.
func stepFindings(where string, steps []workflowStep, inProtected bool) []string {
	var findings []string
	for _, s := range steps {
		if s.Uses != "" && !strings.HasPrefix(s.Uses, "./") && !pinnedAction.MatchString(s.Uses) {
			findings = append(findings, where+": action "+s.Uses+" is not pinned by a full commit sha")
		}
		if strings.Contains(s.Run, "${{") {
			findings = append(findings, where+": a run script holds a workflow expression (pass it in env)")
		}
		for _, values := range []map[string]string{s.Env, s.With} {
			for _, key := range slices.Sorted(maps.Keys(values)) {
				if strings.Contains(values[key], "secrets.") && !inProtected {
					findings = append(findings, where+": a secret is used outside a job with an environment")
				}
			}
		}
	}
	return findings
}

func TestTheReleaseWorkflowKeepsItsPromises(t *testing.T) {
	findings, err := releaseWorkflowFindings(os.DirFS(filepath.Join("..", "..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

const goodWorkflow = `
on:
  push:
    branches: ["F[0-9]*"]
permissions:
  contents: read
jobs:
  plan:
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262
      - run: echo "$BRANCH"
        env:
          BRANCH: ${{ github.ref_name }}
  tag:
    environment: release
    permissions:
      contents: write
    steps:
      - uses: ./.github/actions/release-key
        with:
          key: ${{ secrets.RELEASE_SIGNING_KEY }}
`

// Each promise is judged by a workflow that breaks only it, so a rule that has stopped
// looking is caught here and not by a release.
func TestEachReleaseWorkflowPromiseCanFail(t *testing.T) {
	cases := map[string]struct {
		workflow string
		want     string
	}{
		"a tag instead of a sha": {
			strings.Replace(goodWorkflow, "@11d5960a326750d5838078e36cf38b85af677262", "@v4", 1),
			"not pinned by a full commit sha",
		},
		"a short sha": {
			strings.Replace(goodWorkflow, "@11d5960a326750d5838078e36cf38b85af677262", "@11d5960", 1),
			"not pinned by a full commit sha",
		},
		"pull_request_target": {
			strings.Replace(goodWorkflow, "on:\n  push:", "on:\n  pull_request_target:\n  push:", 1),
			"pull_request_target",
		},
		"wide default permissions": {
			strings.Replace(goodWorkflow, "permissions:\n  contents: read\njobs:", "permissions:\n  contents: write\njobs:", 1),
			"not exactly contents: read",
		},
		"a job without permissions": {
			strings.Replace(goodWorkflow, "  plan:\n    permissions:\n      contents: read\n", "  plan:\n", 1),
			"job plan declares no permissions",
		},
		"an expression in a script": {
			strings.Replace(goodWorkflow, `run: echo "$BRANCH"`, `run: echo "${{ github.head_ref }}"`, 1),
			"workflow expression",
		},
		"a secret outside the environment": {
			strings.Replace(goodWorkflow, "        env:\n          BRANCH: ${{ github.ref_name }}", "        env:\n          K: ${{ secrets.RELEASE_SIGNING_KEY }}", 1),
			"secret is used outside",
		},
	}
	for name, c := range cases {
		root := fstest.MapFS{releaseWorkflow: {Data: []byte(c.workflow)}}
		findings, err := releaseWorkflowFindings(root)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.ContainsFunc(findings, func(f string) bool { return strings.Contains(f, c.want) }) {
			t.Errorf("%s: findings %q, want one containing %q", name, findings, c.want)
		}
	}
}

func TestTheGoodWorkflowHasNoFindings(t *testing.T) {
	findings, err := releaseWorkflowFindings(fstest.MapFS{releaseWorkflow: {Data: []byte(goodWorkflow)}})
	if err != nil || len(findings) != 0 {
		t.Fatalf("findings %q, err %v", findings, err)
	}
}

// A local action is judged like the workflow: an unpinned action or an expression in a script
// inside it would reach the job that holds the key.
func TestALocalActionIsJudgedToo(t *testing.T) {
	root := fstest.MapFS{
		releaseWorkflow: {Data: []byte(goodWorkflow)},
		".github/actions/release-key/action.yml": {Data: []byte(`
runs:
  using: composite
  steps:
    - uses: some/action@main
    - shell: bash
      run: echo "${{ inputs.key }}"
`)},
	}
	findings, err := releaseWorkflowFindings(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings %q, want the unpinned action and the expression", findings)
	}
}

package toolchain

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
	prTitleWorkflow = ".github/workflows/pr-title.yml"
	qaWorkflow      = ".github/workflows/qa.yml"
	actionsDir      = ".github/actions/*/action.yml"

	// releaseEnvironment is the protected environment that alone may read the release key.
	releaseEnvironment = "release"
)

// pinnedAction is owner/repo[/path]@<full commit sha>.
var pinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)

type workflowStep struct {
	Name string            `yaml:"name"`
	If   string            `yaml:"if"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
	With map[string]string `yaml:"with"`
}

// Permissions are pointers so that "declares none" (nil) is told from "declares an empty set".
type workflowJob struct {
	Name        string             `yaml:"name"`
	Permissions *map[string]string `yaml:"permissions"`
	Environment any                `yaml:"environment"`
	Env         map[string]string  `yaml:"env"`
	Steps       []workflowStep     `yaml:"steps"`
}

type workflowDoc struct {
	On          map[string]any     `yaml:"on"`
	Permissions *map[string]string `yaml:"permissions"`
	Env         map[string]string  `yaml:"env"`
	Jobs        map[string]workflowJob
	Runs        struct {
		Steps []workflowStep `yaml:"steps"`
	} `yaml:"runs"`
}

// workflowSpec is what one workflow may hold: its own permissions and each job's, pinned exactly.
// Widening one is a change to this table, reviewed with the workflow it widens.
type workflowSpec struct {
	path        string
	permissions map[string]string
	jobs        map[string]map[string]string
}

var releaseWorkflows = []workflowSpec{
	{
		path:        releaseWorkflow,
		permissions: map[string]string{"contents": "read"},
		jobs: map[string]map[string]string{
			"plan":    {"contents": "read", "pull-requests": "read"},
			"prepare": {"contents": "read", "pull-requests": "write"},
			"tag":     {"contents": "write", "actions": "read"},
		},
	},
	{
		path:        prTitleWorkflow,
		permissions: map[string]string{},
		jobs:        map[string]map[string]string{"title": {}},
	},
}

// permissionsText is a permission set as the workflow spells it, in one fixed order.
func permissionsText(p map[string]string) string {
	if len(p) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(p))
	for _, key := range slices.Sorted(maps.Keys(p)) {
		parts = append(parts, key+": "+p[key])
	}
	return strings.Join(parts, ", ")
}

// environmentName is the name of a job's environment, which is a string or a map with a name.
func environmentName(env any) string {
	switch v := env.(type) {
	case string:
		return v
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			return name
		}
	}
	return ""
}

// releaseWorkflowFindings is every promise the given workflows and the local actions break.
func releaseWorkflowFindings(root fs.FS, specs []workflowSpec) ([]string, error) {
	var findings []string
	for _, spec := range specs {
		got, err := workflowFindings(root, spec)
		if err != nil {
			return nil, err
		}
		findings = append(findings, got...)
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

// workflowFindings judges one workflow against its spec.
func workflowFindings(root fs.FS, spec workflowSpec) ([]string, error) {
	data, err := fs.ReadFile(root, spec.path)
	if err != nil {
		return nil, err
	}
	var wf workflowDoc
	if err = yaml.Unmarshal(data, &wf); err != nil {
		return nil, err
	}
	findings := topLevelFindings(spec, wf)
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		findings = append(findings, jobFindings(spec, name, wf.Jobs[name])...)
	}
	for _, name := range slices.Sorted(maps.Keys(spec.jobs)) {
		if _, found := wf.Jobs[name]; !found {
			findings = append(findings, "pinned job "+name+" is not in "+spec.path)
		}
	}
	return findings, nil
}

// topLevelFindings judges what a workflow says outside its jobs: its trigger, its own
// permissions and an env every job would read.
func topLevelFindings(spec workflowSpec, wf workflowDoc) []string {
	var findings []string
	if _, bad := wf.On["pull_request_target"]; bad {
		findings = append(findings, spec.path+": triggers on pull_request_target")
	}
	if wf.Permissions == nil || !maps.Equal(*wf.Permissions, spec.permissions) {
		findings = append(findings, spec.path+": the workflow's own permissions are not exactly "+permissionsText(spec.permissions))
	}
	for _, key := range slices.Sorted(maps.Keys(wf.Env)) {
		if strings.Contains(wf.Env[key], "secrets.") {
			findings = append(findings, spec.path+": the workflow-level env "+key+" holds a secret, which every job could read")
		}
	}
	return findings
}

// jobFindings judges one job: its permissions against the pinned set, a secret in its env unless
// it is a job of the release environment, and its steps.
func jobFindings(spec workflowSpec, name string, job workflowJob) []string {
	var findings []string
	pinned, isPinned := spec.jobs[name]
	switch {
	case job.Permissions == nil:
		findings = append(findings, "job "+name+" declares no permissions of its own")
	case !isPinned:
		findings = append(findings, "job "+name+" has no pinned permissions in this test: pin what it may hold")
	case !maps.Equal(*job.Permissions, pinned):
		findings = append(findings, "job "+name+" holds "+permissionsText(*job.Permissions)+", not the pinned "+permissionsText(pinned))
	}
	inRelease := environmentName(job.Environment) == releaseEnvironment
	for _, key := range slices.Sorted(maps.Keys(job.Env)) {
		if strings.Contains(job.Env[key], "secrets.") && !inRelease {
			findings = append(findings, "job "+name+": a secret is used outside a job of the "+releaseEnvironment+" environment (env "+key+")")
		}
	}
	return append(findings, stepFindings("job "+name, job.Steps, inRelease)...)
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
					findings = append(findings, where+": a secret is used outside a job of the "+releaseEnvironment+" environment")
				}
			}
		}
	}
	return findings
}

func TestTheReleaseWorkflowKeepsItsPromises(t *testing.T) {
	findings, err := releaseWorkflowFindings(os.DirFS(filepath.Join("..", "..", "..")), releaseWorkflows)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// fixtureSpecs is what goodWorkflow may hold.
var fixtureSpecs = []workflowSpec{{
	path:        releaseWorkflow,
	permissions: map[string]string{"contents": "read"},
	jobs: map[string]map[string]string{
		"plan": {"contents": "read"},
		"tag":  {"contents": "write"},
	},
}}

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
		"a secret in a job-level env": {
			strings.Replace(goodWorkflow, "  plan:\n    permissions:\n      contents: read\n", "  plan:\n    permissions:\n      contents: read\n    env:\n      K: ${{ secrets.RELEASE_SIGNING_KEY }}\n", 1),
			"secret is used outside",
		},
		"a secret in the workflow-level env": {
			strings.Replace(goodWorkflow, "permissions:\n  contents: read\njobs:", "permissions:\n  contents: read\nenv:\n  K: ${{ secrets.RELEASE_SIGNING_KEY }}\njobs:", 1),
			"workflow-level env K holds a secret",
		},
		"a secret in a job of another environment": {
			strings.Replace(goodWorkflow, "environment: release", "environment: staging", 1),
			"secret is used outside a job of the release environment",
		},
		"a job with wider permissions": {
			strings.Replace(goodWorkflow, "  plan:\n    permissions:\n      contents: read\n", "  plan:\n    permissions:\n      contents: write\n", 1),
			"job plan holds contents: write, not the pinned contents: read",
		},
		"a job nobody pinned": {
			goodWorkflow + "  extra:\n    permissions:\n      contents: read\n    steps: []\n",
			"job extra has no pinned permissions",
		},
		"a pinned job that is gone": {
			strings.Replace(goodWorkflow, "  plan:\n", "  decide:\n", 1),
			"pinned job plan is not in",
		},
	}
	for name, c := range cases {
		root := fstest.MapFS{releaseWorkflow: {Data: []byte(c.workflow)}}
		findings, err := releaseWorkflowFindings(root, fixtureSpecs)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.ContainsFunc(findings, func(f string) bool { return strings.Contains(f, c.want) }) {
			t.Errorf("%s: findings %q, want one containing %q", name, findings, c.want)
		}
	}
}

func TestTheGoodWorkflowHasNoFindings(t *testing.T) {
	findings, err := releaseWorkflowFindings(fstest.MapFS{releaseWorkflow: {Data: []byte(goodWorkflow)}}, fixtureSpecs)
	if err != nil || len(findings) != 0 {
		t.Fatalf("findings %q, err %v", findings, err)
	}
}

// An environment is a name or a map with a name; both are the release environment.
func TestTheEnvironmentMayBeNamedInAMap(t *testing.T) {
	mapped := strings.Replace(goodWorkflow, "environment: release", "environment:\n      name: release", 1)
	findings, err := releaseWorkflowFindings(fstest.MapFS{releaseWorkflow: {Data: []byte(mapped)}}, fixtureSpecs)
	if err != nil || len(findings) != 0 {
		t.Fatalf("findings %q, err %v", findings, err)
	}
}

const goodPRTitle = `
on:
  pull_request:
permissions: {}
jobs:
  title:
    permissions: {}
    steps:
      - run: echo "$TITLE"
        env:
          TITLE: ${{ github.event.pull_request.title }}
`

var prTitleSpecs = []workflowSpec{{
	path:        prTitleWorkflow,
	permissions: map[string]string{},
	jobs:        map[string]map[string]string{"title": {}},
}}

// The pull request title workflow handles untrusted text, so it is judged like the release
// workflow: an empty permission set is a declared one, and an expression in its script is a finding.
func TestThePullRequestTitleWorkflowIsJudgedToo(t *testing.T) {
	good, err := releaseWorkflowFindings(fstest.MapFS{prTitleWorkflow: {Data: []byte(goodPRTitle)}}, prTitleSpecs)
	if err != nil || len(good) != 0 {
		t.Fatalf("the good title workflow: findings %q, err %v", good, err)
	}
	bad := strings.Replace(goodPRTitle, `run: echo "$TITLE"`, `run: echo "${{ github.event.pull_request.title }}"`, 1)
	findings, err := releaseWorkflowFindings(fstest.MapFS{prTitleWorkflow: {Data: []byte(bad)}}, prTitleSpecs)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(findings, func(f string) bool { return strings.Contains(f, "workflow expression") }) {
		t.Fatalf("findings %q, want the expression in the title script", findings)
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
	findings, err := releaseWorkflowFindings(root, fixtureSpecs)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings %q, want the unpinned action and the expression", findings)
	}
}

// releaseBranchPattern is the branch release.yml creates for a release: the prefix before $VERSION.
var releaseBranchPattern = regexp.MustCompile(`git switch -c "([A-Za-z0-9._-]+)\$VERSION"`)

// releaseBranchFindings is the promise that CI can pass on the commit the release workflow tags.
// The tag job needs a passing qa.yml run on the release commit, and the only run that commit gets
// is the push of the branch release.yml creates. qa.yml's base job fails a branch that lacks the
// default branch's tip, which a release branch does whenever the default branch moved during the
// approval, and always for the release branch of an older Fedora major, so qa.yml must exempt
// that branch or the tag job can never see CI pass.
func releaseBranchFindings(root fs.FS) ([]string, error) {
	data, err := fs.ReadFile(root, releaseWorkflow)
	if err != nil {
		return nil, err
	}
	var wf workflowDoc
	if err = yaml.Unmarshal(data, &wf); err != nil {
		return nil, err
	}
	var prefixes []string
	for _, name := range slices.Sorted(maps.Keys(wf.Jobs)) {
		for _, step := range wf.Jobs[name].Steps {
			for _, m := range releaseBranchPattern.FindAllStringSubmatch(step.Run, -1) {
				prefixes = append(prefixes, m[1])
			}
		}
	}
	if len(prefixes) != 1 {
		return []string{releaseWorkflow + ": found " + strconv.Itoa(len(prefixes)) + " release branches this test can read (`git switch -c \"<prefix>$VERSION\"`), want exactly one"}, nil
	}
	qaData, err := fs.ReadFile(root, qaWorkflow)
	if err != nil {
		return nil, err
	}
	var qa workflowDoc
	if err = yaml.Unmarshal(qaData, &qa); err != nil {
		return nil, err
	}
	exempt := "!startsWith(github.ref_name, '" + prefixes[0] + "')"
	for _, step := range qa.Jobs["base"].Steps {
		if strings.Contains(step.If, exempt) {
			return nil, nil
		}
	}
	return []string{qaWorkflow + ": the base job does not exempt the release branches (" + prefixes[0] + "<version>) with " + exempt +
		", so CI fails on a release commit whose branch lacks the default branch's tip and the tag job never sees it pass"}, nil
}

func TestQAExemptsTheBranchesTheReleaseWorkflowPushes(t *testing.T) {
	findings, err := releaseBranchFindings(os.DirFS(filepath.Join("..", "..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

const (
	releasePushes = "jobs:\n  prepare:\n    steps:\n      - run: |\n          git switch -c \"release-$VERSION\"\n"
	baseCheck     = "jobs:\n  base:\n    steps:\n      - name: Require\n        if: github.event_name == 'push' && github.ref_name != github.event.repository.default_branch%s\n        run: \"true\"\n"
)

// The promise can fail: a base check that does not exempt the release branch is found, one that
// does is not, and a release workflow whose branch this cannot read is a finding, not a pass.
func TestTheReleaseBranchPromiseCanFail(t *testing.T) {
	judge := func(release, qa string) []string {
		t.Helper()
		findings, err := releaseBranchFindings(fstest.MapFS{
			releaseWorkflow: {Data: []byte(release)},
			qaWorkflow:      {Data: []byte(qa)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return findings
	}
	if got := judge(releasePushes, fmt.Sprintf(baseCheck, "")); len(got) != 1 || !strings.Contains(got[0], "does not exempt") {
		t.Fatalf("findings %q, want the missing exemption", got)
	}
	if got := judge(releasePushes, fmt.Sprintf(baseCheck, " && !startsWith(github.ref_name, 'other-')")); len(got) != 1 {
		t.Fatalf("findings %q, want an exemption of another prefix to count for nothing", got)
	}
	if got := judge(releasePushes, fmt.Sprintf(baseCheck, " && !startsWith(github.ref_name, 'release-')")); len(got) != 0 {
		t.Fatalf("findings %q, want none for an exempted release branch", got)
	}
	if got := judge("jobs:\n  prepare:\n    steps:\n      - run: echo nothing\n", fmt.Sprintf(baseCheck, "")); len(got) != 1 || !strings.Contains(got[0], "found 0 release branches") {
		t.Fatalf("findings %q, want a release workflow without a readable release branch refused", got)
	}
}

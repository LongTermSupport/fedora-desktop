package changed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// checkResult is how one check ended: its merged output and the status that decides pass or fail.
type checkResult struct {
	output string
	code   int
}

// checkDef is one check: how it is named in the plan and the report, what it needs on the host,
// and how it runs. Adding a tool is a row here, not a call site.
type checkDef struct {
	id string
	// listed is whether the plan names the items (files, modules, playbooks) after the id.
	listed bool
	// noun makes the summary "<n> <noun>"; fixed is the summary when there is no noun.
	noun, fixed string
	// gate marks a script whose exit status 2 means it could not run, as against failed.
	gate bool
	// whole is a check that runs over the whole repository whatever file changed, so a deleted
	// file (which has nothing left to check) may still ask for it.
	whole bool
	tools []string
	run   func(ctx context.Context, s *session, cp *checkPlan) (checkResult, error)
}

func (d checkDef) summaryFor(cp *checkPlan) string {
	if d.noun == "" {
		return d.fixed
	}
	return fmt.Sprintf("%d %s", len(cp.items), d.noun)
}

// The tools the checks need, by the name they have on PATH.
const (
	toolBash       = "bash"
	toolShellcheck = "shellcheck"
	toolSemgrep    = "semgrep"
	toolRuff       = "ruff"
	toolPython     = "python3"
	toolAnsible    = "ansible-playbook"
	toolNode       = "node"
	toolGo         = "go"
	toolGolangci   = "golangci-lint"
	nounFiles      = "file(s)"
)

// checkDefs is every check in the order they run. The order is the contract (SPEC 8.2).
var checkDefs = []checkDef{
	{id: "toolchain", fixed: "QA tool versions match .qa-versions", gate: true, tools: []string{toolBash}, run: runToolchain},
	{id: "bash-syntax", listed: true, noun: nounFiles, tools: []string{toolBash}, run: runBashSyntax},
	{id: "shellcheck", listed: true, noun: nounFiles, tools: []string{toolShellcheck}, run: runShellcheck},
	{id: "semgrep", listed: true, noun: nounFiles, tools: []string{toolSemgrep}, run: runSemgrep},
	{id: "ruff", listed: true, noun: nounFiles, tools: []string{toolRuff}, run: runRuff},
	{id: "unit-tests", listed: true, noun: "module(s)", tools: []string{toolPython}, run: runUnitTests},
	{id: "ansible-syntax", listed: true, noun: "playbook(s)", tools: []string{toolAnsible}, run: runAnsibleSyntax},
	{id: "ansible-patterns", fixed: "fail-fast patterns, whole gate", gate: true, tools: []string{toolBash}, run: runAnsiblePatterns},
	{id: "patterns", fixed: "bash-convention rules, whole gate", gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runPatterns},
	{id: "docs", fixed: "whole gate", gate: true, whole: true, tools: []string{toolBash}, run: runDocs},
	{id: "go", fixed: "whole gate (interim qa-go.bash)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolGolangci}, run: runGoGate},
	{id: "bash-limits", fixed: "whole gate (tooling is Go; allowlist ratchet)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runBashLimits},
	{id: "nokill-containerwatch", fixed: "whole gate (the container-watch watchdog never signals a process)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runNoKillContainerWatch},
	{id: "node-check", listed: true, noun: nounFiles, tools: []string{toolNode}, run: runNodeCheck},
	{id: "eslint", listed: true, noun: nounFiles, tools: []string{eslintTool}, run: runEslint},
}

// checkByID is the check with the id, and whether there is one.
func checkByID(id string) (checkDef, bool) {
	i := slices.IndexFunc(checkDefs, func(d checkDef) bool { return d.id == id })
	if i < 0 {
		return checkDef{}, false
	}
	return checkDefs[i], true
}

func isCheckID(id string) bool {
	_, ok := checkByID(id)
	return ok
}

// eslintTool is the preflight name of the extensions/ eslint, which is a path, not a PATH tool.
const eslintTool = "eslint"

// exec runs one command in the repository root with its output merged.
func (s *session) exec(ctx context.Context, env []string, argv ...string) (checkResult, error) {
	res, err := s.runCmd(ctx, proc.Cmd{Argv: argv, Dir: s.root, Env: env, Merge: true})
	if err != nil {
		return checkResult{}, err
	}
	return checkResult{output: res.Stdout, code: res.Code}, nil
}

// script runs one of the repository's gate scripts.
func (s *session) script(ctx context.Context, env []string, name string) (checkResult, error) {
	return s.exec(ctx, env, "bash", filepath.Join(s.scripts, name))
}

func runToolchain(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-toolchain.bash")
}

func runGoGate(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-go.bash")
}

func runBashLimits(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-bash-limits.bash")
}

func runNoKillContainerWatch(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-nokill-containerwatch.bash")
}

// gateWithReport runs a gate that requires QA_JSON_OUT, a file for its machine report, which
// nothing here reads.
func (s *session) gateWithReport(ctx context.Context, name string) (checkResult, error) {
	report, err := s.tempFile()
	if err != nil {
		return checkResult{}, err
	}
	return s.script(ctx, []string{"QA_JSON_OUT=" + report}, name)
}

func runAnsiblePatterns(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-ansible.bash")
}

func runPatterns(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-patterns.bash")
}

func runDocs(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-docs.bash")
}

// eachFile runs argvFor(item) for every item, trying all of them, and fails if any does.
func (s *session) eachFile(ctx context.Context, items []string, argvFor func(string) []string) (checkResult, error) {
	var out strings.Builder
	code := 0
	for _, item := range items {
		res, err := s.exec(ctx, nil, argvFor(item)...)
		if err != nil {
			return checkResult{}, err
		}
		out.WriteString(res.output)
		if res.code != 0 {
			code = 1
		}
	}
	return checkResult{output: out.String(), code: code}, nil
}

func runBashSyntax(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.eachFile(ctx, cp.items, func(f string) []string { return []string{"bash", "-n", f} })
}

func runNodeCheck(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.eachFile(ctx, cp.items, func(f string) []string { return []string{"node", "--check", f} })
}

// shellcheckSeverity is the bar qa-bash.bash gates on: error and warning.
const shellcheckSeverity = "--severity=warning"

// shellcheckFollow lets shellcheck read the files a script sources. qa-bash.bash passes every
// bash file at once, so shellcheck sees a sourced library there; a diff passes only the changed
// files, and without this a variable a script sets for its library reads as unused (SC2034),
// failing here what the full gate passes.
const shellcheckFollow = "--external-sources"

func runShellcheck(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.exec(ctx, nil, append([]string{"shellcheck", shellcheckFollow, shellcheckSeverity}, cp.items...)...)
}

func runRuff(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.exec(ctx, nil, append([]string{"ruff", "check"}, cp.items...)...)
}

func runUnitTests(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	return s.exec(ctx, env, append([]string{"python3", "-m", "unittest"}, cp.items...)...)
}

func runEslint(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	prefix := cp.rel + "/"
	argv := make([]string, 1, 1+len(cp.items))
	argv[0] = "./node_modules/.bin/eslint"
	for _, f := range cp.items {
		argv = append(argv, strings.TrimPrefix(f, prefix))
	}
	res, err := s.runCmd(ctx, proc.Cmd{Argv: argv, Dir: filepath.Join(s.root, cp.rel), Merge: true})
	if err != nil {
		return checkResult{}, err
	}
	return checkResult{output: res.Stdout, code: res.Code}, nil
}

// runAnsibleSyntax checks every playbook (stdout discarded, as the gate does) and names the
// ones that fail.
func runAnsibleSyntax(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	var out strings.Builder
	code := 0
	env := []string{"ANSIBLE_CONFIG=" + filepath.Join(s.root, "ansible.cfg")}
	for _, pb := range cp.items {
		res, err := s.runCmd(ctx, proc.Cmd{Argv: []string{"ansible-playbook", "--syntax-check", pb}, Dir: s.root, Env: env})
		if err != nil {
			return checkResult{}, err
		}
		if res.Code != 0 {
			out.WriteString(res.Stderr)
			fmt.Fprintf(&out, "^^ %s\n", pb)
			code = 1
		}
	}
	return checkResult{output: out.String(), code: code}, nil
}

// semgrepResults is the part of semgrep's JSON report that matters.
type semgrepResults struct {
	Results []struct {
		Path  string `json:"path"`
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
		CheckID string `json:"check_id"`
		Extra   struct {
			Message string `json:"message"`
		} `json:"extra"`
	} `json:"results"`
}

// semgrepAbnormal is the lowest exit status that means semgrep itself broke.
const semgrepAbnormal = 2

// runSemgrep copies the files into a temporary tree (an extensionless one gets .bash appended
// so semgrep's language detection sees shell) and runs the project's bash rules over it. The
// JSON report is decoded here: a report that cannot be read is a failure, never a pass.
func runSemgrep(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	mirror, err := s.tempDir()
	if err != nil {
		return checkResult{}, err
	}
	report, err := s.tempFile()
	if err != nil {
		return checkResult{}, err
	}
	for _, f := range cp.items {
		m := f
		if !strings.HasSuffix(f, ".sh") && !strings.HasSuffix(f, ".bash") {
			m += ".bash"
		}
		if copyErr := copyFile(filepath.Join(s.root, f), filepath.Join(mirror, m)); copyErr != nil {
			return checkResult{}, copyErr
		}
	}
	res, err := s.runCmd(ctx, proc.Cmd{
		Argv: []string{
			"semgrep", "--config", filepath.Join(s.root, ".semgrep", "bash-conventions.yml"),
			"--json-output", report, "--metrics=off", "--quiet", ".",
		},
		Dir:   mirror,
		Merge: true,
	})
	if err != nil {
		return checkResult{}, err
	}
	if res.TimedOut {
		return checkResult{output: res.Stdout, code: res.Code}, nil
	}
	if res.Code >= semgrepAbnormal {
		return checkResult{output: fmt.Sprintf("%ssemgrep failed (rc=%d)\n", res.Stdout, res.Code), code: res.Code}, nil
	}
	return semgrepFindings(report)
}

func semgrepFindings(report string) (checkResult, error) {
	raw, err := os.ReadFile(report)
	if err != nil {
		return checkResult{output: fmt.Sprintf("cannot read semgrep's report: %v\n", err), code: 1}, nil
	}
	var decoded semgrepResults
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return checkResult{output: fmt.Sprintf("cannot read semgrep's report: %v\n", err), code: 1}, nil
	}
	var out strings.Builder
	for _, r := range decoded.Results {
		fmt.Fprintf(&out, "%s:%d %s %s\n", r.Path, r.Start.Line, r.CheckID, r.Extra.Message)
	}
	if out.Len() > 0 {
		return checkResult{output: out.String(), code: 1}, nil
	}
	return checkResult{}, nil
}

// copyMode is the mode of the mirrored files and directories: private, they hold a copy of
// repository files for one run.
const (
	copyFileMode = 0o600
	copyDirMode  = 0o700
)

func copyFile(src, dst string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("cannot mirror %s for semgrep: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), copyDirMode); err != nil {
		return fmt.Errorf("cannot mirror %s for semgrep: %w", src, err)
	}
	if err := os.WriteFile(dst, body, copyFileMode); err != nil {
		return fmt.Errorf("cannot mirror %s for semgrep: %w", src, err)
	}
	return nil
}

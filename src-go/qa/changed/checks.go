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
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/js"
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
	{id: "toolchain", fixed: "QA tool versions match .qa-versions", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runToolchain},
	{id: "bash-syntax", listed: true, noun: nounFiles, tools: []string{toolBash}, run: runBashSyntax},
	{id: "shellcheck", listed: true, noun: nounFiles, tools: []string{toolShellcheck}, run: runShellcheck},
	{id: "shell-directives", listed: true, noun: nounFiles, tools: []string{toolBash, toolGo}, run: runShellDirectives},
	{id: "semgrep", listed: true, noun: nounFiles, tools: []string{toolSemgrep}, run: runSemgrep},
	{id: "ready-wait-files", listed: true, noun: nounFiles, gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runReadyWaitFiles},
	{id: "stt-rules-files", listed: true, noun: nounFiles, gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runSttRulesFiles},
	{id: "ruff", listed: true, noun: nounFiles, tools: []string{toolRuff}, run: runRuff},
	{id: "unit-tests", listed: true, noun: "module(s)", tools: []string{toolPython}, run: runUnitTests},
	{id: "ansible-syntax", listed: true, noun: "playbook(s)", tools: []string{toolAnsible}, run: runAnsibleSyntax},
	{id: "ansible-patterns", fixed: "fail-fast patterns, whole gate", gate: true, tools: []string{toolBash}, run: runAnsiblePatterns},
	{id: "ansible-syntax-gate", fixed: "every playbook, whole gate", gate: true, tools: []string{toolBash, toolGo, toolAnsible}, run: runAnsibleSyntaxGate},
	{id: "patterns", fixed: "bash-convention rules, whole gate", gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runPatterns},
	{id: "bash", fixed: "bash -n and shellcheck, whole gate", gate: true, tools: []string{toolBash, toolGo, toolShellcheck}, run: runBash},
	{id: "python", fixed: "compile and ruff, whole gate", gate: true, tools: []string{toolBash, toolGo, toolRuff, toolPython}, run: runPython},
	{id: "js", fixed: "node --check and ESLint, whole gate", gate: true, tools: []string{toolBash, toolGo, toolNode}, run: runJS},
	{id: "docs", fixed: "whole gate", gate: true, whole: true, tools: []string{toolBash}, run: runDocs},
	{id: "go", fixed: "whole gate (interim qa-go.bash)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolGolangci}, run: runGoGate},
	{id: "bash-limits", fixed: "whole gate (tooling is Go; allowlist ratchet)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runBashLimits},
	{id: "nokill-containerwatch", fixed: "whole gate (the container-watch watchdog never signals a process)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runNoKillContainerWatch},
	{id: "ansible-pause", fixed: "whole gate (every pause registers its answer or says why it waits)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runAnsiblePause},
	{id: "gh-scopes", fixed: "gh-scope-outside-ssot rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runGhScopes},
	{id: "ready-wait", fixed: "whole gate (no wait ignores the exit of the process it waits for)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runReadyWait},
	{id: "plan-script-logging", fixed: "whole gate (no active plan script uses the un-waitable run-log pattern; no plan-local logs/ dir)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runPlanScriptLogging},
	{id: "stt-rules", fixed: "whole gate (no model judged present without its weights, no dropdown label that explains itself)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runSttRules},
	{id: "deployed-drift", fixed: "repo-owned scripts against their deployed copies, whole gate", gate: true, tools: []string{toolBash, toolGo}, run: runDeployedDrift},
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

// scriptOver runs one of the repository's gate scripts over the named files only.
func (s *session) scriptOver(ctx context.Context, name string, files []string) (checkResult, error) {
	return s.exec(ctx, nil, slices.Concat([]string{toolBash, filepath.Join(s.scripts, name)}, files)...)
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

func runAnsiblePause(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-ansible-pause.bash")
}

func runGhScopes(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-gh-scopes.bash")
}

// runReadyWaitFiles runs the ready-wait gate over the changed Python and shell files only.
func runReadyWaitFiles(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.scriptOver(ctx, "qa-ready-wait-rules.bash", cp.items)
}

func runReadyWait(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-ready-wait-rules.bash")
}

func runPlanScriptLogging(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-plan-script-logging.bash")
}

// runSttRulesFiles runs the speech-to-text rules gate over the changed JavaScript and Python files only.
func runSttRulesFiles(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.scriptOver(ctx, "qa-speech-to-text-rules.bash", cp.items)
}

func runSttRules(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-speech-to-text-rules.bash")
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

// runAnsibleSyntaxGate is the whole gate over every playbook (the ansible-syntax check is the
// per-playbook one a diff selects).
func runAnsibleSyntaxGate(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-ansible-syntax.bash")
}

func runDeployedDrift(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-deployed-drift.bash")
}

func runPatterns(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-patterns.bash")
}

func runBash(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-bash.bash")
}

// runShellDirectives gives the changed shell files the suppression verdict the shell gate gives
// them (SPEC-qa-bash.md S20): the gate's own --directives mode, one process for all of them.
func runShellDirectives(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.exec(ctx, nil, append([]string{toolBash, filepath.Join(s.scripts, "qa-bash.bash"), "--directives"}, cp.items...)...)
}

func runPython(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-python.bash")
}

func runJS(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.gateWithReport(ctx, "qa-js.bash")
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

// shellcheckSeverity is the bar the shell gate (qa-bash) gates on: error and warning.
const shellcheckSeverity = "--severity=warning"

// shellcheckNoRc, with an empty SHELLCHECK_OPTS, leaves the rules to shellcheck's defaults: no
// .shellcheckrc beside a script, above it or in the home directory, and no environment option, can
// disable a check, exactly as the shell gate runs it.
const shellcheckNoRc = "--norc"

// shellcheckFollow lets shellcheck read the files a script sources. The shell gate passes the
// same flag and every bash file at once; a diff passes only the changed files, and without this a
// variable a script sets for its library reads as unused (SC2034), failing here what the full
// gate passes.
const shellcheckFollow = "--external-sources"

func runShellcheck(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.exec(ctx, []string{"SHELLCHECK_OPTS="}, append([]string{"shellcheck", shellcheckNoRc, shellcheckFollow, shellcheckSeverity}, cp.items...)...)
}

// configFlag names the one config a tool is run under.
const configFlag = "--config"

func runRuff(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.exec(ctx, nil, append([]string{"ruff", "check", configFlag, filepath.Join(s.root, "ruff.toml"), "--no-cache"}, cp.items...)...)
}

func runUnitTests(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	return s.exec(ctx, env, append([]string{"python3", "-m", "unittest"}, cp.items...)...)
}

func runEslint(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	// The js gate's run (SPEC-qa-js.md J8, J9, J11) over the changed files: its pins, its working
	// directory and absolute paths, and its proof that ESLint read every file, so this check and
	// the gate cannot disagree about the rules or about a file ESLint ignored.
	flags := js.EslintFlags(filepath.Join(s.root, cp.rel, ".eslintrc.json"))
	argv := make([]string, 0, 1+len(flags)+len(cp.items))
	argv = append(argv, filepath.Join(s.root, cp.rel, "node_modules", ".bin", "eslint"))
	argv = append(argv, flags...)
	given := make(map[string]string, len(cp.items))
	for _, f := range cp.items {
		abs := filepath.Join(s.root, f)
		argv = append(argv, abs)
		given[abs] = f
	}
	res, err := s.runCmd(ctx, proc.Cmd{Argv: argv, Dir: s.root})
	if err != nil {
		return checkResult{}, err
	}
	return eslintResult(js.Findings(s.root, given, res.Code, res.Stdout, res.Stderr)), nil
}

// eslintResult is ESLint's judged answer as a check result: one line per finding; an answer that
// proves nothing (ESLint failed, ignored a file or skipped one) fails the check with the reason.
func eslintResult(findings []js.Finding, proofErr error) checkResult {
	if proofErr != nil {
		return checkResult{output: proofErr.Error() + "\n", code: 1}
	}
	var out strings.Builder
	for _, f := range findings {
		out.WriteString(f.String() + "\n")
	}
	return checkResult{output: out.String(), code: min(len(findings), 1)}
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
	Results []semgrepreport.Result `json:"results"`
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
			"semgrep", configFlag, filepath.Join(s.root, ".semgrep", "bash-conventions.yml"),
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

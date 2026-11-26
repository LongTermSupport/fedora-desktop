package changed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ghscopes"
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
	// noun makes the summary "<n> <noun>"; fixed is the summary when there is no noun.
	noun, fixed string
	tools       []string
	run         func(ctx context.Context, s *session, cp *checkPlan) (checkResult, error)
	// listed is whether the plan names the items (files, modules, playbooks) after the id.
	listed bool
	// gate marks a script whose exit status 2 means it could not run, as against failed.
	gate bool
	// whole is a check that runs over the whole repository whatever file changed, so a deleted
	// file (which has nothing left to check) may still ask for it.
	whole bool
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
	toolGitleaks   = "gitleaks"
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
	{id: "hook-handler-rules-files", listed: true, noun: nounFiles, gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runHookHandlerRulesFiles},
	{id: "walk-open-files", listed: true, noun: nounFiles, gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runWalkOpenFiles},
	{id: "stdin-wait-files", listed: true, noun: nounFiles, gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runStdinWaitFiles},
	{id: "ruff", listed: true, noun: nounFiles, tools: []string{toolRuff}, run: runRuff},
	{id: "unit-tests", listed: true, noun: "module(s)", tools: []string{toolPython}, run: runUnitTests},
	{id: "ansible-syntax", listed: true, noun: "playbook(s)", gate: true, tools: []string{toolBash, toolGo, toolAnsible}, run: runAnsibleSyntax},
	{id: "ansible-patterns", fixed: "fail-fast patterns, whole gate", gate: true, tools: []string{toolBash}, run: runAnsiblePatterns},
	{id: "ansible-syntax-gate", fixed: "every playbook, whole gate", gate: true, tools: []string{toolBash, toolGo, toolAnsible}, run: runAnsibleSyntaxGate},
	{id: "patterns", fixed: "bash-convention rules, whole gate", gate: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runPatterns},
	{id: "bash", fixed: "bash -n and shellcheck, whole gate", gate: true, tools: []string{toolBash, toolGo, toolShellcheck}, run: runBash},
	{id: "python", fixed: "compile and ruff, whole gate", gate: true, tools: []string{toolBash, toolGo, toolRuff, toolPython}, run: runPython},
	{id: "js", fixed: "node --check and ESLint, whole gate", gate: true, tools: []string{toolBash, toolGo, toolNode}, run: runJS},
	{id: "docs", fixed: "whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runDocs},
	{id: "go", fixed: "go-qa, both modules (diff mode on a git change)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runGoGate},
	{id: "bash-limits", fixed: "whole gate (tooling is Go; allowlist ratchet)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runBashLimits},
	{id: "nokill-containerwatch", fixed: "whole gate (the container-watch watchdog never signals a process)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runNoKillContainerWatch},
	{id: "teamchat-keeps-off-v1", fixed: "whole gate (no file v2 of the agent-team bus owns holds a v1 name)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runTeamchatKeepsOffV1},
	{id: "ansible-pause", fixed: "whole gate (every pause registers its answer or says why it waits)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runAnsiblePause},
	{id: "gh-scopes", fixed: "gh-scope-outside-ssot rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runGhScopes},
	{id: "find-race", fixed: "find-race-negated-stat-test rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runFindRace},
	{id: "arch-pin", fixed: "arch-pinned-artefact rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runArchPin},
	{id: "argv-leak", fixed: "secret-in-argv rules, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runArgvLeak},
	{id: "place-issued-token", fixed: "token-bundle-into-place-issued rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runPlaceIssuedToken},
	{id: "boot-hold", fixed: "unit-holds-boot-target rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runBootHold},
	{id: "gh-poll", fixed: "github-poll-too-often rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runGHPoll},
	{id: "secret-screen", fixed: "secret-on-normal-screen rules, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runSecretScreen},
	{id: "temp-exclusive", fixed: "temp-not-exclusive rules, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runTempExclusive},
	{id: "ready-wait", fixed: "whole gate (no wait ignores the exit of the process it waits for)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runReadyWait},
	{id: "hidden-bindings", fixed: "whole gate (no action reachable only by a key)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolPython}, run: runHiddenBindings},
	{id: "lxc-attach-streams", fixed: "whole gate (no command reaching lxc-attach has a standard stream on a file)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runLxcAttachStreams},
	{id: "plan-script-logging", fixed: "whole gate (no active plan script uses the un-waitable run-log pattern; no plan-local logs/ dir)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runPlanScriptLogging},
	{id: "stt-rules", fixed: "whole gate (no model judged present without its weights, no dropdown label that explains itself, no job end published before its cleanup)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runSttRules},
	{id: "walk-open", fixed: "walk-opens-non-regular-file rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runWalkOpen},
	{id: "stdin-wait", fixed: "stdin-read-unbounded rule, whole gate", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runStdinWait},
	{id: "hook-handler-rules", fixed: "whole gate (no code the hooks daemon loads resolves a path against the daemon's working directory, matches text against the raw Bash command line outside the sanctioned judge _raw_command_judge, or reuses a name that judge's exemption trusts)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolSemgrep}, run: runHookHandlerRules},
	{id: "version-pins", fixed: "whole gate (the version-pin manifest is well formed and every pin still resolves to a declared var)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runVersionPins},
	{id: "go-deps", fixed: "whole gate (src-go's dependencies allowlisted, pinned and vendored in step)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runGoDeps},
	{id: "go-vuln", fixed: "whole gate (govulncheck finds no vulnerability src-go reaches)", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runGoVuln},
	{id: "hook-handlers", fixed: "whole gate (the project hook handlers pass their tests under the daemon's interpreter)", gate: true, whole: true, tools: []string{toolBash}, run: runHookHandlers},
	{id: "plan-tests", fixed: "whole gate (every active plan folder's own Python tests pass, each run from its folder)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolPython}, run: runPlanTests},
	{id: "vmtest-manifest", fixed: "whole gate (the VM-test scenario manifest is valid and agrees with its guest scripts)", gate: true, whole: true, tools: []string{toolBash, toolGo, toolPython}, run: runVmtestManifest},
	{id: "gitleaks", fixed: "whole gate (no secret gitleaks finds in what a commit would carry)", gate: true, whole: true, tools: []string{toolBash, toolGitleaks}, run: runGitleaks},
	{id: "ccy-version-bump", fixed: "the change from the base by the pre-commit ccy version gates", gate: true, whole: true, tools: []string{toolBash, toolGo}, run: runCcyVersionBump},
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

// runGoGate runs go-qa over the change qa-changed collected: its diff mode against the same base
// (the changed packages, their importers, and everything when a module or rule file changed).
// Diff mode sees only src-go/, so a file outside it that routes here (a parity reference, a table
// the Go tests read) adds the packages its routing row names (--also-packages), which go-qa runs
// with their importers. The full run is kept for a file whose row says @full (one that can change
// every Go result), for an outside file no row named packages for, and for an explicit file list,
// which is no diff: go-qa's diff mode reads git, not the list.
func runGoGate(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	if s.goFullRun(cp.items) {
		return s.script(ctx, nil, goGateScript)
	}
	argv := []string{toolBash, filepath.Join(s.scripts, goGateScript), "--changed", "--base", s.opts.base}
	if len(s.plan.goPackages) > 0 {
		argv = append(argv, "--also-packages", strings.Join(slices.Sorted(slices.Values(s.plan.goPackages)), ","))
	}
	return s.exec(ctx, nil, argv...)
}

// goFullRun is whether the go check runs the whole gate rather than go-qa's diff mode.
func (s *session) goFullRun(items []string) bool {
	if s.opts.explicit || len(s.plan.goFull) > 0 {
		return true
	}
	return slices.ContainsFunc(items, func(rel string) bool {
		return !strings.HasPrefix(rel, "src-go/") && !s.plan.goNamed[rel]
	})
}

// goGateScript runs go-qa-ci's go-qa over both modules of src-go.
const goGateScript = "qa-go-qa.bash"

func runBashLimits(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-bash-limits.bash")
}

func runNoKillContainerWatch(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-nokill-containerwatch.bash")
}

func runTeamchatKeepsOffV1(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-teamchat-separate.bash")
}

func runAnsiblePause(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-ansible-pause.bash")
}

// semgrepGate runs one gate of the ghscopes engine (one semgrep rule file over every published
// file). In CI, or when a changed file is part of the gate itself (gateSource), it runs whole, as
// qa-all does. Otherwise, on a developer's machine, it runs over the changed files only, named
// after --: an unchanged rule can only find something new in a file that changed, and semgrep
// over the whole tree is the cost of the whole run. CI's diff run and qa-all still run it whole.
func (s *session) semgrepGate(ctx context.Context, wrapper string) (checkResult, error) {
	if s.ci || s.gateChanged(wrapper) {
		return s.script(ctx, nil, wrapper)
	}
	s.narrowed[wrapper] = len(s.plan.existing)
	return s.scriptOver(ctx, wrapper, slices.Concat([]string{"--"}, s.plan.existing))
}

// gateChanged is whether a changed file, existing or deleted, is part of the gate the wrapper runs.
// A file whose change is to its comments alone (the trees table's) changed nothing the gate reads.
func (s *session) gateChanged(wrapper string) bool {
	return slices.ContainsFunc(slices.Concat(s.plan.existing, s.plan.deleted), func(f string) bool {
		return gateSource(wrapper, f) && !slices.Contains(s.plan.commentsOnly, f)
	})
}

// gateSource is whether a changed file is part of a semgrep gate run by the wrapper: the rules and
// fixtures under .semgrep/, the QA module that holds the engine, the wrapper, the wrapper library
// and the trees table the engine reads. Such a change re-applies the gate to every file.
func gateSource(wrapper, rel string) bool {
	return strings.HasPrefix(rel, ".semgrep/") || strings.HasPrefix(rel, "src-go/qa/") ||
		rel == "scripts/"+wrapper || rel == "scripts/lib/go-tool.bash" || rel == "scripts/qa-trees.tsv"
}

// ruleGateSpecs is the engine spec of each semgrep gate's check (gateCheck).
var ruleGateSpecs = map[string]ghscopes.Spec{
	"gh-scopes":          ghscopes.GHScopes,
	"find-race":          ghscopes.FindRace,
	"arch-pin":           ghscopes.ArchPin,
	"argv-leak":          ghscopes.ArgvLeak,
	"place-issued-token": ghscopes.PlaceIssuedToken,
	"boot-hold":          ghscopes.BootHold,
	"gh-poll":            ghscopes.GHPoll,
	"secret-screen":      ghscopes.SecretScreen,
	"temp-exclusive":     ghscopes.TempExclusive,
}

// dropUnreadRuleGates takes out of the plan each semgrep gate that would read none of the changed
// files: every one is a file its rule file excludes (paths.exclude, rule by rule) or one
// scripts/qa-trees.tsv keeps out of every gate. A run over them has nothing for the rule to scan
// (issue #403: a plan-only change routed to a gate whose rule leaves out plan prose). A deleted
// file has nothing left to scan and does not count; a change of deletions alone is planned as
// before. A change to the gate itself (gateSource) still runs it; any other zero-file scan still
// fails closed in the gate.
//
// Only a local run drops: CI runs these gates whole (semgrepGate), where the rule always has
// files to scan, so CI's diff run plans and runs exactly what it did before issue #403.
func (s *session) dropUnreadRuleGates() error {
	if s.ci || len(s.plan.existing) == 0 {
		return nil
	}
	for _, d := range checkDefs {
		spec, isRuleGate := ruleGateSpecs[d.id]
		if !isRuleGate || s.plan.checks[d.id] == nil || s.gateChanged("qa-"+d.id+".bash") {
			continue
		}
		reader, err := spec.Reader(s.root)
		if err != nil {
			return fmt.Errorf("cannot tell which files the %s check reads: %w", d.id, err)
		}
		reads := false
		for _, rel := range s.plan.existing {
			if reads, err = reader.Reads(rel); err != nil || reads {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("cannot tell whether the %s check reads a changed file: %w", d.id, err)
		}
		if !reads {
			delete(s.plan.checks, d.id)
			s.plan.notes = append(s.plan.notes, fmt.Sprintf("%s: not needed: nothing for this rule to scan (%s and %s leave out every changed file)", d.id, reader.RuleFile(), "scripts/qa-trees.tsv"))
		}
	}
	return nil
}

// ciEnabled is whether the CI environment variable says this run is CI: set, and neither
// "false" nor "0" (go-qa reads it the same way). GitHub Actions sets CI=true.
func ciEnabled(getenv func(string) string) bool {
	v := getenv("CI")
	return v != "" && v != "false" && v != "0"
}

func runGhScopes(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-gh-scopes.bash")
}

func runFindRace(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-find-race.bash")
}

func runArchPin(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-arch-pin.bash")
}

func runArgvLeak(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-argv-leak.bash")
}

func runPlaceIssuedToken(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-place-issued-token.bash")
}

func runBootHold(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-boot-hold.bash")
}

func runGHPoll(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-gh-poll.bash")
}

func runSecretScreen(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-secret-screen.bash")
}

func runTempExclusive(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.semgrepGate(ctx, "qa-temp-exclusive.bash")
}

// runReadyWaitFiles runs the ready-wait gate over the changed Python and shell files only.
func runReadyWaitFiles(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.scriptOver(ctx, "qa-ready-wait-rules.bash", cp.items)
}

func runReadyWait(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-ready-wait-rules.bash")
}

func runHiddenBindings(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-hidden-bindings.bash")
}

func runLxcAttachStreams(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-lxc-attach-streams.bash")
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

// runHookHandlerRulesFiles runs the hook handler rules gate over the changed Python files only.
func runHookHandlerRulesFiles(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.scriptOver(ctx, "qa-hook-handler-rules.bash", cp.items)
}

func runHookHandlerRules(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-hook-handler-rules.bash")
}

// runWalkOpenFiles runs the walk-open gate over the changed Python files only.
func runWalkOpenFiles(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.scriptOver(ctx, "qa-walk-open.bash", cp.items)
}

func runWalkOpen(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-walk-open.bash")
}

// runStdinWaitFiles runs the stdin-wait gate over the changed Python files only.
func runStdinWaitFiles(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	return s.scriptOver(ctx, "qa-stdin-wait.bash", cp.items)
}

func runStdinWait(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-stdin-wait.bash")
}

// runGitleaks runs CI's gitleaks scan over what a commit of the work tree would carry.
func runGitleaks(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-gitleaks.bash")
}

// runCcyVersionBump judges the change from the base qa-changed diffed against (the work tree as a
// commit would take it) by the pre-commit hook's ccy version gates (B68, issue #428): a checkout
// with no hooks, CI among them, still refuses a ccy or ccy image change without its version bump.
func runCcyVersionBump(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.exec(ctx, nil, toolBash, filepath.Join(s.scripts, "qa-ccy-version-bump.bash"), s.opts.base)
}

func runVersionPins(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-version-pins.bash")
}

func runGoDeps(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-go-deps.bash")
}

func runGoVuln(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-go-vuln.bash")
}

func runHookHandlers(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-hook-handlers.bash")
}

func runPlanTests(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-plan-tests.bash")
}

func runVmtestManifest(ctx context.Context, s *session, _ *checkPlan) (checkResult, error) {
	return s.script(ctx, nil, "qa-vmtest-manifest.bash")
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
// runAnsibleSyntax is the syntax gate over the playbooks the change selected, named on its
// command line (SPEC-qa-changed B57).
func runAnsibleSyntax(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	report, err := s.tempFile()
	if err != nil {
		return checkResult{}, err
	}
	return s.exec(ctx, []string{"QA_JSON_OUT=" + report}, slices.Concat([]string{toolBash, filepath.Join(s.scripts, "qa-ansible-syntax.bash")}, cp.items)...)
}

// semgrepAbnormal is the lowest exit status that means semgrep itself broke.
const semgrepAbnormal = 2

// runSemgrep copies the files into a temporary tree (an extensionless one gets .bash appended
// so semgrep's language detection sees shell) and runs the project's bash rules over it. The tree
// holds an empty .semgrepignore, as the patterns gate's mirror does, so semgrep's default ignore
// list (tests/ among it) skips nothing. The JSON report is decoded here: a report that cannot be
// read, a file semgrep did not scan or an error it gave is a failure, never a pass.
func runSemgrep(ctx context.Context, s *session, cp *checkPlan) (checkResult, error) {
	mirror, err := s.tempDir()
	if err != nil {
		return checkResult{}, err
	}
	report, err := s.tempFile()
	if err != nil {
		return checkResult{}, err
	}
	if writeErr := os.WriteFile(filepath.Join(mirror, ".semgrepignore"), nil, 0o600); writeErr != nil {
		return checkResult{}, fmt.Errorf("cannot write the mirror's .semgrepignore: %w", writeErr)
	}
	handed := make([]string, 0, len(cp.items))
	for _, f := range cp.items {
		m := f
		if !strings.HasSuffix(f, ".sh") && !strings.HasSuffix(f, ".bash") {
			m += ".bash"
		}
		handed = append(handed, m)
		if copyErr := copyFile(filepath.Join(s.root, f), filepath.Join(mirror, m)); copyErr != nil {
			return checkResult{}, copyErr
		}
	}
	res, err := s.runCmd(ctx, proc.Cmd{
		Argv: []string{
			"semgrep", configFlag, filepath.Join(s.root, ".semgrep", "bash-conventions.yml"),
			"--json-output", report, "--metrics=off", "--quiet", semgrepreport.TimeoutFlag, ".",
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
	return semgrepFindings(report, handed)
}

// semgrepFindings judges semgrep's report over the handed mirror files. A file it could not parse
// at all, any other error, or a handed file it did not scan fails the check. A partial parse is
// shown and passes: the full patterns gate judges it against scripts/patterns-partial.allowlist,
// and refusing it here would refuse a file that gate accepts.
func semgrepFindings(report string, handed []string) (checkResult, error) {
	raw, err := os.ReadFile(report)
	if err != nil {
		return checkResult{output: fmt.Sprintf("cannot read semgrep's report: %v\n", err), code: 1}, nil
	}
	decoded, err := semgrepreport.Decode(string(raw))
	if err != nil {
		return checkResult{output: fmt.Sprintf("cannot read semgrep's report: %v\n", err), code: 1}, nil
	}
	var out strings.Builder
	gating := decoded
	gating.Errors = nil
	for _, e := range decoded.Errors {
		if e.Type.Kind() == "PartialParsing" {
			fmt.Fprintf(&out, "note: semgrep parsed only in part (the patterns gate judges this against scripts/patterns-partial.allowlist): %s\n", e.Describe())
			continue
		}
		gating.Errors = append(gating.Errors, e)
	}
	if judgeErr := gating.Judge("shell", handed); judgeErr != nil {
		return checkResult{output: fmt.Sprintf("%s%v\n", out.String(), judgeErr), code: 1}, nil
	}
	for _, r := range decoded.Results {
		fmt.Fprintf(&out, "%s:%d %s %s\n", r.Path, r.Start.Line, r.CheckID, r.Extra.Message)
	}
	if len(decoded.Results) > 0 {
		return checkResult{output: out.String(), code: 1}, nil
	}
	return checkResult{output: out.String()}, nil
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

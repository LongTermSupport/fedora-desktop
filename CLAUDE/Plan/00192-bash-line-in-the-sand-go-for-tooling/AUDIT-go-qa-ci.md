# go-qa-ci audit (read-only)

Repo: /workspace/untracked/go-qa-ci-work/go-qa-ci, head 7b8787c, clean tree, 46 commits.
The DBF spec repo was not read. This audit relies on RESEARCH-go-qa-ci.md and docs/CONFORMANCE.md.

## 1. Complete and in good shape?

Verdict: feature-complete and honest about its gaps. It is not conformant and does not claim to be
(README.md:12-20; docs/CONFORMANCE.md:3). It is not yet usable by the consumer.

### Self-gate and tests

- `go test ./...` here: 31 packages ok, 4 FAIL (`internal/cli`, `internal/defences`,
  `internal/golangci`, `internal/tools`).
- Every one of the 37 failing tests dies on the same cause: `golangci-lint not found on PATH`
  (log: /workspace/untracked/scratch/goqa-test-fail.log).
- This is a host-tool IaC gap, not a code defect. M14 already notes that the suite needs
  golangci-lint on PATH (JOURNAL 26-10-14, 04:42 entry).
- The self-gate `go-qa run --ci` was not run. It needs golangci-lint, ast-grep, deadcode and
  gremlins (only semgrep is installed here). govulncheck needs the network
  (CONFORMANCE.md "Recorded gaps").
- Only `which semgrep` found a tool. `ast-grep` is absent too, so the ast-grep binary paths ran
  untested here.

### Gaps recorded in docs/CONFORMANCE.md

Unaddressed unless noted.

- 4.1 is a Gap (CONFORMANCE.md:22).
  - staticcheck inside golangci-lint cannot host a bespoke rule.
  - Its `//lint:ignore` is detected but not disabled.
  - ast-grep, deadcode and govulncheck are not graded against DETECTOR-SPEC.
- 4.2 is Partial (:23).
  - Linters, staticcheck codes, advisories and deadcode do not resolve through the docs tool.
  - The bundled `gqaci.ast.*` rules have no pages. The pack is empty, so this is moot today.
- 4.3 is Partial (:24). Uncovered suppression routes: ast-grep ignores in other languages, linter
  settings outside the closed table, a `-c` configuration, `//go:build ignore`, unconditional
  `t.Skip`, and the baseline's own `fieldalignment` switch-off.
- 4.4, 5.1 and 7.1 are Partial (:25-34). Linter route listings are coarse. Vuln and deadcode
  findings have no identifiers of their own.
- 8.1 is Partial (:60). There is no release process, so there is no tag gate.
- 8.2 is Partial (:61).
  - gosec G204 and G306 are configured away for the self-gate.
  - Mutation testing is switched off (`qa/go-qa.toml`), so gremlins has never run against the
    repo.
- Other limits (:63-93):
  - `tools.lock` pins linux/amd64 only.
  - The `licences` runner is not built.
  - `actionlint` is pinned in CI, not in `tools.lock`.
  - `write-denylist.sh` has no test.
  - The `install-tools.sh` download paths are untested.
  - The declaration reconciler does not compare gap summary text with the evidence text.
  - The Public safety CI job is red until the owner sets `GQACI_DENYLIST`. It has never run in CI.
  - There is no independent grader.

### Deferred reports

M14 (`subagent-reports/261014-go-qa-ci-M14-sonnet.md`) dispositioned roughly 100 items. Still open:

- OWNER items: the denylist secret, the commit identity, publication, and any tag.
- Item 24 is RECORDED but not fixed: ast-grep silently passes under a gitignored path. No
  `--no-ignore` was proven, because there is no ast-grep binary in this container.
- Three minor notes remain from the verifier. Two fixes are attributed to the wrong commit.
  `--base` does not pass `--end-of-options` to git merge-base.
- Task 3.3 (consumer adoption) is blocked on publication (PLAN.md:106).

## 2. Custom DBF rule mechanisms

Supported (README.md:116-137, docs/analyzers.md, docs/astgrep.md):

1. `go/analysis` analyzers run via `go vet -vettool`. A project can supply its own driver
   (`[analyzers] driver`, `driver.Main(...)`). Findings print as `error[<name>]`, the project
   record is applied, and analysistest proves each analyzer (a red and a clean fixture).
2. Project ast-grep rules via `sgconfig.yml`. The tool enforces an id (`gqaci.rules.unnamed`) and a
   valid/invalid proof (`gqaci.rules.untested`) for each rule.
3. Project stages (`[[stage]]`, docs/stages.md) for arbitrary commands. These take part in the
   record and the docs.
4. Project record exceptions: `covers = "findings"`, with a budget, a ratchet and an expiry.
5. The golangci-lint strict baseline, `configs/golangci.yml` plus a `qa/golangci-overlay.yml`.

Not supported: semgrep. README, docs and AGENTS.md never mention it. The consumer's own QA uses
semgrep. A semgrep rule could only run as a project stage, with no rule-id or proof enforcement.

Bundled rules: 1 analyzer in total.

- `gqaci_sensitive_format` covers sensitive values printed through fmt, log or slog.
- The bundled ast-grep pack is EMPTY (docs/astgrep.md "The pack is empty"; `internal/astgrep/pack/`
  holds only a README).
- The 12 other `ruledocs` pages are toolchain meta-detectors: record, suppression, summary, docs
  and rules hygiene.
- The RESEARCH doc's second planned analyzer, `gqaci_generated_header` (RESEARCH :402-453), was
  not built. A hand-written generated header is a suppression route that golangci exempts. Only
  partial coverage exists in `internal/suppression/directives.go:28`.
- golangci-lint itself supplies the general-purpose Go defences.

How a consumer adds project rules is documented. Examples are in README "Writing rules",
docs/analyzers.md and docs/astgrep.md. There is no scaffolding command and no worked example in the
repo of a project rule with its proof. The tooling is untested end to end against a real consumer.

## 3. Consumer /workspace/src-go

Project-local DBF rules: NONE.

- There is no `qa/` directory, `go-qa.toml`, `sgconfig.yml`, driver or analyzers anywhere under
  src-go.
- The consumer still runs the INTERIM gate, `scripts/qa-go.bash`. That gate is golangci-lint, go
  vet, tests and a coverage floor (CLAUDE/QA.md:50). The gate itself says it is replaced when
  go-qa-ci lands (`scripts/qa-go.bash:6`).
- go-qa-ci appears in the consumer only in comments (qa-reviewer whole-plan, "Checked and clean").
- No DBF defence was added for any defect found during the ports. The ports fixed the instances in
  the specs, but no defect class got a detector.

Defects mined from SPEC-ccy-teams section 8, SPEC-ccy-sessions section 9, the journal, and the
reports. The column "Catch now" lists any rule that exists today.

| Defect class (source)                                                                 | Catch now                                                                                                                                                                                     | Writable?                                                                                                                                                                                                                        |
| ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Function used before definition (teams 8.01, PLAN.md:12)                              | Go cannot have it. None needed.                                                                                                                                                               | N/A                                                                                                                                                                                                                              |
| Discarded error or `_ =` (whole-plan review)                                          | The golangci baseline (errcheck). In non-test src-go, a grep found no `_ =` statements. The one discarded value is `strings.Cut` at `restore/manifest.go:141`, which errcheck does not cover. | Covered. A blank-ident analyzer is optional.                                                                                                                                                                                     |
| `sh -c` / `bash -c` evasion of gosec (go-qa-ci M3c, `m3c-fix`)                        | gosec G204 in go-qa-ci's baseline. The evasion was fixed by removing the `sh -c` indirection. Class: wrapping a call in a shell to hide it.                                                   | Yes. An ast-grep rule on \`Cmd{Name: "sh"                                                                                                                                                                                        |
| Directory/file mode 0755                                                              | gosec G301/G302/G306 in the baseline. In src-go the only match is `0o7777` (`sessions/restore.go:255`).                                                                                       | Covered.                                                                                                                                                                                                                         |
| Test flakiness on date or midnight (go-qa-ci deferred run 2, #25)                     | Nothing. M14 ACCEPTED it.                                                                                                                                                                     | Yes. An analyzer or ast-grep rule forbids `time.Now()` in `_test.go`. src-go has 3 test files that use it.                                                                                                                       |
| tmux target form: `=name:` vs `=name` (teams/sessions specs; tmux-targets suite)      | The bash suite `test-ccy-tmux-targets.bash` scans bash source. For Go, only the `RealTmux` tests (`qa-go`).                                                                                   | Yes. A type-aware analyzer on `Tmux.Run` argument vectors: capture-pane and send-keys need `=name:` (`panes.go:49` `paneTarget`), while kill-session, detach-client and attach-session use `=name`. Today only tests guard this. |
| `sudo -n` omission (teams parity: bug 11, sudo_ready then plain sudo)                 | Only tests. `internal/sudo` centralises the call.                                                                                                                                             | Yes. ast-grep or an analyzer: `execx.Cmd{Name: "sudo"}` without `-n` outside `internal/sudo`.                                                                                                                                    |
| Missing fzf exits 0 / non-zero collapsed into Esc (sessions 9.01, 9.02)               | None.                                                                                                                                                                                         | Hard to write generally. Can be a project analyzer on `ui.Picker.Pick` exit mapping.                                                                                                                                             |
| `ccy-teams` built without `-X main.RootDir` (cutover review #4)                       | Runtime refusal at `cmd/ccy-teams/main.go:23-28`.                                                                                                                                             | Not a lint. A check on the build flags in the play.                                                                                                                                                                              |
| Dead code (the tmux take-over functions)                                              | `deadcode` is built into go-qa-ci for Go. The functions were bash, so no Go tool saw them.                                                                                                    | Covered for Go.                                                                                                                                                                                                                  |
| Substring detection from display text (sessions 9.09)                                 | None.                                                                                                                                                                                         | Partial. An ast-grep rule on `strings.Contains` against row text.                                                                                                                                                                |
| Non-determinism from map iteration (teams 8.07, sessions 9.14)                        | None.                                                                                                                                                                                         | Yes. An analyzer for `range` over a map with output or append, requiring sort.                                                                                                                                                   |
| Nested "~" prefix replace (sessions 9.10)                                             | None.                                                                                                                                                                                         | Yes. An ast-grep rule on `strings.TrimPrefix(.., home)` or `strings.Replace` with `$HOME`.                                                                                                                                       |
| Cross-language contract drift: Go tests run the bash libraries (whole-plan review #1) | The dependency map in `scripts/qa-changed.bash`.                                                                                                                                              | Not a DBF rule. It is a gate-routing fix.                                                                                                                                                                                        |

Every writable item above is a candidate for a consumer-side driver or `sgconfig.yml`. None of them
exists.

## 4. Optimal?

### Performance

- Diff mode `--changed [--base]` is implemented (README.md:55-93). It narrows packages to the
  changed set plus their importers. `deadcode`, `vuln`, `mod` and `mutants` run whole-module
  (README :55-93). This is good.
- Coverage threshold is applied to the affected packages only, so the diff result can differ from a
  full run (the table at README :55-93).
- The type-aware analyzers cost a second package load (RESEARCH :402-453, accepted). golangci-lint
  plus a vet-tool load happen on every run.
- The diff-mode `--base` option does not pass `--end-of-options` to git merge-base (low risk).

### What a strict reviewer would flag

1. Adoption is blocked. Task 3.3 is unmet (PLAN.md:106). The repo is private and CI and the host
   cannot fetch a private module. The consumer has no DBF rule.
2. The bundled rule set is nearly empty: one analyzer and zero ast-grep rules. The siblings
   (rust-qa-ci with `rqaci_lints`; ts-qa-ci and php-qa-ci) ship bespoke rules. Coverage is
   golangci-lint plus one analyzer, so the "Defence Before Fix" value is the machinery, not
   rules.
3. Dependence on pinned external binaries: golangci-lint, ast-grep, deadcode, govulncheck and
   gremlins. Releases are pinned for linux/amd64 only (`tools.lock`). Missing tools fail
   `go test` loudly. That is the right fail-fast behaviour, but the container image must provide
   them.
4. Mutation testing is switched off in the self-gate, and gosec rules G204 and G306 are
   configured away (CONFORMANCE.md :61).
5. A cosmetic point: README says "Status: early" while CONFORMANCE declares partial/gap at both
   levels. Consistent, and honest.
6. `licences` runner not built. No semgrep route, though the consumer's own QA uses it.
7. The conformance document is the project's own reading and has no independent grader.
   Publication is blocked on owner actions: the secret, a no-reply identity, and making the repo
   public.

### Recommendation

1. Add golangci-lint, ast-grep, deadcode and gremlins to the container toolchain (IaC), then run
   `go-qa run --ci` here.
2. After publication, execute Task 3.3. Create `src-go/qa/go-qa.toml` and a project driver, and
   seed it with the writable classes above. Priorities are `time.Now()` in tests, `sudo` without
   `-n`, the tmux target form, and map-range ordering.
3. Build `gqaci_generated_header`, or record it as dropped.
4. Prove the self-gate gremlins run.

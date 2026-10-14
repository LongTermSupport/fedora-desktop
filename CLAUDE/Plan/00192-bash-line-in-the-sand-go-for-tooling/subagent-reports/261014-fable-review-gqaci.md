# go-qa-ci adversarial review (read-only)

Subject: `/workspace/untracked/go-qa-ci-work/go-qa-ci` at `7b8787c` (46 commits, clean tree,
branch `main`, no tags). Context read: `CLAUDE/Plan/00191-go-cli-tooling/RESEARCH-go-qa-ci.md`,
`subagent-reports/*go-qa-ci*`, `CLAUDE/Plan/00192-.../AUDIT-go-qa-ci.md`. Everything below was
checked against the code or by running the binary; scratch under
`/workspace/untracked/scratch/fable-gqaci/` (`gotest.log`, `selfgate.log`, `consumer-trial.log`,
`strictness.log`, the scripts that produced them).

## Verdict

**Not fully sorted, and not optimal. The machinery is sound and honestly graded; the rules are
not there.** go-qa-ci is a well-built *harness* for Defence Before Fix (record, ratchet,
suppression detector, enumeration, offline docs, diff mode, pinned tools, self-gate), and it passes
its own gate here (`go test ./...` 35/35 ok; `go-qa run` over itself green in 147 s with every
tool but mutants). But as a DBF *toolchain* it ships one bespoke rule (`gqaci_sensitive_format`),
an empty ast-grep pack, and a golangci baseline that is "standard plus 27", not strict. Nobody is
building up custom rules for DBF defect classes inside go-qa-ci; the consumer (`src-go`) has built
four project analyzers of its own, in a nested module go-qa-ci cannot drive, run by an interim
bash gate, with no record and no documentation route. The two halves have not met.

Severity scale: **Blocker** (stops adoption or publication), **High** (wrong or materially weak),
**Medium** (real gap, workaround exists), **Low** (polish).

---

## 1. Genericity

**Clean.** `grep -rniE 'fedora|ccy|claude-yolo|/workspace|/home/|/root/|joseph|edmonds|LongTermSupport'`
over the tree matches only the module path `github.com/LongTermSupport/go-qa-ci`. The same grep
over `git log -p --all` matches only the 46 `Author:` lines (the personal commit identity,
author and committer), which is the known owner action (PUBLICATION-CHECKLIST step 3). Examples
use `example.com`, `example-owner`, `example.com/project`. `LICENSE` is "go-qa-ci contributors".
No consumer path, hostname, container name or plan path anywhere.

One assumption that is consumer-shaped rather than consumer-named:

- **Low** — `internal/changed/widen.go:15` hardcodes `widenDir = "qa/"`. The record path and the
  config path are passed as extras, but a project that keeps go-qa material elsewhere gets no
  widening for its rule sources (see 5.2).

## 2. DBF conformance: are the claims true?

I verified each **Met** row of `docs/CONFORMANCE.md` against code and behaviour:

| Clause  | Claim                                          | Verified                                                                                                 |
| ------- | ---------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| 4.5     | detectors before runners, stop on failure      | `internal/pipeline`, and the consumer trial: a failed `suppression` stops runners (`consumer-trial.log`) |
| 5.2     | listing derived from config                    | `go-qa rules` on the consumer copy listed its 90 `default: all` linters as `project` origin with no edit |
| 5.3     | project defences alongside bundled             | listed (stages, sgconfig rules, driver analyzers via `vet.WithAnalyzers`)                                |
| 6.1-6.3 | record read, justification checked, enumerable | `record check`, `record list`, generic-phrase list in `internal/record/validate.go`                      |
| 6.4     | defaults stated                                | README listing, test-kept                                                                                |
| 7.2     | summary in agent file                          | `AGENTS.md` region, `gqaci.summary.stale`                                                                |

The **Partial/Gap** rows are honest, and `tests/conformance` ties them to `defence-before-fix.toml`.
Two things are *over*-stated and two *under*-stated:

- **High (docs drift, claims stale)** — `docs/analyzers.md:113-115` says "The docs detector does
  not know the registered analyzers, so a project page for an analyzer ... is reported as an
  orphan" and `docs/astgrep.md:195-198` says "A project rule's identifier does not yet resolve ...
  the docs detector does not take its identifiers from it yet". Both are false since M12/M14:
  `internal/docs/project.go:38,54-61,72-86` enumerates project ast-grep rules and driver analyzers,
  and CONFORMANCE 4.2 claims exactly that as test-enforced. The per-tool "Recorded gaps" sections
  and CONFORMANCE disagree; the declaration test only reconciles clause status, not these texts
  (CONFORMANCE itself records that limit). **Fix:** delete the two stale bullets; add a test that
  every "Recorded gaps" bullet in `docs/*.md` is either reproduced by a failing probe or carries a
  reference to the gap record, or at minimum regenerate these sections from the declaration.
- **Medium (overstated)** — README table row `ast-grep`: "then the bundled `gqaci.ast.*` pack";
  `internal/astgrep/pack.go:26-27`: "Pack is the bundled rule pack: a few single-file syntactic
  rules". The pack directory holds only a README; `Pack.Empty()` is true. CONFORMANCE 4.2 says
  "the bundled gqaci.ast.\* rules have no pages", which reads as if rules exist. **Fix:** say "the
  pack is empty" in README and the doc comment, or ship rules (section 3).
- **Medium (understated gap)** — the self-gate never exercises the `ast-grep` path at all:
  `selfgate.log` shows `ast-grep skipped (no sgconfig.yml and the bundled pack is empty or switched off)`. So "8.2 Partial: ... only the golangci-lint rules are proven active" is true but
  the ast-grep binary could be absent from the runner forever and `go-qa run --ci` would stay
  green. The `tests/astgrep` tests `t.Skipf` without the binary (`tests/astgrep/astgrep_test.go:32`),
  and `mutants_gremlins_test.go:23` likewise, while a missing golangci-lint fails the suite. The
  "a missing tool is never a pass" rule is applied to one tool and not the others. **Fix:** make
  the ast-grep and gremlins tests fail (not skip) when the binary is absent unless an explicit
  `GQACI_TEST_WITHOUT_TOOLS=1` opt-out is set, and ship at least one bundled ast-grep rule so the
  self-gate runs the binary.
- **Medium (understated gap, time bomb)** — the three self-gate record entries have
  `review_by = 2026-11-13` (`qa/record.toml`), the 30-day horizon of the `claude-sonnet` medium
  tier. On 2026-11-14 the self-gate fails `gqaci.record.expired` on every branch until a human
  re-decides them, and the medium tier is already at `max_count = 3`, so no further model-authored
  exception is possible. That is the design working, but on an unpublished repo with a placeholder
  decider it means CI goes red before the first tag. **Fix:** have the owner re-sign the three
  entries as `added_by_model = "human"` with the real decider when replacing `example-owner`, or
  record the expiry date in PUBLICATION-CHECKLIST as a step.

### The rule-authoring path, tried as a consumer

I copied `/workspace/src-go` to scratch, made it a git repo, and followed the README
(`consumer-trial.log`):

- `go-qa tools`, `go-qa rules`: work; 102 defences listed from the consumer's own `.golangci.yml`.
- `go-qa run -t record -t suppression -t docs -t summary`: **29 `gqaci.suppression.unrecorded`
  findings on `.golangci.yml`**, one per `linters.disable` item (26), the test-file `goconst+dupword`
  exclusion, `govet.disable::fieldalignment`, `gosec.excludes::G204`. See 4.1: the design
  contradicts README's own principle and the default record budget (`max_total = 10`) makes this
  consumer un-adoptable without first raising budgets.
- `go-qa run -t golangci` with the consumer's config: passes. With the bundled baseline
  (`.golangci.yml` removed): **59 `wrapcheck` + 1 `gosec::G204`** findings. The baseline's
  strictness over the consumer's `default: all` config is entirely `wrapcheck`.
- `go-qa run -t analyzers` (bundled only): passes. With `[analyzers] driver = "./qa/driver"`:
  **FAILS** — `main module ... does not contain package .../qa/driver` because the consumer's
  rules live in a nested module `src-go/qa/go.mod` (the natural place: it keeps
  `golang.org/x/tools` out of the product module). `internal/vet/vet.go:71-94` builds the driver
  with `go build` in the *root* module. **Blocker for Task 3.3** unless the consumer moves its
  rules into the product module or go-qa learns to build a driver from a nested module directory
  (`cmd.Dir` = the driver's own module, `-vettool` path is absolute anyway).
- `go-qa run -t deadcode`: **found a real dead function** the interim gate missed
  (`internal/sessprobe.Unknown`, `collect.go:52`). Value demonstrated.
- `go-qa run -t ast-grep`: skipped (no sgconfig, empty pack) — fine, but see 4.3.
- `go-qa run --changed` after touching `internal/marker/marker.go`: scope correctly
  `./internal/marker` plus its four importers; `golangci`, `analyzers`, `test` ran on them only.
- Record defaults (`strictness.log`): with no `qa/go-qa.toml`, no exception can ever be valid
  (`human` needs a configured decider; any model id needs a configured prefix) and the ratchet
  demands `origin/main`. Fail-closed is right, but the three messages do not say "configure
  `[record] deciders` in `qa/go-qa.toml`" in one place. **Low**: add a first-run hint.
- Offline docs: `go-qa rule-doc gqaci.suppression.unrecorded` prints the bundled page;
  `go-qa docs-check` clean; `rule-doc errcheck` and `rule-doc deadcode` are dangling (recorded gap
  4.2, confirmed).

So: the machinery works end to end on a real consumer; the two walls are the nested-module driver
and the `linters.disable` policy.

## 3. Rule content: what a DBF Go toolchain should ship, and what golangci already covers

Planted module (`strict/mod/main.go`, `strictness.log`) run under the bundled baseline, then under
`default: all`, then under `go-qa -t analyzers`:

| Defect class                                                                 | Baseline catches?                                                                    | `default: all` catches?                                                   | Mechanism to ship                                                                                                                                | Recommendation                                                                                                                                             |
| ---------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| unchecked error incl. `defer f.Close()` on writers                           | **yes** (`errcheck`, no `std-error-handling` preset, `check-blank`)                  | yes                                                                       | nothing                                                                                                                                          | covered                                                                                                                                                    |
| `fmt.Errorf("%v", err)` not `%w`                                             | **yes** (`errorlint`)                                                                | yes                                                                       | nothing                                                                                                                                          | covered                                                                                                                                                    |
| context cancel func dropped                                                  | **yes** (`gosec::G118`)                                                              | yes                                                                       | nothing                                                                                                                                          | covered                                                                                                                                                    |
| sensitive type printed                                                       | **yes** (`gqaci_sensitive_format`, confirmed firing at `main.go:43`)                 | no                                                                        | bundled analyzer                                                                                                                                 | covered; the one rule that justifies the toolchain                                                                                                         |
| `exec.Command` without context                                               | **no** (`noctx` did not fire on `exec.Command` in 2.14.0)                            | no                                                                        | analyzer `gqaci_exec_context` (type-aware: resolve `os/exec.Command`, also `syscall.Exec`/`os.StartProcess` for the audit list)                  | **ship**                                                                                                                                                   |
| `defer` inside a loop                                                        | **no** (`gocritic` `deferInLoop` is an experimental checker, not default)            | no                                                                        | enable `gocritic.enabled-checks: [deferInLoop]` in the baseline, or ast-grep rule                                                                | **ship via baseline setting**                                                                                                                              |
| `init()` side effects                                                        | **no**                                                                               | yes (`gochecknoinits`)                                                    | enable `gochecknoinits` in baseline                                                                                                              | **ship via baseline**; record exceptions where a registry pattern needs it                                                                                 |
| `os.Exit` outside `main`                                                     | **no**                                                                               | no (`revive deep-exit` is not in revive's default set)                    | `revive.rules: [deep-exit]` in baseline, or ast-grep rule                                                                                        | **ship via baseline setting**                                                                                                                              |
| `time.Now`/`time.Since` in `_test.go`                                        | **no**                                                                               | no (`usetesting` covers `os.Setenv`, `context.Background`, not the clock) | analyzer `gqaci_test_clock` (the consumer already wrote `project_testclock`, with a data-table allowance; port it)                               | **ship**, promote from the consumer                                                                                                                        |
| non-deterministic output from `range` over a map                             | **no**                                                                               | no                                                                        | analyzer: `range` over map type whose body writes to an `io.Writer`, `append`s, or calls `fmt.Print*` without a `sort` in the enclosing function | **ship** (both consumer defect lists name it: teams 8.07, sessions 9.14)                                                                                   |
| goroutine leak / unbounded `go` without ctx or WaitGroup                     | **no**                                                                               | no                                                                        | hard in general; a narrow analyzer: `go` statement in a loop with no `sync.WaitGroup`/`errgroup` in scope                                        | defer; record as not attempted                                                                                                                             |
| loop variable capture                                                        | obsolete since Go 1.22; `copyloopvar` flags the *unneeded* copies                    | yes (`copyloopvar`, `intrange`)                                           | enable `copyloopvar`, `intrange`                                                                                                                 | low value; enable for modernisation                                                                                                                        |
| `_ = cancel`, `_ = err` discard                                              | `errcheck check-blank` catches `_ = f()` returning error; `_ = cancel` only via G118 | same                                                                      | nothing                                                                                                                                          | covered                                                                                                                                                    |
| shell-wrapped exec (`sh -c`) to evade G204                                   | `gosec::G204` catches the exec; the `sh -c` shape specifically is not named          | no                                                                        | ast-grep rule `gqaci.ast.shell_exec`: `exec.Command("sh"\|"bash", "-c", ...)`                                                                    | **ship**: this exact evasion happened in M3c                                                                                                               |
| hand-written generated header to dodge linters                               | **yes** (`suppression` tool, `go::generated-header`)                                 | n/a                                                                       | nothing                                                                                                                                          | covered                                                                                                                                                    |
| `t.Skip` without condition                                                   | **no** (recorded gap 4.3)                                                            | no                                                                        | ast-grep rule, or analyzer: `t.Skip*` call not inside an `if`                                                                                    | **ship**; closes a recorded gap                                                                                                                            |
| `//go:build ignore` to hide a file                                           | **no** (recorded gap 4.3)                                                            | no                                                                        | suppression scanner: read build constraints before the package clause                                                                            | **ship**; closes a recorded gap                                                                                                                            |
| printing to stdout from a library                                            | no                                                                                   | yes (`forbidigo` default pattern)                                         | not generic; project `forbidigo` config                                                                                                          | leave to projects                                                                                                                                          |
| struct literal without all fields, naked returns, long functions, complexity | no (`gocognit` default 30 only)                                                      | yes (`exhaustruct`, `nonamedreturns`, `funlen`, `cyclop`)                 | baseline settings                                                                                                                                | style; the consumer's own config lowers `gocognit` to 20 and adds `cyclop 15`, `funlen 80/50`, `nestif 4` — the baseline is looser than its first consumer |

The RESEARCH brief planned a second bundled analyzer, `gqaci_generated_header` (RESEARCH 3.3);
it landed as a scanner route instead, which is fine, but the brief's "the rest of the bundled set
is a later milestone" (`docs/analyzers.md:110`) never got a milestone. **The answer to "are we
building up custom rules?" is no inside go-qa-ci, and yes-but-stranded inside the consumer**
(`src-go/qa/rules/`: `project_seam_exec`, `project_seam_commands`, `project_testclock`,
`project_homeabbrev`, each with `analysistest` fixtures — a better DBF rule set than go-qa-ci ships,
with no record, no docs route and no enumeration because it runs through `scripts/qa-go.bash`).

**Fix (High):** a milestone that (a) raises the baseline with `gochecknoinits`, `copyloopvar`,
`intrange`, `gocritic deferInLoop`, `revive deep-exit`, and tightens `gocognit` to 20; (b) ships
`gqaci_exec_context`, `gqaci_test_clock` (generalised from the consumer, allowances via `-allow`
flag), `gqaci_map_range_order`; (c) seeds the ast-grep pack with `gqaci.ast.shell_exec` and
`gqaci.ast.unconditional_skip` so the pack machinery and self-gate ast-grep path are exercised;
(d) closes the `//go:build ignore` scanner gap. Each with a page and red/green fixture, as the
existing one has.

## 4. Gate strength

### 4.1 `linters.disable` policy is wrong for the stated design (High)

README:116-131 and `configs/golangci.yml:2-3` state: "choosing which linters are enabled is
choosing the defence set; only exclusions of findings are suppressions". But
`internal/suppression/golangci.go:118` makes every `linters.disable` item a suppression route,
and the consumer trial produced 26 such findings for linters (wsl, nlreturn, lll, varnamelen,
exhaustruct...) that the **baseline itself never enables**. With the default record budget
(`max_total = 10`, `max_per_rule = 3`) a project using `default: all` cannot adopt go-qa-ci at
all without first inflating the budgets, which defeats the anti-bloat design. The RESEARCH brief
(3.4) said "linter `disable:` of a default-standard linter" — the narrower rule.
**Fix:** flag `linters.disable::X` only when X is in the baseline's `enable` list or in
`standard` (the defence set go-qa vouches for); treat disabling a linter the baseline does not
enable as choosing the defence set, not suppressing a defence. Keep the `default: X` scalar route.

### 4.2 How strict is the baseline? (Medium)

`default: standard` + 27 linters. `golangci-lint linters --config configs/golangci.yml` leaves
**85 linters disabled** (list in `strictness.log`). Against `default: all` on go-qa-ci's own
source the baseline leaves on the table, after the pure-style ones (wsl, nlreturn, lll,
varnamelen, exhaustruct, mnd, godot, err113, noinlineerr): `paralleltest` 532, `goconst` 138,
`govet` 44 (fieldalignment), `cyclop` 39, `nonamedreturns` 36, `modernize` 35,
`gochecknoglobals` 32, `tagliatelle` 31, `gosec` 26 (the G204 exception), `prealloc` 11,
`musttag` 9, `funcorder` 6, `testpackage` 4, `perfsprint` 2, `funlen` 1. Of these,
`modernize`, `musttag`, `prealloc`, `perfsprint`, `intrange`, `copyloopvar`, `gochecknoinits`,
`usetesting`, `fatcontext`, `containedctx`, `recvcheck`, `nilnesserr`, `iface`, `sloglint`,
`spancheck`, `gocheckcompilerdirectives`, `asasalint`, `bidichk`, `gosmopolitan` are defect-class
linters with low style noise and should be in a "strict" baseline. `revive` and `gocritic` run with
their *default* rule sets (revive: ~25 of 90+ rules; gocritic: no `experimental`/`opinionated`
tags). `wrapcheck` is in the baseline and is the single largest source of noise on the consumer
(59 of 60 findings); it is a style policy, not a defect class, and the first consumer disabled it.
**Fix:** add the defect-class linters above; configure `revive` with an explicit rule list including
`deep-exit`, `unhandled-error`, `defer`, `datarace`, `import-shadowing`, `unconditional-recursion`,
`waitgroup-by-value`, `modifies-value-receiver`, `range-val-address`, `time-equal`,
`unused-receiver`, `useless-break`; enable `gocritic` `diagnostic` tag fully plus `deferInLoop`,
`sloppyReassign`, `truncateCmp`; drop `wrapcheck` to a documented opt-in.

### 4.3 Configured away in the self-gate (as recorded)

- `gosec G204` module-wide, `G306` one file, `govet fieldalignment` — recorded with justifications
  (`qa/record.toml`), honest. G204 module-wide is wide for a tool whose job is exec; a per-file
  scope on `internal/runners/command.go`, `internal/golangci/run.go`, `internal/vet/vet.go`,
  `internal/astgrep/run.go`, `internal/changed/*.go`, `internal/record/gate.go` would hold the rule
  on the rest. **Low.**
- `mutants = false` with reason; gremlins pinned but never run. **Medium**: run it once in
  `diff` mode on the self-gate; the fakes-only proof of `runMutants` is the weakest runner.
- Coverage floor default 80 % statements; the self-gate passes it. Fine. A `--changed` run applies
  the module threshold to the affected packages (recorded).
- `govulncheck` symbol-level, record-licensable per call-site file, stale exceptions reported:
  good. Needs network (recorded); worked here.
- `deadcode` with `-test`: good; skipped for libraries with the right reason.
- The `exhaustive` linter runs with `default-signifies-exhaustive: false` (strict). Good.

### 4.4 Suppression detector coverage (as recorded, plus one)

Recorded gaps (`//go:build ignore`, unconditional `t.Skip`, `-c` config, non-Go ast-grep ignores,
settings outside the closed table) stand. One unrecorded route: **`//nolint` inside a `testdata`
or `vendor` tree is skipped by design, but a project `.golangci.yml` can set `run.tests: false`
(caught) or `exclusions.paths` (caught) — fine; however `linters.settings.<linter>.*` for linters
outside the closed table (e.g. `revive.rules[].arguments` loosening a threshold, `gocognit.min-complexity: 200`,
`cyclop.max-complexity: 500`, `funlen.lines: 10000`) is a silent loosening route the table does not
mention.** `docs/suppression.md:448-449` covers "a setting of a linter not listed" generically;
thresholds specifically should be named. **Low.**

## 5. Performance

### 5.1 Diff mode correctness — good, two gaps

`internal/changed/*.go` read in full and exercised. Correct: changed-against-HEAD plus untracked
(`git diff --name-only --no-renames --relative -z`, `ls-files --others --exclude-standard`), merge
base with `--base`, deleted files kept out of scope, importers via `go list -json` closure
(tests, xtests, embeds, testdata owner), orphan file widens, `go.mod`/`go.sum`/`go.work`/
`tools.lock`/`.golangci.*`/`qa/`/record/config widen.

- **Medium** — rule sources do not widen. A change to `sgconfig.yml` or a rule directory, to
  `docs_dir`, or to the `[analyzers] driver` package (documented default
  `./internal/qa/vetdriver`, outside `qa/`) runs the detector only on the changed files/packages:
  a tightened rule is not re-applied to the module. It worked in the consumer trial only because
  the consumer happens to keep rules under `qa/`. **Fix:** add `sgconfig.yml`, the `ruleDirs`,
  `testConfigs`, `docs_dir` and the driver package directory to `Widen`.
- **Low** — `git merge-base` in `changed/git.go:17` lacks `--end-of-options` (the ratchet's
  `gate.go:160` has it); `--base` is not validated against a leading `-` as `history_base` is
  (`config/load.go:154`). Harmless with `git merge-base`'s option set, inconsistent nonetheless.

### 5.2 Speed and cache

- **Medium** — `test` and `coverage` are two `go test` runs: `runners/testrun.go` with
  `-race -shuffle=on -count=1` (never cached, by design), then `runners/coverage.go` with
  `-covermode=atomic -coverpkg` and **no `-count=1`**, so the coverage run is served from the
  go test cache once warm. Measured on go-qa-ci: `-t test` 126 s, `-t coverage` 3 s (warm), whole
  gate 132-147 s, `golangci` and `analyzers` alone under 1 s warm. Two consequences: the gate's
  cost is the race-enabled suite, and the coverage verdict can come from a cached run that was
  not shuffled or race-enabled, which is a different execution from the one `test` vouched for.
  **Fix:** one `go test -race -shuffle=on -count=1 -covermode=atomic -coverprofile` run feeding
  both verdicts (atomic mode is already chosen "so it is correct under the race detector too",
  `coverage.go:46`); halves the cold cost and makes the two verdicts describe one run.
- Three type-check loads per run (golangci-lint, `go vet -vettool`, `go test`), accepted in
  RESEARCH 3.3. golangci-lint's own cache and the Go build cache are used; `--count=1` is
  deliberate. No go-qa-level cache exists and none is needed.
- `-p <file>` still analyses the whole module for `golangci` and `analyzers` (recorded).
- The record is loaded from disk by each of six tools and `golangci.Config` writes the baseline to
  a temp file per module per tool; negligible.

### 5.3 Tool pinning and supply chain

- `tools.lock` pins deadcode, govulncheck, gremlins (`go install pkg@ver`, sumdb-verified),
  golangci-lint 2.14.0, ast-grep 0.50.0 (sha256 pinned). Good.
- **Medium** — golangci-lint is verified against the checksum file downloaded from the **same
  release** (`install-tools.sh:88-95`); a replaced release asset with a replaced checksums file
  passes. Likewise gitleaks and actionlint in the workflows. Pin the sha256 of each archive in
  `tools.lock [checksums]` as ast-grep already is, and have `--check` compare the on-disk binary's
  hash, not only its `--version` string.
- `GOTOOLCHAIN: local` + `go 1.26.9` in `go.mod` means consumers on an older 1.26 patch with
  `GOTOOLCHAIN=local` cannot `go install` the binary; the usual form is `go 1.26` plus
  `toolchain go1.26.9`. **Low**, intentional per M12 (patched stdlib).
- Release archives linux/amd64 only (recorded).

## 6. Code quality

Good. 22 k lines, every package has a test file, `go vet` clean, own gate green, errors wrapped
with context everywhere, cleanups report their own failures, no swallowed errors except the
designed `os.Stderr` note in temp-dir removal. Architecture is a clean port of rust-qa-ci:
`cli` -> `tools.Executor` (switch on step name) -> engines (`golangci`, `vet`, `astgrep`,
`runners`) -> `finding`, with `record.Matcher` applied uniformly and staleness judged only where a
whole-project view exists. Observations:

- **Low** — `tools.Executor.run` string-switch plus `runNarrowed` string-switch duplicate the
  tool catalogue; a per-tool struct with `run`/`narrow` funcs would remove the chance of a new tool
  missing one of the switches (the `Pending` fallback would then fail loudly, which is at least
  safe).
- **Low** — `internal/tools/golangci.go` `runFmt` in CI snapshots every Go file's hash twice; fine
  for size, but a project of 10 k files pays it on every run.
- **Low** — `internal/vet/vet.go:146` identifies an analyzer by the exact flag usage text
  `enable %q analysis`, an undocumented unitchecker string; a future `x/tools` wording change makes
  every analyzer vanish from the listing. `vet_test.go:120` does pin it against the real binary at
  the pinned `x/tools`, so a bump would fail the suite; what is missing is a runtime guard: a vet
  tool that lists zero analyzers should be an error in `go-qa rules` and `run -t analyzers`
  (rust-qa-ci treats an empty dylint listing as a failure, RESEARCH 1.9).
- Tests: thorough, stdlib only, fixtures in `testdata`; the end-to-end tests run the real
  golangci-lint. The two skip-without-binary tests (section 2) are the one soft spot.

## 7. Publication readiness

Beyond the known owner items (`GQACI_DENYLIST` secret; 46 commits authored and committed as
the personal commit identity — a history rewrite or an accepted identity):

- **Blocker** — no tag exists and README's install line says `@<tag>`; the first consumer cannot
  pin. Tag `v0.1.0` is an owner decision (M13) that has not happened.
- **High** — `qa/go-qa.toml` `deciders = ["example-owner"]` and all three record entries
  `decided_by = "example-owner"`: documented as placeholders, but the ratchet signature is a
  *claim of a human decision* by nobody. Replace before the tag, and re-sign the three entries
  (which also resets the 2026-11-13 expiry).
- **Medium** — the `Public safety` workflow has never run green (secret unset), so the denylist
  scan over history has never executed; PUBLICATION-CHECKLIST step 2 must be run locally against
  the owner's real denylist before flipping visibility.
- **Medium** — the first public tag will carry the `0.0.0-dev` version string (`go-qa --version`
  prints it); no release process stamps a version (recorded 8.1).
- Clean: `.gitignore`, no secrets (gitleaks-shaped patterns absent), neutral fixtures, MIT,
  pinned actions by SHA, `permissions: contents: read`.

## Ranked summary

1. **Blocker** — Adoption path broken for the consumer: nested-module driver unsupported
   (`vet.go:71-94`); `linters.disable` treated as suppressions makes a `default: all` project
   un-adoptable under default budgets (`suppression/golangci.go:118`); no tag to pin.
2. **High** — Rule content: one bundled analyzer, empty ast-grep pack, baseline misses exec
   without context, defer-in-loop, init side effects, os.Exit, test clock, map-range order,
   `sh -c` evasion, unconditional `t.Skip`; the consumer's four analyzers are stranded outside
   go-qa-ci. Ship the milestone in section 3.
3. **High** — Doc drift: `docs/analyzers.md` and `docs/astgrep.md` record gaps the code closed.
4. **Medium** — Self-gate never runs ast-grep; skip-on-missing-binary tests; mutants never run;
   record entries expire 2026-11-13; placeholder decider signs the ratchet.
5. **Medium** — Baseline is "standard + 27", 85 linters off, `wrapcheck` noise, revive/gocritic at
   defaults; the first consumer's own config is stricter.
6. **Medium** — Diff mode does not widen on rule-source changes; coverage re-runs the suite;
   golangci-lint/gitleaks/actionlint checksums trusted from the same release.
7. **Low** — widen dir hardcoded `qa/`, `--base` validation asymmetry, G204 scope, version
   string, first-run record hint.

What is right and should not be touched: pipeline ordering and stop semantics, the record/ratchet
design, the suppression detector's configuration routes, `go vet -json` driving with the analyzer
name printed, symbol-level vuln with per-file licensing, deadcode for mains, the tools.lock +
install script shape, and the honesty of CONFORMANCE.

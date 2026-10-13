# go-qa-ci: design brief

Audience: the agents who will build `LongTermSupport/go-qa-ci` (the repo does not exist yet,
checked with `gh repo view` on 2026-10-13). Source of every claim about the siblings is a
reference clone under `/workspace/untracked/repos/`:

| Repo       | Branch | Commit studied             | Paths below are relative to              |
| ---------- | ------ | -------------------------- | ---------------------------------------- |
| rust-qa-ci | main   | `0aca6d5` (v0.4.2)         | `/workspace/untracked/repos/rust-qa-ci/` |
| ts-qa-ci   | main   | `1f61b30` (v0.1.0)         | `/workspace/untracked/repos/ts-qa-ci/`   |
| php-qa-ci  | php8.5 | `e6e00e8b` (85.7.0 + head) | `/workspace/untracked/repos/php-qa-ci/`  |

Anything not read directly from a file is marked **INFERRED**. A fourth sibling,
`LongTermSupport/py-qa-ci`, exists (public) but was not studied.

rust-qa-ci is the template. ts-qa-ci and php-qa-ci are older and differ in shape (npm package
with phases 0-4; composer plugin with four phases and PHP config); where they disagree with
rust-qa-ci, follow rust-qa-ci unless this brief says otherwise.

---

## 1. The family's common shape

### 1.1 What every member is

One QA entry point for one language, built for the Defence Before Fix (DBF) method. The repo
description of rust-qa-ci states the four deliverables verbatim: "One QA entry point for Rust
workspaces: type-aware bespoke rules, project record, defence listing, offline rule docs"
(`gh repo view` description; also `README.md` lines 1-6). ts-qa-ci and php-qa-ci repeat the DBF
definition in their descriptions and README (`ts-qa-ci/README.md`, `php-qa-ci/README.md`
"Defence Before Fix").

### 1.2 rust-qa-ci directory layout

```
Cargo.toml                    workspace; version 0.4.2; [workspace.metadata.defence-before-fix] conformance claim
rust-toolchain.toml           pinned product toolchain (1.99.0, rustfmt/clippy/llvm-tools)
clippy.toml                   test-only allow switches (each one is in qa/record.toml)
tools.lock                    pinned external tool versions ([prebuilt] / [source] / [bootstrap])
crates/rust-qa/src/*.rs       the binary: main.rs CLI, pipeline.rs phases, tools.rs catalogue+executor,
                              config.rs, record.rs + policy.rs (record + anti-bloat), suppression.rs,
                              defences.rs (listing), docs.rs (resolver), summary.rs, stage.rs, hit.rs,
                              finding.rs, builtin.rs (built-in ids), ast_grep.rs, manifest.rs, date.rs
crates/rust-qa/tests/*_test.rs one integration test file per module; main_test.rs end-to-end
crates/rust-qa/docs/rules/*.md bundled rule pages, compiled in with include_str! (docs.rs BUNDLED)
crates/rust-qa/rules/ast-grep/rqaci.ast.*.yml  bundled syntactic pack
lints/rqaci_lints/            dylint library (type-aware lints), its own workspace + nightly toolchain,
                              UI fixtures ui/, ui_config/ with .stderr expectations
qa/rust-qa.toml, qa/record.toml  the repo gating itself (self-hosting)
scripts/install-tools.sh      installs tools.lock versions (cargo-binstall, never QuickInstall)
scripts/public-safety-scan.sh denylist scan (list lives OUTSIDE the repo)
scripts/secret-scan.sh        gitleaks
.githooks/{pre-commit,commit-msg,pre-push}  run the two scans
.github/workflows/ci.yml      fmt/clippy/test job, self-gate job, actionlint+shellcheck job
.github/workflows/public-safety.yml  gitleaks + denylist on push/PR/issues/comments/weekly
.github/scripts/write-denylist.sh   writes the RQACI_DENYLIST secret to $RUNNER_TEMP
docs/CONFORMANCE.md           clause-by-clause DBF evidence record
docs/PUBLICATION-CHECKLIST.md pre-publication audit
docs/denylist.example         placeholder-only denylist format
```

(from `git ls-files`; module roles from each file's `//!` header.)

### 1.3 The single entry point and its CLI

`rust-qa` (`crates/rust-qa/src/main.rs`, clap). Global `--root` (default: `cargo locate-project --workspace`) and `--config` (default `qa/rust-qa.toml`).

| Command                                                         | What                                                                    | Source                                      |
| --------------------------------------------------------------- | ----------------------------------------------------------------------- | ------------------------------------------- |
| `run [-t tool]... [-p path]... [--ci] [--no-fix] [--fail-fast]` | the gate                                                                | `main.rs` `Cmd::Run`, `run()`               |
| `tools`                                                         | catalogue + project stages, phase, enabled/disabled                     | `main.rs` `Cmd::Tools`                      |
| `rules [--json]`                                                | every active defence + the project record + budget, derived from config | `main.rs` `rules()`, `defences.rs`          |
| `rule-doc <id>`                                                 | offline docs for any printed id                                         | `main.rs` `rule_doc()`, `docs.rs`           |
| `docs-check`                                                    | fail if a listed id has no docs                                         | `main.rs` `docs_check()`                    |
| `record check\|list\|prune [--dry-run]\|explain <rule\|n>`      | record admin                                                            | `main.rs` `record_command()`                |
| `summary [--check]`                                             | regenerate/check the agent summary region                               | `main.rs` `summary_command()`, `summary.rs` |

`--ci` is also implied by a non-empty `CI` env var that is not `false`/`0` (`main.rs` dispatch).
CI mode = autofix, then fail if `git status --porcelain` shows the tree changed
(`tools.rs` ~line 1232-1242).

### 1.4 Phases, tools and order

`pipeline.rs`: three phases `format` → `detectors` → `runners`. Every selected detector runs
(one invocation reports all static findings); any format/detector failure stops the run before
the first runner; runners stop at first failure; `--fail-fast` stops at the first failure
anywhere. Within a phase: built-ins in catalogue order, then project stages in declared order.
Catalogue (`tools.rs` `Tool::ALL`, README "What it runs"):

| Phase     | `-t`          | What                                                                        |
| --------- | ------------- | --------------------------------------------------------------------------- |
| format    | `fmt`         | `cargo fmt --all` (`--check` when `--no-fix`)                               |
| detectors | `record`      | record validation + anti-bloat (`rqaci.record.*`)                           |
|           | `suppression` | unrecorded suppressions; `#[allow]` forbidden even when recorded            |
|           | `summary`     | agent summary region freshness                                              |
|           | `docs`        | dangling ids / orphan pages                                                 |
|           | `ast-grep`    | project `sgconfig.yml` rules, then bundled `rqaci.ast.*` pack (second scan) |
|           | `clippy`      | `-D warnings`, Cargo `[lints]` tables, `--message-format=json`              |
|           | `dylint`      | type-aware libraries from `[workspace.metadata.dylint]`                     |
| runners   | `deny`        | cargo-deny (advisories, bans, licences, sources)                            |
|           | `audit`       | cargo-audit, off by default                                                 |
|           | `machete`     | unused deps                                                                 |
|           | `test`        | nextest then doctests                                                       |
|           | `coverage`    | cargo-llvm-cov `--fail-under-lines`                                         |
|           | `mutants`     | cargo-mutants `off\|diff\|full`                                             |

`--locked` is passed when a lock file exists (README "What it runs"). `-p` scoping: per-crate
engines analyse the owning crate and filter diagnostics to the paths (README; CONFORMANCE 4.4).
"Further workspaces" (`workspaces = [...]`) re-run fmt/clippy/dylint/test in nested workspaces
with their own toolchain (README "Further workspaces").

Siblings: ts-qa-ci has phases 0-4 (fast-fail, code modification, lint, static analysis,
testing), stops at the first failing phase unless `--aggregate` (`ts-qa-ci/docs/pipeline.md`).
php-qa-ci has four phases (coding standards, linting, static analysis, testing), fail-fast
when writable, aggregate when read-only (`php-qa-ci/docs/pipeline.md`).

### 1.5 Config the consumer provides

- `qa/rust-qa.toml`, optional, **unknown keys rejected** (README "Configuration"; full default
  listing there): `record`/`[record]`, `docs_dir = "docs/defences"`, `scan_exclude`,
  `workspaces`, `[summary] file/fix_headings`, `[tools]` on/off switches per tool, `[ast_grep] bundled`, `[dylint] force_warn`, `[[stage]]`, `[[resolver]]`, `[coverage] fail_under_lines = 80`, `[mutants] mode/base`.
- `qa/record.toml`, the project record (1.7).
- Language-native lint config that the tool reads and lists: Cargo `[lints]` tables,
  `[workspace.metadata.dylint] libraries`, `sgconfig.yml`, `deny.toml`, `clippy.toml`,
  `dylint.toml` (README; `defences.rs` header).
- `docs/defences/<id>.md` for each project rule (README "Writing rules").
- A tool whose own config is absent reports itself as skipped (README end of "Configuration").

Project stages (`stage.rs`, README "Project stages"): `[[stage]] name, phase, says, fix, command, files_command, files, shebangs, exclude, dir, output = status|findings, rules, summary_rules`.
`output = "findings"` means the command prints JSON
`[{"file","line","column","rule","message","note"}]` and rust-qa applies the record to it.
`[[resolver]] matches = "SC[0-9]*", text = "..."` documents codes no listing names.

### 1.6 Output format and exit codes

Human, compiler-style (`finding.rs`, `hit.rs` `Display`):

```
error[rqaci.record.stale]: <message>
  --> path/to/file.rs:12:5
  = note: <how to fix, when the detector says>
  = doc: rust-qa rule-doc rqaci.record.stale
```

A run ends with a per-step table `phase  name  passed|FAILED|skipped (reason)`, "detectors failed:
runners were not started" when applicable, then the DBF pointer (`lib.rs` `DBF_POINTER`):
"Defence Before Fix: each failure names a rule id; run `rust-qa rule-doc <id>` for the correct
construction. Method: https://defence-before-fix.github.io/".

Machine-readable: `rust-qa rules --json` (`{defences:[{id,engine,level,origin,description,doc}], record:[...], budget:{...}}`, `main.rs` `rules()`), and the stage findings JSON *input* contract.
rust-qa has **no JSON form of a `run`**. ts-qa-ci has `--json` (whole PipelineResult) and `--llm`
(compact summary + `node_modules/.cache/ts-qa/llm/last-run.json`, auto-on for agent env vars)
(`ts-qa-ci/docs/pipeline.md` "Output modes"); php-qa-ci has `--agent-mode` (counts on stdout,
per-file JSON under `var/qa/`) (`php-qa-ci/docs/agent-mode.md`).

Exit codes, rust-qa (`main.rs` `main()`): **0** pass, **1** a defence failed, **2** error
(config, IO, usage, a tool that could not run). ts-qa `rule`: 0/1/2 likewise
(`ts-qa-ci/docs/pipeline.md`). php-qa-ci adds **75** "another run holds the lock"
(`php-qa-ci/docs/pipeline.md` "Exit codes").

### 1.7 The project record

`qa/record.toml` is the only place an exception lives ("nothing else can silence a defence",
README "Project record"). Schema: `[ratchet] high_water, decided_by, decided_on, reason`;
`[[exception]] rule, path, justification, decided_by, added_by_model, decided_on, review_by, policy?, covers?`.

- `rule` is the id exactly as printed; `path` one file (a glob needs `policy = true`).
- Generic/short justifications rejected (`record.rs` `GENERIC` list of 22 phrases,
  `PLACEHOLDERS`).
- Stale exceptions fail (`rqaci.record.stale`); `record prune` removes them and lowers the ratchet.
- Two exception kinds: covers a *suppression written in source* (default: a recorded
  `#[expect(lint, reason)]`), or `covers = "findings"`: covers findings of detectors rust-qa
  applies the record to itself (dylint, ast-grep, findings-stages) (`hit.rs` `apply`, `unused`).
- Anti-bloat (README "Anti-bloat controls", `policy.rs`): budgets (`max_total`, `max_per_rule`,
  `max_policy`, per-tier `max_count`), provenance, review horizon per trust tier (model id
  prefix → `high|medium|low`, `human` must be a configured decider), co-sign categories, expiry,
  and the signed **ratchet** compared at `HEAD` and the merge base with `history_base`
  (a raise needs a fresh signature ≤14 days old; CI fails until a lowered ceiling is committed).
- Self-hosted example: `qa/record.toml` (5 entries, all `clippy.toml::allow-*-in-tests` +
  one `clippy::struct_excessive_bools`).

ts-qa-ci's record is `tsQaConfig/tier-a-exemptions.json` (`ruleId, files, justification`)
(`ts-qa-ci/docs/configuration.md`); php-qa-ci's is `qaConfig/phpstan.neon` `ignoreErrors` with a
justifying comment, plus `qaConfig/qa.php` and `composer.json extra.defence-before-fix`
(`php-qa-ci/docs/defence-before-fix-defaults.md`). rust-qa-ci's is the strictest; copy it.

### 1.8 The suppression guard

`suppression.rs` + `docs/rules/rqaci.suppression.unrecorded.md`: finds **every route** that
silences a defence and requires a matching record entry: lint attributes (incl. `cfg_attr`,
macro bodies, `include!`d files), hand-written `#[automatically_derived]`, `ast-grep-ignore`
comments in every language a project rule reads, `allow` levels in Cargo `[lints]`, `-A` /
`--cap-lints` in `.cargo/config.toml` rustflags, `allow-*` in `clippy.toml`, `deny.toml`
advisory ignores, and an unparseable file. `#[allow]` additionally fails
`rqaci.suppression.allow` even when recorded (only self-expiring `expect` is acceptable).
Suppressions that exist only after macro expansion are caught by the type-aware lint
`rqaci_suppression_generated`. This is how rust-qa meets TOOLING-SPEC 4.3.

### 1.9 The defence listing

`rust-qa rules` (`defences.rs`): every active defence **derived from configuration** (never a
hand list): built-in `rqaci.*` ids (`builtin.rs` `ALL`), rustc/Clippy/rustdoc levels from Cargo
`[lints]` (descriptions from `-W help`), dylint lints (listed by loading the libraries; empty
listing for a configured library is a failure), ast-grep rule dirs + bundled pack, project
stages and the rules their `rules` command prints, and runner gates; plus the record and its
budget. Columns: id, engine, level, origin, description; doc route `rust-qa rule-doc <id>`.
The same data feeds the agent summary (`summary.rs`): a region between
`<!-- BEGIN rust-qa-ci generated defences ... -->` / `<!-- END ... -->` in a configured file
(e.g. `AGENTS.md`), freshness enforced by `rqaci.summary.stale`.

### 1.10 Type-aware bespoke rules

`lints/rqaci_lints/` (dylint, late lint passes with full rustc type info,
`lints/rqaci_lints/README.md`). Two bundled lints:

- `rqaci_sensitive_debug_derive` (`src/sensitive_debug_derive.rs`, doc
  `crates/rust-qa/docs/rules/rqaci_sensitive_debug_derive.md`): derived `Debug` on a type that is
  (or transitively holds, through aliases/generics) a sensitive-named type, unless redacting
  types; configurable via `dylint.toml`. The doc has a "Why type-aware" section.
- `rqaci_suppression_generated`: lint attributes assembled by workspace macros.

Proof: UI fixtures `ui/<lint>/main.rs` + expected `main.stderr`, and `ui/clean/` (red and green).
The lint library version equals the release version, enforced by a test, because dylint caches
by name+version (`lints/rqaci_lints/README.md`). Consumers load it by git tag in
`[workspace.metadata.dylint]` (README "Installing"). Every dylint finding blocks at any level
unless the record covers it; `force_warn = true` stops in-code levels lowering it; a finding with
no lint name fails `rqaci.dylint.nameless`.

ts-qa-ci's type-aware rules are ESLint rules using typescript-eslint parser services
(`ts-qa-ci/src/rules/exhaustiveDiscriminated.ts`); php-qa-ci's are PHPStan rules
(`php-qa-ci/rules-default.neon`, `bin/phpstan-rule` harness).

### 1.11 Offline rule docs

`docs.rs` resolution order, all offline: project page `<docs_dir>/<id>.md` (or a lint-name
variant: case-insensitive, `-`≡`_`, `::`→`.`), a project stage's `says`/`fix`, bundled page
(compiled into the binary), `cargo clippy --explain`, `rustc --explain` / `rust-docs` component /
`rustc -W help`, then `[[resolver]]` globs. Every bundled page must have a "How to fix
correctly" section (test-enforced; `docs/CONFORMANCE.md` 8.1), and the page set is closed in both
directions: `rqaci.docs.dangling` (listed id without a page) and `rqaci.docs.orphan` (page with no
listed id; `README.md` exempt). Page template (see `docs/rules/rqaci.suppression.unrecorded.md`):
`# <id>`, What it flags, Why, How to fix correctly, Why syntax suffices / Why type-aware,
Configuration, Suppression.

### 1.12 DBF: what a qa-ci repo must provide

Specs are at https://defence-before-fix.github.io/ (method `SPEC.html`, `DETECTOR-SPEC.html`,
`TOOLING-SPEC.html`); php-qa-ci vendors them under
`php-qa-ci/remote-docs/defence-before-fix.github.io/` with provenance frontmatter. rust-qa-ci
declares method 1.1.0, detector 1.0.0, toolchain 0.2.0 (`Cargo.toml`); check the live site for
newer versions at build time. TOOLING-SPEC MUSTs (section headings read from the vendored copy)
and the go-qa-ci answer:

| Clause           | Requirement                                                                                                                                                                                   | go-qa-ci must provide                                                                                                    |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| 4.1              | every routed detector conforms to DETECTOR-SPEC (bespoke rules, single-rule harness, stable id printed with every finding, local/subset/in-output reporting, id→docs, suppression detectable) | own `go/analysis` driver (3.3) + golangci-lint routed through the wrapper (record the gap like rust's Clippy note)       |
| 4.2              | every printable id resolves offline; docs state the correct construction                                                                                                                      | `go-qa rule-doc`, embedded pages, `staticcheck -explain`, `golangci-lint help linters`, analyzer `Doc` via driver `help` |
| 4.3              | forbid every suppression route bypassing the record                                                                                                                                           | `gqaci.suppression.*` detector (3.4)                                                                                     |
| 4.4              | entry point local, subset-capable (one file), ids unaltered                                                                                                                                   | `go-qa run -p file.go`                                                                                                   |
| 4.5              | detectors before runners; stop on detector failure                                                                                                                                            | port `pipeline.rs`                                                                                                       |
| 5.1-5.3          | list defences without running them, derived from config, project defences included                                                                                                            | `go-qa rules [--json]`                                                                                                   |
| 6.1-6.3          | record location defined and read by the tool; justification required, generic rejected; record enumerable like defences                                                                       | `qa/record.toml`, `go-qa record list`, record in `rules`                                                                 |
| 6.4 (SHOULD)     | state own defaults                                                                                                                                                                            | README "Configuration"                                                                                                   |
| 7.1-7.2 (SHOULD) | agent summary generated into the project                                                                                                                                                      | `go-qa summary`                                                                                                          |
| 8.1              | release fails if a bundled defence lacks docs                                                                                                                                                 | Go test over embedded pages + `docs-check` in self-gate                                                                  |
| 8.2              | toolchain runs its own defences on its own source                                                                                                                                             | self-gate job `go-qa run --ci`                                                                                           |
| 9.1-9.2          | conformance declared at two levels, with known gaps                                                                                                                                           | machine-readable declaration + `docs/CONFORMANCE.md`                                                                     |

The DBF tool register grades the three Go detectors go-qa-ci will route
(https://defence-before-fix.github.io/tools/ `go-vet.html`, `golangci-lint.html`,
`staticcheck.html`, fetched 2026-10-13):

- **go vet**: 4.1 yes (`-vettool`, `singlechecker`/`multichecker`), 4.2 yes (`analysistest`),
  **4.3 partial: plain output does not print the analyzer name**, 6.1-6.3 yes (`go tool vet help <name>`, `Doc`), 7.1/7.2 yes by omission (no inline suppression exists).
- **golangci-lint v2**: 4.1 yes but only via module plugins + a custom-built binary
  (`golangci-lint custom`, `.custom-gcl.yml`); 4.3 partial (one name per plugin linter; wrapped
  codes like `SA1019` not resolved); 6.1-6.3 partial (wrapped tools' docs are web-only); 7.1/7.2
  met only with `nolintlint` `require-explanation` (+ `require-specific`) enabled.
- **staticcheck**: 4.1 **no** (no bespoke rules); 6.1-6.3 yes (`staticcheck -explain`);
  **7.1 no: `//lint:ignore` / `//lint:file-ignore` cannot be disabled or detected**; the page
  advises forbidding them with a bespoke analyser.

Verified locally (go1.26.9, scratch module, deleted after): `go vet -json ./...` emits
`{"<pkg>":{"<analyzer>":[{"posn","end","message"}]}}` and **exits 0** with findings; plain
`go vet` exits 1 and prints `main.go:6:14: <message>` with **no analyzer name**. So go-qa must use
`-json`, render its own `error[<id>]` lines and decide pass/fail itself.

Conformance declaration location: rust `Cargo.toml [workspace.metadata.defence-before-fix]`;
ts `package.json "defenceBeforeFix"` with top-level + nested `project` levels and `knownGaps`
(`ts-qa-ci/docs/pipeline.md` "The conformance declaration"); php `composer.json extra.defence-before-fix`. `go.mod` has no metadata table, so **INFERRED**: put it in a root
`defence-before-fix.toml` (artefact level + `[project]` level, each with `gaps`), read by a Go
test, and evidenced in `docs/CONFORMANCE.md`.

---

## 2. Adoption, pinning and self-testing

### 2.1 How a consumer adopts rust-qa-ci (README "Installing")

1. `cargo install --locked --git https://github.com/LongTermSupport/rust-qa-ci --tag v0.4.2 rust-qa`
2. `git clone --depth 1 --branch v0.4.2 .../rust-qa-ci /tmp/rust-qa-ci && /tmp/rust-qa-ci/scripts/install-tools.sh`
   (installs exact `tools.lock` versions; `--check` reports drift; `tool...` limits the set).
3. Add the bundled dylint library at the **same tag** in `[workspace.metadata.dylint]`.
4. Optional `qa/rust-qa.toml`, `qa/record.toml`, `docs/defences/`, `sgconfig.yml`, `[[stage]]`s.
5. Run `rust-qa run` locally; `rust-qa run --ci` (or with `CI` set) in CI.

Pinning: **exact git tag** everywhere (binary, tools via tools.lock at that tag, lint library).
Tags `v0.1.0` … `v0.4.2`, no GitHub Releases, no release automation yet (`Cargo.toml` gap "8:
... no tagged-release automation yet"). No package registry (`publish = false`).

Siblings: ts-qa-ci is an npm devDependency pinned to a commit SHA
(`github:LongTermSupport/ts-qa-ci#<sha>`), with committed `dist/`, `npx ts-qa init` scaffolding
`tsQaConfig/` + `.github/workflows/ts-qa.yml` (`ts-qa-ci/docs/github-actions.md`), one tag
`v0.1.0`. php-qa-ci is a composer plugin (`composer require --dev lts/php-qa-ci:~85.1.0`), tags
`<phpline>.<minor>.<patch>` (`85.7.0`), changelog-driven release PRs (`.github/workflows/release.yml`,
README "Branches and versions"), and auto-deploys Claude skills/hooks on composer install
(README "Claude Code Integration").

### 2.2 How rust-qa-ci tests itself

- Unit/integration tests: `crates/rust-qa/tests/*_test.rs`; end-to-end in `main_test.rs`
  (builds fixture workspaces in tempdirs, runs the binary, asserts listing/rule-doc/docs-check/
  unrecorded-suppression-stops-runners).
- Lint library: dylint UI tests (`lints/rqaci_lints/tests/*`, `ui/`, `ui_config/`).
- Bundled ast-grep rules: docs + fixtures proving each fires and stays quiet
  (`tests/ast_grep_test.rs`).
- CI (`.github/workflows/ci.yml`): job `rust` (fmt --check, clippy -D warnings, test --locked);
  job `self-gate` (fetch-depth 0 for the ratchet, install lint nightly, `scripts/install-tools.sh`,
  `cargo install --path crates/rust-qa`, **`rust-qa run --ci`**); job `workflows` (actionlint
  checksum-verified, shellcheck). Actions pinned by commit SHA; `permissions: contents: read`.
- Public safety (`.github/workflows/public-safety.yml`): gitleaks over history+tree; denylist
  scan over tree, full history, PR title/body, maintainers' issue/comment text; weekly schedule;
  fails if secret `RQACI_DENYLIST` is missing (fork PRs excepted).
- Local hooks: `git config core.hooksPath .githooks`.

ts-qa-ci dogfoods via `npx ts-qa --write` and an auto-fix-and-commit CI job
(`ts-qa-ci/.github/workflows/ci.yml`); php-qa-ci via `ci.bash` (README "CI/CD Workflows").
Prefer rust's read-only `--ci` self-gate (no bot commits).

---

## 3. Mapping to Go

### 3.1 Component map

| rust-qa-ci                                           | go-qa-ci equivalent                                                                                                                                        | Notes                                                                                                         |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| binary `rust-qa` (clap)                              | binary `go-qa`, `cmd/go-qa`                                                                                                                                | stdlib `flag` with hand dispatch is enough; same subcommands and flags                                        |
| root discovery `cargo locate-project`                | `go env GOWORK` then `go env GOMOD`                                                                                                                        | a `go.work` root is the workspace                                                                             |
| `rust-toolchain.toml`                                | `go`/`toolchain` directives in `go.mod`; run with `GOTOOLCHAIN` unset                                                                                      |                                                                                                               |
| `--locked`                                           | `GOFLAGS=-mod=readonly` (Go default for build) + `go mod verify`                                                                                           |                                                                                                               |
| "further workspaces"                                 | `modules = [...]`: nested Go modules (not in go.work) checked in their own dir                                                                             |                                                                                                               |
| `fmt` (`cargo fmt`)                                  | `golangci-lint fmt` (v2 formatters: `gofumpt` + `goimports`); `--diff` for check                                                                           | one pinned binary for format and lint                                                                         |
| `clippy -D warnings` + Cargo `[lints]`               | `golangci-lint run` v2 with consumer `.golangci.yml`, JSON output (`--output.json.path=stdout`)                                                            | ids: `<linter>` or `<linter>::<code>` (e.g. `staticcheck::SA1019`, `gosec::G402`) parsed from the issue       |
| rustc lints                                          | `govet` inside golangci (`enable-all`, documented disables)                                                                                                | don't run vet twice                                                                                           |
| `dylint` (type-aware bespoke)                        | **`analyzers`**: go/analysis analyzers via `go vet -vettool=<driver> -json` (3.3)                                                                          |                                                                                                               |
| bundled `rqaci_lints`                                | public package `analyzers/...` with bundled `gqaci_*` analyzers                                                                                            | same module → version can never drift (no dylint cache problem)                                               |
| dylint UI tests                                      | `golang.org/x/tools/go/analysis/analysistest` with `testdata/src/...` + `// want`                                                                          | red and clean fixtures                                                                                        |
| `ast-grep` + bundled pack                            | `ast-grep` (Go grammar supported) on project `sgconfig.yml`; bundled `gqaci.ast.*` pack only for checks no enabled linter covers                           | gosec already covers TLS skip-verify (G402) and much of shell exec (G204); **INFERRED**: pack may start empty |
| `record`, `suppression`, `summary`, `docs`           | port 1:1                                                                                                                                                   | Go suppression routes in 3.4                                                                                  |
| `deny` (cargo-deny)                                  | `vuln`: `govulncheck -format json ./...` (on by default; reachability-based) + bans/sources via golangci `gomodguard`/`depguard` config                    | licence check (`go-licenses check`) as an opt-in `licences` runner, off by default (YAGNI)                    |
| `audit`                                              | folded into `vuln`                                                                                                                                         |                                                                                                               |
| `machete`                                            | `mod`: `go mod tidy -diff` (Go ≥1.23) + `go mod verify`                                                                                                    |                                                                                                               |
| `test` (nextest + doctests)                          | `go test -race -shuffle=on -count=1 ./...` (examples are tests already)                                                                                    |                                                                                                               |
| `coverage` (llvm-cov lines)                          | `go test -covermode=atomic -coverpkg=./... -coverprofile`, total from `go tool cover -func`; key `fail_under_statements = 80`                              | Go measures statements, not lines                                                                             |
| `mutants`                                            | `gremlins` (go-gremlins) `off\|diff\|full`, default off                                                                                                    | **INFERRED**: confirm gremlins' diff mode flag before building                                                |
| `deadcode` (none in Rust)                            | `golang.org/x/tools/cmd/deadcode` as a detector for modules with `main` packages; skipped for libraries (`unused` linter covers those)                     |                                                                                                               |
| `tools.lock` + `install-tools.sh`                    | same; Go-built tools via `go install pkg@ver` (sum.golang.org verified); golangci-lint, ast-grep, gitleaks, actionlint via release tarball + checksum file | golangci-lint upstream discourages `go install` builds                                                        |
| `Cargo.toml [workspace.metadata.defence-before-fix]` | `defence-before-fix.toml` (**INFERRED** location)                                                                                                          |                                                                                                               |
| `include_str!` bundled docs                          | `//go:embed` (pages must live in or below the embedding package dir)                                                                                       |                                                                                                               |

Built-in identifiers: rename `rqaci.` → **`gqaci.`** (`gqaci.record.invalid`,
`gqaci.suppression.unrecorded`, `gqaci.docs.dangling`, …). Analyzer names: `gqaci_<name>`
(Go analyzer names must be identifiers, mirroring dylint's `rqaci_` lints). Keep project ids out
of the `gqaci` namespace.

### 3.2 Strict golangci-lint v2 baseline

Current releases (GitHub API, 2026-10-13): golangci-lint v2.14.0, staticcheck 2026.2.2, gofumpt
v0.12.0, gosec v2.29.0, govulncheck (golang/vuln) v1.9.0, x/tools v0.52.0. Go in container:
1.26.9.

Ship `configs/golangci.yml` (embedded; `go-qa` writes it only when asked, e.g. a later
`go-qa init`). Proposal (finalise in milestone M5):

- `version: "2"`, `linters.default: standard` plus an explicit `enable` list: `bodyclose`,
  `contextcheck`, `durationcheck`, `errchkjson`, `errname`, `errorlint`, `exhaustive`,
  `forcetypeassert`, `gocritic`, `gosec`, `nilerr`, `nilnil`, `noctx`, `nolintlint`, `predeclared`,
  `revive`, `rowserrcheck`, `sqlclosecheck`, `testifylint`, `thelper`, `tparallel`, `unconvert`,
  `unparam`, `usestdlibvars`, `wrapcheck`, `misspell`, `gocognit` (threshold), `depguard` (rules
  empty by default).
- `errcheck`: `check-type-assertions: true`, `check-blank: true`.
- `govet`: `enable-all: true` (`fieldalignment` off, with the reason on the setting).
- `nolintlint`: `require-explanation: true`, `require-specific: true`, `allow-unused: false`
  (an unused `//nolint` then fails, which is Go's equivalent of Rust's self-expiring `expect`).
- `issues`: `max-issues-per-linter: 0`, `max-same-issues: 0` (non-zero caps hide findings).
- `formatters.enable: [gofumpt, goimports]`.

Decision to record in the README: choosing which linters are *enabled* is the defence set (like
choosing Clippy groups). Only *exclusions* of findings are suppressions (3.4).

### 3.3 Type-aware bespoke rules: recommendation

Options:

- **A. golangci-lint module plugins.** The consumer lists analyzer modules in `.custom-gcl.yml`
  and runs `golangci-lint custom` to build a custom binary. One package load (fast), and `//nolint`
  applies to them.
- **B. Own go/analysis driver, run via `go vet -vettool`.** go-qa-ci ships a public `driver`
  package (`driver.Main(extra ...*analysis.Analyzer)` wraps `unitchecker.Main` with the bundled
  analyzers prepended). Without project analyzers, `go-qa` acts as the vettool itself: it re-execs
  `go vet -vettool=$(os.Executable()) -json` with an env marker (e.g. `GQACI_VETTOOL=1`) that makes
  `main` call `unitchecker.Main`. With project analyzers, the consumer keeps a ~5-line `main`
  package (e.g. `internal/qa/vetdriver`) calling `driver.Main(myAnalyzers...)`, names it in
  `[analyzers] driver = "./internal/qa/vetdriver"`, and go-qa builds it with `go build`.

**Recommend B**, for these reasons:

1. **The identifier is always present.** `go vet -json` keys every diagnostic by analyzer name
   (verified above), so each finding prints `error[gqaci_x]`. That closes the DETECTOR-SPEC 4.3
   gap the register records for both go vet (plain output) and golangci plugins (one name per
   plugin).
2. **No inline suppression route.** go vet has no `//nolint`, so the only exception is a
   `covers = "findings"` record entry that go-qa applies itself. This is exactly rust's dylint +
   `force_warn` model (`hit.rs`), and 4.3 holds by construction.
3. **No custom golangci build per consumer.** The bundled analyzers ship in the same module and
   tag as the binary, so `go install …@vX` is the whole install.
4. **Listing and docs come offline from the driver itself.** unitchecker's `-flags` prints the
   analyzer flags as JSON, which enumerates the analyzers (5.1-5.3). The driver's `help <name>`
   prints the `Doc` (6.1-6.3). **INFERRED**: confirm both against x/tools v0.52.0 in M7a.
5. **Proof is the standard harness:** `analysistest.Run(t, testdata, Analyzer, "a")` with
   `// want` comments, for red and clean fixtures (DETECTOR-SPEC 4.2).
6. **Decoupled** from golangci-lint's plugin API and release cadence.

The cost is a second package load in the detectors phase, which is acceptable. Treat this as a
default, not a ban: a consumer who already has golangci plugins can still run them through
`.golangci.yml`, and go-qa lists them like any other linter.

Bundled analyzers (analogues of `rqaci_lints`):

- `gqaci_sensitive_format` (analogue of `rqaci_sensitive_debug_derive`). A value whose resolved
  type is, or transitively holds, a sensitive-named struct is passed to a `%v`/`%+v`/`%#v`/`%s`
  verb in `fmt.*`, to `log`/`log/slog` attributes, or to `errors`/`fmt.Errorf`. It fires only
  when the type implements none of `fmt.Stringer`, `fmt.GoStringer`, `fmt.Formatter` or
  `slog.LogValuer`. Type-aware because aliases, embedding, generics and method sets decide it.
  Configured through analyzer flags (patterns, redacting types). **INFERRED** design: it mirrors
  the Rust lint's doc, and the details are for M7b.
- `gqaci_generated_header`: a `// Code generated … DO NOT EDIT.` header in a file that no
  `//go:generate` directive or configured generator glob accounts for. golangci excludes
  generated files by default, so a hand-written header is a suppression route, the Go twin of a
  hand-written `#[automatically_derived]`. It could live in the suppression detector instead,
  since syntax suffices; decide in M6.

### 3.4 Go suppression routes the `suppression` detector must cover

Each of these needs a matching record exception (`gqaci.suppression.unrecorded`). Unparseable
`.go` files fail.

- `//nolint` / `//nolint:a,b // reason`. A bare `//nolint`, or one without a reason, also fails
  `gqaci.suppression.bare` (the analogue of `rqaci.suppression.allow`), even when recorded.
  nolintlint enforces the same at lint time.
- Staticcheck `//lint:ignore` and `//lint:file-ignore` (the register says these cannot be
  disabled, so go-qa must catch them). Same for `staticcheck.conf` `checks = ["-…"]` in any
  directory.
- gosec `#nosec` / `// #nosec Gxxx`.
- `.golangci.yml`: `linters.exclusions.rules`, `.paths`, `.presets`, `exclusions.generated`
  other than the default, `severity` downgrades, `issues.max-*` above 0, `new`/`new-from-rev`/
  `new-from-merge-base`/`new-from-patch`, `run.tests: false`, linter `disable:` of a
  default-standard linter. Per-linter settings that switch a check off (`errcheck.exclude-functions`,
  `gosec.excludes`, `revive` rule `disabled: true`, `govet.disable`) count too.
- `ast-grep-ignore` comments (as rust).
- `t.Skip` without a condition. **INFERRED** candidate; decide in M6, since it may belong to the
  bundled analyzers rather than the suppression scanner.
- A hand-written generated header (above).

Rule ids recorded for these follow rust's pattern, e.g. `golangci.yml::exclusions.rules`
(compare `clippy.toml::allow-unwrap-in-tests` in rust's `qa/record.toml`).

---

## 4. Proposed repo layout and build order

### 4.1 Layout

```
go.mod                       module github.com/LongTermSupport/go-qa-ci; go 1.26; toolchain pinned
LICENSE                      MIT (as siblings)
README.md                    same section order as rust-qa-ci/README.md
defence-before-fix.toml      conformance claim, two levels, gaps (INFERRED location)
tools.lock                   golangci-lint, staticcheck (for -explain), govulncheck, deadcode, gremlins,
                             ast-grep, gitleaks/actionlint (CI only)
cmd/go-qa/main.go            CLI; also enters unitchecker.Main when GQACI_VETTOOL=1
internal/config/             qa/go-qa.toml load, defaults, unknown-key rejection
internal/pipeline/           Phase, Step, Execute (port of pipeline.rs)
internal/tools/              catalogue, command planning, executor, CI dirty-tree check
internal/finding/ internal/hit/   output types, Display, record application
internal/record/ internal/policy/ internal/date/   record schema, validation, budgets, tiers, ratchet
internal/suppression/        Go + config suppression scan
internal/defences/           listing (golangci linters, analyzers via driver -flags, ast-grep, stages, builtins, runners)
internal/docs/               resolver + bundled page lookup
internal/summary/            agent summary region
internal/stage/              [[stage]] and [[resolver]]
internal/builtin/            gqaci.* identifiers + one-line descriptions
internal/golangci/ internal/vet/ internal/astgrep/   output parsers
analyzers/                   PUBLIC: bundled analyzers (sensitiveformat/, generatedheader/), each with testdata/
analyzers/suite/             All() []*analysis.Analyzer
driver/                      PUBLIC: Main(extra ...*analysis.Analyzer) for consumer vet drivers
ruledocs/                    embed.go + rules/<id>.md (bundled pages, embedded)
astpack/                     embed.go + rules/gqaci.ast.*.yml (if any)
configs/golangci.yml         strict v2 baseline (embedded)
qa/go-qa.toml qa/record.toml go-qa-ci gating itself
scripts/install-tools.sh scripts/public-safety-scan.sh scripts/secret-scan.sh
.githooks/  .github/workflows/{ci.yml,public-safety.yml}  .github/scripts/write-denylist.sh
.github/pull_request_template.md .github/ISSUE_TEMPLATE/generic.md
docs/CONFORMANCE.md docs/PUBLICATION-CHECKLIST.md docs/denylist.example
```

Keep the dependency set small: stdlib, `golang.org/x/tools`, one TOML library with strict
decoding (`github.com/pelletier/go-toml/v2` `DisallowUnknownFields`, or BurntSushi + `Undecoded()`),
one YAML library (`go.yaml.in/yaml/v3` or `gopkg.in/yaml.v3`) for `.golangci.yml`/`sgconfig.yml`.
Tests: stdlib `testing` only.

### 4.2 Milestones

Each one is one PR-sized unit with its own acceptance test. All run `go test ./...` and
`golangci-lint run` green. From M5 onwards, the self-gate `go-qa run --ci` must also be green.

| #   | Milestone                                                                                                                                                                                                                                                                                                                                                        | Acceptance test                                                                                                                                                                                                                                   |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0  | **Public-safety first.** Create the repo with LICENSE, README stub, `go.mod`, `cmd/go-qa` printing `--version`. Port `scripts/public-safety-scan.sh` (env `GQACI_DENYLIST`, git config `gqaci.denylist`), `secret-scan.sh`, `.githooks/`, `public-safety.yml`, `write-denylist.sh`, `docs/denylist.example`, `docs/PUBLICATION-CHECKLIST.md`, PR/issue templates | The public-safety workflow is green with secret `GQACI_DENYLIST` set (**owner action**). Scanning a tree with a planted placeholder term fails and prints the line number, not the term. `go-qa --version` exits 0, an unknown subcommand exits 2 |
| M1  | Config load + defaults + unknown-key rejection; `go-qa tools`                                                                                                                                                                                                                                                                                                    | Table tests: defaults match README; an unknown key gives exit 2 with the key named; `tools` lists the catalogue in phase order                                                                                                                    |
| M2  | Pipeline (port `pipeline.rs` + its tests), `-t` selection, `-p` scoping relative to root, CI detection (`CI` env), exit codes 0/1/2, run summary table, DBF pointer                                                                                                                                                                                              | Ports of `pipeline_test.rs` cases: a detector failure leaves runners unstarted; `--fail-fast`; dedupe/sort; an unknown `-t` lists the known names                                                                                                 |
| M3a | Record schema, `gqaci.record.invalid` (justification rules, `GENERIC` list), `gqaci.record.stale`, `record check\|list\|explain`                                                                                                                                                                                                                                 | Ports of `record_test.rs`: generic or short justification rejected; a glob without `policy` rejected                                                                                                                                              |
| M3b | Budgets, trust tiers, horizons, co-sign, expiry (`gqaci.record.budget`/`expired`)                                                                                                                                                                                                                                                                                | Ports of the `policy_test.rs` budget/tier/expiry cases, with a fixed clock                                                                                                                                                                        |
| M3c | Ratchet with git history (`HEAD`, merge base with `history_base`), `record prune` lowers `high_water`                                                                                                                                                                                                                                                            | Tempdir git repo tests: unsigned raise fails; a stale signature fails; prune rewrites `high_water`                                                                                                                                                |
| M4  | Bundled docs (`ruledocs` embed), resolver order, `rule-doc`, `docs-check`, `docs` detector (`dangling`/`orphan`), `gqaci.*` builtin list                                                                                                                                                                                                                         | Test: every builtin id has a page whose first line is `# <id>` and which has a "How to fix correctly" section (8.1). An orphan page in a fixture fails. `rule-doc` on an unknown id exits 1                                                       |
| M5  | `fmt` (`golangci-lint fmt`) + `golangci` detector: JSON parse to `Hit`s with ids, compiler-style output, record applied; `configs/golangci.yml`; this repo's own `.golangci.yml`; **turn on the self-gate CI job**                                                                                                                                               | Fixture module with an `errcheck` violation fails with `error[errcheck]`. A staticcheck finding prints `staticcheck::SAxxxx`. `--ci` on an unformatted fixture fails with "working tree changed"                                                  |
| M6  | Suppression detector (3.4) incl. `.golangci.yml` exclusion routes and `gqaci.suppression.bare`; decide where the generated-header check lives                                                                                                                                                                                                                    | One fixture per route: unrecorded fails, recorded passes, a bare `//nolint` fails even when recorded. An unparseable file fails                                                                                                                   |
| M7a | `driver` package; go-qa as vettool via `GQACI_VETTOOL`; `analyzers` detector running `go vet -vettool -json`, parsing, applying `covers = "findings"`; project driver config; listing via `-flags`; docs via `help`                                                                                                                                              | A fixture with a trivial test analyzer reports `error[gqaci_test]` and exits 1, though `go vet -json` itself exits 0. A recorded findings exception passes; an unused one fails `gqaci.record.stale`. `go-qa rules` lists a project analyzer      |
| M7b | `gqaci_sensitive_format` + page                                                                                                                                                                                                                                                                                                                                  | `analysistest` red fixtures (alias, embedded, generic, slog) and a clean fixture (a `String()` method, a redacting type)                                                                                                                          |
| M8  | `ast-grep` detector: project `sgconfig.yml` + optional bundled pack, `gqaci.rules.unnamed` / `untested`, `ast-grep test`                                                                                                                                                                                                                                         | Ports of `ast_grep_test.rs` / `main_test.rs` ast-grep cases, with Go fixtures                                                                                                                                                                     |
| M9  | Runners: `vuln` (govulncheck JSON), `mod` (tidy -diff, verify), `test`, `coverage` (threshold), `mutants` (off/diff/full), `deadcode` detector; `licences` stays unbuilt unless asked                                                                                                                                                                            | Fixtures: a coverage gap below the threshold fails with `error[coverage]`; an untidy `go.mod` fails `mod`; a test failure stops later runners                                                                                                     |
| M10 | `[[stage]]` + `[[resolver]]` (port `stage.rs`), `{files}` expansion, `output = "findings"`                                                                                                                                                                                                                                                                       | Port of `stage_test.rs`: a shellcheck-style stage over `**/*.sh` is skipped when no files match; findings JSON gets the record applied                                                                                                            |
| M11 | `rules [--json]` completeness (builtins, golangci linters with descriptions, analyzers, ast-grep, stages, runners, record, budget) + `summary [--check]` + `gqaci.summary.stale`                                                                                                                                                                                 | JSON shape test mirrors `main_test.rs` `rules_json_is_machine_readable`. A stale summary region fails                                                                                                                                             |
| M12 | `tools.lock` + `install-tools.sh [--check] [tool...]`, CI jobs (go: fmt/lint/test; self-gate with fetch-depth 0; actionlint + shellcheck), `defence-before-fix.toml`, `docs/CONFORMANCE.md`, a full README                                                                                                                                                       | Clean runner: `install-tools.sh` then `go install ./cmd/go-qa` then `go-qa run --ci` green. A test asserts the conformance file parses and lists a gap for every clause CONFORMANCE.md marks Gap/Partial                                          |
| M13 | First tag `v0.1.0`: **owner decision**. Go module tags are immutable in the proxy and sum database, so never move or re-push a tag                                                                                                                                                                                                                               | A consumer-shaped fixture repo runs `go install github.com/LongTermSupport/go-qa-ci/cmd/go-qa@v0.1.0 && go-qa run`                                                                                                                                |

Parallelism: M3a-c, M4 and M10 depend only on M1/M2. M5-M9 depend on M2 + M3a. M11 needs most of
them. M7b needs only M7a.

Optional later, not in scope until asked (YAGNI): `run --json` / agent-mode output (ts `--llm`,
php `--agent-mode` precedent), `go-qa init` scaffolding, a run lock with exit code 75, Claude
skill deployment (ts `deploy-skills`, php composer plugin; rust ships none), release automation.

---

## 5. Install-specific or must-not-copy material

- **Personal decider handle.** `rust-qa-ci/qa/rust-qa.toml` has `deciders = [...]` naming the
  owner's personal handle, and every `decided_by` in `rust-qa-ci/qa/record.toml` repeats it. Do
  not copy the record entries. go-qa-ci starts with an empty record (`high_water = 0`) and a
  decider list that the owner fills in.
- **Denylist secret.** `RQACI_DENYLIST` is a repository secret. The real list lives outside every
  repo (`docs/denylist.example`, `.github/scripts/write-denylist.sh`). go-qa-ci needs its own
  secret (`GQACI_DENYLIST`) set by the **owner**. Never commit or echo the list, and never put
  real terms in examples.
- **Model trust map.** The model ids in rust's trust map and records (`opus`/`sonnet`/`haiku`,
  `sonnet-5-5`) are generic and fine to reuse as an example.
- **ts-qa-ci consumer references.** `ts-qa-ci/README.md` "Status" names a specific consuming site
  repo and a plan path inside it, and `ts-qa-ci/package.json` `_peerDependenciesPolicy` refers to
  the same dogfooding. Do not carry these over.
- **php-qa-ci project-specific material.** Do not copy the company/author paragraph ("Long Term
  Support" in `php-qa-ci/README.md`), the Claude hook bundle and hooks-daemon coupling (README
  "Claude Code Integration", `bin/hooks-daemon-full-qa-blocker`), the branch-name policy lane,
  or the PHP-line versioning scheme (`85.x.y`). Go uses plain semver `v0.x.y`.
- **Action pins.** GitHub Actions are pinned by commit SHA in rust's workflows. Copy the pattern,
  but re-resolve the SHAs when writing go-qa-ci's workflows.
- **Neutral wording everywhere.** Use only neutral example names in fixtures, docs, commits and
  PR text: `example.com`, RFC 5737 addresses (`docs/PUBLICATION-CHECKLIST.md` item 7;
  `.github/pull_request_template.md`). go-qa-ci is public from its first push, so M0 must land
  before anything else.

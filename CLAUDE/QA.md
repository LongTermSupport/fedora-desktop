# QA — Quality Assurance Scripts

## Primary Rule

**Two commands, two moments:**

```bash
./scripts/qa-changed.bash   # while iterating: before each commit — only the checks the diff needs
./scripts/qa-all.bash       # before every push — all gates, all suites; must pass
```

Push as soon as `qa-all.bash` passes; a burst of commits may share one full run.
`qa-changed.bash` is a fast pre-commit check, not a substitute: it cannot see a gate that
no changed file names. See [qa-changed.bash](#qa-changedbash--diff-scoped-qa-while-iterating).

**NEVER use the individual gate scripts directly** (`qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`) — use `qa-changed.bash` or `qa-all.bash`.

---

## What qa-all.bash Runs

`qa-all.bash` is a wrapper (a header comment, `set -euo pipefail`, and one call into
`lib/go-tool.bash`, no logic of its own) over a Go tool (`src-go/qa/all`, command
`src-go/qa/cmd/qa-all`, in the nested QA module; `lib/go-tool.bash` builds it into a private
directory under the cache and runs it). **What it runs is data: `scripts/qa-gates.tsv`**, one
row per gate in run order, with the gate's kind, command, the reason on its `✗` line and how
its summary is read. It runs every row in the two tables below, never stopping at a failure.
Seven are *stages*: they write a JSON report that is merged **by stage name** into
`/tmp/qa-results.json` (`QA_RESULTS_JSON` overrides the path; the run removes the previous
report first, so a run that ends early leaves none to be mistaken for its own); the rest are
run, captured and read separately (see below). Those seven emit **eight** named verdict lines
— `qa-bash.bash` prints `bash` and `shellcheck` — so a run shows one more stage name than it
has gates. Each gate has a time limit (`QA_ALL_TIMEOUT`, 30 minutes, `0` disables) and
`qa-all.bash --help` prints the usage; an unknown argument is exit 2 (the bash ignored every
argument and ran the suite).

This page states no gate count, because a number in a document is the first thing to go
stale. For the count, run `./scripts/qa-all.bash` and count the distinct `name:` prefixes
on the `✓`/`✗`/`⚠` lines. A missing **required** tool makes a stage (and the whole
run) exit `2`; a real analyser crash (e.g. ruff/shellcheck exit ≥ 2) is a hard failure,
never silently treated as "0 issues".

**This inventory is derived, not maintained.** `qa-docs.bash` (the Go tool `src-go/qa/docs`) reads the
gates out of `scripts/qa-gates.tsv` and fails the docs gate for any that has no row
below, so a gate added without a row is caught on the same commit. It is derived because
it kept going stale: the counts above were wrong by eleven and the table was missing ten
rows, having been "corrected" more than once by swapping one hand-written list for a
fresher hand-written list. `qa-changed.bash` reads its suite population from the same file,
so no tool scans another script's text for what it runs.

The tool's proofs are its Go tests (a fake process seam over the real table: the order, every
gate failing in turn with the rest still running, each detail pattern against the gate's own
output captured from a real run, the report merge byte for byte against what `jq -s` produced), a golden replay of
the bash it replaced (`src-go/qa/all/golden_test.go`, recorded from a git revision by
`CLAUDE/Plan/00192-.../qa-all-golden/capture.py`; any difference must be a numbered bug of that
plan's `SPEC-qa-all.md` section 10 pinned in `testdata/deviations.json`), and the real tree:
`test-qa-changed.bash` reads the same table, and the docs gate checks it against this page.

| Script                   | Checks                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Files                                                                                                                                                                                                           |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `qa-bash.bash`           | The Go tool `src-go/qa/bash` (spec `SPEC-qa-bash.md`): `bash -n` (always) + shellcheck (**required** — exits 2 if absent, Plan 00075), run with `--norc --external-sources` and an empty `SHELLCHECK_OPTS`, so no `.shellcheckrc` or environment option can change the rules. Exits 2 if discovery finds **0 files**, and (Plan 00076) if it misses **any tracked shell script**. **shellcheck `error` AND `warning` findings GATE** (raised in Plan 00075 — SC2155 is this repo's own defect class); `info`/`style` advisory. **Suppression is gated too** (SPEC S16-S20): a `# shellcheck disable=` directive in any gated file fails unless `scripts/shellcheck-disable.allowlist` records it (a per-file count and codes that only move down, compared with the merge-base so an entry cannot be added or raised); `disable=all` never passes; a `shell=` that contradicts the file's shebang fails. `qa-changed` runs the same check on the changed files (`shell-directives`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | Repo-owned bash (excludes `roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`, `.claude/skills`)                                                                                                              |
| `qa-python.bash`         | Go (`src-go/qa/python`, spec `SPEC-qa-python.md`): one `python3 -I` compile of every file (nothing written into the tree) + ruff (`--no-cache`; exit ≥ 2 = hard fail; no `--fix` mutation). Scope is what git would publish. Exits 2 if discovery finds **0 files**, if ruff does not list **every** file it was given (`ruff check --show-files`), or if `.qa-versions` has no single `RUFF=` pin. A ruff of another version than the pin **fails** the gate (exit 1) without linting. A `.j2` with a Python shebang is **rendered then compiled** — `{{ … }}` → `None`; a `{% … %}` statement is a hard failure, never a skip                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Repo-owned Python files — discovered by extension **or shebang, regardless of file mode**; plus Python `.j2` templates, syntax-checked but not linted                                                           |
| `qa-patterns.bash`       | Semgrep rules from `.semgrep/bash-conventions.yml` (`\|\| echo` and other error-hiding patterns), applied by the Go tool `src-go/qa/patterns` (script is a build-and-run wrapper; spec `SPEC-qa-patterns.md`). Scans only what git would publish, in a temp mirror so coverage does not depend on file mode, and exits 2 if any discovered file is absent from `.paths.scanned`, could not be parsed, or the rules fail their fixture self-test (Plan 00076)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | Repo-owned bash                                                                                                                                                                                                 |
| `qa-gh-scopes.bash`      | Semgrep rule `gh-scope-outside-ssot` (`.semgrep/gh-scopes.yml`, proven against `.semgrep/gh-scopes.fixture` every run): a GitHub OAuth scope named anywhere but its single source. The Go tool `src-go/qa/ghscopes` (script is a build-and-run wrapper; spec `SPEC-qa-gh-scopes.md`) hands semgrep every file git would publish by name and exits 2 if one it should have read is absent from `.paths.scanned` and `.paths.skipped`, if semgrep reports any error, or if nothing was scanned. See [gh-scope-outside-ssot](#gh-scope-outside-ssot)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Every tracked regular file, any language, minus the rule's excludes                                                                                                                                             |
| `qa-ansible.bash`        | Fail-fast scan (`failed_when: false`/`ignore_errors`, bare or quoted in either style, without same-line `# FAIL-FAST-OK:`, case-insensitive; the tool is Go, `src-go/qa/ansible`), **self-default vars** (`x: "{{ x \| default(…) }}"` — the 2.19 recursive-loop footgun `--syntax-check` can't see), **deprecated fact vars** (both `ansible_<fact>` and the un-prefixed injected names like `getent_passwd`, which no `ansible_`-anchored pattern can reach), **a guessed uid in a `/run/user/{{ … }}` path**, **plus** playbook shebang + exec-bit hygiene                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | `playbooks/ tasks/ vars/ environment/ roles/` (excludes `roles/vendor`), `*.yml`/`*.yaml`, only files git would publish (gitignored files are never read); a symlink to a directory or a dangling one is exit 2 |
| `qa-ansible-syntax.bash` | `ansible-playbook --syntax-check` on every playbook — a file with a top-level `- hosts:` **or `- import_playbook:`** (Plan 00081 F9/F14: deriving from `hosts:` alone dropped `playbook-main.yml`). Parse-only — safe in the CCY container. The tool is Go (`src-go/qa/ansiblesyntax`, spec `SPEC-qa-ansible-syntax.md`); it checks four playbooks at a time and reports in path order, and exits 2 up front when `ansible-playbook`, `ansible.cfg` or the vault password file the config names is missing. The pass line states the breakdown, so a coverage change is visible                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | **Repo-wide**, not a fixed path list; excludes vendor/upstream trees. Includes playbooks under `CLAUDE/Plan/**`                                                                                                 |
| `qa-js.bash`             | Go (`src-go/qa/js`, spec `SPEC-qa-js.md`): `node --check` on every JavaScript file git would publish (`.js`, `.mjs`) + the project's ESLint 8 over those under `extensions/`, run as `--no-eslintrc --config extensions/.eslintrc.json --no-inline-config` so a nested config or an `eslint-disable` comment cannot weaken the rules, with the proof that ESLint read every file it was given (`qa-changed`'s per-file eslint runs the same way and is judged by the same code). `--no-inline-config` also makes `/* global X */` and `/* eslint-env */` comments inert: a global or environment a GJS file needs goes in `extensions/.eslintrc.json`. No `eslint-disable` exists in the tree, so there is no suppression allowlist                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Repo-owned `.js` (excludes vendor/node_modules) + `extensions/`                                                                                                                                                 |
| `qa-docs.bash`           | Link targets exist; every `#anchor` matches a real heading; every play imported by `playbook-main.yml` is named in both `docs/playbooks.md` and `docs/architecture.md`; every `CLAUDE/*.md` has an index row; every gate in `scripts/qa-gates.tsv` has a row in these tables and every row is a gate that runs (Plan 00070; the Go port, Plan 00192, reads Markdown with a CommonMark parser, `playbook-main.yml` as YAML and the gate table with the loader `qa-all` uses)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Core docs only — `docs/`, everything under `CLAUDE/` (daemon-owned `CLAUDE/core/` included), `README.md`, `*/CLAUDE.md`, `.claude/rules/`. **Not** `CLAUDE/Plan/**`                                             |
| `qa-go.bash`             | Interim Go gate (replaced by the go-qa-ci toolchain): `golangci-lint fmt --diff` (any diff fails), `go vet`, `go test -race` with a whole-module statement coverage floor (`GO_COVERAGE_FLOOR`) and every package tested (that includes `internal/freeze/golden`, the replay of what the deleted bash `podfreeze` and `lxcfreeze` printed, scenario by scenario: any difference from the recorded bytes must be a numbered bug pinned in `deviations.json`; the freeze tools have no bash suite, their rows in `scripts/go-binary-suites.tsv` say `qa-go`), `golangci-lint run`, from `src-go/` and from its nested `src-go/qa` module (the project rules, kept apart so the QA tooling's dependencies stay out of the shipped binaries; `src-go`'s own are vendored and judged by `qa-go-deps`); then the project rules ([GoProjectRules.md](GoProjectRules.md): a `go vet -vettool` driver, built once, over `src-go` and over the nested `src-go/qa` module), any finding failing. A missing `go` or `golangci-lint` is a hard failure (`play-golang.yml` installs `go`, `play-python.yml` `golangci-lint`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `src-go/`, whole; `qa-changed.bash` runs it for any changed `src-go/` file                                                                                                                                      |
| `qa-bash-limits.bash`    | Tooling is Go (CLAUDE.md, [src-go/CLAUDE.md](../src-go/CLAUDE.md)): every bash file (`*.bash`, `*.sh`, `*.bash.j2`, or any file, whatever its name, whose first line is a bash/sh shebang, or a `.j2` with one after only Jinja lines; tracked and untracked-not-ignored) is parsed with `mvdan.cc/sh` and stays within 60 code lines, 90 statements, 4 functions, no associative arrays, no interactive UI (gum, fzf, whiptail, dialog, select ...), no data parsing (a second `jq`, `python -c`, `yq`), or is on the ratchet allowlist `scripts/bash-limits.allowlist` at its measured size. The measure is defined in the package doc of `src-go/qa/bashlimits`. A listed file may shrink and never grow or gain a flag; one under its record, within limits or gone fails until `scripts/qa-bash-limits.bash --update` lowers or removes the entry (`--update` never raises a record, adds an entry, or drops one whose file still exists, so a rename or a move into an exempt path cannot launder a file). The allowlist carries the measurer version (`# measurer: N`); a change to the measure raises `MeasurerVersion` and the allowlist is rewritten once with `--remeasure`, the only time a record may go up. The base: `QA_BASH_LIMITS_BASE`, else the merge-base of HEAD and origin's default branch (so committed-but-unpushed raises fail locally); CI uses the merge-base with the default branch, or the previous head for a push to the default branch, whose runs are never cancelled. A base without an allowlist, or no derivable base, is an error. Exempt, in `src-go/qa/bashlimits/exempt.go` each with its reason: test suites (`scripts/test-*.bash`, `tests/`), plan-folder scripts, vendored and daemon-owned trees. The nested QA module `src-go/qa` holds the code (its parser dependency stays out of the shipped binaries); the script builds the command into a private directory under the cache | whole tree; `qa-all.bash` hard gate; `qa-changed.bash` runs it for any changed bash file, the allowlist, a path on the allowlist or its Go source                                                               |
| `qa-toolchain.bash`      | Runs FIRST, as a hard gate: every tool a gate runs (ruff, semgrep, shellcheck, golangci-lint) is installed at the version `/.qa-versions` pins, and go is not older than the `go` directive of `src-go/go.mod`. A mismatch, an absent tool or a version that cannot be read fails this gate and lets the rest run — exit 1 (the machine is wrong; exit 2 is the gate being unable to judge); exit 2 is only for pins or a go.mod it cannot parse (`.qa-versions` is parsed as plain `KEY=VALUE` lines, never sourced). The tool is Go (`src-go/qa/toolchain`, command `src-go/qa/cmd/qa-toolchain`, specification `SPEC-qa-toolchain.md` T1-T16); `qa-changed` runs it whole when the pins, `src-go/go.mod`, its wrapper, package or command change (see "The QA Toolchain Is Pinned")                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | The machine, not any file                                                                                                                                                                                       |

The further gates below run inside `qa-all.bash` as **hard, non-structural** checks —
they are rows of kind `gate` (or `drift`, `helpers`), not report stages, so they have no
JSON to merge. Any one of them fails the run, and the gates after it still run:

| Gate                                        | Checks                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `qa-nokill-containerwatch.bash`             | the container-watch watchdog (every file `git` would publish ending `.py` under `helpers/containerwatch/`, nested ones included; the tool is Go, `src-go/qa/nokill`; and the panel's `containerReport.js` and `sections/containers.js`, which are **named** so a missing one fails the gate rather than being globbed away) has gained no process-termination or container-lifecycle call site, **except** `containment.py`, which is named as the one audited exception and held to a stricter list: a graceful `stop` only, never `kill`/`rm`/`pause`/a force flag                                                                                                                                                                                                                                                                                                                                                                                                   |
| `qa-deployed-drift.bash`                    | every repo-owned `files/home/.local/bin/` script matches its deployed `~/.local/bin/` copy                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `qa-helper-tests.bash`                      | the `helpers/` unit suite (Plan 00081 F11), in Go (`qa/helpertests`, `SPEC-qa-helper-tests.md`); `--counts-file` reports its size and skip count as data; exit 0 pass, 1 a test failed, 2 no test found, a tracked test lost, counts that disagree with the request or an unreadable verdict                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-secret-scan.bash`                     | the pre-commit secret scanner's own unit suite (Plan 00092)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-planlib.bash`                         | the `_planlib.inc.bash` regression suite behind every plan script (Plan 00092)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-meta-deploy.bash`                     | `meta-deploy.bash` and its `meta-deploy.list` queue: a malformed entry, a missing plan script or playbook and a failing unit all stop the batch, and the real list validates                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-ccy-rootless-guard.bash`              | ccy's rootless-engine verdict (Plan 00072); pure function, no podman needed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-ccy-token-mode.bash`                  | `select_token`'s per-mode answer to an unusable token pool (Plan 00048, CCY 3.50.0)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-ccy-token-usage-output.bash`          | the token menu's usage view (`u`) prints only its own display lines: driven through the real fetch-and-redraw path with curl stubbed, every line is classified against the intended display, stray assignment or trace lines fail, stderr must stay empty, and the classifier is checked against known noise first                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `test-ccy-token-import-cleanup.bash`        | the import script `ccy --export-token NAME` prints, run as pasted against a scratch pool: the other `NAME.DATE.token` files are removed only on `CCY_TOKEN_IMPORT_REMOVE_OTHERS=1` or `y` at the prompt (answered on a real terminal via `script`), kept on `0`, `n`, Enter and with no terminal; an undated file, another name or a longer name is never touched; an unknown value refuses before writing; the token value is never printed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-ccy-token-usage-command.bash`         | `ccy --token-usage [--json]`, the real launcher from a non-repository directory with curl, podman, docker and tmux stubbed: one entry per token name read with the earliest-dated file and the later one listed as shadowed and never sent, an expired token never sent, the menu's normalised percentages under both scales and over the limit, a scale conflict and a missing bucket reported as `null` rather than a wrong number, valid JSON with an awkward name and an unreachable API, the second run served from the cache, exit 0/1/64, no token value in the output and nothing written but the usage cache                                                                                                                                                                                                                                                                                                                                                  |
| `test-ccy-ssh-handling.bash`                | ccy's SSH key and agent handling into the container                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-ccy-git-signing.bash`                 | ccy carrying commit signing into the container: the key staged and the gitconfig copy repointed, and a launch refused when signing is on with no usable key (Plan 00139)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-git-signing-declared.bash`            | host commit signing only where the machine declares an identity: `git_signing_declared` under each declared state, every signing task of `play-git-configure-and-tools.yml` inside its gate, and that block run by ansible against a temp HOME — a `github_accounts: {}` box gets no signing and no error, a desktop gets signing, a desktop missing its key fails (Plan 00139 T5.5). Needs `community.general`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-qa-tool-pin-converges.bash`           | `play-python.yml` converges an off-pin QA tool: probe, remove (`state: absent`), install, pin, in that order; removal and install select the same probe results; no pipx task sets `force: true`; the removal's `when:` evaluated by ansible keeps a tool at its pin and removes one at another version or missing (Plan 00142)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-agent-browser-session-guard.bash`     | the session cap behind ccy's three browser commands: a new session is refused while one is open, reuse and non-launching commands pass, and all three wrappers route through it (Plan 00140)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-ccy-selinux-verdict.bash`             | ccy's SELinux verdict, including the states that must refuse                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-ccy-relabel-preflight.bash`           | ccy finds workspace entries rootless podman cannot relabel, explains them, and runs the offered fix only on `y`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-agent-mailbox-watch.bash`             | the agent mailbox watcher ([AgentMailbox.md](AgentMailbox.md)) wakes each side once per finished reply, waiting request or message, and makes a missing mailbox mode 0700                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-ccy-container-menus.bash`             | ccy's existing- and orphaned-container menus: a launch nobody answers (restore, restart, headless) takes their safe answer, and a menu with a closed input or three wrong answers gives up instead of looping                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `test-ccy-flag-check.bash`                  | ccy's fail-fast flag check judges only flags before `--` against `claude --help`, and treats help text with no usage line as no help                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-ccy-gpu-device.bash`                  | ccy's GPU device passthrough decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `test-ccy-container-name.bash`              | ccy's container name choice: launches started together each claim a different name (the claim is `ccy-teams internal claim-container-name`), a claim lasts as long as its launcher's process, docker uses the same claim, and a failure stops the launch                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-ccy-restart-request.bash`             | `lib/restart-request.bash` — validating a supervisor restart request (an untrusted file), the restart budget, the `--resume` relaunch argv                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `test-ccy-container-version-hook.bash`      | `scripts/git-hooks/pre-commit` — a change to the Dockerfile, to any file its `COPY` lines take from either build-context root (`files/var/local/claude-yolo/`, `files/opt/claude-yolo/`), or to the pingbus zipapp's sources needs a container version bump, with the LABEL and `REQUIRED_CONTAINER_VERSION` moving together; a deletion counts, and every file `play-claude-yolo.yml` puts into the image must be rejected without a bump while the host-only files under `files/opt/claude-yolo/` must not be judged; and the secret scan's scope: a vendored Go module under `src-go/vendor/` is not scanned when a stand-in `qa-go-deps` passes over the staged tree, a staged vendor change it rejects fails the commit, and it is judged on the index, not the working tree; the same address elsewhere is rejected (real hook, throwaway repository)                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-ccy-git-preflight.bash`               | `entrypoint.sh` stops before Claude starts when the container's git cannot read `/workspace`, printing git's own message (the real block, cut out between its `GIT-PREFLIGHT` markers; Plan 00169)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `test-ccy-project-env.bash`                 | `entrypoint.sh` sources `.claude/ccy/ccy.env`, then the untracked `ccy.env.local`, whose values win (the real block, cut out between its `PROJECT-ENV` markers)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-ccy-agent-bus.bash`                   | `entrypoint.sh`'s agent team bus step (Plan 00161 U31), from its `AGENT-BUS` marker to the final exec (printed, not run), after the real `PROJECT-ENV` block: seats only from the launcher's `PINGBUS_SEATS` (a bus variable set, changed or unset by `ccy.env` or `ccy.env.local` refused, a `PINGBUS_TEAMS` or `PINGBUS_HOME` from the launcher refused), `pingbus seat exec --` in front of both final exec lines, the plugin and settings right after `claude` inside the wrapper's `--`, inert when unset; the Dockerfile and play ship the kit                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-ccy-teams.bash`                       | The launcher's `--teams <seat>@<team>[,...]` (Plan 00161 U31): `lib/agent-bus-seats.bash` against a fake `agent-bus` (`seat check`'s canonical list and codes, `seat take` with `--no-prompt` when unattended, `PINGBUS_SEATS` and the `ccy-seats` label only with a list); a team alone (Plan 00177 T4.8): the seat chooser against a fake `gum` (default row, full handles, held/new words, Esc, gum missing) and `--resolved` when unattended; the re-attach offer with `--teams` (T4.6) over real registry records; the real launcher in a throwaway repository for every refusal before anything runs (no value, given twice, `seat check`'s 64 and 78, the unquoted-space form, `agent-bus` missing, a headless team alone) and a plain launch calling no `agent-bus`; restart and restore keep `--teams`; headless Quick Launch (saved choices, never the prompt) and the passphrase-key refusal, cut out of the launcher                                       |
| `test-ccy-env-local-dist.bash`              | `lib/common.bash` `ccy_env_local_dist_sync` — ccy writes the tracked `ccy.env.local.dist` (versioned, comments only, valid bash), refreshes an old one, leaves one from a newer ccy alone, never writes `ccy.env.local`, and warns when that file's "based on" version is older or missing; `ccy_env_local_mount_args` binds an existing `ccy.env.local` read-only (real functions, throwaway project)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `test-ccy-gitignore-safety.bash`            | `lib/common.bash` `check_ccy_gitignore_safety` — the generated `.claude/ccy/.gitignore` keeps session data and `ccy.env.local` ignored, lets the tracked files through (`ccy.env.local.dist` among them), adds the dist exception to an older file, and flags a tracked `ccy.env.local` (real function, throwaway repository)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `test-ccy-lifecycle.bash`                   | `lib/session-lifecycle.bash` — `--max-age`/`--run-for`/`--until` validation, the deadline kept across a restart, the entrypoint adding the supervisor plugin only when asked (plugin: `tests/helpers/ccy_lifecycle/`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `test-wsi-stop-grace.bash`                  | speech-to-text's delayed stop: the real `wsi` (the Go binary, built from `src-go/cmd/wsi` beside links to its sibling helpers) under stub audio tools keeps the microphone open for the grace after the first TERM, stops at once on a second TERM or a grace of 0, and refuses to record without a grace; then `tests/speech_to_text/`: `wsi-stream`'s stop state, handlers and drain as units, pre-buffer mode end to end (stub RealtimeSTT, fake `pw-record`, TERM during and after the model load), and `wsi-stream-server`'s stop order. Standard streaming and the server-mode client loop are not run end to end (Plan 00148)                                                                                                                                                                                                                                                                                                                                   |
| `qa-stt-limits.bash`                        | each speech-to-text recording limit has one home: `wsi`'s 30 s (`src-go/internal/wsi/limit.go`), `wsi-stream`'s 120 s, continuous dictation's GSettings keys; a literal copy in the panel, the server or a second place in a recorder fails (Plan 00148)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `qa-speech-to-text-rules.bash`              | the Semgrep rules in `.semgrep/speech-to-text.yml`, proven against `.semgrep/speech-to-text.js` and `.py` every run, over every JavaScript and Python file git would publish (extensionless scripts found by shebang); each language is scanned with its own rules, and the gate exits 2 if a rule has no `ruleid:` line in its fixture or a file it handed semgrep was not scanned. The tool is Go (`src-go/qa/sttrules`, spec `SPEC-qa-speech-to-text-rules.md`, S1-S17; `qa-changed` runs it over each changed JavaScript and Python file as `stt-rules-files`, and whole as `stt-rules` when the rules, fixtures or tool change). See [model-present-without-weights](#model-present-without-weights) and [dropdown-label-carries-explanation](#dropdown-label-carries-explanation) (Plan 00156)                                                                                                                                                                   |
| `qa-ready-wait-rules.bash`                  | [ready-wait-ignores-child-exit](#ready-wait-ignores-child-exit) over every tracked Python file (`.semgrep/ready-wait.yml`, proven against `.semgrep/ready-wait.py`) and every tracked shell script (the Go rule in `src-go/qa/readywait`, proven against `.semgrep/ready-wait.bash`), each run; the tool is Go (spec `SPEC-qa-ready-wait-rules.md`, R1-R26; `qa-changed` runs it over each changed Python and shell file as `ready-wait-files`, and whole as `ready-wait` when the rule, fixtures or tool change); exits 2 if a Python file it handed semgrep was not scanned (Plan 00156)                                                                                                                                                                                                                                                                                                                                                                             |
| `qa-hidden-bindings.bash` | [project_hidden_bindings](GoProjectRules.md#project_hidden_bindings) outside Go: every tracked shell file that runs fzf (no `--expect`; no `--bind` of a key to anything but navigation, read by `src-go/qa/internal/fzfbind`) and every tracked Python file that imports textual (each binding's action is navigation or the action of a `Button` in the file, judged by `helpers/qa_environment/textual_bindings.py`), each half proven against its fixture in `tests/fixtures/hidden-bindings` every run; the tool is Go (`src-go/qa/hiddenbindings`; `qa-changed` runs it whole as `hidden-bindings` after any shell or Python change); exits 2 if a file cannot be read or parsed or python3 cannot run |
| `qa-ansible-pause.bash`                     | [pause-without-register](#pause-without-register) over every published YAML file under `playbooks/` and `tasks/` (the shared discovery list): an `ansible.builtin.pause` task with no `register:` and no `# PAUSE-OK: <reason>`. The tool is Go (`src-go/qa/ansiblepause`, spec `SPEC-qa-ansible-pause.md`); the Go tests prove the rule against `tests/fixtures/ansible-pause/flagged.yml` (each `EXPECT-FINDING` line reported, nothing else) and `clean.yml` (nothing reported)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `test-ccy-host-hostname.bash`               | `ccy_host_hostname` — the RFC 1123 grammar guarding `CCY_HOST_HOSTNAME` (Plan 00121)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-ccy-host-time-zone.bash`              | `ccy_host_time_zone` — the host's IANA zone passed to every container as `TZ` (timedatectl first, the `/etc/localtime` link as fallback), refused rather than defaulted to UTC when neither names a zone under zoneinfo, and a project's `ccy.env` / `ccy.env.local` `TZ` still winning (Plan 00165)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-ccy-info-flags.bash`                  | the real launcher from a non-repository directory: `--version`/`-v`, `--help`/`-h` and a bare `--list-tokens` print and exit 0, and every other invocation still refuses with the repository-root error                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `test-ccy-network-disconnect.bash`          | `ccy --disconnect` — the detach, the picker's re-prompt, and whether the saved default network is cleared, kept, or never there, against a stubbed engine                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-ssh-suspend-guard.bash`               | `ssh-suspend-guard` session detection — driven against captured `ss` output, including a host whose sshd is not on port 22                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `test-ccy-session-registry.bash`            | the ccy session registry — record written, removed on return, kept on a kill (the real trampoline under `kill -KILL`), the one-shot replay filter, and restore as a translation over a stubbed live list (Plan 00135)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `test-ccy-compose-restore.bash`             | the compose question and the launch questions nobody can answer — `--compose start\|skip` answers without asking and records started or declined, services already up record running, a stack that will not start under `start` ends a person's launch but in a restore is retried and then asked about in the pane with the record left in place, `ask` with no terminal or under `--headless` is refused by name and never read, a restore of a record with no answer says why before it asks; the launcher's real argument loop sets `COMPOSE_MODE`, the outcome is really written into the session's record, the SSH no-key, passphrase-retry and GitHub-over-443 questions with nobody to answer; and every launcher question has a nobody-to-ask guard of its own, proven by removing each guard in turn (fedora-desktop#87, Plan 00135)                                                                                                                         |
| `test-ccy-sessions-reboot.bash`             | `ccy-sessions --list / notify / reboot / restore` — the Go binary, built from `src-go/cmd/ccy-sessions` as the play builds it (`qa-changed.bash` runs the four `ccy-sessions` suites for any `src-go/` file in that build's closure), under a fake tmux, systemctl and per-project daemon CLI: one signal per project, refusal before any signal on a missing CLI, `--dry-run` touches nothing, `systemctl reboot` last (Plan 00135); a ccy session with no container is left out; `shutdown-with-update` / `reboot-with-update` under fakes: desktop always updates and powers off (failed or hung warnings, inhibitors), server or `--strict-sessions` refuses (Plan 00173)                                                                                                                                                                                                                                                                                          |
| `test-ccy-sessions-take-over.bash`          | `ccy-sessions` (the Go binary) Ctrl-T and the bash library's `ccy_tmux_take_over` — a fake tmux holding sessions and their clients, the real picker under `script` with a fake fzf: the other terminal detached and this one attached, nothing killed, and nothing attached when the session ended, the detach was refused, the other terminal held on, or the user said no (Plan 00158)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-ccy-tmux-targets.bash`                | The tmux targets the bash library and `ccy-sessions`' Go pane code build, run against a REAL tmux on a private socket: a pane command reaches the pane with `=name:`, a session command the session with `=name`, and the bare `=name` pane form is refused (fedora-desktop#69, which fakes had hidden; Plan 00135)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-cc-teams.bash`                        | `cc --teams`, cc's agent team bus library: the argument split, the chooser called with the host type on a terminal only (the owner's one-time Element consent lives in `ccy-teams choose-teams`, tested in Go), the seats checked and taken as `host` through `agent-bus`, the launch block and the prompt sentence, cc's own settings file denying the file tools Element's folders, and the session's text, which names only the correct way to post (Plan 00177)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-ccy-teams-command.bash`               | the `ccy-teams` command — the Go binary, built from `src-go/cmd/ccy-teams` as the play builds it (`qa-changed.bash` runs it for any `src-go/` file in that build's closure); the real `--list` and picker (under `script`) over fakes for tmux, fzf, systemctl, sudo, gum, gh, wl-copy, `agent-bus` and `agent-bus-install`. It covers each team, seat (with its session and restore mark), member and person as those sources say; a down, undeclared or not-installed homeserver; and every key reaching the right command for the right row, with a no changing nothing. Sudo is asked only when needed, and a refusal exits 1 and never says "no teams". The fake sudo grants exactly the sudoers rule's commands. It also covers the password card and clipboard, colour (plain with `NO_COLOR` or piped), the no-teams offer, the gum `create` and `add-repo` questions (Esc and Ctrl-C write nothing), `backup` and `restore` (Plans 00161, 00177)              |
| `test-ccy-restore-askpass.bash`             | the restore-only SSH_ASKPASS, with the real ssh-agent and ssh-add: a server's restored session unlocks its key unattended (host probe and container entrypoint), an ordinary launch never uses askpass, the passphrase reaches no argv or output, a missing or wrong one fails at once, every passphrase copy is removed once its key is added or the launcher is killed, and neither tmux nor the container's configuration inherits any of it (Plan 00135)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-panel-sections.bash`                  | four suites: the panel's decisions on boot-stale/malformed/`state`-disagreeing documents, the indicator's icon per state (Plan 00109), the containers section (Plan 00144), and dock recovery on unlock                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `test-vmtest-prepare-record.bash`           | the fixture→checker record seam — one metacharacter unset every key after it (Plan 00109)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-vmtest-kernel-selection.bash`         | `select_second_kernel` — the one step of that route no machine here can execute (Plan 00109)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-run-bash-headless-localhost-yml.bash` | the headless `localhost.yml` writer (Plan 00119)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-run-bash-ssh-agent-teardown.bash`     | `hl_ssh_agent_stop`, including an agent that SURVIVES the kill (Plan 00063 Task 3.4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-run-bash-changed.bash`                | the real `run.bash --changed` in a throwaway checkout, against a stubbed changed-plays helper: the plays it names run in order under one play lock after one confirmation, a failure stops the run with its status, "cannot tell" and "gone" plays are named and not run, and a helper that cannot answer runs nothing (Plan 00141)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-run-bash-rerun.bash`                  | the real `run.bash --rerun` in a throwaway checkout, against a stubbed changed-plays helper: the menu marks plays that changed or failed, the pick (numbers, `a`, `q`) runs in menu order under one play lock, a mistyped answer is named and asked again up to three times, end of input cancels, and a failure stops the run with its status (Plan 00141)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-run-bash-single-play.bash`            | the real `run.bash` single-play mode in a throwaway checkout: `--headless <play>` unattended (sudo-only preflight, password via `/dev/fd/N`, stdin closed, the play's exit status, a pinned `RUN_BASH_ANSIBLE_PLAYBOOK`) and the shared play lock (exit 75 when held, delegation proven) (Plan 00137 T1.4/T2.1)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-run-bash-gh-scopes.bash`              | GitHub scopes asked for in one pass, from `vars/github-required-scopes.yml`: `run.bash`'s login carries every scope, a short token gets one refresh naming all it lacks, and headless `run.bash` and `gh-account-setup.bash` fail once naming every account and scope (Plan 00139 T1.3); `gh-account-setup.bash` puts gh's active account back however it ends; with several accounts logged in, `run.bash` asks which is the primary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `test-run-bash-ssh-agent-start.bash`        | headless `run.bash` loads the login key through askpass with the real ssh-agent and ssh-add, the helper's text holds no passphrase, and a wrong passphrase stops the run instead of hanging it (found in Plan 00135 T6.2)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-run-bash-config-repo.bash`            | `run.bash` reaches the private `<primary>/fedora-desktop-config` with the primary's own token whichever account gh has active, only a 404 reads as "not there", any other failure stops the run with gh's words, and the save sends the config as a body file, not an argument (found running Plan 00139 T1.4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-gh-alias-wrappers.bash`               | `play-github-cli-multi.yml`'s `gh-<alias>` and `clone-<alias>` run as their account and then put gh back on the account that was active, not the saved default; a failed switch either way, or no active account to return to, fails the wrapper; `gh-token-<alias>` reads by name without switching; only `gh-switch`, `gh-set-default` and `_gh_as_account` switch accounts (found running Plan 00139 T1.4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `test-self-update-cycle.bash`               | the real `fedora-desktop-self-update` root wrapper against a signed git fixture, with `runuser` and `systemctl` stubbed: refusals and exit codes, play then warn then `systemctl reboot` last, both passwords on descriptors, the play lock held, the pinned system ansible, `verify` (Plan 00137 T3.3/T3.4/T4.1)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-agent-bus-install.bash`               | the real `agent-bus-install` against a temporary root (`AGENT_BUS_INSTALL_TEST_PREFIX`), with `systemctl`, `firewall-cmd`, `nmcli`, `curl`, `ip`, `ss`, `dnf` and the admin tool's team commands stubbed (its `render` is the real one): argument and bus-address refusals; malformed and wildcard listen addresses and malformed CIDRs refused; the file layout; no download when the pinned hash matches, a refused hash installs nothing; no restart and no `CHANGED` when nothing changed; a unit that fails to start fails the readiness step; firewalld rules follow `allow_from`; `check`, `backup-now`, `restore`, `remove` (Plan 00161 U16)                                                                                                                                                                                                                                                                                                                   |
| `test-release.bash`                         | the real `scripts/release.bash` against a signed git fixture with only `gh` stubbed: each refusal (dirty tree, branch not `F<major>`, behind remote, CI red, pending or absent), the version following from the last tag numerically, the changelog entry, and the signed annotated tag landing on the release commit and being pushed (Plan 00153 T2.1)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-run-log-scrub.bash`                   | the run-log secret scrubber, driven by a deliberately incomplete redaction (Plan 00121)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `test-host-health-login-snippet.bash`       | the login snippet's interactive guard — an unconditional print breaks `scp` — and its once-a-day gate (Plans 00109, 00136)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `test-bash-history-search.bash`             | the Ctrl+R ranker's tier order, and the per-prompt recorder in a real interactive bash: right directory and status, leading-space and `HISTIGNORE` commands kept out, nothing recorded twice (Plan 00138)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-bash-history-session.bash`            | with the Ctrl+R search bound, up-arrow offers only the current terminal's commands while each is still appended once to the shared file, including a session whose prompt hook was replaced and which ends by `exit`; without it (no fzf), the whole file is loaded for stock Ctrl+R. Real interactive shells, rc-file started, run as a normal user (Plan 00138)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-terminal-modes.bash`                  | the prompt hook that switches stray terminal modes off, on a real pseudo-terminal: the exact sequence at the prompt, nothing on a pipe or for `TERM=dumb`, nothing that clears or swaps the screen, `$?` unchanged for later prompt hooks, added once when sourced twice, bracketed paste re-enabled by readline afterwards                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-fedora-desktop-health.bash`           | the on-demand report command: a sentence on a clean host, `--hold` released by Enter or EOF, a missing checkout named without a traceback (Plan 00136)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `test-qa-ansible-failfast.bash`             | the fail-fast directive pattern, through the built Go `qa-ansible` tool over one-line fixture trees (quoted values included)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-qa-changed.bash`                      | `qa-changed.bash`'s file-to-check mapping through its `--plan` mode, its git collection (modified, untracked, ignored, deleted, `--base`) in a scratch repository, and its command line and exit codes; `QA_CHANGED_BIN=<binary>` runs the same cases against a built binary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-qa-docs-exit-codes.bash`              | `qa-docs.bash`'s three exit codes, driven against fixture trees — a crash must never read as clean                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `helpers.gnome.check_extension_compat`      | every extension declares the GNOME Shell major this branch's Fedora ships                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `helpers.gnome.check_panel_contract`        | the panel's constants, document keys and section ids agree with the producer (Plan 00109)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `qa-vmtest-manifest.bash`                   | `vars/vm-test-scenarios.yml` parses (strictly: one document, no duplicate key) and the host's own validator (`helpers/vmtest/validate_manifest.py`, so python3 is required) accepts it (Plan 00110); every runnable scenario's `planned` equals the `PLANNED` of the guest checker `vmtest` would use (read from its syntax tree), every `guest-prepare-<scenario>.bash` fixture belongs to a scenario that declares `reboot_before_checks: true`, and no guest script is left for a scenario or profile that no longer exists; a guest script counts only if git would publish it. Exit 1 the manifest or a guest script is wrong, exit 2 an input unread or unparsed or python3 missing. The tool is Go (`src-go/qa/vmtestmanifest`, spec `SPEC-qa-vmtest-manifest.md` V1-V16; the old run-time controls are its Go tests); `qa-changed` runs it whole as `vmtest-manifest` when the manifest, the Fedora version, the validator, a guest script or the tool changes |
| `qa-version-pins.bash`                      | `vars/version-pins.yml` is well formed (a YAML parse; shape, installed blocks, at least one tracked pin) and every row still names a file that exists and declares that var with a value (Plan 00109). The tool is Go (`src-go/qa/versionpins`, spec `SPEC-qa-version-pins.md`, V1-V25); it exits 2 for a manifest or playbook it cannot read or parse, or for no pins, and `qa-changed` runs it whole as `version-pins` when the manifest or a playbook changes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `qa-go-deps.bash`                           | `src-go`'s dependencies are vendored, pinned and allowlisted (Plan 00204 D2): every module `src-go/go.mod` requires, direct or indirect, has a row with its reason in `src-go/go-deps.allowlist`; a pseudo-version or a `replace` directive needs a row of its own; a row `go.mod` does not need fails; `go mod verify` passes (the module cache unchanged since download), `go mod tidy -diff` is empty, and `src-go/vendor/` is byte for byte what `go mod vendor` writes (these two check `go.sum`). A cold module cache with no network is exit 2, could not judge. The tool is Go (`src-go/qa/godeps`, spec `SPEC-qa-go-deps.md` in Plan 00204); exit 2 for an allowlist or `go.mod` it cannot read or parse, or a go command that cannot start; `qa-changed` runs it whole as `go-deps` when `go.mod`, `go.sum`, the allowlist, `vendor/` or the tool changes                                                                                                                                                                                                                                           |
| `qa-go-vuln.bash`                           | `govulncheck`, pinned by the `tool` directive of `src-go/qa/go.mod` (nothing installed system-wide; `go tool govulncheck`), finds no known vulnerability that `src-go`'s code reaches; the package load reads `src-go/vendor/`, so the scan is of what the binaries are built from. Status 3 (a reachable vulnerability) is exit 1; any other failure, a vulnerability database it could not fetch included, is exit 2, never a pass. **It needs `vuln.go.dev` on every run**: govulncheck keeps no database cache, so an offline `qa-all` reds this gate with exit 2 and the fetch error. Same package as `qa-go-deps`; `qa-changed` runs it whole as `go-vuln` when `go.mod`, `go.sum`, `vendor/`, the qa module's `go.mod` or the tool changes                                                                                                                                                                                                                                                                                                                                                                           |
| `qa-plan-script-logging.bash`               | no active plan script (`CLAUDE/Plan/NNNNN-*/`, by `.bash`/`.sh` name or shell shebang) redirects a bare `exec` into an output process substitution (`exec > >(tee …)`, judged on the `mvdan.cc/sh` syntax tree), and no plan folder keeps a local `logs/` dir (Plan 00130). The tool is Go (`src-go/qa/planscriptlogging`, spec `SPEC-qa-plan-script-logging.md`); it exits 2 for a script that does not parse, an unreadable file or no script found, and `qa-changed` runs it whole as the `plan-script-logging` check when a plan script changes                                                                                                                                                                                                                                                                                                                                                                                                                    |

`qa-helper-tests.bash` and `check_extension_compat` were **documented here as gates and
not run by `qa-all.bash`** until Plan 00081. Following this document's own "ALWAYS and
ONLY use `qa-all.bash`" rule, a `helpers/` change earned `✓ QA passed` with its unit
suite never executed. The fix was to run them rather than to soften the rule.

The `test-*` suites are the same shape of hole, closed later: each guards a defect
class whose regression is **silent by construction**. A leak the secret scanner stopped
catching produces no signal on any commit, and a `_planlib.inc.bash` regression surfaces
only when someone next runs a plan's host script — which may be months, and on a plan
nobody is working on. Being exercised incidentally is not the same as being tested.

`test-ccy-rootless-guard.bash` was the sharpest case, because it was worse than
unrun: it ran in `.github/workflows/qa.yml` and nowhere locally, so this page's
"ALWAYS and ONLY use `qa-all.bash`" was false for anyone touching ccy's engine
guard — green here, red in CI. **When adding a suite, add a row to
`scripts/qa-gates.tsv` first; CI runs `qa-all.bash`, so a separate CI step is a divergence, not a
belt-and-braces.** `scripts/test-ccy-ssh-probe.bash` is deliberately not a gate:
it needs a real host and a `gh` token, so it is a host diagnostic.

### The same command does not reach the same verdict everywhere

`qa-all.bash` is the authority, but some stages read what the *machine* supplies rather
than what the repository ships. A local run and a CI run disagreeing is a fact about the
stage, not a flaky gate — find which input differs before touching anything.

| Gate                     | What it needs from the machine                                                                                                                           | Where that is missing                                                            |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `qa-ansible-syntax.bash` | the vault password file `ansible-config` reports to **exist and be non-empty** (never read by the gate); exit **2** when it is missing, never N failures | a clean checkout, and a worktree made by bare `git worktree add`                 |
| `qa-js.bash`             | `extensions/node_modules` — its own message says no playbook installs it. Exits **2**, which still aborts the run                                        | a bare `git worktree add`, and any checkout where `npm install` has not been run |
| `qa-deployed-drift.bash` | deployed copies under `~/.local/bin` to compare the repo against                                                                                         | the CCY container and a clean checkout — it self-skips **and names why**         |
| `qa-helper-tests.bash`   | one pair asserts against real `/sys/class/drm` and skips where no connector with a physical display link is present                                      | a VM whose only connector is virtual                                             |

A worktree Claude Code creates gets the first two as symlinks to the main checkout's copies:
the `worktree_create` seed in `.claude/hooks-daemon.yaml` lists them, with `localhost.yml`
for the commit hooks' private-identifier check. A bare `git worktree add` gets none of them.

`qa-deployed-drift.bash` is the shape to copy: it states the dependency, skips only for a
reason it prints, and the reason is checkable.

`qa-js.bash` is the shape NOT to copy, and it is in this table because a reviewer hit it,
not because anyone predicted it: exiting 2 makes it a gate whose absence removes every gate
behind it, which is the mechanism this page's own subject was hiding inside for three weeks.
A *failing* gate no longer does that; a gate that cannot RUN still does, which is why this
row matters more than it did. On a fresh clone `qa-all.bash` reaches `qa-js.bash` and stops
there — including before `qa-docs.bash`, the gate Plan 00125 repaired.

**`qa-docs.bash` used to head this table and no longer belongs in it.** It needed
`.claude/hooks-daemon/` on disk, because eight tracked `.claude/rules/*.md` files are
GENERATED by that repository's installer and link into it — so the same commit was green
here and red in CI for three weeks. It now classifies a link target three ways instead of
two, and the verdict no longer depends on what is installed:

| The target is…                    | Verdict                                                 |
| --------------------------------- | ------------------------------------------------------- |
| tracked by this repo              | checked as before; missing is a **failure**             |
| inside a declared vendored repo   | **warned on, never failed** — three outcomes below      |
| untracked, and vendored by nobody | **failure** — a link to something no clean checkout has |

The third row is what makes the second safe. "Ignored, so skip it" would have quietly
exempted a link into `untracked/` as well, trading a false failure for a silent skip — this
repository's recurring defect, in the fix for an instance of it.

**The third row says *untracked*, not *ignored*, and the two are different populations.** A
tracked file that happens to match an ignore rule moved **out** of that row; a file that is
present, unignored and simply never committed moved **in**, and that one is the finding —
green here, red in a clean checkout. `git check-ignore` answers "does `.gitignore` name this
path"; `git ls-files` answers "does this repository carry it". Only the second is a question
every checkout answers the same way.

**A vendored target is still looked at**, in the one case where looking means something:

| The vendored repo is… | The target is… | Outcome                                        |
| --------------------- | -------------- | ---------------------------------------------- |
| present               | there          | `verified` — silent                            |
| **absent** (CI)       | unknowable     | `unverifiable` — soft; nothing here could say  |
| present               | **missing**    | `broken` — its own `⚠` line, listing the links |

`broken` means the link is demonstrably wrong, which usually means that repository moved the
file and our generated pointers are stale — worth saying loudly, and still not ours to fix.
So it warns and does not fail: **the gate's exit code must never depend on what is
installed**, which is the whole point. What the gate SAYS may, and should — a machine that
can see the repo can say more about it, and saying more never flips a verdict. Anchors into
a vendored repo are not followed even when it is present: their headings are theirs to
rename, and going red on another repo's churn would be a dependency on it for a defect we
could not fix.

That `⚠` line is composed by `vendoredWarning()` in `src-go/qa/docs`, and it returns a
list the tool prints with **no condition of its own**. The state that fills that list needs a vendored repo present *and* a stale pointer
into it, so a `if broken > 0` around the formatting would be a branch whose first execution
is the one nobody is watching. Empty list, empty output, formatting code exercised on every
run — the same reason a gate that only prints on failure gets a pass line.

**The question asked is trackedness** — `git ls-files`, plus the directories it implies,
since a link to `docs/` is a link to something this repo plainly owns. The index ships in
every clean checkout, so CI asks the same question.

Existence is checked **first** and trackedness second, deliberately. A typo'd link is both
absent and untracked, and `target does not exist` is the message that helps. Both are
findings either way, so a present-but-untracked target fails here and fails in CI for the
other reason: same exit code, different sentence.

That ordering is also the fix for the first version, which asked `git check-ignore`. That
question is machine-independent but narrower than the finding it raised: a file sitting on
one disk, never `git add`ed and matching no ignore rule, is not ignored — so it passed there
and failed in CI. Cause A's own shape, inside the classification built to remove it.

**The TARGETS ask trackedness; the DOCUMENTS are what git would publish** (tracked, or
untracked and not ignored: the shared `markdown` list of `qa/discovery`), so the scan
population is still what is on this disk, less the ignored. An in-scope markdown file nobody committed is
scanned here and simply absent in CI. Excluding it would be the wrong fix — a broken link in
a file you have not committed yet is a true finding, and catching it before the commit is
what a local gate is for — so the stage line carries the denominator instead:
`71 files (71 tracked)`, the same shape as `helper-tests`' `65 modules (65 tracked)`. Equal
numbers mean the two machines are reading the same set; unequal numbers name the gap.

The vendored roots are DECLARED in `vendoredRoots` (`src-go/qa/docs/links.go`) rather
than detected, because in CI there is nothing on disk to detect — probing for a `.git` would
answer one way here and another there. They are declared as parents (`roles/vendor/`, not
each role), so vendoring under an existing root needs no code change; a new root is a
one-line edit, and the finding that prompts it names the nested repository when the machine
can see it. **Nothing checks that a declared root really is a vendored repository** — adding
one exempts every link under it, and only review stops that. The stage line carries all
three counts for the same reason `version-pins` prints `COVERAGE: 9 of 9` — an exemption
nobody counts reads exactly like a check that ran and found nothing.

A tree git cannot answer for — no checkout at all — makes the gate exit 2 rather than assume
nothing is tracked. Assuming would be a confident verdict derived from a check that did not
run, and it would condemn every link in the repository.

**A skip is not a pass, so the `helper-tests` line carries the skip count.** `unittest`
counts a skipped test inside `testsRun`, so `Ran N tests` is byte-identical whether a test
asserted or skipped itself — two machines then report the same verdict over different
executed populations, which is this page's own subject appearing in the line used to detect
it. The count differing is the signal; the skip *reason* names what was ignored and is
printed by `python3 -m unittest -v <module>`, not by the suite at its default verbosity.

**That count is read from a file, never scraped from the run's output.**
`qa-helper-tests.bash --counts-file PATH` has
`helpers/qa_environment/unittest_counts.py` take the numbers from unittest's
`TestResult` object and write them there; `HelperCounts` in
`src-go/qa/gates/readers.go` reads the file and **fails** rather than reporting
zero if it cannot. Four readers that parsed the text instead were each defeated by a test
printing unittest-shaped output — a decoy crosses the summary in either direction
depending only on Python's 8KB stdout buffering, and an `atexit` handler writes to stderr
after it. A test can put anything on either stream in any order, so the streams are not a
source of truth for this; the result object is. Both halves are covered by the Go tests of
that package, whose last case runs the real runner end to end so a format change on one
side alone turns it red.

The file is **harder to reach than a stream, not unreachable**: its path travels in `argv`, so
a test that reads `sys.argv` could overwrite it — and this module's own tests are collected by
the runner they test. What protects it there is **write ordering**: the runner writes after
every test has finished, so a mid-run forgery is overwritten. A write landing *after* the
runner's — `atexit`, or a surviving thread — beats that, and nothing detects it.
`--counts-token` covers a different case only: a file written by something that never read
this run's `argv`, such as a stale file or a concurrent run. It is not a lock, because the
token rides in the same `argv` as the path. An **empty** file is refused with its own message,
which matters because a test calling `os._exit(0)` skips the write while the process still
exits 0 — leaving exactly the zero-byte file `mktemp` created. Existence is not generation.

`qa-all.bash` also **captures** the run's stdout and requires it to be empty. That stream is
the one `verdicts.py` parses for stage lines, so a single `print()` anywhere in the suite
could forge or split this suite's own verdict; a precondition that broad is a gate rather
than a comment. (This sentence carried the test count until it had rotted twice and the two
copies of it disagreed. A number that must be re-measured to stay true does not belong in
prose — the stage line prints the live one on every run.)

### The stage-line readers are Go, and a reader that finds nothing fails its gate

`src-go/qa/gates` holds the three readers, and between them they produce the summary in
**every** stage line `qa-all.bash` composes; the runner prints a `✓` line only for a gate that
has not already failed, so a gate cannot report both outcomes. Only `deployed-drift` builds its
own line, because there the line IS the gate's output rather than a summary of it (a pass that
printed none is a failure).

| Reader           | Used by                                 | When it finds nothing                            |
| ---------------- | --------------------------------------- | ------------------------------------------------ |
| `HelperCounts()` | `helper-tests`                          | **the gate fails** (the reader refuses the file) |
| `CaseCount()`    | the gates whose row says `count`        | **the gate fails**: it proved nothing            |
| `Detail()`       | the gates whose row says `detail:<ERE>` | **the gate fails**: its pattern is blind         |

(The `()` is load-bearing, not decoration: `check_qa_gate_inventory` reads any row whose
first cell is a backticked bare name as a **gate this document claims**, so writing them
plain made the table assert three gates that do not exist — caught by that check's own test.)

The bash readers degraded instead: `qa_gate_case_count` to the bare word `passed` and
`qa_gate_detail` to the literal `summary unreadable`, each a green `✓` line over a reader that
had gone blind. That was the defect `nokill-containerwatch` lived with for its whole life
(`[0-9]+ call site[s]? checked` against a gate that only ever printed `N container-watch file(s) clean`, behind a `||` fallback that asserted a sentence on every run), and a suite that
re-ran eleven gates to compare their patterns was the only thing watching for it. The pattern
now sits in the gate's own row and **every full run applies it to the real output**: a pattern
that stops matching, or a suite that prints no `passed: <n>` line, turns that gate red on the
spot. `test-ccy-info-flags.bash` was the live case (it printed `Summary: N passed`, so its tick
had never carried a count) and now prints the line.

Each gate used to inline its own reader. `grep -oE 'passed: [0-9]+'` prints EVERY match, so
an earlier `passed: <digits>` in the capture made the stage line two lines, and `verdicts.py`
reads the first as the stage and loses the rest — one defect, found once, then found again in
21 untested copies, and again in 2 more that had a different regex. `CaseCount()` takes the
last matching line and the last match on it, and a gate with several measurements
(`vmtest-manifest`) lists several `detail:` readers, joined `; ` on one stage line.

**Adding a gate: add a row to `scripts/qa-gates.tsv`** (and a row in the tables above, which the
docs gate checks both ways). Pick the reader that matches what the gate actually prints. The
Go tests load the real table, run every detail pattern against the gate's own output as
captured from a real run (`src-go/qa/all/testdata/wording/<gate>.txt`), and fail each gate in
turn to prove the ones after it still run; no tool scans `qa-all.bash` for what it runs,
because it is a wrapper. The captured wording is a snapshot: a gate that is reworded fails its
next full run (a pattern that matches nothing is a failed gate), and
`go test ./qa/all -run TestCaptureTheRealWordingOfTheDetailGates -capture-wording -timeout 30m`
(from `src-go/`, wording in `qa/all/testdata/wording`) recaptures it by running every detail gate for real and refusing a
gate that fails or that its pattern does not read.

A **count** reader gives a pass only for a count line with `passed: <n>`, n above zero, and
no `failed: <m>` above zero on the same line; no line, zero cases and reported failures are each
a failed gate even when the suite exited 0 (SPEC-qa-all B2, B21, B22). The same reader serves
`qa-changed.bash`'s suites, and so does every other reader of a row (`gates.Summarise`): a suite
picked by `qa-changed` is judged by all of its row's readers, as `qa-all` judges that gate. A gate's own output cannot forge the run's verdict: a gate line
that opens `✓ QA passed:` / `✗ QA FAILED:` is indented, so the verdict at the end of stdout is
the only such line (B23); a push gate may therefore read the verdict from stdout.

A count is a **proxy, not a proof**: two machines could skip the same NUMBER of different
tests. With the three conditional skips the suite has today the four machine shapes give
four distinct counts (0, 1, 2, 3), so it is currently exact — but that is a property of
those three sites, not of the mechanism. Adding a fourth conditional skip means checking
that property still holds, or surfacing the skipped tests by name instead.

That argument also assumes the skip count is bounded by the test count, and **it is not**.
`testsRun` counts test *methods*; `skipped` counts skip *events*, and one method registering
several `subTest` skips reports more skips than tests — `Ran 1 test … 3 skipped` is a
faithful line, not a broken one. No skip is raised *inside* a `subTest` block in
`tests/helpers` today — the three conditional skip sites are all outside one — so the
enumeration above holds; putting a skip inside a `subTest` means re-deriving it. Note that
`subTest` itself appears in ~70 places, so grepping for it will not answer this question.

The line also carries **`N modules (M tracked)`**, because a machine that COLLECTED a
different set of test modules is the same defect one level up and the test count alone
cannot show it. The two numbers cover the two directions, and both are needed:

- **tracked but not discovered** — `qa-helper-tests.bash` cross-checks its walk of
  `tests/helpers` against the tracked list of `qa/discovery` (git's index) and **exits 2** if
  any tracked helper test was not found; a walk that finds no test at all is exit 2 too, and
  the counts file the runner writes must agree with the request (zero tests, another module
  count or another tracked count is exit 2), so a run that did not happen cannot pass. Same
  guard as `qa-bash.bash` and `qa-python.bash`, which each grew it after the same defect.
- **discovered but not tracked** — the walk sees uncommitted files too, so a test nobody
  committed runs here and nowhere else and the counts stop being comparable. That is *not*
  a failure (it is the normal state mid-TDD), so it is reported rather than fatal: `modules`
  above `tracked` in the stage line, and the file names on stderr.

The second number goes in the **stage line** specifically. `qa-all.bash` discards the
child's stderr on a successful run, so a coverage figure reported only there is produced and
never delivered — which is one step short of the class this page is about.

**A stage that cannot pass in an environment is not a strict gate there — it is an absent
one.** Two consequences follow, and the second is the one that bites:

- a permanently-red stage carries no information, because a red run looks exactly like the
  previous red run;
- a failing gate used to **abort the suite**, so a stage that could not pass stopped every
  gate declared after it from running at all. The suite did not merely stay red — the
  number of checks actually executed *fell*, silently, and newly added gates could go their
  whole life without running once in CI. Measured while `helper-tests` was red: the masked
  set grew **5 → 11 → 25**, and 20 of those had never executed in CI.

**Every gate runs now, and the run reports all of them** (Plan 00125). A failing hard gate
records itself and the suite carries on; the final lines name every gate (and every report
stage) that failed rather than the first one that did. Only a report stage's `exit 2`
missing-tool abort still stops the run, because a suite that cannot run its tools has
nothing to accumulate.

Two things this buys, and the second is easy to miss:

- **the count of executed checks cannot fall silently** — a gate that fails still prints a
  stage line, so a run's census is complete whatever its verdict;
- **the failing gate appears in that census at all.** `verdicts.py` matches
  `^[✓✗⚠] QA (?:passed\|FAILED):` as a RUN SUMMARY *before* it tries the stage pattern, so
  the old `✗ QA FAILED: <prose>` gave the failing gate no stage line — it erased itself as
  well as everything behind it. Measured under one mutated gate: **11 of 38** stages parsed
  before, **38 of 38** after, and the failing gate present only in the second.

The Go tests hold the guard, and it is the behaviour itself rather than the shape of a source
file: with the real `scripts/qa-gates.tsv` and a fake process seam, every gate fails in turn
and every other gate must still run, in order, with the failure named in the verdict
(`src-go/qa/all/realtable_test.go`). It used to be a grep for `exit 1` in the bash, because
running the whole suite against a mutated gate was too slow for every commit.

So when a gate needs something an environment lacks, add the dependency (`CLAUDE.md` →
"Missing Dependencies — Fail Fast, Fix in IaC") rather than teaching the gate to tolerate
its absence. A gate taught to skip passes in precisely the environment that could not check
it — and it is the "skip and warn" pattern, one level of indirection away.

Two rules for anything a gate executes: resolve paths relative to the file (`REPO_ROOT`,
`import.meta.url`, `__file__`), never to a fixed absolute root — the repo is checked out at
a different path in the container, on a host and on a runner. And exclude the whole
`.ansible/` tree from discovery, not just `.ansible/roles/`: `ansible-galaxy` populates
`.ansible/collections/` with third-party files, so a stage that misses it counts a
different number of files depending on whether galaxy content has landed.

### All three source gates assert their own coverage (Plans 00076, 00081)

`qa-bash.bash` and `qa-python.bash` (and the coverage yardsticks of
`qa-ansible-syntax.bash`, `qa-helper-tests.bash` and `qa-ready-wait-rules.bash`)
share one discovery, the Go tool `src-go/qa/discovery` (spec
`SPEC-qa-discovery.md`), imported by each Go gate (`discovery.Main` with a list name and the root). It
lists what git would publish, never gitignored files, and stops (exit 2) at an
untracked nested repository, whose files git will not list. `qa-patterns` calls the
same exported shell predicate (`discovery.ShellShebang`; a `-c` or a combined flag group
such as `-ec` makes a shebang an inline one-liner, not a script) over the same trees
table. Change discovery there, not in a gate.

The two languages carry **separate exclusion lists** over that one mechanism,
and the difference is deliberate: `QA_PY_EXCLUDE_DIRS` excludes only
`.claude/ccy/plugins` and `.claude/ccy/file-history`, not the whole `.claude/ccy`
tree, because `.claude/ccy/claude-supervise.py` is **tracked** — it is committed,
shared between clones, and sits squarely inside ruff's default scope.
Unifying the lists would have *dropped* a real file from the Python gate — this
section's own defect, committed inside the fix for it.

**Tracked is not the same as ours.** `claude-supervise.py` is **daemon-owned**:
the hooks daemon rewrites it on every install and upgrade, so a local edit is
discarded. So are `.claude/init.sh`, `.claude/hooks/*`, the `*.sh` scripts under
`.claude/skills/hooks-daemon/scripts/`, and `CLAUDE/Plan/mkplan.bash` — all
tracked, none of them ours to edit.

**Which of those the gates actually open is a separate question**, and the two
must not be conflated. `.claude/init.sh`, `.claude/hooks/*`, `mkplan.bash` and
`claude-supervise.py` are all discovered and gated. The skill scripts are **not**:
`.claude/skills` is in `QA_EXCLUDE_DIRS`, so nothing under it reaches a gate —
confirm with `jq '.paths.scanned[]' /tmp/qa-results.json`, which lists zero paths
there. Upstream guarantees each daemon-owned file is clean under its language's
**default** rule set (`ruff --isolated`, shellcheck with no rc), which is why
gating the four costs nothing today. If an upgrade ever lands a finding under a
rule *this repo* chose, the remedy is to exclude the file (or narrow the rule —
see the `BLE` note in `ruff.toml`) and never to edit it, because the next upgrade
overwrites the fix. Report it upstream if it fails under default rules.

`qa-python.bash` joined them in Plan 00081. It had the identical defect and had
not learned from 00076: it discovered by extension **or the execute bit**, so six
tracked Python programs — mode 0644 with a `#!/usr/bin/env python3` shebang,
deployed 0755 by their plays — were never compiled or linted. Widening discovery
took it from 35 files to 41 and surfaced **31 real ruff findings** in ~4,000
previously-unread lines, while the old gate printed `✓ python: 35 files OK`. This
document's own "For Python files that use external libraries" note named
`wsi-stream` as *the* example of Python needing care; it was one of the six.

This exists because the two gates identified bash by **filename extension or file
mode**, and 27 of this repo's scripts have neither a `.sh`/`.bash` extension nor
an execute bit — including the 140 KB ccy launcher. They were never opened by
`bash -n`, shellcheck, or semgrep, while the gates printed `125 files OK`. Zero
coverage was already treated as a broken gate; *partial* coverage was not.

Two things changed, and neither is a file-mode change:

- Discovery keys on a **shell shebang, regardless of mode**. A `read` builtin, not
  `head`, because this runs against every file in the repo.
- Each gate **asserts its own coverage** and exits `2` on a shortfall, naming the
  files. `qa-bash.bash` compares its discovered set against every tracked shell
  script; `qa-patterns.bash` requires every file it handed semgrep to come back in
  `.paths.scanned`.

Semgrep needs the owner execute bit before it will read a shebang, so
`qa-patterns.bash` **copies** each discovered script into a temp mirror at its own
repo-relative path (appending `.bash` where it has no shell extension) and scans
that, mapping findings back to real repo paths. `chmod +x` was rejected as the
fix: `/var/local/colours` and `/var/local/ps1-prompt` are deployed `0644` because
they are sourced libraries, so an execute bit would be a lie told to a linter and
would still miss every future sourced library.

**If a gate reports a shortfall, widen the discovery — never exclude the file.**

> **This is one instance of a general defect class**, and treating it as a
> local quirk of these two gates is why it kept recurring elsewhere — four more
> instances surfaced in Plans 00079/00080. See
> [AgentNotes.md → *A partial result read as a complete one*](AgentNotes.md#a-partial-result-read-as-a-complete-one--guard-the-empty-case-miss-the-partial).

#### `.paths.scanned` is not proof a file was analysed

Semgrep lists a file it could not **parse** as scanned, returns zero findings for
it, and exits `0` — the reason appears only in `.errors[]`. Three of this repo's
scripts were in that state, the bash `ftp-camera` (2,475 lines; since ported to Go) among them, and two of
them were being scanned by the *old* gate too. All three pass `bash -n`.

`qa-patterns.bash` therefore checks the error list, and treats its two classes
differently because they mean different things:

- **`Syntax error`** — the file did not parse, and **every rule with a match in
  it lost that match** → **gating**. `SEMGREP_CANNOT_PARSE` is the exception
  list; it is **empty today** and self-expiring in both directions: an unlisted
  unparseable file fails the gate, *and* a listed file that starts parsing fails
  it until the entry is removed. Both former entries were the same construct —
  see below.

- **`PartialParsing`** — parsed apart from named ranges. Reported, not gating,
  and measured to cost **nothing** for this ruleset.

Both counts print on **every** run, pass or fail, above the `✓ patterns:` line.
A summary consisting only of a tick and a file count is the format that let this
sit unnoticed.

#### Semgrep parses a file only when a rule's regex has already matched

This is the fact that explains everything else in this section, and it was
established with a probe rule whose regex matches on every line, so the parse is
always attempted. Across ten measured (file, rule) cells the correlation is
exact:

| rule's raw regex matches in the file | parse attempted | error reported |
| ------------------------------------ | --------------- | -------------- |
| 0                                    | no              | none           |
| ≥ 1                                  | yes             | surfaces       |

So a rule that appears to "parse a file fine" may simply never have been asked.
An earlier revision of this document concluded from that appearance that
**parseability is a property of (file × rule)**; it is not, and the reasoning was
this section's own defect class one level up — an *absence of an error* read as
evidence of coverage.

#### `Syntax error` costs everything; `PartialParsing` costs nothing (today)

Both measured, not argued:

- `rclone-tail` carried a whole-file `Syntax error` and reported **0** findings
  while containing **3 real `|| echo` violations**. `rclone-cache-status` hid a
  fourth. All four were invisible for as long as the files were on the exception
  list.
- The bash `ftp-camera` (since ported to Go) reported a `PartialParsing` range of
  lines 585–2486, and a probe for `echo` returned **293 findings against 293 raw
  occurrences** — 262 of them on distinct lines *inside* that range.

The asymmetry is because every rule in `.semgrep/bash-conventions.yml` is
`pattern-regex`, and the regex engine reads raw text — the parse tree is never
consulted for a match. A `PartialParsing` range is therefore a note about a
**future** cost: the day an AST `pattern:` rule is added, those lines stop being
covered. The report keeps the magnitude visible so that day is noticeable (as
it read while the bash `ftp-camera` existed):

```
files/home/.local/bin/ftp-camera — 1902 of 2486 lines outside the parse tree (first gap from 585, last to 2486)
files/var/local/claude-yolo/claude-yolo — 1 of 3021 lines outside the parse tree (first gap from 1252, last to 1252)
```

Those two lines describe situations three orders of magnitude apart, and a bare
list of filenames rendered them identically. The number is the union of the
skipped ranges, so overlaps are not double-counted. The wording is
"outside the parse tree", not "not analysed" — the earlier phrasing was false in
the *alarming* direction, which is no better than false in the reassuring one.

#### The construct that defeats the bash grammar

Both former `SEMGREP_CANNOT_PARSE` entries reduced to one shape — a heredoc fed
directly to an `if` condition, with `then` on the line after the terminator:

```bash
if ! python3 - "$json" <<'PY' 2>/dev/null
...
PY
then
    echo "ERR|parse failed"
fi
```

That is valid bash (`bash -n` passes) and tree-sitter-bash cannot parse it. Feed
the heredoc to a **command substitution** instead and it parses cleanly:

```bash
if ! parsed=$(python3 - "$json" <<'PY' 2>/dev/null
...
PY
); then
    echo "ERR|parse failed"
else
    printf '%s\n' "$parsed"
fi
```

The rewrite is also better bash — the command's status and its output are
handled separately instead of the output flowing straight through the condition.

#### Two files may not claim one mirror path

The mirror appends `.bash` to a file with no shell extension, so `dir/foo` and
`dir/foo.bash` would both land on `dir/foo.bash`: the second `cp` overwrites the
first, and the mirror→repo map — merged from one object per file — keeps a single
value for that key. The overwritten script is then neither scanned **nor** named
by the coverage assertion, which reads the map's *values*. It would simply cease
to exist, and the gate would report a pass over it.

That is this section's own defect committed inside the fix for it, so a collision
is a hard failure (exit 2) naming both files rather than a silent rename. No
collision exists in the repo today; the check is there so one cannot appear
quietly.

### `qa-deployed-drift.bash` — the repo and the host must agree

This is the one QA check whose subject is the **host** rather than the source
tree. It exists because Plan 00094 fixed `files/home/.local/bin/ftp-camera` in the
repo and never ran the play that deploys it — the repo said "fixed", the machine
ran the old build, and it surfaced weeks later as a camera session that would not
copy. No source-reading check can see that: the source was correct.

It compares each repo-owned script against its deployed copy and, on a mismatch,
names the play to run — derived by searching the playbooks for the file's `src:`
path, not from a hand-maintained table. A file is checked **only when a deployed
copy already exists**, so a machine that never installed a feature is never
nagged.

**Scope is wider than `files/home/.local/bin/`.** Beyond the user scripts it also
compares the pairs in the `pairs` table of `src-go/qa/deployeddrift/run.go` — currently the VM
acceptance lab and the agent bus installer's files (Plans 00110, 00161). The tool is Go
(`SPEC-qa-deployed-drift.md`, E1-E18); the table is the authority on the current set. A pair
whose deploying play is missing, or whose glob matches no repo file, is **exit 2**, not a
silent pass; so is an unreadable file, a git that cannot say whether the checkout is a linked
worktree, and an unset `HOME`.

**It self-skips in three situations, and says so with `⚠`, not `✓`:**

| Skipped when                                  | Why it is not drift                                                                                                                                                               |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The repo root is `/workspace` (CCY container) | There is no deployed state in the container to compare against                                                                                                                    |
| The checkout is a **linked git worktree**     | A review branch in a worktree would be compared against whatever the MAIN checkout deployed, an unclearable hard failure — you cannot deploy a review branch just to run QA on it |
| The deployed directory does not exist         | The machine has never installed these scripts                                                                                                                                     |

A skip exits `0`, so `qa-all.bash` still passes. It is marked `⚠` — this repo's
"ran but incomplete" stage symbol, also used by `qa-bash.bash` and
`qa-patterns.bash` — because a `✓` among thirty others is indistinguishable from
a gate that actually compared something, which is the very confusion this gate
exists to remove. **This list is the only way to know when the gate is inert, so
a new skip condition must be added here in the same commit that adds it.**

**The deployed name is not always the repo name** (Plan 00081 F4).
`git-account-helper.j2` deploys as `git-account-helper`, so a basename comparison
looked for a `.j2` in `~/.local/bin`, never found one, skipped the file, and
still printed its pass line — Plan 00094's failure mode reproduced inside the
gate written to catch it. A `.j2` is now verified to have a real playbook `dest:`
under its stripped name (**exit 2** if not), and — since a rendered template
genuinely cannot be byte-compared — is **disclosed in the summary** rather than
silently omitted. The pass line also states how many scripts are not installed on
this host, because "2 match" and "2 match, 35 not installed" are different
sentences and only the first was ever printed.

**This changes when you run QA.** The table below says "before every commit", and
that is still right — but on the HOST this gate makes the repo's documented
`edit → playbook → deploy → test` order (see
[InfrastructureAsCode.md](InfrastructureAsCode.md)) **enforced** rather than
merely recommended: a changed script must be deployed before `qa-all.bash` will
pass. That is the intended sequence, not an obstruction — QA is a pre-*commit*
gate, and the workflow already puts deploy and test ahead of commit. If you are
mid-edit and want the other stages, run the deploy first; do not work around the
gate.

Run it alone with:

```bash
./scripts/qa-deployed-drift.bash
```

### gh-scope-outside-ssot

**What it is.** The single-source rule for GitHub OAuth scopes. There are two facts about
scopes, and each has exactly one home:

- **Which scopes a gh token must carry** lives in `vars/github-required-scopes.yml`.
- **How GitHub scopes imply one another** lives in `helpers/github_scopes/`. For example,
  an `admin:` scope covers the `read:` scope of the same name.

The rule (`.semgrep/gh-scopes.yml`) reports any other published file that names a scope, in
any language, docs and messages included. It reports two shapes:

- a name that can only be a GitHub scope: `prefix:name` with a GitHub prefix, or one of
  the underscore names;
- a literal list passed to `gh auth login` or `gh auth refresh` with `--scopes` or `-s`.
  That is where the bare names (`repo`, `gist`, `workflow`) turn up, and those are ordinary
  words anywhere else.

**Why it exists.** Copies drift. `run.bash` once logged in with no scopes, then made a
separate refresh for one named scope, while the play's audit checked the full list. The
owner therefore went through GitHub's authorisation more than once. The scope hierarchy was
also written out three times, once each in `run.bash`, `gh-account-setup.bash` and the
play. Found and defended under Defence Before Fix (Plan 00139).

**How to fix a finding.**

- **Code that needs the list:** read it from the file.
- **Code that needs to know what a token is missing:** ask the helper.
- **Messages and docs:** name the file, for example "every scope in
  `vars/github-required-scopes.yml`", and leave the scopes themselves out. The file is the
  place to read what each one is for.
- **Never** add an exclude to the rule to clear a finding. Excludes are only for files that
  cannot carry the hazard, such as a plan's prose or a changelog. Adding one is the
  owner's decision.

### model-present-without-weights

**What it is.** A function that names a Hugging Face cache `snapshots` path (the word
right after a quote or a slash) and never names `model.bin`. Run by
`qa-speech-to-text-rules.bash` over every tracked JavaScript and Python file.

**Why it exists.** A snapshot directory appears as soon as a download starts. The small
config files land first and the weights last, so an interrupted download leaves a
snapshot with no `model.bin`, which faster-whisper cannot load. Settings counted such a
snapshot as installed and offered the `auto` model. The warm server then failed to load
it and the panel reported a timeout. `wsi-model-manager` used the same test, so it
called the broken download "already installed" and refused to fetch it again. Found and
defended under Defence Before Fix (Plan 00156).

**How to fix a finding.** Judge a model present only when a snapshot holds `model.bin`,
the file `WhisperModel` loads:

```python
any((rev / "model.bin").is_file() for rev in snapshots.iterdir())
```

In GJS, check `snapshot.get_child('model.bin').query_exists(null)` for each snapshot.
Never satisfy the rule by mentioning `model.bin` in a comment while the check still
looks at the directory.

### dropdown-label-carries-explanation

**What it is.** A dropdown option label that explains itself instead of naming the
option. It is reported when it holds an em dash, a parenthesis, a comma, a semicolon or
a tilde, or runs to 25 characters or more. The rule reads every place the panel builds
labels: an `_addComboRow` call, the `WHISPER_MODELS` catalogue, and a `labels` array
built or pushed to at runtime. Run by `qa-speech-to-text-rules.bash`.

**Why it exists.** An `Adw.ComboRow` shows the selected option beside the row's title,
in whatever width is left, and ellipsizes the rest. Options such as
`Standard — load then start (~3-6s)` were cut off, so neither the chosen value nor the
choices could be read. The 25-character limit sits above the longest short name in the
panel (19) and below the shortest label that was truncated (34). Found and defended
under Defence Before Fix (Plan 00156).

**How to fix a finding.** Give the option a short name (`Standard`, `Sonnet`,
`Ctrl+Shift+V`). Move what it meant into the row's subtitle, or into the group's
description when several rows share it. A state such as "not installed" belongs in the
subtitle, not glued onto the option. Never escape the rule by spelling the explanation
with other punctuation: the rule's characters are a proxy for explanation, not the
thing banned.

### ready-wait-ignores-child-exit

**What it is.** A loop that waits for a process this code started to become ready,
sleeping between tries, and never asks on a try whether that process has already
exited. Run by `qa-ready-wait-rules.bash` in two halves:

- **Python** (`.semgrep/ready-wait.yml`): a loop that calls `time.sleep` or
  `asyncio.sleep`, in a function that starts a process with `subprocess.Popen` or
  `asyncio.create_subprocess_*`, and never calls `.poll()` or `.wait()`, reads
  `.returncode`, or probes with `os.kill(pid, 0)` or `os.waitpid`.
- **Bash** (the Go rule in `src-go/qa/readywait`): a loop that runs `sleep` and
  waits for something (it breaks, returns or exits, or is a `while`/`until` on a test),
  after a background start (`cmd &`, or a call to a function that starts one and keeps
  its `$!`) in the same function or top level, or in a function called from there. A
  start that hands a guest to a manager and returns (`lxc-start`, `virsh start`,
  `virt-install`, run as a command, or a function that runs one) arms it the same
  way: the guest is the child. It is cleared by `kill -0`, `ps -p` or `wait -n`, or by
  asking the manager for the guest's state (`lxc-info -s`, `lxc-ls --running`,
  `virsh domstate`, `virsh list`), in its header or body, or in a function the loop
  calls by name (one level deep: a check made through a further function is not
  seen). `wait` on the child, or the end of a `case` arm, ends the scope's armed
  state. Semgrep is not used for bash because its parser rejects about a quarter of
  this repo's scripts.

**Why it exists.** A failure known within seconds is hidden behind the whole timeout
and reported as "timed out", without the process's own error. The speech server ran out
of GPU memory and exited after 6 s; `wsi-stream` kept polling its socket for 45 s and
told the panel "timeout", so the CUDA error never reached the owner. Found and defended
under Defence Before Fix (Plan 00156).

**How to fix a finding.** Keep the handle or the pid, and on every try ask whether the
process is still there before deciding to sleep again. When it has gone, stop at once
and report its own error: its exit status, the first error in its log, or its stderr.

```python
server = subprocess.Popen(cmd)
for _ in range(90):
    time.sleep(0.5)
    alive = server.poll() is None
    if is_server_running():
        return True
    if not alive:
        raise RuntimeError(f"the server exited: {server_failure()}")
```

```bash
tmate -F &
tmate_pid=$!
for _ in $(seq 1 30); do
    if kill -0 "$tmate_pid" 2>/dev/null; then alive=1; else alive=0; fi
    if tmate show-messages | grep -q "read only"; then break; fi
    if [ "$alive" -eq 0 ]; then
        status=0; wait "$tmate_pid" || status=$?
        echo "ERROR: tmate exited (status $status) before it was ready" >&2
        exit 1
    fi
    sleep 0.5
done
```

Ask liveness before readiness, as above: a child that became ready and then exited in
the same moment is then still reported ready. Never clear a finding by naming
`kill -0` somewhere it does not decide anything; the words are what the rule reads, a
check that changes the outcome is what it stands for.

**Where no rule reaches, and why.** These forms of the class are fixed by hand and
reviewed, not detected:

- **A handing-off start the rule does not name.** Only `lxc-start`, `virsh start` and
  `virt-install` are known to hand a guest to a manager; another daemonizing start
  (`podman run -d`, `systemctl start`, a tool's own `--daemon`) is knowledge about the
  tool, not the text. Check the manager's state on each try.
- **Ansible.** `wait_for:` and `until:` after a `systemd` start wait for a socket or a
  daemon's own answer; which unit serves that socket is in the unit's configuration,
  not the task. Read the unit's `ActiveState` in the same retried command, stop on
  `failed` or `inactive`, and fail on that.
- **GNOME Shell extensions.** A timer cleared by a D-Bus signal stands in for the wait,
  and a spawn with `GLib.spawn_command_line_async` has no handle at all. Spawn with
  `Gio.Subprocess` and `wait_async`, and end the wait when the child exits first.
- **A wait in a later run of the script.** A step script that starts a process in one
  invocation and waits for it in the next keeps the pid in a file; the rule reads one
  file's text, in order, and cannot follow that.

### pause-without-register

**What it is.** An `ansible.builtin.pause` (or bare `pause:`) task under `playbooks/` or
`tasks/` that has no `register:` and no `# PAUSE-OK: <reason>` comment. Run by
`qa-ansible-pause.bash`. The comment may sit on any line of the task or on the comment
lines directly above its `- `. A `PAUSE-OK:` with no reason after it does not count.

**Why it exists.** The owner runs plays unattended in a batch (`run.bash --changed --yes`,
from `CLAUDE/Plan/meta-deploy.bash`), and the batch must not carry confirmations of its
own. A play that ended on "Press ENTER to continue" just to show its closing
instructions hung such a batch on a question nobody knew was owed.

**How to fix a finding.**

- **Status, instructions, a reboot reminder:** use `ansible.builtin.debug` with `msg:`
  carrying the same text, minus "Press ENTER". It prints and moves on.
- **A value or decision the play uses afterwards:** `register:` it, and put a `when:` on
  the task so it asks only when the value is missing, never on a rerun that already has
  it.
- **A pause that must stay without a register** (a fixed few-second wait for a service,
  a wait while the owner logs in somewhere that the next task then checks, an
  Enter-or-Ctrl+C risk decision): annotate it `# PAUSE-OK: <why it has to wait>`. Prefer
  a check that fails with an actionable message over a wait, where one is possible.

---

## Changing a Gate

A gate is what `CLAUDE.md` makes mandatory before every Bash/Python/Ansible
commit. A bug in one is worse than a bug in the code it checks, because it is
silent: the gate returns a confident exit code either way. Two failure modes
have already shipped here, and every change to a gate is measured against them.

**A gate that scans nothing and reports a pass.** `find -path` matches the
**whole** printed path, so an unanchored `*/untracked/*` exclusion once excluded
an entire checkout — this repo is vendored at `untracked/repos/fedora-desktop`
inside another project, and every bash file went unscanned with exit 0
throughout. Anchor repo-root-relative exclusions to `$REPO_ROOT` (the shared
exclusion test of the Go discovery (`src-go/qa/discovery`) tests the repo-relative path
for this reason; it never sees an ancestor of the root) and leave only genuinely any-depth names (`.git`, `node_modules`, `__pycache__`, `.venv`,
`venv`) unanchored. **Never remove a zero-file guard** to make a gate "work"
somewhere — zero is the signal.

**A gate that scans the wrong things.** `qa-python.bash` once had two discovery
passes and only one carried the venv exclusions, so it linted third-party `pip`
scripts; `qa-ansible.bash` once matched a *comment* documenting the removal of
`ignore_errors: true`. Keep multi-pass discovery on one shared exclusion list,
and strip comments before matching a source pattern — after checking whether
the annotation you rely on (`# FAIL-FAST-OK:`) itself lives in a comment.

**Prove a change with a control that could have failed.** A gate turning green
proves nothing on its own. Build a fixture and check that the gate
**discriminates**: the bad case flags, the good case does not, and the
near-miss cases (annotated, or with an unrelated trailing comment) each behave
correctly. A uniform failure across unrelated assertions means you broke the
fixture, not that the checks work.

**Most gates have no tests of their own**; their fixes were proven with
hand-built fixtures that were then discarded. If you are changing one, consider
whether the fixture should become permanent. `qa-docs.bash` is the exception
and the model: its logic lives in the Go package `src-go/qa/docs` (specification
`SPEC-qa-docs.md` in Plan 00192, expectations D1-D25) with unit tests beside it and real-git
tests in `src-go/qa/cmd/qa-docs`, which the Go gate runs. Its slug cases are pinned
against anchors observed working in rendered documents, because that checker's
first implementation was wrong in the same way as the defects it hunts and
under-reported them.

---

## The QA Toolchain Is Pinned, Because a Verdict Belongs to Its Binary

`/.qa-versions` is the single source of truth for the version of every tool a gate
runs — ruff (`qa-python`), semgrep (`qa-patterns`), shellcheck (`qa-bash`), golangci-lint (`qa-go`). Four
consumers read it, so none can drift from the others:

| Consumer                            | Role                                                                            |
| ----------------------------------- | ------------------------------------------------------------------------------- |
| `scripts/qa-toolchain.bash`         | asserts the machine, every `qa-all` run                                         |
| `playbooks/imports/play-python.yml` | installs it on the host (the Go toolchain via the `play-golang.yml` it imports) |
| `.claude/ccy/Dockerfile`            | bakes it into the CCY image                                                     |
| `.github/workflows/qa.yml`          | installs it in CI                                                               |

Unpinned, the divergence is silent: each environment is confident and they
disagree. shellcheck 0.9.0 and 0.11.0 reported 173 and 141 issues on the same
tree. Tools no gate runs (ansible-lint, yamllint) are deliberately **not** in the
file: asserting them would gate on nothing, and a standalone pinned ansible-lint
would carry its own ansible-core beside the one `run.bash` injects into `ansible`.

`ruff.toml` additionally enumerates `select` explicitly (`E4`, `E7`, `E9`, `F`)
so the ruleset does **not** drift with ruff's own default set — an unenumerated
default once turned `main` red with no commit behind it. The version is pinned
on top of that, because a bump can change how the same selected rules behave.

If an assertion fails, match the pin — do not "fix" findings a different binary
invented. **Bumping a pin is a deliberate act**: change the value, triage
whatever the new version reports, and rebuild the CCY image (`ccy --rebuild`),
or every container keeps asserting the old value and fails.

Host and container are brought into line by `./playbooks/imports/play-python.yml`
and `ccy --rebuild` respectively; the gate's failure output names both.

On the host, `play-python.yml` also `pipx pin`s the ruff and semgrep venvs,
because `shutdown-with-update` runs `pipx upgrade-all` and would otherwise move
them off the pin. pipx refuses to `reinstall` a pinned venv, so a rebuild after a
Fedora upgrade unpins first and re-runs the play to re-pin — the steps are in
[docs/post-upgrade.md](../docs/post-upgrade.md).

A tool the play finds at another version is **removed, then installed**, never
`install --force`d: under pipx's uv backend `--force` re-runs `uv venv` on the
existing venv, which uv refuses ("A virtual environment already exists"), and pipx
then will not remove a venv it did not create, so the play would fail instead of
converging. `test-qa-tool-pin-converges.bash` holds the play to that shape. If that
error comes back, start from Plan 00142
([Completed/00142-qa-tool-pin-does-not-converge-under-pipx-uv](Plan/Completed/00142-qa-tool-pin-does-not-converge-under-pipx-uv/)):
its `reproduce-pipx-uv.bash` replays the play's tasks against a real pipx and uv
offline.

Suppression comments (`# noqa`, `# type: ignore`, `# shellcheck disable`) are
blocked by the hooks daemon at `Write`/`Edit` time. Fix the code, or exempt the file in
`ruff.toml` with a stated reason. The shell gate also enforces the ban itself, past the
daemon: `qa-bash.bash` fails any `# shellcheck disable=` directive that
`scripts/shellcheck-disable.allowlist` does not record. The list is a ratchet like
`scripts/bash-limits.allowlist`: each record is a path, a count of directives and their codes;
it may be lowered by hand when a directive is removed (the gate fails on a record above what
the file holds) and never raised or added to (the gate compares it with `QA_BASH_LIMITS_BASE`, the same base CI sets for the
bash-limits list, else the merge-base).
`disable=all` is never allowable, even listed, and a `# shellcheck shell=` that contradicts a
shebang (`shell=bash` on `#!/bin/sh`) fails; `shell=` on a file with no shebang is correct use.
To get a new directive in, fix the code instead. A `.shellcheckrc` is never a place for
suppressions: the gate runs `--norc`, so any such file is ignored (the one that sat beside
`claude-yolo`, comments only, was deleted).

---

## GNOME Shell Extension JavaScript

Run ESLint via the binary directly (NOT `npm run lint` — blocked by hooks):

```bash
cd extensions && node_modules/.bin/eslint speech-to-text@fedora-desktop/extension.js
```

---

## Helper Unit Tests + Extension Version Compatibility

**Both of these now run inside `qa-all.bash`** (Plan 00081 F11) — you do not need
to invoke them separately. The commands below are for running one on its own
while iterating.

Helper packages under `helpers/` are stdlib-only (`helpers/CLAUDE.md`). Their unit
tests are namespace-package modules, so `unittest discover` cannot collect them —
run them with the dedicated runner, which enumerates `tests/helpers/**/test_*.py`
and runs them by explicit module name:

```bash
./scripts/qa-helper-tests.bash
```

A separate **static** gate confirms every `extensions/<uuid>/metadata.json`
declares support for the GNOME Shell major that this branch's Fedora release ships
(`vars/fedora-version.yml`). It is session-free (unlike the runtime
`helpers.gnome.verify_extension`), so it runs in CI on the repo source:

```bash
python3 -m helpers.gnome.check_extension_compat
```

The Fedora→GNOME-Shell map lives in `helpers/gnome/fedora_compat.py`
(`FEDORA_TO_GNOME_MAJOR`). When cutting a new `F<N>` branch, add that release's
GNOME major there — an unmapped Fedora version fails the gate by design, forcing a
human to confirm the GNOME version. Both run automatically in the `helpers` CI job
(`.github/workflows/qa.yml`).

---

## Retired: the CCY ctrl+z patch gate

`./scripts/qa-ctrl-z-patch.bash` and its `scripts/qa-ccy/` npm harness were
deleted in CCY 3.42.0 along with the patch they tested. The gate existed only
because the patch rewrote an anchor inside a minified upstream artifact and had
to be re-proven against each Claude Code release. Suppressing ctrl+z is now the
hooks-daemon PTY supervisor's job, outside Claude Code entirely, so there is
nothing version-coupled left to gate — see
[ContainerRules.md](ContainerRules.md#ctrlz-sigstop-suppression-is-the-supervisors-job--do-not-re-add-a-patch).

---

## `qa-changed.bash` — diff-scoped QA while iterating

`./scripts/qa-changed.bash` runs only the checks the changed files need, so the slow
`qa-all.bash` is needed once before a push rather than before every commit. It is Go
(`src-go/qa/changed`, command `src-go/qa/cmd/qa-changed`, in the nested QA module): the script
is a small wrapper that builds the command into a private directory under the cache and runs it,
through the shared `scripts/lib/go-tool.bash` (which `qa-bash-limits.bash` uses too), so callers, flags, streams and exit codes are the ones agents,
hooks and humans always used. Changed files are `git diff --name-only HEAD` plus untracked,
not-ignored files; `--base <ref>` (e.g. `origin/F44`) diffs against a ref so a burst of local
commits is covered, `--plan` prints the chosen checks without running them, `--files F...` names
the files explicitly (flags may follow them; `--` ends the flags). A git failure or a bad ref is
exit `2`, never "nothing to check". Exit `0` pass or nothing changed, `1` a check failed, `2` bad
usage, a needed tool missing, or a gate that could not run (it exited 2 itself; reported as
`could not run (exit 2)`, and a failed check outranks it). Every selected check runs, in a fixed
order and one at a time (as in `qa-all.bash`), so one run names everything broken; each has a
time limit (`QA_CHANGED_TIMEOUT`, `30m` by default, `0` disables) and is killed with its
children when the limit passes. The tools every planned check needs are checked once, before the first
check runs.

| Changed file                                        | Checks                                                                                                                                                                                        |
| --------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Shell (`.sh`/`.bash`/shebang)                       | the toolchain pin, `bash -n`, shellcheck (`error`/`warning` gate, as in `qa-bash.bash`), the semgrep bash rules via the same temp-mirror idea                                                 |
| Python                                              | the toolchain pin, ruff, and the mirrored `tests/helpers/x/test_y.py` for `helpers/x/y.py` (a changed test runs itself)                                                                       |
| Playbook yml                                        | `ansible-playbook --syntax-check` on it, `qa-ansible.bash` and `qa-docs.bash` whole                                                                                                           |
| Task / vars yml                                     | `--syntax-check` on every playbook naming the file by basename (one level), `qa-ansible.bash` whole                                                                                           |
| Markdown                                            | `qa-docs.bash` whole                                                                                                                                                                          |
| `.js`/`.mjs`                                        | `node --check`; under `extensions/`, the project's eslint on those files with the js gate's pins; a change to the js tool, its wrapper or `extensions/.eslintrc.json` runs `qa-js.bash` whole |
| `src-go/`, any file                                 | `qa-go.bash` whole, and the suites of every command whose build closure holds the file (`go list -deps`, `scripts/go-binary-suites.tsv`)                                                      |
| `src-go/go.mod`, `go.sum`, `vendor/`, the allowlist | `qa-go-deps.bash` and `qa-go-vuln.bash` whole (the allowlist: `qa-go-deps.bash`; `src-go/qa/go.mod`, govulncheck's pin: `qa-go-vuln.bash`)                                                    |
| Any bash file, the allowlist                        | `qa-bash-limits.bash` whole                                                                                                                                                                   |
| Any non-markdown file                               | each `test-*.bash` suite and hard gate script that `scripts/qa-gates.tsv` lists and whose text names the file by path or basename, and any suite that itself changed                          |

The classification is data, not code: `scripts/qa-routing.tsv` (the rules, in order: which
files get which checks, which are covered, which honour an excluded tree) and
`scripts/qa-trees.tsv` (the excluded trees, one list, read by the Go readers in
`qa/changed`, `qa/patterns` and `qa/discovery`; a table with no `shell` row or no `python`
row is exit 2 in all of them). All load
strictly, a malformed row is exit 2 naming the line. A deleted file has nothing left to check
but still owes the whole gates it implies (the bash-limits gate for a deleted bash file or
allowlisted path, the Go gate and the command suites for a deleted `src-go/` file) and selects
the suites that name it. The gate scripts take no file list, so the same tools are invoked on
the changed files; the gates that are cheap (`qa-ansible.bash`, `qa-docs.bash`) run whole. A
changed file that maps to no check — a workflow, a `.j2` no suite names — is listed as `not covered by qa-changed`, never skipped silently. Left to `qa-all.bash`: the Python-template
compile, semgrep parse-coverage assertions, helper-test counts, extension-compat,
panel-contract, deployed-drift, and every suite no changed file is named in.

The proofs: the Go tests beside the code, the golden replay
(`src-go/qa/changed/golden_test.go`: the bash's own output over a fixed world, recorded from
git revision `f0284848` by `CLAUDE/Plan/00192-.../qa-changed-golden/capture.py`; each
difference is a numbered bug of that plan's `SPEC-qa-changed.md` section 10 pinned in
`testdata/deviations.json`), and `test-qa-changed.bash`, the black-box suite, which runs
unmodified against the wrapper or, with `QA_CHANGED_BIN=<built binary>`, against the binary.

---

## When to Run What

| Changed files        | While iterating (before each commit)                           | Before every push       |
| -------------------- | -------------------------------------------------------------- | ----------------------- |
| Bash or Python files | `./scripts/qa-changed.bash`                                    | `./scripts/qa-all.bash` |
| Extension JavaScript | `./scripts/qa-changed.bash` (runs eslint on the changed files) | `./scripts/qa-all.bash` |
| Ansible playbooks    | `./scripts/qa-changed.bash` (syntax of the affected playbooks) | `./scripts/qa-all.bash` |

---

## What QA Catches

- ✅ Bash syntax errors (`bash -n` validation)
- ✅ shellcheck `error` **and `warning`** findings (`info`/`style` advisory). **shellcheck is required** — absent, the stage exits 2 rather than reporting a pass it did not earn (Plan 00075). The bar was raised to `warning` because **SC2155** (`local x=$(cmd)` — `local` becomes the command whose status is reported, discarding the substitution's) **is the discarded-failure-signal class**, and it was sitting in the advisory bucket nobody reads. `info`/`style` stay advisory on purpose: they are dominated by SC2016 and SC2012, where gating would trade signal for noise
- ✅ **Discarded failure signals** (Plan 00075, `.semgrep/bash-conventions.yml`) — the class where a command's failure is silently turned into data that is then trusted:
  - `bash-status-after-block` — `$?` read after `fi`/`done`/`esac`/`}`, which is the *block's* status and so is always 0 after a successful `if`. **shellcheck does not catch this even with `--enable=all`** (verified)
  - `bash-capture-discards-status` — `var=$(cmd 2>/dev/null)` with the status thrown away, so an error written to stdout becomes the value. Scoped to `files/var/local/claude-yolo/**` for now; widening is tracked in Plan 00075. Genuine cases carry a same-line `# FAIL-FAST-OK: <reason>`
- ✅ Python syntax errors (`python3 -m py_compile`)
- ✅ Common Python issues (via `ruff` — **required**; `qa-python.bash` exits 2 with an error if ruff is absent; a ruff crash, exit ≥ 2, is also a hard failure)
- ✅ Error-hiding bash patterns (`|| echo` — Semgrep, `.semgrep/bash-conventions.yml`)
- ✅ Ansible fail-fast violations (`failed_when: false` without `# FAIL-FAST-OK:` annotation)
- ✅ Ansible playbook **syntax** errors (`ansible-playbook --syntax-check`, catches the 2.19 parse hazards)
- ✅ Playbook hygiene (every `- hosts:` playbook has the `ansible-playbook` shebang + exec bit)
- ✅ JavaScript syntax (`node --check`) + ESLint across `extensions/`

## What QA Does NOT Catch (Known Limitations)

- ❌ **Runtime API incompatibilities** — e.g., calling a library method with parameters it no longer accepts
- ❌ **Import errors** — missing dependencies only fail at runtime
- ❌ **Logic errors** — code that runs but produces wrong results
- ❌ **Everything judgement-shaped** — work put in the wrong place in the IaC graph, a
  new playbook that should have been an edit to an existing one, names that describe a
  mood rather than a behaviour, a missing version bump, plan/docs drift, a self-test
  that does not exercise the code path it vouches for, or a real identifier about to be
  posted to a public surface. **Use the `qa-reviewer` agent for these** — see below.

---

## The `qa-reviewer` Agent — Required Before Marking a Plan Complete

`qa-all.bash` is mechanical: syntax, lint, greps, playbook parsing. It passes green on
changes that are structurally wrong. `.claude/agents/qa-reviewer.md` is the holistic
gate for that class of defect.

**Run it as the final step of every plan, and to review any PR or branch diff:**

> Use the qa-reviewer agent to review this plan's changes before I mark it Complete.

It is **read-only** — it reports findings with `file:line` evidence and a verdict
(BLOCK / FIX-BEFORE-MERGE / PASS WITH NITS / PASS); it never edits. Its checklist is
built from this repo's own rules and the mistakes already made here (recorded in
`CLAUDE/AgentNotes.md`), so it grows as new ones are found — when a defect gets past
it, add that case to the agent rather than only fixing the instance.

**For Python files that use external libraries** (like `wsi-stream` using RealtimeSTT):

- After editing, **manually test the script** to verify it works
- Library APIs can change between versions
- Syntax checking alone is not sufficient for integration code

---

## Example Workflow

For a file under `files/home/.local/bin/`, deploy BEFORE running QA — the
deployed-drift gate compares the repo against the host, so it fails by design
while a changed script is still undeployed:

```bash
# 1. Make changes
vim files/home/.local/bin/wsi-stream

# 2. Deploy and TEST the actual script (on HOST, not in CCY container)
./playbooks/imports/optional/common/play-speech-to-text.yml
~/.local/bin/wsi-stream --help  # Verify it imports/runs

# 3. Run the diff-scoped QA — now the repo and the host agree
./scripts/qa-changed.bash

# 4. Commit, then run the full gate and push once it passes
git add files/home/.local/bin/wsi-stream
git commit -m "fix: update wsi-stream"
./scripts/qa-all.bash && git push
```

For changes that deploy nothing to `~/.local/bin/` (playbooks, `scripts/`,
`helpers/`, extension JS), the order does not matter — run QA whenever, as long
as `qa-changed.bash` passes before the commit and `qa-all.bash` before the push.

## Rules Summary

1. **Run `./scripts/qa-changed.bash` before EVERY commit** that touches Bash, Python, Ansible or extension JavaScript
2. **Run `./scripts/qa-all.bash` before EVERY push** and push as soon as it passes; a burst of commits may share one full run
3. **Fix all errors** before committing — QA failures indicate broken code
4. **Do not skip QA** — even for "small" changes
5. **Never add a suppression directive** (`# shellcheck disable`, `# noqa`, `# type: ignore`) — fix the code, or exempt in config with a stated reason

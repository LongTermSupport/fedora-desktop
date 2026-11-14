# QA — Quality Assurance Scripts

## Primary Rule

**Two commands, two moments:**

```bash
./scripts/qa-changed.bash   # while iterating: before each commit — only the checks the diff needs
./scripts/qa-all.bash       # all gates, all suites — GitHub CI runs it after a protected branch moves
```

**GitHub CI's verdict is the gate for a protected branch** (F44, F43, F44-arm: the `exempt`
list of `scripts/branch-policy.toml`). **Diff gate, full after** (issue #245): work is pushed
to a `ci/<topic>` branch, CI runs `qa-changed.bash` in diff mode there against the tip of the
protected line the branch is built on, and when it is green that exact commit is pushed to the
protected branch; the pre-push hook refuses any other move
([push-without-qa-verdict](#push-without-qa-verdict)). The push to the protected branch then
runs the full `qa-all.bash` in CI; a red run there is fixed forward, by the same route. No local
`qa-all.bash` is required before a push:

```bash
git push origin HEAD:refs/heads/ci/<topic>   # 1. CI runs qa-changed.bash (diff mode) on this commit
gh run watch                                 # 2. or: gh run list --commit <sha>
git push origin <sha>:F44                    # 3. the same commit, once CI is green; qa-all runs on F44
git push origin --delete ci/<topic>          # 4. the branch is merged now, so it may go
```

What CI runs is decided per run by `scripts/qa-ci-scope.bash` (Go: `src-go/qa/ciscope`, C1-C12),
the workflow's first job: the full `qa-all.bash` on a push to a protected branch, a
`release-<version>` branch or a release tag; `qa-changed.bash --base <tip>` on a push to any
other branch, against the tip of the protected line it contains (the default branch when it
contains it, otherwise the one other line, so F43 and F44-arm work is judged against its own
line); and diff mode against the base branch for a pull request. Every branch other than a
protected one is judged in diff mode, not only `ci/`: one rule is simpler than a list, and the
full suite judges whatever lands anyway.

The two checks that need the local machine: the hooks-daemon handler tests run in CI under the
exemption `QA_HOOK_HANDLERS_EXEMPT` (CI has no daemon), in both modes, and `qa-deployed-drift`
finds no deployed copies on a runner and reports itself skipped. Both run for real in a local
`qa-changed.bash` before each commit. `qa-changed.bash` is a diff-scoped check, not a substitute
for the full suite: it cannot see a gate that no changed file names, and a file it does not cover
is checked only by the full run after the move. See
[qa-changed.bash](#qa-changedbash--diff-scoped-qa-while-iterating).

**NEVER use the individual gate scripts directly** (`qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`) — use `qa-changed.bash` or `qa-all.bash`.

**QA never runs in parallel, and always runs low.** A second run, from any worktree, CCY
container or the host, **waits** for the first rather than failing, and every run is nice 19,
ionice idle. See [QA runs one at a time](#qa-runs-one-at-a-time-the-qa-lock-and-low-priority).

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

| Script                       | Checks                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Files                                                                                                                                                                                                                              |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `qa-bash.bash`               | The Go tool `src-go/qa/bash` (spec `SPEC-qa-bash.md`): `bash -n` (always) + shellcheck (**required** — exits 2 if absent, Plan 00075), run with `--norc --external-sources` and an empty `SHELLCHECK_OPTS`, so no `.shellcheckrc` or environment option can change the rules. Exits 2 if discovery finds **0 files**, and (Plan 00076) if it misses **any tracked shell script**. **shellcheck `error` AND `warning` findings GATE** (raised in Plan 00075 — SC2155 is this repo's own defect class); `info`/`style` advisory. **Suppression is gated too** (SPEC S16-S20): a `# shellcheck disable=` directive in any gated file fails unless `scripts/shellcheck-disable.allowlist` records it (a per-file count and codes that only move down, compared with the merge-base so an entry cannot be added or raised); `disable=all` never passes; a `shell=` that contradicts the file's shebang fails. `qa-changed` runs the same check on the changed files (`shell-directives`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Repo-owned bash (excludes `roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`, `.claude/skills`)                                                                                                                                 |
| `qa-python.bash`             | Go (`src-go/qa/python`, spec `SPEC-qa-python.md`): one `python3 -I` compile of every file (nothing written into the tree) + ruff (`--no-cache`; exit ≥ 2 = hard fail; no `--fix` mutation). Scope is what git would publish. Exits 2 if discovery finds **0 files**, if ruff does not list **every** file it was given (`ruff check --show-files`), or if `.qa-versions` has no single `RUFF=` pin. A ruff of another version than the pin **fails** the gate (exit 1) without linting. A `.j2` with a Python shebang is **rendered then compiled** — `{{ … }}` → `None`; a `{% … %}` statement is a hard failure, never a skip                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | Repo-owned Python files — discovered by extension **or shebang, regardless of file mode**; plus Python `.j2` templates, syntax-checked but not linted                                                                              |
| `qa-patterns.bash`           | Semgrep rules from `.semgrep/bash-conventions.yml` (`\|\| echo` and other error-hiding patterns), applied by the Go tool `src-go/qa/patterns` (script is a build-and-run wrapper; spec `SPEC-qa-patterns.md`). Scans only what git would publish, in a temp mirror so coverage does not depend on file mode, and exits 2 if any discovered file is absent from `.paths.scanned`, could not be parsed, or the rules fail their fixture self-test (Plan 00076); also [ccy-container-default-pids-limit](#ccy-container-default-pids-limit)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | Repo-owned bash                                                                                                                                                                                                                    |
| `qa-gh-scopes.bash`          | Semgrep rule `gh-scope-outside-ssot` (`.semgrep/gh-scopes.yml`, proven against `.semgrep/gh-scopes.fixture` every run): a GitHub OAuth scope named anywhere but its single source. The Go tool `src-go/qa/ghscopes` (script is a build-and-run wrapper; spec `SPEC-qa-gh-scopes.md`) hands semgrep every file git would publish by name and exits 2 if one it should have read is absent from `.paths.scanned` and `.paths.skipped`, if semgrep reports any error, or if nothing was scanned. See [gh-scope-outside-ssot](#gh-scope-outside-ssot)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | Every tracked regular file, any language, minus `scripts/qa-trees.tsv` (but `.semgrep`) and the rule's excludes                                                                                                                    |
| `qa-find-race.bash`          | Semgrep rule `find-race-negated-stat-test` (`.semgrep/find-race.yml`, proven against `.semgrep/find-race.fixture` every run): a find that ignores readdir races and matches on a negated stat test, which a file deleted during the walk satisfies (issue #152). The engine of `src-go/qa/ghscopes` run with its `FindRace` spec (command `src-go/qa/cmd/qa-find-race`), with the same scope and coverage refusals as `qa-gh-scopes.bash`. See [find-race-negated-stat-test](#find-race-negated-stat-test)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | Every tracked regular file, any language, minus `scripts/qa-trees.tsv` (but `.semgrep`) and the rule's excludes                                                                                                                    |
| `qa-arch-pin.bash`           | Semgrep rule `arch-pinned-artefact` (`.semgrep/arch-pin.yml`, proven against `.semgrep/arch-pin.fixture` every run): a download, package or image fixed to one CPU architecture with no selection by the architecture the build or host runs on (Plan 00243). Blocking, with no allowlist. The engine of `src-go/qa/ghscopes` run with its `ArchPin` spec (command `src-go/qa/cmd/qa-arch-pin`), with the same scope and coverage refusals as `qa-gh-scopes.bash`; `qa-changed` runs it whole for a change to any file kind it guards (a Dockerfile, a play, task or vars file, a `files/` tool, a workflow, an installer, a helper or Go command) and when its rule, fixture, wrapper or tool change, and `qa-all` and CI judge every change. See [arch-pinned-artefact](#arch-pinned-artefact)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Every tracked regular file, any language, minus `scripts/qa-trees.tsv` (but `.semgrep`) and the rule's excludes                                                                                                                    |
| `qa-argv-leak.bash`          | Semgrep rules `secret-in-argv` (`.semgrep/argv-leak.yml`, proven against `.semgrep/argv-leak.fixture.yml` every run): a passphrase, password or other secret on a process's command line, or rendered by Ansible into one (a task's command text, `environment:`) (issue #220). Blocking, with no allowlist. The engine of `src-go/qa/ghscopes` run with its `ArgvLeak` spec (command `src-go/qa/cmd/qa-argv-leak`), with the same scope and coverage refusals as `qa-gh-scopes.bash`; `qa-changed` runs it whole as check `argv-leak` for a change to a shell script, a play, task or vars file, a `files/` tool, a workflow, an installer, a helper or Go command, and when its rules, fixture, wrapper or tool change. See [secret-in-argv](#secret-in-argv)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | Every tracked regular file, any language, minus `scripts/qa-trees.tsv` and the rule's excludes                                                                                                                                     |
| `qa-place-issued-token.bash` | Semgrep rule `token-bundle-into-place-issued` (`.semgrep/place-issued-token.yml`, proven against `.semgrep/place-issued-token.fixture` every run): a token-credential bundle piped into `teamchat seat place-issued`, which places only a pool seat's password (issue #248). Blocking, with no allowlist. The engine of `src-go/qa/ghscopes` run with its `PlaceIssuedToken` spec (command `src-go/qa/cmd/qa-place-issued-token`), with the same scope and coverage refusals as `qa-gh-scopes.bash`; `qa-changed` runs it whole as check `place-issued-token` for a change to the user docs, the `CLAUDE/` tree or teamchat's Go, and when its rule, fixture, wrapper or tool change. See [token-bundle-into-place-issued](#token-bundle-into-place-issued)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | Every tracked regular file, any language, minus `scripts/qa-trees.tsv` and the rule's excludes                                                                                                                                     |
| `qa-boot-hold.bash`          | Semgrep rule `unit-holds-boot-target` (`.semgrep/boot-hold.yml`, proven against `.semgrep/boot-hold.fixture` every run): a systemd unit a boot target wants, whose declared start is longer than 90 s and that is not ordered after that target, so boot waits for it (issue #263). Blocking, with no allowlist. The engine of `src-go/qa/ghscopes` run with its `BootHold` spec (command `src-go/qa/cmd/qa-boot-hold`), with the same scope and coverage refusals as `qa-gh-scopes.bash`; `qa-changed` runs it whole as check `boot-hold` for a change to a `files/` unit or template, a play or task, a helper or Go outside the QA module, and when its rule, fixture, wrapper or tool change. See [unit-holds-boot-target](#unit-holds-boot-target)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Every tracked regular file, any language, minus `scripts/qa-trees.tsv` and the rule's excludes                                                                                                                                     |
| `qa-secret-screen.bash`      | Semgrep rules `secret-on-normal-screen`, `secret-read-echoed`, `secret-on-normal-screen-python` and `secret-on-normal-screen-ansible` (`.semgrep/secret-screen.yml`, each proven against its fixture `.semgrep/secret-screen.bash`, `.py` and `.fixture.yml` every run): a password, token or key written to a terminal's normal screen, where the scrollback and tmux history keep it (issues #255 and #261). Blocking, with no allowlist. The engine of `src-go/qa/ghscopes` run with its `SecretScreen` spec (command `src-go/qa/cmd/qa-secret-screen`); `qa-changed` runs it whole as check `secret-screen` for a shell or Python file anywhere, a play, task or vars file, a `files/` tool or template, a helper or the vault helper, and when its rules, fixtures, wrapper or tool change. Go is the project rule `project_secret_scrollback`'s. See [secret-on-normal-screen](#secret-on-normal-screen)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Every tracked regular file but Go, documents and test suites, minus `scripts/qa-trees.tsv` and the rules' excludes                                                                                                                 |
| `qa-walk-open.bash`          | Semgrep rule `walk-opens-non-regular-file` (`.semgrep/walk-open.yml`, proven against `.semgrep/walk-open.py` every run): Python that opens an entry a directory walk found without checking it is a regular file, so a named pipe blocks it for ever (issue #191). The engine of `src-go/qa/sttrules` run with its `WalkOpen` spec (command `src-go/qa/cmd/qa-walk-open`), with the same scope and coverage refusals as `qa-hook-handler-rules.bash`; `qa-changed` runs it over each changed Python file as `walk-open-files`, and whole as `walk-open` when the rule, fixture, wrapper or tool change. See [walk-opens-non-regular-file](#walk-opens-non-regular-file)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | Every tracked Python file (extensionless scripts found by shebang)                                                                                                                                                                 |
| `qa-ansible.bash`            | Fail-fast scan (`failed_when: false`/`ignore_errors`, bare or quoted in either style, without same-line `# FAIL-FAST-OK:`, case-insensitive; the tool is Go, `src-go/qa/ansible`), **self-default vars** (`x: "{{ x \| default(…) }}"` — the 2.19 recursive-loop footgun `--syntax-check` can't see), **deprecated fact vars** (both `ansible_<fact>` and the un-prefixed injected names like `getent_passwd`, which no `ansible_`-anchored pattern can reach), **a guessed uid in a `/run/user/{{ … }}` path**, **[regex-backref-escape](#regex-backref-escape)** (a `regex_search`/`regex_replace`/`regex_findall` backreference with the wrong backslash count for its YAML scalar style and Jinja context; each file is parsed as YAML, and the rule is proven against `tests/fixtures/ansible-regex-backref/`), **[regex-class-escape](#regex-class-escape)** (a letter escape such as `\d` or `\s` in a regex pattern of those filters or of the tests `match`/`search`/`regex` that reaches the regex as a literal backslash, or `\b` as a backspace; proven against `tests/fixtures/ansible-regex-class-escape/`), **[root-umask](#root-umask)** (a file users read, created as root under the inherited umask: a system cache such as `fc-cache` rebuilt, or a download, redirect, clone or extraction into `/usr`, `/opt` or `/etc`, with no `umask 022` or readable mode; proven against `tests/fixtures/ansible-root-umask/`), the play-UX rules **[setting-checked-late](#setting-checked-late)**, **[vault-secret-by-hand](#vault-secret-by-hand)**, **[ansible-cli-without-chdir](#ansible-cli-without-chdir)**, **[no-log-hides-failure](#no-log-hides-failure)**, **[probe-before-install](#probe-before-install)** and **[module-built-for-running-kernel](#module-built-for-running-kernel)** (proven against `tests/fixtures/ansible-play-ux/`), **[command-always-changed](#command-always-changed)** (a command task that reports changed on every run; proven against `tests/fixtures/ansible-command-always-changed/`), **[changed-when-on-success](#changed-when-on-success)** (a task of any module whose `changed_when` is true on every run it succeeds, such as its own `rc == 0` or an `or` with it; proven against `tests/fixtures/ansible-changed-when-on-success/`), **[scratch-always-changed](#scratch-always-changed)** (a task that only makes, fills or removes a scratch directory and reports changed on every run; proven against `tests/fixtures/ansible-scratch-always-changed/`), **[image-build-arg-volatile](#image-build-arg-volatile)** (an image build given a `--build-arg` or `--label` that is new on every commit or run; proven against `tests/fixtures/ansible-image-build-arg-volatile/`), **[install-guard-trusts-record](#install-guard-trusts-record)** (an install skipped on a record it writes itself, with no stat of the artefact it installs; proven against `tests/fixtures/ansible-install-guard-record/`), **[filter-list-unchecked](#filter-list-unchecked)** (an input list that picks from another by membership with no check that each of its names is there; proven against `tests/fixtures/ansible-filter-list-unchecked/`), **[duplicate-managed-block](#duplicate-managed-block)** (two tasks in any plays or task files that write the same `blockinfile` block or `lineinfile` line of a file with different content; proven against `tests/fixtures/ansible-duplicate-managed-block/`), **plus** playbook shebang + exec-bit hygiene | `playbooks/ tasks/ vars/ environment/ roles/` (excludes `roles/vendor`), `*.yml`/`*.yaml`, only files git would publish (gitignored files are never read); a symlink to a directory or a dangling one is exit 2                    |
| `qa-ansible-syntax.bash`     | `ansible-playbook --syntax-check` on every playbook — a file with a top-level `- hosts:` **or `- import_playbook:`** (Plan 00081 F9/F14: deriving from `hosts:` alone dropped `playbook-main.yml`). Parse-only — safe in the CCY container. The tool is Go (`src-go/qa/ansiblesyntax`, spec `SPEC-qa-ansible-syntax.md`); it checks four playbooks at a time and reports in path order, and exits 2 up front when `ansible-playbook`, `ansible.cfg` or the vault password file the config names is missing. The pass line states the breakdown, so a coverage change is visible. Given playbooks (`qa-ansible-syntax.bash PLAYBOOK...`, relative to the root) it checks those only, with the same prerequisites and no discovery; a named file that is not a playbook is exit 2 (a task file is checked through a playbook that imports it). `qa-changed` runs it that way over the playbooks a change selects (issue #222)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | **Repo-wide**, not a fixed path list; excludes vendor/upstream trees. Includes playbooks under `CLAUDE/Plan/**`                                                                                                                    |
| `qa-js.bash`                 | Go (`src-go/qa/js`, spec `SPEC-qa-js.md`): `node --check` on every JavaScript file git would publish (`.js`, `.mjs`) + the project's ESLint 8 over those under `extensions/`, run as `--no-eslintrc --config extensions/.eslintrc.json --no-inline-config` so a nested config or an `eslint-disable` comment cannot weaken the rules, with the proof that ESLint read every file it was given (`qa-changed`'s per-file eslint runs the same way and is judged by the same code). `--no-inline-config` also makes `/* global X */` and `/* eslint-env */` comments inert: a global or environment a GJS file needs goes in `extensions/.eslintrc.json`. No `eslint-disable` exists in the tree, so there is no suppression allowlist                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | Repo-owned `.js` (excludes vendor/node_modules) + `extensions/`                                                                                                                                                                    |
| `qa-docs.bash`               | Link targets exist; every `#anchor` matches a real heading; every play imported by `playbook-main.yml` is named in both `docs/playbooks.md` and `docs/architecture.md`; every `playbooks/…yml` path written in those documents, `run.bash`, `scripts/` and `files/` is a tracked playbook (issue #109: a command naming a moved play; test suites and `testdata/` are not read, a tracked symlink is skipped, and a pattern, expansion, `...` or `<name>` placeholder is no path, so write an example as `play-<name>.yml`); every `CLAUDE/*.md` has an index row; every gate in `scripts/qa-gates.tsv` has a row in these tables and every row is a gate that runs (Plan 00070; the Go port, Plan 00192, reads Markdown with a CommonMark parser, `playbook-main.yml` as YAML and the gate table with the loader `qa-all` uses)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Core docs only — `docs/`, everything under `CLAUDE/` (daemon-owned `CLAUDE/core/` included), `README.md`, `*/CLAUDE.md`, `.claude/rules/`; the playbook-path check also `run.bash`, `scripts/`, `files/`. **Not** `CLAUDE/Plan/**` |
| `qa-go-qa.bash`              | The Go gate: go-qa-ci's `go-qa`, pinned by the `tool` directive of `src-go/qa/go.mod` (one require, which also gives the project rules driver its `driver.Main`; nothing installed system-wide: go-qa installs its own pinned tools, `tools.lock` in go-qa-ci, into its cache on the first run) and configured by `src-go/qa/go-qa.toml`, over both modules (`src-go` and the nested `src-go/qa`, kept apart so the QA tooling's dependencies stay out of the shipped binaries; `src-go`'s own are vendored and judged by `qa-go-deps`). It runs `go-qa run --no-fix` (formatting is checked, not rewritten; with `CI` set go-qa rewrites its checkout and then fails on the change): formatting (gofumpt, goimports), golangci-lint over `src-go/.golangci.yml` (the qa module links to it), `go vet` with go-qa-ci's bundled analyzers and the project rules ([GoProjectRules.md](GoProjectRules.md), `project_package_tests` among them: every package has tests), dead code, govulncheck, `go mod tidy -diff` and `go mod verify`, `go build`, `go test -race -shuffle=on` (that includes `internal/freeze/golden`, the replay of what the deleted bash `podfreeze` and `lxcfreeze` printed, scenario by scenario: any difference from the recorded bytes must be a numbered bug pinned in `deviations.json`; the freeze tools have no bash suite, their rows in `scripts/go-binary-suites.tsv` say `qa-go`) and a 90% statement coverage floor per module; the busfailpoints `[[stage]]`s of `go-qa.toml` (`teamchat-failpoints-lint`, `-vet` and `-test`) add golangci-lint, `go vet` and `go test -race -shuffle=on -count=2` over `internal/teamchat` (every test twice, so one that leaves package state behind fails) built with `-tags busfailpoints`, the only build its armed failpoints and crash scenarios exist in (the build tags: [src-go/CLAUDE.md](../src-go/CLAUDE.md#build-tags))); the `project-rules-tagged` stage builds the driver once and runs the project rules over the packages built with `-tags busfailpoints`, `tuwunel`, `ccyteamstest` and `difftest`, the builds the analyzers stage does not see (every pair is run, and any failing pair fails the stage). The project record and the suppression check are off until the owner signs a record (Plan 00191). A missing `go` is exit 2 (`play-golang.yml` installs it); a pinned tool go-qa cannot install is a failure, never a skip                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | `src-go/`, both modules; `qa-changed.bash` runs it in go-qa's diff mode (`--changed --base`) for a change inside `src-go/`, whole otherwise                                                                                        |
| `qa-bash-limits.bash`        | Tooling is Go (CLAUDE.md, [src-go/CLAUDE.md](../src-go/CLAUDE.md)): every bash file (`*.bash`, `*.sh`, `*.bash.j2`, or any file, whatever its name, whose first line is a bash/sh shebang, or a `.j2` with one after only Jinja lines; tracked and untracked-not-ignored) is parsed with `mvdan.cc/sh` and stays within 60 code lines, 90 statements, 4 functions, no associative arrays, no interactive UI (gum, fzf, whiptail, dialog, select ...), no data parsing (a second `jq`, `python -c`, `yq`), or is on the ratchet allowlist `scripts/bash-limits.allowlist` at its measured size. The measure is defined in the package doc of `src-go/qa/bashlimits`. A listed file may shrink and never grow or gain a flag; one under its record, within limits or gone fails until `scripts/qa-bash-limits.bash --update` lowers or removes the entry (`--update` never raises a record, adds an entry, or drops one whose file still exists, so a rename or a move into an exempt path cannot launder a file). The allowlist carries the measurer version (`# measurer: N`); a change to the measure raises `MeasurerVersion` and the allowlist is rewritten once with `--remeasure`, the only time a record may go up. The base: `QA_BASH_LIMITS_BASE`, else the merge-base of HEAD and origin's default branch (so committed-but-unpushed raises fail locally); CI uses the merge-base with the default branch, or the previous head for a push to the default branch, whose runs are never cancelled. A base without an allowlist, or no derivable base, is an error. Exempt, in `src-go/qa/bashlimits/exempt.go` each with its reason: test suites (`scripts/test-*.bash`, `tests/`), plan-folder scripts, vendored and daemon-owned trees; single daemon-owned files (`CLAUDE/Plan/mkplan.bash`) are named one by one in `DaemonOwnedFiles` (`daemonowned.go`), each exempt only while a hooks-daemon upgrade commit names it in its `UPGRADE_METADATA` `modified_files`, the one way an allowlisted file that still exists may leave the allowlist. The same run holds every bash file it finds but vendored, daemon-owned and `.semgrep/` ones, exempt or not, to the rule [assignment-masks-substitution-failure](#assignment-masks-substitution-failure), with no allowlist. The nested QA module `src-go/qa` holds the code (its parser dependency stays out of the shipped binaries); the script builds the command into a private directory under the cache                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | whole tree; `qa-all.bash` hard gate; `qa-changed.bash` runs it for any changed bash file, the allowlist, a path on the allowlist or its Go source                                                                                  |
| `qa-private-names.bash`      | As the gate, `tree HEAD`: install-specific names stay out of this public repository (Plan 00245 Phase 3; Go in `src-go/qa/privatenames`): no tracked file, path or line names an agent team, seat, container, host or client project, learned from the agent bus and the machine's names into `<git dir>/private-terms` (never tracked, never printed; findings give the place and the source only). The same tool is the `pre-commit` check on added lines and the `pre-push` check on every commit pushed ([SecurityRules.md](SecurityRules.md), [scripts/git-hooks/README.md](../scripts/git-hooks/README.md)). In a CCY container (the checkout under `/workspace`) a list that cannot be built is exit 2, never a skip; on a machine with no agent bus and no list (CI) it prints `SKIPPED` and passes, because there is nothing to judge against.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |                                                                                                                                                                                                                                    |
| `qa-toolchain.bash`          | Runs FIRST, as a hard gate: every tool a gate runs (ruff, semgrep, shellcheck, golangci-lint) is installed at the version `/.qa-versions` pins, and go is not older than the higher `go` directive of `src-go/go.mod` and `src-go/qa/go.mod`. A mismatch, an absent tool or a version that cannot be read fails this gate and lets the rest run — exit 1 (the machine is wrong; exit 2 is the gate being unable to judge); exit 2 is only for pins or a go.mod it cannot parse (`.qa-versions` is parsed as plain `KEY=VALUE` lines, never sourced). The tool is Go (`src-go/qa/toolchain`, command `src-go/qa/cmd/qa-toolchain`, specification `SPEC-qa-toolchain.md` T1-T16); `qa-changed` runs it whole when the pins, either `go.mod`, its wrapper, package or command change (see "The QA Toolchain Is Pinned")                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | The machine, not any file                                                                                                                                                                                                          |

The further gates below run inside `qa-all.bash` as **hard, non-structural** checks —
they are rows of kind `gate` (or `drift`, `helpers`), not report stages, so they have no
JSON to merge. Any one of them fails the run, and the gates after it still run:

| Gate                                        | Checks                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `qa-teamchat-separate.bash`                 | the gate `teamchat-keeps-off-v1` (Plan 00209 D 4.1): no file v2 of the agent-team bus owns holds a v1 name of `src-go/qa/teamchatseparate/v1-names.txt`. The scope is what `git` would publish under `src-go/internal/teamchat/` (bar the two named homes of deliberate v1 references, `v1names` and `migrate/v1`), `src-go/cmd/teamchat*`, `files/`, `tasks/` and `vars/` below a `teamchat*` component, and any `play-teamchat*.yml` or `.yaml` under `playbooks/imports/`; a v2 play or task file may name only those files and the shared ones the gate lists (`tasks/build-go-command.yml`), each path judged where it leads; the remaining references are rows of `allowed.tsv` with a reason, one row per line, and a row that allows nothing, or names on more than one line, fails. The tool is Go (`src-go/qa/teamchatseparate`; `qa-changed` runs it whole when a v2 file or the gate's own files change); exits 2 when no v2 file is in scope or the list or allowlist is missing or malformed. `src-go/internal/teamchat/paths`' unit test reads the same list                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `qa-nokill-containerwatch.bash`             | the container-watch watchdog (every file `git` would publish ending `.py` under `helpers/containerwatch/`, nested ones included; the tool is Go, `src-go/qa/nokill`; and the panel's `containerReport.js` and `sections/containers.js`, which are **named** so a missing one fails the gate rather than being globbed away) has gained no process-termination or container-lifecycle call site, **except** `containment.py`, which is named as the one audited exception and held to a stricter list: a graceful `stop` only, never `kill`/`rm`/`pause`/a force flag                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `qa-deployed-drift.bash`                    | every repo-owned `files/home/.local/bin/` script matches its deployed `~/.local/bin/` copy                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `qa-helper-tests.bash`                      | the `helpers/` unit suite (Plan 00081 F11), in Go (`qa/helpertests`, `SPEC-qa-helper-tests.md`); `--counts-file` reports its size and skip count as data; exit 0 pass, 1 a test failed, 2 no test found, a tracked test lost, counts that disagree with the request or an unreadable verdict                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `qa-hook-handlers.bash`                     | the project-owned hooks-daemon handlers under `.claude/hooks/handlers/` pass their tests (`**/tests`), run with the daemon's own interpreter (`<daemon>/scripts/lib/resolve_venv.sh python`, never a hardcoded venv name; a linked worktree reads the main checkout's daemon, which is gitignored), isolated (`-I -B`, no pytest plugin autoload), with the project's `pytest` appended to its path: the one the interpreter the venv was built from imports (`sys._base_executable`, never a `python<X.Y>` on `PATH`, so a pyenv global does not change it; `python3-pytest`: the CCY image, and `play-python.yml` on the host), so the venv's modules win and no two Python versions mix. The tool is Go (`src-go/qa/hookhandlers`, K1-K8 in its package doc). Exit 0 pass, 1 a test failed, 2 the daemon, its venv or that `pytest` is missing, naming the fix (Plan 00191 Task 4.6). CI's checkout has no daemon: `.github/workflows/qa.yml` sets `QA_HOOK_HANDLERS_EXEMPT` to its reason, which the gate honours on GitHub Actions only (exit 2 anywhere else), printing `HOOK-HANDLERS-EXEMPT`. "GitHub Actions" is read from the environment (`QA_HOOK_HANDLERS_EXEMPT` set together with `GITHUB_ACTIONS=true`), so a local run can set both on purpose and skip the gate, visibly                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `qa-plan-tests.bash`                        | every active plan folder's own Python tests (`CLAUDE/Plan/NNNNN-*/**/test_*.py`: the tests of its acceptance and triage checkers, which `qa-helper-tests.bash` never read) pass. `Completed/` is not run: a completed plan is frozen history whose checkers ran against code that has since moved on. Each directory holding tests runs in a process of its own, from that directory, as `cd DIR && python3 -m unittest <modules>` would (the runner `helpers/qa_environment/unittest_counts.py --chdir`, so a test imports the checker beside it by name), with no bytecode written and the machine's git configuration kept out; its counts come back as data and must agree with the request. The walk is checked against git's index (`tracked-plan-tests` of `qa/discovery`). Every folder runs whatever the others did, and each failing one is named. The tool is Go (`src-go/qa/plantests`, P1-P9 in its package doc). Exit 0 pass (also when no active plan has a test), 1 a test failed, 2 a tracked test lost, a name that cannot be imported, or a run that reached no verdict                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-secret-scan.bash`                     | the pre-commit secret scanner's own unit suite (Plan 00092)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-planlib.bash`                         | the `_planlib.inc.bash` regression suite behind every plan script (Plan 00092)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-meta-deploy.bash`                     | `meta-deploy.bash` and its `meta-deploy.list` queue: a malformed entry, a missing plan script or playbook and a failing unit all stop the batch, a `changed-plays` entry runs `run.bash --changed --yes` at its place in the list, and the real list validates                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-ccy-rootless-guard.bash`              | ccy's rootless-engine verdict (Plan 00072); pure function, no podman needed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-ccy-token-mode.bash`                  | `select_token`'s per-mode answer to an unusable token pool (Plan 00048, CCY 3.50.0)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-ccy-token-usage-output.bash`          | the token menu's usage view (`u`) prints only its own display lines: driven through the real fetch-and-redraw path with curl stubbed, every line is classified against the intended display, stray assignment or trace lines fail, stderr must stay empty, and the classifier is checked against known noise first                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `test-ccy-token-import-cleanup.bash`        | the import script `ccy --export-token NAME` prints, run as pasted against a scratch pool: the other `NAME.DATE.token` files are removed only on `CCY_TOKEN_IMPORT_REMOVE_OTHERS=1` or `y` at the prompt (answered on a real terminal via `script`), kept on `0`, `n`, Enter and with no terminal; an undated file, another name or a longer name is never touched; an unknown value refuses before writing; the token value is never printed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-ccy-token-usage-command.bash`         | `ccy --token-usage [--json]`, the Go front built and laid out as the play installs it, from a non-repository directory with curl, podman, docker and tmux stubbed: the bars, one entry per token name read with the earliest-dated file and the later one listed as shadowed and never sent, an expired token never sent, the menu's normalised percentages under both scales and over the limit, a scale conflict and a missing bucket reported as `null` rather than a wrong number, valid JSON with an awkward name and an unreachable API, the second run served from the cache, exit 0/1/64, no token value in the output and nothing written but the usage cache                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-ccy-ssh-handling.bash`                | ccy's SSH key and agent handling into the container                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-ccy-git-signing.bash`                 | ccy carrying commit signing into the container: the key staged and the gitconfig copy repointed, and a launch refused when signing is on with no usable key (Plan 00139)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `test-git-signing-declared.bash`            | host commit signing only where the machine declares an identity: `git_signing_declared` under each declared state, every signing task of `play-git-configure-and-tools.yml` inside its gate, and that block run by ansible against a temp HOME — a `github_accounts: {}` box gets no signing and no error, a desktop gets signing, a desktop missing its key fails (Plan 00139 T5.5). Needs `community.general`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-colour-emoji-fallback.bash`           | the colour emoji fallback `play-fonts.yml` deploys, run through fontconfig with fixture fonts made as BDF text: without it an English-capable symbol font wins every emoji (the defect, reproduced); with it Noto Color Emoji wins them under each generic family, its text font and a terminal font with no generic family, in the single best match and the ordered list, while digits, `#`, `*`, the space and text symbols stay on text fonts. Removing any one rule of the file turns it red (Plan 00185). Runs with `FC_LANG=en`. Needs `fc-cache`, `fc-match`, `fc-list`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-qa-tool-pin-converges.bash`           | `play-python.yml` converges an off-pin QA tool: probe, remove (`state: absent`), install, pin, in that order; removal and install select the same probe results; no pipx task sets `force: true`; the removal's `when:` evaluated by ansible keeps a tool at its pin and removes one at another version or missing (Plan 00142)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-agent-browser-session-guard.bash`     | the session cap behind ccy's three browser commands: a new session is refused while one is open, reuse and non-launching commands pass, and all three wrappers route through it (Plan 00140)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-ccy-selinux-verdict.bash`             | ccy's SELinux verdict, including the states that must refuse                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-ccy-relabel-preflight.bash`           | ccy finds workspace entries rootless podman cannot relabel, explains them, and runs the offered fix only on `y`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-agent-mailbox-watch.bash`             | the agent mailbox watcher ([AgentMailbox.md](AgentMailbox.md)) wakes each side once per finished reply, waiting request or message, and makes a missing mailbox mode 0700                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-ccy-container-menus.bash`             | ccy's existing- and orphaned-container menus: a launch nobody answers (restore, restart, headless) takes their safe answer, and a menu with a closed input or three wrong answers gives up instead of looping; their listings and stops, through the real Go front and a fake podman, say an engine failure with its cause and fail the launch, never "no containers" or "may already be stopped", and a container already gone is said so                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-ccy-flag-check.bash`                  | ccy's fail-fast flag check judges only flags before `--` against `claude --help`, and treats help text with no usage line as no help                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-ccy-gpu-device.bash`                  | ccy's GPU device passthrough decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `test-ccy-container-name.bash`              | ccy's container name choice: launches started together each claim a different name (the claim is `ccy-teams internal claim-container-name`), a claim lasts as long as its launcher's process, docker uses the same claim, and a failure stops the launch                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `test-ccy-network-version-probes.bash`      | ccy's network check and Claude Code version probes (Plan 00191 Task 4.10), through the real Go front and a fake podman and curl: a network that reached nothing is said with wget's words and the guide, an engine, pull or image fault is a check that could not run (never "no internet"), and a version that could not be read names the image's or the registry's cause                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-ccy-image-build-lock.bash`            | ccy's image build lock (Plan 00216): launches started together against a slow fake podman build each image once and end on the same image (missing, version bump, in-place update, project image), a forced rebuild waits then builds, different images do not wait for each other, a child the build leaves behind does not hold the lock, a holder killed mid-build leaves the waiter to build, a stale record is named and ignored, the play's `bash -c` form, every build given the commit in the context's `source-commit` as `CCY_SOURCE_COMMIT` and none without it (issue 235), that commit being the last change to the paths `image-source-paths` lists, which a launcher-only commit leaves as it was, and the builder stage taking only those paths (issue 277); that list being exactly `lib/ccy-image-source-paths.bash`'s output ([ccy-image-sources](#ccy-image-sources)), a commit to Go the image does not build from (another command's package, a test file, the qa module) leaving the key as it was, and the builder's own `go build` commands and pingbus zipapp building from nothing but the list's archive of the checkout (issue 285), and the Go front's `ccy --build-lock` reports and stops a build                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `test-ccy-restart-request.bash`             | `lib/restart-request.bash` — validating a supervisor restart request (an untrusted file), the restart budget, the `--resume` relaunch argv                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-ccy-container-version-hook.bash`      | `scripts/git-hooks/pre-commit` — a change to the Dockerfile, to any file its `COPY` lines take from either build-context root (`files/var/local/claude-yolo/`, `files/opt/claude-yolo/`), or to the pingbus zipapp's sources, or to a non-test file the Go commands of the Dockerfile's builder stage build from (matched by the git pathspecs of the staged `image-source-paths`, the list the play keys the stage on, which must be `lib/ccy-image-source-paths.bash`'s output, read from the stage's `go build` lines and resolved by `go list -deps`, vendored and embedded files included; a synthetic module and the real tree, one probe per package; a builder stage running Go in a form the hook cannot read is rejected; issue 237, rejections tagged `CCY-IMAGE-INPUT`; a new import with the list not regenerated, or a list edited by hand, is rejected `CCY-IMAGE-SOURCES`, issue 285) needs a container version bump, with the LABEL and `REQUIRED_CONTAINER_VERSION` moving together; a deletion counts, and every file `play-claude-yolo.yml` puts into the image must be rejected without a bump while the host-only files under `files/opt/claude-yolo/` must not be judged; the build context holds only copies of tracked files where a `COPY` takes from it, so no play task builds or generates image content there (issue 235: the Dockerfile's builder stage does); and the secret scan's scope: a vendored Go module under `src-go/vendor/` is not scanned when a stand-in `qa-go-deps` passes over the staged tree, a staged vendor change it rejects fails the commit, and it is judged on the index, not the working tree; the same address elsewhere is rejected (real hook, throwaway repository) |
| `test-ccy-git-preflight.bash`               | `entrypoint.sh` stops before Claude starts when the container's git cannot read `/workspace`, printing git's own message (the real block, cut out between its `GIT-PREFLIGHT` markers; Plan 00169)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `test-ccy-project-env.bash`                 | `entrypoint.sh` sources `.claude/ccy/ccy.env`, then the untracked `ccy.env.local`, whose values win (the real block, cut out between its `PROJECT-ENV` markers)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-ccy-agent-bus.bash`                   | `entrypoint.sh`'s agent team bus step (Plan 00161 U31), from its `AGENT-BUS` marker to the final exec (printed, not run), after the real `PROJECT-ENV` block: seats only from the launcher's `PINGBUS_SEATS` (a bus variable set, changed or unset by `ccy.env` or `ccy.env.local` refused, a `PINGBUS_TEAMS` or `PINGBUS_HOME` from the launcher refused), `pingbus seat exec --` in front of both final exec lines, the plugin and settings right after `claude` inside the wrapper's `--`, inert when unset; the Dockerfile and play ship the kit                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-ccy-teams.bash`                       | The launcher's `--teams <seat>@<team>[,...]` (Plan 00161 U31): `lib/agent-bus-seats.bash` against a fake `agent-bus` (`seat check`'s canonical list and codes, `seat take` with `--no-prompt` when unattended, `PINGBUS_SEATS` and the `ccy-seats` label only with a list); a team alone (Plan 00177 T4.8): the seat chooser against a fake `gum` (default row, full handles, held/new words, Esc, gum missing) and `--resolved` when unattended; the re-attach offer with `--teams` (T4.6) over real registry records; the real launcher in a throwaway repository for every refusal before anything runs (no value, given twice, `seat check`'s 64 and 78, the unquoted-space form, `agent-bus` missing, a headless team alone) and a plain launch calling no `agent-bus`; restart and restore keep `--teams`; headless Quick Launch (saved choices, never the prompt) and the passphrase-key refusal, cut out of the launcher                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `test-ccy-env-local-dist.bash`              | `lib/common.bash` `ccy_env_local_dist_sync` — ccy writes the tracked `ccy.env.local.dist` (versioned, comments only, valid bash), refreshes an old one, leaves one from a newer ccy alone, never writes `ccy.env.local`, and warns when that file's "based on" version is older or missing; `ccy_env_local_mount_args` binds an existing `ccy.env.local` read-only (real functions, throwaway project)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-ccy-gitignore-safety.bash`            | `lib/common.bash` `check_ccy_gitignore_safety` — the generated `.claude/ccy/.gitignore` keeps session data and `ccy.env.local` ignored, lets the tracked files through (`ccy.env.local.dist` among them), adds the dist exception to an older file, and flags a tracked `ccy.env.local` (real function, throwaway repository)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `test-ccy-lifecycle.bash`                   | `lib/session-lifecycle.bash` — `--max-age`/`--run-for`/`--until` validation, the deadline kept across a restart, the entrypoint adding the supervisor plugin only when asked (plugin: `tests/helpers/ccy_lifecycle/`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `test-wsi-stop-grace.bash`                  | speech-to-text's delayed stop: the real `wsi` (the Go binary, built from `src-go/cmd/wsi` beside links to its sibling helpers) under stub audio tools keeps the microphone open for the grace after the first TERM, stops at once on a second TERM or a grace of 0, and refuses to record without a grace; then `tests/speech_to_text/`: `wsi-stream`'s stop state, handlers and drain as units, pre-buffer mode end to end (stub RealtimeSTT, fake `pw-record`, TERM during and after the model load), and `wsi-stream-server`'s stop order. Standard streaming and the server-mode client loop are not run end to end (Plan 00148)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `qa-stt-limits.bash`                        | each speech-to-text recording limit has one home: `wsi`'s 30 s (`src-go/internal/wsi/limit.go`), `wsi-stream`'s 120 s, continuous dictation's GSettings keys; a literal copy in the panel, the server or a second place in a recorder fails (Plan 00148)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `qa-speech-to-text-rules.bash`              | the Semgrep rules in `.semgrep/speech-to-text.yml`, proven against `.semgrep/speech-to-text.js` and `.py` every run, over every JavaScript and Python file git would publish (extensionless scripts found by shebang); each language is scanned with its own rules, and the gate exits 2 if a rule has no `ruleid:` line in its fixture or a file it handed semgrep was not scanned. The tool is Go (`src-go/qa/sttrules`, spec `SPEC-qa-speech-to-text-rules.md`, S1-S17; `qa-changed` runs it over each changed JavaScript and Python file as `stt-rules-files`, and whole as `stt-rules` when the rules, fixtures or tool change). See [model-present-without-weights](#model-present-without-weights), [dropdown-label-carries-explanation](#dropdown-label-carries-explanation) (Plan 00156), [test-wall-clock-ceiling-under-a-second](#test-wall-clock-ceiling-under-a-second), [test-deadline-under-ten-steps](#test-deadline-under-ten-steps), [stream-stand-in-not-a-text-stream](#stream-stand-in-not-a-text-stream), [completion-published-before-cleanup](#completion-published-before-cleanup), [recorder-killed-leaving-its-files](#recorder-killed-leaving-its-files) [pw-record-stdout-not-raw](#pw-record-stdout-not-raw) (Plan 00229) and [proc-zombie-taken-as-exit](#proc-zombie-taken-as-exit) (fedora-desktop#182)                                                                                                                                                                                                                                                                                                                                                                                          |
| `qa-hook-handler-rules.bash`                | the Semgrep rules for code the hooks daemon loads (the project's handlers in `.claude/hooks/handlers/`), in `.semgrep/hook-handlers.yml`, proven against `.semgrep/hook-handlers.py` every run, over every Python file git would publish (extensionless scripts found by shebang); the gate exits 2 if a rule has no `ruleid:` line in the fixture or a file it handed semgrep was not scanned. The tool is Go: the engine of `src-go/qa/sttrules` run with its `HookHandlers` spec (command `src-go/qa/cmd/qa-hook-handler-rules`; `qa-changed` runs it over each changed Python file as `hook-handler-rules-files`, and whole as `hook-handler-rules` when the rules, fixture or tool change). See [hook-path-resolved-against-cwd](#hook-path-resolved-against-cwd)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `qa-ready-wait-rules.bash`                  | [ready-wait-ignores-child-exit](#ready-wait-ignores-child-exit) and, Python only, [restart-loop-hides-its-exits](#restart-loop-hides-its-exits) over every tracked Python file (`.semgrep/ready-wait.yml`, proven against `.semgrep/ready-wait.py`) and every tracked shell script (the Go rule in `src-go/qa/readywait`, proven against `.semgrep/ready-wait.bash`), each run; the tool is Go (spec `SPEC-qa-ready-wait-rules.md`, R1-R26; `qa-changed` runs it over each changed Python and shell file as `ready-wait-files`, and whole as `ready-wait` when the rule, fixtures or tool change); exits 2 if a Python file it handed semgrep was not scanned (Plan 00156)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `qa-hidden-bindings.bash`                   | [project_hidden_bindings](GoProjectRules.md#project_hidden_bindings) outside Go: every tracked shell file that runs fzf (no `--expect`; no `--bind` of a key to anything but navigation, read by `src-go/qa/internal/fzfbind`; and, for [project_esc_joined](GoProjectRules.md#project_esc_joined), every fzf that draws a screen binds each Esc-joined key to abort in its literal `--bind` words) and every tracked Python file that imports textual (each binding's action is navigation or the action of a `Button` in the file, judged by `helpers/qa_environment/textual_bindings.py`), each half proven against its fixture in `tests/fixtures/hidden-bindings` every run; the tool is Go (`src-go/qa/hiddenbindings`; `qa-changed` runs it whole as `hidden-bindings` after any shell or Python change); exits 2 if a file cannot be read or parsed or python3 cannot run                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `qa-lxc-attach-streams.bash`                | [lxc-attach-file-stream](#lxc-attach-file-stream) over every tracked shell script but `CLAUDE/Plan/Completed/`, judged together so a wrapper defined in one script counts in the one that sources it, proven against `.semgrep/lxc-attach-streams.bash` every run; the tool is Go (`src-go/qa/lxcattach`; `qa-changed` runs it whole as `lxc-attach-streams` after any shell change); exits 2 if a script cannot be read or parsed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `qa-ansible-pause.bash`                     | [pause-without-register](#pause-without-register) over every published YAML file under `playbooks/` and `tasks/` (the shared discovery list): an `ansible.builtin.pause` task with no `register:` and no `# PAUSE-OK: <reason>`. The tool is Go (`src-go/qa/ansiblepause`, spec `SPEC-qa-ansible-pause.md`); the Go tests prove the rule against `tests/fixtures/ansible-pause/flagged.yml` (each `EXPECT-FINDING` line reported, nothing else) and `clean.yml` (nothing reported)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `test-ccy-host-hostname.bash`               | `ccy_host_hostname` — the RFC 1123 grammar guarding `CCY_HOST_HOSTNAME` (Plan 00121)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-ccy-host-time-zone.bash`              | `ccy_host_time_zone` — the host's IANA zone passed to every container as `TZ` (timedatectl first, the `/etc/localtime` link as fallback), refused rather than defaulted to UTC when neither names a zone under zoneinfo, and a project's `ccy.env` / `ccy.env.local` `TZ` still winning (Plan 00165)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-ccy-info-flags.bash`                  | the real launcher from a non-repository directory: `--version`/`-v`, `--help`/`-h` and a bare `--list-tokens` print and exit 0, and every other invocation still refuses with the repository-root error                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-ccy-network-disconnect.bash`          | `ccy --disconnect` — the detach, the picker's re-prompt, and whether the saved default network is cleared, kept, or never there, against a stubbed engine                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-ssh-suspend-guard.bash`               | `ssh-suspend-guard` session detection — driven against captured `ss` output, including a host whose sshd is not on port 22                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-ccy-session-registry.bash`            | the ccy session registry — record written, removed on return, kept on a kill (the real trampoline under `kill -KILL`), the one-shot replay filter, and restore as a translation over a stubbed live list (Plan 00135)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `test-ccy-compose-restore.bash`             | the compose question and the launch questions nobody can answer — `--compose start\|skip` answers without asking and records started or declined, services already up record running, a stack that will not start under `start` ends a person's launch but in a restore is retried and then asked about in the pane with the record left in place, `ask` with no terminal or under `--headless` is refused by name and never read, a restore of a record with no answer says why before it asks; the launcher's real argument loop sets `COMPOSE_MODE`, the outcome is really written into the session's record, the SSH no-key, passphrase-retry and GitHub-over-443 questions with nobody to answer; and every launcher question has a nobody-to-ask guard of its own, proven by removing each guard in turn (fedora-desktop#87, Plan 00135)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-ccy-sessions-reboot.bash`             | `ccy-sessions --list / notify / reboot / restore` — the Go binary, built from `src-go/cmd/ccy-sessions` as the play builds it (`qa-changed.bash` runs the four `ccy-sessions` suites for any `src-go/` file in that build's closure), under a fake tmux, systemctl and per-project daemon CLI: one signal per project, refusal before any signal on a missing CLI, `--dry-run` touches nothing, `systemctl reboot` last (Plan 00135); a ccy session with no container is left out; `shutdown-with-update` / `reboot-with-update` under fakes: desktop always updates and powers off (failed or hung warnings, inhibitors), server or `--strict-sessions` refuses (Plan 00173)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `test-ccy-sessions-take-over.bash`          | `ccy-sessions` (the Go binary) Ctrl-T and the bash library's `ccy_tmux_attach` — a fake tmux holding sessions and their clients, the real picker under `script` with a fake fzf: the other terminal detached and this one attached, nothing killed, and nothing attached when the session ended, the detach was refused, the other terminal held on, or the user said no (Plan 00158)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `test-ccy-tmux-targets.bash`                | The tmux targets the bash library and `ccy-sessions`' Go pane code build, run against a REAL tmux on a private socket: a pane command reaches the pane with `=name:`, a session command the session with `=name`, and the bare `=name` pane form is refused (fedora-desktop#69, which fakes had hidden; Plan 00135)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-cc-teams.bash`                        | `cc --teams`, cc's agent team bus library: the argument split, the chooser called with the host type on a terminal only (the owner's one-time Element consent lives in `ccy-teams choose-teams`, tested in Go), the seats checked and taken as `host` through `agent-bus`, the launch block and the prompt sentence, cc's own settings file denying the file tools Element's folders, and the session's text, which names only the correct way to post (Plan 00177)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-ccy-teams-command.bash`               | the `ccy-teams` command — the Go binary, built from `src-go/cmd/ccy-teams` as the play builds it (`qa-changed.bash` runs it for any `src-go/` file in that build's closure); the real `--list` and picker (under `script`) over fakes for tmux, fzf, systemctl, sudo, gum, gh, wl-copy, `agent-bus` and `agent-bus-install`. It covers each team, seat (with its session and restore mark), member and person as those sources say; a down, undeclared or not-installed homeserver; and every key reaching the right command for the right row, with a no changing nothing. Sudo is asked only when needed, and a refusal exits 1 and never says "no teams". The fake sudo grants exactly the sudoers rule's commands. It also covers the password card and clipboard, colour (plain with `NO_COLOR` or piped), the no-teams offer, the gum `create` and `add-repo` questions (Esc and Ctrl-C write nothing), `backup` and `restore` (Plans 00161, 00177)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-ccy-restore-askpass.bash`             | the restore-only SSH_ASKPASS, with the real ssh-agent and ssh-add: a server's restored session unlocks its key unattended (host probe and container entrypoint), an ordinary launch never uses askpass, the passphrase reaches no argv or output, a missing or wrong one fails at once, every passphrase copy is removed once its key is added or the launcher is killed, and neither tmux nor the container's configuration inherits any of it (Plan 00135)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-ccy-token-handoff.bash`               | the caller's exported `GH_TOKEN` reaching the launcher ccy re-runs inside tmux, driven through the launcher's own entry point under `script` with a fake tmux that runs the pane: a 0600 file in a 0700 directory, only its path on the pane's command, never in any argv or tmux's environment, taken and deleted by the inner launcher, removed when the outer one exits first (Esc), carried by an attach refused and by the custom-Dockerfile re-runs; a missing runtime directory fails; only an absolute `ccy-gh-token.XXXXXX/token` of the user's under a runtime or temporary directory, 0600 with one link in a 0700 directory, is taken (also when the outer launcher's environment named a different one of those), any other path, a relative one included, ending the launch and left in place, an empty one removed; the real launcher takes the file before anything else; `--debug`'s log is 0600 and holds no token (Plan 00191)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `test-ccy-agent-choice.bash`                | `lib/agent-choice.bash`, `ccy --opencode` (`-oc`): both spellings and their near misses, a claude launch's command and banner unchanged, the options opencode cannot take and a bare first message refused (64), a Quick Launch's saved team dropped without the bus kit, a missing, misnamed, readable-by-others, empty, two-line or linked provider key refused (1), each key handed by name to the container run only and gone from the launcher afterwards, a key gone or blank since the check stopping the run, and the launcher calling the library where the flag acts (Plan 00215); the launch itself is in the differential harness                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `test-ccy-container-limits.bash`            | `lib/container-limits.bash`, `CCY_PIDS_LIMIT`: unset or empty gives the documented default (above podman's 2048, still bounded), a whole number from 256 to 32768 is used as given, and 0, -1, `max` and other words, suffixes, padded, leading-zero, out-of-range and shell-text values are refused naming the variable (the limit cannot be turned off); the pane hand-off is in `test-ccy-token-handoff.bash`; the launcher lists, sources and calls the library as soon as it is loaded, before any container runs, and the session container's run passes `--pids-limit "$CCY_PIDS_LIMIT"` (every other ccy run is held to it by the Defence Before Fix rule `ccy-container-default-pids-limit`, `.semgrep/bash-conventions.yml`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-panel-sections.bash`                  | four suites: the panel's decisions on boot-stale/malformed/`state`-disagreeing documents, the indicator's icon per state (Plan 00109), the containers section (Plan 00144), and dock recovery on unlock                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-vmtest-prepare-record.bash`           | the fixture→checker record seam — one metacharacter unset every key after it (Plan 00109)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-vmtest-kernel-selection.bash`         | `select_second_kernel` — the one step of that route no machine here can execute (Plan 00109)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-run-bash-headless-localhost-yml.bash` | the headless `localhost.yml` writer (Plan 00119)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `test-run-bash-ssh-agent-teardown.bash`     | `hl_ssh_agent_stop`, including an agent that SURVIVES the kill (Plan 00063 Task 3.4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-run-bash-changed.bash`                | the real `run.bash --changed` in a throwaway checkout, against a stubbed changed-plays helper: the plays it names run in order under one play lock after one confirmation, a failure stops the run with its status, "cannot tell" and "gone" plays are named and not run, and a helper that cannot answer runs nothing (Plan 00141)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `test-run-bash-rerun.bash`                  | the real `run.bash --rerun` in a throwaway checkout, against a stubbed changed-plays helper: the menu marks plays that changed or failed, the pick (numbers, `a`, `q`) runs in menu order under one play lock, a mistyped answer is named and asked again up to three times, end of input cancels, and a failure stops the run with its status (Plan 00141)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-run-bash-single-play.bash`            | the real `run.bash` single-play mode in a throwaway checkout: `--headless <play>` unattended (sudo-only preflight, password via `/dev/fd/N`, stdin closed, the play's exit status, a pinned `RUN_BASH_ANSIBLE_PLAYBOOK`) and the shared play lock (waited for up to its bound, then exit 75 when still held; delegation proven) (Plan 00137 T1.4/T2.1)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-run-bash-gh-scopes.bash`              | GitHub scopes asked for in one pass, from `vars/github-required-scopes.yml`: `run.bash`'s login carries every scope, a short token gets one refresh naming all it lacks, and headless `run.bash` and `gh-account-setup.bash` fail once naming every account and scope (Plan 00139 T1.3); with several accounts logged in, `run.bash` asks which is the primary. `gh-account-setup.bash` is driven as built (the Go command `src-go/cmd/gh-account-setup`, Plan 00192): a login run makes the key with the real `ssh-keygen` through the command's askpass, uploads it as the account, verifies it and puts gh's active account back, also when the wrapper is sent a TERM during the login (it execs the command); the wrapper passes the command's exit code back and exits 2 with no `go` or a build that fails                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `test-run-bash-optional.bash`               | run.bash's optional-play functions, extracted and run under stub `ansible-playbook` and `sudo`: the headless `RUN_BASH_OPTIONAL_PLAYBOOKS` list and the menu's A, W and B go on past a failed play, print a summary of every play and exit 1; a play ended by a signal or Ctrl-C (exit 99) stops at once; an unknown name runs nothing (Plan 00228 Task 6.6)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-run-bash-consumer.bash`               | the real `run.bash` under a throwaway home with stub `sudo`, `git` and `whoami`: `--consumer`, or a consumer `localhost.yml`, hands over to `scripts/consumer-install.bash`; a developer box and `--changed` stay on the developer path; `--headless --consumer` is refused; the real wrapper builds the Go command (Plan 00228)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `test-run-bash-ssh-agent-start.bash`        | headless `run.bash` loads the login key through askpass with the real ssh-agent and ssh-add, the helper's text holds no passphrase, and a wrong passphrase stops the run instead of hanging it (found in Plan 00135 T6.2)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-run-bash-pipx-inject.bash`            | `run.bash`'s pipx inject step, cut out and run with a stub `pipx` and the real `jq`: only missing Ansible dependencies are injected, a complete install prints one line, and a failed inject or an unreadable `pipx list --json` stops the run (Plan 00188)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-run-bash-upgrade-first.bash`          | `run.bash`'s dependency step, cut out and run with a stub `_sudo`: `dnf -y upgrade --refresh` runs before the install, and a failed upgrade stops the run; on the real file the upgrade comes before pipx, `ansible-galaxy` and the main playbook, so no Ansible process runs on an interpreter replaced under it (Plan 00229 Task 2.2)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-run-bash-git-ref.bash`                | [checkout-ref-bypass](#checkout-ref-bypass): every clone, pull, fetch or checkout in `run.bash` is in `fd_checkout`, checkout code before the repository step follows a pin, and `fd_checkout` puts an absent or existing checkout on a 40-hex or branch `RUN_BASH_GIT_REF` instead of pulling (issue #225)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-run-bash-config-repo.bash`            | `run.bash` reaches the private `<primary>/fedora-desktop-config` with the primary's own token whichever account gh has active, only a 404 reads as "not there", any other failure stops the run with gh's words, and the save sends the config as a body file, not an argument (found running Plan 00139 T1.4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-gh-alias-wrappers.bash`               | `play-github-cli-multi.yml`'s `gh-<alias>` and `clone-<alias>` run as their account and then put gh back on the account that was active, not the saved default; a failed switch either way, or no active account to return to, fails the wrapper; `gh-token-<alias>` reads by name without switching; only `gh-switch`, `gh-set-default` and `_gh_as_account` switch accounts (found running Plan 00139 T1.4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `test-self-update-cycle.bash`               | the real `fedora-desktop-self-update` root wrapper against a signed git fixture, with `runuser` and `systemctl` stubbed: refusals and exit codes, play then warn then `systemctl reboot` last, both passwords on descriptors, the play lock held, the pinned system ansible, `verify` (Plan 00137 T3.3/T3.4/T4.1)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `test-agent-bus-install.bash`               | the real `agent-bus-install` against a temporary root (`AGENT_BUS_INSTALL_TEST_PREFIX`), with `systemctl`, `firewall-cmd`, `nmcli`, `curl`, `ip`, `ss`, `dnf` and the admin tool's team commands stubbed (its `render` is the real one): argument and bus-address refusals; malformed and wildcard listen addresses and malformed CIDRs refused; the file layout; no download when the pinned hash matches, a refused hash installs nothing; no restart and no `CHANGED` when nothing changed; a unit that fails to start fails the readiness step; firewalld rules follow `allow_from`; `check`, `backup-now`, `restore`, `remove` (Plan 00161 U16)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `test-release.bash`                         | the real `scripts/release.bash` against a signed git fixture with only `gh` stubbed: each refusal (dirty tree, branch not `F<major>`, behind remote, CI red, pending or absent), the version following from the last tag numerically, the changelog entry, and the signed annotated tag landing on the release commit and being pushed (Plan 00153 T2.1)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `test-run-log-scrub.bash`                   | the run-log secret scrubber, driven by a deliberately incomplete redaction (Plan 00121)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `test-host-health-login-snippet.bash`       | the login snippet's interactive guard — an unconditional print breaks `scp` — and its once-a-day gate (Plans 00109, 00136)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-bash-history-search.bash`             | the Ctrl+R ranker's tier order, and the per-prompt recorder in a real interactive bash: right directory and status, leading-space and `HISTIGNORE` commands kept out, nothing recorded twice (Plan 00138)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `test-bash-history-session.bash`            | with the Ctrl+R search bound, up-arrow offers only the current terminal's commands while each is still appended once to the shared file, including a session whose prompt hook was replaced and which ends by `exit`; without it (no fzf), the whole file is loaded for stock Ctrl+R. Real interactive shells, rc-file started, run as a normal user (Plan 00138)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `test-terminal-modes.bash`                  | the prompt hook that switches stray terminal modes off, on a real pseudo-terminal: the exact sequence at the prompt, nothing on a pipe or for `TERM=dumb`, nothing that clears or swaps the screen, `$?` unchanged for later prompt hooks, added once when sourced twice, bracketed paste re-enabled by readline afterwards                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `test-fedora-desktop-health.bash`           | the on-demand report command: a sentence on a clean host, `--hold` released by Enter or EOF, a missing checkout named without a traceback (Plan 00136)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `test-randfile.bash`                        | `randfile`: one random regular, non-hidden top-level file of a directory (the current one by default) on stdout; an empty or missing directory and a usage error fail with nothing on stdout, so `open "$(randfile)"` never opens a wrong path                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `test-secret-screen.bash`                   | `scripts/secret-screen.bash`: piped or redirected, what it reads is written out byte for byte; on a terminal (a pseudo-terminal from `script`) it is shown on the alternate screen until Enter, nothing of it reaches the normal screen, and the screen is left when the input closes before it                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `test-podman-store-probe-race.bash`         | the rootless store ownership probe (`tasks/podman-store-ownership-probe.yml`), its argv read from the task, against a directory whose files are created and deleted while find walks it: without its `-links +0` guard it prints a deleted file (run until it does, bounded), with it it prints nothing in 400 runs, and it still finds a file really owned outside the range (issue #152)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `test-qa-ansible-failfast.bash`             | the fail-fast directive pattern, through the built Go `qa-ansible` tool over one-line fixture trees (quoted values included)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-qa-changed.bash`                      | `qa-changed.bash`'s file-to-check mapping through its `--plan` mode, its git collection (modified, untracked, ignored, deleted, `--base`) in a scratch repository, and its command line and exit codes; `QA_CHANGED_BIN=<binary>` runs the same cases against a built binary                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `test-qa-docs-exit-codes.bash`              | `qa-docs.bash`'s three exit codes, driven against fixture trees — a crash must never read as clean                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `helpers.gnome.check_extension_compat`      | every extension declares the GNOME Shell major this branch's Fedora ships                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `helpers.gnome.check_panel_contract`        | the panel's constants, document keys and section ids agree with the producer (Plan 00109)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `qa-vmtest-manifest.bash`                   | `vars/vm-test-scenarios.yml` parses (strictly: one document, no duplicate key) and the host's own validator (`helpers/vmtest/validate_manifest.py`, so python3 is required) accepts it (Plan 00110); every runnable scenario's `planned` equals the `PLANNED` of the guest checker `vmtest` would use (read from its syntax tree), every `guest-prepare-<scenario>.bash` fixture belongs to a scenario that declares `reboot_before_checks: true`, and no guest script is left for a scenario or profile that no longer exists; a guest script counts only if git would publish it. Exit 1 the manifest or a guest script is wrong, exit 2 an input unread or unparsed or python3 missing. The tool is Go (`src-go/qa/vmtestmanifest`, spec `SPEC-qa-vmtest-manifest.md` V1-V16; the old run-time controls are its Go tests); `qa-changed` runs it whole as `vmtest-manifest` when the manifest, the Fedora version, the validator, a guest script or the tool changes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `qa-version-pins.bash`                      | `vars/version-pins.yml` is well formed (a YAML parse; shape, installed blocks, at least one tracked pin) and every row still names a file that exists and declares that var with a value (Plan 00109). The tool is Go (`src-go/qa/versionpins`, spec `SPEC-qa-version-pins.md`, V1-V25); it exits 2 for a manifest or playbook it cannot read or parse, or for no pins, and `qa-changed` runs it whole as `version-pins` when the manifest or a playbook changes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `qa-go-deps.bash`                           | `src-go`'s dependencies are vendored, pinned and allowlisted (Plan 00204 D2): every module `src-go/go.mod` requires, direct or indirect, has a row with its reason in `src-go/go-deps.allowlist`; a pseudo-version or a `replace` directive needs a row of its own; a row `go.mod` does not need fails; `go mod verify` passes (the module cache unchanged since download), `go mod tidy -diff` is empty, and `src-go/vendor/` is byte for byte what `go mod vendor` writes (these two check `go.sum`). A cold module cache with no network is exit 2, could not judge. The tool is Go (`src-go/qa/godeps`, spec `SPEC-qa-go-deps.md` in Plan 00204); exit 2 for an allowlist or `go.mod` it cannot read or parse, or a go command that cannot start; `qa-changed` runs it whole as `go-deps` when `go.mod`, `go.sum`, the allowlist, `vendor/` or the tool changes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `qa-go-vuln.bash`                           | `govulncheck`, pinned by the `tool` directive of `src-go/qa/go.mod` (nothing installed system-wide; `go tool govulncheck`), finds no known vulnerability that `src-go`'s code reaches; the package load reads `src-go/vendor/`, so the scan is of what the binaries are built from. Status 3 (a reachable vulnerability) is exit 1; any other failure, a vulnerability database it could not fetch included, is exit 2, never a pass. **It needs `vuln.go.dev` on every run**: govulncheck keeps no database cache, so an offline `qa-all` reds this gate with exit 2 and the fetch error. Same package as `qa-go-deps`; `qa-changed` runs it whole as `go-vuln` when `go.mod`, `go.sum`, `vendor/`, the qa module's `go.mod` or the tool changes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `qa-plan-script-logging.bash`               | no active plan script (`CLAUDE/Plan/NNNNN-*/`, by `.bash`/`.sh` name or shell shebang) redirects a bare `exec` into an output process substitution (`exec > >(tee …)`, judged on the `mvdan.cc/sh` syntax tree), and no plan folder keeps a local `logs/` dir (Plan 00130). The tool is Go (`src-go/qa/planscriptlogging`, spec `SPEC-qa-plan-script-logging.md`); it exits 2 for a script that does not parse, an unreadable file or no script found, and `qa-changed` runs it whole as the `plan-script-logging` check when a plan script changes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |

### Gates waiting on a sweep (red until swept)

A gate for a defect class whose instances already exist is built detector-first, proven red,
and left **out of `scripts/qa-gates.tsv`** until its sweep has emptied the pile; wiring it in
while red would fail every `qa-all.bash`. Adding its row to `scripts/qa-gates.tsv` is the step
after the sweep, and this section is where the wait is recorded.

- The untracked-work audit (`scripts/qa-untracked-audit.bash`; Plan 00245; Go in
  `src-go/qa/untrackedaudit`, data in `scripts/untracked-policy.yml`) guards work kept only
  where git does not carry it: a file in `untracked/` older than 24 hours that is not a scratch
  shape or is a `.md`, `.patch` or `.diff` (the registered stores are skipped, the daemon's own
  stores are a warning count), a local branch with commits no remote holds, a linked work tree
  with old uncommitted changes, any stash. It waits on Plan 00245 Task 2.2, the sweep of
  `untracked/` and of the local branches and work trees.

**Local only:** a clean checkout has no `untracked/` and no work in progress, so CI cannot run
this gate; it would also be a no-op there. `--policy FILE` reads the policy from another file,
for a checkout that does not carry it yet.

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

**Merge F44 into a branch before pushing it** (or the line it targets, F43 or F44-arm). CI's
first job, "picks the protected line", fails a pushed work branch in seconds when it contains
the tip of no protected line on origin, or the tips of two lines and not the default branch's,
and the QA job waits on it: a diff against a stale base would judge changes the line already has.
Pull requests, tags, release branches and the protected branches themselves are not judged for
their base.

### The same command does not reach the same verdict everywhere

`qa-all.bash` is the authority, but some stages read what the *machine* supplies rather
than what the repository ships. A local run and a CI run disagreeing is a fact about the
stage, not a flaky gate — find which input differs before touching anything.

| Gate                     | What it needs from the machine                                                                                                                                             | Where that is missing                                                                                |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `qa-ansible-syntax.bash` | the vault password file `ansible-config` reports to **exist and be non-empty** (never read by the gate); exit **2** when it is missing, never N failures                   | a clean checkout, and a worktree made by bare `git worktree add`                                     |
| `qa-js.bash`             | `extensions/node_modules` — its own message says no playbook installs it. Exits **2**, which still aborts the run                                                          | a bare `git worktree add`, and any checkout where `npm install` has not been run                     |
| `qa-deployed-drift.bash` | deployed copies under `~/.local/bin` to compare the repo against                                                                                                           | the CCY container and a clean checkout — it self-skips **and names why**                             |
| `qa-helper-tests.bash`   | one pair asserts against real `/sys/class/drm` and skips where no connector with a physical display link is present                                                        | a VM whose only connector is virtual                                                                 |
| `qa-hook-handlers.bash`  | the hooks daemon (gitignored) with its venv, and `python3-pytest` for the interpreter the venv was built from; exit **2** naming the fix when any is missing, never a skip | GitHub Actions, whose checkout has no daemon: the workflow's named exemption, `HOOK-HANDLERS-EXEMPT` |
| `qa-plan-tests.bash`     | Plan 00161's `test_u23_kit_unpack` needs a `tarfile` with extraction filters (Python 3.11.4 or later) and skips without one; the pass line carries the skip count          | an older `python3`                                                                                   |

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

The two languages carry **separate exclusion lists** over that one mechanism
(`scripts/qa-trees.tsv`, `shell` and `python` rows). They differ where a tree
holds code of ours in one language only: the Python project handlers under
`.claude/hooks/handlers/` are gated, while the generated hook scripts beside them
are not.

**Daemon-deployed files are never gated here** (owner's decision). The hooks
daemon ships and rewrites them on every install and upgrade, so a local edit is
discarded, and its own QA is their gate. They are `.claude/ccy` (with
`claude-supervise.py`), `.claude/hooks/*` (but not `handlers/`), `.claude/skills`,
`.claude/init.sh`, `.claude/provision.sh`, `CLAUDE/Plan/mkplan.bash` and
`CLAUDE/core`, each a row of `scripts/qa-trees.tsv`, which the discovery gates and
the ghscopes engine (gh-scopes, find-race, arch-pin) read; bash-limits holds its own
exemptions for the same files (`Exemptions`, `DaemonOwnedFiles`), and
[qa-trees-drift](#qa-trees-drift) proves both agree with the table. A finding in one
of them is reported upstream (`/hooks-daemon issue-report`) and never blocks this
repository: no gate opens them, and none is narrowed or patched to make one pass.
The secret and private-name scans do not read the trees table and still cover
them. When an upgrade ships a new daemon-deployed file a gate would open, it gets
a row in the table in the upgrade commit.

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

### checkout-ref-bypass

**What it is.** `scripts/test-run-bash-git-ref.bash` (issue #225) holds three facts about how
`run.bash` gets the fedora-desktop checkout:

1. **One acquirer.** Every `git clone`, `pull`, `fetch`, `checkout`, `reset` or `switch` in
   `run.bash` sits in `fd_checkout`, whatever global options come before the subcommand (`-C`,
   `-c`, `--git-dir`). The same holds for `gh repo clone`. `fd_checkout` clones when the
   checkout is absent. With `RUN_BASH_GIT_REF` set it pins the checkout to that ref and never
   pulls.

2. **Pin before use.** This applies before the repository step converges, in the main flow,
   to two kinds of line:

   - a line that runs checkout code: `python3 [flags] -m helpers.…`, `python3 …helpers/…`, or a
     checkout's `scripts/`;
   - a call of a function that runs checkout code, directly or through other functions.

   Such a line must follow a call to `fd_checkout`, or to a function that calls it, in its own
   `title` section. It may instead run from a variable that an earlier line set from such a
   call (`gh_scopes_dir="$(gh_scopes_repo)"`). Both repository steps (no-GitHub and account
   mode) must call `fd_checkout`.

3. **Behaviour.** `fd_checkout` is run against a local origin with a 40-hex ref and with a
   branch ref, on an absent checkout and on an existing one whose origin is migrated.

**Why it exists.** A caller that verifies a signed commit passes it as `RUN_BASH_GIT_REF`.
Only the no-GitHub path honoured the ref. The account path cloned or pulled the branch tip,
and `helpers.github443` and `helpers.github_scopes` ran from an unpinned checkout. A push
that landed after the caller's check was therefore provisioned unverified, while the log
claimed the declared ref was used.

**How to fix a finding.**

- **A new way to obtain or update the checkout:** call `fd_checkout <dir> <url> [git update args]`. Never call `git` directly.
- **New code run from a checkout before the repository step:** put an `fd_checkout` call
  ahead of it in the same section.
- **Never** narrow the patterns to clear a finding. Narrowing is the owner's decision.

### arch-pinned-artefact

**What it is.** A download, package or image reference that spells one CPU architecture
(`x86_64`, `x86-64`, `amd64`, `x64`, `linux64`) with no selection by the architecture the build
or host runs on: a release asset URL, a target triple, an RPM arch, a repo baseurl, a platform
key such as Chrome for Testing's `linux64`, an `lxc-create -a amd64`. The literal is allowed only
where it is the selection: a case arm label (`amd64)`), a map key (`x86_64:`), a line that
tests the architecture (`ansible_facts['architecture']`, `uname -m`, `dpkg --print-architecture`,
`TARGETARCH`, `GOARCH`, `platform.machine()`), or a line whose code names the ARM build beside
it (`aarch64`, `arm64`). Comment lines and trailing comments (a `#` or `//` after a space) are
prose, and an ARM name in a trailing comment hides nothing: `curl .../tool-x86_64.tgz # arm64 later` is reported. The rule is `.semgrep/arch-pin.yml`, proven against
`.semgrep/arch-pin.fixture`; run by `qa-arch-pin.bash` over every file git would publish but
documents, plans, tests and test data (the rule's excludes, each with its reason beside it) and
the trees `scripts/qa-trees.tsv` keeps out of every gate but `.semgrep` ([qa-trees-drift](#qa-trees-drift)). It is blocking with no allowlist: a finding fails the gate.

**Why it exists.** fedora-desktop runs on aarch64 as well as x86_64 (Plan 00243). Such a
reference installs nothing on the other architecture, or installs a binary that cannot run: the
ccy image did not build on an arm64 host because its Dockerfiles named x86_64 assets outright,
and Chrome for Testing has no linux-arm64 build at all. Found and defended under Defence Before
Fix (Plan 00243).

**How to fix a finding.** Select the artefact and its checksum by the architecture, one pinned
checksum per architecture, never skipped. In a Dockerfile (Debian based):

```bash
case "$(dpkg --print-architecture)" in
  amd64) asset=yq_linux_amd64; sha="${YQ_SHA256_AMD64}" ;;
  arm64) asset=yq_linux_arm64; sha="${YQ_SHA256_ARM64}" ;;
  *) echo "yq: no build for $(dpkg --print-architecture)" >&2; exit 1 ;;
esac
```

In a play, a map keyed by `ansible_facts['architecture']` (never the deprecated
`ansible_architecture`, which the ansible-patterns gate rejects; an unknown key fails the task,
and an `assert` naming the missing build says why) or a `when:` on it. Each upstream spells the
architecture its own way (`amd64`/`arm64`, `x86_64`/`aarch64`, `x64`), so map the name, never
substitute one into the other. Where no build exists for an architecture, use the alternative
(a distribution package) or fail on that architecture with a message naming the missing
component; never drop it silently. A sample string that installs nothing is still a finding:
write it with the running architecture, or move it into test data. **Never** add an exclude to
clear a finding; that is the owner's decision.

### secret-in-argv

**What it is.** A passphrase, password or other secret on a process's command line. Two rules
in `.semgrep/argv-leak.yml`, proven against `.semgrep/argv-leak.fixture.yml` (YAML, so
the Ansible rule reads it too), report under this one name:

- `secret-in-argv`, in any file: an option that takes a passphrase or password given an
  expanded value (`$var`, `$(...)`, a backtick, a Jinja `{{ }}`, a code identifier): ssh-keygen's
  `-P` and `-N`; `--password`, `--passphrase`, `--passwd` and `--pass`; openssl's `-pass` with
  `pass:`; a password-named variable (`$RC_PASS`, `${PASSPHRASE}`) given to grep, awk, sed or
  perl as an argument, outside a process substitution. A literal
  empty value (`-N ""`) is not a secret. The command is followed across line continuations and
  YAML or code list items. Prose that names the hazard is not judged (issue #221): a comment
  line, a comment after code (a blank, then `#` or `//` outside quotes, to the end of the
  line), and a Python docstring (the triple-quoted string that opens a file or follows a
  `def` or `class` line). Any other triple-quoted string is code, as it may hold a script.
  Comments of other forms (`/* */`, `<!-- -->`) are judged: the rule cannot tell where they
  start without misreading a glob such as `dir/*`.
- `secret-in-argv-ansible`, in YAML: a Jinja expression naming a secret (password, passphrase,
  passwd, secret, token, psk, credential, api key, private key, shadow, hash, salt, `_pp`) inside
  a task's `shell`, `command`, `raw`, `script` or `expect` text or argv, or inside
  `environment:`; and in `environment:`, any Jinja value given to a key so named
  (`UNIFI_ADMIN_HASH: "{{ unifi_admin_hash.stdout }}"`), whatever the value is called; and
  any Jinja `password:` of the user module, which runs `useradd`/`usermod -p HASH`. Not
  under `stdin:` or expect's `responses:`, and not the passwd database (`getent_passwd`), a
  secret's name, label, file or path, or a file checksum (`dockerfile_hash`, `sha256_hash`).

Run by `qa-argv-leak.bash` over every file git would publish but plan prose and changelogs
(the rule's excludes) and the trees `scripts/qa-trees.tsv` keeps out of every gate
([qa-trees-drift](#qa-trees-drift)). Blocking with no allowlist.

**Why it exists.** Every local user can read a running process's argv
(`/proc/<pid>/cmdline`, `ps`). `play-github-cli-multi.yml` wrote the GitHub SSH passphrase to a
file, then gave ssh-keygen `-P` the value `"$(cat /tmp/.github_ssh_pp)"`, which puts it straight back on
ssh-keygen's command line (issue #220). In Ansible the command line is wider than the command:
the shell module runs `/bin/sh -c '<the whole rendered script>'`, and `environment:` is rendered
into the line that starts the module, `/bin/sh -c 'NAME=value python3 .../AnsiballZ_command.py'`.
`no_log` hides the task's output, not either command line. A task's `stdin:` and a module
parameter (copy's `content:`) travel in the module's payload and never reach one. Found and
defended under Defence Before Fix (issue #220).

**How to fix a finding.** Give the secret to the process that needs it on stdin, or in that
process's own environment set by a program (bash `NAME="$value" cmd`, Python `env=`), never by
Ansible's `environment:`. ssh-keygen has no stdin option for `-P`/`-N`: let it ask, with
`SSH_ASKPASS=<helper> SSH_ASKPASS_REQUIRE=force` and a helper that prints the passphrase from
its environment; `helpers/ssh_passphrase` does this for the plays, taking the passphrase on the
task's `stdin:`. A tool that reads a secret from the environment is changed to read it from
stdin when a play runs it. A filter takes the pattern from a process substitution
(`grep -F -f <(printf '%s\n' "$pw")`) or from its environment (`s="$pw" awk '... ENVIRON["s"]'`).
A local user's password is hashed by `openssl passwd -6 -stdin` and set by `chpasswd --encrypted`
from `stdin:`, never through the user module's `password:`. A name-based rule cannot see a secret under an innocent name: name a
variable that holds one for what it holds. **Never** add an exclude to clear a finding; that is
the owner's decision.

### token-bundle-into-place-issued

**What it is.** A bundle issued with `--credential token` and sent into the placement,
`teamchat seat place-issued`. The rule in `.semgrep/place-issued-token.yml`, proven against
`.semgrep/place-issued-token.fixture`, finds the token credential flag (or the choice
of password or token) on the same line as a pipe into the placement, and a command built on one
line (a shell variable, a Go string literal) that a pipe into `place-issued` at most fifteen
lines later receives. Run by `qa-place-issued-token.bash` over every file git would publish but
plan prose and changelogs (the rule's excludes) and the trees `scripts/qa-trees.tsv` keeps out of
every gate. Blocking with no allowlist.

**Why it exists.** `seat place-issued` places a seat in a ccy pool, and a pool seat keeps a
password (gate B, the owner's ruling): it refuses a token bundle with exit 78. `--credential token` is for an LXC guest seat, which the lxc apply places itself. The cross-network guide, the
`team rotate-ca` help and stderr, and the X30 lab script all piped a token bundle into
`place-issued`, so whoever followed them got a refused placement and no seat (issue #248). Found
and defended under Defence Before Fix.

**How to fix a finding.** Issue the seat with `--credential password`; for a lost bundle,
`member reissue --handle H --credential password` piped into the same `place-issued`. **Never**
add an exclude to clear a finding; that is the owner's decision.

### unit-holds-boot-target

**What it is.** A systemd unit that a boot target wants (`WantedBy=` or `RequiredBy=`
`multi-user.target`, `graphical.target`, `default.target`, `basic.target` or `sysinit.target`),
whose start job stays open while it works (`Type=oneshot`, `notify`, `notify-reload`, `dbus` or
`forking`), that declares a start longer than systemd's default 90 s (`TimeoutStartSec=` above
it, `infinity` or `0`), and that is not ordered `After=` the target that wants it. The rule in
`.semgrep/boot-hold.yml`, proven against `.semgrep/boot-hold.fixture`, reads each unit from one
`[Unit]` to the next, with its keys at a line start, after a quote or after a `\n` escape, so a
unit file, a Jinja template, an Ansible `content:` block and a Go string are all read. The finding
is the unit's `[Unit]` line. Run by `qa-boot-hold.bash` over every file git would publish but plan
prose and changelogs and the trees `scripts/qa-trees.tsv` keeps out of every gate. Blocking with
no allowlist. Plan prose is left out because no unit is installed from it: run over it, the rule
finds only design text (Plan 018's first-boot unit, which is meant to run before the desktop, and
a design listing), and a unit taken from a plan into code is judged there.

**Why it exists.** A target that wants a unit is ordered after it by default, so it is reached
only once the unit's start job ends: for a oneshot, when the program exits; for notify, dbus and
forking, when the program says it is ready. A long declared start says the unit waits on
something slow. `fedora-desktop-self-update-verify.service` waited up to 25 minutes on the
restored sessions as a oneshot wanted by `multi-user.target`, held the target for 11 minutes, and
an external post-boot health check that waits for `multi-user.target` timed out on a healthy
guest (issue #263). In the user manager the same shape holds `default.target`, and with it
`user@UID.service` and a login. Found and defended under Defence Before Fix.

**How to fix a finding.** Order the unit `After=` the target that wants it: the target is then
reached first and the unit runs after it (systemd adds no default ordering that would make a
cycle). Where the unit is a long check that need not count as started only once it ends, make
it `Type=exec` and bound it with `RuntimeMaxSec=` instead of `TimeoutStartSec=`, so the boot's job
queue empties as soon as it runs; a failure still fails the unit. Where something must really
wait for it, keep the start and say why in the unit. **Never** add an exclude to clear a finding;
that is the owner's decision.

**What it does not see.** A unit whose `WantedBy=` is held in a separate constant from its
`Type=` lines (teamchat's `installLines` in Go), and a quick-looking unit with no declared bound
whose program is slow (a oneshot has no start timeout by default). The independent search of
issue #263 read every unit the repository writes for both; it found one of the second kind,
`kernel-version-manager.service`, which is ordered after `multi-user.target`. Widening the rule to
every oneshot with no start bound is the owner's decision (issue #263): it would flag
`lxc-docker-user-iptables-reconcile.service` and `ccy-sessions-restore.service`, both quick, and
neither has a free fix.

### secret-on-normal-screen

**What it is.** A secret written to a terminal's normal screen: a password, passphrase, token,
secret, credential, API or product key, named so (a name that ends in what it is, optionally
`_content` or `_value`; not the name, file, path, count or owner of one), or the output of a
command that prints one (`gh auth token`, the vault helper's decryption, `ansible-vault view`,
`secret-tool lookup`, `lpass show --password`, `claude setup-token`). Four rules in
`.semgrep/secret-screen.yml`: `secret-on-normal-screen` (shell, in any file that holds it: a
script, a template, a play's shell block or a bashrc a play writes) finds an `echo` or `printf`
of a secret alone, an unquoted heredoc holding one read by `cat`, a secret-printing command run as
a statement, a variable assigned from one and printed, and `setup-token` piped into `tee`;
`secret-read-echoed` finds `read` of a secret without `-s`; `secret-on-normal-screen-python`
finds a secret printed or written to a stream and `input()` asking for one;
`secret-on-normal-screen-ansible` finds a `debug` task that shows one. Output that is redirected
to a file, captured, piped into a consumer (not one that passes it on: `cat`, `tee`, `tr`,
`sed`, `awk`, `grep`, `cut`, `head`, `tail`, `sort`, `column`), or a function's last line (its
return value, judged at the call site) does not reach the screen and is not a finding. Run by
`qa-secret-screen.bash` over every file git would publish but documents, Go, test suites and
their data, and the trees `scripts/qa-trees.tsv` keeps out of every gate. Blocking with no
allowlist.

**Why it exists.** A terminal keeps what its normal screen showed in its scrollback, and tmux in
a pane's history, long after the program that printed it has moved on: a secret shown there
"once" stays readable to anyone who scrolls up, shares the screen or reads the terminal's or
tmux's log, and a play's output is also its run log. The alternate screen (the one an editor or
pager uses) keeps no scrollback. Issue #255 found the class in Go (rule
`project_secret_scrollback`); issue #261 found it in the vault helper, ccy's token flows, the
agent team bus's human password, the bashrc token functions and two plays. Found and defended
under Defence Before Fix.

**How to fix a finding.** Show the secret through the secret screen, which is plain when its
output is not a terminal (so a capture still gets the value) and on a terminal shows it on the
alternate screen, cleared when Enter is pressed: in shell pipe it through
`scripts/secret-screen.bash`, or in ccy's launcher and libraries (deployed without the checkout)
through `"$SCRIPT_DIR/claude-yolo" --secret-screen`; in Python call `show_secret` of
`helpers/agent_bus/screen.py`; in Go use `term.SecretScreen`. Read a secret with `read -s` (then
a newline on stderr) or `getpass`. In a play print no secret at all: name the command that shows
it on the secret screen (as the MOK enrolment steps name `./vault.bash get mok_password`).
**Never** add an exclude to clear a finding; that is the owner's decision.

**What it does not see.** A secret echoed inside a sentence (`echo "Token: $x"`): names such as
`SELECTED_TOKEN` hold a file's name there, so judging every such line is the owner's decision. A
function whose output is a secret called bare at a prompt, and a secret whose name does not say
what it is. The rules are line-based: a print inside a block whose whole output is piped to the
secret screen on a later line is still a finding, so such a block builds its text and prints it on
the line that pipes it (the vault helper's `dump`), or opens the pipe on the line of the heredocs
(ccy's token export).

### find-race-negated-stat-test

**What it is.** A find over a tree that changes while it is walked must judge only entries that
still exist. GNU find's `-ignore_readdir_race` makes an entry that vanished between readdir and
stat no error, but find still tests it. Every test that needs the stat (`-uid`, `-user`,
`-group`, `-perm`, `-newer`, `-size`, `-links` and the rest) is false for it, so a negated stat
test is true and the vanished entry matches.

The rule (`.semgrep/find-race.yml`) reports, in any published file, the option followed by a
negation (`!`, `-not`, alone or opening a group) of a stat test with no `-links` between them,
and a negated stat test followed by the option. It follows the command across shell line
continuations and across YAML or code list items, so an Ansible `argv:` list and a subprocess
list are read like a shell line.

**Why it exists.** The rootless store ownership probe (`tasks/podman-store-ownership-probe.yml`)
looked for a file owned outside the user's uid and subuid range with negated `-uid` tests. A
running container deleted a `/tmp` file during the walk, the probe printed it, and
`play-podman.yml` failed for a store that was clean (issue #152). Found and defended under
Defence Before Fix.

**How to fix a finding.** Put `-links +0` after the option and before the first negation. It is
true for every entry that exists and false for one that cannot be stat'ed, and find keeps the
stat it made, so every later test judges the same, existing entry. In a YAML `argv:` list quote
it (`"+0"`): unquoted it is the integer 0, Ansible passes `-links 0`, and the find matches
nothing at all, so the rule accepts only the quoted item. A test answered from the
directory entry alone (`-type`, with d_type) is no guard. `scripts/test-podman-store-probe-race.bash`
is the proof against a real churning tree. A find without the option over a live tree is not
reported: it prints the vanished name too, but exits non-zero, which a fail-fast caller stops on.
**Never** add an exclude to clear a finding; that is the owner's decision.

### qa-trees-drift

**What it is.** A gate that chooses its own files must leave out every published file
`scripts/qa-trees.tsv` keeps out of all gates: the daemon-deployed files above all (see
"Daemon-deployed files are never gated here"), vendored trees and the semgrep fixtures. Two Go
tests check this on the real tree, each failing with `qa-trees-drift` and the files the gate
would open: `TestEveryGateLeavesOutWhatTheTreesTableExcludes` (`src-go/qa/ghscopes`) for
`qa-gh-scopes`, `qa-find-race` and `qa-arch-pin`, and
`TestEveryBashFileTheTreesTableExcludesIsUnjudged` (`src-go/qa/bashlimits`) for
`qa-bash-limits`. They run in the Go gate (`qa-go-qa.bash`). The ghscopes test drives the
gate's own `handOver` over the real git, so removing the table filter turns it red. The defence
is a test rather than a static rule because the class is two configuration lists drifting
apart, which no analyzer over code can express; there is no single-rule harness or defence
listing for it beyond `go test -run` and this section.

**The one exception: `.semgrep`.** `qa-gh-scopes`, `qa-find-race` and `qa-arch-pin` still scan
`.semgrep/`, the other rules and their fixtures, minus each gate's own rule and fixture (its
`paths.exclude`), as they did before the table governed them: narrowing a gate is the owner's
decision, and whether to accept that narrowing is open (Q18 of issue #217). Each spec names the
rows it still scans (`stillScans` in `src-go/qa/ghscopes/run.go`); the drift test lets a gate
open only those rows' files, and fails if it then opens none of them.
`TestOnlySemgrepIsStillScanned` fails for any row but `.semgrep`, so a daemon-deployed or
vendored row can never be declared an exception. `qa-argv-leak` was added after the table and
leaves `.semgrep/` out with the rest.

**Why it exists.** A second list of trees that are not ours drifts from the first. The semgrep
rules of the ghscopes engine carried their own `paths.exclude`, and bash-limits its own
exemptions; after the owner's ruling only the table was changed, so arch-pin, gh-scopes and
find-race still opened the daemon's hook scripts, `.claude/ccy`, `init.sh`, `provision.sh` and
`mkplan.bash`, and bash-limits judged a daemon skill's script (issue #217). An upgrade shipping
a matching line would then block this repository, which the ruling forbids. Found and defended
under Defence Before Fix.

**How to fix a finding.** Make the gate read the table: the ghscopes engine drops every file
the table excludes before semgrep sees it (`src-go/qa/internal/qatrees`, which judges a file by
its language's list as the discovery gates do), so a rule's `paths.exclude` names only what is
particular to that rule. For bash-limits, whose single daemon-owned files must also be proven
shipped by an upgrade (`DaemonOwnedFiles`), add the tree to `Exemptions` as `Unjudged`, or the
file to `DaemonOwnedFiles` in its upgrade commit. A daemon skill is exempt by its own name, so a
project skill's script is still measured. **Never** delete a row from the table, or
narrow the test, to clear a finding; that is the owner's decision.

### walk-opens-non-regular-file

**What it is.** Python that opens an entry a directory walk found without first checking that
the entry is a regular file. The rule (`.semgrep/walk-open.yml`, taint mode) follows what
`os.walk`, `os.fwalk`, `os.scandir`, `os.listdir`, `glob.glob`/`iglob` and
`Path.rglob`/`glob`/`iterdir`/`walk` return, through any value built from it and into a set or
list it is added to (`update`, `add`, `append`, `extend`), to `open`, `io.open`, `os.open`,
`.open()`, `.read_text()`, `.read_bytes()`, `.imread()`, or a bare-named function whose name
says it opens (`_read`, `read_text`, `load_json`, `open_spool`), or `zipfile.ZipFile`.
`os.path.isfile(x)`, `x.is_file()` and `stat.S_ISREG(os.stat(x).st_mode)` (or `lstat`,
`x.stat()`) clear `x` only where they guard the open: in the branch the check holds (also with
`and`), in a comprehension's filter, after the check by short circuit (`isfile(x) and read(x)`),
after an `if` whose failed check leaves the block (`continue`, `break`, `return`, `raise`; also
with `or`), or after an `assert` of it. A check that only warns and falls through to the open, a
bare check statement or a check joined by `or` to the branch guards nothing. A function named
for regular files (`_read_regular`, `read_regular_text`) is the check itself, not a sink, so
its callers are not flagged; the companion rule `regular-read-helper-unchecked` holds such a
function to its name: its body opens with `os.open(..., O_NONBLOCK)` (a FIFO cannot hang the
open) and then refuses what `stat.S_ISREG(os.fstat(...).st_mode)` says is not a regular file. A
blocking open checked afterwards is too late, and a non-blocking open never checked reads a
socket or device as a file; both are findings. Run by `qa-walk-open.bash` over every tracked Python file, and by
`qa-changed` over each changed one. A daemon-owned file
that trips it is fixed upstream in hooks-daemon, never skipped.

**Why it exists.** A walk lists named pipes, sockets and devices as well as files, and opening
a named pipe for reading blocks until something writes to it. `play_text()` in
`helpers/play_ledger/group_vars.py` read every name `os.walk()` listed under a play's input
directories, reached the named pipes old plan runs left under `untracked/`, and
`changed_plays` (so `run.bash --changed`) never returned (issue #191). Found and defended under
Defence Before Fix.

**What it does not see.** Semgrep's open-source taint is intraprocedural: a walk in one
function whose entries are opened in another (a helper not named for opening, a function that
returns walked names to a caller that opens them), Python embedded in a shell string, and Go
(no analyzer yet). The sweep that defended the rule found those by reading and fixed them by
hand. A suffix filter (`name.endswith(".yml")`), `exists()` or `not is_symlink()` is not a
check: a named pipe can be called anything, and it exists.

**How to fix a finding.** Check `os.path.isfile(path)` or `path.is_file()` on the variable that
is then opened, before opening it, and decide what a non-regular entry means there. **Skip** it
where the walk lists whatever happens to be in the directory and a stray entry is not this code's
business (a scratch tree, `/proc`, a cache). **Refuse** it, naming the path, where every entry
should have been a file and its absence from the result would mislead: a play that silently
stops being a candidate, a response that makes an answered run look unanswered, a stamp
(`candidate_plays`, `helpers/vmtest/verify_response.py`, `helpers/agent_bus/declare.py`). Under `/proc` or `/sys` too: the
check costs nothing there and the rule cannot tell those trees from any other. Never satisfy
the rule by renaming a helper or moving the open out of sight.

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

### test-wall-clock-ceiling-under-a-second

**What it is.** An assertion, in any Python file, that bounds an elapsed time from above
by less than a second: `assertLess`, `assertLessEqual` or `assert ... <` / `<=` on a
difference (`now - started`, or a variable assigned one), against a number literal below
one second. Run by `qa-speech-to-text-rules.bash` over every tracked Python file.

**Why it exists.** `qa-all` runs while other agents build and test on the same host, and
a ceiling under a second is used up by scheduling alone. The speech-to-text tests failed
there and passed when run on their own: one checked that the microphone closed within
0.4 s of a second TERM, and another that a failed copy returned within 0.5 s. The class is
a test whose pass depends on a wall-clock margin too tight for a loaded host. The rule
catches the shape that can be judged from one line. Found and defended under Defence
Before Fix (the 261022 speech-to-text flaky-tests report).

**How to fix a finding.** Wait on the event when the test can see it: a stub's ready
file, a reply, `wait_until`. Where the time itself is the claim, give the bound about ten
times what it measures, and at least a second. When that would cross a second limit the
same test asserts, lengthen what is being timed against (for example, a stub model that
loads until the microphone has closed). Never drop the bound: a test that cannot fail on
slowness proves nothing about slowness.

### test-deadline-under-ten-steps

**What it is.** In a test function (one whose name starts with `test`), a `time.sleep`
inside a loop whose step is more than a tenth of a deadline-like keyword argument
(`heartbeat`, `timeout`, `deadline`, `grace`, `limit`, `attempt`, `confirm`, `budget`,
`wait`, `silence`, `max`) given earlier in the same function. Run by
`qa-speech-to-text-rules.bash`.

**Why it exists.** `SafetyStopTest.test_keepalives_keep_it_recording` sent a keepalive
every 0.1 s against a 0.3 s heartbeat. On a loaded host one late wake-up was enough, the
heartbeat ended the session mid-test, and the full gate failed. The same class as
[test-wall-clock-ceiling-under-a-second](#test-wall-clock-ceiling-under-a-second).

**How to fix a finding.** Give the deadline at least ten steps (heartbeat 1.0 s against
0.1 s keepalives), and keep the run longer than the deadline, so the test still shows
that only the repeated action keeps it alive.

**What neither rule sees.** A margin split across places: a stub script's `sleep 0.3`
inside a string against a constant patched in `setUp`, a timer that sends a signal before
the code under test reaches the state the test means, or a production constant such as
bash's whole-second `SECONDS`. Those were found by reading and are fixed in the tests. Look
for them when a test passes alone and fails in the full gate.

### stream-stand-in-not-a-text-stream

**What it is.** A class with a `write(self, text)` method and no `io` or `Protocol` base,
defined in a Python file that hands an instance of it over as a text stream: as a
stream-named keyword argument (`stdout`, `stderr`, `out`, `err`, `output`, `stream`,
`file`, `sink`, `unchecked`, `log`, `diagnostics`, `errors`, or ending `_stream` or `_sink`,
or starting `out_` or `err_`), directly or through a variable; as `sys.stdout` or
`sys.stderr`; to `redirect_stdout` or `redirect_stderr`; or assigned over a parameter
whose annotation names `TextIO` or `IO[str]` anywhere in it (`TextIO | None`,
`Optional[TextIO]`, `typing.TextIO`, a quoted `"TextIO"`). Run by
`qa-speech-to-text-rules.bash` over every tracked Python file.

**Why it exists.** Python accepts any object where a `TextIO` is declared and no type
checker runs here, so a stand-in with only `write()` passes every test that only writes.
The consumer may call the rest of the interface. `check_freshness` began writing a stale
play's detail with `writelines`, and the login report, which handed it a `_Sink` with only
`write()`, crashed with `'_Sink' object has no attribute 'writelines'` on every stale
play (Plan 00214). The class is a hand-rolled text-stream stand-in passed where a real
text stream is expected. Found and defended under Defence Before Fix.

**How to fix a finding.** Use `io.StringIO` to collect output. For a wrapper (a lock, a
filter), derive from `io.TextIOBase`, which supplies `writelines` and `print(file=)`
support through `write`, and delegate `flush` to the wrapped stream. Where several
threads share the stream, write each line with one `write` call: `print(file=)` makes two
writes, so another thread's line can land between them. Never add the one missing method:
the next consumer calls another.

**What it does not see.** A stand-in defined in one module and handed over in another, or
passed positionally. Those are found by reading; the sweep that defended this rule found
none.

### completion-published-before-cleanup

**What it is.** Python cleanup that runs, in the same function, after the statement that
tells another thread a job has ended. The published end is an assignment to an attribute
named `state`, `_state`, `status` or `_status` (alone or in a tuple, in a `with` block or
not), or `.set()` on an Event named for an ending (`finished`, `done`, `complete`,
`ended`, `exited`). The cleanup is a path removed (`unlink`, `rmdir`, `os.remove`,
`os.unlink`, `os.rmdir`, `shutil.rmtree`), a process signalled (`terminate`, `kill`), or
an `update_*()` call. A stop request (an Event named `stop`) is not an end. Run by
`qa-speech-to-text-rules.bash` over every tracked Python file.

**Why it exists.** A reader of the state (a client polling `FILE_PROGRESS`, `busy()`,
an Event wait) acts as soon as it sees the end, while the cleanup is still pending.
`wsi-stream-server`'s file job set `done` and only then deleted the marker that tells the
panel a job runs, so a poller told `done` could still see the marker. That made
`scripts/test-wsi-stop-grace.bash` fail one run in seven. The late `unlink` could also
delete the marker of a job a client started in the gap. The marker was moved first on its
own; the rule then found the same order left in the job's `update_activity()` (the idle
clock moved after a reader had judged it) and in the dictation's `abort` and `fail` (the
microphone signalled after the session said it had ended). Found and defended under
Defence Before Fix.

**What it does not see.** A process signalled with `os.kill` or `send_signal`, a
`self.update_*()` method, cleanup in a `finally` after a publish in the `try`, and a
cleanup made through a helper of another name. The sweep that defended the rule found
none of these by reading.

**How to fix a finding.** Do the cleanup first and publish the end last, so that "ended"
means "cleaned up". Where the cleanup must not race the threads the end stops, do both
under the lock those threads take, cleanup first. Never satisfy the rule by renaming the
state attribute or the Event while the order stays wrong.

### recorder-killed-leaving-its-files

**What it is.** JavaScript that kills a recorder, in a function (a method, a function
declaration or expression, or an arrow function) that never calls `discardLeftovers()`
(`extensions/speech-to-text@fedora-desktop/recorderLaunch.js`). A kill is a `discardSignal()`
call, a call given the signal name `'KILL'` or `'SIGKILL'`, an argv array naming `-KILL`, `-9`,
`-SIGKILL`, `KILL` or `SIGKILL`, a `send_signal(9)` or a `force_exit()`. Calls inside a
`node:test` `test(...)` are assertions, not kills. Run by `qa-speech-to-text-rules.bash` over
every tracked JavaScript file.

**Why it exists.** Escape kills a batch recorder's whole process group with SIGKILL, so
that nothing is transcribed. A killed process runs no cleanup, so `wsi` never removed
`/dev/shm/wfile-<user>.wav`. The room audio Escape was meant to throw away stayed until the
next recording or a reboot, and a next run whose recorder wrote nothing transcribed it. The
sweep found the same gap in non-server `wsi-stream`, whose live transcript
`~/.cache/speech-to-text/streaming-buffer.txt` stayed. Found and defended under Defence Before
Fix (Plan 00171 Task 5.12).

**What it does not see.** A signal name built at run time (`` `-${name}` ``, a variable), a
numeric signal other than `send_signal(9)`, a
kill whose function calls `discardLeftovers()` but never runs the removal it returns, and an
uncatchable kill in Python, bash or Go. The sweep that defended the rule read those by hand;
the panel's tests pin that the removal runs. `wsi-server-manager`'s SIGKILL of the server is not a
discard: the server keeps its journal and audio in the runtime directory on purpose, so that
the next server can hand back an undelivered dictation.

**How to fix a finding.** Kill through `discardRecorder()`. It sends the kill and returns the
removal of every file `discardLeftovers()` names for that recorder; `panelSession.js` runs it
once the recorder has exited, before anything new can start and write the same files. A recorder that
writes a new temporary file adds it to `discardLeftovers()` in the same change. The recorder
also removes a stale copy when it starts, but that is a second line of defence, not the fix.

### pw-record-stdout-not-raw

**What it is.** Python or JavaScript that runs `pw-record` or `pw-cat` with its output to
stdout (`-`, `/dev/stdout` or `/dev/fd/1`) and has neither `--raw` nor `-a`: an argv list, or
a command string whose stdout output ends the string or is followed by a pipe, a redirect or a
command separator. Run by `qa-speech-to-text-rules.bash` over every tracked Python and
JavaScript file.

**Why it exists.** What `pw-record` writes to stdout is the tool's choice unless `--raw` says
otherwise. pipewire-utils 1.6.10 began writing a Sun AU container there (`.snd`, big-endian
16-bit samples). `wsi-stream-server` and `wsi-stream`'s pre-buffer mode read the stream as raw
little-endian int16, so every sample was byte-swapped into loud noise that still followed the
speech; Whisper wrote each dictation down as "so so so", and it was pasted as a success. Found
and defended under Defence Before Fix (Plan 00229).

**What it does not see.** A command assembled at run time (a binary or output name held in a
variable the pattern cannot follow), and bash or Go callers. Both readers also check the first
bytes of the stream and fail the dictation on a container header (`.snd`, `RIFF`, `FORM`,
`fLaC`, `OggS`), naming it, so a format change the rule cannot see still fails loudly.

**How to fix a finding.** Add `--raw`. A recording to a named file (`wsi`'s `.wav`) is left
alone: its container follows the file's extension and is read by a tool that understands it.

### proc-zombie-taken-as-exit

**What it is.** Python that compares a process state with `Z` (`==`, `!=`, membership in a
tuple, list or set, `startswith`) in a function that names a `/proc` stat file (a string ending
in `/stat`, or `stat`, or the f-string `f"/proc/{pid}/stat"`) and names no thread count (a
string starting `Threads`, which makes the reading sound). Run by
`qa-speech-to-text-rules.bash` over every tracked Python file. The Go side of the same class is
the project rule `project_zombie_exit` ([GoProjectRules.md](GoProjectRules.md#project_zombie_exit)).

**Why it exists.** `/proc/<pid>/stat` is the thread-group leader's own entry, and its state turns
`Z` as soon as the leader thread exits. A killed multi-threaded program's other threads may still
be exiting then, and the files the process holds (a flock, a socket, a pipe end) are let go only
when the last of them drops the file table. teamchat's `TestClaudeStatusLine` read the status line
once the killed link showed `Z` and found `link.lock` still held (fedora-desktop#182); with a child
holding a flock and running 8 or 32 busy threads, the lock was still held at the first `Z` reading
in 65 and 155 of 300 rounds. Found and defended under Defence Before Fix.

**What it does not see.** A stat path built entirely at run time, and a state compared through a
variable holding `"Z"`.

**How to fix a finding.** Read `/proc/<pid>/status` and require both `State:` `Z` (or `X`) and
`Threads:` `1`: a thread is counted until it is released, which is after it dropped the file
table, so the pair holds only once the whole group has ended (0 of 600 rounds of the same
experiment found the lock held then). Better still, wait for the effect the code relies on (the
lock free, the line drawn) with a bounded wait.

### hook-path-resolved-against-cwd

**What it is.** Python in a module that imports `claude_code_hooks_daemon` in any form (the
project's handlers in `.claude/hooks/handlers/`) that resolves a path against the process's
working directory. `os.getcwd()` and `Path.cwd()` are always reported, as is `.resolve()` or
`.absolute()` at the end of a chain that starts with `Path(...)` not built from `__file__`
(`Path(p).expanduser().resolve()`, `Path(p).parent.absolute()`). `os.path.realpath`, `abspath` or `relpath` of a value, and
`Path(value).resolve()` or `.absolute()`, are reported unless the value is `__file__` or the
same block first has `if not os.path.isabs(value):` whose body joins the value to a root with
`os.path.join` or raises; a guard that does anything else excuses nothing. Run by
`qa-hook-handler-rules.bash` over every tracked Python file (the rule is in
`.semgrep/hook-handlers.yml`, its fixture `.semgrep/hook-handlers.py`).

**Why it exists.** A handler runs in the hooks daemon's process, and the daemon's working
directory is not the agent's project. `system_paths.py` judged a relative tool path with
`os.path.realpath`, so the verdict depended on where the daemon or its test ran. In the
container the working directory was `/workspace`, so the path was allowed. On the host it was
under `/home/`, so the path was blocked as a system file, and `qa-hook-handlers.bash` failed
there and nowhere else. Found and defended under Defence Before Fix.

**What it does not see.** A file-system probe (`exists`, `open`, `stat`) on a relative path,
a resolution through an alias (`from os.path import realpath`), `.resolve()` on a variable
holding a path (`p.resolve()`), a guard that joins the value to a relative root, and a guarded
value reassigned before the call. The sweep that defended the rule found none of these by reading.

A relative tool path is taken from the project root, not from the session's `cwd` in the hook
input; the two are the same directory for a session started in the checkout, and the daemon's
own `absolute_path` handler refuses a relative `Write`/`Edit` path as well.

**How to fix a finding.** Make the path absolute before resolving it. Join a relative tool
path to the project root (`system_paths._absolute`). Refuse a relative value where nothing can
anchor it, as `_resolve_project_root` refuses a relative `CLAUDE_PROJECT_DIR`. Never satisfy
the rule with an `isabs` check that leaves the relative path in place.

### ccy-container-default-pids-limit

**What it is.** A bash engine `run` or `create` (`podman`, `docker`, a variable with
`ENGINE` in its name, `container_cmd` or `ccy_agent_container`) anywhere under
`files/var/local/claude-yolo/` that passes no `--pids-limit`, or passes -1, 0 (or `00`) or
`max` (no limit to the engine). The engine word must be at command position: the start of a
line, or after `;`, `&`, `|`, `(` (so `$(`), `!` or a shell keyword, past any `VAR=value`
prefixes and an `exec`. The flag is looked for in that command and its backslash continuation
lines, up to `;`, `&&`, `||` or `#`. A comment line, and an `echo`/`printf` of a command (its
engine word is not at command position), do not count. The class it defends is "a ccy
container relies on the engine's default pids limit", whatever the container is for: the
session, the npm update, `setup-token`, or a probe. A Semgrep rule in
`.semgrep/bash-conventions.yml`, run by `qa-patterns.bash` and proven against
`.semgrep/bash-conventions.bash`. The Go front's runs (`ccy --help`, and the token check,
network check and version probe the bash asks it for: `engine.ClaudeHelp`, `RunWithSecret`,
`RunOnNetwork`, `ClaudeVersion`) take the same limit, held to the library by
`TestPidsLimitIsTheLibrarys`, by `TestTheLauncherChecksRunUnderThePidsLimit` and by the
difftest scenarios. The Semgrep rule does not read Go: its Go counterpart is the project rule
[project_container_pids_limit](GoProjectRules.md#project_container_pids_limit), which found
`ccc`'s two runs (its entry lists what it cannot see).

**Why it exists.** Without the flag the engine decides, and the limit is a session's
fork-bomb guard, so it must be a decision. A pid in the pids cgroup is a thread. A test
fixture inside a session recursed into itself and climbed to podman's default of 2048 in
about ten seconds; the kernel refused its forks ("fork rejected by pids controller"), Claude
Code's hooks failed with EAGAIN on `posix_spawn` and claude aborted (ccy exit 134), and the
limit kept the host whole. ccy had set nothing, so that bound was the engine's accident: under
docker, whose default is no limit, the same bomb reaches the user's own task limit and the
desktop. Found and defended under Defence Before Fix.

**What it does not see.** A run whose arguments are built in an array elsewhere, an engine
named by a variable without `ENGINE` in its name, a limit given by any variable (the value is
not read), a command split over lines without backslashes, and engine runs outside the ccy
tree. A `;`, `&` or `(` inside a quoted string before an engine word (`echo "a; podman run"`)
is read as a command start, a false positive to reword. Image builds are out of scope:
`docker build` has no `--pids-limit`.

**The wider class, referred to the owner.** "A container relies on the engine's default
limit" also holds for long-running containers that are not sessions, which the sweep found
and did not change, because their hazard is a different one or none:

- no pids limit at all (the docker and LXC default): the `dnode`/`dnpm`/`dnpx`/`dyarn`
  functions' `docker run` in `files/etc/profile.d/zz_lts-fedora-desktop.bash`, the LXC Docker
  host `files/var/local/docker-in-lxc` makes, and the unifi compose services
  (`play-unifi-controller.yml`). A fork bomb there reaches the user's or host's task limit;
  the fixes would be `--pids-limit`, `lxc.cgroup2.pids.max` and `pids_limit:`, with a rule of
  their own.
- podman's 2048, far above its workload: `ccdt`'s devtools viewer.

**How to fix a finding.** Pass `--pids-limit "$CCY_PIDS_LIMIT"`, settled by
`ccy_container_limits` (`lib/container-limits.bash`; docs/ccy.md, "Container limits"). Never
satisfy the rule with a literal that restates the engine's default.

### ccy-unattended-refusal-status

**What it is.** Under `files/var/local/claude-yolo/` and `files/var/local/claude-code/`, a launch
that stops on its unattended path any way but `ccy_unattended_stop` (`lib/unattended-stop.bash`):
an `exit`, or a `return` with a non-zero literal, at command position

- inside an `if` (or `elif`) guarded by `ccy_launch_unattended` or `ccy_nobody_to_ask`, or by a
  `[ -n "$CCY_RESTART_RELAUNCH_SESSION" ]` test (backslash continuations followed), up to its
  `else`, `elif` or `fi`, past one level of nested `if ... fi`;
- after a `ccy_prompt_refuse` or `ccy_restart_refuse` call at command position, up to the next
  `fi`, `else`, `elif`, `esac`, `;;` or closing brace;
- on the same line after `ccy_launch_unattended &&`; and, for the short-circuit forms
  `ccy_launch_unattended || ...` and `if ! ccy_launch_unattended`, anywhere later in the same
  function, because the rule cannot see where their unattended path ends.

The one exit allowed is `exit "$CCY_EXIT_UNATTENDED_STOP"`, inside `ccy_unattended_stop` itself. A
Semgrep rule in `.semgrep/bash-conventions.yml`, run by `qa-patterns.bash` and proven against
`.semgrep/bash-conventions.bash`.

**Why it exists.** A session's tmux pane runs its launcher through the registry's trampoline
(`registry.Trampoline`, `ccy_registry_trampoline`), which deletes the session's record when the
launcher returns: a returned launcher is an ended session. A boot restore and a restart's
relaunch run with nobody at the keyboard, and the launch stopped there with exit 1 where only a
person could go on: a token with no usage left, a launch question, a key that needs a
passphrase, a spent restart budget. The trampoline read each as an ended session and deleted the
record, so `verify-restore` said `DEAD launcher-exited` and nothing was left for the next restore
to start (fedora-desktop#283). `ccy_unattended_stop <reason> <sentence>` writes the reason word
and sentence to the file the trampoline names (`CCY_UNATTENDED_STOP_FILE`, beside the registry in
`stopped/<session>`, cleared on the next start) and exits `CCY_EXIT_UNATTENDED_STOP` (88), for
which the trampoline keeps the record; `verify-restore` reports `NOT-STARTED <reason>`. Found and
defended under Defence Before Fix.

**What it does not see.** An unattended test spelled any other way (`CCY_UNATTENDED_LAUNCH` read
directly); an `if` nested two deep before the exit; a function that returns non-zero to a caller
which then exits; a stop for want of a person that names no guard and no refusal call (the
GitHub probe's unattended timeout is one: it is decided inside a command substitution from a
local variable, so it returns `CCY_EXIT_UNATTENDED_STOP` and its caller in
`build_ssh_mounts_and_validate` ends the launch with `ccy_unattended_stop github-probe-unanswered`, held by `test-ccy-ssh-handling.bash`, not by this rule); and `exit`
spelled inside a string after `;`, `&` or `|`, which it reads as a command (a false positive to
reword). A launch fault (a failed image update, an unreadable file) keeps `exit 1`, and the
trampoline deletes the record as for any ended session.

**How to fix a finding.** End the path with `ccy_unattended_stop <reason> <sentence>`, after any
message the site already prints; the reason is one lower-case word with hyphens (the
`ccy_known_prompts` name for a launch question), the sentence says what a person must do. For a
restart, pass the session id as a third argument and it names the manual `ccy --resume`.

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

### restart-loop-hides-its-exits

**What it is.** A `while` loop that runs a child process, sleeps and runs it again, and
does not both record each exit where a status command reads it and have a way out. The
sibling of [ready-wait-ignores-child-exit](#ready-wait-ignores-child-exit), in the same
rule file and gate (`.semgrep/ready-wait.yml`, proven against `.semgrep/ready-wait.py`,
Python only): the loop body calls `subprocess.run`/`call`, a `.wait()` or a `run_child`,
and a `sleep`, and is cleared only when it also calls a function named `record_*` or
`_record_*` and contains a `return` or `break`. The body is judged by one regex over its
text, because semgrep drops a metavariable bound inside a body once the body itself is
bound.

**Why it exists.** A failure no restart can fix is retried for ever and is visible only
in a log. pingbus's supervised watcher exited 7 on a gap it could not read and was started
again every 60 s for days; only `watch.log` said so, `pingbus status` showed a merely silent
member, and the session-start notice nothing (issue #116). The supervisor now records each
exit in the team's `state/watcher.json` and stops once every team is stopped for a failure
the watcher judged permanent. Found and defended under Defence Before Fix. Whether an exit is
permanent is the program's own knowledge, which no pattern can judge: the rule holds the
shape that lets it be seen and stopped, not the judgement.

**How to fix a finding.** Hand each exit to a recorder that writes it where the status
command prints it (the cause and the restart count), and return when the record says a
restart cannot change the outcome; say so loudly when it does.

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

### homeserver-list-narrow-grammar

**What it is.** In first-party Python under `helpers/` and `CLAUDE/Plan/`, a list drawn
from a homeserver answer (a call to a method the rule's `HOMESERVER_CALLS` names, such as
`.call(`, `.sync(` or `.state(`, or a name bound from one, followed through assignments, loop
targets and into the parameters of a same-module function it is passed to) that is checked with
`protocol.is_room_id` or `protocol.is_event_id` so that one item refuses it all: inside
`all(...)` or `any(...)`, or in a loop whose `if` on the check raises. Run by
`tests/helpers/pingbus/test_identifier_grammar.py` (in `qa-helper-tests.bash`), which
proves the rule against its fixture first.

**Why it exists.** Those two checks are the bus's own grammar: the room-version-12 shape
only. A list the homeserver answers with holds whatever the server holds, of any room
version, so one legitimate foreign item refuses the whole answer. Tuwunel joins the first
registered account to its own server admin room, which it creates as version 11
(`!opaque:server`), and that one room stopped every team's side-room discovery.

**How to fix a finding.** Check the list with the Matrix spec's grammar
(`protocol.is_any_room_id`), so garbage is still refused, then select the entries the bus
can use with `is_room_id` and name the rest where the caller can see them. Do not widen
`is_room_id`: every room the bus creates or adopts is version 12.

### regex-backref-escape

**What it is.** A backreference (`\1`, `\g<name>`) in a string argument of `regex_search`,
`regex_replace` or `regex_findall` that reaches the filter with the wrong number of
backslashes. This covers the filter form and the by-name form (`map('regex_replace', ...)`).
Run by `qa-ansible.bash` over the YAML under `playbooks/ tasks/ vars/ environment/ roles/`. A
file the YAML library cannot parse is reported too, so its backreferences are never skipped
silently. Not read: the free-form `debug: var=...` and `args:` forms, the tests `match`,
`search` and `regex`, YAML aliases and mapping keys.

**Why it exists.** A group argument of `regex_search` must arrive as exactly one backslash
and the digit. Two backslashes fail the task with `'NoneType' object has no attribute 'group'`.
A replacement or pattern that arrives with two backslashes gives a literal `\1`, and nothing
fails. Two layers may strip backslashes before the filter sees them:

- **YAML** unescapes only a double-quoted scalar (`"\\1"` becomes `\1`). Plain, single-quoted
  and block scalars (`|`, `>-`) keep every backslash.
- **Jinja** unescapes everywhere except between `{{` and `}}` in task data, where Ansible's
  lexer cancels it. A bare expression has no `{{ }}`, so it is unescaped. That covers the
  conditionals (`when`, `changed_when`, `failed_when`, `until`, `loop_control.break_when`,
  assert `that`) and the debug action's `var`. `{% %}` statements are unescaped too. In all
  of these, `'\1'` (any of `\0` to `\7`) becomes an octal control character. An unknown
  escape keeps its backslash, so `'\g<name>'`, `'\8'` and `'\9'` arrive intact with either one
  or two backslashes.

So the same text is right in one place and wrong in another. `'\\1'` is correct in a
`>-` `changed_when:` and fails in a `>-` `set_fact:` value.

**How to fix a finding.** Write the spelling the message gives:

| Where the expression sits              | Plain, single-quoted or block scalar | Double-quoted scalar |
| -------------------------------------- | ------------------------------------ | -------------------- |
| `{{ }}` in task data, vars, set_fact   | `'\1'`                               | `'\\1'`              |
| bare conditional, debug `var`, `{% %}` | `'\\1'`                              | `'\\\\1'`            |

A replacement that puts a literal backslash before the group (three backslashes in
`{{ }}`) is allowed. To match a literal backslash before a digit in a pattern, write the
backslash as `[\\]` or `\x5c`, so it does not read as a backreference. The rule does not read
`.j2` template files. There Jinja unescapes, so `'\\1'` is the correct spelling. No tracked
`.j2` uses a regex filter today. The change that adds the first one also extends this rule to
read `.j2` files, so that the template's backreferences are judged.

### regex-class-escape

**What it is.** A letter escape (`\d`, `\s`, `\w`, `\S`, `\b` and the rest) in a regex pattern
that reaches the regex as a literal backslash and the letter, or, for `\b`, as a backspace
character. Read: the pattern argument of `regex_search`, `regex_replace` and `regex_findall`,
and of the tests `match`, `search` and `regex` (`is regex(...)`, `is not match(...)`), in any
namespace, and either given by name to `map`, `select`, `reject`, `selectattr` or
`rejectattr`. A pattern built with `~` has each of its string constants judged. Run by
`qa-ansible.bash` over the same YAML as [regex-backref-escape](#regex-backref-escape), which
owns the digits (`\1`); a file the YAML library cannot parse is reported. Not read:
replacements, module parameters such as `lineinfile`'s `regexp:` (no Jinja string literal
holds them), `.j2` templates.

**Why it exists.** Ansible's lexer keeps every backslash of a string constant between `{{` and
`}}`, and YAML keeps every backslash outside a double-quoted scalar, so `'\\d'` in a `>-`
`set_fact:` or `vars:` value reaches the regex as `\\d`: a literal backslash, then `d`. The
regex never matches and nothing fails. The `/etc/subuid` regex of
`tasks/podman-store-ownership-probe.yml` found no range this way, and the held-lock regex of
`tasks/podman-store-repair.yml` silently found no lock (Plan 00235). The backslashes are
counted as for backreferences: Jinja unescapes a bare conditional (assert `that`, `when` and
the rest), the debug action's `var` and `{% %}`, so `'\\s'` is right there, and so is `'\s'`
(Jinja keeps the backslash of an escape it does not know). The exception is `'\b'`, which
Jinja turns into a backspace there.

**How to fix a finding.** Write the spelling the message gives:

| Where the expression sits              | Plain, single-quoted or block scalar | Double-quoted scalar |
| -------------------------------------- | ------------------------------------ | -------------------- |
| `{{ }}` in task data, vars, set_fact   | `'\d'`                               | `'\\d'`              |
| bare conditional, debug `var`, `{% %}` | `'\\d'`                              | `'\\\\d'`            |

To match a literal backslash before a letter, write it as `[\\]` or `\x5c`.

### root-umask

**What it is.** A task that runs as root and creates files that users read, in a way that
takes the run's inherited umask, without making the files readable. Four shapes are reported:

- A `command`, `shell` or `raw` task that rebuilds a system cache or index without
  `umask 022` (or `0022`) earlier in the same command. The tools are `fc-cache`,
  `update-mime-database`, `gtk-update-icon-cache` (and the gtk4 one), `glib-compile-schemas`,
  `update-desktop-database`, `gdk-pixbuf-query-loaders`, `gio-querymodules`, `mkfontdir`,
  `mkfontscale`, `mandb`, `update-ca-trust`, `systemd-hwdb`, `udevadm hwdb` and
  `dconf update`. `ldconfig` is not in the list: it sets `/etc/ld.so.cache` to 0644 itself.
- A `command`, `shell` or `raw` task that writes a file under `/usr`, `/opt` or `/etc`
  (`wget -O`, `curl -o`, a `>` or `>>` redirect, `tee`) with no `umask 022` before it and no
  `chmod` to a mode others can read after it. `chmod +x` is not enough.
- The `git` module cloning under `/usr` or `/opt` without `umask: '0022'`.
- The `unarchive` module extracting under `/usr` or `/opt` without `mode:`.

Every module form is read: free-form, `cmd`, `argv` or `args:`, in any namespace. A `dest`
that is just one play variable is resolved from the play's `vars:`. A task counts as root
when `become` is in effect for it (from the play, a block or the task) and no `become_user`
other than `root` is set. A task list outside a play (`tasks/`, a role) is taken as root
unless it says otherwise, because those files are included by become plays. Whole-line `#`
comments in a shell block are not read. Run by `qa-ansible.bash` over the YAML under
`playbooks/ tasks/ vars/ environment/ roles/`. The rule is proven against
`tests/fixtures/ansible-root-umask/`.

**Why it exists.** Ansible has no umask setting for a command, so these files take the umask
the run inherited. On a host whose umask is 0077, the play's `fc-cache -f` left every file in
`/usr/lib/fontconfig/cache` at mode 0600, owned by root. No user could read them. fontconfig
quietly used each user's stale cache instead, and the config the play had deployed had no
effect (Plan 00185). Nothing failed. The handler that had already fired did not run again, so
re-running the play did not repair it. A binary downloaded the same way is 0700, and users
cannot run it.

**How to fix a finding.**

- **A rebuild.** Run it from a shell that sets the umask first. Also add a task that repairs
  the existing files on every run, so a host where the rebuild already ran under a restrictive
  umask is fixed even when the rebuild does not fire again. The repair is an
  `ansible.builtin.find` of the cache files, then `ansible.builtin.file` with `mode: '0644'`
  on each one; `play-fonts.yml` is the example.

  ```yaml
  - name: rebuild-font-caches
    ansible.builtin.shell: |
      set -euo pipefail
      umask 022
      fc-cache -f
    args:
      executable: /bin/bash
    changed_when: true
  ```

- **A written file.** Start the command with `umask 022`, or `chmod` the file to an explicit
  mode (`0755`, `0644`, `a+r`). Better still, use `get_url` or `copy` with `mode:`.

- **A clone.** Add `umask: '0022'` to the clone task.

- **An extraction.** Add `mode: u=rwX,go=rX` to the unarchive task. `unarchive` applies it to
  every file it extracts.

A rebuild run as a user, for that user's own cache, is not reported; give the task
`become_user` or `become: false`.

### sudoers-rule

**What it is.** A sudoers rule this repository installs that lets the user it is granted to
steer what it runs as root. `qa-ansible.bash` reads every task that writes sudoers text (a
`copy` or `template` under `/etc/sudoers.d`, a `blockinfile` or `lineinfile` on a sudoers
file, a `command`/`shell`/`raw` that names `/etc/sudoers`), the text it installs (the `src`
file under `files/`, or the inline `content`, `block` or `line`), and every task that deploys
a command the text names, plus the `put_file` lines of the root installers in
`files/usr/local/sbin/`. It reports:

- on the task: no `validate: visudo -cf %s`; a whole drop-in that is not `root:root 0440`; a
  rule written by a command; a source it cannot read;
- in the text: a `Defaults` that loosens the environment (`env_keep`, `setenv`, `!env_reset`,
  `secure_path`, `env_file`, `restricted_env_file`, `exempt_group`, `env_delete -=`,
  `env_check -=`); `#include`/`@include`; a `User_Alias` (or any alias but `Cmnd_Alias`,
  which is expanded); a rule for no fixed user (`ALL`, a `%group`, a template other than
  `{{ user_login }}`); a runas list with `ALL`; `SETENV`; a negated command; `sudoedit`; a
  command path that is relative or has a wildcard; a command with no argument list (sudo then
  admits any arguments), a wildcard argument, or a `^...$` expression not anchored at both
  ends or with a bare `.`;
- for each command: no task or installer deploys it; it is deployed with an owner other than
  root, with no mode, or with a mode the group or others can write; it is a link to a path
  that is not itself such a target; a task makes a directory above it owned by another user
  or writable by group or others.

A rule whose commands are `ALL` is not judged: it grants full root on purpose (the developer
desktop's passwordless sudo), so there is no narrow grant to subvert. Comment lines are not
read. The rule is proven against `tests/fixtures/ansible-sudoers/` (a flagged and a clean
tree) and the cases in `src-go/qa/ansible/sudoers_test.go`.

**Why it exists.** Plan 00231 gave the desktop user a NOPASSWD rule for `shutdown-with-update`.
A narrow rule is root if its target, a directory above it, its environment or its arguments
are the user's to choose: a user-writable script, `PATH` or `BASH_ENV` kept through sudo, a
`*` argument or a bare command (any arguments). None of that shows in the rule itself.

**What it cannot see.** What the target does with what it is given: a root script that runs a
program by a `PATH` the user sets, or reads a user-owned file, is judged by its own tests and
review (Plan 00231 records the audit of `shutdown-with-update`). The host's `/etc/sudoers`
defaults (`env_reset`, `secure_path`), which are Fedora's, not this repository's; the plan's
host acceptance check probes them. A test-only branch of an installer that sets another owner.

**How to fix a finding.** Pin each argument list the target accepts (`""` for none, a literal,
or an anchored `^...$` expression with brackets, no bare dot), grant it to `{{ user_login }}`
with `(root)`, no tags but `NOPASSWD`, no `Defaults`; deploy the target with a task that sets
`owner: root` and a mode such as `"0755"`, and install the drop-in with `owner: root`,
`group: root`, `mode: "0440"` and `validate: visudo -cf %s`.

### setting-checked-late

**What it is.** In a play, an `assert` (its `that`) or a `fail` (its `when`), or a block that
ends the play (its `when`: a skip), that tests whether a setting is there (`X is defined`,
`is not defined`, `is undefined`, `X | default(...) | length`) after a task that has already
changed the machine. A setting is a variable the install supplies in `host_vars`
(`localhost.yml`): not one of the repository's own (`vars/`, the inventory's `group_vars/`),
the play's `vars:` or `vars_prompt`, a task's or block's `vars:`, a loop variable, or a name an
earlier task produced (`register`, `set_fact`, the shared asker's `secret_name`). A change is
any task except the read-only modules (`assert`, `fail`, `debug`, `set_fact`, `stat`, `getent`,
`find`, `slurp`, `pause`, `meta`, the fact modules, `wait_for`), a `command`/`shell`/`raw` with
`changed_when: false`, a `uri` GET, a `check_mode: true` task and the shared asker; any other
include counts as a change, because the rule does not read what it runs. A flag read through
`| default(false) | bool` is its value, not a presence check, and is not reported. Run by
`qa-ansible.bash`; proven against `tests/fixtures/ansible-play-ux/`.

**Why it exists.** play-nvidia asserted the Secure Boot MOK password near its end, after it
had installed RPM Fusion, the driver and its build tools. A laptop without the password got a
half-done driver install and an assert telling the owner to edit the vault by hand.

**How to fix a finding.** Move the check before the first change. If the play can ask for the
value (a password, a key), ask for it there with `tasks/ask-and-save-secret.yml` instead
([vault-secret-by-hand](#vault-secret-by-hand)). A probe the check depends on may come first:
probes are not changes.

### vault-secret-by-hand

**What it is.** An `assert`, `fail` or skip (as in [setting-checked-late](#setting-checked-late))
that tests a secret, a setting whose name says password, passphrase, secret, token, psk,
`api_key`, `_key` or webhook, and whose message sends the person to make it by hand:
`ansible-vault`, `vault.bash`, `encrypt_string` or "vault-encrypt". Also a `blockinfile` or
`lineinfile` into `host_vars/` whose `block` or `line` puts in a secret as plain text, to be
vault-encrypted by hand afterwards. Run by `qa-ansible.bash`; proven against
`tests/fixtures/ansible-play-ux/`.

**Why it exists.** A consumer install's owner cannot be expected to run `ansible-vault edit`
because a play stopped. The play can ask instead, in plain words, and save the answer
vault-encrypted, so the next run never asks (the owner: "check for config/vault, create on
demand").

**How to fix a finding.** Ask for it, before the play changes anything, with the shared task
file, and drop the hand-edit message:

```yaml
- name: Ask for the MOK password when it is missing
  ansible.builtin.include_tasks: "{{ root_dir }}/tasks/ask-and-save-secret.yml"
  vars:
    secret_name: mok_password
    secret_label: MOK password
    secret_explain: What it is for, and where to get it, in plain words.
  when: mok_password is not defined
```

Its header lists the options: asking once for a pasted key (`secret_confirm: false`), an
optional value that an empty answer skips for this run (`secret_optional: true`), and length
bounds. A typing mistake is asked again, up to three times. A run with no keyboard fails with
the IaC route (`./vault.bash set <name>`). A secret another play creates is not asked for:
say which play to run first, without a hand edit (`play-unifi-firmware.yml` is the example).

### ansible-cli-without-chdir

**What it is.** A `command`, `shell` or `raw` task that runs an Ansible command-line tool
(`ansible`, `ansible-vault`, `ansible-playbook`, `ansible-galaxy`, ...) as a command, at the
start of the command or after a shell operator, `sudo`, `env` or assignments, by name or path,
without `chdir:` naming `{{ root_dir }}` and without a `cd "{{ root_dir }}"` before it. Every
module form is read (free-form, `cmd`, `argv`, `args:`). Run by `qa-ansible.bash`; proven
against `tests/fixtures/ansible-play-ux/`.

**Why it exists.** These tools find `ansible.cfg` in their working directory, and the
vault password file, inventory and roles path it names are relative to it. play-nvidia's
`ansible-vault encrypt_string` ran from the module's own directory, found no password file,
and failed, with `no_log` hiding why (2b074fb8a).

**How to fix a finding.** Add `chdir: "{{ root_dir }}"` to the task (in the module's mapping
or under `args:`).

### no-log-hides-failure

**What it is.** A task under `no_log` (its own, or a block's or the play's; a templated
`no_log` counts as on) that can fail on the machine, which is any module except `set_fact`,
`pause`, `debug` and `meta`, and is not a probe: `failed_when: false`, a `register:`, and a later
`fail` or `assert`, not itself under `no_log`, that reads the registered result; nor in a block
whose `rescue` has a `fail` or `assert` not under `no_log`. An `assert`
under `no_log` hides its own `fail_msg`. Handlers are read too. Run by `qa-ansible.bash`;
proven against `tests/fixtures/ansible-play-ux/`.

**Why it exists.** Ansible replaces the whole result of a `no_log` task, its error included,
with "the output has been hidden due to the fact that 'no_log: true' was specified". When
play-nvidia's vault-encrypt failed, that sentence was all the owner saw.

**How to fix a finding.** Keep `no_log` on a task that handles a secret, and make it a probe
whose failure a following task explains, printing only what holds no secret (the exit code,
the stderr of a command that read the secret on stdin, a module's `msg`):

```yaml
- name: Vault-encrypt the MOK password
  ansible.builtin.command:
    argv: [ansible-vault, encrypt_string, --stdin-name, mok_password]
    chdir: "{{ root_dir }}"
    stdin: "{{ mok_password_answer }}"
  register: mok_encrypted
  changed_when: false
  failed_when: false  # FAIL-FAST-OK: probe under no_log; the fail below gives the reason
  no_log: true

- name: Stop if the MOK password could not be vault-encrypted
  ansible.builtin.fail:
    msg: "ansible-vault could not encrypt it (exit {{ mok_encrypted.rc }}): {{ mok_encrypted.stderr }}"
  when: mok_encrypted.rc != 0
```

That probe works for a command, whose `rc` survives. Any other module (`copy`, `user`, ...)
reports failure only through its `failed` flag, which `failed_when: false` overwrites, so put
it in a block whose `rescue` fails readably, from `ansible_failed_result.msg`; a rescue that is
itself under `no_log` does not count:

```yaml
- name: Write the credentials file
  block:
    - name: Write it
      ansible.builtin.copy:
        content: "{{ vpn_password }}"
        dest: /home/{{ user_login }}/.config/vpn/.credentials
        mode: "0600"
      no_log: true
  rescue:
    - name: Stop because the credentials file could not be written
      ansible.builtin.fail:
        msg: "It could not be written: {{ ansible_failed_result.msg | default('no reason given') }}"
```

Where the task handles no secret (an `assert`, whose output names its expression and message
but never a value; a `blockinfile` of vault-encrypted text), drop `no_log` instead.

### probe-before-install

**What it is.** In a play, a `command` or `shell` probe (`failed_when: false`) whose program,
the first word of its command, a later `dnf`, `dnf5`, `package` or `yum` task installs by that
name, with no earlier task installing it. Run by `qa-ansible.bash`; proven against
`tests/fixtures/ansible-play-ux/`.

**Why it exists.** On a machine without the program the probe cannot run, and its
`failed_when: false` turns "not installed" into the probe's answer. play-displaylink probed
`mokutil --sb-state` before installing mokutil, so on a fresh install it read Secure Boot as
off and skipped the key enrolment the signed driver needs.

**How to fix a finding.** Install the program before the probe. For Secure Boot, include
`tasks/secure-boot-mok-password.yml`, which installs mokutil, probes, and asks for the MOK
password when it is needed and missing.

### module-built-for-running-kernel

**What it is.** A `command` or `shell` task that builds a kernel module or an initramfs for
the running kernel: `akmods` without `--kernels`, `dkms autoinstall`, `install` or `build`
without `-k`/`--kernelver`, or `dracut --force` without `--kver` (or `--regenerate-all`), or any
of them given the running kernel by `$(uname -r)` or `ansible_facts.kernel`. Each line of a shell
block is read, split at `&&`, `||` and `;`. Run by `qa-ansible.bash`; proven against
`tests/fixtures/ansible-play-ux/`.

**Why it exists.** The main playbook upgrades the system first, so the next boot runs a newer
kernel than the one running, and `kernel-devel` is installed for that newest kernel only. On a
fresh laptop install play-nvidia's `akmods --force` built for the running kernel, which has no
headers, and its module check found nothing (08ea913a5).

**How to fix a finding.** Name the kernel the next boot runs. `tasks/boot-kernel-version.yml`
reads it (`grubby --default-kernel`) and sets `boot_kernel_version`; pass it in the
environment, not templated into the shell line:

```yaml
- name: Rebuild the module for the boot kernel
  ansible.builtin.shell: |
    set -euo pipefail
    akmods --force --kernels "$KVER"
    dracut --force --kver "$KVER"
  environment:
    KVER: "{{ boot_kernel_version }}"
  changed_when: true
```

### command-always-changed

**What it is.** A `command`, `shell`, `raw`, `script` or `expect` task whose `changed_when`
is absent or the constant `true` (`yes`, `on`, quoted or not, a template of nothing else such
as `"{{ true }}"`, or a list of nothing else), in a play's tasks or a task
list, blocks opened. A `when` does not excuse it: a guard says when the command runs, not
whether that run changed anything. Not reported: any other `changed_when` (`false`, an
expression, a list, a template), a task with `creates` or `removes` (Ansible skips it, with no
change, once that path says the work is done), and a handler (it runs only after a change).
Run by `qa-ansible.bash`; proven against `tests/fixtures/ansible-command-always-changed/`.

**Why it exists.** Ansible cannot see what a command did, so such a task reports changed on
every run. A second run with nothing to do is never clean, and the recap hides the run that
did change something. Two host runs of play-ftp-camera in a row reported its
`systemd-tmpfiles --create`, `restorecon -R` and `nmcli connection modify` tasks changed with
nothing to change (issue #224). ansible-lint's `no-changed-when` is no defence here: no gate
runs ansible-lint, `.ansible-lint` only warns on it, and it passes `changed_when: true` and
any task with a `when`.

**How to fix a finding.** Decide changed from what was observed, never from a constant:

- A command that changes nothing (a query, a probe): `changed_when: false`.
- A command that says what it did: register it and key on its output; `restorecon -v`
  prints one `Relabeled` line per file it relabels.
- A command that says nothing: read the state first (a `stat`, a query command with
  `changed_when: false`), run the command only when the state is wrong, and key
  `changed_when` on that same check. The check must be of the state the command changes:
  a guard that only says the command can run (the profile exists) is no probe of what it
  sets (the profile's channel), and restating it is the constant again.
- A probe under `failed_when: false`: `changed_when: result.rc == 0`, since only there can
  a run that failed reach `changed_when`.
- A command that only makes or removes a path: `creates:` or `removes:` that path.

A finding whose right probe cannot be shown without running the command on a host is
referred to the owner, never suppressed or given a constant.

### changed-when-on-success

**What it is.** A task of any module whose `changed_when` is true on every run in which the
task succeeded, so it reads as a decision but is the constant `true`: the plain constant; the
task's own result (its `register`) tested for success while failure is left to the default
(no `failed_when`, or one restating `rc != 0` or `failed`): `rc == 0` (a command module's),
`not r.failed`, `r is not failed`, `r is succeeded` (`success`, `successful`), `r.failed == false`; at any time, a command module's `r.rc is defined` and any module's `r is not skipped`
(`changed_when` is read only on a run); a template of one; an `or` with one as a side; an
`and`, or a list, of nothing else. `ignore_errors` does not excuse it: a run that succeeded is
still changed. Run by `qa-ansible.bash`; proven against
`tests/fixtures/ansible-changed-when-on-success/`.

**What it leaves out, and why each cannot carry the hazard.**

- A command task whose `changed_when` is the plain constant: not left undefended,
  [command-always-changed](#command-always-changed) reports it, so it is not reported twice.
- A command task with `creates` or `removes`: Ansible skips it, unchanged, once that path says
  the work is done, so it runs, and reports changed, only on a run that has work to do.
- A handler: it runs only on a run in which a task reported a change, so it never reports
  changed on a run that changed nothing.
- A task whose `failed_when` lets a failed command through (`failed_when: false`, or
  `rc not in [0, 2]`): a run whose command failed reaches `changed_when` as a success, so
  `rc == 0` or `not failed` is false on such a run and tells runs apart.
- A condition on another task's result: that result is read from the host on each run (a probe)
  or says whether an earlier task changed something, so it can be false on a converged run.
  Where it restates the task's own `when`, the task is skipped on a run whose probe says there
  is nothing to do and reports changed only on a run it acts; every such task in the repository
  was examined (issue #282). Such a restatement would carry the hazard only behind a guard that
  does not read the state the task changes (a fixed setting), which this rule cannot tell from
  a probe.

**Why it exists.** Such a task reports changed on every successful run, so a second run with
nothing to do is never clean and the recap hides the run that did change something.
play-claude-devtools' image build said `git_clone.changed or image_build.rc == 0`, changed on
every run whether the build made a new image or was a full cache hit (issue #282).
[command-always-changed](#command-always-changed) caught only the literal constant.

**How to fix a finding.** Decide changed from what the task observed, as for
[command-always-changed](#command-always-changed): its output, or a state read before it
(an image build compares the image id before and after, as `tasks/claude-yolo-image.yml`
does); for a task that runs only when a probe says there is work, key `changed_when` on that
same probe; `changed_when: false` for a task that changes nothing.

### scratch-always-changed

**What it is.** A task whose only effect is on a scratch directory and whose `changed_when` is
absent or the constant `true`: an `ansible.builtin.tempfile` task, and a task of a module that
writes a path (`file`, `copy`, `template`, `get_url`, `unarchive`, `lineinfile`, `blockinfile`,
`replace`, `uri`, `git`, `archive`, `synchronize`) whose `path` or `dest` names a scratch
name. The scratch names are each tempfile task's `register`, then every `vars` entry (of a
play, a block, a task, an import or include) and `set_fact` key whose value names one, to a
fixed point over every file judged together, so a path handed to a task file through `vars`
is followed into it. A `when` does not excuse it. Command modules are
[command-always-changed](#command-always-changed)'s. Run by `qa-ansible.bash`; proven against
`tests/fixtures/ansible-scratch-always-changed/`.

**Why it exists.** A tempfile is new on every run, so making it, making a directory in it,
downloading into it and removing it each report changed every time. The play then reports
changes on a converged machine, and the recap that is never clean hides the run that did
change something. play-teamchat's controller build directory gave three changed tasks on
every pass (issue #247).

**How to fix a finding.** `changed_when: false`: the scratch is gone when the play ends, and the
task that installs from it (a `copy` to its real destination) reports the host's change. A
task whose `dest` is the real destination is not scratch and is not reported.

### ccy-image-sources

**What it is.** `CCY-IMAGE-SOURCES`: `files/var/local/claude-yolo/image-source-paths` is not
exactly the output of `scripts/git-hooks/lib/ccy-image-source-paths.bash`. That script reads the
Dockerfile's `ccy-builder` stage and prints, as git pathspecs, the source the stage builds from:
the packages its `go build` lines build, by `go list -deps` for linux on amd64 and arm64 (each
package's own files and what it embeds, vendored ones included, test files excluded), the module
files, and the pingbus zipapp's packages (`PACKAGES` in `helpers/pingbus/bundle.py`). Checked by
the pre-commit hook on a commit staging a `src-go/` file, the Dockerfile, `bundle.py` or the list,
and by `test-ccy-image-build-lock.bash` (the `ccy-image-build-lock` gate).

**Why it exists.** That one list is the image's own source three times over: the play keys the
builder stage on the last commit to change it, the stage takes only those paths from that commit,
and the container version gate counts a staged file as an image input by it. When the key was all
of `src-go` and the gate only the builder's Go closure, a Go change the image does not build from
rebuilt the image with no container version bump (issue 285), so the container version no longer
said whether two images differ. A hand-kept list drifts from the build the same way.

**How to fix a finding.** Regenerate and stage the list:
`scripts/git-hooks/lib/ccy-image-source-paths.bash files/var/local/claude-yolo >files/var/local/claude-yolo/image-source-paths`.
Never edit it by hand. A builder stage that needs a path the script does not name is taught to the
script, so the list, the key and the gate move together.

**Width.** The hook judges the list on the staged tree (exported from the index), not the working
tree. A package's directory is taken whole, so prose beside its code (a README) moves the key and
asks for the bump though nothing is built from it: over-inclusion, never a missed input. The
launcher's own rebuild check (`lib/common.bash`) reads the Dockerfile hash and the version label,
not this list; it follows the source because every change the list matches needs the version bump
it reads. Not built: a generic detector for any hand-kept path list feeding any build, since no
other build here keeps one (the independent search for issue 285 found none untied).

### image-build-arg-volatile

**What it is.** A `command` or `shell` task that runs an image build (a `build` word in its
command) with a `--build-arg` or `--label` whose value is new on every commit or every run:
the output of a command that reads the checkout's current commit (`git rev-parse HEAD` or `@`,
`git log` or `git show` with no `--` path limit, `git describe`), the clock (`date`) or a
fresh id (`uuidgen`), or the facts `ansible_date_time` and `now()`. Such a value is followed
from the task that registered it through every `vars` entry and `set_fact` key, to a fixed
point over every file judged together. Also reported: a `copy` task whose `content` is such a
value and whose `dest` is inside the build context of a build in those files (the build's last
word, an absolute path), where a `COPY` or a launcher that reads it back as a build argument
carries it into the image (`/opt/claude-yolo/source-commit`, which `lib/common.bash` passes to
its own builds). Run by `qa-ansible.bash`; proven against
`tests/fixtures/ansible-image-build-arg-volatile/`.

**Why it exists.** Every stage that reads the argument is built again whenever its value
changes, and the image gets a new id, so a value new on every commit rebuilds the image on
every commit whatever the commit touched. The build pays for work it did not need, running
sessions look out of date when nothing they use changed, and the play's changed status, read
from the image id, reports a change on every run. The ccy image's builder stage was keyed on
the checkout's HEAD and was rebuilt by every play run at a new commit (issue #277).

**How to fix a finding.** Key the build on what the image is built from: the last commit that
touched its source paths (`git log -1 --format=%H HEAD -- <paths>`), a tree id
(`git rev-parse HEAD:<path>`) or a content hash, and write that same key into the context. A
script that builds with a volatile value of its own is not judged here: the rule reads the
plays and task files only.

### install-guard-trusts-record

**What it is.** Work skipped on a record of itself, with nothing checking what the record
vouches for. Two shapes, in a play's tasks or a task list (a block's `when` carried down):

- A record guard. A `stat` (`path`) or `slurp` (`src`) task, with a `register` and no loop,
  reads a file. The work it guards is every later task whose condition reads that register,
  and every task whose condition reads the register of a task already in the work (a record
  written `when: install is changed`). The rule fires when the work makes that file from
  nothing (a `copy` with `content`, a `file` with `state: touch`), so the file is a record
  rather than an artefact, and the work also changes something its condition does not check:
  a path written by `copy`, `template`, `get_url`, `unarchive`, `file`, `lineinfile` or
  `blockinfile` (or a command's `creates`) that no `stat` read by the condition covers, or a
  change with no path the rule can know (a command, a package, a service) when the condition
  reads no read-only probe (`stat`, `slurp`, `find`, `getent`, `package_facts`, or a command
  with `changed_when: false`) of its own. A probe the record's content is made from (the
  source's checksum) checks the source, not the install, and does not count. Scratch is no
  change: a path under `/tmp`, `/var/tmp` or a `tempfile` task's path.
- A self-made marker. A `command` or `shell` whose `creates` names a file its own text
  `touch`es or redirects into, beside other steps.

Run by `qa-ansible.bash`; proven against `tests/fixtures/ansible-install-guard-record/`.

Not judged, and not this rule: a guard that reads one product of the work, or the directory
it installs into, rather than a record (`creates: ~/.ssh/id_lab` for an `ssh-keygen` that
also writes the `.pub`, a `stat` of an install root). Whether such a product guard should
read every product is a wider class with no static answer, because a command's products are
not in the play.

**Why it exists.** The record outlives what it vouches for. If the installed binary is deleted
or damaged and the record stays, the guard still says done, the play skips the install and
reports success, and the service fails at its first start. play-teamchat.yml's Tuwunel install
stat'ed only `tuwunel-<ver>-asset.sha256` (issue #227); the same search found
play-self-update.yml's collections stamp and play-rpm-fusion.yml's codec marker.

**How to fix a finding.** Check what was installed as well, and run the work when any of it
is missing or wrong: stat each artefact the work writes and read it in the condition
(`when: not binary.stat.exists or not record.stat.exists or ...`), or probe what a command
installs (a `stat` of each installed file it is known to make, a query with
`changed_when: false`). Where a module can say whether the state is in place (a package
group, a repository option), use it and drop the marker.

### filter-list-unchecked

**What it is.** A list a person writes (a variable that neither the play, its tasks, the
repository's `vars/` and `group_vars`, nor Ansible supplies) used to pick items from another
list by membership, when no `assert` (`that`) or `fail` (`when`) in the same play checks it
against what it picks from. A membership filter is `selectattr`/`rejectattr` with the `in` or
`not in` test, `select`/`reject` with `in`, `intersect`, or a loop whose `when` tests `item`
(or an attribute of it) `in` the list; one inside an `assert`, a `fail`'s `when` or a
`failed_when` is a check, not a pick. A check is the list followed by `| difference(...)` or
`is subset(...)` whose argument names what one of its filters picks from (the head of the
filtered pipe chain, or the loop), or a variable that is cut down to it by no other input
list: `teamchat_only_teams` checked against `tc_teams` covers the `tc_present` and
`tc_absent` cut from it by state, but `teamchat_approve_teams` checked against `tc_present`
does not cover `tc_scoped_present`, which `teamchat_only_teams` cut down. One finding per
list, at its first filter. Run by `qa-ansible.bash`; proven against
`tests/fixtures/ansible-filter-list-unchecked/`.

**Why it exists.** A name in the list that matches nothing is dropped without a word: a typo,
or a name the other list was narrowed to leave out, is never acted on, and the play reports
success. play-teamchat.yml never approved a team named in `teamchat_approve_teams` but outside
`teamchat_only_teams`, and took a misspelt `teamchat_only_teams` as a scope with nothing in it
(issue #227).

**How to fix a finding.** Refuse the input before anything changes: in the play's input
check, assert `<list> | difference(<what it picks from> | map(attribute='<key>') | list) | length == 0`
(or `<list> is subset(...)`), against the list the filter really picks from, and say in
`fail_msg` which names are unknown.

### duplicate-managed-block

**What it is.** Two tasks, in any plays or task files the gate reads (handlers included),
that own the same region of the same file with different content. A `blockinfile` owns its
`path` (or `dest`) and `marker` (with `marker_begin` and `marker_end`; the default marker
when none is named); a `lineinfile` owns its `path` and `regexp` (else `search_string`, else
`line`). The content is the `state` and the `block` or `line`; a trailing newline of a block
does not count. A loop or `with_items` given as a literal list is expanded into its items; a
task whose path or region reads a loop the gate cannot read (a variable) is not judged.
Template whitespace does not count (`{{user_login}}` is `{{ user_login }}`). Run by
`qa-ansible.bash`; proven against `tests/fixtures/ansible-duplicate-managed-block/`.

**Why it exists.** Each run of one task rewrites what the other wrote. Both report changed on
every run, and what the file holds depends on which play ran last. play-basic-configs and
play-github-cli-multi both wrote the `~/.bashrc` block that sources `~/.bashrc-includes`, one
with `*` and one with `*.bash`; after github-cli-multi ran, an include not named `*.bash`
(play-qobuz's `qobuz-players`) was not sourced by new shells (issue #228).

**How to fix a finding.** Give the region one owner. The other play depends on the owner
(import or document the order, or assert the owner's marker as play-host-health-login-report
does) and drops its copy. Where one play chooses between contents, it does so in one task
whose content is a template, not in two tasks with opposite `when` conditions.

### offline-member-blocks

**What it is.** Code that treats a declared team member whose container or session is not
running as a failure or a precondition. Three detectors, run by
`tests/helpers/agent_bus_lxc/test_offline_member_rule.py` (in `qa-helper-tests.bash`), which
proves each against its fixture first:

- **A.** Bash that reads the declared LXC members (it names `agent_bus_lxc_members` or
  `MEMBERS_JSON`), in an active plan folder, `files/` or `scripts/`, that reads a container's
  state itself (`lxc-info -s`, a `${LXC_INFO[@]}` call), or enters a container (`lxc-attach -n`,
  a `${LXC_ATTACH[@]}` call) when neither the file nor another bash file of its plan folder
  calls `plan_lxc_online` or `plan_lxc_online_named` (which calls it) on a code line. That
  exemption exists because a plan's scripts are sourced together, so an entry in one file is
  of a container another file listed; a comment or a document naming the helper does not count.
- **B.** In team-member code (`helpers/agent_bus*`, `helpers/pingbus`, ccy-teams' packages,
  `declare`, `agentbus`, the `agent-bus-*` sbin tools, and the bash of A), a message that makes
  starting a member the way on: "start it, then ...", "restart that session, then ...",
  "Start container ...", "... with the container running", "then run <tool> again to start it".
- **C.** A Go flow under `src-go/internal/teams` that collects its LXC member rows into a list
  and returns `term.ExitFailure` when that list is not empty.

**Why it exists.** An offline member is normal lifecycle: members go offline and come back
([AgentNotes.md](AgentNotes.md#an-offline-team-member-never-blocks-anything)). A deploy's
pre-flight that refused a stopped LXC member stopped the whole deploy, and the same refusal
was copied into the next plan's acceptance.

**How to fix a finding.** In bash, list the containers with `plan_lxc_online_named <report>`
(in `CLAUDE/Plan/_members.inc.bash`). It prints the running ones and names each other one,
with its lxc-info state, as `<container> (container stopped): offline, not judged` on stderr
and in the report, never failing on it; it fails on a container lxc-info cannot read or one
that is ABORTING. Its base, `plan_lxc_online`, writes the offline names with the fixed prefix
`offline: ` so a caller tells them from other stderr. In Go or Python, ask the
container list or the member record. Then judge only the online members. A check that needs an
online member and has none says NOT JUDGED and does not fail the run. Do not weaken a check of
a running member: a running member with the wrong binary still fails.

**The one known instance it does not flag.** ccy-teams' role change
(`src-go/internal/teams/roles/roles.go`, `isMember`) still stops when a declared LXC entry's
checkout cannot be read and the team has no LXC member record at all, because nothing else says
whether the handle is that entry's. It advises `ccy-teams apply <team>`, never a start. It is
exactly one path, held under the method's allowance for a single recorded known instance, and
Plan 00211 Task 2.9 narrows it (an entry giving `host`, `seat` and `repo` is decided by them).

### lxc-attach-file-stream

**What it is.** A shell command that reaches `lxc-attach` while one of the descriptors it hands
lxc-attach as stdin, stdout or stderr is a file. A command reaches it when the tool, a variable
holding it (`LXC_ATTACH=(sudo -n lxc-attach)`) or a function that runs it is the command, or
follows a program that runs its arguments (`sudo`, `timeout`, `env`, `exec` and the like). The
descriptors are followed on the bash syntax tree: the command's own redirections and those of
every group, loop, subshell or function body around it; a pipe, `$(...)` or a process
substitution makes its end a pipe; dups (`2>&1`, `3>&1` then `1>&3`) carry what they copy. A
function is summarised by the caller's descriptors it passes on to lxc-attach, so a wrapper that
pipes every stream itself is no finding however it is called, and one that leaves stderr
inherited makes `wrapper ... 2>file` one. `/dev/null` (which lxc-attach skips),
here-documents and here-strings are clear; any other path is taken, a device node such as
`/dev/tty` as much as a file under `/dev/shm`. Run by `qa-lxc-attach-streams.bash` over every
tracked shell script but `CLAUDE/Plan/Completed/`, judged together, after proving the rule
against its fixture.

**Why it exists.** lxc-attach makes each of its standard streams that is not `/dev/null` the
container root's own ([AgentNotes.md](AgentNotes.md#lxc-attach-chowns-any-file-its-stdout-or-stderr-points-at)).
Plan 00211's stage-2 acceptance ran `session_run "${COORD}" pingbus ask-human ... 2>"${err}"`;
for an LXC coordinator `session_run` goes through `in_guest` to `sudo -n lxc-attach`, the file
became root's, and the `cat` and the duplicate-question check that read it back failed with
Permission denied although the right path had been taken. Found and defended under Defence
Before Fix (Plan 00211).

**How to fix a finding.** Hand lxc-attach pipes, best in the wrapper, so no caller can get it
wrong: the wrapper runs lxc-attach with stdout piped through `cat` and stderr swapped through a
second `cat` back onto its own stderr, and returns lxc-attach's status from `PIPESTATUS`
(Plan 00211's `in_guest`; Plan 00161's `u23_lxc_attach` pipes stdin too, but does not check
its stdout copy):

```bash
local -a piped
{
    (
        "${LXC_ATTACH[@]}" -n "${container}" -- "$@" 3>&- | cat 3>&-
        inner=("${PIPESTATUS[@]}")
        if [[ "${inner[1]}" -ne 0 ]]; then
            printf 'copying the stdout of a command in %s failed\n' "${container}" >&2
            exit 1
        fi
        exit "${inner[0]}"
    ) 2>&1 1>&3 3>&- | cat >&2 3>&-
    piped=("${PIPESTATUS[@]}")
} 3>&1
if [[ "${piped[1]}" -ne 0 ]]; then
    printf 'copying the stderr of a command in %s failed\n' "${container}" >&2
    return 1
fi
return "${piped[0]}"
```

The caller's `2>"${err}"` then reaches the file through the wrapper's own `cat`, which the
pipeline waits for, so the file is the caller's and complete when the wrapper returns. A
process substitution (`2> >(cat >"${err}")`) also hands lxc-attach a pipe, but the shell does
not wait for it, so a file read straight after can be short. Do not loosen the check.

**What it does not see.** A redirection made earlier by a bare `exec` (`exec 2>file`, or
`exec 3>file` and later `2>&3`), the tool inside a string another shell runs (`bash -c`,
`sh -c`, `ssh`), the tool or a runner held in a variable the rule does not tie to it
(`"${SUDO[@]}" lxc-attach`, `L=$(command -v lxc-attach)`), a command run through a function
that runs its own arguments
(`best_effort_rm what lxc-attach ...` without a runner word before the tool), and a script
whose own caller redirected it to a file. Function and variable names are pooled across every
script judged, so a wrapper one plan defines also makes a finding of a same-named function's
call elsewhere: a false positive, read and renamed if it ever happens.

### fixture-plain-member-bundle

**What it is.** Bash in an active plan folder, `scripts/`, `files/` or `tests/` that runs
`agent-bus add-member --out=<home>/<entry>`, where `<home>` is a value the same program (a plan
folder, whose scripts are sourced together, or one file elsewhere) gives `PINGBUS_HOME` on the
host. Names are followed through the program's assignments. `--out=-`, `--setenv=PINGBUS_HOME=`
(systemd-run, a dedicated agent user's own home) and `-e PINGBUS_HOME=` (podman exec) are not
findings. Run by `tests/helpers/plan_acceptance/test_plain_member_bundle_rule.py` (in
`qa-helper-tests.bash`), which proves the rule against its fixtures first.

**Why it exists.** A bundle the invoking user's pingbus loads is, in production, a seat: its own
0700 directory (`agent-bus seat take`), linked into the session home (the keeper). pingbus judges
the two shapes differently: `config.load_bundle` runs a `host` member beside the owner's Element
profile only through the link. Plan 00211's A3 wrote its worker straight into the home, so the
host refused it for a reason that is not the product's, and nothing after A3 ran.

**How to fix a finding.** Give the bundle a directory of its own (add-member makes `--out` 0700,
owned by the user who ran sudo) and link `<home>/<team>` to it with an absolute `ln -s`, as the
keeper does. Do not loosen the check in `config.py`.

### checker-copies-cited-constant

**What it is.** In an active plan folder, `tests/` or `scripts/` (the test suites), a checker
constant holding a literal (Python: a
module-level `NAME = <literal>`, or a literal wrapped in one call such as `pathlib.Path("...")`;
bash: a top-level `[readonly] NAME=` with no expansion) whose
own comment, the lines directly above it or a trailing one, names a product source file
(`.py` or `.go` under `helpers/`, `src-go/internal/`, `files/` or `extensions/`) together with
a constant that file defines: a word of the comment, or the checker constant's own name. Run by
`tests/helpers/plan_acceptance/test_cited_constant_copy_rule.py` (in `qa-helper-tests.bash`),
which proves the rule against its fixtures first. Its second clause needs no comment: the
module's `OWNED` names product constants (the ccy session home, the membership mount, variable,
file and directory names, the status line's inner-only variable, the checkout's seat directory,
the team-name grammar, the agent-bus state roots, the Tuwunel pin, ccy-teams' step marks and
STATE words, the label ccy-teams finds a session's container by, the message texts the product
names for its checkers, and the team units agent-bus's functions name, such as `checkout.py`
`hs_unit`, whose one `return f"..."` is read as a text with a placeholder), reads each value from
the product source, and reports a line of an active plan checker whose code holds one:
anywhere, as a file name after a `/`, as a directory between two, as a step mark anchored to a
line start (`^✓`), or as a whole path (`/var/lib/agent-bus` and below it, but not
`/var/lib/agent-bus-applied`). Comments, docstrings and the
checkers' own unit tests (`test_*.py`, whose fixtures are fake input) are not judged by it.

**Why it exists.** The comment says the value is the product's, and a copy of it drifts. Plan
00201's `m7_check.py` held `GUARD_TAIL = "exec pingbus status-line"`, a copy of `seat.py`
`STATUS_LINE_GUARD`'s tail; Plan 00213 changed the guard and check M3 failed a correct
session. The rule catches only the copies a comment admits to. A wider rule (any checker
literal inside a product constant) would report the Matrix spec, GitHub's address and install
paths, which the product does not own either; the copies it cannot see soundly are listed in
Plan 00201 Task 2.21, and the ones judged to stay copies, each with its reason, in that plan's
`subagent-reports/261027-task-2-27.md`.

**How to fix a finding.** Read the value from the product: import its module from this checkout,
lazily, through the repo root above the checker (`m7_check.py` `_checkout_module`). A checker
that runs where no checkout is mounted is handed the value by the script that starts it. Do not
delete the comment to quiet the rule: the copy is still a copy. For the second clause, ask
Plan 00201's `m7_check.py` for the value (`product-value FILE NAME` reads any named text of a
`helpers/` module or a `src-go/` file, or calls a `helpers/` function that names one with the
arguments given, as `product-value helpers/agent_bus/health.py hs_unit TEAM`; and
`product-pattern FILE NAME [PATTERN...]` the same
text as a `grep -P` pattern, each placeholder the pattern given, as Plan 00203's M3, M4 and M6
read ccy-teams' step lines from `src-go/internal/teams/hot/texts.go`; `session-paths`, `session-names`, `hot-marks`,
`status-line-names` and the other readers its usage lists group the common ones). When a checker needs a text the
product prints inline, name it as a constant in the product, use it there, and add it to
`OWNED`.

### chooser-skip-condition-text

**What it is.** A sentence in `docs/`, an active plan's markdown (not `JOURNAL/` or
`subagent-reports/`), the launchers, a Go usage text or a Go comment that says a team is taken or chosen
without a list (or alone), names covering or matching the checkout, and does not say the
machine has one team. Run by `tests/helpers/agent_bus/test_chooser_texts.py` (in
`qa-helper-tests.bash`), which proves the rule against its fixtures first.

**Why it exists.** `ccy-teams choose-teams` skips its team list only when the machine has
exactly one team, it covers the checkout and it is up (Plan 00201 `SPEC-many-teams.md` Q5,
S-B2). With two or more teams the list is always shown. Three texts said "the one covering
team" instead, and the host run of Plan 00177 read the correct list as a failure (Task 11.1).

**How to fix a finding.** Say the real condition in the sentence: the machine's only team,
when it covers the checkout, is taken without a list.

### assignment-masks-substitution-failure

**What it is.** A bash assignment whose exit status does not carry the failure of every command
substitution in the values it assigns. Bash gives a statement made only of assignments the
status of the last substitution it performed, a declaration builtin its own status, and a
command its own status over its prefix assignments. So the rule reports, on the `mvdan.cc/sh`
syntax tree:

- a statement of assignments only that holds two or more substitutions not nested in one
  another, across all its assignments: `x="$(a)$(b)"`, `x=$(a) y=$(b)`, an array literal
  `a=("$(a)" "$(b)")` or `a+=(...)`, and substitutions in an index, a `${v:-...}` default or
  arithmetic (`$(( $(a) + $(b) ))`);
- `local`, `declare`, `typeset`, `export` or `readonly` assigning any substitution, also spelled
  `builtin local`, `command export` or `\local`. shellcheck's SC2155 (gated by `qa-bash`) reports
  the plain scalar form, but not an array literal (`local -a a=("$(a)")`) nor a substitution in a
  default (`local x="${1:-$(a)}"`);
- a substitution in an assignment that prefixes a command (`X="$(a)" cmd`), whose status is
  never reported.

It runs in `qa-bash-limits.bash` over every bash file that gate finds, the ones exempt from the
limits too (test suites, plan folders, completed plans), but code this repository never edits
(`src-go/vendor/`, the daemon-owned files) and the semgrep fixtures in `.semgrep/`, which never
run. Each finding is `path:line: <shape>` with this identifier. It does not see bash held in an
Ansible `shell:` block or a template written by `blockinfile` in a playbook, a substitution in
a command's argument (`cmd "$(a)"`, where the command's status is reported), or one nested in
another (`$(a "$(b)")`, whose failure is lost inside the outer subshell unless
`inherit_errexit` is set).

**Why it exists.** Plan 00211's stage-2 acceptance read three product values in one array
literal; only the last read's status reached errexit, so `--fake` passed with the first value's
name gone from the product (Plan 00201 Task 2.22). Every shape above was run under bash 5.2 with
`set -e` and carried on past a failed substitution (Plan 00201 Task 2.26). Found and defended
under Defence Before Fix.

**How to fix a finding.** Give each substitution a plain assignment of its own, which errexit,
`if` and `||` see, then build the value from the variables: `a=$(x)` and `b=$(y)` on their own
lines, then `v="${a}${b}"`; `local v` then `v=$(x)`; for an array, `arr+=("$(x)")` once per
read; for a prefix, `pid=$(<file)` then `PID="${pid}" cmd`. Never quiet a finding with `|| true`
or by moving the substitution into an argument: that keeps the failure hidden.

### play-input-dropped

**What it is.** A checkout reference in a host play that the play's inputs drop. The rule reads
every YAML file a play under `playbooks/imports/` walks (the play and the task, vars and play
files it reaches), finds each checkout reference on a non-comment line, `{{ root_dir }}/<path>`
(or root_dir's own config lookup) or a Jinja join `root_dir ~ '/<path>'` / `root_dir + '/<path>'`,
and reports one whose path is not an input `helpers/self_update/affected_plays.play_inputs`
gives that play and is not named by an UNRESOLVED entry for its line. A reference to a Go module
root (a directory holding `go.mod`) counts as accounted for when the mapper took it as that
module's Go builds. The bare checkout root (`chdir: "{{ root_dir }}"`) is not judged. Run by
`tests/helpers/self_update/test_dropped_input_rule.py` (in `qa-helper-tests.bash`), which proves
the rule against its fixtures first.

**Why it exists.** `run.bash --changed` and the health report run a play again only when one of
its inputs changed. `tasks/build-go-command.yml` reached src-go as `root_dir ~ '/src-go'`, which
the mapper did not read, so no play building a Go binary ever counted its Go source and a change
to `src-go/internal/ui` left `play-podfreeze.yml` and `play-lxcfreeze.yml` unrun (Plan 00192
Task 3.6). Found and defended under Defence Before Fix.

**How to fix a finding.** Teach the mapper the form, so it follows the path or reports it
UNRESOLVED; a play's Go builds are its `go_command_name` commands and every package they import.
Never reword the play to hide the reference: the play still reads the path.

### play-program-read-dropped

**What it is.** A checkout file read by a program a host play runs that the play's inputs drop.
`play-input-dropped` reads the play text; this rule reads the programs. In every helper module
that is an input of a play, a path built from `__file__` and literal segments, or a quoted literal
naming an existing checkout path (a path with a `/`, or a dot-file of the root), in the module's
own package or not, must be an input. In every bash file that is an exact input, a `source` of a file beside
the script must be an input, and a `run_go_tool` / `run_go_command` (`scripts/lib/go-tool.bash`)
must be one of the play's Go builds, whose packages' literals naming checkout paths must be
inputs too. In a bash file the play runs (named on a line of its YAML that is not a `src:`), a
helper module it runs with `python3 -m` must be an input. Not judged: a bare climb from `__file__` to a directory (`--repo-root`'s default),
a source of a host path or a runtime variable, the literals of a Go command a play installs
(it reads the checkout, if at all, live when it runs), and a literal listed for its module in
`helpers/self_update/affected_plays.NOT_PLAY_INPUTS`. Run by `tests/helpers/self_update/test_program_read_rule.py` (in
`qa-helper-tests.bash`), which proves the rule against its fixtures first.

**Why it exists.** `helpers/github_scopes/cli.py` reads `vars/github-required-scopes.yml`
through `__file__`, so `play-github-cli-multi.yml` never ran again when the required scopes
changed; `scripts/qa-toolchain.bash`, run by `play-python.yml`, builds and runs a Go tool that
reads `src-go/go.mod`, and neither the library it sources nor the tool was an input (Plan 00192
Task 3.7). `play-agent-bus.yml` runs `agent-bus-install`, which builds the agent-bus zipapp from
`helpers/agent_bus` with `python3 -m helpers.pingbus.bundle`, so a change to the bus's code never
made the play changed and the host kept the old zipapp (Plan 00177). Found and defended under
Defence Before Fix.

**How to fix a finding.** Teach the mapper the form, so it counts the read or reports it
UNRESOLVED. A literal that is not a read of the checkout (named in a message, relative to another
directory, or read again by the program on every run, so no rerun is owed) goes in
`NOT_PLAY_INPUTS` under its module, with the reason. The table is in the mapper rather than the
modules because some modules are baked into the ccy image, where an edit costs a version bump.

### play-input-tree-untracked

**What it is.** A host play whose input prefixes (the directories every file of which counts as
an input) include the checkout root, a path outside it, or a directory git tracks no file in
other than a `.gitignore`. The rule asks git itself (`git ls-files`), not the mapper. Run by
`tests/helpers/self_update/test_input_tree_rule.py` (in `qa-helper-tests.bash`), which proves the
rule against a fixture repository first. The same file judges the hazard itself, apart from the
mapper's predicate: for every host play it records what `play_text` would read and fails on any
path under `untracked/` or outside the checkout, and it proves that check red on a fixture with
the mapper's fix reverted inside the test.

**Why it exists.** `play_ledger.group_vars.play_text` walks every prefix and reads every file in
it. `tasks/podman-store-old-range.yml` names `root_dir ~ '/untracked/'`, so play-podman and
play-claude-code-consumer counted the whole scratch tree as an input; `changed_plays` walked
it, opened a named pipe a plan run had left there, and never returned (issue #191). A runtime
directory a helper names (`.claude/ccy/pingbus/seats/`) and a spool under `untracked/` were the
same shape. Git never reports a change in such a tree, so it made no play stale either. Found
and defended under Defence Before Fix.

**How to fix a finding.** Teach the mapper (`helpers/self_update/affected_plays`): a directory
reference that holds no tracked content goes in `Inputs.local`, which no change matches and
nothing walks. Never reword the play to hide the reference.

### play-input-listed-past-git

**What it is.** A reader of play inputs that lists a directory from the filesystem instead of
through git. The inputs of a play are the files git would publish: tracked, and untracked but not
ignored (a new task file not yet committed still counts), never an ignored one. Run by
`tests/helpers/self_update/test_listed_past_git_rule.py` (in `qa-helper-tests.bash`), in two
halves. The code half reads every module of `helpers/play_ledger/` and `helpers/self_update/` with
`ast` and fails on any directory listing in them: a call named `walk`, `fwalk`, `scandir`,
`listdir`, `glob`, `iglob`, `rglob` or `iterdir` (`ast.walk` aside) or a `find` argv. The hazard
half plants ignored files in a fixture repository (a `.pyc` under a directory a play copies, an
ignored module in a helper package a play runs, an ignored play) and fails if `play_text`,
`play_inputs` or `candidate_plays` sees one.

**Why it exists.** `play_ledger.group_vars.play_text` walked each input directory and read every
file in it, so the `__pycache__/*.pyc` under four plays' helper trees counted as text the plays
read (issue #198). Bytecode changes with whichever interpreter last ran the code, and git never
reports a change to an ignored file, so a play's change and dirty judgement depended on the
machine. The mapper's helper-tree walk, its Go package listings and `candidate_plays` had the
same shape. Found and defended under Defence Before Fix.

**How to fix a finding.** List through `affected_plays.checkout_files` (`git ls-files --cached --others --exclude-standard`), which raises when git cannot answer rather than falling back to a
walk. Never filter out only the artefact names seen so far (`__pycache__`): every other ignored
file would stay an input. A path a play names exactly is not listed, so it stays an input even
when ignored (`environment/localhost/host_vars/localhost.yml` is local configuration the play
deploys on purpose).

### play-helper-input-unreachable

**What it is.** A host play whose helper Python inputs are not exactly the helper modules it
runs. The rule computes, with its own code (`ast`), the modules a play can execute: the modules
its YAML files name as `helpers.<pkg>.<module>` or as a rooted `{{ root_dir }}/helpers/...` path
(a directory is every module in it), and the modules a bash script the play runs (a rooted path
off a `src:` line) runs with `python3 -m`, then, transitively, every `import` and `from ... import`
anywhere in each module (relative ones too), every `helpers.x.y` and every `helpers/...` checkout
path a non-docstring string in it names (a child's `-m` argv, a directory a module packages). A
helper file `helpers/self_update/affected_plays.play_inputs` counts that this does not reach is a
finding, and so is one it reaches that is not counted. Not judged: a helper a deployed file runs
later, live from the checkout (a systemd unit, a bash tool): it reads the checkout each time it
runs, so no rerun is owed. Run by `tests/helpers/self_update/test_helper_reach_rule.py` (in
`qa-helper-tests.bash`), which proves the rule against its fixtures first.

**Why it exists.** The mapper counted the whole package of any module a play ran, and then every
package any module of that package imported. `helpers/gnome/check_panel_contract.py`, a QA gate no
play runs, imports the play ledger, so a change to the ledger's own code made
play-gnome-shell-extensions, play-fedora-desktop-panel and play-displaylink stale, though they run
only `helpers.gnome` modules that import none of it (Plan 00141). Ten plays carried the widening.
Found and defended under Defence Before Fix.

**How to fix a finding.** Teach the mapper the form: follow what the module runs, never its
package whole, and count a way of running a module the rule reaches that the mapper missed.

### suite-env-isolated

**What it is.** Every command `qa-all` and `qa-changed` start (each row of
`scripts/qa-gates.tsv`, each check and suite) runs in `gates.Isolation`
(`src-go/qa/gates/isolation.go`). It sets the locale to `C.UTF-8` (`LC_ALL`), and drops the
machine's `LANG`, `LANGUAGE` and `LC_*`. It drops every exported bash function
(`BASH_FUNC_*`), `BASH_ENV` and `ENV`. It also replaces each PATH directory that holds a command
this repository ships with a stand-in that holds everything else in that directory. The
shipped commands are the rows of `scripts/go-binary-suites.tsv` and the files of
`files/home/.local/bin`. A suite therefore finds the checkout's copy it put on PATH itself, or
nothing, and never a copy a play installed. The distribution's own directories (`/usr/bin`,
`/bin`, `/usr/sbin`, `/sbin`) are kept, because no play installs a shipped command there.
Every command runs under the umask `077` (`gates.SuiteUmask`), the CCY container's and a host
`cc` session's, under which the suites were written and recorded, whatever umask started the
run: a umask is the process's, so each runner sets its own before its first
command (`Env.Umask`, `syscall.Umask` in its `main`; a run without one is refused) and every
command inherits it. `HOME` is kept, because the suites give the program under test a scratch `HOME`, and go and
semgrep keep their caches in the real one. The guard is a test of each runner over the real
table or a run world, on a machine with an installed `ccy-token-usable` on PATH:
`TestSuiteEnvIsolated*` in `src-go/qa/all` and `src-go/qa/changed`. Its findings start with
`suite-env-isolated:`, and it is part of the `go` gate.

**Why it exists.** Three suites passed in the container and failed on the owner's desktop,
from that machine's environment alone:

- `ccy-session-registry` used `sort` under `en_GB.UTF-8`, which puts `-oc` before `--opencode`.
- `ccy-token-mode`'s "not installed" case found the `ccy-token-usable` installed in
  `~/.local/bin`.
- In `ccy-token-usage-command`, Fedora's exported `which` function made the ccy front hand
  `--help` to the bash launcher (F-spec: an exported function is a run it cannot model).

The verdict was a property of the machine, not of the code. A static rule over the suites could
see an unpinned `sort`, but it could not see which PATH directory a lookup reaches or what
exported function a child inherits. The runner is the one place all three dimensions can be
fixed and checked. Found and defended under Defence Before Fix.

The umask was the fourth dimension. The ccy launcher's differential goldens record the mode of
every file the launcher creates, which its umask decides. They were recorded in a CCY container
(umask `077`), so the GitHub runner (`022`) failed `TestFrontParity` in every scenario. The
goldens stay as recorded: the runner pins the umask they were recorded under, and the suite
pins it itself as well (`TestMain` in `src-go/internal/ccylaunch/difftest`), so a direct
`go test` agrees with QA.

**How to fix a finding.** Run the command through the session's seam: `exec` in `qa/all`,
`runCmd` in `qa/changed`. Both apply the isolation, so never build a `proc.Cmd` that bypasses
them. A suite that needs a program finds it on a PATH it builds itself: its own stub directory
first, or a scratch directory for a program it needs to be absent. It never relies on the
caller's PATH lacking that program. A suite that compares sorted output to a fixed string pins
`LC_ALL=C` at the `sort`, because the suite is also run by hand.

### gitleaks-config-not-as-written

**What it is.** A place in `.gitleaks.toml` that gitleaks, at the version the CI workflow
installs, does not apply as written: a key it does not read (it drops one without a word, keys
matched without regard to case), or a global allowlist (no `targetRules`) that pairs `paths`
with `regexes` or `stopwords` under `condition = "AND"`, whose files `gitleaks dir` skips whole
before any rule runs. Run by `tests/helpers/gitleaks_config/test_config_as_written_rule.py` (in
`qa-helper-tests.bash`), which proves the rule against its fixture first and fails when the
workflow's gitleaks version moves off the one its schema was read from.

**Why it exists.** The containerwatch allowlist said `matchCondition = "AND"` and promised
that another secret in the same file would still be reported. gitleaks reads `condition`, so the
entry was an OR; and as a global allowlist, its path made `gitleaks dir` skip the whole file
anyway. A planted token in that file went unreported. Found and defended under Defence Before
Fix when the agent-bus v2 merge added its own fixture allowlists.

**How to fix a finding.** Spell the key as gitleaks reads it (`condition`, `regexTarget`,
`stopwords`, `targetRules`), and name the rule an allowlist answers in `targetRules`, so its
condition is applied to each finding. Prove the entry with a planted value in a scratch copy of
the tree: a different value in an allowlisted file must still be reported.

### push-without-qa-verdict

**What it is.** A push that moves a protected branch to a commit GitHub CI has not passed. The
protected branches are the release and architecture lines, the `exempt` list of
`scripts/branch-policy.toml` (F44, F43, F44-arm, and F42), read as branch hygiene and
`branch-delete-list` read it. Run by the `pre-push` hook (`scripts/git-hooks/pre-push`, after
the private-names scan) through `scripts/qa-push-gate.bash` (Go: `src-go/qa/pushgate`, command
`qa/cmd/qa-push-gate`). For each ref that moves a protected branch, the gate asks GitHub through
`gh api repos/<owner>/<repo>/actions/runs?head_sha=<sha>&event=push` (owner and repo from the
remote URL) for the runs of `.github/workflows/qa.yml` on the new tip. That run is the diff
gate when the commit was pushed to a work branch (`qa-changed.bash` against the line's tip) and
the full suite when it was a protected branch's own push; either counts. The commit passes when
any of those runs completed with success; otherwise the push is refused (exit 1) with the newest
run's state (queued, in progress, failed, cancelled) and its URL, or "no QA run" when there is
none. Only push runs count: a pull request's run judges a merge of the commit, not the commit.
gh failing or not logged in, a remote that names no GitHub repository, or an unreadable policy
is exit 2. A push to any other ref, a deletion, and a push that leaves a branch's tip where it
was are not judged. `qa-all.bash` still writes `qa-all-verdict.json` in the git common
directory (`src-go/qa/internal/qaverdict`) as a record of the last local run; it gates nothing.

**Why it exists.** Twice a coordinator pushed F44 after a FAILED qa-all run (Plan 00203's
journal; issue #187). The gate was `grep -E 'QA (passed|FAILED)' log && git push`: a grep of the
printed text matches the FAILED line as readily as the pass. The first defence read a verdict
file the local qa-all wrote; the owner then made GitHub CI's verdict the gate for a protected
branch: a local verdict and a CI verdict do not both count. The full suite took about an hour
on every push, so the owner then chose "diff gate, full after" (issue #245): CI's verdict on a
work branch is `qa-changed.bash` in diff mode on a clean checkout, and the full `qa-all.bash`
runs on the protected branch after the move, where a red run is fixed forward. The hooks-daemon
handler tests (which CI exempts with `QA_HOOK_HANDLERS_EXEMPT`) and `qa-deployed-drift` (which
reads the host) are covered by `qa-changed.bash` before each commit.

**How to fix a refusal.** Push the commit to a branch, wait for CI, then push that same commit:

```bash
git push origin HEAD:refs/heads/ci/<topic>
gh run watch                       # or: gh run list --commit <sha>
git push origin <sha>:F44
git push origin --delete ci/<topic>
```

A failed run is fixed on the branch and pushed again; the protected branch takes only a commit
CI passed. A red full run on the protected branch after the move is fixed forward the same way. `QA_PUSH_GATE_EXEMPT=<reason>` passes the gate with the reason printed. It is set only
by the owner's tools (`scripts/release.bash`, whose release pull request CI judges, and
`scripts/mark-branches-superseded.bash`, which stamps retired branches), and it is an exception
awaiting the owner's decision (issue #187). An agent never sets it, and never pushes with
`--no-verify`.

### summary-count-omits-failure-source

**What it is.** A summary count that leaves out some of the failure sources it summarises, so a
failure reads as a failure of nothing. Run by `src-go/qa/all/headline_test.go` (in the Go gate),
which fails every gate of `scripts/qa-gates.tsv` alone in turn and requires qa-all's headline
to count that failure by its kind. A test is the detector because whether a count covers every
source is a property of the run, which no reading of the format string shows.

**Why it exists.** qa-all's headline printed the merged lint reports' error count, `✗ QA FAILED: 0 errors in 1814 files`, when the only failure was a helper-tests case (issue #183): a hard gate,
the helper suite or the drift check adds no lint error, and a failed stage was not counted at
all. Whoever read only the headline saw a failure with zero errors. Found and defended under
Defence Before Fix; `qa-changed`'s summary (it names every failing check) has no count to omit.

**How to fix a finding.** Count every kind of failure the summary covers, first, and label a
partial count as what it is: qa-all's headline is now `✗ QA FAILED: <K> gate(s) and <S> stage(s) failed; <E> lint errors in <N> files`, each kind named only when it failed.

### summary-counts-nongating-failure-as-pass

**What it is.** A run's closing line computed from the count of failures that gate the exit code
alone, so a unit that failed but deliberately does not gate (a read-only triage, an advisory
check, an upload that finished with errors) is reported as passed. Run by
`scripts/test-meta-deploy.bash` (gate `meta-deploy-queue`, and `qa-changed` when
`CLAUDE/Plan/meta-deploy.bash` changes): a plan whose triage exits 1 around a passing deploy
must keep exit 0, name the triage `FAIL ... non-gating`, count both triage runs in the closing
line and never close with "all N unit(s) passed". Its failing cases carry this identifier.

**Why it exists.** meta-deploy recorded a failed triage as a `note` row and closed `all 3 unit(s) passed.` over it (issue #203); a reader who stopped at the last line missed the failure. The
sweep found the same shape in `ftp-camera --copy` (a green "all uploads complete" over errored
files; `TestCopy` pins the fix) and `scripts/setup.bash` ("All preflight checks passed." after
an accepted version mismatch; host-only, fixed by reading). Sibling of
[summary-count-omits-failure-source](#summary-count-omits-failure-source).

**Toolchain gap, for the owner.** The defence is a test, which the method does not accept as a
detector, and it covers meta-deploy only. Two static forms were tried and neither holds the
class: a bash syntax-tree rule for a failure row appended to a results list with no counter
increment in the same branch matches meta-deploy's shape only (the ftp-camera and setup.bash
instances have no results list), and a text rule on closing lines (`all ... passed|complete`)
fires on every correctly guarded closing line, because whether the guard covers every failure
branch is data flow no detector here models (bash has no data-flow analyser; the Go analyzers
would need one per tool's own counters). Leaving the class undefended outside meta-deploy, and
not building that per-language data-flow rule, are the owner's decisions (issue #203).

**How to fix a finding.** Count every outcome the run records, gating or not, and derive the
closing line from all the counts: "all N passed" only when nothing failed, otherwise the passes
and each kind of failure by number (meta-deploy: `P of N unit(s) passed: B failed, were skipped or could not establish; T non-gating triage run(s) failed.`). Keep the exit code as it is; a
non-gating failure is still a failure in the words.

### unit-name-with-suffix-read-as-email

**What it is.** The commit hooks' email pattern refusing a name that is not an address because
its exemption is anchored at the last suffix of the allowed shape: a systemd unit was exempt only
when the token ended at the unit type, so a file named after a template unit (the unit name, then
`.golden`) was refused by `pre-commit` and by `commit-msg`, which share
`scripts/git-hooks/lib/secret-scan.bash`. Run by `scripts/test-secret-scan.bash` (gate
`secret-scan-tests`), whose cases carry this identifier: template unit files with further
suffixes pass, and an instance unit with a suffix, a real address under a `service` label, an
`@.` shape that is not a unit type and a real address beside a template file are all still
flagged.

**Why it exists.** Plan 00246 keeps each template unit's golden text beside its test, and a test
naming that file was refused as "Email address" (issue #204); its author built the name in code
rather than bypass the hook, and the next author might not. A template's `@` is followed
directly by `.`, which no address can be, so that shape with any further suffixes is exempt.
`ExampleValues.md` lists the exempt shapes.

**Toolchain gap, for the owner.** The defence is a test, which the method does not accept as a
detector. Two static forms were tried: a rule over the scanner's allowance regexes for one
anchored at its last suffix fires on the reserved-domain allowances, which must stay anchored
(`example.com` then another label is a real domain); and the scanner itself run over every
tracked line its pattern matches (703 lines) finds no instance, because the hook refuses each
one at commit, so the tree never holds them. Whether to record this as an undefended class is
the owner's decision (issue #204).

**How to fix a finding.** Exempt a non-address name only in a shape no real address can take,
anchored, and add a case beside it that a real address in the nearest shape is still refused.
A name whose tail is domain-shaped (an instance unit followed by a suffix) cannot be told from
an address: build it in code from its parts.

### exempt-token-shields-glued-address

**What it is.** The email pattern stops at the next `@`, so a real address glued straight onto
an exempt name (a template unit file, a `git@host` remote) split into an exempt prefix and an
orphan `@domain` the pattern does not match, and the address passed both commit hooks. Run by
`scripts/test-secret-scan.bash` (gate `secret-scan-tests`), whose cases carry this identifier.

**Why it exists.** Found by the conformance review of
[unit-name-with-suffix-read-as-email](#unit-name-with-suffix-read-as-email): its new allowance
took any further suffixes, so a glued local part read as one, and the older `^git@host`
allowance had no end anchor at all. The filter now extracts each token with whatever is glued
on after a further `@`, and the git allowance is anchored, so every exemption judges the whole
token. Run over every tracked line the email pattern matches, the filter keeps the same lines as
before. The detector is a test, for the reason the parent entry gives.

**How to fix a finding.** Anchor every exemption at both ends of the token, and never shorten
the token the exemptions see.

### tmpfs-file-bind

**What it is.** A systemd sandbox that binds a file (`BindPaths=` or `BindReadOnlyPaths=`,
`source[:dest]`) onto a destination inside a `TemporaryFileSystem=` path, or binds a source
nobody can say is a directory there. The rule is `internal/unitsandbox` (`unitsandbox.Check`,
over unit text: a unit file, a drop-in, a YAML block, and a transient unit's properties written
as string literals in code or as `systemd-run --property=` arguments), run by the Go gate's
tests: `TestNoShippedUnitBindsAFileInsideATmpfs` sweeps every file but Markdown outside
`src-go/` and the trees git does not publish (`untracked/`, `.claude/`, `node_modules/`) that
sets `TemporaryFileSystem=`, plan folders included, since a plan's probe starts transient units
(a source is a file or a directory as `files/` ships it, or a directory listed with its maker
in `knownDirectories`; anything else, a code placeholder included, fails closed); teamchat's
units and a team's transient sandbox are judged on their rendered text by `unitfiles`'
`TestNoUnitBindsAFileInsideATmpfs`; and `TestEveryGoRendererIsJudged` refuses a Go file that
writes `TemporaryFileSystem=` with no such test. A change to a shipped unit runs the Go gate
(`scripts/qa-routing.tsv`). Run it alone with
`go -C src-go test ./internal/unitsandbox ./internal/teamchat/unitfiles -run 'Tmpfs|GoRenderer'`.

**Why it exists.** systemd must create the bind's mount point inside the tmpfs it has made, and
on systemd 259 it cannot create a file there when the tmpfs is read only: the first real run of
`teamchat-server@` exited 226/NAMESPACE (EACCES, no AVC). v1's `agent-bus-hs@.service` had the
same two binds, and so, through `sandbox_properties`, did its restore's transient unit, and Plan
00161's H3 probe bound five files so. Found and defended under Defence Before Fix.

**What it does not see.** A tmpfs in one file and a bind in a drop-in of another are not joined.
A property built from pieces (not one `Key=value` string per line), or passed as `-p` with the
value as the next argument, is not read. No runner check reproduces the 226/NAMESPACE itself:
that needs systemd starting a unit on a host, which no gate does.

**How to fix a finding.** Put the files in one directory and bind that directory over the whole
path, dropping the `TemporaryFileSystem=` line (`BindReadOnlyPaths=<dir>:/run/systemd/resolve`).
A directory bound inside a tmpfs is allowed; a file never is.

### pidns-idmapped-state

**What it is.** A systemd unit that sets `DynamicUser=yes`, a `StateDirectory=` and
`PrivatePIDs=yes` together, each read by its last assignment. The rule is
`unitsandbox.CheckPIDNamespace`, over the same unit text forms as tmpfs-file-bind and run by the
same gate: `TestNoShippedUnitSetsPrivatePIDsWithAnIdmappedState` sweeps every shipped file that
sets `PrivatePIDs=`, `unitfiles`' `TestNoUnitSetsPrivatePIDsWithAnIdmappedState` judges
teamchat's rendered units and a team's transient sandbox, and `TestEveryGoRendererIsJudged`
refuses a Go file that writes `PrivatePIDs=` with no such test. Run it alone with
`go -C src-go test ./internal/unitsandbox ./internal/teamchat/unitfiles -run 'PrivatePIDs|GoRenderer'`.

**Why it exists.** Since systemd 257 a dynamic user's state directory is an idmapped mount. With
`PrivatePIDs=`, systemd forks into the new PID namespace first and then writes the idmap's
`uid_map` by pid through the host's `/proc`, which the kernel refuses, so the unit exits
226/NAMESPACE (systemd issue 41014, open, reported on 257 to 262 on x86_64 and aarch64). Any two
of the three start. `teamchat-server@` failed so on its first deploy, with the restore's staging
unit, which copies the same sandbox, bound to fail the same way. Found and defended under
Defence Before Fix.

**What it does not see.** Directives split between a unit and its drop-ins are not joined. No
runner check starts a unit: the proof of the fault itself is the deploy and the upstream issue.

**How to fix a finding.** Drop `PrivatePIDs=`. Where the per-process isolation it gave matters,
give the unit its own user (a dynamic user per instance) and `ProtectProc=invisible`.

### go-minimum-above-provisioned-go

**What it is.** A Go minimum a host build demands that is newer than the Go the IaC guarantees
the host, `golang_min_version` in `vars/golang.yml` (Fedora's golang, which `play-golang.yml`
fails below): the `go` directive of any `go.mod` outside `vendor/`, `testdata/` and the trees
git does not publish, or a playbook's `*_go_min` variable recording the `go` directive of a
module the play builds. A `toolchain` line is not judged. The rule is
`TestNoGoMinimumAboveTheProvisionedGo` in `src-go/qa/toolchain`, run by the Go gate; its
fixtures are the `ProvisionedGoRule` tests beside it. `qa-changed` runs it for a change to
either module, `vars/golang.yml` or `play-md-viewer.yml` (`scripts/qa-routing.tsv`). Run it
alone with `go -C src-go/qa test ./toolchain -run ProvisionedGo`. Its partner,
`TestEveryWorkflowSetsUpTheGoFloor`, holds every workflow to `actions/setup-go` v6 or later on
`src-go/go.mod`, so CI's Go is that file's `toolchain` line, never a `go` directive.

**Why it exists.** Every host build runs with `GOTOOLCHAIN=local` (Fedora's own `go.env` sets
it too), so a `go` directive above the installed Go is a hard refusal, never a download.
`src-go/qa/go.mod` was raised to `go 1.26.11` to get CI onto a Go security release before
Fedora packaged it, and every stock Fedora 44 host then failed `play-python.yml`'s QA toolchain
verify (issue #249). Found and defended under Defence Before Fix.

**What it does not see.** A module built from a pinned upstream commit whose `go.mod` is not in
this repository is judged only through its `*_go_min` variable, which is a copy the play's
comment says to move with the pin.

**How to fix a finding.** Keep the `go` directive at or below `golang_min_version`. To run CI
or the images ahead of Fedora for a security release, raise `src-go/go.mod`'s `toolchain` line
(which CI's `setup-go` installs, and the ccy image's builder stage matches); never a `go` line.
Raise `golang_min_version` only once Fedora ships that Go.

### matrix-client-unlisted

**What it is.** A Matrix desktop client that a play installs and that
`helpers/pingbus/matrix_clients.json` does not list. That file is the one list of the Matrix
clients this repository installs: each client's Flatpak app id (its data under
`~/.var/app/<app id>`), any native install's folders, and whether it keeps its session in the
keyring. Every guard that keeps an agent away from a human's Matrix session reads it or is
tested against it. The rule is `TestEveryMatrixClientInstalledIsListed` in
`src-go/internal/matrixclients`, run by the Go gate. It reads every YAML file under `playbooks/`
and `tasks/`. A Flatpak installed by `play-matrix-clients.yml`, or by any task file it imports at
any depth, is a Matrix client whatever its name. Anything else installed in any play (a
`flatpak_app_id`, a flatpak or package module's `name`, an app id in a `loop`) is a Matrix client
when its name carries a known Matrix client's word (riot, element, nheko, matrix, fractal and the
others in `matrixWords`). `TestTheUnlistedRuleFindsItsFixture` proves it against
`src-go/internal/matrixclients/testdata/unlisted/`. A change to the Matrix clients play, the list
or a guard runs the Go gate (`scripts/qa-routing.tsv`). A Matrix client added to another play
is caught by the full gate (`qa-all.bash`), not by `qa-changed.bash` for that play alone. Run it
alone with `go -C src-go test ./internal/matrixclients -run Listed`.

**Why it exists.** The guards named only Element Desktop. `play-matrix-clients.yml` also
installed Nheko and Lightning, and a sign-in to either keeps a Matrix session that no refusal
detected and no deny rule named, so a cc host seat could run beside a session the owner's
consent never covered (issue #251). Found and defended under Defence Before Fix.

**How to fix a finding.** Add the client to `helpers/pingbus/matrix_clients.json` with its data
paths and keyring use. Copy the file over `src-go/internal/matrixclients/clients.json`. Then
fix what `matrix-client-guard-uncovered` reports. A Matrix client installed as a package (an
RPM) has no Flatpak app id, and the list cannot express it yet: extend the list's format and
every reader together. A newly listed client is not covered by an owner's earlier consent, so
`cc --teams` asks the owner again (see `docs/agent-bus.md`).

### matrix-client-guard-uncovered

**What it is.** A guard that cannot read the Matrix client list at run time, and does not cover
exactly what the list says. These are cc's deny rules (`files/var/local/claude-code/cc-teams-settings.json`),
the member kit's launcher `agent-bus-claude` (which runs before pingbus and cannot read JSON),
and the list's Go copy `src-go/internal/matrixclients/clients.json`, which must equal the list
byte for byte. The tests are `TestTheDenyRulesCoverEveryClient`,
`TestTheKitLauncherCoversEveryClient` and `TestTheEmbeddedCopyIsTheList` in the same package.
For each client the deny rules must hold a `Read` and an `Edit` rule on its Flatpak data and on
every native folder (and each `-<profile>` folder), plus `Bash(*<app id>*)` and
`Bash(*<parent>/<name>*)`. While any client keeps its session in the keyring they must also
hold the Secret Service denies. The launcher's `MATRIX_PROFILE_PATHS` and
`MATRIX_PROFILE_PREFIXES` arrays, each on one line, must be the list's paths and prefixes in
order. Run it alone with `go -C src-go test ./internal/matrixclients -run 'Cover|Copy'`.

**Why it exists.** These guards spell out the clients by hand, so a client added to the list
would reach every guard that reads the list but none of these (issue #251).

**How to fix a finding.** Add the rules or array words the message names, or copy the list over
its Go copy. Never drop a client from the list to clear a finding.

### matrix-client-named-by-hand

**What it is.** Code that spells out a listed client's app id or a native folder
(`.config/Element`) when it is not the list, its Go copy, a guard covered by
`matrix-client-guard-uncovered`, or a file declared in
`src-go/internal/matrixclients/testdata/client-specific.tsv`. Those declared files configure a
client itself (Element's profiles, launchers and team CA, the installs), so they are not guards.
The test is `TestNoGuardNamesAClientByHand`. It reads `helpers/`, `src-go/`, `files/`,
`scripts/`, `tasks/`, `playbooks/` and `run.bash`, leaving out tests, fixtures, `testdata/` and
`vendor/`. Run it alone with `go -C src-go test ./internal/matrixclients -run ByHand`.

**Why it exists.** A guard that names one client by hand covers that client alone. That is how
pingbus's refusal, the status line's twin of it and the kit launcher came to cover Element and
nothing else (issue #251).

**What it does not see.** A client named in words ("Element") in text a person reads, such as a
consent screen or a refusal. Those texts are built from the list's names (`matrixclients.Names`,
`matrix_clients.names`); review keeps them so.

**How to fix a finding.** In a guard, read the list: `helpers.pingbus.matrix_clients` in Python,
`internal/matrixclients` in Go. A file that configures one client itself gets a row in
`client-specific.tsv` with what it does. A guard never gets a row.

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
runs — ruff (`qa-python`), semgrep (`qa-patterns`), shellcheck (`qa-bash`), golangci-lint (the hooks
daemon lints every Go edit with it; a Go test in `src-go/qa/toolchain` holds the pin equal to the one
go-qa-ci installs for the Go gate, so neither moves alone). Four
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

`./scripts/qa-changed.bash` runs only the checks the changed files need, before each commit, and
GitHub CI runs it in diff mode (`--base <the protected line's tip>`) as the gate on a work
branch; the slow `qa-all.bash` runs in CI after a protected branch moves. It is Go
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

| Changed file                                                                      | Checks                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Shell (`.sh`/`.bash`/shebang)                                                     | the toolchain pin, `bash -n`, shellcheck (`error`/`warning` gate, as in `qa-bash.bash`), the semgrep bash rules via the same temp-mirror idea                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Python                                                                            | the toolchain pin, ruff, and the mirrored `tests/helpers/x/test_y.py` for `helpers/x/y.py` (a changed test runs itself)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Playbook yml                                                                      | `qa-ansible-syntax.bash` on it (named; the gate's prerequisites apply), `qa-ansible.bash` and `qa-docs.bash` whole                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Task / vars yml                                                                   | `qa-ansible-syntax.bash` on every playbook naming the file by basename (one level), `qa-ansible.bash` whole; a deleted play, task or vars file, the playbooks still naming it (an import of a missing file fails the syntax check)                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Markdown                                                                          | `qa-docs.bash` whole                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `.js`/`.mjs`                                                                      | `node --check`; under `extensions/`, the project's eslint on those files with the js gate's pins; a change to the js tool, its wrapper or `extensions/.eslintrc.json` runs `qa-js.bash` whole                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `src-go/`, any file                                                               | `qa-go-qa.bash`, in go-qa's diff mode against the same base (the changed packages and their importers; everything when a `go.mod`, the go-qa configuration or any file of the qa module `src-go/qa` changed, since the qa module is the rule source; a narrowed run, possible only for a root-module change, measures the 90% coverage floor over the changed packages' `-coverpkg` subset, not the whole module, so it can fail where the full gate passes), whole when a file outside `src-go/` also routes to it or for `--files`; and the suites of every command whose build closure holds the file (`go list -deps`, `scripts/go-binary-suites.tsv`) |
| The ccy launcher, its `lib/*.bash`, its Dockerfile                                | `qa-go-qa.bash` whole: Go tests run them (two libraries are sourced as parity references, and the launcher's differential harness `src-go/internal/ccylaunch/difftest` runs the launcher over its scenario matrix)                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `src-go/go.mod`, `go.sum`, `vendor/`, the allowlist                               | `qa-go-deps.bash` and `qa-go-vuln.bash` whole (the allowlist: `qa-go-deps.bash`; `src-go/qa/go.mod`, govulncheck's pin: `qa-go-vuln.bash`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `CLAUDE/Plan/` `*.py`, `*.bash`, `*.sh`; `helpers/pingbus/`, `helpers/agent_bus*` | `qa-plan-tests.bash` whole (the plan tests load the `.py` and bash checkers beside them, and those checkers import the agent-bus helpers; the glob also matches `Completed/` and the plan tooling, which the gate does not read). Other repository files a checker reads (Go sources and goldens, the launcher, pins), and an extensionless Python checker, do not select it: `qa-all.bash` runs it whole                                                                                                                                                                                                                                                  |
| `.claude/hooks/handlers/`, any file                                               | `qa-hook-handlers.bash` whole (the handlers' Python also gets its per-file ruff and unit-test routing)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| Any bash file, the allowlist                                                      | `qa-bash-limits.bash` whole                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Any non-markdown file                                                             | each `test-*.bash` suite and hard gate script that `scripts/qa-gates.tsv` lists and whose text names the file by path or basename, or reaches it through a glob or a directory under a repository-rooted variable (`cp "$REPO_ROOT"/helpers/self_update/*.py`, `cp -r "$LIB_DIR"`; not under `src-go/`, which goes by build closure), and any suite that itself changed                                                                                                                                                                                                                                                                                    |

The classification is data, not code: `scripts/qa-routing.tsv` (the rules, in order: which
files get which checks, which are covered, which honour an excluded tree) and
`scripts/qa-trees.tsv` (the excluded trees, one list, read by the Go readers in
`qa/changed`, `qa/patterns` and `qa/discovery`; a table with no `shell` row or no `python`
row is exit 2 in all of them). All load
strictly, a malformed row is exit 2 naming the line. A deleted file has nothing left to check
but still owes the whole gates it implies (the bash-limits gate for a deleted bash file or
allowlisted path, the Go gate and the command suites for a deleted `src-go/` file) and selects
the suites that name or reach it. The gate scripts take no file list, so the same tools are invoked on
the changed files; the gates that are cheap (`qa-ansible.bash`, `qa-docs.bash`) run whole. A
changed file that maps to no check — a `.j2` no suite names — is listed as `not covered by qa-changed`, never skipped silently. Left to `qa-all.bash`: the Python-template
compile, semgrep parse-coverage assertions, helper-test counts, extension-compat,
panel-contract, deployed-drift, and every suite that neither names nor reaches a changed file (a suite that reads files
it finds at run time, such as a `git ls-files` walk, reaches nothing its text shows).

The proofs: the Go tests beside the code, the golden replay
(`src-go/qa/changed/golden_test.go`: the bash's own output over a fixed world, recorded from
git revision `f0284848` by `CLAUDE/Plan/00192-.../qa-changed-golden/capture.py`; each
difference is a numbered bug of that plan's `SPEC-qa-changed.md` section 10 pinned in
`testdata/deviations.json`), and `test-qa-changed.bash`, the black-box suite, which runs
unmodified against the wrapper or, with `QA_CHANGED_BIN=<built binary>`, against the binary.

---

## QA runs one at a time: the QA lock and low priority

The owner's rule (Plan 00237): **QA MUST NOT run in parallel.** A full `qa-all.bash` beside a
`qa-changed.bash` whose Go gate ran the whole race-and-coverage test suite once pushed a 22-core
host to a load of 63 while the owner was working. The rule is enforced by the tools, not left to
habit.

**The lock.** `qa-all` and `qa-changed` (the Go commands the two scripts run) and
`scripts/qa-go-qa.bash` run on its own (through `src-go/qa/cmd/qa-lock`) each take one lock
before their first check and hold it to their verdict: `<git common dir>/fedora-desktop-qa.lock`
(`git rev-parse --path-format=absolute --git-common-dir`), held with flock(2) by
`src-go/qa/internal/qalock`. The git common directory is the one every view of the repository
shares: the main checkout, every linked worktree, every CCY container that bind-mounts the
checkout, and the host. A flock belongs to the open file description of one inode, and a bind
mount shows the same inode, so a lock taken through one path is seen through every other on the
same kernel. The tests prove the two halves: `TestTwoProcessesShareTheLock` (two real processes
exclude each other) and `TestTheLockBelongsToTheInodeNotThePath` (a hard link, a second path to
the inode, sees the holder). A real bind mount needs a mount namespace no unit test sets up;
it rests on flock's per-inode semantics and is confirmed by a container run waiting for a host
run. The kernel
releases it when the holder ends, however it ends, so a dead holder never blocks a run. A
second clone of the repository has its own common directory and its own lock. `--plan`, `--help`
and a `qa-changed` with nothing changed take no lock; the build of the tool by
`scripts/lib/go-tool.bash` (a cached `go build`) happens before it.

**A second run waits; it does not fail.** The holder writes its record (pid, pid namespace,
command, start, worktree) into the lock file, and a run that finds the lock held prints, on stderr, once:

```text
qa: waiting for the QA run <pid> (<command>, started <time>, in <worktree>)
```

then `qa: still waiting for the QA run <pid> (<command>), <N>m so far` every minute, and starts
the moment the lock is free. The pid and the worktree are as the holder sees them: a run in a
container names its container's pid and path. A coordinator that starts a second QA run while
one is going gets a queued run, not an error.

- `--no-wait` (`qa-all`, `qa-changed`, and as the first argument of `qa-go-qa.bash`) exits `2`
  at once, naming the holder, for CI or a script that would rather fail.
- `QA_LOCK_TIMEOUT` (a Go duration, e.g. `30m`) bounds the wait: exit `2` naming the holder when
  it passes. Unset or `0` waits for as long as the lock is held.
- **Nested runs inherit the lock.** `qa-all` and `qa-changed` both run `qa-go-qa.bash`, which
  takes the lock when run on its own. A run that finds the lock held by a record whose pid
  namespace is its own and whose pid is among its own ancestors (a walk up `/proc/<pid>/stat`)
  does not wait. It is decided from the process tree alone, never the environment, so a gate
  that scrubs its children's environment (`env -i`) cannot make a run wait for its own parent;
  the namespace keeps the same pid number in another container from counting. The lock is taken
  once per top-level run and never deadlocks on its own children
  (`TestANestedRunInheritsTheRealLock` runs the real chain, through an `env -i` shell).
- **A SIGKILLed holder frees the lock with its children still running.** The lock's descriptor
  is not inherited (each gate runs in its own process group), so if `qa-all` itself is killed
  with SIGKILL the next run can start beside its orphaned gates. Ctrl-C and SIGTERM end the
  gates first; kill a QA run with those, never `kill -9` on the parent alone.

**Low priority.** Every `qa-all`, `qa-changed` and `qa-lock` run is a background job: the whole
process tree runs at nice 19 and in the idle I/O class (ionice class 3). Both are per thread on
Linux, inherited from the thread that creates a thread or process, so the command sets them on
its calling thread (`setpriority`, `ioprio_set`, in `src-go/qa/internal/proc/priority.go`) and
re-executes itself from it once at the top: everything it then runs starts low. A run already
low (started under `nice -n 19 ionice -c3`, or a nested run) does nothing. **`QA_FULL_PRIORITY=1`**
keeps the priority the run was started with (a machine with nothing else to do; CI does not
set it, and runs low).

---

## The release workflow — a release judged, signed and tagged by CI

`.github/workflows/release.yml` makes every release after a major's first (Plan 00153 Phase 5;
[docs/releases.md](../docs/releases.md) is the owner's page). Like branch hygiene it is its own
workflow and **not** a row of `scripts/qa-gates.tsv` nor a `qa-changed` check: it acts on a push to
`F<major>` and never judges a change. What decides a release is Go, `src-go/cmd/release-plan` over
`src-go/internal/release` (R1-R7 in its package doc) and `internal/releasever` (V1-V6), proven by
their unit tests; the workflow only runs it and acts on its answer. What the workflow promises is
held by `TestTheReleaseWorkflowKeepsItsPromises` in `src-go/qa/toolchain` over `release.yml`,
`pr-title.yml` and the local actions (every action pinned by commit SHA, each job's permissions
pinned exactly in the test so a widening is a reviewed change, no `pull_request_target`, no
workflow expression in a shell script, the key's secret only in a job of the `release`
environment and in no workflow-level or other job's `env`), which the Go gate runs, with a broken
fixture for each rule. `TestQAExemptsTheBranchesTheReleaseWorkflowPushes` holds that `qa.yml`'s
base check lets the `release-<version>` branch through, since the `tag` job needs a passing CI run
on that branch's commit. `.github/workflows/pr-title.yml` refuses a pull request title bound
for `F<major>` that is no conventional header, since the title is what the release reads. The
owner's one-time setup is a failure, never a skip, until it exists: the environment, its secret,
the deploy key, the tag ruleset and the setting that lets Actions open pull requests.

---

## Branch hygiene — the repository's branches, judged daily

`.github/workflows/branch-hygiene.yml` runs `scripts/qa-branch-hygiene.bash` on a daily
schedule, on every push to F44 and on demand. It is its own workflow and deliberately **not** a
row of `scripts/qa-gates.tsv` nor a `qa-changed` check: it judges the repository's global state,
so a branch's CI must not go red because of other branches. The command is
`src-go/qa/cmd/qa-branch-hygiene` over the rule in `src-go/qa/branchhygiene`; it reads
`origin`'s branches (so run `git fetch --prune origin` first when running it by hand).

**Why the limit exists.** It is there so that work FINISHES, never so that it disappears. Work
that is not on a branch being finished is lost. These "workarounds" are forbidden, because each
one hides work from every check:

- Saving changes as a patch or a copy under `untracked/`: no branch lists it, no CI sees it, it
  never reaches GitHub, and the next cleanup deletes it.
- `git worktree remove --force` or `git branch -D` on work not contained elsewhere.
- Stashes.
- Opening a side branch for a diagnostic or an experiment that serves another lane: it goes on
  that lane's branch as commits, reverted later if it is temporary.

When the rule seems to force a choice between losing work and exceeding the limit, stop and ask a
human. Losing work is never the answer.

The policy is data, `scripts/branch-policy.toml`:

- `cap`: at most this many open work branches (not exempt, not merged into F44). Over the cap
  fails, listing every open branch with its age.
- A branch merged into F44 and still on origin fails (MERGED-NOT-DELETED): delete it.
- `warn_days` / `fail_days`: a work branch with no commit for more than `warn_days` is warned
  about (DORMANT-WARN); for more than `fail_days` it fails the job (DORMANT-FAIL).
- `exempt`: the release and architecture lines (F44, F42, F43, F44-arm), never judged.

**An exempt branch is never a cleanup candidate,** whatever a triage or a verdict says. The
release lines (`F<nn>`, `F<nn>-arm`) are the per-version default-branch model: a machine on that
Fedora release pulls from its line.

**A bulk delete list is built only by `scripts/branch-delete-list.bash`,** never by hand or from a
triage's verdicts:

1. Push each candidate's tip to `refs/archive/<name>` first, so the delete loses no commit.
2. Put the candidate names, one per line, in a file (or on stdin) and run
   `./scripts/branch-delete-list.bash FILE`. It asks origin with `git ls-remote --symref origin`.
3. If it refuses (exit 1), it names each offender on stderr and prints no list. It never drops a
   name by itself: take the offender out of the list, or archive its current tip, and run it
   again. It refuses any name that is exempt or parked in `scripts/branch-policy.toml` (read by
   the same code as `qa-branch-hygiene`), matches `F[0-9]*`, is origin's default branch, is not
   a branch on origin, or has no `refs/archive/<name>` on origin at exactly the branch's tip.
4. When every name passes (exit 0) it prints one `git push origin --delete <name>` line per
   branch on stdout. Read every name, then the owner runs them; the command deletes nothing.

Exit 2 is "could not judge": bad usage, an empty list, an unreadable policy or list, or origin
unreachable. The command is `src-go/qa/cmd/branch-delete-list` over `src-go/qa/branchdelete`.

The owner's account bypasses the "Protect F-version branches" ruleset, so nothing on the forge
stops a delete the owner runs. And the signed-commits ruleset has no bypass, so an old release
tip that is not signed can be put back only by switching that ruleset off. Both happened on
2026-11-05: F42 and F43 were deleted from a triage verdict and restored from their archive refs.

**Parking a branch** waiting on a human decision: add an entry to `scripts/branch-policy.toml`,
in the same commit that records the decision being waited for.

```toml
[[parked]]
branch = "feat/example"
reason = "waiting for the owner to choose between two designs"
until = "2030-01-31"
```

A parked branch is shown under PARKED and is not judged for dormancy until `until` has passed
(it still counts against the cap). A park past its date fails, and so does an entry naming no
open branch: remove it when the branch is merged or deleted.

Exit 0 clean (warnings allowed), 1 findings, 2 when it cannot judge (no `origin/F44`, git
fails, unreadable policy). With `GITHUB_ACTIONS` set it also prints `::error::` and
`::warning::` annotations.

---

## When to Run What

| Changed files        | While iterating (before each commit)                           | Before a protected branch moves            |
| -------------------- | -------------------------------------------------------------- | ------------------------------------------ |
| Bash or Python files | `./scripts/qa-changed.bash`                                    | CI's `qa-all.bash` green on a `ci/` branch |
| Extension JavaScript | `./scripts/qa-changed.bash` (runs eslint on the changed files) | CI's `qa-all.bash` green on a `ci/` branch |
| Ansible playbooks    | `./scripts/qa-changed.bash` (syntax of the affected playbooks) | CI's `qa-all.bash` green on a `ci/` branch |

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

# 4. Commit, push to a branch for CI, and move F44 once CI passes that commit
git add files/home/.local/bin/wsi-stream
git commit -m "fix: update wsi-stream"
git push origin HEAD:refs/heads/ci/wsi-stream
gh run watch
git push origin "$(git rev-parse HEAD)":F44
git push origin --delete ci/wsi-stream
```

For changes that deploy nothing to `~/.local/bin/` (playbooks, `scripts/`,
`helpers/`, extension JS), the order does not matter — run QA whenever, as long
as `qa-changed.bash` passes before the commit and CI passes the commit before F44 moves.

## Rules Summary

1. **Run `./scripts/qa-changed.bash` before EVERY commit** that touches Bash, Python, Ansible or extension JavaScript
2. **A protected branch takes only a commit CI passed**: push to a `ci/<topic>` branch, wait for CI's diff-mode `qa-changed.bash`, then push that same commit (CI's full `qa-all.bash` runs on the protected branch after the move) ([push-without-qa-verdict](#push-without-qa-verdict))
3. **Fix all errors** before committing — QA failures indicate broken code
4. **Do not skip QA** — even for "small" changes
5. **Never add a suppression directive** (`# shellcheck disable`, `# noqa`, `# type: ignore`) — fix the code, or exempt in config with a stated reason

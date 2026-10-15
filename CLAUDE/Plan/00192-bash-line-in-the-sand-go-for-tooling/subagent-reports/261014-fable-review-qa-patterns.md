# Adversarial review: `go-port/qa-patterns` (Task 3.3, qa-patterns ported to Go)

Branch reviewed: `origin/go-port/qa-patterns` at `74f6fce1`, one commit on `origin/F44`
(`0700654d`). Spec: [SPEC-qa-patterns.md](../SPEC-qa-patterns.md), expectations P1-P18,
departures B1-B12; qa-changed deviation B33 in [SPEC-qa-changed.md](../SPEC-qa-changed.md).
Method: read the spec, the Go, the F44 bash and `qa-discovery.bash` side by side; build the real
binary (`go build ./cmd/qa-patterns`) and run it over the live tree, over the tree with 27
planted scripts (every rule's violation in every file shape the task names), and over a
disposable clone with the rules, the fixture, `PATH`, `QA_JSON_OUT` and stdout broken in turn;
probe semgrep 1.177.0 directly in a mini mirror to explain what the gate did; run `go vet`,
`go test`, `golangci-lint` (project config) over the two packages, `qa-bash-limits.bash`, and
`qa-changed.bash --plan` for the three files whose routing B33 changes. `qa-all` was not run, as
instructed. Paths below are repo-relative; the checkout's own path is written `<checkout>`.

## Verdict: fix F1, then merge

The port catches everything the bash caught and more (`#! /bin/bash`, `env sh`, `env -S bash -e`,
names with a space or a newline, a new untracked script), fails closed in every broken-tool case
with no report written, matches the bash on the live tree (433 scanned, the same 17 partial-parse
notes), and its departures B1-B12 are each a real fix of the bash. One thing should be fixed
before merge: the rule ids the gate prints and writes to the report are not rule ids but the
checkout's absolute path, dotted, with the rule id on the end (F1). It is carried over from the
bash unchanged, which is exactly what "the bash is not an oracle" is meant to stop; the spec
promises `<rule>:<line>` and the tests never looked. F2-F4 are evasions and blind spots the Go
could close cheaply because it owns the mirror copy; F5-F7 are drift and wording.

## Findings, ranked

### F1 — Medium — the "rule id" is the checkout path; stdout and the report carry it

`src-go/qa/patterns/semgrep.go:389-390` passes `--config <root>/.semgrep/bash-conventions.yml`
(absolute) and runs semgrep in the mirror; `report.go:801` prints `check_id` as the rule. semgrep
prefixes a rule id with the dotted path of its config file when that file is outside the scan
root, so every finding reads (planted `scripts/zz-rev-plain.bash`, `cmd || true`):

```
  ✗ scripts/zz-rev-plain.bash
    <checkout-with-slashes-as-dots>..semgrep.bash-error-hiding-or-true:2 Error hiding: ...
```

and `.failures[].error` in the report says the same. Three costs: the spec's P15/P17/P18
(`<rule>:<line> <message>`) do not hold; the output a human pastes into a plan or an issue names
the machine's checkout path (the public-repo rule); and the sort by "rule id" (P15) sorts by the
path first, so it is stable only because the prefix is constant. The bash had the identical
defect (`--config "$REPO_ROOT/..."`), so a port built from the bash's output would carry it, and
this one did. Why the tests did not see it: `helpers_test.go:255` fakes `check_id` as the bare
id (`bash-a`), and the one real-semgrep test (`cmd/qa-patterns/main_test.go:63-71`) asserts exit
codes only.

Verified fix: with the rules copied into the work directory next to the mirror and given to
semgrep as `../rules.yml`, `check_id` is the bare `bash-error-hiding-or-true` (probed on the
same semgrep). Add to the real-tree test an assertion that stdout contains
`bash-error-hiding-pipe-echo:2 ` with nothing before the id on that line.

### F2 — Medium — CRLF line endings evade two of the five rules

Planted `scripts/zz-rev-crlf.bash`, CRLF throughout, three violations on lines 2-4:

```
#!/usr/bin/bash
cmd || true
git x || echo fallback
x=$(cmd 2>/dev/null)
```

Reported: `bash-error-hiding-pipe-echo:3` only. `bash-error-hiding-or-true` and
`bash-capture-discards-status` end in `[ \t]*(#...)?$`, and `$` does not match before `\r`, so
the two lines are not matched. The rules are data and the bash had the same hole, but the Go
owns the bytes semgrep sees: `discover.go:270-278` reads each script and writes it into the
mirror verbatim. Normalising `\r\n` to `\n` in that copy (line numbers are unchanged, so every
reported line still points at the real file) closes it for every rule at once, present and
future, where editing five regexes closes it for five. A test: a CRLF script with `|| true` is
a finding on line 2.

### F3 — Medium-low — the symlink coverage line states something the gate did not check

`run.go:720` prints `⚠ patterns: N symlink(s) not scanned in their own right (their targets are, under their own names)`. `discover.go:175-191` classifies a link whose target is a regular
shell script as `kindSymlinkedScript` and counts it; nothing checks that the target is in the
set of scripts about to be scanned. Planted (untracked, not ignored):

```
scripts/zz-rev-link.bash -> ../untracked/scratch/rev/evil.bash   # target gitignored, has `|| true`
```

Output: `⚠ patterns: 2 symlink(s) not scanned ... (their targets are, under their own names)`,
exit 1 for other reasons, and `evil.bash` never scanned by anything. A link whose target is
outside the published set is a published path to shell that no gate reads. The only tracked
symlink today is `scripts/vault` (to a vendored directory, excluded), so the live tree is not
affected. Fix: `filepath.EvalSymlinks` the target, relativise it to the root, and either scan the
link under its own name when the target is not in the script set, or exit 2 naming it; the
coverage line then says only what was verified.

### F4 — Low-medium — a script under `tests/` or over 1 MB stops the gate with no cause named

Planted `tests/zz-rev.bash` (`|| true`): exit 2,
`ERROR: semgrep did not scan 1 discovered shell script(s): tests/zz-rev.bash` and nothing else.
Planted `scripts/zz-rev-big.bash` at 1.2 MB: the same message. Both are fail-closed, which is
right, but the cause is invisible: with `--quiet` semgrep's stderr is empty, and on 1.177.0
`.paths.skipped` is empty too (the default-ignore skips and the size skip are reported only
under `--verbose`). semgrep's built-in semgrepignore drops `tests/`, `test/`, `vendor/`,
`node_modules/`, `build/`, `dist/` and more; `--max-target-bytes` defaults to 1 MB. The repo's
`tests/` holds Python and fixtures today, so the live tree passes, but the first
`tests/*.bash` suite anyone adds turns the gate red with a message that blames semgrep.

Verified: an empty `.semgrepignore` written into the mirror root plus `--max-target-bytes=0`
makes semgrep scan all six probe files (`tests/`, `vendor/`, `node_modules/`, `build/`,
`scripts/`, and the 1.1 MB one). The large file then surfaces as a `Timeout` error, which the
Go already turns into exit 2 (B7) with the file and the type named, which is the true cause. The
mirror is the Go's to shape (P10), so this belongs in `buildMirror`/`scanIn`, with a test that a
`tests/x.bash` in the mirror is in `paths.scanned`.

### F5 — Low — qa-changed B33: the wrapper and the libraries it runs do not trigger the gate; one contradictory line

`scripts/qa-routing.tsv:40-41` route the rules, the fixture, `src-go/qa/patterns/*` and
`src-go/qa/cmd/qa-patterns/*` to `patterns`. In a clone, `qa-changed.bash --plan --files`:

- `.semgrep/bash-conventions.yml` → `check patterns` **and** `skipped (excluded tree): .semgrep/bash-conventions.yml`. Both lines are true of different things (the per-file checks
  are skipped, the whole gate runs) but printed together they read as a contradiction; this is
  also the exact text pinned in `testdata/deviation/file-semgrep-bash-conventions-yml.json`.
- `src-go/qa/patterns/run.go` → `check patterns`, `check go`. Correct.
- `scripts/qa-patterns.bash` → bash-syntax, shellcheck, semgrep, bash-limits on the wrapper;
  **no `patterns`**. The wrapper is the tool's entry point (`--scripts "$SCRIPT_DIR"`), and
  `scripts/lib/go-tool.bash` and `scripts/qa-trees.tsv` (read at run time, P7) route to `go`
  only. SPEC §4 says "run whole when the tool, the rules or the fixture change".

B33 itself is a genuine fix (before: the rules file was skipped as an excluded tree and nothing
ran), pinned by the two goldens, not a convenient pin. The wrapper gap is shared with
`qa-ansible.bash`, whose change also does not run `ansible-patterns`, so it is a routing-table
gap rather than a regression of this branch; a row `scripts/qa-patterns.bash,scripts/lib/go-tool.bash → patterns` closes it for this gate.

### F6 — Low — the spec contradicts itself about qa-changed

`SPEC-qa-patterns.md:20` ("`qa-changed` has its own per-file semgrep check and does not call
this gate") against `SPEC-qa-patterns.md:134-135` and the routing rows (it does, whole, when the
rules, fixture or tool change). The first sentence predates B33 and should go.

### F7 — Low — small things

- `run.go:700-703`: `fail` tests the stderr write's error and returns `ExitCannotRun` on both
  branches; the `if` is dead.
- B7's "bash ignored every error that was not `Syntax error`" is loose: the bash counted them
  into the partial-parse warning (`select(.type != "Syntax error")`), non-gating. The departure
  (gate on them) is real; the wording overstates the bash.
- `judgeErrors` on an error with no `path` (a rule-level or global semgrep error) reports
  `semgrep reported on "", which was not handed to it`, exit 2. Right status, misleading words;
  the bash printed `(no path)`.
- `.golangci.yml:106-108` excludes gosec G304/G703 for the whole `qa/(patterns|cmd/qa-patterns)/`
  path, tests and future files included. Same style as `qa/changed`, no `nolint`, and the
  computed paths do come from git and the command line, so it is legitimate; it is just wider
  than the four reads it is for.

## What held up

- **Every rule, every shape.** Violations planted in a `.bash`, a `.sh` (`#!/bin/sh`, `|| :`),
  extensionless with `#!/bin/bash -e`, `#!/usr/bin/env -S bash -e`, `#!/usr/bin/env sh`,
  `#!/usr/bin/env FOO=bar bash`, `#! /bin/bash` (the bash's `^#!/` regex missed this; B4),
  `#!/usr/bin/bash<TAB>`, `#!/usr/bin/bash --norc`, a BOM-prefixed `.bash`, a name with a space,
  a name with a newline, and a script under `CLAUDE/Plan/`: all 16 reported with the right rule
  and line, exit 1, `16/449 files failed`, report `{"total":449,"passed":433,"failed":16}`. New
  untracked files were in scope (P6). `#!/bin/dash`, `#!/usr/bin/zsh`, `#!/bin/bash -c '...'`,
  `#!/usr/bin/env -S bash -c "..."` and a BOM-prefixed extensionless file were not classified as
  shell, which is right (the kernel would not run the last one as bash either). Files planted
  under `roles/vendor/` and `.semgrep/` were not scanned (P7); a gitignored script was not
  scanned (P6).
- **Live tree.** 433 files, 17 partial-parse notes, identical to the journal's figures and to the
  F44 bash; a root whose own path contains `untracked/` (the clone) scans the same 433, so the
  exclusion is on the repo-relative path (P7).
- **Fail-fast, no report written in any of them:** semgrep off `PATH` → P4's exact message;
  rules file made invalid YAML → self-test failure with semgrep's parser error; one `ruleid`
  annotation removed from the fixture → self-test failure naming the rule and the lines; fixture
  deleted → exit 2 naming it; `QA_JSON_OUT` in a missing directory, and set to a directory →
  P2's message; stdout on `/dev/full` → `ERROR: cannot write to stdout: ... no space left on device`, exit 2, for both the scan and `--help` (B12). (`>&-` exits 0 only because the Go
  runtime reopens a closed fd 1 on `/dev/null`; not a defect.) `--root /tmp` is exit 2 (it fails
  on the rules before reaching git, so P6's message is not the one shown; acceptable).
- **B-rows.** B1-B6 and B8-B12 checked against the F44 bash source and hold: arguments were
  ignored; `find` scanned ignored files; `-type f` hid dangling links; `^#!/.*bash` matched
  `bashful` and missed `env sh`; `rc -ge 2` let status 1 through; the always-empty
  `SEMGREP_CANNOT_PARSE` list with its two-way assertion is gone; the unwritable report was a bare
  `set -e` exit 1. The scope method (`ls-files --cached --others --exclude-standard` minus
  `--deleted`) is equivalent in effect to qa-ansible's walk minus `--others --ignored`, differing
  only on nested repositories, which the exclusion list covers.
- **Tests are behavioural**: every P-expectation is exercised through `Env` and the `proc.Runner`
  fake with the real file system; the real-semgrep test fails rather than skips when semgrep is
  absent. `go vet`, `go test` (both packages) and `golangci-lint` with the project config: clean,
  0 issues.
- **Ratchet row.** `scripts/qa-patterns.bash` is now 7 code lines; `qa-bash-limits.bash` passes
  with the row removed (`BASH-LIMITS-OK 449 bash files`).
- **Docs.** `CLAUDE/QA.md` and `src-go/CLAUDE.md` describe what the code does; the PLAN entry's
  "433 scanned files" and "same result" are reproduced.

## Inputs used

Planting, probing and the mutation runs were scripted under `untracked/scratch/` and the files
removed after each run; nothing of theirs is committed.

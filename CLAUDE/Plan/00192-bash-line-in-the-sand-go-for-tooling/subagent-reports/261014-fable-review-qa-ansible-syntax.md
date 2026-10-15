# Adversarial review: `go-port/qa-ansible-syntax` against `integration/261014`

Reviewer: Fable 5.1 subagent. Method: `src-go/CLAUDE.md` "How a port is built"; the spec
`SPEC-qa-ansible-syntax.md` (S1-S15, D1-D7) is the oracle, not the bash. Everything below was
run with the real binary (`go build ./cmd/qa-ansible-syntax`) over the live tree of a worktree of
the port branch and over scratch git trees under `untracked/scratch/`. Every password file used
was a dummy made for the run; the repository's own was never read, printed or named.

## Verdict

**Mergeable after two fixes (F1, F2); F3 should land with it or be pinned as a follow-up.** The
core of the port holds: the population is identical to the bash's on the live tree (83/83, same
pass line), parallel results are attributed correctly and the report is byte-deterministic,
every planted static defect that the engine can see fails the gate, the prerequisite checks fail
fast with one message, and the password file is only stat'ed. The findings are at the edges:
one spec expectation (S10, colour) is not met and its test does not detect that; one check
(S6) demands files the engine does not need for a parse; and the qa-all stage text for exit 2
now describes one cause out of six.

## Findings, ranked

### F1 (HIGH) — a caller's `ANSIBLE_FORCE_COLOR` puts escape codes in stdout and the report; S10 is not met

- `src-go/qa/ansiblesyntax/run.go:161` pins `ANSIBLE_NOCOLOR=1` only. Ansible's colour decision
  applies `ANSIBLE_FORCE_COLOR` *after* `ANSIBLE_NOCOLOR`, so a caller's force wins. `proc.OS`
  appends the pinned pairs to `os.Environ()` (`internal/proc/proc.go:68`), so the caller's
  variable reaches the engine.
- Failing input: `ANSIBLE_FORCE_COLOR=1 QA_JSON_OUT=/tmp/r.json scripts/qa-ansible-syntax.bash`
  on a tree with one malformed playbook. Measured: stdout carried `[1;35m` / `[0;31m` sequences
  and the JSON report held 16 `\u001b` escapes. S10 says "a colour setting of the caller must
  not change the report text".
- The pin `TestEveryCheckPinsTheConfigAndTheWorkingDirectory` (`run_test.go:151`) only asserts
  `ANSIBLE_NOCOLOR=1` is present in the env slice; nothing asserts the engine's output is plain.
- Fix: add `ANSIBLE_FORCE_COLOR=0` to `engineEnv` (Go's `exec` keeps the last value of a
  duplicated key, so the pin overrides the caller), and prove it in
  `cmd/qa-ansible-syntax/main_test.go` with the real engine: `t.Setenv("ANSIBLE_FORCE_COLOR","1")`
  over the malformed-playbook tree and assert no `\x1b` in stdout or the report. Red first.

### F2 (MEDIUM) — every `*_PASSWORD_FILE` setting is required, but only the vault one matters to a parse

- `src-go/qa/ansiblesyntax/machine.go:14` (`passwordFileSuffix = "_PASSWORD_FILE"`) and `:54`.
  S6 states the same rule, and `TestEveryConfiguredPasswordFileIsRequired` (`run_test.go:215`)
  pins it with `BECOME_PASSWORD_FILE`.
- Failing input: `ANSIBLE_BECOME_PASSWORD_FILE=/nonexistent/bp.txt scripts/qa-ansible-syntax.bash`
  (likewise `ANSIBLE_CONNECTION_PASSWORD_FILE`). The tool exits 2: "the password file
  /nonexistent/bp.txt (BECOME_PASSWORD_FILE) does not exist ... without it every playbook would
  fail with the same message". Measured directly: `ansible-playbook --syntax-check` with that
  same environment exits **0** on a good playbook. The bash gate would have passed; the message's
  justification is false for these settings. The one the engine does load at start-up and reject
  is the vault password file (the "Invalid vault password" the journal recorded).
- Fix: narrow the suffix to `_VAULT_PASSWORD_FILE`, amend S6 to say so and why (the engine loads
  only that one before parsing), and flip the test at `run_test.go:215` to assert a missing
  become file is **not** an exit 2. The case-sensitive protected-path glob does not match the
  upper-case setting name, so the constant can be spelt in source.

### F3 (MEDIUM) — the qa-all stage text names one of six exit-2 causes

- `scripts/qa-gates.tsv:36`: `ERROR: Missing required tools (ansible-playbook). Install them and re-run.`
  Exit 2 now also means: `ansible.cfg` missing, a password file missing or empty, `ansible-config dump` unusable, zero playbooks or a tracked playbook outside the population, a check that hung
  or left a child, an unwritable report. qa-all passes the gate's stderr through first
  (`src-go/qa/all/gate.go:182`, golden `exit2-ansible-syntax.json` shows `no tool` then the tsv
  line), so the reader sees the true `ERROR:` line and then a false one telling them to install a
  tool they have.
- Failing input: a bare `git worktree add` without the password file, then `./scripts/qa-all.bash`:
  the final stderr line says to install ansible-playbook.
- Fix: reword the row to something cause-neutral (`ERROR: ansible-syntax could not run; the line above says why`).
  The realtable tests read the live `qa-gates.tsv` and `exit2-ansible-syntax.json` records the
  bash-era stderr, so the change needs that golden refreshed and the scenario pinned in
  `src-go/qa/all/testdata/deviations.json` with a numbered reason, as the method requires for any
  departure from the recorded bytes. The same row for `ansible` (line 35) has the same shape and
  may deserve the same treatment, but that is outside this port.

### F4 (LOW, latent) — a play whose first key is `name:` is not a playbook to the marker; outside `playbooks/` it is never checked

- `src-go/qa/ansiblesyntax/discover.go:17`, S7: `^\s*-\s+(hosts|import_playbook):`. A legal play
  written `- name: X` / `  hosts: localhost` has `name` first and does not match.
- Failing input: `dev/named-first-broken.yml` (name first, `hosts: localhost`, a task calling
  `no.such.module_xyz`), tracked, outside `playbooks/`. The gate exits 1 for other reasons and
  the file is absent from `results` entirely: a broken playbook passed. Under `playbooks/` the
  S9 yardstick catches it, but only once it is tracked: the same file untracked under
  `playbooks/` was silently outside both the population and the yardstick (measured: 12 results
  either way, no error), until committed, when it became the expected exit 2.
- The live tree has no such play today (scanned every published YAML for `- name:` followed by
  `hosts:`), so this is a gap carried over from the bash, not a regression. The spec should
  either state the limit or widen the marker (a list item whose first keys include `hosts:`
  within the item, or a real YAML parse of the top-level sequence). Recorded here so the next
  reader of S7 does not take "a playbook is" as complete.

### F5 (LOW) — a tasks file with `- hosts:` inside a loop list is judged a playbook and fails the gate

- Same marker. Failing input: `tasks/loop-false-positive.yml` containing `loop:` / `  - hosts: all`:
  the engine reports `'ansible.builtin.debug' is not a valid attribute for a Play` and the gate
  exits 1 for a file that is not a playbook. Carried from the bash; none on the live tree. Same
  remedy as F4 (anchor the marker to the top-level sequence).

### F6 (LOW) — the exit-2 message prints the resolved password-file path

- `machine.go:59`. The bash never printed the path. For the repository's own config the value is
  the tracked `ansible.cfg` setting, so nothing new is disclosed; with an environment override
  the message echoes whatever personal path the caller set into QA and CI logs. Consider naming
  the setting and the path only when it lies under the root, else "outside the repository".

### F7 (LOW) — drift the port left behind

- `scripts/qa-discovery.bash:22`: "The exclusion test stays in bash: qa-js and qa-ansible-syntax
  ask it once per file" — qa-ansible-syntax no longer sources that file or calls `qa_is_excluded`.
- Departures list: a symlinked `.yml` whose target is a playbook is now checked (discovery's
  `regular()` follows links; the bash's `find -type f` skipped them). Measured on a scratch tree
  (`playbooks/link.yml -> good.yml` appears in `results`). Harmless, but it is a departure D1-D7
  does not list; a dangling symlink is now exit 2 where the bash ignored it.
- Report is written `0o600` (`cmd/qa-ansible-syntax/main.go:60`); the bash wrote with the umask.
  qa-all reads it as the same user, so no effect; noted only because the spec does not say.

## What held up

- **Population.** The Go `yaml` list plus the marker gives exactly the bash's set on the live
  tree: 83 playbooks, 80 under `playbooks/imports/`, 3 elsewhere, byte-identical sorted lists
  from `find`+`grep` (bash method) and the Go report. Ignored playbooks are skipped (D1, measured
  with a `.gitignore`d `ignored/play.yml` containing a YAML error: absent from results), untracked
  but not ignored ones are checked (`playbooks/untracked-broken.yml` failed as it should), the
  `roles/vendor` play is excluded, tasks and vars files without the marker are not playbooks.
- **Planted defects the engine can see all fail**: a YAML indentation error, a stray list item,
  an unknown module, `import_tasks` of a missing file, `import_playbook` of a missing file, a
  missing role. Each is attributed to its own file with the engine's stderr. `include_tasks` of a
  missing file passes: a dynamic include is not resolved at parse time, an engine limit the bash
  shared, not a gate defect.
- **Parallelism.** With a shim engine that fails files named `odd` after a random delay, 24
  playbooks across 4 workers: 4 failures, 0 misattributed, 0 `odd` files passing; two runs gave
  byte-identical reports. Results are in path order. A shim that leaves a child holding stdout
  is exit 2 naming the playbook, within 2 s (the `WaitDelay` grace), no report written. A
  non-zero exit with empty stderr reads `ansible-playbook exited 7 with no output` (D4). The 5-minute
  timeout is covered by the fake-runner test `TestAnEngineThatDoesNotJudgeIsExitTwoNamingThePlaybook`;
  `proc.OS` kills the process group, so a hung engine's children go with it. No shared state found:
  `--syntax-check` loads no callbacks (the `play_ledger` writes are all in `v2_*` hooks) and
  gathers no facts, so the jsonfile fact cache and the ledger are untouched; Ansible's local tmp
  is per process.
- **`ansible-config dump` parsing.** The JSON is a list of `{name, origin, value}` with a trailing
  nameless `{GALAXY_SERVERS: ...}` entry; the decode to the zero value handles it. The
  environment override is honoured (the live run used `ANSIBLE_VAULT_PASSWORD_FILE` pointing at a
  dummy and passed). A relative override run from another working directory resolves against the
  root because the dump runs with `Dir: root` (measured: `./vp.txt` found at the root, a missing
  name reported as `<root>/does-not-exist.txt`). An empty file is exit 2 (S6, D2). The check
  exposes nothing about content: `os.Stat` only, size compared to zero, and the tests chmod the
  file to 0 and still pass (`TestPasswordFileMustBeAFileAndIsOnlyStatted`).
- **Prerequisites fail fast.** Missing config and a missing or empty password file are one
  `ERROR:` line, exit 2, no report, no playbook judged.
- **Yardstick.** A tracked name-first playbook under `playbooks/` is the documented exit 2 with
  the remedy text.
- **Tests.** `go test` green for `ansiblesyntax`, `cmd/qa-ansible-syntax` (real git, real
  engine; a machine without ansible fails rather than skips) and `discovery`. The tests are
  expectation-numbered and the fake runner records argv, dir and env.
- **Ratchet and wrapper.** The allowlist row is gone; the wrapper is a 5-line build-and-run
  through `lib/go-tool.bash` so exit codes reach qa-all. `CLAUDE/QA.md` rows and the
  machine-dependence table describe the new behaviour accurately; `src-go/CLAUDE.md` and the
  plan have the entry.
- **Live timing**: about 10 s for 83 playbooks against the bash's 26 s.

## Not done here

- `qa-all.bash` was not run (as instructed). golangci-lint and the Go project rules were not
  re-run on the branch; the journal records them green.
- The 5-minute per-playbook timeout was not exercised with a real hang.

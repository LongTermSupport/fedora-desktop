# qa-ansible: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md), third of the QA orchestrators after
[SPEC-qa-changed.md](SPEC-qa-changed.md) and [SPEC-qa-all.md](SPEC-qa-all.md). **Method (owner's
change): the legacy bash is not an oracle.** This document states the *intended* behaviour as
testable expectations; the bash (`scripts/qa-ansible.bash` at `d5874b54`, 614 lines) was read only
to discover the requirements. Where it was wrong, the expectation says the correct behaviour and
a note says what the bash did. There is no recording of the bash and no deviation file; the proof
is the Go tests (`src-go/qa/ansible`: unit tests per expectation, and the end-to-end scenarios of
`scenarios_test.go`), plus a run of both over the live tree at cut-over.

## 1. Role

The static Ansible gate: six checks on the Ansible source trees that `ansible-playbook --syntax-check` cannot make. Callers: `qa-all` (`scripts/qa-gates.tsv`, stage `ansible`: runs it
with `QA_JSON_OUT`, reads `.summary.{total,passed,failed}` and `.failures` from the report, exit 2
aborts the run), `qa-changed` (per-file gate, same report), and people. It starts no process itself (section 4).

## 2. Expectations

Numbers are cited by the tests.

### Command line and environment

- E1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. `-h`/`--help` prints the usage to stdout, exit 0. Any other argument is exit 2 with
  `ERROR: unknown argument '<arg>'` and the usage on stderr. A flag missing its value is exit 2.
  *(Bash ignored every argument and ran the scan.)*
- E2. `QA_JSON_OUT` is the report path; unset or empty is `/tmp/qa-ansible-results.json`.
- E3. Exit codes: 0 no violation, 1 at least one violation, 2 the run could not complete.
  stdout carries the violation lines and the summary; every exit-2 message is on stderr and starts
  `ERROR:`. A failed write to stdout is exit 2 with `ERROR: cannot write to stdout: <why>` on
  stderr; a failed write to stderr is exit 2.

### Inputs: which files, and when the run cannot complete (all exit 2, no report written)

- E4. The search directories are those of `playbooks tasks vars environment roles` that exist, in
  that order. None existing: `ERROR: none of the expected Ansible dirs (playbooks/ tasks/ vars/ environment/ roles/) exist under <root>`.
- E5. `playbooks/` must exist, and must hold at least one playbook (a file with a `- hosts:`
  line), else exit 2. *(Bash: a missing `playbooks/` printed find's error and passed with 0
  playbooks; an empty one passed too: a pass on no input.)*
- E6. A file is searched when it is a regular file named `*.yml` or `*.yaml`, hidden names
  included, found by walking each directory in name order, depth first. `roles/vendor` (the
  ansible-galaxy install target) is skipped; no other directory is. *(Bash skipped every
  directory named `vendor` at any depth in the pattern checks, so `tasks/vendor/x.yml` was never
  checked, and entered them in the playbook checks: inconsistent.)*
- E6a. **Scope: what git would publish.** Only files in `git ls-files --cached --others --exclude-standard` are judged, within the search directories; a gitignored file is never
  read, never counted and never an error (the tool lists the ignored ones with `git ls-files --others --ignored --exclude-standard` and leaves them out of the walk). The concrete case:
  a worktree's `environment/localhost/host_vars/localhost.yml` is a gitignored symlink into the
  parent checkout's private configuration, and it may dangle; it is neither scanned nor
  reported. The root must therefore be a git work tree: `git` failing is
  `ERROR: cannot list the gitignored files under <root>: <why>`, exit 2.
- E7. Within that scope, a symlink below a search directory that names a `*.yml`/`*.yaml` file
  is read as that file; a symlink to a non-YAML file is ignored like any non-YAML file; one that
  is dangling, or points at a directory or anything but a regular file, is exit 2 whatever it is
  called, with `ERROR: the symlink <path> ...` naming it. A symlinked search directory is
  followed. *(Bash skipped symlinks without a word.)*
- E8. A file that cannot be read is exit 2 naming it, in every check. *(Bash: an unreadable
  playbook was treated as "not a playbook".)*
- E9. A line ends at `\n`; a last line without one counts; `\r` is part of the line. A NUL byte is
  an ordinary byte: the file is matched as text. *(Bash: grep treated such a file as binary and
  a violation in it was silently not reported.)*
- E10. The report path cannot be written: `ERROR: cannot write the report <path>: <why>`, exit 2.

### The checks, in run order; violations print as found, each counted once

- E11. **Fail-fast** (case-insensitive) over the search set: `failed_when:` followed by
  whitespace and `false|no|off`; `ignore_errors:` or `ignore_unreachable:` followed by
  `true|yes|on` or by a template (`{{`); in every case the value may be wrapped in a double or
  a single quote (`"true"`, `'yes'`, `failed_when: "false"`, `'{{ maybe }}'`: Ansible reads them
  as the same booleans and the same always-false expression); each value word ends at a word
  boundary (`not`, `online` are not matches). A matching line is skipped when its text contains
  `FAIL-FAST-OK` in any case, or when nothing matches once a comment is cut (the first `#` that
  starts the text or follows a space or tab). Output `  ERROR (fail-fast): <path>:<n>:<text>`.
  *(Bash looked for `FAIL-FAST-OK` in the whole `path:n:text` string, so a file named
  `fail-fast-ok.yml` exempted every violation in it, and split that string at colons, so a path
  containing a colon cut the comment at the wrong place.)*
- E12. **Hygiene**: every playbook under `playbooks/` (vendor and `optional/archived` included)
  starts with exactly the shebang `#!/usr/bin/env -S bash -c 'p=$(realpath "$0") && exec "${p%/playbooks/*}/run.bash" "$p" "$@"'` (`scripts/make-playbooks-executable.bash` writes it)
  and has an execute bit. Output `  ERROR (hygiene): <path> — missing shebang`, `— not executable`
  or `— missing shebang + not executable`. The playbook count is reported in the summary.
- E13. **Self-default**: a line `<name>: "{{ <name> | default…` (same identifier both sides,
  optional quote, any spacing, `default` ending at a word boundary, the line starting with the
  name after indentation) is `  ERROR (self-ref var): <path>:<n>:<text>`. No annotation or comment
  exemption.
- E14. **Scope and guard**, per playbook (not under `/optional/archived/` in its absolute path):
  more than one `- hosts:` line: `file contains <N> separate '- hosts:' plays; this gate cannot safely vouch for a multi-play file. Split it (one play per file, matching every other playbook in the repo), then give each its own vars.scope (+ guard if needed).` Otherwise exactly one
  `scope:` entry (four-space indent, non-empty value, in the two-space-indented `vars:` block;
  trailing `# comment` and `\r` cut) whose value is `general`, `gnome` or `server`: else
  `missing vars.scope (need exactly one of general|gnome|server)`, `multiple vars.scope entries declared (exactly one required)`, or `invalid vars.scope value: <v> (must be exactly one of general|gnome|server)`. `gnome` and `server` need the canonical guard as the first two tasks
  (`    - name:` lines of the two-space `tasks:` block): task 1 named `Scope guard — assert provisioning_profile is recognised`, task 2 named `Scope guard — end play if provisioning_profile does not match declared scope` with `ansible.builtin.meta: end_play` and
  `when: (scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')` (comments cut), else `scope=<s> requires the 2-task canonical guard (assert + meta:end_play) as its first two tasks; missing or incorrect`. `general`
  must not have task 1 named as either guard task: `unnecessary scope guard on a general-scope play (guard never fires for general; remove it)`. Output `  ERROR (scope): <path> — <message>`.
  The line scan's rules are tested rule by rule (`playbooks_test.go`).
- E15. **Deprecated fact variables**: `ansible_<name>` as a whole word for the fact names listed
  in `facts.go`, and the unprefixed `getent_passwd|group|shadow|hosts|services|protocols|ahostsv4| ahostsv6` as a whole word unless directly preceded by `ansible_facts['`, `ansible_facts["` or
  `ansible_facts.`. A line that is only a comment (`#` after optional whitespace) is skipped; a
  trailing comment on a live line does not exempt it. Output `  ERROR (deprecated fact var): <path>:<n>:<text>` and `    Use ansible_facts['<name>'] — top-level injection is removed in ansible-core 2.24.`
- E16. **Guessed uid**: `/run/user/` followed by a digit, or by `{{` with no `getent_passwd` later
  on the line; comment-only lines skipped. Output `  ERROR (guessed uid in a runtime path): <path>:<n>:<text>` and `    Resolve it: ansible.builtin.getent + ansible_facts['getent_passwd'][user_login][1].`

### Output

- E17. Ordering is deterministic: checks in the order above, files by name. *(Bash printed in
  `grep`/`find` directory order, which differs between file systems.)*
- E18. No violation: `✓ ansible: fail-fast patterns OK; no self-referential vars; no deprecated ansible_<fact> vars; scope+guard declarations OK; <P> playbook(s) have correct shebang+exec (scoped to playbooks/ tasks/ vars/ environment/ roles/ — qa-ansible-syntax counts repo-wide and will be higher)`, exit 0. Otherwise `✗ ansible: <E> violation(s) — <ff> fail-fast, <hy> hygiene, <sr> self-ref var, <sc> scope/guard, <fv> deprecated fact var` (`fv` counts E15 and
  E16), `  Hygiene fixer: ./scripts/make-playbooks-executable.bash`, `  Details: jq '.failures[]' <report path>`, exit 1.
- E19. The report is `{type:"ansible", status, summary:{total,passed,failed}, failures:[…], results:[], checks:{fail_fast,hygiene,self_ref,scope,fact_vars}}`, two-space JSON. `total` is
  playbooks + 1, `failed` the violation count, `passed` is `total - failed` but never below 0.
  *(Bash let it go negative.)* A failure is `{file, type:"ansible", status:"fail", error}`; for
  hygiene and scope `file` is the path and `error` is `hygiene: <why>` / `scope: <why>`; for the
  others `file` is `<path>:<n>:<text>` and `error` is the check's fixed message (as the bash's).
  *(Bash re-split its own strings at ` (`, so a scope value containing ` (` was truncated.)* The
  `checks` lists carry `<path> (<why>)` for hygiene and scope and `<path>:<n>:<text>` for the rest.

## 3. Where the Go departs from the bash

| #   | bash behaviour                                                            | Go                             | expectation |
| --- | ------------------------------------------------------------------------- | ------------------------------ | ----------- |
| B1  | directory-order output                                                    | name order                     | E17         |
| B2  | arguments ignored                                                         | usage / exit 2                 | E1          |
| B3  | unreadable file read as "not a playbook"; grep rc 2 exit text differs     | exit 2 naming the file         | E8          |
| B4  | missing `playbooks/` passes with find's stderr noise                      | exit 2                         | E5          |
| B5  | NUL-byte file: violation silently dropped                                 | matched as text                | E9          |
| B6  | unwritable report: shell error, exit 1                                    | message, exit 2                | E10         |
| B7  | `-x` asks the kernel for the calling user                                 | any execute bit set            | E12         |
| B8  | `FAIL-FAST-OK` in the path exempts a file; colon in a path misparses text | judged on the line text only   | E11         |
| B9  | `vendor` skipped at any depth                                             | only `roles/vendor`            | E6          |
| B10 | symlinks skipped silently                                                 | followed to files, else exit 2 | E7          |
| B11 | `passed` could be negative                                                | clamped at 0                   | E19         |
| B12 | JSON hygiene/scope split on ` (`                                          | structured                     | E19         |
| B13 | empty `playbooks/` passes with 0 playbooks                                | exit 2                         | E5          |
| B14 | quoted `"true"`, `'yes'`, `"false"`, `'{{ x }}'` pass the gate            | caught                         | E11         |
| B15 | gitignored files scanned (and a dangling ignored link would be exit 2)    | never read                     | E6a         |
| B16 | symlink to a directory, or dangling and not `*.yml`, skipped silently     | exit 2 naming it               | E7          |
| B17 | failed stdout write: exit 2, no message                                   | `ERROR:` line on stderr        | E3          |

Accepted limits, same as the bash: `#` inside a quoted string is treated as a comment start for
E11; `\b` and case folding are ASCII; JSON of text that is not valid UTF-8 is replaced with
U+FFFD.

## 4. Go design and cut-over

The package starts no process itself: the one it needs, `git ls-files` for E6a, is injected as
`Env.Ignored` and implemented in `cmd/qa-ansible` through the shared `qa/internal/proc` seam.
`src-go/qa/ansible` (`walk.go` file sets, `failfast.go`, `facts.go`, `playbooks.go` hygiene and
scope, `report.go`, `run.go`), thin `src-go/qa/cmd/qa-ansible`. `scripts/qa-ansible.bash` is a build-and-run wrapper over
`lib/go-tool.bash`; its allowlist row is removed. `scripts/test-qa-ansible-failfast.bash`, which
read the pattern out of the old script text, drives the built tool over fixture trees (the same
18 cases, now 24 with the quoted ones) and keeps its gate row.

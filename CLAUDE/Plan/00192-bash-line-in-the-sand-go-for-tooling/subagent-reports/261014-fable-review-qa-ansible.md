# Adversarial review: `go-port/qa-ansible` (Task 3.3, qa-ansible ported to Go)

Branch reviewed: `origin/go-port/qa-ansible` at `45263127`, diffed against `origin/F44`
(`d5874b54`). Spec: [SPEC-qa-ansible.md](../SPEC-qa-ansible.md), expectations E1-E19,
departures B1-B13. Method: read the spec, the Go and the F44 bash side by side; build the real
binary; run it over planted fixture trees and over the live tree (in an agent worktree, next to
the F44 bash run from a shim root); run the Go tests, `qa-go.bash`, `qa-bash-limits.bash` and
`test-qa-ansible-failfast.bash`. `qa-all` was not run, as instructed.

## Verdict: fix first

The port is faithful where the spec speaks, the departures are genuine fixes, and the live tree
gives identical stdout and report from the bash and the Go. Two things stop a merge as it stands:
the fail-fast check — the repo's #1 rule — is evaded by quoting the value (F1: the spec's own
"optionally quoted" is only half implemented, and the quoted-boolean form is in neither spec nor
code), and a symlinked directory in scope is skipped without a word (F2), which the spec's E7
says is an exit 2. F3 is a decision the spec should record; F4-F5 are cheap.

## Findings, ranked

### F1 — High — a quoted value walks through the fail-fast gate

`src-go/qa/ansible/failfast.go:9-20`. The pattern requires the boolean to follow the key's
whitespace directly, and the templated form allows only a double quote
(`templated = "?[{][{]`). Ansible's `ignore_errors`/`ignore_unreachable` are `bool` fields that
accept the string forms (`"true"`, `'yes'`, `'on'`), and `failed_when: "false"` is the Jinja
expression `false`, which never fails. Planted fixture `tasks/q.yml`, every line a live
error-hider, exit 0 with the green tick:

```yaml
- name: a
  ignore_errors: "true"
- name: b
  ignore_errors: 'yes'
- name: c
  failed_when: "false"
- name: d
  ignore_errors: '{{ maybe }}'
- name: e
  ignore_unreachable: 'on'
- name: f
  failed_when: [false]
- name: g
  ignore_errors: !!bool true
```

Line `d` contradicts E11 as written ("or by a template (`{{`, optionally quoted)"): a
single-quoted template is quoted, and `test-qa-ansible-failfast.bash` / `failfast_test.go` only
try the double-quoted one. Lines `a`, `b`, `c`, `e` are a gap the spec did not state; the bash
had it too, but the method on this port is that the bash is not the oracle, and the spec is the
place that should have caught it. `f` and `g` are rarer spellings, listed for completeness.

Fix: allow an optional `["']` before the boolean alternatives and `['"]?` (not just `"?`) before
`[{][{]`; add the four quoted cases to `TestFailFastPatternClassifies`, one scenario to
`scenarios_test.go`, and the same cases to `scripts/test-qa-ansible-failfast.bash` (the suite
has 18 cases, all unquoted). Re-run over the live tree: a quoted boolean that is currently
green would become a violation, so the first run after the fix is also the sweep.

### F2 — Medium — a symlinked directory, or a dangling link not named `*.yml`, is skipped silently

`src-go/qa/ansible/walk.go:70-73`: `followLink` returns nil for any symlink whose *name* is not
`*.yml`/`*.yaml`, before looking at what it points to. E7 says "one that is dangling, or points
at anything but a regular file, is exit 2", and the package doc on `walk.go:31-33` says the same,
but both are only true for links whose name ends in `.yml`. Planted fixture, exit 0:

```
tasks/shared  -> ../elsewhere      (a directory holding bad.yml: "  failed_when: false")
tasks/broken  -> nowhere           (dangling)
```

The violation in `elsewhere/bad.yml` is never reported and nothing says the subtree was not
read. The bash did the same (`grep -r` without `-R`), so this is parity, not a regression — but
it is exactly the "skip and continue" the Fail Fast rule prohibits, and `walk_test.go:63-85`
tests a dangling `.yml` link and a `dir.yml` link only, so the gap is untested either way.
Either follow a directory link (with a visited set against loops) or exit 2 naming it; then
the spec sentence becomes true as written.

### F3 — Medium — B10 meets the one symlink the live tree actually has

In every agent worktree of this repository, `environment/localhost/host_vars/localhost.yml` is
a symlink into the main checkout's gitignored, private host configuration (observed in the
review worktree: the link is present and the target exists). That file is the only symlink in
scope on the live tree, so B10 ("followed to files, else exit 2") has two concrete consequences
the spec does not mention:

1. A worktree whose parent checkout has no `localhost.yml` (fresh clone, then a worktree) now
   makes the whole gate exit 2 — and `qa-all` abort — over an untracked private file.
   Fixture reproduction: `environment/localhost/host_vars/private.yml -> ../../../../../../nope.yml`
   gives `ERROR: the symlink environment/localhost/host_vars/private.yml cannot be read: ...`,
   exit 2. The bash skipped it.
2. The gate scans a private, untracked file, and a hit there prints that file's line text into
   stdout and `/tmp/qa-ansible-results.json`. (The bash already did this in the main checkout,
   where the file is regular; the worktree was the one place it did not.)

The journal's "same stdout, stderr and report on the live tree" holds in the main checkout and
held in the review worktree only because the private file is clean today. This is a decision,
not a defect in the Go: either the gate is about tracked Ansible source (skip gitignored paths,
or scope to `git ls-files`), or it is about the working tree as found — but the spec should say
which, with this instance named, because it is the case the rule will meet first.

### F4 — Low — docs drift left behind the cut-over

- `CLAUDE/QA.md:153`: `test-qa-ansible-failfast.bash` tests "the fail-fast directive regex in
  `qa-ansible.bash`, read from it rather than copied" — no longer true; it builds and drives the
  Go tool.
- `CLAUDE/QA.md:65`: "Fail-fast grep" and the scope column still describe the bash's mechanism.
- `CLAUDE/AnsibleStyle.md:178,346`: "Check 5", "Check 4" of `scripts/qa-ansible.bash` — still
  meaningful, but the numbering now lives only in the spec's E-rows.

`src-go/CLAUDE.md` was updated; `CLAUDE/QA.md` was not.

### F5 — Low — a stdout write failure is exit 2 with no message

`src-go/qa/ansible/run.go:126-133`: when the emitter recorded a failed write, `Main` returns 2
and says nothing. E3 promises "every exit-2 message is on stderr and starts `ERROR:`". Probe:
`qa-ansible --root <clean tree> > /dev/full` exits 2 with empty stderr (the report is still
written). One `ERROR: cannot write to stdout: <why>` line on stderr closes it. (A closed fd 1
is not a probe for this: the Go runtime reopens it on `/dev/null`, so `>&-` exits 0 correctly.)

### F6 — Info — B7's "any execute bit" is root's view of `-x`

`playbooks.go:75` treats a file with any of the three execute bits as executable. For the
calling user, `-x` is false on a `0700` file owned by someone else; the Go says executable.
Git tracks only the owner bit, so on a checkout the two agree. Acceptable; recording it so the
B-row is read as "root's answer, not the caller's" rather than as a pure fix.

### F7 — Info — four passes over every file

`grepLines` is called once per pattern check (`run.go:224,242,267`), so each YAML file is read
four times plus once more if it is a playbook. The live run is 0.36 s including the Go build
check, so this costs nothing today; noted so a later "speed it up" does not read as a bug.

## What held up

- **Live tree, worktree checkout**: F44 bash (run from a shim root over symlinked dirs) and the
  Go binary both exit 0 with the identical tick line (81 playbooks) and byte-identical sorted
  reports (`total 82, passed 82, failed 0`). The wrapper runs in 0.36 s.
- **Every B-row checked against spec and code, with a fixture where one applied**: B1 name
  order (`TestScanOrdersFilesByName`, and `tasks/.hidden` sorted first); B2 usage/exit 2
  (`--typo`, `extra`, `--root=DIR` all refused with usage on stderr); B3 unreadable file
  (`failingFS` in both the pattern and the playbook path); B4 and B13 (`playbooks` missing, a
  regular file, or holding no play: exit 2 with a clear line, no report); B5 NUL byte matched as
  text; B6 report path a directory / under a missing directory: `ERROR: cannot write the report ...`, exit 2; B8 `tasks/FAIL-FAST-OK.yaml` and `tasks/a:b.yml` judged on the line; B9
  `tasks/vendor/x.yml` checked, `roles/vendor` skipped, `playbooks/vendor/v.yml` in hygiene and
  scope; B11 `passed` clamped; B12 structured `hygiene:`/`scope:` entries with ` (` in the value
  surviving. B10 holds for `*.yml`-named links (F2 is the rest). B7 is F6.
- **Nothing the bash caught is dropped**: the fail-fast spellings (all 18 suite cases), comment
  cut, same-line annotation in any case, self-default with the backreference emulated correctly
  (`foo`/`foobar` both directions), the three fact-var lookbehinds (`ansible_facts['x']`,
  `["x"]`, `.x` exempt; `getent_passwd[u]`, `hostvars[h].ansible_env`, `ansible_user_id`
  flagged), both `/run/user/` forms, comment-only lines skipped for facts and uid but not for
  fail-fast's trailing-comment case, the scope line scan rule for rule (quoted `"general"`,
  trailing space, guard in `pre_tasks` all fail exactly as the awk did; `name:` before `hosts:`
  passes), `optional/archived` exempt from scope but not hygiene, hidden files and directories
  included, `.yaml` included, non-YAML ignored, CRLF kept on the line.
- **Fail-fast of the tool itself**: exit codes 0/1/2 as specified; every exit-2 path writes no
  report (checked: no file left behind); unreadable input is a named error; `/dev/full` on
  stdout is exit 2 (F5 is only the missing message).
- **Routing**: the wrapper is four statements over `scripts/lib/go-tool.bash`;
  `scripts/qa-gates.tsv:35` (stage `ansible`) and `:116` (`failfast-pattern-tests`) are
  unchanged and still point at the two scripts; `qa-changed` runs it through
  `gateWithReport` (`src-go/qa/changed/checks.go:122-131`) with `QA_JSON_OUT` set, which the
  tool honours; CI reaches it only through `qa-all`, so no workflow edit was needed.
- **The suite is still a test**: `test-qa-ansible-failfast.bash` builds the shipped command and
  drives it over one-line fixture trees, asserting exit 1/0 and exiting 2 if the tool cannot
  run — a stronger check than reading the regex out of the script text. 18/18 pass.
- **Tests assert behaviour**: `scenarios_test.go` compares the exact violation lines in order
  over real trees; `playbooks_test.go` tests the line scan rule by rule with inputs the awk
  would have met; `report_test.go` pins the JSON byte for byte on a pass and the structured
  entries on a fail; `run_test.go` covers every exit-2 route with a fake FS or a real one.
  95.2% statement coverage on the package; the command's 50% is `main` and `writeReport`, which
  the fixture run exercised for real (F1's root cause is a missing case, not a weak test).
- **QA gates**: `scripts/qa-go.bash` GO-OK (golangci-lint clean, project rules clean, module at
  94.3%); `scripts/qa-bash-limits.bash` BASH-LIMITS-OK with the `scripts/qa-ansible.bash` row
  removed legitimately (the file is now within limits; nothing was folded into another script).
  The `.golangci.yml` additions (gosec on the package's tests, tagliatelle on `report.go`'s
  snake_case wire format) mirror the existing `qa/changed` entries and are scoped to the files
  they name.
- **Claims**: `src-go/CLAUDE.md` and the PLAN paragraph are accurate. The journal's "same
  output on the live tree" is true as measured (see F3 for where it would stop being so).

## Suggested order

1. F1 — pattern, tests, suite; re-run on the live tree (sweep). 2. F2 — follow or refuse
   directory links; test both. 3. F3 — a sentence in the spec, and a decision on gitignored paths.
2. F4 — two lines in `CLAUDE/QA.md`. 5. F5 — one stderr line. Then the `qa-reviewer` pass the
   plan already requires before merge.

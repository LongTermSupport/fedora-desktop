# QA Review (round 3, confirmation) — branch `worktree-ccy-reboot-restore` vs `F44` (Plan 00123)

Surface established with the merge base (`git merge-base F44 HEAD` = `d8574e31`): 5 commits,
**26 files** over `F44...HEAD`. The round-2 fixes are commit `27ee70f0` (CCY 3.59.1). Worktree:
`/workspace/untracked/worktrees/worktree-ccy-reboot-restore`.

**Verdict**: FIX-BEFORE-MERGE — 11 of the 13 round-2 findings are genuinely fixed, 2 are
partially fixed, and 4 new findings follow from the round-2 fixes. Nothing here breaks another
user, loses data or leaks anything; the top one is a fail-fast contract violation that survives
in the boot service by the route round 2 named and did not close.

---

## Part 1 — NEW-1 .. NEW-13, re-checked against the code

| #      | Round-2 finding                                             | Verdict       | Evidence                                                                                                                                                                                                                                                                                                                                              |
| ------ | ----------------------------------------------------------- | ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| NEW-1  | off path templated a uid only the on path resolved          | **FIXED**     | `when:` gone from both tasks (`play-claude-yolo.yml:490`, `:499`). YAML parse: `Assert The Session User Exists` → `when=None`; the handler (`:771-778`) and the disable task (`:543`) now always have the fact. Still **61 tasks / 1 handler / 1 pre_task**, one play, same top-level keys. `--syntax-check` rc=0.                                       |
| NEW-2  | non-numeric `boot_time` aborts the boot service mid-loop    | **PARTIAL**   | The record field is guarded (`ccy-sessions-restore:324`). The environment side that round 2's own fix note named is **not**. Measured against the real script: `CCY_RESTORE_MAX_AGE_DAYS=notanumber` → `line 328: notanumber: unbound variable`, rc=1, no verdict, no summary, no `last-run`. See **F1**.                                              |
| NEW-3  | `cmd_restore_status` truncated its own report               | **FIXED**     | Both halves probed against the real script. Registry unreadable → whole report printed, `Last restore run` still emitted, rc=1. Failure isolated to `retired/` only → `Not restored, with the reason: COULD NOT BE READ`, report continues, rc=1. Errexit fires at the call site rather than `exit $?`, with the same status.                            |
| NEW-4  | `ccy-sessions` untestable; three fixes verified by reading  | **FIXED**     | `${CCY_LIB:-…}`/`${CCY_LAUNCHER:-…}` at `ccy-sessions:42-43`; `scripts/test-ccy-sessions-status.bash` (42 cases, mode 100755) wired at `qa-all.bash:330-344` with a `✓ ccy-sessions-status: passed: N` line; `CLAUDE/QA.md` row added. Coverage caveats in **F3** and **F4**.                                                                            |
| NEW-5  | D8's table listed four answers                              | **PARTIAL**   | `DESIGN-failure-modes.md:245-256` now has six rows plus a "Six answers, not four" note, and `PLAN.md:43,149` agree. The code's own contract comment does not. See **F2**.                                                                                                                                                                              |
| NEW-6  | `docs/ccy.md` miscounted its own states                     | **FIXED**     | `docs/ccy.md:258` — "Two of those — `installed-state-unknown` and `enabled-linger-unknown`".                                                                                                                                                                                                                                                           |
| NEW-7  | docs named a retirement reason the code never writes        | **FIXED**     | `docs/ccy.md:280` is now `failed-validation`, matching `ccy-sessions-restore:279`. All six documented reasons check out against the six `retire`/quarantine call sites (`:279,303,329,335,344,357,362`).                                                                                                                                               |
| NEW-8  | `stale` documentation did not move with the behaviour       | **FIXED**     | Header `ccy-sessions-restore:51-58`, `--help` `:85-87` ("its BOOT was more than this many days before the current one … measured boot-to-boot"), `docs/ccy.md:279`.                                                                                                                                                                                    |
| NEW-9  | `ccy_registry_count` had no caller                          | **FIXED**     | Function gone. Repo-wide scan over `files/ scripts/ playbooks/ docs/ CLAUDE/` returns only the round-2 report's own prose. No dangling reference.                                                                                                                                                                                                      |
| NEW-10 | the collect error was printed twice                         | **FIXED**     | `ccy_registry_collect` now owns both messages (`session-registry.bash:499`, `:506`) and `ccy_registry_list` adds none. Probed: exactly one `ERROR:` line per failed listing.                                                                                                                                                                           |
| NEW-11 | `ccy_registry_collect` can leak its temp file               | **PARTIAL**   | No trap was added (`session-registry.bash:488-514`). In practice every caller invokes it as `if ! ccy_registry_collect …`, which disables errexit inside the function, so the `rm -f` is reached even if `mapfile` fails — only a signal between `mktemp` and `rm` leaks. The code is fine; the changelog sentence is not. See **N4**.                   |
| NEW-12 | the bare-read scan is narrower than the hang it guards      | **FIXED**     | Addressed the way it should be — `test-ccy-session-registry.bash:693-697` now states the uncovered shape ("an echoed prompt followed by `read -r some_var`") instead of implying full coverage.                                                                                                                                                        |
| NEW-13 | tmux's stderr merged into a value used as a filename        | **FIXED**     | `tmux-session.bash:366` — `name=$(tmux display-message -p '#S')`, no `2>&1`; the message is now "(its own error is above)". Both callers still distinguish 0/1/2: `claude-yolo:3250` captures the rc, `:3254` exits 1 on 2, `:3259` proceeds only on 0; `ccy_tmux_banner:377,381` maps 1→0 and 2→1. No third caller exists.                              |

---

## Part 2 — new findings

### Should fix

#### F1. The boot service still dies mid-loop on a mistyped `CCY_RESTORE_MAX_AGE_DAYS`

`files/home/.local/bin/ccy-sessions-restore:328`

```bash
if [[ "$record_age_days" -gt "$CCY_RESTORE_MAX_AGE_DAYS" ]]; then
```

`[[ … -gt … ]]` evaluates **both** sides as arithmetic, so a non-numeric value on the right is
parsed as a variable name and dies under `set -u`, exactly as the record field did. Measured
against the real script with a scratch registry holding one restorable record:

```
ccy-sessions-restore: boot <uuid>
DRY RUN — nothing will be started, consumed or retired.
…/ccy-sessions-restore: line 328: notanumber: unbound variable
RC=1
```

No verdict for that record, nothing after it processed, no summary line, no `last-run` note —
the same contradiction of `:391-398` ("process everything, name every failure, and still fail")
that NEW-2 was raised for. Round 2's fix note said so in as many words ("`CCY_RESTORE_MAX_AGE_DAYS`
has the same shape from the environment side"); the record half was fixed and this half was not.
This is the operator-facing knob, documented in `--help`, so `=7d` or `=two weeks` is the likely
way in — into a unit that runs at boot with nobody watching.

**Fix**: validate it where the default is taken (`:58`), before the loop —
`[[ "$CCY_RESTORE_MAX_AGE_DAYS" =~ ^[0-9]+$ ]] || { print_error …; exit 1; }`. Failing before any
record is touched is right here: it is a configuration error, not a per-record one.
`CCY_RESTORE_EVIDENCE_DAYS` has the same shape but is safe — `find -mtime "+$x"` errors and
`prune_evidence:239-243` catches it.

#### F2. `restore_installation_state`'s own contract still says four answers; it emits six

`files/home/.local/bin/ccy-sessions:112-113`

```bash
# restore_installation_state — one of not-installed | installed-not-enabled |
# enabled-no-linger | enabled, on stdout. Four answers, because each needs a different fix.
```

The function below it returns six, two of which (`installed-state-unknown`,
`enabled-linger-unknown`) are the "could not tell" answers this whole design exists to keep
separate. NEW-5 was fixed in `DESIGN-failure-modes.md`, `PLAN.md` and `docs/ccy.md` and missed the
one copy a maintainer editing this function actually reads. A scan across the branch's scripts,
docs and plan files for an answer-count claim returns this line as the **only** remaining "four".

Same shape as the repo's recurring "a lesson written down beside the thing it fixed, never
generalised" defect: three instances corrected, the fourth left live in the code itself.

#### F3. The new suite never drives `list_with_reasons`'s "could not be read" branch

`scripts/test-ccy-sessions-status.bash:206-220`

The unreadable-registry case does `reset_all` (which `rm -rf "$STATE"`) then `install_unit` and
one `write_record`, so `restore/attempted`, `restore/retired` and `restore/malformed` **do not
exist**. `ccy_registry_collect` returns 0 at `[[ -d "$dir" ]] || return 0` before it ever reaches
the `mktemp` the case breaks, so all three `list_with_reasons` calls short-circuit and only the
`sessions_dir` branch fails. Probed both ways against the real script with the same broken
`TMPDIR`:

- `retired/` absent (what the suite sets up) — no `Not restored, with the reason: COULD NOT BE
  READ` line at all;
- `retired/` present — the line appears, the rest of the report still prints, rc=1.

So the behaviour is right, but the half of the NEW-3 fix that lives in `list_with_reasons`
(`ccy-sessions:161-167`) is **still verified by reading only** — inside the suite that was added
because reading was not enough (NEW-4). This is the plan's own recurring defect class: a case
that guards one member of the population and reads the pass as covering all of it.

**Fix**: `mkdir -p "$STATE/ccy/restore/retired"` in that case and assert both the section's
COULD-NOT-BE-READ line and that the report continues past it.

#### F4. The suite's header claims the reboot audit; no case exercises it

`scripts/test-ccy-sessions-status.bash:2` ("Unit-test `ccy-sessions restore-status` **and the
reboot audit**"), stub at `:81`.

The tmux stub is `#!/bin/sh` + `exit 0` with no output, so `ccy_tmux_list` always returns an empty
listing and every `reboot`/`notify` case runs the `total -eq 0` path. Measured:

```
Sessions a reboot would interrupt:
  (no sessions are running)

Dry run: nothing was signalled and nothing was rebooted.
```

Never exercised: the per-session table (`ready` / `NO DAEMON CLI`, `ccy-sessions:348-353`), the
`missing > 0` refusal at `:360-365` — the partial-warning refusal that is the only reason
`audit_sessions` returns non-zero — and the listing-failure branch at `:338-342`. Those are the
behaviours round 1 rewrote `audit_sessions` for.

`scripts/test-ccy-session-restore.bash:80-100` already has a stateful tmux stub that lists back
what it created; reusing it here costs almost nothing. Otherwise narrow the header to "the blocked
reboot seam", which is what `CLAUDE/QA.md:60` already claims and what the cases actually cover.

### Nits

#### N1. The rewrite left `ccy_registry_list`'s old header stranded on a variable

`files/var/local/claude-yolo/lib/session-registry.bash:464-473`

Lines 464-470 are the pre-rewrite `ccy_registry_list` header. They now sit directly above
`CCY_REGISTRY_RECORDS=()` with **no blank line**, so the array's own two-line comment reads as a
continuation of a header for a function defined 52 lines below — which has its own, correct header
at `:516-518`. Delete the stale block.

It also claims the listing is "sorted". Measured: eight records named `zulu, mike, alpha, delta,
charlie, bravo, echo1, foxtrot` come back in that order, not lexical — `find -print0` is readdir
order and nothing sorts it. (The claim was wrong before the rewrite too; it is now wrong in a
comment that documents nothing.) If order matters to `restore-status`'s listing, sort inside
`ccy_registry_collect`; otherwise drop the word with the block.

#### N2. `ccy_registry_list` has no production caller

`files/var/local/claude-yolo/lib/session-registry.bash:519-523`. Its only callers are
`scripts/test-ccy-session-registry.bash:186` and `:510`. It was kept "for a caller that wants a
stream rather than the array" — there is no such caller, which is the exact reason
`ccy_registry_count` was deleted **six lines away in the same commit** (NEW-9). A test that
exercises a function nothing uses vouches for nothing in production.

Its docstring also omits that it now clobbers the global `CCY_REGISTRY_RECORDS` as a side effect —
harmless today only because both test call sites are process substitutions.

Either delete it and retarget those two tests at `ccy_registry_collect`, or state the caller it is
being kept for.

#### N3. The `enabled` assertion would also pass for `enabled-no-linger`

`scripts/test-ccy-sessions-status.bash:181` —
`said 'Session restore on this machine: enabled'` is a `grep -F` substring test, and
`Session restore on this machine: enabled-no-linger` and `…: enabled-linger-unknown` both contain
it. It passes for the right reason today (the case sets `enabled`/`yes`), but it would not catch a
regression into either neighbour — the two states this suite exists to keep apart. Pair it with
`check "… and not a variant" "no" "$(said 'enabled-')"`, or match the whole line.

#### N4. The changelog claims a cleanup guarantee the code does not make

`docs/ccy-changelog.md:25` — "The temp file it reads through is cleaned up on every path." There is
no trap (`session-registry.bash:488-514`). In practice nothing leaks, because every caller uses
`if ! ccy_registry_collect …`, which disables errexit inside the function so the trailing `rm -f`
is reached even if `mapfile` fails. "On every path" is still stronger than the code: a signal
between `mktemp` and `rm` leaks. Either add the trap or soften the sentence.

---

## Checked and clean

- **`ccy_registry_collect` as the primitive, and every caller.** Production callers are
  `ccy-sessions:163`, `:223` and `ccy-sessions-restore:257`, all `if ! ccy_registry_collect …`.
  The launcher calls neither. Nothing calls the removed `ccy_registry_count`. Failure paths
  probed: absent directory → rc=0 with an empty array before `mktemp`; `mktemp` failure → named
  error, rc=1, no file; a broken listing → named error, temp removed, rc=1.
- **`ccy_tmux_current_session`'s three statuses.** Both callers re-read and correct; `2>&1`
  removal cannot alter a status, and the success-path value is now the bare session name that
  `claude-yolo:3290` writes into the record filename.
- **`cmd_restore_status` under `set -e`.** Probed three ways (registry-only failure, registry +
  section failure, section-only failure): the full report prints in every case and the command
  exits 1. `CCY_STATUS_INCOMPLETE` is a plain global set from both sites; no subshell hides it,
  because `list_with_reasons` is called directly rather than in a pipeline or substitution.
- **The ungated `getent`/`assert`.** Safe on the restore-ON path (it already ran there) and now
  correct on OFF. Nothing between the pre_task and task 39 reads `getent_passwd`, and tasks 40/42
  plus the handler are the only consumers. The play already fails without a resolvable
  `user_login` (every `copy`/`file` task sets `owner: {{ user_login }}`), so the assert moves an
  existing failure earlier and gives it a message. `ccy_restore_sessions` still defaults to
  `false` (`vars/container-defaults.yml:29`).
- **The `restore-status` test suite's registry-unreadable case.** Genuinely non-vacuous: the
  broken `TMPDIR` makes `mktemp` fail for a directory that exists and holds a record, and the
  case asserts the count is *not* 0, that the report continues (`Last restore run` is printed
  after the failure) and that rc=1. The comment at `:97-101` states why `chmod 000` was useless
  as root. Its coverage gap is F3, not vacuity.
- **Version and container integrity.** `CCY_VERSION` 3.59.0 → **3.59.1** with a rewritten one-line
  comment; changelog section present and matching the code. `REQUIRED_CONTAINER_VERSION="2.37"`
  unchanged, and neither the Dockerfile nor `entrypoint.sh` is in the branch surface, so the
  LABEL does not move. `CCY_HASH` derives from `lib/*.bash`, so the library edits are covered.
- **Fail-fast.** Round-2's added lines contain no `failed_when:`, `ignore_errors:`, `|| true`,
  bare `2>/dev/null` or `set +e` (the only matches are the round-2 report's own prose).
  `qa-ansible.bash` green.
- **Ansible 2.19 traps.** No `: -x` pattern in any added unquoted `name:`. `--syntax-check` rc=0.
  No self-defaulting variable introduced.
- **File modes.** `test-ccy-sessions-status.bash` 100755, both front-ends 100755, the play 100755
  with the repo shebang, the unit file 100644.
- **Public-repo safety.** Scanned all 5,660 added lines of `F44...HEAD` and all five commit
  messages for emails, IPv4 literals and literal `/home/<user>` paths: no hits beyond
  `@example.com`, `user@UID.service` and repo-relative paths. The `/workspace/…` and
  `worktree-ccy-reboot-restore` strings in the committed reports are the container's fixed mount
  point and a branch name in this repo, as round 2 concluded.
- **Plan Commit Rule.** Working tree clean; plan, README row, design docs, journal and both
  earlier reports landed with the code. `PLAN.md` Task 6.1/6.2 now ticked with the suite counts
  named; 6.3 (PR) and 6.4 (HOST deploy) correctly unticked.
- **IaC placement.** Still no new play; everything lives in `play-claude-yolo.yml`, which owns the
  launcher and both front-ends.

## Mechanical gates

- `scripts/test-ccy-session-registry.bash`: **100 passed, 0 failed**, rc=0.
- `scripts/test-ccy-session-restore.bash`: **44 passed, 0 failed**, rc=0.
- `scripts/test-ccy-sessions-status.bash`: **42 passed, 0 failed**, rc=0.
- `qa-bash.bash`, `qa-patterns.bash`, `qa-docs.bash`, `qa-ansible.bash`, `qa-js.bash`,
  `qa-python.bash`: all rc=0.
- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`: rc=0 (throwaway vault
  password file in flag position, per `WORKTREE-QA-GAP.md`). Supplemented by a YAML parse:
  1 play, 1 pre_task, **61 tasks, 1 handler**, no unnamed task — unchanged from round 2.
- `hooks-daemon plan-qa --sweep`: 2 findings, 0 block, 2 advise (Plan 00046 path-existence,
  journal-freshness across twelve unrelated plans). Neither concerns Plan 00123.
- `qa-all.bash` not run end to end: `qa-ansible-syntax.bash` cannot pass in a worktree, documented
  in `WORKTREE-QA-GAP.md` and not re-reported. Every other stage was run individually above.
- **Conditional gates**: the true surface (`F44...HEAD`, 26 files) touches no `helpers/`,
  `tests/helpers/`, `extensions/` or extension JS, so `qa-helper-tests.bash`,
  `check_extension_compat` and ESLint are **not required**. `qa-js.bash` and `qa-python.bash` were
  run anyway and are green. (Note for anyone re-checking: a two-dot `git diff F44..HEAD` shows
  `helpers/`, `lxcfreeze` and `podfreeze` churn that belongs to F44's newer commits, not to this
  branch — use the three-dot form.)

## Reviewer notes

This role has no `Write`/`Edit`; this file was written via `Bash` because the report is too large
to return inline. Nothing else in the worktree was modified — every probe ran under
`untracked/scratch/` and those files were deleted afterwards.

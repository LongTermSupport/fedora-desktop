# QA Review (round 2) — branch `worktree-ccy-reboot-restore` vs `F44` (Plan 00123)

Reviewed: 4 commits, 24 files over `F44...HEAD`; the round-1 fixes are commit `81cb8ba8`
(CCY 3.59.0). Worktree: `/workspace/untracked/worktrees/worktree-ccy-reboot-restore`.

**Verdict**: FIX-BEFORE-MERGE — all 21 round-1 findings are genuinely fixed; 1 fix-before-merge,
7 should-fix and 5 nits are new, and one of them was introduced by a round-1 fix.

---

## Part 1 — every round-1 finding, re-checked against the code

| # | Round-1 finding                                        | Verdict | Evidence                                                                                                                                                                                                                                |
| - | ------------------------------------------------------ | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 | listing failure laundered into "nothing to restore"    | FIXED   | All four sites go through `ccy_registry_collect` (`session-registry.bash:499`), which reads via a temp file. Probed with a stubbed failing `find`: collect returns 1, array empty, error named. Probed the *partial* case too (find exits 0 but writes to stderr) — also returns 1. `ccy-sessions-restore:250`, `ccy-sessions:151,207`. |
| 2 | `stale` retires exactly the sessions the feature is for | FIXED   | `boot_time` recorded from `/proc/stat` btime (`session-registry.bash:181`, `claude-yolo:3261,3295`), compared boot-to-boot at `ccy-sessions-restore:315`. A record with no `boot_time` is restored with its age reported unknown (`:313`), not retired — probed, prints `note … restoring anyway`.                                       |
| 3 | `CCY_UNATTENDED=1` handed to the tmux client           | FIXED   | Delivered as `-- env CCY_UNATTENDED=1 bash -c …` inside the tmux command (`ccy-sessions-restore:219`). Asserted against a recording stub, both positively and negatively (`test-ccy-session-restore.bash:388-391`).                                                                                                                     |
| 4 | scope unit keyed on the project directory              | FIXED   | `--unit "ccy-tmux-restore-${name}"` (`ccy-sessions-restore:216`). Driven for real with two survivors in one project; both restored, two distinct unit names (`test-ccy-session-restore.bash:399-412`).                                                                                                                                  |
| 5 | `reboot --in N` / `notify` exit 0 doing nothing        | FIXED   | `ccy_reboot_signal_blocked` is now called once, unconditionally, after the audit (`ccy-sessions:389`, `:444`). With no sessions the audit returns 0 and the refusal still fires.                                                                                                                                                        |
| 6 | restore-off removed the unit without disabling it      | FIXED   | Stat → `systemd enabled: false` → `file: absent` → `notify` a `daemon_reload` handler (`play-claude-yolo.yml:532-556`, handler `:765-773`). **But see NEW-1** — the off path references a fact the off path never resolves.                                                                                                             |
| 7 | `ccy_registry_retire` laundered a failed read          | FIXED   | `2>&1 >"$tmp"` in the correct order and `awk` for `grep -v` (`session-registry.bash:558`). Probed: missing source → rc=1, error names `awk: cannot open …`, no record staged, temp removed. Probed the `grep -v` edge case (a record that is only `end=1`) → rc=0, correct output.                                                        |
| 8 | unguarded `echo`-then-bare-`read` prompt               | FIXED   | `read -rp "Press Enter to continue... "` (`token-management.bash:791`). Reads identically for an attended human — bash prints a `-p` prompt to stderr without a newline, where the old `echo` went to stdout with one. Plus a derived scan over the launcher and `lib/*.bash` (`test-ccy-session-registry.bash:695`).                     |
| 9 | `ccy_tmux_current_session` conflated 1 and 2           | FIXED   | Three outcomes documented and implemented (`tmux-session.bash:356-366`); the launcher exits 1 on 2 (`claude-yolo:3254`); `ccy_tmux_banner` propagates 2 and swallows only 1 (`tmux-session.bash:372-376`). Both callers checked.                                                                                                        |
| 10 | "could not ask" collapsed into `installed-not-enabled` | FIXED   | `installed-state-unknown:<probe>` is its own answer (`ccy-sessions:118-124`), with `disabled\|static\|indirect` still mapping to `installed-not-enabled`.                                                                                                                                                                                |
| 11 | `enabled-linger-unknown` missing from the docs table   | FIXED   | `docs/ccy.md:249-256` now has all six states.                                                                                                                                                                                                                                                                                            |
| 12 | `--no-supervise` restored with `--supervise`           | FIXED   | Mode recorded (`claude-yolo:3280-3285`) and honoured (`session-registry.bash:667-670`), with tests for `armed`, `off` and an unknown value (`test-ccy-session-registry.bash:545-551`).                                                                                                                                                  |
| 13 | `shift` past the end aborts with no message            | FIXED   | `$# -lt 2` guards on `--in` and `--minutes` (`ccy-sessions:353`, `:418`), and `cmd_notify`'s leading shift is guarded (`:398`).                                                                                                                                                                                                          |
| 14 | flag-classification population narrower than claimed   | FIXED   | Both parse sites derived, and a `COVERAGE:` line printed on every passing run. Observed: `COVERAGE: 25 flags from the argument loop, 2 from the positional checks, 27 total classified`, against 11 durable + 16 one-shot = 27.                                                                                                          |
| 15 | SSH keys silently dropped on a decode failure          | FIXED   | Two-pass decode: status checked, then data read (`session-registry.bash:622-626`), with a test that a corrupt `ssh_keys_b64` refuses (`test-ccy-session-registry.bash:355-358`).                                                                                                                                                         |
| 16 | `ccy-sessions-restore` committed 100644                | FIXED   | `git ls-tree HEAD` → `100755`.                                                                                                                                                                                                                                                                                                           |
| 17 | dead `"${DRY_RUN:+}"`                                  | FIXED   | Gone; all seven `DRY_RUN` uses are real tests.                                                                                                                                                                                                                                                                                            |
| 18 | `ccy-sessions` lib guard named only one library        | FIXED   | Loop over `tmux-session session-registry common-pure` (`ccy-sessions:78-87`).                                                                                                                                                                                                                                                             |
| 19 | design doc drew `ccy-<slug>.record`                    | FIXED   | `DESIGN-failure-modes.md:18` now says "named after the TMUX SESSION".                                                                                                                                                                                                                                                                     |
| 20 | `project_dir` printed raw in restore-status            | FIXED   | `${project/#${HOME}/\~}` in both places (`ccy-sessions:161`, `:221`).                                                                                                                                                                                                                                                                     |
| 21 | a held-open failed restore looks like a restored one   | FIXED   | Explained for the operator at `docs/ccy.md:284-287`.                                                                                                                                                                                                                                                                                      |

No finding was merely relocated. Every fix was checked at the code, and several were
additionally exercised by probe (1, 2, 7 and the `mapfile`/NUL plumbing).

### NUL-delimited output — every consumer on the branch

`ccy_registry_list`, `ccy_registry_restore_flags` and `ccy_registry_decode_argv` all emit
NUL-delimited payloads. Enumerated every consumer across the branch; **none uses `$(…)`**:

- `ccy_registry_list` → `ccy_registry_collect` (temp file, status observed); tests at
  `test-ccy-session-registry.bash:186,510` (`mapfile -t -d '' < <(…)`, NUL-safe).
- `ccy_registry_restore_flags` → `ccy-sessions-restore:176` (`mapfile … < <(…)`). The status is
  still discarded there, but every failure path in that function returns *before* the single
  trailing `printf`, so a failure always yields an empty array and hits the explicit
  `${#flags[@]} -eq 0` refusal at `:177`. Sound, though the message ("produced no restore command
  line") is weaker than the `print_error` that preceded it on stderr.
- `ccy_registry_decode_argv` → `session-registry.bash:622` (status pass, data discarded) and
  `:626` (data pass); test at `:259`.

---

## Part 2 — new findings

### Fix before merge

#### NEW-1. Turning restore OFF fails on a standalone play run: the off path uses a fact only the on path resolves

`playbooks/imports/play-claude-yolo.yml:546` and `:773`

The two tasks the round-1 fix added both template the user's uid:

```yaml
      environment:
        XDG_RUNTIME_DIR: "/run/user/{{ ansible_facts['getent_passwd'][user_login][1] }}"
```

but the tasks that *produce* that fact are gated on the opposite condition:

- `Resolve The Session User UID` — `when: ccy_restore_sessions | bool` (`:491`)
- `Assert The Session User Exists` — `when: ccy_restore_sessions | bool` (`:503`)

So with `ccy_restore_sessions` false and the unit present — precisely the "turn it off"
transition this fix exists to handle — `ansible_facts['getent_passwd']` has never been set by
this play, and both the disable task and the reload handler template an undefined value.
`gather_facts` does not collect `getent_passwd`; only `ansible.builtin.getent` does.

It works in a full `playbook-main.yml` run only by accident:
`playbooks/imports/play-systemd-user-tweaks.yml:24` runs an ungated `getent` and is imported at
`playbook-main.yml:15`, ahead of `play-claude-yolo.yml` at `:39`, and host facts persist across
plays. Standalone it does not — and this play is documented as a standalone command in three
places (`play-claude-yolo.yml:460`, and `ccy-sessions:179,183` prints it to the operator).
`--syntax-check` cannot see this (rc=0, verified).

**Fix**: remove `when: ccy_restore_sessions | bool` from `Resolve The Session User UID` and
`Assert The Session User Exists`. The uid is needed on both branches, and the assert's fail_msg
is the message you want on either.

### Should fix

#### NEW-2. A non-numeric `boot_time` aborts the boot service mid-loop, contradicting its own gather contract

`files/home/.local/bin/ccy-sessions-restore:315`

```bash
elif [[ $(((current_boot_time - record_boot_time) / 86400)) -gt "$CCY_RESTORE_MAX_AGE_DAYS" ]]; then
```

Measured, against the real script with a scratch registry holding `boot_time=not-a-number`:

```
ccy-sessions-restore: boot <uuid>
DRY RUN — nothing will be started, consumed or retired.
ccy-sessions-restore: line 315: not: unbound variable
RC=1
```

The run dies inside the loop: no verdict for that record, no processing of any record after it,
no summary line, no `last-run` note. That directly contradicts `:378-385` ("process everything,
name every failure, and still fail") and the quarantine contract at `:266-279`, which exists so
that a record which cannot be trusted is *named*, not fatal.

It is reachable by the same routes the library header already anticipates for a bad record — a
hand-edit, a restore from backup, a partial overwrite that leaves `end=1` intact.
`ccy_registry_validate` checks the terminator, the schema and `CCY_REGISTRY_REQUIRED_KEYS`
(`project_dir boot_id restore`); `boot_time` is in none of them, and no field is type-checked.

**Fix**: test `[[ "$record_boot_time" =~ ^[0-9]+$ ]]` and, when it does not match, retire the
record with a named reason (it is a malformed field, so the `failed-validation` path).
`CCY_RESTORE_MAX_AGE_DAYS` has the same shape from the environment side.

#### NEW-3. `cmd_restore_status` now truncates its own report when one section cannot be read

`files/home/.local/bin/ccy-sessions:151-154`, called at `:228-230`

The round-1 fix gave `list_with_reasons` a `return 1`. Under the file's `set -euo pipefail`, a
bare call in a `case` body is not a condition context, so errexit fires: an unreadable
`attempted/` prints its "COULD NOT BE READ" line and then the shell exits, skipping `retired/`,
`malformed/` and the last-run note, and never reaching `exit $?` at `:453`. Measured with an
isolated repro (a function returning 1 → only the first section printed, rc=1).

That is the opposite of what this command is for: `:98-101` says it "reports TWO things, and
never multiplies them into one verdict", and the restore service's own gather semantics exist
for exactly this reason.

**Fix**: `list_with_reasons … || status_failed=1` for each of the three, and
`return "$status_failed"` at the end — the same shape as `ccy-sessions-restore:366-385`.

#### NEW-4. `ccy-sessions` is deliberately untestable, and three round-1 fixes landed in it verified by reading only

`files/home/.local/bin/ccy-sessions:36-37`

```bash
CCY_LIB="/var/local/claude-yolo/lib"
CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
```

No `${CCY_LIB:-…}` override. Its sibling `ccy-sessions-restore:48` adds exactly that override,
with the reason written out: *"code that cannot be run in a test is code whose retirement
decisions are verified by reading"*. There is no `scripts/test-ccy-sessions*.bash`, and
`grep -rn 'ccy-sessions\b' scripts/ tests/` (excluding `-restore`) returns nothing.

Round-1 findings 5, 10 and 13 all live in this file; 165 lines of it changed in `81cb8ba8`; none
of it is exercised. The things now unverified include the two blocked-seam exit statuses, the six
answers of `restore_installation_state`, both argument-guard paths, and NEW-3 above — which is
the kind of defect a single test would have caught.

**Fix**: the same `${CCY_LIB:-…}`/`${CCY_LAUNCHER:-…}` override, and a suite driving
`reboot`/`notify`/`restore-status` against stubs, wired into `qa-all.bash` with a pass line.

#### NEW-5. D8's own table still lists four answers; the code emits six, and the two it omits are the "could not tell" ones

`CLAUDE/Plan/00123-…/DESIGN-failure-modes.md` (D8 table), `CLAUDE/Plan/00123-…/PLAN.md` Task 4.2

`docs/ccy.md:249-256` was updated to six rows. The design document that D8 *is* the source of
truth for was not: its table still reads `not-installed`, `installed-not-enabled`,
`enabled-no-linger`, `enabled`. Missing: `enabled-linger-unknown` and the new
`installed-state-unknown` — i.e. both of the "cannot tell" answers, in the decision record whose
entire thesis is that "cannot tell" must not collapse into "nothing to do". `PLAN.md` Task 4.2
still says "the five distinct answers (D8)", which matches neither.

#### NEW-6. `docs/ccy.md:258` miscounts its own states

> "Three of those are 'could not tell' rather than 'will not run'"

There are two: `installed-state-unknown` and `enabled-linger-unknown`. The other four
(`not-installed`, `installed-not-enabled`, `enabled-no-linger`, `enabled`) are all definite.

#### NEW-7. `docs/ccy.md:279` documents a retirement reason the code never writes

The table row is `` `malformed` ``. The reason actually recorded is `failed-validation`
(`ccy-sessions-restore:272`); `malformed` is the *directory* name. An operator reading the
"Quarantined records" section of `restore-status` sees `failed-validation` and will not find it
in the documented table.

#### NEW-8. The `stale` documentation did not move with the behaviour

The rule changed from record age to boot-to-boot age. Still describing the old rule:

- `docs/ccy.md:278` — "older than `CCY_RESTORE_MAX_AGE_DAYS` (default 7)"
- `ccy-sessions-restore:80` (`--help`) — "retire records older than this as 'stale' (default 7)"
- `ccy-sessions-restore:51-53` (header) — "A record older than this…"

Only the in-line comment at `:300-311` and the changelog state the new rule. The `--help` text is
the one an operator setting `CCY_RESTORE_MAX_AGE_DAYS` will read.

### Nits

#### NEW-9. `ccy_registry_count` has no caller anywhere

`files/var/local/claude-yolo/lib/session-registry.bash:521-526`. Not the launcher, not either
front-end, not either test suite (`grep -rn ccy_registry_count files/ scripts/ playbooks/ docs/
CLAUDE/` returns only its own definition). YAGNI — delete it, or give it the caller it was
written for.

#### NEW-10. The collect error is printed twice

`ccy_registry_collect:511` wraps an error `ccy_registry_list:478` has already formatted and
printed. Measured:

```
ERROR: could not list records in /tmp/…: ERROR: could not list records in /tmp/…: find: '…': Permission denied
```

Capture the listing's stderr without re-prefixing it, or let the inner message stand alone.

#### NEW-11. `ccy_registry_collect` can leak its temp file

`session-registry.bash:506-518` — `rm -f "$tmp"` runs on the two failure paths and after a
successful `mapfile`, but not if `mapfile` itself fails or the process is signalled between
`mktemp` and the `rm`. No trap. Low consequence (a small file in `$TMPDIR`), but this function is
the one every caller now depends on.

#### NEW-12. The derived bare-read scan is narrower than the hang it guards against

`scripts/test-ccy-session-registry.bash:695-699` matches a `read` with no `-p` *and no variable*.
`echo "Continue? [y/N]"` followed by `read -r answer` hangs identically and is not matched. The
population is currently empty — all three `-p`-less reads in the launcher and `lib/*.bash`
(`claude-yolo:2109,2391`, `tmux-session.bash:298`) are here-string splits, checked by hand — so
this is a shape to watch, not a live defect.

#### NEW-13. `ccy_tmux_current_session` merges stderr into a value it only reads on failure

`tmux-session.bash:361` — `name=$(tmux display-message -p '#S' 2>&1)`. On success `$name` is
written straight into the record filename (`claude-yolo:3290`), so anything tmux emits on stderr
while still exiting 0 becomes part of the session's record name. No evidence tmux does this;
noted because the merge buys nothing on the success path — `2>"$errfile"` would keep the payload
clean and still name the reason on failure, which is the pattern `ccy_registry_list` already uses.

---

## Checked and clean

- **The `handlers:` block.** Parsed the YAML: one play, top-level keys
  `['become','handlers','hosts','name','pre_tasks','tasks','vars']`, **61 tasks / 1 handler /
  1 pre_task**. Nothing absorbed. The handler fires only on a `changed` removal, which is right.
- **`ccy_registry_collect` failure paths.** `mktemp` failure → named error, rc=1, no file. Listing
  failure → error, temp removed, rc=1. Partial listing (find exits 0 with stderr) → rc=1. All
  probed with a stubbed `find`; the earlier-looking "unreadable directory" probe was invalid in
  this container (uid 0 reads a 0000 directory), which is why the stub was used instead.
- **`ccy_tmux_current_session`'s status 2.** Both callers handle it: `claude-yolo:3254` exits 1
  with a named reason, `ccy_tmux_banner:376` returns 1 for 2 and 0 for 1. No third caller exists.
- **`ccy_registry_retire`'s redirection and awk.** Probed four ways (missing source, record with
  no terminator, record that is only `end=1`, normal record). Correct in all four; no diagnostic
  ever reaches the staged record; the original is only removed after the install succeeds.
- **`cmd_reboot` / `cmd_notify` control flow.** `--dry-run` returns 0; every other path reaches
  the unconditional `ccy_reboot_signal_blocked` and leaves the shell non-zero (errexit fires at
  the function call, before `exit $?`, with the same status). `audit_sessions` failure
  short-circuits with `|| exit 1`. Argument guards refuse with 64 and a message. Verified by
  reading — see NEW-4.
- **`read -rp` for an attended human.** Same text, same position, same "press Enter" semantics;
  the only difference is stderr-without-newline instead of stdout-with-newline, which is the
  house rule anyway. Caught by the guard under `CCY_UNATTENDED=1` (regex `^-[a-zA-Z]*p$` matches
  `-rp`), which is the point of the change.
- **Version and container integrity.** `CCY_VERSION` 3.58.1 → **3.59.0** with a rewritten one-line
  comment; changelog entry present and matching the code. `REQUIRED_CONTAINER_VERSION="2.37"` and
  `Dockerfile` `LABEL claude-yolo-version="2.37"` agree, and neither the Dockerfile nor
  `entrypoint.sh` is in the diff. `CCY_HASH` derives its set from `lib/*.bash`, so the new library
  is covered without a list edit.
- **Fail-fast.** No new `failed_when: false`, `ignore_errors: true`, `|| true` or bare
  `2>/dev/null` anywhere in the branch diff (grepped added lines only). `qa-ansible.bash` green.
- **IaC placement.** Still no new play; everything lives in `play-claude-yolo.yml`, which owns the
  launcher and both front-ends. The handler is the right mechanism for a reload that must follow
  a removal.
- **Public-repo safety.** Scanned every added line and all four commit messages for emails,
  `/home/<user>` paths, hostnames and IPv4 literals: the only hits are `user@UID.service` (a
  systemd unit template) and `@example.com`. No container, project, host or account names. The
  `/workspace/untracked/worktrees/…` strings inside the committed round-1 report are the CCY
  container's generic mount path and a branch name in this repo, not a machine-specific checkout.
- **Plan Commit Rule.** Working tree clean; plan, README row, design docs, journal and the round-1
  report all landed with the code. `PLAN.md` status is honest — Task 6.1 (`qa-all.bash` green) and
  6.3/6.4 unticked, Round 2 marked in progress.
- **QA wiring.** Both suites run from `qa-all.bash:304` and `:321`, each with a visible pass line
  carrying its case count.

---

## Mechanical gates

- `scripts/test-ccy-session-registry.bash`: 100 cases passed, 0 failed, rc=0.
- `scripts/test-ccy-session-restore.bash`: 44 cases passed, 0 failed, rc=0.
- `qa-bash.bash`, `qa-patterns.bash`, `qa-docs.bash`, `qa-ansible.bash`, `qa-js.bash`,
  `qa-python.bash`: all rc=0.
- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`: rc=0 (flag-position
  throwaway vault password file, per `WORKTREE-QA-GAP.md`). Supplemented by a YAML parse counting
  tasks vs handlers, which `--syntax-check` cannot do.
- `hooks-daemon plan-qa --sweep`: 2 findings, 0 block, 2 advise — Plan 00046 path-existence and
  journal-freshness on Plan 00163. Neither relates to Plan 00123.
- `qa-all.bash` was not run end to end: `qa-ansible-syntax.bash` cannot pass in a worktree
  (documented in `WORKTREE-QA-GAP.md`); every other stage was run individually above.
- **Conditional gates**: the diff touches no `helpers/`, `tests/helpers/`, `extensions/` or
  extension JS, so `qa-helper-tests.bash`, `check_extension_compat` and ESLint were not required.
  `qa-js.bash` and `qa-python.bash` were run anyway and are green.

## Reviewer notes

This role has no `Write`/`Edit`; this file was written via `Bash` because the report is too large
to return inline. Nothing else in the worktree was modified — the probes all ran in `mktemp -d`
directories outside the repo and cleaned up after themselves, and the scratch files used under
`untracked/scratch/` were deleted.

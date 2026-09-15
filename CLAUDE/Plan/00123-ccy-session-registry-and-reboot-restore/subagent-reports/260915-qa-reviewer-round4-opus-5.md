# QA Review (round 4, confirmation) — branch `worktree-ccy-reboot-restore` vs `F44` (Plan 00123)

Confirmation pass over commit `44605ac2` (CCY 3.59.2), the fix for round 3's F1-F4 and N1-N4.
Surface: 6 commits, 27 files over `F44...HEAD`. Worktree:
`/workspace/untracked/worktrees/worktree-ccy-reboot-restore`. Working tree clean.

**Verdict**: FIX-BEFORE-MERGE — all eight round-3 findings are genuinely fixed and every
measured probe of the new nameref API behaves. Two things introduced by `44605ac2` need a line
each: the new `sort -z` pipeline silently re-opens the exact "could not tell reads as nothing
here" collapse for any caller without `pipefail`, and `PLAN.md` states suite counts that the
same commit made false.

---

## Part 1 — F1-F4 / N1-N4, re-checked against the code

| #  | Round-3 finding                                             | Verdict   | Evidence                                                                                                                                                                                                                                                                                                                       |
| -- | ----------------------------------------------------------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| F1 | boot service died mid-loop on a mistyped retention setting  | **FIXED** | Validation loop at `ccy-sessions-restore:137-149`, before `SESSIONS_DIR` is even resolved. Probed against the real script: `notanumber` / `7d` / `-1` → rc=2, `ERROR: CCY_RESTORE_MAX_AGE_DAYS must be a whole number of days`, `Nothing was read, started or retired.`; `""` and `env -u` → the `:-7` default, rc=0. Both vars covered. |
| F2 | the contract comment said four answers, the code emits six  | **FIXED** | `ccy-sessions:112-120` now enumerates all six and says `SIX answers`. Repo-wide scan for a four-answer claim over `files/ scripts/ docs/ CLAUDE/Plan/00123*`: only historic report prose and an unrelated `run-bash-changelog.md` line.                                                                                          |
| F3 | the suite never drove `list_with_reasons`' unreadable branch | **FIXED** | `test-ccy-sessions-status.bash:246-262` creates the three evidence dirs and asserts `4` COULD-NOT-BE-READ lines plus `INCOMPLETE`. Probed the real script both ways with a broken `TMPDIR`: without the dirs the count is **1**, with them **4**. Non-vacuous, and it is the count that makes it so.                             |
| F4 | the header claimed the reboot audit; no case exercised it   | **FIXED** | The tmux stub is now stateful (`:82-98`) and driven by `$WORK/tmux-sessions` + `$WORK/tmux-rc`. New cases cover the ready table, `NO DAEMON CLI`, the `1 of 2` partial-warning refusal, the listing-failure `UNKNOWN` branch, and `reboot --in` with sessions live. Suite 42 → **59** cases.                                     |
| N1 | stranded `ccy_registry_list` header, and a false `sorted`   | **FIXED** | `session-registry.bash:464-483` is one header for `ccy_registry_collect`; no orphan block, no global. The `sorted` claim is now TRUE — `LC_ALL=C sort -z` at `:498`, pinned by `test-ccy-session-registry.bash:536-542` (`aaa-first.record` first).                                                                              |
| N2 | `ccy_registry_list` had no production caller                | **FIXED** | Function deleted. Repo-wide grep over `files/ scripts/ playbooks/ docs/ CLAUDE/`: the only hits are the earlier reports' and the journal's own prose. Both tests retargeted at `ccy_registry_collect` (`:187`, `:511`), and the existence guard at `:49` now lists `ccy_registry_collect`.                                       |
| N3 | the `enabled` assertion also passed for `enabled-no-linger` | **FIXED** | `test-ccy-sessions-status.bash:214-218` — `grep -qx 'Session restore on this machine: enabled'`, whole line.                                                                                                                                                                                                                    |
| N4 | changelog claimed cleanup on every path                     | **FIXED** | `docs/ccy-changelog.md:38-41` — "removed on every path the function itself takes; a signal delivered mid-call would leave one behind". Matches the code, which still has no trap.                                                                                                                                               |

## Part 2 — answers to the four specific questions asked

- **Every call site passes an array name and reads the right variable.** Three production
  callers, all direct (never in `$( )`, a pipeline or a subshell): `ccy-sessions:171`
  (`found`, declared `local -a found=()` at `:170`), `ccy-sessions:230` (`records`, declared
  `:229`), `ccy-sessions-restore:272` (`records`, `records=()` at `:271`). Four test call
  sites: `:187 listed`, `:515 absent_listing`, `:526 unreadable_listing`, `:542
  sorted_listing`. `claude-yolo` sources the library but calls neither.
- **Nameref collision is not reachable.** No call site passes a `_ccy_collect_*` name. Probed
  what would happen if one did: `_ccy_collect_out` gives four `circular name reference`
  warnings but still works; `_ccy_collect_tmp` returns **rc=0 with an empty array and no
  warning at all** — silent. See N4-new below; the prefix is the mitigation and it holds
  today.
- **`sort -z` and the find's exit status: yes, it can now be masked.** See F-new-1.
- **`${!ccy_days_var}` under `set -u` is safe.** Both variables are unconditionally assigned
  at `:59` and `:64` with `${VAR:-N}`, which covers unset *and* empty, so the indirect
  expansion at `:144-145` always resolves. Measured: unset → default 7, `""` → default 7,
  non-numeric → rc=2 with the name and the offending value printed.

---

## Part 3 — new findings, introduced by `44605ac2`

### Should fix

#### F-new-1. The new `sort -z` pipeline re-opens the collapse the function exists to prevent

`files/var/local/claude-yolo/lib/session-registry.bash:497-502`

```bash
_ccy_collect_err=$( { find "$_ccy_collect_dir" -maxdepth 1 -name '*.record' -type f -print0 |
    LC_ALL=C sort -z; } 2>&1 >"$_ccy_collect_tmp") || {
```

Before this commit the `find` was the only command in the group, so its exit status was
observed unconditionally. It is now the *first* stage of a pipeline, so without `pipefail` the
status reaching `||` is `sort`'s — and `sort` succeeds on empty input. Measured against the
REAL library with a stub `find` that exits 1 without writing to stderr:

```
caller opts 'set -euo pipefail' -> ERROR: could not list records in .../recs:    (FAILED correctly)
caller opts 'set -eu'           -> SUCCEEDED with 0 records
```

That second line is the exact sentence the function's own header at `:482-483` forbids: "A
directory that exists and cannot be read is a FAILURE, and the two must never look alike."

Not blocking, and I want to be precise about why: both production callers set `set -euo
pipefail` (`ccy-sessions:34`, `ccy-sessions-restore:38`), and GNU `find` diagnoses on stderr,
which the `[[ -n "$_ccy_collect_err" ]]` check at `:505` still catches. So nothing is broken
today. What changed is that an unconditional guarantee became a guarantee conditional on an
**undocumented caller shell option** — in the one library that three programs source, and
`claude-yolo` is `set -e` only (`:127`, no `pipefail`) while sourcing it at `:66`.

**Fix**: one word, confined to the subshell so no caller state is touched —
`_ccy_collect_err=$( set -o pipefail; { find … | sort -z; } 2>&1 >"$tmp" ) || {`. Verified:
silently-failing find under a `set -eu` caller now fails correctly, and a healthy find still
lists both records in sorted order.

#### F-new-2. `PLAN.md` states three suite counts that this commit made false

`CLAUDE/Plan/00123-ccy-session-registry-and-reboot-restore/PLAN.md:170-171`

```
- [x] ✅ **Task 6.1**: every QA stage green bar the one named above; all four `test-ccy-*`
  suites for this plan pass (registry 100, restore 44, status 42)
```

Measured now: registry **105**, restore 44, status **59**. `44605ac2` added 5 registry cases
and 17 status cases and left the ticked line that counts them unchanged — in the commit whose
own subject is tests that claimed more than they delivered. A ✅ beside a measurably false
number is the Plan Commit Rule's drift case.

Two smaller errors in the same sentence: there are **three** suites for this plan, not four
(`git diff F44...HEAD --name-status -- scripts/` adds exactly `test-ccy-session-registry`,
`test-ccy-session-restore`, `test-ccy-sessions-status`, and `qa-all.bash` wires exactly those
three); and naming counts at all is what made the line rot. State the three suite names and
let `qa-all.bash`'s derived `passed: N` lines carry the numbers, as they already do.

### Nits

#### N-new-1. F1's own fix is verified by reading — the defect class this commit is about

`grep -rn 'CCY_RESTORE_MAX_AGE_DAYS\|CCY_RESTORE_EVIDENCE_DAYS' scripts/` returns **nothing**.
`scripts/test-ccy-session-restore.bash` drives the real restore script through every retirement
reason but never sets or malforms either retention variable, so the startup validation at
`ccy-sessions-restore:143-149` has no regression test. Rounds 1-3 each added tests for their
own fixes; this one did not. I probed it by hand and it is correct — that is precisely the
weaker evidence F3 and F4 were raised about. Two cases (`CCY_RESTORE_MAX_AGE_DAYS=notanumber`
→ rc 2 and nothing touched; a valid value → the run proceeds) cost almost nothing.

#### N-new-2. The EXIT CODES header does not list the new cause of exit 2

`files/home/.local/bin/ccy-sessions-restore:32` still reads
`#   2  the environment is wrong (no launcher, no tmux, no systemd-run)`. A non-numeric
retention setting is now a fourth way to exit 2 (`:147`). Same shape as F2, which this commit
fixed: the enumeration in the contract comment did not move with the code beside it.

#### N-new-3. `CLAUDE/QA.md`'s row for the status suite did not grow with the suite

`CLAUDE/QA.md:60` describes it as "`ccy-sessions restore-status` **and the blocked reboot
seam**". After F4 the suite also drives the audit table, the `N of M could not be warned`
refusal and the tmux listing-failure branch — the suite's own header at
`test-ccy-sessions-status.bash:14-17` says so. Round 3 offered narrowing the header *or*
adding the cases; the cases were added and the QA.md row was left at the narrow description.

#### N-new-4. A `grep -c` substring count, six dozen lines from the substring bug this commit fixed

`scripts/test-ccy-sessions-status.bash:322-323`

```bash
check "each is marked ready when its project has the daemon CLI" "2" \
    "$(printf '%s' "$OUT" | grep -c 'ready')"
```

`ready` is a substring: it also matches `already`, and any session name containing it. The very
next case in the same file names a fixture `ccy-ready` (`:335`). It passes for the right reason
today (the fixtures are `ccy-proj-a`/`ccy-proj-b`, and the count is 2) and it does catch a
regression in the direction that matters, but it is the same loose-match shape as N3 —
which this commit fixed thirty lines earlier with `grep -qx`. `grep -cE '^ +ccy-proj-[ab] +ready '`
matches the column the audit actually prints (`ccy-sessions:355`).

#### N-new-5. The reserved argument names are a silent trap, and the header does not mention them

`session-registry.bash:484-487`. Passing `_ccy_collect_tmp`, `_ccy_collect_err` or
`_ccy_collect_dir` as the array name returns **rc=0 with an empty array and no diagnostic** —
the nameref binds to the global, then the function's own `local` shadows it, so `mapfile` fills
a variable that dies at `return`. Measured. Unreachable from every existing call site (the
prefix is exactly the mitigation), but the header documents the API at length and never says
the names are reserved, and the failure mode is the silent one. One sentence closes it.

---

## Checked and clean

- **The F1 validation's placement.** After `--help` (so `--help` still works with a bad value),
  after the tool and launcher checks, before `ccy_registry_sessions_dir` — nothing is read,
  started or retired first, which is what its comment and its message both claim. `print_error`
  is in scope (`common-pure.bash` sourced at `:115`).
- **The nameref rewrite, end to end.** Every production and test call site passes a literal
  array name declared in the caller, none inside `$( )`, a pipeline or any other subshell;
  `list_with_reasons` and `cmd_restore_status` are themselves called directly
  (`ccy-sessions:250-252`, `:482`). `CCY_REGISTRY_RECORDS` has no remaining reference anywhere
  in the repo. All three suites green.
- **Fail-fast over the commit's added lines.** No `failed_when:`, `ignore_errors:`, `|| true`,
  bare `2>/dev/null` or `set +e`. The one skip-shaped construct, `[[ -d "$dir" ]] || return 0`,
  is the documented absent-vs-unreadable distinction and is asserted from both sides
  (`test-ccy-session-registry.bash:511-533`).
- **Version and container integrity.** `CCY_VERSION` 3.59.1 → **3.59.2** with a rewritten
  one-line comment naming both changes; `docs/ccy-changelog.md:20-32` has the matching section.
  The launcher is staged in the same commit as the `lib/` edit, so the pre-commit CCY gate is
  satisfied and `CCY_HASH` covers the library. `REQUIRED_CONTAINER_VERSION="2.37"` untouched;
  nothing baked into the image changed, so the Dockerfile LABEL correctly does not move.
- **No hardcoded test counts anywhere else.** `qa-all.bash` derives every `passed: N` line from
  the suite's own output (`:309`, `:326`, `:343`), and `CLAUDE/QA.md` and `docs/` name no
  counts. The only stale number in the repo is F-new-2's.
- **Stderr hygiene.** The new validation's `print_error` and its follow-up sentence both go to
  stderr; `restore-status`' report remains the documented status-command carve-out.
- **Public-repo safety.** Scanned all 6,140 added lines of `F44...HEAD` and all six commit
  messages: no non-`example.com` email, no IPv4 literal, no private key or token, no real
  hostname or domain beyond `github.com`/`example.com`/`anthropic.com`. The `/home/…` hits are
  either repo-relative `files/home/...` paths or the deliberate space-in-path fixtures
  (`/home/a user/My Projects/app`, `/home/u/work/app`). `/root/.claude` is the container's own
  home, `sk-ant-oat01-` is a pre-existing context line naming a token *prefix*, and
  `Edmonds-Commerce-Limited/claude-code-hooks-daemon#39` is the public upstream repo
  `CLAUDE.md` itself points at. `**Owner**: joseph` matches 95 other plans and is generated by
  `mkplan.bash:271-299` from the git identity — settled convention, not re-litigated.
- **Plan Commit Rule.** Working tree clean. `44605ac2` carries the round-3 report, the journal
  entry and the `PLAN.md` round-3 bullet alongside the code. README index row present. 6.3 (PR)
  and 6.4 (HOST deploy) correctly unticked.
- **IaC placement.** `44605ac2` touches no playbook and adds no play; all of it lands in files
  `play-claude-yolo.yml` already owns.

## Mechanical gates

- `scripts/test-ccy-session-registry.bash`: **105 passed, 0 failed**, rc=0.
- `scripts/test-ccy-session-restore.bash`: **44 passed, 0 failed**, rc=0.
- `scripts/test-ccy-sessions-status.bash`: **59 passed, 0 failed**, rc=0.
- `qa-bash.bash`, `qa-patterns.bash`, `qa-docs.bash`, `qa-ansible.bash`, `qa-js.bash`,
  `qa-python.bash`: all rc=0.
- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`: rc=0 (throwaway
  vault password file in flag position; stdio redirected to a file because Ansible refuses
  non-blocking handles under this agent).
- `hooks-daemon plan-qa --sweep`: 2 findings, 0 block, 2 advise — Plan 00046 path-existence and
  journal-freshness on Plan 00163. Neither concerns Plan 00123.
- `qa-all.bash` not run end to end: the `qa-ansible-syntax.bash` vault gap documented in
  `WORKTREE-QA-GAP.md`, not re-reported. Every other stage run individually above.
- **Conditional gates**: `F44...HEAD` touches no `helpers/`, `tests/helpers/`, `extensions/` or
  extension JS, so `qa-helper-tests.bash`, `check_extension_compat` and ESLint are **not
  required** and were not run. `qa-js.bash` and `qa-python.bash` were run anyway, both green.
  (Use the three-dot form: a two-dot `git diff F44..HEAD` shows `helpers/`, `lxcfreeze` and
  `podfreeze` churn belonging to F44's newer commits, not to this branch.)

## Reviewer notes

This role has no `Write`/`Edit`; the report was written via `Bash` because it is too large to
return inline, as the launching agent requested. Nothing else in the worktree was modified —
every probe ran under `untracked/scratch/qa4/` and that directory was deleted afterwards;
`git status --short` is empty.

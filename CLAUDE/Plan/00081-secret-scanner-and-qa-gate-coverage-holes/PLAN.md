# Plan 00081: Secret-scanner and QA-gate coverage holes

**Status**: In Progress
**Created**: 2026-08-20
**Owner**: joseph
**Priority**: High

## Overview

A repo-wide hunt for the defect class documented in Plan 00079/00080 —
[*A partial result read as a complete one*](../../AgentNotes.md) — returned
**seven** further instances outside those plans. Two are in the **pre-commit
secret scanner** on a public repository, and both let real content reach a
commit while the hook printed a success line.

The class: a check verifies the path its author had in mind, is silent about the
path that carries the load, and returns a result shaped exactly like a complete
one. Nothing fails, everything exits 0, the answer is merely narrower than the
question. An over-match names itself in the output; an under-match is silent.

This plan fixes the security-relevant instances first, then the QA-gate coverage
holes, each behind a test that fails against the unfixed code.

## Goals

- The secret scanner sees every staged path, and whitelisting a token stops
  shielding the rest of its line.
- `qa-python.bash` gains the shebang-based discovery and coverage assertion that
  Plan 00076 gave the bash gates, and the 31 findings it cannot currently see
  are surfaced.
- `qa-deployed-drift.bash` stops silently skipping a repo file whose deployed
  name differs.
- Every fix has a gate that demonstrably fails without it.

## Non-Goals

- **Rewriting published history.** A real project identifier is already on
  `origin/F44` in an earlier plan (see Risks); deciding what to do about that is
  the owner's call, not this plan's.
- Re-auditing the bash gates — Plan 00076 hardened them and the hunt confirmed
  them clean.
- The `docker-health.bash` `dead`-state gap: real but rare, Docker-for-CCY is
  non-default and discouraged, and it could not be exercised here. Recorded as
  F6 for a future decision rather than fixed blind.

## Facts

- **F1** — `pre-commit:89` listed staged files with `--diff-filter=ACM`. Rename
  detection is on by default (`diff.renames`, git ≥ 2.9), so a `git mv` plus an
  edit is classified `R` and was **excluded entirely — never scanned**.
  Reproduced on git 2.39.5: a 40-line file moved with one line appended reports
  `R098`, `--diff-filter=ACM` returns nothing, and the hook printed
  `✓ No files staged for commit` and exited 0. Moving a plan folder into
  `Completed/` is exactly this shape
- **F1b** — the hole **appears and disappears with file size**. A small file
  falls below the rename-similarity threshold and is recorded `D`+`A`, so the
  add *is* scanned. My own first reproduction used a 2-line file, passed, and
  would have let me dismiss a real security finding as unreproducible
- **F2** — the email whitelist filtered whole **lines** (`grep -v`), so a real
  address sharing a line with `git@github.com` was deleted along with it.
  Verified: that line matches the email pattern once and survives the chain with
  zero matches remaining. The file's own comment says a legitimate reference
  "no longer whitelists a genuine leak elsewhere in the same **file**" — that
  was fixed; the same-**line** case was not
- **F2b** — `CLAUDE/PlanTriage.md` already states the correct rule, learned from
  a real leak in Plan 00066: *"Redact by substitution, never by dropping
  anchored lines."* The scanner was doing the opposite of the repo's own
  documented lesson
- **F3** — `qa-python.bash` discovers by extension (`:31`) or **file mode**
  (`:48-56`). This is Plan 00076's bash-gate defect, unfixed in the Python gate:
  7 tracked repo-owned files, 4,003 lines, mode 0644 with a `python3` shebang,
  are never compiled or linted — and the repo's own ruff config finds **31
  violations** in them while the gate prints `✓ python: 35 files OK`.
  `CLAUDE/QA.md` names `wsi-stream` as *the* example of Python needing care; it
  is one of the seven
- **F4** — `qa-deployed-drift.bash:117-125` compares by basename, so
  `git-account-helper.j2` (deployed as `git-account-helper`) never matches, never
  increments `CHECKED`, and the pass line still claims the deployed scripts
  match. That is Plan 00067's failure on the one file in scope it cannot see
- **F5** — `pre-commit:341-348` harvests the denylist with
  `field.endswith("_username") or field.endswith("_account")`, but the file's
  convention is the **plural** (`github_accounts` had to be hardcoded), so
  `lastpass_accounts` and others are never harvested
- **F6** — `docker-health.bash:73` iterates `exited created` while its own
  comment and the user-facing message both name `dead`. Docker-only, rare, and
  unexercisable in a container — recorded, not fixed
- **F7** — **the CCY version-bump gate covers `claude-yolo` alone.**
  `pre-commit:105` keys on that one path, and the runtime hash
  (`claude-yolo:72`) is `md5sum` of `"$0"` — the launcher only. It sources six
  libraries totalling **212 KB against the launcher's 149 KB**. Measured: **71
  commits have touched `lib/`, and 22 of them touched `claude-yolo` not at
  all**, so no bump was ever required and none was made. A behaviour change in
  `lib/token-management.bash` therefore ships with an unchanged `CCY_VERSION`,
  an unchanged hash, `validate_ccy_integrity` reporting a match, and no
  changelog entry. `CLAUDE/ContainerRules.md` states the rule as "ANY code
  change requires a version bump"; enforcement covers one file of seven
- **F8** — **`commit-msg` has no `localhost.yml` denylist.** `pre-commit` runs
  static patterns **and** the SEC-02 dynamic denylist; `commit-msg` runs only
  the static patterns, then prints `✓ Commit message looks clean`. So a private
  identifier with no static pattern — an account alias, a machine hostname, a
  service username — is rejected in a staged *file* and accepted in a *commit
  message*. That is the worse of the two: **a commit message cannot be fixed by
  a follow-up commit**
- **F9** — `qa-ansible-syntax.bash:51-61` hardcodes discovery to
  `playbooks/imports` plus the entrypoint, so `playbooks/dev/play-collect-diagnostics.yml`
  is **never** `--syntax-check`ed — and it has no zero guard either.
  `AgentNotes.md` documents three 2.19 parse hazards that *only* this gate
  catches, on a play that runs during an incident. `qa-ansible.bash`'s greps DO
  cover `playbooks/dev/`, so the two Ansible gates disagree about the population
  and neither says so
- **F10** — `qa-ansible.bash:50`'s fail-fast regex accepts `yes` for
  `ignore_errors` but not for `ignore_unreachable`, and only `false` (not `no`)
  for `failed_when`. Both spellings are valid YAML booleans, so
  `failed_when: no` earns `✓ ansible: fail-fast patterns OK` — a green tick on
  the repo's #1 rule. The asymmetry sits inside a single regex
- **F11** — `CLAUDE/QA.md` says "ALWAYS and ONLY use `./scripts/qa-all.bash`"
  and "NEVER use individual scripts directly", then documents
  `qa-helper-tests.bash` and `helpers.gnome.check_extension_compat` as gates.
  `qa-all.bash` runs neither. Following the stated rule, a `helpers/` change
  gets `✓ QA passed` with its unit suite never run

## Tasks

### Phase 1: The secret scanner (public-repo safety)

- [x] ✅ **Task 1.1**: `--diff-filter=ACMRT` so renames and typechanges are
  scanned; `--name-only` yields the new path, verified rather than assumed
- [x] ✅ **Task 1.2**: Make the email whitelist **per-token**: mask the bracketed
  placeholder shapes, extract each email-like token, and filter the tokens. A
  line survives only if some token is not whitelisted
- [x] ✅ **Task 1.3**: `acceptance.bash` — 4 checks driving the real hook in a
  throwaway repo. Verified to FAIL against the unfixed hook, not merely to pass
  against the fixed one
- [ ] ⬜ **Task 1.4**: Apply the same per-token treatment to the `/home/` and
  credential whitelists, which have the identical line-granularity shape
- [ ] ⬜ **Task 1.5**: Harvest the plural `_accounts` convention (F5)
- [ ] ⬜ **Task 1.6**: **Give `commit-msg` the same denylist as `pre-commit`**
  (F8). Highest remaining security item: a commit message is permanent, so this
  is the one leak route with no remedy after the fact. Extract the denylist
  builder both hooks can share rather than duplicating it

### Phase 1b: The CCY integrity gate covers one file of seven

- [ ] ⬜ **Task 1.7**: Extend the version-bump gate and the runtime hash to the
  `lib/` files (F7) — the hash should cover the launcher **and** everything it
  sources, and the commit gate should require a bump when any of them changes.
  22 commits have already shipped lib-only behaviour changes under an unchanged
  version, so this also needs a decision about whether to bump once for the
  accumulated drift

### Phase 2: The QA gates

- [ ] ⬜ **Task 2.1**: `qa-python.bash` — shebang-based discovery independent of
  the execute bit, plus a tracked-file coverage assertion, mirroring
  `qa-shell-discovery.bash`. Exit 2 on shortfall and on zero discovery
- [ ] ⬜ **Task 2.2**: Fix the 31 findings the widened gate then surfaces
- [ ] ⬜ **Task 2.4**: `qa-ansible-syntax.bash` — discover every file with a
  top-level `- hosts:` rather than hardcoding `playbooks/imports`, add a zero
  guard, and reconcile its population with `qa-ansible.bash`'s (F9)
- [ ] ⬜ **Task 2.5**: Make the fail-fast regex accept both YAML boolean
  spellings on every directive (F10), and decide whether `qa-all.bash` should
  run the two documented-but-unrun gates or `CLAUDE/QA.md` should stop claiming
  it is the only command needed (F11)
- [ ] ⬜ **Task 2.3**: `qa-deployed-drift.bash` — resolve the deployed name from
  the play's `dest:` rather than the basename, and fail rather than skip when a
  repo file maps to no deployed name. A `.j2` source needs its rendered output
  compared, or an explicit documented exclusion — not a silent pass

### Phase 3: Close out

- [ ] ⬜ **Task 3.1**: `qa-reviewer` over the full diff
- [ ] ⬜ **Task 3.2**: Mark Complete, move to `Completed/`, update the README

## Success Criteria

- [ ] A `git mv` + edit carrying a real address is rejected
- [ ] A real address beside `git@github.com` is rejected; a line whose only
  match is whitelisted still passes
- [ ] `qa-python.bash` covers every tracked repo-owned Python file and fails
  loudly on a shortfall
- [ ] Every fix has a gate that fails against the unfixed code
- [ ] `./scripts/qa-all.bash` passes

## Risks & Mitigations

| Risk                                                                            | Impact | Probability | Mitigation                                                                                                                     |
| ------------------------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------ |
| A scanner fix over-corrects into false positives, training people to bypass it  | H      | M           | Check 3 exists solely to catch that, and did — the first per-token draft aborted the hook on clean input                       |
| A real project identifier is **already published** on `origin/F44` (Plan 00062) | M      | Confirmed   | Out of scope here and the owner's decision; a follow-up commit does not remove it from history. Flagged, not silently scrubbed |
| Widening `qa-python.bash` fails CI on 31 pre-existing findings                  | M      | H           | Task 2.2 fixes them in the same plan; the gate is not widened and left red                                                     |

## Delivery & Milestones

- Phase 1 Tasks 1.1–1.3 delivered with `acceptance.bash` proving both fixes

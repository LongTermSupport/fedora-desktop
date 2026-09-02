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
  match. That is Plan 00094's failure on the one file in scope it cannot see
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
- **F12** — F8 is not hypothetical. Building the denylist and scanning history
  found a **private account alias published on `origin/F44` in three places**:
  two tracked files (`00049-full-repo-audit/research/security.md:188`,
  `00065-…/PLAN.md:117`) and **the commit message of `fc20c5c9`**. The message
  is the F8 hole exactly: the identical string in a staged *file* would have
  been rejected by `pre-commit`'s denylist. The alias was in the denylist all
  along under `github_accounts` — `commit-msg` simply never consulted it.
  Rewriting published history is out of scope here (see Non-Goals); the two
  files are now un-editable without scrubbing, which is the gate working
- **F13** — widening the harvest to plural fields is measurably safe, not
  merely plausible: against the real `localhost.yml` it takes the denylist from
  **8 to 10 tokens**, and the two new ones appear in **zero** tracked files and
  **zero** of the last 300 commit messages. A short, common-word token would
  have blocked every future commit, so this was checked rather than assumed
- **F14** — **a coverage LOSS can hide inside a rising count.** Rewriting
  `qa-ansible-syntax.bash` to derive its population from `- hosts:` dropped
  `playbooks/playbook-main.yml`, which contains no play of its own — and the
  reported total went **78 → 79**, reading as a clean gain. The same defect
  class wearing the opposite sign: every previous instance was a number that
  looked complete, this was a number that looked *improved*. Caught only by
  listing the population instead of trusting the total, which is now what the
  gate's own pass line does
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
- [x] ✅ **Task 1.4**: Per-token treatment for the `/home/` and credential
  whitelists too — same line-granularity shape, in both hooks
- [x] ✅ **Task 1.5**: Harvest the plural `_accounts` convention (F5). Measured
  against the real `localhost.yml`: 8 → 10 tokens, the two new ones appearing
  in **zero** tracked files, so the widening blocks nothing that exists
- [x] ✅ **Task 1.6**: **`commit-msg` now runs the same denylist as
  `pre-commit`** (F8), via a shared `lib/secret-scan.bash` that both hooks
  source — extracted rather than duplicated, because duplication is how the two
  drifted apart in the first place. `play-git-hooks-security.yml` verifies the
  library exists, since neither hook can run without it
- [x] ✅ **Task 1.6b**: Extend `acceptance.bash` to 9 checks and add
  `--hooks-dir`, so "these checks fail against the unfixed code" is re-runnable
  rather than asserted. Measured at `0369468b~1`: **6 of 9 fail**

### Phase 1b: The CCY integrity gate covers one file of seven

- [x] ✅ **Task 1.7**: The version-bump gate and the runtime hash now cover the
  launcher **and** the six libraries it sources (F7). `CCY_LIBS` is declared
  once and drives the presence check, the load order and the hash, so those
  three cannot disagree about what "the CCY script" is. Verified: an identical
  lib-only edit moves the new hash and leaves the old formula unchanged.
  Accumulated drift is settled by the 3.41.0 bump itself — every saved config
  reconfigures once on a version change, which is the normal upgrade path

### Phase 2: The QA gates

- [x] ✅ **Task 2.1**: `qa-python.bash` discovers by shebang independent of the
  execute bit and asserts its coverage against the tracked set, exit 2 on a
  shortfall and on zero discovery. The library is now shared by all three source
  gates and renamed `scripts/qa-discovery.bash`; the two languages keep separate
  exclusion lists over one mechanism, because unifying them would have dropped
  `.claude/ccy/claude-supervise.py` from the gate. **35 → 41 files**, bash
  unchanged at 165
- [x] ✅ **Task 2.2**: All 31 findings fixed — 4 F401 (verified genuinely unused,
  not availability probes), 9 F541, 10 E402 fixed at source by moving a constant
  below the imports. The remaining 8 E402 are `gi.require_version()` before
  `from gi.repository import …`, which PyGObject *requires*; scoped to that one
  file in `ruff.toml`'s `per-file-ignores`, not an inline suppression this repo
  blocks and not a global disable
- [x] ✅ **Task 2.4**: `qa-ansible-syntax.bash` derives its population from the
  whole repo — a top-level `- hosts:` **or `- import_playbook:`** — with a zero
  guard, and its pass line now states the breakdown rather than a bare count
  (F9). **78 → 80.** The `import_playbook` marker is not a nicety: deriving from
  `- hosts:` alone dropped `playbook-main.yml`, and the count went 78 → 79 and
  read as a gain. See F14
- [x] ✅ **Task 2.5**: One spelling list drives every fail-fast directive, so
  `failed_when: no` and `ignore_unreachable: yes` are caught (F10) — with a
  trailing `\b`, without which `no` matched inside `not` and produced 10 false
  positives on legitimate probes. And `qa-all.bash` now runs the two
  documented-but-unrun gates (F11): running them is what makes this repo's own
  "ALWAYS and ONLY use `qa-all.bash`" instruction true, rather than softening the
  instruction to match the gap
- [x] ✅ **Task 2.3**: `qa-deployed-drift.bash` no longer relies on the basename
  matching. A `.j2` is checked against a real playbook `dest:` under its stripped
  name — **exit 2 if none exists**, since a template mapping to no deployed name
  is a file the gate would pass over without comparing anything. A rendered
  template genuinely cannot be byte-compared, so it is **disclosed** rather than
  silently skipped, and the pass line now also states how many scripts are not
  installed on this host. Exercised against a fake host: pass discloses 2
  compared / 35 not installed / 1 template; drift still exits 1; an unmapped
  template exits 2

### Phase 3: Close out

- [ ] ⬜ **Task 3.1**: `qa-reviewer` over the full diff
- [ ] ⬜ **Task 3.2**: Mark Complete, move to `Completed/`, update the README

## Success Criteria

- [x] A `git mv` + edit carrying a real address is rejected
- [x] A real address beside `git@github.com` is rejected; a line whose only
  match is whitelisted still passes
- [x] A commit **message** is held to the same denylist as a staged file
- [x] Widening the harvest is shown not to block existing content
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
- Phase 1 complete (Tasks 1.4–1.6): one scanner in `lib/secret-scan.bash`,
  sourced by both hooks. `acceptance.bash` is now 10 checks with `--hooks-dir`,
  so the "fails against the unfixed code" claim is re-runnable: 7 of 10 fail at
  `0369468b~1`

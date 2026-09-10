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

F1–F14, each reproduced with the command that produced its figures, are in
[FINDINGS.md](FINDINGS.md): the rename blind spot, the whole-line whitelist,
`commit-msg`'s missing denylist, the CCY hash covering one file of seven, the
Python gate inheriting 00076's executable-bit defect, the coverage LOSS that hid
inside a rising count, and the published-identifier inventory.

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
- [x] ✅ **Task 1.6b**: Add `--hooks-dir` to `acceptance.bash`, so "these checks
  fail against the unfixed code" is re-runnable rather than asserted. **Current
  figures, re-measured 2026-09-10: 13 checks, 13 pass against HEAD, and 8 fail
  against `0369468b~1` — 1, 2, 5, 6, 7, 9, 10, 11.** Checks 3, 8, 12 and 13 pass
  in both states by design; they guard against the fixes over-correcting into
  false positives. The suite now asserts its own COVERAGE line against
  `PASS+FAIL`, so that line cannot outlive the suite. This task previously
  carried "9 checks / 6 of 9" and Delivery carried "10 checks / 7 of 10", both
  progress notes left in a document that is supposed to state current state — in
  a plan whose thesis is that stale numbers mislead. **The script's own output is
  the source of truth; do not restate a count here.**

### Phase 1b: The CCY integrity gate covers one file of seven

- [x] ✅ **Task 1.7**: The version-bump gate and the runtime hash now cover the
  launcher **and** every library in `lib/` (F7). **Corrected 2026-09-10, CCY
  3.49.2**: this task originally said "the six libraries it sources" and used
  `CCY_LIBS` — the hand-written load-order list — as the hashed set. That list
  was one file short of the directory on the day it was written, because
  `common-pure.bash` is sourced by `common.bash` rather than by the launcher, so
  editing it left the hash unchanged for eight releases. The `pre-commit` half of
  the same fix globbed `lib/*.bash` and did cover it, so the two halves
  disagreed. The hash now derives its own set from the directory; `CCY_LIBS`
  keeps load order and the presence check. **Replacing a stale enumeration with a
  fresher enumeration was not the fix** — see `CLAUDE/AgentNotes.md` row 9b.
  Verified: an identical lib-only edit moves the new hash and leaves the old formula unchanged.
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
  whole repo — a top-level `- hosts:` **or `- import_playbook:`** — and its pass
  line states the breakdown rather than a bare count (F9). The `import_playbook`
  marker is not a nicety: deriving from `- hosts:` alone dropped
  `playbook-main.yml` and the reported count *rose*, reading as a gain. See F14.
  **No population figure is recorded here on purpose** — one was, and it went
  stale; the gate prints the live number every run.
  - [x] ✅ **2026-09-10**: the zero guard was only half the fix. Added the
    PARTIAL-coverage guard the three source gates already had — every tracked
    YAML under `playbooks/` must be in the population, a yardstick derived
    independently of the content marker so narrowing the marker breaks it.
    Verified by re-introducing F14: it now names `playbook-main.yml` and exits 2.
- [x] ✅ **Task 2.5**: One spelling list drives every fail-fast directive, so
  `failed_when: no` and `ignore_unreachable: yes` are caught (F10) — with a
  trailing `\b`, without which `no` matched inside `not` and produced 10 false
  positives on legitimate probes. And `qa-all.bash` now runs the two
  documented-but-unrun gates (F11): running them is what makes this repo's own
  "ALWAYS and ONLY use `qa-all.bash`" instruction true, rather than softening the
  instruction to match the gap
  - [x] ✅ **2026-09-10**: F10's own fix had no gate — reverting the spelling list
    turned nothing red anywhere. `scripts/test-qa-ansible-failfast.bash` drives
    the definitions **read out of** `qa-ansible.bash`, 18 cases including the
    `not`/`no` over-match negative control. Re-introducing F10 fails 5 of them.
    Also closed the asymmetry one directive along: `ignore_unreachable` was
    checked for booleans but not the templated `"{{ … }}"` form.
  - [x] ✅ **2026-09-10**: F11's generalisation had stopped at the two gates
    `QA.md` happened to list. `test-ccy-rootless-guard.bash` ran in CI and
    **nowhere locally** — worse than documented-but-unrun, because a green
    `qa-all.bash` could be a red CI. Wired in, and the duplicate CI job removed
    so the two cannot diverge again
- [x] ✅ **Task 2.3**: `qa-deployed-drift.bash` no longer relies on the basename
  matching. A `.j2` is checked against a real playbook `dest:` under its stripped
  name — **exit 2 if none exists**, since a template mapping to no deployed name
  is a file the gate would pass over without comparing anything. A rendered
  template genuinely cannot be byte-compared, so it is **disclosed** rather than
  silently skipped, and the pass line now also states how many scripts are not
  installed on this host. Exercised against a fake host: pass discloses 2
  compared / 35 not installed / 1 template; drift still exits 1; an unmapped
  template exits 2
  - [x] ✅ **2026-09-10**: the `.j2 → dest:` assertion is a pure source-tree
    invariant that sat **behind three host-availability early exits**, so it never
    ran in the CCY container where it was written, nor in CI. Moved above them;
    an unmapped template now exits 2 in this container

### Phase 3: Close out

- [x] ✅ **Task 3.1**: `qa-reviewer` over the full diff, 2026-09-10. Verdict
  **BLOCK** — 2 blocking, 6 fix-before-merge, 10 nits. Every one resolved; each
  fix verified by reverting it and watching something go red. Report:
  [subagent-reports/260910-qa-review-00081-opus-5.md](subagent-reports/260910-qa-review-00081-opus-5.md)
- [ ] ⬜ **Task 3.2**: Mark Complete, move to `Completed/`, update the README
  index + statistics in the same commit. `CLAUDE/Plan/README.md`'s row still says
  "remaining phases cover `qa-python.bash` and `qa-deployed-drift.bash`" — both
  done — so rewrite it in that commit

## Success Criteria

- [x] A `git mv` + edit carrying a real address is rejected
- [x] A real address beside `git@github.com` is rejected; a line whose only
  match is whitelisted still passes
- [x] A commit **message** is held to the same denylist as a staged file
- [x] Widening the harvest is shown not to block existing content
- [x] ✅ `qa-python.bash` covers every tracked repo-owned Python file and fails
  loudly on a shortfall. **Demonstrated, not asserted**: simulating the pre-fix
  discovery (shebang branch requiring `-executable`) makes the guard report
  6 missed files and exit 2. The `.j2` the exclusion list hid — 349 lines with a
  Python shebang — is now rendered and compiled too
- [x] ✅ Every fix has a gate that fails against the unfixed code. The three that
  had none (F9, F10, F4) now do, and each was proved by re-introducing the
  original defect
- [x] ✅ `./scripts/qa-all.bash` passes

## Risks & Mitigations

| Risk                                                                            | Impact | Probability | Mitigation                                                                                                                     |
| ------------------------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------ |
| A scanner fix over-corrects into false positives, training people to bypass it  | H      | M           | Check 3 exists solely to catch that, and did — the first per-token draft aborted the hook on clean input                       |
| A real project identifier is **already published** on `origin/F44` (Plan 00062) | M      | Confirmed   | Out of scope here and the owner's decision; a follow-up commit does not remove it from history. Flagged, not silently scrubbed |
| Widening `qa-python.bash` fails CI on 31 pre-existing findings                  | M      | H           | Task 2.2 fixes them in the same plan; the gate is not widened and left red                                                     |

## Delivery & Milestones

- Phase 1 Tasks 1.1–1.3 delivered with `acceptance.bash` proving both fixes
- Phase 1 complete (Tasks 1.4–1.6): one scanner in `lib/secret-scan.bash`,
  sourced by both hooks, with `--hooks-dir` so the "fails against the unfixed
  code" claim is re-runnable rather than asserted. Figures live in Task 1.6b and
  in the script's own COVERAGE line, not here

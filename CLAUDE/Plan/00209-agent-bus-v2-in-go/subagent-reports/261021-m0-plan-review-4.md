# Plan 00209 M0: review round 4 (final check of the round-3 fixes)

Reviewed: tip `ef5d3acd` on `feat/bus-v2-m0-plan`, rebuilt onto `e918261b`. Scope: the two
round-3 findings, defects the fix brought in, install-specific names in the plan folder, and
whether the owner's new direction is recorded.

**Verdict: FIX-FIRST** (two small findings, both bookkeeping; the round-3 findings are fixed)

## Round-3 findings

1. **Who answers D4 and D15: fixed.** The Owner decisions preface now says D4 and D15 are the
   two that wait and that the coordinator answers each, by confirming the default or by taking
   it to the owner. Only the merge to F44 waits; spec, build and review go ahead. The D15 row,
   the 00194 2.1/2.1b/2.2 row, Task 3.1, Task 7.2 and DESIGN.md's closing paragraph all say
   the same thing: D4 is answered before Task 3.1 merges (stage 2's hooks) and D15 before 6b
   (Task 7.2) merges. No stale "ships" or "starts" wording is left for these two.
2. **Journal decision and handoff entries: fixed.** `JOURNAL/00209-Journal-26-10-21.md` has
   `16:43 · decision · T1.4` and `16:43 · handoff · P1`. Both stamps match the commit time in
   UTC (17:43 +0100), which is what mkplan.bash writes. Task 1.4 exists and is ✅. Nothing
   earlier in the journal was edited, so it stays append-only.

## New findings

1. **The handoff entry is wrong after the rebuild.** It says the owner must decide on
   rewriting the history of `35acceab` "before any branch that holds it is pushed or merged,
   this branch included". `git merge-base --is-ancestor 35acceab HEAD` fails: the rebuilt
   branch no longer holds that commit. It is still held by `feat/agent-bus-v2-design-2`,
   `feat/bus-v2-m0-plan-OLD-ds2`, and every `feat/bus-v2-m1-*`, `feat/bus-v2-m2-hooks*` and
   `feat/bus-v2-s1-declare` branch. The coordinator would be acting on a false fact. Fix: add
   a `correction` entry through `mkplan.bash --journal` pointing at the 16:43 handoff. It should
   say this branch is clean, and that the M1, M2 and S1 work branches still carry `35acceab`
   and need rebasing onto `e918261b` (or a history rewrite) before they are pushed or merged.
   Do not edit the 16:43 entry itself.
2. **The owner's new direction is missing.** No file in the plan folder records "the Python
   is a prototype; the Go fixes and improves; only the compatibility contract binds". A grep
   for prototype, compatibility contract and fixes-and-improves finds nothing. PLAN.md still
   reads "no loss of behaviour" and "carried as expectations". Without the direction, a
   builder treats every Python quirk as binding. Fix: put the direction in PLAN.md, in the
   Owner decisions preface or the goal. Say what the compatibility contract is (wire protocol
   1 per D5, the v1 files the Go hook reads, the CLI surface that programs call) and that
   behaviour outside it may change. Add a `decision` journal entry through mkplan.bash.

## Install-specific names

The plan folder is clean, BUILD-BRIEF.md included. The diff from `35acceab` to `e918261b`
replaced the team name and the coordinator's seat handle with `<team>` and `<coordinator>`.
No seat handle, the old team name, a `local.podman` suffix, a hostname, an email or a home
path occurs anywhere in the folder or in the branch's first-parent commit messages since
`e918261b`. The only `fedora-desktop` hits are `fedora-desktop-plays.lock`, the repository's
own name. `**Owner**: joseph` in PLAN.md follows the repo-wide convention that review 2
recorded, so it is not a finding.

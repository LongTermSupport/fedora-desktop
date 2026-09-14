# Plan 00117: the acceptance lab certifies a commit with a checker from a different one

**Status**: Not Started
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

Run `20260914T100220Z-desktop-fresh-install` reported **pass, 16/16** against commit
`d307ed28`, and one of those sixteen had judged almost nothing:

```
VMTEST-CHECK pass deployed-extensions-active COVERAGE: 8 of 1 declared ACTIVE (8 on disk)
```

Nine extensions are declared. The check expected **one**, found eight active, and passed.

The cause is that `vmtest` copies the guest checker from the **host's deployed copy**
(`~/.local/share/vmtest/guest-acceptance-<profile>.bash`, `files/home/.local/bin/vmtest:869`
and `:958`), never from the guest's own checkout — even though the guest is provisioned at the
commit under test and carries that file. The host's copy was last deployed before `d307ed28`.
That older checker derived its expected count by grepping the **play** for `id: <n>` lines;
Plan 00112 moved those declarations into `vars/gnome-shell-extensions.yml`, so the grep matched
nothing and `expected` collapsed from 8 to `0 + 1`.

So a commit changed the repo in a way that **silently weakened the yardstick still being used
to certify it**, and the run went green. This is the repo's cardinal defect — a check that
cannot fail — reached by a route none of the existing guards cover:

- `scripts/qa-deployed-drift.bash:204-209` **does** compare that exact glob and would have
  caught it — but it is host-only, skips in the CCY container, and nothing runs it before a
  bridge request.
- `recipe_digest_now` (`vmtest:163`) hashes the cleanup script, the kickstart and the session
  runner, so a stale one of those invalidates the base. The acceptance checker is not in it,
  and correctly so: that digest is about how a **base** is built. Nothing else covers the gap
  it leaves.
- The run records `repo_commit`, implying the whole run is at that commit. The checker's own
  version is recorded nowhere, so the divergence is invisible in the evidence too.

Every acceptance run has this property. Nothing suggests an earlier run was wrong —
`20260914T085408Z` reported `8 of 8` because host and repo agreed then — but no run's verdict
currently carries the evidence to say so.

## Goals

- An acceptance run whose checker differs from the commit under test **fails as a harness
  failure**, naming both versions — never reports a product verdict.
- Every run records the checker's identity as evidence, so an archived run can be re-judged.
- The guard reaches the container-side bridge, where `qa-deployed-drift.bash` cannot run.

## Non-Goals

- **Moving the checker's source of truth into the guest.** The host copy stays authoritative on
  purpose: a checker taken from the tested commit lets a commit weaken its own acceptance test
  with nothing to notice. The fix is to make divergence loud, not to pick the other side.
- Re-verifying every past run. Only the runs a live plan's verdict rests on are re-judged.
- Adding the checker to `recipe_digest_now`. It is not part of base construction, and putting
  it there would rebuild every base on an unrelated edit.

## Tasks

### Phase 1: The gate

- [ ] ⬜ **Task 1.1**: Compare the deployed checker against the guest checkout's copy of the
  same path, before the checker runs. The guest is provisioned at the tested commit and has
  the file, so this needs neither network nor repo access on the host
- [ ] ⬜ **Task 1.2**: On a mismatch, fail as **harness**, not product — Plan 00110 already
  distinguishes the two, and this is squarely a harness fault. The message names both hashes
  and the remedy: re-run `play-vm-test-lab.yml` on the HOST
- [ ] ⬜ **Task 1.3**: Record `acceptance_script_sha256` as run evidence unconditionally, so a
  run can be judged from its archive rather than from an assumption about the host
- [ ] ⬜ **Task 1.4**: Prove the gate can fail — deploy a deliberately altered checker, run a
  scenario, confirm it aborts as a harness failure, then restore. A gate nobody has watched
  fail is a comment

### Phase 2: Close the container-side hole

- [ ] ⬜ **Task 2.1**: `scripts/vmtest-request.bash` cannot run `qa-deployed-drift.bash` — it
  has no deployed copies to compare. The host-side bridge responder can, and is the only side
  that can. Refuse a `run-scenario` with a named reason rather than starting an hour-long run
  whose verdict cannot mean anything
- [ ] ⬜ **Task 2.2**: The `vmtest-manifest` gate runs everywhere. Consider asserting there that
  every `guest-acceptance-*.bash` the lab play deploys exists in the repo, so a rename cannot
  silently orphan one

### Phase 3: Re-judge what rests on a stale run

- [ ] ⬜ **Task 3.1**: Plan 00112 Task 2.2 rests on `deployed-extensions-active` being green.
  Both `20260914T100220Z` (`d307ed28`) and `20260914T110446Z` (`be73d3b0`) used the stale
  checker and both reported `8 of 1` — a prediction made before the second finished, which is
  the cheapest confirmation there was. Neither certifies it. Re-run once the host has
  redeployed, and record which run finally did
- [ ] ⬜ **Task 3.2**: QA, then `qa-reviewer` over the diff

## Success Criteria

- [ ] A run with a mismatched checker **aborts as a harness failure**, demonstrated by
  deliberately breaking it — not asserted
- [ ] Every run's evidence names the checker version that produced its verdict
- [ ] A bridge `run-scenario` request is refused, with a reason, when the lab is stale
- [ ] `deployed-extensions-active` reports the full declared population again — 9, not 1
- [ ] `./scripts/qa-all.bash` passes

## Dependencies

- **Fixes a defect in**: Plan 00110 (VM lifecycle acceptance testing, Complete) — its lab is
  the subject; this does not reopen that plan
- **Blocks**: Plan 00112 Task 2.2, which cannot be certified until the checker matches
- **Do not duplicate**: `scripts/qa-deployed-drift.bash` already compares this glob on the
  HOST. This plan closes the paths that reach an acceptance run without it

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00117-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->

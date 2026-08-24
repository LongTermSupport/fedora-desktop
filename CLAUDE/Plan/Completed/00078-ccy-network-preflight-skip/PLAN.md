# Plan 00078: ccy network preflight skip

**Status**: Complete (`fb3a2d1`)
**Created**: 2026-08-19
**Owner**: joseph
**Priority**: Medium

## Overview

`ccy`'s launch-time internet-reachability preflight (`claude-yolo:2669-2762`) runs
unconditionally on every launch with a network attached: `podman run --rm --network <net> alpine wget http://google.com`, hard-failing with `exit 1` if it does not succeed. That needs an
unpinned `alpine` pull and plain-http egress to `google.com` — both denied by design on a
tightly egress-fenced host such as a self-hosted GitHub Actions runner VM, which proves its
own egress posture independently before ever invoking `ccy` and has no reason to grant a
generic liveness probe requirements its own provisioning does not otherwise need.

This is the one hard blocker `lts-infra` Plan 00030 (runner CI dispatch via real `ccy`) recorded
in its Open Question 2, and the same problem fedora-desktop's own unmerged Plan 00068
(`plan-00066-ccy-ci-runner` branch, 90 commits ahead of `F44`, never merged) chose to solve by a
different route — Decision 8: give `ccy` unfettered egress at launch, which defuses rather than
fixes the preflight (both requirements simply start succeeding). This plan takes the narrower
route instead: make the preflight itself skippable, so a fenced host never has to widen its
egress just to satisfy one launcher-internal liveness check. It does not resolve or replace
Decision 8's broader question of what a *running* `ccy` session should be allowed to reach on
such a host — only the one launch-time gate this plan is scoped to.

## Goals

- A caller that has already proven its own network egress by other means can launch `ccy`
  without also satisfying the built-in preflight's `alpine`-pull + `google.com` requirements.
- No behaviour change for any existing caller that does not opt in.
- Consistent with the existing `CCY_SKIP_TOKEN_OWNER_CHECK` idiom (`lib/ssh-handling.bash`) —
  a named, explicit, single-purpose opt-out, not a general trust switch.

## Non-Goals

- Does **not** implement fedora-desktop Plan 00068's Decision 8 (open egress at launch) — that
  remains a live, separate, larger decision, unresolved by this plan.
- Does **not** touch `ccy`'s other launch-time assumptions (GH_TOKEN derivation, MCP/tool
  restriction, compose/network auto-detection) — see Plan 00068 for that work.
- Does **not** deploy `ccy` itself onto the `lts-infra` runner or wire up dispatch — that is
  `lts-infra` Plan 00030's Phase 1/2, still unbuilt.

## Tasks

### Phase 1: the skip flag

- [x] ✅ **Task 1.1**: Guard the preflight block (`claude-yolo:2669-2762`) on
  `CCY_SKIP_NETWORK_PREFLIGHT` unset, print a one-line confirmation when skipped, and surface the
  escape hatch in the existing failure-path debugging list (as item 6, alongside the other
  reachability fixes already listed there).
- [x] ✅ **Task 1.2**: Document it — `docs/ccy.md` troubleshooting table, `docs/ccy-changelog.md`
  entry, `CCY_VERSION` bump (3.38.0 → 3.39.0) with a short pointer comment rather than growing the
  in-line changelog further (the changelog file exists precisely because that comment once reached
  5,645 characters).
- [x] ✅ **Task 1.3**: Verify — `bash -n` clean; `shellcheck -s bash` and `shellcheck -x -s bash`
  both clean, zero new findings (all pre-existing findings are unrelated lines, confirmed by line
  number). `./scripts/qa-all.bash` could not be run to completion in this environment — its
  `qa-bash.bash` stage reports "file discovery found 0 bash files under
  /workspace/untracked/repos/fedora-desktop" despite `git ls-files '*.bash'` finding 74 tracked
  files from the same working directory, an unrelated, pre-existing environment issue not
  diagnosed further here (recorded, not silently skipped — see JOURNAL).

## Success Criteria

- [x] `CCY_SKIP_NETWORK_PREFLIGHT=1 ccy` bypasses the reachability check entirely (verified by
  reading the guarded control flow; not yet exercised against a live fenced host — that proof
  belongs to `lts-infra` Plan 00030 once it actually deploys `ccy` to the runner).
- [x] Every existing caller's behaviour is unchanged (the new branch is `elif`-gated after the
  opt-in check, so the default path is byte-identical to before).

## Delivery & Milestones

- `fb3a2d1` — CCY 3.39.0, `CCY_SKIP_NETWORK_PREFLIGHT` added.

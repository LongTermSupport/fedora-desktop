# Plan 00089: ssh handling runner token guard

**Status**: In Progress
**Created**: 2026-08-25
**Owner**: joseph
**Priority**: Medium

## Overview

`lib/ssh-handling.bash`'s `build_ssh_mounts_and_validate()` has a fallback branch
(no SSH key selected / no GitHub username detected) that ran
`GH_TOKEN="$(gh auth token 2>&1)"` unconditionally. The initial premise was that
this clobbers a `GH_TOKEN` a caller already exported before invoking `ccy` —
exactly the shape a CI runner launching with `--no-ssh` and a pre-set token
needs.

**Correction (`qa-reviewer`, before this landed): that premise was wrong.**
Measured: `gh` gives an exported `GH_TOKEN` env var precedence over its own
stored credentials, so `GH_TOKEN=x gh auth token` already echoes back `x`
(`rc=0`, no `gh auth login` needed). The runner path worked before this
change — there was no live clobbering bug. The change still ships, on the
reviewer's own recommendation, but reframed: `gh`'s GH_TOKEN precedence is
documented (`gh help environment`), not a hidden quirk — the value is making
it **explicit** and self-documenting in our own code rather than relying on
the reader knowing gh's precedence rules, and it drops the dependency on a
successful `gh auth token` call for this path entirely (no local `gh` login
state needed — the caller's token is used directly). `gh` itself must still
be installed; an earlier, unconditional `command_exists gh` check in the same
function still gates this path regardless of this change.

This is a carve-out from `CLAUDE/Plan/00068-ccy-ci-runner-variant`, which
specified a full CI-runner "token-first" identity but lives only on the
unmerged `plan-00066-ccy-ci-runner` branch. **Correction of an earlier,
imprecise claim**: `files/var/local/claude-yolo/` is NOT untouched on that
branch — `git diff --stat origin/F44 origin/plan-00066-ccy-ci-runner -- files/var/local/claude-yolo/` shows 10 files differing, `lib/ssh-handling.bash`
among them (128 lines). Read in full
(`git diff origin/F44 origin/plan-00066-ccy-ci-runner -- files/var/local/claude-yolo/lib/ssh-handling.bash`): every hunk there is the
branch's copy *missing* F44-only robustness work added since the fork
(`resolve_token_owner_login()`'s retry/validate logic, CCY 3.36.0) — it is
behind, not a competing implementation, and adds no token-first logic of its
own to this function. So the narrower claim that matters still holds: no
code on the branch does what this plan's fix does. Whether this file ends up
a genuine merge conflict in Plan 00090 (my new `elif` sits next to a hunk
both sides touched differently) is noted there, not assumed away here.
Rather than block this narrow fix behind a 90-commit branch resync, it lands
directly on `F44` now; the resync (tracked separately) will absorb Plan
00068's fuller design and should note this fix already landed here.

The container-side half was checked, not assumed: `entrypoint.sh` only
cross-checks `GITHUB_USERNAME` against the authenticated token when
`GITHUB_USERNAME` is non-empty (`if [ -n "$GITHUB_USERNAME" ]; then ...`,
line 64) and otherwise just requires `GH_TOKEN` to be set (line 31). A
runner with no SSH key leaves `GITHUB_USERNAME` empty by construction, so the
entrypoint already accepts a bare token with no matching changes needed there
— this is a single-file fix.

## Goals

- A caller-supplied `GH_TOKEN` is used directly by
  `build_ssh_mounts_and_validate()`'s no-SSH-key fallback branch, without
  routing through `gh auth token` — explicit precedence in our own code, not
  a fix for a live defect (see Overview correction).
- No change in existing behaviour when `GH_TOKEN` is NOT pre-set (the
  `gh auth token` fallback still runs exactly as before).

## Non-Goals

- Full CI-runner "token-first" identity work (multi-account mapping, push
  support with no SSH key at all) — that is Plan 00068's scope, absorbed by
  the branch resync, not this plan.
- Any change to `entrypoint.sh` — confirmed unnecessary above.
- Fixing `discover_and_select_ssh_keys()`'s interactive no-keys-found prompt
  hanging without a TTY — out of scope; a runner using this path passes
  `--no-ssh`, which skips that function entirely (`claude-yolo:931`).

## Tasks

### Phase 1: Guard the token fallback

- [x] ✅ **Task 1.1**: Add a guard in `build_ssh_mounts_and_validate()`'s
  no-username fallback so it only calls `gh auth token` when `GH_TOKEN` is
  not already set by the caller.
  - [x] ✅ Edit `files/var/local/claude-yolo/lib/ssh-handling.bash`
  - [x] ✅ Bump `CCY_VERSION` in `files/var/local/claude-yolo/claude-yolo`
    (lib change — same program, per `CLAUDE/ContainerRules.md`)
  - [x] ✅ Run QA: `./scripts/qa-all.bash`
  - [x] ✅ Run the `qa-reviewer` agent over the diff — first pass returned
    **FIX-BEFORE-MERGE**: the "clobbering bug" premise was empirically wrong
    (see Overview correction); two secondary findings (`gh` binary still
    required; a working-tree-only README row at review time).
  - [x] ✅ Fix findings — rewrote every place asserting the false premise as
    fact (this file, `JOURNAL/`, `lib/ssh-handling.bash` comments, the
    `CCY_VERSION` line, `docs/ccy-changelog.md`); the README row issue
    resolves itself once this plan's folder and the row commit together.
  - [x] ✅ Second `qa-reviewer` pass — still **FIX-BEFORE-MERGE**: my "branch
    touches no file under `files/var/local/claude-yolo/`" claim was itself
    false (10 files differ, `ssh-handling.bash` among them — see Overview
    correction), plus a residual "internal gh behaviour" overclaim (`gh help environment` documents the precedence). Both corrected across every
    touched file.
  - [x] ✅ Third `qa-reviewer` pass — **PASS WITH NITS**, no blockers: the
    refuted premise confirmed absent repo-wide, the `elif` confirmed
    reachable with no early return ahead of it, QA independently verified
    green, README row confirmed under the 500-char limit. Nits (both
    fixed): missing `---` separator in `docs/ccy-changelog.md`; commit only
    the 5 intended paths explicitly (this repo's working tree also carries
    unrelated pre-existing untracked/modified content — a mode-only change
    on `scripts/git-hooks/README.md` and a 708 MB `scripts/qa-ccy/` — that
    must not be swept in).
  - [ ] ⬜ (On HOST, not in CCY container) rebuild the CCY image and prove a
    `--no-ssh` launch with a pre-set `GH_TOKEN` keeps it and authenticates

## Success Criteria

- [x] ✅ Reading the code confirms `GH_TOKEN` is used directly on this path
  — mechanism-level, not yet host-proven. Framed accurately as an explicit-
  precedence / dependency-reduction change, not a bug fix.
- [x] ✅ `./scripts/qa-all.bash` passes.
- [x] ✅ `qa-reviewer` finds nothing blocking (third pass: PASS WITH NITS).
- [ ] ⬜ Live-proven on HOST: a `--no-ssh` launch with `GH_TOKEN` pre-exported
  authenticates inside the container without prompting.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00089-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->

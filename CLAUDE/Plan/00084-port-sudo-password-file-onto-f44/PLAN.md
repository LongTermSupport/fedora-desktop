# Plan 00084: port sudo password file onto F44

**Status**: In Progress
**Created**: 2026-08-24
**Owner**: joseph
**Priority**: Medium

## Overview

`run.bash`'s two headless capabilities currently live on two commits that share no
common descendant. `F44`@`68f1596` (PR #33, this repo's own Plan 00082) carries
`RUN_BASH_GITHUB_ACCOUNTS=none` but has no sudo-password fallback — `headless_preflight`
hard-requires `sudo -k -n true` (NOPASSWD:ALL) with no other path. `86ba6ae` (this
repo's own Plan 00073, `RUN_BASH_SUDO_PASSWORD_FILE`) lives only on
`origin/plan-00066-ccy-ci-runner`, an unmerged branch that diverged from `F44` before
PR #33 landed (90 commits ahead of `F44`, 176 behind) — and that branch's own
`run.bash` still explicitly rejects `GITHUB_ACCOUNTS=none`.

This was discovered downstream, in `lts-infra`'s consumer repo: its Plan 00045 built a
live proof (`verify-github-accounts-none.bash`) of the `GITHUB_ACCOUNTS=none` path, and
that estate's policy grants `NOPASSWD:ALL` to no guest — so the proof cannot run against
any real guest until both mechanisms exist on one commit. `lts-infra`'s current
production `fedora_desktop_ref` pin (`38ac3fc`) is itself sitting on the same unmerged,
diverging branch as `86ba6ae`, for the same reason.

This plan ports Plan 00073's sudo-password-file mechanism onto `F44`, on top of PR #33,
so a single future `F44` commit carries both capabilities and `lts-infra` can pin to a
commit that is both reachable from the default branch and complete.

## Goals

- `F44`'s `run.bash` supports `RUN_BASH_SUDO_PASSWORD_FILE` as an equally-supported
  alternative to `NOPASSWD:ALL` sudo, composing with `RUN_BASH_GITHUB_ACCOUNTS=none`.
- The port is reviewed and merged via PR against `F44` (not a direct push) — a
  cross-repo mechanism port onto the default branch warrants review, unlike a
  plan-local script.
- `docs/headless-provisioning.md` and `docs/headless-server-install.md` correctly
  describe both the sudo-credential choice and the `GITHUB_ACCOUNTS=none` path (the
  latter was found stale during this port — still described as "a planned follow-up"
  on `F44` despite PR #33 already shipping it).

## Non-Goals

- Does not touch `origin/plan-00066-ccy-ci-runner` itself, or resolve whatever that
  branch's own eventual fate is (merge, rebase, abandonment) — out of scope here.
- Does not bump `lts-infra`'s `fedora_desktop_ref` pin — that is `lts-infra`'s own
  Plan 00045 Task 3.1b, gated on this plan's PR landing.
- Does not re-run Plan 00073's or Plan 00082's own acceptance/host-verification work —
  this is a mechanical port of an already-reviewed, already-host-verified mechanism
  onto a new base; only the merge/composition is new.

## Tasks

### Phase 1: port the mechanism

- [x] ✅ **Task 1.1**: Diff `86ba6ae` (Plan 00073, `RUN_BASH_SUDO_PASSWORD_FILE`)
  against its parent to get the isolated feature patch (202 lines in `run.bash`, plus
  `docs/headless-provisioning.md` and `docs/headless-server-install.md`).
- [x] ✅ **Task 1.2**: Manually apply that patch onto current `F44` `run.bash` —
  a raw `git cherry-pick` conflicts (plan-folder path collision at number 00073, now a
  different plan on `F44`; and `run.bash` content conflicts from PR #33's own nearby
  edits) — merged by hand: version header/changelog, the new `_sudo`/
  `hl_sudo_askpass_start`/`hl_sudo_probe_password` helpers, the `headless_preflight`
  sudo-credential decision (composed with the existing `GITHUB_ACCOUNTS` branch,
  untouched), `main()`'s `HL_SUDO_OPTS`/`HL_SUDO_PW_FILE` init, `--help`/
  `--help-run-headless` doc text, `run_playbook_with_issue_option()`'s and the final
  main-playbook invocation's become-password-file branch, and all 14 bare
  `sudo` → `_sudo` call-site swaps.
- [x] ✅ **Task 1.3**: `docs/headless-provisioning.md`/`docs/headless-server-install.md`
  — applied Plan 00073's own doc diff cleanly (`git apply --check` passed with no
  conflict), then fixed a stale claim found adjacent to it: this doc still said the
  `GITHUB_ACCOUNTS=none` path was "a planned follow-up" and "currently fails fast" —
  false since PR #33 merged — and its secrets-required table still marked the GitHub
  token/passphrase files unconditionally required and the vault password conditional,
  the opposite of what `run.bash --help-run-headless` (the authoritative contract, per
  this doc's own header) actually says.
- [x] ✅ **Task 1.4**: Verify: `bash -n run.bash` clean; `shellcheck -x run.bash` clean,
  0 findings; `./scripts/qa-all.bash` — QA passed, 537 files checked (pre-existing
  137-issue shellcheck advisory count across the whole repo is unrelated to this
  change — `run.bash` alone is 0 findings standalone).

### Phase 2: land it

- [x] ✅ **Task 2.1**: Committed (`f38db91`) on branch `plan-00082-port-sudo-password-file`
  off `F44`, pushed, opened as PR #34 against `F44`.
- [ ] ⬜ **Task 2.2**: Address review feedback; merge. Independent review verdict:
  MERGE-READY conditional on one doc fix (`docs/headless-server-install.md` prerequisites
  table still said GitHub was unconditionally mandatory) — fixed. Two non-blocking notes:
  Plan 00073's `_sudo` argv acceptance harness (`_acceptance-cases.inc.bash`/
  `acceptance.bash`) was not ported since it is plan-local to the old 00073 folder; the
  reviewer independently re-derived and ran its core property (shipped `_sudo` builds
  argv with no empty leading arg when `HL_SUDO_OPTS` is empty, verified by perturbation)
  against this PR's `run.bash` and confirmed it holds — promoting that harness into a
  standing check is a good follow-up, not a merge blocker.
- [ ] ⬜ **Task 2.3**: Notify `lts-infra` Plan 00045 the blocker is closed so Task 3.1b
  (bump `fedora_desktop_ref`) can proceed against the merged commit.

## Success Criteria

- [ ] A single `F44` commit's `run.bash` supports both `RUN_BASH_GITHUB_ACCOUNTS=none`
  and `RUN_BASH_SUDO_PASSWORD_FILE`, merged via reviewed PR.
- [ ] `lts-infra`'s Plan 00045 live proof can run against a real guest without
  requiring `NOPASSWD:ALL`.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00084-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Branch `plan-00082-port-sudo-password-file` created off `F44`@`d7ccd83`; manual port
  complete, QA clean.
- Committed `f38db91`; pushed; PR #34 opened against `F44`.
- Independent review: MERGE-READY conditional on a doc fix — fixed. Not yet merged.

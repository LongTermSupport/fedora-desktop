# Plan 00090: resync ccy ci runner branch onto f44

**Status**: In Progress
**Created**: 2026-08-25
**Owner**: joseph
**Priority**: Medium

## Overview

`origin/plan-00066-ccy-ci-runner` (internally renumbered to Plan 00068 mid-flight,
commit `5e50a344`) has been diverging from `F44` since it forked
(merge-base `eb14ba27a0b00df4a2bf591ae6371a216cf9f0d1`). As of this plan's start:
`F44` carries 205 commits the branch lacks; the branch carries 90 commits `F44`
lacks. `git diff --stat origin/F44...origin/plan-00066-ccy-ci-runner` shows 107
files changed, 13523 insertions(+), 373 deletions(-) — real, substantial, mostly
already-proven work: the `CLAUDE/Plan/_planlib.inc.bash` plan-scripting library
(710 lines) and its 623-line test suite, six new `.claude/rules/*.md` pointer
files, a docs-link-checker QA gate (`helpers/docs/link_check.py` + tests), a new
`scripts/qa-docs.bash` QA stage, and several already-completed/documented plans
(00067, 00069–00074, some already moved to `Completed/` on that branch).

A `git merge-tree --write-tree origin/F44 origin/plan-00066-ccy-ci-runner`
dry-run (read-only, no refs touched) shows the branch is **not** hopelessly
diverged: of 107 changed files, only 18 conflict — 10 docs/prose
(`CLAUDE.md`, `CLAUDE/Plan/README.md`, `CLAUDE/QA.md`, `README.md`,
`docs/README.md`, `docs/architecture.md`, `docs/fast-file-manager.md`,
`docs/features/README.md`, `docs/headless-provisioning.md`,
`docs/headless-server-install.md`) and 8 code/script files
(`files/var/local/claude-yolo/claude-yolo`, `helpers/gnome/verify_extension.py`,
`ruff.toml`, `run.bash`, `scripts/qa-ansible.bash`, `scripts/qa-bash.bash`,
`scripts/qa-js.bash`, `scripts/qa-python.bash`). The other 89 files auto-merge
clean. This plan resolves those 18, lands the result as a reviewed PR (never a
direct bypass push — `F*` branches are PR-protected; a direct push only works
because of the owner's bypass, and that is not "clean"), and absorbs Plan
00068's design into the mainline.

`Plan 00089` lands a narrow carve-out of Plan 00068's scope directly on `F44`
first (a `GH_TOKEN` precedence guard in `ssh-handling.bash` — corrected in
that plan's own record: not a bug fix, `gh` already honours a caller-set
`GH_TOKEN`, but worth keeping for explicitness). This resync must not
silently drop that guard, and there is real risk of it doing so quietly:
`git diff origin/F44 origin/plan-00066-ccy-ci-runner -- files/var/local/claude-yolo/lib/ssh-handling.bash` shows the branch's copy is
**missing** F44-only robustness work added since the fork
(`resolve_token_owner_login()`'s retry/validate logic) — it is stale, not a
competing implementation, and this file was NOT in the original 18-file
merge-tree conflict list (computed before Plan 00089's edit existed). Once
Plan 00089's `elif` lands next to a hunk both sides already touch
differently, this file may newly conflict; Task 2.1 must resolve it by
keeping F44's side (Plan 00089's guard plus the retry/validate logic) and
discarding the branch's stale, simpler version — never the reverse.

## Goals

- Merge `origin/plan-00066-ccy-ci-runner`'s 90 commits into `F44` via a
  reviewed PR, preserving ancestry (merge commit or rebase merge — never
  squash; `allow_squash_merge` is already `false` on this repo).
- Resolve all 18 conflicting files correctly — not just absence of conflict
  markers: `./scripts/qa-all.bash` green, and the merged QA scripts run the
  **union** of both sides' stages (both sides added stages independently;
  `CLAUDE/QA.md` documents the positional `.[0]..[5]` jq merge as fragile).
- Confirm `run.bash`'s `RUN_BASH_GITHUB_ACCOUNTS=none` support (Plan 00082,
  proven live from the lts-infra side two days before this plan) survives the
  merge — this is the one regression that would silently un-bank already-proven
  work.
- `qa-reviewer` review of the full merge diff before the PR is opened for
  human merge.

## Non-Goals

- Merging the PR without human sign-off. This plan lands a reviewed,
  QA-green, mergeable PR — the actual merge button is the repo owner's call,
  same as any other PR on their own repo.
- Re-implementing or redesigning Plan 00068's CI-runner requirements beyond
  what already exists as code on the branch — this is a resync, not a
  feature re-scope.
- Any change to lts-infra (Plan 00045 there tracks the lts-infra-side
  dependency; this plan is fedora-desktop-only).

## Tasks

### Phase 1: Pre-merge state

- [x] ✅ **Task 1.1**: Establish divergence facts (commit counts, diffstat,
  merge-base, dry-run conflict list) — done in this plan's overview, verified
  via `git merge-tree --write-tree` (read-only).
- [x] ✅ **Task 1.2**: Commit and push Plan 00089 first (`808ec31`, pushed to
  `F44`), so the merge base includes its `GH_TOKEN` precedence guard rather
  than conflicting with it as uncommitted work.
- [x] ✅ **Task 1.3**: Created the integration branch
  (`resync-ccy-ci-runner-f44`) from `F44` and merged
  `origin/plan-00066-ccy-ci-runner` into it with `--no-ff` (merge in
  progress, all conflicts resolved, commit pending Task 3.4).

### Phase 2: Resolve conflicts

- [x] ✅ **Task 2.1**: Resolved the 8 code/script conflicts, file by file.
  `ssh-handling.bash` did NOT newly conflict (confirmed absent from
  `git status --porcelain=v1 | grep '^UU'` once the merge started) — auto-merged
  clean, so no separate action was needed for it. `files/var/local/claude-yolo/claude-yolo`:
  CCY_VERSION → 3.44.0, took the branch's `engine_assert_rootless()` (Plan
  00072\) over F44's older docker-context-name check. `run.bash`: version →
  1.16.0, kept the branch's new `check_legacy_grub_cgroup()` function body
  with F44's more precise comment, `RUN_BASH_GITHUB_ACCOUNTS` intact (25
  refs, matches pre-merge count). `scripts/qa-bash.bash`: took F44's side
  (shared `qa-discovery.bash` library supersedes the branch's older inline
  discovery). `scripts/qa-ansible.bash`: took the branch's side (uses the
  already-present `ff_strip_comment()` helper F44's side left dead).
  `scripts/qa-js.bash`, `helpers/gnome/verify_extension.py`: trivial
  comment-wording, took the more precise/current side each time. `ruff.toml`:
  union of both sides' `[lint.per-file-ignores]` (adjacent additions, not a
  real conflict). `scripts/qa-python.bash`: merged the branch's unique
  ruff-version-assertion block (Plan 00071) in front of F44's shared
  discovery-library call, discarding the branch's now-superseded old
  discovery logic.
- [x] ✅ **Task 2.2**: Resolved the 10 doc/prose conflicts — merged content
  throughout, not pick-a-side: `CLAUDE.md`'s topic-file index table unioned
  to all 16 files that actually exist on disk (verified via `ls CLAUDE/*.md`);
  `CLAUDE/Plan/README.md`'s Completed-plans section unioned (F44's 00085 row
  - the branch's 00070/00071/00067/00060 rows); `CLAUDE/QA.md` kept F44's
    far more detailed gate documentation as the base and added the branch's
    new `qa-docs.bash` as a 7th JSON-merged-gate row, dropping the branch's
    now-inaccurate "shellcheck is optional" caveat; `docs/headless-provisioning.md`
    and `docs/headless-server-install.md` both took F44's side throughout
    (documents `RUN_BASH_GITHUB_ACCOUNTS=none`, Plan 00082 — the branch's text
    called GitHub "mandatory in v1", stale pre-00082 wording); `docs/fast-file-manager.md`
    mostly took the branch's more precise wording, including its correctly
    `fast_file_manager_`-prefixed variable table (F44's side had unprefixed
    names contradicting its own example 6 lines above); `docs/architecture.md`
    took the branch's more accurate `play-mask-intel-lpmd.yml` description
    (verified against the actual play source) plus its whole new "Desktop or
    server" section (F44's side was empty there); `docs/features/README.md`
    took F44's side (empty) over the branch's stale "Coming Soon: CCY docs"
    (CCY docs already ship and are already linked earlier in the same file).
- [x] ✅ **Task 2.3**: `ssh-handling.bash` auto-merged clean with no newly
  conflicting hunk, so Plan 00089's `GH_TOKEN` guard survived untouched —
  confirmed via `git diff 808ec31 -- files/var/local/claude-yolo/lib/ssh-handling.bash`
  showing no changes from this merge. Noted in the merge commit message.

### Phase 3: Verify

- [x] ✅ **Task 3.1**: `./scripts/qa-all.bash` green — exit 0, 619 files
  checked (log: `/tmp/qa-all-output.log`).
- [x] ✅ **Task 3.2**: Confirmed the union of both sides' stages actually
  runs: `qa-docs.bash` (branch-only addition) ran and passed at 56 files;
  `qa-helper-tests.bash` ran 203 tests (up from the pre-merge 161, confirming
  the branch's additional `_planlib.inc.bash` test suite is included).
- [x] ✅ **Task 3.3**: `RUN_BASH_GITHUB_ACCOUNTS` — 25 references in the
  merged `run.bash`, matching the pre-merge (F44-only) count exactly.
- [x] ✅ **Task 3.4**: `infra-reviewer` verdict: FIX-BEFORE-MERGE (1 blocker,
  3 advisories) — all resolved. Blocker: `ruff.toml` self-contradicted (a
  branch-era header claiming ruff's unenumerated 413-rule default while the
  body enforced an explicit 59-rule `select`, plus an inert `clip-scan`
  BLE001 ignore for a rule that was never actually enabled) — fixed in
  `ruff.toml`, `scripts/qa-python.bash`, and `.claude/rules/qa-gates.md`
  (a branch-introduced file never in the 18-file list, so never previously
  reviewed). Advisories: a duplicated 4-line hunk in `run.bash` (harmless —
  `hl_resolve_secret` is idempotent — but no gate saw it), and a second
  version collision in `docs/run-bash-changelog.md` my own renumbering had
  missed (Plan 00065's `server-recommended` entry was silently dropped
  rather than renumbered; fixed by shifting F44's chain by +2 instead of
  +1, recovering Plan 00065's entry from `git show 808ec31:run.bash`, and
  bumping `RUN_BASH_VERSION` to 1.17.0). Full detail in the journal.
  `./scripts/qa-all.bash` re-run clean after fixes: exit 0, 619 files.

### Phase 4: Land

- [ ] ⬜ **Task 4.1**: Push the integration branch and open a PR into `F44`
  (merge commit or rebase merge target — never squash).
- [ ] ⬜ **Task 4.2**: Report the PR to the user for their own merge decision.

## Success Criteria

- [ ] ⬜ All 18 conflicting files resolved with both sides' intent preserved.
- [ ] ⬜ `./scripts/qa-all.bash` green on the merge result.
- [ ] ⬜ `qa-reviewer` finds nothing blocking.
- [ ] ⬜ PR open against `F44`, mergeable, no squash.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00090-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->

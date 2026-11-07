# Task 2.3: an outcome for every open work branch (2026-11-06)

Read-only classification, after `git fetch origin`. Nothing was written, pushed, deleted or
removed. Measured against origin/F44 at the fetch. Scope: 47 local branches and 33 origin
`refs/heads`, minus F42, F43, F44 and F44-arm: **65 branches**, the same 65 that
`branch-sweep` counts as open work.

## Method

- **MERGED**: `git merge-base --is-ancestor <tip> origin/F44`. No branch passed.
- **CONTENT-IN-F44**: every blob added by any commit in `origin/F44..<tip>` (`git log --raw`,
  deletions excluded) is in `git rev-list --objects origin/F44`. This is the
  "content-preserved" proof `delete-branch` makes.
- **ARCHIVED**: the tip equals `refs/archive/<name>` from `git ls-remote origin`. Each archived
  row also says what the content is, because the owner's delete decision rests on that.
- **SUPERSEDED / WANTED**: decided by reading the unique commits against F44 and its plans.
  Plan 00245's triage ([a](../00245-valuable-work-never-untracked/RESEARCH-branch-triage-261104-a.md),
  [b](../00245-valuable-work-never-untracked/RESEARCH-branch-triage-261104-b.md),
  [c](../00245-valuable-work-never-untracked/RESEARCH-branch-triage-261104-c.md)) was the
  starting point. Each verdict was checked again against today's F44.
- **Order of precedence**: OPEN-PR, MERGED, CONTENT-IN-F44, ARCHIVED, SUPERSEDED, WANTED,
  RESCUE-JUNK. No branch ended as RESCUE-JUNK. Each rescue commit that holds only deletions or
  scratch already passes CONTENT-IN-F44 or is ARCHIVED, and its content note says "rescue-junk".
- **Worktree**: from `git worktree list --porcelain`. **Clean** means
  `git -C <wt> status --porcelain` printed nothing. Every worktree found was clean. Paths are
  relative to `<repo>`.
- **Age** is the tip's committer date, with the days before 2026-11-06 in brackets.

## OPEN-PR (13): left to their pull requests

All are on origin only and have no worktree: `arm/onto-f44` (#189), `feat/bus-v2-n3f-a-int`
(#158), `-b-int` (#166), `-c1b-int` (#168), `-c1c-int` (#171), `-c1d-int` (#175), `-c2a-int`
(#178), `feat/crossnet-s12-int` (#173), `-s13-int` (#176), `-s14-int` (#177), `-s15-int` (#172),
`fix/pt1-p2-readings-156` (#165) and `spike/v2-cross-network` (#128).

## CONTENT-IN-F44 (13)

Every branch here ends in a "WIP rescue" commit (Plan 00245, 2026-11-04) whose parent is an
ancestor of F44. Every file version these commits add is reachable from F44.

| Branch                             | Where  | Age       | Worktree   | Evidence                                                                                                              |
| ---------------------------------- | ------ | --------- | ---------- | --------------------------------------------------------------------------------------------------------------------- |
| `agent-a28b7b76ad0b6a8d7-d011d6bc` | local  | 11-04 (2) | yes, clean | adds one Plan 00194 report, byte-identical in F44; archive also matches                                               |
| `agent-aa8c9ce666ddd4368-2729948d` | local  | 11-04 (2) | yes, clean | adds 9 early Plan 00209 files, each version in F44 history; archive matches                                           |
| `agent-aec67989e2c717703-6b283ee3` | local  | 11-04 (2) | yes, clean | adds one Plan 00201 report, byte-identical in F44; archive matches                                                    |
| `fix/lxc-checkout-typed`           | local  | 11-04 (2) | yes, clean | rescue writes older F44 versions back over 73 files and deletes 14 (a stale-base revert), all in F44; archive matches |
| `fix/teams-full-handles`           | local  | 11-04 (2) | yes, clean | same pattern: 85 modified to older F44 versions, 7 deleted; archive matches                                           |
| `fix/teams-wait-play-lock`         | local  | 11-04 (2) | yes, clean | same pattern: 119 modified to older F44 versions, 34 deleted; archive matches                                         |
| `wip-rescue/bus-v2-n3b-rv2`        | origin | 11-04 (2) | none       | one report added, byte-identical in F44 (patch-equivalent, `git cherry` `-`)                                          |
| `wip-rescue/bus-v2-n3b-rv3`        | origin | 11-04 (2) | none       | same (patch-equivalent)                                                                                               |
| `wip-rescue/bus-v2-n3b-rva`        | origin | 11-04 (2) | none       | one report added, its blob in F44 (`git cherry` shows `+` because the base path differs)                              |
| `wip-rescue/bus-v2-n3c-rv2`        | origin | 11-04 (2) | none       | patch-equivalent; its blob is in F44                                                                                  |
| `wip-rescue/bus-v2-n3c-rv3`        | origin | 11-04 (2) | none       | patch-equivalent; its blob is in F44                                                                                  |
| `wip-rescue/bus-v2-n3d-rva`        | origin | 11-04 (2) | none       | one report, its blob in F44                                                                                           |
| `wip-rescue/bus-v2-n3d-rvb`        | origin | 11-04 (2) | none       | one report, its blob in F44                                                                                           |

Plan 00245 batch c said the n3c-rv2/rv3 reports differed from the spike's copy. Today the blob
each one adds is in F44 history, so that difference is gone.

## ARCHIVED (24): tip byte for byte at `refs/archive/<name>`, unique content kept there

All are local only (no `refs/heads` on origin), and every worktree is clean.

| Branch                               | Age        | Worktree                                | Content (what deleting the local copy would drop, and why it may go)                                                                                                                                                 | Plan  |
| ------------------------------------ | ---------- | --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----- |
| `dbf/cfd-2`                          | 11-04 (2)  | `.claude/worktrees/dbf-cfd-2`           | superseded: the rescue holds an earlier draft of vmtest's error-carrying; F44 has the later form at 2ad41545c ("Plan 00191 DBF: carry command failures in vmtest")                                                   | 00191 |
| `docs/finished-reports-261105`       | 11-05 (1)  | none                                    | superseded: the first merge branch of Plan 00245's finished reports. F44 took the rebuild 6d1b25e6d; the 9 differing files are the unsigned 433 commits' versions                                                    | 00245 |
| `feat/agent-bus-v2-design-2`         | 10-21 (16) | `.claude/worktrees/agent-ac5d0762…`     | superseded: two build-brief commits; F44 holds the later `00209/BUILD-BRIEF.md`                                                                                                                                      | 00209 |
| `feat/bus-v2-m1-proto`               | 10-21 (16) | `untracked/worktrees/bus-v2-m1-proto`   | superseded: `internal/bus/proto`, absent from F44; `internal/teamchat/proto` replaces it (00209 Task 7.2, "rebuilt or dropped")                                                                                      | 00209 |
| `feat/bus-v2-m1-proto-envelope`      | 11-04 (2)  | `untracked/worktrees/…-envelope`        | superseded, as m1-proto; the tip is a rescue adding `bus/proto/render.go` and fixtures                                                                                                                               | 00209 |
| `feat/bus-v2-m1-proto-limits`        | 11-04 (2)  | `untracked/worktrees/…-limits`          | superseded, as m1-proto (rescue adds `lines_test.go` and its fixture)                                                                                                                                                | 00209 |
| `feat/bus-v2-m1-proto-ping`          | 11-04 (2)  | `untracked/worktrees/…-ping`            | superseded, as m1-proto (rescue adds ping, receive and text)                                                                                                                                                         | 00209 |
| `feat/bus-v2-m1-proto-rooms`         | 11-04 (2)  | `untracked/worktrees/…-rooms`           | superseded, as m1-proto (rescue adds qindex, questions, reports and roomstate)                                                                                                                                       | 00209 |
| `feat/bus-v2-m1-state`               | 10-21 (16) | `untracked/worktrees/bus-v2-m1-state`   | superseded: `internal/bus/state`, absent from F44; `internal/teamchat/state` replaces it; 00209 Task 7.2                                                                                                             | 00209 |
| `feat/bus-v2-m1-state-locks`         | 10-21 (16) | `untracked/worktrees/…-state-locks`     | superseded, as m1-state                                                                                                                                                                                              | 00209 |
| `feat/bus-v2-m2-hooks`               | 10-21 (16) | `untracked/worktrees/bus-v2-m2-hooks`   | superseded: F44's 00209 PLAN.md line 130 says "`feat/bus-v2-m2-hooks` is superseded" (v2 uses `teamchat hook`)                                                                                                       | 00209 |
| `feat/bus-v2-m2-hooks-golden`        | 10-21 (16) | `untracked/worktrees/…-hooks-golden`    | superseded: the golden fixtures for the m2-hooks port                                                                                                                                                                | 00209 |
| `feat/bus-v2-n3e-seam`               | 11-04 (2)  | `untracked/worktrees/bus-v2-n3e-seam`   | rescue-junk: the rescue adds `261023-n3e-seam-conformance.md`, which F44 has; the only difference is table column padding                                                                                            | 00209 |
| `feat/bus-v2-s1-declare`             | 10-21 (16) | `untracked/worktrees/bus-v2-s1-declare` | superseded, probably: stage 1 landed (856f9c14f, 082363498); F44 has `bus/declare` and `bus/seat`. Only unique: merge resolutions and `261021-s1-merge.md`. **But** F44's Task 7.1 is still 🔄 and names this branch | 00209 |
| `feat/pingbus-compact-before-wake`   | 10-19 (18) | `.claude/worktrees/agent-afecb6522…`    | superseded: 00194 Task 2.0e is ❌ in F44, moved to 00209 Task 3.1 (v1 frozen); its tip is inside `review/pingbus-compact-before-wake`                                                                                | 00194 |
| `go-qa-ci/adopt`                     | 10-15 (22) | `.claude/worktrees/agent-aca5e949…`     | superseded: F44 adopted go-qa-ci at a later pin (58e6f29ec, e60b218cb)                                                                                                                                               | 00191 |
| `rescue/bus-v2-m1-proto-envelope`    | 11-03 (3)  | none                                    | superseded, as m1-proto (the 11-03 rescue of the same slice)                                                                                                                                                         | 00209 |
| `rescue/bus-v2-m1-proto-limits`      | 11-03 (3)  | none                                    | superseded, as m1-proto                                                                                                                                                                                              | 00209 |
| `rescue/bus-v2-m1-proto-ping`        | 11-03 (3)  | none                                    | superseded, as m1-proto                                                                                                                                                                                              | 00209 |
| `rescue/bus-v2-m1-proto-rooms`       | 11-03 (3)  | none                                    | superseded, as m1-proto                                                                                                                                                                                              | 00209 |
| `review/lxc-checkout-typed-rereview` | 10-19 (18) | `.claude/worktrees/agent-abe52abf…`     | superseded: one review report, absent from F44; a later final review of the same branch is in F44, and so is the work                                                                                                | 00203 |
| `review/teams-full-handles`          | 10-16 (21) | `.claude/worktrees/agent-a374a700…`     | superseded: first review report; F44 holds the final MERGE review 79ff4ad9c and the merge a5bc2b1b5                                                                                                                  | 00201 |
| `review/teams-full-handles-rereview` | 10-19 (18) | `.claude/worktrees/agent-a30eec90…`     | superseded: the re-review; same final review in F44                                                                                                                                                                  | 00201 |
| `review/teams-wait-play-lock`        | 10-16 (21) | `.claude/worktrees/agent-a272344d…`     | superseded: first review report; final check fc5b25c4c and merge 26407a537 are in F44                                                                                                                                | 00201 |

## SUPERSEDED (8): unique content, replaced, no archive yet

| Branch                               | Where          | Age        | Worktree                                      | Replacement and evidence                                                                                                                                           | Plan  |
| ------------------------------------ | -------------- | ---------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----- |
| `agent-a5e07c91129acb55c-99bbdc03`   | local          | 11-02 (4)  | yes, clean (nested in the f44-arm worktree)   | ancestor of F44-arm (local = origin, 589acbba6); PR #189 (`arm/onto-f44`) carries F44-arm's work onto F44                                                          | 00243 |
| `arm/ccy-images`                     | local          | 11-01 (5)  | `.claude/worktrees/arm-ccy-images`, clean     | ancestor of F44-arm; same                                                                                                                                          | 00243 |
| `arm/ci-install-lab`                 | local          | 11-02 (4)  | `.claude/worktrees/arm-rest`, clean           | ancestor of F44-arm; same                                                                                                                                          | 00243 |
| `arm/gate-review-fixes`              | local          | 11-02 (4)  | `.claude/worktrees/arm-gate-fixes`, clean     | ancestor of F44-arm; same                                                                                                                                          | 00243 |
| `arm/main-plays`                     | local          | 11-02 (4)  | `.claude/worktrees/arm-main-plays`, clean     | ancestor of F44-arm; same                                                                                                                                          | 00243 |
| `arm/optional-plays`                 | local          | 11-02 (4)  | `.claude/worktrees/arm-optional-plays`, clean | ancestor of F44-arm; same                                                                                                                                          | 00243 |
| `feat/bus-v2-m1-matrix`              | both, same tip | 10-21 (16) | `untracked/worktrees/bus-v2-m1-matrix`, clean | 8 of its 10 commits are patch-equivalent in F44 (`internal/teamchat/matrix`, 00209 Task 1.5 ✅). The other 2 are the build brief, replaced by F44's BUILD-BRIEF.md | 00209 |
| `review/pingbus-compact-before-wake` | both, same tip | 10-19 (18) | `.claude/worktrees/agent-a83a9b8f…`, clean    | the cold-wake feature (`coldwake.py`, SPEC-compact-wake) plus its FIX-FIRST review; 00194 Task 2.0e is ❌ in F44, re-specified as 00209 Task 3.1 in Go             | 00194 |

## WANTED (7): unique work that nothing in F44 replaces

| Branch                             | Where          | Age        | Worktree   | The work                                                                                                                                                                                     | Plan             |
| ---------------------------------- | -------------- | ---------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| `agent-a5ee80f767118a918-a448e021` | both, same tip | 10-23 (14) | yes, clean | ccy containers get a bounded `--pids-limit` (CCY 3.100.16-.18), a semgrep rule and a test; nothing in F44 sets a pids limit; stopped at review round 2                                       | none found       |
| `agent-a81014c63f66ee6f8-c4d3bbe2` | both, same tip | 10-23 (14) | yes, clean | rule `ansible-umask-pinned` and a callback pinning umask 022 for every Ansible run; F44 has only the separate `root-umask` rule; review round 2 not run                                      | 00185 (Complete) |
| `agent-a8c69aa67110156ce-c48195fd` | both, same tip | 10-23 (14) | yes, clean | `ccy --teams` with an unknown team asks instead of failing (00177 Task 7.6), plus rules `project_child_exit` and `project_row_kind`; none of it in F44, and F44's 00177 PLAN has no Task 7.6 | 00177            |
| `agent-ad49d4886a70d25db-01b66214` | both, same tip | 10-23 (14) | yes, clean | ccy's Go caches on the host per project, bounded, through `os.Root` (CCY 3.100.20-.21); not in F44; stopped at review round 4                                                                | none found       |
| `agent-ae87230b6e5b05508-967f0e77` | both, same tip | 10-13 (24) | yes, clean | 00173: the evening run accepted, and `ccy-sessions` no longer claims a cc session was warned; F44 has 00173 as Blocked without this                                                          | 00173            |
| `feat/release-version`             | origin         | 11-04 (2)  | none       | 00153 Tasks 5.1-5.4: the owner's decisions D1-D4, `internal/releasever`, the release workflow and its docs; all ⬜ in F44's PLAN; no pull request (open or closed)                           | 00153            |
| `plan-team-liaison-role`           | both, same tip | 10-23 (14) | yes, clean | Plan 00225, the team liaison role (plan, design, five review rounds, the owner's input); the plan exists nowhere in F44                                                                      | 00225            |

## Counts

| Outcome        | Count  |
| -------------- | ------ |
| OPEN-PR        | 13     |
| MERGED         | 0      |
| CONTENT-IN-F44 | 13     |
| ARCHIVED       | 24     |
| SUPERSEDED     | 8      |
| WANTED         | 7      |
| RESCUE-JUNK    | 0      |
| **Total**      | **65** |

## What the coordinator would do, per outcome

- **OPEN-PR**: nothing. Each branch ends with its pull request (GitHub's
  `delete_branch_on_merge` fires on a GitHub merge).
- **MERGED**: none. A remote delete of a merged branch is not needed for any branch.
- **CONTENT-IN-F44, local (6)**: `branch-sweep --clean` does not take these, because it sees
  them as `live` rather than merged or patch-equivalent. So, for each: remove its clean worktree
  without `--force` (`git worktree remove <wt>`), then
  `hooks-daemon delete-branch <b>`. That proves content-preserved and writes the bundle. The
  `refs/archive/<b>` copies stay; they are the owner's.
- **CONTENT-IN-F44, origin-only `wip-rescue/*` (7)**: no local branch, so `delete-branch` cannot
  act, and none is merged, so the remote-delete guard refuses the agent. Per Task 2.4 these are
  the owner's: `git push origin --delete wip-rescue/bus-v2-…` at a terminal.
- **ARCHIVED (24)**: nothing proves them, so `delete-branch <b>` refuses. With the owner's
  consent, remove each clean worktree (no `--force`), and the owner runs
  `hooks-daemon delete-branch --allow-unproven --reason "archived at refs/archive/<b>; superseded by <x>" <b>`
  at an interactive terminal. If the owner keeps one instead, it gets
  `hooks-daemon branch-park <b> --reason "…"`.
- **SUPERSEDED, the six held by F44-arm**: remove the clean worktree, then
  `hooks-daemon delete-branch --protected-ref F44-arm <b>`. Ancestry of the integration line
  proves each one, so no owner is needed. Do `agent-a5e07c…` first: its worktree is nested
  inside the f44-arm worktree.
- **SUPERSEDED, `feat/bus-v2-m1-matrix` and `review/pingbus-compact-before-wake`**: push each
  tip to `refs/archive/<b>` and check it with `git ls-remote`. Then the owner deletes the local
  branch (`delete-branch --allow-unproven`) and the origin head (`git push origin --delete`) at
  a terminal.
- **WANTED (7)**: `hooks-daemon branch-park <b> --reason "<plan>: <what is left>"`, unless the
  owner drops one (then it is archived, as for SUPERSEDED). Removing the clean worktree is safe,
  because the branch keeps the work.

## Outcomes only the owner can decide

- `agent-a5ee80f…` (pids limit): finish it under a plan, or drop it? No plan names it.
- `agent-a81014c…` (umask pin): is F44's `root-umask` rule the replacement, so it is dropped, or
  is the per-run callback pin still wanted?
- `agent-a8c69aa…` (00177 Task 7.6, unknown team): finish it under 00177? F44's PLAN has lost the
  task.
- `agent-ad49d48…` (ccy go-cache): finish it under a plan, or drop it? No plan names it.
- `agent-ae87230…` (00173 evening run accepted): merge this record into 00173, or drop it?
- `feat/release-version` (00153 Phase 5): park it until the GitHub objects exist, or open the
  draft pull request Plan 00245 promised?
- `plan-team-liaison-role` (Plan 00225): keep the plan, by merging it or parking it, or drop it?
- `feat/bus-v2-s1-declare`: F44's 00209 Task 7.1 still says 🔄 on this branch, though stage 1
  landed. Is the branch done, so it is deleted and the task corrected, or still live?
- `feat/bus-v2-m1-proto*`, `rescue/bus-v2-m1-proto-*`, `feat/bus-v2-m1-state*`: Plan 00245
  said the Python-parity corpus might be reused by `teamchat/proto`. Delete all nine local
  copies, now that the archives hold them?
- The other ARCHIVED branches: consent to delete their local copies behind the archives?
- The seven `wip-rescue/*` origin heads: delete them? Each one's content is in F44.

## Tooling faults met (for the coordinator to log)

- Every `hooks-daemon` call prints a `resolve_venv … interpreter drift` line: a venv recorded as
  Python 3.14 now runs 3.13. The command still worked.
- `pipe_blocker` denied `git grep … | head` inside a shell function, yet allowed `git diff … | head` inside a `for` loop in the same session. The judgement is inconsistent.
- `branch-sweep` reports 24 branches whose tips are at `refs/archive/*` as `live` or
  `contained-nowhere`. That is the defect already listed as Task 1.6.

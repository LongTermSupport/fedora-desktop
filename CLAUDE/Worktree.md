# Worktree

**Read first:** [CLAUDE/core/Worktree.core.md](core/Worktree.core.md) — the daemon's core
guidance for this subject, and the baseline everything below extends.

That file is DAEMON-owned: it is overwritten wholesale on every
daemon upgrade, so never edit it and never copy its content here. A
second copy is how the two drift apart, which is the failure this
split exists to prevent.

## Project-specific additions

This file is yours. The daemon seeds it once and never modifies it
again, so anything you add below survives every upgrade.

### Branch lifecycle: delete when merged, few open, none dormant

The branch limit exists so that work FINISHES, never so that it disappears. Work outside a lane
being finished gets lost, so never save changes as a patch or copy under `untracked/`, never
`git worktree remove --force` or `git branch -D` work not contained elsewhere, never stash, and
never open a side branch for a diagnostic or experiment that serves another lane (it goes on that
lane's branch as commits, reverted later if temporary). When the rule seems to force a choice
between losing work and exceeding the limit, stop and ask a human; losing work is never the
answer.

At most 10 open work branches; a branch merged into F44 is deleted; a branch with no commit for
more than 3 days is warned about and for more than 7 days fails the daily CI job, unless it is
parked with a reason and a review date. The policy and how to park:
[QA.md, Branch hygiene](QA.md#branch-hygiene--the-repositorys-branches-judged-daily).

### What filled the cap, and the rule against each cause

The cap reached 28 branches on 2026-11-10 (owner: "no more branch sprawl"). Each cause has a rule.
Never meet the cap by exempting branches or raising it. Fix the cause.

- **Rescue copies kept as branches** (`rescue/*`, `wip-rescue/*`). A copy made during a
  recovery goes to `refs/archive/<name>` (`git push origin <ref>:refs/archive/<name>`) or a
  bundle under `untracked/deleted-branches/`, never to a branch. The cap counts branches only,
  so an archive ref costs nothing, and it stays on GitHub.
- **Stacked PR chains**, one branch per slice, each waiting on the one below. Do not stack. Put
  each piece of a stream as commits on that stream's one branch and PR, or land the bottom of the
  stack before starting the next. A stack that exists is collapsed: retarget the top PR to F44
  (it holds every lower commit) and close the lower PRs as superseded.
- **Agent worktrees left behind.** Reap after every agent that had one:
  `hooks-daemon worktree-reap --reap --reap-branches` (with `--base-branch` set to the line the
  work was for, e.g. `F44-arm`). Create a worktree from the main checkout only, never inside
  another worktree's `.claude/worktrees/`.
- **Finished work never put up for review.** A branch whose work is done gets a PR the same day.
  Paused work is parked with `hooks-daemon branch-park <name> --reason ...`, never left to go
  dormant.
- **PRs left open for review rounds.** A PR gets one review and one round of fixes. What is left
  goes to the owner or an issue. It lands when its CI is green: in GitHub with
  `gh pr merge --merge` (organisation admins bypass the required review with `--admin`), or by a
  local `--no-ff` merge and a passing full QA run when its CI is stale against F44.

The daemon commands, in order: `branch-sweep` (report), `worktree-reap`, `branch-sweep --clean`
(local, bundled), `branch-sweep --clean --remote` (only branches merged into F44),
`delete-branch` (local, proven against `--protected-ref`). An agent may delete a remote branch
only when it is merged into F44. Any other remote delete is the owner's, after its tip is in
`refs/archive/`.

### One `.git`, two path worlds: the host and the ccy containers

The host and every ccy container share this checkout's `.git`, but see it at different
paths (the host's checkout path; `/workspace` inside a container). Two rules follow.

- **Never create a worktree with relative paths** (`git worktree add --relative-paths`,
  or `worktree.useRelativePaths=true`). Git 2.48+ then writes `extensions.relativeWorktrees`
  into the shared `.git/config`, and the older git in the ccy image refuses the whole
  repository ("unknown repository extension found: relativeworktrees"): no git and no
  hooks daemon in any new ccy session. This rule stands until the ccy image ships git
  2.48 or newer.
- **Never run `git worktree prune` (or `git worktree repair`) from the host.** The records
  of worktrees made inside containers name `/workspace/...` paths that do not exist on the
  host, so a host prune deletes every one of them as stale. Prune from inside a container,
  and only after checking which records it would remove (`git worktree prune --dry-run -v`).

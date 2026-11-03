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

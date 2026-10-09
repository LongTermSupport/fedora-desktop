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

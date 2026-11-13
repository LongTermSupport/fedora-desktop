## 12:53 · finding · T2.1   — Thread-root cause, prototype, upstream search

Thread-root defect found and prototyped. `qml/ThreadPanel.qml` caps the root card at 190 px with `clip: true` and its body `Label` at `maximumLineCount: 6` with `elide`, both since the panel was written. Prototype on upstream `e6ea127a`, local branch `thread-root-scroll` (commit `0191db89`, never pushed): the body moves into a `Flickable` capped at 40% of the panel height (at least 120 scaled px), interactive only when it overflows, reset to the top when another root is shown; both caps removed; one data-driven QML test added. Not built: the container has no Qt, CMake or Rust toolchain (Task 2.2 is the IaC route).

Upstream search on GitHub (issues and pull requests, "thread" and full title lists): no existing report or fix.

Tooling note: the worktree-isolation guard refused `git -C` on the shared reference clone under `/workspace/untracked/repos/lightning`, so a fresh clone was made in this worktree's own `untracked/repos/` (HEAD identical, `e6ea127a`). The shared clone was not updated.

Owner requirement relayed by the coordinator: one separate Lightning instance per team, never multi-account in one window; made a hard requirement (Goals, Phase 3).

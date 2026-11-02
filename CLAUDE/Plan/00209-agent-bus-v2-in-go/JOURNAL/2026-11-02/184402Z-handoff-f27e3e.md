## 18:44 · handoff · —   — handover: state at the end of the coordinator session

Handover (coordinator, desktop team).

The owner chose to land v2 now: "yes we should push v2 and get it done", the minimum v2 with cross-network membership. The spike branch `spike/v2-merge-up` (f5023d692) merges `feat/bus-v2-n3e` onto F44. It builds and vets clean. Its first CI run produced the work list that is now Phase 8, Tasks 8.1 to 8.6. Two fixes were made in the merge itself: a lost closing brace in `lines_test.go`, and a duplicate go-deps allowlist row, now a comment. The spike also carries CCY 3.104.18. That version is taken on F44 by now, so it will need a fresh bump when the branch next merges F44.

qa-changed was not run before the spike commit. The commit was a measurement, and the owner was told. Every push from here on goes through the base check: F44 must be merged in, or CI fails in seconds.

Next: a worker session works through Phase 8 on `spike/v2-merge-up`, overnight, and reports by pull request. It never merges into F44 and never pushes to it. Only the owner merges.

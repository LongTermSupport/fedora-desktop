## 13:12 · action · —   — Tasks 8.1 and 8.2 are done

Tasks 8.1 and 8.2 are ticked: the work each describes is on this branch.

Evidence, read from the branch history (not re-run; GitHub CI judges the push):

- **Task 8.1** (the project-rule findings): the duration-overflow, realtime-bound, liveness-word and
  goroutine-end fixes were merged one class at a time, and the leakcheck move to `internal/leakcheck`
  (merges 112bf2fde, bf91e6ebb, 118716289, b23be8f83, 96428d20b).
- **Task 8.2** (the gosec exclusions): the Synapse registration MAC and the test-file exclusions are
  recorded as the owner decided on 2026-11-03, and the G304 and G101 sites are fixed in code, in two
  parts (merges f8be7a4be, 7bd950822; the record commits e5134eb51 and 6f397559d).

The subjects of those commits name this plan by the numbers it carried earlier (Plan 00209 and Plan
00244 Task 8.1 and 8.2); the task numbers are the same.

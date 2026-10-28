## 10:43 · action · —   — Tasks 6.6 and 6.5 built, tests first

Tasks 6.6 and 6.5 built, tests first.

- 6.6 (0be9e9a48): the optional plays on the developer path run through run_playbook, which
  records each outcome and goes on to the next play. optional_summary then prints every play,
  and the run exits 1 when any failed. A play ended by a signal or by Ctrl-C (exit 99) stops
  the run at once. Main plays still stop at the first failure.
  - Red: test-run-bash-optional.bash could not extract optional_summary. The 4 new
    test-run-bash-rerun.bash cases failed (exit 4, the plays after the failure not run).
  - Green: 37 of 37, rerun 69 of 69, changed 45, single-play 54 and consumer 40, all
    passing.
  - run.bash stays within its bash-limits record (statements 2811 to 2779).
- 6.5 (7564bbf31): the play compiles post-processing-offered (true only when
  install_audience is developer) into the schema as an override. The panel and Settings read
  its compiled default and hide Ctrl+Insert, Alt+Insert, the Post-Process items, Create
  Article and the Claude settings.
  - Red: test-stt-consumer-post-processing.mjs, 8 of 9 failing.
  - Green: 24 of 24 across the three stt node suites; panel-sections 121 and stop-grace 337
    pass; ESLint is clean (run with the main checkout's install, because this worktree has no
    extensions/node_modules).
- Not done: the consumer VM scenario installing speech-to-text. The attendant has a fixed
  Enter reply, and the manifest allows no optional app for a consumer, so this needs its own
  task.
- Not done: the markdown changes (the QA.md row, the changelog entry, the PLAN.md tick).
  The markdown-location guard refuses every .md write in this nested worktree, so they were
  handed to the coordinator.

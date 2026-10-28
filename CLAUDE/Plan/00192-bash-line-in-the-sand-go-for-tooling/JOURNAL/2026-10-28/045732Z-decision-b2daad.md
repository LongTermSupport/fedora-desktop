## 04:57 · decision · —   — The lab's retention is Go; only the spool sweep stays Python

The lab's retention moved from helpers/vmtest/retention.py into Go (src-go/internal/vmtest/retention.go). This fixes the second qa-reviewer pass, finding 2.

Who called retention.py: only vmtest, through the helper seam (floor from run, member-up and build-base; sweep from sweep and nightly; prune-plan from prune-runs and the disk guard), and its own tests. No play, task or other helper imports or runs it.

What moved to Go: the RAM ceiling (75% of MemTotal), the rebuild floor (room for two bases and the 2 GiB margin), the prune rule (passed, a later pass of the scenario, or older than DAYS), the run-passed rule, and the sweep of runs/ (the newest 10 passing runs kept) and bases/\*.build (fast removed, desktop kept). The margin now has one home (diskMarginBytes), so TestDiskMarginIsRetentionsMargin is gone. A run and a member now have one disk check, the guard (E64), and after it only the RAM check. A rebuild keeps its two-bases floor, which the guard's fixed 26 GiB cannot know.

What stays Python: the bridge spool's sweep, now `retention sweep-spool --checkout --log [--keep-spool]`. It works through spool.py's pinned descriptors, and spool.py is shared with the bridge helpers (bridge_watcher, bridge_run, request and others). A Go copy of that walk would duplicate security logic the Python still owns for them. Its done line is now VMTEST-SPOOL-SWEEP-DONE, so it cannot be read as the whole sweep's.

Departures: a response.json that exists but cannot be read now stops the sweep and the prune (the Python treated every OSError as "did not pass"). A floor refusal now says its figures in the error, not in VMTEST-DISK and VMTEST-RAM lines on stderr.

Red: `go test ./internal/vmtest/` with retention_test.go written first did not build (runFacts, planPrune, runPassed, memTotalMiB, Env.HostMiB undefined). The new test_retention.py failed 5 of 6 against the old helper (no sweep-spool, Entry with keep_always). Green: go test passes, coverage 90.4%, and the Python suite passes 6 of 6.

Host: play-vm-test-lab.yml has to run again, because it deploys both the vmtest binary and the helpers. Then the checks are H5 (nightly writes the sweep log), H7's dry run, and one run.

## 01:06 · action · —

SPEC-carry item 09 (the drill export, SD1-SD5), red. New tests in carry/drill_test.go
(TestDrillExportsAndServes, TestDrillRefusals with 11 rows, TestDrillResumesFromItsRecord,
TestDrillServeFailureLeavesTheRecord, TestDrillCopyFailureServesAgain,
TestDrillReplacesAnEarlierDrill, TestDrillNeedsTheCapAndTheLock, TestDrillKillAtEveryCall with
18 kill rows) fail to build: carry.Drill and Deps.Serve are undefined. In admin,
TestTeamExportDrillRunsTheDrill and TestTeamExportDrillExitCodes fail: `team export --drill` is
still an unknown flag (64). The export world's Files fake now makes MakeCarryDir, Create, Rename,
Remove and SyncDir effects, so each is a kill point.

## 00:43 · action · —

Sealed carry item 08 (SPEC-carry section 2, SX1-SX8, SG4, SL1, CN5), red.

Tests written first, run with `go test -race` over carry, admin and admin/wire: all three
packages fail to build (undefined: carry.Export, carry.OS, carry.Info, Deps.Files,
Deps.MaxArchive, carry.AdminTokenName, admin.carryMaxArchive).

- carry/export_test.go: TestExportWritesTheSealedArchive, TestTwoExportsAreByteIdentical,
  TestExportFailsWhenASourceChangesBetweenPasses, TestExportRemovesALeftPart,
  TestAChangedExportIsAFailure, TestExportRefusals (24 rows), TestExportNeedsTheArchiveCap,
  TestExportMakesTheCarryDirectory, TestExportWaitsForTheLock, TestExportReadsEachSourceTwice.
- admin/exportverb_test.go: TestTeamExportPrintsTheAnswer, TestTeamExportUsage,
  TestTeamExportIsRootsOnly, TestTeamExportExitCodes, TestCarryMaxArchiveFile.
- admin/wire/store_test.go: TestCarryEngineIsTheHostsAdapters now holds Deps.Files to
  carry.OS under the root and Deps.MaxArchive to 78 with no setting.

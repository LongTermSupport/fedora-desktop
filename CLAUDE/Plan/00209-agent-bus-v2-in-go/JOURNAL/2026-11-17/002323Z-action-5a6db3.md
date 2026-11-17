## 00:23 · action · —

SPEC-carry item 07 (issue #324), red: the seal, its verb, SM5's kill at every call, SL2.

Tests written first and seen to fail (build failures on the missing names, `untracked/scratch/red-07-carry.txt`, `red-07-admin.txt`):

- restore/validate_test.go: TestLeftoversAndReadAppliedServeTheSeal (undefined: Leftovers, ReadApplied; section 13's exports).
- carry/seal_test.go (undefined: Deps, Seal): TestSealSealsTheTeam (SC1-SC7, every call in order, the answer is sealed.json's document with `changed`), TestSealWithNoEpochFloorRecordsZero (SC5), TestSealOfASealedTeamChangesNothing (SC7 repeat), TestSealRefusals (12 rows of SC1/SC2/SG4, nothing written by any), TestSealTailFailureWritesNothing (SC2, RS15), TestSealFailsOnAUnitThatDoesNotStop (SC4, RS18), TestSealWaitsForTheLock (SL1, host.PlayWait), TestSealKillAtEveryCall (SM5: a runtime.Goexit at each of the 18 calls, the invariants on disk, then a seal ends sealed and one more changes nothing).
- admin/sealverb_test.go (System.Carry, Carrier undefined): TestTeamSealPrintsTheSealedDocument, TestTeamSealUsage (`--drill` and the rest are 64), TestTeamSealIsRootsOnly (77), TestTeamSealExitCodes (70, 75, 69, 1, 78).
- admin/wire/store_test.go: TestCarryEngineIsTheHostsAdapters (panicked on the nil seam); TestEverySeamIsSetAndNothingIsRead went red on System.Carry.
- health/seal_test.go: TestASealBetweenGatherAndUnderLockDropsTheTeam (SL2). Its first leaf row was red because the test placed the seal after the leaf renewal (the gather's view comes after the terminator step); moved to the first probe, it passes with no code change, as section 13 says for health.

Name clash: carry's manifest type `Seal` is renamed `SealFacts` so the function can be `carry.Seal` as the spec names it.

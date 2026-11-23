# R0 steps 4-5: report (partial, handed off)

Branch `v2-questions`, commit 564548403 (pushed), on origin/v2 5f442cac0 merged in. CCY 3.123.4 /
container 2.84.51.

## Built: step 4, QX3 and QX4

- **QX4.** `team.Team.Questions` runs a compose under the team's judging hold, which a sync
  answer and a builder page hold through judge, store write and commit; so a compose never races
  a copy-and-commit of the index, and each placement lands on the live index before the hold is
  let go. Test: `TestQX4ComposesAreOneAtATimeAndNumberAboveThePlacedOne` (teamsync).
- **QX3.** Placements are provisional (`TestQX3APlacedQuestionIsProvisionalInTheIndex`); the
  queued sends in `sending` are placed again at a run's first compose
  (`TestQX3QueuedQuestionsArePlacedAgainBeforeACompose`); a compose waits while an earlier run's
  post is unechoed and is 20 not-ready at its deadline
  (`TestQX3AnUnechoedPostOfAnEarlierRunHoldsTheCompose`); an echo beaten to its number counts
  uncounted and frees the number (`TestQX3TheEchoReplacesThePlacementAndAnUncountedEchoIsCounted`);
  the sender posts to the questions room, writes `questions_unechoed` and tells the team
  (`TestQX3AQueuedQuestionIsPostedAndAwaitsItsEcho`), holds while not ready and drops a taken
  number (`TestQX3AQueuedQuestionWaitsForTheIndexAndIsDroppedWhenItsNumberIsTaken`); the fold's
  write clears it (`TestPersistClearsTheUnechoedPostWhenItsEchoIsFolded`,
  `TestOutboxFoldedClearsTheEchoAndCountsTheUncounted`). Tried questions sends are recovered
  from the questions room.

## Not built

- **QX7**: no redaction rebuild exists in teamsync (only main items react to `proto.Redact`); the
  proto slots and the builder's owed record exist.
- **The `uncounted` hook line** of QX3.
- **Step 5, every verb**: nothing of ask-human, thread, reply's link-back, close, retitle,
  questions' new flags or bump is built; `run.go`'s `unbuilt` is unchanged.
- **The `--check` duty hint**: the roster carries no duty (duty is host-side, package
  `teamchat/duty`), so no `hint` record can be emitted from the wire.

The handoff, with the suggested shape of each verb, is
`untracked/scratch/trackB-handoff-3.md` in the worktree (the guard refused a write to the main
tree's `untracked/`).

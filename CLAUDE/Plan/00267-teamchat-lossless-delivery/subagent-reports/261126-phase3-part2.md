# Plan 00267 Phase 3 part 2: the owed receipt test hang, and Task 3.2's replay half

## The hang (55e67368c)

- `TestAnOwedReceiptPostedTwiceIsOneEvent` hung intermittently, at HEAD and at 4fca5e018 alike:
  one in three `-count=5 -shuffle=on` runs under busfailpoints, and once in a whole-package
  `-count=2`. Every single run passed.
- The cause is a race in the test, not in the sender loop. The reports room is joined beside the
  session. When the test added its owed receipt before the join finished, the post was kept for a
  backoff (`errReportsNotJoined`, as Task 2.3 means it). That backoff runs on the fake clock, which
  the test never advances, so `sentOwed` waited on a `t.Context()` guard that cannot fire until
  `go test` times out. `TestAnOwedReceiptIsPostedInTheReportsRoom` and
  `TestARemovedTeamPostsNoMoreOwedReceipts` had the same race.
- The fix: those three tests start through `joinedOwedTeam`, which waits for `ReportsJoined`.
- Red: one transient 500 on the join made the original sequence hang every time (panic: test timed
  out, in `sentOwed`). Green: `-count=20 -shuffle=on`, with and without busfailpoints.
- In production a receipt kept before the join goes out within `maxBackoff` (30 s) of it.
- The class: a test that acts before an asynchronous precondition holds, then waits on an event
  that needs it. No rule can see it, since which precondition matters is the code's meaning, as
  with issue 140. A wait guarded only by `t.Context()` turns such a race into a silent hang. There
  are 40 such `select` guards in teamchat's tests (`grep 'case <-.*Context().Done()'`), and
  `project_test_spin` covers only yield loops. A sound replacement is missing too:
  `leakcheck.Idle` never holds in a world with a network poller. Whether to make this a rule is
  the coordinator's or owner's decision.

## The full teamchat-failpoints-test stage

Every package passed except `installplay` `TestThePlayReadsNothingTheTargetLacks`: the
`play-teamchat.yml` sudoers task of Plan 00266 C.3 (74cb85e8d) reads `root_dir`. That task is
not part of this work. It is reported to the coordinator.

## Task 3.2, the replay half (committed)

- `proto.Judge.Replay`, set by teamsync for a resume's answer and for `Refetch`. There a stale
  HUMAN post, PEER text or PING is delivered with `Verdict.Late`. `Pending.late` carries the mark
  (never on a TIMEOUT or a NOTICE), and the entry gets a `late:` line.
- A stale `halt` or `sync` (`proto.WaitsOnSender`) on replay is dropped `expired` and bounced.
  `expired` is a bounce reason only, so `DropReasons` (A 7.6) is unchanged.
- Off replay, stale is still dropped `stale` and bounced. QS6's test holds this.
- Each test was seen red first. Lint, vet and the project rules are clean with and without
  busfailpoints. The tests of proto, deliver, state, teamsync and engine pass with and without the
  tag.
- The delivery frame text (A 13.5) does not mention `late:`. The line explains itself.

## Task 3.2, the forge half (not built)

Handed off in `HANDOFF-phase3.md`, with the design worked out and the six existing tests that
must change.

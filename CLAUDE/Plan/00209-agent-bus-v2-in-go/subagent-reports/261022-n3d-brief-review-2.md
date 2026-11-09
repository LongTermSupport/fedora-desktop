# n3d brief review, round 2 (Plan 00209 N3, slice n3d)

Reviewed: `subagent-reports/261022-n3d-handoff.md` at 2f4bb7270, against round 1
(`261022-n3d-brief-review.md`). This round checks three things: whether each fix landed, whether a
fix added a contradiction or made a part too large, and whether anything from round 1 is still
open.

**Verdict: MERGE.** No blockers, no majors, 3 minors, 3 nits. Every round-1 finding has landed and
is consistent across sections 1, 4-11 and 12. The minors are small gaps that two of the fixes
opened. Paste-ready text is given for each.

## Round-1 findings: where each landed

| Round 1     | Landed in                                                                                                                                                                                                                  | Check                                                                                                                                                                                  |
| ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1          | n3d-D3; section 1; D1 `RosterCache` and `paths`; D3 `SaveRoster`/`Add`/`Remove`; D4 the call; D6a step 2; W e2e; tests                                                                                                     | consistent; the cache is only for composing, and D27 re-checks before the post, so the two fixes back each other up                                                                    |
| M2          | n3d-D2; section 1; D6a "Permanent"; SD3-8b                                                                                                                                                                                 | consistent; queued entries survive a stop and a retry, and only a waiting op's entry is removed                                                                                        |
| M3          | n3d-D29; section 1; D2 `ErrForgeDown` and its health; D4; SD5-8, SD5-r4/r6                                                                                                                                                 | consistent; the send side maps `ErrForgeDown` to 20 `forge`                                                                                                                            |
| M4          | n3d-D6; D3 pruning; SD4-9b                                                                                                                                                                                                 | correct: `handed` items are in `pending`, so they are covered                                                                                                                          |
| M5          | section 3; D6a steps 6-7; 2a/2c proto facts; SD3-5                                                                                                                                                                         | correct; the forge check runs before `Admit`, and the charge on the goroutine orders two racing sends                                                                                  |
| M6          | n3d-D30; D1; D3 Persist/Ack; D6a step 4; D6b; SD1-5, SD4-12                                                                                                                                                                | correct (see nit 1)                                                                                                                                                                    |
| M7          | section 0; D6a, D6b; W in wave 5; section 14                                                                                                                                                                               | sizes are fine: D6a still holds the charge, the sender, the recovery and D27, but `reply` has left it. D3 grew by the roster cache and the verb carry, and is still one builder's work |
| minors 1-15 | 1 D1 exits; 2 D12/D1; 3 step 7, SD3-9; 4 n3d-D4, D4 call; 5 n3d-D7, SD4-6; 6 n3d-D24; 7 n3d-D8; 8 D2 HTTP, SD5-7; 9 n3d-D25; 10 step 8; 11 n3d-D26; 12 RecoverSends, SD3-10; 13 sender; 14 n3d-D27, SD3-12; 15 n3d-D28, D5 | all landed                                                                                                                                                                             |
| nits 1-5    | D2 `Config`; 2a predicate; the sender's `do` and the non-blocking health send; RecoverSends; n3d-D1 text                                                                                                                   | all landed                                                                                                                                                                             |

Nothing from round 1 is still open.

## New minors

1. **The sender can post before `RecoverSends` has run** (D6a "The sender", "RecoverSends").
   The sender starts at `Add`. `Health` starts at its zero value (`Down` false), and `Notices().Send`
   works before `Run`. So after a link restart the sender can PUT a `tried` entry while the search
   for it is still running. For a homeserver that has forgotten its txns, that is the double post
   n3d-D4 exists to prevent: the search misses, then the sender posts; or the sender posts, then the
   search finds that new event and records the entry a second time. It also leaves D27 without a
   verified roster to check against. Paste into D6a, "The sender", first bullet:

   > It posts nothing for a team until that team's `RecoverSends` for the current start has
   > returned. That return also guarantees a verified roster for D27's check. Before then, new
   > sends to the team answer `queued`.

   Add SD3-10b: with fakehs up, an engine over a store holding a `tried` entry makes no PUT before
   `RecoverSends` returns.

2. **Adding methods to `team.Engine` breaks P6's fake** (D1 "Engine gains two methods"). P6 is
   still being built, and its ends tests use "a fake `team.Engine` that records `SetEnded`"
   (`261022-n3c-handoff.md` section 7). D1 stubs only the engine and teamsync's fake. Whichever of
   D1 and P6 merges second will not compile. Paste into D1:

   > Every `team.Engine` implementation on the branch gains the two stubs, P6's ends fake included.
   > If P6 merges after D1, the merge commit adds them there. W checks with
   > `go vet ./internal/teamchat/...`.

3. **`roster.json` is missed by the paths that move or delete a store without the link**
   (n3d-D3, D3). Two paths miss it. The keeper's timed-out-removal deletion (B 2.5, LN8) deletes
   the store only. `seat set-aside` moves the store only. Both leave `roster.json` behind, and a
   re-added seat that starts offline then composes from the old roster. Receivers and D27 check the
   addressing again, so nothing wrong is ever delivered, but it is still state left behind. Paste
   into n3d-D3:

   > `roster.json` goes wherever the store goes: the link unlinks it at removal, the keeper's
   > deletion after a timed-out removal unlinks it too, and `seat set-aside` sets it aside with the
   > store. A cache whose `rev` is older than a verification is replaced at that verification.

   Name the two callers (`keeper`, `seat set-aside`) in D3's hunk list, or record them as W's.

## Nits

1. D6a step 4 builds the Composer's lookup "as `engine.view` is", but `seam.go`'s `view` builds a
   ledger entry's `Related` with `Verb` empty. Say that `view` now fills `Verb` from the ledger's
   new `verb` (D3 or D6a; one place, used by the receive side and the send side).
2. D4 "Answers" names "the `Record` outcome for an `ack`", but n3d-D25 and proto
   (`verdict.go:428`) also make a stale answer `Record`. Write "the `Record` outcome (an `ack`, or a
   stale answer, A 8.5)".
3. D6a "ended with the team" can be read as ending at a stop. Write "ended at `Remove` or
   `Shutdown`; it lives through a stop and a retry (n3d-D2)".

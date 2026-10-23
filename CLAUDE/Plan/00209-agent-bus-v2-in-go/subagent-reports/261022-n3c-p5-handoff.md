# n3c P5 handoff: the sync engine (Plan 00209 N3, slice n3c)

The first P5 builder reached its context budget during design; the second built most of the
package and reached its budget too. Read "Second builder" first; the design below still holds.

## Second builder: where it stands

**Committed, green:**

- `cca490bf9` `matrix.StatusError(method, path string, status int, errcode string) *Error`: the one
  classing of an answer status, which `call.judge` now calls; nil for a 200. This is the fix for
  the draft's defect (a test could not build a classed `*matrix.Error`): a constructor in matrix
  with its own test (`TestStatusError` in `class_test.go`), not a seam in teamsync.
- `8a6816c15` `lookup.go` with `lookup_test.go` and `helpers_test.go` (the shared constants,
  `syncBuffer`, `baseRoster`, `human`/`reply`/`inThread`, `runID`): the bounded LRU (own linked
  list: `container/list` needs unchecked type assertions errcheck refuses), the resolver, the
  poison bound, `relatedOf`, `memLookup` (with a `skip` id for Refetch), `newLogger` (a stdlib
  `log.Logger`, prefix `teamsync <team>: `, nil writer discards).

**Committed as work in progress, NOT green** (the last commit, "WIP"): every test passes with
`-race`, but coverage is 73.0% and golangci reports findings in the test files only (cyclop on
`TestDeliversAHumanMention`; unparam on `mustTeam`, `runID`, `world.say`; unused harness helpers
not yet used: `writeRoster`, `pending`). The source files are lint-clean. Files:

- `teamsync.go`: `Config` (as designed, plus nothing), `Watcher`, `Team`, `New`, `Wake`, `Run`,
  `stopCode`/`isTrustError`, `retryAuth` (n3c-D12), `backoff`/`recovered` (SY8, down and back
  lines), `current` (re-read the pair, `reflect.DeepEqual`, rebuild the client), `anchorsOf`,
  `setRoster` (Role into Health), `freezeDetector` (SY12: `atStart`, `atAnswer(started)`).
  Seams on `*Team`: `onPoll`, `onWait`, `readPair`, `judge`, and `pages pageBound{size, max}`
  (default 100 and 20; a test lowers them to reach SY3's gap without 2000 sends).
- `session.go`: `session`, `verify`/`verifyMain` (403 then one Join), `start` (SY1 a/b/c,
  decided on `NextBatch != ""`, so it holds whether `ResetPosition` keeps `fresh` or not),
  `readd` (c), `resume` (b) with `marker`/`decodeMarker`/`markerContent`, `passed`, `cut` and
  `landmarks` (own join, own latest left/disconnected), `fill`, `ends` (Close, BeginRun, the
  once-per-Team back goroutine on Run's WaitGroup), `epochCheck` (SY6), `poll`, `frozen`,
  `handle` (invites, trust trigger, `eventsOf`, `judgeAll`, SY6, persist, `commit`, floor),
  `persist` (SY13 said once), `invites`/`inviteSenders` (SY11), `trustTriggered` (SY14),
  `judgeAll`/`take`/`unheard` (SY4, UH1), `itemKind`, `copyWindow`, the floor marker
  (`knowFloor`, `floorMoved`, `putFloorIfDue`, `putFloor`, `FlushFloor`).
- `ln6.go` (now `verbs.go`): `readMessage`, `roomOf`, `rosterFor`, `Verify`, `History`/`historyOf`, `Refetch`,
  `Notices`/`Roster` and the `notices` type.
- `watch.go`: `SeatWatcher`, `WatchSeat`, `Changes`, `Close`, `relevant` (parses events with
  `binary.NativeEndian`, no unsafe).
- Tests: `world_test.go` (the draft harness, plus `startTeam`, `healthed`/`backed` channels,
  `healths()`, `counts()`, `runs()`), `teamsync_test.go`, `session_test.go`, `run_test.go`
  (the error table, backoff, SY13, removal, step 4, plain back, New), `ln6_test.go` (now `verbs_test.go`),
  `watch_test.go`.

**Left, in order:**

1. Make it green: the test lint above (split `TestDeliversAHumanMention`; use or drop the unused
   helpers; give `mustTeam`/`runID`/`say` a second caller or fold them), then coverage to 90%+.
   The uncovered paths are mostly session.go's: SY1 (b) with a marker and a `released_ms` (held
   bundle, `w.place(token, &released)`) and the restored marks, an undecodable marker said once,
   SY3's fill and its gap (lower `tm.pages`), invites (pending once, reject leaves, accept joins
   and re-verifies), SY14's triggers and a failing one (a roster without me), a role change,
   UH1 (an emote; a not-addressed human message reaching no agent; a reply to peer's post not
   counted; the dedupe), Drop and Redact counts, SY4a's integration tests (a thread reply under
   my own say is delivered; a transient GetEvent fault persists nothing and `judge` is never
   called; a second event with the same relation costs no second fetch; a 404 counts nothing),
   SY10 (the PUT after a floor move, coalesced within 10 s of `Boot`, `FlushFloor` at once, a
   failed PUT one line), SY12 (unit test the detector at 2 s and 10 s and just below, then one
   integration test with `clk.Suspend`), Refetch, the notices before Run, History of the
   questions room and a ping and a notice.
2. SY7 cases 1-8 on fakehs (`Snapshot`, `RestoreTo`, `Inject` `DropAfter` and 429, `Restart`).
3. The tuwunel-tagged test (`tuwunelhs.Start(ctx, t, server)`, as `rooms/tuwunel_test.go`).
4. golangci with and without `--build-tags tuwunel`; `qa-changed` green; commit.
5. The SPEC-n3 sentence for n3-D20 (n3c-D11: own sends live in memory only, per team, inside the
   resolver, bounded at 1024; a link restart loses them and the fetch backs them), in the same
   commit as the code or the report.
6. The report `subagent-reports/261022-n3c-p5.md`.

**Decisions the second builder took** (add to the list below in the report):

12. `matrix.StatusError` is the honest way to build a classed error in a test (the draft defect).
13. Every batch is `Fresh: false`; SY1's step 01 answer is persisted even when empty, so a
    restart does not run step 01 again.
14. SY1 (b) judges the events after the earlier bound: an event is kept when it is after the
    marker's floor (or, with no marker, after the member's own latest join) or after
    `released_ms`.
15. The failure streak (backoff, down) and the 401 flag reset once an answer is handled (not
    merely received), so a full disk backs off instead of retrying each second.
16. History shows a ping's reference with trust `external`: it proves nothing, so an unproven
    reference is never shown trusted. `From` in History records is the sender's localpart
    (`FromID` the user id for a human); `Pending.From` and `Verified.From` are the user id.
17. History reads `main` or `questions` (the roster's room); Refetch reads `main` only (the
    questions room is n3f's); another room is `exits` usage.
18. A refused event parse in an answer counts as Dropped, with one log line.

## State (first builder)

- Branch `feat/bus-v2-n3c-p5`, worktree `untracked/worktrees/bus-v2-n3c-p5`.
- **Committed:** `proto.StateEvents(raw []json.RawMessage) []proto.StateEvent` in
  `proto/stateevents.go`, with its test (section 8a, "P5, trust"). It is a copy of `notice`'s
  unexported `stateEvents`; W points `notice` at it. proto is at 98.3%.
- **Drafts, not committed** (they do not compile without the package), in the worktree's
  `untracked/scratch/p5/draft/`:
  - `world_test.go` is the test harness. It sets up team `alpha` on fakehs on one
    `clock.Fake` (fakehs `Hold` = `clk.Hold`, `Now` = the fake's wall clock). The accounts are
    `me` (the worker under test), `coord`, `peer` (agent X), `deaf` (a worker with
    `human_text: false`) and the human `alice`, all joined to main. The roster is read back
    after `rooms.Converge`. It writes the seat directory's pair by rename (pool form, or held
    form with `released_ms`). It also has the fakes `fakeEngine` (store in memory, a `batches`
    channel, a `persistErr` hook, `preload`), `fakeEnds` and `fakeWatcher`. The `running` helper
    waits on the team's `onPoll` and `onWait` hooks, so no test sleeps or counts waiters.
  - `lookup_test.go` has tests for the bounded LRU, the resolver order, the 404/403 unknown
    set, the poison bound and `relatedOf`.
  - **One defect in the draft:** `&matrix.Error{Status: 502}` cannot be built in a test,
    because the class sits in unexported fields. Either give the resolver an injected
    classifier, or produce real errors through fakehs `Inject(Fault{...})`.
- Dumps of the dependencies' `go doc -all` are in `untracked/scratch/p5/*.txt` (matrix,
  fakehs, proto, state, bundle, notice, records, seat, homefs, paths, exits).
- **Not started:** the package itself, the SPEC-n3 n3-D20 sentence (n3c-D11), and the report.

## Package

`internal/teamchat/member/teamsync`, package `teamsync`. The directory is not `sync`, because
the package needs the standard `sync`. `New` returns `*Team`, which satisfies `team.Team`.

```go
type Config struct {
	Team paths.TeamID; Seat paths.Seat; Root *homefs.Root
	Run  paths.RunID // this link's run: a store whose run is this one gets no Close and no BeginRun (8a)
	Engine team.Engine; Ends team.Ends; Clock clock.Clock
	Watcher Watcher // nil: no bundle watch
	RemovedUserIDs []string; Plain bool; FirstSessionStart <-chan struct{}; Log io.Writer
	HTTP   func(matrix.Config) (*matrix.Client, error) // nil: matrix.NewMember
	Jitter func(time.Duration) time.Duration         // nil: crypto/rand; gosec refuses math/rand
}
type Watcher interface{ Changes() <-chan struct{} }
func WatchSeat(s paths.Seat) (*SeatWatcher, error) // inotify, IN_NONBLOCK fd through os.NewFile so Close unblocks Read
func New(cfg Config) (*Team, error)
```

The unexported test seams on `*Team` are `onPoll func(since string)` (called just before each
long-poll), `onWait func(time.Duration)` (called after the backoff's `Clock.Hold` is armed, so
`Advance` is safe), `readPair` (default `bundle.ReadPair`, so a test can inject `ErrTorn`) and
`judge func(proto.Judge, proto.Value) proto.Verdict` (counts calls, for gap 14's test).

## Design

**Run.** The loop is `session(ctx)`, classify, act. Run returns only when ctx ends (nil) or on
`team.ErrRemoved` (nil). A stopped team waits for `Wake()` inside Run, so a stop never ends Run.
`Wake` is a non-blocking send on a channel of capacity 1. The watcher's changes call `Wake`.

| Error                                                       | Action                                                                                                                                                                                                                                                                   |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `bundle.ErrTorn`, `fs.ErrNotExist`                          | wait for a wake, without setting `Stopped` (LN9)                                                                                                                                                                                                                         |
| `*exits.Error` from the pair                                | stop `bundle`                                                                                                                                                                                                                                                            |
| `matrix.ErrAuthRefused`                                     | n3c-D12: on the first one, re-read the pair. Changed: rebuild the client and restart at once. Unchanged, or a second 401: stop `token`. The flag resets on a successful answer.                                                                                          |
| `matrix.ErrForbidden`                                       | stop `forbidden`                                                                                                                                                                                                                                                         |
| `*proto.TrustError`, `*proto.RosterError`, a missing roster | stop `untrusted`                                                                                                                                                                                                                                                         |
| `errRestart` (epoch moved, freeze, new pair)                | loop at once                                                                                                                                                                                                                                                             |
| anything else                                               | transient: backoff from 1 s doubling to 30 s, minus jitter, through `Clock.Hold` (a wake ends the wait early). `Health.Down` once 15 s have passed since the streak's first failure, one log line going down and one coming back. A successful answer resets the streak. |

`Health` goes through `Engine.SetHealth`: Role, Down, Stopped (an exits code) and LastSyncOKMS
(`WallMS`).

**The session.** Re-read the pair on every session (MS3: read on each use). Keep the client if
the pair is `reflect.DeepEqual` to the one in use, otherwise build a new client and close the
old one. Then:

1. Verify: `RoomState(main)`, then `proto.VerifyMain(proto.StateEvents(raw), anchors)`. On a
   403, try `Join(main)` once and verify again. Anchors are
   `{bundle.ServerName, team.String(), MainRoom, UserID}`.
2. `Engine.Load(team, pair user id)`.
3. SY1 (`store.Sync.Fresh` holds exactly when `next_batch` is empty, the state invariant):
   - **(a)** not fresh, with an equal epoch: resume from `next_batch`. Not fresh with another
     epoch: `ResetPosition(epoch)`, then (b).
   - **(c)** fresh, and the pair's user id is in `RemovedUserIDs`: one sync without since, using
     `SyncFilterReAdd` (Limited with no PrevBatch: never fill), then persist `next_batch` with
     no items.
   - **(b)** otherwise:
     - Read the marker with `RoomAccountData(self, main, paths.FloorAccountData)`. Decode it
       strictly as `{"v":1,"floor_ms":N,"restore_epoch":N}`. An undecodable marker is none,
       and is said once.
     - Take `released_ms` from `Held`; a guest's bundle has none.
     - Sync with no since. Fill backwards from `prev_batch` (100 per page, 20 pages), stopping
       once past both bounds. A missing marker's bound is passed at the member's own join
       event; a missing `released_ms` counts as passed. Past 20 pages, set `Gap` and count it.
     - Judge oldest first. Mark an item restored when its time is at or before `released_ms`,
       or at or before the member's own latest `left`/`disconnected`.
     - Skip the state section's trust trigger on this first answer.
4. If `store.Run.ID != cfg.Run`, call `Ends.Close(prev)` then `Engine.BeginRun`, after the start
   step and before the first long-poll. Once per Team, and not after (c), start a goroutine:
   wait for `FirstSessionStart` (or nothing under `Plain`), then `Ends.Back` with the reloaded
   store's run. Its error is one log line.
5. SY6 read, then the long-poll loop. Each request is
   `Sync{since, 30 s, proto.SyncFilter(roster, a).Compact()}` with the default presence. The
   filter is rebuilt from the current roster each time (SY11's role change). Each answer goes
   through:
   - **Invites (SY11).** Keys are sorted. The inviter and the create sender come from
     invite_state. `proto.Invite` decides: Accept is `Join`, then re-verify; Pending is logged
     once per room; Reject is `Leave`. A failed Join or Leave is logged and does not hold the
     team, by the owner rule.
   - **Trust (SY14).** Re-verify when the timeline holds a type in `proto.StateTypes`, the state
     section is not empty, the timeline is limited, main is in `Leave`, or a room was joined. An
     update of the roster updates the Role.
   - **The gap (SY3).** When the timeline is limited and has a `prev_batch`, fill backwards with
     `To = since` (the bound as above). The events are deduplicated by id.
   - **Judge**, below.
   - **SY6.** Read `GetState(main, RosterType, "")`, then `proto.Parse` and `proto.DecodeRoster`.
     A 404 or a refusal is `untrusted`. An epoch other than the batch's discards the answer,
     calls `ResetPosition(new)`, and returns `errRestart`. This read runs after every answer and
     before the first long-poll of each session, so "after a failure or a resume" is the
     session restart.
   - **Persist.** An empty batch (no items, redacts, counts, floors or gap) is not persisted,
     but `since` moves on in memory. Otherwise `failpoint.Hit(LinkAfterJudgeBeforePersist)`,
     then `Engine.Persist`. `ErrRemoved` ends Run. Any other failure is SY13: said once per
     streak, then the transient path with the position held.
   - **After a persist.** Commit the deferred effects: the flood window copy, the UH1 ids and
     the log lines.
   - **SY10.** When the floor moved, PUT the marker. It is coalesced without timers: PUT after
     an answer only when 10 s of `Boot` have passed since the last PUT, so at most once per
     10 s. `FlushFloor` PUTs at once. A failed PUT is one log line.
   - **SY12.** A `freeze` detector reads `Boot()-Mono()` at each request's start and answer. A
     growth over 2 s, or a request finishing more than 10 s past its `Boot` deadline (start +
     45 s), closes the client's connections, logs one line with the pause, and returns
     `errRestart`. Unit test the detector at both thresholds with `Fake.Suspend`, and add one
     integration test.

**Judge** (SY4, SY4a, UH1, gap 14). Only main's timeline is judged; the questions room is n3f's.
The steps:

1. Parse each event. Leave out state events (those with a `state_key`). A refused parse is
   dropped and logged.
2. Collect `proto.RelationIDs` (pairs of event and relation).
3. Call `Engine.View(team, ids, relationIDs, roster)`.
4. Resolve. The order is the view, the own sends (`bounded`, 1024), the LRU cache (1024), the
   unknown set (1024), and only then `GetEvent(main, id)`.
   - A 404 or 403 goes into the unknown set.
   - A transient failure returns the error, so the whole answer is held unpersisted and
     `Judge` is never called.
   - After 5 transient failures in a row for the same id (n3c-D9), the id goes into the unknown
     set, with one log line naming the event and the related id.
   - `relatedOf` fills a fetched event's `Related`:
     - for a human: `DecodeHuman`. `AddressedToSelf` is set by a mention of self, `@room`, or a
       reply or thread root that memory says self sent. `AddressedToRosterAgent` uses the same
       rules with `Roster.HumanReader`.
     - for an agent: `DecodeContent` over `MainKeys` (`AdminMainKeys` for the admin), judged by
       its `to`, and `Verb` for a ping.
   - Relations are answered from memory only, never fetched in turn.
5. `memLookup{view, resolved, judged}` is the `proto.Lookup`. Its `Seen` also covers ids
   already judged in this batch.
6. Judge each event with
   `proto.Judge{Server, Self, RoomID: main, Purpose: main, Roster, NowMs: WallMS, Lookup, Flood: &floodCopy}`.

How each outcome is handled:

| Outcome                | What happens                                                                                                                                                                       |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deliver                | `team.Item{Pending{ID, Room: "main", Item: records kind (HUMAN→human, PEER→peer, PING→ping, NOTICE→notice, TIMEOUT→timeout), SentMS: origin ts, From: sender, Restored}, Verdict}` |
| Ignore                 | `Ignored++`, except `seen`                                                                                                                                                         |
| Drop                   | `Dropped++`, and one deferred log line                                                                                                                                             |
| Redact                 | `Redacts` and `Redacted++`                                                                                                                                                         |
| Record, Index or Fault | nothing (n3d's or n3f's); `Answered` stays nil                                                                                                                                     |

UH1 counts an event when `Class == SenderHuman`, the outcome is Ignore, and the reason is
`type` or `empty`, or `not-addressed` with `proto.ReachedNoAgent(v.Human, roster, lookup)`.
Each id counts once, through the 1024-entry set, which is committed only after a persist
(n3c-D10). The log line names the id and the reason, never the body. `Floors["main"]` is the
newest origin ts judged, when it is above the last floor.

**Notices** (8a: works before Run). Each method takes the current client, which is built
lazily from the pair. `Anchors()` and `Token()` give empty values when the pair cannot be read.
`Roster()` gives the last verified roster. `RecordOwnSend` goes to the resolver.
`SetPresence`, `RoomState`, `Messages` and `Send` delegate to the client.

**LN6.**

- `Verify(ctx, id)`: `GetEvent(main, id)`, then decide authenticity by the roster's sender
  class with `DecodeHuman` or `DecodeContent` (not addressing). The result is
  `records.Verified{Item kind, From, Room "main", Text, BodySHA256 hex}`. A 404 is
  `exits.New(CodeNotFound)`; an unknown sender or an undecodable event is `CodeUntrusted`.
- `History(room, limit)`: `Messages` backwards, `Types [m.room.message]`, at most 100,
  returned oldest first as `records.History{Dir in/out}`.
- `Refetch`: `GetEvent`, then `Judge` with a fresh flood window and a lookup whose `Seen`
  leaves out the id itself.

## Tests still to write

Every test named in SPEC-n3 SY1-SY14, SY4a and UH1. SY7 cases 1-8 run on fakehs with `Snapshot`,
`RestoreTo`, `Inject` (`DropAfter`, 429) and `Restart`, on the one fake clock; case 3's late
drop times out on the 45 s `Timers` deadline by `Advance`. Then:

- gap 14's "Judge never called";
- the poison bound;
- the token race (old seat.json with a new token, a 401, a re-read, success);
- minor b (a user id not in `RemovedUserIDs` resumes by (b));
- LN9: a torn pair read again on the second event; the hot reissue (`MintToken`, the token
  placed by rename, the old device logged out);
- the compile-time assertions `var _ matrix.Timers = clock.Clock(nil)` and
  `fakehs.Options{Hold: (*clock.Fake)(nil).Hold}`;
- a tuwunel-tagged test on the real binary (`tuwunelhs.Start(ctx, t, server)`, as
  `rooms/tuwunel_test.go` does), with `FAKEHS_TUWUNEL_BIN` from the common file.

## Decisions taken so far (for the report)

01. The package is `teamsync`, and `New` returns `*Team`.
02. `Config` gains `Run` (8a needs it) and `Jitter`.
03. A stop waits for `Wake` inside Run.
04. An empty answer is not persisted, but SY6 still reads after every answer.
05. Join and Leave failures in SY11 are logged and do not hold the team (the owner rule).
06. The floor is coalesced without timers.
07. A freeze discards the answer and restarts the session.
08. The unknown set is bounded at 1024, as the cache is.
09. `seen` ignores are not counted in `Ignored`.
10. `Verify` judges authenticity, not addressing.
11. `History` is oldest first.

**The owner rule.** Each team has its own goroutine and its own client. Nothing blocks across
teams. A down team backs off with a bound of 30 s and is reported `down`. A poison relation is
bounded at 5 tries. `Back` runs in its own goroutine.

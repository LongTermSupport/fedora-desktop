# n3c brief review (Plan 00209 N3, slice n3c)

Adversarial review of `261022-n3c-handoff.md` at a5fca1a83, against SPEC-n3.md sections 4, 8, 9
and 10, DESIGN-v2-B-member.md 2.3-2.6, 2.10, 2.11, and the built packages' code and `go doc -all`
at that tip (matrix, fakehs, proto, member/state, lock, bundle, homefs, cli, exits, records,
paths, reexec, execx, notice, seat).

**Verdict: FIX-FIRST.** The split and the order are sound, and the quoted built APIs match the
code (P1's line cites included: `filter.go:35`, `strictjson.go:149`, `client.go:373/387`,
`member.go:171`, `sync.go:100`, `syncGrace`). But P2's `member/team` and `wire` seams are not
complete: P6 cannot reach the token, the engine cannot create a store or hand a store's run to a
new session, the keeper cannot learn a no-team session's first SessionStart, and three verbs'
args lack their team. Those are notes 1-12 for the P2 builder. P1 has one wrong fact (note 13).
Sections 4-9 are fixed in place: section 4 directly, sections 5-8 by the new section 8a, and
section 9 by five new decisions (n3c-D19 to D23) and two corrected ones.

## Notes for the P1 and P2 builders (not applied: those parts are being built)

**P2 (section 3)**

01. Line 345-351, `team.Notices`: it has no token, so P6 cannot fill `proto.Composer.Token` (the
    own-token screen of every notice body) or `notice.Notice.Token` (EN3's `notice.Disconnected`).
    Add `Token() string` (the held pair's token; never logged or formatted).
02. Line 330, `Engine.Load(t)`: the engine cannot create a store. `state.NewStore` needs the
    user id, and the engine reads no bundle. Make it `Load(t paths.TeamID, userID string)`.
03. Lines 329-336, `Engine`: nothing replaces the store's `run` with this link's run. EN3 needs
    the previous run first, so add `BeginRun(ctx, t) error`: write `run` = this link's run with
    `ended` null, a no-op when it already is. The team calls it after `Ends.Close` returns nil.
04. Lines 337-344, `team.Team`: add `FlushFloor(ctx) error`. SY10 puts the marker at a removal
    before the store goes. That is the engine's step (P4), and the engine holds only `team.Team`.
05. Line 292, `state ... session_started`: a per-team message cannot reach the keeper of a session
    with no team (T6). Every `--claude` CMD exit there would then be `launch-failed`. Make it a
    session-level link-to-keeper message, `started {}`, sent once (n3c-D6 corrected).
06. Line 292, `state`: add `handle`. `report.json` refuses a missing handle or one not of the seat
    (`state/report.go` `check`), and the keeper reads no bundle. Drop `link`: that is the keeper's
    own fact.
07. Lines 260-263, args: `WaitArgs` and `VerifyArgs` need `Team *paths.TeamID`, because
    `cli/verbs.go:157,188` give `wait` (multi-team) and `verify` (one team) a `--team`. Give
    `HistoryArgs` and `TeamsArgs` JSON tags (`team`, `room`, `limit`).
08. Line 268, n3c-D3, the 8 KiB message cap: `launch` carries `cwd` and `claude_config_dir`, each
    up to PATH_MAX (4096), so 8 KiB cannot hold it. Use 64 KiB. `Recv` must:
    - receive with `MSG_CMSG_CLOEXEC`;
    - refuse `MSG_TRUNC` and `MSG_CTRUNC`;
    - close every descriptor of a refused message.
09. Lines 210-211 and 387: `matrix.Timers` and `fakehs.Options.Hold` are P1's, which is built at
    the same time. So `clock`'s compile-time assertions against them, and the test of a matrix
    deadline through `AfterFunc`, do not compile on P2's base. P2 asserts `lock.Clock` and
    `matrix.Clock` only. P5 adds the other two assertions and the test (section 8a).
10. Line 327, `Health`: add `LastSyncOKMS int64`. `report.json` and `status` need it, and an empty
    sync answer is never persisted.
11. Line 314, `Batch.Floors`: the key is the room's purpose (`main`, `questions`), as in the
    store's `floor_ms` and `Pending.Room`. It is not a room id. Say so in the comment.
12. Line 373, `bundle.ReadPairAt(root, dir, team)`: EN5 has no team. `seat disconnected` takes
    `--bundle`, `--since` and `--seed` only. Take `team *paths.TeamID`: nil means seat.json's own
    team, without the check against the directory's name.

**P1 (section 2)**

13. Lines 98-101, `ReachedNoAgent`: "A relation the lookup misses counts as reaching no agent" is
    backwards. After SY4a's resolution, a miss is only a 404 or 403. n3-D20 and SY4a say that
    case is "not counted unheard": it undercounts and never miscounts. So a reply, thread root or
    edit original that the lookup misses makes the predicate **false**. Include that row in the
    table test.

## Findings in sections 4-9 (applied to the brief)

14. Section 4, lines 407-410: "imports no proto" cannot hold transitively, because the keeper
    imports `state` and `cli`, which both import `proto`. Applied:
    - B14's scan is transitive (`go list -deps`: no `matrix`, no `net/http`);
    - B11's scan covers direct imports only (no `proto`, `bundle`, `seat` or `team`).
15. Section 4, line 422: `execx.Cmd` already has `NewSession`, which is setsid. A `Setsid` field
    would duplicate it. Applied: reuse `NewSession`.
16. Section 4, `reexec.Command`: `reexec`'s helpers block on a stdin line (`Ready`), and the
    keeper gives the link stdin `/dev/null`. Applied: `Command` helpers are free-running, and
    their tests wait on the `wire` `ready`.
17. Section 4, lines 508, 543 and 559: the built `state.Report` has no `why`, no launch failure
    and no holder run. Also, `report.json` lives in the seat directory, so a keeper refused a held
    seat would overwrite the holder's report. Applied as n3c-D21:
    - `why` is added;
    - CMD's stderr line stays in `keeper.log`;
    - the keeper writes `report.json` only for seats it holds;
    - the holder's run id goes to `keeper.log` and to `status`.
18. Section 4, line 483: `--resume-recorded` named no rule for checking that the transcript
    exists. Applied: `fstatat AT_SYMLINK_NOFOLLOW`, a regular file, never opened (no FIFO).
19. Section 4, line 474: the kit's `settings.json` is host-placed, so it may not be the member's
    file. homefs would refuse it, and nothing else was named. Applied: no-follow, non-blocking,
    regular, capped, owned by the uid or root, not group- or other-writable.
20. Section 4, lines 437-445 and 521: Claude Code under a wrapper is not the keeper's child, so
    `execx.Started` gives no pidfd for it. Applied: a `Pidfds` seam, with the start time
    re-checked after the open.
21. Section 5, lines 634-645 (section 8a):
    - `engine.Config.Ends` is a construction cycle, because the ends need `Engine.SetEnded`. It
      becomes `NewEnds func(team.Engine)`.
    - `TeamConfig` lacks four fields `teamsync.Config` needs: `Ends`, `Root`, `Plain` and
      `FirstSessionStart`.
    - `EngineFactory` was undefined.
22. Section 5: the link half of RC1, and of the `desired`, `refused`, `token`, `owed`, `claude`
    and `end` messages, had no owner. Section 8a gives all of them to P4.
23. Section 5, the removal: SY10 puts the floor marker before the store is deleted, but B 2.5's
    10 s for the `remove` answer covers only local work. Applied as n3c-D22: the PUT is bounded
    to 2 s and best effort.
24. Section 5, `owed`: to post the leave, the link needs a team client for a team it does not
    run. Section 8a: `Team.Notices()` works without `Run` (P5's contract).
25. Section 5, line 640: `Serve` returns an `ack`, but no method delivers on the ack or returns
    the items on a timeout. Section 8a adds `Ack` and `Return`.
26. Section 6, lines 832-837 (section 8a):
    - Close and `BeginRun` run in that order.
    - On a link restart within the same run there is no close.
    - Whether a fresh store is (b) or (c) is decided again from `RemovedUserIDs`, because the
      store has no field for it.
    - "P6's `Back` checks launch-failed" is vacuous and is replaced: `Back` posts only when
      `LatestOwn` finds `disconnected`.
27. Sections 4 and 6 (RC1, SY8): "a NOTICE" has no seam in n3c, which has no locally made item.
    Applied as n3c-D23: n3c shows both cases in `status` and `status.json`; the NOTICE and the
    hook line are n3e's. The departure from SPEC-n3 SY8 and RC1 is recorded in the P3 and P5
    reports. **For the lead:** add one line to SPEC-n3.
28. Section 9, n3c-D9: as written, an outage could poison every relation of a batch, and its
    arithmetic was wrong (the waits are 1, 2, 4 and 8 s, about 15 s, not 31 s). Applied:
    - a failure counts only when the sync answer itself arrived;
    - the log line names both ids;
    - the time is corrected.
29. Section 9, n3c-D18 and section 7, EN3: an `attempted` end that is re-sent blind can be a
    second goodbye. The link's send goes unanswered, the link exits and frees the seat, the
    host's pass posts `tc-disc-<run>`, and the next link re-sends by its `tc` txn. That breaks P5.
    Applied as n3c-D19: an `attempted` end is judged from the room first.
30. Section 7, EN1: after `launch-failed`, `run.ended` stays null, so the next take would close
    the run as `disconnected`. B 2.11 says nothing is posted for such a run. Applied as n3c-D20:
    record `none`/`launch-failed`. Also, `End` skips a team whose run never began.
31. Section 7, line 1049: the client's `kit` was taken from `TEAMCHAT_KIT`'s digest. ccy's kit has
    no digest name, and B 2.4.4 checks the CLI's own build. Section 8a: it is `cli.Env.Digest`.
32. Section 7, LN3: one row was missing: `keeper.lock` held and the link `running` (not yet ready).
    Section 8a: that is 69 `no-link` with no restart time.
33. Section 6: P5 cannot import `notice`, but it needs `notice`'s unexported `stateEvents` to
    call `VerifyMain` on `RoomState`. Section 8a: `proto.StateEvents`, and W makes `notice` use
    it.
34. Section 6: round-3 minor b had no test. Section 8a adds one to P5's list: a new user id is
    re-added by SY1 (b).
35. Section 8, W: `teamsync.New`'s type is not `NewTeam`'s. Section 8a: W writes the adapter.

## Checked and sound

- Every quoted built signature matches the code. That covers:
  - `Related` and `Lookup`;
  - `SyncRequest`, `RoomFilter`, `Clock` and `Error.Transient`;
  - fakehs `Options`;
  - `Invite`, `VerifyMain`, `Composer.Notice` and `Judge`;
  - `lock`'s `TakeKeeper`, `TakeSeat`, `TakeLink`, `AdoptSeat` and `Dup`;
  - `state`'s `WriteLockRecord`, `NewStoreWriter`, `StoreRun`, `RunEnd`, `Pending`, `Sync` and
    the `^tc[0-9a-f]{32}$` txn;
  - `bundle.ReadPair` and `ErrTorn`;
  - `seat.MemberBundle` and `Held.ReleasedMS`;
  - `notice.Disconnected`, `TxnID` and its page bound;
  - the `exits` codes (`size`, `runtime`, `no-link`, `link-failed`, `no-session`, `token`,
    `unreachable`, `bundle`, `launch`);
  - the records `Verified`, `History`, `More`, `KeeperCost` and `LinkCost`;
  - `paths.InboxSocket`, `Run.*`, `Seat.Report` and `FloorAccountData`;
  - `cli.Launch` and `Env`;
  - `execx.Cmd` and `ExtraFiles`.
- Ownership:
  - every section 4 expectation has an owner;
  - section 8's RS1, RS2 and UH1 have owners;
  - gaps 5-7, 9, 11, 12 and 14 have owners;
  - PT1's eight n3c failpoints, plus n3c-D2's, are each owned once (P3 three, P4 four, P5 one,
    P6 one);
  - minors a-f and the token race are placed;
  - minor c is closed by n3-D20's text.
- n3c-D1, D2, D4, D5, D7, D8 and D10-D17 are simpler, safe and testable. No n3c step has root
  read a member-written file. EN5 reads only the host-held bundle. The no-follow and no-FIFO
  rules hold, with findings 18 and 19 applied.

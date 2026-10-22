# n3d D6a handoff: `say`, `ping`, the charge, the sender, the recovery (WIP)

Worktree `/workspace/untracked/worktrees/bus-v2-n3d-d6a`, branch `feat/bus-v2-n3d-d6a`. The first
builder stopped at its context budget. The WIP commit **does not build yet**: finish it, then go
green, then write `261022-n3d-d6a.md`.

## Done (in the WIP commit)

- `proto/send.go`: `func (o Outgoing) DuplicateKey() string` ("" when exempt from the window),
  test `TestOutgoingDuplicateKey`. Needed because the store keeps the window as SHA-256 digests
  of the key while `SendGate.Admit` checks the raw key: `sendGate` maps the one matching digest
  back to the raw key, `storeGate` writes the digests back (keys older than 60 s dropped).
- `member/engine/engine_test.go`: `fakeNotices` gained `hs *matrix.Client` (fakehs-backed sends
  and reads), `token`, `sendErr`, `messagesErr`, `txns`, `contents`, `own` (RecordOwnSend); with
  no `hs` a send returns `evID(9000+n)`. `world` gained `mains` (the main room the seat.json
  bundle names, for fakehs teams) and `forge *fakeForge` (default proven), and passes
  `Forge: w.forge` in `config`.
- `member/engine/send_test.go` (pure fakes) and `sender_test.go` (fakehs: SD3-2, SD3-10, a
  failing recovery): every test of section 9 is written, named by expectation id.
- `member/engine/send.go`: say, ping, resolving (`addressed`, `candidates`, `resolve`,
  `members`), `send` (steps 3-8), `charge`, the gate mapping, `refusal`, `forgeCheck`,
  `sendingOf`, `storeLookup` (a proto.Lookup over a store copy, roster applied by `with`),
  `queuedRecords`.

## Left

1. **Compile fixes**: `candidate` in send.go collides with an existing name in the package
   (rename to `sendCandidate`). `sender_test.go`'s `mapKeys` should be `maps.Keys`.
   `send.go` uses helpers not written yet: `ledgerRelated(l state.Ledger, main string, r proto.Roster)`
   and `pingRelated(p state.Ping, self, main string, r proto.Roster)`: extract them from
   `view` in seam.go (and make `roomID` take the main room id instead of `*held`), so view and
   storeLookup share them.
2. **engine.go**: `Config.Forge team.Forge` (nil: a referenced ping is 20 forge-down).
3. **teams.go / held**: add `recovered bool`, `poke chan struct{}` (cap 1), `waiting map[string]chan sendResult`, `unsent int64`; init `poke`/`waiting` in Add; `func (h *held) pokeSender()` (non-blocking send). `start` runs `go e.sendLoop(runCtx, h)` beside the team
   goroutine (h.cancel ends both). `drop`'s LN8 line gains `and %d queued sends`
   (test wants "2 queued sends"), and answers each waiter with notHeld.
4. **seam.go**: `SetHealth` pokes the sender after recording health. Replace the no-op
   `RecoverSends`: on the goroutine collect the `tried` entries, main room, self, cached roster;
   if any, ONE `n.Messages(ctx, main, {Dir: Backward, Limit: 100})` (decision: one read for all
   entries, not one per entry); for each entry, an event with sender self, type m.room.message,
   content `proto.Value.Equal` to the entry's, `origin_server_ts >= since_ms` is recorded with
   `recordPosted(t, entry, id, origin)` and `n.RecordOwnSend`; a read error is returned (teamsync
   backs off and calls again). Then `h.recovered = true; h.pokeSender()`.
5. **sender.go** (new; its test file exists): `sendLoop(ctx, h)`: `nextSend` on the goroutine
   returns the oldest entry when `h.recovered && h.team != nil && store has sends && Stopped == ""` and either no hold and `!Down`, or a hold whose `LastSyncOKMS > afterSync && !Down`, or whose 30 s timer (`e.cfg.Clock.Hold(30*time.Second)`, transient holds only) fired;
   else wait on `h.poke`, the timer or ctx. `post`: roster = `Notices().Roster()` else the
   cached one; re-check (n3d-D27) by decoding the entry and re-composing it (`Text(dc.Text, dc.Relation.InReplyTo, dc.Body)` or `Ping(dc.Ping, root if dc.Relation.IsAnswer(), dc.Body)`)
   with the current roster: a refusal removes the entry, `h.unsent++`, logs "... was not posted:
   <reason>" (test greps "not posted"), answers a waiter with `refusal(...)`. An untried entry is
   marked tried in a store write before its PUT. `Notices().Send(ctx, main, Type, Txn, Content)`.
   Posted: `failpoint.Hit(LinkAfterSendBeforeRecord)`, `recordPosted` (one write: entry removed
   by txn, an outbox ping for `Ping != nil` with `SentMS` the wall time, `DueMS` +TimeoutMS,
   `Answered`/`Reported` `[]int{}`, `pruneOutbox` + `h.forget`, `e.arm()`, the waiter answered
   with the id), then `RecordOwnSend(id, Related{Sender: self, RoomID: main, Verb: ping verb or "", AddressedToRosterAgent: slices.ContainsFunc(to, r.HumanReader)})`. Errors:
   `matrix.ErrAuthRefused` -> token, `ErrForbidden` -> forbidden: remove only the entries with a
   waiter, answer them 21/22, hold permanently (until a newer LastSyncOKMS with Stopped "");
   another non-transient `*matrix.Error`: drop the entry, unsent++, waiter internal
   (`msgServerRefused`); transient: hold 30 s or a newer sync.
6. **status.go**: `teamView` gains `sending`, `unsent`; `teamRecords` sets `Counts.Unsent` and
   appends `queuedRecords(...)` after the outbox records.
7. **serve.go**: `OpSay` -> decode `wire.SayArgs`, `e.say(ctx, a, req.Stdin, out)`; `OpPing`
   likewise. `serve_test.go`'s `TestServeRefusesBadArgumentsAndUnbuiltOps` uses `OpSay` as the
   unbuilt op: change it to `OpReply`.
8. `TestSD3_7ADeadlineWhileWaitingAnswersQueued`: before `w.clk.Advance(30s)` wait for the
   sender's hold (`w.clk.BlockUntil(ctx, 1)`), or the advance can race the timer's arming.
9. Then: `go test -race -cover ./internal/teamchat/...`, golangci-lint (no tags, tuwunel,
   busfailpoints), `qa-changed`, the report `261022-n3d-d6a.md`, commit. Final message < 600
   chars.

## Decisions taken so far (for the report)

- One `/messages` read per recovery, not one per entry (the same page serves every entry).
- With no `--team`, a team with no roster is skipped; if no team resolves and some had no
  roster, 20 unreachable, else 64 unknown-member. `--to` and `--to-coordinator` are exclusive (the
  CLI's rule too).
- A ping's body is `<verb> [<ref>] -> <short names>` (display only, A 12.1); a text's body is the
  text.
- Proto refusals: sender/role 11 `role`; duplicate 15; rate 14; allowlist/excluded 10 `ref`;
  a reason in exit 10's set keeps its code; the rest (version, edit, stale) 10 `schema`.
- A context end before the charge is 4 timed-out (nothing spent); a forge with no answer or an
  ended forge context is 20 `forge-down` (D1's code); a nil `Config.Forge` is 20 `forge-down`.
- A store write that fails at the charge is 78 `store` (logged).
- `tried` is set at the charge only when the team's recovery is done and the send is not queued.
- A team not yet recovered but not Down: the op waits (does not answer queued at once).

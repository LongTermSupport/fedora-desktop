# n3e-13, PT1 slice 3: the pure half built; the runner, the planted defects and the redelivery row not built

Plan 00209 Phase 3, Task 3.2. Base fast-forwarded to `feat/bus-v2-n3e-wire` (head `19637f05b`, the fourth merge of F44;
`pt1_run_test.go` present). This agent's context passed its budget (about 300,000 tokens) during the design and the pure
half, so it stopped at a clean committed point. **(A), (B) and (C) are not built as asked: P2, P4 and P5 are still not
reached by any real run, and nothing was shown red against production code.** What is built is the part that needs no
process, and a worked design for the rest that the next agent can build without repeating the investigation.

## 1. What is built and committed

All in `src-go/internal/teamchat/member/wiring/`, `busfailpoints`, no production file touched:

- `pt1_model_test.go`: four more operations, `recv-fail`, `remove`, `end` and `die`; the generator rules for them; the
  checks P4, P5 and P2 for returned items.
  - `remove` is a desired generation that releases the seat. It is drawn only after 6 operations, never while the inbox
    is held, and only while a `say` is still within its cap; after it only `say`, `peer` and `kill-link` are drawn (no
    recv, wait or send, which need the seat, and no homeserver fault, which could lose the leave's post), 1 to 3 of them;
    a schedule with a removal and no post after it gets a `say`, so that P4 has an input.
  - `end` (SIGTERM of the keeper) and `die` (SIGKILL of CMD) are drawn once, after the body (30 % and 15 % of the
    seeds), and are the last operation. The body of a seed does not depend on its ending.
  - `recv-fail` is recv whose output fails.
  - `pt1Seen` gained `want` (the one notice the ending must have posted), `removed`, `atRemoval`, `afterRemoval` and
    `marked`.
  - P4 (`pt1Removal`): no item copy reaches the transcript after the removal (the count at the removal is the
    boundary), and an item posted to main after it reaches no recv.
  - P5 (`pt1Notices`), stricter than slice 2's: at most one goodbye (a `left` of any reason) and one `disconnected` in a
    run; the ending's notice (`want`) posted exactly once; a run that did not end posts none. The last two stop the check
    passing for want of a notice to judge.
  - P2 (`pt1Items`): beside "a second transcript copy carries repeat", every copy after the mark of an item the link
    returned to pending (`marked`) carries `repeat:`.
- `pt1_model_unit_test.go`: the bounds test knows the new rules (`pt1Bounds`, with its state in `pt1Walk`);
  `TestPT1RemovalAndReturnedItemChecks` (P4 three ways, P2 for a returned item three ways) and, in
  `TestPT1ChecksFireOnTheirOwnProperty`, the new P5 cases (twice, two reasons, never posted, posted in a run that did not
  end, and the quiet ones). `TestPT1FixedSeedsDrawEveryOperationTheRunnerHas` replaces the old fixed-seed test.
- `pt1_run_test.go`: `pt1Unbuilt` (the four kinds) and a loud `t.Fatalf` in `pt1RunAt` for a schedule that draws one;
  `pt1Fixed` is now 7, 15, 27, 38, 47, 55, 56, 71, 92, 95, 100, 108, seeds whose schedules draw none of the four and draw
  every other kind between them; `TestPT1Points` row i runs `pt1Fixed[i]` (it used seeds 100 to 107).

So the gate runs the same kinds of run as slices 1 and 2, on new seed numbers (the new kinds change the stream), and
`-pt1.seed=N` or `-pt1.random=<n>` fails loudly on a seed that draws an unbuilt kind.

## 2. Red and green

Test first. The new unit file against the old model did not compile (`undefined: pt1Remove`, run recorded). After the
model: `go test -tags busfailpoints -run 'TestPT1(Schedule|Fixed|Checks|Reads|Transcript|Every)'` ok (0.014 s). The
full `-run TestPT1` result and the lint and vet results are in section 6. No planted defect was run: the runner that
would show it does not exist.

## 3. Findings from the investigation (what the next agent needs)

These come from reading the code (`inbox`, `fakeinbox`, `engine`, `client`, `link`, `keeper`, `ends`); none was run, so
each is a claim to confirm by the row that builds on it.

1. **The fakeinbox cannot make a second transcript copy, and the fake Claude Code cannot either.** The link writes the
   user line (at most 12 KiB) in one `write`, which completes into the socket buffer before the fake reads it, so no
   fault of the fake can fail the link's write after the line is in the transcript. A failure after that is `Finish`'s,
   and `inbox.go` says it never undoes a full write (E11). A cut line (`ErrPartial`) needs a line larger than the
   buffer. `ErrRetry` (nothing written) leaves the transcript empty and `unawait` returns the items, with no repeat, to a
   retry 10 s later (`retryWait`); an `awaiting` entry of unknown outcome waits 180 s for `missing`. `Hold` does not
   block the write either: with the inbox held, the link's write succeeds, the items count as delivered, and the
   transcript gets the line when the hold is released (its `Finish` runs out at the 5 s phase bound). So with no
   production change and no clock seam a transcript never holds two copies of one item.
2. **A redelivery that reaches the transcript in gate time exists, by another route: the hand-out's return.** A recv
   that hands items out and is never acknowledged (the client does not ack when its stdout fails, `client.go`, `finish`;
   the link then `Return`s them, `server.go` `settle`, `engine/recv.go` `settleStore`) leaves them in pending marked
   `repeat`; the next socket delivery writes them with the `repeat:` line, 2 s after the last delivery (`paceWait`), so
   the row is about 3 s. Build it in process, with no production change and no `/dev/full`:
   `cli.Run(cli.Env{Args: {"recv","--team","alpha"}, Stdout: <a writer that keeps the bytes and returns an error>, Stderr, Digest: testDigest, Getenv: <TEAMCHAT_RUNTIME = the session's runtime, HOME, XDG_RUNTIME_DIR of the world>, Verbs: Verbs()})`. The ids it printed are the items handed out; record each in `marked` with the number of its copies
   in the transcript at that time, and do not count them in `recv` (the agent saw nothing). A row for P2 is
   deterministic like this: say A, wait until A is in the transcript, say B (it waits for the pace), `recv-fail`, and if
   it printed B, drain; if it printed nothing (B was already being delivered), try a new item, at most three times, and
   fail the row if none was handed. Under the planted defect `if e.Repeat && e.Kind == "mutation-never"` in
   `proto/envelope.go` `closingLines`, B's transcript copy carries no `repeat:`, `marked` holds B, and P2 fires. (P11
   does not, because the failed recv's print is not a show; that is why `marked` exists.) Not yet run: it was designed,
   not built.
3. **P5 needs a defect that makes a second event, not a second post with the same transaction id.** The leave and the end
   post by transaction id (`ends.Leave` takes the keeper's, `end` a fresh `tc` id), and fakehs honours it, so posting the
   same notice twice leaves one event in main. To show P5 red, plant a second `post` with a different id: in
   `ends/ends.go` `Leave`, `post(ctx, n, proto.EventLeft, reasonReleased, txn+"x")` after the first, or in `send` after
   `post(ctx, n, end.Notice, reason, *end.Txn)`. Both are real code paths.
4. **P4 will be hard to show red with a cheap plant.** After the removal the engine deletes the team from `e.teams`
   (`engine/teams.go` `drop`), the store's writer is removed, and the delivery loop picks only held teams, so a leak
   needs several layers to fail. The candidate to try first: make `Engine.Remove` return `Removed{UserID: userID}` right
   after `failpoint.Hit(failpoint.LinkInRemove)` (no stop, no drop), so the team goroutine keeps syncing; if the keeper's
   store deletion still keeps the post-removal item out of the transcript, say so in the report.
5. **P4's detection has no dwell.** An item posted after the removal that leaked would show within the sync latency and
   the delivery pace (up to 2 s); a run that ends at once after the removal could miss it. Do not add a `Sleep`
   (`project_realtime_bound` does not catch it, but it is the pattern the rule warns of): let the tail's own waits (the
   leave's post, the keeper's "seat alpha released" log line) carry it, and report that P4's window is that.
6. **After a removal or an end the world has no seat or no CMD**, so: `recv`, `status` and `seatedIn` fail for a removed
   team, and the fake Claude Code's `transcript` command is gone after an end. Read the transcript from its file
   instead: record `begun.Transcript` in `claude()` (`e2e_claude_test.go`, a `transcript string` field on `session` in
   `e2e_world_test.go`) and read it through `testfiles.Try`, keeping only whole lines (up to the last newline).

## 4. What the runner needs (the design; not built)

- **A barrier before `remove` and `end`/`die`:** the drain's first three steps (release a held inbox, `seatedIn`, every
  accepted item shown or in limbo, nothing owed, every answered send in main). A removal drops pending, handed and
  awaiting items by design (`drop`), so without the barrier P1 could not stay strict; the barrier means the removal and
  the end run at rest, and what they race is left to the failpoint rows (`TestFailpointInRemove` and the rest).
- **`remove`:** barrier, then `w.release()` (generation 2; `desiredFlags` wrote 1), `eventually` the store file is gone,
  then `removed = true`, `atRemoval = len(copies)`, `want = "left released"`. After it a `say` or `peer` goes to
  `afterRemoval`, not `posted`.
- **`end`:** barrier unless removed, `kill(keeper, SIGTERM)`, `want = "left other"` (the keeper's `stop` signals the fake
  Claude Code by its pidfd, `how` is `stop` whatever it died of, the goodbye is `left other`). **`die`:**
  `kill(agent, SIGKILL)`, `want = "disconnected"` (`TestE2ECmdKilled`). With a removal before it `want` stays `left released`.
- **The tail:** an ended run waits `s.wait()` (the keeper leaves after its link has), then the notice; a removed run waits
  for the keeper's log line `seat alpha released` (the link's `left`, after both posts of the leave) and the notice;
  neither is the old drain. `seen()` fills the new fields; `oneLink` is not called after an ending.
- **The fixed seeds** are then chosen again to draw every kind, and `pt1Unbuilt` empties. Seeds that draw a removal in
  the first 60: 14, 25, 33, 36, 42, 49, 52, 54; the old fixed seeds 3, 4, 8 and 10 draw one too.
- **Cost:** not measured. Slices 1 and 2 are about 52 s a count (47.5 s and 54.5 s in two runs here); the runs with an end
  are cheap (the schedule stops) and the redelivery row should be about 3 s: an estimate of 5 to 10 s a count more, to be
  measured, and doubled in the gate's `-count=2`.

## 5. Not done, exactly

- (A) the runner for `remove`, `end` and `die`; P4 and P5 over a real run; the planted defects (a second leave or
  goodbye post, and a P4 candidate).
- (B) the redelivery row, the drawn `recv-fail` in the runner, and the P2 red proof on the `closingLines` defect.
- (C) the cost of any of it.
- `-tags tuwunel` was not run (`FAKEHS_TUWUNEL_BIN` is unset).

## 6. Gates and tests, exactly (container)

`FAKEHS_TUWUNEL_BIN` is unset. `qa-all.bash` was not run (not allowed here).

| What                                                                                     | Result                                                                                                                                                              |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `go test -race -shuffle=on -count=1 -tags busfailpoints -run TestPT1` (wiring), baseline | ok, 54.5 s (before this slice)                                                                                                                                      |
| the same after the pure half                                                             | ok, 47.5 s, and 48.3 s after the lint refactor; the 12 fixed seeds and 8 point rows run, the unit tests 0.02 s                                                      |
| `-run 'TestPT1(Schedule\|Fixed\|Checks\|Removal\|Reads\|Transcript\|Every)'`             | ok, 0.022 s, after the last edit                                                                                                                                    |
| `golangci-lint run` default and `--build-tags busfailpoints` (wiring)                    | 0 issues both, after the last edit (the first tagged run found 5: exhaustive, funlen, two gocognit, one staticcheck; all fixed)                                     |
| `go vet` default and `-tags busfailpoints` (wiring)                                      | clean                                                                                                                                                               |
| `scripts/qa-changed.bash` (uncommitted tree, 6 files)                                    | docs, go (go-qa on both modules, with the tagged lint, vet and `-count=2` test stages) and teamchat-keeps-off-v1 all passed; it did not end red on issue 135 or 136 |
| `-tags tuwunel`                                                                          | not run: `FAKEHS_TUWUNEL_BIN` is unset                                                                                                                              |
| a planted defect in production code                                                      | none run (no runner for the new operations); production files are untouched                                                                                         |

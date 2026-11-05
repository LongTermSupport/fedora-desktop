# n3e-13, PT1: slices 1 and 2 built (the driver, the fixed seeds, the point rows); slices 3 and 4 partly

Plan 00209 Phase 3, Task 3.2. Base fast-forwarded to `feat/bus-v2-n3e-wire` (head `b4f4b8bdb`, the third merge of F44;
`e2e_claude_test.go` present). Code commits `7aeac454a` (slice 1) and `a43c75567` (slice 2); documents in the commit after.
This agent's context passed the 300,000-token soft budget, so it stopped at a clean committed point; section 6 says what
the next agent needs.

## 1. Where it lives, and why

Inside package `wiring`, as test files under `//go:build busfailpoints`, not in a new `member/pt1` package. The world (the
keeper and link processes, the fakehs, `claude`, `session`, `eventually`, the arm-once mechanism of `TestMain`, the three
re-exec names) is `_test.go` code of `wiring`; a separate package could not import it without moving about 1,400 lines of
test helpers into a non-test package that the production tree would then carry, and the move would touch the lines PR 128
also edits. The cost: PT1 shares the package's one `TestMain` and its run time. Rows are named `TestPT1*` so
`-run TestPT1` runs PT1 alone. The gate needs no new row: the stage `teamchat-failpoints-test` of `qa/go-qa.toml` already runs
`go test -race -shuffle=on -count=2 -tags busfailpoints ./internal/teamchat/...` (n3e-D19's row exists), so the seeds run
there twice.

## 2. The numbered expectations (written before the code)

- PT1-1 A seed alone fixes the schedule: the same seed is the same operations on every machine and toolchain (ChaCha8 keyed by
  the seed). Tested: `TestPT1ScheduleIsAFunctionOfItsSeed`.
- PT1-2 A schedule keeps the bounds a run relies on: a `say` first, the inbox held only when free and never at the end, one
  refusal, each kind within its cap (a peer's flood budget is 10 in 600 s, so at most 6 peer posts). Tested in the same test.
- PT1-3 Every operation is drawn by some seed, and by the fixed list: `TestPT1ScheduleDrawsEveryOperation`,
  `TestPT1FixedSeedsDrawEveryOperation`.
- PT1-4 The transcript is read back into item copies (id, `repeat` marked or not): `TestPT1ReadsTheTranscriptBackIntoCopies`,
  `TestPT1TranscriptLinesThatAreNoUserTurnAreNoCopies`.
- PT1-5 Each check fires on its own property and on nothing else: `TestPT1ChecksFireOnTheirOwnProperty` (14 cases).
- PT1-6 A run is real processes: the world's keeper and link, the fake Claude Code as CMD (`--claude`, inbound `accept`),
  fakehs on HTTP; the operations are the real verbs and the fakes' faults. `TestPT1Seeds`.
- PT1-7 After the schedule the run is drained on conditions (inbox released, link answering, every accepted item shown or in
  limbo, nothing owed, every answered send in main) and judged. No sleeps; the waits are `eventually` and `within`.
- PT1-8 Each run prints its seed and its schedule; a failure prints the replay command; `-pt1.seed=N` replays it;
  `-pt1.random=<n>` runs n random seeds, each printed first (a manual target).
- PT1-9 A SIGKILL at each failpoint a one-team session can reach, one row each, which fails if its point never fired:
  `TestPT1Points`. Every point in `failpoint/names.go` is reached or has a stated reason:
  `TestPT1EveryFailpointIsReachedOrSaidWhy` (it parses the source of `names.go`).
- PT1-10 The harness is shown red on a planted defect (section 4).

## 3. What is built

Files, all in `src-go/internal/teamchat/member/wiring/`, all `busfailpoints`, no production file touched:

- `pt1_model_test.go`: the operations (`say`, `peer`, `recv`, `wait`, `send`, `hold`, `release`, `refuse`, `restart`,
  `kill-link`), the schedule generator, the transcript reader and the checks `pt1Check`.
- `pt1_run_test.go`: the runner, the drain, `TestPT1Seeds` (12 fixed seeds), `TestPT1Random`, `TestPT1Points` with its
  provokers, and the flags.
- `pt1_model_unit_test.go`: the unit tests above.
- `e2e_claudecode_test.go` (untagged, 24 lines): the fake Claude Code gains `hold`, `release` and `refuse` for its inbox.

Checks (`pt1Check`): P1 (an accepted item is shown by recv or the transcript, or is held in a store's awaiting entry; an
answered send is in main exactly once; none is in main twice), P2 (a transcript copy after the first carries `repeat`), P5
(a `left` or `disconnected` notice at most once), P6 (at most one link of the session alive, also after every operation), P11
(an item recv printed has no second unmarked copy, one extra allowed per link crash). P3 is not checked directly: an item the
position outran is an item P1 finds lost.

Cost: 12 seeds about 20 s (1 to 3 s each); 8 point rows about 27 s; the unit tests 0.02 s. The gate's `-count=2` makes that
about 100 s in the tagged stage. The whole wiring package was about 176 s with the tag before; it is about 50 s more now.

## 4. Red and green

Test-first for the pure half: the unit test was written first and failed to compile ("undefined: pt1Copy"), then went green.
The checks were each shown firing on their own property by the 14 table cases (a case per property, and the clean and the
allowed cases that must stay silent).

Red on a planted defect in production code (put in, run, reverted; `git diff` of production files is empty):
`engine/recv.go`, `Engine.Ack` returning `false` for each item (an acknowledged item goes back to pending instead of into the
ledger). Result: seeds 1, 2, 3, 5, 7 and 8 fail with `P11: <id> was shown 11 times with no repeat mark (recv 11) after 0 crashes` (and 7, 6, 5, 4, 2 times for others), seeds 4, 6 and 9 never settle (the drain times out at 30 s with the item still
owed); the output of seeds 10 to 12 was not read. Green again after
the revert.

Two harness faults were found by running and fixed (not production faults): a row name that put a link socket path over the
107-byte limit (rows are now named without `link-`, and the test is `TestPT1Points`), and an `awaiting` delivery that is in
limbo for 180 s by design (section 5, finding 2).

## 5. Findings

1. **`link-after-missing` and P8, P9, P10 are not reachable by real processes.** `engine/proof.go` has `quietAfter = 180 * time.Second` and a one-hour cap, no override and no clock seam in the link (the link reads the system clock). A run that
   waits for `missing` costs three minutes per row. The failpoint exists in the code (`engine/points.go`, hit at
   `proof.go:418`), so nothing is missing from the code; what is missing is a way for a test to shorten the quiet clock. To
   reach P8 to P10 and this point a production change is needed, which this slice did not make: for example a
   `busfailpoints`-only override of `quietAfter` and `missingCap` read from an environment variable at the link's start,
   next to `points_armed.go`. The engine's fake-clock tests own these properties today.
2. **A crash at `link-after-awaiting` leaves the items in limbo for 180 s by design**: the awaiting entry says `written: "unknown"`, no needle will appear in the transcript (the user line was never written), so the items are neither pending
   nor shown until `missing`. P1 therefore cannot demand a copy in gate time; the checks accept an item held in a store's
   awaiting entry, and the drain waits only for that. If the owner wants P1 strict here, it needs the same clock seam as
   finding 1. (Plan note: the same crash at `link-after-user-line` is proven at once because the frame reached the
   transcript.)
3. **`link-between-store-writes` needs two teams in one session** (a hand-out writing two stores). The engine's tests hit it
   (`engine/deliver_test.go`); no real-process row does, and PT1's world has one team. The wiring world has `join` for a second
   team, so a row is possible (not built).
4. Every failpoint the spec lists exists in the code: all 16 of `names.go` (nine of n3c, two of n3d, five of n3e), and each is
   called: none is declared and unused. The guard test keeps it so.
5. The step "between two stores' writes of one delivery" in the spec reads as one delivery; in the code it is recv's hand-out,
   `Ack` and `Return` (n3e-D8), since a socket delivery is one team and one store (T18).
6. `go test ./pkg -pt1.seed=N` needs the package before the flag; the printed replay command has it in that order.

## 6. Not built, and what the next slice needs

- **Slice 3 (the rest of the faults and ends)**: the driver has the cheap faults (inbox hold, release, refusal, homeserver
  `Restart`, link kill) and the sends. Not in it: the homeserver `Stop`/`Start` fault (it costs the link's backoff, up to 30
  s per cycle, so it was left out of the fixed seeds), a fakehs `Inject` of `Drop` or `DropAfter` on a PUT (the cheap way to
  lose a send's answer), a held inbox past the link's 5 s phase bound (5 real seconds per run), a SIGKILL of the keeper, and a
  `SIGKILL` of a CLI under a request (P7: a request whose CLI died is never acted on after its deadline).
- **Removal and end ops in the driver (P4, P5)**: `release` (a desired generation that releases the seat) and `end` (SIGTERM
  the keeper) as drawn operations, with P4's check (a removed team's items never reach the transcript after the removal) in
  `pt1Check`. The points `link-in-remove`, `link-after-remove-answered`, `keeper-before-store-delete`, `link-mid-end-sends`,
  `keeper-before-end` and `keeper-link-writing` are in `pt1Unreached` with the existing rows named; each needs a provoker that
  arms `run=` for the keeper points (`arm("run", point)`) and the check after it. `pt1Provokers` is a map from point to a
  function: a point moves from `pt1Unreached` to `pt1Provokers` and the guard test stays green.
- **Slice 4 remainder**: the red proof is one defect. A second, in the transcript path (for example `engine/recv.go`'s `Return`
  not marking `repeat`, caught only by a row that kills the link while handed and then reads the transcript), and one for P2
  (a delivery that is not marked `repeat` after a crash) would show the point rows red as well; not done.
- The `-pt1.random=<n>` run was not run for a long count here (it was run only as the skipped case). Run it by hand:
  `go test -race -tags busfailpoints -run 'TestPT1Random$' ./internal/teamchat/member/wiring/ -pt1.random=50`.

## 7. Residual non-determinism, named

A seed fixes the operations, their order and the fault points; it does not fix which goroutine of the link wins a race with a
recv (a delivery against a `recv` of the same item), how long the keeper's restart backoff takes, or whether a `wait` sees an
item before its one-second timeout. So a failing seed usually fails again and always prints the same schedule, but it is a
replay of the schedule and not of the interleaving below it. Each run is on real wall time for the link's own timers (the 2 s
delivery pace, the restart backoff of 1 s); the test adds no sleep.

## 8. Gates and tests, exactly (container, `FAKEHS_TUWUNEL_BIN` unset)

| What                                                                           | Result                                                                                                                                                                                                                           |
| ------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `go test -race -shuffle=on -count=1 -tags busfailpoints -run TestPT1` (wiring) | ok: 12 seeds 22 s, 8 point rows 27 s, unit tests 0.02 s, `TestPT1Random` skipped                                                                                                                                                 |
| the same, planted defect in `Engine.Ack`                                       | red as section 4; reverted                                                                                                                                                                                                       |
| `golangci-lint run` default and `--build-tags busfailpoints` (wiring)          | 0 issues                                                                                                                                                                                                                         |
| `go vet` default and `-tags busfailpoints` (wiring)                            | clean                                                                                                                                                                                                                            |
| the whole wiring package, `-race -shuffle=on`, default tags and tagged         | not rerun after the change: the new files are tagged, so the default build is unchanged except `e2e_claudecode_test.go`'s 24 lines; the tagged build adds about 50 s                                                             |
| `scripts/qa-changed.bash`                                                      | `--base feat/bus-v2-n3e-wire` (the two code commits and the documents): docs, go (go-qa on both modules, with the tagged lint, vet and test stages) and teamchat-keeps-off-v1 all passed; it did not end red on issue 135 or 136 |
| `-tags tuwunel`                                                                | not run: `FAKEHS_TUWUNEL_BIN` is unset                                                                                                                                                                                           |
| `scripts/qa-all.bash`                                                          | not run (not allowed here)                                                                                                                                                                                                       |

## 9. Reviewer's checks and findings (the coordinator, after this report)

Run in the builder's worktree, `-race -shuffle=on -tags busfailpoints`, container, `FAKEHS_TUWUNEL_BIN` unset:

- Stability: `-run TestPT1 -count=3`, two copies at once (so under load): both ok, 177 s and 179 s for three counts each (a
  count takes about 59 s under that load, 52 s alone). No seed or row failed in six full counts.
- Lint and vet were not rerun by the reviewer; the builder's results in section 8 stand.
- **A second planted defect, P2: the harness does not catch it.** In `proto/envelope.go`, `closingLines`, the `repeat:` line
  was never written (`if e.Repeat && e.Kind == "mutation-never"`). All of `-run TestPT1` stayed green (ok, 52 s, 12 seeds and 8
  rows). So the P2 check is proven only by its unit case (`TestPT1ChecksFireOnTheirOwnProperty`), and no real run produces a
  redelivery in gate time: the point that would (`link-after-awaiting`) leaves the items in limbo for 180 s (finding 2), and
  none of the other eight rows or the drawn operations makes the link deliver a second copy. The mutation was reverted; the
  worktree was clean afterwards.
- **P5 was not shown to fire in a real run either.** No drawn operation ends or removes a seat; whether a link kill posts a
  notice was not looked at. The check has its unit case only.
- What this means for the claim: PT1 slices 1 and 2 reach P1, P6 and P11 with real processes (P11 shown red by the builder's
  defect); P2 and P5 are checked by a pure function whose real-run input never contains what they judge. The next slice needs
  a way to make a redelivery in gate time (the clock seam of finding 1, or a socket fault that fails a write after the line is
  in the transcript) and a removal or end operation, and a planted defect for each before either is called covered.

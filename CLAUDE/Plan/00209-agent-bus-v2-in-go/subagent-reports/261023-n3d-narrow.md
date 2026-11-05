The lead saved this read-only check's reply. Lead's rulings on its points: A-M2's clear on
health leaving a stop or Down is accepted (it is the "teamsync stop that clears" case, bounded to
one extra PUT); A-n3 is accepted as decided, since no n3c or n3d store exists outside tests, and
it gets a plan line; the exit 20 design row was already corrected in 7e09694fd (the check read
fixb's report, not the file); guest `TestRegister` failing under `-count=2` is queued as a defect.

Both reviews are checked against merged code at 7e09694fd. All 14 A and B findings are fixed or soundly handled, but two points need a lead decision first (A-M2's clearing rule and the exit 20 text). `go vet ./internal/teamchat/member/...` is clean, and `go test -race -shuffle=on` passes on every `member/*` package, including engine, teamsync, forge, link, state, client and exits. I read the fix code and the fix reports. I did not revert any fix to see its test go red, so each "would fail without the fix" below rests on the fix reports' red runs, which I did not reproduce. The `clock`, `proto` and `matrix/fakehs` suites were still running when I wrote this; they are not in the green list above.

## Review A

- **A-M1: FIXED, matches the ruling.**
  - `recordPosted` also writes `outbox.last_sent` in the same store write that removes the entry.
  - `newerThan` (`engine/sender.go:505`) reads back a page at a time to that anchor, at most 5 pages.
  - `matchTried` (`:554`) pairs by `unsigned.transaction_id` first. It falls back to content only when the event has no txn id. A different txn is a different send, and the `since_ms` comparison is gone.
  - `Outbox.check` refuses a duplicate ping id.
  - Pinned by `TestN3dD4TheRecoveryIsAnchoredOnTheLastPosted`, which covers both same-device and new-device. The store rules `ping same id twice` and `last sent bad id` pin the other two changes.
- **A-M2: FIXED, with two points for the lead.**
  - `failed` (`:305-322`) sets `h.sendStop`. `shown()` (`teams.go:76`) turns it into `Stopped`, which `stoppedErr`, `mayPost`, status and `status.json` read.
  - It clears in `RecoverSends` (`:440`), which covers a new pair and a link restart. It also clears in `SetHealth` (`seam.go:399`) on a move out of `Stopped` or `Down`.
  - Point 1: the `SetHealth` arm also fires on Down to up, not only on a teamsync stop clearing. That is wider than the ruling. It costs at most one extra PUT and a re-stop, so I judge it acceptable, but the lead should confirm it.
  - Point 2: there is no `Down` guard on the clear, so a flapping homeserver that is also 403ing the PUT gets one PUT per recovery. This is bounded.
  - Pinned by `TestSD3_8cA403StopsTheTeamsSends`.
- **A-M3: FIXED.**
  - `teamsync/session.go:57` replaces `sendsRecovered` with `recoveredPair`, compared by `DeepEqual`.
  - `RecoverSends` sets `recovered=false` first and holds `postMu`.
  - `post` (`:181-194`) compares the token digest with `recoveredUnder` before every PUT and recovers first if it differs. That covers the gap between a token change on disk and the next session.
  - Pinned by `TestN3dD4ANewTokenRecoversBeforeTheRetry` (engine) and `TestN3dD4ANewPairRecoversAgain` (teamsync).
- **A-m1: FIXED.** The time bound is gone, replaced by the anchor and 5 pages. `TestN3dD4TheRecoveryReadsBackToTheAnchor` puts the event 120 back. It was shown red with `recoveryPages=1`.
- **A-m2: FIXED.** `send.go:584` has `queuedNow = Down || older || !h.recovered`. `TestN3dD19ASendBeforeTheRecoveryIsQueuedAtOnce` pins it.
- **A-m3: FIXED.** `resolveAll` returns the first roster-less team, and the answer is 20 ahead of 64. `TestSD2_1bAnUnverifiedTeamIsUnreachableNotUnknown` pins it. The old `TestN3dD3ACachedRosterComposes` assertion was changed to match.
- **A-m4: FIXED DIFFERENTLY, sound.** The nil-interface panic cannot occur because `e.single` already answers 1 for a team with no goroutine. The real gap was a stopped team fetching through its stopped state. `replyTeam` now runs `sendable` first. `TestSD1_7AReplyToAStoppedTeamIsItsStop` pins it.
- **A-n1: FIXED.** A removal answers a tried send with 4 `timed-out`, and an untried one with 64. `TestN3dD23ARemovalAnswersATriedSendTimedOut` pins it.
- **A-n2: FIXED by rename.** The test is now `TestSD3_7ARequestEndedWhileFailingAnswersQueued` and its comment says what it runs.
- **A-n3: NOT FIXED, by decision.** `Version` is not bumped and n3c and n3d stores are refused whole. This is harmless before release, but the plan still needs a line saying so. The lead owes that line.

## Review B

- **B-M1: FIXED.**
  - `ratedUntil` sits beside down health (`forge.go:204-210`). `isRated` answers `forge-rate` at once, with no request.
  - `waitRate` (`:568`) refuses to sleep when the wait ends at or after `clock.Bound`.
  - The link server sets the bound from `deadline_ms` (`link/server.go:207`). teamsync sets now plus 30 s per batch (`teamsync/forge.go:87`).
  - Pinned by `TestSD5_7RateWaitPastTheBoundIsForgeRate`, `TestSD5_7RateHealth`, and the link and teamsync bound tests.
  - Remaining gap, which fixb also noted: a send whose context is cut mid-request still exits 20, because `forgeCheck` maps any error to forge-down. This is now rare and I count it a nit.
- **B-M2: FIXED, matches the ruling.**
  - Per-sender budget of 6 per 60 s (`spentChecks.spend`, `teamsync/forge.go:58`), taken once before the first request, receive side only.
  - Dedupe in a batch (`keyOf`).
  - A reserve at a fifth of the limit (`mayAsk`/`inReserve`), receive only, so sends keep the rest.
  - There is no negative cache.
  - Pinned by `TestSD5r8ASendersChecksAreBudgeted`, `TestSD5r9IdenticalChecksAreAskedOnce`, `TestSD5_9AReceiveCheckSpendsOncePerUncachedCheck` and `TestSD5_9ReceiveChecksKeepTheReserve`.
- **B-M3: FIXED.**
  - `lost` (`:664`) returns `ErrDown` only when no connection was made (httptrace, reset per attempt) or when the repository's own request is unanswered or 5xx (`:552`, `path == r.base`).
  - A compare or contents 5xx or cut is `unresolved` for that check only.
  - Pinned by `TestSD5_8ACompareCutIsUnresolvedNotDown` (it also checks a second check still asks), `TestSD5_8AServerErrorOffTheRepositoryIsNotDown`, and the guard `TestSD5_8ANoConnectionOnACompareIsDown`.
- **B-m1: FIXED.** gh runs in its own goroutine under a 10 s bound, from `context.WithoutCancel`. Callers wait through a `select` on their own context, and a failure is held for 30 s. `TestSD5_4GhRunsUnderItsOwnBound` pins it.
- **B-m2: FIXED.** `fakehs.ForgetTransactions()` plus two new rows, `TestFailpointChargeBeforeSendServerForgot` and `TestFailpointSendBeforeRecordServerForgot`. Only the SendBeforeRecord row discriminates, because no PUT was made before the crash in the other. The report states this.
- **B-m3: FIXED.** `msgSendCut` tells the agent to run `status` before sending again, and the code stays 69. `TestASendCutOffAfterItsRequestSaysCheckStatus` pins it.
- **B-m4: FIXED, one concern.** The row-20 text now separates the two codes (`exits.go:155`). The unreachable-or-forge-down row also reads "tell a human if it persists", which is acceptable. The design chapter `DESIGN-v2-B-member.md:507` still says "the homeserver did not answer", and the lead owns that edit.
- **B-n1: FIXED.** The branch fetch requires `v.Name == b`. `TestSD5_5ABranchIsTheBranchItNames` pins it.
- **B-n2: FIXED by rewording.** Forge-login is said once per team, and the brief's D10 row and line 656 were changed to match.
- **B-n3: FIXED.** The `down` bool is gone; `downUntil` alone is the health.
- **Out of slice, not touched:** `guest.TestRegister` panics under `-count=2`. The lead should file it. It passes in this single run.

## The two fixes together

- No use of a removed name remains: `sendsRecovered` has no hits, and `recoveredPair` and `spent` coexist on `Team` (`teamsync.go:105,109`).
- No fixb test assumed once-per-goroutine recovery; the teamsync suite is green.
- The `postMu` / `recoveredUnder` ordering is consistent. `RecoverSends` clears `recovered` on the state goroutine and then takes `postMu`. `post` re-checks `recovered` and the entry under `postMu`.
- Decision id D31 is used once in the SPEC row. fixa's text changes went into the n3d-D4 row, so no id clash appeared.

## Verdict

Ready for the qa-reviewer, with these for the lead to settle or note first:

- Confirm A-M2's clearing rule, including the Down-to-up clear.
- Add the plan line that n3c and n3d stores are not carried over (A-n3).
- Update the design chapter row for exit 20.
- File the `guest.TestRegister` `-count=2` panic.

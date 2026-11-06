# Fix-sender report: issue #159 and the retry-hold question (Plan 00209)

Builder: Opus. Base `origin/merge/pr157-onto-f44`. Branches: `fix/recover-stale-401-159` (fix 1)
and `fix/retry-hold-lift`, stacked on it (fix 2). Nothing pushed, nothing merged. This file on
the fix-1 branch covers fix 1; the fix-2 branch completes it.

## Fix 1: a stale 401 or 403 stops the wrong token (issue #159)

### What is built

- `held.sendsUnder` (`member/engine/teams.go`): the SHA-256 of the token the team's sends run
  under, owned by the state goroutine. A recovery sets it in its first state-goroutine step:
  `RecoverSends` in the same step that clears `sendStop` (the digest of `n.Token()`, read before
  that step), and `recoverLocked` (the sender's own recovery under a changed token) in its
  first step.
- `post` reads the token once, posts under it, and hands its digest to `sendFailed`.
- `sendFailed`: for a 401 (`ErrAuthRefused`) or 403 (`ErrForbidden`), the check
  `h.sendsUnder == under` and the stop (with `refuseWaiting`) are one `e.do` step. A refusal of
  a token the sends no longer run under stops nothing, answers no waiting op, is logged, and
  returns no hold: the sender loops and posts the send under the new token.

### Why it is race-free

The team reissues the pair before it calls `RecoverSends`, so the digest that step stores is the
new token's. The two state-goroutine steps come in either order: the sender's check and stop
first (the stop is set under the old token, and the recovery's step clears it), or the
recovery's step first (`sendsUnder` is the new digest, the PUT's the old one: nothing is
stopped). Nothing can fall between a check and its set: they are one step. `recoveredUnder`
cannot serve: the recovery writes it only once it holds `postMu`, which the sender holds across
the PUT and its answer, so in the window of #159 it still names the old token.

Invariant that keeps the stale path from looping: the sender PUTs only under `recoveredUnder`,
which only `recoverLocked` sets, and `recoverLocked` sets `sendsUnder` to the same digest first.
A stale refusal therefore happens only once a recovery under a newer token has begun; the
sender's next reading then finds `recovered` false (`RecoverSends`) or a changed token (it
recovers under it).

### Tests and what each was shown to catch

- `TestARefusalOfAnOldTokenDoesNotStopTheNewOne` (`send_test.go`, subtests 401 and 403): the
  PUT under the old token is held (`holdAnswers`), the token is changed, `RecoverSends` runs in
  a goroutine until its first step has cleared the stop, then the answer is released. Once the
  recovery returns (it takes `postMu` only after the sender read the refusal) no stop may stand,
  and the send is posted under the new token.
  - Red before the fix, both subtests, for the right reason:
    `the old token's refusal stops the new token's sends: "token"` (and `"forbidden"`).
  - Planted (`RecoverSends` not setting `sendsUnder`): red, the same message.
- `TestARefusalOfTheTokenTheSenderRecoveredUnderStops`: a token changed with no team recovery;
  the sender recovers under it itself, and its 401 must stop the sends (21 to the waiting op
  and to the next send). Planted (`recoverLocked` not setting `sendsUnder`): the stale path
  loops, a PUT per turn, and the test is caught only by `-timeout 30s` (a panic, not a clean
  failure): the plant breaks the invariant above.
- Green after the fix: both, `-race -count=5`, with `TestSD3_8*`,
  `TestAPutIsRecordedBeforeItsAnswerIsHandled` and `TestN3dD4*`.
- `TestN3dTheRecoveryOfATeamNotHeldIsRemoved` passed a nil `Notices`; `RecoverSends` now reads
  the token before it looks the team up, so it passes `&fakeNotices{}`. A held team never took
  a nil `Notices` (`recoverLocked` dereferences it); only the not-held path skipped it.

Coverage (`-race`): engine 92.7, teamsync 95.3, team 100.0, state 99.8.

### Defence Before Fix

- Class: a refusal state set from the answer to a request without checking that the answer
  belongs to the credential held now.
- Detector: none can be written. Recorded in `CLAUDE/GoProjectRules.md`, "Defect classes that
  cannot be a rule", with what holds it instead (the state carries the credential it applies
  to). Which field is a refusal state, which call is a credentialed request and which value is
  the credential held now are the program's meaning; a rule keyed on `sendStop` would check this
  instance, not the class.
- Sweep, every place in the member's engine and teamsync that sets a stop or refusal from an
  answer:
  - engine `sendFailed` `sendStop`: the instance, fixed.
  - engine `teams.go` `refused`, `Stopped = "store"` / `"bundle"`: set from local reads (the
    store, seat.json), not an answer: not the class.
  - engine `deliver.go` `stopTold`: what was told, not a refusal: not the class.
  - teamsync session stop (`stopCode`, 401 and 403): sound; a 401 re-reads the pair and retries
    a changed one (n3c-D12, `retryAuth`), so the stop is set only under the session's pair.
  - teamsync `verbErr`: an answer to the caller, no state kept: not the class.
  - teamsync receipts' 401: ends the receipts loop until the next session, which starts it under
    its own pair: sound.
  - teamsync resolver (`lookup.go`) caches a 403 or 404 as unknown: about the user and the event,
    which a reissue does not change; a 401 is not cached: sound.

### Gate result (fix 1)

`./scripts/qa-changed.bash --base origin/merge/pr157-onto-f44` over the fix-1 commits (8 changed
files): docs whole gate passed; go: go-qa, both modules (diff mode) passed, and no module printed
test output, so the root module's tests passed; teamchat-keeps-off-v1 passed; exit 0. The run
waited about three hours for the QA lock behind other agents' runs.

## Findings (fix 1)

1. `SPEC-n3d.md`, named by the brief, does not exist in this tree. The n3d rules are in
   `subagent-reports/261022-n3d-handoff.md` (section 9 and the n3d-D table). Fix 1 matches its
   text ("no PUT is made until the team runs again: a recovery (a session under a new pair), or
   a health out of a stop or down"). The spec does not cover an answer that arrives after the
   run under the new pair began; the fix takes the reading that a stop belongs to the run whose
   token it refused.
2. A 403 refuses the user (membership), not the token, so gating it on the token is not needed
   for correctness. It is gated as decided, at no cost: a 403 under the new token stops the
   sends at its own PUT.

## Fix 2: the retry hold and the 55 to 62 s delivery

No production code changes in fix 2. As the coordinator's correction said, the lift and the
wake already exist, and the delay is not the sender's.

### (a) A newer sync lifts a standing hold: GREEN, now pinned

`TestN3dD1ANewerSyncLiftsTheRetryHold` (`sender_test.go`, subtests "a newer sync" and "the
timer"): a send's PUT fails 502 with its answer held; a sync is answered while it is in flight;
the answer is released and the hold arms (`awaitDue(retryHold)`); once the world is idle
(`quiet`, through `leakcheck.Drive`) the send must not have been posted (that sync came before
the failure was read); then either a newer sync is answered with the clock untouched, or the
clock is advanced to `retryHold`, and the send must be posted.

- Green against the code as built (`-race -count=5`).
- Planted, the lift removed from `mayPost`: "a newer sync" red (`leakcheck: every other goroutine is blocked and the condition does not hold`).
- Planted, any sync lifts (`>=`): both red, `a sync answered before the failure lifted the hold`.
- Planted, the timer ignored (`hold.fired` dropped): "the timer" red (leakcheck).

### (b) Where the 55 to 62 s comes from: the team's up-mark waits for its first long-poll

Measured on the fake clock in `teamsync`, with a test of the claim "a team is shown up as soon as
the server answers it again" (not committed: it is red by design). A team with a stored position
long-polls; six `/sync` answers are 502 (waits 1, 2, 4, 8, 16, 30 s; down at the fifth); after
the sixth wait the retry reaches the server, the new session verifies main (answered) and, since
`start()` makes no sync when `next_batch` is stored, goes straight to its long-poll, which the
fake holds in an idle room. Red, 3 of 3 runs:

```
the server answers (main's state verified, the long-poll accepted), yet the team is shown down: {Down:true ... LastSyncOKMS:1760000030000 ...}
shown up 30000 ms (fake wall) after the retry reached the server; LastSyncOKMS 1760000030000 -> 1760000121000
```

So a team back after an outage is shown up, and its `LastSyncOKMS` moves, exactly `longPoll`
(30 s) after the retry that reached the server, in an idle room: `recovered()` and `answeredAt()`
run only on a sync's answer, and the first one is the long-poll's. The E2E worst case is the retry
(within `backoffCap`, 30 s) plus `longPoll` (30 s) plus the processes' own time, which explains
55 to 62 s against 60. In those tests the send is queued while the team is shown down, so the
sender makes no PUT and no retry hold ever stands: `retryHold` is not on that path. Faster runs
are those where an event lands in the room during the long-poll (the other member's activity
after its own retry), which answers it early.

Committed in its place: `TestABackTeamIsShownUpAtItsFirstLongPoll` (`run_test.go`), green,
pinning the bound the wiring now derives from (shown down while the first long-poll is held; up,
with `LastSyncOKMS` at `retried + longPoll`, at its answer). Planted, the team marked up when the
session's verify succeeds: red, but at an earlier step (`not down after six failures`), because
the fault refuses only `/sync`, so verify succeeds at every retry: the plant changes the outage
the test builds rather than giving the exact message.

### (c) The wiring bounds

`member/wiring/e2e_test.go`: `retryHold` removed (no wait depends on it), `longPoll` added
(teamsync's), `upBound = backoffCap + longPoll + settle` with its derivation, and
`deliveredBound = upBound` (the sender posts nothing while the team is shown down, then posts
at once). The value is unchanged (90 s); the derivation was wrong. `TestE2EOfflineServer` waited
`backoffCap + settle` for the team to show up: the same class as #140 (no margin past retry plus
long-poll), so it now waits `upBound`. The #140 entry of `GoProjectRules.md`, which named that
wait as the model, is corrected.

### Recommendation (the coordinator's call, not built)

Mark a team up, and record an answered request, at a session's first successful request under
the pair (verify's main-state read) rather than at its first sync answer. That would make the
up-mark `backoffCap + settle`, lift a standing hold at once, and shrink `upBound` and
`deliveredBound` by `longPoll`. The cost to weigh: `LastSyncOKMS` would then mean "last answered
request", which status and status.json show as the last sync; a separate field may be cleaner.
The n3d handoff's sender rule ("waits for a health change with Down false and a newer
LastSyncOKMS, or 30 s") matches what is built. SY8's text on when a team is back was not read.

### Tests run (fix 2)

engine 92.7, teamsync 95.2, wiring 100.0 (`go test -race -cover`, all passed); golangci-lint 0
issues on those three packages.

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

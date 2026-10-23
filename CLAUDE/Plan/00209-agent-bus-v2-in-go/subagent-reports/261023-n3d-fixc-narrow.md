The lead saved this read-only narrow check reply. Its item 2 (NOT DONE) is wrong: the A-n3 and the guest
TestRegister lines were already in PLAN.md n3d line at 23ffe8edf. Both optional nits are accepted as they stand.

**Verdict: READY TO MERGE.** All nine findings fixc owned (1 and 3 to 11) are fixed. Finding 2 belonged to you and fixc did not touch it. The changed lines introduce no regression.

**Checks:** read-only. `go test -count=1` passes on engine, teamsync, forge, state and link. `TestE2EAReferencedPingWithNoGhIsForgeLogin` passes when run alone. I did not run the full wiring suite. For the "fails without the fix" calls below I reasoned from the old code; I did not revert anything to see a test go red.

**Narrow-check (`261023-n3d-narrow.md`) carry-overs.** fixc made no change to any A or B fix, so those stand as that check found them. Its forge-cut nit was B-M1's leftover, and it is now fixed by finding 4.

**QA review (`261023-n3d-qa-review.md`)**
1. **FIXED.** Row 14 of `DESIGN-v2-B-member.md:505` now lists `forge-rate`, matching `exits.go` and `TestTableIsBsTable`.
2. **Not in fixc.** The A-n3 plan line and the `guest.TestRegister` `-count=2` defect are still yours to record.
3. **FIXED, and the test proves it.** `wiring/e2e_sends_test.go:180` runs the real `link/main.go` wiring and expects exit 78 `forge-login` with nothing queued. With a nil `Login` the link panics and the test gets 69. With a nil `Forge` the link refuses to start (finding 5). Either break fails the row.
4. **FIXED, and the test proves it.**
   - `engine/send.go:111-130` now logs the cause, then answers: request ended is 4 `timed-out` (the same `msgTimedOut` as every other send step), `ErrForgeLogin` is 78, `ErrForgeDown` is 20, anything else is 1 `internal`.
   - `forge.ErrDown` and `forge.ErrLogin` are aliases of the `team` errors (`forge.go:74-76`). So a real forge-down from the checker still reaches 20, not 1.
   - After the forge's own outcomes, the only bare error left is "no forge kind" (`forge.go:171`). Mapping that to `internal` is right.
   - `TestN3dD32AForgeCheckPastItsDeadlineTimesOut` would get 20 from the old code, so it fails without the fix.
   - A side effect: a link shutting down cancels the context too (`link/server.go:211`), so a check cut that way also answers 4. Every other send step already does the same.
5. **FIXED, and the test proves it.** `engine.go:134` refuses a nil forge, and the new "no forge" row in `TestNewRefusesAnIncompleteConfig` would fail on the old code. Every production engine gets its forge from `link/link.go:276`, which takes it from `main.go:77`, so nothing that ships breaks.
6. **FIXED, and the tests prove it.**
   - `teamsync/forge.go:71`: `spendAny` charges the first sender in batch order who has budget left. `Window.Admit` records nothing on a refusal, so a spent sender is never over-charged. The spend runs under `spentChecks.mu`.
   - Charging the honest sender is correct: that sender's own ping needed the same check anyway.
   - `TestSD5r10` would drop both pings on the old code. `TestSD5r11` expects `forge-rate`, where the old code answered `unresolved`.
7. **FIXED.** `state.MaxPings`, `MaxPingTargets` and `MaxSending` are now the only definition. The engine's copies are gone and the three messages fill the number in. The argument order matches each template (`{} sends … team {}` and `team {} … {} pings`), and the full-message asserts pin it. `TestTheSendCapsAreTheStores` only pins the numbers; with one definition, the code structure is what keeps them equal now.
8. **FIXED.** `e2e_failpoints_test.go:355-361` now says which row discriminates and that it matches by transaction id.
9. **FIXED, and the test proves it.** `charged` is set only when a sender's budget was actually charged. Both the reserve refusal and the instant `forge-rate` (`isRated`) return before any charge, so neither counts as answered. `TestSD5r12` would print `forge-login` twice on the old code.
10. **FIXED** (`sender.go:594`).
11. **FIXED** (`forge.go:16-19`).

**Regressions looked for in the changed lines:** none found.
- **Exit classes:** 4 is wait, 20 is wait, 78 is tell, 1 is tell; all match the table in `exits.go:147-162`.
- **Send cap:** defined once.
- **Forge budget:** charged only to a sender who has budget left, never to a spent one.
- **Offline members:** nothing in this diff touches them, so no offline member can block anything.

**Optional nits, not blocking:**
- The budget is charged in `mayAsk` before the gh token is read (`forge.go:528-531`), so a login failure costs a charge with no request. This was there before fixc.
- n3d-D31's text still says `unresolved`. n3d-D34 states that it replaces it, which is acceptable for decisions that are only ever appended to.
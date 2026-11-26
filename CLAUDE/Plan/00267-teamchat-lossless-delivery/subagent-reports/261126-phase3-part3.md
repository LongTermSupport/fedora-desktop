# Plan 00267 Task 3.2, part three: the forge hold, made durable (Tasks 3.2 and 3.2a)

## First cut (c2ed79bfc), replaced

The handoff's design held the ping in memory and kept main's floor below it. It was not
durable: `session.start` resumes from the stored `next_batch` whenever one is stored, so a
restarted link read past the held ping and lost it, with no bounce. The coordinator ruled that
an in-memory hold is a regression against the plan's goal and asked for the store-backed hold
(option B). It is below; the in-memory hold, `heldFloor` and `Team.forgeHeld` are gone.

## Built

- `proto`: `ForgeRetryEveryMs` (30 s), `ForgeRetryLimitMs` (10 min), `ForgeTransient`.
- `member/state`: `Pending.forge_until`, only on a PING, a time.
- `member/teamsync`: a transient failure is taken as a delivered ping marked unproven
  (`judged.holdUnproven`, `take`): `ForgeUntil` its deadline, `NotBefore` its next check, one
  `held <id>: ...` log line; no drop, no bounce. A relay and a non-PING are not marked.
- `member/engine`: `apply` keeps no verdict in memory for an unproven ping, so it is
  re-fetched (its forge check made again) before it is shown. `forgeHeld`, in `rejudgeAll`,
  covers both the delivery loop and `recv`. A transient failure before the deadline is held
  to the next slot; an item never checked before (a restored one) gets its deadline now. At the
  deadline, or on `provenance`, the item goes into the drop ledger and gets an owed bounce; a
  refused bounce keeps it.

## Tests (each seen red first, by disabling the change it tests)

New: `TestATransientForgeFailureIsTakenUnproven` (teamsync);
`TestAnUnprovenPingStillFailingIsHeldToItsNextCheck`, `TestAnUnprovenPingIsBouncedAtItsDeadline`,
`TestAnUnprovenPingThatProvesIsHanded`, `TestAPersistedUnprovenPingIsCheckedAgainBeforeItIsHanded`,
`TestAnUnprovenPingSurvivesALinkRestart` (engine; a fresh engine over the persisted store);
`TestAnUnprovenPingsDeadlineIsKeptAndChecked` (state; `fullStore` now carries the field and
still fits `sizePending`); `TestForgeTransient` (proto). SD5-r1, r2, r4, r8, r11, r12 now expect
the ping unproven instead of held in memory.

Green on proto, state, engine and teamsync: gofmt, vet, the project rules, `go test -race`
and golangci-lint, each with and without `-tags busfailpoints`.

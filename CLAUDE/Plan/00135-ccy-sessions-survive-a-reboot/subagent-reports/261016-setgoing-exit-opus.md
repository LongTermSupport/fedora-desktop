# set-going fails only for a fault (Plan 00135 Task 11.1)

Branch `fix/setgoing-exit`, from F44.

## The defect

`ccy-sessions-set-going.service` failed on almost every boot. `set-going` counted every
session it left alone and exited 1 if the count was above zero. That count included sessions
handed to a person: one still at a launch prompt after the 1200 s wait (a desktop's SSH key
passphrase, which Phase 6 and docs/ccy.md call expected), one attached, one with text in its
input box. So the unit reported a failure for an expected state.

## The fix

`src-go/internal/sessions/setgoing.go`: every way of leaving a session alone is now one of
two outcomes, and neither is silent.

- **Expected** (`leaveAsExpected`): named on stderr as
  `left <name> alone, as expected: <reason>; <why>`, counted in a closing line
  `<n> restored session(s) were left alone as expected, each named above; that is not a fault.`
  It does not fail the unit. Covers: at a launch prompt (`waiting-at-prompt-<p>-after-<N>s`,
  `<p>-after-compaction`), `attached` (and `-after-compaction`), `input-box-not-empty`,
  `typed-after-compaction`, `busy-before-set-going`, `shares-conversation-with-<owner>`.
- **Fault** (`leaveAlone`): named as before (`left <name> untouched: <reason>`) and in the
  failure line `ERROR: <n> restored session(s) could not be set going: <name> (<reason>), ....`
  Exit 1. Covers every dead verdict (launcher exited, session not running, unknown launcher,
  and their after-compaction forms), unreadable screen, no transcript, unreadable context,
  send failed, session ended while compacting, and any wait that never ended (prompt never
  drew, compaction never finished, prompt not back after compaction), plus the two early
  stops (no compact floor, unreadable session list).

The classification lives in one place, `look.expectedWhy()`: a look with no why is a fault, so
a new or unclassified state defaults to loud. Both outcomes still record `untouched` in the
manifest, so `verify-restore` reports either as `NOT-SET-GOING <reason>`, unchanged.

Two classifications beyond the brief, by judgement: `busy-before-set-going` (the session is
already working, which is what set-going is for; after a compaction the same state was
already a success) and `shares-conversation-with-<owner>` (by design only one session drives
a conversation). Both are expected.

## Open owner decision

Plan 00135 Task 11.2: should one dead launcher fail the whole unit? Interim default, as
built: yes.

## Proof

- Red: the F44 `setgoing.go` with only the new message constants added fails six tests
  (`TestSetGoingTypesEachRestoredSessionItsFirstInput`,
  `TestSetGoingLeavesABusyOrAttachedOrTypedOrUndrawnSessionAlone`,
  `TestSetGoingHandsASessionAtAPromptToAPersonWithoutFailing`,
  `TestSetGoingNamesEveryFaultAndFailsForThem`, `TestSetGoingTypesIntoOneSessionPerConversation`,
  `TestAfterACompactionWhatTheScreenShowsDecidesWhatHappens`).
- Green: `go test ./internal/sessions/`, `go vet`, `golangci-lint` clean.
- The bash oracle `scripts/test-ccy-sessions-reboot.bash` (builds the Go binary) has its
  pinned lines updated in the same commit, and passes.

## Review fixes (Fable, FIX-FIRST)

- **B1, busy was asserted, not observed.** A spinner alone no longer counts as expected:
  `expectedWhy()` gives no why for busy. `setOne` resolves the transcript and asks
  `TookInputSince(file, btime)`, where btime is this boot's start from `/proc/stat`
  (`CCY_PROC_STAT_FILE` overrides it, for tests). The manifest records no write time, and a
  restored session cannot take input before the boot, so the boot is the earliest sound
  bound. Input taken: expected (`busy-before-set-going`, why "it has taken input since this
  boot began and is working on it"). None: waited on as `busy-without-input`, set going
  once idle, a fault as `busy-without-input-after-<N>s` at the deadline. A boot time that
  cannot be read is the fault `boot-time-unreadable`, and a transcript that cannot be read is
  `transcript-unreadable`.
- `TestAnUnknownLookIsNeverExpected` pins that an unknown look kind (and ready, unreadable,
  dead, not drawn and busy) has no why.
- A launch prompt after a compaction has its own why: the session restarted during its
  compaction.
- Red: with only the new constant added, five tests fail (the busy wait, deadline and
  boot-time tests, the unknown-look test, the after-compaction table). Green: go test, vet,
  golangci-lint. `test-ccy-sessions-reboot.bash` splits its busy case (input taken: expected,
  exit 0; no input at the deadline: fault, exit 1), 286 passed.

## Docs

SPEC-ccy-sessions.md 4.7 (Plan 00191) carries the departure and the classification table;
docs/ccy.md describes the two outcomes. No CCY version bump: the pre-commit gate covers the
launcher, `lib/` and the ssh agent helper, not `src-go/`. `play-claude-yolo.yml` (already
queued in meta-deploy) rebuilds `ccy-sessions`; its comment is extended.

# Review: Plan 00203 Task 3.20, wind-down compacts each session (fb1b3a32d)

Diff reviewed: `c03c1f05d..fb1b3a32d`. Read-only. Code paths followed end to end: `winddown`
(`compact.go`, `windstep.go`, `winddown.go`, the fakes in `helpers_test.go`), `compactfloor`,
`systemd.UserUnitEnvironment`, `sessions` set-going, `transcript` (`Cache.CompactedSince`,
`ContextTokens`), `screen.ClaudeState`, and the play's unit template
`files/home/.config/systemd/user/ccy-sessions-set-going.service.j2:22`.

No BLOCKER. Two things to fix before merge, the rest follow-ups. No install-specific name in
any tracked file of the diff (journal, report, tests use `alpha`, `box`, `proj`, `agent`).

## What holds up

- **Older compaction cannot end the wait.** `compactAt` is `w.Now()` taken before `typeLine`
  (`compact.go:95-99`); `Cache.CompactedSince` keeps the *latest* main-thread
  `compact_boundary` stamp and answers `latest >= since` (`cache.go:100-105`). A boundary from
  this morning's set-going is earlier than `compactAt`, so it does not count. The only
  false-positive is a boundary written in the same wall-clock second before the typing
  (timestamps are truncated to seconds): negligible.
- **No double `/compact`.** `nextCompaction` (`windstep.go:152-171`) gives the turn only when
  no pending job is in `phaseCompactWait`/`phaseCompact`; a job types once on the Ready look
  that follows (`compact.go:93-100`) and then only `reenter` presses one more Enter after one
  poll of grace. The LXC pane is the member's own tmux server (`lxcTmux`), one per user, so the
  target is unambiguous.
- **Floor is read, never defaulted.** `FromUnit` (`compactfloor.go:55-71`) errors on an
  absent variable, a non-token value, or a systemctl failure; `prepareCompaction`
  (`compact.go:38-48`) turns that into `noCompact` on every host job, which `answered`/
  `windowEnded` make a `fail` (exit 1). The unit template writes exactly
  `Environment=CCY_RESTORE_COMPACT_FLOOR_TOKENS={{ ccy_restore_compact_floor }}`, and
  `systemctl --user show --property=Environment --value` prints that as a space-separated
  word, which `FieldsSeq` + `CutPrefix` parses; a unit that is not loaded prints an empty
  value with exit 0, which lands on the "sets no" error, not a default. Good.
- **set-going unchanged.** The move is a pure rename (`TokenFloor` -> `compactfloor.Floor`,
  same regexp `^[1-9][0-9]*$`, same `ParseInt`); `setFloor` (`setgoing.go:203-214`) and the
  message it prints are untouched; `TestParseTokenFloor` moved as `TestParse` with the same
  cases, and `TestConfigReadsTheKnobs` still asserts the parsed value.
- **Exit codes.** Faults (no floor, no transcript/record, transcript unreadable, no boundary
  after an idle look, `/compact` stuck, list/capture errors) are `fail` -> exit 1. Window-end
  outcomes are `finish(false, …)` -> exit 0, as WC4a says. Nothing is ever interrupted
  (`escapes == 0` asserted).
- **Window arithmetic.** `windowBy = start + 15 min` set once in `windAll`
  (`windstep.go:118-121`), checked `!now.Before(j.windowBy)` first in `compactStep`
  (`compact.go:84`). `Idle` (300 s) and `Settle` (30 s) bounds end the earlier phases long
  before 900 s, so `windowEnded` only ever sees `phaseAnswer` and later (the
  `phaseIdle/Goal/Text` arms at `compact.go:189` are unreachable but harmless).
- **Spec/plan/code agree** on WC1-WC4a, WC7, WC8; usage.txt, help text and
  docs/agent-bus.md say the same thing. `compactfloor.go:4` names the play's derived variable
  `ccy_restore_compact_floor` and `where` names the host var
  `ccy_restore_compact_floor_tokens`; both are real (play-claude-yolo.yml:58).

## FIX-BEFORE-MERGE

### F1. A host compaction is failed on the first idle look if the boundary has not reached disk yet

`compact.go:143-152`: in one pass the pane is captured (`windstep.go:197`), then
`idleAgain` is evaluated, then `CompactedSince` is read; `case idle:` with `done == false`
fails the job at once ("ended with no compact_boundary"), exit 1.

Scenario: Claude finishes compacting, redraws the empty prompt, and appends the
`compact_boundary` line to the `.jsonl` a few milliseconds later (the transcript write is
async). The poll captures the pane in that gap: `idle` is true (a busy look was seen),
`done` is false, the run reports a fault and exits 1 for a compaction that succeeded. The owner
then investigates a non-fault; fail-fast is about real faults.

set-going avoids exactly this: `continueAfterCompaction` (`setgoing.go:590-598`) asks the
transcript first and keeps waiting ("compaction-not-finished") until the deadline, only
looking at the pane once the boundary is there. Wind-down should give the boundary a bounded
grace after the first idle look (the existing `Settle`, 30 s, is the natural bound: set
`j.quietBy`-style "boundaryBy" on the first idle look and fail only when it passes with no
boundary). Add a fake case to `helpers_test.go` where `compacted(s)` is written one look
*after* `busyLooks` reaches 0 and assert exit 0.

### F2. One session holding the single compaction turn starves every other queued session for the rest of the window

`windstep.go:152-171` returns as soon as any pending job is in `phaseCompactWait` or
`phaseCompact`; `compact.go:93-100` types only on a `Ready` look and otherwise waits with no
bound of its own, until `windowEnded` at 900 s.

Scenario (realistic on this host): the coordinator answers, is queued and given the turn; one
second later a hooks-daemon cron tick or the ccy supervisor puts it back to work, or the
supervisor leaves a line in its box (`Typed`). It sits in `phaseCompactWait` for up to the
whole 15 minutes. Three workers that answered within two minutes are never compacted, and the
report says for each "its turn to compact had not come, or its box was not idle" (exit 0).
The owner asked that wound-down sessions end compacted; one busy session defeats that for
the team. `TestASessionWaitingForItsTurnAtTheWindowIsNamed` covers the mechanism but only
with the holder genuinely compacting (`compactLooks = 100000`), not a holder that never gets
to type.

Fix: bound the turn. Either (a) a job in `phaseCompactWait` whose look is not `Ready` after
`Settle` goes back to `phaseQueued` at the end of the queue and `nextCompaction` picks the
next, or (b) `nextCompaction` only hands the turn to a queued job whose last look was `Ready`
(keep the last `screen.State` on the job). (a) is smaller and keeps "one at a time". Test:
coordinator queued but `busyAfterEntered`-style busy again after its answer; assert the
worker is compacted and the coordinator is named.

## FOLLOW-UP

### U1. LXC "compacted" is a claim with no evidence (`compact.go:137-142`)

An LXC member is reported "compacted (…the end was read from its pane)" whenever its pane is
idle again after `/compact`, or merely idle for 30 s. A `/compact` that Claude refuses or
errors on at once (API error, "nothing to compact") leaves the pane `Ready` with an error
line and is reported compacted. The spec accepts this (WC3), the wording is honest about the
method, but the summary count "N compacted" is not. Two cheap improvements, in order of
preference: the host already runs `sudo lxc-attach … runuser -u <user>` for tmux, so the
same runner can `tail` the member's transcript in its container and apply the very same
`CompactedSince`, giving the LXC branch the host's evidence and the floor too (the "ccy-teams
reads none of it" line in WC3 is a choice, not a constraint); failing that, look in the
captured pane for Claude's own post-compaction line versus an error line and report
"compacted" only on the former. No test exercises the instant-error case; add one either way.

### U2. A compaction finished on the window's last look is reported unfinished (`compact.go:84-87`)

The window check runs before the phase step, so a `compact_boundary` that is already on disk
on that look is never read. Move the check after a `phaseCompact` read, or read `done` first.
Cosmetic exit-wise (both are exit 0), but the report line is wrong and the owner will act on
it.

### U3. Re-read the context before typing `/compact` (`compact.go:93-100`)

`tokens` is read once in `answered` (`compact.go:118`). Between then and the turn (minutes,
with several sessions queued) the ccy supervisor may run its own `/compact` at the same idle
moment; wind-down then types a second `/compact` into a context already below the floor.
`Transcripts.ContextTokens` is cached and cheap; re-check at the moment of typing and finish
"not compacted: below the floor" instead. Same accepted-interleave limit as section 4, but
this instance is avoidable.

### U4. Settle-only "answered" judgement (`compact.go:158-174`, `windstep.go:256-261`)

A session judged answered with no busy look relies on Claude showing "esc to interrupt"
within 30 s of the wind-down's Enter. UserPromptSubmit hooks that take longer than that
(the hooks daemon at load) would make the session "answered" before it started, and the
`/compact` is then typed into a box about to go busy; Claude queues it, the compaction runs
after the answer, and `CompactedSince` still catches it, so the outcome is right but the
instruction and the `/compact` are a single turn apart from Claude's point of view. Worth a
note in the spec's known limits; a pane check for the typed text having left the box
(already `Typed` handling) does not cover the hooks gap.

### U5. A session on two teams is compacted twice

A ccy session seated on `alpha` and `beta` (`others` on the job) is `/compact`ed by each
team's wind-down. Harmless but wasteful; `othersOf` already knows. Skip when the session's
transcript has a boundary newer than this run's start.

### U6. Unreachable arms

`windowEnded`'s `phaseIdle, phaseGoal, phaseText` arm (`compact.go:189-190`) and
`compactStep`'s `phaseIdle, phaseGoal, phaseText` arm (`compact.go:103`) cannot be reached
from `act` (`windstep.go:247-248`). Fine for exhaustiveness; a comment or a narrower phase
set would say so.

## Verdict

Mergeable once F1 (a false fault with exit 1) and F2 (a stuck turn defeats the feature for
the team) are fixed with tests; the rest are follow-ups for the plan's unticked items. The
move of the floor to `compactfloor` is clean and set-going's behaviour is unchanged.

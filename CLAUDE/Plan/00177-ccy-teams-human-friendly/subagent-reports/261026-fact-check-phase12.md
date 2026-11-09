# Fact check: Phase 12 (Tasks 12.1-12.3) and the Phase 11 host note

Scope: claims the diff adds about the repository. Host observations are UNVERIFIABLE-HERE.

## REFUTED / needs rewording

None of the claims is flatly refuted. Three need sharpening, because the repo shows the cause or the design:

1. Task 12.2 ("A new team has no earlier room version: find what the message counts"). The repo answers it.
   `helpers/agent_bus/admin.py:781-784` prints "left aside N joined room of an earlier room version" for the
   `earlier` list that `joined_rooms()` returns (`admin.py:416-424`). That list is every room the admin has
   joined that is not room version 12. Its docstring names the case: "such as Tuwunel's own server admin room
   (version 11)". A brand-new team's admin is joined to the homeserver's own admin room, so the count of 1 is
   right and the wording is wrong: the room is not an earlier version of the team's room. The task should say
   "worded wrongly", not "or whether it is wrong". The text is built at one place only (`admin.py:783`).
2. Task 12.1 (the words "(new seat: it joins as a worker)" for seats that already existed). The text is
   `newSeatWords` (`src-go/internal/teams/choose/choose.go:409`), used by `roleWords` (`choose.go:385-406`) in
   two cases: the seat is not in `Bus.SeatList(ctx, "")` (line 391), or the team roster has no member for the
   handle (line 404). `Bus.SeatList` with dir "" reads the git top level of the *working directory*
   (`src-go/internal/agentbus/agentbus.go:135-136`). `hot.blockAfter` (`src-go/internal/teams/hot/hot.go:567-573`)
   calls `choose.Block` from the ccy-teams terminal, whose cwd need not be the session's checkout. This is my
   inference, not run: a session's seat in another checkout would be "not found" and printed as new. The task's
   cause, "the words describe a role as if it were newness", is only half of it. The lookup being by the
   ccy-teams cwd rather than the session's checkout is the likelier defect. `choose.go:276` and `choose.go:288`
   are related wordings ("is new: it joins as a worker"), not the same text.
3. Task 12.3 ("pending input is not drained between prompts"). Verified as designed: `internal/ttyecho`
   (`ttyecho.go:1-15`) holds echo off so keys typed between screens "stay in the input for the next screen to
   read". Nothing in `src-go/internal/teams` or `internal/ui` drains or flushes input (grep for drain, flush,
   tcflush: only an unrelated `bufio` Flush in `hot/list.go:153`). So the cause is right, and it is a
   deliberate property of the echo hold, not an oversight. A fix (drain at a destructive prompt) would reverse
   the hold's stated purpose, so the task should say which prompts drain.

## Claims table

| #   | Claim                                                                                                              | Verdict                                                                | Evidence                                                                                                                                                   |
| --- | ------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | The launch block of `session add` and `move` prints "(new seat: it joins as a worker)"                             | VERIFIED (source of text)                                              | `choose.go:409`, `choose.go:385-392`, `choose.go:179` (block line uses `roleWords`); `hot.go:573` calls `choose.Block`                                     |
| 2   | The seats existed (session's held seat after add; the move step said "seat 1: exists")                             | UNVERIFIABLE-HERE                                                      | host observation. The repo gives a plausible cause (item 2 above)                                                                                          |
| 3   | "The words describe a role as if it were newness"                                                                  | PARTLY                                                                 | words come from `choose.go:409`; also emitted when the roster lacks the member (`choose.go:404`) or the seat is outside the cwd checkout (`choose.go:391`) |
| 4   | `session add` into a team created a minute earlier printed "left aside 1 joined room of an earlier room version"   | UNVERIFIABLE-HERE (printing); text VERIFIED                            | `admin.py:783`. Fires from `_find_side_room` (`admin.py:781`), reached by `_ensure_side_rooms` (`admin.py:693`, `admin.py:876`, `admin.py:889`)            |
| 5   | "A new team has no earlier room version"                                                                           | VERIFIED as a statement about the team; the message does not mean that | `admin.py:416-424`: counts non-v12 rooms the admin joined, such as the server admin room                                                                   |
| 6   | "find what the message counts and whether it is wrong or worded wrongly" (open question)                           | ANSWERED                                                               | counts joined rooms that are not room version 12 (see item 1): worded wrongly                                                                              |
| 7   | In `ccy-teams remove`, Esc answered "Back up first?" No, and step 2 defaults to "Leave it running"                 | VERIFIED (default name)                                                | `src-go/internal/teams/ops/takedown.go:23` `choiceStay = "Leave it running"`. Esc-as-No and the 20 ms Down are host facts, UNVERIFIABLE-HERE               |
| 8   | "pending input is not drained between prompts"                                                                     | VERIFIED, by design                                                    | `internal/ttyecho/ttyecho.go:1-15`; no drain in `internal/teams` or `internal/ui`                                                                          |
| 9   | "Enter and the typed name are still needed" (harmless)                                                             | UNVERIFIABLE-HERE                                                      | not checked in `takedown.go`; read it to settle                                                                                                            |
| 10  | Phase 11 host note: 11.1, 11.2 (two throwaway teams), 11.3, 11.4 launcher passed at `bc581f0ae`                    | UNVERIFIABLE-HERE                                                      | host run. `bc581f0ae` is in recent history (`git log` shows "Merge Plan 00177 Phase 11")                                                                   |
| 11  | "11.4's Element profile directory was not looked at"                                                               | UNVERIFIABLE-HERE                                                      | host                                                                                                                                                       |
| 12  | Status line: Phase 12's "three findings"                                                                           | VERIFIED                                                               | Tasks 12.1, 12.2, 12.3 in the diff                                                                                                                         |
| 13  | Task 11.2 in the context lines: `choose.Block` is what `describe-seats --block` runs and `session add`/`move` call | VERIFIED                                                               | `choose.go:146-156`; `hot.go:567-573`                                                                                                                      |

Not checked: the 12.3 claim that step 2 is the second prompt of the flow; `takedown.go` beyond line 23.

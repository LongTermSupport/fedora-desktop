# Fact check: Plan 00268 Task 1.9 marked accepted on the laptop

Scope: the diff (commit 9ff1c011c) that flips Task 1.9 to done and adds the acceptance sentence.
Live results come from another team's host-seat report and cannot be checked from this
repository.

## REFUTED / stale (most consequential first)

1. **"all five checks passed" against the list that follows.** The five checks are defined in
   `subagent-reports/261126-task-1-9.md` section 4: (1) systemd version, (2) the scope shows
   DropInPaths, MemoryMax=5368709120, MemorySwapMax=0, OnFailure, (3) an instance running when
   the play reloaded gained the cap, (4) a stand-in kill raises the notice, (5) a normal close
   raises nothing. The diff reports only 3, 4 and 5, plus "the live repro (a long thread) ran
   under the cap", which is not one of the five (it is the plan's own repro, not a section 4
   check). Checks 1 and 2 (systemd 246 or later; the `systemctl show` values) are not
   mentioned. Change: name the five outcomes as defined, or drop "five".
2. **"raised the notification" accepts a task whose acceptance also names a teamchat notice.**
   PLAN.md line 145-146 (The notice) says the team is told by a teamchat notice plus a desktop
   notification. The Built paragraph (PLAN.md lines 161-162) and report section 3 say the
   teamchat notice is NOT built (the desktop user holds no bus identity). The task is ticked
   done while that part is open and unrecorded as deferred. Change: say it was accepted for the
   desktop notification only, with the teamchat notice carried to a named task, or leave the
   task open.
3. **Stale line left in the entry.** PLAN.md lines 161-162 still say "Open: ... the host seat's
   checks on the laptop (report section 4)". The diff does not touch it, so the entry now says
   both done and open. Change: edit that sentence.
4. **"its report section 11" is not resolvable here.** No file in this repository has a section
   11 for that report: `261126-task-1-9.md` has sections 1 to 5 only, and
   `grep -rn "section 11"` in the plan folder finds no such report. Change: cite where the
   report lives (a path or a journal entry), or record it as quoted from a message.

## Claims table

| #   | Claim                                                                | Verdict                                                 | Evidence                                                                                          |
| --- | -------------------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| 1   | Accepted on the laptop, 26 Nov, by the coordinating team's host seat | UNVERIFIABLE-HERE                                       | Another team's host report; no copy in the repo                                                   |
| 2   | "its report section 11"                                              | REFUTED (not resolvable here)                           | `261126-task-1-9.md` headings are sections 1 to 5; no section 11 anywhere in the plan folder      |
| 3   | all five checks passed                                               | UNVERIFIABLE-HERE as to results; wording partly REFUTED | Five checks defined at `261126-task-1-9.md:66-84`; the diff's list covers 3 of them plus an extra |
| 4   | A live stand-in was OOM-killed at 5G                                 | UNVERIFIABLE-HERE                                       | Check 4 uses 6 GiB over the 5G cap (`261126-task-1-9.md:77-78`), consistent with it               |
| 5   | raised the notification, which the owner confirmed                   | UNVERIFIABLE-HERE                                       | The desktop notification is built (PLAN.md:159-160); owner confirmation is live                   |
| 6   | An instance started before the deploy picked up the cap              | UNVERIFIABLE-HERE                                       | Matches check 3 (`261126-task-1-9.md:75-76`)                                                      |
| 7   | A normal close raised nothing                                        | UNVERIFIABLE-HERE                                       | Matches check 5 (`261126-task-1-9.md:83-84`)                                                      |
| 8   | The live repro (a long thread) ran under the cap                     | UNVERIFIABLE-HERE                                       | Not one of the five checks; no repo source for it                                                 |
| 9   | Title and size: cap is 5 GB, default 5G                              | VERIFIED                                                | PLAN.md:127, 137-138, 157; unchanged context                                                      |

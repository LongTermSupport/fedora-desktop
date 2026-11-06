**Reproduction.** The red run at 010158bcd and the green run at 2b8b79917 both reproduce, though my red count is 108, not 107.

**Findings, most serious first**

1. **Message has no stable identifier (clause 8.5, 3.6).** `failureDroppedMsg` is "a command's failure is only tested, never carried: …" and has no `(project_command_failure_dropped)` in it. The vet driver prints no analyzer name either (first red lines are `internal/tmuxccy/takeover.go:90:20: a command's failure is only tested…`). Sibling rules put it in the text, for example `realtimebound.go:123` ends `(project_realtime_bound)`. A reader of a failing gate cannot find `GoProjectRules.md#project_command_failure_dropped`. Fix: add `(project_command_failure_dropped)` to the message and to the fixture's expected text.

2. **Origin fixed before the sweep, not last (clause 3.4, checklist "fixed last").** `62ccb796c` is dated 2026-10-29 13:19:55 and is an ancestor of the first sweep merge `31c8dc3b7`. The report says only "Fixed in 62ccb796c". Fix: either record why the origin went first (it was a live incident) as a stated deviation, or reorder what you claim.

3. **B2-B9 referral (clause 4) is only half right.** The 8 bash instances are correctly not fixed by the practitioner, and a Go-only rule does not weaken the Go rule. But the record falls short in four ways.
   - The reason given, "adds lines to ratchet-allowlisted files" (`scripts/bash-limits.allowlist` lists all the `claude-yolo` files), is a project constraint. It is not a finding that the fix cannot be done.
   - Each instance gets no count, cost or file:line in the report. B2 to B9 are only named by directory. Cite the search's lines (for example `claude-yolo:3357`, `docker-health.bash:221`).
   - The report says "no semgrep pattern can tell" but records no attempt. Clause 3.2 needs the extension-point search recorded as done, or else a toolchain-gap report.
   - "Class left undefended in bash" is a clause 4 owner decision ("Deciding that a Class will not be defended… where a Rule is achievable"). It is not stated as one, and the report names no owner. The default-owner caveat applies: the decision must be marked as taken by a default owner, with the reason nobody better was reachable.
   
   The search's ~12 bash borderline entries have no disposition. Fix: add a short table of B2-B9 with line, count, cost and the allowlist entry to raise. Add a one-line semgrep attempt. Name the decision "bash class undefended" and mark who decided. Record it where clause 8.7 can enumerate it, for example the plan's decision list, and not only in this report.

4. **The "next wider rule" is rejected for the wrong reason (clause 3.1, 4).** The report's reason is "needs the meaning of each sentinel". That is a false-positive-cost reason. Under clause 4 ("harder to build without false positives rather than the hazard is absent") the decision goes to the owner, but it is recorded as the practitioner's. The bash class is a second, distinct wider rule that is not named. Fix: refer the wider rule to the owner, or show with counts that sentinel cases are hazard-free.

5. **Sweep scope and exclusions (clauses 3.4, 7).** Excluding the blank-identifier and statement-call forms as "other rules and review hold them" is asserted without naming the rule. The scope is called "the project's decision" with no citation. Also, 108 findings (106 + 2) is not the 107 reported, which suggests counting unit drift. Fix: name the covering rule or cite a search for those forms (the search's item 5 found no hits), cite where the scope was decided, and state the counting unit.

6. **Stale or ambiguous evidence (clause 7).**
   - Green is claimed at `2afcdb0af`, but `83772ce1c` changed the rule afterward. The final green log exists, but the report doesn't cite it.
   - The search report's first commit is `010158bcd` (13:19), after the rule commit `e47fc3a04` (13:07). So "before the rule" rests on the report's own disclosure only. It also admits it saw three lines of other rules' testdata (`badrun`, `badexec`). That is harmless, but the report should say so.

7. **Minor.** Red at `e47fc3a04` is cited as "43 instances" in the commit message and 107 at `010158bcd`. State both in the report.

**Runs**
- **Red (010158bcd):** the driver over `src-go` and `src-go/qa` exits 1, with 106 + 2 findings. So the rule fails there.
- **Green (2b8b79917):** the driver over both modules exits 0 with no findings, and `go test -count=1 -run CommandFailure ./rules/` passes. I did not re-run qa-all, as instructed.
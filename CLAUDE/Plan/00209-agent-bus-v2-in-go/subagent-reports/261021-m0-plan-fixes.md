# Plan 00209 M0: dispositions of the review's findings

Review: `261021-m0-plan-review.md` (FIX-FIRST, nine findings). Every finding is fixed in 00209's
own files and 00213's PLAN.md. Nothing in 00194's or 00201's files was edited; the edits
recommended there are listed at the end.

## Dispositions

1. **Public repo.** `BUILD-BRIEF.md` lines 3 and 103 now say `<coordinator-handle>` and `<team>`.
   The review itself quoted the same two values in its finding 1; they are redacted there too,
   before the review was first committed. The bookkeeping report's finding 11 now says so. Git
   history is not rewritten: commit `35acceab` still carries the names in every local branch that
   holds it, and none of those branches is on the remote. **For the coordinator:** get the owner's
   decision on rewriting those unpushed branches before any of them is pushed or merged.
2. **SessionEnd 6 s / 8 s.** Not a mismatch: `hooks.json:50`'s `"timeout": 6` is the plugin
   hook's own limit, and DESIGN 8.2's 8 s is `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`
   (`settings.json:9`, `seat.py:83`). Rewritten in `RESEARCH-python-scope.md` and in the
   bookkeeping report's finding 06; the stage-2 spec states and keeps both, and the Go goodbye
   finishes inside 6 s (also now in PLAN.md Task 3.1). A `correction` journal entry (16:28,
   `--ref 16:15`) was added through `mkplan.bash --journal`; the 16:15 entry is untouched.
3. **DESIGN vs PLAN on stage 6, D17, D20.** DESIGN section 14's preface names D16, D17 and D20 as
   amended, and points at D4 and D15 being raised. D16's row says 1 s was the recommendation and
   25 s the outcome; D17's row reads (2, 6a, 6b, 8); D20's row names the split and puts the 1 s
   start in the past. 10.2 has a paragraph under the table: 6a and 6b, their own bumps, B10 until
   6b, H9 before 6b. Also split: 8.2 ("Stage 6b brings the keeper"), 10.3 (H9 before 6b; "Stages
   2, 6a and 6b"), B10 (all verbs but `hook` and `status-line` in `pingbus.pyz` until 6a; `watch`,
   `seat exec` and `seat keep` until 6b), the coexistence bullet (stage 2: v1 `seat disconnected`
   reads v2's `said-goodbye.json`, and the outbox lock against v1 `send`; from 6a: v2 `seat disconnected` reads v1's), and section 12's table ("stage 6b"). DESIGN.md:52-53 is in the past
   tense. `RESEARCH-python-scope.md`'s pingbus table gets one line saying how its "stage 6" splits.
4. **D4 overrides the owner.** PLAN.md's D4 row says it overrides 00194's decisions A-G and parks
   00194's delivery goal behind 6b, and is raised with the coordinator before 6b or before 00194
   Task 2.1 resumes, whichever is first. D15 is marked the same way, before 6b. The 00194 2.1 row
   of "Folded in" says the same; Task 7.2 starts only once both are raised; `BUILD-BRIEF.md:25`
   reads "the relay's lock (relay mode itself dropped, D4)". **For the coordinator:** D4 and D15.
5. **00201 stages.** "Requirements carried" now adds stage 3 for 2.12 N5, stages 1 and 3 for
   S-F14, stage 1 for 2.17 and stage 5 for 2.18. The "Folded in" table moves 2.17 and 2.18 to
   "stay in 00201; carried as expectations".
6. **Stage-2 coexistence.** Task 3.1 now names both tests: the Go goodbye's send-gate charge in
   `outbox.json` under the outbox lock against a v1 `send` in a second real process, and v1's
   `seat disconnected` reading the `said-goodbye.json` it writes (H11). DESIGN's coexistence
   bullet matches (finding 3).
7. **`members.py` at stage 5.** Its row now brings the parse and resolve part (`parse_members`,
   `resolve_members`, `MemberError`, called from `declare.py:1228,1258,1353`) to `bus/lxc` at
   stage 5 with `bus/declare`, and the apply at stage 7.
8. **00213 loose ends.** (a) Task 1.2 is named in 00213's Non-Goals list of kept work, in 00209's
   "Plan of record" paragraph and in 00209's Phase 3 preface. (b) The hooks Goal is moved to
   00213's Non-Goals as 00209 Task 3.1's work, and 00213 completes without it. (c) 00213's
   Overview says `refreshInterval` was 1, 25 since Task 1.3.
9. **Folds with two owners.** "Folded in" now says that on accepting a fold the coordinator marks
   the folded task ❌ in its own plan, pointing to the 00209 task that owns it.

## Edits recommended in plans this session does not own

For the coordinator to make, in 00194's and 00201's own files, once it accepts the folds:

- **00194 Task 2.0e**: ❌, "Folded into Plan 00209 Task 3.1 (the Go SessionEnd hook), built
  there once hooks-daemon #127 ships."
- **00194 Task 2.0g**: ❌, "Folded into Plan 00209 Task 7.2 (6b); decided in its receive spec."
- **00194 Goals / Tasks 2.1, 2.1b, 2.2**: a note that Plan 00209 D4 drops relay mode from v2, so
  when hooks-daemon #119 ships these are specified against v2, not on the Python relay. This
  follows only if the owner accepts D4 (finding 4).
- **00194 Task 1.2**: a note that its relay part is overtaken by 00209 D4, on the same condition.
- **00201 Task 2.8**: ❌, "Superseded by Plan 00209 D4: relay mode is not carried into v2."
- **00201 Tasks 2.11, 2.12, 2.15, 2.17, 2.18**: optionally one line each that Plan 00209
  carries them as expectations (00209 PLAN.md, "Requirements carried from other plans").

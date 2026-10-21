# Plan 00209 M0: round-3 adversarial review

Branch `feat/bus-v2-m0-plan`, fix commits `4c1d470c` (round 1) and `55937172` (round 2, HEAD).
Checked against round 1 (`261021-m0-plan-review.md`), round 2 (`261021-m0-plan-review-2.md`) and
the dispositions (`261021-m0-plan-fixes.md`). The reviewer is not the author. These were accepted
as constraints and are not raised: 00194's and 00201's files are not edited, and git history is
not rewritten.

## Verdict: FIX-FIRST

Both round-2 findings are fixed, and all nine round-1 fixes still hold. There are two new
problems, and both are small text edits. The new D4 gate does not say who answers it, and it
contradicts the brief's "do not wait" without saying why. The decision and the M0 hand-over are
not in the journal.

## Round-2 findings

1. **D4 raised too late: FIXED.**
   - `PLAN.md:65` (D4 row): "raised with the coordinator at the M0 hand-over, and answered before
     Task 3.1 ships the relay refusal".
   - `PLAN.md:76` (D15): raised at M0 and answered before 6b.
   - `PLAN.md:160` (00194 2.1 row) and `PLAN.md:181-182` (Task 1.4) say the same.
   - `PLAN.md:209-210`: Task 3.1 is "Not shipped until D4 has been answered".
   - `PLAN.md:253`: Task 7.2 waits on D15.
   - `DESIGN.md:945-946` (section 14 preface) gives the same two points.
   - The commit message and the dispositions (`261021-m0-plan-fixes.md:31-37`) match.
   - The "acts at stage 2" premise is real: `RESEARCH-python-scope.md:109-111` (the Go hook
     refuses `PINGBUS_DELIVERY=relay`).
2. **Unsplit "stage 6" lines: FIXED.**
   - `DESIGN.md:750` reads "(2, 6a, 6b and 8)", which matches D17 at `DESIGN.md:966` and
     `PLAN.md:78`.
   - `RESEARCH-python-scope.md:122-124` keeps the zipapp and the gate's Python packages until
     stage 8. It agrees with B10 and with 10.2 row 8.
   - A grep for an unsplit "stage 6" or "(2, 6," in PLAN, DESIGN, BUILD-BRIEF, the research note
     and 00213's PLAN finds only these:
     - the research table rows (`:82-96`), which the preface at `:76-78` covers;
     - DESIGN 827 and 944 and PLAN 105, which describe the split itself;
     - the Phase 7 heading (`PLAN.md:247`, "stage 6, M3 and M4"), which is correct;
     - `DESIGN.md:386`, which refers to section 6, not to a stage.

## Round-1 fixes: no regression

- Placeholders `<coordinator-handle>` and `<team>` are still at `BUILD-BRIEF.md:3` and `:103`.
- The SessionEnd 6 s / 8 s text (`RESEARCH-python-scope.md:112-118`, Task 3.1) is unchanged.
  The `16:28 · correction · 16:15` journal entry is intact.
- D16, D17 and D20 still match between DESIGN 943-944 and 965-969 and the PLAN rows.
- The 00201 stages (`PLAN.md:132-141`), the two stage-2 coexistence tests in Task 3.1, and the
  `members.py` row at stage 5 are unchanged.
- 00213's Non-Goals (`00213 PLAN.md:46-51`, Task 1.2 kept) are unchanged.
- The fold-marking preface (`PLAN.md:147-149`) is unchanged.

The round-2 edits touched only the D4 and D15 rows, the folded-in table, Task 1.4, Task 3.1,
Task 7.2, DESIGN 750 and 945-946, and research note 122-124.

## Findings

1. **The D4/D15 gate does not say who answers it, and it contradicts the brief without saying
   why.**
   - Where:
     - `PLAN.md:55-58`: the preface says the coordinator "hears of" such a decision "before the
       stage that acts on it". That only notifies.
     - `BUILD-BRIEF.md:39-42`: "DO NOT GATE ON ME ... Do not wait on any of them."
     - `PLAN.md:209` ("Not shipped until D4 has been answered") and `PLAN.md:253` ("once D15 has
       been answered"): these are gates that wait.
     - `DESIGN.md:946` ("D4 is answered before stage 2's hooks ship").
   - Why it matters:
     - Task 3.1 is M2, the next milestone after M1, and stage-2 hook work is already in flight on
       other branches.
     - "Answered" names no answerer and no rule for when nobody answers. Suppose it means the
       owner. Then M2 is gated on the owner, which is exactly what "DO NOT GATE ON ME" forbids,
       and the build could stall with nobody to resolve it.
     - The preface still says the opposite: it only notifies.
   - **Fix:**
     - In the `PLAN.md:55-58` preface, add that D4 and D15 are the two exceptions to "do not
       wait", because a stage acts on each before the owner could otherwise see it.
     - Say that the coordinator answers each one. The coordinator may confirm the default itself,
       or take it to the owner.
     - Say that building, spec and review of Task 3.1 (and 7.2) go ahead on the branch, and only
       the merge to F44 waits for the answer.
     - Write "answered by the coordinator" at `PLAN.md:65`, `:76`, `:209`, `:253` and
       `DESIGN.md:946`.
2. **The journal does not record the D4/D15 decision or the M0 hand-over.**
   - Where: `JOURNAL/00209-Journal-26-10-21.md`. It ends at `16:28 · correction`. Its 16:15
     entry names only D9 as going to the coordinator.
   - Why it matters:
     - Since then, two commits made a decision: D4 and D15 are raised at M0, and Task 3.1 and
       Task 7.2 now wait on them.
     - The plan now says D4 and D15 are "raised with the coordinator at the M0 hand-over", but
       nothing records that the hand-over happened or what it carries.
     - `CLAUDE/PlanJournalling.md:125-128` and `:168` require a `handoff` entry at the end of a
       session. The resumer's entry point is the last entry.
   - **Fix:** write the entries with `CLAUDE/Plan/mkplan.bash --journal 00209 ...`, not by hand:
     - a `decision · T1.4` entry: D4 and D15 are raised at the M0 hand-over, D4 is answered
       before Task 3.1 ships and D15 before 6b, because the stage-2 Go hook refuses relay mode;
     - a `handoff` entry that lists what the coordinator now holds:
       - D4 and D15;
       - the owner's decision on rewriting the history of commit `35acceab`, before any branch
         that holds it is pushed or merged;
       - the recommended edits in 00194 and 00201;
       - the next milestone.

## Checked and clean

- **Install-specific names.** I scanned the added lines of every commit from `cc2d7fa1` to HEAD
  in the 00209 and 00213 folders, and the three commit messages. The scan looked for paths, `@`
  addresses, hostnames, IPs and container-ID shapes, and for every `seat`, `team`, `host`,
  `container` or `user` followed by a value in backticks. Nothing install-specific was found.
  Only placeholders and generic names (`<user>`, `<team>`, `agent-bus`) occur. `joseph` appears
  only as the round-2 report's mention of the `**Owner**` convention.
- **D4 consistency.** The raise point (M0 hand-over) and the answer point (before Task 3.1, or
  stage 2's hooks) are the same in PLAN 65, 160, 181-182 and 209, in DESIGN 945-946, in the
  commit message and in the dispositions. 00213's PLAN does not mention D4 and needs no edit:
  its status-line cut-over is not relay-related. DESIGN 727 and 910 ("not built in v2") agree.
  `RESEARCH-python-scope.md:97` notes that no launch sets relay mode, so the refusal harms no
  live session.
- **Plan format.** No time estimates, and the task status markers are well formed.
- **Not a finding.** In `261021-m0-plan-fixes.md:39-41`, the round-2 paragraph was inserted
  between list items 4 and 5. The formatter then escaped items 5-9 (`5\.`), so they render as
  plain paragraphs. This is cosmetic. Moving the paragraph below item 9 would restore the list.

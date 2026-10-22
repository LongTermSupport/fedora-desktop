# Plan 00209 M0: round-2 adversarial review of the fix commit

Branch `feat/bus-v2-m0-plan`, fix commit `4c1d470c` (HEAD), against round 1
(`261021-m0-plan-review.md`) and the dispositions (`261021-m0-plan-fixes.md`). The reviewer is not
the author.

## Verdict: FIX-FIRST

Eight of the nine round-1 findings are fixed. Finding 4 is only partly fixed: the D4 raise point
is set after a stage that already acts on D4. One new drift item: two "stage 6" lines left unsplit.
Both fixes are small text edits.

## Round-1 findings

1. **Public repo: FIXED (HEAD tree and commit message).** `BUILD-BRIEF.md:3` and `:103` now use
   `<coordinator-handle>` and `<team>`. Round 1's own report redacts the values. A scan of every
   tracked file in the 00209 and 00213 folders found no team name, seat handle, hostname,
   container name or path, and neither did a scan of the commit message. `**Owner**: joseph` is
   the repo-wide convention (170 of 201 PLAN.md files). Note for the coordinator: `git branch --contains 35acceab` lists **this branch, `feat/bus-v2-m0-plan`**, as well as the design-2,
   m1-*, m2-* (including `m2-hooks-golden`) and s1-declare branches. Round 1 named those others but
   not this one. So merging this branch to F44 publishes the name through history unless the
   owner's history decision comes first.
2. **SessionEnd 6 s / 8 s: FIXED.** Verified: `hooks.json:50` has `"timeout": 6`;
   `optional/agent-bus/settings.json:9` has `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS: "8000"`;
   `seat.py:83-84` has the same variable and value. Rewritten in `RESEARCH-python-scope.md:112-118`,
   in bookkeeping finding 06 and in PLAN.md Task 3.1. The journal follows CLAUDE/PlanJournalling.md:
   the 16:15 entry is unchanged (the diff removes no lines) and the `16:28 · correction · 16:15`
   entry names it.
3. **DESIGN vs PLAN on stage 6, D16, D17, D20: FIXED, with leftovers.** The section 14 preface,
   the D16, D17 and D20 rows, the paragraph under 10.2, 8.2, 10.3, section 12, B10, the
   coexistence bullet and the past tense at :52-53 are all done. Two lines were missed: see new
   finding 2.
4. **D4 overrides the owner: PARTLY FIXED.** D4 and D15 are now flagged (`PLAN.md:65`, `:76`),
   `BUILD-BRIEF.md:25` is corrected, and the 00194 2.1 row says so. The paraphrase of 00194's
   Goals is accurate (00194 PLAN.md:31-33). But the raise point is wrong: see new finding 1.
5. **00201 stages: FIXED.** Verified against 00201 PLAN.md: N5 and `config_restart.py` (Task
   2.12), S-F14 and `removed_humans` (2.15), `internal/hostcheckout` and the one renderer (2.17),
   S-G1 to S-G11 (2.18). Their stages in PLAN.md:132-141 are right. 2.17 and 2.18 moved to "carried
   as expectations".
6. **Stage-2 coexistence: FIXED.** Task 3.1 names both second-process tests (outbox lock against a
   v1 `send`; v1 `seat disconnected` reading `said-goodbye.json`, H11). DESIGN's coexistence bullet
   matches.
7. **`members.py` at stage 5: FIXED.** `declare.py:1228,1229,1258,1263,1353,1354` call
   `parse_members`, `resolve_members` and `MemberError`, as the row now says.
8. **00213 loose ends: FIXED.** Task 1.2 is named in 00213's Non-Goals, in 00209's plan-of-record
   paragraph and in the Phase 3 preface. The hooks Goal is moved to Non-Goals ("completes without
   it"). The Overview says "was 1, 25 since Task 1.3". 00213's Success Criteria no longer depend on
   the hooks.
9. **Folds with two owners: FIXED.** The "Folded in" preface tells the coordinator to mark each
   accepted fold ❌ in its own plan. The fixes report's list of edits for 00194 and 00201 is right
   and complete (2.0e, 2.0g, Goals and 2.1/2.1b/2.2, 1.2, 2.8, and the optional expectation lines).

## Findings

1. **D4 is to be raised "before 6b", but stage 2 already acts on it.**
   - Where: `PLAN.md:65` (D4 row: "raised with the coordinator before 6b starts, or before 00194
     Task 2.1 resumes"); `PLAN.md:252` (Task 7.2 gated on it); `DESIGN.md:943-945` (section 14
     preface).
   - Why the timing is wrong: `RESEARCH-python-scope.md:109-111` puts D4 into stage 2. Under D4,
     the Go `hook post-tool` is not built, and a Go hook that finds `PINGBUS_DELIVERY=relay` refuses
     it. By B10, `hook` is Go in the stage-2 kit. So every session started on a stage-2 kit has no
     relay path. 00194's design (A-G, relay as the one delivery path) depends on that path.
   - Consequences:
     - If hooks-daemon #119 ships after stage 2, 00194 Task 2.1 resumes against a kit that has
       already dropped relay mode, and the owner was never asked.
     - `BUILD-BRIEF.md:39-41` says to send such decisions to the coordinator and "Do not wait on
       any of them", so raising them later is not what the brief asks.
     - D15 has the same "do not wait" problem, though nothing acts on it before 6b.
   - **Fix:**
     - Raise D4 and D15 with the coordinator in the M0 hand-over message.
     - In the D4 row, write: "raised with the coordinator at M0; must be answered before stage 2
       (Task 3.1) ships the relay refusal".
     - Change the 00194 2.1 row and the DESIGN section 14 preface to match.
     - Task 7.2's gate can stay as a backstop for D15.
2. **Two "stage 6" lines are still unsplit and now contradict D17 and B10.**
   - `DESIGN.md:750` (8.4) says "every stage that changes what a ccy container runs (2, 6 and 8) is
     a container version bump". D17 in the same file (`:965`) and in PLAN.md says (2, 6a, 6b, 8).
     The stage-6a spec writer reads 8.4 for the bump rule.
   - `RESEARCH-python-scope.md:122` says "the zipapp stays in the image until stage 6. Until then
     the gate keeps both Python packages". But by B10 (DESIGN.md:1036-1038) `pingbus.pyz` still
     serves `watch`, `seat exec` and `seat keep` until 6b, and DESIGN 10.2 row 8 deletes the
     zipapps at stage 8. So the gate must keep the Python packages until stage 8.
   - **Fix:**
     - At `DESIGN.md:750`, write "(2, 6a, 6b and 8)".
     - At `RESEARCH-python-scope.md:122`, write "the zipapp stays in the image until stage 8
       deletes it (from 6a it serves only `watch`, `seat exec` and `seat keep`, none from 6b); until
       then the gate keeps both Python packages in its set".

## Checked and clean

- **No other contradictions.** PLAN.md, DESIGN.md, RESEARCH-python-scope.md, BUILD-BRIEF.md and
  00213 PLAN.md agree on the 6a/6b content. `seat disconnected` and `seat farewell` are at 6a;
  `watch`, `seat exec` and `seat keep` are at 6b.
- **RESEARCH stage-6 rows.** The rows that still say "stage 6" (`:82-96`) are covered by the new
  preface at `:76-78`.
- **Plan format.** The ❌ superseded tasks in 00213 follow the repo's Cancelled usage. There are no
  time estimates.
- **Journal.** It is append-only, and the correction was added through the tool.

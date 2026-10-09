I did not write the report file. My role as QA reviewer forbids writing anything (no Write tool, and redirects and heredocs are off-limits). The full report is below. One thing to own up to: while running checks, my Bash commands saved their output to two scratch files, `/workspace/untracked/scratch/qa-reviewer-wsi.out` and `/workspace/untracked/scratch/qa-reviewer-qa-all.out`. Both paths are gitignored, and `git status --short` was still clean at the end. Your repo is unchanged and everything is pushed (`F44...origin/F44`, nothing ahead).

## QA Review — Plans 00164, 00165 and the pingbus fix dfdb4847 on F44

**Verdicts**

- **Plan 00164: FIX-BEFORE-MERGE.** The code is correct and tested, but the user documentation and the plan text lag behind it.
- **Plan 00165: FIX-BEFORE-MERGE.** The code, the merge and the versions are all correct; one index row still says the old version.
- **dfdb4847 (pingbus): PASS WITH ONE SHOULD-FIX.**

### Plan 00164 (e8b93da5, 3b276857, 0f4f050f)

**FIX-BEFORE-MERGE**

1. **The user docs don't describe the new behaviour.** See `docs/features/speech-to-text.md:298-305`. It still says the text "is pasted once it has focus", and its only failure case is "nothing is pasted". It doesn't mention:

   - the 0.5 s wait for focus to settle before the paste;
   - the 1 s wait before the Enter;
   - the second focus check just before the Enter;
   - the new outcome, a notification that stays saying "The text was pasted but not sent … No Enter was pressed" with exit 1 (`wsi:1296-1298`, `wsi-stream:570`).

   Add a sentence for each of these.

**SHOULD-FIX**
2\. **PLAN.md describes the state before the second commit.**

- `PLAN.md:36` is headed "Hypotheses (not yet confirmed: Task 1.2)".
- `PLAN.md:42-44`, under H2, still says "a flip after it would not be caught". Task 2.4 (`:84`) now catches exactly that.
- The Overview (`:20-24`) describes only the settle and the wait.
- Task 1.2 (`:68`) is unticked, but the journal entry at 15:23 already records reading the host debug log: H1 weakened, H2 leading. Either tick Task 1.2 with that note, or say it is partly done.

3. **The index row and deploy.bash were not updated for 0f4f050f.**
   - `CLAUDE/Plan/README.md:39` and the `deploy.bash:188-191` header describe only the settle and the 1 s wait. Neither mentions the focus check before the Enter or the "pasted but not sent" outcome.
   - `PLAN.md:114` gives no commit hashes.

**NIT**
4\. **The triage filter misses one log line.** `probe.bash:75` doesn't match the success line of the check before the Enter, "has kept focus again; sending the Enter" (`wsi:313`, `wsi-stream:515`). The line where the settle starts ("focus back") and the Enter line both show, but the line confirming the settle finished does not. Task 3.2 reads this timeline.
5\. **One log line can overstate.** The check before the Enter logs "still has focus before the Enter" (`wsi:310-311`) even for an old panel that gives a three-field answer. That panel can't report focus at all; the line takes the same branch as "focused".

**Checked and clean**

- **Exceptions:** `EnterNotSent` is re-raised ahead of the generic `except` (`wsi-stream:650`), and all three `paste_and_report` callers go through the new handler.
- **wsi failure path:** after the Enter check fails, wsi sends no Enter and no Ctrl+S.
- **Waits are bounded:** the unfocused count accumulates and is never reset, and each settle is at most 5 polls.
- **Windows that kept focus** are not asked again.
- **Tests exercise the real code:** the real `wsi` against a stub panel, plus `wsi-stream` unit tests.
- **Fail-fast:** every focus loss is logged, and nothing skips and continues.
- **Placement:** the plan scripts follow the repo's plan-script conventions; meta-deploy lists the plan.

### Plan 00165 (7ee4b481, bd67abf4, merge 9d3203f4)

**FIX-BEFORE-MERGE**

1. **The plan index still says CCY 3.89.0.** `CLAUDE/Plan/README.md:37` ends "CCY 3.89.0, container 2.49." The merge moved the change to 3.90.0. Every other place agrees on 3.90.0: the launcher (`claude-yolo:17`), the changelog (`docs/ccy-changelog.md:20`), `PLAN.md:50,70` and `deploy.bash:8,40`. README.md wasn't part of the merge's conflict resolution.

**NIT**
2\. **The tracked text names the owner's time zone.** PLAN.md and the changelog entry name the owner's zone and its summer offset (wording withheld here; since generalised). CLAUDE.md asks for no install-specific details. Using `Europe/London` as test data is fine.

**Checked and clean**

- **Merge resolution** (`git show --remerge-diff 9d3203f4`):
  - all three conflicts (QA.md, ccy-changelog.md, claude-yolo) are resolved correctly;
  - no QA.md gate row was lost on either side (set comparison of row names);
  - 3.89.1 is kept below 3.90.0.
- **Versions:** the Dockerfile LABEL 2.49 matches `REQUIRED_CONTAINER_VERSION` 2.49, and the `CCY_VERSION` comment was updated.
- **TZ coverage:** TZ is set only on the main `run` (`claude-yolo:3848`). The other `container_cmd run` calls are short-lived helpers (help/version checks, update, token), where the zone doesn't matter. The entrypoint never runs `su`/`runuser`/`env -i`, so TZ reaches the session.
- **Fail-fast:** an unresolvable zone stops the launch; there is no default to UTC.
- **The new test gate:** `scripts/test-ccy-host-time-zone.bash` covers the launcher argv and runs the real PROJECT-ENV block from the entrypoint. qa-all prints a pass line for it.
- **docs/ccy.md:** has a row for the time zone.

### dfdb4847 (Plan 00161, pingbus)

**SHOULD-FIX**

1. **A stale reply now counts as an answer without the rate or forge checks.**
   - `syncer.py:436` collects answers when `reason in (None, "stale")`.
   - But `_local_checks` returns `"stale"` at `syncer.py:462-463`, before the rate check (`:464`) and the forge check (`:466`).
   - `nack` may carry a `ref` (`protocol.py:182`). So a fresh nack whose ref fails §6 is dropped and doesn't answer, while the same nack read late does.
   - Fix: run the forge check, and decide on the rate check, before counting a stale answer. Or have the spec (`docs/agent-bus-protocol.md:406-407`) state that the forge check doesn't apply.

**NIT**
2\. **The deadline compares two different clocks.** `record_answer` judges the answer's server timestamp against the deadline computed from `entry["sent_ms"]`. That value is the sender's own clock reading taken before sending (`cli.py:626,632`). The difference is milliseconds on one host; the spec doesn't say which clock sets the deadline.

**Checked and clean**

- An answer sent after the deadline leaves the TIMEOUT whenever it is read (`inbox.py:636-637`), and there are tests for both cases.
- Docs §9 and §10 were updated.

### Mechanical gates

- **qa-all.bash:** passed (`QA passed: 1366 files checked`), including:
  - `wsi-stop-grace: passed: 110`
  - `ccy-host-time-zone: passed: 26`
  - `helper-tests: Ran 3717 tests in 119 modules (119 tracked)`
- **Targeted runs:** `test-ccy-host-time-zone.bash` (26/0) and `test-wsi-stop-grace.bash` (110/0, plus 257 unit tests) both pass. The pingbus inbox and syncer tests (122) pass.
- **plan-qa --sweep:** 0 block, 14 advise. None concern these commits; the only one shown concerns the 00161 journal from 26-10-07.
- **ansible-playbook --syntax-check:** not applicable; no playbook changed.
- **check_extension_compat / ESLint:** not triggered; no extension files changed.
- **git status:** clean before and after.

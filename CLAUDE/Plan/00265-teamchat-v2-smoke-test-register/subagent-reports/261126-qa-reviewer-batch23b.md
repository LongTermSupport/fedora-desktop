STOPPING BECAUSE: the review is finished. I could not write the report file: I have no Write or Edit tool, and my rules forbid writing files through Bash. The full report is below for you to save at `/workspace/CLAUDE/Plan/00265-teamchat-v2-smoke-test-register/subagent-reports/261126-qa-reviewer-batch23b.md`.

## QA Review: batch 23 rebuilt (`bf9ec6130..5fff6d691`)

The tree at `5fff6d691` is identical to the current HEAD `55c2f34e6` (`git diff --stat` between them is empty), so every check below ran against the reviewed tree.

**Verdict: BLOCKED.** Both earlier blockers are fixed. The new R79 guard change does not fail closed in a container, and the private-name check is blind to the name from blocker 1.

### The two earlier blockers

- **Blocker 2 (the `--help` seat rule) is fixed.** The new wording in `src-go/internal/ccylaunch/help.txt:53-55` and in the launcher's copy, "takes its only seat if free, adds one if none, else asks (Enter: lowest free, else a new one)", matches `src-go/internal/teams/chooser/teamchat.go:92-114`:
  - no seats: a seat is added;
  - exactly one seat and it is free: it is taken;
  - otherwise it asks, with the lowest free seat as the default, or the new-seat row when every seat is held.

  The non-teamchat path (`chooser.go:231-236`) still asks, and `docs/ccy.md` says so. `NewSeat` is wired at `internal/teams/app.go:201`, so "adds one" is true. The 3.124.81 changelog entry is accurate.
- **Blocker 1 (the team name in REGISTER) is fixed in REGISTER.** The batch removes 4 lines holding that name and adds none, and no commit message holds it. Across the tree it now appears in one file only (`git grep -i` on `5fff6d691`).

### Blocking

1. **The private-name guard does not see the name from blocker 1, so the "two known findings" figure is incomplete.**
   - `qa-private-names tree 5fff6d691` reports 2 findings, both term #19 (agent-bus team), at `CLAUDE/Plan/00265-teamchat-v2-smoke-test-register/JOURNAL/2026-11-25/134943Z-finding-cf58c8.md:15` and `:21`.
   - The blocker-1 name is on lines **15, 20 and 21** of that same file (counted with `grep -n -i`, the name not printed). Line 20 is never reported.
   - The masked context run showed the name unmasked: it is not a term at all. `<git dir>/private-terms-pinned` is absent in this checkout.
   - So the entry holds 5 leaked occurrences across two names, not 2, and the guard would pass a fresh leak of that name anywhere. This is exactly the gap R79 describes.
   - **Fix:** the owner pins that name in `.git/private-terms-pinned` (the route R79 itself names). Then re-run `tree` and expect 5 findings. The pending redaction of that journal entry must cover lines 15, 20 and 21.
2. **In a container, the guard passes without ever asking teamchat if `teamchat status` fails to start.**
   - `src-go/qa/privatenames/main.go:302-305` treats any `a.run` error as "client not found" and moves on (`continue`). It does not tell "not on PATH" apart from any other start failure, such as a timeout or a permission error.
   - `bus.session` then stays false, so the container check at `:220` never fires.
   - The other container check at `:208` (`len(fresh) == 0`) cannot fire in practice. `fresh` includes the host names, and `cmd/qa-private-names/main.go:58-65` always supplies `os.Hostname()`.
   - Result: a v2 session whose teamchat cannot start is judged only against host names and the stored list, and passes. This `continue` arrived with the teamchat client in this batch.
   - **Fix:** in a container, teamchat is in the image, so a start failure should be exit 2. Elsewhere, only `exec.ErrNotFound` should count as "not found". Base the container check on names learned from the bus or pinned names, not on `fresh`, which always holds host names. Add a test for teamchat failing to start in a container.

### Should fix

3. **The R76 row is stale.** `REGISTER.md:1404` says "Fix (batch 24 …)" and `:1415` says "waits on batch 24". The fix ships in this batch: `nolink.go`, plus "(R76)" in the 3.124.81 changelog. Restate it as batch 23.
4. **The R79 row is stale.** `REGISTER.md` (the R79 row, around 1490-1495) still says the sweep of "23 hits" is owed and the gate is red. The sweep is already in this tree: 2 hits remain, both in the journal entry. Update the row, and record that the blocker-1 name is not pinned (finding 1).

### Nits

5. **`PRIVATE_NAMES_CONTEXT=1` prints any private name that is not a term.** I used it to triage line 20, and the blocker-1 name came out unmasked in my transcript. It is not in any file. Context mode is only as safe as the term list, so the usage text should warn about this, or callers should not use it while the list is known to be incomplete.
6. **The R76 changelog line says "A stopped teamchat link that still holds its lock is reported busy."** The code reports busy when the link is running (`k.Link == LinkRunning`) and holds `link.lock` (`nolink.go:72`), not when it has stopped. Reword it.

### Checked and clean

- **The coalesce window** (`member/engine/coalesce.go`, `deliverloop.go:203-217`):
  - Nothing is lost. A held team's `looked` is dropped for that pass only, and the items stay in `h.store.Pending`.
  - Order is unchanged. Items are re-read in store order, and `plan()`'s sort is not touched.
  - A halt ping is not delayed. `dueAt` returns 0 as soon as a halt is among the deliverable items. `Verb` is set when the pending item is persisted (`storewrite.go:307`), so it is known before the content fetch.
  - Every hold is bounded by `first + 10 s`. Trailing facts that are due wait with the held items; facts not yet due keep their own time (`min(at, fd)`).
  - `coalesce_test.go` covers a burst, the agent ceiling, the human settle, the human ceiling and the halt.
- **R76 DownBusy:** it is reached only while `keeper.lock` is held, after the failed and restarting cases. If `link.lock` is free, the state stays "starting". The status-line text was added, and the tests pass.
- **Version bumps:**
  - `CCY_VERSION` goes 3.124.78 to 3.124.81, the Dockerfile LABEL and `REQUIRED_CONTAINER_VERSION` both read 2.84.90, and the changelog has entries for .79, .80 and .81.
  - Every in-repo package the three image commands import (`go list -deps`, 56 packages) is in `image-source-paths`, and the changed teamchat packages are all covered.
  - `kit/skill.txt` and the plugin's `SKILL.md` carry the same added paragraph.
  - `meta-deploy.list` queues `play-claude-yolo.yml` and `play-teamchat.yml`.
- **Public safety:** the batch's added lines hold no non-RFC IPs, home paths, emails or UUIDs. The 00268 command files use `<team>` placeholders.

### Mechanical gates

- **`qa-all.bash`:** not run. Sub-agents are barred from the full suite (R-SUBAGENT-FULL-QA).
- **Targeted tests:** `go test -race -count=1` passed on `member/engine`, `member/client`, `proto`, `member/deliver`, `teams/chooser`, `conversation`, `qa/privatenames` and `qa/cmd/qa-private-names`. `go test -count=1` passed on `ccylaunch`, `member/statusline` and `member/kit`. The `qa/rules` run did not report a result before this review ended; the first review had already passed it.
- **`qa-private-names tree 5fff6d691`:** exit 1, 2 findings (term #19). The guard does not see the blocker-1 name's 3 occurrences (finding 1).
- **`plan-qa --sweep`:** 3 blocking findings, all in plans this diff does not touch (00244, 00262, 00238).
- **Ansible syntax check, `qa-helper-tests`, ESLint:** not triggered; the diff changes no playbook, helper or extension.
- **The working tree has uncommitted edits by another agent** (`chat-client-info`, `play-claude-yolo.yml` and others). They are not part of this review.
## QA Review: Plan 00150, confirmation of 2b2a903e (full range `cf6bc90f~1..2b2a903e`)

**Verdict**: PASS WITH NITS

Both findings from the last review are fixed. I found no new defects in what the commit changed. Two small nits remain.

### Prior findings (261003-qa-reviewer-merge-opus.md)

| Finding | Status |
|---|---|
| 1. Plan index row 00062 still named `play-disk-reclaim.yml` | **Fixed.** `/workspace/CLAUDE/Plan/README.md:159` now names `play-cli-tools.yml` with tag `disk-reclaim`. |
| 2. Comment named the wrong check as the one that stops the run; imgpaste's WebP assert ran first | **Fixed.** The imgpaste block moved to lines 207-240 of `/workspace/playbooks/imports/optional/common/play-cli-tools.yml`, after compression. The header at lines 17-21 now says the compression block can only stop imgpaste and the WebP assert stops nothing. I checked the file in full and the comment matches the order. Open and reclaim depend on nothing outside the repo, and they come first. |
| Nit: PLAN.md:82 said "this plan's next commit" | **Fixed.** It now gives the hash `36e61be9`. |
| Nit: acceptance.bash header listed only two refusals | **Fixed.** Lines 9-11 and 40-41 now list all four. |
| Nit: the pixel reason check was loose | **Fixed.** Line 135 now checks `"9000x9000, limit is"`, and `imgpaste:46` prints `"input is ${width}x${height}, limit is … pixels"`, so they match. |
| Nit: 032 PLAN.md:254 "Next" line | **Fixed.** Line 255 now gives the new command: `play-cli-tools.yml --tags compression-helpers`. |
| Nit: `tasks/deploy-freeze-lib.yml:26` names the deleted play | **Not fixed** (see Nits). |

### Nits

1. **`/workspace/tasks/deploy-freeze-lib.yml:26` still quotes reasoning about "depending on play-open-command.yml having been run".** This is live task code, not a plan archive, and that file no longer exists. Add "(now `play-cli-tools.yml --tags open-command`)" after the quote.
2. **The summary lists the tools in a different order from the header.** `play-cli-tools.yml:248` puts imgpaste first. The header (lines 11-14) and the task order put it last. Cosmetic only, but lines 17-21 say the order matters, so the summary should follow it.

### Checked and clean

- **Leftover references to the deleted plays:** I grepped the whole repo, excluding `.git`, `untracked` and `node_modules`. The only hits outside the subagent reports and journals are:
  - historical text: ticked tasks, archived plans, research and review files;
  - the annotated lines in 00062 and 00064 plans;
  - the three `retired-plays.json` mappings;
  - `deploy-freeze-lib.yml` (nit 1).
- **IaC placement:** this is one play, at the same scope and with the same `become` as the three old ones. That is the right merge, not separation for its own sake. The baobab task is still gated with `when: provisioning_profile != 'server'` (line 117). The ncompress probe is still annotated `FAIL-FAST-OK`, and its result is checked at line 145.
- **Plan state:**
  - Task 2.2a is ticked, and its HOST items are left open.
  - Task 2.6 and its criteria are correctly left unticked until this review and `qa-all.bash` are clean.
  - The status is In Progress, which matches the unticked tasks.
- **Scripts:** `shellcheck -S warning` on `acceptance.bash` and `files/home/.local/bin/imgpaste` exits 0, and `bash -n` passes.
- **Not re-raised:** the `RUN_BASH_OPTIONAL_PLAYBOOKS` migration, the deploy and acceptance runs, and the Ctrl+V check. All are marked HOST in PLAN.md (lines 59, 60 and 62).

### Mechanical gates

- **qa-all.bash:** **this did not finish, so treat it as not run.** The toolchain check passed. Then the shellcheck step was killed: output `Terminated`, then `✗ bash: shellcheck invocation failed (rc=143)`, and the whole run exited 144. A shellcheck failure would not exit 143, so something outside the script killed it. I did not confirm what. Task 2.6 needs a full `qa-all.bash` pass that the coordinator runs before it is ticked.
- **plan-qa --sweep:** 0 blocking, 11 advisory. None are about 00150. One says Plan 032 has had no journal entry for 10 days, even though this range edited its PLAN.md.
- **syntax-check:** `ansible-playbook --syntax-check` on `play-cli-tools.yml` exits 0. The first attempt failed on non-blocking stdout; it passed once output went to a file.
- **qa-helper-tests, extension check, ESLint:** not triggered. Under `helpers/`, only the data file `retired-plays.json` changed; the last review ran the ledger tests (592 passed). Nothing under `extensions/` changed.
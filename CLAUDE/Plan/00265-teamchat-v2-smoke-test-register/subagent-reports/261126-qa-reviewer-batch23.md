I could not write the report to `/workspace/CLAUDE/Plan/00265-teamchat-v2-smoke-test-register/subagent-reports/261126-qa-reviewer-batch23.md`. I have no Write or Edit tool, and my rules forbid writing files through Bash. The full report is below for you to save.

## QA Review — batch 23 (`bf9ec6130..f1b1fe063`)

**Verdict**: BLOCK. The one blocking item is a one-line placeholder fix in a plan file; the code in this batch is sound.

### Blocking

1. **A real team name is added to a tracked file.** `CLAUDE/Plan/00265-teamchat-v2-smoke-test-register/REGISTER.md:99` adds `cc --teams 4@<team-B>` (the real team name, redacted in this copy). That identifies what runs on one install, which `CLAUDE.md` ("Public Repository") forbids. Replace it with `4@<team>`. The name was already tracked before this batch: 4 lines in `REGISTER.md` (one is `:423`) and 3 in `JOURNAL/2026-11-25/134943Z-finding-cf58c8.md`. Removing those from history is the owner's decision.

### Should fix

2. **The new `--help` text says the launcher takes a seat in cases where it actually asks.** The wording is "one free seat is taken, none adds one". It appears in `files/var/local/claude-yolo/claude-yolo:245-247`, `src-go/internal/ccylaunch/help.txt:53-55`, `docs/ccy.md:1142`, `docs/ccy-changelog.md:23` and the `CCY_VERSION` comment. The code in `src-go/internal/teams/chooser/teamchat.go:92-114` does this:

   - It takes a seat without asking only when the checkout has exactly one seat and that seat is free (`len(seats)==1 && anyFree`).
   - It adds a seat only when there are no seats at all.
   - With two or more seats and only one free, it still asks, with the free seat as the default.
   - When every seat is held, it asks too (re-attach a session or add a new seat; the default is a new seat). The help says "none adds one".

   Suggested wording: "its only seat is taken when free; with no seat, one is added; otherwise it asks, Enter taking the lowest free seat, or a new seat when all are held". The same drift is in `REGISTER.md:100-101`.

### Checked and clean

- **The new rule (`project_root_create_follows_link`):**
  - It catches the six forms it claims. The clean fixtures cover `O_EXCL`, opening after `ErrExist`, `os.OpenFile`, flags passed as a parameter, and test files.
  - The `qa/rules` tests pass, and `rules_test.go` and `table.go` now count 67 rules.
  - I ran the driver with `go vet -vettool` over both modules, untagged and with each of the four build tags: no findings, so the sweep fixes clear it.
  - Wrapped creates it cannot see (`homefs.Root.Create`, `guestroot`) use `openat` with `O_EXCL|O_NOFOLLOW`. They are safe, and the catalogue entry names them.
- **`seatrecord.go:207-222`:** the exclusive create, then reopening without `O_CREATE`, then the `SameFile` check is correct. If the lock is removed between the two opens, the launch refuses; it fails safe. The test with a dangling link passes.
- **`bashlimits/app.go:252-270`:** writes an exclusive temporary, sets its mode, then renames it over the allowlist. The temporary is removed on every error path, and the test with a linked allowlist passes.
- **`selftest.go:122-137`:** the exclusive create runs inside a `MkdirTemp` directory that `RemoveAll` cleans up, so nothing is left behind.
- **Version bump:** `CCY_VERSION` goes from 3.124.78 to 3.124.80, with changelog entries for both .79 and .80. `internal/conversation` is not in `image-source-paths`, so the container label does not need to move.
- **The two copies of the help text** match each other, and the five goldens pass (difftest ran 397 s under `-race`).
- **The README** has index rows for 00266 and 00267.
- **No other leaks:** the added paths are generic to the repository.

### Mechanical gates

- **`qa-all.bash`:** not run. Sub-agents are barred from running the full suite (R-SUBAGENT-FULL-QA); the coordinator runs it.
- **Targeted Go tests:** `go test -race -count=1` passed on `conversation`, `ccylaunch/...`, `cmd/claude-yolo`, `qa/rules`, `qa/bashlimits` and `qa/internal/semgrepreport`.
- **`plan-qa --sweep`:** 3 blocking findings, all in plans this diff does not touch (00244, 00262, 00238).
- **Ansible syntax check, `qa-helper-tests`, ESLint:** not triggered by this diff.

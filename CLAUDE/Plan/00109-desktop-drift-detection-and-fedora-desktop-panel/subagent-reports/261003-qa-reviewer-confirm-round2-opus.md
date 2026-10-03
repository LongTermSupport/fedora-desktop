## QA Review: Plan 00109, commit 12bd6a21

**Verdict: PASS WITH NITS.** All four should-fix items and the nit from the last pass are fixed. Check [19] is correct. One leftover wording problem, and it does not block.

### The previous findings
| # | Status | Evidence |
|---|---|---|
| SF1: the Task 3.2 human line asked about the wrong thing | Resolved | `acceptance.bash` (FOR THE HUMAN list) now asks for "a CLEAN login is silent". That matches the open item at PLAN.md:140. |
| SF2: the Task 4.5 human line was stale | Resolved | The line is deleted. PLAN.md:192 has 4.5 ticked, and the PLAN.md:263 list leaves it out. |
| SF3: nothing checked that the timer is armed | Resolved | Check [19] is at `acceptance.bash:928-940`. The 4.6 human line now points at it. |
| SF4: "since fixed" claimed too much | Resolved | PLAN.md:301-303 is reworded, and the criterion stays unticked until a further pass confirms. |
| Nit: docs said "after every play run" | Resolved | `docs/playbooks.md:891` now says "every play run the ledger records". |

### Check [19]
- **Registered:** `EXPECTED_CHECKS` goes up to 19 (`acceptance.bash:142`), and `check 19` records the run in `RAN_CHECKS`.
- **Follows the script's conventions:** it uses `check`, `ok` and `bad` with a remedy line, in the same shape as check [11] (`acceptance.bash:692`). `bad` writes to stderr and verdict output stays on stdout.
- **Fail-fast:** the capture sits inside `if !`, so `set -e` cannot abort the run. A failure is counted, never skipped.
- **Semantics are right:** "enabled" checks the wants-symlink and "active" checks that it is started, and both are needed. This matches what the play does (`play-host-health-login-report.yml:366-369`, `enabled: true` and `state: started`) and the template's `WantedBy=timers.target` (line 49). The remedy names the play that owns the timer.
- **Gates:** `shellcheck -x` is clean and `bash -n` is OK.

### Nits
1. **The playbook header has the same overclaim the docs nit fixed.** `playbooks/imports/optional/common/play-host-health-login-report.yml:25` still says the callback "restarts it after every play run". Change it to "every play run the ledger records" so it matches `docs/playbooks.md:891`.
2. **One failure message is too specific.** The second `bad` in check [19] always says "enabled without being started". If `is-active` returns `failed` rather than `inactive`, that diagnosis is wrong. The printed `'${timer_active}'` value does show the real state, so this costs little.

### Checked and clean
- The commit message matches the diff.
- PLAN.md:263-271 no longer gives a count.
- No new public-repo identifiers in the diff.
- The new confirmation report under `subagent-reports/` is filed where the plan expects it.

### Mechanical gates
- `shellcheck -x acceptance.bash`: clean. `bash -n`: OK.
- `plan-qa --sweep`: for 00109, only the three journal-ordering advisories already accepted (26-09-11, -14, -15). Nothing blocking.
- Not run, as instructed: `qa-all.bash` and anything Ansible (including `--syntax-check`; no playbook changed).

Files:
- `/workspace/CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/acceptance.bash`
- `/workspace/CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/PLAN.md`
- `/workspace/docs/playbooks.md`
- `/workspace/playbooks/imports/optional/common/play-host-health-login-report.yml`
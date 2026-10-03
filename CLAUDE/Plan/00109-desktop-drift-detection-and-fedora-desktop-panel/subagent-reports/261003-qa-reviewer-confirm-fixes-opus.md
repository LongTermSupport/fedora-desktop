## QA confirmation: Plan 00109, commit 57d53def

**Verdict: FIX-BEFORE-MERGE.** Should-fix 2 is only partly done. The FOR THE HUMAN list still has two lines that contradict the task tree, and the reviewer criterion's "since fixed" claim is too strong. Everything else is resolved.

### Prior findings
| # | Status | Evidence |
|---|---|---|
| SF1 docs say server-only and daily | **Resolved** | `docs/playbooks.md:880-894`: hourly on a desktop, daily on a server, post-play restart, `HEALTH-REFRESH-FAILED`. All of it matches `host-health-collect.timer.j2:7-44`, `health_refresh.py:31-86` and `callback_plugins/play_ledger.py:204-211`. No stale "daily" or server-only wording is left in 840-940. |
| SF2 FOR THE HUMAN contradicts PLAN | **Partial** | The 0.2, VM, CI, 4.3 and T5.4a lines were fixed. The T5.4a and 4.3 text matches PLAN.md:188-190 and :231-236. See the open items below. |
| SF3 / Minor 4 stale count, 4.6 deploy | **Resolved** | PLAN.md:19 has no count. PLAN.md:197-198 says "deployed (`6c88cdf0`) … HOST check is pending". |
| Minor 5 criterion inconsistent | **Resolved** | PLAN.md:296-302. `git status` is clean. |
| Minor 6 journal ordering | **Not resolved, but disclosed** | `JOURNAL/00109-Journal-26-10-03.md` (10:43 entry) accepts the advisory. `plan-qa --sweep` still reports ordering for 26-09-11, 26-09-14 and 26-09-15. This is an advisory, so accepting it is fine. |
| Nit 7 history in comments | **Resolved** | `store.py:110-112` and `check_freshness.py:291-293` now describe the current state. |
| Nit 8 hard-coded `DOC_SECTIONS` | **Deliberately kept, acceptable** | The comment at `acceptance.bash:131-133` says why: deriving the ids from the producer would make the check agree with itself. Check 3 only tests that these four are present, so the producer's fifth id (`self-update`) cannot cause a false failure. |

### Should fix
1. **The Task 3.2 human line names the wrong check.** `acceptance.bash:926` asks the human to confirm "that a notification actually APPEARS on screen at login". PLAN.md:138-139 already ticks that (owner, 2026-09-25). The open HOST item is PLAN.md:140, "a clean login is silent", and the list does not mention it. Fix: rewrite the line to ask for the silent-clean-login check.
2. **The Task 4.5 human line is stale.** `acceptance.bash:929` still asks for the icon to be seen in the top bar. PLAN.md:195-196 has that ✅, and the new PLAN.md:263 list (3.2, 4.2, 4.3, 4.6, T5.4a, 5.4) leaves 4.5 out. Fix: delete the line.
3. **The Task 4.6 human line drops "confirm the timer is armed"** (PLAN.md:203), and no check covers it. `grep host-health-collect acceptance.bash` finds nothing. The last review offered two fixes: add a check, or drop the "two scripts" claim. The commit did neither for this item. Whether the timer is armed can be checked by a script (`systemctl --user is-active host-health-collect.timer`), so it should be a numbered check, not a hand step.
4. **The PLAN.md:301 "since fixed" claim is too strong.** Items 1-3 and the accepted journal advisory mean not all the drift was fixed. Reword it once they are done.

### Nit
- `docs/playbooks.md:891` says "After every play run". The restart happens only for runs the ledger records (`play_ledger.py:189`, which skips it when the ledger is disabled or broken). "every recorded play run" would be exact.

### No new defects in
- The commit's doc text matches the timer template and the helper (above).
- The `acceptance.bash:251` abort message still holds after the reword.
- No new public-repo identifiers.

### Mechanical gates
- `shellcheck -x acceptance.bash`: clean. `bash -n`: OK.
- `plan-qa --sweep`: the only 00109 output is the three journal-ordering advisories, nothing blocking.
- Not run, as instructed: `qa-all.bash`, and anything Ansible.

Files: `/workspace/CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/acceptance.bash`, `/workspace/CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/PLAN.md`, `/workspace/docs/playbooks.md`
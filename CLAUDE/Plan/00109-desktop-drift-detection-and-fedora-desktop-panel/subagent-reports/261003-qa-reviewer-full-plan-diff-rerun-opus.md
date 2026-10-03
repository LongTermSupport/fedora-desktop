## QA Review: Plan 00109, full-plan diff re-run (branch F44)

**Verdict**: FIX-BEFORE-MERGE. There are no blocking issues. All eleven earlier findings are resolved or deliberately excluded, apart from finding 4, which has come back, and finding 9, which is disclosed but still flagged. The newest work (Task 4.6, `60a730bd`) brought new doc and plan drift. On this evidence the last success criterion should stay unticked until the drift below is fixed.

**I did not write the report file.** You asked for `/workspace/CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/subagent-reports/261003-qa-reviewer-full-plan-diff-rerun-opus.md`. I have no Write or Edit tool here, and my rules forbid writing files through Bash. Everything below is meant to be saved to that path as it stands.

### Earlier findings (from `260916-qa-reviewer-full-plan-diff-opus-5.md`)

| # | Status | Evidence |
|---|---|---|
| 1 BLOCK, pins compared nothing but read as clean | **Resolved** | Coverage is now counted after the loop (`helpers/version_pins/check_pins.py:417-421`). A zero or partial count produces an "unchecked" finding (`:425-445`). A host with no DKMS gets a stated "no tracked pin applies" sentence, not a fake `N of N` (`:229-231`). That outcome is the owner's decision of 26-09-24, recorded in `DESIGN-server-route.md:125-133`. The decision that the panel does not show coverage is written down (`helpers/host_health/status_document.py:197-205`), and `acceptance.bash:443-475` checks the numbers. |
| 2 "three checks" vs four | **Resolved** | The play header (`:3-5`), `docs/playbooks.md:851` and `sections/health.js:7,25` now say four and name LEDGER. The two remaining "three checks emit" phrases (`helpers/host_health/handoff.py:48`, `DESIGN-panel.md:66`) record a past measurement, not the current section count. |
| 3 `login_message.py` docstring | **Resolved** | It now says `scope: general` and describes both deliveries (`helpers/host_health/login_message.py:3-9`). |
| 4 stale counts | **Partly; it came back** | "17 tests" and "7 constants" are gone from PLAN.md, and `freshness.py:9-11` now says "dozens". But **PLAN.md:19 says "46 today outside `archived/`"**, and `find playbooks/imports/optional -name '*.yml' -not -path '*/archived/*'` returns **47**. Plan 00137 added one (`27f2cff8`, 2026-09-23). This is the same defect, under the same advice to drop the number. |
| 5 `extension.js` untestable | **Resolved** | `tests/extensions/gi-stubs.mjs:112,119` exports `Extension` and `Button`. `tests/extensions/test-panel-indicator.mjs` drives `enable()`. |
| 6 `clear_broken` writes an undated marker | **Resolved** | `at` is now required and an empty value raises (`helpers/play_ledger/store.py:101,116-117`). The caller passes `repo.utc_now()` (`check_freshness.py:318`). |
| 7 genesis timestamp written but never read | **Resolved** | The comment now says plainly that no consumer reads it yet (`store.py:31-35`). |
| 8 nothing detects that the detector was never run | **Excluded** | This is Decision 3, open and the owner's (`DECISIONS.md:32`). |
| 9 journal chronology | **Disclosed, not cleared** | `plan-qa --sweep` still reports the same out-of-order entries in `-26-09-11.md`, `-14.md` and `-15.md`. The fixes are hand-written entries stamped `## 23:59 · correction` (e.g. `JOURNAL/00109-Journal-26-09-11.md:338`) with no `--ref`. The sweep says a `mkplan.bash --journal … correction --ref HH:MM` entry would void them. So PLAN.md:297's claim that "every finding actionable from a container is resolved" is not literally true. |
| 10 `check-pinned-versions` pipeline | **Resolved** | Conversion and validation are now separate steps (`scripts/check-pinned-versions.bash:95-106`). |
| 11 QA.md gate counts | **Resolved** | No hand-maintained gate-count prose remains in `CLAUDE/QA.md`. |

### Should fix

1. **`docs/playbooks.md` still describes the collector as server-only and daily** (`docs/playbooks.md:880-889`, `:918-921`). Since `60a730bd`, `host-health-collect.service` and its timer deploy on both profiles. The timer is hourly on a desktop (`files/home/.config/systemd/user/host-health-collect.timer.j2:30-45`), and the play-ledger callback restarts the collector after every recorded play run (`callback_plugins/play_ledger.py:202-211`). The docs still say:
   - "desktop: a systemd --user unit at the end of a graphical login"
   - "The timer is **daily**"
   - nothing about the refresh after a play run, or the `HEALTH-REFRESH-FAILED` line it can print to stderr during any play run.

   Fix: give the desktop bullet the hourly timer and the post-play refresh, make the "daily" bullet server-only, and mention `HEALTH-REFRESH-FAILED`.

2. **`acceptance.bash`'s FOR THE HUMAN list contradicts PLAN.md.** PLAN.md:271-273 says these lines are what the owner must do by hand. Five are now false (`CLAUDE/Plan/00109-…/acceptance.bash`):
   - `:926` Task 0.2 "run ./triage.bash…": done and ticked (PLAN.md:82-93).
   - `:927` Task 3.2 VM scenario: passed 15 of 15 (PLAN.md:153-162).
   - `:932` T5.4a "OWNER'S CALL… not unwritten code": the owner chose option C and the code exists (PLAN.md:222-231).
   - `:933` Task 4.3 "the play/task runner is not written": wrong. `sections/plays.js` and `helpers/host_health/play_runner.py` exist.
   - `:937` "host-only checks skipping cleanly in CI": now verified. `gh run view 37040514044` shows success on `a142f2d7`, and its log has `⚠ deployed-drift: skipped (/home/runner/.local/bin does not exist)`.

   Also, PLAN.md:263 says "The HOST items in the task tree are now two scripts", yet nothing in acceptance.bash covers the open HOST items for Task 4.3 (PLAN.md:189), Task 4.6 (PLAN.md:204) or T5.4a (PLAN.md:232-237). They exist only as PLAN.md prose. Fix: rewrite the human lines to match the task tree, and either add a check for 4.6 (`host-health-collect.timer` enabled and active on a desktop) or remove the "two scripts" claim.

3. **PLAN.md:19 count is stale again** (see finding 4 above). Drop the number.

### Minor

4. **Task 4.6 still says "deploy and HOST check pending"** (PLAN.md:198), but the deploy happened. `60a730bd` is an ancestor of `085d78f0`, which queued `play-host-health-login-report.yml` in meta-deploy. `6c88cdf0` records the run as "failed=0" on 2026-10-02. Only the HOST check is still open; the plan should say so.
5. **The uncommitted PLAN.md edit leaves its own criterion inconsistent.** `git status` shows `M …/PLAN.md`, which ticks the CI criterion (`:291-294`). But the last criterion still says "Left open: the CI half above" (`:298`). Fix that line and commit the plan edit (Plan Commit Rule).
6. **Journal ordering** (finding 9): append `--ref` correction entries through `mkplan.bash --journal` so the sweep clears.

### Nits

7. Change-history narrative in comments: `helpers/play_ledger/store.py:110-114` ("it used to default to `""`… the sole caller duly wrote one") and `check_freshness.py:291`. Comments should describe current state; the history belongs in the JOURNAL.
8. `acceptance.bash:134` hard-codes `DOC_SECTIONS`, while the contract gate derives the section ids (it now prints 5, including `self-update`). This does no harm today because check 3 only tests that these four are present.

### Checked and clean

- **IaC placement (Task 4.6)**: the collector and timer go into the play that already owns the report (`play-host-health-login-report.yml`), with no new play. The removed "Remove The Server Collector" task is correct now that both profiles deploy it, and the cleanup that removes the desktop login unit on a server is kept (`:338-345`). The playbook keeps its exec bit (mode 100755).
- **Fail-fast**: `health_refresh.request` turns every failure into a named stderr line (`helpers/play_ledger/health_refresh.py:52-86`). This is the documented exception for callbacks, where Ansible swallows exceptions. A missing unit is skipped silently, with the reason recorded (`:79-80`). The refresh never touches the ledger's BROKEN sentinel and runs only after the records are written (`play_ledger.py:199-211`). No `failed_when: false` or `ignore_errors` was added.
- **Verification**: `test_health_refresh.py:103-108` ties `COLLECT_UNIT` to the play's `collect_service` var. The freshness change in `dbc01123` drops a removed play only when it was never tracked, and a git error makes the axis report itself untrustworthy rather than silent (`check_freshness.py:151-162`).
- **Self-update interaction**: the unattended cycle runs plays as the user with `XDG_RUNTIME_DIR` set (`helpers/self_update/cycle.py:574-583`), so the callback's `systemctl --user` has a bus to reach.
- **Public-repo safety**: changed plan, helper, extension, unit and playbook files contain no home paths, private IPs, personal emails or hostnames. `**Owner**: joseph` is the `mkplan.bash` convention, present in 59 plans.
- **Version bumps**: no `files/var/local/claude-yolo/**` path is in the 00109 commit set. Extension metadata covers Shell 45-50.
- **Plan index**: row present at `CLAUDE/Plan/README.md:85`.

### Mechanical gates

- **`qa-all.bash`**: not run, as you asked; the coordinator owns it.
- **`plan-qa --sweep`**: exit 1, 0 blocking and 11 advisories. Three of the advisories are 00109's journal ordering.
- **`ansible-playbook --syntax-check`**: not run, because Ansible must not run in the CCY container. `qa-all`'s `qa-ansible-syntax` covers it.
- **Targeted, all passing**:
  - The 8 directly touched helper test modules: `Ran 325 tests … OK`.
  - `scripts/test-panel-sections.bash`: `passed: 126`.
  - `check_panel_contract`: `PANEL-CONTRACT-OK 10 constant(s) … 5 section id(s)`.
  - `check_extension_compat`: 5 of 5.
  - `eslint .` in `extensions/`: exit 0.
  - `test-host-health-login-snippet.bash`: `passed: 16 failed: 0`.
  - `qa-version-pins.bash`: `COVERAGE: 9 of 9`.
# QA Review: feat/update-all-changed-plays (Plan 00214, #110)

Saved by the coordinating worker from the qa-reviewer agent's final reply (the agent is
read-only).

**Verdict: FIX-FIRST.** There was one finding: two changed deployed files had no queued run
to put them on the host. The rest is clean.

## FIX-FIRST

1. **The queue does not deploy what this branch changes on the host.**
   `CLAUDE/Plan/meta-deploy.list:170-179`.
   - Two deployed files change:
     - `files/home/.local/bin/fedora-desktop-health.j2:43-44` (the `--help` text), deployed
       by `playbooks/imports/optional/common/play-host-health-login-report.yml:324`.
     - `extensions/fedora-desktop@fedora-desktop/sections/plays.js:38-45` (the panel count),
       deployed by `play-fedora-desktop-panel.yml`.
   - Neither play is queued. The 00214 entry's comment told the owner to "paste the printed
     command to run every changed play, or add a changed-plays line here". That breaks
     `CLAUDE/AgentNotes.md:107` ("Never hand over a second command") and `:115`.
   - Until those plays run, the host's panel still counts with the old rule
     (`state !== 'fresh'`), so it disagrees with the report on `unresolved` plays.
   - **Fix:** add a `changed-plays` line below the 00214 entry, so the acceptance still runs
     first while plays are stale. Its description names the two plays. Drop the
     "paste … or add" sentence.

## NOTE

2. `subagent-reports/261022-review-round5.md` is untracked. Commit it with the Task 4.3 tick.

## Checked and clean

- **Placement:** no new play was added. The judgement now lives in one place
  (`changed_plays.judge`).
- **Deploy path:** the deployed `fedora-desktop-health` and the unit files run the
  checkout's own `helpers/` (`PYTHONPATH="$ROOT_DIR"`). So the new report and reminder
  text reaches the host without a play.
- **Production path:** `check-changed-plays.bash` runs the installed
  `fedora-desktop-health` and the real `login_report` and `login_message` modules. It
  compares the plays lists line by line with `diff`, and gives exit 2 when nothing could be
  judged.
- **meta-deploy.bash:** the `status` reset is correct. Words after `changed-plays` are
  refused. The ratchet allowlist was tightened to 161 code lines and 180 statements.
- **Version bumps:** none are needed (nothing under `files/var/local/claude-yolo/`).
- **Public-repo safety:** the only paths are `/home/me` placeholders.
- **Docs:** they match the new behaviour.
- **Gates:** `test-meta-deploy.bash` exit 0; pytest play_ledger, host_health and
  self_update: 986 passed, 1 skipped; `plan-qa --sweep`: no 00214 findings.

## Resolution

Fixed in the commit that saves this report. A `changed-plays` entry follows the 00214
acceptance, and its description names both plays and the logout the panel needs.

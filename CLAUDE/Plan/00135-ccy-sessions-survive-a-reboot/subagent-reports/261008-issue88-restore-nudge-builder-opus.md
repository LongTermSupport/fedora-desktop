# Plan 00135 Phase 8 (fedora-desktop#88): restored sessions set going, verify waits for it

Builder report for Tasks 8.1 to 8.4. Task 8.5 is the host check and is still open.

## What was built

- **8.1 design.** The design is the 26-10-08 decision entry in this plan's journal. The
  points that matter:

  - **The prompt.** Claude Code's prompt counts as drawn when the pane shows its input box: a
    line beginning `❯` with a rule of `─` above it and another below. This was measured by
    running Claude Code 2.1.293 in a pty and stripping the escapes. The selection dialogs use
    `❯` as a cursor, but they have no rule lines, so they do not count.
  - **Busy.** The input box plus `esc to interrupt` on the screen means a turn is running.
  - **Context size.** It comes from the transcript, not the pane. The value is the last
    main-thread assistant `usage` (input + cache_creation + cache_read), or the `postTokens`
    of a later `compact_boundary`. `<synthetic>` messages and sub-agent entries are skipped.
  - **The floor.** `ccy_restore_compact_floor_tokens`, 150000 by default.

- **8.2 `ccy-sessions set-going`.** It runs in a new user unit,
  `ccy-sessions-set-going.service`. That unit is `Type=exec`, ordered after the restore and
  pulled in by the restore's `Wants=`. It is not part of the restore itself, for this reason:

  - The restore is a oneshot, and `default.target` waits for it.
  - The user manager's start, `user@UID.service` (90 s default start timeout) and a desktop
    login all wait for `default.target`.
  - Waiting for prompts inside the restore would hold all three.

  How it runs:

  - It handles only the sessions this restore started. The manifest is now format 2: each
    entry adds `going`, `at`, `detail` and `transcript`. A session the restore found already
    running is `none` and is never typed into.
  - It sends keys the way the supervisor's `_perform_injection` does: `send-keys -l <text>`,
    a 0.2 s pause, then `Enter` on its own. Nothing else in the repo typed into a pane, so
    there was no existing pattern to follow.
  - Each session that is left alone is named on stderr with a reason code, and the other
    sessions carry on. The reasons are: not running, launcher exited, waiting at a prompt or
    prompt not drawn past the wait, busy, no transcript, context unreadable, send failed, no
    floor, session list unreadable.
  - The exit status is 1 if any session was left alone.

- **8.3 `verify-restore`.** Two new states:

  - `SETTING-GOING`: the session is pending, or keys were typed and the start window has not
    passed yet.
  - `NOT-SET-GOING <reason>`: the session was left alone, or it shows `compact-not-started` or
    `continue-not-started` because its transcript has no main-thread, non-meta user entry at
    or after the send time within `CCY_SESSIONS_START_WINDOW` (120 s).

  The test for "took the input" is the transcript, because submitted input is written there
  at once, while a line left in the input box writes nothing.

  `--wait` now stops as soon as the restore has settled: no session is STARTING or
  SETTING-GOING, and the same report has come back twice in a row.

- **8.4 self-update verify.** `VERIFY_WAIT_SECONDS` goes from 300 to 1500. That is a ceiling:
  verify returns as soon as the restore settles. A test reads the set-going wait and start
  window from the tool and `TimeoutStartSec` from the unit, so a change that breaks their
  order fails. A pass runs `systemctl reset-failed fedora-desktop-self-update-verify.service`,
  the unit the login banner lists from `systemctl --failed`. If that command fails, the result
  is still `deployed` with exit 0, but the failure is written to stderr and to the result's
  detail. The exit codes are unchanged.

- **IaC.** `play-claude-yolo.yml` resolves the floor with
  `ccy_restore_compact_floor_tokens | default(150000) | int`, under a different name so it
  does not default itself. It asserts the floor is above 0, then templates the new unit with
  it. The restore unit gains `Wants=`. The existing user daemon-reload task covers both
  units.

- **Versions, docs and the plan.**

  - CCY_VERSION is 3.87.0. The container version is unchanged.
  - Changelog entry added. `docs/ccy.md` (Sessions Survive a Reboot) and
    `docs/configuration.md` (self-update, after the reboot) are updated.
  - PLAN.md Tasks 8.1 to 8.4 are marked done.
  - `meta-deploy.bash` PLANS now includes `playbooks/imports/play-claude-yolo.yml`.

## Files

- `files/home/.local/bin/ccy-sessions`
- `files/var/local/claude-yolo/lib/session-registry.bash`
- `files/var/local/claude-yolo/claude-yolo` (version line only)
- `files/home/.config/systemd/user/ccy-sessions-set-going.service.j2` (new)
- `files/home/.config/systemd/user/ccy-sessions-restore.service`
- `playbooks/imports/play-claude-yolo.yml`
- `helpers/self_update/cycle.py`
- `tests/helpers/self_update/test_cycle.py`
- `scripts/test-ccy-session-registry.bash`, `scripts/test-ccy-sessions-reboot.bash`,
  `scripts/test-self-update-cycle.bash`
- `docs/ccy.md`, `docs/configuration.md`, `docs/ccy-changelog.md`
- `CLAUDE/Plan/00135-ccy-sessions-survive-a-reboot/PLAN.md`, its JOURNAL 26-10-08,
  `CLAUDE/Plan/meta-deploy.bash`

## Verification run here

- `scripts/test-ccy-session-registry.bash`: green, and green again under `LC_ALL=C`. The
  rule-line test was changed from a regex repeat to a literal prefix because of C-locale
  byte matching.
- `scripts/test-ccy-sessions-reboot.bash`: green. The new set-going section uses the real
  tool and library with the fake tmux, which now also handles `send-keys`.
- Also green: `scripts/test-ccy-restore-askpass.bash`, `test-ccy-sessions-take-over.bash`
  and `test-ccy-session-network.bash`.
- `python3 -m unittest tests.helpers.self_update.test_cycle` and
  `scripts/test-self-update-cycle.bash`: green.
- `scripts/qa-docs.bash`: green.
- shellcheck is clean on every changed bash file.
- ruff check is clean. `ruff format --check` flags `cycle.py`, but that file was already
  unformatted before this work.
- `ansible-playbook --syntax-check` on the play passes when given a dummy vault file (the
  worktree has no vault password).
- `qa-all.bash` was not run; that is for the coordinator.

## Open points for the owner

01. **Nothing continues a session after its compaction unless it has a supervisor.** That
    covers cc sessions and ccy sessions run with `--no-supervise`. They are compacted and then
    sit idle. set-going could wait for the compaction to finish and then type `continue` for
    those sessions. That was not built because the issue names the supervisor as the one that
    carries the session on.
02. **Two restored sessions in one project share a transcript directory.** Both `--continue`
    the same newest conversation, which was already true before this work. set-going reads
    that one transcript for both, and verify can mistake one session's input for the other's.
03. **The floor value.** 150000 is the supervisor's red line for a 200k-window model.
    Sessions on a 1M window are compacted earlier than the supervisor would compact them. This
    is the owner's call to tune.
04. **The floor is not written back to host_vars.** AnsibleStyle has a pattern for
    persisting per-host options into host_vars; it was not applied. This matches
    `ccy_restore_sessions`, which also uses `| default` at the point of use.
05. **A by-hand re-run after a failed verify does not re-check.** A failed verify still
    clears `owed-verify` (the Plan 00137 contract), so running verify again says nothing is
    owed. `reset-failed` therefore helps in two cases only: an attempt the unit killed (where
    the owed record was never cleared) re-run with `systemctl start` or sudo, and any pass
    that follows an earlier failed state in the same boot. Making a failed verify
    re-checkable would change the 00137 contract.
06. **The TUI markers are tied to the Claude Code version.** `❯` framed by `─` rules, and
    `esc to interrupt`, were measured on 2.1.293. If a TUI change breaks them, the result is
    loud, not silent: sessions are named `prompt-not-drawn-after-1200s`.
07. **The cc transcript directory encoding is partly assumed.** The rule "every
    non-alphanumeric character becomes `-`" was checked only against existing directory
    names that contain `/`. Other characters are assumed. If the guess is wrong, the session
    is named `no-transcript`, not skipped silently.
08. **`busy-before-set-going` counts as not OK in verify.** A session that the supervisor or
    queued work started before set-going reached it makes verify fail. That is honest, since
    it was never compacted first, but it may be noisy.
09. **The set-going unit can show as failed.** When any session is left alone, the user unit
    ends failed and appears in `systemctl --user --failed`. It clears at the next boot.
10. **Deploying without a reboot breaks `verify-restore` until the next restore.** On a
    machine where the play is deployed but not rebooted, the manifest on disk is format 1.
    `verify-restore` refuses it, by name, until the next restore writes format 2.
11. **Merge.** Expect a CCY_VERSION line conflict with the concurrent ssh-handling work.
    This branch sets 3.87.0.

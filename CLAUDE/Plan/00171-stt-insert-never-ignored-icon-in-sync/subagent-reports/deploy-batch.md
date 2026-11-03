# Meta-deploy batch: plans waiting only on a host run (2026-10-13)

Read-only survey. Sources: each candidate's PLAN.md open tasks, newest JOURNAL day-file,
its scripts, and the `untracked/plan-runs/_meta-deploy/*/summary.txt` history. Nothing was
run. 00161, 00177 and 00178 left to the coordinator.

## How meta-deploy behaves on a long list (from the script)

- Unattended: no confirmation prompt; it exports `PLAN_ASSUME_YES=1` and
  `RUN_BASH_OFFER_ISSUE=0`. The one thing at the start that needs the owner is a sudo
  password: plan deploy scripts call `plan_prime_sudo` before their log starts.
- A failing unit does NOT stop the rest. Each unit's status goes into a `BAD` count and
  the loop goes on; the exit status is 1 if anything failed. Within a plan folder entry, a
  failed deploy.bash skips that plan's acceptance (counted bad). Acceptance exit 2 is
  reported `UNSET`, also counted bad.
- The whole list is checked BEFORE anything runs: a missing plan folder, or a script
  entry that is not executable, is FATAL with nothing executed.
- A folder entry runs triage (before), deploy, triage (after), acceptance. A
  `<plan>/<script>.bash` entry runs just that one script. A `playbooks/...yml` entry runs
  that play through run.bash.
- Verdict: `untracked/plan-runs/_meta-deploy/<newest>/summary.txt`.

## 1. The PLANS array (round 1)

Most of the survey's candidates have nothing left for a script to do: their deploys have
already landed, and what remains is the owner at the screen, a reboot, another machine, or
an owner decision. Section 2 gives the reason for each. Four units remain:

```bash
PLANS=(
    # stt: runs play-speech-to-text.yml once. It carries the built code of 00171, 00164, 00156 and 00148. Asks for sudo at the start; ends by saying "log out, then the Insert replay"
    "00171-stt-insert-never-ignored-icon-in-sync/deploy.bash"
    # stt: read-only gate, run right after that deploy. Deployed recorders and extension files match the checkout, settings parse, resolver answers, login unit enabled. Names the logout and the microphone checks as not established
    "00148-stt-unlimited-dictation-loop-and-buffer/acceptance.bash"
    # ccy child-claude: read-only host triage (Task 6.4 steps 1 and 4). H4 checks that the built image carries the required version; play-claude-yolo.yml already passed today, so no deploy is owed
    "00092-ccy-child-claude-spawn-mode/triage.bash"
    # startup-log triage, read-only: vmtest-nightly's last Result (Task 2.7's last check), SELinux relabel state, and the rest of the boot-log legs
    "00134-startup-log-triage-and-status-panel-unavailable/triage.bash"
)
```

Order and conflicts:

- 00171, 00164, 00156 and 00148 all deploy `play-speech-to-text.yml`. Listing their
  folders would run the same play up to three times. 00156 has no scripts of its own: its
  Task 3.2 says 00148's deploy carries it. So the play runs once, through 00171's
  deploy.bash.
- 00171's deploy changes the GNOME extension, which needs a logout. Its `acceptance.bash`
  needs the logout AND the owner's Insert replay first. That is why the plan's own Task 4.1
  splits it into two rounds, and why round 1 lists its deploy.bash only. A folder entry
  would run acceptance before the logout and report UNSET or FAIL.
- 00148's acceptance.bash checks files and settings, so it can run before the logout. Its
  header names the logout as something it cannot establish.
- 00092 must NOT be a folder entry. Its acceptance.bash calls `plan_require_container` and
  refuses on the host, so a folder entry would FAIL. Only its host triage belongs here.
- The two triage scripts are read-only and go last, as the rule says.

### Round 2: add after the owner has logged out and done the dictations (section 3)

```bash
PLANS=(
    # stt: judges the Insert replay from the panel's debug log (session tokens, BLOCKED flashes, the next dictation starts and stops on fresh presses)
    "00171-stt-insert-never-ignored-icon-in-sync/acceptance.bash"
    # stt: read-only. For the focus-loss dictations, "has focus back", the re-ask "before the Enter" and the Enter lines, in that order (Task 3.2)
    "00164-stt-enter-lost-after-focus-retake/triage.bash"
)
```

## 2. Excluded, and why

| Plan                                                          | Why it is not in the list                                                                                                                                                                                                                                                                                                               |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 00164                                                         | Same play as 00171, and its code already deployed (meta-deploy 2026-10-09, twice). What remains is owner dictations, then triage. That triage is in round 2.                                                                                                                                                                            |
| 00156                                                         | No scripts. Covered by the stt deploy (its Task 3.2). Left: Task 3.3, an owner check (log out, Settings, Insert in server mode). Tasks 4.1, 4.2 and 4.5 are owner or agent items, not host runs.                                                                                                                                        |
| 00148                                                         | Only its acceptance is in the list; its deploy is the same play. Host Tasks 0.5, 6.2, 8.4 and 9.5 are owner dictation checks. Phases 10-12 are not built and wait on owner decisions 11.1-11.4, 12.1 and 12.2.                                                                                                                          |
| 00153                                                         | No host run of its own. Phase 4 is the owner asking for the `44.0.0` tag once this round's acceptance is in. Agents never tag.                                                                                                                                                                                                          |
| 00048                                                         | No scripts. Its `cc` wrapper is deployed by `play-claude-yolo.yml`, which passed today. Left: Tasks 4.3, 4.6 and 4.7, interactive `cc` checks by the owner (empty pool, expired-only pool, picking Desktop, the ccy chooser).                                                                                                           |
| 00139                                                         | Phases 1-5 are deployed and accepted (14 of 14). Phase 6 is not built: the owner is holding it, and decision 6.4 is open. The Task 3.1 item is on a server (another machine).                                                                                                                                                           |
| 00137                                                         | Everything left is on the server: trusted keys in `self_update_signing_public_keys`, one alert post, and one full cycle. It needs a `44.0.0` tag (00153) or the branch channel. That is another machine, through the infra agent.                                                                                                       |
| 00135                                                         | No scripts. `play-claude-yolo.yml` (CCY 3.89.1 or later) is already deployed. Left: reboot tests 5.1-5.7 and 9.4, server checks 6.3, 7.4 and 8.5, owner PR merge 9.5, and the container-side tmux test plus patch for Task 7.1.                                                                                                         |
| 00141                                                         | Both plays ran (meta-deploy 2026-10-02). Left: the owner logs out, opens the panel's "Re-run a play…" row and picks a play.                                                                                                                                                                                                             |
| 00144                                                         | Deploy and acceptance passed 8 of 8, also after a reboot. Left: the visual pass (one icon, an injected finding, copy a hint). Task 7.5 waits on every host running the play.                                                                                                                                                            |
| 00136                                                         | Both plays ran (2026-10-02). Left: owner checks in a terminal, on the panel row and over SSH.                                                                                                                                                                                                                                           |
| 00109                                                         | Acceptance passed 22 of 22. Left: eyes-only and docked/undocked checks (Tasks 3.2, 4.2, 4.3, 4.6, T5.4a). One needs the upstream black-background bug to happen.                                                                                                                                                                        |
| 00158                                                         | Desktop deploy passed. What is left is on the VM (infra agent).                                                                                                                                                                                                                                                                         |
| 00165                                                         | Deployed. In a fresh session `TZ` and `date` give the host's zone (checked in this container). Left: the owner compares the status line with the desktop clock. Close candidate.                                                                                                                                                        |
| 00160                                                         | Deployed (2026-10-06, image rebuilt). Left: the owner's `ccy.env.local` check that deploy.bash prints.                                                                                                                                                                                                                                  |
| 00173                                                         | Deployed. Left: the owner's evening `shutdown-with-update` test, and the failed-dnf decision.                                                                                                                                                                                                                                           |
| 00055, 00062, 00064, 00054, 00074, 00061, 00073, 00039, 00058 | Dormant by the owner's choice. What is left is a visual pass (00055), live TUI or `open` checks (00062, 00064), a 443 network check (00054), a run on another or fresh box (00074, 00061, 00073), a manual test matrix (00039), or agent work first (00058's darktable bump, 00062 Phase 5). Not pending until the owner un-parks them. |

## 3. What the owner must do

At the start of round 1:

- Be there for the sudo password prompt (00171's deploy.bash primes sudo).

After round 1, before round 2:

1. Read the summary. Expect PASS on all four units. If 00092's triage H4 is red, the image
   needs a `ccy --rebuild`; that is the owner running ccy, not a meta unit.
2. **Do not dictate between the deploy and the logout** (00171: an old panel with new
   recorders is untested). Log out and back in.
3. 00171: turn on Debug Logging (panel menu), then do the replay deploy.bash prints.
   Dictate and press Insert to stop. Press Insert again in the grace, and again while it
   transcribes: each press should flash a red no-entry sign and start nothing. When the icon
   is ready, press Insert, speak, then press Insert once more.
4. 00164: with Debug Logging still on, dictate into a terminal and into a GUI app. Click
   another window before stopping, and once switch workspace. Do it in both batch and
   streaming mode. The text should paste into the first window and be sent.
5. 00148 host checks, at the owner's pace: 0.5 (Insert on the last word, in each mode,
   plus a pre-buffer cold start), 9.5 (more than 120 s into GNOME Text Editor, with chunks
   on and then off, switching windows), 8.4 (keep-warm: 30 idle minutes, then Insert),
   6.2 (more than five minutes with a forced segment failure).
6. 00156 Task 3.3: open Settings, and press Insert in server mode with no model, then
   with one.
7. Tell the agent. The agent puts the round 2 list in and asks for the meta-deploy run.

Outside meta-deploy (owner only, any time):

- Screen checks: 00141 (panel re-run row), 00144 (visual pass), 00136 (terminal report and
  panel row), 00109 (icon clears without a login; docked unlock), 00048 (`cc` empty and
  expired pool, picking Desktop, the ccy chooser), 00160 (`ccy.env.local`), 00165 (status
  line clock).
- A reboot: 00135, Tasks 5.1-5.7 and 9.4 (sessions survive a reboot, compose stack
  restored).
- An evening: 00173 (`shutdown-with-update` with a ccy session at its prompt).
- 00092: rotate the OAuth token (its I1 gate is a true red until then). Then the agent runs
  `acceptance.bash` inside a ccy session started with `CCY_CHILD_CLAUDE=1`, and again in a
  later session with the flag removed.
- Decisions: 00153 (ask for the `44.0.0` tag once this round is accepted), 00148
  (11.1-11.4, 12.1, 12.2), 00139 (6.4, and whether to resume Phase 6), 00173 (failed-dnf).
- Other machines, through the infra agent: 00137 and 00139 server keys, plus one full
  cycle; 00135 server restore checks; 00158 on the VM.

# The host run at fe894347c: results recorded, Task 2.31 built

Branch `fix/00201-host-run-fe89`, cut from F44 7463f6579. Source: the host agent's report on
its run at F44 fe894347c.

## Part A: results recorded

- **Plan 00177 Task 9.9**: PASS on the host, marked done. lxc-add without gh stopped "declared
  but not joined" (by design), `declare lxc-remove` exited 0, and `ccy-teams remove` found
  nothing to park and removed the team.
- **Plan 00201 Task 2.28**: PASS (steps 03, 10 and 13 on chooser-teams-up's teams), marked
  done. **Task 2.29**: PASS (step 12 exit 0, the ended cc seat "nothing to shut down", seats
  free, lxc-add's Stop exit 0), marked done. **Task 2.14** is marked done too: its only open
  item was check 12, which now passes. Status, Task 2.7's host line and the Success Criteria's
  "proven by" line updated. ACCEPTANCE-owner steps 03 and 10 now rest on `ActiveEnterTimestamp`.
- **Plan 00194**: success criterion step 5, the Task 2.0e lifecycle-restart baseline, recorded
  under the step. The team sees `left the team: session ended (exit)`, which cannot be told
  apart from a real `/exit`. The return is silent: one `agent_bus.status` event, the same
  session id resumed. The criterion is marked done, since steps 1-4 passed earlier. Task 2.0e's
  text points at the baseline.
- A journal entry for each plan (`mkplan.bash --journal`, category finding).

## Part B: Task 2.31

1. **Fixture allow_from.** `m7_install_teams` (shared by acceptance.bash and
   chooser-teams-up.bash) installs each team from `m7_check.py rendered-team`. That command
   runs `helpers/agent_bus/bridge.py`'s `apply` on the live bridge and adds `state: present`,
   which is what the play's "Add the LXC bridge's subnet to every team's allow_from" and
   "Render each team file" produce. A test proves its config digest equals
   `declare.declared_config` for the host_vars entry (the board's "up to date" check). The
   host_vars keep the declared list, as an owner's would.
2. **Step 03 evidence.** Steps 03 and 10 now use the timestamp; the doc says why the play's
   output cannot show a restart.
3. **Shutdown and background work.** Claude Code 2.1.311 is installed in this container, so
   its bundle gave the dialog's exact text: the title "Background work is running", the
   subtitle "The following will stop when you exit:", and the options "Exit and stop tasks",
   "Move to background and exit" (when offered) and "Stay". Esc means Stay.
   `screen.AtBackgroundWorkExit` recognises the dialog. Shutdown presses Enter only while the
   cursor is on "Exit and stop tasks", once, and then waits again (SPEC-shutdown SD10a). With
   the cursor anywhere else, the "stopped" line names the question.
   **Open**: the `❯` arrow on the focused option is inferred from Claude Code's other selection
   dialogs (screen.go relies on it too); nobody has seen it on this dialog. Host check: step 12
   with a background task running in one session.
4. **CCY_TEAMS_HOST_VARS at the tmux hand-off.** Something in the session does read it: the
   launch inside the session runs `ccy-teams describe-seats --block`, and `Choose.descriptions`
   reads the host_vars through declare.py. A harness's teams therefore lost their descriptions
   in the launch block. membership-init, record-seats, claim-container-name and restart-args
   read no host_vars. The fix is in `lib/tmux-session.bash`: a second `env` prefix on the
   pane's command, on the same line as the token hand-off, so the ratchet record is not
   exceeded. It is tested in `scripts/test-ccy-token-handoff.bash`, whose fake tmux now drops
   the variable from the pane's environment, as a running server would. CCY 3.100.34 with a
   changelog entry; the container is unchanged.
5. **Board KIND.** This was a display bug. Seat rows are `KindCcy` for every flow, and
   shutdown, restart and the seat menus all key on that. The KIND column now shows the type in
   the handle (`host` for a cc seat), and the flows are untouched.
6. **Settings filter.** Every picker passes `--no-sort`, so filtered rows keep the list's order.
   The new `ui.PickSpec.RankMatches` drops that flag, and the settings screen sets it.

## Host checks (Task 2.31, queued in meta-deploy)

After `changed-plays` (play-agent-bus.yml and play-agent-bus-element.yml for Task 2.30's assert
text, and play-claude-yolo.yml for CCY 3.100.34 and ccy-teams):

- Step 12 with a background task running in one session: it should read "exited cleanly". A
  `stopped` line that names the question means the arrow is drawn some other way.
- `chooser-teams-up.bash`, then one play on a chooser team: its `ActiveEnterTimestamp` should be
  unchanged, and the board should read "up to date" before the play.

# T3.6 review fixes (Task 3.7)

These are fixes for the qa-reviewer findings on commit 4d808e9f. The same findings are
in the day's JOURNAL entry; this file keeps the four-case table and says what is tested
and what only a host can show.

## Why the kind follows the restore opt-in

`_render_operator_message` in the daemon has two fixed texts. It is daemon-owned and not
edited here.

| Kind               | What the agent is told                                               |
| ------------------ | -------------------------------------------------------------------- |
| `reboot-warning`   | "will reboot… a session restore will follow"                         |
| `shutdown-warning` | "will shut down and NO session restore will follow… leave a handoff" |

What the agent does next is decided by whether restore follows. Before this change the
kind came from the command name:

| Command  | Restore on                                       | Restore off                       |
| -------- | ------------------------------------------------ | --------------------------------- |
| reboot   | right                                            | told a restore follows; none does |
| shutdown | told to hand off for good; then restored at boot | right                             |

After it, the kind comes from the opt-in: `going_down_kind` in `ccy-sessions` checks the
`default.target.wants` symlink. The action is right in all four cases. The verb is wrong
in two: "will reboot" for a shutdown with restore on, and "will shut down" for a reboot
with restore off. Fixing the verb needs a daemon kind that is independent of the action.
It has not been filed.

## Tested

All in `scripts/test-ccy-sessions-reboot.bash`, which runs the real scripts under fakes:

- `notify going-down` in both opt-in states, `--dry-run`, and the usage errors.
- `reboot --in 3` with restore off: both warnings are `shutdown-warning`.
- `shutdown-with-update` for real under stubs for sudo, getent, dnf, fwupdmgr, flatpak,
  shutdown, systemd-inhibit and systemctl:
  - success in both opt-in states;
  - blocked and answered N: `reboot-cancelled` is sent and the script exits 1;
  - blocked with no terminal: `reboot-cancelled` is sent and the exit is non-zero;
  - blocked and answered Y: forced poweroff, nothing withdrawn;
  - `SUDO_USER=root`: refused before `dnf` runs.
- `reboot-with-update` with restore off: `shutdown-warning`, then `systemctl reboot`.

## HOST-only

- Task 5.7: the text a real session receives, in both opt-in states.
- A real `shutdown -h now` that is refused by inhibitors.
- A forced poweroff that fails.

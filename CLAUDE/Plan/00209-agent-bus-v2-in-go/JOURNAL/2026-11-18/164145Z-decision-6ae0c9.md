## 16:41 · decision · —

`team remove --purge` failed on a guest at step `purge`. `systemctl clean --what=state teamchat-server@<team>.service` was refused with "bad unit file setting".

The cause: the template unit has no `ExecStart=`, which the team's drop-in supplies, and the remove's `files` step deletes that drop-in before purgeState runs. Reordering the steps is not enough. n2d-H16 supports a purge after an earlier plain remove, and by then the drop-in is long gone.

Decision: the purge removes the homeserver's state itself. The `StateDirectory=` link under /var/lib/teamchat/ is removed as a link, never followed, or removed whole if it is a real directory. The private directory under /var/lib/private/teamchat/ is removed after it. systemd is not asked.

`Systemd.CleanState` and `units.Manager.cleanState` then had no caller and are removed. `internal/teamchat/units` is no longer an owner of `systemctl` in src-go/qa/rules/table.go.

The fake systemd in host's tests had answered `clean` regardless of the drop-in, so no test could see the defect. The new tests:

- hold that nothing asks systemd after the drop-ins are gone;
- cover a purge after a plain remove with state, a link elsewhere (not followed), a real directory, and an unreadable state root.

SPEC-n2d HS14 and n2d-H16, and SPEC-n2f SD6 and n2f-D4, are updated.

The guest's leftovers were removed by hand, using the purge's own steps, read from the source for the coordinator.

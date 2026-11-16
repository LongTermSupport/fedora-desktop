# Agent team bus v2: what a backup holds

What `teamchat-admin backup` keeps of a v2 team, where each part is, and what is not in it. The
design is [DESIGN-v2-C-host.md](../CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md)
section 3.12.1; this page says what to expect.

## Two halves, taken together

The daily `teamchat-backup.timer`, or `teamchat-admin backup --team T`, takes backup `N` of a
team as two halves under one team lock, so no admin change falls between them:

- **The admin half**, `/var/lib/teamchat-admin/<team>/backups/<N>.tar`, root-owned, mode 0600.
  It holds, in order:
  - `backup.json`, the manifest: its version, when the backup was taken, the server name;
  - `team.json`, the team as it was applied;
  - `config.toml`, the homeserver's configuration;
  - `roster.json`, the team's roster;
  - `rooms.json`, the team's room ids;
  - `admin.token`, the control account's token;
  - `ca.crt` and `ca.key`, the team's own CA, from manifest version 2 on, so a restore keeps
    every issued bundle valid. The TLS leaf is not kept: the next `team apply` makes it again.
- **The homeserver's half**, Tuwunel's own database backup `N`, under
  `/var/lib/private/teamchat/<team>/backups/` (`meta/N` and its data). The tar holds no
  database, and the database backup holds no admin state.

A backup is usable only while both halves are there. Tuwunel keeps seven database backups, and
each backup run deletes the tars whose database backup Tuwunel has pruned.

## What is in neither half

- **Seat files.** A member's credential bundle and a pool seat's `seat.json` live with the
  member, on the machine it runs on, not on the bus host. Keep them by exporting them from the
  member's side.
- **The restore proof record**, `/var/lib/teamchat-admin/restore-proof.json`. It is evidence
  about the machine that ran the restore drill (its build, Tuwunel and systemd versions), so it
  is never copied to another machine.

## Copying a backup off the machine

A copy taken off the bus host needs both halves: the tar alone cannot bring the history back,
and the database backup alone has no roster, token or CA. Putting a copy back onto a machine
that never held the team is not built: `teamchat-admin restore` restores onto the host that
took the backup, and is refused until the restore drill's proof record says every check
passed (issue #324).

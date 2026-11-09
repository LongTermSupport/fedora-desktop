## 10:41 · action · —

The coordinator answered the four decisions the units builder raised, and the play's findings, on
#193. Applied in the stage A branch:

- `teamchat-tls@` keeps `Before=lxc.service`; its comment now says why (the homeserver is on
  loopback alone, so a guest's member reaches it only through the terminator).
- The health unit no longer writes the admin state. The leaves moved to their own state root,
  `paths.TLSStateRoot` (`/var/lib/teamchat-tls`), `paths.TLSDir(team)` is the leaf directory and
  `paths.CADir(team)` the CA's; the CA files keep their place, so the backup and restore code
  changed only in the directory it makes (`host.RestoreCA`) and in its tests. `team apply` makes
  both directories, a purge removes the leaf's (new line `leaf removed`), the terminator's
  drop-in loads the leaf from the new root. The health unit's `ReadWritePaths=` is the status
  records, the lock directory and the leaves' root.
- A tmpfiles entry (`paths.TmpfilesFile`, text from `unitfiles`) makes the lock directory under
  `/run` and the leaves' root; the play runs `systemd-tmpfiles --create` on it after writing the
  units and before the first apply. Defence Before Fix: `TestEveryDirectoryAUnitWritesIsMadeBeforeItRuns`,
  red with the lock directory left out of the entry.
- `ExecBinaries` is called, in the same change, by `TestEveryBinaryAUnitRunsIsInstalledByThePlay`
  over every unit text and both drop-ins. go-qa's built-in dead-code stage runs `deadcode -test`
  (read in go-qa-ci's `internal/runners/deadcode.go`), so the plan's claim stands and the
  builder's doubt was only that `qa/go-qa.toml` does not name a built-in stage.
- The play asserts `firewall-cmd --state` answers `running`; it writes each unit text trimmed plus
  one newline; Task 8.10 tracks `declare check` and the health IP drop-in.
- The lab scenario carries the host-move procedure, for a team whose `url` is a DNS name.

Still unproven, because the container never runs Ansible or systemd: the play itself, the
tmpfiles entry on a real host, and the restore's order against the first apply on a new host.

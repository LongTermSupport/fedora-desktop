# Plan 00137 — the cycle's contract

This is the interface between the orchestrator (Tasks 3.3, 3.4 and 4.1) and the IaC that
deploys it (Tasks 0.3, 2.2, 4.2 and 4.6). Both sides build against this file. A change to
it is a change to both sides.

## Names and paths

| Thing           | Where                                                                                                                 | Owner / mode                   |
| --------------- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------ |
| Entry point     | `/usr/local/sbin/fedora-desktop-self-update` (source: `files/usr/local/sbin/`)                                        | root:root 0700                 |
| Config          | `/etc/fedora-desktop/self-update.conf`, `KEY=value`, read with `read`, never sourced                                  | root:root 0600                 |
| Become password | `/etc/fedora-desktop/self-update.become` (vault-provisioned)                                                          | root:root 0600                 |
| Vault password  | `/etc/fedora-desktop/self-update.vault` (copied from the checkout the play runs from)                                 | root:root 0600                 |
| Allowed signers | `/etc/fedora-desktop/self-update.allowed_signers` (the owner's public key only)                                       | root:root 0644, dir 0755 root  |
| Deploy clone    | `/var/lib/fedora-desktop/deploy` (D4), owned by root                                                                  | never mounted into a container |
| State           | `/var/lib/fedora-desktop/self-update/` (the last result, the owed post-boot check)                                    | root:root 0700                 |
| Cycle units     | `fedora-desktop-self-update.{service,timer}`, **system** units                                                        | timer: nightly, D9             |
| Post-boot units | `fedora-desktop-self-update-verify.service`, **system**, `After=` the user's restore                                  | runs once per boot when owed   |
| Sudoers         | `/etc/sudoers.d/fedora-desktop-self-update`: `<user> ALL=(root) NOPASSWD: /usr/local/sbin/fedora-desktop-self-update` | validated with `visudo -cf`    |

### Deploy clone and plays

The plays must run from a tree the user can read. The deploy clone is root-owned and
world-readable (0755 dirs, 0644 files), but not writable by the user. The plays run as
the user, from that clone.

Settled by the IaC (`play-self-update.yml`); the orchestrator must match:

- **Vault password.** A root-only copy lives at `/etc/fedora-desktop/self-update.vault`
  (0600). It is copied from the file `ansible.cfg` names in the checkout the play runs
  from. The orchestrator opens it and passes it to the plays on an inherited descriptor:
  `ANSIBLE_VAULT_PASSWORD_FILE=/dev/fd/N`. The clone's own relative path does not
  resolve, because the file is untracked.
- **host_vars.** The play copies `environment/localhost/host_vars/localhost.yml` into the
  clone as `root:<user> 0640`. It is refreshed only when the play runs.
- **git ownership.** The play adds the clone to the user's `safe.directory`. Without it,
  git refuses a root-owned repository, and the ledger callback records nothing.
- **Fact cache (orchestrator's job).** `ansible.cfg` writes `./untracked/facts/`, which is
  root-owned in the clone. The orchestrator must point
  `ANSIBLE_CACHE_PLUGIN_CONNECTION` at a per-run directory the user owns, or set
  `ANSIBLE_CACHE_PLUGIN=memory`. Otherwise the plays fail writing the cache.
- **Post-boot unit.** It runs at every boot with no condition of its own, so `verify`
  exits 0 when no check is owed.

## Config keys

These keys have no defaults. The orchestrator refuses to run if one is missing:

- `USER`
- `BRANCH`
- `REMOTE_URL` (HTTPS for the public repo, Task 2.2)
- `PRINCIPAL`
- `WARN_MINUTES` (default in IaC: 3)
- `ALERT_SINKS` (for example `slack`, `github`, or empty until Task 0.4)
- `ANSIBLE_COLLECTIONS_DIR`: the root-owned collections path for the system
  `ansible-core` (D5 hardening, Task 4.7). The orchestrator refuses, with exit 70, when
  the `ansible-playbook` the child would resolve is not a root-owned file.

## Subcommands

- `run`: the timer's cycle. It takes the play lock, updates through the trust gate, works
  out the affected plays, and runs them as `USER` via `run.bash --headless <play>`, with
  the become password on an inherited fd. If every play succeeded, it records the owed
  post-boot check, warns with `ccy-sessions notify going-down --minutes WARN_MINUTES` (as
  `USER`), waits, and reboots. On any failure it does not reboot (D8), records the
  result and alerts.
- `run --dry-run`: every step up to the plays, which it prints and does not run. It never
  warns, reboots or moves the clone.
- `verify`: the post-boot check. It runs `ccy-sessions verify-restore --wait` as `USER`,
  then records and alerts on the result.
- `status`: prints the last result for a human.

## Exit codes

| Code | Meaning                                   |
| ---- | ----------------------------------------- |
| 0    | done, or nothing to do                    |
| 64   | usage                                     |
| 70   | config invalid                            |
| 75   | lock held                                 |
| 20   | gate or update refused (reason on stderr) |
| 21   | a play failed                             |
| 22   | a session could not be warned             |
| 23   | the verify found a session not OK         |

## Result record

`state/last-result` holds one line per key: `at`, `phase`, `outcome`, `old`, `new`,
`plays`, `detail`. `helpers/host_health` reads it for Task 4.3, and the alert sinks send
it for Task 4.5. It carries no hostname, username or path.

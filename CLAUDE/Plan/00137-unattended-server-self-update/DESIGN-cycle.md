# Plan 00137 — the cycle's contract

This is the interface between the orchestrator (Tasks 3.3, 3.4 and 4.1) and the IaC that
deploys it (Tasks 0.3, 2.2, 4.2 and 4.6). Both sides build against this file. A change to
it is a change to both sides.

## Names and paths

| Thing           | Where                                                                                                                 | Owner / mode                    |
| --------------- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------- |
| Entry point     | `/usr/local/sbin/fedora-desktop-self-update` (source: `files/usr/local/sbin/`)                                        | root:root 0700                  |
| Config          | `/etc/fedora-desktop/self-update.conf`, `KEY=value`, read with `read`, never sourced                                  | root:root 0600                  |
| Become password | `/etc/fedora-desktop/self-update.become` (vault-provisioned)                                                          | root:root 0600                  |
| Vault password  | `/etc/fedora-desktop/self-update.vault` (copied from the checkout the play runs from)                                 | root:root 0600                  |
| Allowed signers | `/etc/fedora-desktop/self-update.allowed_signers` (the owner's public key only)                                       | root:root 0644, dir 0755 root   |
| Deploy clone    | `/var/lib/fedora-desktop/deploy` (D4), owned by root                                                                  | never mounted into a container  |
| State           | `/var/lib/fedora-desktop/self-update/` (the last result, the owed post-boot check)                                    | root:root 0700                  |
| Published       | `/var/lib/fedora-desktop/self-update-status/result`, the user's copy of each result (Task 4.3)                        | dir root:<user> 2750, file 0640 |
| Cycle units     | `fedora-desktop-self-update.{service,timer}`, **system** units                                                        | timer: nightly, D9              |
| Post-boot units | `fedora-desktop-self-update-verify.service`, **system**, `After=` the user's restore                                  | runs once per boot when owed    |
| Sudoers         | `/etc/sudoers.d/fedora-desktop-self-update`: the entry point with exactly `run`, `run --dry-run`, `verify` or `status` | validated with `visudo -cf`     |

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
| 20   | the clone is untrusted, or the gate refused |
| 21   | a play failed                             |
| 22   | a session could not be warned             |
| 23   | the verify found a session not OK         |
| 24   | the reboot was refused                    |
| 77   | not run as root                           |
| 130  | the warning countdown was cancelled       |

## Result record

`state/last-result` holds one line per key: `at`, `phase`, `outcome`, `old`, `new`,
`plays`, `detail`. The alert sinks send it for Task 4.5. It carries no hostname or
username. `plays` holds repo-relative play paths (`playbooks/imports/...`), and `detail`
may name one; no path outside the repository appears.

Outcomes, as `helpers/self_update/published.py` classifies them (a test reads the cycle's
source and fails on one that is not classified):

- ok: `nothing`, `deployed`;
- in progress: `rebooting`;
- failed: `refused`, `config-invalid`, `play-failed`, `unwarnable`, `cancelled`,
  `reboot-failed`, `verify-failed`.

## The published copy (Task 4.3)

The host-health report runs as the user and cannot read `state/`. Loosening `state/` was
rejected: `deployed` and `owed-verify` decide what the next root run does, so they stay
root-only. Instead, every `write_result` also writes `self-update-status/result`, which
holds the result keys plus `owed_boot`: the boot id an owed post-boot check was recorded
in, or empty. The cycle never reads it back.

- **Permissions.** The play creates the directory `root:<user's primary group>` mode
  2750\. The setgid bit gives each file the user's group without a chown in the cycle.
  The file is written atomically at 0640, so the user reads it and only root writes it.
- **Enabled or not.** The play creates the directory when `self_update_enabled` is true
  and removes it when false. The report treats "no directory" as "self-update is not on
  this host" and says nothing. The entry point refuses (exit 70) when the directory is
  missing, as it does for `state/`.
- **What the report says** (`helpers/host_health/self_update_check.py`, section
  `self-update`):
  - a failed outcome is a fault, naming when, the phase and the detail;
  - an unknown outcome, an unreadable file or stamp, or a stamp in the future is "not
    checked";
  - a newest result older than 3 days is a fault. Every run that reaches the update
    records one, including "nothing", so 3 days is two whole missed nights of margin.
    With no result yet, the age is taken from the directory's creation;
  - `owed_boot` set to a boot other than this one is a fault once this boot is older
    than 35 minutes: the verify unit's `TimeoutStartSec=30min` plus margin. An unknown
    uptime does not buy the grace, and an unknown boot id is "not checked".
- **The panel.** The section is always in the document, so the section set does not
  depend on the host. The panel hides it while it is clean (`quietWhenOk`), because a
  desktop never runs self-update. A missing section still renders as unavailable.

## The clone's HEAD is always a signed commit

The gate (`update.py`) judges only commits **above** HEAD, and root imports the cycle's own
code from the clone. So HEAD itself must be a commit the pinned key signed. Three places
hold that, each for a different moment:

1. **The play, after cloning** (`update.py --anchor`, run from the owner's checkout, never
   from the clone it judges). A fresh clone sits on the remote's tip, which nobody vouched
   for. The anchor leaves a signed HEAD alone. Otherwise it moves the branch back to the
   newest signed commit in HEAD's first-parent history, or fails the play. It never moves
   forward: that is the cycle's job, and moving forward here would skip the plays.
2. **The entry point, before importing anything.** It checks, in bash with git's signing
   programs pinned on the command line, that HEAD is `G` for `PRINCIPAL` and that the
   tree has no changes. Otherwise it exits 20 having run nothing from the clone. Nothing is
   recorded, because recording would need the code it refused to run. The unit then
   fails, and the host-health report's failed-units check shows it.
3. **The cycle, on a first run** (`verify_head`). With no deployed record, it refuses
   (exit 20, recorded and alerted) when HEAD is not signed. Layer 2 already covers the
   same case; this one also covers a caller that bypasses the entry point, such as the
   tests.

Keeping the helpers in a separate root-owned install, verified once by the play, was
rejected. That copy would drift from the clone the plays run from, and it would need its
own update path through the same gate. Checking in the entry point costs two git calls
and keeps a single tree.

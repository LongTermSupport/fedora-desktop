# Tasks 3.3, 3.4 and 4.1: the cycle orchestrator (worker fork report)

The task was to build `fedora-desktop-self-update` (`run`, `run --dry-run`, `verify` and
`status`) against DESIGN-cycle.md. The logic lives in the TDD'd `helpers/self_update/cycle.py`,
behind a thin root-only bash wrapper, and is driven end to end by a bash test wired into
`qa-all.bash`. Two later contract points came from the coordinator and are built in: the vault
password and fact cache (from the IaC fork), and the D5 hardening that pins a system
ansible-core.

## What was built

| Path                                              | What                                                                                                                                              |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `files/usr/local/sbin/fedora-desktop-self-update` | root 0700 wrapper: argument and root checks, fixed PATH, every path a constant, `exec python3 -m helpers.self_update.cycle` from the deploy clone |
| `helpers/self_update/cycle.py`                    | config parser, state files, the cycle's order of operations behind a `Host` seam, `RealHost`                                                      |
| `helpers/self_update/update.py`                   | `--dry-run`: every step up to the fast-forward; prints `SELF-UPDATE-TARGET`                                                                       |
| `run.bash` 1.22.1                                 | a `/dev/fd/N` become password is read from the descriptor, not reopened                                                                           |
| `run.bash` 1.23.0                                 | `RUN_BASH_ANSIBLE_PLAYBOOK` pins the ansible-playbook an unattended single play runs                                                              |
| `tests/helpers/self_update/test_cycle.py`         | the order of effects: plays, marker before warning, reboot last, every failure path                                                               |
| `scripts/test-self-update-cycle.bash`             | the real wrapper against a signed git fixture, `runuser` and `systemctl` stubbed                                                                  |

## Measured, not assumed

`untracked/scratch/vault-fd-probe.py` (not tracked) ran as root and offered three descriptors
to `nobody` under `runuser`. The child opened `/dev/fd/N` the way ansible's `FileVaultSecret`
opens its vault password file, with `open(path, "rb")`:

| Descriptor                                     | Result                         |
| ---------------------------------------------- | ------------------------------ |
| a root-owned 0600 regular file                 | `[Errno 13] Permission denied` |
| a pipe root created                            | `[Errno 13] Permission denied` |
| a pipe root created, then fchown'd to the user | reads the password             |

`runuser` kept the passed descriptors open in all three cases. ansible reopens the path; it does
not read the inherited descriptor. So the contract's "open the vault file as root and pass the
descriptor" cannot work if the descriptor is the opened file. The cycle therefore copies each
password into a pipe whose read end it hands to the user (`password_pipe`). That keeps the
contract's interface (`ANSIBLE_VAULT_PASSWORD_FILE=/dev/fd/N`) and nothing touches a disk. A
fresh pipe is made for every play, because a pipe can be read only once. The become password
travels the same way, and run.bash reads it with `cat <&N`.

## Decisions within the contract

- **Fact cache: `ANSIBLE_CACHE_PLUGIN=memory`.** No play sets `gather_facts: false`, so
  every run gathers its own facts. The facts are fresh after the reboot a previous cycle
  made. Nothing is created, chowned or removed, and the user never writes into the
  root-owned clone. A per-run directory would add three root-side steps that can each fail,
  for a cache nothing reads again.

- **System ansible (D5).** The wrapper pins `/usr/bin/ansible-playbook`. Before the first
  play, `check_toolchain` makes two checks:

  - that file, its symlink target and the target's directory, and `ANSIBLE_COLLECTIONS_DIR`,
    are owned by root and not group- or world-writable;
  - `command -v ansible-playbook`, run as the user with the play's own environment, resolves
    to exactly that path.

  If either check fails, the cycle exits 70 with outcome `config-invalid`, an alert is sent,
  no play runs, and the plays stay owed. The play's PATH starts with the pinned directory,
  then `/usr/bin`, and puts `~/.local/bin` last. run.bash 1.23.0 no longer puts
  `~/.local/bin` first when `RUN_BASH_ANSIBLE_PLAYBOOK` is set, and refuses the play unless
  PATH resolves to that file. Without that change, run.bash's own PATH prepend would have
  picked the user's pipx ansible whatever the cycle passed.

- **`ANSIBLE_COLLECTIONS_DIR`** is a required config key: an absolute, normalised path with
  no whitespace. It is passed to the play as `ANSIBLE_COLLECTIONS_PATH`.

## Contract gaps and additions (for DESIGN-cycle.md)

01. **Exit codes outside the table.** 24 means `systemctl reboot` itself refused (the warning
    is withdrawn). 130 means the countdown was interrupted (the warning is withdrawn). 77
    means the wrapper was not run as root.
02. **A `deployed` state file** (`sha=`) is the diff basis, not the pre-update HEAD. A cycle
    whose play failed has already moved the clone, and the next cycle must still owe the play.
03. **The first cycle** (no `deployed` record) runs every allowlisted play, then reboots.
04. **An owed reboot survives.** An unwarnable session (22), a refused reboot (24) or a
    cancelled countdown (130) keeps the owed-verify marker. The next cycle in the same boot
    retries the warning and the reboot, with nothing new to run.
05. **An allowlisted play the mapper cannot follow** refuses the cycle (20), because a change
    to that play could be missed.
06. **REMOTE_URL** is checked against the clone's `origin` URL before any fetch (20).
07. **Dry-run** maps plays using the tree at the current HEAD, because it never moves the
    clone. It records nothing.
08. **The vault handover is a user-owned pipe,** not the opened file (measured above).
09. **The config template on F44 has no `ANSIBLE_COLLECTIONS_DIR` line.** Until Task 4.7 adds
    it, the cycle refuses the config (70). No system ansible-core is installed yet either, so
    plays are refused until 4.7 lands.
10. **Test seams.** `FEDORA_DESKTOP_SELF_UPDATE_TEST_PREFIX` (wrapper) roots every path
    under a scratch directory and lifts the root check. `FEDORA_DESKTOP_SELF_UPDATE_MINUTE_SECONDS`
    (cycle) scales the countdown. The wrapper is root 0700, and sudo's `env_reset` drops both
    variables for the one command the sudoers drop-in allows.

## HOST-only (cannot be exercised in a container)

- The real `/run/user/<uid>` play lock: root takes it, and the user's run.bash inherits it.
- `ccy-sessions notify` and `verify-restore` reached through `runuser … env -i`. Nothing
  in that environment points at the user's tmux server except `XDG_RUNTIME_DIR` and the
  default socket path.
- A real play as the user from the root-owned clone. That depends on the IaC's host_vars
  copy, its `safe.directory` entry, and the system ansible-core and collections (Task 4.7).
- A real reboot, and then the post-boot `verify` unit.

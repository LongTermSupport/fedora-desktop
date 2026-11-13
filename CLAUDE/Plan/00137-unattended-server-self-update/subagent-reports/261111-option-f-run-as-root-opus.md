# Option F: run the cycle's plays as root, no become password (Task 6.1)

Requested by a downstream estate as "option F": the unattended self-update cycle runs its
plays as root, from the root-owned deploy clone with `/usr/bin/ansible-playbook`, so
`self_update_become_password` is no longer required, written to
`/etc/fedora-desktop/self-update.become`, opened, or read from the clone's host_vars copy.
Its motive, stated generically: the vaulted become password and its vault key are both
readable by the login user, and that user has password sudo, so the at-rest copy is root.

**Verdict: stopped, needs the owner.** No code was changed. Both of the request's own stop
conditions hold: the change reverses a recorded owner decision (D5), and running the
allowlisted play as root breaks tasks that rely on the login user's environment. The
options and the question are below.

## 1. What runs as whom today

Read from `files/usr/local/sbin/fedora-desktop-self-update`, `helpers/self_update/cycle.py`
(`RealHost`, `play_environment`, `password_pipe`, `main`) and `run.bash`'s single-play path.

1. The timer starts `fedora-desktop-self-update.service`, a system unit, so the entry point
   and `cycle.py` run as **root**. The entry point refuses anything else (exit 77).
2. `main()` checks `/etc/fedora-desktop/self-update.become` and `self-update.vault` are
   root-owned 0600 (`read_private(..., secret=True)`) before a real `run`.
3. For each play, `RealHost.run_play` reads each file into a fresh pipe whose read end is
   `fchown`ed to the login user (`password_pipe`), and runs
   `runuser -u <USER> -- env -i ... <clone>/run.bash --headless <play>` with
   `RUN_BASH_SUDO_PASSWORD_FILE=/dev/fd/N`, `ANSIBLE_VAULT_PASSWORD_FILE=/dev/fd/M`, the
   held play-lock descriptor, `RUN_BASH_ANSIBLE_PLAYBOOK=/usr/bin/ansible-playbook` and every
   code search path pinned to root-owned or clone paths. `USER` is the config file's
   `USER=`, rendered from `user_login`.
4. So the **plays run as the login user**. `run.bash` reads the become password once from
   the descriptor in `headless_play_preflight` and hands it to
   `ansible-playbook --become-password-file -` on a pipe; every `become: true` task is
   `sudo` with that password. `run.bash` refuses to run as root
   (`headless_play_preflight`: "An unattended play is executing as root").
5. `play-self-update.yml` writes the root-only `self-update.become` from
   `self_update_become_password`, which an install supplies vaulted in the user's own
   `environment/localhost/host_vars/localhost.yml`. That file is also copied into the
   deploy clone, where the cycle's plays read it as data (vault-decrypted with the
   root-only vault copy).

## 2. Why option F cannot be done as asked

The cycle runs only the plays in `helpers/self_update/unattended-plays.json`, which is
`playbooks/imports/play-claude-yolo.yml` (it imports `play-golang.yml`). The downstream
report's §4 judged `play-self-update.yml` (play-level `become: true`, three
`become_user` tasks), which the cycle never runs. `play-claude-yolo.yml` is play-level
`become: false`: its unelevated tasks run as whoever runs `ansible-playbook`. As root these
land on root, not the user:

| Task (file)                                                                                                                                                                        | As the user today                                                    | As root                                                                                                                                                                |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Read Claude YOLO Image ID Before Build`, `Build Claude YOLO Container Image with Hash` (`tasks/claude-yolo-image.yml`)                                                            | builds the ccy image in the user's rootless podman storage           | builds it in root's storage; the user's ccy never sees it, then rebuilds on next launch or runs the old image                                                          |
| `Build the {{ go_command_name }} Command` (`tasks/build-go-command.yml`, explicit `become: false`, used for `ccy-sessions`, `ccy-teams`, `ccy-token-usable`, `ccy-network-policy`) | Go build cache and output under the user's home, files the user owns | root-owned build cache and outputs in the user's home                                                                                                                  |
| `Enable the ccy Session Restore Unit`, `Reload The User Manager ...`, the two `Read Back ...` tasks, `Run the Seat Sweep Timer` (`play-claude-yolo.yml`)                           | `systemd scope: user` reaches the user's manager                     | reaches root's user manager; the restore unit the reboot depends on (D1) is not enabled for the user                                                                   |
| The play ledger callback (`callback_plugins/play_ledger.py`) and its `health_refresh`                                                                                              | records to the user's `$XDG_STATE_HOME`, restarts the user's collect | records to root's home and `systemctl --user` as root; the user's ledger and host-health report go stale, so the drift checks report `play-claude-yolo.yml` as not run |

Every file task that sets no `owner:` would also leave root-owned files in the user's home.

D5 (PLAN.md) records exactly this: "the plays cannot run as root outright, because their
`systemctl --user` tasks need the user's own manager ... Running the controller as root
was rejected: every play assumes it starts as the user." Option F is a reversal of D5, and
it is more than a change to the cycle. The request's scope limits say to stop with
`needs-owner` in both cases, so this report stops.

## 3. Options for the owner

**F-full: make the allowlisted plays root-safe, then run them as root.**

- `cycle.py` runs `/usr/bin/ansible-playbook` directly as root, not through `run.bash`,
  which refuses root. The cycle already holds the play lock.
- Every unelevated task in `play-claude-yolo.yml`, `play-golang.yml` and the task files
  they import gets an explicit `become_user: "{{ user_login }}"`, with `become: true`
  under the cycle only. `podman` and `systemctl --user` tasks also need `XDG_RUNTIME_DIR`
  for the user. The interactive path on every desktop must behave exactly as now.
- The play ledger needs a way to record into the user's ledger when run by root.
- Then the password, the `.become` file, the descriptor and the `--become` argument go.
  The play deletes an existing `.become` file. `self_update_become_password` in host_vars
  is ignored with a deprecation warning.
- A QA gate is needed (Defence Before Fix): an allowlisted play has no task that runs as
  the connection user. Without it, the next play added to the allowlist breaks silently
  as root.
- Closes R3 fully. It is a large change to the most-used play, and it can degrade
  desktops.

**G: keep D5, and stop the password living where the user can read it.**

- `play-self-update.yml` keeps the root-only `self-update.become`, but stops requiring
  `self_update_become_password` in host_vars once that file exists and is root:root 0600.
  It asks for the value only on an interactive first run, or the estate writes the root
  file over its own root connection.
- The host_vars value becomes optional and ignored once the file exists, with a
  deprecation warning.
- When the value is dropped from the user's `localhost.yml`, the user holds neither the
  ciphertext nor, through the clone copy, a copy of it. That closes R3.
- It does not meet outcome 2 as worded: the root-only file and the descriptor stay. The
  user process still sees the password in transit during a cycle. D5 already accepts
  that as the "trusted account".
- A small change, no play rewrite, and no desktop effect.

**H: grant time-boxed `NOPASSWD: ALL` for the cycle's duration.** Not recommended. Any
process running as the user gets root, without a password, while the cycle runs.

**The question:** reverse D5 and take F-full, which needs a gate and a rewrite of
`play-claude-yolo.yml`'s user tasks? Or take G, which closes the at-rest route R3 and
keeps D5's root-only file and descriptor?

## 4. Security reasoning

- **Today.** Root reads the password only from a root-only file and gives it to the user
  process for each play. The real exposure is upstream of the cycle. An install that keeps
  the vaulted value in the user's checkout, with the vault key beside it, gives any process
  running as the user the root password. That is R3.
- **F-full.** No password exists, so nothing can leak, and root runs only signed,
  root-owned code from the clone. The tasks that drop to the user then run code from root
  through `become_user`. That direction needs no secret and is safe, as long as no task
  sources anything the user can write while root. The ledger plugin and the Go build are
  the places to audit.
- **G.** Closes R3, the at-rest copy, which is the route the request names. It leaves the
  in-flight exposure D5 already accepts: the trusted user account during a cycle.

## 5. Acceptance list against this stop

None of the issue's acceptance items are met, because nothing was changed. Each item has
a home in F-full and in G. The "tolerated, not rejected" item holds today already: a
host_vars file that defines the value runs as before.

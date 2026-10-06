# fedora-desktop#69: three restore defects (Plan 00135 Phase 7)

Agent: Opus 5.5, worktree branch off F44. Tracked in PLAN.md Phase 7.

## Defect 1: `verify-restore` cannot run (`can't find pane: =<session>`) — NOT FIXED, blocked

- The cause is as the issue says: `restore_states` in `files/home/.local/bin/ccy-sessions`
  runs `ccy_tmux capture-pane -p -t "=${name}"`. A pane command needs `=${name}:`.
- Sweep of every `-t "=…"` target in `ccy-sessions`, `files/var/local/claude-yolo/` and the
  `cc` launcher: the only pane command is that `capture-pane`. The others (`attach-session`,
  `kill-session`, `list-clients`) take a session or client target, where `=name` is
  correct.
- **Why it is not fixed.** The brief asks for a test against a real tmux server first, one that
  fails on the `=name` form, and says to stop on this item if tmux is not available. tmux is
  **not installed in the CCY container** (`command -v tmux` finds nothing), so I stopped
  there and did not use a stub.
- The suite missed it because the fake tmux in `scripts/test-ccy-sessions-reboot.bash`
  strips the leading `=` from any `-t` value (`target="${2#=}"`), so both forms pass.
- Ways to unblock, for the coordinator to choose from:
  1. Add tmux to the CCY image. That is a Dockerfile change and a container version bump.
     The project rule on missing dependencies points this way.
  2. Write the real-tmux test, run it on the host, and make the one-character code fix.

## Defect 2: a restored session stops at the SSH key menu — FIXED (CCY 3.82.0)

- **The premise in the brief needed correcting.** The `ccy-ssh-keys` label is on the
  container, and the container is started with `--rm`. After a reboot it no longer exists,
  so the restore has no label to read. The label also stores basenames, not paths. The
  session record held no key choice, so it now gets one.

- The menu appears on a restore when the record has no `--ssh-key`, `--no-ssh` or
  `--ssh-agent`, and Quick Launch does not apply. Quick Launch is skipped when the args
  carry `--token`, `--network` or `--no-network`. It also falls away when the saved config is
  deleted, which `load_launch_config` does on **any** `CCY_VERSION` change; see below.

- Fix: after `discover_and_select_ssh_keys` in `claude-yolo`, the launcher handles a single
  chosen key file. It finds its own session (`ccy_tmux_current_session`, new in
  `lib/tmux-session.bash`, which the banner now uses too) and calls
  `ccy_registry_record_ssh_key` (new in `lib/session-registry.bash`). That rewrites the
  record with `--ssh-key <file>`, placed before any `--`.

- **Only records that skip Quick Launch get the key.** Adding `--ssh-key` to a record with
  no flags would switch Quick Launch off. A restore that now takes the saved token and
  network without asking would then ask for them.

- A forwarded agent or "no key" is not recorded: the agent socket path changes after a
  reboot, and `--no-ssh` does not behave the same as choosing 0 at the menu. Those restores
  still stop at the menu. `ccy_known_prompts` already listed `ssh-key`, so `verify-restore`
  names them `WAITING-AT-PROMPT ssh-key` once defect 1 is fixed. A test now pins that verdict
  against the real menu line.

- Tests (red first, then green): `scripts/test-ccy-session-registry.bash`, section "the SSH
  key chosen at the prompt goes into the record". It covers:

  - `--token`, `--network` with `--`, and `--no-network` with restore=no;
  - records left alone: Quick Launch, `--token` after `--`, a key already named, `--no-ssh`,
    and cc;
  - no record, and an unreadable record (a failure);
  - the restore args;
  - a source-order check that the launcher calls this after the key menu.

  `scripts/test-ccy-sessions-take-over.bash` covers `ccy_tmux_current_session`.

- **Still open, outside the brief:** a session with **no** launch flags is restored through
  Quick Launch. After a ccy upgrade (any `CCY_VERSION` bump, so most deploys),
  `load_launch_config` deletes the saved config. The first restore after the upgrade then
  goes through every interactive prompt, starting with the key menu. This may be what the
  owner saw on the real host. `verify-restore` reports it, but no restore avoids it. Fixing
  it is a design decision: for example, accept a config from an older version on a restore,
  or record every launch choice the way the restart path does (`ccy_restart_choice_args`).
  It needs the owner.

## Defect 3: `ccy-sessions reboot` fails late from a headless login — FIXED

- `reboot_permitted` asks `busctl --system call org.freedesktop.login1 … CanReboot`. Only
  `yes` goes ahead. It refuses when the answer is `challenge`, `no` or `na`, when the answer
  is anything else, or when busctl cannot be asked. The refusal names the answer and the way
  round it: `sudo reboot-with-update --in N`, or the machine's own desktop session.
  - This refuses `challenge` even from an SSH login that has a terminal, which polkit could
    prompt at the end of the countdown. The brief asked for that.
- `signal_projects` is split into two parts. `check_projects` finds every problem before
  anything is signalled. `deliver_signal` then signals. `cmd_reboot` runs the permission
  check and `check_projects` together, before it arms the withdrawal trap. A refusal
  therefore reports both problems at once, warns nobody, and sends no `reboot-cancelled`.
  The dry run makes the same checks.
- Every session that cannot be warned is listed by name and project, under two headings:
  no daemon CLI, or a CLI that cannot run. One options line follows: end it
  (`ccy-sessions`, Ctrl-X), or install or repair the daemon.
- A failed `systemctl reboot` now prints "the reboot failed: systemctl reboot exited N".
  The withdrawal line reads "Telling the sessions the warned reboot is not happening".
  `reboot-cancelled` is still sent, because it is the daemon's only withdrawal kind (its
  `operator_signal.py` has three kinds). Telling sessions "failed" instead of "cancelled"
  would need a new daemon kind upstream.
- `reboot-with-update` and `shutdown-with-update` run as root, where `CanReboot` is `yes`,
  so the permission check does not apply to them. They share the session check through
  their `notify going-down --dry-run` rehearsal, so they get the listing of every session.
- Tests (red first, then green): `scripts/test-ccy-sessions-reboot.bash` has a new fake
  `busctl` and the section "everything that would stop it is found before anyone is
  warned". The existing "systemctl refusing the reboot is a failure" case gained two
  assertions. Two existing assertions changed from "no `cli` call at all" to "no `cli`
  signal". The project that does have a CLI is now asked `signal --help`, which changes
  nothing, so that a broken CLI elsewhere appears in the same refusal.

## Verification run here

- Passing: `test-ccy-sessions-reboot.bash`, `test-ccy-session-registry.bash`,
  `test-ccy-sessions-take-over.bash`, `test-ccy-restore-askpass.bash`,
  `test-ccy-restart-request.bash`, `test-ccy-lifecycle.bash` and `test-ccy-ssh-handling.bash`.
- `shellcheck -x -S warning` is clean on every changed bash file, and `bash -n` passes on
  `claude-yolo`.
- The full `qa-all.bash` was not run: the coordinator runs it.
- Not exercised here: the launcher's call into `ccy_registry_record_ssh_key`. The launcher
  cannot run in this container, so only a source-order check covers it. Task 7.4's host
  check is the real proof, and it needs Task 7.1 first.

## Changed files

- `files/home/.local/bin/ccy-sessions`
- `files/var/local/claude-yolo/claude-yolo` (`CCY_VERSION` 3.82.0; the container version
  is unchanged at 2.43, because the image content did not change)
- `files/var/local/claude-yolo/lib/session-registry.bash`, `lib/tmux-session.bash`
- `scripts/test-ccy-sessions-reboot.bash`, `scripts/test-ccy-session-registry.bash`,
  `scripts/test-ccy-sessions-take-over.bash`
- `docs/ccy.md`, `docs/ccy-changelog.md`
- `PLAN.md` Phase 7, and the journal

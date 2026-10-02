# Plan 00135: context and the decisions as first asked

Moved out of [PLAN.md](PLAN.md) to keep it lean. Nothing here is current task state.

## Context & Background

Established by reading, before any code was written. Each is load-bearing for a task
below.

- **`ccy-sessions` already exists** (`files/home/.local/bin/ccy-sessions`, 152 lines) and
  is **picker-only**: its argument parsing accepts `""` or `-h|--help` and rejects
  everything else with exit 64. It also **hard-exits without a TTY** (`[[ ! -t 0 || ! -t 1 ]]`,
  line 42) and refuses to run inside a CCY container. `notify` will be called by an
  automated patch cycle with no terminal, so the TTY guard has to move below dispatch —
  see Task 3.1. This is a restructure of an existing tool, not a new one.
- **`lib/tmux-session.bash`** (339 lines) already exposes `ccy_tmux_list` →
  `<name> <attached-count> <directory>` per line, plus `ccy_tmux_project_sessions`,
  `ccy_tmux_next_name` and `ccy_tmux_is_detached`. Listing live sessions is solved; do not
  re-implement it.
- **`ccy_tmux_insulate <project> <command> [args...]`** (line 248) is the launcher's
  single entry point for creating a session. It is the one choke point where a registry
  write and its matching delete both belong. Anywhere else and they drift apart.
- **No per-session state is written today.** The `.claude/ccy/sessions/` paths in the
  launcher are Claude's own transcripts, not a ccy registry. The registry is genuinely
  new.
- **The daemon dependency has landed and is verified present** in the installed clone:
  `hooks-daemon signal {reboot-warning,shutdown-warning,reboot-cancelled} [--minutes N] [--all-sessions] [--project-root PATH]`.
  `--project-root` matters: the helper runs from outside each project.
- **The `systemd --user` pattern to copy** is `play-container-watch.yml` — unit file under
  `files/home/.config/systemd/user/`, explicit loop, uid resolved via `getent` + assert,
  `scope: user` plus `XDG_RUNTIME_DIR`. **Linger is not its job**:
  `play-systemd-user-tweaks.yml:23-50` already owns that.
  `play-host-health-login-report.yml` is worth reading alongside it for one trap: the
  `ansible.builtin.systemd` module runs `daemon_reload` **before** it changes the enabled
  state, so a reload requested on the enable task re-reads a directory that does not yet
  contain the symlink the enable is about to create. That play splits the reload into its
  own task *after* the enable and reads back `list-dependencies` rather than trusting
  `is-enabled`. Task 2.3 must do the same.
- **The registry path needs deciding, not assuming.** The issue says
  `~/.local/state/ccy/sessions/`, but this repo's own XDG convention is
  `$XDG_STATE_HOME/fedora-desktop` (`helpers/play_ledger/ledger.py:48-81`). Pick one
  deliberately; do not end up with two conventions because the issue named a path.
- **`docs/tmux-sessions.md:27-34`** is the table to amend, and `:36` says in prose
  "Nothing restarts them after a reboot". Both contradict this plan once it lands, so both
  change together — a table row without the sentence leaves the page arguing with itself.
- **Test harness**: `scripts/test-*.bash`, run by `scripts/qa-all.bash`. Seven
  `test-ccy-*.bash` scripts already exist to model on. There is **no** existing test for
  `tmux-session.bash`.

## The open decisions as first asked

Both of the first two came from [research/launcher-facts.md](research/launcher-facts.md)
and neither was answered by the issue. The original questions, for the record:

**1. The helper only warns when the helper is used.** The issue names "an automated patch
cycle" as a reason to want this, but a patch cycle runs `systemctl reboot`, not
`ccy-sessions reboot` — so it would warn nobody. This repo already ships the pattern that
would catch every path: `files/usr/local/bin/ssh-suspend-guard` +
`playbooks/imports/play-prevent-ssh-suspend.yml` hold a `systemd-inhibit --what=sleep`
lock in a loop as a system unit, and `--what=shutdown` is its sibling. An inhibitor would
warn on *any* reboot; the helper warns on one. The helper is still worth having (it owns
the countdown and the deliberate case), but if the patch-cycle case matters, the inhibitor
is the mechanism that actually covers it. Decide which is being bought here.

**2. Replay argv, or reuse the quick-launch path?** `--token`, `--ssh-key` and `--network`
are **already persisted per project** in `.last-launch.conf` (`claude-yolo:2880`), with a
quick-launch path at `:911-915`. Restoring through that path rather than replaying a
filtered argv would make the whole one-shot filter above unnecessary — the persisted set
contains no one-shot flags by construction. That is a materially simpler design than the
issue proposes. It needs checking that the quick-launch path covers everything a restore
needs, but it should be checked before the filter is built.

**3. `shutdown-with-update` already exists** (`files/usr/local/bin/shutdown-with-update`,
119 lines) as a user-invoked pre-shutdown updater. It is not a hook on `systemctl reboot`
— nothing intercepts a plain reboot today — but it is the existing "operator deliberately
brings the machine down" path, and two commands that both mean that should know about each
other rather than diverge.

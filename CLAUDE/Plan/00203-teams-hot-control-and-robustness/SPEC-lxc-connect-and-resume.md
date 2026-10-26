# SPEC: how an LXC member is reached and resumed

Plan 00203, Task 3.18 (extends 3.15 and 3.17; cross-reference Plan 00205). Draft expectations,
numbered for the Fable review, built tests first after it. The owner's ruling is Decision 13
of [DECISIONS-lifecycle.md](DECISIONS-lifecycle.md); Decisions 20, 23 and 24 there are this
spec's open defaults, and CR1 to CR8 its offline and lock rules.

The owner reaches a member by `ssh` or `lxc-attach`, a `cd` into its recorded checkout, then a
command. Some teams use a per-container shell alias (`<alias>`) that starts the container and
opens an ssh session.

## 1. Facts this spec rests on

- F1. Today's route into a member is `sudo -n lxc-attach --clear-env` as the member's user
  (`lxcfreeze.Lister.InContainer`), which runs tmux only, on the socket and session
  `agent-bus-claude` (members.py `SERVICE_TMUX`, `winddown.go` `lxcTmux`).
- F2. Inside the owner's LXC containers `ccy` is the owner's own shell alias for a claude
  wrapper, not this repository's `/var/local/claude-yolo/claude-yolo`. Run with no shell, the
  word `ccy` is not found; in a login interactive shell it resolves. The owner has ruled that
  `ccy --continue` run inside the container resumes cleanly.
- F3. Which transcript store that wrapper's claude reads is not known here, so it is a fact to
  read from a running member (the build's first step), not assumed. The two forms are
  `<checkout>/.claude/ccy/projects/-workspace` (`src-go/internal/transcript/transcript.go`
  `Dir("ccy", ...)`, per checkout) and the user's `<home>/.claude/projects/<escaped directory>`
  (`Dir("cc", ...)`). A service member's unit runs `agent-bus-claude`, which execs `claude`
  directly, so its store is always the user's own.
- F4. `agent-bus-claude` (files/opt/claude-yolo/optional/agent-bus/) has 56 code lines against
  the bash limit of 60 and is measured (it is not on `scripts/bash-limits.allowlist`). It
  resolves its config home (`XDG_CONFIG_HOME`, else `$HOME/.config`) before it sources the
  user's env file, and `PINGBUS_HOME` only after, since the env file may set it.
- F5. Both declare parsers refuse unknown keys (`helpers/agent_bus/declare.py` and
  `src-go/internal/bus/declare`, whose parity oracle pins it with
  `testdata/parity/lxc/unknown-keys.yml`).
- F6. Attach refuses an LXC member row today ("its checkout is not known here" / "it has no
  running session", `src-go/internal/teams/picker/menu.go` `attachRefusal`).
- F7. Plan 00205 (ssh members; research done, nothing built) owns ssh as a provisioning route,
  with ssh-config `Host` aliases as destinations.

The build's first step, in a running member and through the team's connect command: confirm
that `ccy` resolves in a login interactive shell as the member's user and that `ccy --continue`
resumes (F2), and read which transcript store it reads (F3), which settles the default of
`transcripts:` (K1, Decision 23). If `ccy` does not resolve, K2 and Decision 20 are revised
before the build.

## 2. Expectations

- K1. **The settings.** A `connect` and a `resume` setting on the `agent_bus_teams` entry,
  overridable on each `agent_bus_lxc_members` entry, in Decision 20's form, and an optional
  `transcripts: ccy|claude` (Decision 23), whose default is `claude` (the user's own store)
  until the first step (section 1) shows the wrapper keeps a per-checkout one. Both declare parsers, `helpers/agent_bus_lxc/members.py`
  (the unit) and the parity fixtures change together (F5).
- K2. **The defaults.** The connect is today's route (F1). The resume command is the text
  `ccy --continue`, run in a login interactive shell (`bash -lic`) inside the container as the
  member's user so the alias resolves (F2, Decision 20), unless the team or seat configures
  another. No conversation id is passed to claude
  in the container; the newest transcript's name the shutdown records (LS8) is only compared,
  never resumed by.
- K3. **A member without a service** is resumed by the connect, a `cd` into its recorded
  working directory, then the resume command, started in the tmux session `agent-bus-claude`
  on the socket `agent-bus-claude`, as its user (`new-session -d -s agent-bus-claude -c <dir>`).
  These are the names the service unit uses, so the resumed agent outlives the `lxc-attach`
  (or ssh) that started it, and 3.14's, 3.15's and 3.16's `InContainer` route can type to it and
  end it.
- K4. **A `service: true` member** is resumed through its unit (Decision 24), never by a second
  claude beside it, and never by the connect, `cd` and resume command. A seat's resume override
  does not apply to it.
- K5. **The `continue-once` flag** is at a fixed path, `<config home>/pingbus/continue-once`,
  where the config home is resolved as `agent-bus-claude` resolves it before the env file (F4):
  `XDG_CONFIG_HOME`, else `$HOME/.config`, with the `HOME` the unit sets. It is not in
  `PINGBUS_HOME`, which the env file may move.
- K6. `agent-bus-claude` reads and removes the flag just before its `exec`, after
  `pingbus config check`, and adds `--continue` for that start only. So a launch refused by the
  config check, which systemd retries, still continues, and a later `Restart=always` restart
  finds no flag. The change fits in 4 code lines (56 of 60, F4) or moves into pingbus.
  `agent-bus-claude` is in the image's agent-bus kit, so this takes the CCY and container
  version bump, and a member gets it only once play-agent-bus refreshes its kit.
- K7. **A service member's resume, in order:** write the flag as the member's user; remove the
  hold (LS4) and run `systemctl daemon-reload`; `systemctl start` the unit; then check that
  `ActiveState` is active and the `agent-bus-claude` session exists. The flag is written first,
  so an apply that starts the unit in between also continues. If the start fails, the resume
  removes the flag it left, and the entry is kept (exit 1).
- K8. **The transcript check** (Decision 23 (b), LS8, RS9). The store's path comes from
  `transcript.Dir`: the "ccy" form with the working directory when `transcripts: ccy`, the
  "cc" form with the user's `<home>/.claude` for a service member or `transcripts: claude`, so
  the escaping rule is never copied. The folder is listed through the user runner, read-only,
  and the newest is chosen by `transcript.Newest`'s rule (the newest `*.jsonl` directly in the
  folder, ties by name), factored so the Go rule takes a listing rather than a local directory.
- K9. **Shared stores are refused** (Decision 23 (c)): both parsers refuse two entries for one
  (container, user, team), and two entries of different users in one container that name one
  checkout when the store is per checkout (`transcripts: ccy`), naming the two entries and the
  checkout. The second refusal applies only if the first step finds the default store is per
  checkout.
- K10. **The runners.** `InContainer` stays tmux only. A new `lxcfreeze` runner runs a named
  program inside the container through the same `sudo -n lxc-attach --clear-env`, each program
  on an allow list:
  - as root: `install` and `rm` of the hold path only (LS4), and `systemctl daemon-reload`,
    `stop`, `start` and `show` of `agent-bus-claude-<user>.service` only;
  - as the member's user: `agent-bus seat list` (RS5), the transcript listing
    (`find <store> -maxdepth 1 -name '*.jsonl' -printf '%T@ %f\n'`), the flag's write and
    removal, `tmux -L agent-bus-claude new-session` (K3), and the configured resume command.
- K11. **Attach on an LXC member row** is new. It runs the connect, then
  `tmux -L agent-bus-claude attach -t agent-bus-claude` as the member's user, in place of the
  refusal (F6). An offline member's row reports exit 69 (CR2).
- K12. The settings are used by 3.17's resume, by 3.14's and 3.15's paths (the connect only),
  and by Attach.
- K13. **Plan 00205's boundary.** This task only names a command for an already provisioned
  member, and takes 00205's destination as its ssh form once that is built, never a second ssh
  model.

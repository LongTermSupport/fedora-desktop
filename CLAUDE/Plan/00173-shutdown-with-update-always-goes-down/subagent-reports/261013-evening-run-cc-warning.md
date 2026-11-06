# Plan 00173 follow-up: the owner's evening run and the missing cc warning

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

## Branch and commits

- Branch `agent-ae87230b6e5b05508-967f0e77`, pushed, **not merged**.

- `1adc6c92` Plan 00173: evening run accepted; cc sessions named as not shown the warning.

- `959a79b7` Plan 00173: qa-reviewer fixes. The reviewer's verdict was fix-before-merge, for two reasons:

  - "ccy/cc sessions" warning claims were left in other places: `shutdown-with-update`, its bashrc include, `fedora-desktop-self-update`, `play-self-update.yml`, `play-basic-configs.yml` and `docs/playbooks.md`.
  - The strict-mode help implied that a session which cannot be warned always stops the run, but a cc session does not.

  Both are fixed. I did not re-run the reviewer after the fixes.

- QA run: `scripts/test-ccy-sessions-reboot.bash` passes in full (291 passed, 0 failed). Shellcheck shows no warnings and the YAML parses. The full `qa-all.bash` was not run, because sub-agents are blocked from it; the coordinator runs it before merging.

## The owner's test (Task 2.2)

- Recorded through `mkplan.bash --journal 00173 finding`, in `JOURNAL/00173-Journal-26-10-13.md` (two finding entries).
- Task 2.2 is ticked: the full run updated, powered off and rebooted cleanly.
- The success criterion "with a container-less ccy session open ..." is **left open**. The report does not say whether a ccy session was at its startup prompt during the run. The fake-backed tests cover that case.

## Verdict on the message: (b) it works as designed, and the claims about it were wrong

**No host Claude Code session can be shown the warning, whether started through `cc` or as plain `claude`.**

1. `shutdown-with-update` warns through `ccy-sessions notify going-down`. That command lists the sessions on CCY's tmux server (ccy and cc both run there) and runs each project's daemon CLI once: `hooks-daemon signal <kind> --minutes N --all-sessions`.
2. The daemon's `signal` (`.claude/hooks-daemon/src/.../utils/operator_signal.py`) only writes a `<session>.operator-signal` file. It writes one for each session that has a context sidecar in the project's `context-sidecar/` directory. A host session's status line writes a sidecar, so a file was very likely written for the cc session too.
3. Only the ccy supervisor reads that file (`.claude/ccy/claude-supervise.py`, `load_operator_signal`), and only for its own sessions. ccy's entrypoint runs claude inside that supervisor. `files/var/local/claude-code/cc` runs `claude "$@"` with no supervisor. So no process on the host ever reads the signal.
4. A `claude` started outside `cc` is not on CCY's tmux server, so its project is not even signalled, unless a ccy or cc session is open in the same project.

The defect on our side was in what the code and docs claimed:

- the `ccy-sessions` comment said "ccy and cc alike";
- `shutdown-with-update --help` said it warns "every running ccy/cc session";
- `docs/ccy.md` said one delivery reaches every session.

None of these is true for cc.

**What the commit does (small, inside the plan's intent: whoever runs it is told what is not covered):**

- `ccy-sessions` check_projects now names every `cc-*` session on stderr under "Not shown the warning", on each warning and on the dry-run rehearsal. It does not do so on a withdrawal.
- Delivery is unchanged, and strict mode does not refuse over a cc session.
- The comments, `--help` and `docs/ccy.md` are corrected.
- `scripts/test-ccy-sessions-reboot.bash` has a new section plus one desktop check. The whole suite passes. Shellcheck is clean at warning level.

**Not implemented: a way for cc sessions to be shown the warning.** Two routes, each its own plan, and the owner's choice (Task 3.3):

- Run the project supervisor in `cc`, the way ccy's entrypoint does. It brings ctrl+z handling and plugin behaviour with it, so it is a feature in its own right.
- An upstream daemon handler that shows a pending operator signal on the session's next hook event. An idle session never gets it.

## Host run needed

I have updated meta-deploy: `PLANS` now lists `00173-shutdown-with-update-always-goes-down`. That only takes effect once this branch is merged. Then run `./CLAUDE/Plan/meta-deploy.bash`. It runs this plan's `deploy.bash`:

- `play-claude-yolo.yml` installs the new `ccy-sessions`;
- `play-basic-configs.yml` installs the corrected `shutdown-with-update` help text and its bashrc include.

The self-update entry point's comment change only reaches a server when `play-self-update.yml` next runs there. It is a comment, so nothing needs to run for it.

## Open for the owner

- Whether a ccy session was left at its startup prompt during the evening run. Answering this closes the remaining success criterion.
- Task 3.3 has two decisions:
  - whether cc sessions should be shown the warning at all, and if so, by which route;
  - whether a session-strict run should refuse over a cc session. Strict runs are the server profile, `--strict-sessions` and the unattended self-update. Today a cc session never stops any run, and the help and docs now say so.
- The failed-dnf question is **settled**. The owner's earlier words were "MUST do the update and shutdown", and the plan's Technical Decisions record them. A desktop goes on past a failed dnf; strict mode stops.

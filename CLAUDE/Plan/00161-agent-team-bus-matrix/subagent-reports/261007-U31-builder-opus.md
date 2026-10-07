# U31 builder report: ccy `--teams`, headless Quick Launch

Unit U31 of DESIGN.md §12, built on `seats/U31` from `seats-integration`. CCY 3.85.2 → 3.86.0,
container 2.45 → 2.46.

## What was built

| Piece                                                        | What it does                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `files/var/local/claude-yolo/lib/agent-bus-seats.bash` (new) | `ccy_seats_check` (runs `agent-bus seat check <list>`, prints the canonical list, passes its code on; 78 for a missing `agent-bus`, naming `play-agent-bus.yml`, or for output that is not one list), `ccy_seats_take` (runs `agent-bus seat take <canonical> [--no-prompt]`, its report on stderr, its code passed on; fills `CCY_SEAT_RUN_ARGS` with `-e PINGBUS_SEATS=…` and `--label ccy-seats=…`; an empty list calls nothing), `ccy_teams_canonical_args` (the launcher's arguments with `--teams` and its value replaced by `--teams <canonical>`)                                                                                              |
| `claude-yolo`                                                | `--teams <list>` / `--teams=<list>` in the parser (given twice, or followed by an option: 64 at once; no value or empty: 64 after the loop); `seat check` right after the parser, its code the launch's, plus a quoted-spelling hint when the list ends in a comma and the shell split off a word; the canonical list rewritten into `"$@"` and `CCY_ORIG_ARGS` before tmux insulation records the session; `seat take` immediately before `container_cmd run` (`--no-prompt` when headless, restored or restarted); `"${CCY_SEAT_RUN_ARGS[@]}"` on the run line; `--help` and the did-you-mean list; headless Quick Launch and the headless key check |
| `lib/session-registry.bash`                                  | `--teams` is a kept-value flag and `--teams=<list>` a kept word, so restore and restart replay it                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `entrypoint.sh`                                              | PROJECT-ENV records `PINGBUS_SEATS`, `PINGBUS_TEAMS`, `PINGBUS_HOME` (set or not, and value) before sourcing; AGENT-BUS refuses any that either file set, changed or unset (naming `ccy --teams`), refuses `PINGBUS_TEAMS`/`PINGBUS_HOME` from the launcher (an older ccy), and with `PINGBUS_SEATS` links `pingbus`, adds `--plugin-dir`/`--settings` after `claude` and sets `_ccy_seat_exec=(pingbus seat exec --)`; both final exec lines start with `"${_ccy_seat_exec[@]}"`                                                                                                                                                                      |
| `lib/common.bash`                                            | dist version 3: U19's `PINGBUS_TEAMS` block removed; the `HOOKS_DAEMON_HOSTNAME` note says it also names a new seat's host (else `local`) and that the container refuses a bus variable from the file                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `Dockerfile`, `play-claude-yolo.yml`                         | label 2.46; the comment on the kit; the play installs the new lib                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `.claude/ccy/ccy.env.local.dist`                             | regenerated at version 3, as a launch would                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `docs/ccy.md`, `docs/ccy-changelog.md`, `CLAUDE/QA.md`       | the `--teams` section (replacing `PINGBUS_TEAMS`), the two "CAN reach" rows, the `--headless` and `--teams` rows, the `ccy-seats` label; the 3.86.0 entry; the two gate rows                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `scripts/qa-all.bash`                                        | new `ccy-teams` gate                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |

## Tests

- `scripts/test-ccy-teams.bash` (new, 73 cases): the library against a fake `agent-bus`;
  the real launcher in a throwaway `git init` repository with stub `podman`, `ssh` and
  `ssh-add` that record and refuse, for `--teams` with no value, followed by an option,
  empty, twice, `seat check`'s 64 and 78, the unquoted-space form (64, nothing reaching the
  engine), `agent-bus` missing (78), an accepted list going on to the next check with no
  `take` yet, and a plain launch in a checkout with a seat directory calling no `agent-bus`;
  restore and restart arguments with and without `--teams`; the Quick Launch block and the
  headless key check cut out of the launcher (`read -p` replaced by a recorder; real
  `ssh-keygen` keys with and without a passphrase); the wiring (lib in `CCY_LIBS`, sourced,
  installed by the play, `--help`, take just before the run line, the rewrite before tmux).
- `scripts/test-ccy-agent-bus.bash` reworked (41 cases), `scripts/test-ccy-env-local-dist.bash`
  (35), and `scripts/test-ccy-restore-askpass.bash`'s exec-line lookup updated to the new
  final line (87).
- Seen failing first: the dist test (3 cases), the entrypoint test (19 cases), the launcher
  test (library missing). All callers rerun green: project-env, session-registry,
  restart-request, info-flags, lifecycle, container-version-hook, sessions-reboot,
  sessions-take-over, gitignore-safety, git-signing, gpu-device, relabel-preflight,
  session-network, network-disconnect, host-hostname, ssh-handling, token-mode;
  `test_member_docs`, `test_admin`.
- shellcheck: every touched bash file clean; the launcher shows the same six pre-existing
  findings as the base (compose code, `$NETWORK_FLAG`). No Python changed.
- `ansible-playbook --syntax-check` of `play-claude-yolo.yml` could not run here: the
  worktree has no vault password file (the edit-time hook reported only that).

## Decisions taken (DESIGN.md D56, D57)

- **D56** `--teams` followed by an option is the launcher's own "no value" refusal; the
  canonical list replaces the given one in the launcher's own arguments before tmux records
  the session (so restore and restart replay the canonical spelling); `--teams` is a
  kept-value registry flag; the comma hint; `ccy-seats` only with `--teams` (no `none`
  value); a new library rather than growing `common.bash`.
- **D57** headless: an empty saved key or network becomes `--no-ssh`/`--no-network`
  (otherwise the picker or network detection would read stdin); a saved file with no token
  is refused; the passphrase check applies to every headless launch.

## Risks and things the coordinator must know

- **Needs U32 to launch into a team.** The launcher calls `agent-bus seat check` and
  `agent-bus seat take`, which U32 builds. The contract assumed: `seat check <list>` prints
  exactly one line, the canonical list, on stdout and exits 64/78 on refusal (it must name an
  empty item clearly, since the launcher only adds a hint); `seat take <canonical> [--no-prompt]` exits 0, 75 or 78 and may print `CHANGED` lines (shown on stderr). Both run
  in the checkout's top level as the calling user. Without U32 every `--teams` launch fails
  with the wrapper's own usage error (not 0); a plain launch is unaffected.
- **U32's docs.** `README.podman`, `docs/agent-bus.md` and `helpers/agent_bus/admin.py`'s
  `NEXT_STEPS["podman"]` still tell a ccy member to put `PINGBUS_TEAMS` in `ccy.env.local`,
  which the entrypoint now refuses. The design gives the first two to U32; the admin text is
  printed into the bundle's README by `add-member`, so U32 (which writes those bundles) is the
  natural owner too.
- **Headless after an upgrade.** `load_launch_config` discards a saved file written by
  another ccy version, so the first headless launch after deploying 3.86.0 in a checkout is
  refused until one interactive launch there (or explicit `--token` and `--ssh-key`/
  `--ssh-agent`/`--no-ssh`). U20's rework launches headless sessions: it must pass the
  choices as flags or expect one interactive launch first.
- **Decision numbering.** D56-D57 may collide with rows U32 adds on its own branch;
  renumber on integration as D55 was.
- **Host run.** None of its own: `deploy.bash`'s last leg (play-claude-yolo.yml) already
  installs the launcher and rebuilds the image; its label now names U31. Plan 00161 is
  already in `meta-deploy.bash`'s `PLANS`. A real `--teams` launch is exercised by U20.

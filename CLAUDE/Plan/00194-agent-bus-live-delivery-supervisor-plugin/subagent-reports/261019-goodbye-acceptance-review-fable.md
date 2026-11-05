# Review: Task 2.0b host acceptance (branch feat/goodbye-acceptance at c1143334, from F44 442143f5)

Reviewed: `acceptance.bash`, `_acceptance-goodbye.inc.bash`, `goodbye_check.py`,
`test_goodbye_check.py`, the PLAN.md and journal edits, the `meta-deploy.list` entry, against
Plan 00201's `_acceptance-m7.inc.bash` / `m7_check.py`, `_planlib.inc.bash`, `meta-deploy.bash`,
the launcher (`claude-yolo`, `lib/agent-bus-membership.bash`, `lib/tmux-session.bash`,
`lib/session-registry.bash`, `lib/docker-health.bash`), `helpers/pingbus/cli.py` and `hooks.py`,
`helpers/agent_bus/checkout.py` and `files/usr/local/sbin/agent-bus-install`. Nothing changed.

## Ran here (container)

- `python3 test_goodbye_check.py`: 26 tests OK (includes the `--fake` run).
- `shellcheck -x acceptance.bash _acceptance-goodbye.inc.bash`: clean.
- `ruff check` on both Python files with the repo's `ruff.toml`: clean.
- `acceptance.bash --fake`: exit 0, FAKE ACCEPTED; every negative case rejected with the
  reason named. Not run for real.

## Verdict

**MERGE.** No finding is a safety defect or a wrong verdict. The findings below are
documentation accuracy and bounded failure modes already named by the builder.

## Safety (the owner's live machine)

Nothing of the owner's is stopped, removed, signalled or purged by any path:

- **Containers**: `gb_containers` filters `--filter label=ccy-session=<name>` with the three
  fixed names `accept194-ctrlc|stop|window`; podman's label filter with a value is an exact
  match, not a prefix. `gb_remove_my_containers`, `gb_container_up`, `gb_no_container` and
  `gb_check_gone` all go through it. `m7_remove_containers` removes only containers whose
  membership mount names a seat of `accept194a`/`accept194b`. `podman stop` in G2 targets
  `GB_CTR`, resolved from that label and required to be exactly one container.
- **tmux**: every tmux call is `gb_tmux`, i.e. `tmux -S $XDG_RUNTIME_DIR/accept194-tmux/ccy`
  with `TMUX`/`TMUX_PANE` unset. The owner's ccy server is `tmux -L ccy`
  (`/tmp/tmux-<uid>/ccy`): a different socket. `kill-server`, `kill-session -t =name`,
  `kill-window -t =name:` are exact-name targets on the run's own server. Inside the launcher,
  `ccy_tmux_insulate` returns at once because `$TMUX`'s socket basename is `ccy`, so no
  session record is written to the owner's registry (`~/.local/state/ccy/sessions`) and
  `ccy-sessions restore` never learns of these sessions (`--no-restore` is therefore moot but
  harmless). `ccy_tmux_current_session` asks tmux through `$TMUX`, so it answers from the
  run's server. The explicit `tmux -L ccy` calls in `tmux-session.bash` are all on the
  insulate/attach path that is not reached.
- **Teams**: a team is purged only when (a) its name is one of the two fixed names, (b) the
  owner's `localhost.yml` does not declare that name (`gb_prerequisites`, via
  `helpers.agent_bus.declare`), and (c) an installed team of that name has a description
  starting `Plan 00194 goodbye acceptance` (`gb_own_or_absent` → `m7_check.py expect-own-team`); install state with no team file is refused, never purged.
  `agent-bus-install remove --team X --purge` on an absent team is a no-op (its `cmd_remove`
  does not call `require_team_dir`), so the unconditional `m7_remove_teams leftover` is safe.
- **Files**: `rm -rf` targets are the three membership dirs by exact name under
  `$XDG_RUNTIME_DIR/ccy/membership/`, `checkout/` dirs under this script's own
  `untracked/plan-runs/<plan>/acceptance/*/`, and `GB_CHECKOUT` inside the run dir. The
  owner's checkout, seats and `.last-launch.conf` are only read (the token *name*).
- **Cleanup on every exit path**: `plan_on_cleanup gb_teardown` (no hand-rolled trap, R4) runs
  on EXIT and INT/TERM/HUP, before the log drains. `check_leg`'s `exit 1`, a failed
  `plan_deploy_leg`, and a Ctrl-C all reach it. It kills the run's tmux server (launchers get
  SIGHUP, their cleanup posts the disconnect notice into the throwaway teams), waits for
  every launcher (bounded), removes labelled containers, parks seats, deletes the checkout
  and membership dirs, purges the teams last. A failure mid-setup is covered:
  `TEAMS_PRESENT=1` is set before the installer runs. A fresh run first removes an
  interrupted run's leftovers (`gb_leftovers`).
- **Bounded waits**: `gb_poll` deadlines 600/180/180/60 s, mash 120 s, logins/pages `curl --max-time 30`, `podman stop|rm -t 10`, pagination capped at 40 pages. The only
  unbounded call is `podman exec … python3 -c` in `gb_session_started` (negligible).
- **Secrets**: the human password goes to `login-body` on stdin; the access token goes to curl
  as a `--config -` header line on stdin; `GB_PASSWORDS` is cleared after login; only the
  token *name* is printed. The run log is under `untracked/`.
- **Install-specific names**: none in the diff (`fedora-desktop`, `tester`, `accept194*` only).

## Correctness (do the counts prove G1-G4)

- What is counted is what the human sees: `/messages` as the team's logged-in human,
  `dir=b` paged to the end, filtered to `m.room.message` from the seat's member user with the
  `agent_bus.text` content key, at or after the scenario's `start-ms`. Both the goodbye
  (`post_goodbye`) and the disconnect notice (`_tell_disconnected`) go through
  `_post_to_humans_and_coordinators` → `_send_text`, which writes that key, and the acceptance
  human is a listed human of the team file, so it is an addressee. The exact wording is
  checked against the templates (`hooks.py` `goodbye`, `cli.py` `DISCONNECT_TEXT`), and the
  reason `exit` is what `GOODBYE_REASONS` maps `prompt_input_exit` (a double Ctrl-C) to.
- No post can land after the count: the goodbye is posted inside the container before Claude
  exits and the container is gone before the count; the disconnect notice is posted by the
  launcher's EXIT trap (`ccy_bus_disconnected`), and `gb_finished` waits for the launcher's
  exit *and* the container's absence first. Nothing of a scenario survives into the next
  (launcher dead, container gone), and each scenario's `since` is its own `start-ms`, so an
  earlier goodbye cannot leak into a later count. G4 is enforced in every scenario by
  `expect_tally` (both present fails whatever was wanted).
- Ctrl-C mashing is faithful: `send-keys C-c` every 0.2 s until `#{pane_dead}` is 1, i.e.
  through Claude's exit, the container's exit and the launcher's cleanup, which is exactly
  the owner's "on past the container's exit". The pane check then catches a traceback or a
  failed/stopped `pingbus seat disconnected`.
- "Claude Code is up" = the pingbus SessionStart hook has written a per-session record in the
  container (`/tmp/pingbus-sessions-<uid>` non-empty), plus 5 s. The hook only runs once
  Claude Code has started the session (past any first-run dialog), so it is a sound signal.
- G2/G3 encode the plan's Success Criteria exactly (disconnected, no goodbye). If Claude Code
  turns out to run SessionEnd on SIGTERM (podman stop) or SIGHUP (closed window), the run will
  REJECT with "posted 1 goodbye(s), want 0" — a true report about the behaviour, not a test
  bug.

## PlanScriptStandards and the plan library

R1 verbatim bootstrap; R2 `plan_require_host` (fake mode's exception is annotated
`STANDARD-EXCEPTION(R2)`); R3 `plan_prime_sudo` before `plan_start_log`; R4 `plan_start_log auto`, `plan_on_cleanup`, no own trap; R5 no `read`; R7 `plan_mode deploy`, every leg a bare
top-level statement; R8 no confirmation; R9 acceptance renders the verdict, no deploy.bash;
R10 report in `PLAN_RUN_DIR`; R11 no `|| true`, no silencing redirect (tmux stderr is captured
and matched); R12 `acceptance.bash` is 100755 (the sourced `.inc.bash` 100644 is right); R13
fine; R14 untouched. `check_leg` mirrors Plan 00201's pattern.

## meta-deploy.list entry

Confirmed from `meta-deploy.bash`: `ENTRY_RE` permits slashes but `UNIT_RE` only admits
`playbooks/….yml` or `<folder>/<script>.bash` (two segments), so a `CLAUDE/Plan/<plan>`
prefix is rejected as "not an entry". The bare plan-folder entry is the right form; with no
`triage.bash`/`deploy.bash`, `deployOk` stays 1 and only `acceptance.bash` runs. Exit 2 is
reported as UNSET, as the script intends. Header lines 1-21 unchanged.

## Findings (none blocking)

1. **(Doc) The list comment says "It changes nothing else"; each of the three launches also
   runs the launcher's `clean_stale_containers_startup`, which removes every exited/created
   `*_yolo` container on the host, the owner's included.** The script header says so; the
   list comment, which is what the owner reads before running, should too. One clause.
2. **(Bounded failure, documented) sudo after `seat take`/`seat remove`.** Both end with
   `sudo -k` (`checkout.py` `sudo_forget`). On this host the desktop profile grants
   `NOPASSWD: ALL` (`play-basic-configs.yml`), which is why Plans 00201 M7 and 00203 M6 held
   and why `plan_prime_sudo` will print "needs no password". On a host without it, the purge
   after `gb_park_seats` fails, the teardown's purge fails too, and the two teams stay
   installed (units running, firewalld rules open on the bus address) until the next run's
   `gb_leftovers` removes them. The script names it. Acceptable; worth one sentence in the
   header ("needs passwordless sudo, or the teams stay until a re-run").
3. **(Bounded failure) The launcher's zombie-container menu.** The launch is interactive
   (`CCY_UNATTENDED_LAUNCH` false), so if the host has any running `*_yolo` container with a
   TTY and no `podman run` process (the launcher's zombie definition), the launcher shows a
   menu in the pane and waits for keys. Nothing types into it (Ctrl-C only starts once the
   container is up), so it is safe, but the run then fails after the 600 s start bound with
   only the pane capture to say why. Low likelihood on a desktop; could be pre-checked with
   `find_zombie_containers`' criteria and reported as COULD NOT ESTABLISH.
4. **(Nit) `--check` exits 64.** R8 calls `--check` the rehearsal; refusing it is
   defensible (there is nothing to rehearse), but printing the plan of what the run would do
   and exiting 0 would be more in its spirit.
5. **(Nit) `FakeRunTest` leaves a run directory under `untracked/plan-runs/…/acceptance/`
   on every test run.** Harmless accumulation.
6. **(Observation) PLAN.md marks Task 2.0b ✅ while the host steps remain unticked in
   Success Criteria** — pre-existing; the diff correctly leaves the criteria unticked and
   points at `acceptance.bash`. No drift introduced.

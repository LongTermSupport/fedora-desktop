# U20 rework: M2 in this repository's own checkout (builder report)

Unit U20 of Plan 00161, reworked to DESIGN.md §12 "U20" (D34, D44, D48-D50) on
`seats-integration`. It replaces the throwaway-checkout version of the M2 slice. Named
`-rework-` so it cannot collide with `261007-u20-builder-opus.md` on a case-insensitive clone.

## What was built

- `_acceptance-u20.inc.bash`, rewritten. Sessions run in `PLAN_REPO_ROOT` as
  `ccy --headless --no-restore --no-supervise [--teams <seat>@acceptance] --prompt … -- --input-format stream-json --output-format stream-json --verbose --model haiku`,
  with stdin a fifo. Each child closes every other session's input before it execs ccy, so
  sessions can be launched one after another, each running before the next starts. No
  `--token`, `--ssh-key` or `--no-ssh`: headless Quick Launch (D38, D57) takes the saved
  choices.
  - Prerequisites, all before M1, each failing with an `OWNER:` line: ccy and `agent-bus`
    installed; the image at `REQUIRED_CONTAINER_VERSION` with the kit in it; a
    `.last-launch.conf` from the installed ccy version and record schema (a headless
    launch would otherwise discard it and be refused); the token unexpired; every saved
    key usable unattended (`ssh-agent` with `ssh-add -l` answering, or a key file that opens
    with `ssh-keygen -y -P ''`, which is ccy's own test). Then containers holding an
    acceptance seat are removed, and `.claude/ccy/pingbus/seats/acceptance/` from an
    interrupted run is deleted, with `seats/` and `pingbus/` pruned when left empty.
  - M2.0: record `ccy.env.local` (sha256 or absent), the `.claude/ccy/pingbus/` listing (to
    the seat level), `git status --porcelain=v1`, HEAD and `sudo agent-bus list`. Then
    `sudo -n true` refreshes sudo, the three launches run in turn, handles are read from
    `agent-bus seat list`, and `set-role acceptance <acca> --role orchestrator` runs. The
    check covers `ccy.env.local` unchanged; each seat tree 0700/0600 and owned by the user;
    the seat directory, `member.json` and `token` git-ignored; each handle's seat, its
    `<host>` (from `checkout.checkout_host`, the seat commands' own rule) and type
    `podman`; exactly three new members with their roles, all active; all three held.
  - Orders: each session's `init` line must list the `pingbus` plugin (the "plugin loads"
    confirmation PLAN.md owed). Each seat's `pingbus status` must show `wake=watcher`, read
    on the host through a home under the run directory whose one link leads to the seat
    (D50's shape).
  - M2.1/M2.2 work as built, with transcripts found by session ID
    (`.claude/ccy/projects/*/<id>.jsonl`). M2.1 also requires a notice in `accb`'s
    transcript before a later command that ran `pingbus recv`, and exactly one
    `stdin.jsonl` line.
  - M2.3 (D39, D59): the watcher is found in `accc`'s container by its argv (a python
    listing of `/proc`, judged by `u20_check.py watcher-pids`) and sent TERM. The check
    then waits for `wake=none`, sends a "nothing to do" input and waits for `wake=waiter`.
    M1's `a` sends a review and the ack must reach the room. The transcript must hold the
    Stop guard's no-waker text and no notices, and `stdin.jsonl` two lines.
  - M2.4 works as built.
  - M2.5: with all three held, launches with `accb@acceptance` (75, "is held by another
    session"), `accd@acceptance,acce@acceptance` (64, "one seat per team per session") and
    `accd@acceptance,` (64, "trailing comma"). Seats, members, the pingbus listing and the
    seat-labelled container listing must all be unchanged.
  - Ending sessions: `watch.log` is copied out with `podman exec … cat`, the input closed,
    and the transcript (with its sibling directory, when present) moved into the run
    directory.
  - M2.6: all three seats are free.
  - M2.7: `accb2` runs `pingbus history`. The member handles must be the same, with
    `HISTORY in` for M2.1's review and `HISTORY out` for its ack.
  - M2.8: `seat remove accc@acceptance` must delete the directory and leave the handle
    `worker parked`. After a sudo refresh, `accc2` must come back as the same handle, active,
    with no new member, and its history must hold M2.3's review in and its ack out.
  - M2.9: the plain container is the only new `ccy=true` container. Its labels must carry no
    `ccy-seats`, its `init` line nothing of pingbus, a command's output must say pingbus is
    not found, and seats and members must be unchanged.
  - Cleanup: any live session is ended, containers removed, and `seat remove` run on the
    present acceptance seats, followed by the token scrub. M2.10 then compares the record.
  - Early stop (`plan_on_cleanup`, before the team purge): every step is tried. If
    `seat remove` cannot run (no team, or it fails), the acceptance seat tree is deleted.
- `u20_check.py`: new commands are `session-id`, `transcript`, `expect-plugin`,
  `expect-notice-before-recv`, `expect-no-waker-block`, `expect-history`,
  `expect-not-found`, `seat-handle`, `expect-seats`, `expect-handle`, `expected-host`,
  `seat-permissions`, `expect-new-members`, `expect-member`, `expect-same-handles`,
  `watcher-pids`, `seat-containers`, `expect-no-seats-label` and `launch-keys`. The
  notice commands now take a transcript file. The orders kinds are `bus|idle|history|plain`.
  Every order forbids edits, commits and pushes, names
  `STOPPING BECAUSE: waiting on the agent team bus`, and single-quotes event IDs.
- `test_u20_check.py`: rewritten (66 tests). It was seen failing (41 failures and errors),
  then passing.
- `acceptance.bash`: header, usage text and legs M2.0-M2.10.
- ccy (D60): `entrypoint.sh` under `--no-supervise` unsets any wrapper, saying which one on
  stderr. CCY 3.86.1, container 2.47 (Dockerfile label and `REQUIRED_CONTAINER_VERSION`),
  plus entries in `docs/ccy-changelog.md` and `docs/ccy.md`. `scripts/test-ccy-agent-bus.bash`
  gains four cases and a `+` mode (neither forwarded). Three of the cases were seen failing,
  then all 45 passed.
- DESIGN.md: the M2.3 bullet (D59), the `set-role` spelling (`--role`), the "to confirm"
  bullet (now confirmed; D60 made), and new decisions D59 and D60. PLAN.md's M2 line is
  updated.

## Verification (container)

- `test_u20_check.py` 66 OK; the other plan-folder tests (acceptance_check 49, deploy_check
  19, triage_probe 57, u01_probe 73) pass.
- `scripts/test-ccy-agent-bus.bash` 45/0, `test-ccy-lifecycle.bash` 214/0,
  `test-ccy-container-version-hook.bash` 73/0, `test-ccy-teams.bash` 13/0.
- shellcheck: clean on `acceptance.bash`, `_acceptance-u20.inc.bash`, `entrypoint.sh` and
  the test. ruff 0.16.8: clean.
- The bash include has no unit harness: it drives host processes, and no plan include has
  one. Its logic sits in `u20_check.py`, which is tested, and the include passed
  shellcheck and `bash -n`.

## Risks and things only the host run can show

- **The owner's interactive launch.** The 3.86.1 bump discards every saved
  `.last-launch.conf`, as any CCY version change does. meta-deploy runs deploy (which
  upgrades ccy) and then acceptance, so the first acceptance run stops at the U20
  prerequisites with an `OWNER:` line asking for one interactive `ccy` launch here. After
  that launch, run meta-deploy again.
- **SessionStart and the watcher.** Every session gets its orders before any watcher wait
  (D59), so it does not matter whether SessionStart fires before the first stream-json
  input.
- **What the host run must confirm:**
  - that haiku follows the orders under this checkout's `CLAUDE.md` and hooks daemon (else
    set `U20_MODEL=sonnet`);
  - that a background `pingbus wait` completing starts a turn in a headless stream-json
    session (M2.3);
  - the transcript shapes the checks rely on: `tool_result` content for `HISTORY` and "not
    found", a Stop hook's reason text somewhere in the transcript, `init.plugins[].name`.
    If any of these differs, the check fails naming what it looked for; it never passes
    falsely.
- **Concurrent ccy launches by the owner.** A plain `ccy` launched by the owner during M2.9
  fails that check ("another ccy session started meanwhile: run again"). Launches naming
  `@acceptance` are excluded by the owner prerequisite.
- **Leftover Claude Code files.** Only the session's transcript (and its sibling
  directory) is moved out of `.claude/ccy/projects/`. Other per-session files Claude Code
  writes under `.claude/ccy/` (git-ignored) stay. The design asks only for the transcript,
  and M2.10's checks do not see these files.
- **The idle input turn.** The Stop guard blocks for no waker at most once per window
  (`NO_WAKER_EVERY_S`). accc's first turn ends with a watcher live, so the block at the end
  of the idle turn is the first one.

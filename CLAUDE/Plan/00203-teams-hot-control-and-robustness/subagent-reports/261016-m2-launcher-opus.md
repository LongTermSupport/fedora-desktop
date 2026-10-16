# M2 build notes: the launcher and the entrypoint

Plan 00203, Task 2.3, milestone M2 of [SPEC-hot-control.md](../SPEC-hot-control.md). Branch
`feat/hot-control-m2-launcher`, from F44 at the spec's revision 2.

## What was built, by layer

- **Go, `ccy-teams internal membership-init <session> <checkout> <seats>`**
  (`src-go/internal/teams/membership`). It writes generation 1 of section 3.1's file
  into `$XDG_RUNTIME_DIR/ccy/membership/<session>/membership.json`. The directory and its two
  parents are 0700, checked with `Lstat` (a symlink is refused) and chmodded. The file is 0600,
  written atomically through `fsx.WriteAtomic`. It prints the directory on stdout.
  - The seat and team grammar is a copy of pingbus's, pinned by a test that reads
    `helpers/pingbus/protocol.py`.
  - The `internal` group is dispatched from `App.run`; `dispatch` was at the cyclop limit.
  - The usage text lists it beside `record-seats`.
  - `.golangci.yml` excludes tagliatelle for this one file, because snake_case is pingbus's
    wire format.
- **Launcher.** A new `lib/agent-bus-membership.bash` (25 code lines, 2 functions):
  - `ccy_no_bus_check` refuses `--no-bus` beside a team with 64. It runs right after parsing,
    and again for a Quick Launch team.
  - `ccy_membership_args` fills `CCY_SEAT_RUN_ARGS`. In ccy's tmux it adds
    `--label ccy-session=<name>`. Unless `--no-bus`, it calls membership-init and adds
    `-v <dir>:/run/ccy/membership:ro${CCY_MOUNT_RELABEL:+,Z}` and
    `-e PINGBUS_MEMBERSHIP=/run/ccy/membership`; otherwise it adds `PINGBUS_MEMBERSHIP=off`.
  - `ccy_seats_take` calls it first, so a membership failure or a `--no-bus` conflict comes
    before any sudo. It no longer adds `PINGBUS_SEATS` or `ccy-seats`.
  - `--no-bus` is parsed, documented in `--help` and in the did-you-mean list, and replayed on
    restore (`CCY_REGISTRY_KEEP_FLAGS` and Go `keepFlags`, pinned equal).
  - The play installs the new lib.
- **Entrypoint, the AGENT-BUS block.**
  - `off` adds nothing.
  - `/run/ccy/membership` checks the zipapp, the plugin and `membership.json`. It then links
    pingbus, passes `--plugin-dir` and `--settings /tmp/pingbus-home/settings.json`, and puts
    `pingbus seat exec --` in front.
  - It refuses an unset or unknown marker, and a leftover `PINGBUS_SEATS`, `PINGBUS_TEAMS` or
    `PINGBUS_HOME`, naming each as "ccy on the host is older than this image".
  - D62's ccy.env snapshot already covers `PINGBUS_MEMBERSHIP`, as every `PINGBUS_*` variable.
- **pingbus (the minimum M2 needs, in `seat.py` and `cli.py`).**
  - `parse_membership`: section 3.1 exactly, version 1. Any refusal is a ConfigError (78).
  - `_launch_seats` reads `PINGBUS_MEMBERSHIP` when it is set. It refuses `off`, a relative
    path, a missing file, and both variables set at once. `PINGBUS_SEATS` stays for `cc --teams`
    until M6.
  - An empty membership is valid. The home is made and nothing is held. There is no
    `PINGBUS_TEAMS`, and no forge check, so a plain session in a project without a gh login
    still starts.
  - Under the membership, the claim writes three files into the home, each 0600 and `O_EXCL`:
    - `settings.json`: `crossSessionInbound: accept`, and the status line guard;
    - `statusline-inner`: the first `statusLine` of settings.local, then the project's, then
      the user's;
    - `active`, when a seat is held.
  - The guard's interval is 1 s when the session is seated, and the inner line's own when it
    is not. A malformed settings file is refused (78) before any lock.
- **Plugin `hooks.json`.**
  - Every hook is `[ -z "$PINGBUS_MEMBERSHIP" ] || [ -e "$PINGBUS_HOME/active" ] || exit 0; exec ...`.
    So `cc --teams` and `agent-bus-claude` are unchanged.
  - SessionStart's inert branch writes `socket.json` with `jq ... env`, so the token never
    goes on argv.
  - Plugin 1.3.0.
- **Versions.** CCY 3.99.0, container 2.67 (Dockerfile label and `REQUIRED_CONTAINER_VERSION`),
  and the changelog entry.
- **Bash limits.** The allowlisted entrypoint and launcher did not grow.
  - The entrypoint is at 443/444, against its 443/448 record. Its refusals became `printf`s,
    and leftover variables fold into the case.
  - The launcher is at 2823/2745, against its 2825/2747 record. The SSH-key label loop became
    one expansion, `--version` became one `printf`, and the unattended flag became
    `${HEADLESS_MODE/false/$CCY_UNATTENDED_LAUNCH}`.
  - `agent-bus-seats.bash` stays at 57 code lines and 4 functions.

## The H34 spike

Run by a sub-agent in this container on Claude Code 2.1.301, in a real interactive session in
tmux, with no prompt sent:

- A `--settings` status line ran each second.
- An atomic replace, an in-place rewrite and a removal of `statusLine` were each ignored for
  20-30 s.
- A file with no status line at start never gained one.

**Verdict: no re-read.** The spec's fallback is built: inbound is accepted from launch, and the
guard is in place from launch. The spec records the consequence for M3: the status line's
`refreshInterval` is fixed at launch too.

## Tests (red, then green)

- **Go.** `membership_test.go`: red was `undefined: Init`. The app tests for `internal`, and
  `FlagClassAgreesWithBash` with `--no-bus`, were each red before the code.
- **pingbus.**
  - `test_seat.py`: `ParseMembershipTest`, `MembershipClaimTest` and `StatusLineGuardTest`,
    17 errors red, then green.
  - `test_plugin_contract.py`: the inert, seated and no-marker runs of every hook, and the
    socket record, 8 failures red, then green.
- **Bash suites**, all green:
  - `scripts/test-ccy-agent-bus.bash`: 77 checks, including the unset and unknown marker, a
    leftover `PINGBUS_SEATS`, a missing `membership.json`, and the real zipapp replay seated and
    plain, with the settings and flag.
  - `scripts/test-ccy-teams.bash`: 185 checks, covering the label, the mount with and without
    the relabel, an unnamed launch, `--no-bus`, a membership-init failure, and the wiring.
  - `scripts/test-cc-teams.bash`: 69 checks.
  - `scripts/test-ccy-session-registry.bash`: 253 checks.

## For the coordinator

- **M1 overlap.** M2 built a strict 3.1 reader (`seat.parse_membership`) and an empty
  membership in `seat exec`. Both are on M1's list. M1 should build on them, not beside them.
  M1's keeper then takes over the `active` flag, and the settings stay a launch-time write.
- **The keeper hook point.** `claim()` is where `seat exec` would fork `pingbus seat keep`.
  Today the locks are still inheritable and held by claude's tree, as before.
- **M0 conflicts are expected** in `hooks.json` (every command now carries the guard prefix)
  and in the plugin version.
- **Older plans' host acceptance scripts are now stale.** Plan 00201's `_acceptance-m7.inc.bash`
  and Plan 00161's `_acceptance-u20.inc.bash` find their containers by the retired `ccy-seats`
  label. 00201's P1 also expects no `PINGBUS_` variable on a plain container, which now has
  `PINGBUS_MEMBERSHIP`. Both have passed on the host already. They will fail if run again
  until they read the membership instead.
- **Not done in M2.**
  - The age restart reading the record (H5) is M4.
  - The status line `bus:` from `applied.json` (H14) is M3.
  - qa-all and the qa-reviewer agent were not run, as sub-agents may not.

## After the Fable review (FIX-FIRST)

The review is `261016-m2-review-fable.md`.

- **Inbound.** A session launched on no team is now launched without `crossSessionInbound`:
  `seat.session_settings` sets it only when the session is seated. So the session has no inbox
  socket, and Decision 1 holds.
  - Its first join relaunches it once through the restart path. The spec's H22 states this, and
    so does the ccy-teams per-step report line M3 must print.
  - The keeper's report gains `inbound`.
  - SessionStart's inert branch no longer writes `socket.json`, since there is no socket. It is
    now the same guard as every other hook, which also removes `jq` from the hooks.
- **Socket path, probed here on Claude Code 2.1.301.** The socket is
  `$XDG_RUNTIME_DIR/cc-socks/<pid>.sock`, on the container's own overlay root. It is not under
  `/root/.claude` and not on any mount, so a sibling container of the same checkout cannot reach
  it. Recorded in the spec's H34.
- **Settings that are not JSON.** They no longer refuse the launch. The claim names the file on
  stderr (⚠) and launches with no `statusLine` of its own, so Claude Code's own status line
  applies and there is no bus segment. This holds for seated and plain sessions alike.
- **Versions.**
  - F44 (M0) is merged in.
  - CCY 3.99.1 and container 2.68. The version line, the label and `REQUIRED_CONTAINER_VERSION`
    are the conflicts, resolved.
  - The changelog keeps both entries.
- **Plan 00201 and Plan 00161 acceptances** are ported.
  - `m7_seat_listing` and `u20_membership_listing` build `ID<TAB>seats` from each ccy
    container's `/run/ccy/membership` mount source, reading the host's `membership.json`. This
    replaces the `ccy-seats` label, and it works for unlabelled headless launches too.
  - `expect-plain` allows exactly `PINGBUS_MEMBERSHIP=off`. Its test was red first, then green.
  - Both are queued in meta-deploy after M2's acceptance, as `<plan>/acceptance.bash` entries,
    so 00161's `deploy.bash` does not run.
- **acceptance.bash** is now the robustness branch's dispatcher shape: `check_leg`, `REPORT`,
  and one literal `source` per milestone.
  - The coordinator's merge adds the `_acceptance-robust.inc.bash` source and its
    `acceptance_robust` call beside M2's.
  - `_acceptance-m2.inc.bash` starts its own throwaway sessions, `m2-plain-<pid>` and
    `m2-nobus-<pid>`. They run with `tmux -L ccy new-session -d` and `ccy --headless
    --no-restore --no-supervise`, with stdin a fifo held open, so no turn is sent.
  - It cleans up on any exit: tmux, containers, registry records, membership directories and
    transcripts.
  - It no longer picks the owner's live sessions. Check (a) is covered by the ported 00201 and
    00161 runs.
- **`--no-bus` restored with a saved Quick Launch team.** This cannot reach the 64 refusal. A
  restore, a restart and a headless launch never take the saved team: the existing
  test-ccy-teams cases "a session restore never takes the saved team" and "headless, a saved
  team: not launched into" pin it.
- **Smaller fixes.**
  - The Dockerfile comment says the kit's `settings.json` is agent-bus-claude's.
  - A membership test refuses the session names `..` and `../s`.
  - The "differ only by the mount" sentence is corrected in H33b, docs/ccy.md and `--help`.
  - The spec section 6 says M1 builds on `parse_membership` and the claim's session files.

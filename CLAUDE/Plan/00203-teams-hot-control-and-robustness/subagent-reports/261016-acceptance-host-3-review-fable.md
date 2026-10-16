# Review: fix/acceptance-host-3 at 2cee2dfe (three host acceptance failures of F44 9367bf64)

Verdict: **MERGE**. No blocking finding. Four low notes at the end, none of which needs to hold
the merge.

Read-only review of the branch against F44 3420e48a. I read the installed Claude Code 2.1.301
bundle (the `bin/claude.exe` single-file build) for every product claim, inspected a live seated
ccy session in a CCY container, and ran the branch's targeted tests as modules
(`test_m2_inbound` 8, `test_m7_check` 45, `test_u20_check` 79, `tests.helpers.pingbus.test_seat`
105, `scripts/test-ccy-agent-bus.bash` 78 checks): all green. No qa-all, no commit.

## 1. Plan 00201 M3: a test fault, confirmed

The claim: the bare `podman exec` lacked `PINGBUS_HOME`, which `pingbus seat exec` gives claude,
so `pingbus status-line` printed "membership is unreadable".

- `cli.cmd_seat_exec` (helpers/pingbus/cli.py:2379-2391) sets `child_env["PINGBUS_HOME"]` and
  `execvpe`s claude; the entrypoint refuses a launcher that passes it (entrypoint.sh:578). So the
  variable exists only in claude's process tree, never in a `podman exec`.
- **Does the status line inherit it?** Yes. In the bundle, the status line is run by `jyr(...)`
  through `_H(...)`, the same executor the hooks use, whose child env is
  `{...rs(), ..., CLAUDE_PROJECT_DIR}` where `rs()` is `process.env`. So the status-line command
  gets everything claude has, `PINGBUS_HOME` included, exactly as the SessionStart hook did in the
  failing run's capture.
- **Live check.** In a CCY container running 2.1.301, the claude process's `/proc/<pid>/environ`
  carries `PINGBUS_HOME=/tmp/pingbus-home` and its `/proc/<pid>/cmdline` is
  `claude --plugin-dir … --settings <file> --dangerously-skip-permissions --resume`. The bundle
  sets `process.title="claude"`, but Bun does not rewrite argv memory as Node does, so the
  `--settings` scan in `m7_check._claude_processes` and `m2_inbound._claudes` sees the real
  arguments. The entrypoint passes the literal `--settings /tmp/pingbus-home/settings.json`
  (entrypoint.sh:595), which is what `m2_inbound.py` compares against.
- **Other readers.** `config.resolve_home` (status, send, say, recv, wait, watch, the hooks) takes
  `PINGBUS_HOME` from the inherited environment; the keeper sets it when it spawns the watcher
  (seat.py:691). None runs from a bare exec in production. Nothing else breaks.
- The new `expect-session-settings` checks the file claude is actually launched with rather than
  the kit's `/opt/.../settings.json`, which only `agent-bus-claude` uses; a seated session must
  say `accept` and name the guard. Correct.

## 2. Plan 00203 M2 (b): the spec was wrong, the product change is right

**Is the Claude Code reading right?** Yes, from the bundle:

- Resolution (`I()`): iterate `["policySettings","flagSettings","userSettings"]`, first one that
  sets `crossSessionInbound` wins; then `localSettings`/`projectSettings` may only move it to a
  stricter value (`p[s] > p[e ?? "accept"]`); a schema warning on the key forces `hold`
  (`invalidSetting`). So **"beats user settings" is correct**: `--settings` is `flagSettings`,
  which is read before `userSettings`, and `refuse` is the strictest value so nothing below can
  change it. Only managed policy outranks it, which `m2_inbound.py` checks
  (`/etc/claude-code/managed-settings.json` and the `managed-settings.d/` drop-ins).
- Default when unset (`H()`): `bypass-permissions` mode gives `{policy:"hold", holdCause:"bypass-default"}`;
  a prompting session gives `accept`. Per message (`E()`) there are also the parity holds
  (`mode-mismatch`, `no-mode-asserted`). A held message in a headless session expires. So the
  old plain session **did** have a socket and **did** hold messages for review: the spec's
  "binds no inbox socket" was wrong, as the branch now says.
- `refuse` gives `{policy:"refuse", refuseCause:"opt-out"}` and the message is dropped with a
  receipt to the sender; never a turn, never a dialog.
- The socket: bound in every session (the live seated session owns `cc-socks/<pid>.sock`); the
  only "refusing to bind" path is a sockets-directory setup failure (`[uds-messaging] Failed to set up sockets directory …`), not a setting. Reporting rather than judging the socket is right.

**Does `refuse` break the first-join relaunch (H22)?** No.

- The launch keeper publishes `inbound = bool(self._held)` (seat.py:602-603), so a plain
  session's applied file says `inbound: false`. `add.go:217-225` reads `rep.Inbound`
  (falling back to "had seats before") and calls `relaunch`, which writes `relaunch.json`.
- `ccy_lifecycle.py:313-338` sees the file and returns `ExitForRestart`; the restart replays
  the record, the membership file now names the seat, `cmd_seat_exec` gets `seats` non-empty,
  `write_session_files(..., seated=True)` writes `accept`, and the launch removes the request
  (`membership.go:132`). `add.go:286` then requires `rep.Inbound == true` to mark done. The
  Repair keeper's `_settings_inbound` (`== "accept"`) is `False` for `refuse`, as it was for
  the old unset value. Unchanged semantics throughout.

**`--no-bus`, `cc`, the owner's plain sessions.**

- `--no-bus` launches with no settings file, so Claude Code's default applies: in bypass mode a
  message is held for review (or expires headless). H33b now says exactly this. Unchanged.
- `cc --teams` on the host uses the `LaunchSeats` source; `write_session_files` runs only
  `if from_membership` (cli.py:2375), so `cc` never writes these settings. Unchanged
  (`test-cc-teams.bash:180` still passes).
- The one real behaviour change for a plain ccy session: an explicit `refuse` also drops
  **self-sent** peer messages (a child process of the session writing to its parent's socket),
  which the unset default accepted before the mode check (`E()`: `if(e?.selfSent) return accept`
  runs only when no setting is present), and `b0e()==="refuse"` revokes the session's idle
  notices to peers that subscribed. Nothing in this repository relies on either in a plain
  session (the watcher runs only with a seat; the hooks-daemon uses hook stdout). This is what
  Decision 1 asked for, and the old state (hold with an approvable dialog) was further from it
  than the spec admitted. See note L3 for a one-line doc suggestion.

**The check.** `m2-check plain` runs `m2_inbound.py inbound /proc /etc/claude-code /tmp/pingbus-home/settings.json` in the container. The sessions are started in this checkout
(`tmux … -c "${PLAN_REPO_ROOT}"`, `_acceptance-m2.inc.bash:81`), which ccy mounts at
`/workspace`, so the `/workspace/CLAUDE/Plan/<plan>/m2_inbound.py` path resolves. It proves the
launch argv, the file's value, and the absence of a looser managed policy; it rightly does not
claim to see server-managed settings. The developer's rejection of a wire-level delivery proof
is sound: a refused message is acknowledged only to the sender, so the only observable would be
pane scraping.

## 3. Plan 00161 M2.3: a test fault, and the right fix

The transcript showed the hooks did their part (`watcher_failed` on the idle turn; the Stop hook
blocked twice with the same text). The old idle order "run no command" contradicted rule 3, and
the model obeyed the more specific instruction. The new order (u20_check.py:163-166) says
"nothing to do but what your rules ask for", names rule 3 for the two hook phrases
(`hooks.py:108` "nothing will wake this session", `hooks.py:142` "the watcher could not be
started"), and otherwise "run nothing". `test_idle_orders_give_nothing_to_do` pins both.

M2.3 is still sound: the product facts are asserted independently of the model's prose —
`expect-hook-text … watcher_failed` on the transcript and `pingbus status` reporting
`wake=waiter` (`_acceptance-u20.inc.bash:808-809`), then a real review from member a waking the
session. Naming the condition in the orders is harness instruction, not a product shortcut: the
model still has to start `pingbus wait` and the waiter still has to wake it. DESIGN.md says why.

## Versions, queue, names

- **Versions.** CCY `3.99.8.1`, container `2.73.1`, label and `REQUIRED_CONTAINER_VERSION`
  equal. The pre-commit gate compares the strings exactly (`pre-commit:271`) and requires a
  container bump for an image input (`helpers/pingbus/seat.py` is one; bumped). The launcher's
  `validate_container_version` (common.bash:953) and `m2-check image` (m2-check.bash:176) are
  string comparisons, so a three-part version is accepted and forces one rebuild. Changelog entry
  present; the launcher's version comment follows the one-line rule.
- **meta-deploy.list.** Five entries, each valid under `ENTRY_RE`/`UNIT_RE`:
  `playbooks/imports/play-claude-yolo.yml`, `playbooks/imports/play-agent-bus.yml`,
  `00203-teams-hot-control-and-robustness` (acceptance only; the plan has no `deploy.bash`, so
  the plays are not duplicated), `00201-…/acceptance.bash`, `00161-…/acceptance.bash`. Order is
  by dependency. Each has its description comment. Exact.
- **Install-specific names.** Scanned every added line for usernames, hostnames, paths,
  container/session names, addresses: none. Throwaway team names (`acc-m3a`, `acc-robust`,
  `accept201a`) are the plan's fixtures.

## Low notes (not blocking)

- **L1** `scripts/test-ccy-lifecycle.bash:485-486` reduces both versions with
  `grep -oE '[0-9]+\.[0-9]+'`, which turns `2.73.1` into `2.73`. The check passes, but it would
  also pass `2.73.1` against `2.73.2`. The pre-commit hook is the real gate and compares exactly.
  Worth tightening to `[0-9.]+` when that file is next touched.
- **L2** `ACCEPTANCE.md` row (d) still says "REQUIRED_CONTAINER_VERSION (2.71)". Pre-existing
  drift, now two bumps stale; the check itself reads the launcher so it is correct.
- **L3** `docs/ccy.md` could say in one sentence that a plain ccy session drops Claude Code's own
  cross-session messages too (including from its own child processes), while `--no-bus` keeps
  Claude Code's default of holding them for review. The spec (H33b, H34) has this; the user doc
  only implies it.
- **L4** Both `/proc` scans require exactly one claude started with `--settings`. A Bash tool
  child running `claude … --settings` during the probe would fail the leg. Acceptable for idle
  headless sessions; the failure message names what it found, so a flake would be obvious.

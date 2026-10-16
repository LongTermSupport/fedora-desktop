# Acceptance host run 3 (F44 9367bf64): three failures, root causes and fixes

Branch `fix/acceptance-host-3`, from F44 3420e48a. CCY 3.99.8.1, container 2.73.1.

## 1. Plan 00201 M3: "this session's membership is unreadable"

**Root cause: the check, not the product.** The leg ran `podman exec <c> env ... pingbus status-line`. A `podman exec` carries the container's environment, which has
`PINGBUS_MEMBERSHIP=/run/ccy/membership` (M2) but not `PINGBUS_HOME`: that variable is set only
for claude, by `pingbus seat exec` (`cli.cmd_seat_exec`, `child_env["PINGBUS_HOME"]`), and the
entrypoint refuses a launcher that passes it. M3 (c60b7ff0) made `statusline.membership_teams`
read the applied file from `PINGBUS_HOME`, so from the bare exec it returned None and printed
`NO_MEMBERSHIP`. Before M3 the segment came from the session record, which needs no home, which
is why the same leg passed at 7fd72106.

The real status line is fine. claude is launched with `--settings /tmp/pingbus-home/settings.json`,
whose command is the H34 guard `[ -e "$PINGBUS_HOME/active" ] || exec sh ...; exec pingbus status-line`, run as claude's child with `PINGBUS_HOME` set. The same capture shows claude's hooks
had it: the SessionStart hook, guarded on `$PINGBUS_HOME/active`, reported both teams. The
membership format, permissions and parser were checked against `membership.write_applied`
(the statusline tests already write through it). The leg also checked the kit's
`/opt/claude-yolo/optional/agent-bus/settings.json`, which only `agent-bus-claude` uses, not the
settings a ccy session is started with.

Other readers: `config.active_teams` (status, send, say, recv, wait, watch) and the hooks resolve
the home with `config.resolve_home`, and get `PINGBUS_HOME` the same way: inherited from claude,
or set by the keeper when it spawns the watcher (`seat._wake`). The fault is not in any of them.

**Fix (test):** `m7_check.py session-status-line /proc`, run in the container, finds the one
claude started with `--settings`, reads the status line command from that file, and runs it
through `/bin/sh -c` with claude's own environment and working directory read from /proc and the
payload on stdin, as Claude Code does. It prints only the status line, never the environment.
`expect-session-settings` replaces `expect-kit-status-line`: the session's settings accept inbound
and name the guard. Tests (red, then green): `SessionStatusLineTest`, `test_expect_session_settings`.

## 2. Plan 00203 M2 (b): "the plain session has an inbox socket"

**Root cause: the spec was wrong.** Read from the Claude Code 2.1.301 binary:

- The socket path is `$XDG_RUNTIME_DIR/cc-socks/<pid>.sock` (`/tmp/cc-socks-<uid>/` if too long),
  derived from the pid and bound in every session. The host saw the same: every claude process
  has one.
- `crossSessionInbound` is `accept`, `hold` or `refuse`. It is taken from the first of policy
  (managed), flag (`--settings`) and user settings that sets it. Local and project settings may
  only tighten it.
- Unset: a session that bypasses permissions holds a message for review (`bypass-default`); one
  that prompts accepts it. A held message in a headless session expires.

So the plain session, launched with no setting, did have a socket, which held messages for
review. SPEC H34's "binds no inbox socket" (and H22, H33b, PLAN, ACCEPTANCE (b), docs/ccy.md and
an add.go comment) are corrected.

**Fix (product, container 2.73.1):** `seat.session_settings` now writes
`crossSessionInbound: refuse` for a session launched on no team (it wrote nothing before). A
refused message is dropped, never a turn and never a review dialog the human could approve. As a
`--settings` value, it also outranks the user's settings, which live in the shared checkout
that the session can write. The keeper's `_settings_inbound` still tests `== "accept"`, so the
first-join relaunch is unchanged. Tests: `test_a_session_launched_on_no_team_refuses_inbound` and
the keeper-later test (both red first), and the `scripts/test-ccy-agent-bus.bash` plain-session check.

**Fix (check):** `m2-check plain` (b) runs the new `m2_inbound.py inbound /proc /etc/claude-code /tmp/pingbus-home/settings.json` in the container. It requires exactly one claude, started with
`--settings` naming the session's file, and that file saying `refuse`. It also requires that no
managed settings (`managed-settings.json` or a `managed-settings.d/` drop-in) set anything looser.
The socket is listed for information only. Server-managed settings cannot be seen from inside;
ccy signs in with a personal token. A live delivery proof was rejected: the socket answers
nothing on the wire, so "refused" could only be seen by scraping the pane or a debug log.
Tests: `test_m2_inbound.py`.

## 3. Plan 00161 M2.3: neither watcher nor waiter within 240 s

**Root cause: the test.** In the capture (u20/accc/transcript.jsonl) the product did what E26
says. The supervisor was started at SessionStart (04:40:51) and stopped within 300 s, so on the
idle turn (04:41:44) the UserPromptSubmit hook added `watcher_failed` ("the watcher could not be
started ... meanwhile run `pingbus wait` with run_in_background"). The Stop hook then blocked
with the same text, twice. The idle input said "run no command", and the model obeyed it over
the hook, and said so. So no waiter was armed. The hook revive works on image 2.73.

**Fix (test):** the `idle` orders now give nothing to do but what the rules ask for, naming
rule 3 for a hook that says the watcher could not be started or that nothing will wake the
session. Test `test_idle_orders_give_nothing_to_do` (red, then green). DESIGN.md M2.3 notes why.

## Versions and queue

CCY 3.99.8.1 and container 2.73.1 (the gate compares the strings and accepts the format). The
changelog entry is in docs/ccy-changelog.md. Queue: play-claude-yolo.yml, play-agent-bus.yml
(same pingbus source on the host; no behaviour change for host members), the
00203 acceptance, the 00201 acceptance.bash, the 00161 acceptance.bash.

## QA

qa-changed passed (23 files, every named suite). Ran by hand: test_m7_check.py,
test_u20_check.py, test_m2_inbound.py, tests.helpers.pingbus test_seat, test_plugin_contract,
test_statusline, test_cli, and scripts/test-ccy-agent-bus.bash. qa-all was not run, as instructed.

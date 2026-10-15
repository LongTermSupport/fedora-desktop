# Plan 00201: the final qa-reviewer's seven findings, fixed (Opus)

Source: `261015-final-qa-reviewer.md`. Branch `fix/201-qa-review`, CCY 3.98.7 / container 2.62
(above the pingbus short-name fix's 3.98.6 / 2.61, merged in from F44).

## 1. `bus: A,B` in any checkout

Route: the member kit's `settings.json` sets `"statusLine": {"type": "command", "command": "pingbus status-line"}`. Every bus launch (ccy's entrypoint, `cc --teams`,
`agent-bus-claude`) already passes that file with `--settings`, and command-line settings
outrank project and user settings, so it applies in any checkout without a hooks-daemon
release. `pingbus status-line` (`helpers/pingbus/statusline.py`) runs the status line the
session would otherwise have had (project `settings.local.json`, project `settings.json`, then
the user's `settings.json`; never itself), with the same stdin and exit status, and appends
`| bus: A,B` from the per-session record. The fedora-desktop hooks-daemon handler and its
registration are removed; in this repo the daemon's status line is the inner line.

Rejected: an upstream hooks-daemon handler (needs a release and covers only daemon projects);
a plugin-contributed segment (plugins cannot set a status line).

Known limit: the inner status line's `refreshInterval` is not carried over (Claude Code takes
the `statusLine` object from the winning settings). Documented in docs/agent-bus.md. Whether
Claude Code honours `statusLine` from `--settings` is verified live by owner steps 05 and 09
(not testable in a headless run); M3 of acceptance.bash checks the kit settings and the
command's output inside the session's container.

## 2. Tests in a QA gate

The handler's tests are gone with it. The replacement's tests are
`tests/helpers/pingbus/test_statusline.py` (21 tests), run by the helper-tests gate that the Go
qa routing already selects for `helpers/pingbus/` changes (`qa-changed` ran it as
`unit-tests`).

## 3. A failed record is visible

SessionStart adds the fixed line `record_failed` to the session's context (besides its stderr
line), and `pingbus status-line` shows `bus: no record of this session's teams (pingbus status)`
in red when `PINGBUS_TEAMS` is set and no usable record exists. Nothing is shown only for a
session on no team.

## 4-6. Spec, config, milestones

S-C6 and Q7 rewritten to the kit route; Q6 and S-C8 say `--re` inference is `send` only (`say`
has no `--re`, and `recv`'s reply command for `say` already names its team), matching the
code and the protocol doc. The `.claude/hooks-daemon.yaml` comment went with the handler.
PLAN.md milestones add 3a4014b0 and 43d6b861; this work is Task 2.9.

## 7. `cc --teams` makes nothing before consent

`cc_teams_ccy_dir` now runs after `cc_teams_choose`; a declined consent leaves no
`.claude/ccy/` (new test in `scripts/test-cc-teams.bash`). The symlink refusal still comes
before any seat is taken.

## Deploy

Queued in `CLAUDE/Plan/meta-deploy.list`: `play-claude-yolo.yml` (ccy 3.98.7, image 2.62 with
the kit settings and pingbus, cc-teams.bash), `play-agent-bus.yml` (kit in
`/usr/local/share/agent-bus/kit`), then this plan's acceptance.bash (M3 changed). After merge the
running hooks daemon in this checkout needs a restart, since its config lost the bus-teams
plugin.

## Overlaps

- `fix/pingbus-short-name-reply` (merged to F44 before this branch): conflicts only in version
  lines, changelog, meta-deploy.list and PLAN.md/SPEC; resolved. No code overlap (it changed
  `cli.py` target refusals and `protocol.py`; this adds one command to `cli.py`).
- `feat/bus-inject-message`: spec only so far; it plans to change `cli.py`, `hooks.py`,
  `notify.py`, `envelope.py`. This branch touches `cli.py` (one import, one command
  registration, one handler) and `hooks.py` (`_session_teams_lines` and one template), so
  expect small textual merges there and in the version lines.

## Fable review fixes (`/workspace/untracked/scratch/261015-201-fixes-review-fable.md`)

Versions unchanged (3.98.7 / 2.62, unshipped). Tests first, each red before the fix.

- **F1:** `pingbus status-line` always exits 0, since Claude Code blanks the line on a non-zero
  exit. Unreadable settings show red `status line: <file> is not valid JSON`. An inner non-zero
  exit shows its output then red `(status line exit N)`. Both keep the bus segment.
- **F2:** the kit's `statusLine` carries `"refreshInterval": 1`, the project's value (a test
  pins it equal to `.claude/settings.json`'s), so the daemon's context sidecar stays fresh for
  `ccy --supervise` in an idle session. The docs no longer call the interval a known limit.
- **F4:** the inner runs in its own process group (`start_new_session`) with a 10 s timeout. On
  timeout, or when pingbus gets SIGTERM/SIGINT/SIGHUP, the whole group is SIGKILLed. Tests
  check that a background grandchild of the inner is gone in both cases.
- **F5:** `statusline.py` imports no other pingbus module. It holds the sessions-dir names, the
  session-id and team-name patterns and the input cap; `hooks.py` takes `sessions_dir` from it,
  and a test keeps the copies equal to `hooks`/`protocol`. The zipapp's `__main__` (generated
  by `bundle.py`, `FAST_COMMANDS`) runs `status-line` from `statusline.main()` without importing
  the cli; a bundle test checks with `-X importtime` that `helpers.pingbus.cli` is not loaded.
  Measured with a freshly built zipapp, record present, no inner status line, 21 runs:
  **before 127 ms median (122 min, 140 max); after 23 ms median (19 min, 39 max)**. A bare
  `python3 -c pass` is 8 ms on the same machine.

## QA

`qa-changed.bash` passed before the merge (26 files; helper unit tests, test-cc-teams 59,
test-ccy-agent-bus 59, test-agent-bus-install 283, and the ccy suites). After the merge the
pingbus modules were re-run and pass.

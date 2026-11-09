# Plan 00201 final-review fixes (fix/201-qa-review, tip b1848447): adversarial review

Verdict: **FIX FIRST** (two small defects in the status-line route; everything else holds).

Base: F44 local HEAD 158855fb is the merge-base, so `F44...origin/fix/201-qa-review` is the
two commits 6cbef1fc + b1848447; 28 files. Tests re-run in a detached worktree of the tip:
`test_statusline`, `test_hooks`, `test_plugin_contract` (148 tests, OK), `test_m7_check.py`
(41, OK).

## Findings

### F1 (fix first) — a broken settings file is reported with exit 1, which blanks the line

`statusline.render` returns status 1 for `SettingsError` (project or user settings.json
unreadable / not JSON), with the red `status line: ... is not valid JSON` text, and
`test_unreadable_settings_are_shown_not_skipped` enshrines `rc == 1`. Claude Code's status
line doc (Script errors or hangs): "Scripts that exit with non-zero codes or produce no output
cause the status line to go blank." So the red text, and the `bus: A,B` segment beside it,
are never shown: the "shown, not skipped" intent is defeated by its own exit code. Return 0
in that branch (the text is the report) and flip the test. The same consideration applies,
more mildly, to a failing inner command: pingbus faithfully propagates the inner's non-zero
status, so an inner failure blanks the bus segment too (previously it blanked the same line,
so this is parity, not a regression; a red marker with exit 0 would be better but is optional).

### F2 (fix first) — the lost `refreshInterval` is not a cosmetic limit here

`.claude/settings.json` in this repo sets `"refreshInterval": 1` on the daemon's status line.
The report and docs/agent-bus.md call its loss a "known limit". It is more than that for ccy
`--supervise` bus sessions in this checkout: `.claude/ccy/claude-supervise.py` (lines
1104-1127) decides `/compact` from the daemon's context sidecar, which the daemon's status-line
handler writes on every render, with a 30 s freshness window and a margin "sized at roughly
one status refresh interval". Claude Code's event-driven triggers "can go quiet when the main
session is idle, for example while a coordinator waits on background subagents" (the bus
coordinator's normal state). With the kit's statusLine object winning whole, the sidecar goes
stale while idle and the supervisor sits on `sidecar stale`. Fix: put `"refreshInterval": 1`
in the kit's `statusLine` (every bus session then re-renders each second; see F5 for the cost),
or document the supervisor consequence explicitly if that is accepted. The acceptance does not
exercise this (headless, no supervisor), so nothing would catch it.

### F3 (low) — trust model: a hair wider than Claude Code's, acceptable

`inner_command` reads `statusLine.command` from the project's `.claude/settings.local.json`,
`.claude/settings.json`, then the user's, and runs it through `/bin/sh -c` with the full
environment and the stdin payload. Claude Code would run exactly that command itself without
the kit, under the same precedence (settings doc: command line applies "above your user,
project, and local files and below managed settings"; local above shared project above user),
so the code being run is code Claude Code already trusts. Differences: (a) the status line doc
says Claude Code runs `statusLine` "under the same workspace trust rule as hooks" and skips it
until trust is accepted; pingbus has no trust check, but it only runs once Claude Code chose
to run the kit's statusLine, which is itself behind that gate, so the window is nil in
practice (ccy runs with bypass; cc shows the trust dialog before any render). (b) a managed
`statusLine`, or `disableAllHooks`/`allowManagedHooksOnly` set in a lower file, would make
Claude Code run something else or nothing, while pingbus would still run the project's; not a
concern for this project's hosts. Recursion is guarded (`PINGBUS_STATUS_LINE_INNER`, and the
kit command itself is passed over). Passthrough of stdin is byte-exact (same `raw`). The
`--settings` override claim is confirmed by the settings doc text quoted above.

### F4 (low) — no timeout; a cancelled pingbus orphans the inner

Claude Code has no status-line timeout; "if a new update triggers while a slow script is
running, the in-flight script is cancelled". That cancels `pingbus`, not its `/bin/sh -c`
child, so a hung inner keeps running (and a steady stream of updates could accumulate hung
children). Before, Claude Code cancelled the inner directly. Low: this repo's inner is the
daemon status line, which is fast. Starting the inner in its own process group and killing it
on SIGTERM, or a generous `timeout=`, would close it.

### F5 (info) — cost per render

`import helpers.pingbus.cli` is ~60 ms on importtime here (registry, forge, http.client,
protocol) plus interpreter start: roughly 100 ms per render, times the inner. Fine
event-driven; with F2's `refreshInterval: 1` it is ~10% of a core per bus session for the
pingbus part alone. A lazy import of the heavy modules in `cli.py` (status-line needs only
`hooks` and `protocol`) would cut most of it. Not blocking.

### F6 (ok) — failure modes

Record missing vs broken: `session_teams` returns None for no file, wrong owner/mode, symlink,
oversize, non-ASCII, duplicates, bad names, and `render` then shows the red `bus: no record ... (pingbus status)` whenever `PINGBUS_TEAMS` is set; nothing when unset. SessionStart appends
`record_failed` to the context when the write raised (test added) and still writes stderr.
One gap: a payload with no usable `session_id` writes no record and says nothing in context
(`_session_teams_lines` only reports an `OSError`), while the status line shows red; edge
case, both end red, acceptable.

### F7 (ok) — finding 7, `cc --teams` creates nothing before consent

`cc_teams_ccy_dir` now follows `cc_teams_choose` (`cc-teams.bash:163-164`). `cc_teams_choose`
calls only `ccy-teams choose-teams` and `agent-bus seat check`, neither needing `.claude/ccy/`.
New checks in `scripts/test-cc-teams.bash:283-298`: declined consent leaves no `.claude/ccy/`
in a fresh repo (exit 1), and a symlinked `.claude/ccy` is still refused (78) before `seat take`. Report says test-cc-teams ran 59 checks.

### F8 (ok) — QA gate, versions, deploy list, docs, leaks

- `tests/helpers/pingbus/test_statusline.py` is picked up by the helper-tests gate
  (`scripts/qa-gates.tsv:42`, routing `@py -> unit-tests`, `qa-routing.tsv:150`); the deleted
  daemon handler tests had no gate, so this is a net gain. No reference to `bus_teams`,
  `BusTeamsHandler` or `handlers/status_line` remains outside reports/journals; the
  `.claude/hooks/handlers/status_line/` tree is gone from the tip.
- Versions: `CCY_VERSION="3.98.7"`, `REQUIRED_CONTAINER_VERSION="2.62"`, Dockerfile LABEL
  2.62, changelog entry 3.98.7/2.62; no stray 2.61/3.98.6 in files/, scripts/, tests/, docs.
- meta-deploy.list: `playbooks/imports/play-claude-yolo.yml`, `playbooks/imports/play-agent-bus.yml`
  and the bare plan folder (acceptance.bash is executable, which `meta-deploy.bash:84` accepts)
  each have an accurate comment. Not in the list and rightly so (container-side): the hooks
  daemon in this checkout must be restarted after merge because its config lost the plugin;
  the report says so, the handover message to the owner must too.
- cc --teams on the host: the kit settings are merged (`jq -s '.[0] * .[1]'`) so statusLine
  carries over; `pingbus` is on the host PATH (`/usr/local/bin/pingbus` -> zipapp,
  `test-agent-bus-install.bash:425`), the same way the plugin's `pingbus hook ...` commands
  already rely on it. In the image the entrypoint links pingbus only for bus launches, so a
  plain ccy session has neither the kit settings nor the command: unchanged status line.
- Docs: agent-bus.md "A session's status line", agent-bus-protocol.md, ccy.md and
  ACCEPTANCE-owner.md steps 05/08/09 match the code (modulo F1/F2 wording). SPEC S-C6/S-C8/Q6/Q7
  revised in place with "(revised after review)". PLAN Task 2.9 added, Task 2.7 notes the
  rerun. No install-specific names in the diff (placeholders A/B, example paths only).

## Not verified here

Whether Claude Code honours `statusLine` from `--settings` live: doc-confirmed (above), owner
steps 05/09 are the live check. The acceptance M3 leg runs `pingbus status-line` directly in
the container, not through Claude Code, so it vouches for the command, not for the override.

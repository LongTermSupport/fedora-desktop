# Supervisor plugin API: structure map and design notes

Research for Plan 00146 by an opus subagent. Read against supervisor and daemon **v3.67.0**,
which is the newest upstream release. `S:` means `.claude/ccy/claude-supervise.py` (7073
lines; byte-identical to the daemon's bundled copy at
`.claude/hooks-daemon/.claude/ccy/claude-supervise.py`). `D:` means
`.claude/hooks-daemon/src/claude_code_hooks_daemon/`.

The issue text is
[UPSTREAM-REQUEST-supervisor-plugins-draft.md](../UPSTREAM-REQUEST-supervisor-plugins-draft.md).

## 1. Structure map

### Main loop and the idle choke point

- `main()` S:6929. It handles `--emit-model-switch` (S:6945), worker mode (S:6950), splits
  the child argv at `--` (S:6977), parses flags (S:6982), writes `supervisor-status.json`
  (S:6990), starts the policy worker (S:7002), then calls `supervise()` (S:7005).
- `supervise()` S:6510. It calls `pty.fork()` **once** (S:6616); the child calls
  `os.execvp(argv[0], argv)` (S:6622). Raw mode is set at S:6634. The tick closure is
  `_on_poll` (S:6642). Then `_forward_io` runs (S:6782). When it returns, the supervisor calls
  `os.waitpid` (S:6803) and returns the child's exit code (S:6812). There is no respawn path.
- `_forward_io()` S:6393. The select loop (S:6432) runs a monotonic tick (S:6447-6454), so a
  tick fires even while the child is streaming. Stdin passes through `CtrlCGate`, then
  `strip_suspend`, and is forwarded (S:6460-6491). The loop returns on master EOF
  (S:6501-6502), and that EOF is the only exit signal.
- The choke point is `can_inject = facts.idle and facts.input_line_empty` (S:4958). `idle`
  is the keystroke floor (`_is_idle` S:4817). Work-idle (S:4828) gates only the lower red band.

### Request families: detection, validation, priority

- `decide_once()` S:4873 is the "brain". It is pure with respect to the PTY and runs either
  in the worker or in-process. It reaps stale files (S:4908), reads the foreground sidecar
  (S:4913) and the compaction signal (S:4923), then runs the state machine (S:4980) for
  compact/continue/escape/resubmit.
- Every later family is gated on `payload is None and decision NOOP and signal_path is None and state MONITOR`. That is the "subordinate to compact/continue" rule. The cascade in
  order:
  01. operator signal (S:5091-5161);
  02. manual model switch (S:5162);
  03. the supervisor's own-line follow-up (S:5218);
  04. goal (S:5269);
  05. goal clear (S:5331);
  06. flag-compact (S:5375);
  07. model restore (S:5412);
  08. audit banner flush (S:5452);
  09. standing-auth (S:5535);
  10. session actions (S:5596).
- Validation lives in each loader:
  - `load_operator_signal` S:3061: a closed `kind` set (S:3011), positive-int `minutes`, and
    rendering from fixed templates (S:3028). It returns a 4-tuple, the reject reason included.
  - `load_goal_signal` S:2494, `load_goal_clear_signal` S:2426, `load_standing_auth_signal`
    S:2589, `load_session_actions_signal` S:2692, `load_model_switch_signal` S:2769,
    `load_model_downgrade_signal` S:2917.
  - Every loader scopes by `_session_in_scope` (S:1285) and a TTL.
- `Decision` enum S:1478: one hard-coded member per family. `TickOutcome` S:1630 carries
  per-family fields, and `_apply_post_injection_bookkeeping` S:5749 has per-family branches.

### Signal and request channel

- Everything is under `_daemon_untracked_dir()` S:1735: `{project}/.claude/hooks-daemon/ untracked`, or `{project}/untracked` on a self-install. Signals sit in its
  `context-sidecar/` (`_default_sidecar_dir` S:1712), as JSON files keyed by glob:
  - `*.compacting` S:1147;
  - `*.goal-intent` S:1166;
  - `*.goal-clear` S:2420;
  - `*.standing-auth-intent` S:2543;
  - `*.session-actions` S:2644;
  - `*.model-switch-intent` S:2746;
  - `*.model-downgrade` S:2855;
  - `*.operator-signal` S:2994.
- A signal is consumed only after a successful injection (`_consume_signal` S:4802, called
  from `_apply_decision` S:5736).
- Own-session identity comes from scanning `/proc/*/environ` for `CLAUDE_CODE_SESSION_ID`
  (`resolve_own_session_ids` S:1242, cached and union-only at S:1259). That gives a **set**,
  so it can be ambiguous. The CLI helper `_newest_sidecar_session_id` S:6864 picks the
  newest sidecar's id. The supervisor never reads Claude Code's `sessions/<pid>.json`.

### Config

- There is no config file: the supervisor is stdlib-only and imports nothing from the daemon
  (docstring S:8-13).
- CLI flags are only `--dry-run|--arm` and `--log` (`_parse_supervisor_flags` S:6823). An
  unknown flag is an argparse error, exit 2, as the reproduction below shows.
- Environment variables:
  - `CCY_MODEL_RESTORE_SECONDS` S:1367;
  - `CCY_MODEL_CONFIRM_ENTERS` S:1407;
  - `CCY_FLAG_COMPACT` S:1443;
  - `CCY_CTRL_C_GUARD` / `CCY_CTRL_C_WINDOW_SECONDS` S:546-547;
  - `CLAUDE_SUPERVISE_NO_BANNER` S:282;
  - `CLAUDE_SUPERVISE_NO_WORKER` S:290.
- `CompactPolicy` S:1535 holds the defaults.
- The daemon side has only `ccy.deploy_supervisor` (`CcyConfig` D:config/models.py:1989),
  which deploys and arms the script. Arming writes `CCY_CLAUDE_WRAPPER` into the launcher's
  env file and never overwrites an existing line (D:install/ccy_supervisor.py:63-73).

### Spawn, wait, return, inject

- Spawn and wait: see the main loop above (S:6616, 6622, 6803, 6812).
- `_perform_injection` S:4758 types the text bracketed-paste framed (S:4794), waits
  `_SUBMIT_DELAY_SECONDS`, sends CR, then optional confirm Enters. `submit=False` writes the
  bytes raw (ESC, bare Enter).
- `_apply_decision` S:5686 is the host-side performer. `/exit` would simply be a payload
  `"/exit"` with `submit=True`.

### Status line and audit log

- `write_status_message` S:1999: an atomic replace of `supervise/status-message.json`, with
  info/warning levels, TTL and countdown. An INFO write yields to a live WARNING (S:2046).
- `StatusMessagePoster` S:2070 adds a rate limit and is documented as a "GENERAL, reusable"
  channel (S:1908-1915).
- `write_supervisor_status` S:1860 holds the identity (version, hash, pid). The daemon reads
  it (`D:utils/ccy_supervisor.py:137`, `D:handlers/status_line/supervisor_indicator.py`).
- `DecisionLog` S:979 is append-only, timestamped and fail-fast, with NOOP dedup (S:1037)
  and a size cap. Its default path is `$CLAUDE_PROJECT_DIR/untracked/supervise/decision.log`
  (S:1011-1013). That is **not** the daemon untracked dir that the status files use, a minor
  inconsistency worth knowing when wiring audit lines.
- The audit banner (S:4572-4669, `arm_audit` S:3483) is a status-line summary of
  machine-taken actions.

### Things that already look like extension points

- `supervise(decider=...)` S:6523 takes an injected `Callable[[TickFacts], TickOutcome | None]`. That is the host/worker seam, not a public API.
- The line-JSON host-worker protocol (`TickFacts` S:1580, `TickOutcome` S:1630) is built for
  backward compatibility: every new field has a default, and `import_state` merges by key.
  A new `plugin_slot_free: bool = False` field fits this pattern exactly.
- The status message channel and `DecisionLog` are general-purpose writers.
- The `--emit-model-switch` CLI (S:6880) shows the pattern of a helper that writes a signal.
- The worker's per-tick broad catch (S:6059-6067) and `WorkerCrashGuard` (S:6276) are
  existing failure-containment patterns.

### Precedent: how the daemon loads extensions

- **Plugins** (`plugins:` in yaml; `PluginConfig` D:config/models.py:456, `PluginsConfig`
  :512). These are explicit config entries with a path, which may be absolute or use
  `{REPO_ROOT}`. `PluginLoader.load_from_plugins_config` D:plugins/loader.py:222 uses
  `importlib.util.spec_from_file_location`. It is **fail-fast**: a configured plugin that
  cannot load raises `RuntimeError` (:333).
- **Project handlers** (`project_handlers:`; `ProjectHandlersConfig` D:config/models.py:554,
  default path `.claude/project-handlers`, :571). These are convention-scanned per event
  subdirectory. `ProjectHandlerLoader.load_handler_from_file` D:handlers/project_loader.py:152
  requires exactly one concrete subclass. Versioning goes through `_ABSTRACT_METHOD_VERSIONS`
  (:73): a missing abstract method produces an upgrade hint naming the version that
  introduced it. Discovery is **skip-and-alert** (`discover_handlers_with_failures` :325),
  with failures persisted for a SessionStart alert (`project_handler_load_checker`). The
  scaffold is `init-project-handlers` (D:daemon/cli.py:4990).
- Neither mechanism touches the supervisor. Both load into the daemon process for hook
  events. This project has no `.claude/project-handlers/` directory.

## 2. Design choices, and why

**Superseded in part by three owner rulings; the draft issue is authoritative.** First,
plugins run at two levels: a worker half (the default, hot-reloadable) and an optional host
half that has only `before_spawn`. Second, a plugin can never block the supervisor: a load
failure is skipped and announced, which reverses item 6 below. Third, every failure takes
one path: detect, disable, recover (restart the worker without the plugin, or respawn with
plain `--resume` and no overlay), then type a fixed-template notice into the session through
a new built-in family. The host-hook timeout is resolved by running the host half **in the
forked child, between `fork` and `exec`**. A close-on-exec pipe reports success (EOF) or a
failure code, and the parent kills the child with `SIGKILL` on a deadline. That is
stdlib-only, abandons no thread, and keeps the secret out of the supervisor's memory and off
every pipe. It beats a deadline thread, which cannot be killed and holds the secret in the
host, and a helper subprocess, which would have to pipe the secret back. Items 2, 4, 6 and 7
below are therefore replaced, and `on_child_exit` moved out of scope.

01. **Discovery by `--plugin` flag, not YAML and not a directory scan.** The supervisor
    cannot read YAML (stdlib-only, a deliberate isolation from the daemon venv). The flag
    lives in the launcher's wrapper line, which is the project's own opt-in. A YAML key
    (`ccy.supervisor_plugins`) rendered by the installer would not reach existing installs,
    because the installer never rewrites an existing `CCY_CLAUDE_WRAPPER` line. It could be
    added later as sugar.
02. **The hooks run in the host, and the worker only says the slot is free.** The built-in
    priority stays in the hot-reloadable worker, as one new boolean in `TickOutcome`. Only
    the host can fork, wait and respawn. This grows the host-tier surface that upstream
    deliberately thinned (the S:109-119 audit), which is the likeliest push-back. The
    alternative, plugins in the worker, is in the issue's "Alternatives considered". The
    in-process fallback (`_poll_once` S:5798) must compute the same flag.
03. **Subordinate to every built-in family, not only compact/continue.** This is simpler and
    safer. The cost is one tick of latency when a goal or operator signal is pending.
04. **`Restart` carries no secret; `before_spawn` is the only env route.** The secret never
    travels inside an action object, so audit logging of actions is structurally safe. A new
    credential also persists across any later respawn, because the plugin keeps returning it.
05. **Validate everything before `/exit`.** Session id, env keys and the plugin's own
    credential checks all happen in `on_idle`, before the only destructive step.
06. **A load failure refuses to start the supervisor.** This follows the daemon's plugin
    loader (fail-fast) rather than its project-handler loader (skip-and-alert). Trade-off: a
    broken plugin blocks every launch of that project until it is fixed or unlisted. Owner
    call.
07. **Failed respawn: one retry with the original env, then exit non-zero.** "Never leave a
    session with no `claude`" competes with fail-fast here. The retry is loud, not silent,
    and it lands on the old account. Owner call.
08. **No general `Inject` action in v1** (YAGNI). The worked example needs only `/exit`,
    which the supervisor types itself. A general one would also have to carry the
    `🤖 [ccy-supervisor` provenance marker and the own-line rules (S:5218).
09. **API version.** `PLUGIN_API` major in the module, `host.api_version` (major, minor);
    minor additions are optional hooks found by `hasattr`. This mirrors the intent of the
    daemon's `_ABSTRACT_METHOD_VERSIONS` hints, but suits duck typing: the plugin cannot
    import the supervisor, which is a script.
10. **Hook latency.** Hooks run synchronously on the select loop, so a slow hook stalls I/O.
    The issue should state a budget, and the supervisor should log an overrun. A hook cannot
    be killed in-thread.

## 3. Issue-report generator: findings

- Free-text fields: `summary`, `expected`, `observed`, `reproduction` (D:issue_report/
  build.py:40). No length limit exists anywhere in the generator.
- `reproduction` is **refused** on any path-looking token outside the daemon prefixes
  (D:issue_report/reproduction.py:301-311). A leading `/` counts as an absolute path, so a
  slash command like `/exit` cannot appear there, and nor can `.claude/ccy/...`. The other
  three fields are **scrubbed**, not refused (assemble.py docstring). So the full proposal
  goes in `expected`.
- The no-reproduction sentinel is `CANNOT-REPRODUCE-SYNTHETICALLY`, which must come first
  (reproduction.py:294). It was not needed: a synthetic reproduction works.
- `source_citation` must resolve under `src/claude_code_hooks_daemon/`
  (D:issue_report/citation.py:768). The supervisor is not there: it is bundled at
  `<daemon>/.claude/ccy/` (D:install/ccy_supervisor.py:129). That line is cited as the
  nearest.
- Currency: installed 3.67.0 is the latest release (`gh release list` agrees).

## 4. Synthetic reproduction (run)

```text
untracked/scratch/plugin-repro/demo_plugin.py   (a 4-line module, never loaded)
CLAUDE_PROJECT_DIR=<repo>/untracked/scratch/plugin-repro \
  python3 .claude/hooks-daemon/.claude/ccy/claude-supervise.py \
  --plugin untracked/scratch/plugin-repro/demo_plugin.py -- true
-> claude-supervise: error: unrecognized arguments: --plugin ...   (exit 2)
grep -cE 'importlib|--plugin' .claude/hooks-daemon/.claude/ccy/claude-supervise.py  -> 0
```

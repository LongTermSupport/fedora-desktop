# DRAFT: upstream feature request: a plugin API for the ccy supervisor

**Not filed.** This supersedes [UPSTREAM-REQUEST-draft.md](UPSTREAM-REQUEST-draft.md). The
owner decided to ask for a generic plugin API in the supervisor, not a hard-coded
credential-switch feature, so that the downstream credential switch ships as our own plugin
and the daemon needs no knowledge of any downstream project. Owner rulings folded in: plugins
run at two levels, with the policy worker as the default; a plugin can never block the
supervisor or the session; and every plugin failure is announced inside the session. Filing
is the owner's call (Plan 00146 Task 1.1). It goes only through `hooks-daemon issue-report`.
The fields file is `untracked/scratch/supervisor-plugins-fields.json`, rebuilt from this
file by `untracked/scratch/build_supervisor_plugins_fields.py`. The generated body is the
newest `untracked/issue-reports/issue-report-*.md`. If this text changes, regenerate the
report; never edit the report by hand. Before filing, re-read everything below the rule for
anything that identifies one install. There must be none.

Design notes, source citations and the reasoning behind each choice:
[subagent-reports/261002-supervisor-plugin-research-opus.md](subagent-reports/261002-supervisor-plugin-research-opus.md).

---

## Title

ccy supervisor: a small plugin API so downstream launchers can extend it without forking

## Problem

The ccy supervisor (`claude-supervise.py`) is the only process that owns `claude`'s
lifecycle. It starts `claude` with `pty.fork` + `os.execvp`, watches it, and acts at a
well-guarded idle choke point: idle, an empty input box, and subordinate to compact/continue.
Every request family it handles is built in, as a hard-coded loader, a branch in
`decide_once` and a `Decision` member. Its only configuration is three CLI flags and a few
environment variables. It spawns `claude` exactly once and returns its exit code.

So a downstream launcher that needs one more behaviour has to ask upstream for a feature that
is really its own. Our case: the launcher passes the Claude credential in through an
environment variable. When that account hits its rate limit, moving the session to another
account means ending `claude` and starting `claude --resume <session-id>` with a different
value in that variable. Claude Code reads the variable once, so this cannot happen inside a
running `claude`. Only the supervisor can do it without tearing down everything around the
session, but it is launcher policy, and the daemon should not have to know it.

## Proposal

Let a project name plugins explicitly. Each plugin has a **worker half**, which holds the
logic, and an optional, tiny **host half** for the one thing only the PTY host can do.

**Discovery.** Repeatable flags before `--` in the launcher's wrapper line. The supervisor is
stdlib-only and reads no YAML, so flags are its natural config:

```text
claude-supervise.py --plugin <name>=<worker.py> [--plugin-host <name>=<host.py>] --arm -- claude ...
```

Nothing is found by scanning a directory. Each file must be a regular file owned by the
supervisor's uid or root, and neither it nor its directory may be group- or world-writable.
The host never imports a worker half, the worker never imports a host half, and both halves
are stdlib-only.

**Hooks by level.**

| Level  | Hook                                               | Why it is here                                                                                                                              |
| ------ | -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| worker | `on_start()`                                       | Default level: hot-reloadable, inside the existing per-tick safety net                                                                      |
| worker | `on_idle(tick) -> Restart \| None`                 | Sees the same facts as the built-in families; its `Restart` crosses the existing host-worker channel and carries no secret                  |
| host   | `before_spawn(spawn) -> Mapping[str, str] \| None` | A spawn's environment exists only in the process that calls `execvp`; this is the one way a secret reaches `claude` without crossing a pipe |

Ending and respawning `claude` is a **supervisor primitive**, triggered by a `Restart`. It
is not a plugin hook. The host has no other plugin code.

```python
# worker half                                    # host half (optional)
PLUGIN_API = 1                                   PLUGIN_API = 1
def create_worker_half(api) -> WorkerHalf: ...   def create_host_half(api) -> HostHalf: ...

class WorkerHalf:                                class HostHalf:
    name: str; version: str                          env_keys: frozenset[str]  # only vars it may set
    def on_start(self) -> None: ...                  def before_spawn(self, spawn: SpawnInfo
    def on_idle(self, tick: IdleTick                     ) -> Mapping[str, str] | None: ...
        ) -> Restart | None: ...

# api (worker): api_version (major, minor); state_dir: Path (0700, per plugin);
#   session_id() -> str | None (own session, None if ambiguous); status(text, level, ttl);
#   audit(message) -> a decision.log line, carried to the host in TickOutcome; Restart(reason)
IdleTick  = (now: float, session_id: str | None)
SpawnInfo = (attempt: int, resume_session: str | None)
Restart   = (reason: str)                        # short, logged only, never typed into the chat
```

**Where plugins sit in priority.** The worker calls `on_idle` only when `decide_once` has
reached NOOP in MONITOR, with no compaction signal pending, no unconfirmed own line, and
`can_inject` true. That puts plugins below every built-in family. Plugins are asked in flag
order, and the first `Restart` of a tick wins.

**Restart, the one action in v1.** The worker returns `Restart` in a new `TickOutcome`
field. The host then:

1. refuses unless the session id is unambiguous;
2. types `/exit` through the existing injection path and holds every injection until the
   child exits, giving up if the wait times out;
3. forks the new child: the original argv minus `--continue`/`-c`/`--resume`/`-r [id]`/
   `--fork-session`, plus `--resume <id>`;
4. returns to the normal loop on the new child.

The supervisor's exit code is the last child's.

**The host half runs in the forked child, between `fork` and `exec`.** Supervisor code in
the child imports the host half and calls `before_spawn`. It then checks the overlay against
`env_keys` and a fixed denylist (`PATH`, `LD_*`, `PYTHON*`, `CLAUDE_PROJECT_DIR`,
`CLAUDE_CODE_SESSION_ID`), and calls `execvpe`. A pipe marked close-on-exec reports the
result to the parent: end-of-file means the exec happened; a short failure code means the
hook or exec failed; silence past a deadline (a few seconds) means the parent kills the
child with `SIGKILL`. All of this is stdlib. No thread is ever abandoned. The secret never
enters the supervisor's memory or crosses any pipe. A hang, a crash or `os._exit` in the
plugin kills only that child, never the supervisor.

**Versioning.** `PLUGIN_API` is a major version, and a mismatch is a load failure. Minor
versions only add optional hooks or fields, which a plugin detects through `api.api_version`.
`supervisor-status.json` lists each plugin: name, version, level, and
`loaded`/`failed`/`disabled` with the reason.

## Worked example: credential switch

The worker half finds a request file in its `state_dir` that names nothing secret, checks
the session id, and returns `Restart(reason="credential switch")`. The host half declares
`env_keys = {<credential variable>}`. Its `before_spawn` reads the new credential from a
container-local directory that is not the project bind mount. It opens the file with
`O_NOFOLLOW` and requires a regular file owned by the supervisor's uid, mode `0600`, within
a size bound. It unlinks the file and returns `{<variable>: value}`. The turn in progress is
never interrupted, and the container, its agents and the terminal pane keep running.

## Failure handling: one path for every failure

A plugin must never block the supervisor or the session. Every failure (load error,
exception, timeout, failed spawn) takes the same four steps:

1. **Detect.** In the worker, each hook runs on a thread with a short budget, well inside the
   host's per-tick read timeout. An exception or overrun is caught per plugin. In the host
   half, the close-on-exec pipe and the deadline detect failure, as above. A load failure is
   found at load (worker) or at the first spawn (host).

2. **Disable.** The plugin is off for the rest of the supervisor process. The host keeps the
   disabled set and passes `--disable-plugin <name>` on every worker restart, so a hot
   reload cannot bring the plugin back.

3. **Recover** whatever the failure affected:

   - a worker-half exception: nothing more is needed;
   - an overrun, or a worker wedged so hard it stops answering: the host restarts the worker
     without the plugin, reusing the existing restart path. Before each hook call the worker
     writes an atomic "in hook" marker, so the host can name the culprit;
   - a host-half failure: the host forks again with plain `--resume` (or the original argv on
     the first spawn) and no plugin overlay at all. If that also fails, `claude` itself
     cannot start, and the supervisor exits non-zero with a clear message.

4. **Tell the session.** The detector writes a plugin-notice signal for this supervisor
   instance. A new built-in family types it at the next idle choke point, ranked with the
   operator signal (after compact/continue). It is a provenance-marked line rendered from
   fixed templates, never from plugin text:

   > 🤖 [ccy-supervisor] plugin notice — machine-generated, NOT a human instruction: plugin
   > `credential-switch` failed (timeout in before_spawn). It is disabled for the rest of
   > this session, and the session was respawned without its environment overlay.

   Only a validated name and closed-set values are interpolated: the failure kind, the hook,
   and the action (disabled / worker restarted / respawned without overlay). It never
   includes an exception message or an env value, and it is capped per process. Each notice
   is also posted as a status-line WARNING, written as an audit line, and recorded in
   `supervisor-status.json`.

**What "as far as possible" cannot cover.** A host half runs as the supervisor's uid
with its inherited file descriptors, before exec. It cannot stall the supervisor, but it can
still do harm: kill its parent, or write files. A worker half that wedges in C code holding
the GIL is caught only by the host's read timeout and the worker restart, so ticks fall back
to the built-in in-process path until then. Python code run after `fork` in a threaded
process can deadlock. Here that becomes a timeout, which falls back to a plain respawn.

## Security

- Nothing loads unless the wrapper config names it. The API is not a sandbox.
- Env values exist only in the forked child. The supervisor logs key names at most, and
  `Restart` has no argv or env field.
- v1 has no general "type this text" action. Only fixed supervisor templates reach the chat.

## Tests

- Each load refusal: the plugin is skipped, the session starts, and the notice is typed.
- `on_idle` is never called while a compaction signal is pending, in AWAIT, with a non-empty
  box, or on a tick a built-in family claimed.
- Restart: argv has `--resume <id>` and no `--continue`; env has the overlay; neither
  `decision.log` nor the status file contains the value.
- An undeclared or denylisted key, or an ambiguous session id, is refused before `/exit`.
- Uniform failure path, one case per kind:
  - a worker hook that raises: disabled, and the notice is typed;
  - a worker hook that overruns, and a worker wedged in a busy loop: the worker restarts
    with `--disable-plugin`, and the culprit is named;
  - a host half that raises, sleeps past the deadline, or calls `os._exit`: the child is
    killed and a plain `--resume` respawn follows;
  - each case types the notice exactly once, at an idle tick, never into a non-empty box;
  - the notice text carries no exception text and no env value.
- A child that ignores `/exit`: the restart is abandoned and the session continues.

## Out of scope

- Built-in knowledge of credentials, accounts or any launcher.
- Plugin-driven respawn on an unsolicited exit, and a general text-injection action.
- Changing a running `claude`'s credential. Claude Code does not support that.

## Alternatives considered

- **Worker-only plugins.** The worker cannot fork or exec `claude`, so the env overlay
  would have to cross the host-worker pipe with the secret in it.
- **Host-only plugins.** Plugin logic would run in the process that owns the live session,
  with no hot reload and no per-tick safety net, and a hang there stalls the PTY.
- **Host half on a deadline thread in the host process.** An overrun thread cannot be
  killed, only abandoned, and the secret would sit in the supervisor's memory. Running the
  host half in the forked child avoids both.
- **A hard-coded credential-switch family.** It puts one launcher's policy into the daemon.
- **An outer wrapper loop plus a built-in "exit at idle" request.** This is the smallest
  upstream change, but the supervisor and its worker restart on every switch. We would
  accept it if a plugin API is unwelcome.
- **The daemon's own `plugins:` and `project_handlers:`.** They load into the daemon process,
  which never spawns `claude`.

# DRAFT: upstream feature request: a plugin API for the ccy supervisor

**Not filed.** This supersedes [UPSTREAM-REQUEST-draft.md](UPSTREAM-REQUEST-draft.md). The
owner decided to ask for a generic plugin API in the supervisor, not a hard-coded
credential-switch feature, so that the downstream credential switch ships as our own plugin
and the daemon needs no knowledge of any downstream project. Filing is the owner's call
(Plan 00146 Task 1.1). It goes only through `hooks-daemon issue-report`; the fields file is
`untracked/scratch/supervisor-plugins-fields.json` (rebuilt from this file by
`untracked/scratch/build_supervisor_plugins_fields.py`), and the generated body is
`untracked/issue-reports/issue-report-20261002-171440.md`. If this text changes, regenerate
the report; never edit the report by hand. Before filing, re-read everything below
the rule for anything that identifies one install. There must be none.

Design notes, source citations and the reasoning behind each choice:
[subagent-reports/261002-supervisor-plugin-research-opus.md](subagent-reports/261002-supervisor-plugin-research-opus.md).

---

## Title

ccy supervisor: a small plugin API so downstream launchers can extend it without forking

## Problem

The ccy supervisor (`claude-supervise.py`) is the only process that owns `claude`'s
lifecycle: it starts it with `pty.fork` + `os.execvp`, watches it, and acts at a well-guarded
idle choke point (idle, empty input box, subordinate to compact/continue). Every request
family it handles (compact/continue, goal, goal clear, model switch and restore, operator
signal, standing-auth, session actions) is built in: a hard-coded loader, a hard-coded
branch in `decide_once`, a hard-coded entry in `Decision`. Its only configuration is three
CLI flags and a handful of environment variables, and it spawns `claude` exactly once and
returns its exit code.

So a downstream launcher that needs the supervisor to do one more thing has to ask upstream
for a feature that is really its own. Our case: the launcher passes the Claude credential in
through an environment variable. When that account hits its rate limit, moving the session
to another account means ending `claude` and starting `claude --resume <session-id>` with a
different value in that variable. Claude Code reads the variable once, so this cannot happen
inside a running `claude`, and only the supervisor can do it without tearing down everything
around the session. That is launcher policy, and the daemon should not have to know it.

## Proposal

Let a project name supervisor plugins explicitly, and give them a few lifecycle hooks plus
the primitives the supervisor already has.

**Discovery.** A repeatable supervisor flag, placed before `--` in the launcher's wrapper
line (the supervisor is stdlib-only and reads no YAML, so a flag is the natural config):

```text
claude-supervise.py --plugin <path-to-module.py> [--plugin ...] --arm -- claude ...
```

Nothing is discovered by scanning a directory. A plugin file must be a regular file owned by
the supervisor's uid or root, and neither it nor its directory may be group- or
world-writable. Plugins must be stdlib-only, like the supervisor.

**Contract (API version 1).** The module defines the API major version it was written for
and a factory. The plugin object is duck-typed and every hook is optional:

```python
PLUGIN_API = 1                                   # major; a mismatch refuses to load

def create_plugin(host: PluginHost) -> Plugin: ...

class Plugin:
    name: str                                    # [a-z0-9-]{1,32}, unique per supervisor
    version: str
    env_keys: frozenset[str]                     # the ONLY variables it may set on a spawn

    def on_start(self) -> None: ...              # once, before the first spawn
    def before_spawn(self, spawn: SpawnInfo) -> Mapping[str, str] | None: ...
    def on_idle(self, tick: IdleTick) -> Restart | None: ...
    def on_child_exit(self, exit: ChildExit) -> Respawn | None: ...
    def on_stop(self) -> None: ...               # once, before the supervisor returns

class PluginHost:                                # supplied by the supervisor
    api_version: tuple[int, int]                 # (major, minor)
    state_dir: Path                              # <daemon untracked>/supervise/plugins/<name>/, 0700
    Restart: type; Respawn: type                 # action constructors (no import needed)
    def session_id(self) -> str | None: ...      # own session, or None if ambiguous
    def status(self, text: str, *, level: str = "info", ttl: float = 10.0) -> None: ...
    def audit(self, message: str) -> None: ...   # decision.log line, prefixed "plugin <name>:"

@dataclass(frozen=True)
class IdleTick:    now: float; session_id: str | None
class SpawnInfo:   attempt: int; resume_session: str | None
class ChildExit:   exit_code: int; requested_by: str | None   # plugin whose Restart caused it
class Restart:     reason: str; resume: bool = True           # carries no env and no argv
class Respawn:     reason: str; resume: bool = True
```

**Where the hooks run.** In the PTY host, because only the host owns the child. `on_idle` is
called only on a tick where nothing built in wants the slot: the worker reports a new
`TickOutcome` flag, set when the tick decided NOOP in MONITOR with no compaction signal
pending, no unconfirmed own line, and `can_inject` true. So a plugin is subordinate to every
built-in family, compact/continue included. Plugins are asked in `--plugin` order, and at
most one action is taken per tick.

**Restart, the one action in v1.** The supervisor (1) refuses unless `session_id()` is
unambiguous; (2) types `/exit` through the existing injection path; (3) stops all injections
and plugin hooks until the child exits, giving up loudly and carrying on with the existing
child if it is still running after a bounded wait; (4) forks a new `claude` on a new PTY:
the original argv minus `--continue`/`-c`/`--resume`/`-r [id]`/`--fork-session`, plus
`--resume <id>`, with the parent environment overlaid by every plugin's `before_spawn`
result; (5) returns to the normal loop on the new child. The supervisor's exit code is the
last child's. `before_spawn` is the only way an environment value travels, and it applies to
every later spawn as well, so a changed value survives any later respawn.

**Versioning.** `PLUGIN_API` is a major version: a mismatch refuses to load. Minor versions
only add optional hooks or fields, which a plugin detects through `host.api_version`. The
supervisor records each loaded plugin's name, version and API in `supervisor-status.json`
and in `decision.log` at start.

## Worked example: credential switch

A downstream launcher passes the Claude credential via an environment variable. Its plugin
declares `env_keys = {<that variable>}`. A launcher command writes a request file into the
plugin's `state_dir`, naming nothing secret, and places the new credential in a
container-local directory that is not the project bind mount. On `on_idle` the plugin finds
the request and checks everything before anything destructive happens: the session id is
unambiguous; the credential file is opened with `O_NOFOLLOW`, is a regular file owned by the
supervisor's uid, mode `0600`, within a size bound; and it is unlinked at once, where a
failed unlink refuses the request. Only then does the plugin keep the value in memory and
return `Restart(reason="credential switch")`. Its `before_spawn` returns `{<variable>: value}`
on every spawn from then on. The turn in progress is never interrupted, because the request
waits for the idle choke point. The container, its agents and the terminal pane all keep
running.

## Security

- Plugins run with the supervisor's privileges, in its process. The API is not a sandbox,
  so the guarantee is that nothing loads unless the project's wrapper config names it, and
  that the file passes the ownership and mode checks.
- An env override outside the plugin's `env_keys` is refused. So is a fixed denylist even if
  declared (`PATH`, `LD_*`, `PYTHON*`, `CLAUDE_PROJECT_DIR`, `CLAUDE_CODE_SESSION_ID`).
- The supervisor never writes an env value anywhere. Audit and status lines name keys only.
  `Restart` and `Respawn` have no argv or env field, so a secret has no route into argv or
  the log through them.
- v1 offers no general "type this text" action. That would need the provenance marker and
  the own-line follow-up rules the built-in families obey, and nothing needs it yet.

## Failure handling

- **Load failure** (missing file, import error, no factory, API mismatch, bad or duplicate
  name, bad `env_keys`, ownership or mode): the supervisor exits 2 **before** spawning
  `claude`, naming the plugin and the reason. The config named it, so this is fail-fast, and
  no session exists yet that could be left without `claude`.
- **Hook exception at runtime:** the traceback goes to the worker error log, an audit line
  and a WARNING status notice name the plugin, and the plugin is disabled for the rest of
  the process. The session carries on untouched. Nothing is retried every tick, and nothing
  is swallowed silently.
- **After `/exit`:** if `before_spawn` raises, or the new child dies within a short window,
  the supervisor makes one loud retry with the unmodified environment and `--resume`. If that
  fails too, it exits non-zero with a clear message. It never sits on an empty PTY.
- `on_child_exit` respawns are capped (say, 3 in 10 minutes) so a buggy plugin cannot loop.

## Tests

Fake plugins and a fake child, a small stdlib script on the PTY that records its argv and
env and exits on `/exit`:

- each load refusal;
- `on_idle` is never called while a compaction signal is pending, in AWAIT, with a non-empty
  box, or on a tick a goal or operator signal claimed;
- a raising hook is disabled, logged and posted, and the child is untouched;
- after a restart, argv carries `--resume <id>` and no `--continue`, and env has the override;
- the value appears in neither `decision.log` nor the status file;
- an undeclared or denylisted key is refused before `/exit`;
- an ambiguous session id is refused before `/exit`;
- a child that ignores `/exit` causes an abandoned restart, and the session continues;
- a failed respawn makes one retry, then exits non-zero.

## Out of scope

- Any built-in knowledge of credentials, accounts or a particular launcher.
- Hot-reloading plugins. They live in the host, so changing one needs a supervisor restart.
- A general text-injection action, and plugins in the policy worker.
- Changing a running `claude`'s credential. Claude Code does not support that.

## Alternatives considered

- **A hard-coded "respawn with a changed credential" family.** It works, but it puts one
  launcher's policy into the daemon, and the next downstream need is another feature request.
- **Plugins in the policy worker.** That would bring hot reload and the existing per-tick
  safety net. But the worker cannot fork or wait on `claude`, a secret would have to cross
  the host-worker pipe, and plugin state would be lost on every worker restart.
- **An outer wrapper loop plus one built-in "exit at idle" request.** This is the smallest
  upstream change. The downstream wrapper re-runs the supervisor with `--resume` and a new
  environment after it returns. But the supervisor, its worker and its status file restart
  on every switch, and the plumbing is split across two processes. We would accept this if a
  plugin API is unwelcome.
- **Daemon handler plugins (`plugins:`, `project_handlers:`).** These load handlers into the
  daemon process for hook events. The daemon neither spawns nor waits on `claude`, so they
  cannot respawn it.

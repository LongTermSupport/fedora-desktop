"""The `pingbus` command line: dispatch, exit codes, output lines and the commands.

Spec: docs/agent-bus-protocol.md §13 (commands), §14 (exit codes), §15 (output lines),
§9 (on send; on receive; reading the inbox), §7 (agent text to humans), §10 (the send
bucket, TIMEOUT), §12 (multi-team, the lock), §3 (`suggest-handle`), §1 (`version`).
The offline commands are `version`, `validate`, `config check`, `suggest-handle`,
`inbox`, `status` and `hook …` (the hooks are `hooks`'s); the network commands are `send`,
`say`, `recv`, `wait` (Plan 00161 U11) and `watch` (U12), built on `syncer` for room trust
and receiving; `watch` wakes the session through `notify`.

Streams: stdout carries only a command's payload (the §15 stdout lines, `validate`'s
verdict, and the report commands' text); every diagnostic goes to stderr. Nothing read
from an event or a bundle is ever echoed: refusals print a reason code, and the config
errors name keys and files, never values. `recv` and `wait` print an item only from the
copy they re-fetch from the homeserver, after re-validating it: the inbox is a cache.
"""

from __future__ import annotations

import argparse
import collections
import dataclasses
import hashlib
import io
import json
import os
import pathlib
import queue
import re
import stat
import subprocess
import sys
import threading
import time
from collections.abc import Callable, Mapping, Sequence
from typing import BinaryIO, TextIO

from helpers.agent_bus import registry
from helpers.pingbus import config, forge, hooks, inbox, limits, matrix, notify, protocol, syncer

PROG = "pingbus"
TOOL_VERSION = "0.1.0"

EXIT_OK = 0
EXIT_NOTHING = 3
EXIT_REFUSED = 4
EXIT_FORGE = 5
EXIT_DROPPED = 6
EXIT_UNREACHABLE = 7
EXIT_AUTH = 8
EXIT_RATE = 9
EXIT_UNTRUSTED = 10
EXIT_USAGE = config.UsageError.EXIT_CODE
EXIT_BUSY = 75
EXIT_CONFIG = config.ConfigError.EXIT_CODE

#: Spec §14, word for word; test_cli_offline holds it equal to the document.
EXIT_CODES = {
    EXIT_OK: "success; `recv`/`wait` printed at least one line (drops and other teams' failures, if any, are on stderr)",
    1: "never assigned (an uncaught exception)",
    2: "never assigned (argparse's default is remapped to 64)",
    EXIT_NOTHING: "nothing: `recv` found nothing; `wait` reached its timeout",
    EXIT_REFUSED: "refused by the validator (`secret` included), or by role",
    EXIT_FORGE: "the reference did not resolve at the forge, or failed the provenance check",
    EXIT_DROPPED: "`recv` only: received items were dropped and no valid line was printed",
    EXIT_UNREACHABLE: "homeserver unreachable",
    EXIT_AUTH: "authentication refused (token rejected)",
    EXIT_RATE: "rate limited (local limit, duplicate, server 429 after retries, or forge rate limit)",
    EXIT_UNTRUSTED: "the team room is not trusted (§8), or not joined",
    EXIT_USAGE: "usage error",
    EXIT_BUSY: "busy: another process holds this account's sync lock (`wait`, `watch`)",
    EXIT_CONFIG: "configuration refused (§12, §10 bounds, Python older than 3.11)",
}

STDOUT = "stdout"
STDERR = "stderr"
ABSENT = "-"
#: Spec §15: each line's stream and the fields after `<KIND>` and the version.
LINES = {
    "PING": (STDOUT, ("team", "event ID", "sender", "verb", "ref", "re")),
    "HUMAN": (STDOUT, ("team", "event ID", "sender", "origin_server_ts", "text as a JSON string")),
    "TIMEOUT": (STDOUT, ("team", "event ID of the unanswered ping", "silent target", "verb", "ref")),
    "SENT": (STDOUT, ("team", "event ID")),
    "DROPPED": (STDERR, ("count", "reason=count pairs joined by ,")),
}

#: The last verified team record (spec §12 `state/team.json`): the `agent_bus.team`
#: content as the syncer verified it. Re-parsed on every read: it is a cache.
TEAM_RECORD_CACHE = inbox.TEAM_FILE
TEAM_RECORD_MAX_BYTES = inbox.TEAM_RECORD_MAX_BYTES
#: Matrix's own limit on a whole event; nothing larger can have come from a homeserver.
EVENT_FILE_MAX_BYTES = 65536

#: Spec §6, in precedence order. `GH_TOKEN`/`GITHUB_TOKEN` apply only when the team
#: record's `forge_api` is exactly https://api.github.com; the forge client decides that.
FORGE_TOKEN_FILE_VAR = "PINGBUS_FORGE_TOKEN_FILE"
FORGE_TOKEN_VARS = ("PINGBUS_FORGE_TOKEN", "GH_TOKEN", "GITHUB_TOKEN")
FORGE_NONE = "none"

ROLE_VAR = "HOOKS_DAEMON_HOSTNAME"
#: Values of the `container` variable podman, docker and LXC set, taken as the type.
CONTAINER_ENV_TYPES = ("podman", "docker", "lxc")
#: `systemd-detect-virt --container` names that map to a §3 type.
DETECT_VIRT_CONTAINER_TYPES = {"lxc": "lxc", "lxc-libvirt": "lxc", "podman": "podman", "docker": "docker"}
PODMAN_MARKER = "/run/.containerenv"
DOCKER_MARKER = "/.dockerenv"

_CONTROL_RE = re.compile(r"[\x00-\x1f\x7f]")


class _ParserExit(Exception):
    """argparse finished on its own (`--help`): return its status instead of exiting."""

    def __init__(self, status: int) -> None:
        super().__init__(status)
        self.status = status


# ---------------------------------------------------------------- output lines (§15)


def format_line(kind: str, *fields: str | None) -> str:
    """One §15 line. `None` is an absent field; a field must be a non-empty string with
    no control character. Callers pass fields that already passed their grammar."""
    names = LINES[kind][1]
    if len(fields) != len(names):
        raise ValueError(f"{kind} takes {len(names)} fields, got {len(fields)}")
    out = []
    for name, value in zip(names, fields, strict=True):
        text = ABSENT if value is None else value
        if not isinstance(text, str) or not text or _CONTROL_RE.search(text):
            raise ValueError(f"{kind} field {name!r} is not printable")
        out.append(text)
    return "\t".join((kind, str(protocol.PROTOCOL_VERSION), *out))


def emit(line: str, stdout: TextIO, stderr: TextIO) -> None:
    """Write a formatted line to the stream §15 gives its kind."""
    stream = LINES[line.partition("\t")[0]][0]
    (stdout if stream == STDOUT else stderr).write(line + "\n")


def _team(team: str) -> str:
    if not protocol.is_team_name(team):
        raise ValueError("team field is not a team name")
    return team


def _event_id(event_id: str) -> str:
    if not protocol.is_event_id(event_id):
        raise ValueError("event ID field is not an event ID")
    return event_id


def _localpart(user_id: str, server_name: str) -> str:
    localpart = protocol.parse_user_id(user_id, server_name)
    if localpart is None:
        raise ValueError("user field is not a user ID on this server")
    return localpart


def _verb(verb: str) -> str:
    if verb not in protocol.VERBS:
        raise ValueError("verb field is not a verb")
    return verb


def _ref(ref: str | None) -> str | None:
    if ref is not None and protocol.parse_ref(ref) is None:
        raise ValueError("ref field is not a reference")
    return ref


def ping_line(team: str, server_name: str, ping: protocol.Ping) -> str:
    return format_line(
        "PING", _team(team), _event_id(ping.event_id), _localpart(ping.sender, server_name),
        _verb(ping.verb), _ref(ping.ref.text if ping.ref else None),
        None if ping.re is None else _event_id(ping.re),
    )


def human_line(team: str, server_name: str, message: protocol.HumanMessage) -> str:
    if not protocol.is_human_user_id(message.sender, server_name):
        raise ValueError("sender field is not a human user ID")
    ts = message.origin_server_ts
    if type(ts) is not int or ts < 0:
        raise ValueError("origin_server_ts field is not a non-negative integer")
    return format_line(
        "HUMAN", _team(team), _event_id(message.event_id),
        _localpart(message.sender, server_name), str(ts),
        json.dumps(message.text, ensure_ascii=True),
    )


def timeout_line(
    team: str, server_name: str, event_id: str, target: str, verb: str, ref: str | None
) -> str:
    return format_line(
        "TIMEOUT", _team(team), _event_id(event_id), _localpart(target, server_name),
        _verb(verb), _ref(ref),
    )


def sent_line(team: str, event_id: str) -> str:
    return format_line("SENT", _team(team), _event_id(event_id))


def dropped_line(counts: Mapping[str, int]) -> str:
    """The batch's aggregate drop line, reasons in spec order. Only drop reasons count:
    an ignore (`agent-text` among them) is never a drop."""
    unknown = set(counts) - set(protocol.DROP_REASONS)
    if unknown:
        raise ValueError(f"not drop reasons: {sorted(unknown)}")
    if any(type(n) is not int or n < 0 for n in counts.values()):
        raise ValueError("drop counts must be non-negative integers")
    pairs = [f"{r}={counts[r]}" for r in protocol.DROP_REASONS if counts.get(r, 0) > 0]
    total = sum(counts.values())
    if total == 0:
        raise ValueError("nothing was dropped")
    return format_line("DROPPED", str(total), ",".join(pairs))


# ---------------------------------------------------------------- shared lookups


def load_cached_record(member: config.Member) -> protocol.TeamRecord:
    """The team record last verified for `member`, re-checked; `Untrusted` (exit 10) when
    there is none yet, it fails §8, or it gives this member no role."""
    return inbox.load_cached_record(member)


def _one_member(environ: Mapping[str, str], team: str | None) -> config.Member:
    (member,) = config.load_active(environ, team=team, single=True)
    return member


def _team_active(environ: Mapping[str, str], team: str | None) -> bool:
    return team is not None or environ.get("PINGBUS_TEAMS", "") != ""


# ---------------------------------------------------------------- version


def cmd_version(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    out.write(f"{PROG} {TOOL_VERSION} protocol {protocol.PROTOCOL_VERSION}\n")
    return EXIT_OK


# ---------------------------------------------------------------- validate


def check_request_grammar(verb: str, ref: str | None, re_: str | None) -> None:
    """The checks of §5 and §6 that need no team record: the verb, the `ref` form the
    verb allows, and `re`. Raises `Refusal`. Targets, allowlists and roles need one."""
    rule = protocol.VERBS.get(verb)
    if rule is None:
        raise protocol.Refusal("verb")
    protocol.check_ref_form(ref, rule)
    protocol.check_re(re_, rule)


def _validate_request(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    ref = None if args.ref is None else protocol.lowercase_ref_repo(args.ref)
    try:
        if _team_active(environ, args.team):
            member = _one_member(environ, args.team)
            ctx = load_cached_record(member).context(member.server_name)
            # Addressed to itself, the ping exercises every §4-§6 and §11 rule `send` runs.
            protocol.validate_content(protocol.build_ping(args.verb, [member.user_id], ref, args.re), ctx)
            protocol.check_role(args.verb, member.user_id, ctx)
        else:
            err.write(f"{PROG}: no active team: grammar only; targets, allowlists and roles not checked\n")
            check_request_grammar(args.verb, ref, args.re)
    except protocol.Refusal as refusal:
        out.write(f"{refusal.reason}\n")
        return EXIT_REFUSED
    out.write("OK\n")
    return EXIT_OK


def _read_event_file(path: str) -> bytes:
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC)
    except OSError as exc:
        raise config.UsageError(f"validate --event: {path}: {exc.strerror}") from None
    with os.fdopen(fd, "rb") as handle:
        if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
            raise config.UsageError(f"validate --event: {path}: not a regular file")
        return handle.read(EVENT_FILE_MAX_BYTES + 1)


def _validate_event(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    if not _team_active(environ, args.team):
        raise config.ConfigError(
            "validate --event needs an active team (PINGBUS_TEAMS): the sender's class comes "
            "from that team's record"
        )
    member = _one_member(environ, args.team)
    ctx = load_cached_record(member).context(member.server_name)
    raw = _read_event_file(args.event)
    if len(raw) > EVENT_FILE_MAX_BYTES:
        out.write("size\n")
        return EXIT_REFUSED
    try:
        event = json.loads(raw)
    except (UnicodeDecodeError, ValueError):
        out.write("schema\n")
        return EXIT_REFUSED
    outcome = protocol.validate_event(event, ctx, member.user_id, human_text=member.human_text)
    if outcome.kind == protocol.DROP:
        out.write(f"{outcome.reason}\n")
        return EXIT_REFUSED
    if outcome.kind == protocol.IGNORE:
        out.write(f"ignore {outcome.reason}\n")
        return EXIT_OK
    out.write("OK\n")
    return EXIT_OK


def cmd_validate(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    if args.event is not None:
        if args.verb is not None or args.ref is not None or args.re is not None:
            raise config.UsageError("validate --event FILE takes no VERB, REF or --re")
        return _validate_event(args, environ, out, err)
    if args.verb is None:
        raise config.UsageError("validate needs VERB [REF] [--re EVENT_ID], or --event FILE")
    return _validate_request(args, environ, out, err)


# ---------------------------------------------------------------- config check


def _check_forge_token_file(path: str) -> None:
    where = f"{FORGE_TOKEN_FILE_VAR} ({path})"
    if not os.path.isabs(path):
        raise config.ConfigError(f"{where} must be an absolute path")
    with config.open_private_file(path, where) as handle:
        size = os.fstat(handle.fileno()).st_size
    if size == 0:
        raise config.ConfigError(f"{where}: empty")


def forge_credential_source(environ: Mapping[str, str]) -> str:
    """The name of the variable the forge credential would come from (§6), never its
    value; `none` for public repositories only. A named token file must be usable."""
    path = environ.get(FORGE_TOKEN_FILE_VAR, "")
    if path:
        _check_forge_token_file(path)
        return FORGE_TOKEN_FILE_VAR
    for name in FORGE_TOKEN_VARS:
        if environ.get(name, ""):
            return name
    return FORGE_NONE


def cmd_config_check(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    members = config.load_active(environ, team=args.team)
    source = forge_credential_source(environ)
    for member in members:
        human_text = "true" if member.human_text else "false"
        out.write(f"OK\t{member.team}\t{member.handle}\thuman_text={human_text}\n")
    out.write(f"FORGE\t{source}\n")
    return EXIT_OK


# ---------------------------------------------------------------- suggest-handle


def checkout_origin(cwd: str) -> tuple[str, str | None]:
    """The checkout's top directory (or `cwd` outside one) and its `origin` URL, if any.
    Git runs in the C locale: "not a repository" is recognised by its English message."""
    git_env = {**os.environ, "LC_ALL": "C"}
    try:
        top = subprocess.run(
            ["git", "-C", cwd, "rev-parse", "--show-toplevel"],
            capture_output=True, text=True, check=False, env=git_env,
        )
    except FileNotFoundError:
        raise config.ConfigError("suggest-handle needs git to read the checkout's remote") from None
    if top.returncode != 0:
        if "not a git repository" in top.stderr:
            return cwd, None
        raise config.ConfigError(f"git rev-parse failed: {top.stderr.strip()}")
    top_dir = top.stdout.strip()
    remote = subprocess.run(
        ["git", "-C", top_dir, "remote", "get-url", "origin"],
        capture_output=True, text=True, check=False, env=git_env,
    )
    if remote.returncode == 0:
        return top_dir, remote.stdout.strip()
    if remote.returncode == 2:  # git: no such remote
        return top_dir, None
    raise config.ConfigError(f"git remote get-url origin failed: {remote.stderr.strip()}")


def _detect_virt(*flags: str) -> subprocess.CompletedProcess:
    try:
        return subprocess.run(
            ["systemd-detect-virt", *flags], capture_output=True, text=True, check=False
        )
    except FileNotFoundError:
        raise config.ConfigError(
            "suggest-handle needs systemd-detect-virt to tell a container or VM from a host"
        ) from None


def detect_member_type(environ: Mapping[str, str]) -> str:
    """Where this session runs (§3 `<type>`): a container's own markers, then
    systemd-detect-virt's container check (LXC sets `container` only for PID 1 and writes
    no marker), then its VM check. A container of no §3 type is refused, never `host`."""
    value = environ.get("container", "")
    if value in CONTAINER_ENV_TYPES:
        return value
    if os.path.exists(PODMAN_MARKER):
        return "podman"
    if os.path.exists(DOCKER_MARKER):
        return "docker"
    container = _detect_virt("--container")
    if container.returncode == 0:
        name = container.stdout.strip()
        member_type = DETECT_VIRT_CONTAINER_TYPES.get(name)
        if member_type is None:
            raise config.ConfigError(
                f"systemd-detect-virt reports a {name!r} container, which has no handle type; "
                "pass --type to agent-bus add-member"
            )
        return member_type
    if container.returncode != 1:
        raise config.ConfigError(
            f"systemd-detect-virt --container failed with status {container.returncode}"
        )
    probe = _detect_virt("--vm", "--quiet")
    if probe.returncode == 0:
        return "vm"
    if probe.returncode == 1:
        return "host"
    raise config.ConfigError(f"systemd-detect-virt --vm failed with status {probe.returncode}")


def cmd_suggest_handle(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    role = environ.get(ROLE_VAR, "")
    if not role:
        raise config.ConfigError(
            f"{ROLE_VAR} is not set, so this install has no role; a handle never takes the "
            "host name, because handles reach public forge text. Set the role, or pass "
            "--host to agent-bus add-member"
        )
    try:
        host = registry.resolve_host(None, role)
        top, remote = checkout_origin(os.getcwd())
        repo = registry.repo_from_remote(remote, top)
    except registry.HandleError as exc:
        raise config.ConfigError(str(exc)) from None
    member_type = detect_member_type(environ)
    out.write(f"--repo={repo} --host={host} --type={member_type}\n")
    return EXIT_OK


# ---------------------------------------------------------------- the network commands

#: How long `wait` lets a `recv` that holds the lock for its one sync finish, before it
#: calls the team busy: a `recv` holder is not a waker (§12).
RECV_HOLD_WAIT_S = 5.0
RECV_HOLD_RETRY_S = 0.1
#: `say` reads at most the §7 limit, one trailing newline and one byte more: anything
#: longer is refused (`size`) without reading the rest.
SAY_READ_MAX = protocol.MAX_AGENT_TEXT_BYTES + 2
#: The send gate (§10) keys a send by verb, ref, re and targets. An agent text has no verb
#: or ref, so it is keyed by this pseudo-verb and a digest of the text: the same text to the
#: same humans within the window is a duplicate, and the text itself is never kept at rest.
SAY_GATE_VERB = "text"


def _now_ms() -> int:
    return int(time.time() * 1000)


@dataclasses.dataclass
class Runtime:
    """What the network commands take from their surroundings; tests replace the parts.

    `forge_for(member, environ)` gives the per-record forge factory (`syncer.forge_factory`);
    `clock_ms` is wall-clock milliseconds for limits and ack deadlines; `sleep` serves the
    client's 429 retries and the lock retries; `monotonic` times `wait`'s deadline and its
    wait for a `recv` lock holder; `long_poll_ms` caps each `/sync` of `wait`; `tick_s` is how often
    `wait` looks for a TIMEOUT falling due while nothing arrives; `threads` collects
    `wait`'s and `watch`'s long-poll threads; `notice_interval_s` spaces the watcher's
    notices; `spawn` starts the watcher from the SessionStart hook."""

    forge_for: Callable[
        [config.Member, Mapping[str, str]], Callable[[protocol.TeamRecord], forge.Forge]
    ] = syncer.forge_factory
    clock_ms: Callable[[], int] = _now_ms
    sleep: Callable[[float], object] = time.sleep
    monotonic: Callable[[], float] = time.monotonic
    long_poll_ms: int = limits.SYNC_LONG_POLL_S * 1000
    tick_s: float = 1.0
    threads: list[threading.Thread] = dataclasses.field(default_factory=list)
    notice_interval_s: float = notify.NOTICE_MIN_INTERVAL_S
    spawn: Callable[[Mapping[str, str], pathlib.Path], object] = hooks.spawn_watcher


class _LockedStream:
    """A text stream several threads write whole lines to."""

    def __init__(self, stream: TextIO) -> None:
        self._stream = stream
        self._lock = threading.Lock()

    def write(self, text: str) -> None:
        with self._lock:
            self._stream.write(text)


#: Failures a command reports with their §14 exit code; anything else is a bug (exit 1).
FAILURES = (
    config.UsageError, config.ConfigError, inbox.StateError, protocol.Untrusted,
    protocol.Refusal, limits.RateLimited, forge.ForgeError, matrix.MatrixError, inbox.Busy,
)


def failure(exc: BaseException) -> tuple[int, str]:
    """The exit code and the one-line message for a `FAILURES` exception. No message
    carries anything an event, a stdin text or a token held: refusals name a reason code."""
    if isinstance(exc, config.UsageError):
        return EXIT_USAGE, str(exc)
    if isinstance(exc, (config.ConfigError, inbox.StateError)):
        return EXIT_CONFIG, str(exc)
    if isinstance(exc, protocol.Untrusted):
        return EXIT_UNTRUSTED, f"the team room is not trusted: {exc}"
    if isinstance(exc, protocol.Refusal):
        return EXIT_REFUSED, f"refused: {exc.reason}"
    if isinstance(exc, limits.RateLimited):
        return EXIT_RATE, f"rate limited: {exc.reason}"
    if isinstance(exc, forge.ForgeError):
        return exc.exit_code, f"forge check refused: {exc}"
    if isinstance(exc, matrix.MatrixError):
        return exc.exit_code, f"homeserver: {exc}"
    if isinstance(exc, inbox.Busy):
        return EXIT_BUSY, str(exc)
    raise TypeError(f"not a reported failure: {type(exc).__name__}")


@dataclasses.dataclass
class _Seat:
    """One active team's member with its clients, state and sync engine. `fetcher`, for
    the commands that read the inbox (`reads`), is a second client, so `wait`'s main
    thread re-fetches while a long-poll thread syncs."""

    member: config.Member
    client: matrix.Client
    fetcher: matrix.Client | None
    state: inbox.TeamState
    syncer: syncer.Syncer


def _seat(member: config.Member, environ: Mapping[str, str], rt: Runtime, err: TextIO,
          *, reads: bool = False) -> _Seat:
    client = matrix.Client.for_member(member, sleep=rt.sleep)
    fetcher = matrix.Client.for_member(member, sleep=rt.sleep) if reads else None
    state = inbox.TeamState.for_member(member)
    engine = syncer.Syncer(member, client, state, forge_for=rt.forge_for(member, environ),
                           clock_ms=rt.clock_ms, log=lambda text: err.write(f"{PROG}: {text}\n"))
    return _Seat(member, client, fetcher, state, engine)


def _user_ids(names: str, server_name: str) -> list[str]:
    """`--to`'s comma-separated handles, human localparts or full user IDs, as user IDs.
    Nothing is judged here: the validator refuses whatever is not allowed (`target`)."""
    return [name if name.startswith("@") else f"@{name}:{server_name}" for name in names.split(",")]


def _orchestrators(record: protocol.TeamRecord, self_user_id: str) -> list[str]:
    found = sorted(user for user, role in record.roles.items()
                   if role == protocol.ROLE_ORCHESTRATOR and user != self_user_id)
    if not found:
        raise protocol.Refusal("target")
    return found


def cmd_send(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """§9 on send, steps 1-7, in order; the first failure sends nothing."""
    rt: Runtime = args.runtime
    member = _one_member(environ, args.team)
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    ctx = record.context(member.server_name)
    to = (_orchestrators(record, member.user_id) if args.to_orchestrator
          else _user_ids(args.to, member.server_name))
    ref = None if args.ref is None else protocol.lowercase_ref_repo(args.ref)
    content = protocol.build_ping(args.verb, to, ref, args.re)
    ping = protocol.validate_content(content, ctx)
    protocol.check_role(args.verb, member.user_id, ctx)
    now = rt.clock_ms()
    seat.state.admit_send(member.limits, args.verb, ref, args.re, ping.to, now)
    if ping.ref is not None:
        forge.check_ping(ping, record, rt.forge_for(member, environ)(record))
    event_id = seat.client.send_message(member.room, content)
    with seat.state.outbox() as box:
        box.record_sent(event_id, args.verb, ref, ping.to, now)
    emit(sent_line(member.team, event_id), out, err)
    return EXIT_OK


def _read_text(stdin: BinaryIO) -> str:
    """`say`'s text: stdin as UTF-8, less one trailing newline (what `echo` adds)."""
    raw = stdin.read(SAY_READ_MAX)
    if raw.endswith(b"\n"):
        raw = raw[:-1]
    if len(raw) > protocol.MAX_AGENT_TEXT_BYTES:
        raise protocol.Refusal("size")
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError:
        raise protocol.Refusal("schema") from None


def cmd_say(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """§9 on send for an agent text: steps 1 and 2, §7's rules and the role in place of
    3, 4 and 6, the member's own token refused as `secret`, then 5 and 7."""
    rt: Runtime = args.runtime
    member = _one_member(environ, args.team)
    text = _read_text(args.stdin)
    seat = _seat(member, environ, rt, err)
    ctx = seat.syncer.verify_room().context(member.server_name)
    content = protocol.build_text(_user_ids(args.to, member.server_name), text)
    said = protocol.validate_text_content(content, ctx)
    if config.read_token(member) in text:
        raise protocol.Refusal("secret")
    protocol.check_text_sender(member.user_id, ctx)
    digest = "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()
    seat.state.admit_send(member.limits, SAY_GATE_VERB, digest, None, said.to, rt.clock_ms())
    event_id = seat.client.send_message(member.room, content)
    emit(sent_line(member.team, event_id), out, err)
    return EXIT_OK


def _refetch(seat: _Seat, ctx: protocol.Context, event_id: str) -> tuple[protocol.Outcome, object]:
    """§9 reading the inbox: the item as the homeserver serves it, re-validated; and
    its sender, for a drop line. A copy the server no longer has is a `schema` drop."""
    member = seat.member
    if seat.fetcher is None:
        raise TypeError("a seat that reads the inbox is built with reads=True")
    try:
        event = seat.fetcher.get_event(member.room, event_id)
    except matrix.NotFound:
        return protocol.Outcome(protocol.DROP, "schema"), None
    if event.get("event_id") != event_id:
        return protocol.Outcome(protocol.DROP, "schema"), None
    outcome = protocol.validate_event(event, ctx, member.user_id, human_text=member.human_text)
    return outcome, event.get("sender")


def _deliver(seat: _Seat, record: protocol.TeamRecord, rt: Runtime, out: TextIO,
             err: TextIO) -> tuple[int, collections.Counter[str]]:
    """Print and consume every pending item from its re-fetched copy, then every TIMEOUT
    due. (lines, what failed on re-fetch: the caller adds it to the batch's DROPPED line)"""
    member, state = seat.member, seat.state
    ctx = record.context(member.server_name)
    now = rt.clock_ms()
    printed = 0
    drops: collections.Counter[str] = collections.Counter()
    for item in state.pending(ctx, member.user_id, human_text=member.human_text).items:
        outcome, sender = _refetch(seat, ctx, item.event_id)
        if outcome.kind == protocol.ACCEPT:
            if outcome.ping is not None:
                line = ping_line(member.team, member.server_name, outcome.ping)
            else:
                line = human_line(member.team, member.server_name, outcome.human)
            emit(line, out, err)
            printed += 1
        elif outcome.kind == protocol.DROP:
            state.log_drop(member.team, item.event_id, sender, outcome.reason, now)
            drops[outcome.reason] += 1
        state.consume(item.event_id)
    with state.outbox() as box:
        due = box.due_timeouts(now, member.limits)
        for timeout in due:
            emit(timeout_line(member.team, member.server_name, timeout.event_id, timeout.target,
                              timeout.verb, timeout.ref), out, err)
        box.mark_reported(due)
    return printed + len(due), drops


def _recv_team(member: config.Member, environ: Mapping[str, str], rt: Runtime,
               out: TextIO, err: TextIO) -> tuple[int, int]:
    """One team's `recv`: sync once when the lock is free, then deliver; one DROPPED line
    for the sync's and the re-fetch's drops together. (lines, drops)"""
    seat = _seat(member, environ, rt, err, reads=True)
    drops: collections.Counter[str] = collections.Counter()
    try:
        lock = inbox.acquire_lock(seat.state, "recv", sleep=rt.sleep)
    except inbox.Busy as busy:
        err.write(f"{PROG}: team {member.team}: not syncing: a {busy.holder} process holds "
                  "the sync lock; reading the inbox\n")
        record = load_cached_record(member)
    else:
        with lock:
            batch = seat.syncer.sync_once(0)
        record = seat.syncer.record
        drops.update(batch.drops)
    printed, refetched = _deliver(seat, record, rt, out, err)
    drops.update(refetched)
    if drops:
        emit(dropped_line(drops), out, err)
    return printed, sum(drops.values())


def cmd_recv(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Every active team (or `--team`), in `PINGBUS_TEAMS` order. A team that fails is
    reported and the others still run. Lines printed are consumed, so they make exit 0
    whatever else failed; with none, the exit code is the first failure's."""
    rt: Runtime = args.runtime
    printed = dropped = 0
    first_failure: int | None = None
    for member in config.load_active(environ, team=args.team):
        try:
            lines, drops = _recv_team(member, environ, rt, out, err)
        except FAILURES as exc:
            code, message = failure(exc)
            err.write(f"{PROG}: team {member.team}: {message}\n")
            if first_failure is None:
                first_failure = code
            continue
        printed += lines
        dropped += drops
    if printed:
        return EXIT_OK
    if first_failure is not None:
        return first_failure
    return EXIT_DROPPED if dropped else EXIT_NOTHING


def _take_waiter_lock(state: inbox.TeamState, rt: Runtime, kind: str = "wait") -> inbox.Lock:
    """The team's sync lock as a waker (`wait` or `watch`). A `recv` holds it only for one
    sync, so that holder is waited for (up to `RECV_HOLD_WAIT_S`); a waker is `Busy`."""
    give_up = rt.monotonic() + RECV_HOLD_WAIT_S
    while True:
        try:
            return inbox.acquire_lock(state, kind, sleep=rt.sleep)
        except inbox.Busy as busy:
            if busy.holder != "recv" or rt.monotonic() >= give_up:
                raise
        rt.sleep(RECV_HOLD_RETRY_S)


@dataclasses.dataclass
class _Waiting:
    """What `wait`'s long-poll threads hand to its main thread."""

    deadline: float
    woke: threading.Event = dataclasses.field(default_factory=threading.Event)
    stop: threading.Event = dataclasses.field(default_factory=threading.Event)
    drops: queue.SimpleQueue = dataclasses.field(default_factory=queue.SimpleQueue)
    failures: list[tuple[str, Exception]] = dataclasses.field(default_factory=list)


def _long_poll(seat: _Seat, lock: inbox.Lock, rt: Runtime, waiting: _Waiting) -> None:
    """One team's thread: long-poll until stopped or the deadline, waking the main thread
    on every batch that stored an item. It owns `lock` and releases it when it ends. A
    failure is handed to the main thread, which reports it and exits with its code."""
    try:
        while not waiting.stop.is_set():
            remaining_ms = int((waiting.deadline - rt.monotonic()) * 1000)
            if remaining_ms <= 0:
                return
            batch = seat.syncer.sync_once(min(rt.long_poll_ms, remaining_ms))
            if batch.drops:
                waiting.drops.put((seat.member.team, batch.drops))
            if batch.accepted:
                waiting.woke.set()
    except Exception as exc:
        waiting.failures.append((seat.member.team, exc))
        waiting.woke.set()
    finally:
        lock.release()


_Drops = dict[str, collections.Counter[str]]


def _collect_drops(waiting: _Waiting, pending: _Drops) -> None:
    """Move the long-poll threads' drop counts into `pending`, by team."""
    while not waiting.drops.empty():
        team, counts = waiting.drops.get()
        pending.setdefault(team, collections.Counter()).update(counts)


def _emit_drops(pending: _Drops, out: TextIO, err: TextIO) -> None:
    for counts in pending.values():
        if counts:
            emit(dropped_line(counts), out, err)
    pending.clear()


def _deliver_all(seats: Sequence[_Seat], pending: _Drops, rt: Runtime, out: TextIO,
                 err: TextIO) -> int:
    """One pass of `wait` over its teams: deliver each trusted team's items, then one
    DROPPED line per team for its synced and re-fetched drops together. Each record is
    read once: a long-poll thread may lose trust (record None) at any moment. (lines)"""
    printed = 0
    for seat in seats:
        record = seat.syncer.record
        if record is None:
            continue
        lines, refetched = _deliver(seat, record, rt, out, err)
        printed += lines
        if refetched:
            pending.setdefault(seat.member.team, collections.Counter()).update(refetched)
    _emit_drops(pending, out, err)
    return printed


def cmd_wait(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Hold every active team's lock as a waiter (a busy team is reported and skipped;
    exit 75 only when every team is busy), long-poll them all at once, one thread per
    team, and exit on the first batch that prints a line, or at the timeout (exit 3).
    Drops are reported on stderr and never end the wait."""
    rt: Runtime = args.runtime
    err = _LockedStream(err)
    members = config.load_active(environ, team=args.team)
    if args.timeout is None:
        timeout = min(member.limits.wait_timeout_s for member in members)
    else:
        try:
            timeout = limits.check_wait_timeout(args.timeout)
        except limits.LimitsError as exc:
            raise config.UsageError(f"wait --timeout: {exc}") from None
    waiting = _Waiting(rt.monotonic() + timeout)
    held: list[tuple[_Seat, inbox.Lock]] = []
    pending: _Drops = {}
    handed_over: set[str] = set()
    try:
        for member in members:
            seat = _seat(member, environ, rt, err, reads=True)
            try:
                held.append((seat, _take_waiter_lock(seat.state, rt)))
            except inbox.Busy as busy:
                err.write(f"{PROG}: team {member.team}: busy: a {busy.holder} process holds the "
                          "sync lock\n")
        if not held:
            err.write(f"{PROG}: every active team is busy: another process is already waiting\n")
            return EXIT_BUSY
        seats = [seat for seat, _ in held]
        for seat in seats:
            batch = seat.syncer.sync_once(0)
            if batch.drops:
                pending.setdefault(seat.member.team, collections.Counter()).update(batch.drops)
        if _deliver_all(seats, pending, rt, out, err):
            return EXIT_OK
        until = rt.clock_ms() + timeout * 1000
        for seat in seats:
            seat.syncer.publish_status(until)
        for seat, lock in held:
            thread = threading.Thread(target=_long_poll, args=(seat, lock, rt, waiting), daemon=True,
                                      name=f"pingbus-wait-{seat.member.team}")
            rt.threads.append(thread)
            handed_over.add(seat.member.team)
            thread.start()
        while True:
            waiting.woke.wait(max(0.0, min(waiting.deadline - rt.monotonic(), rt.tick_s)))
            waiting.woke.clear()
            _collect_drops(waiting, pending)
            if waiting.failures:
                _emit_drops(pending, out, err)
                team, exc = waiting.failures[0]
                if not isinstance(exc, FAILURES):
                    raise exc
                code, message = failure(exc)
                err.write(f"{PROG}: team {team}: {message}\n")
                return code
            if _deliver_all(seats, pending, rt, out, err):
                return EXIT_OK
            if rt.monotonic() >= waiting.deadline:
                err.write(f"{PROG}: nothing within {timeout} s: run wait again to re-arm\n")
                return EXIT_NOTHING
    finally:
        waiting.stop.set()
        for seat, lock in held:
            if seat.member.team not in handed_over:
                lock.release()


# ---------------------------------------------------------------- watch

#: How long the watcher's `listening` status (§8) runs, and how often it renews it.
WATCH_STATUS_TTL_MS = 600_000
WATCH_STATUS_REFRESH_MS = 300_000


def _pending_view(seats: Sequence[_Seat]) -> tuple[tuple[int, int, int], set[str]]:
    """(total, from humans, pings) pending over every trusted team, re-validated offline,
    and the pending items' `team:event ID` names (what the notifier compares)."""
    total = humans = pings = 0
    ids: set[str] = set()
    for seat in seats:
        record = seat.syncer.record
        if record is None:
            continue
        member = seat.member
        pending = seat.state.pending(record.context(member.server_name), member.user_id,
                                     human_text=member.human_text)
        t, h, p = pending.counts()
        total, humans, pings = total + t, humans + h, pings + p
        ids.update(f"{member.team}:{item.event_id}" for item in pending.items)
    return (total, humans, pings), ids


def _watch_poll(seat: _Seat, lock: inbox.Lock, rt: Runtime, waiting: _Waiting, status_ms: int) -> None:
    """One team's watcher thread: long-poll until stopped, waking the main thread on every
    batch that stored an item, and renewing the `listening` status. It owns `lock` and
    releases it when it ends; a failure is handed to the main thread."""
    try:
        while not waiting.stop.is_set():
            batch = seat.syncer.sync_once(rt.long_poll_ms)
            if batch.drops:
                waiting.drops.put((seat.member.team, batch.drops))
            if batch.accepted:
                waiting.woke.set()
            now = rt.clock_ms()
            if now - status_ms >= WATCH_STATUS_REFRESH_MS:
                seat.syncer.publish_status(now + WATCH_STATUS_TTL_MS)
                status_ms = now
    except Exception as exc:
        waiting.failures.append((seat.member.team, exc))
        waiting.woke.set()
    finally:
        lock.release()


def _session_socket(environ: Mapping[str, str]) -> tuple[str, str]:
    try:
        session = notify.session_socket(environ)
    except ValueError as exc:
        raise config.ConfigError(str(exc)) from None
    if session is None:
        raise config.ConfigError(
            f"watch needs the session's inbox socket ({notify.SOCKET_ENV}): the SessionStart "
            "hook starts it; without the socket, run `pingbus wait` in the background"
        )
    return session


def cmd_watch(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Hold every active team's lock as a watcher (a busy team is reported and skipped;
    exit 75 only when every team is busy), keep the inbox filled, and write a counts-only
    notice to the session's socket whenever a new item is pending. Exits 0 when the socket
    goes (the session ended). A team that fails is reported and dropped, and the others go
    on; once no team is left, it exits with the first failure's code."""
    rt: Runtime = args.runtime
    err = _LockedStream(err)
    path, token = _session_socket(environ)
    members = config.load_active(environ, team=args.team)
    if not notify.socket_present(path):
        err.write(f"{PROG}: the session socket is gone: nothing to wake\n")
        return EXIT_OK
    waiting = _Waiting(float("inf"))
    held: list[tuple[_Seat, inbox.Lock]] = []
    pending: _Drops = {}
    handed_over: set[str] = set()
    try:
        for member in members:
            seat = _seat(member, environ, rt, err)
            try:
                held.append((seat, _take_waiter_lock(seat.state, rt, "watch")))
            except inbox.Busy as busy:
                err.write(f"{PROG}: team {member.team}: busy: a {busy.holder} process holds the "
                          "sync lock\n")
        if not held:
            err.write(f"{PROG}: every active team is busy: another watcher or waiter holds the seat\n")
            return EXIT_BUSY
        seats = [seat for seat, _ in held]
        for seat in seats:
            batch = seat.syncer.sync_once(0)
            if batch.drops:
                pending.setdefault(seat.member.team, collections.Counter()).update(batch.drops)
        _emit_drops(pending, out, err)
        status_ms = rt.clock_ms()
        for seat in seats:
            seat.syncer.publish_status(status_ms + WATCH_STATUS_TTL_MS)
        notifier = notify.Notifier(lambda text: notify.send_notice(path, token, text),
                                   first_number=max(1, rt.clock_ms()), clock=rt.monotonic,
                                   min_interval_s=rt.notice_interval_s)
        for seat, lock in held:
            thread = threading.Thread(target=_watch_poll, args=(seat, lock, rt, waiting, status_ms),
                                      daemon=True, name=f"pingbus-watch-{seat.member.team}")
            rt.threads.append(thread)
            handed_over.add(seat.member.team)
            thread.start()
        reported = 0
        first_failure: int | None = None
        while True:
            if not notify.socket_present(path):
                err.write(f"{PROG}: the session socket is gone: the watcher exits\n")
                return EXIT_OK
            try:
                notifier.observe(*_pending_view(seats))
            except notify.SocketGone as gone:
                err.write(f"{PROG}: {gone}: the watcher exits\n")
                return EXIT_OK
            waiting.woke.wait(rt.tick_s)
            waiting.woke.clear()
            _collect_drops(waiting, pending)
            _emit_drops(pending, out, err)
            while len(waiting.failures) > reported:
                team, exc = waiting.failures[reported]
                reported += 1
                if not isinstance(exc, FAILURES):
                    raise exc
                code, message = failure(exc)
                err.write(f"{PROG}: team {team}: {message}: this team is no longer watched\n")
                first_failure = first_failure or code
                seats = [seat for seat in seats if seat.member.team != team]
            if not seats:
                err.write(f"{PROG}: no team is left to watch: the watcher exits\n")
                return first_failure
    finally:
        waiting.stop.set()
        for seat, lock in held:
            if seat.member.team not in handed_over:
                lock.release()


# ---------------------------------------------------------------- hooks

HOOK_INPUT_MAX = hooks.INPUT_MAX_BYTES


def cmd_hook(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """A Claude Code hook: stdin hook JSON, stdout hook JSON, always exit 0 (§13)."""
    rt: Runtime = args.runtime
    raw = args.stdin.read(HOOK_INPUT_MAX + 1)
    result = hooks.run(args.event, raw, environ, now_ms=rt.clock_ms(), spawn=rt.spawn, err=err)
    out.write(json.dumps(result, ensure_ascii=True) + "\n")
    return EXIT_OK


# ---------------------------------------------------------------- inbox and status (offline)

_REPORT_FIELD_RE = re.compile(r"[\x21-\x7e]{1,255}|[\x20-\x7e]{1,200}")


def report_line(kind: str, *fields: str) -> str:
    """One report line: `kind` and its fields, tab-separated. Each field has passed its own
    grammar already; this refuses anything that could add a field or a line."""
    for value in fields:
        if not isinstance(value, str) or not _REPORT_FIELD_RE.fullmatch(value):
            raise ValueError(f"{kind}: a field is not printable")
    return "\t".join((kind, *fields))


def _sender_localpart(outcome: protocol.Outcome, server_name: str) -> str:
    item = outcome.ping or outcome.human
    return _localpart(item.sender, server_name)


def cmd_inbox(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Every pending item, oldest first, without consuming it: team, event ID, sender,
    time, and the verb or `human`. Never the text: only `recv` prints that, from the copy it
    re-fetches. A team that fails is reported and the others still run."""
    first_failure: int | None = None
    for member in config.load_active(environ, team=args.team):
        try:
            record = load_cached_record(member)
            state = inbox.TeamState.for_member(member)
            items = state.pending(record.context(member.server_name), member.user_id,
                                  human_text=member.human_text).items
        except FAILURES as exc:
            code, message = failure(exc)
            err.write(f"{PROG}: team {member.team}: {message}\n")
            first_failure = first_failure or code
            continue
        for item in items:
            kind = _verb(item.outcome.ping.verb) if item.outcome.ping else "human"
            out.write(report_line(
                "PENDING", _team(member.team), _event_id(item.event_id),
                _sender_localpart(item.outcome, member.server_name), str(int(item.origin_server_ts)),
                kind) + "\n")
    return EXIT_OK if first_failure is None else first_failure


WAKE_NAMES = {"watch": "watcher", "wait": "waiter"}


def _member_status(user_id: str, statuses: Mapping[str, int], now_ms: int) -> str:
    until = statuses.get(user_id)
    if until is None:
        return ABSENT
    return protocol.STATUS_LISTENING if until > now_ms else "stale"


def _overdue(state: inbox.TeamState, member: config.Member, now_ms: int) -> int:
    """TIMEOUTs owed and not yet reported, read without taking the outbox lock (it is
    replaced atomically) and without creating anything."""
    path = state.path / inbox.OUTBOX_FILE
    box = inbox.Outbox.from_state(inbox.read_json_file(path), path)
    return len(box.due_timeouts(now_ms, member.limits))


def _status_team(member: config.Member, now_ms: int, out: TextIO) -> None:
    state = inbox.TeamState.for_member(member)
    wake = WAKE_NAMES.get(inbox.probe_lock(state) or "", "none")
    members, statuses = state.room_view()
    overdue, dropped = _overdue(state, member, now_ms), state.drop_count()
    try:
        record: protocol.TeamRecord | None = load_cached_record(member)
    except protocol.Untrusted:
        record = None
    fields = [f"wake={wake}"]
    if record is None:
        fields += ["pending=-", "humans=-", "pings=-"]
        unexpected: list[str] = []
    else:
        total, humans, pings = state.pending(record.context(member.server_name), member.user_id,
                                             human_text=member.human_text).counts()
        fields += [f"pending={total}", f"humans={humans}", f"pings={pings}"]
        known = set(record.humans) | set(record.roles) | {member.admin}
        unexpected = sorted(uid for uid in members if uid not in known)
    fields += [f"overdue={overdue}", f"dropped={dropped}",
               "unexpected=-" if record is None else f"unexpected={len(unexpected)}"]
    trust = "trust=trusted" if record is not None else "trust=untrusted"
    out.write(report_line("TEAM", _team(member.team), member.handle, trust, *fields) + "\n")
    if record is None:
        reason = state.untrusted_reason()
        if reason is not None:
            out.write(report_line("UNTRUSTED", member.team, reason) + "\n")
        return
    sn = member.server_name
    for uid in sorted(record.humans):
        out.write(report_line("MEMBER", member.team, _localpart(uid, sn), "human", ABSENT) + "\n")
    for uid in sorted(record.roles):
        out.write(report_line("MEMBER", member.team, _localpart(uid, sn), record.roles[uid],
                              _member_status(uid, statuses, now_ms)) + "\n")
    for uid in unexpected:  # room_view() checked each against inbox.is_user_id_text
        out.write(report_line("UNEXPECTED", member.team, uid, members[uid]) + "\n")


def cmd_status(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Per active team, offline: the handle, room trust (and why not), the wake path from
    the lock, pending, overdue and dropped counts, unexpected members; then each member of
    the last verified team record with its role or `human` and its status. A team that
    fails is reported and prints nothing; the others still run."""
    rt: Runtime = args.runtime
    now = rt.clock_ms()
    first_failure: int | None = None
    for member in config.load_active(environ, team=args.team):
        lines = io.StringIO()
        try:
            _status_team(member, now, lines)
        except FAILURES as exc:
            code, message = failure(exc)
            err.write(f"{PROG}: team {member.team}: {message}\n")
            first_failure = first_failure or code
            continue
        out.write(lines.getvalue())
    return EXIT_OK if first_failure is None else first_failure


def build_parser(stdout: TextIO) -> argparse.ArgumentParser:
    class Parser(argparse.ArgumentParser):
        """Usage errors exit 64 (never argparse's 2); help goes to the given stdout."""

        def error(self, message: str) -> None:
            raise config.UsageError(f"{self.prog}: {message}")

        def exit(self, status: int = 0, message: str | None = None) -> None:
            if message:
                raise config.UsageError(message.strip())
            raise _ParserExit(status)

        def print_help(self, file: TextIO | None = None) -> None:
            stdout.write(self.format_help())

    def team_option(parser: argparse.ArgumentParser, default: object) -> None:
        parser.add_argument("--team", metavar="NAME", default=default,
                            help="select one active team")

    parser = Parser(prog=PROG, description="Agent team bus member client (protocol "
                    f"{protocol.PROTOCOL_VERSION}).")
    team_option(parser, None)
    commands = parser.add_subparsers(dest="command", metavar="COMMAND", required=True)

    def command(name: str, handler: object, help_text: str) -> argparse.ArgumentParser:
        sub = commands.add_parser(name, help=help_text)
        team_option(sub, argparse.SUPPRESS)
        sub.set_defaults(handler=handler)
        return sub

    command("version", cmd_version, "print the tool and protocol versions")
    validate = command("validate", cmd_validate, "run the offline validator")
    validate.add_argument("verb", nargs="?", metavar="VERB")
    validate.add_argument("ref", nargs="?", metavar="REF")
    validate.add_argument("--re", metavar="EVENT_ID")
    validate.add_argument("--event", metavar="FILE", help="a timeline event, as JSON")
    config_parser = commands.add_parser("config", help="member configuration")
    team_option(config_parser, argparse.SUPPRESS)
    config_commands = config_parser.add_subparsers(dest="config_command", metavar="SUBCOMMAND",
                                                   required=True)
    check = config_commands.add_parser("check", help="validate every active team's bundle")
    team_option(check, argparse.SUPPRESS)
    check.set_defaults(handler=cmd_config_check)
    command("suggest-handle", cmd_suggest_handle,
            "print the agent-bus add-member arguments this environment implies")
    send = command("send", cmd_send, "send a ping (the only way to emit one)")
    send.add_argument("verb", metavar="VERB")
    send.add_argument("ref", nargs="?", metavar="REF")
    targets = send.add_mutually_exclusive_group(required=True)
    targets.add_argument("--to", metavar="HANDLE[,HANDLE...]",
                         help="handles, human localparts or full user IDs")
    targets.add_argument("--to-orchestrator", action="store_true",
                         help="every orchestrator in the team record")
    send.add_argument("--re", metavar="EVENT_ID")
    say = command("say", cmd_say, "send text from stdin to the team's humans")
    say.add_argument("--to", required=True, metavar="HUMAN[,HUMAN...]",
                     help="human localparts or full user IDs, never a handle")
    command("recv", cmd_recv, "sync once if the lock is free, then print every pending item")
    wait = command("wait", cmd_wait, "long-poll every active team until an item arrives")
    wait.add_argument("--timeout", type=int, metavar="S",
                      help="seconds (default: the member's wait_timeout_s)")
    command("watch", cmd_watch, "fill the inbox and notify the session socket (SessionStart starts it)")
    command("inbox", cmd_inbox, "list pending items without consuming them (offline)")
    command("status", cmd_status, "per team: trust, wake path, counts and members (offline)")
    hook = command("hook", cmd_hook, "a Claude Code hook: stdin hook JSON, stdout hook JSON")
    hook.add_argument("event", choices=hooks.EVENTS, metavar="EVENT",
                      help=" | ".join(hooks.EVENTS))
    return parser


def main(
    argv: Sequence[str] | None = None,
    *,
    environ: Mapping[str, str] | None = None,
    stdout: TextIO | None = None,
    stderr: TextIO | None = None,
    stdin: BinaryIO | None = None,
    runtime: Runtime | None = None,
) -> int:
    env = os.environ if environ is None else environ
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    try:
        config.check_python(sys.version_info)
        args = build_parser(out).parse_args(argv)
        args.stdin = sys.stdin.buffer if stdin is None else stdin
        args.runtime = Runtime() if runtime is None else runtime
        return args.handler(args, env, out, err)
    except _ParserExit as done:
        return done.status
    except FAILURES as exc:
        code, message = failure(exc)
        err.write(f"{PROG}: {message}\n")
        return code


if __name__ == "__main__":
    sys.exit(main())

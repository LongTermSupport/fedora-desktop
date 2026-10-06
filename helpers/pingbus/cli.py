"""The `pingbus` command line: dispatch, exit codes, output lines and the offline commands.

Spec: docs/agent-bus-protocol.md §13 (commands), §14 (exit codes), §15 (output lines),
§3 (`suggest-handle`), §1 (`version`). This module holds the offline commands
(`version`, `validate`, `config check`, `suggest-handle`); the network commands are added
beside them by later units and use the same dispatch, exit codes and line formatter.

Streams: stdout carries only a command's payload (the §15 stdout lines, `validate`'s
verdict, and the report commands' text); every diagnostic goes to stderr. Nothing read
from an event or a bundle is ever echoed: refusals print a reason code, and the config
errors name keys and files, never values.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import stat
import subprocess
import sys
from collections.abc import Mapping, Sequence
from typing import TextIO

from helpers.agent_bus import registry
from helpers.pingbus import config, protocol

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
    EXIT_OK: "success; `recv`/`wait` printed at least one line (drops, if any, are on stderr)",
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
TEAM_RECORD_CACHE = "team.json"
TEAM_RECORD_MAX_BYTES = 65536
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
    path = member.state_dir / TEAM_RECORD_CACHE
    where = f"team {member.team}: {path}"
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        raise protocol.Untrusted(f"{where}: no verified team record yet (not joined)") from None
    except OSError as exc:
        raise protocol.Untrusted(f"{where}: cannot be read ({exc.strerror})") from None
    with os.fdopen(fd, "rb") as handle:
        if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
            raise protocol.Untrusted(f"{where}: not a regular file")
        raw = handle.read(TEAM_RECORD_MAX_BYTES + 1)
    if len(raw) > TEAM_RECORD_MAX_BYTES:
        raise protocol.Untrusted(f"{where}: larger than {TEAM_RECORD_MAX_BYTES} bytes")
    try:
        content = json.loads(raw)
    except (UnicodeDecodeError, ValueError):
        raise protocol.Untrusted(f"{where}: not JSON") from None
    try:
        record = protocol.parse_team_record(content, member.server_name, member.team)
    except protocol.Untrusted as exc:
        raise protocol.Untrusted(f"{where}: {exc}") from None
    if record.roles.get(member.user_id) not in protocol.ROLES:
        raise protocol.Untrusted(f"{where}: the team record gives this member no role")
    return record


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


# ---------------------------------------------------------------- dispatch


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
    return parser


def main(
    argv: Sequence[str] | None = None,
    *,
    environ: Mapping[str, str] | None = None,
    stdout: TextIO | None = None,
    stderr: TextIO | None = None,
) -> int:
    env = os.environ if environ is None else environ
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    try:
        config.check_python(sys.version_info)
        args = build_parser(out).parse_args(argv)
        return args.handler(args, env, out, err)
    except _ParserExit as done:
        return done.status
    except config.UsageError as exc:
        err.write(f"{PROG}: {exc}\n")
        return EXIT_USAGE
    except config.ConfigError as exc:
        err.write(f"{PROG}: {exc}\n")
        return EXIT_CONFIG
    except protocol.Untrusted as exc:
        err.write(f"{PROG}: the team room is not trusted: {exc}\n")
        return EXIT_UNTRUSTED


if __name__ == "__main__":
    sys.exit(main())

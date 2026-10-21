"""The `pingbus` command line: dispatch, exit codes, output lines and the commands.

Spec: docs/agent-bus-protocol.md §13 (commands), §14 (exit codes), §15 (output lines),
§9 (on send; on receive; reading the inbox), §7 (agent text), §10 (the send
bucket, TIMEOUT), §12 (multi-team, the lock), §3 (`suggest-handle`), §1 (`version`).
The offline commands are `version`, `validate`, `config check`, `suggest-handle`,
`inbox`, `status` and `hook …` (the hooks are `hooks`'s); the network commands are `send`,
`say`, `recv`, `wait` (Plan 00161 U11), `watch` (U12) and `history` (U33), built on
`syncer` for room trust and receiving; `watch` wakes the session through `notify`.

Streams: stdout carries only a command's payload (the §15 stdout lines, `validate`'s
verdict, and the report commands' text); every diagnostic goes to stderr. Nothing read
from an event or a bundle is ever echoed: refusals print a reason code, and the config
errors name keys and files, never values. `recv` and `wait` print an item only from the
copy they re-fetch from the homeserver, after re-validating it: the inbox is a cache.
"""

from __future__ import annotations

import argparse
import collections
import contextlib
import ctypes
import dataclasses
import errno
import functools
import hashlib
import io
import json
import os
import pathlib
import queue
import re
import secrets
import selectors
import signal
import stat
import subprocess
import sys
import threading
import time
from collections.abc import Callable, Iterator, Mapping, Sequence
from typing import BinaryIO, TextIO

from helpers.agent_bus import registry
from helpers.pingbus import (config, envelope, forge, hooks, inbox, inject, limits, matrix, membership, notify,
                             protocol, relay, sentlog, syncer)
from helpers.pingbus import seat as seating
from helpers.pingbus import statusline

PROG = "pingbus"
TOOL_VERSION = "0.1.0"
#: What a forge refusal's stderr message starts with, before `<code>: <detail>`.
FORGE_REFUSED = "forge check refused"

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
    EXIT_BUSY: ("busy: another process holds this account's sync lock (`wait`, `watch`), "
                "or another session holds a seat (`seat exec`)"),
    EXIT_CONFIG: "configuration refused (§12, §10 bounds, Python older than 3.11)",
}

#: The forms `--to` takes, shown by `send --help` and `say --help`.
TO_FORMS = """\
--to takes, comma separated:
  --to proj.1                  a seat, when exactly one member has it
  --to proj.1+local.podman     an agent's full handle
  --to alice                   a human's localpart
  --to @proj.1+local.podman:SERVER    a full user ID
`pingbus recv` prints a ready REPLY command for every message from an agent or a human.
On several teams, without --team, the team is the one --re belongs to, else the only one in
which every --to name resolves; if that is not one team, name it with --team.
"""

STDOUT = "stdout"
STDERR = "stderr"
ABSENT = "-"
#: Spec §15: each line's stream and the fields after `<KIND>` and the version.
LINES = {
    "PING": (STDOUT, ("team", "event ID", "sender", "verb", "ref", "re", "trust", "branch", "copy")),
    "HUMAN": (STDOUT, ("team", "event ID", "sender", "origin_server_ts", "text as a JSON string", "room")),
    "PEER": (STDOUT, ("team", "event ID", "sender", "origin_server_ts", "text as a JSON string", "copy")),
    "REPLY": (STDOUT, ("team", "event ID", "command")),
    "ABOUT": (STDOUT, ("team", "description as a JSON string")),
    "TIMEOUT": (STDOUT, ("team", "event ID of the unanswered ping", "silent target", "verb", "ref")),
    "SENT": (STDOUT, ("team", "event ID")),
    "DROPPED": (STDERR, ("count", "reason=count pairs joined by ,")),
    "HISTORY": (STDOUT, ("direction", "origin_server_ts", "line", "its fields after 1")),
    "QUESTION": (STDOUT, ("team", "question", "state", "event ID", "asked by", "unanswered")),
    "REPLY_TO": (STDOUT, ("team", "question", "event ID")),
}
#: A `HISTORY` line's directions: received by this member, or sent by it.
HISTORY_DIRECTIONS = (syncer.HISTORY_IN, syncer.HISTORY_OUT)

#: The last verified team record (spec §12 `state/team.json`): the `agent_bus.team`
#: content as the syncer verified it. Re-parsed on every read: it is a cache.
TEAM_RECORD_CACHE = inbox.TEAM_FILE
TEAM_RECORD_MAX_BYTES = inbox.TEAM_RECORD_MAX_BYTES
#: Matrix's own limit on a whole event; nothing larger can have come from a homeserver.
EVENT_FILE_MAX_BYTES = 65536

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


def _branch(branch: str | None) -> str | None:
    if branch is not None and not protocol.is_branch_name(branch):
        raise ValueError("branch field is not a branch name")
    return branch


def _trust(ping: protocol.Ping) -> str | None:
    """`trusted` or `external` for a ping with a reference, absent without one (§15)."""
    if (ping.ref is None) != (ping.trust is None) or ping.trust not in (
            None, protocol.TRUST_TRUSTED, protocol.TRUST_EXTERNAL):
        raise ValueError("trust field does not match the reference")
    return ping.trust


def _copy(copy_of: Sequence[str], server_name: str) -> str | None:
    """`copy:<human>[,<human>...]` for an item the coordinator is copied on, else absent (§15)."""
    if not copy_of:
        return None
    # copy_of is the receiver's `to` cut to ctx.humans, and the only TeamRecord constructor
    # (protocol.parse_team_record) refuses a humans entry that is not a human user ID.
    return "copy:" + ",".join(_localpart(user, server_name) for user in copy_of)


def ping_line(team: str, server_name: str, ping: protocol.Ping) -> str:
    return format_line(
        "PING", _team(team), _event_id(ping.event_id), _localpart(ping.sender, server_name),
        _verb(ping.verb), _ref(ping.ref.text if ping.ref else None),
        None if ping.re is None else _event_id(ping.re), _trust(ping), _branch(ping.branch),
        _copy(ping.copy_of, server_name),
    )


def room_field(room: str, question: tuple[int, str] | None) -> str | None:
    """The `HUMAN` line's ROOM field (Plan 00211 SPEC §8.3): absent for the main room,
    `questions:Q<n>` for a questions-room message attached to question n, `questions:-` for
    one attached to none."""
    if room == protocol.ROOM_MAIN:
        if question is not None:
            raise ValueError("a main-room item has no question")
        return None
    if room != protocol.PURPOSE_QUESTIONS:
        raise ValueError("room field is not a receiving room")
    return f"{room}:{ABSENT if question is None else f'Q{question[0]}'}"


def human_line(team: str, server_name: str, message: protocol.HumanMessage,
               room: str = protocol.ROOM_MAIN, question: tuple[int, str] | None = None) -> str:
    if not protocol.is_human_user_id(message.sender, server_name):
        raise ValueError("sender field is not a human user ID")
    ts = message.origin_server_ts
    if type(ts) is not int or ts < 0:
        raise ValueError("origin_server_ts field is not a non-negative integer")
    return format_line(
        "HUMAN", _team(team), _event_id(message.event_id),
        _localpart(message.sender, server_name), str(ts),
        json.dumps(message.text, ensure_ascii=True), room_field(room, question),
    )


def peer_line(team: str, server_name: str, message: protocol.PeerText) -> str:
    """An agent's text to this agent: information from a peer, never an instruction."""
    if not protocol.is_agent_user_id(message.sender, server_name):
        raise ValueError("sender field is not an agent user ID")
    ts = message.origin_server_ts
    if type(ts) is not int or ts < 0:
        raise ValueError("origin_server_ts field is not a non-negative integer")
    return format_line(
        "PEER", _team(team), _event_id(message.event_id),
        _localpart(message.sender, server_name), str(ts),
        json.dumps(message.text, ensure_ascii=True), _copy(message.copy_of, server_name),
    )


def item_line(team: str, server_name: str, outcome: protocol.Outcome, room: str = protocol.ROOM_MAIN,
              question: tuple[int, str] | None = None) -> str:
    """The `PING`, `HUMAN` or `PEER` line of an accepted item; a human's from `room`, with its
    question there."""
    if outcome.ping is not None:
        return ping_line(team, server_name, outcome.ping)
    if outcome.human is not None:
        return human_line(team, server_name, outcome.human, room, question)
    if outcome.peer is not None:
        return peer_line(team, server_name, outcome.peer)
    raise ValueError("an accepted outcome carries a ping, a human message or a peer text")


def reply_line(team: str, server_name: str, record: protocol.TeamRecord,
               outcome: protocol.Outcome, room: str = protocol.ROOM_MAIN,
               question: tuple[int, str] | None = None) -> str | None:
    """The ready `say` command that answers a `HUMAN` or `PEER` item; None for a ping, whose
    answer is a verb the agent chooses. The name is the sender's short name when no other
    member of the team has it, else the full handle; a human's is the localpart. `--re` names
    the item, single-quoted for the shell (an event ID starts with `$`), so the answer is
    threaded under it in Element. A questions-room item is answered in its question's thread,
    or, unattached, as a reply to it (Plan 00211 SPEC §8.3)."""
    message = outcome.human or outcome.peer
    if message is None:
        return None
    if room == protocol.PURPOSE_QUESTIONS:
        event_id = _event_id(message.event_id)
        target = f"Q{question[0]}" if question is not None else f"--re '{event_id}'"
        return format_line("REPLY", _team(team), event_id, f"{PROG} --team {team} thread {target}")
    name = _localpart(message.sender, server_name)
    if outcome.peer is not None:
        short = name.partition(protocol.HANDLE_SEP)[0]
        if len(_agents_named(record, server_name, short)) == 1:
            name = short
    event_id = _event_id(message.event_id)
    return format_line("REPLY", _team(team), event_id,
                       f"{PROG} --team {team} say --to {name} --re '{event_id}'")


def timeout_line(
    team: str, server_name: str, event_id: str, target: str, verb: str, ref: str | None
) -> str:
    return format_line(
        "TIMEOUT", _team(team), _event_id(event_id), _localpart(target, server_name),
        _verb(verb), _ref(ref),
    )


def sent_line(team: str, event_id: str) -> str:
    return format_line("SENT", _team(team), _event_id(event_id))


def relay_line(team: str, server_name: str, outcome: protocol.Outcome, room: str = protocol.ROOM_MAIN,
               question: tuple[int, str] | None = None) -> str:
    """The spool line of an accepted item: its §15 line with the text capped to
    `relay.BODY_MAX_BYTES`, and one final field, `cut`: `-` or `shown/total` bytes."""
    cut = relay.CUT_NONE
    if outcome.human is not None:
        text, cut = relay.cap_text(outcome.human.text)
        outcome = dataclasses.replace(outcome, human=dataclasses.replace(outcome.human, text=text))
    elif outcome.peer is not None:
        text, cut = relay.cap_text(outcome.peer.text)
        outcome = dataclasses.replace(outcome, peer=dataclasses.replace(outcome.peer, text=text))
    return f"{item_line(team, server_name, outcome, room, question)}\t{cut}"


def history_line(team: str, server_name: str, item: syncer.HistoryItem) -> str:
    """A `HISTORY` line: the direction, the time, then the `PING`, `HUMAN`, `PEER` or `SENT` line
    the item would be, less its version. Each part passes its own line's grammar."""
    if item.direction not in HISTORY_DIRECTIONS:
        raise ValueError("direction field is not in or out")
    ts = item.origin_server_ts
    if type(ts) is not int or ts < 0:
        raise ValueError("origin_server_ts field is not a non-negative integer")
    if item.ping is not None:
        inner = ping_line(team, server_name, item.ping)
    elif item.human is not None:
        inner = human_line(team, server_name, item.human, item.room, item.question)
    elif item.peer is not None:
        inner = peer_line(team, server_name, item.peer)
    elif item.sent_event_id is not None:
        inner = sent_line(team, item.sent_event_id)
    else:
        raise ValueError("a history item records a ping, a human message, a peer text or a sent text")
    kind, _version, fields = inner.split("\t", 2)
    return "\t".join(("HISTORY", str(protocol.PROTOCOL_VERSION), item.direction, str(ts), kind, fields))


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


def forge_credential_source(environ: Mapping[str, str]) -> str:
    """The forge credential's source (§6), never its value: `gh`, once gh has answered with
    a token. gh missing or not logged in is a `config.ConfigError` naming the fix."""
    forge.gh_token(environ)
    return forge.GH_SOURCE


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


_GITHUB_URL_RE = re.compile(
    r"(?:git@github\.com[^:/]*:|ssh://git@github\.com[^/]*/|https://github\.com/)"
    r"(?P<owner>[^/]+)/(?P<repo>[^/]+?)(?:\.git)?/?")


def checkout_upstream(cwd: str) -> tuple[str, str] | None:
    """The `OWNER/REPO` (lowercase) of the checkout's `origin` on GitHub and the branch its
    current branch tracks there, or None when there is no such pair: no checkout, a
    detached head, no upstream, an upstream on another remote, or an origin that is not a
    GitHub repository. `pingbus send` names that branch in a ping (D64), so an agent never
    types it; when it cannot be found the receiver falls back to the default branch."""
    git_env = {**os.environ, "LC_ALL": "C"}
    try:
        upstream = subprocess.run(
            ["git", "-C", cwd, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"],
            capture_output=True, text=True, check=False, env=git_env,
        )
    except FileNotFoundError:
        return None
    remote, _, branch = upstream.stdout.strip().partition("/")
    if upstream.returncode != 0 or remote != "origin" or not protocol.is_branch_name(branch):
        return None
    url = subprocess.run(
        ["git", "-C", cwd, "remote", "get-url", "origin"],
        capture_output=True, text=True, check=False, env=git_env,
    )
    match = _GITHUB_URL_RE.fullmatch(url.stdout.strip()) if url.returncode == 0 else None
    if match is None:
        return None
    repo = f"{match['owner']}/{match['repo']}".lower()
    return (repo, branch) if protocol.is_owner_repo(repo) else None


def _send_branch(args: argparse.Namespace, ref: str | None, record: protocol.TeamRecord,
                 upstream: Callable[[], tuple[str, str] | None]) -> str | None:
    """The ping's `branch`: `--branch` as given; else, for a `path` or `commit` reference
    into a repository whose every branch the team trusts (an unlisted one of its org, or one
    listed with no `branches`), the branch the checkout tracks when its origin is that
    repository (DESIGN.md D64); else none (the default branch, or a listed repository's
    listed branches)."""
    if args.branch is not None:
        return args.branch
    parsed = None if ref is None else protocol.parse_ref(ref)
    if parsed is None or parsed.form not in protocol.BRANCH_REF_FORMS:
        return None
    if not protocol.trusts_any_branch(parsed, record.org, record.repos):
        return None
    found = upstream()
    return found[1] if found is not None and found[0] == parsed.repo_full else None


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
#: same addressees within the window is a duplicate, and the text itself is never kept at rest.
SAY_GATE_VERB = "text"


def _now_ms() -> int:
    return int(time.time() * 1000)


@dataclasses.dataclass
class DeliveryTiming:
    """Socket delivery's clocks (SPEC-inject.md): the close wait of one write (E11), the
    retry after a failed write and the stall after failures (E15), the quiet and the
    give-up before a delivery not in the transcript is missing (E16), and how often a
    delivery of held-back lines alone may go (E19)."""

    send_timeout_s: float = notify.SEND_TIMEOUT_S
    retry_s: float = 10.0
    stall_after_s: float = relay.STALL_AFTER_S
    missing_quiet_s: float = inject.MISSING_QUIET_S
    missing_give_up_s: float = inject.MISSING_GIVE_UP_S
    held_back_every_s: float = 60.0


@dataclasses.dataclass
class Runtime:
    """What the network commands take from their surroundings; tests replace the parts.

    `forge_for(member, environ)` gives the per-record forge factory (`syncer.forge_factory`);
    `clock_ms` is wall-clock milliseconds for limits and ack deadlines; `sleep` serves the
    client's 429 retries and the lock retries; `monotonic` times `wait`'s deadline and its
    wait for a `recv` lock holder; `long_poll_ms` caps each `/sync` of `wait`; `tick_s` is how often
    `wait` looks for a TIMEOUT falling due while nothing arrives; `threads` collects
    `wait`'s and `watch`'s long-poll threads; `notice_interval_s` spaces the watcher's
    deliveries, and `delivery` holds its other clocks; `spawn` starts the watcher from the SessionStart hook; `seats_root` is the
    checkout's seats (`SEAT` lines, the claim), `session_home` the home a claim builds, and
    `execvpe` replaces the process with the session once its seats are claimed;
    `upstream` is the working directory's checkout and the branch it tracks (`send`);
    `stopping`, once set, ends a relay-mode `watch` (the plugin that owns it otherwise
    stops it by signal); `outage_retry_s` to `outage_retry_max_s` is a watcher team's backoff
    through a transient fault (SPEC-inject E25), and `supervise_retry_s` to
    `supervise_retry_max_s` the supervisor's between restarts of the watcher it runs with
    `run_child(argv, environ)` (E26)."""

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
    delivery: DeliveryTiming = dataclasses.field(default_factory=DeliveryTiming)
    spawn: Callable[[Mapping[str, str], pathlib.Path], object] = hooks.spawn_watcher
    seats_root: pathlib.Path = seating.CCY_SEATS_ROOT
    session_home: pathlib.Path = seating.SESSION_HOME
    execvpe: Callable[[str, list[str], dict[str, str]], object] = os.execvpe
    upstream: Callable[[], tuple[str, str] | None] = lambda: checkout_upstream(os.getcwd())
    stopping: threading.Event = dataclasses.field(default_factory=threading.Event)
    #: Posts a session-end goodbye `(team, text)`; None is the real one (`post_goodbye`).
    post: Callable[[str, str], object] | None = None
    outage_retry_s: float = 1.0
    outage_retry_max_s: float = 30.0
    supervise_retry_s: float = 1.0
    supervise_retry_max_s: float = 60.0
    run_child: Callable[[Sequence[str], Mapping[str, str]], int] = (
        lambda argv, environ: _run_child(argv, environ))
    #: How to run pingbus again (the seat keeper `seat exec` starts), and how it is started.
    keeper_argv: Callable[[], list[str]] = lambda: pingbus_argv()
    spawn_keeper: Callable[[Sequence[str], Mapping[str, str]], subprocess.Popen] = (
        lambda argv, environ: spawn_keeper(argv, environ))
    #: How long `seat retire` and `seat farewell` wait for a removed team's sync lock: its
    #: watcher thread lets it go once the long poll in flight ends (Plan 00203 H11, H23).
    sync_wait_s: float = limits.SYNC_LONG_POLL_S + 10.0
    #: How long `seat disconnected` waits for a seat the session held to be let go: a closed
    #: terminal runs the launcher's exit while the container is still stopping.
    disconnect_wait_s: float = 15.0
    #: The keeper's ties to its process: dying with its parent, and its output once running.
    die_with_parent: Callable[[], object] = lambda: die_with_parent()
    detach: Callable[[pathlib.Path, bool], object] = lambda log, ready: detach_keeper(log, ready)


def pingbus_argv() -> list[str]:
    """How to run this pingbus again: the zipapp it was loaded from, else its source tree."""
    top = pathlib.Path(__file__).resolve().parents[2]
    if top.is_file():
        return [sys.executable, str(top)]
    start = (f"import sys; sys.path.insert(0, {str(top)!r}); "
             "from helpers.pingbus.cli import main; sys.exit(main())")
    return [sys.executable, "-c", start]


def spawn_keeper(argv: Sequence[str], environ: Mapping[str, str]) -> subprocess.Popen:
    """Start the seat keeper in a session of its own, so no terminal signal reaches it. It
    reports on its stdout and stderr until it is ready (`detach_keeper`)."""
    return subprocess.Popen(list(argv), stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, env=dict(environ), close_fds=True,
                            start_new_session=True)


#: prctl's option that sends the caller a signal when its parent dies (Linux).
PR_SET_PDEATHSIG = 1


def die_with_parent() -> None:
    """Have the kernel end this process when its parent does, so a `cc --teams` session's
    seats are free the moment the session ends."""
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(PR_SET_PDEATHSIG, signal.SIGTERM, 0, 0, 0) != 0:
        raise OSError(ctypes.get_errno(), "prctl(PR_SET_PDEATHSIG) failed")


KEEPER_READY = b"READY\n"
#: How long `seat exec` waits for its keeper to say the seats are taken.
KEEPER_READY_S = 30.0


def detach_keeper(log_path: pathlib.Path, ready: bool) -> None:
    """From here the keeper's stderr is its log in the session home and its stdout goes
    nowhere; a launch's keeper says READY on its stdout in between, which tells `seat
    exec` that the seats are taken."""
    sys.stderr.flush()
    fd = os.open(log_path, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    os.dup2(fd, 2)
    os.close(fd)
    if ready:
        os.write(1, KEEPER_READY)
    null = os.open(os.devnull, os.O_WRONLY | os.O_CLOEXEC)
    os.dup2(null, 1)
    os.close(null)


def _run_child(argv: Sequence[str], environ: Mapping[str, str]) -> int:
    """The supervised watcher, its stderr this process's own (`watch.log`). Descriptors
    made inheritable (the session's watch lock) pass to it; every other one is close-on-exec.
    Its exit code is the result the supervisor acts on, so `check=False`."""
    return subprocess.run(list(argv), stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                          env=dict(environ), close_fds=False, check=False).returncode


class _Backoff:
    """A retry wait that doubles from `first_s` up to `cap_s`, until `reset`."""

    def __init__(self, first_s: float, cap_s: float) -> None:
        self.first_s, self.cap_s = first_s, cap_s
        self._next = first_s

    def next(self) -> float:
        wait, self._next = self._next, min(self._next * 2, self.cap_s)
        return min(wait, self.cap_s)

    def reset(self) -> None:
        self._next = self.first_s


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
        return EXIT_REFUSED, f"refused: {exc.reason}" + (f": {exc.detail}" if exc.detail else "")
    if isinstance(exc, limits.RateLimited):
        return EXIT_RATE, f"rate limited: {exc.reason}"
    if isinstance(exc, forge.ForgeError):
        return exc.exit_code, f"{FORGE_REFUSED}: {exc}"
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


def _agent_handles(record: protocol.TeamRecord, server_name: str) -> list[str]:
    """The handles of the record's agents, in the record's order."""
    found = (protocol.parse_user_id(user, server_name) for user in record.roles)
    return [handle for handle in found if handle is not None]


def _agents_named(record: protocol.TeamRecord, server_name: str, short: str) -> list[str]:
    """The agents whose short name (the handle before `+`, `<repo>.<seat>`) is `short`."""
    return [handle for handle in _agent_handles(record, server_name)
            if handle.partition(protocol.HANDLE_SEP)[0] == short]


def _user_id(name: str, record: protocol.TeamRecord, member: config.Member) -> str:
    """One `--to` name as a user ID, tried in order: a full user ID, an exact agent handle,
    a human localpart, a reserved localpart, then a seat's short name, which must match
    exactly one agent. Anything else is a usage error naming the team's members; the
    validator refuses what resolution cannot (`target`: `@room`, the sender itself)."""
    server = member.server_name
    if name.startswith("@"):
        return name
    handles = _agent_handles(record, server)
    humans = sorted(localpart for localpart in
                    (protocol.parse_user_id(user, server) for user in record.humans) if localpart)
    if name in handles or name in humans or name in protocol.RESERVED_LOCALPARTS:
        return f"@{name}:{server}"
    named = _agents_named(record, server, name)
    if len(named) == 1:
        return f"@{named[0]}:{server}"
    if named:
        raise config.UsageError(f"--to {name} is ambiguous in team {member.team}: {', '.join(named)}")
    raise config.UsageError(f"--to {name}: no member of team {member.team} is named that. "
                            f"Agents: {', '.join(handles) or '-'}. Humans: {', '.join(humans) or '-'}")


@contextlib.contextmanager
def _explained_targets(args: argparse.Namespace, verb: str | None, record: protocol.TeamRecord,
                       member: config.Member) -> Iterator[list[str]]:
    """`--to`'s addressees (`_addressees`), with a `target` refusal raised inside re-raised
    with what was refused and who this command can address, from the command line and the
    record only. `verb` is the ping's, None for `say`."""
    server = member.server_name
    to: list[str] = []
    try:
        to += _addressees(args, record, member)
        yield to
    except protocol.Refusal as exc:
        if exc.reason != "target" or exc.detail is not None:
            raise
        ctx = record.context(server)
        command = "say" if verb is None else f"send {verb}"
        refused = [protocol.parse_user_id(user, server) or user for user in to
                   if not protocol.addressable(user, verb, ctx)]
        if refused:
            what = f"--to {','.join(refused)} cannot be addressed by {command}"
        elif not to:
            what = f"--to-coordinator: team {member.team} has no coordinator but this agent"
        else:
            limit = protocol.MAX_TEXT_TO if verb is None else protocol.MAX_TO
            checked = protocol.with_coordinator_copy(to, member.user_id, ctx)
            what = f"{command} takes at most {limit} addressees, not {len(checked)}"
            if len(checked) > len(to):
                what += " (the coordinator is copied on a worker's message to a human)"
        users = [user for user in (*record.roles, *sorted(record.humans))
                 if user != member.user_id and protocol.addressable(user, verb, ctx)]
        agents = [_localpart(user, server) for user in users if protocol.is_agent_user_id(user, server)]
        humans = [_localpart(user, server) for user in users if protocol.is_human_user_id(user, server)]
        raise protocol.Refusal("target", f"{what}. Agents: {', '.join(agents) or '-'}. "
                                         f"Humans: {', '.join(humans) or '-'}") from None


def _user_ids(names: str, record: protocol.TeamRecord, member: config.Member) -> list[str]:
    """`--to`'s comma-separated names, as user IDs (`_user_id`)."""
    return [_user_id(name, record, member) for name in names.split(",")]


def _addressees(args: argparse.Namespace, record: protocol.TeamRecord, member: config.Member) -> list[str]:
    """`--to`'s names then, for `--to-coordinator`, the team's coordinator but the sender;
    each user once. Nothing else is judged here: the validator refuses what is not allowed."""
    to = [] if args.to is None else _user_ids(args.to, record, member)
    if args.to_coordinator:
        found = [user for user in protocol.coordinators(record.roles) if user != member.user_id]
        if not found:
            raise protocol.Refusal("target")
        to += found
    if not to:
        raise config.UsageError("name an addressee: --to NAME[,NAME...] or --to-coordinator")
    return list(dict.fromkeys(to))


def _cached_record(member: config.Member) -> protocol.TeamRecord | None:
    """The team's verified record, or None while it has none (not synced yet, or untrusted)."""
    try:
        return load_cached_record(member)
    except protocol.Untrusted:
        return None


def _fits(member: config.Member, record: protocol.TeamRecord, names: list[str]) -> bool:
    """Whether every `--to` name resolves in this team's verified record (`_user_id`); a full
    user ID fits only the team whose server it names."""
    for name in names:
        if name.startswith("@"):
            if not name.endswith(f":{member.server_name}"):
                return False
            continue
        try:
            _user_id(name, record, member)
        except config.UsageError:
            return False
    return True


def _choices_line(member: config.Member, unknown: bool) -> str:
    """One active team with what it is for (or why it cannot be judged), for the message that
    asks which to name."""
    if unknown:
        return (f"  {member.team}  (no verified team record yet: run pingbus recv or name it "
                "with --team)")
    try:
        about = inbox.TeamState.for_member(member).about()
    except inbox.StateError:
        about = "(description unreadable)"
    return f"  {member.team}  {about or '(no description)'}"


def _sending_member(args: argparse.Namespace, environ: Mapping[str, str]) -> config.Member:
    """The team `send` or `say` acts on: `--team`, the one active team, or, when several are
    active, the one `--re` belongs to (its inbox or consumed items hold that event) or else the
    only one in which every `--to` name resolves. Anything else is a usage error that lists the
    active teams, because guessing would put a message in the wrong room."""
    members = config.load_active(environ, team=args.team)
    if len(members) == 1:
        return members[0]
    re_id = getattr(args, "re", None)
    holders = [] if re_id is None else [
        member for member in members if re_id in inbox.TeamState.for_member(member).known_event_ids()]
    if len(holders) == 1:
        return holders[0]
    records = {member.team: _cached_record(member) for member in members}
    # A team whose record is not verified yet cannot be judged, so `--to` cannot pick between
    # teams: guessing would put a message in the wrong room.
    if args.to is not None and all(record is not None for record in records.values()):
        fitting = [member for member in members
                   if _fits(member, records[member.team], args.to.split(","))]
        if len(fitting) == 1:
            return fitting[0]
    raise config.UsageError(
        "several teams are active and --to/--re fit more than one (or none): name one with --team:\n"
        + "\n".join(_choices_line(member, records[member.team] is None) for member in members))


def cmd_send(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """§9 on send, steps 1-7, in order; the first failure sends nothing."""
    rt: Runtime = args.runtime
    relaying = relay.enabled(environ)
    member = _sending_member(args, environ)
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    ctx = record.context(member.server_name)
    with _explained_targets(args, args.verb, record, member) as addressees:
        to = protocol.with_coordinator_copy(addressees, member.user_id, ctx)
        ref = None if args.ref is None else protocol.lowercase_ref_repo(args.ref)
        content = protocol.build_ping(args.verb, to, ref, args.re, _send_branch(args, ref, record, rt.upstream))
        ping = protocol.validate_content(content, ctx)
    protocol.check_role(args.verb, member.user_id, ctx)
    now = rt.clock_ms()
    seat.state.admit_send(member.limits, args.verb, ref, args.re, ping.to, now)
    if ping.ref is not None:
        forge.check_ping(ping, record, rt.forge_for(member, environ)(record))
    event_id = seat.client.send_message(member.room, content)
    with seat.state.outbox() as box:
        box.record_sent(event_id, args.verb, ref, ping.to, now)
    if relaying:
        _record_sent(environ, err, sentlog.ping_entry(
            member.team, event_id, now, _localparts(ping.to, member.server_name), args.verb, ref, args.re))
    emit(sent_line(member.team, event_id), out, err)
    return EXIT_OK


def _localparts(user_ids: Sequence[str], server_name: str) -> list[str]:
    return [_localpart(user_id, server_name) for user_id in user_ids]


def _record_sent(environ: Mapping[str, str], err: TextIO, entry: dict) -> None:
    """Append a send the homeserver accepted to the sent log (relay mode). The send
    stands whatever happens here, so a failure is said loudly on stderr, not raised."""
    try:
        sentlog.append(config.resolve_home(environ), entry)
    except (OSError, ValueError) as exc:
        err.write(f"{PROG}: the send went through but could not be recorded in {sentlog.SENT_LOG}, "
                  f"so it will not be echoed: {getattr(exc, 'strerror', None) or exc}\n")


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
    relaying = relay.enabled(environ)
    if args.re is not None and not protocol.is_event_id(args.re):
        raise protocol.Refusal("re")
    member = _sending_member(args, environ)
    text = _read_text(args.stdin)
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    ctx = record.context(member.server_name)
    with _explained_targets(args, None, record, member) as addressees:
        to = protocol.with_coordinator_copy(addressees, member.user_id, ctx)
        event_id, now, said_to = _send_text(seat, ctx, to, text, rt, re=args.re)
    if relaying:
        _record_sent(environ, err, sentlog.say_entry(
            member.team, event_id, now, _localparts(said_to, member.server_name), text))
    emit(sent_line(member.team, event_id), out, err)
    return EXIT_OK


# ---------------------------------------------------------------- reports and questions


QUESTION_RE = re.compile(r"Q([1-9][0-9]{0,8})")
NO_ROOM = "team {team} has no {purpose} room yet; it is created by the agent-bus play"


def _read_side_text(stdin: BinaryIO, cap: int) -> str:
    """A report's, question's or thread post's text: stdin as UTF-8, less one trailing
    newline. Over `cap` bytes is refused as `body`, never cut (Plan 00211 SPEC §7)."""
    raw = stdin.read(cap + 2)
    if raw.endswith(b"\n"):
        raw = raw[:-1]
    if len(raw) > cap:
        raise protocol.Refusal("body", f"at most {cap} bytes")
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError:
        raise protocol.Refusal("schema") from None


def _no_token(member: config.Member, text: str) -> None:
    if config.read_token(member) in text:
        raise protocol.Refusal("secret")


def _side_room(seat: _Seat, purpose: str) -> str:
    """The side room `purpose` of a verified team: `no-room` before the play made it; exit 10
    naming it when it is not joined or fails its trust check (SPEC §4.3). The reports room is
    never synced, so it is verified here, at each send."""
    found = seat.syncer.side.get(purpose)
    if found is None:
        raise protocol.Refusal("no-room", NO_ROOM.format(team=seat.member.team, purpose=purpose))
    if found.state == "join" and purpose == protocol.PURPOSE_REPORTS:
        found = seat.syncer.verify_side(purpose, found.room, seat.syncer.record)
    if found.state != "join":
        raise syncer.RoomUntrusted(f"the {purpose} room {found.room} is {found.state}: "
                                   f"{found.reason or 'not joined'}")
    return found.room


def _coordinator(member: config.Member, record: protocol.TeamRecord, command: str) -> None:
    if record.roles.get(member.user_id) != protocol.ROLE_COORDINATOR:
        raise protocol.Refusal("role", f"{command} is the coordinator's; a worker asks the coordinator "
                                       "with pingbus say --to-coordinator")


def _digest(text: str) -> str:
    return "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()


def cmd_report(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """SPEC §7: stdin as a quiet notice in the reports room, for the humans to read."""
    rt: Runtime = args.runtime
    member = _one_member(environ, args.team)
    text = _read_side_text(args.stdin, protocol.MAX_REPORT_BYTES)
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    protocol.check_report_text(text)
    _no_token(member, text)
    protocol.check_text_sender(member.user_id, record.context(member.server_name))
    room_id = _side_room(seat, protocol.PURPOSE_REPORTS)
    seat.state.admit_report(member.limits, _digest(text), rt.clock_ms())
    emit(sent_line(member.team, seat.client.send_message(room_id, protocol.build_report(text))), out, err)
    return EXIT_OK


def _asked_by(args: argparse.Namespace, record: protocol.TeamRecord, member: config.Member) -> list[str]:
    if args.asked_by is None:
        return []
    found = _user_ids(args.asked_by, record, member)
    if not all(protocol.is_agent_user_id(user, member.server_name) and user in record.roles for user in found):
        raise protocol.Refusal("target", "--asked-by names the team's agents")
    return list(dict.fromkeys(found))


def _main_room_events(seat: _Seat, names: str | None) -> list[str]:
    """`--re`'s event IDs, each an event of the team room (`re` otherwise)."""
    if names is None:
        return []
    found = list(dict.fromkeys(names.split(",")))
    for event_id in found:
        if not protocol.is_event_id(event_id):
            raise protocol.Refusal("re")
        try:
            seat.client.get_event(seat.member.room, event_id)
        except matrix.NotFound:
            raise protocol.Refusal("re", f"{event_id} is not a message of the team room") from None
    return found


def question_line(team: str, server_name: str, view: protocol.QuestionView) -> str:
    """A `QUESTION` line: the number, state, root event, who asked and the replies unanswered."""
    asked = [protocol.parse_user_id(user, server_name) for user in view.question.asked_by]
    return format_line("QUESTION", _team(team), f"Q{view.question.n}", view.state,
                       _event_id(view.question.event_id), ",".join(a for a in asked if a) or None,
                       str(len(view.unanswered)))


def cmd_ask_human(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """SPEC §8.2: a coordinator's question, numbered `Q<n>` after the highest in the room."""
    rt: Runtime = args.runtime
    member = _one_member(environ, args.team)
    text = _read_side_text(args.stdin, protocol.MAX_QUESTION_BYTES)
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    _coordinator(member, record, "ask-human")
    _, asked = protocol.check_question_text(text)
    _no_token(member, text)
    asked_by = _asked_by(args, record, member)
    re_ = _main_room_events(seat, args.re)
    room_id = _side_room(seat, protocol.PURPOSE_QUESTIONS)
    index = seat.syncer.question_index()
    same = index.open_duplicate(asked)
    if same is not None:
        err.write(f"{PROG}: team {member.team}: Q{same.n} is open and asks the same question\n")
        raise limits.RateLimited("duplicate")
    n = index.next_number()
    content = protocol.build_question(n, text, record.humans, asked_by, re_)
    seat.state.admit_send(member.limits, "question", _digest(asked), None, (), rt.clock_ms())
    event_id = seat.client.send_message(room_id, content)
    emit(sent_line(member.team, event_id), out, err)
    view = protocol.QuestionView(protocol.Question(n, event_id, member.user_id, "", asked, tuple(asked_by),
                                                   tuple(re_)), protocol.STATE_OPEN, (), ())
    emit(question_line(member.team, member.server_name, view), out, err)
    return EXIT_OK


def _thread_target(args: argparse.Namespace) -> int | None:
    """`thread`'s question number, checked before anything is read: a question, or `--re`."""
    if args.question is None:
        if args.re is None:
            raise config.UsageError("thread: name the question (Q<n>) or the message it answers (--re)")
        if args.close:
            raise config.UsageError("thread --close: name the question it closes (Q<n>)")
        return None
    match = QUESTION_RE.fullmatch(args.question)
    if match is None:
        raise config.UsageError(f"thread: {args.question} is not a question number (Q<n>)")
    return int(match[1])


def cmd_thread(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """SPEC §8.4: a coordinator's post in a question's thread; `--ask` notifies the humans,
    `--close` closes it in the same message."""
    rt: Runtime = args.runtime
    n = _thread_target(args)
    if args.re is not None and not protocol.is_event_id(args.re):
        raise protocol.Refusal("re")
    member = _one_member(environ, args.team)
    text = _read_side_text(args.stdin, protocol.MAX_THREAD_BYTES)
    protocol.check_thread_text(text, closing=args.close)
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    _coordinator(member, record, "thread")
    _no_token(member, text)
    room_id = _side_room(seat, protocol.PURPOSE_QUESTIONS)
    index = seat.syncer.question_index()
    root = None
    if n is not None:
        if n not in index.by_n:
            raise config.UsageError(f"thread: team {member.team} has no question Q{n}")
        root = index.by_n[n].event_id
    if args.re is not None and not (args.re in index.attached or args.re in index.by_event
                                    or args.re in index.thread_of):
        raise protocol.Refusal("re", f"{args.re} is not a message of the questions room")
    content = protocol.build_thread_post(root, text, record.humans, ask=args.ask,
                                         close=n if args.close else None, re=args.re)
    seat.state.admit_send(member.limits, "thread", _digest(f"{root}\n{text}"), None, (), rt.clock_ms())
    emit(sent_line(member.team, seat.client.send_message(room_id, content)), out, err)
    return EXIT_OK


def cmd_questions(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """SPEC §8.4: one `QUESTION` line per question, each followed by its `REPLY_TO` lines."""
    rt: Runtime = args.runtime
    member = _one_member(environ, args.team)
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    _coordinator(member, record, "questions")
    _side_room(seat, protocol.PURPOSE_QUESTIONS)
    for view in seat.syncer.question_index().views():
        if view.state == protocol.STATE_CLOSED and not args.all:
            continue
        emit(question_line(member.team, member.server_name, view), out, err)
        for event_id in view.unanswered:
            emit(format_line("REPLY_TO", _team(member.team), f"Q{view.question.n}", _event_id(event_id)), out, err)
    return EXIT_OK


def _send_text(seat: _Seat, ctx: protocol.Context, to: list[str], text: str, rt: Runtime,
               re: str | None = None, deduplicate: bool = True) -> tuple[str, int, list[str]]:
    """§9 steps from the build of an agent text to its send; the event ID, the send time
    and the validated addressees. With `re`, the text answers that event (§7). Without
    `deduplicate`, the duplicate window (§10) is not applied: see `_post_to_humans_and_coordinators`."""
    member = seat.member
    content = protocol.build_text(to, text, re=re)
    said = protocol.validate_text_content(content, ctx)
    if config.read_token(member) in text:
        raise protocol.Refusal("secret")
    protocol.check_text_sender(member.user_id, ctx)
    digest = "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()
    now = rt.clock_ms()
    seat.state.admit_send(member.limits, SAY_GATE_VERB, digest, None, said.to, now, deduplicate)
    return seat.client.send_message(member.room, content), now, said.to


def post_goodbye(rt: Runtime, environ: Mapping[str, str], err: TextIO, team: str, text: str) -> None:
    """The session-end goodbye as a plain room text from this member, to the team's listed
    humans and its coordinator(s) (at most `MAX_TEXT_TO`, humans first), not to every agent:
    each agent addressed costs a model turn, and the seat lock already tells agents the seat
    is free. A refusal or homeserver failure is `hooks.GoodbyeFailed` with the command's own one-line message.
    It is never written to the sent log: a goodbye is not an echo the session is owed."""
    try:
        _post_to_humans_and_coordinators(_one_member(environ, team), environ, rt, err, text)
    except FAILURES as exc:
        raise hooks.GoodbyeFailed(failure(exc)[1]) from None


def _post_to_humans_and_coordinators(member: config.Member, environ: Mapping[str, str], rt: Runtime,
                                     err: TextIO, text: str) -> None:
    """pingbus's own fixed-text notice (goodbye, farewell, disconnected). Each is told once per
    session or claim by the seat, so the duplicate window, which stops an agent repeating
    itself, could only lose a second, true one (Plan 00194 Task 2.0i); the bucket still applies."""
    seat = _seat(member, environ, rt, err)
    record = seat.syncer.verify_room()
    ctx = record.context(member.server_name)
    coordinators = [user for user in protocol.coordinators(ctx.roles) if user != member.user_id]
    to = list(dict.fromkeys([*sorted(ctx.humans), *coordinators]))[:protocol.MAX_TEXT_TO]
    _send_text(seat, ctx, to, text, rt, deduplicate=False)


def _room_id(seat: _Seat, room: str) -> str | None:
    """The room ID of an item's room: the team room, or the questions room this member met;
    None when it has met none (its `rooms.json` is gone)."""
    if room == protocol.ROOM_MAIN:
        return seat.member.room
    found = seat.syncer.side.get(room)
    return None if found is None else found.room


def _refetch(seat: _Seat, ctx: protocol.Context, item: inbox.Item) -> tuple[protocol.Outcome, object]:
    """§9 reading the inbox: the item as the homeserver serves it, re-validated as an event
    of its own room; and its sender, for a drop line. A copy the server no longer has, or
    one from a room this member no longer knows, is a `schema` drop."""
    member = seat.member
    if seat.fetcher is None:
        raise TypeError("a seat that reads the inbox is built with reads=True")
    room_id = _room_id(seat, item.room)
    if room_id is None:
        return protocol.Outcome(protocol.DROP, "schema"), None
    try:
        event = seat.fetcher.get_event(room_id, item.event_id)
    except matrix.NotFound:
        return protocol.Outcome(protocol.DROP, "schema"), None
    if event.get("event_id") != item.event_id:
        return protocol.Outcome(protocol.DROP, "schema"), None
    outcome = protocol.validate_event(event, ctx, member.user_id, human_text=member.human_text, room=item.room)
    return outcome, event.get("sender")


def _held(spool: relay.Spool | None) -> contextlib.AbstractContextManager[None]:
    """The spool's exclusive lock, or nothing where there is no spool (relay off)."""
    return contextlib.nullcontext() if spool is None else spool.exclusive()


def _timeout_key(member: config.Member, timeout: inbox.Timeout) -> str:
    """The spool key of one silent target of a ping: a ping can owe several TIMEOUTs."""
    return relay.key(timeout.event_id, _localpart(timeout.target, member.server_name))


def _deliver(seat: _Seat, record: protocol.TeamRecord, rt: Runtime, out: TextIO,
             err: TextIO, spool: relay.Spool | None = None,
             relaying: bool = False) -> tuple[int, collections.Counter[str]]:
    """Print and consume every pending item from its re-fetched copy, then every TIMEOUT
    due. (lines, what failed on re-fetch: the caller adds it to the DROPPED line)

    Whatever it consumes is withdrawn from `spool`, so relay never types what was read.
    With `relaying` it prints nothing and consumes only what fails re-validation: each
    accepted item and each TIMEOUT is spooled instead, once, and stays owed until the
    plugin confirms it (`_relay_confirm`). (lines spooled, in that case)

    `recv` and the watcher run in different processes. The re-fetch is a network round
    trip, so it happens outside the spool lock; after it each takes the lock and finds out
    whether the item is still in the inbox. Only the one that finds it there delivers it."""
    member, state = seat.member, seat.state
    ctx = record.context(member.server_name)
    now = rt.clock_ms()
    printed = 0
    drops: collections.Counter[str] = collections.Counter()
    for item in state.pending(ctx, member.user_id, human_text=member.human_text).items:
        if relaying and spool.has(member.team, item.event_id):
            continue
        outcome, sender = _refetch(seat, ctx, item)
        with _held(spool):
            if not state.in_inbox(item.event_id):
                continue
            if outcome.kind == protocol.ACCEPT:
                if relaying:
                    spool.write(member.team, item.event_id, relay_line(
                        member.team, member.server_name, outcome, item.room, item.question))
                    printed += 1
                    continue
                emit(item_line(member.team, member.server_name, outcome, item.room, item.question), out, err)
                printed += 1
                reply = reply_line(member.team, member.server_name, record, outcome, item.room, item.question)
                if reply is not None:
                    emit(reply, out, err)
            elif outcome.kind == protocol.DROP:
                state.log_drop(member.team, item.event_id, sender, outcome.reason, now)
                drops[outcome.reason] += 1
            state.consume(item.event_id)
            if spool is not None:
                spool.withdraw(member.team, item.event_id)
    with _held(spool), state.outbox() as box:
        due = box.due_timeouts(now, member.limits)
        for timeout in due:
            line = timeout_line(member.team, member.server_name, timeout.event_id, timeout.target,
                                timeout.verb, timeout.ref)
            if not relaying:
                emit(line, out, err)
                if spool is not None:
                    spool.withdraw(member.team, _timeout_key(member, timeout))
            elif spool.has(member.team, _timeout_key(member, timeout)):
                continue
            else:
                spool.write(member.team, _timeout_key(member, timeout), f"{line}\t{relay.CUT_NONE}")
            printed += 1
        if not relaying:
            box.mark_reported(due)
    return printed, drops


def _relay_confirm(seat: _Seat, spool: relay.Spool, rt: Runtime) -> int:
    """Finish what the plugin confirmed typing: consume the inbox item (or report the
    TIMEOUT for that one target) exactly once, then remove its line and marker. A marker
    whose line `recv` already withdrew only has its marker removed. (confirmations handled)"""
    member, state = seat.member, seat.state
    if not spool.confirmed(member.team):
        return 0
    with spool.exclusive():
        confirmed = spool.confirmed(member.team)
        owed = [name for name in confirmed if spool.has(member.team, name)]
        silent: list[tuple[str, str]] = []
        for name in owed:
            event_id, target = relay.split_key(name)
            if target is None:
                state.consume(event_id)
            else:
                silent.append((event_id, target))
        if silent:
            with state.outbox() as box:
                box.mark_reported([t for t in box.due_timeouts(rt.clock_ms(), member.limits)
                                   if (t.event_id, _localpart(t.target, member.server_name)) in silent])
        for name in confirmed:
            spool.finish(member.team, name)
    return len(owed)


def _spool_lock(environ: Mapping[str, str]) -> relay.Spool:
    """The spool whose lock `recv` and `wait` share with the watcher in either delivery mode
    (relay, or the socket's: SPEC-inject.md E12), so an item is shown by one of them only.
    Off relay mode nothing is spooled, so withdrawing from it only finds nothing."""
    relay.enabled(environ)  # a mistyped PINGBUS_DELIVERY is refused here too
    return relay.Spool(config.resolve_home(environ))


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
    printed, refetched = _deliver(seat, record, rt, out, err, _spool_lock(environ))
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


class _TeamStop(threading.Event):
    """One team's thread's stop: set when the watcher or waiter ends, or `remove`d when the
    owner took the team out of the session (Plan 00203 H11)."""

    def __init__(self) -> None:
        super().__init__()
        self.removed = False

    def remove(self) -> None:
        self.removed = True
        self.set()


def _discarded(seat: _Seat, stop: threading.Event, batch: syncer.Batch) -> bool:
    """H23a: whether the team was removed while this batch's long poll was in flight; if so,
    what it fetched is consumed undelivered, so nothing posted after the remove reaches the
    session on a re-add. A thread stopped because its process ends keeps what it fetched."""
    if not (isinstance(stop, _TeamStop) and stop.removed):
        return False
    for event_id in batch.accepted:
        seat.state.consume(event_id)
    return True


def _long_poll(seat: _Seat, lock: inbox.Lock, rt: Runtime, waiting: _Waiting,
               stop: threading.Event | None = None) -> None:
    """One team's thread: long-poll until stopped (all, or this team by `stop`) or the
    deadline, waking the main thread on every batch that stored an item. It owns `lock` and
    releases it when it ends. A failure is handed to the main thread, which reports it and
    exits with its code."""
    stop = stop or waiting.stop
    try:
        while not waiting.stop.is_set() and not stop.is_set():
            remaining_ms = int((waiting.deadline - rt.monotonic()) * 1000)
            if remaining_ms <= 0:
                return
            batch = seat.syncer.sync_once(min(rt.long_poll_ms, remaining_ms))
            if _discarded(seat, stop, batch):
                return
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
                 err: TextIO, spool: relay.Spool | None = None) -> int:
    """One pass of `wait` over its teams: deliver each trusted team's items, then one
    DROPPED line per team for its synced and re-fetched drops together. Each record is
    read once: a long-poll thread may lose trust (record None) at any moment. (lines)"""
    printed = 0
    for seat in seats:
        record = seat.syncer.record
        if record is None:
            continue
        lines, refetched = _deliver(seat, record, rt, out, err, spool)
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
    spool = _spool_lock(environ)
    held: list[tuple[_Seat, inbox.Lock]] = []
    pending: _Drops = {}
    handed_over: set[str] = set()
    stops: dict[str, _TeamStop] = {}
    following = _Following(environ, err, rt)
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
        if _deliver_all(seats, pending, rt, out, err, spool):
            return EXIT_OK
        until = rt.clock_ms() + timeout * 1000
        for seat in seats:
            seat.syncer.publish_status(until)
        for seat, lock in held:
            stops[seat.member.team] = _TeamStop()
            thread = threading.Thread(target=_long_poll, args=(seat, lock, rt, waiting, stops[seat.member.team]),
                                      daemon=True, name=f"pingbus-wait-{seat.member.team}")
            rt.threads.append(thread)
            handed_over.add(seat.member.team)
            thread.start()
        while True:
            waiting.woke.wait(max(0.0, min(waiting.deadline - rt.monotonic(), rt.tick_s)))
            waiting.woke.clear()
            _collect_drops(waiting, pending)
            if following.on:
                seats = following.follow(seats, stops, None)
                if not seats:
                    err.write(f"{PROG}: this session is on no team now: wait ends\n")
                    return EXIT_OK
            failures = [(team, exc) for team, exc in waiting.failures
                        if any(seat.member.team == team for seat in seats)]
            if failures:
                _emit_drops(pending, out, err)
                team, exc = failures[0]
                if not isinstance(exc, FAILURES):
                    raise exc
                code, message = failure(exc)
                err.write(f"{PROG}: team {team}: {message}\n")
                return code
            if _deliver_all(seats, pending, rt, out, err, spool):
                return EXIT_OK
            if rt.monotonic() >= waiting.deadline:
                err.write(f"{PROG}: nothing within {timeout} s: run wait again to re-arm\n")
                return EXIT_NOTHING
    finally:
        waiting.stop.set()
        for stop in stops.values():
            stop.set()
        for seat, lock in held:
            if seat.member.team not in handed_over:
                lock.release()


# ---------------------------------------------------------------- watch

#: How long the watcher's `listening` status (§8) runs, and how often it renews it.
WATCH_STATUS_TTL_MS = 600_000
WATCH_STATUS_REFRESH_MS = 300_000


def _permanent(exc: Exception) -> bool:
    """Whether a watcher team's failure is an answer that retrying cannot change (SPEC-inject
    E25): a refusal, a lost trust, a broken bundle or state. A homeserver restart (a
    transient `MatrixError`) and any other error, a full disk among them, are retried."""
    if isinstance(exc, matrix.MatrixError):
        return not exc.transient
    return isinstance(exc, FAILURES)


def _describe(exc: Exception) -> str:
    """One line for `watch.log`: a reported failure's message, else the error's type and
    its OS reason (never a lower-level message, which may carry anything)."""
    if isinstance(exc, FAILURES):
        return failure(exc)[1]
    if isinstance(exc, OSError) and exc.strerror:
        return f"{type(exc).__name__}: {exc.strerror}"
    return type(exc).__name__


def _mark_down(home: pathlib.Path | None, team: str, down: bool, err: TextIO) -> None:
    """Set or clear the status line's H26 mark. It only decorates the status line, so a mark
    that cannot be written is said on the watcher's stderr and the team stays watched."""
    if home is None:
        return
    try:
        statusline.set_team_down(home, team, down)
    except OSError as exc:
        err.write(f"{PROG}: team {team}: the status line's down mark was not "
                  f"{'written' if down else 'removed'}: {_describe(exc)}\n")


def _watch_poll(seat: _Seat, lock: inbox.Lock, rt: Runtime, waiting: _Waiting, err: TextIO,
                stop: threading.Event | None = None, home: pathlib.Path | None = None) -> None:
    """One team's watcher thread: a first sync, then long-poll until stopped, waking the
    main thread on every batch that stored an item, and keeping the `listening` status
    fresh. A transient fault, from the very first sync on, is retried with a backoff from
    the stored sync token, said once when it starts and once when it ends (E25); only a
    permanent one is handed to the main thread. `stop` ends this team alone (the owner
    removed it, Plan 00203 H11), after the long poll in flight. It owns `lock` and releases
    it when it ends. While its homeserver does not answer, the team is marked down in `home`
    for the status line, the mark refreshed on each retry and removed once it syncs again or
    the thread ends (Plan 00203 H26)."""
    team = seat.member.team
    stop = stop or waiting.stop
    backoff = _Backoff(rt.outage_retry_s, rt.outage_retry_max_s)
    status_ms: int | None = None
    timeout_ms = 0
    outage: str | None = None
    marked = False
    try:
        while not waiting.stop.is_set() and not stop.is_set():
            try:
                batch = seat.syncer.sync_once(timeout_ms)
                timeout_ms = rt.long_poll_ms
                if _discarded(seat, stop, batch):
                    return
                if batch.drops:
                    waiting.drops.put((team, batch.drops))
                now = rt.clock_ms()
                if status_ms is None or now - status_ms >= WATCH_STATUS_REFRESH_MS:
                    seat.syncer.publish_status(now + WATCH_STATUS_TTL_MS)
                    status_ms = now
                if outage is not None:
                    err.write(f"{PROG}: team {team}: the team syncs again\n")
                    outage = None
                    backoff.reset()
                if marked:
                    _mark_down(home, team, False, err)
                    marked = False
                if batch.accepted:
                    waiting.woke.set()
            except Exception as exc:
                if _permanent(exc):
                    raise
                if isinstance(exc, matrix.MatrixError):
                    _mark_down(home, team, True, err)
                    marked = True
                if outage is None:
                    outage = _describe(exc)
                    what = ("the homeserver does not answer" if isinstance(exc, matrix.MatrixError)
                            else "this team's sync failed")
                    err.write(f"{PROG}: team {team}: {what} ({outage}): retrying, the team stays "
                              "watched\n")
                stop.wait(backoff.next())
    except Exception as exc:
        waiting.failures.append((team, exc))
        waiting.woke.set()
    finally:
        if marked:
            _mark_down(home, team, False, err)
        lock.release()


#: After a relay pass fails (the homeserver is down), how long before it tries again.
RELAY_RETRY_S = 10.0


@dataclasses.dataclass
class _RelayLedger:
    """What a relay-mode watcher keeps between passes."""

    spool: relay.Spool
    stall: relay.Stall
    retry_at: float = 0.0


def _relay_pass(seats: Sequence[_Seat], ledger: _RelayLedger, rt: Runtime, pending: _Drops,
                err: TextIO) -> None:
    """One relay pass over the trusted teams: finish what the plugin confirmed, spool every
    pending item and TIMEOUT not yet spooled (each from its re-fetched, re-validated copy),
    then record whether relay has stalled. A team whose pass fails is reported and tried
    again after `RELAY_RETRY_S`: a homeserver that is down delays messages, never loses one."""
    confirmed = 0
    for seat in seats:
        record = seat.syncer.record
        if record is None:
            continue
        confirmed += _relay_confirm(seat, ledger.spool, rt)
        if rt.monotonic() < ledger.retry_at:
            continue
        try:
            _, refetched = _deliver(seat, record, rt, io.StringIO(), err, ledger.spool, relaying=True)
        except FAILURES as exc:
            _, message = failure(exc)
            err.write(f"{PROG}: team {seat.member.team}: {message}: relay retries in {RELAY_RETRY_S:.0f} s\n")
            ledger.retry_at = rt.monotonic() + RELAY_RETRY_S
            continue
        if refetched:
            pending.setdefault(seat.member.team, collections.Counter()).update(refetched)
    stalled = ledger.stall.observe(waiting=ledger.spool.waiting(), confirmed=confirmed)
    if stalled != ledger.spool.stalled():
        ledger.spool.set_stalled(stalled)
        err.write(f"{PROG}: relay {'stalled: no relay was confirmed in time' if stalled else 'delivers again'}\n")


@dataclasses.dataclass
class _Candidate:
    """One entry of a socket delivery being built: an inbox item, or a TIMEOUT owed."""

    seat: _Seat
    entry: envelope.Entry
    event_id: str
    timeout: inbox.Timeout | None = None
    #: The room the item came from, for its read receipt.
    room: str = protocol.ROOM_MAIN


@dataclasses.dataclass
class _Batch:
    chosen: list[_Candidate]
    dropped: list[tuple[_Seat, str, object, protocol.Outcome]]
    held: list[tuple[_Seat, str, tuple[str, str]]]
    more: int


@dataclasses.dataclass
class _Injector:
    """What a socket-mode watcher keeps between passes (SPEC-inject.md)."""

    path: str
    token: str
    spool: relay.Spool
    marks: inject.Marks
    stall: relay.Stall
    check: inject.DeliveryCheck
    budget: envelope.SenderBudget
    held: dict[tuple[str, str], int] = dataclasses.field(default_factory=dict)
    # The human messages of each delivery written but not yet found in the transcript: their
    # read receipts wait for it (E16), so none is sent for a delivery that is missing.
    unread: dict[str, list[_Candidate]] = dataclasses.field(default_factory=dict)
    last_sent: float | None = None
    held_sent: float | None = None
    retry_at: float = 0.0
    waiting: int = 0


def _reply_command(team: str, server_name: str, record: protocol.TeamRecord,
                   outcome: protocol.Outcome, item: inbox.Item) -> str | None:
    line = reply_line(team, server_name, record, outcome, item.room, item.question)
    return None if line is None else line.rsplit("\t", 1)[1]


_Found = tuple[tuple[int, int, int], _Seat, protocol.TeamRecord, inbox.Item | None, inbox.Timeout | None]


def _candidates(seats: Sequence[_Seat], rt: Runtime) -> list[_Found]:
    """Every trusted team's pending inbox items by `origin_server_ts`, then its TIMEOUTs due
    (E20), across teams."""
    order: list[_Found] = []
    for seat in seats:
        record = seat.syncer.record
        if record is None:
            continue
        member = seat.member
        ctx = record.context(member.server_name)
        for item in seat.state.pending(ctx, member.user_id, human_text=member.human_text).items:
            order.append(((0, item.origin_server_ts, len(order)), seat, record, item, None))
        with seat.state.outbox() as box:
            due = box.due_timeouts(rt.clock_ms(), member.limits)
        order += [((1, 0, len(order)), seat, record, None, timeout) for timeout in due]
    order.sort(key=lambda found: found[0])
    return order


def _choose(seats: Sequence[_Seat], inj: _Injector, rt: Runtime) -> _Batch:
    """The next delivery's candidates, each item from its re-fetched copy (E1). Re-fetch is a
    homeserver call, so this runs outside the spool lock and `_send_delivery` re-checks
    everything under it. An agent sender past its budget is held back (E19)."""
    batch = _Batch([], [], [], 0)
    per_sender: collections.Counter[tuple[str, str]] = collections.Counter()
    for _, seat, record, item, timeout in _candidates(seats, rt):
        if len(batch.chosen) >= envelope.DELIVERY_MAX_ENTRIES:
            batch.more += 1
            continue
        member = seat.member
        if timeout is not None:
            line = timeout_line(member.team, member.server_name, timeout.event_id, timeout.target,
                                timeout.verb, timeout.ref)
            entry = envelope.entry_from_line(line, handle=member.handle, ts_ms=None, reply=None)
            batch.chosen.append(_Candidate(seat, entry, timeout.event_id, timeout))
            continue
        outcome, sender = _refetch(seat, record.context(member.server_name), item)
        if outcome.kind != protocol.ACCEPT:
            batch.dropped.append((seat, item.event_id, sender, outcome))
            continue
        entry = envelope.entry_from_line(
            item_line(member.team, member.server_name, outcome, item.room, item.question), handle=member.handle,
            ts_ms=item.origin_server_ts,
            reply=_reply_command(member.team, member.server_name, record, outcome, item),
            question_headline=None if item.question is None else item.question[1],
            revises=None if outcome.human is None else outcome.human.revises)
        key = (entry.team, entry.sender)
        if not inj.budget.allowed(entry.team, entry.sender, entry.kind, chosen=per_sender[key]):
            batch.held.append((seat, item.event_id, key))
            continue
        per_sender[key] += 1
        batch.chosen.append(_Candidate(seat, entry, item.event_id, room=item.room))
    held = collections.Counter(inj.held)
    held.update(key for _, _, key in batch.held)
    fits = envelope.fit([c.entry for c in batch.chosen], held_back=held, more=batch.more)
    batch.more += len(batch.chosen) - fits
    batch.chosen = batch.chosen[:fits]
    return batch


def _settle_aside(batch: _Batch, inj: _Injector, rt: Runtime, pending: _Drops) -> None:
    """With the spool lock held: consume what failed re-validation (a drop is logged, as
    `recv` does) and what the flood limit holds back (counted for the next delivery)."""
    now = rt.clock_ms()
    for seat, event_id, sender, outcome in batch.dropped:
        if not seat.state.in_inbox(event_id):
            continue
        if outcome.kind == protocol.DROP:
            seat.state.log_drop(seat.member.team, event_id, sender, outcome.reason, now)
            pending.setdefault(seat.member.team, collections.Counter())[outcome.reason] += 1
        seat.state.consume(event_id)
    for seat, event_id, key in batch.held:
        if seat.state.in_inbox(event_id) and seat.state.consume(event_id):
            inj.held[key] = inj.held.get(key, 0) + 1


def _live(batch: _Batch, rt: Runtime, stack: contextlib.ExitStack) -> tuple[list[_Candidate], dict[str, object]]:
    """The candidates still owed, re-checked with the spool lock held: an item still in the
    inbox, a TIMEOUT still due and unreported. The outboxes stay open on `stack`."""
    boxes: dict[str, object] = {}
    live: list[_Candidate] = []
    for candidate in batch.chosen:
        state, member = candidate.seat.state, candidate.seat.member
        if candidate.timeout is None:
            if state.in_inbox(candidate.event_id):
                live.append(candidate)
            continue
        if member.team not in boxes:
            boxes[member.team] = stack.enter_context(state.outbox())
        if candidate.timeout in boxes[member.team].due_timeouts(rt.clock_ms(), member.limits):
            live.append(candidate)
    return live, boxes


def _send_delivery(batch: _Batch, inj: _Injector, rt: Runtime, pending: _Drops, err: TextIO) -> bool:
    """With the spool lock held (E10, E12): re-check each candidate, write the delivery, and
    consume what it carried once the payload was written in full (E11). True when a delivery
    was written. `SocketGone` propagates and consumes nothing."""
    now = rt.monotonic()
    with inj.spool.exclusive(), contextlib.ExitStack() as stack:
        _settle_aside(batch, inj, rt, pending)
        live, boxes = _live(batch, rt, stack)
        held_due = bool(inj.held) and (inj.held_sent is None
                                       or now - inj.held_sent >= rt.delivery.held_back_every_s)
        if not live and not held_due:
            inj.waiting = batch.more
            return False
        nonce = secrets.token_hex(4)
        text = envelope.render([c.entry for c in live], nonce, held_back=inj.held, more=batch.more)
        before = inj.check.prepare()
        try:
            closed = notify.send_notice(inj.path, inj.token, text, timeout_s=rt.delivery.send_timeout_s)
        except notify.SocketGone:
            raise
        except OSError as exc:
            err.write(f"{PROG}: delivery not written, nothing consumed: {exc.strerror or exc}: "
                      f"retry in {rt.delivery.retry_s:g} s\n")
            inj.retry_at = now + rt.delivery.retry_s
            inj.waiting = len(live) + batch.more
            return False
        for candidate in live:
            if candidate.timeout is None:
                candidate.seat.state.consume(candidate.event_id)
            else:
                boxes[candidate.seat.member.team].mark_reported([candidate.timeout])
            inj.budget.spend(candidate.entry.team, candidate.entry.sender, candidate.entry.kind)
        inj.held.clear()
        inj.held_sent = inj.last_sent = now
        inj.waiting = batch.more
        inj.check.sent(nonce, before)
        humans = [c for c in live if c.entry.kind == "HUMAN"]
        if humans:
            inj.unread[nonce] = humans
    if not closed:
        err.write(f"{PROG}: delivery {nonce} was written in full but the socket did not close within "
                  f"{rt.delivery.send_timeout_s:g} s: counted as sent\n")
    return True


def _read_receipts(delivered: Sequence[_Candidate], err: TextIO) -> None:
    """A read receipt for each human message of a delivery found in the transcript, so Element
    shows this agent's avatar under it (and none under a message that reached no model).
    Outside the spool lock: a homeserver call. The delivery stands whatever happens here, so a
    failure is one stderr line, never raised."""
    for candidate in delivered:
        member = candidate.seat.member
        room_id = _room_id(candidate.seat, candidate.room)
        if room_id is None:
            err.write(f"{PROG}: message {candidate.event_id} on {member.team} was delivered, but its "
                      f"{candidate.room} room is no longer known, so no read receipt was sent\n")
            continue
        try:
            candidate.seat.client.read_receipt(room_id, candidate.event_id)
        except FAILURES as exc:
            err.write(f"{PROG}: message {candidate.event_id} on {member.team} was delivered, but its read "
                      f"receipt was not sent, so Element does not show it read: {failure(exc)[1]}\n")


def _inject_pass(seats: Sequence[_Seat], inj: _Injector, rt: Runtime, pending: _Drops, err: TextIO) -> None:
    """One socket-mode pass: send the read receipts of deliveries found in the transcript and
    report those found missing from it (E16), then, once the pacing interval and any retry
    allow (E13, E15), build and write the next delivery, and keep the stall mark (E15)."""
    found, missing = inj.check.poll()
    for nonce in found:
        _read_receipts(inj.unread.pop(nonce, []), err)
    for nonce in missing:
        unread = " (no read receipt was sent for it)" if inj.unread.pop(nonce, None) else ""
        err.write(f"{PROG}: delivery {nonce} is missing{unread}: it is not in the session's transcript; "
                  "Claude Code may be holding socket messages. Its items are in `pingbus history`\n")
    now = rt.monotonic()
    paced = inj.last_sent is not None and now - inj.last_sent < rt.notice_interval_s
    delivered = False
    if not paced and now >= inj.retry_at:
        try:
            batch = _choose(seats, inj, rt)
        except FAILURES as exc:
            _, message = failure(exc)
            err.write(f"{PROG}: {message}: delivery retries in {rt.delivery.retry_s:g} s\n")
            inj.retry_at = now + rt.delivery.retry_s
            # What waits is still owed: the stall clock must run (Fable M1). Counting is local;
            # if even that fails, something is owed all the same.
            try:
                owed = len(_candidates(seats, rt))
            except FAILURES:
                owed = 1
            inj.waiting = max(inj.waiting, owed)
        else:
            delivered = _send_delivery(batch, inj, rt, pending, err)
    stalled = inj.stall.observe(waiting=inj.waiting, confirmed=1 if delivered else 0)
    if stalled != inj.marks.stalled():
        inj.marks.set_stalled(stalled)
        state = "stalled: nothing was written to the socket in time" if stalled else "works again"
        err.write(f"{PROG}: delivery {state}\n")


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


#: A supervised watcher's exits that a restart cannot change: the supervisor ends with them.
#: 0 is only ever "the session socket is gone" (missing, or refusing connections when a
#: killed session left its file behind), so it is final too.
SUPERVISE_FINAL = (EXIT_OK, EXIT_AUTH, EXIT_UNTRUSTED, EXIT_CONFIG, EXIT_USAGE)


def _session_watch_lock(environ: Mapping[str, str], path: str, err: TextIO) -> inbox.Lock | None:
    """The session's own watch lock (SPEC-inject E26), or None, said, when it is held:
    one supervisor or watcher per session socket, whatever team locks it holds."""
    marks = inject.Marks(config.resolve_home(environ), path)
    marks.ensure_dir()
    try:
        return inbox.acquire_lock_at(marks.watch_lock_path(), "watch")
    except inbox.Busy as busy:
        err.write(f"{PROG}: this session already has a watcher (a {busy.holder} holds its watch lock)\n")
        return None


def _supervise(environ: Mapping[str, str], path: str, lock: inbox.Lock, rt: Runtime, err: TextIO) -> int:
    """Run `watch --child` until the session's socket goes, starting it again after any
    other exit with a backoff, except a final one (SPEC-inject E26). The child inherits the
    session's watch lock, so a supervisor killed on its own leaves its watcher counted."""
    try:
        argv = [*hooks.watch_argv(environ), "--child"]
    except FileNotFoundError as exc:
        raise config.ConfigError(f"watch --supervise: {exc}") from None
    lock.set_inheritable()
    backoff = _Backoff(rt.supervise_retry_s, rt.supervise_retry_max_s)
    while True:
        started = rt.monotonic()
        code = rt.run_child(argv, environ)
        if not notify.socket_present(path):
            err.write(f"{PROG}: the session socket is gone: the supervisor exits\n")
            return EXIT_OK
        if code == EXIT_OK:
            err.write(f"{PROG}: the watcher ended (the session is on no team, or its socket refuses "
                      "connections): the supervisor exits\n")
            return EXIT_OK
        if code in SUPERVISE_FINAL:
            err.write(f"{PROG}: the watcher was refused (exit {code}), which a restart cannot change: "
                      "the supervisor exits\n")
            return code
        if rt.monotonic() - started >= rt.supervise_retry_max_s:
            backoff.reset()
        wait = backoff.next()
        err.write(f"{PROG}: the watcher exited (exit {code}): starting it again in {wait:g} s\n")
        rt.sleep(wait)


def cmd_watch(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """`watch`: one watcher per session socket, by the session's watch lock. `--supervise`
    holds that lock and runs `watch --child`, which runs under it, restarting it (E26)."""
    rt: Runtime = args.runtime
    err = _LockedStream(err)
    relaying = relay.enabled(environ)
    if relaying and (args.supervise or args.child):
        raise config.UsageError("watch --supervise and --child are for socket delivery; in relay "
                                "mode the plugin owns the watcher")
    path, token = ("", "") if relaying else _session_socket(environ)
    members = config.load_active(environ, team=args.team)
    if not relaying and not notify.socket_present(path):
        err.write(f"{PROG}: the session socket is gone: nothing to wake\n")
        return EXIT_OK
    lock = None
    if not relaying and not args.child:
        lock = _session_watch_lock(environ, path, err)
        if lock is None:
            return EXIT_BUSY
    try:
        if args.supervise:
            return _supervise(environ, path, lock, rt, err)
        return _watch(members, path, token, relaying, environ, rt, out, err)
    finally:
        if lock is not None:
            lock.release()


def _watch(members: Sequence[config.Member], path: str, token: str, relaying: bool,
           environ: Mapping[str, str], rt: Runtime, out: TextIO, err: TextIO) -> int:
    """Hold every active team's lock as a watcher (a busy team is reported and skipped;
    exit 75 only when every team is busy), keep the inbox filled, and write each pending item
    to the session's socket as a delivery envelope (SPEC-inject.md), consuming what was
    written. Exits 0 when the socket goes (the session ended). A transient fault or any
    other error is retried (E25); a team with a permanent failure is reported and dropped,
    and the others go on; once no team is left, it exits with the first failure's code."""
    waiting = _Waiting(float("inf"))
    held: list[tuple[_Seat, inbox.Lock]] = []
    pending: _Drops = {}
    handed_over: set[str] = set()
    stops: dict[str, _TeamStop] = {}
    following = _Following(environ, err, rt)
    down_home = config.resolve_home(environ)

    def start_team(member: config.Member) -> _Seat | None:
        """Hold one team's lock as a watcher and start its thread; None, said, when busy."""
        seat = _seat(member, environ, rt, err, reads=True)
        try:
            lock = _take_waiter_lock(seat.state, rt, "watch")
        except inbox.Busy as busy:
            err.write(f"{PROG}: team {member.team}: busy: a {busy.holder} process holds the "
                      "sync lock\n")
            return None
        held.append((seat, lock))
        stops[member.team] = _TeamStop()
        thread = threading.Thread(target=_watch_poll,
                                  args=(seat, lock, rt, waiting, err, stops[member.team], down_home),
                                  daemon=True, name=f"pingbus-watch-{member.team}")
        rt.threads.append(thread)
        handed_over.add(member.team)
        thread.start()
        return seat

    try:
        seats = [seat for seat in map(start_team, members) if seat is not None]
        if not seats:
            err.write(f"{PROG}: every active team is busy: another watcher or waiter holds the seat\n")
            return EXIT_BUSY
        home = config.resolve_home(environ)
        ledger = _RelayLedger(relay.Spool(home), relay.Stall(rt.monotonic))
        marks = inject.Marks(home, path)
        if not relaying:
            marks.set_stalled(False)  # nothing is outstanding yet: old marks are not this watcher's
            marks.set_missing(False)
        injector = None if relaying else _Injector(
            path, token, relay.Spool(home), marks, relay.Stall(rt.monotonic, rt.delivery.stall_after_s),
            inject.DeliveryCheck(marks, rt.monotonic, quiet_s=rt.delivery.missing_quiet_s,
                                 give_up_s=rt.delivery.missing_give_up_s),
            envelope.SenderBudget(rt.monotonic))
        reported = 0
        first_failure: int | None = None
        pass_failing = False
        while True:
            if relaying:
                if rt.stopping.is_set():
                    return EXIT_OK
            elif not notify.socket_present(path):
                err.write(f"{PROG}: the session socket is gone: the watcher exits\n")
                return EXIT_OK
            waiting.woke.wait(rt.tick_s)
            waiting.woke.clear()
            _collect_drops(waiting, pending)
            if following.on:
                seats = following.follow(seats, stops, start_team)
                try:
                    if injector is not None:
                        _tell(environ, injector, rt, err)
                except notify.SocketGone as gone:
                    err.write(f"{PROG}: {gone}: the watcher exits\n")
                    return EXIT_OK
                if not seats:
                    err.write(f"{PROG}: this session is on no team now: the watcher exits\n")
                    return EXIT_OK
            elif injector is not None:
                try:
                    _tell(environ, injector, rt, err, hooks.tell_roles)
                except notify.SocketGone as gone:
                    err.write(f"{PROG}: {gone}: the watcher exits\n")
                    return EXIT_OK
            try:
                if relaying:
                    _relay_pass(seats, ledger, rt, pending, err)
                else:
                    _inject_pass(seats, injector, rt, pending, err)
            except notify.SocketGone as gone:
                err.write(f"{PROG}: {gone}: the watcher exits\n")
                return EXIT_OK
            except Exception as exc:  # contained (E25): the next tick tries again
                if not pass_failing:
                    err.write(f"{PROG}: a delivery pass failed ({_describe(exc)}): trying again\n")
                pass_failing = True
            else:
                if pass_failing:
                    err.write(f"{PROG}: delivery passes work again\n")
                pass_failing = False
            _emit_drops(pending, out, err)
            while len(waiting.failures) > reported:
                team, exc = waiting.failures[reported]
                reported += 1
                if all(seat.member.team != team for seat in seats):
                    continue  # a team the owner removed meanwhile
                if not isinstance(exc, FAILURES):
                    raise exc
                code, message = failure(exc)
                err.write(f"{PROG}: team {team}: {message}: this team is no longer watched\n")
                first_failure = first_failure or code
                following.failed.add(team)
                seats = [seat for seat in seats if seat.member.team != team]
            if not seats:
                err.write(f"{PROG}: no team is left to watch: the watcher exits\n")
                return first_failure
    finally:
        waiting.stop.set()
        for stop in stops.values():
            stop.set()
        for seat, lock in held:
            if seat.member.team not in handed_over:
                lock.release()


class _Following:
    """Plan 00203 H11: a watcher or waiter under `PINGBUS_MEMBERSHIP` follows the session's
    active teams as the owner changes them, each tick: a team that went is stopped (its
    thread releases the sync lock after the long poll in flight, and what that poll fetched
    is discarded) and no longer delivered; a team that came is started. A team whose sync
    lock is busy (a `wait` took it first) is tried again with the watcher's backoff until it
    starts. A team dropped for a permanent failure is not started again until it has left
    and come back. Without the membership, nothing changes at runtime."""

    def __init__(self, environ: Mapping[str, str], err: TextIO, rt: Runtime) -> None:
        self.environ, self.err, self.rt = environ, err, rt
        marker = environ.get(membership.MEMBERSHIP_VAR)
        self.on = marker is not None and marker != membership.MEMBERSHIP_OFF
        self.failed: set[str] = set()
        self.busy: dict[str, tuple[_Backoff, float]] = {}
        self._refused = False

    def _retry_later(self, team: str) -> None:
        """A busy lock is transient (H11): the team is tried again after the next backoff."""
        backoff, _ = self.busy.get(team) or (_Backoff(self.rt.outage_retry_s, self.rt.outage_retry_max_s), 0.0)
        self.busy[team] = (backoff, self.rt.monotonic() + backoff.next())

    def active(self) -> tuple[str, ...] | None:
        """The active teams now; None, said once, while the files are refused (the seats
        then stay as they are, as the keeper keeps them)."""
        try:
            desired = membership.read_desired(membership.mount_dir(self.environ[membership.MEMBERSHIP_VAR]))
            teams = membership.active(desired, membership.read_applied(config.resolve_home(self.environ)))
        except membership.MembershipError as exc:
            if not self._refused:
                self.err.write(f"{PROG}: the membership is refused, so the teams stay as they are: {exc}\n")
            self._refused = True
            return None
        self._refused = False
        return teams

    def follow(self, seats: list[_Seat], stops: Mapping[str, _TeamStop],
               start: Callable[[config.Member], _Seat | None] | None) -> list[_Seat]:
        teams = self.active()
        if teams is None:
            return seats
        self.failed &= set(teams)
        self.busy = {team: due for team, due in self.busy.items() if team in teams}
        kept = []
        for seat in seats:
            if seat.member.team in teams:
                kept.append(seat)
                continue
            stops[seat.member.team].remove()
            self.err.write(f"{PROG}: team {seat.member.team}: removed from this session: no longer watched\n")
        if start is None:
            return kept
        watched = {seat.member.team for seat in kept}
        for team in teams:
            if team in watched or team in self.failed:
                continue
            if team in self.busy and self.rt.monotonic() < self.busy[team][1]:
                continue
            try:
                member = config.load_bundle(config.resolve_home(self.environ), team)
            except config.ConfigError as exc:
                self.err.write(f"{PROG}: team {team}: added, but its bundle is refused: {exc}\n")
                self.failed.add(team)
                continue
            seat = start(member)
            if seat is not None:
                self.busy.pop(team, None)
                self.err.write(f"{PROG}: team {team}: added to this session: watched\n")
                kept.append(seat)
            else:
                self._retry_later(team)
        return kept


def _tell(environ: Mapping[str, str], inj: _Injector, rt: Runtime, err: TextIO,
          teller: Callable[[Mapping[str, str], Callable[[list[str]], bool]], bool | None] = hooks.tell_membership) -> None:
    """H12: tell the session, through its socket, what changed in its teams, once per change:
    a ccy session its membership (hooks.tell_membership), a session outside the membership a
    change of its role (hooks.tell_roles, Plan 00203 Task 3.8). A write that fails is left to
    the next prompt's hook (H13); the socket gone ends the watcher (`SocketGone`)."""

    def deliver(lines: list[str]) -> bool:
        text = envelope.render_membership(lines, secrets.token_hex(4))
        try:
            notify.send_notice(inj.path, inj.token, text, timeout_s=rt.delivery.send_timeout_s)
        except notify.SocketGone:
            raise
        except OSError as exc:
            err.write(f"{PROG}: the {what} was not written ({exc.strerror or exc}): the next "
                      "prompt's hook tells it\n")
            return False
        inj.last_sent = rt.monotonic()
        return True

    what = "role notice (this session's role changed)" if teller is hooks.tell_roles else "membership notice"
    try:
        if teller is hooks.tell_roles:
            hooks.tell_roles(environ, deliver, err=err)
        else:
            teller(environ, deliver)
    except (hooks.Failure, membership.MembershipError, OSError, ValueError) as exc:
        err.write(f"{PROG}: the {what} could not be built ({type(exc).__name__})\n")


# ---------------------------------------------------------------- history


def cmd_history(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """What this member received and sent in each active team (or `--team`), newest first,
    read from the room: a record, never work. Nothing is consumed, acked or stored. A team
    that fails is reported and the others still print; the exit is the first failure's."""
    rt: Runtime = args.runtime
    limit = args.limit
    if not 1 <= limit <= syncer.HISTORY_MAX_ITEMS:
        raise config.UsageError(f"history --limit: from 1 to {syncer.HISTORY_MAX_ITEMS}")
    first_failure: int | None = None
    for member in config.load_active(environ, team=args.team):
        try:
            found = _seat(member, environ, rt, err).syncer.history(limit, args.room)
            lines = [history_line(member.team, member.server_name, item) for item in found.items]
        except FAILURES as exc:
            code, message = failure(exc)
            err.write(f"{PROG}: team {member.team}: {message}\n")
            first_failure = first_failure or code
            continue
        for line in lines:
            emit(line, out, err)
        if found.drops:
            emit(dropped_line(found.drops), out, err)
        if not found.complete:
            scanned = syncer.HISTORY_MAX_PAGES * syncer.HISTORY_PAGE_LIMIT
            err.write(f"{PROG}: team {member.team}: history read the newest {scanned} room "
                      "messages; older messages were not read\n")
    return EXIT_OK if first_failure is None else first_failure


# ---------------------------------------------------------------- hooks

HOOK_INPUT_MAX = hooks.INPUT_MAX_BYTES


def cmd_hook(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """A Claude Code hook: stdin hook JSON, stdout hook JSON, always exit 0 (§13)."""
    rt: Runtime = args.runtime
    raw = args.stdin.read(HOOK_INPUT_MAX + 1)
    post = rt.post or functools.partial(post_goodbye, rt, environ, err)
    result = hooks.run(args.event, raw, environ, now_ms=rt.clock_ms(), spawn=rt.spawn, err=err,
                       post=post)
    out.write(json.dumps(result, ensure_ascii=True) + "\n")
    return EXIT_OK


def cmd_status_line(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """The session's status line, with its agent-bus teams (statusline.py): the kit's statusLine."""
    text, status = statusline.render(args.stdin.read(HOOK_INPUT_MAX + 1), environ, err=err)
    out.write(text + "\n")
    return status


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
    return _localpart(outcome.item.sender, server_name)


def _pending_kind(outcome: protocol.Outcome) -> str:
    if outcome.ping is not None:
        return _verb(outcome.ping.verb)
    return "human" if outcome.human is not None else "peer"


def cmd_inbox(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Every pending item, oldest first, without consuming it: team, event ID, sender,
    time, and the verb, `human` or `peer`. Never the text: only `recv` prints that, from the copy it
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
            kind = _pending_kind(item.outcome)
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


def relay_state(environ: Mapping[str, str]) -> str:
    """`off` without relay delivery, else `stalled` while the watcher recorded that no
    relay was confirmed in time, else `on`."""
    if not relay.enabled(environ):
        return "off"
    return "stalled" if relay.Spool(config.resolve_home(environ)).stalled() else "on"


def _status_team(member: config.Member, now_ms: int, relay_field: str, out: TextIO) -> None:
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
        fields += ["pending=-", "humans=-", "peers=-", "pings=-"]
        unexpected: list[str] = []
    else:
        total, humans, peers, pings = state.pending(record.context(member.server_name), member.user_id,
                                                    human_text=member.human_text).counts()
        fields += [f"pending={total}", f"humans={humans}", f"peers={peers}", f"pings={pings}"]
        known = set(record.humans) | set(record.roles) | {member.admin}
        unexpected = sorted(uid for uid in members if uid not in known)
    fields += [f"overdue={overdue}", f"dropped={dropped}", f"ignored={state.ignored_count()}",
               "unexpected=-" if record is None else f"unexpected={len(unexpected)}",
               f"relay={relay_field}"]
    trust = "trust=trusted" if record is not None else "trust=untrusted"
    out.write(report_line("TEAM", _team(member.team), member.handle, trust, *fields) + "\n")
    about = state.about() if record is not None else None
    if about is not None:
        out.write(format_line("ABOUT", _team(member.team), json.dumps(about, ensure_ascii=True)) + "\n")
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
    side = state.side_rooms()
    for purpose in protocol.PURPOSES:
        if purpose in side:
            found = side[purpose]
            out.write(report_line("ROOM", member.team, purpose, found.state, found.reason or ABSENT) + "\n")


def cmd_status(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Per active team, offline: the handle, room trust (and why not), the wake path from
    the lock, pending, overdue, dropped and ignored counts, unexpected members; then each member of
    the last verified team record with its role or `human` and its status. A team that
    fails is reported and prints nothing; the others still run."""
    rt: Runtime = args.runtime
    now = rt.clock_ms()
    relay_field = relay_state(environ)
    first_failure: int | None = None
    for member in config.load_active(environ, team=args.team):
        lines = io.StringIO()
        try:
            _status_team(member, now, relay_field, lines)
        except FAILURES as exc:
            code, message = failure(exc)
            err.write(f"{PROG}: team {member.team}: {message}\n")
            first_failure = first_failure or code
            continue
        out.write(lines.getvalue())
    try:
        seat_lines, seat_failures = seating.seat_lines(rt.seats_root, config.resolve_home(environ))
    except inbox.StateError as exc:
        seat_lines, seat_failures = [], [str(exc)]
    for line in seat_lines:
        out.write(line + "\n")
    for message in seat_failures:
        err.write(f"{PROG}: {message}\n")
        first_failure = first_failure or EXIT_CONFIG
    return EXIT_OK if first_failure is None else first_failure


# ---------------------------------------------------------------- the claim: seat exec


def cmd_seat_exec(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Start the seat keeper, which takes every seat of the launch (the desired membership
    `PINGBUS_MEMBERSHIP` names), builds the session home
    and checks every bundle; check the forge credential source as `config check` does when a
    seat is held; then replace this process with the command. The keeper holds every seat
    lock and the command holds none (Plan 00203 D4). A membership with no seat is a session
    on no team: the home and its settings, nothing held.
    `--root` and `--home` are for a launch on the host (`cc --teams`, `cc --bus`), where the checkout
    is not /workspace and /tmp is shared."""
    rt: Runtime = args.runtime
    command = list(args.command)
    if command[:1] == ["--"]:
        command = command[1:]
    if not command:
        raise config.UsageError(f"{PROG} seat exec: name the command to run")
    root, home = _seat_paths(args, rt, "seat exec")
    want = seating.desired_source(environ).read()
    inner, guard, warnings = None, True, []
    try:
        inner = seating.inner_status_line(seating.settings_sources(environ, pathlib.Path.cwd()))
    except seating.SettingsUnreadable as exc:
        guard = False
        warnings.append(f"{exc}: the session keeps Claude Code's own status line, with no bus "
                        "segment; fix the file and relaunch to get it")
    if want.refs:
        forge_credential_source(environ)
    _start_keeper(rt, environ, root, home, err)
    applied = membership.read_applied(home)
    seats = () if applied is None else applied.seats
    # Inbound is decided here, once (H33b): a session launched on no team accepts none,
    # whatever the keeper takes later; its first join relaunches it (M3).
    seating.write_session_files(home, inner, seated=bool(seats), guard=guard)
    child_env = dict(environ)
    child_env["PINGBUS_HOME"] = str(home)
    child_env.pop("PINGBUS_TEAMS", None)
    if seats:
        child_env["PINGBUS_TEAMS"] = ",".join(entry.team for entry in seats)
    names = ", ".join(f"{entry.seat}@{entry.team} ({entry.handle})" for entry in seats)
    for warning in warnings:
        err.write(f"⚠ agent team bus: {warning}\n")
    err.write(f"✓ agent team bus: {names or 'no team (the kit is loaded and idle)'}\n")
    err.flush()
    out.flush()
    try:
        rt.execvpe(command[0], command, child_env)
    except OSError as exc:
        raise config.ConfigError(f"{PROG} seat exec: cannot run {command[0]}: {exc.strerror or exc}") from None
    return EXIT_OK


def _seat_paths(args: argparse.Namespace, rt: Runtime, name: str) -> tuple[pathlib.Path, pathlib.Path]:
    """`--root` and `--home`, absolute, else ccy's."""
    paths = []
    for option, given, default in (("--root", args.root, rt.seats_root),
                                   ("--home", args.home, rt.session_home)):
        if given is not None and not os.path.isabs(given):
            raise config.UsageError(f"{PROG} {name}: {option} must be an absolute path")
        paths.append(default if given is None else pathlib.Path(given))
    return paths[0], paths[1]


def _start_keeper(rt: Runtime, environ: Mapping[str, str], root: pathlib.Path, home: pathlib.Path,
                  err: TextIO) -> None:
    """Start this launch's seat keeper (Plan 00203 D4) and wait for it to say the seats are
    taken. A keeper that refuses is this command's failure, with its message and exit code."""
    argv = [*rt.keeper_argv(), "seat", "keep", "--launch", "--parent", str(os.getpid()),
            "--root", str(root), "--home", str(home)]
    proc = rt.spawn_keeper(argv, environ)
    with selectors.DefaultSelector() as selector:
        selector.register(proc.stdout, selectors.EVENT_READ)
        if not selector.select(KEEPER_READY_S):
            proc.kill()
            proc.wait()
            raise config.ConfigError(f"{PROG} seat exec: the seat keeper did not answer within "
                                     f"{KEEPER_READY_S:g} s")
    ready = proc.stdout.readline() == KEEPER_READY
    said = proc.stderr.read().decode("utf-8", errors="replace") if ready else ""
    if not ready:
        code = proc.wait()
        said = proc.stderr.read().decode("utf-8", errors="replace")
        err.write(said)
        if code > 0:
            raise _KeeperFailed(code)
        raise config.ConfigError(f"{PROG} seat exec: the seat keeper ended without taking the seats")
    err.write(said)


class _KeeperFailed(Exception):
    """The keeper refused the launch and said why on stderr; its exit code is this command's."""

    def __init__(self, code: int) -> None:
        super().__init__(f"the seat keeper exited {code}")
        self.code = code


def cmd_seat_keep(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """The seat keeper (Plan 00203 D4, H9, H10): take the session's seats and hold them,
    following the desired membership, until the parent named by `--parent` is gone (or, with
    no `--parent`, as Repair runs it in a container, until it is killed). `--launch` is `seat
    exec`'s: a new home, all or nothing, and READY on stdout once the seats are taken."""
    rt: Runtime = args.runtime
    root, home = _seat_paths(args, rt, "seat keep")
    if args.parent is not None:
        rt.die_with_parent()
        if os.getppid() != args.parent:
            err.write(f"{PROG} seat keep: the session it was started for has ended\n")
            return EXIT_OK
    keeper = seating.Keeper(seating.desired_source(environ), root=root, home=home,
                            clock_ms=rt.clock_ms, err=err, sleep=rt.sleep, environ=environ,
                            spawn_watcher=rt.spawn)
    keeper.start(launch=args.launch)
    try:
        rt.detach(home / seating.KEEPER_LOG, args.launch)
    except BaseException:
        keeper.release_all()
        raise
    return seating.keep(keeper, parent=args.parent, sleep=rt.sleep)


#: How long the route probe waits for the homeserver (Plan 00203 H22 step 5).
REACH_TIMEOUT_S = 3.0


def cmd_reach(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Whether this environment reaches the team's homeserver (Plan 00203 H22 step 5, run
    by the host inside a session before a hot add): an unauthenticated versions request with
    a 3 s limit, to the base URL of the team's seats in this checkout."""
    rt: Runtime = args.runtime
    team = getattr(args, "team", None)
    if team is None:
        raise config.UsageError(f"{PROG} reach: name the team with --team")
    if not protocol.is_team_name(team):
        raise config.UsageError(f"{PROG} reach: --team is not a team name")
    root, _ = _seat_paths(argparse.Namespace(root=args.root, home=None), rt, "reach")
    refs = seating.list_seats(root, team=team)
    if not refs:
        raise config.ConfigError(f"team {team}: no seat under {root}: the host creates one first")
    member = config.read_member(team, seating.seat_dir(root, refs[0]))
    started = rt.monotonic()
    try:
        matrix.Client.for_member(member, sleep=rt.sleep).versions(timeout=REACH_TIMEOUT_S)
    except matrix.MatrixError as exc:
        err.write(f"{PROG}: team {team}: {member.base_url} did not answer: {exc}\n")
        return exc.exit_code
    err.write(f"✓ team {team}: {member.base_url} answers ({(rt.monotonic() - started) * 1000:.0f} ms)\n")
    return EXIT_OK


def cmd_seat_retire(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Re-baseline a seat removed from a session (Plan 00203 H23 step 5), run on the host once
    the seat is free: clear its sync position, so a later re-add starts as a first sync and
    nothing posted meanwhile is delivered, then publish an expired `listening` status. The
    seat's lock is held throughout, so no session can take the seat half retired."""
    rt: Runtime = args.runtime
    with _free_seat(args, rt, "seat retire") as member:
        ref = f"{args.seat}"
        state = inbox.TeamState.for_member(member)
        with _sync_lock_waited(state, rt):
            cleared = state.forget_sync_token()
        err.write(f"✓ seat {ref}: sync position {'cleared' if cleared else 'already clear'}\n")
        _seat(member, environ, rt, err).syncer.publish_status(rt.clock_ms())
        err.write(f"✓ seat {ref}: listening status expired\n")
    return EXIT_OK


@contextlib.contextmanager
def _free_seat(args: argparse.Namespace, rt: Runtime, name: str) -> Iterator[config.Member]:
    """The one `<seat>@<team>` named, held by this process (so no session takes it
    meanwhile), as its member; `SeatHeld` (75) when a session holds it."""
    root, _ = _seat_paths(argparse.Namespace(root=args.root, home=None), rt, name)
    refs = seating.parse_seat_list(args.seat)
    if len(refs) != 1 or not isinstance(refs[0], seating.SeatRef):
        raise config.UsageError(f"{PROG} {name}: name one <seat>@<team>")
    ref = refs[0]
    try:
        seating.check_seat_dir(root, ref)
    except seating.SeatRefused as refusal:
        raise refusal.error from None
    try:
        lock = inbox.acquire_lock_at(seating.lock_path(root, ref), inbox.SEAT_KIND,
                                     claimed_ms=rt.clock_ms(), sleep=rt.sleep)
    except inbox.Busy:
        raise seating.SeatHeld(ref) from None
    with lock:
        yield config.read_member(ref.team, seating.seat_dir(root, ref))


def _sync_lock_waited(state: inbox.TeamState, rt: Runtime) -> inbox.Lock:
    """The team's sync lock as `recv`, waited for up to `rt.sync_wait_s`: a removed team's
    watcher thread lets it go once its long poll ends. Still held then: `Busy` (75)."""
    give_up = rt.monotonic() + rt.sync_wait_s
    while True:
        try:
            return inbox.acquire_lock(state, "recv", sleep=rt.sleep)
        except inbox.Busy:
            if rt.monotonic() >= give_up:
                raise
        rt.sleep(RECV_HOLD_RETRY_S * 5)


#: Plan 00203 H23 step 6: the goodbye of a seat the owner removed from a session.
FAREWELL_TEXT = "{handle} left the team: removed by the owner"


def cmd_seat_farewell(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO, err: TextIO) -> int:
    """Post the goodbye of a seat removed from a session (Plan 00203 H23 step 6), on the host,
    once the seat is free: a fixed text from the seat's own member, to the team's humans and
    coordinator(s), as the session-end goodbye is. The seat is held throughout."""
    rt: Runtime = args.runtime
    with _free_seat(args, rt, "seat farewell") as member:
        _post_to_humans_and_coordinators(member, environ, rt, err,
                                         FAREWELL_TEXT.format(handle=member.handle))
    err.write(f"✓ seat {args.seat}: goodbye posted\n")
    return EXIT_OK


#: Plan 00194 Task 2.0b: what the host says of a seat whose session ended without its goodbye.
DISCONNECT_TEXT = "{handle} disconnected: session ended without a goodbye"
DISCONNECT_POLL_S = 0.5


def cmd_seat_disconnected(args: argparse.Namespace, environ: Mapping[str, str], out: TextIO,
                          err: TextIO) -> int:
    """On the host, once a launch's container has gone (protocol §12): for each seat the
    session last held, by its report beside the checkout's seats, a seat whose lock still
    names a claim of this launch and no goodbye for it is told `DISCONNECT_TEXT`, from its
    member to the team's humans and coordinator(s). Holding the lock meanwhile, and letting
    it go, empties it, so a claim is told once. The exit is the first failure's.

    It runs as a session ends, when its terminal may have gone: SIGINT and SIGHUP are ignored
    meanwhile (Ctrl-C pressed again reaches the launcher's process group), and every seat is
    told before any line is written, so a stderr that has gone stops no notice
    (`_report_unless_gone`)."""
    rt: Runtime = args.runtime
    root, _ = _seat_paths(argparse.Namespace(root=args.root, home=None), rt, "seat disconnected")
    if not membership.is_session_name(args.session) or args.since < 0:
        raise config.UsageError(f"{PROG} seat disconnected: --session is a session's name and "
                                "--since a time in epoch milliseconds")
    report = root.parent / seating.SESSIONS_DIR / args.session
    try:
        applied = membership.read_applied(report)
    except membership.MembershipError as exc:
        raise config.ConfigError(f"{PROG} seat disconnected: {exc}") from None
    first_failure: int | None = None
    lines = io.StringIO()
    outcomes: list[tuple[str, str]] = []
    if applied is None or not applied.seats:
        outcomes.append(("-", "not told: " + ("no applied session report" if applied is None
                                              else "the session held no seat")))
    with hooks.terminal_signals_ignored():
        for held in () if applied is None else applied.seats:
            ref = seating.SeatRef(held.seat, held.team)
            try:
                outcome = _tell_disconnected(root, ref, args.since, environ, rt, lines)
            except FAILURES as exc:
                code, message = failure(exc)
                lines.write(f"{PROG}: seat {ref.text}: no disconnect notice: {message}\n")
                outcome = f"failed: {message}"
                first_failure = first_failure or code
            outcomes.append((ref.text, outcome))
        log = root.parent / DISCONNECT_LOG
        try:
            _append_disconnect_log(log, rt, args.session, outcomes)
        except OSError as exc:
            lines.write(f"{PROG}: seat disconnected: {log} was not written: {exc.strerror or exc}\n")
            first_failure = first_failure or EXIT_CONFIG
    _report_unless_gone(err, lines.getvalue())
    return EXIT_OK if first_failure is None else first_failure


#: Beside the checkout's seats and sessions: one line per seat `seat disconnected` judged,
#: told or not and why, owner-only, never a message text. A closed terminal takes stderr with
#: it; this file stays. The launcher's `ccy_bus_disconnected` writes its own lines here too.
DISCONNECT_LOG = "disconnect.log"


def _append_disconnect_log(path: pathlib.Path, rt: Runtime, session: str,
                           outcomes: Sequence[tuple[str, str]]) -> None:
    stamp = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(rt.clock_ms() / 1000))
    text = "".join(f"{stamp} pingbus session={session} seat={ref} {outcome}\n" for ref, outcome in outcomes)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW | os.O_CLOEXEC,
                 inbox.FILE_MODE)
    try:
        os.fchmod(fd, inbox.FILE_MODE)
        os.write(fd, text.encode("utf-8"))
    finally:
        os.close(fd)


#: What a write to the stderr of a closed terminal or tmux window fails with: the pty has gone.
TERMINAL_GONE_ERRNOS = frozenset({errno.EIO, errno.EBADF, errno.EPIPE})


def _report_unless_gone(err: TextIO, text: str) -> None:
    """`text` to `err`, unless `err` is a terminal that has gone. Then the report has nowhere to
    go: the notices are posted and the exit code still says how it went. Any other write
    failure is raised."""
    try:
        err.write(text)
        err.flush()
    except OSError as exc:
        if exc.errno not in TERMINAL_GONE_ERRNOS:
            raise


def _tell_disconnected(root: pathlib.Path, ref: seating.SeatRef, since: int, environ: Mapping[str, str],
                       rt: Runtime, err: TextIO) -> str:
    """Tell the seat's team, unless its claim is not this launch's to tell; the outcome, for
    `DISCONNECT_LOG`."""
    path = seating.lock_path(root, ref)
    give_up = rt.monotonic() + rt.disconnect_wait_s
    while inbox.probe_lock_at(path) is not None:
        if rt.monotonic() >= give_up:
            err.write(f"{PROG}: seat {ref.text}: still held {rt.disconnect_wait_s:g} s after the "
                      "session ended, so it is left to its holder; no disconnect notice\n")
            return f"not told: still held {rt.disconnect_wait_s:g} s after the session ended"
        rt.sleep(DISCONNECT_POLL_S)
    claimed = inbox.read_claim_at(path)
    if claimed is None:
        return "not told: no claim (the seat was let go in order, or never claimed)"
    if claimed < since:
        return f"not told: older than this launch (claim {claimed}, launch {since})"
    try:
        lock = inbox.acquire_lock_at(path, inbox.SEAT_KIND, sleep=rt.sleep)
    except inbox.Busy:
        return "not told: a session took the seat meanwhile"
    with lock:
        if inbox.parse_claim(lock.previous) != claimed:
            return "not told: the claim changed meanwhile"
        directory = seating.seat_dir(root, ref)
        try:
            said = seating.goodbye_said(directory, claimed)
        except inbox.StateError as exc:
            err.write(f"{PROG}: seat {ref.text}: its goodbye record is unreadable, so none is "
                      f"counted: {exc}\n")
            said = False
        if said:
            return f"not told: goodbye already posted (claim {claimed})"
        member = config.read_member(ref.team, directory)
        _post_to_humans_and_coordinators(member, environ, rt, err, DISCONNECT_TEXT.format(handle=member.handle))
    err.write(f"✓ seat {ref.text}: the session ended without a goodbye, so the team was told it "
              "disconnected\n")
    return f"told: claim {claimed}"


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

    def command(name: str, handler: object, help_text: str, epilog: str | None = None
                ) -> argparse.ArgumentParser:
        sub = commands.add_parser(name, help=help_text, epilog=epilog,
                                  formatter_class=argparse.RawDescriptionHelpFormatter)
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
    send = command("send", cmd_send, "send a ping (the only way to emit one)", epilog=TO_FORMS)
    send.add_argument("verb", metavar="VERB")
    send.add_argument("ref", nargs="?", metavar="REF")
    send.add_argument("--to", metavar="HANDLE[,HANDLE...]",
                      help="handles, human localparts or full user IDs")
    send.add_argument("--to-coordinator", action="store_true",
                      help="the team's coordinator, besides any --to")
    send.add_argument("--re", metavar="EVENT_ID")
    send.add_argument("--branch", metavar="BRANCH",
                      help="the branch holding the commit; by default the one this checkout tracks")
    say = command("say", cmd_say, "send text from stdin to the team's humans and agents",
                  epilog=TO_FORMS)
    say.add_argument("--to", metavar="NAME[,NAME...]",
                     help="human localparts, handles or full user IDs; never @room")
    say.add_argument("--to-coordinator", action="store_true",
                     help="the team's coordinator, besides any --to")
    say.add_argument("--re", metavar="EVENT_ID",
                     help="the message this answers (the REPLY command gives it): Element shows "
                          "the text as a reply to it")
    command("recv", cmd_recv, "sync once if the lock is free, then print every pending item")
    wait = command("wait", cmd_wait, "long-poll every active team until an item arrives")
    wait.add_argument("--timeout", type=int, metavar="S",
                      help="seconds (default: the member's wait_timeout_s)")
    watch = command("watch", cmd_watch, "fill the inbox and notify the session socket (SessionStart starts it)")
    watch_mode = watch.add_mutually_exclusive_group()
    watch_mode.add_argument("--supervise", action="store_true",
                            help="run the watcher and start it again until the session ends (what the hooks start)")
    watch_mode.add_argument("--child", action="store_true", help=argparse.SUPPRESS)
    history = command("history", cmd_history,
                      "what this member received and sent, newest first, read from the room")
    history.add_argument("--limit", type=int, default=syncer.HISTORY_DEFAULT_ITEMS, metavar="N",
                         help=f"at most N items per team (default {syncer.HISTORY_DEFAULT_ITEMS}, "
                              f"at most {syncer.HISTORY_MAX_ITEMS})")
    history.add_argument("--room", choices=protocol.RECEIVING_ROOMS, default=protocol.ROOM_MAIN,
                         help="the team room (main, the default) or, for a coordinator, the questions room")
    command("report", cmd_report, "post stdin to the team's reports room, for the humans to read (wakes nobody)")
    ask = command("ask-human", cmd_ask_human,
                  "a coordinator's question for the humans, from stdin, in the questions room")
    ask.add_argument("--asked-by", metavar="NAME[,NAME...]",
                     help="the agents who asked, resolved as say --to resolves them")
    ask.add_argument("--re", metavar="EVENT_ID[,EVENT_ID...]",
                     help="the main-room messages the question came from")
    thread = command("thread", cmd_thread,
                     "a coordinator's post from stdin in a question's thread of the questions room")
    thread.add_argument("question", nargs="?", metavar="Q<n>")
    thread.add_argument("--ask", action="store_true", help="mention the humans, so Element notifies")
    thread.add_argument("--close", action="store_true", help="close the question; the text may be empty")
    thread.add_argument("--re", metavar="EVENT_ID",
                        help="the questions-room message this answers (the REPLY command gives it)")
    questions = command("questions", cmd_questions,
                        "the questions room's questions, their state and the replies not yet answered")
    questions.add_argument("--all", action="store_true", help="closed questions too")
    command("inbox", cmd_inbox, "list pending items without consuming them (offline)")
    command("status", cmd_status, "per team: trust, wake path, counts and members (offline)")
    hook = command("hook", cmd_hook, "a Claude Code hook: stdin hook JSON, stdout hook JSON")
    hook.add_argument("event", choices=hooks.EVENTS, metavar="EVENT",
                      help=" | ".join(hooks.EVENTS))
    command("status-line", cmd_status_line,
            "the Claude Code status line: the session's own, then 👥 <teams> on rows of their own (the kit's statusLine)")
    seat_parser = commands.add_parser("seat", help="this ccy session's seats")
    team_option(seat_parser, argparse.SUPPRESS)
    seat_commands = seat_parser.add_subparsers(dest="seat_command", metavar="SUBCOMMAND",
                                               required=True)
    seat_exec = seat_commands.add_parser(
        "exec", help="claim the launch's seats (PINGBUS_MEMBERSHIP's desired membership), "
                     "then run CMD holding them (ccy's entrypoint, cc --teams, cc --bus)")
    team_option(seat_exec, argparse.SUPPRESS)
    seat_exec.add_argument("--root", metavar="DIR",
                           help="the checkout's seats (default: ccy's, under /workspace)")
    seat_exec.add_argument("--home", metavar="DIR",
                           help="the session home to make (default: ccy's, under /tmp)")
    seat_exec.add_argument("command", nargs=argparse.REMAINDER, metavar="CMD")
    seat_exec.set_defaults(handler=cmd_seat_exec)

    def positive(text: str) -> int:
        if not text.isdigit() or int(text) < 1:
            raise argparse.ArgumentTypeError("not a process ID")
        return int(text)

    seat_keep = seat_commands.add_parser(
        "keep", help="hold the session's seats, following its desired membership (seat exec starts it; "
                     "run alone, it repairs a session whose keeper stopped)")
    team_option(seat_keep, argparse.SUPPRESS)
    seat_keep.add_argument("--root", metavar="DIR", help="the checkout's seats (default: ccy's)")
    seat_keep.add_argument("--home", metavar="DIR", help="the session home (default: ccy's)")
    seat_keep.add_argument("--parent", type=positive, metavar="PID",
                           help="end, letting every seat go, when this parent is gone")
    seat_keep.add_argument("--launch", action="store_true",
                           help="seat exec's keeper: a new home, all or nothing, READY on stdout")
    seat_keep.set_defaults(handler=cmd_seat_keep)
    seat_retire = seat_commands.add_parser(
        "retire", help="on the host, for a seat removed from a session: clear its sync position "
                       "and expire its listening status")
    team_option(seat_retire, argparse.SUPPRESS)
    seat_retire.add_argument("--root", metavar="DIR", help="the checkout's seats (default: ccy's)")
    seat_retire.add_argument("seat", metavar="SEAT@TEAM")
    seat_retire.set_defaults(handler=cmd_seat_retire)
    seat_farewell = seat_commands.add_parser(
        "farewell", help="on the host, for a seat removed from a session: post its goodbye to the "
                         "team's humans and coordinator(s)")
    team_option(seat_farewell, argparse.SUPPRESS)
    seat_farewell.add_argument("--root", metavar="DIR", help="the checkout's seats (default: ccy's)")
    seat_farewell.add_argument("seat", metavar="SEAT@TEAM")
    seat_farewell.set_defaults(handler=cmd_seat_farewell)
    seat_disconnected = seat_commands.add_parser(
        "disconnected", help="on the host, once a launch's container has gone: tell each team of a "
                             "seat it held that ended without its goodbye")
    team_option(seat_disconnected, argparse.SUPPRESS)
    seat_disconnected.add_argument("--root", metavar="DIR", required=True, help="the checkout's seats")
    seat_disconnected.add_argument("--session", metavar="NAME", required=True,
                                   help="the session whose report names its seats")
    seat_disconnected.add_argument("--since", metavar="MS", type=int, required=True,
                                   help="the launch's start, in epoch milliseconds")
    seat_disconnected.set_defaults(handler=cmd_seat_disconnected)
    reach = command("reach", cmd_reach, "whether this environment reaches --team's homeserver (3 s)")
    reach.add_argument("--root", metavar="DIR", help="the checkout's seats (default: ccy's)")
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
    except _KeeperFailed as failed:
        return failed.code
    except FAILURES as exc:
        code, message = failure(exc)
        err.write(f"{PROG}: {message}\n")
        return code


if __name__ == "__main__":
    sys.exit(main())

"""The agent team bus wire protocol, version 1: constants and the pure validator.

Spec (single source of truth): docs/agent-team-bus-protocol.md. Every table there that
this module implements is held equal to the constants below by
tests/helpers/pingbus/test_protocol_doc.py.

Pure: no I/O, no clock, no network. Callers pass in what the room state and the member
config say (`Context`) and get back a parsed `Ping`, or a `Refusal` carrying one of the
closed set of drop reason codes. The same functions run on send (`pingbus send`, the
warden) and on receive (the syncer, inbox reads), so a sender that skips pingbus is
still held to them by every receiver.

What is not decided here: `stale` and `rate` (limits), `unresolved` and `provenance`
(the forge check), duplicates (the inbox's seen set may be passed in), and whether the
member answering an `ack`/`nack` was addressed by the ping it answers (the outbox).
"""

from __future__ import annotations

import dataclasses
import json
import re
from collections.abc import Container, Mapping, Sequence

#: Reverse-domain namespace of every bus event type. Permanent in room history once used;
#: change it here only.
NAMESPACE = "io.github.longtermsupport.agentbus"
PROTOCOL_VERSION = 1

EVENT_TYPE_SUFFIXES = ("ping", "room", "roles", "status", "control")
EVENT_PING = f"{NAMESPACE}.ping"
EVENT_ROOM = f"{NAMESPACE}.room"
EVENT_ROLES = f"{NAMESPACE}.roles"
EVENT_STATUS = f"{NAMESPACE}.status"
EVENT_CONTROL = f"{NAMESPACE}.control"

ROLE_ORCHESTRATOR = "orchestrator"
ROLE_WORKER = "worker"
ROLES = (ROLE_ORCHESTRATOR, ROLE_WORKER)
SENDER_WARDEN = "warden"
#: "May be sent by: anyone addressed by `re`" — any role holder or the warden here; the
#: addressing itself is checked against the outbox, not offline.
SENDER_ADDRESSED = "addressed"
ROLES_MAX = 64
STATUS_LISTENING = "listening"

RESERVED_LOCALPARTS = ("admin", "steward", "warden", "conduit")

HANDLE_PATTERN = (
    r"(?P<repo>[a-z0-9][a-z0-9_-]{0,47})\.(?P<n>[1-9][0-9]{0,5})"
    r"\+(?P<host>[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)"
    r"\.(?P<type>podman|lxc|docker|vm|host)"
)
HUMAN_LOCALPART_PATTERN = r"[a-z][a-z0-9_-]{0,31}"
ROOM_ID_PATTERN = r"![A-Za-z0-9_-]{43}"
EVENT_ID_PATTERN = r"\$[A-Za-z0-9_-]{43}"
ROOM_PAIR_NAME_PATTERN = r"[a-z0-9][a-z0-9-]{0,47}"

OWNER_PATTERN = r"[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?"
REPO_PATTERN = r"[a-z0-9._-]{1,100}"
SHA_PATTERN = r"[0-9a-f]{40}"
NUM_PATTERN = r"[1-9][0-9]{0,9}"
SEG_PATTERN = r"[A-Za-z0-9._-]{1,64}"
PATH_MAX_SEGMENTS = 8
BRANCH_PATTERN = r"[A-Za-z0-9._/-]{1,100}"

REF_FORMS = ("path", "commit", "pr", "issue")
REF_TEMPLATES = {
    "path": "path:OWNER/REPO@SHA:PATH",
    "commit": "commit:OWNER/REPO@SHA",
    "pr": "pr:OWNER/REPO#NUM@SHA",
    "issue": "issue:OWNER/REPO#NUM",
}
MAX_REF_LEN = 700

NOTE_MAX = 80
NOTE_PATTERN = r"[A-Za-z0-9 .,:_/#()=-]{1,80}"

PING_KEYS = ("v", "verb", "to", "ref", "re", "note", "on_behalf_of")
EDIT_KEYS = ("m.relates_to", "m.new_content")
MAX_CONTENT_BYTES = 2048
MAX_TO = 32

DROP_REASONS = (
    "version", "schema", "size", "edit", "sender", "role", "target", "verb", "ref",
    "allowlist", "note", "re", "behalf", "stale", "rate", "unresolved", "provenance",
)
#: Codes this module never produces: limits (`stale`, `rate`) and the forge check.
REASONS_DECIDED_ELSEWHERE = frozenset({"stale", "rate", "unresolved", "provenance"})

REQUIRED = "required"
OPTIONAL = "optional"
FORBIDDEN = "forbidden"


@dataclasses.dataclass(frozen=True)
class VerbRule:
    ref: str
    ref_forms: frozenset[str]
    re: str
    ack_expected: bool
    senders: frozenset[str]


def _rule(ref: str, forms: Sequence[str], re_: str, ack: bool, senders: Sequence[str]) -> VerbRule:
    return VerbRule(ref, frozenset(forms), re_, ack, frozenset(senders))


_ORCH_WARDEN = (ROLE_ORCHESTRATOR, SENDER_WARDEN)
_ORCH_WORKER = (ROLE_ORCHESTRATOR, ROLE_WORKER)

#: The closed verb set, in spec order.
VERBS: dict[str, VerbRule] = {
    "fetch": _rule(REQUIRED, ("path", "commit"), FORBIDDEN, True, _ORCH_WARDEN),
    "sync": _rule(REQUIRED, ("path", "commit"), FORBIDDEN, True, _ORCH_WARDEN),
    "review": _rule(REQUIRED, ("path", "commit", "pr"), FORBIDDEN, True, _ORCH_WORKER),
    "run-qa": _rule(REQUIRED, ("commit", "pr"), FORBIDDEN, True, _ORCH_WORKER),
    "halt": _rule(FORBIDDEN, (), FORBIDDEN, True, _ORCH_WARDEN),
    "ack": _rule(FORBIDDEN, (), REQUIRED, False, (SENDER_ADDRESSED,)),
    "nack": _rule(OPTIONAL, REF_FORMS, REQUIRED, False, (SENDER_ADDRESSED,)),
    "done": _rule(REQUIRED, REF_FORMS, OPTIONAL, False, _ORCH_WORKER),
    "blocked": _rule(REQUIRED, REF_FORMS, OPTIONAL, False, _ORCH_WORKER),
}
#: Verbs that may be addressed to the warden when they answer one of its pings.
VERBS_ANSWERING_WARDEN = frozenset({"ack", "nack", "done", "blocked"})

_HANDLE_RE = re.compile(HANDLE_PATTERN)
_HUMAN_RE = re.compile(HUMAN_LOCALPART_PATTERN)
_ROOM_ID_RE = re.compile(ROOM_ID_PATTERN)
_EVENT_ID_RE = re.compile(EVENT_ID_PATTERN)
_ROOM_PAIR_RE = re.compile(ROOM_PAIR_NAME_PATTERN)
_BRANCH_RE = re.compile(BRANCH_PATTERN)
_NOTE_RE = re.compile(NOTE_PATTERN)
_PATH = rf"{SEG_PATTERN}(?:/{SEG_PATTERN}){{0,{PATH_MAX_SEGMENTS - 1}}}"
_PATH_RE = re.compile(_PATH)
_OWNER_REPO = rf"(?P<owner>{OWNER_PATTERN})/(?P<repo>{REPO_PATTERN})"
_REF_RES = {
    "path": re.compile(rf"path:{_OWNER_REPO}@(?P<sha>{SHA_PATTERN}):(?P<path>{_PATH})"),
    "commit": re.compile(rf"commit:{_OWNER_REPO}@(?P<sha>{SHA_PATTERN})"),
    "pr": re.compile(rf"pr:{_OWNER_REPO}#(?P<num>{NUM_PATTERN})@(?P<sha>{SHA_PATTERN})"),
    "issue": re.compile(rf"issue:{_OWNER_REPO}#(?P<num>{NUM_PATTERN})"),
}
_REF_REPO_PART_RE = re.compile(r"(path|commit|pr|issue):([^@#]*)(.*)", re.S)


class Refusal(ValueError):
    """A validation failure carrying one drop reason code from `DROP_REASONS`."""

    def __init__(self, reason: str) -> None:
        if reason not in DROP_REASONS:
            raise ValueError(f"not a drop reason code: {reason!r}")
        super().__init__(reason)
        self.reason = reason


@dataclasses.dataclass(frozen=True)
class Handle:
    repo: str
    n: int
    host: str
    type: str


@dataclasses.dataclass(frozen=True)
class Ref:
    text: str
    form: str
    owner: str
    repo: str
    sha: str | None
    num: int | None
    path: str | None

    @property
    def repo_full(self) -> str:
        return f"{self.owner}/{self.repo}"


@dataclasses.dataclass(frozen=True)
class Context:
    """What the member config and the room's state say, as the validator needs it.

    `roles` maps user IDs to `orchestrator`/`worker` (from `<ns>.roles`); `repos` maps
    `owner/repo` to its trusted branches; `humans` are the configured humans' user IDs.
    """

    server_name: str
    warden: str
    humans: frozenset[str]
    roles: Mapping[str, str]
    repos: Mapping[str, Sequence[str]]
    path_prefixes: Sequence[str]


@dataclasses.dataclass(frozen=True)
class Ping:
    verb: str
    to: tuple[str, ...]
    ref: Ref | None
    re: str | None
    note: str | None
    on_behalf_of: str | None
    sender: str
    event_id: str | None = None


ACCEPT = "accept"
IGNORE = "ignore"
DROP = "drop"


@dataclasses.dataclass(frozen=True)
class Outcome:
    """`kind` is ACCEPT (with `ping`), IGNORE (silently; `reason` says why) or DROP
    (`reason` is a drop reason code)."""

    kind: str
    reason: str | None = None
    ping: Ping | None = None


def _full(regex: re.Pattern[str], value: object) -> re.Match[str] | None:
    return regex.fullmatch(value) if isinstance(value, str) else None


def parse_handle(value: object) -> Handle | None:
    m = _full(_HANDLE_RE, value)
    if m is None:
        return None
    return Handle(m["repo"], int(m["n"]), m["host"], m["type"])


def is_human_localpart(value: object) -> bool:
    return _full(_HUMAN_RE, value) is not None and value not in RESERVED_LOCALPARTS


def is_room_id(value: object) -> bool:
    return _full(_ROOM_ID_RE, value) is not None


def is_event_id(value: object) -> bool:
    return _full(_EVENT_ID_RE, value) is not None


def is_room_pair_name(value: object) -> bool:
    return _full(_ROOM_PAIR_RE, value) is not None


def is_branch_name(value: object) -> bool:
    return _full(_BRANCH_RE, value) is not None


def parse_user_id(value: object, server_name: str) -> str | None:
    """The localpart of `@<localpart>:<server_name>` when it is a handle, a reserved
    localpart or a human localpart, on exactly this server; else None."""
    if not isinstance(value, str) or not value.startswith("@"):
        return None
    localpart, sep, server = value[1:].partition(":")
    if not sep or server != server_name:
        return None
    if parse_handle(localpart) or localpart in RESERVED_LOCALPARTS or is_human_localpart(localpart):
        return localpart
    return None


def _path_ok(path: str) -> bool:
    return _PATH_RE.fullmatch(path) is not None and not any(
        seg in (".", "..") for seg in path.split("/")
    )


def parse_ref(value: object) -> Ref | None:
    """A §6 reference, or None when it fails the grammar or the length limit."""
    if not isinstance(value, str) or len(value) > MAX_REF_LEN:
        return None
    for form, regex in _REF_RES.items():
        m = regex.fullmatch(value)
        if m is None:
            continue
        repo = m["repo"]
        if repo in (".", "..") or repo.endswith(".git"):
            return None
        groups = m.groupdict()
        path = groups.get("path")
        if path is not None and not _path_ok(path):
            return None
        num = groups.get("num")
        return Ref(value, form, m["owner"], repo, groups.get("sha"),
                   int(num) if num else None, path)
    return None


def lowercase_ref_repo(value: str) -> str:
    """Lowercase only `OWNER/REPO` of a reference, as `pingbus send` does before
    validating; anything not shaped like a reference is returned unchanged."""
    m = _REF_REPO_PART_RE.fullmatch(value)
    if m is None:
        return value
    return f"{m[1]}:{m[2].lower()}{m[3]}"


def is_path_prefix(value: object) -> bool:
    """A `path_prefixes` entry: a PATH ending in `/`, or an exact file PATH."""
    if not isinstance(value, str):
        return False
    return _path_ok(value[:-1] if value.endswith("/") else value)


def path_allowed(path: str, prefixes: Sequence[str]) -> bool:
    return any(
        path.startswith(prefix) if prefix.endswith("/") else path == prefix
        for prefix in prefixes
    )


def sender_class(user_id: object, ctx: Context) -> str | None:
    """`warden`, the role the user holds in the room, or None."""
    if user_id == ctx.warden:
        return SENDER_WARDEN
    role = ctx.roles.get(user_id) if isinstance(user_id, str) else None
    return role if role in ROLES else None


def content_size(content: object) -> int:
    """Bytes of the compact UTF-8 JSON serialisation (what `build_ping` sends)."""
    return len(json.dumps(content, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))


def _check_types(content: Mapping[str, object]) -> None:
    for key, value in content.items():
        if key == "v":
            ok = type(value) is int
        elif key == "to":
            ok = isinstance(value, list) and all(isinstance(x, str) for x in value)
        else:
            ok = isinstance(value, str)
        if not ok:
            raise Refusal("schema")


def _check_targets(to: list[str], verb: str, has_re: bool, ctx: Context) -> tuple[str, ...]:
    if not 1 <= len(to) <= MAX_TO or len(set(to)) != len(to):
        raise Refusal("target")
    for user in to:
        if user == ctx.warden:
            if verb not in VERBS_ANSWERING_WARDEN or not has_re:
                raise Refusal("target")
        elif ctx.roles.get(user) not in ROLES or parse_handle(
            parse_user_id(user, ctx.server_name)
        ) is None:
            raise Refusal("target")
    return tuple(to)


def _check_ref(value: object, rule: VerbRule, ctx: Context) -> Ref | None:
    if value is None:
        if rule.ref == REQUIRED:
            raise Refusal("ref")
        return None
    if rule.ref == FORBIDDEN:
        raise Refusal("ref")
    ref = parse_ref(value)
    if ref is None or ref.form not in rule.ref_forms:
        raise Refusal("ref")
    if ref.repo_full not in ctx.repos:
        raise Refusal("allowlist")
    if ref.form == "path" and not path_allowed(ref.path, ctx.path_prefixes):
        raise Refusal("allowlist")
    return ref


def _check_re(value: object, rule: VerbRule) -> str | None:
    if value is None:
        if rule.re == REQUIRED:
            raise Refusal("re")
        return None
    if rule.re == FORBIDDEN or not is_event_id(value):
        raise Refusal("re")
    return value


def _check_note(value: object) -> str | None:
    if value is None:
        return None
    if _NOTE_RE.fullmatch(value) is None or value != value.strip(" ") or "  " in value:
        raise Refusal("note")
    return value


def _check_behalf(value: object, sender: str, ctx: Context) -> str | None:
    if sender == ctx.warden:
        if value is None or value not in ctx.humans:
            raise Refusal("behalf")
        return value
    if value is not None:
        raise Refusal("behalf")
    return None


def validate_content(content: object, sender: str, ctx: Context) -> Ping:
    """Check ping content against spec §4–§7 and §10 (not the sender's role: see
    `check_role`). Returns the parsed ping or raises `Refusal`."""
    if not isinstance(content, dict):
        raise Refusal("schema")
    if any(key in content for key in EDIT_KEYS):
        raise Refusal("edit")
    try:
        size = content_size(content)
    except (TypeError, ValueError):
        raise Refusal("schema") from None
    if size > MAX_CONTENT_BYTES:
        raise Refusal("size")
    if set(content) - set(PING_KEYS) or not {"v", "verb", "to"} <= set(content):
        raise Refusal("schema")
    _check_types(content)
    if content["v"] != PROTOCOL_VERSION:
        raise Refusal("version")
    verb = content["verb"]
    rule = VERBS.get(verb)
    if rule is None:
        raise Refusal("verb")
    to = _check_targets(content["to"], verb, "re" in content, ctx)
    ref = _check_ref(content.get("ref"), rule, ctx)
    re_ = _check_re(content.get("re"), rule)
    note = _check_note(content.get("note"))
    behalf = _check_behalf(content.get("on_behalf_of"), sender, ctx)
    return Ping(verb, to, ref, re_, note, behalf, sender)


def check_role(verb: str, sender: str, ctx: Context) -> None:
    """Raise `Refusal("role")` unless `sender` may send `verb` (spec §5)."""
    rule = VERBS.get(verb)
    if rule is None:
        raise Refusal("verb")
    cls = sender_class(sender, ctx)
    if cls is None:
        raise Refusal("role")
    if SENDER_ADDRESSED not in rule.senders and cls not in rule.senders:
        raise Refusal("role")


def validate_event(
    event: object,
    ctx: Context,
    self_user_id: str,
    seen: Container[str] = frozenset(),
    mirror_all: bool = False,
) -> Outcome:
    """The offline receive steps of spec §8 for one ping-type timeline event.

    `seen` holds event IDs already processed; `mirror_all` is the warden's mode, which
    keeps pings not addressed to it.
    """
    if not isinstance(event, dict) or not is_event_id(event.get("event_id")):
        return Outcome(DROP, "schema")
    event_id = event["event_id"]
    if event_id in seen:
        return Outcome(IGNORE, "seen")
    sender = event.get("sender")
    if sender == self_user_id:
        return Outcome(IGNORE, "self")
    unsigned = event.get("unsigned")
    redacted = isinstance(unsigned, dict) and "redacted_because" in unsigned
    if event.get("type") != EVENT_PING or "state_key" in event or redacted or "content" not in event:
        return Outcome(DROP, "schema")
    body = event["content"]
    if isinstance(body, dict) and any(key in body for key in EDIT_KEYS):
        return Outcome(DROP, "edit")
    if sender_class(sender, ctx) is None:
        return Outcome(DROP, "sender")
    try:
        ping = validate_content(body, sender, ctx)
        check_role(ping.verb, sender, ctx)
    except Refusal as refusal:
        return Outcome(DROP, refusal.reason)
    if self_user_id not in ping.to and not mirror_all:
        return Outcome(IGNORE, "not-addressed")
    return Outcome(ACCEPT, ping=dataclasses.replace(ping, event_id=event_id))


def _state_body(content: object, keys: set[str]) -> dict:
    if not isinstance(content, dict) or set(content) != keys:
        raise Refusal("schema")
    if type(content["v"]) is not int or content["v"] != PROTOCOL_VERSION:
        raise Refusal("schema")
    return content


def parse_roles(content: object, server_name: str) -> dict[str, str]:
    """`<ns>.roles` content: 1 to ROLES_MAX agent handles, exactly one orchestrator."""
    roles = _state_body(content, {"v", "roles"})["roles"]
    if not isinstance(roles, dict) or not 1 <= len(roles) <= ROLES_MAX:
        raise Refusal("schema")
    for user, role in roles.items():
        if role not in ROLES or parse_handle(parse_user_id(user, server_name)) is None:
            raise Refusal("schema")
    if list(roles.values()).count(ROLE_ORCHESTRATOR) != 1:
        raise Refusal("schema")
    return dict(roles)


def parse_status(content: object) -> int:
    """`<ns>.status` content; returns `until` (Unix ms). Staleness is the caller's."""
    body = _state_body(content, {"v", "state", "until"})
    until = body["until"]
    if body["state"] != STATUS_LISTENING or type(until) is not int or until < 0:
        raise Refusal("schema")
    return until


def _room_marker(content: object, key: str) -> str:
    room = _state_body(content, {"v", key})[key]
    if not is_room_id(room):
        raise Refusal("schema")
    return room


def parse_room_marker(content: object) -> str:
    """`<ns>.room` content; returns the paired control room ID."""
    return _room_marker(content, "control")


def parse_control_marker(content: object) -> str:
    """`<ns>.control` content; returns the paired bus room ID."""
    return _room_marker(content, "bus_room")


def build_ping(
    verb: str,
    to: Sequence[str],
    ref: str | None = None,
    re: str | None = None,
    note: str | None = None,
    on_behalf_of: str | None = None,
) -> dict:
    """Ping content with absent fields omitted; validate it before sending."""
    body: dict = {"v": PROTOCOL_VERSION, "verb": verb, "to": list(to)}
    for key, value in (("ref", ref), ("re", re), ("note", note), ("on_behalf_of", on_behalf_of)):
        if value is not None:
            body[key] = value
    return body

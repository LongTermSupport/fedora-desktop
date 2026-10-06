"""The agent team bus wire protocol, version 1: constants and the pure validator.

Spec (single source of truth): docs/agent-bus-protocol.md. Every table there that this
module implements is held equal to the constants below by
tests/helpers/pingbus/test_protocol_doc.py.

Pure: no I/O, no clock, no network. Callers pass in what the team record and the member
config say (`Context`) and get back a parsed `Ping`, `HumanMessage` or (on send only)
`AgentText`, or a `Refusal` carrying one of the closed set of reason codes. The same functions run on send
(`pingbus send`, `pingbus validate`) and on receive (the syncer, inbox reads), so a sender
that skips pingbus is still held to them by every receiver.

What is not decided here: `stale` and `rate` (limits), `unresolved` and `provenance` (the
forge check), and whether the member answering an `ack`/`nack` was addressed by the ping
it answers (the outbox). Room trust beyond the team record and the power levels (the
create event, invites) is the syncer's.
"""

from __future__ import annotations

import dataclasses
import json
import re
from collections.abc import Container, Iterable, Mapping, Sequence

PROTOCOL_VERSION = 1

#: Prefix of every bus event type and content key. Permanent in room history once used;
#: change it here only.
PREFIX = "agent_bus"
EVENT_TEAM = f"{PREFIX}.team"
EVENT_STATUS = f"{PREFIX}.status"
PING_KEY = f"{PREFIX}.ping"
TEXT_KEY = f"{PREFIX}.text"

EVENT_MESSAGE = "m.room.message"
MSGTYPE_PING = "m.notice"
MSGTYPE_HUMAN = "m.text"
MSGTYPE_TEXT = "m.notice"
RENDER_TAG = "[agent-bus]"

ROLE_ORCHESTRATOR = "orchestrator"
ROLE_WORKER = "worker"
ROLES = (ROLE_ORCHESTRATOR, ROLE_WORKER)
#: "May be sent by: anyone addressed by `re`": any role holder here; the addressing itself
#: is checked against the outbox, not offline.
SENDER_ADDRESSED = "addressed"
SENDER_HUMAN = "human"
STATUS_LISTENING = "listening"

RESERVED_LOCALPARTS = ("admin", "conduit")

#: Between `<n>` and `<host>` in a handle. Probe H4 decides whether it stays `+`.
HANDLE_SEP = "+"
HANDLE_TYPES = ("podman", "lxc", "docker", "vm", "host")
#: The handle's `<repo>` and `<host>` parts; the registry's seat key is built from them.
HANDLE_REPO_PATTERN = r"[a-z0-9][a-z0-9_-]{0,47}"
HANDLE_HOST_PATTERN = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"


def handle_pattern(sep: str) -> str:
    """The agent-handle pattern with `sep` as the separator."""
    return (
        rf"(?P<repo>{HANDLE_REPO_PATTERN})\.(?P<n>[1-9][0-9]{{0,5}})"
        rf"{re.escape(sep)}(?P<host>{HANDLE_HOST_PATTERN})"
        rf"\.(?P<type>{'|'.join(HANDLE_TYPES)})"
    )


HANDLE_PATTERN = handle_pattern(HANDLE_SEP)
TEAM_NAME_PATTERN = r"[a-z][a-z0-9-]{0,23}"
HUMAN_LOCALPART_PATTERN = r"[a-z][a-z0-9_-]{0,31}"
ROOM_ID_PATTERN = r"![A-Za-z0-9_-]{43}"
EVENT_ID_PATTERN = r"\$[A-Za-z0-9_-]{43}"

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

CONTENT_KEYS = ("msgtype", "body", "m.mentions", PING_KEY)
PING_KEYS = ("v", "verb", "to", "ref", "re")
PING_REQUIRED_KEYS = ("v", "verb", "to")
EDIT_KEYS = ("m.relates_to", "m.new_content")
MAX_CONTENT_BYTES = 4096
MAX_HUMAN_BODY_BYTES = 16384
MAX_STATUS_BYTES = 256
MAX_TO = 32

TEXT_CONTENT_KEYS = ("msgtype", "body", "m.mentions", TEXT_KEY)
TEXT_OBJECT_KEYS = ("v", "to", "text")
MAX_AGENT_TEXT_BYTES = 4096

#: Text an agent may not send to a human (spec §7). Conservative: a false positive refuses
#: the send, and the agent points at a reference instead.
SECRET_PATTERNS = (
    ("private-key", r"-{5}BEGIN[A-Z0-9 ]*PRIVATE KEY-{5}"),
    ("github-token", r"\bgh[pousr]_[A-Za-z0-9]{30,}"),
    ("github-pat", r"\bgithub_pat_[A-Za-z0-9_]{20,}"),
    ("aws-access-key-id", r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b"),
    ("slack-token", r"\bxox[abposr]-[A-Za-z0-9-]{10,}"),
    ("sk-api-key", r"\bsk-[A-Za-z0-9_-]{20,}"),
    ("google-api-key", r"\bAIza[0-9A-Za-z_-]{35}"),
    ("jwt", r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}"),
    ("bearer", r"(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}"),
    ("ansible-vault", r"\$ANSIBLE_VAULT;"),
    ("url-credentials", r"[A-Za-z][A-Za-z0-9+.-]*://[^\s/:@]+:[^\s/@]+@"),
    ("credential-assignment",
     r"(?i)\b(?:password|passwd|pwd|secret|token|access[_-]?token|api[_-]?key|access[_-]?key"
     r"|private[_-]?key|client[_-]?secret)\b[\"']?\s*[:=]\s*[\"']?[^\s\"']{8,}"),
)

TEAM_KEYS = ("v", "team", "humans", "roles", "repos", "path_prefixes", "forge_api")
FORGE_API_PATTERN = r"https://[^\s/]+(/\S*)?"
HUMANS_MAX = 16
ROLES_MAX = 64
REPOS_MAX = 32
BRANCHES_MAX = 8
PATH_PREFIXES_MAX = 32
STATUS_KEYS = ("v", "state", "until")

HUMAN_POWER = 50

DROP_REASONS = (
    "version", "schema", "size", "edit", "sender", "role", "target", "verb", "ref",
    "allowlist", "re", "body", "stale", "rate", "unresolved", "provenance",
)
#: The ignore reason for an agent's text to humans received by an agent (spec §7, §9):
#: it is for the humans, so an agent ignores it silently and it is never a drop.
IGNORE_AGENT_TEXT = "agent-text"
#: Codes that refuse a send and never name a receive drop.
SEND_REFUSALS = ("secret",)
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


_ORCH = (ROLE_ORCHESTRATOR,)
_ORCH_WORKER = (ROLE_ORCHESTRATOR, ROLE_WORKER)

#: The closed verb set, in spec order.
VERBS: dict[str, VerbRule] = {
    "fetch": _rule(REQUIRED, ("path", "commit"), FORBIDDEN, True, _ORCH),
    "sync": _rule(REQUIRED, ("path", "commit"), FORBIDDEN, True, _ORCH),
    "review": _rule(REQUIRED, ("path", "commit", "pr"), FORBIDDEN, True, _ORCH_WORKER),
    "run-qa": _rule(REQUIRED, ("commit", "pr"), FORBIDDEN, True, _ORCH_WORKER),
    "halt": _rule(FORBIDDEN, (), FORBIDDEN, True, _ORCH),
    "ack": _rule(FORBIDDEN, (), REQUIRED, False, (SENDER_ADDRESSED,)),
    "nack": _rule(OPTIONAL, REF_FORMS, REQUIRED, False, (SENDER_ADDRESSED,)),
    "done": _rule(REQUIRED, REF_FORMS, OPTIONAL, False, _ORCH_WORKER),
    "blocked": _rule(REQUIRED, REF_FORMS, OPTIONAL, False, _ORCH_WORKER),
}
#: Verbs whose `to` may name a listed human as well as role holders.
VERBS_TO_HUMANS = frozenset({"ack", "nack", "done", "blocked"})

_HANDLE_RE = re.compile(HANDLE_PATTERN)
_TEAM_RE = re.compile(TEAM_NAME_PATTERN)
_HUMAN_RE = re.compile(HUMAN_LOCALPART_PATTERN)
_ROOM_ID_RE = re.compile(ROOM_ID_PATTERN)
_EVENT_ID_RE = re.compile(EVENT_ID_PATTERN)
_BRANCH_RE = re.compile(BRANCH_PATTERN)
_PATH = rf"{SEG_PATTERN}(?:/{SEG_PATTERN}){{0,{PATH_MAX_SEGMENTS - 1}}}"
_PATH_RE = re.compile(_PATH)
_OWNER_REPO = rf"(?P<owner>{OWNER_PATTERN})/(?P<repo>{REPO_PATTERN})"
_OWNER_REPO_RE = re.compile(_OWNER_REPO)
_REF_RES = {
    "path": re.compile(rf"path:{_OWNER_REPO}@(?P<sha>{SHA_PATTERN}):(?P<path>{_PATH})"),
    "commit": re.compile(rf"commit:{_OWNER_REPO}@(?P<sha>{SHA_PATTERN})"),
    "pr": re.compile(rf"pr:{_OWNER_REPO}#(?P<num>{NUM_PATTERN})@(?P<sha>{SHA_PATTERN})"),
    "issue": re.compile(rf"issue:{_OWNER_REPO}#(?P<num>{NUM_PATTERN})"),
}
_REF_REPO_PART_RE = re.compile(r"(path|commit|pr|issue):([^@#]*)(.*)", re.S)
_FORGE_API_RE = re.compile(FORGE_API_PATTERN)
_SECRET_RES = tuple((name, re.compile(pattern)) for name, pattern in SECRET_PATTERNS)


class Refusal(ValueError):
    """A validation failure carrying one code from `DROP_REASONS` or `SEND_REFUSALS`."""

    def __init__(self, reason: str) -> None:
        if reason not in DROP_REASONS and reason not in SEND_REFUSALS:
            raise ValueError(f"not a drop reason code: {reason!r}")
        super().__init__(reason)
        self.reason = reason


class Untrusted(ValueError):
    """The team room's record or power levels are not exactly what spec §8 allows."""


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
    """What the validator needs from the member config and the verified team record.

    `roles` maps agent user IDs to `orchestrator`/`worker`; `repos` maps `owner/repo` to
    its trusted branches; `humans` are the team's human user IDs.
    """

    server_name: str
    humans: frozenset[str]
    roles: Mapping[str, str]
    repos: Mapping[str, Sequence[str]]
    path_prefixes: Sequence[str]


@dataclasses.dataclass(frozen=True)
class TeamRecord:
    team: str
    humans: frozenset[str]
    roles: Mapping[str, str]
    repos: Mapping[str, tuple[str, ...]]
    path_prefixes: tuple[str, ...]
    forge_api: str

    def context(self, server_name: str) -> Context:
        return Context(server_name, self.humans, self.roles, self.repos, self.path_prefixes)


@dataclasses.dataclass(frozen=True)
class Ping:
    verb: str
    to: tuple[str, ...]
    ref: Ref | None
    re: str | None
    sender: str | None = None
    event_id: str | None = None
    origin_server_ts: int | None = None


@dataclasses.dataclass(frozen=True)
class AgentText:
    to: tuple[str, ...]
    text: str


@dataclasses.dataclass(frozen=True)
class HumanMessage:
    event_id: str
    sender: str
    origin_server_ts: int
    text: str


ACCEPT = "accept"
IGNORE = "ignore"
DROP = "drop"


@dataclasses.dataclass(frozen=True)
class Outcome:
    """`kind` is ACCEPT (with `ping` or `human`), IGNORE (silently; `reason` says why) or
    DROP (`reason` is a drop reason code)."""

    kind: str
    reason: str | None = None
    ping: Ping | None = None
    human: HumanMessage | None = None


def _full(regex: re.Pattern[str], value: object) -> re.Match[str] | None:
    return regex.fullmatch(value) if isinstance(value, str) else None


def parse_handle(value: object) -> Handle | None:
    m = _full(_HANDLE_RE, value)
    if m is None:
        return None
    return Handle(m["repo"], int(m["n"]), m["host"], m["type"])


def format_handle(repo: str, n: int, host: str, type_: str) -> str:
    return f"{repo}.{n}{HANDLE_SEP}{host}.{type_}"


def is_team_name(value: object) -> bool:
    return _full(_TEAM_RE, value) is not None


def is_human_localpart(value: object) -> bool:
    return _full(_HUMAN_RE, value) is not None and value not in RESERVED_LOCALPARTS


def is_room_id(value: object) -> bool:
    return _full(_ROOM_ID_RE, value) is not None


def is_event_id(value: object) -> bool:
    return _full(_EVENT_ID_RE, value) is not None


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


def is_agent_user_id(value: object, server_name: str) -> bool:
    return parse_handle(parse_user_id(value, server_name)) is not None


def is_human_user_id(value: object, server_name: str) -> bool:
    return is_human_localpart(parse_user_id(value, server_name))


def _path_ok(path: str) -> bool:
    return _PATH_RE.fullmatch(path) is not None and not any(
        seg in (".", "..") for seg in path.split("/")
    )


def _repo_name_ok(repo: str) -> bool:
    return repo not in (".", "..") and not repo.endswith(".git")


def is_owner_repo(value: object) -> bool:
    """A lowercase `OWNER/REPO` as allowlists and references name it (spec §6, §11)."""
    m = _full(_OWNER_REPO_RE, value)
    return m is not None and _repo_name_ok(m["repo"])


def parse_ref(value: object) -> Ref | None:
    """A §6 reference, or None when it fails the grammar or the length limit."""
    if not isinstance(value, str) or len(value) > MAX_REF_LEN:
        return None
    for form, regex in _REF_RES.items():
        m = regex.fullmatch(value)
        if m is None:
            continue
        repo = m["repo"]
        if not _repo_name_ok(repo):
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
    """`human` for a listed human, the role an agent holds, or None (drop `sender`)."""
    if not isinstance(user_id, str):
        return None
    if user_id in ctx.humans:
        return SENDER_HUMAN
    role = ctx.roles.get(user_id)
    return role if role in ROLES else None


def content_size(content: object) -> int:
    """Bytes of the compact UTF-8 JSON serialisation (what `build_ping` sends)."""
    return len(json.dumps(content, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))


def render(ping: Mapping[str, object]) -> str:
    """The canonical `body` of a ping (spec §4)."""
    to = " ".join(sorted(ping["to"]))
    ref = ping.get("ref")
    text = f"{RENDER_TAG} {ping['verb']} {ref if ref is not None else '-'} -> {to}"
    if ping.get("re") is not None:
        text += f" re {ping['re']}"
    return text


def _check_ping_types(obj: Mapping[str, object]) -> None:
    for key, value in obj.items():
        if key == "v":
            ok = type(value) is int
        elif key == "to":
            ok = isinstance(value, list) and all(isinstance(x, str) for x in value)
        else:
            ok = isinstance(value, str)
        if not ok:
            raise Refusal("schema")


def _check_version_first(obj: object) -> None:
    """§1: an object whose `v` is an integer other than 1 is `version`, before any other
    check, so a later version's new keys or envelope are never reported as `schema`."""
    if isinstance(obj, dict):
        v = obj.get("v")
        if type(v) is int and v != PROTOCOL_VERSION:
            raise Refusal("version")


def _check_targets(to: list[str], verb: str, ctx: Context) -> tuple[str, ...]:
    if not 1 <= len(to) <= MAX_TO or len(set(to)) != len(to):
        raise Refusal("target")
    for user in to:
        if ctx.roles.get(user) in ROLES and is_agent_user_id(user, ctx.server_name):
            continue
        if verb in VERBS_TO_HUMANS and user in ctx.humans and is_human_user_id(user, ctx.server_name):
            continue
        raise Refusal("target")
    return tuple(sorted(to))


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


def validate_content(content: object, ctx: Context) -> Ping:
    """Check a ping's `m.room.message` content against spec §4-§6 and §11 (not the
    sender's role: see `check_role`). Returns the parsed ping or raises `Refusal`."""
    if not isinstance(content, dict):
        raise Refusal("schema")
    _check_version_first(content.get(PING_KEY))
    if any(key in content for key in EDIT_KEYS):
        raise Refusal("edit")
    try:
        size = content_size(content)
    except (TypeError, ValueError):
        raise Refusal("schema") from None
    if size > MAX_CONTENT_BYTES:
        raise Refusal("size")
    if set(content) != set(CONTENT_KEYS) or content["msgtype"] != MSGTYPE_PING:
        raise Refusal("schema")
    obj = content[PING_KEY]
    if not isinstance(obj, dict) or set(obj) - set(PING_KEYS):
        raise Refusal("schema")
    if not set(PING_REQUIRED_KEYS) <= set(obj):
        raise Refusal("schema")
    _check_ping_types(obj)
    rule = VERBS.get(obj["verb"])
    if rule is None:
        raise Refusal("verb")
    to = _check_targets(obj["to"], obj["verb"], ctx)
    ref = _check_ref(obj.get("ref"), rule, ctx)
    re_ = _check_re(obj.get("re"), rule)
    if content["m.mentions"] != {"user_ids": list(to)}:
        raise Refusal("schema")
    if not isinstance(content["body"], str):
        raise Refusal("schema")
    if content["body"] != render(obj):
        raise Refusal("body")
    return Ping(obj["verb"], to, ref, re_)


def check_role(verb: str, sender: str, ctx: Context) -> None:
    """Raise `Refusal("role")` unless `sender` holds a role that may send `verb` (§5)."""
    rule = VERBS.get(verb)
    if rule is None:
        raise Refusal("verb")
    cls = sender_class(sender, ctx)
    if cls not in ROLES:
        raise Refusal("role")
    if SENDER_ADDRESSED not in rule.senders and cls not in rule.senders:
        raise Refusal("role")


def strip_reply_fallback(text: str) -> str:
    """Remove a reply's leading block of `>` lines and the one blank line after it."""
    lines = text.split("\n")
    i = 0
    while i < len(lines) and lines[i].startswith(">"):
        i += 1
    if i == 0:
        return text
    if i < len(lines) and lines[i] == "":
        i += 1
    return "\n".join(lines[i:])


def _addressed(content: Mapping[str, object], self_user_id: str) -> bool:
    mentions = content.get("m.mentions")
    if not isinstance(mentions, dict):
        return False
    if mentions.get("room") is True:
        return True
    users = mentions.get("user_ids")
    return isinstance(users, list) and self_user_id in users


def _human_outcome(
    content: Mapping[str, object], event_id: str, sender: str, ts: int, self_user_id: str,
    human_text: bool,
) -> Outcome:
    """Spec §9 human path, steps 04h-09h (10h `stale` and 11h `rate` are the caller's)."""
    if not human_text:
        return Outcome(DROP, "sender")
    if content.get("msgtype") != MSGTYPE_HUMAN:
        return Outcome(DROP, "schema")
    relates = content.get("m.relates_to")
    if relates is not None and not isinstance(relates, dict):
        return Outcome(DROP, "schema")
    if "m.new_content" in content or (relates is not None and relates.get("rel_type") == "m.replace"):
        return Outcome(DROP, "edit")
    body = content.get("body")
    if not isinstance(body, str):
        return Outcome(DROP, "schema")
    if len(body.encode("utf-8")) > MAX_HUMAN_BODY_BYTES:
        return Outcome(DROP, "size")
    if not _addressed(content, self_user_id):
        return Outcome(IGNORE, "not-addressed")
    if relates is not None and "m.in_reply_to" in relates:
        body = strip_reply_fallback(body)
        if body == "":
            return Outcome(IGNORE, "empty")
    return Outcome(ACCEPT, human=HumanMessage(event_id, sender, ts, body))


def validate_event(
    event: object,
    ctx: Context,
    self_user_id: str,
    seen: Container[str] = frozenset(),
    human_text: bool = True,
) -> Outcome:
    """The offline receive steps of spec §9 for one timeline event.

    `seen` holds event IDs already processed; `human_text` is the bundle's flag. An event
    that is not `m.room.message` is ignored before anything else of it is read: only
    messages are pings or human text.
    """
    if not isinstance(event, dict):
        return Outcome(DROP, "schema")
    if event.get("type") != EVENT_MESSAGE:
        return Outcome(IGNORE, "type")
    if not is_event_id(event.get("event_id")):
        return Outcome(DROP, "schema")
    event_id = event["event_id"]
    if event_id in seen:
        return Outcome(IGNORE, "seen")
    sender = event.get("sender")
    if sender == self_user_id:
        return Outcome(IGNORE, "self")
    unsigned = event.get("unsigned")
    redacted = isinstance(unsigned, dict) and "redacted_because" in unsigned
    ts = event.get("origin_server_ts")
    content = event.get("content")
    if "state_key" in event or redacted or not isinstance(content, dict) or type(ts) is not int or ts < 0:
        return Outcome(DROP, "schema")
    cls = sender_class(sender, ctx)
    if cls is None:
        return Outcome(DROP, "sender")
    if cls == SENDER_HUMAN:
        return _human_outcome(content, event_id, sender, ts, self_user_id, human_text)
    if TEXT_KEY in content:
        # Agent text is for humans; beside a ping it is neither, and the sender is at fault.
        if PING_KEY in content:
            return Outcome(DROP, "schema")
        return Outcome(IGNORE, IGNORE_AGENT_TEXT)
    try:
        ping = validate_content(content, ctx)
        check_role(ping.verb, sender, ctx)
    except Refusal as refusal:
        return Outcome(DROP, refusal.reason)
    if self_user_id not in ping.to:
        return Outcome(IGNORE, "not-addressed")
    return Outcome(ACCEPT, ping=dataclasses.replace(
        ping, sender=sender, event_id=event_id, origin_server_ts=ts))


def _list_of(value: object, low: int, high: int) -> list:
    if not isinstance(value, list) or not low <= len(value) <= high or len(value) != len(
        {json.dumps(x, sort_keys=True) for x in value}
    ):
        raise Untrusted("list size or duplicates")
    return value


def _parse_repos(value: object) -> dict[str, tuple[str, ...]]:
    repos: dict[str, tuple[str, ...]] = {}
    for item in _list_of(value, 1, REPOS_MAX):
        if not isinstance(item, dict) or set(item) != {"repo", "branches"}:
            raise Untrusted("repos entry")
        name = item["repo"]
        if not is_owner_repo(name) or name in repos:
            raise Untrusted("repos entry")
        branches = _list_of(item["branches"], 1, BRANCHES_MAX)
        if not all(is_branch_name(b) for b in branches):
            raise Untrusted("branch name")
        repos[name] = tuple(branches)
    return repos


def parse_team_record(content: object, server_name: str, team_name: str) -> TeamRecord:
    """`agent_bus.team` content (spec §8); raises `Untrusted` on any deviation."""
    if not isinstance(content, dict) or set(content) != set(TEAM_KEYS):
        raise Untrusted("team record keys")
    if type(content["v"]) is not int or content["v"] != PROTOCOL_VERSION:
        raise Untrusted("team record version")
    if not is_team_name(content["team"]) or content["team"] != team_name:
        raise Untrusted("team name")
    humans = _list_of(content["humans"], 1, HUMANS_MAX)
    if not all(is_human_user_id(h, server_name) for h in humans):
        raise Untrusted("humans")
    roles = content["roles"]
    if not isinstance(roles, dict) or len(roles) > ROLES_MAX:
        raise Untrusted("roles")
    for user, role in roles.items():
        if role not in ROLES or not is_agent_user_id(user, server_name):
            raise Untrusted("roles")
    prefixes = _list_of(content["path_prefixes"], 1, PATH_PREFIXES_MAX)
    if not all(is_path_prefix(x) for x in prefixes):
        raise Untrusted("path_prefixes")
    forge = content["forge_api"]
    if _full(_FORGE_API_RE, forge) is None:
        raise Untrusted("forge_api")
    return TeamRecord(
        content["team"], frozenset(humans), dict(roles), _parse_repos(content["repos"]),
        tuple(prefixes), forge,
    )


def parse_team_event(event: object, admin: str, server_name: str, team_name: str) -> TeamRecord:
    """The team record state event: type, empty state key, sent by `admin`, valid content."""
    if (
        not isinstance(event, dict)
        or event.get("type") != EVENT_TEAM
        or event.get("state_key") != ""
        or event.get("sender") != admin
    ):
        raise Untrusted("team record event")
    return parse_team_record(event.get("content"), server_name, team_name)


def read_status(event: object, ctx: Context) -> int | None:
    """`until` (Unix ms) of a valid `agent_bus.status` event, or None when it is to be
    ignored (spec §8). Staleness is the caller's."""
    if not isinstance(event, dict) or event.get("type") != EVENT_STATUS:
        return None
    sender = event.get("sender")
    if event.get("state_key") != sender or sender_class(sender, ctx) not in ROLES:
        return None
    content = event.get("content")
    if not isinstance(content, dict) or set(content) != set(STATUS_KEYS):
        return None
    if content_size(content) > MAX_STATUS_BYTES:
        return None
    until = content["until"]
    if type(content["v"]) is not int or content["v"] != PROTOCOL_VERSION:
        return None
    if content["state"] != STATUS_LISTENING or type(until) is not int or until < 0:
        return None
    return until


def expected_power_levels(humans: Iterable[str]) -> dict:
    """The exact `m.room.power_levels` content of a trusted team room (spec §8)."""
    return {
        "users": {h: HUMAN_POWER for h in sorted(humans)},
        "users_default": 0, "events_default": 0, "state_default": 100,
        "invite": 100, "kick": 100, "ban": 100, "redact": 100,
        "notifications": {"room": HUMAN_POWER},
        "events": {"m.room.power_levels": 100, "m.room.tombstone": 150,
                   "m.room.redaction": 100, "m.reaction": HUMAN_POWER, "m.sticker": 100,
                   EVENT_STATUS: 0},
    }


def _canonical(value: object) -> str:
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def check_power_levels(content: object, humans: Iterable[str]) -> None:
    """Raise `Untrusted` unless `content` is exactly `expected_power_levels(humans)`,
    types included (`true` is not `1`, `100.0` is not `100`)."""
    if _canonical(content) != _canonical(expected_power_levels(humans)):
        raise Untrusted("power levels")


def build_ping(verb: str, to: Sequence[str], ref: str | None = None, re: str | None = None) -> dict:
    """A ping's full `m.room.message` content; absent fields omitted, `to` sorted.
    Validate it before sending."""
    obj: dict = {"v": PROTOCOL_VERSION, "verb": verb, "to": sorted(to)}
    for key, value in (("ref", ref), ("re", re)):
        if value is not None:
            obj[key] = value
    return {
        "msgtype": MSGTYPE_PING,
        "body": render(obj),
        "m.mentions": {"user_ids": list(obj["to"])},
        PING_KEY: obj,
    }


def render_text(obj: Mapping[str, object]) -> str:
    """The canonical `body` of an agent's text to humans (spec §7)."""
    return f"{RENDER_TAG} text -> {' '.join(sorted(obj['to']))}\n{obj['text']}"


def build_text(to: Sequence[str], text: str) -> dict:
    """An agent's text to humans as full `m.room.message` content, `to` sorted. Validate it
    before sending."""
    obj: dict = {"v": PROTOCOL_VERSION, "to": sorted(to), "text": text}
    return {
        "msgtype": MSGTYPE_TEXT,
        "body": render_text(obj),
        "m.mentions": {"user_ids": list(obj["to"])},
        TEXT_KEY: obj,
    }


def secret_shaped(text: str) -> str | None:
    """The name of the first `SECRET_PATTERNS` entry found in `text`, or None."""
    for name, regex in _SECRET_RES:
        if regex.search(text):
            return name
    return None


def _check_human_targets(to: list[str], ctx: Context) -> tuple[str, ...]:
    if not 1 <= len(to) <= HUMANS_MAX or len(set(to)) != len(to):
        raise Refusal("target")
    if not all(user in ctx.humans and is_human_user_id(user, ctx.server_name) for user in to):
        raise Refusal("target")
    return tuple(sorted(to))


def validate_text_content(content: object, ctx: Context) -> AgentText:
    """Check an agent's text to humans against spec §7 on send. Returns the parsed text or
    raises `Refusal` (`secret` among them). Agents receiving it ignore it: see `validate_event`."""
    if not isinstance(content, dict):
        raise Refusal("schema")
    _check_version_first(content.get(TEXT_KEY))
    if any(key in content for key in EDIT_KEYS):
        raise Refusal("edit")
    if set(content) != set(TEXT_CONTENT_KEYS) or content["msgtype"] != MSGTYPE_TEXT:
        raise Refusal("schema")
    obj = content[TEXT_KEY]
    if not isinstance(obj, dict) or set(obj) != set(TEXT_OBJECT_KEYS):
        raise Refusal("schema")
    to, text = obj["to"], obj["text"]
    if (
        type(obj["v"]) is not int
        or not isinstance(to, list)
        or not all(isinstance(x, str) for x in to)
        or not isinstance(text, str)
        or text == ""
    ):
        raise Refusal("schema")
    if len(text.encode("utf-8")) > MAX_AGENT_TEXT_BYTES:
        raise Refusal("size")
    targets = _check_human_targets(to, ctx)
    if secret_shaped(text) is not None:
        raise Refusal("secret")
    if content["m.mentions"] != {"user_ids": list(targets)}:
        raise Refusal("schema")
    if not isinstance(content["body"], str):
        raise Refusal("schema")
    if content["body"] != render_text(obj):
        raise Refusal("body")
    return AgentText(targets, text)


def check_text_sender(sender: str, ctx: Context) -> None:
    """Raise `Refusal("role")` unless `sender` is an agent holding a role (§7)."""
    if sender_class(sender, ctx) not in ROLES:
        raise Refusal("role")

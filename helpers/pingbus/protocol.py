"""The agent team bus wire protocol, version 1: constants and the pure validator.

Spec (single source of truth): docs/agent-bus-protocol.md. Every table there that this
module implements is held equal to the constants below by
tests/helpers/pingbus/test_protocol_doc.py.

Pure: no I/O, no clock, no network. Callers pass in what the team record and the member
config say (`Context`) and get back a parsed `Ping`, `HumanMessage`, `PeerText` (an
agent's text to this agent) or, on send, `AgentText`, or a `Refusal` carrying one of the closed set of reason codes. The same functions run on send
(`pingbus send`, `pingbus validate`) and on receive (the syncer, inbox reads), so a sender
that skips pingbus is still held to them by every receiver.

What is not decided here: `stale` and `rate` (limits), `forge-rate`, `unresolved` and `provenance` (the
forge check), and whether the member answering an `ack`/`nack` was addressed by the ping
it answers (the outbox). Room trust beyond the team record and the power levels (the
create event, invites) is the syncer's.
"""

from __future__ import annotations

import dataclasses
import json
import re
import unicodedata
from collections.abc import Container, Iterable, Mapping, Sequence

PROTOCOL_VERSION = 1
#: The team room's version: the homeserver's default and what `bootstrap` creates and checks.
ROOM_VERSION = "12"
#: The room topic of a team that has no description (spec §8): the admin sets it, and a
#: member reading the topic treats it as "no description".
ROOM_TOPIC = "Agent team bus: agents' pings and the team's humans' addressed messages"

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

ROLE_COORDINATOR = "coordinator"
ROLE_WORKER = "worker"
ROLES = (ROLE_COORDINATOR, ROLE_WORKER)
#: Role spellings a team record or a registry may still hold, and the role each reads as
#: (spec §8): a record written before the rename needs no migration.
LEGACY_ROLES = {"orchestrator": ROLE_COORDINATOR}
#: "May be sent by: anyone addressed by `re`": any role holder here; the addressing itself
#: is checked against the outbox, not offline.
SENDER_ADDRESSED = "addressed"
SENDER_HUMAN = "human"
STATUS_LISTENING = "listening"

RESERVED_LOCALPARTS = ("admin", "conduit")

#: Between `<seat>` and `<host>` in a handle. Probe H4 decides whether it stays `+`.
HANDLE_SEP = "+"
HANDLE_TYPES = ("podman", "lxc", "docker", "vm", "host")
#: The handle's `<repo>` and `<host>` parts; the registry's counter prefix is built from them.
HANDLE_REPO_PATTERN = r"[a-z0-9][a-z0-9_-]{0,47}"
HANDLE_HOST_PATTERN = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
#: A seat is a number (the registry's counter issues these) or a name; never `-`, `_`,
#: `.` or `@`, which separate it from its neighbours in a handle and from its team.
HANDLE_SEAT_PATTERN = r"[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}"


def handle_pattern(sep: str) -> str:
    """The agent-handle pattern with `sep` as the separator."""
    return (
        rf"(?P<repo>{HANDLE_REPO_PATTERN})\.(?P<seat>{HANDLE_SEAT_PATTERN})"
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
PING_KEYS = ("v", "verb", "to", "ref", "re", "branch")
#: Reference forms a ping's `branch` may accompany (spec §4, §6): the branch whose history
#: holds the commit. A pull request names its own head; an issue has no commit.
BRANCH_REF_FORMS = ("path", "commit")
PING_REQUIRED_KEYS = ("v", "verb", "to")
EDIT_KEYS = ("m.relates_to", "m.new_content")
MAX_CONTENT_BYTES = 4096
MAX_HUMAN_BODY_BYTES = 16384
MAX_STATUS_BYTES = 256
MAX_TO = 32

TEXT_CONTENT_KEYS = ("msgtype", "body", "m.mentions", TEXT_KEY)
#: The one optional key of an agent's text: the event it answers (spec §7).
TEXT_REPLY_KEY = "m.relates_to"
TEXT_OBJECT_KEYS = ("v", "to", "text")
MAX_AGENT_TEXT_BYTES = 4096
#: Addressees of one agent text: listed humans and role-holding agents together.
MAX_TEXT_TO = 16

#: Text an agent may not send (spec §7). Conservative: a false positive refuses
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

TEAM_KEYS = ("v", "team", "humans", "roles", "org", "exclude", "repos", "path_prefixes", "forge_api")
#: Keys a team record may leave out (spec §8); every other key is required.
TEAM_OPTIONAL_KEYS = ("org", "exclude", "path_prefixes")
FORGE_API_PATTERN = r"https://[^\s/]+(/\S*)?"
HUMANS_MAX = 16
ROLES_MAX = 64
REPOS_MIN = 0
REPOS_MAX = 32
#: The same bound as `repos`: both are lists of the team's repositories (spec §11).
EXCLUDE_MAX = REPOS_MAX
#: A team's `description` (what the team is for): at most this many characters. It reaches
#: members as the room topic, never as a key of the team record.
DESCRIPTION_MAX = 120
BRANCHES_MAX = 8
PATH_PREFIXES_MAX = 32
STATUS_KEYS = ("v", "state", "until")

HUMAN_POWER = 50

DROP_REASONS = (
    "version", "schema", "size", "edit", "sender", "role", "target", "verb", "ref",
    "allowlist", "excluded", "re", "body", "stale", "rate", "forge-rate", "unresolved", "provenance",
)
#: The ignore reason for an agent's text that does not name the receiving agent (spec §7,
#: §9): it is for others, so the agent skips it silently and it is never a drop.
IGNORE_AGENT_TEXT = "agent-text"
#: Codes that refuse a send and never name a receive drop.
SEND_REFUSALS = ("secret",)
#: Codes this module never produces: limits (`stale`, `rate`) and the forge check (`forge-rate`, `unresolved`, `provenance`).
REASONS_DECIDED_ELSEWHERE = frozenset({"stale", "rate", "forge-rate", "unresolved", "provenance"})

REQUIRED = "required"
OPTIONAL = "optional"
FORBIDDEN = "forbidden"

#: Where a reference's repository stands with the team (spec §11): listed in `repos`, of the
#: team's `org`, or neither (a public repository outside the team: data, never authority).
SOURCE_LISTED = "listed"
SOURCE_ORG = "org"
SOURCE_EXTERNAL = "external"
#: A ping's trust as the PING line shows it (spec §15): the receiver may act on a
#: `trusted` reference; an `external` one is content anyone could have written.
TRUST_TRUSTED = "trusted"
TRUST_EXTERNAL = "external"


@dataclasses.dataclass(frozen=True)
class VerbRule:
    ref: str
    ref_forms: frozenset[str]
    re: str
    ack_expected: bool
    senders: frozenset[str]
    #: Whether the `ref` may name a repository outside the team (spec §5, §11): only verbs
    #: that ask the receiver to read or that report a result, never to follow or run it.
    external: bool


def _rule(ref: str, forms: Sequence[str], re_: str, ack: bool, senders: Sequence[str],
          external: bool) -> VerbRule:
    return VerbRule(ref, frozenset(forms), re_, ack, frozenset(senders), external)


_COORD = (ROLE_COORDINATOR,)
_COORD_WORKER = (ROLE_COORDINATOR, ROLE_WORKER)

#: The closed verb set, in spec order.
VERBS: dict[str, VerbRule] = {
    "fetch": _rule(REQUIRED, ("path", "commit"), FORBIDDEN, True, _COORD, True),
    "sync": _rule(REQUIRED, ("path", "commit"), FORBIDDEN, True, _COORD, False),
    "review": _rule(REQUIRED, ("path", "commit", "pr"), FORBIDDEN, True, _COORD_WORKER, True),
    "run-qa": _rule(REQUIRED, ("commit", "pr"), FORBIDDEN, True, _COORD_WORKER, False),
    "halt": _rule(FORBIDDEN, (), FORBIDDEN, True, _COORD, False),
    "ack": _rule(FORBIDDEN, (), REQUIRED, False, (SENDER_ADDRESSED,), False),
    "nack": _rule(OPTIONAL, REF_FORMS, REQUIRED, False, (SENDER_ADDRESSED,), True),
    "done": _rule(REQUIRED, REF_FORMS, OPTIONAL, False, _COORD_WORKER, True),
    "blocked": _rule(REQUIRED, REF_FORMS, OPTIONAL, False, _COORD_WORKER, True),
}
#: Verbs whose `to` may name a listed human as well as role holders.
VERBS_TO_HUMANS = frozenset({"ack", "nack", "done", "blocked"})

_HANDLE_RE = re.compile(HANDLE_PATTERN)
_SEAT_RE = re.compile(HANDLE_SEAT_PATTERN)
_TEAM_RE = re.compile(TEAM_NAME_PATTERN)
_HUMAN_RE = re.compile(HUMAN_LOCALPART_PATTERN)
_ROOM_ID_RE = re.compile(ROOM_ID_PATTERN)
_EVENT_ID_RE = re.compile(EVENT_ID_PATTERN)
_BRANCH_RE = re.compile(BRANCH_PATTERN)
_PATH = rf"{SEG_PATTERN}(?:/{SEG_PATTERN}){{0,{PATH_MAX_SEGMENTS - 1}}}"
_PATH_RE = re.compile(_PATH)
_OWNER_RE = re.compile(OWNER_PATTERN)
_REPO_NAME_RE = re.compile(REPO_PATTERN)
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
    """A validation failure carrying one code from `DROP_REASONS` or `SEND_REFUSALS`, and on
    a send, optionally, a `detail` built only from the command line and the team record."""

    def __init__(self, reason: str, detail: str | None = None) -> None:
        if reason not in DROP_REASONS and reason not in SEND_REFUSALS:
            raise ValueError(f"not a drop reason code: {reason!r}")
        super().__init__(reason)
        self.reason = reason
        self.detail = detail


class Untrusted(ValueError):
    """The team room's record or power levels are not exactly what spec §8 allows."""


@dataclasses.dataclass(frozen=True)
class Handle:
    repo: str
    seat: str
    host: str
    type: str

    @property
    def n(self) -> int | None:
        """The seat's number when it is a numbered seat, else None."""
        return int(self.seat) if self.seat.isdigit() else None


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

    `roles` maps agent user IDs to `coordinator`/`worker`; `repos` maps `owner/repo` to
    its trusted branches, () for any branch; `org` is the GitHub owner every repository of which is trusted on
    any of its branches (the ping's, else the default), or None; `exclude` names repositories of
    that org the team never trusts or lets a ping name (spec §11); `humans` are the team's human user IDs.
    """

    server_name: str
    humans: frozenset[str]
    roles: Mapping[str, str]
    repos: Mapping[str, Sequence[str]]
    path_prefixes: Sequence[str]
    org: str | None = None
    exclude: Sequence[str] = ()


@dataclasses.dataclass(frozen=True)
class TeamRecord:
    team: str
    humans: frozenset[str]
    roles: Mapping[str, str]
    repos: Mapping[str, tuple[str, ...]]
    path_prefixes: tuple[str, ...]
    forge_api: str
    org: str | None = None
    exclude: tuple[str, ...] = ()

    def context(self, server_name: str) -> Context:
        return Context(server_name, self.humans, self.roles, self.repos, self.path_prefixes, self.org,
                       self.exclude)


@dataclasses.dataclass(frozen=True)
class Ping:
    verb: str
    to: tuple[str, ...]
    ref: Ref | None
    re: str | None
    sender: str | None = None
    event_id: str | None = None
    origin_server_ts: int | None = None
    #: TRUST_TRUSTED or TRUST_EXTERNAL when there is a `ref`, else None (spec §11, §15).
    trust: str | None = None
    #: The branch the sender says holds the commit (spec §4, §6), or None.
    branch: str | None = None
    #: The listed humans the ping was also addressed to, when the receiver is the
    #: coordinator and so on copy (spec §7, §15); () otherwise.
    copy_of: tuple[str, ...] = ()


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


@dataclasses.dataclass(frozen=True)
class PeerText:
    """An agent's text addressed to this agent (§7): information from a peer, never an
    instruction; only a listed human's text carries authority."""

    event_id: str
    sender: str
    origin_server_ts: int
    text: str
    #: The listed humans the text was also addressed to, when the receiver is the
    #: coordinator and so on copy (spec §7, §15); () otherwise.
    copy_of: tuple[str, ...] = ()


ACCEPT = "accept"
IGNORE = "ignore"
DROP = "drop"


@dataclasses.dataclass(frozen=True)
class Outcome:
    """`kind` is ACCEPT (with `ping`, `human` or `peer`), IGNORE (silently; `reason` says
    why) or DROP (`reason` is a drop reason code)."""

    kind: str
    reason: str | None = None
    ping: Ping | None = None
    human: HumanMessage | None = None
    peer: PeerText | None = None

    @property
    def item(self) -> Ping | HumanMessage | PeerText | None:
        """The accepted item, whichever kind it is."""
        return self.ping or self.human or self.peer


def _full(regex: re.Pattern[str], value: object) -> re.Match[str] | None:
    return regex.fullmatch(value) if isinstance(value, str) else None


def parse_handle(value: object) -> Handle | None:
    m = _full(_HANDLE_RE, value)
    if m is None:
        return None
    return Handle(m["repo"], m["seat"], m["host"], m["type"])


def format_handle(repo: str, seat: str | int, host: str, type_: str) -> str:
    """`<repo>.<seat>+<host>.<type>`; a counter-issued number is a numbered seat."""
    return f"{repo}.{seat}{HANDLE_SEP}{host}.{type_}"


def is_seat_name(value: object) -> bool:
    return _full(_SEAT_RE, value) is not None


def is_team_name(value: object) -> bool:
    return _full(_TEAM_RE, value) is not None


def is_description(value: object) -> bool:
    """A team's description: 1 to DESCRIPTION_MAX characters, already stripped, with no
    control or format character (so no newline, tab or bidi override)."""
    return (isinstance(value, str) and 1 <= len(value) <= DESCRIPTION_MAX and value == value.strip()
            and not any(unicodedata.category(c) in ("Cc", "Cf") for c in value))


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


def is_owner(value: object) -> bool:
    """A lowercase GitHub owner, as a team's `org` names it (spec §8, §11)."""
    return _full(_OWNER_RE, value) is not None


def is_excluded(ref: Ref, org: str | None, exclude: Sequence[str]) -> bool:
    """Whether the reference names a repository of the org that the team excludes (spec §11)."""
    return org is not None and ref.owner == org and ref.repo in exclude


def ref_source(ref: Ref, org: str | None, repos: Mapping[str, Sequence[str]]) -> str:
    """SOURCE_LISTED when `repos` lists the reference's repository (its listed branches are
    the trusted ones, in the org or not), SOURCE_ORG when the org owns it, else
    SOURCE_EXTERNAL (spec §11)."""
    if ref.repo_full in repos:
        return SOURCE_LISTED
    if org is not None and ref.owner == org:
        return SOURCE_ORG
    return SOURCE_EXTERNAL


def trusts_any_branch(ref: Ref, org: str | None, repos: Mapping[str, Sequence[str]]) -> bool:
    """Whether any branch of the reference's repository is trusted: an org repository not
    listed, or one listed with no `branches` (spec §11). Such a reference is checked against
    the ping's branch, else the repository's default branch."""
    source = ref_source(ref, org, repos)
    return source == SOURCE_ORG or (source == SOURCE_LISTED and not repos[ref.repo_full])


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


def normalise_role(role: object) -> object:
    """The role a legacy spelling reads as (`orchestrator` is `coordinator`); anything
    else unchanged, for the caller to judge against `ROLES`."""
    return LEGACY_ROLES.get(role, role) if isinstance(role, str) else role


def coordinators(roles: Mapping[str, str]) -> list[str]:
    """The user IDs holding the coordinator role, sorted: one in a team the writers keep
    in order, every one in a record that somehow lists more."""
    return sorted(user for user, role in roles.items() if role == ROLE_COORDINATOR)


def with_coordinator_copy(to: Sequence[str], sender: str, ctx: Context) -> list[str]:
    """`to` with the team's coordinator added when a worker is writing to a human (spec §7):
    the coordinator is copied on what workers tell humans without the worker remembering.
    Unchanged when the sender is not a worker, no human is named, or the coordinator already
    is (or there is none)."""
    if ctx.roles.get(sender) != ROLE_WORKER or not any(user in ctx.humans for user in to):
        return list(to)
    return [*to, *(user for user in coordinators(ctx.roles) if user not in to)]


def _copy_of(to: Iterable[str], self_user_id: str, ctx: Context) -> tuple[str, ...]:
    """The listed humans `to` names, when the receiver is the coordinator: it is on copy."""
    if ctx.roles.get(self_user_id) != ROLE_COORDINATOR:
        return ()
    return tuple(sorted(user for user in to if user in ctx.humans))


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
    text = f"{RENDER_TAG} {ping['verb']} {ref if ref is not None else '-'}"
    if ping.get("branch") is not None:
        text += f" on {ping['branch']}"
    text += f" -> {to}"
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
    if not all(addressable(user, verb, ctx) for user in to):
        raise Refusal("target")
    return tuple(sorted(to))


def addressable(user: str, verb: str | None, ctx: Context) -> bool:
    """Whether `user` may be in a `to`: an agent holding a role; or a listed human, for an
    agent text (`verb` None) or a verb in `VERBS_TO_HUMANS`. Never `admin` or the server user."""
    if ctx.roles.get(user) in ROLES and is_agent_user_id(user, ctx.server_name):
        return True
    return ((verb is None or verb in VERBS_TO_HUMANS) and user in ctx.humans
            and is_human_user_id(user, ctx.server_name))


def check_ref_form(value: object, rule: VerbRule) -> Ref | None:
    """The part of the `ref` check that needs no team record: present when the verb
    requires it, absent when it forbids it, and of a form the verb allows."""
    if value is None:
        if rule.ref == REQUIRED:
            raise Refusal("ref")
        return None
    if rule.ref == FORBIDDEN:
        raise Refusal("ref")
    ref = parse_ref(value)
    if ref is None or ref.form not in rule.ref_forms:
        raise Refusal("ref")
    return ref


def _check_ref(value: object, rule: VerbRule, ctx: Context) -> tuple[Ref | None, str | None]:
    """The reference and its trust (spec §11): a listed or org repository's is trusted and,
    when the team sets `path_prefixes`, held to them (none set: any path); any other repository's is external, accepted only for a verb
    that allows it, and never held to the prefixes, which name the team's own documents."""
    ref = check_ref_form(value, rule)
    if ref is None:
        return None, None
    if is_excluded(ref, ctx.org, ctx.exclude):
        raise Refusal("excluded")
    if ref_source(ref, ctx.org, ctx.repos) == SOURCE_EXTERNAL:
        if not rule.external:
            raise Refusal("allowlist")
        return ref, TRUST_EXTERNAL
    if ref.form == "path" and ctx.path_prefixes and not path_allowed(ref.path, ctx.path_prefixes):
        raise Refusal("allowlist")
    return ref, TRUST_TRUSTED


def check_branch(value: object, ref: Ref | None) -> str | None:
    """A ping's `branch` (spec §4): absent, or a branch name beside a `path` or `commit`
    reference; anything else is `ref`."""
    if value is None:
        return None
    if ref is None or ref.form not in BRANCH_REF_FORMS or not is_branch_name(value):
        raise Refusal("ref")
    return value


def check_re(value: object, rule: VerbRule) -> str | None:
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
    ref, trust = _check_ref(obj.get("ref"), rule, ctx)
    branch = check_branch(obj.get("branch"), ref)
    re_ = check_re(obj.get("re"), rule)
    if content["m.mentions"] != {"user_ids": list(to)}:
        raise Refusal("schema")
    if not isinstance(content["body"], str):
        raise Refusal("schema")
    if content["body"] != render(obj):
        raise Refusal("body")
    return Ping(obj["verb"], to, ref, re_, trust=trust, branch=branch)


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


def _names(content: Mapping[str, object], self_user_id: str) -> bool:
    """Whether an agent text's `to` or its mentions name this agent, read before it is
    validated: text for others is skipped unread, however it is formed."""
    obj = content.get(TEXT_KEY)
    to = obj.get("to") if isinstance(obj, dict) else None
    mentions = content.get("m.mentions")
    users = mentions.get("user_ids") if isinstance(mentions, dict) else None
    return any(isinstance(x, list) and self_user_id in x for x in (to, users))


def _agent_text_outcome(
    content: Mapping[str, object], event_id: str, sender: str, ts: int, ctx: Context,
    self_user_id: str, human_text: bool,
) -> Outcome:
    """Spec §9 ping path, step 04p: an agent's text. Not naming this agent: ignore
    (`agent-text`). Naming it: the send rules but the secret shapes, then a peer text."""
    if not _names(content, self_user_id):
        return Outcome(IGNORE, IGNORE_AGENT_TEXT)
    if not human_text:
        return Outcome(DROP, "sender")
    try:
        text = validate_text_content(content, ctx, check_secret=False)
    except Refusal as refusal:
        return Outcome(DROP, refusal.reason)
    # Valid text has its mentions equal to its `to`, so either naming this agent puts it in `to`.
    return Outcome(ACCEPT, peer=PeerText(event_id, sender, ts, text.text,
                                         _copy_of(text.to, self_user_id, ctx)))


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
        # Beside a ping it is neither, and the sender is at fault.
        if PING_KEY in content:
            return Outcome(DROP, "schema")
        return _agent_text_outcome(content, event_id, sender, ts, ctx, self_user_id, human_text)
    try:
        ping = validate_content(content, ctx)
        check_role(ping.verb, sender, ctx)
    except Refusal as refusal:
        return Outcome(DROP, refusal.reason)
    if self_user_id not in ping.to:
        return Outcome(IGNORE, "not-addressed")
    return Outcome(ACCEPT, ping=dataclasses.replace(
        ping, sender=sender, event_id=event_id, origin_server_ts=ts,
        copy_of=_copy_of(ping.to, self_user_id, ctx)))


def _list_of(value: object, low: int, high: int) -> list:
    if not isinstance(value, list) or not low <= len(value) <= high or len(value) != len(
        {json.dumps(x, sort_keys=True) for x in value}
    ):
        raise Untrusted("list size or duplicates")
    return value


def _parse_repos(value: object) -> dict[str, tuple[str, ...]]:
    """`repos` as {OWNER/REPO: its branches}; an entry with no `branches` maps to (), any
    branch (spec §11)."""
    repos: dict[str, tuple[str, ...]] = {}
    for item in _list_of(value, REPOS_MIN, REPOS_MAX):
        if not isinstance(item, dict) or set(item) not in ({"repo"}, {"repo", "branches"}):
            raise Untrusted("repos entry")
        name = item["repo"]
        if not is_owner_repo(name) or name in repos:
            raise Untrusted("repos entry")
        if "branches" not in item:
            repos[name] = ()
            continue
        branches = _list_of(item["branches"], 1, BRANCHES_MAX)
        if not all(is_branch_name(b) for b in branches):
            raise Untrusted("branch name")
        repos[name] = tuple(branches)
    return repos


def is_repo_name(value: object) -> bool:
    """A lowercase repository name without its owner, as a team's `exclude` names one."""
    m = _full(_REPO_NAME_RE, value)
    return m is not None and _repo_name_ok(value)


def _parse_exclude(value: object, org: str | None, repos: Mapping[str, Sequence[str]]) -> tuple[str, ...]:
    """`exclude` as the org's repository names (spec §11): needs an `org`, 1 to EXCLUDE_MAX
    distinct names, none also listed in `repos`, which would trust and refuse it at once."""
    names = _list_of(value, 1, EXCLUDE_MAX)
    if org is None or not all(is_repo_name(n) for n in names):
        raise Untrusted("exclude")
    if any(f"{org}/{n}" in repos for n in names):
        raise Untrusted("exclude repository is also listed")
    return tuple(names)


def parse_team_record(content: object, server_name: str, team_name: str) -> TeamRecord:
    """`agent_bus.team` content (spec §8); raises `Untrusted` on any deviation."""
    required = set(TEAM_KEYS) - set(TEAM_OPTIONAL_KEYS)
    if not isinstance(content, dict) or not required <= set(content) <= set(TEAM_KEYS):
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
    roles = {user: normalise_role(role) for user, role in roles.items()}
    for user, role in roles.items():
        if role not in ROLES or not is_agent_user_id(user, server_name):
            raise Untrusted("roles")
    prefixes = _list_of(content["path_prefixes"], 1, PATH_PREFIXES_MAX) if "path_prefixes" in content else []
    if not all(is_path_prefix(x) for x in prefixes):
        raise Untrusted("path_prefixes")
    forge = content["forge_api"]
    if _full(_FORGE_API_RE, forge) is None:
        raise Untrusted("forge_api")
    org = content.get("org")
    if "org" in content and not is_owner(org):
        raise Untrusted("org")
    repos = _parse_repos(content["repos"])
    if org is None and not repos:
        raise Untrusted("neither an org nor a repository is trusted")
    exclude = _parse_exclude(content["exclude"], org, repos) if "exclude" in content else ()
    return TeamRecord(
        content["team"], frozenset(humans), dict(roles), repos, tuple(prefixes), forge, org, exclude,
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


def build_ping(verb: str, to: Sequence[str], ref: str | None = None, re: str | None = None,
               branch: str | None = None) -> dict:
    """A ping's full `m.room.message` content; absent fields omitted, `to` sorted.
    Validate it before sending."""
    obj: dict = {"v": PROTOCOL_VERSION, "verb": verb, "to": sorted(to)}
    for key, value in (("ref", ref), ("re", re), ("branch", branch)):
        if value is not None:
            obj[key] = value
    return {
        "msgtype": MSGTYPE_PING,
        "body": render(obj),
        "m.mentions": {"user_ids": list(obj["to"])},
        PING_KEY: obj,
    }


def render_text(obj: Mapping[str, object]) -> str:
    """The canonical `body` of an agent's text (spec §7)."""
    return f"{RENDER_TAG} text -> {' '.join(sorted(obj['to']))}\n{obj['text']}"


def build_text(to: Sequence[str], text: str, re: str | None = None) -> dict:
    """An agent's text as full `m.room.message` content, `to` sorted; with `re`, a reply to
    that event (`m.in_reply_to`), which Element shows threaded under it. Validate it before
    sending. ValueError for an `re` that is not an event ID."""
    obj: dict = {"v": PROTOCOL_VERSION, "to": sorted(to), "text": text}
    content = {
        "msgtype": MSGTYPE_TEXT,
        "body": render_text(obj),
        "m.mentions": {"user_ids": list(obj["to"])},
        TEXT_KEY: obj,
    }
    if re is not None:
        if not is_event_id(re):
            raise ValueError("re is not an event ID")
        content[TEXT_REPLY_KEY] = {"m.in_reply_to": {"event_id": re}}
    return content


def _check_text_reply(content: Mapping[str, object]) -> None:
    """An agent's text may answer one event: `m.relates_to` is absent or exactly
    `{"m.in_reply_to": {"event_id": <event ID>}}`; an edit is `edit`, any other shape `schema`."""
    if "m.new_content" in content:
        raise Refusal("edit")
    if TEXT_REPLY_KEY not in content:
        return
    relates = content[TEXT_REPLY_KEY]
    if isinstance(relates, dict) and relates.get("rel_type") == "m.replace":
        raise Refusal("edit")
    if (not isinstance(relates, dict) or set(relates) != {"m.in_reply_to"}
            or not isinstance(relates["m.in_reply_to"], dict)
            or set(relates["m.in_reply_to"]) != {"event_id"}
            or not is_event_id(relates["m.in_reply_to"]["event_id"])):
        raise Refusal("schema")


def secret_shaped(text: str) -> str | None:
    """The name of the first `SECRET_PATTERNS` entry found in `text`, or None."""
    for name, regex in _SECRET_RES:
        if regex.search(text):
            return name
    return None


def _check_text_targets(to: list[str], ctx: Context) -> tuple[str, ...]:
    """Listed humans and role-holding agents; never `admin`, the server user or anyone else."""
    if not 1 <= len(to) <= MAX_TEXT_TO or len(set(to)) != len(to):
        raise Refusal("target")
    if not all(addressable(user, None, ctx) for user in to):
        raise Refusal("target")
    return tuple(sorted(to))


def validate_text_content(content: object, ctx: Context, *, check_secret: bool = True) -> AgentText:
    """Check an agent's text against spec §7. Returns the parsed text or raises `Refusal`.
    On send `secret` is among the refusals; a receiver passes `check_secret=False`, because
    `secret` is a send-only code (see `validate_event`)."""
    if not isinstance(content, dict):
        raise Refusal("schema")
    _check_version_first(content.get(TEXT_KEY))
    _check_text_reply(content)
    if set(content) - {TEXT_REPLY_KEY} != set(TEXT_CONTENT_KEYS) or content["msgtype"] != MSGTYPE_TEXT:
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
    targets = _check_text_targets(to, ctx)
    if check_secret and secret_shaped(text) is not None:
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

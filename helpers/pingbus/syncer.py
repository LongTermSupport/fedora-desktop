"""The sync engine: one team's `/sync`, room trust, and both receive pipelines.

Spec: docs/agent-bus-protocol.md §8 (the trusted room, the invite rule, status), §9 (on
receive: the first sync records only `next_batch`, only the trusted team room is
considered, the human and ping paths in order, the token saved after the batch's inbox
writes are durable), §10 (stale, the receive flood limit), §6 (the forge check on receive)
and §14 (exit 10). Plan 00161's DESIGN.md section 4 ("Joining") and section 12 row U10.

One `Syncer` serves one team for one process: the receive flood count lives in it (§10),
and it re-verifies the room when it starts, after a join, and whenever a batch carries a
change to `m.room.create`, `m.room.power_levels` or `agent_bus.team`. A room that fails
verification is not trusted: `team.json` is replaced by `untrusted.json` holding the
reason (for `status`), nothing of that batch is stored, the
sync token stays where it was, and `RoomUntrusted` (exit 10) is raised; a missing room or
team record, which the client reports as `matrix.NotFound` (exit 7), and a room this
account has not joined (`matrix.Forbidden`) are the same. Other client errors propagate
with their own exit codes.

Memberships and the statuses §8 lets it read are kept in `room.json` (`inbox`), so an
offline `status` can list unexpected members and stale statuses (Plan 00161 U12).

`history` (§13, U33) reads the room backwards with `/messages` through the same checks and
stores nothing: the room, not the local cache, is a seat's record.

Validation is `protocol`'s; storage is `inbox`'s; the forge check is `forge`'s. Diagnostics
go to `log` (stderr by default) and name a room by its ID only, never anything an event
carried.
"""

from __future__ import annotations

import collections
import dataclasses
import sys
import time
from collections.abc import Callable, Mapping, Sequence

from helpers.pingbus import config, forge, inbox, limits, matrix, protocol

EXIT_UNTRUSTED = 10

CREATE = "m.room.create"
POWER_LEVELS = "m.room.power_levels"
MEMBER = "m.room.member"
#: A change to any of these re-verifies the room (§8).
TRUST_TYPES = (CREATE, POWER_LEVELS, protocol.EVENT_TEAM)
#: The filter probe H4 recorded against Tuwunel 1.9.3 (fixtures 040, 044, 071).
STATE_TYPES = (CREATE, POWER_LEVELS, MEMBER, protocol.EVENT_TEAM, protocol.EVENT_STATUS)
TIMELINE_TYPES = (protocol.EVENT_MESSAGE, MEMBER, POWER_LEVELS, protocol.EVENT_TEAM, protocol.EVENT_STATUS)
TIMELINE_LIMIT = 50
#: A limited timeline's gap is read forward with `/messages` (fixture 072).
GAP_FILTER = {"types": [protocol.EVENT_MESSAGE]}
GAP_PAGE_LIMIT = 100
#: A gap longer than this many pages is a misbehaving server, not a backlog: exit 7.
GAP_MAX_PAGES = 20
#: `history` reads the room backwards in pages of this many messages, at most this many
#: pages: the newest 5000 room messages bound one team's scan (§13).
HISTORY_PAGE_LIMIT = 100
HISTORY_MAX_PAGES = 50
HISTORY_DEFAULT_ITEMS = 50
HISTORY_MAX_ITEMS = 500
HISTORY_IN = "in"
HISTORY_OUT = "out"


class RoomUntrusted(protocol.Untrusted):
    """The team room is not trusted (§8), or not joined: exit 10."""

    exit_code = EXIT_UNTRUSTED


@dataclasses.dataclass(frozen=True)
class Batch:
    """One sync's result: `accepted` event IDs (in room order) written to the inbox, of
    which `stored` were new; drop counts by reason code (never an ignore)."""

    first: bool
    stored: int
    accepted: tuple[str, ...]
    drops: Mapping[str, int]


@dataclasses.dataclass(frozen=True)
class HistoryItem:
    """One item of a seat's history (§13 `history`): a ping received (`in`, `ping`) or
    sent (`out`, `ping`), a human message received (`in`, `human`), or an agent text this
    member sent (`out`, `sent_event_id`; its text is never kept)."""

    direction: str
    origin_server_ts: int
    ping: protocol.Ping | None = None
    human: protocol.HumanMessage | None = None
    sent_event_id: str | None = None


@dataclasses.dataclass(frozen=True)
class History:
    """`items` newest first; drop counts by reason code; `complete` is False when the scan
    stopped at `HISTORY_MAX_PAGES` with older messages left unread."""

    items: tuple[HistoryItem, ...]
    drops: Mapping[str, int]
    complete: bool


def _envelope_ok(event: Mapping[str, object]) -> bool:
    """§9 receive steps 1 and 3 for an event this member sent (step 2 ignores those)."""
    unsigned = event.get("unsigned")
    ts = event.get("origin_server_ts")
    return (protocol.is_event_id(event.get("event_id"))
            and "state_key" not in event
            and not (isinstance(unsigned, dict) and "redacted_because" in unsigned)
            and isinstance(event.get("content"), dict)
            and type(ts) is int and ts >= 0)


def sync_filter(room_id: str, timeline_limit: int, *, lazy_members: bool = True) -> dict:
    """The inline `/sync` filter: the team room only, the state and timeline types the
    syncer reads, nothing else (no presence, account data or ephemeral events). The first
    sync and the one after a join load every member (`lazy_members=False`), so `status`
    knows who had joined before this member; later syncs see joins in the timeline."""
    return {
        "presence": {"types": []},
        "account_data": {"types": []},
        "room": {
            "rooms": [room_id],
            "ephemeral": {"types": []},
            "account_data": {"types": []},
            "state": {"types": list(STATE_TYPES), "lazy_load_members": lazy_members},
            "timeline": {"types": list(TIMELINE_TYPES), "limit": timeline_limit},
        },
    }


def invite_acceptable(room_id: object, invite: object, member: config.Member) -> bool:
    """§8: accept only an invite to the bundle's `room` whose inviter and stripped
    `m.room.create` sender are both the bundle's `admin`."""
    if room_id != member.room or not isinstance(invite, dict):
        return False
    invite_state = invite.get("invite_state")
    events = invite_state.get("events") if isinstance(invite_state, dict) else None
    if not isinstance(events, list):
        return False
    create_sender = inviter = None
    for event in events:
        if not isinstance(event, dict):
            continue
        content = event.get("content")
        if event.get("type") == CREATE and event.get("state_key") == "":
            create_sender = event.get("sender")
        elif (
            event.get("type") == MEMBER
            and event.get("state_key") == member.user_id
            and isinstance(content, dict)
            and content.get("membership") == "invite"
        ):
            inviter = event.get("sender")
    return create_sender == member.admin and inviter == member.admin


def forge_factory(member: config.Member, environ: Mapping[str, str],
                  **kwargs: object) -> Callable[[protocol.TeamRecord], forge.Forge]:
    """The default `forge_for`: a forge client for the record's `forge_api`, with the
    credential §6 names and the member's `forge-cache.json`."""

    def make(record: protocol.TeamRecord) -> forge.Forge:
        credential = forge.resolve_credential(environ, record.forge_api)
        inbox.TeamState.for_member(member).ensure_dirs()
        cache = forge.ForgeCache.load(member.state_dir / forge.CACHE_FILE)
        return forge.Forge.for_record(record, credential, cache=cache, **kwargs)

    return make


def _now_ms() -> int:
    return int(time.time() * 1000)


def _stderr(text: str) -> None:
    sys.stderr.write(text + "\n")


def _malformed(detail: str) -> matrix.Unreachable:
    return matrix.Unreachable("GET /sync", detail=detail)


def _next_batch(response: Mapping[str, object]) -> str:
    token = response.get("next_batch")
    if not config.is_printable_token(token) or len(token) > inbox.SYNC_TOKEN_MAX:
        raise _malformed("the answer has no valid next_batch")
    return token


def _section(parent: object, key: str) -> dict:
    """`parent[key]` as an object; absent is empty; anything else is a malformed answer."""
    value = parent.get(key, {}) if isinstance(parent, dict) else None
    if not isinstance(value, dict):
        raise _malformed(f"{key} is not an object")
    return value


def _events(parent: dict, key: str) -> list:
    events = _section(parent, key).get("events", [])
    if not isinstance(events, list):
        raise _malformed(f"{key}.events is not a list")
    return events


def _type_of(event: object) -> object:
    return event.get("type") if isinstance(event, dict) else None


def _is_answer(ping: protocol.Ping | None) -> bool:
    """An `ack` or `nack` naming the ping it answers."""
    return ping is not None and ping.verb in ("ack", "nack") and ping.re is not None


class Syncer:
    """One team's sync engine. `forge_for(record)` gives the forge client for a verified
    record (`forge_factory` builds the real one); `clock_ms` and `log` are injected."""

    def __init__(
        self,
        member: config.Member,
        client: matrix.Client,
        state: inbox.TeamState,
        *,
        forge_for: Callable[[protocol.TeamRecord], forge.Forge],
        clock_ms: Callable[[], int] = _now_ms,
        log: Callable[[str], None] = _stderr,
    ) -> None:
        self.member = member
        self.client = client
        self.state = state
        self._forge_for = forge_for
        self._clock_ms = clock_ms
        self._log = log
        self._flood = limits.ReceiveFlood(member.limits)
        self._forge: forge.Forge | None = None
        #: The verified team record; None until verified, and again once trust is lost.
        self.record: protocol.TeamRecord | None = None
        #: Joined and invited members, and members' `listening` statuses (`until`, Unix
        #: ms) as §8 allows them to be read; kept in `room.json` for an offline `status`.
        self.members, self.statuses = state.room_view()

    def _say(self, text: str) -> None:
        self._log(f"team {self.member.team}: {text}")

    # ── room trust ───────────────────────────────────────────────────────────────────────

    def verify_room(self) -> protocol.TeamRecord:
        """Check the room as §8 says; save the record to `team.json`, or lose trust."""
        try:
            record, content = self._read_trusted_state()
        except protocol.Untrusted as exc:
            raise self._lose_trust(str(exc)) from None
        except (matrix.NotFound, matrix.Forbidden):
            raise self._lose_trust("not joined, or the room or its team record is missing") from None
        self.state.save_team_record(content)
        if self.record is None or self.record.forge_api != record.forge_api:
            self._forge = None
        self.record = record
        # §8: a status counts only from a role holder, so a member dropped from `roles` loses it.
        kept = {uid: until for uid, until in self.statuses.items() if uid in record.roles}
        if kept != self.statuses:
            self.statuses = kept
            self._save_room_view()
        return record

    def _read_trusted_state(self) -> tuple[protocol.TeamRecord, dict]:
        member, room = self.member, self.member.room
        create = self.client.get_state(room, CREATE, "", as_event=True)
        content = create.get("content")
        if (
            create.get("type") != CREATE
            or create.get("sender") != member.admin
            or create.get("event_id") != "$" + room[1:]
            or not isinstance(content, dict)
            or content.get("room_version") != protocol.ROOM_VERSION
            or "additional_creators" in content
        ):
            raise protocol.Untrusted("the room's create event")
        team_event = self.client.get_state(room, protocol.EVENT_TEAM, "", as_event=True)
        record = protocol.parse_team_event(team_event, member.admin, member.server_name, member.team)
        if record.roles.get(member.user_id) not in protocol.ROLES:
            raise protocol.Untrusted("the team record gives this member no role")
        protocol.check_power_levels(self.client.get_state(room, POWER_LEVELS, ""), record.humans)
        return record, team_event["content"]

    def _lose_trust(self, reason: str) -> RoomUntrusted:
        self.record = None
        self._forge = None
        if self.statuses:
            self.statuses = {}
            self._save_room_view()
        self.state.mark_untrusted(reason)
        self._say(f"the team room is not trusted: {reason}")
        return RoomUntrusted(reason)

    # ── syncing ──────────────────────────────────────────────────────────────────────────

    def sync_once(self, timeout_ms: int = 0) -> Batch:
        """One `/sync` (long-polling up to `timeout_ms`), its items stored, its token saved."""
        since = self.state.sync_token()
        if since is None:
            return self._first_sync()
        if self.record is None:
            self.verify_room()
        response = self.client.sync(since=since, filter=sync_filter(self.member.room, TIMELINE_LIMIT),
                                    timeout_ms=timeout_ms)
        next_batch = _next_batch(response)
        rooms = _section(response, "rooms")
        if self._take_invites(rooms):
            return self._after_join(next_batch, first=False)
        entry = _section(_section(rooms, "join"), self.member.room)
        state_events = _events(entry, "state")
        timeline = _section(entry, "timeline")
        events = _events(entry, "timeline")
        limited = timeline.get("limited") is True
        if limited or any(_type_of(e) in TRUST_TYPES for e in state_events + events):
            self.verify_room()
        if limited:
            events = self._fill_gap(since, timeline.get("prev_batch")) + events
        self._read_room_state(state_events + events)
        return self._receive(events, next_batch)

    def _first_sync(self) -> Batch:
        """§9: no stored token, so `timeline.limit: 0` and only `next_batch` is kept."""
        response = self.client.sync(filter=sync_filter(self.member.room, 0, lazy_members=False),
                                    timeout_ms=0)
        next_batch = _next_batch(response)
        rooms = _section(response, "rooms")
        if self._take_invites(rooms):
            return self._after_join(next_batch, first=True)
        self.verify_room()
        self._read_room_state(_events(_section(_section(rooms, "join"), self.member.room), "state"))
        self.state.commit_batch([], next_batch)
        return Batch(True, 0, (), {})

    def _take_invites(self, rooms: dict) -> bool:
        """Join the one acceptable invite; reject every other (§8), logged by room ID only."""
        joined = False
        for room_id, invite in _section(rooms, "invite").items():
            if invite_acceptable(room_id, invite, self.member):
                self.client.join(room_id)
                joined = True
            elif protocol.is_room_id(room_id):
                self.client.leave(room_id)
                self._say(f"rejected an invite to room {room_id}")
            else:
                self._say("ignored an invite with a malformed room ID")
        return joined

    def _after_join(self, since: str, *, first: bool) -> Batch:
        """Verify the room just joined, then move the token past the join with a
        `limit: 0` sync, so the room's history is never processed."""
        self.verify_room()
        response = self.client.sync(since=since, filter=sync_filter(self.member.room, 0, lazy_members=False),
                                    timeout_ms=0)
        next_batch = _next_batch(response)
        entry = _section(_section(_section(response, "rooms"), "join"), self.member.room)
        self._read_room_state(_events(entry, "state"))
        self.state.commit_batch([], next_batch)
        return Batch(first, 0, (), {})

    def _fill_gap(self, since: str, prev_batch: object) -> list:
        """The messages a limited timeline left out, oldest first, read forward from the
        previous sync's token to the timeline's `prev_batch`."""
        if not config.is_printable_token(prev_batch):
            raise _malformed("a limited timeline has no prev_batch")
        found: list = []
        cursor = since
        for _ in range(GAP_MAX_PAGES):
            page = self.client.messages(self.member.room, from_token=cursor, to_token=prev_batch,
                                        direction="f", limit=GAP_PAGE_LIMIT, filter=GAP_FILTER)
            chunk = page.get("chunk")
            if not isinstance(chunk, list):
                raise matrix.Unreachable("GET /messages", detail="the answer has no chunk list")
            found.extend(chunk)
            end = page.get("end")
            if not chunk or not isinstance(end, str) or end == cursor:
                return found
            cursor = end
        raise matrix.Unreachable("GET /messages", detail=f"the gap runs past {GAP_MAX_PAGES} pages")

    def _read_room_state(self, events: Sequence[object]) -> None:
        """Memberships and statuses, saved to `room.json` when either changed. §8: a status
        counts only under its sender's own user ID, from a role holder, in the exact shape;
        a member's own key holding anything else clears its status."""
        ctx = self.record.context(self.member.server_name)
        before = (dict(self.members), dict(self.statuses))
        for event in events:
            kind = _type_of(event)
            if kind == MEMBER:
                self._read_membership(event)
            elif kind == protocol.EVENT_STATUS:
                until = protocol.read_status(event, ctx)
                sender = event.get("sender")
                if until is not None:
                    self.statuses[sender] = until
                elif isinstance(sender, str) and event.get("state_key") == sender:
                    self.statuses.pop(sender, None)
        if (self.members, self.statuses) != before:
            self._save_room_view()

    def _read_membership(self, event: dict) -> None:
        user_id, content = event.get("state_key"), event.get("content")
        if not inbox.is_user_id_text(user_id) or not isinstance(content, dict):
            self._say("ignored a membership event with a malformed user ID")
            return
        if content.get("membership") in inbox.ROOM_MEMBERSHIPS:
            self.members[user_id] = content["membership"]
        else:
            self.members.pop(user_id, None)

    def _save_room_view(self) -> None:
        self.state.save_room_view(self.members, self.statuses)

    # ── the receive pipelines ────────────────────────────────────────────────────────────

    def _receive(self, events: Sequence[object], next_batch: str) -> Batch:
        record, member = self.record, self.member
        ctx = record.context(member.server_name)
        seen = set(self.state.known_event_ids())
        now = self._clock_ms()
        accepted: list[dict] = []
        answers: list[protocol.Ping] = []
        drops: collections.Counter[str] = collections.Counter()
        for event in events:
            outcome = protocol.validate_event(event, ctx, member.user_id, seen, human_text=member.human_text)
            if outcome.kind == protocol.IGNORE and outcome.reason == "type":
                continue
            event_id = event.get("event_id") if isinstance(event, dict) else None
            if protocol.is_event_id(event_id):
                seen.add(event_id)
            if outcome.kind == protocol.ACCEPT:
                stale, reason = self._local_checks(outcome, record, now)
                if reason is None and _is_answer(outcome.ping):
                    answers.append(outcome.ping)
                reason = "stale" if stale else reason
                outcome = protocol.Outcome(protocol.DROP, reason) if reason else outcome
            if outcome.kind == protocol.DROP:
                sender = event.get("sender") if isinstance(event, dict) else None
                self.state.log_drop(member.team, event_id, sender, outcome.reason, now)
                drops[outcome.reason] += 1
            elif outcome.kind == protocol.ACCEPT:
                accepted.append(event)
        if answers:
            with self.state.outbox() as box:
                for answer in answers:
                    box.record_answer(answer.re, answer.sender, answer.origin_server_ts, member.limits)
        stored = self.state.commit_batch(accepted, next_batch)
        return Batch(False, stored, tuple(e["event_id"] for e in accepted), dict(drops))

    def _local_checks(self, outcome: protocol.Outcome, record: protocol.TeamRecord,
                      now: int) -> tuple[bool, str | None]:
        """The steps that need a clock or the network: 10h-11h, 08p-10p. Whether the item is
        stale, and the drop reason of the other steps or None. A read-late answer still counts
        if sent by the ping's deadline (§9 08p, §10), so it faces the other steps; any other
        stale item stops there, spending no flood budget and no forge call."""
        lim = self.member.limits
        if outcome.human is not None:
            message = outcome.human
            if limits.human_stale(message.origin_server_ts, now, lim):
                return True, None
            return False, None if self._flood.admit(message.sender, now) else "rate"
        ping = outcome.ping
        stale = limits.ping_stale(ping.origin_server_ts, ping.verb, now, lim)
        if stale and not _is_answer(ping):
            return True, None
        if not self._flood.admit(ping.sender, now):
            return stale, "rate"
        return stale, self._forge_check(ping, record)

    def _forge_check(self, ping: protocol.Ping, record: protocol.TeamRecord) -> str | None:
        """§6 for a ping with a reference: its drop reason, or None when it resolves."""
        if ping.ref is None:
            return None
        if self._forge is None:
            self._forge = self._forge_for(record)
        try:
            forge.check_ping(ping, record, self._forge)
        except forge.ForgeError as exc:
            return exc.drop_reason
        return None

    # ── history ──────────────────────────────────────────────────────────────────────────

    def history(self, limit: int) -> History:
        """This member's past items, newest first, at most `limit` (§13 `history`), read
        from the room itself, so fresh local state loses nothing. Each received item goes
        through the offline receive steps and the forge check; an item this member sent
        goes through the send-side rules. The age and flood limits do not apply: history is
        old by design and nothing here is acted on. Nothing is stored, consumed or acked,
        and the sync token is left alone."""
        if type(limit) is not int or not 1 <= limit <= HISTORY_MAX_ITEMS:
            raise ValueError(f"limit must be an integer from 1 to {HISTORY_MAX_ITEMS}")
        record = self.verify_room()
        ctx = record.context(self.member.server_name)
        room = self.member.room
        cursor = _next_batch(self.client.sync(filter=sync_filter(room, 0), timeout_ms=0))
        items: list[HistoryItem] = []
        drops: collections.Counter[str] = collections.Counter()
        seen: set[str] = set()
        for _ in range(HISTORY_MAX_PAGES):
            page = self.client.messages(room, from_token=cursor, direction="b",
                                        limit=HISTORY_PAGE_LIMIT, filter=GAP_FILTER)
            chunk = page.get("chunk")
            if not isinstance(chunk, list):
                raise matrix.Unreachable("GET /messages", detail="the answer has no chunk list")
            for event in chunk:
                item, reason = self._history_item(event, record, ctx, seen)
                if reason is not None:
                    drops[reason] += 1
                elif item is not None:
                    items.append(item)
                    if len(items) == limit:
                        return History(tuple(items), dict(drops), True)
            end = page.get("end")
            if not chunk or not isinstance(end, str) or end == cursor:
                return History(tuple(items), dict(drops), True)
            cursor = end
        return History(tuple(items), dict(drops), False)

    def _history_item(self, event: object, record: protocol.TeamRecord, ctx: protocol.Context,
                      seen: set[str]) -> tuple[HistoryItem | None, str | None]:
        """(the item, None), (None, a drop reason), or (None, None) for an event that is
        not this member's to see."""
        member = self.member
        if isinstance(event, dict) and event.get("sender") == member.user_id:
            return self._own_item(event, record, ctx)
        outcome = protocol.validate_event(event, ctx, member.user_id, seen, human_text=member.human_text)
        if outcome.kind == protocol.IGNORE:
            return None, None
        seen.add(event["event_id"])
        if outcome.kind == protocol.DROP:
            return None, outcome.reason
        if outcome.human is not None:
            return HistoryItem(HISTORY_IN, outcome.human.origin_server_ts, human=outcome.human), None
        reason = self._forge_check(outcome.ping, record)
        if reason is not None:
            return None, reason
        return HistoryItem(HISTORY_IN, outcome.ping.origin_server_ts, ping=outcome.ping), None

    def _own_item(self, event: dict, record: protocol.TeamRecord,
                  ctx: protocol.Context) -> tuple[HistoryItem | None, str | None]:
        """An event this member sent: a ping (§4-§6, §11, the role, the forge check) or an
        agent text (§7, a role holder); anything else, or both at once, is `schema`."""
        if not _envelope_ok(event):
            return None, "schema"
        content, sender = event["content"], self.member.user_id
        event_id, ts = event["event_id"], event["origin_server_ts"]
        try:
            if protocol.TEXT_KEY in content:
                if protocol.PING_KEY in content:
                    raise protocol.Refusal("schema")
                protocol.validate_text_content(content, ctx)
                protocol.check_text_sender(sender, ctx)
                return HistoryItem(HISTORY_OUT, ts, sent_event_id=event_id), None
            ping = protocol.validate_content(content, ctx)
            protocol.check_role(ping.verb, sender, ctx)
        except protocol.Refusal as refusal:
            return None, refusal.reason
        ping = dataclasses.replace(ping, sender=sender, event_id=event_id, origin_server_ts=ts)
        reason = self._forge_check(ping, record)
        if reason is not None:
            return None, reason
        return HistoryItem(HISTORY_OUT, ts, ping=ping), None

    # ── this member's status ─────────────────────────────────────────────────────────────

    def publish_status(self, until_ms: int) -> str:
        """Write this member's `listening` status (§8) under its own user ID; its event ID."""
        if type(until_ms) is not int or until_ms < 0:
            raise ValueError("until_ms must be a non-negative integer of milliseconds")
        if self.record is None:
            self.verify_room()
        content = {"v": protocol.PROTOCOL_VERSION, "state": protocol.STATUS_LISTENING, "until": until_ms}
        if protocol.content_size(content) > protocol.MAX_STATUS_BYTES:
            raise ValueError("the status is larger than §8 allows")
        return self.client.put_state(self.member.room, protocol.EVENT_STATUS, self.member.user_id, content)

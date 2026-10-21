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

The side rooms (Plan 00211 SPEC §4, §6): the main room's `agent_bus.rooms` index names them.
Each member joins the reports room on reading it, a restricted join it never syncs; a
coordinator with human text joins the questions room it is invited to, verifies it on its own
(a failure leaves the main room trusted), names it in its `/sync` filter, and receives every
human message there, stored with the question it answers. Their state is `rooms.json`.

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
TOPIC = "m.room.topic"
MEMBER = "m.room.member"
#: A change to any of these re-verifies the room (§8).
TRUST_TYPES = (CREATE, POWER_LEVELS, protocol.EVENT_TEAM, protocol.EVENT_ROOMS)
#: A change to any of these in a side room re-verifies that room alone (Plan 00211 SPEC §4.3).
SIDE_TRUST_TYPES = (*TRUST_TYPES, protocol.JOIN_RULES, protocol.EVENT_ROOM_LINK)
#: The filter probe H4 recorded against Tuwunel 1.9.3 (fixtures 040, 044, 071), with the
#: topic and the rooms index added.
STATE_TYPES = (CREATE, POWER_LEVELS, MEMBER, protocol.EVENT_TEAM, protocol.EVENT_STATUS, TOPIC,
               protocol.EVENT_ROOMS)
TIMELINE_TYPES = (protocol.EVENT_MESSAGE, MEMBER, POWER_LEVELS, protocol.EVENT_TEAM, protocol.EVENT_STATUS, TOPIC,
                  protocol.EVENT_ROOMS)
#: Read too while the filter names a side room, so a change to its trust is seen.
SIDE_TYPES = (protocol.JOIN_RULES, protocol.EVENT_ROOM_LINK)
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

#: What becomes of an invite (`invite_action`).
INVITE_MAIN = "main"
INVITE_QUESTIONS = "questions"
INVITE_PENDING = "pending"
INVITE_REJECT = "reject"

#: Why a side room is not joined, as `status` and the hook context print it.
REASON_NOT_INVITED = "not invited yet"
REASON_NOT_COORDINATOR = "this member is not a coordinator"
REASON_NO_HUMAN_TEXT = "human_text is false; re-issue the bundle without --no-human-text"
REASON_JOIN_REFUSED = "the homeserver refused the join"


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
    sent (`out`, `ping`), a human message received (`in`, `human`), an agent text naming
    this member (`in`, `peer`), or an agent text this member sent (`out`, `sent_event_id`;
    its text is never kept)."""

    direction: str
    origin_server_ts: int
    ping: protocol.Ping | None = None
    human: protocol.HumanMessage | None = None
    peer: protocol.PeerText | None = None
    sent_event_id: str | None = None
    #: `main` or `questions`; in the questions room, the item's question as (n, headline).
    room: str = protocol.ROOM_MAIN
    question: tuple[int, str] | None = None


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


def sync_filter(room_id: str, timeline_limit: int, *, lazy_members: bool = True,
                also: Sequence[str] = ()) -> dict:
    """The inline `/sync` filter: the team room, and `also` (a coordinator's questions room),
    the state and timeline types the syncer reads, nothing else (no presence, account data or
    ephemeral events). The first sync and the one after a join load every member
    (`lazy_members=False`), so `status` knows who had joined before this member; later syncs
    see joins in the timeline."""
    extra = SIDE_TYPES if also else ()
    return {
        "presence": {"types": []},
        "account_data": {"types": []},
        "room": {
            "rooms": [room_id, *also],
            "ephemeral": {"types": []},
            "account_data": {"types": []},
            "state": {"types": [*STATE_TYPES, *extra], "lazy_load_members": lazy_members},
            "timeline": {"types": [*TIMELINE_TYPES, *extra], "limit": timeline_limit},
        },
    }


def invite_acceptable(room_id: object, invite: object, member: config.Member) -> bool:
    """§8: accept only an invite to the bundle's `room` whose inviter and stripped
    `m.room.create` sender are both the bundle's `admin`."""
    return room_id == member.room and _from_admin(invite, member)


def invite_action(room_id: object, invite: object, member: config.Member, questions_room: str | None,
                  may_take_questions: bool) -> str:
    """§8 with Plan 00211 SPEC §4.4: an admin invite to the main room is joined, and one to the
    questions room the index names when this member may take it (a coordinator with human
    text). Any other admin invite is left pending, never rejected, since rejecting would need a
    re-invite. Every other invite is rejected."""
    if not _from_admin(invite, member):
        return INVITE_REJECT
    if room_id == member.room:
        return INVITE_MAIN
    if room_id == questions_room and may_take_questions:
        return INVITE_QUESTIONS
    return INVITE_PENDING


def _from_admin(invite: object, member: config.Member) -> bool:
    """Whether the invite's inviter and its stripped `m.room.create` sender are both `admin`."""
    if not isinstance(invite, dict):
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
        #: The side rooms this member has met, by purpose; kept in `rooms.json`.
        self.side: dict[str, inbox.SideRoom] = state.side_rooms()
        #: True from joining the questions room until the sync that holds the join: that
        #: sync's events before the join are the room's history, never processed.
        self._fresh_questions = False

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
        self.state.save_about(self._read_about())
        if self.record is None or self.record.forge_api != record.forge_api:
            self._forge = None
        self.record = record
        # §8: a status counts only from a role holder, so a member dropped from `roles` loses it.
        kept = {uid: until for uid, until in self.statuses.items() if uid in record.roles}
        if kept != self.statuses:
            self.statuses = kept
            self._save_room_view()
        self._sync_side_rooms(record)
        return record

    def _read_about(self) -> str | None:
        """The team's description: the room topic when `admin` set it to something other than
        the fixed sentence and it passes the description grammar. Informational only, so a
        missing, odd or foreign topic is no description and never a loss of trust."""
        member = self.member
        try:
            event = self.client.get_state(member.room, TOPIC, "", as_event=True)
        except (matrix.NotFound, matrix.Forbidden):
            return None
        return self._about_of(event)

    def _about_of(self, event: object) -> str | None:
        """The description a room-topic event gives, or None for anything else."""
        if not isinstance(event, dict):
            return None
        content = event.get("content")
        topic = content.get("topic") if isinstance(content, dict) else None
        if (event.get("sender") != self.member.admin or not protocol.is_description(topic)
                or topic == protocol.ROOM_TOPIC):
            return None
        return topic

    def _check_create(self, room: str) -> None:
        create = self.client.get_state(room, CREATE, "", as_event=True)
        content = create.get("content")
        if (
            create.get("type") != CREATE
            or create.get("sender") != self.member.admin
            or create.get("event_id") != "$" + room[1:]
            or not isinstance(content, dict)
            or content.get("room_version") != protocol.ROOM_VERSION
            or "additional_creators" in content
        ):
            raise protocol.Untrusted("the room's create event")

    def _read_trusted_state(self) -> tuple[protocol.TeamRecord, dict]:
        member, room = self.member, self.member.room
        self._check_create(room)
        team_event = self.client.get_state(room, protocol.EVENT_TEAM, "", as_event=True)
        record = protocol.parse_team_event(team_event, member.admin, member.server_name, member.team)
        if record.roles.get(member.user_id) not in protocol.ROLES:
            raise protocol.Untrusted("the team record gives this member no role")
        protocol.check_power_levels(self.client.get_state(room, POWER_LEVELS, ""), record.humans)
        return record, team_event["content"]

    def _lose_trust(self, reason: str) -> RoomUntrusted:
        self.record = None
        self._forge = None
        self.side = {}
        if self.statuses:
            self.statuses = {}
            self._save_room_view()
        self.state.mark_untrusted(reason)
        self._say(f"the team room is not trusted: {reason}")
        return RoomUntrusted(reason)

    # ── the side rooms (Plan 00211) ──────────────────────────────────────────────────────

    def _read_index(self) -> dict[str, str]:
        """The trusted main room's `agent_bus.rooms` index; empty when there is none yet, and
        when it is not `admin`'s exact form (logged: only `admin` can write it)."""
        try:
            event = self.client.get_state(self.member.room, protocol.EVENT_ROOMS, "", as_event=True)
        except matrix.NotFound:
            return {}
        try:
            return protocol.parse_rooms_index(event, self.member.admin, self.member.room)
        except protocol.Untrusted as exc:
            self._say(f"ignored the rooms index: {exc}")
            return {}

    def _sync_side_rooms(self, record: protocol.TeamRecord) -> None:
        """Bring each side room the index names to its state, after the main room verifies."""
        index = self._read_index()
        side: dict[str, inbox.SideRoom] = {}
        for purpose in protocol.PURPOSES:
            room_id = index.get(purpose)
            if room_id is None:
                continue
            prev = self.side.get(purpose)
            if prev is not None and prev.room != room_id:
                prev = None
            if purpose == protocol.PURPOSE_REPORTS:
                side[purpose] = self._reports_room(room_id, prev)
            else:
                side[purpose] = self._questions_room(room_id, prev, record)
        self._set_side(side)

    def _set_side(self, side: Mapping[str, inbox.SideRoom]) -> None:
        if dict(side) != self.side:
            self.side = dict(side)
            self.state.save_side_rooms(self.side)

    def _reports_room(self, room_id: str, prev: inbox.SideRoom | None) -> inbox.SideRoom:
        """Joined once, by the restricted join, and never synced; `report` verifies it."""
        if prev is not None and prev.state == "join":
            return prev
        try:
            self.client.join(room_id)
        except (matrix.Forbidden, matrix.NotFound):
            self._say(f"could not join the reports room {room_id}")
            return inbox.SideRoom(room_id, "refused", REASON_JOIN_REFUSED)
        return inbox.SideRoom(room_id, "join", None)

    def _may_take_questions(self, record: protocol.TeamRecord | None) -> bool:
        return (record is not None and record.roles.get(self.member.user_id) == protocol.ROLE_COORDINATOR
                and self.member.human_text)

    def _questions_room(self, room_id: str, prev: inbox.SideRoom | None,
                        record: protocol.TeamRecord) -> inbox.SideRoom:
        """Taken only by a coordinator with human text (SPEC §4.4, §4.5), then verified."""
        if record.roles.get(self.member.user_id) != protocol.ROLE_COORDINATOR:
            return inbox.SideRoom(room_id, "pending", REASON_NOT_COORDINATOR)
        if not self.member.human_text:
            return inbox.SideRoom(room_id, "refused", REASON_NO_HUMAN_TEXT)
        if prev is None or prev.state in ("pending", "refused"):
            try:
                self.client.join(room_id)
            except (matrix.Forbidden, matrix.NotFound):
                return inbox.SideRoom(room_id, "pending", REASON_NOT_INVITED)
            self._fresh_questions = True
        return self._verify_side(protocol.PURPOSE_QUESTIONS, room_id, record)

    def _verify_side(self, purpose: str, room_id: str, record: protocol.TeamRecord) -> inbox.SideRoom:
        """SPEC §4.3: the create event, the back-link, the power levels the record calls for
        and the join rules. A failure is this room's alone."""
        member = self.member
        try:
            self._check_create(room_id)
            link = self.client.get_state(room_id, protocol.EVENT_ROOM_LINK, "", as_event=True)
            protocol.check_room_link(link, member.admin, member.team, purpose, member.room)
            protocol.check_side_power_levels(self.client.get_state(room_id, POWER_LEVELS, ""), purpose,
                                             record.humans, bool(protocol.coordinators(record.roles)))
            protocol.check_join_rules(self.client.get_state(room_id, protocol.JOIN_RULES, ""), purpose,
                                      member.room)
        except protocol.Untrusted as exc:
            reason = str(exc)
        except (matrix.NotFound, matrix.Forbidden):
            reason = "not joined, or its state is missing"
        else:
            return inbox.SideRoom(room_id, "join", None)
        self._say(f"the {purpose} room {room_id} is not trusted: {reason}")
        return inbox.SideRoom(room_id, "untrusted", reason)

    def _listening_room(self) -> str | None:
        """The questions room while this member is its coordinator and it is not untrusted:
        named in the filter, so its invite and its messages are seen."""
        found = self.side.get(protocol.PURPOSE_QUESTIONS)
        if found is None or found.state not in ("join", "pending") or not self._may_take_questions(self.record):
            return None
        return found.room

    def _questions_events(self, rooms: dict, room_id: str | None, since: str) -> list:
        """The questions room's new messages in this sync, oldest first: after this member's
        join when it has just joined (the history is skipped), gap-filled when limited, none
        while the room is untrusted or this member is not its coordinator."""
        if room_id is None:
            return []
        entry = _section(_section(rooms, "join"), room_id)
        if not entry:
            return []
        state_events = _events(entry, "state")
        timeline = _section(entry, "timeline")
        events = _events(entry, "timeline")
        if self._fresh_questions:
            self._fresh_questions = False
            joins = [n for n, e in enumerate(events) if _type_of(e) == MEMBER and e.get("state_key") == self.member.user_id
                     and isinstance(e.get("content"), dict) and e["content"].get("membership") == "join"]
            # Without its join, the timeline is all after it: a timeline is the newest events.
            return events[joins[-1] + 1:] if joins else events
        limited = timeline.get("limited") is True
        if limited or any(_type_of(e) in SIDE_TRUST_TYPES for e in state_events + events):
            side = dict(self.side)
            side[protocol.PURPOSE_QUESTIONS] = self._verify_side(protocol.PURPOSE_QUESTIONS, room_id, self.record)
            self._set_side(side)
        if self._listening_room() != room_id or self.side[protocol.PURPOSE_QUESTIONS].state != "join":
            return []
        if limited:
            events = self._fill_gap(room_id, since, timeline.get("prev_batch")) + events
        return events

    def _room_messages(self, room_id: str) -> tuple[list, bool]:
        """A room's messages oldest first, read backwards in pages, at most `HISTORY_MAX_PAGES`;
        whether the scan reached the start."""
        cursor = _next_batch(self.client.sync(filter=sync_filter(room_id, 0), timeout_ms=0))
        found: list = []
        for _ in range(HISTORY_MAX_PAGES):
            page = self.client.messages(room_id, from_token=cursor, direction="b",
                                        limit=HISTORY_PAGE_LIMIT, filter=GAP_FILTER)
            chunk = page.get("chunk")
            if not isinstance(chunk, list):
                raise matrix.Unreachable("GET /messages", detail="the answer has no chunk list")
            found.extend(chunk)
            end = page.get("end")
            if not chunk or not isinstance(end, str) or end == cursor:
                return found[::-1], True
            cursor = end
        return found[::-1], False

    def question_index(self) -> protocol.QuestionIndex:
        """The questions room's questions and their replies, read from the room (SPEC §8.3),
        through a verified main room and a joined questions room."""
        room_id = self._joined_questions()
        events, _ = self._room_messages(room_id)
        return protocol.build_question_index(events, self.record.context(self.member.server_name))

    def _joined_questions(self) -> str:
        found = self.side.get(protocol.PURPOSE_QUESTIONS)
        if self.record is None:
            self.verify_room()
            found = self.side.get(protocol.PURPOSE_QUESTIONS)
        if found is None:
            raise RoomUntrusted(f"team {self.member.team} has no questions room yet")
        if found.state != "join" or not self._may_take_questions(self.record):
            raise RoomUntrusted(f"the questions room {found.room} is {found.state}: "
                                f"{found.reason or REASON_NOT_COORDINATOR}")
        return found.room

    @staticmethod
    def _question_of(index: protocol.QuestionIndex, event: Mapping[str, object]) -> tuple[int, str] | None:
        event_id = event.get("event_id")
        found = index.attached_to(event_id) if event_id in index.attached else protocol.attach_question(event, index)
        if found is None:
            own = index.by_event.get(event_id) or index.by_event.get(index.thread_of.get(event_id, ""))
            found = own
        return None if found is None else (found.n, found.headline)

    # ── syncing ──────────────────────────────────────────────────────────────────────────

    def sync_once(self, timeout_ms: int = 0) -> Batch:
        """One `/sync` (long-polling up to `timeout_ms`), its items stored, its token saved."""
        since = self.state.sync_token()
        if since is None:
            return self._first_sync()
        if self.record is None:
            self.verify_room()
        questions = self._listening_room()
        response = self.client.sync(since=since, filter=sync_filter(
            self.member.room, TIMELINE_LIMIT, also=(questions,) if questions else ()), timeout_ms=timeout_ms)
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
            events = self._fill_gap(self.member.room, since, timeline.get("prev_batch")) + events
        elif any(_type_of(e) == TOPIC for e in state_events + events):
            self.state.save_about(self._read_about())
        self._read_room_state(state_events + events)
        return self._receive(events, next_batch, self._questions_events(rooms, questions, since))

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
        """Join the main room's invite and an acceptable questions invite, leave other admin
        invites pending, reject every other (§8, SPEC §4.4), logged by room ID only. Whether
        the main room was joined."""
        joined = False
        found = self.side.get(protocol.PURPOSE_QUESTIONS)
        questions = found.room if found is not None else None
        may = self._may_take_questions(self.record)
        for room_id, invite in _section(rooms, "invite").items():
            if not protocol.is_room_id(room_id):
                self._say("ignored an invite with a malformed room ID")
                continue
            action = invite_action(room_id, invite, self.member, questions, may)
            if action == INVITE_MAIN:
                self.client.join(room_id)
                joined = True
            elif action == INVITE_QUESTIONS:
                self.client.join(room_id)
                self._fresh_questions = True
                side = dict(self.side)
                side[protocol.PURPOSE_QUESTIONS] = self._verify_side(protocol.PURPOSE_QUESTIONS, room_id, self.record)
                self._set_side(side)
            elif action == INVITE_PENDING:
                self._say(f"left an invite to room {room_id} pending")
            else:
                self.client.leave(room_id)
                self._say(f"rejected an invite to room {room_id}")
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
        # The token is past any questions-room join that verify made, so nothing is skipped.
        self._fresh_questions = False
        return Batch(first, 0, (), {})

    def _fill_gap(self, room_id: str, since: str, prev_batch: object) -> list:
        """The messages a limited timeline of `room_id` left out, oldest first, read forward
        from the previous sync's token to the timeline's `prev_batch`."""
        if not config.is_printable_token(prev_batch):
            raise _malformed("a limited timeline has no prev_batch")
        found: list = []
        cursor = since
        for _ in range(GAP_MAX_PAGES):
            page = self.client.messages(room_id, from_token=cursor, to_token=prev_batch,
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

    def _receive(self, events: Sequence[object], next_batch: str, questions: Sequence[object] = ()) -> Batch:
        record, member = self.record, self.member
        ctx = record.context(member.server_name)
        seen = set(self.state.known_event_ids())
        now = self._clock_ms()
        accepted: list[dict] = []
        asked: list[dict] = []
        answers: list[protocol.Ping] = []
        drops: collections.Counter[str] = collections.Counter()
        for room, event in [(protocol.ROOM_MAIN, e) for e in events] + [(protocol.PURPOSE_QUESTIONS, e) for e in questions]:
            outcome = protocol.validate_event(event, ctx, member.user_id, seen, human_text=member.human_text,
                                              room=room)
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
                (accepted if room == protocol.ROOM_MAIN else asked).append(event)
            elif room == protocol.ROOM_MAIN and protocol.reached_no_agent(event, ctx, outcome):
                self.state.log_ignored(member.team, event_id, event.get("sender"), outcome.reason, now)
        if asked:
            index = self.question_index()
            for event in asked:
                question = self._question_of(index, event)
                accepted.append({**event, inbox.ROOM_KEY: protocol.PURPOSE_QUESTIONS, inbox.QUESTION_KEY:
                                 None if question is None else {"n": question[0], "headline": question[1]}})
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
        if outcome.ping is None:
            # Free text, a human's or a peer's: one age limit and the flood limit.
            message = outcome.item
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

    def history(self, limit: int, room: str = protocol.ROOM_MAIN) -> History:
        """This member's past items, newest first, at most `limit` (§13 `history`), read
        from the room itself, so fresh local state loses nothing. Each received item goes
        through the offline receive steps and the forge check; an item this member sent
        goes through the send-side rules. The age and flood limits do not apply: history is
        old by design and nothing here is acted on. Nothing is stored, consumed or acked,
        and the sync token is left alone. `room="questions"` reads the questions room, each
        item with its question (Plan 00211 SPEC §8.4)."""
        if type(limit) is not int or not 1 <= limit <= HISTORY_MAX_ITEMS:
            raise ValueError(f"limit must be an integer from 1 to {HISTORY_MAX_ITEMS}")
        if room not in protocol.RECEIVING_ROOMS:
            raise ValueError(f"no history is read from room {room!r}")
        record = self.verify_room()
        ctx = record.context(self.member.server_name)
        if room == protocol.PURPOSE_QUESTIONS:
            return self._questions_history(limit, ctx)
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

    def _questions_history(self, limit: int, ctx: protocol.Context) -> History:
        events, complete = self._room_messages(self._joined_questions())
        index = protocol.build_question_index(events, ctx)
        items: list[HistoryItem] = []
        drops: collections.Counter[str] = collections.Counter()
        seen: set[str] = set()
        for event in reversed(events):
            if not isinstance(event, dict):
                drops["schema"] += 1
                continue
            question = self._question_of(index, event)
            if event.get("sender") == self.member.user_id:
                if not _envelope_ok(event):
                    drops["schema"] += 1
                    continue
                items.append(HistoryItem(HISTORY_OUT, event["origin_server_ts"], sent_event_id=event["event_id"],
                                         room=protocol.PURPOSE_QUESTIONS, question=question))
            else:
                outcome = protocol.validate_event(event, ctx, self.member.user_id, seen,
                                                  human_text=self.member.human_text, room=protocol.PURPOSE_QUESTIONS)
                if outcome.kind == protocol.IGNORE:
                    continue
                seen.add(event["event_id"])
                if outcome.kind == protocol.DROP:
                    drops[outcome.reason] += 1
                    continue
                items.append(HistoryItem(HISTORY_IN, outcome.human.origin_server_ts, human=outcome.human,
                                         room=protocol.PURPOSE_QUESTIONS, question=question))
            if len(items) == limit:
                return History(tuple(items), dict(drops), True)
        return History(tuple(items), dict(drops), complete)

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
        if outcome.peer is not None:
            return HistoryItem(HISTORY_IN, outcome.peer.origin_server_ts, peer=outcome.peer), None
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

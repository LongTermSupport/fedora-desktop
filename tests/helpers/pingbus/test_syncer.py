"""Unit tests for helpers/pingbus/syncer.py: the sync engine and room trust.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_syncer

Spec: docs/agent-bus-protocol.md §8 (trusted room, the invite rule, status), §9 (the
receive pipelines; the first sync records only `next_batch`; the token is saved after the
batch is durable), §10 (stale and flood), §14 (exit 10). Plan 00161's DESIGN.md section 4
(joining, loss of trust) and section 12 row U10.

Every test talks real HTTP on loopback to the U08 fake homeserver through U09's client;
the forge is a stand-in recording what it was asked. Clocks are injected.
"""

from __future__ import annotations

import json
import pathlib
import sys
import tempfile
import unittest
import urllib.parse

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import config, forge, inbox, limits, matrix, protocol, syncer
from tests.helpers.pingbus import fake_client_api

SN = "team-a.agent-bus.internal"
TEAM = "team-a"
ME_LP = "myrepo.1+workstation.podman"
ORCH_LP = "orch.1+workstation.podman"
PEER_LP = "peer.1+workstation.podman"
SHA = "0123456789abcdef0123456789abcdef01234567"
PATH_REF = f"path:example-org/myrepo@{SHA}:docs/plan.md"
COMMIT_REF = f"commit:example-org/myrepo@{SHA}"
T0 = 1_791_000_000.0
FIXTURES = pathlib.Path(__file__).resolve().parent / "fixtures" / "tuwunel"


def q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


class StubForge:
    """The forge client's interface (`api`, `check`): records each check, fails on demand."""

    api = "https://api.github.com"

    def __init__(self) -> None:
        self.checked: list[tuple[str, tuple[str, ...]]] = []
        self.fail: dict[str, str] = {}

    def check(self, ref: protocol.Ref, branches: tuple[str, ...]) -> None:
        self.checked.append((ref.text, tuple(branches)))
        code = self.fail.get(ref.text)
        if code is not None:
            raise forge.ForgeError(code, "stub")


class TeamCase(unittest.TestCase):
    """`admin`'s trusted team room: a human and two agents joined, this member invited."""

    human_text = True
    member_limits = limits.Limits()

    def setUp(self) -> None:
        self.now = T0
        self.fake = fake_client_api.FakeHomeserver(server_name=SN, clock=lambda: self.now)
        self.admin = self.fake.add_user("admin", admin=True)
        self.me = self.fake.add_user(ME_LP)
        self.orch = self.fake.add_user(ORCH_LP)
        self.peer = self.fake.add_user(PEER_LP)
        self.alice = self.fake.add_user("alice")
        self.outsider = self.fake.add_user("mallory")
        self.tokens = {u: self.fake.mint_token(u) for u in
                       (self.admin, self.me, self.orch, self.peer, self.alice, self.outsider)}
        self.room = self.create_room(self.admin, [self.me, self.orch, self.peer, self.alice])
        self.record = {
            "v": 1, "team": TEAM, "humans": [self.alice],
            "roles": {self.me: "worker", self.orch: "orchestrator", self.peer: "worker"},
            "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
            "path_prefixes": ["docs/"], "forge_api": "https://api.github.com",
        }
        self.put_state(self.admin, protocol.EVENT_TEAM, "", self.record)
        for uid in (self.orch, self.peer, self.alice):
            self.call(uid, "POST", f"/_matrix/client/v3/rooms/{q(self.room)}/join", {})
        self.base = self.enterContext(fake_client_api.serve(self.fake))
        self.bundle = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.state = inbox.TeamState(self.bundle / "state")
        self.forge = StubForge()
        self.logged: list[str] = []
        self.txn = 0

    # ── the homeserver, driven as the other accounts ─────────────────────────────────────

    def call(self, uid: str, method: str, path: str, body: object = None) -> dict:
        status, answer = self.fake.request(method, path, body=body, token=self.tokens[uid])
        self.assertEqual(status, 200, answer)
        return answer

    def create_room(self, uid: str, invite: list[str], levels: dict | None = None) -> str:
        return self.call(uid, "POST", "/_matrix/client/v3/createRoom", {
            "preset": "private_chat", "room_version": "12", "name": TEAM,
            "power_level_content_override": levels or protocol.expected_power_levels([self.alice]),
            "invite": invite})["room_id"]

    def put_state(self, uid: str, event_type: str, key: str, content: dict) -> str:
        return self.call(uid, "PUT", f"/_matrix/client/v3/rooms/{q(self.room)}/state/{event_type}/{q(key)}",
                         content)["event_id"]

    def send(self, uid: str, content: dict) -> str:
        self.txn += 1
        return self.call(uid, "PUT", f"/_matrix/client/v3/rooms/{q(self.room)}/send/m.room.message/t{self.txn}",
                         content)["event_id"]

    def ping(self, uid: str, verb: str, to: list[str] | None = None, ref: str | None = None,
             re: str | None = None) -> str:
        return self.send(uid, protocol.build_ping(verb, to or [self.me], ref=ref, re=re))

    def human(self, body: str = "please halt", *, to: list[str] | None = None, room: bool = False,
              relates: dict | None = None, uid: str | None = None) -> str:
        mentions: dict = {"room": True} if room else {"user_ids": to if to is not None else [self.me]}
        content = {"msgtype": "m.text", "body": body, "m.mentions": mentions}
        if relates is not None:
            content["m.relates_to"] = relates
        return self.send(uid or self.alice, content)

    # ── this member ──────────────────────────────────────────────────────────────────────

    def member(self, room: str | None = None) -> config.Member:
        token_path = self.bundle / "token"
        token_path.write_text(self.tokens[self.me], encoding="ascii")
        token_path.chmod(0o600)
        return config.Member(
            team=TEAM, user_id=self.me, handle=ME_LP, member_type="podman", server_name=SN,
            base_url=self.base, plain_http_hosts=("127.0.0.1",), admin=self.admin,
            room=room or self.room, human_text=self.human_text, limits=self.member_limits,
            bundle_dir=self.bundle, token_path=token_path)

    def syncer(self, room: str | None = None) -> syncer.Syncer:
        member = self.member(room)
        return syncer.Syncer(
            member, matrix.Client.for_member(member, sleep=lambda s: None), self.state,
            forge_for=lambda record: self.forge, clock_ms=lambda: int(self.now * 1000),
            log=self.logged.append)

    def ready(self) -> syncer.Syncer:
        """A syncer past its first sync: invited, joined, the room verified."""
        s = self.syncer()
        s.sync_once()
        return s

    def stored(self) -> list[str]:
        return [item.event_id for item in self.pending().items]

    def pending(self) -> inbox.Pending:
        ctx = protocol.parse_team_record(self.record, SN, TEAM).context(SN)
        return self.state.pending(ctx, self.me, human_text=self.human_text)

    def drop_log(self) -> list[list[str]]:
        path = self.state.path / inbox.DROPPED_LOG
        if not path.exists():
            return []
        return [line.split("\t") for line in path.read_text(encoding="ascii").splitlines()]

    def sync_queries(self) -> list[dict[str, str]]:
        return [r.query for r in self.fake.http_log if r.path == "/_matrix/client/v3/sync"]


class FilterTest(unittest.TestCase):
    def test_filter_is_the_one_probe_h4_recorded(self):
        recorded = json.loads((FIXTURES / "071-sync-limited.json").read_text(encoding="utf-8"))
        flt = json.loads(recorded["query"]["filter"])
        room = flt["room"]["rooms"][0]
        self.assertEqual(syncer.sync_filter(room, flt["room"]["timeline"]["limit"]), flt)

    def test_gap_fill_filter_is_the_one_probe_h4_recorded(self):
        recorded = json.loads((FIXTURES / "072-messages-gap-fill.json").read_text(encoding="utf-8"))
        self.assertEqual(syncer.GAP_FILTER, json.loads(recorded["query"]["filter"]))
        self.assertEqual(syncer.GAP_PAGE_LIMIT, int(recorded["query"]["limit"]))

    def test_untrusted_is_exit_10(self):
        self.assertEqual(syncer.RoomUntrusted("x").exit_code, 10)
        self.assertIsInstance(syncer.RoomUntrusted("x"), protocol.Untrusted)


class FirstSyncTest(TeamCase):
    def test_first_sync_takes_next_batch_only(self):
        self.human("history before this member synced")
        s = self.syncer()
        batch = s.sync_once()
        self.assertTrue(batch.first)
        self.assertEqual(batch.accepted, ())
        self.assertEqual(json.loads(self.sync_queries()[0]["filter"])["room"]["timeline"]["limit"], 0)
        self.assertNotIn("since", self.sync_queries()[0])
        self.assertIsNotNone(self.state.sync_token())
        self.assertEqual(self.stored(), [])
        after = self.human("after the first sync")
        s.sync_once()
        self.assertEqual(self.stored(), [after])

    def test_history_of_a_newly_joined_room_is_never_processed(self):
        s = self.syncer()
        s.sync_once()
        self.assertEqual(s.sync_once().accepted, ())
        self.assertEqual(self.stored(), [])

    def test_the_saved_token_is_used_by_the_next_process(self):
        self.ready()
        token = self.state.sync_token()
        event = self.human("for the next process")
        batch = self.syncer().sync_once()
        self.assertFalse(batch.first)
        self.assertEqual(self.sync_queries()[-1]["since"], token)
        self.assertEqual(batch.accepted, (event,))


class InviteTest(TeamCase):
    def test_admins_invite_to_the_bundles_room_is_accepted_and_verified(self):
        s = self.syncer()
        s.sync_once()
        self.assertEqual(self.fake.rooms[self.room].membership(self.me), "join")
        self.assertEqual(s.record, protocol.parse_team_record(self.record, SN, TEAM))
        self.assertEqual(json.loads((self.state.path / inbox.TEAM_FILE).read_text()), self.record)

    def test_an_invite_from_anyone_else_is_declined_and_logged_by_room_id_only(self):
        levels = protocol.expected_power_levels([self.alice])
        other = self.create_room(self.outsider, [self.me], levels)
        with self.assertRaises(syncer.RoomUntrusted):
            self.syncer(room=other).sync_once()
        self.assertEqual(self.fake.rooms[other].membership(self.me), "invite")
        self.assertIsNone(self.state.sync_token())
        self.assertTrue(any(other in line for line in self.logged), self.logged)
        self.assertFalse(any(self.outsider in line for line in self.logged), self.logged)

    def invite(self, *, inviter: str, creator: str) -> dict:
        return {"invite_state": {"events": [
            {"type": "m.room.create", "state_key": "", "sender": creator, "content": {"room_version": "12"}},
            {"type": "m.room.member", "state_key": self.me, "sender": inviter,
             "content": {"membership": "invite"}},
        ]}}

    def test_invite_rule(self):
        member = self.member()
        ok = self.invite(inviter=self.admin, creator=self.admin)
        self.assertTrue(syncer.invite_acceptable(self.room, ok, member))
        cases = {
            "another room": ("!" + "B" * 43, ok),
            "inviter not admin": (self.room, self.invite(inviter=self.orch, creator=self.admin)),
            "creator not admin": (self.room, self.invite(inviter=self.admin, creator=self.outsider)),
            "no create event": (self.room, {"invite_state": {"events": ok["invite_state"]["events"][1:]}}),
            "no invite event": (self.room, {"invite_state": {"events": ok["invite_state"]["events"][:1]}}),
            "not an object": (self.room, []),
            "events not a list": (self.room, {"invite_state": {"events": {}}}),
        }
        for name, (room_id, invite) in cases.items():
            with self.subTest(name):
                self.assertFalse(syncer.invite_acceptable(room_id, invite, member))


class TrustTest(TeamCase):
    def assert_untrusted_now(self, s: syncer.Syncer, token: str | None) -> None:
        with self.assertRaises(syncer.RoomUntrusted) as caught:
            s.sync_once()
        self.assertEqual(caught.exception.exit_code, 10)
        self.assertFalse((self.state.path / inbox.TEAM_FILE).exists())
        self.assertEqual(self.state.sync_token(), token)
        self.assertEqual(self.stored(), [])
        self.assertTrue(any("not trusted" in line for line in self.logged), self.logged)

    def test_power_levels_changed_loses_trust_and_receives_nothing(self):
        s = self.ready()
        token = self.state.sync_token()
        self.human("sent in the batch that also loses trust")
        levels = protocol.expected_power_levels([self.alice])
        levels["users"][self.outsider] = 50
        self.put_state(self.admin, "m.room.power_levels", "", levels)
        self.assert_untrusted_now(s, token)
        self.assert_untrusted_now(s, token)

    def test_team_record_no_longer_listing_this_member_loses_trust(self):
        s = self.ready()
        token = self.state.sync_token()
        self.record["roles"].pop(self.me)
        self.put_state(self.admin, protocol.EVENT_TEAM, "", self.record)
        self.assert_untrusted_now(s, token)

    def test_team_record_with_an_unknown_key_loses_trust(self):
        s = self.ready()
        token = self.state.sync_token()
        self.put_state(self.admin, protocol.EVENT_TEAM, "", {**self.record, "extra": 1})
        self.assert_untrusted_now(s, token)

    def test_a_valid_record_change_is_taken_up(self):
        s = self.ready()
        self.record["roles"][self.peer] = "orchestrator"
        self.put_state(self.admin, protocol.EVENT_TEAM, "", self.record)
        s.sync_once()
        self.assertEqual(s.record.roles[self.peer], "orchestrator")
        self.assertEqual(json.loads((self.state.path / inbox.TEAM_FILE).read_text()), self.record)

    def test_verify_checks_the_create_event(self):
        s = self.ready()
        create = self.fake.rooms[self.room].state[("m.room.create", "")]
        for name, change in {
            "sender": lambda e: setattr(e, "sender", self.outsider),
            "room version": lambda e: e.content.update(room_version="11"),
            "additional creators": lambda e: e.content.update(additional_creators=[self.outsider]),
        }.items():
            with self.subTest(name):
                saved = (create.sender, dict(create.content))
                change(create)
                with self.assertRaises(syncer.RoomUntrusted):
                    s.verify_room()
                create.sender, create.content = saved[0], saved[1]
                s.verify_room()

    def test_a_missing_team_record_is_exit_10_not_7(self):
        self.fake.rooms[self.room].state.pop((protocol.EVENT_TEAM, ""))
        with self.assertRaises(syncer.RoomUntrusted) as caught:
            self.syncer().sync_once()
        self.assertEqual(caught.exception.exit_code, 10)
        self.assertNotIsInstance(caught.exception, matrix.MatrixError)

    def test_a_missing_room_is_exit_10_not_7(self):
        with self.assertRaises(syncer.RoomUntrusted) as caught:
            self.syncer(room="!" + "Z" * 43).sync_once()
        self.assertEqual(caught.exception.exit_code, 10)

    def test_not_joined_is_exit_10(self):
        self.fake.rooms[self.room].state.pop(("m.room.member", self.me))
        with self.assertRaises(syncer.RoomUntrusted):
            self.syncer().sync_once()


class HumanPipelineTest(TeamCase):
    def test_addressed_text_is_stored_and_unaddressed_ignored(self):
        s = self.ready()
        mine = self.human("for me")
        room = self.human("for everyone", room=True)
        self.human("for the orchestrator", to=[self.orch])
        batch = s.sync_once()
        self.assertEqual(batch.accepted, (mine, room))
        self.assertEqual(sorted(self.stored()), sorted([mine, room]))
        self.assertEqual(dict(batch.drops), {})

    def test_reply_fallback_removed(self):
        s = self.ready()
        quoted = self.ping(self.orch, "halt")
        self.human(f"> <{self.orch}> [agent-bus] halt\n\nyes, halt now",
                   relates={"m.in_reply_to": {"event_id": quoted}})
        s.sync_once()
        texts = [item.outcome.human.text for item in self.pending().items if item.outcome.human]
        self.assertEqual(texts, ["yes, halt now"])

    def test_edit_and_wrong_msgtype_dropped(self):
        s = self.ready()
        first = self.human("original")
        edit = self.human("* edited", relates={"rel_type": "m.replace", "event_id": first})
        notice = self.send(self.alice, {"msgtype": "m.notice", "body": "x", "m.mentions": {"user_ids": [self.me]}})
        batch = s.sync_once()
        self.assertEqual(batch.accepted, (first,))
        self.assertEqual(dict(batch.drops), {"edit": 1, "schema": 1})
        self.assertEqual({row[1] for row in self.drop_log()}, {edit, notice})

    def test_stale_human_message_dropped(self):
        s = self.ready()
        self.human("old")
        self.now += self.member_limits.human_max_age_s + 1
        batch = s.sync_once()
        self.assertEqual(batch.accepted, ())
        self.assertEqual(dict(batch.drops), {"stale": 1})

    def test_non_team_and_admin_senders_dropped(self):
        s = self.ready()
        self.call(self.admin, "POST", f"/_matrix/client/v3/rooms/{q(self.room)}/invite", {"user_id": self.outsider})
        self.call(self.outsider, "POST", f"/_matrix/client/v3/rooms/{q(self.room)}/join", {})
        self.human("from a non-team account", uid=self.outsider)
        self.human("from admin", uid=self.admin)
        batch = s.sync_once()
        self.assertEqual(dict(batch.drops), {"sender": 2})


class HumanTextOffTest(TeamCase):
    human_text = False

    def test_every_human_message_dropped_as_sender(self):
        s = self.ready()
        event = self.human("addressed, but this member takes pings only")
        batch = s.sync_once()
        self.assertEqual(batch.accepted, ())
        self.assertEqual(dict(batch.drops), {"sender": 1})
        self.assertEqual(self.drop_log()[0][1:4], [event, self.alice, "sender"])


class FloodTest(TeamCase):
    member_limits = limits.Limits(recv_per_sender_minute=10)

    def test_over_the_receive_flood_limit_dropped(self):
        s = self.ready()
        for n in range(12):
            self.human(f"message {n}")
        batch = s.sync_once()
        self.assertEqual(len(batch.accepted), 10)
        self.assertEqual(dict(batch.drops), {"rate": 2})


class PingPipelineTest(TeamCase):
    def test_valid_ping_checked_at_the_forge_then_stored(self):
        s = self.ready()
        event = self.ping(self.orch, "review", ref=PATH_REF)
        batch = s.sync_once()
        self.assertEqual(batch.accepted, (event,))
        self.assertEqual(self.forge.checked, [(PATH_REF, ("main",))])
        self.assertEqual(self.pending().items[0].outcome.ping.verb, "review")

    def test_ping_without_a_ref_needs_no_forge(self):
        s = self.ready()
        event = self.ping(self.orch, "halt")
        self.assertEqual(s.sync_once().accepted, (event,))
        self.assertEqual(self.forge.checked, [])

    def test_forge_failures_drop_as_unresolved_or_provenance(self):
        s = self.ready()
        self.forge.fail = {PATH_REF: "provenance", COMMIT_REF: "not-found"}
        self.ping(self.orch, "review", ref=PATH_REF)
        self.ping(self.orch, "run-qa", ref=COMMIT_REF)
        batch = s.sync_once()
        self.assertEqual(batch.accepted, ())
        self.assertEqual(dict(batch.drops), {"provenance": 1, "unresolved": 1})

    def test_ping_to_another_agent_ignored_and_not_forge_checked(self):
        s = self.ready()
        self.ping(self.orch, "review", to=[self.peer], ref=PATH_REF)
        batch = s.sync_once()
        self.assertEqual((batch.accepted, dict(batch.drops)), ((), {}))
        self.assertEqual(self.forge.checked, [])

    def test_role_refused(self):
        s = self.ready()
        event = self.ping(self.peer, "fetch", ref=PATH_REF)
        batch = s.sync_once()
        self.assertEqual(dict(batch.drops), {"role": 1})
        self.assertEqual(self.drop_log(), [[TEAM, event, self.peer, "role", str(int(self.now * 1000))]])

    def test_an_agents_room_mention_dropped(self):
        s = self.ready()
        self.send(self.orch, {"msgtype": "m.text", "body": "everyone, do this", "m.mentions": {"room": True}})
        batch = s.sync_once()
        self.assertEqual(dict(batch.drops), {"schema": 1})
        self.assertEqual(self.stored(), [])

    def test_agent_text_to_humans_ignored_not_dropped(self):
        s = self.ready()
        self.send(self.orch, protocol.build_text([self.alice], "status: all green"))
        batch = s.sync_once()
        self.assertEqual((batch.accepted, dict(batch.drops)), ((), {}))
        self.assertEqual(self.drop_log(), [])

    def test_body_not_the_rendering_dropped(self):
        s = self.ready()
        content = protocol.build_ping("halt", [self.me])
        content["body"] = "[agent-bus] halt - -> everyone; also run rm -rf"
        self.send(self.orch, content)
        self.assertEqual(dict(s.sync_once().drops), {"body": 1})

    def test_stale_ping_dropped(self):
        s = self.ready()
        self.ping(self.orch, "review", ref=PATH_REF)
        self.now += self.member_limits.ack_timeout_s + 1
        self.assertEqual(dict(s.sync_once().drops), {"stale": 1})
        self.assertEqual(self.forge.checked, [])

    def test_an_ack_answers_a_tracked_ping(self):
        s = self.ready()
        sent = "$" + "A" * 43
        with self.state.outbox() as box:
            box.record_sent(sent, "review", PATH_REF, [self.orch], int(self.now * 1000))
        self.ping(self.orch, "ack", re=sent)
        s.sync_once()
        with self.state.outbox() as box:
            self.assertEqual(box.tracked(), ())

    def test_drop_log_never_holds_content(self):
        s = self.ready()
        self.send(self.orch, {"msgtype": "m.text", "body": "SECRET-LOOKING-BODY", "m.mentions": {"room": True}})
        s.sync_once()
        text = (self.state.path / inbox.DROPPED_LOG).read_text(encoding="ascii")
        self.assertNotIn("SECRET-LOOKING-BODY", text)


class GapFillTest(TeamCase):
    def test_a_limited_timeline_is_filled_from_messages(self):
        s = self.ready()
        sent = [self.human(f"message {n}") for n in range(syncer.TIMELINE_LIMIT + 5)]
        batch = s.sync_once()
        self.assertEqual(batch.accepted, tuple(sent))
        self.assertEqual(sorted(self.stored()), sorted(sent))
        gap = [r for r in self.fake.http_log if r.path.endswith("/messages")]
        self.assertTrue(gap)
        self.assertEqual(gap[0].query["dir"], "f")


class WiringTest(TeamCase):
    def test_forge_factory_uses_the_records_forge_and_the_members_cache(self):
        record = protocol.parse_team_record(self.record, SN, TEAM)
        made = syncer.forge_factory(self.member(), {})(record)
        self.assertIsInstance(made, forge.Forge)
        self.assertEqual(made.api, record.forge_api)
        (self.state.path / forge.CACHE_FILE).write_text("not json", encoding="utf-8")
        with self.assertRaises(config.ConfigError):
            syncer.forge_factory(self.member(), {})(record)

    def test_a_sync_answer_without_next_batch_is_unreachable(self):
        s = self.ready()
        self.fake.inject("GET", r"/sync$", 200, {"rooms": {}})
        with self.assertRaises(matrix.Unreachable):
            s.sync_once()


class StatusTest(TeamCase):
    def test_status_written_under_the_members_own_user_id(self):
        s = self.ready()
        until = int(self.now * 1000) + 60_000
        s.publish_status(until)
        event = self.fake.rooms[self.room].state[(protocol.EVENT_STATUS, self.me)]
        self.assertEqual(event.content, {"v": 1, "state": "listening", "until": until})

    def test_a_foreign_key_status_ignored(self):
        s = self.ready()
        until = int(self.now * 1000) + 60_000
        self.put_state(self.orch, protocol.EVENT_STATUS, self.orch, {"v": 1, "state": "listening", "until": until})
        self.put_state(self.orch, protocol.EVENT_STATUS, "", {"v": 1, "state": "listening", "until": until + 1})
        self.put_state(self.peer, protocol.EVENT_STATUS, "peer", {"v": 1, "state": "listening", "until": until + 2})
        s.sync_once()
        self.assertEqual(s.statuses, {self.orch: until})

    def test_statuses_from_the_state_of_a_new_process(self):
        until = int(self.now * 1000) + 60_000
        self.put_state(self.orch, protocol.EVENT_STATUS, self.orch, {"v": 1, "state": "listening", "until": until})
        s = self.ready()
        self.assertEqual(s.statuses, {self.orch: until})

    def test_status_needs_a_trusted_room(self):
        s = self.syncer()
        with self.assertRaises(syncer.RoomUntrusted):
            s.publish_status(int(self.now * 1000))
        self.assertNotIn((protocol.EVENT_STATUS, self.me), self.fake.rooms[self.room].state)


if __name__ == "__main__":
    unittest.main()

"""Unit tests for helpers/agent_bus/admin.py, against the fake Tuwunel admin API (U08).

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_admin

DESIGN.md sections 3.4 (bootstrap), 3.7 (secrets) and 4 (accounts, the team room, the
commands); docs/agent-bus-protocol.md §8 (team record, power levels) and §12 (bundle).

Most calls go to `FakeAdminHomeserver`, whose answers U08 checked against what Tuwunel
1.9.3 recorded in probe H4. Five calls were never recorded, so the fake refuses them
(`Unmodelled`): the kick, `PUT v2/users` with `deactivated` or `locked`, and the device
list. `Stubbed` answers those itself and records them, so these tests pin the request
the tool sends, not Tuwunel's answer to it.
"""

from __future__ import annotations

import copy
import io
import json
import os
import pathlib
import re
import secrets
import stat
import sys
import tarfile
import tempfile
import unittest
import urllib.parse

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import admin, registry, teamfile
from helpers.pingbus import config, protocol
from tests.helpers.pingbus import fake_admin_api, fake_client_api

SN = fake_client_api.DEFAULT_SERVER_NAME
ADMIN_ID = f"@admin:{SN}"
ALICE = f"@alice:{SN}"
BOB = f"@bob:{SN}"
_USER_PATH = re.compile(r"/_synapse/admin/v2/users/([^/]+)")
_DEVICES_PATH = re.compile(r"/_synapse/admin/v2/users/([^/]+)/devices")
_KICK_PATH = re.compile(r"/_matrix/client/v3/rooms/([^/]+)/kick")


def team_data(**overrides: object) -> dict:
    data = {
        "team": "team-a",
        "port": 8448,
        "listen": ["192.0.2.10"],
        "allow_from": ["192.0.2.0/24"],
        "humans": ["alice"],
        "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
        "path_prefixes": ["CLAUDE/Plan/", "docs/"],
        "forge_api": "https://api.github.com",
    }
    data.update(overrides)
    return data


class Stubbed:
    """The fake, plus recorded answers for the calls H4 did not record (module docstring)."""

    def __init__(self, fake: fake_admin_api.FakeAdminHomeserver) -> None:
        self.fake = fake
        self.calls: list[tuple[str, str, dict, object]] = []
        self.kicked: list[tuple[str, str]] = []
        self.deactivated: set[str] = set()
        self.locked: dict[str, bool] = {}
        self.failing_kicks = 0

    def _is_admin(self, token: str | None) -> bool:
        status, who = self.fake.request("GET", "/_matrix/client/v3/account/whoami", token=token)
        return status == 200 and self.fake.users[who["user_id"]].admin

    def __call__(self, method: str, path: str, query: dict | None = None, body: object = None,
                 token: str | None = None) -> tuple[int, dict]:
        self.calls.append((method, path, dict(query or {}), copy.deepcopy(body)))
        plain = urllib.parse.unquote(path)
        kick = _KICK_PATH.fullmatch(plain)
        if method == "POST" and kick:
            if not self._is_admin(token):
                return 403, {"errcode": "M_FORBIDDEN", "error": "stub: admin only"}
            if self.failing_kicks:
                self.failing_kicks -= 1
                return 500, {"errcode": "M_UNKNOWN", "error": "stub: injected kick failure"}
            self.kicked.append((kick.group(1), body["user_id"]))
            with self.fake._cond:
                self.fake._append(self.fake.rooms[kick.group(1)], fake_client_api.MEMBER, ADMIN_ID,
                                  {"membership": "leave", "reason": body.get("reason")}, body["user_id"])
            return 200, {}
        devices = _DEVICES_PATH.fullmatch(plain)
        if method == "GET" and devices and self._is_admin(token):
            return 200, {"devices": [
                {"device_id": "PHONE1", "display_name": "evil\tname", "last_seen_ip": "192.0.2.9",
                 "last_seen_ts": 1791000000000, "user_id": devices.group(1)},
                {"device_id": "DESK2", "display_name": None, "last_seen_ip": None,
                 "last_seen_ts": None, "user_id": devices.group(1)}], "total": 2}
        user = _USER_PATH.fullmatch(plain)
        if user and method == "PUT" and isinstance(body, dict) and {"deactivated", "locked"} & set(body):
            if not self._is_admin(token):
                return 403, {"errcode": "M_FORBIDDEN", "error": "stub: admin only"}
            if set(body) - {"deactivated", "locked"}:
                return 400, {"errcode": "M_UNKNOWN", "error": "stub: mixed body"}
            if body.get("deactivated") is True:
                self.deactivated.add(user.group(1))
                self.fake.logout_all(user.group(1))
            if "locked" in body:
                self.locked[user.group(1)] = body["locked"]
            status, obj = self.fake.request("GET", path, token=token)
            return status, obj
        status, obj = self.fake.request(method, path, query, body, token)
        if user and method == "GET" and status == 200 and user.group(1) in self.deactivated:
            obj["deactivated"] = True
        return status, obj

    def writes(self) -> list[tuple[str, str, dict, object]]:
        return [c for c in self.calls if c[0] != "GET"]


class AdminTestCase(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = pathlib.Path(tmp.name)
        self.team_dir = self.root / "team-a"
        self.team_dir.mkdir(mode=0o700)
        (self.team_dir / "secrets").mkdir(mode=0o700)
        self.shared_secret = secrets.token_hex(64)
        admin.write_private(self.team_dir / "secrets" / "registration_shared_secret", self.shared_secret)
        self.write_team(team_data())
        self.fake = fake_admin_api.FakeAdminHomeserver(shared_secret=self.shared_secret)
        self.transport = Stubbed(self.fake)

    def write_team(self, data: dict) -> None:
        tf = teamfile.parse_team_file(data)
        (self.team_dir / "team.json").write_text(teamfile.dump_team_file(tf), encoding="utf-8")

    def ctx(self) -> admin.Team:
        return admin.load_team(self.root, "team-a")

    def bootstrap(self) -> list[str]:
        return admin.bootstrap(self.ctx(), self.transport)

    def admin_token(self) -> str:
        return (self.team_dir / "secrets" / "admin.token").read_text(encoding="utf-8")

    def room(self) -> str:
        return (self.team_dir / "room_id").read_text(encoding="utf-8")

    def state(self, event_type: str, key: str = "", event: bool = False) -> tuple[int, dict]:
        room = urllib.parse.quote(self.room(), safe="")
        return self.fake.request("GET", f"/_matrix/client/v3/rooms/{room}/state/{event_type}/{urllib.parse.quote(key, safe='')}",
                                 {"format": "event"} if event else None, token=self.admin_token())

    def membership(self, user_id: str) -> str | None:
        status, content = self.state("m.room.member", user_id)
        return content.get("membership") if status == 200 else None

    def whoami(self, token: str) -> tuple[int, dict]:
        return self.fake.request("GET", "/_matrix/client/v3/account/whoami", token=token)

    def team_files_text(self) -> str:
        return "".join(p.read_text(encoding="utf-8", errors="replace")
                       for p in self.team_dir.rglob("*") if p.is_file())

    def add_member(self, **kwargs: object) -> tarfile.TarFile:
        args = {"repo": "myrepo", "host": "workstation", "type_": "podman", "role": "worker",
                "address": "192.0.2.10", "human_text": True}
        args.update(kwargs)
        bundle = admin.add_member(self.ctx(), self.transport, **args)
        self.assertEqual(json.loads(read_member(tarfile.open(fileobj=io.BytesIO(bundle.tar)), "member.json"))["user_id"],
                         f"@{bundle.handle}:{SN}")
        return tarfile.open(fileobj=io.BytesIO(bundle.tar))


def read_member(tar: tarfile.TarFile, name: str) -> bytes:
    return tar.extractfile(name).read()


class MacTest(unittest.TestCase):
    def test_equals_the_fakes_and_synapses_layout(self) -> None:
        for is_admin in (True, False):
            self.assertEqual(
                admin.registration_mac("s3cret", "n0nce", "admin", "pw", is_admin),
                fake_admin_api.registration_mac("s3cret", "n0nce", "admin", "pw", is_admin))


class BootstrapTest(AdminTestCase):
    def test_fresh_bootstrap(self) -> None:
        changes = self.bootstrap()
        self.assertTrue(changes)
        for name in ("admin.token", "admin.password"):
            info = (self.team_dir / "secrets" / name).stat()
            self.assertEqual(stat.S_IMODE(info.st_mode), 0o600, name)
        self.assertEqual(self.whoami(self.admin_token())[1]["user_id"], ADMIN_ID)
        admins = [u for u in self.fake.users.values() if u.admin]
        self.assertEqual([u.user_id for u in admins], [ADMIN_ID])
        self.assertIn(ALICE, self.fake.users)
        self.assertEqual(self.membership(ALICE), "invite")

    def test_room_is_exactly_section_8(self) -> None:
        self.bootstrap()
        status, create = self.state("m.room.create", event=True)
        self.assertEqual(status, 200)
        self.assertEqual(create["sender"], ADMIN_ID)
        self.assertEqual(create["content"]["room_version"], "12")
        self.assertNotIn("additional_creators", create["content"])
        _, levels = self.state("m.room.power_levels")
        protocol.check_power_levels(levels, [ALICE])
        _, record_event = self.state(protocol.EVENT_TEAM, event=True)
        record = protocol.parse_team_event(record_event, ADMIN_ID, SN, "team-a")
        self.assertEqual(record.humans, frozenset({ALICE}))
        self.assertEqual(record.roles, {})
        self.assertEqual(record.repos, {"example-org/myrepo": ("main",)})
        self.assertEqual(record.path_prefixes, ("CLAUDE/Plan/", "docs/"))
        _, rules = self.state("m.room.join_rules")
        self.assertEqual(rules, {"join_rule": "invite"})
        _, name = self.state("m.room.name")
        self.assertEqual(name, {"name": "team-a"})
        self.assertTrue(protocol.is_room_id(self.room()))

    def test_human_account_has_no_admin_key_and_a_discarded_password(self) -> None:
        self.bootstrap()
        puts = [c for c in self.transport.calls
                if c[0] == "PUT" and urllib.parse.unquote(c[1]) == f"/_synapse/admin/v2/users/{ALICE}"]
        self.assertEqual(len(puts), 1)
        body = puts[0][3]
        self.assertNotIn("admin", body)
        self.assertEqual(body["displayname"], "alice")
        password = self.fake.users[ALICE].password
        self.assertGreaterEqual(len(password), 32)
        self.assertNotIn(password, self.team_files_text())

    def test_second_run_changes_nothing(self) -> None:
        self.bootstrap()
        before = len(self.transport.calls)
        self.assertEqual(self.bootstrap(), [])
        self.assertEqual([c for c in self.transport.calls[before:] if c[0] != "GET"], [])

    def test_refuses_a_second_server_admin(self) -> None:
        self.bootstrap()
        self.fake.add_user("intruder", admin=True)
        with self.assertRaisesRegex(admin.AdminError, "server admin"):
            self.bootstrap()

    def test_wrong_shared_secret_fails_and_is_not_echoed(self) -> None:
        wrong = secrets.token_hex(64)
        path = self.team_dir / "secrets" / "registration_shared_secret"
        path.unlink()
        admin.write_private(path, wrong)
        with self.assertRaises(admin.AdminError) as caught:
            self.bootstrap()
        self.assertNotIn(wrong, str(caught.exception))
        self.assertNotIn(self.shared_secret, str(caught.exception))
        self.assertFalse((self.team_dir / "secrets" / "admin.token").exists())

    def test_recovers_from_a_run_that_lost_the_token(self) -> None:
        self.bootstrap()
        (self.team_dir / "secrets" / "admin.token").unlink()
        self.assertIn("admin token", " ".join(self.bootstrap()))
        self.assertEqual(self.whoami(self.admin_token())[1]["user_id"], ADMIN_ID)

    def test_rejected_admin_token_fails_without_echo(self) -> None:
        self.bootstrap()
        token = self.admin_token()
        self.fake.logout_all(ADMIN_ID)
        with self.assertRaises(admin.AdminError) as caught:
            self.bootstrap()
        self.assertNotIn(token, str(caught.exception))

    def test_loose_secret_file_is_refused(self) -> None:
        os.chmod(self.team_dir / "secrets" / "registration_shared_secret", 0o640)
        with self.assertRaisesRegex(admin.AdminError, "0600"):
            self.bootstrap()

    def test_server_name_mismatch_is_refused(self) -> None:
        self.write_team(team_data(server_name="other.internal"))
        with self.assertRaisesRegex(admin.AdminError, "server_name"):
            self.bootstrap()

    def test_team_file_change_adds_and_removes_humans(self) -> None:
        self.bootstrap()
        self.write_team(team_data(humans=["bob"], path_prefixes=["docs/"]))
        changes = self.bootstrap()
        self.assertTrue(changes)
        self.assertIn(BOB, self.fake.users)
        self.assertEqual(self.membership(BOB), "invite")
        _, levels = self.state("m.room.power_levels")
        protocol.check_power_levels(levels, [BOB])
        _, record_event = self.state(protocol.EVENT_TEAM, event=True)
        record = protocol.parse_team_event(record_event, ADMIN_ID, SN, "team-a")
        self.assertEqual(record.humans, frozenset({BOB}))
        self.assertEqual(record.path_prefixes, ("docs/",))
        self.assertEqual(self.transport.deactivated, {ALICE})
        self.assertEqual(self.transport.kicked, [(self.room(), ALICE)])
        self.assertEqual(self.bootstrap(), [])

    def test_a_failed_removal_is_retried_on_the_next_run(self) -> None:
        self.bootstrap()
        self.write_team(team_data(humans=["bob"]))
        self.transport.failing_kicks = 1
        with self.assertRaises(admin.AdminError):
            self.bootstrap()
        self.assertEqual(self.transport.deactivated, set())
        self.bootstrap()
        self.assertEqual(self.transport.kicked, [(self.room(), ALICE)])
        self.assertEqual(self.transport.deactivated, {ALICE})
        _, record_event = self.state(protocol.EVENT_TEAM, event=True)
        record = protocol.parse_team_event(record_event, ADMIN_ID, SN, "team-a")
        self.assertEqual(record.humans, frozenset({BOB}))
        self.assertEqual(self.bootstrap(), [])

    def test_a_deactivated_listed_human_is_refused(self) -> None:
        self.bootstrap()
        self.transport.deactivated.add(ALICE)
        with self.assertRaisesRegex(admin.AdminError, "deactivated"):
            self.bootstrap()

    def test_missing_team_dir_is_refused(self) -> None:
        with self.assertRaisesRegex(admin.AdminError, "team-b"):
            admin.load_team(self.root, "team-b")

    def test_bad_team_name_is_refused(self) -> None:
        with self.assertRaises(admin.AdminError):
            admin.load_team(self.root, "../etc")


class AddMemberTest(AdminTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.bootstrap()
        self.handle = "myrepo.1+workstation.podman"
        self.user_id = f"@{self.handle}:{SN}"

    def test_bundle_tar(self) -> None:
        tar = self.add_member()
        self.assertEqual(sorted(tar.getnames()), ["README", "member.json", "token"])
        for info in tar.getmembers():
            self.assertTrue(info.isfile())
            self.assertEqual(info.mode, 0o600)
        member = json.loads(read_member(tar, "member.json"))
        self.assertEqual(member, {
            "protocol": 1, "team": "team-a", "user_id": self.user_id, "server_name": SN,
            "base_url": "http://192.0.2.10:8448", "plain_http_hosts": ["192.0.2.10"],
            "token_file": "token", "admin": ADMIN_ID, "room": self.room(), "human_text": True})
        token = read_member(tar, "token").decode()
        self.assertEqual(self.whoami(token)[1]["user_id"], self.user_id)
        self.assertIn("podman", read_member(tar, "README").decode())
        self.assertNotIn(token, self.team_files_text())

    def test_bundle_is_accepted_by_pingbus(self) -> None:
        tar = self.add_member()
        home = self.root / "home"
        bundle = home / "team-a"
        bundle.mkdir(parents=True, mode=0o700)
        for name in tar.getnames():
            path = bundle / name
            path.write_bytes(read_member(tar, name))
            os.chmod(path, 0o600)
        member = config.load_bundle(home, "team-a")
        self.assertEqual(member.handle, self.handle)
        self.assertTrue(member.human_text)

    def test_account_has_no_admin_key(self) -> None:
        self.add_member()
        puts = [c for c in self.transport.calls
                if c[0] == "PUT" and urllib.parse.unquote(c[1]).endswith(self.user_id)]
        self.assertEqual(len(puts), 1)
        self.assertNotIn("admin", puts[0][3])
        self.assertEqual(puts[0][3]["displayname"], self.handle)
        self.assertNotIn(self.fake.users[self.user_id].password, self.team_files_text())

    def test_registry_record_and_invite(self) -> None:
        self.add_member(role="orchestrator")
        reg = registry.load_registry(self.team_dir / "registry.json", "team-a")
        self.assertEqual(dict(reg.members), {self.handle: "orchestrator"})
        _, record_event = self.state(protocol.EVENT_TEAM, event=True)
        record = protocol.parse_team_event(record_event, ADMIN_ID, SN, "team-a")
        self.assertEqual(record.roles, {self.user_id: "orchestrator"})
        self.assertEqual(self.membership(self.user_id), "invite")

    def test_no_human_text(self) -> None:
        member = json.loads(read_member(self.add_member(human_text=False), "member.json"))
        self.assertIs(member["human_text"], False)

    def test_seat_numbers_increase(self) -> None:
        self.add_member()
        second = json.loads(read_member(self.add_member(), "member.json"))
        self.assertEqual(second["user_id"], f"@myrepo.2+workstation.podman:{SN}")

    def test_repo_is_normalised(self) -> None:
        member = json.loads(read_member(self.add_member(repo="My.Repo"), "member.json"))
        self.assertEqual(member["user_id"], f"@my-repo.1+workstation.podman:{SN}")

    def test_addresses(self) -> None:
        member = json.loads(read_member(self.add_member(address="127.0.0.1", type_="host"), "member.json"))
        self.assertEqual(member["base_url"], "http://127.0.0.1:8448")
        # The address pasta gives host.containers.internal is its own choice and may change
        # (DESIGN.md section 5.3), so a podman member gets no exception for it.
        for address, type_ in (("203.0.113.5", "podman"), ("169.254.1.2", "podman"),
                               ("169.254.1.2", "lxc"), ("localhost", "host"),
                               ("192.0.2.010", "podman"), ("0.0.0.0", "podman")):
            with self.subTest(address=address), self.assertRaises(admin.AdminError):
                self.add_member(address=address, type_=type_)

    def test_ipv6_address(self) -> None:
        self.write_team(team_data(listen=["2001:db8::10"], allow_from=["2001:db8::/64"]))
        member = json.loads(read_member(self.add_member(address="2001:db8::10"), "member.json"))
        self.assertEqual(member["base_url"], "http://[2001:db8::10]:8448")

    def test_failed_account_creation_rolls_back_the_member_but_not_the_counter(self) -> None:
        self.fake.inject("PUT", r"/_synapse/admin/v2/users/", 500, {"errcode": "M_UNKNOWN", "error": "boom"})
        with self.assertRaises(admin.AdminError):
            self.add_member()
        reg = registry.load_registry(self.team_dir / "registry.json", "team-a")
        self.assertEqual(dict(reg.members), {})
        member = json.loads(read_member(self.add_member(), "member.json"))
        self.assertEqual(member["user_id"], f"@myrepo.2+workstation.podman:{SN}")

    def test_needs_bootstrap(self) -> None:
        (self.team_dir / "room_id").unlink()
        with self.assertRaisesRegex(admin.AdminError, "bootstrap"):
            self.add_member()


class MemberCommandsTest(AdminTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.bootstrap()
        tar = self.add_member()
        self.token = read_member(tar, "token").decode()
        self.handle = "myrepo.1+workstation.podman"
        self.user_id = f"@{self.handle}:{SN}"

    def record(self) -> protocol.TeamRecord:
        _, record_event = self.state(protocol.EVENT_TEAM, event=True)
        return protocol.parse_team_event(record_event, ADMIN_ID, SN, "team-a")

    def test_set_role(self) -> None:
        self.assertTrue(admin.set_role(self.ctx(), self.transport, self.handle, "orchestrator"))
        self.assertEqual(self.record().roles, {self.user_id: "orchestrator"})
        reg = registry.load_registry(self.team_dir / "registry.json", "team-a")
        self.assertEqual(dict(reg.members), {self.handle: "orchestrator"})
        self.assertEqual(admin.set_role(self.ctx(), self.transport, self.handle, "orchestrator"), [])

    def test_set_role_refuses_unknown(self) -> None:
        with self.assertRaises(admin.AdminError):
            admin.set_role(self.ctx(), self.transport, "other.1+workstation.podman", "worker")
        with self.assertRaises(admin.AdminError):
            admin.set_role(self.ctx(), self.transport, self.handle, "boss")

    def test_remove_member(self) -> None:
        changes = admin.remove_member(self.ctx(), self.transport, self.handle)
        self.assertTrue(changes)
        self.assertEqual(self.record().roles, {})
        reg = registry.load_registry(self.team_dir / "registry.json", "team-a")
        self.assertEqual(dict(reg.members), {})
        self.assertEqual(reg.counters["myrepo+workstation.podman"], 1)
        self.assertEqual(self.transport.kicked, [(self.room(), self.user_id)])
        self.assertEqual(self.transport.deactivated, {self.user_id})
        self.assertEqual(self.whoami(self.token)[0], 401)
        self.assertEqual(admin.remove_member(self.ctx(), self.transport, self.handle), [])

    def test_remove_refuses_a_handle_never_issued(self) -> None:
        with self.assertRaises(admin.AdminError):
            admin.remove_member(self.ctx(), self.transport, "myrepo.7+workstation.podman")
        with self.assertRaises(admin.AdminError):
            admin.remove_member(self.ctx(), self.transport, "alice")

    def test_rotate_token(self) -> None:
        tar = tarfile.open(fileobj=io.BytesIO(admin.rotate_token(self.ctx(), self.transport, self.handle)))
        self.assertEqual(tar.getnames(), ["token"])
        new = read_member(tar, "token").decode()
        self.assertEqual(self.whoami(self.token)[0], 401)
        self.assertEqual(self.whoami(new)[1]["user_id"], self.user_id)
        self.assertNotIn(new, self.team_files_text())

    def test_rotate_refuses_a_removed_member(self) -> None:
        admin.remove_member(self.ctx(), self.transport, self.handle)
        with self.assertRaises(admin.AdminError):
            admin.rotate_token(self.ctx(), self.transport, self.handle)

    def test_list(self) -> None:
        lines = admin.list_members(self.ctx(), self.transport)
        self.assertEqual(lines, [
            "HUMAN\talice\tinvite",
            f"MEMBER\t{self.handle}\tworker\tinvite\tactive",
        ])

    def test_rotate_refuses_a_parked_member(self) -> None:
        admin.park_member(self.ctx(), self.transport, self.handle)
        with self.assertRaisesRegex(admin.AdminError, "parked"):
            admin.rotate_token(self.ctx(), self.transport, self.handle)


class SeatTest(AdminTestCase):
    """`add-member --seat` and `park-member` (DESIGN.md section 5.5, "Reuse")."""

    def setUp(self) -> None:
        super().setUp()
        self.bootstrap()
        self.handle = "myrepo.dev+local.podman"
        self.user_id = f"@{self.handle}:{SN}"

    def seat(self, seat: str = "dev", **kwargs: object) -> admin.Bundle:
        args = {"repo": "myrepo", "host": "local", "type_": "podman", "role": "worker",
                "address": "192.0.2.10", "human_text": True, "seat": seat}
        args.update(kwargs)
        return admin.add_member(self.ctx(), self.transport, **args)

    def token(self, bundle: admin.Bundle) -> str:
        return read_member(tarfile.open(fileobj=io.BytesIO(bundle.tar)), "token").decode()

    def registry(self) -> registry.Registry:
        return registry.load_registry(self.team_dir / "registry.json", "team-a")

    def record(self) -> protocol.TeamRecord:
        _, record_event = self.state(protocol.EVENT_TEAM, event=True)
        return protocol.parse_team_event(record_event, ADMIN_ID, SN, "team-a")

    def account_creations(self) -> list:
        return [c for c in self.transport.calls if c[0] == "PUT" and "password" in (c[3] or {})
                and "logout_devices" not in c[3]]

    def test_seat_builds_the_handle(self) -> None:
        bundle = self.seat("dev1")
        self.assertEqual((bundle.handle, bundle.role, bundle.returned), ("myrepo.dev1+local.podman", "worker", False))
        member = json.loads(read_member(tarfile.open(fileobj=io.BytesIO(bundle.tar)), "member.json"))
        self.assertEqual(member["user_id"], f"@myrepo.dev1+local.podman:{SN}")
        self.assertEqual(self.whoami(self.token(bundle))[1]["user_id"], member["user_id"])
        self.assertEqual(dict(self.registry().counters), {})
        self.assertEqual(self.record().roles, {member["user_id"]: "worker"})

    def test_a_numbered_seat_moves_the_counter(self) -> None:
        self.assertEqual(self.seat("3", host="workstation").handle, "myrepo.3+workstation.podman")
        member = json.loads(read_member(self.add_member(), "member.json"))
        self.assertEqual(member["user_id"], f"@myrepo.4+workstation.podman:{SN}")

    def test_park_revokes_the_token_and_keeps_role_and_room(self) -> None:
        bundle = self.seat()
        admin.set_role(self.ctx(), self.transport, self.handle, "orchestrator")
        changes = admin.park_member(self.ctx(), self.transport, self.handle)
        self.assertIn(f"parked {self.handle}", changes)
        self.assertEqual(self.whoami(self.token(bundle))[0], 401)
        self.assertEqual(self.registry().parked, frozenset({self.handle}))
        self.assertEqual(self.record().roles, {self.user_id: "orchestrator"})
        self.assertEqual(self.membership(self.user_id), "invite")
        self.assertEqual(self.transport.kicked, [])
        self.assertEqual(self.transport.deactivated, set())
        self.assertNotIn(self.fake.users[self.user_id].password, self.team_files_text())
        lines = admin.list_members(self.ctx(), self.transport)
        self.assertIn(f"MEMBER\t{self.handle}\torchestrator\tinvite\tparked", lines)
        # Parking again revokes again and changes nothing in the registry.
        self.assertNotIn(f"parked {self.handle}", admin.park_member(self.ctx(), self.transport, self.handle))

    def test_parked_seat_returns_with_the_same_account_and_role_and_a_new_token(self) -> None:
        old = self.token(self.seat())
        admin.set_role(self.ctx(), self.transport, self.handle, "orchestrator")
        admin.park_member(self.ctx(), self.transport, self.handle)
        creations = len(self.account_creations())
        bundle = self.seat(role="worker")
        self.assertEqual((bundle.handle, bundle.role, bundle.returned), (self.handle, "orchestrator", True))
        new = self.token(bundle)
        self.assertNotEqual(new, old)
        self.assertEqual(self.whoami(new)[1]["user_id"], self.user_id)
        self.assertEqual(self.whoami(old)[0], 401)
        self.assertEqual(len(self.account_creations()), creations)
        self.assertEqual(self.registry().parked, frozenset())
        self.assertEqual(dict(self.registry().members), {self.handle: "orchestrator"})
        self.assertEqual(self.record().roles, {self.user_id: "orchestrator"})
        self.assertEqual(self.membership(self.user_id), "invite")
        self.assertNotIn(new, self.team_files_text())

    def test_a_current_unparked_seat_is_refused_and_no_account_is_created(self) -> None:
        token = self.token(self.seat())
        calls = len(self.transport.calls)
        with self.assertRaisesRegex(admin.AdminError, "current member"):
            self.seat()
        self.assertEqual([c for c in self.transport.calls[calls:] if c[0] != "GET"], [])
        self.assertEqual(self.whoami(token)[1]["user_id"], self.user_id)

    def test_a_deactivated_seat_is_refused_and_no_account_is_created(self) -> None:
        self.seat()
        admin.remove_member(self.ctx(), self.transport, self.handle)
        creations = len(self.account_creations())
        with self.assertRaisesRegex(admin.AdminError, "already has an account"):
            self.seat()
        self.assertEqual(len(self.account_creations()), creations)
        self.assertEqual(dict(self.registry().members), {})

    def test_a_returning_seat_with_a_deactivated_account_is_refused_and_stays_parked(self) -> None:
        self.seat()
        admin.park_member(self.ctx(), self.transport, self.handle)
        self.transport.deactivated.add(self.user_id)
        with self.assertRaisesRegex(admin.AdminError, "no active account"):
            self.seat()
        self.assertEqual(self.registry().parked, frozenset({self.handle}))

    def test_a_failed_mint_on_return_leaves_the_seat_parked(self) -> None:
        self.seat()
        admin.park_member(self.ctx(), self.transport, self.handle)
        self.fake.inject("POST", r"/_synapse/admin/v1/users/.*/login", 500, {"errcode": "M_UNKNOWN", "error": "boom"})
        with self.assertRaises(admin.AdminError):
            self.seat()
        self.assertEqual(self.registry().parked, frozenset({self.handle}))

    def test_park_refusals(self) -> None:
        with self.assertRaisesRegex(admin.AdminError, "not a member"):
            admin.park_member(self.ctx(), self.transport, self.handle)
        self.seat()
        admin.remove_member(self.ctx(), self.transport, self.handle)
        with self.assertRaises(admin.AdminError):
            admin.park_member(self.ctx(), self.transport, self.handle)


class HumanCommandsTest(AdminTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.bootstrap()

    def login(self, password: str) -> tuple[int, dict]:
        return self.fake.request("POST", "/_matrix/client/v3/login", body={
            "type": "m.login.password", "identifier": {"type": "m.id.user", "user": "alice"},
            "password": password})

    def test_password_set_once_and_not_stored(self) -> None:
        password = admin.human_password(self.ctx(), self.transport, "alice")
        self.assertEqual(len(password), 32)
        self.assertRegex(password, r"[A-Za-z0-9]{32}")
        status, session = self.login(password)
        self.assertEqual(status, 200)
        self.assertNotIn(password, self.team_files_text())
        again = admin.human_password(self.ctx(), self.transport, "alice")
        self.assertNotEqual(again, password)
        self.assertEqual(self.whoami(session["access_token"])[0], 401)
        self.assertEqual(self.login(password)[0], 403)

    def test_refuses_a_name_not_in_the_team(self) -> None:
        for name in ("bob", "admin", "myrepo.1+workstation.podman", "../x"):
            with self.subTest(name=name), self.assertRaises(admin.AdminError):
                admin.human_password(self.ctx(), self.transport, name)

    def test_logout_all(self) -> None:
        password = admin.human_password(self.ctx(), self.transport, "alice")
        _, session = self.login(password)
        admin.human_logout_all(self.ctx(), self.transport, "alice")
        self.assertEqual(self.whoami(session["access_token"])[0], 401)
        self.assertEqual(self.login(password)[0], 403)

    def test_lock_and_unlock(self) -> None:
        admin.human_lock(self.ctx(), self.transport, "alice", True)
        self.assertEqual(self.transport.locked, {ALICE: True})
        admin.human_lock(self.ctx(), self.transport, "alice", False)
        self.assertEqual(self.transport.locked, {ALICE: False})
        locks = [c[3] for c in self.transport.calls if c[0] == "PUT" and c[3] in ({"locked": True}, {"locked": False})]
        self.assertEqual(locks, [{"locked": True}, {"locked": False}])

    def test_devices_print_ids_and_times_only(self) -> None:
        self.assertEqual(admin.human_devices(self.ctx(), self.transport, "alice"),
                         ["DEVICE\tPHONE1\t1791000000000", "DEVICE\tDESK2\t-"])


class BaseUrlTest(unittest.TestCase):
    def test_loopback_only(self) -> None:
        self.assertEqual(admin.loopback_url(8448), "http://127.0.0.1:8448")
        admin.check_base_url("http://127.0.0.1:8448")
        for bad in ("http://192.0.2.1:8448", "https://127.0.0.1:8448", "http://localhost:8448",
                    "http://127.0.0.1:8448/x", "http://user@127.0.0.1:8448", "http://127.0.0.2:8448",
                    "http://[::1]:8448", "http://127.0.0.1"):
            with self.subTest(url=bad), self.assertRaises(admin.AdminError):
                admin.check_base_url(bad)
            with self.subTest(url=bad), self.assertRaises(admin.AdminError):
                admin.http_transport(bad)


class HttpTransportTest(AdminTestCase):
    def test_bootstrap_over_real_http_ignores_proxies(self) -> None:
        with fake_client_api.serve(self.fake) as url:
            saved = {k: os.environ.get(k) for k in ("http_proxy", "HTTP_PROXY", "no_proxy", "NO_PROXY")}
            os.environ.update({"http_proxy": "http://192.0.2.99:9", "HTTP_PROXY": "http://192.0.2.99:9"})
            os.environ.pop("no_proxy", None)
            os.environ.pop("NO_PROXY", None)
            try:
                transport = admin.http_transport(url)
                admin.bootstrap(self.ctx(), transport)
                status, body = transport("GET", "/_matrix/client/v3/account/whoami", None, None, "nope")
            finally:
                for key, value in saved.items():
                    if value is None:
                        os.environ.pop(key, None)
                    else:
                        os.environ[key] = value
        self.assertEqual(status, 401)
        self.assertEqual(body["errcode"], "M_UNKNOWN_TOKEN")
        self.assertIn(ALICE, self.fake.users)
        token = self.admin_token()
        for record in self.fake.http_log:
            self.assertNotIn(token, record.path)
            self.assertNotIn(token, json.dumps(record.query))

    def test_unreachable(self) -> None:
        transport = admin.http_transport("http://127.0.0.1:9")
        with self.assertRaisesRegex(admin.Unreachable, "unreachable") as caught:
            transport("GET", "/_matrix/client/versions", None, None, "secret-token")
        self.assertNotIn("secret-token", str(caught.exception))


class WritePrivateTest(unittest.TestCase):
    def test_atomic_0600_and_never_overwrites(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "f"
            admin.write_private(path, "abc")
            self.assertEqual(path.read_text(), "abc")
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            admin.write_private(path, "def", replace=True)
            self.assertEqual(path.read_text(), "def")
            with self.assertRaises(FileExistsError):
                admin.write_private(path, "ghi")


if __name__ == "__main__":
    unittest.main()

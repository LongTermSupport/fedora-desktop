"""Unit tests for helpers/agent_bus_lxc/members.py: existing LXC containers joined to agent
teams by play-agent-bus.yml (`agent_bus_lxc_members`).

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus_lxc.test_members

Spec: files/opt/claude-yolo/optional/agent-bus/README.lxc, whose six steps the helper runs.
The container is replaced by a fake whose root and agent commands run the helper's guest
scripts here, against a temporary directory (so the kit, bundle and env file are really
written and read back); `suggest-handle`, `config check`, the source address and `getent`
are answered by the fake. The homeserver host's `agent-bus` is a recorder that keeps a
registry and hands back a bundle tar, as the root wrapper's `add-member --out=-` does.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest
from collections.abc import Sequence
from unittest import mock

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.agent_bus import teamfile
from helpers.agent_bus_lxc import members

TEAM = "team-a"
OTHER_TEAM = "team-b"
BUS_IP = "192.0.2.10"
PORT = 8448
#: The LXC bridge's network, and the container's address on it (documentation ranges).
SOURCE = "198.51.100.5"
BRIDGE_CIDR = "198.51.100.0/24"
ELSEWHERE_CIDR = "203.0.113.0/24"
IP_JSON = json.dumps([
    {"ifname": "lo", "addr_info": [{"family": "inet", "local": "127.0.0.1", "prefixlen": 8}]},
    {"ifname": "agentbus0", "addr_info": [{"family": "inet", "local": BUS_IP, "prefixlen": 32}]},
    {"ifname": "lxcbr0", "addr_info": [{"family": "inet", "local": "198.51.100.1", "prefixlen": 24}]},
])
SUGGEST = "--repo=proj --host=box --type=lxc"
HANDLE = "proj.devbox+box.lxc"
CHECKOUT = "/srv/proj"
AGENT_SCRIPTS = (members.OBSERVE_PY, members.PLACE_PY, members.REMOVE_PY, members.WRITE_ENV_PY)
ROOT_SCRIPTS = (members.KIT_SYNC_PY, members.SOURCE_PY)


def entry(**overrides: object) -> dict:
    base = {"team": TEAM, "container": "devbox", "user": "agent", "host": "box",
            "checkout": CHECKOUT}
    base.update(overrides)
    return {key: value for key, value in base.items() if value is not None}


def team_file(allow_from: Sequence[str] = (BRIDGE_CIDR,), team: str = TEAM) -> teamfile.TeamFile:
    return teamfile.TeamFile(team=team, state="present", server_name=f"{team}.agent-bus.internal",
                             port=PORT, listen=(BUS_IP,), allow_from=tuple(allow_from), humans=("alice",),
                             org="example-org", repos=(), path_prefixes=(), forge_api="https://api.github.com")


def tar_of(files: dict[str, bytes]) -> bytes:
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as tar:
        for name, data in files.items():
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(data), 0o600
            tar.addfile(info, io.BytesIO(data))
    return buffer.getvalue()


class FakeBus:
    """`agent-bus` on the homeserver host: a registry, and bundles with numbered tokens."""

    def __init__(self) -> None:
        self.members: dict[str, list] = {}
        self.calls: list[tuple[str, ...]] = []
        self.minted = 0

    def __call__(self, argv: Sequence[str]) -> bytes:
        self.calls.append(tuple(argv))
        command = argv[0]
        if command == "list":
            return "".join(f"MEMBER\t{h}\t{role}\tjoin\t{'parked' if parked else 'active'}\n"
                           for h, (role, parked) in sorted(self.members.items())).encode()
        if command == "add-member":
            opts = dict(a[2:].split("=", 1) for a in argv[2:] if a.startswith("--") and "=" in a)
            assert opts["out"] == "-" and opts["type"] == "lxc"
            handle = f"{opts['repo']}.{opts['seat']}+{opts['host']}.lxc"
            if handle in self.members:
                if not self.members[handle][1]:
                    raise members.MemberError(f"{handle} is a current member")
                self.members[handle][1] = False
            else:
                self.members[handle] = [opts["role"], False]
            self.minted += 1
            member = {"team": argv[1], "user_id": f"@{handle}:{argv[1]}.agent-bus.internal",
                      "base_url": f"http://{opts['address']}:{PORT}",
                      "human_text": "--no-human-text" not in argv, "token_file": "token"}
            return tar_of({"member.json": json.dumps(member).encode(),
                           "token": f"tok{self.minted}".encode(), "README": b"next steps"})
        if command == "park-member":
            self.members[argv[2]][1] = True
            return f"CHANGED\tparked {argv[2]}\n".encode()
        if command == "set-role":
            self.members[argv[2]][0] = argv[3].split("=", 1)[1]
            return b""
        raise AssertionError(f"unexpected agent-bus {argv}")


class FakeGuest:
    """One container: guest scripts run here, against a temporary directory."""

    def __init__(self, root: pathlib.Path, *, source: str = SOURCE, suggest: str = SUGGEST,
                 users: Sequence[str] = ("agent",), config_ok: bool = True) -> None:
        self.dir = root
        self.kit_parent = str(root / "share" / "agent-bus")
        self.bin_dir = str(root / "bin")
        self.kit_owner = os.getuid()
        (root / "bin").mkdir(parents=True, exist_ok=True)
        self.source, self.suggest, self.users, self.config_ok = source, suggest, users, config_ok
        self.agent_runs: list[tuple[str, str, tuple[str, ...]]] = []

    def home(self, user: str) -> str | None:
        if user not in self.users:
            return None
        home = self.dir / "home" / user
        home.mkdir(parents=True, exist_ok=True)
        return str(home)

    def root(self, argv: Sequence[str], what: str, stdin: bytes = b"") -> bytes:
        # Root never writes or reads in the agent's home: those scripts run as the agent.
        assert argv[3:4] not in ([s] for s in AGENT_SCRIPTS), f"{what} ran as root"
        return self._run(argv, what, stdin, ())

    def agent(self, user: str, home: str, cwd: str, argv: Sequence[str], what: str,
              stdin: bytes = b"", env: Sequence[str] = ()) -> bytes:
        assert argv[3:4] not in ([s] for s in ROOT_SCRIPTS), f"{what} ran as the agent"
        self.agent_runs.append((user, cwd, tuple(env)))
        return self._run(argv, what, stdin, env)

    def _run(self, argv: Sequence[str], what: str, stdin: bytes, env: Sequence[str]) -> bytes:
        if list(argv) == ["pingbus", "suggest-handle"]:
            assert "HOOKS_DAEMON_HOSTNAME=box" in env
            return (self.suggest + "\n").encode()
        if argv[0] == "sh":
            if not self.config_ok:
                raise members.MemberError(f"{what}: failed (exit 78): bundle unreadable")
            return b""
        assert list(argv[:3]) == ["python3", "-I", "-c"], argv
        if argv[3] == members.SOURCE_PY:
            return (self.source + "\n").encode()
        run = subprocess.run([sys.executable, "-I", "-c", *argv[3:]], input=stdin,
                             capture_output=True, check=False)
        if run.returncode != 0:
            raise members.MemberError(f"{what}: failed (exit {run.returncode}): {run.stderr.decode().strip()}")
        return run.stdout


class Fixture(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = pathlib.Path(self._tmp.name)
        kit_parent = self.tmp / "host-share"
        (kit_parent / "kit" / "plugin").mkdir(parents=True)
        (kit_parent / "kit" / "pingbus").write_bytes(b"zipapp")
        (kit_parent / "kit" / "pingbus").chmod(0o755)
        (kit_parent / "kit" / "agent-bus-claude").write_bytes(b"#!/bin/sh\n")
        (kit_parent / "kit" / "agent-bus-claude").chmod(0o755)
        (kit_parent / "kit" / "plugin" / "plugin.json").write_bytes(b"{}")
        (kit_parent / "kit" / "plugin" / "plugin.json").chmod(0o644)
        self.kit_parent = kit_parent
        self.guest = FakeGuest(self.tmp / "guest")
        self.bus = FakeBus()
        self.state: str | None = "RUNNING"
        self.states: dict[str, str | None] = {}
        self.allow_from: Sequence[str] = (BRIDGE_CIDR,)
        self.bridge = True
        self.active = {TEAM, OTHER_TEAM}
        self.changed: list[str] = []

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def host(self) -> members.Host:
        return members.Host(
            container_state=lambda name: self.states.get(name, self.state),
            agent_bus=self.bus,
            ip_json=lambda: IP_JSON,
            is_bridge=lambda dev: self.bridge and dev == "lxcbr0",
            team_active=lambda team: team in self.active,
            bus_address=lambda: BUS_IP,
            team_file=lambda team: team_file(self.allow_from, team),
            kit_tar=lambda: members.kit_tar(self.kit_parent),
            guest=lambda name: self.guest,
        )

    def run_phase(self, phase: str, entries: Sequence[dict], teams_present: Sequence[str] = (TEAM,)) -> list[str]:
        self.changed = []
        members.run_phase(phase, members.parse_members(list(entries)), teams_present, self.host(),
                          emit=self.changed.append, say=lambda text: None)
        return self.changed

    def bundle_dir(self, team: str = TEAM) -> pathlib.Path:
        return self.tmp / "guest" / "home" / "agent" / ".config" / "pingbus" / team

    def env_file(self) -> pathlib.Path:
        return self.tmp / "guest" / "home" / "agent" / ".config" / "pingbus" / "env"


# ── the entries ──────────────────────────────────────────────────────────────────────────


class ParseMembersTest(unittest.TestCase):
    def test_defaults(self) -> None:
        (member,) = members.parse_members([entry(container="Dev-Box_1", host="Build Box")])
        self.assertEqual(member.seat, "devbox1")
        self.assertEqual(member.host, "build-box")
        self.assertEqual((member.role, member.human_text, member.state, member.repo),
                         ("worker", True, "present", None))

    def test_seat_and_repo_given(self) -> None:
        (member,) = members.parse_members([entry(seat=2, repo="Owner-Proj", role="orchestrator",
                                                 human_text=False)])
        self.assertEqual((member.seat, member.repo, member.role, member.human_text),
                         ("2", "owner-proj", "orchestrator", False))
        self.assertEqual(member.handle("proj"), "proj.2+box.lxc")

    def test_absent_needs_checkout_or_repo(self) -> None:
        (member,) = members.parse_members([entry(state="absent", checkout=None, repo="proj")])
        self.assertIsNone(member.checkout)
        with self.assertRaisesRegex(members.MemberError, "checkout or repo"):
            members.parse_members([entry(state="absent", checkout=None)])

    def test_refusals(self) -> None:
        cases = {
            "not a list": {"team": TEAM},
            "not a mapping": ["x"],
            "unknown key": [entry(colour="blue")],
            "missing key": [entry(user=None)],
            "bad team": [entry(team="Team A")],
            "bad container": [entry(container="-x")],
            "bad user": [entry(user="Agent User")],
            "bad seat": [entry(seat="a.b")],
            "bad role": [entry(role="boss")],
            "bad state": [entry(state="gone")],
            "human_text not a boolean": [entry(human_text="yes")],
            "present without checkout": [entry(checkout=None)],
            "relative checkout": [entry(checkout="proj")],
            "seat a container name cannot give": [entry(container="1box")],
            "a number is never a default seat": [entry(container="42")],
            "declared twice": [entry(), entry(seat="other")],
            "one user, two hosts": [entry(), entry(team=OTHER_TEAM, host="other")],
            "one seat, two containers": [entry(seat="dev"), entry(container="box2", seat="dev")],
        }
        for label, data in cases.items():
            with self.subTest(label), self.assertRaises(members.MemberError):
                members.parse_members(data)


# ── the decisions ────────────────────────────────────────────────────────────────────────


class PresentPlanTest(unittest.TestCase):
    BASE = f"http://{BUS_IP}:{PORT}"

    def setUp(self) -> None:
        (self.member,) = members.parse_members([entry()])

    def found(self, *, handle: str = HANDLE, token: bool = True, human_text: bool = True,
              base_url: str = BASE) -> members.Found:
        return members.Found(True, {"user_id": f"@{handle}:{TEAM}.agent-bus.internal",
                                    "base_url": base_url, "human_text": human_text}, token)

    def plan(self, found: members.Found, registered: tuple[str, bool] | None) -> members.Plan:
        return members.present_plan(HANDLE, self.BASE, self.member, found, registered)

    def test_current_member_is_left_alone(self) -> None:
        self.assertEqual(self.plan(self.found(), ("worker", False)), members.Plan())

    def test_role_change_only_sets_the_role(self) -> None:
        self.assertEqual(self.plan(self.found(), ("orchestrator", False)), members.Plan(set_role=True))

    def test_reissued_when_the_bundle_is_stale(self) -> None:
        reissue = members.Plan(remove_bundle=True, park=True, add=True)
        self.assertEqual(self.plan(self.found(token=False), ("worker", False)), reissue)
        self.assertEqual(self.plan(self.found(human_text=False), ("worker", False)), reissue)
        self.assertEqual(self.plan(self.found(base_url="http://192.0.2.99:8448"), ("worker", False)), reissue)

    def test_lost_bundle_parks_then_returns(self) -> None:
        self.assertEqual(self.plan(members.Found(False, None, False), ("worker", False)),
                         members.Plan(park=True, add=True))

    def test_parked_handle_returns(self) -> None:
        self.assertEqual(self.plan(members.Found(False, None, False), ("orchestrator", True)),
                         members.Plan(add=True, set_role=True))
        self.assertEqual(self.plan(self.found(), ("worker", True)),
                         members.Plan(remove_bundle=True, add=True))

    def test_new_member_is_added(self) -> None:
        self.assertEqual(self.plan(members.Found(False, None, False), None), members.Plan(add=True))
        self.assertEqual(self.plan(members.Found(True, None, False), None),
                         members.Plan(remove_bundle=True, add=True))

    def test_a_bundle_for_another_handle_is_refused(self) -> None:
        with self.assertRaisesRegex(members.MemberError, r"proj\.dev\+box\.lxc"):
            self.plan(self.found(handle="proj.dev+box.lxc"), ("worker", False))

    def test_a_bundle_joined_by_hand_names_its_seat(self) -> None:
        with self.assertRaisesRegex(members.MemberError, "set seat: 3 on the entry"):
            self.plan(self.found(handle="proj.3+box.lxc"), ("worker", False))

    def test_an_active_numbered_handle_without_a_bundle_is_not_parked(self) -> None:
        (numbered,) = members.parse_members([entry(seat=1)])
        handle = "proj.1+box.lxc"
        with self.assertRaisesRegex(members.MemberError, "another container's"):
            members.present_plan(handle, self.BASE, numbered, members.Found(False, None, False),
                                 ("worker", False))
        self.assertEqual(members.present_plan(handle, self.BASE, numbered, members.Found(False, None, False),
                                              ("worker", True)), members.Plan(add=True))


class AbsentPlanTest(unittest.TestCase):
    def test_parks_only_an_active_handle_of_a_team_that_runs_here(self) -> None:
        found = members.Found(True, None, False)
        self.assertEqual(members.absent_plan(found, ("worker", False), True),
                         members.Plan(remove_bundle=True, park=True))
        self.assertEqual(members.absent_plan(found, ("worker", True), True), members.Plan(remove_bundle=True))
        self.assertEqual(members.absent_plan(found, ("worker", False), False), members.Plan(remove_bundle=True))
        self.assertEqual(members.absent_plan(members.Found(False, None, False), None, True), members.Plan())


class MergeEnvTest(unittest.TestCase):
    def merge(self, text: str | None, **kwargs: object) -> str | None:
        return members.merge_env(text, where="env", **kwargs)

    def test_new_file(self) -> None:
        self.assertEqual(self.merge(None, add=[TEAM], host="box"),
                         f"PINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n")

    def test_adds_a_team_keeping_the_rest(self) -> None:
        text = "# mine\nPINGBUS_TEAMS=elsewhere\nHOOKS_DAEMON_HOSTNAME=box\nOTHER=1\n"
        self.assertEqual(self.merge(text, add=[TEAM], host="box"),
                         f"# mine\nPINGBUS_TEAMS=elsewhere,{TEAM}\nHOOKS_DAEMON_HOSTNAME=box\nOTHER=1\n")
        same = f"PINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n"
        self.assertEqual(self.merge(same, add=[TEAM], host="box"), same)

    def test_a_different_role_for_other_teams_is_refused(self) -> None:
        with self.assertRaisesRegex(members.MemberError, "elsewhere"):
            self.merge("PINGBUS_TEAMS=elsewhere\nHOOKS_DAEMON_HOSTNAME=old\n", add=[TEAM], host="box")
        self.assertEqual(self.merge(f"PINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=old\n", add=[TEAM], host="box"),
                         f"PINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n")

    def test_removing(self) -> None:
        self.assertEqual(self.merge(f"PINGBUS_TEAMS=elsewhere,{TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n", remove=[TEAM]),
                         "PINGBUS_TEAMS=elsewhere\nHOOKS_DAEMON_HOSTNAME=box\n")
        self.assertIsNone(self.merge(f"PINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n", remove=[TEAM]))
        self.assertEqual(self.merge(f"OTHER=1\nPINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n", remove=[TEAM]),
                         "OTHER=1\n")

    def test_a_form_it_cannot_read_is_refused(self) -> None:
        for text in ("export PINGBUS_TEAMS=x\n", 'PINGBUS_TEAMS="x"\n', "PINGBUS_TEAMS=a\nPINGBUS_TEAMS=b\n"):
            with self.subTest(text), self.assertRaises(members.MemberError):
                self.merge(text, add=[TEAM], host="box")


class NetworkTest(unittest.TestCase):
    def test_host_network(self) -> None:
        self.assertEqual(members.host_network(SOURCE, IP_JSON), ("lxcbr0", BRIDGE_CIDR))
        with self.assertRaisesRegex(members.MemberError, "no network of this host"):
            members.host_network("203.0.113.7", IP_JSON)

    def test_covered(self) -> None:
        self.assertTrue(members.covered(SOURCE, ["198.51.0.0/16"]))
        self.assertFalse(members.covered(SOURCE, [ELSEWHERE_CIDR, BUS_IP]))

    def test_base_url(self) -> None:
        self.assertEqual(members.base_url(BUS_IP, PORT), f"http://{BUS_IP}:{PORT}")
        self.assertEqual(members.base_url("2001:db8::1", PORT), f"http://[2001:db8::1]:{PORT}")

    def test_parse_suggest(self) -> None:
        self.assertEqual(members.parse_suggest(SUGGEST + "\n", "x"), ("proj", "box"))
        with self.assertRaisesRegex(members.MemberError, "--type=host"):
            members.parse_suggest("--repo=proj --host=box --type=host", "x")

    def test_parse_list(self) -> None:
        text = f"HUMAN\talice\tjoin\nMEMBER\t{HANDLE}\tworker\tjoin\tparked\n"
        self.assertEqual(members.parse_list(text), {HANDLE: ("worker", True)})


# ── the steps, end to end ────────────────────────────────────────────────────────────────


class PresentPhaseTest(Fixture):
    def test_join_then_nothing(self) -> None:
        changed = self.run_phase("present", [entry()])
        self.assertIn(f"{HANDLE} joined team {TEAM} (agent@devbox)", changed)
        self.assertTrue(any(line.startswith("container devbox: kit") for line in changed))
        self.assertIn(f"agent@devbox: {members.ENV_PATH} lists {TEAM}", changed)
        add = next(call for call in self.bus.calls if call[0] == "add-member")
        self.assertEqual(add, ("add-member", TEAM, "--repo=proj", "--seat=devbox", "--host=box", "--type=lxc",
                               "--role=worker", f"--address={BUS_IP}", "--out=-"))
        bundle = self.bundle_dir()
        self.assertEqual(stat.S_IMODE(bundle.stat().st_mode), 0o700)
        for name in ("member.json", "token", "README"):
            self.assertEqual(stat.S_IMODE((bundle / name).stat().st_mode), 0o600, name)
        self.assertEqual((bundle / "token").read_text(), "tok1")
        self.assertEqual(self.env_file().read_text(), f"PINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n")
        kit = pathlib.Path(self.guest.kit_parent) / "kit"
        self.assertEqual((kit / "pingbus").read_bytes(), b"zipapp")
        self.assertEqual(os.readlink(pathlib.Path(self.guest.bin_dir) / "pingbus"), str(kit / "pingbus"))
        suggest_cwd = [cwd for _, cwd, env in self.guest.agent_runs if env]
        self.assertEqual(suggest_cwd, [CHECKOUT])

        self.bus.calls.clear()
        self.assertEqual(self.run_phase("present", [entry()]), [])
        self.assertEqual([call[0] for call in self.bus.calls], ["list"])
        self.assertEqual((bundle / "token").read_text(), "tok1")

    def test_a_lost_bundle_is_parked_and_returned(self) -> None:
        self.run_phase("present", [entry()])
        for path in self.bundle_dir().iterdir():
            path.unlink()
        self.bundle_dir().rmdir()
        changed = self.run_phase("present", [entry()])
        self.assertIn(f"parked {HANDLE} in team {TEAM}, to issue a new bundle", changed)
        self.assertEqual((self.bundle_dir() / "token").read_text(), "tok2")

    def test_role_and_human_text_follow_the_entry(self) -> None:
        self.run_phase("present", [entry()])
        changed = self.run_phase("present", [entry(role="orchestrator")])
        self.assertEqual(changed, [f"{HANDLE} is now orchestrator in team {TEAM}"])
        changed = self.run_phase("present", [entry(role="orchestrator", human_text=False)])
        self.assertIn(f"{HANDLE} joined team {TEAM} (agent@devbox)", changed)
        member = json.loads((self.bundle_dir() / "member.json").read_text())
        self.assertIs(member["human_text"], False)

    def test_two_teams_share_the_env_file(self) -> None:
        self.run_phase("present", [entry(), entry(team=OTHER_TEAM)], (TEAM, OTHER_TEAM))
        self.assertEqual(self.env_file().read_text(),
                         f"PINGBUS_TEAMS={TEAM},{OTHER_TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n")

    def test_allow_from_must_cover_the_bridge(self) -> None:
        self.allow_from = (ELSEWHERE_CIDR,)
        with self.assertRaises(members.MemberError) as caught:
            self.run_phase("present", [entry()])
        self.assertIn(f'add "{BRIDGE_CIDR}" to allow_from of team {TEAM}', str(caught.exception))
        self.assertNotIn("add-member", [call[0] for call in self.bus.calls])

    def test_the_route_must_be_a_bridge(self) -> None:
        self.bridge = False
        with self.assertRaisesRegex(members.MemberError, "not a bridge"):
            self.run_phase("present", [entry()])

    def test_refusals_before_anything_is_made(self) -> None:
        cases = {
            "no such container": ("state", None, "does not exist"),
            "stopped container": ("state", "STOPPED", "is STOPPED"),
            "homeserver not running": ("active", set(), "not active"),
        }
        for label, (attr, value, message) in cases.items():
            with self.subTest(label):
                self.tearDown()
                self.setUp()
                setattr(self, attr, value)
                with self.assertRaisesRegex(members.MemberError, message):
                    self.run_phase("present", [entry()])
                self.assertEqual(self.bus.calls, [])

    def test_a_later_container_refused_before_any_change(self) -> None:
        self.states = {"box2": "STOPPED"}
        with self.assertRaisesRegex(members.MemberError, "box2 is STOPPED"):
            self.run_phase("present", [entry(), entry(container="box2")])
        self.assertFalse(pathlib.Path(self.guest.kit_parent).exists())
        self.assertEqual(self.bus.calls, [])

    def test_team_must_be_declared_present(self) -> None:
        with self.assertRaisesRegex(members.MemberError, "agent_bus_teams"):
            self.run_phase("present", [entry()], teams_present=())

    def test_missing_agent_user(self) -> None:
        self.guest.users = ()
        with self.assertRaisesRegex(members.MemberError, "no user agent"):
            self.run_phase("present", [entry()])

    def test_suggest_handle_must_say_lxc(self) -> None:
        self.guest.suggest = "--repo=proj --host=box --type=host"
        with self.assertRaisesRegex(members.MemberError, "--type=host"):
            self.run_phase("present", [entry()])

    def test_declared_repo_must_match(self) -> None:
        with self.assertRaisesRegex(members.MemberError, "other"):
            self.run_phase("present", [entry(repo="other")])

    def test_config_check_failure_fails(self) -> None:
        self.guest.config_ok = False
        with self.assertRaisesRegex(members.MemberError, "bundle unreadable"):
            self.run_phase("present", [entry()])


class AbsentPhaseTest(Fixture):
    def test_parks_and_clears(self) -> None:
        self.run_phase("present", [entry(), entry(team=OTHER_TEAM)], (TEAM, OTHER_TEAM))
        changed = self.run_phase("absent", [entry(state="absent"), entry(team=OTHER_TEAM)],
                                 (TEAM, OTHER_TEAM))
        self.assertIn(f"parked {HANDLE} in team {TEAM}", changed)
        self.assertFalse(self.bundle_dir().exists())
        self.assertTrue(self.bundle_dir(OTHER_TEAM).exists())
        self.assertEqual(self.env_file().read_text(), f"PINGBUS_TEAMS={OTHER_TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n")
        self.assertEqual(self.run_phase("absent", [entry(state="absent")]), [])
        self.assertEqual(self.run_phase("absent", [entry(state="absent", checkout=None, repo="proj")]), [])

    def test_last_team_removes_the_env_file(self) -> None:
        self.run_phase("present", [entry()])
        self.run_phase("absent", [entry(state="absent")])
        self.assertFalse(self.env_file().exists())
        self.assertTrue(self.bus.members[HANDLE][1])

    def test_a_destroyed_container_needs_the_repo(self) -> None:
        self.run_phase("present", [entry()])
        self.state = None
        with self.assertRaisesRegex(members.MemberError, "repo"):
            self.run_phase("absent", [entry(state="absent")])
        changed = self.run_phase("absent", [entry(state="absent", repo="proj")])
        self.assertEqual(changed, [f"parked {HANDLE} in team {TEAM}"])

    def test_a_team_not_running_here_parks_nothing(self) -> None:
        self.run_phase("present", [entry()])
        self.active = set()
        changed = self.run_phase("absent", [entry(state="absent")], teams_present=())
        self.assertNotIn("park-member", [call[0] for call in self.bus.calls])
        self.assertFalse(self.bundle_dir().exists())
        self.assertTrue(changed)

    def test_a_declared_team_that_is_down_is_refused(self) -> None:
        self.active = set()
        with self.assertRaisesRegex(members.MemberError, "not active"):
            self.run_phase("absent", [entry(state="absent")])


class KitSyncTest(Fixture):
    def sync(self) -> bytes:
        return self.guest.root(["python3", "-I", "-c", members.KIT_SYNC_PY, self.guest.kit_parent,
                                self.guest.bin_dir, str(self.guest.kit_owner)], "kit",
                               members.kit_tar(self.kit_parent))

    def test_a_kit_directory_others_can_write_is_refused(self) -> None:
        self.sync()
        plugin = pathlib.Path(self.guest.kit_parent) / "kit" / "plugin"
        plugin.chmod(0o777)
        with self.assertRaisesRegex(members.MemberError, "writable by others"):
            self.sync()
        plugin.chmod(0o755)
        self.guest.kit_owner = os.getuid() + 1
        with self.assertRaisesRegex(members.MemberError, "not owned by uid"):
            self.sync()

    def test_updates_only_what_differs_and_removes_strays(self) -> None:
        first = self.sync().decode().splitlines()
        self.assertIn("kit/pingbus", first)
        self.assertEqual(self.sync(), b"")
        kit = pathlib.Path(self.guest.kit_parent) / "kit"
        (kit / "stray").write_text("x")
        (kit / "pingbus").write_bytes(b"old")
        self.assertEqual(sorted(self.sync().decode().splitlines()), ["kit/pingbus", "removed kit/stray"])

    def test_a_foreign_command_is_refused(self) -> None:
        (pathlib.Path(self.guest.bin_dir) / "pingbus").write_text("mine")
        with self.assertRaisesRegex(members.MemberError, "not a link"):
            self.sync()

    def test_kit_tar_refuses_a_symlink(self) -> None:
        (self.kit_parent / "kit" / "link").symlink_to("pingbus")
        with self.assertRaises(members.MemberError):
            members.kit_tar(self.kit_parent)


class LxcGuestTest(unittest.TestCase):
    """The real container commands: root through lxc-attach with a cleared environment, the
    agent through runuser inside it, the bundle on stdin and never in argv."""

    def setUp(self) -> None:
        self.calls: list[tuple[list[str], bytes]] = []
        self.answer = (0, b"", b"")
        patcher = mock.patch.object(members, "_run", side_effect=self.fake_run)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.guest = members.LxcGuest("devbox")

    def fake_run(self, argv: Sequence[str], stdin: bytes = b"") -> tuple[int, bytes, bytes]:
        self.calls.append((list(argv), stdin))
        return self.answer

    PREFIX = ["lxc-attach", "-n", "devbox", "--clear-env", "--", "env", "-i",
              f"PATH={members.GUEST_PATH}", "HOME=/root"]

    def test_root(self) -> None:
        self.guest.root(["getent", "group", "x"], "what")
        self.assertEqual(self.calls, [(self.PREFIX + ["getent", "group", "x"], b"")])

    def test_agent_runs_as_the_user_in_its_directory(self) -> None:
        self.guest.agent("agent", "/home/<user>", CHECKOUT, ["python3", "-I", "-c", "S", "a"], "what",
                         stdin=b"bundle-tar", env=["K=v"])
        argv, stdin = self.calls[0]
        self.assertEqual(argv, self.PREFIX + ["runuser", "-u", "agent", "--", "env", "-i", "-C", CHECKOUT,
                                              f"PATH={members.GUEST_PATH}", "HOME=/home/<user>", "K=v",
                                              "python3", "-I", "-c", "S", "a"])
        self.assertEqual(stdin, b"bundle-tar")

    def test_a_failure_raises_with_its_stderr(self) -> None:
        self.answer = (1, b"", b"boom")
        with self.assertRaisesRegex(members.MemberError, "what failed \\(exit 1\\): boom"):
            self.guest.root(["false"], "what")

    def test_home(self) -> None:
        self.answer = (0, b"agent:x:1000:1000::/home/<user>:/bin/bash\n", b"")
        self.assertEqual(self.guest.home("agent"), "/home/<user>")
        self.answer = (2, b"", b"")
        self.assertIsNone(self.guest.home("agent"))


class MainTest(unittest.TestCase):
    def test_bad_entries_exit_78(self) -> None:
        err = io.StringIO()
        code = members.main(["--phase", "present", "--members", json.dumps([{"team": TEAM}]),
                             "--teams-present", "[]"], host_factory=lambda: None, stdout=io.StringIO(), stderr=err)
        self.assertEqual(code, 78)
        self.assertIn("missing", err.getvalue())

    def test_bad_json_exits_64(self) -> None:
        code = members.main(["--phase", "present", "--members", "{", "--teams-present", "[]"],
                            host_factory=lambda: None, stdout=io.StringIO(), stderr=io.StringIO())
        self.assertEqual(code, 64)

    def test_nothing_declared_does_nothing(self) -> None:
        out = io.StringIO()
        code = members.main(["--phase", "absent", "--members", "[]", "--teams-present", "[]"],
                            host_factory=lambda: None, stdout=out, stderr=io.StringIO())
        self.assertEqual((code, out.getvalue()), (0, ""))


if __name__ == "__main__":
    unittest.main()

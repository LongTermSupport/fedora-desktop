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

import dataclasses
import io
import json
import os
import pathlib
import shlex
import shutil
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
from helpers.agent_bus_lxc import derive, members
from helpers.pingbus import config
from helpers.pingbus import bundle as zipapp

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
#: The kit's pingbus as the host builds it: its first line names the host's interpreter.
HOST_SHEBANG = b"#!/usr/bin/python3\n"
#: The interpreter an EL9-like member is found to have (EL9_LAYOUT).
NEW_PY = "/usr/local/bin/python3.13"
OLD = "old"
NEW = "new"
#: An EL9 container as it was met on a host: /usr/bin/python3 is the 3.9 system interpreter,
#: python3 on PATH a 3.13 in /usr/local/bin, and a packaged 3.13 also in /usr/bin.
EL9_LAYOUT = {"usr/bin/python3.9": OLD, "usr/bin/python3": "python3.9", "usr/bin/python3.13": NEW,
              "usr/local/bin/python3.13": NEW, "usr/local/bin/python3": "python3.13",
              "usr/local/bin/python3.13-config": OLD}


def make_interpreters(root: pathlib.Path, layout: dict[str, str]) -> None:
    """`layout` made under `root`: NEW runs this test's Python, OLD stands for one below the
    minimum and fails whatever it is asked to run, any other value is a symlink to it."""
    for name, what in layout.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        if what == NEW:
            path.write_text(f"#!/bin/sh\nexec {shlex.quote(sys.executable)} \"$@\"\n")
        elif what == OLD:
            path.write_text("#!/bin/sh\necho 'this is Python 3.9.21, below the minimum' >&2\nexit 1\n")
        else:
            path.symlink_to(what)
            continue
        path.chmod(0o755)


def entry(**overrides: object) -> dict:
    # The explicit host and seat are the overrides that entries written before they were
    # derived carry; the derivation tests drop them (host=None, seat=None).
    base = {"team": TEAM, "container": "devbox", "user": "agent", "host": "box", "checkout": CHECKOUT}
    base.update(overrides)
    base.setdefault("seat", "".join(ch for ch in str(base["container"]).lower() if ch.isalnum()))
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
                 users: Sequence[str] = ("agent",), config_ok: bool = True,
                 interpreters: dict[str, str] | None = None) -> None:
        self.dir = root
        self.container = root.name
        # The container's own interpreters, at their paths under its root (EL9-like by default).
        make_interpreters(root, EL9_LAYOUT if interpreters is None else interpreters)
        #: The interpreter of every guest script run, by its path in the container.
        self.interpreters: list[str] = []
        self.python_searches = 0
        self.kit_parent = str(root / "share" / "agent-bus")
        self.bin_dir = str(root / "bin")
        self.kit_owner = os.getuid()
        (root / "bin").mkdir(parents=True, exist_ok=True)
        self.source, self.suggest, self.users, self.config_ok = source, suggest, users, config_ok
        self.gh_ok = True
        self.gh_checks: list[tuple[str, str]] = []
        self.agent_runs: list[tuple[str, str, tuple[str, ...]]] = []
        # systemd inside the container, for a member's supervised session (Plan 00203 gap 9).
        self.unit_dir = str(root / "etc" / "systemd" / "system")
        self.systemd = True
        self.tmux: str | None = "/usr/bin/tmux"
        #: unit -> {"enabled": bool, "active": bool}, for the units systemctl has seen.
        self.units: dict[str, dict[str, bool]] = {}
        self.systemctl: list[tuple[str, ...]] = []

    def _systemctl(self, args: Sequence[str]) -> bytes:
        self.systemctl.append(tuple(args))
        if not self.systemd:
            raise members.MemberError(f"systemctl {args[0]} failed (exit 1): System has not been booted "
                                      "with systemd as init system (PID 1). Can't operate.")
        verb, rest = args[0], [a for a in args[1:] if not a.startswith("-")]
        if verb == "daemon-reload":
            return b""
        unit = rest[-1]
        state = self.units.setdefault(unit, {"enabled": False, "active": False})
        loaded = (pathlib.Path(self.unit_dir) / unit).exists()
        if verb == "show":
            return (f"ActiveState={'active' if state['active'] else 'inactive'}\n"
                    f"LoadState={'loaded' if loaded else 'not-found'}\n"
                    f"UnitFileState={'enabled' if state['enabled'] else 'disabled'}\n").encode()
        if verb == "enable":
            state["enabled"] = True
        elif verb in ("start", "restart"):
            state["active"] = True
        elif verb == "disable":
            state["enabled"] = False
            if "--now" in args:
                state["active"] = False
        else:
            raise AssertionError(f"unexpected systemctl {args}")
        return b""

    def home(self, user: str) -> str | None:
        if user not in self.users:
            return None
        home = self.dir / "home" / user
        home.mkdir(parents=True, exist_ok=True)
        return str(home)

    def root(self, argv: Sequence[str], what: str, stdin: bytes = b"") -> bytes:
        # Root never writes or reads in the agent's home: those scripts run as the agent.
        assert argv[3:4] not in ([s] for s in AGENT_SCRIPTS), f"{what} ran as root"
        if argv[0] == "systemctl":
            return self._systemctl(argv[1:])
        if argv[3:4] == [members.SYSTEMD_PROBE_PY]:
            return b"systemd\n" if self.systemd else b""
        if list(argv) == ["sh", "-c", members.SERVICE_NEEDS_SH]:
            if not self.systemd:
                raise members.MemberError(f"{what} failed (exit 3): no systemd")
            if self.tmux is None:
                raise members.MemberError(f"{what} failed (exit 4): no tmux")
            return f"{self.tmux}\n".encode()
        if list(argv[:3]) == ["sh", "-c", members.PYTHON_FIND_SH]:
            # The real search, over this container's root: what it prints is said as the
            # container sees it.
            assert tuple(argv[4:]) == members.PYTHON_DIRS, argv
            self.python_searches += 1
            done = subprocess.run(["sh", "-c", members.PYTHON_FIND_SH, "sh",
                                   *(f"{self.dir}{d}" for d in members.PYTHON_DIRS)],
                                  capture_output=True, text=True, check=True)
            return "".join(line.removeprefix(str(self.dir)) + "\n" for line in done.stdout.splitlines()).encode()
        return self._run(argv, what, stdin, ())

    def agent(self, user: str, home: str, cwd: str, argv: Sequence[str], what: str,
              stdin: bytes = b"", env: Sequence[str] = ()) -> bytes:
        assert argv[3:4] not in ([s] for s in ROOT_SCRIPTS), f"{what} ran as the agent"
        if list(argv) == ["sh", "-c", members.GH_CHECK_SH]:
            self.gh_checks.append((user, argv[2]))
            if not self.gh_ok:
                raise members.MemberError(f"{what} failed (exit 1): no oauth token found for github.com")
            return b""
        self.agent_runs.append((user, cwd, tuple(env)))
        return self._run(argv, what, stdin, env)

    def _run(self, argv: Sequence[str], what: str, stdin: bytes, env: Sequence[str]) -> bytes:
        if list(argv) == ["pingbus", "suggest-handle"]:
            assert any(item.startswith("HOOKS_DAEMON_HOSTNAME=") for item in env)
            return (self.suggest + "\n").encode()
        if argv[0] == "sh":
            if not self.config_ok:
                raise members.MemberError(f"{what}: failed (exit 78): bundle unreadable")
            return b""
        # The container's own interpreter by its path there, run from under this root: one
        # the container lacks, or one below the minimum, fails as it would in the container.
        assert argv[0].startswith("/") and list(argv[1:3]) == ["-I", "-c"], argv
        self.interpreters.append(argv[0])
        if argv[3] == members.SOURCE_PY:
            return (self.source + "\n").encode()
        run = subprocess.run([f"{self.dir}{argv[0]}", *argv[1:]], input=stdin,
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
        (kit_parent / "kit" / "pingbus").write_bytes(HOST_SHEBANG + b"zipapp")
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
        self.reads: list[tuple[str, str, str]] = []
        self.unreadable: str | None = None
        self.remote_config = '[remote "origin"]\n\turl = https://github.com/Example-Org/Proj.git\n'
        self.env_local: str | None = "export HOOKS_DAEMON_HOSTNAME=box\n"
        #: The containers lxc-autostart starts at boot (lxc.start.auto = 1).
        self.autostart = {"devbox"}
        #: The member records (L8), by team, as the host keeps them; and the clock.
        self.records: dict[str, dict] = {}
        self.clock = 1_792_000_000.0
        self.reports: list[members.Report] = []

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
            read_checkout=self.read_checkout,
            autostarts=lambda name: name in self.autostart,
            read_record=lambda team: self.records.get(team),
            write_record=self.write_record,
            record_teams=lambda: sorted(self.records),
            now=lambda: self.clock,
        )

    def write_record(self, team: str, record: dict | None) -> bool:
        before = self.records.get(team)
        if record is None:
            self.records.pop(team, None)
        else:
            self.records[team] = json.loads(json.dumps(record))
        return before != self.records.get(team)

    def item(self, container: str = "devbox", team: str = TEAM) -> dict:
        (found,) = [i for i in self.records[team]["members"] if i["container"] == container]
        return found

    def read_checkout(self, container: str, user: str, path: str) -> str:
        """What the root-side reader prints for the checkout (its script is tested in test_derive)."""
        self.reads.append((container, user, path))
        if self.unreadable:
            raise members.MemberError(self.unreadable)
        return json.dumps({"path": path, "uid": 1000, "git_config": self.remote_config,
                           "env_uid": 1000 if self.env_local else None, "env_local": self.env_local})

    def run_phase(self, phase: str, entries: Sequence[dict], teams_present: Sequence[str] = (TEAM,)) -> list[str]:
        self.changed = []
        self.reports = members.run_phase(phase, members.parse_members(list(entries)), teams_present,
                                         self.host(), emit=self.changed.append, say=lambda text: None)
        # The member record's own line is said by the tests that are about it (L8).
        return [line for line in self.changed if not line.startswith("the LXC member record")]

    def bundle_dir(self, team: str = TEAM) -> pathlib.Path:
        return self.tmp / "guest" / "home" / "agent" / ".config" / "pingbus" / team

    def env_file(self) -> pathlib.Path:
        return self.tmp / "guest" / "home" / "agent" / ".config" / "pingbus" / "env"


# ── the entries ──────────────────────────────────────────────────────────────────────────


class ParseMembersTest(unittest.TestCase):
    def test_defaults(self) -> None:
        (member,) = members.parse_members([entry(host="Build Box", seat=None)])
        self.assertEqual(member.host, "build-box")
        self.assertIsNone(member.seat)
        self.assertEqual((member.role, member.human_text, member.state, member.repo),
                         ("worker", True, "present", None))

    def test_host_and_seat_are_optional_overrides_so_they_are_left_to_be_derived(self) -> None:
        (member,) = members.parse_members([{"team": TEAM, "container": "devbox", "user": "agent",
                                            "checkout": CHECKOUT}])
        self.assertEqual((member.host, member.seat), (None, None))
        with self.assertRaisesRegex(members.MemberError, "derived"):
            member.handle("proj")

    def test_seat_and_repo_given(self) -> None:
        (member,) = members.parse_members([entry(seat=2, repo="Owner-Proj", role="worker",
                                                 human_text=False)])
        self.assertEqual((member.seat, member.repo, member.role, member.human_text),
                         ("2", "owner-proj", "worker", False))
        self.assertEqual(member.handle("proj"), "proj.2+box.lxc")

    def test_a_coordinator_must_take_human_text(self) -> None:
        # Plan 00211 SPEC 4.5: every message in the questions room is a human's, so a
        # coordinator dropping human text would hear none of them.
        with self.assertRaisesRegex(members.MemberError, "entry 1: a coordinator must take human text"):
            members.parse_members([entry(role="coordinator", human_text=False)])
        (member,) = members.parse_members([entry(role="coordinator", human_text=False, state="absent")])
        self.assertEqual(member.state, "absent")

    def test_an_entry_still_saying_orchestrator_is_a_coordinator(self) -> None:
        (member,) = members.parse_members([entry(role="orchestrator")])
        self.assertEqual(member.role, "coordinator")

    def test_two_coordinators_in_one_team_are_refused_before_anything_runs(self) -> None:
        with self.assertRaisesRegex(members.MemberError, "entries 1 and 2 both declare the coordinator of team"):
            members.parse_members([entry(container="box-a", role="coordinator"),
                                   entry(container="box-b", role="coordinator")])

    def test_one_coordinator_per_team_is_fine_and_an_absent_one_does_not_count(self) -> None:
        found = members.parse_members([
            entry(container="box-a", role="coordinator"),
            entry(container="box-b", role="coordinator", team="team-b"),
            entry(container="box-c", role="coordinator", state="absent"),
        ])
        self.assertEqual(len(found), 3)

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
            "a host that is not a string": [entry(host=5)],
            "declared twice": [entry(), entry(seat="other")],
            "one user, two hosts": [entry(), entry(team=OTHER_TEAM, host="other")],
            "one seat, two containers": [entry(seat="dev"), entry(container="box2", seat="dev")],
            "one seat, two containers, one repo": [entry(seat="dev", repo="proj"),
                                                   entry(container="box2", seat="dev", repo="proj")],
            "one seat, two containers, one repo unknown": [entry(seat="dev", repo="proj"),
                                                           entry(container="box2", seat="dev")],
        }
        for label, data in cases.items():
            with self.subTest(label), self.assertRaises(members.MemberError):
                members.parse_members(data)

    def test_one_seat_number_in_two_repos_is_two_handles_so_two_containers_may_hold_it(self) -> None:
        # proj-a.dev+box.lxc and proj-b.dev+box.lxc: a seat is numbered within its repo and host.
        found = members.parse_members([entry(seat="dev", repo="proj-a"),
                                       entry(container="box2", seat="dev", repo="proj-b")])
        self.assertEqual([m.seat for m in found], ["dev", "dev"])


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
        self.assertEqual(self.plan(self.found(), ("coordinator", False)), members.Plan(set_role=True))

    def test_reissued_when_the_bundle_is_stale(self) -> None:
        reissue = members.Plan(remove_bundle=True, park=True, add=True)
        self.assertEqual(self.plan(self.found(token=False), ("worker", False)), reissue)
        self.assertEqual(self.plan(self.found(human_text=False), ("worker", False)), reissue)
        self.assertEqual(self.plan(self.found(base_url="http://192.0.2.99:8448"), ("worker", False)), reissue)

    def test_lost_bundle_parks_then_returns(self) -> None:
        self.assertEqual(self.plan(members.Found(False, None, False), ("worker", False)),
                         members.Plan(park=True, add=True))

    def test_parked_handle_returns(self) -> None:
        self.assertEqual(self.plan(members.Found(False, None, False), ("coordinator", True)),
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
        self.assertEqual((kit / "pingbus").read_bytes(), f"#!{NEW_PY}\n".encode() + b"zipapp")
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
        changed = self.run_phase("present", [entry(role="coordinator")])
        self.assertEqual(changed, [f"{HANDLE} is now coordinator in team {TEAM}"])
        changed = self.run_phase("present", [entry(role="worker", human_text=False)])
        self.assertIn(f"{HANDLE} joined team {TEAM} (agent@devbox)", changed)
        member = json.loads((self.bundle_dir() / "member.json").read_text())
        self.assertIs(member["human_text"], False)

    def test_a_handover_demotes_before_it_promotes_whatever_the_entry_order(self) -> None:
        guests = {name: FakeGuest(self.tmp / name) for name in ("box-a", "box-b")}
        host = dataclasses.replace(self.host(), guest=lambda name: guests[name])
        old = entry(container="box-a", role="coordinator")
        members.run_phase("present", members.parse_members([old]), (TEAM, OTHER_TEAM), host,
                          emit=lambda text: None, say=lambda text: None)
        self.bus.calls.clear()
        # box-b lists a worker of another team before its coordinator of this one, and is itself
        # listed before the container whose coordinator is being demoted.
        handover = [entry(container="box-b", team=OTHER_TEAM, role="worker", seat="other"),
                    entry(container="box-b", role="coordinator"),
                    entry(container="box-a", role="worker")]
        members.run_phase("present", members.parse_members(handover), (TEAM, OTHER_TEAM), host,
                          emit=lambda text: None, say=lambda text: None)
        roles = [call for call in self.bus.calls
                 if call[0] in ("set-role", "add-member") and call[1] == TEAM]
        self.assertEqual([call[0] for call in roles], ["set-role", "add-member"])
        self.assertEqual(roles[0][3], "--role=worker")
        self.assertIn("--role=coordinator", roles[1])

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
        self.states = {"box2": None}
        with self.assertRaisesRegex(members.MemberError, "box2 does not exist"):
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

    def test_gh_is_checked_as_the_agent_user_before_any_change(self) -> None:
        """README.lxc: gh is required and logged in. The play checks it, never provides it."""
        self.run_phase("present", [entry()])
        self.assertEqual(self.guest.gh_checks, [("agent", members.GH_CHECK_SH)])

    def test_gh_missing_or_logged_out_refused_naming_the_container(self) -> None:
        self.guest.gh_ok = False
        with self.assertRaises(members.MemberError) as caught:
            self.run_phase("present", [entry()])
        message = str(caught.exception)
        self.assertIn("LXC container devbox", message)
        self.assertIn("gh auth login --hostname github.com", message)
        self.assertIn("no oauth token", message)
        self.assertFalse(pathlib.Path(self.guest.kit_parent).exists())
        self.assertEqual(self.bus.calls, [])


class ResolveMembersTest(unittest.TestCase):
    """Host and seat left out of an entry are derived, in file order, as ccy derives them."""

    IDENTITY = derive.Identity("proj", "box")

    def resolve(self, entries: Sequence[dict], *, roster: dict[str, bool] | None = None,
                bundles: dict[str, str] | None = None) -> tuple[members.Member, ...]:
        return members.resolve_members(
            members.parse_members(list(entries)),
            identify=lambda member: self.IDENTITY,
            roster=lambda team: roster or {},
            bundle=lambda member: (bundles or {}).get(member.container))

    def test_an_entry_with_both_is_untouched_and_never_asks_for_the_checkout(self) -> None:
        found = members.parse_members([entry()])
        resolved = members.resolve_members(found, identify=lambda member: self.fail("read the checkout"),
                                           roster=lambda team: self.fail("read the roster"),
                                           bundle=lambda member: self.fail("read the bundle"))
        self.assertEqual(resolved, found)

    def test_host_and_seat_come_from_the_checkout_and_the_team(self) -> None:
        (member,) = self.resolve([entry(host=None, seat=None)])
        self.assertEqual((member.host, member.seat), ("box", "1"))
        self.assertEqual(member.handle("proj"), "proj.1+box.lxc")

    def test_two_containers_of_one_repo_get_the_next_seats_in_file_order(self) -> None:
        one, two = self.resolve([entry(host=None, seat=None, container="box-a"),
                                 entry(host=None, seat=None, container="box-b")])
        self.assertEqual((one.seat, two.seat), ("1", "2"))

    def test_two_containers_one_user_two_repos_each_take_seat_one_of_their_own_repo(self) -> None:
        # The shape lxc-add writes for a second container: no host, no seat, the same user name,
        # a checkout of another repository. Each handle is numbered within its own repo and host.
        identities = {"/srv/proj-a": derive.Identity("proj-a", "box"), "/srv/proj-b": derive.Identity("proj-b", "box")}
        one, two = members.resolve_members(
            members.parse_members([entry(host=None, seat=None, container="box-a", checkout="/srv/proj-a"),
                                   entry(host=None, seat=None, container="box-b", checkout="/srv/proj-b")]),
            identify=lambda member: identities[member.checkout], roster=lambda team: {},
            bundle=lambda member: None)
        self.assertEqual((one.handle(one.repo), two.handle(two.repo)), ("proj-a.1+box.lxc", "proj-b.1+box.lxc"))

    def test_a_member_joined_first_keeps_its_seat_when_an_earlier_entry_is_added_before_it(self) -> None:
        # Derivation never renumbers a joined member: its bundle names its seat, wherever it is in
        # the file, and the new entry takes the next seat free of it.
        new, joined = self.resolve([entry(host=None, seat=None, container="box-b"),
                                    entry(host=None, seat=None, container="box-a")],
                                   roster={"proj.1+box.lxc": False, "proj.2+box.lxc": True},
                                   bundles={"box-a": "proj.1+box.lxc"})
        self.assertEqual((new.seat, joined.seat), ("2", "1"))

    def test_an_explicit_seat_is_kept_and_not_given_to_another(self) -> None:
        one, two = self.resolve([entry(host=None, seat=None, container="box-a"),
                                 entry(host=None, seat="1", container="box-b")])
        self.assertEqual((one.seat, two.seat), ("2", "1"))

    def test_the_team_decides_not_the_file(self) -> None:
        one, two = self.resolve([entry(host=None, seat=None, container="box-a"),
                                 entry(host=None, seat=None, container="box-b", team=OTHER_TEAM)])
        self.assertEqual((one.seat, two.seat), ("1", "1"))

    def test_an_explicit_host_overrides_what_the_checkout_assigns(self) -> None:
        (member,) = self.resolve([entry(host="desk", seat=None)])
        self.assertEqual(member.host, "desk")

    def test_an_explicit_seat_overrides_the_derivation(self) -> None:
        (member,) = self.resolve([entry(host=None, seat="mine")])
        self.assertEqual((member.host, member.seat), ("box", "mine"))

    def test_a_member_already_joined_keeps_its_bundles_seat(self) -> None:
        (member,) = self.resolve([entry(host=None, seat=None)], bundles={"devbox": "proj.devbox+box.lxc"},
                                 roster={"proj.devbox+box.lxc": False})
        self.assertEqual(member.seat, "devbox")

    def test_a_container_joined_when_its_name_was_the_seat_keeps_that_handle(self) -> None:
        # An entry written before seats were derived: host given, seat omitted, joined by name.
        (member,) = self.resolve([entry(seat=None)], roster={"proj.devbox+box.lxc": False})
        self.assertEqual((member.host, member.seat), ("box", "devbox"))

    def test_a_parked_seat_returns(self) -> None:
        (parked,) = self.resolve([entry(host=None, seat=None)], roster={"proj.1+box.lxc": True})
        self.assertEqual(parked.seat, "1")

    def test_an_active_seat_nobody_declared_is_refused_as_a_possible_lost_bundle(self) -> None:
        with self.assertRaisesRegex(members.MemberError, r"agent@devbox in team .*proj\.1\+box\.lxc.*state: absent.*`seat` explicitly"):
            self.resolve([entry(host=None, seat=None)], roster={"proj.1+box.lxc": False})

    def test_an_active_seat_another_declared_container_holds_by_its_bundle_is_skipped(self) -> None:
        # The holder is later in the file than the entry being derived: its bundle still claims the seat.
        new, held = self.resolve([entry(host=None, seat=None, container="box-b"),
                                  entry(host=None, seat=None, container="box-a")],
                                 roster={"proj.1+box.lxc": False}, bundles={"box-a": "proj.1+box.lxc"})
        self.assertEqual((new.seat, held.seat), ("2", "1"))

    def test_the_preview_cannot_see_bundles_so_it_skips_an_active_seat(self) -> None:
        (member,) = members.resolve_members(members.parse_members([entry(host=None, seat=None)]),
                                            identify=lambda member: self.IDENTITY,
                                            roster=lambda team: {"proj.1+box.lxc": False},
                                            bundle=lambda member: None, unheld_ok=True)
        self.assertEqual(member.seat, "2")

    def test_the_derived_repo_is_carried_on_the_resolved_entry(self) -> None:
        (member,) = self.resolve([entry(host=None, seat=None)])
        self.assertEqual(member.repo, "proj")

    def test_a_declared_repo_that_differs_from_the_checkouts_is_refused(self) -> None:
        with self.assertRaisesRegex(members.MemberError, "repo: other.*proj"):
            self.resolve([entry(host=None, seat=None, repo="other")])

    def test_absent_entries_are_left_for_their_bundle_to_name(self) -> None:
        (member,) = self.resolve([entry(host=None, seat=None, state="absent")])
        self.assertEqual((member.host, member.seat), (None, None))

    def test_one_user_whose_checkouts_assign_two_hosts_is_refused(self) -> None:
        hosts = iter(["box", "desk"])
        with self.assertRaisesRegex(members.MemberError, "two hosts"):
            members.resolve_members(
                members.parse_members([entry(host=None, seat=None), entry(host=None, seat=None, team=OTHER_TEAM)]),
                identify=lambda member: derive.Identity("proj", next(hosts)), roster=lambda team: {},
                bundle=lambda member: None)

    def test_an_unusable_checkout_is_refused_naming_the_entry(self) -> None:
        def broken(member: members.Member) -> derive.Identity:
            raise derive.DeriveError("no origin")

        with self.assertRaisesRegex(members.MemberError, "agent@devbox.*no origin"):
            members.resolve_members(members.parse_members([entry(host=None, seat=None)]), identify=broken,
                                    roster=lambda team: {}, bundle=lambda member: None)


class DerivedPresentPhaseTest(Fixture):
    """The play, for entries that leave host and seat out."""

    def derived(self, **overrides: object) -> dict:
        return entry(**{"host": None, "seat": None, **overrides})

    def test_the_handle_is_derived_from_the_checkout_and_joined(self) -> None:
        self.run_phase("present", [self.derived()])
        self.assertEqual(self.reads, [("devbox", "agent", CHECKOUT)])
        self.assertEqual(sorted(self.bus.members), ["proj.1+box.lxc"])
        self.assertEqual(self.env_file().read_text(), f"PINGBUS_TEAMS={TEAM}\nHOOKS_DAEMON_HOSTNAME=box\n")

    def test_a_checkout_assigning_no_role_is_local(self) -> None:
        self.env_local = None
        self.guest.suggest = "--repo=proj --host=local --type=lxc"
        self.run_phase("present", [self.derived()])
        self.assertEqual(sorted(self.bus.members), ["proj.1+local.lxc"])

    def test_a_second_run_changes_nothing_and_keeps_the_seat(self) -> None:
        self.run_phase("present", [self.derived()])
        self.assertEqual(self.run_phase("present", [self.derived()]), [])
        self.assertEqual(sorted(self.bus.members), ["proj.1+box.lxc"])
        self.assertEqual(self.bus.minted, 1)

    def test_an_active_seat_nobody_declared_is_not_taken_and_not_skipped(self) -> None:
        # It may be this container's, whose bundle is gone, or another's joined by hand: unknowable.
        self.bus.members["proj.1+box.lxc"] = ["worker", False]
        with self.assertRaisesRegex(members.MemberError, r"proj\.1\+box\.lxc.*state: absent.*`seat` explicitly"):
            self.run_phase("present", [self.derived()])
        self.assertEqual(sorted(self.bus.members), ["proj.1+box.lxc"])

    def test_an_active_seat_is_passed_by_naming_another_one(self) -> None:
        self.bus.members["proj.1+box.lxc"] = ["worker", False]
        self.run_phase("present", [self.derived(seat="2")])
        self.assertEqual(sorted(self.bus.members), ["proj.1+box.lxc", "proj.2+box.lxc"])

    def test_a_lost_bundle_does_not_silently_give_the_member_a_new_handle(self) -> None:
        self.run_phase("present", [self.derived()])
        shutil.rmtree(self.bundle_dir())
        with self.assertRaisesRegex(members.MemberError, r"proj\.1\+box\.lxc.*state: absent.*`seat` explicitly"):
            self.run_phase("present", [self.derived()])
        self.assertEqual(sorted(self.bus.members), ["proj.1+box.lxc"])
        self.assertEqual(self.bus.minted, 1)

    def test_a_lost_bundle_is_refused_the_same_way_with_the_seat_named(self) -> None:
        self.run_phase("present", [self.derived()])
        shutil.rmtree(self.bundle_dir())
        with self.assertRaisesRegex(members.MemberError, "holds no bundle"):
            self.run_phase("present", [self.derived(seat="1")])

    def test_the_container_cannot_choose_the_repo_of_the_handle(self) -> None:
        # Its pingbus says --repo=other; the checkout, read from the host's side, says proj.
        self.guest.suggest = "--repo=other --host=box --type=lxc"
        with self.assertRaisesRegex(members.MemberError, "is repo other, and the entry says proj"):
            self.run_phase("present", [self.derived()])
        self.assertEqual(self.bus.members, {})
        self.assertEqual(self.bus.minted, 0)

    def test_the_handle_is_minted_with_the_derived_repo(self) -> None:
        self.run_phase("present", [self.derived()])
        (add,) = [call for call in self.bus.calls if call[0] == "add-member"]
        self.assertIn("--repo=proj", add)

    def test_an_entry_with_host_and_seat_does_not_read_the_checkout(self) -> None:
        self.run_phase("present", [entry()])
        self.assertEqual(self.reads, [])
        self.assertEqual(sorted(self.bus.members), [HANDLE])

    def test_a_container_joined_under_its_name_before_seats_were_derived_keeps_its_handle(self) -> None:
        self.run_phase("present", [entry()])
        self.assertEqual(self.run_phase("present", [entry(seat=None)]), [])
        self.assertEqual(sorted(self.bus.members), [HANDLE])

    def test_a_checkout_that_cannot_be_read_is_refused_before_anything_changes(self) -> None:
        self.unreadable = "container devbox is unprivileged"
        with self.assertRaisesRegex(members.MemberError, "unprivileged"):
            self.run_phase("present", [self.derived()])
        self.assertEqual(self.bus.members, {})
        self.assertFalse(self.env_file().exists())

    def test_what_the_reader_printed_is_judged(self) -> None:
        self.remote_config = "[remote \"origin\"]\n\turl = https://h/o/\x1b[31mred.git\n"
        with self.assertRaisesRegex(members.MemberError, "plain text"):
            self.run_phase("present", [self.derived()])

    def test_suggest_handle_must_agree_with_the_derived_host(self) -> None:
        self.guest.suggest = "--repo=proj --host=other --type=lxc"
        with self.assertRaisesRegex(members.MemberError, "suggest-handle gives --host=other"):
            self.run_phase("present", [self.derived()])


class SecondContainerTest(Fixture):
    """A team given a second LXC member by `ccy-teams lxc-add`: two seatless, hostless entries of
    one user name, in two containers, as the host_vars hold them (placeholder names)."""

    ENTRIES = [
        {"team": TEAM, "container": "container-a", "user": "agent", "checkout": "/srv/agent/repo-a"},
        {"team": TEAM, "container": "container-b", "user": "agent", "checkout": "/srv/agent/repo-b"},
    ]

    def setUp(self) -> None:
        super().setUp()
        self.origins = {"/srv/agent/repo-a": "proj-a", "/srv/agent/repo-b": "proj-b"}
        self.guests = {"container-a": FakeGuest(self.tmp / "guest-a"),
                       "container-b": FakeGuest(self.tmp / "guest-b")}

    def host(self) -> members.Host:
        return dataclasses.replace(super().host(), guest=lambda name: self.guests[name])

    def read_checkout(self, container: str, user: str, path: str) -> str:
        self.remote_config = f'[remote "origin"]\n\turl = https://github.com/example-org/{self.origins[path]}.git\n'
        self.guests[container].suggest = f"--repo={self.origins[path]} --host=box --type=lxc"
        return super().read_checkout(container, user, path)

    def test_two_repos_join_each_as_its_own_seat_one_and_a_second_run_changes_nothing(self) -> None:
        self.run_phase("present", self.ENTRIES)
        self.assertEqual(sorted(self.bus.members), ["proj-a.1+box.lxc", "proj-b.1+box.lxc"])
        self.assertEqual(self.run_phase("present", self.ENTRIES), [])
        self.assertEqual(self.bus.minted, 2)

    def test_the_second_container_of_one_repo_takes_the_next_seat_and_the_first_keeps_its_own(self) -> None:
        self.origins["/srv/agent/repo-b"] = "proj-a"
        self.run_phase("present", self.ENTRIES[:1])
        self.assertEqual(sorted(self.bus.members), ["proj-a.1+box.lxc"])
        self.run_phase("present", self.ENTRIES)
        self.assertEqual(sorted(self.bus.members), ["proj-a.1+box.lxc", "proj-a.2+box.lxc"])
        self.assertEqual(self.run_phase("present", self.ENTRIES), [])
        self.assertEqual(self.bus.minted, 2)

    def test_a_member_joined_first_is_never_renumbered_by_an_entry_written_before_it(self) -> None:
        self.origins["/srv/agent/repo-b"] = "proj-a"
        self.run_phase("present", self.ENTRIES[1:])
        self.run_phase("present", self.ENTRIES)
        held = {name: json.loads((self.tmp / name / "home" / "agent" / ".config" / "pingbus" / TEAM /
                                  "member.json").read_text())["user_id"] for name in ("guest-a", "guest-b")}
        self.assertEqual(held, {"guest-a": f"@proj-a.2+box.lxc:{TEAM}.agent-bus.internal",
                                "guest-b": f"@proj-a.1+box.lxc:{TEAM}.agent-bus.internal"})
        self.assertEqual(self.run_phase("present", self.ENTRIES), [])


class ServiceTest(Fixture):
    """Plan 00203 gap 9: an entry with `service: true` gets its session supervised inside the
    container: a system unit, enabled in the container's multi-user.target, that runs
    `agent-bus-claude` as the agent user under tmux and restarts it on any exit; the container
    must start at host boot (lxc.start.auto) and run systemd, and tmux must be there."""

    UNIT = "agent-bus-claude-agent.service"

    def unit_file(self) -> pathlib.Path:
        return pathlib.Path(self.guest.unit_dir) / self.UNIT

    def test_the_session_is_supervised_and_a_second_run_changes_nothing(self) -> None:
        changed = self.run_phase("present", [entry(service=True)])
        text = self.unit_file().read_text(encoding="utf-8")
        unit = parse_unit(text)
        service = unit["Service"]
        self.assertEqual(service["User"], ["agent"])
        self.assertEqual(service["WorkingDirectory"], [CHECKOUT])
        self.assertEqual(service["Restart"], ["always"])
        self.assertEqual(service["Type"], ["forking"])
        self.assertIn("/usr/bin/tmux -L agent-bus-claude new-session -d -s agent-bus-claude "
                      f"{members.GUEST_BIN}/agent-bus-claude", service["ExecStart"][0])
        self.assertEqual(unit["Unit"]["StartLimitIntervalSec"], ["0"])
        self.assertEqual(unit["Install"]["WantedBy"], ["multi-user.target"])
        self.assertEqual(stat.S_IMODE(self.unit_file().stat().st_mode), 0o644)
        self.assertEqual(self.guest.units[self.UNIT], {"enabled": True, "active": True})
        self.assertIn(f"container devbox: {self.UNIT} installed", changed)
        self.assertIn(f"container devbox: {self.UNIT} enabled", changed)
        self.assertIn(f"container devbox: {self.UNIT} started", changed)

        self.guest.systemctl.clear()
        self.assertEqual(self.run_phase("present", [entry(service=True)]), [])
        self.assertEqual([call[0] for call in self.guest.systemctl], ["show"])

    def test_a_changed_unit_restarts_the_session(self) -> None:
        self.run_phase("present", [entry(service=True)])
        changed = self.run_phase("present", [entry(service=True, checkout="/srv/other")])
        self.assertIn(f"container devbox: {self.UNIT} restarted (its unit changed)", changed)
        self.assertIn(("daemon-reload",), self.guest.systemctl)

    def test_a_session_that_stopped_is_started(self) -> None:
        self.run_phase("present", [entry(service=True)])
        self.guest.units[self.UNIT]["active"] = False
        self.assertEqual(self.run_phase("present", [entry(service=True)]),
                         [f"container devbox: {self.UNIT} started"])

    def test_turning_the_service_off_removes_the_unit(self) -> None:
        self.run_phase("present", [entry(service=True)])
        changed = self.run_phase("present", [entry()])
        self.assertFalse(self.unit_file().exists())
        self.assertEqual(self.guest.units[self.UNIT], {"enabled": False, "active": False})
        self.assertIn(f"container devbox: {self.UNIT} stopped and removed", changed)
        self.assertEqual(self.run_phase("present", [entry()]), [])

    def test_leaving_the_last_team_removes_the_unit(self) -> None:
        self.run_phase("present", [entry(service=True)])
        changed = self.run_phase("absent", [entry(service=True, state="absent")])
        self.assertFalse(self.unit_file().exists())
        self.assertIn(f"container devbox: {self.UNIT} stopped and removed", changed)

    def test_leaving_one_team_of_two_keeps_the_unit(self) -> None:
        both = [entry(service=True), entry(service=True, team=OTHER_TEAM, state="absent")]
        self.run_phase("present", [entry(service=True)], teams_present=(TEAM, OTHER_TEAM))
        self.run_phase("absent", both, teams_present=(TEAM, OTHER_TEAM))
        self.assertTrue(self.unit_file().exists())

    def test_a_container_that_does_not_start_at_boot_is_refused_before_any_change(self) -> None:
        self.autostart = set()
        with self.assertRaisesRegex(members.MemberError, "lxc.start.auto = 1"):
            self.run_phase("present", [entry(service=True)])
        self.assertEqual(self.bus.calls, [])
        self.assertFalse(self.unit_file().exists())

    def test_systemd_and_tmux_are_required_before_any_change(self) -> None:
        for attr, value, words in (("systemd", False, "systemd"), ("tmux", None, "tmux")):
            with self.subTest(attr=attr):
                setattr(self.guest, attr, value)
                with self.assertRaisesRegex(members.MemberError, words):
                    self.run_phase("present", [entry(service=True)])
                self.assertEqual(self.bus.calls, [])
                setattr(self.guest, attr, True if attr == "systemd" else "/usr/bin/tmux")

    def test_an_entry_without_service_asks_nothing_of_the_container(self) -> None:
        self.autostart = set()
        self.guest.tmux = None
        self.run_phase("present", [entry()])
        self.assertEqual([call[0] for call in self.guest.systemctl], ["show"])

    def test_a_container_without_systemd_is_fine_for_an_entry_without_service(self) -> None:
        """Review B2: a member that never asked for a unit must not need systemd in its container."""
        self.guest.systemd = False
        self.assertIn(f"{HANDLE} joined team {TEAM} (agent@devbox)", self.run_phase("present", [entry()]))
        self.run_phase("absent", [entry(state="absent")])
        self.assertEqual(self.guest.systemctl, [])

    def test_a_unit_file_left_in_a_container_without_systemd_is_removed(self) -> None:
        self.guest.systemd = False
        self.unit_file().parent.mkdir(parents=True)
        self.unit_file().write_text("stale\n", encoding="utf-8")
        changed = self.run_phase("present", [entry()])
        self.assertFalse(self.unit_file().exists())
        self.assertIn(f"container devbox: {self.UNIT} removed", changed)
        self.assertEqual(self.guest.systemctl, [])

    def test_entries_of_one_user_agree_on_the_service_and_its_checkout(self) -> None:
        with self.assertRaisesRegex(members.MemberError, "service"):
            members.parse_members([entry(service=True), entry(team=OTHER_TEAM, seat="x")])
        with self.assertRaisesRegex(members.MemberError, "one checkout"):
            members.parse_members([entry(service=True), entry(team=OTHER_TEAM, seat="x", service=True,
                                                              checkout="/srv/other")])
        with self.assertRaisesRegex(members.MemberError, "service must be true or false"):
            members.parse_members([entry(service="yes")])

    def test_a_path_systemd_would_read_otherwise_is_refused(self) -> None:
        for bad in ("/srv/my proj", "/srv/100%", "/srv/a\\b"):
            with self.subTest(bad=bad), self.assertRaises(members.MemberError):
                members.render_member_unit("agent", "/srv/agent-home", bad, "/usr/bin/tmux")


def parse_unit(text: str) -> dict[str, dict[str, list[str]]]:
    sections: dict[str, dict[str, list[str]]] = {}
    current: dict[str, list[str]] = {}
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        if line.startswith("["):
            current = sections.setdefault(line.strip("[]"), {})
            continue
        key, _, value = line.partition("=")
        current.setdefault(key, []).append(value)
    return sections


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


class StoppedFixture(Fixture):
    """Two containers, devbox and box2, each with its own guest."""

    def setUp(self) -> None:
        super().setUp()
        self.guests = {name: FakeGuest(self.tmp / name) for name in ("devbox", "box2")}

    def host(self) -> members.Host:
        return dataclasses.replace(super().host(), guest=lambda name: self.guests[name])

    def guest_runs(self) -> int:
        return sum(len(g.agent_runs) for g in self.guests.values())

    def kinds(self) -> list[tuple[str, str, str]]:
        return [(r.kind, r.team, r.label) for r in self.reports]


class StoppedContainerTest(StoppedFixture):
    """SPEC-lxc-stopped-members.md (Plan 00203 Task 2.6): a container that is not running never
    fails the play. A member already joined is skipped, one not joined is deferred, and every
    member whose container runs is joined as before in the same run."""

    def test_a_joined_member_whose_container_stopped_is_skipped_and_nothing_enters_it(self) -> None:
        self.run_phase("present", [entry()])
        self.assertEqual(self.item()["status"], "joined")
        self.states = {"devbox": "STOPPED"}
        runs, self.bus.calls = self.guest_runs(), []
        self.assertEqual(self.run_phase("present", [entry()]), [])
        self.assertEqual(self.kinds(), [("SKIPPED", TEAM, "agent@devbox")])
        self.assertIn("STOPPED", self.reports[0].why)
        self.assertIn("refreshes", self.reports[0].why)
        self.assertEqual(self.guest_runs(), runs)
        self.assertEqual([call[0] for call in self.bus.calls], ["list"])
        self.assertEqual((self.item()["status"], self.item()["handle"]), ("stopped", HANDLE))

    def test_a_member_never_joined_whose_container_is_stopped_is_deferred_and_the_others_join(self) -> None:
        self.states = {"box2": "STOPPED"}
        both = [entry(), entry(container="box2")]
        changed = self.run_phase("present", both)
        self.assertIn(f"{HANDLE} joined team {TEAM} (agent@devbox)", changed)
        self.assertEqual(self.kinds(), [("DEFERRED", TEAM, "agent@box2")])
        why = self.reports[0].why
        self.assertIn("box2 is STOPPED", why)
        self.assertIn("start it", why)
        self.assertIn("not to declare it yet", why)
        self.assertEqual(sorted(self.bus.members), [HANDLE])
        self.assertEqual(self.item("box2")["status"], "deferred")
        self.assertEqual(self.guests["box2"].agent_runs, [])
        # Once it is up, the next run joins it; the one after changes nothing.
        self.states = {}
        changed = self.run_phase("present", both)
        self.assertIn(f"proj.box2+box.lxc joined team {TEAM} (agent@box2)", changed)
        self.assertEqual(self.reports, [])
        self.assertEqual(self.item("box2")["status"], "joined")
        self.assertEqual(self.run_phase("present", both), [])

    def test_a_stopped_member_whose_handle_is_parked_is_deferred_not_skipped(self) -> None:
        self.run_phase("present", [entry()])
        self.bus.members[HANDLE][1] = True
        self.states = {"devbox": "STOPPED"}
        self.run_phase("present", [entry()])
        self.assertEqual(self.kinds(), [("DEFERRED", TEAM, "agent@devbox")])

    def test_an_entry_naming_host_seat_and_repo_is_known_joined_without_a_record(self) -> None:
        self.run_phase("present", [entry(repo="proj")])
        self.records.clear()
        self.states = {"devbox": "STOPPED"}
        self.run_phase("present", [entry(repo="proj")])
        self.assertEqual(self.kinds(), [("SKIPPED", TEAM, "agent@devbox")])

    def test_a_record_for_another_checkout_does_not_make_the_member_joined(self) -> None:
        self.run_phase("present", [entry()])
        self.states = {"devbox": "STOPPED"}
        self.run_phase("present", [entry(checkout="/srv/other")])
        self.assertEqual(self.kinds(), [("DEFERRED", TEAM, "agent@devbox")])

    def test_another_state_than_stopped_is_not_entered_either(self) -> None:
        self.states = {"devbox": "FROZEN"}
        self.run_phase("present", [entry()])
        self.assertEqual(self.kinds(), [("DEFERRED", TEAM, "agent@devbox")])
        self.assertIn("FROZEN", self.reports[0].why)

    def test_a_failure_of_a_running_member_still_fails_while_another_is_stopped(self) -> None:
        self.states = {"box2": "STOPPED"}
        self.guests["devbox"].gh_ok = False
        with self.assertRaisesRegex(members.MemberError, "LXC container devbox"):
            self.run_phase("present", [entry(), entry(container="box2")])
        self.assertEqual(self.records, {})

    def test_a_container_that_does_not_exist_still_fails(self) -> None:
        self.states = {"box2": None}
        with self.assertRaisesRegex(members.MemberError, "box2 does not exist"):
            self.run_phase("present", [entry(), entry(container="box2")])

    def test_every_container_stopped_changes_nothing_and_does_not_fail(self) -> None:
        self.run_phase("present", [entry(), entry(container="box2")])
        self.state = "STOPPED"
        self.assertEqual(self.run_phase("present", [entry(), entry(container="box2")]), [])
        self.assertEqual([r.kind for r in self.reports], ["SKIPPED", "SKIPPED"])


class StoppedSeatTest(Fixture):
    """L4: the seat of a stopped member is never given to a member added beside it. Two
    containers of one repo, as SecondContainerTest's entries."""

    ENTRIES = SecondContainerTest.ENTRIES

    def setUp(self) -> None:
        super().setUp()
        self.guests = {"container-a": FakeGuest(self.tmp / "guest-a"),
                       "container-b": FakeGuest(self.tmp / "guest-b")}
        for guest in self.guests.values():
            guest.suggest = "--repo=proj-a --host=box --type=lxc"
        self.remote_config = '[remote "origin"]\n\turl = https://github.com/example-org/proj-a.git\n'

    def host(self) -> members.Host:
        return dataclasses.replace(super().host(), guest=lambda name: self.guests[name])

    def test_a_skipped_members_seat_is_claimed(self) -> None:
        self.run_phase("present", self.ENTRIES[:1])
        self.assertEqual(sorted(self.bus.members), ["proj-a.1+box.lxc"])
        self.states = {"container-a": "STOPPED"}
        self.run_phase("present", self.ENTRIES)
        self.assertEqual(sorted(self.bus.members), ["proj-a.1+box.lxc", "proj-a.2+box.lxc"])
        self.assertEqual([(r.kind, r.label) for r in self.reports], [("SKIPPED", "agent@container-a")])
        self.states = {}
        self.assertEqual(self.run_phase("present", self.ENTRIES), [])

    def test_a_deferred_member_with_no_record_does_not_block_the_next_seat(self) -> None:
        # Joined before this host kept records: its seat is active, and no entry can name it
        # while its container is stopped. The new member passes over it rather than failing.
        self.run_phase("present", self.ENTRIES[:1])
        self.records.clear()
        self.states = {"container-a": "STOPPED"}
        self.run_phase("present", self.ENTRIES)
        self.assertEqual(sorted(self.bus.members), ["proj-a.1+box.lxc", "proj-a.2+box.lxc"])
        self.assertEqual([(r.kind, r.label) for r in self.reports], [("DEFERRED", "agent@container-a")])
        self.assertIn("no record that it joined", self.reports[0].why)
        # Up again, it keeps its own seat by its bundle.
        self.states = {}
        self.assertEqual(self.run_phase("present", self.ENTRIES), [])
        self.assertEqual(self.item("container-a")["handle"], "proj-a.1+box.lxc")

    def test_with_no_stopped_member_a_lost_bundle_is_still_refused(self) -> None:
        self.run_phase("present", self.ENTRIES[:1])
        shutil.rmtree(self.tmp / "guest-a" / "home" / "agent" / ".config" / "pingbus" / TEAM)
        with self.assertRaisesRegex(members.MemberError, "state: absent"):
            self.run_phase("present", self.ENTRIES)


class StoppedAbsentTest(StoppedFixture):
    """L6: a member taken off a team while its container is stopped."""

    def test_its_handle_is_parked_from_the_host_and_the_rest_waits_for_the_container(self) -> None:
        self.run_phase("present", [entry()])
        self.states = {"devbox": "STOPPED"}
        changed = self.run_phase("absent", [entry(state="absent")])
        self.assertIn(f"parked {HANDLE} in team {TEAM}", changed)
        self.assertTrue(self.bus.members[HANDLE][1])
        self.assertEqual(self.kinds(), [("DEFERRED", TEAM, "agent@devbox")])
        self.assertIn("bundle", self.reports[0].why)
        self.assertTrue((self.tmp / "devbox" / "home" / "agent" / ".config" / "pingbus" / TEAM).exists())
        self.assertEqual(self.item()["status"], "deferred")
        # Up again: the bundle and env file go, and nothing is parked twice.
        self.states, self.bus.calls = {}, []
        self.run_phase("absent", [entry(state="absent")])
        self.assertNotIn("park-member", [call[0] for call in self.bus.calls])
        self.assertFalse((self.tmp / "devbox" / "home" / "agent" / ".config" / "pingbus" / TEAM).exists())
        self.assertEqual(self.reports, [])
        self.assertEqual(self.item()["status"], "left")

    def test_with_no_known_handle_nothing_is_parked_and_it_is_deferred(self) -> None:
        self.run_phase("present", [entry()])
        self.records.clear()
        self.states = {"devbox": "STOPPED"}
        self.run_phase("absent", [entry(state="absent")])
        self.assertFalse(self.bus.members[HANDLE][1])
        self.assertEqual(self.kinds(), [("DEFERRED", TEAM, "agent@devbox")])
        self.assertIn("not parked", self.reports[0].why)


class MemberRecordTest(StoppedFixture):
    """L8: what the play last found for each entry, read by ccy-teams."""

    def test_a_join_is_recorded_once_and_a_second_run_reports_no_change(self) -> None:
        self.run_phase("present", [entry()])
        self.assertIn(f"the LXC member record of team {TEAM}", self.changed[-1])
        self.assertEqual(self.records[TEAM], {"version": 1, "team": TEAM, "members": [{
            "container": "devbox", "user": "agent", "state": "present", "status": "joined",
            "handle": HANDLE, "checkout": CHECKOUT, "why": "", "since": int(self.clock)}]})
        self.clock += 60
        self.run_phase("present", [entry()])
        self.assertEqual(self.changed, [])
        self.assertEqual(self.item()["since"], int(self.clock) - 60)

    def test_since_moves_when_the_status_does(self) -> None:
        self.run_phase("present", [entry()])
        self.clock += 60
        self.states = {"devbox": "STOPPED"}
        self.run_phase("present", [entry()])
        self.assertEqual(self.item()["since"], int(self.clock))

    def test_each_phase_keeps_the_others_items_and_an_undeclared_one_is_dropped(self) -> None:
        both = [entry(), entry(container="box2")]
        self.run_phase("present", both)
        self.run_phase("absent", [entry(), entry(container="box2", state="absent")])
        self.assertEqual({i["container"]: i["status"] for i in self.records[TEAM]["members"]},
                         {"devbox": "joined", "box2": "left"})
        self.run_phase("present", [entry()])
        self.assertEqual([i["container"] for i in self.records[TEAM]["members"]], ["devbox"])

    def test_a_team_not_declared_present_has_no_record(self) -> None:
        self.run_phase("present", [entry()])
        self.active = set()
        self.run_phase("absent", [entry(state="absent")], teams_present=())
        self.assertNotIn(TEAM, self.records)


class RecordSweepTest(StoppedFixture):
    """L8 for a team's last entry: a team with no entry keeps no record, so the board and
    ccy-teams apply never ask the owner to start a container nobody declares any more. The
    present phase sweeps it, even when no entry is left at all; with agent_bus_only_teams
    only the teams in scope, since the member list is scoped and --teams-present is not."""

    def main(self, entries: Sequence[dict], *extra: str, phase: str = "present") -> tuple[int, str]:
        out = io.StringIO()
        code = members.main(["--phase", phase, "--members", json.dumps(list(entries)),
                             "--teams-present", json.dumps([TEAM, OTHER_TEAM]), *extra],
                            host_factory=self.host, stdout=out, stderr=io.StringIO())
        return code, out.getvalue()

    def deferred_in_both_teams(self) -> None:
        self.states = {"devbox": "STOPPED", "box2": "STOPPED"}
        self.run_phase("present", [entry(), entry(container="box2", team=OTHER_TEAM)],
                       teams_present=(TEAM, OTHER_TEAM))
        self.assertEqual(sorted(self.records), [TEAM, OTHER_TEAM])

    def test_the_record_of_a_team_whose_last_entry_went_is_removed(self) -> None:
        self.deferred_in_both_teams()
        code, out = self.main([entry(container="box2", team=OTHER_TEAM)])
        self.assertEqual(code, 0)
        self.assertEqual(sorted(self.records), [OTHER_TEAM])
        self.assertIn(f"CHANGED\tthe LXC member record of team {TEAM}\n", out)

    def test_an_empty_member_list_removes_every_record(self) -> None:
        self.deferred_in_both_teams()
        code, out = self.main([])
        self.assertEqual((code, self.records), (0, {}))
        self.assertIn(f"CHANGED\tthe LXC member record of team {OTHER_TEAM}\n", out)
        self.assertEqual(self.main([]), (0, ""))

    def test_a_scoped_run_sweeps_only_the_teams_in_scope(self) -> None:
        self.deferred_in_both_teams()
        code, _ = self.main([], "--only-teams", json.dumps([TEAM]))
        self.assertEqual((code, sorted(self.records)), (0, [OTHER_TEAM]))

    def test_a_scoped_run_keeps_the_record_of_a_team_in_scope_that_has_entries(self) -> None:
        self.deferred_in_both_teams()
        code, _ = self.main([entry(container="box2", team=OTHER_TEAM)], "--only-teams", json.dumps([OTHER_TEAM]))
        self.assertEqual((code, sorted(self.records)), (0, [TEAM, OTHER_TEAM]))

    def test_the_absent_phase_sweeps_nothing(self) -> None:
        self.deferred_in_both_teams()
        self.assertEqual(self.main([], phase="absent"), (0, ""))
        self.assertEqual(sorted(self.records), [TEAM, OTHER_TEAM])

    def test_only_teams_must_be_a_list_of_names(self) -> None:
        code, _ = self.main([], "--only-teams", json.dumps(TEAM))
        self.assertEqual(code, 78)


class WriteRecordTest(unittest.TestCase):
    def test_a_stamp_group_that_does_not_exist_is_a_refusal_not_a_traceback(self) -> None:
        with mock.patch.object(members.teamfile, "read_stamp_group", return_value="nosuchgroup"), \
                mock.patch.object(members.grp, "getgrnam", side_effect=KeyError("getgrnam(): name not found")), \
                self.assertRaisesRegex(members.MemberError, "group nosuchgroup .*does not exist"):
            members._write_record(TEAM, None)


class MarkerLinesTest(StoppedFixture):
    """L1, L2, L7: the lines the play reads and shows."""

    def test_skipped_and_deferred_lines_then_one_summary(self) -> None:
        self.run_phase("present", [entry()])
        self.states = {"devbox": "STOPPED", "box2": "STOPPED"}
        out = io.StringIO()
        code = members.main(["--phase", "present", "--members", json.dumps([entry(), entry(container="box2")]),
                             "--teams-present", json.dumps([TEAM])], host_factory=self.host,
                            stdout=out, stderr=io.StringIO())
        self.assertEqual(code, 0)
        lines = [line for line in out.getvalue().splitlines() if not line.startswith("CHANGED\t")]
        self.assertTrue(lines[0].startswith(f"SKIPPED\t{TEAM}\tagent@devbox\t"), lines)
        self.assertTrue(lines[1].startswith(f"DEFERRED\t{TEAM}\tagent@box2\t"), lines)
        self.assertTrue(lines[-1].startswith("LXC-SUMMARY\t"), lines)
        self.assertIn("agent@devbox (team-a, skipped)", lines[-1])
        self.assertIn("agent@box2 (team-a, deferred)", lines[-1])

    def test_no_summary_when_nothing_was_held_back(self) -> None:
        out = io.StringIO()
        members.main(["--phase", "present", "--members", json.dumps([entry()]),
                      "--teams-present", json.dumps([TEAM])], host_factory=self.host,
                     stdout=out, stderr=io.StringIO())
        self.assertTrue(all(line.startswith("CHANGED\t") for line in out.getvalue().splitlines()))


def kit_sync_argv(guest: FakeGuest, python: str = NEW_PY) -> list[str]:
    return [python, "-I", "-c", members.KIT_SYNC_PY, guest.kit_parent, guest.bin_dir, str(guest.kit_owner), python]


class KitSyncTest(Fixture):
    def sync(self) -> bytes:
        return self.guest.root(kit_sync_argv(self.guest), "kit", members.kit_tar(self.kit_parent))

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


class InterpreterTest(Fixture):
    """Plan 00213 Task 1.4 regression: the kit's pingbus was built to start /usr/bin/python3,
    which in an EL9 member is the 3.9 system interpreter, so the member step failed for every
    LXC member (exit 78, "this container has 3.9.21"). Each member's interpreter is found in
    the member, once, and named on the first line of its copy of pingbus."""

    def test_an_el9_member_runs_everything_under_its_newest_python(self) -> None:
        self.run_phase("present", [entry()])
        pingbus = (pathlib.Path(self.guest.kit_parent) / "kit" / "pingbus").read_bytes()
        self.assertEqual(pingbus, f"#!{NEW_PY}\n".encode() + b"zipapp")
        self.assertEqual(set(self.guest.interpreters), {NEW_PY})
        self.assertEqual(self.guest.python_searches, 1)
        self.assertEqual(self.run_phase("present", [entry()]), [])

    def test_a_member_with_only_usr_bin_python3_new_enough_keeps_it(self) -> None:
        self.guest = FakeGuest(self.tmp / "fedora", interpreters={"usr/bin/python3.14": NEW,
                                                                  "usr/bin/python3": "python3.14"})
        self.run_phase("present", [entry()])
        pingbus = (pathlib.Path(self.guest.kit_parent) / "kit" / "pingbus").read_bytes()
        self.assertTrue(pingbus.startswith(b"#!/usr/bin/python3.14\n"), pingbus)

    def test_no_python_new_enough_is_refused_before_any_change(self) -> None:
        self.guest = FakeGuest(self.tmp / "old", interpreters={"usr/bin/python3.9": OLD, "usr/bin/python3.10": OLD,
                                                               "usr/bin/python3": "python3.9"})
        with self.assertRaisesRegex(members.MemberError, r"container old has no Python 3\.11 or later "
                                    r"in /usr/local/bin or /usr/bin \(it has python3\.9, python3\.10\)"):
            self.run_phase("present", [entry(container="old")])
        self.assertFalse(pathlib.Path(self.guest.kit_parent).exists())
        self.assertEqual(self.bus.calls, [])

    def test_the_newest_by_number_is_chosen_and_others_are_ignored(self) -> None:
        listing = ("/usr/local/bin/python3.9\n/usr/local/bin/python3.12-config\n/usr/bin/python3.12\n"
                   "/usr/local/bin/python3.12\n/usr/bin/python3.11\n/usr/bin/python3.x\n")
        self.assertEqual(members.choose_python(listing, "box"), "/usr/local/bin/python3.12")
        self.assertEqual(members.choose_python("/usr/bin/python3.10\n/usr/bin/python3.11\n", "box"),
                         "/usr/bin/python3.11")

    def test_the_kit_gate_is_pingbus_minimum(self) -> None:
        self.assertIn(f"sys.version_info < {config.MIN_PYTHON}", members.KIT_SYNC_PY)
        self.assertEqual(members.MIN_PYTHON, config.MIN_PYTHON)

    def test_the_real_zipapp_runs_after_its_first_line_is_rewritten(self) -> None:
        """A longer first line moves every offset in the archive; zipimport finds its entries
        from the end, as zipapp's own interpreter change relies on."""
        (self.kit_parent / "kit" / "pingbus").write_bytes(zipapp.build("pingbus", REPO_ROOT))
        self.guest.root(kit_sync_argv(self.guest), "kit", members.kit_tar(self.kit_parent))
        pingbus = pathlib.Path(self.guest.kit_parent) / "kit" / "pingbus"
        with pingbus.open("rb") as handle:
            self.assertEqual(handle.readline(), f"#!{NEW_PY}\n".encode())
        with tempfile.TemporaryDirectory() as cwd:
            done = subprocess.run([f"{self.guest.dir}{NEW_PY}", str(pingbus), "version"], cwd=cwd,
                                  capture_output=True, text=True, check=False, timeout=60)
        self.assertEqual(done.returncode, 0, done.stderr)

    def test_a_kit_pingbus_without_a_first_line_is_refused(self) -> None:
        (self.kit_parent / "kit" / "pingbus").write_bytes(b"zipapp")
        with self.assertRaisesRegex(members.MemberError, "no #! line"):
            self.guest.root(kit_sync_argv(self.guest), "kit", members.kit_tar(self.kit_parent))


def mode_of(path: pathlib.Path) -> int:
    return stat.S_IMODE(os.lstat(path).st_mode)


class UmaskTest(Fixture):
    """The guest scripts inherit the umask of whatever lxc-attach started, which has been 077:
    the kit's parent was then made 0700 and the agent user could not reach `kit/pingbus`
    (`env: pingbus: Permission denied`). What each script makes has the mode it names under
    any inherited umask, and a secret is never readable by others under a permissive one."""

    def with_umask(self, mask: int) -> None:
        old = os.umask(mask)
        self.addCleanup(os.umask, old)

    def kit_dirs(self) -> list[pathlib.Path]:
        parent = pathlib.Path(self.guest.kit_parent)
        return [parent.parent, parent, parent / "kit", parent / "kit" / "plugin"]

    def sync(self) -> list[str]:
        return self.guest.root(kit_sync_argv(self.guest), "kit", members.kit_tar(self.kit_parent)).decode().splitlines()

    def test_under_umask_077_the_kit_is_traversable(self) -> None:
        self.with_umask(0o077)
        self.run_phase("present", [entry()])
        for path in self.kit_dirs():
            self.assertEqual(mode_of(path), 0o755, path)
        kit = pathlib.Path(self.guest.kit_parent) / "kit"
        self.assertEqual(mode_of(kit / "pingbus"), 0o755)
        self.assertEqual(mode_of(kit / "plugin" / "plugin.json"), 0o644)

    def test_a_kit_directory_left_0700_is_repaired_and_said(self) -> None:
        self.sync()
        parent = pathlib.Path(self.guest.kit_parent)
        parent.chmod(0o700)
        (parent / "kit" / "plugin").chmod(0o700)
        said: list[str] = []
        changed: list[str] = []
        members._sync_kit(self.host(), self.guest, "devbox", changed.append, said.append)
        self.assertEqual(mode_of(parent), 0o755)
        self.assertEqual(mode_of(parent / "kit" / "plugin"), 0o755)
        for lines in (changed, said):
            self.assertIn(f"container devbox: kit repaired {parent} from 0700 to 0755", lines)
            self.assertIn(f"container devbox: kit repaired {parent / 'kit' / 'plugin'} from 0700 to 0755", lines)
        self.assertEqual(self.sync(), [])

    def test_a_kit_parent_that_is_a_symlink_is_refused_by_name(self) -> None:
        parent = pathlib.Path(self.guest.kit_parent)
        elsewhere = self.tmp / "elsewhere"
        elsewhere.mkdir(mode=0o700)
        parent.parent.mkdir(parents=True)
        parent.symlink_to(elsewhere)
        with self.assertRaisesRegex(members.MemberError, f"{parent} is not a directory"):
            self.sync()
        self.assertEqual(mode_of(elsewhere), 0o700)

    def test_a_kit_parent_others_can_write_or_not_ours_is_refused_not_repaired(self) -> None:
        parent = pathlib.Path(self.guest.kit_parent)
        parent.mkdir(parents=True)
        parent.chmod(0o775)
        with self.assertRaisesRegex(members.MemberError, "writable by others"):
            self.sync()
        self.assertEqual(mode_of(parent), 0o775)
        parent.chmod(0o700)
        self.guest.kit_owner = os.getuid() + 1
        with self.assertRaisesRegex(members.MemberError, f"{parent} is not owned by uid"):
            self.sync()
        self.assertEqual(mode_of(parent), 0o700)

    def test_under_umask_077_the_unit_directory_is_readable(self) -> None:
        self.with_umask(0o077)
        self.run_phase("present", [entry(service=True)])
        unit_dir = pathlib.Path(self.guest.unit_dir)
        for path in (unit_dir.parent.parent, unit_dir.parent, unit_dir):
            self.assertEqual(mode_of(path), 0o755, path)
        self.assertEqual(mode_of(unit_dir / ServiceTest.UNIT), 0o644)

    def test_secrets_stay_private_under_any_umask(self) -> None:
        home = self.tmp / "guest" / "home" / "agent"
        for mask in (0o000, 0o022, 0o077):
            with self.subTest(umask=oct(mask)):
                if home.exists():
                    shutil.rmtree(home)
                home.mkdir(parents=True, mode=0o700)
                self.bus.members.clear()
                old = os.umask(mask)
                try:
                    self.run_phase("present", [entry()])
                finally:
                    os.umask(old)
                bundle = self.bundle_dir()
                for path in (home / ".config", bundle.parent, bundle):
                    self.assertEqual(mode_of(path), 0o700, path)
                for name in ("member.json", "token", "README"):
                    self.assertEqual(mode_of(bundle / name), 0o600, name)
                self.assertEqual(mode_of(self.env_file()), 0o600)


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

    def test_autostart_is_read_from_the_containers_config(self) -> None:
        for printed, starts in ((b"lxc.start.auto = 1\n", True), (b"lxc.start.auto = 0\n", False),
                                (b"lxc.start.auto =\n", False)):
            with self.subTest(printed=printed):
                self.answer = (0, printed, b"")
                self.assertIs(members._autostarts("devbox"), starts)
        self.assertEqual(self.calls[0][0], ["lxc-info", "-n", "devbox", "-c", "lxc.start.auto"])
        self.answer = (1, b"", b"no such container")
        with self.assertRaisesRegex(members.MemberError, "no such container"):
            members._autostarts("devbox")

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


class ReadCheckoutTest(unittest.TestCase):
    """The real reader: the host's own python3 runs the shared script as an argument list, so no
    shell and no file the container or the user could change is loaded."""

    def test_it_runs_the_script_with_an_argument_list_in_isolated_mode(self) -> None:
        calls = []

        def run(argv: Sequence[str], stdin: bytes = b"") -> tuple[int, bytes, bytes]:
            calls.append(list(argv))
            return (0, b"/var/lib/lxc\n", b"") if argv[0] == "lxc-config" else (0, b'{"x": 1}', b"")

        with mock.patch.object(members, "_run", run):
            self.assertEqual(members.read_checkout("devbox", "agent", CHECKOUT), '{"x": 1}')
        self.assertEqual(calls[0], ["lxc-config", "lxc.lxcpath"])
        self.assertEqual(calls[1], ["python3", "-I", "-c", derive.ROOT_READ_PY, "probe", "/var/lib/lxc",
                                    "devbox", "agent", CHECKOUT])

    def test_a_failing_reader_is_refused_with_its_reason(self) -> None:
        def run(argv: Sequence[str], stdin: bytes = b"") -> tuple[int, bytes, bytes]:
            return (0, b"/var/lib/lxc\n", b"") if argv[0] == "lxc-config" else (1, b"", b"symbolic link")

        with mock.patch.object(members, "_run", run), self.assertRaisesRegex(members.MemberError, "symbolic link"):
            members.read_checkout("devbox", "agent", CHECKOUT)

    def test_a_lxc_path_that_is_not_a_path_is_refused(self) -> None:
        with mock.patch.object(members, "_run", lambda argv, stdin=b"": (0, b"relative\n", b"")), \
                self.assertRaises(members.MemberError):
            members.read_checkout("devbox", "agent", CHECKOUT)


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

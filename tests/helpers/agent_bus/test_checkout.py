"""Unit tests for helpers/agent_bus/checkout.py and the `agent-bus seat` commands.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_checkout

Spec: Plan 00161 DESIGN.md section 5.6 ("Seat commands"), 5.5 (the list rules, the seat
layout, reuse) and 5.2 (a new seat's `<host>`: the role `ccy.env.local` assigns, else
`local`, D49). Each test builds a real git checkout in a temporary directory; the host's
side (`systemctl is-active`, `agentbus0`'s address and `sudo agent-bus`) is replaced by a
recorder whose `add-member` places a bundle as the root wrapper would.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
from collections.abc import Sequence
from unittest import mock

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.agent_bus import checkout, cli
from helpers.pingbus import inbox, seat

TEAM_A, TEAM_B = "team-a", "team-b"
ROOM = "!" + "R" * 43
BUS_IP = "192.0.2.10"
T0_MS = 1_791_000_000_000
EX_USAGE, EX_BUSY, EX_CONFIG = 64, 75, 78

#: Git reads neither the machine's nor the user's configuration (helpers/CLAUDE.md).
GIT_ENV = {"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1", "LC_ALL": "C"}


def member_json(team: str, handle: str) -> str:
    sn = f"{team}.agent-bus.internal"
    return json.dumps({"protocol": 1, "team": team, "user_id": f"@{handle}:{sn}", "server_name": sn,
                       "base_url": f"http://{BUS_IP}:8448", "plain_http_hosts": [BUS_IP],
                       "token_file": "token", "admin": f"@admin:{sn}", "room": ROOM})


def arg(argv: Sequence[str], name: str) -> str:
    return next(a.split("=", 1)[1] for a in argv if a.startswith(f"--{name}="))


class FakeHost:
    """The host side: which teams run here, the bus address, and `sudo agent-bus`."""

    def __init__(self) -> None:
        self.active = {TEAM_A, TEAM_B}
        self.address = BUS_IP
        self.calls: list[tuple[tuple[str, ...], bool]] = []
        self.fail: dict[str, int] = {}

    def team_active(self, team: str) -> bool:
        return team in self.active

    def bus_address(self) -> str:
        if self.address is None:
            raise checkout.CheckoutError("agentbus0 is absent")
        return self.address

    def agent_bus(self, argv: Sequence[str], no_prompt: bool) -> tuple[int, str]:
        self.calls.append((tuple(argv), no_prompt))
        if argv[0] in self.fail:
            return self.fail[argv[0]], ""
        if argv[0] == "add-member":
            out = pathlib.Path(arg(argv, "out"))
            handle = f"{arg(argv, 'repo')}.{arg(argv, 'seat')}+{arg(argv, 'host')}.podman"
            out.mkdir(mode=0o700)
            out.chmod(0o700)
            (out / "member.json").write_text(member_json(argv[1], handle), encoding="utf-8")
            (out / "token").write_text("syt_secret", encoding="ascii")
            (out / "token").chmod(0o600)
            return 0, ""
        return 0, f"CHANGED\trevoked the token of {argv[2]}\nCHANGED\tparked {argv[2]}\n"

    def verbs(self) -> list[tuple[str, ...]]:
        return [call[:2] if call[0] == "park-member" else (call[0], call[1], arg(call, "seat"))
                for call, _ in self.calls]


class CheckoutTestCase(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_CONFIG")}
        patcher = mock.patch.dict(os.environ, {**env, **GIT_ENV}, clear=True)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.top = pathlib.Path(tmp.name).resolve() / "My_Repo"
        self.top.mkdir()
        self.git("init", "-q")
        self.git("remote", "add", "origin", "git@forge.example.com:owner/myrepo.git")
        self.ccy = self.top / ".claude" / "ccy"
        self.ccy.mkdir(parents=True)
        (self.ccy / ".gitignore").write_text("*\n!.gitignore\n", encoding="utf-8")
        self.host = FakeHost()
        self.seats = self.top / ".claude" / "ccy" / "pingbus" / "seats"
        self.held: list[inbox.Lock] = []
        self.addCleanup(self.release_held)

    def git(self, *args: str) -> None:
        subprocess.run(["git", "-C", str(self.top), *args], check=True, capture_output=True)

    def release_held(self) -> None:
        for lock in self.held:
            lock.release()

    def system(self, cwd: pathlib.Path | None = None, uid: int | None = None) -> checkout.System:
        return checkout.System(cwd=cwd or self.top, uid=os.getuid() if uid is None else uid,
                               team_active=self.host.team_active, bus_address=self.host.bus_address,
                               agent_bus=self.host.agent_bus, clock_ms=lambda: T0_MS)

    def run_cli(self, *argv: str, system: checkout.System | None = None) -> tuple[int, str, str]:
        out, err = io.BytesIO(), io.StringIO()
        code = cli.main(list(argv), stdout=out, stderr=err, checkout_system=system or self.system())
        self.assertNotIn("syt_secret", out.getvalue().decode() + err.getvalue())
        return code, out.getvalue().decode(), err.getvalue()

    def env_local(self, text: str) -> pathlib.Path:
        path = self.ccy / "ccy.env.local"
        path.write_text(text, encoding="utf-8")
        return path

    def hold(self, name: str, team: str = TEAM_A) -> None:
        self.held.append(inbox.acquire_lock_at(self.seats / team / name / seat.LOCK_FILE, "seat",
                                               claimed_ms=1))

    def tree(self) -> list[str]:
        root = self.top / ".claude"
        return sorted(str(p.relative_to(root)) for p in root.rglob("*") if p.name != seat.LOCK_FILE)


# ── the role and <host> (pure) ───────────────────────────────────────────────────────────


class RoleTest(unittest.TestCase):
    def role(self, text: str) -> str | None:
        return checkout.role_from_env_local(text)

    def test_the_assignment_forms(self):
        for text in ("export HOOKS_DAEMON_HOSTNAME=work1\n", "HOOKS_DAEMON_HOSTNAME=work1",
                     '  export  HOOKS_DAEMON_HOSTNAME="work1"  # the role\n',
                     "export HOOKS_DAEMON_HOSTNAME='work1'\n"):
            with self.subTest(text=text):
                self.assertEqual(self.role(text), "work1")

    def test_comments_and_other_lines_are_skipped(self):
        text = ("# based on ccy.env.local.dist version 3\n"
                "#export HOOKS_DAEMON_HOSTNAME=<role-name>\n"
                "  # HOOKS_DAEMON_HOSTNAME=nope\n"
                "export OTHER=1\nexport MY_HOOKS_DAEMON_HOSTNAME_X=nope\n")
        self.assertIsNone(self.role(text))
        self.assertIsNone(self.role(""))

    def test_an_empty_assignment_is_no_role(self):
        self.assertIsNone(self.role("export HOOKS_DAEMON_HOSTNAME=\n"))
        self.assertIsNone(self.role('export HOOKS_DAEMON_HOSTNAME=""\n'))

    def test_assigned_twice_is_refused(self):
        with self.assertRaises(checkout.CheckoutError) as caught:
            self.role("export HOOKS_DAEMON_HOSTNAME=a\nexport HOOKS_DAEMON_HOSTNAME=a\n")
        self.assertIn("twice", str(caught.exception))
        self.assertIn("lines 1 and 2", str(caught.exception))
        self.assertEqual(caught.exception.EXIT_CODE, EX_CONFIG)

    def test_a_form_needing_the_shell_is_refused(self):
        for text in ("export HOOKS_DAEMON_HOSTNAME=$(hostname)\n",
                     "export HOOKS_DAEMON_HOSTNAME=`hostname`\n",
                     'export HOOKS_DAEMON_HOSTNAME="$ROLE"\n',
                     "export HOOKS_DAEMON_HOSTNAME=a#b\n",
                     "export A=1 HOOKS_DAEMON_HOSTNAME=x\n",
                     "declare -x HOOKS_DAEMON_HOSTNAME=x\n",
                     ": ${HOOKS_DAEMON_HOSTNAME:=x}\n",
                     "export HOOKS_DAEMON_HOSTNAME=a b\n"):
            with self.subTest(text=text):
                with self.assertRaises(checkout.CheckoutError) as caught:
                    self.role(text)
                self.assertIn("line 1", str(caught.exception))

    def test_the_host_is_the_normalised_role_else_local(self):
        self.assertEqual(checkout.seat_host(None), "local")
        self.assertEqual(checkout.seat_host("Work_Station"), "work-station")
        with self.assertRaises(checkout.CheckoutError):
            checkout.seat_host("-x")


class EnvLocalFileTest(CheckoutTestCase):
    def test_no_file_is_local(self):
        self.assertEqual(checkout.checkout_host(self.top, os.getuid()), "local")

    def test_the_file_is_read_never_sourced(self):
        marker = self.top / "ran"
        self.env_local(f"touch {marker}\nexport HOOKS_DAEMON_HOSTNAME=Desk.Top\n")
        self.assertEqual(checkout.checkout_host(self.top, os.getuid()), "desk-top")
        self.assertFalse(marker.exists())

    def test_a_symlink_is_refused(self):
        real = self.top / "elsewhere"
        real.write_text("export HOOKS_DAEMON_HOSTNAME=x\n", encoding="utf-8")
        (self.ccy / "ccy.env.local").symlink_to(real)
        with self.assertRaises(checkout.CheckoutError) as caught:
            checkout.checkout_host(self.top, os.getuid())
        self.assertIn("symlink", str(caught.exception))

    def test_a_foreign_owned_file_is_refused(self):
        self.env_local("export HOOKS_DAEMON_HOSTNAME=x\n")
        with self.assertRaises(checkout.CheckoutError) as caught:
            checkout.checkout_host(self.top, os.getuid() + 1)
        self.assertIn("owned by", str(caught.exception))

    def test_not_a_regular_file_is_refused(self):
        (self.ccy / "ccy.env.local").mkdir()
        with self.assertRaises(checkout.CheckoutError):
            checkout.checkout_host(self.top, os.getuid())


class BusAddressTest(unittest.TestCase):
    def test_the_one_address(self):
        data = [{"ifname": "agentbus0", "addr_info": [{"family": "inet", "local": BUS_IP, "scope": "global"}]}]
        self.assertEqual(checkout.parse_bus_address(json.dumps(data)), BUS_IP)

    def test_link_scope_is_skipped(self):
        data = [{"addr_info": [{"family": "inet6", "local": "fe80::1", "scope": "link"},
                               {"family": "inet6", "local": "2001:db8::1", "scope": "global"}]}]
        self.assertEqual(checkout.parse_bus_address(json.dumps(data)), "2001:db8::1")

    def test_none_or_several_is_refused(self):
        for addrs in ([], [{"family": "inet", "local": BUS_IP, "scope": "global"},
                           {"family": "inet", "local": "192.0.2.11", "scope": "global"}]):
            with self.subTest(addrs=addrs):
                with self.assertRaises(checkout.CheckoutError):
                    checkout.parse_bus_address(json.dumps([{"addr_info": addrs}]))
        with self.assertRaises(checkout.CheckoutError):
            checkout.parse_bus_address("not json")


# ── the actions for an observed state (pure) ─────────────────────────────────────────────


class ActionsTest(unittest.TestCase):
    ROOT = pathlib.Path("/c/.claude/ccy/pingbus/seats")
    WHO = checkout.Identity(repo="myrepo", host="local")

    def obs(self, name: str, team: str = TEAM_A, *, exists: bool = False, held: bool = False,
            handle: str | None = None) -> checkout.Observed:
        return checkout.Observed(seat.SeatRef(name, team), exists=exists, held=held, handle=handle)

    def test_take_adds_only_the_missing_seats(self):
        observed = (self.obs("dev1"), self.obs("qa", TEAM_B, exists=True, handle="myrepo.qa+local.podman"))
        actions = checkout.take_actions(observed, self.ROOT, lambda: (self.WHO, BUS_IP))
        self.assertEqual(len(actions), 1)
        self.assertEqual(actions[0].handle, "myrepo.dev1+local.podman")
        self.assertEqual(actions[0].argv, (
            "add-member", TEAM_A, "--repo=myrepo", "--seat=dev1", "--host=local", "--type=podman",
            "--role=worker", f"--address={BUS_IP}", f"--out={self.ROOT}/{TEAM_A}/dev1"))

    def test_take_with_every_seat_present_needs_nothing_from_the_host(self):
        def never():
            raise AssertionError("the identity is not needed")
        observed = (self.obs("dev1", exists=True, handle="myrepo.dev1+local.podman"),)
        self.assertEqual(checkout.take_actions(observed, self.ROOT, never), ())

    def test_one_held_seat_refuses_everything(self):
        observed = (self.obs("dev1"), self.obs("qa", TEAM_B, exists=True, held=True, handle="h"))
        for plan in (lambda: checkout.take_actions(observed, self.ROOT, lambda: (self.WHO, BUS_IP)),
                     lambda: checkout.remove_actions(observed, lambda: self.WHO)):
            with self.assertRaises(seat.SeatHeld) as caught:
                plan()
            self.assertEqual(caught.exception.EXIT_CODE, EX_BUSY)
            self.assertIn("qa@team-b", str(caught.exception))

    def test_take_refuses_a_seat_directory_without_a_bundle(self):
        with self.assertRaises(checkout.CheckoutError) as caught:
            checkout.take_actions((self.obs("dev1", exists=True),), self.ROOT, lambda: (self.WHO, BUS_IP))
        self.assertIn("agent-bus seat remove dev1@team-a", str(caught.exception))

    def test_remove_parks_the_bundles_handle_or_the_checkouts(self):
        observed = (self.obs("dev1", exists=True, handle="myrepo.dev1+old.podman"), self.obs("qa", TEAM_B))
        actions = checkout.remove_actions(observed, lambda: self.WHO)
        self.assertEqual([a.argv for a in actions], [("park-member", TEAM_A, "myrepo.dev1+old.podman"),
                                                     ("park-member", TEAM_B, "myrepo.qa+local.podman")])
        self.assertEqual([a.delete for a in actions], [True, False])


# ── the commands ─────────────────────────────────────────────────────────────────────────


class CheckCommandTest(CheckoutTestCase):
    def test_prints_the_canonical_list_and_changes_nothing(self):
        before = self.tree()
        code, out, err = self.run_cli("seat", "check", " dev1@team-a , qa@team-b")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "dev1@team-a,qa@team-b\n")
        self.assertEqual(self.host.calls, [])
        self.assertEqual(self.tree(), before)

    def test_needs_no_checkout(self):
        code, out, err = self.run_cli("seat", "check", "dev1@team-a", system=self.system(cwd=pathlib.Path("/")))
        self.assertEqual(code, 0, err)


class RefusalsAlikeTest(CheckoutTestCase):
    """A malformed list (64) and a team not running here (78), by check, take and remove."""

    def test_a_malformed_list(self):
        for command in ("check", "take", "remove"):
            for text in ("dev1@t,dev2@t", "dev1@team-a,", "dev1"):
                with self.subTest(command=command, text=text):
                    code, out, err = self.run_cli("seat", command, text)
                    self.assertEqual(code, EX_USAGE, err)
                    self.assertEqual(out, "")
        self.assertEqual(self.host.calls, [])
        self.assertFalse((self.top / ".claude" / "ccy" / "pingbus").exists())

    def test_a_team_not_running_here(self):
        self.host.active = {TEAM_A}
        for command in ("check", "take", "remove"):
            with self.subTest(command=command):
                code, out, err = self.run_cli("seat", command, "dev1@team-a,qa@team-b")
                self.assertEqual(code, EX_CONFIG, err)
                self.assertIn("team-b", err)
                self.assertIn("agent-bus-hs@team-b.service", err)
                self.assertEqual(out, "")
        self.assertEqual(self.host.calls, [])
        self.assertFalse((self.top / ".claude" / "ccy" / "pingbus").exists())


class TakeCommandTest(CheckoutTestCase):
    def test_creates_each_new_seat(self):
        code, out, err = self.run_cli("seat", "take", "dev1@team-a,qa@team-b")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "CHANGED\tseat dev1@team-a myrepo.dev1+local.podman\n"
                              "CHANGED\tseat qa@team-b myrepo.qa+local.podman\n")
        self.assertEqual(self.host.verbs(), [("add-member", TEAM_A, "dev1"), ("add-member", TEAM_B, "qa")])
        self.assertEqual([no_prompt for _, no_prompt in self.host.calls], [False, False])
        self.assertIn("myrepo.dev1+local.podman", err)
        for path in (self.top / ".claude/ccy/pingbus", self.seats, self.seats / TEAM_A, self.seats / TEAM_B):
            self.assertEqual(path.stat().st_mode & 0o777, 0o700, path)

    def test_the_host_comes_from_ccy_env_local(self):
        self.env_local("export HOOKS_DAEMON_HOSTNAME=Work1\n")
        code, out, err = self.run_cli("seat", "take", "dev1@team-a")
        self.assertEqual(code, 0, err)
        self.assertIn("myrepo.dev1+work1.podman", out)

    def test_no_prompt_runs_sudo_without_a_prompt(self):
        code, _, err = self.run_cli("seat", "take", "dev1@team-a", "--no-prompt")
        self.assertEqual(code, 0, err)
        self.assertEqual([no_prompt for _, no_prompt in self.host.calls], [True])

    def test_a_second_identical_take_creates_nothing(self):
        self.assertEqual(self.run_cli("seat", "take", "dev1@team-a")[0], 0)
        before = self.tree()
        self.host.calls.clear()
        self.host.address = None  # not needed when nothing is created
        code, out, err = self.run_cli("seat", "take", "dev1@team-a")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "")
        self.assertEqual(self.host.calls, [])
        self.assertEqual(self.tree(), before)

    def test_only_the_named_seats(self):
        self.run_cli("seat", "take", "dev1@team-a")
        self.host.calls.clear()
        code, _, err = self.run_cli("seat", "take", "qa@team-b")
        self.assertEqual(code, 0, err)
        self.assertEqual(self.host.verbs(), [("add-member", TEAM_B, "qa")])

    def test_a_parked_seat_returns_by_the_same_call(self):
        self.run_cli("seat", "take", "dev1@team-a")
        self.run_cli("seat", "remove", "dev1@team-a")
        self.host.calls.clear()
        code, out, err = self.run_cli("seat", "take", "dev1@team-a")
        self.assertEqual(code, 0, err)
        self.assertEqual(self.host.verbs(), [("add-member", TEAM_A, "dev1")])
        self.assertIn("myrepo.dev1+local.podman", out)

    def test_one_held_seat_creates_nothing(self):
        self.run_cli("seat", "take", "dev1@team-a")
        self.hold("dev1")
        self.host.calls.clear()
        before = self.tree()
        code, out, err = self.run_cli("seat", "take", "qa@team-b,dev1@team-a")
        self.assertEqual(code, EX_BUSY, err)
        self.assertIn("seat `dev1@team-a` is held by another session", err)
        self.assertEqual((out, self.host.calls), ("", []))
        self.assertEqual(self.tree(), before)

    def test_a_failed_add_member_stops_the_take(self):
        self.host.fail["add-member"] = 1
        code, out, err = self.run_cli("seat", "take", "dev1@team-a,qa@team-b", "--no-prompt")
        self.assertEqual(code, EX_CONFIG)
        self.assertEqual(out, "")
        self.assertEqual(len(self.host.calls), 1)
        self.assertIn("sudo -v", err)

    def test_the_checkout_must_ignore_the_bundles(self):
        (self.ccy / ".gitignore").write_text("!*\n", encoding="utf-8")
        code, _, err = self.run_cli("seat", "take", "dev1@team-a")
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("check-ignore", err)
        self.assertEqual(self.host.calls, [])
        self.assertFalse((self.top / ".claude/ccy/pingbus").exists())

    def test_the_checkout_rules(self):
        cases = {"not a checkout": self.system(cwd=pathlib.Path(tempfile.mkdtemp())),
                 "foreign": self.system(uid=os.getuid() + 1)}
        for name, system in cases.items():
            with self.subTest(name):
                code, _, err = self.run_cli("seat", "take", "dev1@team-a", system=system)
                self.assertEqual(code, EX_CONFIG, err)
        os.rmdir(cases["not a checkout"].cwd)
        self.assertEqual(self.host.calls, [])

    def test_a_symlinked_tree_is_refused(self):
        elsewhere = self.top / "elsewhere"
        elsewhere.mkdir()
        (self.top / ".claude/ccy/pingbus").symlink_to(elsewhere)
        code, _, err = self.run_cli("seat", "take", "dev1@team-a")
        self.assertEqual(code, EX_CONFIG, err)
        self.assertIn("symlink", err)
        self.assertEqual(self.host.calls, [])
        self.assertEqual(list(elsewhere.iterdir()), [])

    def test_runs_from_a_subdirectory_of_the_checkout(self):
        sub = self.top / "docs"
        sub.mkdir()
        code, _, err = self.run_cli("seat", "take", "dev1@team-a", system=self.system(cwd=sub))
        self.assertEqual(code, 0, err)
        self.assertTrue((self.seats / TEAM_A / "dev1" / "member.json").exists())

    def test_no_bus_address_is_refused_before_any_sudo(self):
        self.host.address = None
        code, _, err = self.run_cli("seat", "take", "dev1@team-a")
        self.assertEqual(code, EX_CONFIG, err)
        self.assertEqual(self.host.calls, [])


class ListCommandTest(CheckoutTestCase):
    def test_seat_lines(self):
        self.run_cli("seat", "take", "dev1@team-a,qa@team-b")
        self.hold("qa", TEAM_B)
        code, out, err = self.run_cli("seat", "list")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "SEAT\tteam-a\tdev1\tfree\t-\tmyrepo.dev1+local.podman\n"
                              "SEAT\tteam-b\tqa\theld\t-\tmyrepo.qa+local.podman\n")

    def test_no_seats_is_no_lines(self):
        self.assertEqual(self.run_cli("seat", "list")[:2], (0, ""))

    def test_a_broken_seat_is_said_and_the_rest_still_print(self):
        self.run_cli("seat", "take", "dev1@team-a,qa@team-b")
        (self.seats / TEAM_A / "dev1" / "member.json").write_text("{", encoding="utf-8")
        code, out, err = self.run_cli("seat", "list")
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("SEAT\tteam-b\tqa", out)
        self.assertIn("dev1@team-a", err)


class RemoveCommandTest(CheckoutTestCase):
    def test_parks_deletes_and_leaves_nothing_of_the_bus(self):
        self.run_cli("seat", "take", "dev1@team-a,qa@team-b")
        self.host.calls.clear()
        code, out, err = self.run_cli("seat", "remove", "dev1@team-a,qa@team-b")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "CHANGED\tseat dev1@team-a myrepo.dev1+local.podman\n"
                              "CHANGED\tseat qa@team-b myrepo.qa+local.podman\n")
        self.assertEqual(self.host.verbs(), [("park-member", TEAM_A), ("park-member", TEAM_B)])
        self.assertFalse((self.top / ".claude/ccy/pingbus").exists())
        self.assertEqual(self.tree(), ["ccy", "ccy/.gitignore"])

    def test_keeps_what_is_not_empty(self):
        self.assertEqual(self.run_cli("seat", "take", "dev1@team-a,dev2@team-b")[0], 0)
        self.assertEqual(self.run_cli("seat", "take", "qa@team-b")[0], 0)
        code, _, err = self.run_cli("seat", "remove", "dev1@team-a,qa@team-b")
        self.assertEqual(code, 0, err)
        self.assertFalse((self.seats / TEAM_A).exists())
        self.assertTrue((self.seats / TEAM_B / "dev2" / "member.json").exists())
        self.assertFalse((self.seats / TEAM_B / "qa").exists())

    def test_refused_while_a_named_seat_is_held(self):
        self.run_cli("seat", "take", "dev1@team-a,qa@team-b")
        self.hold("qa", TEAM_B)
        self.host.calls.clear()
        code, out, err = self.run_cli("seat", "remove", "dev1@team-a,qa@team-b")
        self.assertEqual(code, EX_BUSY, err)
        self.assertEqual((out, self.host.calls), ("", []))
        self.assertTrue((self.seats / TEAM_A / "dev1" / "member.json").exists())

    def test_a_lost_directory_parks_the_checkouts_handle(self):
        code, out, err = self.run_cli("seat", "remove", "dev1@team-a")
        self.assertEqual(code, 0, err)
        self.assertEqual(self.host.calls, [(("park-member", TEAM_A, "myrepo.dev1+local.podman"), False)])
        self.assertIn("CHANGED\tseat dev1@team-a", out)

    def test_a_refused_park_keeps_the_directory(self):
        self.run_cli("seat", "take", "dev1@team-a")
        self.host.fail["park-member"] = 70
        code, out, err = self.run_cli("seat", "remove", "dev1@team-a")
        self.assertEqual(code, EX_CONFIG)
        self.assertEqual(out, "")
        self.assertTrue((self.seats / TEAM_A / "dev1" / "member.json").exists())


if __name__ == "__main__":
    unittest.main()

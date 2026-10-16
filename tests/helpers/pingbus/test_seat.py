"""Unit tests for helpers/pingbus/seat.py and `pingbus seat exec`.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_seat

Spec: Plan 00161 DESIGN.md section 5.5 (seat names, the `<seat>@<team>` list and its
canonical form, the layout `seats/<team>/<seat>/`, the claim, the session home, `SEAT`
lines), D35, D36, D48, D50; docs/agent-bus-protocol.md §12-§15. Every seat is built in a
temporary directory; the claim runs with the session home and the seats root moved there,
and `exec` is replaced by a recorder except in the tests that exec a real process.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import stat
import subprocess
import sys
import tempfile
import textwrap
import time
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.pingbus import cli, config, inbox, protocol, seat
from tests.helpers.pingbus import fake_gh

TEAM_A, SN_A = "team-a", "team-a.agent-bus.internal"
TEAM_B, SN_B = "team-b", "team-b.agent-bus.internal"
ROOM = "!" + "R" * 43
T0_MS = 1_791_000_000_000

EX_USAGE, EX_BUSY, EX_CONFIG = 64, 75, 78


def handle_of(seat_name: str) -> str:
    return f"myrepo.{seat_name}+local.podman"


# ── the list ─────────────────────────────────────────────────────────────────────────────


class ListRulesTest(unittest.TestCase):
    """Section 5.5 "Launch": every rule, refused as a usage error (64) naming the rule."""

    def test_one_item_and_several(self):
        self.assertEqual(seat.parse_seat_list("dev1@team-a"), (seat.SeatRef("dev1", "team-a"),))
        self.assertEqual(
            seat.parse_seat_list("dev1@team-a,qa2@team-b"),
            (seat.SeatRef("dev1", "team-a"), seat.SeatRef("qa2", "team-b")),
        )

    def test_numbered_seats_are_seats(self):
        self.assertEqual(seat.parse_seat_list("1@team-a"), (seat.SeatRef("1", "team-a"),))

    def test_whitespace_around_items_is_dropped(self):
        for text in ("dev1@team-a, qa2@team-b", " dev1@team-a ,\tqa2@team-b\t", "dev1@team-a ,qa2@team-b"):
            with self.subTest(text=text):
                refs = seat.parse_seat_list(text)
                self.assertEqual(seat.canonical(refs), "dev1@team-a,qa2@team-b")

    def test_the_canonical_form_keeps_the_order_given(self):
        refs = seat.parse_seat_list(" qa2@team-b , dev1@team-a")
        self.assertEqual(seat.canonical(refs), "qa2@team-b,dev1@team-a")
        self.assertEqual([ref.text for ref in refs], ["qa2@team-b", "dev1@team-a"])

    def refused(self, text: str, *needles: str) -> str:
        with self.assertRaises(seat.SeatListError) as caught:
            seat.parse_seat_list(text)
        self.assertEqual(caught.exception.EXIT_CODE, EX_USAGE)
        self.assertIsInstance(caught.exception, config.UsageError)
        message = str(caught.exception)
        for needle in needles:
            self.assertIn(needle, message)
        return message

    def test_an_empty_list_is_refused(self):
        for text in ("", " ", "\t"):
            with self.subTest(text=text):
                self.refused(text, "empty")

    def test_an_empty_item_is_refused(self):
        for text, position in ((",dev1@team-a", "1"), ("dev1@team-a,,qa2@team-b", "2"),
                               ("dev1@team-a,", "2"), ("dev1@team-a, ", "2")):
            with self.subTest(text=text):
                self.refused(text, f"item {position} is empty")

    def test_a_trailing_comma_shows_the_list_without_the_space(self):
        message = self.refused("dev1@team-a,", "dev1@dev-team,qa2@other-team", "quote")
        self.assertIn("no space", message)

    def test_an_item_needs_exactly_one_at(self):
        for text in ("dev1", "dev1@team-a@x", "dev1team-a"):
            with self.subTest(text=text):
                self.refused(text, "exactly one @")

    def test_a_bad_seat_is_refused(self):
        for bad in ("Dev1", "dev-1", "dev_1", "dev.1", "0", "01", "1a", "abcdefghijklm", ""):
            with self.subTest(seat=bad):
                self.refused(f"{bad}@team-a", "seat")

    def test_a_bad_team_is_refused(self):
        for bad in ("Team", "1team", "team_a", "t" * 25, ""):
            with self.subTest(team=bad):
                self.refused(f"dev1@{bad}", "team")

    def test_a_team_named_twice_is_refused(self):
        message = self.refused("dev1@t,dev2@t", "one seat per team per session")
        self.assertIn("`dev1@t` and `dev2@t` both name team `t`", message)
        self.refused("dev1@t, dev1@t", "one seat per team per session")

    def test_a_list_not_for_one_session_may_name_a_team_twice(self):
        refs = seat.parse_seat_list("dev1@t,dev2@t,qa@u", one_per_team=False)
        self.assertEqual(seat.canonical(refs), "dev1@t,dev2@t,qa@u")

    def test_a_seat_named_twice_is_refused_in_any_list(self):
        with self.assertRaises(seat.SeatListError) as caught:
            seat.parse_seat_list("dev1@t, dev1@t", one_per_team=False)
        self.assertIn("`dev1@t` is named twice", str(caught.exception))

    def test_an_unprintable_item_is_not_echoed(self):
        message = self.refused("dev1@team-a,\x1b[2Jx@y\x00", "item 2")
        self.assertNotIn("\x1b", message)
        self.assertNotIn("\x00", message)

    def test_a_mixed_case_team_gets_a_lower_case_suggestion(self):
        message = self.refused("dev1@LongTermSupport", "team names are lower-case")
        self.assertIn("`longtermsupport`", message)
        self.assertIn("`long-term-support`", message)
        message = self.refused("dev1@Alpha", "team names are lower-case")
        self.assertIn("did you mean `alpha`?", message)


class BareTeamTest(unittest.TestCase):
    """A team alone (`allow_bare`): its seat is chosen later, on a terminal (Task 4.8)."""

    def test_a_bare_team_is_a_team_ref(self):
        refs = seat.parse_seat_list(" alpha ,dev1@team-b", allow_bare=True)
        self.assertEqual(refs, (seat.TeamRef("alpha"), seat.SeatRef("dev1", "team-b")))
        self.assertEqual(seat.canonical(refs), "alpha,dev1@team-b")

    def test_without_allow_bare_it_is_refused_naming_the_way(self):
        with self.assertRaises(seat.SeatListError) as caught:
            seat.parse_seat_list("alpha")
        self.assertIn("exactly one @", str(caught.exception))
        self.assertIn("ccy --teams alpha", str(caught.exception))

    def test_a_bare_team_beside_a_seat_of_it_is_one_team_twice(self):
        with self.assertRaises(seat.SeatListError) as caught:
            seat.parse_seat_list("alpha,dev1@alpha", allow_bare=True)
        self.assertIn("both name team `alpha`", str(caught.exception))

    def test_a_bare_mixed_case_team_gets_the_suggestion(self):
        with self.assertRaises(seat.SeatListError) as caught:
            seat.parse_seat_list("LongTermSupport", allow_bare=True)
        self.assertIn("team names are lower-case", str(caught.exception))
        self.assertIn("`long-term-support`", str(caught.exception))

    def test_a_bare_word_that_is_no_team_is_refused(self):
        for bad in ("1team", "team_a", "t" * 25):
            with self.subTest(bad=bad), self.assertRaises(seat.SeatListError):
                seat.parse_seat_list(bad, allow_bare=True)


# ── a checkout's seats ───────────────────────────────────────────────────────────────────


class SeatCase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.root = self.tmp / "checkout" / ".claude" / "ccy" / "pingbus" / "seats"
        self.root.mkdir(parents=True)
        self.session_home = self.tmp / "session" / "pingbus-home"
        self.session_home.parent.mkdir()
        self.execs: list[tuple[str, list[str], dict[str, str]]] = []
        self.env = {"PATH": fake_gh.path_with(fake_gh.fake_gh_dir(self.tmp)), "HOME": str(self.tmp)}

    def make_seat(self, name: str, team: str = TEAM_A, *, member: bool = True) -> pathlib.Path:
        sn = SN_A if team == TEAM_A else SN_B
        directory = self.root / team / name
        directory.mkdir(parents=True, mode=0o700)
        directory.chmod(0o700)
        if member:
            data = {"protocol": 1, "team": team, "user_id": f"@{handle_of(name)}:{sn}",
                    "server_name": sn, "base_url": "http://127.0.0.1:1",
                    "plain_http_hosts": ["127.0.0.1"], "token_file": "token",
                    "admin": f"@admin:{sn}", "room": ROOM}
            (directory / "member.json").write_text(json.dumps(data), encoding="utf-8")
            token = directory / "token"
            token.write_text(f"syt_token_{team}_{name}".replace("-", "_"), encoding="ascii")
            token.chmod(0o600)
        return directory

    def runtime(self) -> cli.Runtime:
        def execvpe(file: str, argv: list[str], env: dict[str, str]) -> None:
            self.execs.append((file, list(argv), dict(env)))

        return cli.Runtime(clock_ms=lambda: T0_MS, sleep=lambda _s: None, seats_root=self.root,
                           session_home=self.session_home, execvpe=execvpe)

    def run_cli(self, *argv: str, env: dict[str, str] | None = None) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = cli.main(list(argv), environ=self.env if env is None else env, stdout=out,
                        stderr=err, runtime=self.runtime())
        self.assertNotIn("syt_token", out.getvalue() + err.getvalue())
        return code, out.getvalue(), err.getvalue()

    def lock_of(self, name: str, team: str = TEAM_A) -> pathlib.Path:
        return self.root / team / name / seat.LOCK_FILE

    def hold_elsewhere(self, name: str, team: str = TEAM_A) -> subprocess.Popen:
        """Another real process holding a seat's lock, until its stdin closes."""
        child = subprocess.Popen(
            [sys.executable, "-c",
             "import pathlib, sys\n"
             "from helpers.pingbus import inbox\n"
             "lock = inbox.acquire_lock_at(pathlib.Path(sys.argv[1]), 'seat', claimed_ms=1)\n"
             "print('held', flush=True)\n"
             "sys.stdin.read()\n",
             str(self.lock_of(name, team))],
            cwd=REPO_ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True,
        )
        self.addCleanup(child.stdout.close)
        self.addCleanup(child.wait)
        self.addCleanup(child.stdin.close)
        self.assertEqual(child.stdout.readline().strip(), "held")
        return child

    def wait_free(self, path: pathlib.Path) -> None:
        deadline = time.monotonic() + 10
        while inbox.probe_lock_at(path) is not None:
            self.assertLess(time.monotonic(), deadline, f"{path} was never released")
            time.sleep(0.05)


class ListSeatsTest(SeatCase):
    def test_the_seats_are_the_directories(self):
        self.make_seat("qa2", TEAM_B)
        self.make_seat("dev1")
        self.make_seat("1")
        self.assertEqual(seat.list_seats(self.root), (
            seat.SeatRef("1", TEAM_A), seat.SeatRef("dev1", TEAM_A), seat.SeatRef("qa2", TEAM_B),
        ))
        self.assertEqual(seat.list_seats(self.root, team=TEAM_B), (seat.SeatRef("qa2", TEAM_B),))

    def test_no_seats_directory_is_no_seats(self):
        self.assertEqual(seat.list_seats(self.tmp / "nothing"), ())

    def test_an_entry_that_is_not_a_seat_is_refused(self):
        self.make_seat("dev1")
        for bad in (self.root / "Not_A_Team", self.root / TEAM_A / "dev-1"):
            with self.subTest(entry=bad.name):
                bad.mkdir()
                with self.assertRaises(inbox.StateError):
                    seat.list_seats(self.root)
                bad.rmdir()

    def test_a_symlinked_seat_is_refused(self):
        target = self.make_seat("dev1")
        (self.root / TEAM_A / "dev2").symlink_to(target)
        with self.assertRaises(inbox.StateError):
            seat.list_seats(self.root)

    def test_a_symlinked_seats_root_is_refused(self):
        link = self.tmp / "linked-seats"
        link.symlink_to(self.root)
        with self.assertRaises(inbox.StateError):
            seat.list_seats(link)


class SeatLinesTest(SeatCase):
    def test_one_line_per_seat_held_or_free_with_its_handle(self):
        self.make_seat("dev1")
        self.make_seat("qa2", TEAM_B)
        holder = self.hold_elsewhere("qa2", TEAM_B)
        lines, failures = seat.seat_lines(self.root, None)
        self.assertEqual(failures, [])
        self.assertEqual(lines, [
            f"SEAT\t{TEAM_A}\tdev1\tfree\t-\t{handle_of('dev1')}",
            f"SEAT\t{TEAM_B}\tqa2\theld\t-\t{handle_of('qa2')}",
        ])
        holder.stdin.close()
        holder.wait()
        self.assertIn("\tfree\t", seat.seat_lines(self.root, None)[0][1])

    def test_self_is_a_seat_the_session_home_links_to(self):
        own = self.make_seat("dev1")
        self.make_seat("dev2")
        self.session_home.mkdir(mode=0o700)
        (self.session_home / TEAM_A).symlink_to(own)
        lines, _ = seat.seat_lines(self.root, self.session_home)
        self.assertEqual([line.split("\t")[4] for line in lines], ["self", "-"])

    def test_a_seat_whose_member_file_is_broken_is_a_failure_and_the_others_still_print(self):
        self.make_seat("dev1")
        broken = self.make_seat("dev2")
        (broken / "member.json").write_text("{", encoding="utf-8")
        lines, failures = seat.seat_lines(self.root, None)
        self.assertEqual(len(lines), 1)
        self.assertEqual(len(failures), 1)
        self.assertIn("dev2@team-a", failures[0])


# ── the claim: `pingbus seat exec` ───────────────────────────────────────────────────────


class ClaimTest(SeatCase):
    def setUp(self) -> None:
        super().setUp()
        self.dev1 = self.make_seat("dev1")
        self.qa2 = self.make_seat("qa2", TEAM_B)
        self.env[seat.SEATS_VAR] = "dev1@team-a,qa2@team-b"
        self.addCleanup(self.release_claim)

    def claim(self, *command: str, env: dict[str, str] | None = None) -> tuple[int, str, str]:
        return self.run_cli("seat", "exec", "--", *(command or ("claude", "--resume")), env=env)

    @staticmethod
    def release_claim() -> None:
        """An in-process claim keeps its locks for the program it execs; drop them."""
        for lock in seat.held_locks():
            lock.release()
        seat.forget_held_locks()

    def test_every_seat_is_claimed_and_the_command_execd_with_the_session_home(self):
        code, out, err = self.claim("claude", "--resume")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "")
        ((file, argv, env),) = self.execs
        self.assertEqual((file, argv), ("claude", ["claude", "--resume"]))
        self.assertEqual(env["PINGBUS_HOME"], str(self.session_home))
        self.assertEqual(env["PINGBUS_TEAMS"], "team-a,team-b")
        self.assertEqual(env[seat.SEATS_VAR], "dev1@team-a,qa2@team-b")
        self.assertEqual(env["PATH"], self.env["PATH"])
        self.assertIn(f"✓ agent team bus: dev1@team-a ({handle_of('dev1')}), "
                      f"qa2@team-b ({handle_of('qa2')})", err)

    def test_the_locks_hold_the_seat_kind_and_the_claim_time_and_are_inheritable(self):
        self.assertEqual(self.claim()[0], 0)
        for path in (self.lock_of("dev1"), self.lock_of("qa2", TEAM_B)):
            self.assertEqual(path.read_text(encoding="ascii"), f"seat {T0_MS}")
            self.assertEqual(inbox.probe_lock_at(path), inbox.SEAT_KIND)
        self.assertEqual(len(seat.held_locks()), 2)
        self.assertTrue(all(os.get_inheritable(lock.fileno()) for lock in seat.held_locks()))

    def test_the_session_home_is_private_with_one_link_per_seat(self):
        self.assertEqual(self.claim()[0], 0)
        self.assertEqual(stat.S_IMODE(self.session_home.stat().st_mode), 0o700)
        self.assertEqual(sorted(p.name for p in self.session_home.iterdir()), [TEAM_A, TEAM_B])
        self.assertEqual(os.readlink(self.session_home / TEAM_A), str(self.dev1))
        self.assertEqual(os.readlink(self.session_home / TEAM_B), str(self.qa2))

    def test_on_the_host_the_seats_root_and_session_home_are_given(self):
        """`cc --teams` claims a checkout's seat on the host (Plan 00177 Task 4.10): no
        /workspace there, and a session home of its own in a private directory."""
        home = self.tmp / "cc-session" / "pingbus-home"
        home.parent.mkdir(mode=0o700)
        code, _, err = self.run_cli("seat", "exec", "--root", str(self.root), "--home", str(home),
                                    "--", "claude")
        self.assertEqual(code, 0, err)
        ((_, _, env),) = self.execs
        self.assertEqual(env["PINGBUS_HOME"], str(home))
        self.assertEqual(os.readlink(home / TEAM_A), str(self.dev1))
        self.assertFalse(self.session_home.exists())

    def test_a_relative_seats_root_or_session_home_is_refused_64(self):
        for option in ("--root", "--home"):
            with self.subTest(option=option):
                code, _, err = self.run_cli("seat", "exec", option, "relative/dir", "--", "claude")
                self.assertEqual(code, EX_USAGE, err)
                self.assertIn(option, err)
        self.assertEqual(self.execs, [])

    def test_an_existing_session_home_is_refused(self):
        self.session_home.mkdir()
        code, _, err = self.claim()
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("already exists", err)
        self.assertEqual(self.execs, [])
        self.assertIsNone(inbox.probe_lock_at(self.lock_of("dev1")))

    def test_a_missing_list_is_refused_78(self):
        env = {k: v for k, v in self.env.items() if k != seat.SEATS_VAR}
        for value in (None, ""):
            with self.subTest(value=value):
                run_env = dict(env) if value is None else dict(env, **{seat.SEATS_VAR: value})
                code, _, err = self.claim(env=run_env)
                self.assertEqual(code, EX_CONFIG)
                self.assertIn(seat.SEATS_VAR, err)
        self.assertEqual(self.execs, [])

    def test_a_malformed_list_is_refused_78_not_64(self):
        for value in ("dev1@team-a,", "dev1", "dev1@t,dev2@t", "Dev@team-a"):
            with self.subTest(value=value):
                code, _, err = self.claim(env=dict(self.env, **{seat.SEATS_VAR: value}))
                self.assertEqual(code, EX_CONFIG, err)
        self.assertEqual(self.execs, [])
        self.assertFalse(self.session_home.exists())

    def test_a_seat_with_no_directory_is_refused_78_naming_the_host(self):
        code, _, err = self.claim(env=dict(self.env, **{seat.SEATS_VAR: "dev1@team-a,pm@team-b"}))
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("pm@team-b", err)
        self.assertIn("ccy --teams", err)
        self.assertEqual(self.execs, [])
        self.assertIsNone(inbox.probe_lock_at(self.lock_of("dev1")))

    def test_one_of_two_seats_held_elsewhere_is_75_without_waiting_and_keeps_neither(self):
        self.hold_elsewhere("qa2", TEAM_B)
        code, _, err = self.claim()
        self.assertEqual(code, EX_BUSY)
        self.assertIn("seat `qa2@team-b` is held by another session", err)
        self.assertEqual(self.execs, [])
        self.assertIsNone(inbox.probe_lock_at(self.lock_of("dev1")), "the first seat was kept")
        self.assertEqual(seat.held_locks(), ())
        self.assertFalse(self.session_home.exists())

    def test_a_bundle_that_fails_config_check_is_refused_78(self):
        self.qa2.chmod(0o750)
        self.addCleanup(self.qa2.chmod, 0o700)
        code, _, err = self.claim()
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("team team-b", err)
        self.assertEqual(self.execs, [])
        self.assertIsNone(inbox.probe_lock_at(self.lock_of("dev1")))

    def test_a_missing_bundle_names_the_launch_that_creates_it(self):
        (self.qa2 / "member.json").unlink()
        code, _, err = self.claim()
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("ccy --teams qa2@team-b", err)

    def test_gh_not_logged_in_is_refused_78_before_any_claim(self):
        env = dict(self.env, PATH=fake_gh.path_with(fake_gh.fake_gh_dir(self.tmp, "logged-out")))
        code, _, err = self.claim(env=env)
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("gh auth login --hostname github.com", err)
        self.assertEqual(self.execs, [])
        self.assertIsNone(inbox.probe_lock_at(self.lock_of("dev1")))

    def test_no_command_is_a_usage_error(self):
        code, _, err = self.run_cli("seat", "exec")
        self.assertEqual(code, EX_USAGE, err)
        code, _, err = self.run_cli("seat", "exec", "--")
        self.assertEqual(code, EX_USAGE, err)
        self.assertEqual(self.execs, [])

    def test_the_command_may_follow_without_the_separator(self):
        code, _, err = self.run_cli("seat", "exec", "claude", "--plugin-dir", "/x")
        self.assertEqual(code, 0, err)
        self.assertEqual(self.execs[0][1], ["claude", "--plugin-dir", "/x"])


def membership_text(seats: list[dict[str, str]] | None = None, **change: object) -> str:
    """A desired membership file (Plan 00203 SPEC-hot-control.md section 3.1)."""
    data: dict[str, object] = {
        "v": 1, "session": "ccy-proj-1", "kind": "ccy", "checkout": "/home/u/code/proj",
        "generation": 1, "written_ms": T0_MS, "seats": [] if seats is None else seats, "farewell": [],
    }
    data.update(change)
    return json.dumps(data) + "\n"


class ParseMembershipTest(unittest.TestCase):
    def test_the_seats_in_the_files_order(self):
        text = membership_text([{"team": TEAM_B, "seat": "qa2"}, {"team": TEAM_A, "seat": "1"}])
        self.assertEqual(seat.parse_membership(text), (seat.SeatRef("qa2", TEAM_B), seat.SeatRef("1", TEAM_A)))

    def test_no_seats_is_a_valid_membership(self):
        self.assertEqual(seat.parse_membership(membership_text()), ())

    def test_a_farewell_entry_is_read_past(self):
        farewell = [{"team": TEAM_A, "seat": "1", "reason": "removed"}]
        self.assertEqual(seat.parse_membership(membership_text(farewell=farewell)), ())

    def test_what_is_not_a_version_1_membership_is_refused_78(self):
        good = json.loads(membership_text())
        missing = {k: v for k, v in good.items() if k != "checkout"}
        cases = {
            "not JSON": "{",
            "not an object": "[]",
            "an unknown key": membership_text(extra=1),
            "a missing key": json.dumps(missing),
            "version 2": membership_text(v=2),
            "a version that is a string": membership_text(v="1"),
            "a version that is true": membership_text(v=True),
            "seats not a list": membership_text(seats={}),
            "a seat entry with an extra key": membership_text([{"team": TEAM_A, "seat": "1", "role": "worker"}]),
            "a seat entry missing its seat": membership_text([{"team": TEAM_A}]),
            "a seat that is not a seat name": membership_text([{"team": TEAM_A, "seat": "Dev"}]),
            "a team that is not a team name": membership_text([{"team": "Team", "seat": "1"}]),
            "a team twice": membership_text([{"team": TEAM_A, "seat": "1"}, {"team": TEAM_A, "seat": "2"}]),
            "farewell not a list": membership_text(farewell={}),
            "a generation of zero": membership_text(generation=0),
            "a relative checkout": membership_text(checkout="code/proj"),
            "an empty session": membership_text(session=""),
            "another kind": membership_text(kind="lxc"),
        }
        for name, text in cases.items():
            with self.subTest(case=name), self.assertRaises(config.ConfigError) as caught:
                seat.parse_membership(text)
            self.assertNotIsInstance(caught.exception, config.UsageError, name)


class MembershipClaimTest(SeatCase):
    """A ccy launch since Plan 00203 M2 passes PINGBUS_MEMBERSHIP, the read-only directory with
    the session's desired membership, instead of PINGBUS_SEATS; a session on no team is valid."""

    def setUp(self) -> None:
        super().setUp()
        self.dev1 = self.make_seat("dev1")
        self.qa2 = self.make_seat("qa2", TEAM_B)
        self.desired = self.tmp / "run" / "ccy" / "membership"
        self.desired.mkdir(parents=True)
        self.project = self.tmp / "checkout"
        self.user = self.tmp / "dot-claude"
        self.user.mkdir()
        self.env[seat.MEMBERSHIP_VAR] = str(self.desired)
        self.env["CLAUDE_CONFIG_DIR"] = str(self.user)
        self.write_desired([{"team": TEAM_A, "seat": "dev1"}, {"team": TEAM_B, "seat": "qa2"}])
        self.addCleanup(ClaimTest.release_claim)
        cwd = os.getcwd()
        os.chdir(self.project)
        self.addCleanup(os.chdir, cwd)

    def write_desired(self, seats: list[dict[str, str]] | None, **change: object) -> None:
        (self.desired / seat.MEMBERSHIP_FILE).write_text(membership_text(seats, **change), encoding="utf-8")

    def claim(self, env: dict[str, str] | None = None) -> tuple[int, str, str]:
        return self.run_cli("seat", "exec", "--", "claude", env=env)

    def home_file(self, name: str) -> str:
        return (self.session_home / name).read_text(encoding="utf-8")

    def write_settings(self, path: pathlib.Path, data: object) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(data), encoding="utf-8")

    def test_the_desired_seats_are_claimed_and_the_session_is_marked_active(self):
        code, out, err = self.claim()
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "")
        ((_, argv, env),) = self.execs
        self.assertEqual(argv, ["claude"])
        self.assertEqual(env["PINGBUS_HOME"], str(self.session_home))
        self.assertEqual(env["PINGBUS_TEAMS"], "team-a,team-b")
        self.assertEqual(env[seat.MEMBERSHIP_VAR], str(self.desired))
        self.assertNotIn(seat.SEATS_VAR, env)
        self.assertEqual(inbox.probe_lock_at(self.lock_of("dev1")), inbox.SEAT_KIND)
        self.assertEqual(os.readlink(self.session_home / TEAM_B), str(self.qa2))
        self.assertTrue((self.session_home / seat.ACTIVE_FLAG).is_file())
        self.assertIn("✓ agent team bus: dev1@team-a", err)

    def test_an_empty_membership_makes_the_home_holds_nothing_and_asks_no_gh(self):
        self.write_desired(None)
        env = dict(self.env, PATH=fake_gh.path_with(fake_gh.fake_gh_dir(self.tmp, "logged-out")))
        code, _, err = self.claim(env=env)
        self.assertEqual(code, 0, err)
        ((_, _, child_env),) = self.execs
        self.assertEqual(child_env["PINGBUS_HOME"], str(self.session_home))
        self.assertNotIn("PINGBUS_TEAMS", child_env)
        self.assertEqual(stat.S_IMODE(self.session_home.stat().st_mode), 0o700)
        self.assertEqual(sorted(p.name for p in self.session_home.iterdir()),
                         [seat.SESSION_SETTINGS, seat.STATUS_LINE_INNER])
        self.assertEqual(seat.held_locks(), ())
        self.assertIsNone(inbox.probe_lock_at(self.lock_of("dev1")))
        self.assertIn("no team", err)

    def test_the_session_settings_accept_inbound_and_guard_the_status_line(self):
        """H34 with the M2 spike's answer (Claude Code does not re-read --settings): inbound is
        accepted from the start, and the status line is a shell guard that runs no Python for
        a session on no team."""
        self.assertEqual(self.claim()[0], 0)
        settings = json.loads(self.home_file(seat.SESSION_SETTINGS))
        self.assertEqual(settings, {
            "crossSessionInbound": "accept",
            "statusLine": {"type": "command", "command": seat.STATUS_LINE_GUARD, "refreshInterval": 1},
        })
        for name in (seat.SESSION_SETTINGS, seat.STATUS_LINE_INNER, seat.ACTIVE_FLAG):
            self.assertEqual(stat.S_IMODE((self.session_home / name).stat().st_mode), 0o600, name)

    def test_the_inner_status_line_is_the_one_the_session_would_have_had(self):
        local = self.project / ".claude" / "settings.local.json"
        project = self.project / ".claude" / "settings.json"
        user = self.user / "settings.json"
        self.write_settings(user, {"statusLine": {"type": "command", "command": "user-line"}})
        self.write_settings(project, {"statusLine": {"type": "command", "command": "project-line",
                                                     "refreshInterval": 5, "padding": 2}})
        self.write_settings(local, {"model": "x"})
        self.write_desired(None)
        self.assertEqual(self.claim()[0], 0)
        self.assertEqual(self.home_file(seat.STATUS_LINE_INNER), "project-line\n")
        line = json.loads(self.home_file(seat.SESSION_SETTINGS))["statusLine"]
        self.assertEqual(line, {"type": "command", "command": seat.STATUS_LINE_GUARD,
                                "refreshInterval": 5, "padding": 2})

    def test_the_local_settings_outrank_the_project_and_the_user(self):
        self.write_settings(self.user / "settings.json", {"statusLine": {"type": "command", "command": "u"}})
        self.write_settings(self.project / ".claude" / "settings.local.json",
                            {"statusLine": {"type": "command", "command": "l"}})
        self.assertEqual(seat.inner_status_line(seat.settings_sources(self.env, self.project))["command"], "l")
        (self.project / ".claude" / "settings.local.json").unlink()
        self.assertEqual(seat.inner_status_line(seat.settings_sources(self.env, self.project))["command"], "u")

    def test_no_status_line_anywhere_leaves_the_inner_one_empty(self):
        self.write_desired(None)
        self.assertEqual(self.claim()[0], 0)
        self.assertEqual(self.home_file(seat.STATUS_LINE_INNER), "")
        self.assertNotIn("refreshInterval", json.loads(self.home_file(seat.SESSION_SETTINGS))["statusLine"])

    def test_a_settings_file_that_is_not_json_is_refused_78(self):
        (self.project / ".claude" / "settings.json").write_text("{", encoding="utf-8")
        code, _, err = self.claim()
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("settings.json", err)
        self.assertEqual(self.execs, [])
        self.assertFalse(self.session_home.exists())
        self.assertIsNone(inbox.probe_lock_at(self.lock_of("dev1")))

    def test_a_refused_claim_leaves_no_home(self):
        self.qa2.chmod(0o750)
        self.addCleanup(self.qa2.chmod, 0o700)
        self.assertEqual(self.claim()[0], EX_CONFIG)
        self.assertFalse(self.session_home.exists())

    def test_what_the_marker_cannot_mean_is_refused_78(self):
        cases = {
            "off (a --no-bus launch never claims)": {seat.MEMBERSHIP_VAR: seat.MEMBERSHIP_OFF},
            "a relative directory": {seat.MEMBERSHIP_VAR: "run/ccy/membership"},
            "no membership file": {seat.MEMBERSHIP_VAR: str(self.tmp)},
            "beside PINGBUS_SEATS (a mixed deploy)": {seat.SEATS_VAR: "dev1@team-a"},
        }
        for name, change in cases.items():
            with self.subTest(case=name):
                code, _, err = self.claim(env=dict(self.env, **change))
                self.assertEqual(code, EX_CONFIG, err)
                self.assertIn(seat.MEMBERSHIP_VAR, err)
        self.assertEqual(self.execs, [])
        self.assertFalse(self.session_home.exists())

    def test_a_membership_with_an_unknown_key_is_refused_78(self):
        self.write_desired(None, extra=1)
        code, _, err = self.claim()
        self.assertEqual(code, EX_CONFIG)
        self.assertIn("extra", err)


class StatusLineGuardTest(SeatCase):
    """The status line command is a shell guard (H34): with no `active` flag it runs the inner
    status line and no pingbus, so a plain session starts no Python each second."""

    def run_guard(self, *, active: bool) -> str:
        home = self.tmp / "home"
        home.mkdir()
        bin_dir = self.tmp / "bin"
        bin_dir.mkdir()
        (bin_dir / "pingbus").write_text("#!/bin/sh\necho pingbus \"$@\"\n", encoding="ascii")
        (bin_dir / "pingbus").chmod(0o755)
        (home / seat.STATUS_LINE_INNER).write_text("echo inner; cat\n", encoding="ascii")
        if active:
            (home / seat.ACTIVE_FLAG).write_text("", encoding="ascii")
        result = subprocess.run(["sh", "-c", seat.STATUS_LINE_GUARD], input="{}", capture_output=True,
                                text=True, check=True,
                                env={"PATH": f"{bin_dir}:/usr/bin:/bin", "PINGBUS_HOME": str(home)})
        return result.stdout

    def test_without_the_flag_the_inner_status_line_runs_and_reads_the_input(self):
        self.assertEqual(self.run_guard(active=False), "inner\n{}")

    def test_with_the_flag_pingbus_draws_it(self):
        self.assertEqual(self.run_guard(active=True), "pingbus status-line\n")


class RealExecTest(SeatCase):
    """The claim's descriptors survive a real `exec`: the session's processes hold every
    seat until the last of them exits, a detached one included."""

    def driver(self, *command: str) -> str:
        return textwrap.dedent(f"""
            import pathlib, sys
            from helpers.pingbus import cli
            rt = cli.Runtime(seats_root=pathlib.Path({str(self.root)!r}),
                             session_home=pathlib.Path({str(self.session_home)!r}))
            sys.exit(cli.main(["seat", "exec", "--", *{list(command)!r}], runtime=rt))
        """)

    def start(self, seats: str, *command: str) -> subprocess.Popen:
        env = dict(os.environ, PATH=self.env["PATH"], **{seat.SEATS_VAR: seats})
        child = subprocess.Popen([sys.executable, "-c", self.driver(*command)], cwd=REPO_ROOT,
                                 env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE, text=True)
        self.addCleanup(child.stderr.close)
        self.addCleanup(child.stdout.close)
        self.addCleanup(child.wait)
        self.addCleanup(child.stdin.close)
        return child

    def test_the_locks_are_held_by_the_session_until_its_last_process_exits(self):
        self.make_seat("dev1")
        self.make_seat("qa2", TEAM_B)
        session = textwrap.dedent("""
            import os, subprocess, sys
            helper = subprocess.Popen([sys.executable, "-c", "import sys; sys.stdin.read()"],
                                      stdin=subprocess.PIPE, close_fds=False)
            print(os.environ["PINGBUS_HOME"], os.environ["PINGBUS_TEAMS"], flush=True)
            sys.stdin.readline()
            helper.stdin.close()
            helper.wait()
        """)
        child = self.start("dev1@team-a,qa2@team-b", sys.executable, "-c", session)
        line = child.stdout.readline().split()
        self.assertEqual(line, [str(self.session_home), "team-a,team-b"])
        for name, team in (("dev1", TEAM_A), ("qa2", TEAM_B)):
            self.assertEqual(inbox.probe_lock_at(self.lock_of(name, team)), inbox.SEAT_KIND)
        child.stdin.write("\n")
        child.stdin.close()
        self.assertEqual(child.wait(10), 0)
        for name, team in (("dev1", TEAM_A), ("qa2", TEAM_B)):
            self.assertIsNone(inbox.probe_lock_at(self.lock_of(name, team)))

    def test_a_detached_process_keeps_the_seat_until_it_exits(self):
        self.make_seat("dev1")
        session = textwrap.dedent("""
            import subprocess, sys
            subprocess.Popen([sys.executable, "-c", "import sys; sys.stdin.read()"],
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                             close_fds=False, start_new_session=True)
        """)
        child = self.start("dev1@team-a", sys.executable, "-c", session)
        self.assertEqual(child.wait(10), 0)
        self.assertEqual(inbox.probe_lock_at(self.lock_of("dev1")), inbox.SEAT_KIND,
                         "the detached process inherited the seat")
        child.stdin.close()
        self.wait_free(self.lock_of("dev1"))


class ProtocolDocTest(unittest.TestCase):
    """docs/agent-bus-protocol.md §12-§15 state the seat layout, the session home and its
    links, `PINGBUS_SEATS`, `seat exec`, the `SEAT` line and exit 75 for a held seat."""

    @staticmethod
    def section(number: int) -> str:
        text = (REPO_ROOT / "docs" / "agent-bus-protocol.md").read_text(encoding="utf-8")
        start = text.index(f"\n## {number}. ")
        end = text.find("\n## ", start + 1)
        return " ".join(text[start:end if end != -1 else None].split())

    def test_section_12_states_the_layout_and_the_link_rule(self):
        body = self.section(12)
        for phrase in (f"`{seat.SEATS_VAR}`", f"`{seat.SESSION_HOME}`", f"`{seat.LOCK_FILE}`",
                       f"`{seat.CHECKOUT_SEATS}/<team>/<seat>/`", "`seat <claim time in ms>`",
                       "a symlink is accepted only onto a real directory",
                       f"mode {config.LINKED_BUNDLE_MODE:04o}"):
            self.assertIn(phrase, body)

    def test_section_13_names_seat_exec_and_the_seat_line(self):
        body = self.section(13)
        self.assertIn("`seat exec [--root DIR] [--home DIR] [--] CMD…`", body)
        self.assertIn("`SEAT` (team, seat, `held` or `free`, `self` or `-`, handle)", body)

    def test_section_14_gives_75_to_a_held_seat(self):
        self.assertIn("another session holds a seat (`seat exec`)", self.section(14))
        self.assertIn("another session holds a seat (`seat exec`)", cli.EXIT_CODES[cli.EXIT_BUSY])

    def test_section_15_describes_the_seat_line(self):
        self.assertIn("`SEAT`", self.section(15))


class LayoutTest(unittest.TestCase):
    def test_the_paths(self):
        self.assertTrue(protocol.is_seat_name("dev1"))
        self.assertEqual(seat.SESSION_HOME, pathlib.Path("/tmp/pingbus-home"))
        self.assertEqual(seat.CCY_SEATS_ROOT, pathlib.Path("/workspace/.claude/ccy/pingbus/seats"))
        self.assertEqual(seat.checkout_seats_root(pathlib.Path("/c")),
                         pathlib.Path("/c/.claude/ccy/pingbus/seats"))
        self.assertEqual(seat.LOCK_FILE, "seat.lock")
        self.assertEqual(seat.SEATS_VAR, "PINGBUS_SEATS")


if __name__ == "__main__":
    unittest.main()

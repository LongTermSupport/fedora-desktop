"""Unit tests for `pingbus seat disconnected` (Plan 00194 Task 2.0b, protocol §12 and §13):
the notice the ccy launcher posts from the host, once its container has gone, for each seat
the session last held that ended without the goodbye. Against the fake homeserver of test_cli.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_seat_disconnected
"""

from __future__ import annotations

import errno
import fcntl
import io
import json
import os
import pathlib
import shutil
import signal
import sys
import time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, config, inbox, membership, seat
from tests.helpers.pingbus import test_cli as bus
from tests.helpers.pingbus.test_seat_retire import SeatBusCase

SESSION = "ccy-myrepo-1"
REF = seat.SeatRef("1", bus.TEAM_A)
#: The launch began at SINCE; its keeper claimed the seat at CLAIM.
SINCE = int(bus.T0 * 1000) - 60_000
CLAIM = SINCE + 1_500
NOTICE = f"{bus.ME_LP} disconnected: session ended without a goodbye"


class DisconnectedCase(SeatBusCase):
    def setUp(self) -> None:
        super().setUp()
        self.lock = seat.lock_path(self.root, REF)
        self.report(REF)

    def report(self, *refs: seat.SeatRef) -> None:
        """The keeper's report beside the checkout's seats: the seats the session last held."""
        directory = self.root.parent / seat.SESSIONS_DIR / SESSION
        directory.mkdir(parents=True, exist_ok=True)
        handle = config.read_member(bus.TEAM_A, self.seat_dir).handle
        membership.write_applied(directory / membership.APPLIED_FILE, membership.Applied(
            1, tuple(membership.AppliedSeat(ref.team, ref.seat, handle) for ref in refs), ()))

    def died_holding(self, claimed_ms: int = CLAIM) -> None:
        """What a keeper killed with its container leaves: the claim, and no lock held."""
        self.lock.write_text(f"{inbox.SEAT_KIND} {claimed_ms}", encoding="ascii")

    def said_goodbye(self, claimed_ms: int) -> None:
        (self.seat_dir / seat.SAID_GOODBYE_FILE).write_text(
            json.dumps({"v": 1, "claimed_ms": claimed_ms}), encoding="utf-8")

    def disconnected(self, *extra: str, since: int = SINCE, **kwargs) -> tuple[int, str, str]:
        return self.run_cli("seat", "disconnected", "--root", str(self.root), "--session", SESSION,
                            "--since", str(since), *extra, **kwargs)

    def notices(self) -> list[str]:
        return [content["body"] for content in self.a.messages_from_me()]


class DisconnectedTest(DisconnectedCase):
    def test_a_seat_whose_holder_died_without_a_goodbye_is_told(self):
        self.joined()
        self.died_holding()
        code, out, err = self.disconnected()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(out, "")
        (content,) = self.a.messages_from_me()
        self.assertIn(NOTICE, content["body"])
        self.assertEqual(set(content["m.mentions"]["user_ids"]), {self.a.alice, self.a.orch},
                         "to the team's humans and coordinator(s), as the goodbye")
        self.assertIn("1@team-a", err)
        self.assertEqual(self.lock.read_text(encoding="ascii"), "", "the claim is told once")
        self.assertIsNone(inbox.probe_lock_at(self.lock))

    def test_the_text_is_the_fixed_template(self):
        self.assertEqual(cli.DISCONNECT_TEXT.format(handle=bus.ME_LP), NOTICE)

    def test_a_rerun_posts_nothing_more(self):
        self.joined()
        self.died_holding()
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertEqual(len(self.notices()), 1)

    def test_a_goodbye_said_for_that_claim_posts_nothing(self):
        self.joined()
        self.died_holding()
        self.said_goodbye(CLAIM)
        code, _, err = self.disconnected()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.notices(), [])
        self.assertEqual(self.lock.read_text(encoding="ascii"), "")

    def test_a_goodbye_said_in_an_earlier_tenure_does_not_count(self):
        self.joined()
        self.died_holding()
        self.said_goodbye(CLAIM - 1)
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertEqual(len(self.notices()), 1)

    def test_an_unreadable_goodbye_record_is_no_goodbye(self):
        self.joined()
        self.died_holding()
        (self.seat_dir / seat.SAID_GOODBYE_FILE).write_text("{", encoding="utf-8")
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertEqual(len(self.notices()), 1)

    def test_a_claim_before_this_launch_is_not_its_to_tell_and_is_left_alone(self):
        self.died_holding(SINCE - 1)
        code, _, err = self.disconnected()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.notices(), [])
        self.assertEqual(self.lock.read_text(encoding="ascii"), f"seat {SINCE - 1}")

    def test_an_orderly_release_posts_nothing(self):
        """A keeper that let its seat go (a removal, whose farewell the host posts) empties it."""
        lock = inbox.acquire_lock_at(self.lock, inbox.SEAT_KIND, claimed_ms=CLAIM)
        lock.release()
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertEqual(self.notices(), [])

    def test_a_seat_never_claimed_posts_nothing(self):
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertFalse(self.lock.exists())

    def test_a_session_with_no_report_has_nothing_to_tell(self):
        self.died_holding()
        code, _, err = self.run_cli("seat", "disconnected", "--root", str(self.root),
                                    "--session", "ccy-other-2", "--since", str(SINCE))
        self.assertEqual((code, err), (cli.EXIT_OK, ""))
        self.assertEqual(self.notices(), [])

    def test_only_the_seats_the_session_held_are_told(self):
        other = seat.SeatRef("2", bus.TEAM_A)
        other_dir = seat.seat_dir(self.root, other)
        other_dir.mkdir(mode=0o700)
        (other_dir / config.MEMBER_FILE).write_text((self.seat_dir / config.MEMBER_FILE).read_text())
        seat.lock_path(self.root, other).write_text(f"seat {CLAIM}", encoding="ascii")
        self.joined()
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertEqual(self.notices(), [])
        self.assertEqual(seat.lock_path(self.root, other).read_text(encoding="ascii"), f"seat {CLAIM}")

    def test_a_refused_report_is_78(self):
        path = self.root.parent / seat.SESSIONS_DIR / SESSION / membership.APPLIED_FILE
        path.write_text("{", encoding="utf-8")
        code, _, err = self.disconnected()
        self.assertEqual(code, cli.EXIT_CONFIG)
        self.assertIn(str(path), err)

    def test_a_homeserver_down_fails_and_says_so(self):
        self.died_holding()
        self.a.write_bundle(base_url=f"http://127.0.0.1:{bus.free_port()}")
        code, _, err = self.disconnected()
        self.assertNotEqual(code, cli.EXIT_OK)
        self.assertIn("1@team-a", err)
        self.assertIn("homeserver", err)

    def test_the_arguments_are_checked(self):
        for argv in (["--since", "-1"], ["--since", "x"], ["--session", "../x"], ["--root", "rel"]):
            with self.subTest(argv=argv):
                args = {"--root": str(self.root), "--session": SESSION, "--since": str(SINCE)}
                args.update(zip(argv[::2], argv[1::2]))
                code, _, _ = self.run_cli("seat", "disconnected", *[x for kv in args.items() for x in kv])
                self.assertEqual(code, cli.EXIT_USAGE, argv)


class DisconnectedWaitTest(DisconnectedCase):
    """A closed terminal: the launcher's exit runs while the container is still stopping, so a
    seat the session held is waited for, bounded, before it is judged."""

    def runtime(self, *args, **kwargs) -> cli.Runtime:
        rt = super().runtime(*args, **kwargs)
        rt.sleep = time.sleep
        rt.disconnect_wait_s = 1.0
        return rt

    def hold_as_a_process_that_will_die(self) -> int:
        """Hold the lock as the keeper does, and let it go as its death would: no emptying."""
        fd = os.open(self.lock, os.O_RDWR | os.O_CREAT, 0o600)
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        os.pwrite(fd, f"seat {CLAIM}".encode("ascii"), 0)
        return fd

    def test_a_seat_let_go_during_the_wait_is_told(self):
        self.joined()
        fd = self.hold_as_a_process_that_will_die()
        self.later(0.3, lambda: os.close(fd))
        code, _, err = self.disconnected()
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(len(self.notices()), 1)

    def test_a_seat_still_held_after_the_wait_is_left_alone_and_said(self):
        lock = inbox.acquire_lock_at(self.lock, inbox.SEAT_KIND, claimed_ms=CLAIM)
        self.addCleanup(lock.release)
        started = time.monotonic()
        code, _, err = self.disconnected()
        self.assertLess(time.monotonic() - started, 5)
        self.assertEqual(code, cli.EXIT_OK, err)
        self.assertEqual(self.notices(), [])
        self.assertIn("1@team-a", err)
        self.assertIn("held", err)
        (line,) = (self.root.parent / cli.DISCONNECT_LOG).read_text(encoding="utf-8").splitlines()
        self.assertIn("seat=1@team-a not told: still held", line)

    def test_the_default_wait_is_bounded(self):
        self.assertTrue(5 <= cli.Runtime().disconnect_wait_s <= 30)

    def test_a_ctrl_c_or_a_hangup_while_it_runs_does_not_stop_it(self):
        """Ctrl-C mashed after the container has gone reaches the launcher's process group."""
        self.joined()
        fd = self.hold_as_a_process_that_will_die()
        self.later(0.3, lambda: os.close(fd))
        sent: list[int] = []

        def sleep(seconds: float) -> None:
            for signum in (signal.SIGINT, signal.SIGHUP):
                if signum not in sent:
                    sent.append(signum)
                    os.kill(os.getpid(), signum)
            time.sleep(seconds)

        before = {signum: signal.getsignal(signum) for signum in (signal.SIGINT, signal.SIGHUP)}
        rt = self.runtime()
        rt.sleep = sleep
        err = io.StringIO()
        code = cli.main(["seat", "disconnected", "--root", str(self.root), "--session", SESSION,
                         "--since", str(SINCE)], environ=self.env, stdout=io.StringIO(), stderr=err,
                        stdin=io.BytesIO(b""), runtime=rt)
        self.assertEqual(code, cli.EXIT_OK, err.getvalue())
        self.assertEqual(sent, [signal.SIGINT, signal.SIGHUP])
        self.assertEqual(len(self.notices()), 1)
        self.assertEqual({signum: signal.getsignal(signum) for signum in before}, before,
                         "the handlers are put back")


class DisconnectedLogTest(DisconnectedCase):
    """Every seat's outcome, told or not, is a line in the checkout's disconnect log beside its
    seats and sessions, owner-only: the one record a closed terminal leaves (G3)."""

    def log(self) -> pathlib.Path:
        return self.root.parent / cli.DISCONNECT_LOG

    def lines(self) -> list[str]:
        return self.log().read_text(encoding="utf-8").splitlines()

    def only_line(self) -> str:
        (line,) = self.lines()
        self.assertIn(f"session={SESSION}", line)
        return line

    def test_a_notice_posted_is_logged_owner_only_without_its_text(self):
        self.joined()
        self.died_holding()
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        line = self.only_line()
        self.assertIn("seat=1@team-a told:", line)
        self.assertNotIn(NOTICE, line)
        self.assertNotIn(bus.ME_LP, line)
        self.assertEqual(self.log().stat().st_mode & 0o777, 0o600)

    def test_a_log_left_readable_by_others_is_made_owner_only(self):
        self.log().write_text("", encoding="utf-8")
        self.log().chmod(0o644)
        self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertEqual(self.log().stat().st_mode & 0o777, 0o600)

    def test_each_reason_for_telling_no_one_is_logged(self):
        cases = {
            "no claim": lambda: None,
            "older than this launch": lambda: self.died_holding(SINCE - 1),
            "goodbye already posted": lambda: (self.died_holding(), self.said_goodbye(CLAIM)),
        }
        for reason, arrange in cases.items():
            with self.subTest(reason=reason):
                self.lock.unlink(missing_ok=True)
                (self.seat_dir / seat.SAID_GOODBYE_FILE).unlink(missing_ok=True)
                self.log().unlink(missing_ok=True)
                arrange()
                self.assertEqual(self.disconnected()[0], cli.EXIT_OK)
                self.assertIn(f"seat=1@team-a not told: {reason}", self.only_line())
                self.assertEqual(self.notices(), [])

    def test_a_session_with_no_report_is_logged(self):
        code, _, err = self.run_cli("seat", "disconnected", "--root", str(self.root),
                                    "--session", "ccy-other-2", "--since", str(SINCE))
        self.assertEqual((code, err), (cli.EXIT_OK, ""))
        (line,) = self.lines()
        self.assertIn("session=ccy-other-2 seat=- not told: no applied session report", line)

    def test_a_failure_is_logged(self):
        self.died_holding()
        self.a.write_bundle(base_url=f"http://127.0.0.1:{bus.free_port()}")
        self.assertNotEqual(self.disconnected()[0], cli.EXIT_OK)
        self.assertIn("seat=1@team-a failed:", self.only_line())

    def test_lines_are_appended_across_runs(self):
        self.joined()
        self.died_holding()
        self.disconnected()
        self.disconnected()
        self.assertEqual([("seat=1@team-a told:" in line, "no claim" in line) for line in self.lines()],
                         [(True, False), (False, True)])

    def test_a_log_that_cannot_be_written_fails_after_the_notice(self):
        self.joined()
        self.died_holding()
        self.log().mkdir()
        code, _, err = self.disconnected()
        self.assertEqual(code, cli.EXIT_CONFIG)
        self.assertIn(cli.DISCONNECT_LOG, err)
        self.assertEqual(len(self.notices()), 1, "the team was told first")


class _DeadTerminal(io.TextIOBase):
    """The stderr of a process whose terminal was closed: every write fails with EIO."""

    def write(self, text: str) -> int:
        raise OSError(errno.EIO, os.strerror(errno.EIO))


class DisconnectedDeadStderrTest(DisconnectedCase):
    """A closed terminal or tmux window: the pty has gone, so stderr cannot be written."""

    def setUp(self) -> None:
        super().setUp()
        self.b = self.add_team()
        self.b_dir = self.root / bus.TEAM_B / "1"
        self.b_dir.mkdir(parents=True)
        for directory in (self.root / bus.TEAM_B, self.b_dir):
            directory.chmod(0o700)
        shutil.rmtree(self.b.bundle)
        self.b.bundle = self.b_dir
        self.b.write_bundle()
        (self.home / bus.TEAM_B).symlink_to(self.b_dir)
        self.ref_b = seat.SeatRef("1", bus.TEAM_B)
        self.report(REF, self.ref_b)
        self.joined()

    def run_dead(self) -> int:
        return cli.main(["seat", "disconnected", "--root", str(self.root), "--session", SESSION,
                         "--since", str(SINCE)], environ=self.env, stdout=io.StringIO(),
                        stderr=_DeadTerminal(), stdin=io.BytesIO(b""), runtime=self.runtime())

    def test_two_seats_are_both_told_with_stderr_gone(self):
        self.died_holding()
        seat.lock_path(self.root, self.ref_b).write_text(f"seat {CLAIM}", encoding="ascii")
        self.assertEqual(self.run_dead(), cli.EXIT_OK)
        self.assertEqual(len(self.notices()), 1, "team A")
        self.assertEqual(len([c for c in self.b.messages_from_me() if NOTICE in c["body"]]), 1, "team B")
        log = (self.root.parent / cli.DISCONNECT_LOG).read_text(encoding="utf-8")
        self.assertEqual(log.count(" told: claim"), 2, "the log is written with stderr gone")

    def test_a_failure_with_stderr_gone_is_still_the_exit_and_the_other_seat_is_told(self):
        self.died_holding()
        seat.lock_path(self.root, self.ref_b).write_text(f"seat {CLAIM}", encoding="ascii")
        self.a.write_bundle(base_url=f"http://127.0.0.1:{bus.free_port()}")
        self.assertNotEqual(self.run_dead(), cli.EXIT_OK)
        self.assertEqual(len([c for c in self.b.messages_from_me() if NOTICE in c["body"]]), 1)

    def test_every_seat_is_told_before_any_line_is_written(self):
        self.died_holding()
        seat.lock_path(self.root, self.ref_b).write_text(f"seat {CLAIM}", encoding="ascii")
        told_at_first_line: list[int] = []

        class Watching(io.StringIO):
            def write(inner, text: str) -> int:
                if not told_at_first_line:
                    told_at_first_line.append(len(self.notices()) + len(self.b.messages_from_me()))
                return super().write(text)

        err = Watching()
        code = cli.main(["seat", "disconnected", "--root", str(self.root), "--session", SESSION,
                         "--since", str(SINCE)], environ=self.env, stdout=io.StringIO(), stderr=err,
                        stdin=io.BytesIO(b""), runtime=self.runtime())
        self.assertEqual(code, cli.EXIT_OK, err.getvalue())
        self.assertEqual(told_at_first_line, [2])
        self.assertEqual(err.getvalue().count("✓ seat"), 2)

    def test_a_stderr_that_fails_otherwise_is_not_hidden(self):
        """Only a terminal that has gone is tolerated; any other write failure is raised."""
        self.died_holding()

        class Full(io.TextIOBase):
            def write(self, text: str) -> int:
                raise OSError(errno.ENOSPC, os.strerror(errno.ENOSPC))

        with self.assertRaises(OSError):
            cli.main(["seat", "disconnected", "--root", str(self.root), "--session", SESSION,
                      "--since", str(SINCE)], environ=self.env, stdout=io.StringIO(), stderr=Full(),
                     stdin=io.BytesIO(b""), runtime=self.runtime())
        self.assertEqual(len(self.notices()), 1, "the seat was told before the report failed")

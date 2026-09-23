"""Unit tests for helpers/play_lock/lock.py — the one lock every play run on a host shares.

Run from the repo root:

    python3 -m unittest tests.helpers.play_lock.test_lock

The lock is a real flock(2) on a real file in a temporary runtime directory, so two
"holders" here are two genuinely separate open file descriptions contending in the kernel,
the same thing two processes do.
"""

from __future__ import annotations

import fcntl
import io
import os
import pathlib
import pwd
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.play_lock import lock

UID = os.getuid()
GID = os.getgid()


class _RuntimeDir:
    """A private 0700 runtime directory, the shape /run/user/<uid> has."""

    def __enter__(self) -> str:
        self._tmp = tempfile.TemporaryDirectory()
        os.chmod(self._tmp.name, 0o700)
        return self._tmp.name

    def __exit__(self, *exc: object) -> None:
        self._tmp.cleanup()


class TestRuntimeDirFor(unittest.TestCase):
    def test_own_user_uses_xdg_runtime_dir(self) -> None:
        self.assertEqual(
            lock.runtime_dir_for(UID, env={"XDG_RUNTIME_DIR": "/somewhere"}, euid=UID),
            "/somewhere",
        )

    def test_own_user_without_xdg_uses_run_user(self) -> None:
        self.assertEqual(lock.runtime_dir_for(UID, env={}, euid=UID), f"/run/user/{UID}")

    def test_another_user_ignores_the_callers_xdg(self) -> None:
        """Root serving a user must not lock in ROOT's runtime directory."""
        self.assertEqual(
            lock.runtime_dir_for(1234, env={"XDG_RUNTIME_DIR": "/run/user/0"}, euid=0),
            "/run/user/1234",
        )


class TestOpenLock(unittest.TestCase):
    def test_creates_a_private_regular_file(self) -> None:
        with _RuntimeDir() as rt:
            fd = lock.open_lock(rt, UID, GID)
            try:
                st = os.fstat(fd)
                self.assertTrue(stat.S_ISREG(st.st_mode))
                self.assertEqual(stat.S_IMODE(st.st_mode), 0o600)
                self.assertEqual(st.st_uid, UID)
                self.assertEqual(os.path.basename(lock.lock_path(rt)), lock.LOCK_NAME)
            finally:
                os.close(fd)

    def test_reopens_an_existing_file(self) -> None:
        with _RuntimeDir() as rt:
            os.close(lock.open_lock(rt, UID, GID))
            fd = lock.open_lock(rt, UID, GID)
            os.close(fd)

    def test_a_symlinked_lock_file_is_refused(self) -> None:
        """Root opens this path on a user's behalf; following a link would let the user
        aim root at any file on the system."""
        with _RuntimeDir() as rt, tempfile.NamedTemporaryFile() as target:
            os.symlink(target.name, lock.lock_path(rt))
            with self.assertRaisesRegex(lock.LockError, "symlink"):
                lock.open_lock(rt, UID, GID)

    def test_a_dangling_symlink_is_refused_not_created_through(self) -> None:
        with _RuntimeDir() as rt, tempfile.TemporaryDirectory() as elsewhere:
            target = os.path.join(elsewhere, "made-by-root")
            os.symlink(target, lock.lock_path(rt))
            with self.assertRaises(lock.LockError):
                lock.open_lock(rt, UID, GID)
            self.assertFalse(os.path.exists(target))

    def test_a_directory_at_the_lock_path_is_refused(self) -> None:
        with _RuntimeDir() as rt:
            os.mkdir(lock.lock_path(rt))
            with self.assertRaises(lock.LockError):
                lock.open_lock(rt, UID, GID)

    def test_a_missing_runtime_dir_is_refused(self) -> None:
        with _RuntimeDir() as rt:
            with self.assertRaisesRegex(lock.LockError, "does not exist"):
                lock.open_lock(os.path.join(rt, "absent"), UID, GID)

    def test_a_group_writable_runtime_dir_is_refused(self) -> None:
        with _RuntimeDir() as rt:
            os.chmod(rt, 0o770)
            with self.assertRaisesRegex(lock.LockError, "writable"):
                lock.open_lock(rt, UID, GID)

    def test_a_runtime_dir_owned_by_someone_else_is_refused(self) -> None:
        with _RuntimeDir() as rt:
            with self.assertRaisesRegex(lock.LockError, "owned"):
                lock.open_lock(rt, UID + 1, GID)

    @unittest.skipUnless(os.geteuid() == 0, "root serving another user needs root")
    def test_root_creates_the_file_owned_by_the_user_it_serves(self) -> None:
        """The cycle's orchestrator is root. A lock file it creates must belong to the
        user, or that user's own run.bash could not open it read-write."""
        other = 54321
        with _RuntimeDir() as rt:
            os.chown(rt, other, other)
            fd = lock.open_lock(rt, other, other)
            try:
                st = os.fstat(fd)
                self.assertEqual((st.st_uid, st.st_gid), (other, other))
            finally:
                os.close(fd)

    def test_a_symlinked_runtime_dir_is_refused(self) -> None:
        with _RuntimeDir() as rt:
            link = os.path.join(rt, "link")
            real = os.path.join(rt, "real")
            os.mkdir(real, 0o700)
            os.symlink(real, link)
            with self.assertRaises(lock.LockError):
                lock.open_lock(link, UID, GID)


class TestTwoHolders(unittest.TestCase):
    def test_the_second_holder_is_refused_until_the_first_lets_go(self) -> None:
        with _RuntimeDir() as rt:
            first = lock.open_lock(rt, UID, GID)
            second = lock.open_lock(rt, UID, GID)
            try:
                self.assertTrue(lock.acquire(first))
                lock.write_note(first, "cycle")
                self.assertFalse(lock.acquire(second))
                self.assertIn("what=cycle", lock.read_note(second))
                self.assertIn(f"pid={os.getpid()}", lock.read_note(second))
                os.close(first)
                first = -1
                self.assertTrue(lock.acquire(second))
            finally:
                if first != -1:
                    os.close(first)
                os.close(second)

    def test_a_bounded_wait_gets_the_lock_when_the_holder_releases(self) -> None:
        with _RuntimeDir() as rt:
            holder = lock.open_lock(rt, UID, GID)
            waiter = lock.open_lock(rt, UID, GID)
            now = [0.0]
            released = []

            def sleep(seconds: float) -> None:
                now[0] += seconds
                if now[0] >= 2 and not released:
                    os.close(holder)
                    released.append(True)

            try:
                self.assertTrue(lock.acquire(holder))
                self.assertTrue(lock.acquire(waiter, wait_seconds=5, sleep=sleep, clock=lambda: now[0]))
                self.assertEqual(released, [True])
            finally:
                if not released:
                    os.close(holder)
                os.close(waiter)

    def test_a_bounded_wait_gives_up_at_its_bound(self) -> None:
        with _RuntimeDir() as rt:
            holder = lock.open_lock(rt, UID, GID)
            waiter = lock.open_lock(rt, UID, GID)
            now = [0.0]

            def sleep(seconds: float) -> None:
                now[0] += seconds

            try:
                self.assertTrue(lock.acquire(holder))
                self.assertFalse(lock.acquire(waiter, wait_seconds=3, sleep=sleep, clock=lambda: now[0]))
                self.assertGreaterEqual(now[0], 3)
            finally:
                os.close(holder)
                os.close(waiter)


class TestHeld(unittest.TestCase):
    def test_an_inherited_held_descriptor_is_recognised(self) -> None:
        with _RuntimeDir() as rt:
            fd = lock.open_lock(rt, UID, GID)
            try:
                self.assertTrue(lock.acquire(fd))
                ok, why = lock.held({lock.FD_ENV: str(fd)}, rt)
                self.assertTrue(ok, why)
            finally:
                os.close(fd)

    def test_no_delegation_is_not_held_and_says_nothing(self) -> None:
        with _RuntimeDir() as rt:
            self.assertEqual(lock.held({}, rt), (False, ""))

    def test_a_descriptor_for_another_file_is_refused(self) -> None:
        with _RuntimeDir() as rt, tempfile.TemporaryFile() as other:
            os.close(lock.open_lock(rt, UID, GID))
            fcntl.flock(other.fileno(), fcntl.LOCK_EX)
            ok, why = lock.held({lock.FD_ENV: str(other.fileno())}, rt)
            self.assertFalse(ok)
            self.assertIn("not the play lock", why)

    def test_a_descriptor_that_does_not_hold_the_lock_is_refused(self) -> None:
        """A second open of the same file while someone else holds it: same inode, but
        it is not the holder, so trusting it would let two runs overlap."""
        with _RuntimeDir() as rt:
            holder = lock.open_lock(rt, UID, GID)
            imposter = lock.open_lock(rt, UID, GID)
            try:
                self.assertTrue(lock.acquire(holder))
                ok, why = lock.held({lock.FD_ENV: str(imposter)}, rt)
                self.assertFalse(ok)
                self.assertIn("does not hold", why)
            finally:
                os.close(holder)
                os.close(imposter)

    def test_a_garbage_value_is_refused(self) -> None:
        with _RuntimeDir() as rt:
            ok, why = lock.held({lock.FD_ENV: "three"}, rt)
            self.assertFalse(ok)
            self.assertIn("not a file descriptor", why)

    def test_a_closed_descriptor_is_refused(self) -> None:
        with _RuntimeDir() as rt:
            fd = lock.open_lock(rt, UID, GID)
            os.close(fd)
            ok, _why = lock.held({lock.FD_ENV: str(fd)}, rt)
            self.assertFalse(ok)


class TestCli(unittest.TestCase):
    def _run(self, argv: list[str], env: dict[str, str]) -> tuple[int, str, str]:
        """Always names the current user: under root (the CCY container) the CLI refuses
        to guess, and naming yourself resolves to your own runtime directory either way."""
        out, err = io.StringIO(), io.StringIO()
        code = lock.main([*argv, "--user", pwd.getpwuid(UID).pw_name], env=env, stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def test_path_prints_the_validated_path_and_creates_the_file(self) -> None:
        with _RuntimeDir() as rt:
            code, out, _err = self._run(["path"], {"XDG_RUNTIME_DIR": rt})
            self.assertEqual(code, 0)
            self.assertEqual(out, lock.lock_path(rt) + "\n")
            self.assertTrue(os.path.isfile(lock.lock_path(rt)))

    def test_path_refuses_an_unsafe_runtime_dir_with_a_reason(self) -> None:
        with _RuntimeDir() as rt:
            os.chmod(rt, 0o777)
            code, out, err = self._run(["path"], {"XDG_RUNTIME_DIR": rt})
            self.assertEqual(code, lock.EXIT_ERROR)
            self.assertEqual(out, "")
            self.assertIn("writable", err)

    def test_held_exits_zero_only_for_a_real_delegation(self) -> None:
        with _RuntimeDir() as rt:
            code, _out, err = self._run(["held"], {"XDG_RUNTIME_DIR": rt})
            self.assertEqual((code, err), (1, ""))
            fd = lock.open_lock(rt, UID, GID)
            try:
                self.assertTrue(lock.acquire(fd))
                code, _out, _err = self._run(["held"], {"XDG_RUNTIME_DIR": rt, lock.FD_ENV: str(fd)})
                self.assertEqual(code, 0)
            finally:
                os.close(fd)

    def test_held_reports_a_bogus_delegation(self) -> None:
        with _RuntimeDir() as rt:
            code, _out, err = self._run(["held"], {"XDG_RUNTIME_DIR": rt, lock.FD_ENV: "99999"})
            self.assertEqual(code, lock.EXIT_ERROR)
            self.assertIn(lock.FD_ENV, err)

    def test_root_must_name_the_user(self) -> None:
        """Root's own runtime directory is not where the user's play runs lock."""
        err = io.StringIO()
        code = lock.main(["path"], env={}, stdout=io.StringIO(), stderr=err, euid=0)
        self.assertEqual(code, lock.EXIT_ERROR)
        self.assertIn("--user", err.getvalue())


if __name__ == "__main__":
    unittest.main()

"""Tests for files/usr/local/bin/agent-bus, the root wrapper around the admin tool.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_wrapper

DESIGN.md section 5.1: the `agent-bus` user cannot write into a human's 0700 home, so
`add-member` writes the bundle to its stdout as a tar and the root wrapper unpacks it in a
fresh private directory. With `--out=DIR` it places each file there as the sudo user
(setpriv), 0700 directory and 0600 files, never as root; with `add-member --out=-` it
prints the three files as a tar on stdout and writes nothing (D61). The wrapper is sourced
with its `run_admin_tool` replaced, so these tests need neither the `agent-bus` user nor
the zipapp.
"""

from __future__ import annotations

import io
import os
import pathlib
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest

REPO = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO))

from helpers.agent_bus import admin
from helpers.pingbus import config
from tests.helpers.agent_bus.test_admin import AdminTestCase

WRAPPER = REPO / "files" / "usr" / "local" / "bin" / "agent-bus"
#: As root the bundle goes to another user, as `sudo` makes it; otherwise to the caller.
ROOT = os.geteuid() == 0
OWNER = 4321 if ROOT else os.getuid()
GROUP = 4322 if ROOT else os.getgid()
#: Replaces the privilege drop: records its arguments, writes the canned tar.
STUB = 'run_admin_tool() { printf "%s\\n" "$@" > "$ARGS_FILE"; cat "$TAR_FILE"; }'


def make_tar(files: dict[str, bytes]) -> bytes:
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as tar:
        for name, data in files.items():
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(data), 0o644
            tar.addfile(info, io.BytesIO(data))
    return buffer.getvalue()


BUNDLE = {"member.json": b'{"team": "team-a"}', "token": b"syt_abc", "README": b"next steps"}


def owner_home(base: pathlib.Path) -> pathlib.Path:
    """`base/home`, the sudo user's own directory: the wrapper writes there as that user,
    so `base` is made passable and `home` theirs."""
    os.chmod(base, 0o711)
    home = base / "home"
    home.mkdir(mode=0o700)
    if ROOT:
        os.chown(home, OWNER, GROUP)
    return home


class WrapperTest(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = pathlib.Path(tmp.name)
        self.args_file = self.tmp / "args"
        self.tar_file = self.tmp / "bundle.tar"
        self.tar_file.write_bytes(make_tar(BUNDLE))
        self.out = owner_home(self.tmp) / "pingbus" / "team-a"

    def run_wrapper(self, *args: str, sudo: bool = True, stub: str = STUB,
                    text: bool = True) -> subprocess.CompletedProcess:
        env = {"PATH": os.environ["PATH"], "ARGS_FILE": str(self.args_file), "TAR_FILE": str(self.tar_file)}
        if sudo:
            env.update({"SUDO_UID": str(OWNER), "SUDO_GID": str(GROUP)})
        script = f'source "$1"; {stub}; shift; main "$@"'
        return subprocess.run(["bash", "-c", script, "bash", str(WRAPPER), *args],
                              capture_output=True, text=text, env=env, check=False)

    def make_out(self, mode: int = 0o700) -> None:
        """The sudo user's existing bundle directory, as rotate-token finds it."""
        for path in (self.out.parent, self.out):
            path.mkdir(mode=mode, exist_ok=True)
            os.chmod(path, mode)
            if ROOT:
                os.chown(path, OWNER, GROUP)
        (self.out / "member.json").write_text("{}")

    def files_under_tmp(self) -> list[str]:
        return sorted(str(p.relative_to(self.tmp)) for p in self.tmp.rglob("*"))

    # ── --out=-: the bundle on stdout, nothing written (D61) ──────────────────────────

    def test_out_dash_prints_the_bundle_and_writes_nothing(self) -> None:
        before = self.files_under_tmp()
        run = self.run_wrapper("add-member", "team-a", "--seat=dev1", "--out=-", text=False)
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(self.tool_args(), ["add-member", "team-a", "--seat=dev1"])
        self.assertEqual(self.files_under_tmp(), sorted([*before, "args"]))
        with tarfile.open(fileobj=io.BytesIO(run.stdout), mode="r:") as tar:
            members = tar.getmembers()
            self.assertEqual(sorted(m.name for m in members), sorted(BUNDLE))
            for member in members:
                self.assertTrue(member.isreg())
                self.assertEqual(member.mode, 0o600)
                self.assertEqual(tar.extractfile(member).read(), BUNDLE[member.name])
        self.assertNotIn(b"syt_abc", run.stderr)

    def test_out_dash_passes_only_the_bundle_files(self) -> None:
        self.tar_file.write_bytes(make_tar({**BUNDLE, "../escape": b"x", "extra": b"y"}))
        run = self.run_wrapper("add-member", "team-a", "--out=-", text=False)
        self.assertEqual(run.returncode, 0, run.stderr)
        with tarfile.open(fileobj=io.BytesIO(run.stdout), mode="r:") as tar:
            self.assertEqual(sorted(tar.getnames()), sorted(BUNDLE))

    def test_out_dash_with_an_incomplete_bundle_prints_nothing(self) -> None:
        self.tar_file.write_bytes(make_tar({"token": b"syt_abc"}))
        run = self.run_wrapper("add-member", "team-a", "--out=-", text=False)
        self.assertNotEqual(run.returncode, 0)
        self.assertEqual(run.stdout, b"")

    def test_out_dash_is_for_add_member_only(self) -> None:
        run = self.run_wrapper("rotate-token", "team-a", "h.1+x.podman", "--out=-")
        self.assertEqual(run.returncode, 64)
        self.assertFalse(self.args_file.exists())

    # ── --out=DIR: written as the sudo user, never as root (D61) ────────────────────

    @unittest.skipUnless(ROOT, "the privilege drop is seen only from root")
    def test_out_dir_is_written_as_the_sudo_user_not_root(self) -> None:
        # A directory the sudo user cannot write in: root could, the user cannot, so a
        # bundle placed there would mean root wrote it.
        system_dir = self.tmp / "system-dir"
        system_dir.mkdir(mode=0o755)
        os.chmod(system_dir, 0o755)
        link = self.out.parent.parent / "link"
        link.symlink_to(system_dir)
        for out in (system_dir / "team-a", link / "team-a"):
            with self.subTest(out=out):
                run = self.run_wrapper("add-member", "team-a", f"--out={out}")
                self.assertNotEqual(run.returncode, 0)
                self.assertEqual(list(system_dir.iterdir()), [])
                self.assertNotIn("syt_abc", run.stderr)

    def tool_args(self) -> list[str]:
        return self.args_file.read_text().splitlines()

    def test_add_member_places_the_bundle(self) -> None:
        run = self.run_wrapper("add-member", "team-a", "--role=worker", f"--out={self.out}")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(run.stdout, "")
        self.assertEqual(self.tool_args(), ["add-member", "team-a", "--role=worker"])
        info = self.out.stat()
        self.assertEqual((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)), (OWNER, GROUP, 0o700))
        for name, data in BUNDLE.items():
            path = self.out / name
            info = path.lstat()
            self.assertTrue(stat.S_ISREG(info.st_mode))
            self.assertEqual((info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)), (OWNER, GROUP, 0o600))
            self.assertEqual(path.read_bytes(), data)
        self.assertNotIn("syt_abc", run.stderr)

    def test_extra_or_hostile_tar_members_are_not_placed(self) -> None:
        self.tar_file.write_bytes(make_tar({**BUNDLE, "../escape": b"x", "extra": b"y"}))
        run = self.run_wrapper("add-member", "team-a", f"--out={self.out}")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(sorted(p.name for p in self.out.iterdir()), sorted(BUNDLE))
        self.assertFalse((self.out.parent / "escape").exists())

    def test_incomplete_tar_places_nothing(self) -> None:
        self.tar_file.write_bytes(make_tar({"token": b"syt_abc"}))
        run = self.run_wrapper("add-member", "team-a", f"--out={self.out}")
        self.assertNotEqual(run.returncode, 0)
        self.assertFalse(self.out.exists())

    def test_tool_failure_places_nothing(self) -> None:
        run = self.run_wrapper("add-member", "team-a", f"--out={self.out}",
                               stub='run_admin_tool() { echo "refused" >&2; return 70; }')
        self.assertEqual(run.returncode, 70)
        self.assertFalse(self.out.exists())

    def test_existing_bundle_is_not_overwritten(self) -> None:
        self.out.mkdir(parents=True)
        (self.out / "token").write_text("old")
        run = self.run_wrapper("add-member", "team-a", f"--out={self.out}")
        self.assertEqual(run.returncode, 64)
        self.assertEqual((self.out / "token").read_text(), "old")
        self.assertFalse(self.args_file.exists())

    def test_add_member_refuses_any_existing_out(self) -> None:
        # Root would otherwise chown and chmod it, through a symlink too (/etc, say).
        target = self.tmp / "system-dir"
        target.mkdir(mode=0o755)
        link = self.tmp / "link"
        link.symlink_to(target)
        dangling = self.tmp / "dangling"
        dangling.symlink_to(self.tmp / "nowhere")
        before = target.stat()
        for out in (target, link, dangling):
            with self.subTest(out=out):
                run = self.run_wrapper("add-member", "team-a", f"--out={out}")
                self.assertEqual(run.returncode, 64)
                self.assertIn("already exists", run.stderr)
                self.assertFalse(self.args_file.exists())
        after = target.stat()
        self.assertEqual((after.st_uid, after.st_gid, after.st_mode), (before.st_uid, before.st_gid, before.st_mode))
        self.assertEqual(list(target.iterdir()), [])
        self.assertFalse((self.tmp / "nowhere").exists())

    def test_rotate_token_refuses_a_symlinked_or_foreign_out(self) -> None:
        self.out.mkdir(parents=True)
        (self.out / "member.json").write_text("{}")
        link = self.tmp / "link"
        link.symlink_to(self.out)
        cases = [link]
        if ROOT:
            foreign = self.tmp / "foreign"
            foreign.mkdir()
            (foreign / "member.json").write_text("{}")
            os.chown(foreign, OWNER + 1, GROUP)
            cases.append(foreign)
        for out in cases:
            with self.subTest(out=out):
                run = self.run_wrapper("rotate-token", "team-a", "h.1+x.podman", f"--out={out}")
                self.assertEqual(run.returncode, 64)
                self.assertFalse(self.args_file.exists())
        self.assertFalse((self.out / "token").exists())

    def test_rotate_token_leaves_the_directory_alone(self) -> None:
        self.make_out(0o750)
        self.tar_file.write_bytes(make_tar({"token": b"syt_new"}))
        run = self.run_wrapper("rotate-token", "team-a", "h.1+x.podman", f"--out={self.out}")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(stat.S_IMODE(self.out.stat().st_mode), 0o750)

    def test_rotate_token_replaces_only_the_token(self) -> None:
        self.make_out()
        (self.out / "token").write_text("old")
        if ROOT:
            os.chown(self.out / "token", OWNER, GROUP)
        self.tar_file.write_bytes(make_tar({"token": b"syt_new"}))
        run = self.run_wrapper("rotate-token", "team-a", "h.1+x.podman", f"--out={self.out}")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual((self.out / "token").read_text(), "syt_new")
        self.assertEqual((self.out / "member.json").read_text(), "{}")
        self.assertEqual(stat.S_IMODE((self.out / "token").stat().st_mode), 0o600)

    def test_rotate_token_needs_an_existing_bundle(self) -> None:
        run = self.run_wrapper("rotate-token", "team-a", "h.1+x.podman", f"--out={self.out}")
        self.assertEqual(run.returncode, 64)
        self.assertFalse(self.args_file.exists())

    def test_out_refusals(self) -> None:
        for args in (["add-member", "team-a"], ["add-member", "team-a", "--out=relative/dir"],
                     ["add-member", "team-a", "--out", str(self.out)],
                     ["add-member", "team-a", f"--out={self.out}", f"--out={self.out}"]):
            with self.subTest(args=args):
                run = self.run_wrapper(*args)
                self.assertEqual(run.returncode, 64)
                self.assertFalse(self.args_file.exists())

    def test_needs_sudo_for_the_owner(self) -> None:
        run = self.run_wrapper("add-member", "team-a", f"--out={self.out}", sudo=False)
        self.assertEqual(run.returncode, 64)
        self.assertIn("sudo", run.stderr)

    def test_other_commands_pass_through(self) -> None:
        run = self.run_wrapper("list", "team-a", stub='run_admin_tool() { printf "%s\\n" "$@"; }')
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(run.stdout, "list\nteam-a\n")

    def test_park_member_passes_through_and_places_nothing(self) -> None:
        run = self.run_wrapper("park-member", "team-a", "myrepo.dev+local.podman",
                               stub='run_admin_tool() { printf "%s\\n" "$@"; }')
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(run.stdout, "park-member\nteam-a\nmyrepo.dev+local.podman\n")

    def test_add_member_passes_the_seat_to_the_tool(self) -> None:
        run = self.run_wrapper("add-member", "team-a", "--seat=dev", f"--out={self.out}")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(self.tool_args(), ["add-member", "team-a", "--seat=dev"])

    def test_usage_lines_name_the_seat_commands(self) -> None:
        usage = [line for line in WRAPPER.read_text().splitlines() if line.startswith("#   sudo agent-bus ")]
        self.assertTrue(any("add-member TEAM" in line and "[--seat=S]" in line for line in usage), usage)
        self.assertIn("#   sudo agent-bus park-member TEAM HANDLE", usage)

    def test_refuses_without_root(self) -> None:
        script = 'source "$1"; shift; main "$@"'
        wrapper, drop = WRAPPER, []
        if ROOT:
            setpriv = shutil.which("setpriv")
            self.assertIsNotNone(setpriv)
            drop = [setpriv, "--reuid=65534", "--regid=65534", "--clear-groups"]
            # The checkout may not be readable by nobody; a world-readable copy is.
            public = tempfile.mkdtemp()
            self.addCleanup(shutil.rmtree, public)
            os.chmod(public, 0o755)
            wrapper = pathlib.Path(public) / "agent-bus"
            shutil.copy(WRAPPER, wrapper)
            os.chmod(wrapper, 0o644)
        run = subprocess.run([*drop, "bash", "-c", script, "bash", str(wrapper), "list", "team-a"],
                             capture_output=True, text=True, check=False, cwd="/")
        self.assertEqual(run.returncode, 77)
        self.assertIn("root", run.stderr)

    def run_unprivileged(self, *args: str, stub: str = "") -> subprocess.CompletedProcess:
        """The wrapper as an ordinary user: as root, dropped to nobody (a world-readable copy
        of the wrapper, since the checkout may not be readable by nobody)."""
        wrapper, drop = WRAPPER, []
        if ROOT:
            setpriv = shutil.which("setpriv")
            self.assertIsNotNone(setpriv)
            drop = [setpriv, "--reuid=65534", "--regid=65534", "--clear-groups"]
            public = tempfile.mkdtemp()
            self.addCleanup(shutil.rmtree, public)
            os.chmod(public, 0o755)
            wrapper = pathlib.Path(public) / "agent-bus"
            shutil.copy(WRAPPER, wrapper)
            os.chmod(wrapper, 0o644)
        script = f'source "$1"; {stub}; shift; main "$@"'
        return subprocess.run([*drop, "bash", "-c", script, "bash", str(wrapper), *args],
                              capture_output=True, text=True, check=False, cwd="/")

    def test_seat_commands_run_as_the_caller_in_place(self) -> None:
        stub = 'run_user_tool() { printf "%s\\n" "$PWD" "$@"; }'
        for args in (["seat", "list"], ["seat", "take", "dev1@team-a", "--no-prompt"]):
            with self.subTest(args=args):
                run = self.run_unprivileged(*args, stub=stub)
                self.assertEqual(run.returncode, 0, run.stderr)
                self.assertEqual(run.stdout.splitlines(), ["/", *args])

    def test_seat_commands_are_refused_as_root(self) -> None:
        stub = 'run_user_tool() { echo ran; }; run_admin_tool() { echo ran; }'
        run = self.run_wrapper("seat", "take", "dev1@team-a", stub=stub)
        if ROOT:
            self.assertEqual(run.returncode, 77)
            self.assertIn("not through sudo", run.stderr)
            self.assertEqual(run.stdout, "")
        else:
            self.assertEqual(run.stdout, "ran\n")

    def test_the_user_tool_runs_the_zipapp_without_a_privilege_change(self) -> None:
        text = WRAPPER.read_text()
        body = text[text.index("run_user_tool() {"):]
        body = body[:body.index("\n}\n")]
        self.assertIn('/usr/bin/python3 -I "$AGENT_BUS_PYZ" "$@"', body)
        self.assertNotIn("runuser", body)
        self.assertNotIn("cd ", body)

    def test_usage_lines_name_the_checkout_seat_commands(self) -> None:
        usage = [line for line in WRAPPER.read_text().splitlines() if line.startswith("#   agent-bus seat ")]
        self.assertEqual([line.split()[3] for line in usage], ["check", "take", "list", "remove"])

    def test_shellcheck(self) -> None:
        run = subprocess.run(["shellcheck", "-x", str(WRAPPER)], capture_output=True, text=True, check=False)
        self.assertEqual(run.returncode, 0, run.stdout)

    def test_real_drop_is_runuser_to_agent_bus(self) -> None:
        text = WRAPPER.read_text()
        self.assertIn('runuser -u "$AGENT_BUS_USER" --', text)
        self.assertIn("readonly AGENT_BUS_USER=agent-bus", text)
        self.assertIn("readonly AGENT_BUS_PYZ=/usr/local/lib/agent-bus/agent-bus.pyz", text)


class RealBundleTest(AdminTestCase):
    """The admin tool's own tar, placed by the wrapper, is a bundle pingbus accepts."""

    def test_placed_bundle_loads(self) -> None:
        self.bootstrap()
        bundle = admin.add_member(self.ctx(), self.transport, repo="myrepo", host="workstation",
                                  type_="podman", role="worker", address="192.0.2.10", human_text=False)
        tar_file = self.root / "bundle.tar"
        tar_file.write_bytes(bundle.tar)
        home = owner_home(self.root) / "pingbus"
        env = {"PATH": os.environ["PATH"], "ARGS_FILE": str(self.root / "args"), "TAR_FILE": str(tar_file),
               "SUDO_UID": str(OWNER), "SUDO_GID": str(GROUP)}
        script = f'source "$1"; {STUB}; shift; main "$@"'
        run = subprocess.run(["bash", "-c", script, "bash", str(WRAPPER), "add-member", "team-a",
                              f"--out={home / 'team-a'}"], capture_output=True, text=True, env=env, check=False)
        self.assertEqual(run.returncode, 0, run.stderr)
        member = config.load_bundle(home, "team-a", uid=OWNER)
        self.assertEqual(member.handle, bundle.handle)
        self.assertFalse(member.human_text)


if __name__ == "__main__":
    unittest.main()

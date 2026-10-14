"""Unit tests for helpers/agent_bus_lxc/derive.py: the `<repo>`, `<host>` and `<seat>` of an
LXC member, derived from the checkout inside its container exactly as ccy derives them for a
podman seat.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus_lxc.test_derive

Spec: docs/agent-bus.md ("A seat's handle is ..."), Plan 00161 DESIGN.md section 5.2 and D49
(`<repo>` from the forge remote, `<host>` from `ccy.env.local`, else `local`), and
`checkout.seat_choices` (a new seat is the lowest number no active member of the same
`<repo>+<host>.<type>` holds; a parked one returns). The container is read from the host as
plain files: the host-side script runs here, as the test's own user, against a fake LXC tree
in a temporary directory; no git is run, no link is followed, every size is capped.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.agent_bus_lxc import derive

REMOTE = "git@github.com:Example-Org/Proj.git"
CONTAINER = "devbox"
USER = "agent"
CHECKOUT = f"/home/{USER}/work"


def run_script(*args: str, search_path: str | None = None) -> subprocess.CompletedProcess:
    environment = dict(os.environ)
    if search_path is not None:
        environment["PATH"] = search_path
    return subprocess.run([sys.executable, "-I", "-c", derive.ROOT_READ_PY, *args], capture_output=True,
                          text=True, check=False, env=environment)


class FakeLxc:
    """A rootful container's tree on this machine: <lxcpath>/devbox/config and its rootfs. The
    script runs as the test's own user, who owns everything, so the user's uid is ours."""

    def __init__(self, root: pathlib.Path) -> None:
        self.lxcpath = root / "lxc"
        self.rootfs = root / "lxc" / CONTAINER / "rootfs"
        self.outside = root / "outside"
        self.outside.mkdir()
        self.config_lines = [f"lxc.rootfs.path = dir:{self.rootfs}"]
        (self.rootfs / "etc").mkdir(parents=True)
        (self.rootfs / "etc" / "passwd").write_text(
            f"root:x:0:0::/root:/bin/sh\n{USER}:x:{os.getuid()}:{os.getgid()}::/home/{USER}:/bin/bash\n",
            encoding="utf-8")
        self.checkout = self.rootfs / "home" / USER / "work"
        (self.checkout / ".git").mkdir(parents=True)
        self.write_config()

    def write_config(self) -> None:
        (self.lxcpath / CONTAINER / "config").write_text("\n".join(self.config_lines) + "\n", encoding="utf-8")

    def git_config(self, text: str) -> None:
        (self.checkout / ".git" / "config").write_text(text, encoding="utf-8")

    def remote(self, url: str = REMOTE) -> None:
        self.git_config(f'[core]\n\tbare = false\n[remote "origin"]\n\turl = {url}\n'
                        "\tfetch = +refs/heads/*:refs/remotes/origin/*\n")

    def env_local(self, text: str) -> pathlib.Path:
        path = self.checkout / ".claude" / "ccy" / "ccy.env.local"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        return path

    def probe(self, path: str = CHECKOUT, *, user: str = USER, search_path: str | None = None) -> subprocess.CompletedProcess:
        return run_script("probe", str(self.lxcpath), CONTAINER, user, path, search_path=search_path)

    def checkouts(self) -> subprocess.CompletedProcess:
        return run_script("checkouts", str(self.lxcpath), CONTAINER, USER, "-")


class ProbeScriptTest(unittest.TestCase):
    """What runs on the host, as root, against the container's tree."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self._tmp.name)
        self.lxc = FakeLxc(self.root)
        self.lxc.remote()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def probed(self) -> derive.Probe:
        run = self.lxc.probe()
        self.assertEqual(run.returncode, 0, run.stderr)
        return derive.parse_probe(run.stdout)

    def refused(self, why: str) -> None:
        run = self.lxc.probe()
        self.assertNotEqual(run.returncode, 0, run.stdout)
        self.assertIn(why, run.stderr)

    def test_no_file_gives_the_local_host(self) -> None:
        probe = self.probed()
        self.assertEqual((probe.top, probe.origin, probe.env_local), (CHECKOUT, REMOTE, None))
        self.assertEqual(derive.identity_of(probe), derive.Identity("proj", "local"))

    def test_remote_and_role_are_read(self) -> None:
        self.lxc.env_local("export HOOKS_DAEMON_HOSTNAME=Build.Box\n")
        self.assertEqual(derive.identity_of(self.probed()), derive.Identity("proj", "build-box"))

    def test_no_remote_gives_the_directory_name(self) -> None:
        self.lxc.git_config("[core]\n\tbare = false\n")
        self.assertEqual(derive.identity_of(self.probed()), derive.Identity("work", "local"))

    def test_the_role_is_read_never_run(self) -> None:
        marker = self.root / "ran"
        self.lxc.env_local(f"touch {marker}\nexport HOOKS_DAEMON_HOSTNAME=up\n")
        self.assertEqual(derive.identity_of(self.probed()).host, "up")
        self.assertFalse(marker.exists())

    def test_git_is_never_run_so_a_repository_config_cannot_execute(self) -> None:
        # A git on PATH that records being run, and a config that would make git run a program.
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        ran = self.root / "git-ran"
        fake = bin_dir / "git"
        fake.write_text(f"#!/bin/sh\ntouch {ran}\n", encoding="utf-8")
        fake.chmod(0o755)
        fsmonitor = self.root / "fsmonitor-ran"
        self.lxc.git_config(f'[core]\n\tfsmonitor = touch {fsmonitor}\n[remote "origin"]\n\turl = {REMOTE}\n')
        run = self.lxc.probe(search_path=f"{bin_dir}:{os.environ['PATH']}")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertFalse(ran.exists(), "git was run")
        self.assertFalse(fsmonitor.exists(), "core.fsmonitor was executed")
        self.assertEqual(derive.parse_probe(run.stdout).origin, REMOTE)

    def test_a_symlinked_git_config_is_refused(self) -> None:
        outside = self.lxc.outside / "secret"
        outside.write_text('[remote "origin"]\n\turl = https://example.com/leak/secret.git\n', encoding="utf-8")
        (self.lxc.checkout / ".git" / "config").unlink()
        (self.lxc.checkout / ".git" / "config").symlink_to(outside)
        self.refused("symbolic link")

    def test_a_symlinked_directory_in_the_path_is_refused(self) -> None:
        (self.lxc.rootfs / "home" / USER / "work").rename(self.lxc.outside / "work")
        (self.lxc.rootfs / "home" / USER / "work").symlink_to(self.lxc.outside / "work")
        self.refused("symbolic link")

    def test_a_symlinked_env_file_is_refused(self) -> None:
        real = self.lxc.outside / "env"
        real.write_text("export HOOKS_DAEMON_HOSTNAME=x\n", encoding="utf-8")
        (self.lxc.checkout / ".claude" / "ccy").mkdir(parents=True)
        (self.lxc.checkout / ".claude" / "ccy" / "ccy.env.local").symlink_to(real)
        self.refused("symbolic link")

    def test_a_symlinked_dot_git_is_refused(self) -> None:
        (self.lxc.checkout / ".git" / "config").rename(self.lxc.outside / "config")
        (self.lxc.checkout / ".git").rmdir()
        (self.lxc.checkout / ".git").symlink_to(self.lxc.outside)
        self.refused("symbolic link")

    def test_an_oversized_git_config_is_refused(self) -> None:
        self.lxc.git_config("# " + "x" * derive.FILE_MAX_BYTES + "\n")
        self.refused("larger than")

    def test_an_oversized_env_file_is_refused(self) -> None:
        self.lxc.env_local("# " + "x" * derive.FILE_MAX_BYTES + "\n")
        self.refused("larger than")

    def test_a_file_that_is_not_utf8_is_refused(self) -> None:
        (self.lxc.checkout / ".git" / "config").write_bytes(b"\xff\xfe")
        self.refused("UTF-8")

    def test_a_fifo_is_refused_without_being_opened(self) -> None:
        (self.lxc.checkout / ".git" / "config").unlink()
        os.mkfifo(self.lxc.checkout / ".git" / "config")
        self.refused("not a regular file")

    def test_a_directory_in_place_of_the_env_file_is_refused(self) -> None:
        (self.lxc.checkout / ".claude" / "ccy" / "ccy.env.local").mkdir(parents=True)
        self.refused("not a regular file")

    def test_a_worktree_gitfile_is_refused(self) -> None:
        (self.lxc.checkout / ".git" / "config").unlink()
        (self.lxc.checkout / ".git").rmdir()
        (self.lxc.checkout / ".git").write_text("gitdir: /elsewhere\n", encoding="utf-8")
        self.refused("not a directory")

    def test_a_directory_that_is_not_a_checkout_is_refused(self) -> None:
        (self.lxc.checkout / ".git" / "config").unlink()
        self.refused("not a git checkout")

    def test_a_missing_checkout_is_refused(self) -> None:
        run = self.lxc.probe(f"/home/{USER}/nowhere")
        self.assertNotEqual(run.returncode, 0)
        self.assertIn("does not exist", run.stderr)

    def test_a_path_with_dots_is_refused(self) -> None:
        run = self.lxc.probe(f"/home/{USER}/../../etc")
        self.assertIn("not a clean path", run.stderr)

    def test_a_user_the_container_lacks_is_refused(self) -> None:
        run = self.lxc.probe(user="nobody2")
        self.assertIn("has no user nobody2", run.stderr)

    def test_an_unprivileged_container_is_refused(self) -> None:
        self.lxc.config_lines.append("lxc.idmap = u 0 100000 65536")
        self.lxc.write_config()
        self.refused("unprivileged")

    def test_a_rootfs_that_is_not_a_directory_is_refused(self) -> None:
        self.lxc.config_lines = ["lxc.rootfs.path = overlayfs:/a:/b"]
        self.lxc.write_config()
        self.refused("not a directory")

    def test_a_bind_mounted_checkout_is_read_from_its_source(self) -> None:
        # The checkout lives on a desktop path the config binds in, not in the rootfs.
        source = self.lxc.outside / "projects"
        (source / "app" / ".git").mkdir(parents=True)
        (source / "app" / ".git" / "config").write_text(
            f'[remote "origin"]\n\turl = {REMOTE.replace("Proj", "App")}\n', encoding="utf-8")
        self.lxc.config_lines.append(f"lxc.mount.entry = {source} home/{USER}/projects none bind,create=dir 0 0")
        self.lxc.write_config()
        run = self.lxc.probe(f"/home/{USER}/projects/app")
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertEqual(derive.identity_of(derive.parse_probe(run.stdout)).repo, "app")

    def test_a_link_inside_a_bind_mounted_tree_is_refused(self) -> None:
        source = self.lxc.outside / "projects"
        (source / "app").mkdir(parents=True)
        (source / "app" / ".git").symlink_to(self.lxc.outside)
        self.lxc.config_lines.append(f"lxc.mount.entry = {source} home/{USER}/projects none bind 0 0")
        self.lxc.write_config()
        run = self.lxc.probe(f"/home/{USER}/projects/app")
        self.assertIn("symbolic link", run.stderr)

    def test_a_mount_that_is_not_a_bind_is_not_followed(self) -> None:
        self.lxc.config_lines.append(f"lxc.mount.entry = tmpfs home/{USER}/work tmpfs defaults 0 0")
        self.lxc.write_config()
        self.assertEqual(self.lxc.probe().returncode, 0)


class ParseProbeTest(unittest.TestCase):
    """Everything the container's files held is hostile."""

    GOOD = {"path": CHECKOUT, "uid": 1000, "git_config": '[remote "origin"]\n\turl = ' + REMOTE + "\n",
            "env_uid": None, "env_local": None}

    def parse(self, **changes: object) -> derive.Probe:
        return derive.parse_probe(json.dumps({**self.GOOD, **changes}))

    def test_a_good_probe(self) -> None:
        self.assertEqual(self.parse().origin, REMOTE)

    def test_refusals(self) -> None:
        cases = {
            "not json": "{",
            "not an object": "[]",
            "a missing key": json.dumps({"path": "/x"}),
            "an extra key": json.dumps({**self.GOOD, "more": 1}),
            "a relative path": json.dumps({**self.GOOD, "path": "x"}),
            "a control character in the path": json.dumps({**self.GOOD, "path": "/x\x1b[31m"}),
            "a config that is not text": json.dumps({**self.GOOD, "git_config": 5}),
            "no config": json.dumps({**self.GOOD, "git_config": None}),
            "a control character in the url": json.dumps({**self.GOOD, "git_config": '[remote "origin"]\n\turl = a\x1bb\n'}),
            "a file too large": json.dumps({**self.GOOD, "env_uid": 1000,
                                            "env_local": "x" * (derive.FILE_MAX_BYTES + 1)}),
            "a file with no owner": json.dumps({**self.GOOD, "env_local": "x"}),
            "an owner that is another user": json.dumps({**self.GOOD, "env_uid": 0, "env_local": "x"}),
            "a quoted url": json.dumps({**self.GOOD, "git_config": '[remote "origin"]\n\turl = "a b"\n'}),
        }
        for label, text in cases.items():
            with self.subTest(label), self.assertRaises(derive.DeriveError):
                derive.parse_probe(text)

    def test_a_remote_naming_nothing_usable_is_refused_when_the_identity_is_made(self) -> None:
        probe = self.parse(git_config='[remote "origin"]\n\turl = https://github.com/o/%%%.git\n')
        with self.assertRaises(derive.DeriveError):
            derive.identity_of(probe)

    def test_an_assigned_role_that_fits_no_host_is_refused(self) -> None:
        with self.assertRaises(derive.DeriveError):
            derive.identity_of(self.parse(env_uid=1000, env_local="export HOOKS_DAEMON_HOSTNAME=-x\n"))


class OriginTest(unittest.TestCase):
    def test_only_the_origin_section_counts(self) -> None:
        text = ('[remote "upstream"]\n\turl = https://h/o/up.git\n[remote "origin"]\n\tfetch = x\n'
                "\turl = https://h/o/mine.git\n\turl = https://h/o/second.git\n")
        self.assertEqual(derive.origin_from_git_config(text), "https://h/o/mine.git")

    def test_no_origin_is_none(self) -> None:
        self.assertIsNone(derive.origin_from_git_config('[remote "upstream"]\n\turl = https://h/o/up.git\n'))
        self.assertIsNone(derive.origin_from_git_config(""))

    def test_a_trailing_comment_and_the_key_in_capitals(self) -> None:
        text = '[remote "origin"] # mine\n\tURL = https://h/o/r.git # note\n'
        self.assertEqual(derive.origin_from_git_config(text), "https://h/o/r.git")

    def test_a_backslash_is_refused(self) -> None:
        with self.assertRaises(derive.DeriveError):
            derive.origin_from_git_config('[remote "origin"]\n\turl = https://h/o/r\\\n')


class SeatTest(unittest.TestCase):
    IDENTITY = derive.Identity("proj", "box")

    def seat(self, roster: dict[str, bool] | None = None, *, container: str = "devbox",
             bundle: str | None = None, taken: tuple[str, ...] = ()) -> str:
        return derive.derive_seat(self.IDENTITY, container, roster or {}, bundle, set(taken))

    def test_the_first_seat_is_one(self) -> None:
        self.assertEqual(self.seat(), "1")

    def test_the_lowest_number_no_active_member_holds(self) -> None:
        roster = {"proj.1+box.lxc": False, "proj.3+box.lxc": False}
        self.assertEqual(self.seat(roster), "2")

    def test_a_parked_seat_returns(self) -> None:
        self.assertEqual(self.seat({"proj.1+box.lxc": True, "proj.2+box.lxc": False}), "1")

    def test_another_repo_host_or_type_does_not_count(self) -> None:
        roster = {"other.1+box.lxc": False, "proj.1+desk.lxc": False, "proj.1+box.podman": False}
        self.assertEqual(self.seat(roster), "1")

    def test_a_seat_another_entry_has_taken_is_not_offered(self) -> None:
        self.assertEqual(self.seat(taken=("1", "2")), "3")

    def test_a_bundle_of_the_same_prefix_keeps_its_seat(self) -> None:
        self.assertEqual(self.seat({"proj.5+box.lxc": False}, bundle="proj.5+box.lxc"), "5")
        self.assertEqual(self.seat({"proj.devbox+box.lxc": False}, bundle="proj.devbox+box.lxc"), "devbox")

    def test_a_bundle_of_another_prefix_is_not_its_seat(self) -> None:
        self.assertEqual(self.seat(bundle="proj.5+desk.lxc"), "1")

    def test_a_bundle_that_names_no_handle_is_not_its_seat(self) -> None:
        self.assertEqual(self.seat(bundle="garbage"), "1")

    def test_a_container_already_joined_by_its_name_keeps_that_seat(self) -> None:
        roster = {"proj.devbox1+box.lxc": True}
        self.assertEqual(self.seat(roster, container="Dev-Box_1"), "devbox1")

    def test_a_container_name_is_not_a_seat_when_nobody_joined_by_it(self) -> None:
        self.assertEqual(self.seat(container="Dev-Box_1"), "1")


class CheckoutsScriptTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.lxc = FakeLxc(pathlib.Path(self._tmp.name))
        self.home = self.lxc.rootfs / "home" / USER
        (self.lxc.checkout / ".git").rmdir()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def repo(self, relative: str, remote: str | None) -> None:
        path = self.home / relative
        (path / ".git").mkdir(parents=True)
        (path / ".git" / "config").write_text(
            "" if remote is None else f'[remote "origin"]\n\turl = {remote}\n', encoding="utf-8")

    def found(self) -> dict[str, str | None]:
        run = self.lxc.checkouts()
        self.assertEqual(run.returncode, 0, run.stderr)
        return dict(derive.parse_checkouts(run.stdout))

    def test_checkouts_in_the_home_are_found_with_their_origin_and_the_container_path(self) -> None:
        self.repo("proj", REMOTE)
        self.repo("src/deep/tool", "https://github.com/example-org/tool.git")
        self.repo("noremote", None)
        self.assertEqual(self.found(), {f"/home/{USER}/proj": REMOTE,
                                        f"/home/{USER}/src/deep/tool": "https://github.com/example-org/tool.git",
                                        f"/home/{USER}/noremote": None})

    def test_a_symlinked_directory_is_not_followed(self) -> None:
        outside = self.lxc.outside / "elsewhere"
        (outside / ".git").mkdir(parents=True)
        (self.home / "link").symlink_to(outside)
        self.assertEqual(self.found(), {})

    def test_a_checkout_below_a_checkout_is_not_listed(self) -> None:
        self.repo("proj", REMOTE)
        self.repo("proj/vendor/inner", REMOTE)
        self.assertEqual(list(self.found()), [f"/home/{USER}/proj"])

    def test_hidden_directories_are_skipped(self) -> None:
        self.repo(".cache/thing", REMOTE)
        self.assertEqual(self.found(), {})

    def test_the_search_is_bounded(self) -> None:
        for number in range(derive.CHECKOUTS_MAX + 5):
            self.repo(f"r{number:03d}", REMOTE)
        self.assertEqual(len(self.found()), derive.CHECKOUTS_MAX)

    def test_a_symlinked_git_config_is_refused(self) -> None:
        secret = self.lxc.outside / "secret"
        secret.write_text("x", encoding="utf-8")
        self.repo("proj", None)
        (self.home / "proj" / ".git" / "config").unlink()
        (self.home / "proj" / ".git" / "config").symlink_to(secret)
        run = self.lxc.checkouts()
        self.assertNotEqual(run.returncode, 0)
        self.assertIn("symbolic link", run.stderr)

    def test_what_the_script_printed_is_validated(self) -> None:
        good = json.dumps([{"path": "/home/a/p", "git_config": '[remote "origin"]\n\turl = ' + REMOTE + "\n"}])
        self.assertEqual(derive.parse_checkouts(good), [("/home/a/p", REMOTE)])
        cases = {"not json": "[", "not a list": "{}",
                 "a relative path": json.dumps([{"path": "p", "git_config": None}]),
                 "a control character": json.dumps([{"path": "/p\x07", "git_config": None}]),
                 "an extra key": json.dumps([{"path": "/p", "git_config": None, "x": 1}]),
                 "too many": json.dumps([{"path": f"/p{n}", "git_config": None}
                                         for n in range(derive.CHECKOUTS_MAX + 1)])}
        for label, text in cases.items():
            with self.subTest(label), self.assertRaises(derive.DeriveError):
                derive.parse_checkouts(text)


if __name__ == "__main__":
    unittest.main()

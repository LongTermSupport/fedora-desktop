"""Tests for U23's kit copy (README step 1), _acceptance-u23.inc.bash's U23_KIT_UNPACK_PY.

The run that found it: the owner's shell had umask 077, sudo and lxc-attach carried it into
the LXC member, tarfile's "data" filter made the kit's directories with it, and the agent
user could not run pingbus (`env: 'pingbus': Permission denied`, exit 126). These tests run
the snippet as the include defines it, under umask 077, and require a kit others can run.

Run from anywhere:
    python3 CLAUDE/Plan/00161-agent-team-bus-matrix/test_u23_kit_unpack.py
"""

from __future__ import annotations

import io
import os
import pathlib
import stat
import subprocess
import sys
import tarfile
import tempfile
import unittest

_HERE = pathlib.Path(__file__).resolve().parent
_INC = _HERE / "_acceptance-u23.inc.bash"


def _unpack_script() -> str:
    """The snippet exactly as the include defines it, read by sourcing the include."""
    out = subprocess.run(
        ["bash", "-c", 'source "$1" && printf %s "$U23_KIT_UNPACK_PY"', "bash", str(_INC)],
        check=True, capture_output=True, text=True)
    if not out.stdout.strip():
        raise AssertionError(f"{_INC} defines no U23_KIT_UNPACK_PY")
    return out.stdout


def _kit_tar(members: dict[str, tuple[bytes | None, int]]) -> bytes:
    """A tar of kit/...: name -> (data, mode); data None is a directory."""
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as tar:
        for name, (data, mode) in members.items():
            info = tarfile.TarInfo(name)
            info.mode = mode
            if data is None:
                info.type = tarfile.DIRTYPE
                tar.addfile(info)
            else:
                info.size = len(data)
                tar.addfile(info, io.BytesIO(data))
    return buffer.getvalue()


def _mode(path: pathlib.Path) -> int:
    return stat.S_IMODE(path.stat().st_mode)


@unittest.skipUnless(hasattr(tarfile, "data_filter"),
                     "this Python's tarfile has no extraction filters (3.11.4 or later, as the"
                     " LXC member's Fedora python3 has); run these tests on the host")
class KitUnpackTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.parent = pathlib.Path(self._tmp.name) / "share" / "agent-bus"
        self.script = _unpack_script()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def _run(self, tar: bytes) -> subprocess.CompletedProcess[bytes]:
        def restrictive_umask() -> None:
            os.umask(0o077)

        return subprocess.run(
            [sys.executable, "-I", "-c", self.script, str(self.parent)],
            input=tar, capture_output=True, check=False, preexec_fn=restrictive_umask)

    def test_directories_are_0755_under_umask_077(self) -> None:
        tar = _kit_tar({
            "kit": (None, 0o755),
            "kit/pingbus": (b"#!/usr/bin/env python3\n", 0o755),
            "kit/agent-bus-claude": (b"#!/bin/sh\n", 0o755),
            "kit/README.lxc": (b"readme\n", 0o644),
            "kit/sub": (None, 0o755),
            "kit/sub/file": (b"x\n", 0o644),
        })
        result = self._run(tar)
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        for path in (self.parent, self.parent / "kit", self.parent / "kit" / "sub"):
            self.assertEqual(_mode(path), 0o755, f"{path} is {_mode(path):o}")
        self.assertEqual(_mode(self.parent / "kit" / "pingbus"), 0o755)
        self.assertEqual(_mode(self.parent / "kit" / "agent-bus-claude"), 0o755)
        self.assertEqual(_mode(self.parent / "kit" / "README.lxc"), 0o644)

    def test_a_tar_without_pingbus_fails(self) -> None:
        result = self._run(_kit_tar({"kit": (None, 0o755), "kit/README.lxc": (b"r\n", 0o644)}))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"made no", result.stderr)


if __name__ == "__main__":
    unittest.main()

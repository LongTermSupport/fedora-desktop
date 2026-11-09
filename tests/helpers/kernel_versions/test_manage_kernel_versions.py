"""Tests for files/usr/local/bin/manage-kernel-versions.py: the kernel NEVRA names the host's arch.

dnf versionlock names a kernel by its full NEVRA, whose last field is the CPU architecture, and
`uname -r` ends with it too. A lock written as `.x86_64` on an aarch64 host locks nothing, so the
kernel the script exists to keep is removed (Plan 00243, the arm64 sweep). The script is deployed
as a file, not a helper package, so it is loaded by path.
"""

from __future__ import annotations

import importlib.util
import platform
import sys
import unittest
from pathlib import Path
from types import ModuleType

SCRIPT = Path(__file__).resolve().parents[3] / "files" / "usr" / "local" / "bin" / "manage-kernel-versions.py"


def load_script() -> ModuleType:
    """Import the deployed script by path, registered first as its dataclass requires."""
    name = "manage_kernel_versions_under_test"
    spec = importlib.util.spec_from_file_location(name, SCRIPT)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {SCRIPT}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


mkv = load_script()


class Recorder:
    """A run_command stand-in: answers from a table and records every command it is given."""

    def __init__(self, replies: dict[str, tuple[int, str, str]]) -> None:
        self.replies = replies
        self.calls: list[list[str]] = []

    def __call__(self, cmd: list[str], check: bool = True) -> tuple[int, str, str]:
        self.calls.append(cmd)
        return self.replies.get(" ".join(cmd), (0, "", ""))


def manager(arch: str, replies: dict[str, tuple[int, str, str]]) -> tuple[object, Recorder]:
    km = mkv.KernelManager(arch=arch)
    recorder = Recorder(replies)
    km.run_command = recorder
    return km, recorder


KERNEL = "6.17.5-200.fc42"


class TestTheNevraNamesTheHostArchitecture(unittest.TestCase):
    def test_the_default_architecture_is_the_running_machine(self) -> None:
        self.assertEqual(mkv.KernelManager().arch, platform.machine())

    def test_a_lock_on_aarch64_names_the_aarch64_kernel(self) -> None:
        km, recorder = manager("aarch64", {})
        km.lock_kernel(mkv.KernelVersion.parse(KERNEL))
        self.assertIn(["dnf", "versionlock", "add", f"kernel-0:{KERNEL}.aarch64"], recorder.calls)

    def test_an_unlock_on_aarch64_names_the_aarch64_kernel(self) -> None:
        km, recorder = manager("aarch64", {"dnf versionlock list": (0, f"kernel-0:{KERNEL}.aarch64\n", "")})
        km.unlock_kernel(mkv.KernelVersion.parse(KERNEL))
        self.assertIn(["dnf", "versionlock", "delete", f"kernel-0:{KERNEL}.aarch64"], recorder.calls)

    def test_a_lock_on_x86_64_still_names_the_x86_64_kernel(self) -> None:
        km, recorder = manager("x86_64", {})
        km.lock_kernel(mkv.KernelVersion.parse(KERNEL))
        self.assertIn(["dnf", "versionlock", "add", f"kernel-0:{KERNEL}.x86_64"], recorder.calls)

    def test_the_running_kernel_loses_the_host_arch_suffix(self) -> None:
        km, _ = manager("aarch64", {"uname -r": (0, f"{KERNEL}.aarch64\n", "")})
        running = km.get_running_kernel()
        self.assertIsNotNone(running)
        self.assertEqual(running.full_version, KERNEL)


if __name__ == "__main__":
    unittest.main()

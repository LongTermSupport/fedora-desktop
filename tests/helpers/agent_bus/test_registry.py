"""Unit tests for helpers/agent_bus/registry.py: handles and the per-team registry.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_registry

`registry.json` (`/var/lib/agent-bus/<team>/registry.json`) holds each member handle
with its role, the parked handles, and the counter of every `<repo>+<host>.<type>`
prefix, so a counter-issued seat number is never reused (docs/agent-bus-protocol.md §3,
Plan 00161's DESIGN.md sections 3.4 and 5.5).
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import registry
from helpers.pingbus import protocol


class NormaliseRepoTest(unittest.TestCase):
    CASES = {
        "https://github.com/example-org/myrepo.git": "myrepo",
        "https://github.com/example-org/myrepo": "myrepo",
        "https://github.com/example-org/myrepo/": "myrepo",
        "git@github.com:example-org/My.Repo.git": "my-repo",
        "git@example.com:myrepo.git": "myrepo",
        "ssh://git@example.com/org/_my_repo.git": "my_repo",
        "https://example.com/org/--Fancy Repo!": "fancy-repo-",
        "myrepo": "myrepo",
    }

    def test_remote_urls(self) -> None:
        for remote, expected in self.CASES.items():
            with self.subTest(remote):
                self.assertEqual(registry.repo_from_remote(remote, "/srv/checkout-dir"), expected)

    def test_no_remote_uses_checkout_directory(self) -> None:
        self.assertEqual(registry.repo_from_remote(None, "/srv/work/Some_Checkout"), "some_checkout")
        self.assertEqual(registry.repo_from_remote("", "/srv/work/checkout/"), "checkout")

    def test_truncated_to_48(self) -> None:
        self.assertEqual(registry.normalise_repo("a" * 60), "a" * 48)

    def test_empty_after_normalising_refused(self) -> None:
        for raw in ("", "---", "__", "!!!"):
            with self.subTest(raw), self.assertRaises(registry.HandleError):
                registry.normalise_repo(raw)
        with self.assertRaises(registry.HandleError):
            registry.repo_from_remote("https://example.com/org/.git", "/srv/checkout")
        with self.assertRaises(registry.HandleError):
            registry.repo_from_remote(None, "/")


class ResolveHostTest(unittest.TestCase):
    def test_explicit_host_wins_over_role(self) -> None:
        self.assertEqual(registry.resolve_host("Build-Box", "workstation"), "build-box")

    def test_role_used_when_no_explicit_host(self) -> None:
        self.assertEqual(registry.resolve_host(None, "workstation"), "workstation")

    def test_host_normalised(self) -> None:
        self.assertEqual(registry.resolve_host("Work Station.1", None), "work-station-1")

    def test_host_required_when_no_role(self) -> None:
        for explicit, role in ((None, None), ("", ""), (None, "")):
            with self.subTest((explicit, role)), self.assertRaises(registry.HandleError) as caught:
                registry.resolve_host(explicit, role)
            self.assertIn("--host", str(caught.exception))

    def test_host_not_fitting_the_grammar_refused(self) -> None:
        for raw in ("-lead", "trail-", "x" * 64, "___"):
            with self.subTest(raw), self.assertRaises(registry.HandleError):
                registry.resolve_host(raw, None)


class BuildHandleTest(unittest.TestCase):
    def test_build(self) -> None:
        self.assertEqual(registry.build_handle("myrepo", 3, "workstation", "podman"), "myrepo.3+workstation.podman")

    def test_build_with_a_seat(self) -> None:
        self.assertEqual(registry.build_handle("myrepo", "dev1", "local", "podman"), "myrepo.dev1+local.podman")
        self.assertEqual(registry.build_handle("myrepo", "4", "local", "podman"), "myrepo.4+local.podman")

    def test_build_refuses_bad_parts(self) -> None:
        for args in (
            ("MyRepo", 1, "ws", "podman"),
            ("myrepo", 0, "ws", "podman"),
            ("myrepo", 1_000_000, "ws", "podman"),
            ("myrepo", 1, "ws", "kvm"),
            ("myrepo", 1, "Ws", "podman"),
            ("myrepo", True, "ws", "podman"),
            ("myrepo", "0", "ws", "podman"),
            ("myrepo", "01", "ws", "podman"),
            ("myrepo", "dev-1", "ws", "podman"),
            ("myrepo", "dev_1", "ws", "podman"),
            ("myrepo", "dev.1", "ws", "podman"),
            ("myrepo", "Dev", "ws", "podman"),
            ("myrepo", "", "ws", "podman"),
            ("myrepo", None, "ws", "podman"),
        ):
            with self.subTest(args), self.assertRaises(registry.HandleError):
                registry.build_handle(*args)

    def test_grammar_is_protocols(self) -> None:
        # The handle grammar has one home, helpers/pingbus/protocol.py; probe H4 may change
        # its separator, and the registry must follow without an edit of its own.
        self.assertIs(registry.HANDLE_SEP, protocol.HANDLE_SEP)
        self.assertIs(registry.TYPES, protocol.HANDLE_TYPES)
        self.assertIs(registry.ROLES, protocol.ROLES)
        self.assertFalse(hasattr(registry, "HANDLE_PATTERN"))
        for type_ in protocol.HANDLE_TYPES:
            handle = registry.build_handle("a_b", 999_999, "h-1", type_)
            parsed = protocol.parse_handle(handle)
            self.assertEqual((parsed.repo, parsed.n, parsed.host, parsed.type), ("a_b", 999_999, "h-1", type_))

    def test_prefix_of_handle(self) -> None:
        # The counter key: "seat" now means only `<seat>` (DESIGN.md section 5.5).
        self.assertEqual(registry.prefix_of("myrepo.12+ws.lxc"), "myrepo+ws.lxc")
        self.assertEqual(registry.prefix_of("myrepo.dev+local.podman"), "myrepo+local.podman")
        with self.assertRaises(registry.HandleError):
            registry.prefix_of("alice")
        self.assertFalse(hasattr(registry, "seat_of"))
        self.assertFalse(hasattr(registry, "SEAT_PATTERN"))


class RegistryLogicTest(unittest.TestCase):
    def test_empty(self) -> None:
        reg = registry.Registry.empty("alpha")
        self.assertEqual((reg.team, dict(reg.counters), dict(reg.members), reg.parked),
                         ("alpha", {}, {}, frozenset()))

    def test_add_member_with_a_named_seat(self) -> None:
        reg, handle = registry.Registry.empty("alpha").add_member("myrepo", "local", "podman", "worker", seat="dev1")
        self.assertEqual(handle, "myrepo.dev1+local.podman")
        self.assertEqual(dict(reg.members), {handle: "worker"})
        self.assertEqual(dict(reg.counters), {})

    def test_counter_skips_a_number_issued_with_seat(self) -> None:
        reg = registry.Registry.empty("alpha")
        reg, h1 = reg.add_member("myrepo", "ws", "podman", "worker")
        reg, h3 = reg.add_member("myrepo", "ws", "podman", "worker", seat="3")
        reg, h4 = reg.add_member("myrepo", "ws", "podman", "worker")
        self.assertEqual((h1, h3, h4), ("myrepo.1+ws.podman", "myrepo.3+ws.podman", "myrepo.4+ws.podman"))
        self.assertEqual(dict(reg.counters), {"myrepo+ws.podman": 4})
        # A number below the counter, never issued, may still be named; the counter stays.
        reg, h2 = reg.add_member("myrepo", "ws", "podman", "worker", seat="2")
        self.assertEqual(h2, "myrepo.2+ws.podman")
        self.assertEqual(dict(reg.counters), {"myrepo+ws.podman": 4})

    def test_add_member_refuses_a_current_or_parked_handle(self) -> None:
        reg, handle = registry.Registry.empty("alpha").add_member("myrepo", "local", "podman", "worker", seat="dev")
        with self.assertRaisesRegex(registry.RegistryError, "current member"):
            reg.add_member("myrepo", "local", "podman", "worker", seat="dev")
        parked = reg.park(handle)
        with self.assertRaisesRegex(registry.RegistryError, "parked"):
            parked.add_member("myrepo", "local", "podman", "worker", seat="dev")

    def test_add_member_refuses_a_bad_seat(self) -> None:
        for seat in ("dev-1", "0", "Dev", ""):
            with self.subTest(seat), self.assertRaises(registry.HandleError):
                registry.Registry.empty("alpha").add_member("myrepo", "local", "podman", "worker", seat=seat)

    def test_park_then_return(self) -> None:
        reg, handle = registry.Registry.empty("alpha").add_member("myrepo", "local", "podman", "worker", seat="dev")
        reg = reg.set_role(handle, "orchestrator")
        parked = reg.park(handle)
        self.assertEqual(parked.parked, frozenset({handle}))
        self.assertEqual(dict(parked.members), {handle: "orchestrator"})
        self.assertTrue(parked.is_parked(handle))
        returned = parked.unpark(handle)
        self.assertEqual(returned.parked, frozenset())
        self.assertEqual(dict(returned.members), {handle: "orchestrator"})
        self.assertEqual(returned, reg)

    def test_park_and_unpark_refusals(self) -> None:
        reg, handle = registry.Registry.empty("alpha").add_member("myrepo", "local", "podman", "worker", seat="dev")
        with self.assertRaises(registry.RegistryError):
            reg.park("myrepo.qa+local.podman")
        with self.assertRaises(registry.RegistryError):
            reg.unpark(handle)
        self.assertEqual(reg.park(handle).park(handle), reg.park(handle))

    def test_remove_a_parked_member(self) -> None:
        reg, handle = registry.Registry.empty("alpha").add_member("myrepo", "local", "podman", "worker", seat="dev")
        removed = reg.park(handle).remove_member(handle)
        self.assertEqual((dict(removed.members), removed.parked), ({}, frozenset()))

    def test_add_member_numbers_per_seat(self) -> None:
        reg = registry.Registry.empty("alpha")
        reg, h1 = reg.add_member("myrepo", "ws", "podman", "orchestrator")
        reg, h2 = reg.add_member("myrepo", "ws", "podman", "worker")
        reg, h3 = reg.add_member("myrepo", "ws", "host", "worker")
        self.assertEqual((h1, h2, h3), ("myrepo.1+ws.podman", "myrepo.2+ws.podman", "myrepo.1+ws.host"))
        self.assertEqual(dict(reg.counters), {"myrepo+ws.podman": 2, "myrepo+ws.host": 1})
        self.assertEqual(reg.members[h1], "orchestrator")

    def test_n_never_reused_after_removal(self) -> None:
        reg = registry.Registry.empty("alpha")
        reg, h1 = reg.add_member("myrepo", "ws", "podman", "worker")
        reg = reg.remove_member(h1)
        self.assertNotIn(h1, reg.members)
        reg, h2 = reg.add_member("myrepo", "ws", "podman", "worker")
        self.assertEqual(h2, "myrepo.2+ws.podman")

    def test_add_member_is_pure(self) -> None:
        reg = registry.Registry.empty("alpha")
        reg.add_member("myrepo", "ws", "podman", "worker")
        self.assertEqual(dict(reg.counters), {})

    def test_counter_exhausted_refused(self) -> None:
        reg = registry.parse_registry({"v": 1, "team": "alpha", "counters": {"r+h.vm": 999999}, "members": {}})
        with self.assertRaises(registry.RegistryError):
            reg.add_member("r", "h", "vm", "worker")

    def test_bad_role_refused(self) -> None:
        with self.assertRaises(registry.RegistryError):
            registry.Registry.empty("alpha").add_member("r", "h", "vm", "admin")

    def test_set_role(self) -> None:
        reg, h = registry.Registry.empty("alpha").add_member("r", "h", "vm", "worker")
        self.assertEqual(reg.set_role(h, "orchestrator").members[h], "orchestrator")
        with self.assertRaises(registry.RegistryError):
            reg.set_role(h, "boss")
        with self.assertRaises(registry.RegistryError):
            reg.set_role("r.9+h.vm", "worker")

    def test_remove_unknown_refused(self) -> None:
        with self.assertRaises(registry.RegistryError):
            registry.Registry.empty("alpha").remove_member("r.1+h.vm")


class ParseRegistryTest(unittest.TestCase):
    GOOD = {
        "v": 2,
        "team": "alpha",
        "counters": {"myrepo+ws.podman": 2},
        "members": {"myrepo.2+ws.podman": "worker", "myrepo.dev+local.podman": "orchestrator"},
        "parked": ["myrepo.dev+local.podman"],
    }
    V1 = {
        "v": 1,
        "team": "alpha",
        "counters": {"myrepo+ws.podman": 2},
        "members": {"myrepo.2+ws.podman": "worker"},
    }

    def _bad(self, **changes: object) -> dict:
        data = json.loads(json.dumps(self.GOOD))
        data.update(changes)
        return data

    def test_good(self) -> None:
        reg = registry.parse_registry(self.GOOD)
        self.assertEqual(dict(reg.members), {"myrepo.2+ws.podman": "worker", "myrepo.dev+local.podman": "orchestrator"})
        self.assertEqual(reg.parked, frozenset({"myrepo.dev+local.podman"}))
        self.assertEqual(reg.as_dict(), self.GOOD)

    def test_v1_loads_as_v2_with_nothing_parked(self) -> None:
        reg = registry.parse_registry(self.V1)
        self.assertEqual(dict(reg.members), {"myrepo.2+ws.podman": "worker"})
        self.assertEqual(reg.parked, frozenset())
        self.assertEqual(reg.as_dict(), {**self.V1, "v": 2, "parked": []})

    def test_v1_refusals(self) -> None:
        for name, data in {"v1 with parked": {**self.V1, "parked": []},
                           "v1 missing members": {k: v for k, v in self.V1.items() if k != "members"}}.items():
            with self.subTest(name), self.assertRaises(registry.RegistryError):
                registry.parse_registry(data)

    def test_refusals(self) -> None:
        cases = {
            "not an object": [],
            "unknown key": self._bad(extra=1),
            "missing members": {k: v for k, v in self.GOOD.items() if k != "members"},
            "missing parked": {k: v for k, v in self.GOOD.items() if k != "parked"},
            "version": self._bad(v=3),
            "version bool": self._bad(v=True),
            "parked not a list": self._bad(parked={}),
            "parked not a member": self._bad(parked=["myrepo.qa+local.podman"]),
            "parked twice": self._bad(parked=["myrepo.dev+local.podman", "myrepo.dev+local.podman"]),
            "parked not a handle": self._bad(parked=[7]),
            "team": self._bad(team="Alpha"),
            "counter key": self._bad(counters={"myrepo.ws.podman": 2}),
            "counter zero": self._bad(counters={"myrepo+ws.podman": 0}),
            "counter bool": self._bad(counters={"myrepo+ws.podman": True}),
            "member handle": self._bad(members={"alice": "worker"}),
            "member role": self._bad(members={"myrepo.2+ws.podman": "admin"}),
            "member beyond counter": self._bad(members={"myrepo.3+ws.podman": "worker"}),
            "member without counter": self._bad(members={"other.1+ws.podman": "worker"}),
        }
        for name, data in cases.items():
            with self.subTest(name), self.assertRaises(registry.RegistryError):
                registry.parse_registry(data)


class LoadSaveTest(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.path = pathlib.Path(tmp.name) / "registry.json"

    def test_missing_file_gives_empty_registry(self) -> None:
        self.assertEqual(registry.load_registry(self.path, "alpha"), registry.Registry.empty("alpha"))

    def test_v1_file_loads(self) -> None:
        self.path.write_text(json.dumps(ParseRegistryTest.V1), encoding="utf-8")
        reg = registry.load_registry(self.path, "alpha")
        self.assertEqual((dict(reg.members), reg.parked), ({"myrepo.2+ws.podman": "worker"}, frozenset()))

    def test_round_trip(self) -> None:
        reg, _ = registry.Registry.empty("alpha").add_member("myrepo", "ws", "podman", "worker")
        reg, _ = reg.add_member("other", "ws", "host", "orchestrator")
        reg, dev = reg.add_member("myrepo", "local", "podman", "worker", seat="dev")
        reg = reg.park(dev)
        registry.save_registry(self.path, reg)
        loaded = registry.load_registry(self.path, "alpha")
        self.assertEqual(loaded, reg)
        self.assertEqual(self.path.read_text(encoding="utf-8"), registry.dump_registry(reg))
        self.assertEqual(stat.S_IMODE(os.stat(self.path).st_mode), 0o600)

    def test_save_leaves_no_temporary_file(self) -> None:
        registry.save_registry(self.path, registry.Registry.empty("alpha"))
        self.assertEqual(sorted(p.name for p in self.path.parent.iterdir()), ["registry.json"])

    def test_team_mismatch_refused(self) -> None:
        registry.save_registry(self.path, registry.Registry.empty("alpha"))
        with self.assertRaises(registry.RegistryError):
            registry.load_registry(self.path, "beta")

    def test_corrupt_file_refused(self) -> None:
        self.path.write_text("{", encoding="utf-8")
        with self.assertRaises(registry.RegistryError):
            registry.load_registry(self.path, "alpha")

    def test_duplicate_keys_refused(self) -> None:
        self.path.write_text('{"v": 1, "v": 1, "team": "alpha", "counters": {}, "members": {}}', encoding="utf-8")
        with self.assertRaises(registry.RegistryError):
            registry.load_registry(self.path, "alpha")


_PROBE_LOCK = """
import fcntl, sys
with open(sys.argv[1], "a") as handle:
    try:
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        print("busy")
    else:
        print("free")
"""


def _probe_lock(lock_path: pathlib.Path) -> str:
    """Try the lock from another process, without waiting; `busy` or `free`."""
    result = subprocess.run(
        [sys.executable, "-I", "-c", _PROBE_LOCK, str(lock_path)],
        check=True,
        capture_output=True,
        text=True,
        timeout=30,
    )
    return result.stdout.strip()


class UpdateRegistryTest(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.path = pathlib.Path(tmp.name) / "registry.json"
        self.lock_path = pathlib.Path(tmp.name) / "registry.lock"

    def test_another_process_cannot_take_the_lock_while_it_is_held(self) -> None:
        seen = []

        def change(reg: registry.Registry) -> tuple[registry.Registry, None]:
            seen.append(_probe_lock(self.lock_path))
            return reg, None

        registry.update_registry(self.path, "alpha", change)
        self.assertEqual(seen, ["busy"])
        self.assertEqual(_probe_lock(self.lock_path), "free")

    def test_change_is_saved_and_result_returned(self) -> None:
        def add(reg: registry.Registry) -> tuple[registry.Registry, str]:
            return reg.add_member("myrepo", "ws", "podman", "worker")

        first = registry.update_registry(self.path, "alpha", add)
        second = registry.update_registry(self.path, "alpha", add)
        self.assertEqual((first, second), ("myrepo.1+ws.podman", "myrepo.2+ws.podman"))
        loaded = registry.load_registry(self.path, "alpha")
        self.assertEqual(sorted(loaded.members), ["myrepo.1+ws.podman", "myrepo.2+ws.podman"])
        self.assertEqual(stat.S_IMODE(os.stat(self.lock_path).st_mode), 0o600)

    def test_failed_change_saves_nothing_and_releases_the_lock(self) -> None:
        def fail(reg: registry.Registry) -> tuple[registry.Registry, None]:
            raise registry.RegistryError("refused")

        with self.assertRaises(registry.RegistryError):
            registry.update_registry(self.path, "alpha", fail)
        self.assertFalse(self.path.exists())
        self.assertEqual(_probe_lock(self.lock_path), "free")

    def test_unchanged_registry_is_not_written(self) -> None:
        registry.update_registry(self.path, "alpha", lambda reg: (reg, None))
        self.assertFalse(self.path.exists())


if __name__ == "__main__":
    unittest.main()

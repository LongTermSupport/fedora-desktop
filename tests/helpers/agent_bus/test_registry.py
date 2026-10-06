"""Unit tests for helpers/agent_bus/registry.py: handles and the per-team registry.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_registry

`registry.json` (`/var/lib/agent-bus/<team>/registry.json`) holds each member handle
with its role and the counter of every `<repo>+<host>.<type>` seat, so a handle's
`<n>` is never reused (PROTOCOL.md section 3, DESIGN.md section 3.4).
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import registry


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

    def test_build_refuses_bad_parts(self) -> None:
        for args in (
            ("MyRepo", 1, "ws", "podman"),
            ("myrepo", 0, "ws", "podman"),
            ("myrepo", 1_000_000, "ws", "podman"),
            ("myrepo", 1, "ws", "kvm"),
            ("myrepo", 1, "Ws", "podman"),
            ("myrepo", True, "ws", "podman"),
        ):
            with self.subTest(args), self.assertRaises(registry.HandleError):
                registry.build_handle(*args)

    def test_seat_of_handle(self) -> None:
        self.assertEqual(registry.seat_of("myrepo.12+ws.lxc"), "myrepo+ws.lxc")
        with self.assertRaises(registry.HandleError):
            registry.seat_of("alice")


class RegistryLogicTest(unittest.TestCase):
    def test_empty(self) -> None:
        reg = registry.Registry.empty("alpha")
        self.assertEqual((reg.team, dict(reg.counters), dict(reg.members)), ("alpha", {}, {}))

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
        self.assertEqual(dict(reg.members), {"myrepo.2+ws.podman": "worker"})

    def test_refusals(self) -> None:
        cases = {
            "not an object": [],
            "unknown key": self._bad(extra=1),
            "missing members": {k: v for k, v in self.GOOD.items() if k != "members"},
            "version": self._bad(v=2),
            "version bool": self._bad(v=True),
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

    def test_round_trip(self) -> None:
        reg, _ = registry.Registry.empty("alpha").add_member("myrepo", "ws", "podman", "worker")
        reg, _ = reg.add_member("other", "ws", "host", "orchestrator")
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


if __name__ == "__main__":
    unittest.main()

"""Unit tests for helpers/pingbus/matrix_clients.py, the one Matrix client list (issue #251).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_matrix_clients

The reader mirrors the Go package src-go/internal/matrixclients rule for rule: the list's
validation, the paths derived from it, the profiles present under a home, and the consent
record's grammar.
"""

from __future__ import annotations

import os
import pathlib
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.pingbus import matrix_clients

TWO_CLIENTS = """{
  "clients": [
    {"name": "Element", "app_id": "im.riot.Riot", "keyring": true,
     "native": [{"parent": ".config", "name": "Element", "profiles": true}]},
    {"name": "Nheko", "app_id": "im.nheko.Nheko", "keyring": true, "native": []}
  ],
  "legacy_consent": ["im.riot.Riot"]
}"""


def two_clients() -> matrix_clients.MatrixClients:
    return matrix_clients.parse(TWO_CLIENTS.encode())


class ListTests(unittest.TestCase):
    def test_the_packaged_list_loads_and_is_the_repository_file(self) -> None:
        listed = matrix_clients.load()
        self.assertIn("im.riot.Riot", listed.app_ids())
        on_disk = matrix_clients.parse((REPO_ROOT / "helpers/pingbus/matrix_clients.json").read_bytes())
        self.assertEqual(listed, on_disk)

    def test_paths_are_derived_from_the_list(self) -> None:
        listed = two_clients()
        self.assertEqual(listed.profile_paths(), (".var/app/im.riot.Riot", ".config/Element", ".var/app/im.nheko.Nheko"))
        self.assertEqual(listed.profile_prefixes(), (".config/Element-",))
        self.assertEqual(listed.names(["im.nheko.Nheko", "im.riot.Riot"]), "Element and Nheko")
        self.assertEqual(listed.client("Nheko").flatpak_dir, ".var/app/im.nheko.Nheko")

    def test_words_join_as_a_sentence_does(self) -> None:
        self.assertEqual(matrix_clients.words([]), "")
        self.assertEqual(matrix_clients.words(["A"]), "A")
        self.assertEqual(matrix_clients.words(["A", "B", "C"]), "A, B and C")

    def test_a_bad_list_is_refused(self) -> None:
        bad = {
            "unknown key": '{"clients": [], "legacy_consent": [], "extra": 1}',
            "no clients": '{"clients": [], "legacy_consent": []}',
            "bad app id": TWO_CLIENTS.replace("im.nheko.Nheko", "nheko", 1),
            "app id twice": TWO_CLIENTS.replace("im.nheko.Nheko", "im.riot.Riot", 1),
            "empty name": TWO_CLIENTS.replace('"Nheko"', '""', 1),
            "parent escapes": TWO_CLIENTS.replace('".config"', '"../x"', 1),
            "name with a slash": TWO_CLIENTS.replace('"name": "Element", "profiles"', '"name": "a/b", "profiles"', 1),
            "legacy unlisted": TWO_CLIENTS.replace('"legacy_consent": ["im.riot.Riot"]', '"legacy_consent": ["org.x.Y"]', 1),
            "trailing data": TWO_CLIENTS + "{}",
            "keyring not a bool": TWO_CLIENTS.replace('"keyring": true, "native": []', '"keyring": 1, "native": []', 1),
            "not an object": "[]",
        }
        for name, text in bad.items():
            with self.subTest(name), self.assertRaises(matrix_clients.ListError):
                matrix_clients.parse(text.encode())


class ConsentTests(unittest.TestCase):
    def test_the_consent_record_is_read(self) -> None:
        listed = two_clients()
        good = {
            "": ["im.riot.Riot"],
            "accepted\n": ["im.riot.Riot"],
            "accepted 2026-10-15T12:30:00Z\n": ["im.riot.Riot"],
            "accepted x\nclients\n": [],
            "clients im.nheko.Nheko\n": ["im.nheko.Nheko"],
            "accepted x\nclients im.riot.Riot im.nheko.Nheko\n": ["im.riot.Riot", "im.nheko.Nheko"],
            "clients org.other.Client": ["org.other.Client"],
        }
        for text, want in good.items():
            with self.subTest(text):
                self.assertEqual(listed.parse_consent(text.encode()), want)

    def test_anything_else_is_not_a_record(self) -> None:
        listed = two_clients()
        bad = [
            "clients im.riot.Riot\nclients im.nheko.Nheko\n",
            "clients  im.riot.Riot\n",
            "clients im.riot.Riot \n",
            "clients im.riot.Riot im.riot.Riot\n",
            "clients riot\n",
            "clients\tim.riot.Riot\n",
            "x" * (matrix_clients.CONSENT_MAX_BYTES + 1),
        ]
        for text in bad:
            with self.subTest(text[:40]), self.assertRaises(matrix_clients.ConsentError):
                listed.parse_consent(text.encode())

    def test_a_record_that_is_not_utf8_is_read_as_bytes(self) -> None:
        self.assertEqual(two_clients().parse_consent(b"accepted \xff\n"), ["im.riot.Riot"])
        with self.assertRaises(matrix_clients.ConsentError):
            two_clients().parse_consent(b"clients \xff.a.b\n")


class PresentTests(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.home = pathlib.Path(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_an_empty_home_has_none(self) -> None:
        self.assertEqual(two_clients().present(self.home), [])

    def test_every_clients_profile_is_found_in_list_order(self) -> None:
        (self.home / ".config/Element-work").mkdir(parents=True)
        (self.home / ".config/Elementary").mkdir()
        (self.home / ".var/app").mkdir(parents=True)
        (self.home / ".var/app/im.nheko.Nheko").symlink_to("gone")
        found = two_clients().present(self.home)
        self.assertEqual([(f.client.name, f.path) for f in found],
                         [("Element", self.home / ".config/Element-work"),
                          ("Nheko", self.home / ".var/app/im.nheko.Nheko")])
        uncovered = matrix_clients.uncovered(found, ["im.riot.Riot"])
        self.assertEqual([c.app_id for c in uncovered], ["im.nheko.Nheko"])
        self.assertEqual(matrix_clients.uncovered(found, ["im.riot.Riot", "im.nheko.Nheko"]), [])

    @unittest.skipIf(os.geteuid() == 0, "root lists any directory")
    def test_an_unlistable_parent_is_an_error(self) -> None:
        parent = self.home / ".config"
        parent.mkdir()
        parent.chmod(0o300)
        try:
            with self.assertRaisesRegex(matrix_clients.PresenceError, "cannot be listed"):
                two_clients().present(self.home)
        finally:
            parent.chmod(0o700)


if __name__ == "__main__":
    unittest.main()

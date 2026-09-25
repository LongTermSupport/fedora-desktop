"""Unit tests for helpers/github_signing/signing.py — the pure rules behind signing-key registration.

python3 -m unittest tests.helpers.github_signing.test_signing
"""

from __future__ import annotations

import pathlib
import sys
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.github_signing import signing

ED = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAA"
ED2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBBB"
ED3 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICCCC"


class TestParseAccounts(unittest.TestCase):
    def test_a_map_of_alias_to_login(self):
        self.assertEqual(
            signing.parse_accounts('{"work": "alice", "home.2": "bob"}'),
            {"work": "alice", "home.2": "bob"},
        )

    def test_anything_else_is_refused(self):
        for bad in (
            "",
            "[]",
            "{}",
            '{"a": 1}',
            '{"a": ""}',
            '{"-a": "x"}',
            '{"a/b": "x"}',
        ):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                signing.parse_accounts(bad)


class TestAccountKeyName(unittest.TestCase):
    def test_it_sits_beside_the_account_login_key(self):
        self.assertEqual(signing.account_key_name("work"), "github_work_signing")


class TestKeyBlob(unittest.TestCase):
    def test_the_blob_is_the_second_field(self):
        self.assertEqual(signing.key_blob(f"{ED} someone@example.com"), ED.split()[1])

    def test_a_line_with_no_comment_is_accepted(self):
        self.assertEqual(signing.key_blob(ED), ED.split()[1])

    def test_surrounding_whitespace_is_ignored(self):
        self.assertEqual(signing.key_blob(f"  {ED}  \n"), ED.split()[1])

    def test_a_line_that_is_not_a_public_key_is_refused(self):
        for bad in ("", "AAAAC3Nza", "not-a-type AAAA", "ssh-ed25519"):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                signing.key_blob(bad)


class TestBlobs(unittest.TestCase):
    def test_one_key_per_line(self):
        self.assertEqual(
            signing.blobs(f"{ED}\n{ED2} x\n"), {ED.split()[1], ED2.split()[1]}
        )

    def test_an_empty_listing_is_no_keys(self):
        self.assertEqual(signing.blobs("\n"), set())

    def test_a_malformed_line_is_refused_not_skipped(self):
        with self.assertRaises(ValueError):
            signing.blobs(f"{ED}\ngarbage\n")


class TestOwnerOfLoginKey(unittest.TestCase):
    def test_the_one_account_holding_the_login_key(self):
        auth = {"alice": {"b1"}, "bob": {"login", "b2"}}
        self.assertEqual(signing.owner_of_login_key("login", auth), "bob")

    def test_no_account_holding_it_is_refused(self):
        with self.assertRaisesRegex(ValueError, "none of"):
            signing.owner_of_login_key("login", {"alice": {"b1"}})

    def test_two_accounts_holding_it_is_refused(self):
        with self.assertRaisesRegex(ValueError, "alice, bob"):
            signing.owner_of_login_key("login", {"alice": {"login"}, "bob": {"login"}})


class TestMissingRegistrations(unittest.TestCase):
    def test_a_key_already_registered_is_not_asked_for(self):
        wanted = [signing.Wanted("alice", "k1", "b1")]
        self.assertEqual(signing.missing_registrations(wanted, {"alice": {"b1"}}), [])

    def test_a_key_on_another_account_still_counts_as_missing(self):
        wanted = [signing.Wanted("alice", "k1", "b1")]
        self.assertEqual(
            signing.missing_registrations(wanted, {"alice": set(), "bob": {"b1"}}),
            wanted,
        )

    def test_order_is_kept(self):
        wanted = [
            signing.Wanted("bob", "k2", "b2"),
            signing.Wanted("alice", "k1", "b1"),
        ]
        self.assertEqual(
            signing.missing_registrations(wanted, {"alice": set(), "bob": set()}),
            wanted,
        )

    def test_one_account_can_want_two_keys(self):
        wanted = [
            signing.Wanted("alice", "k1", "b1"),
            signing.Wanted("alice", "machine", "b3"),
        ]
        self.assertEqual(
            signing.missing_registrations(wanted, {"alice": {"b1"}}), [wanted[1]]
        )


if __name__ == "__main__":
    unittest.main()

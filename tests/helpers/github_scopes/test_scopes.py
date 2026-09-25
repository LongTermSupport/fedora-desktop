"""Unit tests for helpers/github_scopes/scopes.py — the one copy of the scope rules.

    python3 -m unittest tests.helpers.github_scopes.test_scopes
"""

from __future__ import annotations

import pathlib
import sys
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.github_scopes import scopes


class TestLoadRequired(unittest.TestCase):
    def test_reads_the_list_and_drops_comments(self):
        text = (
            "---\n"
            "# a header comment\n"
            "github_required_scopes:\n"
            "  - repo       # full repo access\n"
            "  - gist\n"
            "\n"
            "  - workflow  # push workflow files\n"
        )
        self.assertEqual(scopes.load_required(text), ["repo", "gist", "workflow"])

    def test_the_repository_file_parses(self):
        text = (REPO_ROOT / "vars" / "github-required-scopes.yml").read_text(encoding="utf-8")
        loaded = scopes.load_required(text)
        self.assertIn("repo", loaded)
        self.assertIn("admin:public_key", loaded)
        self.assertEqual(len(loaded), len(set(loaded)))

    def test_no_list_is_an_error(self):
        with self.assertRaises(ValueError):
            scopes.load_required("---\nsomething_else:\n  - repo\n")

    def test_an_empty_list_is_an_error(self):
        with self.assertRaises(ValueError):
            scopes.load_required("github_required_scopes:\n")

    def test_a_duplicate_is_an_error(self):
        with self.assertRaises(ValueError):
            scopes.load_required("github_required_scopes:\n  - repo\n  - repo\n")

    def test_a_line_it_does_not_understand_is_an_error(self):
        # A flow list or a nested map would be read as nothing at all by a lax parser, and
        # a required scope would silently drop out of every check.
        with self.assertRaises(ValueError):
            scopes.load_required("github_required_scopes: [repo, gist]\n")
        with self.assertRaises(ValueError):
            scopes.load_required("github_required_scopes:\n  - repo\n  extra: 1\n")

    def test_a_later_top_level_key_ends_the_list(self):
        text = "github_required_scopes:\n  - repo\nother_key: 1\n"
        self.assertEqual(scopes.load_required(text), ["repo"])


class TestParseGranted(unittest.TestCase):
    def test_reads_the_scopes_header_from_an_http_response(self):
        response = (
            "HTTP/2.0 200 OK\r\n"
            "Access-Control-Expose-Headers: ETag, X-OAuth-Scopes, X-Accepted-OAuth-Scopes\r\n"
            "X-Oauth-Scopes: admin:public_key, gist, repo\r\n"
            "\r\n"
            '{"login": "someone"}\n'
        )
        self.assertEqual(scopes.parse_granted_response(response), {"admin:public_key", "gist", "repo"})

    def test_the_expose_headers_line_is_not_read_as_scopes(self):
        response = "Access-Control-Expose-Headers: X-OAuth-Scopes\r\n\r\n{}\n"
        self.assertEqual(scopes.parse_granted_response(response), set())

    def test_an_empty_header_grants_nothing(self):
        self.assertEqual(scopes.parse_granted_response("X-OAuth-Scopes: \r\n\r\n"), set())

    def test_a_plain_comma_separated_list(self):
        self.assertEqual(scopes.parse_granted_list("gist, repo,workflow"), {"gist", "repo", "workflow"})


class TestMissing(unittest.TestCase):
    def test_nothing_missing(self):
        self.assertEqual(scopes.missing(["repo", "gist"], {"gist", "repo"}), [])

    def test_missing_scopes_keep_the_required_order(self):
        self.assertEqual(
            scopes.missing(["repo", "gist", "workflow"], {"gist"}),
            ["repo", "workflow"],
        )

    def test_admin_implies_write_implies_read(self):
        for family in ("org", "public_key", "repo_hook", "gpg_key", "ssh_signing_key"):
            with self.subTest(family=family):
                self.assertEqual(scopes.missing([f"read:{family}"], {f"admin:{family}"}), [])
                self.assertEqual(scopes.missing([f"read:{family}"], {f"write:{family}"}), [])
                self.assertEqual(scopes.missing([f"write:{family}"], {f"admin:{family}"}), [])
                self.assertEqual(scopes.missing([f"admin:{family}"], {f"write:{family}"}), [f"admin:{family}"])

    def test_user_implies_its_children(self):
        for child in ("read:user", "user:email", "user:follow"):
            with self.subTest(child=child):
                self.assertEqual(scopes.missing([child], {"user"}), [])

    def test_project_implies_read_project(self):
        self.assertEqual(scopes.missing(["read:project"], {"project"}), [])
        self.assertEqual(scopes.missing(["project"], {"read:project"}), ["project"])

    def test_a_child_never_satisfies_its_parent(self):
        self.assertEqual(scopes.missing(["user"], {"user:email"}), ["user"])
        self.assertEqual(scopes.missing(["admin:org"], {"read:org"}), ["admin:org"])

    def test_an_unrelated_scope_is_only_satisfied_by_itself(self):
        self.assertEqual(scopes.missing(["workflow"], {"repo"}), ["workflow"])


if __name__ == "__main__":
    unittest.main()

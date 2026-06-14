"""Unit tests for helpers/pyenv/resolver.py — pure pyenv version-selection logic.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.pyenv.test_resolver
    # or directly:
    python3 tests/helpers/pyenv/test_resolver.py

The resolver is a namespace package (no __init__.py); we put the repo root on
sys.path so `from helpers.pyenv import resolver` resolves. The sys.path edit
before the import is why ruff E402 is ignored for tests/** in ruff.toml.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pyenv import resolver

# A representative `pyenv install --list` dump: indented entries, stable CPython
# lines mixed with pre-releases, free-threaded (t) builds, legacy 2.x, and
# non-CPython interpreters that must all be ignored.
SAMPLE_LISTING = """\
Available versions:
  2.7.18
  3.9.18
  3.9.19
  3.10.13
  3.10.14
  3.11.8
  3.11.9
  3.12.2
  3.12.3
  3.13.0
  3.13.1
  3.13.2
  3.14.0
  3.14.1
  3.13.0t
  3.14.0a1
  3.14.0rc1
  pypy3.10-7.3.15
  miniconda3-latest
  anaconda3-2024.02
"""


class TestParseCpythonVersions(unittest.TestCase):
    def test_extracts_only_stable_cpython(self):
        versions = resolver.parse_cpython_versions(SAMPLE_LISTING)
        self.assertIn((3, 14, 1), versions)
        self.assertIn((2, 7, 18), versions)
        self.assertIn((3, 13, 2), versions)

    def test_ignores_prerelease_freethreaded_and_alt_interpreters(self):
        versions = resolver.parse_cpython_versions(SAMPLE_LISTING)
        # No string artefacts survive (pre-release, t-suffix, pypy, conda).
        flat = " ".join(resolver.fmt(v) for v in versions)
        self.assertNotIn("a1", flat)
        self.assertNotIn("rc", flat)
        self.assertNotIn("t", flat)
        self.assertNotIn("pypy", flat)

    def test_sorted_numerically_not_lexically(self):
        versions = resolver.parse_cpython_versions(SAMPLE_LISTING)
        # Lexical sort would put 3.9.x after 3.14.x; numeric must not.
        self.assertEqual(versions, sorted(versions))
        self.assertLess(versions.index((3, 9, 18)), versions.index((3, 10, 13)))
        self.assertLess(versions.index((3, 10, 14)), versions.index((3, 14, 0)))

    def test_deduplicates(self):
        versions = resolver.parse_cpython_versions("  3.12.3\n  3.12.3\n")
        self.assertEqual(versions, [(3, 12, 3)])

    def test_empty_listing_yields_empty(self):
        self.assertEqual(resolver.parse_cpython_versions(""), [])


class TestNewestMinors(unittest.TestCase):
    def test_picks_newest_n_minor_lines(self):
        versions = resolver.parse_cpython_versions(SAMPLE_LISTING)
        self.assertEqual(
            resolver.newest_minors(versions, 4),
            [(3, 14), (3, 13), (3, 12), (3, 11)],
        )

    def test_count_larger_than_available_returns_all(self):
        versions = [(3, 13, 0), (3, 14, 0)]
        self.assertEqual(resolver.newest_minors(versions, 10), [(3, 14), (3, 13)])


class TestLatestPatch(unittest.TestCase):
    def test_returns_highest_patch_for_minor(self):
        versions = resolver.parse_cpython_versions(SAMPLE_LISTING)
        self.assertEqual(resolver.latest_patch(versions, (3, 13)), (3, 13, 2))
        self.assertEqual(resolver.latest_patch(versions, (3, 10)), (3, 10, 14))

    def test_unknown_minor_returns_none(self):
        versions = resolver.parse_cpython_versions(SAMPLE_LISTING)
        self.assertIsNone(resolver.latest_patch(versions, (3, 99)))


class TestParseInstalledMinors(unittest.TestCase):
    def test_extracts_unique_sorted_minors(self):
        bare = "3.10.5\n3.13.0\n3.13.2\n"
        self.assertEqual(resolver.parse_installed_minors(bare), [(3, 10), (3, 13)])

    def test_empty_is_empty(self):
        self.assertEqual(resolver.parse_installed_minors(""), [])


class TestBuildInstallPlan(unittest.TestCase):
    def test_top_n_latest_patches_when_nothing_installed(self):
        plan = resolver.build_install_plan(SAMPLE_LISTING, "", 4)
        self.assertEqual(
            plan.to_install, ["3.14.1", "3.13.2", "3.12.3", "3.11.9"]
        )
        self.assertEqual(plan.stale_kept, [])
        self.assertEqual(plan.top_minors, ["3.14", "3.13", "3.12", "3.11"])

    def test_installed_minor_outside_top_is_kept_and_flagged_stale(self):
        # Top 2 = 3.14, 3.13. Installed 3.10 + 3.13 → 3.10 is the stale extra.
        plan = resolver.build_install_plan(SAMPLE_LISTING, "3.10.5\n3.13.0\n", 2)
        self.assertEqual(plan.to_install, ["3.14.1", "3.13.2", "3.10.14"])
        self.assertEqual(plan.stale_kept, ["3.10"])

    def test_installed_minor_no_longer_in_listing_is_stale_not_installed(self):
        # 3.7 is EOL and absent from the listing: keep+warn, never try to install.
        plan = resolver.build_install_plan(SAMPLE_LISTING, "3.7.9\n", 2)
        self.assertNotIn("3.7.9", plan.to_install)
        self.assertEqual(plan.stale_kept, ["3.7"])

    def test_empty_listing_raises(self):
        with self.assertRaises(ValueError):
            resolver.build_install_plan("", "", 4)

    def test_zero_minor_count_raises(self):
        with self.assertRaises(ValueError):
            resolver.build_install_plan(SAMPLE_LISTING, "", 0)


if __name__ == "__main__":
    unittest.main()

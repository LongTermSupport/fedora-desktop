"""Tests for helpers/vmtest/validate_manifest.py — the thin manifest validator.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_validate_manifest

The executor reads the JSON form of the manifest on stdin, validates it with
scenarios.parse_manifest, and prints marker lines on stdout. It is driven here
as a subprocess so what is tested is the real entry point, exit code and
stream split (markers on stdout, diagnostics on stderr).
"""

from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from tests.helpers.vmtest.test_scenarios import MANIFEST


def _run(stdin_text: str, *args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, "-m", "helpers.vmtest.validate_manifest", "--fedora-version", "44", *args],
        cwd=REPO_ROOT,
        input=stdin_text,
        capture_output=True,
        text=True,
        check=False,
    )


class TestValidateManifest(unittest.TestCase):
    def test_valid_manifest_exits_zero_with_markers_on_stdout(self):
        result = _run(json.dumps(MANIFEST))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VMTEST-MANIFEST-OK scenarios=3 runnable=2 bases=3", result.stdout)
        self.assertEqual(result.stderr, "")

    def test_allowlist_flag_prints_the_allowlist_only(self):
        result = _run(json.dumps(MANIFEST), "--allowlist")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "server-fast-provision\nserver-full-provision\n")

    def test_invalid_manifest_exits_non_zero_with_the_reason_on_stderr(self):
        document = json.loads(json.dumps(MANIFEST))
        document["vm_test_scenarios"]["server-fast-provision"]["base"] = "nope"
        result = _run(json.dumps(document))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("VMTEST-MANIFEST-INVALID", result.stdout)
        self.assertIn("nope", result.stderr)

    def test_invalid_json_exits_non_zero(self):
        result = _run("{not json")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("VMTEST-MANIFEST-INVALID", result.stdout)

    def test_missing_fedora_version_is_a_usage_error(self):
        result = subprocess.run(
            [sys.executable, "-m", "helpers.vmtest.validate_manifest"],
            cwd=REPO_ROOT,
            input=json.dumps(MANIFEST),
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--fedora-version", result.stderr)


if __name__ == "__main__":
    unittest.main()

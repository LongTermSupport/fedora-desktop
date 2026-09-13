"""Tests for helpers/vmtest/freshness_gate.py — the executor that gates a run on freshness.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_freshness_gate

The gate reads base.json, the probe's marker lines, the manifest and the
current recipe digest, recomputes the base's LIVE identity from what upstream
says today, and prints the §4.4 verdict as one marker line. It is what
`vmtest run` consults before cloning an overlay (DESIGN.md §9 T6.1), so it is
driven here as a subprocess against fixture files.
"""

from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.vmtest import basejson
from tests.helpers.vmtest.test_probe_upstream import PROBE_MANIFEST

SHA_ART = "28680fe5" + "0" * 50 + "f90b7f"
RECIPE = "b" * 64
BASE_SHA = "c" * 64
NOW = 1_800_000_000
REVISION = 1789172543

PROBE_TEXT = (
    "VMTEST-FRESHNESS-COMPOSE-ID Fedora-44-20260422.1\n"
    f"VMTEST-FRESHNESS-REVISION {REVISION}\n"
    "VMTEST-FRESHNESS-BODHI F44 current\n"
    "VMTEST-FRESHNESS-TREE Everything build_timestamp=1776865868\n"
    "VMTEST-FRESHNESS-TREE-CHECKSUM Everything images/install.img " + "d" * 64 + "\n"
    f"VMTEST-FRESHNESS-ARTEFACT server-fast-44 Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2 {SHA_ART} "
    "label=44-1.7 variant=Cloud url=https://example.org/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2\n"
    "VMTEST-FRESHNESS-DONE unreadable=0\n"
)


def _record(**overrides):
    fields = {
        "fedora_version": 44,
        "profile": "server",
        "kind": "fast",
        "name": "server-fast-44",
        "compose_id": "Fedora-44-20260422.1",
        "compose_label": "44-1.7",
        "artefacts": ({"name": "Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2", "sha256": SHA_ART},),
        "treeinfo_checksums": None,
        "recipe_digest": RECIPE,
        "installed_at": NOW - 86400,
        "last_upgraded_at": NOW - 86400,
        "last_upgraded_revision": REVISION,
        "last_upgraded_mirror": "https://mirror.example.net/updates/",
        "refresh_state": "complete",
        "base_sha256": BASE_SHA,
        "base_size": 5_000_000_000,
        "base_mtime": NOW - 86400,
    }
    fields.update(overrides)
    return basejson.render_record(basejson.build_record(**fields))


class GateCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        (self.root / "manifest.json").write_text(json.dumps(PROBE_MANIFEST))

    def run_gate(self, record_text=None, probe_text=PROBE_TEXT, *extra):
        (self.root / "base.json").write_text(record_text if record_text is not None else _record())
        (self.root / "probe.txt").write_text(probe_text)
        return subprocess.run(
            [
                sys.executable, "-m", "helpers.vmtest.freshness_gate",
                "--fedora-version", "44",
                "--manifest", str(self.root / "manifest.json"),
                "--base-json", str(self.root / "base.json"),
                "--probe", str(self.root / "probe.txt"),
                "--recipe-digest", RECIPE,
                "--actual-base-sha256", BASE_SHA,
                "--now", str(NOW),
                *extra,
            ],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            check=False,
        )

    def verdict_line(self, result):
        lines = [line for line in result.stdout.splitlines() if line.startswith("VMTEST-FRESHNESS-VERDICT ")]
        self.assertEqual(len(lines), 1, result.stdout + result.stderr)
        return dict(field.split("=", 1) for field in lines[0].split(" ", 1)[1].split("\t"))


class TestGate(GateCase):
    def test_current_base_passes_the_gate(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        verdict = self.verdict_line(result)
        self.assertEqual(verdict["decision"], "current")
        self.assertEqual(verdict["degraded"], "false")
        self.assertIn("VMTEST-BASE-RECORD name=server-fast-44 kind=fast profile=server", result.stdout)

    def test_advanced_revision_is_refresh_and_exits_zero(self):
        # refresh is applied after the run (§4.4a); the run may proceed.
        result = self.run_gate(None, PROBE_TEXT.replace(str(REVISION), str(REVISION + 100)))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.verdict_line(result)["decision"], "refresh")

    def test_changed_artefact_hash_upstream_is_reinstall_and_exits_non_zero(self):
        result = self.run_gate(None, PROBE_TEXT.replace(SHA_ART, "e" * 64))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.verdict_line(result)["decision"], "reinstall")

    def test_changed_recipe_is_reinstall(self):
        result = self.run_gate(None, PROBE_TEXT, "--recipe-digest", "f" * 64)
        self.assertNotEqual(result.returncode, 0)
        verdict = self.verdict_line(result)
        self.assertEqual(verdict["decision"], "reinstall")
        self.assertIn("recipe", verdict["reason"])

    def test_base_disk_mismatch_is_reinstall(self):
        result = self.run_gate(None, PROBE_TEXT, "--actual-base-sha256", "9" * 64)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.verdict_line(result)["decision"], "reinstall")

    def test_unreadable_artefacts_is_unknown_and_exits_non_zero(self):
        probe = PROBE_TEXT.replace(
            f"VMTEST-FRESHNESS-ARTEFACT server-fast-44 Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2 {SHA_ART} "
            "label=44-1.7 variant=Cloud url=https://example.org/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2\n",
            "VMTEST-FRESHNESS-UNREADABLE artefacts https://fedoraproject.org/releases.json HTTP Error 503\n",
        ).replace("unreadable=0", "unreadable=1")
        result = self.run_gate(None, probe)
        self.assertNotEqual(result.returncode, 0)
        verdict = self.verdict_line(result)
        self.assertEqual(verdict["decision"], "unknown")
        self.assertIn("releases.json", verdict["reason"])

    def test_unreadable_revision_inside_ttl_is_current_but_degraded(self):
        probe = PROBE_TEXT.replace(
            f"VMTEST-FRESHNESS-REVISION {REVISION}\n",
            "VMTEST-FRESHNESS-UNREADABLE revision https://dl.fedoraproject.org/x/repomd.xml timed out\n",
        ).replace("unreadable=0", "unreadable=1")
        result = self.run_gate(None, probe)
        self.assertEqual(result.returncode, 0, result.stderr)
        verdict = self.verdict_line(result)
        self.assertEqual(verdict["decision"], "current")
        self.assertEqual(verdict["degraded"], "true")
        self.assertIn("package-revision-unreadable", verdict["divergences"])

    def test_bodhi_not_current_warns_but_passes(self):
        result = self.run_gate(None, PROBE_TEXT.replace("F44 current", "F44 archived"))
        self.assertEqual(result.returncode, 0, result.stderr)
        verdict = self.verdict_line(result)
        self.assertEqual(verdict["decision"], "current")
        self.assertIn("release-not-current", verdict["divergences"])
        self.assertIn("archived", result.stderr)

    def test_record_only_prints_the_record_and_no_verdict(self):
        # Even with a wrong disk hash the record-only mode exits 0: it judges
        # nothing, it only reads.
        result = self.run_gate(None, PROBE_TEXT, "--actual-base-sha256", "9" * 64, "--record-only")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VMTEST-BASE-RECORD name=server-fast-44", result.stdout)
        self.assertIn(f"base_sha256={BASE_SHA}", result.stdout)
        self.assertNotIn("VMTEST-FRESHNESS-VERDICT", result.stdout)

    def test_invalid_record_is_a_hard_error(self):
        result = self.run_gate("{not json")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("VMTEST-FRESHNESS-VERDICT", result.stdout)
        self.assertIn("base.json", result.stderr)

    def test_record_for_a_base_the_manifest_does_not_know_is_a_hard_error(self):
        # Manifest and record must agree on kind and tree for the live identity
        # to be computed from the right upstream facts.
        result = self.run_gate(_record(name="server-full-44", kind="full", treeinfo_checksums={"images/install.img": "d" * 64}))
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("VMTEST-FRESHNESS-VERDICT", result.stdout)


if __name__ == "__main__":
    unittest.main()

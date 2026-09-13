"""Tests for helpers/vmtest/probe_upstream.py — the thin executor that reads upstream.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_probe_upstream

The probe is driven as a subprocess against a `--fixture-dir`: a directory
mirroring each upstream URL as `<host>/<path>` (a trailing `/` becomes
`index`). No test here touches the network. A missing or malformed fixture is
the offline analogue of a transport error, which is how the per-signal
UNREADABLE path is exercised.

Marker lines are the contract (Plan 00110 DESIGN.md §9 T1.4): stdout carries
only `VMTEST-FRESHNESS-*` lines; everything else goes to stderr.
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

from tests.helpers.vmtest.test_scenarios import MANIFEST
from tests.helpers.vmtest.test_upstream import (
    BODHI_TEXT,
    COMPOSE_ID_TEXT,
    RELEASES_JSON_TEXT,
    REPOMD_TEXT,
    TREEINFO_TEXT,
)

FIXTURES = {
    "dl.fedoraproject.org/pub/fedora/linux/releases/44/COMPOSE_ID": COMPOSE_ID_TEXT,
    "dl.fedoraproject.org/pub/fedora/linux/releases/44/Server/x86_64/os/.treeinfo": TREEINFO_TEXT,
    "dl.fedoraproject.org/pub/fedora/linux/releases/44/Everything/x86_64/os/.treeinfo": TREEINFO_TEXT,
    "dl.fedoraproject.org/pub/fedora/linux/updates/44/Everything/x86_64/repodata/repomd.xml": REPOMD_TEXT,
    "fedoraproject.org/releases.json": RELEASES_JSON_TEXT,
    "bodhi.fedoraproject.org/releases/index": BODHI_TEXT,
}

# The test manifest's Server selector names the netinst, which the trimmed
# releases.json fixture does not carry; the fixture-side manifest points it at
# the entries the fixture does have.
PROBE_MANIFEST = json.loads(json.dumps(MANIFEST))
PROBE_MANIFEST["vm_test_bases"]["server-full"]["artefacts"] = [
    {"variant": "Server", "subvariant": "Server", "prefix": "Fedora-Server-Guest-Generic-", "suffix": ".qcow2"},
]


def _write_fixtures(root: pathlib.Path, **overrides: str | None) -> None:
    for relative, text in {**FIXTURES, **overrides}.items():
        if text is None:
            continue
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)


def _run(fixture_dir: pathlib.Path, manifest_path: pathlib.Path) -> subprocess.CompletedProcess:
    return subprocess.run(
        [
            sys.executable,
            "-m",
            "helpers.vmtest.probe_upstream",
            "--fedora-version",
            "44",
            "--manifest",
            str(manifest_path),
            "--fixture-dir",
            str(fixture_dir),
        ],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=False,
    )


class ProbeCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.fixture_dir = self.root / "upstream"
        self.manifest_path = self.root / "manifest.json"
        self.manifest_path.write_text(json.dumps(PROBE_MANIFEST))

    def markers(self, result):
        lines = result.stdout.splitlines()
        for line in lines:
            self.assertTrue(line.startswith("VMTEST-FRESHNESS-"), f"non-marker line on stdout: {line!r}")
        return lines


class TestProbeReadsEverySignal(ProbeCase):
    def test_full_probe_emits_every_signal_and_exits_zero(self):
        _write_fixtures(self.fixture_dir)
        result = _run(self.fixture_dir, self.manifest_path)
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = self.markers(result)
        self.assertIn("VMTEST-FRESHNESS-COMPOSE-ID Fedora-44-20260422.1", lines)
        self.assertIn("VMTEST-FRESHNESS-REVISION 1789172543", lines)
        self.assertIn("VMTEST-FRESHNESS-BODHI F44 current", lines)
        self.assertIn("VMTEST-FRESHNESS-TREE Server build_timestamp=1776865868", lines)
        self.assertIn("VMTEST-FRESHNESS-TREE Everything build_timestamp=1776865868", lines)
        self.assertIn(
            "VMTEST-FRESHNESS-TREE-CHECKSUM Everything images/install.img "
            "c2571f26c8d46411f8700388f7ab61d8e27356f960430dcc476325b7157ac8b0",
            lines,
        )
        self.assertIn(
            "VMTEST-FRESHNESS-ARTEFACT server-fast-44 Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2 "
            "28680fe5" + "0" * 50 + "f90b7f label=44-1.7",
            lines,
        )
        self.assertIn(
            "VMTEST-FRESHNESS-ARTEFACT desktop-44 Fedora-Workstation-Live-44-1.7.x86_64.iso "
            "1620295f" + "0" * 50 + "426ddf label=44-1.7",
            lines,
        )
        self.assertEqual(lines[-1], "VMTEST-FRESHNESS-DONE unreadable=0")
        self.assertFalse([line for line in lines if line.startswith("VMTEST-FRESHNESS-UNREADABLE")])

    def test_a_fast_base_gets_no_tree_lines_and_each_tree_is_fetched_once(self):
        _write_fixtures(self.fixture_dir)
        lines = self.markers(_run(self.fixture_dir, self.manifest_path))
        trees = [line for line in lines if line.startswith("VMTEST-FRESHNESS-TREE ")]
        self.assertEqual(len(trees), 2, trees)
        self.assertFalse([line for line in trees if "server-fast" in line])

    def test_diagnostics_go_to_stderr_not_stdout(self):
        _write_fixtures(self.fixture_dir)
        result = _run(self.fixture_dir, self.manifest_path)
        self.markers(result)
        self.assertIn("fixture", result.stderr)


class TestProbeFailsClosedPerSignal(ProbeCase):
    def test_missing_compose_id_is_unreadable_but_other_signals_still_report(self):
        # §4.4 is per signal: identity unreadable and revision unreadable have
        # different verdicts, so the probe must report each signal's fate
        # rather than abort on the first failure.
        _write_fixtures(self.fixture_dir, **{"dl.fedoraproject.org/pub/fedora/linux/releases/44/COMPOSE_ID": None})
        result = _run(self.fixture_dir, self.manifest_path)
        self.assertNotEqual(result.returncode, 0)
        lines = self.markers(result)
        unreadable = [line for line in lines if line.startswith("VMTEST-FRESHNESS-UNREADABLE compose-id ")]
        self.assertEqual(len(unreadable), 1, lines)
        self.assertIn("https://dl.fedoraproject.org/pub/fedora/linux/releases/44/COMPOSE_ID", unreadable[0])
        self.assertIn("VMTEST-FRESHNESS-REVISION 1789172543", lines)
        self.assertEqual(lines[-1], "VMTEST-FRESHNESS-DONE unreadable=1")

    def test_malformed_repomd_is_unreadable_with_the_parse_error(self):
        _write_fixtures(
            self.fixture_dir,
            **{"dl.fedoraproject.org/pub/fedora/linux/updates/44/Everything/x86_64/repodata/repomd.xml": "<html>503</html>"},
        )
        result = _run(self.fixture_dir, self.manifest_path)
        self.assertNotEqual(result.returncode, 0)
        lines = self.markers(result)
        unreadable = [line for line in lines if line.startswith("VMTEST-FRESHNESS-UNREADABLE revision ")]
        self.assertEqual(len(unreadable), 1, lines)
        self.assertIn("revision", unreadable[0])
        self.assertFalse([line for line in lines if line.startswith("VMTEST-FRESHNESS-REVISION ")])

    def test_unreadable_releases_json_marks_every_artefact_unreadable(self):
        _write_fixtures(self.fixture_dir, **{"fedoraproject.org/releases.json": None})
        result = _run(self.fixture_dir, self.manifest_path)
        self.assertNotEqual(result.returncode, 0)
        lines = self.markers(result)
        self.assertFalse([line for line in lines if line.startswith("VMTEST-FRESHNESS-ARTEFACT ")])
        self.assertTrue([line for line in lines if line.startswith("VMTEST-FRESHNESS-UNREADABLE artefacts ")])

    def test_a_selector_that_matches_nothing_is_unreadable_for_that_base(self):
        # §4.5: a base whose media cannot be located upstream is the fail-fast
        # case, reported per base so the others still get their lines.
        document = json.loads(json.dumps(PROBE_MANIFEST))
        document["vm_test_bases"]["server-fast"]["artefacts"][0]["suffix"] = ".raw.xz"
        self.manifest_path.write_text(json.dumps(document))
        _write_fixtures(self.fixture_dir)
        result = _run(self.fixture_dir, self.manifest_path)
        self.assertNotEqual(result.returncode, 0)
        lines = self.markers(result)
        self.assertTrue([line for line in lines if line.startswith("VMTEST-FRESHNESS-UNREADABLE artefact server-fast-44 ")])
        self.assertTrue([line for line in lines if line.startswith("VMTEST-FRESHNESS-ARTEFACT desktop-44 ")])

    def test_invalid_manifest_is_a_hard_error_before_any_fetch(self):
        self.manifest_path.write_text("{}")
        _write_fixtures(self.fixture_dir)
        result = _run(self.fixture_dir, self.manifest_path)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("manifest", result.stderr)


if __name__ == "__main__":
    unittest.main()

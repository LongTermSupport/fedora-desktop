"""Tests for helpers/vmtest/write_base_record.py — the thin base.json writer.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_write_base_record

The base builder (bash) collects facts and hands them to this executor, which
builds the record through basejson and prints it on stdout. Driven as a
subprocess so the argv contract and stream split are what is tested.
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

SHA_A = "a" * 64
SHA_B = "b" * 64
SHA_C = "c" * 64

BASE_ARGS = [
    "--fedora-version", "44",
    "--profile", "server",
    "--kind", "fast",
    "--name", "server-fast-44",
    "--compose-id", "Fedora-44-20260422.1",
    "--compose-label", "44-1.7",
    "--artefact", f"Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2={SHA_A}",
    "--recipe-digest", SHA_B,
    "--installed-at", "1800000000",
    "--last-upgraded-at", "1800000100",
    "--last-upgraded-revision", "1789172543",
    "--last-upgraded-mirror", "https://mirror.example.net/fedora/linux/updates/44/Everything/x86_64/",
    "--refresh-state", "complete",
    "--base-sha256", SHA_C,
    "--base-size", "5368709120",
    "--base-mtime", "1800000200",
]


def _run(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        [sys.executable, "-m", "helpers.vmtest.write_base_record", *args],
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        check=False,
    )


class TestWriteBaseRecord(unittest.TestCase):
    def test_prints_a_parseable_record_on_stdout(self):
        result = _run(*BASE_ARGS)
        self.assertEqual(result.returncode, 0, result.stderr)
        record = basejson.parse_record(result.stdout)
        self.assertEqual(record.name, "server-fast-44")
        self.assertEqual(record.artefacts[0]["sha256"], SHA_A)
        self.assertEqual(result.stderr, "")

    def test_full_kind_reads_treeinfo_checksums_from_a_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "treeinfo.json"
            path.write_text(json.dumps({"images/install.img": SHA_B}))
            args = list(BASE_ARGS)
            args[args.index("fast")] = "full"
            args[args.index("server-fast-44")] = "server-full-44"
            result = _run(*args, "--treeinfo-checksums", str(path))
        self.assertEqual(result.returncode, 0, result.stderr)
        record = basejson.parse_record(result.stdout)
        self.assertEqual(record.treeinfo_checksums, {"images/install.img": SHA_B})

    def test_full_kind_reads_treeinfo_checksums_from_the_probe_for_one_tree(self):
        probe = (
            "VMTEST-FRESHNESS-TREE Server build_timestamp=1\n"
            f"VMTEST-FRESHNESS-TREE-CHECKSUM Server images/install.img {SHA_B}\n"
            f"VMTEST-FRESHNESS-TREE-CHECKSUM Server images/pxeboot/vmlinuz {SHA_C}\n"
            f"VMTEST-FRESHNESS-TREE-CHECKSUM Everything images/install.img {SHA_A}\n"
        )
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "probe.txt"
            path.write_text(probe)
            args = list(BASE_ARGS)
            args[args.index("fast")] = "full"
            args[args.index("server-fast-44")] = "server-full-44"
            result = _run(*args, "--treeinfo-probe", str(path), "--tree", "Server")
            self.assertEqual(result.returncode, 0, result.stderr)
            record = basejson.parse_record(result.stdout)
            self.assertEqual(record.treeinfo_checksums, {"images/install.img": SHA_B, "images/pxeboot/vmlinuz": SHA_C})
            missing = _run(*args, "--treeinfo-probe", str(path), "--tree", "Workstation")
        self.assertEqual(missing.returncode, 2)
        self.assertEqual(missing.stdout, "")

    def test_invalid_facts_exit_non_zero_with_nothing_on_stdout(self):
        args = list(BASE_ARGS)
        args[args.index("complete")] = "finished"
        result = _run(*args)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("refresh_state", result.stderr)

    def test_malformed_artefact_argument_is_a_usage_error(self):
        args = list(BASE_ARGS)
        args[args.index(f"Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2={SHA_A}")] = "no-equals-sign"
        result = _run(*args)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("NAME=SHA256", result.stderr)


if __name__ == "__main__":
    unittest.main()

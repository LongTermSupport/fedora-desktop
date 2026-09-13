"""Tests for helpers/vmtest/verify_response.py — the HOST-side signature check (§6.6 rule 11).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_verify_response

`vmtest verify <run-id>` is the only place a response signature can be
checked, because the key is host-only. The executor finds the response for a
run id in the spool, verifies it, and prints one marker line.
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

from helpers.vmtest import spool, verdict

KEY = b"k" * 32
OTHER_KEY = b"o" * 32
NAME = "20260913T114500Z-run-scenario-0123456789abcdef.json"
RUN_ID = "20260913T114500Z-server-fast-provision"


class VerifyCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.bridge = self.root / "checkout" / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.key = self.root / "response.key"
        self.key.write_bytes(KEY)
        request = spool.Request(name=NAME, timestamp="20260913T114500Z", verb="run-scenario", argument="server-fast-provision", nonce="0123456789abcdef")
        self.document = verdict.errored(verdict.accepted(request, run_id=RUN_ID, now=1_800_000_000), now=1_800_000_100, stage="ssh", reason="x")

    def write(self, text):
        (self.bridge / "responses" / f"{NAME}.response.json").write_text(text)

    def run_verify(self, run_id=RUN_ID):
        return subprocess.run(
            [sys.executable, "-m", "helpers.vmtest.verify_response", "--checkout", str(self.root / "checkout"), "--key", str(self.key), "--run-id", run_id],
            cwd=REPO_ROOT, capture_output=True, text=True, check=False,
        )


class TestVerify(VerifyCase):
    def test_a_host_signed_response_verifies(self):
        self.write(verdict.sign(self.document, KEY, nonce="0123456789abcdef"))
        result = self.run_verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), f"VMTEST-VERIFY {RUN_ID} signature=ok request={NAME} verdict=error")

    def test_a_response_signed_with_another_key_is_reported_as_not_the_hosts(self):
        self.write(verdict.sign(self.document, OTHER_KEY, nonce="0123456789abcdef"))
        result = self.run_verify()
        self.assertEqual(result.returncode, 1)
        self.assertIn("signature=bad", result.stdout)

    def test_a_tampered_field_is_reported(self):
        signed = json.loads(verdict.sign(self.document, KEY, nonce="0123456789abcdef"))
        signed["verdict"] = "pass"
        self.write(json.dumps(signed))
        result = self.run_verify()
        self.assertEqual(result.returncode, 1)
        self.assertIn("signature=bad", result.stdout)

    def test_a_replayed_nonce_is_reported(self):
        # The signed payload carries the request nonce; a response copied under
        # another request's name does not verify against that name.
        self.write(verdict.sign(self.document, KEY, nonce="ffffffffffffffff"))
        result = self.run_verify()
        self.assertEqual(result.returncode, 1)
        self.assertIn("nonce", result.stdout + result.stderr)

    def test_no_response_for_the_run_id_exits_two(self):
        result = self.run_verify("20260913T114500Z-nothing")
        self.assertEqual(result.returncode, 2)
        self.assertIn("no response", result.stderr)


if __name__ == "__main__":
    unittest.main()

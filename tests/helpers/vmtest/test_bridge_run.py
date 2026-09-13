"""Tests for helpers/vmtest/bridge_run.py — the body of the dispatched run scope.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_bridge_run

The scope moves the accepted response to running (refreshing its heartbeat
while the verb executes), runs the verb through the deployed `vmtest` CLI,
archives the transcript and response into the spool through a pinned
directory fd, writes the signed finished response, and clears the in-flight
lock — on every path, including a crash. Driven as a subprocess with a FAKE
vmtest that writes a run directory and prints the VMTEST-RUN line.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.vmtest import spool, verdict

KEY = b"k" * 32
NOW = 1_800_000_000
NONCE = "0123456789abcdef"
STAMP = "20260913T114500Z"
SCENARIO = "server-fast-provision"
NAME = f"{STAMP}-run-scenario-{NONCE}.json"
RUN_ID = f"{STAMP}-{SCENARIO}"

FAKE_VMTEST = """#!/usr/bin/bash
set -euo pipefail
# fake vmtest: `run <scenario>` writes a run directory and prints the marker;
# `refresh-base <target>` prints what a rebuild prints
if [[ "$1" == refresh-base ]]; then
    printf 'VMTEST-BASE-BUILT server-fast-44 /lab/bases/server-fast-44/base.qcow2 refresh_state=complete\\n'
    [[ "${{FAKE_VERDICT}}" == pass ]] || {{ echo 'ERROR: guest did not answer SSH' >&2; exit 1; }}
    printf 'VMTEST-REFRESH-DONE refreshed=1\\n'
    exit 0
fi
[[ "$1" == run ]] || {{ echo "fake vmtest: unexpected subcommand $1" >&2; exit 64; }}
run_id="{run_id}"
dir="$VMTEST_HOME/runs/$run_id"
mkdir -p "$dir"
printf 'transcript body\\n' > "$dir/transcript.log"
cp "$FAKE_RESPONSE" "$dir/response.json"
sleep "${{FAKE_SLEEP:-0}}"
printf 'VMTEST-RUN %s verdict=%s response=%s\\n' "$run_id" "$FAKE_VERDICT" "$dir/response.json"
[[ "$FAKE_VERDICT" == pass ]]
"""


def judged_response(verdict_name: str = "pass") -> dict:
    document = {
        "schema": 1,
        "request": NAME,
        "verb": "run-scenario",
        "argument": SCENARIO,
        "state": "finished",
        "verdict": verdict_name,
        "run_id": RUN_ID,
        "accepted_at": "2027-01-15T08:00:00Z",
        "started_at": "2027-01-15T08:00:01Z",
        "heartbeat_at": "2027-01-15T08:20:00Z",
        "finished_at": "2027-01-15T08:20:00Z",
        "checks": {"planned": 13, "total": 13, "passed": 13, "failed": 0, "skipped": 0},
        "failure": None,
        "evidence": {"base": {"kind": "server", "name": "server-fast-44"}},
    }
    if verdict_name == "fail":
        document["checks"] = {"planned": 13, "total": 13, "passed": 12, "failed": 1, "skipped": 0}
        document["failure"] = {"stage": "assert", "reason": "1 check failed"}
    return document


class BridgeRunCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.checkout = self.root / "checkout"
        self.bridge = self.checkout / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.config = self.root / "config"
        self.config.mkdir()
        (self.config / "response.key").write_bytes(KEY)
        self.state = self.root / "state"
        self.state.mkdir()
        self.vmtest_home = self.root / "vmtest-home"
        (self.vmtest_home / "runs").mkdir(parents=True)
        (self.vmtest_home / "scenarios.json").write_text(
            json.dumps({"vm_test_scenarios": {SCENARIO: {"description": "d", "planned": 13, "base": "server-fast"}}})
        )
        (self.vmtest_home / "scenarios.allowlist").write_text(f"{SCENARIO}\n")
        self.fake = self.root / "vmtest"
        self.fake.write_text(FAKE_VMTEST.format(run_id=RUN_ID))
        self.fake.chmod(0o755)
        self.fake_response = self.root / "fake-response.json"
        self.fake_response.write_text(json.dumps(judged_response()))

    def plant_accepted(self, verb="run-scenario", argument=SCENARIO, run_id=RUN_ID, planned=13):
        name = f"{STAMP}-{verb}-{NONCE}.json"
        request = spool.Request(name=name, timestamp=STAMP, verb=verb, argument=argument, nonce=NONCE)
        (self.bridge / "processing" / name).write_bytes(b"{}")
        (self.state / "in-flight").write_text(run_id + "\n")
        accepted = verdict.accepted(request, run_id=run_id, now=NOW, planned=planned)
        (self.bridge / "responses" / f"{name}.response.json").write_text(verdict.sign(accepted, KEY, nonce=NONCE))
        return name

    def run_scope(self, name=NAME, verb="run-scenario", argument=SCENARIO, *, fake_verdict="pass", fake_sleep="0", heartbeat="60"):
        argv = [
            sys.executable, "-m", "helpers.vmtest.bridge_run",
            "--checkout", str(self.checkout),
            "--slug", "test-slug",
            "--config-dir", str(self.config),
            "--state-dir", str(self.state),
            "--request", name,
            "--verb", verb,
            "--vmtest", str(self.fake),
            "--heartbeat-seconds", heartbeat,
            "--now", str(NOW + 10),
        ]
        if argument is not None:
            argv += ["--argument", argument]
        return subprocess.run(
            argv,
            cwd=REPO_ROOT,
            env={
                **os.environ,
                "VMTEST_HOME": str(self.vmtest_home),
                "FAKE_RESPONSE": str(self.fake_response),
                "FAKE_VERDICT": fake_verdict,
                "FAKE_SLEEP": fake_sleep,
            },
            capture_output=True,
            text=True,
            check=False,
        )

    def response(self, name=NAME):
        return verdict.verify((self.bridge / "responses" / f"{name}.response.json").read_text(), KEY)

    def service_log(self):
        return (self.state / "service.log").read_text()


class TestRunScenario(BridgeRunCase):
    def test_finished_pass_is_written_signed_with_the_archive(self):
        self.plant_accepted()
        result = self.run_scope()
        self.assertEqual(result.returncode, 0, result.stderr)
        response = self.response()
        self.assertEqual(response["state"], "finished")
        self.assertEqual(response["verdict"], "pass")
        self.assertEqual(response["request"], NAME)
        self.assertEqual(response["run_id"], RUN_ID)
        self.assertEqual(response["checks"]["passed"], 13)
        self.assertEqual(response["evidence"]["transcript"], f"untracked/vmtest-bridge/archive/{RUN_ID}/transcript.log")
        self.assertEqual((self.bridge / "archive" / RUN_ID / "transcript.log").read_text(), "transcript body\n")
        self.assertTrue((self.bridge / "archive" / RUN_ID / "response.json").exists())
        self.assertFalse((self.state / "in-flight").exists())
        self.assertFalse((self.bridge / "processing" / NAME).exists())
        self.assertIn(" finished ", self.service_log())

    def test_failing_run_is_finished_fail_and_exits_non_zero(self):
        self.plant_accepted()
        self.fake_response.write_text(json.dumps(judged_response("fail")))
        result = self.run_scope(fake_verdict="fail")
        self.assertNotEqual(result.returncode, 0)
        response = self.response()
        self.assertEqual(response["verdict"], "fail")
        self.assertEqual(response["failure"]["stage"], "assert")
        self.assertFalse((self.state / "in-flight").exists())

    def test_heartbeat_is_refreshed_while_the_verb_runs(self):
        self.plant_accepted()
        result = self.run_scope(fake_sleep="1", heartbeat="0.2")
        self.assertEqual(result.returncode, 0, result.stderr)
        log = self.service_log()
        self.assertIn(" running ", log)
        self.assertGreaterEqual(log.count(" heartbeat "), 2)

    def test_vmtest_crash_is_a_finished_error_at_the_named_stage(self):
        self.plant_accepted()
        self.fake.write_text("#!/usr/bin/bash\necho '==> waiting for SSH on 127.0.0.1:1 (up to 600s)' >&2\necho 'ERROR: guest did not answer SSH' >&2\nexit 1\n")
        result = self.run_scope()
        self.assertNotEqual(result.returncode, 0)
        response = self.response()
        self.assertEqual(response["state"], "finished")
        self.assertEqual(response["verdict"], "error")
        self.assertEqual(response["failure"]["stage"], "ssh")
        self.assertIn("did not answer SSH", response["failure"]["reason"])
        self.assertFalse((self.state / "in-flight").exists())

    def test_vmtest_crash_before_any_stage_marker_is_a_boot_error(self):
        self.plant_accepted()
        self.fake.write_text("#!/usr/bin/bash\necho 'virt-install: unexpected' >&2\nexit 1\n")
        self.run_scope()
        self.assertEqual(self.response()["failure"]["stage"], "boot")

    def test_symlinked_archive_dir_refuses_but_still_finishes_with_error(self):
        # The archive write is refused (nothing lands outside the spool); the
        # response — written through the responses fd pinned at start — still
        # says why, so the requester is not left waiting.
        self.plant_accepted()
        outside = self.root / "outside"
        outside.mkdir()
        (self.bridge / "archive" / RUN_ID).symlink_to(outside)
        result = self.run_scope()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sorted(p.name for p in outside.iterdir()), [])
        response = self.response()
        self.assertEqual(response["verdict"], "error")
        self.assertEqual(response["failure"]["stage"], "collect")
        self.assertFalse((self.state / "in-flight").exists())

    def test_missing_accepted_stub_is_a_refusal(self):
        self.plant_accepted()
        (self.bridge / "responses" / f"{NAME}.response.json").unlink()
        result = self.run_scope()
        self.assertEqual(result.returncode, 2)
        self.assertFalse((self.state / "in-flight").exists())
        self.assertIn(" refused ", self.service_log())

    def test_tampered_accepted_stub_is_a_refusal(self):
        self.plant_accepted()
        stub = json.loads((self.bridge / "responses" / f"{NAME}.response.json").read_text())
        stub["argument"] = "desktop-fresh-install"
        (self.bridge / "responses" / f"{NAME}.response.json").write_text(json.dumps(stub))
        result = self.run_scope()
        self.assertEqual(result.returncode, 2)

    def test_stub_for_a_different_request_is_a_refusal(self):
        # The argv is the watcher's; the stub must agree with it.
        self.plant_accepted()
        result = self.run_scope(argument="server-main-playbook-fails")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(self.response()["state"], "accepted")


class TestOtherVerbs(BridgeRunCase):
    def test_list_scenarios_finishes_with_the_deployed_manifest_as_evidence(self):
        name = self.plant_accepted(verb="list-scenarios", argument=None, run_id=f"{STAMP}-list-scenarios", planned=None)
        result = self.run_scope(name, "list-scenarios", None)
        self.assertEqual(result.returncode, 0, result.stderr)
        response = self.response(name)
        self.assertEqual(response["state"], "finished")
        self.assertEqual(response["verdict"], "pass")
        self.assertEqual(response["evidence"]["scenarios"][SCENARIO]["planned"], 13)
        self.assertEqual(response["evidence"]["allowlist"], [SCENARIO])
        self.assertFalse((self.state / "in-flight").exists())

    def test_refresh_base_finishes_pass_with_the_bases_it_built(self):
        name = self.plant_accepted(verb="refresh-base", argument="server", run_id=f"{STAMP}-server", planned=None)
        result = self.run_scope(name, "refresh-base", "server")
        self.assertEqual(result.returncode, 0, result.stderr)
        response = self.response(name)
        self.assertEqual(response["verdict"], "pass")
        self.assertEqual(len(response["evidence"]["bases_built"]), 1)
        self.assertIn("server-fast-44", response["evidence"]["bases_built"][0])
        self.assertFalse((self.state / "in-flight").exists())

    def test_refresh_base_that_dies_is_a_finished_error_at_base(self):
        name = self.plant_accepted(verb="refresh-base", argument="server", run_id=f"{STAMP}-server", planned=None)
        result = self.run_scope(name, "refresh-base", "server", fake_verdict="fail")
        self.assertNotEqual(result.returncode, 0)
        response = self.response(name)
        self.assertEqual(response["verdict"], "error")
        self.assertEqual(response["failure"]["stage"], "base")
        self.assertIn("did not answer SSH", response["failure"]["reason"])

    def test_unimplemented_verbs_finish_as_error_not_silence(self):
        for verb, argument in (("lab-status", None), ("abort-run", None)):
            with self.subTest(verb=verb):
                name = self.plant_accepted(verb=verb, argument=argument, run_id=f"{STAMP}-{argument or verb}", planned=None)
                result = self.run_scope(name, verb, argument)
                self.assertNotEqual(result.returncode, 0)
                response = self.response(name)
                self.assertEqual(response["state"], "finished")
                self.assertEqual(response["verdict"], "error")
                self.assertIn("not implemented", response["failure"]["reason"])
                self.assertFalse((self.state / "in-flight").exists())


if __name__ == "__main__":
    unittest.main()

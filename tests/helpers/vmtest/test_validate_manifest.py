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


def _with_host_only() -> dict:
    document = json.loads(json.dumps(MANIFEST))
    document["vm_test_scenarios"]["server-github-token"] = {
        "base": "server-fast",
        "description": "provision with a real scoped PAT and a passphrase-protected key",
        "planned": 9,
        "max_skipped": 0,
        "host_only": True,
    }
    return document


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
        self.assertIn("VMTEST-MANIFEST-OK scenarios=3 runnable=2 bridge=2 host_only=0 bases=3", result.stdout)
        self.assertEqual(result.stderr, "")

    def test_allowlist_flag_prints_the_allowlist_only(self):
        result = _run(json.dumps(MANIFEST), "--allowlist")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "server-fast-provision\nserver-full-provision\n")

    def test_base_flag_prints_that_bases_facts_as_one_marker(self):
        result = _run(json.dumps(MANIFEST), "--base", "desktop")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout,
            "VMTEST-BASE key=desktop name=desktop-44 kind=full profile=desktop tree=Everything vcpus=4 ram_mib=8192\n",
        )
        fast = _run(json.dumps(MANIFEST), "--base", "server-fast")
        self.assertIn("kind=fast profile=server tree=- vcpus=2", fast.stdout)

    def test_scenario_flag_prints_that_scenarios_facts_as_one_marker(self):
        result = _run(json.dumps(MANIFEST), "--scenario", "server-fast-provision")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stdout,
            "VMTEST-SCENARIO id=server-fast-provision base=server-fast base_name=server-fast-44 "
            "profile=server planned=12 max_skipped=0 runnable=true host_only=false "
            "reboot_before_checks=false run_env=-\n",
        )
        unplanned = _run(json.dumps(MANIFEST), "--scenario", "desktop-fresh-install")
        self.assertIn(
            "planned=- max_skipped=0 runnable=false host_only=false reboot_before_checks=true run_env=-",
            unplanned.stdout,
        )

    def test_scenario_flag_reports_reboot_before_checks(self):
        # Printed for every scenario, like host_only and for the same reason: a CLI
        # that only ever saw the field on scenarios that reboot would fall back to
        # its own idea of when a reboot is needed, which is the profile-shaped
        # guess this field replaces.
        document = json.loads(json.dumps(MANIFEST))
        document["vm_test_scenarios"]["server-fast-provision"]["reboot_before_checks"] = True
        result = _run(json.dumps(document), "--scenario", "server-fast-provision")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("host_only=false reboot_before_checks=true", result.stdout)

    def test_scenario_flag_reports_host_only(self):
        # The `vmtest` CLI reads this field to decide which enumeration a run must
        # be in. It is printed for every scenario, not only the host-only ones, so
        # a CLI that never sees `host_only=true` still fails on a marker it cannot
        # parse rather than defaulting a credential-bearing run to "ordinary".
        result = _run(json.dumps(_with_host_only()), "--scenario", "server-github-token")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("runnable=true host_only=true", result.stdout)

    def test_allowlist_flag_omits_a_host_only_scenario(self):
        result = _run(json.dumps(_with_host_only()), "--allowlist")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "server-fast-provision\nserver-full-provision\n")

    def test_host_only_flag_prints_the_host_only_list_only(self):
        result = _run(json.dumps(_with_host_only()), "--host-only")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "server-github-token\n")

    def test_ok_marker_counts_bridge_and_host_only_separately(self):
        # The playbook gates the two deployed lists on these numbers. Reporting
        # only `runnable` would make it subtract, and a lab whose one runnable
        # scenario is host-only would try to render an allowlist the validator
        # refuses to produce.
        result = _run(json.dumps(_with_host_only()))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("scenarios=4 runnable=3 bridge=2 host_only=1 bases=3", result.stdout)

    def test_host_only_flag_prints_nothing_when_there_is_none(self):
        # An empty host-only list is the ordinary case, so it exits 0 with no
        # output — unlike an empty bridge allowlist, which is a manifest error.
        result = _run(json.dumps(MANIFEST), "--host-only")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")

    def test_scenario_flag_carries_run_env_as_comma_separated_pairs(self):
        document = json.loads(json.dumps(MANIFEST))
        document["vm_test_scenarios"]["server-fast-provision"]["run_env"] = {
            "RUN_BASH_OPTIONAL_PLAYBOOKS": "play-nvidia.yml",
            "RUN_BASH_PROVISIONING_PROFILE": "server",
        }
        result = _run(json.dumps(document), "--scenario", "server-fast-provision")
        self.assertIn(
            "run_env=RUN_BASH_OPTIONAL_PLAYBOOKS=play-nvidia.yml,RUN_BASH_PROVISIONING_PROFILE=server",
            result.stdout,
        )

    def test_scenario_flag_with_an_unknown_id_fails_closed(self):
        result = _run(json.dumps(MANIFEST), "--scenario", "server-medium-provision")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("VMTEST-MANIFEST-INVALID", result.stdout)

    def test_base_flag_with_an_unknown_key_fails_closed(self):
        result = _run(json.dumps(MANIFEST), "--base", "server-medium")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("VMTEST-MANIFEST-INVALID", result.stdout)
        self.assertIn("server-medium", result.stderr)

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

"""Unit tests for helpers/vmtest/scenarios.py — manifest parsing and check accounting.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_scenarios

Plan 00110 DESIGN.md §3.5 (each scenario names its base), §6.2 (the argument
grammar and the deny list), §6.6 rule 03 (the pass rule). The parser takes an
already-decoded mapping — the JSON shape Ansible renders from
vars/vm-test-scenarios.yml — so the tests need no YAML library. The tracked
YAML itself is validated by scripts/qa-vmtest-manifest.bash.
"""

from __future__ import annotations

import copy
import itertools
import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import scenarios

FEDORA_VERSION = 44

MANIFEST = {
    "vm_test_ttl_upgrade_days": 7,
    "vm_test_ttl_rebuild_days": 90,
    "vm_test_bases": {
        "server-fast": {
            "kind": "fast",
            "profile": "server",
            "tree": None,
            "artefacts": [
                {"variant": "Cloud", "subvariant": "Cloud_Base", "prefix": "Fedora-Cloud-Base-Generic-", "suffix": ".qcow2"},
            ],
            "vcpus": 2,
            "ram_mib": 4096,
        },
        "server-full": {
            "kind": "full",
            "profile": "server",
            "tree": "Server",
            "artefacts": [
                {"variant": "Server", "subvariant": "Server", "prefix": "Fedora-Server-netinst-", "suffix": ".iso"},
            ],
            "vcpus": 2,
            "ram_mib": 4096,
        },
        "desktop": {
            "kind": "full",
            "profile": "desktop",
            "tree": "Everything",
            "artefacts": [
                {"variant": "Everything", "subvariant": "Everything", "prefix": "Fedora-Everything-netinst-", "suffix": ".iso"},
                {"variant": "Workstation", "subvariant": "Workstation", "prefix": "Fedora-Workstation-Live-", "suffix": ".iso"},
            ],
            "vcpus": 4,
            "ram_mib": 8192,
        },
    },
    "vm_test_scenarios": {
        "server-fast-provision": {
            "base": "server-fast",
            "description": "provision a Fedora Cloud Base guest with run.bash",
            "planned": 12,
            "max_skipped": 0,
        },
        "server-full-provision": {
            "base": "server-full",
            "description": "provision an Anaconda-installed Fedora Server guest",
            "planned": 12,
            "max_skipped": 0,
        },
        "desktop-fresh-install": {
            "base": "desktop",
            "description": "the repo's own installer shape plus desktop provisioning",
            "planned": None,
            "max_skipped": 0,
        },
    },
}


def _parse(document=None):
    return scenarios.parse_manifest(document if document is not None else MANIFEST, FEDORA_VERSION)


class TestParseManifest(unittest.TestCase):
    def test_ttls_are_converted_to_seconds(self):
        manifest = _parse()
        self.assertEqual(manifest.ttl_upgrade_seconds, 7 * 86400)
        self.assertEqual(manifest.ttl_rebuild_seconds, 90 * 86400)

    def test_base_names_carry_the_fedora_version(self):
        # §3.5 names bases `server-fast-44`; the manifest key is the version-free
        # part so the version is not duplicated from vars/fedora-version.yml.
        manifest = _parse()
        self.assertEqual(manifest.bases["server-fast"].name, "server-fast-44")
        self.assertEqual(manifest.bases["desktop"].name, "desktop-44")

    def test_base_kind_and_profile_are_carried(self):
        base = _parse().bases["server-full"]
        self.assertEqual(base.kind, "full")
        self.assertEqual(base.profile, "server")
        self.assertEqual(base.vcpus, 2)
        self.assertEqual(base.ram_mib, 4096)

    def test_base_names_its_one_tree_and_its_artefacts(self):
        # §4.3: each base names exactly ONE install tree, and the desktop base
        # hashes BOTH the netinst it boots and the Live squashfs it installs.
        manifest = _parse()
        self.assertIsNone(manifest.bases["server-fast"].tree)
        self.assertEqual(manifest.bases["server-full"].tree, "Server")
        self.assertEqual(manifest.bases["desktop"].tree, "Everything")
        self.assertEqual(len(manifest.bases["desktop"].artefacts), 2)
        selector = manifest.bases["server-fast"].artefacts[0]
        self.assertEqual(
            (selector.variant, selector.subvariant, selector.prefix, selector.suffix),
            ("Cloud", "Cloud_Base", "Fedora-Cloud-Base-Generic-", ".qcow2"),
        )

    def test_scenario_resolves_its_base_by_name(self):
        scenario = _parse().scenarios["server-full-provision"]
        self.assertEqual(scenario.base.name, "server-full-44")
        self.assertEqual(scenario.profile, "server")
        self.assertEqual(scenario.planned, 12)
        self.assertEqual(scenario.max_skipped, 0)

    def test_run_env_defaults_to_empty_and_is_carried_when_declared(self):
        # The negative scenarios (T3.4) differ from the positive one only by the
        # RUN_BASH_* values the host hands run.bash; everything else is shared.
        self.assertEqual(_parse().scenarios["server-fast-provision"].run_env, {})
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"]["server-main-playbook-fails"] = {
            "base": "server-fast",
            "description": "an unrecognised profile fails play 1 and must propagate out of run.bash",
            "planned": 12,
            "max_skipped": 0,
            "run_env": {"RUN_BASH_PROVISIONING_PROFILE": "not-a-profile"},
        }
        scenario = scenarios.parse_manifest(document, FEDORA_VERSION).scenarios["server-main-playbook-fails"]
        self.assertEqual(scenario.run_env, {"RUN_BASH_PROVISIONING_PROFILE": "not-a-profile"})

    def test_scenario_without_a_planned_count_is_not_runnable(self):
        # A scenario whose guest script has not yet declared its check count
        # cannot be run: `checks.planned` would be a guess and rule 02 could not
        # catch a harness that died early. It is listed, and refused.
        manifest = _parse()
        self.assertFalse(manifest.scenarios["desktop-fresh-install"].runnable)
        self.assertTrue(manifest.scenarios["server-fast-provision"].runnable)

    def test_json_round_trip_parses_identically(self):
        # The deployed form is JSON rendered by Ansible; the same document must
        # parse to the same manifest whether it arrived as a mapping or as text.
        self.assertEqual(scenarios.load_manifest(json.dumps(MANIFEST), FEDORA_VERSION), _parse())


class TestAllowlist(unittest.TestCase):
    def test_lists_only_runnable_scenarios_sorted(self):
        self.assertEqual(
            scenarios.allowlist(_parse()),
            ("server-fast-provision", "server-full-provision"),
        )

    def test_allowlist_text_is_one_id_per_line_with_trailing_newline(self):
        text = scenarios.allowlist_text(_parse())
        self.assertEqual(text, "server-fast-provision\nserver-full-provision\n")

    def test_empty_allowlist_is_an_error(self):
        # A deployed allowlist with nothing in it would make every run-scenario
        # request fail closed — correct — but silently, as if the lab had no
        # scenarios. Producing it is the mistake to catch.
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"]["server-fast-provision"]["planned"] = None
        document["vm_test_scenarios"]["server-full-provision"]["planned"] = None
        manifest = scenarios.parse_manifest(document, FEDORA_VERSION)
        with self.assertRaises(scenarios.ManifestError):
            scenarios.allowlist_text(manifest)


class TestHostOnlyScenario(unittest.TestCase):
    """A scenario that handles a real credential is runnable but NOT enumerated to the sandbox.

    Plan 00110 DESIGN.md:2084 — "It is not in the bridge's argument enumeration,
    because a sandboxed agent asking the host to put a PAT into a VM is the precise
    shape the bridge exists to prevent." Before this flag, `runnable` meant both
    "the host may run it" and "the sandbox may ask for it", so declaring the check
    count a scenario needs in order to run at all would have enumerated it.
    """

    @staticmethod
    def _with_host_only(**overrides):
        document = copy.deepcopy(MANIFEST)
        entry = {
            "base": "server-fast",
            "description": "provision with a real scoped PAT and a passphrase-protected key",
            "planned": 9,
            "max_skipped": 0,
            "host_only": True,
        }
        entry.update(overrides)
        document["vm_test_scenarios"]["server-github-token"] = entry
        return document

    def test_host_only_defaults_to_false(self):
        self.assertFalse(_parse().scenarios["server-fast-provision"].host_only)

    def test_a_host_only_scenario_is_runnable(self):
        # It must keep a declared check count, or rule 02 could not catch a
        # harness that died early — which is exactly the run where a credential
        # was in play and the evidence matters most.
        scenario = scenarios.parse_manifest(self._with_host_only(), FEDORA_VERSION).scenarios["server-github-token"]
        self.assertTrue(scenario.runnable)
        self.assertTrue(scenario.host_only)

    def test_a_host_only_scenario_is_absent_from_the_bridge_allowlist(self):
        manifest = scenarios.parse_manifest(self._with_host_only(), FEDORA_VERSION)
        self.assertNotIn("server-github-token", scenarios.allowlist(manifest))
        self.assertNotIn("server-github-token", scenarios.allowlist_text(manifest))

    def test_the_two_enumerations_are_disjoint(self):
        # Derived from one flag rather than maintained as two lists, so a scenario
        # cannot drift onto both. If it could, the bridge enumeration would be the
        # thing keeping a PAT run off the shared mount while also permitting it.
        manifest = scenarios.parse_manifest(self._with_host_only(), FEDORA_VERSION)
        bridge = set(scenarios.allowlist(manifest))
        host = set(scenarios.host_only_list(manifest))
        self.assertEqual(bridge & host, set())
        self.assertEqual(host, {"server-github-token"})

    def test_host_only_list_holds_only_runnable_scenarios_sorted(self):
        document = self._with_host_only()
        document["vm_test_scenarios"]["server-github-token-unplanned"] = {
            "base": "server-fast",
            "description": "a host-only scenario whose guest script has not declared its count",
            "planned": None,
            "max_skipped": 0,
            "host_only": True,
        }
        manifest = scenarios.parse_manifest(document, FEDORA_VERSION)
        self.assertEqual(scenarios.host_only_list(manifest), ("server-github-token",))

    def test_host_only_text_is_one_id_per_line_with_trailing_newline(self):
        manifest = scenarios.parse_manifest(self._with_host_only(), FEDORA_VERSION)
        self.assertEqual(scenarios.host_only_text(manifest), "server-github-token\n")

    def test_no_host_only_scenario_yields_empty_text_not_an_error(self):
        # Unlike the bridge allowlist, nothing is wrong with a lab that has no
        # host-only scenario — that is the ordinary case and the safer one.
        self.assertEqual(scenarios.host_only_text(_parse()), "")

    def test_host_only_must_be_a_bool(self):
        # A truthy string would read as "yes" here and as a parse error nowhere,
        # so `host_only: "false"` would enumerate a PAT scenario to the sandbox.
        for value in ("true", "false", 1, 0, None):
            with self.subTest(value=value):
                with self.assertRaises(scenarios.ManifestError) as caught:
                    scenarios.parse_manifest(self._with_host_only(host_only=value), FEDORA_VERSION)
                self.assertIn("host_only", str(caught.exception))

    def test_a_manifest_of_only_host_only_scenarios_has_no_bridge_allowlist(self):
        # Fails closed and says so, rather than deploying an empty file that
        # looks like a lab with no scenarios at all.
        document = copy.deepcopy(MANIFEST)
        for scenario in document["vm_test_scenarios"].values():
            scenario["host_only"] = True
        manifest = scenarios.parse_manifest(document, FEDORA_VERSION)
        with self.assertRaises(scenarios.ManifestError):
            scenarios.allowlist_text(manifest)


class TestManifestRejections(unittest.TestCase):
    """Every malformed manifest raises ManifestError naming the offending entry."""

    def assert_rejected(self, document, *fragments):
        with self.assertRaises(scenarios.ManifestError) as caught:
            scenarios.parse_manifest(document, FEDORA_VERSION)
        for fragment in fragments:
            self.assertIn(fragment, str(caught.exception))

    def test_non_mapping_document(self):
        self.assert_rejected([], "mapping")

    def test_missing_top_level_keys(self):
        for key in ("vm_test_ttl_upgrade_days", "vm_test_ttl_rebuild_days", "vm_test_bases", "vm_test_scenarios"):
            with self.subTest(key=key):
                document = copy.deepcopy(MANIFEST)
                del document[key]
                self.assert_rejected(document, key)

    def test_unknown_top_level_key_is_rejected(self):
        # A misspelt key would otherwise be ignored and its intended value
        # silently replaced by nothing.
        document = copy.deepcopy(MANIFEST)
        document["vm_test_ttl_upgrade_dayz"] = 3
        self.assert_rejected(document, "vm_test_ttl_upgrade_dayz")

    def test_non_positive_ttl(self):
        for key, value in itertools.product(("vm_test_ttl_upgrade_days", "vm_test_ttl_rebuild_days"), (0, -1, "7")):
            with self.subTest(key=key, value=value):
                document = copy.deepcopy(MANIFEST)
                document[key] = value
                self.assert_rejected(document, key)

    def test_rebuild_ttl_must_exceed_upgrade_ttl(self):
        # A rebuild backstop shorter than the refresh backstop would make every
        # TTL-U refresh a reinstall, which is the expensive path for no reason.
        document = copy.deepcopy(MANIFEST)
        document["vm_test_ttl_rebuild_days"] = 7
        self.assert_rejected(document, "vm_test_ttl_rebuild_days")

    def test_scenario_naming_an_unknown_base(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"]["server-fast-provision"]["base"] = "server-medium"
        self.assert_rejected(document, "server-fast-provision", "server-medium")

    def test_scenario_and_base_profiles_are_bound_through_the_base(self):
        # A scenario has no profile of its own: it inherits the base's, so it
        # cannot claim `desktop` while running on a server base.
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"]["server-fast-provision"]["profile"] = "desktop"
        self.assert_rejected(document, "server-fast-provision", "profile")

    def test_unknown_base_kind(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_bases"]["server-fast"]["kind"] = "medium"
        self.assert_rejected(document, "server-fast", "medium")

    def test_unknown_profile(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_bases"]["server-fast"]["profile"] = "laptop"
        self.assert_rejected(document, "server-fast", "laptop")

    def test_full_base_must_name_a_tree(self):
        # A `full` base is an Anaconda install; without a tree there are no
        # installer hashes and the reinstall trigger would be vacuous.
        for value in (None, "", 7):
            with self.subTest(tree=value):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_bases"]["server-full"]["tree"] = value
                self.assert_rejected(document, "server-full", "tree")

    def test_fast_base_must_not_name_a_tree(self):
        # A `fast` base never runs Anaconda; a tree here would bind its identity
        # to media it was not built from.
        document = copy.deepcopy(MANIFEST)
        document["vm_test_bases"]["server-fast"]["tree"] = "Server"
        self.assert_rejected(document, "server-fast", "tree")

    def test_tree_must_be_a_variant_name(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_bases"]["server-full"]["tree"] = "../rawhide"
        self.assert_rejected(document, "server-full", "tree")

    def test_base_needs_at_least_one_artefact(self):
        for value in ([], None, "Fedora-Server-netinst"):
            with self.subTest(artefacts=value):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_bases"]["server-fast"]["artefacts"] = value
                self.assert_rejected(document, "server-fast", "artefacts")

    def test_artefact_selector_fields_are_exact_and_non_empty(self):
        for key in ("variant", "subvariant", "prefix", "suffix"):
            with self.subTest(missing=key):
                document = copy.deepcopy(MANIFEST)
                del document["vm_test_bases"]["server-fast"]["artefacts"][0][key]
                self.assert_rejected(document, "server-fast", key)
            with self.subTest(empty=key):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_bases"]["server-fast"]["artefacts"][0][key] = ""
                self.assert_rejected(document, "server-fast", key)
        document = copy.deepcopy(MANIFEST)
        document["vm_test_bases"]["server-fast"]["artefacts"][0]["arch"] = "x86_64"
        self.assert_rejected(document, "server-fast", "arch")

    def test_duplicate_artefact_selector_is_rejected(self):
        document = copy.deepcopy(MANIFEST)
        artefacts = document["vm_test_bases"]["desktop"]["artefacts"]
        artefacts.append(dict(artefacts[0]))
        self.assert_rejected(document, "desktop", "twice")

    def test_base_sizing_must_be_positive_integers(self):
        for key, value in itertools.product(("vcpus", "ram_mib"), (0, -1, "2", 2.5, None)):
            with self.subTest(key=key, value=value):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_bases"]["desktop"][key] = value
                self.assert_rejected(document, "desktop", key)

    def test_missing_scenario_field(self):
        for key in ("base", "description", "planned", "max_skipped"):
            with self.subTest(key=key):
                document = copy.deepcopy(MANIFEST)
                del document["vm_test_scenarios"]["server-fast-provision"][key]
                self.assert_rejected(document, "server-fast-provision", key)

    def test_unknown_scenario_field(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"]["server-fast-provision"]["max_skiped"] = 1
        self.assert_rejected(document, "server-fast-provision", "max_skiped")

    def test_planned_must_be_a_positive_integer_or_null(self):
        for value in (0, -3, "12", 1.5, True):
            with self.subTest(value=value):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_scenarios"]["server-fast-provision"]["planned"] = value
                self.assert_rejected(document, "server-fast-provision", "planned")

    def test_max_skipped_must_leave_at_least_one_check(self):
        # §6.6 rule 03: `passed >= 1`. A cap equal to `planned` would let a run
        # skip everything and still be within its cap, so the cap must leave
        # room for the one check that has to pass.
        for value in (12, 13, -1, "0", None):
            with self.subTest(value=value):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_scenarios"]["server-fast-provision"]["max_skipped"] = value
                self.assert_rejected(document, "server-fast-provision", "max_skipped")

    def test_run_env_keys_are_a_closed_allowlist(self):
        # A scenario may steer run.bash's non-secret knobs and nothing else: no
        # secret-bearing variable, no arbitrary environment, no misspelling.
        for key in ("RUN_BASH_VAULT_PASSWORD", "RUN_BASH_GITHUB_TOKEN_FILE", "PATH", "RUN_BASH_PROVISIONING_PROFIL", "run_bash_reboot"):
            with self.subTest(key=key):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_scenarios"]["server-fast-provision"]["run_env"] = {key: "x"}
                self.assert_rejected(document, "server-fast-provision", key)

    def test_run_env_values_are_plain_tokens(self):
        for value in ("", "a b", "x;rm -rf /", "$(id)", "play-a.yml\nplay-b.yml", 3):
            with self.subTest(value=value):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_scenarios"]["server-fast-provision"]["run_env"] = {"RUN_BASH_OPTIONAL_PLAYBOOKS": value}
                self.assert_rejected(document, "server-fast-provision", "RUN_BASH_OPTIONAL_PLAYBOOKS")

    def test_run_env_must_be_a_mapping(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"]["server-fast-provision"]["run_env"] = ["RUN_BASH_REBOOT=1"]
        self.assert_rejected(document, "server-fast-provision", "run_env")

    def test_empty_description(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"]["server-fast-provision"]["description"] = "  "
        self.assert_rejected(document, "server-fast-provision", "description")

    def test_scenario_id_must_match_the_argument_grammar(self):
        # §6.2: `^[a-z][a-z0-9_-]*$`. An id that fails the grammar could never be
        # requested, so listing it is a manifest error rather than a dead entry.
        for bad in ("Server-Fast", "1server", "server fast", "server.fast", "", "-server"):
            with self.subTest(id=bad):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_scenarios"][bad] = document["vm_test_scenarios"].pop("server-fast-provision")
                self.assert_rejected(document, repr(bad))

    def test_scenario_id_on_the_deny_list_is_rejected(self):
        # The deny list is checked before the allowlist by the bridge (§6.4 step
        # 4), so a scenario with such an id could never run. Every word on the
        # list is tried, so the list and the parser cannot drift apart.
        for denied in scenarios.DENY_LIST:
            with self.subTest(id=denied):
                document = copy.deepcopy(MANIFEST)
                document["vm_test_scenarios"][denied] = document["vm_test_scenarios"].pop("server-fast-provision")
                self.assert_rejected(document, denied, "deny")

    def test_deny_list_covers_every_exec_shaped_word_the_design_names(self):
        for word in ("exec", "shell", "sh", "bash", "run", "eval", "system", "ansible", "ansible-playbook"):
            self.assertIn(word, scenarios.DENY_LIST)

    def test_base_key_must_match_the_argument_grammar(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_bases"]["Server Fast"] = document["vm_test_bases"].pop("server-fast")
        document["vm_test_scenarios"]["server-fast-provision"]["base"] = "Server Fast"
        self.assert_rejected(document, "'Server Fast'")

    def test_no_scenarios_at_all(self):
        document = copy.deepcopy(MANIFEST)
        document["vm_test_scenarios"] = {}
        self.assert_rejected(document, "vm_test_scenarios")

    def test_base_nobody_uses_is_rejected(self):
        # An orphan base would be built, refreshed and kept on disk for nothing.
        document = copy.deepcopy(MANIFEST)
        document["vm_test_bases"]["spare"] = dict(document["vm_test_bases"]["server-fast"])
        self.assert_rejected(document, "spare")

    def test_load_manifest_rejects_invalid_json(self):
        with self.assertRaises(scenarios.ManifestError):
            scenarios.load_manifest("{not json", FEDORA_VERSION)


class TestJudgeChecks(unittest.TestCase):
    """§6.6 rule 03: pass requires ALL of failed == 0, passed + skipped == total,
    total == planned, passed >= 1, skipped <= max_skipped."""

    PLANNED = 27
    MAX_SKIPPED = 2

    def judge(self, **counts):
        fields = {"planned": self.PLANNED, "total": 27, "passed": 27, "failed": 0, "skipped": 0}
        fields.update(counts)
        return scenarios.judge_checks(max_skipped=self.MAX_SKIPPED, **fields)

    def test_all_passed_is_pass(self):
        self.assertEqual(self.judge().verdict, "pass")

    def test_skipped_within_cap_is_pass_and_names_the_skips(self):
        judgement = self.judge(passed=25, skipped=2)
        self.assertEqual(judgement.verdict, "pass")
        self.assertIn("2 skipped", judgement.reason)

    def test_any_failure_is_fail(self):
        self.assertEqual(self.judge(passed=26, failed=1).verdict, "fail")

    def test_failure_outranks_every_error_condition(self):
        # A run that failed an assertion AND died early is still a product
        # failure: the assertion did not hold. `fail` is the more informative
        # verdict and must not be masked by `error`.
        self.assertEqual(self.judge(total=4, passed=3, failed=1).verdict, "fail")

    def test_harness_that_died_early_is_error_not_pass(self):
        # Rule 02: 4 of 27 checks ran, all green. This is the case the whole
        # contract exists for.
        judgement = self.judge(total=4, passed=4)
        self.assertEqual(judgement.verdict, "error")
        self.assertIn("planned 27", judgement.reason)
        self.assertIn("total 4", judgement.reason)

    def test_more_checks_than_planned_is_error(self):
        self.assertEqual(self.judge(total=28, passed=28).verdict, "error")

    def test_all_skipped_is_error(self):
        # Rule 03's whole reason to exist: passed + skipped == total holds here.
        judgement = self.judge(passed=0, skipped=27)
        self.assertEqual(judgement.verdict, "error")
        self.assertIn("passed 0", judgement.reason)

    def test_skipped_over_the_cap_is_error(self):
        judgement = self.judge(passed=24, skipped=3)
        self.assertEqual(judgement.verdict, "error")
        self.assertIn("max_skipped 2", judgement.reason)

    def test_counters_that_do_not_add_up_are_error(self):
        self.assertEqual(self.judge(passed=20, skipped=0).verdict, "error")

    def test_unfinished_counters_are_error(self):
        # The response carries null counters until the run finishes (§6.6).
        for field in ("total", "passed", "failed", "skipped"):
            with self.subTest(field=field):
                self.assertEqual(self.judge(**{field: None}).verdict, "error")

    def test_negative_counter_is_error(self):
        self.assertEqual(self.judge(passed=28, failed=-1).verdict, "error")

    def test_verdicts_are_the_three_named_values(self):
        self.assertEqual(scenarios.VERDICTS, frozenset({"pass", "fail", "error"}))

    def test_exhaustive_small_space_never_returns_pass_unless_the_rule_holds(self):
        # Derive the pass rule independently over a small count space and
        # compare every cell, so no single cell can be forgotten.
        planned, max_skipped = 3, 1
        counts = range(0, 5)
        for total, passed, failed, skipped in itertools.product(counts, counts, counts, counts):
            with self.subTest(total=total, passed=passed, failed=failed, skipped=skipped):
                verdict = scenarios.judge_checks(
                    planned=planned, total=total, passed=passed, failed=failed,
                    skipped=skipped, max_skipped=max_skipped,
                ).verdict
                rule_holds = (
                    failed == 0
                    and passed + skipped == total
                    and total == planned
                    and passed >= 1
                    and skipped <= max_skipped
                )
                if rule_holds:
                    self.assertEqual(verdict, "pass")
                elif failed > 0:
                    self.assertEqual(verdict, "fail")
                else:
                    self.assertEqual(verdict, "error")


if __name__ == "__main__":
    unittest.main()

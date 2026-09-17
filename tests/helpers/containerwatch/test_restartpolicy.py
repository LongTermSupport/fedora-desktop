"""Auditing restart policies BEFORE they storm (Plan 00132 Phase 6).

Containment is reactive: it acts once a container is already restarting hard
enough to threaten the host. This module is the preventive half — it identifies
the configuration that MAKES an unbounded storm possible, so it can be fixed
while nothing is on fire.

The facts are from `podman-run(1)`, verified during the incident research:

  - `on-failure[:max_retries]` is the ONLY policy that accepts a cap.
  - `always` and `unless-stopped` retry indefinitely, and `MaximumRetryCount` is
    not consulted for them at all — a container can carry a retry count that
    reads like a limit and is ignored.
  - Podman applies NO backoff, which is why the observed storm sustained roughly
    2.4 restarts per second rather than decaying.

That last point is why "it will settle down on its own" is not true here.
"""

import unittest

from helpers.containerwatch import restartpolicy


class ClassificationTests(unittest.TestCase):
    def test_always_is_uncapped(self):
        self.assertEqual(restartpolicy.classify("always", 0), "uncapped")

    def test_unless_stopped_is_uncapped(self):
        self.assertEqual(restartpolicy.classify("unless-stopped", 0), "uncapped")

    def test_always_with_a_retry_count_is_still_uncapped(self):
        # The count is not consulted for `always`. Reading it as a limit is the
        # trap this check exists to expose: the config LOOKS bounded.
        self.assertEqual(restartpolicy.classify("always", 5), "uncapped")

    def test_on_failure_with_a_count_is_capped(self):
        self.assertEqual(restartpolicy.classify("on-failure", 5), "capped")

    def test_on_failure_without_a_count_is_uncapped(self):
        # `on-failure` alone retries indefinitely; only `on-failure:N` bounds it.
        self.assertEqual(restartpolicy.classify("on-failure", 0), "uncapped")

    def test_no_is_not_a_restart_policy_at_all(self):
        self.assertEqual(restartpolicy.classify("no", 0), "none")

    def test_never_is_the_podman_spelling_of_no(self):
        self.assertEqual(restartpolicy.classify("never", 0), "none")

    def test_an_empty_policy_is_none(self):
        self.assertEqual(restartpolicy.classify("", 0), "none")

    def test_an_unrecognised_policy_is_reported_as_unknown(self):
        # Not silently treated as safe: a policy we cannot classify is a policy
        # we cannot vouch for.
        self.assertEqual(restartpolicy.classify("banana", 0), "unknown")


class ParsingTests(unittest.TestCase):
    def test_a_well_formed_line_is_parsed(self):
        rows = restartpolicy.parse_lines("id-a\tc-a\talways\t0\n")
        self.assertEqual(
            rows, [{"container_id": "id-a", "container_name": "c-a", "policy": "always", "max_retries": 0}]
        )

    def test_a_leading_slash_is_stripped_from_the_name(self):
        rows = restartpolicy.parse_lines("id-a\t/c-a\talways\t0\n")
        self.assertEqual(rows[0]["container_name"], "c-a")

    def test_a_non_numeric_retry_count_reads_as_zero(self):
        # Docker omits the field in some versions; absent means "no cap set",
        # which is what zero denotes here.
        rows = restartpolicy.parse_lines("id-a\tc-a\ton-failure\t<no value>\n")
        self.assertEqual(rows[0]["max_retries"], 0)

    def test_a_malformed_line_is_dropped(self):
        self.assertEqual(restartpolicy.parse_lines("garbage\n"), [])

    def test_empty_input_yields_nothing(self):
        self.assertEqual(restartpolicy.parse_lines(""), [])


class AuditTests(unittest.TestCase):
    def _rows(self):
        return [
            {"container_id": "id-a", "container_name": "c-a", "policy": "always", "max_retries": 0},
            {"container_id": "id-b", "container_name": "c-b", "policy": "on-failure", "max_retries": 5},
            {"container_id": "id-c", "container_name": "c-c", "policy": "no", "max_retries": 0},
        ]

    def test_only_uncapped_containers_are_reported(self):
        findings = restartpolicy.audit(self._rows(), engine="podman")
        self.assertEqual([f["container_name"] for f in findings], ["c-a"])

    def test_a_finding_carries_the_remedy_not_just_the_complaint(self):
        finding = restartpolicy.audit(self._rows(), engine="podman")[0]
        self.assertIn("on-failure", finding["advice"])

    def test_the_finding_is_marked_as_its_own_kind(self):
        finding = restartpolicy.audit(self._rows(), engine="podman")[0]
        self.assertEqual(finding["kind"], "restart-policy")

    def test_an_unknown_policy_is_surfaced_rather_than_passed(self):
        rows = [{"container_id": "id-x", "container_name": "c-x", "policy": "banana", "max_retries": 0}]
        findings = restartpolicy.audit(rows, engine="podman")
        self.assertEqual(len(findings), 1)
        self.assertIn("unknown", findings[0]["classification"])

    def test_a_clean_host_produces_no_findings(self):
        rows = [{"container_id": "id-b", "container_name": "c-b", "policy": "on-failure", "max_retries": 5}]
        self.assertEqual(restartpolicy.audit(rows, engine="podman"), [])

    def test_the_advice_names_the_container_so_it_can_be_acted_on(self):
        finding = restartpolicy.audit(self._rows(), engine="podman")[0]
        self.assertIn("c-a", finding["advice"])


if __name__ == "__main__":
    unittest.main()

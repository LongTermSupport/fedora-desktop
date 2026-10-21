"""Plan 00209 Task 1.4: tests for the host checks' pure parts (no homeserver needed).

Run from this folder: `python3 -m unittest test_hca`. The parts that talk to Tuwunel are
proved only by running acceptance-hca.bash on the host.
"""

from __future__ import annotations

import pathlib
import tomllib
import unittest

import hca_element
import hca_probe
import hca_server

HUMAN = "@owner:hca.teamchat.internal"
AGENT = "@hca.1+lab.host:hca.teamchat.internal"

PIN = """# a comment
tuwunel_version: v1.9.4
tuwunel_sha256_x86_64: aa
tuwunel_sha256_aarch64: bb
"""


class PinTest(unittest.TestCase):
    def test_parse_pin_reads_keys_and_skips_comments(self) -> None:
        self.assertEqual(hca_server.parse_pin(PIN), {
            "tuwunel_version": "v1.9.4", "tuwunel_sha256_x86_64": "aa", "tuwunel_sha256_aarch64": "bb"})

    def test_pinned_binary_names_the_versioned_file(self) -> None:
        got = hca_server.pinned_binary(hca_server.parse_pin(PIN), "x86_64", pathlib.Path("/lib/ab"))
        self.assertEqual(got, (pathlib.Path("/lib/ab/tuwunel-1.9.4"), "1.9.4", "aa"))

    def test_pinned_binary_refuses_an_unpinned_machine(self) -> None:
        with self.assertRaises(hca_server.MissingDependency):
            hca_server.pinned_binary(hca_server.parse_pin(PIN), "riscv64", pathlib.Path("/lib/ab"))

    def test_pinned_binary_refuses_a_pin_without_a_version(self) -> None:
        with self.assertRaises(hca_server.MissingDependency):
            hca_server.pinned_binary({"tuwunel_sha256_x86_64": "aa"}, "x86_64", pathlib.Path("/l"))


class ConfigTest(unittest.TestCase):
    def test_config_is_valid_toml_with_the_team_key_set_and_extras(self) -> None:
        text = hca_server.config_toml(8123, pathlib.Path("/w"), {"presence_idle_timeout_s": 20})
        got = tomllib.loads(text)
        self.assertEqual(got["port"], 8123)
        self.assertEqual(got["database_path"], "/w/db")
        self.assertIs(got["allow_federation"], False)
        self.assertIs(got["error_on_unknown_config_opts"], True)
        self.assertEqual(got["presence_idle_timeout_s"], 20)
        self.assertEqual(got["rate_limiting"]["login"]["failed"]["burst_count"], 3)
        self.assertEqual(set(got) - {"presence_idle_timeout_s"}, set(hca_server.TEAM_KEYS))


class LevelsTest(unittest.TestCase):
    def test_main_room_is_section_4_2_exactly(self) -> None:
        self.assertEqual(hca_server.levels_main([HUMAN]), {
            "users": {HUMAN: 50}, "users_default": 0, "events_default": 100, "state_default": 100,
            "invite": 100, "kick": 100, "ban": 100, "redact": 100, "notifications": {"room": 50},
            "events": {"m.room.message": 0}})

    def test_reports_room_is_section_4_2_exactly(self) -> None:
        self.assertEqual(hca_server.levels_reports([HUMAN]), {
            "users": {HUMAN: 0}, "users_default": 10, "events_default": 100, "state_default": 100,
            "invite": 100, "kick": 100, "ban": 100, "redact": 100, "notifications": {"room": 100},
            "events": {"m.room.message": 10}})

    def test_closed_questions_closes_messages_only(self) -> None:
        closed = hca_server.levels_questions_closed([HUMAN])
        self.assertEqual(closed["events"], {"m.room.message": 100})
        self.assertEqual({k: v for k, v in closed.items() if k != "events"},
                         {k: v for k, v in hca_server.levels_main([HUMAN]).items() if k != "events"})


class MacTest(unittest.TestCase):
    def test_registration_mac_matches_synapse_shared_secret_form(self) -> None:
        # HMAC-SHA1("secret", "n\0u\0p\0admin"), computed independently.
        self.assertEqual(hca_server.registration_mac("secret", "n", "u", "p", True),
                         "f393a2fa070427923635b76f35737172331c3aef")


class VerdictTest(unittest.TestCase):
    def test_any_failed_fact_fails(self) -> None:
        facts = [hca_probe.Fact("a", True, ""), hca_probe.Fact("b", False, ""), hca_probe.Fact("c", None, "")]
        self.assertEqual(hca_probe.verdict(facts), hca_probe.FAIL)

    def test_an_unjudged_fact_without_a_failure_is_not_judged(self) -> None:
        facts = [hca_probe.Fact("a", True, ""), hca_probe.Fact("c", None, "")]
        self.assertEqual(hca_probe.verdict(facts), hca_probe.NOT_JUDGED)

    def test_every_fact_holding_passes(self) -> None:
        self.assertEqual(hca_probe.verdict([hca_probe.Fact("a", True, "")]), hca_probe.PASS)

    def test_no_facts_is_not_judged(self) -> None:
        self.assertEqual(hca_probe.verdict([]), hca_probe.NOT_JUDGED)

    def test_exit_codes(self) -> None:
        self.assertEqual(hca_probe.exit_code([hca_probe.PASS, hca_probe.PASS]), 0)
        self.assertEqual(hca_probe.exit_code([hca_probe.PASS, hca_probe.NOT_JUDGED]), 2)
        self.assertEqual(hca_probe.exit_code([hca_probe.NOT_JUDGED, hca_probe.FAIL]), 1)
        self.assertEqual(hca_probe.exit_code([]), 2)

    def test_report_lists_each_check_and_ends_with_the_verdict(self) -> None:
        results = [hca_probe.Result("HC-A7", "the question", [hca_probe.Fact("sender", True, "@admin")])]
        text = hca_probe.render_report("Title", ["binary: x"], results)
        self.assertIn("| HC-A7 | PASS |", text)
        self.assertIn("- holds: sender: @admin", text)
        self.assertTrue(text.rstrip().endswith("Verdict: PASS (exit 0)"))

    def test_report_escapes_pipes_in_facts(self) -> None:
        results = [hca_probe.Result("X", "q", [hca_probe.Fact("a|b", None, "c|d")])]
        self.assertIn("a\\|b", hca_probe.render_report("T", [], results))


class SinceTest(unittest.TestCase):
    def test_an_error_status_is_an_error(self) -> None:
        self.assertEqual(hca_probe.classify_since(500, 0.1, 3.0), "an error (HTTP 500)")

    def test_a_quick_answer_is_the_current_position(self) -> None:
        self.assertEqual(hca_probe.classify_since(200, 0.2, 3.0), "an answer at once")

    def test_a_held_answer_is_a_wait(self) -> None:
        self.assertEqual(hca_probe.classify_since(200, 2.9, 3.0), "a wait for the timeout")


class PushRulesTest(unittest.TestCase):
    def test_suppress_notices_before_the_mention_rule(self) -> None:
        rules = {"global": {"override": [{"rule_id": ".m.rule.master"}, {"rule_id": ".m.rule.suppress_notices"},
                                         {"rule_id": ".m.rule.is_user_mention"}]}}
        self.assertEqual(hca_probe.override_order(rules), [".m.rule.master", ".m.rule.suppress_notices",
                                                           ".m.rule.is_user_mention"])
        self.assertTrue(hca_probe.notices_suppressed_first(rules))

    def test_a_mention_rule_first_is_not_suppressed_first(self) -> None:
        rules = {"global": {"override": [{"rule_id": ".m.rule.is_user_mention"},
                                         {"rule_id": ".m.rule.suppress_notices"}]}}
        self.assertFalse(hca_probe.notices_suppressed_first(rules))

    def test_a_missing_rule_is_not_suppressed_first(self) -> None:
        self.assertFalse(hca_probe.notices_suppressed_first({"global": {"override": []}}))


class ConfigDumpTest(unittest.TestCase):
    def test_keys_matching_finds_assignments(self) -> None:
        dump = "presence_offline_timeout_s = 1800\nlogin_with_password = true\nfoo: 1\n"
        self.assertEqual(hca_probe.keys_matching(dump, ("password", "deactivat")), ["login_with_password"])


def rel(content: dict) -> dict:
    return content["m.relates_to"]


class ShapeTest(unittest.TestCase):
    def test_a_plain_reply_holds(self) -> None:
        content = {"msgtype": "m.text", "body": "hca reply", "m.mentions": {"user_ids": [AGENT]},
                   "m.relates_to": {"m.in_reply_to": {"event_id": "$t"}}}
        facts = hca_element.judge_reply(content, "$t", AGENT)
        self.assertTrue(all(f.holds is not False for f in facts), facts)

    def test_a_reply_without_its_target_fails(self) -> None:
        facts = hca_element.judge_reply({"msgtype": "m.text", "body": "x"}, "$t", AGENT)
        self.assertIn(False, [f.holds for f in facts])

    def test_a_thread_reply_holds(self) -> None:
        content = {"msgtype": "m.text", "body": "hca thread", "m.relates_to": {
            "rel_type": "m.thread", "event_id": "$root", "is_falling_back": True,
            "m.in_reply_to": {"event_id": "$root"}}}
        facts = hca_element.judge_thread(content, "$root", AGENT)
        self.assertTrue(all(f.holds is not False for f in facts), facts)

    def test_a_thread_reply_to_another_root_fails(self) -> None:
        content = {"msgtype": "m.text", "body": "x", "m.relates_to": {"rel_type": "m.thread", "event_id": "$x"}}
        self.assertIn(False, [f.holds for f in hca_element.judge_thread(content, "$root", AGENT)])

    def test_a_quoted_post_in_a_thread_holds(self) -> None:
        content = {"msgtype": "m.text", "body": "hca quoted", "m.relates_to": {
            "rel_type": "m.thread", "event_id": "$root", "is_falling_back": False,
            "m.in_reply_to": {"event_id": "$post"}}}
        facts = hca_element.judge_thread_quote(content, "$root", "$post")
        self.assertTrue(all(f.holds is not False for f in facts), facts)

    def test_a_quoted_post_that_falls_back_fails(self) -> None:
        content = {"msgtype": "m.text", "body": "x", "m.relates_to": {
            "rel_type": "m.thread", "event_id": "$root", "is_falling_back": True,
            "m.in_reply_to": {"event_id": "$root"}}}
        self.assertIn(False, [f.holds for f in hca_element.judge_thread_quote(content, "$root", "$post")])

    def test_an_edit_holds(self) -> None:
        content = {"msgtype": "m.text", "body": "* hca edited", "m.new_content": {
            "msgtype": "m.text", "body": "hca edited"}, "m.relates_to": {"rel_type": "m.replace", "event_id": "$o"}}
        facts = hca_element.judge_edit(content, "$o")
        self.assertTrue(all(f.holds is not False for f in facts), facts)
        self.assertEqual(rel(content)["event_id"], "$o")

    def test_an_edit_without_new_content_fails(self) -> None:
        content = {"msgtype": "m.text", "body": "x", "m.relates_to": {"rel_type": "m.replace", "event_id": "$o"}}
        self.assertIn(False, [f.holds for f in hca_element.judge_edit(content, "$o")])

    def test_answer_parses_yes_and_no_only(self) -> None:
        self.assertIs(hca_element.parse_answer("yes"), True)
        self.assertIs(hca_element.parse_answer(" NO\r"), False)
        self.assertIsNone(hca_element.parse_answer("maybe"))


if __name__ == "__main__":
    unittest.main()

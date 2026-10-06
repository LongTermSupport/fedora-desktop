"""Tests for helpers.pingbus.limits, the agent team bus's local limits (spec §10).

The clock is always injected: every function takes `now_ms` (wall-clock milliseconds,
the unit of Matrix `origin_server_ts`), so nothing here sleeps or reads the real time.

Spec: CLAUDE/Plan/00161-agent-team-bus-matrix/PROTOCOL.md §9, §10 (moving to
docs/agent-bus-protocol.md in U02).
"""

from __future__ import annotations

import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import limits as lim

MIN = 60_000
T0 = 1_791_234_567_890
A = "@myrepo.1+host.podman:server.test"
B = "@myrepo.2+host.podman:server.test"
HUMAN = "@human1:server.test"
REF = "commit:example-org/myrepo@" + "0" * 40
RE = "$" + "A" * 43


class DefaultsAndBoundsTest(unittest.TestCase):
    def test_defaults_match_spec_table(self):
        self.assertEqual(
            lim.DEFAULTS,
            {
                "send_per_minute": 20,
                "send_burst": 10,
                "recv_per_sender_minute": 60,
                "ack_timeout_s": 900,
                "halt_ack_timeout_s": 300,
                "human_max_age_s": 86400,
                "wait_timeout_s": 1500,
            },
        )

    def test_bounds_match_spec_table(self):
        self.assertEqual(
            lim.BOUNDS,
            {
                "send_per_minute": (1, 60),
                "send_burst": (1, 30),
                "recv_per_sender_minute": (10, 600),
                "ack_timeout_s": (60, 86400),
                "halt_ack_timeout_s": (30, 3600),
                "human_max_age_s": (3600, 604800),
                "wait_timeout_s": (1, 1790),
            },
        )

    def test_every_default_is_inside_its_bounds(self):
        for key, value in lim.DEFAULTS.items():
            low, high = lim.BOUNDS[key]
            self.assertTrue(low <= value <= high, key)

    def test_fixed_values(self):
        self.assertEqual(lim.DUPLICATE_WINDOW_S, 60)
        self.assertEqual(lim.SYNC_LONG_POLL_S, 30)
        self.assertEqual(lim.SERVER_429_MAX_TRIES, 3)
        self.assertEqual(lim.SERVER_429_DEFAULT_WAIT_S, 5)
        self.assertEqual(lim.PING_CONTENT_MAX_BYTES, 4096)
        self.assertEqual(lim.HUMAN_BODY_MAX_BYTES, 16384)
        self.assertEqual(lim.ACK_EXPECTED_VERBS, frozenset({"fetch", "sync", "review", "run-qa", "halt"}))


class ParseLimitsTest(unittest.TestCase):
    def test_absent_gives_defaults(self):
        self.assertEqual(lim.parse_limits(None), lim.Limits())
        self.assertEqual(lim.Limits().as_dict(), lim.DEFAULTS)

    def test_empty_object_gives_defaults(self):
        self.assertEqual(lim.parse_limits({}), lim.Limits())

    def test_override_inside_bounds_taken(self):
        got = lim.parse_limits({"send_per_minute": 5, "ack_timeout_s": 60})
        self.assertEqual(got.send_per_minute, 5)
        self.assertEqual(got.ack_timeout_s, 60)
        self.assertEqual(got.send_burst, 10)

    def test_both_bounds_inclusive(self):
        for key, (low, high) in lim.BOUNDS.items():
            self.assertEqual(getattr(lim.parse_limits({key: low}), key), low, key)
            self.assertEqual(getattr(lim.parse_limits({key: high}), key), high, key)

    def test_out_of_bounds_refused_never_clamped(self):
        for key, (low, high) in lim.BOUNDS.items():
            for bad in (low - 1, high + 1):
                with self.subTest(key=key, value=bad):
                    with self.assertRaises(lim.LimitsError) as ctx:
                        lim.parse_limits({key: bad})
                    self.assertIn(key, str(ctx.exception))

    def test_unknown_key_refused(self):
        with self.assertRaises(lim.LimitsError):
            lim.parse_limits({"duplicate_window_s": 10})

    def test_wrong_types_refused(self):
        for bad in (True, False, 20.0, "20", None, [20], {"v": 20}):
            with self.subTest(value=bad):
                with self.assertRaises(lim.LimitsError):
                    lim.parse_limits({"send_per_minute": bad})

    def test_non_object_refused(self):
        for bad in ([], "x", 3, True):
            with self.subTest(value=bad):
                with self.assertRaises(lim.LimitsError):
                    lim.parse_limits(bad)

    def test_limits_error_is_a_value_error(self):
        self.assertTrue(issubclass(lim.LimitsError, ValueError))

    def test_check_wait_timeout(self):
        self.assertEqual(lim.check_wait_timeout(1), 1)
        self.assertEqual(lim.check_wait_timeout(1790), 1790)
        for bad in (0, 1791, True, 1.5, "10"):
            with self.subTest(value=bad):
                with self.assertRaises(lim.LimitsError):
                    lim.check_wait_timeout(bad)


class AckDeadlineTest(unittest.TestCase):
    def setUp(self):
        self.limits = lim.Limits()

    def test_ack_timeout_per_verb(self):
        for verb in ("fetch", "sync", "review", "run-qa"):
            self.assertEqual(lim.ack_timeout_s(verb, self.limits), 900, verb)
        self.assertEqual(lim.ack_timeout_s("halt", self.limits), 300)
        for verb in ("ack", "nack", "done", "blocked"):
            self.assertIsNone(lim.ack_timeout_s(verb, self.limits), verb)

    def test_ack_timeout_follows_overrides(self):
        limits = lim.parse_limits({"ack_timeout_s": 120, "halt_ack_timeout_s": 30})
        self.assertEqual(lim.ack_timeout_s("review", limits), 120)
        self.assertEqual(lim.ack_timeout_s("halt", limits), 30)

    def test_unknown_verb_refused(self):
        with self.assertRaises(ValueError):
            lim.ack_timeout_s("explode", self.limits)

    def test_deadline_ms(self):
        self.assertEqual(lim.ack_deadline_ms(T0, "review", self.limits), T0 + 900_000)
        self.assertEqual(lim.ack_deadline_ms(T0, "halt", self.limits), T0 + 300_000)
        self.assertIsNone(lim.ack_deadline_ms(T0, "done", self.limits))

    def test_overdue_only_after_deadline(self):
        deadline = T0 + 900_000
        self.assertFalse(lim.ack_overdue(T0, "review", deadline, self.limits))
        self.assertTrue(lim.ack_overdue(T0, "review", deadline + 1, self.limits))
        self.assertFalse(lim.ack_overdue(T0, "done", deadline * 2, self.limits))


class StaleTest(unittest.TestCase):
    def setUp(self):
        self.limits = lim.Limits()

    def test_stale_ping_uses_its_verbs_timeout(self):
        self.assertFalse(lim.ping_stale(T0, "review", T0 + 900_000, self.limits))
        self.assertTrue(lim.ping_stale(T0, "review", T0 + 900_001, self.limits))
        self.assertFalse(lim.ping_stale(T0, "halt", T0 + 300_000, self.limits))
        self.assertTrue(lim.ping_stale(T0, "halt", T0 + 300_001, self.limits))

    def test_stale_ping_without_ack_uses_ack_timeout(self):
        limits = lim.parse_limits({"ack_timeout_s": 600, "halt_ack_timeout_s": 60})
        for verb in ("ack", "nack", "done", "blocked"):
            self.assertFalse(lim.ping_stale(T0, verb, T0 + 600_000, limits), verb)
            self.assertTrue(lim.ping_stale(T0, verb, T0 + 600_001, limits), verb)

    def test_future_timestamp_is_not_stale(self):
        self.assertFalse(lim.ping_stale(T0 + 10 * MIN, "review", T0, self.limits))
        self.assertFalse(lim.human_stale(T0 + 10 * MIN, T0, self.limits))

    def test_stale_human(self):
        self.assertFalse(lim.human_stale(T0, T0 + 86_400_000, self.limits))
        self.assertTrue(lim.human_stale(T0, T0 + 86_400_001, self.limits))
        limits = lim.parse_limits({"human_max_age_s": 3600})
        self.assertTrue(lim.human_stale(T0, T0 + 3_600_001, limits))

    def test_timestamps_must_be_integers(self):
        for bad in (True, 1.5, "1", None):
            with self.subTest(value=bad):
                with self.assertRaises(ValueError):
                    lim.ping_stale(bad, "review", T0, self.limits)
                with self.assertRaises(ValueError):
                    lim.human_stale(bad, T0, self.limits)


class TokenBucketTest(unittest.TestCase):
    def test_starts_full_and_allows_a_burst(self):
        bucket = lim.TokenBucket.full(lim.Limits(), T0)
        for _ in range(10):
            self.assertTrue(bucket.take(T0))
        self.assertFalse(bucket.take(T0))

    def test_refills_at_rate_per_minute(self):
        bucket = lim.TokenBucket.full(lim.Limits(), T0)
        for _ in range(10):
            bucket.take(T0)
        # 20 per minute: one token every 3 s.
        self.assertFalse(bucket.take(T0 + 2_999))
        self.assertTrue(bucket.take(T0 + 3_000))
        self.assertFalse(bucket.take(T0 + 3_000))

    def test_never_exceeds_burst(self):
        bucket = lim.TokenBucket.full(lim.Limits(), T0)
        bucket.take(T0)
        later = T0 + 60 * MIN
        for _ in range(10):
            self.assertTrue(bucket.take(later))
        self.assertFalse(bucket.take(later))

    def test_refused_take_spends_nothing(self):
        limits = lim.parse_limits({"send_per_minute": 1, "send_burst": 1})
        bucket = lim.TokenBucket.full(limits, T0)
        self.assertTrue(bucket.take(T0))
        for step in range(1, 6):
            self.assertFalse(bucket.take(T0 + step * 10_000))
        self.assertTrue(bucket.take(T0 + MIN))

    def test_clock_going_back_adds_nothing(self):
        limits = lim.parse_limits({"send_per_minute": 1, "send_burst": 1})
        bucket = lim.TokenBucket.full(limits, T0)
        self.assertTrue(bucket.take(T0))
        self.assertFalse(bucket.take(T0 - 10 * MIN))
        self.assertFalse(bucket.take(T0 + MIN - 1))
        self.assertTrue(bucket.take(T0 + MIN))

    def test_round_trips_through_json(self):
        limits = lim.Limits()
        bucket = lim.TokenBucket.full(limits, T0)
        bucket.take(T0)
        bucket.take(T0)
        again = lim.TokenBucket.from_dict(json.loads(json.dumps(bucket.to_dict())), limits)
        self.assertEqual(again, bucket)

    def test_stored_tokens_clipped_to_a_lowered_burst(self):
        stored = lim.TokenBucket.full(lim.Limits(), T0).to_dict()
        smaller = lim.parse_limits({"send_burst": 2})
        bucket = lim.TokenBucket.from_dict(stored, smaller)
        self.assertTrue(bucket.take(T0))
        self.assertTrue(bucket.take(T0))
        self.assertFalse(bucket.take(T0))

    def test_corrupt_state_refused(self):
        for bad in ({}, {"credit": "1", "at_ms": T0}, {"credit": 1, "at_ms": True},
                    {"credit": -1, "at_ms": T0}, {"credit": 1, "at_ms": T0, "x": 1}, []):
            with self.subTest(state=bad):
                with self.assertRaises(ValueError):
                    lim.TokenBucket.from_dict(bad, lim.Limits())


class DuplicateWindowTest(unittest.TestCase):
    def test_same_ping_inside_window_is_duplicate(self):
        window = lim.DuplicateWindow()
        key = lim.duplicate_key("review", REF, None, [A, B])
        self.assertFalse(window.seen(key, T0))
        window.record(key, T0)
        self.assertTrue(window.seen(key, T0 + 59_999))
        self.assertFalse(window.seen(key, T0 + 60_000))

    def test_to_is_a_set(self):
        self.assertEqual(
            lim.duplicate_key("review", REF, None, [A, B]),
            lim.duplicate_key("review", REF, None, [B, A, B]),
        )

    def test_each_field_distinguishes(self):
        base = lim.duplicate_key("review", REF, None, [A])
        self.assertNotEqual(base, lim.duplicate_key("fetch", REF, None, [A]))
        self.assertNotEqual(base, lim.duplicate_key("review", REF + "x", None, [A]))
        self.assertNotEqual(base, lim.duplicate_key("review", REF, RE, [A]))
        self.assertNotEqual(base, lim.duplicate_key("review", REF, None, [A, B]))
        self.assertNotEqual(lim.duplicate_key("halt", None, None, [A]),
                            lim.duplicate_key("halt", "", None, [A]))

    def test_record_prunes_expired(self):
        window = lim.DuplicateWindow()
        old = lim.duplicate_key("halt", None, None, [A])
        new = lim.duplicate_key("halt", None, None, [B])
        window.record(old, T0)
        window.record(new, T0 + MIN)
        self.assertEqual(set(window.to_dict()), {new})

    def test_round_trips_through_json(self):
        window = lim.DuplicateWindow()
        key = lim.duplicate_key("sync", REF, None, [A])
        window.record(key, T0)
        again = lim.DuplicateWindow.from_dict(json.loads(json.dumps(window.to_dict())))
        self.assertTrue(again.seen(key, T0 + 1))

    def test_corrupt_state_refused(self):
        for bad in ([], {"k": "1"}, {"k": True}, {1: T0}):
            with self.subTest(state=bad):
                with self.assertRaises(ValueError):
                    lim.DuplicateWindow.from_dict(bad)


class SendGateTest(unittest.TestCase):
    def test_duplicate_refused_before_spending_a_token(self):
        limits = lim.parse_limits({"send_per_minute": 1, "send_burst": 2})
        gate = lim.SendGate.fresh(limits, T0)
        gate.admit("review", REF, None, [A], T0)
        with self.assertRaises(lim.RateLimited) as ctx:
            gate.admit("review", REF, None, [A], T0 + 1)
        self.assertEqual(ctx.exception.reason, "duplicate")
        gate.admit("review", REF, None, [B], T0 + 2)

    def test_bucket_empty_refused(self):
        limits = lim.parse_limits({"send_per_minute": 1, "send_burst": 1})
        gate = lim.SendGate.fresh(limits, T0)
        gate.admit("halt", None, None, [A], T0)
        with self.assertRaises(lim.RateLimited) as ctx:
            gate.admit("halt", None, None, [B], T0 + 1)
        self.assertEqual(ctx.exception.reason, "rate")

    def test_refused_by_rate_is_not_recorded_as_sent(self):
        limits = lim.parse_limits({"send_per_minute": 1, "send_burst": 1})
        gate = lim.SendGate.fresh(limits, T0)
        gate.admit("halt", None, None, [A], T0)
        with self.assertRaises(lim.RateLimited):
            gate.admit("halt", None, None, [B], T0 + 1)
        gate.admit("halt", None, None, [B], T0 + MIN)

    def test_round_trips_through_json(self):
        limits = lim.parse_limits({"send_per_minute": 1, "send_burst": 1})
        gate = lim.SendGate.fresh(limits, T0)
        gate.admit("halt", None, None, [A], T0)
        again = lim.SendGate.from_dict(json.loads(json.dumps(gate.to_dict())), limits)
        with self.assertRaises(lim.RateLimited) as ctx:
            again.admit("halt", None, None, [A], T0 + 1)
        self.assertEqual(ctx.exception.reason, "duplicate")

    def test_rate_limited_is_not_a_value_error(self):
        # A refusal by limit is exit 9, never confused with a validator refusal (exit 4).
        self.assertFalse(issubclass(lim.RateLimited, ValueError))


class ReceiveFloodTest(unittest.TestCase):
    def test_excess_per_sender_dropped(self):
        flood = lim.ReceiveFlood(lim.parse_limits({"recv_per_sender_minute": 10}))
        for i in range(10):
            self.assertTrue(flood.admit(A, T0 + i))
        self.assertFalse(flood.admit(A, T0 + 10))

    def test_senders_counted_separately_humans_included(self):
        flood = lim.ReceiveFlood(lim.parse_limits({"recv_per_sender_minute": 10}))
        for i in range(10):
            flood.admit(A, T0 + i)
        self.assertTrue(flood.admit(HUMAN, T0 + 10))
        self.assertTrue(flood.admit(B, T0 + 10))
        self.assertFalse(flood.admit(A, T0 + 10))

    def test_sliding_window_reopens(self):
        flood = lim.ReceiveFlood(lim.parse_limits({"recv_per_sender_minute": 10}))
        for i in range(10):
            flood.admit(A, T0 + i * 1000)
        self.assertFalse(flood.admit(A, T0 + MIN - 1))
        self.assertTrue(flood.admit(A, T0 + MIN))
        self.assertFalse(flood.admit(A, T0 + MIN))
        self.assertTrue(flood.admit(A, T0 + MIN + 1000))

    def test_dropped_events_do_not_extend_the_window(self):
        flood = lim.ReceiveFlood(lim.parse_limits({"recv_per_sender_minute": 10}))
        for _ in range(10):
            flood.admit(A, T0)
        for i in range(1, 100):
            self.assertFalse(flood.admit(A, T0 + i * 500))
        self.assertTrue(flood.admit(A, T0 + MIN))

    def test_default_is_sixty(self):
        flood = lim.ReceiveFlood(lim.Limits())
        self.assertEqual(sum(flood.admit(A, T0) for _ in range(70)), 60)


if __name__ == "__main__":
    unittest.main()

"""Crash-loop detection: restart-rate and absolute gates.

Plan 00132. These tests encode behaviour that was established against a REAL
crash loop on the host, not invented at a desk:

  - the offending container reached 131,377 restarts at ~130/minute
  - the next-highest container on the same host had 19, for its whole lifetime
  - every other container had exactly 0, and stayed at 0

and one defect that only the true-negative run exposed: `RestartCount` is
cumulative and never resets, so an absolute threshold keeps firing for ever on a
container that has already been stopped. The `running` gate exists for that, and
`test_absolute_does_not_fire_once_stopped` is the regression.
"""

import contextlib
import io
import os
import shutil
import tempfile
import unittest

from helpers.containerwatch import cli, crashloop


class RestartDeltaTests(unittest.TestCase):
    """The rate signal: how many restarts happened between two ticks."""

    def test_delta_is_the_difference_between_ticks(self):
        self.assertEqual(crashloop.restart_delta(previous=100, current=260), 160)

    def test_unchanged_count_is_zero_delta(self):
        self.assertEqual(crashloop.restart_delta(previous=19, current=19), 0)

    def test_no_previous_sample_yields_none_not_zero(self):
        # None and 0 must not collapse: 0 means "measured, nothing happened",
        # None means "not measurable yet". Returning 0 here would let a first
        # tick assert a container is healthy on no evidence at all.
        self.assertIsNone(crashloop.restart_delta(previous=None, current=500))

    def test_a_recreated_container_counts_backwards_and_is_not_measurable(self):
        # Removing and recreating a container resets RestartCount to 0, so the
        # delta goes negative. That is not a rate; it is a different container
        # wearing the same id slot.
        self.assertIsNone(crashloop.restart_delta(previous=900, current=3))


class ScaledThresholdTests(unittest.TestCase):
    """The bound is stated per production tick and scaled to real elapsed time."""

    def test_threshold_scales_with_elapsed_time(self):
        self.assertEqual(
            crashloop.scaled_rate_threshold(per_tick=10, tick_s=120, elapsed_s=60), 5
        )

    def test_a_full_tick_uses_the_stated_bound(self):
        self.assertEqual(
            crashloop.scaled_rate_threshold(per_tick=10, tick_s=120, elapsed_s=120), 10
        )

    def test_scaling_rounds_down_but_never_below_one(self):
        # Rounding UP would make a short interval STRICTER than the production
        # tick and manufacture findings the real probe would not raise.
        self.assertEqual(
            crashloop.scaled_rate_threshold(per_tick=10, tick_s=120, elapsed_s=1), 1
        )

    def test_nonsensical_elapsed_time_does_not_produce_a_zero_threshold(self):
        # A zero threshold would flag every container on the host, since every
        # delta is >= 0. Clamping to 1 fails toward silence, not toward noise.
        self.assertEqual(
            crashloop.scaled_rate_threshold(per_tick=10, tick_s=120, elapsed_s=0), 1
        )


class EvaluateTests(unittest.TestCase):
    """The decision, over a whole sample of containers."""

    def _evaluate(self, previous, current, running, elapsed_s=120.0):
        return crashloop.evaluate(
            previous=previous,
            current=current,
            running=running,
            elapsed_s=elapsed_s,
            rate_per_tick=10,
            tick_s=120,
            absolute_threshold=1000,
        )

    def test_the_real_offender_is_flagged_on_rate(self):
        # ~130 restarts/min sustained: the measured behaviour of the live loop.
        out = self._evaluate(
            previous={"loop": 131_000},
            current={"loop": 131_260},
            running={"loop": True},
        )
        self.assertEqual([f["container_id"] for f in out], ["loop"])
        self.assertIn("rate", out[0]["reasons"])
        self.assertEqual(out[0]["restart_delta"], 260)

    def test_a_static_healthy_host_produces_nothing(self):
        # Every other container on the real host sat at exactly 0 and stayed
        # there. A healthy host is not merely low on this metric, it is static.
        out = self._evaluate(
            previous={"a": 0, "b": 0, "c": 0},
            current={"a": 0, "b": 0, "c": 0},
            running={"a": True, "b": True, "c": True},
        )
        self.assertEqual(out, [])

    def test_real_historical_churn_is_not_flagged(self):
        # The nearest borderline case on the host: 19 lifetime restarts, no
        # current churn. If this ever flags, the thresholds are wrong.
        out = self._evaluate(
            previous={"busy": 19},
            current={"busy": 19},
            running={"busy": True},
        )
        self.assertEqual(out, [])

    def test_absolute_catches_a_loop_already_running_at_first_tick(self):
        # No previous sample, so rate is unmeasurable. Without this gate a loop
        # that predates the watchdog is invisible until the SECOND tick.
        out = self._evaluate(
            previous={},
            current={"loop": 131_377},
            running={"loop": True},
        )
        self.assertEqual([f["container_id"] for f in out], ["loop"])
        self.assertIn("absolute", out[0]["reasons"])
        self.assertIsNone(out[0]["restart_delta"])

    def test_absolute_does_not_fire_once_stopped(self):
        # THE REGRESSION. RestartCount is cumulative and never resets, so after
        # the loop was stopped the absolute test went on flagging 131,377 for
        # ever -- an alarm nobody can clear is one the operator learns to
        # ignore, and what they would learn to ignore is this exact signal.
        # A container that is not running cannot be looping.
        out = self._evaluate(
            previous={"loop": 131_377},
            current={"loop": 131_377},
            running={"loop": False},
        )
        self.assertEqual(out, [])

    def test_both_gates_can_fire_together(self):
        out = self._evaluate(
            previous={"loop": 131_000},
            current={"loop": 131_377},
            running={"loop": True},
        )
        self.assertEqual(sorted(out[0]["reasons"]), ["absolute", "rate"])

    def test_rate_fires_on_a_running_container_below_the_absolute_floor(self):
        # A NEW loop: churning hard but nowhere near 1000 cumulative yet. This
        # is the case the absolute gate cannot catch, and the reason both exist.
        out = self._evaluate(
            previous={"fresh": 2},
            current={"fresh": 140},
            running={"fresh": True},
        )
        self.assertEqual([f["container_id"] for f in out], ["fresh"])
        self.assertEqual(out[0]["reasons"], ["rate"])

    def test_a_container_that_vanished_between_ticks_is_not_reported(self):
        out = self._evaluate(
            previous={"gone": 50_000},
            current={},
            running={},
        )
        self.assertEqual(out, [])

    def test_findings_are_ordered_worst_first(self):
        out = self._evaluate(
            previous={"mild": 0, "severe": 0},
            current={"mild": 40, "severe": 400},
            running={"mild": True, "severe": True},
        )
        self.assertEqual([f["container_id"] for f in out], ["severe", "mild"])


class FindingShapeTests(unittest.TestCase):
    """The finding must be self-describing: a count alone is not actionable."""

    def test_finding_carries_what_a_human_needs_to_act(self):
        finding = crashloop.make_finding(
            container_id="abc123",
            container_name="some-container",
            engine="podman",
            restart_count=131_377,
            restart_delta=260,
            elapsed_s=120.0,
            reasons=["rate", "absolute"],
        )
        self.assertEqual(finding["kind"], "crashloop")
        self.assertEqual(finding["container_id"], "abc123")
        self.assertEqual(finding["container_name"], "some-container")
        self.assertEqual(finding["engine"], "podman")
        self.assertEqual(finding["restart_count"], 131_377)
        self.assertEqual(finding["restart_delta"], 260)
        self.assertEqual(finding["reasons"], ["rate", "absolute"])

    def test_kind_distinguishes_it_from_a_cpu_finding(self):
        # The existing findings are process-shaped (host_pid, cmd, cpu_pct); a
        # crash-loop finding is container-shaped and has no process at all. A
        # consumer must be able to tell them apart without guessing from keys.
        finding = crashloop.make_finding(
            container_id="abc123",
            container_name="n",
            engine="podman",
            restart_count=1,
            restart_delta=None,
            elapsed_s=None,
            reasons=["absolute"],
        )
        self.assertEqual(finding["kind"], "crashloop")
        self.assertNotIn("host_pid", finding)
        self.assertNotIn("cpu_pct", finding)

    def test_rate_per_minute_is_derived_not_left_to_the_reader(self):
        finding = crashloop.make_finding(
            container_id="a",
            container_name="n",
            engine="podman",
            restart_count=500,
            restart_delta=260,
            elapsed_s=120.0,
            reasons=["rate"],
        )
        self.assertEqual(finding["restarts_per_min"], 130)

    def test_rate_per_minute_is_none_when_not_measurable(self):
        finding = crashloop.make_finding(
            container_id="a",
            container_name="n",
            engine="podman",
            restart_count=500,
            restart_delta=None,
            elapsed_s=None,
            reasons=["absolute"],
        )
        self.assertIsNone(finding["restarts_per_min"])


class PreviousSampleTests(unittest.TestCase):
    """Reading the last tick's counts back out of report.json.

    The rate gate needs a previous sample, and the watchdog's own report is
    already written every tick and read by the CLI -- so it is the state store,
    and no second one is introduced.
    """

    def test_counts_and_timestamp_are_recovered(self):
        counts, generated_at = crashloop.previous_sample(
            {"generated_at": 1000, "restart_counts": {"a": 5, "b": 0}}
        )
        self.assertEqual(counts, {"a": 5, "b": 0})
        self.assertEqual(generated_at, 1000)

    def test_a_report_from_before_this_feature_yields_no_previous_counts(self):
        # Every report already on disk predates this field. It must read as "no
        # basis to measure a rate" rather than as "every container was at zero",
        # which would flag the whole host on the first tick after an upgrade.
        counts, generated_at = crashloop.previous_sample(
            {"generated_at": 1000, "findings": []}
        )
        self.assertEqual(counts, {})
        self.assertEqual(generated_at, 1000)

    def test_a_missing_report_is_not_an_error(self):
        counts, generated_at = crashloop.previous_sample({})
        self.assertEqual(counts, {})
        self.assertIsNone(generated_at)

    def test_a_corrupt_counts_block_is_discarded_not_trusted(self):
        counts, _ = crashloop.previous_sample(
            {"generated_at": 1, "restart_counts": "not-a-mapping"}
        )
        self.assertEqual(counts, {})

    def test_non_integer_counts_are_dropped_individually(self):
        counts, _ = crashloop.previous_sample(
            {"generated_at": 1, "restart_counts": {"good": 7, "bad": None}}
        )
        self.assertEqual(counts, {"good": 7})


class PreviousReportForRateTests(unittest.TestCase):
    """A corrupt cache must not stop the scan that would overwrite it."""

    def setUp(self):
        self._tmp = tempfile.mkdtemp()
        self._prev_runtime = os.environ.get("XDG_RUNTIME_DIR")
        os.environ["XDG_RUNTIME_DIR"] = self._tmp
        os.makedirs(os.path.join(self._tmp, "container-watch"), exist_ok=True)

    def tearDown(self):
        if self._prev_runtime is None:
            os.environ.pop("XDG_RUNTIME_DIR", None)
        else:
            os.environ["XDG_RUNTIME_DIR"] = self._prev_runtime
        shutil.rmtree(self._tmp, ignore_errors=True)

    def _write(self, text):
        with open(cli.report_path(), "w", encoding="utf-8") as fh:
            fh.write(text)

    def test_a_valid_report_is_returned(self):
        self._write('{"generated_at": 7, "restart_counts": {"a": 3}}')
        self.assertEqual(cli.previous_report_for_rate()["restart_counts"], {"a": 3})

    def test_a_truncated_report_does_not_raise(self):
        # A half-written file is exactly what a killed tick leaves behind. The
        # scan must still run and write a good one over it.
        self._write('{"generated_at": 7, "restart_c')
        self.assertEqual(cli.previous_report_for_rate(), {})

    def test_a_corrupt_report_leaves_the_rate_gate_inactive_not_wrong(self):
        self._write("this is not json")
        counts, generated_at = crashloop.previous_sample(cli.previous_report_for_rate())
        self.assertEqual(counts, {})
        self.assertIsNone(generated_at)

    def test_the_failure_is_announced_on_stderr(self):
        # Silent degradation would hide a report that is corrupt every tick.
        self._write("nope")
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            cli.previous_report_for_rate()
        self.assertIn("unreadable", err.getvalue())


class ElapsedTests(unittest.TestCase):
    """Elapsed time drives the scaled threshold, so a bad value is dangerous."""

    def test_elapsed_is_the_gap_between_reports(self):
        self.assertEqual(crashloop.elapsed_since(previous_at=1000, now=1120), 120.0)

    def test_no_previous_report_means_no_elapsed(self):
        self.assertIsNone(crashloop.elapsed_since(previous_at=None, now=1120))

    def test_a_clock_that_went_backwards_is_rejected(self):
        # Rather than yielding a negative elapsed, which would scale the
        # threshold to the clamp of 1 and flag any container that restarted once.
        self.assertIsNone(crashloop.elapsed_since(previous_at=2000, now=1000))


if __name__ == "__main__":
    unittest.main()

"""Automatic crash-loop containment: the decision to stop (Plan 00132 Phase 6).

Detection without enforcement leaves a defect that can take a machine down while
a report is being written about it. On a DESKTOP that costs a session; on a
SERVER nobody is looking at a panel at all, so the only thing standing between a
restart storm and an unusable host is this decision.

Stopping someone's container is destructive, so every guard here is a test rather
than a comment. The module is PURE: it decides, and a separate executor acts, so
none of these cases needs a container to exercise.
"""

import contextlib
import io
import unittest
import unittest.mock

from helpers.containerwatch import cli, containment


def _sample(ts, count):
    return {"at": ts, "count": count}


class WindowedRateDecidesTests(unittest.TestCase):
    """Rolling window, NOT the cumulative count.

    `RestartCount` never resets, so a cumulative threshold would eventually
    condemn any long-lived container that had a rough patch months ago. The
    question is what is happening now.
    """

    def test_enough_restarts_inside_the_window_trips(self):
        history = [_sample(0, 0), _sample(300, 150)]
        self.assertTrue(
            containment.should_contain(
                history=history, now=300, window_s=600, threshold=100
            )
        )

    def test_the_same_restarts_spread_beyond_the_window_do_not_trip(self):
        # Samples older than the window are not evidence about now.
        history = [_sample(0, 0), _sample(5000, 150)]
        self.assertFalse(
            containment.should_contain(
                history=history, now=5000, window_s=600, threshold=100
            )
        )

    def test_below_threshold_does_not_trip(self):
        history = [_sample(0, 0), _sample(300, 99)]
        self.assertFalse(
            containment.should_contain(
                history=history, now=300, window_s=600, threshold=100
            )
        )

    def test_exactly_the_threshold_trips(self):
        history = [_sample(0, 0), _sample(300, 100)]
        self.assertTrue(
            containment.should_contain(
                history=history, now=300, window_s=600, threshold=100
            )
        )

    def test_a_single_sample_never_trips(self):
        # One reading is a level, not a rate. Containing on it would stop a
        # container for restarts that may have happened weeks ago.
        self.assertFalse(
            containment.should_contain(
                history=[_sample(0, 999999)], now=0, window_s=600, threshold=100
            )
        )

    def test_an_empty_history_never_trips(self):
        self.assertFalse(
            containment.should_contain(
                history=[], now=0, window_s=600, threshold=100
            )
        )

    def test_a_counter_reset_does_not_trip(self):
        # RestartCount returns to 0 when a container is recreated in the same id
        # slot. A negative delta is a DIFFERENT container, not a storm.
        history = [_sample(0, 5000), _sample(300, 3)]
        self.assertFalse(
            containment.should_contain(
                history=history, now=300, window_s=600, threshold=100
            )
        )


class HistoryIsTrimmedTests(unittest.TestCase):
    def test_samples_outside_the_window_are_dropped(self):
        history = [_sample(0, 0), _sample(100, 5), _sample(900, 9)]
        trimmed = containment.trim_history(history, now=1000, window_s=600)
        self.assertEqual(trimmed, [_sample(900, 9)])

    def test_the_boundary_sample_is_kept(self):
        history = [_sample(400, 1)]
        self.assertEqual(
            containment.trim_history(history, now=1000, window_s=600), [_sample(400, 1)]
        )

    def test_appending_records_the_new_sample(self):
        history = containment.record_sample([], at=10, count=3)
        self.assertEqual(history, [_sample(10, 3)])

    def test_appending_trims_at_the_same_time(self):
        history = [_sample(0, 0)]
        out = containment.record_sample(history, at=1000, count=7, window_s=600)
        self.assertEqual(out, [_sample(1000, 7)])

    def test_a_malformed_sample_is_discarded_not_guessed_at(self):
        history = [{"at": "nonsense", "count": 1}, _sample(900, 9)]
        self.assertEqual(
            containment.trim_history(history, now=1000, window_s=600), [_sample(900, 9)]
        )


class SafetyGuardsTests(unittest.TestCase):
    """Every reason NOT to stop a container. Each one is a way to do harm."""

    def _candidate(self, **over):
        base = {
            "container_id": "id-a",
            "container_name": "c-a",
            "engine": "podman",
            "running": True,
            "restart_policy": "uncapped",
            "history": [_sample(0, 0), _sample(300, 500)],
        }
        base.update(over)
        return base

    def test_a_running_storming_container_is_contained(self):
        decision = containment.decide(
            self._candidate(), now=300, allowlist=[], enabled=True
        )
        self.assertTrue(decision.contain)

    def test_a_container_that_is_not_running_is_left_alone(self):
        decision = containment.decide(
            self._candidate(running=False), now=300, allowlist=[], enabled=True
        )
        self.assertFalse(decision.contain)
        self.assertIn("not running", decision.reason)

    def test_an_allowlisted_container_is_never_contained(self):
        decision = containment.decide(
            self._candidate(),
            now=300,
            allowlist=[{"container_name": "c-a"}],
            enabled=True,
        )
        self.assertFalse(decision.contain)
        self.assertIn("allowlist", decision.reason)

    def test_containment_disabled_means_report_only(self):
        decision = containment.decide(
            self._candidate(), now=300, allowlist=[], enabled=False
        )
        self.assertFalse(decision.contain)
        self.assertIn("disabled", decision.reason)

    def test_an_unknown_engine_is_not_acted_on(self):
        # We would not know which command to run, and guessing means running an
        # invented command as a privileged timer.
        decision = containment.decide(
            self._candidate(engine="lxc"), now=300, allowlist=[], enabled=True
        )
        self.assertFalse(decision.contain)
        self.assertIn("engine", decision.reason)

    def test_a_quiet_container_is_not_contained(self):
        decision = containment.decide(
            self._candidate(history=[_sample(0, 0), _sample(300, 2)]),
            now=300,
            allowlist=[],
            enabled=True,
        )
        self.assertFalse(decision.contain)


class OnlyUnboundedPoliciesAreContainedTests(unittest.TestCase):
    """The structural guard: containment acts ONLY where the policy is unbounded.

    This is stronger than a threshold. A container whose policy is `no` cannot be
    restarted by the engine at all, so whatever is cycling it is something else —
    a systemd unit, a compose supervisor, a human — and stopping it would neither
    address the cause nor stay stopped. A capped `on-failure:N` gives up on its
    own, which is the behaviour being asked for rather than a fault.

    So the only containers this can ever touch are the ones configured to restart
    for ever. Every CCY session runs with no restart policy, which puts them out
    of reach by construction rather than by luck.
    """

    def _candidate(self, **over):
        base = {
            "container_id": "id-a",
            "container_name": "c-a",
            "engine": "podman",
            "running": True,
            "restart_policy": "uncapped",
            "history": [_sample(0, 0), _sample(300, 500)],
        }
        base.update(over)
        return base

    def test_an_unbounded_policy_is_contained(self):
        decision = containment.decide(
            self._candidate(), now=300, allowlist=[], enabled=True
        )
        self.assertTrue(decision.contain)

    def test_a_container_with_no_restart_policy_is_never_contained(self):
        decision = containment.decide(
            self._candidate(restart_policy="none"), now=300, allowlist=[], enabled=True
        )
        self.assertFalse(decision.contain)
        self.assertIn("policy", decision.reason)

    def test_a_capped_policy_is_never_contained(self):
        decision = containment.decide(
            self._candidate(restart_policy="capped"), now=300, allowlist=[], enabled=True
        )
        self.assertFalse(decision.contain)
        self.assertIn("policy", decision.reason)

    def test_an_unknown_policy_is_not_contained(self):
        # Acting on a policy we could not classify means stopping a container on a
        # guess. Reported, never acted on.
        decision = containment.decide(
            self._candidate(restart_policy="unknown"), now=300, allowlist=[], enabled=True
        )
        self.assertFalse(decision.contain)

    def test_an_absent_policy_field_is_not_contained(self):
        # A candidate assembled without policy information must fail toward doing
        # nothing, not toward stopping something.
        candidate = self._candidate()
        del candidate["restart_policy"]
        decision = containment.decide(
            candidate, now=300, allowlist=[], enabled=True
        )
        self.assertFalse(decision.contain)

    def test_the_policy_guard_outranks_a_storming_history(self):
        # However fast it is cycling: wrong policy, not our business.
        decision = containment.decide(
            self._candidate(
                restart_policy="none",
                history=[_sample(0, 0), _sample(300, 100000)],
            ),
            now=300,
            allowlist=[],
            enabled=True,
        )
        self.assertFalse(decision.contain)


class TheExecutorRunsNothingItShouldNotTests(unittest.TestCase):
    """Pin the safety claim AT THE LAYER THAT EXECUTES.

    `decide()` is thoroughly tested, but it cannot stop a container — this can.
    These assert on the subprocess call itself, so a future refactor that wires
    the executor to the wrong field, or forgets to consult `decide` at all, fails
    here rather than on someone's machine.
    """

    def _candidate(self, **over):
        base = {
            "container_id": "id-a",
            "container_name": "c-a",
            "engine": "podman",
            "running": True,
            "restart_policy": "uncapped",
            "history": [{"at": 0, "count": 0}, {"at": 300, "count": 500}],
        }
        base.update(over)
        return base

    def _run_with_spy(self, candidates, **kw):
        calls = []

        def spy(argv, **_):
            calls.append(argv)
            return unittest.mock.Mock(returncode=0, stdout="", stderr="")

        opts = {"now": 300, "allowlist": [], "enabled": True}
        opts.update(kw)
        with unittest.mock.patch.object(cli.subprocess, "run", spy), \
                contextlib.redirect_stderr(io.StringIO()):
            outcomes = cli.apply_containment(candidates, **opts)
        return calls, outcomes

    def test_a_qualifying_container_is_stopped_exactly_once(self):
        calls, outcomes = self._run_with_spy([self._candidate()])
        self.assertEqual(calls, [["podman", "stop", "--time", "10", "id-a"]])
        self.assertTrue(outcomes[0]["stopped"])

    def test_no_subprocess_runs_when_the_policy_is_not_unbounded(self):
        for policy in ("none", "capped", "unknown", ""):
            calls, outcomes = self._run_with_spy(
                [self._candidate(restart_policy=policy)]
            )
            self.assertEqual(calls, [], f"policy {policy!r} must run nothing")
            self.assertFalse(outcomes[0]["stopped"])

    def test_no_subprocess_runs_when_disabled(self):
        calls, _ = self._run_with_spy([self._candidate()], enabled=False)
        self.assertEqual(calls, [])

    def test_no_subprocess_runs_for_an_allowlisted_container(self):
        calls, _ = self._run_with_spy(
            [self._candidate()], allowlist=[{"container_name": "c-a"}]
        )
        self.assertEqual(calls, [])

    def test_no_subprocess_runs_below_the_threshold(self):
        calls, _ = self._run_with_spy(
            [self._candidate(history=[{"at": 0, "count": 0}, {"at": 300, "count": 3}])]
        )
        self.assertEqual(calls, [])

    def test_one_container_being_stopped_does_not_stop_the_others(self):
        calls, outcomes = self._run_with_spy(
            [
                self._candidate(),
                self._candidate(container_id="id-b", container_name="c-b", restart_policy="none"),
            ]
        )
        self.assertEqual(calls, [["podman", "stop", "--time", "10", "id-a"]])
        self.assertEqual([o["stopped"] for o in outcomes], [True, False])

    def test_a_failing_stop_is_recorded_not_swallowed(self):
        def boom(argv, **_):
            raise cli.subprocess.CalledProcessError(1, argv)

        stderr = io.StringIO()
        with unittest.mock.patch.object(cli.subprocess, "run", boom), \
                contextlib.redirect_stderr(stderr):
            outcomes = cli.apply_containment(
                [self._candidate()], now=300, allowlist=[], enabled=True
            )
        self.assertFalse(outcomes[0]["stopped"])
        self.assertIn("CONTAINMENT FAILED", stderr.getvalue())

    def test_a_refusal_records_why(self):
        # "Why was this NOT stopped" is the question asked after an incident.
        _, outcomes = self._run_with_spy([self._candidate(restart_policy="none")])
        self.assertTrue(outcomes[0]["reason"])


class TheOptOutMustNotFailOpenTests(unittest.TestCase):
    """A mistyped opt-out must not silently mean "keep stopping containers".

    The config is JSON, so `{"containment": "false"}` and `{"containment": 0}` are
    the obvious ways to get this wrong. Reading either permissively hands the
    destructive behaviour to someone who explicitly tried to turn it off — the
    worst possible direction for a default-on feature to fail in.
    """

    def test_absent_means_enabled(self):
        self.assertTrue(cli.containment_enabled({}))

    def test_explicit_true_means_enabled(self):
        self.assertTrue(cli.containment_enabled({"containment": True}))

    def test_explicit_false_means_disabled(self):
        self.assertFalse(cli.containment_enabled({"containment": False}))

    def test_the_string_false_is_refused_not_read_as_enabled(self):
        with self.assertRaises(ValueError):
            cli.containment_enabled({"containment": "false"})

    def test_a_number_is_refused(self):
        with self.assertRaises(ValueError):
            cli.containment_enabled({"containment": 0})

    def test_null_is_refused(self):
        with self.assertRaises(ValueError):
            cli.containment_enabled({"containment": None})

    def test_the_error_names_the_key_so_it_can_be_fixed(self):
        with self.assertRaises(ValueError) as ctx:
            cli.containment_enabled({"containment": "no"})
        self.assertIn("containment", str(ctx.exception))


class TheCommandIsStopAndOnlyStopTests(unittest.TestCase):
    """`stop`, never `kill`/`rm`/`pause` — each of the others is a different harm.

    `kill` denies the workload its shutdown path, `rm` destroys it outright, and
    `pause` is actively wrong here: a frozen container still holds the transient
    units whose accumulation is the damage being prevented.
    """

    def test_podman_uses_stop_with_a_timeout(self):
        self.assertEqual(
            containment.build_stop_argv(engine="podman", container_id="id-a"),
            ["podman", "stop", "--time", "10", "id-a"],
        )

    def test_docker_uses_the_docker_spelling(self):
        self.assertEqual(
            containment.build_stop_argv(engine="docker", container_id="id-a"),
            ["docker", "stop", "--time", "10", "id-a"],
        )

    def test_an_unsupported_engine_raises_rather_than_improvising(self):
        with self.assertRaises(ValueError):
            containment.build_stop_argv(engine="lxc", container_id="id-a")

    def test_no_destructive_verb_can_be_produced(self):
        for engine in ("podman", "docker"):
            argv = containment.build_stop_argv(engine=engine, container_id="id-a")
            self.assertNotIn("kill", argv)
            self.assertNotIn("rm", argv)
            self.assertNotIn("pause", argv)
            self.assertNotIn("-f", argv)
            self.assertNotIn("--force", argv)


if __name__ == "__main__":
    unittest.main()

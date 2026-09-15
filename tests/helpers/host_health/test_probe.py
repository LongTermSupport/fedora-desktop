"""Tests for helpers.host_health.probe — Plan 00109 Task 3.1's executor.

The half that touches the machine: it runs `dkms status` and `systemctl --failed`
for both scopes, asks the kernel what booted, and hands all of it to the classifier.
Every rule about what counts as broken lives in `probe_results` and is tested there.

What is pinned here is the part a classifier test cannot see:

1. **A command that cannot be run is a FINDING, not a crash and not a skip.** The
   executor runs at the end of a login, so an exception here takes the whole health
   surface down and the user sees nothing — which is indistinguishable from a clean
   host, and is the failure this plan exists for.
2. **Silent when clean**, and the exit status distinguishes the two outcomes so a
   caller does not have to parse the text to find out.
3. The probes are run with **argv lists, never a shell string**.
"""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.host_health import probe, probe_results

RUNNING_KERNEL = "7.2.4-200.fc44.x86_64"
DKMS_HEALTHY = f"evdi/1.15.0, {RUNNING_KERNEL}, x86_64: installed"
DKMS_STALE_ONLY = "evdi/1.15.0, 7.1.9-200.fc44.x86_64, x86_64: installed"


class FakeRunner:
    """Stands in for the subprocess call, recording the argv it was handed."""

    def __init__(self, answers: dict[str, tuple[bool, str, str]]) -> None:
        self.answers = answers
        self.calls: list[list[str]] = []

    def __call__(self, argv: list[str]) -> probe.probe_results.ProbeOutcome:
        self.calls.append(list(argv))
        for key, (ok, text, error) in self.answers.items():
            if key in " ".join(argv):
                return probe.probe_results.ProbeOutcome(ok=ok, text=text, error=error)
        return probe.probe_results.ProbeOutcome(ok=True, text="", error="")


def healthy() -> FakeRunner:
    return FakeRunner({"dkms": (True, DKMS_HEALTHY, "")})


class TestRunProbe(unittest.TestCase):
    """The real runner, against commands that exist and commands that do not."""

    def test_a_command_that_succeeds_reports_its_stdout(self) -> None:
        outcome = probe.run_probe([sys.executable, "-c", "print('hello')"])
        self.assertTrue(outcome.ok)
        self.assertEqual(outcome.text.strip(), "hello")

    def test_a_missing_command_is_not_ok_and_says_why(self) -> None:
        """`dkms` is genuinely absent on a host that never installed it, and this is
        the path that turns that into a finding instead of a traceback."""
        outcome = probe.run_probe(["definitely-not-a-real-command-00109"])
        self.assertFalse(outcome.ok)
        self.assertIn("definitely-not-a-real-command-00109", outcome.error)

    def test_a_nonzero_exit_is_not_ok_and_carries_the_stderr(self) -> None:
        outcome = probe.run_probe(
            [sys.executable, "-c", "import sys; sys.stderr.write('boom'); sys.exit(3)"])
        self.assertFalse(outcome.ok)
        self.assertIn("boom", outcome.error)

    def test_a_nonzero_exit_with_silent_stderr_still_says_something(self) -> None:
        """An empty error string would render as 'could not run: ' and tell a user
        nothing at all."""
        outcome = probe.run_probe([sys.executable, "-c", "raise SystemExit(4)"])
        self.assertFalse(outcome.ok)
        self.assertTrue(outcome.error.strip())

    def test_a_MULTI_LINE_stderr_is_collapsed_to_one_line(self) -> None:
        """Real systemctl answers an unreachable bus in two lines. Each finding is
        one line by contract — the notification splits on newlines — so a second
        line would arrive as a finding nobody wrote."""
        outcome = probe.run_probe(
            [sys.executable, "-c",
             "import sys; sys.stderr.write('first line\\nsecond line\\n'); sys.exit(1)"])
        self.assertFalse(outcome.ok)
        self.assertNotIn("\n", outcome.error)
        self.assertIn("first line second line", outcome.error)


class TestProbesRunAsArgv(unittest.TestCase):
    def test_three_probes_are_run(self) -> None:
        runner = healthy()
        probe.collect(running_kernel=RUNNING_KERNEL, runner=runner)
        self.assertEqual(len(runner.calls), 3)

    def test_every_probe_is_an_argv_list_not_a_shell_string(self) -> None:
        runner = healthy()
        probe.collect(running_kernel=RUNNING_KERNEL, runner=runner)
        for call in runner.calls:
            self.assertIsInstance(call, list)
            self.assertTrue(all(isinstance(part, str) for part in call))

    def test_the_user_scope_probe_is_distinguished_from_the_system_one(self) -> None:
        runner = healthy()
        probe.collect(running_kernel=RUNNING_KERNEL, runner=runner)
        systemctl = [c for c in runner.calls if c[0] == "systemctl"]
        self.assertEqual(len(systemctl), 2)
        self.assertEqual(len([c for c in systemctl if "--user" in c]), 1)

    def test_systemctl_is_asked_for_output_a_parser_can_read(self) -> None:
        """Without --no-legend the count footer parses as a unit named '0'."""
        runner = healthy()
        probe.collect(running_kernel=RUNNING_KERNEL, runner=runner)
        for call in [c for c in runner.calls if c[0] == "systemctl"]:
            self.assertIn("--no-legend", call)
            self.assertIn("--no-pager", call)
            self.assertIn("--plain", call)


class TestCollect(unittest.TestCase):
    def test_a_healthy_host_is_clean(self) -> None:
        self.assertTrue(probe.collect(running_kernel=RUNNING_KERNEL, runner=healthy()).clean)

    def test_the_incidents_own_shape_is_a_finding(self) -> None:
        """evdi built for yesterday's kernel: `dkms status` is not empty, so only a
        check against the RUNNING kernel catches it."""
        runner = FakeRunner({"dkms": (True, DKMS_STALE_ONLY, "")})
        report = probe.collect(running_kernel=RUNNING_KERNEL, runner=runner)
        self.assertFalse(report.clean)
        self.assertTrue(any("evdi" in f for f in report.texts))

    def test_a_missing_dkms_is_a_finding_not_a_crash(self) -> None:
        runner = FakeRunner({"dkms": (False, "", "dkms: command not found")})
        report = probe.collect(running_kernel=RUNNING_KERNEL, runner=runner)
        self.assertFalse(report.clean)

    def test_a_failed_systemctl_is_a_finding_not_an_empty_unit_list(self) -> None:
        runner = FakeRunner({
            "dkms": (True, DKMS_HEALTHY, ""),
            "systemctl --failed": (False, "", "Failed to connect to bus"),
        })
        report = probe.collect(running_kernel=RUNNING_KERNEL, runner=runner)
        self.assertFalse(report.clean)

    def test_it_reports_on_the_host_only(self) -> None:
        """Phase 2's findings do not come through here. They merge in
        `login_report.collect`, which is the only layer that can guard each check
        separately, and whose own tests pin the one-notification property."""
        report = probe.collect(running_kernel=RUNNING_KERNEL, runner=healthy())
        self.assertTrue(report.clean)


class TestRunningKernel(unittest.TestCase):
    def test_an_absent_state_directory_says_there_is_no_dkms_subsystem(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            registry = probe.dkms_registry(os.path.join(base, "no-such-dir"))
            self.assertIs(registry.present, False)
            self.assertEqual(registry.modules, ())

    def test_an_empty_state_directory_is_NOT_the_same_as_an_absent_one(self) -> None:
        """The distinction H4 turned on. The `dkms` rpm owns `/var/lib/dkms`, so a host
        that ran play-displaylink.yml has the directory — and an empty registry there
        means the module was REMOVED, which is exactly what the pin check exists to
        report. Answering both cases "no modules found" silences it."""
        with tempfile.TemporaryDirectory() as base:
            registry = probe.dkms_registry(base)
            self.assertIs(registry.present, True)
            self.assertEqual(registry.modules, ())

    def test_registered_modules_are_named(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            os.mkdir(os.path.join(base, "evdi"))
            os.mkdir(os.path.join(base, "vboxhost"))
            self.assertEqual(
                probe.dkms_registry(base).modules, ("evdi", "vboxhost"))

    def test_files_beside_the_module_trees_are_not_modules(self) -> None:
        """DKMS keeps more than module directories in there; a stray file counted as a
        module would report a host as having DKMS modules it does not have. `dkms` keeps
        `dkms_dbversion` here, which is why a real emptied registry reads as empty."""
        with tempfile.TemporaryDirectory() as base:
            os.mkdir(os.path.join(base, "evdi"))
            with open(os.path.join(base, "dkms_dbversion"), "w", encoding="utf-8") as f:
                f.write("3\n")
            self.assertEqual(probe.dkms_registry(base).modules, ("evdi",))

    def test_a_dangling_symlink_answers_could_not_tell(self) -> None:
        """`DirEntry.is_dir` swallows FileNotFoundError, so a link that does not resolve
        reads as "not a module" and the entry vanishes from a registry whose emptiness
        decides things. A partial read must not answer "no DKMS modules"."""
        with tempfile.TemporaryDirectory() as base:
            os.mkdir(os.path.join(base, "evdi"))
            os.symlink(os.path.join(base, "gone"), os.path.join(base, "vboxhost"))
            self.assertIsNone(probe.dkms_registry(base).present)

    def test_a_symlink_to_a_real_module_tree_still_counts(self) -> None:
        with tempfile.TemporaryDirectory() as base:
            os.mkdir(os.path.join(base, "real"))
            os.symlink(os.path.join(base, "real"), os.path.join(base, "evdi"))
            self.assertEqual(probe.dkms_registry(base).modules, ("evdi", "real"))

    def test_a_state_path_that_is_not_a_directory_answers_could_not_tell(self) -> None:
        """`scandir` on a regular file raises NotADirectoryError, an OSError that is not
        FileNotFoundError — so it must read as "could not tell", not as absence."""
        with tempfile.TemporaryDirectory() as base:
            path = os.path.join(base, "not-a-dir")
            with open(path, "w", encoding="utf-8") as handle:
                handle.write("")
            self.assertIsNone(probe.dkms_registry(path).present)

    def test_collect_passes_the_registry_through(self) -> None:
        """The seam exists so the silence rule is reachable from `collect`, not only
        from the classifier it delegates to."""
        report = probe.collect(
            running_kernel=RUNNING_KERNEL,
            runner=lambda argv: probe_results.ProbeOutcome(
                ok=False, text="", error="dkms: command not found", missing=True)
            if argv[0] == "dkms"
            else probe_results.ProbeOutcome(ok=True, text="", error=""),
            registry=probe_results.DkmsRegistry(present=False),
        )
        self.assertTrue(report.clean)

    def test_the_running_kernel_is_read_from_the_kernel_not_from_a_command(self) -> None:
        """`uname -r` would be a fourth probe that can fail; os.uname cannot."""
        self.assertEqual(probe.running_kernel(), os.uname().release)

    def test_it_is_not_empty(self) -> None:
        self.assertTrue(probe.running_kernel())


class TestMain(unittest.TestCase):
    def test_a_clean_host_prints_NOTHING_and_exits_zero(self) -> None:
        """Silent when clean. A health check that speaks every login gets muted."""
        stdout = io.StringIO()
        status = probe.main(argv=[], stdout=stdout, runner=healthy(), kernel=RUNNING_KERNEL)
        self.assertEqual(status, probe.EXIT_OK)
        self.assertEqual(stdout.getvalue(), "")

    def test_findings_are_printed_and_the_status_says_so(self) -> None:
        stdout = io.StringIO()
        status = probe.main(
            argv=[], stdout=stdout,
            runner=FakeRunner({"dkms": (True, DKMS_STALE_ONLY, "")}), kernel=RUNNING_KERNEL)
        self.assertEqual(status, probe.EXIT_FINDINGS)
        self.assertIn("evdi", stdout.getvalue())

    def test_nothing_leaks_to_the_REAL_stderr(self) -> None:
        """The findings are the payload and a caller captures stdout to build one
        notification. `main` takes no stderr seam on purpose — there is nothing to
        write there — so this redirects the real one, which a stray `print` would
        reach and an unused parameter would not."""
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stderr(stderr):
            probe.main(
                argv=[], stdout=stdout,
                runner=FakeRunner({"dkms": (True, DKMS_STALE_ONLY, "")}),
                kernel=RUNNING_KERNEL)
        self.assertEqual(stderr.getvalue(), "")
        self.assertTrue(stdout.getvalue())

    def test_one_finding_per_line(self) -> None:
        stdout = io.StringIO()
        probe.main(
            argv=[], stdout=stdout,
            runner=FakeRunner({
                "dkms": (True, DKMS_STALE_ONLY, ""),
                "systemctl --failed": (True, "a.service loaded failed failed A", ""),
            }),
            kernel=RUNNING_KERNEL)
        self.assertEqual(len(stdout.getvalue().strip().splitlines()), 2)

    def test_the_two_exit_codes_are_distinct(self) -> None:
        self.assertNotEqual(probe.EXIT_OK, probe.EXIT_FINDINGS)


if __name__ == "__main__":
    unittest.main()

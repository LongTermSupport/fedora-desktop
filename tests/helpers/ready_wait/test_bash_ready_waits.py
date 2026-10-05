"""Tests for the bash half of `ready-wait-ignores-child-exit` (CLAUDE/QA.md).

A ready-wait is a loop that sleeps between tries while it waits for a process this
script started in the background. If the loop never asks whether that process is
still alive, a child that died in its first second is reported only when the loop
gives up, and as "timed out" rather than with the child's own error.
"""

import os
import pathlib
import sys
import tempfile
import textwrap
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.ready_wait import bash_ready_waits

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]


def lines(script):
    return bash_ready_waits.findings(textwrap.dedent(script))


class TheOriginatingShapes(unittest.TestCase):

    def test_a_poll_after_a_background_start_is_reported(self):
        self.assertEqual(lines("""\
            tmate -F &
            TMATE_PID=$!
            for _ in $(seq 1 30); do
                tmate show-messages | grep -q "read only" && break
                sleep 0.5
            done
            """), [3])

    def test_a_while_poll_on_a_port_file_is_reported(self):
        self.assertEqual(lines("""\
            python3 proxy.py "$port_file" &
            proxy_pid=$!
            while [ ! -s "$port_file" ]; do
                sleep 0.1
            done
            """), [3])

    def test_a_wait_helper_called_after_a_launcher_is_reported(self):
        # The launch and the wait live in two functions; the caller runs one, then
        # the other.
        self.assertEqual(lines("""\
            wait_for_state() {
                local _
                for _ in $(seq 1 100); do
                    grep -q RECORDING "$log" && return 0
                    sleep 0.05
                done
                return 1
            }
            start_it() {
                program > "$out" 2> "$err" &
                PROGRAM_PID=$!
            }
            start_it
            if wait_for_state; then
                kill -TERM "$PROGRAM_PID"
            fi
            """), [3])


class WhatClearsAWait(unittest.TestCase):

    def test_kill_0_in_the_loop_clears_it(self):
        self.assertEqual(lines("""\
            tmate -F &
            TMATE_PID=$!
            for _ in $(seq 1 30); do
                if ! kill -0 "$TMATE_PID" 2>/dev/null; then
                    wait "$TMATE_PID"
                    exit 1
                fi
                sleep 0.5
            done
            """), [])

    def test_kill_0_in_the_loop_condition_clears_it(self):
        self.assertEqual(lines("""\
            server &
            pid=$!
            while kill -0 "$pid" && [ ! -S "$sock" ]; do
                sleep 0.1
            done
            """), [])

    def test_kill_0_only_after_the_loop_does_not_clear_it(self):
        self.assertEqual(lines("""\
            tmate -F &
            TMATE_PID=$!
            for _ in $(seq 1 30); do
                tmate show-messages | grep -q "read only" && break
                sleep 0.5
            done
            if kill -0 "$TMATE_PID"; then kill "$TMATE_PID"; fi
            """), [3])

    def test_wait_n_and_ps_p_count_as_liveness(self):
        self.assertEqual(lines("""\
            a &
            pid=$!
            for _ in 1 2 3; do
                ps -p "$pid" >/dev/null || exit 1
                sleep 1
            done
            for _ in 1 2 3; do
                wait -n -p done_pid "$pid" || exit 1
                sleep 1
            done
            """), [])


class WhatIsNotAReadyWait(unittest.TestCase):

    def test_a_poll_with_no_background_start_is_not_reported(self):
        self.assertEqual(lines("""\
            for _ in $(seq 1 30); do
                curl -fs http://localhost && break
                sleep 1
            done
            """), [])

    def test_a_poll_before_the_start_is_not_reported(self):
        self.assertEqual(lines("""\
            until [ -e "$flag" ]; do
                sleep 1
            done
            worker &
            pid=$!
            """), [])

    def test_a_loop_without_sleep_is_not_reported(self):
        self.assertEqual(lines("""\
            worker &
            pid=$!
            for f in *.txt; do
                echo "$f"
            done
            """), [])

    def test_a_poll_after_the_child_was_waited_for_is_not_reported(self):
        # Once the script has reaped its child, a later loop cannot be waiting for
        # that child to become ready.
        self.assertEqual(lines("""\
            pw-record "$file" &
            REC_PID=$!
            wait "$REC_PID"
            for _ in $(seq 1 20); do
                gdbus call --method Ask && break
                sleep 0.1
            done
            """), [])

    def test_a_fire_and_forget_helper_does_not_arm_its_callers(self):
        # A function that backgrounds a signal and keeps no pid starts nothing that
        # anyone waits for, so calling it does not make the caller's loops waits.
        self.assertEqual(lines("""\
            emit_state() {
                gdbus emit --signal StateChanged "$1" &
            }
            emit_state READY
            for _ in $(seq 1 20); do
                gdbus call --method Ask && break
                sleep 0.1
            done
            """), [])

    def test_case_arms_are_alternatives(self):
        self.assertEqual(lines("""\
            case "$1" in
                capture)
                    reader &
                    pid=$!
                    ;;
                watch)
                    while true; do
                        cat /sys/power/state
                        sleep 1
                    done
                    ;;
            esac
            """), [])

    def test_a_sampler_that_runs_its_full_course_is_not_a_wait(self):
        # Sleeping once per sample, with no condition that ends it early, measures;
        # it does not wait for anything to become ready.
        self.assertEqual(lines("""\
            recorder &
            pid=$!
            for ((i = 0; i < secs; i++)); do
                sleep 1
                snap
            done
            end=$((SECONDS + secs))
            while ((SECONDS < end)); do
                cat state
                sleep 0.1
            done
            while true; do
                date
                sleep 1
            done
            """), [])

    def test_a_while_true_that_breaks_is_a_wait(self):
        self.assertEqual(lines("""\
            server &
            pid=$!
            while true; do
                [ -S "$sock" ] && break
                sleep 0.1
            done
            """), [3])

    def test_a_function_run_in_the_background_is_a_start_not_a_wait(self):
        self.assertEqual(lines("""\
            watch_it() {
                while [ -e "$flag" ]; do
                    sleep 1
                done
            }
            reader &
            pid=$!
            watch_it "$x" >"$out" 2>&1 &
            """), [])

    def test_a_backgrounded_loop_is_not_a_wait_for_itself(self):
        self.assertEqual(lines("""\
            while true; do
                date
                sleep 1
            done &
            """), [])


class WhatIsNotABackgroundStart(unittest.TestCase):

    def test_redirections_and_and_lists_are_not_launches(self):
        self.assertEqual(lines("""\
            exec {fd}<&0 </dev/null
            echo hi >&2 && echo there &>/dev/null
            cmd 2>&1 |& cat
            for _ in 1 2 3; do
                [ -e "$ready" ] && break
                sleep 1
            done
            """), [])

    def test_an_ampersand_in_a_string_or_comment_is_not_a_launch(self):
        self.assertEqual(lines("""\
            echo "run it &"   # then wait &
            printf '%s &\\n' x
            for _ in 1 2 3; do
                [ -e "$ready" ] && break
                sleep 1
            done
            """), [])

    def test_an_ampersand_in_a_heredoc_is_not_a_launch(self):
        self.assertEqual(lines("""\
            cat > "$f" <<'EOF'
            server &
            EOF
            for _ in 1 2 3; do
                [ -e "$ready" ] && break
                sleep 1
            done
            """), [])


class TheLexer(unittest.TestCase):

    def test_nested_quotes_in_a_command_substitution_stay_code(self):
        # "$( ... "inner" ... )" restarts quoting inside the substitution; a lexer
        # that pairs the inner quote with the outer one blanks the rest of the file.
        self.assertEqual(lines("""\
            check "x" "$([ -e "$events/a" ] && echo present || echo absent)"
            worker &
            pid=$!
            for _ in 1 2 3; do
                [ -e "$ready" ] && break
                sleep 1
            done
            """), [4])

    def test_a_dollar_hash_is_not_a_comment(self):
        self.assertEqual(lines("""\
            [ $# -gt 0 ] && n=${#items[@]}; worker &
            pid=$!
            for _ in 1 2 3; do
                [ -e "$ready" ] && break
                sleep 1
            done
            """), [3])

    def test_code_only_keeps_line_numbers(self):
        text = "a='x\ny'\n# c\nb \"q\nr\"\n"
        self.assertEqual(bash_ready_waits.code_only(text).count("\n"), text.count("\n"))


class Fixture(unittest.TestCase):
    """The rule's fixture is the contract the gate proves on every run."""

    def test_the_fixture_annotations_match_the_findings(self):
        fixture = REPO_ROOT / ".semgrep" / "ready-wait.bash"
        text = fixture.read_text(encoding="utf-8")
        self.assertNotEqual(bash_ready_waits.fixture_expectations(text), [],
                            "the fixture marks no line that must fire")
        self.assertEqual(bash_ready_waits.findings(text),
                         bash_ready_waits.fixture_expectations(text))

    def test_a_fixture_mismatch_fails_the_self_test(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "f.bash"
            path.write_text("# ruleid: ready-wait-ignores-child-exit\nuntil [ -e r ]; do sleep 1; done\n",
                            encoding="utf-8")
            self.assertEqual(bash_ready_waits.main(["--fixture", str(path)]), 1)


class CommandLine(unittest.TestCase):

    def test_findings_are_printed_one_per_line_and_exit_1(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "s.bash"
            path.write_text("w &\np=$!\nwhile :; do [ -e r ] && break; sleep 1; done\n", encoding="utf-8")
            clean = pathlib.Path(tmp) / "c.bash"
            clean.write_text("echo hi\n", encoding="utf-8")
            out = []
            rc = bash_ready_waits.main([str(path), str(clean)], out=out.append)
            self.assertEqual(rc, 1)
            self.assertEqual(out, [f"{path}:3: ready-wait-ignores-child-exit"])

    def test_a_clean_run_exits_0(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "c.bash"
            path.write_text("echo hi\n", encoding="utf-8")
            self.assertEqual(bash_ready_waits.main([str(path)], out=lambda _line: None), 0)

    def test_an_unreadable_file_is_an_error_not_a_pass(self):
        with self.assertRaises(OSError):
            bash_ready_waits.main(["/nonexistent/ready-wait.bash"], out=lambda _line: None)


if __name__ == "__main__":
    unittest.main()

"""SystemPathsHandler declares a rule id, and its acceptance tests drive the tool it guards (issue #294).

The handler judges Write and Edit tool calls by their file path. Its acceptance tests ran
`echo "Write to /etc/hosts"` through the Bash tool and expected a deny: a Bash echo is never a
Write, so the handler never saw the case it claimed to prove. Each acceptance test here is the
Write or Edit call itself, on a path that cannot exist, so a guard that missed it would still
change nothing (an Edit of a file that does not exist fails).
"""

import sys
from pathlib import Path

import pytest
from claude_code_hooks_daemon.daemon.playbook_harness import expand_project_dir

sys.path.insert(0, str(Path(__file__).parent.parent))

import system_paths
from system_paths import SystemPathsHandler

# The harness expands `$CLAUDE_PROJECT_DIR` in a payload before it dispatches it, and its probes
# act only under this directory of the project.
PROBE_ROOT = "$CLAUDE_PROJECT_DIR/untracked/acceptance/"


@pytest.fixture
def handler() -> SystemPathsHandler:
    return SystemPathsHandler()


def _write(path: str) -> dict:
    return {"tool_name": "Write", "tool_input": {"file_path": path, "content": "x"}}


def test_rule_id_is_stable():
    assert system_paths.RULE_ID == "R-SYSTEM-PATH-DEPLOYED-FILE-EDIT"


def test_get_rules_declares_the_rule(handler):
    assert [rule.rule_id for rule in handler.get_rules()] == [system_paths.RULE_ID]


@pytest.mark.parametrize("path", ["/etc/hosts", "/var/local/claude-yolo/claude-yolo", "/usr/bin/x", "/opt/x", "/root/x", "/home/x/y"])
def test_deny_reason_starts_with_the_rule_headline(handler, path):
    result = handler.handle(_write(path))
    assert result.decision == "deny"
    assert result.reason.startswith(f"BLOCKED [{system_paths.RULE_ID}]")


def test_acceptance_tests_drive_the_write_and_edit_tools(handler):
    tests = handler.get_acceptance_tests()
    assert tests
    for test in tests:
        assert test.tool_payload is not None, test.title
        assert test.tool_payload.tool_name in {"Write", "Edit"}, test.title


def _as_dispatched(test) -> dict:
    """The hook input the harness sends for ``test``: `$CLAUDE_PROJECT_DIR` expanded."""
    project_root = Path(system_paths._resolve_project_root())
    tool_input = {
        key: expand_project_dir(value, project_root) if isinstance(value, str) else value
        for key, value in test.tool_payload.tool_input.items()
    }
    return {"tool_name": test.tool_payload.tool_name, "tool_input": tool_input}


def test_acceptance_tests_get_the_verdict_they_expect(handler):
    for test in handler.get_acceptance_tests():
        expected = str(test.expected_decision) == "deny"
        assert handler.matches(_as_dispatched(test)) is expected, test.title


def test_acceptance_file_paths_are_absolute_when_dispatched(handler):
    """R-ABSOLUTE-PATH-REQUIRED (priority 12) denies a relative `file_path` in the live harness,
    before this handler could allow it: every probe path is absolute once the harness expanded it."""
    for test in handler.get_acceptance_tests():
        path = _as_dispatched(test)["tool_input"]["file_path"]
        assert path.startswith("/"), (test.title, path)
        raw = test.tool_payload.tool_input["file_path"]
        assert raw.startswith(("/", "$CLAUDE_PROJECT_DIR/")), (test.title, raw)


def test_the_allow_probe_acts_only_under_the_harness_probe_root(handler):
    """The harness refuses to act outside `untracked/acceptance/`, so the project-path probe lives there."""
    allows = [test for test in handler.get_acceptance_tests() if str(test.expected_decision) == "allow"]
    assert allows
    for test in allows:
        assert test.tool_payload.tool_input["file_path"].startswith(PROBE_ROOT), test.title


@pytest.mark.parametrize("path", ["/etc/hosts", "/usr/bin/x", "/opt/x"])
def test_the_deny_reason_carries_one_blocked_headline(handler, path):
    """The rule's headline is the only `BLOCKED`: no second headline line below it."""
    assert handler.handle(_write(path)).reason.count("BLOCKED") == 1


def test_acceptance_tests_would_change_nothing_if_the_guard_missed(handler):
    """An Edit needs an existing file, so one on a path that cannot exist fails harmlessly."""
    for test in handler.get_acceptance_tests():
        assert test.tool_payload.tool_name == "Edit", test.title
        assert "nonexistent" in test.tool_payload.tool_input["file_path"], test.title


def test_acceptance_tests_cover_a_block_and_an_allow(handler):
    decisions = {str(test.expected_decision) for test in handler.get_acceptance_tests()}
    assert {"deny", "allow"} <= decisions


def test_denying_acceptance_tests_pin_the_rule_id(handler):
    for test in handler.get_acceptance_tests():
        if str(test.expected_decision) == "deny":
            assert any(system_paths.RULE_ID in pattern for pattern in test.expected_message_patterns)

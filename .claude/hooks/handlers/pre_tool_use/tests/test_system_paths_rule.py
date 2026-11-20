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

sys.path.insert(0, str(Path(__file__).parent.parent))

import system_paths
from system_paths import SystemPathsHandler


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


def test_acceptance_tests_get_the_verdict_they_expect(handler):
    for test in handler.get_acceptance_tests():
        hook_input = {"tool_name": test.tool_payload.tool_name, "tool_input": test.tool_payload.tool_input}
        expected = str(test.expected_decision) == "deny"
        assert handler.matches(hook_input) is expected, test.title


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

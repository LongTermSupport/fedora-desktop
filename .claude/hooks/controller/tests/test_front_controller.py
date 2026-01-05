#!/usr/bin/env python3
"""Unit tests for front_controller module."""

import unittest
import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import HookResult, Handler, FrontController, get_bash_command, get_file_path, get_file_content


class TestHookResult(unittest.TestCase):
    """Test suite for HookResult class."""

    def test_allow_with_no_context_returns_empty_dict(self):
        """Silent allow should return empty dict."""
        result = HookResult("allow")
        output = result.to_json("PreToolUse")
        self.assertEqual(output, {})

    def test_deny_with_reason(self):
        """Deny with reason should include decision and reason."""
        result = HookResult("deny", reason="Test reason")
        output = result.to_json("PreToolUse")

        self.assertIn("hookSpecificOutput", output)
        hook_output = output["hookSpecificOutput"]
        self.assertEqual(hook_output["hookEventName"], "PreToolUse")
        self.assertEqual(hook_output["permissionDecision"], "deny")
        self.assertEqual(hook_output["permissionDecisionReason"], "Test reason")

    def test_allow_with_context(self):
        """Allow with context should include context."""
        result = HookResult("allow", context="Additional context")
        output = result.to_json("PreToolUse")

        self.assertIn("hookSpecificOutput", output)
        hook_output = output["hookSpecificOutput"]
        self.assertEqual(hook_output["additionalContext"], "Additional context")


class MockHandler(Handler):
    """Mock handler for testing."""

    def __init__(self, should_match=True, decision="allow"):
        super().__init__(name="mock-handler", priority=50)
        self.should_match = should_match
        self.decision = decision
        self.matches_called = False
        self.handle_called = False

    def matches(self, hook_input: dict) -> bool:
        self.matches_called = True
        return self.should_match

    def handle(self, hook_input: dict) -> HookResult:
        self.handle_called = True
        return HookResult(self.decision)


class TestFrontController(unittest.TestCase):
    """Test suite for FrontController class."""

    def test_register_handler(self):
        """Should register handler instance."""
        controller = FrontController("PreToolUse")
        handler = MockHandler()
        controller.register(handler)
        self.assertEqual(len(controller.handlers), 1)
        self.assertEqual(controller.handlers[0], handler)

    def test_priority_ordering(self):
        """Handlers should be sorted by priority (lower first)."""
        controller = FrontController("PreToolUse")

        high_priority = MockHandler()
        high_priority.priority = 10

        low_priority = MockHandler()
        low_priority.priority = 100

        # Register in wrong order
        controller.register(low_priority)
        controller.register(high_priority)

        # Should be sorted correctly
        self.assertEqual(controller.handlers[0], high_priority)
        self.assertEqual(controller.handlers[1], low_priority)

    def test_dispatch_to_matching_handler(self):
        """Should dispatch to first matching handler."""
        controller = FrontController("PreToolUse")
        matching_handler = MockHandler(should_match=True, decision="deny")
        controller.register(matching_handler)

        hook_input = {"tool_name": "Bash", "tool_input": {"command": "test"}}
        result = controller.dispatch(hook_input)

        self.assertTrue(matching_handler.matches_called)
        self.assertTrue(matching_handler.handle_called)
        self.assertEqual(result.decision, "deny")

    def test_no_match_returns_allow(self):
        """Should return allow if no handlers match."""
        controller = FrontController("PreToolUse")
        non_matching_handler = MockHandler(should_match=False)
        controller.register(non_matching_handler)

        hook_input = {"tool_name": "Bash", "tool_input": {"command": "test"}}
        result = controller.dispatch(hook_input)

        self.assertTrue(non_matching_handler.matches_called)
        self.assertFalse(non_matching_handler.handle_called)
        self.assertEqual(result.decision, "allow")

    def test_first_match_wins(self):
        """Should execute first matching handler and stop."""
        controller = FrontController("PreToolUse")

        first_handler = MockHandler(should_match=True, decision="deny")
        first_handler.priority = 10

        second_handler = MockHandler(should_match=True, decision="allow")
        second_handler.priority = 20

        controller.register(first_handler)
        controller.register(second_handler)

        hook_input = {"tool_name": "Bash", "tool_input": {"command": "test"}}
        result = controller.dispatch(hook_input)

        # First handler should execute
        self.assertTrue(first_handler.matches_called)
        self.assertTrue(first_handler.handle_called)

        # Second handler should check match but not execute
        self.assertFalse(second_handler.matches_called)
        self.assertFalse(second_handler.handle_called)

        self.assertEqual(result.decision, "deny")


class TestUtilityFunctions(unittest.TestCase):
    """Test suite for utility functions."""

    def test_get_bash_command_success(self):
        """Should extract bash command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git status"}
        }
        result = get_bash_command(hook_input)
        self.assertEqual(result, "git status")

    def test_get_bash_command_non_bash_tool(self):
        """Should return None for non-Bash tools."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/test.txt"}
        }
        result = get_bash_command(hook_input)
        self.assertIsNone(result)

    def test_get_file_path_success(self):
        """Should extract file path from Write/Edit."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/workspace/test.txt", "content": "test"}
        }
        result = get_file_path(hook_input)
        self.assertEqual(result, "/workspace/test.txt")

    def test_get_file_path_non_file_tool(self):
        """Should return None for non-Write/Edit tools."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "ls"}
        }
        result = get_file_path(hook_input)
        self.assertIsNone(result)

    def test_get_file_content_success(self):
        """Should extract file content from Write/Edit."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {"file_path": "/test.txt", "content": "file contents"}
        }
        result = get_file_content(hook_input)
        self.assertEqual(result, "file contents")


class TestFrontControllerEdgeCases(unittest.TestCase):
    """Test edge cases and error handling."""

    def test_empty_handlers_list(self):
        """Controller with no handlers should allow all."""
        controller = FrontController("PreToolUse")
        hook_input = {"tool_name": "Bash", "tool_input": {"command": "test"}}
        result = controller.dispatch(hook_input)
        self.assertEqual(result.decision, "allow")

    def test_multiple_handlers_same_priority(self):
        """Handlers with same priority should maintain registration order."""
        controller = FrontController("PreToolUse")

        handler1 = MockHandler(should_match=True, decision="deny")
        handler1.priority = 50
        handler1.name = "handler1"

        handler2 = MockHandler(should_match=True, decision="allow")
        handler2.priority = 50
        handler2.name = "handler2"

        controller.register(handler1)
        controller.register(handler2)

        hook_input = {"tool_name": "Bash", "tool_input": {"command": "test"}}
        result = controller.dispatch(hook_input)

        # First registered should win
        self.assertEqual(result.decision, "deny")
        self.assertTrue(handler1.handle_called)
        self.assertFalse(handler2.matches_called)

    def test_handler_with_negative_priority(self):
        """Handlers can have negative priorities."""
        controller = FrontController("PreToolUse")

        negative_handler = MockHandler(should_match=True, decision="deny")
        negative_handler.priority = -10

        positive_handler = MockHandler(should_match=True, decision="allow")
        positive_handler.priority = 10

        controller.register(positive_handler)
        controller.register(negative_handler)

        # Negative priority should run first
        self.assertEqual(controller.handlers[0], negative_handler)
        self.assertEqual(controller.handlers[1], positive_handler)

    def test_malformed_hook_input(self):
        """Dispatcher should handle malformed input gracefully."""
        controller = FrontController("PreToolUse")
        handler = MockHandler(should_match=True)
        controller.register(handler)

        # Missing tool_name
        result = controller.dispatch({})
        self.assertEqual(result.decision, "allow")

        # None as input
        result = controller.dispatch(None)
        # Should not crash, but behavior depends on handler


class TestHookResultEdgeCases(unittest.TestCase):
    """Test edge cases for HookResult."""

    def test_ask_decision(self):
        """Ask decision should be included in output."""
        result = HookResult("ask", reason="Need user input")
        output = result.to_json("PreToolUse")

        hook_output = output["hookSpecificOutput"]
        self.assertEqual(hook_output["permissionDecision"], "ask")
        self.assertEqual(hook_output["permissionDecisionReason"], "Need user input")

    def test_deny_without_reason(self):
        """Deny without reason should still work."""
        result = HookResult("deny")
        output = result.to_json("PreToolUse")

        hook_output = output["hookSpecificOutput"]
        self.assertEqual(hook_output["permissionDecision"], "deny")
        self.assertNotIn("permissionDecisionReason", hook_output)

    def test_allow_with_context_and_reason(self):
        """Allow with both context and reason."""
        result = HookResult("allow", reason="Some reason", context="Some context")
        output = result.to_json("PreToolUse")

        hook_output = output["hookSpecificOutput"]
        self.assertEqual(hook_output["additionalContext"], "Some context")
        # Reason should not be included for allow
        self.assertNotIn("permissionDecisionReason", hook_output)

    def test_different_event_names(self):
        """Should work with all event types."""
        event_types = ["PreToolUse", "PostToolUse", "UserPromptSubmit", "SubagentStop"]

        for event_type in event_types:
            result = HookResult("deny", reason="test")
            output = result.to_json(event_type)
            self.assertEqual(output["hookSpecificOutput"]["hookEventName"], event_type)


class TestUtilityFunctionsEdgeCases(unittest.TestCase):
    """Test edge cases for utility functions."""

    def test_get_bash_command_empty_command(self):
        """Should return empty string for empty command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": ""}
        }
        result = get_bash_command(hook_input)
        self.assertEqual(result, "")

    def test_get_bash_command_missing_tool_input(self):
        """Should handle missing tool_input."""
        hook_input = {"tool_name": "Bash"}
        result = get_bash_command(hook_input)
        self.assertEqual(result, "")

    def test_get_bash_command_missing_command_key(self):
        """Should return empty string if command key missing."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {}
        }
        result = get_bash_command(hook_input)
        self.assertEqual(result, "")

    def test_get_file_path_edit_tool(self):
        """Should work for Edit tool as well as Write."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {"file_path": "/test.txt"}
        }
        result = get_file_path(hook_input)
        self.assertEqual(result, "/test.txt")

    def test_get_file_path_empty(self):
        """Should return empty string for empty path."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": ""}
        }
        result = get_file_path(hook_input)
        self.assertEqual(result, "")

    def test_get_file_content_empty(self):
        """Should return empty string for empty content."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"content": ""}
        }
        result = get_file_content(hook_input)
        self.assertEqual(result, "")

    def test_get_file_content_missing_content_key(self):
        """Should return empty string if content key missing."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {}
        }
        result = get_file_content(hook_input)
        self.assertEqual(result, "")


if __name__ == '__main__':
    unittest.main()

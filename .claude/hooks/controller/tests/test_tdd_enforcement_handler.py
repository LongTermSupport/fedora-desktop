#!/usr/bin/env python3
"""Unit tests for TDD enforcement handler."""

import unittest
import sys
import os
import tempfile
from pathlib import Path
from unittest.mock import patch, MagicMock

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler, HookResult
from handlers.pre_tool_use.tdd_enforcement_handler import TddEnforcementHandler


class TestTddEnforcementHandler(unittest.TestCase):
    """Test suite for TddEnforcementHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = TddEnforcementHandler()

    def test_handler_initialization(self):
        """Should initialize with correct name and priority."""
        self.assertEqual(self.handler.name, "enforce-tdd")
        self.assertEqual(self.handler.priority, 15)

    # Test MATCHES - should match Write operations to handler files

    def test_matches_write_pre_tool_use_handler(self):
        """Should match Write operations to handlers/pre_tool_use/*.py files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/new_handler.py",
                "content": "class NewHandler(Handler):\n    pass"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_write_post_tool_use_handler(self):
        """Should match Write operations to handlers/post_tool_use/*.py files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/post_tool_use/validation_handler.py",
                "content": "class ValidationHandler(Handler):\n    pass"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_write_user_prompt_submit_handler(self):
        """Should match Write operations to handlers/user_prompt_submit/*.py files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/user_prompt_submit/prompt_handler.py",
                "content": "class PromptHandler(Handler):\n    pass"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_write_subagent_stop_handler(self):
        """Should match Write operations to handlers/subagent_stop/*.py files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/subagent_stop/agent_handler.py",
                "content": "class AgentHandler(Handler):\n    pass"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    # Test MATCHES - should NOT match non-handler files

    def test_does_not_match_non_handler_file(self):
        """Should NOT match Write operations to non-handler files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/front_controller.py",
                "content": "# Front controller code"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_test_file(self):
        """Should NOT match Write operations to test files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/tests/test_new_handler.py",
                "content": "# Test code"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_init_file(self):
        """Should NOT match __init__.py files in handler directories."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/__init__.py",
                "content": "# Init file"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_non_write_tools(self):
        """Should NOT match non-Write tools."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/existing_handler.py",
                "old_string": "old",
                "new_string": "new"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_non_python_files(self):
        """Should NOT match non-Python files in handler directories."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/README.md",
                "content": "# Documentation"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # Test HANDLE - when test file exists

    @patch('pathlib.Path.exists')
    def test_handle_allows_when_test_file_exists(self, mock_exists):
        """Should allow Write when corresponding test file exists."""
        mock_exists.return_value = True

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/new_handler.py",
                "content": "class NewHandler(Handler):\n    pass"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNone(result.reason)

    # Test HANDLE - when test file does NOT exist

    @patch('pathlib.Path.exists')
    def test_handle_denies_when_test_file_missing(self, mock_exists):
        """Should deny Write when test file doesn't exist."""
        mock_exists.return_value = False

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/new_handler.py",
                "content": "class NewHandler(Handler):\n    pass"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("TDD REQUIRED", result.reason)
        self.assertIn("new_handler.py", result.reason)
        self.assertIn("test_new_handler.py", result.reason)

    @patch('pathlib.Path.exists')
    def test_handle_includes_helpful_guidance(self, mock_exists):
        """Should include helpful guidance on creating test file first."""
        mock_exists.return_value = False

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/post_tool_use/validator.py",
                "content": "class Validator(Handler):\n    pass"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("Create the test file first", result.reason)
        self.assertIn("tests/test_validator.py", result.reason)
        self.assertIn("TDD", result.reason)

    @patch('pathlib.Path.exists')
    def test_handle_explains_philosophy(self, mock_exists):
        """Should explain TDD philosophy in denial message."""
        mock_exists.return_value = False

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/security_handler.py",
                "content": "class SecurityHandler(Handler):\n    pass"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertIn("Test-Driven Development", result.reason)
        self.assertIn("test first", result.reason.lower())

    # Test test file path resolution

    def test_get_test_file_path_pre_tool_use(self):
        """Should correctly resolve test file path for pre_tool_use handler."""
        handler_path = "/workspace/.claude/hooks/controller/handlers/pre_tool_use/git_handler.py"
        expected = Path("/workspace/.claude/hooks/controller/tests/test_git_handler.py")

        result = self.handler._get_test_file_path(handler_path)

        self.assertEqual(result, expected)

    def test_get_test_file_path_post_tool_use(self):
        """Should correctly resolve test file path for post_tool_use handler."""
        handler_path = "/workspace/.claude/hooks/controller/handlers/post_tool_use/eslint_validator.py"
        expected = Path("/workspace/.claude/hooks/controller/tests/test_eslint_validator.py")

        result = self.handler._get_test_file_path(handler_path)

        self.assertEqual(result, expected)

    def test_get_test_file_path_nested_handler(self):
        """Should extract just the filename for test file path."""
        handler_path = "/workspace/.claude/hooks/controller/handlers/subagent_stop/special/nested_handler.py"
        expected = Path("/workspace/.claude/hooks/controller/tests/test_nested_handler.py")

        result = self.handler._get_test_file_path(handler_path)

        self.assertEqual(result, expected)

    # Test edge cases

    @patch('pathlib.Path.exists')
    def test_handle_multiple_handlers_same_test_file(self, mock_exists):
        """Should handle case where multiple handlers might share a test file."""
        # Simulate test file exists for aggregated handlers
        mock_exists.return_value = True

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/hooks/controller/handlers/pre_tool_use/bash_handler.py",
                "content": "class BashHandler(Handler):\n    pass"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")

    def test_priority_runs_before_workflow_handlers(self):
        """Handler should run before workflow handlers (priority 15 < 25)."""
        # Priority 15 ensures it runs after safety (10-20) but before workflow (25-45)
        self.assertLess(self.handler.priority, 25)
        self.assertGreaterEqual(self.handler.priority, 10)


if __name__ == '__main__':
    unittest.main()

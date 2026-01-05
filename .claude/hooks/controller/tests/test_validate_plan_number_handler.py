#!/usr/bin/env python3
"""
Unit tests for ValidatePlanNumberHandler (PreToolUse version).

This handler validates plan folder numbering BEFORE directory creation,
fixing the timing bug where PostToolUse saw just-created directories.
"""

import unittest
import sys
import os
from pathlib import Path
from unittest.mock import patch, MagicMock

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# Import will fail until handler is created - this is expected in TDD
try:
    from handlers.pre_tool_use.validate_plan_number_handler import ValidatePlanNumberHandler
    HANDLER_EXISTS = True
except ImportError:
    HANDLER_EXISTS = False
    ValidatePlanNumberHandler = None


@unittest.skipUnless(HANDLER_EXISTS, "Handler not yet implemented")
class TestValidatePlanNumberHandler(unittest.TestCase):
    """Test suite for ValidatePlanNumberHandler (PreToolUse)."""

    def setUp(self):
        """Set up test handler."""
        self.handler = ValidatePlanNumberHandler()

    def test_handler_initialization(self):
        """Should initialise with correct name and priority."""
        self.assertEqual(self.handler.name, "validate-plan-number")
        self.assertEqual(self.handler.priority, 30)

    # =========================================================================
    # matches() tests
    # =========================================================================

    def test_matches_write_plan_file(self):
        """Should match Write operations creating plan files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/056-new-feature/PLAN.md",
                "content": "# Plan 056"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_bash_mkdir_plan(self):
        """Should match Bash mkdir creating plan folders."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "mkdir -p CLAUDE/Plan/057-another-feature"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_bash_mkdir_with_full_path(self):
        """Should match Bash mkdir with full /workspace path."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "mkdir -p /workspace/CLAUDE/Plan/058-test-feature"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_does_not_match_non_plan_file(self):
        """Should NOT match files outside CLAUDE/Plan/."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Sitemap/services.md",
                "content": "# Services"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_non_numbered_plan(self):
        """Should NOT match plan folders without NNN- prefix."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "mkdir CLAUDE/Plan/Completed"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_readme_plan(self):
        """Should NOT match README in Plan directory (no numbered folder)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/README.md",
                "content": "# Plans"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_read_tool(self):
        """Should NOT match Read operations."""
        hook_input = {
            "tool_name": "Read",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/056-feature/PLAN.md"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # =========================================================================
    # handle() tests - correct plan numbers
    # =========================================================================

    @patch.object(ValidatePlanNumberHandler, '_get_highest_plan_number', return_value=55)
    def test_handle_correct_plan_number(self, mock_highest):
        """Should allow without context if plan number is correct (highest + 1)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/056-new-feature/PLAN.md",
                "content": "# Plan 056"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNone(result.context)

    @patch.object(ValidatePlanNumberHandler, '_get_highest_plan_number', return_value=60)
    def test_handle_correct_plan_number_061(self, mock_highest):
        """
        KEY BUG FIX TEST: Creating 061 when highest is 060 should NOT warn.

        This is the exact scenario that triggered the bug report:
        - User ran official command, got 060
        - User created 061 (correct)
        - PostToolUse saw 061 as existing and wrongly said use 062

        With PreToolUse, we check BEFORE creation, so 061 is validated correctly.
        """
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/061-new-feature/PLAN.md",
                "content": "# Plan 061"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNone(result.context,
            "BUG: Handler incorrectly warned about plan 061 when highest is 060")

    @patch.object(ValidatePlanNumberHandler, '_get_highest_plan_number', return_value=0)
    def test_handle_first_plan_001(self, mock_highest):
        """Should allow plan 001 when no plans exist."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "mkdir -p CLAUDE/Plan/001-first-plan"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNone(result.context)

    # =========================================================================
    # handle() tests - incorrect plan numbers
    # =========================================================================

    @patch.object(ValidatePlanNumberHandler, '_get_highest_plan_number', return_value=55)
    def test_handle_incorrect_plan_number_too_high(self, mock_highest):
        """Should warn if plan number is too high (skipping numbers)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/057-new-feature/PLAN.md",
                "content": "# Plan 057"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNotNone(result.context)
        self.assertIn("PLAN NUMBER INCORRECT", result.context)
        self.assertIn("057", result.context)
        self.assertIn("056", result.context)  # Expected number

    @patch.object(ValidatePlanNumberHandler, '_get_highest_plan_number', return_value=55)
    def test_handle_incorrect_plan_number_duplicate(self, mock_highest):
        """Should warn if plan number duplicates existing."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/055-duplicate/PLAN.md",
                "content": "# Plan 055"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNotNone(result.context)
        self.assertIn("PLAN NUMBER INCORRECT", result.context)

    @patch.object(ValidatePlanNumberHandler, '_get_highest_plan_number', return_value=55)
    def test_handle_incorrect_plan_number_too_low(self, mock_highest):
        """Should warn if plan number is too low."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "mkdir -p CLAUDE/Plan/050-old-number"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNotNone(result.context)
        self.assertIn("PLAN NUMBER INCORRECT", result.context)

    @patch.object(ValidatePlanNumberHandler, '_get_highest_plan_number', return_value=0)
    def test_handle_incorrect_first_plan_not_001(self, mock_highest):
        """Should warn if first plan is not 001."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/005-wrong-start/PLAN.md",
                "content": "# Plan 005"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNotNone(result.context)
        self.assertIn("PLAN NUMBER INCORRECT", result.context)
        self.assertIn("001", result.context)  # Expected first number

    # =========================================================================
    # _get_highest_plan_number() tests
    # =========================================================================

    @patch('pathlib.Path.exists', return_value=True)
    @patch('pathlib.Path.iterdir')
    def test_get_highest_plan_number_with_plans(self, mock_iterdir, mock_exists):
        """Should find highest plan number from both active and completed."""
        # Mock active plans
        plan_051 = MagicMock()
        plan_051.name = '051-feature'
        plan_051.is_dir.return_value = True

        plan_055 = MagicMock()
        plan_055.name = '055-another'
        plan_055.is_dir.return_value = True

        completed_dir = MagicMock()
        completed_dir.name = 'Completed'
        completed_dir.is_dir.return_value = True

        # Mock completed plans
        plan_048 = MagicMock()
        plan_048.name = '048-old'
        plan_048.is_dir.return_value = True

        plan_052 = MagicMock()
        plan_052.name = '052-archived'
        plan_052.is_dir.return_value = True

        mock_iterdir.side_effect = [
            iter([plan_051, plan_055, completed_dir]),
            iter([plan_048, plan_052])
        ]

        highest = self.handler._get_highest_plan_number()

        self.assertEqual(highest, 55)

    @patch('pathlib.Path.exists', return_value=True)
    @patch('pathlib.Path.iterdir')
    def test_get_highest_plan_number_completed_higher(self, mock_iterdir, mock_exists):
        """Should return completed plan number if it's higher than active."""
        # Mock active plans
        plan_051 = MagicMock()
        plan_051.name = '051-active'
        plan_051.is_dir.return_value = True

        completed_dir = MagicMock()
        completed_dir.name = 'Completed'
        completed_dir.is_dir.return_value = True

        # Mock completed plans - higher number
        plan_060 = MagicMock()
        plan_060.name = '060-archived'
        plan_060.is_dir.return_value = True

        mock_iterdir.side_effect = [
            iter([plan_051, completed_dir]),
            iter([plan_060])
        ]

        highest = self.handler._get_highest_plan_number()

        self.assertEqual(highest, 60)

    @patch('pathlib.Path.exists', return_value=False)
    def test_get_highest_plan_number_no_plans(self, mock_exists):
        """Should return 0 if no plans exist."""
        highest = self.handler._get_highest_plan_number()
        self.assertEqual(highest, 0)

    @patch('pathlib.Path.exists', return_value=True)
    @patch('pathlib.Path.iterdir')
    def test_get_highest_plan_number_empty_directory(self, mock_iterdir, mock_exists):
        """Should return 0 if Plan directory is empty."""
        mock_iterdir.return_value = iter([])

        highest = self.handler._get_highest_plan_number()
        self.assertEqual(highest, 0)

    # =========================================================================
    # PreToolUse timing verification tests
    # =========================================================================

    @patch('pathlib.Path.exists', return_value=True)
    @patch('pathlib.Path.iterdir')
    def test_pretooluse_timing_no_false_warning(self, mock_iterdir, mock_exists):
        """
        Verify PreToolUse timing: creating 061 when 060 exists should NOT warn.

        This test uses real mocked filesystem to verify the timing fix works.
        In PostToolUse, this would fail because 061 would already exist.
        In PreToolUse, we check BEFORE creation, so 060 is the highest.
        """
        # Simulate: only 060 exists (we haven't created 061 yet)
        plan_060 = MagicMock()
        plan_060.name = '060-existing'
        plan_060.is_dir.return_value = True

        completed_dir = MagicMock()
        completed_dir.name = 'Completed'
        completed_dir.is_dir.return_value = True

        mock_iterdir.side_effect = [
            iter([plan_060, completed_dir]),
            iter([])
        ]

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/061-new/PLAN.md",
                "content": "# Plan 061"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNone(result.context,
            "PreToolUse timing bug: Handler saw 061 as existing before it was created")


if __name__ == '__main__':
    unittest.main()

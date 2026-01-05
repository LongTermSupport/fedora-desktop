#!/usr/bin/env python3
"""
Unit tests for OfficialPlanCommandHandler.

This handler enforces use of the official plan number discovery command
and blocks fragile ad-hoc alternatives.
"""

import unittest
import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from handlers.pre_tool_use.official_plan_command_handler import OfficialPlanCommandHandler


class TestOfficialPlanCommandHandler(unittest.TestCase):
    """Test suite for OfficialPlanCommandHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = OfficialPlanCommandHandler()

    def test_handler_initialization(self):
        """Should initialise with correct name and priority."""
        self.assertEqual(self.handler.name, "enforce-official-plan-command")
        self.assertEqual(self.handler.priority, 25)

    # =========================================================================
    # matches() tests - should BLOCK these ad-hoc commands
    # =========================================================================

    def test_matches_ls_claude_plan(self):
        """Should match (block) ls CLAUDE/Plan/ variations."""
        ad_hoc_commands = [
            "ls CLAUDE/Plan/",
            "ls -la CLAUDE/Plan/",
            "ls -1 CLAUDE/Plan/",
            "ls CLAUDE/Plan",
        ]
        for cmd in ad_hoc_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertTrue(self.handler.matches(hook_input),
                          f"Should match (block): {cmd}")

    def test_matches_ls_plan_with_pattern(self):
        """Should match (block) ls with plan number patterns."""
        ad_hoc_commands = [
            "ls CLAUDE/Plan/0*",
            "ls -d CLAUDE/Plan/[0-9]*",
            "ls */Plan/[0-9]*",
        ]
        for cmd in ad_hoc_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertTrue(self.handler.matches(hook_input),
                          f"Should match (block): {cmd}")

    def test_matches_ls_plan_with_grep(self):
        """Should match (block) ls CLAUDE/Plan | grep patterns."""
        ad_hoc_commands = [
            "ls CLAUDE/Plan | grep '^[0-9]'",
            "ls -1 CLAUDE/Plan | grep -E '^[0-9]{3}'",
        ]
        for cmd in ad_hoc_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertTrue(self.handler.matches(hook_input),
                          f"Should match (block): {cmd}")

    def test_matches_cd_plan_then_ls(self):
        """Should match (block) cd into Plan then list."""
        ad_hoc_commands = [
            "cd CLAUDE/Plan && ls",
            "cd CLAUDE/Plan && ls -la",
        ]
        for cmd in ad_hoc_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertTrue(self.handler.matches(hook_input),
                          f"Should match (block): {cmd}")

    def test_matches_find_wrong_maxdepth(self):
        """Should match (block) find with wrong maxdepth."""
        ad_hoc_commands = [
            "find CLAUDE/Plan",
            "find CLAUDE/Plan -maxdepth 1",
            "find CLAUDE/Plan -type d",
        ]
        for cmd in ad_hoc_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertTrue(self.handler.matches(hook_input),
                          f"Should match (block): {cmd}")

    # =========================================================================
    # matches() tests - should NOT block these legitimate operations
    # =========================================================================

    def test_no_match_official_command(self):
        """Should NOT match (allow) the official plan number discovery command."""
        official_command = (
            "find CLAUDE/Plan -maxdepth 2 -type d -name '[0-9]*' | "
            "sed 's|.*/\\([0-9]\\{3\\}\\).*|\\1|' | sort -n | tail -1"
        )
        hook_input = {"tool_name": "Bash", "tool_input": {"command": official_command}}
        self.assertFalse(self.handler.matches(hook_input),
                        "Should NOT match official command")

    def test_no_match_official_command_whitespace_variation(self):
        """Should NOT match official command with extra whitespace."""
        # Test with extra spaces
        official_command = (
            "find CLAUDE/Plan  -maxdepth  2  -type d  -name '[0-9]*'  | "
            "sed 's|.*/\\([0-9]\\{3\\}\\).*|\\1|' |  sort -n  |  tail -1"
        )
        hook_input = {"tool_name": "Bash", "tool_input": {"command": official_command}}
        self.assertFalse(self.handler.matches(hook_input),
                        "Should NOT match official command (whitespace variant)")

    def test_no_match_plan_archival_operations(self):
        """Should NOT match legitimate plan archival/organization operations."""
        archival_commands = [
            "mkdir -p CLAUDE/Plan/Completed && mv CLAUDE/Plan/060-test CLAUDE/Plan/Completed/",
            "mv CLAUDE/Plan/060-done CLAUDE/Plan/Completed/",
            "mkdir -p CLAUDE/Plan/Archive && mv CLAUDE/Plan/050-old CLAUDE/Plan/Archive/",
            "cp -r CLAUDE/Plan/060-backup CLAUDE/Plan/Backup/",
            "mkdir -p CLAUDE/Plan/Completed",
            "mkdir -p CLAUDE/Plan/Archive",
            "mkdir -p CLAUDE/Plan/Backup",
            "mv CLAUDE/Plan/055-feature CLAUDE/Plan/Completed/055-feature",
        ]
        for cmd in archival_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertFalse(self.handler.matches(hook_input),
                           f"Should NOT match archival: {cmd}")

    def test_no_match_plan_creation(self):
        """Should NOT match plan creation operations."""
        creation_commands = [
            "mkdir -p CLAUDE/Plan/061-new-feature",
            "mkdir CLAUDE/Plan/062-another-plan",
        ]
        for cmd in creation_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertFalse(self.handler.matches(hook_input),
                           f"Should NOT match creation: {cmd}")

    def test_no_match_non_bash_tool(self):
        """Should NOT match non-Bash tools."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/061-test/PLAN.md",
                "content": "# Plan 061"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_non_plan_operations(self):
        """Should NOT match operations not involving CLAUDE/Plan."""
        non_plan_commands = [
            "ls src/",
            "find . -name '*.py'",
            "ls -la",
        ]
        for cmd in non_plan_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertFalse(self.handler.matches(hook_input),
                           f"Should NOT match non-plan: {cmd}")

    # =========================================================================
    # handle() tests
    # =========================================================================

    def test_handle_blocks_with_guidance(self):
        """Should block ad-hoc command with helpful guidance."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "ls CLAUDE/Plan/0*"}
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("Ad-hoc plan discovery", result.reason)
        self.assertIn("OFFICIAL COMMAND", result.reason)
        self.assertIn("find CLAUDE/Plan -maxdepth 2", result.reason)
        self.assertIn("CLAUDE/Plan/CLAUDE.md", result.reason)


if __name__ == '__main__':
    unittest.main()

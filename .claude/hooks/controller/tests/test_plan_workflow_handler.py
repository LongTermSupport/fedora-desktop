"""Tests for PlanWorkflowHandler - provides guidance for plan creation."""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import unittest
from handlers.pre_tool_use.plan_workflow_handler import PlanWorkflowHandler


class TestPlanWorkflowHandler(unittest.TestCase):
    """Test suite for PlanWorkflowHandler."""

    def setUp(self):
        """Set up test fixtures."""
        self.handler = PlanWorkflowHandler()

    # Positive cases - should match and provide guidance

    def test_matches_plan_md_write(self):
        """Should match when writing PLAN.md in CLAUDE/Plan/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE/Plan/061-new-feature/PLAN.md",
                "content": "# Plan 061: New Feature\n\n## Overview\nImplementing new feature"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_nested_plan_md(self):
        """Should match nested plan directories."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE/Plan/062-complex-feature/phase-1/PLAN.md",
                "content": "# Phase 1"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_handle_provides_guidance(self):
        """Should provide guidance about plan workflow."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE/Plan/061-new-feature/PLAN.md",
                "content": "# Plan 061"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNotNone(result.guidance)
        self.assertIn("status icons", result.guidance.lower())
        self.assertIn("success criteria", result.guidance.lower())

    def test_guidance_mentions_task_icons(self):
        """Guidance should mention specific task status icons."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE/Plan/061-test/PLAN.md",
                "content": "# Plan"
            }
        }
        result = self.handler.handle(hook_input)

        # Should mention the specific icons to use
        self.assertIn("⬜", result.guidance)  # Not started
        self.assertIn("🔄", result.guidance)  # In progress
        self.assertIn("✅", result.guidance)  # Completed

    # Negative cases - should NOT match

    def test_no_match_edit_tool(self):
        """Should not match Edit tool (only Write)."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "CLAUDE/Plan/061-test/PLAN.md",
                "old_string": "old",
                "new_string": "new"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_other_tool(self):
        """Should not match non-Write tools."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "ls"}
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_non_plan_file(self):
        """Should not match files outside CLAUDE/Plan/."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/components/Button.tsx",
                "content": "export const Button = () => <button />"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_plan_supporting_docs(self):
        """Should not match supporting docs in plan directories."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE/Plan/061-test/analysis.md",
                "content": "# Analysis"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_uppercase_variations(self):
        """Should match case-insensitive PLAN.md variations."""
        variations = [
            "CLAUDE/Plan/061-test/PLAN.md",
            "CLAUDE/Plan/061-test/Plan.md",
            "CLAUDE/Plan/061-test/plan.md",
        ]
        for path in variations:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": path,
                    "content": "# Plan"
                }
            }
            self.assertTrue(self.handler.matches(hook_input), f"Should match {path}")

    # Handler properties

    def test_handler_name(self):
        """Should have descriptive handler name."""
        self.assertEqual(self.handler.name, "plan-workflow-guidance")

    def test_handler_priority(self):
        """Should have appropriate priority (guidance, not critical)."""
        # Guidance handlers should run later (higher priority number)
        self.assertGreater(self.handler.priority, 40)
        self.assertLess(self.handler.priority, 60)

    # Edge cases

    def test_no_crash_missing_file_path(self):
        """Should handle missing file_path gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "content": "test"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_crash_empty_file_path(self):
        """Should handle empty file_path gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "",
                "content": "test"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))


if __name__ == '__main__':
    unittest.main()

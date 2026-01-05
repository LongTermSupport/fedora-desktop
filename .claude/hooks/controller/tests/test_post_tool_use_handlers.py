#!/usr/bin/env python3
"""Comprehensive unit tests for PostToolUse handlers."""

import unittest
import sys
import os
from pathlib import Path
from unittest.mock import patch, MagicMock, mock_open

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler, HookResult
from handlers.post_tool_use.file_handlers import (
    ValidateEslintOnWriteHandler,
    ValidateSitemapHandler,
)

# NOTE: ValidatePlanNumberHandler moved to PreToolUse
# Tests are now in: test_validate_plan_number_handler.py


class TestValidateEslintOnWriteHandler(unittest.TestCase):
    """Test suite for ValidateEslintOnWriteHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = ValidateEslintOnWriteHandler()

    def test_handler_initialization(self):
        """Should initialize with correct name and priority."""
        self.assertEqual(self.handler.name, "validate-eslint-on-write")
        self.assertEqual(self.handler.priority, 10)

    def test_matches_write_tsx_file(self):
        """Should match Write operations on .tsx files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/components/Button.tsx",
                "content": "export const Button = () => <button>Click</button>;"
            }
        }

        with patch('os.path.exists', return_value=True):
            self.assertTrue(self.handler.matches(hook_input))

    def test_matches_edit_ts_file(self):
        """Should match Edit operations on .ts files."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/src/utils/helpers.ts",
                "old_string": "const x = 1",
                "new_string": "const x = 2"
            }
        }

        with patch('os.path.exists', return_value=True):
            self.assertTrue(self.handler.matches(hook_input))

    def test_does_not_match_non_ts_file(self):
        """Should NOT match non-TypeScript files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/README.md",
                "content": "# Test"
            }
        }

        with patch('os.path.exists', return_value=True):
            self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_node_modules(self):
        """Should NOT match files in node_modules."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/node_modules/some-lib/index.ts",
                "content": "export const x = 1"
            }
        }

        with patch('os.path.exists', return_value=True):
            self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_dist_files(self):
        """Should NOT match files in dist directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/dist/bundle.ts",
                "content": "export const x = 1"
            }
        }

        with patch('os.path.exists', return_value=True):
            self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_nonexistent_file(self):
        """Should NOT match if file doesn't exist (PostToolUse check)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/missing.ts",
                "content": "export const x = 1"
            }
        }

        with patch('os.path.exists', return_value=False):
            self.assertFalse(self.handler.matches(hook_input))

    @patch('subprocess.run')
    def test_handle_eslint_pass(self, mock_run):
        """Should allow if ESLint passes."""
        mock_run.return_value = MagicMock(
            returncode=0,
            stdout="",
            stderr=""
        )

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.tsx",
                "content": "export const x = 1"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNone(result.reason)

    @patch('subprocess.run')
    def test_handle_eslint_fail(self, mock_run):
        """Should deny if ESLint fails."""
        mock_run.return_value = MagicMock(
            returncode=1,
            stdout="Error: Missing semicolon",
            stderr=""
        )

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.tsx",
                "content": "export const x = 1"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("ESLint validation FAILED", result.reason)
        self.assertIn("Missing semicolon", result.reason)

    @patch('subprocess.run')
    def test_handle_eslint_timeout(self, mock_run):
        """Should deny if ESLint times out."""
        from subprocess import TimeoutExpired
        mock_run.side_effect = TimeoutExpired(cmd=['eslint'], timeout=30)

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.tsx",
                "content": "export const x = 1"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("timed out", result.reason)

    @patch('subprocess.run')
    def test_handle_worktree_file(self, mock_run):
        """Should handle worktree files with wrapper script."""
        mock_run.return_value = MagicMock(returncode=0, stdout="", stderr="")

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/untracked/worktrees/feature/src/test.tsx",
                "content": "export const x = 1"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        # Verify wrapper script was called
        mock_run.assert_called_once()
        call_args = mock_run.call_args
        self.assertIn('tsx', call_args[0][0][0])
        self.assertIn('eslint-wrapper.ts', call_args[0][0][1])


class TestValidateSitemapHandler(unittest.TestCase):
    """Test suite for ValidateSitemapHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = ValidateSitemapHandler()

    def test_handler_initialization(self):
        """Should initialize with correct name and priority."""
        self.assertEqual(self.handler.name, "validate-sitemap-on-edit")
        self.assertEqual(self.handler.priority, 20)

    def test_matches_sitemap_md_write(self):
        """Should match Write operations on sitemap .md files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Sitemap/services.md",
                "content": "# Services"
            }
        }

        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_sitemap_md_edit(self):
        """Should match Edit operations on sitemap .md files."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Sitemap/about.md",
                "old_string": "old",
                "new_string": "new"
            }
        }

        self.assertTrue(self.handler.matches(hook_input))

    def test_does_not_match_sitemap_claude_md(self):
        """Should NOT match CLAUDE/Sitemap/CLAUDE.md (docs file)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Sitemap/CLAUDE.md",
                "content": "# Docs"
            }
        }

        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_non_sitemap_file(self):
        """Should NOT match files outside CLAUDE/Sitemap/."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/055.md",
                "content": "# Plan"
            }
        }

        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_non_md_file(self):
        """Should NOT match non-markdown files in sitemap dir."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Sitemap/data.json",
                "content": "{}"
            }
        }

        self.assertFalse(self.handler.matches(hook_input))

    def test_handle_adds_validation_reminder(self):
        """Should add reminder to validate sitemap after editing."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Sitemap/services.md",
                "old_string": "old",
                "new_string": "new"
            }
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNotNone(result.context)
        self.assertIn("Sitemap file modified", result.context)
        self.assertIn("sitemap-validator", result.context)
        self.assertIn("services.md", result.context)


# NOTE: TestValidatePlanNumberHandler was moved to test_validate_plan_number_handler.py
# The handler was moved from PostToolUse to PreToolUse to fix a timing bug where
# PostToolUse saw just-created directories as "existing".


if __name__ == '__main__':
    unittest.main()

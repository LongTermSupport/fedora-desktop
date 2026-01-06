#!/usr/bin/env python3
"""Tests for AbsolutePathHandler - prevents /workspace/ absolute paths."""

import unittest
import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import HookResult
from handlers.pre_tool_use import AbsolutePathHandler


class TestAbsolutePathHandler(unittest.TestCase):
    """Test absolute path validation for Write and Edit operations."""

    def setUp(self):
        self.handler = AbsolutePathHandler()

    # ========================================
    # NEW TESTS: Check file CONTENT, not file_path parameter
    # ========================================

    def test_allows_absolute_filepath_parameter(self):
        """Should ALLOW absolute paths in file_path parameter (tool parameters can be absolute)."""
        # This is fine - the file_path parameter can be absolute
        # We only care about /workspace/ paths INSIDE the code content
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/foo.ts",
                "content": "import x from './bar.ts';\nconsole.log('test');"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_absolute_filepath_parameter_edit(self):
        """Should ALLOW absolute paths in Edit file_path parameter."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/src/foo.ts",
                "old_string": "old",
                "new_string": "new"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_blocks_workspace_in_write_content(self):
        """Should BLOCK /workspace/ paths in Write content."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/foo.ts",
                "content": "import x from '/workspace/bar.ts';\nconsole.log('hello');"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_blocks_workspace_in_write_content_multiple(self):
        """Should BLOCK when Write content has multiple /workspace/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/config.ts",
                "content": """
                    const dataPath = '/workspace/data';
                    const configPath = '/workspace/config.json';
                """
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_blocks_workspace_in_edit_new_string(self):
        """Should BLOCK /workspace/ paths in Edit new_string."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "src/foo.ts",
                "old_string": "old",
                "new_string": "const path = '/workspace/data/file.json';"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_blocks_workspace_in_edit_old_string(self):
        """Should BLOCK /workspace/ paths in Edit old_string (detecting existing bad code)."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "src/foo.ts",
                "old_string": "const bad = '/workspace/data';",
                "new_string": "const good = './data';"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_shows_content_snippet_in_error(self):
        """Should show the problematic content snippet in error message."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/config.ts",
                "content": "const dataDir = '/workspace/data';\nconst configFile = '/workspace/config.json';"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIsNotNone(result.reason)
        self.assertIn("/workspace/", result.reason)
        # Should show content snippet, not file_path
        self.assertIn("content", result.reason.lower())
        self.assertIn("const dataDir", result.reason)

    def test_shows_new_string_snippet_in_error_edit(self):
        """Should show new_string snippet for Edit operations."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "src/foo.ts",
                "old_string": "old",
                "new_string": "import data from '/workspace/data.json';"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("import data", result.reason)
        self.assertIn("/workspace/data.json", result.reason)

    # ========================================
    # OLD TESTS: Now these should PASS (allow operations)
    # These were incorrectly blocking before
    # ========================================

    def test_matches_workspace_absolute_path_write(self):
        """OLD TEST: Now should ALLOW - file_path parameter can be absolute."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "content": "console.log('test');"
            }
        }
        # Should NOT match (allow operation) - no /workspace/ in content
        self.assertFalse(self.handler.matches(hook_input))

    def test_matches_workspace_absolute_path_edit(self):
        """OLD TEST: Now should ALLOW - file_path parameter can be absolute."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/001-test/PLAN.md",
                "old_string": "old",
                "new_string": "new"
            }
        }
        # Should NOT match - no /workspace/ in old_string or new_string
        self.assertFalse(self.handler.matches(hook_input))

    def test_matches_workspace_with_trailing_content(self):
        """OLD TEST: Now should ALLOW - file_path parameter can be absolute."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/deeply/nested/directory/file.js",
                "content": "test"
            }
        }
        # Should NOT match - no /workspace/ in content
        self.assertFalse(self.handler.matches(hook_input))

    def test_blocks_with_clear_reason(self):
        """OLD TEST: Updated to actually have /workspace/ in CONTENT."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/components/Button.tsx",
                "content": "import styles from '/workspace/src/components/Button.module.css';\nexport const Button = () => {};"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIsNotNone(result.reason)
        self.assertIn("/workspace/", result.reason)
        self.assertIn("content", result.reason.lower())

    # Negative tests (should NOT match, allow operation)

    def test_ignores_relative_paths(self):
        """Should NOT match relative paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/test.ts",
                "content": "console.log('test');"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_dot_relative_paths(self):
        """Should NOT match ./ relative paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "./src/test.ts",
                "content": "console.log('test');"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_parent_relative_paths(self):
        """Should NOT match ../ relative paths."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "../other-project/file.ts",
                "old_string": "old",
                "new_string": "new"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_other_absolute_paths(self):
        """Should NOT match other absolute paths (only /workspace/)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/tmp/project/src/test.ts",
                "content": "console.log('test');"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_non_file_tools(self):
        """Should NOT match non-Write/Edit tools."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "ls /workspace/"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_read_tool(self):
        """Should NOT match Read tool (read-only operation)."""
        hook_input = {
            "tool_name": "Read",
            "tool_input": {
                "file_path": "/workspace/src/test.ts"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # Edge cases

    def test_handles_empty_file_path(self):
        """Should handle empty file_path gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "",
                "content": "test"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handles_missing_file_path(self):
        """Should handle missing file_path gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "content": "test"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handles_empty_content(self):
        """Should handle empty content gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/test.ts",
                "content": ""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handles_missing_content(self):
        """Should handle missing content gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/test.ts"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_case_sensitive_workspace(self):
        """Should be case-sensitive for /workspace/."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/test.ts",
                "content": "const path = '/WORKSPACE/data';"
            }
        }
        # /WORKSPACE/ is different from /workspace/ - should NOT match
        self.assertFalse(self.handler.matches(hook_input))

    def test_workspace_in_middle_of_content(self):
        """Should match /workspace/ anywhere in content."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/test.ts",
                "content": "// This path /workspace/data is hardcoded\nconst x = 1;"
            }
        }
        # Should match - /workspace/ appears in content (even in comment)
        self.assertTrue(self.handler.matches(hook_input))

    def test_suggestion_strips_workspace_prefix(self):
        """OLD TEST: No longer applies - we don't suggest path replacements for content."""
        # This test is obsolete since we're no longer checking file_path
        # We're checking content, and there's no simple "fix" to suggest
        pass


if __name__ == '__main__':
    unittest.main()

#!/usr/bin/env python3
"""Tests for SedBlockerHandler - blocks sed command usage."""

import unittest
import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import HookResult
from handlers.pre_tool_use import SedBlockerHandler


class TestSedBlockerHandler(unittest.TestCase):
    """Test sed command blocking for safety."""

    def setUp(self):
        self.handler = SedBlockerHandler()

    # Positive tests (should MATCH and BLOCK)

    def test_matches_direct_sed_command(self):
        """Should match direct sed command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "sed -i 's/foo/bar/g' file.txt"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_sed_in_place_edit(self):
        """Should match sed in-place editing."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "sed -i.bak 's/old/new/g' src/test.ts"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_sed_with_find_exec(self):
        """Should match sed within find -exec."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "find . -name '*.ts' -exec sed -i 's/foo/bar/g' {} \\;"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_sed_in_pipeline(self):
        """Should match sed in pipeline."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "cat file.txt | sed 's/old/new/g' > output.txt"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_sed_with_multiple_commands(self):
        """Should match sed in command chain."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "git diff && sed -i 's/foo/bar/g' file.txt && git commit"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_sed_case_variations(self):
        """Should match sed with different spacing."""
        test_cases = [
            "sed -i 's/old/new/g' file.txt",
            " sed -i 's/old/new/g' file.txt",  # leading space
            "sed  -i 's/old/new/g' file.txt",  # double space
            "sed\t-i 's/old/new/g' file.txt",  # tab after sed
        ]

        for cmd in test_cases:
            with self.subTest(command=cmd):
                hook_input = {
                    "tool_name": "Bash",
                    "tool_input": {"command": cmd}
                }
                self.assertTrue(self.handler.matches(hook_input))

    def test_matches_write_shell_script_with_sed(self):
        """Should match Write tool creating .sh file containing sed."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "scripts/update.sh",
                "content": "#!/bin/bash\nfind . -name '*.ts' -exec sed -i 's/old/new/g' {} \\;\n"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_write_bash_script_with_sed(self):
        """Should match Write tool creating .bash file containing sed."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "update.bash",
                "content": "sed -i 's/foo/bar/g' *.txt"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_blocks_with_clear_reason(self):
        """Should block with comprehensive error message."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "sed -i 's/foo/bar/g' file.txt"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIsNotNone(result.reason)
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("sed", result.reason)
        self.assertIn("FORBIDDEN", result.reason)
        self.assertIn("corruption", result.reason.lower())
        self.assertIn("haiku agents", result.reason.lower())
        self.assertIn("Edit tool", result.reason)
        # Should include the blocked command
        self.assertIn("sed -i 's/foo/bar/g' file.txt", result.reason)

    # Negative tests (should NOT match, allow operation)

    def test_ignores_grep_sed(self):
        """Should NOT match grep searching for 'sed'."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "grep -r 'sed' ."
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_echo_sed(self):
        """Should NOT match echo mentioning sed."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "echo 'Do not use sed command'"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_file_paths_with_sed(self):
        """Should NOT match file paths containing 'sed'."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "cat src/based/test.ts"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_read_tool(self):
        """Should NOT match Read tool (read-only)."""
        hook_input = {
            "tool_name": "Read",
            "tool_input": {
                "file_path": "scripts/old-sed-script.sh"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_non_bash_non_write_tools(self):
        """Should NOT match non-Bash/Write tools."""
        hook_input = {
            "tool_name": "Grep",
            "tool_input": {
                "pattern": "sed",
                "path": "."
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_write_non_script_with_sed_content(self):
        """Should NOT match Write to non-script files even with sed in content."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "README.md",
                "content": "The sed command is dangerous, never use: sed -i 's/old/new/g'"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_write_typescript_with_sed_string(self):
        """Should NOT match Write to .ts file with sed in string."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/test.ts",
                "content": "const cmd = \"sed -i 's/old/new/g' file.txt\";"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_sed_in_quoted_string(self):
        """Should NOT match sed in quoted strings (grep example)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "echo 'The sed command is blocked' > output.txt"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_sed_as_part_of_word(self):
        """Should NOT match 'sed' as part of another word."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "ls -la /tmp/based/files"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # Edge cases

    def test_handles_empty_command(self):
        """Should handle empty command gracefully."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": ""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handles_missing_command(self):
        """Should handle missing command gracefully."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {}
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handles_empty_file_content(self):
        """Should handle empty file content gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "script.sh",
                "content": ""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_word_boundary_detection(self):
        """Should use word boundaries to match sed command."""
        # Should match (sed as command)
        match_cases = [
            "sed -i 's/old/new/g' file.txt",
            "| sed 's/old/new/g'",
            "(sed -i 's/old/new/g' file.txt)",
            ";sed -i 's/old/new/g' file.txt",
        ]

        for cmd in match_cases:
            with self.subTest(command=cmd, should_match=True):
                hook_input = {
                    "tool_name": "Bash",
                    "tool_input": {"command": cmd}
                }
                self.assertTrue(self.handler.matches(hook_input),
                               f"Should match: {cmd}")

        # Should NOT match (sed as part of word)
        no_match_cases = [
            "ls based/file.txt",
            "parsed_data='test'",
            "cat confused.txt",
        ]

        for cmd in no_match_cases:
            with self.subTest(command=cmd, should_match=False):
                hook_input = {
                    "tool_name": "Bash",
                    "tool_input": {"command": cmd}
                }
                self.assertFalse(self.handler.matches(hook_input),
                                f"Should NOT match: {cmd}")

    def test_multiline_sed_in_script(self):
        """Should match sed in multiline shell script."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "update.sh",
                "content": """#!/bin/bash
# Update all TypeScript files
find . -name "*.ts" \\
  -exec sed -i 's/old/new/g' {} \\;
echo "Done"
"""
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_script_extensions(self):
        """Should only check .sh and .bash extensions."""
        script_extensions = [".sh", ".bash"]
        non_script_extensions = [".txt", ".md", ".ts", ".py", ".js"]

        # Script files should be checked
        for ext in script_extensions:
            with self.subTest(extension=ext, should_check=True):
                hook_input = {
                    "tool_name": "Write",
                    "tool_input": {
                        "file_path": f"script{ext}",
                        "content": "sed -i 's/old/new/g' file.txt"
                    }
                }
                self.assertTrue(self.handler.matches(hook_input))

        # Non-script files should NOT be checked
        for ext in non_script_extensions:
            with self.subTest(extension=ext, should_check=False):
                hook_input = {
                    "tool_name": "Write",
                    "tool_input": {
                        "file_path": f"document{ext}",
                        "content": "sed -i 's/old/new/g' file.txt"
                    }
                }
                self.assertFalse(self.handler.matches(hook_input))

    # NEW TESTS - False Positive Fixes (These should currently FAIL)

    def test_no_match_write_markdown_with_sed_mention(self):
        """Should allow writing markdown files that mention sed."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "docs/commands.md",
                "content": "# Commands\n\nDon't use sed for bulk updates.\nUse `sed -i` carefully."
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_write_markdown_with_sed_code_block(self):
        """Should allow markdown with sed in code blocks."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": ".claude/hooks/CLAUDE.md",
                "content": """# Documentation

## Bad Practice

```bash
sed -i 's/foo/bar/g' file.txt
```

This is dangerous!
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_git_commit_with_sed_in_message(self):
        """Should allow git commit messages that mention sed."""
        commands = [
            'git commit -m "Fix sed blocker"',
            'git commit -m "Block sed usage"',
            'git commit -m "Prevent sed from destroying files"',
            "git commit -m 'Update sed blocker handler'",
        ]
        for cmd in commands:
            with self.subTest(command=cmd):
                hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
                self.assertFalse(self.handler.matches(hook_input),
                                f"Should allow git commit: {cmd}")

    def test_no_match_git_commit_heredoc_with_sed(self):
        """Should allow git commit with heredoc mentioning sed."""
        cmd = 'git commit -m "$(cat <<\'EOF\'\nBlock sed usage\n\nsed causes file corruption.\nEOF\n)"'
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": cmd
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_git_add_before_commit(self):
        """Should allow git add followed by commit mentioning sed."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": 'git add . && git commit -m "Fix sed blocker false positives"'
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_write_readme_with_sed_warning(self):
        """Should allow README files with sed warnings."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "README.md",
                "content": """# Project

## Rules

- NEVER use `sed -i` for bulk updates
- sed causes data loss
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # Edge cases for mixed commands

    def test_blocks_mixed_git_and_sed_execution(self):
        """Should BLOCK when git command is followed by actual sed execution."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": 'git add file.sh && sed -i \'s/a/b/\' file.txt'
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_blocks_sed_before_git(self):
        """Should BLOCK when sed execution comes before git command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "sed -i 's/old/new/' file.txt && git add file.txt"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))


if __name__ == '__main__':
    unittest.main()

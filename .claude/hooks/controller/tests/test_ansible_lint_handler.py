#!/usr/bin/env python3
"""Tests for AnsibleLintHandler - auto-lints playbook files after editing."""

import unittest
import sys
import os
from unittest.mock import patch, MagicMock

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import HookResult
from handlers.post_tool_use.file_handlers import AnsibleLintHandler


class TestAnsibleLintHandler(unittest.TestCase):
    """Test Ansible playbook linting for Write and Edit operations."""

    def setUp(self):
        self.handler = AnsibleLintHandler()

    # ========================================
    # MATCHING TESTS - What should be linted
    # ========================================

    def test_matches_playbook_yml_write(self):
        """Should match Write operations to playbooks/*.yml files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/imports/play-git.yml",
                "content": "---\n- hosts: desktop\n  tasks: []"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_playbook_yaml_write(self):
        """Should match Write operations to playbooks/*.yaml files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/playbook-main.yaml",
                "content": "---\n- hosts: all"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_playbook_edit(self):
        """Should match Edit operations to playbooks/ files."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "playbooks/imports/optional/play-install-docker.yml",
                "old_string": "old",
                "new_string": "new"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_nested_playbook(self):
        """Should match deeply nested playbooks."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/imports/optional/hardware/play-nvidia.yml",
                "content": "---\n- hosts: desktop"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_absolute_path_playbook(self):
        """Should match absolute paths to playbooks."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/playbooks/imports/play-basic.yml",
                "content": "---\n- hosts: desktop"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    # ========================================
    # NON-MATCHING TESTS - What should pass
    # ========================================

    def test_ignores_non_yaml_files(self):
        """Should NOT match non-YAML files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/README.md",
                "content": "# Playbooks"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_yaml_outside_playbooks(self):
        """Should NOT match YAML files outside playbooks/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "environment/localhost/hosts.yml",
                "content": "---\ndesktop:\n  hosts:"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_vars_yaml(self):
        """Should NOT match vars/*.yml files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "vars/fedora-version.yml",
                "content": "---\nfedora_version: 42"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_read_tool(self):
        """Should NOT match Read operations (only Write/Edit)."""
        hook_input = {
            "tool_name": "Read",
            "tool_input": {
                "file_path": "playbooks/playbook-main.yml"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_bash_tool(self):
        """Should NOT match Bash tool operations."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "ansible-playbook playbooks/playbook-main.yml"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # ========================================
    # HANDLE TESTS - Linting behavior
    # ========================================

    @patch('subprocess.run')
    @patch('os.path.exists')
    def test_allows_when_lint_passes(self, mock_exists, mock_run):
        """Should ALLOW operation when ansible-lint passes."""
        mock_exists.return_value = True  # scripts/lint exists
        mock_run.return_value = MagicMock(returncode=0, stdout="✓ No issues", stderr="")

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/imports/play-git.yml",
                "content": "---\n- hosts: desktop"
            }
        }

        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")
        self.assertIn("No ansible-lint issues", result.reason)

    @patch('subprocess.run')
    @patch('os.path.exists')
    def test_blocks_when_lint_fails(self, mock_exists, mock_run):
        """Should BLOCK operation when ansible-lint fails."""
        mock_exists.return_value = True
        mock_run.return_value = MagicMock(
            returncode=1,
            stdout="Found 3 violations:\n- fqcn[action-core]\n- yaml[line-length]",
            stderr=""
        )

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/imports/play-docker.yml",
                "content": "---\n- hosts: desktop\n  tasks:\n  - yum: name=docker"
            }
        }

        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "deny")
        self.assertIn("ansible-lint found issues", result.reason.lower())
        self.assertIn("violations", result.reason.lower())

    @patch('os.path.exists')
    def test_allows_when_lint_script_missing(self, mock_exists):
        """Should ALLOW operation if scripts/lint doesn't exist."""
        mock_exists.return_value = False  # scripts/lint not found

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/imports/play-basic.yml",
                "content": "---\n- hosts: desktop"
            }
        }

        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")
        self.assertIn("skipped", result.reason.lower())

    @patch('subprocess.run')
    @patch('os.path.exists')
    def test_runs_lint_on_correct_file(self, mock_exists, mock_run):
        """Should run ./scripts/lint with correct file path."""
        mock_exists.return_value = True
        mock_run.return_value = MagicMock(returncode=0, stdout="✓ No issues", stderr="")

        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/playbooks/imports/play-vim.yml",
                "old_string": "old",
                "new_string": "new"
            }
        }

        self.handler.handle(hook_input)

        # Verify subprocess.run was called with correct arguments
        mock_run.assert_called_once()
        call_args = mock_run.call_args[0][0]
        self.assertEqual(call_args[0], "./scripts/lint")
        self.assertIn("playbooks/imports/play-vim.yml", call_args[1])

    @patch('subprocess.run')
    @patch('os.path.exists')
    def test_handles_lint_timeout(self, mock_exists, mock_run):
        """Should handle timeout gracefully."""
        mock_exists.return_value = True
        mock_run.side_effect = Exception("Timeout")

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/imports/play-slow.yml",
                "content": "---\n- hosts: desktop"
            }
        }

        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")
        self.assertIn("error", result.reason.lower())

    @patch('subprocess.run')
    @patch('os.path.exists')
    def test_provides_helpful_error_on_failure(self, mock_exists, mock_run):
        """Should provide helpful error message on lint failure."""
        mock_exists.return_value = True
        mock_run.return_value = MagicMock(
            returncode=2,
            stdout="❌ Found 5 failures:\nfqcn[action-core]: Use FQCN for builtin actions",
            stderr=""
        )

        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "playbooks/imports/play-broken.yml",
                "content": "---\n- hosts: desktop"
            }
        }

        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "deny")
        self.assertIn("./scripts/lint", result.reason)
        self.assertIn("playbooks/imports/play-broken.yml", result.reason)


if __name__ == '__main__':
    unittest.main()

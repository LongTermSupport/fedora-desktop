#!/usr/bin/env python3
"""Tests for SystemPathsHandler - prevents direct editing of deployed system files."""

import os
import sys
import unittest

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from handlers.pre_tool_use import SystemPathsHandler


class TestSystemPathsHandler(unittest.TestCase):
    """Test validation of system path editing for Write and Edit operations."""

    def setUp(self):
        self.handler = SystemPathsHandler()

    # ========================================
    # MATCHING TESTS - What should be caught
    # ========================================

    def test_matches_etc_write(self):
        """Should match Write operations to /etc/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/etc/systemd/system/myservice.service",
                "content": "[Unit]\nDescription=Test",
            },
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_var_edit(self):
        """Should match Edit operations to /var/ paths."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/var/local/claude-yolo/claude-yolo",
                "old_string": "old",
                "new_string": "new",
            },
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_usr_write(self):
        """Should match Write operations to /usr/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/usr/local/bin/script.sh",
                "content": "#!/bin/bash\necho test",
            },
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_opt_write(self):
        """Should match Write operations to /opt/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/opt/myapp/config.conf",
                "content": "setting=value",
            },
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_root_home_write(self):
        """Should match Write operations to /root/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/root/.bashrc",
                "content": "export PATH=/usr/local/bin:$PATH",
            },
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_home_user_write(self):
        """Should match Write operations to /home/{{ user_login }}/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/home/{{ user_login }}/.bashrc",  # Placeholder - not actual username
                "content": "alias ll='ls -la'",
            },
        }
        self.assertTrue(self.handler.matches(hook_input))

    # ========================================
    # NON-MATCHING TESTS - What should pass
    # ========================================

    def test_allows_project_files_write(self):
        """Should allow Write to project files/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "files/etc/systemd/system/myservice.service",
                "content": "[Unit]\nDescription=Test",
            },
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_project_files_var_write(self):
        """Should allow Write to project files/var/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "files/var/local/claude-yolo/claude-yolo",
                "content": "#!/bin/bash\necho test",
            },
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_workspace_files_write(self):
        """Should allow Write to /workspace/ paths (project directory)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/files/etc/config.conf",
                "content": "setting=value",
            },
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_relative_paths(self):
        """Should allow relative paths in project."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "./playbooks/imports/play-git.yml",
                "old_string": "old",
                "new_string": "new",
            },
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_read_tool(self):
        """Should allow Read tool on system paths (read-only is fine)."""
        hook_input = {
            "tool_name": "Read",
            "tool_input": {
                "file_path": "/etc/systemd/system/myservice.service",
            },
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_bash_tool(self):
        """Should allow Bash tool (not file editing)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "ls /etc/",
            },
        }
        self.assertFalse(self.handler.matches(hook_input))

    # ========================================
    # HANDLE TESTS - Proper blocking behavior
    # ========================================

    def test_blocks_etc_with_correct_message(self):
        """Should block /etc/ edits with helpful message."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/etc/systemd/system/myservice.service",
                "content": "[Unit]\nDescription=Test",
            },
        }
        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("/etc/", result.reason)
        self.assertIn("files/etc/", result.reason)
        self.assertIn("Ansible", result.reason)

    def test_blocks_var_local_with_playbook_suggestion(self):
        """Should block /var/local/ edits and suggest correct playbook."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/var/local/claude-yolo/claude-yolo",
                "old_string": "old",
                "new_string": "new",
            },
        }
        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "deny")
        self.assertIn("files/var/local/", result.reason)
        self.assertIn("ansible-playbook", result.reason)
        self.assertIn("version control", result.reason)

    def test_blocks_usr_local_bin(self):
        """Should block /usr/local/bin edits."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/usr/local/bin/myscript",
                "content": "#!/bin/bash\necho test",
            },
        }
        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("files/usr/local/", result.reason)

    def test_blocks_home_user_bashrc(self):
        """Should block editing user home directory files."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/home/{{ user_login }}/.bashrc",  # Placeholder - not actual username
                "old_string": "export OLD",
                "new_string": "export NEW",
            },
        }
        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        # Check for infrastructure-as-code concept (with or without hyphens)
        self.assertTrue(
            "infrastructure as code" in result.reason.lower() or
            "infrastructure-as-code" in result.reason.lower(),
        )

    def test_error_message_includes_principles(self):
        """Should mention CLAUDE.md principles in error."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/etc/config.conf",
                "content": "test=value",
            },
        }
        result = self.handler.handle(hook_input)
        self.assertIn("version control", result.reason.lower())
        self.assertIn("ansible", result.reason.lower())
        # Check for infrastructure-as-code concept (with or without hyphens)
        self.assertTrue(
            "infrastructure as code" in result.reason.lower() or
            "infrastructure-as-code" in result.reason.lower(),
        )


if __name__ == "__main__":
    unittest.main()

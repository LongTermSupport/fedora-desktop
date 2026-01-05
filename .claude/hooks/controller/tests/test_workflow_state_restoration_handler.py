#!/usr/bin/env python3
"""Tests for WorkflowStateRestorationHandler."""

import unittest
import json
import os
import glob
from datetime import datetime
from unittest.mock import patch

import sys
# Get the controller directory (parent of tests directory)
controller_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, controller_dir)

from front_controller import Handler, HookResult
from handlers.session_start.workflow_state_restoration_handler import WorkflowStateRestorationHandler


class TestWorkflowStateRestorationHandler(unittest.TestCase):
    """Test workflow state restoration after compaction."""

    def setUp(self):
        """Set up test fixtures."""
        self.handler = WorkflowStateRestorationHandler()

        # Clean up any existing test state files (new directory structure)
        import shutil
        if os.path.exists("./untracked/workflow-state"):
            shutil.rmtree("./untracked/workflow-state")

    def tearDown(self):
        """Clean up test state files."""
        import shutil
        if os.path.exists("./untracked/workflow-state"):
            shutil.rmtree("./untracked/workflow-state")

    def test_inherits_from_handler(self):
        """Handler must inherit from Handler base class."""
        self.assertIsInstance(self.handler, Handler)

    def test_has_correct_name(self):
        """Handler must have descriptive name."""
        self.assertEqual(self.handler.name, "workflow-state-restoration")

    def test_matches_when_source_is_compact(self):
        """Should match when SessionStart source is 'compact'."""
        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        # Even if no state file exists, should match on source="compact"
        self.assertTrue(self.handler.matches(hook_input))

    def test_does_not_match_when_source_is_startup(self):
        """Should not match when SessionStart source is 'startup'."""
        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "startup"
        }

        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_when_source_is_resume(self):
        """Should not match when SessionStart source is 'resume'."""
        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "resume"
        }

        self.assertFalse(self.handler.matches(hook_input))

    def test_handle_reads_most_recent_state_file(self):
        """Should find and read the most recent state file."""
        # Create two state files with different timestamps
        os.makedirs("./untracked", exist_ok=True)

        state1 = {
            "workflow": "First Workflow",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test1.md"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        state2 = {
            "workflow": "Second Workflow",
            "workflow_type": "custom",
            "phase": {"current": 2, "total": 2, "name": "Test 2", "status": "in_progress"},
            "required_reading": ["@test2.md"],
            "created_at": "2025-12-09T11:00:00Z"
        }

        # Write older file first (in new directory structure)
        workflow_dir1 = "./untracked/workflow-state/first-workflow"
        os.makedirs(workflow_dir1, exist_ok=True)
        state_file1 = f"{workflow_dir1}/state-first-workflow-20251209_100000.json"
        with open(state_file1, 'w') as f:
            json.dump(state1, f)

        # Wait a moment to ensure different mtime
        import time
        time.sleep(0.01)

        # Write newer file second (in new directory structure)
        workflow_dir2 = "./untracked/workflow-state/second-workflow"
        os.makedirs(workflow_dir2, exist_ok=True)
        state_file2 = f"{workflow_dir2}/state-second-workflow-20251209_110000.json"
        with open(state_file2, 'w') as f:
            json.dump(state2, f)

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # Should have read the newer file (Second Workflow)
        self.assertEqual(result.decision, "allow")
        self.assertIn("Second Workflow", result.context)

    def test_handle_does_not_delete_state_file(self):
        """State file must persist across compaction cycles (not deleted)."""
        os.makedirs("./untracked", exist_ok=True)

        state = {
            "workflow": "Test Workflow",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test.md"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        # Write to new directory structure
        workflow_dir = "./untracked/workflow-state/test-workflow"
        os.makedirs(workflow_dir, exist_ok=True)
        state_file = f"{workflow_dir}/state-test-workflow-20251209_100000.json"
        with open(state_file, 'w') as f:
            json.dump(state, f)

        self.assertTrue(os.path.exists(state_file))

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # File should NOT be deleted - persists across compaction cycles
        self.assertTrue(os.path.exists(state_file))

    def test_handle_includes_required_reading_with_at_syntax(self):
        """Guidance must include REQUIRED READING with @ syntax."""
        os.makedirs("./untracked", exist_ok=True)

        state = {
            "workflow": "Page Orchestration",
            "workflow_type": "page-orchestration",
            "phase": {"current": 4, "total": 10, "name": "SEO", "status": "in_progress"},
            "required_reading": [
                "@CLAUDE/PageOrchestration.md",
                "@CLAUDE/Sitemap/case-studies.md",
                "@.claude/skills/page-orchestration/SKILL.md"
            ],
            "context": {"plan_number": 39},
            "key_reminders": ["Case studies SKIP Phase 3"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        # Write to new directory structure
        workflow_dir = "./untracked/workflow-state/test-workflow"
        os.makedirs(workflow_dir, exist_ok=True)
        with open(f"{workflow_dir}/state-test-workflow-test.json", 'w') as f:
            json.dump(state, f)

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # Check that guidance includes @ prefixed files
        self.assertIn("@CLAUDE/PageOrchestration.md", result.context)
        self.assertIn("@CLAUDE/Sitemap/case-studies.md", result.context)
        self.assertIn("@.claude/skills/page-orchestration/SKILL.md", result.context)

    def test_handle_includes_workflow_information(self):
        """Guidance must include workflow name, type, and phase."""
        os.makedirs("./untracked", exist_ok=True)

        state = {
            "workflow": "QA Skill Loop",
            "workflow_type": "qa-skill",
            "phase": {"current": 2, "total": 3, "name": "Fixing Errors", "status": "in_progress"},
            "required_reading": ["@.claude/skills/qa/SKILL.md"],
            "context": {"iteration": 3},
            "key_reminders": ["Run llm:qa between iterations"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        # Write to new directory structure
        workflow_dir = "./untracked/workflow-state/test-workflow"
        os.makedirs(workflow_dir, exist_ok=True)
        with open(f"{workflow_dir}/state-test-workflow-test.json", 'w') as f:
            json.dump(state, f)

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # Check workflow details in context
        self.assertIn("QA Skill Loop", result.context)
        self.assertIn("qa-skill", result.context)
        self.assertIn("2/3", result.context)  # Phase current/total
        self.assertIn("Fixing Errors", result.context)

    def test_handle_includes_key_reminders(self):
        """Guidance must include key reminders from state."""
        os.makedirs("./untracked", exist_ok=True)

        state = {
            "workflow": "Test Workflow",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test.md"],
            "context": {},
            "key_reminders": [
                "Critical rule number one",
                "Important reminder number two",
                "Do not forget this"
            ],
            "created_at": "2025-12-09T10:00:00Z"
        }

        # Write to new directory structure
        workflow_dir = "./untracked/workflow-state/test-workflow"
        os.makedirs(workflow_dir, exist_ok=True)
        with open(f"{workflow_dir}/state-test-workflow-test.json", 'w') as f:
            json.dump(state, f)

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # Check all reminders present
        self.assertIn("Critical rule number one", result.context)
        self.assertIn("Important reminder number two", result.context)
        self.assertIn("Do not forget this", result.context)

    def test_handle_fails_gracefully_when_no_state_file(self):
        """Should return allow with no context if no state file exists."""
        # Ensure no state files exist
        for f in glob.glob("./untracked/workflow-state-*.json"):
            os.remove(f)

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # Should allow without error
        self.assertEqual(result.decision, "allow")
        # Should have no context (no workflow to restore)
        self.assertIsNone(result.context)

    def test_handle_fails_gracefully_with_corrupt_json(self):
        """Should fail open if state file has corrupt JSON."""
        os.makedirs("./untracked", exist_ok=True)

        # Write corrupt JSON
        with open("./untracked/workflow-state-corrupt.json", 'w') as f:
            f.write("{invalid json content here")

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # Should allow without error
        self.assertEqual(result.decision, "allow")

    def test_handle_always_allows(self):
        """Handler must never block - always returns allow."""
        os.makedirs("./untracked", exist_ok=True)

        state = {
            "workflow": "Test",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test.md"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        # Write to new directory structure
        workflow_dir = "./untracked/workflow-state/test-workflow"
        os.makedirs(workflow_dir, exist_ok=True)
        with open(f"{workflow_dir}/state-test-workflow-test.json", 'w') as f:
            json.dump(state, f)

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")

    def test_handle_includes_action_required_instructions(self):
        """Guidance must include ACTION REQUIRED section."""
        os.makedirs("./untracked", exist_ok=True)

        state = {
            "workflow": "Test",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test.md"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        # Write to new directory structure
        workflow_dir = "./untracked/workflow-state/test-workflow"
        os.makedirs(workflow_dir, exist_ok=True)
        with open(f"{workflow_dir}/state-test-workflow-test.json", 'w') as f:
            json.dump(state, f)

        hook_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        result = self.handler.handle(hook_input)

        # Check for ACTION REQUIRED section
        self.assertIn("ACTION REQUIRED", result.context)
        self.assertIn("Read ALL files", result.context)
        self.assertIn("DO NOT proceed with assumptions", result.context)


if __name__ == '__main__':
    unittest.main()

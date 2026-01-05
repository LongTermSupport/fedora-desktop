#!/usr/bin/env python3
"""Tests for WorkflowStatePreCompactHandler."""

import unittest
import json
import os
import glob
from datetime import datetime
from unittest.mock import patch, mock_open

import sys
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler, HookResult
from handlers.pre_compact.workflow_state_pre_compact_handler import WorkflowStatePreCompactHandler


class TestWorkflowStatePreCompactHandler(unittest.TestCase):
    """Test workflow state preservation before compaction."""

    def setUp(self):
        """Set up test fixtures."""
        self.handler = WorkflowStatePreCompactHandler()

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
        self.assertEqual(self.handler.name, "workflow-state-precompact")

    def test_matches_when_workflow_active(self):
        """Should match when formal workflow is detected."""
        hook_input = {
            "hook_event_name": "PreCompact",
            "trigger": "auto"
        }

        # Mock workflow detection to return True
        with patch.object(self.handler, '_detect_workflow', return_value=True):
            self.assertTrue(self.handler.matches(hook_input))

    def test_does_not_match_when_no_workflow(self):
        """Should not match when no formal workflow detected."""
        hook_input = {
            "hook_event_name": "PreCompact",
            "trigger": "auto"
        }

        # Mock workflow detection to return False
        with patch.object(self.handler, '_detect_workflow', return_value=False):
            self.assertFalse(self.handler.matches(hook_input))

    def test_handle_creates_timestamped_state_file(self):
        """Should create timestamped JSON file in ./untracked/."""
        hook_input = {
            "hook_event_name": "PreCompact",
            "trigger": "auto"
        }

        workflow_state = {
            "workflow": "Test Workflow",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 3, "name": "Testing", "status": "in_progress"},
            "required_reading": ["@CLAUDE/TestDoc.md"],
            "context": {},
            "key_reminders": [],
            "created_at": "2025-12-09T10:00:00Z"
        }

        with patch.object(self.handler, '_detect_workflow', return_value=True):
            with patch.object(self.handler, '_extract_workflow_state', return_value=workflow_state):
                result = self.handler.handle(hook_input)

        # Check that result allows compaction
        self.assertEqual(result.decision, "allow")

        # Check that state file was created in new directory structure
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        self.assertEqual(len(state_files), 1)

        # Verify file content
        with open(state_files[0], 'r') as f:
            saved_state = json.load(f)

        self.assertEqual(saved_state["workflow"], "Test Workflow")
        self.assertEqual(saved_state["workflow_type"], "custom")

    def test_handle_includes_required_reading_with_at_syntax(self):
        """State file must include REQUIRED READING with @ syntax."""
        hook_input = {
            "hook_event_name": "PreCompact",
            "trigger": "manual"
        }

        workflow_state = {
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

        with patch.object(self.handler, '_detect_workflow', return_value=True):
            with patch.object(self.handler, '_extract_workflow_state', return_value=workflow_state):
                result = self.handler.handle(hook_input)

        # Verify REQUIRED READING has @ prefix
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        with open(state_files[0], 'r') as f:
            saved_state = json.load(f)

        for file_path in saved_state["required_reading"]:
            self.assertTrue(file_path.startswith("@"),
                f"File path {file_path} must start with @")

    def test_handle_allows_compaction_to_proceed(self):
        """Handler must always exit with allow (exit code 0)."""
        hook_input = {
            "hook_event_name": "PreCompact",
            "trigger": "auto"
        }

        workflow_state = {
            "workflow": "Test",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test.md"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        with patch.object(self.handler, '_extract_workflow_state', return_value=workflow_state):
            result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNone(result.reason)

    def test_workflow_detection_checks_conversation_markers(self):
        """Workflow detection should check for workflow state markers."""
        # This tests the _detect_workflow method implementation
        # For now, stub - will implement detection logic in handler
        self.assertIn('_detect_workflow', dir(self.handler))

    def test_extract_workflow_state_builds_generic_structure(self):
        """State extraction should build generic workflow state."""
        # This tests the _extract_workflow_state method implementation
        # For now, stub - will implement extraction logic in handler
        self.assertIn('_extract_workflow_state', dir(self.handler))

    def test_filename_includes_timestamp(self):
        """Filename must include timestamp for uniqueness."""
        hook_input = {
            "hook_event_name": "PreCompact",
            "trigger": "auto"
        }

        workflow_state = {
            "workflow": "Test",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test.md"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        with patch.object(self.handler, '_detect_workflow', return_value=True):
            with patch.object(self.handler, '_extract_workflow_state', return_value=workflow_state):
                result = self.handler.handle(hook_input)

        # Check filename pattern: state-{workflow-name}-{timestamp}.json
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        self.assertEqual(len(state_files), 1)

        filename = os.path.basename(state_files[0])
        self.assertTrue(filename.startswith("state-"))
        self.assertTrue(filename.endswith(".json"))

        # Extract timestamp part (format: state-{workflow-name}-{timestamp}.json)
        # Should contain at least two hyphens (after "state-" and before timestamp)
        self.assertIn("-", filename)
        # Should be in format: YYYYMMDD_HHMMSS
        self.assertIn("_", filename)

    def test_creates_untracked_directory_if_missing(self):
        """Should create ./untracked/ directory if it doesn't exist."""
        # Remove directory if exists
        if os.path.exists("./untracked"):
            for f in os.listdir("./untracked"):
                os.remove(os.path.join("./untracked", f))
            os.rmdir("./untracked")

        hook_input = {
            "hook_event_name": "PreCompact",
            "trigger": "auto"
        }

        workflow_state = {
            "workflow": "Test",
            "workflow_type": "custom",
            "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
            "required_reading": ["@test.md"],
            "created_at": "2025-12-09T10:00:00Z"
        }

        with patch.object(self.handler, '_detect_workflow', return_value=True):
            with patch.object(self.handler, '_extract_workflow_state', return_value=workflow_state):
                result = self.handler.handle(hook_input)

        self.assertTrue(os.path.exists("./untracked"))
        self.assertTrue(os.path.isdir("./untracked"))

    def test_valid_workflow_types(self):
        """Should accept all valid workflow type values."""
        valid_types = [
            "page-orchestration",
            "qa-skill",
            "sitemap-skill",
            "eslint-skill",
            "planning",
            "custom"
        ]

        for wf_type in valid_types:
            hook_input = {
                "hook_event_name": "PreCompact",
                "trigger": "auto"
            }

            workflow_state = {
                "workflow": f"Test {wf_type}",
                "workflow_type": wf_type,
                "phase": {"current": 1, "total": 1, "name": "Test", "status": "in_progress"},
                "required_reading": ["@test.md"],
                "created_at": "2025-12-09T10:00:00Z"
            }

            with patch.object(self.handler, '_extract_workflow_state', return_value=workflow_state):
                result = self.handler.handle(hook_input)

            # Cleanup for next iteration
            for f in glob.glob("./untracked/workflow-state-*.json"):
                os.remove(f)


if __name__ == '__main__':
    unittest.main()

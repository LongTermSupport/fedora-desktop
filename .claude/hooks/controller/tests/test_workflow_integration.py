#!/usr/bin/env python3
"""Integration tests for workflow detection with real scenarios."""

import unittest
import json
import os
import glob
import shutil
from datetime import datetime

import sys
# Get the controller directory (parent of tests directory)
controller_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, controller_dir)

from handlers.pre_compact.workflow_state_pre_compact_handler import WorkflowStatePreCompactHandler
from handlers.session_start.workflow_state_restoration_handler import WorkflowStateRestorationHandler


class TestWorkflowIntegration(unittest.TestCase):
    """Integration tests for complete workflow lifecycle."""

    def setUp(self):
        """Set up test fixtures."""
        self.pre_compact_handler = WorkflowStatePreCompactHandler()
        self.session_start_handler = WorkflowStateRestorationHandler()

        # Clean up any existing test files (new directory structure)
        if os.path.exists("./untracked/workflow-state"):
            shutil.rmtree("./untracked/workflow-state")

        for f in glob.glob("CLAUDE.local.md"):
            try:
                os.remove(f)
            except:
                pass

        # Clean up test plan directories
        if os.path.exists("CLAUDE/Plan/test-plan"):
            shutil.rmtree("CLAUDE/Plan/test-plan")

    def tearDown(self):
        """Clean up test files."""
        if os.path.exists("./untracked/workflow-state"):
            shutil.rmtree("./untracked/workflow-state")

        for f in glob.glob("CLAUDE.local.md"):
            try:
                os.remove(f)
            except:
                pass

        if os.path.exists("CLAUDE/Plan/test-plan"):
            shutil.rmtree("CLAUDE/Plan/test-plan")

    def test_page_orchestration_workflow(self):
        """Test detection and restoration for Page Orchestration workflow."""
        # Create CLAUDE.local.md with page orchestration workflow state
        workflow_memory = """# Current Workflow State

Workflow: Page Orchestration - Case Studies
Phase: 4/10 - SEO Generation

@CLAUDE/PageOrchestration.md
@CLAUDE/Sitemap/case-studies.md
@.claude/skills/page-orchestration/SKILL.md

Key Reminders:
- Case studies SKIP Phase 3 (research pre-exists)
- Use page-seo agent for SEO generation
- Sitemap spec exists at CLAUDE/Sitemap/case-studies.md
"""

        with open("CLAUDE.local.md", 'w') as f:
            f.write(workflow_memory)

        # PreCompact should detect workflow
        hook_input = {"hook_event_name": "PreCompact", "trigger": "auto"}
        self.assertTrue(self.pre_compact_handler.matches(hook_input))

        # PreCompact should create state file
        result = self.pre_compact_handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")

        # Verify state file was created in new directory structure
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        self.assertEqual(len(state_files), 1)

        # Read and verify state content
        with open(state_files[0], 'r') as f:
            state = json.load(f)

        self.assertIn("Page Orchestration", state["workflow"])
        self.assertEqual(state["phase"]["current"], 4)
        self.assertEqual(state["phase"]["total"], 10)
        self.assertEqual(state["phase"]["name"], "SEO Generation")
        self.assertIn("@CLAUDE/PageOrchestration.md", state["required_reading"])
        self.assertIn("@CLAUDE/Sitemap/case-studies.md", state["required_reading"])

        # SessionStart should restore workflow
        session_input = {
            "hook_event_name": "SessionStart",
            "source": "compact"
        }

        self.assertTrue(self.session_start_handler.matches(session_input))
        restore_result = self.session_start_handler.handle(session_input)

        # Verify restoration guidance
        self.assertEqual(restore_result.decision, "allow")
        self.assertIsNotNone(restore_result.context)
        self.assertIn("Page Orchestration", restore_result.context)
        self.assertIn("4/10", restore_result.context)
        self.assertIn("@CLAUDE/PageOrchestration.md", restore_result.context)

        # Verify state file was NOT deleted (persists across compaction)
        remaining_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        self.assertEqual(len(remaining_files), 1)

    def test_active_plan_workflow(self):
        """Test detection from active plan in CLAUDE/Plan/."""
        # Create test plan directory and PLAN.md
        os.makedirs("CLAUDE/Plan/test-plan", exist_ok=True)

        plan_content = """# Plan 066: Workflow Compaction Persistence

**Status**: 🔄 In Progress

## Tasks

### Phase 1: Core Infrastructure

- [x] ✅ Complete task 1
- [ ] 🔄 In progress task 2

See CLAUDE/Plan/066-workflow-compaction-persistence/workflow-state-format.md for details.
"""

        with open("CLAUDE/Plan/test-plan/PLAN.md", 'w') as f:
            f.write(plan_content)

        # PreCompact should detect workflow from plan
        hook_input = {"hook_event_name": "PreCompact", "trigger": "auto"}
        self.assertTrue(self.pre_compact_handler.matches(hook_input))

        # PreCompact should create state file
        result = self.pre_compact_handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")

        # Verify state file
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        self.assertEqual(len(state_files), 1)

        with open(state_files[0], 'r') as f:
            state = json.load(f)

        self.assertEqual(state["workflow"], "Workflow Compaction Persistence")
        self.assertIn("@CLAUDE/Plan/066-workflow-compaction-persistence/workflow-state-format.md",
                      state["required_reading"])

    def test_qa_skill_workflow(self):
        """Test detection and restoration for QA Skill workflow."""
        workflow_memory = """# Current Workflow State

workflow: QA Skill Loop
Phase: 2/3 - Fixing Errors

@.claude/skills/qa/SKILL.md

Iteration: 3
Key Reminders:
- Run llm:qa between iterations
- Fix all type errors before ESLint
"""

        with open("CLAUDE.local.md", 'w') as f:
            f.write(workflow_memory)

        # PreCompact detection
        hook_input = {"hook_event_name": "PreCompact", "trigger": "auto"}
        self.assertTrue(self.pre_compact_handler.matches(hook_input))

        # Create state file
        result = self.pre_compact_handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")

        # Verify state
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        with open(state_files[0], 'r') as f:
            state = json.load(f)

        self.assertIn("QA Skill", state["workflow"])
        self.assertEqual(state["phase"]["current"], 2)
        self.assertEqual(state["phase"]["total"], 3)

    def test_no_workflow_active(self):
        """Test that no state file is created when no workflow is active."""
        # No CLAUDE.local.md, no active plans
        hook_input = {"hook_event_name": "PreCompact", "trigger": "auto"}

        # Should not match
        self.assertFalse(self.pre_compact_handler.matches(hook_input))

        # Handle should still allow compaction
        result = self.pre_compact_handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")

        # No state file should be created
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        self.assertEqual(len(state_files), 0)

    def test_multiple_compaction_cycles(self):
        """Test workflow state survives multiple compaction cycles."""
        # Cycle 1: Create workflow state
        workflow_memory_v1 = """# Workflow State

Workflow: ESLint Skill
Phase: 1/2 - Analysis

@.claude/skills/eslint/SKILL.md
"""

        with open("CLAUDE.local.md", 'w') as f:
            f.write(workflow_memory_v1)

        # First compaction
        hook_input = {"hook_event_name": "PreCompact", "trigger": "auto"}
        self.pre_compact_handler.handle(hook_input)

        # SessionStart restores
        session_input = {"hook_event_name": "SessionStart", "source": "compact"}
        result1 = self.session_start_handler.handle(session_input)
        self.assertIn("ESLint Skill", result1.context)

        # Cycle 2: Update workflow state
        workflow_memory_v2 = """# Workflow State

Workflow: ESLint Skill
Phase: 2/2 - Fixing

@.claude/skills/eslint/SKILL.md
"""

        with open("CLAUDE.local.md", 'w') as f:
            f.write(workflow_memory_v2)

        # Second compaction
        self.pre_compact_handler.handle(hook_input)

        # SessionStart restores updated state
        result2 = self.session_start_handler.handle(session_input)
        self.assertIn("2/2", result2.context)
        self.assertIn("Fixing", result2.context)

    def test_required_reading_with_at_syntax(self):
        """Test that @ syntax is preserved in required reading."""
        workflow_memory = """# Workflow State

Workflow: Sitemap Skill
Phase: 1/1 - Validation

@CLAUDE/Sitemap/CLAUDE.md
@.claude/skills/sitemap/SKILL.md
"""

        with open("CLAUDE.local.md", 'w') as f:
            f.write(workflow_memory)

        # PreCompact
        hook_input = {"hook_event_name": "PreCompact", "trigger": "auto"}
        self.pre_compact_handler.handle(hook_input)

        # SessionStart restoration
        session_input = {"hook_event_name": "SessionStart", "source": "compact"}
        result = self.session_start_handler.handle(session_input)

        # Verify @ syntax in guidance
        self.assertIn("@CLAUDE/Sitemap/CLAUDE.md", result.context)
        self.assertIn("@.claude/skills/sitemap/SKILL.md", result.context)
        self.assertIn("REQUIRED READING", result.context)

    def test_custom_workflow_type(self):
        """Test that custom workflows are detected and preserved."""
        workflow_memory = """# Workflow State

Workflow: Custom Migration Workflow
Phase: 3/5 - Data Transformation

@docs/migration-guide.md
@scripts/migrate.py

Key Reminders:
- Backup database before each phase
- Validate data integrity after transformation
"""

        with open("CLAUDE.local.md", 'w') as f:
            f.write(workflow_memory)

        # PreCompact detection
        hook_input = {"hook_event_name": "PreCompact", "trigger": "auto"}
        self.assertTrue(self.pre_compact_handler.matches(hook_input))

        # Create state
        self.pre_compact_handler.handle(hook_input)

        # Verify state file
        state_files = glob.glob("./untracked/workflow-state/*/state-*.json")
        with open(state_files[0], 'r') as f:
            state = json.load(f)

        self.assertEqual(state["workflow"], "Custom Migration Workflow")
        self.assertEqual(state["workflow_type"], "custom")
        self.assertEqual(state["phase"]["current"], 3)
        self.assertEqual(state["phase"]["total"], 5)
        self.assertIn("@docs/migration-guide.md", state["required_reading"])


if __name__ == '__main__':
    unittest.main()

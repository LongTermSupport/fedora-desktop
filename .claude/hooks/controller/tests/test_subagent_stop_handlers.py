#!/usr/bin/env python3
"""Comprehensive unit tests for SubagentStop handlers."""

import unittest
import sys
import os
import json
import tempfile
from pathlib import Path

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler, HookResult
from handlers.subagent_stop.agent_handlers import RemindPromptLibraryHandler, RemindValidatorHandler


class TestRemindValidatorHandler(unittest.TestCase):
    """Test suite for RemindValidatorHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = RemindValidatorHandler()

    def test_handler_initialization(self):
        """Should initialize with correct name and priority."""
        self.assertEqual(self.handler.name, "remind-validate-after-builder")
        self.assertEqual(self.handler.priority, 10)

    def create_test_transcript_with_agent(self, subagent_type):
        """Helper to create a test transcript with a Task tool call."""
        transcript_file = tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.jsonl')

        # Write a message with Task tool use
        message_entry = {
            "type": "message",
            "message": {
                "role": "assistant",
                "content": [
                    {
                        "type": "tool_use",
                        "name": "Task",
                        "input": {
                            "subagent_type": subagent_type,
                            "prompt": "Test prompt",
                            "model": "haiku"
                        }
                    }
                ]
            }
        }

        transcript_file.write(json.dumps(message_entry) + '\n')
        transcript_file.close()

        return transcript_file.name

    def test_matches_sitemap_modifier_completion(self):
        """Should match when sitemap-modifier agent completes."""
        transcript_path = self.create_test_transcript_with_agent("sitemap-modifier")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            self.assertTrue(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_matches_page_implementer_completion(self):
        """Should match when page-implementer agent completes."""
        transcript_path = self.create_test_transcript_with_agent("page-implementer")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            self.assertTrue(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_matches_eslint_fixer_completion(self):
        """Should match when eslint-fixer agent completes."""
        transcript_path = self.create_test_transcript_with_agent("eslint-fixer")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            self.assertTrue(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_does_not_match_non_builder_agent(self):
        """Should NOT match when non-builder agent completes."""
        transcript_path = self.create_test_transcript_with_agent("random-agent")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            self.assertFalse(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_does_not_match_wrong_event(self):
        """Should NOT match for non-SubagentStop events."""
        transcript_path = self.create_test_transcript_with_agent("sitemap-modifier")

        try:
            hook_input = {
                "hook_event_name": "PreToolUse",
                "transcript_path": transcript_path
            }

            self.assertFalse(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_does_not_match_missing_transcript(self):
        """Should NOT match if transcript doesn't exist."""
        hook_input = {
            "hook_event_name": "SubagentStop",
            "transcript_path": "/nonexistent/transcript.jsonl"
        }

        self.assertFalse(self.handler.matches(hook_input))

    def test_handle_sitemap_modifier_reminder(self):
        """Should add sitemap-validator reminder after sitemap-modifier."""
        transcript_path = self.create_test_transcript_with_agent("sitemap-modifier")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIsNotNone(result.context)
            self.assertIn("sitemap-modifier agent completed", result.context)
            self.assertIn("sitemap-validator", result.context)
            self.assertIn("RECOMMENDED NEXT STEP", result.context)
        finally:
            os.unlink(transcript_path)

    def test_handle_page_implementer_reminder(self):
        """Should add page-technical-reviewer reminder after page-implementer."""
        transcript_path = self.create_test_transcript_with_agent("page-implementer")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("page-implementer agent completed", result.context)
            self.assertIn("page-technical-reviewer", result.context)
        finally:
            os.unlink(transcript_path)

    def test_handle_page_content_updater_reminder(self):
        """Should add page-humanizer reminder after page-content-updater."""
        transcript_path = self.create_test_transcript_with_agent("page-content-updater")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("page-content-updater agent completed", result.context)
            self.assertIn("page-humanizer", result.context)
        finally:
            os.unlink(transcript_path)

    def test_handle_eslint_fixer_reminder(self):
        """Should add eslint-assessor reminder after eslint-fixer."""
        transcript_path = self.create_test_transcript_with_agent("eslint-fixer")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("eslint-fixer agent completed", result.context)
            self.assertIn("eslint-assessor", result.context)
        finally:
            os.unlink(transcript_path)

    def test_handle_typescript_refactor_reminder(self):
        """Should add qa-runner reminder after typescript-refactor."""
        transcript_path = self.create_test_transcript_with_agent("typescript-refactor")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("typescript-refactor agent completed", result.context)
            self.assertIn("qa-runner", result.context)
        finally:
            os.unlink(transcript_path)

    def test_handle_typescript_component_builder_reminder(self):
        """Should add qa-runner reminder after typescript-react-component-builder."""
        transcript_path = self.create_test_transcript_with_agent("typescript-react-component-builder")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("typescript-react-component-builder agent completed", result.context)
            self.assertIn("qa-runner", result.context)
        finally:
            os.unlink(transcript_path)

    def test_handle_typescript_specialist_reminder(self):
        """Should add qa-runner reminder after typescript-specialist."""
        transcript_path = self.create_test_transcript_with_agent("typescript-specialist")

        try:
            hook_input = {
                "hook_event_name": "SubagentStop",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("typescript-specialist agent completed", result.context)
            self.assertIn("qa-runner", result.context)
        finally:
            os.unlink(transcript_path)

    def test_get_last_completed_agent_from_transcript(self):
        """Should extract subagent_type from transcript."""
        transcript_path = self.create_test_transcript_with_agent("sitemap-modifier")

        try:
            agent_type = self.handler._get_last_completed_agent(transcript_path)
            self.assertEqual(agent_type, "sitemap-modifier")
        finally:
            os.unlink(transcript_path)

    def test_get_last_completed_agent_no_task_tool(self):
        """Should return empty string if no Task tool found."""
        transcript_file = tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.jsonl')

        # Write message without Task tool
        message_entry = {
            "type": "message",
            "message": {
                "role": "assistant",
                "content": [
                    {
                        "type": "text",
                        "text": "Just a regular message"
                    }
                ]
            }
        }

        transcript_file.write(json.dumps(message_entry) + '\n')
        transcript_file.close()

        try:
            agent_type = self.handler._get_last_completed_agent(transcript_file.name)
            self.assertEqual(agent_type, "")
        finally:
            os.unlink(transcript_file.name)

    def test_get_last_completed_agent_multiple_tasks(self):
        """Should get the LAST Task tool call from transcript."""
        transcript_file = tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.jsonl')

        # Write multiple Task tool calls
        for agent in ["first-agent", "second-agent", "sitemap-modifier"]:
            message_entry = {
                "type": "message",
                "message": {
                    "role": "assistant",
                    "content": [
                        {
                            "type": "tool_use",
                            "name": "Task",
                            "input": {
                                "subagent_type": agent,
                                "prompt": "Test"
                            }
                        }
                    ]
                }
            }
            transcript_file.write(json.dumps(message_entry) + '\n')

        transcript_file.close()

        try:
            agent_type = self.handler._get_last_completed_agent(transcript_file.name)
            self.assertEqual(agent_type, "sitemap-modifier")
        finally:
            os.unlink(transcript_file.name)

    def test_builder_to_validator_mappings(self):
        """Should have correct mappings for all builder agents."""
        expected_mappings = {
            "sitemap-modifier": "sitemap-validator",
            "page-implementer": "page-technical-reviewer",
            "page-content-updater": "page-humanizer",
            "eslint-fixer": "eslint-assessor",
            "typescript-refactor": "qa-runner",
            "typescript-react-component-builder": "qa-runner",
            "typescript-specialist": "qa-runner",
        }

        for builder, validator in expected_mappings.items():
            self.assertIn(builder, self.handler.BUILDER_TO_VALIDATOR)
            self.assertEqual(
                self.handler.BUILDER_TO_VALIDATOR[builder]["validator"],
                validator,
                f"Mapping incorrect for {builder}"
            )


class TestRemindPromptLibraryHandler(unittest.TestCase):
    """Test suite for RemindPromptLibraryHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = RemindPromptLibraryHandler()

    def test_handler_initialization(self):
        """Should initialize with correct name and priority."""
        self.assertEqual(self.handler.name, "remind-capture-prompt")
        self.assertEqual(self.handler.priority, 100)

    def test_always_matches(self):
        """Should match for any subagent completion."""
        test_cases = [
            {"hook_event_name": "SubagentStop"},
            {"hook_event_name": "SubagentStop", "subagent_type": "typescript-specialist"},
            {"hook_event_name": "SubagentStop", "subagent_type": "page-implementer"},
            {"hook_event_name": "SubagentStop", "subagent_type": "unknown-agent"},
        ]

        for hook_input in test_cases:
            with self.subTest(hook_input=hook_input):
                self.assertTrue(self.handler.matches(hook_input))

    def test_handle_returns_allow_decision(self):
        """Should always return allow decision."""
        hook_input = {"subagent_type": "typescript-specialist"}
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIsNotNone(result.reason)

    def test_handle_includes_agent_type(self):
        """Should include the agent type in the message."""
        hook_input = {"subagent_type": "page-implementer"}
        result = self.handler.handle(hook_input)

        self.assertIn("page-implementer", result.reason)

    def test_handle_includes_prompt_library_info(self):
        """Should include prompt library information in message."""
        hook_input = {"subagent_type": "typescript-specialist"}
        result = self.handler.handle(hook_input)

        # Check for key information
        self.assertIn("llm:prompts", result.reason)
        self.assertIn("add --from-json", result.reason)
        self.assertIn("Benefits", result.reason)
        self.assertIn("Reuse successful prompts", result.reason)
        self.assertIn("Track what works", result.reason)
        self.assertIn("institutional knowledge", result.reason)

    def test_handle_includes_documentation_link(self):
        """Should include link to documentation."""
        hook_input = {"subagent_type": "qa-fixer"}
        result = self.handler.handle(hook_input)

        self.assertIn("CLAUDE/PromptLibrary/README.md", result.reason)

    def test_handle_with_missing_agent_type(self):
        """Should handle missing agent type gracefully."""
        hook_input = {}
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "allow")
        self.assertIn("unknown", result.reason)

    def test_handle_various_agent_types(self):
        """Should work with various agent types."""
        agent_types = [
            "typescript-specialist",
            "page-implementer",
            "content-editor",
            "eslint-fixer",
            "online-researcher",
            "Explore",
        ]

        for agent_type in agent_types:
            with self.subTest(agent_type=agent_type):
                hook_input = {"subagent_type": agent_type}
                result = self.handler.handle(hook_input)

                self.assertEqual(result.decision, "allow")
                self.assertIn(agent_type, result.reason)


if __name__ == '__main__':
    unittest.main()

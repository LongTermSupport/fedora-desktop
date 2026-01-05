#!/usr/bin/env python3
"""Comprehensive unit tests for UserPromptSubmit handlers."""

import unittest
import sys
import os
import json
import tempfile
from pathlib import Path
from unittest.mock import patch, mock_open

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler, HookResult
from handlers.user_prompt_submit.prompt_handlers import AutoContinueHandler


class TestAutoContinueHandler(unittest.TestCase):
    """Test suite for AutoContinueHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = AutoContinueHandler()

    def test_handler_initialization(self):
        """Should initialize with correct name and priority."""
        self.assertEqual(self.handler.name, "auto-continue")
        self.assertEqual(self.handler.priority, 10)

    def create_test_transcript(self, assistant_message):
        """Helper to create a test transcript file."""
        transcript_file = tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.jsonl')

        # Write a message entry
        message_entry = {
            "type": "message",
            "message": {
                "role": "assistant",
                "content": [
                    {
                        "type": "text",
                        "text": assistant_message
                    }
                ]
            }
        }

        transcript_file.write(json.dumps(message_entry) + '\n')
        transcript_file.close()

        return transcript_file.name

    def test_matches_minimal_yes_after_confirmation(self):
        """Should match 'yes' response after confirmation prompt."""
        transcript_path = self.create_test_transcript("Would you like me to continue with the next phase?")

        try:
            hook_input = {
                "prompt": "yes",
                "transcript_path": transcript_path
            }

            self.assertTrue(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_matches_y_after_confirmation(self):
        """Should match 'y' response after confirmation prompt."""
        transcript_path = self.create_test_transcript("Should I proceed with the implementation?")

        try:
            hook_input = {
                "prompt": "y",
                "transcript_path": transcript_path
            }

            self.assertTrue(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_matches_continue_after_confirmation(self):
        """Should match 'continue' response after confirmation prompt."""
        transcript_path = self.create_test_transcript("Shall I continue with batch 2?")

        try:
            hook_input = {
                "prompt": "continue",
                "transcript_path": transcript_path
            }

            self.assertTrue(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_does_not_match_detailed_response(self):
        """Should NOT match detailed responses (not minimal)."""
        transcript_path = self.create_test_transcript("Would you like me to continue?")

        try:
            hook_input = {
                "prompt": "yes, but please also check the styling",
                "transcript_path": transcript_path
            }

            self.assertFalse(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_does_not_match_without_confirmation_prompt(self):
        """Should NOT match minimal responses if no confirmation prompt."""
        transcript_path = self.create_test_transcript("Here's the implementation you requested.")

        try:
            hook_input = {
                "prompt": "yes",
                "transcript_path": transcript_path
            }

            self.assertFalse(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_does_not_match_missing_transcript(self):
        """Should NOT match if transcript doesn't exist."""
        hook_input = {
            "prompt": "yes",
            "transcript_path": "/nonexistent/transcript.jsonl"
        }

        self.assertFalse(self.handler.matches(hook_input))

    def test_does_not_match_empty_prompt(self):
        """Should NOT match empty prompt."""
        transcript_path = self.create_test_transcript("Would you like to continue?")

        try:
            hook_input = {
                "prompt": "",
                "transcript_path": transcript_path
            }

            self.assertFalse(self.handler.matches(hook_input))
        finally:
            os.unlink(transcript_path)

    def test_handle_minimal_yes_response(self):
        """Should enhance minimal 'yes' with strong auto-continue directive."""
        transcript_path = self.create_test_transcript("Would you like me to continue?")

        try:
            hook_input = {
                "prompt": "yes",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("AUTO-CONTINUE MODE", result.context)
            self.assertIn("Do NOT ask for confirmation again", result.context)
            self.assertIn("yes", result.context)
        finally:
            os.unlink(transcript_path)

    def test_handle_detailed_response(self):
        """Should add mild reminder for detailed responses."""
        transcript_path = self.create_test_transcript("Shall I proceed?")

        try:
            # This won't match, but if it did handle, should add mild reminder
            # We test handle() directly here
            hook_input = {
                "prompt": "yes, and also update the docs",
                "transcript_path": transcript_path
            }

            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "allow")
            self.assertIn("Auto-continue mode enabled", result.context)
            self.assertIn("yes, and also update the docs", result.context)
        finally:
            os.unlink(transcript_path)

    def test_contains_confirmation_prompt_patterns(self):
        """Should detect various confirmation prompt patterns."""
        test_cases = [
            ("Would you like me to continue?", True),
            ("Should I proceed with the next step?", True),
            ("Shall I begin implementation?", True),
            ("Do you want me to start?", True),
            ("May I continue with batch 2?", True),
            ("Can I proceed?", True),
            ("Ready to implement?", True),
            ("Would you like to review the plan?", True),
            ("Shall we move on?", True),
            ("Here's the result.", False),
            ("Implementation complete.", False),
        ]

        for text, expected in test_cases:
            result = self.handler._contains_confirmation_prompt(text)
            self.assertEqual(result, expected, f"Failed for: {text}")

    def test_is_minimal_response(self):
        """Should correctly identify minimal responses."""
        test_cases = [
            ("yes", True),
            ("y", True),
            ("yep", True),
            ("yeah", True),
            ("ok", True),
            ("okay", True),
            ("continue", True),
            ("proceed", True),
            ("go ahead", True),
            ("sure", True),
            ("go", True),
            ("do it", True),
            ("yes please", True),
            ("YES", True),  # Case insensitive
            ("yes, but also check styling", False),
            ("continue with modifications", False),
            ("let me think", False),
        ]

        for text, expected in test_cases:
            result = self.handler._is_minimal_response(text)
            self.assertEqual(result, expected, f"Failed for: {text}")

    def test_get_last_assistant_message_multiple_messages(self):
        """Should get the last assistant message from transcript with multiple messages."""
        transcript_file = tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.jsonl')

        # Write multiple messages
        messages = [
            {
                "type": "message",
                "message": {
                    "role": "user",
                    "content": [{"type": "text", "text": "Do task 1"}]
                }
            },
            {
                "type": "message",
                "message": {
                    "role": "assistant",
                    "content": [{"type": "text", "text": "Task 1 complete."}]
                }
            },
            {
                "type": "message",
                "message": {
                    "role": "assistant",
                    "content": [{"type": "text", "text": "Would you like me to continue?"}]
                }
            }
        ]

        for msg in messages:
            transcript_file.write(json.dumps(msg) + '\n')

        transcript_file.close()

        try:
            result = self.handler._get_last_assistant_message(transcript_file.name)
            self.assertEqual(result, "Would you like me to continue?")
        finally:
            os.unlink(transcript_file.name)

    def test_get_last_assistant_message_malformed_transcript(self):
        """Should handle malformed transcript gracefully."""
        transcript_file = tempfile.NamedTemporaryFile(mode='w', delete=False, suffix='.jsonl')

        # Write some invalid JSON
        transcript_file.write("not json\n")
        transcript_file.write('{"incomplete": \n')
        transcript_file.close()

        try:
            result = self.handler._get_last_assistant_message(transcript_file.name)
            self.assertEqual(result, "")
        finally:
            os.unlink(transcript_file.name)


if __name__ == '__main__':
    unittest.main()

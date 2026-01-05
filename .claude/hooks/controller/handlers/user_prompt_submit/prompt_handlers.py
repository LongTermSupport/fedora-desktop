"""UserPromptSubmit handlers - prompt enhancement before submission."""

import re
import sys
import os
import json

# Add parent directories to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult


class AutoContinueHandler(Handler):
    """Automatically enhance minimal 'yes' responses with continuation context."""

    # Common confirmation patterns that Claude uses
    CONFIRMATION_PATTERNS = [
        # Generic continuation prompts
        r'would you like me to (?:continue|proceed|start|begin)',
        r'should I (?:continue|proceed|start|begin)',
        r'shall I (?:continue|proceed|start|begin)',
        r'do you want me to (?:continue|proceed|start|begin)',
        r'may I (?:continue|proceed|start|begin)',
        r'can I (?:continue|proceed|start|begin)',
        r'ready (?:for me )?to (?:continue|proceed|start)',

        # Review/approval prompts
        r'would you (?:like|prefer) to review',
        r'do you want to review',
        r'ready to (?:implement|execute|run)',

        # Launch/execution prompts
        r'would you like me to (?:launch|execute|run)',
        r'should I (?:launch|execute|run)',

        # Next steps
        r'would you like me to move (?:on|forward)',
        r'shall we (?:continue|proceed|move on)',

        # Or/choice questions
        r'or would you (?:like|prefer)',
        r'or should I',

        # Explicit permission requests
        r'(?:would you like|do you want) (?:me )?to',

        # Batch/phase continuation (common in agent workflows)
        r'continue with (?:batch|phase|step)',
        r'would you like.+(?:batch|phase|step)',
        r'shall I proceed.+(?:batch|phase|step)',
    ]

    MINIMAL_RESPONSES = [
        'yes', 'y', 'yep', 'yeah', 'ok', 'okay',
        'continue', 'proceed', 'go ahead', 'sure',
        'go', 'do it', 'yes please'
    ]

    def __init__(self):
        super().__init__(name="auto-continue", priority=10)

    def matches(self, hook_input: dict) -> bool:
        """Check if this is a minimal response to a confirmation prompt."""
        prompt = hook_input.get('prompt', '').strip()
        transcript_path = hook_input.get('transcript_path', '')

        if not prompt or not transcript_path:
            return False

        # Get the last assistant message
        last_assistant_msg = self._get_last_assistant_message(transcript_path)

        # Check if the last assistant message contained a confirmation prompt
        if not last_assistant_msg or not self._contains_confirmation_prompt(last_assistant_msg):
            return False

        # Check if user's response is minimal
        return self._is_minimal_response(prompt)

    def handle(self, hook_input: dict) -> HookResult:
        """Enhance prompt with auto-continue context."""
        prompt = hook_input.get('prompt', '').strip()
        transcript_path = hook_input.get('transcript_path', '')

        # Get the last assistant message
        last_assistant_msg = self._get_last_assistant_message(transcript_path)

        # Check if user gave minimal response
        if self._is_minimal_response(prompt):
            # Add VERY clear continuation instruction
            enhanced_prompt = (
                f"{prompt}\n\n"
                "[AUTO-CONTINUE MODE: YES, continue with ALL remaining work. "
                "Do NOT ask for confirmation again. "
                "Proceed through all batches/phases/steps automatically. "
                "Only stop if you encounter an error or need critical information. "
                "Execute the full plan without further approval requests.]"
            )
        else:
            # User gave a more detailed response
            enhanced_prompt = (
                f"{prompt}\n\n"
                "[Note: Auto-continue mode enabled - proceed automatically without asking for confirmation]"
            )

        # Return modified prompt
        # UserPromptSubmit hooks need to print the enhanced prompt to stdout
        # The front controller will handle the JSON output
        return HookResult(decision="allow", context=enhanced_prompt)

    def _get_last_assistant_message(self, transcript_path: str) -> str:
        """Read the transcript and get the last assistant message."""
        try:
            with open(transcript_path, 'r') as f:
                lines = f.readlines()

            # Parse JSONL format (each line is a JSON object)
            messages = []
            for line in lines:
                try:
                    msg = json.loads(line.strip())
                    if msg.get('type') == 'message':
                        messages.append(msg)
                except (json.JSONDecodeError, ValueError):
                    continue

            # Find the last assistant message
            for msg in reversed(messages):
                if msg.get('message', {}).get('role') == 'assistant':
                    # Extract text content
                    content = msg.get('message', {}).get('content', [])
                    text_parts = []
                    for part in content:
                        if isinstance(part, dict) and part.get('type') == 'text':
                            text_parts.append(part.get('text', ''))
                        elif isinstance(part, str):
                            text_parts.append(part)
                    return ' '.join(text_parts)

            return ""
        except Exception:
            return ""

    def _contains_confirmation_prompt(self, text: str) -> bool:
        """Check if text contains a confirmation prompt."""
        if not text:
            return False

        text_lower = text.lower()

        # Check each pattern
        for pattern in self.CONFIRMATION_PATTERNS:
            if re.search(pattern, text_lower, re.IGNORECASE | re.MULTILINE):
                return True

        # Check for question marks with confirmation words
        last_section = text[-300:] if len(text) > 300 else text
        if '?' in last_section:
            confirmation_words = [
                'would you', 'should i', 'shall i', 'do you want',
                'may i', 'can i', 'ready', 'prefer', 'like me to',
                'want me to', 'continue', 'proceed', 'start', 'begin'
            ]
            last_lower = last_section.lower()
            for word in confirmation_words:
                if word in last_lower:
                    return True

        return False

    def _is_minimal_response(self, prompt: str) -> bool:
        """Check if the user's response is minimal."""
        prompt_lower = prompt.lower().strip()
        return prompt_lower in self.MINIMAL_RESPONSES

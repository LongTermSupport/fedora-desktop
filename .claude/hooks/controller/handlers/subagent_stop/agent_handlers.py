"""SubagentStop handlers - reminders after agent completion."""

import json
import sys
import os
from pathlib import Path

# Add parent directories to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult


class RemindPromptLibraryHandler(Handler):
    """Remind to capture successful prompts to the library."""

    def __init__(self):
        super().__init__(name="remind-capture-prompt", priority=100)

    def matches(self, hook_input: dict) -> bool:
        """Always match - remind after every sub-agent completion."""
        return True

    def handle(self, hook_input: dict) -> HookResult:
        """Remind user to capture prompt if successful."""
        agent_type = hook_input.get("subagent_type", "unknown")

        return HookResult(
            decision="allow",
            reason=(
                f"\n💡 Sub-agent '{agent_type}' completed.\n\n"
                "If this prompt worked well, consider capturing it:\n"
                "  npm run llm:prompts -- add --from-json <prompt-file>\n\n"
                "Benefits:\n"
                "  • Reuse successful prompts later\n"
                "  • Track what works (metrics)\n"
                "  • Build institutional knowledge\n\n"
                "📖 See: CLAUDE/PromptLibrary/README.md"
            )
        )


class RemindValidatorHandler(Handler):
    """Remind to run validator agents after builder agents complete."""

    # Map builder agents to their corresponding validator agents
    BUILDER_TO_VALIDATOR = {
        "sitemap-modifier": {
            "validator": "sitemap-validator",
            "description": "sitemap modifications",
            "validation_target": "CLAUDE/Sitemap/ files",
            "validation_command": """Task tool:
    subagent_type: sitemap-validator
    prompt: Validate all sitemap files in CLAUDE/Sitemap/
    model: haiku"""
        },
        "page-implementer": {
            "validator": "page-technical-reviewer",
            "description": "page implementation",
            "validation_target": "Implemented page (ESLint, TSC, component usage)",
            "validation_command": """Task tool:
    subagent_type: page-technical-reviewer
    prompt: Review the page implementation at [page-path]
    model: sonnet"""
        },
        "page-content-updater": {
            "validator": "page-humanizer",
            "description": "page content updates",
            "validation_target": "Page prose and content (removes LLM tells)",
            "validation_command": """Task tool:
    subagent_type: page-humanizer
    prompt: Humanize content on the page at [page-path]
    model: sonnet"""
        },
        "eslint-fixer": {
            "validator": "eslint-assessor",
            "description": "ESLint fixes",
            "validation_target": "Fixed files (re-run ESLint, check quality)",
            "validation_command": """Task tool:
    subagent_type: eslint-assessor
    prompt: Verify ESLint fixes and assess quality
    model: haiku"""
        },
        "typescript-refactor": {
            "validator": "qa-runner",
            "description": "TypeScript refactoring",
            "validation_target": "Refactored code (ESLint, TypeScript, tests)",
            "validation_command": """Task tool:
    subagent_type: qa-runner
    prompt: Run QA checks on refactored code (ESLint + TypeScript)
    model: haiku"""
        },
        "typescript-react-component-builder": {
            "validator": "qa-runner",
            "description": "React component creation",
            "validation_target": "New component (ESLint, TypeScript, tests)",
            "validation_command": """Task tool:
    subagent_type: qa-runner
    prompt: Run QA checks on new component (ESLint + TypeScript)
    model: haiku"""
        },
        "typescript-specialist": {
            "validator": "qa-runner",
            "description": "TypeScript feature implementation",
            "validation_target": "New TypeScript code (ESLint, TypeScript, tests)",
            "validation_command": """Task tool:
    subagent_type: qa-runner
    prompt: Run QA checks on new TypeScript code (ESLint + TypeScript + tests)
    model: haiku"""
        },
    }

    def __init__(self):
        super().__init__(name="remind-validate-after-builder", priority=10)

    def matches(self, hook_input: dict) -> bool:
        """Check if a builder agent just completed."""
        hook_event = hook_input.get("hook_event_name", "")

        if hook_event != "SubagentStop":
            return False

        transcript_path = hook_input.get("transcript_path", "")
        if not transcript_path:
            return False

        # Get the agent that just completed
        completed_agent = self._get_last_completed_agent(transcript_path)

        # Match if this agent has a validator configured
        return completed_agent in self.BUILDER_TO_VALIDATOR

    def handle(self, hook_input: dict) -> HookResult:
        """Add reminder to run validator agent."""
        transcript_path = hook_input.get("transcript_path", "")
        completed_agent = self._get_last_completed_agent(transcript_path)

        validation_config = self.BUILDER_TO_VALIDATOR.get(completed_agent)

        if not validation_config:
            return HookResult(decision="allow")

        # Build reminder message
        validator_name = validation_config["validator"]
        description = validation_config["description"]
        target = validation_config["validation_target"]
        command = validation_config["validation_command"]

        reminder = f"""
✅ {completed_agent} agent completed

⚠️ RECOMMENDED NEXT STEP: Validate the {description}

Run {validator_name} agent:
  {command}

This completes the build→check workflow loop.

Target: {target}

If using an orchestration skill, validation runs automatically.
If called {completed_agent} directly, you should validate manually.
"""

        return HookResult(decision="allow", context=reminder)

    def _get_last_completed_agent(self, transcript_path: str) -> str:
        """Parse transcript to detect which agent just completed."""
        try:
            transcript_file = Path(transcript_path)
            if not transcript_file.exists():
                return ""

            # Read transcript lines (JSONL format)
            with open(transcript_file, 'r') as f:
                lines = f.readlines()

            # Parse lines in reverse to find most recent Task tool call
            for line in reversed(lines):
                try:
                    entry = json.loads(line)

                    # Look for tool_use with name "Task"
                    if entry.get("type") == "message":
                        content = entry.get("message", {}).get("content", [])
                        for block in content:
                            if block.get("type") == "tool_use" and block.get("name") == "Task":
                                # Extract subagent_type
                                params = block.get("input", {})
                                subagent_type = params.get("subagent_type", "")

                                if subagent_type:
                                    return subagent_type

                except (json.JSONDecodeError, KeyError):
                    continue

            return ""

        except Exception:
            return ""

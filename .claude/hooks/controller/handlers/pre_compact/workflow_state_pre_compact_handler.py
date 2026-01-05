#!/usr/bin/env python3
"""
WorkflowStatePreCompactHandler - Preserves workflow state before compaction.

Detects formal workflows and saves state to timestamped file in ./untracked/
for restoration after compaction.
"""

import os
import json
import glob
from datetime import datetime

import sys
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult


class WorkflowStatePreCompactHandler(Handler):
    """Detect and preserve workflow state before compaction."""

    def __init__(self):
        """Initialize handler with no priority (PreCompact doesn't use priorities)."""
        super().__init__(name="workflow-state-precompact")

    def matches(self, hook_input: dict) -> bool:
        """
        Match if formal workflow is active.

        Detection question: "Are you in a formally documented workflow?"

        Args:
            hook_input: PreCompact hook input with trigger, session_id, etc.

        Returns:
            True if formal workflow detected, False otherwise
        """
        # Check if this is actually a PreCompact event
        if hook_input.get("hook_event_name") != "PreCompact":
            return False

        # Detect workflow
        return self._detect_workflow(hook_input)

    def handle(self, hook_input: dict) -> HookResult:
        """
        Update or create workflow state file.

        Lifecycle:
        - If workflow state file exists: UPDATE it with current state
        - If workflow state file missing: CREATE it with start_time timestamp
        - File persists across compaction cycles
        - Only deleted when workflow completes

        Directory structure: ./untracked/workflow-state/{workflow-name}/
        Filename format: state-{workflow-name}-{start_time}.json

        Args:
            hook_input: PreCompact hook input

        Returns:
            HookResult with decision="allow" (always allows compaction)
        """
        try:
            # Only process if workflow is detected
            if not self._detect_workflow(hook_input):
                return HookResult(decision="allow")

            # Extract workflow state
            workflow_state = self._extract_workflow_state(hook_input)

            # Create ./untracked/ directory if it doesn't exist
            os.makedirs("./untracked", exist_ok=True)

            # Sanitize workflow name for directory/filename
            workflow_name = self._sanitize_workflow_name(workflow_state["workflow"])

            # Create workflow-specific directory
            workflow_dir = f"./untracked/workflow-state/{workflow_name}"
            os.makedirs(workflow_dir, exist_ok=True)

            # Look for existing state file for this workflow
            pattern = f"{workflow_dir}/state-{workflow_name}-*.json"
            existing_files = glob.glob(pattern)

            if existing_files:
                # Update existing file (should only be one)
                state_file = existing_files[0]
                # Preserve the original created_at timestamp
                try:
                    with open(state_file, 'r') as f:
                        old_state = json.load(f)
                        workflow_state["created_at"] = old_state.get("created_at", workflow_state["created_at"])
                except:
                    pass
            else:
                # Create new file with start_time timestamp
                start_time = datetime.now().strftime("%Y%m%d_%H%M%S")
                state_file = f"{workflow_dir}/state-{workflow_name}-{start_time}.json"

            # Write/update state file
            with open(state_file, 'w') as f:
                json.dump(workflow_state, f, indent=2)

        except Exception as e:
            # Fail open - if anything goes wrong, just allow compaction
            pass

        # Always allow compaction to proceed
        return HookResult(decision="allow")

    def _detect_workflow(self, hook_input: dict) -> bool:
        """
        Detect if agent is in a formal workflow.

        Detection methods (in order):
        1. Check for workflow state in CLAUDE.local.md
        2. Check for active plan with workflow phases
        3. Check conversation context for workflow markers

        Args:
            hook_input: PreCompact hook input

        Returns:
            True if formal workflow detected, False otherwise
        """
        # Method 1: Check CLAUDE.local.md for workflow state
        if os.path.exists("CLAUDE.local.md"):
            try:
                with open("CLAUDE.local.md", 'r') as f:
                    content = f.read()
                    if "WORKFLOW STATE" in content or "workflow:" in content.lower():
                        return True
            except:
                pass

        # Method 2: Check for active plans
        plan_files = glob.glob("CLAUDE/Plan/*/PLAN.md")
        for plan_file in plan_files:
            try:
                with open(plan_file, 'r') as f:
                    content = f.read()
                    # Check for "In Progress" status and phase markers
                    if "🔄 In Progress" in content or "🔄 in_progress" in content.lower():
                        if "phase" in content.lower() or "workflow" in content.lower():
                            return True
            except:
                pass

        # Method 3: Check transcript for workflow skill markers
        # (Would require reading transcript_path, but for now we keep it simple)

        # No formal workflow detected
        return False

    def _extract_workflow_state(self, hook_input: dict) -> dict:
        """
        Extract workflow state from current context.

        Builds generic workflow state structure by:
        1. Parsing CLAUDE.local.md for workflow info
        2. Checking active plan for context
        3. Building REQUIRED READING list with @ syntax

        Args:
            hook_input: PreCompact hook input

        Returns:
            dict: Workflow state in standard format
        """
        # Initialize default state
        state = {
            "workflow": "Unknown Workflow",
            "workflow_type": "custom",
            "phase": {
                "current": 1,
                "total": 1,
                "name": "In Progress",
                "status": "in_progress"
            },
            "required_reading": [],
            "context": {},
            "key_reminders": [],
            "created_at": datetime.now().isoformat() + "Z"
        }

        # Try to extract from CLAUDE.local.md
        if os.path.exists("CLAUDE.local.md"):
            try:
                with open("CLAUDE.local.md", 'r') as f:
                    content = f.read()
                    state = self._parse_workflow_from_memory(content, state)
            except:
                pass

        # Try to extract from active plan
        plan_files = glob.glob("CLAUDE/Plan/*/PLAN.md")
        for plan_file in plan_files:
            try:
                with open(plan_file, 'r') as f:
                    content = f.read()
                    if "🔄 In Progress" in content or "🔄 in_progress" in content.lower():
                        state = self._parse_workflow_from_plan(plan_file, content, state)
                        break
            except:
                pass

        return state

    def _parse_workflow_from_memory(self, content: str, state: dict) -> dict:
        """
        Parse workflow state from CLAUDE.local.md content.

        Args:
            content: File content from CLAUDE.local.md
            state: Current state dict to update

        Returns:
            dict: Updated state
        """
        # Look for workflow markers
        lines = content.split('\n')

        for line in lines:
            # Extract workflow name
            if line.startswith("Workflow:") or line.startswith("workflow:"):
                state["workflow"] = line.split(":", 1)[1].strip()

            # Extract phase info
            if line.startswith("Phase:") or line.startswith("phase:"):
                phase_info = line.split(":", 1)[1].strip()
                # Try to parse "4/10 - SEO Generation" format
                if "/" in phase_info:
                    parts = phase_info.split("-", 1)
                    phase_numbers = parts[0].strip().split("/")
                    if len(phase_numbers) == 2:
                        state["phase"]["current"] = int(phase_numbers[0])
                        state["phase"]["total"] = int(phase_numbers[1])
                    if len(parts) > 1:
                        state["phase"]["name"] = parts[1].strip()

            # Extract required reading (look for @ syntax)
            if line.strip().startswith("@"):
                file_path = line.strip()
                if file_path not in state["required_reading"]:
                    state["required_reading"].append(file_path)

        return state

    def _parse_workflow_from_plan(self, plan_file: str, content: str, state: dict) -> dict:
        """
        Parse workflow state from active plan file.

        Args:
            plan_file: Path to PLAN.md file
            content: File content
            state: Current state dict to update

        Returns:
            dict: Updated state
        """
        # Extract plan number from path
        plan_dir = os.path.dirname(plan_file)
        plan_name = os.path.basename(plan_dir)

        # Parse plan number (format: 066-workflow-name)
        if plan_name and plan_name[0].isdigit():
            plan_number = int(plan_name.split("-", 1)[0])
            state["context"]["plan_number"] = plan_number
            state["context"]["plan_name"] = plan_name

        # Extract workflow name from plan title (first # heading)
        lines = content.split('\n')
        for line in lines:
            if line.startswith("# Plan"):
                # Format: "# Plan 066: Workflow Name"
                if ":" in line:
                    state["workflow"] = line.split(":", 1)[1].strip()
                break

        # Look for workflow documentation references
        for line in lines:
            # Find CLAUDE/ references that could be required reading
            if "CLAUDE/" in line or ".claude/" in line:
                # Extract file paths
                import re
                paths = re.findall(r'(CLAUDE/[^\s\)]+\.md)', line)
                paths += re.findall(r'(\.claude/[^\s\)]+\.md)', line)
                for path in paths:
                    formatted_path = f"@{path}"
                    if formatted_path not in state["required_reading"]:
                        state["required_reading"].append(formatted_path)

        return state

    def _sanitize_workflow_name(self, workflow_name: str) -> str:
        """
        Sanitize workflow name for use in directory/filename.

        Converts to lowercase, replaces spaces/special chars with hyphens.

        Args:
            workflow_name: Raw workflow name from state

        Returns:
            str: Sanitized name safe for filesystem
        """
        import re
        # Convert to lowercase
        sanitized = workflow_name.lower()
        # Replace spaces and special characters with hyphens
        sanitized = re.sub(r'[^a-z0-9]+', '-', sanitized)
        # Remove leading/trailing hyphens
        sanitized = sanitized.strip('-')
        # Limit length to 50 characters
        sanitized = sanitized[:50]
        return sanitized


if __name__ == '__main__':
    # Allow module to be imported
    pass

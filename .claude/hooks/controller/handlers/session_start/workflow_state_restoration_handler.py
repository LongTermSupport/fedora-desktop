#!/usr/bin/env python3
"""
WorkflowStateRestorationHandler - Restores workflow state after compaction.

Reads workflow state from timestamped file in ./untracked/ and provides
guidance to force re-reading of workflow documentation.
"""

import os
import json
import glob

import sys
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult


class WorkflowStateRestorationHandler(Handler):
    """Restore workflow state after compaction."""

    def __init__(self):
        """Initialize handler with no priority (SessionStart doesn't use priorities)."""
        super().__init__(name="workflow-state-restoration")

    def matches(self, hook_input: dict) -> bool:
        """
        Match when SessionStart source is 'compact'.

        Args:
            hook_input: SessionStart hook input with source field

        Returns:
            True if source="compact", False otherwise
        """
        # Check if this is actually a SessionStart event
        if hook_input.get("hook_event_name") != "SessionStart":
            return False

        # Match only when resuming after compaction
        return hook_input.get("source") == "compact"

    def handle(self, hook_input: dict) -> HookResult:
        """
        Read workflow state files and provide guidance with REQUIRED READING.

        Finds all active workflow state files in directory structure, reads
        the most recently updated one, and builds guidance with @ syntax for
        forced file reading.

        DOES NOT delete state files - they persist across compaction cycles
        and are only deleted when workflow completes.

        Directory structure: ./untracked/workflow-state/{workflow-name}/
        Filename format: state-{workflow-name}-{start_time}.json

        Args:
            hook_input: SessionStart hook input

        Returns:
            HookResult with decision="allow" and context containing guidance
        """
        try:
            # Find all workflow state files in directory structure
            state_files = glob.glob("./untracked/workflow-state/*/state-*.json")

            if not state_files:
                # No state files found - normal session start
                return HookResult(decision="allow")

            # Sort by modification time (most recently updated first)
            state_files = sorted(state_files, key=os.path.getmtime, reverse=True)

            # Read most recently updated state file
            latest_state_file = state_files[0]

            # Read state file
            try:
                with open(latest_state_file, 'r') as f:
                    state = json.load(f)
            except (json.JSONDecodeError, IOError):
                # Corrupt or unreadable file - fail open
                return HookResult(decision="allow")

            # Build guidance message with workflow state
            guidance = self._build_guidance_message(state)

            # DO NOT DELETE - state file persists across compaction cycles
            # Only deleted when workflow completes

            # Return guidance with workflow context
            return HookResult(decision="allow", context=guidance)

        except Exception as e:
            # Fail open on any error
            return HookResult(decision="allow")

    def _build_guidance_message(self, state: dict) -> str:
        """
        Build comprehensive guidance message with workflow state.

        Args:
            state: Workflow state dict

        Returns:
            str: Formatted guidance message
        """
        workflow = state.get("workflow", "Unknown Workflow")
        workflow_type = state.get("workflow_type", "custom")
        phase = state.get("phase", {})
        required_reading = state.get("required_reading", [])
        context = state.get("context", {})
        key_reminders = state.get("key_reminders", [])

        # Format phase info
        phase_current = phase.get("current", 1)
        phase_total = phase.get("total", 1)
        phase_name = phase.get("name", "Unknown")
        phase_status = phase.get("status", "in_progress")

        # Build guidance message
        guidance_parts = [
            "⚠️ WORKFLOW RESTORED AFTER COMPACTION ⚠️",
            "",
            f"Workflow: {workflow}",
            f"Type: {workflow_type}",
            f"Phase: {phase_current}/{phase_total} - {phase_name} ({phase_status})",
            ""
        ]

        # Add REQUIRED READING section with @ syntax
        if required_reading:
            guidance_parts.append("REQUIRED READING (read ALL now with @ syntax):")
            for file_path in required_reading:
                guidance_parts.append(file_path)
            guidance_parts.append("")

        # Add key reminders
        if key_reminders:
            guidance_parts.append("Key Reminders:")
            for reminder in key_reminders:
                guidance_parts.append(f"- {reminder}")
            guidance_parts.append("")

        # Add context if present
        if context:
            guidance_parts.append("Context:")
            guidance_parts.append(json.dumps(context, indent=2))
            guidance_parts.append("")

        # Add ACTION REQUIRED section
        guidance_parts.extend([
            "ACTION REQUIRED:",
            "1. Read ALL files listed above using @ syntax",
            "2. Confirm understanding of workflow phase",
            "3. DO NOT proceed with assumptions or hallucinated logic"
        ])

        return "\n".join(guidance_parts)


if __name__ == '__main__':
    # Allow module to be imported
    pass

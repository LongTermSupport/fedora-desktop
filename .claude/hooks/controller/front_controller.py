#!/usr/bin/env python3
"""
Front Controller for Claude Code Hooks

Implements efficient pattern-based dispatch to avoid spawning multiple processes.
Each event type (PreToolUse, PostToolUse, etc.) uses this shared engine.
"""

import json
import sys
from typing import List, Optional


class HookResult:
    """Standardised hook result."""

    def __init__(
        self,
        decision: str = "allow",  # "allow", "deny", "ask"
        reason: Optional[str] = None,
        context: Optional[str] = None,
        guidance: Optional[str] = None  # Guidance for agent (allow with feedback)
    ):
        self.decision = decision
        self.reason = reason
        self.context = context
        self.guidance = guidance

    def to_json(self, event_name: str) -> dict:
        """Convert to Claude Code hook JSON format."""
        if self.decision == "allow" and not self.context and not self.guidance:
            return {}  # Silent allow

        output = {"hookEventName": event_name}

        if self.decision in ["deny", "ask"]:
            output["permissionDecision"] = self.decision
            if self.reason:
                output["permissionDecisionReason"] = self.reason

        if self.context:
            output["additionalContext"] = self.context

        if self.guidance:
            output["guidance"] = self.guidance

        return {"hookSpecificOutput": output} if output else {}


class Handler:
    """Base class for all handlers with built-in matching logic."""

    def __init__(self, name: str, priority: int = 100):
        self.name = name
        self.priority = priority

    def matches(self, hook_input: dict) -> bool:
        """
        Check if this handler applies to the given input.

        Override this method to implement custom matching logic.
        Can use complex conditions, multiple checks, etc.

        Returns True if this handler should execute.
        """
        raise NotImplementedError(f"{self.__class__.__name__} must implement matches()")

    def handle(self, hook_input: dict) -> HookResult:
        """
        Execute the handler logic.

        Override this method to implement the actual hook behaviour.

        Returns HookResult with decision and optional reason/context.
        """
        raise NotImplementedError(f"{self.__class__.__name__} must implement handle()")


class FrontController:
    """Front controller that dispatches to exactly ONE handler."""

    def __init__(self, event_name: str):
        self.event_name = event_name
        self.handlers: List[Handler] = []

    def register(self, handler: Handler):
        """Register a handler instance."""
        self.handlers.append(handler)
        # Keep handlers sorted by priority (lower = runs first)
        self.handlers.sort(key=lambda h: h.priority)

    def dispatch(self, hook_input: dict) -> HookResult:
        """Find first matching handler and execute it.

        Returns HookResult from matched handler, or HookResult("allow") if no match.
        """
        for handler in self.handlers:
            if handler.matches(hook_input):
                # MATCH FOUND - execute handler and STOP
                return handler.handle(hook_input)

        # No handlers matched - allow by default
        return HookResult("allow")

    def run(self):
        """Main entry point - read stdin, dispatch, write output."""
        try:
            hook_input = json.load(sys.stdin)
        except json.JSONDecodeError:
            # Fail open if input invalid
            print('{}')
            sys.exit(0)

        # Dispatch to matching handler
        result = self.dispatch(hook_input)

        # Output JSON
        output = result.to_json(self.event_name)
        json.dump(output, sys.stdout)
        sys.exit(0)


# Common utility methods for handlers

def get_bash_command(hook_input: dict) -> Optional[str]:
    """Extract bash command from hook input, or None if not Bash tool."""
    if hook_input.get("tool_name") != "Bash":
        return None
    return hook_input.get("tool_input", {}).get("command", "")


def get_file_path(hook_input: dict) -> Optional[str]:
    """Extract file path from hook input, or None if not Write/Edit."""
    if hook_input.get("tool_name") not in ["Write", "Edit"]:
        return None
    return hook_input.get("tool_input", {}).get("file_path", "")


def get_file_content(hook_input: dict) -> Optional[str]:
    """Extract file content from hook input, or None if not Write/Edit."""
    if hook_input.get("tool_name") not in ["Write", "Edit"]:
        return None
    return hook_input.get("tool_input", {}).get("content", "")

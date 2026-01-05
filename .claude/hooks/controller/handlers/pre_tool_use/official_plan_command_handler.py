"""OfficialPlanCommandHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class OfficialPlanCommandHandler(Handler):
    """Enforce official plan numbering lookup command (blocks ad-hoc bash lookups)."""

    # The official command from CLAUDE/Plan/CLAUDE.md (updated to use grep instead of sed)
    OFFICIAL_COMMAND_NORMALIZED = "find CLAUDE/Plan -maxdepth 2 -type d -name '[0-9]*' | grep -oP '/\\K\\d{3}(?=-)' | sort -n | tail -1"

    # Ad-hoc patterns that should be blocked
    AD_HOC_PATTERNS = [
        r'ls\s+(?:-\w+\s+)*\*/Plan/\[0-9\]',  # ls -d */Plan/[0-9]*
        r'ls\s+(?:-\w+\s+)*CLAUDE/Plan/0',     # ls CLAUDE/Plan/0*
        r'ls\s+(?:-\w+\s+)*CLAUDE/Plan[^|]*\|\s*grep',  # ls CLAUDE/Plan | grep
        r'find\s+CLAUDE/Plan(?:\s+(?!-maxdepth\s+2)|\s*$)',  # find CLAUDE/Plan without -maxdepth 2, or no args
        r'find\s+CLAUDE/Plan\s+-maxdepth\s+1\b',      # Wrong maxdepth (1 instead of 2)
        r'cd\s+CLAUDE/Plan\s+&&\s+ls',  # cd into Plan then list
        r'ls\s+(?:-\w+\s+)?CLAUDE/Plan(?:/|$)',      # ls [options] CLAUDE/Plan/ or CLAUDE/Plan
    ]

    # Archival/organization subdirectories that are legitimate
    ARCHIVAL_SUBDIRS = ['Completed', 'Archive', 'Backup', 'Cancelled']

    def __init__(self):
        super().__init__(name="enforce-official-plan-command", priority=25)

    def _is_archival_operation(self, command: str) -> bool:
        """Check if this is a legitimate plan archival/organization operation.

        Archival operations include:
        - mkdir -p CLAUDE/Plan/{Completed,Archive,Backup}
        - mv CLAUDE/Plan/NNN-* CLAUDE/Plan/Completed/
        - cp -r CLAUDE/Plan/NNN-* CLAUDE/Plan/Archive/
        - Commands that reference archival subdirectories
        """
        # Check for operations involving archival subdirectories
        for subdir in self.ARCHIVAL_SUBDIRS:
            if f'CLAUDE/Plan/{subdir}' in command:
                # Check if it's a mkdir, mv, or cp operation
                if any(op in command for op in ['mkdir', 'mv', 'cp']):
                    return True
                # Check if it's listing an archival directory (not plan discovery)
                if f'ls' in command and f'CLAUDE/Plan/{subdir}' in command:
                    # Allow ls of archival directories (e.g., ls -la CLAUDE/Plan/Completed/)
                    return True

        return False

    def _is_official_command(self, command: str) -> bool:
        """Check if this is the official plan number discovery command.

        Normalizes whitespace before comparison to allow flexible formatting.
        """
        normalized_cmd = re.sub(r'\s+', ' ', command).strip()
        normalized_official = re.sub(r'\s+', ' ', self.OFFICIAL_COMMAND_NORMALIZED).strip()
        return normalized_cmd == normalized_official

    def matches(self, hook_input: dict) -> bool:
        """Check if this is an ad-hoc plan number lookup command."""
        command = get_bash_command(hook_input)
        if not command:
            return False

        # Quick check: does it reference CLAUDE/Plan or */Plan?
        if "CLAUDE/Plan" not in command and "*/Plan/" not in command:
            return False

        # IMPORTANT: Check for archival operations FIRST before pattern matching
        # Archival operations should NOT be blocked even if they contain 'ls' or other commands
        if self._is_archival_operation(command):
            return False

        # Check if it's the official command (allow it) - BEFORE pattern matching
        # This prevents false positives from patterns matching parts of the official command
        if self._is_official_command(command):
            return False

        # Check against all ad-hoc patterns
        for pattern in self.AD_HOC_PATTERNS:
            if re.search(pattern, command, re.IGNORECASE):
                return True

        # If it references plan discovery but isn't the official command
        if any(x in command for x in ["[0-9]", "Plan/[0-9]"]):
            return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block ad-hoc plan lookup and guide to official command."""
        command = get_bash_command(hook_input)

        return HookResult(
            decision="deny",
            reason=(
                "🚫 BLOCKED: Ad-hoc plan number lookup command detected\n\n"
                f"Command: {command}\n\n"
                "WHY:\n"
                "Ad-hoc plan discovery commands are fragile and undocumented. "
                "They make assumptions about directory structure that can break.\n\n"
                "✅ USE THE OFFICIAL COMMAND:\n"
                "Read CLAUDE/Plan/CLAUDE.md for the canonical plan numbering command.\n"
                "The official command handles all edge cases and is documented.\n\n"
                "OFFICIAL COMMAND:\n"
                "  find CLAUDE/Plan -maxdepth 2 -type d -name '[0-9]*' | "
                "grep -oP '/\\K\\d{3}(?=-)' | sort -n | tail -1\n\n"
                "This command:\n"
                "  • Finds all plan directories (NNN-*)\n"
                "  • Extracts just the 3-digit plan number\n"
                "  • Sorts numerically\n"
                "  • Returns the highest number\n\n"
                "REFERENCE:\n"
                "  File: CLAUDE/Plan/CLAUDE.md\n"
                "  Section: Plan Numbering"
            )
        )



"""WorktreeFileCopyHandler - individual handler file."""

import re
import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_bash_command, get_file_path, get_file_content


class WorktreeFileCopyHandler(Handler):
    """Prevent copying files between worktrees and main repo."""

    def __init__(self):
        super().__init__(name="prevent-worktree-file-copying", priority=15)

    def matches(self, hook_input: dict) -> bool:
        """Check if copying between worktree and main repo."""
        command = get_bash_command(hook_input)
        if not command or 'untracked/worktrees' not in command:
            return False

        # Check for forbidden operations
        if not re.search(r'\b(cp|mv|rsync)\b', command, re.IGNORECASE):
            return False

        # Check patterns
        patterns = [
            r'untracked/worktrees/[^/\s]+/\S+\s+.*\b(src/|tests/|config/)',
            r'rsync.*untracked/worktrees.*\b(src|tests|config)\b',
        ]

        for pattern in patterns:
            if re.search(pattern, command, re.IGNORECASE):
                # Check if within same worktree
                worktree_count = command.count('untracked/worktrees')
                if worktree_count >= 2:
                    worktree_paths = re.findall(r'untracked/worktrees/([^/\s]+)', command)
                    if len(worktree_paths) >= 2 and worktree_paths[0] == worktree_paths[1]:
                        continue
                return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block worktree file copying."""
        command = get_bash_command(hook_input)

        return HookResult(
            decision="deny",
            reason=(
                "❌ BLOCKED: Attempting to copy files from worktree to main repo\n\n"
                f"Command: {command}\n\n"
                "🔥 WHY THIS IS CATASTROPHIC:\n"
                "  1. Defeats entire purpose of worktrees (isolation)\n"
                "  2. Destroys branch isolation\n"
                "  3. Loses git history (bypasses git tracking)\n"
                "  4. Nukes untracked work in target directory\n"
                "  5. Creates merge conflicts\n\n"
                "✅ CORRECT WORKFLOW:\n"
                "  1. cd untracked/worktrees/your-branch\n"
                "  2. git add . && git commit -m 'feat: changes'\n"
                "  3. cd /workspace (main repo)\n"
                "  4. git merge your-branch\n\n"
                "📖 See CLAUDE/Worktree.md for complete guide."
            )
        )


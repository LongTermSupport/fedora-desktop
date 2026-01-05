#!/usr/bin/env python3
"""Tests for ClaudeReadmeHandler - validate CLAUDE.md content quality."""

import unittest
import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import HookResult
from handlers.pre_tool_use import ClaudeReadmeHandler


class TestClaudeReadmeHandler(unittest.TestCase):
    """Test CLAUDE.md content validation."""

    def setUp(self):
        self.handler = ClaudeReadmeHandler()

    # ========================================
    # LEGITIMATE USE CASES - Should ALLOW
    # ========================================

    def test_allows_bash_commands_in_code_blocks(self):
        """Should ALLOW bash commands in code blocks (legitimate instructions)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE/Plan/CLAUDE.md",
                "content": """# Plan Number Discovery

To find the next plan number, run:

```bash
find CLAUDE/Plan -maxdepth 2 -type d -name '[0-9]*' | sed 's|.*/\\([0-9]\\{3\\}\\).*|\\1|' | sort -n | tail -1
```

Next plan number = highest + 1
"""
            }
        }
        # Should NOT match - code blocks are legitimate in instructional docs
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_typescript_examples_in_code_blocks(self):
        """Should ALLOW TypeScript examples in code blocks."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "scripts/CLAUDE.md",
                "content": """# Script Requirements

All scripts must have shebangs:

```typescript
#!/usr/bin/env -S npx tsx
/**
 * Script description
 */
```

This ensures executability.
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_javascript_examples_in_code_blocks(self):
        """Should ALLOW JavaScript examples in code blocks."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "eslint-rules/CLAUDE.md",
                "content": """# ESLint Rules

Handlers must be synchronous:

```javascript
// ❌ WRONG
create(context) {
  return {
    async Program(node) {
      context.report({...}); // NEVER REPORTED
    }
  };
}

// ✅ CORRECT
create(context) {
  return {
    Program(node) {
      context.report({...}); // WORKS
    }
  };
}
```
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_multiple_code_blocks(self):
        """Should ALLOW multiple code blocks in same file."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/components/CLAUDE.md",
                "content": """# Component Examples

TypeScript example:
```typescript
export const Button = () => {};
```

CSS example:
```css
.button { color: blue; }
```

Both are valid examples.
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_edit_with_code_blocks(self):
        """Should ALLOW Edit operations that add code blocks."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "CLAUDE/Plan/CLAUDE.md",
                "old_string": "Instructions here",
                "new_string": """Instructions here

```bash
npm run build
```
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_inline_code(self):
        """Should ALLOW inline code with backticks."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE.md",
                "content": """# Project Instructions

Use `npm run build` to build the project.

The `src/` directory contains source code.
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_non_claude_md_files(self):
        """Should NOT match non-CLAUDE.md files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "README.md",
                "content": "```bash\nSome code here\n```"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_claude_md_with_references(self):
        """Should ALLOW CLAUDE.md files with proper structure and references."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "src/pages/CLAUDE.md",
                "content": """# Page Development

See PlanWorkflow.md for planning instructions.

Use components from src/components/CLAUDE.md.

Example usage:
```typescript
import { Hero } from '@/components/hero/Hero';
```
"""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # ========================================
    # EDGE CASES
    # ========================================

    def test_ignores_read_operations(self):
        """Should NOT match Read operations (read-only)."""
        hook_input = {
            "tool_name": "Read",
            "tool_input": {
                "file_path": "CLAUDE.md"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_ignores_bash_tool(self):
        """Should NOT match Bash tool operations."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "cat CLAUDE.md"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handles_empty_content(self):
        """Should handle empty content gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE.md",
                "content": ""
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handles_missing_content(self):
        """Should handle missing content gracefully."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE.md"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_case_sensitive_filename(self):
        """Should only match CLAUDE.md, not claude.md or Claude.md."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "claude.md",
                "content": "```bash\ntest\n```"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_matches_claude_md_in_subdirectories(self):
        """Should match CLAUDE.md in any subdirectory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "deeply/nested/path/CLAUDE.md",
                "content": "Valid content"
            }
        }
        # Should NOT match if content is valid (no forbidden patterns)
        self.assertFalse(self.handler.matches(hook_input))

    # ========================================
    # POTENTIAL QUALITY ISSUES - Currently no blocks
    # These tests document that we've decided NOT to block these patterns
    # ========================================

    def test_allows_implementation_details_phrase(self):
        """Should ALLOW phrase 'implementation details' (common in docs)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE.md",
                "content": """# Project Structure

For implementation details, see the specific component docs.
"""
            }
        }
        # Phrase "implementation details" is legitimate in context
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_specific_code_examples_phrase(self):
        """Should ALLOW phrase 'specific code examples' (legitimate instruction)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "CLAUDE.md",
                "content": """# Guidelines

Avoid specific code examples in CLAUDE.md files.

Instead, reference detailed documentation.
"""
            }
        }
        # The phrase itself is fine when used instructionally
        self.assertFalse(self.handler.matches(hook_input))


if __name__ == '__main__':
    unittest.main()

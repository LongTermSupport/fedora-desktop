#!/usr/bin/env python3
"""Live test for SedBlockerHandler."""

import json
import subprocess
import sys


def test_hook(tool_name, tool_input, should_block=True):
    """Test hook with given input."""
    hook_input = {
        "tool_name": tool_name,
        "tool_input": tool_input
    }

    result = subprocess.run(
        ["python3", "pre_tool_use.py"],
        input=json.dumps(hook_input),
        capture_output=True,
        text=True,
        cwd="/workspace/.claude/hooks/controller"
    )

    output = json.loads(result.stdout) if result.stdout.strip() else {}
    is_blocked = bool(output.get("hookSpecificOutput", {}).get("permissionDecision") == "deny")

    status = "✓" if is_blocked == should_block else "✗"
    expected = "BLOCK" if should_block else "ALLOW"
    actual = "BLOCKED" if is_blocked else "ALLOWED"

    print(f"{status} {expected}: {tool_input.get('command', tool_input.get('file_path'))} -> {actual}")

    return is_blocked == should_block


def main():
    """Run live tests."""
    print("Testing SedBlockerHandler live...\n")

    tests = [
        # Should BLOCK
        ("Bash", {"command": "sed -i 's/foo/bar/g' file.txt"}, True),
        ("Bash", {"command": "find . -name '*.ts' -exec sed -i 's/old/new/g' {} \\;"}, True),
        ("Bash", {"command": "cat file.txt | sed 's/old/new/g' > output.txt"}, True),
        ("Write", {"file_path": "script.sh", "content": "#!/bin/bash\nsed -i 's/old/new/g' file.txt"}, True),

        # Should ALLOW
        ("Bash", {"command": "grep -r 'sed' ."}, False),
        ("Bash", {"command": "echo 'Do not use sed'"}, False),
        ("Bash", {"command": "cat src/based/test.ts"}, False),
        ("Write", {"file_path": "README.md", "content": "Do not use sed"}, False),
        ("Read", {"file_path": "script.sh"}, False),
    ]

    passed = sum(test_hook(*test_case) for test_case in tests)
    total = len(tests)

    print(f"\n{'='*60}")
    print(f"Results: {passed}/{total} tests passed")
    print(f"{'='*60}")

    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())

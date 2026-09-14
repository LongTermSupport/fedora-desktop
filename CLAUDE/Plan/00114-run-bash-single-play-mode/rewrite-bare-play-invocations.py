#!/usr/bin/env python3
"""Rewrite bare play invocations in tracked docs to `./run.bash --play …` (Plan 00114 T2.2).

Two shapes are rewritten wherever they appear as a command (code block, inline code, echo
hint):

    ansible-playbook [./]playbooks/<x>.yml [args]   ->  ./run.bash --play playbooks/<x>.yml [args]
    ./playbooks/<x>.yml [args]                       ->  ./run.bash --play playbooks/<x>.yml [args]

A trailing ` --ask-become-pass` is dropped from a rewritten line: the runner supplies it
when the box needs it. Files are given explicitly on argv; nothing is discovered.
Exit 1 if any named file was not changed, so a stale list is loud.
"""
import re
import sys
from pathlib import Path

ANSIBLE = re.compile(r"(?<![\w./-])ansible-playbook\s+(?:\./)?(playbooks/[^\s`)\"']+\.yml)")
SHEBANG = re.compile(r"(?<![\w./-])\./(playbooks/[^\s`)\"']+\.yml)")
ASK_PASS = re.compile(r"(\./run\.bash --play [^\n`]*?) --ask-become-pass")


def rewrite(text: str) -> str:
    text = ANSIBLE.sub(r"./run.bash --play \1", text)
    text = SHEBANG.sub(r"./run.bash --play \1", text)
    return ASK_PASS.sub(r"\1", text)


def main(argv: list[str]) -> int:
    unchanged = []
    for name in argv:
        path = Path(name)
        before = path.read_text(encoding="utf-8")
        after = rewrite(before)
        if after == before:
            unchanged.append(name)
            continue
        path.write_text(after, encoding="utf-8")
        changed = sum(1 for a, b in zip(before.splitlines(), after.splitlines()) if a != b)
        print(f"rewrote {name}: {changed} line(s)")
    if unchanged:
        print("ERROR: no bare invocation found in: " + ", ".join(unchanged), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

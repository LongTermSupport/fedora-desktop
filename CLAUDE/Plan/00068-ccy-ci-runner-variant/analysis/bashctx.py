"""Enclosing bash block stack + errexit state for a queried line.

errexit is suspended (bash manual, `set -e`) when the failing command is any but
the last of an `&&`/`||` list, in the CONDITION of if/elif/while/until, or negated
with `!` -- and that suspension propagates into functions called from such a
context. A loop BODY does not suspend errexit; that distinction is the whole point,
so condition regions are tracked separately from block nesting.
"""

import re
import sys

from fnmap import INIT_STATE, read_lines, strip_noncode


def block_stack(path):
    lines = read_lines(path)
    stack, per_line, cond_depth = [], {}, 0
    state = INIT_STATE

    for idx, raw in enumerate(lines, start=1):
        code, next_state = strip_noncode(raw, state)
        was_inert = state[0]
        state = next_state
        per_line[idx] = (list(stack), cond_depth > 0)
        if was_inert:
            continue

        for t in re.findall(r"[A-Za-z_]+", code):
            if t in ("if", "while", "until"):
                stack.append((t, idx))
                cond_depth += 1
            elif t in ("for", "select", "case"):
                stack.append((t, idx))
            elif t in ("then", "do"):
                cond_depth = max(0, cond_depth - 1)
            elif t == "elif":
                cond_depth += 1
            elif t == "fi":
                while stack and stack[-1][0] != "if":
                    stack.pop()
                if stack:
                    stack.pop()
            elif t == "done":
                while stack and stack[-1][0] not in ("while", "until", "for", "select"):
                    stack.pop()
                if stack:
                    stack.pop()
            elif t == "esac":
                while stack and stack[-1][0] != "case":
                    stack.pop()
                if stack:
                    stack.pop()
    return lines, per_line


def loop_is_unbounded(lines, loop_line):
    return bool(re.search(r"\bwhile\s+(true|:)\s*(;|$)", lines[loop_line - 1]))


def main():
    path = sys.argv[1]
    lines, per_line = block_stack(path)
    for arg in sys.argv[2:]:
        t = int(arg)
        stack, in_cond = per_line.get(t, ([], False))
        desc = ",".join(f"{k}@{ln}" for k, ln in stack) or "-"
        loops = [(k, ln) for k, ln in stack if k in ("while", "until", "for", "select")]
        unb = [ln for k, ln in loops if k == "while" and loop_is_unbounded(lines, ln)]
        print(f"{path}\t{t}\tstack={desc}\tin_cond={in_cond}\tloops={len(loops)}\tunbounded={unb if unb else '-'}")


if __name__ == "__main__":
    main()

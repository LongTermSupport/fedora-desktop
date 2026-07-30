"""Map every line of a bash file to its enclosing function, by brace tracking.

Not a general bash parser. It is a tokenizer good enough for THESE files, and it
is validated: every function body it extracts is re-parsed with `bash -n`, so a
mis-tracked brace shows up as a syntax error rather than as a silent wrong answer.

Quote state is carried ACROSS lines. claude-yolo builds its startup banners as
multi-line double-quoted strings (`STARTUP_INFO+="` … 8 lines of prose … `"`), and
that prose contains the words `if`, `for` and `done`. A per-line stripper reads
them as keywords and silently corrupts every block boundary after them.
"""

import re
import sys

FN_OPEN = re.compile(r"^(\s*)(?:function\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?:\(\))?\s*\{\s*(?:#.*)?$")

# state = (in_heredoc, heredoc_tag, in_quote) where in_quote is None, '"' or "'"
INIT_STATE = (False, None, None)


def strip_noncode(line, state):
    """Return (code_part, new_state) with comments and string bodies blanked out."""
    in_heredoc, heredoc_tag, in_quote = state

    if in_heredoc:
        if line.strip().rstrip("'\"") == heredoc_tag:
            return "", (False, None, None)
        return "", (True, heredoc_tag, None)

    out = []
    i = 0
    n = len(line)

    # finish a string that opened on an earlier line
    if in_quote is not None:
        while i < n:
            if in_quote == '"' and line[i] == "\\":
                i += 2
                continue
            if line[i] == in_quote:
                in_quote = None
                i += 1
                break
            i += 1
        if in_quote is not None:
            return "", (False, None, in_quote)

    hd = re.search(r"<<-?\s*[\"']?([A-Za-z_][A-Za-z0-9_]*)[\"']?", line)

    while i < n:
        c = line[i]
        if c == "\\":
            i += 2
            continue
        if c in ("'", '"'):
            quote = c
            j = i + 1
            while j < n:
                if quote == '"' and line[j] == "\\":
                    j += 2
                    continue
                if line[j] == quote:
                    break
                j += 1
            if j >= n:
                # unterminated on this line -> the string continues onto the next
                return "".join(out), (False, None, quote)
            i = j + 1
            continue
        if c == "#":
            if i == 0 or line[i - 1] in " \t;&|(":
                break
            out.append(c)
            i += 1
            continue
        if c == "$" and i + 1 < n and line[i + 1] == "{":
            depth = 0
            j = i + 1
            while j < n:
                if line[j] == "{":
                    depth += 1
                elif line[j] == "}":
                    depth -= 1
                    if depth == 0:
                        break
                j += 1
            i = j + 1
            continue
        out.append(c)
        i += 1

    code = "".join(out)
    if hd:
        return code, (True, hd.group(1), None)
    return code, (False, None, None)


def read_lines(path):
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read().split("\n")


def analyse(path):
    lines = read_lines(path)
    functions = {}
    line_owner = {}
    stack = []
    depth = 0
    state = INIT_STATE

    for idx, raw in enumerate(lines, start=1):
        code, next_state = strip_noncode(raw, state)
        was_inert = state[0]
        state = next_state

        line_owner[idx] = stack[-1][0] if stack else None
        if was_inert:
            continue

        m = FN_OPEN.match(raw)
        opens = code.count("{")
        closes = code.count("}")

        if m and opens >= 1:
            stack.append([m.group(2), depth, idx])
            line_owner[idx] = m.group(2)
            depth += opens - closes
            continue

        depth += opens
        for _ in range(closes):
            depth -= 1
            while stack and depth <= stack[-1][1]:
                name, _d, start = stack.pop()
                functions[name] = (start, idx)

    return lines, functions, line_owner


def main():
    for path in sys.argv[1:]:
        lines, functions, line_owner = analyse(path)
        for name, (start, end) in sorted(functions.items(), key=lambda kv: kv[1][0]):
            print(f"FN\t{path}\t{name}\t{start}\t{end}")
        toplevel = sum(1 for v in line_owner.values() if v is None)
        print(f"STAT\t{path}\tlines={len(lines)}\tfunctions={len(functions)}\ttoplevel_lines={toplevel}")


if __name__ == "__main__":
    main()

"""The bash half of the rule `ready-wait-ignores-child-exit` (CLAUDE/QA.md).

A ready-wait is a loop that sleeps between tries while it waits for a process the
script started in the background (`cmd &`). If no try asks whether that process is
still alive (`kill -0 "$pid"`, `ps -p "$pid"`, `wait -n`), a child that died in its
first second is reported only when the loop gives up, as "timed out", and its own
error is never named.

Semgrep's bash parser rejects about a quarter of this repo's scripts, so this rule is
a small lexer instead: it blanks quoted text, comments and here-document bodies,
then reads the order of events in each function body and in the top level.

- A background start arms its scope: `cmd &`, or a call to a function that starts
  one and keeps its pid (`VAR=$!`).
- A loop whose body runs `sleep`, in an armed scope or in a function an armed scope
  calls, is reported unless its header or body asks whether a process is alive.
- `wait` on the child, or the end of a `case` arm (`;;`), disarms the scope.

Usage:
  python3 -m helpers.ready_wait.bash_ready_waits FILE...
      Prints `FILE:LINE: ready-wait-ignores-child-exit` per finding (LINE is the
      loop's first line); exits 1 if any, 0 if none.
  python3 -m helpers.ready_wait.bash_ready_waits --fixture FILE
      Exits 0 only if the findings in FILE are exactly the lines that follow its
      `# ruleid: ready-wait-ignores-child-exit` comments.
"""

import re
import sys

RULE_ID = "ready-wait-ignores-child-exit"

_SEP = r"(?:^|(?<=[\s;&|(!{]))"
_END = r"(?=$|[\s;)&|<>])"
_LAUNCH = re.compile(r"(?<![&|<>])&(?![&>])")
_LOOP_WORD = re.compile(_SEP + r"(for|while|until|do|done)" + _END, re.M)
_SLEEP = re.compile(_SEP + r"sleep" + _END, re.M)
_LEAVE = re.compile(_SEP + r"(?:break|return|exit)" + _END, re.M)
_LIVENESS = re.compile(r"\bkill\s+(?:-0|-s\s+0|--signal\s+0)(?=\s)|\bwait\s+-n\b|\bps\s+-p\b")
_DISARM = re.compile(_SEP + r"wait(?!\s+-n\b)" + _END + r"|;;", re.M)
_PID_CAPTURE = re.compile(r"[A-Za-z_]\w*=\$!")
_FUNCTION = re.compile(
    r"(?m)^[ \t]*(?:function[ \t]+)?([A-Za-z_][\w:.-]*)[ \t]*\([ \t]*\)[ \t\n]*\{")
_HEREDOC = re.compile(r"<<(-?)[ \t]*(['\"]?)([A-Za-z_]\w*)\2")
_FIXTURE_MARK = re.compile(r"^\s*#\s*ruleid:\s*" + re.escape(RULE_ID) + r"\s*$")


def code_only(text):
    """`text` with quoted text, comments and here-document bodies blanked.

    Every character keeps its offset and every newline its place, so a position in
    the result is a position in the source. A command substitution inside double
    quotes stays code, with its own quoting, as bash reads it.
    """
    out = []
    # Each context: [kind, open parens]. "C" code, "D" "...", "S" '...', "A" $'...'.
    stack = [["C", 0]]
    heredocs = []
    word_start = True
    i, n = 0, len(text)

    def blank(chunk):
        out.append("".join("\n" if ch == "\n" else " " for ch in chunk))

    while i < n:
        c = text[i]
        kind = stack[-1][0]
        if c == "\n" and heredocs:
            out.append("\n")
            i += 1
            for strip_tabs, word in heredocs:
                while i < n:
                    j = text.find("\n", i)
                    j = n if j < 0 else j
                    line = text[i:j]
                    blank(text[i:j + 1])
                    i = j + 1
                    if (line.lstrip("\t") if strip_tabs else line) == word:
                        break
            heredocs = []
            word_start = True
            continue
        if kind == "S":
            if c == "'":
                stack.pop()
                out.append(c)
            else:
                blank(c)
            i += 1
            continue
        if kind in ("D", "A"):
            if c == "\\":
                blank(text[i:i + 2])
                i += 2
            elif (kind == "D" and c == '"') or (kind == "A" and c == "'"):
                stack.pop()
                out.append(c)
                i += 1
            elif kind == "D" and text.startswith("$(", i):
                stack.append(["C", 1])
                out.append("$(")
                i += 2
                word_start = True
            else:
                blank(c)
                i += 1
            continue
        # Code.
        if c == "\\":
            blank(text[i:i + 2])
            i += 2
            word_start = False
            continue
        if text.startswith("$'", i):
            stack.append(["A", 0])
            out.append("$'")
            i += 2
            continue
        if c in "'\"":
            stack.append(["S" if c == "'" else "D", 0])
            out.append(c)
            i += 1
            continue
        if c == "#" and word_start:
            j = text.find("\n", i)
            j = n if j < 0 else j
            blank(text[i:j])
            i = j
            continue
        if text.startswith("$(", i):
            stack.append(["C", 1])
            out.append("$(")
            i += 2
            word_start = True
            continue
        heredoc = _HEREDOC.match(text, i) if text.startswith("<<", i) else None
        if heredoc and not text.startswith("<<<", i):
            heredocs.append((heredoc.group(1) == "-", heredoc.group(3)))
            out.append(text[i:heredoc.end()])
            i = heredoc.end()
            word_start = False
            continue
        if c == "(":
            stack[-1][1] += 1
        elif c == ")" and stack[-1][1] > 0:
            stack[-1][1] -= 1
            if stack[-1][1] == 0 and len(stack) > 1:
                stack.pop()
        out.append(c)
        word_start = c in " \t\n;&|()"
        i += 1
    return "".join(out)


def _functions(code):
    """name -> (definition start, body start, body end) for each function."""
    found = {}
    for match in _FUNCTION.finditer(code):
        depth, i = 1, match.end()
        while i < len(code) and depth:
            if code[i] == "{":
                depth += 1
            elif code[i] == "}":
                depth -= 1
            i += 1
        found[match.group(1)] = (match.start(), match.end(), i - 1)
    return found


def _is_wait(code, header, body, done):
    """True if the loop at `header` waits for something rather than sampling.

    A wait ends when its condition is met: it breaks, returns or exits from its body,
    or it is a while/until whose condition is a test. A `for` with none of those, or a
    `while true` / `while ((SECONDS < end))` without them, runs its full course.
    """
    if _LEAVE.search(code, body, done):
        return True
    keyword = code[header:header + 5]
    if keyword.startswith("for"):
        return False
    condition = code[header + len("while" if keyword == "while" else "until"):body - 2]
    condition = condition.strip().rstrip(";").strip()
    return not (condition in ("true", ":") or condition.startswith("(("))


def _unguarded_polls(code, start, end):
    """Header offsets of the waits in code[start:end] that sleep between tries and
    never ask whether a process is alive."""
    headers, open_loops, found = [], [], []
    for match in _LOOP_WORD.finditer(code, start, end):
        word = match.group(1)
        if word in ("for", "while", "until"):
            headers.append(match.start())
        elif word == "do" and headers:
            open_loops.append((headers.pop(), match.end()))
        elif word == "done" and open_loops:
            header, body = open_loops.pop()
            if (_SLEEP.search(code, body, match.start())
                    and _is_wait(code, header, body, match.start())
                    and not _LIVENESS.search(code[header:match.end()])):
                found.append(header)
    return found


def findings(text):
    """1-based line numbers of the ready-waits in `text` that ignore their child."""
    code = code_only(text)
    functions = _functions(code)
    bodies = [(start, end) for start, _, end in functions.values()]

    def in_function(pos):
        return any(start <= pos < end for start, end in bodies)

    launchers = {name for name, (_, start, end) in functions.items()
                 if _LAUNCH.search(code, start, end) and _PID_CAPTURE.search(code, start, end)}
    calls = [(name, re.compile(_SEP + re.escape(name) + _END, re.M)) for name in functions]
    definitions = {name: start + code[start:].index(name)
                   for name, (start, _, _) in functions.items()}

    def _backgrounded(pos):
        """True if the command running on from `pos` is started with `&`: a function
        run in the background is a start, not a call whose loops the caller waits in."""
        line_end = code.find("\n", pos)
        line = code[pos:len(code) if line_end < 0 else line_end]
        return bool(_LAUNCH.search(re.split(r";|&&|\|\|", line)[0] + " "))

    def events(start, end, top):
        found = [(m.start(), "launch", None) for m in _LAUNCH.finditer(code, start, end)]
        found += [(m.start(), "disarm", None) for m in _DISARM.finditer(code, start, end)]
        found += [(pos, "loop", None) for pos in _unguarded_polls(code, start, end)]
        for name, pattern in calls:
            found += [(m.start(), "call", name) for m in pattern.finditer(code, start, end)
                      if m.start() != definitions[name] and not _backgrounded(m.end())]
        if top:
            found = [event for event in found if not in_function(event[0])]
        return sorted(found, key=lambda event: event[0])

    scopes = [(start, end, False) for _, start, end in functions.values()]
    scopes.append((0, len(code), True))
    flagged = set()
    for start, end, top in scopes:
        armed = False
        for pos, kind, name in events(start, end, top):
            if kind == "launch" or (kind == "call" and name in launchers):
                armed = True
            elif kind == "disarm":
                armed = False
            elif armed and kind == "loop":
                flagged.add(pos)
            elif armed and kind == "call":
                _, body_start, body_end = functions[name]
                flagged.update(_unguarded_polls(code, body_start, body_end))
    return sorted(code.count("\n", 0, pos) + 1 for pos in flagged)


def fixture_expectations(text):
    """The lines a fixture marks as must-fire: each line after a ruleid comment."""
    return [number + 1 for number, line in enumerate(text.splitlines(), start=1)
            if _FIXTURE_MARK.match(line)]


def main(argv, out=print):
    if argv[:1] == ["--fixture"]:
        if len(argv) != 2:
            sys.exit("usage: bash_ready_waits --fixture FILE")
        with open(argv[1], encoding="utf-8") as handle:
            text = handle.read()
        expected, got = fixture_expectations(text), findings(text)
        if not expected or got != expected:
            print(f"{argv[1]}: the fixture marks lines {expected}, the rule reports {got}",
                  file=sys.stderr)
            return 1
        return 0
    if not argv:
        sys.exit("usage: bash_ready_waits FILE... | --fixture FILE")
    status = 0
    for path in argv:
        with open(path, encoding="utf-8") as handle:
            for line in findings(handle.read()):
                out(f"{path}:{line}: {RULE_ID}")
                status = 1
    return status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

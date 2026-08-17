# Upstream report — claude-code-hooks-daemon

Two findings from Plan 00075, both in daemon-owned files that are replaced
wholesale on upgrade, so **neither may be patched locally**. Ready to file; the
text below is written to be pasted into an issue as-is.

Both are scrubbed of project identifiers (paths shown are the container's
generic `/workspace` root).

---

## Issue 1 — `_resolve_python_cmd` returns success when python resolution fails

**File**: `.claude/init.sh` (as deployed), lines 308–314.

```bash
    if PYTHON_CMD="$(resolve_venv_python "$HOOKS_DAEMON_ROOT_DIR")"; then
        return 0
    fi

    local rv=$?
    PYTHON_CMD=""
    return "$rv"
```

`$?` on line 312 is read **after the `fi`**, so it is the status of the `if`
*statement*, not of its condition. An `if` whose condition fails and which has no
`else` completes successfully, so `$?` is **0**. The function therefore returns
**success with an empty `PYTHON_CMD`** on exactly the path written to report
failure.

**Demonstration** (plain bash, no daemon involved):

```bash
$ bash -c 'f() { if X="$(false)"; then return 0; fi; local rv=$?; echo "rv=$rv"; return "$rv"; }; f; echo "caller sees: $?"'
rv=0
caller sees: 0
```

**Impact**: every caller that checks `_resolve_python_cmd`'s status believes the
interpreter was resolved. The failure surfaces later and somewhere else — as an
empty command, not as "no python found" — which is strictly harder to diagnose
than the original error.

**Suggested fix** — capture the status where it is produced:

```bash
    local rv=0
    if ! PYTHON_CMD="$(resolve_venv_python "$HOOKS_DAEMON_ROOT_DIR")"; then
        rv=$?          # inside the branch: this IS the condition's status
        PYTHON_CMD=""
    fi
    return "$rv"
```

**Note**: `shellcheck` does **not** catch this, even with `--enable=all`
(verified). SC2181 covers the `[ $? -eq 0 ]` idiom, not a status read from the
wrong scope. We now gate it with a custom semgrep rule; the pattern is:

```yaml
- pattern-regex: '(?m)^[ \t]*(fi|done|esac|\})[ \t]*$\n(?:[ \t]*(#[^\n]*)?\n)*[ \t]*(local[ \t]+|declare[ \t]+)?[A-Za-z_][A-Za-z0-9_]*=\$\?'
```

Happy to contribute that rule upstream if it would be useful.

---

## Issue 2 — `lint_on_edit` cannot be exempted for a deliberately-invalid fixture

**Handler**: `post_tool_use.lint_on_edit`.

A project that keeps an **annotated linter fixture** — a file whose every line is
a deliberate anti-pattern, because `# ruleid:` assertions are what test the
rules — cannot exempt it from `lint_on_edit`. Both documented routes were tried,
and neither is honoured:

1. Per-handler options:

   ```yaml
   post_tool_use:
     lint_on_edit:
       enabled: true
       priority: 25
       options:
         exclude_paths:
           - '.semgrep/**'
   ```

2. Project-wide (as documented in the generated `CLAUDE.md` guidance for
   `error_hiding_blocker` and `qa_suppression`):

   ```yaml
   daemon:
     exclude_paths:
       - '.semgrep/**'
   ```

The daemon was restarted after each change and the behaviour was unchanged: every
edit to the fixture still returns ~20 shellcheck findings as a
`PostToolUse ... blocking error`.

**Impact**: minor but persistent. The edit *does* land — the handler reports but
does not prevent the write — so it is noise rather than a blocked path. The
practical effect is that a wall of findings is printed on every edit to a file
where those findings are the point, which trains the reader to ignore the
handler's output.

**What we did**: removed the inert config rather than leave settings that look
active and are not, and left a comment recording the finding.

**Ask**: either honour `daemon.exclude_paths` in `lint_on_edit`, or document that
it does not apply to this handler.

---

## Also observed, not worth an issue

`/hooks-daemon restart` via the skill script fails in the CCY container:

```
/tmp/tmp.XXXXXXXX: line 142: /tmp/_resolve-venv.sh: No such file or directory
```

The documented direct path works fine and is what we use:

```
/workspace/.claude/hooks-daemon/bin/hooks-daemon restart
```

Mentioned only in case it is the same venv-resolution path as Issue 1.

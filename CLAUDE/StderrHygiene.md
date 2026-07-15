# Stderr Hygiene — Diagnostics to stderr, stdout is the payload

**A script or function's stdout is its *return value*. Everything that is not
that return value — status, progress, prompts, warnings, diagnostics — goes to
stderr.**

This is a first-class coding standard for every script and shell/Python function
this repo ships or generates. It is the general rule; the human-interactive UX
rules in [InteractiveScripts.md](InteractiveScripts.md) (rule 08) are a *subset*
of it that also covers prompting and colour.

---

## The rule

> **stdout = the one thing a caller would `$(capture)`. stderr = everything a
> human reads to follow along.**

If a function emits a value a caller captures — a path, a JSON blob, a username,
a token, a computed number, a single status word — then **nothing else may touch
stdout**. Status lines (`Switching to …`, `Installing …`, `Done`), warnings,
prompts, and diagnostics must be redirected with `>&2`.

Mixing the two is the bug. It is silent until a caller captures the output, and
often *conditionally* silent — it only bites on the code path that emits the
extra chatter. The canonical case that motivated this standard: a generated
`gh-<alias>()` wrapper printed `echo "Switching to <user>..."` on stdout before
running `gh "$@"`, so `$(gh-<alias> … --json)` returned that line glued to the
JSON and `jq` broke — but only when that account was not the box's default.

---

## Decision procedure

Ask of every `echo` / `printf` / `print()`:

1. **Is this the function's captured output** — the value a caller assigns from
   `$(...)` or pipes into `jq`/`read`? → **stdout** (leave it bare).
2. **Is this for a human to watch** — status, progress, "switching", "installing",
   "warning", a prompt, an error explanation? → **stderr** (`>&2`).
3. **Does the function emit *both*** a captured value *and* human chatter? → the
   value goes to stdout, the chatter goes to stderr. They must not share stdout.

If a function has **no** captured-value output — its entire job is to print a
report for a human (`gh-accounts`, `git-accounts`, a `*-status`/`*-help`
command) — then its human text legitimately IS its stdout. Those are **not
bugs**; do not "fix" them by redirecting to stderr. The test is *"does any caller
capture this in `$(...)`?"* — if no, the human report is the payload.

---

## Canonical patterns

### Bash — chatter to stderr, value to stdout

```bash
resolve_thing() {
    echo "Resolving thing for $1..." >&2      # human progress → stderr
    local value
    value="$(compute "$1")" || {
        echo "Error: could not resolve $1" >&2  # error explanation → stderr
        return 1
    }
    printf '%s\n' "$value"                     # the captured value → stdout
}

# Caller gets a clean value regardless of the chatter:
thing="$(resolve_thing foo)"
```

### Bash — a passthrough wrapper must keep the wrapped command's stdout pure

```bash
gh-<alias>() {
    if needs_switch; then
        echo "Switching to <user>..." >&2     # >&2 so $(gh-<alias> … --json) stays pure
        gh auth switch --user "<user>" 2>/dev/null || {
            echo "Error: <user> not authenticated." >&2
            return 1
        }
    fi
    gh "$@"                                     # the wrapped command owns stdout
}
```

### Python helper — diagnostics to stderr, marker/value lines to stdout

Helpers under `helpers/` print **stable marker lines** (`PYENV-CHANGED`,
`PYENV-DONE`) that a play parses — those are intended stdout. Human diagnostics
go to stderr:

```python
import sys

print("Resolving pyenv versions...", file=sys.stderr)  # diagnostic → stderr
...
print("PYENV-CHANGED")                                  # parsed marker → stdout
```

---

## Scope

Applies to:

- Extensionless executables and `*.bash` under `files/home/.local/bin/`.
- **Generated** bash written into user dotfiles by playbooks (blockinfile /
  template / copy) — these define functions users call and capture, so they are
  exactly the high-risk surface.
- Embedded `shell:` blocks in `playbooks/**`.
- `scripts/**` helpers whose stdout another script consumes.
- `helpers/**` Python executors (marker lines = stdout; diagnostics = stderr).

Out of scope (legitimately stdout): help/status/report commands whose entire
purpose is to print for a human and whose output no script captures.

---

## Review checklist

- [ ] Does this function/script emit a value a caller captures? If so, is stdout
  *only* that value?
- [ ] Are all status/progress/"switching"/"installing" lines on stderr (`>&2`)?
- [ ] Are error explanations on stderr (the non-zero return code carries the
  failure; the message is for the human)?
- [ ] For a passthrough wrapper: is the wrapped command the sole owner of stdout?
- [ ] For a Python helper: are parsed marker lines the only thing on stdout, with
  diagnostics on stderr?

---

## Possible follow-up

A lightweight semgrep/grep QA gate could flag "human-looking `echo` (no `>&2`)
inside a function that also emits a captured value" — but the signal is noisy
(distinguishing a report command from a value-emitter needs judgement), so it is
**not** built here. Left as a deliberate future decision, not an accidental gap.

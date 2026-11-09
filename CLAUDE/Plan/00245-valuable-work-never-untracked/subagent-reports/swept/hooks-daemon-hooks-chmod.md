# Bug report: `git_hooks_executable_fixer` chmods non-hook files, and fires on any text match

**Component**: `claude-code-hooks-daemon` — `handlers/post_tool_use/git_hooks_executable_fixer.py`
**Version observed**: 3.51.0
**Severity (reporter's assessment)**: Low — no security impact. It produces spurious tracked-file mode changes in the user's repository, which is confusing and can be committed by accident.
**Environment**: Linux, rootless Podman container, repo using `core.hooksPath`

---

## Summary

Two related defects, both reproduced in normal use:

1. **The fix is applied too broadly.** `_make_hooks_executable()` chmods *every*
   non-`.sample` file in the hooks directory, regardless of whether git would
   ever execute it. In a repo whose `core.hooksPath` points at a **tracked**
   directory, this makes documentation executable and produces a tracked mode
   change (`100644` → `100755`) that shows up as an unexplained dirty file.

2. **The trigger is a substring match on arbitrary Bash output.** `matches()`
   fires whenever the combined stdout/stderr of *any* Bash command contains
   `"not set as executable"`. That includes grepping the handler's own source,
   reading a log, or quoting the message in docs — so a **read** operation
   causes a **write** to the filesystem.

I hit (2) while investigating (1), which is how both are confirmed rather than theoretical.

---

## Evidence

### Defect 1 — a README made executable

This repo sets `core.hooksPath` to a tracked directory:

```console
$ git config --get core.hooksPath
scripts/git-hooks

$ git ls-files -s scripts/git-hooks/
100644 ... scripts/git-hooks/README.md      <- tracked as NON-executable
100755 ... scripts/git-hooks/commit-msg
100755 ... scripts/git-hooks/pre-commit
```

After the handler ran, the working tree disagreed with the index:

```console
$ ls -l scripts/git-hooks/
-rwxr-xr-x. 1 root root  1696 README.md     <- now executable
-rwxr-xr-x. 1 root root  6621 commit-msg
-rwxr-xr-x. 1 root root 19964 pre-commit

$ git diff --summary scripts/git-hooks/README.md
 mode change 100644 => 100755 scripts/git-hooks/README.md

$ git config --get core.fileMode
true
```

The two real hooks were **already** `100755` and needed nothing. The only file
the handler changed was the one file git will never execute.

This surfaced as an unexplained modified file in `git status` at the start of a
session, with no author. Tracking it down cost real time, and on a less careful
day it would have been committed as an unrelated mode change.

### Defect 2 — a grep of the handler's own source triggers the chmod

```console
$ rg -n "sample|chmod|st_mode" .claude/hooks-daemon/src/.../git_hooks_executable_fixer.py
...
183:  "it's not set as executable`, this handler automatically `chmod +x`s "
```

That output contains the signature string, so the handler matched and ran:

```
🔧 Auto-fixed non-executable git hook(s) so they will no longer be silently
skipped: README.md (in <repo>/scripts/git-hooks).
```

Note the sequence: I had chmod'd the README back to `644` in the *same* turn,
and reading the source to understand why undid the fix. No git command was
involved and no hook was skipped.

---

## Root cause

### Defect 1 — `_make_hooks_executable` (lines 155-177)

```python
for entry in hooks_dir.iterdir():
    if not entry.is_file():
        continue
    if entry.name.endswith(_SAMPLE_SUFFIX):
        continue
    ...
    os.chmod(entry, mode | exec_bits)
```

The only exclusion is `.sample`. Git does not execute arbitrary files in the
hooks directory — it invokes hooks **by exact name** (`pre-commit`,
`commit-msg`, `pre-push`, …). Anything not on that list is inert regardless of
its execute bit, so chmod'ing it has no upside and a real downside.

The `.sample` carve-out shows the intent was already "only touch things git
would run" — the implementation just approximates that with the wrong test.

### Defect 2 — `matches` (lines 73-77)

```python
def matches(self, hook_input: dict[str, Any]) -> bool:
    if hook_input.get(HookInputField.TOOL_NAME) != ToolName.BASH:
        return False
    return _WARNING_SIGNATURE in self._combined_output(hook_input).lower()
```

`_WARNING_SIGNATURE = "not set as executable"` — a bare substring, matched
against all Bash output. There is no check that the command was a git command,
nor that the surrounding text is git's actual hint.

For a purely advisory handler this would be harmless. This one **mutates the
filesystem**, which raises the bar: a false positive is a real side effect.

---

## Impact

Not a security issue and not data-destroying — the changes are additive execute
bits, and least-privilege bit arithmetic is already correctly implemented.

The practical costs:

1. **Unexplained dirty files.** With `core.hooksPath` on a tracked directory
   (a common, documented git setup), the user gets a mode change they did not
   make, cannot attribute, and may commit by accident.
2. **The fix does not stick.** Correcting the mode by hand is undone the next
   time any Bash output happens to contain the trigger phrase — including
   reading this handler, reading a log of it, or reading this bug report.
3. **Surprising read-causes-write behaviour.** Inspecting the source of a
   handler should never change files on disk.

---

## Reproduction

**Defect 1**:

1. In any repo: `git config core.hooksPath scripts/git-hooks`
2. Put a `README.md` (mode `644`) in that directory alongside real hooks.
3. Cause git to emit the hint (e.g. temporarily `chmod -x` a hook, then run a
   command that fires it).
4. `git status` → the README now shows `mode change 100644 => 100755`.

**Defect 2** (no git involvement at all):

1. Ensure some file in the hooks directory is non-executable.
2. Run any Bash command whose output contains the phrase `not set as executable` — e.g. `grep -r "not set as executable" <daemon-src>`, or simply
   `echo` the phrase.
3. The handler fires and chmods the directory contents.

---

## Suggested fixes

### Defect 1 — restrict to files git will actually execute

Git's hook names are a fixed, documented set. Matching against it makes the
handler do exactly what its docstring claims:

```python
# githooks(5) - the names git will invoke.
_GIT_HOOK_NAMES = frozenset({
    "applypatch-msg", "pre-applypatch", "post-applypatch",
    "pre-commit", "pre-merge-commit", "prepare-commit-msg", "commit-msg",
    "post-commit", "pre-rebase", "post-checkout", "post-merge",
    "pre-push", "pre-receive", "update", "proc-receive", "post-receive",
    "post-update", "reference-transaction", "push-to-checkout",
    "pre-auto-gc", "post-rewrite", "sendemail-validate",
    "fsmonitor-watchman", "p4-changelist", "p4-prepare-changelist",
    "p4-post-changelist", "p4-pre-submit", "post-index-change",
})

for entry in hooks_dir.iterdir():
    if not entry.is_file() or entry.name not in _GIT_HOOK_NAMES:
        continue
```

This subsumes the `.sample` check (`pre-commit.sample` is not in the set), so
that special case can go.

A cheaper variant, if you would rather not carry the name list: parse the
offending hook's name out of git's hint (it is quoted in the message) and fix
**only** that file. That is arguably more correct still — it fixes what git
actually complained about, and nothing else.

### Defect 2 — require the match to be a genuine git hint

Tighten `matches()` so a passing mention cannot trigger a filesystem write.
Options, roughly in order of preference:

1. **Match git's full hint shape**, not a fragment — e.g. a regex like
   `hint:\s*The '([^']+)' hook was ignored because it's not set as executable`.
   This also hands you the hook name for the targeted fix above.
2. **Also require the command to be a git invocation** (`tool_input.command`
   starts with `git` / contains a git subcommand). Cheap, and rules out logs
   and greps.
3. At minimum, require the `hint:` prefix, so quoted prose does not qualify.

Option 1 fixes both defects at once, which is why I would start there.

### Regression test suggestions

- A hooks dir containing `README.md` (644) plus a non-executable `pre-commit`:
  assert `pre-commit` gains `+x` and `README.md` is **unchanged**.
- A Bash event whose output merely *mentions* the phrase (no `hint:` prefix,
  non-git command): assert `matches()` is `False` and no file is touched.

---

## Note on the existing tests

I have not read your test suite, so treat this as a question rather than a
claim: if the current tests build a fixture directory containing only hook-named
files, both defects would pass unnoticed — the first because there is no
non-hook file to wrongly chmod, the second because the input is always a
realistic git hint. Adding the two cases above would cover the gap either way.

---

## What we did downstream

Nothing in `.claude/hooks-daemon/` was modified — we treat it as a read-only
vendored dependency.

We restore the file mode by hand when we notice it (`chmod 644` on the README).
That is not a fix: any later Bash output containing the trigger phrase reverts
it, and the phrase now appears in this very report.

Happy to test a patch against this environment.

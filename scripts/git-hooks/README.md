# Git Hooks for Security

This directory contains git hooks that prevent accidental commits of sensitive information to this public repository.

## Hooks

- **pre-commit**: Scans staged files for sensitive patterns (API keys, tokens, private emails, etc.)
- **commit-msg**: Validates commit messages for sensitive information
- **lib/secret-scan.bash**: Sourced by *both* hooks — per-token whitelist filtering and the private-identifier denylist built from the gitignored `localhost.yml`

The library exists because the two hooks had drifted: only `pre-commit` ran the
denylist, and only `pre-commit` filtered whitelists per token rather than per
line. So an account alias or hostname was rejected in a staged **file** and
accepted in a commit **message** — the surface no follow-up commit can fix. One
implementation, sourced twice, is what keeps them in step.

Both hooks abort if the library is missing rather than continuing with fewer
checks, and `playbooks/imports/play-git-hooks-security.yml` verifies it is
present. Denylist matches are always reported by their **source field name** —
the private value itself is never printed.

## Installation

These hooks are **automatically configured** by `run.bash` during initial setup.

To manually enable these hooks in an existing clone:

```bash
# Configure git to use this directory for hooks
git config core.hooksPath scripts/git-hooks
```

To verify hooks are active:

```bash
git config core.hooksPath
# Should output: scripts/git-hooks
```

## How It Works

Git's `core.hooksPath` configuration tells git to look for hooks in this tracked directory instead of `.git/hooks/`. This ensures:

- ✅ Hooks are version-controlled and distributed with the repository
- ✅ All contributors use the same hook scripts
- ✅ Updates to hooks are automatically pulled
- ✅ No manual copying or symlinking required

## Bypassing Hooks (Not Recommended)

If absolutely necessary, hooks can be bypassed with:

```bash
git commit --no-verify
```

**WARNING:** Only use this if you are certain your commit contains no sensitive information.

## Testing Hooks

The scanner library has a unit suite, and `qa-all.bash` runs it as a hard gate. It can
also be run alone:

```bash
./scripts/test-secret-scan.bash
```

It is wired in, unlike `test-planlib.bash`, because a false-negative regression in the
scanner is silent by construction: a leak it stopped catching produces no signal on any
commit. The library running on every commit proves nothing about that.

Every value in it is synthetic, because the real denylist is built from a gitignored file
holding the owner's actual identifiers and this repository is public.

It pins one regression in particular. Matching used to be a bare substring test, so a short
identity token matched inside longer unrelated words, and commits touching files that
legitimately contain such a word were rejected with no way to comply. Matching is now
word-boundary (`grep -qwF`), and the suite asserts both that the false positive is gone and
that every shape a leaked identifier actually takes is still caught.

Test that the hooks themselves are wired up:

```bash
# Test that private email domains are blocked (.dev, .internal, .corp, .local):
echo "user@privateco.invalid" > test.txt  # Use .invalid to demonstrate
git add test.txt
git commit -m "test"  # Replace .invalid with .corp to test blocking

# Test that safe domains pass:
echo "test@example.com" > test.txt
git add test.txt
git commit -m "test"  # Should succeed
```

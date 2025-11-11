# Git Hooks for Security

This directory contains git hooks that prevent accidental commits of sensitive information to this public repository.

## Hooks

- **pre-commit**: Scans staged files for sensitive patterns (API keys, tokens, private emails, etc.)
- **commit-msg**: Validates commit messages for sensitive information

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

Test that hooks are working:

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

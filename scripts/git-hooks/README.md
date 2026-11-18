# Git Hooks for Security

This directory contains git hooks that prevent accidental commits of sensitive information to this public repository.

## Hooks

- **pre-commit**: Scans staged files for sensitive patterns (API keys, tokens, private emails, etc.)
- **pre-merge-commit**: Runs `pre-commit`'s ccy version gates alone on a merge git completes without a conflict, which never runs `pre-commit` (issue #311). A merge is judged against its parent on the protected line ([CLAUDE/ContainerRules.md](../../CLAUDE/ContainerRules.md#ccy-version-bump-requirement)); CI judges every merge commit the same way (`scripts/qa-ccy-merge-commits.bash`), since a hook can be skipped
- **commit-msg**: Validates commit messages for sensitive information
- **pre-push**: Scans every commit being pushed (message and added lines) for install-specific names: agent teams, seats, containers, hosts and client projects (below); then scans each pushed commit's tree with CI's gitleaks scan (`scripts/qa-gitleaks.bash push`, issue #329), so a secret is caught before CI sees it; then refuses a push that moves a protected branch (F44, F43, F44-arm: the `exempt` list of `scripts/branch-policy.toml`) to a commit GitHub CI's QA workflow has not passed on a push (`scripts/qa-push-gate.bash`, rule `push-without-qa-verdict` in [CLAUDE/QA.md](../../CLAUDE/QA.md#push-without-qa-verdict)); a docs-only range, which CI does not run, is judged instead by a local `qa-changed.bash` run before the move. Work goes to a `ci/<topic>` branch first; once CI is green there, that same commit is pushed to the protected branch
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

## Install-specific names (pre-commit and pre-push)

The localhost.yml denylist cannot know a team name, a seat handle or a container name: the file is
absent in a ccy container, where agents commit, and never held them. `scripts/qa-private-names.bash`
(Go: `src-go/qa/privatenames`) learns them from what the session can read: `pingbus status` (teams
and member handles, with each handle's project name) and the machine's host names. They go to
`<git dir>/private-terms` (mode 0600). The git directory is never tracked, so no `git add` can
commit the list, and every work tree of the checkout shares it. Each run re-learns and keeps the
terms of teams that left the bus.

- `pre-commit` runs `qa-private-names.bash staged`: the added lines and added paths of the staged
  change.
- `commit-msg` runs `qa-private-names.bash message FILE`: the message, before the commit exists
  (git's `#` lines are skipped). A name there is refused while it is still one edit away.
- `pre-push` runs `qa-private-names.bash push`: for each ref, the commits the remote lacks, their
  messages and their added lines. It catches what `--no-verify` or another clone let through.
- **In a ccy container** (the checkout is under `/workspace`) a list that cannot be built (no
  names learned at all, a `pingbus status` that fails, an unreadable or damaged list) is exit 2:
  the hook fails, never skips. Elsewhere, with no agent bus and no list, the scan says `SKIPPED`
  and passes.
- **A session on no team** is not a failure: `pingbus status` exits 78 ("no team is active") and
  has no team or member names to report, so the guard learns the machine's host names alone, and the
  terms an earlier run left stay in the list. Any other failing status is still exit 2.
- **Matching** is case-insensitive and on word boundaries (a hyphen, dot or slash is a boundary;
  a letter, digit or underscore is not).
- **Short or generic terms cannot block everything**: a term under six characters is dropped; a
  term whose every word is generic (`dev`, `infra`, `desktop`, `test`, ...) is dropped; a part
  of a name (its first word) is never a term alone, since it is often an everyday word; tokens in
  `.claude/public-token-allowlist.yml` and this repository's own name are never terms.
- Findings name the place and the source, with the term's number in the list (`#N` is the list
  line), never the term; a path that holds a term is printed with it masked. For triage,
  `PRIVATE_NAMES_CONTEXT=1` adds each line, masked.
- `qa-private-names.bash denylist FILE` writes the terms that block a commit (the same list and
  rule as the scans, so no word that passes is in it) as go-qa-ci's public-safety denylist: one
  extended regular expression per line, which that scan reads with `grep -E -i`, on the
  boundaries above. FILE must be new and outside every work tree of this repository (exit 2
  otherwise); it is written mode 0600, and only `private-names: N terms written` is printed.
  go-qa-ci's `GQACI_DENYLIST` repository secret is set from this mode's output; refresh it when
  the bus's members change.
- `<git dir>/private-terms-export-skip` (untracked, beside the term list) names, one per line,
  terms left out of that denylist only: ordinary words a public project may contain, by the
  owner's decision. Every local scan still blocks them. `#` and blank lines are ignored; an
  entry that names no term is refused by its line number, so a misspelling cannot publish what
  it meant to keep. The count left out is printed beside the count written.

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

It is wired in because a false-negative regression in the scanner is silent by
construction: a leak it stopped catching produces no signal on any commit. The library
running on every commit proves nothing about that. `test-planlib.bash` is wired in
alongside it, for the mirror-image reason — `_planlib.inc.bash` backs every plan script in
the repo, and those scripts may go unrun for months, so a regression there surfaces at the
worst possible moment rather than on the commit that caused it.

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

# Where Claude Code records that a folder is trusted

Supporting research for Plan 00226 Decision 5 and SPEC S25-S29. It answers three questions:
where Claude Code writes the workspace-trust acceptance, under which key, and how cc can
read it without running anything.

## The record

The record is `projects["<key>"].hasTrustDialogAccepted`, a JSON `true`, in Claude Code's
global config file `~/.claude.json`. That file is a sibling of `~/.claude/`, not inside it.

The documentation says so in three places:

- Settings, "Settings files and who they affect"
  (<https://code.claude.com/docs/en/settings>): `~/.claude.json` "holds your account details,
  MCP server configurations, per-project state such as trust decisions, and the global
  config keys that `/config` writes for you".
- Permissions, "Project allow rules and workspace trust"
  (<https://code.claude.com/docs/en/permissions#project-allow-rules-and-workspace-trust>):
  "For the rows that need this exact folder trusted, trust it by hand: set
  `projects["<path>"].hasTrustDialogAccepted` to `true` in `~/.claude.json`, where `<path>`
  is the repository root, or the folder itself outside a repository."
- Claude directory reference, "Clear local data"
  (<https://code.claude.com/docs/en/claude-directory>): the purge plan lists
  `projects["/home/user/work/my-repo"]` as the "project entry in ~/.claude.json (trust,
  history, MCP servers)".

This repository already depends on the same key: ccy's `entrypoint.sh` sets
`.projects["/workspace"].hasTrustDialogAccepted = true` in the container's `.claude.json`
so that the dialog does not appear (`files/var/local/claude-yolo/entrypoint.sh`, the block
headed "Mark /workspace as trusted").

## The key

The same Permissions section says how the key is chosen:

- "In a repository, Claude Code keys the trust on the git repository root, so the trust
  covers the whole repository apart from any git repository nested inside it, such as a
  submodule. In a worktree, it uses the main checkout's root."
- Outside a repository the key is the starting directory, and that trust covers its
  subdirectories.
- "When you start in your home directory, Claude Code holds the trust for the current
  session only and doesn't write it to disk."

## What the shipped code does (Claude Code 2.1.310)

These expressions were read from the bundled JavaScript inside the installed executable.
The function names are minified and change between builds, so only the logic is recorded
here:

- The check is `projects?.[key]?.hasTrustDialogAccepted === true` on the global config.
- Inside a repository the lookup walks up from the starting directory and stops at the
  repository root. A trusted directory above the root (the home directory, say) does not
  count for a folder inside the repository.
- When `CLAUDE_CODE_IGNORE_PERSISTED_TRUST` is set, the global config is replaced by `{}`
  for this check: "no workspace trust record counts in this process, because
  CLAUDE_CODE_IGNORE_PERSISTED_TRUST is set".
- `CLAUDE_CODE_SANDBOXED` makes the check return trusted with no record.
- A config-path helper builds the file as `join(configDir || homedir(), ".claude.json")`.
  So with `CLAUDE_CONFIG_DIR` set, the file is `$CLAUDE_CONFIG_DIR/.claude.json`.
  - The documentation names only `~/.claude.json`.
  - `cc` itself leaves its onboarding seed at `$HOME/.claude.json`, and its own comment
    says not to infer this (`files/var/local/claude-code/cc`, the block above
    `CLAUDE_JSON=`).
  - SPEC S25 follows the shipped code. Acceptance S21 confirms the path for the case this
    host uses, which is the variable unset. If the rule were wrong with the variable set,
    cc would read a file that holds no trust and run unwrapped, which is the safe
    direction.

## How cc reads it without running anything

cc-claude opens the file with a plain read and decodes it with `encoding/json`. It runs no
`git`, no `claude` and no project file. It finds the main checkout of a linked worktree
from files alone.

**Following `commondir` can be spoofed (review r2 B2).** Both the worktree's `.git` file and
the `commondir` it leads to can belong to the tree being launched. With git 2.39, a tree
inside a trusted checkout that holds these was accepted as a worktree:

- `.git` = `gitdir: .x`;
- `.x/HEAD`;
- `.x/commondir` = `../../.git`.

`git rev-parse --git-common-dir` then named the trusted checkout's `.git`. The probe ran in
`untracked/scratch/`. It is not known whether Claude Code is fooled the same way, and cc must
not depend on it either way.

**The rule SPEC S25 uses instead**: git's own back-link.

- `git worktree add` writes `<main>/.git/worktrees/<id>/gitdir`, naming the worktree's
  `.git` file. git writes it absolute by default, and relative to its own directory with
  `worktree.useRelativePaths`.
- That file sits in the main checkout's `.git`, which the launched tree cannot write.
- So cc accepts a `.git` file only when its `gitdir:` resolves outside the checkout to
  `<C>/worktrees/<id>`, with `<C>` named `.git`, and that back-link names this checkout's
  `.git`. The key is then `<C>`'s parent.
- A directory `.git` keys on the checkout itself.
- Anything else has no key. That includes a `.git` symlink, a submodule
  (`.git/modules/...`), a `--separate-git-dir` checkout, and a missing target.

Claude Code keys a submodule on its own root, so cc is stricter than Claude Code there, which
is the safe direction.

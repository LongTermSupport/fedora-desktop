# Claude Code Configuration

## Critical Rules

### CCY Container: Edit Only, Deploy on Host

**IF THE PROJECT PATH IS `/workspace/` — YOU ARE IN A CCY CONTAINER.**

- **NEVER run Ansible playbooks** in the container
- **Only edit and commit** — then tell the user to deploy on their HOST system
- CCY version bump required when modifying `files/var/local/claude-yolo/claude-yolo`

**Full container rules and the retired ctrl+z patch:** [CLAUDE/ContainerRules.md](CLAUDE/ContainerRules.md)

### Fail Fast — HARD RULE

**This is the #1 principle of this project. It is non-negotiable.**

- **Exit immediately on errors** — Use `set -e` in all bash scripts
- **No silent failures** — Every error must stop execution with clear message
- **NEVER skip and continue** — If an operation should succeed, FAIL on error
- **NEVER decouple dependent operations** — If task B depends on task A, failure in A must prevent B
- ❌ `failed_when: false` — PROHIBITED unless annotated with `# FAIL-FAST-OK: <reason>`
- ❌ `ignore_errors: true` — PROHIBITED unless annotated with `# FAIL-FAST-OK: <reason>`
- ❌ "Skip and warn" pattern — NEVER use `debug` to warn and continue
- ✅ Probe-then-fail pattern — `failed_when: false` is OK when registered result is explicitly checked

### Public Repository — Never Commit Secrets

This is a public repository. Never commit personal information, credentials, hardcoded paths, or sensitive data. Always use Ansible variables, placeholders, and Vault encryption.

**Full security rules, vault management, and pre-commit checks:** [CLAUDE/SecurityRules.md](CLAUDE/SecurityRules.md)

### Infrastructure as Code — No Manual Operations

ALL system changes MUST go through Ansible playbooks. Never perform manual file copies, installations, service management, or configuration edits.

**STRICT IAC — NO MANUAL FIX. DO NOT SUGGEST A MANUAL/QUICK FIX, EVEN AS A "do this now" shortcut alongside the playbook. ONLY IAC: fix it in the playbook and tell the user to run the playbook. Nothing else.**

**Full IaC workflow (edit → playbook → deploy → test):** [CLAUDE/InfrastructureAsCode.md](CLAUDE/InfrastructureAsCode.md)

### Missing Dependencies — Fail Fast, Fix in IaC

**Never accept a missing dependency. Never paper over it. Surface it loudly and fix it via IaC.**

When a script, QA gate, or workflow errors with "tool not found" (`semgrep not found`, `pdm: command not found`, `which: no foo in …`, etc.):

1. ❌ **DO NOT** install the tool manually (`pipx install foo`, `dnf install foo`, `npm i -g foo`).
2. ❌ **DO NOT** make the script tolerate the missing tool (skip-if-absent, `|| true`, advisory-only mode).
3. ❌ **DO NOT** dismiss the gap as "pre-existing environment issue" or "unrelated to this task" — it is now your task.
4. ✅ **DO** grep the playbooks for an existing install task — if one exists, instruct the user to re-run that play.
5. ✅ **DO** add the dependency to the relevant playbook if it is missing, then ask the user to run that play.

The host's tool inventory is owned by Ansible. A missing tool is an IaC gap, not a runtime fallback to engineer around. This rule applies to host tools, CCY container tools, and anything else the repo depends on.

### QA Mandatory Before Commits

Run `./scripts/qa-all.bash` before every commit touching Bash or Python files. Run ESLint for extension JavaScript.

**`qa-all.bash` is mechanical and passes green on work that is structurally wrong.** Run the **`qa-reviewer` agent** as the required final step of every plan, and to review any PR or branch diff — it catches misplaced work in the IaC graph, a new playbook that should have been an edit, jargon naming, missing version bumps, plan/docs drift, and verification that does not exercise the code path it vouches for.

**Full QA reference (scripts, what they check, limitations):** [CLAUDE/QA.md](CLAUDE/QA.md)

### Always Push — GitHub Is the Backup

**Push after every commit.** The GitHub remote is this repository's backup; a commit that exists only in a local checkout is not saved. A lost disk, a discarded container, or a deleted worktree takes unpushed commits with it. Never end a session with a branch ahead of its remote, and never hold commits back to batch them. This is standing authorisation: no need to ask before a plain `git push` of the current branch. Force-pushes remain the user's call.

### Debug Commands: Always Non-Interactive

When providing diagnostic commands to users, always use `--no-pager`, `| cat`, or `| head`. Never open pagers or editors.

**Full rules and examples:** [CLAUDE/DebugCommands.md](CLAUDE/DebugCommands.md)

### Interactive Scripts: Human-Friendly, Standardised

Scripts a human runs and is prompted by (extensionless executables and executed `*.bash` files under `files/home/.local/bin/`) MUST follow the standard UX rules: **strict validation, friendly recovery** — validate strictly, but on a recoverable input mistake show a clear error and **re-prompt in a bounded loop** instead of aborting. Reserve hard aborts for genuinely unrecoverable states. Sourced libraries and non-interactive/Ansible-invoked helpers are out of scope (those stay fully non-interactive and fail fast).

**Full rules, scope, and canonical patterns:** [CLAUDE/InteractiveScripts.md](CLAUDE/InteractiveScripts.md)

### Stderr Hygiene: Diagnostics to stderr, stdout is the payload

A script or function's **stdout is its return value** — the one thing a caller
would `$(capture)`. Everything that is not that value (status, progress,
"switching…", prompts, warnings, diagnostics) goes to **stderr** with `>&2`.
Mixing chatter into stdout silently pollutes `$(cmd)` captures and breaks
`jq`/`read` downstream — often only on a conditional code path. This applies to
bash executables, **generated** bash written into user dotfiles by playbooks,
embedded `shell:` blocks, `scripts/`, and `helpers/` Python (parsed marker lines
= stdout; diagnostics = stderr). Help/status/report commands whose entire job is
to print for a human are the exception — their text IS the payload.

**Full rule, decision procedure, patterns, and review checklist:** [CLAUDE/StderrHygiene.md](CLAUDE/StderrHygiene.md)

### Container Engines: Podman First

**Use Podman wherever possible — it is the better system.** Reach for Docker only when a tool genuinely needs it for compatibility or legacy reasons, and understand that Docker is significantly less secure than Podman.

- **Podman** (rootless) — default for everything. CCY, devtools, ad-hoc work.
- **Docker** (rootful) — compatibility mode only, e.g. DDEV. `docker` group = root-equivalent.
- **LXC** (rootful) — VM-like full-system containers with systemd inside.

New playbooks needing a container engine must use the `container_engine` variable (default `podman`), not hardcode an engine.

**Full role split, coexistence, and FAQ:** [CLAUDE/ContainerEngines.md](CLAUDE/ContainerEngines.md)

---

## Core Design Principles

- **YAGNI** — Don't add features until actually needed. No speculative code. Delete unused code.
- **DRY** — Extract common patterns. Use variables for repeated values. Reference, don't duplicate.
- **Idempotent** — All operations safe to run multiple times. Use `creates`, conditionals, declarative state.
- **Security First** — Never hardcode secrets. Validate inputs. Least privilege. No credentials in logs.
- **Self-documenting** — Clear names over comments. Comments explain WHY, not what.

---

## Ansible Style

**Full Ansible style rules (playbook structure, markers, packages, services, variables, tasks):** [CLAUDE/AnsibleStyle.md](CLAUDE/AnsibleStyle.md)

---

## Plan Commit Rule

**Never let plan state lag behind the work it tracks.** When code work completes, advances, or invalidates a plan task, the corresponding plan file changes must be committed too — ideally in the same commit as the code, but at minimum within the same session.

This rule exists to prevent **drift between plan state and code state**. It is *not* a restriction on when plans can be committed.

### Encouraged

- Committing a brand-new plan on its own, with no related code yet — fine and recommended so the plan is tracked immediately
- Committing plan research, decision-gate notes, or status updates on their own
- Committing plan progress in the same commit as the code that implements it (preferred when both change in one session)

### Prohibited

- Committing code that completes plan tasks while leaving the plan file unchanged on disk
- Leaving an untracked `CLAUDE/Plan/NNN-…/` directory after committing related work
- Marking tasks ✅ in conversation but not in the plan file
- Bundling unrelated plan edits with unrelated code changes — split them into separate commits

### Quick check before any work commit

```bash
git status  # Look for untracked CLAUDE/Plan/ dirs and unstaged plan edits
git add CLAUDE/Plan/NNN-description/  # Stage plan alongside related code
```

If `git status` shows plan files modified by your session, decide before committing: stage them with the related code, **or** make a separate plan-only commit. Do not leave them dangling.

---

## Path-Triggered Rules — these load themselves

The topic files below are linked, **not** `@`-imported: an `@`-import is re-inlined into
every session whether or not it is relevant, while a link costs nothing until followed.

So that nothing is missed by not being resident, `.claude/rules/*.md` carry `paths:` globs
and load **on demand** whenever a matching file is touched. Each rule is a thin pointer to
the topic file that owns the fact — never a copy.

| Touching…                                                    | Loads                  | Which points at                                 |
| ------------------------------------------------------------ | ---------------------- | ----------------------------------------------- |
| `playbooks/`, `tasks/`, `vars/`, `environment/`, any `*.yml` | `ansible-editing.md`   | AnsibleStyle, InfrastructureAsCode, helpers     |
| any `*.bash`, `scripts/`, `files/**/bin/`                    | `bash-scripts.md`      | StderrHygiene, InteractiveScripts, QA           |
| `files/var/local/claude-yolo/`                               | `ccy-version-bump.md`  | ContainerRules — **the mandatory version bump** |
| `helpers/`, `tests/helpers/`, any `*.py`                     | `python-helpers.md`    | helpers/CLAUDE.md, the ruff pin                 |
| `environment/`, `vault.bash`, `*_vars/`                      | `secrets-and-vault.md` | SecurityRules, ExampleValues                    |
| `extensions/`, gnome-shell dirs                              | `gnome-extensions.md`  | GnomeShell, the ESLint requirement              |
| `scripts/qa-*.bash`, `ruff.toml`, workflows                  | `qa-gates.md`          | QA.md, and how these gates have failed before   |

**When you add a fact that only matters for certain paths, add it to the rule, not here.**
`CLAUDE.md` is resident in every session; the rules are not.

## CLAUDE/ Topic Files Index

Each of these is the single source of truth for its subject. Follow the row you need.

| File                                                      | Content                                                                     |
| --------------------------------------------------------- | --------------------------------------------------------------------------- |
| [ContainerRules.md](CLAUDE/ContainerRules.md)             | CCY container detection, version bump, retired ctrl+z patch                 |
| [ContainerEngines.md](CLAUDE/ContainerEngines.md)         | Podman/Docker/LXC role split; when to use which; security trade-offs        |
| [InfrastructureAsCode.md](CLAUDE/InfrastructureAsCode.md) | Ansible-only workflow, prohibited manual actions                            |
| [AnsibleStyle.md](CLAUDE/AnsibleStyle.md)                 | Playbook structure, markers, packages, services, variables                  |
| [SecurityRules.md](CLAUDE/SecurityRules.md)               | Public repo warning, vault management, pre-commit checks                    |
| [ExampleValues.md](CLAUDE/ExampleValues.md)               | Reserved example IPs/emails/hostnames the secret scanner whitelists         |
| [QA.md](CLAUDE/QA.md)                                     | QA scripts reference, what to run when                                      |
| [DebugCommands.md](CLAUDE/DebugCommands.md)               | Non-interactive command rules for user diagnostics                          |
| [InteractiveScripts.md](CLAUDE/InteractiveScripts.md)     | Human-friendly interactive script rules (validate strictly, retry on loop)  |
| [StderrHygiene.md](CLAUDE/StderrHygiene.md)               | Diagnostics → stderr; stdout is the captured payload (bash + Python)        |
| [GnomeShell.md](CLAUDE/GnomeShell.md)                     | GNOME Shell extension development (Wayland, ESLint, APIs)                   |
| [PlanWorkflow.md](CLAUDE/PlanWorkflow.md)                 | Planning workflow and plan document structure                               |
| [PlanTriage.md](CLAUDE/PlanTriage.md)                     | How to establish facts: plan-local triage scripts (probes go IN the script) |
| [PlanJournalling.md](CLAUDE/PlanJournalling.md)           | `JOURNAL/` entry grammar and the append-only discipline                     |
| [PlanScriptStandards.md](CLAUDE/PlanScriptStandards.md)   | Plan-folder orchestrator rules (R1–R14) and `_planlib.inc.bash`             |
| [AgentNotes.md](CLAUDE/AgentNotes.md)                     | Working practices and project gotchas (feedback + project knowledge)        |

## User Documentation

User-facing docs (installation, architecture, playbooks, troubleshooting) are in `docs/`. See `docs/README.md` for the full index.

---

### Hooks Daemon

This project uses [claude-code-hooks-daemon](https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon) for automated safety and workflow enforcement.

After editing `.claude/hooks-daemon.yaml` — restart the daemon using the `hooks-daemon` skill:

- **Restart**: use the `hooks-daemon` skill with args `restart`
- **Health check**: use the `hooks-daemon` skill with args `health`

> **Important**: `/hooks-daemon` is a **skill** (slash command), not a bash command.
> Invoke it using the Skill tool, e.g. `Skill(skill="hooks-daemon", args="restart")`.
> Do NOT attempt to run `/hooks-daemon` as a bash command — it will fail.

**Key files**:

- `.claude/hooks-daemon.yaml` — handler configuration (enable/disable handlers)
- `.claude/hooks/handlers/` — project-specific custom handlers

**Documentation**: `.claude/hooks-daemon/CLAUDE/LLM-INSTALL.md`

<hooksdaemon>
<!-- Auto-generated by hooks daemon on restart. Do not edit this section — changes will be overwritten. -->

## Hooks Daemon — Active Handler Guidance

The handlers listed below are active in this project. Read this section to avoid triggering unnecessary blocks.

**When a tool is blocked by a handler, do not stop working.** Read the block reason, modify your approach, and continue with your task.

**A file written through Bash is not seen by the content guards that run BEFORE the write.** The PreToolUse handlers below that inspect what a file CONTAINS, or where it lives, key on the `Write` and `Edit` tools — so a `>`, `>>`, `tee` or a `cat <<EOF` heredoc reaches disk unexamined by them: no block, no advisory, no record. **A Bash write that drew no complaint is NOT a write that passed those checks** — use `Write`/`Edit` for file content and they apply.

**The LINTERS are the exception, and they DENY.** `lint_on_edit` and `validate_eslint_on_write` do run on a file a Bash command AUTHORS — a redirect, `tee`, a heredoc — so unparseable Python or failing TypeScript is reported however it reached disk. The write has already landed, so the denial is a failure report to repair with `Edit`, not a rollback. A file the command merely RELOCATES (`cp`, `mv`, `install`, `dd`) is never linted: those bytes were already on disk, so blaming the copy would report a defect the command did not introduce.

The handlers that judge a Bash COMMAND — destructive git, `sed`, pipes, permissions, `curl | sh` — are unaffected and still cover you.

Full detail on any rule: `bin/hooks-daemon explain-rule <ID>`.

## All other enforced rules

<!-- handler: require-absolute-paths -->

<!-- handler: block-ancestry-severing-merge -->

<!-- handler: block-artefact-publishing -->

<!-- handler: block-ask-user-question -->

<!-- handler: bash-safe-mode -->

<!-- handler: block-comment-changelog -->

<!-- handler: block-comment-size -->

<!-- handler: block-curl-pipe-shell -->

<!-- handler: daemon-location-guard -->

<!-- handler: block-dangerous-permissions -->

<!-- handler: prevent-destructive-git -->

<!-- handler: docs-qa-commit-gate -->

<!-- handler: docs-qa-edit -->

<!-- handler: error-hiding-blocker -->

<!-- handler: flaggable-content-channel-guard -->

<!-- handler: require-gh-issue-comments -->

<!-- handler: require-gh-pr-comments -->

<!-- handler: block-git-message-backtick -->

<!-- handler: block-git-stash -->

<!-- handler: github_auto_close_keywords -->

<!-- handler: lock-file-edit-blocker -->

<!-- handler: enforce-lsp-usage -->

<!-- handler: enforce-markdown-organization -->

<!-- handler: enforce-npm-commands -->

<!-- handler: block-pip-break-system -->

<!-- handler: pipe-blocker -->

<!-- handler: plan-number-helper -->

<!-- handler: plan-qa-commit-gate -->

<!-- handler: plan-qa-edit -->

<!-- handler: block-plan-time-estimates -->

<!-- handler: qa-suppression-blocker -->

<!-- handler: quarantine-artefact-read-guard -->

<!-- handler: root-recursion-guard -->

<!-- handler: block-secret-file-read -->

<!-- handler: block-security-antipatterns -->

<!-- handler: block-sed-command -->

<!-- handler: block-sensitive-content -->

<!-- handler: staged-lint-gate -->

<!-- handler: block-sudo-pip -->

<!-- handler: enforce-tdd -->

<!-- handler: validate-instruction-content -->

<!-- handler: verification-result-gate -->

<!-- handler: prevent-worktree-file-copying -->

<!-- handler: block-unread-overwrite -->

<!-- handler: lint-on-edit -->

<!-- handler: validate-eslint-on-write -->

<!-- handler: auto-continue-stop -->

| ID                                 | Blocked                                                                                                            | Why                                                                                                                             | Fix                                                                                                              |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| R-ABSOLUTE-PATH-REQUIRED           | `Read`/`Write`/`Edit` file_path requires absolute path                                                             | Ambiguous about the current working directory and can target the wrong file                                                     | Use an absolute path starting with /                                                                             |
| R-GIT-MERGE-SQUASH                 | `git merge --squash`                                                                                               | Severs ancestry -- git branch -d refuses the branch forever                                                                     | Use git merge --no-ff instead                                                                                    |
| R-GH-PR-MERGE-SQUASH               | `gh pr merge --squash`                                                                                             | Severs ancestry -- git branch -d refuses the branch forever                                                                     | Use gh pr merge --merge instead                                                                                  |
| R-GH-PR-MERGE-REBASE               | `gh pr merge --rebase`                                                                                             | Severs ancestry -- git branch -d refuses the branch forever                                                                     | Use gh pr merge --merge instead                                                                                  |
| R-ARTIFACT-PUBLISH                 | publishing an artefact via the `Artifact` tool                                                                     | The page lives OUTSIDE the project and the repository cannot audit or retract it                                                | Write the file locally and tell the user its path, or ask a human to publish                                     |
| R-ASK-USER-QUESTION-UNJUSTIFIED    | AskUserQuestion without `ASKING BECAUSE:` prefix                                                                   | Asking pauses the session for a question the daemon cannot verify was necessary                                                 | State the assumed answer in output text and proceed, or retry every question prefixed `ASKING BECAUSE: <reason>` |
| R-BASH-SAFE-MODE-PRELUDE-MISSING   | a sequenced Bash invocation with no `set` safety prelude                                                           | Errors in earlier statements can be silently ignored                                                                            | Add `set -euo pipefail` at the top, or gate explicitly with `&&`/\`                                              |
| R-COMMENT-CHANGELOG                | changelog narrative in a code comment                                                                              | A comment describes CURRENT STATE; history belongs elsewhere                                                                    | Move it to git, a changelog file, or the plan's JOURNAL/                                                         |
| R-COMMENT-SIZE                     | a comment growing past its configured size limit                                                                   | Comments should describe current state, not accumulate                                                                          | Shorten the comment, or declare MUST_EXCEED_COMMENT_SIZE_BECAUSE                                                 |
| R-CURL-PIPE-SHELL                  | \`curl                                                                                                             | wget ...                                                                                                                        | bash                                                                                                             |
| R-DAEMON-DIR-CD                    | `cd` into `.claude/hooks-daemon/`                                                                                  | Daemon CLI commands must be run from PROJECT ROOT, causing path confusion otherwise                                             | Run daemon commands from project root, e.g. `bin/hooks-daemon status`                                            |
| R-CHMOD-WORLD-WRITABLE             | `chmod 777`/`chmod a+w`/`chmod o+w`                                                                                | Allows anyone to read, write, and execute, bypassing all file permission security                                               | Use least-privilege permissions instead (755/644/600)                                                            |
| R-GIT-RESET-HARD                   | `git reset --hard`                                                                                                 | Permanently destroys all uncommitted changes                                                                                    | Ask the user to run it manually                                                                                  |
| R-GIT-CLEAN-FORCE                  | `git clean -f`                                                                                                     | Permanently deletes untracked files                                                                                             | Ask the user to run it manually                                                                                  |
| R-GIT-CHECKOUT-DISCARD             | `git checkout -- <file>` / `git checkout .`                                                                        | Discards local changes to file(s) permanently                                                                                   | Ask the user to run it manually                                                                                  |
| R-GIT-RESTORE                      | `git restore <file>`                                                                                               | Discards local changes to files permanently (`--staged`/`-S` is allowed)                                                        | Ask the user to run it manually                                                                                  |
| R-GIT-STASH-DROP                   | `git stash drop`                                                                                                   | Permanently destroys a stashed change                                                                                           | Ask the user to run it manually                                                                                  |
| R-GIT-STASH-CLEAR                  | `git stash clear`                                                                                                  | Permanently destroys all stashed changes                                                                                        | Ask the user to run it manually                                                                                  |
| R-GIT-PUSH-FORCE                   | `git push --force`                                                                                                 | Can overwrite remote history and destroy team members' work                                                                     | Ask the user to run it manually, or coordinate and use `--force-with-lease`                                      |
| R-GIT-BRANCH-FORCE-DELETE          | `git branch -D`                                                                                                    | Force-deletes a branch without checking if it has been merged                                                                   | Use `git branch -d` first (refuses unmerged branches); ask the user for -D                                       |
| R-GIT-COMMIT-AMEND                 | `git commit --amend`                                                                                               | Rewrites the previous commit, creating messy history and potential data loss                                                    | Create a new commit instead                                                                                      |
| R-DOCS-QA-COMMIT                   | a git commit violates a block-level docs QA staged-tree check                                                      | Most doc rot that matters at commit time is cross-file drift a single-file edit hook cannot see                                 | Fix the content per each finding's remediation below and amend the commit                                        |
| R-DOCS-QA-EDIT                     | a documentation Write/Edit violates a block-level docs QA check                                                    | A finding only denies the write when it is BLOCK severity AND the resolved mode for that check is block                         | Fix the content per each finding's remediation below and retry                                                   |
| R-ERROR-HIDING                     | an error-hiding pattern (bare except,                                                                              |                                                                                                                                 | true, empty catch, _ = err, ...)                                                                                 |
| R-FLAGGABLE-CONTENT-CHANNEL        | a content-revealing git/grep command shape over a flaggable path                                                   | It would reveal flaggable content inside routine command output, with no deliberate Read at all                                 | Delegate the WHOLE review to the quarantine subagent instead                                                     |
| R-GH-ISSUE-VIEW-NO-COMMENTS        | `gh issue view` without `--comments`                                                                               | Issue comments contain critical context, clarifications and updates not in the issue body                                       | Add --comments, or include comments in --json fields                                                             |
| R-GH-PR-VIEW-NO-COMMENTS           | `gh pr view` without `--comments`                                                                                  | PR comments contain review feedback and discussion context not in the PR body                                                   | Add --comments, or include comments in --json fields                                                             |
| R-GIT-MESSAGE-BACKTICK             | an unescaped backtick in a double-quoted git commit/tag message                                                    | Bash performs command substitution inside double quotes -- the span is EXECUTED, not quoted                                     | Use single quotes, or git commit -F <file>                                                                       |
| R-GIT-STASH-PUSH                   | `git stash` / `git stash push` / `git stash save`                                                                  | Stashes get forgotten, lost, and block git pull                                                                                 | Use git commit instead — WIP commits are fine                                                                    |
| R-GH-AUTO-CLOSE-KEYWORD            | a GitHub closing keyword + issue reference in a git/gh message                                                     | Auto-closes the referenced issue/PR the moment the commit reaches the default branch, and cannot be disabled repository-side    | Use a non-closing reference instead, e.g. Addresses #123                                                         |
| R-LOCK-FILE-EDIT                   | Direct `Write`/`Edit` of a package manager lock file                                                               | Lock files are generated artifacts; manual edits create checksum mismatches and broken dependency graphs                        | Use the package manager commands instead (e.g. `npm install`, `cargo update`)                                    |
| R-LSP-SYMBOL-LOOKUP                | a symbol-like Grep/Bash grep lookup                                                                                | LSP tools give semantic ~50ms code intelligence; grep is slow and imprecise                                                     | Use goToDefinition/findReferences/workspaceSymbol/hover/documentSymbol instead                                   |
| R-MARKDOWN-WRONG-LOCATION          | MARKDOWN FILE IN WRONG LOCATION — a new `.md` file written to an unrecognised location                             | Markdown files must follow project organization rules                                                                           | Move it into an allowed location, or configure `extra_allowed_markdown_paths`                                    |
| R-MARKDOWN-UNTRACKED-MEMORY        | UNTRACKED CLAUDE MEMORY IS DISABLED FOR THIS PROJECT — a write to `~/.claude/projects/*/memory/*.md`               | That knowledge is per-checkout, un-reviewed, and invisible to teammates — it drifts from the repo and bypasses code review      | Document it in tracked project docs instead (CLAUDE.md, .claude/rules/\*.md, docs/)                              |
| R-MARKDOWN-PLAN-SYNC               | a `.claude/settings.json` `plansDirectory` out of sync with the daemon's plan_workflow config                      | Plan workflow requires plansDirectory to match daemon config to redirect writes correctly                                       | Fix `.claude/settings.json`'s `plansDirectory` key, then restart your session                                    |
| R-NPM-PIPED-COMMAND                | a piped `npm run`/`npx` command                                                                                    | Piping npm/npx commands is pointless — llm: cache files hold the full data                                                      | Run the plain command, then query the cache file with jq                                                         |
| R-NPM-NON-LLM-COMMAND              | a raw `npm run`/`npx` command when llm: wrappers exist                                                             | llm: commands provide LLM-friendly, machine-readable output                                                                     | Use the project's `npm run llm:*` equivalent instead                                                             |
| R-PIP-BREAK-SYSTEM-PACKAGES        | `pip install --break-system-packages`                                                                              | Bypasses PEP 668 protection and can corrupt the system Python installation                                                      | Use a virtual environment or `pip install --user` instead                                                        |
| R-PIPE-TO-TAIL                     | \`                                                                                                                 | tail\`                                                                                                                          | Truncates output and causes information loss                                                                     |
| R-PIPE-TO-HEAD                     | \`                                                                                                                 | head\`                                                                                                                          | Truncates output and causes information loss                                                                     |
| R-PLAN-NUMBER-DISCOVERY            | a bash discovery scan (ls/find/sort+tail) for the next plan number                                                 | Misses subdirectories like Completed/ and disagrees across branches                                                             | Use the printed next plan number, or the git counter directly                                                    |
| R-PLAN-FOLDER-MKDIR                | `mkdir <plan-dir>/NNNNN-name` (hand-creating a plan folder)                                                        | Claims a plan number the moment the folder appears, but nothing records the claim until PLAN.md is written                      | Use the mkplan.bash scaffolder instead                                                                           |
| R-PLAN-QA-COMMIT                   | a git commit violates a block-level plan QA cross-file invariant                                                   | Most plan rot is cross-file and a single-file edit hook cannot see it                                                           | Amend the commit to also stage what each finding's remediation names below                                       |
| R-PLAN-QA-EDIT                     | a PLAN.md/README.md Write/Edit violates a block-level plan QA check                                                | Plan QA linting catches issues you can fix immediately, before they reach commit                                                | Fix the content per each finding's remediation below and retry                                                   |
| R-PLAN-TIME-ESTIMATE               | Time estimates not allowed in plan documents                                                                       | Time estimates in plans create false expectations and pressure                                                                  | Break work into concrete tasks and implementation steps; let the user decide scheduling                          |
| R-QA-SUPPRESSION                   | a QA suppression directive (noqa, type: ignore, eslint-disable, ...)                                               | Suppression comments hide real problems and create technical debt                                                               | Fix the underlying issue; do not suppress the warning                                                            |
| R-QUARANTINE-ARTEFACT-READ         | reading a quarantined `*-opus-security-DETAIL*` artefact into the coordinator                                      | A DETAIL artefact holds raw flaggable substance meant for a human or another quarantine agent only                              | Read the paired `*-opus-security-SUMMARY*` artefact instead                                                      |
| R-ROOT-RECURSION-CATASTROPHIC      | `grep -r`/`find`/`rg`/... rooted at `/`, `/proc`, `/sys`, `/home`, `/root`, `~`, `$HOME`                           | Walks the entire filesystem and can pin every CPU core for hours                                                                | Scope the search to the project (e.g. `rg -l "pattern" .`)                                                       |
| R-SECRET-READ                      | Read/Write/Edit/NotebookEdit/Grep targeting a protected path                                                       | The file's contents must NEVER be read into context by any route — not Read, not Bash, not an interpreter one-liner, not a copy | Use `bin/hooks-daemon secret-meta <path>` for metadata, or ask the user                                          |
| R-SECRET-BASH-MENTION              | a Bash command whose text mentions a protected path                                                                | The file's contents must NEVER be read into context by any route — not Read, not Bash, not an interpreter one-liner, not a copy | Use `bin/hooks-daemon secret-meta <path>` for metadata, or ask the user                                          |
| R-SECRET-SCRIPT-AUTHOR             | a script authored via Write/Edit whose content references a protected path                                         | The file's contents must NEVER be read into context by any route — not Read, not Bash, not an interpreter one-liner, not a copy | Use `bin/hooks-daemon secret-meta <path>` for metadata, or ask the user                                          |
| R-SEC-CODE-INJECTION               | `eval`, `exec`, `new Function`, `__import__`, `instance_eval`, `yaml.load`                                         | Dynamic execution of a string as code                                                                                           | Avoid dynamic code execution; use safe parsing/import alternatives                                               |
| R-SEC-CMD-INJECTION                | `os.system`, `subprocess(..., shell=True)`, `shell_exec`, `proc_open`, `Runtime.exec`, `Process.Start`, `IO.popen` | Shell command construction from untrusted input enables command injection                                                       | Use argument-list APIs (no shell=True) instead of shell string concatenation                                     |
| R-SEC-DESERIALISATION              | `pickle.load`, `Marshal.load`, `unserialize`, `ObjectInputStream`, `XMLDecoder`, `BinaryFormatter`                 | Deserialising untrusted data can execute arbitrary code                                                                         | Use a safe serialisation format (e.g. JSON) instead                                                              |
| R-SEC-XSS                          | `innerHTML`, `dangerouslySetInnerHTML`, `document.write`, `template.HTML`/`JS`/`URL`                               | Injects unescaped content into the DOM/output, enabling XSS                                                                     | Use the framework's safe templating/escaping APIs                                                                |
| R-SEC-HARDCODED-CREDS              | AWS access keys, GitHub tokens, Stripe keys, private key blocks                                                    | Hardcoded credentials leak via source control history and code review                                                           | Use environment variables, never hardcode credentials                                                            |
| R-SEC-UNSAFE-MEMORY                | Rust `from_raw_parts`, `transmute`                                                                                 | Bypasses Rust's memory/type safety guarantees                                                                                   | Use safe conversions (`as`, `From`/`Into`) or validated slice operations                                         |
| R-SED-FILE-MODIFICATION            | `sed`                                                                                                              | Claude gets sed syntax wrong regularly and a single error can destroy hundreds of files                                         | Use the Edit tool (or parallel Haiku agents with Edit for bulk changes)                                          |
| R-SENSITIVE-PUBLIC-PATTERN         | content matching a configured public pattern                                                                       | The pattern is a named, safe-to-disclose signal (a path, a placeholder, profanity, ...)                                         | Remove or replace the matched text before retrying                                                               |
| R-SENSITIVE-SECRET-TERM            | content matching a configured blocked term                                                                         | A gitignored secret word list term was found in this write                                                                      | Ask the user what the cited entry covers, then remove the matching text                                          |
| R-STAGED-LINT-FAILURE              | a staged file fails the cheap syntax check at commit time                                                          | lint_on_edit only ever runs at Write/Edit time, so a git add of pre-existing content skips it entirely                          | Fix the failing file(s) above and re-stage before committing                                                     |
| R-SUDO-PIP-INSTALL                 | `sudo pip install`                                                                                                 | Conflicts with the OS package manager and can corrupt system Python                                                             | Use a virtual environment or `pip install --user` instead                                                        |
| R-TDD-TEST-FIRST                   | creating a production source file without its test file                                                            | TDD requires the test file to exist before the source file                                                                      | Create the test file first (RED), then the source file (GREEN)                                                   |
| R-INSTRUCTION-IMPLEMENTATION-LOG   | implementation logs (e.g. 'created the file X', 'added the class Y')                                               | Instruction files hold permanent instructions, not a log of past edits                                                          | Remove the log sentence; put implementation history in git or a plan JOURNAL/                                    |
| R-INSTRUCTION-STATUS-INDICATOR     | status indicators (e.g. checkmark + 'Complete', 'Done', 'Success', 'Fixed')                                        | A completion emoji records a moment in time, not a permanent fact                                                               | Remove the status marker; instruction files describe the project, not its history                                |
| R-INSTRUCTION-TIMESTAMP            | timestamps (ISO dates such as 2024-03-15)                                                                          | A dated entry is a log line, and instruction files are not a log                                                                | Remove the date; if it is genuinely load-bearing, put it in git history                                          |
| R-INSTRUCTION-LLM-SUMMARY          | LLM summaries (section headings such as '## Summary', '## Key Points', '## Overview')                              | A summary heading is the shape an LLM's own turn-report takes, not project documentation                                        | Remove the heading and fold any durable content into the surrounding instructions                                |
| R-INSTRUCTION-TEST-OUTPUT          | test output counts (e.g. '42 tests passed', '1 test failed')                                                       | A test run's result is a point-in-time fact, not a stable instruction                                                           | Remove the count; CI already reports this on every run                                                           |
| R-INSTRUCTION-FILE-LISTING         | changelog-style file listings (e.g. 'created src/Service/Foo.php')                                                 | A file path preceded by a past-tense action verb is changelog narrative                                                         | Remove the log line; a bare path reference used as documentation stays allowed                                   |
| R-INSTRUCTION-CHANGE-SUMMARY       | change summaries (e.g. 'Added 15 lines', 'Removed 8 lines')                                                        | A line-count delta describes one diff, not a stable instruction                                                                 | Remove the summary; the diff itself is preserved in git                                                          |
| R-INSTRUCTION-COMPLETION-INDICATOR | completion indicators (e.g. 'ALL DONE!', 'Task complete!', 'Finished task')                                        | A completion phrase announces a session's end, not a fact about the project                                                     | Remove the phrase; instruction files should never celebrate finishing a task                                     |
| R-VERIFICATION-RESULT-NOT-CONSUMED | a verifier followed by a mutator with nothing consuming the result                                                 | The verifier can fail and the mutator would still run                                                                           | Gate with `&&`, an explicit exit-code check, or `set -euo pipefail`                                              |
| R-WORKTREE-FILE-COPY               | `cp`/`mv`/`rsync` between a worktree and the main repo                                                             | Defeats worktree isolation, bypasses git tracking, and can nuke untracked work in the target directory                          | cd into the worktree, commit, then git merge back                                                                |
| R-WRITE-CLOBBER                    | `Write` to an existing file you have not read this session                                                         | You cannot know what you are destroying, so you could not report the loss even afterwards                                       | `Read` the file then retry, or use `Edit` for a targeted change                                                  |
| R-LINT-FAILURE                     | a written/authored file that fails its language's lint check                                                       | The write has already landed on disk; this is a failure report, not a rollback                                                  | Fix the reported problems with Edit — do not re-Write the file from scratch                                      |
| R-ESLINT-ERRORS                    | a written/authored TS/TSX file with reported ESLint errors                                                         | The write has already landed on disk; this is a failure report, not a rollback                                                  | Fix the reported problems with Edit (`npx eslint <file> --fix` clears most)                                      |
| R-ESLINT-TIMEOUT                   | an ESLint run that did not finish within the configured timeout                                                    | This handler DENIES on a timeout — unlike lint_on_edit, which allows                                                            | Investigate why ESLint is slow (config, project size); retry the edit                                            |
| R-ESLINT-RUN-FAILURE               | an ESLint invocation that failed to run at all                                                                     | ESLint could not be launched (exception raised invoking it)                                                                     | Check the ESLint wrapper/tsx setup, then retry the edit                                                          |
| R-STOP-QA-FAILURE                  | Stopping while the last QA tool run's own output indicated failure                                                 | QA failures detected in the last QA tool run                                                                                    | Fix the failures, re-run the QA tool, and continue without stopping                                              |
| R-STOP-TAUTOLOGICAL-QUESTION       | Stopping behind a rhetorical continue/confirmation question                                                        | The answer is obvious -- yes, continue the already-planned work now                                                             | Resume the next unit of work immediately; STOPPING BECAUSE: does not exempt this                                 |
| R-STOP-AFTER-TOOL-ERROR            | Stopping right after an unresolved tool_use_error                                                                  | The correct action is to address the cause and retry, not stop                                                                  | Address the tool_use_error's cause (e.g. Read before Edit/Write) and retry                                       |
| R-STOP-CONFIRMATION-QUESTION       | Stopping to ask an obvious confirmation question                                                                   | The daemon auto-continues through confirmation-style questions                                                                  | Proceed with the remaining work; stop with STOPPING BECAUSE: only if truly stuck                                 |
| R-STOP-NO-REASON                   | Stopping without a STOPPING BECAUSE: explanation                                                                   | The stop hook enforces intentional stops                                                                                        | Prefix your stop message with STOPPING BECAUSE: <reason>, or keep working                                        |
| R-STOP-GOAL-LEDGER                 | Stopping while ledgered plan(s) are still In Progress                                                              | The daemon-side goal ledger owes a goal for EVERY In Progress plan, not only the newest /goal condition                         | Continue the listed plan(s), or stop with STOPPING BECAUSE: naming why each cannot proceed                       |

## Advisories and other active handlers

One line each; these fire with their own guidance when relevant. Full text: `bin/hooks-daemon explain-handler <name>`.

<!-- handler: agent-isolation-advisor -->

- agent_isolation_advisor — isolate concurrent agents

<!-- handler: verify-daemon-restart -->

- daemon_restart_verifier — restart the daemon before committing

<!-- handler: flaggable-work-advisor -->

- flaggable_work_advisor — delegate flaggable work BEFORE reading it

<!-- handler: plan-workflow-guidance -->

- plan_workflow — PLAN.md, supporting docs and JOURNAL/ obey DIFFERENT contracts

<!-- handler: prevent-system-file-edits -->

- system_paths — do not edit deployed system files directly

<!-- handler: enforce-ansible-deployment -->

- ansible_enforcement — no direct system management commands

<!-- handler: background-process-tracker -->

- background_process_tracker — backgrounded processes are tracked

<!-- handler: command-hints -->

- command_hints — advisory reminders after specific commands

<!-- handler: git-hooks-executable-fixer -->

- git_hooks_executable_fixer — auto-fixes non-executable git hooks

<!-- handler: goal-injection -->

- goal_injection — plan-start goal signal for the ccy supervisor

<!-- handler: markdown-table-formatter -->

- markdown_table_formatter — markdown tables are auto-aligned

<!-- handler: recovery-cron-advisor -->

- recovery_cron_advisor — failsafe recovery cron lifecycle advisory

<!-- handler: ccy-supervisor-integrity -->

- ccy_supervisor_integrity — keep the ccy supervisor properly set up

<!-- handler: docs-qa-sweep -->

- docs_qa_sweep — documentation drift report at session start

<!-- handler: git-upstream-checker -->

- git_upstream_checker — additive fetch + pull/cleanup advice on session start

<!-- handler: hook-registration-checker -->

- hook_registration_checker — hooks configuration policy

<!-- handler: model-fallback-detector -->

- model_fallback_detector — silent model substitution is surfaced

<!-- handler: plan-qa-sweep -->

- plan_qa_sweep — plan-tree drift report at session start

<!-- handler: plan-workflow-asset-checker -->

- plan_workflow_asset_checker — plan tooling provisioning alert

<!-- handler: project-handler-load-checker -->

- project_handler_load_checker — project protection degraded alert

<!-- handler: secret-file-hygiene-checker -->

- secret_file_hygiene_checker -- on-disk hygiene for protected paths

<!-- handler: tool-disable-advisor -->

- tool_disable_advisor — declared never-want tools are checked at session start

<!-- handler: idle-housekeeping-advisory -->

- idle_housekeeping_advisory — report-first idle housekeeping (beta, opt-in)

<!-- handler: standing-authorisations -->

- standing_authorisations — a project can record a standing request

<!-- handler: auto-approve-reads -->

- auto_approve_reads — gated on bypassPermissions mode

<!-- handler: worktree-create -->

- worktree_create — semantic worktree naming

</hooksdaemon>

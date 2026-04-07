# Claude Code Configuration

## Critical Rules

### CCY Container: Edit Only, Deploy on Host

**IF THE PROJECT PATH IS `/workspace/` — YOU ARE IN A CCY CONTAINER.**

- **NEVER run Ansible playbooks** in the container
- **Only edit and commit** — then tell the user to deploy on their HOST system
- CCY version bump required when modifying `files/var/local/claude-yolo/claude-yolo`

**Full container rules and ctrl+z patch details:** @CLAUDE/ContainerRules.md

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

**Full security rules, vault management, and pre-commit checks:** @CLAUDE/SecurityRules.md

### Infrastructure as Code — No Manual Operations

ALL system changes MUST go through Ansible playbooks. Never perform manual file copies, installations, service management, or configuration edits.

**Full IaC workflow (edit → playbook → deploy → test):** @CLAUDE/InfrastructureAsCode.md

### QA Mandatory Before Commits

Run `./scripts/qa-all.bash` before every commit touching Bash or Python files. Run ESLint for extension JavaScript. Run `./scripts/qa-ctrl-z-patch.bash` for CCY patch changes.

**Full QA reference (scripts, what they check, limitations):** @CLAUDE/QA.md

### Debug Commands: Always Non-Interactive

When providing diagnostic commands to users, always use `--no-pager`, `| cat`, or `| head`. Never open pagers or editors.

**Full rules and examples:** @CLAUDE/DebugCommands.md

---

## Core Design Principles

- **YAGNI** — Don't add features until actually needed. No speculative code. Delete unused code.
- **DRY** — Extract common patterns. Use variables for repeated values. Reference, don't duplicate.
- **Idempotent** — All operations safe to run multiple times. Use `creates`, conditionals, declarative state.
- **Security First** — Never hardcode secrets. Validate inputs. Least privilege. No credentials in logs.
- **Self-documenting** — Clear names over comments. Comments explain WHY, not what.

---

## Ansible Style

**Full Ansible style rules (playbook structure, markers, packages, services, variables, tasks):** @CLAUDE/AnsibleStyle.md

---

## Plan Commit Rule

Plans MUST be committed alongside the work they describe. Never leave plan directories untracked after related work is committed.

```bash
git status  # Check for untracked CLAUDE/Plan/ directories
git add CLAUDE/Plan/NNN-description/  # Stage the plan alongside code changes
```

---

## CLAUDE/ Topic Files Index

| File | Content |
|------|---------|
| @CLAUDE/ContainerRules.md | CCY container detection, version bump, ctrl+z patch |
| @CLAUDE/InfrastructureAsCode.md | Ansible-only workflow, prohibited manual actions |
| @CLAUDE/AnsibleStyle.md | Playbook structure, markers, packages, services, variables |
| @CLAUDE/SecurityRules.md | Public repo warning, vault management, pre-commit checks |
| @CLAUDE/QA.md | QA scripts reference, what to run when |
| @CLAUDE/DebugCommands.md | Non-interactive command rules for user diagnostics |
| @CLAUDE/GnomeShell.md | GNOME Shell extension development (Wayland, ESLint, APIs) |
| @CLAUDE/PlanWorkflow.md | Planning workflow and plan document structure |

## User Documentation

User-facing docs (installation, architecture, playbooks, troubleshooting) are in `docs/`. See `docs/README.md` for the full index.

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

## absolute_path — always use absolute paths

The `Read`, `Write`, and `Edit` tools require absolute paths. Relative paths are blocked.

- **Correct**: `/workspace/src/main.py`, `/workspace/tests/test_utils.py`
- **Blocked**: `src/main.py`, `./config.yaml`, `../other/file.txt`

The working directory is `/workspace`. Prepend `/workspace/` to any relative path before calling these tools.

## curl_pipe_shell — never pipe curl/wget to bash/sh

Piping network content directly to a shell is blocked. It executes untrusted remote code without any inspection.

**Blocked**: `curl URL | bash`, `curl URL | sh`, `wget URL | bash`, `curl URL | sudo bash`

**Safe alternative**: download first, inspect, then execute:
```
curl -o /tmp/script.sh URL
cat /tmp/script.sh          # inspect
bash /tmp/script.sh         # execute if safe
```

## daemon_restart_verifier — restart the daemon before committing

Before making a `git commit` in the hooks daemon repository, this handler advises verifying that the daemon can restart successfully with the current code changes. This is advisory — it adds context but does not block the commit.

**Why**: Unit tests alone don't catch import errors. A handler that fails to import silently disables protection without any test-time error. Daemon restart is the definitive check.

**Run before committing** (in this repo only):
`$PYTHON -m claude_code_hooks_daemon.daemon.cli restart` then verify status shows RUNNING.

## dangerous_permissions — chmod 777 is blocked

`chmod 777` and other world-writable permission commands are blocked. Overly permissive file permissions are a security vulnerability.

**Blocked**: `chmod 777`, `chmod 666`, `chmod a+w`, `chmod o+w`

**Use least-privilege permissions instead**:
- Executable scripts: `chmod 755` (owner rwx, group/other rx)
- Regular files: `chmod 644` (owner rw, group/other r)
- Private files: `chmod 600` (owner rw only)

## destructive_git — blocked git commands

The following git commands are permanently blocked and will always be denied:

| Command | Reason |
|---------|--------|
| `git reset --hard` | Permanently destroys all uncommitted changes |
| `git clean -f` | Permanently deletes untracked files |
| `git checkout -- <file>` | Discards all local changes to that file |
| `git restore <file>` | Discards local changes (`--staged` is allowed) |
| `git stash drop` | Permanently destroys stashed changes |
| `git stash clear` | Permanently destroys all stashes |
| `git push --force` | Can overwrite remote history and destroy teammates' work |
| `git branch -D` | Force-deletes branch without checking if merged (lowercase `-d` is safe) |
| `git commit --amend` | Rewrites the previous commit — create a new commit instead |

If the user needs to run one of these, ask them to do it manually. Do not attempt to work around the block.

**Safe alternatives**: `git stash` (recoverable), `git diff` / `git status` (inspect first), `git commit` (save changes permanently first).

## error_hiding_blocker — error-suppression patterns are blocked

Writing code that silently swallows errors is blocked. All errors must be handled explicitly.

**Blocked patterns (examples)**:
- Python: bare `except` clauses with an empty body, catching and discarding all exceptions
- Shell: redirecting stderr to `/dev/null` to silence failures, `|| true` to suppress non-zero exit codes
- JavaScript/TypeScript: empty `catch` blocks that swallow exceptions
- Go: `_ = err` (discarding error return values without handling)

**Required action**: Handle errors explicitly — log them, return them to the caller, or propagate them. Silent error suppression masks bugs and makes debugging impossible.

## gh_issue_comments — always include --comments on gh issue view

`gh issue view` without `--comments` is blocked. Issue comments often contain critical context, clarifications, and updates not in the issue body.

**Blocked**: `gh issue view 123`, `gh issue view 123 --repo owner/repo`

**Allowed**: `gh issue view 123 --comments`, `gh issue view 123 --json title,body,comments`

If using `--json`, include `comments` in the field list instead of adding `--comments`.

## git_stash — git stash is advisory by default

`git stash`, `git stash push`, and `git stash save` trigger this handler. `git stash pop`, `git stash apply`, `git stash list`, and `git stash show` are always allowed.

**Default mode** (`warn`): stash is allowed but an advisory message explains risks.
**Deny mode** (`deny`): stash is blocked — use `git commit` to checkpoint work instead.

Configure via `handlers.pre_tool_use.git_stash.options.mode: deny` to enforce the stricter policy.

## lock_file_edit_blocker — never directly edit lock files

Direct `Write` or `Edit` to package manager lock files is blocked. Lock files are generated artifacts; manual edits create checksum mismatches and broken dependency graphs.

**Blocked files**: `composer.lock`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `Gemfile.lock`, `Cargo.lock`, `go.sum`, `Package.resolved`, `Pipfile.lock`, and others.

**Use package manager commands instead**:
- PHP: `composer install` / `composer require package`
- Node: `npm install` / `yarn add package`
- Ruby: `bundle install` / `bundle add gem`
- Rust: `cargo add crate`
- Go: `go get module`

## lsp_enforcement — use LSP tools for code symbol lookups

Using `Grep` or `Bash` (grep/rg) to find class definitions, function signatures, or symbol references is blocked or redirected to LSP tools, which are faster and semantically accurate.

**Prefer LSP tools for**:
- Finding where a class or function is defined → `goToDefinition`
- Finding all usages of a symbol → `findReferences`
- Getting type information or documentation → `hover`
- Listing all symbols in a file → `documentSymbol`
- Searching symbols across the project → `workspaceSymbol`

**Grep/Bash grep is still appropriate for**: text patterns in content, log searching, finding strings in config files.

Default mode (`block_once`): the first symbol-lookup grep in a session is denied with guidance; subsequent retries are allowed.

## markdown_organization — markdown files must go in allowed locations

Writing a new `.md` file to an unrecognised location is blocked. Markdown files must be placed in project-configured allowed paths.

**Common allowed locations**: `CLAUDE/`, `docs/`, `RELEASES/`, `CLAUDE/Plan/`, root-level `README.md`, or any path matching the `allowed_markdown_paths` config.

**Plan file redirection**: when `track_plans_in_project` is enabled, Claude Code planning mode writes are automatically redirected to the project's `CLAUDE/Plan/` directory. Plan folders must follow the `NNNN-description/` naming convention.

If you need a markdown file in a new location, add a pattern to `allowed_markdown_paths` in `.claude/hooks-daemon.yaml`.

## npm_command — use llm: prefixed npm commands

Direct `npm run` and `npx` commands are blocked or advised against. Projects with `llm:` prefixed scripts in `package.json` should use those instead.

**Why**: `llm:` commands are configured for LLM-friendly output (no spinners, no colour codes, structured results).

**Example**: Use `npm run llm:build` instead of `npm run build`.

If no `llm:` commands exist in `package.json`, the handler operates in advisory mode (warns but does not block).

### Pipe Blocker

Commands piped to `tail` or `head` are **blocked** — piping truncates output and causes information loss.

**Use a temp file instead:**

```bash
# WRONG — blocked:
pytest tests/ 2>&1 | tail -20

# RIGHT — redirect to temp file:
pytest tests/ > /tmp/pytest_out.txt 2>&1
# Then read selectively if needed
```

**Allowed** (whitelisted): `grep`, `rg`, `awk`, `sed`, `jq`, `ls`, `cat`, `git log`, `git tag`, `git branch`, and other cheap filtering commands.

**Add to whitelist** (if safe to pipe): set `extra_whitelist` in `.claude/hooks-daemon.yaml` under `pipe_blocker`.

## qa_suppression — QA suppression annotations are blocked

Writing QA suppression directives into source files is blocked across all supported languages. Fix the underlying code issue instead.

**Blocked annotation types (by language)**:
- Python: `noqa` directives, `type: ignore` annotations
- JavaScript/TypeScript: `eslint-disable` inline directives
- Go: `nolint` directives (golangci-lint)
- PHP: `phpstan-ignore`, `psalm-suppress` annotations
- Java/Kotlin: `@SuppressWarnings`, `@Suppress` annotations
- C#: `pragma warning disable` directives
- Rust: `allow(clippy::...)` attributes on type-level items

**Required action**: Fix the code so QA passes without suppression. If a suppression is genuinely necessary, ask the user to add it manually — this signals a conscious decision rather than a shortcut.

## security_antipattern — OWASP security antipatterns are blocked

Writing code that contains security antipatterns is blocked across all supported languages. Fix the code to use safe patterns instead.

**Blocked categories**:
- SQL injection: building queries via string concatenation (use parameterised queries)
- Command injection: passing unvalidated input to subprocess (use argument lists)
- Hardcoded credentials: API keys, passwords, tokens embedded in source code
- Weak cryptography: MD5 or SHA1 for password hashing (use bcrypt/argon2)
- Path traversal: unvalidated user input used in file paths

**Supported languages**: Python, JavaScript/TypeScript, Go, PHP, Ruby, Java, Kotlin, C#, Rust, Swift, Dart.

## sed_blocker — sed is forbidden for file modification

`sed` is blocked because Claude gets sed syntax wrong and a single error can silently destroy hundreds of files with no recovery possible.

**Blocked**:
- `sed -i` / `sed -e` (in-place file editing via Bash tool)
- `grep -rl X | xargs sed -i` (mass file modification)
- Shell scripts (`.sh`/`.bash`) written via Write tool that contain `sed`

**Allowed** (read-only, no file modification):
- `cat file | sed 's/x/y/' | grep z` (pipeline transforming stdout only)
- `sed` mentioned in commit messages, PR bodies, or `.md` documentation files

**Use instead**:
- `Edit` tool — safe, atomic, verifiable
- Parallel Haiku agents with `Edit` tool for bulk changes across many files:
  1. Identify all files to update
  2. Dispatch one Haiku agent per file
  3. Each agent uses the `Edit` tool (never `sed`)

## tdd_enforcement — test file must exist before source file

Creating a production source file is blocked until a corresponding test file exists.

**TDD workflow (required)**:
1. Create the **test file first** (e.g. `tests/unit/handlers/test_my_handler.py`)
2. Write failing tests — RED phase
3. Create the source file and implement until tests pass — GREEN phase
4. Refactor — REFACTOR phase

**Supported languages**: Python, Go, JavaScript/TypeScript, PHP, Rust, Java, C#, Kotlin, Ruby, Swift, Dart

**Test file locations checked** (any satisfies the block):
- Separate mirror: `tests/unit/{subdir}/test_{module}.py`
- Collocated: `{source_dir}/{module}.test.ts` (JS/TS projects)
- Test subdirectory: `{source_dir}/__tests__/{module}.test.ts`

**Allowed through without blocking**: vendor dirs, node_modules, build outputs, generated files, and file extensions not in the supported language list.

## validate_instruction_content — CLAUDE.md and README.md must have stable content

Writing ephemeral or session-specific content to `CLAUDE.md` or `README.md` is blocked. These files should contain only stable instructions, not implementation logs or session state.

**Blocked content types**:
- Timestamps and ISO dates
- Status emoji followed by completion words (e.g. checkmark + 'Done')
- Implementation log sentences ('created the file X', 'added the class Y')
- Test output counts ('3 tests passed')
- LLM summary section headings ('## Summary', '## Key Points')

Content inside markdown code blocks is exempt from validation.

## worktree_file_copy — do not copy files between worktrees and the main repo

`cp`, `mv`, and `rsync` operations that move files from a worktree directory (`untracked/worktrees/` or `.claude/worktrees/`) into the main repo (`src/`, `tests/`, `config/`) — or vice versa — are blocked.

Worktrees are isolated branches. Cross-copying corrupts that isolation and can silently overwrite in-progress work.

**Allowed**: operations within the same worktree branch. **To merge changes**: use `git merge` or `git cherry-pick` instead.

## system_paths — do not edit deployed system files directly

Writing or editing files under system paths (/etc/, /var/, /usr/, /opt/, /root/, /home/) is blocked.
These are deployed files managed by Ansible.

**Edit the project source instead**:
- `/etc/foo` → `files/etc/foo`
- `/var/local/foo` → `files/var/local/foo`
- `/usr/bin/foo` → `files/usr/bin/foo`

Then deploy via Ansible playbook.

## ansible_enforcement — no direct system management commands

Direct package management, service management, and system configuration commands are blocked. All system changes must go through Ansible playbooks.

**Blocked**: `dnf install`, `systemctl enable/start/stop`, `gsettings set`, `useradd`, `pip install` (system), `npm install -g`, `flatpak install`

**Allowed** (read-only queries): `dnf info/list/search`, `systemctl status`, `gsettings get`, `flatpak list`

**Use instead**: Create/update Ansible playbook in `playbooks/imports/` and deploy with `ansible-playbook`.

### Stop Explanation Required

Before stopping, **prefix your final message** with `STOPPING BECAUSE:` followed by a clear reason:

```
STOPPING BECAUSE: all tasks complete, QA passes, daemon restart verified.
```

**Why**: The stop hook enforces intentional stops. Stopping without an explanation triggers an auto-block that asks you to explain or continue.

**Alternatives**:
- `STOPPING BECAUSE: <reason>` — stops cleanly with explanation
- Continue working — no need to stop unless all work is genuinely complete

**Do NOT**:
- Stop mid-task without explanation
- Ask confirmation questions and then stop (the hook auto-continues those)
- Use `AUTO-CONTINUE` unless you intend to keep working indefinitely

**Before asking a question, evaluate it critically**:
- Tautological/rhetorical questions with obvious answers ("Should I continue?", "Would you like me to proceed?") — do NOT ask, just do it
- Errors with a clear next step ("The test failed, should I fix it?") — do NOT ask, just fix it
- Genuine choice questions where all options are valid ("Which of A, B, or C should we use?") — these deserve a response. Use `STOPPING BECAUSE: need user input` and ask your question

</hooksdaemon>

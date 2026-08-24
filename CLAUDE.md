# Claude Code Configuration

## Critical Rules

### CCY Container: Edit Only, Deploy on Host

**IF THE PROJECT PATH IS `/workspace/` — YOU ARE IN A CCY CONTAINER.**

- **NEVER run Ansible playbooks** in the container
- **Only edit and commit** — then tell the user to deploy on their HOST system
- CCY version bump required when modifying `files/var/local/claude-yolo/claude-yolo`

**Full container rules and the retired ctrl+z patch:** @CLAUDE/ContainerRules.md

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

**STRICT IAC — NO MANUAL FIX. DO NOT SUGGEST A MANUAL/QUICK FIX, EVEN AS A "do this now" shortcut alongside the playbook. ONLY IAC: fix it in the playbook and tell the user to run the playbook. Nothing else.**

**Full IaC workflow (edit → playbook → deploy → test):** @CLAUDE/InfrastructureAsCode.md

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

**Full QA reference (scripts, what they check, limitations):** @CLAUDE/QA.md

### Debug Commands: Always Non-Interactive

When providing diagnostic commands to users, always use `--no-pager`, `| cat`, or `| head`. Never open pagers or editors.

**Full rules and examples:** @CLAUDE/DebugCommands.md

### Interactive Scripts: Human-Friendly, Standardised

Scripts a human runs and is prompted by (extensionless executables and executed `*.bash` files under `files/home/.local/bin/`) MUST follow the standard UX rules: **strict validation, friendly recovery** — validate strictly, but on a recoverable input mistake show a clear error and **re-prompt in a bounded loop** instead of aborting. Reserve hard aborts for genuinely unrecoverable states. Sourced libraries and non-interactive/Ansible-invoked helpers are out of scope (those stay fully non-interactive and fail fast).

**Full rules, scope, and canonical patterns:** @CLAUDE/InteractiveScripts.md

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

**Full rule, decision procedure, patterns, and review checklist:** @CLAUDE/StderrHygiene.md

### Container Engines: Podman First

**Use Podman wherever possible — it is the better system.** Reach for Docker only when a tool genuinely needs it for compatibility or legacy reasons, and understand that Docker is significantly less secure than Podman.

- **Podman** (rootless) — default for everything. CCY, devtools, ad-hoc work.
- **Docker** (rootful) — compatibility mode only, e.g. DDEV. `docker` group = root-equivalent.
- **LXC** (rootful) — VM-like full-system containers with systemd inside.

New playbooks needing a container engine must use the `container_engine` variable (default `podman`), not hardcode an engine.

**Full role split, coexistence, and FAQ:** @CLAUDE/ContainerEngines.md

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

## CLAUDE/ Topic Files Index

| File                            | Content                                                                     |
| ------------------------------- | --------------------------------------------------------------------------- |
| @CLAUDE/ContainerRules.md       | CCY container detection, version bump, retired ctrl+z patch                 |
| @CLAUDE/ContainerEngines.md     | Podman/Docker/LXC role split; when to use which; security trade-offs        |
| @CLAUDE/InfrastructureAsCode.md | Ansible-only workflow, prohibited manual actions                            |
| @CLAUDE/AnsibleStyle.md         | Playbook structure, markers, packages, services, variables                  |
| @CLAUDE/SecurityRules.md        | Public repo warning, vault management, pre-commit checks                    |
| @CLAUDE/ExampleValues.md        | Reserved example IPs/emails/hostnames the secret scanner whitelists         |
| @CLAUDE/QA.md                   | QA scripts reference, what to run when                                      |
| @CLAUDE/DebugCommands.md        | Non-interactive command rules for user diagnostics                          |
| @CLAUDE/InteractiveScripts.md   | Human-friendly interactive script rules (validate strictly, retry on loop)  |
| @CLAUDE/StderrHygiene.md        | Diagnostics → stderr; stdout is the captured payload (bash + Python)        |
| @CLAUDE/GnomeShell.md           | GNOME Shell extension development (Wayland, ESLint, APIs)                   |
| @CLAUDE/PlanWorkflow.md         | Planning workflow and plan document structure                               |
| @CLAUDE/PlanTriage.md           | How to establish facts: plan-local triage scripts (probes go IN the script) |
| @CLAUDE/AgentNotes.md           | Working practices and project gotchas (feedback + project knowledge)        |

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

<!-- handler: require-absolute-paths -->

## absolute_path — always use absolute paths

The `Read`, `Write`, and `Edit` tools require absolute paths. Relative paths are blocked.

- **Blocked**: `src/main.py`, `./config.yaml`, `../other/file.txt`
- **Correct**: each of those prefixed with the project's absolute root

Prepend the absolute path of the project root — the working directory Claude Code reports for this session — to any relative path before calling these tools. The block message names the exact path to use, so there is nothing to guess.

<!-- handler: agent-isolation-advisor -->

## agent_isolation_advisor — isolate concurrent agents

When more than one agent thread is live in this checkout, spawning another Agent without isolation is flagged (advisory, never blocked).

Agents in one working tree share a single `.git/index`, so a peer's bare `git commit` can silently absorb another agent's staged work.

**Prefer**: `isolation: "worktree"` on the Agent tool, then `git merge` or `git cherry-pick` to bring work back.

**Keep the shared tree** for agents that need the real project root — daemon restart verification and client-mode testing do not work in a worktree.

<!-- handler: block-ancestry-severing-merge -->

## ancestry_preserving_merge — ancestry-severing merges are blocked by default

`git merge --squash`, `gh pr merge --squash` and `gh pr merge --rebase` are blocked. A squash merge collapses every commit into one new commit on the target; a rebase merge replays them with new shas. Either way this branch's commits never become **ancestors** of the target, so `git branch -d` (the safe, battle-tested delete) refuses the branch FOREVER, even though its content is fully upstream. This is about the ancestry consequence, not a style opinion on squashing or rebasing.

**Always allowed**: `git merge`, `git merge --no-ff`, `gh pr merge --merge`, and a LOCAL `git rebase <branch>` on your own feature branch before merging -- that preserves ancestry once merged with `--no-ff`. It is the REBASE MERGE *integration button* that severs ancestry, not local rebasing.

**Use instead**:

```
git merge --no-ff <branch>      # merge commit, preserves ancestry
gh pr merge --merge <number>    # GitHub equivalent of --no-ff
```

**Escape hatch** (when your platform genuinely mandates squash-only or rebase-only merging):

```
MUST_SQUASH_BECAUSE="explain why"; git merge --squash <branch>
```

**Not covered**: a squash or rebase merge performed through the GitHub web UI. The daemon sees tool calls, not browser clicks, so this handler has no visibility into a merge button pressed in a browser.

Configure via `handlers.pre_tool_use.ancestry_preserving_merge.options.mode: warn` for advisory-only mode.

<!-- handler: block-ask-user-question -->

## ask_user_question_blocker — questions need `ASKING BECAUSE:` justification

AskUserQuestion calls are only allowed when every `question` string begins with `ASKING BECAUSE:` (case-sensitive, leading whitespace OK). The convention mirrors the Stop handler's `STOPPING BECAUSE:` pattern — explicit declared intent gates the privilege of pausing the session.

**Before asking, evaluate critically**:

- Tautological/rhetorical questions with one obvious answer ("Should I continue?", "Would you like me to proceed?") — do NOT ask. State the question and your assumed-correct answer in plain output text and proceed. The user is watching and will interrupt if the assumption is wrong.
- Questions whose options reduce to **good vs. bad** are tautological — the answer is always the good option. Examples: best practice vs. bodge, increasing vs. decreasing code quality, delivering the requirement vs. not delivering it, fixing the failing test vs. leaving it broken, following project conventions vs. inventing your own. Do NOT ask; pick the good option and proceed.
- Errors with a clear recovery path ("Should I fix the failing test?") — do NOT ask. Fix it.
- Genuine choice questions where you cannot resolve the answer from context — these are the legitimate use case. Prefix every question text with `ASKING BECAUSE: <one-line reason you cannot decide>` so the daemon allows the call through.

**Audit log pattern** (preferred for tautological questions):

```
I would normally ask: <question>.
Assumed answer: <your assumption>.
Proceeding on that basis; the user will interrupt if wrong.
```

**Escape hatch** (genuine ambiguity): prefix every question text with `ASKING BECAUSE: <reason>`. Mixing prefixed and non-prefixed questions in one call still triggers a block — prefix all or none.

<!-- handler: block-comment-changelog -->

## comment_changelog — no changelog narrative in code comments

Writing HISTORICAL NARRATIVE into a code comment is blocked. A comment describes CURRENT STATE; changelog narrative belongs in git (the commit message), the project's changelog file, or a plan's `JOURNAL/` day-file.

**Blocked (high-precision) signals**, either of which denies the write:

- `Prior <version>:` / `Previously <version>:` phrasing
- a dated entry (`2026-08-12: ...`)

Both were measured with ZERO false positives across this project's own ~1,080 source/test files (Plan 00208's whole-repo self-scan) — every real hit was either the field-report shape itself or this handler's own test fixtures.

**NOT blocked — advisory only**: a version-transition arrow (`1.2 -> 1.3`), a changelog verb naming a version (`Removed in v2.1.224`), two or more distinct versioned/dated entries in one comment (configurable via `max_history_entries`, default 1), `Fixed:`/`Added:`/`Changed:` bullet runs, retrospective phrasing (`used to`, `no longer`, `we switched from`). These four started as blocking signals but the same self-scan found each firing on legitimate code — version-processing utilities (upgrade compatibility checkers) legitimately cite multiple versions in their own docstrings, and "removed in vX.Y" describing an EXTERNAL tool's own deprecation is rationale, not a changelog entry about this project.

**History as RATIONALE is legitimate and is NOT flagged.** A comment may recount the past when the past is the reason the code looks the way it is now, and re-litigating it would reintroduce a fixed bug — e.g. `# Plan 00047: do NOT re-add DISABLE_MOUSE, see...`. The separating test: an entry keyed by a RELEASE NUMBER is a changelog; an entry keyed by a FAILURE MODE (a plan number, a bug description) is a rationale.

**No escape hatch** — unlike `comment_size`, this handler has no `MUST_..._BECAUSE` override: changelog content should be MOVED to git/a changelog file/a plan JOURNAL/, never exempted in place.

**Scope**: only comment spans are scanned (not code), via the same Strategy Pattern language registry as `qa_suppression`. `.md` files are skipped entirely — markdown prose is not a comment. Only the ADDED text is checked on `Edit` (`new_string`) — removing changelog content is never blocked.

**Excluded paths**: vendor/build/fixture dirs are skipped by default. Exempt more paths via `handlers.pre_tool_use.comment_changelog.options.exclude_paths` or the project-wide `daemon.exclude_paths`.

<!-- handler: block-comment-size -->

## comment_size — over-long comments are capped, tiered like plan-doc-size

A `Write`/`Edit` whose content contains an over-limit comment is blocked or advised, using the SAME grow/shrink/same-size tiering as `plan-doc-size`: only an edit that GROWS an already-over-limit comment can be denied.

**Two independent limits (either trips it)**:

- a single comment line longer than `max_comment_line_chars` (default 400)
- a contiguous comment block longer than `max_comment_block_lines` (default 40)

**Tiering**:

- **Shrinking is silent** — always allowed, no context, so an over-commented legacy file stays editable and can be refactored down.
- **Same-size only advises** — never blocks, so a legitimately-unchanged oversized comment does not trap the file.
- **Growing an already-over-limit comment is BLOCKED** unless the escape hatch is declared or `mode: warn` is configured.

**Escape hatch** (in-content, matching the daemon's `MUST_..._BECAUSE` convention):

```
# MUST_EXCEED_COMMENT_SIZE_BECAUSE: verbatim upstream licence text, must not be reflowed
```

**Docstrings and JSDoc are API documentation, not comments** — exempt from this handler entirely (still subject to `comment_changelog`).

**Excluded paths**: vendor/build/fixture dirs are skipped by default. Exempt more paths via `handlers.pre_tool_use.comment_size.options.exclude_paths` or the project-wide `daemon.exclude_paths`.

<!-- handler: block-curl-pipe-shell -->

## curl_pipe_shell — never pipe curl/wget to bash/sh

Piping network content directly to a shell is blocked. It executes untrusted remote code without any inspection.

**Blocked**: `curl URL | bash`, `curl URL | sh`, `wget URL | bash`, `curl URL | sudo bash`

**Safe alternative**: download first, inspect, then execute:

```
curl -o /tmp/script.sh URL
cat /tmp/script.sh          # inspect
bash /tmp/script.sh         # execute if safe
```

<!-- handler: daemon-location-guard -->

## daemon_location_guard — do not cd into .claude/hooks-daemon/

Bash commands that change directory into `.claude/hooks-daemon/` (or `cd` into a daemon-internal subdirectory and then run something) are blocked. The daemon is an upstream dependency that must remain untouched in client repos.

**Run daemon CLI from the project root instead** — it always works regardless of cwd:

```
.claude/hooks-daemon/bin/hooks-daemon status
.claude/hooks-daemon/bin/hooks-daemon restart
.claude/hooks-daemon/bin/hooks-daemon logs
```

If you need to inspect daemon source for debugging, use `Read` from the project root with the absolute path — never `cd` in. Do NOT edit anything inside `.claude/hooks-daemon/`; changes will be overwritten on the next upgrade.

<!-- handler: verify-daemon-restart -->

## daemon_restart_verifier — restart the daemon before committing

Before making a `git commit` in the hooks daemon repository, this handler advises verifying that the daemon can restart successfully with the current code changes. This is advisory — it adds context but does not block the commit.

**Why**: Unit tests alone don't catch import errors. A handler that fails to import silently disables protection without any test-time error. Daemon restart is the definitive check.

**Run before committing** (in this repo only):
`.claude/hooks-daemon/bin/hooks-daemon restart` then verify status shows RUNNING.

<!-- handler: block-dangerous-permissions -->

## dangerous_permissions — chmod 777 is blocked

`chmod 777` and other world-writable permission commands are blocked. Overly permissive file permissions are a security vulnerability.

**Blocked**: `chmod 777`, `chmod 666`, `chmod a+w`, `chmod o+w`

**Use least-privilege permissions instead**:

- Executable scripts: `chmod 755` (owner rwx, group/other rx)
- Regular files: `chmod 644` (owner rw, group/other r)
- Private files: `chmod 600` (owner rw only)

<!-- handler: prevent-destructive-git -->

## destructive_git — blocked git commands

The following git commands are permanently blocked and will always be denied:

| Command                  | Reason                                                                   |
| ------------------------ | ------------------------------------------------------------------------ |
| `git reset --hard`       | Permanently destroys all uncommitted changes                             |
| `git clean -f`           | Permanently deletes untracked files                                      |
| `git checkout -- <file>` | Discards all local changes to that file                                  |
| `git restore <file>`     | Discards local changes (`--staged` is allowed)                           |
| `git stash drop`         | Permanently destroys stashed changes                                     |
| `git stash clear`        | Permanently destroys all stashes                                         |
| `git push --force`       | Can overwrite remote history and destroy teammates' work                 |
| `git branch -D`          | Force-deletes branch without checking if merged (lowercase `-d` is safe) |
| `git commit --amend`     | Rewrites the previous commit — create a new commit instead               |

If the user needs to run one of these, ask them to do it manually. Do not attempt to work around the block.

**To delete a branch, ALWAYS try `git branch -d` first.** It is allowed, it is battle-tested, and it refuses unless the branch is genuinely merged. Reach for anything else only once it has actually refused:

```
git branch -d <name>                          # ALWAYS TRY THIS FIRST
hooks-daemon delete-branch --dry-run <name>    # only if -d refused
hooks-daemon delete-branch <name>              # deletes only if provably safe
```

`git branch -d` refuses a branch whose commits are not ancestors of the target, which after a history rewrite or a squash merge means EVERY branch — the content is upstream but the ancestry is severed. That specific gap is what `delete-branch` fills; it is not a general replacement. It refuses by default and deletes only what it can prove is recoverable: merged, or every commit already upstream by patch-id, or every file version byte-identical to a blob still reachable from `main`. For a merged branch it delegates to `git branch -d` anyway, so git re-checks the work independently. A recovery bundle is written first unless you pass `--no-bundle`. If it refuses, it names the files whose CONTENT exists nowhere else.

**Not every refusal is about content.** If another agent advances a branch after it was proved safe, `delete-branch` refuses THAT branch and names both shas — the proof described the old commit, and the recovery bundle was written before the new one exists, so it does not cover it. Re-run to reclassify against the current tip; never force past it.

**A second, less obvious case where `-d` refuses**: it measures a branch against its OWN upstream when it has one, so a branch fully merged into `main` is still refused while it is ahead of `origin/<name>` — git says "not yet merged to `refs/remotes/origin/<name>`, even though it is merged to HEAD". Pushing the branch, or `delete-branch`, both resolve it; the latter reports this as the `merged-unpushed` tier and deletes it, because every one of those commits is already in `main`.

**A third case, and the most ordinary of the three**: with NO upstream, git measures the branch against the checked-out `HEAD` instead — so a branch fully merged into `main` is refused whenever you are standing on some other branch, which is the normal way you notice a branch needs tidying. Git says "not fully merged" and suggests the force delete, which is blocked. `delete-branch` reports this as the `merged-not-in-head` tier and deletes it; checking out `main` first also resolves it.

**Abandoning unmerged work is human-gated and you cannot complete it.** When no proof holds, the branch holds the only copy of real work, so `--allow-unproven --reason` is not enough: the command also requires a human to type a confirmation at an interactive terminal, which your non-interactive shell does not have. Those flags declare intent; consent is separate and cannot be self-granted. Report the named files to the user and ask them to run the command themselves — do not hunt for a way around it.

**Safe alternatives**: `git stash` (recoverable), `git diff` / `git status` (inspect first), `git commit` (save changes permanently first).

<!-- handler: error-hiding-blocker -->

## error_hiding_blocker — error-suppression patterns are blocked

Writing code that silently swallows errors is blocked. All errors must be handled explicitly.

**Blocked patterns (examples)**:

- Python: bare `except` clauses with an empty body, catching and discarding all exceptions
- Shell: redirecting stderr to `/dev/null` to silence failures, `|| true` to suppress non-zero exit codes
- JavaScript/TypeScript: empty `catch` blocks that swallow exceptions
- Go: `_ = err` (discarding error return values without handling)

**Required action**: Handle errors explicitly — log them, return them to the caller, or propagate them. Silent error suppression masks bugs and makes debugging impossible.

**Excluded paths**: vendor/, node_modules/, and test-fixture dirs (tests/fixtures/, tests/assets/, __fixtures__/) are skipped by default. Exempt more paths with glob patterns via `handlers.pre_tool_use.error_hiding_blocker.options.exclude_paths` or the project-wide `daemon.exclude_paths` — use these for fixtures of deliberately-broken code instead of disabling the handler.

<!-- handler: require-gh-issue-comments -->

## gh_issue_comments — always include --comments on gh issue view

`gh issue view` without `--comments` is blocked. Issue comments often contain critical context, clarifications, and updates not in the issue body.

**Blocked**: `gh issue view 123`, `gh issue view 123 --repo owner/repo`

**Allowed**: `gh issue view 123 --comments`, `gh issue view 123 --json title,body,comments`

If using `--json`, include `comments` in the field list instead of adding `--comments`.

<!-- handler: require-gh-pr-comments -->

## gh_pr_comments — always include --comments on gh pr view

`gh pr view` without `--comments` is blocked. PR comments often contain review feedback, reviewer requests, and decisions not in the PR body.

**Blocked**: `gh pr view 123`, `gh pr view 123 --repo owner/repo`

**Allowed**: `gh pr view 123 --comments`, `gh pr view 123 --json title,body,comments`

If using `--json`, include `comments` in the field list instead of adding `--comments`.

<!-- handler: block-git-message-backtick -->

## git_message_backtick — backticks in a double-quoted git message

Bash runs command substitution inside DOUBLE quotes, so backticks in `git commit -m "..."` (and `git tag -m "..."`) are EXECUTED and the span is replaced by the command's stdout. The commit still succeeds, so the text is lost silently — this is not hypothetical, a commit in this repo lost a phrase exactly this way.

**Blocked**: an unescaped backtick inside a double-quoted `-m`/`--message` value on `git commit` or `git tag`.

**Always allowed** — none of these substitute:

- Single quotes: `git commit -m 'text with `backticks` stays literal'`
- A message file: `git commit -F <file>`
- A backslash-escaped `` \` `` inside double quotes

**Prefer single quotes or `-F` for any message containing markdown.** If a message needs BOTH backticks and interpolation, put it in a file and use `-F` — do not try to escape your way through it.

Note this handler covers the CORRUPTION case only. A *dangerous* command inside backticks is already denied by the full-command-string matching in `destructive_git` and friends, which run at a lower priority and give the better reason.

<!-- handler: block-git-stash -->

## git_stash — git stash is blocked by default

`git stash`, `git stash push`, and `git stash save` are blocked. `git stash pop`, `git stash apply`, `git stash list`, and `git stash show` are always allowed.

**Why**: stashes get forgotten, lost, and block `git pull`. Use `git commit -m 'WIP: ...'` instead — WIP commits are acceptable.

**Escape hatch** (when commit truly won't work):

```
MUST_STASH_BECAUSE="explain why"; git stash
```

Configure via `handlers.pre_tool_use.git_stash.options.mode: warn` for advisory-only mode.

<!-- handler: lock-file-edit-blocker -->

## lock_file_edit_blocker — never directly edit lock files

Direct `Write` or `Edit` to package manager lock files is blocked. Lock files are generated artifacts; manual edits create checksum mismatches and broken dependency graphs.

**Blocked files**: `composer.lock`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `Gemfile.lock`, `Cargo.lock`, `go.sum`, `Package.resolved`, `Pipfile.lock`, and others.

**Use package manager commands instead**:

- PHP: `composer install` / `composer require package`
- Node: `npm install` / `yarn add package`
- Ruby: `bundle install` / `bundle add gem`
- Rust: `cargo add crate`
- Go: `go get module`

<!-- handler: enforce-lsp-usage -->

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

<!-- handler: enforce-markdown-organization -->

## markdown_organization — tracked-docs policy (untracked Claude memory BLOCKED)

This project sets `allow_untracked_claude_memory: false`. Writing to Claude
auto-memory files (`~/.claude/projects/*/memory/*.md`) is **blocked** — via the
Write/Edit tools AND via bash redirect/`tee` side-doors. **Reading memory is
still allowed** so existing memory can be migrated out.

**Put durable knowledge in TRACKED project docs (progressive disclosure):**

- Always-relevant facts → `CLAUDE.md` (keep lean; resident every session)
- Path-specific guidance → `.claude/rules/*.md` with `paths:` glob frontmatter (loads on demand only when matching files are touched)
- Intent-triggered procedures → a thin skill under `.claude/skills/` pointing at a single-source-of-truth doc body
- Human-facing reference → `docs/`
- Link docs with plain markdown links (zero token cost until followed); **avoid `@`-imports** (they re-inline eagerly rather than defer)

Keep ONE source of truth per fact and link to it. Normal markdown-location rules (below) still apply to every other `.md` file.

**Allowed locations**: `CLAUDE/`, `docs/`, `RELEASES/`, `CLAUDE/Plan/`, root-level `README.md`, `.claude/rules/`, or any `extra_allowed_markdown_paths` pattern.

<!-- handler: enforce-npm-commands -->

## npm_command — use llm: prefixed npm commands

Direct `npm run` and `npx` commands are blocked or advised against. Projects with `llm:` prefixed scripts in `package.json` should use those instead.

**Why**: `llm:` commands are configured for LLM-friendly output (no spinners, no colour codes, structured results).

**Example**: Use `npm run llm:build` instead of `npm run build`.

If no `llm:` commands exist in `package.json`, the handler operates in advisory mode (warns but does not block).

<!-- handler: block-pip-break-system -->

## pip_break_system — --break-system-packages is blocked

`pip install --break-system-packages` (and the `pip3` / `python -m pip` / `python3 -m pip` variants) is blocked. The flag bypasses PEP 668 system-package protection and corrupts the system Python environment in containers and on modern Linux distros.

**Use a virtualenv or `--user` install instead**:

```
python3 -m venv /tmp/venv && /tmp/venv/bin/pip install <package>
# or
pip install --user <package>
```

If a tool's installer insists on `--break-system-packages` (some quick-start scripts do), download it first, inspect, and run it inside a venv — do not shortcut by adding the flag.

<!-- handler: pipe-blocker -->

### Pipe Blocker

Commands piped to `tail` or `head` are **blocked** — piping truncates output and causes information loss.

**Do NOT do the theatre** of capturing output to a file and then echoing the WHOLE file to stdout — that defeats the point and just bloats tokens.

**Preferred — `echd-capture`**: capture the FULL output, see only a preview. When the block fires it prints the exact invocation to use — an ABSOLUTE path to the deployed helper, not a bare name — so copy the path from the block message (the helper is not guaranteed to be on `PATH`). If no helper path can be resolved, the block recommends the temp-file redirect below instead.

```bash
# WRONG — blocked (and truncates):
pytest tests/ 2>&1 | tail -20

# RIGHT — full capture, bounded preview + path to the rest. Use the ABSOLUTE
# echd-capture path from the block message (shown here as /…/scripts/echd-capture):
set -o pipefail
pytest tests/ 2>&1 | /…/scripts/echd-capture 20
# prints the last 20 lines + '(full output: /…/command-output-….txt)'.
# Use --head N for the first N lines. pipefail keeps pytest's exit code visible.
```

**Always-works alternative** (no helper, no pipe): `pytest tests/ > /tmp/out.txt 2>&1` then read the file selectively.

**Allowed** (whitelisted): `grep`, `rg`, `awk`, `sed`, `jq`, `ls`, `cat`, `git log`, `git tag`, `git branch`, and other cheap filtering commands.

**EVERY pipe in the command is judged, on its own producer.** A cheap pipe does not buy cover for an expensive one, so `git log | head -2 && pytest | head -1` is blocked on the `pytest` half. The `tail -f` / `head -c` exemptions are also per-pipe — an unrelated `&& tail -f x` elsewhere in the command exempts nothing.

**A pipe inside `$( )` or backticks belongs to the command INSIDE it.** `echo $(pytest tests/ | head -1)` is blocked on `pytest`, not allowed because `echo` is cheap — the output being thrown away is pytest's. Nesting and `<( )` behave the same. Whitelisted inner producers are still fine: `echo $(git log --format=%H | head -1)` is allowed. A `$( )` or backtick inside SINGLE quotes is literal text, so it is not treated as a substitution. That exemption is about SUBSTITUTION only — an ordinary single-quoted ARGUMENT containing `| head` is still scanned and still blocked, because the shell can hand that string to something that runs it. The exemptions that do cover a whole value are a git `-m`/`-F` message and a quoted-delimiter heredoc.

**Only PIPES are restricted — reading a file directly is not.** `tail -n 40 <file>`, `head -n 40 <file>` and `grep pattern <file>` take the path as an ARGUMENT, so no pipe exists and this handler never sees them. That is the supported way to sample a large append-only file such as a plan's `JOURNAL/` day-file — which you should tail or grep rather than read whole.

**Add to whitelist** (if safe to pipe): set `extra_whitelist` in `.claude/hooks-daemon.yaml` under `pipe_blocker`.

**A git message VALUE is exempt only while the shell cannot run it.** Prose in `git commit -m`/`git tag -m` is not scanned, so a literal `| tail` inside a message never counts as a pipe — but that exemption ends at a command substitution. Bash expands `$( )` and backticks inside DOUBLE quotes, so `git commit -m "$(pytest | tail -1)"` genuinely runs pytest and truncates it, and is blocked on the `pytest`. Single quotes substitute nothing and are exempt unconditionally, as is the `"$(cat <<'EOF' ... EOF)"` idiom, whose QUOTED delimiter makes the body literal. The exemption is also scoped to commands that actually take a message: `python -m pytest ... | tail` names `pytest` as its producer, because `-m` there means module.

**A heredoc whose DELIMITER IS QUOTED is never scanned at all.** `cat >> notes.md <<'EOF' ... EOF` writes its body out verbatim — the shell expands nothing in it — so a `| tail` sitting in that body was never going to run, and blocking it would be wrong. Quote the delimiter (`<<'EOF'`) whenever the body is prose, a code snippet, or anything else you are writing rather than executing.

**An UNQUOTED `<<EOF` IS still scanned, and that boundary is deliberate.** Bash performs command substitution inside an unquoted heredoc, so `cat <<EOF` with `$(pytest | tail -1)` in the body really does run pytest and truncate it. A bare `| tail` in unquoted prose can therefore still false-trigger: when the matched text reads as ENGLISH rather than as a command — it starts with a function word like "the", or such words make up a large share of it — the block reason is short and does NOT echo your text back or suggest a fabricated `extra_whitelist` entry. Just quote the delimiter and retry, or write prose content with the `Write` tool instead of a heredoc.

**Length is NOT part of that judgement.** A long command is still a command: a 100-character invocation with a worktree branch name and absolute paths gets the normal block reason, naming what matched and how to whitelist it. If you ever see the short prose reason for text that really was a command, that is a bug worth reporting — retrying it unchanged will block again.

<!-- handler: plan-number-helper -->

## plan_number_helper — use `mkplan.bash` to create a plan

**Before creating one, check nothing already covers it.** Dispatch the `hooks-daemon-plan-dedupe-scout` agent with a sentence describing the intended work; it reads the still-live plans and names any that already cover it, so you can merge or supersede instead of filing alongside. This is a SUGGESTION — it never blocks, it is a judgement call rather than a rule, and it can be wrong. It is worth the few seconds because the alternative failure is expensive and silent: a duplicate plan is usually discovered only after an agent has spent a lot of context re-deriving conclusions that already existed on disk.

**To create a new plan, run the deployed scaffolding script:**

```
CLAUDE/Plan/mkplan.bash "descriptive-kebab-name"
```

**Hand-creating the folder is BLOCKED.** `mkdir <plan-dir>/NNNNN-name` is denied when the scaffolder is deployed: `mkdir` claims a number the moment the folder appears, but nothing records the claim until PLAN.md is written, so a concurrent agent reading the counter in between gets the SAME number and the collision surfaces only at the commit gate. This is narrow — `mkdir <plan-dir>/Completed`, a `JOURNAL/` inside a plan that already exists, and a `-p` re-create of an existing folder are all allowed, as is any path outside this workspace.

(Use the project's configured plan directory if it is not `CLAUDE/Plan/`.) The script takes a lock, reads the same authoritative git counter (`hooksdaemon.latestPlanNumber`), assigns the next number atomically, creates the `NNNNN-name/` folder, scaffolds `PLAN.md`, and advances the counter — so concurrent runs can never collide on a number. It prints the new folder path on stdout. You still add the README index row yourself (the script reminds you).

**If you only need the *number* (not a folder)**, read the counter and add 1 — this is the fallback, not the primary path:

```
git config --local hooksdaemon.latestPlanNumber
```

Add 1 to that value (zero-pad to 5 digits, e.g. counter `117` → next plan `00118`). The git counter is the source of truth; the daemon keeps it correct across branches.

**Do NOT** scan `CLAUDE/Plan/` with `ls`/`find`/glob pipelines to discover the next number. Folder scans miss plans in `Completed/` and other subdirectories, and disagree across branches. The folder scan is only used to bootstrap the counter when the git key is unset (which `mkplan.bash` and the daemon both handle).

<!-- handler: plan-qa-commit-gate -->

## plan_qa_commit_gate — cross-file plan checks at git commit

Every `git commit` is checked against the STAGED tree's plan QA
invariants. In `commit_gate_mode: warn` (the rollout default)
violations appear as advisory context — read them and amend the
commit content BEFORE committing; in `block` mode they deny the
commit with a TODO list of what the commit must also contain.

**The invariants**:

- creating a plan folder ⇒ the SAME commit stages its README
  index row (`index-at-birth`) and the number must come from the
  git counter / mkplan.bash (`counter-sanity`, `no-new-collisions`)
- flipping a plan to Complete/Cancelled/Superseded ⇒ the SAME
  commit contains the `git mv` into the archive dir AND the README
  row + statistics update (`terminal-state-atomic`)
- every folder has a README row in the section matching its
  location, and every row's link resolves
  (`row-folder-bijection`, `stats-recount`)
- every line of the README index stays under 500 characters
  (`index-row-length`): a row is a POINTER — a link, a status and
  one clause — because the rationale belongs in the linked PLAN.md
- a commit claiming `Plan NNNNN` that stages src/tests/config
  changes should also update that plan's PLAN.md
  (`same-commit-plan-doc`); reference plans as `Plan NNNNN:`
  (`plan-ref-format`)
- (advise-only, Plan 00163) a commit that changes a plan's PLAN.md
  tasks should stage a `JOURNAL/` entry recording what changed
  (`journal-entry-with-progress`); a terminal-status flip should
  stage a closing journal entry when
  `plan_workflow.qa.journal.enforce_on_completion` is on
  (`journal-completion-entry`)
- (advise-only, Plan 00190) a commit whose PLAN.md loses 2,000+
  bytes while staging NO journal entry is flagged
  (`plan-shrink-without-journal`): that shape usually means
  narrative was DELETED rather than relocated into `JOURNAL/`.
  If the content was genuinely obsolete this is fine as it stands
  — git keeps the history; the check exists so you notice which
  of the two you just did

Check the staged tree any time without committing:
`.claude/hooks-daemon/bin/hooks-daemon plan-qa --check-staged`.
Commits inside nested/vendor repos or foreign worktrees are exempt.

<!-- handler: plan-qa-edit -->

## plan_qa_edit — PLAN.md writes are linted in real time

Every Write/Edit of a `PLAN.md` under the plan directory is checked
against the plan QA edit-stage rules on the content the file WOULD
have. Block-level violations (in `edit_mode: block`) deny the tool
call with the exact remediation; fix the content and retry.

The plan-index `README.md` is linted too, against ONE rule:
`index-row-length`. Keep every line under 500 characters — an index
row is a POINTER (a link, a status and one clause), not a summary,
because the rationale belongs in the linked `PLAN.md` and a second
copy in the index is the one that goes stale. Only an edit that
makes the index WORSE is blocked (more over-long lines, or a longer
worst offender), so an index that already has one stays editable —
including by the edit that fixes it. No other plan-document rule
applies to the index: it has no `**Status**:` line and needs none.

**Rules that block new plan material**:

- a parseable `**Status**:` line must exist (`status-line-present`)
- the status token must be one of: Not Started, In Progress,
  Complete, Blocked, Cancelled, Superseded, Dormant
  (`status-enum-and-date`)
- the header must not contradict the body — do not leave
  `Not Started`/`In Progress` above an all-ticked task list or
  "ALL DONE" prose; flip the status instead
  (`header-body-coherence`)
- use the template task grammar `- [ ] ⬜ **Task N.N**:` — not
  ad-hoc markers like `[✓]`/`[⏳]` (`task-grammar`)
- a `PLAN.md` must stay under the size tiers (`plan-doc-size`):
  advisory above 18,000 bytes / 350 lines, escalated warning above
  25,000 / 500, and edits BLOCKED above 35,000 / 900. Three remedies, and NONE is deletion: (1) EXTRACT durable detail — research output, findings, decisions and their reasoning, drafts, evidence tables — into a named supporting document in this plan folder (e.g. `RESEARCH-*.md`, `DECISIONS.md`) and link to it from the task; (2) RELOCATE dated narrative — progress notes, incident write-ups, hand-off prose — into this plan's JOURNAL/ day-file, which is append-only and unbounded by design; or (3) SPLIT the plan if the task tree itself is the bulk, since an over-scoped plan is not fixed by better journalling. Keep PLAN.md lean, current and correct — history belongs in git and in JOURNAL/. Only an edit that
  GROWS the file can be blocked (shrinking is silent, same-size
  only advises), so an oversized plan can always be updated and
  refactored down; declare a genuine exception in the file with
  `<!-- MUST_EXCEED_PLAN_SIZE_BECAUSE: <reason> -->`. Journals,
  supporting docs and the plan-index README are exempt at any
  size — if the advisory notes the folder has none, that is a
  hint the bulk may want a named supporting document, not proof.

**Advisory rules**: missing Created/Owner/Priority headers on new
plans; a terminal status set while the folder is still in the plan
root (the same commit must `git mv` it to the archive dir and
update the README row); edits to archived plans; backticked
`src/...` paths that no longer exist.

**Journal day-files** (`JOURNAL/NNNNN-Journal-YY-MM-DD.md`) are also
linted: the name must match the grammar and the enclosing plan
number (`journal-dayfile-naming`, ADVISE), and edits must APPEND —
never rewrite or remove earlier entries (`journal-append-only`,
ADVISE). Corrections are new dated entries at the bottom, not edits
to old ones.

**A Write/Edit to a journal day-file dated anything other than
TODAY is BLOCKED by default** (`journal-dayfile-is-today`) — this
includes yesterday's date. A session that spans midnight must start
TODAY's day-file, not keep appending to yesterday's; the block
message names the exact today-dated filename to write instead.
Controlled independently of the other journal checks via
`plan_workflow.qa.journal.today_only_mode` (advise | block | off;
default block).

A journal is **unbounded by design** — its length is never a problem
and it must not be tidied or trimmed. It is safe to grow forever
precisely because it is never read whole: grep it, `tail -n N` the
newest day-file directly, or send a sub-agent. `PLAN.md` is the
opposite — read in full every session, so keep it lean and curated,
with history in git rather than in the file body.

Grandfathered plans in `plan_workflow.qa.legacy_plan_allowlist`
only ever advise. Lint any file on demand:
`.claude/hooks-daemon/bin/hooks-daemon plan-qa --lint <file>`.

<!-- handler: block-plan-time-estimates -->

## plan_time_estimates — plans describe WHAT, not WHEN

Writing time estimates into a plan document is blocked — that is any `CLAUDE/Plan/**/*.md` EXCEPT anything under a plan's `JOURNAL/`. Plans capture the work to be done, not how long it will take.

**Everything under `JOURNAL/` is exempt** — day-files (`NNNNN-Journal-YY-MM-DD.md`) and any other file in there. A journal records what actually happened, so an elapsed duration is a historical fact, not a forward estimate. The exemption is by LOCATION as well as by filename, so a mis-named day-file stays exempt.

**Blocked in plan documents:**

- Effort estimates — `**Estimated Effort**: 4 hours`, `Total Estimated Time: 2 days`
- Per-phase durations — `Phase 1: ... (3 days)`, `takes 8-12 hours`
- Target/completion dates — `**Target Completion**: 2026-06-30`, `Completion: 2026-06-30`
- `ETA:`, `timeline:`, `deadline:`, `due date:` lines

**Instead:** break work into concrete tasks and implementation steps, and let the user decide scheduling. Technical durations that describe a feature (cache TTL, session timeout, retention window) are allowed — only work/effort estimates are blocked.

<!-- handler: plan-workflow-guidance -->

## plan_workflow — PLAN.md, supporting docs and JOURNAL/ obey DIFFERENT contracts

Confusing these is the single most common plan-hygiene failure: narrative AND durable detail both get crammed into `PLAN.md` until it is tens of KB of stale log. Each file has a WRITE contract and a READ contract, and the read contract is what justifies the write contract.

|             | `PLAN.md`                                                                                | `SOME-DOC.md`                                                                                                                                   | `JOURNAL/NNNNN-Journal-YY-MM-DD.md`                                                                  |
| ----------- | ---------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| **Write**   | Commit if dirty, EDIT IN PLACE, commit. Rewrite freely — git holds the history           | EDIT IN PLACE, freely — a named supporting document, not a log                                                                                  | APPEND ONLY. Never edit or remove an earlier entry; corrections are new dated entries at the bottom  |
| **Content** | LEAN, surgical, always correct — current truth only: goals, decisions, task tree, status | Durable detail that is current but too big for the task list: research output, findings, decisions and their reasoning, drafts, evidence tables | What actually happened: dated progress, findings, incidents, hand-offs                               |
| **Read**    | Read IN FULL every session — it is your grounding                                        | ON DEMAND, only when its link from `PLAN.md` is followed                                                                                        | NEVER read whole. `tail -n N` the newest day-file, grep it, or send a sub-agent for deep archaeology |
| **Size**    | Bounded — see tiers below                                                                | UNBOUNDED — never opened by a session that doesn't follow its link, so it costs that session nothing                                            | UNBOUNDED by design. Length is never a problem; never tidy or trim a journal                         |

**Why the asymmetry**: a plan is re-read in full at the start of every session that touches it, so every KB is a recurring context cost paid before any work starts. A supporting doc and a journal are both only ever read ON DEMAND — one via its link, the other by tailing/grepping — so both are safe to grow forever. This is the same progressive-disclosure argument the `markdown_organization` handler already makes for `.claude/rules/*.md`.

**Size tiers on `PLAN.md`** (bytes OR lines, whichever trips first): advisory above 18,000 bytes / 350 lines; escalated warning above 25,000 / 500; edits BLOCKED above 35,000 / 900.

**When a plan gets too big there are three remedies, and NONE is deletion**:

1. **EXTRACT** durable detail — research output, findings, decisions and their reasoning, drafts, evidence tables — into a named supporting document in this plan folder (e.g. `RESEARCH-*.md`, `DECISIONS.md`) and link to it from the task.
2. **RELOCATE** dated narrative — progress notes, incident write-ups, hand-off prose — into this plan's JOURNAL/ day-file, which is append-only and unbounded by design.
3. **SPLIT** the plan if the task tree itself is the bulk, since an over-scoped plan is not fixed by better journalling.

**Only an edit that GROWS the file can be blocked.** Shrinking it is silent and a same-size edit (ticking a checkbox) only advises — so an oversized plan can always be updated and refactored down. If a plan genuinely warrants its size, record why in the file: `<!-- MUST_EXCEED_PLAN_SIZE_BECAUSE: <reason> -->`.

**Task status icons**: ⬜ not started, 🔄 in progress, ✅ complete. Include a Success Criteria section and break work into phases.

<!-- handler: qa-suppression-blocker -->

## qa_suppression — QA suppression annotations are blocked

Writing QA suppression directives into source files is blocked across all supported languages. Fix the underlying code issue instead.

**Blocked annotation types (by language)**:

- Python: `noqa` directives, `type: ignore` annotations
- JavaScript/TypeScript: `eslint-disable` inline directives
- Go: `nolint` directives (golangci-lint)
- PHP: `phpstan-ignore`, `psalm-suppress` annotations
- Java/Kotlin: `@SuppressWarnings`, `@Suppress` annotations
- C#: `pragma warning disable` directives
- Rust: `allow(...)` attributes anywhere in the file (item-level `#[allow(...)]` and crate-level `#![allow(...)]`)

**Required action**: Fix the code so QA passes without suppression. If a suppression is genuinely necessary, ask the user to add it manually — this signals a conscious decision rather than a shortcut.

**Excluded paths**: per-language vendor/build/node_modules dirs are skipped by default. Exempt more paths with glob patterns via `handlers.pre_tool_use.qa_suppression.options.exclude_paths` or the project-wide `daemon.exclude_paths` — use these for fixtures that must contain suppression annotations.

<!-- handler: root-recursion-guard -->

## root_recursion_guard — recursive scans rooted at / are blocked

A recursive scanner whose path argument resolves to a catastrophic root location is blocked, because it walks the entire filesystem and can pin every CPU core for hours.

**Blocked** (recursive scanner + dangerous root path):

- `grep -r`/`-R`/`-rl`, `ugrep -r`, `rgrep`, `find`, `fd`/`fdfind`, `rg`
- pointed at `/`, `/proc`, `/sys`, `/home`, `/root`, `~`, `$HOME`

**Allowed**: the same scanners scoped to the project — `rg -l "x" .`, `grep -rl "x" "$CLAUDE_PROJECT_DIR"`, `grep -rl x src/`, `find . -name y`. Non-recursive `grep x /etc/hosts` is not affected.

**Note**: `... | head` does NOT bound a `-l`/`-rl` scan — a producer that matches nothing never writes, so it never receives SIGPIPE and runs to completion across the whole disk.

**Escape hatch** (rare legitimate whole-disk scan):

```
MUST_SCAN_ROOT_BECAUSE="explain why"; grep -rl x /
```

<!-- handler: block-security-antipatterns -->

## security_antipattern — OWASP security antipatterns are blocked

Writing code that contains security antipatterns is blocked across all supported languages. Fix the code to use safe patterns instead.

**Blocked categories**:

- Code injection: `eval`, `exec`, `new Function`, `__import__`, `instance_eval`, `yaml.load` — dynamic execution of a string
- Command injection: `os.system`, `subprocess(..., shell=True)`, `shell_exec`, `proc_open`, `Runtime.exec`, `Process.Start`, `IO.popen`
- Unsafe deserialization: `pickle.load`, `Marshal.load`, `unserialize`, `ObjectInputStream`, `XMLDecoder`, `BinaryFormatter`
- XSS: `innerHTML`, `dangerouslySetInnerHTML`, `document.write`, `template.HTML`/`JS`/`URL`
- Hardcoded credentials: AWS access keys, GitHub tokens, Stripe keys, private key blocks

**This is pattern matching on known-dangerous constructs, not analysis.** It does NOT detect SQL injection, weak hashing, or path traversal — those are properties of how a value FLOWS, which a regex cannot see. Do not read a passing write as 'this code is secure'.

**Supported languages**: Python, JavaScript/TypeScript, Go, PHP, Ruby, Java, Kotlin, C#, Rust, Swift, Dart. Coverage varies by language — a construct blocked in one is not necessarily blocked in another.

**Excluded paths**: vendor/, node_modules/, and test fixtures are skipped by default. Exempt more paths with glob patterns via `handlers.pre_tool_use.security_antipattern.options.exclude_paths` or the project-wide `daemon.exclude_paths`.

<!-- handler: block-sed-command -->

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

<!-- handler: block-sensitive-content -->

## sensitive_content — blocked patterns and secret terms are never written

Writing content that matches a configured public pattern or a gitignored secret word list is blocked. Two sources, two different disclosure rules:

**Public patterns** (`handlers.pre_tool_use.sensitive_content.options.public_patterns`): named regexes safe to name — the deny reason shows the pattern name and the exact matched text so you can fix it.

**Secret word list** (`options.secret_word_list_path`, default `.claude/block-words.secret`, gitignored): a term never appears anywhere — not in the deny reason, not in any log, not in payload capture, not in a transcript archive. The deny reason names only an index (`entry N of M in the secret word list`), which is meaningless without the gitignored file. **Do NOT try to guess or work around the block** — open the secret word list file (if you have access) to see what matched, or ask the user. Only the ADDED text is checked on `Edit` (`new_string`) — removing sensitive content is never blocked.

**Git metadata is checked too.** File contents and file PATHS are only two of the seven places a term can enter a repository — the other five are git metadata, and none of them is a file write. So a `Bash` command that records metadata is also checked: `git commit` (messages), `git tag` (names and messages), `git branch` / `checkout -b` / `switch -c` (branch names), `git config user.name|user.email` (author identity), `git merge -m`. A match denies the command.

**Reading is never blocked.** Only commands that WRITE metadata are candidates, so `grep`, `cat`, `git log --grep=`, `git show`, `git branch --list` and `git tag -l` stay allowed even when the term is right there on the command line — searching for a term and removing it are exactly the work of cleaning a repository.

If a compound command is denied because an unrelated part of it carries a term (`grep <term> f && git commit -m 'clean'`), split it into two calls rather than trying to disguise the term.

Missing/empty/comments-only secret file = this source is silently inert.

<!-- handler: block-sudo-pip -->

## sudo_pip — sudo pip install is blocked

`sudo pip install` (and the `sudo pip3` / `sudo python -m pip` / `sudo python3 -m pip` variants) is blocked. Installing as root corrupts the system Python managed by the OS package manager and creates permission/ownership issues that are painful to recover from.

**Use a virtualenv or `--user` install instead**:

```
python3 -m venv /tmp/venv && /tmp/venv/bin/pip install <package>
# or
pip install --user <package>
```

Even in a container running as root, `sudo` adds nothing — drop it and use a venv.

<!-- handler: enforce-tdd -->

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

**The deny message lists every location it searched.** If your project's real test directory is not in that list, no amount of retrying will satisfy the gate — the project needs to DECLARE the directory (below), not move the test.

**A layout the resolvers cannot infer is declarable** via `handlers.pre_tool_use.tdd_enforcement.options.test_path_map` — a list of `{source_glob, test_dir}` entries. `test_dir` is project-root-relative (or absolute) and FLAT: the test filename is placed directly in it, not mirrored under it. This keeps enforcement ON and is the preferred fix, because a test that exists is worth more than an exemption:

```yaml
test_path_map:
  - source_glob: "**/qaConfig/PHPStan/Rules/**"
    test_dir: "apps/app/qaConfig/Tests"
```

**A path can also be exempted entirely** via that handler's `exclude_paths` option or the project-wide `daemon.exclude_paths` — additive gitignore-style globs. Prefer `test_path_map`: excluding turns the gate OFF for those files.

**Allowed through without blocking**: vendor dirs, node_modules, build outputs, generated files, and file extensions not in the supported language list.

<!-- handler: validate-instruction-content -->

## validate_instruction_content — CLAUDE.md and README.md must have stable content

Writing ephemeral or session-specific content to `CLAUDE.md` or `README.md` is blocked. These files should contain only stable instructions, not implementation logs or session state.

**Blocked content types**:

- Timestamps and ISO dates
- Status emoji followed by completion words (e.g. checkmark + 'Done')
- Implementation log sentences ('created the file X', 'added the class Y')
- Test output counts ('3 tests passed')
- LLM summary section headings ('## Summary', '## Key Points')

Content inside markdown code blocks is exempt from validation.

<!-- handler: prevent-worktree-file-copying -->

## worktree_file_copy — do not copy files between worktrees and the main repo

`cp`, `mv`, and `rsync` operations that move files from a worktree directory (`untracked/worktrees/` or `.claude/worktrees/`) into the main repo (`src/`, `tests/`, `config/`) — or vice versa — are blocked.

Worktrees are isolated branches. Cross-copying corrupts that isolation and can silently overwrite in-progress work.

**Allowed**: operations within the same worktree branch. **To merge changes**: use `git merge` or `git cherry-pick` instead.

<!-- handler: prevent-system-file-edits -->

## system_paths — do not edit deployed system files directly

Writing or editing files under system paths (/etc/, /var/, /usr/, /opt/, /root/, /home/) is blocked.
These are deployed files managed by Ansible.

**Edit the project source instead**:

- `/etc/foo` → `files/etc/foo`
- `/var/local/foo` → `files/var/local/foo`
- `/usr/bin/foo` → `files/usr/bin/foo`

Then deploy via Ansible playbook.

<!-- handler: enforce-ansible-deployment -->

## ansible_enforcement — no direct system management commands

Direct package management, service management, and system configuration commands are blocked. All system changes must go through Ansible playbooks.

**Blocked**: `dnf install`, `systemctl enable/start/stop`, `gsettings set`, `useradd`, `pip install` (system), `npm install -g`, `flatpak install`

**Allowed** (read-only queries): `dnf info/list/search`, `systemctl status`, `gsettings get`, `flatpak list`

**Use instead**: Create/update Ansible playbook in `playbooks/imports/` and deploy with `ansible-playbook`.

<!-- handler: background-process-tracker -->

## background_process_tracker — backgrounded processes are tracked

A PostToolUse advisory that fires when a Bash call backgrounds a process (`run_in_background: true`, or a `&`/`nohup`/`setsid`/`disown` command). It records the command to `background-processes.jsonl` and injects rate-limited guidance.

**The daemon never kills.** It surfaces runaways; you decide.

When you background a long-lived process:

- Ensure **EXACTLY ONE** non-durable recurring **watchdog cron** exists — one covers the whole session, since its prompt harvests ALL tracked background processes. `CronList` FIRST: reuse the one already running (`CronDelete` any extras), and only if none is listed create it (CronCreate, durable:false) with a prompt that runs `.claude/hooks-daemon/bin/hooks-daemon harvest-background` and acts on any runaway — this covers the idle/compaction window a tool-call hook cannot. Do NOT wait for the cron; keep working.
- Check on demand: run `harvest-background` (exit 1 == runaways surfaced).
- Reap a runaway by its **process group**: `kill -- -<pgid>` (not just the pid).
- Keep a wanted long task: note `KEEP_RUNNING_BECAUSE="reason"`.
- Delete the watchdog cron (CronDelete) when no backgrounded work remains.

Advisory is rate-limited per session (default-on). Disable with `handlers.post_tool_use.background_process_tracker.enabled: false`.

<!-- handler: command-hints -->

## command_hints — advisory reminders after specific commands

PostToolUse advisory (never blocks). When a configured command is detected in a Bash call, a HINT is injected reminding you of a follow-up action. Shipped default: running `agent-browser` reminds you to close the browser session when finished.

**Rate-limited per hint** — each hint has a `ttl_seconds` cooldown (tracked per session + hint id) so it does not repeat on every matching command; state resets on daemon restart, so a hint may fire once more after a restart.

**Configure** via `handlers.post_tool_use.command_hints.options`: `mode: additive` (default) appends your `hints` list to the built-in set — a project entry whose `id` matches a built-in one overrides it; `mode: replace` discards the built-in set entirely and uses only your list. Each hint: `id`, `pattern` (a literal command name, matched at the start of a shell segment — path-qualified and `env`-prefixed spellings are recognised, but it never fires on the word appearing as an unrelated argument), `hint` (the reminder text), `ttl_seconds`, and optional `min_calls_between` (secondary count-based gate). Disable with `handlers.post_tool_use.command_hints.enabled: false`.

<!-- handler: git-hooks-executable-fixer -->

## git_hooks_executable_fixer — auto-fixes non-executable git hooks

When a git command prints `hint: The '...' hook was ignored because it's not set as executable`, this handler automatically `chmod +x`s every non-`.sample` file in the repository's hooks directory (resolved via `git rev-parse --git-path hooks`, so worktrees and `core.hooksPath` are handled). Execute bits are added with least privilege (only where read is already granted). It never blocks the command and reports which hooks it fixed via advisory context. `.sample` files and already-executable hooks are left untouched.

<!-- handler: lint-on-edit -->

## lint_on_edit — source writes are linted, and a failure DENIES

Every `Write`/`Edit` to a Python, Shell, Go, PHP, Ruby, Rust, Swift, Kotlin or
Dart file is linted immediately. A lint failure DENIES the tool call.

**The write has ALREADY landed on disk.** A PostToolUse denial is a failure
report, not a rollback — the file exists, with your content in it. Fix the
reported problems with `Edit`. Do NOT re-`Write` the file from scratch: that
rewrites content already on disk from memory, and loses anything you no longer
have in hand.

A denial also cancels every sibling tool call batched in the same turn, so
re-issue those separately.

Each language runs a cheap syntax check first (`python -m py_compile`, `bash -n`, `go vet`, `php -l`, …) and then an optional deeper linter (`ruff`,
`shellcheck`, `golangci-lint`, `rubocop`, …). Tools are resolved from the
daemon's venv before `PATH`.

**A linter that is not installed never blocks.** You get an advisory saying it
was not found and the write stands — so that message means the check was
SKIPPED, not that it passed. That leniency is specific to THIS handler:
`.ts`/`.tsx` files are handled by `validate_eslint_on_write`, which denies on
a timeout and on any failure to run ESLint.

Narrow it under `handlers.post_tool_use.lint_on_edit.options`: `languages`
restricts which languages are checked, `command_overrides` replaces a
language's `default`/`extended` command (set `extended: null` to run only the
syntax check), and `exclude_paths` exempts paths entirely via gitignore-style
globs. The project-wide `daemon.exclude_paths` applies here too; the two are
additive and neither overrides the other.

<!-- handler: markdown-table-formatter -->

## markdown_table_formatter — markdown tables are auto-aligned

After every `Write` or `Edit` of a `.md` or `.markdown` file, the content is re-formatted via `mdformat + mdformat-gfm` so that table pipes are aligned and column widths are consistent. The handler is non-terminal and advisory — it never blocks, it just rewrites the file on disk.

**What changes:**

- Table pipes are aligned vertically and delimiter rows widened to match cell widths.
- Ordered lists keep consecutive numbering (`1.` `2.` `3.`).
- `---` thematic breaks are preserved (mdformat's 70-underscore default is post-processed back).
- Asterisks in table cells are escaped (`*` → `\*`) as required by GFM.

**Exempt:** anything under a plan's `JOURNAL/` directory is NEVER reformatted — day-files (`JOURNAL/NNNNN-Journal-YY-MM-DD.md`, Plan 00163) and any other file in there. A journal is an append-only, byte-stable log; rewriting it would trip the `journal-append-only` check. The exemption is by LOCATION as well as by filename, so a mis-named day-file is still safe.

**Ad-hoc formatting of existing files:**

```
.claude/hooks-daemon/bin/hooks-daemon format-markdown <path>
```

<!-- handler: recovery-cron-advisor -->

## recovery_cron_advisor — failsafe recovery cron lifecycle advisory

An advisory PostToolUse handler that fires across a plan's lifecycle and
injects guidance telling the agent to manage a non-durable hourly failsafe
recovery cron.

**There must be EXACTLY ONE recovery cron per session — never one per
plan.** The canonical prompt is plan-agnostic ('the active plan/task'), so a
single cron covers every plan in the session and a second only double-fires
on the same session. Always `CronList` before creating: reuse what is
running, delete extras, create only when none exists.

### What it does

Three lifecycle phases are detected from Write/Edit to `CLAUDE/Plan/<digits>-<name>/PLAN.md`
(never from files inside `Completed/`) and from `mkplan.bash` Bash invocations:

| Phase          | Trigger                                               | Guidance injected                                                                                                                                                                                                                                         |
| -------------- | ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Creation**   | New PLAN.md written, or `mkplan.bash` invoked         | `CronList` FIRST: reuse the recovery cron already running (record THAT id in the plan's `JOURNAL/`, create nothing) and `CronDelete` any extras; create one (CronCreate, durable:false) ONLY if none is listed. Do NOT wait for the cron.                 |
| **Progress**   | Edit to PLAN.md touching task-status icons (⬜/🔄/✅) | `CronList`: exactly one → nothing to do; more than one → `CronDelete` the extras; none → create one. Keep working.                                                                                                                                        |
| **Completion** | `**Status**: Complete[d]` written/edited              | Plan complete — **warns first**: deleting now leaves the still-live session with no recovery coverage. Keep the cron if any further work may happen (it is non-durable and dies on session exit); `CronDelete` only when certain the session is finished. |

Progress reminders are rate-limited per plan: the handler advises on the first
progress edit and then once every few progress edits for that plan, so it does
not spam context on every edit. Completion always advises (bypasses the interval).

### CRITICAL: recovery cron is NOT a heartbeat

The recovery cron is a **failsafe safety net**, not a pacing mechanism:

- The agent **must never** wait for the cron between units of work.
- Work proceeds at **full speed** until an external factor (Claude API error,
  rate limit, 5-hour usage limit, network failure) actually stalls it.
- The cron fires only while the REPL is idle; it cannot interrupt active work.
- Treating the cron as a heartbeat is an **own goal** — it would convert a
  safety net into an artificial hourly throttle.

### Canonical recovery-cron prompt

Use this verbatim as the CronCreate prompt:

```
**FAILSAFE RECOVERY CHECK (automated hourly safety net — NOT a heartbeat).**
If your most recent work on the active plan/task was interrupted by an
*external* factor (Claude API error/overload, rate limit, 5-hour usage limit,
network failure) and is now resumable, resume it immediately and carry it to
completion. If you are blocked **only** on human input, do nothing and keep
waiting. If work is already proceeding normally, this is a **no-op** — do not
interrupt, restart, or duplicate anything in flight. Never treat this as a
heartbeat or pacing signal: between checks, continue at full speed until an
external factor actually stops you — waiting for the cron is an own goal. Do
NOT delete this cron merely because a tick finds nothing to resume: it is
non-durable and ends automatically when the session exits, and a still-live
session stays exposed to the next rate limit without it. Remove it (CronDelete)
only once the session is genuinely finished with no further work.
```

### Configuration

This handler is **on by default** (opt-out). Disable with:

```yaml
handlers:
  post_tool_use:
    recovery_cron_advisor:
      enabled: false
```

<!-- handler: validate-eslint-on-write -->

## validate_eslint_on_write — TypeScript writes are ESLint-checked, and a failure DENIES

A `Write`/`Edit` to a `.ts` or `.tsx` file is run through ESLint. Reported
errors DENY the tool call.

**The write has ALREADY landed on disk.** The denial is a failure report, not
a rollback — the file exists with your content in it. Fix the reported problems
with `Edit` (`npx eslint <file> --fix` clears most of them), and re-issue any
sibling tool calls that were cancelled alongside the denied one.

**This is STRICTER than `lint_on_edit`, which covers the other languages.**
That handler ALLOWs when its linter is missing or when the check times out;
this one DENIES on an ESLint timeout and on any failure to run ESLint at all.
Do not carry "a missing linter never blocks" across to TypeScript.

**Enforcement is gated on `llm:` scripts in `package.json`.** With none
present this handler only advises — and suggests adding `llm:lint` — so silence
is not evidence that a `.ts` file is clean.

<!-- handler: ccy-supervisor-integrity -->

## ccy_supervisor_integrity — keep the ccy supervisor properly set up

At session start this handler checks a ccy project (`.claude/ccy/`) whose supervisor is **armed** (`ccy.env` exports `CCY_CLAUDE_WRAPPER` referencing `claude-supervise.py`). It warns — never blocks — when the setup is brick-risky:

- **`claude-supervise.py` missing** → the launcher's `exec` fails. Redeploy via a daemon upgrade or restore from git.
- **not executable** → `chmod +x .claude/ccy/claude-supervise.py`.
- **git-ignored** → it won't be committed; teammates get a broken supervisor. Add a `!claude-supervise.py` / `!ccy.env` whitelist line to `.claude/ccy/.gitignore` and commit the files.
- **`ccy.deploy_supervisor: false` while armed+present** → the installer skips deploy on `false`, so upgrades never refresh `claude-supervise.py` and the project runs an increasingly stale supervisor. Set it to `true` (or disarm `CCY_CLAUDE_WRAPPER` if you truly want it off).

It also detects a **stale running supervisor** (Plan 00164): when a daemon upgrade has put a NEWER `claude-supervise.py` on disk than the live process (compared by source fingerprint, not just version), it advises restarting ccy so the wrapper re-execs the updated supervisor. Nothing is broken meanwhile — the old supervisor keeps working until the session is relaunched.

When you see this alert, fix the listed item(s) and commit the ccy files so the supervisor works for everyone.

<!-- handler: git-upstream-checker -->

## git_upstream_checker — additive fetch + pull/cleanup advice on session start

On each new session the daemon runs an **additive** `git fetch --all` (never `--prune` — it never removes anything automatically) and then:

**If your branch is behind its upstream**, acts on the configured `mode`:

- `warn` (default): strongly advises you to run `git pull`.
- `agent-pull`: instructs you to run `git pull` as your first action.
- `auto-pull`: the daemon runs `git pull --ff-only` for you on a clean, non-diverged tree; if it cannot fast-forward (dirty tree or diverged history) it degrades to a warning and you pull manually.

**If the upstream was REWRITTEN**, every mode above is overridden and NO pull is advised in any wording. The signal is a divergence whose two sides share no commit shas yet resolve to the SAME tree: identical content, so there is nothing to merge and each local commit is a pre-rewrite duplicate. Pulling would merge the entire pre-rewrite history back in and republish whatever the rewrite (a `filter-repo` secret-strip, say) was run to remove. The advisory instead asks a human to realign the branch onto its upstream and to re-fetch tags with `--force`, since a rewrite moves every tag to a new sha. Do NOT work around this by pulling — if you believe the divergence is genuine, check the trees yourself before merging.

**If local branches track a remote branch that was deleted**, it lists them (marked merged = safe vs not-merged = has unique commits) and asks you to clean up AFTER checking: `git branch -d <name>` for merged branches, ask the human for the rest, and optionally `git fetch --prune` the stale remote-tracking refs. The daemon never prunes or deletes a branch itself; never use `git branch -D`.

It is silent when up to date with no gone branches, not in a git repo, on a detached HEAD, or without an upstream. Configure via `handlers.session_start.git_upstream_checker.options.mode`.

<!-- handler: hook-registration-checker -->

## hook_registration_checker — hooks configuration policy

On every new session this handler audits hook configuration across `.claude/settings.json` and `.claude/settings.local.json`. When it reports issues, fix them — do not ignore the warning.

### Policy

1. **All hooks live in `settings.json`.** That file is tracked in version control, visible to teammates, and is the single source of truth for the daemon.
2. **`settings.local.json` must contain ZERO `hooks` entries.** It exists for per-developer `permissions` and IDE state only. A `hooks` block there is either (a) invisible to the rest of the team, or (b) duplicated with `settings.json` — in which case the hook fires twice per event.
3. **Hook commands must invoke the daemon wrapper.** Every registered command must end with `/.claude/hooks/{event}`. Anything else (inline Python, custom shell scripts, bespoke paths) is a legacy setup that bypasses the daemon entirely.

### Remediation

- **Hooks in `settings.local.json`**: move each `hooks` entry to `settings.json`, then delete the `hooks` key from `settings.local.json`. Confirm no duplicates remain.
- **Legacy-style commands**: replace them with a project-level handler. Run `.claude/hooks-daemon/bin/hooks-daemon init-project-handlers` to scaffold `.claude/project-handlers/`, port the logic into a handler class, then restore the daemon wrapper in `settings.json`. The daemon will auto-discover the new handler on restart.
- **Missing hooks**: by default this handler SELF-HEALS — it merges the full wired registration set into `settings.json` on session start (additive; preserves `permissions`/`env`/`statusLine` and any custom hooks; one-shot backup to `settings.json.bak.pre-registration-repair`), so the flood stops without a reinstall. Opt out with `handlers.session_start.hook_registration_checker.options.auto_repair_registrations: false`, then re-run the installer or add the missing `{event_name}` entry manually.
- **Duplicate hooks**: a hook registered in both files fires twice. Keep the `settings.json` entry and remove the duplicate in `settings.local.json`.

<!-- handler: plan-qa-sweep -->

## plan_qa_sweep — plan-tree drift report at session start

At the start of each new session the plan directory is swept with the
plan QA check catalogue. That covers the cross-file invariants
(index/folder bijection, number collisions, statistics recount,
archive structure, status-vs-location coherence, staleness) AND the
document-level rules applied to every PLAN.md already on disk —
status line present, status token in the enum, header/body coherence,
task grammar, path existence, journal day-file naming. Findings are
injected once as advisory context — the sweep never blocks.

**A rule that only fires at write time cannot see what predates it.**
The document-level checks run on BOTH surfaces for that reason, so a
violation introduced before the rule existed — or by a `git mv`, a
merge, or any path other than a Write/Edit tool call — is still
reported. The rules that are deliberately edit-only are the ones
about the ACT of writing (editing an archived plan, rewriting a
journal, growing an oversized document); each records its reason in
`plan_qa/checks/common.py`.

**When a drift report appears**: fix the listed findings (each names
its exact remediation) as part of your plan housekeeping, then
re-check with:

```
.claude/hooks-daemon/bin/hooks-daemon plan-qa --sweep
```

The CLI exits 1 while findings remain (CI-able). Single-file lint:
`plan-qa --lint <PLAN.md>`; staged-commit check: `plan-qa --check-staged`.
Policy lives under `plan_workflow.qa` in `.claude/hooks-daemon.yaml`
(archive dir names, staleness window, legacy/collision allowlists).

<!-- handler: plan-workflow-asset-checker -->

## plan_workflow_asset_checker — plan tooling provisioning alert

At session start, when the plan workflow is enabled but the daemon-owned `mkplan.bash` is missing from the plan directory, this advisory fires (it never blocks). A missing `mkplan.bash` means `CLAUDE.md` and `plan_number_helper` reference a scaffolder that does not exist and journalling is inert.

**Fix**: (re)deploy the assets on demand —

```
.claude/hooks-daemon/bin/hooks-daemon deploy-plan-workflow
```

The deploy is idempotent (fills gaps only, never overwrites client-owned files). Silent when `mkplan.bash` is present or the workflow is disabled.

<!-- handler: project-handler-load-checker -->

## project_handler_load_checker — project protection degraded alert

At session start this handler reports any **project handlers** (`.claude/project-handlers/`) that FAILED to load in the running daemon. A skipped handler is a silently-disabled protection — the alert exists so you never assume a guardrail is active when it is not.

### When you see `🚨 PROJECT PROTECTION DEGRADED 🚨`

1. **Do not assume normal guardrails are in force.** The listed handlers are OFF for this session.
2. **Diagnose** each failure: `.claude/hooks-daemon/bin/hooks-daemon validate-project-handlers` names the file, the missing method, and the daemon version that introduced it.
3. **Fix** the handler(s) — usually adding a required method stub (e.g. `get_claude_md`) that a daemon upgrade made mandatory.
4. **Restart the daemon** (`.claude/hooks-daemon/bin/hooks-daemon restart`). The alert reflects the *running* daemon, so it clears only after a restart reloads the fixed handlers — fixing the file alone is not enough.

The handler is silent when every project handler loads, so seeing this alert always means real action is required.

<!-- handler: idle-housekeeping-advisory -->

## idle_housekeeping_advisory — report-first idle housekeeping (beta, opt-in)

When the session is idle and caught up (repeated no-op failsafe-recovery ticks), this advisory suggests a bounded HOUSEKEEPING MODE: dispatch specialist housekeeping sub-agents that run read-only audits and write shareable **markdown report files** (default `untracked/reports/`). It is REPORT-ONLY — never auto-fix or auto-commit — and strictly lower priority than real work (a real user prompt aborts it). Off by default; enable via `handlers.user_prompt_submit.idle_housekeeping_advisory.enabled: true`. A project can point it at its own doc via the `custom_guidance_doc` option (`custom_guidance_mode: additive` appends it to the default, `replace` uses only the project doc). See docs/guides/CREATING_REPORTS.md.

<!-- handler: standing-authorisations -->

## standing_authorisations — a project can record a standing request

Some instructions are conditional on the user having asked ("unless the user requested it"). A request made in conversation does not survive the session, so this project can record one in config instead, and the daemon replays it on each prompt.

Configured in `.claude/hooks-daemon.yaml` under `handlers.user_prompt_submit.standing_authorisations.options.authorisations`, as a list of `{id, enabled}` entries. Built-in ids: `subagent-delegation`, `workflow-orchestration`.

**Every entry ships disabled.** The handler is enabled so the options are discoverable, but nothing is authorised until the project turns it on — the daemon must never assert consent that was not given. Enabling one is a deliberate act by whoever owns the repository, and removing it withdraws the authorisation.

<!-- handler: auto-approve-reads -->

## auto_approve_reads — gated on bypassPermissions mode

Read-only tool permission requests (`Read`, `Glob`, `Grep`) are auto-approved **only** when Claude Code reports `permission_mode == "bypassPermissions"` (YOLO mode).

In every other mode (`default`, `plan`, `acceptEdits`, `dontAsk`) the handler defers and Claude Code's normal approval prompt is shown — the user has not opted out of per-tool approvals, so the daemon must not silently approve on their behalf.

If a permission prompt for `Read` appears in `default` mode, that is correct behaviour — approve it via Claude Code's UI.

<!-- handler: auto-continue-stop -->

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
- Smuggle a rhetorical continue question inside a `STOPPING BECAUSE:` message ('STOPPING BECAUSE: slice 1 done. Want me to build slice 2?') — this is HARD-BLOCKED; the prefix does not exempt tautological questions. Just continue with the next unit of work
- Use `AUTO-CONTINUE` unless you intend to keep working indefinitely

**Before asking a question, evaluate it critically**:

- Tautological/rhetorical questions with obvious answers ("Should I continue?", "Would you like me to proceed?") — do NOT ask, just do it
- Errors with a clear next step ("The test failed, should I fix it?") — do NOT ask, just fix it
- Genuine choice questions where all options are valid ("Which of A, B, or C should we use?") — these deserve a response. Use `STOPPING BECAUSE: need user input` and ask your question

**Recovering from a `tool_use_error` — do NOT stop silently**:

Some tool errors require an explicit recovery action, not a halt. The most common shape:

- You call `Edit` or `Write` on a file you have not yet read.
- Claude Code returns a `tool_use_error` (e.g. "File has not been read yet").
- The correct recovery is **Read the file, then retry Edit/Write** — **do not stop**. Stopping silently after a tool error triggers a Stop-hook re-entry loop and wastes a turn.

**Rule: Read before Edit/Write.** If you must edit a file you have not read, Read it first in the same turn. The daemon's Stop handler will detect a `tool_use_error` followed by a silent stop and re-fire to force recovery.

**On Stop hook re-entry (the hook fires again after a prior block)**: your next response is treated like any other — it must either prefix with `STOPPING BECAUSE:` or continue the work. Re-entry does not exempt you from the explanation rule.

<!-- handler: worktree-create -->

## worktree_create — semantic worktree naming

When Claude Code creates a worktree (an `isolation: "worktree"` agent or `--worktree` session), the daemon creates it at a human-friendly path `.claude/worktrees/<slug-of-name>-<shorthash>/` and echoes that path. Name an agent semantically (the Agent tool's `name:`) to get a readable worktree directory (e.g. `refactor-auth-4f2a1c9b`) instead of an opaque `wf_<hash>`. The short hash suffix keeps identically-named agents from colliding.

</hooksdaemon>

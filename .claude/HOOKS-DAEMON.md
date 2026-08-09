# Hooks Daemon - Active Configuration

> Generated on 2026-08-09 (v3.51.0) by `generate-docs`. Regenerate: `/workspace/.claude/hooks-daemon/bin/hooks-daemon generate-docs`

## Plan Mode

> Write plans DIRECTLY to project version control.

**Plan location**: `CLAUDE/Plan/{number}-{name}/PLAN.md`
**Next number**: Scan `CLAUDE/Plan/` (including `Completed/`) for highest number, increment.
**Workflow docs**: @CLAUDE/PlanWorkflow.md

The redirect handler intercepts `~/.claude/plans/` writes as a safety net only.

## Active Handlers

### PreToolUse (37 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 10 | ask_user_question_blocker | TERMINAL | Allow AskUserQuestion only when every question is prefix-justified |
| 10 | curl_pipe_shell | TERMINAL | Block curl/wget piped to shell commands |
| 10 | daemon_restart_verifier | ADVISORY | Verify daemon can restart before allowing git commits |
| 10 | destructive_git | BLOCKING | Block destructive git commands that permanently destroy data |
| 10 | lock_file_edit_blocker | TERMINAL | Block direct editing of package manager lock files |
| 10 | pip_break_system | TERMINAL | Block pip install --break-system-packages commands |
| 10 | sed_blocker | BLOCKING | Block sed used for file modification - Claude gets sed wrong and causes file destruction |
| 10 | sudo_pip | TERMINAL | Block sudo pip install commands |
| 11 | daemon_location_guard | BLOCKING | Prevent agents from cd-ing into .claude/hooks-daemon and running commands |
| 12 | absolute_path | BLOCKING | Require absolute paths for Read/Write/Edit tool file_path parameters |
| 13 | error_hiding_blocker | BLOCKING | Block error-hiding patterns in code written via Write or Edit tools |
| 15 | dangerous_permissions | TERMINAL | Block chmod 777 and dangerous permission commands |
| 15 | pipe_blocker | BLOCKING | Block expensive commands piped to tail/head to prevent information loss |
| 15 | security_antipattern | BLOCKING | Block Write/Edit of files containing security antipatterns |
| 15 | tdd_enforcement | BLOCKING | Enforce TDD by blocking production file creation without corresponding test file |
| 15 | worktree_file_copy | BLOCKING | Prevent copying files between worktrees and main repo |
| 16 | root_recursion_guard | BLOCKING | Block recursive scanners (grep -r, find, fd, rg, ...) rooted at ``/``/home/etc |
| 20 | git_stash | BLOCKING | Block or warn about git stash based on mode configuration |
| 30 | plan_number_helper | ADVISORY | Detect bash commands attempting to discover plan numbers and provide correct answer |
| 30 | qa_suppression | BLOCKING | Block QA suppression comments across all supported languages |
| 30 | validate_plan_number | ADVISORY | Validate plan folder numbering to ensure sequential plans |
| 35 | markdown_organization | BLOCKING | Enforce markdown file organization rules |
| 38 | lsp_enforcement | BLOCKING | Enforce LSP tool usage instead of Grep/Bash grep for symbol lookups |
| 40 | gh_issue_comments | BLOCKING | Ensure gh issue view commands always include --comments flag |
| 40 | gh_pr_comments | BLOCKING | Ensure gh pr view commands always include --comments flag |
| 40 | global_npm_advisor | NON-TERMINAL | Advise on global npm/yarn package installations |
| 40 | plan_time_estimates | ADVISORY | Block time estimates in plan documents |
| 44 | plan_qa_commit_gate | NON-TERMINAL | Warn-first cross-file plan QA gate on git commit |
| 44 | plan_qa_edit | NON-TERMINAL | Blocking/advisory edit-time lint for plan documents |
| 45 | plan_workflow | ADVISORY | Provide guidance when creating plan files |
| 45 | task_tdd_advisor | ADVISORY | Advise on TDD workflow when spawning Task agents for implementation work |
| 50 | npm_command | ADVISORY | Enforce llm: prefixed npm commands and block direct npx tool usage |
| 50 | plan_completion_advisor | ADVISORY | Advise when a plan is being marked as complete |
| 50 | validate_instruction_content | TERMINAL | Validates content being written to CLAUDE.md and README.md files |
| 55 | web_search_year | ADVISORY | Validate WebSearch queries don't use outdated years |
| 57 | daemon_docs_guard | ADVISORY | Warn when reading from the hooks-daemon internal CLAUDE/ docs directory |
| 60 | british_english | ADVISORY | Warn about American English spellings in content files (non-blocking) |

### PostToolUse (7 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 10 | validate_eslint_on_write | ADVISORY | Run ESLint validation on TypeScript/TSX files after write |
| 25 | lint_on_edit | NON-TERMINAL | Run language-aware lint validation on files after Write/Edit |
| 26 | markdown_table_formatter | NON-TERMINAL | Auto-format markdown tables after Write/Edit of .md files |
| 27 | git_hooks_executable_fixer | NON-TERMINAL | Detect git's "not set as executable" hint and fix the hooks automatically |
| 28 | background_process_tracker | ADVISORY | Track backgrounded Bash processes and advise on watchdog/harvest (never kills) |
| 30 | recovery_cron_advisor | ADVISORY | Advisory handler that manages failsafe recovery cron across plan lifecycle |
| 50 | bash_error_detector | ADVISORY | Detect errors and warnings in Bash command output |

### SessionStart (12 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 40 | yolo_container_detection | ADVISORY | Detects YOLO container environments using precise OS-level container markers |
| 50 | project_handler_load_checker | ADVISORY | Loudly alert at session start when project handlers failed to load |
| 51 | hook_registration_checker | ADVISORY | Validate hook registrations in Claude Code settings on session start |
| 52 | optimal_config_checker | ADVISORY | Check Claude Code environment for optimal configuration on session start |
| 53 | git_filemode_checker | ADVISORY | Warn when git core.fileMode=false is detected |
| 54 | gitignore_safety_checker | ADVISORY | Warn when required .claude/ paths are absent from .gitignore |
| 55 | suggest_status_line | ADVISORY | Suggest setting up daemon-based statusline on session start |
| 55 | version_check | ADVISORY | Check daemon version against latest GitHub release on new sessions |
| 56 | git_upstream_checker | ADVISORY | Full-fetch + configurable pull policy when a branch is behind upstream |
| 57 | plan_qa_sweep | ADVISORY | Advisory SessionStart sweep over the plan tree (silent when clean) |
| 58 | ccy_supervisor_integrity | ADVISORY | Advisory: warn when the ccy supervisor is armed but its files are unsafe |
| 59 | plan_workflow_asset_checker | ADVISORY | Advise when plan_workflow is enabled but its assets are not provisioned |

### SessionEnd (1 handler)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 10 | cleanup | NON-TERMINAL | Clean up temporary files when session ends |

### PreCompact (2 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 10 | transcript_archiver | NON-TERMINAL | Archive conversation transcript before compaction |
| 20 | compaction_signal | NON-TERMINAL | Write a ``<session>.compacting`` signal on PreCompact for the supervisor |

### UserPromptSubmit (4 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 20 | git_context_injector | CONTEXT | Inject current git status as context when user submits a prompt |
| 54 | post_clear_auto_execute | ADVISORY | Inject execution guidance on the first prompt of a new session |
| 55 | critical_thinking_advisory | ADVISORY | Periodically inject advisory context encouraging critical evaluation |
| 56 | idle_housekeeping_advisory | ADVISORY | After N consecutive no-op recovery ticks, advise a report-first |

### PermissionRequest (1 handler)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 10 | auto_approve_reads | TERMINAL | Auto-approve read-only tool permission requests |

### Notification (1 handler)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 10 | notification_logger | NON-TERMINAL | Log all notification events to a JSONL file |

### Stop (6 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 15 | auto_continue_stop | TERMINAL | Intercept Stop events and enforce explicit stop reasons or auto-continue |
| 30 | hedging_language_detector | ADVISORY | Detect hedging language that signals guessing instead of researching |
| 50 | task_completion_checker | ADVISORY | Remind agent to verify task completion before stopping |
| 58 | dismissive_language_detector | ADVISORY | Detect dismissive language that signals avoiding work |
| 100 | remind_prompt_library | ADVISORY | Remind to capture successful prompts to the library |
| 100 | subagent_completion_logger | NON-TERMINAL | Log subagent completion events to a JSONL file |

### SubagentStop (2 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 10 | subagent_completion_logger | NON-TERMINAL | Log subagent completion events to a JSONL file |
| 20 | remind_prompt_library | ADVISORY | Remind to capture successful prompts to the library |

### Status (14 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 2 | multithread_indicator | NON-TERMINAL | Show this thread's rank among live Agent-View threads (``🧵 Y/X``) |
| 4 | environment_indicator | NON-TERMINAL | Show 💻 (desktop/host) or a container icon (🐳 docker / 📦 podman / 🧊 lxc) |
| 5 | git_repo_name | NON-TERMINAL | Show git repository name at start of status line |
| 6 | account_display | NON-TERMINAL | Display Claude account username in status line |
| 10 | model_context | NON-TERMINAL | Format model name with effort level and color-coded context percentage |
| 12 | context_sidecar | NON-TERMINAL | Write an observe-only context-state sidecar for the PTY supervisor |
| 13 | supervisor_indicator | NON-TERMINAL | Show whether the ccy PTY supervisor is overseeing the session |
| 14 | current_time | NON-TERMINAL | Display current local time in status line (24-hour format, no seconds) |
| 15 | usage_tracking | NON-TERMINAL | Display daily and weekly token usage percentages |
| 20 | git_branch | NON-TERMINAL | Show current git branch with magicmonty-style status icons if in a git repo |
| 25 | working_directory | NON-TERMINAL | Display working directory when it differs from project root |
| 28 | startup_cleanup | NON-TERMINAL | Show 🧹 briefly after daemon startup to indicate stale-file cleanup ran |
| 30 | daemon_stats | NON-TERMINAL | Show daemon health: uptime, memory, last error, log level |
| 32 | upgrade_notifier | NON-TERMINAL | Show a daemon-upgrade-available indicator on the status line |

### WorktreeCreate (1 handler)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 50 | worktree_create | TERMINAL | Create a git worktree at a semantic path and return its absolute path |

### WorktreeRemove (1 handler)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 50 | worktree_remove | TERMINAL | Prune stale worktree registrations (and remove a named worktree) |

### Plugin (2 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 8 | SystemPathsHandler | TERMINAL | Block Write/Edit operations on deployed system files |
| 10 | AnsibleEnforcementHandler | TERMINAL | Block direct system management commands - enforce Ansible deployment |

## Quick Config Reference

**Config file**: `.claude/hooks-daemon.yaml`
**Enable/disable**: Set `enabled: true/false` under handler name
**Handler options**: Set under `options:` key per handler

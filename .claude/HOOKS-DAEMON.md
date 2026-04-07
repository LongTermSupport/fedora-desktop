# Hooks Daemon - Active Configuration

> Generated on 2026-04-07 (v3.0.0) by `generate-docs`. Regenerate: `$PYTHON -m claude_code_hooks_daemon.daemon.cli generate-docs`

## Active Handlers

### PreToolUse (34 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_pre_tool_use | NON-TERMINAL | Simple test handler that confirms PreToolUse hook is working |
| 10 | ask_user_question_blocker | TERMINAL | Block AskUserQuestion to prevent progress-blocking user prompts |
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
| 14 | security_antipattern | BLOCKING | Block Write/Edit of files containing security antipatterns |
| 15 | dangerous_permissions | TERMINAL | Block chmod 777 and dangerous permission commands |
| 15 | pipe_blocker | BLOCKING | Block expensive commands piped to tail/head to prevent information loss |
| 15 | worktree_file_copy | BLOCKING | Prevent copying files between worktrees and main repo |
| 20 | git_stash | BLOCKING | Block or warn about git stash based on mode configuration |
| 30 | plan_number_helper | ADVISORY | Detect bash commands attempting to discover plan numbers and provide correct answer |
| 30 | qa_suppression | BLOCKING | Block QA suppression comments across all supported languages |
| 35 | tdd_enforcement | BLOCKING | Enforce TDD by blocking production file creation without corresponding test file |
| 38 | lsp_enforcement | BLOCKING | Enforce LSP tool usage instead of Grep/Bash grep for symbol lookups |
| 40 | gh_issue_comments | BLOCKING | Ensure gh issue view commands always include --comments flag |
| 40 | global_npm_advisor | NON-TERMINAL | Advise on global npm/yarn package installations |
| 40 | validate_plan_number | ADVISORY | Validate plan folder numbering to ensure sequential plans |
| 45 | plan_time_estimates | ADVISORY | Block time estimates in plan documents |
| 45 | plan_workflow | ADVISORY | Provide guidance when creating plan files |
| 45 | task_tdd_advisor | ADVISORY | Advise on TDD workflow when spawning Task agents for implementation work |
| 50 | markdown_organization | BLOCKING | Enforce markdown file organization rules |
| 50 | npm_command | ADVISORY | Enforce llm: prefixed npm commands and block direct npx tool usage |
| 50 | plan_completion_advisor | ADVISORY | Advise when a plan is being marked as complete |
| 50 | validate_instruction_content | TERMINAL | Validates content being written to CLAUDE.md and README.md files |
| 55 | web_search_year | ADVISORY | Validate WebSearch queries don't use outdated years |
| 57 | daemon_docs_guard | ADVISORY | Warn when reading from the hooks-daemon internal CLAUDE/ docs directory |
| 60 | british_english | ADVISORY | Warn about American English spellings in content files (non-blocking) |

### PostToolUse (4 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_post_tool_use | NON-TERMINAL | Simple test handler that confirms PostToolUse hook is working |
| 10 | validate_eslint_on_write | ADVISORY | Run ESLint validation on TypeScript/TSX files after write |
| 25 | lint_on_edit | NON-TERMINAL | Run language-aware lint validation on files after Write/Edit |
| 50 | bash_error_detector | ADVISORY | Detect errors and warnings in Bash command output |

### SessionStart (9 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_session_start | NON-TERMINAL | Simple test handler that confirms SessionStart hook is working |
| 10 | workflow_state_restoration | ADVISORY | Restore workflow state after compaction |
| 40 | yolo_container_detection | ADVISORY | Detects YOLO container environments using multi-tier confidence scoring |
| 51 | hook_registration_checker | ADVISORY | Validate hook registrations in Claude Code settings on session start |
| 52 | optimal_config_checker | ADVISORY | Check Claude Code environment for optimal configuration on session start |
| 53 | git_filemode_checker | ADVISORY | Warn when git core.fileMode=false is detected |
| 54 | gitignore_safety_checker | ADVISORY | Warn when required .claude/ paths are absent from .gitignore |
| 55 | suggest_status_line | ADVISORY | Suggest setting up daemon-based statusline on session start |
| 55 | version_check | ADVISORY | Check daemon version against latest GitHub release on new sessions |

### SessionEnd (2 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_session_end | NON-TERMINAL | Simple test handler that confirms SessionEnd hook is working |
| 100 | cleanup | NON-TERMINAL | Clean up temporary files when session ends |

### PreCompact (3 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_pre_compact | NON-TERMINAL | Simple test handler that confirms PreCompact hook is working |
| 10 | transcript_archiver | NON-TERMINAL | Archive conversation transcript before compaction |
| 10 | workflow_state_pre_compact | NON-TERMINAL | Detect and preserve workflow state before compaction |

### UserPromptSubmit (4 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_user_prompt_submit | NON-TERMINAL | Simple test handler that confirms UserPromptSubmit hook is working |
| 10 | git_context_injector | CONTEXT | Inject current git status as context when user submits a prompt |
| 54 | post_clear_auto_execute | ADVISORY | Inject execution guidance on the first prompt of a new session |
| 55 | critical_thinking_advisory | ADVISORY | Periodically inject advisory context encouraging critical evaluation |

### PermissionRequest (2 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_permission_request | NON-TERMINAL | Simple test handler that confirms PermissionRequest hook is working |
| 10 | auto_approve_reads | TERMINAL | Auto-approve read-only tool permission requests |

### Notification (2 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_notification | NON-TERMINAL | Simple test handler that confirms Notification hook is working |
| 100 | notification_logger | NON-TERMINAL | Log all notification events to a JSONL file |

### Stop (8 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_stop | NON-TERMINAL | Simple test handler that confirms Stop hook is working |
| 5 | hello_world_subagent_stop | NON-TERMINAL | Simple test handler that confirms SubagentStop hook is working |
| 15 | auto_continue_stop | TERMINAL | Intercept Stop events and enforce explicit stop reasons or auto-continue |
| 30 | hedging_language_detector | ADVISORY | Detect hedging language that signals guessing instead of researching |
| 50 | task_completion_checker | ADVISORY | Remind agent to verify task completion before stopping |
| 58 | dismissive_language_detector | ADVISORY | Detect dismissive language that signals avoiding work |
| 100 | remind_prompt_library | ADVISORY | Remind to capture successful prompts to the library |
| 100 | subagent_completion_logger | NON-TERMINAL | Log subagent completion events to a JSONL file |

### SubagentStop (3 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | hello_world_subagent_stop | NON-TERMINAL | Simple test handler that confirms SubagentStop hook is working |
| 10 | remind_prompt_library | ADVISORY | Remind to capture successful prompts to the library |
| 100 | subagent_completion_logger | NON-TERMINAL | Log subagent completion events to a JSONL file |

### Status (10 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 5 | account_display | NON-TERMINAL | Display Claude account username in status line |
| 5 | git_repo_name | NON-TERMINAL | Show git repository name at start of status line |
| 10 | model_context | NON-TERMINAL | Format model name with effort level and color-coded context percentage |
| 12 | thinking_mode | NON-TERMINAL | Display thinking mode and effort level in status line |
| 14 | current_time | NON-TERMINAL | Display current local time in status line (24-hour format, no seconds) |
| 15 | usage_tracking | NON-TERMINAL | Display daily and weekly token usage percentages |
| 20 | git_branch | NON-TERMINAL | Show current git branch if in a git repo |
| 25 | working_directory | NON-TERMINAL | Display working directory when it differs from project root |
| 28 | startup_cleanup | NON-TERMINAL | Show 🧹 briefly after daemon startup to indicate stale-file cleanup ran |
| 30 | daemon_stats | NON-TERMINAL | Show daemon health: uptime, memory, last error, log level |

### Plugin (2 handlers)

| Priority | Handler | Behavior | Description |
|----------|---------|----------|-------------|
| 8 | SystemPathsHandler | TERMINAL | Block Write/Edit operations on deployed system files |
| 10 | AnsibleEnforcementHandler | TERMINAL | Block direct system management commands - enforce Ansible deployment |

## Quick Config Reference

**Config file**: `.claude/hooks-daemon.yaml`
**Enable/disable**: Set `enabled: true/false` under handler name
**Handler options**: Set under `options:` key per handler

# Implementation Plans Directory

This directory contains implementation plans following the claude-code-hooks-daemon plan workflow.

## Plan Workflow

**IMPORTANT**: All non-trivial implementation work should follow the planning workflow documented in [PlanWorkflow.md](../PlanWorkflow.md).

### Quick Reference

Plans use numbered prefixes for sequential organization:

- `001-description/` - First plan
- `002-description/` - Second plan
- etc.

Each plan directory contains:

- `PLAN.md` - Main plan document with tasks, goals, and progress tracking
- Supporting files (implementation, tests, documentation)

### Task Status Icons

Use these Unicode icons in plan documents:

- ⬜ `TODO` - Not started
- ✅ `DONE` - Completed successfully
- 🔄 `IN_PROGRESS` - Currently working on
- 🚫 `BLOCKED` - Cannot proceed (dependency/issue)
- ❌ `FAILED` - Attempted but failed (requires rework)
- ⏸️ `PAUSED` - Temporarily suspended
- 👁️ `REVIEW` - Needs review/approval

## Active Plans

- [002-nordvpn-openvpn-manager](002-nordvpn-openvpn-manager/) - NordVPN OpenVPN connection manager
- [004-comprehensive-feature-documentation](004-comprehensive-feature-documentation/) - Documentation for all major features (CCY, CCB, Nord, Speech-to-Text, etc.)
- [007-speech-to-text-resource-leak-fixes](007-speech-to-text-resource-leak-fixes/) - Fix microphone resource leak, transcription truncation, and browser paste failures
- [009-claude-devtools](009-claude-devtools/) - Install and integrate claude-devtools session visualiser (implementation committed, pending host deployment and testing)
- [011-claude-devtools](011-claude-devtools/) - claude-devtools (ccdt) installation plan (supersedes 009)
- [013-claude-devtools](013-claude-devtools/) - claude-devtools (ccdt) installation plan (latest iteration)
- [014-whisper-model-manager](014-whisper-model-manager/) - Replace cluttered model dropdown with a dedicated Textual TUI (`wsi-model-manager`) for browsing and downloading Whisper models
- [020-semgrep-custom-bash-rules](020-semgrep-custom-bash-rules/) - Add Semgrep with custom bash convention rules (no error hiding, fail-fast enforcement) integrated into qa-all.bash
- [023-hostname-based-inventory](023-hostname-based-inventory/) - Migrate Ansible inventory from hardcoded `localhost` to machine hostname, supporting per-machine host_vars and multiple laptops
- [024-claude-md-modular-restructure](024-claude-md-modular-restructure/) - Restructure monolithic CLAUDE.md (40k+ chars) into modular architecture: lean front page + CLAUDE/ topic files + docs/ for user content + subdirectory stubs with @ pointers
- [025-ccy-spring-cleaning](025-ccy-spring-cleaning/) - CCY codebase spring cleaning: fix 63 shellcheck warnings, remove 20 dead functions, fix double-sourcing, exit-vs-return, and code quality issues
- [026-repo-spring-cleaning](026-repo-spring-cleaning/) - Repository-wide spring cleaning (non-CCY): remove tracked backups, fix bash scripts (set -e, shellcheck), fix Ansible playbooks (duplicate shebangs, curl-to-bash, state:latest)
- [027-contextual-shell-history](027-contextual-shell-history/) - Replace bash history with Atuin for directory/git-workspace-aware command recall
- [028-fedora-screen-sharing](028-fedora-screen-sharing/) - Diagnose and fix unstable screen sharing on Fedora 43 GNOME (Slack desktop broken by `app.asar` hardcode; Meet freezes traced to mutter ScreenCast bugs fixed in 49.3/49.5)
- [029-rapid-raw-cloud-ai](029-rapid-raw-cloud-ai/) - Evaluate cloud GPU paths for RapidRAW Tier 2 generative AI: free local-first verification (dGPU + Tier 1), local SD 1.5 prototype, vast.ai $10-credit prototype with SDXL/Flux Fill, then evidence-based decision gate before any productionisation
- [030-phpantom-lsp](030-phpantom-lsp/) - Research PHPantom (Rust-based PHP LSP) as replacement for Intelephense; decision gate before implementation
- [031-reliable-screen-sharing](031-reliable-screen-sharing/) - Find reliable screen-sharing alternatives for WFH devs on Fedora 43 Wayland (complements Plan 028); 4 parallel research tracks: self-hosted platforms, native Linux tools, current SaaS, unconventional approaches
- [032-compression-helpers](032-compression-helpers/) - `compress` / `uncompress` CLI wrappers around `ouch`: xz by default, `--zip` flag, auto-detect on decompress, always-extract-into-folder (tarbomb protection)
- [033-ddev-installation](033-ddev-installation/) - Install DDEV (Docker-based local dev environment) via yum repo + mkcert for local HTTPS

## Completed Plans

- None yet

## Cancelled Plans

- [012-fix-plugin-handlers](012-fix-plugin-handlers/) - Upstream bug in `claude-code-hooks-daemon`; bug report filed at `untracked/upstream-bug-report-plugin-handler-suffix.md`

## Archive

The `Archive/` directory contains legacy plans created before adopting the structured plan workflow:

- **ccb-browser-automation.md** - CCB browser automation implementation
- **ccyb.md** - CCY background service planning
- **speech-to-text.md** - Speech-to-text integration
- **workspace-names-overview.md** - Workspace naming conventions

These are preserved for reference but don't follow the current plan structure.

## Creating New Plans

See [PlanWorkflow.md](../PlanWorkflow.md) for complete instructions.

**Quick start:**

```bash
# Create new plan directory
mkdir -p CLAUDE/Plan/001-my-feature

# Copy plan template
cp CLAUDE/Plan/templates/PLAN-template.md CLAUDE/Plan/001-my-feature/PLAN.md

# Edit plan with goals, approach, and tasks
vim CLAUDE/Plan/001-my-feature/PLAN.md
```

## Plan Workflow Integration

The hooks daemon enforces plan workflow standards:

- ✅ `validate_plan_number` - Ensures correct numbering format
- ✅ `plan_time_estimates` - Blocks time estimates in plans
- ✅ `plan_workflow` - Provides guidance when creating plans
- ✅ `workflow_state_pre_compact` - Saves workflow state before compaction
- ✅ `workflow_state_restoration` - Restores state after compaction

## References

- [PlanWorkflow.md](../PlanWorkflow.md) - Complete plan workflow documentation
- [CLAUDE.md](../../CLAUDE.md) - Project-level Claude configuration

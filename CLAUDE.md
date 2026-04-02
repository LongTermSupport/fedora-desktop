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

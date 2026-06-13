---
name: hooks-daemon
description: Manage Claude Code Hooks Daemon - install, upgrade, check health, restart, and develop project-level handlers
argument-hint: "[install|upgrade|health|restart|dev-handlers|logs] [args...]"
disable-model-invocation: false
user-invocable: true
allowed-tools: Bash, Read, Write, Edit
---

# Hooks Daemon Management

Manage your Claude Code Hooks Daemon installation with these commands.

## Available Commands

### Install Daemon
Install the hooks daemon on a fresh clone (daemon not yet present):
```bash
/hooks-daemon install          # Install daemon from GitHub
/hooks-daemon install --force  # Force reinstall over existing
```

See [install.md](install.md) for detailed install documentation.

### Upgrade Daemon
Update to a new version of the hooks daemon:
```bash
/hooks-daemon upgrade          # Auto-detect and upgrade to latest version
/hooks-daemon upgrade 2.14.0   # Upgrade to specific version
/hooks-daemon upgrade --force  # Force reinstall current version
```

See [upgrade.md](upgrade.md) for detailed upgrade documentation.

### Restart Daemon
**Required after editing `.claude/hooks-daemon.yaml` or project handlers:**
```bash
/hooks-daemon restart
```

The daemon caches config at startup — restart picks up any config or handler changes.

See [restart.md](restart.md) for details.

### Check Health & Status
Verify daemon is running correctly:
```bash
/hooks-daemon health           # Quick health check
/hooks-daemon logs             # View last 50 lines of logs
/hooks-daemon logs --follow    # Stream logs in real-time
```

See [health.md](health.md) for health check details.

### Develop Project Handlers
Scaffold new project-level handlers:
```bash
/hooks-daemon dev-handlers     # Interactive handler scaffolding
```

See [dev-handlers.md](dev-handlers.md) for handler development guide.

### Investigate an Issue
Generate a detailed investigation report with timeline, evidence, and analysis:
```bash
/hooks-daemon report "daemon stopped responding during edits"
```

The report is saved to `./untracked/hooks-daemon-{description}.md` for sharing with maintainers.

See [report.md](report.md) for details.

## Quick Start

After editing `.claude/hooks-daemon.yaml`:
```bash
/hooks-daemon restart   # Apply config changes
/hooks-daemon health    # Verify it's running
```

If you're experiencing issues:
```bash
# 1. Check daemon status
/hooks-daemon health

# 2. View recent logs
/hooks-daemon logs

# 3. Generate a quick bug report with diagnostics
/hooks-daemon bug-report "description of the issue"

# 4. Generate a full investigation report with timeline
/hooks-daemon report "description of the issue"

# 5. Restart to recover
/hooks-daemon restart
```

## Troubleshooting

See [references/troubleshooting.md](references/troubleshooting.md) for common issues and solutions.

## Implementation

Parse subcommand and route to appropriate script:

```bash
# Get skill directory (where this SKILL.md is located)
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Parse subcommand from $ARGUMENTS
SUBCOMMAND="${1:-help}"
shift || true  # Remove subcommand from arguments

# Route to appropriate script
case "$SUBCOMMAND" in
    install)
        bash "$SKILL_DIR/scripts/install.sh" "$@"
        ;;

    upgrade)
        bash "$SKILL_DIR/scripts/upgrade.sh" "$@"
        ;;

    health)
        bash "$SKILL_DIR/scripts/health-check.sh" "$@"
        ;;

    dev-handlers)
        bash "$SKILL_DIR/scripts/init-handlers.sh" "$@"
        ;;

    report)
        # LLM-driven investigation report — outputs prompt for Claude to follow
        cat "$SKILL_DIR/report.md" | sed "s/\$ARGUMENTS/$*/"
        ;;

    logs|status|restart|handlers|validate-config|bug-report)
        # Forward to daemon CLI wrapper
        bash "$SKILL_DIR/scripts/daemon-cli.sh" "$SUBCOMMAND" "$@"
        ;;

    help|--help|-h|"")
        # Show help (this SKILL.md content)
        echo "Usage: /hooks-daemon <command> [args...]"
        echo ""
        echo "Available commands:"
        echo "  install [--force]     Install daemon (fresh clone)"
        echo "  restart               Restart daemon (required after config changes)"
        echo "  health                Check daemon health and status"
        echo "  upgrade [VERSION]     Upgrade daemon to new version"
        echo "  dev-handlers          Scaffold new project handlers"
        echo "  logs [--follow]       View daemon logs"
        echo "  status                Show daemon status"
        echo "  handlers              List loaded handlers"
        echo "  bug-report DESC       Generate bug report with diagnostics"
        echo "  report DESC           Investigate an issue and generate a detailed report"
        echo ""
        echo "After editing .claude/hooks-daemon.yaml, always run: /hooks-daemon restart"
        echo ""
        echo "For detailed documentation, see the skill files or run:"
        echo "  /hooks-daemon <command> --help"
        ;;

    *)
        echo "Error: Unknown subcommand: $SUBCOMMAND"
        echo ""
        echo "Usage: /hooks-daemon <command> [args...]"
        echo "Run '/hooks-daemon help' for available commands."
        exit 1
        ;;
esac
```

**Note**: All daemon management commands require manual user approval. The daemon will not auto-invoke these operations.

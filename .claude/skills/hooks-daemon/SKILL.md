---
name: hooks-daemon
description: Manage Claude Code Hooks Daemon - install, upgrade, check health, restart, run the housekeeping pass, and report issues
argument-hint: "[install|upgrade|optimise|housekeeping|restart|health|bug-report|report] [args...]"
disable-model-invocation: false
user-invocable: true
allowed-tools: Bash, Read, Write, Edit
---

# Hooks Daemon Management

Manage your Claude Code Hooks Daemon installation with these commands.

The routed surface is deliberately small (Plan 00330): a subcommand exists
only for something a human types. Everything else the daemon can do is a
CLI verb, listed under [Capabilities](#capabilities-cli-verbs) below with the
exact command — the same verb agents already run directly.

## Available Commands

### Install Daemon

Install the hooks daemon on a fresh clone (daemon not yet present):

```claude-code
/hooks-daemon install          # Install daemon from GitHub
/hooks-daemon install --force  # Force reinstall over existing
```

See [install.md](install.md) for detailed install documentation.

### Upgrade Daemon

Update to a new version of the hooks daemon:

```claude-code
/hooks-daemon upgrade          # Auto-detect and upgrade to latest version
/hooks-daemon upgrade 2.14.0   # Upgrade to specific version
/hooks-daemon upgrade --force  # Force reinstall current version
```

See [upgrade.md](upgrade.md) for detailed upgrade documentation.

### Optimise Configuration

The config-optimisation review — the mandatory closing step of every upgrade,
and the repeatable answer to "enable all relevant handlers and ensure optimal
configuration for this project":

```claude-code
/hooks-daemon optimise
```

Scores every registered handler across six derived areas, surfaces handlers
that are new or disabled-but-relevant, reports the inapplicable ones as such,
and applies its recommendations only on explicit confirmation.

See [optimise.md](optimise.md) — it starts by running
`scripts/optimise-invoke.sh`, which prints the procedure to follow.

### Housekeeping Pass

One invocation for the whole housekeeping pass — plan QA, docs QA, the
daemon's own audits, the formatters, and `optimise` to close:

```claude-code
/hooks-daemon housekeeping                     # report everything; formatters act
/hooks-daemon housekeeping --apply prune-venvs # also release one held step
/hooks-daemon housekeeping --list              # the steps, in order
```

Report-only steps run first, in parallel, one sub-agent each; mutating steps
follow in a fixed order with `optimise` last because it restarts the daemon.
Only `format-markdown` and `regenerate-docs` act without confirmation — every
other mutating step is HELD and acts only when named on `--apply`. Each
sub-agent reports what it CHANGED, never what it read.

See [housekeeping.md](housekeeping.md) for the step list and the sub-agent
contract.

### Restart Daemon

**Required after editing `.claude/hooks-daemon.yaml` or project handlers:**

```claude-code
/hooks-daemon restart
```

The daemon caches config at startup — restart picks up any config or handler changes.

See [restart.md](restart.md) for details.

### Check Health & Status

Verify daemon is running correctly:

```claude-code
/hooks-daemon health           # Quick health check
```

See [health.md](health.md) for health check details, including where the logs
and the verbose environment audit are.

### Report an Issue

Two different actions — pick by what you need:

```claude-code
/hooks-daemon bug-report "description of the issue"   # diagnostic bundle for maintainers
/hooks-daemon report "daemon stopped responding"       # LLM-driven investigation with a timeline
```

`bug-report` is fast and mechanical: version, status, config, handlers, recent
logs and a health checklist, written to `untracked/bug-reports/`. `report` is
an investigation: it collects evidence, builds a timeline, and writes a
narrative to `./untracked/hooks-daemon-{description}.md`. Reach for
`bug-report` first; use `report` when the bug-report was not enough to explain
what happened.

See [bug-report.md](bug-report.md) and [report.md](report.md).

## Capabilities (CLI verbs)

These are things the daemon does that nobody types as a skill subcommand, so
they are not routed. Run the verb directly (on a self-install the wrapper is
`bin/hooks-daemon` at the repository root):

```bash
.claude/hooks-daemon/bin/hooks-daemon logs               # last 50 log lines (--follow to stream)
.claude/hooks-daemon/bin/hooks-daemon status             # one-line daemon status
.claude/hooks-daemon/bin/hooks-daemon handlers           # every loaded handler with its priority
.claude/hooks-daemon/bin/hooks-daemon config-validate    # validate the project config (validate-config also works)
.claude/hooks-daemon/bin/hooks-daemon check              # verbose environment & configuration audit
.claude/hooks-daemon/bin/hooks-daemon regenerate-docs    # rewrite HOOKS-DAEMON.md + the CLAUDE.md block, no restart
.claude/hooks-daemon/bin/hooks-daemon explain-rule R-GIT-RESET-HARD   # full detail for a rule (--list for every ID)
.claude/hooks-daemon/bin/hooks-daemon explain-handler destructive_git # a handler's rules + guidance
.claude/hooks-daemon/bin/hooks-daemon init-project-handlers           # scaffold project-level handlers
.claude/hooks-daemon/bin/hooks-daemon release-notes      # installed version's notes (--latest, --version, --list)
.claude/hooks-daemon/bin/hooks-daemon plan-qa --sweep    # plan-tree drift (--lint <PLAN.md>, --check-staged)
.claude/hooks-daemon/bin/hooks-daemon housekeeping --list # the housekeeping pass, step by step
```

Detail per capability: [check.md](check.md), [regen-docs.md](regen-docs.md),
[rule-explain.md](rule-explain.md), [dev-handlers.md](dev-handlers.md),
[plan-qa.md](plan-qa.md). `bin/hooks-daemon --help` lists every verb.

## Quick Start

After editing `.claude/hooks-daemon.yaml`:

```claude-code
/hooks-daemon restart   # Apply config changes
/hooks-daemon health    # Verify it's running
```

If you're experiencing issues:

```claude-code
# 1. Check daemon health
/hooks-daemon health

# 2. View recent logs
.claude/hooks-daemon/bin/hooks-daemon logs

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

    optimise|optimize)
        # Prints the review procedure for Claude to follow (like `report`).
        # `optimize` is accepted so the US spelling does not hit the
        # unknown-subcommand branch.
        bash "$SKILL_DIR/scripts/optimise-invoke.sh" "$@"
        ;;

    housekeeping)
        # Prints the full-pass procedure (Plan 00330); every step is then
        # delegated to a sub-agent. --apply <step> releases a held step.
        bash "$SKILL_DIR/scripts/daemon-cli.sh" housekeeping "$@"
        ;;

    report)
        # LLM-driven investigation report — outputs prompt for Claude to follow,
        # with the human's description standing in for report.md's $ARGUMENTS
        # placeholder.
        #
        # Bash parameter expansion substitutes LITERALLY, so the description is
        # data: no character in it can terminate the replacement or be read as
        # a further command. Handing it to a stream editor instead made every
        # character syntax — a `/` (a file path in the description) ended the
        # replacement and the remainder was parsed as more editor commands.
        REPORT_PROMPT="$(cat "$SKILL_DIR/report.md")"
        printf '%s\n' "${REPORT_PROMPT//\$ARGUMENTS/$*}"
        ;;

    restart|bug-report)
        # Forward to daemon CLI wrapper.
        bash "$SKILL_DIR/scripts/daemon-cli.sh" "$SUBCOMMAND" "$@"
        ;;

    help|--help|-h|"")
        # Show help (this SKILL.md content)
        echo "Usage: /hooks-daemon <command> [args...]"
        echo ""
        echo "Available commands:"
        echo "  install [--force]     Install daemon (fresh clone)"
        echo "  upgrade [VERSION]     Upgrade daemon to new version"
        echo "  optimise              Config-optimisation review (closes every upgrade)"
        echo "  housekeeping [--apply STEP] [--list]"
        echo "                        Full housekeeping pass: reports first, held steps on request, optimise last"
        echo "  restart               Restart daemon (required after config changes)"
        echo "  health                Check daemon health and status"
        echo "  bug-report DESC       Diagnostic bundle for maintainers"
        echo "  report DESC           LLM-driven investigation report with a timeline"
        echo ""
        echo "After editing .claude/hooks-daemon.yaml, always run: /hooks-daemon restart"
        echo ""
        echo "Everything else is a CLI verb: .claude/hooks-daemon/bin/hooks-daemon --help"
        echo "(logs, status, handlers, config-validate, check, regenerate-docs, explain-rule,"
        echo " init-project-handlers, release-notes, plan-qa ...)"
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

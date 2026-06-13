#!/bin/bash
#
# install.sh - Install hooks daemon into current project
#
# Usage:
#   ./install.sh [--force]
#
# Arguments:
#   --force (optional): Force reinstall over existing installation
#

set -euo pipefail

GITHUB_ORG="Edmonds-Commerce-Limited"
GITHUB_REPO="claude-code-hooks-daemon"
INSTALL_URL="https://raw.githubusercontent.com/${GITHUB_ORG}/${GITHUB_REPO}/main/install.sh"
# Plan 00100 Task 0.3: daemon's requires-python is the single source of truth
# for minimum Python. Fetched from the repo's pyproject.toml below and used
# to pre-check the active python3 BEFORE the installer runs.
PYPROJECT_URL="https://raw.githubusercontent.com/${GITHUB_ORG}/${GITHUB_REPO}/main/pyproject.toml"
# Plan 00110 Task 4.3: canonical glob-and-sort interpreter discovery helper.
# Fetched alongside pyproject.toml so the skill bootstrap can pick the latest
# compatible python3.NN on $PATH — no hardcoded version list, no bl-admin
# "suggests python3.11 even though python3.13/3.14 are installed" trap.
PYTHON_DISCOVERY_URL="https://raw.githubusercontent.com/${GITHUB_ORG}/${GITHUB_REPO}/main/scripts/lib/python_discovery.sh"

# Detect project root by searching upward for .claude/
PROJECT_ROOT="$(pwd)"
while [ "$PROJECT_ROOT" != "/" ]; do
    if [ -d "$PROJECT_ROOT/.claude" ]; then
        break
    fi
    PROJECT_ROOT="$(dirname "$PROJECT_ROOT")"
done

if [ ! -d "$PROJECT_ROOT/.claude" ]; then
    echo "Error: Not in a Claude Code project (no .claude/ directory found)"
    echo ""
    echo "The hooks daemon must be installed in a project that has Claude Code configured."
    echo "Ensure you are in a project directory with a .claude/ folder."
    exit 1
fi

DAEMON_DIR="$PROJECT_ROOT/.claude/hooks-daemon"
FORCE_FLAG="${1:-}"

echo "Claude Code Hooks Daemon - Install"
echo ""
echo "Project: $PROJECT_ROOT"
echo ""

# Plan 00100 Task 0.3 + Plan 00110 Task 4.3: Python pre-check BEFORE download.
# Fetch two artifacts from main: (1) pyproject.toml for requires-python (the
# floor), (2) scripts/lib/python_discovery.sh — the canonical glob-and-sort
# interpreter discovery helper. The helper walks $PATH for python3.NN, picks
# the latest meeting the floor, and on failure emits a diagnostic naming
# interpreters ACTUALLY observed on this host (never a hardcoded suggestion
# that may not exist — the bl-admin trap that Plan 00110 closes).
PYPROJECT_TMP="/tmp/hooks-daemon-precheck-pyproject.toml.$$"
DISCOVERY_TMP="/tmp/hooks-daemon-precheck-python-discovery.sh.$$"
trap 'rm -f "$PYPROJECT_TMP" "$DISCOVERY_TMP"' EXIT

if ! curl -sSL "$PYPROJECT_URL" -o "$PYPROJECT_TMP" || [ ! -s "$PYPROJECT_TMP" ]; then
    echo "Error: Failed to fetch pyproject.toml from $PYPROJECT_URL"
    echo "Check your network connection and try again."
    exit 1
fi
if ! curl -sSL "$PYTHON_DISCOVERY_URL" -o "$DISCOVERY_TMP" || [ ! -s "$DISCOVERY_TMP" ]; then
    echo "Error: Failed to fetch python_discovery.sh from $PYTHON_DISCOVERY_URL"
    echo "Check your network connection and try again."
    exit 1
fi

MIN_PY="$(grep -E '^requires-python[[:space:]]*=' "$PYPROJECT_TMP" | grep -oE '[0-9]+\.[0-9]+' | head -n 1)"
if [ -z "$MIN_PY" ]; then
    echo "Error: Could not parse requires-python from pyproject.toml"
    exit 1
fi

# shellcheck source=/dev/null
. "$DISCOVERY_TMP"
if ! FOUND_PY="$(find_latest_python "$MIN_PY" "$PYPROJECT_TMP")"; then
    # Helper already wrote a remediation hint to stderr enumerating every
    # interpreter observed during the glob. Add a one-line summary line and
    # exit — no second guessing of the helper's diagnostic.
    echo ""
    echo "Aborting install: no compatible Python (>=$MIN_PY) found on \$PATH."
    exit 1
fi

# Export so the inner installer (downloaded below) reuses the discovered
# interpreter without re-running discovery on its own. The downstream
# scripts/install/prerequisites.sh also honours HOOKS_DAEMON_PYTHON via
# the same helper (Plan 00110 Task 4.2).
export HOOKS_DAEMON_PYTHON="$FOUND_PY"
echo "Using Python: $FOUND_PY"
echo ""

# Plan 00122 BUG 3: an install counts as "already installed" only if it is
# HEALTHY — a venv python exists AND the daemon package imports. A bare
# directory check treated a broken/partial install (dir present, no working
# venv) as complete, so the documented `/hooks-daemon install` could not repair
# it (only `--force` could, which re-clones from scratch). Returns 0 when
# healthy, non-zero otherwise. Probe output goes to a temp file (never
# /dev/null) so failures stay inspectable.
_installation_is_healthy() {
    local dir="$1"
    local py probe_out
    probe_out="$(mktemp)"
    # canonical-resolver-exempt: this is the skill bootstrap, which runs
    # standalone in a client project BEFORE the daemon source tree (and
    # scripts/lib/resolve_venv.sh) exists. This is a lightweight health probe,
    # not venv resolution for use, so it globs directly.
    for py in "$dir"/untracked/venv-*/bin/python "$dir"/untracked/venv/bin/python; do
        if [ -x "$py" ] && "$py" -c "import claude_code_hooks_daemon" > "$probe_out" 2> "$probe_out"; then
            rm -f "$probe_out"
            return 0
        fi
    done
    rm -f "$probe_out"
    return 1
}

# Check if already installed
if [ -d "$DAEMON_DIR" ] && [ "$FORCE_FLAG" != "--force" ]; then
    if _installation_is_healthy "$DAEMON_DIR"; then
        echo "Daemon is already installed at: $DAEMON_DIR"
        echo ""
        echo "To upgrade to a new version:"
        echo "  /hooks-daemon upgrade"
        echo ""
        echo "To force reinstall:"
        echo "  /hooks-daemon install --force"
        exit 0
    fi
    echo "Daemon directory exists at: $DAEMON_DIR"
    echo "but the installation looks broken (venv missing or package not importable)."
    echo "Repairing now (equivalent to --force)..."
    echo ""
    FORCE_FLAG="--force"
fi

# Download installer to temp file (never pipe curl to shell — we block that pattern)
INSTALLER="/tmp/hooks-daemon-install.sh"
echo "Downloading installer..."
echo "  URL: $INSTALL_URL"
curl -sSL "$INSTALL_URL" -o "$INSTALLER"

if [ ! -s "$INSTALLER" ]; then
    echo ""
    echo "Error: Failed to download installer (empty file)"
    echo "Check your network connection and try again."
    exit 1
fi

INSTALLER_SIZE=$(wc -c < "$INSTALLER")
echo "  Downloaded: ${INSTALLER_SIZE} bytes"
echo ""

# Run installer from project root
echo "Running installer..."
echo ""
cd "$PROJECT_ROOT"

if [ "$FORCE_FLAG" = "--force" ]; then
    FORCE=true bash "$INSTALLER"
else
    bash "$INSTALLER"
fi

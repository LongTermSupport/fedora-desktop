#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See CLAUDE/LLM-INSTALL.md, "Which Files Under
# .claude/ Are Yours?", for the full list and the linter exclusions.
#
# Claude Code Hooks Daemon - Provision a fresh checkout (Plan 00477)
#
# A fresh clone of a project that uses the daemon carries the tracked assets
# (hook forwarders, init.sh, settings.json, hooks-daemon.yaml) but not the
# daemon itself: .claude/hooks-daemon/ is gitignored and per-checkout. This
# script builds that local part, at EXACTLY the version the project names, and
# starts the daemon. It is tracked so that it exists before the daemon does.
#
# Usage:
#   bash .claude/provision.sh
#
# What it does:
#   1. Resolves the expected version: daemon.expected_version in
#      .claude/hooks-daemon.yaml, else the .claude/HOOKS-DAEMON.md header.
#      Unknown or malformed: it stops and says so. It never guesses, and it
#      never installs main.
#   2. Fetches that one TAG (vX.Y.Z, depth 1) into .claude/hooks-daemon/.
#   3. Builds the venv through the clone own venv build path and lock
#      (scripts/venv_bootstrap.sh repair).
#   4. Starts the daemon through init.sh, as a hook would.
#
# What it never does:
#   - write a tracked file (everything it creates is under the gitignored
#     .claude/hooks-daemon/);
#   - take the clone URL from the environment or from project config: the URL
#     is the constant below, the same one install.sh trusts;
#   - keep a fetched tag whose version.py does not name the version asked for;
#   - replace a clone that is already there (that is upgrade, or repair).
#
# Hooks never run this. A person or an agent runs it, deliberately.
#

set -euo pipefail

# The trusted clone source. Keep equal to DAEMON_REPO in install.sh (a test
# asserts it). Deliberately not overridable.
readonly PROVISION_REPO_URL="https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon.git"

_provision_say() {
    printf '%s\n' "$*"
}

_provision_fail() {
    printf 'PROVISION FAILED: %s\n' "$1" >&2
    shift
    local line
    for line in "$@"; do
        printf '  %s\n' "$line" >&2
    done
    exit 1
}

_PROVISION_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_PROVISION_PROJECT_ROOT="$(dirname "$_PROVISION_SCRIPT_DIR")"

[[ "$(basename "$_PROVISION_SCRIPT_DIR")" == ".claude" ]] ||
    _provision_fail "this script must live in the project .claude/ directory (found in $_PROVISION_SCRIPT_DIR)"

# The daemon own repository is set up by scripts/bootstrap-self-install.sh, and
# sourcing init.sh there would report the unconfigured repository instead.
if [[ -f "$_PROVISION_PROJECT_ROOT/src/claude_code_hooks_daemon/version.py" ]]; then
    _provision_fail "this is the hooks-daemon repository itself, not a project that uses it." \
        "Set it up for development with: scripts/bootstrap-self-install.sh"
fi

[[ -f "$_PROVISION_SCRIPT_DIR/init.sh" ]] ||
    _provision_fail "$_PROVISION_SCRIPT_DIR/init.sh is missing." \
        "It is a tracked file of the project; restore it from git, then run this again."

command -v git > /dev/null ||
    _provision_fail "git is not installed." "Install git, then run this again."

# init.sh supplies the ONE version resolver and the clone-state probes. Sourcing
# it creates the empty .claude/hooks-daemon/untracked/ skeleton, which is
# expected and handled below.
# shellcheck source=init.sh
source "$_PROVISION_SCRIPT_DIR/init.sh"

DAEMON_DIR="$HOOKS_DAEMON_ROOT_DIR"
if [[ "$DAEMON_DIR" != "$PROJECT_PATH/.claude/hooks-daemon" ]]; then
    _provision_fail "the daemon root is overridden to $DAEMON_DIR (HOOKS_DAEMON_ROOT_DIR)." \
        "Provision only builds the standard location, $PROJECT_PATH/.claude/hooks-daemon."
fi

# ------------------------------------------------------------------
# Step 1: which version?
# ------------------------------------------------------------------
if ! _resolve_expected_version; then
    if [[ "$_HOOKS_DAEMON_EXPECTED_VERSION_SOURCE" == "config-invalid" ]]; then
        _provision_fail "daemon.expected_version in .claude/hooks-daemon.yaml is not X.Y.Z." \
            "It must be a released version such as 3.68.0 (no v prefix, no branch name)." \
            "Correct it in that file, commit it, then run this again."
    fi
    _provision_fail "the daemon version this project expects is unknown." \
        "Neither daemon.expected_version in .claude/hooks-daemon.yaml nor the" \
        "  .claude/HOOKS-DAEMON.md header names one, and provision will not guess." \
        "Ask whoever maintains the project, or set daemon.expected_version to the" \
        "  released version the project was last upgraded to, commit it, and run this again."
fi
VERSION="$_HOOKS_DAEMON_EXPECTED_VERSION"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
    _provision_fail "refusing to use $VERSION as a git tag: it is not X.Y.Z."
TAG="v$VERSION"

_provision_say "Provisioning the hooks daemon $TAG for this checkout (version from $_HOOKS_DAEMON_EXPECTED_VERSION_SOURCE)."

# ------------------------------------------------------------------
# Step 2: is this a fresh checkout?
# ------------------------------------------------------------------
if _daemon_clone_present; then
    # The resolved interpreter must also exist on disk, so it is judged as well
    # as the status of _resolve_python_cmd.
    PYTHON_CMD=""
    if ! _resolve_python_cmd; then
        PYTHON_CMD=""
    fi
    if [[ -n "$PYTHON_CMD" && -f "$PYTHON_CMD" ]]; then
        PRESENT_VERSION=""
        if ! PRESENT_VERSION="$(_clone_version)"; then
            PRESENT_VERSION="unknown"
        fi
        _provision_fail "a daemon clone is already provisioned here (version $PRESENT_VERSION)." \
            "Provision only builds a clone that is missing; it never replaces one." \
            "To change version, use: /hooks-daemon upgrade"
    fi
    _provision_fail "a daemon clone is already here, but no venv resolves for this project path." \
        "That is a different repair, and it keeps the clone: run" \
        "  $DAEMON_DIR/bin/hooks-daemon repair" \
        "If the clone is at the wrong version, use: /hooks-daemon upgrade"
fi

# Anything in the daemon directory besides the untracked/ skeleton (or a venv
# under it) is someone work, or a clone too damaged to recognise. Neither is
# ours to overwrite.
_provision_unexpected_entries() {
    local entry
    shopt -s nullglob dotglob
    for entry in "$DAEMON_DIR"/*; do
        [[ "$(basename "$entry")" == "untracked" ]] && continue
        printf '%s\n' "$entry"
    done
    shopt -u nullglob dotglob
}
_PROVISION_UNEXPECTED="$(_provision_unexpected_entries)"
if [[ -n "$_PROVISION_UNEXPECTED" ]] || _daemon_orphan_venv_present; then
    _provision_fail "$DAEMON_DIR is not empty, and is not a clone provision recognises." \
        "Provision never overwrites what it did not create. Inspect it, move it aside" \
        "  if it is disposable, and run this again."
fi

# ------------------------------------------------------------------
# Step 3: fetch exactly the tag
# ------------------------------------------------------------------
# init + fetch rather than clone: the directory already holds the untracked/
# skeleton init.sh made, and clone refuses a non-empty directory. The result is
# the same as a depth 1 clone of the tag: a detached checkout with origin set.
# Removing what this step made is safe because the check above established that
# nothing but the skeleton was there before it.
_provision_abort_clone() {
    local entry
    shopt -s nullglob dotglob
    for entry in "$DAEMON_DIR"/*; do
        [[ "$(basename "$entry")" == "untracked" ]] && continue
        rm -rf "$entry"
    done
    shopt -u nullglob dotglob
}

_provision_say "Fetching $TAG from $PROVISION_REPO_URL ..."
mkdir -p "$DAEMON_DIR"
git -C "$DAEMON_DIR" -c init.defaultBranch=main init -q
git -C "$DAEMON_DIR" remote add origin "$PROVISION_REPO_URL"
if ! git -C "$DAEMON_DIR" fetch -q --depth 1 origin "refs/tags/$TAG:refs/tags/$TAG"; then
    _provision_abort_clone
    _provision_fail "could not fetch tag $TAG from $PROVISION_REPO_URL." \
        "Check that $TAG is a released version and that this machine can reach the repository." \
        "Nothing was installed."
fi
if ! git -C "$DAEMON_DIR" -c advice.detachedHead=false checkout -q "refs/tags/$TAG"; then
    _provision_abort_clone
    _provision_fail "could not check out tag $TAG." "Nothing was installed."
fi

CLONE_VERSION=""
if ! CLONE_VERSION="$(_clone_version)"; then
    CLONE_VERSION=""
fi
if [[ "$CLONE_VERSION" != "$VERSION" ]]; then
    _provision_abort_clone
    _provision_fail "tag $TAG carries daemon version ${CLONE_VERSION:-unreadable}, not $VERSION." \
        "Nothing was installed. Report this: a release tag should name its own version."
fi

# ------------------------------------------------------------------
# Step 4: the venv, through the clone own build path and lock
# ------------------------------------------------------------------
BOOTSTRAP="$DAEMON_DIR/scripts/venv_bootstrap.sh"
if [[ ! -f "$BOOTSTRAP" ]]; then
    _provision_abort_clone
    _provision_fail "$TAG predates the venv build driver (scripts/venv_bootstrap.sh)." \
        "Nothing was installed. Set daemon.expected_version to a newer release, or use /hooks-daemon upgrade."
fi
_provision_say "Building the venv (this can take a while on a cold cache) ..."
if ! bash "$BOOTSTRAP" repair "$DAEMON_DIR"; then
    _provision_fail "the clone is in place at $TAG but its venv could not be built." \
        "The output above says which condition failed. Once fixed, run:" \
        "  $DAEMON_DIR/bin/hooks-daemon repair" \
        "No tracked file was changed."
fi

# ------------------------------------------------------------------
# Step 5: start the daemon, the way a hook would
# ------------------------------------------------------------------
# start_daemon waits until a deadline measured from the start of the script,
# sized for a hook. A build before it would have used that budget up.
_HOOKS_DAEMON_START_DEADLINE=$((SECONDS + 60))
PYTHON_CMD=""
if ! start_daemon; then
    _provision_fail "the clone and venv are in place at $TAG but the daemon did not start." \
        "Try: $DAEMON_DIR/bin/hooks-daemon restart" \
        "Then: $DAEMON_DIR/bin/hooks-daemon logs"
fi

_provision_say ""
_provision_say "PROVISIONED: hooks daemon $TAG is running for this checkout."
_provision_say "No tracked file was changed."
SETTINGS="$PROJECT_PATH/.claude/settings.json"
if [[ -f "$SETTINGS" ]] && grep -q '\.claude/hooks/' "$SETTINGS"; then
    _provision_say "No session restart is needed: the hooks are already registered in"
    _provision_say ".claude/settings.json, and the next tool call reaches the running daemon."
else
    _provision_say "WARNING: .claude/settings.json does not register the hooks, so nothing will"
    _provision_say "call this daemon yet. That file is tracked and belongs to the project;"
    _provision_say "restore it from git or ask whoever maintains the project."
fi

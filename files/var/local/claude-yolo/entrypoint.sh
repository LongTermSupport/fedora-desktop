#!/bin/bash
# Claude Code YOLO Container Entrypoint
# In rootless Docker, UID 0 = host user, so this is safe
# IMPORTANT: Uses ccy-specific tokens from ~/.claude-tokens/ccy/ (NOT desktop tokens)

set -e

# Enable debug mode if requested (for entrypoint layer only)
if [ "$DEBUG_ENTRYPOINT" = "true" ]; then
    set -x
fi

# Verify GH_TOKEN is set
if [ -z "$GH_TOKEN" ]; then
    echo "ERROR: GH_TOKEN environment variable not set" >&2
    exit 1
fi

# Note: Claude Code uses /workspace/.claude/ for project-level state
# (settings, history, todos, etc.) - this is part of the workspace mount
# We only need to set up git, gh CLI, and SSH

# Configure git
if [ -f /tmp/claude-config-import/gitconfig ]; then
    cp /tmp/claude-config-import/gitconfig ~/.gitconfig
fi

# Configure GitHub CLI with token
mkdir -p ~/.config/gh
TEMP_TOKEN="$GH_TOKEN"
unset GH_TOKEN

if ! echo "$TEMP_TOKEN" | gh auth login --with-token 2>&1; then
    echo "ERROR: gh auth login failed" >&2
    exit 1
fi

# Verify the authenticated account matches the expected GitHub username
if [ -n "$GITHUB_USERNAME" ]; then
    AUTHENTICATED_USER=$(gh api user --jq .login 2>/dev/null)
    if [ "$AUTHENTICATED_USER" != "$GITHUB_USERNAME" ]; then
        echo "ERROR: Token authentication mismatch" >&2
        echo "Expected: $GITHUB_USERNAME" >&2
        echo "Got: $AUTHENTICATED_USER" >&2
        echo "" >&2
        echo "This means the gh-token-<alias> function on the host returned the wrong token." >&2
        echo "Please ensure play-github-cli-multi.yml is properly configured." >&2
        exit 1
    fi
    echo "✓ Authenticated as GitHub account: $GITHUB_USERNAME"
fi

if ! gh auth status 2>&1; then
    echo "ERROR: GitHub CLI authentication failed" >&2
    exit 1
fi

# Configure SSH for git operations if keys provided
if [ -n "$SSH_KEY_PATHS" ]; then
    eval "$(ssh-agent -s)" > /dev/null 2>&1

    IFS=: read -ra KEYS <<< "$SSH_KEY_PATHS"
    for key in "${KEYS[@]}"; do
        if ! ssh-add "$key" 2>&1; then
            echo "ERROR: Failed to add SSH key: $key" >&2
            exit 1
        fi
    done
else
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "⚠  WARNING: Running without SSH keys"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Git push operations will NOT work."
    echo ""
    echo "To add SSH keys, use one of these methods:"
    echo ""
    echo "  1. Use github_ keys (recommended):"
    echo "     ccy --ssh-key ~/.ssh/github_<alias>"
    echo ""
    echo "     Set up github_ keys with:"
    echo "     ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
    echo ""
    echo "  2. Use existing SSH key:"
    echo "     ccy --ssh-key ~/.ssh/id_ed25519"
    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
fi

# Add GitHub host keys to avoid SSH verification prompts on in-container git ops.
# CCY-08/BSH-16: capture the fetch explicitly instead of piping straight into
# known_hosts. If it fails (offline build-cache reuse, API hiccup), an empty
# known_hosts would make the first `git push` hang on an interactive host-key
# prompt — so fall back to StrictHostKeyChecking=accept-new and report which
# path was taken, rather than silently continuing (fail-fast visibility).
mkdir -p ~/.ssh
chmod 700 ~/.ssh

# Build the `Host github.com` directives the container needs. In --github-443
# mode (GITHUB_SSH_443=1, set by the wrapper) rewrite the endpoint to
# ssh.github.com:443 — used when the host/container network firewalls port 22.
# ssh.github.com:443 serves the SAME host keys as github.com:22, so this is a
# transparent endpoint swap, not a separate identity.
github_ssh_directives=()
if [ "${GITHUB_SSH_443:-0}" = "1" ]; then
    github_ssh_directives+=("    HostName ssh.github.com" "    Port 443" "    User git")
    echo "✓ GitHub SSH routed over ssh.github.com:443 (--github-443)"
fi

github_meta=$(curl -sL --max-time 5 https://api.github.com/meta 2>/dev/null) || github_meta=""
github_ssh_keys=""
if [ -n "$github_meta" ]; then
    github_ssh_keys=$(echo "$github_meta" | jq -r '.ssh_keys | .[]' 2>/dev/null) || github_ssh_keys=""
fi

if [ -n "$github_ssh_keys" ]; then
    # Pin the fetched keys. In 443 mode also pin them under [ssh.github.com]:443 —
    # the known_hosts lookup key SSH uses once HostName/Port are rewritten — so the
    # first push does not hang on an interactive host-key prompt.
    while IFS= read -r ghkey; do
        [ -n "$ghkey" ] || continue
        echo "github.com $ghkey"
        if [ "${GITHUB_SSH_443:-0}" = "1" ]; then
            echo "[ssh.github.com]:443 $ghkey"
        fi
    done <<< "$github_ssh_keys" >> ~/.ssh/known_hosts
    chmod 600 ~/.ssh/known_hosts
    echo "✓ GitHub SSH host keys pinned in known_hosts"
else
    echo "⚠ Could not fetch GitHub SSH host keys (offline?) — using StrictHostKeyChecking=accept-new for git/ssh" >&2
    github_ssh_directives+=("    StrictHostKeyChecking accept-new")
fi

# Write the github.com config stanza if any directives were collected (the 443
# endpoint rewrite and/or the offline accept-new fallback).
if [ "${#github_ssh_directives[@]}" -gt 0 ]; then
    {
        echo "Host github.com"
        printf '%s\n' "${github_ssh_directives[@]}"
    } >> ~/.ssh/config
    chmod 600 ~/.ssh/config
fi

# Set sandbox mode to bypass root detection
export IS_SANDBOX=1

# Disable Ink's hardcoded ctrl+z suspend handler (patched in Dockerfile to check this env var).
# Without this, ctrl+z sends unblockable SIGSTOP to the process - unrecoverable in a container.
export CCY_DISABLE_SUSPEND=1

# CCY-07: the build-time ctrl+z patch is best-effort. If it soft-failed it leaves
# a sentinel — surface that here (once, at launch) so a container that freezes on
# ctrl+z is diagnosable rather than mysterious.
if [ -f /opt/claude-yolo/.ctrlz-patch-status ] && [ "$(cat /opt/claude-yolo/.ctrlz-patch-status)" = "failed" ]; then
    echo "⚠ ctrl+z suspend patch did NOT apply when this image was built — ctrl+z may freeze the container." >&2
    echo "  Update ccy-ctrl-z-patch.js knownPatterns and rebuild (see CLAUDE/ContainerRules.md)." >&2
fi

# Disable mouse capture so native terminal selection works.
# NOTE: CLAUDE_CODE_NO_FLICKER (fullscreen / alt-screen rendering) was previously
# set here but has been REMOVED — see CLAUDE/Plan/00047/DECISION.md. The short
# version: fullscreen mode uses the terminal's alt-screen buffer, which on
# Wayland (kitty, GNOME-Terminal, etc.) breaks mouse-wheel scroll because the
# wheel-to-PageUp emulator remap is impossible without re-enabling mouse
# tracking (which in turn would break native click-drag selection). Reverting
# to the classic in-band renderer means kitty's native scrollback IS the
# conversation; wheel/touchpad scroll, click-drag selection, and Ctrl+F search
# all work natively. Trade-off: minor flicker on big re-renders.
#
# CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN is the kill-switch that forces the
# classic in-band renderer regardless of any persisted "tui": "fullscreen"
# setting in ~/.claude/settings.json (which CCY symlinks to
# /workspace/.claude/ccy/settings.json). Just unsetting CLAUDE_CODE_NO_FLICKER
# is NOT sufficient — if anyone ever ran `/tui fullscreen` in a prior CC
# session, that setting is sticky and the env-var absence does not override
# it. Setting DISABLE_ALTERNATE_SCREEN makes the result independent of
# persisted state.
# See: https://docs.anthropic.com/en/docs/claude-code/fullscreen
export CLAUDE_CODE_DISABLE_MOUSE=1
export CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1

# Symlink /root/.claude to /workspace/.claude/ccy for project-local session storage
# This keeps containers ephemeral while persisting sessions in the project directory
mkdir -p /workspace/.claude/ccy

# Remove /root/.claude if it exists (Claude Code might create it before entrypoint runs)
# Then create symlink to project directory
if [ -e /root/.claude ]; then
    if [ ! -L /root/.claude ]; then
        # It's not a symlink, remove it (directory or file)
        rm -rf /root/.claude
    fi
fi
ln -sf /workspace/.claude/ccy /root/.claude


# Ensure Claude Code settings have LSP enabled (non-destructive: preserves existing settings)
# Language servers are pre-installed in the image; this flag activates the LSP tool.
# PHPantom LSP plugin is enabled by default; Intelephense available as fallback.
# To switch PHP LSP: change enabledPlugins in settings.json
#   PHPantom (default):  "phpantom-lsp": true,  "php-lsp@claude-plugins-official": false
#   Intelephense:        "phpantom-lsp": false,  "php-lsp@claude-plugins-official": true
SETTINGS_FILE="/root/.claude/settings.json"
if [ -f "$SETTINGS_FILE" ]; then
    # Merge ENABLE_LSP_TOOL and PHPantom plugin into existing settings without overwriting other keys
    UPDATED=$(jq '
        .env = ((.env // {}) + {"ENABLE_LSP_TOOL": "1"}) |
        .enabledPlugins = ((.enabledPlugins // {}) + {"phpantom-lsp": true})
    ' "$SETTINGS_FILE") \
        && echo "$UPDATED" > "$SETTINGS_FILE"
    echo "✓ LSP enabled in existing settings.json (PHPantom default)"
else
    cat > "$SETTINGS_FILE" <<'SETTINGS_EOF'
{
  "env": {
    "ENABLE_LSP_TOOL": "1"
  },
  "enabledPlugins": {
    "phpantom-lsp": true
  }
}
SETTINGS_EOF
    chmod 600 "$SETTINGS_FILE"
    echo "✓ Created settings.json with LSP enabled (PHPantom default)"
fi

# Install PHPantom LSP plugin if not already present
# This copies the plugin from the image to the user's plugin directory
PHPANTOM_PLUGIN_DIR="/root/.claude/plugins/phpantom-lsp"
if [ ! -d "$PHPANTOM_PLUGIN_DIR/.claude-plugin" ]; then
    mkdir -p "$PHPANTOM_PLUGIN_DIR"
    cp -r /opt/claude-yolo/plugins/phpantom-lsp/.claude-plugin "$PHPANTOM_PLUGIN_DIR/"
    echo "✓ PHPantom LSP plugin installed"
else
    echo "✓ PHPantom LSP plugin already present"
fi

# Create .claude.json if it doesn't exist (preserves existing state in project)
if [ ! -f /root/.claude.json ]; then
    cat > /root/.claude.json <<'EOF'
{
  "hasCompletedOnboarding": true,
  "installMethod": "native",
  "bypassPermissionsModeAccepted": true
}
EOF
    chmod 600 /root/.claude.json
    echo "✓ Created .claude.json with bypass permissions acceptance"
else
    echo "✓ Using existing .claude.json from project storage"
fi

# Mark /workspace as trusted so the "do you trust this folder?" prompt is suppressed.
# hasTrustDialogAccepted is stored per-project in .claude.json — set it unconditionally
# since the file may have been created without it (or the container may be fresh).
trust_updated=$(jq '.projects["/workspace"].hasTrustDialogAccepted = true' /root/.claude.json)
if [ -z "$trust_updated" ]; then
    echo "ERROR: Failed to update .claude.json trust flag" >&2
    exit 1
fi
echo "$trust_updated" > /root/.claude.json
echo "✓ /workspace marked as trusted (hasTrustDialogAccepted)"

# Source the project's ccy.env (if present) so per-project ccy config is
# declarative and tracked, not ad-hoc host exports. Sourced HERE, inside the
# container (the same sandbox where claude --dangerously-skip-permissions runs),
# never on the host — so a project cannot execute code on the host via it.
_ccy_env_file="/workspace/.claude/ccy/ccy.env"
if [ -f "$_ccy_env_file" ]; then
    echo "Sourcing project ccy env: $_ccy_env_file"
    # shellcheck source=/dev/null
    . "$_ccy_env_file"
fi

# Execute the command.
# Optional supervisor wrap (default OFF): if CCY_CLAUDE_WRAPPER is set, prepend it
# to the claude invocation. Unset => unchanged behaviour. The wrapper command must
# exist on PATH inside the image (provided separately — see fedora-desktop issue #31).
if [[ -n "${CCY_CLAUDE_WRAPPER:-}" ]]; then
    read -ra _ccy_wrapper <<< "$CCY_CLAUDE_WRAPPER"
    exec "${_ccy_wrapper[@]}" "$@"
fi
exec "$@"

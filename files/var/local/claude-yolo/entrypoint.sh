#!/bin/bash
# Claude Code YOLO Container Entrypoint
# In rootless Docker, UID 0 = host user, so this is safe
# IMPORTANT: Uses ccy-specific tokens from ~/.claude-tokens/ccy/ (NOT desktop tokens)

set -e

# Every file Claude Code writes under /workspace/.claude/ccy is session state:
# full conversation transcripts, verbatim pre-edit file bodies in file-history/,
# shell snapshots, prompt history. Anthropic documents that this state is NOT
# encrypted at rest and that OS file permissions are its only protection
# (code.claude.com/docs/en/claude-directory, "Plaintext storage").
#
# The default umask (022) therefore makes every one of those files readable by
# every local user, forever. Plan 00071 measured 887 of 990 files and 331 of 348
# directories carrying group/other bits before this line existed. Set it here,
# before anything creates state, so the posture is correct by construction
# rather than by periodic repair.
#
# 077 = owner keeps rwx; group and other get nothing. Execute bits on files that
# need them are unaffected, because umask only ever clears bits the creator asks
# for — it cannot add them.
umask 077

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

# Verify the authenticated account matches the expected GitHub username.
#
# The lookup is retried and its answer VALIDATED as a login before comparison.
# This is the container-side twin of the host check in lib/ssh-handling.bash, and
# it had the identical defect (CCY 3.36.0 fixed the host, this fixes here): the
# exit status was discarded, so when GitHub answered 502 the JSON error body —
# which gh writes to stdout — became "the authenticated user" and the container
# refused to start, blaming the user's gh-token configuration. The configuration
# was fine; GitHub was down.
if [ -n "$GITHUB_USERNAME" ]; then
    AUTHENTICATED_USER=""
    GH_LOOKUP_ERROR=""
    for attempt in 1 2 3; do
        if AUTH_OUT="$(gh api user --jq .login 2>&1)"; then
            # Only a login is an acceptable answer. An error body, an HTML page,
            # or empty output all mean the lookup failed, whatever gh's status
            # said.
            if printf '%s' "$AUTH_OUT" | grep -qE '^[A-Za-z0-9][A-Za-z0-9-]*$'; then
                AUTHENTICATED_USER="$AUTH_OUT"
                break
            fi
        fi
        GH_LOOKUP_ERROR="$AUTH_OUT"
        if [ "$attempt" -lt 3 ]; then
            sleep "$attempt"
        fi
    done

    if [ -z "$AUTHENTICATED_USER" ]; then
        echo "ERROR: Could not verify which account this token belongs to" >&2
        echo "" >&2
        echo "GitHub's API did not return a usable answer after 3 attempts." >&2
        echo "What it said:" >&2
        echo "  ${GH_LOOKUP_ERROR:-(no output)}" >&2
        echo "" >&2
        echo "This is almost always GitHub being briefly unavailable, NOT a problem" >&2
        echo "with your token or configuration. Check https://www.githubstatus.com/" >&2
        echo "and retry." >&2
        exit 1
    fi

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

# NOTE: CCY_DISABLE_SUSPEND and the build-time ctrl+z patch sentinel used to be
# set/read here. Both are gone — ctrl+z suppression is the PTY supervisor's job
# now (claude-supervise.py strips the 0x1a SUSP byte from forwarded stdin and
# swallows SIGTSTP/SIGQUIT). See CLAUDE/ContainerRules.md.

# Mouse / fullscreen rendering: CCY sets NEITHER CLAUDE_CODE_DISABLE_MOUSE nor
# CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN. Claude Code's own defaults apply — the
# classic in-band renderer by default, with `/tui fullscreen` opting in and
# persisting via the settings.json CCY symlinks to
# /workspace/.claude/ccy/settings.json.
#
# History (Plan 00047 — do NOT re-add DISABLE_MOUSE without reading this):
# fullscreen draws on the terminal alt-screen, and with mouse capture OFF (the
# old DISABLE_MOUSE=1, kept "for native click-drag selection") Wayland
# terminals — GNOME-Terminal/VTE, and even kitty — fall back to DECSET-1007
# "alternate scroll" and remap the wheel to arrow keys, which the prompt reads
# as history recall and clobbers your input. Plan 00047 chased per-emulator
# wheel→PageUp remaps (dead: kitty bypasses mouse_map with tracking off) and
# then forced the classic renderer via DISABLE_ALTERNATE_SCREEN=1. Every one of
# those dead-ends assumed mouse tracking stayed OFF. It doesn't have to: letting
# Claude Code capture the mouse (i.e. NOT setting DISABLE_MOUSE) makes CC handle
# the wheel itself inside the alt-screen, so fullscreen scroll works natively on
# VTE. With the wheel fixed there is no reason to force the classic renderer, so
# the kill switch is gone too and fullscreen is a normal opt-in again.
# Trade-off in fullscreen: click-drag selection becomes Shift-drag and native
# Ctrl+F search becomes Ctrl+O transcript mode; the classic default avoids both.
# See: https://docs.anthropic.com/en/docs/claude-code/fullscreen

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

# Install the CCY built-in skills from their staging area in the image.
#
# This MUST run after the /root/.claude symlink above: the Dockerfile cannot write
# them to /root/.claude/skills/ directly, because the symlink step rm -rf's that
# directory on every start. Copied UNCONDITIONALLY (unlike the plugin above, which is
# install-once) so a rebuilt image always delivers current guidance — these are
# image-owned content, not user state, and a stale skill teaching a stale rule is the
# failure mode this whole path exists to prevent.
CCY_SKILLS_SRC="/opt/claude-yolo/skills"
if [ -d "$CCY_SKILLS_SRC" ]; then
    mkdir -p /root/.claude/skills
    cp -r "$CCY_SKILLS_SRC/." /root/.claude/skills/
    echo "✓ CCY skills installed: $(find /root/.claude/skills -mindepth 1 -maxdepth 1 -printf '%f ')"
else
    echo "ERROR: $CCY_SKILLS_SRC is missing from the image — skills cannot be installed." >&2
    echo "  The image is built by files/var/local/claude-yolo/Dockerfile." >&2
    exit 1
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

# ── Optional: child-claude spawn mode (Plan 00092) ────────────────────────────
#
# Opt-in per project with CCY_CHILD_CLAUDE=1 in the ccy.env sourced just above.
# When on, a session gets `ccy-claude` on PATH and a skill telling the agent the
# capability exists. When off, it gets neither.
#
# THIS MUST RUN AFTER the ccy.env source — the flag does not exist before it —
# and therefore AFTER the unconditional skills install higher up. That ordering
# is why the optional tree lives OUTSIDE /opt/claude-yolo/skills/: everything in
# that directory is copied to every session, so an opt-in skill cannot live there.
#
# What this gate is, and is not: it decides whether the TOOLING and the GUIDANCE
# are installed. It is not a security control and must never be described as one.
# The agent runs as root and the token is in PID 1's environment, so anything
# root can do here it could already do. See the plan's SECURITY-MODEL.md.
_ccy_child_claude_src="/opt/claude-yolo/optional/child-claude"
_ccy_child_claude_skill="/root/.claude/skills/child-claude"
_ccy_child_claude_bin="/usr/local/bin/ccy-claude"

case "${CCY_CHILD_CLAUDE:-}" in
    1 | 0 | "") ;;
    *)
        # A typo silently disabling a feature the project asked for is a bad
        # failure mode: the session looks fine and the capability is just absent.
        echo "ERROR: CCY_CHILD_CLAUDE must be 1 or unset, got '$CCY_CHILD_CLAUDE'" >&2
        echo "  Set it in $_ccy_env_file as: export CCY_CHILD_CLAUDE=1" >&2
        exit 1
        ;;
esac

if [ "${CCY_CHILD_CLAUDE:-}" = "1" ]; then
    if [ ! -d "$_ccy_child_claude_src" ]; then
        echo "ERROR: this project asked for child-claude mode, but the image does not ship it." >&2
        echo "  Expected: $_ccy_child_claude_src" >&2
        echo "  The image predates the feature. Rebuild it: ccy --rebuild" >&2
        echo "  Refusing to start rather than run without the tooling the project asked for." >&2
        exit 1
    fi

    ln -sf "$_ccy_child_claude_src/bin/ccy-claude" "$_ccy_child_claude_bin"

    # Replaced wholesale, not merged, so a rebuilt image always delivers current
    # guidance — same reasoning as the unconditional skills install above.
    rm -rf "$_ccy_child_claude_skill"
    cp -r "$_ccy_child_claude_src/skills/child-claude" "$_ccy_child_claude_skill"

    # Exported so the wrapper and the plan's acceptance script see them. A value
    # set in ccy.env without `export` would not survive the exec into claude.
    export CCY_CHILD_CLAUDE
    export CCY_CHILD_CLAUDE_MAX_DEPTH="${CCY_CHILD_CLAUDE_MAX_DEPTH:-1}"

    echo "✓ child-claude mode ON: ccy-claude on PATH, skill installed, max depth $CCY_CHILD_CLAUDE_MAX_DEPTH" >&2
else
    # The removal is the whole reason this branch exists. /root/.claude is a
    # symlink to /workspace/.claude/ccy, so the skills directory is HOST-PERSISTED
    # across containers — a skill installed by an earlier enabled session would
    # otherwise still be there, and the mode could be turned on but never off.
    #
    # Only the skill needs this. The PATH symlink lives on the container's own
    # filesystem, which is discarded on every run (`podman run --rm`).
    if [ -e "$_ccy_child_claude_skill" ]; then
        rm -rf "$_ccy_child_claude_skill"
        echo "child-claude mode off: removed the skill left by an earlier session" >&2
    fi
fi

# ── Supervisor wrap: DEFAULT ON when the project ships a supervisor ───────────
#
# Precedence, highest first:
#   1. CCY_CLAUDE_WRAPPER forwarded from the host (`ccy --supervise`, or a host
#      export) — an explicit operator instruction, always wins.
#   2. CCY_CLAUDE_WRAPPER set by the project ccy.env sourced just above — the
#      per-project choice (this is where `--arm` is opted into).
#   3. This default: the project supervisor, unarmed, if it is there.
#
# Why default ON (CCY 3.42.0). The supervisor is the ONLY remaining ctrl+z
# guard: it strips the 0x1a SUSP byte from forwarded stdin and swallows
# SIGTSTP/SIGQUIT, and the image-level byte patch that used to do that job was
# retired. Leaving the guard opt-in meant every project without a ccy.env could
# still be frozen by a keypress with no way to recover inside a container.
#
# Unarmed by default: without --arm the supervisor injects one harmless visible
# marker per session instead of a real /compact. Automatic compaction changes
# what a session DOES and stays an explicit opt-in (ccy.env --arm, or
# `ccy --supervise`); the terminal-key guard does not, and is what we want
# everywhere. Opt out entirely with CCY_NO_SUPERVISOR=1 / `ccy --no-supervise`.
CCY_SUPERVISOR_PATH="${CCY_SUPERVISOR_PATH:-/workspace/.claude/ccy/claude-supervise.py}"

if [[ -z "${CCY_CLAUDE_WRAPPER:-}" ]] && [[ "${CCY_NO_SUPERVISOR:-}" != "1" ]]; then
    if [ -f "$CCY_SUPERVISOR_PATH" ]; then
        # Syntax-check before exec. A corrupt or truncated supervisor would
        # otherwise take every session in every project down with it, and this
        # path is now reached by default rather than only by opt-in. Parsed with
        # ast rather than py_compile so nothing is written to the project.
        #
        # The probe FAILS THE LAUNCH rather than quietly running unwrapped: an
        # unwrapped session has no ctrl+z guard, and silently downgrading the
        # one protection left is exactly the "skip and continue" this repo bans.
        # The message names the bypass, so the operator is never stuck.
        if ! _ccy_sup_probe=$(python3 -c 'import ast,sys; ast.parse(open(sys.argv[1]).read(), sys.argv[1])' \
            "$CCY_SUPERVISOR_PATH" 2>&1); then
            echo "✗ CCY: the project supervisor at $CCY_SUPERVISOR_PATH does not parse." >&2
            echo "$_ccy_sup_probe" >&2
            echo "  Restore it from git, or re-deploy it with the hooks daemon." >&2
            echo "  To launch without it (NO ctrl+z guard): ccy --no-supervise" >&2
            exit 1
        fi
        # Invoked through python3 rather than executed directly: the exec bit on
        # a git-tracked file is one more thing that can be wrong, and it has been.
        CCY_CLAUDE_WRAPPER="python3 $CCY_SUPERVISOR_PATH --"
        echo "Supervisor: on by default (ctrl+z guard active, auto-compaction unarmed)" >&2
    else
        # Say so. An absent supervisor is a normal state, but after 3.42.0 it is
        # also the state with no ctrl+z guard at all — and a silence there reads
        # as "protected" to anyone who does not know the patch was removed.
        echo "Supervisor: not present at $CCY_SUPERVISOR_PATH — ctrl+z is UNGUARDED in this session." >&2
        echo "  Install the hooks daemon in this project to get the guard back." >&2
    fi
fi

# Execute the command.
if [[ -n "${CCY_CLAUDE_WRAPPER:-}" ]]; then
    read -ra _ccy_wrapper <<< "$CCY_CLAUDE_WRAPPER"
    exec "${_ccy_wrapper[@]}" "$@"
fi
exec "$@"

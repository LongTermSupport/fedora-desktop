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
# every local user, forever. Plan 00098 measured 887 of 990 files and 331 of 348
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

# Configure SSH for git operations if keys provided.
#
# With the launcher's SSH_AGENT_FORWARDED=1 the session's own agent is mounted at
# $SSH_AUTH_SOCK: no agent is started here, and mounted key FILES are wired by
# IdentityFile (github_key_directives, folded into the github.com stanza below)
# rather than ssh-add — adding to a forwarded agent would load keys into the
# person's agent on the far side, which is theirs, not this container's.
#
# A server's session restore (Plan 00135) mounts a passphrase copy and an askpass helper at
# RESTORE_ASKPASS_MOUNT; on any other launch nothing is there. ssh-add is pointed at the
# helper for its own run only, so nothing else in the container, a `podman exec` included,
# ever sees SSH_ASKPASS. Once the keys are added both files are removed, before Claude starts.
RESTORE_ASKPASS_MOUNT=/run/ccy/restore-askpass

# restore_askpass_ssh_add <askpass-dir> <key>
restore_askpass_ssh_add() {
    local dir="$1" key="$2"
    if [ ! -e "$dir/askpass" ]; then
        ssh-add "$key"
        return
    fi
    SSH_ASKPASS="$dir/askpass" SSH_ASKPASS_REQUIRE=force CCY_RESTORE_PP_FILE="$dir/pp" \
        ssh-add "$key" </dev/null
}

# restore_askpass_finish <askpass-dir>
restore_askpass_finish() {
    local dir="$1"
    if ! rm -f -- "$dir/pp" "$dir/askpass"; then
        echo "ERROR: could not remove the session-restore passphrase copy in $dir" >&2
        exit 1
    fi
}

github_key_directives=()
if [ "${SSH_AGENT_FORWARDED:-0}" = "1" ]; then
    if ! ssh-add -l >/tmp/ccy-agent-probe.out 2>&1; then
        echo "ERROR: SSH_AGENT_FORWARDED=1 but the agent at ${SSH_AUTH_SOCK:-(unset)} answers nothing usable:" >&2
        cat /tmp/ccy-agent-probe.out >&2
        exit 1
    fi
    echo "✓ Forwarded ssh-agent in use ($(grep -c . /tmp/ccy-agent-probe.out) key(s))"
    if [ -n "$SSH_KEY_PATHS" ]; then
        IFS=: read -ra KEYS <<< "$SSH_KEY_PATHS"
        for key in "${KEYS[@]}"; do
            github_key_directives+=("    IdentityFile $key")
        done
    fi
elif [ -n "$SSH_KEY_PATHS" ]; then
    eval "$(ssh-agent -s)" > /dev/null 2>&1

    IFS=: read -ra KEYS <<< "$SSH_KEY_PATHS"
    for key in "${KEYS[@]}"; do
        if ! restore_askpass_ssh_add "$RESTORE_ASKPASS_MOUNT" "$key" 2>&1; then
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

restore_askpass_finish "$RESTORE_ASKPASS_MOUNT"

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
    # first push does not hang on an interactive host-key prompt. The same applies
    # to every host:port the launcher's alias stanza points at (SSH_KNOWN_HOSTS_PINS,
    # space-separated): GitHub serves the same host keys on all of them.
    while IFS= read -r ghkey; do
        [ -n "$ghkey" ] || continue
        echo "github.com $ghkey"
        if [ "${GITHUB_SSH_443:-0}" = "1" ]; then
            echo "[ssh.github.com]:443 $ghkey"
        fi
        for pin in ${SSH_KNOWN_HOSTS_PINS:-}; do
            pin_host="${pin%:*}"
            pin_port="${pin##*:}"
            if [ "$pin_port" = "22" ]; then
                echo "$pin_host $ghkey"
            else
                echo "[$pin_host]:$pin_port $ghkey"
            fi
        done
    done <<< "$github_ssh_keys" >> ~/.ssh/known_hosts
    chmod 600 ~/.ssh/known_hosts
    echo "✓ GitHub SSH host keys pinned in known_hosts"
else
    echo "⚠ Could not fetch GitHub SSH host keys (offline?) — using StrictHostKeyChecking=accept-new for git/ssh" >&2
    github_ssh_directives+=("    StrictHostKeyChecking accept-new")
fi

# Write the github.com config stanza if any directives were collected (the 443
# endpoint rewrite, the offline accept-new fallback, and/or the IdentityFile lines
# that stand in for ssh-add when an agent is forwarded).
if [ "${#github_ssh_directives[@]}" -gt 0 ] || [ "${#github_key_directives[@]}" -gt 0 ]; then
    {
        echo "Host github.com"
        if [ "${#github_ssh_directives[@]}" -gt 0 ]; then
            printf '%s\n' "${github_ssh_directives[@]}"
        fi
        if [ "${#github_key_directives[@]}" -gt 0 ]; then
            printf '%s\n' "${github_key_directives[@]}"
        fi
    } >> ~/.ssh/config
    chmod 600 ~/.ssh/config
fi

# The project remote's alias stanza, rendered by the launcher from the host's own
# ssh config (SSH_CONFIG_EXTRA_B64). Without it `git@<alias>:owner/repo.git`
# resolves nothing in here and the checkout cannot even fetch.
if [ -n "${SSH_CONFIG_EXTRA_B64:-}" ]; then
    if ! printf '%s' "$SSH_CONFIG_EXTRA_B64" | base64 -d >> ~/.ssh/config; then
        echo "ERROR: SSH_CONFIG_EXTRA_B64 is not valid base64" >&2
        exit 1
    fi
    echo "" >> ~/.ssh/config
    chmod 600 ~/.ssh/config
    echo "✓ Remote alias stanza written to ~/.ssh/config: $(printf '%s' "$SSH_CONFIG_EXTRA_B64" | base64 -d | awk 'NR==1')"
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
# Then the untracked ccy.env.local, if present: per-checkout settings that must not be
# committed. It is sourced second, so its values win over ccy.env's.
# Top level, not a function: a `declare` in either file must stay global.
# scripts/test-ccy-project-env.bash runs the block between the markers.
# >>> PROJECT-ENV
for _ccy_env_file in /workspace/.claude/ccy/ccy.env /workspace/.claude/ccy/ccy.env.local; do
    if [ "$_ccy_env_file" = /workspace/.claude/ccy/ccy.env.local ]; then
        # The agent team bus is joined from ccy.env.local only; AGENT-BUS below reads this.
        _ccy_pingbus_teams_before_local=${PINGBUS_TEAMS+set}
    fi
    if [ -f "$_ccy_env_file" ]; then
        echo "Sourcing project ccy env: $_ccy_env_file"
        # shellcheck source=/dev/null
        . "$_ccy_env_file"
    fi
done
# <<< PROJECT-ENV

# ── Optional: child-claude spawn mode (Plan 00092) ────────────────────────────
#
# Opt-in per project with CCY_CHILD_CLAUDE=1 in the ccy.env (or ccy.env.local) sourced just above.
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
        echo "ERROR: CCY_CHILD_CLAUDE must be 1, 0 or unset, got '$CCY_CHILD_CLAUDE'" >&2
        echo "  Set it in .claude/ccy/ccy.env (or ccy.env.local) as: export CCY_CHILD_CLAUDE=1" >&2
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

    # Validated on the same terms as CCY_CHILD_CLAUDE above, and for the same reason.
    # ccy-claude re-checks this at use time, but the banner below is printed NOW and
    # would announce "max depth abc" as though it were a working bound — the operator
    # is told the feature is configured when it is not.
    case "${CCY_CHILD_CLAUDE_MAX_DEPTH:-1}" in
        '' | *[!0-9]*)
            echo "ERROR: CCY_CHILD_CLAUDE_MAX_DEPTH must be a whole number, got '$CCY_CHILD_CLAUDE_MAX_DEPTH'" >&2
            echo "  Set it in .claude/ccy/ccy.env (or ccy.env.local) as: export CCY_CHILD_CLAUDE_MAX_DEPTH=1" >&2
            exit 1
            ;;
    esac

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
    #
    # Removed only when it is recognisably OURS. The skills directory is the user's
    # real filesystem and the unconditional install above merges into it, so a
    # user-authored skill could share the name; deleting that by name alone would
    # destroy someone else's work. The shipped SKILL.md carries its own name in the
    # frontmatter, which is the marker checked here.
    if [ -e "$_ccy_child_claude_skill" ]; then
        if [ -f "$_ccy_child_claude_skill/SKILL.md" ] \
            && grep -q '^name: child-claude$' "$_ccy_child_claude_skill/SKILL.md"; then
            rm -rf "$_ccy_child_claude_skill"
            echo "child-claude mode off: removed the skill left by an earlier session" >&2
        else
            echo "WARNING: $_ccy_child_claude_skill exists but is not the shipped skill — left untouched" >&2
        fi
    fi
fi

# ── Optional: the agent team bus (Plan 00161) ─────────────────────────────────
#
# Opt-in is PINGBUS_TEAMS in this checkout's ccy.env.local, which the launcher binds
# read-only, so a session cannot choose its own teams. Set before that file is read (by
# ccy.env, the image or the container environment) it is refused. Opted in, `pingbus config
# check` must accept every listed team's bundle under PINGBUS_HOME or the container does not
# start: opted in but broken is an error, not a silent no-op. Then pingbus goes on PATH (the
# plugin's hooks run it by name) and claude gets the plugin and the settings that let the
# watcher's wake notice start a turn. They go right after `claude`, so they land inside a
# supervisor wrapper's `--`. Unset, nothing is linked or added and the image's copy is inert.
# scripts/test-ccy-agent-bus.bash runs this block and the rest of the file after it.
# >>> AGENT-BUS
_ccy_agent_bus=/opt/claude-yolo/optional/agent-bus
if [ -n "${PINGBUS_TEAMS:-}" ]; then
    if [ "${_ccy_pingbus_teams_before_local:-}" = set ]; then
        echo "✗ CCY: PINGBUS_TEAMS was set before .claude/ccy/ccy.env.local was read (by ccy.env, the image or the container environment)." >&2
        echo "  A checkout joins the agent team bus only from ccy.env.local, which its install's IaC places and a session cannot edit." >&2
        exit 1
    fi
    for _ccy_need in pingbus settings.json plugin/pingbus/.claude-plugin/plugin.json plugin/pingbus/hooks/hooks.json; do
        if [ ! -f "$_ccy_agent_bus/$_ccy_need" ]; then
            echo "✗ CCY: this checkout joins the agent team bus (PINGBUS_TEAMS=$PINGBUS_TEAMS), but the image has no $_ccy_agent_bus/$_ccy_need." >&2
            echo "  The image predates the feature. Rebuild it: ccy --rebuild" >&2
            exit 1
        fi
    done
    if [ "${1:-}" != claude ]; then
        echo "✗ CCY: this checkout joins the agent team bus, but the command is '${1:-}', not claude, so the bus plugin cannot be added." >&2
        exit 1
    fi
    export PINGBUS_TEAMS
    export PINGBUS_HOME="${PINGBUS_HOME:-/workspace/.claude/ccy/pingbus}"
    _ccy_bus_rc=0
    "$_ccy_agent_bus/pingbus" config check >&2 || _ccy_bus_rc=$?
    if [ "$_ccy_bus_rc" -ne 0 ]; then
        echo "✗ CCY: pingbus config check refused this checkout's agent team bus setup (exit $_ccy_bus_rc)." >&2
        echo "  Teams: $PINGBUS_TEAMS; bundles under $PINGBUS_HOME/<team>/. Fix the bundle, or take PINGBUS_TEAMS out of ccy.env.local." >&2
        exit 1
    fi
    ln -sf "$_ccy_agent_bus/pingbus" /usr/local/bin/pingbus
    set -- "$1" --plugin-dir "$_ccy_agent_bus/plugin/pingbus" --settings "$_ccy_agent_bus/settings.json" "${@:2}"
    echo "✓ agent team bus: $PINGBUS_TEAMS (bundles in $PINGBUS_HOME)" >&2
fi
unset _ccy_pingbus_teams_before_local
# <<< AGENT-BUS

# ── Supervisor wrap: DEFAULT ON when the project ships a supervisor ───────────
#
# Precedence, highest first:
#   1. CCY_CLAUDE_WRAPPER forwarded from the host (`ccy --supervise`, or a host
#      export) — an explicit operator instruction, always wins.
#   2. CCY_CLAUDE_WRAPPER set by the project ccy.env (or this checkout's ccy.env.local)
#      sourced just above — the per-project choice (this is where `--arm` is opted into).
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

# ── Session lifecycle plugin: only when --max-age/--run-for/--until asked for it ──────
#
# The launcher passes the settings as CCY_LIFECYCLE_* (lib/session-lifecycle.bash on the
# host; a project ccy.env sourced above may set them too). When none is set this block does
# nothing and the wrapper line below is exactly what it was before the plugin existed.
#
# The plugin file lives in the image, outside the project mount, root owned: the supervisor
# refuses a plugin that is group- or world-writable, and a project could not otherwise be
# trusted not to supply its own. It is named on the supervisor's command line, before the
# final `--`, which is where the supervisor takes `--plugin` flags.
ccy_lifecycle_wanted() {
    [[ -n "${CCY_LIFECYCLE_MAX_AGE_SECONDS:-}" || -n "${CCY_LIFECYCLE_DEADLINE_EPOCH:-}" ]]
}

# ccy_lifecycle_extend_wrapper — add the plugin to the _ccy_wrapper array. Refuses, with the
# reason on stderr, anything it cannot honour: the user asked for a session limit, and a
# session that quietly runs without it is the "skip and continue" this project bans.
ccy_lifecycle_extend_wrapper() {
    local plugin="/opt/claude-yolo/supervisor-plugins/ccy_lifecycle.py"
    local last=$((${#_ccy_wrapper[@]} - 1))
    # The supervisor is named directly, or through the hooks daemon's own launcher
    # (.claude/ccy/claude-supervise, what the daemon's ccy.env arms projects with from its
    # release after 3.68.0), which execs its sibling claude-supervise.py with every argument
    # unchanged. With no usable Python the launcher runs claude unsupervised instead, which
    # this check cannot see; the image's python3 makes that a misconfigured CCY_PYTHON only.
    local supervisor="" word want have
    for word in "${_ccy_wrapper[@]}"; do
        case "$word" in
        *claude-supervise.py)
            supervisor="$word"
            break
            ;;
        */claude-supervise)
            supervisor="$word.py"
            break
            ;;
        esac
    done
    if [[ -z "$supervisor" ]] || ((last < 1)) || [[ "${_ccy_wrapper[$last]}" != "--" ]]; then
        echo "✗ CCY: --max-age/--run-for/--until need the hooks-daemon supervisor as the claude wrapper, but the wrapper is:" >&2
        echo "    $CCY_CLAUDE_WRAPPER" >&2
        echo "  It must run the supervisor (claude-supervise.py, or the daemon's claude-supervise launcher) and end in --." >&2
        echo "  Fix CCY_CLAUDE_WRAPPER, or drop the option." >&2
        return 1
    fi
    if [[ ! -f "$plugin" ]]; then
        echo "✗ CCY: the lifecycle plugin is missing from this image ($plugin)." >&2
        echo "  Rebuild the image: ccy --rebuild" >&2
        return 1
    fi
    # A supervisor older than the plugin API rejects --plugin as an unknown argument and the
    # container exits with an argparse error, so read its declared API major first.
    if [[ ! -f "$supervisor" ]]; then
        echo "✗ CCY: the supervisor the wrapper runs is not there: $supervisor" >&2
        echo "    (the wrapper is: $CCY_CLAUDE_WRAPPER)" >&2
        return 1
    fi
    if ! want=$(awk '/^PLUGIN_API = [0-9]+$/ {print $3; exit}' "$plugin") || [[ -z "$want" ]]; then
        echo "✗ CCY: the lifecycle plugin in this image declares no PLUGIN_API ($plugin). Rebuild: ccy --rebuild" >&2
        return 1
    fi
    if ! have=$(awk '/^_PLUGIN_API_MAJOR = [0-9]+$/ {print $3; exit}' "$supervisor"); then
        echo "✗ CCY: could not read the supervisor at $supervisor." >&2
        return 1
    fi
    if [[ -z "$have" ]]; then
        echo "✗ CCY: this project's supervisor predates the plugin API that --max-age/--run-for/--until need." >&2
        echo "  upgrade the hooks daemon in this project to a release with the supervisor plugin API, or drop the option." >&2
        return 1
    fi
    if [[ "$have" != "$want" ]]; then
        echo "✗ CCY: this project's supervisor speaks plugin API $have, and the lifecycle plugin speaks plugin API $want." >&2
        echo "  Update ccy, or upgrade the hooks daemon in this project, so the two agree; or drop the option." >&2
        return 1
    fi
    _ccy_wrapper=("${_ccy_wrapper[@]:0:$last}" --plugin "ccy-lifecycle=$plugin" --)
}

# Execute the command.
if [[ -n "${CCY_CLAUDE_WRAPPER:-}" ]]; then
    read -ra _ccy_wrapper <<< "$CCY_CLAUDE_WRAPPER"
    if ccy_lifecycle_wanted; then
        ccy_lifecycle_extend_wrapper || exit 1
    fi
    exec "${_ccy_wrapper[@]}" "$@"
fi
if ccy_lifecycle_wanted; then
    echo "✗ CCY: --max-age/--run-for/--until are carried out by the supervisor, and this session runs without one." >&2
    echo "  Install the hooks daemon in this project (it deploys the supervisor), or drop the option." >&2
    exit 1
fi
exec "$@"

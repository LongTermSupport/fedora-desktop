#!/bin/bash
# Terminal Emulator Detection — CCY Pre-flight
#
# Version: 1.0.0
#
# Detects the host terminal emulator and gates CCY launch on whether the wheel
# can be remapped to PageUp/PageDown at the emulator level (see Plan 00047).
#
# Supported set (wheel-remap config deployed by play-terminal-emulators.yml):
#   kitty, alacritty, wezterm
#
# Detected but unsupported (no emulator-level wheel→PageUp possible):
#   ghostty, foot, konsole, gnome-terminal, ptyxis, tilix, xterm
#
# Used by claude-yolo. Honours CCY_ACCEPT_UNSUPPORTED_TERM=1 as an override.
#
# Depends on common.bash for color codes (COLOR_RED etc.). gum is used for the
# banner if available; falls back to plain printf otherwise.

# Echoes the detected terminal name, or "unknown" if nothing matches.
# Detection order: strong env-var signal → $TERM string → parent process name.
detect_terminal() {
    # 1. Strong env-var signals (most reliable — emulator sets these unconditionally)
    if [ -n "${KITTY_WINDOW_ID:-}${KITTY_PID:-}" ]; then echo kitty; return; fi
    if [ -n "${ALACRITTY_LOG:-}${ALACRITTY_WINDOW_ID:-}" ]; then echo alacritty; return; fi
    if [ -n "${WEZTERM_PANE:-}${WEZTERM_EXECUTABLE:-}" ]; then echo wezterm; return; fi
    if [ -n "${GHOSTTY_RESOURCES_DIR:-}${GHOSTTY_BIN_DIR:-}" ]; then echo ghostty; return; fi
    if [ -n "${FOOT_VERSION:-}" ]; then echo foot; return; fi
    if [ -n "${KONSOLE_VERSION:-}${KONSOLE_DBUS_SERVICE:-}" ]; then echo konsole; return; fi

    # VTE family — distinguish ptyxis / tilix / gnome-terminal via parent process
    if [ -n "${VTE_VERSION:-}" ]; then
        local parent
        parent=$(_parent_comm)
        case "$parent" in
            ptyxis-agent|ptyxis) echo ptyxis ;;
            tilix) echo tilix ;;
            *) echo gnome-terminal ;;
        esac
        return
    fi

    # 2. $TERM string fallback (less reliable — many emulators set xterm-256color)
    case "${TERM:-}" in
        xterm-kitty*) echo kitty; return ;;
        alacritty*) echo alacritty; return ;;
        ghostty*|xterm-ghostty*) echo ghostty; return ;;
        wezterm*) echo wezterm; return ;;
        foot*) echo foot; return ;;
    esac

    # 3. Parent process name as last resort
    local parent
    parent=$(_parent_comm)
    case "$parent" in
        kitty) echo kitty ;;
        alacritty) echo alacritty ;;
        wezterm-gui|wezterm) echo wezterm ;;
        ghostty) echo ghostty ;;
        gnome-terminal-) echo gnome-terminal ;;
        ptyxis-agent|ptyxis) echo ptyxis ;;
        tilix) echo tilix ;;
        konsole) echo konsole ;;
        foot|footclient) echo foot ;;
        xterm) echo xterm ;;
        *) echo unknown ;;
    esac
}

# Helper: get the parent process command name. Returns empty string on failure.
# We suppress only stderr — a dead/missing $PPID is recoverable, so we fall
# through to the next detection layer rather than spewing "no such process".
_parent_comm() {
    ps -o comm= -p "$PPID" 2>/dev/null | tr -d ' '
}

# Returns 0 if the named terminal has a deployable emulator-level wheel remap.
# Optional arg: terminal name. Defaults to current detection.
is_supported_terminal() {
    local term="${1:-}"
    [ -z "$term" ] && term=$(detect_terminal)
    case "$term" in
        kitty|alacritty|wezterm) return 0 ;;
        *) return 1 ;;
    esac
}

# Returns 0 if the wheel-fix config has actually been deployed for the given
# terminal — checks for the Ansible-managed marker block in the user's config.
# Being in a supported emulator is necessary but not sufficient; the playbook
# must also have run.
is_wheel_fix_deployed() {
    local term="${1:-}"
    [ -z "$term" ] && term=$(detect_terminal)
    local marker='ANSIBLE MANAGED: CC wheel to PageUp'
    case "$term" in
        kitty)
            [ -f "$HOME/.config/kitty/kitty.conf" ] && \
                grep -qF "$marker" "$HOME/.config/kitty/kitty.conf"
            ;;
        alacritty)
            [ -f "$HOME/.config/alacritty/alacritty.toml" ] && \
                grep -qF "$marker" "$HOME/.config/alacritty/alacritty.toml"
            ;;
        wezterm)
            [ -f "$HOME/.config/wezterm/wezterm.lua" ] && \
                grep -qF "$marker" "$HOME/.config/wezterm/wezterm.lua"
            ;;
        *)
            return 1
            ;;
    esac
}

# Pre-flight check called by claude-yolo before container start.
# Returns 0 to proceed, 1 to abort.
# Honours CCY_ACCEPT_UNSUPPORTED_TERM=1 (skip check entirely).
terminal_preflight_check() {
    if [ "${CCY_ACCEPT_UNSUPPORTED_TERM:-}" = "1" ]; then
        return 0
    fi

    local term
    term=$(detect_terminal)

    if is_supported_terminal "$term"; then
        # Supported emulator — but verify the wheel-fix config is actually
        # deployed. Being in kitty alone doesn't fix anything if the playbook
        # hasn't run; the user would silently get the wheel-clobbers-history
        # bug while CCY claims everything is fine.
        if is_wheel_fix_deployed "$term"; then
            return 0
        fi

        if command -v gum >/dev/null; then
            _wheel_undeployed_banner_gum "$term"
        else
            _wheel_undeployed_banner_plain "$term"
        fi

        printf '\nContinue anyway? [y/N] '
        local answer
        read -r answer
        case "$answer" in
            y|Y|yes|YES|Yes) return 0 ;;
            *)
                echo "" >&2
                echo "Aborted. Deploy the wheel-fix config first:" >&2
                echo "  ansible-playbook playbooks/imports/play-terminal-emulators.yml" >&2
                echo "" >&2
                echo "To suppress this check permanently, export CCY_ACCEPT_UNSUPPORTED_TERM=1" >&2
                return 1
                ;;
        esac
    fi

    # Print the banner (gum if available, plain printf otherwise)
    if command -v gum >/dev/null; then
        _terminal_banner_gum "$term"
    else
        _terminal_banner_plain "$term"
    fi

    # Prompt — default N (abort)
    printf '\nContinue anyway? [y/N] '
    local answer
    read -r answer
    case "$answer" in
        y|Y|yes|YES|Yes)
            return 0
            ;;
        *)
            echo "" >&2
            echo "Aborted. To re-launch CCY in a supported terminal:" >&2
            echo "  1. Open kitty (or alacritty)" >&2
            echo "  2. cd to your project root" >&2
            echo "  3. Run: ccy" >&2
            echo "" >&2
            echo "To suppress this check permanently, export CCY_ACCEPT_UNSUPPORTED_TERM=1" >&2
            return 1
            ;;
    esac
}

# Render a yellow banner warning that the wheel-fix config hasn't been deployed
# yet, even though the user is in a supported emulator. Path-out is one
# ansible-playbook command.
_wheel_undeployed_banner_gum() {
    local term="$1"
    gum style \
        --border double \
        --border-foreground 220 \
        --padding "1 2" \
        --margin 1 \
        --width 80 \
        "$(printf '%s\n\n%s\n\n%s\n\n%s\n\n%s' \
            "$(gum style --foreground 220 --bold "⚠  $term detected, but the wheel→PageUp config is NOT deployed.")" \
            "The pre-flight check identified a supported terminal, but the Ansible-managed mouse_map block is missing from your config. The mouse-wheel bug is still active until you deploy the playbook." \
            "Fix (on host, from this repo root):" \
            "  ansible-playbook playbooks/imports/play-terminal-emulators.yml" \
            "Override: export CCY_ACCEPT_UNSUPPORTED_TERM=1 to skip this check permanently.")"
}

_wheel_undeployed_banner_plain() {
    local term="$1"
    printf '\n%b' "${COLOR_YELLOW:-\033[33m}"
    printf '════════════════════════════════════════════════════════════════════════════════\n'
    printf '⚠  %s detected, but the wheel→PageUp config is NOT deployed.\n' "$term"
    printf '════════════════════════════════════════════════════════════════════════════════\n'
    printf '%b' "${COLOR_RESET:-\033[0m}"
    printf '\n'
    printf 'The pre-flight identified a supported terminal, but the Ansible-managed\n'
    printf 'mouse_map block is missing from your config. The mouse-wheel bug is\n'
    printf 'still active until you deploy the playbook.\n\n'
    printf 'Fix (on host, from this repo root):\n'
    printf '  ansible-playbook playbooks/imports/play-terminal-emulators.yml\n\n'
    printf 'Override: export CCY_ACCEPT_UNSUPPORTED_TERM=1 to skip this check permanently.\n'
}

# Render the gum-styled banner (red double border, table inside a styled box).
_terminal_banner_gum() {
    local term="$1"
    local why
    why=$(_terminal_why "$term")
    local table
    table=$(_terminal_table "$term")

    gum style \
        --border double \
        --border-foreground 196 \
        --padding "1 2" \
        --margin 1 \
        --width 80 \
        "$(printf '%s\n\n%s\n\n%s\n\n%s\n\n%s' \
            "$(gum style --foreground 196 --bold "⚠  CCY is running in an UNSUPPORTED terminal: $term")" \
            "Claude Code's mouse-wheel events get translated to arrow-up/down on the alternate screen, which the prompt interprets as command-history recall — clobbering whatever you were typing." \
            "$why" \
            "$table" \
            "Override: export CCY_ACCEPT_UNSUPPORTED_TERM=1 to skip this check permanently.")"
}

# Plain printf banner (fallback when gum is not installed).
_terminal_banner_plain() {
    local term="$1"
    printf '\n%b' "${COLOR_RED:-\033[31m}"
    printf '════════════════════════════════════════════════════════════════════════════════\n'
    printf '⚠  CCY is running in an UNSUPPORTED terminal emulator: %s\n' "$term"
    printf '════════════════════════════════════════════════════════════════════════════════\n'
    printf '%b' "${COLOR_RESET:-\033[0m}"
    printf '\n'
    printf "Claude Code's mouse-wheel events get translated to arrow-up/down on the\n"
    printf 'alternate screen, which the prompt interprets as command-history recall —\n'
    printf 'clobbering whatever you were typing.\n\n'
    printf '%s\n\n' "$(_terminal_why "$term")"
    _terminal_table "$term"
    printf '\n'
    printf 'Override: export CCY_ACCEPT_UNSUPPORTED_TERM=1 to skip this check permanently.\n'
}

# One-line explanation of why a given emulator is unsupported.
_terminal_why() {
    case "$1" in
        ghostty)
            echo "Why: Ghostty has no mouse-binding config in v1.x. Open feature request: github.com/ghostty-org/ghostty/discussions/4169"
            ;;
        foot)
            echo "Why: Foot has no send-key action — only the wheel scrollback fallback can be disabled, leaving the wheel inert."
            ;;
        konsole)
            echo "Why: Konsole's key-bindings vocabulary has no wheel_up/wheel_down token."
            ;;
        gnome-terminal|ptyxis|tilix)
            echo "Why: VTE-based terminals (gnome-terminal, Ptyxis, Tilix) have no mouse-binding config at all."
            ;;
        xterm)
            echo "Why: xterm only exposes alternateScroll on/off — no per-emulator wheel-to-key remap."
            ;;
        unknown)
            echo "Why: Could not identify your terminal. The CCY pre-flight cannot verify wheel behaviour."
            ;;
        *)
            echo "Why: No emulator-level wheel→PageUp remap is available for $1."
            ;;
    esac
}

# Render the installed/supported/notes table, marking the current terminal.
_terminal_table() {
    local current="$1"
    local rows=(
        "kitty:yes:★ recommended"
        "alacritty:yes:"
        "wezterm:yes:not in default install"
        "ghostty:no:no mouse-binding config"
        "foot:no:half-measure only"
        "konsole:no:no wheel token"
        "gnome-terminal:no:VTE limitation"
        "ptyxis:no:VTE limitation"
    )
    printf '  %-16s %-11s %-11s %s\n' 'Emulator' 'Installed' 'Wheel-fix' 'Notes'
    printf '  %-16s %-11s %-11s %s\n' '----------' '---------' '---------' '-----'
    local row name supported note installed marker
    for row in "${rows[@]}"; do
        IFS=':' read -r name supported note <<< "$row"
        if command -v "$name" >/dev/null; then
            installed='yes'
        else
            installed='no'
        fi
        if [ "$name" = "$current" ]; then
            marker='  ← you are here'
        else
            marker=''
        fi
        printf '  %-16s %-11s %-11s %s%s\n' "$name" "$installed" "$supported" "$note" "$marker"
    done
}

export -f detect_terminal
export -f is_supported_terminal
export -f is_wheel_fix_deployed
export -f terminal_preflight_check

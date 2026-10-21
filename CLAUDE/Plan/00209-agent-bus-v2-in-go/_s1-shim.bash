#!/usr/bin/env bash
# _s1-shim.bash — the stand-in acceptance-s1.bash puts first on PATH for an open of ccy-teams,
# under four names (symlinks in the run directory), so the open's processes are seen without
# strace. ccy-teams starts each of these by name, through PATH.
#
#   env        logs "declare <subcommand>" for a declare.py run, then runs the real env
#   agent-bus  logs "agent-bus <first two arguments>", then runs the real agent-bus
#   fzf        the first screen: stamps S1_UI_MARK once, exits 130 (Esc), draws nothing
#   gum        a question (confirm, input, choose, write, filter, file) is a screen as fzf is;
#              anything else (style, spin) runs the real gum
#
# The caller sets S1_SHIM_LOG, S1_UI_MARK, S1_REAL_AGENT_BUS and S1_REAL_GUM.
set -euo pipefail

name="${0##*/}"

stamp_screen() {
    printf 'ui %s %s\n' "${name}" "${1:-}" >>"${S1_SHIM_LOG}"
    if [[ ! -e "${S1_UI_MARK}" ]]; then
        printf '%s %s %s\n' "${EPOCHREALTIME}" "${name}" "${1:-}" >"${S1_UI_MARK}"
    fi
}

case "${name}" in
env)
    args=("$@")
    for ((i = 0; i < ${#args[@]}; i++)); do
        if [[ "${args[i]}" == "helpers.agent_bus.declare" ]]; then
            printf 'declare %s\n' "${args[i + 1]:-}" >>"${S1_SHIM_LOG}"
            break
        fi
    done
    exec /usr/bin/env "$@"
    ;;
agent-bus)
    printf 'agent-bus %s %s\n' "${1:-}" "${2:-}" >>"${S1_SHIM_LOG}"
    exec "${S1_REAL_AGENT_BUS}" "$@"
    ;;
fzf)
    stamp_screen
    exit 130
    ;;
gum)
    case "${1:-}" in
    confirm | input | choose | write | filter | file)
        stamp_screen "$1"
        exit 130
        ;;
    esac
    exec "${S1_REAL_GUM}" "$@"
    ;;
*)
    printf '_s1-shim.bash: run as %s, which it does not stand in for\n' "${name}" >&2
    exit 2
    ;;
esac

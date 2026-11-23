#!/usr/bin/env bash
# Driver for scripts/test-ccy-launch-args-capture.bash: runs the library's real
# ccy_tmux_insulate for /launcher "$@", as cc does (prefix cc), with the session-choosing and
# tmux steps stubbed. It records the replay argv written to the registry in <out>.replay and
# the launch argv the session is started with in <out>.launch, each as the count and each
# argument bracketed; the tmux on PATH is a stub, so the closing attach ends at once.
# Usage: ccy-test-insulate-driver.bash <out> <lib dir> [args...] (in a pseudo-terminal)
set -uo pipefail
out="$1"
lib="$2"
shift 2
unset TMUX TMUX_PANE
# shellcheck source=/dev/null
source "$lib/common-pure.bash"
# shellcheck source=/dev/null
source "$lib/session-registry.bash"
# shellcheck source=/dev/null
source "$lib/tmux-session.bash"
export CCY_TMUX_SESSION_PREFIX="cc"
ccy_tmux_offer() { echo "new"; }
ccy_tmux_next_name() { echo "t-1"; }
ccy_registry_write() {
    shift 5
    printf '%d:' "$#" >"$out.replay"
    printf '[%s]' "$@" >>"$out.replay"
}
ccy_tmux_start_detached() {
    shift 2
    printf '%d:' "$#" >"$out.launch"
    printf '[%s]' "$@" >>"$out.launch"
}
ccy_tmux_insulate project /launcher "$@"

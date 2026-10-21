#!/bin/bash
# The caller's exported GH_TOKEN, carried to the launcher that an interactive launch re-runs
# inside tmux (lib/tmux-session.bash). The launcher takes the export off the token at its
# first line, so the re-run would not see it, and exporting it again would be worse: tmux's
# server keeps the environment of the client that started it and gives it to every later
# session, so one launch's token would reach another's panes. So the token goes through a
# file: fresh, 0600, in a 0700 directory made for it under XDG_RUNTIME_DIR (else TMPDIR).
# Only the file's path travels, as CCY_GH_TOKEN_HANDOFF on the pane's command, and the
# launcher that finds it reads the token and deletes the file as soon as this library is
# sourced. The outer launcher removes the file on every exit; a launcher that replaces itself
# with exec leaves it for the one it runs.
#
# Sourced by the launcher before any other library that does work at load.

# ccy_token_handoff_take — when CCY_GH_TOKEN_HANDOFF names a file, set GH_TOKEN from it
# (the launcher's first line has taken the export off it) and delete the file and its
# directory. A path that is not a regular
# file of this user's fails: a token is never read through a link or from a file not ours.
ccy_token_handoff_take() {
    local file="${CCY_GH_TOKEN_HANDOFF:-}"
    unset CCY_GH_TOKEN_HANDOFF
    [ -n "$file" ] || return 0
    if [ -L "$file" ] || [ ! -f "$file" ] || [ ! -O "$file" ]; then
        print_error "CCY_GH_TOKEN_HANDOFF=$file is not a file of this user's; the GitHub token handed on by the launch that started this one cannot be read."
        return 1
    fi
    GH_TOKEN="$(<"$file")" || return 1
    rm -f -- "$file" && rmdir -- "$(dirname -- "$file")"
}

# ccy_token_handoff_give — write GH_TOKEN, when set, to a fresh hand-off file and set
# CCY_GH_TOKEN_HANDOFF (not exported) to its path; the EXIT trap removes it.
ccy_token_handoff_give() {
    [ -n "${GH_TOKEN:-}" ] || return 0
    local base="${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}" dir
    if ! dir="$(mktemp -d "$base/ccy-gh-token.XXXXXX")"; then
        print_error "cannot make a private directory in $base to hand the GitHub token to the session."
        return 1
    fi
    CCY_GH_TOKEN_HANDOFF="$dir/token"
    trap ccy_token_handoff_drop EXIT
    (umask 077 && printf '%s' "$GH_TOKEN" >"$CCY_GH_TOKEN_HANDOFF") || {
        print_error "cannot write the GitHub token hand-off file in $dir."
        return 1
    }
}

# ccy_token_handoff_drop — remove a hand-off file not yet taken.
ccy_token_handoff_drop() {
    [ -n "${CCY_GH_TOKEN_HANDOFF:-}" ] || return 0
    rm -rf -- "$(dirname -- "$CCY_GH_TOKEN_HANDOFF")"
    unset CCY_GH_TOKEN_HANDOFF
}

# ccy_token_handoff_insulate <project> <launcher> [args...] — ccy_tmux_insulate with the token
# handed on. When insulation does not apply it returns here, and this launcher takes the
# file back itself.
ccy_token_handoff_insulate() {
    ccy_token_handoff_give || return 1
    ccy_tmux_insulate "$@" || return 1
    ccy_token_handoff_take
}

ccy_token_handoff_take || exit 1

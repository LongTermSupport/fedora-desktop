#!/bin/bash
# The caller's exported GH_TOKEN, carried to the launcher that an interactive launch re-runs
# inside tmux (lib/tmux-session.bash). The launcher takes the export off the token at its
# first line, so the re-run would not see it, and exporting it again would be worse: tmux's
# server keeps the environment of the client that started it and gives it to every later
# session, so one launch's token would reach another's panes. So the token goes through a
# file: fresh, 0600, in a 0700 directory made for it under XDG_RUNTIME_DIR (else TMPDIR).
# Only the file's path travels, as CCY_GH_TOKEN_HANDOFF on the pane's command, and the
# launcher that finds it reads the token and deletes the file as soon as this library is
# sourced, which is the first thing it does after its first line. A new session's outer
# launcher ends in `exec tmux attach-session`, which drops its EXIT trap, so only that taking
# removes the file; an inner launcher that never starts leaves it in the user's own 0700
# directory (XDG_RUNTIME_DIR is emptied at logout).
#
# No line here expands the token where `set -x` (--debug) would trace it.
#
# Sourced by the launcher before every other library, common.bash included, so it prints
# its own errors.

# ccy_token_handoff_take — when CCY_GH_TOKEN_HANDOFF names a hand-off file, set GH_TOKEN
# from it (the launcher's first line has taken the export off it) and delete the file and
# its directory. Only an absolute <base>/ccy-gh-token.XXXXXX/token is taken, <base> one of
# this launcher's XDG_RUNTIME_DIR, /run/user/<uid>, TMPDIR and /tmp, resolved as
# ccy_token_handoff_give resolves it: the pane's environment is the tmux server's, not the
# outer launcher's, so the two can name different bases. The directory must be the user's,
# 0700 and not a link, the file the user's, 0600, one link and not a link: anything else is
# refused, unread and left in place, and the caller ends the launch. An empty file is
# refused and removed.
ccy_token_handoff_take() {
    local file="${CCY_GH_TOKEN_HANDOFF:-}" dir candidate known=no uid
    unset CCY_GH_TOKEN_HANDOFF
    [ -n "$file" ] || return 0
    # id -u, not $EUID (bash takes EUID from an exported variable of that name), and the
    # system's id, not one a PATH entry puts first.
    uid="$(command -p id -u)" || return 1
    dir="${file%/token}"
    for candidate in "${XDG_RUNTIME_DIR:-}" "/run/user/$uid" "${TMPDIR:-}" /tmp; do
        [ ! -d "$candidate" ] || [ "$(cd -P -- "$candidate" && pwd)" != "${dir%/*}" ] || known=yes
    done
    # %y%m is type and mode: "d700 f600 " is a real directory and a regular file, the file alone with one link.
    if [ "$known" != yes ] || [[ ! "$file" =~ ^/.*/ccy-gh-token\.[[:alnum:]]{6}/token$ ]] \
        || [ "$(find "$dir" "$file" -maxdepth 0 -user "$uid" \( -type d -o -links 1 \) -printf '%y%m ')" != 'd700 f600 ' ]; then
        printf 'ERROR: CCY_GH_TOKEN_HANDOFF=%s is not a GitHub token hand-off file made by ccy (<XDG_RUNTIME_DIR, /run/user/%s, TMPDIR or /tmp>/ccy-gh-token.XXXXXX/token, 0600 and one link, in a 0700 directory, both yours); it was not read and is left in place.\n' \
            "$file" "$uid" >&2
        return 1
    fi
    # read fails at an end of file with no newline, which a launcher before 3.99.13.17 wrote,
    # and succeeds with an empty value on a file of one newline: the value is what counts.
    { IFS= read -r GH_TOKEN || [ -n "${GH_TOKEN:+set}" ]; } <"$file" \
        && [ -n "${GH_TOKEN:+set}" ] \
        || printf 'ERROR: the GitHub token hand-off file %s (CCY_GH_TOKEN_HANDOFF) is empty; it is removed.\n' "$file" >&2
    rm -f -- "$file" && rmdir -- "$dir" && [ -n "${GH_TOKEN:+set}" ]
}

# ccy_token_handoff_give — write GH_TOKEN, when set, to a fresh hand-off file and set
# CCY_GH_TOKEN_HANDOFF (not exported) to its path. The EXIT trap removes it on every exit
# except an exec, which replaces the launcher and drops the trap.
ccy_token_handoff_give() {
    [ -n "${GH_TOKEN:+set}" ] || return 0
    local base dir
    if ! base="$(cd -P -- "${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}" && pwd)" \
        || ! dir="$(mktemp -d "$base/ccy-gh-token.XXXXXX")"; then
        printf 'ERROR: cannot make a private directory in %s to hand the GitHub token to the session.\n' \
            "${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}" >&2
        return 1
    fi
    CCY_GH_TOKEN_HANDOFF="$dir/token"
    trap ccy_token_handoff_drop EXIT
    # set +x in the subshell keeps the token out of a --debug trace, the caller's unchanged.
    (set +x && umask 077 && printf '%s\n' "$GH_TOKEN" >"$CCY_GH_TOKEN_HANDOFF") || {
        printf 'ERROR: cannot write the GitHub token hand-off file in %s.\n' "$dir" >&2
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
    ccy_token_handoff_give && ccy_tmux_insulate "$@" && ccy_token_handoff_take
}

ccy_token_handoff_take || exit 1

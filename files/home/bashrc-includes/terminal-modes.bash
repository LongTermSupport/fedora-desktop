# shellcheck shell=bash
# terminal-modes.bash — switch off, at every prompt, the terminal modes a full-screen
# program turns on and is meant to turn off again on exit. play-basic-configs.yml deploys
# it for the desktop user only.
#
# A program killed before it can restore them leaves them on: an SSH session whose server
# went away is the usual one, since the remote program died with the connection. The local
# terminal then keeps reporting mouse movement, focus changes and keys as escape sequences,
# which land on the bash prompt as garbage. `reset` fixes that but clears the scrollback;
# this only writes "off" sequences, so it is invisible and safe to repeat. It does not
# leave the alternate screen (`\e[?1049l` outside it can restore a stale saved cursor) and
# clears nothing.
#
# Bracketed paste is switched off here too, and is not lost: readline switches it on
# again each time it starts reading a line, which is after PROMPT_COMMAND has run.

[[ $- == *i* ]] || return 0

# bash hands every PROMPT_COMMAND element the command's own $?, whatever the element
# before it returned; this still returns that status, so it can sit anywhere in the array.
__terminal_modes_off() {
    local status=$?
    if [[ -t 1 && "${TERM-}" != dumb ]]; then
        # Mouse reporting (1000 1002 1003, encodings 1005 1006 1015), focus events (1004),
        # bracketed paste (2004), xterm modifyOtherKeys back to its default (>4m), kitty
        # keyboard protocol: pop 99 entries, which empties the stack (<99u), cursor shown.
        printf '\e[?1000l\e[?1002l\e[?1003l\e[?1005l\e[?1006l\e[?1015l\e[?1004l\e[?2004l\e[>4m\e[<99u\e[?25h'
    fi
    return "${status}"
}

if [[ " ${PROMPT_COMMAND[*]-} " != *" __terminal_modes_off "* ]]; then
    PROMPT_COMMAND+=(__terminal_modes_off)
fi

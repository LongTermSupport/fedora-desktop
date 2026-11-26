# shellcheck shell=bash
# history-record.bash — the recorder behind the Ctrl+R history search (Plan 00138;
# history-search.bash sources it, and play-basic-configs.yml deploys both for the desktop
# user only; root keeps stock Ctrl+R).
#
# For each command it notes the directory the command started in and its exit status,
# NUL-terminated, in ~/.local/state/bash/context. It uses bash builtins only, so a command
# line never appears in another process's argv. A command bash kept out of history
# (HISTCONTROL, HISTIGNORE) is kept out of this file too: a record is written only when
# the history number has moved.

[[ $- == *i* ]] || return 0
[[ ${EUID} -ne 0 ]] || return 0
# The history block in zz_lts-fedora-desktop.bash has already said why when this is
# missing; recording into it would fail at every prompt.
[[ -O "${HOME}/.local/state/bash" ]] || return 0

# __history_search_file <name> sets the variable <name> to the record file's path. A function,
# not a global: Claude Code's shell snapshot keeps functions but leaves out unexported
# variables (CLAUDE/QA.md, shell-snapshot-dropped-name). printf -v forks nothing.
__history_search_file() {
    printf -v "$1" '%s' "${HOME}/.local/state/bash/context"
}
__history_search_new=""
__history_search_file __history_search_new
if [[ ! -e "${__history_search_new}" ]]; then
    (umask 077 && : >>"${__history_search_new}")
fi
unset __history_search_new

# Kept if already set: a re-source (`source ~/.bashrc`) must not make the next prompt
# start over and drop the command that did the sourcing.
__history_search_number="${__history_search_number-}"
__history_search_dir="${__history_search_dir-${PWD}}"

# bash hands every PROMPT_COMMAND element the command's own $?, whatever the element
# before it returned, so this reads it first and can sit anywhere in the array.
__history_search_record() {
    local status=$? entry number command="" file
    __history_search_file file
    entry="$(HISTTIMEFORMAT='' builtin history 1)"
    if [[ "${entry}" =~ ^[[:space:]]*([0-9]+)[*[:space:]][[:space:]](.*)$ ]]; then
        number="${BASH_REMATCH[1]}"
        command="${BASH_REMATCH[2]}"
    else
        number=0
    fi
    # The first prompt only learns where history stands: any entry it sees was not run
    # in this shell.
    if [[ -n "${__history_search_number}" && "${number}" != "${__history_search_number}" && -n "${command}" ]]; then
        printf '%s\t%s\t%s\t%s\0' "${EPOCHSECONDS}" "${status}" \
            "${__history_search_dir}" "${command}" >>"${file}"
    fi
    __history_search_number="${number}"
    __history_search_dir="${PWD}"
}

if [[ " ${PROMPT_COMMAND[*]-} " != *" __history_search_record "* ]]; then
    PROMPT_COMMAND+=(__history_search_record)
fi

# Up-arrow, `history`, `!prefix` and `fc` see only this terminal's commands, since the
# Ctrl+R search (history-search.bash) reads every terminal's from the file itself. bash
# loads HISTFILE after the rc files, so it names nothing until the first prompt, where
# __history_append (the history block in zz_lts-fedora-desktop.bash) points it back at the
# shared file. The EXIT trap does the same for a session whose prompt hook never ran
# (something replaced PROMPT_COMMAND), when it ends by `exit` or Ctrl+D. Closing the
# terminal is not covered: on SIGHUP bash saves history before any trap runs. An EXIT trap
# already set is left alone. Only while the search is bound (fzf present): stock Ctrl+R
# searches the loaded list, and must see all. It lives here, not in history-search.bash,
# because that file is held at its size by the bash-limits ratchet.
if command -v fzf >/dev/null && [[ "${HISTFILE-}" == "${HOME}/.local/state/bash/history" ]]; then
    # __history_shared_file <name> sets <name> to the history file every terminal shares.
    # Defined only here, where HISTFILE is pointed away from it, so a HISTFILE of the
    # user's own is never pointed back. A function, not a global: Claude Code's shell
    # snapshot keeps functions but drops unexported variables.
    __history_shared_file() {
        printf -v "$1" '%s' "${HOME}/.local/state/bash/history"
    }
    HISTFILE=/dev/null
    if [[ -z "$(trap -p EXIT)" ]]; then
        trap '__history_shared_file HISTFILE' EXIT
    fi
fi

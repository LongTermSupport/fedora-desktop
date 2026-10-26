# shellcheck shell=bash
# history-search.bash — Ctrl+R over every terminal's history, ranked for the current
# directory (Plan 00138). play-basic-configs.yml deploys it for the desktop user only;
# root keeps stock Ctrl+R.
#
# The recorder notes, for each command, the directory it started in and its exit status,
# NUL-terminated, in ~/.local/state/bash/context. It uses bash builtins only, so a command
# line never appears in another process's argv. A command bash kept out of history
# (HISTCONTROL, HISTIGNORE) is kept out of this file too: a record is written only when
# the history number has moved.
#
# Ctrl+R: bash-history-rank orders all history — this directory first, then this git
# repository, then everything — and fzf draws it. The pick replaces the line for review;
# it is never run straight away.

[[ $- == *i* ]] || return 0
[[ ${EUID} -ne 0 ]] || return 0
# The history block in zz_lts-fedora-desktop.bash has already said why when this is
# missing; recording into it would fail at every prompt.
[[ -O "${HOME}/.local/state/bash" ]] || return 0

__history_search_file="${HOME}/.local/state/bash/context"
if [[ ! -e "${__history_search_file}" ]]; then
    (umask 077 && : >>"${__history_search_file}")
fi

# Kept if already set: a re-source (`source ~/.bashrc`) must not make the next prompt
# start over and drop the command that did the sourcing.
__history_search_number="${__history_search_number-}"
__history_search_dir="${__history_search_dir-${PWD}}"

# bash hands every PROMPT_COMMAND element the command's own $?, whatever the element
# before it returned, so this reads it first and can sit anywhere in the array.
__history_search_record() {
    local status=$? entry number command=""
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
            "${__history_search_dir}" "${command}" >>"${__history_search_file}"
    fi
    __history_search_number="${number}"
    __history_search_dir="${PWD}"
}

if [[ " ${PROMPT_COMMAND[*]-} " != *" __history_search_record "* ]]; then
    PROMPT_COMMAND+=(__history_search_record)
fi

__history_search() {
    local context="${__history_search_file}" picked
    # What is typed so far becomes the query through fzf's ENVIRONMENT, which only this user
    # can read. As a --query argument it would sit in fzf's argv, readable by every local
    # user through /proc for as long as the list is open. The bound command is fixed text,
    # spelt in the call so the hidden-bindings gate can read it. The long --bind is Esc: fzf
    # reads an Esc and the key typed just after it as that key with Alt, so each is bound to
    # abort, but alt-b, alt-f, alt-d and alt-bs keep fzf's word editing of the query
    # (project_esc_joined, CLAUDE/GoProjectRules.md).
    if [[ ! -r "${context}" ]]; then
        context=/dev/null
    fi
    if ! picked="$(bash-history-rank "${context}" "${HISTFILE}" "${PWD}" |
        __history_search_query="${READLINE_LINE}" fzf --read0 --scheme=history --tiebreak=index \
            --height=40% --layout=reverse --prompt='history> ' --bind=ctrl-r:toggle-sort --bind='alt-up:abort,alt-down:abort,alt-left:abort,alt-right:abort,alt-enter:abort,alt-space:abort,alt-a:abort,alt-c:abort,alt-e:abort,alt-g:abort,alt-h:abort,alt-i:abort,alt-j:abort,alt-k:abort,alt-l:abort,alt-m:abort,alt-n:abort,alt-o:abort,alt-p:abort,alt-q:abort,alt-r:abort,alt-s:abort,alt-t:abort,alt-u:abort,alt-v:abort,alt-w:abort,alt-x:abort,alt-y:abort,alt-z:abort,ctrl-alt-a:abort,ctrl-alt-b:abort,ctrl-alt-c:abort,ctrl-alt-d:abort,ctrl-alt-e:abort,ctrl-alt-f:abort,ctrl-alt-g:abort,ctrl-alt-h:abort,ctrl-alt-i:abort,ctrl-alt-j:abort,ctrl-alt-k:abort,ctrl-alt-l:abort,ctrl-alt-m:abort,ctrl-alt-n:abort,ctrl-alt-o:abort,ctrl-alt-p:abort,ctrl-alt-q:abort,ctrl-alt-r:abort,ctrl-alt-s:abort,ctrl-alt-t:abort,ctrl-alt-u:abort,ctrl-alt-v:abort,ctrl-alt-w:abort,ctrl-alt-x:abort,ctrl-alt-y:abort,ctrl-alt-z:abort,alt-0:abort,alt-1:abort,alt-2:abort,alt-3:abort,alt-4:abort,alt-5:abort,alt-6:abort,alt-7:abort,alt-8:abort,alt-9:abort' \
            --bind="start:transform-query:printf %s \"\${__history_search_query}\"")"; then
        return 0
    fi
    READLINE_LINE="${picked}"
    READLINE_POINT="${#picked}"
}

if command -v fzf >/dev/null; then
    bind -m emacs-standard -x '"\C-r": __history_search'
    bind -m vi-insert -x '"\C-r": __history_search'
    bind -m vi-command -x '"\C-r": __history_search'
    # Up-arrow, `history`, `!prefix` and `fc` see only this terminal's commands, since this
    # Ctrl+R reads every terminal's from the file itself. bash loads HISTFILE after the rc
    # files, so it names nothing until the first prompt, where __history_append (the
    # history block in zz_lts-fedora-desktop.bash) points it back at the shared file.
    # The EXIT trap does the same for a session whose prompt hook never ran (something
    # replaced PROMPT_COMMAND), when it ends by `exit` or Ctrl+D. Closing the terminal is
    # not covered: on SIGHUP bash saves history before any trap runs. An EXIT trap already
    # set is left alone. Kept only when this
    # search is bound: without it, stock Ctrl+R searches the loaded list, and must see all.
    if [[ "${HISTFILE-}" == "${HOME}/.local/state/bash/history" ]]; then
        __history_shared_file="${HISTFILE}"
        HISTFILE=/dev/null
        if [[ -z "$(trap -p EXIT)" ]]; then
            trap 'HISTFILE="${__history_shared_file}"' EXIT
        fi
    fi
else
    echo "history search: fzf is not installed, so Ctrl+R is the stock search. Re-run play-basic-configs.yml." >&2
fi

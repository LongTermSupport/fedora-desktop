# shellcheck shell=bash
# history-search.bash — Ctrl+R over every terminal's history, ranked for the current
# directory (Plan 00138). play-basic-configs.yml deploys it for the desktop user only;
# root keeps stock Ctrl+R.
#
# Ctrl+R: bash-history-rank orders all history — this directory first, then this git
# repository, then everything — and fzf draws it. The pick replaces the line for review;
# it is never run straight away. The recorder that feeds it is history-record.bash.

[[ $- == *i* ]] || return 0
[[ ${EUID} -ne 0 ]] || return 0
[[ -O "${HOME}/.local/state/bash" ]] || return 0

# shellcheck source-path=SCRIPTDIR
# shellcheck source=history-record.bash
source "${BASH_SOURCE[0]%/*}/history-record.bash"

__history_search() {
    local context="${__history_search_file}" picked status=0 take=query moved
    # What is typed so far becomes the query through fzf's ENVIRONMENT, which only this user
    # can read. As a --query argument it would sit in fzf's argv, readable by every local
    # user through /proc for as long as the list is open. The bound command is fixed text,
    # spelt in the call so the hidden-bindings gate can read it. The long --bind is Esc: fzf
    # reads an Esc and the key typed just after it as that key with Alt, so each is bound to
    # abort, but alt-b, alt-f, alt-d and alt-bs keep fzf's word editing of the query
    # (project_esc_joined, CLAUDE/GoProjectRules.md).
    # Enter takes what was typed until the person moves the cursor, then the highlighted
    # match; typing again returns to the query. Only events carry that (a key bound to
    # anything but a move or an accept is a hidden binding): a move touches a file in a
    # private directory, a change removes it, and both change the prompt to say what Enter
    # will take. --print-query hands back both, and the file says which to use. An empty
    # query has nothing to keep, so Enter takes the match. SHELL pins the shell fzf runs the
    # bound commands with.
    [[ -r "${context}" ]] || context=/dev/null
    moved="$(mktemp -d)"
    picked="$(bash-history-rank "${context}" "${HISTFILE}" "${PWD}" |
        __history_search_query="${READLINE_LINE}" __history_search_moved="${moved}/m" SHELL=/bin/bash fzf --read0 --print-query --scheme=history --tiebreak=index \
            --height=40% --layout=reverse --prompt='history [Enter: this match]> ' --footer='nohist=1 CMD: run unrecorded (any case) · histclean TEXT: remove from history' --bind=ctrl-r:toggle-sort --bind='alt-up:abort,alt-down:abort,alt-left:abort,alt-right:abort,alt-enter:abort,alt-space:abort,alt-a:abort,alt-c:abort,alt-e:abort,alt-g:abort,alt-h:abort,alt-i:abort,alt-j:abort,alt-k:abort,alt-l:abort,alt-m:abort,alt-n:abort,alt-o:abort,alt-p:abort,alt-q:abort,alt-r:abort,alt-s:abort,alt-t:abort,alt-u:abort,alt-v:abort,alt-w:abort,alt-x:abort,alt-y:abort,alt-z:abort,ctrl-alt-a:abort,ctrl-alt-b:abort,ctrl-alt-c:abort,ctrl-alt-d:abort,ctrl-alt-e:abort,ctrl-alt-f:abort,ctrl-alt-g:abort,ctrl-alt-h:abort,ctrl-alt-i:abort,ctrl-alt-j:abort,ctrl-alt-k:abort,ctrl-alt-l:abort,ctrl-alt-m:abort,ctrl-alt-n:abort,ctrl-alt-o:abort,ctrl-alt-p:abort,ctrl-alt-q:abort,ctrl-alt-r:abort,ctrl-alt-s:abort,ctrl-alt-t:abort,ctrl-alt-u:abort,ctrl-alt-v:abort,ctrl-alt-w:abort,ctrl-alt-x:abort,ctrl-alt-y:abort,ctrl-alt-z:abort,alt-0:abort,alt-1:abort,alt-2:abort,alt-3:abort,alt-4:abort,alt-5:abort,alt-6:abort,alt-7:abort,alt-8:abort,alt-9:abort' \
            --bind="change:transform:rm -f \"\${__history_search_moved}\"; if [ -n \"\${FZF_QUERY}\" ]; then echo 'change-prompt(history [Enter: as typed]> )'; else echo 'change-prompt(history [Enter: this match]> )'; fi" \
            --bind="focus:transform:case \"\${FZF_ACTION}\" in up|down|page-up|page-down|half-page-up|half-page-down|first|last|*click*|*scroll*) : >\"\${__history_search_moved}\"; echo 'change-prompt(history [Enter: this match]> )';; esac" \
            --bind="start:transform-query:printf %s \"\${__history_search_query}\"")" || status=$?
    [[ -e "${moved}/m" ]] && take=match
    rm -rf "${moved}"
    [[ ${status} -le 1 ]] || return 0
    if [[ ${take} == query && -n "${picked%%$'\n'*}" ]]; then picked="${picked%%$'\n'*}"; else picked="${picked#*$'\n'}"; fi
    [[ -n "${picked}" ]] || return 0
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

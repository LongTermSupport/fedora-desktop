#!/usr/bin/env bash
# Fixture of the shell half of the rule project_esc_joined (scripts/qa-hidden-bindings.bash
# proves it against this file on every run). A line whose comment says finding must be reported
# and no other line may be. The gate never judges this tree as code.
set -euo pipefail

# fzf reads an Esc and the key after it, inside its 100 ms wait, as that key with Alt: a screen
# that binds none of those keys loses the Esc.
pick() {
    local rows out
    rows=$(cat -)
    out=$(printf '%s\n' "${rows}" | fzf --ansi --header="$1" --bind='q:abort') || return $? # finding
    printf '%s\n' "${out}"
}

history() {
    __q="${READLINE_LINE}" fzf --read0 --bind=ctrl-r:toggle-sort # finding
    command fzf --height=40% # finding
    fzf --bind='alt-up:abort,alt-down:abort,alt-left:abort,alt-right:abort' # finding
}

leaves() {
    if [[ -z "$(command -v fzf)" ]]; then
        return 1
    fi
    fzf --bind='q:abort' \
        --bind='alt-up:abort,alt-down:abort,alt-left:abort,alt-right:abort,alt-enter:abort,alt-space:abort' \
        --bind='alt-a:abort,alt-c:abort,alt-e:abort,alt-g:abort,alt-h:abort,alt-i:abort,alt-j:abort,alt-k:abort,alt-l:abort,alt-m:abort,alt-n:abort,alt-o:abort,alt-p:abort,alt-q:abort,alt-r:abort,alt-s:abort,alt-t:abort,alt-u:abort,alt-v:abort,alt-w:abort,alt-x:abort,alt-y:abort,alt-z:abort' \
        --bind 'ctrl-alt-a:abort,ctrl-alt-b:abort,ctrl-alt-c:abort,ctrl-alt-d:abort,ctrl-alt-e:abort,ctrl-alt-f:abort,ctrl-alt-g:abort,ctrl-alt-h:abort,ctrl-alt-i:abort,ctrl-alt-j:abort,ctrl-alt-k:abort,ctrl-alt-l:abort,ctrl-alt-m:abort,ctrl-alt-n:abort,ctrl-alt-o:abort,ctrl-alt-p:abort,ctrl-alt-q:abort,ctrl-alt-r:abort,ctrl-alt-s:abort,ctrl-alt-t:abort,ctrl-alt-u:abort,ctrl-alt-v:abort,ctrl-alt-w:abort,ctrl-alt-x:abort,ctrl-alt-y:abort,ctrl-alt-z:abort' \
        --bind='alt-0:abort,alt-1:abort,alt-2:abort,alt-3:abort,alt-4:abort,alt-5:abort,alt-6:abort,alt-7:abort,alt-8:abort,alt-9:abort' \
        --bind='alt-b:backward-word'
}

# fzf's word editing keys bound to abort throw the typed query away.
edits() {
    fzf --bind='alt-up:abort,alt-down:abort,alt-left:abort,alt-right:abort,alt-enter:abort,alt-space:abort,alt-a:abort,alt-c:abort,alt-e:abort,alt-g:abort,alt-h:abort,alt-i:abort,alt-j:abort,alt-k:abort,alt-l:abort,alt-m:abort,alt-n:abort,alt-o:abort,alt-p:abort,alt-q:abort,alt-r:abort,alt-s:abort,alt-t:abort,alt-u:abort,alt-v:abort,alt-w:abort,alt-x:abort,alt-y:abort,alt-z:abort,ctrl-alt-a:abort,ctrl-alt-b:abort,ctrl-alt-c:abort,ctrl-alt-d:abort,ctrl-alt-e:abort,ctrl-alt-f:abort,ctrl-alt-g:abort,ctrl-alt-h:abort,ctrl-alt-i:abort,ctrl-alt-j:abort,ctrl-alt-k:abort,ctrl-alt-l:abort,ctrl-alt-m:abort,ctrl-alt-n:abort,ctrl-alt-o:abort,ctrl-alt-p:abort,ctrl-alt-q:abort,ctrl-alt-r:abort,ctrl-alt-s:abort,ctrl-alt-t:abort,ctrl-alt-u:abort,ctrl-alt-v:abort,ctrl-alt-w:abort,ctrl-alt-x:abort,ctrl-alt-y:abort,ctrl-alt-z:abort,alt-0:abort,alt-1:abort,alt-2:abort,alt-3:abort,alt-4:abort,alt-5:abort,alt-6:abort,alt-7:abort,alt-8:abort,alt-9:abort,alt-bs:abort' # finding
}

# Not caught, kept as the record of what the rule leaves out (CLAUDE/GoProjectRules.md): the
# bindings spread in from an array (counted as missing, so reported), fzf's options from
# FZF_DEFAULT_OPTS, and gum, whose prompts cannot be given bindings.
spread() {
    local -a opts=(--bind='q:abort')
    fzf "${opts[@]}" # finding
}

pick "$@"

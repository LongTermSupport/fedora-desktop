#!/usr/bin/env bash
# Fixture of the rule project_hidden_bindings (scripts/qa-hidden-bindings.bash proves its
# shell half against this file on every run). A line whose comment says finding must be reported and
# no other line may be. The gate never judges this tree as code.
set -euo pipefail

pick() {
    local spec="$1" header="$2"
    local -a opts=(--bind='start:last')
    opts+=(--expect="$3") # finding
    fzf --expect=ctrl-n # finding
    fzf --expect ctrl-x # finding
    fzf --bind='ctrl-a:become(x)' # finding
    fzf --bind 'ctrl-x:execute(rm {})' # finding
    fzf --bind="${spec}" # finding
    fzf --bind="focus:transform-header^${header}^,ctrl-o:become(x)" # finding
    fzf "${opts[@]}" --bind='q:abort' --bind=ctrl-a:accept --bind=ctrl-w:change-preview-window --bind=ctrl-r:toggle-sort --bind='tab:toggle+down'
    fzf --print-query --bind='ctrl-s:put(sync)+accept' # finding
    fzf --multi --bind=ctrl-a:select-all+accept # finding
    fzf --multi --bind='tab:toggle+accept' --bind='ctrl-y:accept+yank'
    fzf --bind='start:transform-query:printf %s query'
    fzf --bind="focus:transform-header^${header};; esac^"
}

# A scalar spread unquoted into fzf, and fzf's options from the environment, are read word by
# word; a scalar that does not start with an option is not argv.
scalars() {
    local flags='--height=40% --expect=ctrl-a' # finding
    export FZF_DEFAULT_OPTS='--bind=ctrl-k:become(x)' # finding
    local label='press --expect to see'
    local -a words
    read -r -a words <<<"${flags}"
    fzf "${words[@]}" --header="${label}"
}

# Not caught, kept as the record of what the rule leaves out (CLAUDE/GoProjectRules.md): fzf's
# options set in another file, and a single-key read that is not fzf.
not_caught() {
    local choice
    read -r -n1 choice
    printf '%s\n' "${choice}"
}

pick "$@"

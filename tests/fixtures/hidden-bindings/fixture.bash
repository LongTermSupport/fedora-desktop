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
    fzf --bind=ctrl-a:accept # finding
    fzf --bind 'ctrl-x:execute(rm {})' # finding
    fzf --bind="${spec}" # finding
    fzf --bind="focus:transform-header^${header}^,ctrl-o:accept" # finding
    fzf "${opts[@]}" --bind='q:abort' --bind=ctrl-r:toggle-sort --bind='tab:toggle+down'
    fzf --bind='start:transform-query:printf %s query'
    fzf --bind="focus:transform-header^${header};; esac^"
}

pick "$@"

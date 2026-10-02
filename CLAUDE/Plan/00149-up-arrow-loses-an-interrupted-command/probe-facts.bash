#!/usr/bin/env bash
# probe-facts.bash <report> <repo root> — Plan 00149: the facts around bash history on this
# host. Read-only; appends markdown to <report>.
#
# - which bash runs, and from which package;
# - whether the deployed history files match this checkout;
# - every line in the startup files that touches history, PROMPT_COMMAND, traps or key
#   bindings, so a setting that overrides this repo's can be found by name;
# - which terminal this ran in.
set -euo pipefail

report="${1:?usage: probe-facts.bash <report> <repo root>}"
repo="${2:?usage: probe-facts.bash <report> <repo root>}"

# pair <deployed> <checkout copy> — SAME, MISSING, or DIFFERS with the diff. diff exits 1 for
# "differs" and 2 for "could not compare"; only 2 is a failure.
pair() {
    local deployed="$1" source="$2" rc=0
    if [[ ! -e "$deployed" ]]; then
        echo "MISSING  $deployed"
        return 0
    fi
    if cmp -s "$deployed" "$source"; then
        echo "SAME     $deployed"
        return 0
    fi
    echo "DIFFERS  $deployed (diff follows)"
    diff "$source" "$deployed" || rc=$?
    if [[ "$rc" -gt 1 ]]; then
        echo "diff could not compare $source with $deployed (exit $rc)" >&2
        return 1
    fi
}

# matches <file> — the lines that touch history, prompts, traps or bindings. grep exits 1
# for "no match" and 2 for "could not read"; only 2 is a failure.
matches() {
    local f="$1" rc=0
    grep -n -H -E 'HIST|history|PROMPT_COMMAND|trap |bind |set -o|shopt' "$f" || rc=$?
    if [[ "$rc" -eq 1 ]]; then
        echo "(none)  $f"
    elif [[ "$rc" -gt 1 ]]; then
        echo "grep could not read $f (exit $rc)" >&2
        return 1
    fi
}

files=(/etc/bashrc /etc/profile "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.inputrc" /etc/inputrc)
for f in /etc/profile.d/*.sh /etc/profile.d/*.bash "$HOME"/.bashrc-includes/* "$HOME"/.bashrc.d/*; do
    if [[ -f "$f" ]]; then
        files+=("$f")
    fi
done

{
    echo "## Facts"
    echo ""
    echo "### bash"
    echo ""
    echo '```text'
    bash --version | awk 'NR == 1'
    rpm -q bash
    echo '```'
    echo ""
    echo "### Deployed history files against this checkout"
    echo ""
    echo '```text'
    pair /etc/profile.d/zz_lts-fedora-desktop.bash "$repo/files/etc/profile.d/zz_lts-fedora-desktop.bash"
    pair "$HOME/.bashrc-includes/history-search.bash" "$repo/files/home/bashrc-includes/history-search.bash"
    echo '```'
    echo ""
    echo "### Startup lines that touch history, PROMPT_COMMAND, traps or bindings"
    echo ""
    echo '```text'
    for f in "${files[@]}"; do
        if [[ -f "$f" ]]; then
            matches "$f"
        fi
    done
    echo '```'
    echo ""
    echo "### Terminal"
    echo ""
    echo '```text'
    echo "TERM=${TERM-} VTE_VERSION=${VTE_VERSION-} TERM_PROGRAM=${TERM_PROGRAM-} TMUX=${TMUX:+set}"
    echo '```'
    echo ""
} >>"$report"
echo "facts written to $report"

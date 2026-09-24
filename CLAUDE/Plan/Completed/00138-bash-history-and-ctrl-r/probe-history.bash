#!/usr/bin/env bash
# probe-history.bash — gather FACTS about how bash history is configured and behaving.
#
# Fact-finding only: appends to the report file given as $1 and renders no verdict
# (PlanScriptStandards R9). READ-ONLY: reads shell init files, package metadata and the
# SHAPE of ~/.bash_history. It never prints a history line — history holds typed
# passwords and tokens — only counts, sizes, timestamps and matches in init files.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-history.bash /tmp/report.md
#
# READ THIS FOR:
#   "Effective settings in a fresh interactive shell" — what a new terminal really ends
#   up with after every init file has run, which is what decides whether history is
#   written per command or only on exit, and whether anything overrides the repo's values.
#   "History file shape" — a line count sitting exactly on a size limit is the signature
#   of truncation; zero timestamp lines means HISTTIMEFORMAT has never been active.
#
# EXIT CODES:
#   0  every probe reached a definite answer ("absent" IS an answer)
#   1  a probe could not be answered — the fact-finding is incomplete
#  64  usage error
set -euo pipefail

# ── R1 bootstrap ──────────────────────────────────────────────────────────────────────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
    printf 'usage: probe-history.bash <report-file>\n' >&2
    exit 64
fi

plan_require_host "it reads the host user's shell init files and history file"

# The file in use: the Bash Tweaks' location once deployed, bash's default before.
HISTORY_FILE="${HOME}/.local/state/bash/history"
if [[ ! -e "${HISTORY_FILE}" ]]; then
    HISTORY_FILE="${HOME}/.bash_history"
fi
readonly HISTORY_FILE
DEPLOYED_TWEAKS="/etc/profile.d/zz_lts-fedora-desktop.bash"
readonly DEPLOYED_TWEAKS

out() { printf '%s\n' "$*" >>"${REPORT}"; }

# Markdown code fence, built from octal escapes: a literal backtick anywhere in this file
# makes semgrep's bash parser reject the whole file, and the QA gate then runs no rule on it.
FENCE="$(printf '\140\140\140')"
readonly FENCE

# A non-zero exit is DATA, not a failure: record the rc and the output, carry on.
record() {
    local label="$1" rc="$2" result="$3"
    out ""
    out "### ${label}  (rc=${rc})"
    out ""
    out "${FENCE}"
    out "${result:-(no output)}"
    out "${FENCE}"
    return 0
}

probe() {
    local label="$1"
    shift
    local result rc
    if result="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    record "${label}" "${rc}" "${result}"
}

RESULT=""
RC=0

for tool in rpm bash grep awk stat cmp pgrep; do
    if ! command -v "${tool}" >/dev/null; then
        out "**${tool} is not on PATH** — this probe cannot answer from here."
        printf '[INCOMPLETE] %s is not on PATH\n' "${tool}" >&2
        exit 1
    fi
done

out ""
out "## Shell"
record "bash version" 0 "${BASH_VERSION}"
probe "login shell of this user" getent passwd "$(id -un)"

out ""
out "## Deployed tweaks file versus the checkout"
out ""
out "rc=0 means the deployed file is byte-identical to the repo copy."
probe "cmp deployed vs repo" cmp "${DEPLOYED_TWEAKS}" "${PLAN_REPO_ROOT}/files${DEPLOYED_TWEAKS}"

out ""
out "## Which init files touch history, the prompt hook or Ctrl+R"
out ""
out "Every match, file:line. Order of sourcing decides who wins: /etc/bashrc, then ~/.bashrc"
out "top to bottom (the repo's tweaks block, bash-git-prompt, ~/.bashrc-includes)."
show_init_matches() {
    local pattern='HIST|history|PROMPT_COMMAND|\\C-r|reverse-search|fzf|atuin|mcfly|hstr|preexec|ble\.sh|blesh'
    local f found=0
    for f in /etc/bashrc /etc/profile /etc/profile.d/* /etc/inputrc \
        "${HOME}/.bashrc" "${HOME}/.bash_profile" "${HOME}/.profile" "${HOME}/.inputrc" \
        "${HOME}"/.bashrc-includes/* "${HOME}"/.bashrc.d/* /var/local/ps1-prompt \
        "${HOME}/.bash-git-prompt/gitprompt.sh" "${HOME}/.bash-git-prompt/prompt-colors.sh"; do
        [[ -f "${f}" && -r "${f}" ]] || continue
        if grep -nHE "${pattern}" "${f}"; then
            found=1
        fi
    done
    if [[ "${found}" -ne 1 ]]; then
        echo "(no init file mentions history, PROMPT_COMMAND or Ctrl+R)"
    fi
}
if RESULT="$(show_init_matches 2>&1)"; then RC=0; else RC=$?; fi
record "init-file matches" "${RC}" "${RESULT}"

show_source_order() {
    local f
    for f in "${HOME}/.bashrc" "${HOME}/.bash_profile"; do
        echo "== ${f}"
        if [[ ! -f "${f}" ]]; then
            echo "(absent)"
            continue
        fi
        if ! grep -nE '^[[:space:]]*(source|\.)[[:space:]]|ANSIBLE MANAGED|/etc/bashrc' "${f}"; then
            echo "(sources nothing)"
        fi
    done
}
if RESULT="$(show_source_order 2>&1)"; then RC=0; else RC=$?; fi
record "what ~/.bashrc and ~/.bash_profile source, in order" "${RC}" "${RESULT}"

out ""
out "## Effective settings in a fresh interactive shell"
out ""
out "A child 'bash -i' runs every init file, prints the result between markers, then unsets"
out "HISTFILE so its exit writes nothing back to the real history file. Start-up chatter from"
out "init files falls outside the markers and is dropped."
show_effective() {
    local login_flag="${1:-}"
    # shellcheck disable=SC2016
    bash ${login_flag:+"${login_flag}"} -i -c '
        echo "@@BEGIN"
        for v in HISTFILE HISTSIZE HISTFILESIZE HISTCONTROL HISTIGNORE HISTTIMEFORMAT PROMPT_COMMAND FZF_CTRL_R_OPTS FZF_DEFAULT_OPTS; do
            declare -p "$v" 2>&1
        done
        shopt histappend cmdhist lithist histverify histreedit
        echo "-- Ctrl+R binding(s):"
        if ! bind -p 2>&1 | grep -F "\"\\C-r\""; then echo "(no readline binding for C-r)"; fi
        if ! bind -X 2>&1 | grep -F "C-r"; then echo "(no shell-command binding for C-r)"; fi
        echo "@@END"
        unset HISTFILE
    ' </dev/null 2>&1 | awk '/^@@BEGIN/{on=1; next} /^@@END/{on=0} on'
}
if RESULT="$(show_effective)"; then RC=0; else RC=$?; fi
record "effective history settings (bash -i)" "${RC}" "${RESULT}"
out ""
out "tmux starts every pane as a LOGIN shell, which reads ~/.bash_profile — and that sources"
out "the user's .bashrc and then the tweaks file a second time. Compare PROMPT_COMMAND with the above."
if RESULT="$(show_effective -l)"; then RC=0; else RC=$?; fi
record "effective history settings (bash -l -i, as tmux starts it)" "${RC}" "${RESULT}"

show_fedora_history_block() {
    if ! awk '/[Hh]istory/{on=1} on{print FILENAME ":" FNR ": " $0; if (++n >= 8) exit}' /etc/bashrc | grep .; then
        echo "(no history block in /etc/bashrc)"
    fi
}
if RESULT="$(show_fedora_history_block 2>&1)"; then RC=0; else RC=$?; fi
record "Fedora's own history block in /etc/bashrc" "${RC}" "${RESULT}"
probe "tmux default-command" tmux show-options -g default-command
probe "tmux default-shell" tmux show-options -g default-shell

out ""
out "## History file shape (no contents)"
out ""
out "Compare the line count with HISTFILESIZE above and with bash's default of 500: a count"
out "sitting exactly on a limit is what truncation leaves behind."
show_history_shape() {
    if [[ ! -e "${HISTORY_FILE}" ]]; then
        echo "${HISTORY_FILE} does not exist"
        return 0
    fi
    stat -c 'type=%F size=%s bytes owner=%U mode=%a modified=%y' "${HISTORY_FILE}"
    if [[ -L "${HISTORY_FILE}" ]]; then
        echo "symlink -> $(readlink -f "${HISTORY_FILE}")"
    fi
    awk '
        /^#[0-9]+$/ && length($0) >= 10 { ts++; if (!first) first=$0; last=$0; next }
        { cmds++; seen[$0]++ }
        END {
            printf "total lines: %d\n", NR
            printf "command lines: %d\n", cmds
            printf "timestamp lines: %d\n", ts
            u=0; d=0; for (k in seen) { u++; if (seen[k] > 1) d++ }
            printf "distinct commands: %d\n", u
            printf "commands appearing more than once: %d\n", d
            if (ts) printf "first timestamp: %s  last timestamp: %s\n", substr(first,2), substr(last,2)
        }' "${HISTORY_FILE}"
}
if RESULT="$(show_history_shape 2>&1)"; then RC=0; else RC=$?; fi
record "shape of ${HISTORY_FILE}" "${RC}" "${RESULT}"

list_other_history_files() {
    local found=0 f
    for f in "${HOME}"/.bash_history?* "${HOME}"/.bash_eternal_history "${HOME}/.local/share/atuin" \
        "${HOME}/.local/share/mcfly" "${HOME}/.mcfly" "${HOME}/.hstr_favorites"; do
        [[ -e "${f}" ]] || continue
        found=1
        stat -c '%n  %F  %s bytes  modified=%y' "${f}"
    done
    if [[ "${found}" -ne 1 ]]; then
        echo "(no other history stores)"
    fi
}
if RESULT="$(list_other_history_files 2>&1)"; then RC=0; else RC=$?; fi
record "other history stores in HOME" "${RC}" "${RESULT}"

out ""
out "## Concurrency — how many shells share the file right now"
count_shells() {
    local n tmux_out
    if ! n="$(pgrep -c -u "$(id -u)" -x bash)"; then
        n=0
    fi
    echo "bash processes owned by this user: ${n}"
    if [[ -e "${HISTORY_FILE}" ]]; then
        # A shell that started before the file's last write and is still alive holds every
        # command typed since in memory only — unless something appends per prompt.
        local file_age
        file_age=$(($(date +%s) - $(stat -c %Y "${HISTORY_FILE}")))
        echo "seconds since ${HISTORY_FILE} was last written: ${file_age}"
        echo "live bash processes started before that write: $(ps -u "$(id -u)" -o comm=,etimes= |
            awk -v age="${file_age}" '$1 == "bash" && $2 > age { c++ } END { print c + 0 }')"
    fi
    if ! command -v tmux >/dev/null; then
        echo "tmux: not installed"
    elif tmux_out="$(tmux ls 2>&1)"; then
        echo "tmux sessions: $(wc -l <<<"${tmux_out}")"
    else
        echo "tmux: ${tmux_out}"
    fi
}
if RESULT="$(count_shells 2>&1)"; then RC=0; else RC=$?; fi
record "live shells" "${RC}" "${RESULT}"

out ""
out "## Ctrl+R candidates installed"
probe "packages" rpm -q fzf atuin mcfly hstr bash-preexec blesh
show_fzf_shell_files() {
    local fzf_bash
    if ! rpm -q fzf >/dev/null; then
        echo "fzf not installed"
        return 0
    fi
    fzf --version
    if ! rpm -ql fzf | grep -E 'key-bindings|completion|profile\.d'; then
        echo "(fzf package ships no shell integration files)"
    fi
    if fzf_bash="$(fzf --bash 2>&1)"; then
        echo "fzf --bash: supported ($(wc -l <<<"${fzf_bash}") lines of integration script)"
    else
        echo "fzf --bash: NOT supported by this version: ${fzf_bash}"
    fi
}
if RESULT="$(show_fzf_shell_files 2>&1)"; then RC=0; else RC=$?; fi
record "fzf version and shipped shell integration" "${RC}" "${RESULT}"
list_binaries() {
    local b
    for b in fzf atuin mcfly hstr sk; do
        if command -v "${b}"; then :; else echo "${b}: absent"; fi
    done
}
if RESULT="$(list_binaries 2>&1)"; then RC=0; else RC=$?; fi
record "binaries on PATH" "${RC}" "${RESULT}"

exit 0

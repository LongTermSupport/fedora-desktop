#!/usr/bin/env bash
# Plan 00183 — triage-probe.bash: the probes behind triage.bash, one fact group per call.
#
# Usage: triage-probe.bash <probe> <report-file> [checkout]
#   prompt     the installed bash-git-prompt: its commit, its fetch code, and every
#              GIT_PROMPT_ setting the shell start-up files give it
#   tools      git, find -mmin and perl (what bash-git-prompt's fetch timer needs), and the
#              deployed git-fetch-age
#   fetchhead  every FETCH_HEAD of the checkout's repository (size, date, age), the tracking
#              refs against the remote's heads, and git-fetch-age's mark there
#   agent      the SSH agent this run sees and the keys in it
#   reach      whether the remote answers, as bash-git-prompt's fetch would ask it: with the
#              agent, and with no agent, never with a terminal to prompt on
#
# Read-only: it reads files and runs git ls-remote, git rev-parse, git for-each-ref, ssh-add -l
# and the tools' --version. It never fetches, so no ref and no FETCH_HEAD moves. Each fact goes
# to stdout and is appended to the report. A probe that cannot establish its fact exits
# non-zero, so triage.bash names the leg.
set -euo pipefail

probe="${1:?usage: triage-probe.bash <probe> <report-file> [checkout]}"
report="${2:?usage: triage-probe.bash <probe> <report-file> [checkout]}"
checkout="${3:-}"

readonly FENCE='```'
readonly PROMPT_DIR="${HOME}/.bash-git-prompt"
# How long a remote may take to answer before the probe calls it unanswered.
readonly REACH_TIMEOUT=30

fact() {
    printf '%s\n' "$1" | tee -a "${report}"
}

block() {
    printf '\n%s:\n\n%s\n%s\n%s\n' "$1" "${FENCE}" "$2" "${FENCE}" | tee -a "${report}"
}

# run <title> <cmd...> — a fenced block of the command's output; a non-zero exit is data.
run() {
    local title="$1" out rc
    shift
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    block "${title} (rc=${rc})" "${out:-(no output)}"
}

need_checkout() {
    if [[ -z "${checkout}" || ! -d "${checkout}" ]]; then
        printf '[ERROR] probe %s needs a checkout directory, got "%s"\n' "${probe}" "${checkout}" >&2
        return 1
    fi
}

probe_prompt() {
    if [[ ! -f "${PROMPT_DIR}/gitprompt.sh" ]]; then
        printf '[ERROR] %s/gitprompt.sh is missing; play-git-configure-and-tools.yml clones it\n' "${PROMPT_DIR}" >&2
        return 1
    fi
    run "installed bash-git-prompt commit" git -C "${PROMPT_DIR}" log -1 --format='%H %ci %s'
    run "installed bash-git-prompt: a shallow clone?" git -C "${PROMPT_DIR}" rev-parse --is-shallow-repository
    run "the last background fetch's words (git-prompt-fetch)" cat "${HOME}/.cache/git-prompt-fetch.log"
    run "local changes to the installed copy (empty = none)" git -C "${PROMPT_DIR}" status --porcelain
    run "its fetch code (FETCH_HEAD path, timer, fetch command)" grep -nE 'FETCH_HEAD|FETCH_TIMEOUT|FETCH_REMOTE_STATUS|git fetch|git-common-dir|olderThanMinutes \(' "${PROMPT_DIR}/gitprompt.sh"
    run "GIT_PROMPT_ settings in ~/.bashrc" grep -n 'GIT_PROMPT_\|bash-git-prompt' "${HOME}/.bashrc"
    run "GIT_PROMPT_ settings in other start-up files" grep -rn 'GIT_PROMPT_' "${HOME}/.bash_profile" "${HOME}/.profile" "${HOME}/.bashrc-includes" "${HOME}/.git-prompt-colors.sh" /etc/bashrc /etc/profile.d
}

probe_tools() {
    run "git --version" git --version
    run "find -mmin works (bash-git-prompt's fetch timer)" find "${HOME}" -maxdepth 0 -mmin +0
    run "gfind and perl (the timer's fallbacks)" which gfind perl
    run "git-fetch-age on PATH" which git-fetch-age
    run "git-fetch-age file" stat -c '%U:%G %a %s bytes %y %n' /usr/local/bin/git-fetch-age
    run "git-fetch-age checksum" sha256sum /usr/local/bin/git-fetch-age
    run "the deployed prompt calls git-fetch-age" grep -n 'git-fetch-age' /var/local/ps1-prompt
}

probe_fetchhead() {
    local common now path
    need_checkout
    common="$(git -C "${checkout}" rev-parse --path-format=absolute --git-common-dir)"
    now="$(date +%s)"
    fact "- checkout: ${checkout}"
    fact "- common git dir: ${common}"
    run "remotes" git -C "${checkout}" remote -v
    run "this worktree's own FETCH_HEAD path" git -C "${checkout}" rev-parse --path-format=absolute --git-path FETCH_HEAD
    local rows=""
    for path in "${common}/FETCH_HEAD" "${common}"/worktrees/*/FETCH_HEAD; do
        [[ -e "${path}" ]] || continue
        rows+="$(stat -c '%s bytes  %y' "${path}")  age $(((now - $(stat -c %Y "${path}")) / 60)) min  ${path}"$'\n'
    done
    block "every FETCH_HEAD of the repository (0 bytes = a fetch that failed or is running)" "${rows:-(none)}"
    run "upstream of the current branch" git -C "${checkout}" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}'
    run "ahead/behind against the local tracking ref (behind ahead)" git -C "${checkout}" rev-list --left-right --count '@{upstream}...HEAD'
    run "tracking refs (local, as of the last fetch)" git -C "${checkout}" for-each-ref --format='%(objectname) %(refname)' refs/remotes
    run "reflog of the upstream ref (each entry is a fetch that moved it)" git -C "${checkout}" reflog show --date=iso -n 10 '@{upstream}'
    run "git-fetch-age in the checkout (no output = no mark)" env -C "${checkout}" git-fetch-age
}

probe_agent() {
    fact "- SSH_AUTH_SOCK: ${SSH_AUTH_SOCK:-(unset)}"
    fact "- SSH_CONNECTION: ${SSH_CONNECTION:-(unset, not an SSH session)}"
    run "keys in the agent (ssh-add -l; rc 1 = agent with no keys, rc 2 = no agent)" ssh-add -l
    if [[ -n "${SSH_AUTH_SOCK:-}" ]]; then
        run "agent socket" stat -c '%U %a %n' "${SSH_AUTH_SOCK}"
    fi
    run "agent processes" ps -o pid=,args= -C ssh-agent,gcr-ssh-agent,gnome-keyring-daemon
}

# reach_as <title> <env...> — git ls-remote of the checkout's upstream remote, the way the
# prompt's background fetch would reach it: no terminal (setsid, stdin from /dev/null), no
# askpass window, GIT_TERMINAL_PROMPT=0. ls-remote writes nothing, unlike a fetch.
reach_as() {
    local title="$1" remote
    shift
    remote="$(git -C "${checkout}" config --get "branch.$(git -C "${checkout}" symbolic-ref --short HEAD).remote")"
    run "${title}: git ls-remote --heads ${remote}" env "$@" GIT_TERMINAL_PROMPT=0 SSH_ASKPASS_REQUIRE=never \
        timeout "${REACH_TIMEOUT}" setsid -w git -C "${checkout}" ls-remote --heads "${remote}" < /dev/null
}

probe_reach() {
    need_checkout
    reach_as "with this run's agent, as the prompt's fetch runs"
    reach_as "with this run's agent, ssh in BatchMode" GIT_SSH_COMMAND='ssh -o BatchMode=yes'
    reach_as "with no agent, as the prompt's fetch runs in a session without one" -u SSH_AUTH_SOCK
    reach_as "with no agent, ssh in BatchMode" -u SSH_AUTH_SOCK GIT_SSH_COMMAND='ssh -o BatchMode=yes'
}

case "${probe}" in
    prompt) probe_prompt ;;
    tools) probe_tools ;;
    fetchhead) probe_fetchhead ;;
    agent) probe_agent ;;
    reach) probe_reach ;;
    *)
        printf '[FATAL] unknown probe: %s\n' "${probe}" >&2
        exit 64
        ;;
esac

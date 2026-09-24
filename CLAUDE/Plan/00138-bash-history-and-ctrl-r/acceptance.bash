#!/usr/bin/env bash
# Plan 00138 — acceptance.bash
#
# PURPOSE: render the VERDICT on what deploy.bash left behind (CLAUDE/PlanScriptStandards.md
# R9): durable, timestamped, unlimited history for the user and root, each prompt hook
# present exactly once in login and non-login shells, and the ranked Ctrl+R search bound
# for the user and not for root. HOST ONLY. Run it from a terminal opened AFTER the deploy.
#
# It changes nothing except one line: check 10 runs a harmless marker command
# (`: plan-00138-acceptance-<timestamp>`) in a real interactive shell, so that line lands
# in the user's history and the recorder's file — that is the whole point of the check.
# Root's checks read /root through sudo.
#
# NOT ESTABLISHABLE by a script, named for the human at the end: how Ctrl+R feels in a real
# terminal (the list, the ranking, the pick landing on the line).
#
# Usage: ./acceptance.bash [-h|--help]
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
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
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash [-h|--help]

The Plan 00138 acceptance gate. Run deploy.bash first, then run this from a NEW
terminal. It checks the deployed files, the history directory and file modes, the
effective settings in fresh login and non-login shells, an end-to-end recording of
a marker command, and root's history. It needs sudo for root's checks.

EXIT STATUS
  0  ACCEPTED — every declared check ran and passed
  1  REJECTED — a check failed, or a declared check never ran
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks this host's deployed shell configuration and the user's history"
plan_prime_sudo
plan_start_log auto

readonly DECLARED=15
readonly USER_STATE="${HOME}/.local/state/bash"
readonly ROOT_STATE="/root/.local/state/bash"
PASS=0
FAIL=0

ok() {
    PASS=$((PASS + 1))
    printf '✓ %s\n' "$1"
}
bad() {
    FAIL=$((FAIL + 1))
    printf '✗ %s\n' "$1"
    if [[ -n "${2:-}" ]]; then
        printf '    %s\n' "$2"
    fi
}
# verdict <label> <want> <got>
verdict() {
    if [[ "$2" == "$3" ]]; then
        ok "$1"
    else
        bad "$1" "want: $2 | got: $3"
    fi
}

# effective <runner...> — the history settings, prompt hooks and Ctrl+R binding a fresh
# interactive shell ends up with after every init file, one KEY=value per line. HISTFILE is
# unset before the child exits, so reading them writes nothing to any history file.
effective() {
    # shellcheck disable=SC2016
    # The markers go on lines of their own and are matched anywhere: ps1-prompt prints a
    # terminal-title escape with no newline as it is sourced, which glued itself to the
    # start marker and made root's probe read as empty.
    "$@" '
        printf "\n@@BEGIN\n"
        printf "HISTFILE=%s\n" "${HISTFILE-unset}"
        printf "HISTSIZE=%s\n" "${HISTSIZE-unset}"
        printf "HISTFILESIZE=%s\n" "${HISTFILESIZE-unset}"
        printf "HISTCONTROL=%s\n" "${HISTCONTROL-unset}"
        printf "HISTTIMEFORMAT=%s\n" "${HISTTIMEFORMAT:+set}"
        if shopt -q lithist; then echo lithist=on; else echo lithist=off; fi
        printf "PC=%s\n" "${PROMPT_COMMAND[*]-}"
        printf "CR=%s\n" "$(bind -m emacs-standard -X 2>&1 | grep -F "C-r" | tr "\n" " ")"
        echo @@END
        unset HISTFILE
    ' </dev/null 2>>"${PLAN_RUN_DIR}/effective-stderr.log" | awk '/@@BEGIN$/ { on = 1; next } /^@@END$/ { on = 0 } on'
}
field() { awk -F= -v k="$1" '$1 == k { sub(/^[^=]*=/, ""); print }' <<<"$2"; }
# hook_counts <PC value> — how many times each hook this plan relies on appears. awk, not
# grep -c: a count of zero is an answer to report, not a failure to abort on.
hook_counts() {
    awk '
        BEGIN { split("ps1Prompt setGitPrompt __history_append __history_search_record", hooks, " ") }
        { for (i = 1; i <= NF; i++) seen[$i]++ }
        END { for (h = 1; h <= 4; h++) printf "%s%s=%d", (h > 1 ? " " : ""), hooks[h], seen[hooks[h]] }
    ' <<<"$1"
}
readonly ALL_ONCE="ps1Prompt=1 setGitPrompt=1 __history_append=1 __history_search_record=1"

echo "== deployed files"
# 1
missing=""
for pkg in fzf gawk; do
    if ! rpm -q "${pkg}" >/dev/null; then missing+="${pkg} "; fi
done
verdict "1. fzf and gawk are installed" "" "${missing}"

# 2
drift=""
while read -r deployed source; do
    if ! cmp -s "${deployed}" "${PLAN_REPO_ROOT}/${source}"; then drift+="${deployed} "; fi
done <<EOF
/etc/profile.d/zz_lts-fedora-desktop.bash files/etc/profile.d/zz_lts-fedora-desktop.bash
/var/local/ps1-prompt files/var/local/ps1-prompt
${HOME}/.bashrc-includes/history-search.bash files/home/bashrc-includes/history-search.bash
${HOME}/.local/bin/bash-history-rank files/home/.local/bin/bash-history-rank
EOF
verdict "2. the deployed files match this checkout" "" "${drift}"

echo "== the user's history files"
# 3, 4
verdict "3. ${USER_STATE} is 0700 and the user's" "700 $(id -un)" "$(stat -c '%a %U' "${USER_STATE}" 2>&1)"
verdict "4. the history file is 0600 and the user's" "600 $(id -un)" "$(stat -c '%a %U' "${USER_STATE}/history" 2>&1)"
# 5
if grep -q 'ANSIBLE MANAGED: Bash Tweaks' "${HOME}/.bash_profile"; then
    bad "5. ~/.bash_profile no longer loads the tweaks a second time" "the Bash Tweaks block is still there"
else
    ok "5. ~/.bash_profile no longer loads the tweaks a second time"
fi

echo "== a fresh shell, as a terminal tab and as a tmux pane"
plain="$(effective bash -i -c)"
login="$(effective bash -l -i -c)"
# 6
verdict "6. history settings in a fresh shell" \
    "HISTFILE=${USER_STATE}/history HISTSIZE=-1 HISTFILESIZE=-1 HISTCONTROL=ignoreboth HISTTIMEFORMAT=set lithist=on" \
    "HISTFILE=$(field HISTFILE "${plain}") HISTSIZE=$(field HISTSIZE "${plain}") HISTFILESIZE=$(field HISTFILESIZE "${plain}") HISTCONTROL=$(field HISTCONTROL "${plain}") HISTTIMEFORMAT=$(field HISTTIMEFORMAT "${plain}") lithist=$(field lithist "${plain}")"
# 7, 8
verdict "7. each prompt hook appears exactly once (terminal tab)" "${ALL_ONCE}" "$(hook_counts "$(field PC "${plain}")")"
verdict "8. each prompt hook appears exactly once (login shell, as tmux starts)" "${ALL_ONCE}" "$(hook_counts "$(field PC "${login}")")"
# 9
case "$(field CR "${plain}")" in
    *__history_search*) ok "9. Ctrl+R runs the ranked history search" ;;
    *) bad "9. Ctrl+R runs the ranked history search" "binding: $(field CR "${plain}")" ;;
esac

echo "== end to end: a command run in a real shell is recorded"
# 10
stamp="$(date +%s)"
marker=": plan-00138-acceptance-${stamp}"
hidden=": plan-00138-kept-out-${stamp}"
e2e_dir="${PLAN_RUN_DIR}"
# Without SSH_CONNECTION: over SSH with no agent, ~/.bashrc's agent prompt would read these
# lines from stdin as its answer.
(cd "${e2e_dir}" && printf ' %s\n%s\n' "${hidden}" "${marker}" |
    env -u SSH_CONNECTION bash -i >"${PLAN_RUN_DIR}/e2e-shell.log" 2>&1)
in_history="absent"
if [[ -r "${USER_STATE}/history" ]]; then
    in_history="$(awk -v m="${marker}" '$0 == m { found = (prev ~ /^#[0-9]+$/) ? "with timestamp" : "without timestamp" } { prev = $0 } END { print found ? found : "absent" }' "${USER_STATE}/history")"
fi
in_context=""
if [[ -r "${USER_STATE}/context" ]]; then
    in_context="$(tr '\0' '\n' <"${USER_STATE}/context" | awk -F'\t' -v m="${marker}" -v d="${e2e_dir}" '$4 == m { print ($3 == d && $2 == "0") ? "filed under its directory" : "filed wrongly: " $2 " " $3 }')"
fi
verdict "10. the marker reached the history file and the recorder" \
    "with timestamp|filed under its directory" "${in_history}|${in_context:-absent}"
# 11
leaks=""
for f in "${USER_STATE}/history" "${USER_STATE}/context"; do
    # An unreadable file proves nothing either way, so it cannot count as clean.
    if [[ ! -r "${f}" ]]; then
        leaks+="${f}(unreadable) "
    elif tr '\0' '\n' <"${f}" | grep -qF -- "${hidden}"; then
        leaks+="${f} "
    fi
done
verdict "11. a command typed with a leading space reached neither file" "" "${leaks}"

echo "== root: durable history, and no recorder or Ctrl+R search"
# 12
verdict "12. ${ROOT_STATE} is 0700 and root's" "700 root" "$(sudo stat -c '%a %U' "${ROOT_STATE}" 2>&1)"
root="$(effective sudo -H bash -i -c)"
# 13
verdict "13. root's history settings" "HISTFILE=${ROOT_STATE}/history HISTSIZE=-1 HISTFILESIZE=-1" \
    "HISTFILE=$(field HISTFILE "${root}") HISTSIZE=$(field HISTSIZE "${root}") HISTFILESIZE=$(field HISTFILESIZE "${root}")"
# 14
# A probe that returned nothing contains no __history_search either, so the absence only
# counts once root's shell has demonstrably answered.
case "$(field PC "${root}") $(field CR "${root}")" in
    " ") bad "14. root has no recorder and stock Ctrl+R" "root's shell probe returned nothing; see effective-stderr.log in the run directory" ;;
    *__history_search*) bad "14. root has no recorder and stock Ctrl+R" "PROMPT_COMMAND: $(field PC "${root}") | binding: $(field CR "${root}")" ;;
    *) ok "14. root has no recorder and stock Ctrl+R" ;;
esac
# 15
if sudo grep -q 'ANSIBLE MANAGED: Bash Tweaks' /root/.bash_profile; then
    bad "15. root's ~/.bash_profile no longer loads the tweaks a second time" "the Bash Tweaks block is still there"
else
    ok "15. root's ~/.bash_profile no longer loads the tweaks a second time"
fi

echo
ran=$((PASS + FAIL))
echo "COVERAGE: ${ran} of ${DECLARED} checks executed"
echo "NOT ESTABLISHABLE here:"
echo "  - how Ctrl+R feels in a real terminal inside a project: its own commands should head"
echo "    the list, the whole history should be searchable, and the pick should land on the"
echo "    line without running."
echo "  - that no command line reaches another process's argv: a live check cannot catch a"
echo "    process mid-flight. The bash-history-search QA gate proves the typed query goes to"
echo "    fzf through its environment and in none of its arguments; the recorder uses only"
echo "    builtins, so it starts no process that could carry one."
if [[ "${ran}" -ne "${DECLARED}" ]]; then
    echo "VERDICT: REJECTED — ${ran} of ${DECLARED} declared checks ran; an incomplete gate establishes nothing."
    PLAN_FAILED_LEGS="coverage"
elif [[ "${FAIL}" -ne 0 ]]; then
    echo "VERDICT: REJECTED — ${FAIL} of ${ran} checks failed."
    PLAN_FAILED_LEGS="acceptance"
else
    echo "VERDICT: ACCEPTED — ${PASS} of ${DECLARED} checks passed."
fi
plan_finish

#!/usr/bin/env bash
# Plan 00192 — deploy.bash
#
# WHAT THIS CHANGES (HOST-only; running it is the consent, CLAUDE/PlanScriptStandards.md R8):
#   play-claude-yolo.yml — builds and installs the ccy-sessions and ccy-teams commands through
#   the new shared tasks/build-go-command.yml. Same flags, paths and modes as before, so an
#   unchanged source reports ok.
#   play-podfreeze.yml, play-lxcfreeze.yml — import play-golang.yml, then build podfreeze and
#   lxcfreeze from this checkout (Go binaries, src-go/cmd/<tool>), install them at the paths the
#   bash tools had, and remove the retired ~/.local/lib/freeze library. fzf stays installed.
#
#   Before any play, while the bash freeze tools are still installed, it saves what they print
#   for `list` and `freeze --all -n` under untracked/freeze-acceptance/before-*; the plays then
#   replace them. It ends by running acceptance.bash, which runs the Go tools through the same
#   four commands and compares stdout with that baseline. Skipped under --check.
#
# Usage: ./deploy.bash [--check] [-y|--yes] [-h|--help]
set -euo pipefail

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

PLAN_USAGE="usage: deploy.bash [--check] [-y|--yes] [-h|--help]

Saves what the bash podfreeze and lxcfreeze print (while they are still installed), runs
play-claude-yolo.yml, play-podfreeze.yml and play-lxcfreeze.yml on the HOST, then runs
acceptance.bash to compare the Go tools with that baseline. Then follow ACCEPTANCE-freeze.md."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible plays that build Go commands into ~/.local/bin and remove ~/.local/lib/freeze"

# Before the run log, so a password prompt is not flooded by the tee (R3).
plan_prime_sudo

plan_start_log auto

readonly BIN_DIR="${HOME}/.local/bin"
readonly BASELINE_DIR="${PLAN_REPO_ROOT}/untracked/freeze-acceptance"

# is_bash_script <file> — an interpreter line, not a compiled program.
is_bash_script() {
    local first=""
    IFS= read -r -n 2 first <"$1"
    [[ "${first}" == '#!' ]]
}

# save_one <tool-path> <tool> <label> <verb-and-args...> — the bash tool's stdout, stderr and
# exit status for one command, as before-<tool>-<label>.{txt,err,rc}. A non-zero status is part
# of what the tool answers (with no containers, `freeze --all -n` says "nothing matched" and
# exits 1), so it is recorded, not treated as a failure. Only a tool that cannot run at all
# (126: not executable, 127: not found) stops the deploy: replacing it would lose the only
# reference there is.
save_one() {
    local path="$1" tool="$2" label="$3" status=0
    shift 3
    "${path}" "$@" >"${BASELINE_DIR}/before-${tool}-${label}.txt" 2>"${BASELINE_DIR}/before-${tool}-${label}.err" || status=$?
    if [[ "${status}" -eq 126 || "${status}" -eq 127 ]]; then
        printf '[FATAL] the bash %s could not run (exit %s; stderr in %s/before-%s-%s.err); not replacing it\n' \
            "${tool}" "${status}" "${BASELINE_DIR}" "${tool}" "${label}" >&2
        return 1
    fi
    printf '%s\n' "${status}" >"${BASELINE_DIR}/before-${tool}-${label}.rc"
    printf '    %s %s: exit %s\n' "${tool}" "$*" "${status}"
}

# save_baseline — what each bash tool answers to the two acceptance commands, taken before the
# plays replace it. A tool that is not installed has nothing to save; one that is already a
# compiled program keeps the baseline an earlier run saved.
save_baseline() {
    local tool path
    mkdir -p -- "${BASELINE_DIR}"
    for tool in podfreeze lxcfreeze; do
        path="${BIN_DIR}/${tool}"
        if [[ ! -e "${path}" ]]; then
            printf '==> %s is not installed: no bash output to save\n' "${tool}"
            continue
        fi
        if ! is_bash_script "${path}"; then
            printf '==> %s is already a compiled program: keeping the baseline saved earlier\n' "${tool}"
            continue
        fi
        printf '==> saving what the bash %s prints\n' "${tool}"
        save_one "${path}" "${tool}" list list || return 1
        save_one "${path}" "${tool}" dry freeze --all -n || return 1
    done
}

if [[ "${PLAN_CHECK:-0}" == "1" ]]; then
    printf '\n==> --check: the bash output is not saved, nothing is replaced\n'
else
    printf '\n==> saving the bash freeze tools output, before any play replaces them\n'
    save_baseline || exit 1
fi

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
plan_deploy_leg "play-podfreeze.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-podfreeze.yml
plan_deploy_leg "play-lxcfreeze.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-lxcfreeze.yml

if [[ "${PLAN_CHECK:-0}" == "1" ]]; then
    printf '\n==> --check: acceptance.bash skipped, nothing was deployed to test\n'
else
    plan_deploy_leg "acceptance.bash" "${PLAN_SCRIPT_DIR}/acceptance.bash"
fi

printf '\n'
printf 'Deploy done. podfreeze and lxcfreeze are Go binaries now.\n'
printf 'Next: the freeze and menu checks in %s/ACCEPTANCE-freeze.md.\n' "${PLAN_SCRIPT_DIR}"

plan_finish

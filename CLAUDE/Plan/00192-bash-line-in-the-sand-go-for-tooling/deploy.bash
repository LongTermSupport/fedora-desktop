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
#   play-agent-bus.yml — builds agent-bus-install (a Go binary, src-go/cmd/agent-bus-install)
#   from this checkout as the login user through the same shared task, with the installer's
#   source digest baked in, installs a root-owned copy at /usr/local/sbin/agent-bus-install
#   (0755) in place of the bash installer, then runs it: `software`, and `remove`/`team` for
#   the teams localhost.yml declares. Root runs only that installed copy, never a checkout file.
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
play-claude-yolo.yml, play-agent-bus.yml, play-podfreeze.yml and play-lxcfreeze.yml on the HOST,
then runs acceptance.bash to compare the Go tools with that baseline and to check the installed
agent-bus-install. Then follow ACCEPTANCE-freeze.md and ACCEPTANCE-agent-bus-install.md."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible plays that build Go commands into ~/.local/bin and /usr/local/sbin and remove ~/.local/lib/freeze"

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

# save_baseline — what each bash tool prints for the two acceptance commands, taken before the
# plays replace it. A tool that is not installed has nothing to save; one that is already a
# compiled program keeps the baseline an earlier run saved. A bash tool that fails here stops
# the deploy: replacing it would lose the only reference there is.
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
        "${path}" list >"${BASELINE_DIR}/before-${tool}-list.txt" 2>"${BASELINE_DIR}/before-${tool}-list.err" || {
            printf '[FATAL] the bash %s failed (stderr in %s/before-%s-list.err); not replacing a tool whose output cannot be saved\n' \
                "${tool}" "${BASELINE_DIR}" "${tool}" >&2
            return 1
        }
        "${path}" freeze --all -n >"${BASELINE_DIR}/before-${tool}-dry.txt" 2>"${BASELINE_DIR}/before-${tool}-dry.err" || {
            printf '[FATAL] the bash %s freeze --all -n failed (stderr in %s/before-%s-dry.err); not replacing a tool whose output cannot be saved\n' \
                "${tool}" "${BASELINE_DIR}" "${tool}" >&2
            return 1
        }
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
plan_deploy_leg "play-agent-bus.yml" \
    plan_ansible_playbook playbooks/imports/play-agent-bus.yml
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
printf 'Deploy done. podfreeze, lxcfreeze and agent-bus-install are Go binaries now.\n'
printf 'Next: the freeze and menu checks in %s/ACCEPTANCE-freeze.md and the installer checks in\n' "${PLAN_SCRIPT_DIR}"
printf '%s/ACCEPTANCE-agent-bus-install.md.\n' "${PLAN_SCRIPT_DIR}"

plan_finish

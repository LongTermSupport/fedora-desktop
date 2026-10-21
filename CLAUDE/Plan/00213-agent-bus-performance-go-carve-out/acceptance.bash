#!/usr/bin/env bash
# Plan 00213 — acceptance.bash: the verdict on Task 2.3, the bus status line's cut-over from the
# Python `pingbus status-line` to the Go `pingbus-status-line`.
#
#   C1 the member kit's binary (/usr/local/share/agent-bus/kit/pingbus-status-line, built by
#      play-agent-bus.yml) and the ccy build context's (/opt/claude-yolo/optional/agent-bus/,
#      built by play-claude-yolo.yml and baked into the image) are root:root 0755, static, and
#      byte for byte this checkout's build;
#   C2 both copies of the kit's settings.json run `pingbus-status-line`;
#   C3 every LXC container a present agent_bus_lxc_members entry names is RUNNING, its kit
#      holds that same binary, and it runs there;
#   C4 the recorded Python goldens pass for the installed Python (`pingbus status-line`, the
#      host's zipapp) and for both installed Go binaries, so the two print the same over every
#      fixture. A failure for the Python alone means this host's Python prints differently from
#      the one the goldens were recorded with (SPEC-status-line.md, the interpreter note);
#   C5 the CPU one render costs, Python (before) against Go (after), in a scratch world with no
#      inner status line: Go must cost under a tenth. The figures are in the report.
#
# RUN ON THE HOST, as the desktop user (not root): deploy.bash runs it as its last leg, and
# meta-deploy.list queues the plan folder. The one prompt is sudo's, before the log opens (R3);
# lxc-info and lxc-attach run under `sudo -n`.
#
# WHAT IT CHANGES: nothing. It builds this checkout's binary into its run directory, copies
# agent_bus_lxc_members there through ad-hoc Ansible, and runs renders in a scratch world there.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (each failed check is named); 64 usage.
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

Plan 00213 Task 2.3's verdict: the Go status line is installed in the member kit, the ccy
build context and every declared LXC member (C1-C3), it prints what the Python prints over the
recorded goldens (C4), and one render costs under a tenth of the Python's CPU (C5).
Exit 0 ACCEPTED, 1 REJECTED, 64 usage. Prompts for sudo once, before the run log opens."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: every check only reads\n' >&2
    exit 64
fi

plan_require_host "it reads the host's member kit, the ccy build context and the LXC containers"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for lxc-info and lxc-attach\n' >&2
    exit 1
fi
for tool in go jq ansible lxc-info lxc-attach sha256sum cmp od; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s is not installed: its play has not run on this host\n' "${tool}" >&2
        exit 1
    fi
done

KIT_BIN=/usr/local/share/agent-bus/kit/pingbus-status-line
STAGED_BIN=/opt/claude-yolo/optional/agent-bus/pingbus-status-line
HOST_PINGBUS=/usr/local/bin/pingbus
KIT_SETTINGS=/usr/local/share/agent-bus/kit/settings.json
STAGED_SETTINGS=/opt/claude-yolo/optional/agent-bus/settings.json
RENDERS=40
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance.inc.bash"

plan_prime_sudo
plan_start_log auto
REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
BUILT="${PLAN_RUN_DIR}/pingbus-status-line"
MEMBERS_JSON="${PLAN_RUN_DIR}/lxc-members.json"
printf '# Plan 00213 acceptance, Task 2.3\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

plan_gather_leg "this checkout's pingbus-status-line, built static" build_reference
plan_gather_leg "C1 the member kit's and the ccy build context's binaries are this checkout's" c1_installed
plan_gather_leg "C2 both kit settings run pingbus-status-line" c2_settings
plan_gather_leg "read agent_bus_lxc_members" read_members
plan_gather_leg "C3 every declared LXC member is running with this binary in its kit" c3_lxc
plan_gather_leg "C4 the installed Python and Go both pass the recorded goldens" c4_parity
plan_gather_leg "C5 one Go render costs under a tenth of one Python render's CPU" c5_cpu

if [[ -n "${PLAN_FAILED_LEGS}" ]]; then
    printf '\nREJECTED: %s\n' "${PLAN_FAILED_LEGS}" | tee -a "${REPORT}"
else
    printf '\nACCEPTED: the Go status line is installed everywhere, matches the Python and costs a fraction of it\n' |
        tee -a "${REPORT}"
fi
plan_finish

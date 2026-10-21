#!/usr/bin/env bash
# Plan 00211 — acceptance.bash: the verdict on each deploy (SPEC §11.2). Stage 1 only so far;
# stage 2 (A1-A4) and --after-owner (A5) come with Phase 2 (Task 2.5).
#
#   S1 the host's pingbus (/usr/local/bin/pingbus, installed by play-agent-bus.yml) and the one
#      staged into the ccy build context (/opt/claude-yolo/optional/agent-bus/pingbus, built by
#      play-claude-yolo.yml and baked into the image) both print this checkout's
#      `pingbus version` line: 0.2.0 is the release that knows the reports and questions rooms;
#   S2 every LXC container named by a present agent_bus_lxc_members entry is RUNNING and its
#      kit's pingbus prints that same line; a container that is down is refused, named.
#
# RUN ON THE HOST, as the desktop user (not root): deploy.bash --stage 1 runs it as its last
# leg. The one prompt is sudo's, before the log opens (R3); lxc-info and lxc-attach run under
# `sudo -n`. `--fake` runs the same verdict functions over fixture commands and a fixture
# member list it writes, anywhere (the CCY container included), changing nothing.
#
# WHAT IT CHANGES: nothing on the host. It copies agent_bus_lxc_members into its own run
# directory through ad-hoc Ansible, and runs `pingbus version` in each declared container.
# plan_mode is deploy because stage 2 will take a seat and post a report (SPEC §11.2).
#
# Never queue the plan folder itself in meta-deploy.list: that runs deploy.bash and this script
# with no --stage, which both refuse. Queue deploy-stage-1.bash.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check failed and names itself); 64 usage.
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

PLAN_USAGE="usage: acceptance.bash --stage 1 [--fake] [-h|--help]

Plan 00211's verdict on deploy 1: the host's pingbus and the ccy image's staged pingbus are
this checkout's version (S1), and so is every declared LXC member's, none of them down (S2).
  --stage 1   the deploy-1 checks (stage 2 is not written yet: Phase 2)
  --fake      the same verdicts over fixture commands; runs anywhere, changes nothing
Exit 0 ACCEPTED, 1 REJECTED, 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"

STAGE=""
FAKE=0
args=("${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}")
i=0
while [[ "${i}" -lt "${#args[@]}" ]]; do
    case "${args[i]}" in
        --stage)
            i=$((i + 1))
            STAGE="${args[i]:-}"
            ;;
        --stage=*) STAGE="${args[i]#--stage=}" ;;
        --fake) FAKE=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${args[i]}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
    i=$((i + 1))
done
case "${STAGE}" in
    1) ;;
    2)
        printf '[FATAL] --stage 2 is not written yet (Plan 00211 Task 2.5)\n' >&2
        exit 64
        ;;
    *)
        printf '[FATAL] --stage 1 is required\n%s\n' "${PLAN_USAGE}" >&2
        exit 64
        ;;
esac
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the checks only read (--fake runs the verdicts alone)\n' >&2
    exit 64
fi

# What the verdicts run. --fake points each at a fixture; nothing else differs.
HOST_PINGBUS="/usr/local/bin/pingbus"
STAGED_PINGBUS="/opt/claude-yolo/optional/agent-bus/pingbus"
LXC_INFO=(sudo -n lxc-info)
LXC_ATTACH=(sudo -n lxc-attach)
MEMBERS_JSON=""
REPORT=""
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-stage1.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-stage1.inc.bash"

# check_leg <name> <cmd...> — one check, a bare top-level statement like plan_deploy_leg; the
# first that does not pass ends the run (exit 1).
check_leg() {
    local name="$1"
    shift
    printf '\n==> [check] %s\n' "${name}"
    if ! "$@"; then
        printf 'FAIL  %s\n' "${name}" >&2
        printf '## FAIL %s\n' "${name}" >>"${REPORT}"
        exit 1
    fi
    printf 'PASS  %s\n' "${name}"
    printf '## PASS %s\n\n' "${name}" >>"${REPORT}"
}

# reject_leg <name> <cmd...> — the fake mode's negative check: the verdict must refuse it.
reject_leg() {
    local name="$1"
    shift
    printf '\n==> [must be rejected] %s\n' "${name}"
    if "$@"; then
        printf 'FAIL  %s: the verdict accepted it\n' "${name}" >&2
        printf '## FAIL %s (accepted)\n' "${name}" >>"${REPORT}"
        exit 1
    fi
    printf 'PASS  %s (rejected, as it must be)\n' "${name}"
    printf '## PASS %s (rejected)\n\n' "${name}" >>"${REPORT}"
}

readonly TWO='[{"team":"t1","container":"lxc-a","user":"u1"},{"team":"t2","container":"lxc-a","user":"u2"},{"team":"t1","container":"lxc-b","user":"u3","state":"present"}]'
readonly GONE='[{"team":"t1","container":"lxc-c","user":"u1","state":"absent"}]'

if [[ "${FAKE}" -eq 1 ]]; then
    # STANDARD-EXCEPTION(R2): --fake reads only the fixtures it writes, so where it runs does not matter
    plan_start_log auto
    REPORT="${PLAN_RUN_DIR}/acceptance-fake-report.md"
    printf '# Plan 00211 acceptance, stage 1, fake mode\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"
    plan_deploy_leg "this checkout's pingbus version" want_version
    plan_deploy_leg "write the fixture commands" fake_setup
    check_leg "S1 (fake) host and staged pingbus are new" fake_s1 new new
    reject_leg "S1 (fake) an old host pingbus" fake_s1 old new
    reject_leg "S1 (fake) an old staged pingbus" fake_s1 new old
    reject_leg "S1 (fake) a missing staged pingbus" fake_s1 new missing
    check_leg "S2 (fake) two running containers, both new, one shared by two members" \
        fake_s2 "${TWO}" lxc-a=RUNNING:new lxc-b=RUNNING:new
    check_leg "S2 (fake) no member declared" fake_s2 '[]'
    check_leg "S2 (fake) an absent member's container is not judged" fake_s2 "${GONE}" lxc-c=STOPPED:old
    reject_leg "S2 (fake) a stopped container" fake_s2 "${TWO}" lxc-a=RUNNING:new lxc-b=STOPPED:new
    reject_leg "S2 (fake) a running container with the old pingbus" \
        fake_s2 "${TWO}" lxc-a=RUNNING:old lxc-b=RUNNING:new
    printf '\nFAKE ACCEPTED: the stage-1 verdicts accept new versions and refuse old, missing and down ones\n' | tee -a "${REPORT}"
    plan_finish
fi

plan_require_host "it reads the host's installed pingbus, the ccy build context and the LXC containers"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for lxc-info and lxc-attach\n' >&2
    exit 1
fi
for tool in jq python3 lxc-info lxc-attach ansible; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s is not installed: its play has not run on this host\n' "${tool}" >&2
        exit 1
    fi
done

plan_prime_sudo
plan_start_log auto
REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
MEMBERS_JSON="${PLAN_RUN_DIR}/lxc-members.json"
printf '# Plan 00211 acceptance, stage 1\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

plan_deploy_leg "this checkout's pingbus version" want_version
plan_deploy_leg "read agent_bus_lxc_members" read_members
check_leg "S1 the host's pingbus and the ccy image's staged pingbus are this checkout's" s1_check
check_leg "S2 every declared LXC member's pingbus is this checkout's, and none is down" s2_check
printf '\nACCEPTED: deploy 1 is on the host, in the ccy build context and in every declared LXC container\n' | tee -a "${REPORT}"
plan_finish

#!/usr/bin/env bash
# Plan 00161 — triage.bash (unit U00): the host probes H1-H6 of DESIGN.md section 13.
#
# PURPOSE: establish the facts the agent-bus build rests on, before the installer exists:
#   H3  the Tuwunel release asset (name, sha256, decompression), and Tuwunel started under the
#       section 3.5 sandbox as a transient system unit with the resolver stub;
#   H4  a throwaway Tuwunel on loopback with the section 3.6 config: every admin and client
#       call the design makes, recorded and scrubbed as fixtures for U08; header logging;
#       the unknown-key warning;
#   H5  a backup on SIGUSR2 and a restore onto a copy;
#   H1  which host addresses ccy-image containers reach, and the source address seen;
#   H2  the same from docker, LXC and libvirt guests, where installed (a libvirt guest's own
#       connection needs a guest shell, so it is an owner step, listed, not a failure);
#   H1 and H2 never create the dummy bus address (nothing persistent): they test the host's
#   primary address and, with --bus-address=, an address already assigned. Without it they
#   report the dummy address as not tested and fail, since a dummy's firewalld zone and
#   routing can differ from the primary address's.
#   H6  Element Desktop under pasta with a packet capture.
# H7 (the phone) and the login legs are the owner's; the report lists them.
# Unit U01, the Claude Code probes (DESIGN.md sections 6 and 13), run as the last legs, on
# the host because a child claude needs this user's login: four throwaway `claude -p`
# sessions, each loading a probe plugin with --plugin-dir, record which hooks fire
# (SessionStart, UserPromptSubmit, Stop, SessionEnd) and, through a detached process that
# the SessionStart hook starts, what the session inbox socket does with a notice: its wire
# format, whether it starts a turn in an idle session, how the model sees it, identical
# repeats and the dedupe window, back-to-back notices, and the same without bypass mode or
# without crossSessionInbound accept. Each costs a few one-word haiku turns.
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The probes themselves
# are in triage_probe.py and u01_probe.py beside this script, tested by
# test_triage_probe.py and test_u01_probe.py.
#
# RUN ON THE HOST, as the desktop user (not root): through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00161-agent-team-bus-matrix/triage.bash [--bus-address=<ip>] \
#       [--docker-image=<ref>] [--element-seconds=<n>] [--claude-only]
# It never prompts. sudo is primed before the log opens (R3); the H3 unit, LXC and
# firewalld legs need it, and fail by name without it. --claude-only runs just the U01
# legs, which need no sudo.
#
# EFFECT ON THE HOST: nothing persistent. It downloads the pinned Tuwunel release into a
# scratch directory under this run's directory and removes it on the way out (the scrubbed
# fixtures and the report stay in the run directory, which is untracked); runs Tuwunel as a
# plain process on 127.0.0.1 and as a transient unit (DynamicUser, its data in a runtime
# directory systemd removes when it stops); creates and removes one rootless podman network
# (H1); starts throwaway containers with --rm; runs Element for a bounded time with a new
# profile it removes (H6). It pulls no image and installs nothing. The U01 sessions run in
# an empty directory under the scratch directory, with this user's own settings, hooks,
# plugins, tools and MCP servers left out, and never see a running session's socket; their
# transcript is copied into the run directory, then `claude purge` and a sweep of the files
# named by each session's fresh UUID remove them from the Claude config directory.
#
# EXIT CODES: 0 every leg established its facts; 1 at least one leg did not (the failing leg
# names itself; the fact-finding is incomplete, the system is not judged; H1 and H2 always
# give 1 without --bus-address); 64 usage.
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

PLAN_USAGE="usage: triage.bash [--bus-address=<ip>] [--docker-image=<ref>] [--element-seconds=<n>] [--claude-only] [-h|--help]

Host probes H1-H6 and the Claude Code probes (U01) for Plan 00161 (agent team bus).
Host-only, read-only, never prompts.
  --bus-address=<ip>     an address already assigned on this host (the future agentbus0
                         address); H1, H2 and the H3 unit also try it
  --docker-image=<ref>   a locally present image with sh and nc for H2's docker leg
                         (default docker.io/library/busybox:latest; nothing is pulled)
  --element-seconds=<n>  how long H6 runs Element under pasta (default 60)
  --claude-only          only the U01 Claude Code legs (no sudo)
Writes triage-report.md, fixtures/tuwunel/ and u01/ (each session's evidence) into its run
directory and names them."

plan_mode gather
plan_parse_common_flags "$@"

busAddress=""
dockerImage="docker.io/library/busybox:latest"
elementSeconds="60"
claudeOnly=0
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --claude-only) claudeOnly=1 ;;
        --bus-address=?*) busAddress="${arg#--bus-address=}" ;;
        --docker-image=?*) dockerImage="${arg#--docker-image=}" ;;
        --element-seconds=?*) elementSeconds="${arg#--element-seconds=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ ! "${elementSeconds}" =~ ^[1-9][0-9]{0,3}$ ]]; then
    printf '[FATAL] --element-seconds must be 1-9999, got %s\n' "${elementSeconds}" >&2
    exit 64
fi

plan_require_host "it probes the host's systemd, podman, bridges and firewalld, and runs Tuwunel and Element on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: rootless podman and Element are per user; the legs that need root use sudo\n' >&2
    exit 1
fi
if [[ "${claudeOnly}" -eq 0 ]]; then
    plan_prime_sudo
fi
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/triage-report.md"
SCRATCH="${PLAN_RUN_DIR}/scratch"
FIXTURES="${PLAN_RUN_DIR}/fixtures/tuwunel"

remove_scratch() {
    rm -rf -- "${SCRATCH}"
}
plan_on_cleanup remove_scratch

mkdir -p "${SCRATCH}"
printf '# Plan 00161 triage (U00): host probes\n\n' >"${REPORT}"

probe=(python3 "${PLAN_SCRIPT_DIR}/triage_probe.py")
common=(--report "${REPORT}" --scratch "${SCRATCH}")
busArgs=()
if [[ -n "${busAddress}" ]]; then
    busArgs=(--bus-address "${busAddress}")
fi

if [[ "${claudeOnly}" -eq 0 ]]; then
    plan_gather_leg "environment" "${probe[@]}" env "${common[@]}"
    plan_gather_leg "H3 Tuwunel release asset" "${probe[@]}" h3-asset "${common[@]}"
    plan_gather_leg "H4 admin and client API, fixtures" \
        "${probe[@]}" h4 "${common[@]}" --fixtures "${FIXTURES}"
    plan_gather_leg "H5 backup and restore" "${probe[@]}" h5 "${common[@]}"
    plan_gather_leg "H3 sandboxed transient unit and resolver stub" \
        "${probe[@]}" h3-unit "${common[@]}" "${busArgs[@]+"${busArgs[@]}"}"
    plan_gather_leg "H1 ccy-image containers" \
        "${probe[@]}" h1 "${common[@]}" "${busArgs[@]+"${busArgs[@]}"}"
    plan_gather_leg "H2 docker, LXC and libvirt guests" \
        "${probe[@]}" h2 "${common[@]}" --docker-image "${dockerImage}" "${busArgs[@]+"${busArgs[@]}"}"
    plan_gather_leg "H6 Element Desktop under pasta" \
        "${probe[@]}" h6 "${common[@]}" --element-seconds "${elementSeconds}"
    plan_gather_leg "owner steps" "${probe[@]}" owner "${common[@]}"
fi

u01=(python3 "${PLAN_SCRIPT_DIR}/u01_probe.py")
plan_gather_leg "U01 Claude Code version and login" "${u01[@]}" claude-env "${common[@]}"
for variant in main bypass-no-accept default-accept default-no-accept; do
    plan_gather_leg "U01 throwaway session: ${variant}" \
        "${u01[@]}" session --variant "${variant}" --evidence "${PLAN_RUN_DIR}/u01" "${common[@]}"
done
remove_scratch # the same call an interrupted run makes through plan_on_cleanup
plan_finish

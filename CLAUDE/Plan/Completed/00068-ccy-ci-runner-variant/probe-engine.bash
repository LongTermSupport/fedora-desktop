#!/usr/bin/env bash
# probe-engine.bash — gather FACTS about the host's container engine and ccy's device flag.
#
# Fact-finding only: appends to the report file given as $1 and renders no verdict
# (PlanScriptStandards R9). READ-ONLY: builds nothing, installs nothing, pulls nothing. Every
# container it starts is `--rm` and runs `true`.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-engine.bash /tmp/report.md
#
# Engine override: CCY_CONTAINER_ENGINE (default podman, per CLAUDE/ContainerEngines.md).
#
# EXIT CODES:
#   0  every probe reached a definite answer (including "absent", which IS an answer)
#   1  a probe could not be answered — the fact-finding is incomplete, not the system broken
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
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
    printf 'usage: probe-engine.bash <report-file>\n' >&2
    exit 64
fi

# A nested engine result is not evidence about the host. Enforced, not advised (R2).
plan_require_host "it probes the host container engine and its device handling"

ENGINE="${CCY_CONTAINER_ENGINE:-podman}"
readonly ENGINE
INCOMPLETE=0

out() { printf '%s\n' "$*" >>"${REPORT}"; }

out ""
out "## Container engine"
out ""

if [[ -z "$(command -v "${ENGINE}")" ]]; then
    out "\`${ENGINE}\` is **NOT on PATH**. Every engine probe below is unanswerable."
    printf '[INCOMPLETE] %s is not on PATH\n' "${ENGINE}" >&2
    exit 1
fi

out '```'
if engineVer="$("${ENGINE}" --version 2>&1)"; then
    out "${engineVer}"
else
    out "${ENGINE} --version failed: ${engineVer}"
    INCOMPLETE=1
fi
if rootless="$("${ENGINE}" info --format '{{.Host.Security.Rootless}}' 2>&1)"; then
    out "rootless: ${rootless}"
else
    out "could not read rootless status: ${rootless}"
    INCOMPLETE=1
fi
out '```'

# ── a local image to probe with, WITHOUT pulling ──────────────────────────────────────────
# The device probes need an image. Pulling would make this depend on egress and would not be
# read-only in spirit, so prefer one the host already has and say so plainly if there is none.
out ""
out "## Local images available for probing"
out ""
PROBE_IMAGE=""
if images="$("${ENGINE}" images --format '{{.Repository}}:{{.Tag}}' 2>&1)"; then
    out '```'
    out "${images}"
    out '```'
    for candidate in claude-yolo:latest claude-yolo:full claude-yolo:base; do
        if printf '%s\n' "${images}" | grep -qx -- "${candidate}"; then
            PROBE_IMAGE="${candidate}"
            break
        fi
    done
    if [[ -z "${PROBE_IMAGE}" ]]; then
        first="$(printf '%s\n' "${images}" | grep -v '<none>' | awk 'NR==1')"
        if [[ -n "${first}" ]]; then
            PROBE_IMAGE="${first}"
        fi
    fi
else
    out "Could not list images: ${images}"
    INCOMPLETE=1
fi

if [[ -z "${PROBE_IMAGE}" ]]; then
    out ""
    out "**No local image found**, so the device probes below are UNANSWERED rather than"
    out "passed — a pull would make this probe depend on egress. Re-run after any \`ccy\`"
    out "session has built \`claude-yolo:latest\`."
    INCOMPLETE=1
else
    out ""
    out "Probe image: \`${PROBE_IMAGE}\`"
fi

# ── /dev/dri on this host, and ccy's unconditional --device ────────────────────────────────
out ""
out "## /dev/dri and ccy's unconditional --device"
out ""
if [[ -e /dev/dri ]]; then
    out "\`/dev/dri\` EXISTS. Nodes:"
    out '```'
    if driLs="$(ls -la /dev/dri 2>&1)"; then out "${driLs}"; else
        out "ls failed: ${driLs}"
        INCOMPLETE=1
    fi
    out '```'
else
    out "\`/dev/dri\` **DOES NOT EXIST** on this host — the shape a headless server has, and"
    out "exactly the case the next probe decides the consequence of."
fi

if [[ -n "${PROBE_IMAGE}" ]]; then
    out ""
    out "Running ccy's real flag (\`--device /dev/dri:/dev/dri\`):"
    out '```'
    # `--entrypoint true` is LOAD-BEARING. `true` as the COMMAND does not replace an
    # ENTRYPOINT, and the ccy image has one — so the first run of this probe reported
    # "EXIT 1 — REJECTED" for a flag podman had ACCEPTED, the non-zero coming from
    # entrypoint.sh dying on a missing GH_TOKEN. Only 125 means the engine refused.
    if cOut="$("${ENGINE}" run --rm --entrypoint true --device /dev/dri:/dev/dri "${PROBE_IMAGE}" 2>&1)"; then
        out "EXIT 0 — accepted."
        if [[ -n "${cOut}" ]]; then out "${cOut}"; fi
    else
        cRc=$?
        if [[ "${cRc}" -eq 125 ]]; then
            out "EXIT ${cRc} — REJECTED by ${ENGINE} (the engine refused the command line)."
        else
            out "EXIT ${cRc} — the container RAN and then exited non-zero. The flag was"
            out "ACCEPTED; this says nothing about --device support."
        fi
        out "${cOut}"
    fi
    out '```'
fi

# ── THE CRUX: is a MISSING device node fatal, or silently ignored? ─────────────────────────
out ""
out "## Is a MISSING --device path fatal?"
out ""
if [[ -z "${PROBE_IMAGE}" ]]; then
    out "UNANSWERED — no local image (see above). This is not a pass."
else
    missing="/dev/plan00068-definitely-absent"
    if [[ -e "${missing}" ]]; then
        out "Unexpected: \`${missing}\` exists, so this probe is invalid. Pick another path."
        INCOMPLETE=1
    else
        out "Probing \`--device ${missing}:${missing}\` (path confirmed absent):"
        out '```'
        # Same entrypoint override as above. This particular case happened to be safe
        # without it — podman rejects a missing --device at 125 BEFORE any container runs,
        # so the entrypoint never executed — but relying on that is relying on luck.
        if dOut="$("${ENGINE}" run --rm --entrypoint true --device "${missing}:${missing}" "${PROBE_IMAGE}" 2>&1)"; then
            out "EXIT 0 — a missing --device is IGNORED by ${ENGINE}."
            if [[ -n "${dOut}" ]]; then out "${dOut}"; fi
        else
            dRc=$?
            if [[ "${dRc}" -eq 125 ]]; then
                out "EXIT ${dRc} — a missing --device is FATAL to ${ENGINE}."
            else
                out "EXIT ${dRc} — INCONCLUSIVE: the container ran, so the engine accepted the"
                out "missing path. Expected 125 (engine refusal) or 0 (ignored)."
                INCOMPLETE=1
            fi
            out "${dOut}"
        fi
        out '```'
        out ""
        out "FACT ONLY — the consequence for ccy's unconditional \`--device\` is a verdict, and"
        out "verdicts belong in the acceptance gate, not in triage (PlanScriptStandards R9)."
    fi
fi

# ── image provenance ──────────────────────────────────────────────────────────────────────
out ""
out "## Image provenance"
out ""
out '```'
for tag in claude-yolo:latest claude-yolo:full claude-yolo:base; do
    if insp="$("${ENGINE}" image inspect "${tag}" \
        --format '{{.Created}} version={{index .Config.Labels "claude-yolo-version"}} hash={{index .Config.Labels "claude-yolo-dockerfile-hash"}}' 2>&1)"; then
        out "${tag}: ${insp}"
    else
        out "${tag}: absent"
    fi
done
out '```'
out ""
out "Compare \`claude-yolo-version\` above with \`REQUIRED_CONTAINER_VERSION\` in the launcher."
out "A mismatch — of the version OR the Dockerfile md5, either alone is enough"
out "(\`common.bash:472-476\`) — makes ccy rebuild on the next launch."
out ""
out "**That is ccy working correctly, not a problem to design around.** The image is built"
out "from the ccy Dockerfile; a stale image SHOULD be rebuilt. This runner IS the ccy system"
out "and inherits that behaviour deliberately — the same rebuild the operator already relies"
out "on at the desktop."
out ""
out "The thing worth planning for is WHEN: the first run after a Dockerfile change spends the"
out "build before it starts work."
out ""
out "A caution against the obvious-looking reading, which was made and corrected here. The build"
out "is a host-side \`podman build\` (\`common.bash:556\`), so the agent container's \`--network\`"
out "restriction does not apply to it. That is true, and it does NOT mean the build is"
out "unconstrained: on a runner VM the host's own egress goes through squid, whose runtime"
out "allowlist deliberately excludes the Debian archives, go.dev, the Go module proxy and pypi"
out "that a build needs. A job that tried to build is refused — measured live, not predicted"
out "(\`lts-infra tasks/runner-ccy-project-image.yml:19-23\`)."
out ""
out "Hence the by-time split: build at PROVISION time with the egress window armed, run the"
out "already-built image at JOB time. That is not a system built beside ccy — it is ccy's own"
out "rebuild, scheduled when it can actually reach the network."

if [[ "${INCOMPLETE}" -ne 0 ]]; then
    printf '[INCOMPLETE] at least one engine probe could not be answered; see %s\n' "${REPORT}" >&2
    exit 1
fi
printf '==> engine probes complete: %s\n' "${REPORT}"

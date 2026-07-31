#!/usr/bin/env bash
# probe-label.bash — MEASURE the label-reader behaviour the LABEL convention spec rests on.
#
# Settles hardware-proof group F (reports/hardware-proof-checklist.md), whose F1 is described
# there as "the single most consequential unproven claim in the specification". The spec
# (reports/label-convention-spec.md §4) asserts, from documentation rather than measurement,
# that `image inspect --format '{{index .Config.Labels "…"}}'` returns an EMPTY STRING for an
# absent label rather than failing. Everything downstream depends on that:
#
#   - if it returns empty and BOTH sides of a comparison are empty, the staleness check
#     compares "" to "" and reports FRESH while knowing nothing — a check that fires and does
#     not discriminate;
#   - if instead it ERRORS, the hazard is inverted (loud, not silent) and §4's mandatory
#     non-empty assertion is guarding something that cannot happen.
#
# Both are defensible designs. Which one is needed is a fact about the engine, and this script
# is how that fact gets established instead of assumed.
#
# Fact-finding only: appends to the report file given as $1 and renders no verdict
# (PlanScriptStandards R9). The naive-vs-prescribed comparison below REPORTS which answer each
# shape produces; it does not pronounce on whether the spec is right.
#
# NOT READ-ONLY — and this is the one probe in this plan that is not. It BUILDS three throwaway
# label-only images from `FROM scratch` (no pull, no network, no filesystem layers) and REMOVES
# them on exit. It refuses to run rather than reuse or overwrite a tag that already exists, so
# it can never clobber something real. Nothing pre-existing is modified.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-label.bash /tmp/report.md
#
# Engine override: CCY_CONTAINER_ENGINE (default podman, per CLAUDE/ContainerEngines.md).
#
# EXIT CODES:
#   0  every probe reached a definite answer (including "errors instead of empty", which IS an
#      answer, and arguably the more useful one)
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
    printf 'usage: probe-label.bash <report-file>\n' >&2
    exit 64
fi

# A nested engine result is not evidence about the host — the same rule that governs the other
# two probes, and the reason the /dev/dri question had to be re-asked on hardware.
plan_require_host "it builds and inspects images with the host container engine"

ENGINE="${CCY_CONTAINER_ENGINE:-podman}"
readonly ENGINE

# The two canonical keys this plan specifies, plus the base-image key their check-3 reads.
KEY_SHA="claude-yolo-project-dockerfile-sha256"
KEY_BASE="claude-yolo-project-base-version"
KEY_VER="claude-yolo-version"
readonly KEY_SHA KEY_BASE KEY_VER

IMG_NONE="plan00068-probe-nolabels:probe"
IMG_OTHER="plan00068-probe-otherlabel:probe"
IMG_HAVE="plan00068-probe-haslabel:probe"
readonly IMG_NONE IMG_OTHER IMG_HAVE

INCOMPLETE=0
CTX=""
BUILT=()

out() { printf '%s\n' "$*" >>"${REPORT}"; }

# Traps at top level only, and the INT handler exits so Ctrl-C is not swallowed
# (.claude/rules/bash-standards.md §7). EXIT still fires after it, so cleanup runs once.
_cleanup() {
    local img rmOut
    for img in "${BUILT[@]:-}"; do
        # Explicit if, not `[[ -z ]] && continue`: this runs as the EXIT trap, where a stray
        # non-zero status is the most expensive place to be clever.
        if [[ -n "${img}" ]]; then
            if ! rmOut="$("${ENGINE}" image rm -f "${img}" 2>&1)"; then
                printf '[WARN] could not remove probe image %s: %s\n' "${img}" "${rmOut}" >&2
            fi
        fi
    done
    if [[ -n "${CTX}" ]] && [[ -d "${CTX}" ]]; then
        if ! rmOut="$(rm -rf "${CTX}" 2>&1)"; then
            printf '[WARN] could not remove build context %s: %s\n' "${CTX}" "${rmOut}" >&2
        fi
    fi
    return 0
}
trap 'printf "\n" >&2; exit 130' INT TERM QUIT
trap _cleanup EXIT

out ""
out "## Label-reader behaviour (hardware-proof group F)"
out ""
out "Engine: \`${ENGINE}\`. Three throwaway \`FROM scratch\` images are built and removed."
out ""

if [[ -z "$(command -v "${ENGINE}")" ]]; then
    out "\`${ENGINE}\` is **NOT on PATH**. Every group-F probe below is unanswerable."
    printf '[INCOMPLETE] %s is not on PATH\n' "${ENGINE}" >&2
    exit 1
fi

# ── refuse to clobber ─────────────────────────────────────────────────────────────────────
# Look at the target before creating it. A pre-existing tag is somebody else's, and reusing it
# would both corrupt this measurement and destroy their image on cleanup.
for img in "${IMG_NONE}" "${IMG_OTHER}" "${IMG_HAVE}"; do
    if existing="$("${ENGINE}" image inspect "${img}" --format '{{.Id}}' 2>&1)"; then
        out "**Refusing to run**: \`${img}\` already exists (\`${existing}\`). This probe will"
        out "not reuse or overwrite an existing tag. Remove it yourself if it is scrap."
        printf '[FATAL] probe tag already exists: %s\n' "${img}" >&2
        exit 1
    fi
done

CTX="$(mktemp -d)"

# `FROM scratch` needs no pull and produces no filesystem layers — these images are label
# carriers and are never run.
printf 'FROM scratch\n' >"${CTX}/Containerfile.none"
printf 'FROM scratch\nLABEL org.opencontainers.image.title="plan00068 probe"\n' >"${CTX}/Containerfile.other"
printf 'FROM scratch\nLABEL %s="2.22"\nLABEL %s="deadbeef"\n' "${KEY_BASE}" "${KEY_SHA}" >"${CTX}/Containerfile.have"

build_probe_image() {
    local tag="$1" file="$2" desc="$3" bOut=""
    if bOut="$("${ENGINE}" build -q -t "${tag}" -f "${file}" "${CTX}" 2>&1)"; then
        BUILT+=("${tag}")
        out "- built \`${tag}\` — ${desc}"
        return 0
    fi
    out "- **could not build** \`${tag}\` (${desc}):"
    out '```'
    out "${bOut}"
    out '```'
    INCOMPLETE=1
    return 1
}

out "### Probe images"
out ""
# `|| :` would be error-hiding; a failed build is recorded by the helper and must not abort the
# remaining probes, so the return value is consumed by an explicit if with an empty then-branch
# equivalent — expressed here as a guarded call so shellcheck sees the status is handled.
if build_probe_image "${IMG_NONE}" "${CTX}/Containerfile.none" "no labels at all (\`.Config.Labels\` may be nil)"; then
    :
fi
if build_probe_image "${IMG_OTHER}" "${CTX}/Containerfile.other" "one UNRELATED label — the realistic pre-convention project image"; then
    :
fi
if build_probe_image "${IMG_HAVE}" "${CTX}/Containerfile.have" "both canonical keys present — the positive control"; then
    :
fi

# ── the reader ────────────────────────────────────────────────────────────────────────────
LABEL_OUT=""
LABEL_RC=0
read_label() {
    local image="$1" key="$2"
    LABEL_OUT=""
    if LABEL_OUT="$("${ENGINE}" image inspect "${image}" --format "{{index .Config.Labels \"${key}\"}}" 2>&1)"; then
        LABEL_RC=0
    else
        LABEL_RC=$?
    fi
    return 0
}

report_label() {
    local image="$1" key="$2"
    if ! printf '%s\n' "${BUILT[@]:-}" | grep -qx -- "${image}"; then
        out "| \`${image}\` | \`${key}\` | — | — | image was not built |"
        INCOMPLETE=1
        return 0
    fi
    read_label "${image}" "${key}"
    # Length is the fact; "empty string" is a description of it. ${#var} needs no extra tool.
    out "| \`${image}\` | \`${key}\` | ${LABEL_RC} | ${#LABEL_OUT} | ${LABEL_OUT:-(nothing on stdout)} |"
    return 0
}

out ""
out "### F1 — what does the reader return for an ABSENT label?"
out ""
out "| image | key | exit | bytes | output |"
out "| ----- | --- | ---- | ----- | ------ |"
report_label "${IMG_NONE}" "${KEY_BASE}"
report_label "${IMG_OTHER}" "${KEY_BASE}"
report_label "${IMG_HAVE}" "${KEY_BASE}"
report_label "${IMG_HAVE}" "${KEY_SHA}"
out ""
out "The first two rows ARE F1. \`bytes\` of 0 with exit 0 confirms the spec's assumption"
out "(silent empty, hazard real). A non-zero exit, or output naming a template error, refutes"
out "it — in which case §4's non-empty assertion guards a case that cannot arise, and the"
out "engine is failing loudly instead. The two rows are separated deliberately: an image with"
out "NO labels may present a nil map to the Go template, which is a different code path from"
out "a non-nil map that lacks the key, and only the second is what a real project image looks"
out "like."
out ""
out "The last two rows are the positive control. If they are also empty, the reader is not"
out "discriminating and NOTHING above this line means what it appears to mean"
out "(.claude/rules/bash-standards.md §9)."

# ── F2: the two comparison shapes, run for real ───────────────────────────────────────────
out ""
out "### F2 — naive vs prescribed comparison, executed"
out ""
if ! printf '%s\n' "${BUILT[@]:-}" | grep -qx -- "${IMG_OTHER}"; then
    out "UNANSWERED — the pre-convention stand-in image was not built."
    INCOMPLETE=1
else
    # Both sides read off an image that carries NEITHER key: the exact two-empties case the
    # spec says degrades to a no-op. This is check 3, the one §4 identifies as exposed.
    read_label "${IMG_OTHER}" "${KEY_VER}"
    wanted="${LABEL_OUT}"
    wantedRc="${LABEL_RC}"
    read_label "${IMG_OTHER}" "${KEY_BASE}"
    have="${LABEL_OUT}"
    haveRc="${LABEL_RC}"

    out "Stand-in base and project image both lack their key (check 3, the exposed one):"
    out ""
    out "- wanted (\`${KEY_VER}\`): exit ${wantedRc}, ${#wanted} bytes"
    out "- have (\`${KEY_BASE}\`): exit ${haveRc}, ${#have} bytes"
    out ""
    if [[ "${wanted}" == "${have}" ]]; then
        out "- **naive comparison** (\`[[ \"\$wanted\" == \"\$have\" ]]\`) → **FRESH**. Two unknowns"
        out "  compared equal. This is the no-op the spec predicts, reproduced."
    else
        out "- **naive comparison** → STALE. The two sides differ, so the predicted no-op did"
        out "  not occur under these conditions — record why before relying on it."
    fi
    if [[ -z "${wanted}" ]]; then
        out "- **prescribed comparison** (assert wanted non-empty first) → **REBUILD**, because"
        out "  the wanted side is empty and an unknown is not a match."
    else
        out "- **prescribed comparison** → falls through to the value comparison; the wanted"
        out "  side was non-empty, so the assertion did not fire."
    fi

    # Positive control for the comparison itself: a known-good and a known-bad wanted value
    # against a real label. A gate that only ever says REBUILD is as useless as one that only
    # ever says FRESH.
    if printf '%s\n' "${BUILT[@]:-}" | grep -qx -- "${IMG_HAVE}"; then
        read_label "${IMG_HAVE}" "${KEY_BASE}"
        out ""
        out "Positive control against \`${IMG_HAVE}\` (label = \`${LABEL_OUT}\`):"
        if [[ "2.22" == "${LABEL_OUT}" ]]; then
            out "- wanted \`2.22\` → **FRESH** (matches, correctly)"
        else
            out "- wanted \`2.22\` → STALE — unexpected; the positive control did not match."
            INCOMPLETE=1
        fi
        if [[ "9.99" != "${LABEL_OUT}" ]]; then
            out "- wanted \`9.99\` → **STALE** (differs, correctly)"
        else
            out "- wanted \`9.99\` → FRESH — unexpected; the reader is not discriminating."
            INCOMPLETE=1
        fi
    fi
fi

# ── F3: does the real base image carry the version label? ─────────────────────────────────
out ""
out "### F3 — does \`claude-yolo:latest\` on THIS host carry \`${KEY_VER}\`?"
out ""
if inspOut="$("${ENGINE}" image inspect claude-yolo:latest --format '{{.Id}}' 2>&1)"; then
    read_label "claude-yolo:latest" "${KEY_VER}"
    out "\`claude-yolo:latest\` is present (\`${inspOut}\`)."
    out ""
    out "- \`${KEY_VER}\`: exit ${LABEL_RC}, ${#LABEL_OUT} bytes, value \`${LABEL_OUT:-}\`"
    out ""
    if [[ "${LABEL_RC}" -eq 0 ]] && [[ -n "${LABEL_OUT}" ]]; then
        out "Non-empty, so check 3's WANTED side can be computed and the check can be a"
        out "control rather than decoration."
    else
        out "**Empty or failed.** Check 3's wanted side cannot be computed on this host, which"
        out "is precisely the condition that makes the two-empties no-op reachable in"
        out "production rather than only in this probe."
    fi
else
    out "\`claude-yolo:latest\` is **absent** on this host, so F3 is unanswered here. That is"
    out "itself worth recording — it is the same rebuild-history question as group A."
    INCOMPLETE=1
fi

out ""
out "FACT ONLY — whether the spec should change in consequence is a verdict, and verdicts"
out "belong in the acceptance gate, not in triage (PlanScriptStandards R9)."

if [[ "${INCOMPLETE}" -ne 0 ]]; then
    printf '[INCOMPLETE] at least one group-F probe could not be answered; see %s\n' "${REPORT}" >&2
    exit 1
fi
printf '==> label probes complete: %s\n' "${REPORT}"

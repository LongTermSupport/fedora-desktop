#!/usr/bin/env bash
# probe-network.bash — re-measure the borrowed network claims under THIS engine (group C).
#
# The hardware-proof checklist calls C3 "the one to re-measure first": that
# `--network pasta:…` and `--network <name>` are mutually exclusive. It is the borrowed claim
# with the most weight on it — Task 5.1 specifies a HARD ERROR on the strength of it — and it
# was measured by a consumer on its own runner, never under ccy's container shape.
#
# The experimental design matters more than the commands. A single run of
# `--network pasta:… --network podman` returning non-zero would be consistent with several
# stories, and the mundane one is usually true: pasta is not installed, the image is wrong,
# the engine is too old. So each combination is run alongside the two SINGLE-flag baselines.
# Exclusivity is only demonstrated if both singles SUCCEED and the combination FAILS. If pasta
# alone already fails, the combination proves nothing and this probe says so rather than
# banking the convenient reading (.claude/rules/bash-standards.md §9).
#
# Fact-finding only: appends to the report file given as $1 and renders no verdict (R9).
# READ-ONLY: builds nothing, installs nothing, pulls nothing. Every container is `--rm` and
# runs `true`.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-network.bash /tmp/report.md
#
# Engine override: CCY_CONTAINER_ENGINE (default podman, per CLAUDE/ContainerEngines.md).
#
# EXIT CODES:
#   0  every probe reached a definite answer
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
    printf 'usage: probe-network.bash <report-file>\n' >&2
    exit 64
fi

plan_require_host "it measures host container networking, which a nested container cannot answer"

ENGINE="${CCY_CONTAINER_ENGINE:-podman}"
readonly ENGINE
INCOMPLETE=0

out() { printf '%s\n' "$*" >>"${REPORT}"; }

out ""
out "## Network flag interaction (hardware-proof group C)"
out ""

if [[ -z "$(command -v "${ENGINE}")" ]]; then
    out "\`${ENGINE}\` is **NOT on PATH**. Every group-C probe below is unanswerable."
    printf '[INCOMPLETE] %s is not on PATH\n' "${ENGINE}" >&2
    exit 1
fi

# ── an image to run `true` in, WITHOUT pulling ────────────────────────────────────────────
# Same discipline as probe-engine.bash: a pull would make this depend on egress, which is the
# very thing under test.
PROBE_IMAGE=""
if images="$("${ENGINE}" images --format '{{.Repository}}:{{.Tag}}' 2>&1)"; then
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
    out "**No local image found**, so every probe below is UNANSWERED rather than passed — a"
    out "pull would make this depend on the egress under test. Re-run after any \`ccy\` session"
    out "has built \`claude-yolo:latest\`."
    printf '[INCOMPLETE] no local image to probe with\n' >&2
    exit 1
fi

out "Probe image: \`${PROBE_IMAGE}\` (runs \`true\`; every container is \`--rm\`)."
out ""

# ── the runner ────────────────────────────────────────────────────────────────────────────
RUN_RC=0
RUN_OUT=""
run_case() {
    RUN_OUT=""
    if RUN_OUT="$("${ENGINE}" run --rm "$@" "${PROBE_IMAGE}" true 2>&1)"; then
        RUN_RC=0
    else
        RUN_RC=$?
    fi
    return 0
}

report_case() {
    local label="$1"
    shift
    run_case "$@"
    if [[ "${RUN_RC}" -eq 0 ]]; then
        out "- **${label}** → exit 0 (accepted)"
    else
        out "- **${label}** → exit ${RUN_RC} (rejected)"
        out "  \`\`\`"
        out "  ${RUN_OUT}"
        out "  \`\`\`"
    fi
    return 0
}

PASTA_SPEC="pasta:-T,3128"
readonly PASTA_SPEC

out "### Baselines — each flag ALONE"
out ""
out "Without these two, a failure of the combination below is uninterpretable: it would be"
out "equally consistent with pasta simply being unavailable on this host."
out ""
report_case "--network ${PASTA_SPEC}" --network "${PASTA_SPEC}"
pastaAloneRc="${RUN_RC}"
report_case "--network podman" --network podman
podmanAloneRc="${RUN_RC}"

out ""
out "### C3 — both flags TOGETHER, in both orders"
out ""
out "Order is varied because \"last flag wins\" and \"the combination is rejected\" are"
out "different behaviours that a single ordering cannot tell apart."
out ""
report_case "--network ${PASTA_SPEC} --network podman" --network "${PASTA_SPEC}" --network podman
bothRc1="${RUN_RC}"
report_case "--network podman --network ${PASTA_SPEC}" --network podman --network "${PASTA_SPEC}"
bothRc2="${RUN_RC}"

out ""
out "### What these four results license"
out ""
if [[ "${pastaAloneRc}" -ne 0 ]] || [[ "${podmanAloneRc}" -ne 0 ]]; then
    out "**C3 is UNANSWERED on this host.** At least one single-flag baseline failed"
    out "(pasta alone: exit ${pastaAloneRc}; podman alone: exit ${podmanAloneRc}), so a failure"
    out "of the combination cannot be attributed to exclusivity. This is not a refutation of"
    out "C3 and must not be recorded as one — it is the measurement not being available here."
    INCOMPLETE=1
elif [[ "${bothRc1}" -ne 0 ]] && [[ "${bothRc2}" -ne 0 ]]; then
    out "Both singles succeeded and **both orderings of the combination were rejected**. That"
    out "is the pattern C3 asserts, reproduced under this engine — the first direct measurement"
    out "of it rather than a claim borrowed from another repo's runner."
elif [[ "${bothRc1}" -eq 0 ]] && [[ "${bothRc2}" -eq 0 ]]; then
    out "Both singles succeeded and **both orderings of the combination were ACCEPTED**. C3 as"
    out "borrowed is not reproduced here: the flags are not mutually exclusive under this"
    out "engine. Task 5.1 specifies a hard error on the strength of C3, so this outcome"
    out "invalidates that specification and is the result most worth having."
else
    out "Both singles succeeded, but the two orderings **disagreed** (first: exit ${bothRc1};"
    out "second: exit ${bothRc2}). That is neither exclusivity nor acceptance — it looks like"
    out "last-flag-wins or an order-dependent parse, which is a third behaviour C3 does not"
    out "describe and which Task 5.1 does not currently account for."
fi

out ""
out "FACT ONLY — what Task 5.1's specification should become in consequence is a verdict, and"
out "verdicts belong in the acceptance gate, not in triage (PlanScriptStandards R9)."
out ""
out "**C1 and C2 are deliberately NOT probed here.** Both need a listener on the host to mean"
out "anything (C1: that \`-T,3128\` forwards exactly one port; C2: that"
out "\`--map-host-loopback\` exposes the whole host loopback), and a probe that opens host"
out "sockets is no longer read-only. They remain borrowed and unverified, which is recorded"
out "rather than quietly closed by a probe that did something adjacent and easier."

if [[ "${INCOMPLETE}" -ne 0 ]]; then
    printf '[INCOMPLETE] at least one group-C probe could not be answered; see %s\n' "${REPORT}" >&2
    exit 1
fi
printf '==> network probes complete: %s\n' "${REPORT}"

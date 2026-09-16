#!/usr/bin/env bash
# Plan 00122 — acceptance.bash. Renders the VERDICT (R9), against the DEPLOYED artefacts
# rather than the repo copies: Plan 00099's lesson is that a repo can be correct while the
# host runs the old build, and a gate reading the source tree passes through that.
#
# HOST ONLY (R2), enforced by plan_require_host. Both freeze tools refuse to start inside
# a container by design, which is why this plan's remaining tasks need a human.
#
# IT CHANGES STATE, AND ONLY ITS OWN. A freeze that is only read about is no evidence that
# freezing works, so this gate CREATES one throwaway container called
# 'plan00122-freeze-probe', freezes and thaws it, and DESTROYS it from an EXIT trap. Every
# command names that one container. If it cannot build one, the checks needing it report
# NOT ESTABLISHED and the run refuses to render a pass — never a quiet skip, because a
# check that did not run is not a check that passed.
#
# STANDARD-EXCEPTION(R7): declared 'gather' though it changes state. What it changes it
# created and destroys, and declaring 'deploy' would demand plan_gate_change (R8) —
# prompting for consent to a machine change before a read of what the last deploy did.
#
# STANDARD-EXCEPTION(R9): ends on an explicit exit, not plan_finish. The verdict is
# THREE-valued and plan_finish exits 0 or 1, so a probe that never ran would have to be
# reported as one of the two answers it is precisely not.
#
# WHAT IT ASSERTS — outcomes, not the presence of code:
#   0.  precondition: LXC and sudo can answer at all
#   1.  every deployed artefact is byte-identical to its repo copy, at the right mode
#   2.  the shared library sits where BOTH tools resolve it from
#   3.  fzf is present, so both tools offer the same picker
#   4.  both deployed tools start against that library and list containers
#   5.  the shared decisions answer identically under BOTH engines' vocabularies
#   6.  a throwaway container is created and running (or NOT ESTABLISHED)
#   7.  the tool FREEZES it — verified with lxc-info -s, not with the tool's own report
#   8.  running it again THAWS it back, and the DHCP renewal behaves as declared
#   9.  a no-op verb and a dry run change nothing
#   10. a refused sudo is a named failure, never an empty machine
#   11. lxc absent is reported as a missing dependency, not as zero containers
#
# Usage: ./CLAUDE/Plan/00122-lxc-freeze-thaw-shared-with-podfreeze/acceptance.bash
#          [-h|--help] [-y|--yes]
# Exit 0 = ACCEPTED, 1 = REJECTED, 2 = NOT ESTABLISHED (no verdict).
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

PLAN_USAGE="usage: acceptance.bash [-h|--help] [-y|--yes]

The Plan 00122 acceptance gate. Run it on the HOST, after ./deploy.bash, in the
same session.

It checks the DEPLOYED podfreeze, lxcfreeze and freeze-common.bash, and it
CREATES and DESTROYS one throwaway LXC container called 'plan00122-freeze-probe'
so that freeze and thaw are proved against a real container rather than read
about. No other container is named by any command it runs.

--check is rejected: this gate runs no Ansible, so there is nothing to dry-run.

Exit 0 = ACCEPTED, 1 = REJECTED, 2 = NOT ESTABLISHED (no verdict rendered)."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi
# Refused rather than ignored. --check threads a dry run into ansible, this gate runs
# none, and accepting the flag silently would let an operator believe a run that froze a
# real container had changed nothing.
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check is meaningless here: this gate runs no Ansible, and it freezes a real container for real.\n' >&2
    exit 64
fi

plan_require_host "it drives the deployed freeze tools against a real LXC container, and neither tool will start inside a container"
plan_prime_sudo
plan_start_log auto

# ── what is under test ───────────────────────────────────────────────────────────────────
REPO_BIN="${PLAN_REPO_ROOT}/files/home/.local/bin"
REPO_LIB="${PLAN_REPO_ROOT}/files/home/.local/lib/freeze/freeze-common.bash"
DEPLOYED_BIN="${HOME}/.local/bin"
DEPLOYED_LIB="${HOME}/.local/lib/freeze/freeze-common.bash"
LXCFREEZE="${DEPLOYED_BIN}/lxcfreeze"
PODFREEZE="${DEPLOYED_BIN}/podfreeze"
REPORT="${PLAN_RUN_DIR}/acceptance-report.md"

# Generic and plan-scoped on purpose: it says which plan owns it and nothing about this
# machine, and no real container would be called it.
PROBE_CT="plan00122-freeze-probe"
# Scratch for the two PATH harnesses in checks 10 and 11.
PROBE_TMP=""

# 1 once lxc-create has been ATTEMPTED — set before the attempt, not after, so a create
# that failed half-way still gets torn down.
PROBE_MAY_EXIST=0
# 1 once the container is created, started and confirmed RUNNING.
PROBE_READY=0
# 1 if NetworkManager is inside the probe container, which decides which outcome check 8
# must demand of the thaw's DHCP renewal.
PROBE_HAS_NM=0
PROBE_TORN_DOWN=0

# ── the ledger ───────────────────────────────────────────────────────────────────────────
#
# COVERAGE is stated, not inferred. The PASS count cannot carry it: several checks emit
# more than one assertion, and every container-dependent check emits none at all when the
# container could not be built. "ACCEPTED — 14 assertions passed" reads identically
# whether 12 of 12 checks ran or 8 of 12, and coverage implied by a count rather than
# stated is this repo's named recurring defect class.
EXPECTED_CHECKS=(0 1 2 3 4 5 6 7 8 9 10 11)
RAN_CHECKS=()
UNRESOLVED=()
PASS=0
FAIL=0

record() {
    printf '%s\n' "$1" >> "$REPORT"
}

# Announce a check AND record that it ran. Every numbered section starts here; a section
# that prints its own header instead is invisible to COVERAGE.
check() {
    RAN_CHECKS+=("$1")
    echo ""
    echo "[$1] $2"
    record ""
    record "## [$1] $2"
}

ok() {
    echo "  PASS  $1"
    PASS=$((PASS + 1))
    record "- PASS — $1"
}

bad() {
    echo "  FAIL  $1" >&2
    record "- FAIL — $1"
    if [ "$#" -gt 1 ]; then
        echo "        $2" >&2
        record "  - \`$2\`"
    fi
    FAIL=$((FAIL + 1))
}

# NOT ESTABLISHED is its own answer, distinct from both PASS and FAIL. The check ran, so
# it counts towards coverage; it could not reach an answer, so the run must not render a
# pass. Folding it into FAIL would report a broken machine where the truth is an
# unanswered question, and folding it into PASS is the silent skip this gate refuses.
unresolved() {
    UNRESOLVED+=("$1")
    echo "  NOT ESTABLISHED  $2" >&2
    record "- NOT ESTABLISHED — $2"
    if [ "$#" -gt 2 ]; then
        echo "                   $3" >&2
        record "  - \`$3\`"
    fi
}

contains() {
    grep -qF -- "$2" <<< "$1"
}

# ── the throwaway container ──────────────────────────────────────────────────────────────

# probe_exists — does a container of that name exist at all? `lxc-info -s` answers for a
# STOPPED container too, so this is existence, not liveness.
#
# The reason is printed rather than discarded, even though "it does not exist" is the
# expected answer both times this is called — before the container is created and after it
# is destroyed. A probe that swallowed lxc-info's message would look identical when the
# call failed for some other reason entirely.
probe_exists() {
    local out rc=0
    out="$(sudo lxc-info -n "$PROBE_CT" -s 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ]; then
        printf 'probe: lxc-info found no %s — %s\n' "${PROBE_CT}" "${out}" >&2
        return 1
    fi
    return 0
}

# probe_state — the container's state, read with lxc-info DIRECTLY. The success criterion
# is "verified with lxc-info -s rather than by the tool's own report", so this deliberately
# neither calls lxcfreeze nor reuses its parser. stdout is the value (StderrHygiene).
probe_state() {
    local out state rc=0
    out="$(sudo lxc-info -n "$PROBE_CT" -s 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ]; then
        printf 'lxc-info -s failed for the probe container: %s\n' "${out}" >&2
        return 1
    fi
    state="$(printf '%s\n' "$out" | awk '/^State:/ { print $2 }')"
    # An empty parse is a failure, not a state. Returned as a value it would be compared
    # against FROZEN and reported as "the container is  after the freeze" — a defect in
    # this parser dressed up as a defect in the tool under test.
    if [ -z "$state" ]; then
        printf 'lxc-info -s printed no State: line for the probe container: %s\n' "${out}" >&2
        return 1
    fi
    printf '%s' "$state"
}

# probe_teardown — destroy the throwaway container. Idempotent, and it never lets a
# failure pass unmentioned: a container left behind is printed with the command to remove
# it by hand, and the caller re-reads existence afterwards.
#
# Unfreeze first. A frozen container has its processes suspended, so a stop that expects
# them to respond has nothing to talk to.
probe_teardown() {
    local out rc
    if [ "$PROBE_MAY_EXIST" -ne 1 ] || [ "$PROBE_TORN_DOWN" -eq 1 ]; then
        return 0
    fi
    PROBE_TORN_DOWN=1
    echo "" >&2
    echo "==> teardown: removing the throwaway container ${PROBE_CT}" >&2

    rc=0
    out="$(sudo lxc-unfreeze -n "$PROBE_CT" 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "    lxc-unfreeze said (expected when it was not frozen): ${out}" >&2
    fi
    rc=0
    out="$(sudo lxc-stop -n "$PROBE_CT" -k -t 15 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "    lxc-stop said (expected when it was not running): ${out}" >&2
    fi
    rc=0
    out="$(sudo lxc-destroy -n "$PROBE_CT" -f 2>&1)" || rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "    lxc-destroy FAILED: ${out}" >&2
        echo "    Remove it by hand: sudo lxc-destroy -n ${PROBE_CT} -f" >&2
    fi
}

cleanup_tmp() {
    if [ -n "$PROBE_TMP" ] && [ -d "$PROBE_TMP" ]; then
        rm -rf "$PROBE_TMP"
    fi
}

# Registered, not trapped. A plain `trap … EXIT` here would REPLACE the library's own
# handler and lose the run log's final buffered chunk — the lines written as a run was
# dying. `plan_on_cleanup` runs these before the log drains, on EXIT and on INT/TERM/HUP
# alike, so a Ctrl-C'd run still destroys its container.
plan_on_cleanup probe_teardown
plan_on_cleanup cleanup_tmp

# ── the shared decisions, driven under one engine's vocabulary ───────────────────────────
#
# Sources the DEPLOYED library in a subshell and drives its decisions over a fabricated
# three-container inventory, returning a one-line fingerprint. No answer below contains a
# state word, so the two engines' fingerprints must come back IDENTICAL — a library that
# hardcoded either vocabulary produces two different lines, and that is the whole claim
# Phase 4 makes about the shared half.
freeze_decision_fingerprint() {
    (
        FREEZE_TOOL="plan00122-acceptance"
        FREEZE_STATE_RUNNING="$1"
        FREEZE_STATE_FROZEN="$2"
        FREEZE_HOST_ONLY_NOTE="unused: no decision below prints it"
        FREEZE_TARGET_HINT="unused: no decision below prints it"
        # The directive names the REPO copy while the runtime path is the DEPLOYED one.
        # Both are source DIRECTIVES, not suppressions: without them `shellcheck -x` cannot
        # follow the library, and every setting and array below reads as unused (SC2034) —
        # the same lapse R1 warns about, and the same shape lxcfreeze itself uses. Check 1
        # is what establishes that the two copies are the same bytes.
        # shellcheck source-path=SCRIPTDIR
        # shellcheck source=../../../files/home/.local/lib/freeze/freeze-common.bash
        source "$DEPLOYED_LIB"
        INV_NAME=(c1 c2 c3)
        INV_STATE=("$FREEZE_STATE_RUNNING" "$FREEZE_STATE_FROZEN" "$FREEZE_STATE_RUNNING")
        SELECTED=(c1 c2 c3)
        ACTION=""
        freeze_partition freeze c1 c2 ghost
        printf '%s|%s|%s|%s|%s|%s|%s|%s|%s' \
            "$(infer_action)" \
            "$(target_effect c1 c2 c3)" \
            "$(target_effect c2)" \
            "$(row_verb c1)" \
            "$(row_verb c2)" \
            "$(row_verb ghost)" \
            "${FREEZE_ACT_ON[*]}" \
            "${FREEZE_SKIPPED[*]}" \
            "${FREEZE_VANISHED[*]}"
    )
}

# ── the verdict ──────────────────────────────────────────────────────────────────────────
#
# One renderer, called from the end AND from the early exits, so no path can leave without
# a coverage line. A check that is deleted, renumbered or skipped by an early exit
# disappears from RAN_CHECKS and is NAMED here.
render_verdict() {
    local expected missing=()
    for expected in "${EXPECTED_CHECKS[@]}"; do
        case " ${RAN_CHECKS[*]} " in
            *" $expected "*) ;;
            *) missing+=("$expected") ;;
        esac
    done

    # PRINTED, not merely recorded in a report nobody opens. These are claims this plan
    # makes that no gate can settle, and an operator reading a green verdict without them
    # would reasonably conclude the plan was fully proven. They are never counted.
    echo ""
    echo "FOR THE HUMAN — not establishable by this script, not counted above:"
    echo "  * ssh into the container succeeds on the FIRST attempt after a freeze longer"
    echo "    than the one-hour lease. Needs a real hour-long freeze and a container"
    echo "    running sshd and NetworkManager. Check 8 proves the renewal runs on the"
    echo "    thaw path and that a failed renewal is named — not that ssh then works."
    echo "  * podfreeze's fzf picker. It needs a TTY and keystrokes. The checks prove fzf"
    echo "    is installed and that both tools enter one shared implementation; the"
    echo "    keystroke behaviour itself is yours to try."

    echo ""
    echo "=============================================================="
    echo "COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed" \
        "(${PASS} assertion(s) passed, ${FAIL} failed)"
    record ""
    record "## Verdict"
    record ""
    record "COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed (${PASS} passed, ${FAIL} failed)"
    if [ "${#missing[@]}" -ne 0 ]; then
        echo "  NOT RUN: ${missing[*]}" >&2
        record "NOT RUN: ${missing[*]}"
    fi
    if [ "${#UNRESOLVED[@]}" -ne 0 ]; then
        echo "  NOT ESTABLISHED: ${UNRESOLVED[*]}" >&2
        record "NOT ESTABLISHED: ${UNRESOLVED[*]}"
    fi

    plan_list_reports

    if [ "$FAIL" -gt 0 ]; then
        echo "REJECTED — $FAIL assertion(s) failed, $PASS passed." >&2
        echo "  Fix, re-run ./deploy.bash, then re-run this gate." >&2
        echo "==============================================================" >&2
        record "REJECTED"
        exit 1
    fi
    if [ "${#missing[@]}" -ne 0 ] || [ "${#UNRESOLVED[@]}" -ne 0 ]; then
        echo "NOT ESTABLISHED — nothing failed, but this run did not answer every" >&2
        echo "  question it declares. THIS IS NOT AN ACCEPTANCE: a check that did not" >&2
        echo "  run is not a check that passed. Resolve the items above and re-run." >&2
        echo "==============================================================" >&2
        record "NOT ESTABLISHED — this is not an acceptance"
        exit 2
    fi
    echo "ACCEPTED — every declared check ran and every assertion passed."
    echo "=============================================================="
    record "ACCEPTED"
    exit 0
}

# ── the run ──────────────────────────────────────────────────────────────────────────────

record "# Plan 00122 acceptance — lxcfreeze, podfreeze and the shared freeze library"
record ""
record "Deployed artefacts under test:"
record ""
record "- \`${LXCFREEZE}\`"
record "- \`${PODFREEZE}\`"
record "- \`${DEPLOYED_LIB}\`"

echo "=============================================================="
echo "Plan 00122 acceptance — freeze/thaw, and the library both tools share"
echo "=============================================================="

# --- 0. precondition: this host can answer the question at all ---------------------------
# Without LXC or without sudo every probe below would come back empty, and an empty answer
# read as a pass is precisely what lxcfreeze's own guards exist to refuse.
check 0 "precondition: LXC is installed and sudo can be used non-interactively"
missing_bins=()
for probe_bin in lxc-ls lxc-info lxc-create lxc-start lxc-stop lxc-destroy lxc-wait \
    lxc-freeze lxc-unfreeze lxc-attach; do
    if ! command -v "$probe_bin" > /dev/null; then
        missing_bins+=("$probe_bin")
    fi
done
if [ "${#missing_bins[@]}" -ne 0 ]; then
    unresolved 0 "LXC is not installed here — missing: ${missing_bins[*]}" \
        "ansible-playbook playbooks/imports/play-lxc-install-config.yml"
    render_verdict
fi
ok "every lxc binary this gate drives is present"

sudo_rc=0
sudo_out="$(sudo -n true 2>&1)" || sudo_rc=$?
if [ "$sudo_rc" -ne 0 ]; then
    unresolved 0 "sudo is not usable non-interactively, so no LXC state can be read" \
        "run 'sudo -v' in this terminal and re-run: ${sudo_out}"
    render_verdict
fi
ok "sudo answers non-interactively (every LXC query here is rootful)"

# --- 1. the deployed artefacts are the repo's --------------------------------------------
# The subject is the HOST. Every check below drives the deployed copies, so this is what
# licenses reading their behaviour as a statement about the repo.
check 1 "deployed artefacts are byte-identical to their repo copies, at the declared mode"
artefacts_present=1
for pair in "${REPO_BIN}/lxcfreeze:${LXCFREEZE}:755" \
    "${REPO_BIN}/podfreeze:${PODFREEZE}:755" \
    "${REPO_LIB}:${DEPLOYED_LIB}:644"; do
    repo_file="${pair%%:*}"
    rest="${pair#*:}"
    host_file="${rest%:*}"
    want_mode="${rest##*:}"
    if [ ! -f "$repo_file" ]; then
        bad "the repo copy is missing: ${repo_file}" "this checkout is incomplete"
        continue
    fi
    if [ ! -f "$host_file" ]; then
        artefacts_present=0
        bad "not deployed: ${host_file}" "run ./deploy.bash — both plays, in that order"
        continue
    fi
    repo_sum="$(sha256sum < "$repo_file")"
    host_sum="$(sha256sum < "$host_file")"
    if [ "$repo_sum" != "$host_sum" ]; then
        bad "${host_file} has drifted from ${repo_file}" \
            "the host is running a different build — run ./deploy.bash"
    else
        ok "$(basename "$host_file") matches its repo copy"
    fi
    host_mode="$(stat -c '%a' "$host_file")"
    if [ "$host_mode" != "$want_mode" ]; then
        bad "$(basename "$host_file") is mode ${host_mode}, expected ${want_mode}" \
            "the library is sourced and must not be executable; the tools must be"
    else
        ok "$(basename "$host_file") is mode ${want_mode}"
    fi
done
if [ "$artefacts_present" -ne 1 ]; then
    echo "" >&2
    echo "Stopping here: the artefacts under test are not on this host, so nothing" >&2
    echo "below would be testing this plan's work. Run ./deploy.bash first." >&2
    render_verdict
fi

# --- 2. the library is where BOTH tools resolve it from ----------------------------------
# Each tool finds the library by ONE relative hop from its own directory
# ("$dir/../lib/freeze/freeze-common.bash"), which is what lets the same line work from a
# checkout and from ~/.local/bin. So the fact to establish is not "a library exists"; it is
# that the hop each tool actually performs lands on the deployed file.
check 2 "the shared library sits where both deployed tools resolve it from"
resolved_lib="${DEPLOYED_BIN}/../lib/freeze/freeze-common.bash"
if [ ! -r "$resolved_lib" ]; then
    bad "the one relative hop both tools make does not reach a readable library" \
        "expected ${resolved_lib}"
elif [ ! "$resolved_lib" -ef "$DEPLOYED_LIB" ]; then
    bad "that hop reaches a DIFFERENT file from ${DEPLOYED_LIB}" \
        "two copies of the library on one host means two behaviours"
else
    ok "both tools' relative hop resolves to ${DEPLOYED_LIB}"
fi
for tool in "$LXCFREEZE" "$PODFREEZE"; do
    tool_src="$(cat "$tool")"
    if contains "$tool_src" '/../lib/freeze/freeze-common.bash'; then
        ok "$(basename "$tool") resolves the library by that same relative hop"
    else
        bad "$(basename "$tool") does not look for the library at ../lib/freeze/" \
            "the deployed build predates Phase 4 — run ./deploy.bash"
    fi
done

# --- 3. the picker the library branches on -----------------------------------------------
# fzf is deployed WITH the library, by the task file both plays include, precisely so a
# host that ran only one play cannot end up with one tool on fzf and the other on the
# numbered menu. Absent, both tools still work — and both plays' promise that the menus are
# identical stops being backed by anything.
check 3 "fzf is installed, so both tools offer the same picker"
if command -v fzf > /dev/null; then
    ok "fzf is present"
else
    bad "fzf is not installed" \
        "tasks/deploy-freeze-lib.yml installs it — run ./deploy.bash"
fi

# --- 4. both deployed tools start, against that library, and list -------------------------
# Neither tool starts without the library, so a successful `list` is positive proof that the
# deployed tool loaded the deployed library and ran the shared table code — which grepping
# the file for a path cannot establish.
check 4 "both deployed tools start against the library and list containers"
for tool in "$LXCFREEZE" "$PODFREEZE"; do
    list_rc=0
    list_out="$("$tool" list 2>&1)" || list_rc=$?
    if [ "$list_rc" -ne 0 ]; then
        bad "$(basename "$tool") list exited ${list_rc}" "$list_out"
    elif ! contains "$list_out" "=== frozen ("; then
        bad "$(basename "$tool") list printed no frozen section" "$list_out"
    elif ! contains "$list_out" "=== running ("; then
        bad "$(basename "$tool") list printed no running section" "$list_out"
    else
        ok "$(basename "$tool") list ran and printed both sections"
    fi
done

# --- 5. the shared decisions are engine-blind ---------------------------------------------
# Phase 4's claim is that the two tools cannot teach different habits because the decisions
# and the menu are ONE implementation. This drives that implementation — the deployed one —
# under podman's vocabulary and LXC's, and demands the same answers.
check 5 "the shared decisions answer identically under both engines' state vocabularies"
FP_EXPECTED='freeze|FREEZE 2|THAW   1|FREEZE|THAW|?|c1|c2|ghost'
fp_pod_rc=0
fp_pod="$(freeze_decision_fingerprint running paused)" || fp_pod_rc=$?
fp_lxc_rc=0
fp_lxc="$(freeze_decision_fingerprint RUNNING FROZEN)" || fp_lxc_rc=$?
if [ "$fp_pod_rc" -ne 0 ] || [ "$fp_lxc_rc" -ne 0 ]; then
    bad "the deployed library could not be driven" \
        "podman pass rc=${fp_pod_rc} (${fp_pod}); LXC pass rc=${fp_lxc_rc} (${fp_lxc})"
elif [ "$fp_pod" != "$fp_lxc" ]; then
    bad "the same decisions answer differently for the two engines" \
        "podman: ${fp_pod} / LXC: ${fp_lxc}"
elif [ "$fp_pod" != "$FP_EXPECTED" ]; then
    # Identical is not enough: identically wrong is also identical.
    bad "the shared decisions agree with each other but not with the contract" \
        "got ${fp_pod}, expected ${FP_EXPECTED}"
else
    ok "derived verb, group effect, row verb and the three-way partition all match"
    ok "and they match for podman's running/paused as well as LXC's RUNNING/FROZEN"
fi

# --- 6. a real container to act on --------------------------------------------------------
# Built here rather than borrowed: acting on a container someone is using is not a gate's
# business, and a gate that quietly acts on nothing when it finds nothing is worse than one
# that says it could not run.
check 6 "a throwaway container is created and running"
if probe_exists; then
    unresolved 6 "a container called ${PROBE_CT} already exists on this host" \
        "this gate will not touch a container it did not create — remove it and re-run"
elif [ ! -x /usr/share/lxc/templates/lxc-download ]; then
    unresolved 6 "the lxc download template is not installed" \
        "expected /usr/share/lxc/templates/lxc-download from the lxc-templates package"
else
    case "$(uname -m)" in
        x86_64) probe_arch="amd64" ;;
        aarch64) probe_arch="arm64" ;;
        *) probe_arch="$(uname -m)" ;;
    esac
    # Alpine because it is the smallest rootfs the index carries, and 'edge' because a
    # pinned release number rots into a gate that stops running one day with no code change.
    # Nothing here depends on the distribution: the container exists to be frozen.
    PROBE_MAY_EXIST=1
    create_rc=0
    create_out="$(sudo lxc-create -n "$PROBE_CT" -t download -- \
        --dist alpine --release edge --arch "$probe_arch" 2>&1)" || create_rc=$?
    if [ "$create_rc" -ne 0 ]; then
        unresolved 6 "could not create a throwaway container (it needs network to reach the image index)" \
            "lxc-create exited ${create_rc}: ${create_out}"
    else
        ok "created ${PROBE_CT} from the download template"
        start_rc=0
        start_out="$(sudo lxc-start -n "$PROBE_CT" -d 2>&1)" || start_rc=$?
        if [ "$start_rc" -ne 0 ]; then
            unresolved 6 "the throwaway container would not start" \
                "lxc-start exited ${start_rc}: ${start_out}"
        else
            wait_rc=0
            wait_out="$(sudo lxc-wait -n "$PROBE_CT" -s RUNNING -t 30 2>&1)" || wait_rc=$?
            probe_st=""
            if [ "$wait_rc" -ne 0 ]; then
                unresolved 6 "the throwaway container did not reach RUNNING within 30s" \
                    "lxc-wait exited ${wait_rc}: ${wait_out}"
            elif ! probe_st="$(probe_state)"; then
                unresolved 6 "lxc-info could not read the throwaway container's state"
            elif [ "$probe_st" != "RUNNING" ]; then
                unresolved 6 "the throwaway container is ${probe_st}, not RUNNING"
            else
                PROBE_READY=1
                ok "${PROBE_CT} is RUNNING, per lxc-info -s"
                # Which outcome check 8 must demand of the thaw. lxcfreeze renews the DHCP
                # lease through NetworkManager and declares — in the play's ready message
                # and in docs/playbooks.md — that a container without it FAILS the renewal
                # loudly. So the assertion is chosen from the container rather than
                # assumed: where nmcli is present a failed renewal is a defect, and where
                # it is absent the named failure IS the declared behaviour.
                nm_rc=0
                nm_out="$(sudo lxc-attach -n "$PROBE_CT" -- \
                    sh -c 'command -v nmcli' 2>&1)" || nm_rc=$?
                if [ "$nm_rc" -eq 0 ]; then
                    PROBE_HAS_NM=1
                    ok "the throwaway container has nmcli, so thaw must renew its lease"
                else
                    ok "the throwaway container has no nmcli (${nm_out}), so thaw must FAIL the renewal by name"
                fi
            fi
        fi
    fi
fi

# --- 7. the tool freezes a real container --------------------------------------------------
# No verb is given, so the verb is DERIVED: the container is running, so this must freeze it.
# The state is then read with lxc-info directly — the success criterion says "rather than by
# the tool's own report", because a tool that lied about what it did would pass a gate that
# believed it.
check 7 "lxcfreeze freezes a running container, verified with lxc-info -s"
if [ "$PROBE_READY" -ne 1 ]; then
    unresolved 7 "no running throwaway container to freeze (see check 6)"
else
    freeze_rc=0
    freeze_out="$("$LXCFREEZE" "$PROBE_CT" 2>&1)" || freeze_rc=$?
    if [ "$freeze_rc" -ne 0 ]; then
        bad "lxcfreeze exited ${freeze_rc} freezing ${PROBE_CT}" "$freeze_out"
    else
        ok "lxcfreeze reported success"
    fi
    state_after_freeze=""
    if ! state_after_freeze="$(probe_state)"; then
        bad "could not read the container's state after the freeze"
    elif [ "$state_after_freeze" != "FROZEN" ]; then
        bad "the container is ${state_after_freeze} after the freeze, not FROZEN" "$freeze_out"
    else
        ok "lxc-info -s says FROZEN — the state really changed"
    fi
    # Task 5.4's note, printed beside the thaw instruction because that is the last moment
    # the cost is avoidable. It is only ever emitted on the freeze path.
    if contains "$freeze_out" "Thaw them with: lxcfreeze thaw"; then
        ok "the freeze said how to undo itself"
    else
        bad "the freeze printed no thaw instruction" "$freeze_out"
    fi
    if contains "$freeze_out" "every ssh session into it"; then
        ok "and warned that ssh sessions into the container die with the freeze"
    else
        bad "the freeze note about severed ssh sessions was not printed" "$freeze_out"
    fi
fi

# --- 8. and thaws it back -------------------------------------------------------------------
# The SAME command again. That is the toggle the plan promises: the verb is derived from
# current state, so running lxcfreeze twice on one target freezes it and then thaws it.
check 8 "the same command run again thaws it back, and the DHCP renewal behaves as declared"
if [ "$PROBE_READY" -ne 1 ]; then
    unresolved 8 "no throwaway container to thaw (see check 6)"
else
    thaw_rc=0
    thaw_out="$("$LXCFREEZE" "$PROBE_CT" 2>&1)" || thaw_rc=$?
    state_after_thaw=""
    if ! state_after_thaw="$(probe_state)"; then
        bad "could not read the container's state after the thaw"
    elif [ "$state_after_thaw" != "RUNNING" ]; then
        bad "the container is ${state_after_thaw} after the thaw, not RUNNING" "$thaw_out"
    else
        ok "lxc-info -s says RUNNING again — the same command toggled it back"
    fi
    if [ "$PROBE_HAS_NM" -eq 1 ]; then
        if [ "$thaw_rc" -ne 0 ]; then
            bad "the thaw exited ${thaw_rc} on a container that HAS NetworkManager" "$thaw_out"
        elif ! contains "$thaw_out" "✓ ${PROBE_CT}"; then
            bad "the thaw did not report the container as done" "$thaw_out"
        else
            ok "the thaw succeeded and renewed the lease through NetworkManager"
        fi
    else
        # Task 5.1's contract, and the reason this is an assertion rather than a tolerated
        # failure: a container with no NetworkManager MUST fail the renewal, by name, in a
        # message that says the container itself IS thawed. A silent success here would
        # mean the renewal was never attempted at all.
        if [ "$thaw_rc" -eq 0 ]; then
            bad "the thaw reported success on a container that cannot renew its lease" \
                "the renewal is meant to be a named failure, not a silent one: ${thaw_out}"
        elif ! contains "$thaw_out" "thawed, but the DHCP lease was not renewed"; then
            bad "the failed renewal was not named, or did not say the container is thawed" "$thaw_out"
        else
            ok "the failed renewal is a named failure whose message says the container IS thawed"
        fi
    fi
fi

# --- 9. a no-op verb and a dry run change nothing -------------------------------------------
check 9 "an explicit no-op verb and a dry run leave the container alone"
if [ "$PROBE_READY" -ne 1 ]; then
    unresolved 9 "no throwaway container to leave alone (see check 6)"
else
    noop_rc=0
    noop_out="$("$LXCFREEZE" thaw "$PROBE_CT" 2>&1)" || noop_rc=$?
    noop_state=""
    if [ "$noop_rc" -ne 0 ]; then
        bad "thawing an already-running container exited ${noop_rc}" "$noop_out"
    elif ! contains "$noop_out" "Nothing to do"; then
        bad "an explicit thaw of a running container did not report a no-op" "$noop_out"
    elif ! contains "$noop_out" "Skipped — not currently FROZEN"; then
        bad "the skipped container was not named, or not with LXC's state word" "$noop_out"
    else
        ok "an explicit thaw of a running container is a named no-op"
    fi
    if ! noop_state="$(probe_state)"; then
        bad "could not read the container's state after the no-op thaw"
    elif [ "$noop_state" != "RUNNING" ]; then
        bad "the no-op thaw changed the state to ${noop_state}" "$noop_out"
    else
        ok "and it changed nothing"
    fi

    dry_rc=0
    dry_out="$("$LXCFREEZE" freeze "$PROBE_CT" -n 2>&1)" || dry_rc=$?
    dry_state=""
    if [ "$dry_rc" -ne 0 ]; then
        bad "the dry run exited ${dry_rc}" "$dry_out"
    elif ! contains "$dry_out" "DRY RUN"; then
        bad "the dry run did not announce itself" "$dry_out"
    else
        ok "the dry run previewed the freeze"
    fi
    if ! dry_state="$(probe_state)"; then
        bad "could not read the container's state after the dry run"
    elif [ "$dry_state" != "RUNNING" ]; then
        bad "the dry run FROZE the container (state ${dry_state})" "$dry_out"
    else
        ok "and the container is still RUNNING, so it changed nothing"
    fi
fi

# The container's work is done. Torn down here rather than only in the trap, so the host is
# clean before the last two checks and the removal itself is verified.
probe_teardown
if [ "$PROBE_MAY_EXIST" -eq 1 ]; then
    if probe_exists; then
        bad "the throwaway container ${PROBE_CT} is still on this host" \
            "remove it by hand: sudo lxc-destroy -n ${PROBE_CT} -f"
    else
        ok "the throwaway container was removed"
    fi
fi

# --- 10. a refused sudo is a named failure, not an empty machine ------------------------------
# Driven with a sudo on PATH that always refuses, which is the one way to reach this branch
# without editing the host's sudoers. Everything else is the real deployed tool on the real
# host. The assertion that matters is the NEGATIVE one: a tool that answered "nothing here"
# would be reporting a machine it was never able to look at.
check 10 "a refused sudo is a named failure, never an empty container list"
PROBE_TMP="$(mktemp -d)"
refuse_dir="${PROBE_TMP}/refusing-sudo"
mkdir -p "$refuse_dir"
printf '%s\n' '#!/bin/sh' 'exit 1' > "${refuse_dir}/sudo"
chmod 0755 "${refuse_dir}/sudo"
refuse_rc=0
refuse_out="$(PATH="${refuse_dir}:${PATH}" "$LXCFREEZE" list 2>&1)" || refuse_rc=$?
if contains "$refuse_out" "command not found"; then
    unresolved 10 "this harness could not drive the tool" "$refuse_out"
elif [ "$refuse_rc" -eq 0 ]; then
    bad "lxcfreeze exited 0 with sudo refusing every query" "$refuse_out"
elif ! contains "$refuse_out" "sudo was refused"; then
    bad "the refusal was not named" "$refuse_out"
elif contains "$refuse_out" "=== frozen ("; then
    bad "it printed a container list it could not have read" "$refuse_out"
else
    ok "a refused sudo names itself and prints no list at all"
fi

# --- 11. lxc absent is a missing dependency, not zero containers -------------------------------
# The same shape from the other side: a PATH holding only what the tool needs to REACH its own
# guard. `lxc` absent and `lxc` present with nothing running are different answers, and
# reporting the first as the second would tell someone their containers are gone.
check 11 "lxc absent is reported as a missing dependency, distinguishable from zero containers"
bare_dir="${PROBE_TMP}/no-lxc"
mkdir -p "$bare_dir"
harness_ok=1
for needed in bash dirname; do
    needed_path="$(command -v "$needed")"
    if ! ln -s "$needed_path" "${bare_dir}/${needed}"; then
        harness_ok=0
    fi
done
if [ "$harness_ok" -ne 1 ]; then
    unresolved 11 "could not build a PATH without lxc to drive the guard"
else
    nolxc_rc=0
    nolxc_out="$(PATH="$bare_dir" "$LXCFREEZE" list 2>&1)" || nolxc_rc=$?
    if [ "$nolxc_rc" -eq 0 ]; then
        bad "lxcfreeze exited 0 with no lxc on PATH" "$nolxc_out"
    elif contains "$nolxc_out" "LXC is not installed"; then
        if contains "$nolxc_out" "play-lxc-install-config.yml"; then
            ok "it says LXC is not installed and names the play that installs it"
        else
            bad "it reported LXC missing without naming the play that deploys it" "$nolxc_out"
        fi
        if contains "$nolxc_out" "no running or frozen containers"; then
            bad "it also claimed there are no containers, which it cannot know" "$nolxc_out"
        else
            ok "and it does not claim the machine has no containers"
        fi
    elif contains "$nolxc_out" "command not found"; then
        unresolved 11 "this harness could not drive the tool" "$nolxc_out"
    else
        bad "a missing LXC was not reported as a missing dependency" "$nolxc_out"
    fi
fi
cleanup_tmp
PROBE_TMP=""

render_verdict

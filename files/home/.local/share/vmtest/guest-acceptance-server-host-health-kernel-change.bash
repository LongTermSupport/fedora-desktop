#!/usr/bin/bash
# guest-acceptance-server-host-health-kernel-change.bash — the assertion set for
# Plan 00109's server drift-reporting route, run INSIDE the guest AFTER the
# harness has rebooted it into a different kernel (DESIGN-server-route.md §4).
#
# The scenario this whole route was built for: a status document collected under
# one kernel, read by a login shell running another. Everything before the reboot
# was set up by guest-prepare-server-host-health-kernel-change.bash, which judges
# nothing and records what it saw; this script reads that record and does all the
# judging, so the transcript declares its check count exactly once.
#
# Contract (helpers/vmtest/transcript.py parses exactly these lines):
#   VMTEST-CHECK-PLANNED <n>                declared BEFORE the first check
#   VMTEST-CHECK pass|fail|skip <name> [detail]
#   VMTEST-EVIDENCE <key>=<value>
#   VMTEST-CHECKS-DONE total=N passed=N failed=N skipped=N
#
# `planned` is fixed by the list of checks below and must equal the scenario's
# `planned` in vars/vm-test-scenarios.yml. Every check runs even after a failure
# — the transcript should show the whole picture, not the first crack.
#
# Inputs (environment, set by the host over SSH):
#   VMTEST_COMMIT       the 40-hex commit the guest was told to provision from
#   VMTEST_USER_EMAIL   the git identity run.bash was given
set -uo pipefail

PLANNED=14
readonly PLANNED
REPO="${HOME}/Projects/fedora-desktop"
STATE_DIR="${XDG_STATE_HOME:-${HOME}/.local/state}/fedora-desktop"
DOCUMENT="${STATE_DIR}/host-status.json"
UNITS_DIR="${HOME}/.config/systemd/user"
EVIDENCE_DIR="${HOME}/.vmtest"
PREPARED="${EVIDENCE_DIR}/host-health-prepared.env"
SELF_KEY="${EVIDENCE_DIR}/selfscp"
# The heading login_message.py puts above the group that is NOT a set of current
# faults. Matching on a fragment of it, not the whole sentence: the words that
# carry the meaning are what a reader relies on, and pinning the punctuation as
# well would make this fail on a rewording that changed nothing.
NOT_CHECKED_HEADING="Not checked"
readonly REPO STATE_DIR DOCUMENT UNITS_DIR EVIDENCE_DIR PREPARED SELF_KEY NOT_CHECKED_HEADING

total=0
passed=0
failed=0
skipped=0

check() {
    # check <status> <name> [detail]
    local status="${1:?}" name="${2:?}" detail="${3:-}"
    total=$((total + 1))
    case "${status}" in
        pass) passed=$((passed + 1)) ;;
        fail) failed=$((failed + 1)) ;;
        skip) skipped=$((skipped + 1)) ;;
        *)
            echo "ERROR: check status must be pass|fail|skip, got ${status}" >&2
            exit 70
            ;;
    esac
    detail="$(printf '%s' "${detail}" | tr '\n' ' ')"
    if [[ -n "${detail}" ]]; then
        printf 'VMTEST-CHECK %s %s %s\n' "${status}" "${name}" "${detail}"
    else
        printf 'VMTEST-CHECK %s %s\n' "${status}" "${name}"
    fi
}

evidence() {
    printf 'VMTEST-EVIDENCE %s=%s\n' "${1:?}" "$(printf '%s' "${2-}" | tr '\n' ' ')"
}

unb64() { printf '%s' "${1-}" | base64 -d; }

# group_of <message> <needle> — which group of the report a line sits in:
# `findings` (a current fault), `unchecked` (below the not-checked heading), or
# `absent`. The whole value of this report is that those two groups are kept
# apart, so "the text appears somewhere" is not a claim worth making about it.
group_of() {
    HH_MESSAGE="${1-}" HH_NEEDLE="${2-}" HH_HEADING="${NOT_CHECKED_HEADING}" python3 -c '
import os
needle = os.environ["HH_NEEDLE"]
heading = os.environ["HH_HEADING"]
below = False
for line in os.environ["HH_MESSAGE"].splitlines():
    if heading in line:
        below = True
        continue
    if needle and needle in line:
        print("unchecked" if below else "findings")
        break
else:
    print("absent")
'
}

# a_login — what an interactive login shell prints, right now, on this boot.
a_login() {
    local rc=0 output
    output="$(bash -lic true </dev/null 2>&1)" || rc=$?
    LOGIN_RC="${rc}"
    LOGIN_OUTPUT="${output}"
}

# an_scp <destination> — a real transfer through this guest's own sshd, which is
# the path an unconditional print on stdout breaks. The key was made by the
# prepare step; the lab's own private key never enters a guest.
an_scp() {
    local destination="${1:?}" rc=0 output
    output="$(scp -q -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
        -o LogLevel=ERROR -i "${SELF_KEY}" \
        "${USER}@localhost:/etc/os-release" "${destination}" 2>&1)" || rc=$?
    SCP_RC="${rc}"
    SCP_OUTPUT="${output}"
    SCP_BYTES=0
    if [[ -r "${destination}" ]]; then
        SCP_BYTES="$(stat -c %s "${destination}")"
    fi
}

printf 'VMTEST-CHECK-PLANNED %d\n' "${PLANNED}"

# The prepare step's record. Without it there is nothing to compare this boot
# against, so every check below would be judging one half of a two-boot claim.
if [[ -r "${PREPARED}" ]]; then
    # shellcheck source=/dev/null
    source "${PREPARED}"
else
    echo "ERROR: no prepare record at ${PREPARED}; the fixture did not run" >&2
    exit 70
fi

running_kernel="$(uname -r)"
clean_login="$(unb64 "${CLEAN_LOGIN_B64:-}")"
finding_login="$(unb64 "${FINDING_LOGIN_B64:-}")"

# ── 1. the play armed the collection timer ────────────────────────────────────────────
if [[ "${PREPARED_TIMER_ENABLED:-}" == "enabled" ]]; then
    check pass collect-timer-was-armed "${PREPARED_TIMER_ENABLED}"
else
    check fail collect-timer-was-armed "is-enabled said '${PREPARED_TIMER_ENABLED:-}', not 'enabled'"
fi

# ── 2. the collector ran to completion ────────────────────────────────────────────────
if [[ "${CLEAN_COLLECT_RC:-1}" == "0" ]]; then
    check pass collect-service-ran "$(unb64 "${CLEAN_COLLECT_STATUS_B64:-}")"
else
    check fail collect-service-ran "systemctl start exited ${CLEAN_COLLECT_RC:-} ($(unb64 "${CLEAN_COLLECT_STATUS_B64:-}"))"
fi

# ── 3. the document names the kernel it was collected under ───────────────────────────
# The whole predicate rests on this field being the producer's own answer rather
# than a default, so it is checked before anything is concluded from a mismatch.
if [[ -n "${PREPARED_DOCUMENT_KERNEL:-}" && "${PREPARED_DOCUMENT_KERNEL}" == "${PREPARED_RUNNING_KERNEL:-}" ]]; then
    check pass document-names-the-collecting-kernel "${PREPARED_DOCUMENT_KERNEL}"
else
    check fail document-names-the-collecting-kernel \
        "document says '${PREPARED_DOCUMENT_KERNEL:-}', the collecting boot was '${PREPARED_RUNNING_KERNEL:-}'"
fi

# ── 4. a clean server login says NOTHING ──────────────────────────────────────────────
# The claim that decides whether this surface survives contact with a user. A
# report that speaks on every login is one that gets muted, and a muted report
# is this plan's incident with extra steps.
if [[ -z "${clean_login}" ]]; then
    check pass clean-login-is-silent
else
    check fail clean-login-is-silent "a clean host printed: ${clean_login}"
fi

# ── 5. a fault present at collection is reported AS a fault ───────────────────────────
fixture_finding="${PREPARED_FIXTURE_FINDING:-}"
before_group="$(group_of "${finding_login}" "${fixture_finding}")"
if [[ "${before_group}" == "findings" ]]; then
    check pass login-reports-a-live-fault "${fixture_finding}"
else
    check fail login-reports-a-live-fault \
        "'${fixture_finding}' was ${before_group} in the pre-reboot report, not a current finding"
fi

# ── 6. an scp completed before the reboot ─────────────────────────────────────────────
if [[ "${PREPARED_SCP_RC:-1}" == "0" && "${PREPARED_SCP_BYTES:-0}" -gt 0 ]]; then
    check pass scp-completes-before-reboot "${PREPARED_SCP_BYTES} bytes"
else
    check fail scp-completes-before-reboot \
        "exit ${PREPARED_SCP_RC:-}, ${PREPARED_SCP_BYTES:-0} bytes: $(unb64 "${PREPARED_SCP_OUTPUT_B64:-}")"
fi

# ── 7. this boot is a DIFFERENT kernel ────────────────────────────────────────────────
# The premise of everything below it. Both halves are asserted: not merely that
# the kernel changed, but that it changed to the one the fixture selected — a
# guest that fell back to its old entry would still satisfy "not equal" if the
# fixture had also installed a third.
if [[ "${running_kernel}" == "${PREPARED_TARGET_KERNEL:-}" && "${running_kernel}" != "${PREPARED_RUNNING_KERNEL:-}" ]]; then
    check pass rebooted-into-a-different-kernel "${PREPARED_RUNNING_KERNEL} -> ${running_kernel}"
else
    check fail rebooted-into-a-different-kernel \
        "running ${running_kernel}; collected under ${PREPARED_RUNNING_KERNEL:-}, selected ${PREPARED_TARGET_KERNEL:-}"
fi

# ── 8. the document is the one collected on the previous boot ─────────────────────────
# If the collector had run again, it would name this kernel and there would be no
# mismatch left to report — the checks below would pass on a document that never
# went stale. This is what makes them mean what they say.
document_sha=""
if [[ -r "${DOCUMENT}" ]]; then
    document_sha="$(sha256sum "${DOCUMENT}" | cut -d' ' -f1)"
fi
if [[ -n "${document_sha}" && "${document_sha}" == "${PREPARED_DOCUMENT_SHA:-}" ]]; then
    check pass document-was-not-recollected "${document_sha:0:12}"
else
    check fail document-was-not-recollected \
        "document is ${document_sha:0:12}, the prepared one was ${PREPARED_DOCUMENT_SHA:0:12}"
fi

# ── 9. the login names the boot mismatch ──────────────────────────────────────────────
a_login
after_login="${LOGIN_OUTPUT}"
if [[ "${after_login}" == *"collected under kernel ${PREPARED_RUNNING_KERNEL:-}"* &&
    "${after_login}" == *"now running ${running_kernel}"* ]]; then
    check pass login-names-the-boot-mismatch "${PREPARED_RUNNING_KERNEL} -> ${running_kernel}"
else
    check fail login-names-the-boot-mismatch "the report did not name both kernels: ${after_login}"
fi

# ── 10. the previous boot's fault is DEMOTED, not repeated as current ─────────────────
# "no DKMS module installed for the running kernel 7.1.9" reads as present tense
# while naming a kernel that is not running. Left among the current faults it puts
# two different values for "the running kernel" on consecutive lines of one report,
# one of them wrong, in exactly the scenario the rule exists for.
after_group="$(group_of "${after_login}" "${fixture_finding}")"
if [[ "${after_group}" == "unchecked" ]]; then
    check pass boot-findings-are-demoted-not-current "'${fixture_finding}' sits below the not-checked heading"
else
    check fail boot-findings-are-demoted-not-current \
        "'${fixture_finding}' was ${after_group} after the reboot, not demoted"
fi

# ── 11. an scp still completes when the report HAS something to say ───────────────────
# The one that matters. Before the reboot the guard was never under load; here the
# report is non-empty, which is precisely when an unconditional print corrupts the
# transfer.
an_scp "${EVIDENCE_DIR}/scp-after.out"
if [[ "${SCP_RC}" == "0" && "${SCP_BYTES}" -gt 0 ]]; then
    check pass scp-completes-after-reboot "${SCP_BYTES} bytes with a non-empty report"
else
    check fail scp-completes-after-reboot "exit ${SCP_RC}, ${SCP_BYTES} bytes: ${SCP_OUTPUT}"
fi

# ── 12. the play's unit files are still where it put them ─────────────────────────────
# The fixture disables the timer, and disabling must leave the units in place: a
# host missing them is not the host the play describes, and the checks above would
# be judging something else.
missing=""
for unit in host-health-collect.service host-health-collect.timer; do
    if [[ ! -r "${UNITS_DIR}/${unit}" ]]; then
        missing="${missing} ${unit}"
    fi
done
if [[ -z "${missing}" ]]; then
    check pass collect-units-are-installed "${UNITS_DIR}"
else
    check fail collect-units-are-installed "absent from ${UNITS_DIR}:${missing}"
fi

# ── 13. the remote fetches with no agent in the environment ───────────────────────────
# What the collection timer has: a systemd --user unit with no ssh-agent. A remote
# only reachable through an agent would leave the freshness axis reporting "never
# reached the remote" for ever (§6).
fetch_rc=0
fetch_output="$(env -u SSH_AUTH_SOCK -u SSH_AGENT_PID git -C "${REPO}" fetch --dry-run origin 2>&1)" || fetch_rc=$?
if [[ "${fetch_rc}" == "0" ]]; then
    check pass remote-fetches-without-an-agent "${fetch_output:-no new refs}"
else
    check fail remote-fetches-without-an-agent "git fetch exited ${fetch_rc}: ${fetch_output}"
fi

# ── 14. reporting never costs the user their login ────────────────────────────────────
# `login_message.main` always exits 0 and the snippet always `return 0`s, for one
# reason: a non-zero status out of a file sourced by a login profile can trip
# `set -e` in the surrounding shell, and a health reporter that locks a user out
# of the host it reports on is worse than no reporter. Three shells, one claim —
# clean, with findings, and across the boot mismatch.
login_statuses="clean=${CLEAN_LOGIN_RC:-} findings=${FINDING_LOGIN_RC:-} after-reboot=${LOGIN_RC}"
if [[ "${CLEAN_LOGIN_RC:-1}" == "0" && "${FINDING_LOGIN_RC:-1}" == "0" && "${LOGIN_RC}" == "0" ]]; then
    check pass login-shell-exits-clean "${login_statuses}"
else
    check fail login-shell-exits-clean "${login_statuses}"
fi

# ── evidence (never a check) ──────────────────────────────────────────────────────────
evidence boot_id "$(cat /proc/sys/kernel/random/boot_id 2>&1)"
evidence kernel "${running_kernel}"
evidence collecting_kernel "${PREPARED_RUNNING_KERNEL:-}"
evidence installed_kernels "$(rpm -q kernel-core --queryformat '%{VERSION}-%{RELEASE}.%{ARCH} ' 2>&1)"
evidence default_kernel "${PREPARED_DEFAULT_KERNEL:-}"
evidence timer_after_fixture "${PREPARED_TIMER_AFTER:-}"
evidence report_after_reboot "${after_login}"
evidence report_before_reboot "${finding_login}"
evidence repo_commit "${VMTEST_COMMIT:-}"

printf 'VMTEST-CHECKS-DONE total=%d passed=%d failed=%d skipped=%d\n' "${total}" "${passed}" "${failed}" "${skipped}"
if [[ "${total}" -ne "${PLANNED}" ]]; then
    echo "ERROR: ${total} checks ran but ${PLANNED} were declared; this script is inconsistent" >&2
    exit 70
fi
exit 0

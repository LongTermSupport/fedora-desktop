#!/usr/bin/env bash
# Unit-test select_second_kernel (the server host-health kernel-change fixture, Plan 00109
# Task 3.2) — the step that gives a guest a kernel it is not running.
#
# WHY THIS EXISTS. This is the only step of the route nothing could reach. The container
# QA runs in is Debian: no dnf, no rpm, no grubby, so not one line of it had ever been
# executed by anything. The only other executor is a guest that has already spent twenty
# minutes provisioning to get here, and a defect found there costs that twenty minutes to
# see and another twenty to confirm a fix. Every check in the scenario stands on this
# step: if the guest reboots into the kernel it already ran, "the document is stale
# against the running kernel" is false and the fourteen checks after it judge nothing.
#
# What it can and cannot settle. Driving the real function against stubbed dnf, rpm and
# grubby proves the SELECTION — which version is chosen from what the repos offer, what is
# installed, what is handed to the bootloader, and that every way of having no second
# kernel is a refusal rather than a quiet success. It cannot prove dnf's real output
# format, and does not pretend to: the stubs emit the NEVRA shape `%{version}-%{release}
# .%{arch}` is documented to produce, and the first real run is what confirms that.
#
# The function is extracted with awk rather than sourced — the fixture is a script, and
# sourcing it would run the whole guest preparation. A refactor that moves the function
# keeps working, one that renames it fails loudly at extraction.
#
# THE ACCEPTANCE CASES COME FIRST. A refusal is only evidence if something proves the
# function accepts the good case, or "it refused" and "it can never do anything" look
# identical from here — which is how a checker that could not pass at all survived
# eleven tests earlier in this plan.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FIXTURE="$REPO_ROOT/files/home/.local/share/vmtest/guest-prepare-server-host-health-kernel-change.bash"

if [ ! -f "$FIXTURE" ]; then
    echo "FAIL: fixture not found at $FIXTURE" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

FN="$work/fn.bash"
awk -v fn="select_second_kernel() {" '$0 == fn {p=1} p {print} p && /^\}/ {exit}' "$FIXTURE" >"$FN"
if ! grep -q '^select_second_kernel() {' "$FN"; then
    echo "FAIL: could not extract select_second_kernel from the fixture" >&2
    exit 1
fi

# Realistic Fedora 44 kernel NEVRAs. `sort -Vr` orders these -100 above -63 numerically,
# so "newest" below means what it would mean on a real guest.
K_NEW=6.17.4-100.fc44.x86_64
K_OLD=6.17.0-63.fc44.x86_64
K_OLDEST=6.16.9-200.fc44.x86_64

passed=0
failed=0

report() {
    # report <status> <name> [detail]
    local status="$1" name="$2" detail="${3:-}"
    if [ "$status" = pass ]; then
        passed=$((passed + 1))
        printf 'ok   %s\n' "$name"
    else
        failed=$((failed + 1))
        printf 'FAIL %s%s\n' "$name" "${detail:+ — $detail}" >&2
    fi
}

# The collaborators. `die` EXITS, as it does in the fixture, so what happened travels back
# through files rather than variables. `grubby` is STATEFUL: --default-kernel reports back
# what --set-default was given, so the read-back check is exercised against the selection
# the function actually made rather than against a constant the test supplies. A case that
# wants them to disagree sets STUB_GRUBBY_DEFAULT.
STUB_PRELUDE=$(
    cat <<'STUB'
die() { printf 'DIE %s' "$*" >"$TRACE_DIR/die"; exit 1; }
log() { :; }
record() { printf '%s=%s\n' "${1:?}" "${2-}" >>"$TRACE_DIR/record"; }
sudo() { if [ "${1:-}" = "-n" ]; then shift; fi; "$@"; }
dnf() {
    local a
    for a in "$@"; do
        case "$a" in
            repoquery)
                if [ "${STUB_REPOQUERY_RC:-0}" != 0 ]; then
                    printf '%s\n' "${STUB_REPOQUERY_ERR:-repo metadata unreachable}" >&2
                    return "${STUB_REPOQUERY_RC}"
                fi
                printf '%s' "${STUB_REPOQUERY-}"
                return 0
                ;;
            install)
                printf 'dnf-install %s\n' "${*: -1}" >>"$TRACE_DIR/trace"
                return "${STUB_INSTALL_RC:-0}"
                ;;
        esac
    done
    return 0
}
# rpm prints its complaint on STDOUT and exits 1 — that is why the fixture captures it
# with 2>&1 and judges the status before reading it as versions.
rpm() { printf '%s' "${STUB_RPM-}"; return "${STUB_RPM_RC:-0}"; }
grubby() {
    case "${1:-}" in
        --set-default)
            printf 'grubby-set %s\n' "${2:-}" >>"$TRACE_DIR/trace"
            printf '%s' "${2:-}" >"$TRACE_DIR/default"
            ;;
        --default-kernel)
            if [ -n "${STUB_GRUBBY_DEFAULT-}" ]; then
                printf '%s\n' "$STUB_GRUBBY_DEFAULT"
            elif [ -r "$TRACE_DIR/default" ]; then
                printf '%s\n' "$(cat "$TRACE_DIR/default")"
            fi
            ;;
    esac
}
STUB
)

# run_case <running-kernel> <boot-kernel…> — drive the function with the stub environment
# and recover its stdout, its refusal and what it called. Each named kernel gets a file in
# the case's own stand-in for /boot; naming none is a guest whose bootloader has nothing.
STDOUT=""
TRACE=""
RECORD=""
DIE_MESSAGE=""
run_case() {
    local running="$1"
    shift
    rm -rf "${work:?}/trace" "${work:?}/boot" "${work:?}/evidence"
    mkdir -p "$work/trace" "$work/boot" "$work/evidence" "$work/nothing"
    # The externals the function reaches for, and deliberately not grubby. Resolved from
    # the real PATH now, while it is still intact.
    local tool tool_path
    for tool in sort cat; do
        if [ ! -e "$work/nothing/$tool" ]; then
            tool_path="$(command -v "$tool")" ||
                { echo "FAIL: no $tool on PATH; the no-grubby case cannot be built" >&2; exit 1; }
            ln -s "$tool_path" "$work/nothing/$tool"
        fi
    done
    local k
    for k in "$@"; do
        printf 'vmlinuz\n' >"$work/boot/vmlinuz-$k"
    done
    (
        export TRACE_DIR="$work/trace"
        if [ "${STUB_NO_GRUBBY:-0}" = 1 ]; then
            # A guest with no grubby, and HERMETICALLY so. Renaming the stub is not
            # enough: this gate runs on the user's own Fedora machine, where grubby is a
            # real binary on a real PATH — `command -v` would find it, and the stubbed
            # sudo would then hand the host's actual bootloader tool a --set-default.
            #
            # A PATH holding everything the function needs EXCEPT grubby, rather than an
            # empty one. Emptying it also takes away `sort`, and the case then dies of a
            # missing coreutil while still reporting the refusal it was looking for —
            # true for the wrong reason, and worse, a build with the grubby check removed
            # would never reach the install this case checks did not happen.
            eval "${STUB_PRELUDE/grubby() \{/absent_grubby() \{}"
            export PATH="$work/nothing"
        else
            eval "$STUB_PRELUDE"
        fi
        # Exported because the extracted function reads it from the shell it is sourced
        # into, exactly as it does in the fixture.
        export EVIDENCE_DIR="$work/evidence"
        # shellcheck source=/dev/null
        source "$FN"
        select_second_kernel "$running" "$work/boot"
    ) >"$work/out" 2>"$work/err"
    local rc=$?
    STDOUT=""
    [ -r "$work/out" ] && STDOUT="$(cat "$work/out")"
    TRACE=""
    [ -r "$work/trace/trace" ] && TRACE="$(cat "$work/trace/trace")"
    RECORD=""
    [ -r "$work/trace/record" ] && RECORD="$(cat "$work/trace/record")"
    DIE_MESSAGE=""
    [ -r "$work/trace/die" ] && DIE_MESSAGE="$(cat "$work/trace/die")"
    return "$rc"
}

# ── 0. POSITIVE CONTROL: a guest on the release kernel is given a newer one ────────────
# Everything the step promises, in one case: the newest offered version that is not the
# running one is chosen, installed by NEVRA, handed to the bootloader, read back, and
# returned on stdout for the caller.
STUB_REPOQUERY="$K_NEW
$K_OLD" STUB_RPM="$K_NEW
$K_OLD" run_case "$K_OLD" "$K_NEW" "$K_OLD"
rc=$?
if [ "$rc" -ne 0 ]; then
    report fail a-newer-kernel-is-installed-and-selected "refused: ${DIE_MESSAGE:-none}"
elif [ "$STDOUT" != "$K_NEW" ]; then
    report fail a-newer-kernel-is-installed-and-selected "returned '${STDOUT}', expected $K_NEW"
elif [[ "$TRACE" != *"dnf-install kernel-$K_NEW"* ]]; then
    report fail a-newer-kernel-is-installed-and-selected "did not install kernel-$K_NEW: ${TRACE:-none}"
elif [[ "$TRACE" != *"grubby-set $work/boot/vmlinuz-$K_NEW"* ]]; then
    report fail a-newer-kernel-is-installed-and-selected "did not select it for boot: ${TRACE:-none}"
elif [[ "$RECORD" != *"PREPARED_WANTED_KERNEL=$K_NEW"* ]] ||
    [[ "$RECORD" != *"PREPARED_TARGET_KERNEL=$K_NEW"* ]] ||
    [[ "$RECORD" != *"PREPARED_DEFAULT_KERNEL=$work/boot/vmlinuz-$K_NEW"* ]]; then
    report fail a-newer-kernel-is-installed-and-selected "record incomplete: ${RECORD:-none}"
else
    report pass a-newer-kernel-is-installed-and-selected
fi

# ── 1. a guest already on the NEWEST kernel is given an OLDER one ─────────────────────
# The case the step is written for and nothing proved: "the newest available that is not
# this one" is supposed to answer both directions without a branch. A guest built from a
# current image runs the newest kernel there is, so this is the likelier of the two in the
# lab — and a selection that only ever looks forward would refuse every such guest.
STUB_REPOQUERY="$K_NEW
$K_OLD" STUB_RPM="$K_NEW
$K_OLD" run_case "$K_NEW" "$K_NEW" "$K_OLD"
rc=$?
if [ "$rc" -ne 0 ]; then
    report fail a-guest-on-the-newest-kernel-gets-an-older-one "refused: ${DIE_MESSAGE:-none}"
elif [ "$STDOUT" != "$K_OLD" ]; then
    report fail a-guest-on-the-newest-kernel-gets-an-older-one "returned '${STDOUT}', expected $K_OLD"
elif [[ "$TRACE" != *"dnf-install kernel-$K_OLD"* ]]; then
    report fail a-guest-on-the-newest-kernel-gets-an-older-one "did not install kernel-$K_OLD: ${TRACE:-none}"
else
    report pass a-guest-on-the-newest-kernel-gets-an-older-one
fi

# ── 1a. with several to choose from, it takes the NEWEST that is not running ──────────
# Every case above has exactly one candidate, so all of them pass whether the selection
# takes the newest or merely the first thing that differs. Three on offer with the running
# one in the middle is what tells those apart. It matters: a repo's oldest kernel-core can
# be several releases back, and a guest handed one may not boot the drivers its image
# expects — a reboot failure the lab would report as this plan's claim being false.
if STUB_REPOQUERY="$K_NEW
$K_OLD
$K_OLDEST" STUB_RPM="$K_NEW
$K_OLD
$K_OLDEST" run_case "$K_OLD" "$K_NEW" "$K_OLD" "$K_OLDEST"; then
    # Asserted on what was INSTALLED, not only on what came back. Those are two separate
    # decisions — the repo list picks what to download, the installed list picks what to
    # boot — and they can disagree. A selection that took the oldest on offer would still
    # return the newest here, because the second loop finds the newest kernel already on
    # disk; only the download names the choice that was actually made.
    if [[ "$TRACE" == *"dnf-install kernel-$K_OLDEST"* ]]; then
        report fail the-newest-not-the-merely-different-is-chosen "downloaded the OLDEST on offer ($K_OLDEST)"
    elif [[ "$TRACE" != *"dnf-install kernel-$K_NEW"* ]]; then
        report fail the-newest-not-the-merely-different-is-chosen "did not install $K_NEW: ${TRACE:-none}"
    elif [ "$STDOUT" != "$K_NEW" ]; then
        report fail the-newest-not-the-merely-different-is-chosen "returned '${STDOUT}', expected $K_NEW"
    else
        report pass the-newest-not-the-merely-different-is-chosen
    fi
else
    report fail the-newest-not-the-merely-different-is-chosen "refused: ${DIE_MESSAGE:-none}"
fi

# ── 2. the same version offered by several repos is still one kernel ──────────────────
# Enabled repos overlap, so a NEVRA arriving three times is ordinary. It must be chosen
# once and installed once, not once per repo.
STUB_REPOQUERY="$K_NEW
$K_NEW
$K_OLD
$K_NEW" STUB_RPM="$K_NEW
$K_OLD" run_case "$K_OLD" "$K_NEW" "$K_OLD"
rc=$?
installs=$(printf '%s\n' "$TRACE" | grep -c 'dnf-install') || installs=0
if [ "$rc" -eq 0 ] && [ "$STDOUT" = "$K_NEW" ] && [ "$installs" -eq 1 ]; then
    report pass a-duplicated-version-is-installed-once
else
    report fail a-duplicated-version-is-installed-once "rc=$rc stdout='$STDOUT' installs=$installs"
fi

# ── 3. every offered kernel IS the running one → refuse ───────────────────────────────
if STUB_REPOQUERY="$K_OLD" STUB_RPM="$K_OLD" run_case "$K_OLD" "$K_OLD"; then
    report fail only-the-running-kernel-on-offer-refuses "it succeeded, returning '$STDOUT'"
elif [[ "$DIE_MESSAGE" == *"cannot be given a second kernel"* ]]; then
    report pass only-the-running-kernel-on-offer-refuses
else
    report fail only-the-running-kernel-on-offer-refuses "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 4. the repos offer NOTHING, and say so successfully → refuse ──────────────────────
# The empty population, which is the defect shape this plan exists to catch. A repoquery
# that exits 0 having printed nothing is a misconfigured guest, not a guest with no newer
# kernel, and a loop over an empty list reaches its end without selecting anything. The
# refusal has to come from the emptiness being judged, never from the loop running out.
if STUB_REPOQUERY="" STUB_RPM="$K_OLD" run_case "$K_OLD" "$K_OLD"; then
    report fail an-empty-repoquery-refuses "it succeeded on an empty list, returning '$STDOUT'"
elif [[ "$DIE_MESSAGE" == *"cannot be given a second kernel"* ]]; then
    report pass an-empty-repoquery-refuses
else
    report fail an-empty-repoquery-refuses "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 5. repoquery FAILS → refuse, naming what dnf said ─────────────────────────────────
if STUB_REPOQUERY_RC=1 STUB_REPOQUERY_ERR="Errors during downloading metadata" \
    run_case "$K_OLD" "$K_OLD"; then
    report fail a-failed-repoquery-refuses "it succeeded after dnf failed"
elif [[ "$DIE_MESSAGE" == *"Errors during downloading metadata"* ]]; then
    report pass a-failed-repoquery-refuses
else
    report fail a-failed-repoquery-refuses "refused without quoting dnf: ${DIE_MESSAGE:-none}"
fi

# ── 6. rpm FAILS → refuse naming RPM, not the bootloader ──────────────────────────────
# The one that had no symptom. `rpm -q` prints `package kernel-core is not installed` on
# STDOUT and exits 1, and a process substitution's status is not part of the pipeline, so
# `pipefail` never sees it. Read straight, that sentence becomes a candidate version,
# gets rejected for naming no vmlinuz, and the run dies about /boot — pointing a reader
# at the bootloader for a fault that belongs to rpm.
if STUB_REPOQUERY="$K_NEW
$K_OLD" STUB_RPM="package kernel-core is not installed" STUB_RPM_RC=1 \
    run_case "$K_OLD" "$K_NEW" "$K_OLD"; then
    report fail a-failed-rpm-refuses-naming-rpm "it succeeded after rpm failed"
elif [[ "$DIE_MESSAGE" == *"asking rpm which kernel-core packages are installed failed"* ]]; then
    report pass a-failed-rpm-refuses-naming-rpm
elif [[ "$DIE_MESSAGE" == *vmlinuz* ]]; then
    report fail a-failed-rpm-refuses-naming-rpm "blamed the bootloader for an rpm failure: $DIE_MESSAGE"
else
    report fail a-failed-rpm-refuses-naming-rpm "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 7. dnf reported success but no second kernel reached /boot → refuse ───────────────
# A guest whose /boot is full, or a dnf that resolved the NEVRA to a no-op. Only the
# running kernel has a vmlinuz, so there is nothing to reboot into.
if STUB_REPOQUERY="$K_NEW
$K_OLD" STUB_RPM="$K_NEW
$K_OLD" run_case "$K_OLD" "$K_OLD"; then
    report fail an-uninstalled-kernel-refuses "it succeeded, returning '$STDOUT'"
elif [[ "$DIE_MESSAGE" == *"has a $work/boot/vmlinuz-*"* ]]; then
    report pass an-uninstalled-kernel-refuses
else
    report fail an-uninstalled-kernel-refuses "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 8. grubby accepted the selection but reports a different default → refuse ─────────
# Setting a boot entry is not the same as it being the one that boots. A grubby that took
# the command and kept its old default would send the guest back into the kernel it just
# ran, and every check after the reboot would be judging the wrong boot.
if STUB_REPOQUERY="$K_NEW
$K_OLD" STUB_RPM="$K_NEW
$K_OLD" STUB_GRUBBY_DEFAULT="/boot/vmlinuz-$K_OLDEST" \
    run_case "$K_OLD" "$K_NEW" "$K_OLD"; then
    report fail a-disagreeing-grubby-refuses "it succeeded, returning '$STDOUT'"
elif [[ "$DIE_MESSAGE" == *"grubby reports /boot/vmlinuz-$K_OLDEST as the default"* ]]; then
    report pass a-disagreeing-grubby-refuses
else
    report fail a-disagreeing-grubby-refuses "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 9. no grubby in the guest at all → refuse before anything is installed ────────────
# Checked first in the function, so a guest that could never have its boot entry set does
# not spend several minutes downloading a kernel to discover it. The stub is renamed
# rather than removed, so this case differs from the ones above by exactly one thing.
if STUB_NO_GRUBBY=1 STUB_REPOQUERY="$K_NEW
$K_OLD" STUB_RPM="$K_NEW
$K_OLD" run_case "$K_OLD" "$K_NEW" "$K_OLD"; then
    report fail a-guest-without-grubby-refuses "it succeeded with no way to set a boot entry"
elif [[ "$TRACE" == *dnf-install* ]]; then
    report fail a-guest-without-grubby-refuses "installed a kernel before noticing grubby was missing"
elif [[ "$DIE_MESSAGE" == *"no grubby in this guest"* ]]; then
    report pass a-guest-without-grubby-refuses
else
    report fail a-guest-without-grubby-refuses "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 10. a refusal reaches the CALLER through the capture ──────────────────────────────
# The fixture takes this function's answer with `target_kernel="$(select_second_kernel …)"`
# and `die` inside a command substitution exits only the subshell. If the assignment
# swallowed that, the fixture would carry on to its completion marker and the harness
# would reboot a guest into the kernel it already ran — the one failure that produces a
# green transcript for a scenario that never happened.
mkdir -p "$work/trace10" "$work/evidence"
# An UNQUOTED heredoc: the paths and the stubs have to be interpolated now, while the
# `$` of the capture and of the variable it assigns must survive into the generated
# script — hence the escaping on exactly those two.
cat >"$work/caller.bash" <<CALLER
set -euo pipefail
TRACE_DIR=$(printf '%q' "$work/trace10")
EVIDENCE_DIR=$(printf '%q' "$work/evidence")
STUB_REPOQUERY=$(printf '%q' "$K_OLD")
STUB_RPM=$(printf '%q' "$K_OLD")
$STUB_PRELUDE
source $(printf '%q' "$FN")
target_kernel="\$(select_second_kernel $(printf '%q' "$K_OLD") $(printf '%q' "$work/boot"))"
printf 'CALLER-CONTINUED %s\\n' "\$target_kernel"
CALLER
caller_out="$(bash "$work/caller.bash" 2>&1)"
caller_rc=$?
if [ "$caller_rc" -eq 0 ]; then
    report fail a-refusal-stops-the-caller "the caller exited 0 after the refusal"
elif [[ "$caller_out" == *CALLER-CONTINUED* ]]; then
    report fail a-refusal-stops-the-caller "the caller ran on past the refusal: $caller_out"
else
    report pass a-refusal-stops-the-caller
fi

# A run that selected nothing must not be able to report success.
if [ "$passed" -eq 0 ]; then
    printf 'FAIL: no case ran\n' >&2
    exit 1
fi

printf 'passed: %d\n' "$passed"
if [ "$failed" -gt 0 ]; then
    printf 'failed: %d\n' "$failed" >&2
    exit 1
fi
exit 0

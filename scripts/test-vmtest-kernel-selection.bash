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
    local a latest_limit="" newest
    for a in "$@"; do
        case "$a" in
            # dnf5 has no --showduplicates; it refuses the call, as a real guest did.
            --showduplicates)
                printf 'Unknown argument "%s" for command "repoquery".\n' "$a" >&2
                return 2
                ;;
            --latest-limit=*) latest_limit="${a#--latest-limit=}" ;;
        esac
    done
    for a in "$@"; do
        case "$a" in
            repoquery)
                if [ "${STUB_REPOQUERY_RC:-0}" != 0 ]; then
                    printf '%s\n' "${STUB_REPOQUERY_ERR:-repo metadata unreachable}" >&2
                    return "${STUB_REPOQUERY_RC}"
                fi
                # dnf5's behaviour, MODELLED: every available version, unless
                # --latest-limit=1 asks for the newest only. An earlier stub modelled dnf4
                # (newest only unless --showduplicates), so the fixture passed here and was
                # refused on a real guest. The case that matters most in the lab, a guest
                # already on the newest kernel, rests on more than one version coming back.
                if [ "$latest_limit" != 1 ]; then
                    printf '%s' "${STUB_REPOQUERY-}"
                else
                    newest=""
                    while read -r a; do
                        if [ -n "$a" ]; then newest="$a"; break; fi
                    done < <(printf '%s\n' "${STUB_REPOQUERY-}" | sort -Vr)
                    printf '%s\n' "$newest"
                fi
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
            if [ "${STUB_GRUBBY_SET_RC:-0}" != 0 ]; then
                printf 'grubby: cannot update the boot entry\n' >&2
                return "${STUB_GRUBBY_SET_RC}"
            fi
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
    # A command substitution, because that is how the fixture calls it. This is a small
    # point, deliberately stated small: an earlier version of this comment claimed the
    # previous `( … )` shape ran the function under errexit and that switching to this one
    # is what let a failing package transaction be caught. Not so — this suite runs
    # without `set -e`, so the subshell inherited errexit OFF exactly as a command
    # substitution does, and reverting to it with case 6a present still catches the
    # mutant. The transaction went untested because nobody wrote the case, not because
    # the harness could not express it.
    STDOUT="$(
        {
        export TRACE_DIR="$work/trace"
        # NOTHING REAL IS REACHABLE BY ANY CASE. PATH holds the two coreutils the function
        # needs and nothing else, for every case rather than only the no-grubby one.
        #
        # This gate runs on the user's own Fedora machine, where dnf and grubby are real
        # binaries and sudo may be passwordless. The stubs are the only thing standing
        # between this suite and `sudo -n dnf -y install kernel-<NEVRA>` against that
        # machine — and they are installed by an `eval`, which discards its status inside
        # a subshell with errexit off. A prelude left syntactically broken by a future
        # edit therefore defines nothing, the error goes to a file nothing reads, and the
        # names resolve to the real tools. Measured: eval exits 2 and execution continues.
        #
        # So hermeticity is enforced three ways, not assumed: the eval is judged, the
        # collaborators are checked to be functions, and PATH could not reach a real one
        # even if both of those were removed. Emptying PATH entirely is not the answer —
        # that also takes away `sort`, and a case then dies of a missing coreutil while
        # still reporting the refusal it was looking for.
        export PATH="$work/nothing"
        stub_expect_grubby=function
        if [ "${STUB_NO_GRUBBY:-0}" = 1 ]; then
            # Renaming the stub is what makes grubby absent; PATH above is what keeps the
            # host's real one out of reach of `command -v`.
            eval "${STUB_PRELUDE/grubby() \{/absent_grubby() \{}" ||
                { echo "HARNESS: the stub prelude did not parse" >&2; exit 97; }
            stub_expect_grubby=absent
        else
            eval "$STUB_PRELUDE" ||
                { echo "HARNESS: the stub prelude did not parse" >&2; exit 97; }
        fi
        for stub_name in sudo dnf rpm; do
            [ "$(type -t "$stub_name")" = function ] ||
                { echo "HARNESS: the prelude did not define $stub_name" >&2; exit 97; }
        done
        if [ "$stub_expect_grubby" = absent ]; then
            if command -v grubby >/dev/null; then
                echo "HARNESS: grubby is reachable in the case that requires it absent" >&2
                exit 97
            fi
        elif [ "$(type -t grubby)" != function ]; then
            echo "HARNESS: the prelude did not define grubby" >&2
            exit 97
        fi
        # Exported because the extracted function reads it from the shell it is sourced
        # into, exactly as it does in the fixture.
        export EVIDENCE_DIR="$work/evidence"
        # shellcheck source=/dev/null
        source "$FN"
        if [ "${STUB_OMIT_BOOT_DIR:-0}" = 1 ]; then
            select_second_kernel "$running"
        else
            select_second_kernel "$running" "$work/boot"
        fi
        } 2>"$work/err"
    )"
    local rc=$?
    # 97 is the harness failing its own integrity check, not a refusal under test. It
    # stops everything: per-case verdicts from a run whose stubs were not installed would
    # be reporting on whatever the host happened to have.
    if [ "$rc" -eq 97 ]; then
        echo "FAIL: harness integrity check failed" >&2
        [ -r "$work/err" ] && cat "$work/err" >&2
        exit 1
    fi
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
#
# BOTH LISTS ARRIVE OLDEST-FIRST, which is what makes the two `sort -Vr` calls load
# bearing. Every list in this file was written newest-first to begin with, so deleting
# both sorts passed all twelve cases: the first line was already the answer and nothing
# ever had to be ordered. Neither dnf nor rpm promises an order, so neither should a stub.
if STUB_REPOQUERY="$K_OLDEST
$K_OLD
$K_NEW" STUB_RPM="$K_OLDEST
$K_OLD
$K_NEW" run_case "$K_OLD" "$K_NEW" "$K_OLD" "$K_OLDEST"; then
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

# ── 4. the repos offer NOTHING, and say so successfully → refuse, DIFFERENTLY ─────────
# The empty population, which is the defect shape this plan exists to catch. A repoquery
# that exits 0 having printed nothing is a misconfigured guest, not a guest with no newer
# kernel, and a loop over an empty list reaches its end without selecting anything. The
# refusal has to come from the emptiness being judged, never from the loop running out.
#
# And it must be its OWN refusal. Asserting case 3's message here would have been the
# cheap way to make this pass, and it would have cemented a misdiagnosis of exactly the
# class this commit fixed for rpm: "every kernel-core the repos offer is X" is FALSE when
# the repos offered nothing at all, and sends a reader looking for a kernel when the fault
# is the repository configuration.
if STUB_REPOQUERY="" STUB_RPM="$K_OLD" run_case "$K_OLD" "$K_OLD"; then
    report fail an-empty-repoquery-refuses "it succeeded on an empty list, returning '$STDOUT'"
elif [[ "$DIE_MESSAGE" == *"cannot be given a second kernel"* ]]; then
    report fail an-empty-repoquery-refuses "blamed the kernel choice for an unusable repository: $DIE_MESSAGE"
elif [[ "$DIE_MESSAGE" == *"listed no kernel-core at all"* ]]; then
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

# ── 6a. the package transaction FAILS → refuse, naming the transaction ────────────────
# The case nobody wrote. STUB_INSTALL_RC existed from the first draft of this file and no
# case ever set it, so the one command that actually changes the guest had its exit status
# unexamined by anything. That mattered because extracting this step into a function moved
# the install from script top level, where errexit was live, into a function the fixture
# calls in a command substitution — where bash turns errexit OFF. On a guest that already
# carries two kernels the run would then have SUCCEEDED, with PREPARED_WANTED_KERNEL
# naming a kernel nothing ever downloaded.
if STUB_REPOQUERY="$K_OLD
$K_NEW" STUB_RPM="$K_OLD
$K_NEW" STUB_INSTALL_RC=1 run_case "$K_OLD" "$K_NEW" "$K_OLD"; then
    report fail a-failed-install-refuses "it succeeded after the transaction failed, returning '$STDOUT'"
elif [[ "$DIE_MESSAGE" == *"installing kernel-$K_NEW failed"* ]]; then
    report pass a-failed-install-refuses
elif [[ "$DIE_MESSAGE" == *vmlinuz* ]]; then
    report fail a-failed-install-refuses "blamed the boot directory for a failed transaction: $DIE_MESSAGE"
else
    report fail a-failed-install-refuses "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 6b. the bootloader REFUSES the selection → refuse ─────────────────────────────────
# Same shape as 6a one step later, and the same reason it was invisible: `grubby
# --set-default` had its status discarded too. A guest whose boot entry was never changed
# reboots into the kernel it was already running, which is the single failure that makes
# every check after the reboot judge the wrong boot while still producing a transcript.
if STUB_REPOQUERY="$K_OLD
$K_NEW" STUB_RPM="$K_OLD
$K_NEW" STUB_GRUBBY_SET_RC=1 run_case "$K_OLD" "$K_NEW" "$K_OLD"; then
    report fail a-refusing-bootloader-refuses "it succeeded after grubby refused, returning '$STDOUT'"
elif [[ "$DIE_MESSAGE" == *"grubby refused to make"* ]]; then
    report pass a-refusing-bootloader-refuses
else
    report fail a-refusing-bootloader-refuses "refused for another reason: ${DIE_MESSAGE:-none}"
fi

# ── 6c. the boot directory DEFAULTS to /boot ──────────────────────────────────────────
# The parameter exists for the test's benefit, so nothing otherwise pins its default:
# `${2:-/bot}` would have passed every case in this file. Called with one argument and an
# installed version that cannot exist on any machine, so the refusal has to name the real
# path. Read-only — no case here ever writes into /boot.
if STUB_REPOQUERY="$K_OLD
$K_NEW" STUB_RPM="0.0.0-0.fcnone.noarch" STUB_OMIT_BOOT_DIR=1 \
    run_case "$K_OLD" "$K_NEW" "$K_OLD"; then
    report fail the-boot-directory-defaults-to-boot "it succeeded, returning '$STDOUT'"
# `has a ` is load-bearing, not decoration. Every other case runs with boot_dir set to
# `$work/boot` — a path that ENDS in /boot — so a bare `*/boot/vmlinuz-**` is satisfied by
# `/tmp/tmp.X/boot/vmlinuz-*` just as well as by the real thing. Delete the
# STUB_OMIT_BOOT_DIR branch and this case would stay green while testing nothing, and the
# mutation harness could not see it because it only ever mutates the fixture.
elif [[ "$DIE_MESSAGE" == *"has a /boot/vmlinuz-*"* ]]; then
    report pass the-boot-directory-defaults-to-boot
else
    report fail the-boot-directory-defaults-to-boot "did not name /boot: ${DIE_MESSAGE:-none}"
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
rm -rf "${work:?}/trace10" "${work:?}/boot10"
mkdir -p "$work/trace10" "$work/boot10" "$work/evidence"
printf 'vmlinuz\n' >"$work/boot10/vmlinuz-$K_OLD"
# Its OWN boot directory. Reusing $work/boot made this case depend on whatever the
# previous one happened to leave there, so a reordering could have changed its meaning
# silently.
#
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
target_kernel="\$(select_second_kernel $(printf '%q' "$K_OLD") $(printf '%q' "$work/boot10"))"
printf 'CALLER-CONTINUED %s\\n' "\$target_kernel"
CALLER
caller_out="$(bash "$work/caller.bash" 2>&1)"
caller_rc=$?
# The refusal the function actually reached, not merely "it exited non-zero". Without
# this the case passes on a truncated heredoc, a renamed function or a `set -u` error —
# every way of failing to run at all looks like the propagation it means to prove.
caller_die=""
[ -r "$work/trace10/die" ] && caller_die="$(cat "$work/trace10/die")"
if [[ "$caller_die" != *"cannot be given a second kernel"* ]]; then
    report fail a-refusal-stops-the-caller "the function never reached its refusal: ${caller_die:-none}; output: ${caller_out:-none}"
elif [ "$caller_rc" -eq 0 ]; then
    report fail a-refusal-stops-the-caller "the caller exited 0 after the refusal"
elif [[ "$caller_out" == *CALLER-CONTINUED* ]]; then
    report fail a-refusal-stops-the-caller "the caller ran on past the refusal: $caller_out"
else
    report pass a-refusal-stops-the-caller
fi

# A run that selected nothing must not be able to report success — and neither may a run
# that quietly lost a case. `passed: N` is only evidence if something knows what N is;
# without this, deleting a case makes the suite greener rather than louder.
EXPECTED_CASES=15
if [ "$((passed + failed))" -ne "$EXPECTED_CASES" ]; then
    printf 'FAIL: %d case(s) reported, expected %d — a case was added or lost without updating EXPECTED_CASES\n' \
        "$((passed + failed))" "$EXPECTED_CASES" >&2
    exit 1
fi

printf 'passed: %d\n' "$passed"
if [ "$failed" -gt 0 ]; then
    printf 'failed: %d\n' "$failed" >&2
    exit 1
fi
exit 0

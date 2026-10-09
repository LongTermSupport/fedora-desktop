#!/usr/bin/env bash
# check-lid.bash — the checks behind acceptance.bash. Read-only. Prints one PASS / FAIL /
# NOT ESTABLISHABLE line per check, then `COVERAGE: n of m checks executed`.
#
# Exit 0 = ACCEPTED: every check executed and passed.
# Exit 1 = REJECTED: a check failed.
# Exit 2 = COULD NOT ESTABLISH: nothing failed, but the docked, lid-closed state was not
#          there to check, so the fix is not shown to work (meta-deploy.bash's third verdict).

set -euo pipefail

TOTAL=0
EXECUTED=0
FAILED=0

pass() { TOTAL=$((TOTAL + 1)); EXECUTED=$((EXECUTED + 1)); printf 'PASS  %s\n' "$1"; }
fail() { TOTAL=$((TOTAL + 1)); EXECUTED=$((EXECUTED + 1)); FAILED=$((FAILED + 1)); printf 'FAIL  %s\n' "$1"; }
unestablishable() { TOTAL=$((TOTAL + 1)); printf 'NOT ESTABLISHABLE  %s\n' "$1"; }

upower_property() {
    busctl get-property org.freedesktop.UPower /org/freedesktop/UPower org.freedesktop.UPower "$1"
}

# grep exits 1 on no match; that is a finding (no IgnoreLid line), not a crash.
if ! ignore_lid="$(grep -E '^IgnoreLid=' /etc/UPower/UPower.conf)"; then
    ignore_lid="(no IgnoreLid line)"
fi
if [[ "${ignore_lid}" == "IgnoreLid=false" ]]; then
    pass "UPower.conf sets IgnoreLid=false"
else
    fail "UPower.conf sets IgnoreLid=false (found: ${ignore_lid})"
fi

lid_present="$(upower_property LidIsPresent)"
if [[ "${lid_present}" == "b true" ]]; then
    pass "UPower reports the lid (LidIsPresent)"
else
    fail "UPower reports the lid (LidIsPresent=${lid_present})"
fi

# Recorded, not judged: closing the lid must not change it, and the owner compares it with
# the reading taken before closing (matrix row 1).
printf 'INFO  power profile: %s\n' "$(busctl get-property org.freedesktop.UPower.PowerProfiles \
    /org/freedesktop/UPower/PowerProfiles org.freedesktop.UPower.PowerProfiles ActiveProfile)"

# The symptom check needs the broken state's preconditions: lid shut, another monitor lit.
acpi_lid="(no lid state)"
for f in /proc/acpi/button/lid/*/state; do
    [[ -r "${f}" ]] && acpi_lid="$(cat "${f}")"
done
external_lit=0
builtin_connectors=()
for d in /sys/class/drm/card*-*; do
    [[ -r "${d}/enabled" ]] || continue
    case "$(basename "${d}")" in
        *-eDP-*|*-LVDS-*) builtin_connectors+=("${d}") ;;
        *) [[ "$(cat "${d}/enabled")" == "enabled" ]] && external_lit=1 ;;
    esac
done

if [[ "${acpi_lid}" != *closed* ]] || [[ "${external_lit}" != "1" ]]; then
    unestablishable "built-in panel is off with the lid closed while docked (needs: lid closed, an external monitor on)"
else
    if [[ "$(upower_property LidIsClosed)" == "b true" ]]; then
        pass "UPower reports the lid closed"
    else
        fail "UPower reports the lid closed"
    fi
    if [[ "${#builtin_connectors[@]}" -eq 0 ]]; then
        fail "a built-in panel connector exists to check"
    fi
    for d in "${builtin_connectors[@]}"; do
        if [[ "$(cat "${d}/enabled")" == "disabled" ]]; then
            pass "$(basename "${d}") is disabled with the lid closed"
        else
            fail "$(basename "${d}") is disabled with the lid closed"
        fi
    done
fi

printf 'COVERAGE: %d of %d checks executed\n' "${EXECUTED}" "${TOTAL}"
if [[ "${FAILED}" -ne 0 ]]; then
    printf 'REJECTED: %d check(s) failed\n' "${FAILED}" >&2
    exit 1
fi
if [[ "${EXECUTED}" -ne "${TOTAL}" ]]; then
    printf 'COULD NOT ESTABLISH: incomplete run — dock the laptop, close the lid and run it again\n' >&2
    exit 2
fi
printf 'ACCEPTED\n'

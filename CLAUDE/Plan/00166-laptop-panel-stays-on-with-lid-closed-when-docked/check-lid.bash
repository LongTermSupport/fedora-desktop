#!/usr/bin/env bash
# check-lid.bash — the checks behind acceptance.bash. Read-only. Prints one PASS / FAIL /
# NOT ESTABLISHABLE line per check, then `COVERAGE: n of m checks executed`, and exits
# non-zero unless every check executed AND passed: a run that could not see the docked,
# lid-closed state has not shown the fix works, however clean the rest looks.

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

ignore_lid="$(grep -E '^IgnoreLid=' /etc/UPower/UPower.conf)"
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

# The symptom check needs the broken state's preconditions: lid shut, another monitor lit.
acpi_lid="$(cat /proc/acpi/button/lid/*/state)"
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
    printf 'REJECTED: incomplete run — dock the laptop, close the lid and run it again\n' >&2
    exit 1
fi
printf 'ACCEPTED\n'

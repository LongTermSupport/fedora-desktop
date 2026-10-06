#!/usr/bin/env bash
# u01-plugin-probe-check.bash — read one U01 child-claude run's evidence and report which
# of the probe plugin's hooks fired, and the D13 install route that evidence supports
# (DESIGN.md section 6, "Install route, decided by probe U01").
#
# Evidence directory layout, written by u01-plugin-probe-run.bash:
#   marks/<Event>   touched by the hook for <Event>
#   child.rc        the child's exit code
#   child.stdout    the child's reply
#
# The optional control is a run of the SAME three hooks declared another way (user-level
# settings.json) under the same conditions. A child with no credential still dispatches
# SessionStart and UserPromptSubmit before the API call fails (Stop never comes), so an
# event that fires in the control and not in the plugin run is a proven miss even though
# no turn completed. An event that fired in neither run proves nothing.
#
# stdout (the payload), one line each:
#   hook <Event> fired|not-fired          for SessionStart, UserPromptSubmit, Stop
#   child rc=<n> turn=complete|incomplete
#   control hooks=<n>/3                   only when a control is given
#   plugin-loaded yes|no|unknown
#   route plugin|fallback|undetermined reason=<code>
# With --hooks-only, only the hook and child lines are printed (used for the controls).
#
# A turn is complete only when the child exited 0 AND replied.
#
# Exit: 0 route decided (or --hooks-only), 3 undetermined, 64 usage, 66 evidence missing.
#
# Usage: u01-plugin-probe-check.bash <evidence-dir> [<control-evidence-dir> | --hooks-only]
set -euo pipefail

readonly EVENTS=(SessionStart UserPromptSubmit Stop)

usage() {
    printf 'usage: u01-plugin-probe-check.bash <evidence-dir> [<control-evidence-dir> | --hooks-only]\n' >&2
    exit 64
}

# require_evidence <dir> — exit 66 unless <dir> holds marks/ and a numeric child.rc.
require_evidence() {
    local dir="$1"
    if [[ ! -d "${dir}/marks" ]] || [[ ! -f "${dir}/child.rc" ]]; then
        printf 'u01-plugin-probe-check: no evidence (marks/ and child.rc) in %s\n' "${dir}" >&2
        exit 66
    fi
    if [[ ! "$(<"${dir}/child.rc")" =~ ^[0-9]+$ ]]; then
        printf 'u01-plugin-probe-check: child.rc is not a number in %s\n' "${dir}" >&2
        exit 66
    fi
}

# count_fired <dir> — stdout: how many of EVENTS left a marker in <dir>/marks.
count_fired() {
    local dir="$1" event n=0
    for event in "${EVENTS[@]}"; do
        if [[ -e "${dir}/marks/${event}" ]]; then
            n=$((n + 1))
        fi
    done
    printf '%d\n' "${n}"
}

[[ "$#" -ge 1 ]] && [[ "$#" -le 2 ]] || usage
dir="$1"
control=""
hooksOnly=0
if [[ "$#" -eq 2 ]]; then
    if [[ "$2" == --hooks-only ]]; then
        hooksOnly=1
    else
        control="$2"
    fi
fi
require_evidence "${dir}"
if [[ -n "${control}" ]]; then
    require_evidence "${control}"
fi

for event in "${EVENTS[@]}"; do
    if [[ -e "${dir}/marks/${event}" ]]; then
        printf 'hook %s fired\n' "${event}"
    else
        printf 'hook %s not-fired\n' "${event}"
    fi
done
fired="$(count_fired "${dir}")"

childRc="$(<"${dir}/child.rc")"
turn=incomplete
if [[ "${childRc}" -eq 0 ]] && [[ -s "${dir}/child.stdout" ]]; then
    turn=complete
fi
printf 'child rc=%s turn=%s\n' "${childRc}" "${turn}"
if [[ "${hooksOnly}" -eq 1 ]]; then
    exit 0
fi

# A miss is proven by a completed turn (every hook should have run), or by an event that
# fired in the control but not here. An event neither run fired proves nothing.
missedVsControl=0
if [[ -n "${control}" ]]; then
    printf 'control hooks=%s/%d\n' "$(count_fired "${control}")" "${#EVENTS[@]}"
    for event in "${EVENTS[@]}"; do
        if [[ -e "${control}/marks/${event}" ]] && [[ ! -e "${dir}/marks/${event}" ]]; then
            missedVsControl=$((missedVsControl + 1))
        fi
    done
fi

hooksWouldRun=0
if [[ "${turn}" == complete ]]; then
    hooksWouldRun=1
    whileWhat=in-a-completed-turn
elif [[ "${missedVsControl}" -gt 0 ]]; then
    hooksWouldRun=1
    whileWhat=while-control-fired
fi

if [[ "${fired}" -gt 0 ]]; then
    loaded=yes
elif [[ "${hooksWouldRun}" -eq 1 ]]; then
    loaded=no
else
    loaded=unknown
fi
printf 'plugin-loaded %s\n' "${loaded}"

if [[ "${fired}" -eq "${#EVENTS[@]}" ]]; then
    printf 'route plugin reason=all-hooks-fired\n'
    exit 0
fi
if [[ "${hooksWouldRun}" -eq 1 ]]; then
    if [[ "${fired}" -eq 0 ]]; then
        printf 'route fallback reason=no-hook-fired-%s\n' "${whileWhat}"
    else
        printf 'route fallback reason=some-hooks-did-not-fire-%s\n' "${whileWhat}"
    fi
    exit 0
fi
printf 'route undetermined reason=child-turn-did-not-complete\n'
exit 3

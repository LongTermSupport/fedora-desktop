#!/usr/bin/env bash
# Plan 00112 — probe-gnome-extensions.bash
#
# The probe body for triage.bash (CLAUDE/PlanTriage.md: the probes live IN the script, not
# in a transcript). Read-only. Appends its findings to the report file named as $1.
#
# It answers the three questions Task 2.1 needs before and after the deploy:
#
#   1. which extension UUIDs are deployed under the user's extensions directory,
#   2. what `org.gnome.shell enabled-extensions` holds RIGHT NOW — the list the deploy must
#      add to and never subtract from,
#   3. what live State GNOME reports for each one.
#
# It also lists the SYSTEM extensions under /usr/share/gnome-shell/extensions and whether
# each is enabled. That is not this plan's scope, but dash-to-dock is installed there by DNF
# and the play configures its dconf keys two tasks later, so whether it is enabled decides
# if the same defect exists in a second place. Evidence, not an assertion.
#
# Usage: probe-gnome-extensions.bash <report-file>
set -euo pipefail

readonly report="${1:?usage: probe-gnome-extensions.bash <report-file>}"
readonly userExtensions="${HOME}/.local/share/gnome-shell/extensions"
readonly systemExtensions="/usr/share/gnome-shell/extensions"

# stdout is the report; progress goes to stderr (CLAUDE/StderrHygiene.md).
printf '==> probing GNOME extension state\n' >&2

# A session bus is needed for gsettings and gnome-extensions to say anything true. Its
# absence is a RESULT this probe records, not an error — a headless triage run is valid.
sessionBus="absent"
if [[ -n "${DBUS_SESSION_BUS_ADDRESS:-}" ]]; then
    sessionBus="from DBUS_SESSION_BUS_ADDRESS"
else
    busSocket="/run/user/$(id -u)/bus"
    if [[ -S "${busSocket}" ]]; then
        DBUS_SESSION_BUS_ADDRESS="unix:path=${busSocket}"
        export DBUS_SESSION_BUS_ADDRESS
        sessionBus="${busSocket}"
    fi
fi

# probe <label> <cmd...> — run a read-only command and print its output, or the exit status
# and message that explain why there is none. An unanswerable question is recorded as
# unanswered rather than aborting the triage.
probe() {
    local label="$1"
    shift
    local output status=0
    output="$("$@" 2>&1)" || status=$?
    if [[ "${status}" -eq 0 ]]; then
        printf -- '- %s: %s\n' "${label}" "${output}"
    else
        printf -- '- %s: UNREADABLE (exit %s): %s\n' "${label}" "${status}" "${output}"
    fi
}

# extension_state <uuid> — the live State GNOME reports, or the reason there is none.
extension_state() {
    local uuid="$1" info line
    if ! info="$(gnome-extensions info "${uuid}" 2>&1)"; then
        printf '(no session / gnome-extensions unavailable)'
        return 0
    fi
    if line="$(printf '%s\n' "${info}" | grep -E '^[[:space:]]*State:')"; then
        line="${line#*:}"
        printf '%s' "${line# }"
        return 0
    fi
    printf '(no State line reported)'
}

# declared_uuid <metadata-path> — the uuid metadata.json claims, or why it could not be read.
declared_uuid() {
    local metadata="$1" value
    if [[ ! -f "${metadata}" ]]; then
        printf '(no metadata.json)'
        return 0
    fi
    if value="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("uuid", "(no uuid key)"))' "${metadata}" 2>&1)"; then
        printf '%s' "${value}"
        return 0
    fi
    printf '(unreadable metadata.json)'
}

{
    printf '\n## 1. Session\n\n'
    printf -- '- D-Bus session bus: %s\n' "${sessionBus}"
    printf -- '- XDG_SESSION_TYPE: %s\n' "${XDG_SESSION_TYPE:-unset}"
    probe 'GNOME Shell version' gnome-shell --version

    printf '\n## 2. The declared list (what the deploy must add to, never subtract from)\n\n'
    probe 'org.gnome.shell enabled-extensions' \
        gsettings get org.gnome.shell enabled-extensions
    probe 'org.gnome.shell disable-user-extensions' \
        gsettings get org.gnome.shell disable-user-extensions

    printf '\n## 3. Deployed user extensions and their live State\n\n'
    if [[ ! -d "${userExtensions}" ]]; then
        printf -- '- %s does not exist — nothing is deployed for this user.\n' "${userExtensions}"
    else
        deployedCount=0
        while IFS= read -r uuidDir; do
            [[ -n "${uuidDir}" ]] || continue
            deployedCount=$((deployedCount + 1))
            printf -- '- %s — metadata uuid %s — State: %s\n' \
                "${uuidDir}" \
                "$(declared_uuid "${userExtensions}/${uuidDir}/metadata.json")" \
                "$(extension_state "${uuidDir}")"
        done < <(find "${userExtensions}" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
        printf -- '- deployed user extensions: %s\n' "${deployedCount}"
    fi

    printf '\n## 4. System extensions (evidence only — out of this plan scope)\n\n'
    printf 'dash-to-dock is installed here by DNF and the play configures its dconf keys.\n'
    printf 'If it is not enabled, the same defect exists in a second place.\n\n'
    if [[ ! -d "${systemExtensions}" ]]; then
        printf -- '- %s does not exist.\n' "${systemExtensions}"
    else
        while IFS= read -r uuidDir; do
            [[ -n "${uuidDir}" ]] || continue
            printf -- '- %s — State: %s\n' "${uuidDir}" "$(extension_state "${uuidDir}")"
        done < <(find "${systemExtensions}" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
    fi

    printf '\n## 5. What the play would declare\n\n'
    printf 'The discovery half of the applier, run read-only — this is the set the deploy\n'
    printf 'would add to the list above, changing nothing else:\n\n'
    printf '```\n'
    probe 'discovered' python3 -c '
import sys

sys.path.insert(0, sys.argv[1])
from helpers.gnome import enabled_extensions as ee

print(", ".join(ee.discover_deployed_uuids(sys.argv[2])) or "(none)")
' "${PLAN_REPO_ROOT:?probe-gnome-extensions.bash must be run from triage.bash}" "${userExtensions}"
    printf '```\n'
} >>"${report}"

printf '==> probe complete: %s\n' "${report}" >&2

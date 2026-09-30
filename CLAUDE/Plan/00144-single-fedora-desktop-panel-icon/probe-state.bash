#!/usr/bin/env bash
# probe-state.bash <report-file> <facet> — one read-only fact probe for Plan 00144's
# triage.bash. Facets: enabled | deployed | report | timer. Appends a markdown section to
# the report and exits non-zero only when the PROBE could not run, never on what it found.
set -euo pipefail

report="${1:?usage: probe-state.bash <report-file> <enabled|deployed|report|timer>}"
facet="${2:?usage: probe-state.bash <report-file> <enabled|deployed|report|timer>}"

panelUuid="fedora-desktop@fedora-desktop"
oldUuid="container-watch@fedora-desktop"
extensionsDir="${HOME}/.local/share/gnome-shell/extensions"

out() { printf '%s\n' "$*" >>"${report}"; }

case "${facet}" in
    enabled)
        raw="$(gsettings get org.gnome.shell enabled-extensions)"
        out "## enabled-extensions membership"
        out ""
        for uuid in "${panelUuid}" "${oldUuid}"; do
            case "${raw}" in
                *"'${uuid}'"*) out "- ${uuid}: present" ;;
                *) out "- ${uuid}: absent" ;;
            esac
        done
        out ""
        ;;
    deployed)
        out "## deployed extension directories"
        out ""
        for uuid in "${panelUuid}" "${oldUuid}"; do
            if [[ -d "${extensionsDir}/${uuid}" ]]; then
                files="$(find "${extensionsDir}/${uuid}" -type f -printf '%P\n' | sort | tr '\n' ' ')"
                out "- ${uuid}: deployed (${files})"
            else
                out "- ${uuid}: not deployed"
            fi
        done
        out ""
        ;;
    report)
        runtimeDir="${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR is unset; run from a graphical login session}"
        path="${runtimeDir}/container-watch/report.json"
        out "## container-watch report.json"
        out ""
        if [[ -f "${path}" ]]; then
            out "- report.json: present (mtime $(stat -c '%y' "${path}"), $(stat -c '%s' "${path}") bytes)"
        else
            out "- report.json: absent (no scan since this boot, or the backend is not installed)"
        fi
        if [[ -x "${HOME}/.local/bin/container-watch" ]]; then
            out "- backend CLI: installed"
        else
            out "- backend CLI: not installed"
        fi
        out ""
        ;;
    timer)
        out "## container-watch timer"
        out ""
        for query in is-enabled is-active; do
            if state="$(systemctl --user "${query}" container-watch.timer 2>&1)"; then
                out "- ${query}: ${state}"
            else
                out "- ${query}: ${state} (exit non-zero: a result, not a probe failure)"
            fi
        done
        out ""
        ;;
    *)
        printf '[FATAL] unknown facet: %s\n' "${facet}" >&2
        exit 64
        ;;
esac

#!/usr/bin/env bash
# Plan 00198 — triage-probe.bash: the probes behind triage.bash, one fact group per call.
#
# Usage: triage-probe.bash <probe> <report-file>
#   install    how GNOME Text Editor is installed: the RPM, a Flatpak, or both
#   help       its command-line options (is there a new-window or standalone flag)
#   desktop    every org.gnome.TextEditor.desktop on the XDG data path: Exec, DBusActivatable
#   mime       the default handler for text/plain and text/markdown
#   settings   the editor's own GSettings and the workspace settings of mutter
#
# Read-only: it reads files, asks rpm, flatpak, gio and gsettings, and runs the editor only
# with --help-all, which prints and exits without opening a window. Each fact goes to stdout
# and is appended to the report. A probe that cannot establish its fact exits non-zero, so
# triage.bash names the leg.
set -euo pipefail

probe="${1:?usage: triage-probe.bash <probe> <report-file>}"
report="${2:?usage: triage-probe.bash <probe> <report-file>}"

readonly APP_ID="org.gnome.TextEditor"
readonly BINARY="gnome-text-editor"
readonly FENCE='```'

# fact <text> — one line of the report, on stdout too.
fact() {
    printf '%s\n' "$1" | tee -a "${report}"
}

# block <title> <text> — a fenced block in the report, on stdout too.
block() {
    printf '\n%s:\n\n%s\n%s\n%s\n' "$1" "${FENCE}" "$2" "${FENCE}" | tee -a "${report}"
}

probe_install() {
    local rpm flatpak
    if rpm="$(rpm -q "${BINARY}" 2>&1)"; then
        fact "- rpm: ${rpm}"
    else
        fact "- rpm: not installed (${rpm})"
    fi
    if ! command -v flatpak >/dev/null; then
        fact "- flatpak: the flatpak command is absent"
        return 0
    fi
    if flatpak="$(flatpak info "${APP_ID}" 2>&1)"; then
        block "flatpak info ${APP_ID}" "${flatpak}"
    else
        fact "- flatpak: ${APP_ID} not installed (${flatpak})"
    fi
}

probe_help() {
    local out info
    if command -v "${BINARY}" >/dev/null; then
        out="$("${BINARY}" --help-all 2>&1)"
        block "${BINARY} --help-all" "${out}"
        return 0
    fi
    if command -v flatpak >/dev/null && info="$(flatpak info "${APP_ID}" 2>&1)"; then
        out="$(flatpak run --command="${BINARY}" "${APP_ID}" --help-all 2>&1)"
        block "flatpak run ${APP_ID} --help-all" "${out}"
        return 0
    fi
    printf '[ERROR] neither %s nor the %s Flatpak is installed: %s\n' "${BINARY}" "${APP_ID}" "${info:-no flatpak command}" >&2
    return 1
}

probe_desktop() {
    local dirs dir file found=0
    local -a dataDirs
    dirs="${XDG_DATA_HOME:-${HOME}/.local/share}:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
    IFS=':' read -r -a dataDirs <<<"${dirs}"
    for dir in "${dataDirs[@]}"; do
        file="${dir}/applications/${APP_ID}.desktop"
        if [[ -f "${file}" ]]; then
            found=1
            block "${file} (group, Exec, DBusActivatable, MimeType lines)" \
                "$(grep -E '^(\[|Exec=|DBusActivatable=|MimeType=)' "${file}")"
        fi
    done
    if [[ "${found}" -eq 0 ]]; then
        printf '[ERROR] no %s.desktop on the XDG data path: %s\n' "${APP_ID}" "${dirs}" >&2
        return 1
    fi
}

probe_mime() {
    local type
    for type in text/plain text/markdown; do
        block "gio mime ${type}" "$(gio mime "${type}" 2>&1)"
    done
}

probe_settings() {
    local schema schemas
    schemas="$(gsettings list-schemas)"
    for schema in "${APP_ID}" org.gnome.mutter; do
        if grep -qx "${schema}" <<<"${schemas}"; then
            block "gsettings list-recursively ${schema}" "$(gsettings list-recursively "${schema}")"
        else
            fact "- gsettings: schema ${schema} is not installed on the host"
        fi
    done
    fact "- org.gnome.desktop.wm.preferences focus-new-windows: $(gsettings get org.gnome.desktop.wm.preferences focus-new-windows)"
}

case "${probe}" in
    install) probe_install ;;
    help) probe_help ;;
    desktop) probe_desktop ;;
    mime) probe_mime ;;
    settings) probe_settings ;;
    *)
        printf '[FATAL] unknown probe: %s\n' "${probe}" >&2
        exit 64
        ;;
esac

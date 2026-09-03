#!/usr/bin/env bash
# probe-dock.bash — gather FACTS about the terminal, Dash to Dock and the focused window.
#
# Fact-finding only: appends to the report file given as $1 and renders no verdict
# (PlanScriptStandards R9). READ-ONLY: reads dconf, rpm/flatpak metadata, desktop files
# and the GNOME Shell window list over the session bus. Writes nothing to the desktop.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-dock.bash /tmp/report.md
#
# READ THIS FOR:
#   "Focused window as GNOME Shell sees it" — the decisive section. It shows the app-id and
#   wm-class Shell attached to the window you ran this from. Compare them with the
#   "Ptyxis desktop files" section: Dash to Dock's default intellihide mode only dodges
#   windows Shell can tie to the focused application's desktop file.
#
# EXIT CODES:
#   0  every probe reached a definite answer ("absent" and "denied" ARE answers)
#   1  a probe could not be answered — the fact-finding is incomplete
#  64  usage error
set -euo pipefail

# ── R1 bootstrap ──────────────────────────────────────────────────────────────────────────
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

REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
    printf 'usage: probe-dock.bash <report-file>\n' >&2
    exit 64
fi

plan_require_host "it reads the host's dconf, packages and the GNOME Shell session bus"

readonly DOCK_SCHEMA="org.gnome.shell.extensions.dash-to-dock"
INCOMPLETE=0

out() { printf '%s\n' "$*" >>"${REPORT}"; }

# Markdown code fence, built from octal escapes: a literal backtick anywhere in this file
# makes semgrep's bash parser reject the whole file, and the QA gate then runs no rule on it.
FENCE="$(printf '\140\140\140')"
readonly FENCE

# A non-zero exit is DATA, not a failure: record the rc and the output, carry on.
record() {
    local label="$1" rc="$2" result="$3"
    out ""
    out "### ${label}  (rc=${rc})"
    out ""
    out "${FENCE}"
    out "${result:-(no output)}"
    out "${FENCE}"
    return 0
}

probe() {
    local label="$1"
    shift
    local result rc
    if result="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    record "${label}" "${rc}" "${result}"
}

# Local helper functions are called DIRECTLY (not through probe's "$@"), so shellcheck can
# see them reached; their rc and output are recorded the same way.
RESULT=""
RC=0

# Every tool below is part of a stock Fedora Workstation; a missing one means the probe
# is being run somewhere it cannot answer, so the leg fails rather than skipping.
for tool in rpm gsettings gnome-extensions gdbus journalctl; do
    if ! command -v "${tool}" >/dev/null; then
        out "**${tool} is not on PATH** — this probe cannot answer from here."
        printf '[INCOMPLETE] %s is not on PATH\n' "${tool}" >&2
        INCOMPLETE=1
    fi
done
if [[ "${INCOMPLETE}" -ne 0 ]]; then
    exit 1
fi

out ""
out "## Session"
probe "GNOME Shell version" gnome-shell --version
record "session type / desktop" 0 "XDG_SESSION_TYPE=${XDG_SESSION_TYPE:-unset}
XDG_CURRENT_DESKTOP=${XDG_CURRENT_DESKTOP:-unset}"

out ""
out "## Which terminal is this"
out ""
out "Settles the premise that the failing terminal is Ptyxis and not a second gnome-terminal."
probe "terminal packages (rpm)" rpm -q ptyxis gnome-terminal
list_terminal_flatpaks() {
    if ! command -v flatpak >/dev/null; then
        echo "flatpak not installed"
        return 0
    fi
    local apps
    apps="$(flatpak list --app --columns=application,origin)"
    if ! grep -i -E 'ptyxis|terminal' <<<"${apps}"; then
        echo "(no terminal flatpaks)"
    fi
}
if RESULT="$(list_terminal_flatpaks 2>&1)"; then
    RC=0
else
    RC=$?
fi
record "terminal flatpaks" "${RC}" "${RESULT}"
probe "process owning this terminal" ps -o pid=,comm=,args= -p "${PPID}"

out ""
out "## Ptyxis desktop files"
out ""
out "The window tracker matches a Wayland app-id against a desktop file of the same name."
show_ptyxis_desktop_files() {
    local f found=0
    for f in /usr/share/applications/*Ptyxis*.desktop /usr/share/applications/*ptyxis*.desktop \
        "${HOME}"/.local/share/applications/*Ptyxis*.desktop "${HOME}"/.local/share/applications/*ptyxis*.desktop \
        /var/lib/flatpak/exports/share/applications/*Ptyxis*.desktop; do
        [[ -e "${f}" ]] || continue
        found=1
        echo "== ${f}"
        if ! grep -E '^(Exec|TryExec|StartupWMClass|DBusActivatable|X-Flatpak)=' "${f}"; then
            echo "(none of Exec/StartupWMClass/DBusActivatable present)"
        fi
    done
    if [[ "${found}" -ne 1 ]]; then
        echo "(no Ptyxis desktop file found in system, user or flatpak dirs)"
    fi
}
if RESULT="$(show_ptyxis_desktop_files 2>&1)"; then
    RC=0
else
    RC=$?
fi
record "Ptyxis desktop files and their identity keys" "${RC}" "${RESULT}"

out ""
out "## Dash to Dock"
probe "dash-to-dock package" rpm -q gnome-shell-extension-dash-to-dock
# The UUID is discovered rather than hardcoded: it has the shape of an email address, which
# the pre-commit secret scanner rejects.
show_extension_state() {
    local uuid
    if ! uuid="$(gnome-extensions list | grep -i 'dash-to-dock')"; then
        echo "(no dash-to-dock extension known to gnome-extensions)"
        return 0
    fi
    gnome-extensions info "${uuid}"
}
if RESULT="$(show_extension_state 2>&1)"; then
    RC=0
else
    RC=$?
fi
record "extension state" "${RC}" "${RESULT}"
probe "intellihide-mode allowed values" gsettings range "${DOCK_SCHEMA}" intellihide-mode
show_hide_settings() {
    gsettings list-recursively "${DOCK_SCHEMA}" | grep -E ' (intellihide|intellihide-mode|autohide|dock-fixed|autohide-in-fullscreen|require-pressure-to-show|multi-monitor|isolate-workspaces|isolate-monitors) '
}
out ""
out "Defaults are intellihide=true, intellihide-mode='FOCUS_APPLICATION_WINDOWS', dock-fixed=false."
if RESULT="$(show_hide_settings 2>&1)"; then
    RC=0
else
    RC=$?
fi
record "hide-related settings (effective values)" "${RC}" "${RESULT}"
probe "keys the user has changed from default (dconf dump)" dconf dump /org/gnome/shell/extensions/dash-to-dock/

out ""
out "## Focused window as GNOME Shell sees it"
out ""
out "READ THIS FOR: the entry with has-focus=true. Its app-id / wm-class must match a Ptyxis"
out "desktop file above for the default intellihide mode to consider this window at all."
out "An AccessDenied reply is itself a fact: the Introspect API is gated by the"
out "org.gnome.shell 'introspect' key, reported next."
probe "org.gnome.shell introspect key" gsettings get org.gnome.shell introspect
probe "GetWindows over the session bus" gdbus call --session --dest org.gnome.Shell \
    --object-path /org/gnome/Shell/Introspect --method org.gnome.Shell.Introspect.GetWindows

out ""
out "## Shell log lines mentioning the dock this boot"
probe "journal (user, this boot, dock-related)" journalctl --user -b --no-pager -n 40 -g 'dash-to-dock|dashtodock|intellihide'

exit 0

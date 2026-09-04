#!/usr/bin/env bash
# probe-slack.bash — gather FACTS about the Slack Flatpak and the file that fails to drop.
#
# Fact-finding only: appends to the report file given as $1 and renders no verdict
# (PlanScriptStandards R9). READ-ONLY: reads flatpak metadata and overrides, runs stat /
# wc / grep INSIDE the Slack sandbox against the file given as $2, lists processes, and
# greps Slack's local log directory. Writes nothing outside the report.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-slack.bash /tmp/report.md ~/Documents/some-file.md
#
# READ THIS FOR:
#   "The file as the Slack sandbox sees it" — the decisive section for H1. If stat inside
#   the sandbox fails while the host stat above it succeeds, the drop hands Slack a path
#   it cannot open. Then "MIME typing" for H2: host and sandbox must both say
#   text/markdown. Then "How Slack is running" for H4 (look for ozone-platform).
#   H3 (admin restriction) is a manual observation the plan lists; no probe can see it.
#
# EXIT CODES:
#   0  every probe reached a definite answer ("absent", "denied" and "not found" ARE answers)
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
DROP_FILE="${2:-}"
if [[ -z "${REPORT}" || -z "${DROP_FILE}" ]]; then
    printf 'usage: probe-slack.bash <report-file> <file-that-fails-to-drop>\n' >&2
    exit 64
fi
if [[ ! -f "${DROP_FILE}" ]]; then
    printf '[FATAL] %s is not a regular file on the host\n' "${DROP_FILE}" >&2
    exit 64
fi

plan_require_host "it inspects the host flatpak installation and runs commands inside the Slack sandbox"

readonly APP_ID="com.slack.Slack"
readonly APP_HOME="${HOME}/.var/app/${APP_ID}"
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

# Sandbox probes spawn a NEW instance of the Slack sandbox for one read-only command; the
# running Slack is untouched. That instance has exactly the filesystem grants Slack itself
# has, which is the whole point of asking from in there.
# Local helper functions are called DIRECTLY (not through probe's "$@"), so shellcheck can
# see them reached; their rc and output are recorded the same way.
RESULT=""
RC=0

# Every tool below is part of a stock Fedora Workstation. flatpak itself is what
# play-comms.yml installs Slack with; a missing one means this probe is being run
# somewhere it cannot answer, so the leg fails rather than skipping.
for tool in flatpak rpm stat file xdg-mime xdg-user-dir pgrep systemctl; do
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
record "session type / desktop" 0 "XDG_SESSION_TYPE=${XDG_SESSION_TYPE:-unset}
XDG_CURRENT_DESKTOP=${XDG_CURRENT_DESKTOP:-unset}
WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-unset}
DISPLAY=${DISPLAY:-unset}"
probe "GNOME Shell version" gnome-shell --version
probe "Nautilus package" rpm -q nautilus

out ""
out "## Slack installation"
out ""
out "Settles the premise that Slack is the Flathub Flatpak from play-comms.yml and nothing else."
probe "flatpak info" flatpak info "${APP_ID}"
probe "slack flatpaks (all installations)" flatpak list --app --columns=application,version,origin,installation
probe "competing rpm install" rpm -q slack

out ""
out "## Sandbox permissions"
out ""
out "The manifest's finish-args, then any system and user overrides. Look at filesystems="
out "and sockets= : the file's directory must be covered by a filesystems grant for the"
out "sandbox to open it by path (H1); the wayland/x11 sockets decide H4."
probe "manifest permissions" flatpak info --show-permissions "${APP_ID}"
probe "system overrides" flatpak override --show "${APP_ID}"
probe "user overrides" flatpak override --user --show "${APP_ID}"

out ""
out "## The file on the host"
probe "stat on the host" stat -c '%A %U:%G %s bytes %n' "${DROP_FILE}"
show_xdg_dirs() {
    local d
    for d in DOWNLOAD DOCUMENTS DESKTOP; do
        printf '%s=%s\n' "${d}" "$(xdg-user-dir "${d}")"
    done
    printf 'HOME=%s\n' "${HOME}"
}
if RESULT="$(show_xdg_dirs 2>&1)"; then RC=0; else RC=$?; fi
record "XDG user dirs (compare with the file path and the filesystems= grants)" "${RC}" "${RESULT}"

out ""
out "## The file as the Slack sandbox sees it"
out ""
out "READ THIS FOR: H1. stat succeeding here with the same size as on the host means the"
out "sandbox can open the path. A 'No such file or directory' here, with a good host stat"
out "above, means the drop hands Slack a path the sandbox cannot see."
probe "stat inside the sandbox" flatpak run --command=stat "${APP_ID}" -c '%A %U:%G %s bytes %n' "${DROP_FILE}"
probe "byte count inside the sandbox" flatpak run --command=wc "${APP_ID}" -c "${DROP_FILE}"
probe "document portal mount inside the sandbox" flatpak run --command=ls "${APP_ID}" -la /run/user/"$(id -u)"/doc/

out ""
out "## MIME typing"
out ""
out "READ THIS FOR: H2. All four should agree on text/markdown. An empty or"
out "application/octet-stream answer from inside the sandbox is the fact H2 predicts."
probe "host: xdg-mime" xdg-mime query filetype "${DROP_FILE}"
probe "host: file --mime-type" file --mime-type -b "${DROP_FILE}"
probe "host: shared-mime-info package" rpm -q shared-mime-info
probe "host: .md glob in /usr/share/mime/globs2" grep -E ':\*\.md$' /usr/share/mime/globs2
probe "sandbox: .md glob in the runtime's /usr/share/mime/globs2" flatpak run --command=grep "${APP_ID}" -E ':\*\.md$' /usr/share/mime/globs2
probe "sandbox: xdg-mime" flatpak run --command=xdg-mime "${APP_ID}" query filetype "${DROP_FILE}"
probe "sandbox: file --mime-type" flatpak run --command=file "${APP_ID}" --mime-type -b "${DROP_FILE}"

out ""
out "## How Slack is running"
out ""
out "READ THIS FOR: H4. The main process line shows whether Slack was launched with"
out "--ozone-platform=x11 / wayland, and any electron flags file the sandbox home carries."
probe "slack processes (command lines)" pgrep -af -u "$(id -u)" 'com.slack.Slack|/app/slack|slack --'
probe "electron flags files in the sandbox home" find "${APP_HOME}" -maxdepth 2 -name '*flags*.conf' -print
show_flags_files() {
    local f found=0
    for f in "${APP_HOME}"/config/electron-flags.conf "${APP_HOME}"/config/slack-flags.conf "${APP_HOME}"/config/electron25-flags.conf; do
        [[ -e "${f}" ]] || continue
        found=1
        echo "== ${f}"
        cat "${f}"
    done
    if [[ "${found}" -ne 1 ]]; then
        echo "(no electron/slack flags file in ${APP_HOME}/config)"
    fi
}
if RESULT="$(show_flags_files 2>&1)"; then RC=0; else RC=$?; fi
record "contents of any flags file" "${RC}" "${RESULT}"

out ""
out "## Portals"
out ""
out "A Flatpak app with no filesystem grant for a path can still receive it through the"
out "document portal, but only if the portal services are present and running."
probe "portal packages" rpm -q xdg-desktop-portal xdg-desktop-portal-gnome xdg-desktop-portal-gtk
probe "portal units" systemctl --user --no-pager --no-legend list-units 'xdg-desktop-portal*' 'xdg-document-portal*'

out ""
out "## Slack's own log, upload-related lines"
out ""
out "Lines mentioning unsupported / mime / file type / drop from the newest log files."
out "Slack API tokens are redacted by substitution before anything is written, and the"
out "result is checked; a redaction failure aborts this section rather than write it."
grep_slack_logs() {
    local logdir="${APP_HOME}/config/Slack/logs"
    if [[ ! -d "${logdir}" ]]; then
        echo "(no log directory at ${logdir})"
        return 0
    fi
    local hits
    if ! hits="$(grep -rhiE 'unsupported|mime|file ?type|filetype|drop' "${logdir}" \
        | awk '{ gsub(/xox[a-z]-[A-Za-z0-9-]+/, "<redacted>"); print }' \
        | awk 'NR <= 80')"; then
        echo "(no matching lines under ${logdir})"
        return 0
    fi
    if grep -qE 'xox[a-z]-[A-Za-z0-9]' <<<"${hits}"; then
        echo "ERROR: redaction check FAILED — a token-shaped string survived; section withheld." >&2
        return 1
    fi
    printf '%s\n' "${hits}"
}
probe "log directory listing" ls -la "${APP_HOME}/config/Slack/logs"
if RESULT="$(grep_slack_logs 2>&1)"; then RC=0; else RC=$?; fi
record "matching log lines (tokens redacted, first 80)" "${RC}" "${RESULT}"
if [[ "${RC}" -ne 0 ]]; then
    printf '[INCOMPLETE] slack log section failed its redaction check\n' >&2
    exit 1
fi

exit 0

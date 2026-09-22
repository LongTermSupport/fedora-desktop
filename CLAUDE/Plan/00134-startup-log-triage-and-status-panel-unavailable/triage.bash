#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about what the current boot's logs report, and
# about the state behind each finding in RESEARCH-startup-log-findings.md. Fact-finding
# only: renders no verdict (R9) and changes nothing. Read-only and safe to re-run.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2) — a
# container has no journal, no abrt spool and no user session to report on.
#
# The report is UNSCRUBBED host state (unit names, container names, paths). It is
# written under untracked/plan-runs/ by plan_start_log and must never be committed.
#
# Probes map to the research doc:
#   F1  status panel state + the host-health findings document it renders
#   F2  play-ledger sentinel, and the ad-hoc reproduction against a THROWAWAY state dir
#   F3  abrt backlog: total, unreported, newest
#   F4  WirePlumber Lua files present + the "NOT supported" journal line
#   F5  duplicate dnf repo ids
#   F6  units logging "RuntimeMaxSec= has no effect"
#   F7  SELinux AVC denials by source context and target, this boot and last 2 days
#   F8  autostart exec bit, firewalld/docker noise, orphan Thunar
#
# Usage: ./CLAUDE/Plan/00134-startup-log-triage-and-status-panel-unavailable/triage.bash [-h|--help]
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

usage() {
    cat <<'EOF'
Usage: triage.bash [--help]

Read-only fact-gathering for Plan 00134 (startup log triage). Run on the HOST.
Writes a full report under untracked/plan-runs/ and names the path on the way out.

Options:
  --help    Show this help and exit (creates nothing).
EOF
}

# --help must work before any environment resolution (PlanTriage.md).
for arg in "$@"; do
    case "$arg" in
        --help | -h)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $arg (see --help)" >&2
            exit 1
            ;;
    esac
done

plan_mode gather
plan_require_host "it reads the host journal, abrt spool, dnf repos and user session state"

plan_start_log auto

# Non-zero exit status is data, not failure (PlanTriage.md probe pattern).
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

ledgerDir="${XDG_STATE_HOME:-${HOME}/.local/state}/fedora-desktop/play-ledger"

boot_error_lines_by_unit() {
    journalctl --no-pager -b -p 4 -o short-iso | awk '{print $3}' | sort | uniq -c | sort -rn
}

extension_states() {
    local ext
    for ext in $(gnome-extensions list --enabled); do
        printf '%s  ' "$ext"
        gnome-extensions info "$ext" | grep -E '^ *State:'
    done
}

ledger_sentinel() {
    ls -la "$ledgerDir"
    echo "--- BROKEN:"
    cat "$ledgerDir/BROKEN"
}

# F2 reproduction: an ad-hoc play against a THROWAWAY XDG_STATE_HOME. The real
# ledger is never touched; the temp dir is removed before the probe returns.
adhoc_marks_throwaway_ledger() {
    local tmp
    tmp="$(mktemp -d)"
    (cd "${repoRoot}" && XDG_STATE_HOME="$tmp" ansible localhost -m ping < /dev/null)
    echo "--- throwaway ledger after the ad-hoc run:"
    ls -la "$tmp/fedora-desktop/play-ledger"
    cat "$tmp/fedora-desktop/play-ledger/BROKEN"
    rm -rf "$tmp"
}

abrt_summary() {
    echo "total:      $(abrt-cli list | grep -c '^Id')"
    echo "unreported: $(abrt-cli list --not-reported | grep -c '^Id')"
    echo "--- newest 5 (executable + time only):"
    abrt-cli list | grep -E '^(Executable|Component|Time)' | tail -n 10
}

wireplumber_lua_files() {
    ls -R "${HOME}/.config/wireplumber"
    echo "--- journal:"
    journalctl --no-pager --user -b | grep -E 'Lua configuration files are NOT supported'
}

duplicate_repo_ids() {
    grep -H '^\[' /etc/yum.repos.d/*.repo | awk -F: '{print $2}' | sort | uniq -d
    echo "--- dnf5daemon:"
    journalctl --no-pager -b | grep -E 'Id is present more than once'
}

oneshot_runtime_max() {
    journalctl --no-pager -b | grep -E 'RuntimeMaxSec= has no effect'
    echo "--- repo units declaring RuntimeMaxSec:"
    grep -rlE '^RuntimeMaxSec=' "${repoRoot}/files"
}

avc_by_context_this_boot() {
    journalctl --no-pager -b | grep 'avc:  denied' | grep -oE 'scontext=[^ ]+' | cut -d: -f1-3 | sort | uniq -c
}

avc_targets_two_days() {
    journalctl --no-pager --since '-2 days' | grep 'avc:  denied' \
        | grep -oE 'name="[^"]+"|path="[^"]+"' | sort | uniq -c | sort -rn | awk 'NR<=20'
}

checkout_labels() {
    ls -dZ "${repoRoot}" "${repoRoot}/.claude" "${repoRoot}/.claude/hooks-daemon/untracked"
}

firewalld_docker_noise() {
    journalctl --no-pager -b | grep -cE 'firewalld.*COMMAND_FAILED'
    journalctl --no-pager -b | grep -E 'NAME_CONFLICT'
}

echo "================================================================"
echo "Plan 00134 triage — startup log facts"
echo "================================================================"

echo "### READ THIS FOR: did anything actually crash this boot?"
probe "coredumps this boot" coredumpctl list --no-pager --since "$(uptime -s)"
probe "failed system units" systemctl --failed --no-pager --no-legend
probe "failed user units" systemctl --user --failed --no-pager --no-legend
probe "gnome-shell JS ERROR count" bash -c 'journalctl --no-pager --user -b | grep -c "JS ERROR"'
probe "enabled extensions and their state" extension_states
probe "warning+ lines this boot, by unit" boot_error_lines_by_unit

echo "### READ THIS FOR: F1 — what the status panel is rendering"
probe "host-health findings document" cat "$ledgerDir/host-health-findings.md"

echo "### READ THIS FOR: F2 — the ledger sentinel, and whether an ad-hoc run recreates it"
echo "###   A BROKEN file in the throwaway dir = the callback still marks ad-hoc plays."
probe "play-ledger state dir" ledger_sentinel
probe "ad-hoc ansible against a throwaway state dir" adhoc_marks_throwaway_ledger

echo "### READ THIS FOR: F3 — the ABRT backlog the applet re-announces at login"
probe "abrt summary" abrt_summary
probe "abrt-applet assertions this boot" bash -c 'journalctl --no-pager --user -b | grep -c "g_app_info_should_show"'

echo "### READ THIS FOR: F4 — WirePlumber Lua files that 0.5 ignores"
probe "wireplumber user config + journal line" wireplumber_lua_files

echo "### READ THIS FOR: F5 — repo ids defined in more than one .repo file"
probe "duplicate repo ids" duplicate_repo_ids

echo "### READ THIS FOR: F6 — oneshot units with an ignored RuntimeMaxSec"
probe "RuntimeMaxSec on Type=oneshot" oneshot_runtime_max

echo "### READ THIS FOR: F7 — SELinux denials: who, against what"
probe "AVC by source context, this boot" avc_by_context_this_boot
probe "AVC top targets, last 2 days" avc_targets_two_days
probe "labels on this checkout" checkout_labels

echo "### READ THIS FOR: F8 — small repo-owned noise"
probe "autostart entries with exec bit" find "${HOME}/.config/autostart" -maxdepth 1 -name '*.desktop' -perm -u+x
probe "firewalld docker noise (count, conflicts)" firewalld_docker_noise
probe "Thunar installed / required by" bash -c 'rpm -q Thunar; rpm -q --whatrequires Thunar'

echo "================================================================"
echo "END OF REPORT — read F2 first: if the throwaway ledger holds a BROKEN"
echo "file, Task 1.1 is still open and the panel will go unavailable again"
echo "on the next ad-hoc ansible command."
echo "================================================================"

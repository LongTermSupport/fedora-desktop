#!/usr/bin/env bash
# Plan 00078 triage — grounded facts for the podfreeze tool design.
#
# READ-ONLY: this script only queries podman/dnf/system state. It never
# starts, stops, pauses, builds, or removes anything. Safe to re-run at any
# time, on a live system, with CCY sessions running.
#
# Run this on the HOST (not inside a CCY container). It writes its full
# report to this plan's logs/ directory (gitignored), so the agent can read
# it from inside the container at the same repo-relative path.
#
# Probes map to PLAN.md hypotheses:
#   H1  rootless pause support (cgroups v2 + podman version)
#   H2  CCY containers match --filter label=claude-yolo-version (inherited image label)
#   H3  --filter network= works on ps/pause in the installed podman
#   H4  podman-tui packaging status
set -euo pipefail

usage() {
    cat <<'EOF'
Usage: triage.bash [--help]

Read-only fact-gathering for Plan 00078 (podman container control).
Run on the HOST. Writes a full report to <plan>/logs/podfreeze-triage.log.

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

# This is HOST triage: inside a container there is no host podman to probe,
# and an empty report would read as evidence of absence (PlanTriage.md).
if [ -e /run/.containerenv ] || [ -e /.dockerenv ]; then
    echo "ERROR: running inside a container — this triage probes the HOST podman." >&2
    echo "  Run it on the host: CLAUDE/Plan/00078-podman-container-control/triage.bash" >&2
    exit 1
fi

if ! command -v podman > /dev/null; then
    echo "ERROR: podman is not installed on this host." >&2
    echo "  Podman is declared in playbooks/imports/play-podman.yml. Deploy it with:" >&2
    echo "    ansible-playbook playbooks/imports/play-podman.yml" >&2
    echo "  Do NOT install it by hand (CLAUDE.md: Missing Dependencies — Fail Fast, Fix in IaC)." >&2
    exit 1
fi

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/podfreeze-triage.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

# Non-zero exit status is data, not failure (PlanTriage.md probe pattern).
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

show_ps_by_each_network() {
    # H3: does --filter network= return sane results for every defined network?
    local net
    while IFS= read -r net; do
        echo "== network: $net"
        podman ps -a --filter "network=$net" --format '{{.Names}}\t{{.Status}}'
    done < <(podman network ls --format '{{.Name}}')
}

pause_filter_support() {
    # H1/H3: pause must advertise --filter; we only read help text, never pause.
    podman pause --help
}

echo "================================================================"
echo "Plan 00078 triage — podman container control facts"
echo "Host: (hostname withheld from log by design — this repo is public,"
echo "       but logs/ is gitignored; still, no need to embed it)"
echo "================================================================"
echo

echo "### READ THIS FOR: H1 (rootless pause viability)"
echo "###   rootless=true + cgroupVersion=v2 + cgroupManager=systemd = pause works rootless"
probe "podman version" podman --version
probe "rootless / cgroups / manager" podman info --format 'rootless={{.Host.Security.Rootless}} cgroupVersion={{.Host.CgroupsVersion}} cgroupManager={{.Host.CgroupManager}} runtime={{.Host.OCIRuntime.Name}}'

echo "### READ THIS FOR: H1+H3 (pause supports --filter, incl. network=)"
echo "###   look for '--filter' in the option list below"
probe "podman pause --help" pause_filter_support

echo "### READ THIS FOR: current container inventory (names, status, networks, labels)"
probe "podman ps -a (names/status/networks)" podman ps -a --format '{{.Names}}\t{{.Status}}\t{{.Networks}}'
probe "podman ps -a (labels)" podman ps -a --format '{{.Names}}\t{{.Labels}}'

echo "### READ THIS FOR: H2 (CCY containers match the inherited image label)"
echo "###   every running <project>_yolo* container should appear below;"
echo "###   if this list is empty while CCY sessions run, H2 is REFUTED and"
echo "###   the tool must rely on the name pattern until Phase 1 labels land"
probe "ps --filter label=claude-yolo-version" podman ps --filter label=claude-yolo-version --format '{{.Names}}\t{{.Labels}}'

echo "### READ THIS FOR: H3 (network filter behaves per network)"
probe "network list" podman network ls
probe "ps --filter network=<each>" show_ps_by_each_network

echo "### READ THIS FOR: H4 (podman-tui packaging) + picker dependency"
probe "dnf info podman-tui (read-only query)" dnf info podman-tui
probe "fzf present" command -v fzf
probe "podman.socket (user) status" systemctl --user status podman.socket --no-pager -l

echo "================================================================"
echo "END OF REPORT — read the H1 and H2 sections first; they gate the plan"
echo "(Phase 0 decision gate in PLAN.md)."
echo "================================================================"

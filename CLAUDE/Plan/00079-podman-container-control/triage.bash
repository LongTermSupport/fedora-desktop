#!/usr/bin/env bash
# Plan 00079 triage — grounded facts for the podfreeze tool design.
#
# READ-ONLY: this script only queries podman/dnf/system state. It never
# starts, stops, pauses, builds, or removes anything. Safe to re-run at any
# time, on a live system, with CCY sessions running.
#
# Run this on the HOST (not inside a CCY container). plan_start_log writes its
# full report under untracked/plan-runs/ (gitignored), and names the path on
# the way out, so the agent can read it from inside the container at the same
# repo-relative path.
#
# Probes map to PLAN.md hypotheses:
#   H1  rootless pause support (cgroups v2 + podman version)
#   H2  CCY containers match --filter label=claude-yolo-version (inherited image
#       label) — CONFIRMED but it OVER-matches; superseded by H6. See F16/D6.
#   H3  --filter network= works on ps/pause in the installed podman
#   H4  podman-tui packaging status
#   H6  the CCY 3.40.0 run-time labels (ccy, ccy-project, ccy-github, ccy-token,
#       ccy-ssh-keys) are present on a RELAUNCHED session
#   H7  'podman' is ONE shared bridge, not a per-container default, so every
#       CCY session launched without --network shares it
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

Read-only fact-gathering for Plan 00079 (podman container control).
Run on the HOST. Writes a full report under untracked/plan-runs/, and names
the exact path on the way out.

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

# This is HOST triage: inside a container there is no host podman to probe,
# and an empty report would read as evidence of absence (PlanTriage.md).
plan_require_host "it probes the HOST podman, its networks and the CCY session labels"

if ! command -v podman > /dev/null; then
    echo "ERROR: podman is not installed on this host." >&2
    echo "  Podman is declared in playbooks/imports/play-podman.yml. Deploy it with:" >&2
    echo "    ansible-playbook playbooks/imports/play-podman.yml" >&2
    echo "  Do NOT install it by hand (CLAUDE.md: Missing Dependencies — Fail Fast, Fix in IaC)." >&2
    exit 1
fi

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

show_ps_by_each_network() {
    # H3: does --filter network= return sane results for every defined network?
    local net
    while IFS= read -r net; do
        echo "== network: $net"
        podman ps -a --filter "network=$net" --format '{{.Names}}\t{{.Status}}'
    done < <(podman network ls --format '{{.Name}}')
}

deployed_ccy_version() {
    # Read from the deployed file, never by running it: the launcher's `--version` is
    # reached only after its git-repo check on the cwd and a migration prompt, so run
    # from anywhere but a main checkout's root it exits 1 without printing a version.
    local launcher=/var/local/claude-yolo/claude-yolo version
    if [ ! -r "$launcher" ]; then
        echo "no readable launcher at $launcher" >&2
        return 1
    fi
    version="$(awk -F'"' '/^CCY_VERSION="[0-9.]+"/ { print $2; exit }' "$launcher")"
    if [ -z "$version" ]; then
        echo "no CCY_VERSION=\"x.y.z\" line in $launcher" >&2
        return 1
    fi
    printf '%s\n' "$version"
}

pause_filter_support() {
    # H1/H3: pause must advertise --filter; we only read help text, never pause.
    podman pause --help
}

echo "================================================================"
echo "Plan 00079 triage — podman container control facts"
echo "Host: (hostname withheld from log by design — this repo is public,"
echo "       and although untracked/ is gitignored, no need to embed it)"
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

echo "### READ THIS FOR: H2 — SETTLED, and settled the OTHER way (F16, D6)"
echo "###   The inherited image label matches, but it OVER-matches: it marks"
echo "###   anything BUILT FROM the CCY image, session or not. Anything below"
echo "###   that is not a <project>_yolo[_N] session is an instance of that."
echo "###   podfreeze no longer reads this label at all — kept as a probe only"
echo "###   so the over-match stays visible on this machine."
probe "ps --filter label=claude-yolo-version" podman ps --filter label=claude-yolo-version --format '{{.Names}}\t{{.Labels}}'

echo "### READ THIS FOR: H6 — did the CCY 3.40.0 run-time labels land?"
echo "###   THIS SECTION NAMES ACCOUNTS AND TOKEN LABELS. The log is"
echo "###   gitignored; do not paste it into an issue, PR, or gist."
echo "###"
echo "###   A session must be RELAUNCHED after deploying the new launcher to"
echo "###   carry these — a session started by the old ccy has none, which is"
echo "###   not a failure. An EMPTY list means no session has been relaunched"
echo "###   yet; it does not mean the labels are broken."
echo "###   Every row should show ccy=true plus project/github/token/ssh-keys,"
echo "###   with 'none' where an axis does not apply — never an empty value."
probe "ps --filter label=ccy=true (identity labels)" \
    podman ps --all --filter label=ccy=true \
    --format '{{.Names}}\tproject={{index .Labels "ccy-project"}}\tgithub={{index .Labels "ccy-github"}}\ttoken={{index .Labels "ccy-token"}}\tkeys={{index .Labels "ccy-ssh-keys"}}'
probe "deployed ccy version" deployed_ccy_version

echo "### READ THIS FOR: H3 (network filter behaves per network)"
probe "network list" podman network ls
probe "ps --filter network=<each>" show_ps_by_each_network

echo "### READ THIS FOR: H7 — is 'podman' ONE network, or one per container?"
echo "###   'podman network ls' shows a single NETWORK ID for it, so it is one"
echo "###   shared bridge and every container launched without --network joins"
echo "###   it. The inspect below settles what that means in practice: ONE"
echo "###   subnet listed = one L2 domain; the per-container IPs that follow"
echo "###   should all fall inside it."
echo "###   This is podman's default, not something CCY chooses — but it does"
echo "###   mean sessions sharing it can address each other, so read the IPs"
echo "###   rather than assuming isolation."
probe "network inspect podman (subnet/gateway)" \
    podman network inspect podman --format '{{.Name}} driver={{.Driver}} subnets={{range .Subnets}}{{.Subnet}} gw={{.Gateway}} {{end}}'
probe "per-container IPs on the podman network" \
    podman ps --all --filter network=podman \
    --format '{{.Names}}\t{{.Networks}}\t{{.Status}}'

echo "### READ THIS FOR: H4 (podman-tui packaging) + picker dependency"
probe "dnf info podman-tui (read-only query)" dnf info podman-tui
probe "fzf present" command -v fzf
probe "podman.socket (user) status" systemctl --user status podman.socket --no-pager -l

echo "================================================================"
echo "END OF REPORT — the Phase 0 decision gate (H1) has passed, so the"
echo "section to read first is now H6: whether a relaunched CCY session"
echo "carries the 3.40.0 run-time labels podfreeze selects on."
echo "================================================================"

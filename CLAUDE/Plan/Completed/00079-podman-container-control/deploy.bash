#!/usr/bin/env bash
#
# Plan 00079 — deploy podfreeze. HOST ONLY.
#
# Runs acceptance.bash itself when it finishes, and exits with ITS status, so a
# zero exit means "deployed AND verified" rather than merely "ansible did not
# error". A deploy whose verification is a separate command the human has to
# remember is a deploy that routinely goes unverified.
#
# Usage: deploy.bash [--no-verify] [--help]

set -uo pipefail
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
# STANDARD-EXCEPTION(R1): this script runs without errexit (see the `set` line above), so
# every library call that RETURNS 1 rather than exiting is gated explicitly. Without the
# gate a failed plan_init would flow on into the deploy.
plan_init "${BASH_SOURCE[0]}" || exit 1

VERIFY=1
for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00079 — deploy podfreeze (HOST ONLY)

Usage: deploy.bash [--no-verify] [--help]

  --no-verify   deploy only; do not run acceptance.bash afterwards

Runs two plays, then hands over to acceptance.bash and exits with its verdict:

  play-claude-yolo.yml   deploys the CCY launcher, which from 3.40.0 labels
                         each session container (ccy, ccy-project, ccy-github,
                         ccy-token, ccy-ssh-keys)
  play-podfreeze.yml     installs fzf and deploys ~/.local/bin/podfreeze,
                         which selects on those labels

Both are idempotent, so re-running is safe, and neither restarts a service or
touches a running container. No image is rebuilt: only the host-side launcher
script changed, so REQUIRED_CONTAINER_VERSION is unmoved.

Already-running sessions were started by the OLD launcher and carry no labels
until they are relaunched. podfreeze still reaches them via --ccy (the
<project>_yolo[_N] name), but not via --github/--token/--ssh-key.
EOF
            exit 0
            ;;
        --no-verify)
            VERIFY=0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: deploy.bash --help" >&2
            exit 1
            ;;
    esac
done

plan_mode deploy || exit 1

# --- container guard: never run Ansible inside CCY (CLAUDE/ContainerRules.md) -
plan_require_host "it runs Ansible against this workstation, which must happen on the HOST" || exit 1

# Before the log opens: a sudo prompt issued after the tee redirect is flooded
# and garbled, and these plays `become`.
plan_prime_sudo || exit 1
plan_start_log auto || exit 1

# BOTH plays, in this order. podfreeze selects CCY sessions on labels the
# LAUNCHER sets, so deploying the tool without the launcher would ship a
# selector for labels no container carries — the exact shape of Plan 00099,
# where the repo held the fix and the host ran the old build because the
# deploy script ran only one of the two plays involved.
PLAYS=(
    "playbooks/imports/play-claude-yolo.yml"
    "playbooks/imports/optional/common/play-podfreeze.yml"
)

echo "=============================================================="
echo "Plan 00079 — deploy podfreeze"
echo "=============================================================="
echo

for play in "${PLAYS[@]}"; do
    if [ ! -f "$PLAN_REPO_ROOT/$play" ]; then
        echo "ERROR: playbook not found: $PLAN_REPO_ROOT/$play" >&2
        exit 1
    fi
done

# READ, never a literal: a version typed into this message is stale the next time
# CCY_VERSION moves, and it is shown to an operator about to deploy that launcher.
# `|| CCY_VER=""` is the explicit-fallback form — grep exits 1 when the line is
# absent, and under pipefail that would kill the script at the assignment.
CCY_VER=""
CCY_VER="$(grep -m1 -oE 'CCY_VERSION="[0-9.]+"' \
    "$PLAN_REPO_ROOT/files/var/local/claude-yolo/claude-yolo" |
    grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?')" || CCY_VER=""
if [ -z "$CCY_VER" ]; then
    CCY_VER="version unreadable"
fi

echo "Plays to run, in order:"
printf '  %s\n' "${PLAYS[@]}"
echo
echo "The first deploys the CCY launcher ($CCY_VER), which labels each session"
echo "container at launch. The second installs fzf and deploys the podfreeze"
echo "command into your ~/.local/bin, which selects on those labels."
echo
echo "Nothing else on this machine is touched — no service is restarted, no"
echo "image is rebuilt, and no container is started, stopped, frozen, or"
echo "thawed. Sessions already running keep the old launcher's behaviour"
echo "until they are relaunched, so they carry no labels yet."
echo

# Sequential and fail-fast: the second play deploys a tool that depends on what
# the first one installs, so running it after a failure would deploy a selector
# for labels nothing sets.
for play in "${PLAYS[@]}"; do
    echo
    echo "### $play"
    echo
    if ! ansible-playbook "$PLAN_REPO_ROOT/$play"; then
        echo >&2
        echo "ERROR: $play failed — see the output above." >&2
        echo "  Nothing after it was run." >&2
        exit 1
    fi
done

# The pre-rename `podman-freeze` binary is removed by play-podfreeze.yml, which
# ran above. It belongs there and not here: an `rm` in this script would be a
# manual system change, which the IaC HARD RULE prohibits outright, and it would
# only reach someone who ran the play through this wrapper.

echo
echo "=============================================================="
echo "Deploy finished."
echo "=============================================================="

if [ "$VERIFY" -eq 0 ]; then
    echo
    echo "Skipping verification (--no-verify). Run it yourself:"
    echo "  $PLAN_SCRIPT_DIR/acceptance.bash"
    exit 0
fi

if [ ! -x "$PLAN_SCRIPT_DIR/acceptance.bash" ]; then
    echo "ERROR: acceptance.bash is missing or not executable." >&2
    echo "  Deploy succeeded but nothing verified it." >&2
    exit 1
fi

echo
echo "### handing over to acceptance.bash"
echo
# It opens its own run log under untracked/plan-runs/; this child also inherits
# THIS run's log stream, so the deploy log carries the verdict too.
"$PLAN_SCRIPT_DIR/acceptance.bash"

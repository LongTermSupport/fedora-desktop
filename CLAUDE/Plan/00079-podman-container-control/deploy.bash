#!/usr/bin/env bash
#
# Plan 00079 — deploy podfreeze. HOST ONLY.
#
# Runs acceptance.bash itself when it finishes, and exits with ITS status, so a
# zero exit means "deployed AND verified" rather than merely "ansible did not
# error". A deploy whose verification is a separate command the human has to
# remember is a deploy that routinely goes unverified.
#
# Usage: deploy.bash [-y|--yes] [--no-verify] [--help]

set -uo pipefail

ASSUME_YES=0
VERIFY=1
for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00079 — deploy podfreeze (HOST ONLY)

Usage: deploy.bash [-y|--yes] [--no-verify] [--help]

  -y, --yes     do not ask for confirmation
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
        -y | --yes)
            ASSUME_YES=1
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

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

# --- container guard: never run Ansible inside CCY ---------------------------
if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "ERROR: this looks like a CCY container (/workspace)." >&2
    echo "  Ansible must run on the HOST, not in the container." >&2
    echo "  See CLAUDE/ContainerRules.md." >&2
    exit 1
fi

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/deploy.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

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
    if [ ! -f "$REPO_ROOT/$play" ]; then
        echo "ERROR: playbook not found: $REPO_ROOT/$play" >&2
        exit 1
    fi
done

echo "Plays to run, in order:"
printf '  %s\n' "${PLAYS[@]}"
echo
echo "The first deploys the CCY launcher (3.40.0), which labels each session"
echo "container at launch. The second installs fzf and deploys the podfreeze"
echo "command into your ~/.local/bin, which selects on those labels."
echo
echo "Nothing else on this machine is touched — no service is restarted, no"
echo "image is rebuilt, and no container is started, stopped, frozen, or"
echo "thawed. Sessions already running keep the old launcher's behaviour"
echo "until they are relaunched, so they carry no labels yet."
echo

if [ "$ASSUME_YES" -ne 1 ]; then
    printf 'Proceed? [y/N] ' >&2
    if ! read -r reply < /dev/tty; then
        reply=""
        echo >&2
    fi
    case "$reply" in
        y | Y | yes | YES) ;;
        *)
            echo "Aborted. Nothing deployed." >&2
            exit 1
            ;;
    esac
fi

# Sequential and fail-fast: the second play deploys a tool that depends on what
# the first one installs, so running it after a failure would deploy a selector
# for labels nothing sets.
for play in "${PLAYS[@]}"; do
    echo
    echo "### $play"
    echo
    if ! ansible-playbook "$REPO_ROOT/$play"; then
        echo >&2
        echo "ERROR: $play failed — see the output above." >&2
        echo "  Nothing after it was run." >&2
        exit 1
    fi
done

# --- one-off migration: the pre-rename binary --------------------------------
#
# The tool shipped for part of one day as `podman-freeze` before being
# shortened to `podfreeze`. Removing the old copy lives HERE rather than as a
# `state: absent` task in the play, because it is transient: it stops being
# useful the moment no machine has a pre-rename build, and a permanent task
# carrying a same-day rename forever is exactly the cruft this repo's
# plan-local rule exists to keep out of the shared tree.
#
# Stale executables on PATH are not cosmetic — two builds of the same tool
# means two versions of a confirmation prompt on one machine, and the one you
# get depends on which name you happen to type.
STALE="$HOME/.local/bin/podman-freeze"
if [ -e "$STALE" ]; then
    echo
    echo "### removing the pre-rename binary"
    if rm -f "$STALE"; then
        echo "  removed $STALE"
    else
        echo "ERROR: could not remove $STALE" >&2
        echo "  Both names are now on PATH — remove it before using podfreeze." >&2
        exit 1
    fi
fi

echo
echo "=============================================================="
echo "Deploy finished."
echo "=============================================================="

if [ "$VERIFY" -eq 0 ]; then
    echo
    echo "Skipping verification (--no-verify). Run it yourself:"
    echo "  $PLAN_DIR/acceptance.bash"
    exit 0
fi

if [ ! -x "$PLAN_DIR/acceptance.bash" ]; then
    echo "ERROR: acceptance.bash is missing or not executable." >&2
    echo "  Deploy succeeded but nothing verified it." >&2
    exit 1
fi

echo
echo "### handing over to acceptance.bash"
echo
# It writes its own log; this exec'd copy inherits the tee, so the deploy log
# carries the verdict too.
"$PLAN_DIR/acceptance.bash"

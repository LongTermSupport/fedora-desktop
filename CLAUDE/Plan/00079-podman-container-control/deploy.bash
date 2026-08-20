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

Runs playbooks/imports/optional/common/play-podfreeze.yml, which installs
fzf and deploys ~/.local/bin/podfreeze, then hands over to acceptance.bash
and exits with its verdict.

The play is idempotent, so re-running it is safe. It touches nothing but the
one script and one package — no services are restarted and no containers are
affected.
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

PLAY="playbooks/imports/optional/common/play-podfreeze.yml"

echo "=============================================================="
echo "Plan 00079 — deploy podfreeze"
echo "=============================================================="
echo

if [ ! -f "$REPO_ROOT/$PLAY" ]; then
    echo "ERROR: playbook not found: $REPO_ROOT/$PLAY" >&2
    exit 1
fi

echo "Play to run:"
echo "  $PLAY"
echo
echo "It installs fzf and deploys ~/.local/bin/podfreeze. Nothing else on"
echo "this machine is touched — no service is restarted, no container is"
echo "started, stopped, frozen, or thawed."
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

echo
if ! ansible-playbook "$REPO_ROOT/$PLAY"; then
    echo >&2
    echo "ERROR: $PLAY failed — see the output above." >&2
    exit 1
fi

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

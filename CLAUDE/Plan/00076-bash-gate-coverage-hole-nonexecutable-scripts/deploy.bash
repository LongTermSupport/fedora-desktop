#!/usr/bin/env bash
#
# Plan 00076 — deploy every script this plan changed. HOST ONLY.
#
# The first version of this script deployed only play-rclone.yml, on the
# assumption that the Task 4.3b rewrite was the plan's only undeployed change.
# It was not. Phase 3 fixed 34 shellcheck and 17 semgrep findings across eleven
# more scripts and every one of them was still sitting undeployed — the repo
# said fixed, the machine ran the old build. That is the Plan 00094 failure
# exactly, and the acceptance run's drift gate is what surfaced it.
#
# So this deploys by MEASUREMENT, not by a remembered list: each repo file is
# compared against its deployed copy, and only the plays whose files actually
# differ are run. That keeps it idempotent, avoids restarting the rclone mounts
# when they are already in sync, and cannot go stale the way a hardcoded list
# just did.
#
# Runs acceptance.bash itself when it finishes. A deploy whose verification is
# a separate command the human has to remember is a deploy that routinely goes
# unverified — and this plan exists because exactly that happened to Phase 3.
#
# Usage: deploy.bash [-y|--yes] [--no-verify] [--help]

set -uo pipefail

ASSUME_YES=0
VERIFY=1
for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00076 — deploy every script this plan changed (HOST ONLY)

Usage: deploy.bash [-y|--yes] [--no-verify] [--help]

  -y, --yes     do not ask for confirmation
  --no-verify   deploy only; do not run acceptance.bash afterwards

Runs acceptance.bash when it finishes and exits with ITS status, so a zero
exit means "deployed AND verified" rather than merely "ansible did not error".
That is deliberate: a deploy whose verification is a separate command someone
has to remember is a deploy that routinely goes unverified, which is how this
plan's Phase 3 sat undeployed while its tasks were marked complete.

Compares each script this plan touched against its deployed copy in
~/.local/bin/ and runs only the plays whose files differ. Files with no
deployed copy are SKIPPED — a machine that never installed a feature is not
nagged about it.

Scripts and their owning plays:

  rclone-tail, rclone-cache-status      play-rclone.yml
  ftp-camera                            play-ftp-camera.yml
  gshell-nested                         play-gnome-shell-dev.yml
  lan-scan                              play-network-tools.yml
  nord                                  play-nordvpn-openvpn.yml
  raw-prune                             play-photography.yml
  wsi                                   play-speech-to-text.yml
  scp-with-key, scp-with-password,      play-basic-configs.yml
  ssh-with-key, ssh-with-password,
  ssh-copy-id-with-password

REFUSES to run play-rclone.yml while an ftp-camera process is in flight: that
play restarts the mounts, which can lose cached-but-not-yet-uploaded data.

Until every difference is deployed, ./scripts/qa-all.bash FAILS on the host by
design — the deployed-drift gate compares the repo against ~/.local/bin/.
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

# script basename -> repo-relative playbook that deploys it
SCRIPT_PLAYS=(
    "rclone-tail:playbooks/imports/optional/common/play-rclone.yml"
    "rclone-cache-status:playbooks/imports/optional/common/play-rclone.yml"
    "ftp-camera:playbooks/imports/optional/common/play-ftp-camera.yml"
    "gshell-nested:playbooks/imports/optional/common/play-gnome-shell-dev.yml"
    "lan-scan:playbooks/imports/optional/common/play-network-tools.yml"
    "nord:playbooks/imports/optional/common/play-nordvpn-openvpn.yml"
    "raw-prune:playbooks/imports/optional/common/play-photography.yml"
    "wsi:playbooks/imports/optional/common/play-speech-to-text.yml"
    "scp-with-key:playbooks/imports/play-basic-configs.yml"
    "scp-with-password:playbooks/imports/play-basic-configs.yml"
    "ssh-with-key:playbooks/imports/play-basic-configs.yml"
    "ssh-with-password:playbooks/imports/play-basic-configs.yml"
    "ssh-copy-id-with-password:playbooks/imports/play-basic-configs.yml"
)

# Chains straight into verification, and exits with ITS status — so the whole
# thing is one command whose exit code means "deployed AND verified".
run_acceptance() {
    if [ "$VERIFY" -eq 0 ]; then
        echo
        echo "Skipping verification (--no-verify). Run it yourself:"
        echo "  $PLAN_DIR/acceptance.bash"
        return 0
    fi
    if [ ! -x "$PLAN_DIR/acceptance.bash" ]; then
        echo "ERROR: acceptance.bash is missing or not executable." >&2
        echo "  Deploy succeeded but nothing verified it." >&2
        return 1
    fi
    echo
    echo "### handing over to acceptance.bash"
    echo
    # It writes its own log; this exec'd copy inherits the tee, so the deploy
    # log carries the verdict too.
    "$PLAN_DIR/acceptance.bash"
}

echo "=============================================================="
echo "Plan 00076 — deploy"
echo "=============================================================="
echo

NEEDED=()
echo "### comparing repo against ~/.local/bin/"
for entry in "${SCRIPT_PLAYS[@]}"; do
    name="${entry%%:*}"
    play="${entry#*:}"
    repo_file="$REPO_ROOT/files/home/.local/bin/$name"
    deployed="$HOME/.local/bin/$name"

    if [ ! -f "$repo_file" ]; then
        echo "  ERROR: repo file missing: $repo_file"
        echo "         The table in this script is out of date."
        exit 1
    fi
    if [ ! -f "$deployed" ]; then
        printf '  %-28s not installed on this host — skipped\n' "$name"
        continue
    fi
    if cmp -s "$repo_file" "$deployed"; then
        printf '  %-28s in sync\n' "$name"
        continue
    fi
    printf '  %-28s DIFFERS -> %s\n' "$name" "$(basename "$play")"
    NEEDED+=("$play")
done

if [ ${#NEEDED[@]} -eq 0 ]; then
    echo
    echo "Nothing to deploy — every script this plan touched is already in sync."
    run_acceptance
    exit $?
fi

# dedupe, preserving nothing about order beyond determinism
PLAYS=()
while IFS= read -r p; do
    PLAYS+=("$p")
done < <(printf '%s\n' "${NEEDED[@]}" | sort -u)

echo
echo "### plays to run (${#PLAYS[@]}):"
for p in "${PLAYS[@]}"; do
    if [ ! -f "$REPO_ROOT/$p" ]; then
        echo "  ERROR: playbook not found: $REPO_ROOT/$p" >&2
        exit 1
    fi
    echo "  $p"
done

# --- in-flight copy guard, only when play-rclone is actually going to run -----
# Anchored to a path component or start-of-line, NOT a bare substring: a plain
# `pgrep -f ftp-camera` also matches any shell whose command line merely
# MENTIONS ftp-camera — including the one that invoked this script. Self and
# parent are excluded for the same reason.
rclone_selected=0
for p in "${PLAYS[@]}"; do
    case "$p" in *play-rclone.yml) rclone_selected=1 ;; esac
done
if [ "$rclone_selected" -eq 1 ]; then
    echo
    echo "  NOTE: play-rclone.yml is in the list — it rewrites the mount units"
    echo "        and RESTARTS the mounts."
    camera_pids=""
    raw_pids=""
    if raw_pids=$(pgrep -f '(^|/)ftp-camera([[:space:]]|$)'); then
        # grep exits 1 when EVERY hit was filtered out — the normal case here.
        # That is not an error, but under a strict shell an unguarded
        # assignment from it would abort the deploy silently, with no message
        # and no play run. Check the status.
        filtered=""
        if filtered=$(printf '%s\n' "$raw_pids" | grep -v -x -e "$$" -e "$PPID"); then
            camera_pids="$filtered"
        fi
    fi
    if [ -n "$camera_pids" ]; then
        echo
        echo "ERROR: an ftp-camera process is running." >&2
        echo "  Deploying now would restart the mount and interrupt the VFS" >&2
        echo "  write-back queue. Wait for the copy to finish, then re-run." >&2
        echo >&2
        echo "  Running processes:" >&2
        printf '%s\n' "$camera_pids" | while IFS= read -r pid; do
            ps -o pid=,args= -p "$pid" >&2
        done
        exit 1
    fi
fi

if [ "$ASSUME_YES" -ne 1 ]; then
    echo
    printf 'Run %d play(s)? [y/N] ' "${#PLAYS[@]}" >&2
    if ! read -r reply < /dev/tty; then reply=""; fi
    case "$reply" in
        y | Y | yes | YES) ;;
        *)
            echo "Aborted. Nothing deployed." >&2
            exit 1
            ;;
    esac
fi

i=0
for p in "${PLAYS[@]}"; do
    i=$((i + 1))
    echo
    echo "--- $i/${#PLAYS[@]}: $p"
    if ! ansible-playbook "$REPO_ROOT/$p"; then
        echo
        echo "ERROR: $p failed. Stopping — later plays are not run." >&2
        echo "  Fix the playbook and re-run; this script re-measures drift" >&2
        echo "  each time, so it will pick up where this left off." >&2
        exit 1
    fi
done

echo
echo "=============================================================="
echo "Deploy finished."
echo "=============================================================="

run_acceptance

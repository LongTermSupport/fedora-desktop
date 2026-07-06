#!/usr/bin/env bash
#
# deploy.bash — Plan 00058: re-run every playbook whose upstream version pin was
# bumped, so the HOST picks up the new releases.
#
# HOST-ONLY. Never run this inside the CCY container (/workspace) — the container
# has no target users/services and CLAUDE/ContainerRules.md forbids running
# Ansible there. Run it on the Fedora host after pulling the plan's commits.
#
# Fail-fast: a failed play stops the whole run (set -e) so you fix it before the
# next one, in line with the project's #1 rule.
#
# darktable is intentionally NOT here: Fedora dist-git still ships 5.4.1, so a
# 5.6.0 bump would break the spec-driven build (see PLAN.md, Phase 2).

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"

if [ "$repo_root" = "/workspace" ]; then
    echo "ERROR: this is the CCY container (/workspace). Run deploy.bash on the HOST, not here." >&2
    exit 1
fi

cd "$repo_root"

# Playbooks whose version pins changed in this plan, in a sensible order.
playbooks=(
    "playbooks/imports/play-nvm-install.yml"
    "playbooks/imports/play-markless.yml"
    "playbooks/imports/optional/common/play-compression-helpers.yml"
    "playbooks/imports/optional/common/play-photography.yml"
    "playbooks/imports/optional/common/play-qobuz.yml"
    # Hardware-specific: DKMS/evdi + Secure Boot MOK enrolment. Needs a real
    # DisplayLink dock to verify, and may prompt for a MOK password / reboot.
    "playbooks/imports/optional/hardware-specific/play-displaylink.yml"
)

echo "Plan 00058 — deploying ${#playbooks[@]} playbook(s) with bumped version pins."
echo

for play in "${playbooks[@]}"; do
    echo "==================================================================="
    echo ">>> ansible-playbook $play"
    echo "==================================================================="
    ansible-playbook "$play"
    echo
done

echo "All plays completed. Verify the pins are now current with:"
echo "  scripts/check-pinned-versions.bash"

#!/usr/bin/env bash
#
# Plan 00054 — deploy GitHub SSH-over-443 host tooling, in the correct order.
#
# Run this ON THE HOST (not inside the CCY container). It deploys, in order:
#   1. play-github-cli-multi.yml  — github-ssh-443 CLI + helper + ~/.ssh/config
#                                    override apply + always-on profile.d export
#   2. play-claude-yolo.yml       — the updated ccy wrapper (banner + env honour)
#
# Then it offers to enable 443 mode now (temporary, for this network/session).
#
# Idempotent: safe to re-run. Fails fast on any error (project rule #1).
set -euo pipefail

# ── Resolve repo root from this script's location (CLAUDE/Plan/NNNNN-x/update.bash
#    is three levels under the repo root) ──────────────────────────────────────
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../../.." && pwd)"

# ── Refuse to run inside the CCY container (edit/commit only there) ────────────
if [ "${REPO_ROOT}" = "/workspace" ]; then
    echo "ERROR: This is the CCY container (/workspace). Ansible must run on the HOST." >&2
    echo "       Run this script from your host clone, e.g. ~/Projects/fedora-desktop." >&2
    exit 1
fi

if ! command -v ansible-playbook >/dev/null; then
    echo "ERROR: ansible-playbook not found on PATH — run the main playbook first." >&2
    exit 1
fi

cd "${REPO_ROOT}"

echo "════════════════════════════════════════════════════════════════════════════════"
echo "  Plan 00054 — GitHub SSH-over-443 host tooling"
echo "════════════════════════════════════════════════════════════════════════════════"
echo "  Repo root: ${REPO_ROOT}"
echo "  Will deploy (in order):"
echo "    1. playbooks/imports/play-github-cli-multi.yml"
echo "    2. playbooks/imports/play-claude-yolo.yml"
echo "════════════════════════════════════════════════════════════════════════════════"
echo ""

read -rp "Proceed with deployment? [Y/n] " reply_deploy
case "${reply_deploy}" in
    [Nn]*)
        echo "Aborted — nothing deployed."
        exit 0
        ;;
esac

echo ""
echo "▶ [1/2] Deploying GitHub multi-account + 443 tooling …"
ansible-playbook playbooks/imports/play-github-cli-multi.yml

echo ""
echo "▶ [2/2] Deploying the updated ccy wrapper …"
ansible-playbook playbooks/imports/play-claude-yolo.yml

echo ""
echo "✓ Deployment complete. The 'github-ssh-443' CLI is now at /usr/local/bin."
echo ""

# ── Offer to enable 443 mode now (temporary — this session/network) ───────────
if ! command -v github-ssh-443 >/dev/null; then
    echo "NOTE: github-ssh-443 is not yet on this shell's PATH (it was just installed)."
    echo "      Open a new terminal, then: github-ssh-443 status"
    exit 0
fi

echo "Enable GitHub SSH-over-443 now? This edits ~/.ssh/config + known_hosts so all"
echo "GitHub SSH routes over ssh.github.com:443 (use it when port 22 is firewalled)."
echo "It is reversible at any time with: github-ssh-443 off"
echo ""
read -rp "Enable 443 mode now? [y/N] " reply_443
case "${reply_443}" in
    [Yy]*)
        echo ""
        github-ssh-443 on
        cat <<'EOF'

Now make THIS shell (and any ccy launched from it) follow the toggle:
    eval "$(github-ssh-443 env)"

ccy will display a "🔒 GitHub SSH-over-443 MODE ACTIVE" banner at launch.
EOF
        ;;
    *)
        echo ""
        echo "Left 443 mode off. Enable later with one of:"
        echo "    github-ssh-443 on        # temporary, this network"
        echo "    github-ssh-443 auto      # enable only if port 22 is blocked"
        echo ""
        echo "For ALWAYS-ON: set 'github_ssh_over_443: true' in"
        echo "  environment/localhost/host_vars/localhost.yml  and re-run this script."
        ;;
esac

#!/usr/bin/env bash
# acceptance.bash — Plan 00061 host-side gating proof (Task 3.8).
#
# SAFE-BY-DESIGN verification that the provisioning-profile self-guard gates
# correctly on a REAL host WITHOUT changing any system state:
#
#   * Server-path tests force `-e provisioning_profile=server`. The guard's
#     `meta: end_play` ends each gnome play BEFORE its first real (install)
#     task, so nothing can be installed — safe by construction, no --check
#     needed. We then assert NO real task actually ran.
#   * The desktop-path test forces `-e provisioning_profile=desktop --check`
#     (dry-run) — it proves real tasks are REACHED (guard does not over-block)
#     while applying zero changes.
#   * The detection + typo tests are read-only / fail-before-any-task.
#
# MUST run on the HOST (this repo's plays are `connection: local`, so the
# controller IS the target). It REFUSES to run inside the CCY container, where
# running Ansible is forbidden anyway.
#
# Usage (from anywhere in the repo, on your laptop host):
#   ./CLAUDE/Plan/00061-headless-server-provisioning/acceptance.bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# --- guards -------------------------------------------------------------------
if [[ "$REPO_ROOT" == "/workspace" ]]; then
    echo "REFUSING: this looks like the CCY container (/workspace)." >&2
    echo "Run this on your HOST clone of the repo instead." >&2
    exit 1
fi
export ANSIBLE_CONFIG="$REPO_ROOT/ansible.cfg"
if [[ ! -f "$REPO_ROOT/vault-pass.secret" ]]; then
    echo "ERROR: vault-pass.secret not found at repo root — it is needed to load host_vars." >&2
    exit 1
fi
if ! command -v ansible-playbook >/dev/null; then
    echo "ERROR: ansible-playbook not on PATH — run this on the provisioned host." >&2
    exit 1
fi

# Sample gnome plays (no pre_tasks, check-safe) used to exercise the guard.
GNOME_SAMPLES=(
    playbooks/imports/play-firefox.yml
    playbooks/imports/play-vscode.yml
    playbooks/imports/play-ms-fonts.yml
)
DESKTOP_CHECK_PLAY=playbooks/imports/play-firefox.yml

pass=0
fail=0
ok()  { echo "  PASS: $1"; pass=$((pass + 1)); }
bad() { echo "  FAIL: $1"; fail=$((fail + 1)); }

# Print the "leaked" (real, non-guard, non-facts) TASK names from a run's log.
# Empty output == the guard stopped the play before any real work. Pure awk so
# there is no pipeline-exit / error-hiding footgun.
real_tasks() {
    awk -F'[][]' '
        /^TASK \[/ {
            name = $2
            if (name != "Gathering Facts" && name !~ /^Scope guard/) print name
        }' "$1"
}

echo "=== Plan 00061 self-guard acceptance (host: $(hostname), repo: $REPO_ROOT) ==="

# --- 1. Auto-detection is read-only and resolves to a recognised profile ------
echo
echo "[1/4] Auto-detected provisioning_profile on this host:"
log="$(mktemp)"
detect_rc=0
ansible desktop -m debug -a "var=provisioning_profile" > "$log" 2>&1 || detect_rc=$?
if [[ $detect_rc -eq 0 ]] && grep -qE '"provisioning_profile": "(desktop|server)"' "$log"; then
    ok "detection resolved to $(grep -oE '"provisioning_profile": "[a-z]+"' "$log")"
else
    bad "detection did not resolve to desktop|server (rc=$detect_rc)"
    cat "$log" >&2
fi
rm -f "$log"

# --- 2. Server path: every gnome play ends at the guard, no real task runs -----
echo
echo "[2/4] Server-path guard (forced -e provisioning_profile=server, no changes possible):"
for play in "${GNOME_SAMPLES[@]}"; do
    log="$(mktemp)"
    if ansible-playbook "$play" -e provisioning_profile=server > "$log" 2>&1; then
        leaked="$(real_tasks "$log")"
        if [[ -z "$leaked" ]]; then
            ok "$play ended at the guard (no real task ran)"
        else
            bad "$play ran real task(s) under server profile: ${leaked//$'\n'/, }"
        fi
    else
        bad "$play exited non-zero under server profile (unexpected)"
        cat "$log" >&2
    fi
    rm -f "$log"
done

# --- 3. Typo protection: an unrecognised profile hard-fails at the assert ------
echo
echo "[3/4] Typo protection (forced -e provisioning_profile=srever, must fail fast):"
log="$(mktemp)"
if ansible-playbook "$DESKTOP_CHECK_PLAY" -e provisioning_profile=srever > "$log" 2>&1; then
    bad "run SUCCEEDED with bad profile 'srever' — the assert should have failed it"
    cat "$log" >&2
else
    if grep -q 'is not recognised' "$log"; then
        ok "bad profile 'srever' hard-failed at the recognised-value assert"
    else
        bad "run failed, but not at the recognised-value assert (see log)"
        cat "$log" >&2
    fi
fi
rm -f "$log"

# --- 4. Desktop path: real tasks ARE reached (dry-run, zero changes) -----------
echo
echo "[4/4] Desktop passthrough (forced -e provisioning_profile=desktop --check, dry-run):"
log="$(mktemp)"
if ansible-playbook "$DESKTOP_CHECK_PLAY" -e provisioning_profile=desktop --check > "$log" 2>&1; then
    leaked="$(real_tasks "$log")"
    if [[ -n "$leaked" ]]; then
        ok "$DESKTOP_CHECK_PLAY reached real task(s) under --check (guard did not over-block)"
    else
        bad "$DESKTOP_CHECK_PLAY reached no real task even on desktop profile"
        cat "$log" >&2
    fi
else
    bad "$DESKTOP_CHECK_PLAY --check errored (possible check-mode false failure — inspect log)"
    cat "$log" >&2
fi
rm -f "$log"

# --- summary ------------------------------------------------------------------
echo
echo "=== acceptance: $pass passed, $fail failed ==="
if [[ $fail -gt 0 ]]; then
    exit 1
fi

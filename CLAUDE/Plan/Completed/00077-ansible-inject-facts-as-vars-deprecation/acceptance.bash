#!/usr/bin/env bash
#
# Plan 00077 Task 2.2 — prove the fact-var conversion under the post-2.24 world.
# HOST ONLY.
#
# Disabling fact injection is exactly what ansible-core 2.24 will do
# permanently. A converted play that runs clean under it is proven; one that is
# merely argued to be correct is not.
#
# THE ENV VAR IS NOT NAMED AFTER THE SETTING. The config key is
# `inject_facts_as_vars`, the reported setting is `INJECT_FACTS_AS_VARS`, and
# the environment variable is `ANSIBLE_INJECT_FACT_VARS` — no "AS", and "FACT"
# singular. The first run of this script used the obvious-looking
# `ANSIBLE_INJECT_FACTS_AS_VARS`, which does not exist; ansible ignored it in
# silence and the run proved nothing. Confirm with:
#     ansible-config list | grep -A 14 '^INJECT_FACTS_AS_VARS'
#
# Two harness checks run before the real play, cheapest first:
#   0. `ansible-config dump --only-changed` must SHOW the setting as False.
#      This catches a wrong variable name in one command, before any play runs.
#   1. A throwaway play referencing `ansible_distribution` must FAIL. This is
#      the end-to-end version: the setting can be reported correctly and still
#      not change task-time behaviour.
#
# If either check does not do what it must, the positive case is SKIPPED rather
# than reported — a gate that passes because it never ran is this repo's
# recurring defect, and it is what check 0 and check 1 exist to prevent.
#
# Usage: acceptance.bash [--play PATH] [--help]

set -uo pipefail

PLAY_REL="playbooks/imports/play-rpm-fusion.yml"

while [ $# -gt 0 ]; do
    case "$1" in
        -h | --help)
            cat << 'EOF'
Plan 00077 Task 2.2 — acceptance (HOST ONLY)

Usage: acceptance.bash [--play PATH] [--help]

  --play PATH   repo-relative playbook to run as the positive case
                (default: playbooks/imports/play-rpm-fusion.yml)

Fact injection is disabled with ANSIBLE_INJECT_FACT_VARS=False — note the
name: no "AS", and "FACT" singular. The setting it controls is reported as
INJECT_FACTS_AS_VARS, which is NOT the variable name.

Runs, in order:
  0. HARNESS CHECK — `ansible-config dump --only-changed` must report the
     setting as False. Catches a wrong variable name before any play runs.
  1. NEGATIVE CONTROL — a throwaway play referencing the deprecated
     `ansible_distribution`. It MUST FAIL. If it succeeds, the setting did
     not change task-time behaviour and every other result is meaningless.
  2. POSITIVE CASE — the converted play. It MUST PASS.

Scope, stated rather than implied: the default play exercises 2 of the 11
converted sites at runtime. The other 9 are covered statically by
scripts/qa-ansible.bash Check 5, which fails on any top-level ansible_<fact>
reference. Use --play to run a heavier one if you want more runtime coverage.

The default play is idempotent (it configures the RPM Fusion repos).
EOF
            exit 0
            ;;
        --play)
            if [ $# -lt 2 ]; then
                echo "ERROR: --play needs a path." >&2
                exit 1
            fi
            PLAY_REL="$2"
            shift 2
            ;;
        *)
            echo "ERROR: unknown argument: $1" >&2
            echo "  Try: acceptance.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "ERROR: this looks like a CCY container (/workspace)." >&2
    echo "  Ansible must run on the HOST, not in the container." >&2
    echo "  See CLAUDE/ContainerRules.md." >&2
    exit 1
fi

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/acceptance.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

PLAY="$REPO_ROOT/$PLAY_REL"
if [ ! -f "$PLAY" ]; then
    echo "ERROR: playbook not found: $PLAY" >&2
    exit 1
fi

# ansible.cfg lives at the repo root and supplies the inventory and vault path.
if ! cd "$REPO_ROOT"; then
    echo "ERROR: could not enter the repo root: $REPO_ROOT" >&2
    exit 1
fi

CONTROL_DIR="$(mktemp -d)"
trap 'rm -rf "$CONTROL_DIR"' EXIT
CONTROL="$CONTROL_DIR/negative-control.yml"

# Deliberately uses the DEPRECATED form. Generated at runtime rather than
# committed, so no file in this repo teaches the shape the plan exists to
# remove.
cat > "$CONTROL" << 'EOF'
- hosts: desktop
  name: Plan 00077 negative control — must fail under INJECT_FACTS_AS_VARS=False
  gather_facts: true
  become: false
  tasks:
    - name: Reference a top-level fact variable
      ansible.builtin.debug:
        msg: "distribution is {{ ansible_distribution }}"
EOF

echo "=============================================================="
echo "Plan 00077 Task 2.2 — acceptance"
echo "  ANSIBLE_INJECT_FACT_VARS=False  (the post-2.24 world)"
echo "=============================================================="

FAIL=0

# --- 0. cheapest harness check: did the variable even land? -------------------
# `--only-changed` lists a setting solely when something moved it off its
# default, and annotates it with the source. A wrong variable name shows up
# here as an absent line, in one command, before any play runs.
echo
echo "### 0. HARNESS CHECK — the setting must actually be off"
dump=""
if ! dump=$(ANSIBLE_INJECT_FACT_VARS=False ansible-config dump --only-changed 2>&1); then
    echo "  FAIL — 'ansible-config dump' did not run:"
    printf '%s\n' "$dump" | sed 's/^/    /'
    FAIL=1
elif printf '%s' "$dump" | grep -q '^INJECT_FACTS_AS_VARS.*=[[:space:]]*False'; then
    printf '%s\n' "$dump" | grep '^INJECT_FACTS_AS_VARS' | sed 's/^/  /'
    echo "  OK — the setting is off and ansible says where from."
else
    echo "  FAIL — INJECT_FACTS_AS_VARS is not reported as False."
    echo "         The variable name is probably wrong. Check it with:"
    echo "           ansible-config list | grep -A 14 '^INJECT_FACTS_AS_VARS'"
    echo "         It is ANSIBLE_INJECT_FACT_VARS — no 'AS', 'FACT' singular."
    FAIL=1
fi

echo
echo "### 1. NEGATIVE CONTROL — a deprecated reference MUST fail"
echo "        $CONTROL"
if [ "$FAIL" -ne 0 ]; then
    echo "  SKIPPED — check 0 already showed the setting is not in force."
elif ANSIBLE_INJECT_FACT_VARS=False ansible-playbook "$CONTROL"; then
    echo
    echo "  FAIL — the control SUCCEEDED. The flag did not take effect, so"
    echo "         nothing below proves anything. Check that ansible.cfg does"
    echo "         not set inject_facts_as_vars, and that this ansible-core"
    echo "         still honours the environment variable."
    FAIL=1
else
    echo
    echo "  OK — the control failed as required (undefined variable)."
    echo "       The flag is in force, so the positive case below is meaningful."
fi

echo
echo "### 2. POSITIVE CASE — the converted play MUST pass"
echo "        $PLAY_REL"
if [ "$FAIL" -ne 0 ]; then
    echo "  SKIPPED — refusing to report a pass from a harness that is not"
    echo "            proven to be applying the flag."
else
    if ANSIBLE_INJECT_FACT_VARS=False ansible-playbook "$PLAY"; then
        echo
        echo "  OK — ran clean with the injection disabled."
    else
        echo
        echo "  FAIL — the converted play still depends on an injected fact"
        echo "         variable, or failed for another reason. Read the task"
        echo "         name in the error above."
        FAIL=1
    fi
fi

echo
echo "=============================================================="
if [ "$FAIL" -ne 0 ]; then
    echo "VERDICT: FAIL — Task 2.2 is not satisfied."
    echo "=============================================================="
    exit 1
fi
echo "VERDICT: PASS — $PLAY_REL runs clean with fact injection disabled,"
echo "         and the harness is proven to be applying the flag."
echo
echo "  Runtime coverage: the sites in this play only. The remaining"
echo "  converted sites are held by scripts/qa-ansible.bash Check 5."
echo "=============================================================="

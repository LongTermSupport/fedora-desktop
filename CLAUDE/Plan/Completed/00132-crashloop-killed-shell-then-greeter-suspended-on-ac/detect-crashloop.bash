#!/usr/bin/env bash
# Plan 00132 — a working prototype of the crash-loop detection probe (Task 5.1).
#
# WHY THIS EXISTS, AND WHY IT IS HERE RATHER THAN IN helpers/containerwatch/:
#
# The production probe belongs in plan 00055's watchdog, and building it there
# is another agent's job. But the TEST CASE expires. A container is crash-looping
# on this host right now at roughly 140 restarts/minute, and it is the only
# unsimulated instance of this defect in existence. The moment anyone stops it,
# the true positive is gone and the production probe can only ever be validated
# against a synthetic reproduction.
#
# So this script implements the proposed detection algorithm EXACTLY as specified
# in research/detection-gap.md, and runs it against the live loop, so that the
# algorithm is shown to fire on real data before the data disappears. Whoever
# builds the production probe inherits a verified algorithm and a recorded true
# positive rather than a paragraph of prose.
#
# READ-ONLY. It runs `podman ps` and `podman inspect` and nothing else. It does
# not stop, kill, pause, restart or configure any container.
#
# ANONYMISED BY DEFAULT, and that is the point: this plan folder is TRACKED in a
# PUBLIC repository, so the output has to be quotable in PLAN.md. Containers are
# reported as container-A, container-B, … in descending restart order. Pass
# --names to see the real ones on a terminal; never paste that form anywhere.
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

plan_mode gather

usage() {
    cat << 'EOF'
Plan 00132 — crash-loop detection prototype (HOST ONLY, read-only)

Usage: detect-crashloop.bash [--interval N] [--names] [--help]

  --interval N   Seconds between the two RestartCount samples. Default 60.
                 The production tick is 120s; the rate threshold scales with
                 this so a shorter sample is not a laxer test.
  --names        Print real container names instead of container-A/B/C.
                 NEVER paste that output into a tracked file — this repo is
                 public. The default anonymised form is the quotable one.
  --help         Show this help and exit.

Implements the algorithm specified in research/detection-gap.md:

  RATE     : RestartCount delta between two samples, >= 10 per 120s tick.
  ABSOLUTE : RestartCount >= 1000 on a single sample, so a loop already
             running when the watchdog starts is caught on the FIRST tick
             rather than the second.

Exit 0 = no crash loop detected. Exit 2 = at least one container flagged.
Exit 1 = the probe could not run, which is NOT the same as "nothing found".
EOF
}

INTERVAL=60
SHOW_NAMES=0

# From research/detection-gap.md. The rate bound is expressed per production
# tick and scaled to whatever sample interval is used, so shortening the
# interval cannot silently weaken the test.
RATE_PER_TICK=10
PRODUCTION_TICK_S=120
ABSOLUTE_THRESHOLD=1000

while [ "$#" -gt 0 ]; do
    case "$1" in
        -h | --help)
            usage
            exit 0
            ;;
        --interval)
            if [ "$#" -lt 2 ]; then
                echo "ERROR: --interval needs a number of seconds" >&2
                exit 1
            fi
            case "$2" in
                '' | *[!0-9]*)
                    echo "ERROR: --interval takes a positive integer, got: $2" >&2
                    exit 1
                    ;;
            esac
            if [ "$2" -lt 5 ]; then
                echo "ERROR: --interval below 5s cannot measure a rate reliably" >&2
                exit 1
            fi
            INTERVAL="$2"
            shift 2
            continue
            ;;
        --names)
            SHOW_NAMES=1
            shift
            continue
            ;;
        *)
            echo "ERROR: unknown argument: $1" >&2
            echo "  Try: detect-crashloop.bash --help" >&2
            exit 1
            ;;
    esac
done

plan_require_host "it reads the host container engine's live restart counters"

if ! command -v podman > /dev/null; then
    echo "ERROR: podman is not installed." >&2
    echo "  It is declared in playbooks/imports/play-podman.yml. Deploy it:" >&2
    echo "    ansible-playbook playbooks/imports/play-podman.yml" >&2
    echo "  Do NOT install it by hand." >&2
    exit 1
fi

# Integer maths, rounding the scaled threshold DOWN, then clamped to at least 1.
# Rounding up would make a short interval stricter than the production tick and
# manufacture findings the real probe would not raise.
RATE_THRESHOLD=$(( RATE_PER_TICK * INTERVAL / PRODUCTION_TICK_S ))
if [ "$RATE_THRESHOLD" -lt 1 ]; then
    RATE_THRESHOLD=1
fi

echo "================================================================"
echo "Plan 00132 — crash-loop detection prototype"
echo "  sample interval   : ${INTERVAL}s"
echo "  rate threshold    : ${RATE_THRESHOLD} restart(s) per ${INTERVAL}s"
echo "                      (= ${RATE_PER_TICK} per ${PRODUCTION_TICK_S}s production tick)"
echo "  absolute threshold: ${ABSOLUTE_THRESHOLD} cumulative restarts"
if [ "$SHOW_NAMES" -eq 1 ]; then
    echo "  names             : REAL — do not paste this output anywhere tracked"
else
    echo "  names             : anonymised (container-A, container-B, …)"
fi
echo "================================================================"
echo

# sample_counts — emit "<id> <restartcount>" per container, one per line.
# LXC has no equivalent counter and is reported as out of scope rather than
# silently omitted: a container engine that is skipped without saying so reads
# as a container engine that was checked and found clean.
sample_counts() {
    local ids id count
    if ! ids="$(podman ps -aq)"; then
        echo "ERROR: could not list containers" >&2
        return 1
    fi
    while read -r id; do
        if [ -z "$id" ]; then
            continue
        fi
        if ! count="$(podman inspect --format '{{.RestartCount}}' "$id" 2>/dev/null)"; then
            continue
        fi
        printf '%s %s\n' "$id" "$count"
    done <<< "$ids"
}

echo "==> sample 1 …"
if ! SAMPLE1="$(sample_counts)"; then
    echo "FATAL: the first sample failed. No conclusion is available -- this is" >&2
    echo "  NOT a clean result." >&2
    exit 1
fi
N1="$(printf '%s\n' "$SAMPLE1" | grep -c . || :)"
echo "    ${N1} container(s)"

echo "==> waiting ${INTERVAL}s …"
sleep "$INTERVAL"

echo "==> sample 2 …"
if ! SAMPLE2="$(sample_counts)"; then
    echo "FATAL: the second sample failed. No conclusion is available." >&2
    exit 1
fi
N2="$(printf '%s\n' "$SAMPLE2" | grep -c . || :)"
echo "    ${N2} container(s)"
echo

# Compare. A container present in sample 2 but not sample 1 has no delta and is
# reported on its absolute count alone rather than skipped.
FINDINGS=0
LABEL_INDEX=0
REPORT=""
HISTORIC=""
ALPHABET='ABCDEFGHIJKLMNOPQRSTUVWXYZ'

while read -r id count2; do
    if [ -z "$id" ]; then
        continue
    fi

    LABEL_INDEX=$(( LABEL_INDEX + 1 ))
    if [ "$LABEL_INDEX" -le 26 ]; then
        label="container-${ALPHABET:$(( LABEL_INDEX - 1 )):1}"
    else
        label="container-${LABEL_INDEX}"
    fi
    if [ "$SHOW_NAMES" -eq 1 ]; then
        if ! label="$(podman inspect --format '{{.Name}}' "$id" 2>/dev/null)"; then
            label="(name unreadable)"
        fi
    fi

    count1="$(printf '%s\n' "$SAMPLE1" | awk -v want="$id" '$1 == want { print $2 }')"

    reasons=""
    if [ -n "$count1" ]; then
        delta=$(( count2 - count1 ))
    else
        delta=""
    fi

    if [ -n "$delta" ] && [ "$delta" -ge "$RATE_THRESHOLD" ]; then
        reasons="RATE (${delta} restarts in ${INTERVAL}s, threshold ${RATE_THRESHOLD})"
    fi

    # The ABSOLUTE test is gated on the container actually RUNNING, and that gate
    # is not a nicety -- without it this test is a permanent false positive.
    #
    # RestartCount is cumulative and never resets. The first version of this
    # script omitted the gate, and the true-negative run caught it immediately:
    # after the loop was stopped the rate test correctly went silent, while the
    # absolute test went on flagging 131,377 restarts on an exited container --
    # and would have done so for ever, on a container already dealt with. An
    # alarm that cannot be cleared is one an operator learns to ignore, which
    # costs more than having no alarm at all.
    #
    # A container that is not running cannot be looping. That is the whole test.
    if [ "$count2" -ge "$ABSOLUTE_THRESHOLD" ]; then
        if ! running="$(podman inspect --format '{{.State.Running}}' "$id" 2>/dev/null)"; then
            running="unknown"
        fi
        if [ "$running" = 'true' ]; then
            reasons="${reasons}${reasons:+ + }ABSOLUTE (${count2} cumulative, threshold ${ABSOLUTE_THRESHOLD}, still running)"
        else
            HISTORIC="${HISTORIC}${label} has ${count2} cumulative restarts but is not running -- historic, not looping"$'\n'
        fi
    fi

    if [ -n "$reasons" ]; then
        FINDINGS=$(( FINDINGS + 1 ))
        REPORT="${REPORT}FLAGGED  ${label}  ${reasons}"$'\n'
    else
        REPORT="${REPORT}ok       ${label}  cumulative=${count2} delta=${delta:-n/a}"$'\n'
    fi
done <<< "$SAMPLE2"

printf '%s' "$REPORT"
if [ -n "$HISTORIC" ]; then
    echo
    echo "Historic, deliberately NOT flagged:"
    printf '%s' "$HISTORIC" | awk '{print "  " $0}'
fi
echo
echo "----------------------------------------------------------------"
echo "LXC: no RestartCount equivalent — OUT OF SCOPE, not checked."
echo "     Stated explicitly so an unchecked engine is never read as a"
echo "     clean one."
echo "----------------------------------------------------------------"

if [ "$FINDINGS" -gt 0 ]; then
    echo
    echo "RESULT: ${FINDINGS} container(s) FLAGGED — detection fired."
    echo
    echo "If a crash loop is known to be running right now, this is the TRUE"
    echo "POSITIVE the design turns on: the algorithm fired against real data"
    echo "rather than a synthetic reproduction."
    exit 2
fi

echo
echo "RESULT: no container flagged."
echo
echo "If a crash loop is known to be running right now, this is a FALSE"
echo "NEGATIVE and the algorithm is wrong — do not ship it."
echo "If the loop has been stopped, this is the TRUE NEGATIVE that completes"
echo "the validation pair."
exit 0

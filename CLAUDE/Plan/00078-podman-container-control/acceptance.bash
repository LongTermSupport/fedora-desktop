#!/usr/bin/env bash
#
# Plan 00078 — acceptance gate for podfreeze. HOST ONLY.
#
# Renders a PASS/FAIL verdict (triage.bash gathers facts and renders none —
# see CLAUDE/PlanTriage.md).
#
# WHAT IT EXERCISES, AND WHAT IT DELIBERATELY DOES NOT:
#
# Every state-changing check runs against a THROWAWAY container on a THROWAWAY
# network created by this script, so a bug in the tool's selection logic cannot
# reach a real container. The one check that must involve real containers —
# that --ccy resolves the live CCY session group — is run with --dry-run, so it
# resolves and prints the set without touching it.
#
# `--all` is never run for real, at any point. Nothing here should be able to
# freeze the machine's containers as a side effect of testing.
#
# Usage: acceptance.bash [--help]

set -uo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00078 — acceptance gate for podfreeze (HOST ONLY)

Usage: acceptance.bash [--help]

Creates a throwaway container on a throwaway network, then checks:

   1  --help works                          (exits 0, prints usage)
   2  refuses to run inside a container      (the container= env path)
   3  list reports the throwaway as running
   4  freeze --network -n previews it and changes nothing
   5  freeze --network -y actually freezes it
   6  list reports it as frozen
   7  freezing again is a clean no-op, not a failure
   8  thaw by name unfreezes it
   9  freeze --ccy -n resolves the live CCY group and excludes non-CCY
  10  an unknown network fails loudly rather than resolving to an empty set
  11  an unknown container name fails loudly
  12  two targets at once is rejected

Writes its log to this plan's logs/acceptance.log. The throwaway container and
network are removed on exit, including on failure.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: acceptance.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "ERROR: this looks like a CCY container (/workspace)." >&2
    echo "  This gate needs the HOST's podman. See CLAUDE/ContainerRules.md." >&2
    exit 1
fi

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/acceptance.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

TOOL="$HOME/.local/bin/podfreeze"
REPO_TOOL="$REPO_ROOT/files/home/.local/bin/podfreeze"
NET="podfreeze-acceptance-net-$$"
CNAME="podfreeze-acceptance-$$"

PASS=0
FAIL=0
SKIP=0

ok() {
    echo "  OK — $*"
    PASS=$((PASS + 1))
}

bad() {
    echo "  FAIL — $*"
    FAIL=$((FAIL + 1))
}

skip() {
    echo "  SKIP — $*"
    SKIP=$((SKIP + 1))
}

echo "=============================================================="
echo "Plan 00078 — acceptance: podfreeze"
echo "=============================================================="
echo

# --- preflight ---------------------------------------------------------------

if ! command -v podman > /dev/null; then
    echo "ERROR: podman is not installed." >&2
    echo "  It is declared in playbooks/imports/play-podman.yml. Deploy it with:" >&2
    echo "    ansible-playbook playbooks/imports/play-podman.yml" >&2
    echo "  Do NOT install it by hand." >&2
    exit 1
fi

if [ ! -x "$TOOL" ]; then
    echo "ERROR: $TOOL is not deployed." >&2
    echo "  Run this plan's deploy.bash first." >&2
    exit 1
fi

# The gate must vouch for what the machine RUNS, not for what the repo says.
if ! cmp -s "$REPO_TOOL" "$TOOL"; then
    echo "ERROR: the deployed podfreeze differs from the repo copy." >&2
    echo "  Deployed: $TOOL" >&2
    echo "  Repo:     $REPO_TOOL" >&2
    echo "  Run this plan's deploy.bash — verifying a stale binary proves nothing." >&2
    exit 1
fi

state_of() {
    local out
    if ! out="$(podman inspect --format '{{.State.Status}}' "$1" 2>&1)"; then
        printf 'MISSING'
        return 0
    fi
    printf '%s' "$out"
}

# --- throwaway fixture -------------------------------------------------------

cleanup() {
    local status
    echo
    echo "### cleanup"
    status="$(state_of "$CNAME")"
    if [ "$status" = "paused" ]; then
        if ! out="$(podman unpause "$CNAME" 2>&1)"; then
            echo "  WARNING: could not unpause $CNAME: $out"
        fi
    fi
    if [ "$status" != "MISSING" ]; then
        if out="$(podman rm --force "$CNAME" 2>&1)"; then
            echo "  removed container $CNAME"
        else
            echo "  WARNING: could not remove $CNAME: $out"
        fi
    fi
    if out="$(podman network rm "$NET" 2>&1)"; then
        echo "  removed network $NET"
    else
        echo "  note: network $NET not removed: $out"
    fi
}
trap cleanup EXIT

echo "### fixture"
if ! out="$(podman network create "$NET" 2>&1)"; then
    echo "ERROR: could not create throwaway network $NET: $out" >&2
    exit 1
fi
echo "  created network $NET"

# Use an image already on this machine — the gate must not depend on a registry
# being reachable. Candidates are tried in turn because an image with no shell
# cannot host the sleep loop.
#
# The fixture image must NOT carry the claude-yolo-version label. The first
# version of this script PREFERRED a claude-yolo image, on the reasoning that
# it is certain to have a shell — and that made check 9 (the throwaway is
# excluded from --ccy) fail against a tool that was behaving perfectly: the
# throwaway genuinely WAS a CCY-labelled container. A test whose fixture
# violates its own precondition reports a defect that is not there, which is
# the same shape of wrong answer this whole plan is about.
#
# So: unlabelled images first, CCY-labelled ones only as a last resort, and
# when only the latter is available check 9's exclusion assertion SKIPS with
# the reason rather than failing.
if ! images="$(podman images --format '{{.Repository}}:{{.Tag}}' 2>&1)"; then
    echo "ERROR: podman images failed: $images" >&2
    exit 1
fi
if ! ccy_images="$(podman images --filter label=claude-yolo-version \
    --format '{{.Repository}}:{{.Tag}}' 2>&1)"; then
    echo "ERROR: podman images --filter label=claude-yolo-version failed: $ccy_images" >&2
    exit 1
fi

PLAIN=()
LABELLED=()
while read -r img; do
    case "$img" in
        "" | *"<none>"*) continue ;;
    esac
    if printf '%s\n' "$ccy_images" | grep -qxF -- "$img"; then
        LABELLED+=("$img")
    else
        PLAIN+=("$img")
    fi
done <<< "$images"

CANDIDATES=("${PLAIN[@]+${PLAIN[@]}}" "${LABELLED[@]+${LABELLED[@]}}")

if [ "${#CANDIDATES[@]}" -eq 0 ]; then
    echo "ERROR: no local container images to build a throwaway container from." >&2
    echo "  Pull any small image and re-run, e.g.: podman pull alpine" >&2
    exit 1
fi

STARTED=0
FIXTURE_IS_CCY=0
for img in "${CANDIDATES[@]:0:5}"; do
    if out="$(podman run --detach --name "$CNAME" --network "$NET" \
        --entrypoint sh "$img" -c 'while true; do sleep 1; done' 2>&1)"; then
        if printf '%s\n' "$ccy_images" | grep -qxF -- "$img"; then
            FIXTURE_IS_CCY=1
            echo "  started $CNAME from $img"
            echo "  NOTE: that image carries the claude-yolo-version label, so the"
            echo "        throwaway is legitimately part of the --ccy set and check 9's"
            echo "        exclusion assertion cannot be made."
        else
            echo "  started $CNAME from $img (no claude-yolo-version label)"
        fi
        STARTED=1
        break
    fi
    echo "  $img would not start a shell — trying the next image"
    # The failed attempt may have left a created-but-dead container holding
    # the name, which would make the next attempt fail for the wrong reason.
    if [ "$(state_of "$CNAME")" != "MISSING" ]; then
        if ! out="$(podman rm --force "$CNAME" 2>&1)"; then
            echo "  WARNING: could not clear the failed attempt: $out"
        fi
    fi
done

if [ "$STARTED" -ne 1 ]; then
    echo "ERROR: could not start a throwaway container from any local image." >&2
    exit 1
fi

if [ "$(state_of "$CNAME")" != "running" ]; then
    echo "ERROR: $CNAME did not reach the running state." >&2
    exit 1
fi
echo

# --- checks ------------------------------------------------------------------

echo "### 1. --help"
if help_out="$("$TOOL" --help 2>&1)"; then
    case "$help_out" in
        *"Usage: podfreeze"*) ok "help printed, exit 0" ;;
        *) bad "help ran but printed no usage line" ;;
    esac
else
    bad "--help exited non-zero"
fi

echo "### 2. refuses to run inside a container"
if refuse_out="$(container=podman "$TOOL" list 2>&1)"; then
    bad "ran anyway inside a simulated container (exit 0)"
else
    case "$refuse_out" in
        *"run podfreeze on the HOST"*) ok "refused, with the HOST instruction" ;;
        *) bad "refused, but not with the expected message: $refuse_out" ;;
    esac
fi

echo "### 3. list reports the throwaway as running"
if list_out="$("$TOOL" list 2>&1)"; then
    case "$list_out" in
        *"$CNAME"*) ok "$CNAME appears in list" ;;
        *) bad "$CNAME missing from list output" ;;
    esac
else
    bad "list exited non-zero: $list_out"
fi

echo "### 4. freeze --network -n previews and changes nothing"
if dry_out="$("$TOOL" freeze --network "$NET" --dry-run 2>&1)"; then
    case "$dry_out" in
        *"DRY RUN"*"$CNAME"*) ok "dry run resolved $CNAME" ;;
        *) bad "dry run did not name $CNAME: $dry_out" ;;
    esac
else
    bad "dry run exited non-zero: $dry_out"
fi
if [ "$(state_of "$CNAME")" = "running" ]; then
    ok "still running after the dry run"
else
    bad "the dry run changed the container state"
fi

echo "### 5. freeze --network -y freezes it"
if freeze_out="$("$TOOL" freeze --network "$NET" --yes 2>&1)"; then
    if [ "$(state_of "$CNAME")" = "paused" ]; then
        ok "$CNAME is paused"
    else
        bad "freeze reported success but the state is $(state_of "$CNAME")"
    fi
else
    bad "freeze exited non-zero: $freeze_out"
fi

echo "### 6. list reports it as frozen"
if list_out="$("$TOOL" list 2>&1)"; then
    frozen_block="${list_out%%=== running*}"
    case "$frozen_block" in
        *"$CNAME"*) ok "$CNAME is listed under frozen" ;;
        *) bad "$CNAME is not under the frozen heading" ;;
    esac
else
    bad "list exited non-zero: $list_out"
fi

echo "### 7. freezing again is a clean no-op"
if again_out="$("$TOOL" freeze --network "$NET" --yes 2>&1)"; then
    case "$again_out" in
        *"Nothing to do"*) ok "reported nothing to do, exit 0" ;;
        *) bad "exit 0 but without the no-op message: $again_out" ;;
    esac
else
    bad "a second freeze failed instead of being a no-op: $again_out"
fi

echo "### 8. thaw by name unfreezes it"
if thaw_out="$("$TOOL" thaw "$CNAME" --yes 2>&1)"; then
    if [ "$(state_of "$CNAME")" = "running" ]; then
        ok "$CNAME is running again"
    else
        bad "thaw reported success but the state is $(state_of "$CNAME")"
    fi
else
    bad "thaw exited non-zero: $thaw_out"
fi

echo "### 9. freeze --ccy -n resolves the live CCY group"
# Asserted as a CONTRACT, not by re-deriving the tool's own selection: every
# running CCY container must appear, and the throwaway (no label, no matching
# name) must not.
#
# Scoped to status=running on purpose. A CCY container that is already paused
# would appear in the tool's output under "Skipped — not currently running",
# so the check would pass without the resolver having selected it — a pass
# earned by a substring rather than by the behaviour being tested.
if ! ccy_live="$(podman ps --filter label=claude-yolo-version \
    --filter status=running --format '{{.Names}}' 2>&1)"; then
    bad "could not list CCY containers: $ccy_live"
elif [ -z "$ccy_live" ]; then
    skip "no CCY containers are running — nothing to resolve"
elif ccy_out="$("$TOOL" freeze --ccy --dry-run 2>&1)"; then
    missing=""
    while read -r name; do
        if [ -z "$name" ]; then
            continue
        fi
        case "$ccy_out" in
            *"$name"*) ;;
            *) missing="$missing $name" ;;
        esac
    done <<< "$ccy_live"
    if [ -n "$missing" ]; then
        bad "--ccy omitted running CCY container(s):$missing"
    else
        ok "every running CCY container is in the --ccy set"
    fi
    if [ "$FIXTURE_IS_CCY" -eq 1 ]; then
        skip "the throwaway was built from a CCY-labelled image, so it belongs in
         the --ccy set — the exclusion assertion needs an unlabelled image"
    else
        case "$ccy_out" in
            *"$CNAME"*) bad "--ccy wrongly included the non-CCY throwaway $CNAME" ;;
            *) ok "the non-CCY throwaway is excluded" ;;
        esac
    fi
else
    bad "freeze --ccy --dry-run exited non-zero: $ccy_out"
fi
if [ "$(state_of "$CNAME")" = "running" ]; then
    ok "nothing was frozen by the --ccy dry run"
else
    bad "the --ccy dry run changed a container state"
fi

echo "### 10. an unknown network fails loudly"
if unknown_out="$("$TOOL" freeze --network "no-such-network-$$" --yes 2>&1)"; then
    bad "an unknown network resolved to an empty set and exited 0"
else
    case "$unknown_out" in
        *"no such network"*) ok "refused, naming the missing network" ;;
        *) bad "failed, but not with the expected message: $unknown_out" ;;
    esac
fi

echo "### 11. an unknown container name fails loudly"
if unknown_out="$("$TOOL" freeze "no-such-container-$$" --yes 2>&1)"; then
    bad "an unknown container name exited 0"
else
    case "$unknown_out" in
        *"not a running or frozen container"*) ok "refused, naming the unknown container" ;;
        *) bad "failed, but not with the expected message: $unknown_out" ;;
    esac
fi

echo "### 12. two targets at once is rejected"
if both_out="$("$TOOL" freeze --ccy --all --dry-run 2>&1)"; then
    bad "--ccy and --all together were accepted"
else
    case "$both_out" in
        *"mutually exclusive"*) ok "refused as mutually exclusive" ;;
        *) bad "failed, but not with the expected message: $both_out" ;;
    esac
fi

echo
echo "=============================================================="
if [ "$FAIL" -eq 0 ]; then
    echo "VERDICT: PASS — $PASS check(s) passed, $SKIP skipped."
else
    echo "VERDICT: FAIL — $FAIL of $((PASS + FAIL)) check(s) failed, $SKIP skipped."
fi
echo "=============================================================="

# The script's status IS this test — deliberately not `exit 0`/`exit 1`.
# Version 0.9.0 of the linter cannot trace the EXIT-trap edge out of a terminal
# `exit` node, so an explicit exit here makes it report the whole cleanup()
# body as unreachable (SC2317). Leaving the final command as the verdict test
# gives the same exit status with no suppression annotation.
[ "$FAIL" -eq 0 ]

#!/usr/bin/env bash
#
# Plan 00079 — acceptance gate for podfreeze. HOST ONLY.
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
Plan 00079 — acceptance gate for podfreeze (HOST ONLY)

Usage: acceptance.bash [--help]

Runs the selection unit test first (no podman needed), then creates a throwaway
container on a throwaway network and checks:

   0  selection/labelling unit test passes
   1  --help works                          (exits 0, prints usage)
   2  refuses to run inside a container      (the container= env path)
   3  list reports the throwaway as running
   4  freeze --network -n previews it and changes nothing
   5  freeze --network actually freezes it
   6  list reports it as frozen
   7  freezing again is a clean no-op, not a failure
   8  thaw by name unfreezes it
  8b  NO VERB freezes a running target       (the derived verb)
  8c  the same command thaws it again        (so it toggles)
   9  freeze --ccy -n resolves the live CCY group and excludes non-CCY
  9b  --ccy also resolves UNLABELLED pre-3.40.0 sessions via the name fallback
  10  an unknown network fails loudly rather than resolving to an empty set
  11  an unknown container name fails loudly
  12  two targets at once is rejected
  13  --github resolves the sessions carrying that ccy-github label
  14  an unknown --github value fails loudly
  15  no pre-rename podman-freeze is left on PATH

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
echo "Plan 00079 — acceptance: podfreeze"
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

# The selection/labelling unit test runs first: if that logic is broken there is
# no point manufacturing containers to discover it more slowly, and its failure
# output points at a function rather than at a symptom.
if [ ! -x "$PLAN_DIR/unit-test-selection.bash" ]; then
    echo "ERROR: unit-test-selection.bash is missing or not executable." >&2
    exit 1
fi
echo "### 0. selection/labelling unit test"
if "$PLAN_DIR/unit-test-selection.bash" > /dev/null; then
    echo "  OK — unit test passes (its own log is in logs/)"
else
    echo "ERROR: the selection unit test FAILED — not proceeding to containers." >&2
    echo "  See $PLAN_DIR/logs/unit-test-selection.log" >&2
    exit 1
fi
echo

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
# WHICH IMAGE IS NO LONGER LOAD-BEARING, and that is worth recording because it
# was, twice over. The first version of this script PREFERRED a claude-yolo
# image (certain to have a shell), which made check 9 — "the throwaway is
# excluded from --ccy" — fail against a tool behaving perfectly: back then --ccy
# keyed on the inherited claude-yolo-version image label, so the throwaway
# genuinely WAS in the set. A fixture violating its own precondition reports a
# defect that is not there, which is the shape of wrong answer this whole plan
# is about. The second version partitioned images on that label and preferred
# the unlabelled ones.
#
# Both are now unnecessary: the tool identifies a CCY session by the RUN-TIME
# ccy=true label or the session name pattern, and consults the image label
# nowhere. A throwaway named podfreeze-acceptance-<pid> has neither, whatever
# image it came from, so check 9 asserts exclusion unconditionally.
if ! images="$(podman images --format '{{.Repository}}:{{.Tag}}' 2>&1)"; then
    echo "ERROR: podman images failed: $images" >&2
    exit 1
fi

CANDIDATES=()
while read -r img; do
    case "$img" in
        "" | *"<none>"*) continue ;;
    esac
    CANDIDATES+=("$img")
done <<< "$images"

if [ "${#CANDIDATES[@]}" -eq 0 ]; then
    echo "ERROR: no local container images to build a throwaway container from." >&2
    echo "  Pull any small image and re-run, e.g.: podman pull alpine" >&2
    exit 1
fi

STARTED=0
for img in "${CANDIDATES[@]:0:5}"; do
    if out="$(podman run --detach --name "$CNAME" --network "$NET" \
        --entrypoint sh "$img" -c 'while true; do sleep 1; done' 2>&1)"; then
        echo "  started $CNAME from $img"
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

echo "### 5. freeze --network freezes it"
if freeze_out="$("$TOOL" freeze --network "$NET" 2>&1)"; then
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
if again_out="$("$TOOL" freeze --network "$NET" 2>&1)"; then
    case "$again_out" in
        *"Nothing to do"*) ok "reported nothing to do, exit 0" ;;
        *) bad "exit 0 but without the no-op message: $again_out" ;;
    esac
else
    bad "a second freeze failed instead of being a no-op: $again_out"
fi

echo "### 8. thaw by name unfreezes it"
if thaw_out="$("$TOOL" thaw "$CNAME" 2>&1)"; then
    if [ "$(state_of "$CNAME")" = "running" ]; then
        ok "$CNAME is running again"
    else
        bad "thaw reported success but the state is $(state_of "$CNAME")"
    fi
else
    bad "thaw exited non-zero: $thaw_out"
fi

echo "### 8b. no verb toggles: running -> frozen"
# The headline behaviour of the derived verb. Deliberately checked in BOTH
# directions from the same command, because "it froze" and "it toggles" are
# different claims and only the second one is the feature.
if toggle_out="$("$TOOL" --network "$NET" 2>&1)"; then
    if [ "$(state_of "$CNAME")" = "paused" ]; then
        ok "no verb froze the running container"
    else
        bad "no verb left it in state $(state_of "$CNAME"), expected paused"
    fi
else
    bad "no-verb invocation exited non-zero: $toggle_out"
fi

echo "### 8c. no verb toggles back: frozen -> running"
if toggle_out="$("$TOOL" --network "$NET" 2>&1)"; then
    if [ "$(state_of "$CNAME")" = "running" ]; then
        ok "the same command thawed it again"
    else
        bad "no verb left it in state $(state_of "$CNAME"), expected running"
    fi
else
    bad "no-verb invocation exited non-zero: $toggle_out"
fi

echo "### 9. freeze --ccy -n resolves the live CCY group"
# Asserted as a CONTRACT: every running Claude SESSION must appear, and the
# throwaway must not.
#
# The expected set is built here from podman directly — every running container
# carrying the run-time ccy=true label that CCY >= 3.40.0 sets. Deliberately NOT
# the inherited claude-yolo-version image label: that marks a lineage rather than
# a session, and using it here is what made an earlier run of this gate report a
# defect the tool did not have.
#
# Scoped to status=running on purpose. A CCY container that is already paused
# would appear in the tool's output under "Skipped — not currently running",
# so the check would pass without the resolver having selected it — a pass
# earned by a substring rather than by the behaviour being tested.
if ! ccy_live="$(podman ps --filter label=ccy=true \
    --filter status=running --format '{{.Names}}' 2>&1)"; then
    bad "could not list CCY containers: $ccy_live"
elif [ -z "$ccy_live" ]; then
    skip "no ccy=true container is running — nothing to resolve. Sessions started
         by a CCY older than 3.40.0 carry no such label; relaunch one to exercise
         this check"
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
        bad "--ccy omitted running CCY session(s):$missing"
    else
        ok "every running ccy=true session is in the --ccy set"
    fi
    case "$ccy_out" in
        *"$CNAME"*) bad "--ccy wrongly included the non-CCY throwaway $CNAME" ;;
        *) ok "the non-CCY throwaway is excluded" ;;
    esac
else
    bad "freeze --ccy --dry-run exited non-zero: $ccy_out"
fi
if [ "$(state_of "$CNAME")" = "running" ]; then
    ok "nothing was frozen by the --ccy dry run"
else
    bad "the --ccy dry run changed a container state"
fi

echo "### 9b. --ccy also resolves UNLABELLED (pre-3.40.0) sessions"
# Check 9 builds its expected set from `label=ccy=true` alone, so it exercises
# only the labelled path. On this host 4 of 6 live sessions predate 3.40.0 and
# carry no label at all — the MAJORITY of the real population reaches --ccy via
# podfreeze's name-pattern fallback, and check 9 is silent on whether that
# fallback works. If the pattern broke, check 9 would still report OK.
#
# That is the same defect this plan's own triage probe had (Plan 00080, P4):
# verify the labelled path, stay quiet about the one carrying most of the load.
# So assert the fallback directly, from the pattern podfreeze itself uses.
#
# The labelled set is recomputed here rather than reusing check 9's $ccy_live:
# that variable holds podman's ERROR TEXT when its call failed, and matching
# names against an error string would silently misclassify every session.
CCY_NAME_PATTERN='^.+_(yolo|browser)(_[0-9]+)?$'
if ! labelled_now="$(podman ps --filter label=ccy=true \
    --filter status=running --format '{{.Names}}' 2>&1)"; then
    bad "could not list labelled CCY containers: $labelled_now"
elif ! all_running="$(podman ps --filter status=running --format '{{.Names}}' 2>&1)"; then
    bad "could not list running containers: $all_running"
else
    unlabelled=""
    while read -r name; do
        if [ -z "$name" ] || ! [[ "$name" =~ $CCY_NAME_PATTERN ]]; then
            continue
        fi
        if ! printf '%s\n' "$labelled_now" | grep -qx -- "$name"; then
            unlabelled="$unlabelled $name"
        fi
    done <<< "$all_running"

    if [ -z "$unlabelled" ]; then
        # Not a skip: the labelled path IS fully covered by check 9 in this
        # state, so there is no gap to report. Say which state we are in.
        ok "every live session carries a label — fallback path not exercisable here"
    elif ccy_fb_out="$("$TOOL" freeze --ccy --dry-run 2>&1)"; then
        fb_missing=""
        for name in $unlabelled; do
            case "$ccy_fb_out" in
                *"$name"*) ;;
                *) fb_missing="$fb_missing $name" ;;
            esac
        done
        if [ -n "$fb_missing" ]; then
            bad "--ccy omitted unlabelled session(s) the name pattern should catch:$fb_missing"
        else
            ok "unlabelled sessions resolve via the name fallback:$unlabelled"
        fi
    else
        bad "freeze --ccy --dry-run exited non-zero: $ccy_fb_out"
    fi
fi

echo "### 10. an unknown network fails loudly"
if unknown_out="$("$TOOL" freeze --network "no-such-network-$$" 2>&1)"; then
    bad "an unknown network resolved to an empty set and exited 0"
else
    case "$unknown_out" in
        *"no such network"*) ok "refused, naming the missing network" ;;
        *) bad "failed, but not with the expected message: $unknown_out" ;;
    esac
fi

echo "### 11. an unknown container name fails loudly"
if unknown_out="$("$TOOL" freeze "no-such-container-$$" 2>&1)"; then
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

echo "### 13. the identity axes resolve against the live labels"
# Only the GitHub axis is asserted here: the token label is a private
# identifier and this gate's log, while gitignored, is still a file — naming
# every token on the machine in it buys nothing the GitHub axis does not
# already prove, since all three axes share one code path (identity_names).
if ! gh_values="$(podman ps --filter label=ccy=true \
    --format '{{index .Labels "ccy-github"}}' 2>&1)"; then
    bad "could not read ccy-github labels: $gh_values"
else
    gh_one=""
    while read -r value; do
        if [ -n "$value" ] && [ "$value" != "none" ]; then
            gh_one="$value"
            break
        fi
    done <<< "$gh_values"

    if [ -z "$gh_one" ]; then
        skip "no running session carries a ccy-github label — relaunch a session
         under CCY 3.40.0 or later to exercise this check"
    elif gh_out="$("$TOOL" freeze --github "$gh_one" --dry-run 2>&1)"; then
        # Asserted against podman, not against the tool's own notion of the set.
        if ! gh_expected="$(podman ps --filter label=ccy=true \
            --filter "label=ccy-github=$gh_one" --filter status=running \
            --format '{{.Names}}' 2>&1)"; then
            bad "could not list sessions for that account: $gh_expected"
        else
            gh_missing=""
            while read -r name; do
                if [ -z "$name" ]; then
                    continue
                fi
                case "$gh_out" in
                    *"$name"*) ;;
                    *) gh_missing="$gh_missing $name" ;;
                esac
            done <<< "$gh_expected"
            if [ -n "$gh_missing" ]; then
                bad "--github omitted session(s):$gh_missing"
            else
                ok "--github resolves every session for that account"
            fi
        fi
        case "$gh_out" in
            *"$CNAME"*) bad "--github wrongly included the throwaway $CNAME" ;;
            *) ok "the unlabelled throwaway is excluded" ;;
        esac
    else
        bad "freeze --github exited non-zero: $gh_out"
    fi
fi

echo "### 14. an unknown identity value fails loudly"
if id_out="$("$TOOL" freeze --github "no-such-account-$$" --dry-run 2>&1)"; then
    bad "an unknown --github value resolved to an empty set and exited 0"
else
    case "$id_out" in
        *"no running or frozen CCY session has --github"*)
            ok "refused, naming the unknown account" ;;
        *) bad "failed, but not with the expected message: $id_out" ;;
    esac
fi

echo "### 15. exactly one build of this tool is installed"
# deploy.bash removes the pre-rename `podman-freeze` and prints when it does.
# On run 2 it printed nothing, which SHOULD mean the file was already gone —
# but silence cannot distinguish "already clean" from "looked in the wrong
# place", and this repo has been bitten by exactly that reading. So the end
# state is asserted here rather than inferred from the absence of a message.
#
# PATH, not one hardcoded directory: two builds are a problem because the one
# you get depends on which name you type, and that is a PATH question.
if stale="$(command -v podman-freeze)"; then
    bad "the pre-rename binary is still on PATH at $stale — two builds of one
       tool, and which you get depends on the name you type. Re-run deploy.bash"
else
    ok "no pre-rename podman-freeze on PATH"
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

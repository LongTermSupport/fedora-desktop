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
# STANDARD-EXCEPTION(R1): this gate runs without errexit (see the `set` line above) because
# every check records a PASS/FAIL and continues, so library calls that RETURN 1 are gated here.
plan_init "${BASH_SOURCE[0]}" || exit 1

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00079 — acceptance gate for podfreeze (HOST ONLY)

Usage: acceptance.bash [--help]

First confirms the deployed podfreeze and freeze library match the repo (each a
counted check; a mismatch stops the run). Then it runs the selection unit test (no
podman needed), creates a throwaway container on a throwaway network, and checks:

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
 13b  an identity selection DISCLOSES the unlabelled sessions it cannot cover,
      on stderr only
  14  an unknown --github value fails loudly
  15  no pre-rename podman-freeze is left on PATH

Writes its run log under untracked/plan-runs/, and names the exact path on the
way out. The throwaway container and network are removed on exit, including on
failure.
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

plan_mode gather || exit 1

# This gate needs the HOST's podman (CLAUDE/ContainerRules.md).
plan_require_host "it exercises the deployed podfreeze against real host containers" || exit 1

plan_start_log auto || exit 1

TOOL="$HOME/.local/bin/podfreeze"
REPO_TOOL="$PLAN_REPO_ROOT/files/home/.local/bin/podfreeze"
# Half of podfreeze is this library (tasks/deploy-freeze-lib.yml deploys it), so the
# deployed-matches-repo check covers both files, not only the entry script.
LIB="$HOME/.local/lib/freeze/freeze-common.bash"
REPO_LIB="$PLAN_REPO_ROOT/files/home/.local/lib/freeze/freeze-common.bash"
NET="podfreeze-acceptance-net-$$"
CNAME="podfreeze-acceptance-$$"

# The fallback podfreeze uses to recognise a session started before CCY 3.40.0.
# Defined once here rather than inside a check, so no check depends on another
# having run — that coupling is how a skipped branch silently becomes a PASS.
CCY_NAME_PATTERN='^.+_(yolo|browser)(_[0-9]+)?$'

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
if [ ! -f "$LIB" ]; then
    echo "ERROR: the freeze library $LIB is not deployed; podfreeze cannot start without it." >&2
    echo "  Run this plan's deploy.bash." >&2
    exit 1
fi
if ! cmp -s "$REPO_LIB" "$LIB"; then
    echo "ERROR: the deployed freeze library differs from the repo copy." >&2
    echo "  Deployed: $LIB" >&2
    echo "  Repo:     $REPO_LIB" >&2
    echo "  Run this plan's deploy.bash — the tool's selection logic lives in this file." >&2
    exit 1
fi
echo "### deployed copies"
ok "the deployed podfreeze matches the repo copy"
ok "the deployed freeze library matches the repo copy"
echo

# The selection/labelling unit test runs first: if that logic is broken there is
# no point manufacturing containers to discover it more slowly, and its failure
# output points at a function rather than at a symptom. It is the repo's own
# scripts/test-podfreeze.bash, the qa-all.bash gate that covers every function this
# plan's former plan-local copy did, so there is one suite to keep current, not two.
PODFREEZE_TEST="$PLAN_REPO_ROOT/scripts/test-podfreeze.bash"
if [ ! -f "$PODFREEZE_TEST" ]; then
    echo "ERROR: $PODFREEZE_TEST is missing." >&2
    exit 1
fi
echo "### 0. selection/labelling unit test (scripts/test-podfreeze.bash)"
# The suite prints PASS and FAIL alike on stdout, so its output is captured, not dropped:
# a passing run stays short, and a failing one prints every line, so the failing cases
# reach the terminal and this script's run log.
if podfreeze_out="$(bash "$PODFREEZE_TEST" 2>&1)"; then
    podfreeze_count="$(printf '%s\n' "$podfreeze_out" | awk '/^passed:/ {print; exit}')"
    if [ -z "$podfreeze_count" ]; then
        echo "ERROR: the podfreeze suite passed but printed no 'passed:' line — its output format changed." >&2
        exit 1
    fi
    ok "podfreeze decision tests pass (${podfreeze_count})"
else
    printf '%s\n' "$podfreeze_out" >&2
    echo "ERROR: the podfreeze decision tests FAILED (above) — not proceeding to containers." >&2
    echo "  Re-run: bash $PODFREEZE_TEST" >&2
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
# R4: register the teardown, never `trap … EXIT`. A hand-written EXIT trap REPLACES
# the library's handler, and the run log then loses its final buffered chunk — the
# lines written as the run was dying, which are the ones that matter.
plan_on_cleanup cleanup || exit 1

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
ccy_dry_ran=0
if ! ccy_live="$(podman ps --filter label=ccy=true \
    --filter status=running --format '{{.Names}}' 2>&1)"; then
    bad "could not list CCY containers: $ccy_live"
elif [ -z "$ccy_live" ]; then
    skip "no ccy=true container is running — nothing to resolve. Sessions started
         by a CCY older than 3.40.0 carry no such label; relaunch one to exercise
         this check"
elif ccy_out="$("$TOOL" freeze --ccy --dry-run 2>&1)"; then
    ccy_dry_ran=1
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
    ccy_dry_ran=1
    bad "freeze --ccy --dry-run exited non-zero: $ccy_out"
fi
# Judged only when the dry run was actually invoked: after the skip or the listing
# failure above, the throwaway is still running because nothing ran, and an ok()
# there would count a pass for an assertion that was never exercised.
if [ "$ccy_dry_ran" = "1" ]; then
    if [ "$(state_of "$CNAME")" = "running" ]; then
        ok "nothing was frozen by the --ccy dry run"
    else
        bad "the --ccy dry run changed a container state"
    fi
fi

echo "### 9b. --ccy also resolves UNLABELLED (pre-3.40.0) sessions"
# Check 9 builds its expected set from `label=ccy=true` alone, so it exercises
# only the labelled path. A session started before CCY 3.40.0 carries no label and
# reaches --ccy only through podfreeze's name-pattern fallback, on which check 9 is
# silent. If the pattern broke, check 9 would still report OK.
#
# That is the same defect this plan's own triage probe had (Plan 00080, P4):
# verify the labelled path, stay quiet about the one carrying most of the load.
# So assert the fallback directly, from the pattern podfreeze itself uses.
#
# The labelled set is recomputed here rather than reusing check 9's $ccy_live:
# that variable holds podman's ERROR TEXT when its call failed, and matching
# names against an error string would silently misclassify every session.
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
        # skip(), not ok(): this branch never invokes the tool, so an ok() would count
        # a pass for an assertion that did not run (the rule checks 13 and 13b follow).
        skip "every live session carries a label — the name fallback was not exercised"
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
        elif [ -z "$gh_expected" ]; then
            # `gh_one` is chosen from a query that includes PAUSED sessions,
            # while this expectation narrows to running — so the only session
            # carrying a github label being paused (entirely normal: pausing
            # them is this tool's job) leaves the expected set empty. The loop
            # below would then assert over a population of zero and print a
            # pass. Check 9 guards its upstream emptiness and this guards the
            # downstream one.
            skip "the only session(s) for that account are paused, so the
         running expectation is empty — nothing to assert. Thaw one and re-run"
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

echo "### 13b. an identity selection discloses the sessions it cannot consider"
# An unlabelled session is in --ccy but in NO identity group, because its
# account genuinely cannot be inferred. The selection is therefore correct as
# far as it can go — and silently narrower than "every session for account X",
# which is what the user asked the axis for.
#
# `select_identity` already refuses the case where NOTHING is labelled. This
# asserts the same disclosure for the PARTIAL case (F19's shape again), which is
# the state the machine is actually in until every session has been relaunched.
# The NOTE goes to stderr, so it never pollutes a captured dry-run set.
# `unlabelled` is recomputed rather than reused from check 9b: if 9b took an
# early failure branch that variable is unset, and `${unlabelled:-}` would then
# read as "nothing to disclose" and report a PASS this check never earned.
# Streams are split with a temp file, not `2>/dev/null` — this repo treats a
# discarded stderr as error-hiding, and here the stderr IS the thing under test.
id_unlabelled=""
if ! id_all="$(podman ps --filter status=running --format '{{.Names}}' 2>&1)"; then
    bad "could not list running containers: $id_all"
elif ! id_labelled="$(podman ps --filter label=ccy=true \
    --filter status=running --format '{{.Names}}' 2>&1)"; then
    bad "could not list labelled CCY containers: $id_labelled"
else
    while read -r name; do
        if [ -z "$name" ] || ! [[ "$name" =~ $CCY_NAME_PATTERN ]]; then
            continue
        fi
        if ! printf '%s\n' "$id_labelled" | grep -qx -- "$name"; then
            id_unlabelled="$id_unlabelled $name"
        fi
    done <<< "$id_all"

    # Both of these are skip(), not ok(). Neither branch invokes the tool, so an
    # ok() here would increment PASS over an assertion that never ran and the
    # closing "N checks passed" would over-count. Check 13's equivalent branch was
    # changed to skip() for exactly this reason and these two were left behind.
    if [ -z "${gh_one:-}" ]; then
        skip "no labelled account to select — disclosure path not exercisable here"
    elif [ -z "$id_unlabelled" ]; then
        # Scoped to running, while the tool's own NOTE lists unlabelled CCY
        # sessions in EITHER state — so this can only claim what it measured.
        skip "no unlabelled RUNNING session — nothing to disclose in this state"
    else
        note_err_file="$(mktemp)"
        if ! note_out="$("$TOOL" freeze --github "$gh_one" --dry-run 2>"$note_err_file")"; then
            bad "freeze --github exited non-zero: $(cat "$note_err_file")"
        else
            note_err="$(cat "$note_err_file")"
            case "$note_err" in
                *"carry no identity labels"*)
                    ok "the NOTE names the sessions the identity axis cannot cover" ;;
                *) bad "no disclosure of unlabelled session(s):$id_unlabelled" ;;
            esac
            # The NOTE must be stderr-only — a caller doing
            # `names=$(podfreeze freeze --github X --dry-run)` must not get prose.
            case "$note_out" in
                *"carry no identity labels"*)
                    bad "the NOTE leaked onto stdout, polluting a captured set" ;;
                *) ok "the NOTE is on stderr only; stdout stays the payload" ;;
            esac
        fi
        rm -f "$note_err_file"
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
# play-podfreeze.yml removes the pre-rename `podman-freeze`. A quiet run cannot
# distinguish "already clean" from "looked in the wrong place", so the end state
# is asserted here rather than inferred from the absence of a message.
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
# COVERAGE, stated as numbers. A verdict line reads identically whether the
# checks covered six sessions or one, so "PASS — 24 checks" is not on its own a
# statement about the population. This repo's own rule
# (CLAUDE/AgentNotes.md, "A partial result read as a complete one") asks for an
# `n of m`; Plan 00080's triage.bash applies it and this gate did not — the same
# rule landing in one file of a pair, precisely the recurrence the note names.
#
# Counted in a loop rather than with `grep -c .`, which exits 1 on zero matches:
# surviving `set -e` would then need the error-suppressing idiom this repo
# blocks, and AgentNotes names that exact reintroduction.
count_matching() {
    local pattern="$1" input="$2" line n=0
    while read -r line; do
        if [ -n "$line" ] && [[ "$line" =~ $pattern ]]; then
            n=$(( n + 1 ))
        fi
    done <<< "$input"
    printf '%s' "$n"
}

cov_sessions="?"
cov_labelled="?"
if cov_all="$(podman ps --filter status=running --format '{{.Names}}' 2>&1)"; then
    cov_sessions="$(count_matching "$CCY_NAME_PATTERN" "$cov_all")"
fi
if cov_lab="$(podman ps --filter label=ccy=true --filter status=running \
    --format '{{.Names}}' 2>&1)"; then
    cov_labelled="$(count_matching '.' "$cov_lab")"
fi
echo "COVERAGE: the live-fleet checks (9, 9b, 13, 13b) ran against"
echo "  $cov_sessions running CCY session(s), of which $cov_labelled carry ccy=true."
echo "  A fleet of 1 and a fleet of 6 give the same verdict line; this does not."
echo "--------------------------------------------------------------"
if [ "$FAIL" -eq 0 ]; then
    echo "VERDICT: PASS — $PASS check(s) passed, $SKIP skipped."
else
    echo "VERDICT: FAIL — $FAIL of $((PASS + FAIL)) check(s) failed, $SKIP skipped."
fi
echo "=============================================================="

# The script's status IS this test — deliberately not `exit 0`/`exit 1`. The
# linter cannot trace the teardown edge out of a terminal `exit` node, so an
# explicit exit here makes it report the whole cleanup() body as unreachable
# (SC2317). Leaving the final command as the verdict test gives the same exit
# status with no suppression annotation.
[ "$FAIL" -eq 0 ]

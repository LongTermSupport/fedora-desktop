#!/usr/bin/env bash
#
# Plan 00079 — unit test for podfreeze's selection and labelling logic.
# Runs ANYWHERE, including inside the CCY container: it needs no podman.
#
# WHY THIS EXISTS ALONGSIDE acceptance.bash:
#
# acceptance.bash proves the tool works end-to-end on the HOST, but it can only
# reach the states the host happens to be in. The logic most likely to be
# subtly wrong — splitting a comma-separated network list, a container on two
# networks, a group whose members are half frozen, an explicit verb overriding
# the derived one — needs states that would be tedious and slow to manufacture
# with real containers, and some of them (an empty CCY group) cannot be made on
# a machine that is running CCY sessions at all.
#
# So this drives the functions directly against a synthetic inventory. It reads
# the REAL script and sources its function region verbatim, cut at the
# "Argument parsing" marker so the main flow does not execute. It is not a copy:
# if a function changes, this runs the changed bytes.
#
# Usage: unit-test-selection.bash [--help]

set -uo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00079 — unit test for podfreeze selection/labelling (no podman needed)

Usage: unit-test-selection.bash [--help]

Sources podfreeze's function region verbatim and drives it against a synthetic
container inventory shaped like the real host: CCY sessions on the default
network, one of them also on an app network, an app stack, a container on two
networks, one with no network, and one already frozen.

Covers ccy_names, build_network_map, network_names, count_in_state,
target_effect (both the derived-verb rule and an explicit verb overriding it),
infer_action, and the selectors. Writes its log to this plan's logs/.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: unit-test-selection.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/unit-test-selection.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

TOOL="$REPO_ROOT/files/home/.local/bin/podfreeze"
if [ ! -f "$TOOL" ]; then
    echo "ERROR: $TOOL not found." >&2
    exit 1
fi

# Cut at the marker rather than at a line number, so this does not rot as the
# script grows. A missing marker is a hard failure: silently sourcing the whole
# file would run the main flow.
CUT=""
if ! CUT="$(grep -n '^# Argument parsing$' "$TOOL")"; then
    echo "ERROR: could not find the '# Argument parsing' marker in $TOOL." >&2
    echo "  This test cuts the file there to source functions without running" >&2
    echo "  main. Restore the marker, or update this test deliberately." >&2
    exit 1
fi
CUT_LINE="${CUT%%:*}"

FUNCS="$PLAN_DIR/logs/.podfreeze-funcs.bash"
awk -v n="$CUT_LINE" 'NR < n - 2' "$TOOL" > "$FUNCS"

ACTION=""
# shellcheck source=/dev/null
source "$FUNCS"

FAILED=0
is() {
    local label="$1" got="$2" want="$3"
    if [ "$got" = "$want" ]; then
        printf '  ok    %-46s %s\n' "$label" "$got"
    else
        printf '  FAIL  %-46s got [%s] want [%s]\n' "$label" "$got" "$want"
        FAILED=$((FAILED + 1))
    fi
}

echo "=============================================================="
echo "Plan 00079 — podfreeze selection/labelling unit test"
echo "=============================================================="
echo

INV_NAME=(proj_yolo proj_yolo_2 app-db app-web lone old_browser)
INV_STATE=(running paused running running running running)
INV_NETS=(podman podman "appnet" "appnet,podman" none podman)
INV_IS_CCY=([proj_yolo]=1 [proj_yolo_2]=1 [app-db]=0 [app-web]=0 [lone]=0 [old_browser]=1)

# Identity labels, populated only for CCY sessions — the shape load_inventory
# produces. old_browser deliberately has NONE: it stands for a session started
# by a CCY older than 3.40.0, which is selectable by name but carries no labels,
# and it is the case that would otherwise be silently dropped from a group.
# proj_yolo_2 shares the GitHub account with proj_yolo but uses a different
# token, so the two axes cannot both be satisfied by the same partition — a
# fixture where every axis agrees proves nothing about any of them.
INV_GITHUB=([proj_yolo]=octocat [proj_yolo_2]=octocat)
INV_TOKEN=([proj_yolo]=personal [proj_yolo_2]=work)
INV_SSHKEYS=([proj_yolo]="github_personal" [proj_yolo_2]="github_work github_personal")

echo "### fixture inventory (${#INV_NAME[@]} containers, explicit verb: ${ACTION:-<none>})"
for i in "${!INV_NAME[@]}"; do
    printf '  %-14s %-8s %-6s %-14s gh=%-9s token=%-9s keys=%s\n' \
        "${INV_NAME[$i]}" "${INV_STATE[$i]}" \
        "$([ "${INV_IS_CCY[${INV_NAME[$i]}]}" = "1" ] && echo CCY || echo -)" \
        "${INV_NETS[$i]}" \
        "${INV_GITHUB[${INV_NAME[$i]}]:--}" \
        "${INV_TOKEN[${INV_NAME[$i]}]:--}" \
        "${INV_SSHKEYS[${INV_NAME[$i]}]:--}"
done
echo

echo "### ccy_names"
is "ccy set" "$(ccy_names | tr '\n' ',')" "proj_yolo,proj_yolo_2,old_browser,"

echo "### build_network_map — comma split, multi-network, 'none' skipped"
build_network_map
is "network count" "${#NET_MEMBERS[@]}" "2"
is "podman members" "$(network_names podman | sort | tr '\n' ',')" \
    "app-web,old_browser,proj_yolo,proj_yolo_2,"
is "appnet members" "$(network_names appnet | sort | tr '\n' ',')" "app-db,app-web,"
is "networkless container excluded" "$(network_names podman | grep -c '^lone$')" "0"

echo "### count_in_state"
is "running in podman" "$(count_in_state running app-web old_browser proj_yolo proj_yolo_2)" "3"
is "frozen in podman" "$(count_in_state paused app-web old_browser proj_yolo proj_yolo_2)" "1"
is "unknown name ignored" "$(count_in_state running ghost)" "0"

echo "### target_effect — derived verb (the toggle rule)"
is "mixed group sizes the running" "$(target_effect proj_yolo proj_yolo_2 old_browser)" "FREEZE 2"
is "all-frozen group thaws" "$(target_effect proj_yolo_2)" "THAW   1"
is "empty group" "$(target_effect)" "nothing to do"

echo "### target_effect — an explicit verb overrides the rule"
ACTION="thaw"
echo "  (ACTION=$ACTION)"
is "thaw sizes the frozen only" "$(target_effect proj_yolo proj_yolo_2 old_browser)" "THAW   1"
is "thaw with none frozen" "$(target_effect proj_yolo)" "nothing to thaw"
ACTION="freeze"
is "freeze with none running" "$(target_effect proj_yolo_2)" "nothing to freeze"
ACTION=""

echo "### infer_action agrees with the labels above"
SELECTED=(proj_yolo proj_yolo_2)
is "any running -> freeze" "$(infer_action)" "freeze"
SELECTED=(proj_yolo_2)
is "none running -> thaw" "$(infer_action)" "thaw"
SELECTED=()
is "empty -> thaw" "$(infer_action)" "thaw"

echo "### identity axes — distinct values come only from labelled sessions"
is "github values" "$(identity_values github | tr '\n' ',')" "octocat,"
is "token values" "$(identity_values token | tr '\n' ',')" "personal,work,"
is "ssh-key values (multi-value split)" "$(identity_values ssh-key | tr '\n' ',')" \
    "github_personal,github_work,"

echo "### identity_names — whole-value match, and word match for ssh-key"
is "github octocat" "$(identity_names github octocat | tr '\n' ',')" \
    "proj_yolo,proj_yolo_2,"
is "token work" "$(identity_names token work | tr '\n' ',')" "proj_yolo_2,"
is "ssh-key shared by two" "$(identity_names ssh-key github_personal | tr '\n' ',')" \
    "proj_yolo,proj_yolo_2,"
is "ssh-key held by one" "$(identity_names ssh-key github_work | tr '\n' ',')" \
    "proj_yolo_2,"
# The bug this guards: a substring match would make github_work select the
# container whose label is "github_work github_personal" AND anything whose
# label merely contains that text.
is "ssh-key prefix is not a match" "$(identity_names ssh-key github | tr '\n' ',')" ""
is "unlabelled session is in no identity group" \
    "$(identity_names github octocat | grep -c '^old_browser$')" "0"

echo "### identity_matches — the two comparison modes"
is "single-valued axis compares whole" \
    "$(identity_matches github octocat "octocat-two" && echo yes || echo no)" "no"
is "ssh-key matches one word of many" \
    "$(identity_matches ssh-key github_work "github_work github_personal" \
        && echo yes || echo no)" "yes"
is "empty value never matches" \
    "$(identity_matches github octocat "" && echo yes || echo no)" "no"

echo "### an unknown identity value fails loudly, never as an empty set"
# Run in a subshell: select_identity exits, which is the behaviour under test.
if unknown_err="$( (select_identity github nobody) 2>&1 )"; then
    is "unknown --github exits non-zero" "exit 0" "non-zero exit"
else
    is "unknown --github exits non-zero" "non-zero exit" "non-zero exit"
fi
case "$unknown_err" in
    *"no running or frozen CCY session has --github 'nobody'"*)
        is "…and says so" "named the value" "named the value" ;;
    *) is "…and says so" "$unknown_err" "named the value" ;;
esac
case "$unknown_err" in
    *octocat*) is "…and lists the known values" "listed" "listed" ;;
    *) is "…and lists the known values" "$unknown_err" "listed" ;;
esac

echo "### selectors"
select_ccy
is "select_ccy" "${SELECTED[*]}" "proj_yolo proj_yolo_2 old_browser"
select_all
is "select_all" "${#SELECTED[@]}" "6"
select_identity github octocat
is "select_identity github" "${SELECTED[*]}" "proj_yolo proj_yolo_2"
select_identity ssh-key github_work
is "select_identity ssh-key" "${SELECTED[*]}" "proj_yolo_2"
# A group whose members are half frozen must still size correctly — this is the
# same toggle rule as above, reached through an identity group.
mapfile -t gh_group < <(identity_names github octocat)
is "identity group sizes the running" \
    "$(target_effect "${gh_group[@]}")" "FREEZE 1"

echo
echo "=============================================================="
if [ "$FAILED" -eq 0 ]; then
    echo "VERDICT: PASS"
else
    echo "VERDICT: FAIL — $FAILED assertion(s)"
fi
echo "=============================================================="

[ "$FAILED" -eq 0 ]

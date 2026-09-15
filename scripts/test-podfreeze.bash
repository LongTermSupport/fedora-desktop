#!/usr/bin/env bash
# Unit-test podfreeze's decisions (Plan 00122 Task 4.1).
#
# WRITTEN BEFORE THE EXTRACTION, DELIBERATELY. Task 4.2 lifts these decisions and the menu
# layer out of `podfreeze` into a library both freeze tools source. A suite written after
# that move would only prove the refactor agrees with itself; this one states what the tool
# does TODAY, so the extraction has something to disagree with. `git diff` must therefore
# touch no line of `files/home/.local/bin/podfreeze` — it is pinned AS SHIPPED, not adapted
# for testing.
#
# HOW IT IS SOURCED, AND WHY NOT WHOLE. Unlike `lxcfreeze`, `podfreeze` has no
# `BASH_SOURCE[0] == $0` dispatch guard: its argument loop and main body run at top level,
# so sourcing the file would parse THIS suite's arguments and die in the container guard.
# Only the DEFINITIONS are sourced — everything above the argument loop, found by matching
# that loop's own line rather than by a line number, which would slide onto the wrong side
# of the boundary as the tool grows and silently start sourcing real execution. A rename of
# the loop fails the extraction loudly instead.
#
# WHAT IS UNDER TEST IS THE DECISION, NOT THE QUERY. podfreeze keeps its inventory in
# globals, so the fixtures below populate those directly and call the functions — no podman
# is involved and none is needed. The two functions with no such seam (`load_inventory`,
# `select_network`) do query podman, and a `podman` shell function stubs them; that is the
# same collaborator-stubbing scripts/test-vmtest-reboot-dispatch.bash does. This container
# cannot do better: podfreeze refuses to run inside one by design, because the host's podman
# is unreachable from in here and an empty answer would be a confident lie.
#
# WHAT THAT LEAVES UNCOVERED, stated rather than glossed:
#   - Whether `podman ps --format` emits the record shape the stub feeds the parser. Only a
#     host can say, and Plan 00122 Task 4.6 is where it gets said.
#   - `pick_target` and `drill_into_group`: both die without a TTY, and their numbered
#     branches wrap decisions that ARE covered (`target_effect`, `row_verb`).
#   - `do_action`, `do_list`, `print_table`. `do_action` is the half that calls `podman
#     pause`; its act/skip/vanished split deserves a suite of its own, out of scope here.
#
# `set -e` is deliberately NOT in this file's own prelude: every case must run so the
# summary reports the full picture. Sourcing the tool brings ITS errexit in regardless —
# see the assertion below and the constraint it places on every negative case.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$REPO_ROOT/files/home/.local/bin/podfreeze"

if [ ! -f "$TOOL" ]; then
    echo "FAIL: podfreeze not found at $TOOL" >&2
    exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# ---------------------------------------------------------------------------
# Extract the definitions: everything above the tool's first executing statement.
# ---------------------------------------------------------------------------
BOUNDARY='while [ "$#" -gt 0 ]; do'

boundary_hits="$(awk -v cut="$BOUNDARY" '$0 == cut { n = n + 1 } END { print n + 0 }' "$TOOL")"
if [ "$boundary_hits" -ne 1 ]; then
    echo "FAIL: expected exactly one '$BOUNDARY' line in podfreeze, found $boundary_hits" >&2
    echo "      The boundary between its definitions and its main body is no longer" >&2
    echo "      identifiable, so sourcing it would run part of the tool." >&2
    exit 1
fi

DEFS="$work/podfreeze-definitions.bash"
awk -v cut="$BOUNDARY" '$0 == cut { exit } { print }' "$TOOL" > "$DEFS"

# A cut that landed inside a function leaves an unbalanced brace, and the symptom of that
# would be a confusing syntax error hundreds of lines into a sourced temp file. Parsing it
# first turns the same mistake into a named failure.
if ! bash -n "$DEFS"; then
    echo "FAIL: the extracted definitions do not parse — the boundary cut mid-construct" >&2
    exit 1
fi

# shellcheck source=/dev/null
source "$DEFS"

# podfreeze sets `-euo pipefail` at its top, and SOURCING IT APPLIES THAT TO THIS SHELL —
# so from here down this file runs under errexit, whatever its own prelude said. That is
# asserted rather than assumed, for two reasons:
#
#   1. The prelude arriving is itself worth checking. The fail-fast rule requires it, and
#      a tool that quietly lost it would still pass every case below.
#   2. It constrains how the cases are written, and the constraint is invisible. EVERY
#      case that drives a function to a deliberate non-zero MUST sit inside an `if`
#      condition or a `$( )` substitution, which errexit exempts. A bare call would abort
#      the run before the summary printed. That is what the `notok` helper below is for.
case "$-" in
    *e*) ;;
    *)
        echo "FAIL: sourcing $TOOL did not bring errexit with it" >&2
        echo "      (its 'set -euo pipefail' prelude is missing — a fail-fast violation)" >&2
        exit 1
        ;;
esac

# Every function relied on below must actually be defined. A main dispatch that leaked
# past the boundary would otherwise show up as this suite hanging on a prompt or dying in
# the container guard, rather than as a clean failure.
for fn in inventory_index_of ccy_marker ccy_names select_all select_ccy select_names \
    select_network build_network_map network_names count_in_state identity_value_of \
    identity_matches identity_values identity_names identity_axis_discriminates \
    select_identity unlabelled_ccy_names warn_identity_blind_spot target_effect \
    row_verb infer_action load_inventory; do
    if ! declare -F "$fn" > /dev/null; then
        echo "FAIL: $fn is not defined after sourcing podfreeze's definitions" >&2
        echo "      (the decision is absent, not merely wrong)" >&2
        exit 1
    fi
done

# The tool's own constants must have arrived too — several cases read them rather than
# repeating their values, so an absent one would silently weaken the assertion.
for const in FIELD_SEP ALL_ROW_KEY CCY_SESSION_LABEL CCY_NAME_PATTERN MAX_TRIES; do
    if [ -z "${!const:-}" ]; then
        echo "FAIL: $const is empty or unset after sourcing podfreeze's definitions" >&2
        exit 1
    fi
done

PASSED=0
FAILED=0

pass() {
    printf '  PASS  %s\n' "$1"
    PASSED=$((PASSED + 1))
}
fail() {
    printf '  FAIL  %s\n' "$1"
    shift
    local line
    for line in "$@"; do
        printf '        %s\n' "$line"
    done
    FAILED=$((FAILED + 1))
}

# eq <description> <actual> <expected>
eq() {
    if [ "$2" = "$3" ]; then
        pass "$1"
    else
        fail "$1" "got:  $(printf '%q' "$2")" "want: $(printf '%q' "$3")"
    fi
}

# contains <description> <haystack> <needle>
contains() {
    case "$2" in
        *"$3"*) pass "$1" ;;
        *) fail "$1" "wanted a mention of: $3" "got: $2" ;;
    esac
}

# ok / notok <description> <command...> — run a PREDICATE and assert its status.
#
# These exist for the errexit constraint above: the command runs as an `if` condition, so
# a deliberate non-zero cannot abort the run. Use them only for functions whose answer IS
# the exit status; anything that prints its answer goes through `eq` instead.
#
# Stdout is diverted to a file rather than left to reach the terminal. `inventory_index_of`
# prints an index when it succeeds, so a case that FAILED spliced that index onto the front
# of its own report line — a failure report that corrupts its own format, found while
# mutation-testing this suite. It is reported as a detail instead, which is where it
# belongs. Stderr is untouched, so a diagnostic still surfaces.
ok() {
    local desc="$1"
    shift
    if "$@" > "$work/predicate.out"; then
        pass "$desc"
    else
        fail "$desc" "expected success, got non-zero" \
            "it printed: $(cat "$work/predicate.out")"
    fi
}
notok() {
    local desc="$1"
    shift
    if "$@" > "$work/predicate.out"; then
        fail "$desc" "expected non-zero, but it succeeded" \
            "it printed: $(cat "$work/predicate.out")"
    else
        pass "$desc"
    fi
}

# lines_as_words <command...> — flatten a one-name-per-line emitter to one string, so the
# cases assert ORDER and not merely membership. Several of these emitters feed a menu
# whose row numbers the user types, so the order is part of the answer.
lines_as_words() {
    local -a got=()
    mapfile -t got < <("$@")
    printf '%s' "${got[*]-}"
}

# ---------------------------------------------------------------------------
# The fixture inventory.
#
# DELIBERATELY out of order, mixed-state, mixed-CCY and larger than two entries. A list
# that arrives sorted, or uniform, or two long, cannot falsify a decision that reads the
# wrong element or stops at the first one.
#
# The four CCY sessions are four DIFFERENT kinds on purpose, because the distinctions
# between them are exactly what an extraction loses silently:
#
#   zulu_yolo      CCY by NAME only — a session started before CCY 3.40.0, so it carries
#                  no labels at all. In --ccy, in no identity group.
#   alpha_yolo_2   labelled, and carries a value on all three identity axes.
#   bravo_browser  labelled, shares a token and an SSH key with alpha but not a GitHub
#                  account — so no single axis groups the same pair as another.
#   echo_yolo_3    labelled, but `none` on every axis. Its identity maps are empty EXACTLY
#                  as zulu_yolo's are; INV_HAS_LABELS is the only thing that tells the two
#                  apart, and telling zulu to relaunch is advice that works while telling
#                  echo to relaunch is advice that cannot.
#
# `mike` and `kilo` are not CCY at all. `mike` is running and sits at index 1, so a
# predicate that inspects only the first name it is handed gets several answers wrong.
# ---------------------------------------------------------------------------
fixture() {
    INV_NAME=(zulu_yolo mike alpha_yolo_2 kilo bravo_browser echo_yolo_3)
    INV_STATE=(paused running paused running running paused)
    INV_NETS=(podman none "podman, appnet" appnet podman none)

    INV_IS_CCY=()
    INV_IS_CCY["zulu_yolo"]=1
    INV_IS_CCY["mike"]=0
    INV_IS_CCY["alpha_yolo_2"]=1
    INV_IS_CCY["kilo"]=0
    INV_IS_CCY["bravo_browser"]=1
    INV_IS_CCY["echo_yolo_3"]=1

    INV_HAS_LABELS=()
    INV_HAS_LABELS["alpha_yolo_2"]=1
    INV_HAS_LABELS["bravo_browser"]=1
    INV_HAS_LABELS["echo_yolo_3"]=1

    INV_GITHUB=()
    INV_GITHUB["alpha_yolo_2"]=octo
    INV_GITHUB["bravo_browser"]=hubot

    INV_TOKEN=()
    INV_TOKEN["alpha_yolo_2"]=work
    INV_TOKEN["bravo_browser"]=work

    INV_SSHKEYS=()
    INV_SSHKEYS["alpha_yolo_2"]="key_alpha key_shared"
    INV_SSHKEYS["bravo_browser"]="key_shared"

    NET_MEMBERS=()
    ACTION=""
    SELECTED=()
}

# Every CCY session labelled, one token between them, nothing else. The state in which an
# identity axis is a second name for "all CCY containers" — see identity_axis_discriminates.
fixture_uniform() {
    INV_NAME=(one_yolo two_yolo)
    INV_STATE=(running paused)
    INV_NETS=(none none)

    INV_IS_CCY=()
    INV_IS_CCY["one_yolo"]=1
    INV_IS_CCY["two_yolo"]=1

    INV_HAS_LABELS=()
    INV_HAS_LABELS["one_yolo"]=1
    INV_HAS_LABELS["two_yolo"]=1

    INV_GITHUB=()
    INV_TOKEN=()
    INV_TOKEN["one_yolo"]=work
    INV_TOKEN["two_yolo"]=work
    INV_SSHKEYS=()

    NET_MEMBERS=()
    ACTION=""
    SELECTED=()
}

echo ""
echo "=== the fixture itself resets, so no case inherits another's state ==="
# Load-bearing for this whole file: an associative array that accumulated across cases
# would make later results depend on earlier ones, and the suite would still report green.
fixture
INV_GITHUB["leaked"]=stale
INV_IS_CCY["leaked"]=1
fixture
eq "a stale identity key does not survive"  "${INV_GITHUB["leaked"]:-unset}" "unset"
eq "a stale CCY key does not survive"       "${INV_IS_CCY["leaked"]:-unset}" "unset"
eq "the inventory is the size it declares"  "${#INV_NAME[@]}"              "6"

echo ""
echo "=== inventory_index_of: a name is found, or it is not there ==="
fixture
eq "the first entry"        "$(inventory_index_of zulu_yolo)"     "0"
eq "an entry in the middle" "$(inventory_index_of alpha_yolo_2)"  "2"
eq "the last entry"         "$(inventory_index_of echo_yolo_3)"   "5"
notok "an absent name returns non-zero"        inventory_index_of nosuch
# A name that is a prefix of a real one must not match it, and neither must a suffix.
# Substring matching is the quiet way a selection acts on a container nobody chose — and
# every CCY name here shares the `_yolo` tail with three others.
notok "a prefix of a real name does not match" inventory_index_of zulu
notok "a suffix of a real name does not match" inventory_index_of yolo
notok "the empty string is not a name"         inventory_index_of ""

echo ""
echo "=== ccy_marker: the table's CCY column ==="
fixture
eq "a labelled session"        "$(ccy_marker alpha_yolo_2)" "CCY"
eq "a legacy named session"    "$(ccy_marker zulu_yolo)"    "CCY"
eq "an ordinary container"     "$(ccy_marker mike)"         "-"
# A name with no entry at all defaults to not-CCY rather than erroring under `set -u`.
# The marker is drawn beside a row, so a missing key must not take the whole table down.
eq "a name the map never saw"  "$(ccy_marker nosuch)"       "-"

echo ""
echo "=== ccy_names: every kind of CCY session, in inventory order ==="
fixture
eq "labelled, legacy and identity-less sessions alike" \
    "$(lines_as_words ccy_names)" "zulu_yolo alpha_yolo_2 bravo_browser echo_yolo_3"
# The non-CCY containers sit at indices 1 and 3, between CCY ones, so a filter that
# stopped at the first mismatch would return only zulu_yolo.
eq "the non-CCY containers are not in it" \
    "$(lines_as_words ccy_names | grep -c -w -e mike -e kilo)" "0"

echo ""
echo "=== select_all / select_ccy: what lands in SELECTED ==="
fixture
select_all
eq "every container, in inventory order" \
    "${SELECTED[*]}" "zulu_yolo mike alpha_yolo_2 kilo bravo_browser echo_yolo_3"
fixture
select_ccy
eq "the CCY sessions only" \
    "${SELECTED[*]}" "zulu_yolo alpha_yolo_2 bravo_browser echo_yolo_3"
# An inventory with no CCY session leaves SELECTED EMPTY rather than unset: the caller
# tests `${#SELECTED[@]}` and would die on an unset array under `set -u`.
fixture
INV_IS_CCY=()
select_ccy
eq "no CCY sessions gives an empty selection, not an unset one" "${#SELECTED[@]}" "0"

echo ""
echo "=== select_names: the order the USER gave, and one unknown name is fatal ==="
fixture
select_names bravo_browser alpha_yolo_2
# `bravo_browser` is index 4 and `alpha_yolo_2` index 2, so a selector that walked the
# inventory would silently swap them — and the drill-down menu numbers these rows.
eq "names keep the order they were given" "${SELECTED[*]}" "bravo_browser alpha_yolo_2"
fixture
select_names mike
eq "a single name"                        "${SELECTED[*]}" "mike"
# Duplicates are NOT collapsed. Pinned because it is what the tool does, and because it
# travels: count_in_state then counts the container twice, so `podfreeze freeze mike mike`
# offers "FREEZE 2" for one container.
fixture
select_names mike mike
eq "a repeated name is kept twice"        "${SELECTED[*]}" "mike mike"

# An unknown name exits rather than being dropped, and it does so even when every other
# name is real. Selecting the subset that happened to exist would act on fewer containers
# than were asked for and say nothing about the difference.
if select_out="$(select_names nosuch 2>&1)"; then
    fail "an unknown name is fatal" "select_names nosuch succeeded"
else
    pass "an unknown name is fatal"
    contains "it names the container it could not find" "$select_out" "nosuch"
    contains "and points at the command that lists them" "$select_out" "podfreeze list"
fi
if select_out="$(select_names mike nosuch 2>&1)"; then
    fail "one unknown name among known ones is still fatal" "select_names succeeded"
else
    pass "one unknown name among known ones is still fatal"
fi

echo ""
echo "=== build_network_map: built from the inventory, not from podman network ls ==="
fixture
build_network_map
eq "the networks in use, and only those" \
    "$(printf '%s\n' "${!NET_MEMBERS[@]}" | sort | tr '\n' ' ')" "appnet podman "
# `podman, appnet` is one field with a comma AND a space; both separators are handled, so
# alpha is a member of both. A container on two networks appears under each.
eq "a container on two networks joins both" \
    "$(lines_as_words network_names appnet)" "alpha_yolo_2 kilo"
# zulu_yolo is index 0 and bravo_browser index 4, with alpha between them: inventory
# order, not the order the map's keys happen to hash into.
eq "members keep inventory order" \
    "$(lines_as_words network_names podman)" "zulu_yolo alpha_yolo_2 bravo_browser"
# `none` is what load_inventory writes for a container with no networks. It must not
# become a network of its own — it would be offered as a menu row that groups every
# unattached container under a name that is not a network.
eq "'none' is not a network"       "${NET_MEMBERS[none]:-absent}" "absent"
eq "and its containers are in no network" \
    "$(printf '%s' "${NET_MEMBERS[*]}" | grep -c -w -e mike -e echo_yolo_3)" "0"
# An empty networks field is not 'none' — the loader's `${nets:-none}` is what produces
# that word — and it must not become a network named "" either.
fixture
INV_NETS[1]=""
build_network_map
eq "an empty networks field is not a network" \
    "$(printf '%s\n' "${!NET_MEMBERS[@]}" | sort | tr '\n' ' ')" "appnet podman "
# Rebuilding must REPLACE, not accumulate. The interactive loop rebuilds the map on every
# pass, so a stale member is how a menu row acts on a container that left the network a
# screen ago.
fixture
build_network_map
INV_NAME=(kilo)
INV_STATE=(running)
INV_NETS=(appnet)
build_network_map
eq "a second build replaces the first" "$(lines_as_words network_names podman)" ""
eq "and keeps only what is there now"  "$(lines_as_words network_names appnet)" "kilo"

echo ""
echo "=== network_names: an unknown network is empty, not an error ==="
fixture
build_network_map
eq "an unknown network yields nothing" "$(lines_as_words network_names nosuchnet)" ""
ok  "and does not fail"                network_names nosuchnet

echo ""
echo "=== count_in_state: counts the NAMES GIVEN, not the inventory ==="
fixture
# The whole inventory holds 3 running and 3 paused. Every case below asks about a subset
# whose answer differs from both, so a mutant that ignores its arguments and counts the
# inventory cannot pass.
eq "two running out of three named"   "$(count_in_state running mike kilo zulu_yolo)" "2"
eq "one paused out of three named"    "$(count_in_state paused mike kilo zulu_yolo)"  "1"
eq "a single paused name"             "$(count_in_state paused zulu_yolo)"            "1"
eq "a single name in the other state" "$(count_in_state running zulu_yolo)"           "0"
eq "no names at all is zero"          "$(count_in_state running)"                     "0"
# An unknown name contributes nothing rather than erroring: the count is a count, and
# do_action's act/skip/vanished split is what reports a name that is not there.
eq "an unknown name does not count"   "$(count_in_state running mike nosuch)"         "1"
eq "a state nothing is in"            "$(count_in_state exited mike zulu_yolo)"       "0"
# The other half of select_names keeping duplicates: the menu row's number counts the
# name twice, so the label a user reads says FREEZE 2 for one container.
eq "a repeated name is counted twice" "$(count_in_state running mike mike)"           "2"

echo ""
echo "=== target_effect: the menu row can never disagree with the outcome ==="
fixture
eq "derived, running present"  "$(target_effect zulu_yolo mike alpha_yolo_2)" "FREEZE 1"
eq "derived, only paused"      "$(target_effect zulu_yolo alpha_yolo_2)"      "THAW   2"
eq "derived, neither"          "$(target_effect nosuch)"                      "nothing to do"
eq "derived, no names at all"  "$(target_effect)"                             "nothing to do"
# The count in the label is the count of things the verb will TOUCH, not the size of the
# selection. Three names, one of which will be frozen — this is the whole podman network
# group, and its row says FREEZE 1.
eq "the label counts the acted-on, not the selected" \
    "$(target_effect zulu_yolo alpha_yolo_2 bravo_browser)" "FREEZE 1"
# Running wins over paused when both are present: the derived verb freezes.
eq "a mixed set freezes rather than thaws" \
    "$(target_effect zulu_yolo bravo_browser)" "FREEZE 1"

# With an explicit verb the label must follow the VERB, not the state — a script that said
# `freeze` is not handed a row that says THAW.
ACTION="freeze"
eq "explicit freeze, running present" "$(target_effect zulu_yolo mike)"         "FREEZE 1"
eq "explicit freeze, nothing running" "$(target_effect zulu_yolo alpha_yolo_2)" "nothing to freeze"
ACTION="thaw"
eq "explicit thaw, paused present"    "$(target_effect zulu_yolo alpha_yolo_2)" "THAW   2"
eq "explicit thaw, nothing paused"    "$(target_effect mike kilo)"              "nothing to thaw"
# Distinct refusals. If both explicit verbs said "nothing to do", a user who asked to thaw
# would be told about freezing, and the two cases above would pass with one shared string.
ACTION="freeze"
_eff_freeze="$(target_effect zulu_yolo alpha_yolo_2)"
ACTION="thaw"
_eff_thaw="$(target_effect mike kilo)"
ACTION=""
_eff_derived="$(target_effect nosuch)"
if [ "$_eff_freeze" = "$_eff_thaw" ] || [ "$_eff_freeze" = "$_eff_derived" ] ||
    [ "$_eff_thaw" = "$_eff_derived" ]; then
    fail "the three refusals are distinct" \
        "freeze: $_eff_freeze" "thaw: $_eff_thaw" "derived: $_eff_derived"
else
    pass "the three refusals are distinct"
fi
# The two verbs share a column in the menu, so THAW carries padding FREEZE does not. A
# label that lost its padding would shift the count column on thaw rows only, and nothing
# else in this file would notice.
_w_freeze="$(target_effect mike)"
_w_thaw="$(target_effect zulu_yolo)"
eq "the two verb labels are the same width" "${#_w_freeze}" "${#_w_thaw}"

echo ""
echo "=== row_verb: what this ONE container is about to have done to it ==="
fixture
eq "derived, a running container" "$(row_verb mike)"      "FREEZE"
eq "derived, a paused container"  "$(row_verb zulu_yolo)" "THAW"
ACTION="freeze"
eq "explicit freeze, running"     "$(row_verb mike)"      "FREEZE"
eq "explicit freeze, paused"      "$(row_verb zulu_yolo)" "-"
ACTION="thaw"
eq "explicit thaw, paused"        "$(row_verb zulu_yolo)" "THAW"
eq "explicit thaw, running"       "$(row_verb mike)"      "-"
# A name with no inventory entry is '?', and it is '?' whatever the verb — the unknown
# check runs before the verb is consulted. It must not read as '-' ("nothing will happen
# to this one"), which is a claim about a container that is not there.
ACTION=""
eq "derived, an unknown name"         "$(row_verb nosuch)" "?"
ACTION="freeze"
eq "explicit freeze, an unknown name" "$(row_verb nosuch)" "?"
ACTION="thaw"
eq "explicit thaw, an unknown name"   "$(row_verb nosuch)" "?"
# Every derived-verb case in this file depends on `fixture` clearing ACTION, and the cases
# just above left an explicit verb set. Assert the reset rather than trusting it: a verb
# leaking into a later section would quietly change what those cases assert, and they
# would still pass.
fixture
eq "the fixture clears the explicit verb" "$ACTION" ""

echo ""
echo "=== infer_action: anything running gets frozen ==="
fixture
SELECTED=(mike)
eq "one running container"        "$(infer_action)" "freeze"
SELECTED=(zulu_yolo)
eq "one paused container"         "$(infer_action)" "thaw"
SELECTED=(zulu_yolo alpha_yolo_2 echo_yolo_3)
eq "a wholly paused set"          "$(infer_action)" "thaw"
# THE case a first-element-only predicate fails: two paused names before the running one.
# Without this, a predicate that looked at SELECTED[0] alone would pass every other case.
SELECTED=(zulu_yolo alpha_yolo_2 mike)
eq "a running container behind two paused" "$(infer_action)" "freeze"
SELECTED=(mike zulu_yolo)
eq "running first, paused second"          "$(infer_action)" "freeze"
# An empty selection has nothing running, so the rule gives `thaw`. Pinned because it is
# the rule's consequence and not a special case — do_action refuses an empty selection
# separately, and this documents which of the two speaks first.
SELECTED=()
eq "an empty selection"                    "$(infer_action)" "thaw"
# A selected name that is not in the inventory is skipped by the loop, so a selection of
# nothing but vanished containers reads as "thaw" — the same answer as a wholly paused
# set. do_action is where that difference is reported; infer_action cannot see it.
SELECTED=(nosuch)
eq "a vanished name infers thaw"           "$(infer_action)" "thaw"
SELECTED=(nosuch mike)
eq "but a real running name behind it still freezes" "$(infer_action)" "freeze"

echo ""
echo "=== identity_value_of: one axis, one container ==="
fixture
eq "a github account"           "$(identity_value_of github alpha_yolo_2)"  "octo"
eq "a token config"             "$(identity_value_of token bravo_browser)"  "work"
eq "several ssh keys, verbatim" "$(identity_value_of ssh-key alpha_yolo_2)" "key_alpha key_shared"
# An unlabelled session and a labelled one with `none` on the axis are BOTH empty here.
# These maps cannot tell them apart, which is precisely why INV_HAS_LABELS exists.
eq "a labelled session with no value on the axis" "$(identity_value_of github echo_yolo_3)" ""
eq "a session that predates the labels"           "$(identity_value_of github zulu_yolo)"   ""
eq "a container that is not CCY at all"           "$(identity_value_of token mike)"         ""
# An axis name that is not one of the three is an internal error and dies, rather than
# resolving to an empty value — an empty value would be an empty group, silently.
if id_out="$(identity_value_of bogus mike 2>&1)"; then
    fail "an unknown axis is fatal" "identity_value_of bogus succeeded"
else
    pass "an unknown axis is fatal"
    contains "and names the axis it did not recognise" "$id_out" "bogus"
fi

echo ""
echo "=== identity_matches: whole-value for github/token, WORD match for ssh-key ==="
ok    "github matches whole"           identity_matches github octo octo
notok "github does not match a prefix" identity_matches github oct octo
notok "github does not match a suffix" identity_matches github octo octopus
ok    "token matches whole"            identity_matches token work work
# ccy-ssh-keys can name several keys, so membership is a word match. Getting this wrong in
# the lenient direction would silently widen a freeze.
ok    "an ssh key among several"     identity_matches ssh-key key_shared "key_alpha key_shared"
ok    "the first of several"         identity_matches ssh-key key_alpha "key_alpha key_shared"
ok    "the only one"                 identity_matches ssh-key key_shared "key_shared"
notok "an ssh key that is not there" identity_matches ssh-key key_absent "key_alpha key_shared"
notok "a prefix of an ssh key"       identity_matches ssh-key key_share "key_alpha key_shared"
notok "no keys at all"               identity_matches ssh-key key_shared ""
# A wanted value containing a space can never match on the ssh-key axis, because the
# haystack is split on whitespace and a single word can never equal a two-word string.
# SSH key basenames do not normally contain spaces; pinned so a refactor that starts
# comparing whole values is visible rather than silently widening.
notok "an ssh key name with a space" identity_matches ssh-key "key a" "key a"

echo ""
echo "=== identity_matches: the empty value, which the CALLER is what guards ==="
# On a single-valued axis, empty want and empty have COMPARE EQUAL. Nothing inside this
# function stops `--github ''` from matching every session that has no github label.
# select_identity is what makes that unreachable, by refusing a value that is not in
# identity_values (which never emits the empty string). Pinned so that an extraction which
# moves the guard away from the caller shows up here rather than in a freeze nobody asked
# for.
ok "github: empty matches empty" identity_matches github "" ""
ok "token: empty matches empty"  identity_matches token "" ""
# ssh-key does NOT do this, because an empty haystack has no words to loop over. The two
# axes disagree about the empty value, and the asymmetry is real.
notok "ssh-key: empty does not match empty" identity_matches ssh-key "" ""

echo ""
echo "=== identity_matches: the ssh-key haystack does NOT glob ==="
# The ssh-key axis is the one holding several values, so it is split — and the split
# must not be an unquoted `for word in $have`, which is word splitting AND pathname
# expansion. A label value of `*` would then expand against the working directory and
# every file there would become a key the session appears to hold. A container label is
# not this tool's to trust that far.
#
# Driven in a controlled directory, so the result cannot depend on where the suite runs
# from — with a file present that a glob WOULD have matched.
glob_dir="$work/glob"
mkdir -p "$glob_dir"
: > "$glob_dir/zz-glob-victim"
glob_probe="$(cd "$glob_dir" && if identity_matches ssh-key zz-glob-victim '*'; then
    printf 'matched'
else
    printf 'no-match'
fi)"
eq "an ssh-key label of '*' does not glob-expand against the cwd" "$glob_probe" "no-match"
# The single-valued axes compare quoted and were never affected.
notok "github does not glob the same value" identity_matches github zz-glob-victim '*'
# The splitting itself must still work, or the fix would have broken the feature it
# was protecting — a session really can carry several keys.
ok "a multi-key label still matches its first member" identity_matches ssh-key alpha 'alpha beta'
ok "and its last" identity_matches ssh-key beta 'alpha beta'
notok "and still refuses one it does not hold" identity_matches ssh-key gamma 'alpha beta'

echo ""
echo "=== identity_values: distinct, sorted, and built from what is running ==="
fixture
# alpha (octo) is at index 2 and bravo (hubot) at index 4, so SORTED output reverses
# inventory order — a `sort -u` that was dropped would show up here rather than nowhere.
eq "github values are sorted"    "$(lines_as_words identity_values github)" "hubot octo"
# One token shared by two sessions is ONE value, not two.
eq "a shared value appears once" "$(lines_as_words identity_values token)"  "work"
# ssh-keys is multi-valued per session, so the label is split into its keys and the
# duplicate `key_shared` across two sessions collapses.
eq "ssh keys are split and deduplicated" \
    "$(lines_as_words identity_values ssh-key)" "key_alpha key_shared"
fixture
INV_GITHUB=()
eq "an axis nothing carries is empty" "$(lines_as_words identity_values github)" ""
# The literal "none" is dropped by load_inventory, NOT here. Pinned to record where that
# responsibility lives: a value of `none` reaching these maps would be offered as a group
# of its own, so the loader's filter is load-bearing rather than belt-and-braces.
fixture
INV_GITHUB["echo_yolo_3"]=none
eq "a literal 'none' in the map IS offered as a value" \
    "$(lines_as_words identity_values github)" "hubot none octo"

echo ""
echo "=== identity_names: who is in this group, in inventory order ==="
fixture
eq "a single-member group"  "$(lines_as_words identity_names github octo)" "alpha_yolo_2"
# alpha is index 2 and bravo index 4, with kilo between them — inventory order, and not
# adjacent, so a loop that stopped at the first match returns half the group.
eq "a two-member group"     "$(lines_as_words identity_names ssh-key key_shared)" \
    "alpha_yolo_2 bravo_browser"
eq "a key only one holds"   "$(lines_as_words identity_names ssh-key key_alpha)" "alpha_yolo_2"
eq "a value nobody carries" "$(lines_as_words identity_names github nosuch)" ""
# The consequence of identity_matches' empty-matches-empty above, spelled out: asking for
# an empty github value returns every container WITHOUT one — including containers that
# are not CCY at all. Unreachable through select_identity, and this is the case that would
# stop being unreachable if its guard moved.
eq "an empty value selects everything unlabelled on that axis" \
    "$(lines_as_words identity_names github "")" "zulu_yolo mike kilo echo_yolo_3"
# ssh-key refuses the same question, for the reason pinned above.
eq "ssh-key returns nothing for an empty value" \
    "$(lines_as_words identity_names ssh-key "")" ""

echo ""
echo "=== identity_axis_discriminates: COVERAGE, not cardinality ==="
fixture
ok "two github accounts tell sessions apart" identity_axis_discriminates github
ok "two ssh keys tell sessions apart"        identity_axis_discriminates ssh-key
# THE case a `${#values[@]} -lt 2` proxy gets wrong, and the reason this is a function.
# One token, held by 2 of the 4 CCY sessions: a single distinct value that still splits
# the set. Suppressing the row here would leave only the WIDER "all CCY containers",
# steering the user toward freezing sessions they never asked about.
ok "one value covering SOME of the CCY sessions" identity_axis_discriminates token
fixture
INV_GITHUB=()
notok "an axis nothing carries offers no rows" identity_axis_discriminates github
# One value covering EVERY CCY session is the same button as "all CCY containers", so the
# row is suppressed. This is the case the coverage test exists to keep distinct from the
# one above — both have exactly one distinct value.
fixture_uniform
notok "one value covering ALL of the CCY sessions" identity_axis_discriminates token
# And the boundary is genuinely coverage: add one unlabelled CCY session to the uniform
# fixture and the same single value starts discriminating.
fixture_uniform
INV_NAME+=(three_yolo)
INV_STATE+=(running)
INV_NETS+=(none)
INV_IS_CCY["three_yolo"]=1
ok "the same single value, with one session outside it" identity_axis_discriminates token

echo ""
echo "=== unlabelled_ccy_names: 'no identity' is not 'predates the labels' ==="
fixture
# zulu_yolo has no labels. echo_yolo_3 has labels and `none` on every axis, so its identity
# maps are empty EXACTLY as zulu's are. If these two collapsed, the tool would tell the
# owner of a current session to relaunch it so it can be labelled — advice that cannot
# work, because it already is.
eq "only the session that predates the labels" \
    "$(lines_as_words unlabelled_ccy_names)" "zulu_yolo"
# Non-CCY containers are not in the blind spot: they were never in an identity group and
# were never meant to be.
eq "an ordinary container is not reported" \
    "$(lines_as_words unlabelled_ccy_names | grep -c -w -e mike -e kilo)" "0"
fixture_uniform
eq "a fully labelled machine has no blind spot" "$(lines_as_words unlabelled_ccy_names)" ""

echo ""
echo "=== warn_identity_blind_spot: disclose, do not block ==="
fixture
warn_identity_blind_spot 2> "$work/warn.txt"
warn_txt="$(cat "$work/warn.txt")"
contains "it counts the sessions it could not consider" "$warn_txt" "1 CCY session(s)"
contains "and names them"                               "$warn_txt" "zulu_yolo"
contains "and says what to do instead"                  "$warn_txt" "--ccy"
# Nothing on stdout: this is a diagnostic around a selection, not the selection itself
# (CLAUDE/StderrHygiene.md).
eq "nothing goes to stdout" "$(warn_identity_blind_spot 2> "$work/warn.txt")" ""
fixture_uniform
warn_identity_blind_spot 2> "$work/warn.txt"
eq "silence when every session is labelled" "$(cat "$work/warn.txt")" ""

echo ""
echo "=== select_identity: an unknown value is an error, never an empty set ==="
fixture
select_identity ssh-key key_shared 2> "$work/warn.txt"
eq "a known value selects its group" "${SELECTED[*]}" "alpha_yolo_2 bravo_browser"
contains "and discloses the sessions it could not consider" \
    "$(cat "$work/warn.txt")" "zulu_yolo"
fixture
select_identity github octo 2> "$work/warn.txt"
eq "a single-member group"           "${SELECTED[*]}" "alpha_yolo_2"

# A value nobody carries must fail loudly and list what IS available. Resolving it to an
# empty set and exiting 0 would report "nothing to do" for a question never asked.
if sel_out="$(select_identity github nosuch 2>&1)"; then
    fail "an unknown value is fatal" "select_identity github nosuch succeeded"
else
    pass "an unknown value is fatal"
    contains "it names the value"         "$sel_out" "nosuch"
    contains "and lists the known values" "$sel_out" "octo"
fi

# An axis NOTHING carries gets a different message: there is no list to offer, and the
# reason is almost always that every session predates CCY 3.40.0. Telling that user "known
# values: (nothing)" would leave them with no next step.
fixture
INV_GITHUB=()
if sel_empty_out="$(select_identity github octo 2>&1)"; then
    fail "an axis nothing carries is fatal" "select_identity succeeded"
else
    pass "an axis nothing carries is fatal"
    contains "it says the labels are absent, not the value" "$sel_empty_out" "3.40.0"
fi
# The two refusals must differ, or the case above passes against one shared string that
# gives half of its readers the wrong advice.
if [ "$sel_out" = "$sel_empty_out" ]; then
    fail "the two identity refusals are distinct" "both say: $sel_out"
else
    pass "the two identity refusals are distinct"
fi
# The whole-value axes are exact: a prefix of a real value is refused rather than widened.
fixture
if sel_out="$(select_identity github oct 2>&1)"; then
    fail "a prefix of a known value is refused" "select_identity github oct succeeded"
else
    pass "a prefix of a known value is refused"
fi

# ---------------------------------------------------------------------------
# The two functions that query podman. A `podman` shell function shadows the binary for
# the rest of this file, so the cases drive the real parsing and the real guards without a
# host. Anything the stub is not primed for fails loudly rather than returning nothing —
# an unprimed query answering "no containers" is the exact confident lie this tool refuses
# to tell from inside a container.
# ---------------------------------------------------------------------------
PODMAN_PS_OUT=""
PODMAN_PS_RC=0
PODMAN_CCY_OUT=""
PODMAN_CCY_RC=0
PODMAN_NETWORKS_OUT=""
PODMAN_NETWORKS_RC=0
PODMAN_NETFILTER_OUT=""
PODMAN_NETFILTER_RC=0

podman() {
    case "$*" in
        "network ls"*)
            printf '%s\n' "$PODMAN_NETWORKS_OUT"
            return "$PODMAN_NETWORKS_RC"
            ;;
        *"--filter label=$CCY_SESSION_LABEL"*)
            printf '%s\n' "$PODMAN_CCY_OUT"
            return "$PODMAN_CCY_RC"
            ;;
        *"--filter network="*)
            printf '%s\n' "$PODMAN_NETFILTER_OUT"
            return "$PODMAN_NETFILTER_RC"
            ;;
        "ps --all --format"*)
            printf '%s\n' "$PODMAN_PS_OUT"
            return "$PODMAN_PS_RC"
            ;;
    esac
    echo "podman stub: no answer primed for: podman $*" >&2
    return 99
}

echo ""
echo "=== load_inventory: only running and paused containers are inventoried ==="
PODMAN_PS_OUT="zulu_yolo|paused|podman
mike|running|none
alpha_yolo_2|paused|podman, appnet
gone_thing|exited|podman
never_started|created|podman
kilo|running|appnet
nonet|running|"
PODMAN_PS_RC=0
PODMAN_CCY_OUT=""
PODMAN_CCY_RC=0
load_inventory
# An exited container can be neither frozen nor thawed, so offering one as a candidate
# would only invite a confusing no-op. `gone_thing` and `never_started` sit BETWEEN real
# entries, so a filter that stopped at the first rejection would truncate the inventory.
eq "the states that can be acted on, in podman's order" \
    "${INV_NAME[*]}" "zulu_yolo mike alpha_yolo_2 kilo nonet"
eq "their states travel with them" "${INV_STATE[*]}" "paused running paused running running"
# The networks field is stored VERBATIM — comma-and-space and all. build_network_map is
# what normalises it, and print_table shows the raw string to the user.
eq "the networks field is not normalised" "${INV_NETS[2]}" "podman, appnet"
# An empty networks field becomes the word `none`, which build_network_map then skips.
# Without that default the field would be empty and `none` would never be written, so the
# container would be silently indistinguishable from one on a network called "".
eq "an empty networks field becomes 'none'" "${INV_NETS[4]}" "none"

echo ""
echo "=== load_inventory: which names are CCY, by pattern ==="
PODMAN_PS_OUT="proj_yolo|running|none
proj_yolo_2|running|none
proj_browser|running|none
proj_browser_10|running|none
yolo|running|none
_yolo|running|none
proj_yolo_x|running|none
proj_yoloo|running|none
proj_browser_|running|none
plain|running|none"
PODMAN_CCY_OUT=""
load_inventory
eq "a bare session name"         "${INV_IS_CCY[proj_yolo]}"       "1"
eq "a numbered session"          "${INV_IS_CCY[proj_yolo_2]}"     "1"
eq "a browser session"           "${INV_IS_CCY[proj_browser]}"    "1"
eq "a two-digit browser session" "${INV_IS_CCY[proj_browser_10]}" "1"
# The pattern requires a project prefix, so the bare word and a leading underscore are not
# sessions. A looser pattern would mark ordinary containers as Claude sessions, and the
# CCY column is what a user reads before freezing something.
eq "the bare word is not a session"      "${INV_IS_CCY[yolo]}"          "0"
eq "a missing project prefix"            "${INV_IS_CCY[_yolo]}"         "0"
eq "a non-numeric suffix"                "${INV_IS_CCY[proj_yolo_x]}"   "0"
eq "a name that merely starts the same"  "${INV_IS_CCY[proj_yoloo]}"    "0"
eq "a trailing separator with no number" "${INV_IS_CCY[proj_browser_]}" "0"
eq "an ordinary container"               "${INV_IS_CCY[plain]}"         "0"

echo ""
echo "=== load_inventory: the label pass, and what 'none' means ==="
PODMAN_PS_OUT="zulu_yolo|paused|podman
mike|running|none
alpha_yolo_2|paused|podman, appnet
echo_yolo_3|paused|none
plain_labelled|running|none"
PODMAN_CCY_OUT="alpha_yolo_2|octo|work|key_alpha key_shared
echo_yolo_3|none|none|none
plain_labelled|hubot|none|none
exited_yolo|octo|work|key_shared"
load_inventory
eq "identity values are read off the labels" "${INV_GITHUB[alpha_yolo_2]}"  "octo"
eq "a multi-key label is kept whole"         "${INV_SSHKEYS[alpha_yolo_2]}" "key_alpha key_shared"
# `none` is CCY saying the axis does not apply. It is dropped rather than offered as a
# group of its own — nobody wants to freeze "everything with no GitHub account".
eq "'none' is not stored as a value" "${INV_GITHUB[echo_yolo_3]:-absent}"  "absent"
eq "nor on the token axis"           "${INV_TOKEN[echo_yolo_3]:-absent}"   "absent"
eq "nor on the ssh-key axis"         "${INV_SSHKEYS[echo_yolo_3]:-absent}" "absent"
# But the SESSION is still recorded as labelled, which is the distinction the identity
# blind-spot warning turns on.
eq "a session with no identity is still labelled" "${INV_HAS_LABELS[echo_yolo_3]}"  "1"
eq "a session predating the labels is not"        "${INV_HAS_LABELS[zulu_yolo]:-0}" "0"
eq "and an ordinary container certainly is not"   "${INV_HAS_LABELS[mike]:-0}"      "0"
# The run-time label is authoritative over the name pattern: a container the pattern would
# reject is a CCY session if it carries the label.
eq "the label marks a session the name would not" "${INV_IS_CCY[plain_labelled]}" "1"
# The label query is `--all` and unfiltered by state, so it can name a container the
# inventory does not hold. Every consumer walks INV_NAME, so such a container is in the
# maps but in no group.
eq "a labelled container outside the inventory is in the maps" "${INV_IS_CCY[exited_yolo]}" "1"
eq "but not in any selection" \
    "$(lines_as_words ccy_names)" "zulu_yolo alpha_yolo_2 echo_yolo_3 plain_labelled"

echo ""
echo "=== load_inventory: a label value holding the field separator is FATAL ==="
# The identity fields are label VALUES — a username, a token config name, SSH key
# basenames — and a filename may legally contain the separator. A mis-split record is
# silent: the session would simply be grouped under the wrong identity. The fifth field is
# what catches it, so the failure is loud instead.
PODMAN_PS_OUT="alpha_yolo_2|running|none"
PODMAN_CCY_OUT="alpha_yolo_2|octo|work|key${FIELD_SEP}odd"
if load_out="$(load_inventory 2>&1)"; then
    fail "a separator inside a label value is fatal" "load_inventory succeeded"
else
    pass "a separator inside a label value is fatal"
    contains "it names the session"       "$load_out" "alpha_yolo_2"
    contains "and the character at fault" "$load_out" "$FIELD_SEP"
    contains "and says how to recover"    "$load_out" "relaunch"
fi
# A record with exactly the four expected fields is NOT flagged — otherwise the guard
# above would pass against a function that refused everything.
PODMAN_CCY_OUT="alpha_yolo_2|octo|work|key_shared"
if load_inventory; then
    pass "a well-formed record is accepted"
else
    fail "a well-formed record is accepted" "load_inventory refused a valid record"
fi

echo ""
echo "=== load_inventory: a failed query is fatal, never an empty machine ==="
# The whole reason podfreeze refuses to run in a container: a query that could not be
# answered must not resolve to "nothing is running". That reads as a fact about the host.
PODMAN_PS_OUT="Cannot connect to Podman socket"
PODMAN_PS_RC=1
if load_out="$(load_inventory 2>&1)"; then
    fail "a failed ps is fatal" "load_inventory succeeded with a failing podman"
else
    pass "a failed ps is fatal"
    contains "and reports what podman said" "$load_out" "Cannot connect to Podman socket"
fi
PODMAN_PS_OUT="alpha_yolo_2|running|none"
PODMAN_PS_RC=0
PODMAN_CCY_OUT="permission denied reading labels"
PODMAN_CCY_RC=1
if load_out="$(load_inventory 2>&1)"; then
    fail "a failed label query is fatal" "load_inventory succeeded"
else
    pass "a failed label query is fatal"
    # Falling through here would leave every session looking unlabelled, which is a
    # different and wrong story — "relaunch them all to get labels".
    contains "and reports what podman said" "$load_out" "permission denied reading labels"
fi
PODMAN_CCY_RC=0

echo ""
echo "=== select_network: an unknown network RETURNS, it does not exit ==="
fixture
PODMAN_NETWORKS_OUT="podman
appnet
bridge"
PODMAN_NETWORKS_RC=0
PODMAN_NETFILTER_OUT=""
PODMAN_NETFILTER_RC=0
# Distinguishing `return 1` from `exit 1` needs the sentinel: if the function exited, the
# subshell dies before the printf. The difference matters — from the menu a network may
# simply have been removed between the row being drawn and chosen, which the interactive
# rules say to re-prompt on rather than abort the session.
#
# The assignment is wrapped in `if !` for the same errexit reason the header gives: a `$( )`
# is exempt as an ARGUMENT, but a bare `x="$(…)"` whose substitution fails takes its status
# and aborts the run. That is not hypothetical here — it is exactly what this case is built
# to detect, so writing it the obvious way would make the detection silent.
net_probe="never ran"
if ! net_probe="$(if select_network nosuchnet 2> "$work/probe-err.txt"; then
    printf 'returned 0'
else
    printf 'returned %s' "$?"
fi)"; then
    net_probe="exited"
fi
eq "an unknown network comes back as a status, not an exit" "$net_probe" "returned 1"

# The cases below call it in THIS shell, to see what it left behind. That is only safe once
# the probe has established it returns: a tool that exits would take the suite with it and
# the summary would never print. So the alternative is a named failure, not a skip.
if [ "$net_probe" = "returned 1" ]; then
    fixture
    SELECTED=(sentinel_from_a_previous_choice)
    if select_network nosuchnet 2> "$work/neterr.txt"; then
        fail "an unknown network is refused" "select_network nosuchnet succeeded"
    else
        pass "an unknown network is refused"
    fi
    net_err="$(cat "$work/neterr.txt")"
    contains "it names the network"     "$net_err" "nosuchnet"
    contains "and lists the known ones" "$net_err" "appnet"
    # SELECTED is only cleared AFTER the existence check passes, so the previous selection
    # survives a refusal. Pinned as the tool's actual behaviour: it is why interactive_loop
    # must `continue` on a non-zero rather than fall through to the drill-down.
    eq "the previous selection is left standing" \
        "${SELECTED[*]}" "sentinel_from_a_previous_choice"
else
    fail "the refusal path can be driven from this shell" \
        "select_network did not return ($net_probe), so its message and its effect on" \
        "SELECTED cannot be checked without killing this suite"
fi

echo ""
echo "=== select_network: a known network, filtered through the inventory ==="
fixture
PODMAN_NETFILTER_OUT="alpha_yolo_2
kilo
gone_thing"
if select_network appnet; then
    pass "a known network is accepted"
else
    fail "a known network is accepted" "select_network appnet failed"
fi
# `podman ps --filter network=` is `--all`, so it can name an exited container. The
# inventory holds only running and paused ones, and a name that is not in it is dropped —
# quietly, because it was never a candidate.
eq "only inventoried containers are selected" "${SELECTED[*]}" "alpha_yolo_2 kilo"
# The selection is rebuilt, not appended to.
fixture
SELECTED=(stale_name)
PODMAN_NETFILTER_OUT="kilo"
if select_network appnet; then
    pass "a second selection replaces the first"
else
    fail "a second selection replaces the first" "select_network appnet failed"
fi
eq "and holds only the new members" "${SELECTED[*]}" "kilo"

echo ""
echo "=== select_network: the existence check is a LITERAL match ==="
# A network name is a literal, so the check greps with -F. Without it the name is a
# basic regular expression and a metacharacter matches a network the user never named:
# `podma.` would pass because `podman` exists, then the filter query returns nothing and
# the user is told "Nothing in that group" rather than that the network does not exist.
# select_identity does the same job the same way.
fixture
PODMAN_NETFILTER_OUT=""
if select_network 'podma.'; then
    fail "a regex metacharacter does NOT pass the existence check" \
        "'podma.' was accepted — the check has become a regex match again"
else
    pass "a regex metacharacter does NOT pass the existence check"
fi
# The literal name it was standing in for must still be accepted, or the fix would have
# broken the lookup rather than tightened it.
fixture
PODMAN_NETFILTER_OUT=""
if select_network 'podman'; then
    pass "the literal network name is still accepted"
else
    fail "the literal network name is still accepted" "'podman' was refused"
fi

echo ""
echo "=== select_network: a failed network query is fatal ==="
PODMAN_NETWORKS_OUT="podman network ls: connection refused"
PODMAN_NETWORKS_RC=1
if net_out="$(select_network appnet 2>&1)"; then
    fail "a failed network ls is fatal" "select_network succeeded"
else
    pass "a failed network ls is fatal"
    contains "and reports what podman said" "$net_out" "connection refused"
fi
PODMAN_NETWORKS_RC=0

echo ""
echo "=== the constants the rows and the parser depend on ==="
# The drill-down's "act on all of it" row shares a column with container names. Podman
# requires a name to begin with an alphanumeric, so a sentinel that does not is one a real
# container can never collide with — and the emitters key on exactly that first field.
case "$ALL_ROW_KEY" in
    [a-zA-Z0-9]*)
        fail "the all-row sentinel cannot be a container name" \
            "'$ALL_ROW_KEY' starts with an alphanumeric, so a container could be called this" ;;
    *) pass "the all-row sentinel cannot be a container name" ;;
esac
# The record separator must be one character that cannot appear in a podman name, or the
# inventory query mis-splits on ordinary input rather than on an odd label.
eq "the field separator is a single character" "${#FIELD_SEP}" "1"
case "$FIELD_SEP" in
    [a-zA-Z0-9_.-])
        fail "the field separator cannot occur in a podman name" \
            "'$FIELD_SEP' is legal in a container or network name" ;;
    *) pass "the field separator cannot occur in a podman name" ;;
esac
# A bounded retry budget is what keeps a mistyped menu answer from looping for ever.
if [ "$MAX_TRIES" -ge 2 ]; then
    pass "the menu retry budget allows a recoverable mistake"
else
    fail "the menu retry budget allows a recoverable mistake" "MAX_TRIES is $MAX_TRIES"
fi

echo ""
echo "──────────────────────────────────────────────────────────────"
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"

# A suite that discovers nothing exits 0 and reports clean, which is the failure this whole
# file exists to prevent in the tool it tests.
if [ "$PASSED" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
echo "OK"

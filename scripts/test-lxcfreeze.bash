#!/usr/bin/env bash
# Unit-test lxcfreeze's decisions (Plan 00122).
#
# Sources the SHIPPED tool from files/home/.local/bin/lxcfreeze — not a copy, not a
# library extracted for the tests — so what passes here is what gets deployed. The tool
# guards its own dispatch with the `BASH_SOURCE[0] == $0` idiom, which is why sourcing it
# defines its functions without running anything.
#
# WHAT IS UNDER TEST IS THE DECISION, NOT THE QUERY. This container has no `lxc`, and
# lxcfreeze refuses to run inside a container anyway — by design, since the host's LXC is
# unreachable from in here and an empty answer would be a confident lie. So every function
# here takes fabricated data and returns an answer, and nothing shells out. That split is
# this repo's existing pattern: see scripts/test-ccy-rootless-guard.bash, which says it as
# "you cannot ask a real engine to be rootful just to prove the guard notices".
#
# What that leaves uncovered, stated rather than glossed: whether `lxc-ls -1` and
# `lxc-info -n NAME -s` actually emit what the parsers here are fed. Only a host with LXC
# can say, and Plan 00122 Task 3.4 is where it gets said.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly. Same reason, same shape as
# scripts/test-ccy-rootless-guard.bash.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$REPO_ROOT/files/home/.local/bin/lxcfreeze"

if [ ! -f "$TOOL" ]; then
    echo "FAIL: lxcfreeze not found at $TOOL" >&2
    exit 1
fi

# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/home/.local/bin/lxcfreeze
source "$TOOL"

# lxcfreeze sets `-euo pipefail` at its top, and SOURCING IT APPLIES THAT TO THIS
# SHELL — so from here down this file runs under errexit, whatever its own prelude
# said. That is asserted rather than assumed, for two reasons:
#
#   1. The prelude arriving is itself worth checking. The fail-fast rule requires it,
#      and a tool that quietly lost it would still pass every case below.
#   2. It constrains how the cases are written, and the constraint is invisible.
#      EVERY case that drives a function to a deliberate non-zero MUST sit inside an
#      `if` condition or a `$( )` substitution, which errexit exempts. A bare call
#      would abort the run before the summary printed. Plan 00109 lost a day to
#      exactly that exemption, in the other direction — do not add a bare negative
#      call here and expect the suite to carry on.
case "$-" in
    *e*) ;;
    *)
        echo "FAIL: sourcing $TOOL did not bring errexit with it" >&2
        echo "      (its 'set -euo pipefail' prelude is missing — a fail-fast violation)" >&2
        exit 1
        ;;
esac

# The tool being sourceable is load-bearing for every case below, and a main dispatch that
# leaked would show up as this suite hanging on a prompt or dying in the container guard
# rather than as a clean failure. Assert the functions exist before relying on them.
for fn in lxcf_index_of lxcf_count_in_state lxcf_infer_action lxcf_target_effect \
    lxcf_partition lxcf_parse_state lxcf_parse_bridge lxcf_bridge_label; do
    if ! declare -F "$fn" > /dev/null; then
        echo "FAIL: $fn is not defined after sourcing $TOOL" >&2
        echo "      (the decision is absent, not merely wrong)" >&2
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

# ---------------------------------------------------------------------------
# The fixture inventory.
#
# DELIBERATELY out of order, mixed-state, and larger than two entries. A list
# that arrives sorted, or uniform, or two long, cannot falsify a decision that
# reads the wrong element or stops at the first one — Plan 00109 shipped a suite
# where every stub list was already in the order the code sorted it into, and
# deleting both sorts passed all twelve cases.
#
# `alpha` sits AFTER two frozen entries on purpose: it is the only running
# container in several selections below, so a predicate that inspects only the
# first name it is handed gets the wrong answer.
# ---------------------------------------------------------------------------
fixture() {
    INV_NAME=(zulu mike alpha kilo bravo)
    INV_STATE=(FROZEN FROZEN RUNNING FROZEN RUNNING)
    INV_BRIDGE=(lxcbr0 virbr1 lxcbr0 "$BRIDGE_NONE" virbr1)
    ACTION=""
    SELECTED=()
}

echo ""
echo "=== lxcf_index_of: a name is found, or it is not there ==="
fixture
eq "the first entry"              "$(lxcf_index_of zulu)"  "0"
eq "an entry in the middle"       "$(lxcf_index_of alpha)" "2"
eq "the last entry"               "$(lxcf_index_of bravo)" "4"
if lxcf_index_of nosuch > /dev/null; then
    fail "an absent name returns non-zero" "lxcf_index_of nosuch succeeded"
else
    pass "an absent name returns non-zero"
fi
# A name that is a prefix of a real one must not match it. Substring matching is the
# quiet way a selection acts on a container nobody chose.
if lxcf_index_of alph > /dev/null; then
    fail "a prefix of a real name does not match" "lxcf_index_of alph matched alpha"
else
    pass "a prefix of a real name does not match"
fi

echo ""
echo "=== lxcf_count_in_state: counts the NAMES GIVEN, not the inventory ==="
fixture
# The whole inventory holds 2 running and 3 frozen. Each case below asks about a
# subset whose answer differs from both of those totals, so a mutant that ignores
# its arguments and counts the inventory cannot pass.
eq "two frozen out of three named"   "$(lxcf_count_in_state FROZEN zulu mike alpha)"  "2"
eq "one running out of three named"  "$(lxcf_count_in_state RUNNING zulu mike alpha)" "1"
eq "a single frozen name"            "$(lxcf_count_in_state FROZEN kilo)"             "1"
eq "a single name in the other state" "$(lxcf_count_in_state RUNNING kilo)"           "0"
eq "no names at all is zero"         "$(lxcf_count_in_state FROZEN)"                  "0"
# An unknown name contributes nothing rather than erroring: the count is a count, and
# the act/skip/vanished split below is what reports a name that is not there.
eq "an unknown name does not count"  "$(lxcf_count_in_state RUNNING alpha nosuch)"    "1"
eq "a state nothing is in"           "$(lxcf_count_in_state STOPPED zulu alpha)"      "0"

echo ""
echo "=== lxcf_infer_action: anything running gets frozen ==="
fixture
SELECTED=(alpha)
eq "one running container"                "$(lxcf_infer_action)" "freeze"
SELECTED=(zulu)
eq "one frozen container"                 "$(lxcf_infer_action)" "thaw"
SELECTED=(zulu mike kilo)
eq "a wholly frozen set"                  "$(lxcf_infer_action)" "thaw"
# THE case a first-element-only predicate fails: two frozen names before the running
# one. Without this, a predicate that looked at SELECTED[0] alone would pass every
# other case here.
SELECTED=(zulu mike alpha)
eq "a running container behind two frozen" "$(lxcf_infer_action)" "freeze"
SELECTED=(alpha zulu)
eq "running first, frozen second"          "$(lxcf_infer_action)" "freeze"
# An empty selection has nothing running, so the rule gives `thaw`. Pinned because it
# is the rule's consequence and not a special case — the action refuses an empty
# selection separately, and this documents which of the two speaks first.
SELECTED=()
eq "an empty selection"                    "$(lxcf_infer_action)" "thaw"

echo ""
echo "=== lxcf_target_effect: the menu row can never disagree with the outcome ==="
fixture
eq "derived, running present"  "$(lxcf_target_effect zulu mike alpha)" "FREEZE 1"
eq "derived, only frozen"      "$(lxcf_target_effect zulu mike)"       "THAW   2"
eq "derived, neither"          "$(lxcf_target_effect nosuch)"          "nothing to do"
# The count in the label is the count of things the verb will touch, not the size of
# the selection. Three names, one of which will be frozen.
eq "the label counts the acted-on, not the selected" \
    "$(lxcf_target_effect zulu kilo alpha)" "FREEZE 1"

# With an explicit verb the label must follow the VERB, not the state — a script that
# said `freeze` is not handed a row that says THAW.
ACTION="freeze"
eq "explicit freeze, running present" "$(lxcf_target_effect zulu alpha)" "FREEZE 1"
eq "explicit freeze, nothing running" "$(lxcf_target_effect zulu mike)"  "nothing to freeze"
ACTION="thaw"
eq "explicit thaw, frozen present"    "$(lxcf_target_effect zulu mike)"  "THAW   2"
eq "explicit thaw, nothing frozen"    "$(lxcf_target_effect alpha bravo)" "nothing to thaw"
# Distinct refusals. If both explicit verbs said "nothing to do", a user who asked to
# thaw would be told about freezing, and the two cases above would pass with one
# shared string. Assert they differ from each other AND from the derived refusal.
ACTION="freeze"
_eff_freeze="$(lxcf_target_effect zulu mike)"
ACTION="thaw"
_eff_thaw="$(lxcf_target_effect alpha bravo)"
ACTION=""
_eff_derived="$(lxcf_target_effect nosuch)"
if [ "$_eff_freeze" = "$_eff_thaw" ] || [ "$_eff_freeze" = "$_eff_derived" ] ||
    [ "$_eff_thaw" = "$_eff_derived" ]; then
    fail "the three refusals are distinct" \
        "freeze: $_eff_freeze" "thaw: $_eff_thaw" "derived: $_eff_derived"
else
    pass "the three refusals are distinct"
fi

echo ""
echo "=== lxcf_partition: act, skip, and VANISHED are three answers ==="
fixture
# A selection holding all three kinds at once, so no case can pass by collapsing two
# of the buckets into one.
lxcf_partition freeze alpha zulu nosuch
eq "freeze: the running one is a target"    "${LXCF_TARGETS[*]}"  "alpha"
eq "freeze: the frozen one is skipped"      "${LXCF_SKIPPED[*]}"  "zulu"
eq "freeze: the absent one has vanished"    "${LXCF_VANISHED[*]}" "nosuch"

fixture
lxcf_partition thaw alpha zulu nosuch
eq "thaw: the frozen one is a target"       "${LXCF_TARGETS[*]}"  "zulu"
eq "thaw: the running one is skipped"       "${LXCF_SKIPPED[*]}"  "alpha"
eq "thaw: the absent one has vanished"      "${LXCF_VANISHED[*]}" "nosuch"

# A name that is not in the inventory must be REPORTED, not dropped. Silently dropping
# it means acting on fewer containers than were chosen and saying nothing about the
# difference — an under-match nobody can see. This is the case that fails if `vanished`
# is folded into `skipped`, which reads to a user as "already in that state".
fixture
lxcf_partition freeze nosuch
eq "an all-absent selection has no targets" "${LXCF_TARGETS[*]-}"  ""
eq "and is not reported as skipped"         "${LXCF_SKIPPED[*]-}"  ""
eq "it is reported as vanished"             "${LXCF_VANISHED[*]}"  "nosuch"

# Order is the order the user gave, not inventory order: `bravo` is index 4 and
# `alpha` index 2, so a partition that walked the inventory would swap them.
fixture
lxcf_partition freeze bravo alpha
eq "targets keep the order they were given" "${LXCF_TARGETS[*]}" "bravo alpha"

# Partitioning twice must not accumulate. A stale bucket from a previous pass is how
# the interactive loop would act on something the user chose a screen ago.
fixture
lxcf_partition freeze alpha
lxcf_partition freeze bravo
eq "a second partition replaces the first"  "${LXCF_TARGETS[*]}" "bravo"

echo ""
echo "=== lxcf_parse_state: what lxc-info says, and what it does not say ==="
# The shapes `lxc-info -n NAME -s` emits. Confirmed against a host in Plan 00122
# Task 3.4 — until then these are the documented format, and the parser is written to
# the format rather than to one observed line.
eq "a running container"      "$(lxcf_parse_state 'State:          RUNNING')" "RUNNING"
eq "a frozen container"       "$(lxcf_parse_state 'State:          FROZEN')"  "FROZEN"
eq "a stopped container"      "$(lxcf_parse_state 'State:          STOPPED')" "STOPPED"
eq "a single space"           "$(lxcf_parse_state 'State: RUNNING')"          "RUNNING"
eq "a tab"                    "$(lxcf_parse_state $'State:\tRUNNING')"        "RUNNING"
eq "trailing whitespace"      "$(lxcf_parse_state 'State:   RUNNING   ')"     "RUNNING"

echo ""
echo "=== lxcf_parse_state: silence is not a state ==="
# THE load-bearing block, and the reason this parser is a function at all. A probe that
# could not answer must not resolve to a state — least of all to STOPPED, which reads
# as a fact about the machine and would quietly drop the container from the inventory.
# Plan 00109 hit this exact shape twice: `probe.dkms_registry`, where "no directory"
# and "directory present, no modules" were both `[]`, and `_command_version`, where the
# OS refusing to exec a binary and the binary complaining about something inside itself
# produced one answer. "Could not tell" is its own answer or it is a bug.
eq "no output at all"         "$(lxcf_parse_state '')"                        ""
eq "whitespace only"          "$(lxcf_parse_state $'  \n\t ')"                ""
eq "the error lxc-info gives for an unknown container" \
    "$(lxcf_parse_state "lxc-info: nosuch: tools/lxc_info.c: main: 133 Container is not defined")" ""
eq "a state word this tool does not know" \
    "$(lxcf_parse_state 'State:          ABORTING')"                          ""
eq "the key without a value"  "$(lxcf_parse_state 'State:')"                  ""
eq "a different key entirely" "$(lxcf_parse_state 'PID:            1234')"    ""
# Discrimination control. Every negative above would pass against a parser that
# returned empty unconditionally, which would be worthless — so assert that a real
# state and a refusal are actually different answers.
if [ "$(lxcf_parse_state 'State:          RUNNING')" = "$(lxcf_parse_state '')" ]; then
    fail "a known state and a refusal are distinct" "the parser does not discriminate"
else
    pass "a known state and a refusal are distinct"
fi

echo ""
echo "=== lxcf_parse_bridge: read from the container's own config ==="
# /var/lib/lxc/NAME/config, the file docker-in-lxc already reads for this repo's LXC.
eq "the documented key" "$(lxcf_parse_bridge 'lxc.net.0.link = lxcbr0')" "lxcbr0"
eq "no spaces around =" "$(lxcf_parse_bridge 'lxc.net.0.link=lxcbr0')"   "lxcbr0"
eq "amongst other keys" "$(lxcf_parse_bridge \
    $'lxc.uts.name = box\nlxc.net.0.type = veth\nlxc.net.0.link = virbr1\nlxc.net.0.flags = up')" \
    "virbr1"
# A commented-out key is not configuration. Matching it would report a bridge the
# container is not on, and group it with containers that are.
eq "a commented key is not a value" \
    "$(lxcf_parse_bridge $'#lxc.net.0.link = lxcbr0\nlxc.net.0.type = veth')" ""
eq "a commented key with a space" \
    "$(lxcf_parse_bridge '# lxc.net.0.link = lxcbr0')" ""
# The first interface wins, and it is named explicitly. A container with two NICs has
# two bridges and one group row; this pins which one, rather than leaving it to
# whichever the matcher happened to reach last.
eq "the first interface wins" "$(lxcf_parse_bridge \
    $'lxc.net.0.link = lxcbr0\nlxc.net.1.link = virbr1')" "lxcbr0"
eq "no network configured"    "$(lxcf_parse_bridge 'lxc.uts.name = box')"  ""
eq "an empty config"          "$(lxcf_parse_bridge '')"                    ""
# `lxc.network.link` is the pre-2.1 spelling. It is NOT accepted: this repo installs
# LXC 4.x, and quietly honouring a key that version does not read would report a
# bridge from a config LXC itself is ignoring.
eq "the pre-2.1 key is not honoured" \
    "$(lxcf_parse_bridge 'lxc.network.link = lxcbr0')" ""

echo ""
echo "=== lxcf_bridge_label: 'no network' and 'could not read' are not one answer ==="
# The distinction the bridge groups depend on. `bridge_names` offers one group row per
# bridge in use, so a container whose bridge is UNKNOWN must not join the "no network"
# pile — it would be a container the bridge groups can never reach, filed under a
# claim about the machine that was never established.
#
# Plan 00109 had this exact defect twice. `probe.dkms_registry` returned `[]` for both
# "no state directory" and "directory present, no modules", and the fix was to carry
# the two apart in the type rather than to special-case one of them.
eq "a readable config naming a bridge"  "$(lxcf_bridge_label 0 'lxc.net.0.link = lxcbr0')" "lxcbr0"
eq "a readable config with no network"  "$(lxcf_bridge_label 0 'lxc.uts.name = box')"      "$BRIDGE_NONE"
eq "an empty but readable config"       "$(lxcf_bridge_label 0 '')"                        "$BRIDGE_NONE"
# rc != 0 means `sudo cat` failed, and the second argument is then whatever it wrote
# on stderr. It must not be parsed as configuration, however bridge-shaped it looks.
eq "an unreadable config"               "$(lxcf_bridge_label 1 'cat: …/config: Permission denied')" "$BRIDGE_UNREADABLE"
eq "unreadable wins over parseable content" \
    "$(lxcf_bridge_label 1 'lxc.net.0.link = lxcbr0')" "$BRIDGE_UNREADABLE"
eq "a non-1 failure is still unreadable" "$(lxcf_bridge_label 126 '')"                    "$BRIDGE_UNREADABLE"
# The three answers must be mutually distinct, or every case above passes against a
# function that returns one constant.
if [ "$BRIDGE_NONE" = "$BRIDGE_UNREADABLE" ]; then
    fail "the two non-bridge labels are distinct" "both are: $BRIDGE_NONE"
else
    pass "the two non-bridge labels are distinct"
fi
# Neither label may be mistakable for a real bridge name: both are shown in the table
# beside real ones, and `bridge_names` filters on exactly these strings. A Linux
# interface name cannot contain whitespace, so whitespace is what makes that filter
# safe — assert the property rather than trusting the chosen wording. This assertion
# already earned its place: it failed on a first label of "(unreadable)", which has
# none, and the label was changed rather than the test.
for _label in "$BRIDGE_NONE" "$BRIDGE_UNREADABLE"; do
    case "$_label" in
        *[[:space:]]*) pass "'$_label' cannot collide with an interface name" ;;
        *) fail "'$_label' could collide with an interface name" \
            "it has no whitespace, so a bridge could legally be called this" ;;
    esac
done

echo ""
echo "──────────────────────────────────────────────────────────────"
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"

# A suite that discovers nothing exits 0 and reports clean, which is the failure this
# whole file exists to prevent in the tool it tests.
if [ "$PASSED" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
echo "OK"

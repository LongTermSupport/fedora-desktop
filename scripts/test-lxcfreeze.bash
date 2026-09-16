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
# WHAT MOVED, AND WHERE IT WENT (Plan 00122 Task 4.2). The tool's menu and its decisions
# are now the shared library both freeze tools source, so the cases that drove
# `lxcf_index_of`, `lxcf_count_in_state`, `lxcf_infer_action`, `lxcf_target_effect` and
# `lxcf_partition` moved to scripts/test-freezelib.bash — unchanged in what they assert,
# and now driven under BOTH engines' state vocabularies rather than only LXC's, which is
# strictly more than they could say here. What is left in this file is what is genuinely
# LXC's: the two parsers, the bridge label, the bridge group axis, and the hooks through
# which the library reaches this engine.
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
#
# The list spans both halves on purpose: the LXC-specific decisions this file drives, and
# the shared ones the library must have brought in through the tool's `source`. A library
# that failed to load would otherwise surface as a pile of confusing case failures.
for fn in lxcf_parse_state lxcf_parse_bridge lxcf_bridge_label bridge_names \
    lxcf_parse_ipv4 lxcf_ipv4_label renew_dhcp_lease \
    select_bridge load_inventory do_list assert_lxc assert_sudo \
    freeze_hook_preflight freeze_hook_refresh freeze_hook_menu_rows \
    freeze_hook_select freeze_hook_act freeze_hook_table_header freeze_hook_table_row \
    inventory_index_of count_in_state infer_action target_effect freeze_partition \
    select_all select_names print_table interactive_loop; do
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

# contains <description> <haystack> <needle>
contains() {
    case "$2" in
        *"$3"*) pass "$1" ;;
        *) fail "$1" "wanted a mention of: $3" "got: $2" ;;
    esac
}

# lines_as_words <command...> — flatten a one-name-per-line emitter to one string, so a
# case asserts ORDER and not merely membership. These emitters feed a menu whose row
# numbers the user types, so the order is part of the answer.
lines_as_words() {
    local -a got=()
    mapfile -t got < <("$@")
    printf '%s' "${got[*]-}"
}

# count_with_prefix <prefix> <value>... — how many of the values start with it. A shell
# loop rather than `grep -c`, because grep exits 1 when the count is zero and the
# error-swallowing suffix that would paper over that is the shape this repo gates on.
count_with_prefix() {
    local prefix="$1"
    shift
    local value n=0
    for value in "$@"; do
        if [ "${value#"$prefix"}" != "$value" ]; then
            n=$((n + 1))
        fi
    done
    printf '%s' "$n"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

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
#
# `kilo` carries BRIDGE_NONE, so every bridge-group case has a container that
# belongs to no bridge row and must not be swept into one.
# ---------------------------------------------------------------------------
fixture() {
    INV_NAME=(zulu mike alpha kilo bravo)
    INV_STATE=(FROZEN FROZEN RUNNING FROZEN RUNNING)
    INV_BRIDGE=(lxcbr0 virbr1 lxcbr0 "$BRIDGE_NONE" virbr1)
    # RFC 5737 documentation addresses, per CLAUDE/ExampleValues.md — a real 10/8 or
    # 192.168/16 address here would be a private IP committed to a public repository.
    #
    # `mike` has no address while FROZEN and `kilo` has none because it is on no bridge:
    # two different reasons for the same blank cell, and neither may be mistaken for a
    # running container that lost its lease — which is the symptom this column exists for.
    INV_IPV4=(192.0.2.11 "" 192.0.2.12 "" 198.51.100.5)
    ACTION=""
    SELECTED=()
}

echo ""
echo "=== the tool declared itself to the shared library ==="
# The library refuses to load without these, so their mere presence proves little; what
# matters is that they say what LXC says. A tool that declared podman's words would
# inventory containers it then refused to act on, with no other symptom.
eq "it names itself for every message"  "$FREEZE_TOOL"          "lxcfreeze"
eq "the running state is LXC's word"    "$FREEZE_STATE_RUNNING" "RUNNING"
eq "the frozen state is LXC's word"     "$FREEZE_STATE_FROZEN"  "FROZEN"
# STATE_* is what the parser reads and FREEZE_STATE_* is what the library compares
# against. If those two ever disagreed, a container would parse as RUNNING and then
# match neither branch of the partition — inventoried, offered, and acted on never.
eq "the parser's word and the library's are the same" "$STATE_RUNNING" "$FREEZE_STATE_RUNNING"
eq "and so are the frozen pair"                       "$STATE_FROZEN"  "$FREEZE_STATE_FROZEN"
# The unknown-name error gains LXC's extra sentence: a STOPPED container is not in
# the inventory, and without saying so the message reads as "no such container".
contains "the list note explains the STOPPED exclusion" "$FREEZE_LIST_NOTE" "STOPPED"

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
echo "=== bridge_names: the bridges in use, and only those ==="
fixture
# Sorted and deduplicated: `lxcbr0` is held by two containers and `virbr1` by two more,
# and the inventory presents them interleaved rather than grouped.
eq "each bridge once, sorted" "$(lines_as_words bridge_names)" "lxcbr0 virbr1"
# Neither non-bridge label is offered as a group. "no network" is not a bridge and a
# row for it would read as one; "could not read" is not a set anyone can ask to freeze,
# and a row for it would promise to act on containers nobody has placed.
fixture
INV_BRIDGE[4]="$BRIDGE_UNREADABLE"
eq "the two non-bridge labels are not bridges" \
    "$(lines_as_words bridge_names)" "lxcbr0 virbr1"
fixture
INV_BRIDGE=("$BRIDGE_NONE" "$BRIDGE_NONE" "$BRIDGE_UNREADABLE" "$BRIDGE_NONE" "$BRIDGE_NONE")
eq "a machine with no bridges at all offers none" "$(lines_as_words bridge_names)" ""

echo ""
echo "=== select_bridge: a bridge in use, and one that is not ==="
fixture
select_bridge lxcbr0
# zulu is index 0 and alpha index 2, with mike between them: inventory order, and not
# adjacent, so a loop that stopped at the first match returns half the group.
eq "its members, in inventory order" "${SELECTED[*]}" "zulu alpha"
fixture
select_bridge virbr1
eq "the other bridge's members"      "${SELECTED[*]}" "mike bravo"
# The selection is rebuilt, not appended to — the interactive loop calls this again on
# every pass, and a stale member is how a row acts on a container that left the bridge
# a screen ago.
fixture
SELECTED=(stale_name)
select_bridge lxcbr0
eq "a second selection replaces the first" "${SELECTED[*]}" "zulu alpha"

# An unknown bridge RETURNS non-zero; it does not exit. The menu depends on that to
# re-prompt when a bridge empties between being drawn and being chosen, and the CLI
# path turns the same status into a hard failure of its own accord.
fixture
if select_bridge nosuchbr 2> "$work/bridge.err"; then
    fail "an unknown bridge is refused" "select_bridge nosuchbr succeeded"
else
    pass "an unknown bridge is refused"
fi
bridge_err="$(cat "$work/bridge.err")"
contains "it names the bridge"       "$bridge_err" "nosuchbr"
contains "and lists the ones in use" "$bridge_err" "lxcbr0"
# A machine where NOTHING is on a bridge gets a different message: there is no list to
# offer, and "in use right now:" followed by nothing would leave the reader with no
# next step.
fixture
INV_BRIDGE=("$BRIDGE_NONE" "$BRIDGE_NONE" "$BRIDGE_NONE" "$BRIDGE_NONE" "$BRIDGE_NONE")
if select_bridge lxcbr0 2> "$work/nobridge.err"; then
    fail "a machine with no bridges refuses too" "select_bridge succeeded"
else
    pass "a machine with no bridges refuses too"
fi
nobridge_err="$(cat "$work/nobridge.err")"
contains "saying nothing is on any bridge" "$nobridge_err" "any bridge"
if [ "$bridge_err" = "$nobridge_err" ]; then
    fail "the two bridge refusals are distinct" "both say: $bridge_err"
else
    pass "the two bridge refusals are distinct"
fi

echo ""
echo "=== the table hook: the BRIDGE column is this tool's ==="
fixture
# The column is PADDED, not bare: the drill-down menu prints the verb after it, so an
# unpadded value there makes the verb column ragged. `trim` compares the content and
# the width assertions below compare the shape, because asserting only the trimmed
# value would pass against the unpadded version this replaced.
trim() { local s="$1"; s="${s%"${s##*[![:space:]]}"}"; printf '%s' "$s"; }
# The row is two fixed-width columns now, so trimming the whole thing tests neither of
# them: it returns the LAST non-blank cell, which passes for the wrong reason whenever the
# one being asserted happens to be last. Each column is cut by position instead.
#
# The width comes from the TOOL's own constant, not a 16 repeated here. Three copies of a
# number — header, row, and this cutter — is the "two things that must agree" shape, and
# the cutter's copy is the one that would silently start reading the wrong bytes.
col() {
    local row="$1" n="$2"
    trim "${row:$(( (n - 1) * TABLE_COL_WIDTH )):TABLE_COL_WIDTH}"
}
eq "the header names both columns" "$(freeze_hook_table_header)" \
    "$(printf "%-${TABLE_COL_WIDTH}s%-${TABLE_COL_WIDTH}s" BRIDGE IPV4)"
eq "a row carries the bridge" "$(col "$(freeze_hook_table_row 0)" 1)"  "lxcbr0"
# The label for a container on no bridge is shown VERBATIM rather than blanked: a blank
# cell reads as "unknown", and the whole point of the two labels is that they are not.
eq "and the no-network label, verbatim" "$(col "$(freeze_hook_table_row 3)" 1)" "$BRIDGE_NONE"
# Header and row must be the SAME width or the header stops sitting over its column,
# and both must be wide enough for the longest value the column can hold.
eq "the header and a row are the same width" \
    "$(freeze_hook_table_header | wc -c)" "$(freeze_hook_table_row 0 | wc -c)"
# The BRIDGE COLUMN, not the whole row. Measuring the row against one column's longest
# label was loose with one column and is vacuous with two: a second 16-wide column pads
# the total, so narrowing BRIDGE to `%-8s` would ragged the table and still pass. The
# cut is taken before trimming, so what is measured is the field rather than its content.
fixture
INV_BRIDGE[0]="$BRIDGE_UNREADABLE"
widest_row="$(freeze_hook_table_row 0)"
bridge_field="${widest_row:0:TABLE_COL_WIDTH}"
if [ "${#bridge_field}" -gt "${#BRIDGE_UNREADABLE}" ]; then
    pass "the bridge column is wider than the longest label it can hold"
else
    fail "the bridge column is wider than the longest label it can hold" \
        "field ${#bridge_field} <= label ${#BRIDGE_UNREADABLE}, so they would touch"
fi
eq "and the longest label survives the column intact" \
    "$(col "$(freeze_hook_table_row 0)" 1)" "$BRIDGE_UNREADABLE"
# print_table is the library's, and this is the assembled result — the shared columns
# plus this engine's, which is the seam most likely to be wired up wrong.
fixture
table="$(print_table alpha kilo)"
contains "the assembled table has the shared columns" "$table" "NAME"
contains "and this engine's column"                   "$table" "BRIDGE"
contains "a row names its container"                  "$table" "alpha"
contains "with its state"                             "$table" "RUNNING"
contains "and its bridge"                             "$table" "lxcbr0"
contains "and its address"                            "$table" "192.0.2.12"

echo ""
echo "=== the IPV4 column: a blank next to RUNNING is the symptom, so it must be visible ==="
# The inventory arrays are index-parallel and nothing enforces that, so a fixture one
# entry short would silently give every row past it the wrong address.
fixture
eq "the fixture's addresses are index-parallel with its names" \
    "${#INV_IPV4[@]}" "${#INV_NAME[@]}"
# Task 5.3. A thawed container whose DHCP lease expired is RUNNING with no address, and
# the list is where someone looks first — but only if the list carries the address at all.
fixture
eq "a row carries the address"        "$(col "$(freeze_hook_table_row 2)" 2)" "192.0.2.12"
# BLANK, not a placeholder. `lxc-info -iH` prints nothing for a container with no
# address, and inventing a word for it would make "no address" and "this tool did not
# ask" look alike — the distinction BRIDGE_NONE/BRIDGE_UNREADABLE exists to preserve.
eq "no address prints as blank"       "$(col "$(freeze_hook_table_row 1)" 2)" ""
# …and the row still carries its bridge, so the blank is the ADDRESS being absent rather
# than the row being short. A single-column assertion cannot tell those apart.
eq "the blank row still has its bridge" "$(col "$(freeze_hook_table_row 1)" 1)" "virbr1"
eq "the header and a row stay the same width" \
    "$(freeze_hook_table_header | wc -c)" "$(freeze_hook_table_row 1 | wc -c)"

# The parser, against what `lxc-info -n NAME -iH` really prints.
eq "one IPv4 line"          "$(lxcf_parse_ipv4 '192.0.2.11')"            "192.0.2.11"
eq "surrounding whitespace is trimmed" "$(lxcf_parse_ipv4 '  192.0.2.11  ')" "192.0.2.11"
eq "no address at all"      "$(lxcf_parse_ipv4 '')"                      ""
# `-i` prints every address, and on a dual-stack container the IPv6 one can come first.
# Taking "the first line" would put an IPv6 address in a column headed IPV4.
eq "IPv6 first is skipped"  "$(lxcf_parse_ipv4 "$(printf '2001:db8::1\n192.0.2.11\n')")" "192.0.2.11"
eq "IPv6 only is no IPv4"   "$(lxcf_parse_ipv4 '2001:db8::1')"           ""
# Two IPv4 addresses is a real shape (two interfaces). One column shows the first, which
# is a choice rather than an accident — the drill-down is where the rest belong.
eq "the first IPv4 of several" \
    "$(lxcf_parse_ipv4 "$(printf '192.0.2.11\n198.51.100.5\n')")" "192.0.2.11"
# lxc-info's failure text must never be shown as if it were an address.
eq "an error message is not an address" \
    "$(lxcf_parse_ipv4 'lxc-info: container not running')" ""

# A grep that FAILED is not a container with no address. grep exits 1 for "no match",
# which is an answer; anything above that is grep itself breaking, and returning "" for
# it would report "no address" from a probe that never ran. The tool dies instead — and
# without this case, replacing the `die` with an empty printf leaves the suite green.
grep() { return 2; }
if ipv4_out="$(lxcf_parse_ipv4 '192.0.2.11' 2>&1)"; then
    fail "a broken grep is fatal, not an empty address" "it returned '$ipv4_out'"
else
    pass "a broken grep is fatal, not an empty address"
    contains "and says grep is what failed" "$ipv4_out" "grep failed (status 2)"
fi
unset -f grep

echo ""
echo "=== the IPV4 label: 'no address' and 'could not ask' are not one answer ==="
# The distinction lxcf_bridge_label already draws, and it matters MORE here. A blank
# IPV4 cell is load-bearing — it IS the expired-lease symptom — so a failed probe that
# printed blank would make a column that cannot answer look like a machine that has lost
# every address. An `lxc-info` whose `-i` is unavailable would do exactly that to every
# row at once, and nothing would say the column was blind.
eq "an address is the address"    "$(lxcf_ipv4_label 0 '192.0.2.11')"  "192.0.2.11"
eq "a readable probe with no address is BLANK" "$(lxcf_ipv4_label 0 '')" ""
eq "a failed probe is not blank"  "$(lxcf_ipv4_label 1 'lxc-info: unrecognised option')" \
    "$IPV4_UNREADABLE"
eq "a non-1 failure is also unreadable" "$(lxcf_ipv4_label 126 '')" "$IPV4_UNREADABLE"
# The two must stay distinguishable, which a shared empty value would end.
if [ -z "$IPV4_UNREADABLE" ]; then
    fail "the unreadable label is not itself blank" "IPV4_UNREADABLE is empty"
else
    pass "the unreadable label is not itself blank"
fi
# A label that filled the column would run into the next one, which is exactly what
# `(could not read)` did before the width was widened past it.
if [ "${#IPV4_UNREADABLE}" -lt "$TABLE_COL_WIDTH" ]; then
    pass "and it fits the column with a gap after it"
else
    fail "and it fits the column with a gap after it" \
        "${#IPV4_UNREADABLE} characters in a 16-wide column"
fi
# It reaches the table, not just the label function.
fixture
INV_IPV4[1]="$IPV4_UNREADABLE"
eq "an unreadable address reaches the row" \
    "$(col "$(freeze_hook_table_row 1)" 2)" "$IPV4_UNREADABLE"

echo ""
echo "=== the freeze-time note: what a long freeze costs, said before it costs it ==="
# Task 5.4. Thaw renews the lease, so the container comes back reachable — but every ssh
# session into it, and any agent socket forwarded over one, died with the frozen TCP
# connection. The user otherwise learns this from a `git push` that hangs.
if [ -n "${FREEZE_FREEZE_NOTE:-}" ]; then
    pass "the tool declares a freeze-time note"
else
    fail "the tool declares a freeze-time note" "FREEZE_FREEZE_NOTE is empty or unset"
fi
contains "it names the connection loss"  "$FREEZE_FREEZE_NOTE" "ssh"
contains "and says reconnecting is the fix" "$FREEZE_FREEZE_NOTE" "Reconnect"
# The lease renewal is the half that DOES survive, and saying only the bad half would
# send someone hunting a network fault that thaw already handled.
contains "and that the lease itself is renewed" "$FREEZE_FREEZE_NOTE" "lease"

echo ""
echo "=== the menu hook: groups, and no per-container rows ==="
# What lxcfreeze GAINED by adopting the library: the top-level menu is groups only,
# and the containers are reached by drilling into one — where they can be seen, chosen
# several at a time, and backed out of. The flat list this tool shipped with could do
# none of that, and was the UX divergence Phase 4 exists to close.
fixture
FREEZE_MENU_KEYS=()
FREEZE_MENU_LABELS=()
freeze_hook_menu_rows
eq "everything first, then one row per bridge in use" \
    "${FREEZE_MENU_KEYS[*]}" "all bridge:lxcbr0 bridge:virbr1"
eq "and no row per container" \
    "$(count_with_prefix 'name:' "${FREEZE_MENU_KEYS[@]}")" "0"
contains "the first row says what acting on everything would do" \
    "${FREEZE_MENU_LABELS[0]}" "FREEZE 2"
# Each bridge row counts ITS OWN members, not the machine: lxcbr0 holds one running
# container and virbr1 holds one. A row that counted the inventory would promise to
# act on containers outside the group it names.
contains "a bridge row names the bridge"   "${FREEZE_MENU_LABELS[1]}" "lxcbr0"
contains "and counts only its own members" "${FREEZE_MENU_LABELS[1]}" "FREEZE 1"
# A bridge holding nothing that can be frozen still gets a row, saying what it CAN do.
# Hiding it would leave the user wondering where a bridge they can see went.
fixture
INV_STATE=(FROZEN FROZEN FROZEN FROZEN FROZEN)
FREEZE_MENU_KEYS=()
FREEZE_MENU_LABELS=()
freeze_hook_menu_rows
contains "a wholly frozen bridge offers to thaw it" "${FREEZE_MENU_LABELS[1]}" "THAW"

echo ""
echo "=== the select hook: keys in, a selection out ==="
fixture
freeze_hook_select all
eq "the 'all' key selects everything" "${SELECTED[*]}" "zulu mike alpha kilo bravo"
fixture
freeze_hook_select "bridge:virbr1"
eq "a bridge key selects its members" "${SELECTED[*]}" "mike bravo"
# A bridge that emptied out between the menu being drawn and the row being chosen is a
# RECOVERABLE condition: the hook returns non-zero and the library's loop re-prompts.
# Exiting here would end a session the user was in the middle of.
fixture
if freeze_hook_select "bridge:nosuchbr" 2> "$work/hook.err"; then
    fail "an unknown bridge key returns rather than selecting" "the hook succeeded"
else
    pass "an unknown bridge key returns rather than selecting"
fi
# A key the hook does not recognise is an internal error, not a re-prompt: the library
# built that key from a row this hook supplied, so a mismatch is a bug rather than a
# stale menu.
if hook_out="$(freeze_hook_select "nonsense:key" 2>&1)"; then
    fail "an unrecognised key is fatal" "the hook succeeded"
else
    pass "an unrecognised key is fatal"
    contains "and names the key it did not understand" "$hook_out" "nonsense:key"
fi

echo ""
echo "=== the act hook: the verb maps to LXC's two commands ==="
# The hook is not RUN here — it shells out to sudo, and this container has no lxc. Its
# TEXT is read instead, which is enough to catch the mapping being inverted: a freeze
# that called lxc-unfreeze would thaw everything the user asked to freeze, and nothing
# else that can run in here would see it.
hook_body="$(declare -f freeze_hook_act)"
contains "freeze reaches for lxc-freeze"              "$hook_body" "lxc-freeze -n"
contains "thaw reaches for lxc-unfreeze"              "$hook_body" "lxc-unfreeze -n"
contains "and both escalate, as rootful LXC requires" "$hook_body" "sudo"
# Presence is not pairing: an inverted mapping contains both commands too. The freeze
# command must be the branch taken when the action IS freeze.
mapfile -t hook_lines <<< "$hook_body"
freeze_branch=""
for hook_i in "${!hook_lines[@]}"; do
    case "${hook_lines[$hook_i]}" in
        *'= "freeze"'*) freeze_branch="${hook_lines[$((hook_i + 1))]}" ;;
    esac
done
contains "the freeze branch is the one that freezes" "$freeze_branch" "lxc-freeze -n"

# Thaw does one more thing than freeze undoes: it renews the container's DHCP
# lease, because a freeze longer than the lease leaves the container thawed but
# unreachable for minutes. The renewal must sit on the thaw branch only — a freeze
# that reconnected the network would drop the address it is about to freeze.
if [[ "$freeze_branch" == *renew_dhcp_lease* ]]; then
    fail "and freeze does not touch the network" "the freeze branch renews the lease"
else
    pass "and freeze does not touch the network"
fi
renew_body="$(declare -f renew_dhcp_lease)"
contains "the renewal runs inside the container"        "$renew_body" "lxc-attach -n"
contains "through NetworkManager"                       "$renew_body" "nmcli device connect"
contains "on the devices it reports, not an assumed one" "$renew_body" "nmcli -t -f DEVICE,TYPE device status"
contains "and a failed renewal says the container IS thawed" "$renew_body" "thawed, but"

echo ""
echo "=== the act hook, RUN: the thaw's status is the hook's status ==="
# The hook IS run here. The earlier claim that it could not be — "it shells out to sudo,
# and this container has no lxc" — was wrong, and the wrongness mattered: reading the
# source for `renew_dhcp_lease` on the line AFTER `lxc-unfreeze` asserts that two lines
# are adjacent, which is not the property. What matters is whether the FIRST one's exit
# status survives, and a text scan cannot tell the broken form from the fixed one. It
# takes a `sudo` shell function to find out.
#
# Why this is a defect that reaches a user: the library calls the hook as
# `if out="$(freeze_hook_act …)"`, and bash SUSPENDS errexit inside a command
# substitution whose value is being tested. So the hook returns whatever its last command
# returned — and with an unguarded renewal after a failed unfreeze, a reconnect that
# happens to succeed makes `lxcfreeze thaw` print `✓ name` and exit 0 for a container
# that is still frozen.
#
# `sudo` is defined as a function rather than a script on PATH so the stub is visible to
# the hook without touching the filesystem, and `unset` at the end so nothing after this
# block inherits it. Output goes to a file rather than /dev/null: the assertions below
# read what the hook said, and a discarded stream cannot be asserted on.
# The log is a FILE, not a variable. `renew_dhcp_lease` captures each `lxc-attach` with
# `$( … )`, which is a subshell — a variable appended to in there never reaches the
# caller, so a variable-based log would show the reconnect never happening and an
# assertion on it would pass for the wrong reason.
act_log="$work/act-sudo.log"
sudo() {
    printf '%s\n' "$*" >> "$act_log"
    case "$*" in
        *"lxc-unfreeze"*) return "$STUB_UNFREEZE_RC" ;;
        *"nmcli -t -f DEVICE,TYPE device status"*) printf 'eth0:ethernet\n'; return 0 ;;
        *"nmcli device connect"*) return "$STUB_CONNECT_RC" ;;
        *"lxc-freeze"*) return "$STUB_FREEZE_RC" ;;
        *) return 0 ;;
    esac
}
STUB_UNFREEZE_RC=0
STUB_CONNECT_RC=0
STUB_FREEZE_RC=0

# The control. Both halves succeed, so the hook succeeds — without this, every assertion
# below could be passing because the hook fails unconditionally.
: > "$act_log"
if freeze_hook_act thaw demo > "$work/act.out" 2>&1; then
    pass "a thaw whose unfreeze and renewal both succeed succeeds"
else
    fail "a thaw whose unfreeze and renewal both succeed succeeds" "$(cat "$work/act.out")"
fi
contains "and it did reach the renewal" "$(cat "$act_log")" "nmcli device connect"

# THE ONE THAT WAS BROKEN. The unfreeze fails and the reconnect succeeds — which is not
# contrived: a container that never thawed may well still answer at its old address.
STUB_UNFREEZE_RC=1
STUB_CONNECT_RC=0
: > "$act_log"
if freeze_hook_act thaw demo > "$work/act.out" 2>&1; then
    fail "a failed unfreeze fails the hook even when the renewal would succeed" \
        "the hook reported success for a container that is still frozen"
else
    pass "a failed unfreeze fails the hook even when the renewal would succeed"
fi
# Not merely "the hook failed" — the renewal must not have RUN. Its own failure message
# says "thawed, but the lease was not renewed", which is a lie about a container that was
# never thawed, and running it at all is the decoupling the fail-fast rule forbids.
if grep -q nmcli "$act_log"; then
    fail "and the renewal does not run after a failed unfreeze" \
        "the renewal ran: $(cat "$act_log")"
else
    pass "and the renewal does not run after a failed unfreeze"
fi

# The renewal's own failure still fails the hook, so the two guards are independent
# rather than one masking the other.
STUB_UNFREEZE_RC=0
STUB_CONNECT_RC=1
if hook_out="$(freeze_hook_act thaw demo 2>&1)"; then
    fail "a failed renewal after a good unfreeze still fails the hook" "the hook succeeded"
else
    pass "a failed renewal after a good unfreeze still fails the hook"
    contains "and says the container IS thawed" "$hook_out" "thawed, but"
fi

# Freeze is unaffected by all of this: its status is its own, and it never renews.
STUB_UNFREEZE_RC=0
STUB_CONNECT_RC=0
STUB_FREEZE_RC=1
: > "$act_log"
if freeze_hook_act freeze demo > "$work/act.out" 2>&1; then
    fail "a failed freeze fails the hook" "the hook succeeded"
else
    pass "a failed freeze fails the hook"
fi
if grep -q nmcli "$act_log"; then
    fail "and freeze never touches the network" "the freeze branch ran nmcli"
else
    pass "and freeze never touches the network"
fi
unset -f sudo

echo ""
echo "=== the preflight hook: both guards, and neither one alone ==="
# LXC absent and sudo refused are different failures with different remedies, and
# neither may be skipped: one is a missing dependency with an IaC fix, the other is a
# question that was never asked. A preflight that ran only one of them would let the
# other produce an empty inventory that reads as "no containers".
preflight_body="$(declare -f freeze_hook_preflight)"
contains "it checks LXC is installed" "$preflight_body" "assert_lxc"
contains "and that root is available" "$preflight_body" "assert_sudo"

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

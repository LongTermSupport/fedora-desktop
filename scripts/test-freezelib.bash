#!/usr/bin/env bash
# Unit-test the shared freeze library (Plan 00122 Task 4.2).
#
# WHAT THIS SUITE OWNS, AND WHY IT IS NOT A THIRD COPY OF THE OTHER TWO.
# `scripts/test-podfreeze.bash` pins what podman's tool does and
# `scripts/test-lxcfreeze.bash` what LXC's does. Both now reach the decisions and the
# menu through this library, so the cases that belong here are the ones NEITHER of
# them can make:
#
#   1. The same decision, driven under BOTH engines' state vocabularies. podman says
#      running/paused and LXC says RUNNING/FROZEN, and every decision case below runs
#      twice, once in each. A library that hardcoded either word passes one pass and
#      fails the other — precisely the mutant a single-vocabulary suite cannot kill.
#   2. `do_action`. It is the half that actually acts, it was explicitly out of scope
#      for the pin suite, and it is now shared — so a defect in it is a defect in both
#      tools at once. Its act/skip/vanished reporting, its dry run and its per-target
#      failure accounting are driven here against a recording hook.
#   3. `interactive_loop`'s control flow. A recoverable group failure must RE-PROMPT
#      rather than end the session, which is the interactive rule
#      (CLAUDE/InteractiveScripts.md) and the one thing no CLI path exercises.
#   4. The contract itself: the library refuses to load without an engine, refuses two
#      identical state words, refuses to run as a program, and refuses to open a menu
#      with a hook missing.
#
# THE ENGINE IS THE TEST'S. The library reaches an engine only through freeze_hook_*,
# so this file defines those hooks and IS the engine for the duration. Nothing shells
# out and no container is needed — which is the point, since neither real engine is
# reachable from the container this suite runs in.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the
# full picture, and each result is checked explicitly. Unlike the two tool suites,
# sourcing the library does NOT bring errexit with it — it sets no shell options at
# all, and that is asserted below rather than assumed, because a library that started
# setting them would silently change how every caller's failures behave.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/files/home/.local/lib/freeze/freeze-common.bash"

if [ ! -f "$LIB" ]; then
    echo "FAIL: the freeze library is not at $LIB" >&2
    exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
# Where a case parks output it is not asserting on. Never /dev/null: a discarded
# stream is one nobody can look at when the case fails.
QUIET="$work/quiet.log"

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

# lacks <description> <haystack> <needle>
lacks() {
    case "$2" in
        *"$3"*) fail "$1" "did not want a mention of: $3" "got: $2" ;;
        *) pass "$1" ;;
    esac
}

# ok / notok <description> <command...> — run a PREDICATE and assert its status.
# Stdout goes to a file rather than the terminal: several of these print an answer
# when they succeed, and splicing that onto a failure report breaks its format.
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

# ---------------------------------------------------------------------------
# The contract, driven in CHILD SHELLS.
#
# Every refusal below is an `exit` at file scope — which is right, because a
# misconfigured library must not go on to compare states against the empty string —
# and that would take this suite with it. So each is driven as its own `bash -c`.
# The suite's own variables are not exported, so a child starts with none of them.
# ---------------------------------------------------------------------------

# A complete, minimal engine declaration, which the child shells source.
cat > "$work/config.bash" << 'CONFIG'
FREEZE_TOOL="testfreeze"
FREEZE_STATE_RUNNING="running"
FREEZE_STATE_FROZEN="paused"
FREEZE_HOST_ONLY_NOTE="  the engine is not reachable from in here."
FREEZE_TARGET_HINT="NAME..., or --all"
FREEZE_LIST_NOTE=""
CONFIG

# load_probe <extra-shell-code> — source the library in a child shell with the config
# applied, plus whatever this case wants changed. Echoes the child's output; its
# status is the child's.
load_probe() {
    bash -c "set -uo pipefail
        source '$work/config.bash'
        $1
        source '$LIB'
        echo LOADED" 2>&1
}

echo ""
echo "=== the contract: the library refuses to load without an engine ==="
if probe_out="$(load_probe ':')"; then
    pass "a complete declaration loads"
    contains "and says nothing else on the way in" "$probe_out" "LOADED"
else
    fail "a complete declaration loads" "it refused: $probe_out"
fi
# Each setting is checked BY NAME, so no case here can pass against a library that
# refuses everything for one reason.
for setting in FREEZE_TOOL FREEZE_STATE_RUNNING FREEZE_STATE_FROZEN \
    FREEZE_HOST_ONLY_NOTE FREEZE_TARGET_HINT; do
    if probe_out="$(load_probe "$setting=''")"; then
        fail "an empty $setting is refused" "the library loaded anyway"
    else
        pass "an empty $setting is refused"
        contains "and it names $setting" "$probe_out" "$setting"
    fi
done
# FREEZE_LIST_NOTE is the one OPTIONAL setting — podfreeze has nothing to add to the
# unknown-name error and lxcfreeze does. Empty must therefore NOT be a refusal, or
# the loop above would be refusing a legitimate declaration.
if probe_out="$(load_probe "FREEZE_LIST_NOTE=''")"; then
    pass "an empty FREEZE_LIST_NOTE is accepted — it is the optional one"
else
    fail "an empty FREEZE_LIST_NOTE is accepted — it is the optional one" "$probe_out"
fi

# The two state words must DIFFER. Collapsed into one string, every container would
# be both a freeze target and a thaw target: the derived verb would always say
# freeze, and the partition would never skip anything.
if probe_out="$(load_probe "FREEZE_STATE_FROZEN=\"\$FREEZE_STATE_RUNNING\"")"; then
    fail "two identical state words are refused" "the library loaded anyway"
else
    pass "two identical state words are refused"
    contains "and it says which word they collapsed to" "$probe_out" "running"
fi

echo ""
echo "=== the contract: it is a library, not a program ==="
if run_out="$(bash "$LIB" 2>&1)"; then
    fail "running it as a program is refused" "it exited 0"
else
    pass "running it as a program is refused"
    contains "and it says to source it instead" "$run_out" "source it"
fi

# ---------------------------------------------------------------------------
# From here the library is loaded into THIS shell, with the suite as its engine.
# ---------------------------------------------------------------------------
# shellcheck source=/dev/null
source "$work/config.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/home/.local/lib/freeze/freeze-common.bash
source "$LIB"

# A sourced library must not change its caller's shell options. Both tools set
# `set -euo pipefail` themselves and both their suites assert that it survives; a
# library that set or cleared them would silently change how every caller handles
# failure, and would break the negative cases in this very file.
case "$-" in
    *e*)
        fail "sourcing the library does not turn errexit on" \
            "this suite drives deliberate failures and needs errexit off" ;;
    *) pass "sourcing the library does not turn errexit on" ;;
esac
case "$-" in
    *u*) pass "and it does not turn nounset off" ;;
    *) fail "and it does not turn nounset off" "an unset variable is now silent" ;;
esac

for fn in inventory_index_of count_in_state infer_action target_effect row_verb \
    freeze_partition select_all select_names print_table freeze_menu_row \
    pick_target drill_into_group do_action interactive_loop die have \
    assert_on_host freeze_assert_contract; do
    if ! declare -F "$fn" > /dev/null; then
        echo "FAIL: $fn is not defined after sourcing $LIB" >&2
        echo "      (the shared behaviour is absent, not merely wrong)" >&2
        exit 1
    fi
done

# ---------------------------------------------------------------------------
# The suite's engine. Every hook records what it was asked, so a case can assert
# that the library called it — and how — rather than only what it printed.
# ---------------------------------------------------------------------------
ACT_STATUS=0
ACT_MESSAGE="engine refused"
REFRESH_LOG="$work/refresh.log"
ACT_LOG="$work/act.log"
: > "$ACT_LOG"
: > "$REFRESH_LOG"

declare -a INV_EXTRA=()

freeze_hook_preflight() {
    printf 'preflight\n' >> "$QUIET"
}

freeze_hook_refresh() {
    printf 'x' >> "$REFRESH_LOG"
}

freeze_hook_table_header() {
    printf '%-4s %s' "MARK" "EXTRA"
}

freeze_hook_table_row() {
    local i="$1"
    printf '%-4s %s' "m$i" "${INV_EXTRA[$i]}"
}

freeze_hook_menu_rows() {
    freeze_menu_row "all" "everything" "${INV_NAME[@]+${INV_NAME[@]}}"
}

# `gone` stands in for a group that stopped existing between the menu being drawn
# and the row being chosen, which is the recoverable case the loop must re-prompt on.
# `broke` is the OTHER non-zero: a hook that failed on its way to an answer. The two
# must not be one status — capturing a status suspends errexit through the hook body,
# so a hook that breaks halfway returns 1 exactly like a bare "recoverable" would.
freeze_hook_select() {
    case "$1" in
        all) select_all ;;
        gone) return "$FREEZE_SELECT_GONE" ;;
        broke) return 1 ;;
        empty) SELECTED=() ;;
        *) die "internal error: unknown target key '$1'." ;;
    esac
}

# The engine. Records every call, so a case can prove do_action acted once per
# target, with the right verb, in the right order — and stayed silent when it
# should not have acted at all.
freeze_hook_act() {
    printf '%s %s\n' "$1" "$2" >> "$ACT_LOG"
    if [ "$ACT_STATUS" -ne 0 ]; then
        echo "$ACT_MESSAGE"
        return "$ACT_STATUS"
    fi
    return 0
}

acts() {
    tr '\n' ';' < "$ACT_LOG"
}

reset_engine() {
    : > "$ACT_LOG"
    ACT_STATUS=0
    ACT_MESSAGE="engine refused"
}

# ---------------------------------------------------------------------------
# The fixture, in BOTH vocabularies.
#
# One inventory, described twice: podman says running/paused, LXC says
# RUNNING/FROZEN. Every decision case below runs under both, so a library that
# compared against a literal state word passes one pass and fails the other.
#
# DELIBERATELY out of order, mixed-state and larger than two entries — `charlie` is
# a running container sitting at index 2 behind two frozen ones, so a predicate that
# reads only the first name it is handed gets several answers wrong.
# ---------------------------------------------------------------------------
fixture() {
    local vocabulary="$1"
    if [ "$vocabulary" = "podman" ]; then
        FREEZE_STATE_RUNNING="running"
        FREEZE_STATE_FROZEN="paused"
    else
        FREEZE_STATE_RUNNING="RUNNING"
        FREEZE_STATE_FROZEN="FROZEN"
    fi
    INV_NAME=(alpha bravo charlie delta echo)
    INV_STATE=(
        "$FREEZE_STATE_FROZEN" "$FREEZE_STATE_FROZEN" "$FREEZE_STATE_RUNNING"
        "$FREEZE_STATE_FROZEN" "$FREEZE_STATE_RUNNING"
    )
    INV_EXTRA=(e-alpha e-bravo e-charlie e-delta e-echo)
    SELECTED=()
    ACTION=""
    DRY_RUN=0
    reset_engine
}

for vocab in podman lxc; do
    fixture "$vocab"
    echo ""
    echo "─── the $vocab vocabulary: $FREEZE_STATE_RUNNING / $FREEZE_STATE_FROZEN ───"

    echo ""
    echo "=== inventory_index_of: a name is found, or it is not there ==="
    fixture "$vocab"
    eq "$vocab: the first entry"        "$(inventory_index_of alpha)"   "0"
    eq "$vocab: an entry in the middle" "$(inventory_index_of charlie)" "2"
    eq "$vocab: the last entry"         "$(inventory_index_of echo)"    "4"
    notok "$vocab: an absent name returns non-zero"        inventory_index_of nosuch
    # A prefix or a suffix of a real name must not match it. Substring matching is
    # the quiet way a selection acts on a container nobody chose.
    notok "$vocab: a prefix of a real name does not match" inventory_index_of alph
    notok "$vocab: a suffix of a real name does not match" inventory_index_of harlie
    notok "$vocab: the empty string is not a name"         inventory_index_of ""

    echo ""
    echo "=== count_in_state: counts the NAMES GIVEN, not the inventory ==="
    fixture "$vocab"
    # The whole inventory holds 2 running and 3 frozen, so every subset below has an
    # answer that differs from both: a mutant that ignored its arguments and counted
    # the inventory could not pass.
    eq "$vocab: two frozen out of three named" \
        "$(count_in_state "$FREEZE_STATE_FROZEN" alpha bravo charlie)" "2"
    eq "$vocab: one running out of three named" \
        "$(count_in_state "$FREEZE_STATE_RUNNING" alpha bravo charlie)" "1"
    eq "$vocab: no names at all is zero" "$(count_in_state "$FREEZE_STATE_RUNNING")" "0"
    # An unknown name contributes nothing rather than erroring: the count is a count,
    # and freeze_partition is what reports a name that is not there.
    eq "$vocab: an unknown name does not count" \
        "$(count_in_state "$FREEZE_STATE_RUNNING" charlie nosuch)" "1"
    eq "$vocab: a state nothing is in" "$(count_in_state STOPPED alpha charlie)" "0"

    echo ""
    echo "=== infer_action: anything running gets frozen ==="
    fixture "$vocab"
    SELECTED=(charlie)
    eq "$vocab: one running container" "$(infer_action)" "freeze"
    SELECTED=(alpha)
    eq "$vocab: one frozen container"  "$(infer_action)" "thaw"
    SELECTED=(alpha bravo delta)
    eq "$vocab: a wholly frozen set"   "$(infer_action)" "thaw"
    # THE case a first-element-only predicate fails: two frozen names before the
    # running one.
    SELECTED=(alpha bravo charlie)
    eq "$vocab: a running container behind two frozen" "$(infer_action)" "freeze"
    SELECTED=()
    eq "$vocab: an empty selection"          "$(infer_action)" "thaw"
    SELECTED=(nosuch)
    eq "$vocab: a vanished name infers thaw" "$(infer_action)" "thaw"

    echo ""
    echo "=== target_effect: the menu row can never disagree with the outcome ==="
    fixture "$vocab"
    eq "$vocab: derived, running present" "$(target_effect alpha bravo charlie)" "FREEZE 1"
    eq "$vocab: derived, only frozen"     "$(target_effect alpha bravo)"         "THAW   2"
    eq "$vocab: derived, neither"         "$(target_effect nosuch)"              "nothing to do"
    eq "$vocab: derived, no names at all" "$(target_effect)"                     "nothing to do"
    # The count is of what the verb will TOUCH, not the size of the group.
    eq "$vocab: the label counts the acted-on, not the selected" \
        "$(target_effect alpha bravo delta charlie)" "FREEZE 1"
    ACTION="freeze"
    eq "$vocab: explicit freeze, running present" "$(target_effect alpha charlie)"  "FREEZE 1"
    eq "$vocab: explicit freeze, nothing running" "$(target_effect alpha bravo)"    "nothing to freeze"
    ACTION="thaw"
    eq "$vocab: explicit thaw, frozen present"    "$(target_effect alpha bravo)"    "THAW   2"
    eq "$vocab: explicit thaw, nothing frozen"    "$(target_effect charlie echo)"   "nothing to thaw"
    # Distinct refusals: one shared "nothing to do" would tell a user who asked to
    # thaw about freezing, and would let a single string satisfy three cases.
    ACTION="freeze"
    _eff_freeze="$(target_effect alpha bravo)"
    ACTION="thaw"
    _eff_thaw="$(target_effect charlie echo)"
    ACTION=""
    _eff_derived="$(target_effect nosuch)"
    if [ "$_eff_freeze" = "$_eff_thaw" ] || [ "$_eff_freeze" = "$_eff_derived" ] ||
        [ "$_eff_thaw" = "$_eff_derived" ]; then
        fail "$vocab: the three refusals are distinct" \
            "freeze: $_eff_freeze" "thaw: $_eff_thaw" "derived: $_eff_derived"
    else
        pass "$vocab: the three refusals are distinct"
    fi
    # The two verbs share a column in the menu, so THAW carries padding FREEZE does
    # not. A label that lost it would shift the count column on thaw rows only, and
    # nothing else in this file would notice.
    _w_freeze="$(target_effect charlie)"
    _w_thaw="$(target_effect alpha)"
    eq "$vocab: FREEZE n and THAW n are the same width" "${#_w_freeze}" "${#_w_thaw}"

    echo ""
    echo "=== row_verb: what this ONE container is about to have done to it ==="
    fixture "$vocab"
    eq "$vocab: derived, a running container" "$(row_verb charlie)" "FREEZE"
    eq "$vocab: derived, a frozen container"  "$(row_verb alpha)"   "THAW"
    ACTION="freeze"
    eq "$vocab: explicit freeze, running"     "$(row_verb charlie)" "FREEZE"
    eq "$vocab: explicit freeze, frozen"      "$(row_verb alpha)"   "-"
    ACTION="thaw"
    eq "$vocab: explicit thaw, frozen"        "$(row_verb alpha)"   "THAW"
    eq "$vocab: explicit thaw, running"       "$(row_verb charlie)" "-"
    # A name with no inventory entry is '?', whatever the verb — it must not read as
    # '-' ("nothing will happen to this one"), which is a claim about a container
    # that is not there.
    ACTION=""
    eq "$vocab: derived, an unknown name"         "$(row_verb nosuch)" "?"
    ACTION="freeze"
    eq "$vocab: explicit freeze, an unknown name" "$(row_verb nosuch)" "?"
    ACTION="thaw"
    eq "$vocab: explicit thaw, an unknown name"   "$(row_verb nosuch)" "?"

    echo ""
    echo "=== freeze_partition: act, skip and VANISHED are three answers ==="
    # A selection holding all three kinds at once, so no case can pass by collapsing
    # two of the buckets into one.
    fixture "$vocab"
    freeze_partition freeze charlie alpha nosuch
    eq "$vocab: freeze — the running one is a target" "${FREEZE_ACT_ON[*]}"   "charlie"
    eq "$vocab: freeze — the frozen one is skipped"   "${FREEZE_SKIPPED[*]}"  "alpha"
    eq "$vocab: freeze — the absent one has vanished" "${FREEZE_VANISHED[*]}" "nosuch"
    fixture "$vocab"
    freeze_partition thaw charlie alpha nosuch
    eq "$vocab: thaw — the frozen one is a target"    "${FREEZE_ACT_ON[*]}"   "alpha"
    eq "$vocab: thaw — the running one is skipped"    "${FREEZE_SKIPPED[*]}"  "charlie"
    eq "$vocab: thaw — the absent one has vanished"   "${FREEZE_VANISHED[*]}" "nosuch"
    # A name not in the inventory must be REPORTED, not dropped. This is the case
    # that fails if `vanished` is folded into `skipped`, which reads to a user as
    # "already in that state".
    fixture "$vocab"
    freeze_partition freeze nosuch
    eq "$vocab: an all-absent selection has no targets" "${FREEZE_ACT_ON[*]-}"  ""
    eq "$vocab: and is not reported as skipped"         "${FREEZE_SKIPPED[*]-}" ""
    eq "$vocab: it is reported as vanished"             "${FREEZE_VANISHED[*]}" "nosuch"
    # Order is the order the user gave, not inventory order: `echo` is index 4 and
    # `charlie` index 2, so a partition that walked the inventory would swap them.
    fixture "$vocab"
    freeze_partition freeze echo charlie
    eq "$vocab: targets keep the order they were given" "${FREEZE_ACT_ON[*]}" "echo charlie"
    # Partitioning twice must not accumulate. A stale bucket is how the interactive
    # loop would act on something the user chose a screen ago.
    fixture "$vocab"
    freeze_partition freeze charlie
    freeze_partition freeze echo
    eq "$vocab: a second partition replaces the first"  "${FREEZE_ACT_ON[*]}" "echo"

    echo ""
    echo "=== select_all / select_names ==="
    fixture "$vocab"
    select_all
    eq "$vocab: every container, in inventory order" \
        "${SELECTED[*]}" "alpha bravo charlie delta echo"
    # An empty inventory leaves SELECTED EMPTY rather than unset: the caller tests
    # ${#SELECTED[@]} and would die on an unset array under `set -u`.
    fixture "$vocab"
    INV_NAME=()
    INV_STATE=()
    select_all
    eq "$vocab: an empty inventory gives an empty selection, not an unset one" \
        "${#SELECTED[@]}" "0"
    fixture "$vocab"
    select_names echo charlie
    # `echo` is index 4 and `charlie` index 2, so a selector that walked the
    # inventory would silently swap them — and the drill-down numbers these rows.
    eq "$vocab: names keep the order they were given" "${SELECTED[*]}" "echo charlie"
done

echo ""
echo "=== select_names: an unknown name is fatal, and says what to run ==="
fixture podman
if sel_out="$(select_names nosuch 2>&1)"; then
    fail "an unknown name is fatal" "select_names nosuch succeeded"
else
    pass "an unknown name is fatal"
    contains "it names the container it could not find"  "$sel_out" "nosuch"
    contains "and points at the command that lists them" "$sel_out" "testfreeze list"
fi
# Fatal even when every OTHER name is real: selecting the subset that happened to
# exist would act on fewer containers than were asked for and say nothing about it.
#
# Driven through a command substitution, not called directly: the refusal is an
# `exit`, and an `if` condition does not contain that — it would take this suite
# with it, exactly as it takes the tool down from the CLI.
if mixed_out="$(select_names charlie nosuch 2>&1)"; then
    fail "one unknown name among known ones is still fatal" "select_names succeeded"
else
    pass "one unknown name among known ones is still fatal"
    contains "and names only the one it could not find" "$mixed_out" "nosuch"
    lacks "not the ones it could"                       "$mixed_out" "charlie"
fi
# FREEZE_LIST_NOTE is the engine's extra sentence on that error — lxcfreeze uses it
# to say a STOPPED container is not a candidate. Absent by default, so podfreeze's
# message stays exactly the two lines it has always been.
lacks "the note is absent when the engine set none" "$sel_out" "STOPPED"
FREEZE_LIST_NOTE="  A STOPPED container is not listed."
if sel_out="$(select_names nosuch 2>&1)"; then
    fail "and is printed when the engine set one" "select_names succeeded"
else
    contains "and is printed when the engine set one" \
        "$sel_out" "A STOPPED container is not listed."
fi
FREEZE_LIST_NOTE=""

echo ""
echo "=== print_table: the engine's own columns, through the hooks ==="
fixture podman
table="$(print_table charlie alpha)"
contains "the header names the shared columns"   "$table" "NAME"
contains "and the engine's columns"              "$table" "EXTRA"
contains "a row carries the container's name"    "$table" "charlie"
contains "and its state"                         "$table" "running"
contains "and the engine's per-row values"       "$table" "e-charlie"
eq "one header line plus one row per container"  "$(printf '%s\n' "$table" | wc -l)" "3"
# A name that is not in the inventory adds no row: print_table is handed names by a
# caller that already resolved them, and a blank row would look like a container.
eq "an unknown name adds no row" "$(print_table nosuch | wc -l)" "1"

echo ""
echo "=== freeze_menu_row: one shape for both tools' rows ==="
fixture podman
FREEZE_MENU_KEYS=()
FREEZE_MENU_LABELS=()
freeze_menu_row "all" "everything" "${INV_NAME[@]}"
freeze_menu_row "one" "just the running one" charlie
eq "the key is stored verbatim"              "${FREEZE_MENU_KEYS[*]}"  "all one"
eq "keys and labels stay parallel"           "${#FREEZE_MENU_KEYS[@]}" "${#FREEZE_MENU_LABELS[@]}"
contains "the label carries its description" "${FREEZE_MENU_LABELS[0]}" "everything"
contains "and what choosing it would do"     "${FREEZE_MENU_LABELS[0]}" "FREEZE 2"
# The effect is computed from the MEMBERS given, not from the inventory: the row
# above says FREEZE 2 for the whole machine and this one says FREEZE 1 for one
# container. A row that counted the machine would promise to act outside its group.
contains "a narrower row counts only its own members" "${FREEZE_MENU_LABELS[1]}" "FREEZE 1"
# The description column is padded to a fixed width so the effect column lines up
# down the menu — with a short description the label is longer than the text in it.
if [ "${#FREEZE_MENU_LABELS[1]}" -gt 38 ]; then
    pass "the description column is padded to a fixed width"
else
    fail "the description column is padded to a fixed width" \
        "label is ${#FREEZE_MENU_LABELS[1]} chars: ${FREEZE_MENU_LABELS[1]}"
fi
# A group with no members is a legal row — "nothing to do" is an answer the user
# should see, rather than a row that silently disappears.
freeze_menu_row "none" "an empty group"
contains "an empty group still gets a row" "${FREEZE_MENU_LABELS[2]}" "nothing to do"

echo ""
echo "=== pick_target / drill_into_group: no terminal is a NAMED failure ==="
# Both die without a TTY rather than reading whatever is on stdin: a menu answered
# by a pipe would act on a group nobody chose. This suite runs with stderr captured,
# so the guard is genuinely exercised here rather than simulated.
if pick_out="$(pick_target 2>&1)"; then
    fail "pick_target refuses without a terminal" "it returned a key: $pick_out"
else
    pass "pick_target refuses without a terminal"
    contains "and names the targets the CLI accepts" "$pick_out" "$FREEZE_TARGET_HINT"
    contains "and points at --help"                  "$pick_out" "testfreeze --help"
fi
if drill_out="$(drill_into_group alpha charlie 2>&1)"; then
    fail "drill_into_group refuses without a terminal" "it returned: $drill_out"
else
    pass "drill_into_group refuses without a terminal"
    contains "and names the targets the CLI accepts" "$drill_out" "$FREEZE_TARGET_HINT"
fi
# An EMPTY group returns non-zero before it ever looks for a terminal, because there
# is nothing to ask about. The caller reads that as "back to the menu".
#
# Driven through a command substitution rather than called directly, so that a
# version which DIED here produces a named failure instead of taking the suite down
# with it — a crash mid-run is a kill nobody can read.
if empty_out="$(drill_into_group 2>&1)"; then
    fail "an empty group returns rather than dying" "it succeeded with no group at all"
else
    pass "an empty group returns rather than dying"
    eq "and says nothing on the way out" "$empty_out" ""
fi

echo ""
echo "=== do_action: an empty selection is refused before anything happens ==="
fixture podman
SELECTED=()
if act_out="$(do_action freeze 2>&1)"; then
    fail "an empty selection is fatal" "do_action succeeded with nothing selected"
else
    pass "an empty selection is fatal"
    contains "and says how to see what there is" "$act_out" "testfreeze list"
fi
eq "and the engine was never called" "$(acts)" ""

echo ""
echo "=== do_action: what it acts on, and what it only reports ==="
fixture podman
SELECTED=(charlie alpha nosuch)
act_out="$(do_action freeze 2>&1)"
eq "the engine is called once, for the one running container" "$(acts)" "freeze charlie;"
contains "the container already frozen is reported as skipped" "$act_out" "not currently running"
contains "and named"                                           "$act_out" "alpha"
contains "the absent one is reported as gone"                  "$act_out" "no longer present"
contains "and named"                                           "$act_out" "nosuch"
# Skipped and vanished must not share a sentence: "already in that state" is a claim
# about a container nobody can find.
eq "the two skips are reported separately" \
    "$(printf '%s\n' "$act_out" | grep -c 'Skipped')" "2"
contains "a success is ticked off by name" "$act_out" "✓ charlie"
# After a freeze, the command that undoes it names exactly what was acted on — not
# the whole selection, which would thaw containers this run never touched.
contains "and the undo command is offered"  "$act_out" "testfreeze thaw charlie"
lacks "naming only what was acted on"       "$act_out" "thaw charlie alpha"

echo ""
echo "=== do_action: thaw is the mirror image ==="
fixture podman
SELECTED=(charlie alpha)
act_out="$(do_action thaw 2>&1)"
eq "the engine is called for the frozen one only" "$(acts)" "thaw alpha;"
contains "and the running one is skipped"         "$act_out" "not currently paused"
# The undo hint belongs to freeze alone: after a thaw, running the same command
# again would freeze them, which undoes nothing the user just asked for.
lacks "no undo hint after a thaw" "$act_out" "testfreeze thaw alpha"

echo ""
echo "=== do_action: nothing to do is a no-op, not a run ==="
fixture podman
SELECTED=(alpha bravo)
act_out="$(do_action freeze 2>&1)"
act_rc=$?
eq "the engine is not called at all" "$(acts)"  ""
eq "and it succeeds"                 "$act_rc"  "0"
contains "saying so plainly"         "$act_out" "already in the requested state"

echo ""
echo "=== do_action: a dry run changes nothing and prints to STDOUT ==="
fixture podman
DRY_RUN=1
SELECTED=(charlie echo)
dry_out="$(do_action freeze 2> "$QUIET")"
eq "the engine is never called"               "$(acts)" ""
contains "the preview says what would happen" "$dry_out" "DRY RUN"
contains "and names the containers"           "$dry_out" "charlie"
# The dry-run table IS the payload — `-n` exists to be read or piped — so it goes to
# stdout while the running commentary goes to stderr (CLAUDE/StderrHygiene.md).
contains "the table is on stdout"             "$dry_out" "NAME"
DRY_RUN=0

echo ""
echo "=== do_action: a failing engine is NAMED, counted, and fails the run ==="
fixture podman
SELECTED=(charlie echo)
ACT_STATUS=1
ACT_MESSAGE="cgroup freezer unavailable"
if act_out="$(do_action freeze 2>&1)"; then
    fail "a failing engine fails the run" "do_action returned 0"
else
    pass "a failing engine fails the run"
fi
contains "each failure is named"              "$act_out" "✗ charlie"
contains "with what the engine actually said" "$act_out" "cgroup freezer unavailable"
# Every target is attempted: a failure part-way through must not silently abandon
# the rest of a batch the user chose.
eq "and the batch is not abandoned after the first failure" \
    "$(acts)" "freeze charlie;freeze echo;"
contains "the count of failures is reported"  "$act_out" "2 of 2"
reset_engine

echo ""
echo "=== parse_member_choice: the drill-down's answer, resolved ==="
# The rest of drill_into_group needs a terminal, so before this function was split out
# the entire member-choice grammar — the `2,4,5` parse, the row/index shift, every
# rejection — shipped on one host run and nothing else. These are the cases that used
# to be reachable only by typing into the menu.
#
# `rows` here are the menu's rows IN ORDER. Row 1 is "all", so the first container is 2.
choice_rows=(alpha bravo charlie delta echo)
choose() { parse_member_choice "$1" "${choice_rows[@]}"; }
# Returns are newline-separated; flatten to one line so a case reads as one value.
# The function prints NOTHING on a refusal, so a captured empty string is the refusal
# and no redirect is needed to keep the output clean.
chose() { choose "$1" | tr '\n' ';'; }

eq "row 2 is the FIRST container, not the second" "$(chose '2')"     "alpha;"
eq "row 3 is the second"                          "$(chose '3')"     "bravo;"
eq "the last row is the last container"           "$(chose '6')"     "echo;"
eq "commas separate"                              "$(chose '2,4,5')" "alpha;charlie;delta;"
eq "so do spaces"                                 "$(chose '2 4 5')" "alpha;charlie;delta;"
eq "and a mix of both"                            "$(chose '2, 4')"  "alpha;charlie;"
eq "order is the order typed, not sorted"         "$(chose '5,2')"   "delta;alpha;"

# Every rejection. Each must leave the caller to re-prompt rather than resolving to
# some container — picking the wrong container is silent and irreversible in a way
# "that is not a valid choice" is not.
for bad in "1" "0" "7" "99" "-1" "abc" "2abc" "" "   " "2,abc" "2,0" "2,99"; do
    if choice_out="$(choose "$bad")"; then
        fail "'$bad' is refused" "it resolved to: ${choice_out//$'\n'/;}"
    else
        pass "'$bad' is refused"
    fi
done
# `1` deserves its own word: it is the ALL row, handled by the caller before this is
# reached. If it ever resolved here it would silently mean "the first container".
eq "and 1 in particular resolves to nothing" "$(chose '1')" ""

# ONE bad token rejects the WHOLE reply. A partial answer would act on some of what
# was typed and not the rest, with nothing said about which.
eq "one bad token rejects the whole reply" "$(chose '2,99,4')" ""

# Discrimination control: every rejection above would pass against a function that
# refused unconditionally, so assert that a valid reply and an invalid one differ.
if [ "$(chose '2')" = "$(chose 'abc')" ]; then
    fail "a valid choice and a refusal are distinct" "both gave: $(chose '2')"
else
    pass "a valid choice and a refusal are distinct"
fi

# The rows are addressed positionally, so a SHORTER row list moves the bound with it —
# this is what makes passing the displayed rows, rather than the caller's group,
# load-bearing. With a member skipped from the menu, row 3 is the container printed at
# row 3 and there is no row 4.
choice_rows=(alpha charlie)
eq "a skipped member does not shift the rows below it" "$(chose '3')" "charlie;"
if choice_out="$(choose 4)"; then
    fail "and the bound moves with the list" "row 4 resolved on a two-row menu: $choice_out"
else
    pass "and the bound moves with the list"
fi
choice_rows=(alpha bravo charlie delta echo)

echo ""
echo "=== interactive_loop: quit, re-prompt, and the status it carries out ==="
# pick_target and drill_into_group both need a terminal, so they are stubbed here —
# what is under test is the LOOP: which answers end the session, which return to the
# menu, and what exit status survives a failed action. Plan 00122's whole complaint
# was about this layer, so it is not left to a host to find out.
PICK_SCRIPT=()
PICK_INDEX_FILE="$work/pick.index"
# The index lives in a FILE, not a variable. `interactive_loop` reads the menu's
# answer through `$(pick_target)`, which is a subshell — a variable incremented in
# there is discarded, the same key is served for ever, and the loop never ends. That
# is a property of the code under test, not of this stub, so the stub accommodates it
# rather than hiding it.
pick_target() {
    local index key
    index="$(cat "$PICK_INDEX_FILE")"
    key="${PICK_SCRIPT[$index]-quit}"
    printf '%s' "$((index + 1))" > "$PICK_INDEX_FILE"
    printf '%s' "$key"
}
DRILL_ANSWER="all"
drill_into_group() {
    if [ "$DRILL_ANSWER" = "back" ]; then
        return 1
    fi
    printf '%s\n' "$@"
}

# Each run starts from a clean fixture, a clean engine log, and a clean count of how
# many times the menu was drawn. The engine's status is applied AFTER the fixture,
# which resets it — a case that set it beforehand would be testing a working engine
# and passing for the wrong reason.
LOOP_ACT_STATUS=0
run_loop() {
    printf '0' > "$PICK_INDEX_FILE"
    : > "$REFRESH_LOG"
    fixture podman
    ACT_STATUS="$LOOP_ACT_STATUS"
    PICK_SCRIPT=("$@")
    interactive_loop
}
menus_drawn() {
    wc -c < "$REFRESH_LOG"
}

if loop_out="$(run_loop quit 2>&1)"; then
    pass "quitting leaves with a success status"
else
    fail "quitting leaves with a success status" "$loop_out"
fi

# A group that went away is RECOVERABLE: the hook has already explained it, and the
# loop must come back to the menu rather than end the session. Without this, a
# network removed between the menu being drawn and a row being chosen ends a session
# the user was in the middle of.
run_loop gone quit > "$QUIET" 2>&1
eq "a group that went away re-prompts rather than ending the session" \
    "$(menus_drawn)" "2"
# ...but a hook that BROKE is not that, and must not be treated as it. This is the
# discrimination control for the case above: both return non-zero, and before
# FREEZE_SELECT_GONE existed both re-prompted, so a hook failing halfway through
# redrew the menu with no explanation and no failure. The two cases sharing one
# status is precisely what made that invisible.
if loop_out="$(run_loop broke quit 2>&1)"; then
    fail "a select hook that BROKE is fatal, not a re-prompt" \
        "interactive_loop returned 0 and carried on"
else
    pass "a select hook that BROKE is fatal, not a re-prompt"
fi
contains "and the failure names the status and the key" "$loop_out" "status 1"
contains "and does not claim the group went away" "$loop_out" "select hook failed"
# An empty group is the same shape: say so, and ask again.
loop_out="$(run_loop empty quit 2>&1)"
contains "an empty group says so" "$loop_out" "Nothing in that group."
eq "and asks again"               "$(menus_drawn)" "2"
# Backing out of the drill-down returns to the GROUP menu — one level up, not out.
DRILL_ANSWER="back"
run_loop all quit > "$QUIET" 2>&1
eq "backing out of the drill-down returns to the menu" "$(menus_drawn)" "2"
DRILL_ANSWER="all"

# The inventory is re-read on EVERY pass, so the counts describe the machine as it
# is now rather than as it was when the tool started.
run_loop all all quit > "$QUIET" 2>&1
eq "the inventory is re-read on every pass" "$(menus_drawn)" "3"
eq "and the engine acted on both passes" \
    "$(acts)" "freeze charlie;freeze echo;freeze charlie;freeze echo;"

# An empty machine ends the session without asking anything — there is no menu to
# draw. The refresh hook is what would have filled the inventory.
freeze_hook_refresh() {
    printf 'x' >> "$REFRESH_LOG"
    INV_NAME=()
    INV_STATE=()
}
loop_out="$(run_loop all 2>&1)"
contains "an empty machine is reported, not prompted about" \
    "$loop_out" "No running or frozen containers."
eq "and nothing was acted on" "$(acts)" ""
freeze_hook_refresh() {
    printf 'x' >> "$REFRESH_LOG"
}

# A failed action is reported and the session CARRIES ON — one container refusing to
# freeze is not a reason to throw the user out of the menu — but the status it leaves
# with is still non-zero, or a script wrapping the tool reads a failure as a clean run.
LOOP_ACT_STATUS=1
if run_loop all quit > "$QUIET" 2>&1; then
    fail "a failed action still fails the session" "interactive_loop returned 0"
else
    pass "a failed action still fails the session"
fi
eq "and it kept going to the next menu" "$(menus_drawn)" "2"
LOOP_ACT_STATUS=0
if run_loop all quit > "$QUIET" 2>&1; then
    pass "and a session with no failure returns success"
else
    fail "and a session with no failure returns success" "interactive_loop returned non-zero"
fi

echo ""
echo "=== freeze_assert_contract: a missing hook is caught before the menu ==="
ok "the suite's own engine satisfies the contract" freeze_assert_contract
# Driven in child shells, because the refusal is a `die` and would take this suite
# with it. Each hook is dropped BY NAME in turn, so this cannot pass against a check
# that refuses everything.
for hook in freeze_hook_preflight freeze_hook_refresh freeze_hook_menu_rows \
    freeze_hook_select freeze_hook_act freeze_hook_table_header \
    freeze_hook_table_row; do
    hook_out="$(bash -c "set -uo pipefail
        source '$work/config.bash'
        source '$LIB'
        for h in freeze_hook_preflight freeze_hook_refresh freeze_hook_menu_rows \\
            freeze_hook_select freeze_hook_act freeze_hook_table_header \\
            freeze_hook_table_row; do
            if [ \"\$h\" != '$hook' ]; then
                eval \"\$h() { :; }\"
            fi
        done
        freeze_assert_contract && echo CONTRACT-OK" 2>&1)"
    case "$hook_out" in
        *CONTRACT-OK*) fail "a missing $hook is caught" "the contract passed without it" ;;
        *"$hook"*) pass "a missing $hook is caught, by name" ;;
        *) fail "a missing $hook is caught" "unexpected output: $hook_out" ;;
    esac
done

echo ""
echo "=== assert_on_host: this container is not a host ==="
# The suite runs inside a container, which is exactly the condition the guard
# refuses on — so this drives the real predicate rather than a fabricated one.
if [ -f /run/.containerenv ] || [ -f /.dockerenv ] || [ -n "${container:-}" ]; then
    if host_out="$(assert_on_host 2>&1)"; then
        fail "a container is refused" "assert_on_host allowed it"
    else
        pass "a container is refused"
        contains "it names the tool" "$host_out" "testfreeze"
        contains "and gives the engine's own reason" "$host_out" "not reachable from in here"
    fi
else
    fail "a container is refused" \
        "this suite is not running in a container, so the guard cannot be driven"
fi

echo ""
echo "=== the constants the menu depends on ==="
# The drill-down's "act on all of it" row shares a column with container names.
# Neither engine permits a name starting with '*', so a sentinel that does not begin
# with an alphanumeric is one a real container can never collide with.
case "$ALL_ROW_KEY" in
    [a-zA-Z0-9]*)
        fail "the all-row sentinel cannot be a container name" \
            "'$ALL_ROW_KEY' starts with an alphanumeric" ;;
    *) pass "the all-row sentinel cannot be a container name" ;;
esac
# A bounded retry budget is what keeps a mistyped menu answer from looping for ever,
# and more than one try is what makes a typo recoverable
# (CLAUDE/InteractiveScripts.md).
if [ "$MAX_TRIES" -ge 2 ]; then
    pass "the menu retry budget allows a recoverable mistake"
else
    fail "the menu retry budget allows a recoverable mistake" "MAX_TRIES is $MAX_TRIES"
fi

echo ""
echo "──────────────────────────────────────────────────────────────"
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"

# A suite that discovers nothing exits 0 and reports clean, which is the failure this
# whole file exists to prevent in the code it tests.
if [ "$PASSED" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
echo "OK"

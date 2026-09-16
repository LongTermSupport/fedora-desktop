#!/usr/bin/env bash
# freeze-common.bash — the shared menu, decisions and act-loop behind `podfreeze`
#                      and `lxcfreeze`. SOURCE this; do not run it.
#
# WHY THE MENU IS IN HERE, NOT JUST A HANDFUL OF PURE HELPERS.
#
# `lxcfreeze` shipped first as a standalone tool (Plan 00122 Phase 2) and
# SIMPLIFIED the menu rather than reproducing it, so the two tools taught
# different habits for the same job: one had fzf, a two-level drill-down, member
# selection and a `b`-to-go-back key; the other had a flat numbered list and `q`.
# The owner's complaint was the UX, not the duplication. Extracting only the pure
# decisions would have been tidier and would have left the two exactly as
# divergent — so the MENU LAYER is what lives here, because that layer is where
# the UX lives. The two tools behave identically as a consequence rather than as
# an aspiration.
#
# THE ENGINE NEVER APPEARS IN THIS FILE. There is no `if podman`/`if lxc`
# anywhere below, and adding one would defeat the point: the next engine
# difference would go beside it, and the shared layer would drift back into two
# implementations sharing a file. Differences enter through NAMED HOOKS, listed
# in "The contract" below.
#
# WHAT A TOOL OWNS, AND WHAT THIS FILE OWNS:
#
#   the tool        the inventory query, the privilege/availability guards, the
#                   group axes (a CCY session, a network, a bridge), the engine's
#                   two state words, the table columns beyond NAME and STATE.
#   this file       the group menu, the drill-down, member selection, the bounded
#                   retry on a bad keypress, the derived verb, the dry run, the
#                   act/skip/vanished partition, and the per-target act loop.
#
# OUTPUT CONVENTIONS (CLAUDE/StderrHygiene.md): a function's stdout is its return
# value. `pick_target` and `drill_into_group` emit their ANSWER on stdout and
# every prompt, menu and diagnostic on stderr — they are read through `$( )`, so
# a stray `echo` would be parsed as a chosen container. The dry-run table is the
# exception: printing for a human IS its job.
#
# Deployed by tasks/deploy-freeze-lib.yml, which both freeze plays include.

# ---------------------------------------------------------------------------
# The contract.
#
# A tool sets these variables BEFORE sourcing this file, and defines the
# freeze_hook_* functions afterwards (freeze_assert_contract checks them):
#
#   FREEZE_TOOL             its own name, used in every message it prints.
#   FREEZE_STATE_RUNNING    the engine's word for "running"  (podman: running)
#   FREEZE_STATE_FROZEN     the engine's word for "frozen"   (podman: paused)
#   FREEZE_HOST_ONLY_NOTE   why running inside a container is refused.
#   FREEZE_TARGET_HINT      the targets its CLI accepts, for the no-TTY error.
#   FREEZE_LIST_NOTE        optional extra line on the unknown-name error.
#   FREEZE_FREEZE_NOTE      optional: what a freeze costs while it lasts, printed with
#                           the thaw instruction. Engine-specific by nature — LXC's is
#                           about DHCP leases and severed ssh sessions, which says
#                           nothing about a Podman container — so the library carries
#                           the slot and neither the text nor the assumption that there
#                           is one.
#
#   freeze_hook_preflight        engine present, privilege available.
#   freeze_hook_refresh          (re-)read the inventory and any derived maps.
#   freeze_hook_menu_rows        the group rows, via freeze_menu_row.
#   freeze_hook_select KEY       resolve a menu key into SELECTED. Return 0 on
#                                success, FREEZE_SELECT_GONE when the group no
#                                longer exists (the loop re-prompts). ANY OTHER
#                                non-zero is the hook itself failing and is fatal —
#                                see the note on FREEZE_SELECT_GONE below.
#   freeze_hook_act VERB NAME    act on one container. Its output is captured.
#   freeze_hook_table_header     the columns after NAME and STATE.
#   freeze_hook_table_row INDEX  those columns for one inventory entry.
# ---------------------------------------------------------------------------

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    echo "freeze-common.bash: this is a library — source it, do not run it." >&2
    echo "  It is the shared half of podfreeze and lxcfreeze; on its own it has" >&2
    echo "  no engine to talk to and nothing to report." >&2
    exit 1
fi

# The ONE recoverable status freeze_hook_select may return. It exists because a
# status has to be captured to be acted on, and capturing one — `if ! hook`, or
# `hook || rc=$?` — suspends errexit for the whole hook body either way. That is a
# bash property, not something a caller can opt out of: with a bare "non-zero means
# re-prompt" contract, a hook that breaks halfway through runs on to its own
# `return 0`, or returns 1 from the broken command, and both are indistinguishable
# from "that group went away". So the recoverable case gets its own number and
# everything else is fatal. 2 rather than 1, because 1 is what a failing command
# inside the hook returns by default.
FREEZE_SELECT_GONE=2

# Declared with empty defaults so this file reads as self-contained and so a
# missing one is a NAMED failure below rather than an unbound-variable abort
# three functions deep.
FREEZE_TOOL="${FREEZE_TOOL:-}"
FREEZE_STATE_RUNNING="${FREEZE_STATE_RUNNING:-}"
FREEZE_STATE_FROZEN="${FREEZE_STATE_FROZEN:-}"
FREEZE_HOST_ONLY_NOTE="${FREEZE_HOST_ONLY_NOTE:-}"
FREEZE_TARGET_HINT="${FREEZE_TARGET_HINT:-}"
FREEZE_LIST_NOTE="${FREEZE_LIST_NOTE:-}"
FREEZE_FREEZE_NOTE="${FREEZE_FREEZE_NOTE:-}"

for _freeze_setting in FREEZE_TOOL FREEZE_STATE_RUNNING FREEZE_STATE_FROZEN \
    FREEZE_HOST_ONLY_NOTE FREEZE_TARGET_HINT; do
    if [ -z "${!_freeze_setting}" ]; then
        echo "freeze-common.bash: $_freeze_setting is not set." >&2
        echo "  A tool declares its engine BEFORE sourcing this library. Without" >&2
        echo "  it every message below would name no tool and every state" >&2
        echo "  comparison would match the empty string." >&2
        exit 1
    fi
done
unset _freeze_setting

# The two state words must DIFFER. If they collapsed to one string, every
# container would be simultaneously a freeze target and a thaw target: the
# derived verb would always say freeze, and the act/skip partition would never
# skip anything. Cheap to assert, silent to get wrong.
if [ "$FREEZE_STATE_RUNNING" = "$FREEZE_STATE_FROZEN" ]; then
    echo "freeze-common.bash: FREEZE_STATE_RUNNING and FREEZE_STATE_FROZEN are" >&2
    echo "  both '$FREEZE_STATE_RUNNING'. Every decision here turns on telling" >&2
    echo "  those two states apart." >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Shared state.
#
# The inventory is two parallel arrays, one entry per container that can be
# frozen or thawed. A tool adds its own parallel arrays for the columns it
# shows (networks, a bridge) and indexes them with inventory_index_of.
# ---------------------------------------------------------------------------
declare -a INV_NAME=()
declare -a INV_STATE=()

# The resolved selection, set by a select_* function or by a hook.
declare -a SELECTED=()

# do_action's three buckets, set by freeze_partition.
declare -a FREEZE_ACT_ON=()
declare -a FREEZE_SKIPPED=()
declare -a FREEZE_VANISHED=()

# The group menu, rebuilt on every pass by freeze_hook_menu_rows.
declare -a FREEZE_MENU_KEYS=()
declare -a FREEZE_MENU_LABELS=()

# An explicit CLI verb, or empty for the derived one.
ACTION=""
DRY_RUN=0

MAX_TRIES=3   # bounded retries on a recoverable menu-input mistake

# First-column marker for the "act on the whole group" row in the drill-down.
# A leading '*' cannot begin a container name in either engine, so a real
# container can never be mistaken for this control row.
ALL_ROW_KEY='*all*'

# ---------------------------------------------------------------------------
# Basics
# ---------------------------------------------------------------------------

die() {
    echo "$FREEZE_TOOL: $*" >&2
    exit 1
}

have() {
    command -v "$1" > /dev/null
}

# Inside a container the host's engine is not reachable, so every probe would
# report an empty machine — a misleading empty result, which is worse than an
# error. It also removes any chance of freezing the very session issuing the
# command.
# The two marker paths are ARGUMENTS with the real files as defaults, so the guard can be
# driven on any machine — one drivable only where it fires is testable in a container and
# silently untested everywhere else the suite runs.
#
# Arguments rather than environment variables, and the difference is load-bearing on a
# guard whose entire job is to REFUSE: an argument cannot arrive from a parent shell, while
# an exported override could quietly loosen it. Both tools call this bare. Same shape as
# `_plan_in_container` in CLAUDE/Plan/_planlib.inc.bash, for the same reason.
assert_on_host() {
    local containerenv="${1:-/run/.containerenv}"
    local dockerenv="${2:-/.dockerenv}"
    if [ -f "$containerenv" ] || [ -f "$dockerenv" ] || [ -n "${container:-}" ]; then
        die "this is a container — run $FREEZE_TOOL on the HOST.
$FREEZE_HOST_ONLY_NOTE"
    fi
}

# Every hook must exist before the menu can run. A missing one is a wiring
# mistake in the TOOL, and without this it would surface as `command not found`
# part-way through a menu the user is already looking at.
freeze_assert_contract() {
    local hook
    for hook in freeze_hook_preflight freeze_hook_refresh freeze_hook_menu_rows \
        freeze_hook_select freeze_hook_act freeze_hook_table_header \
        freeze_hook_table_row; do
        if ! declare -F "$hook" > /dev/null; then
            die "internal error: $hook is not defined.
  This library reaches the engine only through its hooks, and nothing would
  answer for that one."
        fi
    done
}

# ---------------------------------------------------------------------------
# The decisions. Every one reads the inventory arrays and returns an answer;
# none of them shells out, which is what makes them testable on a machine with
# no engine and no containers.
# ---------------------------------------------------------------------------

# inventory_index_of <name> — echoes the inventory index, or returns 1.
#
# An exact string comparison, not a pattern match: a name that is a prefix of a
# real one must not resolve to it, or a selection acts on a container nobody
# chose.
inventory_index_of() {
    local wanted="$1" i
    for i in "${!INV_NAME[@]}"; do
        if [ "${INV_NAME[$i]}" = "$wanted" ]; then
            printf '%s' "$i"
            return 0
        fi
    done
    return 1
}

# count_in_state <state> <name>... — how many of THOSE NAMES are in that state.
#
# Scoped to the names given rather than to the whole inventory: this is what the
# menu labels are built from, and a row that counted the machine instead of the
# group would promise to act on containers outside it. "0 frozen" says the
# choice is a no-op before you make it. A name not in the inventory contributes
# nothing — freeze_partition is what reports those.
count_in_state() {
    local want="$1"
    shift
    local name i n=0
    for name in "$@"; do
        if i="$(inventory_index_of "$name")" && [ "${INV_STATE[$i]}" = "$want" ]; then
            n=$(( n + 1 ))
        fi
    done
    printf '%s' "$n"
}

# There is only ever one sensible verb for a given set, so it is derived rather
# than asked: anything running gets frozen, and a set with nothing running gets
# thawed. Running the tool twice on the same target therefore toggles it. An
# explicit `freeze`/`thaw` on the command line still wins — scripts need to say
# what they mean rather than depend on current state.
#
# The whole set is scanned, not just its first member: a group is usually mixed,
# and the common case is exactly "most of these are frozen and one is still
# running".
infer_action() {
    local name i
    for name in "${SELECTED[@]+${SELECTED[@]}}"; do
        if i="$(inventory_index_of "$name")" &&
            [ "${INV_STATE[$i]}" = "$FREEZE_STATE_RUNNING" ]; then
            printf 'freeze'
            return 0
        fi
    done
    printf 'thaw'
}

# target_effect <name>... — what choosing this group would do, right now.
#
# It applies the same rule infer_action does — or an explicit CLI verb when one
# was given — so the label can never disagree with the outcome. The count is of
# containers the verb will TOUCH, not of the group's size.
#
# The two explicit-verb refusals are worded differently from each other and from
# the derived one. One shared "nothing to do" would tell a user who asked to
# thaw about freezing, and would let a single string satisfy three cases.
target_effect() {
    local running frozen
    running="$(count_in_state "$FREEZE_STATE_RUNNING" "$@")"
    frozen="$(count_in_state "$FREEZE_STATE_FROZEN" "$@")"

    case "$ACTION" in
        freeze)
            if [ "$running" -gt 0 ]; then
                printf 'FREEZE %s' "$running"
            else
                printf 'nothing to freeze'
            fi
            return 0
            ;;
        thaw)
            if [ "$frozen" -gt 0 ]; then
                printf 'THAW   %s' "$frozen"
            else
                printf 'nothing to thaw'
            fi
            return 0
            ;;
    esac

    if [ "$running" -gt 0 ]; then
        printf 'FREEZE %s' "$running"
    elif [ "$frozen" -gt 0 ]; then
        printf 'THAW   %s' "$frozen"
    else
        printf 'nothing to do'
    fi
}

# What this ONE container is about to have done to it. The group row says
# "FREEZE 5"; a row inside the group says which of the five it is.
row_verb() {
    local name="$1" i state
    if ! i="$(inventory_index_of "$name")"; then
        printf '?'
        return 0
    fi
    state="${INV_STATE[$i]}"
    # Written out rather than as `test && printf || printf`: that shape silently
    # runs the third branch when the second one fails, which is the discarded-
    # failure-signal class this repo gates on (CLAUDE/AgentNotes.md).
    case "$ACTION" in
        freeze)
            if [ "$state" = "$FREEZE_STATE_RUNNING" ]; then printf 'FREEZE'; else printf '-'; fi
            ;;
        thaw)
            if [ "$state" = "$FREEZE_STATE_FROZEN" ]; then printf 'THAW'; else printf '-'; fi
            ;;
        *)
            if [ "$state" = "$FREEZE_STATE_RUNNING" ]; then printf 'FREEZE'; else printf 'THAW'; fi
            ;;
    esac
}

# freeze_partition <freeze|thaw> <name>... — split a selection three ways, into
# FREEZE_ACT_ON, FREEZE_SKIPPED and FREEZE_VANISHED.
#
# THREE buckets, not two. A name already in the requested state is SKIPPED; a
# name that is not in the inventory at all has VANISHED since the inventory was
# read, and is reported as that rather than folded in with the skips. Collapsing
# them would tell the user a container is "already in that state" when the truth
# is nobody knows where it went — and dropping it silently would act on fewer
# containers than were chosen and say nothing about the difference.
#
# Targets keep the order they were given, which is the order the user typed or
# the group listed, not inventory order.
freeze_partition() {
    local action="$1"
    shift
    local name i want_state

    if [ "$action" = "freeze" ]; then
        want_state="$FREEZE_STATE_RUNNING"
    else
        want_state="$FREEZE_STATE_FROZEN"
    fi

    # Reset, or a second pass through the interactive loop would act on a
    # target the user chose a screen ago.
    FREEZE_ACT_ON=()
    FREEZE_SKIPPED=()
    FREEZE_VANISHED=()

    for name in "$@"; do
        if ! i="$(inventory_index_of "$name")"; then
            FREEZE_VANISHED+=("$name")
        elif [ "${INV_STATE[$i]}" = "$want_state" ]; then
            FREEZE_ACT_ON+=("$name")
        else
            FREEZE_SKIPPED+=("$name")
        fi
    done
}

# ---------------------------------------------------------------------------
# Selection
# ---------------------------------------------------------------------------

select_all() {
    SELECTED=("${INV_NAME[@]+${INV_NAME[@]}}")
}

# One unknown name is fatal even when every other name is real: selecting the
# subset that happened to exist would act on fewer containers than were asked
# for and say nothing about the difference.
select_names() {
    local name
    local -a unknown=()
    SELECTED=()
    for name in "$@"; do
        if inventory_index_of "$name" > /dev/null; then
            SELECTED+=("$name")
        else
            unknown+=("$name")
        fi
    done
    if [ "${#unknown[@]}" -gt 0 ]; then
        echo "$FREEZE_TOOL: not a running or frozen container: ${unknown[*]}" >&2
        echo "  Run '$FREEZE_TOOL list' to see what is available." >&2
        if [ -n "$FREEZE_LIST_NOTE" ]; then
            echo "$FREEZE_LIST_NOTE" >&2
        fi
        exit 1
    fi
}

# ---------------------------------------------------------------------------
# Reporting
#
# `list` and `--dry-run` exist to print for a human, so their table IS the
# payload and goes to stdout. Everywhere else the caller redirects it to stderr,
# because there it is context around a prompt, not a return value
# (CLAUDE/StderrHygiene.md).
# ---------------------------------------------------------------------------

print_table() {
    local name i
    printf '  %-34s %-8s %s\n' "NAME" "STATE" "$(freeze_hook_table_header)"
    for name in "$@"; do
        if ! i="$(inventory_index_of "$name")"; then
            continue
        fi
        printf '  %-34s %-8s %s\n' \
            "$name" "${INV_STATE[$i]}" "$(freeze_hook_table_row "$i")"
    done
}

# ---------------------------------------------------------------------------
# Interactive picker
#
# Validate strictly, recover kindly (CLAUDE/InteractiveScripts.md): a bad
# keypress re-prompts inside a bounded loop rather than aborting, and running
# out of tries is the hard failure.
# ---------------------------------------------------------------------------

# freeze_menu_row <key> <description> <member>... — one row of the group menu.
#
# The label's shape is fixed here rather than in the hook, because the column
# the effect lands in is exactly the UX the two tools are sharing. A hook that
# formatted its own rows could drift apart again one tool at a time.
freeze_menu_row() {
    local key="$1" description="$2"
    shift 2
    FREEZE_MENU_KEYS+=("$key")
    FREEZE_MENU_LABELS+=("$(printf '%-38s %s' "$description" "$(target_effect "$@")")")
}

# Echoes the chosen group's key on stdout, or "quit".
#
# Groups come FIRST and per-container picking is reached by drilling into one,
# because acting on a whole group is the common case — an earlier version opened
# straight into a TAB-to-multi-select container list, which made the group
# targets reachable only by knowing the flag names.
#
# Cancelling is a normal way to leave a menu that loops, not an error, so it
# comes back as a "quit" key rather than as a non-zero exit.
pick_target() {
    local selection reply attempt total i

    if [ ! -t 2 ] || [ ! -r /dev/tty ]; then
        die "no terminal to ask on — name a target explicitly: $FREEZE_TARGET_HINT.
  See '$FREEZE_TOOL --help'."
    fi

    FREEZE_MENU_KEYS=()
    FREEZE_MENU_LABELS=()
    freeze_hook_menu_rows

    total="${#FREEZE_MENU_LABELS[@]}"
    if [ "$total" -eq 0 ]; then
        die "internal error: the menu has no rows to offer."
    fi

    if have fzf; then
        if ! selection="$(printf '%s\n' "${FREEZE_MENU_LABELS[@]}" |
            fzf --height=50% --reverse \
                --prompt="$FREEZE_TOOL: " \
                --header="ENTER acts on the chosen group — ESC to quit")"; then
            printf 'quit'
            return 0
        fi
        for i in "${!FREEZE_MENU_LABELS[@]}"; do
            if [ "${FREEZE_MENU_LABELS[$i]}" = "$selection" ]; then
                printf '%s' "${FREEZE_MENU_KEYS[$i]}"
                return 0
            fi
        done
        die "internal error: chosen entry not found."
    fi

    echo "What to act on:" >&2
    for i in "${!FREEZE_MENU_LABELS[@]}"; do
        printf '  %2d) %s\n' "$(( i + 1 ))" "${FREEZE_MENU_LABELS[$i]}" >&2
    done

    attempt=1
    while :; do
        printf 'Choice [1-%d, q to quit]: ' "$total" >&2
        if ! read -r reply < /dev/tty; then
            echo >&2
            printf 'quit'
            return 0
        fi
        case "$reply" in
            q | Q | quit)
                printf 'quit'
                return 0
                ;;
            "" | *[!0-9]*) ;;
            *)
                if [ "$reply" -ge 1 ] && [ "$reply" -le "$total" ]; then
                    printf '%s' "${FREEZE_MENU_KEYS[$(( reply - 1 ))]}"
                    return 0
                fi
                ;;
        esac
        echo "  Not one of the options — enter a number between 1 and $total, or q." >&2
        attempt=$(( attempt + 1 ))
        if [ "$attempt" -gt "$MAX_TRIES" ]; then
            die "giving up after $MAX_TRIES attempts — nothing changed."
        fi
    done
}

# The second screen: having chosen a group, see WHAT IS IN IT before acting.
#
# The first row is "all N", so the common case — act on the whole group — is
# still one keypress. What it adds is sight of the members: a group row says
# "network: podman — FREEZE 5" and names nothing, which is precisely where a
# live session hides inside an app network.
#
# Emits the chosen names on stdout, one per line. Returns non-zero for back or
# cancel, which the caller treats as "return to the group menu".
# parse_member_choice REPLY NAME... — resolve the numbered menu's answer.
#
# Split out of drill_into_group because everything else in that function needs a
# terminal, and a predicate that only runs behind a `read` from /dev/tty is a
# predicate no suite can execute. `identity_axis_discriminates` was extracted from
# `pick_target` for the same reason, and its comment gives the argument: a predicate
# inlined there is one the unit test can only re-implement, and a re-implementation
# asserts nothing about the code that ships.
#
# The NAMEs are the menu's rows IN MENU ORDER, not the caller's group — row 1 is
# "all" and the containers start at 2, so the caller must pass exactly what it
# displayed or the numbers address the wrong thing.
#
# Echoes one chosen name per line. Returns 1 for any reply that is not a valid,
# non-empty set of row numbers; the caller re-prompts.
parse_member_choice() {
    local reply="$1"
    shift
    local -a rows=("$@") tokens=() picked=()
    local token total="$#"

    read -r -a tokens <<< "${reply//,/ }"
    for token in "${tokens[@]+${tokens[@]}}"; do
        case "$token" in
            "" | *[!0-9]*) return 1 ;;
            *)
                # Row 1 is "all"; containers start at 2, so shift by two to index
                # the rows.
                if [ "$token" -ge 2 ] && [ "$token" -le "$(( total + 1 ))" ]; then
                    picked+=("${rows[$(( token - 2 ))]}")
                else
                    return 1
                fi
                ;;
        esac
    done

    if [ "${#picked[@]}" -eq 0 ]; then
        return 1
    fi
    printf '%s\n' "${picked[@]}"
}

drill_into_group() {
    local -a group=("$@")
    local i total line selection reply attempt name picked_out
    local -a menu=() picked=() rows=()

    total="${#group[@]}"
    if [ "$total" -eq 0 ]; then
        return 1
    fi

    if [ ! -t 2 ] || [ ! -r /dev/tty ]; then
        die "no terminal to ask on — name a target explicitly: $FREEZE_TARGET_HINT.
  See '$FREEZE_TOOL --help'."
    fi

    # Sentinel-prefixed so a container can never be mistaken for a control row:
    # the emitters below key on the first field, and a container named "all"
    # would otherwise collide with the "act on everything" row.
    menu+=("$(printf '%-34s %s' "$ALL_ROW_KEY" \
        "$(target_effect "${group[@]}")")")
    # `rows` is what was actually PRINTED, in order. A member that does not resolve
    # is skipped when building the menu, and numbering the answer against `group`
    # would then shift every row below it — typing 4 would act on the container
    # shown at row 5. Unreachable today, because every selector filters through the
    # inventory, but the skip is written as though it can happen and the failure it
    # would produce is acting on a container nobody chose.
    for name in "${group[@]}"; do
        i="$(inventory_index_of "$name")" || continue
        rows+=("$name")
        menu+=("$(printf '%-34s %-8s %s %s' \
            "$name" "${INV_STATE[$i]}" "$(freeze_hook_table_row "$i")" \
            "$(row_verb "$name")")")
    done
    total="${#rows[@]}"

    if have fzf; then
        if ! selection="$(printf '%s\n' "${menu[@]}" |
            fzf --multi --height=60% --reverse \
                --prompt="In this group: " \
                --header="ENTER on the first row acts on all — TAB picks specific ones, ESC goes back")"; then
            return 1
        fi
        if [ -z "$selection" ]; then
            return 1
        fi
        picked=()
        while read -r line; do
            if [ -z "$line" ]; then
                continue
            fi
            if [ "${line%% *}" = "$ALL_ROW_KEY" ]; then
                printf '%s\n' "${group[@]}"
                return 0
            fi
            picked+=("${line%% *}")
        done <<< "$selection"
        if [ "${#picked[@]}" -eq 0 ]; then
            return 1
        fi
        printf '%s\n' "${picked[@]}"
        return 0
    fi

    echo "In this group:" >&2
    for i in "${!menu[@]}"; do
        printf '  %2d) %s\n' "$(( i + 1 ))" "${menu[$i]}" >&2
    done

    attempt=1
    while :; do
        printf 'Choice [ENTER or 1 for all, numbers for specific, b to go back]: ' >&2
        if ! read -r reply < /dev/tty; then
            echo >&2
            return 1
        fi
        case "$reply" in
            b | B | back | q | Q | quit)
                return 1
                ;;
            "" | 1 | a | A | all)
                printf '%s\n' "${group[@]}"
                return 0
                ;;
        esac

        if picked_out="$(parse_member_choice "$reply" "${rows[@]}")"; then
            printf '%s\n' "$picked_out"
            return 0
        fi

        echo "  Not a valid choice — 1 (or ENTER) for all, 2-$(( total + 1 )) for" >&2
        echo "  specific containers, or 'b' to go back." >&2
        attempt=$(( attempt + 1 ))
        if [ "$attempt" -gt "$MAX_TRIES" ]; then
            die "giving up after $MAX_TRIES attempts — nothing changed."
        fi
    done
}

# ---------------------------------------------------------------------------
# Action
# ---------------------------------------------------------------------------

do_action() {
    local action="$1"
    local name past out want_state
    local failed=0

    if [ "$action" = "freeze" ]; then
        want_state="$FREEZE_STATE_RUNNING"
        past="FROZEN"
    else
        want_state="$FREEZE_STATE_FROZEN"
        past="THAWED"
    fi

    if [ "${#SELECTED[@]}" -eq 0 ]; then
        die "nothing matched — the selection is empty.
  Run '$FREEZE_TOOL list' to see what is running and what is frozen."
    fi

    freeze_partition "$action" "${SELECTED[@]}"

    if [ "${#FREEZE_VANISHED[@]}" -gt 0 ]; then
        echo "Skipped — no longer present (gone since the inventory was read): ${FREEZE_VANISHED[*]}" >&2
    fi

    if [ "${#FREEZE_SKIPPED[@]}" -gt 0 ]; then
        echo "Skipped — not currently $want_state: ${FREEZE_SKIPPED[*]}" >&2
    fi

    if [ "${#FREEZE_ACT_ON[@]}" -eq 0 ]; then
        echo "Nothing to do — every selected container is already in the requested state." >&2
        return 0
    fi

    if [ "$DRY_RUN" -eq 1 ]; then
        echo "DRY RUN — ${#FREEZE_ACT_ON[@]} container(s) would be $past:"
        print_table "${FREEZE_ACT_ON[@]}"
        return 0
    fi

    # Printed before acting, not to ask permission but to leave a record of
    # exactly what was touched. There is deliberately no confirmation prompt:
    # freezing is the cgroup freezer and is undone by running the tool again on
    # the same target, so a prompt would be guarding a reversible act — and in
    # the interactive path the menu row already named the verb and the count
    # before it was chosen.
    echo "${#FREEZE_ACT_ON[@]} container(s) being $past:" >&2
    print_table "${FREEZE_ACT_ON[@]}" >&2

    # Each container is acted on individually so a single failure is NAMED
    # rather than collapsing the batch into one opaque exit status, and so a
    # failure part-way through does not silently abandon the rest. Every failure
    # is printed and the command still exits non-zero.
    for name in "${FREEZE_ACT_ON[@]}"; do
        if out="$(freeze_hook_act "$action" "$name" 2>&1)"; then
            echo "  ✓ $name" >&2
        else
            echo "  ✗ $name — $out" >&2
            failed=$(( failed + 1 ))
        fi
    done

    if [ "$action" = "freeze" ]; then
        echo "" >&2
        echo "  Thaw them with: $FREEZE_TOOL thaw ${FREEZE_ACT_ON[*]}" >&2
        # Printed HERE, beside the thaw instruction, because that is the moment the cost
        # is still avoidable — a freeze already taken is a session already gone. Empty for
        # an engine with nothing to add, and an empty note prints nothing rather than a
        # blank line.
        if [ -n "$FREEZE_FREEZE_NOTE" ]; then
            echo "" >&2
            echo "$FREEZE_FREEZE_NOTE" >&2
        fi
    fi

    # Returns rather than dies, so the interactive loop can report a failure and
    # carry on. The one-shot path runs this as its last command, so the status
    # still becomes the script's.
    if [ "$failed" -gt 0 ]; then
        echo "$FREEZE_TOOL: $failed of ${#FREEZE_ACT_ON[@]} container(s) failed — see above." >&2
        return 1
    fi
    return 0
}

# The interactive session: menu, act, refresh, menu again — the inventory is
# re-read every pass so the counts describe the machine as it is now, not as it
# was when the tool started. Leaves only when you quit.
interactive_loop() {
    local key picked status=0 select_rc

    while :; do
        freeze_hook_refresh

        if [ "${#INV_NAME[@]}" -eq 0 ]; then
            echo "No running or frozen containers." >&2
            return 0
        fi

        key="$(pick_target)"
        if [ "$key" = "quit" ]; then
            return "$status"
        fi

        # A group can stop existing between the menu being drawn and a row being
        # chosen — a network removed, a bridge emptied. The hook has already
        # explained it; re-prompt rather than abort the session, which is what
        # the interactive rules ask for on recoverable input.
        #
        # Only FREEZE_SELECT_GONE re-prompts. Capturing the status at all suspends
        # errexit inside the hook, so any other non-zero is a hook that broke on its
        # way to an answer, and treating that as "the group went away" would redraw
        # the menu with no explanation and no failure.
        select_rc=0
        freeze_hook_select "$key" || select_rc=$?
        if [ "$select_rc" -eq "$FREEZE_SELECT_GONE" ]; then
            echo "" >&2
            continue
        fi
        if [ "$select_rc" -ne 0 ]; then
            die "the select hook failed (status $select_rc) resolving '$key'"
        fi

        if [ "${#SELECTED[@]}" -eq 0 ]; then
            echo "Nothing in that group." >&2
            echo "" >&2
            continue
        fi

        # Second screen: see the members, act on all of them or a subset.
        # Backing out returns to the group menu rather than quitting — it is one
        # level down, not a separate command.
        if ! picked="$(drill_into_group "${SELECTED[@]}")"; then
            echo "" >&2
            continue
        fi
        mapfile -t SELECTED <<< "$picked"

        if ! do_action "${ACTION:-$(infer_action)}"; then
            status=1
        fi
        echo "" >&2
    done
}

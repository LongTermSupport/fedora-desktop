#!/bin/bash
# CCY session-lifecycle library: --max-age, --run-for and --until.
#
# These options configure the ccy-lifecycle supervisor plugin
# (supervisor-plugins/ccy_lifecycle.py, shipped in the image). This library is the host half:
# it validates what the user typed, turns it into the environment the plugin reads, and gives
# every launch an id so the plugin can tell a fresh container from a hot reload.
#
#   --max-age <dur>   restart the session on a fresh container after it has run this long
#   --run-for <dur>   announce a deadline this long after launch
#   --until HH:MM     announce a deadline at the next HH:MM (local time)
#
# <dur> is days, hours and minutes in that order: 90m, 12h, 3d, 1d12h. Nothing is on unless
# asked for: no option, no CCY_MAX_AGE, no plugin. The deadline only announces; it never ends
# the session. The restart reuses the supervisor's restart request (lib/restart-request.bash).
#
# Host environment defaults, read here:
#   CCY_MAX_AGE               the --max-age used when the option is not given
#   CCY_RESTART_WARN_MINUTES  minutes of warning before a max-age restart (1 to 240, default 10)
#
# The environment handed to the container:
#   CCY_LIFECYCLE_LAUNCH_ID          a new value per container launch
#   CCY_LIFECYCLE_MAX_AGE_SECONDS    empty when the feature is off
#   CCY_LIFECYCLE_DEADLINE_EPOCH     empty when there is no deadline
#   CCY_LIFECYCLE_WARN_MINUTES       empty for the plugin's default
# A relaunch after a restart keeps the deadline: it is absolute, so the launcher hands the
# resolved epoch to the next launcher in CCY_RELAUNCH_DEADLINE_EPOCH, and --run-for/--until
# are dropped from the relaunch arguments (lib/session-registry.bash classifies them as such).
# --max-age is kept, so its clock starts again with the new container.

# The same limits the plugin enforces, so a bad value fails here with an example rather than
# in the container with a disabled-plugin notice.
CCY_LIFECYCLE_MAX_AGE_MIN_SECONDS=1800
CCY_LIFECYCLE_MAX_AGE_MAX_SECONDS=2592000
CCY_LIFECYCLE_RUN_FOR_MIN_SECONDS=60
CCY_LIFECYCLE_RUN_FOR_MAX_SECONDS=2592000
CCY_LIFECYCLE_WARN_MIN_MINUTES=1
CCY_LIFECYCLE_WARN_MAX_MINUTES=240
# The supervisor plugin API major ccy_lifecycle.py declares (its PLUGIN_API); a test holds the two equal.
CCY_LIFECYCLE_PLUGIN_API_MAJOR=1

# Where the image keeps the plugin (outside the project mount, root owned).
export CCY_LIFECYCLE_PLUGIN_PATH="/opt/claude-yolo/supervisor-plugins/ccy_lifecycle.py"

# ccy_lifecycle_parse_duration <text> <option-label> — print the duration in seconds.
#   status 0  seconds on stdout
#   status 1  not a duration; a message with examples on stderr
ccy_lifecycle_parse_duration() {
    local text="${1-}" label="${2:?ccy_lifecycle_parse_duration requires an option label}"
    local pattern='^(([0-9]{1,4})d)?(([0-9]{1,4})h)?(([0-9]{1,5})m)?$'
    if [[ -z "$text" ]] || ! [[ "$text" =~ $pattern ]]; then
        printf 'ERROR: %s: %q is not a duration.\n' "$label" "$text" >&2
        printf '  Use days, hours and minutes in that order: 90m, 12h, 3d or 1d12h.\n' >&2
        return 1
    fi
    local days="${BASH_REMATCH[2]}" hours="${BASH_REMATCH[4]}" minutes="${BASH_REMATCH[6]}"
    printf '%s\n' "$((10#${days:-0} * 86400 + 10#${hours:-0} * 3600 + 10#${minutes:-0} * 60))"
}

# ccy_lifecycle_describe <seconds> — 1d12h style text for a message.
ccy_lifecycle_describe() {
    local seconds="${1:?ccy_lifecycle_describe requires seconds}"
    local days=$((seconds / 86400)) hours=$((seconds % 86400 / 3600)) minutes=$((seconds % 3600 / 60))
    local text=""
    if ((days > 0)); then
        text=$(printf '%s%sd' "$text" "$days")
    fi
    if ((hours > 0)); then
        text=$(printf '%s%sh' "$text" "$hours")
    fi
    if ((minutes > 0)) || [[ -z "$text" ]]; then
        text=$(printf '%s%sm' "$text" "$minutes")
    fi
    printf '%s' "$text"
}

# ccy_lifecycle_check_range <seconds> <min> <max> <option-label> — status 1 with a message
# when the duration is outside the range the plugin accepts.
ccy_lifecycle_check_range() {
    local seconds="${1:?}" min="${2:?}" max="${3:?}" label="${4:?}"
    if ((seconds < min || seconds > max)); then
        printf 'ERROR: %s: the duration must be between %s and %s.\n' "$label" \
            "$(ccy_lifecycle_describe "$min")" "$(ccy_lifecycle_describe "$max")" >&2
        return 1
    fi
}

# ccy_lifecycle_parse_until <HH:MM> [now] — print the epoch of the next HH:MM in local time:
# today's if it is still ahead, otherwise tomorrow's. `now` is epoch seconds, injectable.
ccy_lifecycle_parse_until() {
    local text="${1-}" now="${2:-}"
    local pattern='^([01][0-9]|2[0-3]):([0-5][0-9])$'
    if ! [[ "$text" =~ $pattern ]]; then
        printf 'ERROR: --until: %q is not a time of day.\n' "$text" >&2
        printf '  Use 24-hour HH:MM, for example 17:30 or 09:05.\n' >&2
        return 1
    fi
    [[ -n "$now" ]] || now=$(date +%s)
    local target
    if ! target=$(date -d "$text" +%s); then
        printf 'ERROR: --until: could not work out %s.\n' "$text" >&2
        return 1
    fi
    if ((target <= now)); then
        if ! target=$(date -d "tomorrow $text" +%s); then
            printf 'ERROR: --until: could not work out tomorrow at %s.\n' "$text" >&2
            return 1
        fi
    fi
    printf '%s\n' "$target"
}

# ccy_lifecycle_requested — whether any feature is on.
ccy_lifecycle_requested() {
    [[ -n "${CCY_LIFECYCLE_MAX_AGE_SECONDS:-}" || -n "${CCY_LIFECYCLE_DEADLINE_EPOCH:-}" ||
        -n "${CCY_LIFECYCLE_RUN_FOR_SECONDS:-}" || -n "${CCY_LIFECYCLE_UNTIL_TEXT:-}" ]]
}

# ccy_lifecycle_validate_options <max-age> <run-for> <until> <no-supervise true|false>
# Validate the options and set, in the caller's shell:
#   CCY_LIFECYCLE_MAX_AGE_SECONDS   exported; empty when off
#   CCY_LIFECYCLE_WARN_MINUTES      exported; empty for the plugin's default
#   CCY_LIFECYCLE_DEADLINE_EPOCH    exported; set here only for a relaunch's carried deadline
#   CCY_LIFECYCLE_RUN_FOR_SECONDS / CCY_LIFECYCLE_UNTIL_TEXT   resolved by ccy_lifecycle_finalize
# <max-age> falls back to the host's CCY_MAX_AGE. status 1 with a message on any problem.
ccy_lifecycle_validate_options() {
    local max_age_text="${1-}" run_for_text="${2-}" until_text="${3-}" no_supervise="${4:-false}"
    local max_age_label="--max-age"

    CCY_LIFECYCLE_MAX_AGE_SECONDS=""
    CCY_LIFECYCLE_WARN_MINUTES=""
    CCY_LIFECYCLE_DEADLINE_EPOCH=""
    CCY_LIFECYCLE_RUN_FOR_SECONDS=""
    CCY_LIFECYCLE_UNTIL_TEXT=""
    export CCY_LIFECYCLE_MAX_AGE_SECONDS CCY_LIFECYCLE_WARN_MINUTES CCY_LIFECYCLE_DEADLINE_EPOCH

    if [[ -n "$run_for_text" && -n "$until_text" ]]; then
        printf 'ERROR: --run-for and --until both set a deadline; use one.\n' >&2
        return 1
    fi
    if [[ -z "$max_age_text" && -n "${CCY_MAX_AGE:-}" ]]; then
        max_age_text="$CCY_MAX_AGE"
        max_age_label="CCY_MAX_AGE"
    fi

    local seconds
    if [[ -n "$max_age_text" ]]; then
        seconds=$(ccy_lifecycle_parse_duration "$max_age_text" "$max_age_label") || return 1
        ccy_lifecycle_check_range "$seconds" "$CCY_LIFECYCLE_MAX_AGE_MIN_SECONDS" \
            "$CCY_LIFECYCLE_MAX_AGE_MAX_SECONDS" "$max_age_label" || return 1
        CCY_LIFECYCLE_MAX_AGE_SECONDS="$seconds"
    fi
    if [[ -n "$run_for_text" ]]; then
        seconds=$(ccy_lifecycle_parse_duration "$run_for_text" "--run-for") || return 1
        ccy_lifecycle_check_range "$seconds" "$CCY_LIFECYCLE_RUN_FOR_MIN_SECONDS" \
            "$CCY_LIFECYCLE_RUN_FOR_MAX_SECONDS" "--run-for" || return 1
        CCY_LIFECYCLE_RUN_FOR_SECONDS="$seconds"
    fi
    if [[ -n "$until_text" ]]; then
        # Parsed now only to refuse a bad value before any prompt; the epoch is worked out at launch.
        ccy_lifecycle_parse_until "$until_text" >/dev/null || return 1
        CCY_LIFECYCLE_UNTIL_TEXT="$until_text"
    fi

    # A relaunch after a restart carries the first launch's absolute deadline.
    if [[ -n "${CCY_RELAUNCH_DEADLINE_EPOCH:-}" ]]; then
        if [[ -z "$run_for_text" && -z "$until_text" ]]; then
            if ! [[ "$CCY_RELAUNCH_DEADLINE_EPOCH" =~ ^[0-9]{1,12}$ ]]; then
                printf 'ERROR: CCY_RELAUNCH_DEADLINE_EPOCH is not an epoch time: %q\n' \
                    "$CCY_RELAUNCH_DEADLINE_EPOCH" >&2
                return 1
            fi
            CCY_LIFECYCLE_DEADLINE_EPOCH="$((10#$CCY_RELAUNCH_DEADLINE_EPOCH))"
        fi
        unset CCY_RELAUNCH_DEADLINE_EPOCH
    fi

    local warn="${CCY_RESTART_WARN_MINUTES:-}"
    if [[ -n "$warn" && -n "$CCY_LIFECYCLE_MAX_AGE_SECONDS" ]]; then
        if ! [[ "$warn" =~ ^[0-9]{1,3}$ ]] ||
            ((10#$warn < CCY_LIFECYCLE_WARN_MIN_MINUTES || 10#$warn > CCY_LIFECYCLE_WARN_MAX_MINUTES)); then
            printf 'ERROR: CCY_RESTART_WARN_MINUTES must be %s to %s minutes, got %q.\n' \
                "$CCY_LIFECYCLE_WARN_MIN_MINUTES" "$CCY_LIFECYCLE_WARN_MAX_MINUTES" "$warn" >&2
            return 1
        fi
        if ((10#$warn * 60 >= CCY_LIFECYCLE_MAX_AGE_SECONDS)); then
            printf 'ERROR: CCY_RESTART_WARN_MINUTES (%s) must be shorter than the maximum age (%s).\n' \
                "$warn" "$(ccy_lifecycle_describe "$CCY_LIFECYCLE_MAX_AGE_SECONDS")" >&2
            return 1
        fi
        CCY_LIFECYCLE_WARN_MINUTES="$((10#$warn))"
    fi

    if [[ "$no_supervise" == true ]] && ccy_lifecycle_requested; then
        printf 'ERROR: --max-age, --run-for and --until are carried out by the supervisor, and --no-supervise turns it off.\n' >&2
        return 1
    fi
    if ccy_lifecycle_requested && [[ -z "${CCY_CLAUDE_WRAPPER:-}" ]]; then
        ccy_lifecycle_check_supervisor "$PWD/.claude/ccy/claude-supervise.py" || return 1
    fi
}

# ccy_lifecycle_check_supervisor <path> — status 1 with a message unless the project's supervisor
# takes --plugin at the plugin API major ccy_lifecycle.py declares. Run on the host before any
# prompt, so the file is READ, never executed: the host runs no project code. A wrapper the host
# names in CCY_CLAUDE_WRAPPER is not this file, so the entrypoint judges that one.
ccy_lifecycle_check_supervisor() {
    local path="${1:?}" major
    if [[ ! -f "$path" ]]; then
        printf 'ERROR: --max-age, --run-for and --until are carried out by the supervisor, and this project has no hooks-daemon supervisor (%s).\n' "$path" >&2
        printf '  Install the hooks daemon in this project, or drop the option.\n' >&2
        return 1
    fi
    if ! major=$(awk '/^_PLUGIN_API_MAJOR = [0-9]+$/ {print $3; exit}' "$path"); then
        printf 'ERROR: could not read the project supervisor at %s.\n' "$path" >&2
        return 1
    fi
    if [[ -z "$major" ]]; then
        printf 'ERROR: this project'"'"'s supervisor predates the plugin API that --max-age, --run-for and --until need.\n' >&2
        printf '  upgrade the hooks daemon in this project to a release with the supervisor plugin API, or drop the option.\n' >&2
        return 1
    fi
    if [[ "$major" != "$CCY_LIFECYCLE_PLUGIN_API_MAJOR" ]]; then
        printf 'ERROR: this project'"'"'s supervisor speaks plugin API %s, and the ccy lifecycle plugin speaks plugin API %s.\n' \
            "$major" "$CCY_LIFECYCLE_PLUGIN_API_MAJOR" >&2
        printf '  Update ccy, or upgrade the hooks daemon in this project, so the two agree; or drop the option.\n' >&2
        return 1
    fi
}

# ccy_lifecycle_finalize [now] — right before the container starts: resolve a relative
# deadline against the launch time, export CCY_LIFECYCLE_DEADLINE_EPOCH and a new launch id.
# Prints one line on stderr saying what is in force, when anything is.
ccy_lifecycle_finalize() {
    local now="${1:-}"
    [[ -n "$now" ]] || now=$(date +%s)
    if [[ -n "${CCY_LIFECYCLE_RUN_FOR_SECONDS:-}" ]]; then
        CCY_LIFECYCLE_DEADLINE_EPOCH="$((now + CCY_LIFECYCLE_RUN_FOR_SECONDS))"
    elif [[ -n "${CCY_LIFECYCLE_UNTIL_TEXT:-}" ]]; then
        CCY_LIFECYCLE_DEADLINE_EPOCH=$(ccy_lifecycle_parse_until "$CCY_LIFECYCLE_UNTIL_TEXT" "$now") || return 1
    fi
    CCY_LIFECYCLE_LAUNCH_ID="launch-${now}-$$-${RANDOM}"
    export CCY_LIFECYCLE_DEADLINE_EPOCH CCY_LIFECYCLE_LAUNCH_ID

    local said=""
    if [[ -n "${CCY_LIFECYCLE_MAX_AGE_SECONDS:-}" ]]; then
        said="restart after $(ccy_lifecycle_describe "$CCY_LIFECYCLE_MAX_AGE_SECONDS")"
    fi
    if [[ -n "${CCY_LIFECYCLE_DEADLINE_EPOCH:-}" ]]; then
        [[ -z "$said" ]] || said="${said}; "
        said="${said}deadline at $(date -d "@$CCY_LIFECYCLE_DEADLINE_EPOCH" '+%Y-%m-%d %H:%M')"
    fi
    if [[ -n "$said" ]]; then
        printf 'Session limits: %s\n' "$said" >&2
    fi
}

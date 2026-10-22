#!/bin/bash
# The agent a ccy session runs: Claude Code, or opencode with --opencode or -oc (Plan 00215
# decisions O1, O2, O3). The flag changes the command the container runs and nothing the session
# is built from: tmux, mounts, tokens, firewall and the session record are as for any launch, and
# the record keeps the flag, so a restore runs opencode again.
#
# opencode cannot use the Claude login, so it needs a model provider key. Each is a file in
# $CCY_ROOT/provider-keys/ (~/.claude-tokens/ccy/provider-keys/), named for the variable opencode
# reads it from (ANTHROPIC_API_KEY, OPENROUTER_API_KEY, ...: which provider is the owner's
# choice), one line, mode 0600. Like the GitHub token, a key is never exported: ccy_agent_container
# hands it by name to the one container run, and the run's own -e NAME passes it in.
#
# opencode is not a bus member yet (Phase 3), and the PTY supervisor is Claude Code's, so an
# opencode launch carries no bus kit and no supervisor, and the options that need them are
# refused. The image disables opencode's ctrl+z suspend instead (its /etc/opencode/tui.json).
#
# Requires print_error (common-pure.bash), container_cmd (common.bash) and the launcher's CCY_ROOT.

CCY_AGENT=claude
CCY_AGENT_KEYS=()
CCY_AGENT_RUN_ARGS=()

# ccy_agent_arg <arg> — the parse's last branch: either spelling chooses opencode, and any other
# argument is the agent's own.
ccy_agent_arg() {
    case "$1" in
    --opencode | -oc) CCY_AGENT=opencode ;;
    *) CLAUDE_ARGS+=("$1") ;;
    esac
}

# ccy_agent_options — right after the parse. For opencode: 64 for an option it cannot honour, 1
# for no usable provider key (a file in the key directory that is not one fails too: it was meant
# to be one); then the session runs with no bus kit and no supervisor, and claude's --help no
# longer judges the arguments, which are opencode's.
ccy_agent_options() {
    [ "$CCY_AGENT" = opencode ] || return 0
    local dir="$CCY_ROOT/provider-keys" file name mode clash=""
    [ "$HEADLESS_MODE" != true ] || clash="--headless (opencode's own is opencode run)"
    [ "$SUPERVISE_MODE" != true ] || clash="--supervise (the supervisor is Claude Code's)"
    [ "$TEAMS_GIVEN" != true ] || clash="--teams (an opencode bus member is Plan 00215 Phase 3)"
    [ -z "$OPT_MAX_AGE$OPT_RUN_FOR$OPT_UNTIL${CCY_MAX_AGE:-}" ] || clash="a session limit (--max-age, --run-for, --until or CCY_MAX_AGE: the supervisor carries them out)"
    if [ -n "$clash" ]; then
        print_error "--opencode cannot be used with $clash. Drop one."
        return 64
    fi
    for file in "$dir"/*; do
        [ -e "$file" ] || [ -L "$file" ] || continue
        name=${file##*/} mode=$(stat -c %a -- "$file") || return 1
        if ! [[ "$name" =~ ^[A-Z][A-Z0-9_]*_API_KEY$ ]] || [ -L "$file" ] || [ ! -f "$file" ] || [ ! -s "$file" ] || [ "$mode" != 600 ] || [ "$(wc -l <"$file")" -gt 1 ]; then
            print_error "$file is not a provider key: one is a regular file, mode 0600 (this is $mode), of one line, named for the variable opencode reads it from (<PROVIDER>_API_KEY)."
            return 1
        fi
        CCY_AGENT_KEYS+=("$name")
        CCY_AGENT_RUN_ARGS+=(-e "$name")
    done
    if [ "${#CCY_AGENT_KEYS[@]}" -eq 0 ]; then
        print_error "--opencode needs a model provider key, and $dir holds none."
        printf '  Add one: a file named for the variable opencode reads it from, holding the key on one\n  line, mode 0600, e.g. %s/OPENROUTER_API_KEY (docs/ccy.md, "opencode").\n' "$dir" >&2
        return 1
    fi
    for name in NO_BUS_MODE=true NO_SUPERVISE_MODE=true CLAUDE_ARGS_BEFORE_SEPARATOR=0; do
        printf -v "${name%%=*}" %s "${name#*=}"
    done
}

# ccy_agent_command — CCY_AGENT_CMD, the command the container runs, and the banner that shows it.
ccy_agent_command() {
    local title="Claude Code" shown="claude --dangerously-skip-permissions ${CLAUDE_CMD_ARGS[*]}"
    CCY_AGENT_CMD=(claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}")
    if [ "$CCY_AGENT" = opencode ]; then
        # --auto approves what opencode would ask: ccy's isolation is the container, as with
        # claude's --dangerously-skip-permissions.
        CCY_AGENT_CMD=(opencode --auto ${PROMPT_TEXT:+--prompt "$PROMPT_TEXT"} "${CLAUDE_ARGS[@]}")
        title=opencode shown="${CCY_AGENT_CMD[*]}"
    fi
    local rule="════════════════════════════════════════════════════════════════════════════════"
    printf '%s\nExecuting %s with arguments:\n  %s\n%s\n\n' "$rule" "$title" "$shown" "$rule"
}

# ccy_agent_container <engine args...> — the engine with each provider key in its environment
# only: local -x ends with this function, so nothing the launcher starts afterwards has one.
ccy_agent_container() {
    local name
    for name in "${CCY_AGENT_KEYS[@]}"; do
        local -x "$name=$(<"$CCY_ROOT/provider-keys/$name")"
    done
    container_cmd "$@"
}

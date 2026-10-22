#!/bin/bash
# CCY_LAUNCHER_PATH: the program that re-runs and records this launch (the tmux pane and its
# session record, a restart, the custom-Dockerfile modes). It is the Go front when the front
# started this launch, execing claude-yolo.bash with CCY_LAUNCHER_FRONT naming itself;
# otherwise this launcher's own file, so a direct run of claude-yolo.bash stays clear of a
# front that will not start. CCY_LAUNCHER_FRONT is read here and unset, so no program this
# launcher starts inherits it; a value that is not the sibling front ($SCRIPT_DIR/claude-yolo,
# the same file once resolved, an executable regular file) is warned of and not used.
#
# Sourced by lib/token-handoff.bash, the launcher's first library. The variable is gone before
# the launcher starts tmux, the engine or claude; a few system tools (dirname, id, find, rm)
# run before it and do not read it. Not a function: it runs once, as it is sourced.

CCY_LAUNCHER_PATH="$SCRIPT_DIR/${0##*/}"
if [ -n "${CCY_LAUNCHER_FRONT+set}" ]; then
    if [[ "$CCY_LAUNCHER_FRONT" == /* ]] && [ -f "$CCY_LAUNCHER_FRONT" ] && [ -x "$CCY_LAUNCHER_FRONT" ] &&
        [ "$(realpath -e -- "$CCY_LAUNCHER_FRONT")" = "$(realpath -e -- "$SCRIPT_DIR/claude-yolo")" ]; then
        CCY_LAUNCHER_PATH="$CCY_LAUNCHER_FRONT"
    else
        printf 'WARNING: CCY_LAUNCHER_FRONT=%s is not the front beside this launcher (%s/claude-yolo); this launch re-runs and records %s instead.\n' \
            "$CCY_LAUNCHER_FRONT" "$SCRIPT_DIR" "$CCY_LAUNCHER_PATH" >&2
    fi
    unset CCY_LAUNCHER_FRONT
fi

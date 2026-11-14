#!/bin/bash
# Whether a ccy session may start on a token: the Go command ccy-token-usable answers it
# (internal/ccylaunch F15, installed by play-claude-yolo.yml into ~/.local/bin), from the
# token's 5-hour and 7-day usage. Used by the launch (claude-yolo, for the token it is about to
# start on) and by select_token (token-management.bash, for the token picked and for each row's
# used-up mark).
#
# Requires print_error and ccy_launch_unattended (common-pure.bash), and ccy_unattended_stop
# (unattended-stop.bash).

# ccy_token_usable [--mark] -- <token-file> — 0 when the session may start on the token (usage
# left, or it could not be read: said on stderr); 3 when it has no usage left (said on stderr,
# naming the window and when it clears) and a person can choose another. When it has none left
# and nobody attends the launch (headless, a restore, a restart) the launch ends with
# ccy_unattended_stop token-used-up, which keeps the session's record; 1 when the check itself
# could not run. With --mark, the chooser's mark on stdout and 0.
ccy_token_usable() {
    local rc=0
    ccy-token-usable "$@" || rc=$?
    case "$rc" in
    0) return 0 ;;
    3)
        if ccy_launch_unattended; then
            print_error "Nobody is here to choose another token (a headless launch, a restore, a restart, or no terminal), so ccy stops."
            ccy_unattended_stop token-used-up "the token has no usage left (see above): restore it once the window clears, or declare it with another token"
        fi
        return 3
        ;;
    127) print_error "ccy-token-usable is not installed, so ccy cannot check a token's usage: run play-claude-yolo.yml" ;;
    *) print_error "ccy-token-usable failed (exit $rc), so the token's usage was not checked" ;;
    esac
    exit 1
}

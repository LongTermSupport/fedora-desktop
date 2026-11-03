# shellcheck shell=bash
# nohist.bash — run a command with no trace in shell history (Plan 00246). play-basic-configs.yml
# deploys it for the desktop user only.
#
#   nohist 'export TOKEN=abc; cd /srv'    one string, read by this shell
#   nohist ssh -i key host                a command and its arguments, run as typed
#
# The typed line never reaches history because HISTIGNORE (the history block in
# zz_lts-fedora-desktop.bash) holds "nohist" and "nohist *". A line kept out of history does not
# move the history number, so the Ctrl+R recorder in history-search.bash skips it too. It runs in
# the current shell, so cd, export and variable changes stay. The exit status is the command's.

nohist() {
    if [[ $# -eq 0 ]]; then
        echo "usage: nohist 'command line'   or   nohist command [argument...]" >&2
        return 2
    fi
    if [[ $# -gt 1 ]]; then
        # Words, not text: every argument stays one argument, exactly as typed.
        "$@"
        return
    fi
    # One string is a command line, which only this shell can parse into the current shell's
    # state. It is the user's own typing at their own prompt, text they could have typed
    # without the prefix, so reading it as a script is no step up in what it can do; nothing
    # here ever builds it from another source. Read from descriptor 3, not a here
    # document, so the command keeps the terminal as its standard input.
    # shellcheck source=/dev/null
    . /dev/fd/3 3< <(printf '%s\n' "$1")
}

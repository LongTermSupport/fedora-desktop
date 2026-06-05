# shellcheck shell=bash
# gh-multi-profile.sh — wrap `gh auth login` and `gh auth refresh` so the
# device-code URL is printed instead of being passed to xdg-open.
#
# Why: gh's --web flow uses whichever Firefox profile is the system default,
# which is rarely the right one when the host has multiple GitHub identities.
# This wrapper hands the URL to /usr/local/bin/gh-print-auth-url which prints
# it, letting the user paste it into the correct browser profile manually.
#
# Scope: only `gh auth login` and `gh auth refresh` are intercepted; every
# other `gh` subcommand passes through unchanged via `command gh`. The
# wrapper is a no-op outside those two flows.
#
# Deployed by: playbooks/imports/play-github-cli-multi.yml
# Helper:     /usr/local/bin/gh-print-auth-url

gh() {
    if [[ "$1" == "auth" && ( "$2" == "login" || "$2" == "refresh" ) ]]; then
        GH_BROWSER=/usr/local/bin/gh-print-auth-url command gh "$@"
    else
        command gh "$@"
    fi
}

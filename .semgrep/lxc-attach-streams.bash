#!/usr/bin/env bash
# Fixture for lxc-attach-file-stream (src-go/qa/lxcattach; CLAUDE/QA.md). A `ruleid:` comment
# marks the command on the next line as one that must be reported; no other line may be.
#
# lxc-attach makes each of its standard streams that is not /dev/null the container root's own,
# with a mode only root can read, so a file the caller redirected it into is lost to the caller.
set -euo pipefail

LXC_ATTACH=(sudo -n lxc-attach)
err="$(mktemp)"

in_guest() {
    local container="$1"
    shift
    "${LXC_ATTACH[@]}" -n "${container}" --clear-env -- "$@"
}

session_run() {
    case "$1" in
        lxc) in_guest c "${@:2}" ;;
        *) podman exec -i c "${@:2}" ;;
    esac
}

# The originating shape (Plan 00211's test question): stderr to a file two wrappers away.
ask() {
    local out rc=0
    out="$(printf 'q\n' |
        # ruleid: lxc-attach-file-stream
        session_run lxc pingbus ask-human 2>"${err}")" || rc=$?
    cat -- "${err}" >&2
    return "${rc}"
}

# Cleared: stderr goes through a process substitution, so lxc-attach sees a pipe.
ask_fixed() {
    local out rc=0
    out="$(printf 'q\n' | session_run lxc pingbus ask-human 2> >(cat >"${err}"))" || rc=$?
    cat -- "${err}" >&2
    return "${rc}"
}

# The tool named directly, with stdout to a file.
# ruleid: lxc-attach-file-stream
lxc-attach -n c -- true >"${err}"

# Both streams on the file at once.
# ruleid: lxc-attach-file-stream
"${LXC_ATTACH[@]}" -n c -- true &>"${err}"

# Stdin read from a file is handed to root as well.
# ruleid: lxc-attach-file-stream
in_guest c cat <"${err}"

# An enclosing group sends stderr to the file for everything inside it.
{
    # ruleid: lxc-attach-file-stream
    in_guest c true
} 2>"${err}"

# An enclosing loop reads its list from a file.
while read -r name; do
    # ruleid: lxc-attach-file-stream
    sudo -n lxc-attach -n "${name}" -- true
done <"${err}"

# A capture only makes stdout a pipe; stderr is still the file.
# ruleid: lxc-attach-file-stream
out="$(in_guest c true 2>"${err}")"

# A runner in front of a wrapper still runs it.
# ruleid: lxc-attach-file-stream
timeout 5 session_run lxc true >>"${err}"

# stderr joined to a stdout that is a file.
# ruleid: lxc-attach-file-stream
in_guest c true >"${err}" 2>&1

# A function whose own body is redirected.
quiet_guest() {
    # ruleid: lxc-attach-file-stream
    in_guest c "$@"
} 2>"${err}"

# A descriptor carried into a group, then used for stderr.
{
    # ruleid: lxc-attach-file-stream
    in_guest c true 2>&3
} 3>"${err}"

# Cleared: /dev/null is skipped by lxc-attach.
in_guest c true </dev/null >/dev/null 2>/dev/null

# Cleared: every stream a pipe (the AgentNotes form).
printf 'x' | in_guest c cat 2>&1 | cat

# Cleared: both streams swapped through pipes, and stderr back onto the caller's.
{ in_guest c true 2>&1 1>&3 3>&- | cat >&2; } 3>&1 | cat

# Cleared: a capture with stderr joined to it.
out="$(in_guest c true 2>&1)"

# Cleared: a here-string and a here-document are the shell's own.
in_guest c cat <<<"x"
in_guest c cat <<EOF
x
EOF

# Cleared: the tool named as data, not run.
printf 'lxc-attach -n c\n' >"${err}"
grep -c lxc-attach "${err}" >"${err}.count"

# Cleared: a podman session is not an LXC member.
podman exec -i c true 2>"${err}"

printf '%s %s\n' "${out}" "$(quiet_guest true)"

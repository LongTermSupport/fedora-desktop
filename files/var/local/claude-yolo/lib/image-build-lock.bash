#!/bin/bash
# One build of an image at a time on this host (Plan 00216). Every image build and in-place
# update, the launcher's and play-claude-yolo.yml's, runs through ccy_image_build_locked: a
# kernel flock on the image's lock file, $XDG_RUNTIME_DIR/ccy/image-builds/<image>.lock, with
# ':' and '/' in the image written as '_'. claude-yolo:latest's lock covers :base too, since
# both come from one build context. The kernel lets the lock go when its holder exits or dies,
# so the lock itself cannot go stale. The holder's descriptor is closed in every program the
# build starts, so a child that outlives the build (podman's pause process) cannot keep it.
#
# While a build runs, the lock file holds its record, one line, emptied before the lock is let go:
#   <pid> <epoch seconds the build began> <image>
# A record whose process has gone is stale: whoever reads it says so and ignores it. Nobody
# deletes it; the next holder writes over it. `ccy --build-lock` (the Go front,
# src-go/internal/ccylaunch/buildlock.go) reads the same record, and also refuses a pid that
# another process started after the build began.
#
# A waiter waits for as long as the build takes, because a real build takes many minutes, and
# says on stderr every CCY_IMAGE_BUILD_REPORT_SECONDS (30) whose build it waits for and how to
# stop it. Once it holds the lock it runs the check again, so it uses the image the holder built
# instead of building it again. The build it runs gets no errexit (it runs as `build || rc=$?`),
# so every build function checks each of its own steps.

# ccy_image_build_locked <image> <check> [args...] -- <build> [args...]
#   The check succeeds when no build is needed: it runs first without the lock, and again once
#   the lock is held. `false` always builds (an explicit --rebuild). `--unless-changed <image>`
#   builds unless that image's ID changed while this launch waited (an in-place update another
#   launch just made). Returns 0 when the image is current, else the build's status.
ccy_image_build_locked() {
    local key="${1:?ccy_image_build_locked needs the image}" dir file fd rc=0 waited="" seen
    local -a check=()
    shift
    while [[ $# -gt 0 && "$1" != "--" ]]; do check+=("$1") && shift; done
    if [[ $# -lt 2 || ${#check[@]} -eq 0 ]]; then
        printf 'ccy: ccy_image_build_locked %s needs <check> -- <build>.\n' "$key" >&2
        return 2
    fi
    shift
    if [[ "${check[0]}" == --unless-changed ]]; then
        seen="$(container_cmd image inspect "${check[1]:?--unless-changed needs an image}" --format '{{.Id}}')" || return 1
        check=(ccy_image_changed "${check[1]}" "$seen")
    elif "${check[@]}"; then
        return 0
    fi
    dir="${XDG_RUNTIME_DIR:-/run/user/$EUID}/ccy/image-builds"
    file="$dir/${key//[:\/]/_}.lock"
    if ! (umask 077 && mkdir -p "$dir" && [[ -d "$dir" && ! -L "$dir" && -O "$dir" ]] && : >>"$file") || ! exec {fd}>>"$file"; then
        printf 'ccy: cannot open the image build lock %s: its directory must be a directory of your own, not a link.\n' "$file" >&2
        return 1
    fi
    flock -n -E 75 "$fd" || rc=$?
    while [[ $rc -eq 75 ]]; do
        waited=yes rc=0
        ccy_image_build_waiting "$file" "$key"
        flock -w "${CCY_IMAGE_BUILD_REPORT_SECONDS:-30}" -E 75 "$fd" || rc=$?
    done
    if [[ $rc -ne 0 ]] || ! printf '%s %s %s\n' "$$" "$EPOCHSECONDS" "$key" >"$file"; then
        printf 'ccy: could not take the image build lock %s (flock exit %s), or write its record.\n' "$file" "$rc" >&2
        exec {fd}>&-
        return 1
    fi
    if "${check[@]}" >/dev/null {fd}>&-; then
        [[ -z "$waited" ]] || printf 'ccy: ✓ %s was built while this launch waited; using it.\n' "$key" >&2
    else
        "$@" {fd}>&- || rc=$?
    fi
    : >"$file" || { printf 'ccy: could not empty the image build lock record %s.\n' "$file" >&2 && rc=1; }
    exec {fd}>&-
    return "$rc"
}

# ccy_image_build_waiting <lock file> <image> — say whose build this launch waits for.
ccy_image_build_waiting() {
    local pid="" began=""
    if ! read -r pid began _ <"$1" || [[ ! "$pid $began" =~ ^[0-9]+\ [0-9]+$ ]]; then
        printf 'ccy: waiting for another build of %s to finish (it has not written its record yet).\n' "$2" >&2
    elif [[ ! -d "/proc/$pid" ]]; then
        printf 'ccy: waiting for the build lock of %s. Its record names process %s, which has gone: that record is stale and is ignored.\n' "$2" "$pid" >&2
    else
        printf 'ccy: waiting for process %s to finish building %s (started %dm%02ds ago). A build can take many minutes; if this one has hung, see it and stop it with: ccy --build-lock\n' \
            "$pid" "$2" $(((EPOCHSECONDS - began) / 60)) $(((EPOCHSECONDS - began) % 60)) >&2
    fi
}

# ccy_image_changed <image> <id> — the image's ID now differs from that one.
ccy_image_changed() {
    local now
    now="$(container_cmd image inspect "$1" --format '{{.Id}}')" && [[ "$now" != "$2" ]]
}

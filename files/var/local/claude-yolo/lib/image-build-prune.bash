#!/bin/bash
# ccy removes what its own image build replaced (Plan 00235). ccy_image_build_locked
# (lib/image-build-lock.bash, which sources this file) runs every build and in-place update
# through ccy_image_build_pruned while it holds the image's build lock. That lock is the one
# image's, and the record is the whole repository's, so a build of another image of it (a
# project image built on the old :latest) can run beside this one: `replaced` is given this
# build's lock with --lock and removes nothing while any other build lock is held.
#
# Before the build, `container-images before <repository>` records the id each tag of the
# repository names. The whole repository is recorded, because a build of claude-yolo:latest
# re-tags :base too. After a build that succeeded, `container-images replaced` removes each
# recorded image no tag now names and no container, running or stopped, uses, and says on
# stderr what it removed and what it kept and why. A removal the engine refuses (an old :latest
# a project image is built on) is reported and is not a failure: the daily prune
# (container-images-prune.timer) removes it once nothing holds it. A record or a list
# container-images cannot make is a failure: before the build, the build is not run; after a
# build that succeeded, it is reported and the build's success is returned, because the image
# the launch needs is built and the daily prune removes what this run left.
#
# container-images is the Go command src-go/cmd/container-images, installed in ~/.local/bin by
# tasks/claude-yolo-image.yml; CCY_CONTAINER_IMAGES names another path (the play's own build
# runs with no ~/.local/bin on PATH). The engine is the launcher's CONTAINER_ENGINE.

# ccy_image_build_pruned <build lock file> <repository> <build> [args...]
#   Returns the build's status; 1 when the record could not be made, and nothing was built.
ccy_image_build_pruned() {
    local lock="${1:?ccy_image_build_pruned needs the build lock}" repo="${2:?ccy_image_build_pruned needs the repository}" before
    local images="${CCY_CONTAINER_IMAGES:-container-images}" engine="${CONTAINER_ENGINE:-podman}"
    shift 2
    if ! before="$("$images" --engine "$engine" before "$repo")"; then
        printf 'ccy: could not record the images of %s before its build, so it was not built (%s is installed by play-claude-yolo.yml).\n' "$repo" "$images" >&2
        return 1
    fi
    "$@" || return
    # FAIL-FAST-OK: the build succeeded and is what the launch needs; the daily prune
    # (container-images-prune.timer) is the backstop for what this run could not remove.
    "$images" --engine "$engine" replaced --lock "$lock" "$repo" /dev/stdin <<<"$before" ||
        printf 'ccy: the images the build of %s replaced were not removed (container-images exit %s); the daily prune removes them.\n' "$repo" "$?" >&2
}

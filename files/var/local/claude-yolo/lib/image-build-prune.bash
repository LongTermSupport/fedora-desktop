#!/bin/bash
# ccy removes what its own image build replaced (Plan 00235). ccy_image_build_locked
# (lib/image-build-lock.bash, which sources this file) runs every build and in-place update
# through ccy_image_build_pruned while it holds the image's build lock, so no other build of the
# repository runs between the record and the removal.
#
# Before the build, `container-images before <repository>` records the id each tag of the
# repository names. The whole repository is recorded, because a build of claude-yolo:latest
# re-tags :base too. After a build that succeeded, `container-images replaced` removes each
# recorded image no tag now names and no container, running or stopped, uses, and says on
# stderr what it removed and what it kept and why. A removal the engine refuses (an old :latest
# a project image is built on) is reported and is not a failure: the daily prune
# (container-images-prune.timer) removes it once nothing holds it. A record or a list
# container-images cannot make is a failure: before the build, the build is not run.
#
# container-images is the Go command src-go/cmd/container-images, installed in ~/.local/bin by
# tasks/claude-yolo-image.yml; CCY_CONTAINER_IMAGES names another path (the play's own build
# runs with no ~/.local/bin on PATH). The engine is the launcher's CONTAINER_ENGINE.

# ccy_image_build_pruned <repository> <build> [args...]
#   Returns the build's status, or container-images' when the build succeeded.
ccy_image_build_pruned() {
    local repo="${1:?ccy_image_build_pruned needs the repository}" before
    local images="${CCY_CONTAINER_IMAGES:-container-images}" engine="${CONTAINER_ENGINE:-podman}"
    shift
    if ! before="$("$images" --engine "$engine" before "$repo")"; then
        printf 'ccy: could not record the images of %s before its build, so it was not built (%s is installed by play-claude-yolo.yml).\n' "$repo" "$images" >&2
        return 1
    fi
    "$@" || return
    "$images" --engine "$engine" replaced "$repo" /dev/stdin <<<"$before"
}

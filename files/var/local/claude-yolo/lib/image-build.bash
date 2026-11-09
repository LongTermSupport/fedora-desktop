#!/bin/bash
# The launcher's image checks and its base image build, each run under the image's build lock
# (lib/image-build-lock.bash, Plan 00216). A check is what a launch runs before it takes the
# lock and again once it holds it, so a launch that waited for another's build uses that build
# instead of making its own. Each check reads only what the build writes before it lets the
# lock go: the image's labels, or the project image's hash files.
#
# The engine's answer to "is there such an image" is captured, not shown: any failure to
# inspect counts as no image, and the build that follows says what is wrong with the engine.

# ccy_base_image_current <required version> — claude-yolo:latest exists and is that version,
# built from this Dockerfile; validate_container_version says why not.
ccy_base_image_current() {
    local probe
    if ! probe="$(container_cmd image inspect "$IMAGE_NAME" --format '{{.Id}}' 2>&1)" || [ -z "$probe" ]; then
        echo "Building Claude YOLO container image (first time setup)..."
        return 1
    fi
    validate_container_version "$IMAGE_NAME" "$DOCKERFILE_DIR/Dockerfile" "${1:?ccy_base_image_current needs the required version}"
}

# ccy_base_image_build [flag] — build claude-yolo:latest and claude-yolo:base from the build
# context (the flag is --no-cache or empty), then clear the per-image update-check stamps so
# every image checks for a Claude Code update again.
ccy_base_image_build() {
    if [ ! -f "$DOCKERFILE_DIR/Dockerfile" ]; then
        print_error "Dockerfile not found at $DOCKERFILE_DIR/Dockerfile. Run the play: playbooks/imports/play-claude-yolo.yml"
        return 1
    fi
    build_container_with_hash "$IMAGE_NAME" "$DOCKERFILE_DIR" "${1:-}" "$BASE_IMAGE_NAME" || return 1
    rm -rf "$VERSION_CHECK_CACHE" || return 1
    echo "✓ Container image built"
}

# ccy_claude_is <image> <version> — the image's Claude Code is that version, as the daily
# update asks it (claude --version in a throwaway container): an update another launch made
# while this one waited is seen, and a change to the image for any other reason is not.
ccy_claude_is() {
    local probe
    probe="$(container_cmd run --rm --pids-limit "$CCY_PIDS_LIMIT" --entrypoint claude "$1" --version 2>&1)" &&
        [[ "$probe" =~ ([0-9]+\.[0-9]+\.[0-9]+) ]] && [ "${BASH_REMATCH[1]}" = "$2" ]
}

# ccy_project_image_current <reason> <image> <hash file> <hash> <base file> <base> — the
# project image exists, and its hash files say it was built from this Dockerfile hash on this
# base image. Never for a --rebuild (reason "--rebuild flag"), which asked for a new build.
ccy_project_image_current() {
    local probe
    [ "$1" != "--rebuild flag" ] && probe="$(container_cmd image inspect "$2" --format '{{.Id}}' 2>&1)" &&
        [ -n "$probe" ] && [ -f "$3" ] && [ "$(<"$3")" = "$4" ] && [ -f "$5" ] && [ "$(<"$5")" = "$6" ]
}

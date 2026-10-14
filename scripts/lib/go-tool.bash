#!/usr/bin/bash
# run_go_tool <command> [args...] — builds src-go/qa/cmd/<command> into a private directory under
# the cache (not /tmp, which can be noexec) and runs it, so the tool's exit code reaches the
# caller (go run flattens it to 1). A new build per run means the binary always matches the
# source. SOURCE this.
#
# Exit 2: go missing, no usable cache directory (HOME unset), or the tool did not build.

run_go_tool() {
    local tool="$1" cache status=0 qa_dir
    shift
    qa_dir="$(dirname "${BASH_SOURCE[0]}")/../../src-go/qa"
    if ! command -v go > /dev/null; then
        echo "✗ $tool: go not installed (install via playbooks/imports/play-golang.yml)" >&2
        return 2
    fi
    if [[ -z "${XDG_CACHE_HOME:-}" && -z "${HOME:-}" ]]; then
        echo "✗ $tool: neither XDG_CACHE_HOME nor HOME is set; no cache directory for the build" >&2
        return 2
    fi
    cache="${XDG_CACHE_HOME:-$HOME/.cache}/fedora-desktop-qa"
    mkdir -p "$cache"
    GO_TOOL_BIN_DIR="$(mktemp -d -p "$cache")"
    trap 'rm -rf "$GO_TOOL_BIN_DIR"' EXIT
    if ! go build -C "$qa_dir" -o "$GO_TOOL_BIN_DIR/$tool" "./cmd/$tool"; then
        echo "✗ $tool: the tool did not build (src-go/qa/cmd/$tool); qa-go.bash checks it" >&2
        return 2
    fi
    "$GO_TOOL_BIN_DIR/$tool" "$@" || status=$?
    return "$status"
}

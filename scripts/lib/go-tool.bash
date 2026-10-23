#!/usr/bin/bash
# run_go_tool <command> [args...] — builds src-go/qa/cmd/<command>, a QA tool of the nested qa
# module, and runs it. run_go_command <command> [args...] — the same for src-go/cmd/<command> of
# the main module, offline from src-go/vendor (-mod=vendor) with the installed toolchain only
# (GOTOOLCHAIN=local), for a command run from the checkout rather than installed by a play.
#
# Each builds into a private directory under the cache (not /tmp, which can be noexec), removed
# when the caller exits, and returns the command's own exit code (go run flattens it to 1). A new
# build per run means the binary always matches the source. SOURCE this.
#
# Exit 2: go missing, no usable cache directory (HOME unset), or the command did not build.

run_go_tool() {
    _go_build_and_run "src-go/qa" "the Go gate (qa-go-qa.bash) checks it" "$@"
}

run_go_command() {
    GOFLAGS=-mod=vendor GOTOOLCHAIN=local _go_build_and_run "src-go" \
        "the Go gate (qa-go-qa.bash) checks it, and the go installed must be at least go.mod's go line" \
        --release "$@"
}

# _go_build_and_run <module dir, from the checkout> <hint on failure> [--release] <command> [args...]
# --release builds as tasks/build-go-command.yml does: -trimpath -buildvcs=false.
_go_build_and_run() {
    local module="$1" hint="$2" tool cache status=0 dir
    local -a flags=()
    shift 2
    if [[ "${1:-}" == --release ]]; then
        flags+=(-trimpath -buildvcs=false)
        shift
    fi
    tool="$1"
    shift
    dir="$(dirname "${BASH_SOURCE[0]}")/../../$module"
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
    if ! go build -C "$dir" "${flags[@]}" -o "$GO_TOOL_BIN_DIR/$tool" "./cmd/$tool"; then
        echo "✗ $tool: it did not build ($module/cmd/$tool); $hint" >&2
        return 2
    fi
    "$GO_TOOL_BIN_DIR/$tool" "$@" || status=$?
    return "$status"
}

#!/usr/bin/env bash
# Print the staged files that change the ccy program and so demand a CCY_VERSION bump.
# Usage: ccy-staged.bash LAUNCHER LIB_DIR HELPER_DIR FILE...   (run from the repository root)
#
# The program is the bash launcher, its libraries, ccy's one-key agent, and the Go front
# installed as the launcher: its own paths and every file src-go/cmd/claude-yolo builds from
# (lib/go-staged.bash: its packages, vendored code and embedded files, and the module files).
# Test files and the differential harness change no program. Go is asked only when a src-go file
# outside the front's own paths is staged; a Go that cannot answer ends the run (exit 1), since
# an unjudged change is not a clean one.
set -euo pipefail

launcher=$1 lib_dir=$2 helper_dir=$3
shift 3

go_files=()
for file in "$@"; do
    case "$file" in
    src-go/internal/ccylaunch/difftest/* | src-go/*_test.go) ;;
    "$launcher" | "$lib_dir"/*.bash | "$helper_dir"/*.py | src-go/cmd/claude-yolo/* | src-go/internal/ccylaunch/*) echo "$file" ;;
    src-go/*) go_files+=("$file") ;;
    esac
done
if [ ${#go_files[@]} -gt 0 ] && [ -f src-go/cmd/claude-yolo/main.go ]; then
    "${BASH_SOURCE[0]%/*}/go-staged.bash" ./cmd/claude-yolo -- "${go_files[@]}"
fi

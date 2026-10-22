#!/usr/bin/env bash
# Print the staged files that change the ccy program and so demand a CCY_VERSION bump.
# Usage: ccy-staged.bash LAUNCHER LIB_DIR HELPER_DIR FILE...   (run from the repository root)
#
# The program is the bash launcher, its libraries, ccy's one-key agent, and the Go front
# installed as the launcher: its own paths and every package it builds from (`go list -deps` of
# src-go/cmd/claude-yolo, vendored code included), with src-go/go.mod and go.sum. Test files
# and the differential harness change no program. Go is asked once, and only when a src-go file
# outside the front's own paths is staged; a Go that cannot answer ends the run (exit 1), since
# an unjudged change is not a clean one.
set -euo pipefail

launcher=$1 lib_dir=$2 helper_dir=$3
shift 3

closure=""
in_closure() {
    [ -f src-go/cmd/claude-yolo/main.go ] || return 1
    case "$1" in src-go/go.mod | src-go/go.sum) return 0 ;; esac
    if [ -z "$closure" ]; then
        local root dirs
        root=$(pwd -P)
        dirs=$(cd src-go && GOFLAGS=-mod=vendor GOTOOLCHAIN=local go list -deps -f '{{if not .Standard}}{{.Dir}}{{end}}' ./cmd/claude-yolo) ||
            {
                echo "COMMIT REJECTED: go list could not name the packages the CCY Go front builds from." >&2
                exit 1
            }
        closure=$'\n'"${dirs//"$root/"/}"$'\n'
    fi
    [[ "$closure" == *$'\n'"$(dirname "$1")"$'\n'* ]]
}

for file in "$@"; do
    case "$file" in
    src-go/internal/ccylaunch/difftest/* | src-go/*_test.go) ;;
    "$launcher" | "$lib_dir"/*.bash | "$helper_dir"/*.py | src-go/cmd/claude-yolo/* | src-go/internal/ccylaunch/*) echo "$file" ;;
    src-go/qa/*) ;;
    src-go/*) if in_closure "$file"; then echo "$file"; fi ;;
    esac
done

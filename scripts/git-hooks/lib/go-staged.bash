#!/usr/bin/env bash
# Print the staged files a build of the given src-go packages is built from.
# Usage: go-staged.bash PACKAGE... -- FILE...   (run from the repository root; each PACKAGE as
# `go list` takes it in src-go, such as ./cmd/teamchat)
#
# A file is an input when it is src-go/go.mod, src-go/go.sum or src-go/vendor/modules.txt, when
# it sits in the directory of a package `go list -deps` names for those packages (vendored code
# included), or when one of those packages embeds it, which may sit in a subdirectory. Test files
# and the qa module (a module of its own, built into no program) are not inputs. Go is asked once,
# and only when a file that could be an input is staged; a Go that cannot answer ends the run
# (exit 1), since an unjudged change is not a clean one.
set -euo pipefail

packages=()
while [ $# -gt 0 ] && [ "$1" != "--" ]; do
    packages+=("$1")
    shift
done
if [ $# -eq 0 ] || [ ${#packages[@]} -eq 0 ]; then
    echo "usage: go-staged.bash PACKAGE... -- FILE..." >&2
    exit 2
fi
shift

closure="" module=""
in_closure() {
    # Judged by import path, never by filesystem path: the Go tool takes its directory from $PWD,
    # so a checkout reached through a symlink prints directories that disagree with pwd -P.
    if [ -z "$closure" ]; then
        local paths
        paths=$(cd src-go && GOFLAGS=-mod=vendor GOTOOLCHAIN=local go list -deps \
            -f '{{if not .Standard}}{{.ImportPath}}{{range .EmbedFiles}}{{"\n"}}{{$.ImportPath}}/{{.}}{{end}}{{end}}' "${packages[@]}") ||
            {
                echo "COMMIT REJECTED: go list could not name the packages ${packages[*]} (in src-go) build from." >&2
                exit 1
            }
        module=$(grep -m1 '^module ' src-go/go.mod) && module=${module#module }
        closure=$'\n'"$paths"$'\n'
    fi
    local mapped
    case "$1" in
    src-go/vendor/*) mapped=${1#src-go/vendor/} ;;
    *) mapped=$module/${1#src-go/} ;;
    esac
    [[ "$closure" == *$'\n'"$mapped"$'\n'* || "$closure" == *$'\n'"$(dirname "$mapped")"$'\n'* ]]
}

for file in "$@"; do
    case "$file" in
    src-go/go.mod | src-go/go.sum | src-go/vendor/modules.txt) echo "$file" ;;
    src-go/*_test.go | src-go/qa/*) ;;
    src-go/*) if in_closure "$file"; then echo "$file"; fi ;;
    esac
done

#!/usr/bin/env bash
# Print the git pathspecs of the repository source the ccy image's builder stage builds from: the
# one definition of that source (issue 285). files/var/local/claude-yolo/image-source-paths is
# this output, committed. The play keys the builder stage on the last commit that changed these
# paths, the stage takes only these paths from that commit, and the pre-commit hook's container
# version gate counts a staged file as an image input by them; the hook rejects a commit whose list
# is not this output ([CCY-IMAGE-SOURCES], CLAUDE/QA.md#ccy-image-sources). Regenerate with:
#   scripts/git-hooks/lib/ccy-image-source-paths.bash files/var/local/claude-yolo \
#       >files/var/local/claude-yolo/image-source-paths
# Usage: ccy-image-source-paths.bash CONTEXT   (from the repository root; CONTEXT holds the Dockerfile)
#
# Read from the Dockerfile's ccy-builder stage: the packages of its `go build` lines, which go list
# -deps resolves as the stage builds (static, linux) for both architectures the image is built for, each package's own files
# (not its subdirectories) and the files it embeds; the module files; and, when the stage runs
# helpers.pingbus.bundle, the PACKAGES bundle.py names. Test files are built into nothing. A Go
# command the stage runs in a form not read here ends the run (exit 1), as does a Go that cannot
# name the packages.
set -euo pipefail

dockerfile=$1/Dockerfile
builder=$(awk '$1 == "FROM" { b = ($NF == "ccy-builder"); next } b' "$dockerfile")
# The stage's RUN lines joined over their continuations, split into commands. A Go command read
# any other way (no ./ package, or go install, run or generate) prints "!".
mapfile -t packages < <(awk '
    { line = line " " $0 }
    /\\$/ { sub(/\\$/, "", line); next }
    {
        sub(/^[[:space:]]*RUN[[:space:]]+/, "", line)
        n = split(line, commands, /;|&&|\|\|/)
        for (i = 1; i <= n; i++) {
            m = split(commands[i], w, " ")
            found = 0
            if (w[1] == "go" && w[2] == "build") for (j = 3; j <= m; j++) if (w[j] ~ /^\.\//) { print w[j]; found = 1 }
            if (w[1] == "go" && w[2] ~ /^(build|install|run|generate)$/ && !found) print "!"
            if (w[1] != "go" && w[1] !~ /^#/ && commands[i] ~ /(^|[^[:alnum:]_])go (build|install|run|generate) /) print "!"
        }
        line = ""
    }' <<<"$builder")
if [[ " ${packages[*]} " == *" ! "* ]]; then
    echo "COMMIT REJECTED [CCY-IMAGE-INPUT]: the ccy-builder stage of $dockerfile runs Go in a form this hook cannot read; build ./ packages with plain go build commands (see scripts/git-hooks/lib/ccy-image-source-paths.bash)." >&2
    exit 1
fi

if [ ${#packages[@]} -gt 0 ]; then
    printf '%s\n' ':(glob)src-go/go.mod' ':(glob)src-go/go.sum' ':(glob)src-go/vendor/modules.txt'
    # Go takes a package's directory from $PWD, so it is made relative to the same $PWD. A file a
    # package embeds from its own directory is already among that directory's files.
    (
        cd src-go
        for arch in amd64 arm64; do
            if ! GOOS=linux GOARCH=$arch CGO_ENABLED=0 GOFLAGS=-mod=vendor GOTOOLCHAIN=local go list -deps \
                -f '{{if not .Standard}}{{.Dir}}/*{{range .EmbedFiles}}{{"\n"}}{{$.Dir}}/{{.}}{{end}}{{end}}' "${packages[@]}"; then
                echo "go list could not name the packages ${packages[*]} (in src-go) build from for linux/$arch." >&2
                exit 1
            fi
        done | awk -v root="$PWD/" '
            index($0, root) != 1 { print "go list named " $0 ", outside " root > "/dev/stderr"; bad = 1; exit }
            { rel = substr($0, length(root) + 1); dir = rel; sub(/\/[^\/]*$/, "", dir) }
            rel ~ /\/\*$/ { own[dir] = 1 }
            rel ~ /\/\*$/ || !own[dir] { print ":(glob)src-go/" rel }
            END { exit bad }' | LC_ALL=C sort -u
    )
    echo ':(exclude,glob)src-go/**/*_test.go'
fi
if [[ "$builder" == *helpers.pingbus.bundle* ]]; then
    awk -F'"' '/^PACKAGES = / { for (i = 2; i <= NF; i += 2) print $i }' helpers/pingbus/bundle.py
fi

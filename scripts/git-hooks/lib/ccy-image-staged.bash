#!/usr/bin/env bash
# Print the staged files the ccy image is built from, which demand a container version bump.
# Usage: ccy-image-staged.bash CONTEXT FILE...   (run from the repository root; CONTEXT is the
# directory holding the Dockerfile)
#
# Read from the staged Dockerfile, so a file the image starts using is guarded without anyone
# listing it: every repo file its COPY lines take from either build-context root (a --from copy
# takes from a build stage, not the repo), and every file the Go commands its ccy-builder stage
# builds are built from (its `go build` packages, through lib/go-staged.bash). Listed by hand: the
# Dockerfile, the fzf pin the play copies into the context, and the pingbus zipapp's packages,
# which the builder stage bundles with helpers/pingbus/bundle.py. The host-kit files under the
# copied optional/ never reach the context. scripts/test-ccy-container-version-hook.bash checks
# all of this against the real play and the real Dockerfile.
set -euo pipefail

context=$1
shift
dockerfile=$context/Dockerfile
paths=("$dockerfile" helpers/pingbus helpers/agent_bus vars/fzf-pin.yml)
packages=()
if git cat-file -e ":$dockerfile" 2>/dev/null; then
    staged=$(git show ":$dockerfile")
    mapfile -t sources < <(awk '
        $1 == "COPY" {
            from = 0; n = 0
            for (i = 2; i <= NF; i++) {
                if ($i ~ /^--from=/) from = 1
                else if ($i !~ /^--/) args[++n] = $i
            }
            if (!from) for (i = 1; i < n; i++) { s = args[i]; sub(/\/$/, "", s); print s }
        }' <<<"$staged")
    for src in "${sources[@]}"; do paths+=("$context/$src" "files/opt/claude-yolo/$src"); done
    # The builder stage's RUN lines joined over their continuations, split into commands. A Go
    # command read any other way (no ./ package, or go install, run or generate) prints "!", so
    # a build form this does not understand stops the commit instead of going unguarded.
    mapfile -t packages < <(awk '
        $1 == "FROM" { builder = ($NF == "ccy-builder"); line = ""; next }
        !builder { next }
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
        }' <<<"$staged")
fi
if [[ " ${packages[*]} " == *" ! "* ]]; then
    echo "COMMIT REJECTED [CCY-IMAGE-INPUT]: the ccy-builder stage of $dockerfile runs Go in a form this hook cannot read; build ./ packages with plain go build commands (see scripts/git-hooks/lib/ccy-image-staged.bash)." >&2
    exit 1
fi

for file in "$@"; do
    case "$file" in
    files/opt/claude-yolo/optional/agent-bus/README.* | files/opt/claude-yolo/optional/agent-bus/agent-bus-claude) continue ;;
    esac
    for path in "${paths[@]}"; do
        if [ "$file" = "$path" ] || [[ "$file" == "$path/"* ]]; then
            echo "$file"
            continue 2
        fi
    done
done
if [ ${#packages[@]} -gt 0 ]; then
    "${BASH_SOURCE[0]%/*}/go-staged.bash" "${packages[@]}" -- "$@"
fi

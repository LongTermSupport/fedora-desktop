#!/usr/bin/env bash
# Print the staged files the ccy image is built from, which demand a container version bump.
# Usage: ccy-image-staged.bash CONTEXT FILE...   (run from the repository root; CONTEXT is the
# directory holding the Dockerfile)
#
# Read from the staged tree, so a file the image starts using is guarded without anyone listing it
# here: every repo file the Dockerfile's COPY lines take from either build-context root (a --from
# copy takes from a build stage, not the repo), and every file the git pathspecs of the staged
# image-source-paths match: the source the builder stage builds from, its Go and the pingbus
# zipapp's packages. The play keys the builder stage on that same list and the stage takes its
# source by it, so the change that rebuilds the image is the change that needs the bump (issue
# 285). A commit that could change what the stage builds from (a src-go file, the Dockerfile,
# bundle.py, the list) is first held to lib/ccy-image-source-paths.bash: a list that is not its
# output stops the commit ([CCY-IMAGE-SOURCES]). Listed by hand: the Dockerfile and the fzf pin the
# play copies into the context. The host-kit files under the copied optional/ never reach the
# context. scripts/test-ccy-container-version-hook.bash checks all of this against the real play
# and the real Dockerfile.
set -euo pipefail

context=$1
shift
lib=$(cd "${BASH_SOURCE[0]%/*}" && pwd)
dockerfile=$context/Dockerfile
list=$context/image-source-paths
paths=("$dockerfile" vars/fzf-pin.yml)
built=$'\n'
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
    listed=""
    if git cat-file -e ":$list" 2>/dev/null; then listed=$(git show ":$list"); fi
    for file in "$@"; do
        case "$file" in
        src-go/* | "$dockerfile" | "$list" | helpers/pingbus/bundle.py)
            # Judged on the staged tree, as the commit will hold it: a partly staged change or an
            # untracked file in the working tree must not decide what the list should be.
            index=$(mktemp -d)
            trap 'rm -rf "$index"' EXIT
            git ls-files -z -- src-go "$dockerfile" helpers/pingbus/bundle.py | git checkout-index -z --stdin --prefix="$index/"
            wanted=$(cd "$index" && "$lib/ccy-image-source-paths.bash" "$context")
            if [ "$wanted" != "$listed" ]; then
                echo "COMMIT REJECTED [CCY-IMAGE-SOURCES]: $list is not the source the ccy-builder stage builds from; regenerate it with scripts/git-hooks/lib/ccy-image-source-paths.bash $context >$list and stage it (CLAUDE/QA.md#ccy-image-sources)." >&2
                exit 1
            fi
            break
            ;;
        esac
    done
    if [ -n "$listed" ]; then
        mapfile -t specs <<<"$listed"
        built+=$(git diff --cached --name-only --diff-filter=ACDMRT -- "${specs[@]}")$'\n'
    fi
fi

for file in "$@"; do
    case "$file" in
    files/opt/claude-yolo/optional/agent-bus/README.* | files/opt/claude-yolo/optional/agent-bus/agent-bus-claude) continue ;;
    esac
    if [[ "$built" == *$'\n'"$file"$'\n'* ]]; then
        echo "$file"
        continue
    fi
    for path in "${paths[@]}"; do
        if [ "$file" = "$path" ] || [[ "$file" == "$path/"* ]]; then
            echo "$file"
            continue 2
        fi
    done
done

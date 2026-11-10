#!/usr/bin/env bash
# Defence for the class checkout-ref-bypass (issue #225; documented in CLAUDE/QA.md under
# that heading): a run.bash path that obtains or updates the fedora-desktop checkout without
# honouring RUN_BASH_GIT_REF, or runs code from the checkout before it is on that ref.
#
# HAZARD. A caller that verifies a signed commit passes its sha as RUN_BASH_GIT_REF and
# expects exactly that commit to be provisioned. A path that clones the default branch or
# runs `git pull` provisions whatever the branch tip is, so a push landing between the
# caller's check and the pull is run unverified, while the log says the declared ref was used.
#
# THE RULE, in three parts:
#   1. Single acquirer. Every `git clone|pull|fetch|checkout|reset|switch` in run.bash lives in
#      fd_checkout, which clones when absent and, with the ref set, pins instead of running
#      the update; there is no other way in.
#   2. Pin before use. Every main-flow line that runs code from a checkout (python3 -m helpers.*,
#      a checkout's scripts/) before the repository step converges has, earlier in its own
#      section (since the previous `title`), a call to fd_checkout or to a function that calls
#      it; and both repository steps (none and account mode) call fd_checkout.
#   3. Behaviour. fd_checkout, extracted and run against a local origin: a 40-hex ref and a
#      branch ref on an absent and an existing checkout whose origin is migrated (the account
#      path's shape), the plain pull with no ref, and the loud failures.
#
# `set -e` is deliberately NOT used: every case runs so the summary is the full picture.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
# RUN_BASH_UNDER_TEST points the rule at another copy, e.g. a past revision, to prove it red.
RUN_BASH="${RUN_BASH_UNDER_TEST:-$REPO_ROOT/run.bash}"
[ -f "$RUN_BASH" ] || { echo "FAIL: run.bash not found at $RUN_BASH" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
log="$work/git.log"  # everything the git fixtures and fd_checkout print; shown on a failure

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL [checkout-ref-bypass]: $label → '$got' (wanted '$want')"
    fi
}

# fnranges: "name start end" for every function definition, at any indentation, ending at the
# first `}` line with the opening line's indentation.
awk '
    !open && match($0, /^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*\(\) ?\{[[:space:]]*$/) {
        ind = $0; sub(/[^[:space:]].*/, "", ind)
        name = $0; sub(/^[[:space:]]*/, "", name); sub(/\(.*/, "", name)
        if (name != "main") { open = 1; start = NR }
        next
    }
    open && $0 == ind "}" { print name, start, NR; open = 0 }
' "$RUN_BASH" >"$work/fnranges"

in_fn() {  # in_fn <line> [names…] — true when <line> is inside one of the named functions (any, if none named)
    local line="$1"; shift
    awk -v l="$line" -v names=" $* " '
        (names == "  " || index(names, " " $1 " ")) && l >= $2 && l <= $3 { found = 1 }
        END { exit !found }' "$work/fnranges"
}
code_lines() {  # code_lines <ERE> — "N:text" for non-comment lines of run.bash matching
    grep -nE "$1" "$RUN_BASH" | grep -vE '^[0-9]+:[[:space:]]*#'
}

echo "=== 1. every clone/pull/fetch/checkout of the checkout is in fd_checkout ==="
# git with any global options before the subcommand (-C, -c, --git-dir…), and gh's clone.
git_re='(^|[^[:alnum:]_.-])(git([[:space:]]+[^[:space:];|&)]+)*[[:space:]]+(clone|pull|fetch|checkout|reset|switch)|gh[[:space:]]+repo[[:space:]]+clone)([^[:alnum:]_-]|$)'
msg_re='^[0-9]+:[[:space:]]*(info|success|warning|error|echo|hl_abort|fatal|headless_fail)[[:space:]]'
outside=""
while IFS= read -r hit; do
    n="${hit%%:*}"
    [[ "$hit" =~ $msg_re ]] && continue
    in_fn "$n" fd_checkout && continue
    outside+="${n} "
done < <(code_lines "$git_re")
check "no git clone/pull/fetch/checkout outside the acquirer (run.bash lines)" "" "${outside% }"

echo "=== 2. no checkout code runs before the checkout is pinned ==="
acquirers="fd_checkout"
while read -r name start end; do
    [ "$name" = fd_checkout ] && continue
    if awk -v s="$start" -v e="$end" 'NR > s && NR <= e && /(^|[^[:alnum:]_])fd_checkout([^[:alnum:]_]|$)/ { f = 1 } END { exit !f }' "$RUN_BASH"; then
        acquirers+=" $name"
    fi
done <"$work/fnranges"
acq_re="(^|[^[:alnum:]_])($(tr ' ' '|' <<<"$acquirers"))([^[:alnum:]_(]|$)"
converge="$(grep -nE '^fi[[:space:]]+# end: HL_GITHUB_ACCOUNTS=none' "$RUN_BASH" | cut -d: -f1)"
check "the repository step's convergence marker is found" "1" "$([ -n "$converge" ] && echo 1)"
converge="${converge:-0}"

section_has_acquirer() {  # section_has_acquirer <line> — an acquirer call between the previous title and <line>
    local line="$1" from
    from="$(awk -v l="$line" 'NR < l && /^[[:space:]]*title "/ { t = NR } END { print t + 0 }' "$RUN_BASH")"
    code_lines "$acq_re" | awk -F: -v a="$from" -v b="$line" '$1 > a && $1 < b { f = 1 } END { exit !f }'
}
runs_from_acquired_dir() {  # runs_from_acquired_dir <line> <text> — the line names a variable an earlier main-flow line set from an acquirer
    local line="$1" text="$2" var
    while read -r var; do
        code_lines "^[[:space:]]*(if ! )?${var}=\"\\\$\\((${acquirers// /|})\\)\"" |
            awk -F: -v b="$line" '$1 < b { f = 1 } END { exit !f }' && return 0
    done < <(grep -oE '\$\{?[A-Za-z_][A-Za-z0-9_]*' <<<"$text" | tr -d "\$\{" | sort -u)
    return 1
}
runner_re='python3([[:space:]]+-[[:alpha:]]+)*[[:space:]]+(-m[[:space:]]+helpers\.|[^[:space:]]*helpers/)|(^|[[:space:];&(])(exec +)?(\./|~/Projects/fedora-desktop/)scripts/[^ ]+'
# A function that runs checkout code is judged where it is called: runner functions are the
# functions holding a runner line, then, to a fixed point, the functions calling one.
runner_fns=""
while :; do
    calls_re="$runner_re"
    [ -z "$runner_fns" ] || calls_re+="|(^|[^[:alnum:]_])($(tr ' ' '|' <<<"${runner_fns# }"))([^[:alnum:]_(]|$)"
    grown="$runner_fns"
    while read -r name start end; do
        [[ " $grown " == *" $name "* ]] && continue
        if code_lines "$calls_re" | awk -F: -v s="$start" -v e="$end" '$1 > s && $1 <= e { f = 1 } END { exit !f }'; then
            grown+=" $name"
        fi
    done <"$work/fnranges"
    [ "$grown" = "$runner_fns" ] && break
    runner_fns="$grown"
done
calls_re="$runner_re"
[ -z "$runner_fns" ] || calls_re+="|(^|[^[:alnum:]_])($(tr ' ' '|' <<<"${runner_fns# }"))([^[:alnum:]_(]|$)"
unpinned=""
while IFS= read -r hit; do
    n="${hit%%:*}"
    [ "$n" -lt "$converge" ] || continue
    in_fn "$n" && continue
    section_has_acquirer "$n" && continue
    runs_from_acquired_dir "$n" "${hit#*:}" && continue
    unpinned+="${n} "
done < <(code_lines "$calls_re")
check "checkout code before the repository step is preceded by a pin in its section (run.bash lines)" "" "${unpinned% }"

while IFS= read -r t; do
    n="${t%%:*}"
    next="$(awk -v l="$n" 'NR > l && /^[[:space:]]*title "/ { print NR; exit }' "$RUN_BASH")"
    got="$(code_lines '(^|[^[:alnum:]_])fd_checkout[[:space:]]' | awk -F: -v a="$n" -v b="${next:-999999}" '$1 > a && $1 < b { f = 1 } END { print f + 0 }')"
    check "the repository step at line ${n} acquires the checkout through fd_checkout" "1" "$got"
done < <(grep -nE '^[[:space:]]*title "Setting up Project Directory and Repository' "$RUN_BASH")

echo "=== 3. fd_checkout honours RUN_BASH_GIT_REF ==="
: >"$work/fn.bash"
for fn in assert_clean_worktree fd_checkout; do
    awk -v fn="$fn" '$0 ~ "^[[:space:]]*" fn "\\(\\) ?\\{" {p=1} p {print} p && /^[[:space:]]*\}$/ {exit}' "$RUN_BASH" >>"$work/fn.bash"
done
if ! grep -qE '^[[:space:]]*fd_checkout\(\) ?\{' "$work/fn.bash"; then
    check "fd_checkout exists in run.bash" "defined" "absent"
else
    export GIT_CONFIG_GLOBAL="$work/gitconfig" GIT_CONFIG_NOSYSTEM=1
    git config --file "$GIT_CONFIG_GLOBAL" user.email "test@example.com"
    git config --file "$GIT_CONFIG_GLOBAL" user.name "test"
    git config --file "$GIT_CONFIG_GLOBAL" init.defaultBranch F44
    git config --file "$GIT_CONFIG_GLOBAL" advice.detachedHead false

    origin="$work/origin.git" seed="$work/seed"
    git init --quiet --bare "$origin"
    git clone --quiet "$origin" "$seed" 2>>"$log"
    commit() { echo "$1" >"$seed/f" && git -C "$seed" add f && git -C "$seed" commit --quiet -m "$1" && git -C "$seed" rev-parse HEAD; }
    c1="$(commit one)"
    git -C "$seed" push --quiet origin HEAD:F44
    c2="$(commit two)"
    git -C "$seed" push --quiet origin HEAD:F44
    git -C "$seed" push --quiet origin HEAD:other

    # The collaborators, stubbed: messages are noise here, and an abort is the exit it ends in.
    printf '%s\n' 'info() { :; }' 'success() { :; }' 'error() { :; }' \
        'hl_abort() { exit 1; }' 'fatal() { exit 1; }' >"$work/stubs.bash"
    # The function runs in a subshell per case, so an abort (exit 1) is a result, not the end.
    run_fd() {  # run_fd <ref|""> <dir> <url> [update args…] — prints "ok" or "aborted"
        local ref="$1"; shift
        if (
            # shellcheck source=/dev/null
            source "$work/stubs.bash"
            # shellcheck source=/dev/null
            source "$work/fn.bash"
            if [ -n "$ref" ]; then export RUN_BASH_GIT_REF="$ref"; else unset RUN_BASH_GIT_REF; fi
            fd_checkout "$@"
        ) >>"$log" 2>&1; then echo ok; else echo aborted; fi
    }
    stale_checkout() {  # a checkout at c1 on F44 whose origin URL is another one (the SSH/HTTPS migration)
        rm -rf "$1" && git clone --quiet "$origin" "$1" 2>>"$log" &&
            git -C "$1" checkout --quiet -B F44 "$c1" && git -C "$1" remote set-url origin "$work/elsewhere.git"
    }
    head_of() { git -C "$1" rev-parse HEAD; }
    branch_of() {  # the branch HEAD is on, or "detached"; symbolic-ref exits 1 only for a detached HEAD
        local b
        if b="$(git -C "$1" symbolic-ref --quiet --short HEAD)"; then echo "$b"; else echo detached; fi
    }

    d="$work/absent-sha"
    check "absent, 40-hex ref: cloned" "ok" "$(run_fd "$c1" "$d" "$origin" pull)"
    check "  and detached at the declared commit, not the tip" "$c1 detached" "$(head_of "$d") $(branch_of "$d")"

    d="$work/absent-branch"
    check "absent, branch ref: cloned" "ok" "$(run_fd other "$d" "$origin" pull)"
    check "  and on that branch at its origin tip" "$c2 other" "$(head_of "$d") $(branch_of "$d")"

    d="$work/account-sha"; stale_checkout "$d"
    commit three >>"$log"; git -C "$seed" push --quiet origin HEAD:F44
    check "existing (account mode), 40-hex ref: pinned" "ok" "$(run_fd "$c2" "$d" "$origin" pull)"
    check "  at the declared commit, detached, and not pulled to the tip" "$c2 detached" "$(head_of "$d") $(branch_of "$d")"
    check "  origin moved to the URL the path declares" "$origin" "$(git -C "$d" remote get-url origin)"
    check "  a second run on the detached checkout still succeeds" "ok" "$(run_fd "$c2" "$d" "$origin" pull)"

    d="$work/account-branch"; stale_checkout "$d"
    check "existing (account mode), branch ref: pinned" "ok" "$(run_fd F44 "$d" "$origin" pull)"
    check "  on the branch at its origin tip" "$(git -C "$seed" rev-parse HEAD) F44" "$(head_of "$d") $(branch_of "$d")"

    d="$work/no-ref-pull"; stale_checkout "$d"
    check "existing, no ref, update args: pulled" "ok" "$(run_fd "" "$d" "$origin" pull --ff-only)"
    check "  to the branch tip" "$(git -C "$seed" rev-parse HEAD)" "$(head_of "$d")"

    d="$work/no-ref-untouched"; stale_checkout "$d"
    check "existing, no ref, no update args: left as it is" "ok $c1" "$(run_fd "" "$d" "$origin") $(head_of "$d")"

    d="$work/dirty"; stale_checkout "$d"; echo dirt >"$d/f"
    check "existing and dirty, with a ref: aborts" "aborted" "$(run_fd "$c2" "$d" "$origin")"

    d="$work/unknown"; stale_checkout "$d"
    check "a 40-hex ref origin does not hold: aborts" "aborted" "$(run_fd "0123456789abcdef0123456789abcdef01234567" "$d" "$origin" pull)"
    check "a branch origin does not hold: aborts" "aborted" "$(run_fd no-such-branch "$d" "$origin" pull)"
fi

# 4. Capability marker. A caller that verifies a commit refuses to hand off unless run.bash
#    declares this fix with exactly one column-0 comment line. It must stay a comment: a
#    RUN_BASH_* variable would be read as configuration by the headless autodetect.
check "the capability marker line is present exactly once" "1" \
    "$(grep -c '^# run-bash-capability: git-ref-account-mode$' "$RUN_BASH")"

echo
echo "RESULT: passed: $passed failed: $failed"
if [ "$failed" -ne 0 ] && [ -s "$log" ]; then
    echo "--- git and fd_checkout output ---"
    cat "$log"
fi
[ "$failed" -eq 0 ]

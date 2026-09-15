#!/usr/bin/env bash
# Unit-test scripts/lib/run-log-scrub.bash — Plan 00121, Tasks 1.2 and 1.3.
#
# WHY THIS EXISTS. This repo scans for secrets at the git boundary only. Runtime artefacts —
# VM console logs, provisioning transcripts — live under untracked/, are never committed, and
# so are never looked at. Plan 00110 keeps a PAT-bearing run's transcript off the shared mount
# for exactly that reason.
#
# The property that matters is NOT "it redacts". It is that a redaction which MISSED something
# is refused rather than published. A scrubber is fail-open by nature: it writes a file it
# believes is clean, and a miss is silent. So the tests that matter here are the ones where the
# redactor was deliberately not told about a secret.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/scripts/lib/run-log-scrub.bash"

if [ ! -f "$LIB" ]; then
    echo "FAIL: run-log-scrub.bash not found at $LIB" >&2
    exit 1
fi

# shellcheck source=/dev/null
source "$LIB"

for fn in scrub_redact scrub_verify scrub_backstop; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: ${fn} is not defined after sourcing the library" >&2
        exit 1
    fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# Literal substring count, and it cannot fail the way `grep -c` does when the count is zero.
# index() is a substring search, not a regex, which is also what the thing under test must do.
count_in() {
    awk -v needle="$1" 'index($0, needle) { c++ } END { print c + 0 }' "$2"
}

# secret_file <name> <value> — a 0600 file holding one secret, trailing newline included,
# because that is how a real secret file arrives.
secret_file() {
    local path="$work/$1"
    printf '%s\n' "$2" > "$path"
    chmod 600 "$path"
    printf '%s' "$path"
}

artefact() {
    local path="$work/$1"
    shift
    printf '%s\n' "$@" > "$path"
    printf '%s' "$path"
}

# ---------------------------------------------------------------------------
# Task 1.2 — redaction by known value
# ---------------------------------------------------------------------------

tok=$(secret_file tok "ghp_AbCdEf0123456789")
log=$(artefact log1 "starting" "using token ghp_AbCdEf0123456789 now" "done")
scrub_redact "$log" "$tok" "github-token"
check "a known secret is removed" "0" "$(count_in 'ghp_AbCdEf0123456789' "$log")"
check "the placeholder names which secret" "1" "$(count_in '[REDACTED:github-token]' "$log")"

# Every occurrence, not just the first: a transcript repeats a token on every retry.
tok2=$(secret_file tok2 "s3cret-value")
log2=$(artefact log2 "a s3cret-value b" "c s3cret-value d" "e s3cret-value f")
scrub_redact "$log2" "$tok2" "tok"
check "every occurrence is redacted" "0" "$(count_in 's3cret-value' "$log2")"

# Regex metacharacters are matched LITERALLY. A secret is an opaque byte string; treating it as
# a pattern would both miss the real value and redact things that are not secrets.
tok3=$(secret_file tok3 'a.b*c[d]')
log3=$(artefact log3 'harmless aXbbbcd line' 'real a.b*c[d] line')
scrub_redact "$log3" "$tok3" "tok"
check "the literal secret is gone" "0" "$(count_in 'a.b*c[d]' "$log3")"
# As a REGEX, `a.b*c[d]` matches "aXbbbcd" — so a pattern-based implementation would redact a
# line containing no secret at all, and this assertion is what catches it.
check "the non-secret a regex would have eaten survives" "1" "$(count_in 'aXbbbcd' "$log3")"

# A secret file's trailing newline is a property of the FILE, not of the value.
tok4=$(secret_file tok4 "trailing-newline-value")
log4=$(artefact log4 "inline trailing-newline-value here")
scrub_redact "$log4" "$tok4" "tok"
check "the trailing newline is not part of the secret" "0" "$(count_in 'trailing-newline-value' "$log4")"

# Nothing to do is not an error: most artefacts of most runs contain no secret at all.
tok5=$(secret_file tok5 "never-appears")
log5=$(artefact log5 "clean line one" "clean line two")
before=$(cat "$log5")
scrub_redact "$log5" "$tok5" "tok"; rc=$?
check "an absent secret is not an error" "0" "$rc"
check "an absent secret leaves the artefact byte-identical" "$before" "$(cat "$log5")"

# An EMPTY secret file must be refused. An empty needle matches at every position, so a
# permissive implementation would either corrupt the artefact or silently redact nothing and
# report success — a scrub that cannot fail.
empty=$(secret_file empty "")
: > "$empty"
log6=$(artefact log6 "untouched content")
before=$(cat "$log6")
err=$(scrub_redact "$log6" "$empty" "tok" 2>&1); rc=$?
check "an empty secret file is refused" "1" "$rc"
check "a refused scrub does not touch the artefact" "$before" "$(cat "$log6")"
case "$err" in
    *empty*) check "the refusal says the secret file was empty" "yes" "yes" ;;
    *) check "the refusal says the secret file was empty" "yes" "no: ${err}" ;;
esac

# ---------------------------------------------------------------------------
# Task 1.3 — verify and refuse. The task this plan turns on.
# ---------------------------------------------------------------------------

# The control: a fully redacted artefact verifies clean.
tokA=$(secret_file tokA "value-alpha")
logA=$(artefact logA "x value-alpha y")
scrub_redact "$logA" "$tokA" "alpha"
scrub_verify "$logA" "$tokA"; rc=$?
check "a fully redacted artefact verifies clean" "0" "$rc"

# THE FIXTURE THIS PLAN TURNS ON. The redactor is told about one secret and not the other, so
# the second survives into a file the caller is about to trust. Verify must refuse. If this
# returns 0, the scrubber is a check that cannot fail, and publishing on its word would be
# worse than never publishing at all.
tokB=$(secret_file tokB "value-known")
tokC=$(secret_file tokC "value-UNTOLD")
logB=$(artefact logB "p value-known q" "r value-UNTOLD s")
scrub_redact "$logB" "$tokB" "known"
err=$(scrub_verify "$logB" "$tokB" "$tokC" 2>&1); rc=$?
check "a residual secret is REFUSED" "1" "$rc"

# The refusal is read by a human and may be pasted into a ticket, so it must describe the
# problem without reproducing the secret it just found.
case "$err" in
    *value-UNTOLD*) check "the refusal does not echo the secret it found" "clean" "it leaked the secret" ;;
    *) check "the refusal does not echo the secret it found" "clean" "clean" ;;
esac

# ---------------------------------------------------------------------------
# Task 1.4 — the pattern backstop, over the SAME engine the commit gate uses
# ---------------------------------------------------------------------------
#
# Known-value redaction covers what a run was handed. The backstop is for what it was not:
# an install identifier that reached the log by another route. It is explicitly the second
# line of defence — if it is ever the thing that saves you, the first line had a hole.

denylist=$(printf '%s' "$work/denylist")
printf 'user_login\tjdoe\nhost_name\tworkstation-7\n' > "$denylist"

# A denylisted identifier in the artefact is refused, and reported by FIELD NAME. The value is
# what we are trying to keep out of messages, so the message must not contain it.
logD=$(artefact logD "provisioning host workstation-7 now" "all done")
err=$(scrub_backstop "$logD" "$denylist" 2>&1); rc=$?
check "a denylisted identifier is refused" "1" "$rc"
case "$err" in
    *host_name*) check "the refusal names the field" "yes" "yes" ;;
    *) check "the refusal names the field" "yes" "no: ${err}" ;;
esac
case "$err" in
    *workstation-7*) check "the refusal does not echo the value" "clean" "it leaked the value" ;;
    *) check "the refusal does not echo the value" "clean" "clean" ;;
esac

# A clean artefact passes. The backstop must not be a gate that refuses everything either.
logE=$(artefact logE "provisioning a host" "all done")
scrub_backstop "$logE" "$denylist"; rc=$?
check "a clean artefact passes the backstop" "0" "$rc"

# An EMPTY denylist is refused. The underlying engine returns 0 early on one, so a wrapper
# that passed it through would be a backstop over nothing — scanning zero tokens and
# reporting clean, which is this repo's cardinal defect wearing a security control's clothes.
emptydeny=$(printf '%s' "$work/denylist-empty")
: > "$emptydeny"
err=$(scrub_backstop "$logE" "$emptydeny" 2>&1); rc=$?
check "an empty denylist is refused" "1" "$rc"

# A BINARY artefact must not hide an identifier. The engine reads its text through a command
# substitution, which drops NUL bytes — so a console log (which always has some) could carry a
# secret past a naive wrapper that piped the file straight in.
logF="$work/logF"
printf 'start\n' > "$logF"
printf 'host workstation-7 here\n' >> "$logF"
printf 'raw \xff\xfe\x00 bytes\n' >> "$logF"
err=$(scrub_backstop "$logF" "$denylist" 2>&1); rc=$?
check "a binary artefact does not hide an identifier" "1" "$rc"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]

#!/usr/bin/env bash
# Unit tests for scripts/git-hooks/lib/secret-scan.bash — the pre-commit secret scanner.
#
#   scripts/test-secret-scan.bash
#
# Run this whenever the scanner is touched. It is not wired into qa-all.bash, for the same
# reason test-planlib.bash is not: it covers a library that changes rarely and is exercised
# on every commit anyway.
#
# EVERY value here is SYNTHETIC. The real denylist is built from a gitignored file holding
# the owner's actual identifiers, and a test that hardcoded those would put them in this
# public repository — the exact leak the scanner exists to prevent.
#
# The regression that prompted this file: matching was a bare substring test, so a
# 5-character identity token matched inside an unrelated longer word already committed here,
# and every commit touching those tracked files was rejected with no way to comply.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
readonly HERE
LIB="${HERE}/git-hooks/lib/secret-scan.bash"
readonly LIB

if [[ ! -e "${LIB}" ]]; then
    printf 'FATAL: library not found: %s\n' "${LIB}" >&2
    exit 1
fi

# shellcheck source-path=SCRIPTDIR
# shellcheck source=git-hooks/lib/secret-scan.bash
source "${LIB}"

TMPROOT="$(mktemp -d)"
readonly TMPROOT
cleanup() { rm -rf "${TMPROOT}"; }
trap cleanup EXIT

PASSED=0
FAILED=0

# deny <field> <token>... — write a synthetic denylist and echo its path.
deny() {
    local out="${TMPROOT}/deny-$$-${RANDOM}.tsv"
    : >"${out}"
    while [[ "$#" -gt 0 ]]; do
        printf '%s\t%s\n' "$1" "$2" >>"${out}"
        shift 2
    done
    printf '%s' "${out}"
}

# assert_scan <label> <expected-fields> <denylist> <text>
# expected-fields is the exact stdout expected, newline separated, or "" for none.
assert_scan() {
    local label="$1" expected="$2" denylist="$3" text="$4"
    local actual
    actual="$(printf '%s' "${text}" | hook_scan_text_for_private "${denylist}")"
    if [[ "${actual}" == "${expected}" ]]; then
        printf 'PASS: %s\n' "${label}"
        PASSED=$((PASSED + 1))
    else
        printf 'FAIL: %s\n      expected [%s]\n      actual   [%s]\n' \
            "${label}" "${expected}" "${actual}"
        FAILED=$((FAILED + 1))
    fi
}

# ── the regression: a short token must not match inside a longer word ─────────────────────

DL="$(deny lastpass_accounts acme)"
assert_scan "a short token does NOT match inside a longer word" \
    "" "${DL}" "the donor of the concept is acmecorp-infra, see its plan library"
assert_scan "the same token DOES match when it stands alone" \
    "lastpass_accounts" "${DL}" "lastpass_user: acme"

# ── the shapes a leaked identifier actually takes must all still be caught ────────────────

assert_scan "caught at the start of an email local part" \
    "lastpass_accounts" "${DL}" "contact acme@example.com for access"
assert_scan "caught inside a filesystem path" \
    "lastpass_accounts" "${DL}" "state lives under /srv/deploy/acme/current"
assert_scan "caught inside a URL with credentials" \
    "lastpass_accounts" "${DL}" "https://acme:redacted@host.example.com/repo.git"
assert_scan "caught in a YAML value" \
    "lastpass_accounts" "${DL}" $'accounts:\n  - acme\n'
assert_scan "caught when quoted" \
    "lastpass_accounts" "${DL}" 'login = "acme"'

# The documented trade-off of -w, asserted so nobody discovers it by surprise: a token
# welded inside a longer word with no boundary on either side is not reported.
assert_scan "NOT caught when welded inside a longer word (the -w trade-off)" \
    "" "${DL}" "the variable is called myacmevalue here"

# ── the OTHER -w edge: a token whose first or last character is not a word character ─────
#
# `-w` requires the match to be bounded by non-word characters, or the text edge. For a
# token that itself starts or ends with punctuation, the boundary test is applied to the
# token's own edge characters — so `.internal` is only found when the character BEFORE the
# dot is also a non-word character. Measured: `mydomain.internal` does NOT match `.internal`
# under -w, though it does under a bare substring test. The denylist is built from raw
# localhost.yml values, so a token of that shape is possible. This case documents the exact
# behaviour so it is a known trade-off and not a surprise; if such a token ever appears in
# the real denylist, the matcher should anchor on [^A-Za-z0-9_] instead of relying on -w.
DL_PUNCT="$(deny private_domain '.internal')"
assert_scan "a punctuation-edged token matches when bounded by whitespace" \
    "private_domain" "${DL_PUNCT}" "suffix is .internal here"
assert_scan "a punctuation-edged token is NOT found glued to a word (documented -w edge)" \
    "" "${DL_PUNCT}" "host is mydomain.internal here"

# ── full email tokens still match, dots and all ──────────────────────────────────────────

DL_EMAIL="$(deny user_email 'someone@example.com')"
assert_scan "a full email token matches literally" \
    "user_email" "${DL_EMAIL}" "git config user.email someone@example.com"
assert_scan "a full email token is FIXED-string, not a regex" \
    "" "${DL_EMAIL}" "git config user.email someoneXexample.com"

# ── multi-field behaviour ────────────────────────────────────────────────────────────────

DL_MULTI="$(deny github_accounts acmedev user_email 'someone@example.com')"
assert_scan "two different fields are both reported, in denylist order" \
    $'github_accounts\nuser_email' "${DL_MULTI}" \
    "acmedev pushed as someone@example.com"

DL_DUP="$(deny github_accounts acmedev github_accounts acmeops)"
assert_scan "a field matching twice is reported ONCE" \
    "github_accounts" "${DL_DUP}" "both acmedev and acmeops appear here"

# ── degenerate inputs must not produce a false clean pass ────────────────────────────────

EMPTY_DL="${TMPROOT}/empty.tsv"
: >"${EMPTY_DL}"
assert_scan "an empty denylist reports nothing" "" "${EMPTY_DL}" "acme is everywhere"
assert_scan "empty text reports nothing" "" "${DL}" ""

# ── the real hook must actually use the word-boundary matcher ────────────────────────────
#
# A structural assertion, because the behaviour above is only reached through this call and
# a future edit could silently drop the flag.
if grep -q 'grep -qwF' "${LIB}"; then
    printf 'PASS: the matcher uses word-boundary fixed-string matching\n'
    PASSED=$((PASSED + 1))
else
    printf 'FAIL: the matcher no longer uses grep -qwF — the regression above can return\n'
    FAILED=$((FAILED + 1))
fi

printf '\npassed: %d   failed: %d\n' "${PASSED}" "${FAILED}"
if [[ "${FAILED}" -gt 0 ]]; then
    printf 'test-secret-scan: FAILED\n'
    exit 1
fi
printf 'test-secret-scan: PASSED\n'

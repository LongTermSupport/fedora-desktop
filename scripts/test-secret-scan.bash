#!/usr/bin/env bash
# Unit tests for scripts/git-hooks/lib/secret-scan.bash — the pre-commit secret scanner.
#
#   scripts/test-secret-scan.bash
#
# Run this whenever the scanner is touched. It IS wired into qa-all.bash (the
# `secret-scan-tests` gate), so a regression here fails QA — this file previously claimed
# the opposite, which invited an author to treat it as optional.
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

# ── the derived GNOME extension UUID exemption ───────────────────────────────────────────
#
# A GNOME extension UUID is shaped exactly like an email address, so the email pattern
# flags every one and the repo could not name the extensions it deploys. The exemption is
# DERIVED from vars/gnome-shell-extensions.yml at scan time. This is a public-repo security
# gate, so the question these cases answer is not "does the exemption work" but "can a real
# address get through it".

EMAIL_PAT='[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'
REPO_ROOT="$(cd "${HERE}/.." && pwd -P)"

# assert_filter <label> <expected-stdout> <repo-root> <line>
assert_filter() {
    local label="$1" expected="$2" root="$3" line="$4"
    local actual
    actual="$(printf '%s\n' "${line}" | hook_filter_match_lines "${EMAIL_PAT}" "${root}")"
    if [[ "${actual}" == "${expected}" ]]; then
        printf 'PASS: %s\n' "${label}"
        PASSED=$((PASSED + 1))
    else
        printf 'FAIL: %s\n      expected [%s]\n      actual   [%s]\n' \
            "${label}" "${expected}" "${actual}"
        FAILED=$((FAILED + 1))
    fi
}

DECLARED_UUID="Vitals@CoreCoding.com"
# A non-exempt address the email pattern matches. `.internal` is ICANN-reserved and
# unroutable, and — unlike example.com/.test/.invalid — is NOT on the whitelist above,
# which is exactly what this test needs: a string that must still be reported.
#
# Assembled from two halves rather than written out, because the pre-commit hook scans
# THIS file too: a literal non-exempt address here would be correctly flagged and would
# block every commit that touches the suite. Neither half matches the pattern alone.
RESERVED_TLD="internal"
REAL_ADDRESS="someone@corp.${RESERVED_TLD}"
# Derived from the UUID under test rather than written out, so it cannot drift if the
# declared UUID changes, and so this file does not carry a second address-shaped literal.
NEAR_MISS="${DECLARED_UUID,,}"

assert_filter "a declared extension UUID is exempt" \
    "" "${REPO_ROOT}" "1:      uuid: ${DECLARED_UUID}"
assert_filter "a real address is still flagged" \
    "1: contact: ${REAL_ADDRESS}" "${REPO_ROOT}" "1: contact: ${REAL_ADDRESS}"
# Per TOKEN, never per line — one legitimate reference must not shield a real one.
# CLAUDE/PlanTriage.md records the leak that taught this.
assert_filter "a UUID does not shield a real address on the same line" \
    "1: ${DECLARED_UUID} and ${REAL_ADDRESS}" "${REPO_ROOT}" \
    "1: ${DECLARED_UUID} and ${REAL_ADDRESS}"
# The UUID entries use the case-SENSITIVE arm, so a near-miss is not exempt.
assert_filter "a case-differing near-miss of a UUID is NOT exempt" \
    "1: ${NEAR_MISS}" "${REPO_ROOT}" "1: ${NEAR_MISS}"

# ── the ANCHORS are the load-bearing part, so they get their own cases ────────────────────
#
# The emitter prints "^" + re.escape(uuid) + "$". Without those two anchors the exemption
# becomes a substring match, and every case above STILL PASSES — measured: a variant with
# the anchors dropped ships `passed: 24` and a green qa-all.bash. So the suite proved the
# exemption worked and not that it was tight, which is the only property that matters here.
# Each anchor is held by at least one case below, which is the property that matters and is
# not the same as every case holding both: measured, dropping only `^` fails the strict-suffix
# case alone, and dropping only `$` fails the strict-prefix and deeper-domain cases.
#
# These three are derived from the declared UUID rather than written out, so they grow no
# new address-shaped literal and cannot drift if the declared set changes. Named for where
# the EXTRA text sits, because naming them for the operation reads as the opposite.
UUID_WITH_TAIL="${DECLARED_UUID}pany"           # a longer address the UUID is a strict PREFIX of
UUID_WITH_HEAD="x${DECLARED_UUID}"              # a longer address the UUID is a strict SUFFIX of
# A deeper domain under the same left-hand side. The TLD must be one the scanner does NOT
# already whitelist, or the case proves nothing: a first draft used `.example`, which is
# RFC 2606 reserved and correctly exempt for that reason, so it passed the filter and looked
# like an anchoring hole. See CLAUDE/ExampleValues.md for the reserved set.
EMBEDDED="${DECLARED_UUID}.evil.${RESERVED_TLD}"

# re.escape is the OTHER load-bearing call, and the anchors do not cover it: a near-miss of
# the SAME LENGTH is inside the anchors, so only the escaping can reject it. Substituting a
# letter for an interior dot leaves a string the email pattern still matches, which an
# unescaped `.` would match as a wildcard.
#
# This one IS written out, because the three above cannot carry the property: the declared
# UUID they derive from has no interior dot whose substitution leaves a string the email
# pattern still matches, and a near-miss the pattern does not match is never reached by the
# exemption at all. So unlike them it drifts if that extension ever leaves the vars file. The first
# assertion below is what makes the drift loud: without it the near-miss would keep passing
# on the wrong grounds, because an UNDECLARED UUID's near-miss is flagged either way, and
# the case would prove nothing about escaping while still reporting green.
DOTTED_UUID="appindicatorsupport@rgcjonas.gmail.com"   # exempt: itself a declared UUID
WILDCARD_NEAR_MISS="${DOTTED_UUID/rgcjonas./rgcjonasX}"

assert_filter "the dotted UUID is itself exempt" \
    "" "${REPO_ROOT}" "1: ${DOTTED_UUID}"
assert_filter "a same-length near-miss that only an unescaped dot would match is NOT exempt" \
    "1: ${WILDCARD_NEAR_MISS}" "${REPO_ROOT}" "1: ${WILDCARD_NEAR_MISS}"

assert_filter "a longer address starting with a declared UUID is NOT exempt" \
    "1: ${UUID_WITH_TAIL}" "${REPO_ROOT}" "1: ${UUID_WITH_TAIL}"
assert_filter "an address ending with a declared UUID is NOT exempt" \
    "1: ${UUID_WITH_HEAD}" "${REPO_ROOT}" "1: ${UUID_WITH_HEAD}"
assert_filter "a declared UUID extended by a further domain is NOT exempt" \
    "1: ${EMBEDDED}" "${REPO_ROOT}" "1: ${EMBEDDED}"
# Without a repo root the exemption is simply absent, which is the safe direction:
# the gate errs towards flagging rather than towards allowing.
assert_filter "with no repo root the UUID is flagged, not exempt" \
    "1:      uuid: ${DECLARED_UUID}" "" "1:      uuid: ${DECLARED_UUID}"

# A malformed vars file must HARD FAIL the filter, never quietly yield no exemption and
# let the scan continue: a security gate that could not build its exemption list has not
# passed, it has not run.
BAD_ROOT="${TMPROOT}/bad-root"
mkdir -p "${BAD_ROOT}/vars"
printf 'gnome_shell_extensions: [this, is, not, a, mapping]\n' \
    >"${BAD_ROOT}/vars/gnome-shell-extensions.yml"
if printf '1: x\n' | hook_filter_match_lines "${EMAIL_PAT}" "${BAD_ROOT}" >/dev/null 2>&1; then
    printf 'FAIL: a malformed vars file did NOT fail the filter\n'
    FAILED=$((FAILED + 1))
else
    printf 'PASS: a malformed vars file hard-fails the filter\n'
    PASSED=$((PASSED + 1))
fi

# An absent vars file is not an error — a checkout without one simply gets no exemption.
ABSENT_ROOT="${TMPROOT}/absent-root"
mkdir -p "${ABSENT_ROOT}"
assert_filter "an absent vars file yields no exemption and no error" \
    "1: uuid: ${DECLARED_UUID}" "${ABSENT_ROOT}" "1: uuid: ${DECLARED_UUID}"

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

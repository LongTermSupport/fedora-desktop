# Gate fixes: qa-changed name matching, and semgrep parsing of the launcher family

## Defect A: qa-changed (commit f8611aba)

- `namesFile` (src-go/qa/changed/suites.go) matched the base name as a raw substring. It now
  matches it as a token: the bytes either side may not be `[A-Za-z0-9_-]`. A full relative path
  is still matched as a substring. Tests, written first and seen red (five word-internal cases
  matched): `TestNamesFileMatchesTheBasenameAsATokenNotInsideAWord`, plus a real-tree test that
  `qa-nokill-containerwatch`, `qa-vmtest-manifest` and `qa-version-pins` are not selected by a
  `cc` change. A real reference (`run cc now`, `/usr/local/bin/cc`, `"cc"`, `cc.`) still matches.
- Second layer: on this base `judgeCount` no longer exists. `judgeSuite` already judges each suite
  by its gate rows' own readers (`detail_suite_test.go` pins a `detail:` suite, and one whose
  reader finds nothing). The one remaining place that demanded a `passed:` line from every
  `test-*` suite was `TestEverySelectableSuitePrintsTheCanonicalCountLine` in `realtree_test.go`;
  it now looks only at suites with a `count` reader row. That test passed before and after, so
  this part has no red step.
- `scripts/qa-gates.tsv` and `scripts/qa-routing.tsv` need no change.

## Defect B: semgrep and the launcher

The line-17 range is how semgrep reports a failed parse (from the last good node to the end of
the file); line 17 itself is fine. The comment on the `CCY_VERSION` line is not the cause and
`CCY_VERSION="x.y.z"` stays greppable. Found by bisecting complete-statement chunks. Constructs
tree-sitter-bash cannot parse, and the equivalent form used:

| construct                                  | replacement                                                   |
| ------------------------------------------ | ------------------------------------------------------------- |
| `exec <&"$FD" {FD}<&-`                     | `exec <&"$FD"; exec {FD}<&-`                                  |
| `${v:-(text)}`, `${v:+ (text)}`            | `${v:-"(text)"}` (quotes nest in the word and vanish)         |
| `$(( (a - b) / n ))`, `$(( (a + b) % n ))` | `$(( $(( a - b )) / n ))`                                     |
| `if ! { a \| b > f; }; then`               | `if ! a \| b > f; then`                                       |
| `if ! cat > f <<'EOF'; then ... fi`        | `cat > f <<'EOF' \|\| { print_error ...; rm ...; return 1; }` |

Fixed: claude-yolo, lib/common.bash, network-management.bash, token-management.bash (the three
asked for), and also docker-health.bash, restart-request.bash, session-registry.bash,
ssh-handling.bash. Semgrep now reads all of them in full.

Still partial: `lib/session-lifecycle.bash` (hard syntax error for the whole file). Cause:
`$(( 10#${days:-0} * 86400 + ... ))` in `ccy_lifecycle_parse_duration`. Any `#` in `$(( ))`
fails, so the base prefix cannot be written; it needs a leading-zero strip (the base prefix is
what stops `08h` being read as octal), which is a design choice, so it was left.

CCY 3.98.3 and a changelog entry. The bash-limits ratchet holds: the rewrites were kept to the
same line and statement counts (a `printf` merging two adjacent `echo` lines pays for the extra
`exec`), and `scripts/bash-limits.allowlist` was tightened for three files that shrank.
`scripts/test-ccy-restart-request.bash` looked for the old one-line `exec`; it now looks for the
dup half. qa-changed passes.

## The parse ratchet (built in the follow-up commit)

`src-go/qa/patterns/ratchet.go`: a shell file with more than half of its lines outside the parse
tree fails the patterns gate unless it is on `scripts/patterns-partial.allowlist` (rows: path,
unparsed lines, the construct). A listed file above its record fails (grew), below it fails
(lower the record), and no longer mostly unparsed fails (delete the row). A missing or malformed
allowlist is exit 2. There is no `--update` and no merge-base comparison, unlike bash-limits: a
record is lowered by hand, and a record raised by hand is not caught. Tests in `ratchet_test.go`
were seen red before the code. SPEC-qa-patterns.md P14 says so.

Findings when the real tree was first scanned: `files/home/.local/bin/ftp-camera` was 1973 of
2569 lines unparsed because of `-FileSize#` (a `#` inside a word in `$( )` starts a comment for
the grammar); quoting it as `"-FileSize#"` fixed it. The one listed file is the completed plan's
`reproduce-pipx-uv.bash` (348 lines, a heredoc in an `if` condition).

Correction to the section above: `lib/session-lifecycle.bash` is not unparsed under the gate. It
failed only under a `--lang bash --pattern` probe; the gate's own scan of it, with the rules,
reports no parse error, so it is not on the allowlist (a row for it would fail as stale).

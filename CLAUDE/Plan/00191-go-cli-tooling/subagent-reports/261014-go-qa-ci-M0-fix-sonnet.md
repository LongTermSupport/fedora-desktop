# go-qa-ci M0 fix report

Fixed verifier problems 1-11 in scripts/public-safety-scan.sh, tests/scripts/public_safety_test.go,
tests/scripts/secret_scan_test.go and README.md.

- Tree scan reads git blobs (ls-files -s + cat-file): binary files and symlink target strings are scanned, nothing outside the repo is read. One locations file; a failed lookup exits 2.
- Denylist: CR and surrounding whitespace stripped, one predicate for comment/blank, all patterns validated up front (exit 2 naming the line), file itself realpath-resolved for the outside-repo check.
- Path-leak guard tests the file name against every pattern. grep -a everywhere; --history uses --text -m; --staged uses --text.
- Tests added: multi-file location (non-last file, beyond last file's line count, middle), cross-line path leak, binary tree/history, symlink target, symlink to outside file not read, NUL stdin, CRLF denylist, symlinked denylist file, tab/indented-comment lines; secret-scan tests with a stub gitleaks (missing tool exit 2, suppression-file refusal, modes and flags, default mode, findings propagate, unknown mode).
- README: dropped the claim of a gap record; none exists yet. No conformance claimed.
- Owner action outstanding: set repository secret GQACI_DENYLIST. The workflow fails loudly without it, so the "green with the secret set" acceptance clause cannot be proven until then.
- Deviation: --tree scans the index (what git publishes), so unstaged working edits are not scanned; --staged covers pre-commit.

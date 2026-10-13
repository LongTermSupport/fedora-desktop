# go-qa-ci M0 fix round: scanner hardening

Built: three fixes in the public-safety and secret scanners (keeping the scans faithful; no conformance claim).

1. public-safety-scan.sh --tree refuses (exit 2) a tracked name containing a newline, so the newline-delimited maps cannot drift.
2. --staged uses core.quotePath=false so non-ASCII staged names are matched literally.
3. secret-scan.sh --tree writes git blobs (ls-files -s -z, cat-file) into the scan directory; symlinks become their target string, nothing outside the repository is read; submodule pins skipped.

Tests added: newline-name refusal, non-ASCII staged name, symlink-to-outside-file not read (stub gitleaks now records scanned file contents).
Deviations: none; the "refuse" option was chosen for newline names.

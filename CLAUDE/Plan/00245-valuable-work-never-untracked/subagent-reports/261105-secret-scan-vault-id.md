# Secret scan: `--vault-id label@source` is not an email

## Class and fix

Class: a scanner pattern that matches a non-secret syntax. The email pattern flags Ansible's
`--vault-id <label>@<source>` argument whenever the source ends in a dotted suffix that is not
on the example-domain whitelist.

Fix, in the shared filter `hook_keep_unwhitelisted` in `scripts/git-hooks/lib/secret-scan.bash`:
the existing masking awk gains one `gsub` that replaces `--vault-id X` and `--vault-id=X` with a
placeholder before tokens are extracted. `pre-commit` and `commit-msg` both call
`hook_filter_match_lines`, so both are fixed by the one edit. The mask is per token: a real
address elsewhere on the line is still reported. The `gsub` is folded onto the existing awk line
because the file is on the bash-limits ratchet (a separate line grew it 225 to 226 and failed
the gate; as it stands the record is unchanged).

## Red / green

New cases in `scripts/test-secret-scan.bash` (a made-up `prod` label with a `vault-pass` file source, built from two halves because the suite is scanned too):

- `--vault-id X`, `--vault-id=X`: exempt. Red before the fix (`passed: 33 failed: 2`).
- `--vault-id X` plus a real address on the line: address still flagged.
- same `label@source` without the flag: still flagged.
- `--vault-idx <real address>`: still flagged.

After the fix: `passed: 35 failed: 0`. The two lines of
`CLAUDE/Plan/Completed/002-nordvpn-openvpn-manager/PLAN.md` that blocked the commit now yield
zero findings through the filter.

## Sweep (other non-email `x@y.z` tokens in tracked files, not fixed)

- GNOME extension UUIDs (`Vitals@...`, `clipboard-indicator@...`, `dash-to-dock@...`,
  `appindicatorsupport@...`, `tilingshell@...`): handled by the derived UUID allowlist, which
  depends on `vars/gnome-shell-extensions.yml` declaring each one.
- `Co-Authored-By: ... <noreply@anthropic.com>`: passes only because the `<...>` bracket mask
  hides it; unbracketed it would be flagged.
- a GitHub users-noreply address (one old `.claude/plan` doc): not whitelisted; the file
  trips the scanner whenever touched.
- Vendored upstream files under `src-go/vendor` (a charm.land address, maintainer addresses in
  MAINTAINERS and SECURITY.md) and a few old plan/research files with upstream author
  addresses: would trip if touched; the scan list presumably excludes vendor, not verified.
- Ansible `@` forms handled by luck of the systemd-unit and `git@` exemptions only.

## Tooling faults met

- The pipe blocker rejects `qa-changed.bash | tail`; the hook-suggested whitelist entry was not
  applied (redirected to a log file instead).
- `git grep -l` with a broad alternation matched 80 files; the vault-id-only sweep needs a
  narrower anchor.

## QA

`./scripts/qa-changed.bash`: passed (3 files; test-secret-scan suite 35 green; bash-limits green).

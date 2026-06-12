# Phase 10 — Follow-Up Research (coverage gaps GAP-01..06)

> Defensive audit of the areas the main 25-agent sweep under-covered. Read-only
> research (three parallel subagents); each finding carries file:line evidence.
> Identifiers are not reproduced here (public-repo rule); hostnames/usernames are
> referred to by role.

## Summary

The follow-up surfaced **one confirmed live defect** (a project hook regex that
silently disabled `pip install` blocking — fixed in this batch) plus a cluster of
**install-bootstrap and entry-point hardening** items in `fedora-install/` and
`run.bash`. The bootstrap items are real but high-blast-radius and untestable in the
CCY container (they wipe disks / set up LUKS / post to the public tracker), so they
are documented here with concrete fixes and recommended as a **dedicated host-tested
follow-up batch**, not blind-edited from the container.

---

## GAP-04 — Tracked `.claude/` custom code

### FUP-01: `ansible_enforcement` pip-block regex was inverted — FIXED in this batch

**Severity: high · `.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py:41`**

The pattern was `\bpip3?\s+install(?!\s)`. The `(?!\s)` negative lookahead means
"`install` NOT followed by whitespace", so every real `pip install <pkg>` (which has a
space after `install`) **failed to match and was allowed**, while `pip installer-thing`
was wrongly blocked. Verified empirically:

| command                   | buggy `(?!\s)` | fixed `\b`    |
| ------------------------- | -------------- | ------------- |
| `pip install numpy`       | not blocked ✗  | blocked ✓     |
| `pip3 install --user pkg` | not blocked ✗  | blocked ✓     |
| `pip installer-thing`     | blocked ✗      | not blocked ✓ |
| `pip install` (bare)      | blocked        | blocked       |

Two tests (`test_matches_pip_install_global`, `test_matches_pip3_install_global`)
already assert the correct behaviour — they were failing but never run (the pytest
suite is absent from the CCY image, the QA-09 gap). The comment "(but not in venv
context)" reveals the intent, but a regex on the command string cannot detect venv
activation; the lookahead never achieved that.

**Fix applied:** `\bpip3?\s+install\b`. The two pre-existing tests now pass (verified by
direct regex evaluation; pytest still cannot run here). Note `sudo pip` / `--break-system-packages`
remain independently caught by the daemon's `sudo_pip` / `pip_break_system` handlers — the
gap was specifically a plain system `pip install <pkg>`.

> **Host action:** restart the hooks daemon (via the `hooks-daemon` skill) so the
> corrected handler loads, and run the daemon's pytest suite once pytest is available
> (QA-09) to lock the two tests in.

### FUP-02: `system_paths` project-root via hardcoded `parents[4]`

**Severity: medium · `.claude/hooks/handlers/pre_tool_use/system_paths.py:69-71`**

`project_dir = Path(__file__).resolve().parents[4]` hard-codes the handler's depth. If
the daemon restructures handler discovery or the file moves, the project-root exemption
silently points at the wrong directory (over- or under-permitting system-path edits).
**Fix:** assert the derived root contains a sentinel (`.claude/settings.json` or
`CLAUDE.md`) so a wrong path fails loudly.

### FUP-03: `system_paths` `/.claude/projects/` exemption uses substring match

**Severity: low · `system_paths.py:78-80`** — `if "/.claude/projects/" in file_path`
matches the substring anywhere. Tighten to a `startswith` on the known roots
(`/root/.claude/projects/`) or a resolved-path prefix check.

### FUP-04: `.claude/ccy/Dockerfile` unpinned dependencies

**Severity: medium · `.claude/ccy/Dockerfile:37,50,59,75`** — `yq` `latest`, ImageMagick
`magick` (no version), `jmespath` unpinned, galaxy collections unpinned. Mirrors SEC-05
and GAP-05. `--break-system-packages` (line 37) is acceptable in an isolated image layer
but should carry a one-line comment explaining why (the project's `pip_break_system` hook
otherwise flags the pattern to any reader).

### Positives (GAP-04)

- Hook wiring is correct: all events delegate through the daemon wrapper; zero `hooks`
  entries in `settings.local.json`; no inline/bespoke commands.
- The two repo-owned handlers use only `re`/`pathlib`/daemon abstractions — no secrets,
  no `eval`/`exec`/`subprocess`/network.
- The `systemctl (?!--user)` exemption is correct (user services allowed, system blocked).

---

## GAP-05 — Galaxy dependency pinning

### FUP-05: `lts.vault-scripts` role pinned to mutable `master`

**Severity: high · `requirements.yml:5`** — `version: master`. `roles/vendor/*` is
gitignored (`.gitignore:6`) and `ansible.cfg:7` targets `roles_path = ./roles/vendor`,
so **nothing is actually vendored** despite the name — every fresh install pulls live
`master`. This role performs vault-password operations during bootstrap (decrypts
`localhost.yml`), making it a genuine supply-chain trust point: a compromised/broken
`master` push silently changes behaviour on the next install.
**Fix (priority order):** (1) pin to an immutable commit SHA; (2) pin to a semver tag;
(3) genuinely vendor (un-gitignore + commit, with a CI drift check). Needs a network
lookup of valid upstream refs — left as a recommendation rather than guessed.

### FUP-06: collections unversioned

**Severity: medium · `requirements.yml:8-9`** — `community.general` and `ansible.posix`
have no `version:`. `community.general` ships 100+ modules with frequent
breaking-change releases; this repo uses its `dconf`/`flatpak*` modules (added in Phase
8\) and `ansible.posix.firewalld`. **Fix:** add `version: ">=X,<Y"` bounds (exact bounds
to be set from the current known-good install versions).

---

## GAP-06 — Licensing

### FUP-07: GPL-2.0 file tracked in an MIT repo

**Severity: medium · `files/usr/bin/gnome-shell-extension-installer:7-8`** — a 604-line
third-party script "Licensed under the GNU General Public License 2.0", deployed verbatim
by `play-gnome-shell-extensions.yml:12`. Repo `LICENSE` is MIT. MIT and GPL-2.0 are not
re-licence-compatible; the implicit "whole repo is MIT" claim is incorrect for this file.
**Fix (preferred):** replace the tracked file with a `get_url` task that fetches the
installer from upstream at a pinned tag/SHA at deploy time — removes the GPL file from the
repo entirely and also de-stales it. Alternatives: add a root `THIRD-PARTY-LICENSES.md`
acknowledging the GPL-2.0 file, or drop it. No other GPL/LGPL/AGPL content found in
tracked project files.

---

## GAP-01 — `fedora-install/` bootstrap (~2,770 lines)

Files: `ks.cfg`, `setup-netinstall-boot.bash`, `build-iso.bash`, `pull-projects.bash`,
`push.bash`. **Strong baseline** (`read -s`, `printf '%s'` not `echo`, `openssl passwd -6`,
`rootpw --lock`, SELinux enforcing, firewall on, `set -euxo pipefail` + `--erroronfail` in
`%post`, `%q`-quoted `install-vars`, two-layer GRUB+auth-code protection, `attempt()`
instead of `|| true`, download size + magic-byte checks). Findings:

| ID     | Sev  | File:line                            | Issue                                                                                                                                                         | Fix                                                                                             |
| ------ | ---- | ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| FUP-08 | high | `ks.cfg:349-350,367-368`             | LUKS passphrase embedded plaintext in `/tmp/part-include`, never removed; persists through install                                                            | `rm -f /tmp/part-include` (+ other tmp includes) as first line of `%post --nochroot`            |
| FUP-09 | high | `ks.cfg:310,375`                     | LUKS passphrase written to an unnecessary `/tmp/ks-luks-pass` intermediate (ramfs, all-process-visible)                                                       | drop the file; use `$KS_LUKS1` directly                                                         |
| FUP-10 | high | `ks.cfg:396,447,542-545`             | WiFi password persisted to the installed system in `install-vars` (and copied to user home) though only needed for the NM profile written in the same `%post` | remove `KS_WIFI_PASS` from `write_var`                                                          |
| FUP-11 | med  | `setup-netinstall-boot.bash:645-684` | downloaded ISOs verified by size + `file` magic only — no CHECKSUM/GPG                                                                                        | fetch `-CHECKSUM`, `gpg --verify` (Fedora keys in `distribution-gpg-keys`), then `sha256sum -c` |
| FUP-12 | med  | `ks.cfg:15`                          | `%pre` runs without `set -uo pipefail` (security-critical collection code)                                                                                    | add `set -uo pipefail` (keep `-e` off for the interactive read loops)                           |
| FUP-13 | med  | `ks.cfg:569`                         | reinstall path `git reset --hard origin/<branch>` silently destroys local changes                                                                             | check `git status --porcelain` / back up, or `git merge --ff-only`                              |
| FUP-14 | med  | `pull-projects.bash:212`             | `StrictHostKeyChecking=no` on first-boot clones (SSH MITM window)                                                                                             | `accept-new` + pre-seed GitHub `known_hosts`                                                    |
| FUP-15 | med  | `build-iso.bash:47-49`               | silently `dnf -y install lorax` as a side effect                                                                                                              | `die` with the install instruction instead (matches how missing `gh`/`git` are handled)         |
| FUP-16 | low  | `ks.cfg:438,459`                     | `set -x` in `%post` logs SSID + username to root-owned logs                                                                                                   | drop `-x`, use explicit `echo` step markers                                                     |
| FUP-17 | low  | `ks.cfg:216-219`                     | invalid hostname silently replaced with default (username loops; hostname doesn't)                                                                            | loop like username                                                                              |
| FUP-18 | low  | `push.bash:304-354`                  | in-place secret re-encryption of `localhost.yml` with no backup / non-atomic                                                                                  | temp file + verify + atomic `mv`                                                                |

---

## GAP-02 — `run.bash` (1,473 lines), primary entry point

**Strong baseline** (`set -euo pipefail` + `IFS`, EXIT trap clears the passphrase temp
file, `read -rsp` for all secrets, `chmod 600` on vault writes, OAuth scope-hierarchy
modelling, Contents-API config sync rather than credential-bearing remotes). Findings
cluster on the **failure-report flow** and **config-repo privacy**:

| ID     | Sev  | File:line                 | Issue                                                                                                                                                                                      | Fix                                                                                                          |
| ------ | ---- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| FUP-19 | high | `run.bash:382,432`        | machine **hostname** embedded verbatim in the public GitHub issue body (a "Never Commit" item)                                                                                             | drop it or replace with `Fedora <ver>`; it adds no diagnostic value                                          |
| FUP-20 | high | `run.bash:404,416`        | Claude sanitiser `… 2>/dev/null \|\| echo ""` silently degrades to the weaker regex-only path on any failure (the banned error-hiding pattern); regex misses git remote URLs / bare tokens | **fail closed** — abort the post if sanitisation was expected but returned empty; broaden the regex fallback |
| FUP-21 | high | `run.bash:470`            | issue-body preview truncated to 50 lines; the pasted error log beyond that is posted unseen before the `confirm`                                                                           | show the full body (or write to a temp file for review) before confirming                                    |
| FUP-22 | high | `run.bash:841,970`        | config-repo existence checked but **not** `.private`; pushes `localhost.yml` (PII + vault) to `<user>/fedora-desktop-config` which could be public                                         | `gh api repos/<repo> --jq .private` and fail/double-confirm if `false` (before both pull and push)           |
| FUP-23 | med  | `run.bash:653`            | `gh-cli.repo` repofile fetched over net and added as a root DNF trust with no GPG-fingerprint check                                                                                        | pin/verify the GPG key fingerprint, or vendor the `.repo`                                                    |
| FUP-24 | med  | `run.bash:788`            | GitHub SSH host keys fetched live from `api.github.com/meta` and appended to `known_hosts` unverified; `-s` hides curl errors (partial append)                                             | `--fail`, compare against pinned GitHub fingerprints                                                         |
| FUP-25 | med  | `run.bash:1108`           | `ansible-galaxy install … >/dev/null 2>&1` — full output suppressed (silent supply-chain install per GAP-05)                                                                               | drop the suppression so failures/installs are visible                                                        |
| FUP-26 | med  | `run.bash:1012-1091`      | `vaultPass` / `_github_ssh_passphrase` never `unset` after last use (live through playbook runs)                                                                                           | `unset` immediately after last use                                                                           |
| FUP-27 | low  | `run.bash:768`            | hostname embedded in the uploaded GitHub SSH-key title                                                                                                                                     | generic label                                                                                                |
| FUP-28 | low  | `run.bash:412`            | `VERBOSE` referenced without `${VERBOSE:-}` under `set -u`                                                                                                                                 | add default                                                                                                  |
| FUP-29 | low  | `run.bash:1014,1023,1040` | `echo "$vaultPass" >` adds a trailing newline (use `printf '%s'`)                                                                                                                          | `printf '%s'`                                                                                                |

---

## Recommended sequencing

1. **FUP-01 (done this batch)** — the only confirmed live defect; fixed + verified.
2. **Quick, unambiguous wins** (good-vs-bad, low risk): FUP-19/FUP-27 (drop hostname from
   public posts), FUP-25/FUP-28/FUP-29, FUP-04/FUP-02/FUP-03. Most are 1–3 line edits.
3. **Privacy/fail-closed in `run.bash`** (FUP-20/21/22) — behaviour changes to the
   issue-reporting + config-sync UX; worth doing but the user should confirm the new
   flow (e.g. fail-closed sanitisation, full-body preview, private-repo gate).
4. **`fedora-install/` secret-lifecycle** (FUP-08/09/10) — high value, but edits to
   disk-wiping install media that **cannot be tested in the CCY container**; do as a
   host-tested batch with a real netinstall run.
5. **Supply-chain pinning** (FUP-05/06/11/23/24, FUP-07 licensing) — needs network
   lookups for valid refs/versions; batch together.

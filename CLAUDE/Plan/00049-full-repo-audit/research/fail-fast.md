# Fail-Fast & Error-Hiding Audit

## Scope & Method

This audit covers the project's #1 hard rule — **fail fast, no silent failures, no error hiding** — across the whole repository (branch `F43`), excluding `.git/`, `node_modules/`, `untracked/`, `.claude/hooks-daemon/` and `roles/vendor`.

Method (systematic, not sample-based):

1. Enumerated every bash/sh file by extension and by shebang (`rg -l '^#!.*\b(bash|sh)\b'`) across `scripts/`, `files/`, `fedora-install/`, `extensions/scripts/`, plus `files/home/.local/bin/` and `files/usr/local/bin/` deployed executables, and checked each for `set -e`/`set -euo pipefail` (head-of-file and whole-file).
2. Swept every `*.yml` under `playbooks/`, `tasks/`, `tests/`, `environment/` for `ignore_errors`, `failed_when: false`, and for error-hiding constructs (`|| true`, `|| echo`, `2>/dev/null`) inside `shell:` blocks.
3. Counted all multi-line `ansible.builtin.shell: |` blocks per playbook and compared against `set -e` usage inside them.
4. Swept all Python files (`*.py` plus shebang-detected scripts in `files/home/.local/bin/`) for broad/bare `except` and `except ...: pass`.
5. Swept GNOME extension JavaScript for silent `catch` blocks.
6. Read every suspicious hit in context to distinguish legitimate probe-then-fail patterns (registered result explicitly checked) and annotated `# FAIL-FAST-OK:` instances from genuine violations.

Files read in full or in relevant context include: `scripts/qa-all.bash`, `scripts/qa-bash.bash`, `scripts/qa-python.bash`, `scripts/qa-patterns.bash`, `scripts/qa-ansible.bash`, `scripts/qa-ctrl-z-patch.bash`, `.semgrep/bash-conventions.yml`, `files/var/local/claude-yolo/entrypoint.sh`, `files/var/local/claude-yolo/lib/*.bash`, `files/var/local/docker-in-lxc`, `files/usr/local/bin/shutdown-with-update`, `files/usr/local/bin/gh-print-auth-url`, `scripts/nvidia-status.bash`, `scripts/check-displaylink-status.sh`, `scripts/test-ccy-ssh-probe.bash`, `fedora-install/setup-netinstall-boot.bash`, and the playbooks cited below.

## Summary

The Ansible layer is in **very good shape**: every `failed_when: false` / `ignore_errors: true` in `playbooks/` carries a `# FAIL-FAST-OK:` annotation (60+ instances verified, all of them genuine probe-then-fail or documented best-effort cases), and `scripts/qa-ansible.bash` enforces this. The CCY wrapper, its libraries, and the QA harness are largely disciplined, with probe results explicitly checked.

The single largest gap is **systemic, not annotated, and invisible to the current QA tooling**: multi-line `ansible.builtin.shell: |` blocks without `set -e`/`set -o pipefail`. The shell module only propagates the **last** command's exit status, so intermediate failures in roughly 60 of 75 such blocks are silently swallowed — including `curl | bash` installers where a network failure produces a *successful* task. Secondary findings concern the QA gate itself (shellcheck silently optional and advisory-only), a textbook "skip and warn" in the JetBrains Toolbox playbook, and warn-and-continue patterns in `docker-in-lxc` provisioning.

| ID    | Severity | Title                                                                             |
| ----- | -------- | --------------------------------------------------------------------------------- |
| FF-01 | high     | ~60 multi-line `shell: \|` blocks lack `set -e`/`pipefail` — intermediate failures silently swallowed |
| FF-02 | medium   | play-toolbox-install.yml uses prohibited "skip and warn" pattern                   |
| FF-03 | medium   | qa-bash.bash: shellcheck silently skipped if absent, advisory-only, crash-masked   |
| FF-04 | medium   | qa-python.bash: ruff crash indistinguishable from "no findings" — silently passes  |
| FF-05 | medium   | docker-in-lxc: warn-and-continue verification and `npm update … \|\| true`         |
| FF-06 | low      | CCY entrypoint: GitHub known_hosts fetch fails completely silently                 |
| FF-07 | low      | shutdown-with-update: no `set -e`; firmware refresh failure warn-and-continue      |
| FF-08 | low      | Diagnostic scripts lack strict mode and any design annotation                      |
| FF-09 | low      | wsi-* Python family: ~40 `except Exception: pass`, some on data-path reads         |
| FF-10 | low      | QA tooling pattern gaps (`\|\| true` uncovered; qa-ansible regex/scope gaps)       |
| FF-11 | info     | ssh-handling.bash: token-owner cross-check silently skipped when GitHub API unreachable |

---

## FF-01: Multi-line `shell: |` blocks without `set -e` — intermediate failures silently swallowed

**Severity: high** | **Area: playbooks** | **Effort: medium**

### Evidence

There are **75** multi-line `ansible.builtin.shell: |` blocks across 28 playbooks; only ~12 of them contain `set -e` (or `set -eo pipefail`). Ansible's `shell` module reports the task as failed **only if the final command exits non-zero**, so every earlier command in an unguarded block can fail without stopping the play — a direct violation of "NEVER decouple dependent operations".

Concrete cases verified by reading each block:

- `playbooks/imports/play-lxc-install-config.yml:73-75`:
  ```yaml
  ansible.builtin.shell: |
    firewall-cmd --zone=trusted --change-interface=lxcbr0 --permanent
    firewall-cmd --reload
  ```
  If `--change-interface` fails, `--reload` still runs and (succeeding) makes the task **pass** with the firewall not configured.

- `playbooks/imports/optional/common/play-rust-dev.yml:68-71`:
  ```yaml
  ansible.builtin.shell: |
    source ~/.cargo/env
    rustup update stable
    rustup default stable
  ```
  A failed `source` or `rustup update` is masked by `rustup default`. The same file's verification task (lines 165-169: `rustc --version; cargo --version; rustup --version`) only gates on `rustup --version`.

- `playbooks/imports/play-claude-code.yml:44-45`:
  ```yaml
  ansible.builtin.shell: |
    curl -fsSL https://claude.ai/install.sh | bash -s {{ claude_code_version }}
  ```
  No `pipefail`: if `curl` fails, `bash` reads empty stdin and exits 0 — the install task reports **success** on a network failure. (A later `claude --version` task partially mitigates, but the installing task itself lies.) The same pattern appears at `play-rust-dev.yml:44` (rustup installer) and `play-rust-dev.yml:97` (cargo-binstall installer).

- `playbooks/imports/play-vscode.yml:11-19`: the block uses `set -x` (tracing) but not `set -e`, and `dnf check-update || true` (line 16) masks *all* dnf failures, not just the legitimate rc=100 "updates available" case.

- `playbooks/imports/play-claude-code.yml:98-106`: a summary block whose `$(claude --version)` / `$(rg --version | head -1)` substitutions are embedded in `echo` lines, so their failures are invisible.

Counter-examples already in the repo show the correct style: `play-AB-dnf-upgrade.yml:151` (`set -eo pipefail`), `play-lxc-install-config.yml:224` (`set -o pipefail` before a pipeline whose output is then asserted), `play-speech-to-text.yml`, `play-unifi-controller.yml`.

Note also that `CLAUDE/AnsibleStyle.md` ("External Repository Integration") recommends `set -x` for shell blocks but **not** `set -e` — the style guide itself encodes the gap.

### Impact

Provisioning steps can silently half-complete (firewall not opened, toolchain not updated, installer never downloaded) while the play reports green. This is the precise drift the project's fail-fast rule exists to prevent, and none of the existing QA scripts (`qa-ansible.bash`, semgrep rules) detects it.

### Recommendation

1. Standardise a header for all multi-command `shell: |` blocks: `set -euo pipefail` (with `executable: /bin/bash`).
2. Add a QA check (extend `qa-ansible.bash` or add a semgrep rule) flagging multi-line `shell: |` blocks that lack `set -e`.
3. Update `CLAUDE/AnsibleStyle.md` to mandate `set -euo pipefail` (not merely `set -x`) in shell blocks.
4. Fix `play-vscode.yml:16` to allow only rc 0/100: `dnf check-update; rc=$?; [ "$rc" -eq 0 ] || [ "$rc" -eq 100 ] || exit "$rc"`.

---

## FF-02: play-toolbox-install.yml uses the prohibited "skip and warn" pattern

**Severity: medium** | **Area: playbooks** | **Effort: small**

### Evidence

`playbooks/imports/play-toolbox-install.yml:97-110`:

```yaml
ansible.builtin.shell: |
  TOOLBOX_BIN=$(find ~/.local/share/JetBrains/Toolbox -name "jetbrains-toolbox" -type f 2>/dev/null | head -1)
  if [ -n "$TOOLBOX_BIN" ] && [ -x "$TOOLBOX_BIN" ]; then
    ...
  else
    echo "WARNING: Could not find JetBrains Toolbox binary after installation"
  fi
```

The task is gated on `st_jetbrains_toolbox.stat.exists == True` (the archive was installed), so the binary being missing afterwards is an error state — yet the block prints a WARNING and exits 0. CLAUDE.md: *"❌ 'Skip and warn' pattern — NEVER use debug to warn and continue"*.

### Impact

A broken Toolbox extraction is reported as a passing play; the user discovers it only when the launcher is missing.

### Recommendation

Replace the `else` branch with `echo "ERROR: …" >&2; exit 1` (and add `set -euo pipefail` per FF-01), or convert to a probe task + explicit `ansible.builtin.fail` task.

---

## FF-03: qa-bash.bash — shellcheck silently optional, advisory-only, and crash-masked

**Severity: medium** | **Area: scripts** | **Effort: small**

### Evidence

`scripts/qa-bash.bash:64-75`:

```bash
if command -v shellcheck &>/dev/null && [[ $TOTAL -gt 0 ]]; then
    printf '%s\0' "${BASH_FILES[@]}" \
        | xargs -0 shellcheck --format json 2>/dev/null \
        | jq -s 'add // []' > "$TMP_SC" || true
    sc_count=$(jq 'length' "$TMP_SC")
    [[ "$sc_count" -gt 0 ]] && echo "⚠ shellcheck: $sc_count issues (see $JSON_OUT .shellcheck_diagnostics)"
else
    printf '[]' > "$TMP_SC"
fi
```

Three distinct problems:

1. **Skip-if-absent** (line 67/73-74): if shellcheck is not installed, the script writes `[]` and the QA run *passes*. This directly contradicts the "Missing Dependencies — Fail Fast" rule and is inconsistent with the sibling scripts: `qa-python.bash:21-24` exits 2 when ruff is missing, and `qa-patterns.bash:26-29` exits 2 when semgrep is missing; `qa-all.bash:29-31` already has the exit-2 plumbing waiting to be used.
2. **Advisory-only** (line 72): shellcheck findings print a `⚠` but never increment `ERRORS`, so they can never fail QA. The comment at line 78 ("754+ issues") suggests a known backlog has been parked as permanent warnings — the "warn and continue" pattern the project prohibits.
3. **Crash-masking** (lines 68-70): the trailing `|| true` plus `2>/dev/null` covers the entire pipeline under `pipefail`. A shellcheck or jq crash yields an empty `$TMP_SC`; `jq 'length'` then yields empty, and `[[ "" -gt 0 ]]` evaluates as 0 — reported as zero issues.

### Impact

The project's own QA gate — the enforcement mechanism for rule #1 — can report "pass" with shellcheck absent, crashed, or drowning in unaddressed findings. `CLAUDE/QA.md` tells agents shellcheck runs on "All bash files", which is not guaranteed true.

### Recommendation

Make shellcheck a hard dependency (`exit 2` when missing, matching ruff/semgrep handling); separate "shellcheck found issues" (rc 1, keep JSON) from "shellcheck failed" (other rc → exit 2); and either gate QA on shellcheck findings (after burning down the backlog, possibly via `--severity=error` initially) or document the advisory status explicitly in `CLAUDE/QA.md`.

---

## FF-04: qa-python.bash — ruff crash indistinguishable from "no findings"

**Severity: medium** | **Area: scripts** | **Effort: small**

### Evidence

`scripts/qa-python.bash:75-77`:

```bash
ruff check --fix "${PY_FILES[@]}" >/dev/null 2>&1 || true
ruff_raw=$(ruff check --output-format json "${PY_FILES[@]}" 2>/dev/null) || true
RUFF_JSON="${ruff_raw:-[]}"
```

The presence check at lines 21-24 is correct fail-fast, but here ruff's stderr is discarded and all exit codes suppressed. Ruff exits 1 for "findings" (handled — JSON captured) but 2 for "ruff itself failed" (bad config, internal error, unreadable file). In the rc=2 case `ruff_raw` is empty, `RUFF_JSON` becomes `[]`, and the run is reported as a clean pass.

### Impact

A broken ruff configuration silently disables the Python lint gate while QA keeps reporting green.

### Recommendation

Capture rc explicitly: `rc=0; ruff_raw=$(ruff check --output-format json … 2>"$TMP_ERR") || rc=$?`; treat `rc -ge 2` as a hard failure (exit 2 with stderr shown), mirroring the exemplary handling in `qa-patterns.bash:35-61`.

---

## FF-05: docker-in-lxc — warn-and-continue verification and suppressed `npm update`

**Severity: medium** | **Area: files** | **Effort: small**

### Evidence

`files/var/local/docker-in-lxc` (has `set -e` at line 9):

- Line 434, inside a `set -e` provisioning heredoc:
  ```bash
  claude --version || echo "Claude Code version check failed (may still work)"
  ```
  The verification step of "Installing Claude Code" warns and continues — exactly the pattern CLAUDE.md prohibits, and also a `|| echo` that the semgrep rule `bash-error-hiding-pipe-echo` would flag in a `.bash` file (this file is extensionless, and heredoc content may evade the scanner).
- Line 465, in the generated in-container `ccy` wrapper:
  ```bash
  npm update -g @anthropic-ai/claude-code 2>&1 | grep -v "npm WARN" || true
  ```
  The `|| true` is presumably aimed at `grep -v` exiting 1 when all output is filtered, but it equally hides a complete `npm update` failure (network down, registry error), after which the wrapper happily `exec`s a stale Claude Code.

(Line 528 `gh auth token 2>/dev/null || echo ""` and line 708 `lxc-stop … || true` were checked and are legitimate probe/cleanup patterns.)

### Impact

A container can be provisioned with a broken Claude Code install reported as success; in-container update failures are permanently invisible.

### Recommendation

Make line 434 fail (`if ! claude --version; then echo "ERROR: …" >&2; exit 1; fi`). For line 465, separate concerns: run `npm update` checking its rc, then filter the captured output — e.g. `out=$(npm update -g @anthropic-ai/claude-code 2>&1) || { echo "WARN: update failed: $out" >&2; }` if best-effort updating is genuinely intended, with an explicit comment saying so.

---

## FF-06: CCY entrypoint — GitHub known_hosts fetch fails completely silently

**Severity: low** | **Area: ccy** | **Effort: small**

### Evidence

`files/var/local/claude-yolo/entrypoint.sh:94-96`:

```bash
if curl -sL --max-time 5 https://api.github.com/meta 2>/dev/null | jq -r '.ssh_keys | .[]' 2>/dev/null | sed -e 's/^/github.com /' >> ~/.ssh/known_hosts 2>/dev/null; then
    chmod 600 ~/.ssh/known_hosts
fi
```

If curl times out, jq receives garbage, or the append fails, there is no `else` branch and **all stderr from all three stages is discarded** — no warning, no failure. The rest of the entrypoint is rigorously fail-fast (GH_TOKEN check, `gh auth login` check, account-mismatch check, ssh-add checks), making this stage the only fully silent one.

### Impact

`git push`/`git pull` over SSH inside the container later fails (or interactively prompts) with host-key verification errors and zero breadcrumb pointing to the real cause. SSH push is a core CCY workflow.

### Recommendation

Add an `else` branch printing a loud warning (or hard-fail, consistent with the surrounding code): `echo "WARNING: could not fetch GitHub host keys — SSH operations may fail host verification" >&2`. Stop discarding curl/jq stderr.

---

## FF-07: shutdown-with-update — no `set -e`; firmware failures warn-and-continue

**Severity: low** | **Area: files** | **Effort: small**

### Evidence

`files/usr/local/bin/shutdown-with-update` has no `set -e` (lines 1-5), and lines 8-14:

```bash
if ! fwupdmgr refresh --force; then
    echo "Warning: Firmware refresh failed, continuing..."
fi
if ! fwupdmgr update -y; then
    echo "Warning: Firmware update failed or no updates available, continuing..."
fi
```

The script otherwise checks every step explicitly (dnf failure exits 1; shutdown failure handled interactively), so the missing `set -e` is mitigated by design. However `fwupdmgr update -y` legitimately returns non-zero for "no updates available", and the code conflates that with genuine update failure — a real error is reduced to the same one-line warning. `fwupdmgr refresh --force` failing (e.g. metadata service down) is similarly waved through.

### Impact

Limited: firmware updates silently skipped before shutdown; package updates still gate correctly.

### Recommendation

Distinguish fwupdmgr's exit codes (rc 2 = nothing to do on modern fwupd) or check output, and add `set -u`/`set -o pipefail` plus per-step handling consistent with the rest of the script.

---

## FF-08: Diagnostic scripts lack strict mode and any design annotation

**Severity: low** | **Area: scripts** | **Effort: small**

### Evidence

The following deployed/runner scripts have no `set -e` (verified by whole-file grep, not just head): `scripts/nvidia-status.bash`, `scripts/check-displaylink-status.sh`, `files/usr/local/bin/debug-pipewire.bash`, `files/usr/local/bin/gh-print-auth-url`.

The two status checkers are *deliberate* aggregate-probe designs (they run many probes and report ✅/❌ per check via `print_status`), where `set -e` would break the design — these are effectively legitimate. `debug-pipewire.bash` is a diagnostic dump that intentionally continues through failures. `gh-print-auth-url` is a three-line printer. None of them carries a comment explaining the deviation, however, which is what the project requires for fail-fast exceptions elsewhere (`# FAIL-FAST-OK:` in Ansible).

By contrast, every bash script in `files/home/.local/bin/` and all `fedora-install/*.bash` scripts have `set -e` or stricter — the repo norm is clearly strict mode.

### Recommendation

Add `set -uo pipefail` (safe even for probe-style scripts) and a one-line header comment (e.g. `# FAIL-FAST-OK: status checker — probes are individually reported, not fatal`) to each, so QA/reviewers can distinguish deliberate design from omission.

---

## FF-09: wsi-* Python family — ~40 `except Exception: pass` blocks, some on data paths

**Severity: low** | **Area: files** | **Effort: medium**

### Evidence

`files/home/.local/bin/wsi-stream`, `wsi-stream-server`, `wsi-article`, `wsi-article-window`, `wsi-server-manager`, `wsi-model-manager` contain ~40 `except Exception: pass` blocks. Spot-checks show the majority are defensible best-effort cleanup/notification paths (e.g. `wsi-stream:55-75` — `cleanup_on_exit` releasing the recorder and PID file; `wsi-stream:120-128` — desktop notification failure). But some swallow **data-path** errors:

- `wsi-article-window:285-313` (`_read_all_raw`, `_update_display`): failures reading `ARTICLE_RAW_FILE` / `ARTICLE_BUFFER_FILE` / partial file are silently treated as empty content. A permissions or I/O error would make the user's dictated article content silently vanish from display and from the subsequent polish step, with no message anywhere.

`CLAUDE/QA.md` already flags these scripts as needing manual runtime testing; silent exception swallowing makes such testing harder.

### Recommendation

Keep best-effort handling for cleanup/notification paths but narrow them (`except OSError`) and add a `log(...)` call; in data-read paths (`_read_all_raw` and friends), at minimum log the exception and surface a status-bar error rather than returning empty content.

---

## FF-10: QA tooling pattern gaps — `|| true` uncovered; qa-ansible regex/scope gaps

**Severity: low** | **Area: scripts** | **Effort: small**

### Evidence

- `.semgrep/bash-conventions.yml` contains a single rule (`bash-error-hiding-pipe-echo`, pattern `$CMD || echo $MSG`). The equally prohibited `|| true` (outside legitimate arithmetic-increment and cleanup idioms), `2>/dev/null`-swallows, and `cmd || :` are not covered, and shell content embedded in playbook `shell: |` blocks is outside semgrep's bash scan entirely.
- `scripts/qa-ansible.bash:8` matches only `failed_when: false|ignore_errors: true|ignore_errors: yes|ignore_unreachable: true`. YAML-equivalent spellings (`failed_when: no`, `ignore_errors: True`) would slip through, and the scan covers `playbooks/` only — `tasks/` (currently `tasks/ensure-jq.yml`, clean today) is not scanned.

### Impact

The enforcement net has known holes; FF-01, FF-02 and FF-05 all exist *because* nothing catches them.

### Recommendation

Extend `qa-ansible.bash` to scan `tasks/` and match case/synonym variants; add a semgrep rule (or grep check) for unannotated `|| true` in repo bash, with an inline `# FAIL-FAST-OK:`-style annotation convention for the legitimate cleanup/arithmetic cases; add the FF-01 shell-block check.

---

## FF-11: ssh-handling.bash — token-owner cross-check silently skipped when GitHub API unreachable

**Severity: info** | **Area: ccy** | **Effort: small**

### Evidence

`files/var/local/claude-yolo/lib/ssh-handling.bash:400-415`:

```bash
token_user=$(GH_TOKEN="$GH_TOKEN" gh api user --jq .login 2>/dev/null)
if [ -n "$token_user" ] && [ "$token_user" != "$GITHUB_USERNAME" ]; then
    print_error "Token owner does not match SSH-detected account"
    ...
```

If `gh api user` fails (network/API outage), `token_user` is empty and the mismatch check is skipped without any message; the success line then prints `✓ … gh token → ` with an empty name. The container entrypoint re-verifies (`entrypoint.sh:39-51`) and fails fast there, so the net behaviour remains safe — but the host-side check, whose stated purpose (per the in-file comment) is to fail *early* on the host, silently downgrades itself.

### Recommendation

Branch on the empty case explicitly: warn loudly ("could not verify token owner — GitHub API unreachable; the container entrypoint will re-verify") so the skip is visible, or fail if offline verification is considered mandatory.

---

## Verified-compliant patterns (not findings)

These were investigated and confirmed as legitimate probe-then-fail or annotated patterns — they must **not** be "fixed":

- All 60+ `failed_when: false` / `ignore_errors: true` instances in `playbooks/` carry `# FAIL-FAST-OK:` annotations with accurate reasons (e.g. `play-claude-yolo.yml:16,34,53`, `play-nvidia.yml:162,203,284`, `play-docker-overlay2-migration.yml` throughout).
- All bare `failed_when: <condition>` overrides are *stricter* custom failure conditions, e.g. `play-git-hooks-security.yml:16-50`, `play-github-cli-multi.yml:437` — the latter's permissive rc list `[0, 1, 124, 255]` is followed by an explicit `assert` on `'successfully authenticated' in item.stdout` and the expected `Hi <user>!` identity (lines 451-475), a model probe-then-assert.
- `scripts/qa-ctrl-z-patch.bash:79` `|| true` — output captured, result verified by inspecting the patched file (line 84), explicitly commented.
- `fedora-install/setup-netinstall-boot.bash` — `set -euo pipefail` at line 26; its many `|| true` instances are in unwind/cleanup paths (umount/losetup/fuser), and `(( errors++ )) || true` is the standard arithmetic-under-`set -e` idiom feeding an explicit final error gate (lines 957-1012).
- `files/var/local/claude-yolo/claude-yolo` — `set -e` at line 41; `|| echo "unknown"` fallbacks are confined to advisory version/label displays and interactive cleanup menus.
- `files/var/local/claude-yolo/lib/token-management.bash:17` — `date` parse failure explicitly handled with fallback return.
- Sourced files correctly avoid `set -e`: `files/home/bashrc-includes/*.bash`, `files/etc/profile.d/*`, `files/var/local/claude-yolo/lib/*.bash` (sourced into the `set -e` main script), `files/var/local/colours`, `files/var/local/ps1-prompt`.
- `scripts/test-ccy-ssh-probe.bash` — deliberately `set -uo pipefail` with every step gated by an explicit `fail()` that exits 1.
- GNOME extension `catch` blocks are commented best-effort handlers or fallback chains (correct for `disable()`/cleanup in GNOME Shell, where throwing breaks the shell).
- `files/usr/bin/gnome-shell-extension-installer` is vendored third-party (GPL, upstream brunelli/gnome-shell-extension-installer) — skimmed only, per audit scope.
- `files/usr/local/bin/manage-kernel-versions.py` — its two `except: pass` blocks are commented, optional-journal-logging and best-effort desktop notification; subprocess errors are otherwise raised via `check=True` handling.

## Positive Observations

- **The annotation discipline is real and enforced.** Every fail-fast suppression in the Ansible tree is justified inline, and `qa-ansible.bash` plus the hooks daemon make regressions hard.
- **`qa-patterns.bash` is a model fail-fast script**: hard tool requirement (exit 2), explicit semgrep rc classification (`rc >= 2` = tool failure), and JSON-output validation before trusting results.
- **`qa-all.bash` propagates missing-tool failures correctly** (exit 2 short-circuit) and aggregates sub-check failures without swallowing them.
- **The CCY entrypoint and ssh-handling library** verify identity end-to-end (token retrieval, owner cross-check, in-container re-verification) with actionable multi-line error messages telling the user exactly which playbook to run.
- **Every bash script in `files/home/.local/bin/` and `fedora-install/`** uses strict mode; `set -euo pipefail` is clearly the house norm for executables.
- **Probe-then-fail is used idiomatically throughout** — registered probe results are checked by follow-up `fail`/`assert` tasks with remediation instructions, exactly as the house rules require.

---

## Adversarial Verification Appendix

### FF-01 — CONFIRMED (high confidence)

Confirmed by reading the cited files. Verified counts: 75 multi-line `ansible.builtin.shell: |` blocks across 28 playbook files; only 14 contain `set -e` (auditor claimed ~12 — materially accurate). Concrete cases all check out: (1) playbooks/imports/play-lxc-install-config.yml:73-75 — `firewall-cmd --zone=trusted --change-interface=lxcbr0 --permanent` followed by `firewall-cmd --reload` with no set -e; a failed change-interface is masked by a successful reload (rc 0). No FAIL-FAST-OK annotation. (2) play-rust-dev.yml:68-71 — `rustup update stable` failure masked by `rustup default stable`; :44 and :97 are `curl … | sh/bash` with no pipefail (curl failure → empty stdin → shell exits 0 → task reports success); :165 — only `rustup --version` (last command) is gated. (3) play-claude-code.yml:44-45 — `curl -fsSL https://claude.ai/install.sh | bash` with no pipefail; NOTE a compensating control exists here that the auditor did not mention: the immediately following task (line 51, `claude --version`) would fail the play if install silently failed, so this specific example is partially mitigated — but the pattern itself remains a violation. (4) play-vscode.yml:16 — `dnf check-update || true` swallows ALL dnf failures (network, repo errors), not just the benign rc 100; recommendation to gate on rc 0/100 is correct. (5) CLAUDE/AnsibleStyle.md ('External Repository Integration') recommends `set -x` only — confirmed it never mandates set -e/pipefail, contradicting CLAUDE.md fail-fast rule #1 ('Use set -e in all bash scripts'). No existing QA gate covers this: .semgrep/bash-conventions.yml catches `|| echo` patterns and qa checks cover failed_when:false annotations, but nothing flags multi-command shell blocks lacking set -e. Severity high is justified: this systemically violates the project's #1 non-negotiable hard rule across ~60 of 75 blocks, and several cases (firewall config, rustup toolchain) have no downstream verification. Minor correction: a handful of the 'missing set -e' blocks are single-command pipelines where only pipefail (not set -e) matters, and play-claude-code.yml:44 has a downstream verify task.



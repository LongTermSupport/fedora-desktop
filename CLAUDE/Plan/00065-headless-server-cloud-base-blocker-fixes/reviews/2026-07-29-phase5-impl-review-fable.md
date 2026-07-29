# Phase 5 Implementation Review — server-recommended bundle (commit 9506e82)

**VERDICT: SOUND-WITH-FIXES**

Reviewed against `LongTermSupport/fedora-desktop` HEAD (F44 branch, freshly
`git pull --ff-only`'d — no drift) commit `9506e82`. `run.bash` under `set -e -u`
(set inside `main()`'s subshell, confirmed) plus `bash -n` and default-severity
`shellcheck` are both clean on the file. One real (currently dormant) defect and
one minor regression-claim inaccuracy; no hang/abort risk found in any of the 12
curated bundle plays for a headless server run.

## Findings

1. **[MEDIUM] Silent last-line drop if the bundle file ever loses its trailing
   newline — `run.bash:404-408`.**

   ```bash
   while IFS= read -r _line; do
     [[ -z "$_line" || "$_line" == \#* ]] && continue
     _expanded+=("$_line")
   done < "$_bundle_file"
   ```

   Classic `while read` gotcha: if the final line of `$_bundle_file` has no
   trailing `\n`, `read` still populates `$_line` but returns non-zero (EOF
   without a delimiter), so the **loop condition fails before the body runs for
   that line** — the last play in the manifest is silently dropped, no error,
   no warning. Reproduced in an isolated harness extracted from this exact
   block: a 2-line bundle with no trailing newline yielded only 1 expanded
   play. This is precisely a silent under-provision (`CLAUDE.md` rule #1: "No
   silent failures").
   **Currently dormant**: the shipped
   `playbooks/imports/optional/server-recommended.bundle` does end with `\n`
   (confirmed via `od -c`), so today's 12-play expansion is complete. But the
   code has no defence against a future edit that strips the trailing newline
   (many editors do this on save; a bare `printf 'play-foo.yml'` append with no
   `\n` would too) — and the failure mode is silence, not a loud abort.
   **Fix**: `while IFS= read -r _line || [[ -n "$_line" ]]; do ... done < "$_bundle_file"`
   — the idiomatic bash-safe pattern that still processes a final line even
   without a trailing newline.

2. **[LOW] De-dup now applies unconditionally, not just to bundle expansion —
   changes existing explicit-list behaviour for duplicate tokens.** The commit
   message states "unknown-token/failure handling unchanged" and the code
   comment implies the block is a no-op for non-bundle specs. In fact the
   de-dup step (`run.bash:420-427`) runs on **every** spec, bundle or not.
   Verified: `RUN_BASH_OPTIONAL_PLAYBOOKS="play-golang.yml play-golang.yml"`
   (no `server-recommended` involved) now runs `play-golang.yml` **once**
   instead of twice, as it would have pre-Phase-5. Harmless in practice (plays
   are idempotent per project convention, and running twice was never
   deliberate), but it is a real behaviour change for a case the review brief
   explicitly asked about ("BYTE-IDENTICAL for non-bundle specs") — the claim
   is not quite accurate. Worth a one-line note in the changelog/comment rather
   than a code change.

3. **[LOW/cosmetic] Manifest lines are not trimmed of leading/trailing
   whitespace.** A manifest line like `" play-golang.yml "` would fail the
   exact-equality match in the resolver loop and hit the loud
   `hl_abort "requested optional playbook '...' not found"` — this is a safe
   failure mode (fail-fast, not silent), just a confusing diagnostic if it ever
   happens. Not a blocker; the shipped manifest has no such lines (`cat -A`
   confirmed no trailing whitespace on any line).

## Confirmed sound

- **Reserved-keyword match is exact equality** (`[[ "$req" == "server-recommended" ]]`),
  not substring/glob — no collision risk with a hypothetical future
  `play-server-recommended.yml`.
- **Empty-array expansion under `set -u` is safe** on this bash version:
  tested `_expanded=()`/`_reqs_deduped=()` staying empty (all-comment bundle,
  empty bundle) through `"${arr[@]}"` expansion with no unbound-variable error
  (bash ≥ 4.4 semantics — confirmed via isolated harness, 0 crashes across 7
  edge-case scenarios: empty bundle, all-comments, bundle+explicit dup,
  `server-recommended` listed twice, no-trailing-newline, whitespace-padded
  line, empty comma segments).
- **De-dup preserves first-seen order** correctly (bundle entries win over a
  later explicit duplicate of the same play).
- **Missing-manifest `hl_abort` is reachable and correctly worded.**
- **`--help-run-headless` entry is correctly placed** (under that flag's
  branch, not the general `--help`), and `RUN_BASH_VERSION` 1.10.0 → 1.11.0 is
  bumped with an inline changelog entry per the file's convention.
- **`bash -n` clean; `shellcheck run.bash` clean at DEFAULT severity** (not
  just `-S error`) — 0 findings, so no SC2068/SC2199/SC2076/unquoted-expansion
  regressions from the added block.
- **No new interactive-prompt path**: the added block calls only `hl_abort`
  (LOUD, non-interactive) on its two error paths; nothing reachable in this
  diff can hit a prompt.
- Docs (`docs/headless-provisioning.md`, `docs/headless-server-install.md`)
  accurately describe the new keyword and link the manifest.

## Per-bundle-play checks (all 12 opened; scope confirmed `general` for every one)

- **play-golang.yml, play-rust-dev.yml, play-distrobox.yml,
  play-network-tools.yml, play-open-command.yml,
  play-compression-helpers.yml** — no `pause`/`vars_prompt`/GUI dependency;
  package installs + `debug` summaries only. Headless-safe.
- **play-rclone.yml** — checked in full. All `systemctl --user`/XDG_RUNTIME_DIR
  mount tasks are gated `when: rclone_mounts is defined`, which is undefined
  by default (no host_vars opt-in) — so with the bundle alone (no extra
  config), this play only installs the `rclone` package + utility scripts, no
  systemd/mount action ever executes. The GNOME-Files-bookmark tasks are
  further gated `provisioning_profile != 'server'`. Headless-safe by default;
  a subsequent explicit `rclone_mounts` opt-in is the user's own decision,
  outside this bundle's scope, and already uses `loginctl enable-linger` so it
  doesn't need an active session either.
- **play-disk-reclaim.yml** — checked in full. Package installs +
  `reclaim` TUI script deployment only; the one GUI-only task (`baobab`) is
  gated `when: provisioning_profile != 'server'`. The deployed `reclaim` script
  is itself interactive but is never *executed* by the play — only installed.
  Headless-safe.
- **play-advanced-kernel-management.yml** — checked in full. System-scope
  `systemd` units only (no `--user`, no XDG_RUNTIME_DIR dependency), a
  `dnf.conf` blockinfile, and a one-shot analysis script run via `command:`
  with `changed_when: false`. No prompts, no GUI. Headless-safe.
- **play-container-watch.yml** — checked in full, one of the four
  explicitly-flagged plays. It DOES contain an `ansible.builtin.pause` (line
  171, "Wait for container-watch extension to unload"), but it sits inside the
  GNOME-extension section that is gated `when: provisioning_profile != 'server'`
  end-to-end (every task from the extension-deploy onward carries that guard,
  and the pause task's own `when:` repeats it alongside
  `extension_enabled.rc == 0`). On any headless server run
  `provisioning_profile == 'server'` always, so this task — and the whole
  GNOME-extension branch — is unreachable. The general watchdog
  daemon/timer part of the play (which does run on server) uses a
  probe-then-conditional-enable pattern (`user_systemd_probe.rc == 0`) with
  `failed_when: false  # FAIL-FAST-OK:` annotations and a `debug` deferral
  message — never a hard abort, never a hang. Headless-safe.
- **play-claude-devtools.yml** — checked in full, one of the four
  explicitly-flagged plays. No prompts; a container-engine-missing condition
  hits a clean `ansible.builtin.fail` (loud, correct fail-fast, not a hang).
  Headless-safe as far as this review's mandate goes. Separately worth
  flagging (not a headless-safety defect, an advisory curation note): this
  play `git clone`s and builds a container image from a third-party,
  **unpinned** GitHub repo (`https://github.com/matt1398/claude-devtools`,
  `update: true` — always tracks upstream `HEAD`). That's pre-existing
  behaviour of the play itself (not introduced by this diff), but Phase 5 is
  what makes it something a server operator gets by default via one keyword —
  worth a conscious note in the design doc if not already there, though it
  does not block this review.
- **play-collaboration.yml** — checked in full. `tmate` package + two
  wrapper-script deployments + a `debug` summary; no prompts, no GUI.
  Headless-safe.
- **play-lastpass.yml (the exclusion)** — checked in full, per the review
  brief's explicit ask. The `ansible.builtin.pause` tasks are gated
  `when: not lastpass_configured`, where
  `lastpass_configured := lastpass_accounts is defined and length > 0`. On a
  genuinely fresh headless server there is no mechanism in this repo to
  pre-seed `lastpass_accounts`, so the guard evaluates `true` by default and
  the pause block **would** fire on a first run. Unlike run.bash's own
  interactive-prompt helpers (which all carry a headless backstop per
  `CLAUDE/Plan/00065` Phase 0/1 work), `ansible.builtin.pause` is a raw Ansible
  module with no such backstop — a headless invocation would hang (or behave
  unpredictably on non-TTY stdin) rather than fail loud. **The exclusion is
  correct, not superstition.**

## Regression check on non-bundle specs

Confirmed via isolated harness (not the live in-container Ansible, per CCY
container rules — pure bash logic extracted verbatim from the new block):
`none`/unset still skips (unchanged, that check precedes the new block
entirely), unknown-token abort still fires, failed-play abort still fires,
missing-manifest abort is reachable. The one behavioural delta is the
unconditional de-dup noted in Finding 2 above — everything else is unchanged.

# Phase 5 design: `server-recommended` optional-play bundle

**Scope**: Plan 00065 Phase 5 (T5.1–T5.4). Design + curation only — no `run.bash`
or play edits made, nothing committed. This doc is the artifact the implementer
executes against.

## DECISION 1 (mechanism): tracked manifest file — `playbooks/imports/optional/server-recommended.bundle`

`run.bash` gains ONE new expansion step in `hl_run_optional_playbooks()`
(`run.bash:370-407`): when a requested token is the literal string
`server-recommended`, it is replaced by the play-basename list read from the
manifest file, before the existing per-token resolution loop runs unchanged.

### Rejected alternatives

- **(a) hardcoded bash array in `run.bash`.** Rejected because it couples pure
  *curation* (which plays are in the bundle — content that will change as new
  optional plays are added/removed) to `run.bash`'s own versioning discipline
  (`RUN_BASH_VERSION` + changelog line on **every** edit, per the file's own
  header, `run.bash:4-6`). A future "add play-X to the bundle" change should be
  a one-line diff to a curation file, not a `run.bash` edit that forces a
  version bump of the whole headless-execution script for content-only churn.
- **(b) per-play marker** (a `# server-recommended` comment or play var grepped
  at runtime). Rejected because it scatters the bundle's membership across N
  files instead of one reviewable list, makes "what exactly is in the bundle"
  a grep exercise instead of an `ls`+`cat`, and gives no natural place to
  control bundle **order** (the manifest's line order is the run order,
  matching how the interactive menu's `find | sort` gives a stable, readable
  order today).
- **(d)** — no better option surfaced. A manifest file is also the repo's
  existing pattern for "a declarative list of names Ansible/tooling consumes"
  (`requirements.yml` at repo root), so it isn't a new convention.

### Manifest grammar

Plain text, one **exact `play-*.yml` basename** per line. Full-line `#`
comments and blank lines are ignored (no inline comments — keeps the parser
trivial and shellcheck-clean):

```
# server-recommended bundle (Plan 00065 Phase 5) — see
# CLAUDE/Plan/00065-headless-server-cloud-base-blocker-fixes/PLAN.md Phase 5.
# One play basename per line. Full-line '#' comments and blank lines ignored.
play-golang.yml
play-rust-dev.yml
play-distrobox.yml
play-network-tools.yml
play-rclone.yml
play-open-command.yml
play-compression-helpers.yml
play-disk-reclaim.yml
play-advanced-kernel-management.yml
play-container-watch.yml
play-claude-devtools.yml
play-collaboration.yml
```

The file is **not** `*.yml`, so it is invisible to every existing
`find playbooks/imports/optional -name "*.yml"` enumeration (`run.bash:382`
and the interactive menu's `find`s, `run.bash:2484`/`2519`/`2563`/`2587`) — no
risk of it being picked up as a play itself.

### Exact `run.bash` change shape

In `hl_run_optional_playbooks()` (`run.bash:370-407`), insert an expansion +
de-dup pass between building `_reqs` (line 383-384) and the existing
resolution loop (line 386):

```bash
  local -a _reqs
  IFS=' ,' read -ra _reqs <<< "$spec"

  # Expand the server-recommended bundle keyword (Plan 00065 Phase 5) into its
  # manifest-listed plays, then de-dup so a play named both by the bundle and
  # an explicit token only runs once. Expansion happens BEFORE the existing
  # per-token resolution loop, so composing with explicit tokens ("server-
  # recommended play-ddev.yml") and unknown-token abort behaviour are both
  # inherited for free — nothing below this block changes.
  local _bundle_file="playbooks/imports/optional/server-recommended.bundle"
  local -a _expanded=()
  local req _line
  for req in "${_reqs[@]}"; do
    [[ -z "$req" ]] && continue
    if [[ "$req" == "server-recommended" ]]; then
      if [[ ! -f "$_bundle_file" ]]; then
        hl_abort "optional playbooks" \
          "RUN_BASH_OPTIONAL_PLAYBOOKS requested 'server-recommended' but ${_bundle_file} is missing" \
          "the ~/Projects/fedora-desktop checkout may be stale/corrupt — re-clone, or drop 'server-recommended' from the list"
      fi
      while IFS= read -r _line; do
        [[ -z "$_line" || "$_line" == \#* ]] && continue
        _expanded+=("$_line")
      done < "$_bundle_file"
    else
      _expanded+=("$req")
    fi
  done

  # De-dup, preserving first-seen order (associative-array pattern already used
  # for _seen_aliases at run.bash:932).
  local -a _reqs_deduped=()
  local -A _seen=()
  for req in "${_expanded[@]}"; do
    [[ -n "${_seen[$req]:-}" ]] && continue
    _seen[$req]=1
    _reqs_deduped+=("$req")
  done
  _reqs=("${_reqs_deduped[@]}")

  local pb base found name
  for req in "${_reqs[@]}"; do
    ... # UNCHANGED from here down
```

Everything from the existing `for req in "${_reqs[@]}"` loop onward
(`run.bash:386-406`) is untouched: unknown-token `hl_abort`, per-play
`hl_abort` on failure, and success logging all already do the right thing
once `_reqs` has been pre-expanded.

**Composability**: `RUN_BASH_OPTIONAL_PLAYBOOKS="server-recommended play-ddev.yml"`
works with zero extra logic — `server-recommended` expands to its 12 plays,
`play-ddev.yml` is appended as-is, dedup is a no-op since it isn't in the
bundle, and the unchanged loop resolves and runs all 13.
`RUN_BASH_OPTIONAL_PLAYBOOKS="server-recommended play-golang.yml"` also works
correctly — the explicit `play-golang.yml` collapses into the one already
contributed by the bundle (de-dup), so it runs exactly once, not twice.

**Unknown-token behaviour**: unchanged for every token except the reserved
keyword itself — `server-recommended` is matched by *exact* string equality
(mirroring the existing exact match on `"none"` at `run.bash:372`), so it
cannot collide with any current or future `play-*.yml` basename.

### Version bump + changelog

Bump `RUN_BASH_VERSION` `1.10.0` → `1.11.0` (minor: new user-facing feature,
backward compatible — existing explicit-list and `none` usages are unaffected)
with a changelog line appended to the header comment block (`run.bash:6`):

> `v1.11.0: Feature (Plan 00065 Phase 5) — RUN_BASH_OPTIONAL_PLAYBOOKS accepts the reserved keyword 'server-recommended', expanded from the tracked manifest playbooks/imports/optional/server-recommended.bundle into its listed plays before the existing per-token resolver runs; composes with explicit tokens via de-dup, unknown-token/failure handling unchanged.`

### `--help-run-headless` text

Add one line directly under the existing `RUN_BASH_OPTIONAL_PLAYBOOKS=...` entry
(`run.bash:522`):

```
  RUN_BASH_OPTIONAL_PLAYBOOKS=...  Space/comma list of optional plays, or 'none'.
                                   'server-recommended' expands to a curated,
                                   generic dev/server bundle (see
                                   playbooks/imports/optional/server-recommended.bundle);
                                   combine with explicit plays, e.g.
                                   "server-recommended play-ddev.yml".
```

### Interactive-menu answer: **do NOT wire it in**

The interactive optional-playbook menu (`run.bash:2461-2600`) already gives a
human strictly more power than the bundle keyword would add: `show_menu()`
(`run.bash:2312+`) offers **`A) Run all`** for the entire "Common Optional"
category (which is exactly where all 12 bundle plays live) plus a
Whitelist/Blacklist numeric selector. A human operator sitting at the menu can
already run "all of Common Optional" in one keystroke, or hand-pick a subset —
adding a 13th menu option that expands to a fixed subset of the same category
is YAGNI: it duplicates existing functionality without adding a capability,
and `show_menu()`'s single-list model has no natural slot for "keyword expands
to N items from the same list" without special-casing the loop. The bundle
keyword's actual value is headless-only (a non-interactive operator has *no*
menu at all — RUN_BASH_OPTIONAL_PLAYBOOKS is their only lever). Scope Phase 5's
`run.bash` change to `hl_run_optional_playbooks()` only.

---

## DECISION 2 (curation): final bundle — 12 plays

All 12 read and verified individually (not inferred from the plan's candidate
list). Every candidate is `scope: general` (confirmed via `grep -n "scope:"` on
each file) and headless-safe (no unconditional GUI dependency, no interactive
prompt reachable in a headless run).

| Play                                  | scope   | Headless-safe?                                                                                                                                                                                                                                                                                    | Verdict |
| ------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `play-golang.yml`                     | general | Yes — single `dnf` task, no GUI/prompt                                                                                                                                                                                                                                                            | **IN**  |
| `play-rust-dev.yml`                   | general | Yes — `rustup-init -y` non-interactive, all shell blocks are `set -euo pipefail`, no prompts                                                                                                                                                                                                      | **IN**  |
| `play-distrobox.yml`                  | general | Yes — package install + non-interactive version check                                                                                                                                                                                                                                             | **IN**  |
| `play-network-tools.yml`              | general | Yes — `dnf` install + script deploy, no GUI/prompt                                                                                                                                                                                                                                                | **IN**  |
| `play-rclone.yml`                     | general | Yes — GNOME-bookmarks tasks already gated `when: provisioning_profile != 'server'` (`play-rclone.yml:320,335`); degrades to package-only install when `rclone_config`/`rclone_mounts` vault vars are undefined (no hard failure)                                                                  | **IN**  |
| `play-open-command.yml`               | general | Yes — pure CLI wrapper + terminal-only viewer packages (`chafa`/`w3m`/`poppler-utils`/…), no GUI task at all                                                                                                                                                                                      | **IN**  |
| `play-compression-helpers.yml`        | general | Yes — the `ncompress`-conflict check is a real fail-fast `assert`/`fail` (correctly annotated `# FAIL-FAST-OK:` on the probe), not an interactive prompt                                                                                                                                          | **IN**  |
| `play-disk-reclaim.yml`               | general | Yes — the one GUI task (`baobab`) is gated `when: provisioning_profile != 'server'` (`play-disk-reclaim.yml:35`); `reclaim` TUI itself is deployed unconditionally and is confirmation-gated per InteractiveScripts.md, not run at provision time                                                 | **IN**  |
| `play-advanced-kernel-management.yml` | general | Yes — dnf.conf blockinfile + systemd path unit + script deploy, no GUI/prompt                                                                                                                                                                                                                     | **IN**  |
| `play-container-watch.yml`            | general | Yes — every GNOME-extension task gated `when: provisioning_profile != 'server'` (`play-container-watch.yml:99,109,156,164,185,191,198`); the systemd --user probe/enable steps use `failed_when: false` with an explicit `rc`-check gate and a `debug` "deferred" message, not a silent skip      | **IN**  |
| `play-claude-devtools.yml`            | general | Yes — depends on `container_engine` (Podman), which `play-podman.yml` (`scope: general`) installs unconditionally via `playbook-main.yml` on every profile including server; fails fast (not silently) if the engine is missing/unreachable; git clone + container build are both non-interactive | **IN**  |
| `play-collaboration.yml`              | general | Yes — `tmate` package + two wrapper scripts, no prompts                                                                                                                                                                                                                                           | **IN**  |

### Excluded candidates (from the plan's own exclusion list) — one-line reason each

| Play                       | scope   | Verdict + reason                                                                                                                                                                                                                                                                                                                                                                               |
| -------------------------- | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-lastpass.yml`        | general | **EXCLUDED-until-fixed** — NOT headless-safe: it issues unconditional `ansible.builtin.pause` prompts (`play-lastpass.yml:23-40+`) with no non-interactive branch at all; a headless run would hang on a TTY-less stdin or abort with no recovery. This is independent of Phases 1-3 (those didn't touch this play) — it needs its own headless-branch fix before it could ever join a bundle. |
| `play-ddev.yml`            | general | **OUT** — technically headless-safe (fails fast via `assert`/`fail` if the Docker daemon isn't reachable, no prompts), but excluded on curation grounds: DDEV is an opinionated, narrow-use-case web-dev framework choice, not something every headless dev/server box needs.                                                                                                                  |
| `play-nordvpn-openvpn.yml` | general | **OUT** — headless-safe (its GNOME-applet package is gated `when: provisioning_profile != 'server'`), but excluded: a VPN client is a personal account/opinionated choice, not a generic server need.                                                                                                                                                                                          |
| `play-cloudflare-warp.yml` | general | **OUT** — the install branch is dead on F44 (`webkit2gtk3` retired) and the play is now uninstall-only by default; even setting that aside it's the same VPN/DNS-vendor opinionated-choice category as NordVPN.                                                                                                                                                                                |
| `play-cloudflare-dns.yml`  | general | **OUT** — technically headless-safe (no prompts, no GUI), but excluded: it silently redirects **all** host DNS to Cloudflare's malware-filtering DoT resolvers — a vendor/network-policy decision that shouldn't be a bundle-default side effect for every operator, same reasoning bucket as the VPN clients even though it isn't a VPN.                                                      |

---

## Implementation checklist

1. **Create** `playbooks/imports/optional/server-recommended.bundle` with the
   12-line manifest shown in Decision 1 (header comment + 12 basenames, in the
   table order above).
2. **Edit `run.bash`**:
   - `hl_run_optional_playbooks()` (`run.bash:370-407`): insert the
     expansion+de-dup block shown above between the `_reqs` read and the
     existing resolution loop.
   - Update the function's leading comment (`run.bash:366-369`) to mention the
     `server-recommended` keyword.
   - Bump `RUN_BASH_VERSION` to `1.11.0` and append the changelog sentence
     (`run.bash:4-6`).
   - Add the `--help-run-headless` line under `RUN_BASH_OPTIONAL_PLAYBOOKS=...`
     (`run.bash:522`).
   - Do **not** touch the interactive menu block (`run.bash:2461-2600`) —
     Decision 1's answer is no wiring there.
3. **Docs**:
   - `docs/headless-provisioning.md:59` — extend the
     `RUN_BASH_OPTIONAL_PLAYBOOKS` table row to mention the bundle keyword and
     link/point at the manifest file.
   - `docs/headless-server-install.md:190` — add a `server-recommended` usage
     example alongside the existing `"none"` / explicit-list example.
4. **QA**: `./scripts/qa-all.bash` (this is a `run.bash`-only change plus a
   plain-text manifest — `qa-bash.bash`'s `bash -n` + shellcheck is the
   relevant gate; the manifest file itself is not Ansible/Bash/Python/JS so no
   stage touches it directly, but confirm it isn't accidentally matched by any
   `find -name "*.yml"` glob — it shouldn't be, since it has no `.yml`
   extension).
5. **Acceptance**: the plan's existing preflight-acceptance harness should
   exercise `RUN_BASH_OPTIONAL_PLAYBOOKS=server-recommended` (dry/parse level
   in-container; a real HOST run is the actual end-to-end proof per this
   plan's established container-boundary note).
6. **Plan bookkeeping**: mark T5.1-T5.3 in `PLAN.md` as the implementer
   completes them; T5.4 is the QA task above.

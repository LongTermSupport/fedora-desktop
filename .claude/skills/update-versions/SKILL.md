---
name: update-versions
description: Review and bump the pinned upstream version variables hardcoded in the Ansible playbooks (markless, ouch, rescrobbled, nvm, darktable, RapidRAW, ART, DisplayLink/evdi, cuDNN, …). Use when the user asks to check whether pinned dependencies/versions are outdated, review or audit version pins, or update a playbook to a newer upstream release. Runs scripts/check-pinned-versions.bash for the drift report, then guides applying a bump safely — including any adjacent sha256 checksum and asset-filename changes.
allowed-tools: [Bash, Read, Edit, Grep]
---

# Update pinned upstream versions

Several playbooks hardcode an upstream release version in a play var
(`marklessVersion: "0.9.6"`, `ouchVersion`, `rescrobbledVersion`,
`nvm_version`, `darktable_version`, `rapidraw_version`, `art_version`,
`displaylink_version`, `evdi_version`, `cudnn_version`, …). These pins drift as
upstream ships new releases. This skill reviews the drift and helps apply bumps
safely.

The playbook is always the single source of truth for the current value — the
review script reads the pin straight from the file, it is never duplicated.

## Step 1 — Review drift (always start here)

Run the review script from the repo root:

```bash
"${CLAUDE_PROJECT_DIR}/scripts/check-pinned-versions.bash"
```

It prints a table of `PINNED` vs upstream `LATEST` and the status of each pin,
and exits:

- `0` — every pin matches upstream latest (nothing to do).
- `1` — one or more pins are behind, or a manual pin (cuDNN) needs a human.
- `2` — hard error (e.g. `gh` missing/unauthenticated, or the manifest has
  drifted from the playbooks — fix the manifest row it names).

Report the outdated pins to the user and confirm which ones to bump before
editing anything. Bumping every dependency at once is rarely wanted — some are
intentionally held back (a newer release may need a matching checksum, a new
asset name, or a Fedora-version-specific build).

## Step 2 — Apply a bump (per pin, deliberately)

For each pin the user agrees to update:

1. **Edit the version var** in its playbook to the new value (drop or keep the
   `v`/`release-` prefix to match the existing style in that file).

2. **Update any adjacent checksum — this is the easy thing to miss.** Several of
   these playbooks pin a `sha256`/checksum next to the version and the download
   will FAIL verification if you bump the version but not the hash. Known
   checksum-pinning plays: `play-markless.yml`, `play-photography.yml` (RapidRAW,
   ART), and the darktable plays. Grep the same file for `sha256`/`checksum`/
   `digest` and fetch the new release's published hash (or compute it from the
   asset) before proceeding.

3. **Confirm the release asset filename still matches.** Vars like
   `rapidraw_rpm: "03_RapidRAW_v{{ rapidraw_version }}_ubuntu-22.04_x86_64.rpm"`
   embed a filename pattern that upstream sometimes changes between releases.
   Check the new release's actual asset names on GitHub and update the pattern if
   it changed, otherwise the `get_url`/download 404s.

4. **DisplayLink is a pair.** `displaylink_version`, `evdi_version`, and
   `evdi_rpm_release` move together and are Fedora-release-specific — verify the
   `fedora-{{ fedora_version }}-...` asset exists for the target release before
   bumping.

5. **Re-run QA** (bash + ansible syntax etc.):

   ```bash
   ./scripts/qa-all.bash
   ```

6. **Re-run the review script** to confirm the pin now reads as up-to-date.

7. **Deploy + test on the HOST, not in the CCY container** (this repo is
   edit-and-commit-only inside `/workspace/`). Tell the user the exact play to
   run, e.g. `ansible-playbook playbooks/imports/play-markless.yml`.

8. **Commit** the version (and checksum/asset) change, referencing the upstream
   release. Do not bundle unrelated pins in one commit.

## cuDNN (manual)

`cudnn_version` in `play-nvidia.yml` is NVIDIA-hosted, not a GitHub release, so
the script only flags it for a human. Check the CUDA/cuDNN repo or
developer.nvidia.com for the current version and matching CUDA compatibility
before bumping.

## Tracking a new pin

When a new playbook hardcodes an upstream version, add a row to the `MANIFEST`
heredoc in `scripts/check-pinned-versions.bash`:

```
<playbook-path>|<var-name>|<owner/repo>|<extra-tag-prefix>|<note>
```

Leave `<owner/repo>` empty for a non-GitHub pin that must be checked by hand
(put the where-to-look hint in `<note>`). Use `<extra-tag-prefix>` for tags that
carry more than a leading `v` (e.g. darktable's `release-`).

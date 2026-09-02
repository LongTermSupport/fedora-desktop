# Plan 00037: Image Watermarking Toolkit

**Status**: In Progress
**Created**: 2026-04-28
**Owner**: joseph
**Priority**: Medium
**Type**: Feature Implementation (CLI tooling + Ansible playbook)

## Overview

This plan delivers a generic, composable image watermarking primitive on the
desktop: a single `watermark` CLI that takes one image and produces a
watermarked sibling (`image.watermarked.jpg`) with both visible marks and full
commercial/licence metadata embedded in EXIF/IPTC/XMP. The tool is a
*primitive*, not a workflow — client projects wrap it with their own naming,
batching, and policy.

The visible mark is two-layer: a small high-contrast corner mark plus a faint
diagonal tile across the whole image that survives crop-and-go theft. The
metadata layer embeds full IPTC/XMP rights information and a custom sentinel
(`XMP-wm:Applied=true`) used for idempotency; the `.watermarked.jpg` suffix
gives the same signal at the filesystem level. The tool is delivered by a new
optional Ansible playbook (`play-image-watermarking.yml`) that installs
ImageMagick and exiftool and deploys the wrapper to `/usr/local/bin/watermark`
with an example config skeleton.

Supporting documents:

- [RESEARCH-watermarking.md](RESEARCH-watermarking.md) — tooling, metadata
  field set, visible-mark technique, idempotency channels, composability
  surface, delivered artefacts
- [DECISIONS.md](DECISIONS.md) — Decisions 1–6 and the risk table
- [PLAN_archive.md](PLAN_archive.md) — the original plan document, kept
  verbatim, including the dated progress notes

## Goals

- `watermark IMAGE` produces `IMAGE.watermarked.jpg` with both visible
  watermark layers and full licence metadata, in one invocation
- Visible watermark scales correctly across image sizes and aspect ratios
  (corner + diagonal tile, sized as a percentage of image dimensions)
- All EXIF/IPTC/XMP commercial-licence fields populated (field list in
  [RESEARCH-watermarking.md](RESEARCH-watermarking.md))
- Idempotent: re-running on `*.watermarked.jpg` or on any file with the
  `XMP-wm:Applied=true` sentinel is a no-op (`--force` to override)
- Composable: every parameter exposed as both CLI flag and config-file key,
  with deterministic precedence (CLI > `--config FILE` > user config >
  system config > built-in defaults)
- Profiles: named preset bundles (`--profile portfolio`) let client projects
  pick a config without writing flags every time
- Stable exit codes and output (absolute path of produced file on stdout)
  so shell wrappers can chain reliably
- Delivered via an optional Ansible playbook installable on any
  fedora-desktop host; not in main install path

## Non-Goals

- Not building a batch processor — `watermark` takes ONE image at a time;
  parallel/batch is the wrapper's job (`xargs -P`, GNU parallel, etc.)
- Not building a GUI
- Not bundling a default logo or copyright text — config must be supplied
  by the user (no opinionated identity)
- Not a steganographic / invisible / DCT-frequency-domain watermark in v1
  (Phase 6 decision gate — separate plan if pursued)
- Not RAW-format input (NEF/CR2/etc.) — JPEG and PNG only; users convert
  with their RAW editor first
- Not modifying RapidRAW, darktable, or other photo editors to call
  `watermark` automatically — downstream wrapper territory
- Not a library / sourceable bash file — CLI-only surface (Decision 1)
- Not video watermarking (different toolchain, different concerns)

## Tasks

### Phase 1: Research & decision gates

- [ ] ⬜ **Task 1.1**: Verify ImageMagick 7 is the version in Fedora 43
  (`dnf info ImageMagick`); confirm `magick` binary path and security
  policy file location (`/etc/ImageMagick-7/policy.xml`)
- [ ] ⬜ **Task 1.2**: Confirm exiftool version supports the full XMP
  field set (`exiftool -listg1 | grep -i rights`)
- [ ] ⬜ **Task 1.3**: Resolve open decisions (see
  [DECISIONS.md](DECISIONS.md)): config file format, sentinel namespace,
  stdin batch mode, output naming when `--output` is passed
- [x] ✅ **Task 1.4**: Write a one-liner reference test: a single
  `magick` + `exiftool` chain that produces the desired result on one
  test image — lock the recipe before scripting. Evidence in
  [RESEARCH-watermarking.md](RESEARCH-watermarking.md) (recipe-lock section).

### Phase 2: Core script — `watermark` CLI

- [x] ✅ **Task 2.1**: Write `files/usr/local/bin/watermark` (bash,
  `set -euo pipefail`, modeled on `files/usr/local/bin/compress`)
  - [x] ✅ Argument parser: `--text`, `--copyright`, `--artist`,
    `--licence-url`, `--licence-summary`, `--profile NAME`,
    `--config FILE`, `--output PATH`, `--force`, `--dry-run`,
    `--verbose`, `--no-tile`, `--no-corner`, `--show-config`, `-h|--help`
  - [x] ✅ Config precedence chain: load `/etc/watermark/defaults.conf`,
    then `~/.config/watermark/defaults.conf`, then `--config FILE`,
    then apply CLI overrides; resolve `--profile` against the merged config
  - [x] ✅ Validate: input file exists, is JPEG or PNG (by extension AND
    `file --mime-type`), copyright/artist/licence-url are non-empty
  - [x] ✅ Idempotency probe: read `XMP-wm:Applied` via exiftool (with
    `-config /etc/watermark/exiftool.config`); if `True` and no
    `--force`, print "already watermarked" + path, exit 3
  - [x] ✅ Filename suffix probe: if input matches `*.watermarked.jpg`
    and no `--force`, exit 3
  - [x] ✅ Compose visible watermark: single `magick` invocation,
    diagonal tile (mpr:tile + draw fill-reset) + southeast-gravity
    corner annotate, sizes as `%[fx:w*N]` percentages
  - [x] ✅ Write metadata: single `exiftool -overwrite_original`
    invocation setting all licence fields plus the three sentinel tags
  - [x] ✅ Output filename: `${input%.*}.watermarked.${ext}` by default,
    `--output PATH` override; refuse to overwrite existing output unless
    `--force`
  - [x] ✅ Print absolute path of output file to stdout on success;
    errors to stderr; exit codes documented in `--help`
  - [x] ✅ `--dry-run` prints the magick + exiftool commands it would run,
    then exits 0
  - [x] ✅ Shellcheck-clean
- [ ] ⬜ **Task 2.2**: Write a small test fixture: a generated test image
  (`magick -size 1920x1080 plasma: /tmp/wm-test.jpg`) plus a script that
  exercises the golden path, the idempotency-skip path, the `--force`
  path, and the missing-required-config path. Lives in
  a `watermark/` directory under the repo `tests/` tree, to be created by this
  task (consistent with other repo tests)
- [ ] ⬜ **Task 2.3**: Run the fixture; resolve any visible-watermark
  legibility issues (font size, opacity, rotation angle) by inspection on
  a handful of varied real images (landscape, portrait, panorama, dark,
  bright)

### Phase 3: Config and composition surface

- [x] ✅ **Task 3.1**: Define the config-file format (Decision 2) and
  write `files/etc/watermark/defaults.conf.example` with commented-out
  keys and one example `[blog]` profile
- [ ] ⬜ **Task 3.2**: Document the wrapping pattern for client projects
  in a short `docs/watermark.md`:
  - Shell wrapper example (define a function in
    `~/.bashrc-includes/portfolio-watermark.inc.bash` that calls
    `watermark --profile portfolio "$@"`)
  - `find ... | watermark --stdin0` example for batch
  - Exit-code reference for scripts that need to distinguish skip from
    error
- [ ] ⬜ **Task 3.3**: Decide on stdin-batch (`--stdin0`) — implement if
  decision is YES (Decision 4 says likely; small surface, large win),
  else exclude

### Phase 4: Ansible playbook

- [x] ✅ **Task 4.1**: Create
  `playbooks/imports/optional/common/play-image-watermarking.yml`
  - [x] ✅ Standard playbook header (`#!/usr/bin/env ansible-playbook`,
    `hosts: desktop`, `become: true`, `root_dir: ...`)
  - [x] ✅ Install packages: `ImageMagick`, `perl-Image-ExifTool`,
    `dejavu-sans-fonts` (idempotent — exiftool already present from
    `play-photography.yml`, will no-op)
  - [x] ✅ Preflight assert: `magick --version` runs and reports
    ImageMagick 7.x; if not, fail with actionable message
  - [x] ✅ Deploy `files/usr/local/bin/watermark` to `/usr/local/bin/`,
    mode `0755`, owner/group `root`
  - [x] ✅ Deploy `files/etc/watermark/exiftool.config` to
    `/etc/watermark/exiftool.config`, mode `0644`, owner/group `root`
    (registers the `XMP-wm:` namespace; script hardcodes `-config` to
    this path; missing file → script fails fast)
  - [x] ✅ Deploy `files/etc/watermark/defaults.conf.example` to
    `/etc/watermark/defaults.conf.example`, mode `0644`,
    owner/group `root` (NOT to user home; user copies to
    `~/.config/watermark/defaults.conf` themselves with their data)
  - [x] ✅ Create `/etc/watermark/` directory with mode `0755`
  - [x] ✅ Display message at end: pointer to
    `/etc/watermark/defaults.conf.example`, instruction to copy and edit,
    `watermark --help` reference
  - [x] ✅ Make playbook executable (`chmod +x`)
- [ ] ⬜ **Task 4.2**: Verify playbook is fully idempotent
  (`--check --diff` clean on second run)

### Phase 5: QA, host deploy, host verification

- [ ] ⬜ **Task 5.1**: Run `./scripts/qa-all.bash` — expect clean
  (script is bash, playbook follows existing patterns)
- [ ] ⬜ **Task 5.2**: Commit with `Plan 00037: Add image watermarking toolkit (initial)` referencing this plan
- [ ] ⬜ **Task 5.3**: Deploy on HOST (not container) with
  `ansible-playbook playbooks/imports/optional/common/play-image-watermarking.yml`
- [ ] ⬜ **Task 5.4**: Host smoke test:
  - [ ] ⬜ Pick 4 real images: landscape JPEG, portrait JPEG, square
    PNG, panorama JPEG
  - [ ] ⬜ Run `watermark` against each with a real `~/.config/watermark/defaults.conf`
  - [ ] ⬜ Open each `.watermarked.jpg` in Geeqie/eog, eyeball quality
  - [ ] ⬜ `exiftool` each output, confirm all licence fields and sentinel
    tags present
  - [ ] ⬜ Re-run `watermark` on each output, confirm exit 3 + skip msg
  - [ ] ⬜ Run with `--force`, confirm overwrite + new sentinel timestamp
  - [ ] ⬜ Pipe `find . -name '*.jpg' | watermark --stdin0` (if Phase 3
    decision was YES), confirm batch behaviour

### Phase 6 (decision gate): Invisible / DCT watermark add-on

- [ ] ⬜ **Task 6.1**: After v1 is in use, evaluate whether to add a
  steganographic layer (DCT-domain). Candidate tools: `openstego` (Java,
  GUI+CLI, BSD-3), `invisible-watermark` (Python, used by Stable
  Diffusion). Out-of-scope work; tracked here so it isn't forgotten.
  **This is a decision gate, not a commitment** — split into a separate
  plan if pursued.

## Success Criteria

- [ ] `watermark image.jpg` produces `image.watermarked.jpg` with both
  visible layers and full licence metadata, in one invocation
- [ ] All required EXIF/IPTC/XMP fields present in output, verified via
  `exiftool -G1 -a image.watermarked.jpg`
- [ ] Custom sentinel tags (`XMP-wm:Applied`, `XMP-wm:AppliedAt`,
  `XMP-wm:AppliedBy`) present and round-trip correctly
- [ ] Re-running `watermark` on a watermarked file exits 3 and prints a
  clear skip message
- [ ] `--force` re-watermarks (new sentinel timestamp)
- [ ] Visible watermark legible on landscape, portrait, square, panorama
  inputs at 1080p and 4K (subjective sign-off after host smoke test)
- [ ] CLI honours full config precedence chain (CLI > `--config FILE` >
  user config > system config > defaults)
- [ ] `--profile NAME` selects the right per-profile config file
- [ ] Stdin batch mode works (if Decision 4 is YES on implementation)
- [ ] Playbook is idempotent (`--check --diff` clean on second run)
- [ ] `./scripts/qa-all.bash` passes
- [ ] Shellcheck-clean
- [ ] Documentation (`docs/watermark.md`) explains wrapping pattern

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00037-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recipe locked: two-layer `magick` composition + exiftool `XMP-wm:`
  sentinel round-trip proven (Task 1.4)
- Container side of Phases 2–4 delivered: `watermark` script, example
  config, exiftool config, `play-image-watermarking.yml`; container smoke
  tests and `qa-all.bash` green
- Next: host deploy and host smoke test (Phase 5)

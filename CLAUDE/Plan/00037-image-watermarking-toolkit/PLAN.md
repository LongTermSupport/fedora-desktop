# Plan 00037: Image Watermarking Toolkit

**Status**: Not Started
**Created**: 2026-04-28
**Owner**: joseph
**Priority**: Medium
**Type**: Feature Implementation (CLI tooling + Ansible playbook)

## Overview

This plan delivers a generic, composable image watermarking primitive on the
desktop: a single `watermark` CLI that takes one image and produces a
watermarked sibling (`image.watermarked.jpg`) with both visible marks and full
commercial/licence metadata embedded in EXIF/IPTC/XMP. The tool is
deliberately scoped as a *primitive*, not a workflow — client projects
(personal portfolio sites, batch-processing scripts, photo-export hooks for
RapidRAW/darktable) wrap it with their own naming, batching, and policy.

The visible mark uses a two-layer technique: a small high-contrast corner
mark for normal viewing, plus a faint diagonal tile across the whole image
that survives crop-and-go theft and is annoying to clone-stamp out. The
metadata layer embeds full IPTC/XMP rights information (artist, copyright,
licence URL, usage terms) and a custom sentinel
(`XMP:WatermarkApplied=true`) used for idempotency. Re-running the tool on
an already-watermarked file is a no-op unless `--force` is passed; the
filename suffix `.watermarked.jpg` provides the same signal at the
filesystem level for humans and shell-loop callers.

The tool is delivered system-wide via a new optional Ansible playbook
(`play-image-watermarking.yml`) that installs ImageMagick and exiftool (both
already in Fedora repos), then deploys the wrapper script to
`/usr/local/bin/watermark` alongside an example config skeleton. No
client-project integration is in scope — the plan provides the primitive
and documents the wrapping pattern; downstream wrappers are out of scope.

## Goals

- `watermark IMAGE` produces `IMAGE.watermarked.jpg` with both visible
  watermark layers and full licence metadata, in one invocation
- Visible watermark scales correctly across image sizes and aspect ratios
  (corner + diagonal tile, sized as a percentage of image dimensions)
- All EXIF/IPTC/XMP commercial-licence fields populated:
  `EXIF:Artist`, `EXIF:Copyright`, `IPTC:By-line`,
  `IPTC:CopyrightNotice`, `XMP-dc:Creator`, `XMP-dc:Rights`,
  `XMP-xmpRights:Marked=True`, `XMP-xmpRights:UsageTerms`,
  `XMP-xmpRights:WebStatement`, `XMP-cc:License`
- Idempotent: re-running on `*.watermarked.jpg` or on any file with the
  `XMP:WatermarkApplied=true` sentinel is a no-op (`--force` to override)
- Composable: every parameter exposed as both CLI flag and config-file key,
  with deterministic precedence (CLI > `--config FILE` > `~/.config/watermark/defaults.conf` > `/etc/watermark/defaults.conf` > built-in defaults)
- Profiles: named preset bundles (`--profile portfolio`, `--profile blog`)
  let client projects pick a config without writing flags every time
- Stable exit codes and output (prints absolute path of produced file to
  stdout) so shell wrappers can chain reliably
- Delivered via an optional Ansible playbook installable on any
  fedora-desktop host; not in main install path

## Non-Goals

- Not building a batch processor — `watermark` takes ONE image at a time;
  parallel/batch is the wrapper's job (`xargs -P`, GNU parallel, etc.)
- Not building a GUI
- Not bundling a default logo or copyright text — config must be supplied
  by the user (no opinionated identity)
- Not a steganographic / invisible / DCT-frequency-domain watermark in v1
  (called out as decision gate Phase 6 — separate plan if pursued)
- Not RAW-format input (NEF/CR2/etc.) — JPEG and PNG only; users convert
  with their RAW editor first
- Not modifying RapidRAW, darktable, or other photo editors to call
  `watermark` automatically — that's downstream wrapper territory
- Not a library / sourceable bash file — CLI-only surface (decision below)
- Not video watermarking (different toolchain, different concerns)

## Context & Background

### Tooling research

| Tool          | Fedora package        | Role                                                 |
| ------------- | --------------------- | ---------------------------------------------------- |
| ImageMagick 7 | `ImageMagick`         | Visible watermark composition, JPEG re-encoding      |
| exiftool      | `perl-Image-ExifTool` | Read/write EXIF/IPTC/XMP metadata; idempotency probe |
| DejaVu Sans   | `dejavu-sans-fonts`   | Default font (already on Fedora desktop)             |

`perl-Image-ExifTool` is already installed by `play-photography.yml:39`.
ImageMagick is NOT currently installed by any tracked playbook (gimp does
not pull it). This plan adds it explicitly. ImageMagick 7 uses the
`magick` binary (not `convert`); the wrapper must call `magick`.

### EXIF/IPTC/XMP fields for commercial licence (research)

The standard set used by stock photo sites and photo-management tools.
exiftool can write all of these in a single invocation:

| Field                        | Purpose                                                |
| ---------------------------- | ------------------------------------------------------ |
| `EXIF:Artist`                | Photographer / creator name (legacy EXIF)              |
| `EXIF:Copyright`             | Short copyright notice                                 |
| `IPTC:By-line`               | Artist, IPTC standard                                  |
| `IPTC:CopyrightNotice`       | Full copyright text                                    |
| `XMP-dc:Creator`             | Dublin Core creator (modern equivalent of Artist)      |
| `XMP-dc:Rights`              | Dublin Core rights (modern copyright statement)        |
| `XMP-dc:Title`               | Optional title for the work                            |
| `XMP-xmpRights:Marked`       | Boolean: `True` = copyrighted, `False` = public domain |
| `XMP-xmpRights:UsageTerms`   | Human-readable licence summary                         |
| `XMP-xmpRights:WebStatement` | URL to full licence terms                              |
| `XMP-cc:License`             | Creative Commons licence URL (when applicable)         |
| `XMP-plus:LicensorURL`       | PLUS-spec licensor URL (commercial extension)          |

**Custom sentinel** (idempotency):

| Field                    | Value                                   |
| ------------------------ | --------------------------------------- |
| `XMP:WatermarkApplied`   | `True`                                  |
| `XMP:WatermarkAppliedAt` | ISO 8601 UTC timestamp                  |
| `XMP:WatermarkAppliedBy` | `watermark/<version>` (tool identifier) |

These three custom XMP tags need either a custom XMP namespace or use of
`XMP-x:` (already-present generic XMP namespace exiftool exposes). Decision
2 below.

### Two-layer visible watermark technique (research)

Single corner watermark loses to cropping; full-coverage opaque watermark
looks crap. The compromise:

1. **Corner mark** — small (`-pointsize "%[fx:w*0.025]"`, ~2.5% of width),
   southeast gravity, white text + 1px black stroke at 70% opacity.
   Readable, unobtrusive.
2. **Diagonal tile layer** — same text, ~30° rotation, repeated as a tile
   across whole image at ~8% opacity. Almost invisible at viewing
   distance, defeats crop-and-go, annoying to clone-stamp.

Both layers can be composited in a single `magick` invocation using
`-tile` for the diagonal layer and `-gravity southeast` for the corner.
Aspect-ratio handling: gravity selection (`southeast` for landscape,
`south` for portrait, `centre` panorama-aware) drives where the corner mark
lands; the diagonal tile is orientation-agnostic.

### Idempotency: dual-channel signal

| Signal                    | Channel        | Survives                             | Used for                              |
| ------------------------- | -------------- | ------------------------------------ | ------------------------------------- |
| `.watermarked.jpg` suffix | filename       | rename (no), move (yes)              | shell-loop skip, human visibility     |
| `XMP:WatermarkApplied`    | metadata (XMP) | rename (yes), move (yes), copy (yes) | tool-level skip, authoritative source |

Both are checked; a positive on either skips re-watermarking. The metadata
wins on disagreement (e.g., a renamed file keeps its mark even if the
suffix is gone).

### Composability surface

The user's stated requirement is "composable and extensible/wrappable by
client projects on the desktop". The chosen surface:

1. **CLI-first** — every parameter is a flag, exit codes are stable
   (0 = ok, 2 = arg error, 3 = idempotency-skip, 4 = magick failure,
   5 = exiftool failure), stdout prints absolute path of output file
2. **Config files with precedence chain**: built-in defaults →
   `/etc/watermark/defaults.conf` → `~/.config/watermark/defaults.conf` →
   `--config FILE` → CLI flags. Each layer is shell-sourced (`KEY=VALUE`).
3. **Profiles** — `--profile NAME` selects a `[NAME]` config block from
   the active config file. Lets a client project ship one
   `~/.config/watermark/defaults.conf` with multiple presets and pick one
   per call.
4. **Stdin batch mode** (decision gate, Phase 3): read filenames from
   stdin, one per line, process each, print results. Likely YES — minimal
   complexity, large composability win for `find -print0 | watermark --stdin0`.

### Where similar tooling lives in this repo

- `files/usr/local/bin/compress`, `files/usr/local/bin/uncompress` — bash
  CLI wrappers around a single ImageMagick-equivalent tool (`ouch`),
  installed via `play-compression-helpers.yml`. Closest pattern to follow:
  bash, fail-fast, single binary delivered via `/usr/local/bin/`, opinionated
  defaults, predictable arg parsing.
- `playbooks/imports/optional/common/play-photography.yml` — already
  installs `perl-Image-ExifTool` for metadata work and `darktable` /
  `rawtherapee` / `gimp` for raster work. The new playbook complements,
  does not replace.

## Tasks

### Phase 1: Research & decision gates

- [ ] ⬜ **Task 1.1**: Verify ImageMagick 7 is the version in Fedora 43
  (`dnf info ImageMagick`); confirm `magick` binary path and security
  policy file location (`/etc/ImageMagick-7/policy.xml`)
- [ ] ⬜ **Task 1.2**: Confirm exiftool version supports the full XMP
  field set listed above (`exiftool -listg1 | grep -i rights`)
- [ ] ⬜ **Task 1.3**: Resolve open decisions (see Technical Decisions
  section): config file format, sentinel namespace, stdin batch mode,
  output naming when `--output` is passed
- [ ] ⬜ **Task 1.4**: Write a one-liner reference test: a single
  `magick` + `exiftool` chain that produces the desired result on one
  test image — lock the recipe before scripting

### Phase 2: Core script — `watermark` CLI

- [ ] ⬜ **Task 2.1**: Write `files/usr/local/bin/watermark` (bash,
  `set -euo pipefail`, modeled on `files/usr/local/bin/compress`)
  - [ ] ⬜ Argument parser: `--text`, `--logo`, `--copyright`,
    `--artist`, `--licence-url`, `--licence-summary`, `--profile NAME`,
    `--config FILE`, `--output PATH`, `--force`, `--dry-run`,
    `--verbose`, `--no-tile`, `--no-corner`, `-h|--help`
  - [ ] ⬜ Config precedence chain: load `/etc/watermark/defaults.conf`,
    then `~/.config/watermark/defaults.conf`, then `--config FILE`,
    then apply CLI overrides; resolve `--profile` against the merged config
  - [ ] ⬜ Validate: input file exists, is JPEG or PNG (by extension AND
    `file --mime-type`), copyright/artist/licence-url are non-empty
  - [ ] ⬜ Idempotency probe: read `XMP:WatermarkApplied` via exiftool;
    if `True` and no `--force`, print "already watermarked" + path,
    exit 3
  - [ ] ⬜ Filename suffix probe: if input matches `*.watermarked.jpg`
    and no `--force`, exit 3
  - [ ] ⬜ Compose visible watermark: single `magick` invocation,
    aspect-ratio-aware gravity for corner, diagonal tile layer,
    sizes as `%[fx:w*N]` percentages
  - [ ] ⬜ Write metadata: single `exiftool -overwrite_original`
    invocation setting all licence fields plus the three sentinel tags
  - [ ] ⬜ Output filename: `${input%.*}.watermarked.${ext}` by default,
    `--output PATH` override; refuse to overwrite existing output unless
    `--force`
  - [ ] ⬜ Print absolute path of output file to stdout on success;
    errors to stderr; exit codes documented in `--help`
  - [ ] ⬜ `--dry-run` prints the magick + exiftool commands it would run,
    then exits 0
  - [ ] ⬜ Shellcheck-clean
- [ ] ⬜ **Task 2.2**: Write a small test fixture: a generated test image
  (`magick -size 1920x1080 plasma: /tmp/wm-test.jpg`) plus a script that
  exercises the golden path, the idempotency-skip path, the `--force`
  path, and the missing-required-config path. Lives in
  `tests/watermark/test_watermark.bash` (consistent with other repo tests)
- [ ] ⬜ **Task 2.3**: Run the fixture; resolve any visible-watermark
  legibility issues (font size, opacity, rotation angle) by inspection on
  a handful of varied real images (landscape, portrait, panorama, dark,
  bright)

### Phase 3: Config and composition surface

- [ ] ⬜ **Task 3.1**: Define the config-file format (decision below) and
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
  decision is YES (likely; small surface, large win), else exclude

### Phase 4: Ansible playbook

- [ ] ⬜ **Task 4.1**: Create
  `playbooks/imports/optional/common/play-image-watermarking.yml`
  - [ ] ⬜ Standard playbook header (`#!/usr/bin/env ansible-playbook`,
    `hosts: desktop`, `become: true`, `root_dir: ...`)
  - [ ] ⬜ Install packages: `ImageMagick`, `perl-Image-ExifTool`,
    `dejavu-sans-fonts` (idempotent — exiftool already present from
    `play-photography.yml`, will no-op)
  - [ ] ⬜ Preflight assert: `magick --version` runs and reports
    ImageMagick 7.x; if not, fail with actionable message
  - [ ] ⬜ Deploy `files/usr/local/bin/watermark` to `/usr/local/bin/`,
    mode `0755`, owner/group `root`
  - [ ] ⬜ Deploy `files/etc/watermark/defaults.conf.example` to
    `/etc/watermark/defaults.conf.example`, mode `0644`,
    owner/group `root` (NOT to user home; user copies to
    `~/.config/watermark/defaults.conf` themselves with their data)
  - [ ] ⬜ Create `/etc/watermark/` directory with mode `0755`
  - [ ] ⬜ Display message at end: pointer to
    `/etc/watermark/defaults.conf.example`, instruction to copy and edit,
    `watermark --help` reference
  - [ ] ⬜ Make playbook executable (`chmod +x`)
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
  **This is a decision gate, not a commitment** — split into Plan 00038
  if pursued.

## Technical Decisions

### Decision 1: Bash vs Python

**Context**: Repo uses both. Compress/uncompress are bash; speech-to-text
helpers are Python. Watermark is a thin orchestrator over `magick` + `exiftool`.

**Options**:

1. Bash — matches `compress`/`uncompress` precedent, no runtime deps
2. Python — easier config-file parsing, would pull in `pyexiftool` or
   shell out anyway

**Decision**: **Bash**. The script is fundamentally argument parsing +
two external command invocations + a config-file load. Python adds a
runtime dep with no win. Config can be a sourceable bash file (next
decision) — simpler than INI/TOML/YAML parsing in bash.
**Date**: 2026-04-28

### Decision 2: Config file format

**Context**: Need per-user defaults and named profiles. Bash-native vs INI.

**Options**:

1. Sourceable bash (`KEY=VALUE`, profiles via `[blog]` shell-function blocks
   or per-profile files like `~/.config/watermark/profiles/blog.conf`)
2. INI (parseable by `awk`/`crudini`)
3. TOML (would require a parser binary)

**Decision**: **Per-profile sourceable bash files** at
`~/.config/watermark/profiles/<name>.conf`. The "default" profile is
`~/.config/watermark/defaults.conf`. Each file is a flat set of `KEY=VALUE`
exports, sourced by the script in a sub-shell to avoid env pollution.
Simpler than INI parsing, no external deps, and `--profile blog` maps to
`~/.config/watermark/profiles/blog.conf` with zero ambiguity. Trade-off:
no INI tooling support (e.g., editors highlighting INI sections), accepted.
**Date**: 2026-04-28

### Decision 3: Sentinel XMP namespace

**Context**: The custom sentinel tags (`WatermarkApplied`,
`WatermarkAppliedAt`, `WatermarkAppliedBy`) need a namespace.

**Options**:

1. Reuse `XMP-x:` (generic XMP, exiftool writes it freely without config)
2. Define a custom namespace (e.g., `XMP-watermark:`) — requires an
   exiftool config file deployed alongside the binary
3. Stash everything in `XMP-dc:Description` as a parseable string

**Decision**: **Option 1 — `XMP-x:`** for v1. It's the lowest-friction
path; exiftool writes to it without configuration. If a later use case
needs cleaner namespacing, migrate to a deployed exiftool config (Option 2)
in a follow-up. Tags written: `XMP-x:WatermarkApplied`,
`XMP-x:WatermarkAppliedAt`, `XMP-x:WatermarkAppliedBy`.
**Date**: 2026-04-28

### Decision 4: stdin batch mode

**Context**: User said "composable and extensible/wrappable". A
single-image CLI plus `xargs` covers most batch needs, but `--stdin0`
(read NUL-delimited paths) makes `find ... -print0 | watermark --stdin0`
trivial.

**Options**:

1. CLI-only single-image; users wrap with `xargs -0 -n1`
2. Add `--stdin0` reading NUL-delimited paths

**Decision**: **Add `--stdin0`** in Phase 3. ~10 lines of bash; large
ergonomic win for batch wrappers; doesn't constrain the CLI design (each
input is still processed independently, exit codes apply per-file with a
final summary). If it adds unforeseen complexity during Phase 3
implementation, drop and document the `xargs` workaround instead.
**Date**: 2026-04-28

### Decision 5: Visible watermark — opacity, font size, tile angle

**Context**: Hardcoded constants in the script, or fully configurable?

**Options**:

1. Hardcoded "good defaults" (corner 70% opacity at 2.5% width, tile 8%
   opacity at 30°), no flags
2. Fully configurable per-flag
3. Hardcoded defaults, configurable via config file (no CLI flags)

**Decision**: **Option 3**. The defaults are good. CLI flags would bloat
the surface (8+ extra options); config-file keys
(`opacity_corner=0.7`, `opacity_tile=0.08`, `tile_angle=30`,
`size_pct=0.025`) keep the CLI focused on per-image semantics and let
profile authors tune presentation once. Phase 1 Task 1.4 locks the actual
default values via image inspection.
**Date**: 2026-04-28

### Decision 6: Output naming when `--output PATH` is passed

**Context**: Default output is `INPUT.watermarked.EXT`. If user passes
`--output foo.jpg`, do we honour it verbatim, or force the
`.watermarked.` suffix?

**Options**:

1. Honour `--output` verbatim; user is responsible for the suffix
2. Force `.watermarked.` insertion (e.g., `--output foo.jpg` becomes
   `foo.watermarked.jpg`)
3. Refuse `--output` paths that lack `.watermarked.`

**Decision**: **Option 1 — honour verbatim**. The metadata sentinel is the
authoritative idempotency channel; the filename suffix is a convenience.
Forcing the suffix into a user-supplied path is surprising. The XMP
sentinel still protects against double-watermarking. Document the
trade-off in `--help`.
**Date**: 2026-04-28

## Success Criteria

- [ ] `watermark image.jpg` produces `image.watermarked.jpg` with both
  visible layers and full licence metadata, in one invocation
- [ ] All required EXIF/IPTC/XMP fields present in output, verified via
  `exiftool -G1 -a image.watermarked.jpg`
- [ ] Custom sentinel tags (`XMP-x:WatermarkApplied`, `…AppliedAt`,
  `…AppliedBy`) present
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

## Risks & Mitigations

| Risk                                                                                                 | Impact | Probability | Mitigation                                                                                                                              |
| ---------------------------------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| ImageMagick `policy.xml` blocks JPEG read/write under default Fedora policy                          | High   | Low         | Phase 1 Task 1.1 verifies; if blocked, document the fix (relax `coder` policy for JPEG/PNG only) but do not auto-modify system file     |
| Visible watermark looks bad on edge cases (very dark images, very small images, mostly-white images) | Med    | Med         | Phase 2 Task 2.3 inspects a varied set; add a `--invert` flag if light/dark contrast becomes an issue                                   |
| Re-encoding JPEG for the watermark loses noticeable quality                                          | Med    | Med         | Hardcode `-quality 95` (config override available); document that the watermarked file is for distribution, originals are preserved     |
| ImageMagick 7 vs 6 syntax drift (`magick` vs `convert`) breaks on hosts with old IM6                 | High   | Very Low    | Preflight assert in playbook; Fedora 43 ships IM7                                                                                       |
| User loses originals because they pointed `--output` at the input path                               | High   | Low         | Refuse if `--output` resolves to the same realpath as input; covered in Phase 2 arg validation                                          |
| Config file precedence becomes confusing                                                             | Low    | Med         | Document precedence in `--help`; add `--show-config` flag (Phase 2 sub-task) that prints the resolved merged config and exits 0         |
| `--stdin0` partial-failure behaviour ambiguous (one file fails, do remaining proceed?)               | Med    | Med         | Define explicit policy: continue on per-file failure, print summary at end (`N succeeded, M skipped, K failed`), exit non-zero if K > 0 |
| `XMP-x:` sentinel namespace collides with another tool                                               | Low    | Very Low    | Use distinctive tag names (`WatermarkApplied`, etc.); migrate to custom namespace if collision is ever observed                         |

## Timeline

- Phase 1: Research & decision gates
- Phase 2: Core `watermark` script
- Phase 3: Config and composition surface
- Phase 4: Ansible playbook
- Phase 5: QA, host deploy, verification
- Phase 6 (decision gate): Invisible/DCT watermark — separate plan if pursued

## Notes & Updates

### 2026-04-28

- Plan created. Research confirms ImageMagick + exiftool are the right
  tools and both are in Fedora repos; exiftool already present via
  `play-photography.yml`. Visible-watermark approach is two-layer
  (corner + faint diagonal tile) to balance aesthetics vs
  removal-resistance. Idempotency uses dual signal: `.watermarked.jpg`
  filename suffix (human-visible) and `XMP-x:WatermarkApplied=true`
  metadata sentinel (authoritative). Composability surface: CLI-first
  with config precedence chain and named profiles via per-profile
  config files. Steganographic / DCT-domain invisible watermarking is
  parked as Phase 6 decision gate, deliberately out of v1 scope.

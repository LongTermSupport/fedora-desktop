# Plan 00037 — Research: tooling, metadata fields, visible-mark technique

Durable research behind the watermarking toolkit. Decisions that rest on it are
in [DECISIONS.md](DECISIONS.md); the task tree is in [PLAN.md](PLAN.md); the
full original prose (including the dated progress notes) is preserved in
[PLAN_archive.md](PLAN_archive.md).

## Tooling

| Tool          | Fedora package        | Role                                                 |
| ------------- | --------------------- | ---------------------------------------------------- |
| ImageMagick 7 | `ImageMagick`         | Visible watermark composition, JPEG re-encoding      |
| exiftool      | `perl-Image-ExifTool` | Read/write EXIF/IPTC/XMP metadata; idempotency probe |
| DejaVu Sans   | `dejavu-sans-fonts`   | Default font (already on Fedora desktop)             |

`perl-Image-ExifTool` is already installed by `play-photography.yml`.
ImageMagick is NOT installed by any other tracked playbook (gimp does not
pull it), so the watermarking playbook adds it explicitly. ImageMagick 7 uses
the `magick` binary (not `convert`); the wrapper must call `magick`.

## EXIF/IPTC/XMP fields for a commercial licence

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

### Custom sentinel (idempotency)

Written under the `XMP-wm:` custom namespace registered via a deployed
exiftool config file (see Decision 3 in [DECISIONS.md](DECISIONS.md)):

| Field              | Value                                                     |
| ------------------ | --------------------------------------------------------- |
| `XMP-wm:Applied`   | `True`                                                    |
| `XMP-wm:AppliedAt` | UTC timestamp in exiftool format (`YYYY:mm:dd HH:MM:SSZ`) |
| `XMP-wm:AppliedBy` | `watermark/<version>` (tool identifier)                   |

The `XMP-wm` namespace is defined by `files/etc/watermark/exiftool.config`
(deployed by the playbook), with namespace URL
`https://example.com/ns/watermark/1.0/` (a generic placeholder: public repo,
no personal domain; the URL is an opaque identifier and need not resolve).
Reusing `XMP-x:` was disproven during the recipe-lock prototype: exiftool
warns `Tag 'XMP-x:WatermarkApplied' is not defined` and silently drops
unknown user tags under that namespace.

**exiftool date-format gotcha**: writes reject ISO 8601 dashes
(`2026-04-28T15:39:39Z`) with
`Warning: Invalid date/time (use YYYY:mm:dd HH:MM:SS[.ss][+/-HH:MM|Z])`.
Use `date -u +'%Y:%m:%d %H:%M:%SZ'` (colons in the date portion).

## Two-layer visible watermark technique

Single corner watermark loses to cropping; full-coverage opaque watermark
looks bad. The compromise:

1. **Corner mark** — small (`-pointsize "%[fx:w*0.025]"`, ~2.5% of width),
   southeast gravity, white text + 1px black stroke at 70% opacity.
   Readable, unobtrusive.
2. **Diagonal tile layer** — same text, ~30° rotation, repeated as a tile
   across whole image at ~8% opacity. Almost invisible at viewing
   distance, defeats crop-and-go, annoying to clone-stamp.

Both layers composite in a single `magick` invocation: a 400×400 `mpr:tile`
register holding the rotated faint text, drawn across the canvas with
`-fill mpr:tile -draw 'color 0,0 reset'`, then a high-contrast
southeast-gravity corner annotate. Quality 95 JPEG output. Aspect-ratio
handling: gravity selection (`southeast` for landscape, `south` for portrait,
`centre` panorama-aware) drives where the corner mark lands; the diagonal
tile is orientation-agnostic.

### Recipe-lock evidence (Task 1.4)

The recipe was locked end-to-end in the dev container on ImageMagick
7.1.2-21 (the Debian 12 container ships IM6 only, so the upstream AppImage
was extracted to `/opt/imagemagick7/` with a `magick` wrapper exporting
`LD_LIBRARY_PATH` and `MAGICK_CONFIGURE_PATH`; container-only, the Fedora
target ships IM7 natively). All 12 metadata tags round-trip cleanly on an
`exiftool -G1 -s` readout: `EXIF:Artist`, `EXIF:Copyright`, `IPTC:By-line`,
`IPTC:CopyrightNotice`, `XMP-dc:Creator`, `XMP-dc:Rights`,
`XMP-xmpRights:Marked`, `XMP-xmpRights:UsageTerms`,
`XMP-xmpRights:WebStatement`, `XMP-wm:Applied`, `XMP-wm:AppliedAt`,
`XMP-wm:AppliedBy`.

## Idempotency: dual-channel signal

| Signal                    | Channel        | Survives                             | Used for                              |
| ------------------------- | -------------- | ------------------------------------ | ------------------------------------- |
| `.watermarked.jpg` suffix | filename       | rename (no), move (yes)              | shell-loop skip, human visibility     |
| `XMP-wm:Applied`          | metadata (XMP) | rename (yes), move (yes), copy (yes) | tool-level skip, authoritative source |

Both are checked; a positive on either skips re-watermarking. The metadata
wins on disagreement (e.g., a renamed file keeps its mark even if the
suffix is gone).

## Composability surface

The stated requirement is "composable and extensible/wrappable by client
projects on the desktop". The chosen surface:

1. **CLI-first** — every parameter is a flag, exit codes are stable
   (0 = ok, 2 = arg error, 3 = idempotency-skip, 4 = magick failure,
   5 = exiftool failure), stdout prints absolute path of output file.
2. **Config files with precedence chain**: built-in defaults →
   `/etc/watermark/defaults.conf` → `~/.config/watermark/defaults.conf` →
   `--profile NAME` (resolves to `~/.config/watermark/profiles/<name>.conf`) →
   `--config FILE` → CLI flags. Each layer is shell-sourced (`KEY=VALUE`).
   A NIL sentinel pattern distinguishes "user passed empty" from "user did
   not pass".
3. **Profiles** — `--profile NAME` selects a per-profile config file so a
   client project can ship several presets and pick one per call.
4. **Stdin batch mode** (`--stdin0`, Decision 4): read NUL-delimited
   filenames from stdin, process each, print results.

## Where similar tooling lives in this repo

- `files/usr/local/bin/compress`, `files/usr/local/bin/uncompress` — bash
  CLI wrappers around a single tool (`ouch`), installed via
  `play-compression-helpers.yml`. Closest pattern to follow: bash,
  fail-fast, single binary delivered via `/usr/local/bin/`, opinionated
  defaults, predictable arg parsing.
- `playbooks/imports/optional/common/play-photography.yml` — already
  installs `perl-Image-ExifTool` for metadata work and `darktable` /
  `rawtherapee` / `gimp` for raster work. The new playbook complements,
  does not replace.

## Delivered artefacts (container-side state at the time of slimming)

- `files/usr/local/bin/watermark` — bash, `set -euo pipefail`,
  shellcheck-clean; full CLI surface and config precedence chain as above.
- `files/etc/watermark/defaults.conf.example` — sourceable bash format with
  semantic keys (artist, copyright, licence_url, licence_summary, text) and
  presentation tuning (font, opacity_corner, opacity_tile, tile_angle,
  size_pct_corner, size_pct_tile, quality).
- `files/etc/watermark/exiftool.config` — registers the `XMP-wm:` namespace.
- `playbooks/imports/optional/common/play-image-watermarking.yml` —
  installs `ImageMagick`, `perl-Image-ExifTool`, `dejavu-sans-fonts`;
  preflight asserts IM7.x; deploys the three files; creates
  `/etc/watermark/`.

Container smoke tests passed for: golden path, filename-suffix idempotency,
XMP-sentinel idempotency on a renamed file, fail-fast on a missing required
arg, `--dry-run`, `--show-config`, `--force` overwrite, `--config` file
values used, and CLI override of `--config` values. `./scripts/qa-all.bash`
passed. Host deploy and host smoke test (Phase 5) remain.

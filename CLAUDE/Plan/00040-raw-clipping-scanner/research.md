# Research: raw clipping scanner (`clip-scan`)

Captured 2026-05-20. Backing document for Plan 00040.

Headline recommendation (final, post-iteration): **Python script using `rawpy` (LibRaw binding) that decodes the raw Bayer mosaic and computes a *weighted clipping score* per side — pixels close to sensor saturation / black level contribute proportionally to a per-image score; files whose score exceeds a configured threshold get `.wclip` / `.bclip` sub-extensions in their filename. Final defaults: 2.0% weighted score for white, 5.0% weighted score for black, with linear ramps starting at 0.95 of `white_level` and 1.05 of `black_level`.**

This document records the **iteration history** that arrived at that design — sections 2 and onward describe the earlier simple-threshold and two-axis-knob designs, with Section 3 introducing the final weighted-score refinement and the prior art that supports it. Reading top-to-bottom shows the reasoning chain; the design ships from Section 3.

## Problem statement

The user shoots Sony ARW (RAW) + JPG pairs on the A7V. Workflow today is:

1. Camera dumps to FTP via `ftp-camera`
2. Files are sorted into `JPG/` and `RAW/` directories by date
3. User imports to Lightroom for selects/edits

The pain: when a 500-photo shoot includes 30+ blown-highlight frames or 50+ crushed-shadow frames, manual triage in Lightroom is slow. A preprocess step that *flags* (not deletes) bad-exposure frames by **filename rename** would let Lightroom users filter on filename in the catalog view and rapidly reject or down-rate them.

Specifically, the user wants:

- `DSC123.ARW` → `DSC123.wclip.ARW` when >N% of pixels are at sensor saturation
- `DSC123.ARW` → `DSC123.bclip.ARW` when >M% of pixels are at sensor black level
- Both flags possible: `DSC123.wclip.bclip.ARW` (wclip always first)
- Sibling JPG (and any future XMP/HIF) renamed in lockstep
- Standalone CLI; may be wired into `ftp-camera` later (Plan 00039)
- The output is **NOT sidecars** — the user is explicit that filename is the channel, because filename-based filtering is dead simple in any tool (LR text filter, shell glob, file manager search)

Critical clarification from the user during planning: **this is a pre-Lightroom-import step**. That means the canonical concern with renaming (existing catalogs see files as "missing") is moot. The renamed file is what Lightroom sees on its first import, and that's the name it records in its database forever.

## 1. Existing tool landscape

Surveyed at conversation start; recap here for the record.

| Tool                                 | What it does                                                            | Why it doesn't fit                                                                                 |
| ------------------------------------ | ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| AfterShoot / FilterPixel / Narrative | AI-based bulk culling: focus, eyes-closed, duplicates, exposure         | Subscription. Clipping threshold is an AI black box, not user-tunable in % terms.                  |
| darktable / RawTherapee              | Interactive raw editors; show clipping warnings while you edit one file | Not bulk-flag-by-threshold tools; no "% pixels clipped" numeric API.                               |
| digiKam                              | Asset manager with optional image-quality scorer (blur, exposure)       | Quality scorer is heuristic and not parameterised by clip-%; output is DB metadata, not filenames. |
| Photo Mechanic                       | Fast browser/cull tool                                                  | Purely manual; no auto-analysis.                                                                   |
| ExifTool                             | Metadata reader/writer                                                  | Doesn't read pixel data; can't compute histograms.                                                 |

The gap this tool fills: **user-tunable numeric clip-% threshold, with simple filename output**. Nothing in the market hits that exact combination.

## 2. Threshold defaults: two-axis design

The tool exposes **two knobs per side**, not one:

| Axis                | White flag              | Black flag              |
| ------------------- | ----------------------- | ----------------------- |
| **Value cutoff**    | `--white-cutoff 0.98`   | `--black-cutoff 1.03`   |
| **Count threshold** | `--white-threshold 2.0` | `--black-threshold 5.0` |

The **cutoff** controls "how close to sensor saturation / black level counts as clipped." The **threshold** controls "how many such pixels are tolerated before the file is flagged."

This matters because a frame with zero pixels at literal `white_level` (16383) but 8% of pixels at 16200+ is photographically blown — and a strict "must equal max" definition would miss it entirely. The tone curve compresses the top stop hard; the difference between 99% and 100% of saturation is invisible in the final image.

### Why 0.98 white-cutoff default

- The top ~1% of the raw range has noise on the order of the quantisation step
- 0.98 gives one stop of margin for sensor noise without being so lenient it catches genuine highlight detail
- Matches in-camera "blinkies" thresholds on Sony/Canon/Nikon (~98-100% trigger)
- Matches Adobe Lightroom's "near-clipped" indicator behaviour

### Why 1.03 black-cutoff default

- The dark-current pedestal has ±10-20 levels of read noise around the reported `black_level`
- 1.03 means "within ~3% above black_level" — i.e. effectively at the noise floor
- More forgiving than the white side because shadow noise is naturally wider than highlight noise
- A pixel at black_level + 1 is indistinguishable from black_level in any downstream output

### Why 2% white-clip (count threshold)

A 14-bit Sony ARW has a sensor saturation value reported by LibRaw as `white_level` (typically ~16383 for uncompressed, slightly lower for cRAW). A pixel at or above that value has *no recoverable highlight detail*.

**Photographer convention** (sources: Adobe LR highlight-clipping indicator, in-camera zebra warnings on Sony/Canon/Nikon, the dpreview "blown highlights" consensus):

- Camera zebra warnings trigger at ~95-100 IRE; the equivalent in raw is ~98%+ of saturation
- Adobe Lightroom's red histogram triangle illuminates at ~0.1% clipped — that's the "is anything clipped at all" canary
- Photographer judgement starts caring at ~1% in skies, ~2% across whole-image
- "Severely blown" is conventionally 5%+

**Why 2% specifically**:

- 1% is too tight — almost every outdoor scene with sun glints, chrome reflections, eye catchlights, or specular metal triggers
- 5% is too loose — a blown sky covering the top quarter of the frame still passes (skies are often ~10% of pixels and clipping 25% of those = 2.5% total)
- 2% is the sweet spot for "clipping that costs you actual scene detail, not just specular twinkle"

This number can be over-ridden with `--white-threshold`, so the default is just the opinionated starting point.

### Why 5% black-clip

Black clipping is **structurally less damaging** than highlight clipping in raw:

- 14-bit raw has ~5 stops of usable shadow recovery before noise dominates
- Shadow detail is recoverable; highlight detail above saturation is mathematically gone
- Photographers routinely *intend* crushed blacks: silhouettes, low-key portraits, black backgrounds
- Lens vignetting can drive corners to true black even on technically well-exposed frames

So the threshold for "this is genuinely a problem" sits higher: 5% of pixels at the camera's reported black level. This catches "subject's hair is a black blob" and "shadow side of face has no detail" while letting stylistic crushed-blacks pass.

Configurable via `--black-threshold`.

### Per-channel "any" vs "all"

A pixel is **clipped** if *any* RGB channel is at saturation (for white) or at black level (for black).

- "Any channel saturated" matches photographer perception: green clips first in daylight, red in sunsets, blue in studio strobe. Restricting to all-channels-saturated would miss colour-specific blowouts (the orange glow of a sunset that's just red-clipped, not white-clipped).
- "All channels saturated" is the specular-only definition. Way too lenient — wouldn't catch the most damaging clips (single-channel red sunset, blue water blown by polariser misjudgement).

### Black level is NOT zero

Raw pixel values from Sony ARW start above zero. The Sony A7V reports a black-level offset around 512 (out of 16383) — this is the dark-current pedestal, not a real measurement of zero light. LibRaw reports `black_level_per_channel` and we **must** use that, not assume 0, or we'd never flag anything as black-clipped.

### Saturation is NOT always 16383

Uncompressed 14-bit ARW saturates at 16383, but compressed cRAW (Sony's losslessly-compressed format) effectively saturates lower (~14600 due to the compression's range-mapping). LibRaw reports `white_level` per file — use that directly, never hardcode.

## 3. Weighted-score refinement (FINAL design — supersedes Section 2)

The two-axis design in Section 2 is a substantial improvement over a binary "% at exact saturation" count, but during planning iteration the user raised a subtler point:

> *"1 px of 100% is worth more than 1px of 98% so maybe for each 1% below max we reduce score by x and then the threshold is score based?"*

That observation collapses the two-axis design into a single weighted score, which is mathematically more honest about how the sensor + tone curve actually behave. **This is the design that ships.** Section 2 is preserved above as the reasoning step that led here.

### The math

For each pixel value `v` (per channel, in raw counts), compute a **weight** that is 1.0 at saturation, 0.0 at the cutoff, and linearly interpolated in between:

```python
# White-side weight, normalised so v=white_level → 1.0
def w_white(v, white_level, cutoff_ratio):
    v_norm = v / white_level
    if v_norm < cutoff_ratio:
        return 0.0
    return (v_norm - cutoff_ratio) / (1.0 - cutoff_ratio)

# Black-side weight, normalised so v=black_level → 1.0
def w_black(v, black_level, cutoff_ratio):
    # cutoff_ratio is a multiplier above black_level (e.g. 1.05 = "5% above")
    cutoff_value = black_level * cutoff_ratio
    if v > cutoff_value:
        return 0.0
    return (cutoff_value - v) / (cutoff_value - black_level)
```

The per-image **score** is the mean weight over all pixels, expressed as a percentage:

```python
score_white_pct = 100.0 * sum(w_white(v, ...) for v in pixels) / len(pixels)
score_black_pct = 100.0 * sum(w_black(v, ...) for v in pixels) / len(pixels)
```

The score has a clean interpretation: **"the equivalent percent of fully-clipped pixels."** A frame with 1% at literal saturation contributes `1.0`. A frame with 2% at the midpoint between cutoff and saturation contributes `1.0` (`2.0 × 0.5`). They score equally — which matches photographer perception (one severely-clipped pixel = two half-way-clipped pixels = equally bad).

Flag when `score_white_pct > white_threshold_pct` (or black equivalent). Two knobs total per side; one threshold, one cutoff defining the start of the weighted ramp.

### Per-channel handling

Compute the score **per channel** then take the *max* across R/G1/B/G2 (since "any channel clipped" matches the Section 2 conclusion). This way a sunset blown only in the red channel still scores high.

**Implementation detail**: rawpy returns `raw_image_visible` as a 2D Bayer mosaic (one channel per pixel) plus `raw_colors_visible` (same shape, contains the channel index 0..3 for each pixel) and `raw_pattern` (the 2x2 CFA pattern). To compute per-channel scores, mask the Bayer array via `np.where(raw_colors_visible == channel_idx)` to extract that channel's pixel pool, then apply the weighted-score math. On a Bayer sensor R and B pools are ~25% of total pixels each; the two G pools are ~25% each (G1 + G2 = ~50% combined).

**Per-channel black levels (subagent review H1)**: LibRaw exposes `black_level_per_channel` as a 4-tuple (R, G1, B, G2). These values are NOT always identical on Sony — some ISOs and some cameras report per-channel pedestals that differ by 2-10 units. The implementation MUST use the matching pedestal per channel pool, not a scalar collapsed via min/max/mean. Phase 3 has a specific test for this using a synthetic 4-tuple with non-identical values.

**Per-channel white level**: rawpy's `white_level` is a single scalar. LibRaw does have per-channel saturation values internally but doesn't expose them via rawpy; in practice they're equal on consumer cameras. Use the scalar directly for now.

### Defaults (final)

| Flag                   | Default | Meaning                                                                      |
| ---------------------- | ------- | ---------------------------------------------------------------------------- |
| `--white-cutoff RATIO` | 0.95    | Pixels with `value / white_level >= 0.95` contribute to the white-clip score |
| `--white-score PCT`    | 2.0     | Flag `.wclip` when weighted white-clip score exceeds 2.0%                    |
| `--black-cutoff RATIO` | 1.05    | Pixels with `value <= 1.05 × black_level` contribute to the black-clip score |
| `--black-score PCT`    | 5.0     | Flag `.bclip` when weighted black-clip score exceeds 5.0%                    |

Why the cutoff defaults moved from Section 2's 0.98 / 1.03 to 0.95 / 1.05:

- In the Section 2 two-axis design, the cutoff was a binary threshold: anything ≥ 0.98 counted equally. Setting it at 0.98 was tight to avoid catching genuine highlight detail.
- In the weighted-score design, the cutoff is the *start* of the ramp — pixels at the cutoff contribute 0, pixels at saturation contribute 1, intermediate values are linearly interpolated. So we can afford to start the ramp earlier (0.95) without over-flagging, because near-cutoff pixels barely add to the score.
- 0.95 white means "the top 5% of the raw range can contribute, weighted by proximity to saturation." That's the natural "near-blown" zone in photographic terms.
- 1.05 black means "values within 5% of the black-level pedestal can contribute, weighted by proximity." This covers the ±10-20-level read-noise zone around the Sony A7V's ~512 black-level.

Score thresholds (2% / 5%) stay at Section 2's values because the units are the same: percent. The interpretation is slightly different (now "weighted-equivalent percent" not "raw count percent"), and in practice the weighted score for a given file is *lower* than the raw count would be (most pixels in the ramp contribute \<1.0). So 2% / 5% are now slightly more forgiving, which is correct.

### Caveats on the defaults (subagent review)

The subagent review surfaced two issues with the proposed defaults that Phase 8 calibration must validate:

**(M1) 60MP A7V may under-flag**: The reference points from RawDigger / Adobe LR / camera blinkies were calibrated against 24MP-class sensors. On the 60MP A7V, a 2.0% score threshold means the *equivalent of ~1.2M pixels at saturation* — a substantial visible fraction of a frame. The threshold may need tightening for 60MP. Phase 8 calibration on a real shoot determines the right value.

**(M2) Kasson's matrix argument actually pushes for a TIGHTER cutoff, not looser**: The Kasson reference (see `references/kasson-see-spot-run.md`) argues that *raw* histograms appear less-clipped than the final image because the colour matrix can push raw values past saturation in three of the four channels during demosaic. This is an argument for being MORE aggressive (tighter cutoff = wider weighted zone) at flagging, not less. The move from 0.98 to 0.95 in this section is therefore consistent with Kasson's argument — going wider on the weighted zone catches the borderline-cases that would clip post-matrix even if they look OK in the raw histogram. Documented for the record; defaults stand.

### Optional: gamma

A `--gamma FLOAT` flag (default 1.0) can apply a power to the ramp:

```python
weight = ((v_norm - cutoff_ratio) / (1.0 - cutoff_ratio)) ** gamma
```

- `gamma=1.0`: linear ramp (default)
- `gamma=2.0`: quadratic — near-max pixels punish much more than near-cutoff
- `gamma=0.5`: square-root — near-cutoff pixels almost as bad as near-max

Default 1.0 is correct for v1. Gamma is left as a power-user knob.

### Prior art justification

The weighted-score design is novel for bulk-culling but builds on well-established prior art:

- **Mertens 2007 Exposure Fusion** (see `references/mertens-exposure-fusion.md`) uses a per-pixel weight derived from a Gaussian centered at mid-grey: `exp(-(I - 0.5)² / (2 × σ²))` with `σ = 0.2`. Mertens uses this to *down-weight* near-clipped pixels for HDR fusion. We **invert** the framing: pixels near the extremes get *up-weighted* because that's exactly the badness we're measuring.
- **RawDigger / FastRawViewer / Adobe LR** all use binary "at-saturation" counts (see `references/rawdigger-histograms.md`, `references/fastrawviewer-calibration.md`). No commercial tool implements a weighted score for bulk culling — the gap this fills.
- **ETTR practitioner guidance** (see `references/lensrentals-ettr.md`, `references/wikipedia-ettr.md`, `references/kasson-see-spot-run.md`) consistently distinguishes "specular highlights (acceptable to clip) vs important detail (must preserve)" — the weighted score's "below cutoff → zero contribution" zone is *exactly* the "specular highlights are noise floor" idea formalised.

### Score interpretation cheat sheet (for the user)

| Situation                                   | Approximate score |
| ------------------------------------------- | ----------------- |
| Properly exposed, no blown highlights       | < 0.1%            |
| Some specular highlights (sun glints, etc.) | 0.1 - 0.5%        |
| Bright sky with no detail in a corner       | 1.0 - 2.0%        |
| Blown sky covering top quarter of frame     | 2.0 - 5.0%        |
| Severely overexposed                        | > 5.0%            |

So `--white-score 2.0` flags everything from "noticeable sky blowout" upward, which matches the user's stated goal of "flag the bad ones, don't reject normal speculars."

## 4. Raw decode library: rawpy is the only choice

### rawpy (Python binding to LibRaw)

- `pip install rawpy` — wheels available for Linux x86_64
- Wraps LibRaw (a mature C library that handles all camera-specific raw decoding)
- `raw_image_visible` gives the Bayer-pattern sensor data as a 2D numpy array — NO demosaic, NO tone curve, just the raw sensor counts
- Exposes `black_level_per_channel`, `white_level`, `raw_colors` (the CFA pattern array)
- Decode time: 0.3-1.0s per ARW on SSD (cRAW slower than uncompressed)
- Peak memory: ~150MB per concurrent file during decompress

### Why not the alternatives

- **dcraw** (CLI): older, mostly unmaintained, harder to extract raw saturation/black via CLI alone
- **ImageMagick / GraphicsMagick**: read raw via dcraw delegate but only after demosaic + tone curve, which destroys the raw saturation information
- **rawkit** (Python): abandoned, last release 2017
- **LibRaw C/C++ direct**: massive overkill for a CLI

Verdict: rawpy. No competition.

### Fedora packaging

Verification needed on host: `dnf info python3-rawpy` (may not be in repos). Fallback: `pip install --user rawpy` (compiles wheel against system LibRaw). Playbook should handle either path with fail-fast on import.

## 5. Filename rename strategy

### Suffix ordering

Per user spec: `.wclip` always first, `.bclip` second.

| Input        | Verdict | Output                   |
| ------------ | ------- | ------------------------ |
| `DSC123.ARW` | None    | `DSC123.ARW` (unchanged) |
| `DSC123.ARW` | W only  | `DSC123.wclip.ARW`       |
| `DSC123.ARW` | B only  | `DSC123.bclip.ARW`       |
| `DSC123.ARW` | W and B | `DSC123.wclip.bclip.ARW` |

### Idempotency: strip-then-rebuild (trailing tokens only)

Re-running with a different threshold must produce the right output, not `DSC123.wclip.wclip.ARW`. Strategy:

1. Split filename on dots
2. Identify the file extension (last token)
3. **Strip only the *trailing* `wclip` / `bclip` tokens** — i.e. tokens immediately before the extension. Stop stripping the moment a non-clip token is encountered (subagent review H3 — stripping any-position tokens would silently corrupt a filename like `DSC123.bclip.report.ARW` where `bclip` is the user's own naming)
4. The remaining tokens form the canonical stem
5. Recompute clip flags
6. Append flags in canonical order (`wclip`, `bclip`) before the extension

Pseudocode (revised per H3):

```python
def canonical_stem(path: Path) -> tuple[str, str]:
    parts = path.name.split('.')
    ext = parts[-1]
    stem_parts = parts[:-1]
    # Strip ONLY trailing wclip/bclip tokens (not any-position)
    while stem_parts and stem_parts[-1].lower() in {'wclip', 'bclip'}:
        stem_parts.pop()
    return '.'.join(stem_parts), ext

def rebuild(stem: str, ext: str, w: bool, b: bool) -> str:
    suffixes = []
    if w: suffixes.append('wclip')
    if b: suffixes.append('bclip')
    return '.'.join([stem, *suffixes, ext])
```

This is also robust against filenames that legitimately contain dots in the stem (e.g. `Holiday.beach.DSC123.ARW`).

### Pair detection

A "photo" is a set of sibling files sharing a canonical stem. Common siblings:

- `DSC123.ARW` (raw)
- `DSC123.JPG` (in-camera JPG; Sony default)
- `DSC123.HIF` (HEIF, if shooting heif mode)
- `DSC123.xmp` or `DSC123.ARW.xmp` (sidecar metadata, if any tool wrote one)

The tool should:

1. Group files by canonical stem
2. Pick the **best analysis source** per group, in priority: ARW > HIF > JPG
3. Analyse that one file
4. Apply the verdict to **every** file in the group, renaming all of them in lockstep

Why ARW-first: raw sensor data is the truth about the scene's exposure. JPG has tone curves applied; analysing the JPG histogram tells you about Sony's tone mapping, not about whether the *scene* was clipped.

### XMP sidecar pattern

Lightroom XMP sidecars are typically named one of:

- `DSC123.xmp` (same stem, replaces extension)
- `DSC123.ARW.xmp` (appended after the raw extension)

The tool needs to recognise both and rename in step with the raw they describe. The user doesn't currently have these (camera doesn't produce them, ftp-camera doesn't either), but post-LR export does, and forward-compatibility is cheap.

### Edge cases

- **Mixed case extensions** (`ARW` vs `arw`): preserve case as-is on rename; match case-insensitively when grouping
- **Read-only files**: fail with a clear per-file error, continue with other files (one bad file shouldn't sink a batch)
- **Already-correctly-named files**: idempotency makes this a no-op
- **Symlinks**: by default, follow target and rename the target file; `--no-follow-symlinks` available to opt-out
- **Network mounts** (SMB/NFS): pathlib handles transparently; rename is server-side
- **Files actively held open** (rare; e.g. mid-FTP): rename will fail with EBUSY on some filesystems — log and continue
- **Filename collisions after rename**: if `DSC123.ARW` would become `DSC123.wclip.ARW` and `DSC123.wclip.ARW` already exists (from a prior run with different threshold), the second file gets the same canonical name — log warning, skip

## 6. Lightroom integration

### Filtering by filename suffix in Lightroom

The simplest filter: **Library module → Filter bar → Text → "Filename contains" → ".wclip."** or `.bclip.`.

Smart collections can encode this for repeated use: *Filename contains all `.wclip.`* — saved smart collection persists across sessions.

Workflow:

1. Import folder into LR (renamed filenames go in, LR records them as gospel)
2. Switch to Library view
3. Apply smart collection "Clipped highlights" to quickly select all `.wclip.` frames
4. Bulk-reject (X) or down-rate (1-star) without opening each
5. Repeat for `.bclip.` if shadow review is wanted

This gives the user a triage pass in seconds instead of minutes per shoot.

### Why filename-rename beats sidecars for this workflow

- **No tool dependency**: filename works in Finder, shell, file managers, ANY catalog tool, web viewers
- **Survives LR import unchanged**: LR uses the filename it sees; no special integration needed
- **Survives format/catalog changes**: switching from LR to Capture One? Filenames still tell you what's flagged. Switching to a fresh LR catalog? Still flagged.
- **Greppable**: `ls **/*.wclip.*` is faster than any catalog query
- **The user explicitly asked for this** and gave the rationale ("subsequent organisation easy somehow") — design respects user choice

The trade-off is filename volatility, but that's already moot because this runs *before* import.

## 7. Performance

Estimates revised after subagent review (#2 — original estimates were calibrated against 24MP uncompressed; A7V is 60MP and the default capture mode is cRAW).

### Single file — uncompressed ARW (24MP-class baseline; rare for A7V)

- ARW decode (libraw): 0.3-1.0s
- numpy histogram + threshold check: \<50ms
- Filename rename (atomic syscall): \<5ms
- **Total ~0.7s per uncompressed ARW**

### Single file — Sony cRAW (A7V default mode)

- cRAW decode is the bottleneck — LibRaw's compressed-raw decoder is well-known to be the slow path, especially at 60MP
- Estimated decode: 1.5-2.5s per file on a single core (per subagent #2)
- numpy + rename: same as above (negligible)
- **Total ~1.5-2.5s per cRAW** — Phase 2 host probe captures the actual number with `time python3 -c "import rawpy; r=rawpy.imread('test_craw.ARW'); _ = r.raw_image_visible.copy()"`

### A 500-photo cRAW shoot (A7V realistic workload)

- Single-threaded: 12-21 minutes
- 8-process pool: **~2-5 minutes** (revised from "50-75 seconds")
- IO-bound on slow disks; CPU-bound on fast SSD (typical case)
- The success criterion in PLAN.md was revised from "under 2 minutes" to "under 5 minutes" to reflect this

### Parallelism

`concurrent.futures.ProcessPoolExecutor` with `os.cpu_count()` workers is the natural fit. ProcessPool (not ThreadPool) because LibRaw's GIL behaviour through rawpy is unclear and process isolation is safer for decompression workloads.

Memory: 8 workers × ~150MB peak each = ~1.2GB during peak decode. Fine for any modern desktop.

### Sequencing

For pairs (ARW + JPG), only the ARW is decoded — the JPG verdict is inherited. Don't waste cycles decoding the JPG separately.

## 8. CLI surface (proposal — FINAL weighted-score design)

```
clip-scan [OPTIONS] [PATH]

Scans PATH (directory, default CWD) recursively for raw and image files,
analyses their clipping levels, and renames any that exceed thresholds
with .wclip and/or .bclip sub-extensions.

OPTIONS:
  -w, --white-score PCT       Flag .wclip when the weighted white-clip score
                              exceeds PCT (default: 2.0)
      --white-cutoff RATIO    Linear weighting ramp starts at value =
                              RATIO * white_level; pixels below this contribute
                              0 to the score (default: 0.95)
  -b, --black-score PCT       Flag .bclip when the weighted black-clip score
                              exceeds PCT (default: 5.0)
      --black-cutoff RATIO    Linear weighting ramp starts at value =
                              RATIO * black_level; pixels above this contribute
                              0 to the score (default: 1.05)
      --gamma FLOAT           Power applied to the ramp; 1.0 = linear,
                              >1 punishes near-max more (default: 1.0)
  -n, --dry-run               List proposed renames, don't apply (default)
  -a, --apply                 Actually perform renames
  -j, --jobs N                Parallel workers (default: nproc)
  -v, --verbose               Per-file analysis stats (per-channel scores)
      --no-follow-symlinks    Don't follow symlinks
      --json                  Emit machine-readable JSON output
  -h, --help                  Show this help
```

Dry-run-by-default is deliberate: renames are reversible but disruptive. The `--apply` flag forces conscious commitment.

`--json` exists so future integrations (ftp-camera, downstream tooling) can parse the output programmatically.

## 9. Future integration with ftp-camera

Plan 00039 (`ftp-camera-viewer-tui`) adds per-upload sort and live preview. A natural Phase-2 extension is `--async-copy-cull`: every sorted file is run through `clip-scan --apply --quiet <file>` immediately after sort, so by the time the shoot ends, the disk is already pre-flagged.

This requires the CLI to accept a single file argument (not just a directory). The proposed surface already supports this — `clip-scan path/to/DSC123.ARW` is just a recursion-of-one.

**Performance gotcha for single-file invocation (subagent review L5)**: spawning a fresh `clip-scan` process per upload pays ~1-3 seconds of Python + numpy + rawpy import overhead per file. For a batch run that's amortised. For ftp-camera's per-file invocation it dominates the actual analysis time (which is ~1.5s for a 60MP cRAW). The right shape for ftp-camera integration is therefore a **long-lived daemon mode** — e.g. `clip-scan --watch <dir>` or `clip-scan --serve` reading paths from stdin — that keeps the rawpy import warm across many files. v1 ships the basic batch CLI; the daemon shape is deferred to a Plan 00039 follow-up.

That extension is **out of scope for Plan 00040**. It will land as a Plan 00039 follow-up.

## 10. Implementation language & dependencies

**Python 3** with:

- `rawpy` (LibRaw binding; required) — `pip install rawpy`
- `numpy` (transitive via rawpy)
- `Pillow` (optional, for JPG-only fallback analysis) — `pip install Pillow`
- `argparse`, `pathlib`, `concurrent.futures` from stdlib

No alternative language is realistic — LibRaw has no shell wrapper that exposes the raw histogram primitives needed.

## 11. Deployment

- Script at `files/home/.local/bin/clip-scan` (matches the `raw-prune` neighbour)
- New Ansible task block added to `playbooks/imports/optional/common/play-photography.yml` (where `raw-prune` already lives) installing `rawpy` via pip-user or system package
- Verify Fedora 43 packaging on host: `dnf info python3-rawpy` (may need `pip install --user rawpy` fallback)

## 12. Open questions (final — locked into PLAN.md decision gate)

Updated to reflect weighted-score design:

1. Exact tool name: `clip-scan`? `raw-cull`? `exposure-flag`?
2. Default score thresholds: 2.0% white / 5.0% black weighted-equivalent percent as proposed?
3. Default cutoffs for the ramp start: 0.95 of `white_level` / 1.05 of `black_level` as proposed?
4. Default `--gamma 1.0` (linear ramp) — confirm? Or pre-set a different curve?
5. Pair handling: ARW as analysis source, propagate to JPG/HIF/XMP siblings — confirm?
6. Dry-run default vs apply default — confirm dry-run-by-default?
7. Where to install rawpy: pip-user vs system package vs venv?
8. JPG-only folders: analyse them? skip them? warn?
9. Parallelism: process pool with `nproc` default — confirm?

## 13. References

### Local archive

Web research is archived in [`references/`](references/) so the plan stays solvable even if a URL rots or paywalls.

- [`references/README.md`](references/README.md) — index mapping each source to what it contributed
- [`references/mertens-exposure-fusion.md`](references/mertens-exposure-fusion.md) — the Mertens 2007 well-exposedness Gaussian formula (load-bearing prior art for the weighted clipping score)
- [`references/rawdigger-histograms.md`](references/rawdigger-histograms.md) — RawDigger's framing of raw saturation (no weighted-score precedent)
- [`references/lensrentals-ettr.md`](references/lensrentals-ettr.md) — Kasson's per-channel clipping and specular-vs-important wisdom
- [`references/wikipedia-ettr.md`](references/wikipedia-ettr.md) — ETTR theory, SNR rationale, bit-depth-per-stop allocation
- [`references/kasson-see-spot-run.md`](references/kasson-see-spot-run.md) — JPEG vs raw histogram divergence
- [`references/fastrawviewer-calibration.md`](references/fastrawviewer-calibration.md) — saturation point identification, DR-per-ISO calibration

### Live URLs

- LibRaw docs: <https://www.libraw.org/docs>
- rawpy docs: <https://letmaik.github.io/rawpy/api/>
- Sony ARW format notes: <https://lclevy.free.fr/raw/> (third-party reverse-engineering)
- Adobe LR highlight clipping: built-in histogram triangle threshold ~0.1% (canary), photographer convention ~1-2% (problem)
- LibRaw `black_level_per_channel` / `white_level`: <https://www.libraw.org/docs/API-CXX.html>

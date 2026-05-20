# FastRawViewer — RAW Histogram Calibration

**Source**: <https://www.fastrawviewer.com/node/503>
**Captured**: 2026-05-20

## Key concepts extracted

### Saturation point identification

Two methods used by FastRawViewer:

1. **Metadata-declared limit**: camera-provided highlight limit stored in file metadata. Conservative — may be lower than the actual sensor saturation.
2. **Histogram shape recognition**: identifies clipping patterns characteristic of saturation (see RawDigger's documentation on histogram overexposure shapes).

This matches what LibRaw exposes: `white_level` is the metadata-declared value. We use it directly; we do not attempt shape-recognition fallback (would add complexity and the metadata value is sufficient for our use case).

### "Near-saturated" definition

The discussion does **not** explicitly define a "near-saturated" threshold. However:

- Dynamic-range calculations conventionally include "+3EV (from midtone to saturation)" as standard headroom
- Camera manufacturers sometimes deviate to provide additional highlight protection (the declared `white_level` may be below sensor max)

### Over/underexposure detection

**Underexposure (UE)** is measured in EV stops below the clipping point. Acceptability depends on:

- Personal noise tolerance
- Output media size and type
- ISO setting (for ISO-invariant cameras, dynamic range drops 1 stop per ISO stop increase above ISO 1600)

Example calibration values for Sony A7III:

| ISO  | Dynamic Range |
| ---- | ------------- |
| 200  | 10.5 EV       |
| 1600 | 8.5 EV        |
| High | 3.5 EV        |

### Per-channel analysis

The document provides no specific per-channel recommendations. (Other sources confirm per-channel is the practitioner-correct approach.)

### Numerical thresholds

- **Midpoint reference**: 117 in 8-bit sRGB with gamma 2.2 — derived from `0.18^(1/2.2) × 255 ≈ 116.9`
- **Standard headroom**: ~3 EV between midpoint and clipping
- **"Generic" raw calibration**: 12.7% (not the 18% commonly cited for legacy reflectance calibration)

## Relevance to `clip-scan` design

Confirms:

1. **Use the camera's declared `white_level`** (LibRaw's `white_level` field) — manufacturers already build in some highlight protection
2. **3 EV is a relevant unit** — "1 stop below saturation" in our linear-ramp cutoff corresponds to a value of `0.5 * white_level`. Our default 0.95 cutoff is roughly 0.07 stops below saturation — a much tighter zone than the conventional 3 EV headroom, which is correct: we're measuring *bad* clipping, not designing exposure
3. **Dynamic range falls with ISO** — implication: high-ISO frames may legitimately clip more than the threshold suggests, because their black-level noise floor is higher. Out of scope for v1 but flagged as a future-iteration consideration if false positives appear on high-ISO frames.

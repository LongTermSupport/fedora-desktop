# RawDigger — What is the raw data histogram?

**Source**: <https://www.rawdigger.com/howtouse/rawdigger-histograms-what-is-the-raw-histogram>
**Author**: LibRaw LLC / Iliah Borg, Alex Tutubalin
**Captured**: 2026-05-20

## Key concepts extracted

### What a raw histogram is

A raw histogram represents the distribution of pixel values in raw sensor data. RawDigger reads the raw data and counts how many pixels took the value 0, 1, 2, etc. up to the bit-depth max (4095 for 12-bit cameras, 16383 for 14-bit). Each bar's height corresponds to the count of pixels holding that particular value.

### Saturation point

For a 12-bit camera, the theoretical maximum is `2^12 - 1 = 4095`. Important caveats:

- "In real life a 12-bit camera may not reach 4095 maximum even for a grossly overexposed shot"
- "Maximum value may depend on ISO setting"
- Different camera manufacturers implement varying maximum values by design

This is why `clip-scan` must use the LibRaw-reported `white_level` from each file rather than hardcoding `2^bit_depth - 1`.

### Acceptable clipping thresholds

RawDigger does **not** provide specific percentage thresholds. Instead, it offers qualitative guidelines: a technically good histogram shows:

- A wide range of values is included
- No wide gaps in the distribution
- A smooth tail in highlights with no "hitting the wall" at the maximum value

### Near-saturated pixels

The article does **not** specifically define "near-saturated" pixels as a distinct category. RawDigger focuses on actual saturation where pixels reach the maximum value. This confirms our finding that no major commercial tool implements a weighted clipping score — they all use binary "at saturation" counts.

### Overexposure measurement

The article does not detail RawDigger's specific algorithm for measuring overexposure percentages. A forum reference mentions "0% over/under exposed pixels" as user-interpretive output.

### Per-channel vs combined clipping

The article does not directly address this. Other sources (see [lensrentals-ettr.md](lensrentals-ettr.md)) confirm per-channel is the practitioner-correct framing.

## Relevance to `clip-scan` design

This source confirms:

1. The need to use LibRaw's per-file `white_level` (not a hardcoded max)
2. The gap in the market for weighted clipping scores
3. The general acceptability of qualitative "smooth tail" thinking — but our use case (bulk batch flagging) needs a numeric threshold, which RawDigger does not provide

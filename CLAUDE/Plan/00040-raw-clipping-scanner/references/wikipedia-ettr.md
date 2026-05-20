# Wikipedia — Exposing to the Right (ETTR)

**Source**: <https://en.wikipedia.org/wiki/Exposing_to_the_right>
**Captured**: 2026-05-20

## Key concepts extracted

### Origin and definition

ETTR was developed by Michael Reichmann in 2003 after discussions with Thomas Knoll (original author of Photoshop). The technique:

> "adjusting the exposure of an image as high as possible at base ISO (without causing unwanted saturation) to collect the maximum amount of light."

The name comes from the resulting histogram: "the image histogram...should be placed close to the right of its display."

### Sensor saturation behaviour

Digital sensors (CCD and CMOS) accumulate electric charge proportionally to light exposure. Critical asymmetry in tonal allocation:

> "when data is recorded digitally, the highest (brightest) stop uses fully half of the discrete tonal values."

Subsequent stops follow a halving pattern:

- Brightest stop: 50% of available tonal values
- Next stop down: 25%
- Next stop down: 12.5%
- ...
- Darkest usable stop: a tiny fraction

This means **underexposure structurally wastes sensor capacity**, leading to "loss of tonal detail in dark areas...and posterization during post-production."

### Signal-to-noise ratio rationale

> "the benefit of more exposure lies not really in the better quantization, because the noise always present in photographic captures renders it invisible to the human eye, but solely in the better SNR, particularly in the shadows of a high-contrast scene."

ETTR maximises photons captured relative to electronic noise, reducing perceived grain in shadows.

### Practical thresholds

ETTR exposure is established using:

- Camera's base (lowest native) ISO
- Histogram right edge or highlight clipping warnings ("blinkies" / "zebras")
- **Not** the camera's meter reading

Constraints:

- Avoid unwanted saturation in important highlights
- Specular highlights (sun reflections, bright light sources) are acceptable to clip
- Raw file processing is essential — ETTR-exposed images appear too bright and need normalisation in raw conversion

For high-DR scenes, practitioners may apply **ETTIH** (Exposing to the Important Highlights), a "slight generalization of ETTR" where only dispensable highlights clip.

### Bit-depth allocation summary

| Position       | % of total quantisation levels (14-bit example) |
| -------------- | ----------------------------------------------- |
| Brightest stop | 50% (8192 levels)                               |
| 1 stop down    | 25% (4096 levels)                               |
| 2 stops down   | 12.5% (2048 levels)                             |
| 3 stops down   | 6.25% (1024 levels)                             |
| 4 stops down   | 3.125% (512 levels)                             |
| 5 stops down   | 1.5625% (256 levels)                            |

This is why pushing exposure to the right and pulling down in post yields more tonal information in shadows than the reverse — even though both achieve the same final brightness.

## Quality flag

> The Wikipedia article carries quality-control flags indicating reliance on primary sources and disputed notability claims (as of August 2024).

## Relevance to `clip-scan` design

Confirms:

1. **Clipping is a real and asymmetric problem** — highlights once lost are unrecoverable; shadows are recoverable, so highlight clipping deserves stricter defaults than shadow clipping (our 2% vs 5% asymmetry has clear physical basis)
2. **Specular highlights are accepted to clip** — the cutoff parameter in our weighted score lets users tune what counts as "specular" vs "important"
3. **ETTIH is a recognised concept** — "tolerable to clip some pixels if they're not important" matches our score-based threshold philosophy

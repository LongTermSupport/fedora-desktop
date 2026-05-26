# Lensrentals — How to Expose Raw Files Part 2

**Source**: <https://www.lensrentals.com/blog/2023/05/how-to-expose-raw-files-part-2/>
**Author**: Jim Kasson
**Captured**: 2026-05-20

## Key concepts extracted

### Raw histogram vs in-camera histogram

> "you want to know when the raw file experiences clipping, and the zebras and histograms tell you when an sRGB or Adobe RGB JPEG image...is clipping."

In-camera histograms derive from JPEG preview images at lower resolution than actual raw files. Lightroom and Adobe Camera Raw histograms display "the histogram of the developed image" with default settings applied, not true raw data.

**Reliable tools for actual raw histograms**: RawDigger and Fast Raw Viewer.

### Saturation point examples

For the Hasselblad X2D referenced in the article: "this camera saturates at just below 64000" (16-bit).

For a Sony A7V (our target): we expect ~16383 for uncompressed 14-bit, slightly less for cRAW. To be confirmed in Phase 2 host probe.

### Channel-specific clipping

Different colour channels saturate at different points in a given image:

> "the blue raw channel is not the one with the highest values; that honor goes to the green raw channels."

One test in the article showed "most of the highlights in the blue and green raw channels are about a stop and a half from clipping."

> A commenter observes the green channel often controls raw exposure in landscape photography, though exceptions exist (sunsets, backlit red subjects).

**Implication for `clip-scan`**: must check each channel independently; flagging on "any channel clipped" is the right design choice.

### Important vs specular highlights

The article distinguishes:

- **Important highlights**: "areas where you want detail" — texture-rich, tonally important
- **Specular highlights**: very bright, small features — reflections, light sources

> "Be careful around bright colorful objects like flowers" where clipping behaviour is unpredictable.

### Specific numerical thresholds

The article provides **no explicit acceptable clipping percentage**. Instead, exposure margins:

- **Conservative approach**: Maintain "a stop and a half from clipping" for safety
- **Aggressive ETTR**: One example showed clipping occurred with "just a third of a stop more exposure" beyond the optimal point
- **In-camera histogram margin**: The article notes you must expose "1 2/3 stops underexposed" from true ETTR before the in-camera histogram stops showing clipping warnings

### Noise context

At 9 stops down from full scale on the sensor measured: photon noise was 8.8 electrons; read noise at ISO 100 was 4.2 electrons; total noise was 9.75 electrons. (Provides context on why shadow recovery is easier than highlight recovery.)

### ETTR methodology

> "Set your ISO to base ISO. Set your f-stop to the stop you need to get the depth of field you want. Set your shutter speed to get as much or as little blur as you want."

Then: "Check the histogram or the zebras to make sure you're not going to clip any important highlights."

### Practical conservative strategy

> "If you don't want to look at the raw histograms, you probably won't have any raw clipping for near-neutral objects lit with common light sources, though you'll likely be underexposed from true raw ETTR."

## Relevance to `clip-scan` design

Confirms:

1. **Per-channel analysis is essential** — flagging on "any channel clipped" is correct
2. **No numerical convention exists** — we're free to choose our own defaults
3. **Specular highlights are the noise floor we have to tolerate** — this is exactly what the weighted-score's "below cutoff → zero contribution" zone is for
4. **Conservative defaults are practitioner-aligned** — better to be slightly forgiving than aggressively flagging

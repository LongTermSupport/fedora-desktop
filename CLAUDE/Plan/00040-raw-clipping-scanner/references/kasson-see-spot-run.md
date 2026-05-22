# Kasson — How to use in-camera histograms for raw exposure ("See Spot Run" version)

**Source**: <https://blog.kasson.com/the-last-word/how-to-use-in-camera-histograms-for-raw-exposure-the-see-spot-run-version/>
**Author**: Jim Kasson
**Captured**: 2026-05-20

## Key concepts extracted

### JPEG-derived vs raw histogram

The in-camera histogram displays data derived from the embedded JPEG preview, not raw sensor values. The JPEG undergoes tone mapping and white balance before display.

> "the histogram will typically indicate clipping sooner than the raw data actually clips."

This conservative bias works in the photographer's favour for *avoiding* clipping (if the JPEG histogram doesn't show clipping, the raw probably doesn't either) but does **not** work for confirming clipping (a clipped JPEG histogram may correspond to unclipped raw with recoverable detail).

### Acceptable clipping standards

A **conservative margin-of-safety approach** rather than absolute limits:

- Move exposure rightward until histogram approaches the right edge
- Stop before important highlights clip
- Specular highlights and reflections can be sacrificed
- Texture-rich, tonally-important areas must be protected

### Specific numerical thresholds

The article provides **no numerical clipping thresholds**. Qualitative language: approach the right edge without being "pressed hard against it in areas that matter."

### Per-channel observations

> "one color channel clips early in the JPEG even though the raw data is still intact"

(Saturation reduction can help reduce this in-camera-vs-raw mismatch.)

A commenter notes the green channel often controls raw exposure in landscape photography, with exceptions for sunsets and backlit red subjects.

### Asymmetry of clipping vs noise

> "Noise can be managed, but clipped highlights cannot be recovered."

Underexposure is recoverable through postprocessing with relatively modest penalties; overexposure causes permanent data loss.

## Relevance to `clip-scan` design

Confirms:

1. **We must read raw data, not the JPEG histogram** — already a load-bearing design decision (`rawpy` decodes the raw mosaic directly)
2. **Per-channel analysis is essential** — already in our design
3. **Highlight clipping is harder than shadow clipping** — supports our asymmetric defaults (stricter white threshold than black)
4. **No numerical convention exists** — we're inventing defaults; documenting our rationale carefully matters

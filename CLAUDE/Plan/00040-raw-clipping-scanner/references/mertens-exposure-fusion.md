# Mertens Exposure Fusion (2007)

**Sources (multiple — primary paper is paywalled / 403):**

- OpenCV API docs: <https://docs.opencv.org/4.x/d6/df5/group__photo__hdr.html>
- OpenCV HDR tutorial: <https://docs.opencv.org/4.x/d3/db7/tutorial_hdr_imaging.html>
- kbmajeed reference implementation: <https://github.com/kbmajeed/exposure_fusion>
- PAS-MEF derivative paper (arxiv): <https://ar5iv.labs.arxiv.org/html/2105.11809>
- WebSearch-extracted formulas (synthesis of multiple summaries)

**Original paper**: Tom Mertens, Jan Kautz, Frank Van Reeth. "Exposure Fusion." Pacific Graphics 2007.

**Captured**: 2026-05-20

---

## Algorithm summary

Mertens fuses a stack of bracketed-exposure images into one well-exposed LDR result without computing an HDR intermediate. Each pixel of each input image is scored by **three quality measures**, the measures are combined into a per-pixel weight, and the inputs are blended via a Laplacian pyramid using those weights.

### The three quality measures

| Measure              | Symbol | Formula                                                     | What it favours                   |
| -------------------- | ------ | ----------------------------------------------------------- | --------------------------------- |
| **Contrast**         | C      | Absolute value of a Laplacian filter applied to greyscale   | Pixels in textured / edge regions |
| **Saturation**       | S      | Standard deviation across R, G, B channels at that pixel    | Vividly coloured pixels           |
| **Well-exposedness** | E      | Gaussian: `exp(-(I_n - 0.5)^2 / (2 * sigma^2))` per channel | Pixels near mid-intensity         |

The Gaussian's mean is **0.5** (normalised mid-grey) and its standard deviation is **σ = 0.2** (in the normalised [0,1] image space).

Well-exposedness is evaluated **per channel** independently, then combined (typically by multiplying the three channel weights).

### Weight combination

Per-pixel weight for image *i* at location *(x,y)*:

```
W_i(x,y) = C_i(x,y)^wc * S_i(x,y)^ws * E_i(x,y)^we
```

The exponents `wc`, `ws`, `we` are user-tunable. OpenCV's `createMergeMertens()` defaults them to `1.0`, `1.0`, `0.0` respectively (note: OpenCV defaults `we=0`, which is unusual — the original paper uses `1.0` for all three).

After computing W_i for each input image, weights are **normalised across the stack** so they sum to 1 at every pixel.

### Pyramid blending

Naive weighted averaging produces visible seams. Mertens avoids this with a **Laplacian-pyramid blend**:

1. For each input image *i*, compute a Laplacian pyramid `L{I_i}`
2. For each weight map W_i, compute a Gaussian pyramid `G{W_i}`
3. At each pyramid level *l*, blend: `L{R}^l = sum_i G{W_i}^l * L{I_i}^l`
4. Collapse `L{R}` back to the final image

This avoids halos and seams that a single-resolution blend would produce.

---

## Default parameters

| Parameter         | Default (paper) | Default (OpenCV) |
| ----------------- | --------------- | ---------------- |
| `wc` (contrast)   | 1.0             | 1.0              |
| `ws` (saturation) | 1.0             | 1.0              |
| `we` (exposure)   | 1.0             | 0.0 (!)          |
| σ (well-exp)      | 0.2             | (not exposed)    |
| mean (well-exp)   | 0.5             | (not exposed)    |

---

## Relevance to `clip-scan` design

Mertens' well-exposedness measure is the load-bearing prior art for our weighted clipping score.

- **Mertens' framing**: Gaussian centered at 0.5 — pixels far from mid-grey (near 0 or 1) get *low* weight. This *down-weights* near-clipped pixels because they're unreliable for fusion.
- **`clip-scan`'s framing**: we want the **opposite** — pixels near the extremes get *high* weight because that's exactly what we're measuring badness of.

So we don't borrow Mertens' formula directly. We borrow the **idea** that per-pixel weights derived from a continuous function of value are more honest than a binary at/above-threshold count. Our weighting function is a one-sided linear ramp (or gamma-corrected ramp) from a cutoff to saturation, integrated over the image to produce a single per-side score.

Where Mertens uses three measures and a Gaussian, we use one measure (proximity to saturation) and a simple ramp. We don't need the perceptual sophistication of contrast + saturation + well-exposedness because we're not fusing images — we're just scoring "how much of this frame is in the unrecoverable zone."

---

## Code reference (OpenCV API)

```python
import cv2 as cv
merge_mertens = cv.createMergeMertens()
fusion = merge_mertens.process(images)
cv.imwrite('fusion.png', fusion * 255)
```

```cpp
Ptr<MergeMertens> merge_mertens = createMergeMertens();
merge_mertens->process(images, fusion);
imwrite("fusion.png", fusion * 255);
```

Signature: `cv::createMergeMertens(float contrast_weight=1.0f, float saturation_weight=1.0f, float exposure_weight=0.0f)`

# Research References

Web sources cited in [../research.md](../research.md) and [../PLAN.md](../PLAN.md), archived here so the plan stays solvable if any URL rots or paywalls.

Each `*.md` below is a content extract from one URL — not a verbatim copy (WebFetch returns model-summarised text). The original URL is recorded at the top of each file along with the access date in the individual file (date is omitted from this index because index files are required to have stable content).

## Index

| File                                                         | Source                                                               | What it contributed                                                                                              |
| ------------------------------------------------------------ | -------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| [mertens-exposure-fusion.md](mertens-exposure-fusion.md)     | Multiple (OpenCV docs + GitHub + a web search returning the formula) | The Mertens 2007 well-exposedness Gaussian formula — the mathematical backbone of the weighted clipping score    |
| [rawdigger-histograms.md](rawdigger-histograms.md)           | rawdigger.com — "What is the raw data histogram"                     | RawDigger's framing of raw saturation and clipping. No formal weighted-score precedent.                          |
| [lensrentals-ettr.md](lensrentals-ettr.md)                   | lensrentals.com (Jim Kasson) — "How to Expose Raw Files Part 2"      | Per-channel clipping wisdom (green clips first), specular-vs-important distinction, conservative margin guidance |
| [wikipedia-ettr.md](wikipedia-ettr.md)                       | en.wikipedia.org — "Exposing to the right"                           | ETTR origin, sensor SNR rationale, bit-depth-per-stop allocation                                                 |
| [kasson-see-spot-run.md](kasson-see-spot-run.md)             | blog.kasson.com — "How to use in-camera histograms..."               | In-camera (JPEG-derived) vs true raw histogram divergence; per-channel clipping notes                            |
| [fastrawviewer-calibration.md](fastrawviewer-calibration.md) | fastrawviewer.com — "RAW Histogram calibration"                      | Saturation point identification (metadata vs histogram shape); DR/EV calibration practice                        |

## Search-only references (no archived page content)

These appeared in WebSearch results but were not WebFetched (paywall, 403, or low marginal value):

- [Hasinoff — Saturation (imaging) preprint](https://people.csail.mit.edu/hasinoff/pubs/hasinoff-saturation-2012-preprint.pdf) — academic treatment of saturation as a pixel-level property, MIT/Google
- [Mertens et al. 2007 — Exposure Fusion (ResearchGate)](https://www.researchgate.net/publication/4295602_Exposure_Fusion) — original paper (403 on fetch)
- [Photography Life — ETTR Explained](https://photographylife.com/exposing-to-the-right-explained) — practitioner guide (402 on fetch, behind paywall)
- [Visual Wilderness — Why Landscape Photographers Should ETTR](https://visualwilderness.com/fieldwork/why-landscape-photographers-should-expose-to-the-right) — practitioner guide (403 on fetch)
- [Grokipedia — Exposure Fusion](https://grokipedia.com/page/exposure_fusion) — Mertens summary (403 on fetch)
- [OpenCV docs — HDR Imaging tutorial](https://docs.opencv.org/4.x/d3/db7/tutorial_hdr_imaging.html) — has `createMergeMertens()` API but not the underlying formulas
- [OpenCV docs — Photo HDR group](https://docs.opencv.org/4.x/d6/df5/group__photo__hdr.html) — API ref only, no formulas
- [GM's blog — Mertens Fusion tutorial](https://gimoonnam.github.io/imageprocessing/MertensFusion/) — referenced in searches (404 on direct fetch)
- [PAS-MEF paper (arxiv 2105.11809)](https://ar5iv.labs.arxiv.org/html/2105.11809) — references Mertens' formula; cited in the Mertens summary file

## How prior art shaped the design

No existing photography tool implements a weighted clipping score for bulk culling. RawDigger, FastRawViewer, and Lightroom report clipping as a binary count (% of pixels at saturation). Mertens' 2007 exposure fusion uses a per-pixel weight derived from a Gaussian centered at mid-grey — the closest formal precedent for weighted exposure evaluation — but Mertens uses it to *down-weight* near-clipped pixels for HDR fusion, the opposite goal from ours.

The `clip-scan` design **inverts Mertens' framing**: pixels near the extremes score *higher*, not lower. We integrate over the image to get a single per-side score, then threshold the score. The math is novel for bulk-culling but rests on well-established weighted-quality-measure prior art.

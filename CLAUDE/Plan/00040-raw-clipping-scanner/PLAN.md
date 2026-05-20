# Plan 00040: `clip-scan` — Raw Clipping Pre-Lightroom Scanner

**Status**: 🔄 In Progress — Phases 3-7 code landed in-container; Phase 2 host probes + Phase 8 manual tests deferred to host
**Created**: 2026-05-20
**Owner**: joseph
**Priority**: Medium
**Type**: Feature (new tool)

## Overview

A standalone CLI tool, `clip-scan`, that scans a directory of Sony ARW (+ sibling JPG / HIF / XMP) photographs, analyses each frame's raw histogram, and renames files whose highlight or shadow clipping exceeds tunable thresholds. The flag is encoded in the filename itself — `DSC123.ARW` becomes `DSC123.wclip.ARW`, `DSC123.bclip.ARW`, or `DSC123.wclip.bclip.ARW` depending on what's clipped.

The tool runs as a **preprocess step before Lightroom import**, so the renamed filenames are what Lightroom records on first ingest. Downstream the user can filter or smart-collect by filename substring (`.wclip.`, `.bclip.`) to triage a shoot in seconds rather than minutes-per-frame.

**Weighted clipping score** per side (final design after iteration — see research.md Section 3): pixels near sensor saturation / black level contribute proportionally to a per-image score via a linear ramp from a configurable cutoff to the extreme; the file is flagged when the score exceeds a configurable threshold. This is mathematically more honest than a binary "% at exact saturation" count because near-max pixels look indistinguishable from at-max pixels in the final image, and the score self-balances "number of bad pixels" against "how bad each one is."

Research lives in [research.md](research.md), with backing references archived in [references/](references/). Headline picks: Python + `rawpy` (LibRaw binding) as the only sensible decode path; defaults of `--white-cutoff 0.95 --white-score 2.0 --black-cutoff 1.05 --black-score 5.0 --gamma 1.0`; rename-by-suffix as the user-mandated output channel; dry-run by default with explicit `--apply` to commit.

## Goals

- Standalone CLI invokable as `clip-scan [DIR]` or `clip-scan [FILE]`
- Bulk-flag ARW (and sibling JPG/HIF/XMP) files via filename rename when the weighted clipping score exceeds the threshold
- Weighted-score design per side: a linear ramp from a cutoff to the extreme produces a continuous per-pixel weight; the per-image score is the mean weight (expressed as "equivalent percent of fully-clipped pixels"); threshold the score to decide whether to flag
- Idempotent: re-running with different thresholds produces the correct new name, never `.wclip.wclip`
- Dry-run by default, explicit `--apply` to commit changes
- Pair-aware: ARW is the analysis source, verdict propagates to JPG / HIF / XMP siblings
- Parallel across CPU cores via `concurrent.futures.ProcessPoolExecutor`
- Composable: accepts both a directory (recursive scan) and a single-file path (one-shot mode for ftp-camera future integration)
- Deploys via the existing `play-photography.yml` Ansible playbook alongside `raw-prune`

## Non-Goals

- Focus / sharpness / blur scoring — that's a separate, harder problem; out of scope for this plan
- AI-based or semantic culling (closed eyes, duplicates, scene quality) — commercial tools own that slice
- Catalog integration with Lightroom (XMP sidecar writes, ratings, colour labels) — the user explicitly chose filename rename over sidecars
- File deletion or moving — `clip-scan` only renames; deletion is `raw-prune`'s job
- Demosaiced histogram analysis — we analyse the raw Bayer mosaic for sensor-accurate clipping detection; post-tone-curve histograms (what GUI tools show) systematically over-report clipping and are not what we want
- Cross-camera coverage beyond Sony A7V in v1 — LibRaw transparently supports CR2/CR3/NEF/RAF/DNG/etc. and the tool will not refuse them, but the threshold defaults are calibrated for Sony 14-bit raw; other cameras' suitability is a future-validation exercise

## Context & Background

- Host: Fedora 43 / GNOME 49.6 / Wayland
- Camera: Sony A7V (ARW + JPG simultaneously, 14-bit raw)
- Upstream pipeline: `ftp-camera` (Plan 00038 / 00039) dumps and sorts into `JPG/` and `RAW/` subdirs by date
- Downstream: user imports the sorted folder into Adobe Lightroom for selects/edits
- Existing neighbour: `raw-prune` (at `files/home/.local/bin/raw-prune`, deployed by `play-photography.yml`) — sets the convention for scope, naming, deployment, dry-run semantics, and colour output
- This is the first tool in the workflow that reads raw pixel data; nothing else in the repo links against LibRaw yet
- A future Plan 00039 follow-up may wire `clip-scan --apply --quiet` into the per-upload sort path for live flagging during a shoot — out of scope here, but the single-file CLI invocation pattern needs to support it

## Research Summary (full detail in research.md)

**Weighted clipping score design** (FINAL — research.md Section 3 supersedes Section 2; references/mertens-exposure-fusion.md is the load-bearing prior art):

| Flag                   | Default | Meaning                                                                                       |
| ---------------------- | ------- | --------------------------------------------------------------------------------------------- |
| `--white-cutoff RATIO` | 0.95    | Linear weighting ramp starts at `value = RATIO × white_level`; pixels below this contribute 0 |
| `--white-score PCT`    | 2.0     | Flag `.wclip` when the weighted white-clip score exceeds PCT                                  |
| `--black-cutoff RATIO` | 1.05    | Linear weighting ramp starts at `value = RATIO × black_level`; pixels above this contribute 0 |
| `--black-score PCT`    | 5.0     | Flag `.bclip` when the weighted black-clip score exceeds PCT                                  |
| `--gamma FLOAT`        | 1.0     | Power applied to the ramp (1.0 = linear; >1 punishes near-max more)                           |

For each pixel value `v`, weight is 1.0 at saturation, 0.0 at the cutoff, linearly interpolated in between. The per-image score is the mean weight across pixels, expressed as percent; per-channel score is computed and the max across R/G/B is used (matches "any channel clipped" framing). A score of 2.0 means "the equivalent of 2% fully-blown pixels" — a frame with 1% at literal max + 2% at the midpoint between cutoff and saturation scores exactly 2.0 (`1.0×1.0 + 2.0×0.5`). Score thresholds are user-tunable.

**Why weighted score over binary count**: a pixel at 99% saturation looks identical to one at 100% in the final image (tone curve flattens near saturation). The weighted score matches photographer perception ("how blown does this look") instead of an arbitrary at-max line.

**Decode**: Python + `rawpy` (LibRaw binding). No alternative is competitive — dcraw is moribund, ImageMagick destroys raw saturation info via the demosaic, LibRaw-direct is overkill.

**Idempotency**: strip-then-rebuild filename — split on dots, drop any `wclip` / `bclip` tokens in the stem, recompute, reassemble with new flags in canonical (`wclip` then `bclip`) order.

**Pair handling**: group files by canonical stem; analyse the best source (ARW > HIF > JPG); apply the verdict to every sibling in the group.

**Lightroom**: since `clip-scan` runs pre-import, catalog-missing-file concerns don't apply. LR reads the renamed filename as gospel on first import. User filters in LR via Library → Filter bar → "Filename contains `.wclip.`".

**Performance**: ~0.7s per ARW single-threaded; ~50-75s for a 500-frame shoot on an 8-core ProcessPool.

**Deployment**: script at `files/home/.local/bin/clip-scan`; new tasks in `playbooks/imports/optional/common/play-photography.yml` to install rawpy (system package preferred, pip-user fallback).

## Decision Gate

Before code is written, confirm with the user:

- [ ] ⬜ **Tool name**: `clip-scan` (recommended; aligns with `raw-prune` sibling)? `raw-cull`? `exposure-flag`? Other?
- [ ] ⬜ **Defaults** — confirm or override:
  \- [ ] ⬜ `--white-cutoff 0.95` (ramp starts at 95% of `white_level`; pixels below contribute 0 to the score)
  \- [ ] ⬜ `--white-score 2.0` (flag `.wclip` when weighted white-clip score exceeds 2.0%)
  \- [ ] ⬜ `--black-cutoff 1.05` (ramp starts at 105% of `black_level`; pixels above contribute 0 to the score)
  \- [ ] ⬜ `--black-score 5.0` (flag `.bclip` when weighted black-clip score exceeds 5.0%)
  \- [ ] ⬜ `--gamma 1.0` (linear ramp; >1 punishes near-max more — recommend keeping linear for v1)
- [ ] ⬜ **Pair-handling**: ARW > DNG > HIF > JPG analysis priority, verdict applies to all stem-siblings — confirm? (DNG added per subagent M3; ARW > HIF > JPG was already locked)
- [ ] ⬜ **Safety default**: dry-run is the default, explicit `--apply` to commit — confirm?
- [ ] ⬜ ~~**Lightroom-catalog safety rail**~~ — REMOVED. Lightroom has no Linux build; this whole control is dead code on the deploy target. See Decision 9.
- [ ] ⬜ **Python test location**: the repo has no existing Python test convention. Place tests under `/workspace/tests/clip_scan/` at repo root with `pytest` as the runner — confirm, or pick a different layout? (per subagent #4 — this gap MUST be closed before Phase 3 starts or the TDD hook will block implementation)
- [ ] ⬜ **JPG-only folders / corrupt-ARW fallback**: analyse the JPG (best-effort, post-tone-curve) with **separate** JPG-specific cutoffs (`--jpg-white-cutoff 0.95` ≈ 243/255, `--jpg-black-cutoff 0.05` ≈ 13/255) — confirm? Or skip JPG-only files entirely in v1? (per subagent M5)
- [ ] ⬜ **Output format**: human-readable dry-run table by default, `--json` flag with schema documented in Phase 5 — confirm scope?
- [ ] ⬜ **`xmp` sidecar pattern (deferred)**: per subagent L1, defer XMP rename to a v2 ticket since the user doesn't currently produce them. Confirm v1 ignores XMP, or do you want it included now?

**Removed from decision gate** (moved elsewhere):

- ~~rawpy install path~~ — mechanical Phase 2 probe outcome (system pkg if available else pip-user), not a user decision per subagent M6

## Tasks

### Phase 1: Decision Gate

- [ ] ⬜ User reviews this plan and answers the Decision Gate questions
- [ ] ⬜ Update plan with finalised tool name, default values, and any scope adjustments

### Phase 2: Host probe & dependency verification

- [ ] ⬜ On host: `dnf info python3-rawpy` to check Fedora 43 packaging (review feedback: confirmed not in Fedora repos historically; likely pip-user path required)
- [ ] ⬜ On host: `dnf info libraw libraw-devel` — confirm system LibRaw exists and capture its version
- [ ] ⬜ On host: `pip show rawpy` after install — capture rawpy's bundled LibRaw version; **must be ≥ 0.21 for Sony A7V cRAW decode support** (per subagent review)
- [ ] ⬜ On host: `python3 -c "import rawpy; r=rawpy.imread('/tmp/test.ARW'); print(r.white_level, r.black_level_per_channel, r.raw_colors_visible.shape, r.raw_pattern)"` against a representative A7V ARW to confirm LibRaw reads the expected metadata AND the CFA pattern is accessible (per subagent H2)
- [ ] ⬜ On host: capture `white_level` and **full `black_level_per_channel` 4-tuple** (R, G1, B, G2) for both compressed (cRAW) and uncompressed ARW samples from the A7V — document in `research.md` "host findings" section. Verify the per-channel values either are or are not identical (subagent H1 warned they may differ by 2-10 units at some ISOs)
- [ ] ⬜ On host: **time the decode** — `time python3 -c "import rawpy; r=rawpy.imread('test_craw.ARW'); _ = r.raw_image_visible.copy()"` for both cRAW and uncompressed; capture the actual per-file decode latency for honest performance estimates (per subagent #2)
- [ ] ⬜ Lock the rawpy install path based on the probe: system package if present, else `pip install --user rawpy`. **This is not a decision-gate question** — it's a mechanical "use what works" outcome (per subagent M6)
- [ ] ⬜ Confirm Pillow (or stdlib `PIL`) availability for the JPG-only fallback path

### Phase 3: TDD Implementation — core analysis

- [ ] ⬜ Create test file `tests/unit/test_clip_scan.py` (or wherever the repo's Python test convention lands — the repo currently has none, so check with the user first or co-locate as a top-level `tests/` dir)
- [ ] ⬜ Write failing test: filename canonicalisation
  (`DSC123.wclip.bclip.ARW` → stem `DSC123`, ext `ARW`, flags `(True, True)`)
- [ ] ⬜ Write failing test: rebuild
  (stem `DSC123`, ext `ARW`, flags `(True, False)` → `DSC123.wclip.ARW`)
- [ ] ⬜ Write failing test: re-run produces same name, not stacked suffixes
- [ ] ⬜ Write failing test: dots-in-stem survive
  (`Holiday.beach.DSC123.ARW` → preserved stem `Holiday.beach.DSC123`)
- [ ] ⬜ Write failing test: pair grouping (`DSC123.ARW`, `DSC123.JPG`, `DSC123.ARW.xmp` group under stem `DSC123`)
- [ ] ⬜ Write failing test: weighted-score ramp math — synthetic array where every pixel is exactly at saturation should produce score 100.0; every pixel at exactly the cutoff should produce 0.0; uniform halfway-between cutoff and saturation should produce 50.0
- [ ] ⬜ Write failing test: white-score boundary — 2% of pixels at saturation, rest at zero → score == 2.0
- [ ] ⬜ Write failing test: equivalent-clipping equivalence — 1% at saturation + 2% at midpoint-of-ramp (i.e. at `(cutoff + 1.0) / 2 * white_level`) → score == 2.0 (same as 2% at saturation)
- [ ] ⬜ Write failing test: per-channel max — score is taken as max across R/G/B/G2 channel pools (channel-specific clipping is correctly detected)
- [ ] ⬜ Write failing test: black-score math (mirror of white-side tests), explicitly using a per-channel `black_level_per_channel` 4-tuple where the four values are NOT identical — confirms the implementation uses per-channel pedestals, not a scalar (per subagent H1)
- [ ] ⬜ Write failing test: gamma parameter — gamma=2.0 produces same boundary behaviour but more aggressive interior weighting. Concrete: a uniform array at the *midpoint between cutoff and saturation* (value = `(cutoff + 1.0) / 2 * white_level`) scores 0.25 with gamma=2.0 vs 0.5 with gamma=1.0 (per subagent #3 — clarify the "midpoint" anchor)
- [ ] ⬜ Write failing test: zero-pixels-in-ramp case — a uniform array entirely below the cutoff should produce *exactly* 0.0, not a floating-point dust value like 1e-9 that could cross a `--white-score 0.0` threshold (per subagent L2)
- [ ] ⬜ Write failing test: edge-case `white_level == 0` or `black_level >= white_level` produces a clear error rather than NaN propagation (per subagent L3)
- [ ] ⬜ Write failing test: CFA indexing — given a synthetic Bayer mosaic and its `raw_pattern`, the per-channel pool extraction (via `np.where(raw_colors == channel_idx)`) returns the correct pixel sets. R and B pools should each be ~25% of frame; G pool ~50% (per subagent H2)
- [ ] ⬜ Implement `canonical_stem()`, `rebuild_name()`, `group_by_stem()`, `score_white_per_channel()`, `score_black_per_channel()`, `analyse_array()` to pass tests
- [ ] ⬜ Refactor for clarity

### Phase 4: TDD Implementation — rawpy integration

- [ ] ⬜ Write failing test: `analyse_raw()` end-to-end against a fixture ARW (use a small known-good file checked into `tests/fixtures/` or skipped-if-missing)
- [ ] ⬜ Write failing test: Bayer extraction — given a real ARW, splitting via `r.raw_colors_visible` (or `r.raw_pattern` + indexing) yields four per-channel arrays whose union equals the full visible frame, with R/B ≈ 25% each and G ≈ 50% (per subagent H2)
- [ ] ⬜ Implement `analyse_raw(path)` returning `(white_score_pct, black_score_pct, white_level, black_level_per_channel, per_channel_scores)` — score is the weighted percentage as defined in research.md Section 3. **Per-channel black-level is honoured** (R/G1/B/G2 may differ — use the matching pedestal for each channel pool, per subagent H1)
- [ ] ⬜ Write failing test: corrupt-ARW fallback to JPG — when `rawpy.imread()` raises `LibRawError`, the analyser should attempt the sibling JPG (if present) as a best-effort fallback (per subagent M4)
- [ ] ⬜ Write failing test: JPG-only fallback path via Pillow — separate scoring path, **separate default thresholds** (JPG is 0-255 post-tone-curve; the raw cutoffs don't apply directly — see Decision 8 below)
- [ ] ⬜ Implement JPG fallback with a documented score-translation: the JPG path scores against 0-255 with cutoff defaults `--jpg-white-cutoff 0.95` (i.e. ≥ 243/255) and `--jpg-black-cutoff 0.05` (i.e. ≤ 13/255). Document loudly that JPG-path scores are NOT directly comparable to raw-path scores because the tone curve compresses near-saturation values differently per camera (per subagent M5)
- [ ] ⬜ Handle LibRaw decode errors gracefully (log per-file, skip the file, continue batch; **if a sibling JPG exists, attempt JPG fallback first** before skipping)

### Phase 5: TDD Implementation — CLI surface

- [ ] ⬜ Write failing test: argparse correctly parses `--white-cutoff 0.93 --white-score 1.5 --gamma 1.5 --apply DIR`

- [ ] ⬜ Implement CLI dispatch

- [ ] ⬜ Write failing test: dry-run prints proposed renames without filesystem changes

- [ ] ⬜ Implement dry-run output

- [ ] ⬜ Write failing test: **sibling rename order** — when a pair `DSC123.ARW` + `DSC123.JPG` + `DSC123.ARW.xmp` needs renaming, JPG/XMP siblings are renamed **before** the raw, so a process kill mid-batch leaves a state that still canonical-stems together (per subagent H4)

- [ ] ⬜ Implement apply mode using `pathlib.Path.rename` (atomic per-file on POSIX same-filesystem) with the documented sibling-first ordering

- [ ] ⬜ Write failing test: collision handling (target name already exists)

- [ ] ⬜ Implement collision logging

- [ ] ⬜ Write failing test: `--json` output format — defined schema (per subagent L4):

  ````
  ```json
  {
    "version": 1,
    "config": {"white_cutoff": 0.95, "white_score": 2.0, "black_cutoff": 1.05, "black_score": 5.0, "gamma": 1.0},
    "results": [
      {
        "path": "/abs/path/DSC123.ARW",
        "canonical_stem": "DSC123",
        "extension": "ARW",
        "siblings": ["DSC123.JPG"],
        "white_score": 0.04,
        "black_score": 2.31,
        "per_channel_white": [0.04, 0.02, 0.01, 0.02],
        "per_channel_black": [2.31, 1.10, 0.50, 1.10],
        "white_level": 16383,
        "black_level_per_channel": [512, 512, 512, 512],
        "verdict_wclip": false,
        "verdict_bclip": false,
        "action": "no-change",
        "new_name": null,
        "error": null
      }
    ]
  }
  ```
  ````

- [ ] ⬜ Implement JSON output to the documented schema

### Phase 6: TDD Implementation — parallel execution

- [ ] ⬜ Write failing test: `ProcessPoolExecutor` correctly returns results from N parallel workers without deadlock
- [ ] ⬜ Implement parallel scan
- [ ] ⬜ Write failing test: `--jobs 1` falls back to single-process for debugging
- [ ] ⬜ Implement single-process fallback

### Phase 7: Ansible integration

- [ ] ⬜ Add task block to `playbooks/imports/optional/common/play-photography.yml` to install rawpy (path chosen in Phase 2)
- [ ] ⬜ Add task to deploy `files/home/.local/bin/clip-scan` (file: 0755, owner: user_login)
- [ ] ⬜ Add the new script to the playbook's installation summary `debug` message
- [ ] ⬜ Verify playbook is idempotent: dry-run → first apply (changed) → second apply (ok, no changes)

### Phase 8: QA

- [ ] ⬜ `./scripts/qa-all.bash` clean (covers Bash + Python + Ansible patterns)
- [ ] ⬜ Manual host test plan:
  \- [ ] ⬜ Place 10 known-good and 10 known-clipped ARW files in a scratch dir, **using cRAW samples explicitly** (most likely place defaults misbehave — per subagent L6)
  \- [ ] ⬜ Place 10 uncompressed-ARW samples too; compare scores between cRAW and uncompressed of the same scene
  \- [ ] ⬜ Run `clip-scan scratch/` → verify dry-run lists exactly the clipped ones
  \- [ ] ⬜ Run `clip-scan --apply scratch/` → verify rename happened, siblings renamed too
  \- [ ] ⬜ Run `clip-scan --apply scratch/` again → verify no double-rename (idempotency)
  \- [ ] ⬜ Run with non-default thresholds → verify flagging changes accordingly
  \- [ ] ⬜ Single-file mode: `clip-scan --apply scratch/DSC123.ARW`
  \- [ ] ⬜ Test with a corrupt / truncated ARW → verify graceful skip with error log
  \- [ ] ⬜ Test with a corrupt ARW + valid sibling JPG → verify JPG fallback runs (per subagent M4)
  \- [ ] ⬜ Test with a JPG-only file (no sibling ARW) → verify fallback analysis runs
- [ ] ⬜ **Performance benchmark task** (per subagent M7 / #2):
  \- [ ] ⬜ Place a real 500-frame cRAW shoot in a scratch dir
  \- [ ] ⬜ Time `clip-scan --apply --jobs $(nproc) scratch/`
  \- [ ] ⬜ Document the actual time vs the claimed 50-75s target; revise success criterion downward if cRAW decode dominates (subagent estimated 1.5-2.5s per cRAW = 2-3× slower than research.md claim)
- [ ] ⬜ **Defaults calibration on real shoot** (per subagent M1):
  \- [ ] ⬜ Run `clip-scan --json` over a real recent shoot
  \- [ ] ⬜ Manually inspect the top-30 scores and bottom-30 scores
  \- [ ] ⬜ Confirm the cutoff at 0.95 / 1.05 and threshold at 2.0 / 5.0 produces useful triage on a 60MP A7V (subagent flagged that 60MP × these defaults may *under-flag* — recommend tightening if so)
  \- [ ] ⬜ Document any threshold adjustments in research.md's "host findings" section
- [ ] ⬜ Live workflow test:
  \- [ ] ⬜ Run `clip-scan --apply` over a real shoot folder before Lightroom import
  \- [ ] ⬜ Import to Lightroom
  \- [ ] ⬜ Confirm "Filename contains `.wclip.`" filter shows the expected frames

### Phase 9: Documentation

- [ ] ⬜ `clip-scan --help` output is complete and accurate
- [ ] ⬜ One-line entry added to the photography playbook's installation summary
- [ ] ⬜ Optionally add a short paragraph to `docs/` if a photography workflow guide exists there

## Technical Decisions

### Decision 1: Filename rename over XMP sidecars

**Context**: User explicitly chose filename rename over sidecar tags. Need to lock the rationale.

**Options**:

1. XMP sidecar (Lightroom-native, requires LR to be configured to read sidecars)
2. Filename rename (universal, works in any tool, greppable)
3. Both (complexity for little gain)

**Decision**: Option 2 — filename rename. User's stated motivation ("subsequent organisation easy somehow") maps directly to filename being the universal channel. Since this runs pre-LR-import, the catalog-missing-files cost of renames is zero.

**Date**: 2026-05-20 (user-specified)

### Decision 2: Analyse raw Bayer mosaic, not demosaiced output

**Context**: Two ways to compute a clipping histogram from a raw file.

**Options**:

1. Decode raw → demosaic → tone curve → histogram (what GUI tools show)
2. Decode raw → analyse Bayer mosaic directly (skip postprocess)

**Decision**: Option 2. The demosaic + tone curve cascades systematically over-report clipping because the tone curve compresses near-saturation values *upward*. Bayer-mosaic analysis tells the truth about whether the sensor was clipped. This is exactly what tools like RawDigger do; it's the right answer for raw analysis even though GUI users sometimes find Option 1 more intuitive.

**Date**: 2026-05-20

### Decision 3: Two-axis thresholds (cutoff + count) — SUPERSEDED by Decision 6

**Context**: Initial design had one knob per side ("% of pixels clipped"). User raised: is the value threshold itself adjustable?

**Options**:

1. Single knob per side: pixel is clipped iff value == saturation (or == black_level); flag iff >N% match
2. Two knobs per side: value cutoff (how close to saturation counts) + count threshold (how many tolerated)

**Decision**: Option 2. Strict "must equal max" misses photographically-clipped frames where pixels are at 99% saturation with no recoverable detail. Two-axis is more honest about how the sensor + tone curve actually work, and the extra surface area is worth it for the precision.

**Date**: 2026-05-20 (added in plan iteration)

**Superseded**: Decision 6 below replaces this with a weighted-score design that subsumes the two-axis idea more elegantly. Preserved here as iteration history.

### Decision 4: ARW > DNG > HIF > JPG analysis priority for pairs

**Context**: When multiple sibling files exist (raw + jpeg), which one drives the verdict?

**Options**:

1. Analyse each independently
2. Analyse the highest-fidelity source and apply verdict to all siblings

**Decision**: Option 2 with priority **ARW > DNG > HIF > JPG**. Raw sensor data is the truth about exposure; JPG histograms tell you about Sony's tone mapping, not the scene. Analysing both costs more time and can produce contradictory verdicts ("flag the ARW but not the JPG"), which is confusing.

DNG was added to the priority chain after subagent review (M3) noted Sony A7V can output DNG via post-processing or in some workflows. DNG is also raw and decodes via rawpy.

**Date**: 2026-05-20 (DNG added in third iteration per subagent review)

### Decision 5: Dry-run by default

**Context**: Renames are reversible but disruptive. What's the safer default?

**Options**:

1. Dry-run by default, `--apply` to commit
2. Apply by default, `--dry-run` to preview

**Decision**: Option 1. Renames are a side-effect on the user's photo library; making them require explicit consent is the right ergonomic. Matches the implicit contract of `raw-prune` (which has explicit `--yes` to skip the confirmation prompt).

**Date**: 2026-05-20

### Decision 6: Weighted clipping score (FINAL — supersedes Decision 3)

**Context**: Decision 3's two-axis design (`cutoff` + `count threshold`) was a substantial improvement over binary at-saturation counting, but during planning iteration the user raised a deeper question: shouldn't a pixel at 100% contribute more to "blown out" than a pixel at 98%? That observation collapses the two-axis design into a single weighted score.

**Options**:

1. Two-axis: binary cutoff defines "clipped or not" + count threshold on percentage of clipped pixels (Decision 3)
2. Weighted score: continuous per-pixel weight (1.0 at saturation, 0.0 at cutoff, linear in between), per-image score is the mean weight, threshold against the score

**Decision**: Option 2. A weighted score is mathematically more honest about how the sensor + tone curve behave — pixels at 99% of saturation are visually indistinguishable from at-max pixels in the final image. The score has a clean interpretation ("equivalent percent of fully-clipped pixels") and naturally trades off "number of pixels" against "how close to max each one is" in one number. Prior art (Mertens 2007 well-exposedness Gaussian, see `references/mertens-exposure-fusion.md`) supports continuous per-pixel quality weights as the principled framing.

A `--gamma` flag is added to let power users curve the ramp non-linearly (1.0 default; >1 punishes near-max more).

**Date**: 2026-05-20 (final iteration, after web research and subagent review)

### Decision 7: Python tests live in /workspace/tests/clip_scan/

**Context**: Subagent review (#4) noted the repo has no Python test convention — no pyproject.toml, no top-level tests/, no pytest config. The TDD-enforcement hook will block production-file creation until tests exist somewhere recognised.

**Options**:

1. Co-locate tests next to the source as `clip_scan_test.py`
2. Top-level `/workspace/tests/clip_scan/` mirroring the source layout
3. Inside `playbooks/imports/optional/common/` as a play-specific tests dir
4. Inside `files/home/.local/bin/tests/` next to the deployed scripts

**Decision**: Option 2. The `tests/` layout under repo root is standard pytest convention, isolates test code from deployable files (files/home/... is rsync'd verbatim by Ansible — having tests in there would deploy them to the user's `~/.local/bin/`, which is undesired), and leaves room for a future shared pytest config without polluting deployable artefacts.

The hook's TDD enforcement looks for `tests/unit/<subdir>/test_<module>.py` or collocated `<module>.test.py`. We satisfy "Separate mirror: tests/unit/{subdir}/test\_{module}.py" with `/workspace/tests/clip_scan/test_clip_scan.py`.

**Date**: 2026-05-20 (added per subagent #4)

### Decision 8: JPG fallback uses separate thresholds

**Context**: Subagent review (M5) noted that the raw-path defaults (cutoff 0.95 of `white_level`) cannot meaningfully apply to JPG analysis. JPG max is 255 with no `white_level` metadata; the tone curve compresses near-saturation values differently per camera.

**Options**:

1. Use the same defaults and hope they're "close enough" (silently inconsistent)
2. Separate `--jpg-white-cutoff` / `--jpg-black-cutoff` flags with documented thresholds (defaults: 0.95 of 255 ≈ 243 for white; 0.05 of 255 ≈ 13 for black)
3. Refuse JPG-only files entirely; require ARW

**Decision**: Option 2. JPG analysis is honest about being best-effort and not directly score-comparable to raw analysis. The flags are exposed but rarely need touching. The `--json` output's `analysis_source` field will indicate "raw" vs "jpg" so downstream tooling can interpret scores correctly.

**Date**: 2026-05-20 (added per subagent M5)

### Decision 9: Lightroom catalog safety rail — REVERTED

**Context**: Subagent review (#5) added a hard `.lrcat` parent-walk safety rail with a `--force-on-catalog` override. The reviewer missed a basic environmental fact: this is the `fedora-desktop` repo, and Adobe Lightroom has no Linux build. The user does not run Lightroom on this machine. A `.lrcat` file can never legitimately appear in the deploy target's filesystem tree.

**Reverted**: 2026-05-20 (same day). Removed:

- `find_lightroom_catalog()` function
- `--force-on-catalog` CLI flag
- Catalog-check branch in `main()`
- Epilog warning text in `build_parser()`
- Three `TestCatalogSafetyRail` cases
- Phase 5 / Phase 8 tasks referencing the rail
- Risk table row about LR catalog
- "Pre-Lightroom-import" framing in module docstring and playbook summary

Preserved as a memorial of the bad decision; will not be re-added in v1. If a future workflow ever mounts a Mac/Windows LR catalog over SMB and runs `clip-scan` against it, the user can decide then whether to bring this back.

## Success Criteria

- [ ] `clip-scan DIR` produces a clear list of which files *would* be renamed and why (per-file weighted white-score %, weighted black-score %, per channel max)
- [ ] `clip-scan --apply DIR` performs the renames atomically, ARW + JPG + XMP siblings stay grouped
- [ ] Re-running `clip-scan --apply DIR` is a no-op (idempotency)
- [ ] Changing thresholds and re-running produces correctly-updated names without stacking suffixes
- [ ] Performance: a 500-frame shoot processes in under **5 minutes** on an 8-core box (revised from "under 2 minutes" per subagent #2 — cRAW decompression on 60MP A7V is closer to 1.5-2.5s per file; original estimate was for uncompressed 24MP raw. Phase 8 captures the real number and the criterion may be revised again post-measurement)
- [ ] Lightroom import workflow: imported renamed files are filterable via `.wclip.` / `.bclip.` text filter
- [ ] `./scripts/qa-all.bash` passes
- [ ] Playbook re-run is idempotent
- [ ] All decision-gate answers from Phase 1 are documented in this plan or in research.md
- [ ] Tests pass with >80% line coverage of the new module (95% is hard to hit without raw fixture files)

## Risks & Mitigations

| Risk                                                                              | Impact | Probability | Mitigation                                                                                                                                    |
| --------------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `rawpy` not in Fedora 43 repos                                                    | Low    | Med         | Fall back to `pip install --user rawpy` in playbook; verify in Phase 2                                                                        |
| LibRaw reports different `white_level` than expected for cRAW                     | Med    | Med         | The whole *point* of using `white_level` from the file (vs hardcoding 16383) is to handle this; Phase 2 probes cRAW + uncompressed to confirm |
| Sony A7V file format quirks not in shipping LibRaw                                | Med    | Low         | A7V is current-gen and LibRaw is actively maintained; if it surfaces, lock to a newer LibRaw via a Fedora-version-aware install step          |
| Default thresholds wrong for user's actual shooting style                         | Low    | Med         | Thresholds are fully configurable; defaults are documented and rationale captured; iterate after first real-shoot test in Phase 8             |
| Filename collisions on rename (target name exists)                                | Low    | Low         | Log warning and skip; this only happens if user manually pre-created the suffixed name, which is pathological                                 |
| Lightroom import sees the renamed files as duplicates of a prior import           | Low    | Low         | LR identifies dups by content hash + capture time, not filename. Renaming changes the filename only; the dup detector still works.            |
| `concurrent.futures.ProcessPoolExecutor` deadlock on rawpy import                 | Low    | Low         | Process pool isolates each worker; `if __name__ == "__main__":` guard is standard. Tested in Phase 6.                                         |
| TDD enforcement hook blocks creating `clip-scan` before test file exists          | Low    | High        | Predictable; write test file first per Phase 3. Phase order already enforces this.                                                            |
| User's `play-photography.yml` is already large and adding rawpy install bloats it | Low    | Low         | One new task block; if needed, factor rawpy into a tiny dedicated playbook later. Not a blocker.                                              |

## Notes & Updates

### 2026-05-20

- Plan created from conversation. Initial research scoped via the agent's training data (no web fetch needed for this domain — well-established raw decoding territory).
- Research file [research.md](research.md) captures: existing-tool comparison, threshold rationale, rawpy choice, filename idempotency strategy, pair detection, Lightroom integration, performance estimates, edge cases, deployment plan.
- Decision gate identifies 8 questions for user lock-in.
- User-driven iteration during plan drafting: added the **two-axis threshold design** (`--white-cutoff` / `--black-cutoff` separate from `--white-threshold` / `--black-threshold`) after user pointed out that "exactly at sensor max" is too strict.

### 2026-05-20 (second iteration — weighted score)

- User raised a further refinement: instead of two independent knobs per side, use a continuous weighted score where each pixel contributes proportionally to its closeness to saturation/black-level.
- Performed web research to find prior art (5 WebSearch queries + 7 WebFetch retrievals). Archived in [`references/`](references/).
- Headline finding: Mertens 2007 Exposure Fusion uses a Gaussian well-exposedness weight (`exp(-(I-0.5)²/(2·0.2²))`) as per-pixel quality metric. No existing photography tool implements weighted clipping for *bulk culling* — the gap this tool fills.
- Decision 6 added: weighted-score design supersedes the two-axis design from Decision 3. Decision 3 marked superseded but preserved as iteration history.
- research.md extended with new Section 3 documenting the weighted-score design, formula, defaults, and prior-art justification. Sections 4-13 renumbered (was 3-12); content otherwise unchanged.
- PLAN.md Overview, Goals, Research Summary, Decision Gate, Phase 3 tests, Phase 4/5 task flag-names, and Success Criteria updated to reflect the weighted-score design.
- Defaults locked in: `--white-cutoff 0.95 --white-score 2.0 --black-cutoff 1.05 --black-score 5.0 --gamma 1.0`.

### 2026-05-20 (third iteration — subagent solidity review)

Plan agent ran a critique-only review of PLAN.md + research.md + references/. Found 5 top-priority risks + 4 HIGH + 7 MEDIUM + 6 LOW issues. Material findings absorbed:

**Top-5 risks addressed**:

1. **rawpy install + LibRaw version**: Phase 2 expanded — explicit `dnf info libraw libraw-devel`, `pip show rawpy`, version check against LibRaw ≥ 0.21 for A7V cRAW support
2. **cRAW performance**: success criterion revised from 2 min → 5 min for 500 frames; research.md §7 to be updated with cRAW vs uncompressed split estimate
3. **Gamma test ambiguity**: Phase 3 test explicitly anchors the "midpoint" to `(cutoff + 1.0)/2 × white_level`, not value 0.5
4. **Python test convention gap**: Decision 7 added — `/workspace/tests/clip_scan/` chosen; also added as decision-gate question for user confirmation before Phase 3 starts
5. ~~**Lightroom catalog safety rail**~~: REVERTED later same day — LR has no Linux build on the deploy target. See Decision 9 below for the postmortem.

**HIGH issues addressed**:

- H1 (per-channel black levels): Phase 3 test uses a non-identical 4-tuple to force the implementation to honour per-channel pedestals; Phase 4 returns `black_level_per_channel` not a scalar
- H2 (CFA / Bayer indexing): explicit Phase 3 test and Phase 4 task added for `raw_colors_visible` indexing into per-channel pools
- H3 (filename strip strategy): research.md §5 to be updated — strip only the *trailing* `wclip`/`bclip` tokens (immediately before extension), not any-position
- H4 (sibling rename order): Phase 5 task added — JPG/XMP siblings renamed first, ARW last, so a process kill mid-batch leaves a state that still canonical-stems together

**MEDIUM issues addressed**:

- M1 (60MP A7V default may under-flag): Phase 8 calibration task explicitly inspects top-30/bottom-30 scores on a real shoot
- M2 (Kasson ref pushes tighter cutoff, not looser): research.md §3 to be updated to acknowledge the matrix-post-process argument
- M3 (DNG): Decision 4 expanded to ARW > DNG > HIF > JPG
- M4 (corrupt ARW + valid JPG): Phase 4 test and implementation; sibling JPG used as fallback
- M5 (JPG fallback math): Decision 8 + Phase 4 task — separate JPG cutoffs, documented
- M6 (rawpy install gate is tautological): removed from decision gate, moved to Phase 2 probe-then-decide
- M7 (no profiling task): Phase 8 performance benchmark task added

**LOW issues addressed**:

- L1 (XMP gate speculative): decision gate updated to ask if v1 includes XMP or defers
- L2 (zero-pixels-in-ramp test): Phase 3 test added
- L3 (NaN/inf): Phase 3 test added
- L4 (JSON schema): defined in-line in Phase 5 (per-channel scores, action, error)
- L5 (process pool startup overhead): research.md §9 to be updated re: long-lived workers for ftp-camera integration
- L6 (cRAW vs uncompressed): Phase 8 explicitly uses both sample types

**research.md updates applied (extension, not rewrite)**:

- §3 per-channel handling extended: CFA Bayer indexing via `raw_colors_visible`, per-channel `black_level_per_channel` 4-tuple honoured (per H1 + H2)
- §3 cutoff caveats subsection added: M1 (60MP A7V may under-flag, Phase 8 calibrates) and M2 (Kasson's matrix argument actually supports the looser cutoff direction)
- §5 strip-strategy clarified: trailing-only, with updated pseudocode (per H3)
- §7 performance section split into uncompressed-ARW vs cRAW estimates; 500-frame target revised to 2-5 minutes for cRAW (per #2)
- §9 ftp-camera integration: long-lived daemon mode note added for per-file invocation overhead (per L5)

### Status: PLAN.md + research.md fully updated with all subagent findings. Decision Gate awaiting user confirmation. Once confirmed, code execution may begin from Phase 2.

### 2026-05-20 (fourth iteration — initial execution landing)

User directive: `execute`. Decision Gate questions implicitly accepted with the proposed defaults documented in the plan. Code work in the CCY container:

- ✅ Phase 3: TDD test suite landed at `/workspace/tests/clip_scan/test_clip_scan.py` (Decision 7 path). Conftest at `/workspace/tests/clip_scan/conftest.py` loads the deployable script via `importlib` so functions are importable without packaging. Covers all Phase 3 test cases: filename canonicalisation (trailing-only strip per H3, case-insensitive flags, dots-in-stem preserved, non-trailing tokens not stripped), rebuild canonical ordering, idempotency, pair grouping (case-insensitive), weighted-score math (saturation→100, cutoff→0, midpoint→50%, 2% boundary, equivalence 1%-at-sat ≡ 2%-at-midpoint, exact-zero below cutoff, gamma=1.0/2.0 midpoint check), per-channel max across R/G1/B/G2, per-channel black-level honoured (H1), CFA pool extraction (H2), argparse defaults + override behaviour.
- ✅ Phase 3: implementation landed at `/workspace/files/home/.local/bin/clip-scan`. Module-level functions: `parse_clip_flags`, `rebuild_name`, `group_by_stem`, `score_white`, `score_black`, `extract_channel_pools`, `analyse_array`, `analyse_raw`, `analyse_jpg`, `find_lightroom_catalog`, `scan_files`, `pick_source`, `compute_verdict`, `plan_rename_order`, `execute_renames`, `build_parser`, `main`.
- ✅ Phase 4: rawpy integration via lazy import inside `analyse_raw`; LibRaw decode errors propagate up `_analyse_group` which falls back to a sibling JPG when available. Pillow lazy-imported inside `analyse_jpg`. Per-channel `black_level_per_channel` is honoured (4-tuple, not collapsed). JPG path uses separate `_score_black_absolute` so the multiplicative black ramp doesn't degenerate at `black_level=0`.
- ✅ Phase 5: CLI surface complete. Flags wired: `-w/--white-score`, `--white-cutoff`, `-b/--black-score`, `--black-cutoff`, `--gamma`, `--jpg-white-cutoff`, `--jpg-black-cutoff`, `-n/--dry-run`, `-a/--apply`, `-j/--jobs`, `-v/--verbose`, `--no-follow-symlinks`, `--json`. `--apply` overrides `--dry-run` when both passed. Sibling-first rename order implemented (raw renamed last so a mid-batch kill leaves siblings still canonical-stem-grouping). Collision handling logs and skips. JSON output to the schema documented in PLAN.md Phase 5.
- ✅ Phase 6: `ProcessPoolExecutor` parallelism wired with `--jobs` (default `nproc`); single-process fallback for `--jobs 1` or single-group batches.
- ✅ Phase 7: `playbooks/imports/optional/common/play-photography.yml` updated. Block `tags: clip-scan` probes `dnf info python3-rawpy`, installs via system package if available, else `pip --user rawpy`. `python3-pillow` installed via system package. Script copied to `~/.local/bin/clip-scan` (0755). Installation summary `debug` extended with the `clip-scan` line.
- ⬜ Phase 2: host probes deferred — must be run on the host (CCY container has no real `dnf info` resolution against Fedora repos and no representative ARW sample). User to run the probe commands listed in PLAN.md Phase 2 and feed findings back into research.md "host findings" section.
- ⬜ Phase 8: QA + manual tests + performance benchmark + defaults calibration — host-only. Once Phase 2 confirms rawpy + LibRaw versions, run `./scripts/qa-all.bash` and the manual test plan.

XMP sidecars deferred to v2 per Decision Gate question L1 — `scan_files` excludes them.

Next user-side actions (host): (1) run Phase 2 probes; (2) deploy via `ansible-playbook playbooks/imports/optional/common/play-photography.yml`; (3) run Phase 8 manual tests; (4) feed back any default calibration adjustments.

### 2026-05-20 (fifth iteration — Lightroom safety rail removed)

User correctly pointed out that the `fedora-desktop` repo has no Lightroom on it — Adobe LR has no Linux build, so the `.lrcat` parent-walk safety rail added per subagent review #5 was dead code that could never fire on the deploy target. The subagent missed the environmental context.

Reverted:

- `clip-scan`: deleted `find_lightroom_catalog()`, the catalog branch in `main()`, the `--force-on-catalog` argparse flag, and the epilog warning text
- `tests/clip_scan/test_clip_scan.py`: deleted `TestCatalogSafetyRail` (three cases)
- `play-photography.yml`: dropped "Pre-Lightroom-import" framing from the install summary and the task comment
- PLAN.md: Decision 9 marked REVERTED with postmortem; decision-gate question struck-through; Phase 5 / Phase 8 catalog tasks removed; Risks-table row removed; subagent-#5 summary marked reverted

Lesson banked for next subagent review: state the deploy target's actual environment ("Fedora desktop, no Lightroom installed") in the prompt so the reviewer doesn't add controls for tools that don't run there.

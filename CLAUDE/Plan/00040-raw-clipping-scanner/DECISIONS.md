# Plan 00040: Decisions, locked defaults and risk register

Supporting document for [PLAN.md](PLAN.md). Holds the durable decision rationale
that the lean plan links to. The verbatim pre-slimming plan, including the dated
iteration narrative, is in [PLAN_archive.md](PLAN_archive.md). The underlying
research is in [research.md](research.md) with archived sources under
[references/](references/).

## Locked defaults (weighted clipping score)

The design is research.md Section 3 (which supersedes Section 2). Prior art is
`references/mertens-exposure-fusion.md`.

| Flag                   | Default | Meaning                                                                                       |
| ---------------------- | ------- | --------------------------------------------------------------------------------------------- |
| `--white-cutoff RATIO` | 0.95    | Linear weighting ramp starts at `value = RATIO × white_level`; pixels below this contribute 0 |
| `--white-score PCT`    | 2.0     | Flag `.wclip` when the weighted white-clip score exceeds PCT                                  |
| `--black-cutoff RATIO` | 1.01    | Linear weighting ramp starts at `value = RATIO × black_level`; pixels above this contribute 0 |
| `--black-score PCT`    | 5.0     | Flag `.bclip` when the weighted black-clip score exceeds PCT                                  |
| `--gamma FLOAT`        | 1.0     | Power applied to the ramp (1.0 = linear; >1 punishes near-max more)                           |
| `--jpg-white-cutoff`   | 0.95    | JPG-only fallback path, against 0-255 (≈ 243/255)                                             |
| `--jpg-black-cutoff`   | 0.05    | JPG-only fallback path, against 0-255 (≈ 13/255)                                              |

For each pixel value `v`, weight is 1.0 at saturation, 0.0 at the cutoff,
linearly interpolated in between. The per-image score is the mean weight across
pixels, expressed as percent; per-channel scores are computed and the max across
R/G1/B/G2 is used ("any channel clipped"). A score of 2.0 means "the equivalent
of 2% fully-blown pixels": 1% at literal max plus 2% at the ramp midpoint scores
exactly 2.0.

**Why the black cutoff is 1.01 and not the originally planned 1.05.** On the
A7V the 1.05 ramp covers `[512, 537]`, which is exactly where the read-noise
floor sits on a normally exposed frame, so the score measured camera noise
rather than crushed shadows. 1.01 narrows the ramp to `[512, 517]`. The
research.md Section 3 justification for 1.05 is out of date on the black side
and needs revisiting with more shoots of data.

**Pixels below `black_level` are excluded from the black ramp entirely.** Every
Sony A7V ARW carries roughly 10% of pixels at literal value 0 (masked or
optical-black border pixels that LibRaw stores as zero). Including them in the
ramp gave every frame a 10% baseline bclip score. `score_black` uses
`in_ramp = (values >= black_level) & (values <= cutoff_value)`.

**Why weighted score over a binary count.** A pixel at 99% saturation looks
identical to one at 100% in the final image because the tone curve flattens near
saturation. The weighted score matches photographer perception ("how blown does
this look") instead of an arbitrary at-max line.

**Other locked picks.** Decode is Python + `rawpy` (LibRaw binding); dcraw is
moribund, ImageMagick destroys raw saturation via the demosaic, LibRaw-direct is
overkill. Idempotent naming is strip-then-rebuild on trailing `wclip`/`bclip`
tokens only, reassembled in canonical `wclip` then `bclip` order. Pairs are
grouped by canonical stem, the best source is analysed and the verdict applies to
all siblings. Lightroom filtering is Library, Filter bar, "Filename contains
`.wclip.`".

## Decision gate (Phase 1)

The user's `execute` directive accepted the proposed defaults implicitly. The
questions are kept here because Phase 1 remains unticked in the task tree.

- Tool name: `clip-scan` (aligns with the `raw-prune` sibling).
- Defaults: the table above.
- Pair handling: analysis priority ARW > DNG > HIF > JPG, verdict applies to all stem-siblings.
- Safety default: dry-run, explicit `--apply` to commit.
- Lightroom catalog safety rail: removed, see Decision 9.
- Python test location: `tests/clip_scan/` at repo root with `pytest`, see Decision 7. Had to be settled before Phase 3 or the TDD hook blocks implementation.
- JPG-only folders and corrupt-ARW fallback: analyse the JPG best-effort with separate JPG cutoffs, see Decision 8.
- Output format: human-readable dry-run table by default, `--json` with the schema below.
- XMP sidecars: deferred to v2 because the user does not currently produce them. `scan_files` excludes them.
- rawpy install path is not a decision: it is the mechanical outcome of the Phase 2 probe (system package if available, else `pip install --user`).

## Technical decisions

### Decision 1: Filename rename over XMP sidecars

Options: XMP sidecar (Lightroom-native, needs LR configured to read sidecars);
filename rename (universal, works in any tool, greppable); both.

Chose filename rename. The user's motivation ("subsequent organisation easy
somehow") maps directly to the filename being the universal channel. Because the
tool runs pre-import, the catalog-missing-files cost of renames is zero.

### Decision 2: Analyse the raw Bayer mosaic, not demosaiced output

Options: decode, demosaic, tone curve, histogram (what GUI tools show); or
analyse the Bayer mosaic directly.

Chose the mosaic. The demosaic plus tone curve systematically over-reports
clipping because the curve pushes near-saturation values upward. Mosaic
analysis tells the truth about whether the sensor clipped, which is what
RawDigger does.

### Decision 3: Two-axis thresholds (superseded by Decision 6)

The first design had one knob per side ("% of pixels clipped"). The user asked
whether the value threshold itself should be adjustable, which produced a
two-axis design: a value cutoff plus a count threshold. Strict "must equal max"
misses frames at 99% saturation with no recoverable detail. Kept as iteration
history; Decision 6 subsumes it.

### Decision 4: ARW > DNG > HIF > JPG analysis priority for pairs

Options: analyse each sibling independently, or analyse the highest-fidelity
source and apply the verdict to all.

Chose the latter. Raw sensor data is the truth about exposure; a JPG histogram
describes Sony's tone mapping. Analysing both costs time and can produce
contradictory verdicts. DNG was added after review because the A7V can output
DNG in some workflows and it also decodes via rawpy.

### Decision 5: Dry-run by default

Renames are a side effect on the user's photo library, so they require explicit
consent via `--apply`. This matches the implicit contract of `raw-prune`.

### Decision 6: Weighted clipping score (final, supersedes Decision 3)

During planning the user asked whether a pixel at 100% should count more than
one at 98%. That observation collapses the two-axis design into a single
weighted score: continuous per-pixel weight, per-image mean, threshold on the
score. It has a clean interpretation, trades pixel count against severity in one
number, and is backed by Mertens 2007 well-exposedness weighting. `--gamma` lets
power users curve the ramp.

### Decision 7: Python tests live in `tests/clip_scan/`

The repo had no Python test convention and the TDD hook blocks production files
without tests. Options: co-locate next to the script, top-level `tests/`,
inside the playbook directory, or inside `files/home/.local/bin/`.

Chose top-level `tests/clip_scan/`. It is standard pytest layout, keeps tests
out of the `files/` tree that Ansible copies verbatim to the user's home, and
leaves room for shared pytest config. `conftest.py` loads the extensionless
script via `SourceFileLoader`. There is no `__init__.py`: with one present,
pytest treated the directory as a package and collided with the `clip_scan`
module registered by conftest.

### Decision 8: JPG fallback uses separate thresholds

The raw-path cutoff (0.95 of `white_level`) cannot apply to a JPG: max is 255
with no metadata, and the tone curve differs per camera. Options: reuse the raw
defaults and hope; separate `--jpg-white-cutoff` / `--jpg-black-cutoff` flags;
refuse JPG-only files.

Chose separate flags. JPG scores are best-effort and not comparable to raw
scores. The JPG path uses an absolute black ramp (`_score_black_absolute`) so
the multiplicative ramp does not degenerate at `black_level = 0`. The `--json`
output carries an `analysis_source` field ("raw" or "jpg").

### Decision 9: Lightroom catalog safety rail, reverted

A review subagent added a `.lrcat` parent-walk safety rail with a
`--force-on-catalog` override. Adobe Lightroom has no Linux build, so a catalog
can never legitimately appear on the deploy target. Removed the function, the
flag, the `main()` branch, the epilog text, three tests, the related plan tasks
and the risk row. Not to be re-added in v1.

Lesson for future reviews: state the deploy target's actual environment
("Fedora desktop, no Lightroom installed") in the reviewer prompt.

### Decision 10: Progress output via `submit` + `as_completed`

`ProcessPoolExecutor.map` batched all futures and printed nothing until the
whole shoot finished, so the tool looked hung on a 347-frame run. Each group's
result now prints to stderr as it lands, a one-line header precedes the workers,
the summary prints below the listing so it stays on screen, and `-q/--quiet`
exists for scripted use.

## `--json` output schema (Phase 5)

```json
{
  "version": 1,
  "config": {"white_cutoff": 0.95, "white_score": 2.0, "black_cutoff": 1.01, "black_score": 5.0, "gamma": 1.0},
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

## Review findings absorbed into the task tree

A critique-only subagent review of the plan and research produced the items
referenced by tag in the task tree:

- **H1** per-channel black levels: tests use a non-identical 4-tuple; `analyse_raw` returns `black_level_per_channel`, never a scalar.
- **H2** CFA indexing: per-channel pools via `raw_colors_visible`; R and B pools about 25% each, G about 50%.
- **H3** strip only trailing `wclip`/`bclip` tokens.
- **H4** siblings renamed before the raw so a mid-batch kill still stems together.
- **M1** 60MP A7V defaults may under-flag: Phase 8 calibration inspects top-30 and bottom-30 scores.
- **M2** Kasson's matrix argument supports the looser cutoff direction (research.md Section 3).
- **M3** DNG in the priority chain. **M4** corrupt ARW falls back to sibling JPG. **M5** separate JPG cutoffs.
- **M6** rawpy install is a probe outcome, not a gate question. **M7** performance benchmark task.
- **L1** XMP deferred. **L2** exact 0.0 below cutoff. **L3** clear error on `white_level == 0` or `black_level >= white_level`.
- **L4** JSON schema defined. **L5** long-lived worker note for ftp-camera (research.md Section 9). **L6** cRAW and uncompressed samples both tested.
- **#2** cRAW decode is 1.5-2.5s per 60MP frame, so the 500-frame criterion moved from 2 to 5 minutes pending measurement.
- **#3** gamma test anchors "midpoint" at `(cutoff + 1.0) / 2 × white_level`.

## Risks and mitigations

| Risk                                                              | Impact | Probability | Mitigation                                                                                                    |
| ----------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------- |
| `rawpy` not in Fedora repos                                       | Low    | Med         | Playbook falls back to `pip install --user rawpy`; Phase 2 verifies                                           |
| LibRaw reports an unexpected `white_level` for cRAW               | Med    | Med         | The tool reads `white_level` from the file rather than hardcoding 16383; Phase 2 probes cRAW and uncompressed |
| Sony A7V format quirks not in shipping LibRaw                     | Med    | Low         | Lock to a newer LibRaw via a Fedora-version-aware install step if it surfaces                                 |
| Default thresholds wrong for the user's shooting style            | Low    | Med         | Fully configurable; Phase 8 calibrates on a real shoot                                                        |
| Filename collision on rename                                      | Low    | Low         | Log a warning and skip; only happens if the suffixed name was pre-created by hand                             |
| Lightroom sees renamed files as duplicates of a prior import      | Low    | Low         | LR identifies duplicates by content hash and capture time, not filename                                       |
| `ProcessPoolExecutor` deadlock on rawpy import                    | Low    | Low         | Workers are isolated processes with the `__main__` guard; tested in Phase 6                                   |
| TDD hook blocks creating `clip-scan` before its test file exists  | Low    | High        | Phase order writes the test file first                                                                        |
| `play-photography.yml` grows further with the rawpy install block | Low    | Low         | One task block; factor into a dedicated playbook later if needed                                              |

# Plan 00040: `clip-scan` — Raw Clipping Pre-Lightroom Scanner

**Status**: In Progress (Phases 3-7 code landed in-container; Phase 2 host probes and Phase 8 manual tests deferred to the host)
**Created**: 2026-05-20
**Owner**: joseph
**Priority**: Medium
**Type**: Feature (new tool)

## Overview

A standalone CLI tool, `clip-scan`, scans a directory of Sony ARW (plus sibling JPG / HIF) photographs, analyses each frame's raw Bayer histogram, and renames files whose highlight or shadow clipping exceeds tunable thresholds. The flag lives in the filename: `DSC123.ARW` becomes `DSC123.wclip.ARW`, `DSC123.bclip.ARW` or `DSC123.wclip.bclip.ARW`. It runs as a preprocess step before Lightroom import, so the renamed names are what Lightroom records, and the user triages a shoot by filtering on `.wclip.` / `.bclip.`.

Each side uses a **weighted clipping score**: pixels near sensor saturation or black level contribute via a linear ramp from a configurable cutoff to the extreme, and the file is flagged when the per-image score exceeds a configurable threshold. Decode is Python + `rawpy`; dry-run is the default with explicit `--apply`.

Supporting documents:

- [DECISIONS.md](DECISIONS.md): locked defaults, decision-gate answers, Decisions 1 to 10, `--json` schema, review-finding tags (H1 to L6) used in the task tree, risk register.
- [research.md](research.md): tool landscape, score math, rawpy, rename strategy, performance, deployment. Backing sources in [references/](references/).
- [PLAN_archive.md](PLAN_archive.md): the full pre-slimming plan, verbatim, including all dated iteration notes.
- [JOURNAL/](JOURNAL/): activity log from 2026-09-02 onward.

## Goals

- Standalone CLI invokable as `clip-scan [DIR]` or `clip-scan [FILE]`
- Bulk-flag ARW (and sibling JPG/HIF) files via filename rename when the weighted clipping score exceeds the threshold
- Weighted-score design per side: linear ramp from cutoff to extreme, per-image score is the mean weight as "equivalent percent of fully-clipped pixels", thresholded to decide the flag
- Idempotent: re-running with different thresholds produces the correct new name, never `.wclip.wclip`
- Dry-run by default, explicit `--apply` to commit changes
- Pair-aware: ARW is the analysis source, verdict propagates to siblings
- Parallel across CPU cores via `concurrent.futures.ProcessPoolExecutor`
- Composable: accepts a directory (recursive) and a single file (one-shot mode for future ftp-camera integration)
- Deploys via the existing `play-photography.yml` playbook alongside `raw-prune`

## Non-Goals

- Focus / sharpness / blur scoring
- AI-based or semantic culling (closed eyes, duplicates, scene quality)
- Lightroom catalog integration (XMP sidecar writes, ratings, colour labels); the user chose filename rename over sidecars
- File deletion or moving; deletion is `raw-prune`'s job
- Demosaiced histogram analysis; the raw Bayer mosaic is analysed for sensor-accurate clipping
- Cross-camera coverage beyond Sony A7V in v1; LibRaw decodes other formats but defaults are calibrated for Sony 14-bit raw
- XMP sidecar renaming (deferred to v2)

## Context & Background

- Host: Fedora 43 / GNOME 49.6 / Wayland. Camera: Sony A7V (ARW + JPG, 14-bit raw, cRAW by default).
- Upstream: `ftp-camera` (Plans 00038 / 00039) sorts into `JPG/` and `RAW/` by date. Downstream: Lightroom import.
- Neighbour: `raw-prune` at `files/home/.local/bin/raw-prune`, deployed by `play-photography.yml`, sets the conventions for scope, naming, deployment, dry-run and colour output.
- First tool in the repo that reads raw pixel data; nothing else links against LibRaw.
- A future Plan 00039 follow-up may wire `clip-scan --apply --quiet` into the per-upload sort path; the single-file invocation must support it.

## Tasks

### Phase 1: Decision Gate

Questions and their proposed answers are in [DECISIONS.md](DECISIONS.md#decision-gate-phase-1).

- [ ] ⬜ User reviews this plan and answers the Decision Gate questions
- [ ] ⬜ Update plan with finalised tool name, default values, and any scope adjustments

### Phase 2: Host probe & dependency verification

- [ ] ⬜ On host: `dnf info python3-rawpy` to check Fedora packaging (historically not in Fedora repos; pip-user path likely)
- [ ] ⬜ On host: `dnf info libraw libraw-devel`; confirm system LibRaw exists and capture its version
- [ ] ⬜ On host: `pip show rawpy` after install; capture rawpy's bundled LibRaw version, **must be ≥ 0.21 for Sony A7V cRAW decode**
- [ ] ⬜ On host: `python3 -c "import rawpy; r=rawpy.imread('/tmp/test.ARW'); print(r.white_level, r.black_level_per_channel, r.raw_colors_visible.shape, r.raw_pattern)"` against a representative A7V ARW to confirm metadata and CFA pattern access (H2)
- [ ] ⬜ On host: capture `white_level` and the full `black_level_per_channel` 4-tuple (R, G1, B, G2) for both cRAW and uncompressed ARW samples; document in research.md "host findings"; verify whether the per-channel values differ (H1)
- [ ] ⬜ On host: time the decode, `time python3 -c "import rawpy; r=rawpy.imread('test_craw.ARW'); _ = r.raw_image_visible.copy()"`, for both cRAW and uncompressed (#2)
- [ ] ⬜ Lock the rawpy install path from the probe: system package if present, else `pip install --user rawpy` (M6)
- [ ] ⬜ Confirm Pillow availability for the JPG-only fallback path

### Phase 3: TDD Implementation — core analysis

- [x] ✅ Create test file, landed as `tests/clip_scan/test_clip_scan.py` with its `conftest.py`, the repo's first Python test directory
- [ ] ⬜ Write failing test: filename canonicalisation (`DSC123.wclip.bclip.ARW` gives stem `DSC123`, ext `ARW`, flags `(True, True)`)
- [ ] ⬜ Write failing test: rebuild (stem `DSC123`, ext `ARW`, flags `(True, False)` gives `DSC123.wclip.ARW`)
- [ ] ⬜ Write failing test: re-run produces the same name, not stacked suffixes
- [ ] ⬜ Write failing test: dots-in-stem survive (`Holiday.beach.DSC123.ARW` keeps stem `Holiday.beach.DSC123`)
- [ ] ⬜ Write failing test: pair grouping (`DSC123.ARW`, `DSC123.JPG`, `DSC123.ARW.xmp` group under stem `DSC123`)
- [ ] ⬜ Write failing test: ramp math; all pixels at saturation scores 100.0, all at the cutoff scores 0.0, uniform halfway scores 50.0
- [ ] ⬜ Write failing test: white-score boundary; 2% of pixels at saturation, rest at zero, scores exactly 2.0
- [ ] ⬜ Write failing test: equivalence; 1% at saturation plus 2% at the ramp midpoint `(cutoff + 1.0) / 2 × white_level` scores 2.0
- [ ] ⬜ Write failing test: per-channel max across R/G/B/G2 channel pools
- [ ] ⬜ Write failing test: black-score math mirroring the white side, using a `black_level_per_channel` 4-tuple whose values are NOT identical (H1)
- [ ] ⬜ Write failing test: gamma; uniform array at the ramp midpoint scores 0.25 with gamma=2.0 versus 0.5 with gamma=1.0 (#3)
- [ ] ⬜ Write failing test: uniform array entirely below the cutoff scores exactly 0.0, not floating-point dust (L2)
- [ ] ⬜ Write failing test: `white_level == 0` or `black_level >= white_level` raises a clear error rather than NaN (L3)
- [ ] ⬜ Write failing test: CFA indexing; given a synthetic Bayer mosaic and `raw_pattern`, per-channel pools are correct, R and B about 25% each, G about 50% (H2)
- [ ] ⬜ Implement `canonical_stem()`, `rebuild_name()`, `group_by_stem()`, `score_white_per_channel()`, `score_black_per_channel()`, `analyse_array()` to pass tests
- [ ] ⬜ Refactor for clarity

### Phase 4: TDD Implementation — rawpy integration

- [ ] ⬜ Write failing test: `analyse_raw()` end-to-end against a fixture ARW (small file in a fixtures directory under `tests/clip_scan/`, or skipped if missing)
- [ ] ⬜ Write failing test: Bayer extraction on a real ARW via `r.raw_colors_visible` yields four channel arrays whose union is the full visible frame (H2)
- [ ] ⬜ Implement `analyse_raw(path)` returning `(white_score_pct, black_score_pct, white_level, black_level_per_channel, per_channel_scores)`, honouring per-channel black level (H1)
- [ ] ⬜ Write failing test: corrupt-ARW fallback to sibling JPG when `rawpy.imread()` raises `LibRawError` (M4)
- [ ] ⬜ Write failing test: JPG-only fallback via Pillow with separate default thresholds (Decision 8)
- [ ] ⬜ Implement JPG fallback scoring against 0-255 with `--jpg-white-cutoff 0.95` and `--jpg-black-cutoff 0.05`; document that JPG scores are not comparable to raw scores (M5)
- [ ] ⬜ Handle LibRaw decode errors gracefully: log per file, try the sibling JPG first, then skip and continue the batch

### Phase 5: TDD Implementation — CLI surface

- [ ] ⬜ Write failing test: argparse parses `--white-cutoff 0.93 --white-score 1.5 --gamma 1.5 --apply DIR`
- [ ] ⬜ Implement CLI dispatch
- [ ] ⬜ Write failing test: dry-run prints proposed renames without filesystem changes
- [ ] ⬜ Implement dry-run output
- [ ] ⬜ Write failing test: sibling rename order; JPG/XMP siblings renamed before the raw so a mid-batch kill still canonical-stems together (H4)
- [ ] ⬜ Implement apply mode using `pathlib.Path.rename` with sibling-first ordering
- [ ] ⬜ Write failing test: collision handling (target name already exists)
- [ ] ⬜ Implement collision logging
- [ ] ⬜ Write failing test: `--json` output matches the schema in [DECISIONS.md](DECISIONS.md#--json-output-schema-phase-5) (L4)
- [ ] ⬜ Implement JSON output to the documented schema

### Phase 6: TDD Implementation — parallel execution

- [ ] ⬜ Write failing test: `ProcessPoolExecutor` returns results from N parallel workers without deadlock
- [ ] ⬜ Implement parallel scan
- [ ] ⬜ Write failing test: `--jobs 1` falls back to single-process for debugging
- [ ] ⬜ Implement single-process fallback

### Phase 7: Ansible integration

- [ ] ⬜ Add task block to `playbooks/imports/optional/common/play-photography.yml` to install rawpy (path chosen in Phase 2)
- [ ] ⬜ Add task to deploy `files/home/.local/bin/clip-scan` (mode 0755, owner `user_login`)
- [ ] ⬜ Add the new script to the playbook's installation summary `debug` message
- [ ] ⬜ Verify playbook idempotency: dry-run, first apply (changed), second apply (ok)

### Phase 8: QA

- [ ] ⬜ `./scripts/qa-all.bash` clean
- [ ] ⬜ Manual host test plan:
  \- [ ] ⬜ Place 10 known-good and 10 known-clipped ARW files in a scratch dir, using cRAW samples explicitly (L6)
  \- [ ] ⬜ Place 10 uncompressed-ARW samples too; compare scores between cRAW and uncompressed of the same scene
  \- [ ] ⬜ Run `clip-scan scratch/`; verify dry-run lists exactly the clipped ones
  \- [ ] ⬜ Run `clip-scan --apply scratch/`; verify rename happened, siblings renamed too
  \- [ ] ⬜ Run `clip-scan --apply scratch/` again; verify no double-rename
  \- [ ] ⬜ Run with non-default thresholds; verify flagging changes accordingly
  \- [ ] ⬜ Single-file mode: `clip-scan --apply scratch/DSC123.ARW`
  \- [ ] ⬜ Corrupt / truncated ARW; verify graceful skip with error log
  \- [ ] ⬜ Corrupt ARW plus valid sibling JPG; verify JPG fallback runs (M4)
  \- [ ] ⬜ JPG-only file (no sibling ARW); verify fallback analysis runs
- [ ] ⬜ Performance benchmark (M7 / #2):
  \- [ ] ⬜ Place a real 500-frame cRAW shoot in a scratch dir
  \- [ ] ⬜ Time `clip-scan --apply --jobs $(nproc) scratch/`
  \- [ ] ⬜ Document the measured time against the success criterion and revise the criterion if cRAW decode dominates
- [ ] ⬜ Defaults calibration on a real shoot (M1):
  \- [ ] ⬜ Run `clip-scan --json` over a recent shoot
  \- [ ] ⬜ Manually inspect the top-30 and bottom-30 scores
  \- [ ] ⬜ Confirm the cutoffs and thresholds give useful triage on a 60MP A7V; tighten if they under-flag
  \- [ ] ⬜ Document any threshold adjustments in research.md "host findings"
- [ ] ⬜ Live workflow test:
  \- [ ] ⬜ Run `clip-scan --apply` over a real shoot folder before Lightroom import
  \- [ ] ⬜ Import to Lightroom
  \- [ ] ⬜ Confirm the "Filename contains `.wclip.`" filter shows the expected frames

### Phase 9: Documentation

- [ ] ⬜ `clip-scan --help` output is complete and accurate
- [ ] ⬜ One-line entry added to the photography playbook's installation summary
- [ ] ⬜ Optionally add a short paragraph to `docs/` if a photography workflow guide exists there

## Dependencies

- Depends on: Plans 00038 / 00039 (`ftp-camera`) for the upstream folder layout; `play-photography.yml` for deployment.
- Blocks: a future Plan 00039 follow-up wiring `clip-scan` into the per-upload sort path.

## Success Criteria

- [ ] `clip-scan DIR` produces a clear list of which files *would* be renamed and why (per-file weighted white-score %, black-score %, per-channel max)
- [ ] `clip-scan --apply DIR` performs the renames atomically; ARW + JPG siblings stay grouped
- [ ] Re-running `clip-scan --apply DIR` is a no-op
- [ ] Changing thresholds and re-running produces correctly updated names without stacking suffixes
- [ ] Performance: a 500-frame shoot processes in under 5 minutes on an 8-core box (revised from 2 minutes for cRAW; Phase 8 measures the real number)
- [ ] Lightroom import workflow: renamed files are filterable via the `.wclip.` / `.bclip.` text filter
- [ ] `./scripts/qa-all.bash` passes
- [ ] Playbook re-run is idempotent
- [ ] All decision-gate answers from Phase 1 are documented in DECISIONS.md or research.md
- [ ] Tests pass with >80% line coverage of the new module

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00040-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan drafted, weighted-score design locked, subagent review absorbed (see PLAN_archive.md, Notes & Updates, iterations one to three)
- Phases 3 to 7 code landed in the CCY container: `files/home/.local/bin/clip-scan`, `tests/clip_scan/`, `play-photography.yml` block tagged `clip-scan`
- Lightroom catalog safety rail removed (Decision 9)
- Real-shoot calibration: progress output fix, black-cutoff default 1.01, sub-`black_level` pixels excluded from the ramp; flagged frames 33 of 347
- Plan slimmed and archived verbatim to PLAN_archive.md; supporting detail in DECISIONS.md
- Pending: Phase 2 host probes, host deploy via `play-photography.yml`, Phase 8 manual tests and benchmark, tick reconciliation of Phases 3 to 7 against the landed code

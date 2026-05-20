"""Tests for clip-scan core analysis and filename logic.

Covers Phase 3 of CLAUDE/Plan/00040-raw-clipping-scanner/PLAN.md.
The clip_scan module is loaded by conftest.py via importlib from the
deployable script at files/home/.local/bin/clip-scan, then registered
in sys.modules under the name "clip_scan".
"""

from __future__ import annotations

import pathlib
import sys

import numpy as np
import pytest

# conftest.py populates sys.modules["clip_scan"]; pull it in by that name.
clip_scan = sys.modules["clip_scan"]


# ─────────────────────────────────────────────────────────────────────────────
# Filename canonicalisation
# ─────────────────────────────────────────────────────────────────────────────


class TestParseClipFlags:
    def test_no_flags(self):
        assert clip_scan.parse_clip_flags("DSC123.ARW") == ("DSC123", "ARW", False, False)

    def test_wclip_only(self):
        assert clip_scan.parse_clip_flags("DSC123.wclip.ARW") == (
            "DSC123",
            "ARW",
            True,
            False,
        )

    def test_bclip_only(self):
        assert clip_scan.parse_clip_flags("DSC123.bclip.ARW") == (
            "DSC123",
            "ARW",
            False,
            True,
        )

    def test_both_flags_canonical_order(self):
        assert clip_scan.parse_clip_flags("DSC123.wclip.bclip.ARW") == (
            "DSC123",
            "ARW",
            True,
            True,
        )

    def test_both_flags_reverse_order(self):
        # Both trailing tokens recognised regardless of order; canonical
        # rebuild fixes the ordering.
        assert clip_scan.parse_clip_flags("DSC123.bclip.wclip.ARW") == (
            "DSC123",
            "ARW",
            True,
            True,
        )

    def test_dots_in_stem_preserved(self):
        assert clip_scan.parse_clip_flags("Holiday.beach.DSC123.ARW") == (
            "Holiday.beach.DSC123",
            "ARW",
            False,
            False,
        )

    def test_dots_in_stem_with_flags(self):
        assert clip_scan.parse_clip_flags("Holiday.beach.DSC123.wclip.ARW") == (
            "Holiday.beach.DSC123",
            "ARW",
            True,
            False,
        )

    def test_non_trailing_clip_token_is_not_stripped(self):
        # A clip-looking token that's NOT trailing belongs to the user's
        # own naming and must be preserved.
        assert clip_scan.parse_clip_flags("DSC123.bclip.report.ARW") == (
            "DSC123.bclip.report",
            "ARW",
            False,
            False,
        )

    def test_case_insensitive_flag_match(self):
        assert clip_scan.parse_clip_flags("DSC123.WCLIP.BCLIP.ARW") == (
            "DSC123",
            "ARW",
            True,
            True,
        )

    def test_mixed_case_extension_preserved(self):
        # We don't normalise the extension case.
        assert clip_scan.parse_clip_flags("DSC123.wclip.arw") == (
            "DSC123",
            "arw",
            True,
            False,
        )


class TestRebuildName:
    def test_no_flags(self):
        assert clip_scan.rebuild_name("DSC123", "ARW", False, False) == "DSC123.ARW"

    def test_wclip_only(self):
        assert clip_scan.rebuild_name("DSC123", "ARW", True, False) == "DSC123.wclip.ARW"

    def test_bclip_only(self):
        assert clip_scan.rebuild_name("DSC123", "ARW", False, True) == "DSC123.bclip.ARW"

    def test_both_flags_canonical_order(self):
        # wclip ALWAYS before bclip.
        assert (
            clip_scan.rebuild_name("DSC123", "ARW", True, True) == "DSC123.wclip.bclip.ARW"
        )

    def test_dots_in_stem_preserved(self):
        assert (
            clip_scan.rebuild_name("Holiday.beach.DSC123", "ARW", True, False)
            == "Holiday.beach.DSC123.wclip.ARW"
        )


class TestIdempotency:
    def test_reparse_then_rebuild_unchanged(self):
        original = "DSC123.wclip.bclip.ARW"
        stem, ext, w, b = clip_scan.parse_clip_flags(original)
        assert clip_scan.rebuild_name(stem, ext, w, b) == original

    def test_apply_new_verdict_to_already_flagged(self):
        # File already has wclip+bclip; new verdict is wclip only — bclip
        # must be dropped, not stacked.
        stem, ext, _, _ = clip_scan.parse_clip_flags("DSC123.wclip.bclip.ARW")
        assert clip_scan.rebuild_name(stem, ext, True, False) == "DSC123.wclip.ARW"

    def test_apply_no_verdict_to_already_flagged_strips_flags(self):
        stem, ext, _, _ = clip_scan.parse_clip_flags("DSC123.wclip.bclip.ARW")
        assert clip_scan.rebuild_name(stem, ext, False, False) == "DSC123.ARW"


# ─────────────────────────────────────────────────────────────────────────────
# Pair grouping
# ─────────────────────────────────────────────────────────────────────────────


class TestGroupByStem:
    def test_groups_raw_jpg_siblings(self, tmp_path):
        a = tmp_path / "DSC123.ARW"
        b = tmp_path / "DSC123.JPG"
        c = tmp_path / "DSC124.ARW"
        for p in (a, b, c):
            p.touch()
        groups = clip_scan.group_by_stem([a, b, c])
        assert groups == {"DSC123": [a, b], "DSC124": [c]}

    def test_groups_pre_flagged_with_unflagged(self, tmp_path):
        # An already-renamed ARW and a not-yet-renamed JPG should group
        # together by canonical stem.
        a = tmp_path / "DSC123.wclip.ARW"
        b = tmp_path / "DSC123.JPG"
        a.touch()
        b.touch()
        groups = clip_scan.group_by_stem([a, b])
        assert set(groups.keys()) == {"DSC123"}
        assert set(groups["DSC123"]) == {a, b}

    def test_case_insensitive_grouping(self, tmp_path):
        a = tmp_path / "dsc123.arw"
        b = tmp_path / "DSC123.JPG"
        a.touch()
        b.touch()
        groups = clip_scan.group_by_stem([a, b])
        assert len(groups) == 1


# ─────────────────────────────────────────────────────────────────────────────
# Weighted-score math
# ─────────────────────────────────────────────────────────────────────────────


class TestScoreWhite:
    WHITE_LEVEL = 16383
    CUTOFF = 0.95

    def test_all_saturated_gives_100(self):
        values = np.full(1000, self.WHITE_LEVEL, dtype=np.uint16)
        score = clip_scan.score_white(values, self.WHITE_LEVEL, self.CUTOFF)
        assert score == pytest.approx(100.0)

    def test_all_at_cutoff_gives_zero(self):
        cutoff_value = int(self.WHITE_LEVEL * self.CUTOFF)
        values = np.full(1000, cutoff_value, dtype=np.uint16)
        score = clip_scan.score_white(values, self.WHITE_LEVEL, self.CUTOFF)
        assert score == pytest.approx(0.0, abs=1e-9)

    def test_all_below_cutoff_gives_exact_zero(self):
        # Zero-pixels-in-ramp case: must produce EXACTLY 0.0, not dust
        # like 1e-12 that could cross --white-score 0.0.
        cutoff_value = int(self.WHITE_LEVEL * self.CUTOFF)
        values = np.full(1000, cutoff_value - 100, dtype=np.uint16)
        score = clip_scan.score_white(values, self.WHITE_LEVEL, self.CUTOFF)
        assert score == 0.0

    def test_uniform_midpoint_gives_50_pct(self):
        # value = (cutoff + 1.0) / 2 * white_level → ramp position 0.5 →
        # weight 0.5 → score 50%
        midpoint_norm = (self.CUTOFF + 1.0) / 2.0
        midpoint_value = int(self.WHITE_LEVEL * midpoint_norm)
        values = np.full(1000, midpoint_value, dtype=np.uint16)
        score = clip_scan.score_white(values, self.WHITE_LEVEL, self.CUTOFF)
        assert score == pytest.approx(50.0, abs=0.5)

    def test_2pct_saturated_rest_zero_gives_score_2(self):
        values = np.zeros(10000, dtype=np.uint16)
        values[:200] = self.WHITE_LEVEL  # 200 / 10000 = 2%
        score = clip_scan.score_white(values, self.WHITE_LEVEL, self.CUTOFF)
        assert score == pytest.approx(2.0, abs=0.01)

    def test_equivalence_1pct_sat_equals_2pct_midpoint(self):
        # 1% at saturation contributes 1.0
        # 2% at midpoint of ramp contributes 0.5 × 2 = 1.0
        # Both should score equally.
        a = np.zeros(10000, dtype=np.uint16)
        a[:100] = self.WHITE_LEVEL  # 1% at saturation

        midpoint_value = int(self.WHITE_LEVEL * (self.CUTOFF + 1.0) / 2.0)
        b = np.zeros(10000, dtype=np.uint16)
        b[:200] = midpoint_value  # 2% at midpoint

        score_a = clip_scan.score_white(a, self.WHITE_LEVEL, self.CUTOFF)
        score_b = clip_scan.score_white(b, self.WHITE_LEVEL, self.CUTOFF)
        assert score_a == pytest.approx(score_b, abs=0.05)
        assert score_a == pytest.approx(1.0, abs=0.05)

    def test_gamma_2_at_midpoint_gives_25_pct(self):
        # With gamma=2.0, weight at midpoint = 0.5^2 = 0.25 → score 25%
        midpoint_value = int(self.WHITE_LEVEL * (self.CUTOFF + 1.0) / 2.0)
        values = np.full(1000, midpoint_value, dtype=np.uint16)
        score = clip_scan.score_white(values, self.WHITE_LEVEL, self.CUTOFF, gamma=2.0)
        assert score == pytest.approx(25.0, abs=0.5)

    def test_gamma_1_at_midpoint_gives_50_pct(self):
        midpoint_value = int(self.WHITE_LEVEL * (self.CUTOFF + 1.0) / 2.0)
        values = np.full(1000, midpoint_value, dtype=np.uint16)
        score = clip_scan.score_white(values, self.WHITE_LEVEL, self.CUTOFF, gamma=1.0)
        assert score == pytest.approx(50.0, abs=0.5)

    def test_invalid_white_level_zero_raises(self):
        with pytest.raises((ValueError, ZeroDivisionError)):
            clip_scan.score_white(np.zeros(10, dtype=np.uint16), 0, self.CUTOFF)


class TestScoreBlack:
    BLACK_LEVEL = 512
    WHITE_LEVEL = 16383
    CUTOFF = 1.05  # ramp ends at 1.05 × black_level

    def test_all_at_black_gives_100(self):
        values = np.full(1000, self.BLACK_LEVEL, dtype=np.uint16)
        score = clip_scan.score_black(values, self.BLACK_LEVEL, self.CUTOFF)
        assert score == pytest.approx(100.0)

    def test_all_at_cutoff_gives_zero(self):
        cutoff_value = int(self.BLACK_LEVEL * self.CUTOFF)
        values = np.full(1000, cutoff_value, dtype=np.uint16)
        score = clip_scan.score_black(values, self.BLACK_LEVEL, self.CUTOFF)
        assert score == pytest.approx(0.0, abs=1e-9)

    def test_all_above_cutoff_gives_exact_zero(self):
        cutoff_value = int(self.BLACK_LEVEL * self.CUTOFF)
        values = np.full(1000, cutoff_value + 100, dtype=np.uint16)
        score = clip_scan.score_black(values, self.BLACK_LEVEL, self.CUTOFF)
        assert score == 0.0

    def test_2pct_at_black_gives_score_2(self):
        values = np.full(10000, self.BLACK_LEVEL + 200, dtype=np.uint16)
        values[:200] = self.BLACK_LEVEL  # 2% at exact black
        score = clip_scan.score_black(values, self.BLACK_LEVEL, self.CUTOFF)
        assert score == pytest.approx(2.0, abs=0.01)

    def test_invalid_black_at_or_above_white_raises(self):
        # Pathological case — sanity guard.
        with pytest.raises(ValueError):
            clip_scan.score_black(
                np.zeros(10, dtype=np.uint16),
                black_level=20000,
                cutoff_ratio=self.CUTOFF,
            )


# ─────────────────────────────────────────────────────────────────────────────
# Bayer / CFA indexing
# ─────────────────────────────────────────────────────────────────────────────


class TestChannelExtraction:
    """Per H2 — verify CFA pattern indexing recovers per-channel pools."""

    def _make_rggb_mosaic(self, h=64, w=64):
        # Build a synthetic RGGB Bayer mosaic with distinguishable per-channel
        # values so we can verify the extraction picked the right pixels.
        # rawpy convention: color indices are positions in color_desc 'RGBG'.
        # 0=R, 1=G1, 2=B, 3=G2 for RGGB.
        raw_image = np.zeros((h, w), dtype=np.uint16)
        raw_colors = np.zeros((h, w), dtype=np.uint8)
        raw_colors[0::2, 0::2] = 0  # R
        raw_colors[0::2, 1::2] = 1  # G1
        raw_colors[1::2, 0::2] = 3  # G2
        raw_colors[1::2, 1::2] = 2  # B

        raw_image[0::2, 0::2] = 1000  # R values
        raw_image[0::2, 1::2] = 2000  # G1 values
        raw_image[1::2, 0::2] = 4000  # G2 values
        raw_image[1::2, 1::2] = 3000  # B values
        return raw_image, raw_colors

    def test_extract_pools_yields_expected_sizes(self):
        raw_image, raw_colors = self._make_rggb_mosaic()
        pools = clip_scan.extract_channel_pools(raw_image, raw_colors)
        total = raw_image.size
        # On RGGB, R and B are 25% each; G1 and G2 are 25% each.
        assert pools.r.size == total // 4
        assert pools.g1.size == total // 4
        assert pools.b.size == total // 4
        assert pools.g2.size == total // 4

    def test_extract_pools_unions_to_full_frame(self):
        raw_image, raw_colors = self._make_rggb_mosaic()
        pools = clip_scan.extract_channel_pools(raw_image, raw_colors)
        assert (
            pools.r.size + pools.g1.size + pools.b.size + pools.g2.size == raw_image.size
        )

    def test_extract_pools_picks_right_pixels(self):
        raw_image, raw_colors = self._make_rggb_mosaic()
        pools = clip_scan.extract_channel_pools(raw_image, raw_colors)
        assert np.all(pools.r == 1000)
        assert np.all(pools.g1 == 2000)
        assert np.all(pools.b == 3000)
        assert np.all(pools.g2 == 4000)


# ─────────────────────────────────────────────────────────────────────────────
# Per-channel analysis: max-across-channels
# ─────────────────────────────────────────────────────────────────────────────


class TestAnalyseArray:
    WHITE_LEVEL = 16383
    BLACK_LEVELS = (512, 512, 512, 512)
    WHITE_CUTOFF = 0.95
    BLACK_CUTOFF = 1.05

    def _make_mosaic(self):
        # Build an RGGB mosaic where every channel sits just below the
        # white cutoff so the baseline score is ~0.
        h, w = 64, 64
        raw_image = np.full((h, w), 8000, dtype=np.uint16)
        raw_colors = np.zeros((h, w), dtype=np.uint8)
        raw_colors[0::2, 0::2] = 0  # R
        raw_colors[0::2, 1::2] = 1  # G1
        raw_colors[1::2, 0::2] = 3  # G2
        raw_colors[1::2, 1::2] = 2  # B
        return raw_image, raw_colors

    def test_max_across_channels_picks_red(self):
        # Push only the red channel into clipping; black/green untouched.
        raw_image, raw_colors = self._make_mosaic()
        raw_image[raw_colors == 0] = self.WHITE_LEVEL  # blow red
        result = clip_scan.analyse_array(
            raw_image,
            raw_colors,
            white_level=self.WHITE_LEVEL,
            black_level_per_channel=self.BLACK_LEVELS,
            white_cutoff=self.WHITE_CUTOFF,
            black_cutoff=self.BLACK_CUTOFF,
        )
        # Red is 100% blown → its per-channel score is 100. Others ~0.
        # Max across channels → 100.
        assert result.white_score == pytest.approx(100.0)
        assert result.per_channel_white[0] == pytest.approx(100.0)
        # Other channels stay near zero.
        assert max(result.per_channel_white[1:]) < 1.0

    def test_per_channel_black_uses_per_channel_pedestal(self):
        # H1: per-channel black levels may differ. Construct a case where
        # using a scalar collapse would give the wrong answer.
        # Red pixels sit at value 600. With per-channel black_level=600 for
        # red, those pixels are AT black → score should be 100 for red.
        # If implementation collapsed to scalar min/max/mean, it would
        # mis-score.
        raw_image, raw_colors = self._make_mosaic()
        raw_image[raw_colors == 0] = 600  # red pixels at 600
        # Other channels stay at 8000 (well above any black cutoff).
        per_channel_black = (600, 512, 512, 512)  # red pedestal at 600
        result = clip_scan.analyse_array(
            raw_image,
            raw_colors,
            white_level=self.WHITE_LEVEL,
            black_level_per_channel=per_channel_black,
            white_cutoff=self.WHITE_CUTOFF,
            black_cutoff=self.BLACK_CUTOFF,
        )
        # Red is 100% at its (custom) black level → black-score = 100
        assert result.per_channel_black[0] == pytest.approx(100.0)
        assert result.black_score == pytest.approx(100.0)
        # Other channels are well above their pedestals (cutoff value
        # 512*1.05 = 537), so they score 0.
        assert max(result.per_channel_black[1:]) == pytest.approx(0.0, abs=1e-9)


# ─────────────────────────────────────────────────────────────────────────────
# CLI surface — argparse
# ─────────────────────────────────────────────────────────────────────────────


class TestCLI:
    def test_parses_full_argv(self):
        argv = [
            "--white-cutoff",
            "0.93",
            "--white-score",
            "1.5",
            "--gamma",
            "1.5",
            "--apply",
            "/some/dir",
        ]
        args = clip_scan.build_parser().parse_args(argv)
        assert args.white_cutoff == pytest.approx(0.93)
        assert args.white_score == pytest.approx(1.5)
        assert args.gamma == pytest.approx(1.5)
        assert args.apply is True
        assert args.path == pathlib.Path("/some/dir")

    def test_defaults(self):
        args = clip_scan.build_parser().parse_args([])
        assert args.white_cutoff == pytest.approx(0.95)
        assert args.white_score == pytest.approx(2.0)
        assert args.black_cutoff == pytest.approx(1.05)
        assert args.black_score == pytest.approx(5.0)
        assert args.gamma == pytest.approx(1.0)
        assert args.apply is False  # dry-run by default

    def test_apply_overrides_dry_run(self):
        # If both are passed, --apply wins (explicit override).
        args = clip_scan.build_parser().parse_args(["--dry-run", "--apply"])
        assert args.apply is True


# ─────────────────────────────────────────────────────────────────────────────
# Lightroom catalog safety rail
# ─────────────────────────────────────────────────────────────────────────────


class TestCatalogSafetyRail:
    def test_no_lrcat_in_tree_allows_apply(self, tmp_path):
        # No .lrcat anywhere → safe.
        target = tmp_path / "shoot"
        target.mkdir()
        assert clip_scan.find_lightroom_catalog(target) is None

    def test_lrcat_in_target_dir_is_detected(self, tmp_path):
        target = tmp_path / "shoot"
        target.mkdir()
        (target / "MyCatalog.lrcat").touch()
        assert clip_scan.find_lightroom_catalog(target) is not None

    def test_lrcat_in_parent_is_detected(self, tmp_path):
        parent = tmp_path / "Photos"
        parent.mkdir()
        (parent / "MyCatalog.lrcat").touch()
        target = parent / "shoot"
        target.mkdir()
        assert clip_scan.find_lightroom_catalog(target) is not None

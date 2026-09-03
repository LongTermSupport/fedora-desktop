"""Integration tests for clip-scan: rawpy path, CLI, renames, JSON, parallelism.

Covers Phases 4 to 6 of CLAUDE/Plan/00040-raw-clipping-scanner/PLAN.md.

No real ARW fixture exists in the repo and rawpy is not installed in the
test environment, so every rawpy-dependent path is exercised through a fake
``rawpy`` module (the ``fake_rawpy`` fixture) that exposes the same surface
``analyse_raw`` touches: ``imread`` returning a context manager with
``raw_image_visible``, ``raw_colors_visible``, ``white_level``,
``black_level_per_channel``, ``raw_pattern``, plus a ``LibRawError`` class.
Running the same paths against a real Sony A7V ARW is the Phase 2 host task.

The JPG paths need Pillow; they skip with a reason when it is absent.
"""

from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import types

import numpy as np
import pytest

clip_scan = sys.modules["clip_scan"]

SCRIPT_PATH = (
    pathlib.Path(__file__).resolve().parents[2] / "files" / "home" / ".local" / "bin" / "clip-scan"
)

WHITE_LEVEL = 16383
BLACK_LEVELS = (512, 514, 512, 514)  # non-identical on purpose (H1)
RGGB_PATTERN = np.array([[0, 1], [3, 2]], dtype=np.uint8)


# ─────────────────────────────────────────────────────────────────────────────
# Fixtures: fake rawpy, synthetic mosaics, Pillow JPGs
# ─────────────────────────────────────────────────────────────────────────────


def make_mosaic(fill: int, h: int = 16, w: int = 16) -> tuple[np.ndarray, np.ndarray]:
    """Uniform RGGB mosaic at ``fill`` with raw_colors tiled from RGGB_PATTERN."""
    raw_image = np.full((h, w), fill, dtype=np.uint16)
    raw_colors = np.tile(RGGB_PATTERN, (h // 2, w // 2))
    return raw_image, raw_colors


def normal_frame() -> dict:
    """A frame with every channel mid-range: no clipping on either side."""
    raw_image, raw_colors = make_mosaic(8000)
    return {"raw_image": raw_image, "raw_colors": raw_colors}


def blown_red_frame() -> dict:
    """Red channel fully saturated, everything else mid-range."""
    raw_image, raw_colors = make_mosaic(8000)
    raw_image[raw_colors == 0] = WHITE_LEVEL
    return {"raw_image": raw_image, "raw_colors": raw_colors}


def crushed_frame() -> dict:
    """Every channel sitting exactly on its own black pedestal."""
    raw_image, raw_colors = make_mosaic(8000)
    for channel, black in enumerate(BLACK_LEVELS):
        raw_image[raw_colors == channel] = black
    return {"raw_image": raw_image, "raw_colors": raw_colors}


@pytest.fixture
def fake_rawpy(monkeypatch):
    """Install a fake ``rawpy`` module keyed by filename.

    Returns the registry dict: ``frames[filename] = frame_dict``. A filename
    absent from the registry makes ``imread`` raise ``LibRawError``, which is
    how a corrupt ARW is simulated.
    """
    frames: dict[str, dict] = {}

    class LibRawError(Exception):
        pass

    class _FakeRaw:
        def __init__(self, frame: dict):
            self.raw_image_visible = frame["raw_image"]
            self.raw_colors_visible = frame["raw_colors"]
            self.white_level = frame.get("white_level", WHITE_LEVEL)
            self.black_level_per_channel = list(frame.get("black_levels", BLACK_LEVELS))
            self.raw_pattern = RGGB_PATTERN

        def __enter__(self):
            return self

        def __exit__(self, *exc):
            return False

    def imread(path):
        name = pathlib.Path(path).name
        if name not in frames:
            raise LibRawError(f"Cannot open file {path}: Input/output error")
        return _FakeRaw(frames[name])

    module = types.ModuleType("rawpy")
    module.imread = imread
    module.LibRawError = LibRawError
    monkeypatch.setitem(sys.modules, "rawpy", module)
    return frames


@pytest.fixture
def pil_image():
    """Pillow's Image module, or a skip when Pillow is not installed."""
    return pytest.importorskip("PIL.Image", reason="Pillow not installed; JPG path untestable")


def write_jpg(pil_image, path: pathlib.Path, grey: int) -> pathlib.Path:
    """Write a uniform 16x16 JPEG at the given 0-255 grey value."""
    pil_image.new("RGB", (16, 16), (grey, grey, grey)).save(
        path, "JPEG", quality=100, subsampling=0
    )
    return path


def touch(path: pathlib.Path, content: bytes = b"dummy") -> pathlib.Path:
    path.write_bytes(content)
    return path


def dir_listing(path: pathlib.Path) -> dict[str, bytes]:
    return {p.name: p.read_bytes() for p in sorted(path.iterdir())}


def run_main(argv: list[str], capsys) -> tuple[int, str, str]:
    """Drive main() in-process, single-process unless argv sets --jobs.

    The fake rawpy module and the conftest-loaded clip_scan module exist only
    in this interpreter; a forkserver/spawn worker could not see either, so
    in-process tests never let --jobs default to nproc. The pool path has its
    own subprocess tests in TestParallelExecution.
    """
    if "--jobs" not in argv and "-j" not in argv:
        argv = ["--jobs", "1", *argv]
    rc = clip_scan.main(argv)
    captured = capsys.readouterr()
    return rc, captured.out, captured.err


def default_args(**overrides):
    return clip_scan.build_parser().parse_args(
        [f"--{k.replace('_', '-')}={v}" for k, v in overrides.items()]
    )


# ─────────────────────────────────────────────────────────────────────────────
# Phase 4: analyse_raw end-to-end and Bayer extraction
# ─────────────────────────────────────────────────────────────────────────────


class TestAnalyseRawEndToEnd:
    def test_analyse_raw_end_to_end_with_fake_rawpy(self, fake_rawpy, tmp_path):
        """analyse_raw() opens the file via rawpy, honours per-channel black
        levels (H1) and reports the max-across-channels white score.
        A real-ARW run of this path is the Phase 2 host task."""
        arw = touch(tmp_path / "DSC001.ARW")
        fake_rawpy[arw.name] = blown_red_frame()

        result = clip_scan.analyse_raw(arw, white_cutoff=0.95, black_cutoff=1.01)

        assert result.analysis_source == "raw"
        assert result.white_level == WHITE_LEVEL
        assert result.black_level_per_channel == BLACK_LEVELS
        assert result.white_score == pytest.approx(100.0)
        assert result.per_channel_white[0] == pytest.approx(100.0)
        assert max(result.per_channel_white[1:]) == 0.0
        assert result.black_score == 0.0

    def test_analyse_raw_crushed_frame_scores_black_per_channel(self, fake_rawpy, tmp_path):
        arw = touch(tmp_path / "DSC002.ARW")
        fake_rawpy[arw.name] = crushed_frame()

        result = clip_scan.analyse_raw(arw, white_cutoff=0.95, black_cutoff=1.01)

        assert result.black_score == pytest.approx(100.0)
        assert all(v == pytest.approx(100.0) for v in result.per_channel_black)
        assert result.white_score == 0.0

    def test_analyse_raw_pads_short_black_level_tuple(self, fake_rawpy, tmp_path):
        """LibRaw can report fewer than four black levels; the last value is
        repeated so the result always carries a 4-tuple."""
        arw = touch(tmp_path / "DSC003.ARW")
        frame = normal_frame()
        frame["black_levels"] = (512, 516)
        fake_rawpy[arw.name] = frame

        result = clip_scan.analyse_raw(arw, white_cutoff=0.95, black_cutoff=1.01)

        assert result.black_level_per_channel == (512, 516, 516, 516)

    def test_analyse_raw_propagates_libraw_error(self, fake_rawpy, tmp_path):
        arw = touch(tmp_path / "DSC004.ARW")  # not registered: imread raises
        with pytest.raises(sys.modules["rawpy"].LibRawError):
            clip_scan.analyse_raw(arw, white_cutoff=0.95, black_cutoff=1.01)


class TestBayerExtractionFromRawpySurface:
    def test_raw_colors_visible_pools_union_to_full_visible_frame(self, fake_rawpy, tmp_path):
        """H2: the four channel pools built from ``r.raw_colors_visible``
        partition the visible frame, with R and B at 25% each and G at 50%,
        and ``raw_colors_visible`` agrees with the tiled ``raw_pattern``.
        Confirming this against a real ARW is the Phase 2 host task."""
        arw = touch(tmp_path / "DSC005.ARW")
        fake_rawpy[arw.name] = normal_frame()

        with sys.modules["rawpy"].imread(str(arw)) as r:
            raw_image = r.raw_image_visible
            raw_colors = r.raw_colors_visible
            pattern = r.raw_pattern

        h, w = raw_colors.shape
        assert np.array_equal(raw_colors, np.tile(pattern, (h // 2, w // 2)))

        pools = clip_scan.extract_channel_pools(raw_image, raw_colors)
        sizes = [pools.r.size, pools.g1.size, pools.b.size, pools.g2.size]
        assert sum(sizes) == raw_image.size
        assert pools.r.size == raw_image.size // 4
        assert pools.b.size == raw_image.size // 4
        assert pools.g1.size + pools.g2.size == raw_image.size // 2


# ─────────────────────────────────────────────────────────────────────────────
# Phase 4: corrupt-ARW fallback (M4) and JPG-only fallback (Decision 8)
# ─────────────────────────────────────────────────────────────────────────────


class TestCorruptRawFallback:
    def test_corrupt_arw_falls_back_to_sibling_jpg(self, fake_rawpy, pil_image, tmp_path):
        """M4: rawpy raising LibRawError on the ARW must not lose the group;
        the sibling JPG is analysed instead and reported as source 'jpg'."""
        arw = touch(tmp_path / "DSC010.ARW")  # unregistered: LibRawError
        jpg = write_jpg(pil_image, tmp_path / "DSC010.JPG", 255)

        analysis, err = clip_scan._analyse_group([arw, jpg], default_args())

        assert err is None
        assert analysis is not None
        assert analysis.analysis_source == "jpg"
        assert analysis.white_score == pytest.approx(100.0)

    def test_corrupt_arw_without_sibling_is_an_error_row(self, fake_rawpy, tmp_path, capsys):
        touch(tmp_path / "DSC011.ARW")
        touch(tmp_path / "DSC012.ARW")
        fake_rawpy["DSC012.ARW"] = normal_frame()

        rc, out, err = run_main(["--json", str(tmp_path)], capsys)

        assert rc == 1, "a group with no analysable source must fail the batch"
        rows = {r["canonical_stem"]: r for r in json.loads(out)["results"]}
        assert rows["DSC011"]["action"] == "error"
        assert "LibRawError" in rows["DSC011"]["error"]
        assert rows["DSC011"]["white_score"] is None
        assert rows["DSC012"]["action"] == "no-change"
        assert rows["DSC012"]["error"] is None

    def test_healthy_arw_is_preferred_over_sibling_jpg(self, fake_rawpy, pil_image, tmp_path):
        arw = touch(tmp_path / "DSC013.ARW")
        fake_rawpy[arw.name] = normal_frame()
        jpg = write_jpg(pil_image, tmp_path / "DSC013.JPG", 255)

        analysis, err = clip_scan._analyse_group([jpg, arw], default_args())

        assert err is None
        assert analysis.analysis_source == "raw"
        assert analysis.white_score == 0.0


class TestJpgOnlyFallback:
    def test_white_jpg_scores_100_against_255(self, pil_image, tmp_path):
        jpg = write_jpg(pil_image, tmp_path / "DSC020.JPG", 255)
        result = clip_scan.analyse_jpg(jpg, jpg_white_cutoff=0.95, jpg_black_cutoff=0.05)
        assert result.analysis_source == "jpg"
        assert result.white_level == clip_scan.JPG_MAX
        assert result.black_level_per_channel == (0, 0, 0, 0)
        assert result.white_score == pytest.approx(100.0)
        assert result.black_score == 0.0
        assert len(result.per_channel_white) == 4
        assert result.per_channel_white[3] == result.per_channel_white[1]

    def test_black_jpg_scores_100_on_black_side(self, pil_image, tmp_path):
        jpg = write_jpg(pil_image, tmp_path / "DSC021.JPG", 0)
        result = clip_scan.analyse_jpg(jpg, jpg_white_cutoff=0.95, jpg_black_cutoff=0.05)
        assert result.black_score == pytest.approx(100.0)
        assert result.white_score == 0.0

    def test_mid_grey_jpg_scores_zero_both_sides(self, pil_image, tmp_path):
        jpg = write_jpg(pil_image, tmp_path / "DSC022.JPG", 128)
        result = clip_scan.analyse_jpg(jpg, jpg_white_cutoff=0.95, jpg_black_cutoff=0.05)
        assert result.white_score == 0.0
        assert result.black_score == 0.0

    def test_jpg_cutoffs_are_separate_from_raw_cutoffs(self, pil_image, tmp_path):
        """Decision 8: a 240/255 pixel is below the default JPG white cutoff
        (0.95 x 255 = 242.25) but inside a looser 0.90 ramp; the raw
        ``--white-cutoff`` never touches the JPG path."""
        jpg = write_jpg(pil_image, tmp_path / "DSC023.JPG", 240)
        strict = clip_scan.analyse_jpg(jpg, jpg_white_cutoff=0.95, jpg_black_cutoff=0.05)
        loose = clip_scan.analyse_jpg(jpg, jpg_white_cutoff=0.90, jpg_black_cutoff=0.05)
        assert strict.white_score == 0.0
        assert loose.white_score > 0.0

    def test_jpg_only_group_is_flagged_through_main(self, pil_image, tmp_path, capsys):
        write_jpg(pil_image, tmp_path / "DSC024.JPG", 255)

        rc, out, _ = run_main(["--json", str(tmp_path)], capsys)

        assert rc == 0
        (row,) = json.loads(out)["results"]
        assert row["analysis_source"] == "jpg"
        assert row["verdict_wclip"] is True
        assert row["new_name"] == "DSC024.wclip.JPG"


# ─────────────────────────────────────────────────────────────────────────────
# Phase 5: dry-run, sibling order, collisions, --json
# ─────────────────────────────────────────────────────────────────────────────


@pytest.fixture
def flagged_pair(fake_rawpy, tmp_path):
    """DSC100.ARW (blown red) with a DSC100.JPG sibling, plus a clean DSC101.ARW."""
    arw = touch(tmp_path / "DSC100.ARW", b"raw-bytes")
    jpg = touch(tmp_path / "DSC100.JPG", b"jpg-bytes")
    clean = touch(tmp_path / "DSC101.ARW", b"clean-bytes")
    fake_rawpy[arw.name] = blown_red_frame()
    fake_rawpy[clean.name] = normal_frame()
    return tmp_path, arw, jpg, clean


class TestDryRun:
    def test_dry_run_reports_renames_without_touching_the_filesystem(
        self, flagged_pair, capsys
    ):
        root, arw, jpg, clean = flagged_pair
        before = dir_listing(root)

        rc, out, _ = run_main([str(root)], capsys)

        assert rc == 0
        assert dir_listing(root) == before
        assert "DSC100.ARW → DSC100.wclip.ARW" in out
        assert "DSC100.JPG → DSC100.wclip.JPG" in out
        assert "DSC101" not in out
        assert "DRY-RUN" in out
        assert "re-run with --apply" in out

    def test_explicit_dry_run_flag_also_makes_no_change(self, flagged_pair, capsys):
        root, *_ = flagged_pair
        before = dir_listing(root)
        rc, _, _ = run_main(["--dry-run", str(root)], capsys)
        assert rc == 0
        assert dir_listing(root) == before


class TestApplyRenameOrder:
    def test_plan_rename_order_puts_raw_last(self, tmp_path):
        arw = tmp_path / "DSC200.ARW"
        jpg = tmp_path / "DSC200.JPG"
        hif = tmp_path / "DSC200.HIF"
        assert clip_scan.plan_rename_order([arw, jpg, hif]) == [jpg, hif, arw]

    def test_apply_renames_siblings_before_the_raw(self, flagged_pair, capsys, monkeypatch):
        """H4: the JPG sibling is renamed before the ARW, so a kill between
        the two leaves DSC100.wclip.JPG + DSC100.ARW, which still group under
        canonical stem DSC100 on the retry."""
        root, arw, jpg, clean = flagged_pair
        order: list[str] = []
        original_rename = pathlib.Path.rename

        def recording_rename(self, target):
            order.append(self.name)
            return original_rename(self, target)

        monkeypatch.setattr(pathlib.Path, "rename", recording_rename)

        rc, out, _ = run_main(["--apply", str(root)], capsys)

        assert rc == 0
        assert order == ["DSC100.JPG", "DSC100.ARW"]
        assert sorted(p.name for p in root.iterdir()) == [
            "DSC100.wclip.ARW",
            "DSC100.wclip.JPG",
            "DSC101.ARW",
        ]
        assert (root / "DSC100.wclip.ARW").read_bytes() == b"raw-bytes"
        assert (root / "DSC100.wclip.JPG").read_bytes() == b"jpg-bytes"
        assert "APPLY" in out

    def test_second_apply_is_a_no_op(self, fake_rawpy, flagged_pair, capsys):
        root, *_ = flagged_pair
        assert run_main(["--apply", str(root)], capsys)[0] == 0
        after_first = dir_listing(root)
        # The registry is keyed by filename, so the renamed ARW needs its own
        # entry before the second pass can decode it.
        fake_rawpy["DSC100.wclip.ARW"] = blown_red_frame()

        rc, out, _ = run_main(["--apply", str(root)], capsys)

        assert rc == 0
        assert dir_listing(root) == after_first
        assert "DSC100.wclip.ARW = DSC100.wclip.ARW" in out


class TestCollision:
    def test_existing_target_is_left_alone_and_reported(self, flagged_pair, capsys):
        root, arw, jpg, clean = flagged_pair
        stranger = touch(root / "DSC100.wclip.ARW", b"pre-existing")

        rc, out, _ = run_main(["--apply", str(root)], capsys)

        assert rc == 0, "a collision is reported, not counted as a batch error"
        assert stranger.read_bytes() == b"pre-existing"
        assert arw.exists(), "the source ARW must not be renamed over an existing file"
        assert arw.read_bytes() == b"raw-bytes"
        assert "Collisions (skipped)" in out
        assert "DSC100.ARW → DSC100.wclip.ARW: target name already exists" in out
        assert "collisions: 1" in out
        # The sibling has no collision, so it is renamed as planned.
        assert (root / "DSC100.wclip.JPG").exists()
        assert not jpg.exists()

    def test_collision_is_reported_in_json(self, flagged_pair, capsys):
        root, arw, *_ = flagged_pair
        touch(root / "DSC100.wclip.ARW", b"pre-existing")

        rc, out, _ = run_main(["--apply", "--json", str(root)], capsys)

        assert rc == 0
        rows = {r["path"]: r for r in json.loads(out)["results"]}
        row = rows[str(arw.resolve())]
        assert row["action"] == "skip-collision"
        assert row["new_name"] is None
        assert "target name already exists" in row["error"]


# The documented schema in DECISIONS.md "--json output schema (Phase 5)".
DOCUMENTED_TOP_LEVEL = {"version", "config", "results"}
DOCUMENTED_CONFIG = {"white_cutoff", "white_score", "black_cutoff", "black_score", "gamma"}
DOCUMENTED_RESULT = {
    "path": str,
    "canonical_stem": str,
    "extension": str,
    "siblings": list,
    "white_score": float,
    "black_score": float,
    "per_channel_white": list,
    "per_channel_black": list,
    "white_level": int,
    "black_level_per_channel": list,
    "verdict_wclip": bool,
    "verdict_bclip": bool,
    "action": str,
    "new_name": (str, type(None)),
    "error": (str, type(None)),
}
# Fields the implementation adds beyond the documented schema (PLAN.md Phase 5).
EXTRA_CONFIG = {"jpg_white_cutoff", "jpg_black_cutoff"}
EXTRA_RESULT = {"analysis_source"}


class TestJsonSchema:
    def test_json_output_matches_documented_schema(self, flagged_pair, capsys):
        """L4: every documented field is present with the documented type,
        and the only additions are the ones PLAN.md already names."""
        root, arw, jpg, clean = flagged_pair

        rc, out, err = run_main(["--json", str(root)], capsys)

        assert rc == 0
        assert err == "", "--json must silence the stderr progress stream"
        doc = json.loads(out)

        assert set(doc) == DOCUMENTED_TOP_LEVEL
        assert doc["version"] == 1
        assert set(doc["config"]) == DOCUMENTED_CONFIG | EXTRA_CONFIG
        assert doc["config"] == {
            "white_cutoff": 0.95,
            "white_score": 2.0,
            "black_cutoff": 1.01,
            "black_score": 5.0,
            "gamma": 1.0,
            "jpg_white_cutoff": 0.95,
            "jpg_black_cutoff": 0.05,
        }

        assert len(doc["results"]) == 3
        for row in doc["results"]:
            assert set(row) == set(DOCUMENTED_RESULT) | EXTRA_RESULT
            for field, expected_type in DOCUMENTED_RESULT.items():
                assert isinstance(row[field], expected_type), (field, row[field])
            assert pathlib.Path(row["path"]).is_absolute()
            assert len(row["per_channel_white"]) == 4
            assert len(row["per_channel_black"]) == 4
            assert len(row["black_level_per_channel"]) == 4
            assert row["analysis_source"] == "raw"
            assert row["error"] is None

        rows = {r["path"]: r for r in doc["results"]}
        flagged_arw = rows[str(arw.resolve())]
        assert flagged_arw["canonical_stem"] == "DSC100"
        assert flagged_arw["extension"] == "ARW"
        assert flagged_arw["siblings"] == ["DSC100.JPG"]
        assert flagged_arw["white_score"] == pytest.approx(100.0)
        assert flagged_arw["white_level"] == WHITE_LEVEL
        assert flagged_arw["black_level_per_channel"] == list(BLACK_LEVELS)
        assert flagged_arw["verdict_wclip"] is True
        assert flagged_arw["verdict_bclip"] is False
        assert flagged_arw["action"] == "rename"
        assert flagged_arw["new_name"] == "DSC100.wclip.ARW"

        flagged_jpg = rows[str(jpg.resolve())]
        assert flagged_jpg["siblings"] == ["DSC100.ARW"]
        assert flagged_jpg["new_name"] == "DSC100.wclip.JPG"

        clean_row = rows[str(clean.resolve())]
        assert clean_row["siblings"] == []
        assert clean_row["action"] == "no-change"
        assert clean_row["new_name"] is None

    def test_json_config_echoes_non_default_thresholds(self, flagged_pair, capsys):
        root, *_ = flagged_pair
        rc, out, _ = run_main(
            ["--json", "--white-cutoff", "0.93", "--gamma", "1.5", str(root)], capsys
        )
        assert rc == 0
        config = json.loads(out)["config"]
        assert config["white_cutoff"] == pytest.approx(0.93)
        assert config["gamma"] == pytest.approx(1.5)


# ─────────────────────────────────────────────────────────────────────────────
# Phase 6: parallel execution and the --jobs 1 fallback
# ─────────────────────────────────────────────────────────────────────────────


def run_script(*argv: str) -> subprocess.CompletedProcess:
    """Run the deployable script as a subprocess under the test interpreter."""
    return subprocess.run(
        [sys.executable, str(SCRIPT_PATH), *argv],
        capture_output=True,
        text=True,
        timeout=120,
        check=False,
    )


class TestParallelExecution:
    def test_jobs_2_returns_one_result_per_group_from_the_process_pool(
        self, pil_image, tmp_path
    ):
        """Runs the deployable script as a real subprocess so the worker
        processes import it exactly as production does (no fake rawpy: the
        inputs are Pillow JPGs). Four groups in, four results out."""
        expected = {}
        for n, grey in enumerate((255, 128, 0, 255), start=1):
            write_jpg(pil_image, tmp_path / f"DSC30{n}.JPG", grey)
            expected[f"DSC30{n}"] = grey

        proc = run_script("--jobs", "2", "--json", str(tmp_path))

        assert proc.returncode == 0, proc.stderr
        rows = {r["canonical_stem"]: r for r in json.loads(proc.stdout)["results"]}
        assert set(rows) == set(expected)
        assert rows["DSC301"]["verdict_wclip"] is True
        assert rows["DSC302"]["verdict_wclip"] is False
        assert rows["DSC302"]["verdict_bclip"] is False
        assert rows["DSC303"]["verdict_bclip"] is True
        assert rows["DSC304"]["verdict_wclip"] is True
        assert all(r["error"] is None for r in rows.values())

    def test_jobs_2_progress_header_names_the_workers(self, pil_image, tmp_path):
        write_jpg(pil_image, tmp_path / "DSC310.JPG", 128)
        write_jpg(pil_image, tmp_path / "DSC311.JPG", 128)

        proc = run_script("--jobs", "2", str(tmp_path))

        assert proc.returncode == 0, proc.stderr
        assert "2 group(s), 2 file(s), 2 workers" in proc.stderr
        assert "[2/2]" in proc.stderr
        assert "No files require renaming" in proc.stdout

    def test_jobs_1_takes_the_single_process_path(
        self, fake_rawpy, tmp_path, capsys, monkeypatch
    ):
        """With --jobs 1 the pool must never be constructed; the fake rawpy
        registry lives in this process, so a worker could not see it anyway."""

        def explode(*args, **kwargs):
            raise AssertionError("ProcessPoolExecutor constructed under --jobs 1")

        monkeypatch.setattr(clip_scan.concurrent.futures, "ProcessPoolExecutor", explode)
        for n in range(3):
            name = f"DSC32{n}.ARW"
            touch(tmp_path / name)
            fake_rawpy[name] = normal_frame()

        rc, out, err = run_main(["--jobs", "1", str(tmp_path)], capsys)

        assert rc == 0
        assert "3 group(s), 3 file(s), single process" in err
        assert "[3/3]" in err
        assert "scanned: 3 groups, 3 files" in out

    def test_single_group_skips_the_pool_even_with_many_jobs(
        self, fake_rawpy, tmp_path, capsys, monkeypatch
    ):
        def explode(*args, **kwargs):
            raise AssertionError("ProcessPoolExecutor constructed for one group")

        monkeypatch.setattr(clip_scan.concurrent.futures, "ProcessPoolExecutor", explode)
        touch(tmp_path / "DSC330.ARW")
        fake_rawpy["DSC330.ARW"] = normal_frame()

        rc, _, err = run_main(["--jobs", "8", str(tmp_path)], capsys)

        assert rc == 0
        assert "single process" in err

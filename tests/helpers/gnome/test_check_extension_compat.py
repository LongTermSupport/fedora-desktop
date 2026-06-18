"""Unit tests for helpers/gnome/check_extension_compat.py — the thin I/O CLI.

Filesystem facts are written into a temp dir; no GNOME session or network is
touched. Run from the repo root:

    python3 -m unittest tests.helpers.gnome.test_check_extension_compat
"""

from __future__ import annotations

import contextlib
import io
import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.gnome import check_extension_compat as cec


def _write_fedora_version(root: pathlib.Path, version) -> pathlib.Path:
    path = root / "fedora-version.yml"
    path.write_text(f"---\n# comment line\nfedora_version: {version}\n")
    return path


def _write_extension(ext_root: pathlib.Path, uuid: str, shell_versions) -> None:
    ext_dir = ext_root / uuid
    ext_dir.mkdir(parents=True)
    (ext_dir / "metadata.json").write_text(
        json.dumps({"uuid": uuid, "shell-version": shell_versions})
    )


class TestReadFedoraVersion(unittest.TestCase):
    def test_parses_version_int(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = _write_fedora_version(pathlib.Path(tmp), 44)
            self.assertEqual(cec.read_fedora_version(str(path)), 44)

    def test_missing_file_exits(self):
        with self.assertRaises(SystemExit):
            cec.read_fedora_version("/no/such/fedora-version.yml")

    def test_file_without_key_exits(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "fedora-version.yml"
            path.write_text("---\nsomething_else: 1\n")
            with self.assertRaises(SystemExit):
                cec.read_fedora_version(str(path))


class TestDiscoverExtensions(unittest.TestCase):
    def test_finds_only_dirs_with_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            _write_extension(root, "a@x", ["50"])
            (root / "no-metadata-here").mkdir()
            found = cec.discover_extensions(str(root))
            self.assertEqual([p.name for p in found], ["a@x"])

    def test_missing_dir_exits(self):
        with self.assertRaises(SystemExit):
            cec.discover_extensions("/no/such/extensions")


class TestMain(unittest.TestCase):
    def _run(self, *, fedora, exts):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            fv = _write_fedora_version(root, fedora)
            ext_root = root / "extensions"
            ext_root.mkdir()
            for uuid, versions in exts.items():
                _write_extension(ext_root, uuid, versions)
            out = io.StringIO()
            with contextlib.redirect_stdout(out):
                rc = cec.main(
                    ["--fedora-version-file", str(fv), "--extensions-dir", str(ext_root)]
                )
            return rc, out.getvalue()

    def test_all_covered_passes(self):
        rc, out = self._run(
            fedora=44, exts={"speech-to-text@fedora-desktop": ["48", "49", "50"]}
        )
        self.assertEqual(rc, 0)
        self.assertIn("COMPAT-OK", out)

    def test_uncovered_extension_fails(self):
        rc, out = self._run(
            fedora=44, exts={"speech-to-text@fedora-desktop": ["48", "49"]}
        )
        self.assertEqual(rc, 1)
        self.assertIn("COMPAT-FAIL", out)

    def test_one_bad_among_many_fails_whole_run(self):
        rc, out = self._run(
            fedora=44,
            exts={"good@x": ["50"], "bad@x": ["49"]},
        )
        self.assertEqual(rc, 1)
        self.assertIn("COMPAT-OK", out)
        self.assertIn("COMPAT-FAIL", out)

    def test_unknown_fedora_fails(self):
        rc, out = self._run(fedora=999, exts={"good@x": ["50"]})
        self.assertEqual(rc, 1)
        self.assertIn("COMPAT-FAIL", out)


if __name__ == "__main__":
    unittest.main()

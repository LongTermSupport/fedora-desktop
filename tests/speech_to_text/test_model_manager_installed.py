"""wsi-model-manager counts a model as installed only when its weights are on disk.

An interrupted download leaves a cache snapshot holding the small config files and no
model.bin. The manager once counted any snapshot as installed, and then refused to
download that model again as "already installed", so the broken download could not be
repaired from the manager (Plan 00156).

Stdlib only, plus the manager's own imports (textual, huggingface_hub), which
play-speech-to-text.yml installs. Run by scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import pathlib
import tempfile
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

manager = stt_stubs.load_script("wsi-model-manager", "wsi_model_manager_installed")

DISTIL_SNAPSHOT = "models--distil-whisper--distil-large-v3.5-ct2/snapshots/9793ccc0"


class GetInstalledTest(unittest.TestCase):

    def setUp(self):
        self.cache = pathlib.Path(tempfile.mkdtemp())
        patcher = mock.patch.object(manager, "CACHE_DIR", self.cache)
        patcher.start()
        self.addCleanup(patcher.stop)

    def snapshot(self, *files):
        snapshot = self.cache / DISTIL_SNAPSHOT
        snapshot.mkdir(parents=True)
        for name in files:
            (snapshot / name).write_text("")

    def test_a_snapshot_with_only_config_files_is_not_installed(self):
        self.snapshot("config.json", "tokenizer.json", "vocabulary.json")
        self.assertNotIn("distil-large-v3.5", manager.get_installed())

    def test_a_snapshot_holding_model_bin_is_installed(self):
        self.snapshot("config.json", "model.bin")
        self.assertIn("distil-large-v3.5", manager.get_installed())

    def test_an_empty_cache_has_nothing_installed(self):
        self.assertEqual(manager.get_installed(), set())


if __name__ == "__main__":
    unittest.main()

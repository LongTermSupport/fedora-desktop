"""wsi-model-manager counts a model as installed only when its weights are on disk.

An interrupted download leaves a cache snapshot holding the small config files and no
model.bin. The manager once counted any snapshot as installed, and then refused to
download that model again as "already installed", so the broken download could not be
repaired from the manager (Plan 00156).

Stdlib only. The manager imports textual, rich and huggingface_hub at load and exits without
them; neither is used by the code under test, so stand-ins are loaded in their place and
the test runs wherever qa-all.bash does, the CCY container included. Run by
scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import pathlib
import re
import sys
import tempfile
import types
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)


class _Stand:
    """Stands in for a textual class or callable the manager names at load time."""

    def __init__(self, *args, **kwargs):
        pass


def _stand_in_modules():
    def module(name, **attrs):
        mod = types.ModuleType(name)
        mod.__dict__.update(attrs)
        return mod

    return {
        "textual": module("textual", work=lambda **kwargs: (lambda fn: fn)),
        "textual.app": module("textual.app", App=_Stand, ComposeResult=_Stand),
        "textual.binding": module("textual.binding", Binding=_Stand),
        "textual.screen": module("textual.screen", Screen=_Stand),
        "textual.widgets": module("textual.widgets", DataTable=_Stand, Footer=_Stand,
                                  Header=_Stand, Input=_Stand, Static=_Stand),
        "rich": module("rich"),
        "rich.text": module("rich.text", Text=_Stand),
        "huggingface_hub": module("huggingface_hub", snapshot_download=_Stand),
    }


with mock.patch.dict(sys.modules, _stand_in_modules()):
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


class CataloguesAgreeTest(unittest.TestCase):
    """The manager downloads, Settings lists and wsi-resolve-model checks the disk for
    the same repo per name (Plan 00156 Task 2.4). A name that maps to a different repo in
    one of them is downloaded to one place and looked for in another, so the recorder
    would refuse a model the manager shows as installed."""

    PREFS = stt_stubs.REPO_ROOT / "extensions" / "speech-to-text@fedora-desktop" / "prefs.js"

    @classmethod
    def setUpClass(cls):
        cls.resolver = stt_stubs.load_script("wsi-resolve-model", "wsi_resolve_model_catalogue")
        block = re.search(r"const WHISPER_MODELS = \[(.*?)\n\];", cls.PREFS.read_text(), re.S)
        if block is None:
            raise AssertionError(f"no WHISPER_MODELS list in {cls.PREFS}")
        cls.prefs = dict(re.findall(r"\['([^']+)',\s*'([^']+)',", block.group(1)))

    def test_every_manager_model_is_one_the_resolver_knows_by_the_same_repo(self):
        for model_id, _label, repo, *_ in manager.MODELS:
            self.assertEqual(self.resolver.HF_REPO.get(model_id), repo, model_id)

    def test_every_settings_model_is_one_the_resolver_knows_by_the_same_repo(self):
        self.assertTrue(self.prefs, "no models read from prefs.js")
        for model_id, repo in self.prefs.items():
            self.assertEqual(self.resolver.HF_REPO.get(model_id), repo, model_id)

    def test_settings_and_the_manager_offer_the_same_models(self):
        self.assertEqual(set(self.prefs), {m[0] for m in manager.MODELS})


class AutoSuggestionTest(unittest.TestCase):
    """The manager marks the model `auto` picks on this machine, the one to download when
    none is (Plan 00156 Task 2.4), by asking wsi-resolve-model --suggest for each mode."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = pathlib.Path(self._tmp.name)
        self.calls = self.dir / "calls"

    def stub(self, name, body):
        script = self.dir / name
        script.write_text("#!/bin/sh\n" + body)
        script.chmod(0o755)
        return script

    def resolver(self, batch, streaming, rc=0):
        script = self.stub("wsi-resolve-model",
                           f'echo "$*" >> "{self.calls}"\n'
                           f'case "$*" in *"--mode batch"*) echo "{batch}" ;; '
                           f'*) echo "{streaming}" ;; esac\n'
                           f'[ {rc} -eq 0 ] || {{ echo "wsi-resolve-model: no GPU answer" >&2; exit {rc}; }}\n')
        patcher = mock.patch.object(manager, "RESOLVER", script)
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_each_mode_is_asked_without_the_disk_check(self):
        self.resolver("small", "base")
        manager.auto_suggestions("en")
        calls = self.calls.read_text().splitlines()
        self.assertEqual(len(calls), 2)
        for call in calls:
            self.assertIn("--suggest", call)
            self.assertIn("--language en", call)
            self.assertTrue(call.endswith(" auto"), call)

    def test_the_answer_maps_back_to_model_ids_with_their_modes(self):
        self.resolver("distil-whisper/distil-large-v3.5-ct2", "distil-whisper/distil-large-v3.5-ct2")
        self.assertEqual(manager.auto_suggestions("en"),
                         {"distil-large-v3.5": ["batch", "streaming"]})
        self.resolver("small", "base")
        self.assertEqual(manager.auto_suggestions("en"),
                         {"small": ["batch"], "base": ["streaming"]})

    def test_a_resolver_failure_is_raised_with_its_reason(self):
        self.resolver("small", "base", rc=1)
        with self.assertRaises(RuntimeError) as raised:
            manager.auto_suggestions("en")
        self.assertIn("no GPU answer", str(raised.exception))

    def test_the_language_is_the_setting_and_system_means_lang(self):
        reader = self.stub("wsi-setting", f'cat "{self.dir}/language"\n')
        with mock.patch.object(manager, "SETTING_READER", reader), \
                mock.patch.dict(manager.os.environ, {"LANG": "de_DE.UTF-8"}):
            (self.dir / "language").write_text("fr\n")
            self.assertEqual(manager.session_language(), "fr")
            (self.dir / "language").write_text("system\n")
            self.assertEqual(manager.session_language(), "de")


if __name__ == "__main__":
    unittest.main()

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

import contextlib
import importlib.util
import io
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


class DownloadAutoTest(unittest.TestCase):
    """`wsi-model-manager --download-auto` downloads what `auto` picks on this machine,
    with no TUI and no prompt, for an owner-requested unattended run (Plan 00156 Phase 4).
    It then asks the resolver WITHOUT --suggest, which exits 3 when model.bin is not on
    disk, so a download that returned without the weights still fails."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = pathlib.Path(self._tmp.name)
        self.calls = self.dir / "calls"
        self.downloads = []
        for name, value in (("session_language", lambda: "en"),
                            ("snapshot_download", self.fake_download)):
            patcher = mock.patch.object(manager, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        app = mock.patch.object(manager, "ModelManagerApp",
                                mock.Mock(side_effect=AssertionError("the TUI was started")))
        self.app = app.start()
        self.addCleanup(app.stop)

    def fake_download(self, **kwargs):
        self.downloads.append(kwargs)

    def resolver(self, batch, streaming, suggest_rc=0, check_rc=0):
        script = self.dir / "wsi-resolve-model"
        script.write_text(
            "#!/bin/sh\n"
            f'echo "$*" >> "{self.calls}"\n'
            'case "$*" in\n'
            f'  *--suggest*) [ {suggest_rc} -eq 0 ] || '
            f'{{ echo "wsi-resolve-model: no GPU answer" >&2; exit {suggest_rc}; }}\n'
            f'    case "$*" in *"--mode batch"*) echo "{batch}" ;; *) echo "{streaming}" ;; esac ;;\n'
            f'  *) [ {check_rc} -eq 0 ] || '
            f'{{ echo "wsi-resolve-model: auto picked x, which is not downloaded" >&2; '
            f'exit {check_rc}; }}\n'
            '    echo resolved ;;\n'
            'esac\n')
        script.chmod(0o755)
        patcher = mock.patch.object(manager, "RESOLVER", script)
        patcher.start()
        self.addCleanup(patcher.stop)

    def run_flag(self):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = manager.main(["--download-auto"])
        return rc, out.getvalue(), err.getvalue()

    def test_downloads_each_of_autos_picks_exactly_once(self):
        self.resolver("distil-whisper/distil-large-v3.5-ct2",
                      "distil-whisper/distil-large-v3.5-ct2")
        rc, out, _err = self.run_flag()
        self.assertEqual(rc, 0)
        self.assertEqual([d["repo_id"] for d in self.downloads],
                         ["distil-whisper/distil-large-v3.5-ct2"])
        self.assertEqual(out.splitlines(), [
            "WSI-MODEL-DOWNLOADED distil-large-v3.5 distil-whisper/distil-large-v3.5-ct2"])

    def test_two_picks_are_both_downloaded(self):
        self.resolver("small", "base")
        rc, out, _err = self.run_flag()
        self.assertEqual(rc, 0)
        self.assertEqual(sorted(d["repo_id"] for d in self.downloads),
                         ["Systran/faster-whisper-base", "Systran/faster-whisper-small"])
        self.assertEqual(len(out.splitlines()), 2)

    def test_the_download_is_anonymous_and_may_use_the_network(self):
        self.resolver("small", "small")
        self.run_flag()
        self.assertEqual(self.downloads, [{"repo_id": "Systran/faster-whisper-small",
                                           "local_files_only": False, "token": False}])

    def test_the_tui_is_never_constructed(self):
        self.resolver("small", "base")
        rc, _out, _err = self.run_flag()
        self.assertEqual(rc, 0)
        self.app.assert_not_called()

    def test_each_mode_is_checked_on_disk_without_suggest_after_downloading(self):
        self.resolver("small", "base")
        self.run_flag()
        checks = [c for c in self.calls.read_text().splitlines() if "--suggest" not in c]
        self.assertEqual(sorted(checks), ["--mode batch --language en auto",
                                          "--mode streaming --language en auto"])

    def test_a_download_error_fails_naming_the_repo(self):
        self.resolver("small", "base")

        def broken(**kwargs):
            raise OSError("connection reset")

        with mock.patch.object(manager, "snapshot_download", broken):
            rc, out, err = self.run_flag()
        self.assertNotEqual(rc, 0)
        self.assertEqual(out, "")
        self.assertIn("connection reset", err)
        self.assertIn("Systran/faster-whisper-", err)

    def test_weights_still_missing_after_the_download_fails(self):
        self.resolver("small", "base", check_rc=3)
        rc, out, err = self.run_flag()
        self.assertNotEqual(rc, 0)
        self.assertEqual(out, "")
        self.assertIn("not downloaded", err)

    def test_a_resolver_that_cannot_choose_fails_before_any_download(self):
        self.resolver("small", "base", suggest_rc=1)
        rc, out, err = self.run_flag()
        self.assertNotEqual(rc, 0)
        self.assertEqual(self.downloads, [])
        self.assertEqual(out, "")
        self.assertIn("no GPU answer", err)

    def test_an_unknown_argument_is_a_usage_error_not_the_tui(self):
        err = io.StringIO()
        with contextlib.redirect_stderr(err):
            rc = manager.main(["--bogus"])
        self.assertEqual(rc, 2)
        self.app.assert_not_called()


if __name__ == "__main__":
    unittest.main()

"""wsi-model-manager counts a model as installed only when its weights are on disk, and
offers only the models that fit this machine's GPU (Plan 00234).

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
        "textual.containers": module("textual.containers", Horizontal=_Stand),
        "textual.screen": module("textual.screen", Screen=_Stand),
        "textual.widgets": module("textual.widgets", Button=_Stand, DataTable=_Stand, Footer=_Stand,
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
            '  --realtime-models) echo tiny.en ;;\n'
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
            "WSI-MODEL-DOWNLOADED distil-large-v3.5 distil-whisper/distil-large-v3.5-ct2",
            "WSI-MODEL-PRESENT tiny.en Systran/faster-whisper-tiny.en"])

    def test_two_picks_are_both_downloaded(self):
        self.resolver("small", "base")
        rc, out, _err = self.run_flag()
        self.assertEqual(rc, 0)
        self.assertEqual(sorted(d["repo_id"] for d in self.downloads),
                         ["Systran/faster-whisper-base", "Systran/faster-whisper-small"])
        self.assertEqual(len(out.splitlines()), 3, "two picks and the real-time model")

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
        checks = [c for c in self.calls.read_text().splitlines()
                  if "--suggest" not in c and c.endswith(" auto")]
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


class DownloadRealtimeTest(unittest.TestCase):
    """Plan 00234 Task 4.1: `--download-realtime` fetches each live preview model the
    resolver lists (`--realtime-models`) that is not on disk, for play-speech-to-text.yml;
    `--download-auto` fetches them too. One already on disk is not fetched again, so a
    run with nothing missing needs no network. Every model is checked on disk afterwards
    with the resolver, and any failure exits 1 with nothing on stdout."""

    REPOS = {"tiny.en": "Systran/faster-whisper-tiny.en", "tiny": "Systran/faster-whisper-tiny"}

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.dir = pathlib.Path(self._tmp.name)
        self.present = self.dir / "present"
        self.present.mkdir()
        self.downloads = []
        self.lands = True
        script = self.dir / "wsi-resolve-model"
        script.write_text(
            "#!/bin/sh\n"
            'case "$*" in\n'
            '  --realtime-models) [ -e "$STUB_LIST_FAILS" ] && { echo "wsi-resolve-model: broken" >&2; exit 1; }\n'
            '    printf "tiny.en\\ntiny\\n" ;;\n'
            '  *--suggest*) echo small ;;\n'
            '  *) for last in "$@"; do :; done\n'
            f'    [ -e "{self.present}/$last" ] || '
            '{ echo "wsi-resolve-model: $last is not downloaded" >&2; exit 3; }\n'
            '    echo "$last" ;;\n'
            'esac\n')
        script.chmod(0o755)
        self.list_fails = self.dir / "list-fails"
        for patcher in (mock.patch.object(manager, "RESOLVER", script),
                        mock.patch.object(manager, "snapshot_download", self.fake_download),
                        mock.patch.object(manager, "session_language", lambda: "en"),
                        mock.patch.dict(manager.os.environ,
                                        {"STUB_LIST_FAILS": str(self.list_fails)})):
            patcher.start()
            self.addCleanup(patcher.stop)

    def fake_download(self, **kwargs):
        self.downloads.append(kwargs["repo_id"])
        name = next((n for n, repo in self.REPOS.items() if repo == kwargs["repo_id"]), None)
        if self.lands and name:
            (self.present / name).touch()

    def on_disk(self, *names):
        for name in names:
            (self.present / name).touch()

    def run_flag(self, flag="--download-realtime"):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = manager.main([flag])
        return rc, out.getvalue(), err.getvalue()

    def test_each_missing_real_time_model_is_downloaded_and_reported(self):
        rc, out, err = self.run_flag()
        self.assertEqual(rc, 0, err)
        self.assertEqual(self.downloads, list(self.REPOS.values()))
        self.assertEqual(out.splitlines(), [f"WSI-MODEL-DOWNLOADED {n} {r}"
                                            for n, r in self.REPOS.items()])

    def test_a_model_on_disk_is_not_fetched_again(self):
        self.on_disk("tiny.en", "tiny")
        rc, out, err = self.run_flag()
        self.assertEqual(rc, 0, err)
        self.assertEqual(self.downloads, [], "a model on disk was fetched again")
        self.assertEqual(out.splitlines(), [f"WSI-MODEL-PRESENT {n} {r}"
                                            for n, r in self.REPOS.items()])

    def test_a_download_error_fails_naming_the_repo(self):
        def broken(**kwargs):
            raise OSError("connection reset")

        with mock.patch.object(manager, "snapshot_download", broken):
            rc, out, err = self.run_flag()
        self.assertEqual(rc, 1)
        self.assertEqual(out, "")
        self.assertIn("connection reset", err)
        self.assertIn("Systran/faster-whisper-tiny.en", err)

    def test_a_download_that_leaves_no_weights_fails(self):
        self.lands = False
        rc, out, err = self.run_flag()
        self.assertEqual(rc, 1)
        self.assertEqual(out, "")
        self.assertIn("tiny.en", err)
        self.assertIn("not on disk", err)

    def test_a_resolver_that_cannot_list_them_fails_before_any_download(self):
        self.list_fails.touch()
        rc, out, err = self.run_flag()
        self.assertEqual(rc, 1)
        self.assertEqual(out, "")
        self.assertEqual(self.downloads, [])
        self.assertIn("broken", err)

    def test_download_auto_fetches_the_real_time_models_too(self):
        self.on_disk("auto")
        rc, out, err = self.run_flag("--download-auto")
        self.assertEqual(rc, 0, err)
        self.assertEqual(self.downloads, ["Systran/faster-whisper-small",
                                          *self.REPOS.values()])
        self.assertIn("WSI-MODEL-DOWNLOADED tiny.en Systran/faster-whisper-tiny.en",
                      out.splitlines())


class FitsThisMachineTest(unittest.TestCase):
    """Plan 00234: the manager shows which models fit this machine's GPU, preselects the
    biggest English-only one that does, and refuses to download one that does not. The
    sizes and the fit rule are wsi-resolve-model's (`--fit`), so these tests build the
    machine's report with the resolver's own fit_report."""

    @classmethod
    def setUpClass(cls):
        cls.resolver = stt_stubs.load_script("wsi-resolve-model", "wsi_resolve_model_fit")

    def report(self, gpu=("NVIDIA RTX A2000 Laptop GPU", 4096)):
        return self.resolver.fit_report(gpu)

    def test_every_offered_model_has_a_size_and_the_resolvers_language(self):
        report = self.report()
        for model_id, _label, _repo, english_only, _desc in manager.MODELS:
            self.assertIn(model_id, report["models"], model_id)
            self.assertEqual(english_only, self.resolver.is_english_only(model_id), model_id)

    def test_the_biggest_english_only_model_that_fits_is_preselected(self):
        self.assertEqual(manager.best_english_fit(self.report()), "distil-large-v3.5")
        self.assertEqual(manager.best_english_fit(self.report(("Small GPU", 2048))), "small.en")
        self.assertEqual(manager.best_english_fit(self.report(None)), "distil-large-v3.5")

    def test_nothing_is_preselected_when_no_english_model_fits(self):
        self.assertIsNone(manager.best_english_fit(self.report(("Tiny GPU", 512))))

    def test_a_model_too_big_is_refused_saying_what_it_needs_and_what_fits(self):
        reason = manager.download_refusal("large-v3", self.report(), "")
        self.assertIsNotNone(reason)
        for phrase in ("Large v3", "4.4 GB", "4.0 GB", "NVIDIA RTX A2000 Laptop GPU",
                       "Distil Large v3.5"):
            self.assertIn(phrase, reason)

    def test_a_model_that_fits_is_not_refused(self):
        self.assertIsNone(manager.download_refusal("distil-large-v3.5", self.report(), ""))

    def test_on_the_cpu_nothing_is_refused(self):
        self.assertIsNone(manager.download_refusal("large-v3", self.report(None), ""))

    def test_when_the_fit_cannot_be_told_every_download_is_refused_with_the_reason(self):
        reason = manager.download_refusal("tiny", None, "nvidia-smi is not installed")
        self.assertIn("nvidia-smi is not installed", reason)

    def test_the_fit_column_says_fits_or_too_big_with_the_memory_needed(self):
        report = self.report()
        self.assertEqual(manager.fit_cell("distil-large-v3.5", report), "✓ fits (2.6 GB)")
        self.assertEqual(manager.fit_cell("large-v3", report), "✗ too big (4.4 GB)")
        self.assertEqual(manager.fit_cell("large-v3", self.report(None)), "CPU")
        self.assertEqual(manager.fit_cell("tiny", None), "?")

    def test_the_machine_line_names_the_gpu_or_says_the_cpu_is_used(self):
        self.assertEqual(manager.machine_line(self.report(), ""),
                         "GPU: NVIDIA RTX A2000 Laptop GPU, 4.0 GB")
        line = manager.machine_line(self.report(None), "")
        self.assertIn("No usable NVIDIA GPU", line)
        self.assertIn("CPU", line)
        self.assertIn("cannot tell", manager.machine_line(None, "nvidia-smi failed"))

    def test_the_report_is_asked_of_the_resolver(self):
        with tempfile.TemporaryDirectory() as tmp:
            script = pathlib.Path(tmp) / "wsi-resolve-model"
            script.write_text(f"#!/bin/sh\necho \"$*\" > {tmp}/args\n"
                              "echo '{\"device\": \"cpu\", \"gpu\": null, "
                              "\"gpu_memory_mb\": null, \"models\": {}}'\n")
            script.chmod(0o755)
            with mock.patch.object(manager, "RESOLVER", script):
                self.assertEqual(manager.machine_fit()["device"], "cpu")
            self.assertEqual(pathlib.Path(tmp, "args").read_text().split(), ["--fit"])

    def test_a_resolver_that_cannot_judge_raises_its_last_stderr_line(self):
        with tempfile.TemporaryDirectory() as tmp:
            script = pathlib.Path(tmp) / "wsi-resolve-model"
            script.write_text("#!/bin/sh\necho 'detail' >&2\n"
                              "echo 'wsi-resolve-model: nvidia-smi failed' >&2\nexit 1\n")
            script.chmod(0o755)
            with mock.patch.object(manager, "RESOLVER", script), \
                    self.assertRaises(RuntimeError) as raised:
                manager.machine_fit()
            self.assertIn("nvidia-smi failed", str(raised.exception))


if __name__ == "__main__":
    unittest.main()

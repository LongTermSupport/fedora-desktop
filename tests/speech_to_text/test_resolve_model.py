"""wsi-resolve-model: what the `auto` model means, and the distil-large-v3.5 name (Plan 00148 Task 7.4).

`auto` on a machine with a CUDA device picks distil-large-v3.5 for English and
large-v3-turbo for any other language; without one it keeps the old per-mode defaults
(small for batch, base for streaming). distil-large-v3.5 is always handed to
faster-whisper as its Hugging Face repo, because faster-whisper 1.2.1 knows the short
name but the 1.1.1 that RealtimeSTT 0.3.104 pins does not. An English-only model with
another language set is refused rather than silently transcribed as English. A count of
0 CUDA devices on a machine with NVIDIA device nodes is a failure, not a CPU fallback.

The script runs for real with a stub `ctranslate2` module first on PYTHONPATH, so the
GPU answer is controlled without CUDA. Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

_REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
_BIN = pathlib.Path(os.environ.get("STT_TEST_BIN", _REPO_ROOT / "files" / "home" / ".local" / "bin"))
_RESOLVER = _BIN / "wsi-resolve-model"

_STUB_CTRANSLATE2 = '''
import os
def get_cuda_device_count():
    return int(os.environ["STUB_CUDA_DEVICES"])
'''

# Every repo faster-whisper 1.2.1's name table points at (faster_whisper/utils.py _MODELS)
_ALL_REPOS = (
    "Systran/faster-whisper-tiny.en", "Systran/faster-whisper-tiny",
    "Systran/faster-whisper-base.en", "Systran/faster-whisper-base",
    "Systran/faster-whisper-small.en", "Systran/faster-whisper-small",
    "Systran/faster-whisper-medium.en", "Systran/faster-whisper-medium",
    "Systran/faster-whisper-large-v1", "Systran/faster-whisper-large-v2",
    "Systran/faster-whisper-large-v3", "Systran/faster-distil-whisper-large-v2",
    "Systran/faster-distil-whisper-medium.en", "Systran/faster-distil-whisper-small.en",
    "Systran/faster-distil-whisper-large-v3", "distil-whisper/distil-large-v3.5-ct2",
    "mobiuslabsgmbh/faster-whisper-large-v3-turbo",
)


def make_cache(root, *repos, weights=True):
    """A Hugging Face hub cache at `root` with a snapshot of each repo.

    With weights, model.bin is a symlink to its blob, as huggingface_hub lays it out;
    without, the snapshot holds only the small files an interrupted download leaves.
    """
    root = pathlib.Path(root)
    root.mkdir(parents=True, exist_ok=True)
    for repo in repos:
        repo_dir = root / ("models--" + repo.replace("/", "--"))
        snapshot = repo_dir / "snapshots" / "0123abcd"
        snapshot.mkdir(parents=True)
        (snapshot / "config.json").write_text("{}")
        if weights:
            blob = repo_dir / "blobs" / "f00d"
            blob.parent.mkdir()
            blob.write_bytes(b"weights")
            (snapshot / "model.bin").symlink_to(blob)
    return root


class ResolverRun(unittest.TestCase):
    """Runs the resolver with a stub GPU answer and a stand-in model cache."""

    @classmethod
    def setUpClass(cls):
        cls._tmp = tempfile.TemporaryDirectory()
        stub_dir = pathlib.Path(cls._tmp.name)
        (stub_dir / "ctranslate2.py").write_text(_STUB_CTRANSLATE2)
        cls.stub_dir = str(stub_dir)
        # Stand-ins for /dev: one machine without NVIDIA device nodes, one with a GPU
        cls.dev_without_gpu = stub_dir / "dev-none"
        cls.dev_without_gpu.mkdir()
        (cls.dev_without_gpu / "nvidiactl").touch()  # not a GPU node
        cls.dev_with_gpu = stub_dir / "dev-gpu"
        cls.dev_with_gpu.mkdir()
        (cls.dev_with_gpu / "nvidia0").touch()
        # Every model downloaded, so the choice is tested apart from the disk check
        cls.full_cache = make_cache(stub_dir / "full-cache", *_ALL_REPOS)

    @classmethod
    def tearDownClass(cls):
        cls._tmp.cleanup()

    def resolve(self, *args, gpus=1, nvidia_nodes=False, cache=None, env_extra=None):
        env = {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "PYTHONPATH": self.stub_dir,
               "STUB_CUDA_DEVICES": str(gpus),
               "WSI_DEV_DIR": str(self.dev_with_gpu if nvidia_nodes else self.dev_without_gpu),
               "HOME": str(pathlib.Path(self._tmp.name) / "no-home"),
               "HF_HUB_CACHE": str(self.full_cache if cache is None else cache)}
        env.update(env_extra or {})
        return subprocess.run([sys.executable, str(_RESOLVER), *args], capture_output=True,
                              text=True, env={k: v for k, v in env.items() if v is not None},
                              timeout=30, check=False)


class ResolveModelTest(ResolverRun):

    def assert_resolves(self, args, want, gpus=1):
        result = self.resolve(*args, gpus=gpus)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, want + "\n", "stdout must be the model and nothing else")

    def test_auto_with_a_gpu_and_english_is_distil_large_v3_5_as_its_repo(self):
        self.assert_resolves(["--mode", "batch", "--language", "en", "auto"],
                             "distil-whisper/distil-large-v3.5-ct2")
        self.assert_resolves(["--mode", "streaming", "--language", "en", "auto"],
                             "distil-whisper/distil-large-v3.5-ct2")

    def test_auto_with_a_gpu_and_another_language_is_turbo(self):
        self.assert_resolves(["--mode", "batch", "--language", "de", "auto"], "large-v3-turbo")
        self.assert_resolves(["--mode", "streaming", "--language", "fr", "auto"], "large-v3-turbo")

    def test_auto_with_a_gpu_and_language_detection_is_turbo(self):
        self.assert_resolves(["--mode", "batch", "--language", "", "auto"], "large-v3-turbo")
        self.assert_resolves(["--mode", "batch", "auto"], "large-v3-turbo")

    def test_auto_without_a_gpu_keeps_the_per_mode_defaults(self):
        self.assert_resolves(["--mode", "batch", "--language", "en", "auto"], "small", gpus=0)
        self.assert_resolves(["--mode", "streaming", "--language", "en", "auto"], "base", gpus=0)

    def test_no_model_argument_means_auto(self):
        self.assert_resolves(["--mode", "streaming", "--language", "en"], "base", gpus=0)
        self.assert_resolves(["--mode", "streaming", "--language", "en", ""], "base", gpus=0)

    def test_a_chosen_model_is_kept_without_asking_about_the_gpu(self):
        # gpus=-1 would make the stub's answer nonsense; a chosen model must not ask
        self.assert_resolves(["--mode", "batch", "--language", "en", "medium"], "medium", gpus=-1)
        self.assert_resolves(["--mode", "streaming", "--language", "de", "large-v3-turbo"],
                             "large-v3-turbo", gpus=-1)

    def test_a_chosen_distil_large_v3_5_is_passed_as_its_repo(self):
        self.assert_resolves(["--mode", "streaming", "--language", "en", "distil-large-v3.5"],
                             "distil-whisper/distil-large-v3.5-ct2", gpus=0)

    def test_an_english_only_model_with_another_language_is_refused(self):
        for model in ("distil-large-v3.5", "base.en"):
            result = self.resolve("--mode", "batch", "--language", "de", model)
            self.assertEqual(result.returncode, 1, model)
            self.assertEqual(result.stdout, "", model)
            self.assertIn("English only", result.stderr, model)

    def test_an_english_only_model_with_language_detection_is_allowed(self):
        self.assert_resolves(["--mode", "batch", "--language", "", "small.en"], "small.en")

    def test_the_auto_choice_is_reported_on_stderr(self):
        result = self.resolve("--mode", "batch", "--language", "en", "auto")
        self.assertIn("distil-large-v3.5", result.stderr)
        self.assertIn("GPU", result.stderr)

    def test_an_unusable_gpu_probe_fails_loudly(self):
        result = self.resolve("--mode", "batch", "--language", "en", "auto", gpus="not-a-number")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        self.assertIn("CUDA", result.stderr)

    def test_a_gpu_ctranslate2_cannot_see_fails_instead_of_falling_back_to_the_cpu(self):
        # CTranslate2 answers 0, not an error, when CUDA is broken on a GPU machine
        result = self.resolve("--mode", "batch", "--language", "en", "auto", gpus=0,
                              nvidia_nodes=True)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        self.assertIn("counts 0", result.stderr)
        self.assertIn("nvidia0", result.stderr)

    def test_a_gpu_ctranslate2_sees_is_used(self):
        result = self.resolve("--mode", "batch", "--language", "en", "auto", gpus=1,
                              nvidia_nodes=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "distil-whisper/distil-large-v3.5-ct2\n")

    def test_gpu_count_prints_the_count_alone(self):
        for gpus, nodes in ((0, False), (2, True)):
            result = self.resolve("--gpu-count", gpus=gpus, nvidia_nodes=nodes)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, f"{gpus}\n")

    def test_gpu_count_fails_on_a_gpu_ctranslate2_cannot_see(self):
        result = self.resolve("--gpu-count", gpus=0, nvidia_nodes=True)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")

    def test_a_missing_mode_is_a_usage_error(self):
        result = self.resolve("auto")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")


class ModelOnDiskTest(ResolverRun):
    """Plan 00156 Task 2.4: no model is downloaded by default, so the resolver names a
    model only when its weights are on disk, and otherwise exits 3 with one line saying
    what to download and where. faster-whisper is never left to download it."""

    NOT_DOWNLOADED = 3

    def setUp(self):
        self._case = tempfile.TemporaryDirectory()
        self.case = pathlib.Path(self._case.name)

    def tearDown(self):
        self._case.cleanup()

    def assert_not_downloaded(self, result, *phrases):
        self.assertEqual(result.returncode, self.NOT_DOWNLOADED, result.stderr)
        self.assertEqual(result.stdout, "", "nothing a recorder could load may be printed")
        last = result.stderr.strip().splitlines()[-1]
        for phrase in ("model manager", *phrases):
            self.assertIn(phrase, last, "the reason is the last stderr line, for the panel")

    def test_no_model_downloaded_at_all_says_download_one(self):
        result = self.resolve("--mode", "streaming", "--language", "en", "auto",
                              cache=self.case / "empty")
        self.assert_not_downloaded(result, "No speech model is downloaded",
                                   "distil-large-v3.5")

    def test_auto_picking_a_model_not_downloaded_names_it_and_the_ones_there_are(self):
        cache = make_cache(self.case / "cache", "Systran/faster-whisper-small")
        result = self.resolve("--mode", "batch", "--language", "en", "auto", cache=cache)
        self.assert_not_downloaded(result, "auto picked distil-large-v3.5",
                                   "not downloaded", "small")

    def test_a_chosen_model_not_downloaded_is_refused(self):
        cache = make_cache(self.case / "cache", "Systran/faster-whisper-small")
        result = self.resolve("--mode", "batch", "--language", "en", "medium", cache=cache)
        self.assert_not_downloaded(result, "Whisper Model setting is medium", "not downloaded")

    def test_an_interrupted_download_is_not_downloaded(self):
        cache = make_cache(self.case / "cache", "Systran/faster-whisper-base", weights=False)
        result = self.resolve("--mode", "streaming", "--language", "en", "auto", gpus=0,
                              cache=cache)
        self.assert_not_downloaded(result, "No speech model is downloaded", "base")

    def test_weights_whose_blob_is_gone_are_not_downloaded(self):
        cache = make_cache(self.case / "cache", "Systran/faster-whisper-base")
        (cache / "models--Systran--faster-whisper-base" / "blobs" / "f00d").unlink()
        result = self.resolve("--mode", "streaming", "--language", "en", "base", cache=cache)
        self.assert_not_downloaded(result, "base")

    def test_the_downloaded_model_resolves(self):
        cache = make_cache(self.case / "cache", "mobiuslabsgmbh/faster-whisper-large-v3-turbo")
        result = self.resolve("--mode", "batch", "--language", "de", "auto", cache=cache)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "large-v3-turbo\n")

    def test_the_cache_is_found_where_huggingface_hub_looks(self):
        repo = "Systran/faster-whisper-small"
        homes = {
            "HF_HOME": ({"HF_HOME": str(self.case / "hf")}, self.case / "hf" / "hub"),
            "XDG_CACHE_HOME": ({"XDG_CACHE_HOME": str(self.case / "xdg")},
                               self.case / "xdg" / "huggingface" / "hub"),
            "HOME": ({"HOME": str(self.case / "home")},
                     self.case / "home" / ".cache" / "huggingface" / "hub"),
        }
        for label, (env, where) in homes.items():
            make_cache(where, repo)
            result = self.resolve("--mode", "batch", "--language", "en", "small",
                                  env_extra={"HF_HUB_CACHE": None, **env})
            self.assertEqual(result.returncode, 0, f"{label}: {result.stderr}")
            self.assertEqual(result.stdout, "small\n", label)

    def test_a_local_model_directory_needs_its_weights(self):
        model_dir = self.case / "my-model"
        model_dir.mkdir()
        result = self.resolve("--mode", "batch", "--language", "en", str(model_dir))
        self.assert_not_downloaded(result, str(model_dir))
        (model_dir / "model.bin").write_bytes(b"weights")
        result = self.resolve("--mode", "batch", "--language", "en", str(model_dir))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, f"{model_dir}\n")

    def test_a_name_faster_whisper_does_not_know_is_refused(self):
        result = self.resolve("--mode", "batch", "--language", "en", "enormous")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        self.assertIn("enormous", result.stderr)

    def test_suggest_names_the_choice_without_looking_at_the_disk(self):
        result = self.resolve("--suggest", "--mode", "streaming", "--language", "en", "auto",
                              cache=self.case / "empty")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "distil-whisper/distil-large-v3.5-ct2\n")


if __name__ == "__main__":
    unittest.main()

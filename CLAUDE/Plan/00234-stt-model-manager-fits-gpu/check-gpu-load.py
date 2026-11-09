#!/usr/bin/env python3
"""Plan 00234 — check-gpu-load.py: one leg of gpu-load-acceptance.bash. Loads the model
the manager preselects on this machine's real GPU, the way the recorders do, and compares
the peak GPU memory with what wsi-resolve-model --fit says the model needs.

    download    fetch distil-large-v3.5 if it is not on disk (the only download)
    decode      WhisperModel on CUDA in float16, as wsi-stream-server loads it, transcribing
                30 s of noise with beam 5 (the case the fit rule was calibrated on)
    streaming   wsi-stream's own AudioToTextRecorder settings (real-time transcription on,
                so its live preview model, wsi-resolve-model --realtime's, loads beside the
                main one); that model must already be on disk, or the leg fails at once

Each measurement runs in a child process with HF_HUB_OFFLINE=1, so nothing else can be
downloaded; the parent samples nvidia-smi's memory.used every 0.2 s while the child runs and
reports the peak above the level before it started. A load that needs more than the fit
rule's figure fails: the rule under-states it, and the manager would offer a model that
does not fit. The report goes to stdout, failures say why on stderr, exit 1.

Usage: check-gpu-load.py download|decode|streaming
"""

import json
import os
import pathlib
import subprocess
import sys
import threading
import time

BIN = pathlib.Path.home() / ".local" / "bin"
MODEL = "distil-large-v3.5"
REPO = "distil-whisper/distil-large-v3.5-ct2"
NOT_DOWNLOADED = 3
SETTLE_SECONDS = 5


def load_decode(model):
    """Child: the model as wsi-stream-server loads it, transcribing 30 s of noise."""
    import numpy as np
    from faster_whisper import WhisperModel
    whisper = WhisperModel(model, device="cuda", compute_type="float16")
    audio = (np.random.default_rng(0).standard_normal(16000 * 30) * 0.1).astype(np.float32)
    segments, _ = whisper.transcribe(audio, language="en", beam_size=5, vad_filter=False)
    print("decoded:", " ".join(s.text for s in segments)[:120], file=sys.stderr)
    time.sleep(SETTLE_SECONDS)


def load_streaming(model, realtime_model):
    """Child: wsi-stream's prebuffered recorder_config, word for word but the callback.

    RealtimeSTT 1.0.0 loads the main model in a worker thread it means to be a daemon
    (`thread.deamon = True`, misspelt). An error there ends only that thread and the
    constructor waits for ever; an error the constructor raises leaves that thread keeping
    the process alive. Both end the child here at once, as wsi-stream's load_recorder does."""
    from RealtimeSTT import AudioToTextRecorder

    def thread_died(hook_args):
        print(f"FAIL: a recorder thread died: {hook_args.exc_value!r}", file=sys.stderr)
        sys.stderr.flush()
        os._exit(1)

    threading.excepthook = thread_died
    try:
        recorder = AudioToTextRecorder(
            model=model, language="en", use_microphone=False, silero_sensitivity=0.4,
            enable_realtime_transcription=True, realtime_model_type=realtime_model,
            realtime_processing_pause=0.2, on_realtime_transcription_update=lambda text: None,
            no_log_file=True, spinner=False, device="cuda", compute_type="float16")
    except Exception as e:
        print(f"FAIL: the recorder did not load: {e!r}", file=sys.stderr)
        sys.stderr.flush()
        os._exit(1)
    print("recorder loaded", file=sys.stderr)
    time.sleep(SETTLE_SECONDS)
    recorder.shutdown()


CHILDREN = {"decode": load_decode, "streaming": load_streaming}


def fail(reason):
    print(f"FAIL: {reason}", file=sys.stderr)
    return 1


def resolve(*args):
    """The model wsi-stream would hand faster-whisper (by default the one under test), or
    None if it is not downloaded."""
    result = subprocess.run([str(BIN / "wsi-resolve-model"),
                             *(args or ("--mode", "streaming", "--language", "en", MODEL))],
                            capture_output=True, text=True, timeout=60, check=False)
    if result.returncode == NOT_DOWNLOADED:
        return None
    if result.returncode != 0:
        raise RuntimeError(f"wsi-resolve-model exited {result.returncode}: {result.stderr.strip()}")
    return result.stdout.strip()


def needs_mb():
    result = subprocess.run([str(BIN / "wsi-resolve-model"), "--fit"], capture_output=True,
                            text=True, timeout=60, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"wsi-resolve-model --fit exited {result.returncode}: "
                           f"{result.stderr.strip()}")
    report = json.loads(result.stdout)
    if report["device"] != "cuda":
        raise RuntimeError("--fit judged no GPU, so there is no GPU load to measure")
    print(f"GPU: {report['gpu']}, {report['gpu_memory_mb']} MiB total")
    return report["models"][MODEL]["needs_mb"]


def memory_used_mb():
    result = subprocess.run(["nvidia-smi", "--query-gpu=memory.used",
                             "--format=csv,noheader,nounits"],
                            capture_output=True, text=True, timeout=30, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"nvidia-smi failed (exit {result.returncode}): {result.stderr.strip()}")
    return sum(int(line) for line in result.stdout.split())


def check_download():
    if resolve() is not None:
        print(f"{MODEL}: already downloaded, nothing fetched")
        return 0
    from huggingface_hub import snapshot_download
    print(f"{MODEL}: not on disk, downloading {REPO}", file=sys.stderr)
    snapshot_download(repo_id=REPO, token=False)  # public repo: no keyring prompt
    if resolve() is None:
        return fail(f"{MODEL} still does not resolve after the download")
    print(f"{MODEL}: downloaded")
    return 0


def realtime_model():
    """wsi-stream's live preview model for English, or None after saying how to get it."""
    model = resolve("--realtime", "--language", "en")
    if model is None:
        fail("the live preview model wsi-stream loads (wsi-resolve-model --realtime "
             "--language en) is not on disk, so the streaming load cannot run offline. "
             "Run play-speech-to-text.yml, which downloads it, then this again.")
    return model


def measure(name):
    model = resolve()
    if model is None:
        return fail(f"{MODEL} is not downloaded; the download leg runs first")
    child_args = [name, model]
    if name == "streaming":
        preview = realtime_model()
        if preview is None:
            return 1
        print(f"streaming: the live preview model is {preview}")
        child_args.append(preview)
    need = needs_mb()
    baseline = memory_used_mb()
    run_dir = os.environ.get("PLAN_RUN_DIR")
    if not run_dir:
        return fail("PLAN_RUN_DIR is not set: run this through gpu-load-acceptance.bash")
    # The child is this file, so the processes RealtimeSTT spawns re-import it safely. It runs
    # in the run's log directory, so nothing it writes lands in the checkout.
    child = subprocess.Popen([sys.executable, __file__, "--child", *child_args],
                             env={**os.environ, "HF_HUB_OFFLINE": "1"}, cwd=run_dir)
    peak = baseline
    sampling_error = []

    def sample():
        nonlocal peak
        while child.poll() is None:
            try:
                peak = max(peak, memory_used_mb())
            except (RuntimeError, ValueError, subprocess.TimeoutExpired) as e:
                sampling_error.append(str(e))
                return
            time.sleep(0.2)

    sampler = threading.Thread(target=sample)
    sampler.start()
    try:
        status = child.wait(timeout=600)
    except subprocess.TimeoutExpired:
        child.kill()
        raise
    finally:
        sampler.join()
    if sampling_error:
        return fail(f"cannot read the GPU memory while {name} ran: {sampling_error[0]}")
    if status != 0:
        return fail(f"the {name} load exited {status} (in use before it started: {baseline} MiB)")
    used = peak - baseline
    print(f"{name}: peak {used} MiB above the {baseline} MiB in use before; "
          f"the fit rule says {MODEL} needs {need} MiB")
    if used > need:
        return fail(f"{name} used {used} MiB, more than the {need} MiB the fit rule allows")
    return 0


CHECKS = {"download": check_download,
          "decode": lambda: measure("decode"),
          "streaming": lambda: measure("streaming")}

if __name__ == "__main__":
    if len(sys.argv) >= 4 and sys.argv[1] == "--child" and sys.argv[2] in CHILDREN:
        CHILDREN[sys.argv[2]](*sys.argv[3:])
        # The load has been measured; RealtimeSTT's worker thread (above) must not keep
        # the child alive past it.
        sys.stdout.flush()
        sys.stderr.flush()
        os._exit(0)
    if len(sys.argv) != 2 or sys.argv[1] not in CHECKS:
        print(f"usage: check-gpu-load.py {'|'.join(CHECKS)}", file=sys.stderr)
        sys.exit(64)
    try:
        sys.exit(CHECKS[sys.argv[1]]())
    except (RuntimeError, OSError, ValueError, KeyError, subprocess.TimeoutExpired) as e:
        sys.exit(fail(str(e)))

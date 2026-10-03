"""Installed speech package versions and what they can do: <report.md>.

Plan 00148 triage (research section 3.6, and Task 4.6's pins). Reports, without judging:
the version and location of each package the recorders import; which Silero VAD asset
faster-whisper ships and whether its model loads and answers for 512-sample frames (the
continuous-dictation segmenter depends on it); whether faster-whisper knows the name
distil-large-v3.5; RealtimeSTT's declared faster-whisper requirement; the CUDA count.
"""

from __future__ import annotations

import importlib.metadata
import pathlib
import sys

from probe_common import append, run_helper, say

PACKAGES = ["RealtimeSTT", "faster-whisper", "ctranslate2", "onnxruntime", "numpy"]


def main(report: str) -> int:
    lines = ["## Versions", "", f"- python: {sys.version.split()[0]} ({sys.executable})"]
    missing = []
    for name in PACKAGES:
        try:
            dist = importlib.metadata.distribution(name)
        except importlib.metadata.PackageNotFoundError:
            lines.append(f"- {name}: NOT INSTALLED")
            missing.append(name)
            continue
        lines.append(f"- {name}: {dist.version} ({dist.locate_file('')})")
        if name == "RealtimeSTT":
            needs = [r for r in (dist.requires or []) if r.lower().startswith("faster")]
            lines.append(f"  - declares: {', '.join(needs) or 'no faster-whisper requirement'}")

    import faster_whisper.utils as fw_utils
    import faster_whisper.vad as fw_vad
    import numpy as np
    lines.append(f"- faster-whisper knows distil-large-v3.5: {'distil-large-v3.5' in fw_utils._MODELS}")
    assets = sorted(p.name for p in pathlib.Path(fw_utils.get_assets_path()).iterdir())
    lines.append(f"- faster-whisper assets: {', '.join(assets)}")
    model = fw_vad.get_vad_model()
    one_second = np.zeros(16000 - 16000 % 512, dtype=np.float32)
    shape = "2-D (batch, samples)" if hasattr(model, "encoder_session") else "1-D"
    probs = model(one_second[None, :] if shape.startswith("2") else one_second)
    lines.append(f"- Silero VAD loads: yes; input {shape}; {np.asarray(probs).size} "
                 f"probabilities for {len(one_second) // 512} frames of silence, "
                 f"max {float(np.max(probs)):.3f}")
    lines.append(f"- CUDA devices CTranslate2 counts: {run_helper('wsi-resolve-model', '--gpu-count')}")
    append(report, "\n".join(lines))
    say("\n".join(lines))
    if missing:
        raise RuntimeError(f"not installed: {', '.join(missing)}")
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:]))

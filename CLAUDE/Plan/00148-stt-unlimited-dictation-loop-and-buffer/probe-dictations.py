"""How real dictation performed, from the warm server's own figures: <report.md> [<server.log>].

Plan 00148 triage (research section 3.6: the real-time factor of the chosen model, and how
often a hard cut lands inside speech). The server logs one "Dictation stats:" line per
finished dictation (wsi-stream-server, DictationSession._log_stats_locked), so this reads
those lines rather than loading a model of its own: a second copy of the model does not
fit on the GPU beside the warm server's, and the figures of real dictation are the ones
that matter anyway. Nothing is recorded and nothing is transcribed here.

The log is the default ~/.local/share/speech-to-text/server.log unless one is named.
"""

from __future__ import annotations

import pathlib
import sys

from probe_common import append, say

DEFAULT_LOG = pathlib.Path.home() / ".local" / "share" / "speech-to-text" / "server.log"
MARKER = "Dictation stats: "


def parse(line: str) -> dict[str, str]:
    """The key=value fields of one stats line, with its timestamp under "at"."""
    fields = dict(field.split("=", 1) for field in line.split(MARKER, 1)[1].split())
    fields["at"] = line[1:20] if line.startswith("[") else "?"
    return fields


def main(report: str, log_path: str = str(DEFAULT_LOG)) -> int:
    path = pathlib.Path(log_path)
    if not path.is_file():
        raise RuntimeError(f"{path} does not exist: the warm server has never run here")
    rows = [parse(line) for line in path.read_text(errors="replace").splitlines() if MARKER in line]
    continuous = [r for r in rows if r["mode"] == "continuous"]
    lines = ["## Real dictation, from the server's own figures", "",
             f"- log: {path}",
             f"- dictations with figures: {len(rows)}; continuous: {len(continuous)}"]
    if not continuous:
        lines.append("- no continuous dictation logged yet: turn on Settings -> Continuous "
                     "Dictation, dictate at length in Server mode, then run this again")
        append(report, "\n".join(lines))
        say("\n".join(lines))
        return 0

    audio = sum(float(r["audio_s"]) for r in continuous)
    took = sum(float(r["transcribe_s"]) for r in continuous)
    hard = sum(int(dict(c.split(":") for c in r["cuts"].split(","))["hard"]) for r in continuous)
    lines += [
        f"- continuous audio: {audio / 60:.1f} min in {sum(int(r['segments']) for r in continuous)} segments",
        f"- real-time factor overall {took / audio if audio else 0:.3f}; worst segment "
        f"{max(float(r['rtf_max']) for r in continuous):.3f} (above 1, transcription falls behind)",
        f"- worst backlog: {max(float(r['backlog_max_s']) for r in continuous):.1f} s of audio waiting",
        f"- hard cuts (inside speech, no pause by 28 s): {hard}, "
        f"{hard / (audio / 60) if audio else 0:.2f} per minute",
        f"- failed: {sum(1 for r in continuous if r['outcome'] == 'failed')}",
        "", "| at | outcome | audio (s) | segments | RTF mean | RTF max | backlog max (s) | cuts |",
        "| --- | --- | --- | --- | --- | --- | --- | --- |",
    ]
    lines += [f"| {r['at']} | {r['outcome']} | {r['audio_s']} | {r['segments']} | {r['rtf_mean']} "
              f"| {r['rtf_max']} | {r['backlog_max_s']} | {r['cuts']} |" for r in continuous]
    append(report, "\n".join(lines))
    say("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:]))

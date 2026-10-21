#!/usr/bin/python3
"""Plan 00212 acceptance fixture: a fake pactl with one sound server's sink inputs in a file.

    fake-pactl.py --format=json list sink-inputs
    fake-pactl.py set-sink-input-volume INDEX V...
    fake-pactl.py init PID VOLUME          (test only: one stream, index 7, of process PID)
    fake-pactl.py end-stream INDEX         (test only: the stream ends)

The sink inputs are kept in $FAKE_PACTL_STATE as pactl --format=json prints them (the fields
wsi-lower-media-volume reads, and a few it does not). Each set-sink-input-volume appends
"<index> <v>..." to $FAKE_PACTL_STATE.log. check-media-volume.bash puts it first on PATH as
pactl, so no real stream's volume is touched. Anything else is exit 1, as pactl's own refusal.
"""

import json
import os
import sys

CHANNELS = ["front-left", "front-right"]


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def save(path, streams):
    with open(path + ".tmp", "w", encoding="utf-8") as f:
        json.dump(streams, f)
    os.replace(path + ".tmp", path)


def stream(index, pid, volume):
    return {
        "index": index,
        "driver": "PipeWire",
        "corked": False,
        "mute": False,
        "channel_map": ",".join(CHANNELS),
        "volume": {ch: {"value": volume, "value_percent": "", "db": ""} for ch in CHANNELS},
        "properties": {"application.process.id": str(pid), "application.name": "plan00212"},
    }


def refuse(words):
    print(f"Failure: {words}", file=sys.stderr)
    return 1


def main(argv):
    path = os.environ.get("FAKE_PACTL_STATE")
    if not path:
        return refuse("FAKE_PACTL_STATE is not set")
    if argv == ["--format=json", "list", "sink-inputs"]:
        print(json.dumps(load(path)))
        return 0
    if len(argv) == 3 and argv[0] == "init":
        save(path, [stream(7, int(argv[1]), int(argv[2]))])
        return 0
    streams = load(path)
    if len(argv) == 2 and argv[0] == "end-stream":
        save(path, [s for s in streams if s["index"] != int(argv[1])])
        return 0
    if len(argv) >= 3 and argv[0] == "set-sink-input-volume":
        found = [s for s in streams if s["index"] == int(argv[1])]
        if not found:
            return refuse("No such entity")
        channels = found[0]["channel_map"].split(",")
        if len(argv) - 2 != len(channels):
            return refuse("Invalid argument")
        for ch, v in zip(channels, argv[2:], strict=True):
            found[0]["volume"][ch]["value"] = int(v)
        save(path, streams)
        with open(path + ".log", "a", encoding="utf-8") as log:
            log.write(" ".join(argv[1:]) + "\n")
        return 0
    return refuse("fake pactl: unscripted: " + " ".join(argv))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

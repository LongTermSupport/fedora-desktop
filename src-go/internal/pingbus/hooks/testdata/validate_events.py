"""The oracle of the Go validator: v1's protocol.validate_event over the cases on stdin.

Usage: python3 -I validate_events.py CHECKOUT < cases.json

stdin: {"record": <team record JSON text>, "server": S, "team": T, "cases": [{"event": <event
JSON text>, "self": U, "human_text": B, "room": R}, ...]}
stdout: one JSON object per case: {"kind": K, "reason": R, "class": human|peer|ping|""}, or
{"error": <exception class>} when v1 raises (the hooks report that as internal).
"""

import importlib
import json
import sys


def main() -> None:
    sys.path.insert(0, sys.argv[1])
    protocol = importlib.import_module("helpers.pingbus.protocol")
    request = json.load(sys.stdin)
    server = request["server"]
    record = protocol.parse_team_record(json.loads(request["record"]), server, request["team"])
    ctx = record.context(server)
    for case in request["cases"]:
        event = json.loads(case["event"])
        try:
            outcome = protocol.validate_event(event, ctx, case["self"], human_text=case["human_text"],
                                              room=case["room"])
        except (TypeError, ValueError) as exc:
            print(json.dumps({"error": type(exc).__name__}))
            continue
        cls = "human" if outcome.human else "peer" if outcome.peer else "ping" if outcome.ping else ""
        print(json.dumps({"kind": outcome.kind, "reason": outcome.reason, "class": cls}))


main()

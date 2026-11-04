"""Corpus adapters for the ping, text, secret and receive suites (SPEC-proto.md P24-P42).

Each adapter takes a case's `args` and its context and returns the verdict as JSON; see
test_protocol_corpus.py for the corpus format. A receive outcome is
`{"verdict": "drop"|"ignore", "reason": <code>}` or `{"verdict": "accept", <kind>: <item>}`
with kind `ping`, `human` or `peer`.
"""

from __future__ import annotations

from helpers.pingbus import protocol as p
from helpers.pingbus.protocol import secret_shaped


def _ping_fields(ping: p.Ping) -> dict:
    return {"verb": ping.verb, "to": list(ping.to), "ref": ping.ref.text if ping.ref is not None else None,
            "re": ping.re, "trust": ping.trust, "branch": ping.branch}


def _optional_keys(args: dict) -> dict:
    return {key: args[key] for key in ("ref", "re", "branch") if args[key] is not None}


def _render_ping(args: dict, _ctx: p.Context | None) -> object:
    return p.render({"verb": args["verb"], "to": args["to"], **_optional_keys(args)})


def _validate_ping_content(args: dict, ctx: p.Context) -> object:
    return _ping_fields(p.validate_content(args["content"], ctx))


def _validate_text_content(args: dict, ctx: p.Context) -> object:
    text = p.validate_text_content(args["content"], ctx, check_secret=args["check_secret"])
    return {"to": list(text.to), "text": text.text}


def _outcome(outcome: p.Outcome) -> object:
    if outcome.kind != p.ACCEPT:
        return {"verdict": outcome.kind, "reason": outcome.reason}
    if outcome.ping is not None:
        ping = outcome.ping
        return {"verdict": p.ACCEPT, "ping": {**_ping_fields(ping), "sender": ping.sender,
                                              "event_id": ping.event_id, "ts": ping.origin_server_ts,
                                              "copy_of": list(ping.copy_of)}}
    if outcome.human is not None:
        human = outcome.human
        return {"verdict": p.ACCEPT, "human": {"event_id": human.event_id, "sender": human.sender,
                                               "ts": human.origin_server_ts, "text": human.text,
                                               "revises": human.revises}}
    peer = outcome.peer
    return {"verdict": p.ACCEPT, "peer": {"event_id": peer.event_id, "sender": peer.sender,
                                          "ts": peer.origin_server_ts, "text": peer.text,
                                          "copy_of": list(peer.copy_of)}}


def _receive(args: dict, ctx: p.Context) -> p.Outcome:
    return p.validate_event(args["event"], ctx, args["self"], frozenset(args["seen"]), args["human_text"],
                            args["room"])


def _closed_sets(_args: dict, _ctx: p.Context | None) -> object:
    return {"drop": list(p.DROP_REASONS), "send": list(p.SEND_REFUSALS), "unheard": list(p.UNHEARD_REASONS)}


ADAPTERS = {
    # ping (P24-P29)
    "build_ping": lambda args, _ctx: p.build_ping(args["verb"], args["to"], args["ref"], args["re"],
                                                  args["branch"]),
    "render_ping": _render_ping,
    "validate_ping_content": _validate_ping_content,
    # agent text (P30-P32)
    "build_text": lambda args, _ctx: p.build_text(args["to"], args["text"], args["re"]),
    "render_text": lambda args, _ctx: p.render_text({"to": args["to"], "text": args["text"]}),
    "validate_text_content": _validate_text_content,
    # the shapes an agent may not send (P33)
    "secret_shape": lambda args, _ctx: secret_shaped(args["value"]),
    # receive (P35-P42)
    "validate_event": lambda args, ctx: _outcome(_receive(args, ctx)),
    "strip_reply_fallback": lambda args, _ctx: p.strip_reply_fallback(args["value"]),
    "reached_no_agent": lambda args, ctx: p.reached_no_agent(args["event"], ctx, _receive(args, ctx)),
    "closed_sets": _closed_sets,
}

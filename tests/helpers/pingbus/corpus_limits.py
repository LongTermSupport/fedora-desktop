"""Corpus adapters for the limits and lines suites (SPEC-proto.md P59-P72); see
test_protocol_corpus.py for the corpus format.

The stateful limits (the token bucket, the duplicate window, the send gate, the report window,
the receive flood) are run as a list of steps from a given state, and answer each step's result
and the final state. A server wait is answered in whole milliseconds, held to int64.
"""

from __future__ import annotations

from helpers.pingbus import cli
from helpers.pingbus import limits as lim
from helpers.pingbus import protocol as p
from helpers.pingbus import syncer

_INT64_MAX = 2 ** 63 - 1


def _limits(args: dict) -> lim.Limits:
    return lim.parse_limits(args["limits"])


def _bucket_run(args: dict, _ctx: p.Context | None) -> object:
    limits = _limits(args)
    state = args["state"]
    bucket = (lim.TokenBucket.full(limits, args["start_ms"]) if state is None
              else lim.TokenBucket.from_dict(state, limits))
    took = [bucket.take(now) for now in args["takes"]]
    return {"took": took, "state": bucket.to_dict()}


def _window_run(args: dict, _ctx: p.Context | None) -> object:
    window = lim.DuplicateWindow.from_dict(args["state"])
    results: list[object] = []
    for op, key, now in args["ops"]:
        if op == "seen":
            results.append(window.seen(key, now))
        else:
            window.record(key, now)
            results.append(None)
    return {"results": results, "state": window.to_dict()}


def _gate_run(args: dict, _ctx: p.Context | None) -> object:
    limits = _limits(args)
    state = args["state"]
    gate = lim.SendGate.fresh(limits, args["start_ms"]) if state is None else lim.SendGate.from_dict(state, limits)
    results = []
    for s in args["sends"]:
        try:
            gate.admit(s["verb"], s["ref"], s["re"], s["to"], s["now"], deduplicate=s["dedupe"])
        except lim.RateLimited as exc:
            results.append(exc.reason)
        else:
            results.append("ok")
    return {"results": results, "state": gate.to_dict()}


def _report_run(args: dict, _ctx: p.Context | None) -> object:
    window = lim.ReportWindow.from_dict(args["state"])
    results = []
    for op, now in args["ops"]:
        try:
            (window.check if op == "check" else window.admit)(args["per_hour"], now)
        except lim.RateLimited as exc:
            results.append(exc.reason)
        else:
            results.append("ok")
    return {"results": results, "state": window.to_dict()}


def _flood_run(args: dict, _ctx: p.Context | None) -> object:
    flood = lim.ReceiveFlood(_limits(args))
    return [flood.admit(sender, now) for sender, now in args["admits"]]


def _server_wait(args: dict, _ctx: p.Context | None) -> object:
    wait = lim.server_retry_wait_s(args["headers"], args["body"], reset_header=args["reset_header"],
                                   now_s=args["now_s"])
    ms = wait * 1000 if isinstance(wait, int) else round(wait * 1000)
    return min(ms, _INT64_MAX)


def _constants(_args: dict, _ctx: p.Context | None) -> object:
    return {"duplicate_window_s": lim.DUPLICATE_WINDOW_S, "report_window_s": lim.REPORT_WINDOW_S,
            "sync_long_poll_s": lim.SYNC_LONG_POLL_S, "server_429_max_tries": lim.SERVER_429_MAX_TRIES,
            "server_429_default_wait_s": lim.SERVER_429_DEFAULT_WAIT_S,
            "server_429_wait_max_s": lim.SERVER_429_WAIT_MAX_S}


def _ref(text: str | None) -> p.Ref | None:
    """A holder for the reference text: the line re-checks the text itself."""
    return None if text is None else p.Ref(text, "commit", "", "", None, None, None)


def _ping(d: dict) -> p.Ping:
    return p.Ping(d["verb"], (), _ref(d["ref"]), d["re"], d["sender"], d["event_id"], 0, d["trust"], d["branch"],
                  tuple(d["copy_of"]))


def _human(d: dict) -> p.HumanMessage:
    return p.HumanMessage(d["event_id"], d["sender"], d["ts"], d["text"])


def _peer(d: dict) -> p.PeerText:
    return p.PeerText(d["event_id"], d["sender"], d["ts"], d["text"], tuple(d["copy_of"]))


def _question(q: list | None) -> tuple[int, str] | None:
    return None if q is None else (q[0], q[1])


def _reply_line(args: dict, ctx: p.Context) -> object:
    record = p.TeamRecord(args["team"], ctx.humans, ctx.roles, ctx.repos, tuple(ctx.path_prefixes),
                          "https://api.github.com", ctx.org, tuple(ctx.exclude))
    kind, m = args["kind"], args["message"]
    outcome = p.Outcome(p.ACCEPT, ping=_ping(m) if kind == "ping" else None,
                        human=_human(m) if kind == "human" else None, peer=_peer(m) if kind == "peer" else None)
    return cli.reply_line(args["team"], ctx.server_name, record, outcome, args["room"], _question(args["question"]))


def _history_line(args: dict, _ctx: p.Context | None) -> object:
    it = args["item"]
    item = syncer.HistoryItem(
        it["direction"], it["ts"],
        ping=None if it["ping"] is None else _ping(it["ping"]),
        human=None if it["human"] is None else _human(it["human"]),
        peer=None if it["peer"] is None else _peer(it["peer"]),
        sent_event_id=it["sent_event_id"], room=it["room"], question=_question(it["question"]))
    return cli.history_line(args["team"], args["server_name"], item)


def _question_line(args: dict, _ctx: p.Context | None) -> object:
    v = args["view"]
    question = p.Question(v["n"], v["event_id"], "@app.1+desk.podman:server.test", "headline", "question",
                          tuple(v["asked_by"]), ())
    view = p.QuestionView(question, v["state"], (), tuple(["$reply"] * v["unanswered"]))
    return cli.question_line(args["team"], args["server_name"], view)


def _reply_to_line(args: dict, _ctx: p.Context | None) -> object:
    return cli.format_line("REPLY_TO", cli._team(args["team"]), f"Q{args['n']}", cli._event_id(args["event_id"]))


ADAPTERS = {
    # limits (P59-P66)
    "parse_limits": lambda args, _ctx: lim.parse_limits(args["value"]).as_dict(),
    "check_wait_timeout": lambda args, _ctx: lim.check_wait_timeout(args["value"]),
    "ack_timeout_s": lambda args, _ctx: lim.ack_timeout_s(args["verb"], _limits(args)),
    "ack_deadline_ms": lambda args, _ctx: lim.ack_deadline_ms(args["sent_ms"], args["verb"], _limits(args)),
    "ack_overdue": lambda args, _ctx: lim.ack_overdue(args["sent_ms"], args["verb"], args["now_ms"], _limits(args)),
    "ping_stale": lambda args, _ctx: lim.ping_stale(args["origin_ms"], args["verb"], args["now_ms"], _limits(args)),
    "human_stale": lambda args, _ctx: lim.human_stale(args["origin_ms"], args["now_ms"], _limits(args)),
    "token_bucket_run": _bucket_run,
    "duplicate_key": lambda args, _ctx: lim.duplicate_key(args["verb"], args["ref"], args["re"], args["to"]),
    "duplicate_window_run": _window_run,
    "send_gate_run": _gate_run,
    "report_window_run": _report_run,
    "receive_flood_run": _flood_run,
    "server_retry_wait": _server_wait,
    "limits_constants": _constants,
    # item lines (P67-P72)
    "format_line": lambda args, _ctx: cli.format_line(args["kind"], *args["fields"]),
    "ping_line": lambda args, _ctx: cli.ping_line(args["team"], args["server_name"], _ping(args["ping"])),
    "human_line": lambda args, _ctx: cli.human_line(args["team"], args["server_name"], _human(args["message"]),
                                                    args["room"], _question(args["question"])),
    "peer_line": lambda args, _ctx: cli.peer_line(args["team"], args["server_name"], _peer(args["message"])),
    "reply_line": _reply_line,
    "timeout_line": lambda args, _ctx: cli.timeout_line(args["team"], args["server_name"], args["event_id"],
                                                        args["target"], args["verb"], args["ref"]),
    "sent_line": lambda args, _ctx: cli.sent_line(args["team"], args["event_id"]),
    "dropped_line": lambda args, _ctx: cli.dropped_line(args["counts"]),
    "history_line": _history_line,
    "question_line": _question_line,
    "reply_to_line": _reply_to_line,
}

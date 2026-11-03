"""Corpus adapters for the envelope and unicode suites (SPEC-proto.md P73-P84); see
test_protocol_corpus.py.

An entry is given as its §15 line and the caller's arguments (`entry_from_line`), so every
suite builds entries the way pingbus does. An entry is returned with its `sent` rendering in
place of the timestamp, which both sides spell the same whatever its size.
"""

from __future__ import annotations

import unicodedata

from helpers.pingbus import envelope as e
from helpers.pingbus import protocol as p


def _entry(s: dict) -> e.Entry:
    return e.entry_from_line(s["line"], handle=s["handle"], ts_ms=s["ts_ms"], reply=s["reply"],
                             question_headline=s["headline"], revises=s["revises"])


def _entry_json(entry: e.Entry) -> object:
    return {"kind": entry.kind, "team": entry.team, "event_id": entry.event_id, "sender": entry.sender,
            "handle": entry.handle, "sent": e._sent(entry.ts_ms), "text": entry.text, "copy": list(entry.copy),
            "verb": entry.verb, "ref": entry.ref, "re": entry.re, "trust": entry.trust, "branch": entry.branch,
            "reply": entry.reply, "room": entry.room,
            "question": None if entry.question is None else list(entry.question), "revises": entry.revises}


def _held(args: dict) -> dict[tuple[str, str], object] | None:
    held = args["held_back"]
    return None if held is None else {(team, sender): count for team, sender, count in held}


def _delivery(args: dict, _ctx: p.Context | None) -> object:
    return e.render([_entry(s) for s in args["entries"]], args["nonce"], held_back=_held(args), more=args["more"])


def _fit(args: dict, _ctx: p.Context | None) -> object:
    return e.fit([_entry(s) for s in args["entries"]], held_back=_held(args), more=args["more"])


def _cap_body(args: dict, _ctx: p.Context | None) -> object:
    return list(e.cap_body(args["text"]))


def _sender_budget(args: dict, _ctx: p.Context | None) -> object:
    now = [0.0]
    budget = e.SenderBudget(lambda: now[0])
    out: list[object] = []
    for s in args["steps"]:
        now[0] = s["t_ms"] / 1000
        if s["op"] == "allowed":
            out.append(budget.allowed(s["team"], s["sender"], s["kind"], chosen=s["chosen"]))
        else:
            budget.spend(s["team"], s["sender"], s["kind"])
            out.append(None)
    return out


def _nfkc_structural(args: dict, _ctx: p.Context | None) -> object:
    """True when the code points the NFKC rule of `_shown` escapes, among those its category
    and block rules would show, are exactly the corpus's list (P74, P84)."""
    found = []
    for point in range(0x7F, 0x110000):
        if 0xD800 <= point <= 0xDFFF or point in e._ESCAPED or point in e._TEXT_PRESENTATION:
            continue
        char = chr(point)
        category = unicodedata.category(char)
        if category in e._MARK_CATEGORIES:
            continue
        if category in e._PS_CATEGORIES:
            if not e._ps_allowed(point):
                continue
        elif category not in e._WORD_CATEGORIES:
            continue
        folded = unicodedata.normalize("NFKC", char)
        if any(c in e._STRUCTURAL or not c.isprintable() for c in folded):
            found.append(point)
    return found == args["points"]


ADAPTERS = {
    "entry_from_line": lambda args, _ctx: _entry_json(_entry(args["entry"])),
    "render_body": lambda args, _ctx: e._render(args["text"]),
    "cap_body": _cap_body,
    "wire_bytes": lambda args, _ctx: e.wire_bytes(args["text"]),
    "entry_lines": lambda args, _ctx: e._entry_lines(_entry(args["entry"])),
    "render_delivery": _delivery,
    "render_membership": lambda args, _ctx: e.render_membership(args["lines"], args["nonce"]),
    "fit": _fit,
    "sender_budget": _sender_budget,
    "nfkc_structural": _nfkc_structural,
}

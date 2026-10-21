"""Corpus adapters for the values, grammar, build and roles suites (SPEC-proto.md P3-P23).

Each adapter takes a case's `args` and its context and returns the verdict as JSON; see
test_protocol_corpus.py for the corpus format.
"""

from __future__ import annotations

import unicodedata

from helpers.agent_bus import registry
from helpers.pingbus import protocol as p


def _handle(h: p.Handle | None) -> object:
    if h is None:
        return None
    return {"repo": h.repo, "seat": h.seat, "host": h.host, "type": h.type, "n": h.n}


def _ref(r: p.Ref | None) -> object:
    if r is None:
        return None
    return {"text": r.text, "form": r.form, "owner": r.owner, "repo": r.repo, "sha": r.sha,
            "num": r.num, "path": r.path}


def _content_size(args: dict, _ctx: p.Context | None) -> object:
    try:
        return p.content_size(args["content"])
    except UnicodeEncodeError:
        return {"error": True}  # P3: an unencodable content is an error the caller reads


def _ref_trust(args: dict, ctx: p.Context) -> object:
    ref = p.parse_ref(args["ref"])
    if ref is None:
        raise ValueError("not a reference")
    return {"source": p.ref_source(ref, ctx.org, ctx.repos),
            "any_branch": p.trusts_any_branch(ref, ctx.org, ctx.repos),
            "excluded": p.is_excluded(ref, ctx.org, ctx.exclude)}


def _verb_rule(args: dict, _ctx: p.Context | None) -> object:
    rule = p.VERBS.get(args["verb"])
    if rule is None:
        raise ValueError("not a verb")
    return {"ref": rule.ref, "forms": sorted(rule.ref_forms), "re": rule.re, "ack": rule.ack_expected,
            "senders": sorted(rule.senders), "external": rule.external,
            "to_humans": args["verb"] in p.VERBS_TO_HUMANS}


def _unicode15_table(args: dict, _ctx: p.Context | None) -> object:
    """True when every code point of the ranges is unassigned in this Python's tables: the
    Unicode 15.0 additions the Go escapes are exactly what Unicode 14.0 lacks (P84)."""
    return all(unicodedata.category(chr(cp)) == "Cn" for a, b in args["ranges"] for cp in range(a, b + 1))


def _check(fn: object) -> object:
    def adapter(args: dict, ctx: p.Context) -> object:
        fn(args["verb"], args["sender"], ctx) if "verb" in args else fn(args["sender"], ctx)
        return None
    return adapter


def _value(fn: object) -> object:
    return lambda args, _ctx: fn(args["value"])


ADAPTERS = {
    # values (P3)
    "content_size": _content_size,
    "unicode15_table": _unicode15_table,
    # identifiers (P4-P16)
    "is_team_name": _value(p.is_team_name),
    "parse_handle": lambda args, _ctx: _handle(p.parse_handle(args["value"])),
    "is_seat_name": _value(p.is_seat_name),
    "is_human_localpart": _value(p.is_human_localpart),
    "parse_user_id": lambda args, _ctx: p.parse_user_id(args["value"], args["server_name"]),
    "is_agent_user_id": lambda args, _ctx: p.is_agent_user_id(args["value"], args["server_name"]),
    "is_human_user_id": lambda args, _ctx: p.is_human_user_id(args["value"], args["server_name"]),
    "is_room_id": _value(p.is_room_id),
    "is_event_id": _value(p.is_event_id),
    "is_branch_name": _value(p.is_branch_name),
    "is_owner": _value(p.is_owner),
    "is_repo_name": _value(p.is_repo_name),
    "is_owner_repo": _value(p.is_owner_repo),
    "parse_ref": lambda args, _ctx: _ref(p.parse_ref(args["value"])),
    "lowercase_ref_repo": _value(p.lowercase_ref_repo),
    "is_path_prefix": _value(p.is_path_prefix),
    "path_allowed": lambda args, _ctx: p.path_allowed(args["path"], args["prefixes"]),
    "is_description": _value(p.is_description),
    "ref_trust": _ref_trust,
    # building a handle (P17)
    "normalise_repo": lambda args, _ctx: registry.normalise_repo(args["raw"]),
    "repo_from_remote": lambda args, _ctx: registry.repo_from_remote(args["remote"], args["dir"]),
    "resolve_host": lambda args, _ctx: registry.resolve_host(args["explicit"], args["role"]),
    "build_handle": lambda args, _ctx: registry.build_handle(args["repo"], args["seat"], args["host"],
                                                             args["type"]),
    "prefix_of": lambda args, _ctx: registry.prefix_of(args["handle"]),
    # roles, verbs, addressing (P18-P23)
    "normalise_role": _value(p.normalise_role),
    "verb_rule": _verb_rule,
    "coordinators": lambda _args, ctx: p.coordinators(ctx.roles),
    "with_coordinator_copy": lambda args, ctx: p.with_coordinator_copy(args["to"], args["sender"], ctx),
    "copy_of": lambda args, ctx: list(p._copy_of(args["to"], args["self"], ctx)),
    "sender_class": lambda args, ctx: p.sender_class(args["user"], ctx),
    "addressable": lambda args, ctx: p.addressable(args["user"], args["verb"], ctx),
    "check_role": _check(p.check_role),
    "check_text_sender": _check(p.check_text_sender),
}

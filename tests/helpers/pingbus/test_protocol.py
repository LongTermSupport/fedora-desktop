"""Tests for helpers.pingbus.protocol, the pure validator of the agent team bus.

The validator is the one function every sender and every receiver runs, so these tests
are tables: every verb against every reference form, every grammar edge, the ping's
envelope (canonical body, mentions), the human-message rules, the team record, status and
power levels, and every drop reason code. A rule the table does not exercise is a rule the
bus does not have.

Spec: docs/agent-bus-protocol.md. Fixture names are placeholders (`server.test`,
`example-org/myrepo`).
"""

from __future__ import annotations

import copy
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import protocol as p

SN = "server.test"
TEAM = "team-a"
ORCH = "@myrepo.1+host.podman:server.test"
WORKER = "@myrepo.2+host.podman:server.test"
WORKER2 = "@other.1+host.podman:server.test"
NO_ROLE = "@myrepo.3+host.podman:server.test"
HUMAN = "@human1:server.test"
HUMAN2 = "@human2:server.test"
ADMIN = "@admin:server.test"
CONDUIT = "@conduit:server.test"
FOREIGN = "@myrepo.4+host.podman:elsewhere.test"
FOREIGN_HUMAN = "@human1:elsewhere.test"

SHA = "0123456789abcdef0123456789abcdef01234567"
EVENT_ID = "$" + "A" * 43
EVENT_ID_2 = "$" + "B" * 43
ROOM_ID = "!" + "r" * 43

REFS = {
    "path": f"path:example-org/myrepo@{SHA}:CLAUDE/Plan/00001-x/PLAN.md",
    "commit": f"commit:example-org/myrepo@{SHA}",
    "pr": f"pr:example-org/myrepo#12@{SHA}",
    "issue": "issue:example-org/myrepo#7",
}

ROLES = {ORCH: p.ROLE_ORCHESTRATOR, WORKER: p.ROLE_WORKER, WORKER2: p.ROLE_WORKER}


def make_ctx(**over: object) -> p.Context:
    fields: dict = {
        "server_name": SN,
        "humans": frozenset({HUMAN, HUMAN2}),
        "roles": dict(ROLES),
        "repos": {"example-org/myrepo": ("main",)},
        "path_prefixes": ("CLAUDE/Plan/", "docs/spec.md"),
    }
    fields.update(over)
    return p.Context(**fields)


def ping_obj(verb: str, **extra: object) -> dict:
    body: dict = {"v": 1, "verb": verb, "to": [WORKER]}
    body.update(extra)
    return body


def wrap(obj: object) -> dict:
    """A ping object in a correct §4 envelope, built from the object as given (a
    malformed object gets a best-effort body and mentions so that the object, not the
    envelope, is what fails)."""
    to = obj.get("to") if isinstance(obj, dict) else None
    users = sorted(to) if isinstance(to, list) and all(isinstance(u, str) for u in to) else []
    try:
        body = p.render(obj)
    except (TypeError, KeyError, AttributeError):
        body = "[agent-bus] x"
    return {
        "msgtype": "m.notice",
        "body": body,
        "m.mentions": {"user_ids": users},
        p.PING_KEY: obj,
    }


def content(verb: str, **extra: object) -> dict:
    return wrap(ping_obj(verb, **extra))


def event(sender: str, body: object, **extra: object) -> dict:
    ev: dict = {
        "event_id": EVENT_ID,
        "type": "m.room.message",
        "sender": sender,
        "content": body,
        "origin_server_ts": 1_700_000_000_000,
    }
    ev.update(extra)
    return ev


def human_content(text: str = "please look", mention: object = (WORKER,), **extra: object) -> dict:
    body: dict = {"msgtype": "m.text", "body": text}
    if mention is not None:
        body["m.mentions"] = {"user_ids": list(mention)}
    body.update(extra)
    return body


class RefusalAssertions(unittest.TestCase):
    def assertRefused(self, reason: str, fn, *args, **kwargs) -> None:
        with self.assertRaises(p.Refusal) as caught:
            fn(*args, **kwargs)
        self.assertEqual(caught.exception.reason, reason)

    def assertUntrusted(self, fn, *args, **kwargs) -> None:
        with self.assertRaises(p.Untrusted):
            fn(*args, **kwargs)


class TestConstants(unittest.TestCase):
    def test_prefix_is_one_constant_every_name_derives_from(self) -> None:
        self.assertEqual(p.PREFIX, "agent_bus")
        self.assertEqual(p.EVENT_TEAM, "agent_bus.team")
        self.assertEqual(p.EVENT_STATUS, "agent_bus.status")
        self.assertEqual(p.PING_KEY, "agent_bus.ping")
        for name in (p.EVENT_TEAM, p.EVENT_STATUS, p.PING_KEY):
            self.assertTrue(name.startswith(p.PREFIX + "."))

    def test_prefix_is_a_matrix_namespaced_identifier_and_not_a_domain(self) -> None:
        import re
        for name in (p.EVENT_TEAM, p.EVENT_STATUS, p.PING_KEY):
            self.assertRegex(name, r"\A[a-z][a-z0-9._-]{0,254}\Z")
            self.assertFalse(name.startswith("m."))
        self.assertIn("_", p.PREFIX)
        self.assertIsNone(re.fullmatch(r"[a-z0-9-]+", p.PREFIX))

    def test_version_is_one(self) -> None:
        self.assertEqual(p.PROTOCOL_VERSION, 1)

    def test_drop_reasons_are_the_closed_set(self) -> None:
        self.assertEqual(
            p.DROP_REASONS,
            (
                "version", "schema", "size", "edit", "sender", "role", "target", "verb",
                "ref", "allowlist", "re", "body", "text", "stale", "rate", "unresolved",
                "provenance",
            ),
        )

    def test_send_only_refusals(self) -> None:
        self.assertEqual(p.SEND_REFUSALS, ("secret",))
        self.assertFalse(set(p.SEND_REFUSALS) & set(p.DROP_REASONS))
        self.assertEqual(p.Refusal("secret").reason, "secret")

    def test_refusal_rejects_a_code_outside_the_set(self) -> None:
        for bad in ("made-up", "note", "behalf"):
            with self.subTest(code=bad), self.assertRaises(ValueError):
                p.Refusal(bad)

    def test_no_warden(self) -> None:
        self.assertEqual(p.RESERVED_LOCALPARTS, ("admin", "conduit"))
        self.assertEqual(p.ROLES, ("orchestrator", "worker"))
        self.assertFalse(hasattr(p, "SENDER_WARDEN"))


class TestHandleSeparator(unittest.TestCase):
    def test_separator_is_one_constant_in_the_handle_pattern(self) -> None:
        self.assertEqual(p.HANDLE_SEP, "+")
        self.assertIn("\\" + p.HANDLE_SEP, p.HANDLE_PATTERN)
        self.assertEqual(p.HANDLE_PATTERN.count("\\+"), 1)

    def test_pattern_follows_the_constant(self) -> None:
        self.assertEqual(p.handle_pattern("="), p.HANDLE_PATTERN.replace("\\+", "="))
        self.assertEqual(p.handle_pattern(p.HANDLE_SEP), p.HANDLE_PATTERN)

    def test_format_handle(self) -> None:
        self.assertEqual(p.format_handle("myrepo", 3, "workstation", "lxc"), "myrepo.3+workstation.lxc")
        h = p.parse_handle(p.format_handle("a_b", 999999, "h-1", "vm"))
        self.assertEqual((h.repo, h.n, h.host, h.type), ("a_b", 999999, "h-1", "vm"))


class TestVerbByRefForm(RefusalAssertions):
    """Every verb against every reference form and against no reference at all."""

    ALLOWED = {
        "fetch": {"path", "commit"},
        "sync": {"path", "commit"},
        "review": {"path", "commit", "pr"},
        "run-qa": {"commit", "pr"},
        "halt": set(),
        "ack": set(),
        "nack": {"path", "commit", "pr", "issue"},
        "done": {"path", "commit", "pr", "issue"},
        "blocked": {"path", "commit", "pr", "issue"},
    }
    REF_OPTIONAL = {"nack"}
    RE_REQUIRED = {"ack", "nack"}

    def sender_for(self, verb: str) -> str:
        return WORKER if verb in {"ack", "nack", "review", "run-qa", "done", "blocked"} else ORCH

    def test_matrix(self) -> None:
        self.assertEqual(set(self.ALLOWED), set(p.VERBS))
        ctx = make_ctx()
        for verb, allowed in self.ALLOWED.items():
            sender = self.sender_for(verb)
            target = ORCH if sender == WORKER else WORKER
            extra: dict = {"to": [target]}
            if verb in self.RE_REQUIRED:
                extra["re"] = EVENT_ID_2
            for form, ref in list(REFS.items()) + [(None, None)]:
                obj = ping_obj(verb, **extra)
                if ref is not None:
                    obj["ref"] = ref
                body = wrap(obj)
                with self.subTest(verb=verb, form=form):
                    accept = form in allowed or (
                        form is None and (not allowed or verb in self.REF_OPTIONAL)
                    )
                    if accept:
                        ping = p.validate_content(body, ctx)
                        p.check_role(verb, sender, ctx)
                        self.assertEqual(ping.verb, verb)
                        self.assertEqual(ping.ref.form if ping.ref else None, form)
                    else:
                        self.assertRefused("ref", p.validate_content, body, ctx)

    def test_re_rules(self) -> None:
        ctx = make_ctx()
        cases = [
            ("fetch", REFS["path"], True, "re"),
            ("halt", None, True, "re"),
            ("ack", None, False, "re"),
            ("nack", None, False, "re"),
            ("done", REFS["commit"], False, None),
            ("done", REFS["commit"], True, None),
            ("blocked", REFS["issue"], True, None),
        ]
        for verb, ref, with_re, reason in cases:
            obj = ping_obj(verb, to=[ORCH])
            if ref:
                obj["ref"] = ref
            if with_re:
                obj["re"] = EVENT_ID_2
            with self.subTest(verb=verb, with_re=with_re):
                if reason:
                    self.assertRefused(reason, p.validate_content, wrap(obj), ctx)
                else:
                    self.assertEqual(p.validate_content(wrap(obj), ctx).re, obj.get("re"))

    def test_re_must_be_an_event_id(self) -> None:
        for bad in ("", "$short", "!" + "A" * 43, "$" + "A" * 42 + "+"):
            with self.subTest(re=bad):
                self.assertRefused(
                    "re", p.validate_content, content("ack", to=[ORCH], re=bad), make_ctx()
                )

    def test_unknown_verb(self) -> None:
        for bad in ("deploy", "FETCH", "", "run_qa"):
            with self.subTest(verb=bad):
                self.assertRefused("verb", p.validate_content, content(bad), make_ctx())

    def test_ack_expected_column(self) -> None:
        expected = {"fetch", "sync", "review", "run-qa", "halt"}
        self.assertEqual({v for v, r in p.VERBS.items() if r.ack_expected}, expected)


class TestRoles(RefusalAssertions):
    SENDERS = {
        "fetch": {"orchestrator"},
        "sync": {"orchestrator"},
        "review": {"orchestrator", "worker"},
        "run-qa": {"orchestrator", "worker"},
        "halt": {"orchestrator"},
        "ack": {"orchestrator", "worker"},
        "nack": {"orchestrator", "worker"},
        "done": {"orchestrator", "worker"},
        "blocked": {"orchestrator", "worker"},
    }

    def test_sender_class_by_verb(self) -> None:
        ctx = make_ctx()
        who = {"orchestrator": ORCH, "worker": WORKER}
        for verb, permitted in self.SENDERS.items():
            for cls, user in who.items():
                with self.subTest(verb=verb, sender=cls):
                    if cls in permitted:
                        p.check_role(verb, user, ctx)
                    else:
                        self.assertRefused("role", p.check_role, verb, user, ctx)

    def test_humans_and_non_members_never_send_pings(self) -> None:
        for user in (NO_ROLE, HUMAN, ADMIN, CONDUIT, FOREIGN):
            for verb in ("ack", "halt", "done"):
                with self.subTest(user=user, verb=verb):
                    self.assertRefused("role", p.check_role, verb, user, make_ctx())

    def test_unknown_verb_is_verb(self) -> None:
        self.assertRefused("verb", p.check_role, "deploy", ORCH, make_ctx())

    def test_sender_class(self) -> None:
        ctx = make_ctx()
        self.assertEqual(p.sender_class(ORCH, ctx), "orchestrator")
        self.assertEqual(p.sender_class(WORKER, ctx), "worker")
        self.assertEqual(p.sender_class(HUMAN, ctx), p.SENDER_HUMAN)
        for user in (NO_ROLE, ADMIN, CONDUIT, FOREIGN, FOREIGN_HUMAN, None, 5):
            with self.subTest(user=user):
                self.assertIsNone(p.sender_class(user, ctx))


class TestTargets(RefusalAssertions):
    def check(self, verb: str, to: object, reason: str | None, **extra: object) -> None:
        body = content(verb, to=to, **extra)
        with self.subTest(verb=verb, to=to, extra=extra):
            if reason:
                self.assertRefused(reason, p.validate_content, body, make_ctx())
            else:
                self.assertEqual(p.validate_content(body, make_ctx()).to, tuple(sorted(to)))

    def test_agent_targets(self) -> None:
        ok_ref = {"ref": REFS["path"]}
        self.check("fetch", [WORKER], None, **ok_ref)
        self.check("fetch", [WORKER2, WORKER], None, **ok_ref)
        self.check("fetch", [], "target", **ok_ref)
        self.check("fetch", [WORKER, WORKER], "target", **ok_ref)
        self.check("fetch", [NO_ROLE], "target", **ok_ref)
        self.check("fetch", [ADMIN], "target", **ok_ref)
        self.check("fetch", [CONDUIT], "target", **ok_ref)
        self.check("fetch", [FOREIGN], "target", **ok_ref)
        self.check("fetch", ["myrepo.2+host.podman"], "target", **ok_ref)

    def test_a_human_is_a_target_only_of_the_answering_verbs(self) -> None:
        for verb, ref in (("ack", None), ("nack", None), ("done", REFS["pr"]), ("blocked", REFS["issue"])):
            extra: dict = {"re": EVENT_ID_2} if verb in ("ack", "nack") else {}
            if ref:
                extra["ref"] = ref
            self.check(verb, [HUMAN], None, **extra)
            self.check(verb, [HUMAN, ORCH], None, **extra)
        self.check("done", [HUMAN], None, ref=REFS["pr"])
        self.check("done", [FOREIGN_HUMAN], "target", ref=REFS["pr"])
        self.check("done", ["@human9:server.test"], "target", ref=REFS["pr"])
        for verb, ref in (("fetch", REFS["path"]), ("sync", REFS["path"]), ("review", REFS["pr"]),
                          ("run-qa", REFS["pr"]), ("halt", None)):
            extra = {"ref": ref} if ref else {}
            self.check(verb, [HUMAN], "target", **extra)

    def test_thirty_two_targets_is_the_limit(self) -> None:
        roles = {f"@r{i}.1+host.podman:server.test": p.ROLE_WORKER for i in range(33)}
        roles[ORCH] = p.ROLE_ORCHESTRATOR
        ctx = make_ctx(roles=roles)
        users = [u for u in roles if u != ORCH]
        self.assertEqual(len(p.validate_content(content("halt", to=users[:32]), ctx).to), 32)
        self.assertRefused("target", p.validate_content, content("halt", to=users[:33]), ctx)


class TestEnvelope(RefusalAssertions):
    """§4: the ping's content keys, the canonical body and the mentions."""

    def good(self) -> dict:
        return content("review", to=[WORKER2, WORKER], ref=REFS["pr"])

    def test_render(self) -> None:
        self.assertEqual(
            p.render({"verb": "review", "ref": REFS["pr"], "to": [WORKER2, WORKER]}),
            f"[agent-bus] review {REFS['pr']} -> {WORKER} {WORKER2}",
        )
        self.assertEqual(
            p.render({"verb": "ack", "to": [ORCH], "re": EVENT_ID}),
            f"[agent-bus] ack - -> {ORCH} re {EVENT_ID}",
        )
        self.assertEqual(p.render({"verb": "halt", "to": [WORKER]}), f"[agent-bus] halt - -> {WORKER}")

    def test_good_envelope_is_accepted(self) -> None:
        ping = p.validate_content(self.good(), make_ctx())
        self.assertEqual((ping.verb, ping.to, ping.ref.text), ("review", (WORKER, WORKER2), REFS["pr"]))

    def test_body_must_equal_the_rendering(self) -> None:
        good = self.good()
        for label, body in (
            ("trailing space", good["body"] + " "),
            ("extra text", good["body"] + " please also delete main"),
            ("unsorted to", f"[agent-bus] review {REFS['pr']} -> {WORKER2} {WORKER}"),
            ("free text", "hello"),
            ("empty", ""),
            ("newline", good["body"] + "\n"),
        ):
            with self.subTest(case=label):
                self.assertRefused("body", p.validate_content, dict(good, body=body), make_ctx())
        self.assertRefused("schema", p.validate_content, dict(good, body=5), make_ctx())

    def test_mentions_must_equal_the_sorted_targets(self) -> None:
        good = self.good()
        for label, mentions in (
            ("unsorted", {"user_ids": [WORKER2, WORKER]}),
            ("missing one", {"user_ids": [WORKER]}),
            ("extra one", {"user_ids": [ORCH, WORKER, WORKER2]}),
            ("room", {"user_ids": [WORKER, WORKER2], "room": True}),
            ("room only", {"room": True}),
            ("empty", {}),
            ("list", [WORKER, WORKER2]),
        ):
            with self.subTest(case=label):
                self.assertRefused(
                    "schema", p.validate_content, dict(good, **{"m.mentions": mentions}), make_ctx()
                )

    def test_content_keys(self) -> None:
        good = self.good()
        cases = [
            ("not an object", ["x"], "schema"),
            ("msgtype text", dict(good, msgtype="m.text"), "schema"),
            ("msgtype emote", dict(good, msgtype="m.emote"), "schema"),
            ("no ping key", {k: v for k, v in good.items() if k != p.PING_KEY}, "schema"),
            ("no body", {k: v for k, v in good.items() if k != "body"}, "schema"),
            ("no mentions", {k: v for k, v in good.items() if k != "m.mentions"}, "schema"),
            ("no msgtype", {k: v for k, v in good.items() if k != "msgtype"}, "schema"),
            ("format", dict(good, format="org.matrix.custom.html"), "schema"),
            ("formatted_body", dict(good, formatted_body="<b>x</b>"), "schema"),
            ("other key", dict(good, extra=1), "schema"),
            ("relates_to", dict(good, **{"m.relates_to": {"rel_type": "m.replace"}}), "edit"),
            ("reply", dict(good, **{"m.relates_to": {"m.in_reply_to": {"event_id": EVENT_ID}}}), "edit"),
            ("new_content", dict(good, **{"m.new_content": {}}), "edit"),
        ]
        for label, body, reason in cases:
            with self.subTest(case=label):
                self.assertRefused(reason, p.validate_content, body, make_ctx())

    def test_size_limit(self) -> None:
        roles = {f"@{'r' * 40}{i}.1+{'h' * 40}.podman:server.test": p.ROLE_WORKER for i in range(32)}
        ctx = make_ctx(roles=dict(roles, **{ORCH: p.ROLE_ORCHESTRATOR}))
        body = content("halt", to=sorted(roles))
        self.assertGreater(p.content_size(body), p.MAX_CONTENT_BYTES)
        self.assertRefused("size", p.validate_content, body, ctx)
        self.assertEqual(p.MAX_CONTENT_BYTES, 4096)

    def test_content_size_is_compact_utf8(self) -> None:
        self.assertEqual(p.content_size({"a": "é"}), len('{"a":"é"}'.encode("utf-8")))


class TestPingObjectSchema(RefusalAssertions):
    def test_schema_table(self) -> None:
        good = ping_obj("fetch", ref=REFS["path"])
        cases = [
            ("string", "ping", "schema"),
            ("list", ["v", 1], "schema"),
            ("unknown key", dict(good, extra="x"), "schema"),
            ("note key", dict(good, note="x"), "schema"),
            ("on_behalf_of key", dict(good, on_behalf_of=HUMAN), "schema"),
            ("missing v", {k: v for k, v in good.items() if k != "v"}, "schema"),
            ("missing verb", {k: v for k, v in good.items() if k != "verb"}, "schema"),
            ("missing to", {k: v for k, v in good.items() if k != "to"}, "schema"),
            ("v bool", dict(good, v=True), "schema"),
            ("v float", dict(good, v=1.0), "schema"),
            ("v string", dict(good, v="1"), "schema"),
            ("v null", dict(good, v=None), "schema"),
            ("v two", dict(good, v=2), "version"),
            ("v zero", dict(good, v=0), "version"),
            ("verb int", dict(good, verb=3), "schema"),
            ("to string", dict(good, to=WORKER), "schema"),
            ("to nested", dict(good, to=[[WORKER]]), "schema"),
            ("to object", dict(good, to={"a": WORKER}), "schema"),
            ("ref null", dict(good, ref=None), "schema"),
            ("ref object", dict(good, ref={"x": 1}), "schema"),
            ("ref bool", dict(good, ref=False), "schema"),
            ("re list", dict(good, re=[EVENT_ID]), "schema"),
        ]
        for label, obj, reason in cases:
            with self.subTest(case=label):
                self.assertRefused(reason, p.validate_content, wrap(obj), make_ctx())

    def test_another_version_is_version_before_any_other_check(self) -> None:
        """§1/§4: a later version's new keys or envelope are never reported as `schema`."""
        v2 = ping_obj("fetch", ref=REFS["path"], v=2)
        cases = [
            ("new ping key", wrap(dict(v2, extra="x"))),
            ("required key gone", wrap({k: v for k, v in v2.items() if k != "to"})),
            ("new verb", wrap(dict(v2, verb="deploy"))),
            ("new value type", wrap(dict(v2, to=WORKER))),
            ("new envelope key", dict(wrap(v2), extra=1)),
            ("envelope key gone", {k: v for k, v in wrap(v2).items() if k != "body"}),
            ("other msgtype", dict(wrap(v2), msgtype="m.text")),
            ("other body", dict(wrap(v2), body="x")),
            ("large", dict(wrap(v2), body="x" * p.MAX_CONTENT_BYTES)),
        ]
        for label, body in cases:
            with self.subTest(case=label):
                self.assertRefused("version", p.validate_content, body, make_ctx())
                out = p.validate_event(event(ORCH, body), make_ctx(), WORKER)
                self.assertEqual((out.kind, out.reason), (p.DROP, "version"))
        self.assertRefused("schema", p.validate_content, wrap(dict(v2, v=True, extra="x")), make_ctx())


class TestRefGrammar(unittest.TestCase):
    OWNER39 = "a" + "b" * 37 + "c"

    def ref(self, owner: str = "example-org", repo: str = "myrepo", sha: str = SHA, path: str = "a/b.md") -> str:
        return f"path:{owner}/{repo}@{sha}:{path}"

    def test_edges(self) -> None:
        cases = [
            (self.ref(), True),
            (self.ref(owner="a"), True),
            (self.ref(owner=self.OWNER39), True),
            (self.ref(owner=self.OWNER39 + "d"), False),
            (self.ref(owner="-ab"), False),
            (self.ref(owner="ab-"), False),
            (self.ref(owner="a_b"), False),
            (self.ref(owner="Example-org"), False),
            (self.ref(owner="a.b"), False),
            (self.ref(repo="my.repo_x-y"), True),
            (self.ref(repo="r" * 100), True),
            (self.ref(repo="r" * 101), False),
            (self.ref(repo="."), False),
            (self.ref(repo=".."), False),
            (self.ref(repo="myrepo.git"), False),
            (self.ref(repo="MyRepo"), False),
            (self.ref(repo=""), False),
            (self.ref(sha=SHA[:39]), False),
            (self.ref(sha=SHA + "0"), False),
            (self.ref(sha=SHA.upper()), False),
            (self.ref(sha="g" * 40), False),
            (self.ref(sha="main"), False),
            (self.ref(path="/".join(["s"] * 8)), True),
            (self.ref(path="/".join(["s"] * 9)), False),
            (self.ref(path="x" * 64), True),
            (self.ref(path="x" * 65), False),
            (self.ref(path="/a"), False),
            (self.ref(path="a/"), False),
            (self.ref(path="a//b"), False),
            (self.ref(path="a/./b"), False),
            (self.ref(path="a/../b"), False),
            (self.ref(path="."), False),
            (self.ref(path=".hidden/x"), True),
            (self.ref(path="a b"), False),
            (self.ref(path="a+b"), False),
            (self.ref(path=""), False),
            (" " + self.ref(), False),
            (self.ref() + " ", False),
            (self.ref() + "\n", False),
            (f"commit:example-org/myrepo@{SHA}", True),
            (f"commit:example-org/myrepo@{SHA}:a", False),
            (f"pr:example-org/myrepo#1@{SHA}", True),
            (f"pr:example-org/myrepo#9999999999@{SHA}", True),
            (f"pr:example-org/myrepo#99999999999@{SHA}", False),
            (f"pr:example-org/myrepo#0@{SHA}", False),
            (f"pr:example-org/myrepo#01@{SHA}", False),
            ("pr:example-org/myrepo#12", False),
            ("issue:example-org/myrepo#7", True),
            (f"issue:example-org/myrepo#7@{SHA}", False),
            ("issue:example-org/myrepo", False),
            (f"tree:example-org/myrepo@{SHA}", False),
            ("https://github.com/example-org/myrepo/pull/12", False),
            (f"commit:example-org@{SHA}", False),
            (f"commit:example-org/myrepo/x@{SHA}", False),
            (f"commit:example-org/myrepo@{SHA[:7]}", False),
            ("", False),
            (None, False),
        ]
        for ref, valid in cases:
            with self.subTest(ref=ref):
                self.assertEqual(p.parse_ref(ref) is not None, valid)

    def test_fields(self) -> None:
        r = p.parse_ref(REFS["path"])
        self.assertEqual(
            (r.form, r.owner, r.repo, r.sha, r.num, r.path, r.repo_full),
            ("path", "example-org", "myrepo", SHA, None, "CLAUDE/Plan/00001-x/PLAN.md", "example-org/myrepo"),
        )
        r = p.parse_ref(REFS["pr"])
        self.assertEqual((r.form, r.num, r.sha, r.path), ("pr", 12, SHA, None))
        r = p.parse_ref(REFS["issue"])
        self.assertEqual((r.form, r.num, r.sha), ("issue", 7, None))
        self.assertEqual(p.parse_ref(REFS["commit"]).text, REFS["commit"])

    def test_length_limit_applies_to_a_grammatical_ref(self) -> None:
        head = f"path:{self.OWNER39}/{'r' * 100}@{SHA}:"
        segs = ["x" * 64] * 7
        room = p.MAX_REF_LEN - len(head) - len("/".join(segs)) - 1
        at_limit = head + "/".join(segs + ["y" * room])
        self.assertEqual(len(at_limit), 700)
        self.assertIsNotNone(p.parse_ref(at_limit))
        over = head + "/".join(segs + ["y" * (room + 1)])
        self.assertIsNone(p.parse_ref(over))

    def test_lowercase_ref_repo_touches_only_owner_and_repo(self) -> None:
        cases = [
            (f"path:Example-Org/MyRepo@{SHA}:Docs/X.md", f"path:example-org/myrepo@{SHA}:Docs/X.md"),
            (f"pr:Example-Org/MyRepo#3@{SHA}", f"pr:example-org/myrepo#3@{SHA}"),
            ("issue:Example-Org/MyRepo#3", "issue:example-org/myrepo#3"),
            (f"commit:Example-Org/MyRepo@{SHA.upper()}", f"commit:example-org/myrepo@{SHA.upper()}"),
            ("garbage", "garbage"),
            ("Path:X/Y@z", "Path:X/Y@z"),
        ]
        for given, want in cases:
            with self.subTest(ref=given):
                self.assertEqual(p.lowercase_ref_repo(given), want)


class TestAllowlist(RefusalAssertions):
    def test_allowlist(self) -> None:
        ctx = make_ctx()
        cases = [
            (f"path:example-org/myrepo@{SHA}:CLAUDE/Plan/x.md", None),
            (f"path:example-org/myrepo@{SHA}:docs/spec.md", None),
            (f"path:example-org/myrepo@{SHA}:docs/spec.md.x", "allowlist"),
            (f"path:example-org/myrepo@{SHA}:docs/other.md", "allowlist"),
            (f"path:example-org/myrepo@{SHA}:CLAUDE/Planx/a.md", "allowlist"),
            (f"path:example-org/myrepo@{SHA}:CLAUDE/Plan", "allowlist"),
            (f"path:example-org/other@{SHA}:CLAUDE/Plan/x.md", "allowlist"),
            (f"commit:example-org/other@{SHA}", "allowlist"),
            (f"commit:fork-owner/myrepo@{SHA}", "allowlist"),
            (f"commit:example-org/myrepo@{SHA}", None),
        ]
        for ref, reason in cases:
            body = content("fetch", ref=ref)
            with self.subTest(ref=ref):
                if reason:
                    self.assertRefused(reason, p.validate_content, body, ctx)
                else:
                    p.validate_content(body, ctx)

    def test_path_allowed_and_prefix_grammar(self) -> None:
        self.assertTrue(p.path_allowed("a/b/c", ("a/",)))
        self.assertFalse(p.path_allowed("ab/c", ("a/",)))
        self.assertTrue(p.path_allowed("a/f.md", ("a/f.md",)))
        self.assertFalse(p.path_allowed("a/f.mdx", ("a/f.md",)))
        self.assertFalse(p.path_allowed("a/f.md", ()))
        for prefix, valid in (("a/", True), ("a/b/", True), ("a/f.md", True), ("/a/", False),
                              ("a//", False), ("../a/", False), ("", False), ("/", False), (5, False)):
            with self.subTest(prefix=prefix):
                self.assertEqual(p.is_path_prefix(prefix), valid)

    def test_branch_names(self) -> None:
        for name, valid in (("main", True), ("release/1.x", True), ("b" * 100, True),
                            ("b" * 101, False), ("", False), ("a b", False), ("a~1", False)):
            with self.subTest(branch=name):
                self.assertEqual(p.is_branch_name(name), valid)


class TestIdentifiers(unittest.TestCase):
    def test_handles(self) -> None:
        cases = [
            ("myrepo.1+workstation.podman", True),
            ("myrepo.1+workstation.lxc", True),
            ("myrepo.1+workstation.docker", True),
            ("myrepo.1+workstation.vm", True),
            ("myrepo.1+workstation.host", True),
            ("myrepo.1+workstation.kvm", False),
            ("r" * 48 + ".1+h.podman", True),
            ("r" * 49 + ".1+h.podman", False),
            ("a_b-c.1+h.podman", True),
            ("_ab.1+h.podman", False),
            ("-ab.1+h.podman", False),
            ("my.repo.1+h.podman", False),
            ("MyRepo.1+h.podman", False),
            ("myrepo.0+h.podman", False),
            ("myrepo.01+h.podman", False),
            ("myrepo.999999+h.podman", True),
            ("myrepo.1000000+h.podman", False),
            ("myrepo.1+" + "h" * 63 + ".podman", True),
            ("myrepo.1+" + "h" * 64 + ".podman", False),
            ("myrepo.1+h-.podman", False),
            ("myrepo.1+-h.podman", False),
            ("myrepo.1+a-b.podman", True),
            ("myrepo.1+H.podman", False),
            ("myrepo.1.h.podman", False),
            ("myrepo.1=h.podman", False),
            ("myrepo.1+h.podman ", False),
            ("admin", False),
            (None, False),
        ]
        for handle, valid in cases:
            with self.subTest(handle=handle):
                self.assertEqual(p.parse_handle(handle) is not None, valid)
        h = p.parse_handle("myrepo.12+workstation.podman")
        self.assertEqual((h.repo, h.n, h.host, h.type), ("myrepo", 12, "workstation", "podman"))

    def test_team_names(self) -> None:
        for name, valid in (("a", True), ("team-a", True), ("a" * 24, True), ("a" * 25, False),
                            ("1a", False), ("-a", False), ("a_b", False), ("A", False), ("", False)):
            with self.subTest(name=name):
                self.assertEqual(p.is_team_name(name), valid)

    def test_ids(self) -> None:
        for prefix, check in (("!", p.is_room_id), ("$", p.is_event_id)):
            other = "$" if prefix == "!" else "!"
            cases = [
                (prefix + "a" * 43, True),
                (prefix + "A-_9" * 10 + "abc", True),
                (prefix + "a" * 42, False),
                (prefix + "a" * 44, False),
                (prefix + "a" * 42 + "+", False),
                (prefix + "a" * 42 + "/", False),
                (other + "a" * 43, False),
                ("a" * 44, False),
                (prefix + "a" * 43 + ":server.test", False),
                (None, False),
                (5, False),
            ]
            for value, valid in cases:
                with self.subTest(kind=prefix, value=value):
                    self.assertEqual(check(value), valid)

    def test_human_localparts(self) -> None:
        for name, valid in (("human1", True), ("a", True), ("a" * 32, True), ("a" * 33, False),
                            ("1abc", False), ("a+b", False), ("a.b", False), ("Abc", False),
                            ("a_b-c", True), ("admin", False), ("conduit", False), ("warden", True)):
            with self.subTest(name=name):
                self.assertEqual(p.is_human_localpart(name), valid)

    def test_a_handle_is_never_a_human_localpart(self) -> None:
        self.assertNotIn(p.HANDLE_SEP, "abcdefghijklmnopqrstuvwxyz0123456789_-")
        self.assertFalse(p.is_human_localpart("myrepo.1+h.podman"))

    def test_user_ids(self) -> None:
        self.assertEqual(p.parse_user_id(ORCH, SN), "myrepo.1+host.podman")
        self.assertEqual(p.parse_user_id(ADMIN, SN), "admin")
        self.assertEqual(p.parse_user_id(HUMAN, SN), "human1")
        self.assertIsNone(p.parse_user_id(FOREIGN, SN))
        self.assertIsNone(p.parse_user_id("myrepo.1+host.podman:server.test", SN))
        self.assertIsNone(p.parse_user_id("@:server.test", SN))
        self.assertIsNone(p.parse_user_id("@a b:server.test", SN))
        self.assertIsNone(p.parse_user_id(None, SN))
        self.assertTrue(p.is_agent_user_id(ORCH, SN))
        self.assertFalse(p.is_agent_user_id(HUMAN, SN))
        self.assertTrue(p.is_human_user_id(HUMAN, SN))
        self.assertFalse(p.is_human_user_id(ADMIN, SN))
        self.assertFalse(p.is_human_user_id(ORCH, SN))
        self.assertFalse(p.is_human_user_id(FOREIGN_HUMAN, SN))


class TestPingEvent(unittest.TestCase):
    def outcome(self, ev: object, me: str = WORKER, **kw: object) -> p.Outcome:
        return p.validate_event(ev, make_ctx(), me, **kw)

    def good(self) -> dict:
        return event(ORCH, content("fetch", ref=REFS["path"]))

    def test_accepts_a_valid_ping_addressed_to_me(self) -> None:
        out = self.outcome(self.good())
        self.assertEqual(out.kind, p.ACCEPT)
        self.assertIsNone(out.human)
        self.assertEqual(out.ping.sender, ORCH)
        self.assertEqual(out.ping.event_id, EVENT_ID)
        self.assertEqual(out.ping.origin_server_ts, 1_700_000_000_000)

    def test_ignored_silently(self) -> None:
        good = self.good()
        self.assertEqual(self.outcome(good, seen={EVENT_ID}).kind, p.IGNORE)
        self.assertEqual(self.outcome(good, me=ORCH).kind, p.IGNORE)
        self.assertEqual(self.outcome(good, me=WORKER2).kind, p.IGNORE)
        other_type = dict(good, type="m.room.member")
        self.assertEqual(self.outcome(other_type).kind, p.IGNORE)

    def test_other_types_are_ignored_before_the_event_id_check(self) -> None:
        for label, ev in (
            ("bad event id", {"type": "m.room.member", "event_id": "bad"}),
            ("no event id", {"type": "m.room.member"}),
            ("state event", {"type": p.EVENT_STATUS, "event_id": 5, "state_key": ""}),
        ):
            with self.subTest(case=label):
                out = self.outcome(ev)
                self.assertEqual((out.kind, out.reason), (p.IGNORE, "type"))
        out = self.outcome({"event_id": EVENT_ID})
        self.assertEqual((out.kind, out.reason), (p.IGNORE, "type"))

    def test_event_level_drops(self) -> None:
        good = self.good()
        body = good["content"]
        cases = [
            ("bad event id", dict(good, event_id="$x"), "schema"),
            ("missing event id", {k: v for k, v in good.items() if k != "event_id"}, "schema"),
            ("not an object", ["x"], "schema"),
            ("state key", dict(good, state_key=""), "schema"),
            ("redacted", dict(good, unsigned={"redacted_because": {}}), "schema"),
            ("no content", {k: v for k, v in good.items() if k != "content"}, "schema"),
            ("content list", dict(good, content=[1]), "schema"),
            ("ts missing", {k: v for k, v in good.items() if k != "origin_server_ts"}, "schema"),
            ("ts string", dict(good, origin_server_ts="1"), "schema"),
            ("ts bool", dict(good, origin_server_ts=True), "schema"),
            ("ts negative", dict(good, origin_server_ts=-1), "schema"),
            ("edit", event(ORCH, dict(body, **{"m.relates_to": {"rel_type": "m.replace"}})), "edit"),
            ("no role", event(NO_ROLE, body), "sender"),
            ("admin", event(ADMIN, body), "sender"),
            ("conduit", event(CONDUIT, body), "sender"),
            ("foreign", event(FOREIGN, body), "sender"),
            ("sender missing", dict(good, sender=None), "sender"),
            ("worker sends fetch", event(WORKER2, body), "role"),
        ]
        for label, ev, reason in cases:
            with self.subTest(case=label):
                out = self.outcome(ev)
                self.assertEqual((out.kind, out.reason), (p.DROP, reason))

    def test_agent_free_text_never_reaches_an_agent(self) -> None:
        cases = [
            ("plain text", {"msgtype": "m.text", "body": "hi"}, "schema"),
            ("room mention", {"msgtype": "m.text", "body": "hi", "m.mentions": {"room": True}}, "schema"),
            ("addressed text", {"msgtype": "m.text", "body": "hi", "m.mentions": {"user_ids": [WORKER]}}, "schema"),
            ("ping-less notice", {"msgtype": "m.notice", "body": "hi"}, "schema"),
            ("body differs", dict(content("halt"), body="[agent-bus] halt - -> " + WORKER + " now"), "body"),
        ]
        for label, body, reason in cases:
            with self.subTest(case=label):
                out = self.outcome(event(ORCH, body))
                self.assertEqual((out.kind, out.reason), (p.DROP, reason))

    def test_every_drop_reason_code(self) -> None:
        """Each code the offline validator owns is produced by an event; the rest are
        named as decided by limits, forge and provenance code, and nothing is unaccounted."""
        ok = ping_obj("fetch", ref=REFS["path"])
        many = {f"@{'r' * 40}{i}.1+{'h' * 40}.podman:server.test": p.ROLE_WORKER for i in range(32)}
        producers = {
            "version": (event(ORCH, wrap(dict(ok, v=2))), make_ctx()),
            "schema": (event(ORCH, wrap(dict(ok, extra=1))), make_ctx()),
            "size": (event(ORCH, content("halt", to=sorted(many))),
                     make_ctx(roles=dict(many, **{ORCH: p.ROLE_ORCHESTRATOR}))),
            "edit": (event(ORCH, dict(wrap(ok), **{"m.new_content": {}})), make_ctx()),
            "sender": (event(NO_ROLE, wrap(ok)), make_ctx()),
            "role": (event(WORKER2, wrap(ok)), make_ctx()),
            "target": (event(ORCH, wrap(dict(ok, to=[NO_ROLE]))), make_ctx()),
            "verb": (event(ORCH, wrap(dict(ok, verb="deploy"))), make_ctx()),
            "ref": (event(ORCH, wrap(dict(ok, ref=REFS["issue"]))), make_ctx()),
            "allowlist": (event(ORCH, wrap(dict(ok, ref=f"commit:example-org/other@{SHA}"))), make_ctx()),
            "re": (event(ORCH, wrap(dict(ok, re=EVENT_ID_2))), make_ctx()),
            "body": (event(ORCH, dict(wrap(ok), body="[agent-bus] fetch")), make_ctx()),
            "text": (event(ORCH, p.build_text([HUMAN], "status: all green")), make_ctx()),
        }
        for reason, (ev, ctx) in producers.items():
            with self.subTest(reason=reason):
                out = p.validate_event(ev, ctx, WORKER)
                self.assertEqual((out.kind, out.reason), (p.DROP, reason))
        self.assertEqual(set(producers) | p.REASONS_DECIDED_ELSEWHERE, set(p.DROP_REASONS))
        self.assertFalse(set(producers) & p.REASONS_DECIDED_ELSEWHERE)
        self.assertEqual(p.REASONS_DECIDED_ELSEWHERE, {"stale", "rate", "unresolved", "provenance"})


class TestHumanMessage(unittest.TestCase):
    def outcome(self, body: object, sender: str = HUMAN, me: str = WORKER, **kw: object) -> p.Outcome:
        return p.validate_event(event(sender, body), make_ctx(), me, **kw)

    def test_addressed_by_mention_is_delivered_as_written(self) -> None:
        text = "please halt\n\tand commit what you have é"
        out = self.outcome(human_content(text))
        self.assertEqual(out.kind, p.ACCEPT)
        self.assertIsNone(out.ping)
        self.assertEqual(
            (out.human.event_id, out.human.sender, out.human.origin_server_ts, out.human.text),
            (EVENT_ID, HUMAN, 1_700_000_000_000, text),
        )

    def test_room_mention_addresses_every_agent(self) -> None:
        body = human_content(mention=None, **{"m.mentions": {"room": True}})
        for me in (ORCH, WORKER, WORKER2):
            with self.subTest(me=me):
                self.assertEqual(self.outcome(body, me=me).kind, p.ACCEPT)

    def test_not_addressed_is_ignored(self) -> None:
        cases = [
            ("other agent", human_content(mention=(WORKER2,))),
            ("no mentions", human_content(mention=None)),
            ("handle typed only", human_content("myrepo.2+host.podman: do it", mention=None)),
            ("empty mentions", human_content(mention=())),
            ("room false", human_content(mention=None, **{"m.mentions": {"room": False}})),
            ("room string", human_content(mention=None, **{"m.mentions": {"room": "true"}})),
            ("mentions list", human_content(mention=None, **{"m.mentions": [WORKER]})),
            ("user_ids string", human_content(mention=None, **{"m.mentions": {"user_ids": WORKER}})),
        ]
        for label, body in cases:
            with self.subTest(case=label):
                self.assertEqual(self.outcome(body).kind, p.IGNORE)

    def test_format_and_formatted_body_are_never_delivered(self) -> None:
        body = human_content("plain", format="org.matrix.custom.html", formatted_body="<b>other</b>")
        out = self.outcome(body)
        self.assertEqual((out.kind, out.human.text), (p.ACCEPT, "plain"))

    def test_drops(self) -> None:
        cases = [
            ("notice", human_content(msgtype="m.notice"), "schema"),
            ("emote", human_content(msgtype="m.emote"), "schema"),
            ("no msgtype", {"body": "x", "m.mentions": {"user_ids": [WORKER]}}, "schema"),
            ("body not string", human_content(body=5), "schema"),
            ("no body", {"msgtype": "m.text", "m.mentions": {"user_ids": [WORKER]}}, "schema"),
            ("edit", human_content(**{"m.relates_to": {"rel_type": "m.replace", "event_id": EVENT_ID_2}}), "edit"),
            ("new content", human_content(**{"m.new_content": {"body": "x"}}), "edit"),
            ("relates_to list", human_content(**{"m.relates_to": [1]}), "schema"),
            ("over size", human_content("x" * (p.MAX_HUMAN_BODY_BYTES + 1)), "size"),
            ("over size utf8", human_content("é" * (p.MAX_HUMAN_BODY_BYTES // 2 + 1)), "size"),
        ]
        for label, body, reason in cases:
            with self.subTest(case=label):
                out = self.outcome(body)
                self.assertEqual((out.kind, out.reason), (p.DROP, reason))

    def test_size_boundary(self) -> None:
        self.assertEqual(p.MAX_HUMAN_BODY_BYTES, 16384)
        self.assertEqual(self.outcome(human_content("x" * p.MAX_HUMAN_BODY_BYTES)).kind, p.ACCEPT)

    def test_edit_and_size_drop_before_addressing(self) -> None:
        edit = human_content(mention=(WORKER2,), **{"m.new_content": {}})
        self.assertEqual(self.outcome(edit).reason, "edit")
        big = human_content("x" * (p.MAX_HUMAN_BODY_BYTES + 1), mention=(WORKER2,))
        self.assertEqual(self.outcome(big).reason, "size")

    def test_human_text_false_drops_every_human_message(self) -> None:
        out = self.outcome(human_content(), human_text=False)
        self.assertEqual((out.kind, out.reason), (p.DROP, "sender"))
        out = self.outcome(human_content(mention=(WORKER2,)), human_text=False)
        self.assertEqual((out.kind, out.reason), (p.DROP, "sender"))

    def test_only_listed_humans_on_this_server(self) -> None:
        for sender in ("@human9:server.test", FOREIGN_HUMAN, ADMIN, CONDUIT):
            with self.subTest(sender=sender):
                out = self.outcome(human_content(), sender=sender)
                self.assertEqual((out.kind, out.reason), (p.DROP, "sender"))

    def test_an_agents_room_mention_is_dropped_by_sender_class_first(self) -> None:
        body = human_content(mention=None, **{"m.mentions": {"room": True}})
        out = self.outcome(body, sender=ORCH)
        self.assertEqual((out.kind, out.reason), (p.DROP, "schema"))
        out = self.outcome(body, sender=NO_ROLE)
        self.assertEqual((out.kind, out.reason), (p.DROP, "sender"))

    def test_reply_and_thread_are_delivered(self) -> None:
        thread = human_content("in thread", **{"m.relates_to": {"rel_type": "m.thread", "event_id": EVENT_ID_2}})
        self.assertEqual(self.outcome(thread).human.text, "in thread")
        reply = human_content("no fallback", **{"m.relates_to": {"m.in_reply_to": {"event_id": EVENT_ID_2}}})
        self.assertEqual(self.outcome(reply).human.text, "no fallback")

    def test_reply_fallback_removed(self) -> None:
        reply = {"m.relates_to": {"m.in_reply_to": {"event_id": EVENT_ID_2}}}
        cases = [
            ("> <@a:server.test> agent words\n> more\n\nmy answer", "my answer"),
            ("> quoted\n\nline 1\n\nline 2", "line 1\n\nline 2"),
            ("> quoted\nno blank line", "no blank line"),
            ("> quoted\n\n\ntwo blanks", "\ntwo blanks"),
            ("answer\n> not leading", "answer\n> not leading"),
            (">tight\n\nx", "x"),
        ]
        for text, want in cases:
            with self.subTest(text=text):
                out = self.outcome(human_content(text, **reply))
                self.assertEqual((out.kind, out.human.text), (p.ACCEPT, want))

    def test_fallback_kept_when_not_a_reply(self) -> None:
        text = "> quoted\n\nmy answer"
        self.assertEqual(self.outcome(human_content(text)).human.text, text)

    def test_empty_after_fallback_removal_is_ignored(self) -> None:
        reply = {"m.relates_to": {"m.in_reply_to": {"event_id": EVENT_ID_2}}}
        for text in ("> only a quote", "> only a quote\n", "> q\n\n"):
            with self.subTest(text=text):
                self.assertEqual(self.outcome(human_content(text, **reply)).kind, p.IGNORE)

    def test_strip_reply_fallback_function(self) -> None:
        self.assertEqual(p.strip_reply_fallback("> a\n> b\n\nc"), "c")
        self.assertEqual(p.strip_reply_fallback("c"), "c")
        self.assertEqual(p.strip_reply_fallback(""), "")


#: One example per secret pattern, assembled so that no literal credential sits in the file.
SECRET_EXAMPLES = {
    "private-key": "here: " + "-" * 5 + "BEGIN OPENSSH PRIVATE KEY" + "-" * 5,
    "github-token": "use gh" + "p_" + "a1B2" * 9,
    "github-pat": "github" + "_pat_" + "A1b2" * 6,
    "aws-access-key-id": "key AK" + "IA" + "ABCDEFGH23456789 ok",
    "slack-token": "xo" + "xb-" + "1234567890-abcdef",
    "sk-api-key": "s" + "k-" + "ant-api03-" + "x" * 24,
    "google-api-key": "AI" + "za" + "B" * 35,
    "jwt": "ey" + "JhbGciOiJIUzI1NiJ9." + "ey" + "JzdWIiOiIxMjM0In0." + "abcdefghijk",
    "bearer": "Authorization: Bearer " + "abcdef0123456789xyz",
    "ansible-vault": "$ANSIBLE" + "_VAULT;1.1;AES256",
    "url-credentials": "clone https://user:" + "pa55word@example.com/x",
    "credential-assignment": "pass" + "word = hunter2hunter2",
}


class TestAgentText(unittest.TestCase):
    """§7: an agent's free text to the team's humans, refused on send unless well formed and
    clean, and dropped by every agent on receive."""

    def assertRefused(self, reason: str, body: object, ctx: p.Context | None = None) -> None:
        with self.assertRaises(p.Refusal) as caught:
            p.validate_text_content(body, ctx or make_ctx())
        self.assertEqual(caught.exception.reason, reason)

    def good(self) -> dict:
        return p.build_text([HUMAN2, HUMAN], "review done, see the PR\nthanks")

    def test_constants(self) -> None:
        self.assertEqual(p.TEXT_KEY, "agent_bus.text")
        self.assertTrue(p.TEXT_KEY.startswith(p.PREFIX + "."))
        self.assertEqual(p.TEXT_CONTENT_KEYS, ("msgtype", "body", "m.mentions", "agent_bus.text"))
        self.assertEqual(p.TEXT_OBJECT_KEYS, ("v", "to", "text"))
        self.assertEqual(p.MSGTYPE_TEXT, "m.notice")
        self.assertEqual(p.MAX_AGENT_TEXT_BYTES, 4096)

    def test_build_and_render(self) -> None:
        body = self.good()
        self.assertEqual(set(body), set(p.TEXT_CONTENT_KEYS))
        self.assertEqual(body["msgtype"], "m.notice")
        self.assertEqual(body["m.mentions"], {"user_ids": [HUMAN, HUMAN2]})
        self.assertEqual(body[p.TEXT_KEY], {"v": 1, "to": [HUMAN, HUMAN2], "text": "review done, see the PR\nthanks"})
        self.assertEqual(body["body"], f"[agent-bus] text -> {HUMAN} {HUMAN2}\nreview done, see the PR\nthanks")
        self.assertEqual(body["body"], p.render_text(body[p.TEXT_KEY]))

    def test_good_text_validates(self) -> None:
        text = p.validate_text_content(self.good(), make_ctx())
        self.assertEqual((text.to, text.text), ((HUMAN, HUMAN2), "review done, see the PR\nthanks"))
        one = p.validate_text_content(p.build_text([HUMAN], "x" * p.MAX_AGENT_TEXT_BYTES), make_ctx())
        self.assertEqual(one.to, (HUMAN,))

    def test_addressed_to_humans_only(self) -> None:
        for label, to in (
            ("an agent", [WORKER]),
            ("a human and an agent", [HUMAN, ORCH]),
            ("unlisted human", ["@human9:server.test"]),
            ("foreign human", [FOREIGN_HUMAN]),
            ("admin", [ADMIN]),
            ("conduit", [CONDUIT]),
            ("nobody", []),
            ("duplicate", [HUMAN, HUMAN]),
            ("not a user ID", ["human1"]),
        ):
            with self.subTest(case=label):
                self.assertRefused("target", p.build_text(to, "hello"))
        humans = [f"@h{i}:server.test" for i in range(p.HUMANS_MAX + 1)]
        ctx = make_ctx(humans=frozenset(humans))
        p.validate_text_content(p.build_text(humans[:p.HUMANS_MAX], "hi"), ctx)
        self.assertRefused("target", p.build_text(humans, "hi"), ctx)

    def test_size(self) -> None:
        self.assertRefused("size", p.build_text([HUMAN], "x" * (p.MAX_AGENT_TEXT_BYTES + 1)))
        self.assertRefused("size", p.build_text([HUMAN], "é" * (p.MAX_AGENT_TEXT_BYTES // 2 + 1)))

    def test_shape(self) -> None:
        good = self.good()
        obj = good[p.TEXT_KEY]
        cases = [
            ("not an object", "x", "schema"),
            ("msgtype text", dict(good, msgtype="m.text"), "schema"),
            ("extra content key", dict(good, extra=1), "schema"),
            ("format", dict(good, format="org.matrix.custom.html", formatted_body="<b>x</b>"), "schema"),
            ("no body", {k: v for k, v in good.items() if k != "body"}, "schema"),
            ("object string", dict(good, **{p.TEXT_KEY: "hi"}), "schema"),
            ("object extra key", dict(good, **{p.TEXT_KEY: dict(obj, re=EVENT_ID)}), "schema"),
            ("object missing text", dict(good, **{p.TEXT_KEY: {"v": 1, "to": obj["to"]}}), "schema"),
            ("v bool", dict(good, **{p.TEXT_KEY: dict(obj, v=True)}), "schema"),
            ("text int", dict(good, **{p.TEXT_KEY: dict(obj, text=5)}), "schema"),
            ("to string", dict(good, **{p.TEXT_KEY: dict(obj, to=HUMAN)}), "schema"),
            ("empty text", p.build_text([HUMAN], ""), "schema"),
            ("v two", dict(good, extra=1, **{p.TEXT_KEY: dict(obj, v=2)}), "version"),
            ("edit", dict(good, **{"m.relates_to": {"rel_type": "m.replace"}}), "edit"),
            ("new content", dict(good, **{"m.new_content": {}}), "edit"),
            ("mentions unsorted", dict(good, **{"m.mentions": {"user_ids": [HUMAN2, HUMAN]}}), "schema"),
            ("mentions room", dict(good, **{"m.mentions": {"user_ids": [HUMAN, HUMAN2], "room": True}}), "schema"),
            ("mentions an agent", dict(good, **{"m.mentions": {"user_ids": [HUMAN, HUMAN2, WORKER]}}), "schema"),
            ("body differs", dict(good, body=good["body"] + " also"), "body"),
            ("body int", dict(good, body=5), "schema"),
        ]
        for label, body, reason in cases:
            with self.subTest(case=label):
                self.assertRefused(reason, body)

    def test_secret_shaped_text_is_refused(self) -> None:
        self.assertEqual([name for name, _ in p.SECRET_PATTERNS], list(SECRET_EXAMPLES))
        for name, text in SECRET_EXAMPLES.items():
            with self.subTest(pattern=name):
                self.assertEqual(p.secret_shaped(text), name)
                self.assertRefused("secret", p.build_text([HUMAN], text))

    def test_ordinary_text_is_not_secret_shaped(self) -> None:
        for text in (
            "status: all green",
            f"see {REFS['pr']} and commit {SHA}",
            "the token bucket refills at 20 per minute",
            "password: see the vault",
            "https://example.com/a/b?x=1",
            "Bearer of bad news",
            "use sk-learn",
            "ask in #agents",
        ):
            with self.subTest(text=text):
                self.assertIsNone(p.secret_shaped(text))

    def test_sender_must_hold_a_role(self) -> None:
        ctx = make_ctx()
        for sender in (ORCH, WORKER):
            p.check_text_sender(sender, ctx)
        for sender in (NO_ROLE, HUMAN, ADMIN, CONDUIT, FOREIGN):
            with self.subTest(sender=sender), self.assertRaises(p.Refusal) as caught:
                p.check_text_sender(sender, ctx)
            self.assertEqual(caught.exception.reason, "role")

    def test_every_agent_drops_agent_text_whatever_its_addressing(self) -> None:
        good = self.good()
        cases = [
            ("to humans", good),
            ("mentions me", dict(good, **{"m.mentions": {"user_ids": [WORKER]}})),
            ("room mention", dict(good, **{"m.mentions": {"room": True}})),
            ("malformed object", dict(good, **{p.TEXT_KEY: "x"})),
            ("as m.text", dict(good, msgtype="m.text")),
            ("beside a ping", dict(content("halt"), **{p.TEXT_KEY: good[p.TEXT_KEY]})),
        ]
        for label, body in cases:
            for sender, me in ((ORCH, WORKER), (ORCH, WORKER2), (WORKER, ORCH)):
                with self.subTest(case=label, sender=sender, me=me):
                    out = p.validate_event(event(sender, body), make_ctx(), me)
                    self.assertEqual((out.kind, out.reason), (p.DROP, "text"))

    def test_a_non_member_sending_text_is_sender(self) -> None:
        out = p.validate_event(event(NO_ROLE, self.good()), make_ctx(), WORKER)
        self.assertEqual((out.kind, out.reason), (p.DROP, "sender"))


class TestTeamRecord(unittest.TestCase):
    def good(self) -> dict:
        return {
            "v": 1,
            "team": TEAM,
            "humans": [HUMAN, HUMAN2],
            "roles": {ORCH: "orchestrator", WORKER: "worker"},
            "repos": [{"repo": "example-org/myrepo", "branches": ["main", "release/1.x"]}],
            "path_prefixes": ["CLAUDE/Plan/", "docs/spec.md"],
            "forge_api": "https://api.github.com",
        }

    def parse(self, content: object) -> p.TeamRecord:
        return p.parse_team_record(content, SN, TEAM)

    def test_good_record(self) -> None:
        rec = self.parse(self.good())
        self.assertEqual(rec.team, TEAM)
        self.assertEqual(rec.humans, frozenset({HUMAN, HUMAN2}))
        self.assertEqual(rec.roles, {ORCH: "orchestrator", WORKER: "worker"})
        self.assertEqual(rec.repos, {"example-org/myrepo": ("main", "release/1.x")})
        self.assertEqual(rec.path_prefixes, ("CLAUDE/Plan/", "docs/spec.md"))
        self.assertEqual(rec.forge_api, "https://api.github.com")
        ctx = rec.context(SN)
        self.assertEqual((ctx.server_name, ctx.humans, ctx.roles), (SN, rec.humans, rec.roles))
        self.assertEqual(ctx.repos, rec.repos)
        p.validate_content(content("fetch", ref=REFS["path"]), ctx)

    def test_bounds(self) -> None:
        good = self.good()
        self.parse(dict(good, roles={}))
        roles64 = {f"@r{i}.1+h.podman:server.test": "worker" for i in range(64)}
        self.parse(dict(good, roles=roles64))
        self.parse(dict(good, humans=[f"@h{i}:server.test" for i in range(16)]))
        self.parse(dict(good, repos=[{"repo": f"o/r{i}", "branches": ["main"]} for i in range(32)]))
        self.parse(dict(good, path_prefixes=[f"p{i}/" for i in range(32)]))
        self.parse(dict(good, repos=[{"repo": "o/r", "branches": [f"b{i}" for i in range(8)]}]))
        for forge in ("https://api.github.com", "https://ghe.example.com/api/v3", "https://h:8443"):
            with self.subTest(forge=forge):
                self.assertEqual(self.parse(dict(good, forge_api=forge)).forge_api, forge)

    def test_refusals(self) -> None:
        good = self.good()
        roles65 = {f"@r{i}.1+h.podman:server.test": "worker" for i in range(65)}
        cases = [
            ("not an object", "team"),
            ("unknown key", dict(good, extra=1)),
            ("missing key", {k: v for k, v in good.items() if k != "forge_api"}),
            ("v two", dict(good, v=2)),
            ("v bool", dict(good, v=True)),
            ("other team", dict(good, team="team-b")),
            ("team not a name", dict(good, team="Team")),
            ("humans empty", dict(good, humans=[])),
            ("humans 17", dict(good, humans=[f"@h{i}:server.test" for i in range(17)])),
            ("human duplicate", dict(good, humans=[HUMAN, HUMAN])),
            ("human is a handle", dict(good, humans=[ORCH])),
            ("human is admin", dict(good, humans=[ADMIN])),
            ("human foreign", dict(good, humans=[FOREIGN_HUMAN])),
            ("humans string", dict(good, humans=HUMAN)),
            ("roles 65", dict(good, roles=roles65)),
            ("role unknown", dict(good, roles={ORCH: "admin"})),
            ("role for a human", dict(good, roles={HUMAN: "worker"})),
            ("role foreign", dict(good, roles={FOREIGN: "worker"})),
            ("roles list", dict(good, roles=[ORCH])),
            ("repos empty", dict(good, repos=[])),
            ("repos 33", dict(good, repos=[{"repo": f"o/r{i}", "branches": ["main"]} for i in range(33)])),
            ("repo upper", dict(good, repos=[{"repo": "Example-org/myrepo", "branches": ["main"]}])),
            ("repo .git", dict(good, repos=[{"repo": "o/r.git", "branches": ["main"]}])),
            ("repo dup", dict(good, repos=[{"repo": "o/r", "branches": ["a"]}, {"repo": "o/r", "branches": ["b"]}])),
            ("repo extra key", dict(good, repos=[{"repo": "o/r", "branches": ["main"], "x": 1}])),
            ("branches empty", dict(good, repos=[{"repo": "o/r", "branches": []}])),
            ("branches 9", dict(good, repos=[{"repo": "o/r", "branches": [f"b{i}" for i in range(9)]}])),
            ("branch bad", dict(good, repos=[{"repo": "o/r", "branches": ["a b"]}])),
            ("prefixes empty", dict(good, path_prefixes=[])),
            ("prefixes 33", dict(good, path_prefixes=[f"p{i}/" for i in range(33)])),
            ("prefix bad", dict(good, path_prefixes=["../x/"])),
            ("forge http", dict(good, forge_api="http://api.github.com")),
            ("forge bare", dict(good, forge_api="https://")),
            ("forge int", dict(good, forge_api=5)),
            ("forge space in host", dict(good, forge_api="https://a b")),
            ("forge space in path", dict(good, forge_api="https://a/b c")),
            ("forge no host", dict(good, forge_api="https:///api")),
            ("forge newline", dict(good, forge_api="https://a\n")),
            ("forge tab", dict(good, forge_api="https://a/\tb")),
        ]
        for label, body in cases:
            with self.subTest(case=label), self.assertRaises(p.Untrusted):
                self.parse(body)

    def team_event(self, **over: object) -> dict:
        ev: dict = {"type": p.EVENT_TEAM, "state_key": "", "sender": ADMIN,
                    "event_id": EVENT_ID, "content": self.good()}
        ev.update(over)
        return ev

    def test_team_event(self) -> None:
        rec = p.parse_team_event(self.team_event(), ADMIN, SN, TEAM)
        self.assertEqual(rec.team, TEAM)
        for label, ev in (
            ("not admin", self.team_event(sender=ORCH)),
            ("human", self.team_event(sender=HUMAN)),
            ("state key", self.team_event(state_key=ADMIN)),
            ("no state key", {k: v for k, v in self.team_event().items() if k != "state_key"}),
            ("type", self.team_event(type=p.EVENT_STATUS)),
            ("bad content", self.team_event(content=dict(self.good(), extra=1))),
            ("not an object", "x"),
        ):
            with self.subTest(case=label), self.assertRaises(p.Untrusted):
                p.parse_team_event(ev, ADMIN, SN, TEAM)


class TestStatus(unittest.TestCase):
    def status_event(self, sender: str = WORKER, **over: object) -> dict:
        ev: dict = {"type": p.EVENT_STATUS, "state_key": sender, "sender": sender,
                    "event_id": EVENT_ID, "content": {"v": 1, "state": "listening", "until": 5}}
        ev.update(over)
        return ev

    def test_good_status(self) -> None:
        self.assertEqual(p.read_status(self.status_event(), make_ctx()), 5)
        self.assertEqual(p.read_status(self.status_event(ORCH), make_ctx()), 5)

    def test_ignored(self) -> None:
        good_content = {"v": 1, "state": "listening", "until": 5}
        cases = [
            ("key is another member", self.status_event(state_key=WORKER2)),
            ("key empty", self.status_event(state_key="")),
            ("no key", {k: v for k, v in self.status_event().items() if k != "state_key"}),
            ("sender human", self.status_event(HUMAN)),
            ("sender no role", self.status_event(NO_ROLE)),
            ("sender admin", self.status_event(ADMIN)),
            ("type", self.status_event(type=p.EVENT_TEAM)),
            ("state busy", self.status_event(content=dict(good_content, state="busy"))),
            ("no until", self.status_event(content={"v": 1, "state": "listening"})),
            ("until bool", self.status_event(content=dict(good_content, until=True))),
            ("until float", self.status_event(content=dict(good_content, until=1.5))),
            ("until negative", self.status_event(content=dict(good_content, until=-1))),
            ("extra key", self.status_event(content=dict(good_content, note="x"))),
            ("v two", self.status_event(content=dict(good_content, v=2))),
            ("content list", self.status_event(content=[1])),
            ("oversize", self.status_event(content=dict(good_content, until=10 ** 250))),
            ("not an object", "x"),
        ]
        for label, ev in cases:
            with self.subTest(case=label):
                self.assertIsNone(p.read_status(ev, make_ctx()))

    def test_size_cap(self) -> None:
        self.assertEqual(p.MAX_STATUS_BYTES, 256)
        content_ = {"v": 1, "state": "listening", "until": 10 ** 200}
        self.assertLessEqual(p.content_size(content_), p.MAX_STATUS_BYTES)
        self.assertEqual(p.read_status(self.status_event(content=content_), make_ctx()), 10 ** 200)


class TestPowerLevels(unittest.TestCase):
    def test_expected_shape(self) -> None:
        want = {
            "users": {HUMAN: 50, HUMAN2: 50},
            "users_default": 0, "events_default": 0, "state_default": 100,
            "invite": 100, "kick": 100, "ban": 100, "redact": 100,
            "notifications": {"room": 50},
            "events": {"m.room.power_levels": 100, "m.room.tombstone": 150,
                       "m.room.redaction": 100, "m.reaction": 50, "m.sticker": 100,
                       "agent_bus.status": 0},
        }
        self.assertEqual(p.expected_power_levels([HUMAN2, HUMAN]), want)
        p.check_power_levels(want, frozenset({HUMAN, HUMAN2}))

    def test_any_difference_is_untrusted(self) -> None:
        humans = frozenset({HUMAN, HUMAN2})
        good = p.expected_power_levels(humans)

        def changed(fn) -> dict:
            body = copy.deepcopy(good)
            fn(body)
            return body

        cases = [
            ("admin listed", changed(lambda b: b["users"].update({ADMIN: 100}))),
            ("agent listed", changed(lambda b: b["users"].update({ORCH: 50}))),
            ("human missing", changed(lambda b: b["users"].pop(HUMAN))),
            ("human 100", changed(lambda b: b["users"].update({HUMAN: 100}))),
            ("bool level", changed(lambda b: b.update({"users_default": False}))),
            ("float level", changed(lambda b: b.update({"state_default": 100.0}))),
            ("events_default", changed(lambda b: b.update({"events_default": 50}))),
            ("state_default", changed(lambda b: b.update({"state_default": 50}))),
            ("extra event", changed(lambda b: b["events"].update({"agent_bus.team": 0}))),
            ("status raised", changed(lambda b: b["events"].update({"agent_bus.status": 50}))),
            ("notifications", changed(lambda b: b.update({"notifications": {"room": 0}}))),
            ("extra key", changed(lambda b: b.update({"historical": 100}))),
            ("missing key", changed(lambda b: b.pop("redact"))),
            ("not an object", "x"),
        ]
        for label, body in cases:
            with self.subTest(case=label), self.assertRaises(p.Untrusted):
                p.check_power_levels(body, humans)


class TestBuildPing(RefusalAssertions):
    def test_build_is_a_valid_envelope_and_round_trips(self) -> None:
        body = p.build_ping("review", [WORKER2, WORKER], ref=REFS["pr"])
        self.assertEqual(body, {
            "msgtype": "m.notice",
            "body": f"[agent-bus] review {REFS['pr']} -> {WORKER} {WORKER2}",
            "m.mentions": {"user_ids": [WORKER, WORKER2]},
            "agent_bus.ping": {"v": 1, "verb": "review", "to": [WORKER, WORKER2], "ref": REFS["pr"]},
        })
        ping = p.validate_content(body, make_ctx())
        self.assertEqual((ping.verb, ping.to, ping.ref.text), ("review", (WORKER, WORKER2), REFS["pr"]))

    def test_build_omits_absent_fields(self) -> None:
        body = p.build_ping("ack", [HUMAN], re=EVENT_ID)
        self.assertEqual(body[p.PING_KEY], {"v": 1, "verb": "ack", "to": [HUMAN], "re": EVENT_ID})
        self.assertEqual(body["body"], f"[agent-bus] ack - -> {HUMAN} re {EVENT_ID}")
        p.validate_content(body, make_ctx())

    def test_build_does_not_validate(self) -> None:
        body = p.build_ping("halt", [HUMAN])
        self.assertRefused("target", p.validate_content, body, make_ctx())


if __name__ == "__main__":
    unittest.main()

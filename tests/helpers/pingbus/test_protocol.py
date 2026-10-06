"""Tests for helpers.pingbus.protocol, the pure validator of the agent team bus.

The validator is the one function every sender and every receiver runs, so these tests
are tables: every verb against every reference form, every grammar edge, every note
character, every drop reason code. A rule the table does not exercise is a rule the bus
does not have.

Spec: docs/agent-team-bus-protocol.md. Fixture names are placeholders (`server.test`,
`example-org/myrepo`).
"""

from __future__ import annotations

import os
import string
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import protocol as p

SN = "server.test"
ORCH = "@myrepo.1+host.podman:server.test"
WORKER = "@myrepo.2+host.podman:server.test"
WORKER2 = "@other.1+host.podman:server.test"
NO_ROLE = "@myrepo.3+host.podman:server.test"
WARDEN = "@warden:server.test"
HUMAN = "@human1:server.test"
STEWARD = "@steward:server.test"
FOREIGN = "@myrepo.4+host.podman:elsewhere.test"

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


def make_ctx() -> p.Context:
    return p.Context(
        server_name=SN,
        warden=WARDEN,
        humans=frozenset({HUMAN}),
        roles={ORCH: p.ROLE_ORCHESTRATOR, WORKER: p.ROLE_WORKER, WORKER2: p.ROLE_WORKER},
        repos={"example-org/myrepo": ("main",)},
        path_prefixes=("CLAUDE/Plan/", "docs/spec.md"),
    )


def content(verb: str, **extra: object) -> dict:
    body: dict = {"v": 1, "verb": verb, "to": [WORKER]}
    body.update(extra)
    return body


def event(sender: str, body: object, **extra: object) -> dict:
    ev: dict = {
        "event_id": EVENT_ID,
        "type": p.EVENT_PING,
        "sender": sender,
        "content": body,
        "origin_server_ts": 1,
    }
    ev.update(extra)
    return ev


class RefusalAssertions(unittest.TestCase):
    def assertRefused(self, reason: str, fn, *args, **kwargs) -> None:
        with self.assertRaises(p.Refusal) as caught:
            fn(*args, **kwargs)
        self.assertEqual(caught.exception.reason, reason)


class TestConstants(unittest.TestCase):
    def test_namespace_is_one_constant_every_type_derives_from(self) -> None:
        self.assertEqual(p.NAMESPACE, "io.github.longtermsupport.agentbus")
        for suffix, name in zip(
            p.EVENT_TYPE_SUFFIXES,
            (p.EVENT_PING, p.EVENT_ROOM, p.EVENT_ROLES, p.EVENT_STATUS, p.EVENT_CONTROL),
        ):
            self.assertEqual(name, f"{p.NAMESPACE}.{suffix}")

    def test_version_is_one(self) -> None:
        self.assertEqual(p.PROTOCOL_VERSION, 1)

    def test_drop_reasons_are_the_closed_set(self) -> None:
        self.assertEqual(
            p.DROP_REASONS,
            (
                "version", "schema", "size", "edit", "sender", "role", "target", "verb",
                "ref", "allowlist", "note", "re", "behalf", "stale", "rate",
                "unresolved", "provenance",
            ),
        )

    def test_refusal_rejects_a_code_outside_the_set(self) -> None:
        with self.assertRaises(ValueError):
            p.Refusal("made-up")


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
                body = content(verb, **extra)
                if ref is not None:
                    body["ref"] = ref
                with self.subTest(verb=verb, form=form):
                    accept = form in allowed or (
                        form is None and (not allowed or verb in self.REF_OPTIONAL)
                    )
                    if accept:
                        ping = p.validate_content(body, sender, ctx)
                        p.check_role(verb, sender, ctx)
                        self.assertEqual(ping.verb, verb)
                        self.assertEqual(ping.ref.form if ping.ref else None, form)
                    else:
                        self.assertRefused("ref", p.validate_content, body, sender, ctx)

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
            body = content(verb, to=[ORCH])
            if ref:
                body["ref"] = ref
            if with_re:
                body["re"] = EVENT_ID_2
            with self.subTest(verb=verb, with_re=with_re):
                if reason:
                    self.assertRefused(reason, p.validate_content, body, WORKER, ctx)
                else:
                    self.assertEqual(p.validate_content(body, WORKER, ctx).re, body.get("re"))

    def test_re_must_be_an_event_id(self) -> None:
        for bad in ("", "$short", "!" + "A" * 43, "$" + "A" * 42 + "+"):
            with self.subTest(re=bad):
                self.assertRefused(
                    "re", p.validate_content, content("ack", to=[ORCH], re=bad), WORKER, make_ctx()
                )

    def test_unknown_verb(self) -> None:
        for bad in ("deploy", "FETCH", "", "run_qa"):
            with self.subTest(verb=bad):
                self.assertRefused("verb", p.validate_content, content(bad), ORCH, make_ctx())

    def test_ack_expected_column(self) -> None:
        expected = {v for v in self.ALLOWED if v in {"fetch", "sync", "review", "run-qa", "halt"}}
        self.assertEqual({v for v, r in p.VERBS.items() if r.ack_expected}, expected)


class TestRoles(RefusalAssertions):
    SENDERS = {
        "fetch": {"orchestrator", "warden"},
        "sync": {"orchestrator", "warden"},
        "review": {"orchestrator", "worker"},
        "run-qa": {"orchestrator", "worker"},
        "halt": {"orchestrator", "warden"},
        "ack": {"orchestrator", "worker", "warden"},
        "nack": {"orchestrator", "worker", "warden"},
        "done": {"orchestrator", "worker"},
        "blocked": {"orchestrator", "worker"},
    }

    def test_sender_class_by_verb(self) -> None:
        ctx = make_ctx()
        who = {"orchestrator": ORCH, "worker": WORKER, "warden": WARDEN}
        for verb, permitted in self.SENDERS.items():
            for cls, user in who.items():
                with self.subTest(verb=verb, sender=cls):
                    if cls in permitted:
                        p.check_role(verb, user, ctx)
                    else:
                        self.assertRefused("role", p.check_role, verb, user, ctx)

    def test_no_role_and_human_are_refused(self) -> None:
        for user in (NO_ROLE, HUMAN, STEWARD, FOREIGN):
            with self.subTest(user=user):
                self.assertRefused("role", p.check_role, "done", user, make_ctx())

    def test_sender_class(self) -> None:
        ctx = make_ctx()
        self.assertEqual(p.sender_class(ORCH, ctx), "orchestrator")
        self.assertEqual(p.sender_class(WORKER, ctx), "worker")
        self.assertEqual(p.sender_class(WARDEN, ctx), "warden")
        self.assertIsNone(p.sender_class(NO_ROLE, ctx))
        self.assertIsNone(p.sender_class(HUMAN, ctx))


class TestTargets(RefusalAssertions):
    def check(self, verb: str, to: object, reason: str | None, sender: str = ORCH, **extra: object) -> None:
        body = content(verb, to=to, **extra)
        with self.subTest(verb=verb, to=to, extra=extra):
            if reason:
                self.assertRefused(reason, p.validate_content, body, sender, make_ctx())
            else:
                self.assertEqual(p.validate_content(body, sender, make_ctx()).to, tuple(to))

    def test_targets(self) -> None:
        ok_ref = {"ref": REFS["path"]}
        self.check("fetch", [WORKER], None, **ok_ref)
        self.check("fetch", [WORKER, WORKER2], None, **ok_ref)
        self.check("fetch", [], "target", **ok_ref)
        self.check("fetch", [WORKER, WORKER], "target", **ok_ref)
        self.check("fetch", [NO_ROLE], "target", **ok_ref)
        self.check("fetch", [HUMAN], "target", **ok_ref)
        self.check("fetch", [STEWARD], "target", **ok_ref)
        self.check("fetch", [FOREIGN], "target", **ok_ref)
        self.check("fetch", ["myrepo.2+host.podman"], "target", **ok_ref)
        self.check("fetch", [WARDEN], "target", **ok_ref)
        self.check("halt", [WARDEN], "target")

    def test_warden_is_a_target_only_when_answering(self) -> None:
        for verb, ref in (("ack", None), ("nack", None), ("done", REFS["pr"]), ("blocked", REFS["issue"])):
            extra: dict = {"re": EVENT_ID_2}
            if ref:
                extra["ref"] = ref
            self.check(verb, [WARDEN], None, sender=WORKER, **extra)
        self.check("done", [WARDEN], "target", sender=WORKER, ref=REFS["pr"])

    def test_thirty_two_targets_is_the_limit(self) -> None:
        roles = {f"@r{i}.1+host.podman:server.test": p.ROLE_WORKER for i in range(33)}
        roles[ORCH] = p.ROLE_ORCHESTRATOR
        ctx = make_ctx()
        ctx = p.Context(
            server_name=SN, warden=WARDEN, humans=ctx.humans, roles=roles,
            repos=ctx.repos, path_prefixes=ctx.path_prefixes,
        )
        users = [u for u in roles if u != ORCH]
        body = content("halt", to=users[:32])
        self.assertEqual(len(p.validate_content(body, ORCH, ctx).to), 32)
        body = content("halt", to=users[:33])
        self.assertRefused("target", p.validate_content, body, ORCH, ctx)


class TestBehalf(RefusalAssertions):
    def test_on_behalf_of_exactly_when_sender_is_warden(self) -> None:
        ctx = make_ctx()
        base = content("halt", to=[ORCH])
        p.validate_content(dict(base, on_behalf_of=HUMAN), WARDEN, ctx)
        self.assertRefused("behalf", p.validate_content, base, WARDEN, ctx)
        self.assertRefused("behalf", p.validate_content, dict(base, on_behalf_of=ORCH), WARDEN, ctx)
        self.assertRefused(
            "behalf", p.validate_content, dict(base, on_behalf_of="@human1:elsewhere.test"), WARDEN, ctx
        )
        self.assertRefused("behalf", p.validate_content, dict(base, on_behalf_of=HUMAN), ORCH, ctx)


class TestSchema(RefusalAssertions):
    def test_schema_table(self) -> None:
        good = content("fetch", ref=REFS["path"])
        cases = [
            ("not an object", ["v", 1], "schema"),
            ("string", "ping", "schema"),
            ("unknown key", dict(good, extra="x"), "schema"),
            ("body key", dict(good, body="x"), "schema"),
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
            ("note float", dict(good, note=1.5), "schema"),
            ("re list", dict(good, re=[EVENT_ID]), "schema"),
            ("behalf null", dict(good, on_behalf_of=None), "schema"),
            ("relates_to", dict(good, **{"m.relates_to": {"rel_type": "m.replace"}}), "edit"),
            ("new_content", dict(good, **{"m.new_content": {}}), "edit"),
        ]
        for label, body, reason in cases:
            with self.subTest(case=label):
                self.assertRefused(reason, p.validate_content, body, ORCH, make_ctx())

    def test_size_limit(self) -> None:
        body = content("fetch", ref=REFS["path"], to=[WORKER] * 60)
        self.assertGreater(p.content_size(body), p.MAX_CONTENT_BYTES)
        self.assertRefused("size", p.validate_content, body, ORCH, make_ctx())

    def test_content_size_is_compact_utf8(self) -> None:
        self.assertEqual(p.content_size({"a": "é"}), len('{"a":"é"}'.encode("utf-8")))


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
        ]
        for ref, valid in cases:
            with self.subTest(ref=ref):
                parsed = p.parse_ref(ref)
                self.assertEqual(parsed is not None, valid)

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
                    self.assertRefused(reason, p.validate_content, body, ORCH, ctx)
                else:
                    p.validate_content(body, ORCH, ctx)

    def test_path_allowed_and_prefix_grammar(self) -> None:
        self.assertTrue(p.path_allowed("a/b/c", ("a/",)))
        self.assertFalse(p.path_allowed("ab/c", ("a/",)))
        self.assertTrue(p.path_allowed("a/f.md", ("a/f.md",)))
        self.assertFalse(p.path_allowed("a/f.mdx", ("a/f.md",)))
        self.assertFalse(p.path_allowed("a/f.md", ()))
        for prefix, valid in (("a/", True), ("a/b/", True), ("a/f.md", True), ("/a/", False),
                              ("a//", False), ("../a/", False), ("", False), ("/", False)):
            with self.subTest(prefix=prefix):
                self.assertEqual(p.is_path_prefix(prefix), valid)

    def test_branch_names(self) -> None:
        for name, valid in (("main", True), ("release/1.x", True), ("b" * 100, True),
                            ("b" * 101, False), ("", False), ("a b", False), ("a~1", False)):
            with self.subTest(branch=name):
                self.assertEqual(p.is_branch_name(name), valid)


class TestNote(RefusalAssertions):
    ALLOWED_CHARS = set(string.ascii_letters + string.digits + " .,:_/#()=-")

    def note(self, text: object) -> None:
        p.validate_content(content("fetch", ref=REFS["path"], note=text), ORCH, make_ctx())

    def test_boundaries(self) -> None:
        self.note("a")
        self.note("x" * 80)
        self.note("see PLAN.md: step (2) = done, #3 a_b/c-d")
        for bad in ("", "x" * 81, " a", "a ", "a  b", " "):
            with self.subTest(note=bad):
                self.assertRefused("note", self.note, bad)

    def test_every_forbidden_character(self) -> None:
        candidates = set(string.printable) | set("\x00\x7fé‮​ ．")
        forbidden = sorted(candidates - self.ALLOWED_CHARS)
        self.assertIn("\t", forbidden)
        self.assertIn("`", forbidden)
        for ch in forbidden:
            with self.subTest(char=repr(ch)):
                self.assertRefused("note", self.note, f"a{ch}b")

    def test_every_allowed_character(self) -> None:
        for ch in sorted(self.ALLOWED_CHARS - {" "}):
            with self.subTest(char=ch):
                self.note(f"a{ch}b")


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
            ("myrepo.1+h.podman ", False),
            ("warden", False),
        ]
        for handle, valid in cases:
            with self.subTest(handle=handle):
                self.assertEqual(p.parse_handle(handle) is not None, valid)
        h = p.parse_handle("myrepo.12+workstation.podman")
        self.assertEqual((h.repo, h.n, h.host, h.type), ("myrepo", 12, "workstation", "podman"))

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
                            ("admin", False), ("steward", False), ("warden", False), ("conduit", False)):
            with self.subTest(name=name):
                self.assertEqual(p.is_human_localpart(name), valid)

    def test_room_pair_names(self) -> None:
        for name, valid in (("pair-1", True), ("a" * 48, True), ("a" * 49, False),
                            ("-a", False), ("a_b", False), ("", False)):
            with self.subTest(name=name):
                self.assertEqual(p.is_room_pair_name(name), valid)

    def test_user_ids(self) -> None:
        self.assertEqual(p.parse_user_id(ORCH, SN), "myrepo.1+host.podman")
        self.assertEqual(p.parse_user_id(WARDEN, SN), "warden")
        self.assertIsNone(p.parse_user_id(FOREIGN, SN))
        self.assertIsNone(p.parse_user_id("myrepo.1+host.podman:server.test", SN))
        self.assertIsNone(p.parse_user_id("@:server.test", SN))
        self.assertIsNone(p.parse_user_id("@a b:server.test", SN))
        self.assertIsNone(p.parse_user_id(None, SN))


class TestEvent(unittest.TestCase):
    def outcome(self, ev: object, me: str = WORKER, **kw: object) -> p.Outcome:
        return p.validate_event(ev, make_ctx(), me, **kw)

    def test_accepts_a_valid_ping_addressed_to_me(self) -> None:
        out = self.outcome(event(ORCH, content("fetch", ref=REFS["path"], note="read it")))
        self.assertEqual(out.kind, p.ACCEPT)
        self.assertEqual(out.ping.sender, ORCH)
        self.assertEqual(out.ping.event_id, EVENT_ID)
        self.assertEqual(out.ping.note, "read it")

    def test_ignored_silently(self) -> None:
        good = event(ORCH, content("fetch", ref=REFS["path"]))
        self.assertEqual(self.outcome(good, seen={EVENT_ID}).kind, p.IGNORE)
        self.assertEqual(self.outcome(good, me=ORCH).kind, p.IGNORE)
        self.assertEqual(self.outcome(good, me=WORKER2).kind, p.IGNORE)

    def test_warden_mirrors_pings_not_addressed_to_it(self) -> None:
        good = event(ORCH, content("fetch", ref=REFS["path"]))
        self.assertEqual(self.outcome(good, me=WARDEN, mirror_all=True).kind, p.ACCEPT)

    def test_event_level_drops(self) -> None:
        good_body = content("fetch", ref=REFS["path"])
        cases = [
            ("bad event id", dict(event(ORCH, good_body), event_id="$x"), "schema"),
            ("missing event id", {k: v for k, v in event(ORCH, good_body).items() if k != "event_id"}, "schema"),
            ("not an object", ["x"], "schema"),
            ("wrong type", dict(event(ORCH, good_body), type="m.room.message"), "schema"),
            ("state key", dict(event(ORCH, good_body), state_key=""), "schema"),
            ("redacted", dict(event(ORCH, good_body), unsigned={"redacted_because": {}}), "schema"),
            ("no content", {k: v for k, v in event(ORCH, good_body).items() if k != "content"}, "schema"),
            ("edit", event(ORCH, dict(good_body, **{"m.relates_to": {}})), "edit"),
            ("no role", event(NO_ROLE, good_body), "sender"),
            ("human", event(HUMAN, good_body), "sender"),
            ("steward", event(STEWARD, good_body), "sender"),
            ("foreign", event(FOREIGN, good_body), "sender"),
            ("sender missing", dict(event(ORCH, good_body), sender=None), "sender"),
            ("role", event(WORKER2, good_body), "role"),
        ]
        for label, ev, reason in cases:
            with self.subTest(case=label):
                out = self.outcome(ev)
                self.assertEqual((out.kind, out.reason), (p.DROP, reason))

    def test_every_drop_reason_code(self) -> None:
        """Each code the offline validator owns is produced by an event; the rest are
        named as decided by limits, forge and provenance code, and nothing is unaccounted."""
        ok = content("fetch", ref=REFS["path"])
        producers = {
            "version": event(ORCH, dict(ok, v=2)),
            "schema": event(ORCH, dict(ok, extra=1)),
            "size": event(ORCH, dict(ok, to=[WORKER] * 60)),
            "edit": event(ORCH, dict(ok, **{"m.new_content": {}})),
            "sender": event(NO_ROLE, ok),
            "role": event(WORKER2, ok),
            "target": event(ORCH, dict(ok, to=[NO_ROLE])),
            "verb": event(ORCH, dict(ok, verb="deploy")),
            "ref": event(ORCH, dict(ok, ref=REFS["issue"])),
            "allowlist": event(ORCH, dict(ok, ref=f"commit:example-org/other@{SHA}")),
            "note": event(ORCH, dict(ok, note="a\tb")),
            "re": event(ORCH, dict(ok, re=EVENT_ID_2)),
            "behalf": event(ORCH, dict(ok, on_behalf_of=HUMAN)),
        }
        for reason, ev in producers.items():
            with self.subTest(reason=reason):
                out = self.outcome(ev)
                self.assertEqual((out.kind, out.reason), (p.DROP, reason))
        self.assertEqual(set(producers) | p.REASONS_DECIDED_ELSEWHERE, set(p.DROP_REASONS))
        self.assertFalse(set(producers) & p.REASONS_DECIDED_ELSEWHERE)


class TestStateContent(RefusalAssertions):
    def test_roles(self) -> None:
        good = {"v": 1, "roles": {ORCH: "orchestrator", WORKER: "worker"}}
        self.assertEqual(p.parse_roles(good, SN), {ORCH: "orchestrator", WORKER: "worker"})
        many = {f"@r{i}.1+h.podman:server.test": "worker" for i in range(63)}
        many[ORCH] = "orchestrator"
        self.assertEqual(len(p.parse_roles({"v": 1, "roles": many}, SN)), 64)
        many["@r99.1+h.podman:server.test"] = "worker"
        bad = [
            {"v": 1, "roles": many},
            {"v": 1, "roles": {}},
            {"v": 1, "roles": {WORKER: "worker"}},
            {"v": 1, "roles": {ORCH: "orchestrator", WORKER: "orchestrator"}},
            {"v": 1, "roles": {ORCH: "orchestrator", WORKER: "admin"}},
            {"v": 1, "roles": {ORCH: "orchestrator", HUMAN: "worker"}},
            {"v": 1, "roles": {ORCH: "orchestrator", WARDEN: "worker"}},
            {"v": 1, "roles": {ORCH: "orchestrator", FOREIGN: "worker"}},
            {"v": 2, "roles": {ORCH: "orchestrator"}},
            {"v": True, "roles": {ORCH: "orchestrator"}},
            {"roles": {ORCH: "orchestrator"}},
            {"v": 1, "roles": {ORCH: "orchestrator"}, "extra": 1},
            {"v": 1, "roles": [ORCH]},
            "roles",
        ]
        for body in bad:
            with self.subTest(body=str(body)[:60]):
                self.assertRefused("schema", p.parse_roles, body, SN)

    def test_status(self) -> None:
        self.assertEqual(p.parse_status({"v": 1, "state": "listening", "until": 5}), 5)
        for body in ({"v": 1, "state": "busy", "until": 5}, {"v": 1, "state": "listening"},
                     {"v": 1, "state": "listening", "until": True},
                     {"v": 1, "state": "listening", "until": 1.5},
                     {"v": 1, "state": "listening", "until": -1},
                     {"v": 1, "state": "listening", "until": 5, "x": 1}):
            with self.subTest(body=body):
                self.assertRefused("schema", p.parse_status, body)

    def test_room_and_control_markers(self) -> None:
        self.assertEqual(p.parse_room_marker({"v": 1, "control": ROOM_ID}), ROOM_ID)
        self.assertEqual(p.parse_control_marker({"v": 1, "bus_room": ROOM_ID}), ROOM_ID)
        for fn, key in ((p.parse_room_marker, "control"), (p.parse_control_marker, "bus_room")):
            for body in ({"v": 1, key: "!short"}, {"v": 1}, {"v": 1, key: ROOM_ID, "x": 1},
                         {"v": 2, key: ROOM_ID}, {key: ROOM_ID}):
                with self.subTest(fn=fn.__name__, body=body):
                    self.assertRefused("schema", fn, body)


class TestBuildPing(unittest.TestCase):
    def test_build_omits_absent_fields_and_round_trips(self) -> None:
        body = p.build_ping("review", [ORCH], ref=REFS["pr"])
        self.assertEqual(body, {"v": 1, "verb": "review", "to": [ORCH], "ref": REFS["pr"]})
        ping = p.validate_content(body, WORKER, make_ctx())
        self.assertEqual((ping.verb, ping.to, ping.ref.text), ("review", (ORCH,), REFS["pr"]))
        full = p.build_ping("halt", [ORCH], note="now", on_behalf_of=HUMAN)
        self.assertEqual(full, {"v": 1, "verb": "halt", "to": [ORCH], "note": "now", "on_behalf_of": HUMAN})
        self.assertEqual(p.build_ping("ack", [ORCH], re=EVENT_ID)["re"], EVENT_ID)


if __name__ == "__main__":
    unittest.main()

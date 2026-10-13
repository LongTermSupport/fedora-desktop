"""Unit tests for helpers/pingbus/forge.py: the forge check and provenance (spec §6).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_forge

Every HTTP exchange goes through an injected opener (`FakeOpener`), so nothing here
touches the network; the clock and the sleep are injected too.
"""

from __future__ import annotations

import dataclasses
import email.message
import http.client
import io
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
import urllib.error
import urllib.request
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import config, forge, limits, protocol
from tests.helpers.pingbus import fake_gh

API = "https://api.github.com"
GHE_API = "https://forge.example.com/api/v3"
SHA = "0123456789abcdef0123456789abcdef01234567"
HEAD_REF = "feature/x"
TOKEN = "ghp_notarealtoken0123456789abcdefghijkl"
REPO = "example-org/myrepo"
BRANCHES = ("main",)
T0 = 1_800_000_000.0


def ref(text: str) -> protocol.Ref:
    parsed = protocol.parse_ref(text)
    assert parsed is not None, text
    return parsed


PATH_REF = ref(f"path:{REPO}@{SHA}:CLAUDE/Plan/00161-x/PLAN.md")
COMMIT_REF = ref(f"commit:{REPO}@{SHA}")
PR_REF = ref(f"pr:{REPO}#12@{SHA}")
ISSUE_REF = ref(f"issue:{REPO}#7")


def compare_url(branch: str, api: str = API) -> str:
    return f"{api}/repos/{REPO}/compare/{branch}...{SHA}?per_page=1"


CONTENTS_URL = f"{API}/repos/{REPO}/contents/CLAUDE/Plan/00161-x/PLAN.md?ref={SHA}"
PULL_URL = f"{API}/repos/{REPO}/pulls/12"
ISSUE_URL = f"{API}/repos/{REPO}/issues/7"


def headers(values: dict[str, str] | None = None) -> email.message.Message:
    msg = email.message.Message()
    for key, value in (values or {}).items():
        msg[key] = value
    return msg


class FakeResponse:
    def __init__(self, body: object, status: int = 200, raw: bytes | None = None) -> None:
        self.status = status
        self.headers = headers()
        self._raw = raw if raw is not None else json.dumps(body).encode("utf-8")

    def read(self, size: int = -1) -> bytes:
        """The whole body (up to `size`) on every call, so a queued response can repeat."""
        return self._raw if size < 0 else self._raw[:size]

    def close(self) -> None:
        """Nothing to release; present because the caller closes what it opened."""

    def __enter__(self) -> FakeResponse:
        return self

    def __exit__(self, *exc: object) -> None:
        self.close()


class FailingBody:
    """A body whose read fails part-way, as a dropped connection does."""

    def __init__(self, exc: BaseException) -> None:
        self.exc = exc

    def read(self, size: int = -1) -> bytes:
        raise self.exc

    def close(self) -> None:
        """Nothing to release."""


class FailingResponse(FakeResponse):
    def __init__(self, exc: BaseException) -> None:
        super().__init__(None, raw=b"")
        self.exc = exc

    def read(self, size: int = -1) -> bytes:
        raise self.exc


class FakeHTTPError:
    """An HTTP error to raise: a fresh `HTTPError` each time, as its body is read once."""

    def __init__(self, url: str, code: int, hdrs: dict[str, str] | None, body: bytes) -> None:
        self.url, self.code, self.hdrs, self.body = url, code, hdrs, body

    def make(self) -> urllib.error.HTTPError:
        return urllib.error.HTTPError(self.url, self.code, "error", headers(self.hdrs), io.BytesIO(self.body))


def http_error(url: str, code: int, hdrs: dict[str, str] | None = None, body: bytes = b"{}") -> FakeHTTPError:
    return FakeHTTPError(url, code, hdrs, body)


def ok(body: object) -> FakeResponse:
    return FakeResponse(body)


def status(value: str) -> FakeResponse:
    return ok({"status": value})


class FakeOpener:
    """Answers each URL from its queue of responses (the last one repeats); an exception in
    the queue is raised. Every request is recorded."""

    def __init__(self, routes: dict[str, list[object]]) -> None:
        self.routes = {url: list(queue) for url, queue in routes.items()}
        self.requests: list[urllib.request.Request] = []
        self.timeouts: list[object] = []

    def open(self, request: urllib.request.Request, timeout: object = None) -> object:
        self.requests.append(request)
        self.timeouts.append(timeout)
        queue = self.routes.get(request.full_url)
        if not queue:
            raise AssertionError(f"unexpected request: {request.full_url}")
        item = queue.pop(0) if len(queue) > 1 else queue[0]
        if isinstance(item, FakeHTTPError):
            raise item.make()
        if isinstance(item, BaseException):
            raise item
        return item

    @property
    def urls(self) -> list[str]:
        return [r.full_url for r in self.requests]


class ForgeCase(unittest.TestCase):
    def setUp(self) -> None:
        self.now = T0
        self.slept: list[float] = []

    def make(self, routes: dict[str, list[object]], *, token: str = TOKEN,
             api: str = API, cache: forge.ForgeCache | None = None) -> tuple[forge.Forge, FakeOpener]:
        opener = FakeOpener(routes)
        client = forge.Forge(
            api, token=token, opener=opener, sleep=self.slept.append,
            clock=lambda: self.now, cache=cache,
        )
        return client, opener

    def assert_refused(self, code: str, client: forge.Forge, the_ref: protocol.Ref,
                       branches: tuple[str, ...] = BRANCHES) -> forge.ForgeError:
        with self.assertRaises(forge.ForgeError) as caught:
            client.check(the_ref, branches)
        self.assertEqual(caught.exception.code, code)
        self.assertNotIn(TOKEN, str(caught.exception))
        return caught.exception


class TestForgeError(unittest.TestCase):
    def test_codes_are_the_spec_refusals(self) -> None:
        self.assertEqual(
            forge.REFUSALS,
            ("not-found", "wrong-kind", "provenance", "forge-unreachable", "forge-auth", "forge-rate"),
        )

    def test_unknown_code_is_a_programming_error(self) -> None:
        with self.assertRaises(ValueError):
            forge.ForgeError("nope", "x")

    def test_exit_codes(self) -> None:
        for code in forge.REFUSALS:
            expected = 9 if code == "forge-rate" else 5
            self.assertEqual(forge.ForgeError(code, "x").exit_code, expected, code)

    def test_receive_drop_reasons_are_protocol_codes(self) -> None:
        for code in forge.REFUSALS:
            reason = forge.ForgeError(code, "x").drop_reason
            expected = {"provenance": "provenance", "forge-rate": "rate"}.get(code, "unresolved")
            self.assertEqual(reason, expected, code)
            self.assertIn(reason, protocol.REASONS_DECIDED_ELSEWHERE)


class TestRequests(ForgeCase):
    def test_token_is_an_unredirected_bearer_header(self) -> None:
        client, opener = self.make({compare_url("main"): [status("identical")]}, token=TOKEN)
        client.check(COMMIT_REF, BRANCHES)
        request = opener.requests[0]
        self.assertEqual(request.unredirected_hdrs.get("Authorization"), f"Bearer {TOKEN}")
        self.assertNotIn("Authorization", request.headers)
        self.assertEqual(request.get_method(), "GET")
        self.assertEqual(opener.timeouts[0], forge.TIMEOUT_S)

    def test_there_is_no_anonymous_client(self) -> None:
        """D66: every forge call carries gh's token; a client without one cannot be built."""
        for token in (None, ""):
            with self.subTest(token=token), self.assertRaises(ValueError):
                forge.Forge(API, token=token, opener=FakeOpener({}))
        with self.assertRaises(TypeError):
            forge.Forge(API, opener=FakeOpener({}))

    def test_github_accept_header(self) -> None:
        client, opener = self.make({compare_url("main"): [status("identical")]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(opener.requests[0].get_header("Accept"), "application/vnd.github+json")

    def test_api_with_a_path_prefix(self) -> None:
        client, opener = self.make({compare_url("main", GHE_API): [status("behind")]}, api=GHE_API)
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(opener.urls, [compare_url("main", GHE_API)])

    def test_api_must_be_https(self) -> None:
        with self.assertRaises(ValueError):
            forge.Forge("http://api.github.com", token=TOKEN, opener=FakeOpener({}))

    def test_branch_with_slash_is_kept_as_a_path(self) -> None:
        client, opener = self.make({compare_url("release/44"): [status("identical")]})
        client.check(COMMIT_REF, ("release/44",))
        self.assertEqual(opener.urls, [compare_url("release/44")])


class TestReachability(ForgeCase):
    def test_identical_and_behind_reach(self) -> None:
        for value in ("identical", "behind"):
            client, _ = self.make({compare_url("main"): [status(value)]})
            client.check(COMMIT_REF, BRANCHES)

    def test_ahead_and_diverged_are_provenance(self) -> None:
        for value in ("ahead", "diverged"):
            client, _ = self.make({compare_url("main"): [status(value)]})
            self.assert_refused("provenance", client, COMMIT_REF)

    def test_a_later_branch_may_reach(self) -> None:
        client, opener = self.make({
            compare_url("main"): [status("diverged")],
            compare_url("release"): [status("behind")],
        })
        client.check(COMMIT_REF, ("main", "release"))
        self.assertEqual(opener.urls, [compare_url("main"), compare_url("release")])

    def test_stops_at_the_first_branch_that_reaches(self) -> None:
        client, opener = self.make({compare_url("main"): [status("identical")]})
        client.check(COMMIT_REF, ("main", "release"))
        self.assertEqual(opener.urls, [compare_url("main")])

    def test_unknown_everywhere_is_not_found(self) -> None:
        client, _ = self.make({
            compare_url("main"): [http_error(compare_url("main"), 404)],
            compare_url("release"): [http_error(compare_url("release"), 404)],
        })
        self.assert_refused("not-found", client, COMMIT_REF, ("main", "release"))

    def test_found_but_unreachable_beats_not_found(self) -> None:
        client, _ = self.make({
            compare_url("main"): [http_error(compare_url("main"), 404)],
            compare_url("release"): [status("ahead")],
        })
        self.assert_refused("provenance", client, COMMIT_REF, ("main", "release"))

    def test_no_merge_base_is_provenance(self) -> None:
        client, _ = self.make({compare_url("main"): [http_error(compare_url("main"), 422)]})
        self.assert_refused("provenance", client, COMMIT_REF)

    def test_unexpected_status_value_is_provenance(self) -> None:
        for body in ({"status": "weird"}, {}, {"status": 1}, []):
            client, _ = self.make({compare_url("main"): [ok(body)]})
            self.assert_refused("provenance", client, COMMIT_REF)

    def test_no_branches_is_a_programming_error(self) -> None:
        client, _ = self.make({})
        with self.assertRaises(ValueError):
            client.check(COMMIT_REF, ())


class TestPath(ForgeCase):
    def test_reachable_file_resolves(self) -> None:
        client, opener = self.make({
            compare_url("main"): [status("behind")],
            CONTENTS_URL: [ok({"type": "file", "path": "x"})],
        })
        client.check(PATH_REF, BRANCHES)
        self.assertEqual(opener.urls, [compare_url("main"), CONTENTS_URL])

    def test_unreachable_is_refused_before_contents(self) -> None:
        client, opener = self.make({compare_url("main"): [status("diverged")]})
        self.assert_refused("provenance", client, PATH_REF)
        self.assertEqual(opener.urls, [compare_url("main")])

    def test_directory_is_wrong_kind(self) -> None:
        for body in ([{"type": "file"}], {"type": "dir"}, {"type": "symlink"}, {"type": "submodule"}):
            client, _ = self.make({
                compare_url("main"): [status("identical")],
                CONTENTS_URL: [ok(body)],
            })
            self.assert_refused("wrong-kind", client, PATH_REF)

    def test_missing_file_is_not_found(self) -> None:
        client, _ = self.make({
            compare_url("main"): [status("identical")],
            CONTENTS_URL: [http_error(CONTENTS_URL, 404)],
        })
        self.assert_refused("not-found", client, PATH_REF)


class TestPullRequest(ForgeCase):
    def pull(self, **overrides: object) -> dict[str, object]:
        body: dict[str, object] = {
            "head": {"ref": HEAD_REF, "repo": {"full_name": "Example-Org/MyRepo"}},
            "author_association": "MEMBER",
        }
        body.update(overrides)
        return body

    def routes(self, pull: object, compare: object = None) -> dict[str, list[object]]:
        return {
            PULL_URL: [ok(pull)],
            compare_url(HEAD_REF): [compare if compare is not None else status("identical")],
        }

    def test_same_repo_member_at_head_resolves(self) -> None:
        for association in ("OWNER", "MEMBER", "COLLABORATOR"):
            client, opener = self.make(self.routes(self.pull(author_association=association)))
            client.check(PR_REF, BRANCHES)
            self.assertEqual(opener.urls, [PULL_URL, compare_url(HEAD_REF)])

    def test_behind_head_resolves(self) -> None:
        client, _ = self.make(self.routes(self.pull(), status("behind")))
        client.check(PR_REF, BRANCHES)

    def test_ahead_or_diverged_of_head_is_provenance(self) -> None:
        for value in ("ahead", "diverged"):
            client, _ = self.make(self.routes(self.pull(), status(value)))
            self.assert_refused("provenance", client, PR_REF)

    def test_fork_head_is_provenance(self) -> None:
        pull = self.pull(head={"ref": HEAD_REF, "repo": {"full_name": "someone/myrepo"}})
        client, opener = self.make(self.routes(pull))
        self.assert_refused("provenance", client, PR_REF)
        self.assertEqual(opener.urls, [PULL_URL])

    def test_deleted_head_repo_is_provenance(self) -> None:
        client, _ = self.make(self.routes(self.pull(head={"ref": HEAD_REF, "repo": None})))
        self.assert_refused("provenance", client, PR_REF)

    def test_outside_author_is_provenance(self) -> None:
        for association in ("CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "NONE", "member", None):
            client, _ = self.make(self.routes(self.pull(author_association=association)))
            self.assert_refused("provenance", client, PR_REF)

    def test_head_ref_outside_the_branch_grammar_is_provenance(self) -> None:
        for bad in ("a b", "", "x" * 101, None, "../x?y"):
            pull = self.pull(head={"ref": bad, "repo": {"full_name": REPO}})
            client, opener = self.make(self.routes(pull))
            self.assert_refused("provenance", client, PR_REF)
            self.assertEqual(opener.urls, [PULL_URL])

    def test_malformed_pull_is_provenance(self) -> None:
        for body in ([], {"author_association": "MEMBER"}, {"head": "x", "author_association": "MEMBER"}):
            client, _ = self.make(self.routes(body))
            self.assert_refused("provenance", client, PR_REF)

    def test_missing_pull_is_not_found(self) -> None:
        client, _ = self.make({PULL_URL: [http_error(PULL_URL, 404)]})
        self.assert_refused("not-found", client, PR_REF)

    def test_trusted_branches_are_not_consulted(self) -> None:
        client, opener = self.make(self.routes(self.pull()))
        client.check(PR_REF, ("main", "release"))
        self.assertNotIn(compare_url("main"), opener.urls)


class TestIssue(ForgeCase):
    def test_issue_resolves(self) -> None:
        client, opener = self.make({ISSUE_URL: [ok({"number": 7, "title": "anything"})]})
        client.check(ISSUE_REF, BRANCHES)
        self.assertEqual(opener.urls, [ISSUE_URL])

    def test_pull_request_is_wrong_kind(self) -> None:
        client, _ = self.make({ISSUE_URL: [ok({"number": 7, "pull_request": {}})]})
        self.assert_refused("wrong-kind", client, ISSUE_REF)

    def test_not_an_object_is_wrong_kind(self) -> None:
        client, _ = self.make({ISSUE_URL: [ok([1])]})
        self.assert_refused("wrong-kind", client, ISSUE_REF)

    def test_missing_issue_is_not_found(self) -> None:
        for code in (404, 410):
            client, _ = self.make({ISSUE_URL: [http_error(ISSUE_URL, code)]})
            self.assert_refused("not-found", client, ISSUE_REF)


class TestFailures(ForgeCase):
    def test_401_and_plain_403_are_auth(self) -> None:
        for code in (401, 403):
            client, _ = self.make({compare_url("main"): [http_error(compare_url("main"), code)]}, token=TOKEN)
            self.assert_refused("forge-auth", client, COMMIT_REF)
        self.assertEqual(self.slept, [])

    def test_server_error_and_odd_codes_are_unreachable(self) -> None:
        for code in (500, 502, 503, 418, 304):
            client, _ = self.make({compare_url("main"): [http_error(compare_url("main"), code)]})
            self.assert_refused("forge-unreachable", client, COMMIT_REF)

    def test_network_error_is_unreachable(self) -> None:
        for exc in (urllib.error.URLError("down"), TimeoutError("slow"), ConnectionResetError("reset")):
            client, _ = self.make({compare_url("main"): [exc]})
            self.assert_refused("forge-unreachable", client, COMMIT_REF)

    def test_failure_while_reading_the_body_is_unreachable(self) -> None:
        for exc in (TimeoutError("slow"), ConnectionResetError("reset"), http.client.IncompleteRead(b"")):
            with self.subTest(exc=type(exc).__name__):
                client, _ = self.make({compare_url("main"): [FailingResponse(exc)]})
                self.assert_refused("forge-unreachable", client, COMMIT_REF)

    def test_failure_while_reading_a_rate_limit_body_is_unreachable(self) -> None:
        url = compare_url("main")
        error = urllib.error.HTTPError(url, 429, "error", headers(), FailingBody(TimeoutError("slow")))
        client, _ = self.make({url: [error]})
        self.assert_refused("forge-unreachable", client, COMMIT_REF)
        self.assertEqual(self.slept, [])

    def test_non_json_is_unreachable(self) -> None:
        client, _ = self.make({compare_url("main"): [FakeResponse(None, raw=b"<html>")]})
        self.assert_refused("forge-unreachable", client, COMMIT_REF)

    def test_oversized_response_is_unreachable(self) -> None:
        raw = b'{"status": "identical", "pad": "' + b"x" * forge.MAX_RESPONSE_BYTES + b'"}'
        client, _ = self.make({compare_url("main"): [FakeResponse(None, raw=raw)]})
        self.assert_refused("forge-unreachable", client, COMMIT_REF)

    def test_token_never_in_a_message(self) -> None:
        body = json.dumps({"message": TOKEN}).encode()
        client, _ = self.make(
            {compare_url("main"): [http_error(compare_url("main"), 401, body=body)]}, token=TOKEN,
        )
        error = self.assert_refused("forge-auth", client, COMMIT_REF)
        self.assertNotIn(TOKEN, repr(error))


class TestRateLimit(ForgeCase):
    URL = compare_url("main")

    def test_429_then_success_honours_retry_after(self) -> None:
        client, opener = self.make({self.URL: [
            http_error(self.URL, 429, {"Retry-After": "2"}), status("identical"),
        ]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.slept, [2])
        self.assertEqual(len(opener.requests), 2)

    def test_403_with_no_remaining_is_a_rate_limit(self) -> None:
        client, _ = self.make({self.URL: [
            http_error(self.URL, 403, {"x-ratelimit-remaining": "0"}), status("identical"),
        ]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.slept, [5])

    def test_403_primary_limit_waits_until_the_reset(self) -> None:
        reset = str(int(T0) + 12)
        client, _ = self.make({self.URL: [
            http_error(self.URL, 403, {"x-ratelimit-remaining": "0", "x-ratelimit-reset": reset}),
            status("identical"),
        ]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.slept, [12])

    def test_a_reset_beyond_the_cap_is_not_slept(self) -> None:
        reset = str(int(T0) + limits.SERVER_429_WAIT_MAX_S + 1)
        client, opener = self.make({self.URL: [
            http_error(self.URL, 403, {"x-ratelimit-remaining": "0", "x-ratelimit-reset": reset}),
        ]})
        self.assert_refused("forge-rate", client, COMMIT_REF)
        self.assertEqual(len(opener.requests), 1)
        self.assertEqual(self.slept, [])

    def test_a_reset_already_past_retries_at_once(self) -> None:
        reset = str(int(T0) - 30)
        client, _ = self.make({self.URL: [
            http_error(self.URL, 403, {"x-ratelimit-remaining": "0", "x-ratelimit-reset": reset}),
            status("identical"),
        ]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.slept, [0])

    def test_retry_after_outranks_the_reset(self) -> None:
        reset = str(int(T0) + 40)
        client, _ = self.make({self.URL: [
            http_error(self.URL, 403, {"x-ratelimit-remaining": "0", "x-ratelimit-reset": reset,
                                       "Retry-After": "3"}),
            status("identical"),
        ]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.slept, [3])

    def test_retry_after_ms_in_the_body_is_second_choice(self) -> None:
        body = json.dumps({"retry_after_ms": 1500}).encode()
        client, _ = self.make({self.URL: [http_error(self.URL, 429, body=body), status("identical")]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.slept, [1.5])

    def test_three_tries_then_forge_rate(self) -> None:
        client, opener = self.make({self.URL: [http_error(self.URL, 429)]})
        self.assert_refused("forge-rate", client, COMMIT_REF)
        self.assertEqual(len(opener.requests), 3)
        self.assertEqual(self.slept, [5, 5])

    def test_a_wait_beyond_the_cap_is_not_slept(self) -> None:
        wait = str(limits.SERVER_429_WAIT_MAX_S + 1)
        client, opener = self.make({self.URL: [http_error(self.URL, 429, {"Retry-After": wait})]})
        self.assert_refused("forge-rate", client, COMMIT_REF)
        self.assertEqual(len(opener.requests), 1)
        self.assertEqual(self.slept, [])

    def test_unparseable_retry_after_falls_back(self) -> None:
        client, _ = self.make({self.URL: [
            http_error(self.URL, 429, {"Retry-After": "Wed, 21 Oct 2015 07:28:00 GMT"}), status("identical"),
        ]})
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.slept, [5])


class CacheCase(ForgeCase):
    def setUp(self) -> None:
        super().setUp()
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.state = pathlib.Path(tmp.name) / "state"
        self.state.mkdir(mode=0o700)
        self.path = self.state / forge.CACHE_FILE

    def cache(self) -> forge.ForgeCache:
        return forge.ForgeCache.load(self.path)


class TestCache(CacheCase):
    def path_routes(self) -> dict[str, list[object]]:
        return {compare_url("main"): [status("identical")], CONTENTS_URL: [ok({"type": "file"})]}

    def test_missing_file_is_an_empty_cache(self) -> None:
        self.assertFalse(self.path.exists())
        self.cache()

    def test_second_check_within_the_hour_asks_nothing(self) -> None:
        client, opener = self.make(self.path_routes(), cache=self.cache())
        client.check(PATH_REF, BRANCHES)
        client2, opener2 = self.make(self.path_routes(), cache=self.cache())
        self.now += forge.REACH_TTL_S - 1
        client2.check(PATH_REF, BRANCHES)
        self.assertEqual(opener2.urls, [])

    def test_after_the_hour_reachability_is_asked_again_but_content_is_not(self) -> None:
        client, _ = self.make(self.path_routes(), cache=self.cache())
        client.check(PATH_REF, BRANCHES)
        self.now += forge.REACH_TTL_S + 1
        client2, opener2 = self.make(self.path_routes(), cache=self.cache())
        client2.check(PATH_REF, BRANCHES)
        self.assertEqual(opener2.urls, [compare_url("main")])

    def test_negative_results_are_not_cached(self) -> None:
        client, _ = self.make({compare_url("main"): [status("diverged")]}, cache=self.cache())
        self.assert_refused("provenance", client, COMMIT_REF)
        client2, opener2 = self.make({compare_url("main"): [status("identical")]}, cache=self.cache())
        client2.check(COMMIT_REF, BRANCHES)
        self.assertEqual(opener2.urls, [compare_url("main")])

    def test_a_branch_no_longer_trusted_is_not_used_from_the_cache(self) -> None:
        client, _ = self.make({compare_url("old"): [status("identical")]}, cache=self.cache())
        client.check(COMMIT_REF, ("old",))
        client2, opener2 = self.make({compare_url("main"): [status("diverged")]}, cache=self.cache())
        self.assert_refused("provenance", client2, COMMIT_REF, ("main",))
        self.assertEqual(opener2.urls, [compare_url("main")])

    def test_another_forge_api_does_not_share_entries(self) -> None:
        client, _ = self.make({compare_url("main"): [status("identical")]}, cache=self.cache())
        client.check(COMMIT_REF, BRANCHES)
        other = compare_url("main", GHE_API)
        client2, opener2 = self.make({other: [status("identical")]}, api=GHE_API, cache=self.cache())
        client2.check(COMMIT_REF, BRANCHES)
        self.assertEqual(opener2.urls, [other])

    def test_pull_request_cached_for_the_hour(self) -> None:
        pull = {"head": {"ref": HEAD_REF, "repo": {"full_name": REPO}}, "author_association": "OWNER"}
        routes = {PULL_URL: [ok(pull)], compare_url(HEAD_REF): [status("identical")]}
        client, _ = self.make(routes, cache=self.cache())
        client.check(PR_REF, BRANCHES)
        client2, opener2 = self.make(routes, cache=self.cache())
        client2.check(PR_REF, BRANCHES)
        self.assertEqual(opener2.urls, [])
        self.now += forge.REACH_TTL_S + 1
        client3, opener3 = self.make(routes, cache=self.cache())
        client3.check(PR_REF, BRANCHES)
        self.assertEqual(opener3.urls, [PULL_URL, compare_url(HEAD_REF)])

    def test_issues_are_never_cached(self) -> None:
        routes = {ISSUE_URL: [ok({"number": 7})]}
        client, _ = self.make(routes, cache=self.cache())
        client.check(ISSUE_REF, BRANCHES)
        client2, opener2 = self.make(routes, cache=self.cache())
        client2.check(ISSUE_REF, BRANCHES)
        self.assertEqual(opener2.urls, [ISSUE_URL])

    def test_file_is_private_and_expired_entries_are_pruned(self) -> None:
        client, _ = self.make({compare_url("main"): [status("identical")]}, cache=self.cache())
        client.check(COMMIT_REF, BRANCHES)
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o600)
        self.assertEqual(len(json.loads(self.path.read_text())["entries"]), 1)
        self.now += forge.REACH_TTL_S + 1
        client2, _ = self.make({compare_url("other"): [status("identical")]}, cache=self.cache())
        client2.check(COMMIT_REF, ("other",))
        entries = json.loads(self.path.read_text())["entries"]
        self.assertEqual(len(entries), 1)
        self.assertEqual([p for p in self.state.iterdir()], [self.path])

    def test_corrupt_file_is_refused(self) -> None:
        for raw in ("not json", "[]", '{"v": 1}', '{"v": 2, "entries": {}}',
                    '{"v": 1, "entries": {"k": "x"}}', '{"v": 1, "entries": {"k": true}}',
                    '{"v": 1, "entries": {"k": -1}}'):
            self.path.write_text(raw)
            with self.assertRaises(config.ConfigError, msg=raw) as caught:
                self.cache()
            self.assertIn(str(self.path), str(caught.exception))

    def test_symlinked_file_is_refused(self) -> None:
        target = self.state / "elsewhere.json"
        target.write_text('{"v": 1, "entries": {}}')
        self.path.symlink_to(target)
        with self.assertRaises(config.ConfigError):
            self.cache()


class TestRedirects(unittest.TestCase):
    def handler(self) -> forge.SameOriginRedirect:
        return forge.SameOriginRedirect(API)

    def redirect(self, new_url: str) -> urllib.request.Request | None:
        request = urllib.request.Request(f"{API}/repos/{REPO}/issues/7")
        request.add_unredirected_header("Authorization", f"Bearer {TOKEN}")
        return self.handler().redirect_request(request, io.BytesIO(), 301, "Moved", headers(), new_url)

    def test_another_host_is_refused(self) -> None:
        for url in ("https://evil.example.com/repos/x", "http://api.github.com/repos/x",
                    "https://api.github.com:8443/repos/x"):
            with self.assertRaises(urllib.error.HTTPError, msg=url):
                self.redirect(url)

    def test_same_origin_follows_without_the_token(self) -> None:
        new = self.redirect(f"{API}/repos/other/repo/issues/7")
        self.assertIsNotNone(new)
        self.assertFalse(new.has_header("Authorization"))
        self.assertNotIn("Authorization", new.unredirected_hdrs)

    def test_default_opener_uses_the_handler(self) -> None:
        opener = forge.default_opener(API)
        self.assertTrue(any(isinstance(h, forge.SameOriginRedirect) for h in opener.handlers))
        self.assertFalse(any(
            type(h) is urllib.request.HTTPRedirectHandler for h in opener.handlers
        ))


class TestCredential(unittest.TestCase):
    """§6: the forge credential's one source is `gh auth token --hostname github.com`. The
    subprocess is stubbed here; TestCredentialThroughAnExecutable runs a stand-in `gh`."""

    ENV = {"PATH": "/usr/bin:/bin", "HOME": "/home/<user>"}

    def stub(self, *, stdout: str = TOKEN + "\n", error: BaseException | None = None) -> mock.MagicMock:
        patcher = mock.patch.object(forge.subprocess, "run")
        run = patcher.start()
        self.addCleanup(patcher.stop)
        if error is not None:
            run.side_effect = error
        else:
            run.return_value = subprocess.CompletedProcess(list(forge.GH_ARGV), 0, stdout, "")
        return run

    def failed(self, code: int, stderr: str, stdout: str = "") -> subprocess.CalledProcessError:
        return subprocess.CalledProcessError(code, list(forge.GH_ARGV), stdout, stderr)

    def refusal(self, environ: dict[str, str] | None = None, api: str = API) -> str:
        with self.assertRaises(config.ConfigError) as caught:
            forge.resolve_credential(self.ENV if environ is None else environ, api)
        return str(caught.exception)

    def test_gh_is_asked_for_its_github_login(self) -> None:
        run = self.stub()
        credential = forge.resolve_credential(self.ENV, API)
        self.assertEqual(credential, forge.Credential("gh", TOKEN))
        run.assert_called_once()
        args, kwargs = run.call_args
        self.assertEqual(args, (["gh", "auth", "token", "--hostname", "github.com"],))
        self.assertIs(kwargs["check"], True)
        self.assertIs(kwargs["capture_output"], True)
        self.assertIs(kwargs["text"], True)
        self.assertEqual(kwargs["stdin"], subprocess.DEVNULL)
        self.assertEqual(kwargs["timeout"], forge.GH_TIMEOUT_S)
        self.assertNotIn("shell", kwargs)
        self.assertEqual(kwargs["env"]["HOME"], "/home/<user>")
        self.assertEqual(kwargs["env"]["GH_PROMPT_DISABLED"], "1")

    def test_gh_token_variables_reach_gh_and_are_not_read_by_pingbus(self) -> None:
        """GH_TOKEN and GITHUB_TOKEN work only through gh, which honours them itself."""
        run = self.stub()
        environ = dict(self.ENV, GH_TOKEN="from-the-environment", GITHUB_TOKEN="another")
        self.assertEqual(forge.resolve_credential(environ, API).token, TOKEN)
        self.assertEqual(run.call_args.kwargs["env"]["GH_TOKEN"], "from-the-environment")

    def test_former_pingbus_variables_are_not_a_source(self) -> None:
        run = self.stub(error=FileNotFoundError("gh"))
        environ = dict(self.ENV, PINGBUS_FORGE_TOKEN="t0123", PINGBUS_FORGE_TOKEN_FILE="/x/forge-token")
        self.refusal(environ)
        run.assert_called_once()

    def test_gh_missing_names_the_install_and_the_login(self) -> None:
        self.stub(error=FileNotFoundError("gh"))
        message = self.refusal()
        self.assertIn("install gh", message)
        self.assertIn("gh auth login --hostname github.com", message)

    def test_gh_not_logged_in_names_the_login(self) -> None:
        for stderr in ("no oauth token found for github.com\n", "no oauth token\n"):
            with self.subTest(stderr=stderr):
                self.stub(error=self.failed(1, stderr))
                message = self.refusal()
                self.assertIn("not logged in", message)
                self.assertIn("gh auth login --hostname github.com", message)

    def test_any_other_gh_failure_is_its_own_refusal(self) -> None:
        self.stub(error=self.failed(4, "failed to read configuration: permission denied\n", TOKEN))
        message = self.refusal()
        self.assertIn("exit 4", message)
        self.assertIn("permission denied", message)
        self.assertNotIn("not logged in", message)
        self.assertNotIn(TOKEN, message)

    def test_gh_that_never_answers_is_refused(self) -> None:
        self.stub(error=subprocess.TimeoutExpired(list(forge.GH_ARGV), forge.GH_TIMEOUT_S, output=TOKEN))
        message = self.refusal()
        self.assertIn(f"{forge.GH_TIMEOUT_S} s", message)
        self.assertNotIn(TOKEN, message)

    def test_gh_that_cannot_be_started_is_refused(self) -> None:
        self.stub(error=PermissionError(13, "Permission denied"))
        self.assertIn("PermissionError", self.refusal())

    def test_an_answer_that_is_not_one_token_is_refused_unquoted(self) -> None:
        for stdout in ("", "\n", "has space\n", "two\nlines\n", "x" * 5000):
            with self.subTest(stdout=stdout[:20]):
                self.stub(stdout=stdout)
                message = self.refusal()
                self.assertNotIn("has space", message)
                self.assertNotIn("lines", message)

    def test_a_forge_other_than_github_never_consults_gh(self) -> None:
        for api in (GHE_API, "https://api.github.com/", "https://api.github.com.example.com"):
            with self.subTest(api=api):
                run = self.stub()
                message = self.refusal(api=api)
                self.assertIn(forge.GITHUB_API, message)
                run.assert_not_called()

    def test_credential_repr_hides_the_token(self) -> None:
        self.stub()
        credential = forge.resolve_credential(self.ENV, API)
        self.assertNotIn(TOKEN, repr(credential))
        self.assertNotIn(TOKEN, str(credential))


class TestCredentialThroughAnExecutable(unittest.TestCase):
    """The real subprocess, with a stand-in `gh` first on the PATH pingbus is given."""

    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = pathlib.Path(tmp.name)

    def environ(self, answer: str) -> dict[str, str]:
        return {"PATH": fake_gh.path_with(fake_gh.fake_gh_dir(self.dir, answer)), "HOME": str(self.dir)}

    def test_logged_in(self) -> None:
        credential = forge.resolve_credential(self.environ("token"), API)
        self.assertEqual(credential, forge.Credential("gh", fake_gh.FAKE_GH_TOKEN))

    def test_logged_out(self) -> None:
        with self.assertRaises(config.ConfigError) as caught:
            forge.resolve_credential(self.environ("logged-out"), API)
        self.assertIn("gh auth login", str(caught.exception))

    def test_broken(self) -> None:
        with self.assertRaises(config.ConfigError) as caught:
            forge.resolve_credential(self.environ("broken"), API)
        self.assertIn("exit 1", str(caught.exception))

    def test_not_installed(self) -> None:
        empty = self.dir / "empty"
        empty.mkdir()
        with self.assertRaises(config.ConfigError) as caught:
            forge.resolve_credential({"PATH": str(empty), "HOME": str(self.dir)}, API)
        self.assertIn("install gh", str(caught.exception))

    def test_the_client_sends_it_only_as_an_unredirected_header(self) -> None:
        credential = forge.resolve_credential(self.environ("token"), API)
        client = forge.Forge(API, token=credential.token, opener=FakeOpener({}))
        request = client._request("/repos/x/y")
        self.assertEqual(request.unredirected_hdrs["Authorization"], f"Bearer {fake_gh.FAKE_GH_TOKEN}")
        self.assertNotIn("Authorization", request.headers)


ORG = "example-org"
ORG_REPO = f"{ORG}/other"
EXT_REPO = "someone/project"


def repo_meta(full: str, *, fork: bool = False, private: bool = False, branch: str = "trunk") -> FakeResponse:
    """`GET /repos/OWNER/REPO` as GitHub answers it, cut to what the forge check reads."""
    owner = full.partition("/")[0]
    return ok({"full_name": full, "owner": {"login": owner.title()}, "fork": fork,
               "private": private, "default_branch": branch})


def url(path: str) -> str:
    return f"{API}{path}"


def trusted(verb: str, the_ref: protocol.Ref | None) -> protocol.Ping:
    return protocol.Ping(verb, ("@x:y",), the_ref, None, trust=None if the_ref is None else protocol.TRUST_TRUSTED)


def external(verb: str, the_ref: protocol.Ref) -> protocol.Ping:
    return protocol.Ping(verb, ("@x:y",), the_ref, None, trust=protocol.TRUST_EXTERNAL)


class TestCheckPing(ForgeCase):
    RECORD = protocol.TeamRecord(
        team="team-a", humans=frozenset(), roles={},
        repos={REPO: ("main", "release")}, path_prefixes=("CLAUDE/Plan/",), forge_api=API, org=ORG,
    )

    def refused(self, code: str, ping: protocol.Ping, client: forge.Forge) -> None:
        with self.assertRaises(forge.ForgeError) as caught:
            forge.check_ping(ping, self.RECORD, client)
        self.assertEqual(caught.exception.code, code)

    def test_no_ref_asks_nothing(self) -> None:
        client, opener = self.make({})
        forge.check_ping(trusted("halt", None), self.RECORD, client)
        self.assertEqual(opener.urls, [])

    def test_listed_ref_checked_against_the_record_branches(self) -> None:
        """A listed repository, even one of the org, keeps its listed branches: no lookup."""
        client, opener = self.make({
            compare_url("main"): [status("diverged")],
            compare_url("release"): [status("behind")],
        })
        forge.check_ping(trusted("fetch", COMMIT_REF), self.RECORD, client)
        self.assertEqual(opener.urls, [compare_url("main"), compare_url("release")])

    def test_org_ref_checked_against_its_default_branch(self) -> None:
        the_ref = ref(f"commit:{ORG_REPO}@{SHA}")
        compare = url(f"/repos/{ORG_REPO}/compare/trunk...{SHA}?per_page=1")
        client, opener = self.make({url(f"/repos/{ORG_REPO}"): [repo_meta(ORG_REPO)], compare: [status("behind")]})
        forge.check_ping(trusted("sync", the_ref), self.RECORD, client)
        forge.check_ping(trusted("review", the_ref), self.RECORD, client)
        # The repository is looked up once per process; the reach is asked again (no cache file).
        self.assertEqual(opener.urls, [url(f"/repos/{ORG_REPO}"), compare, compare])

    def test_org_repo_answer_expires(self) -> None:
        """A watcher lives for hours: a repository moved out of the org, or made a fork,
        stops being trusted once its answer is REACH_TTL_S old."""
        the_ref = ref(f"commit:{ORG_REPO}@{SHA}")
        compare = url(f"/repos/{ORG_REPO}/compare/trunk...{SHA}?per_page=1")
        client, opener = self.make({url(f"/repos/{ORG_REPO}"): [repo_meta(ORG_REPO), repo_meta("elsewhere/other")],
                                    compare: [status("behind")]})
        forge.check_ping(trusted("review", the_ref), self.RECORD, client)
        self.now += forge.REACH_TTL_S - 1
        forge.check_ping(trusted("review", the_ref), self.RECORD, client)
        self.assertEqual(opener.urls.count(url(f"/repos/{ORG_REPO}")), 1)
        self.now += 2
        self.refused("provenance", trusted("review", the_ref), client)
        self.assertEqual(opener.urls.count(url(f"/repos/{ORG_REPO}")), 2)

    def test_org_ref_on_a_named_branch_of_the_repo_itself(self) -> None:
        """D64: any branch of an org repository, when it is that repository's own branch
        (`GET .../branches/B`) and holds the commit; never the API merely returning it."""
        the_ref = ref(f"commit:{ORG_REPO}@{SHA}")
        branch_url = url(f"/repos/{ORG_REPO}/branches/feature%2Fx")
        compare = url(f"/repos/{ORG_REPO}/compare/feature/x...{SHA}?per_page=1")
        ping = dataclasses.replace(trusted("review", the_ref), branch="feature/x")
        client, opener = self.make({url(f"/repos/{ORG_REPO}"): [repo_meta(ORG_REPO)],
                                    branch_url: [ok({"name": "feature/x"})], compare: [status("identical")]})
        forge.check_ping(ping, self.RECORD, client)
        self.assertEqual(opener.urls, [url(f"/repos/{ORG_REPO}"), branch_url, compare])
        # No such branch in the repository itself: refused before any compare.
        client, opener = self.make({url(f"/repos/{ORG_REPO}"): [repo_meta(ORG_REPO)],
                                    branch_url: [http_error(branch_url, 404)]})
        self.refused("provenance", ping, client)
        self.assertNotIn(compare, opener.urls)
        # A fork-network commit the parent's API serves, claimed on a branch it is not on.
        client, _ = self.make({url(f"/repos/{ORG_REPO}"): [repo_meta(ORG_REPO)],
                               branch_url: [ok({"name": "feature/x"})], compare: [status("diverged")],
                               url(f"/repos/{ORG_REPO}/commits/{SHA}"): [ok({"sha": SHA})]})
        self.refused("provenance", ping, client)

    def test_listed_ref_on_a_named_branch(self) -> None:
        listed = dataclasses.replace(trusted("fetch", COMMIT_REF), branch="release")
        client, opener = self.make({compare_url("release"): [status("behind")]})
        forge.check_ping(listed, self.RECORD, client)
        self.assertEqual(opener.urls, [compare_url("release")])
        client, opener = self.make({})
        self.refused("provenance", dataclasses.replace(listed, branch="feature/x"), client)
        self.assertEqual(opener.urls, [])

    def test_listed_with_no_branches_is_trusted_as_an_org_repository(self) -> None:
        """Plan 00177 Task 4.9: a repository listed with no `branches`, of any owner, is
        checked as an org repository is: its owner's own, not a fork, at the ping's branch or
        its default branch."""
        record = dataclasses.replace(self.RECORD, repos={EXT_REPO: ()})
        the_ref = ref(f"commit:{EXT_REPO}@{SHA}")
        compare = url(f"/repos/{EXT_REPO}/compare/trunk...{SHA}?per_page=1")
        client, opener = self.make({url(f"/repos/{EXT_REPO}"): [repo_meta(EXT_REPO, private=True)],
                                    compare: [status("behind")]})
        forge.check_ping(trusted("review", the_ref), record, client)
        self.assertEqual(opener.urls, [url(f"/repos/{EXT_REPO}"), compare])
        for label, meta in (("fork", repo_meta(EXT_REPO, fork=True)), ("moved", repo_meta("elsewhere/other"))):
            with self.subTest(case=label):
                client, _ = self.make({url(f"/repos/{EXT_REPO}"): [meta]})
                with self.assertRaises(forge.ForgeError) as caught:
                    forge.check_ping(trusted("review", the_ref), record, client)
                self.assertEqual(caught.exception.code, "provenance")

    def test_org_fork_or_foreign_owner_is_provenance(self) -> None:
        the_ref = ref(f"commit:{ORG_REPO}@{SHA}")
        for label, meta in (("fork", repo_meta(ORG_REPO, fork=True)),
                            ("moved", repo_meta("elsewhere/other")),
                            ("no fork flag", ok({"owner": {"login": ORG}, "default_branch": "main"})),
                            ("bad branch", repo_meta(ORG_REPO, branch="a b"))):
            with self.subTest(case=label):
                client, opener = self.make({url(f"/repos/{ORG_REPO}"): [meta]})
                self.refused("provenance", trusted("review", the_ref), client)
                self.assertEqual(opener.urls, [url(f"/repos/{ORG_REPO}")])

    def test_org_repo_missing_is_not_found(self) -> None:
        client, _ = self.make({url(f"/repos/{ORG_REPO}"): [http_error(url(f"/repos/{ORG_REPO}"), 404)]})
        self.refused("not-found", trusted("review", ref(f"commit:{ORG_REPO}@{SHA}")), client)

    def test_external_commit_and_path_need_only_exist_in_a_public_repo(self) -> None:
        contents = url(f"/repos/{EXT_REPO}/contents/any/where.md?ref={SHA}")
        client, opener = self.make({
            url(f"/repos/{EXT_REPO}"): [repo_meta(EXT_REPO)],
            url(f"/repos/{EXT_REPO}/commits/{SHA}"): [ok({"sha": SHA})],
            contents: [ok({"type": "file"})],
        })
        forge.check_ping(external("review", ref(f"commit:{EXT_REPO}@{SHA}")), self.RECORD, client)
        forge.check_ping(external("fetch", ref(f"path:{EXT_REPO}@{SHA}:any/where.md")), self.RECORD, client)
        self.assertEqual(opener.urls, [url(f"/repos/{EXT_REPO}"), url(f"/repos/{EXT_REPO}/commits/{SHA}"),
                                       url(f"/repos/{EXT_REPO}/commits/{SHA}"), contents])

    def test_external_private_repo_is_refused(self) -> None:
        client, opener = self.make({url(f"/repos/{EXT_REPO}"): [repo_meta(EXT_REPO, private=True)]})
        self.refused("provenance", external("review", ref(f"commit:{EXT_REPO}@{SHA}")), client)
        self.assertEqual(opener.urls, [url(f"/repos/{EXT_REPO}")])

    def test_external_fork_is_refused(self) -> None:
        """D64: a fork is never referenced, outside the org either. A fork's API serves its
        whole network's commits, so a fork can pass off anyone's commit under its name."""
        for label, meta in (("fork", repo_meta(EXT_REPO, fork=True)),
                            ("no fork flag", ok({"owner": {"login": "someone"}, "private": False}))):
            with self.subTest(case=label):
                client, opener = self.make({url(f"/repos/{EXT_REPO}"): [meta]})
                self.refused("provenance", external("review", ref(f"commit:{EXT_REPO}@{SHA}")), client)
                self.assertEqual(opener.urls, [url(f"/repos/{EXT_REPO}")])

    def test_external_unknown_commit_is_not_found(self) -> None:
        commit = url(f"/repos/{EXT_REPO}/commits/{SHA}")
        client, _ = self.make({url(f"/repos/{EXT_REPO}"): [repo_meta(EXT_REPO)], commit: [http_error(commit, 422)]})
        self.refused("not-found", external("done", ref(f"commit:{EXT_REPO}@{SHA}")), client)

    def test_external_pull_request_at_its_head_from_anywhere(self) -> None:
        head = "f" * 40
        pull = url(f"/repos/{EXT_REPO}/pulls/3")
        compare = url(f"/repos/{EXT_REPO}/compare/{head}...{SHA}?per_page=1")
        the_ref = ref(f"pr:{EXT_REPO}#3@{SHA}")
        body = {"head": {"sha": head, "repo": {"full_name": "a-fork/project"}}, "author_association": "NONE"}
        client, _ = self.make({url(f"/repos/{EXT_REPO}"): [repo_meta(EXT_REPO)], pull: [ok(body)],
                               compare: [status("behind")]})
        forge.check_ping(external("review", the_ref), self.RECORD, client)
        client, _ = self.make({url(f"/repos/{EXT_REPO}"): [repo_meta(EXT_REPO)], pull: [ok(body)],
                               compare: [status("ahead")]})
        self.refused("not-found", external("review", the_ref), client)

    def test_external_issue(self) -> None:
        issue = url(f"/repos/{EXT_REPO}/issues/4")
        client, _ = self.make({url(f"/repos/{EXT_REPO}"): [repo_meta(EXT_REPO)], issue: [ok({"number": 4})]})
        forge.check_ping(external("done", ref(f"issue:{EXT_REPO}#4")), self.RECORD, client)

    def test_trust_must_be_the_records(self) -> None:
        """A ping validated against another record (or not at all) is a programming error."""
        client, _ = self.make({})
        for ping in (protocol.Ping("fetch", ("@x:y",), COMMIT_REF, None),
                     external("fetch", COMMIT_REF),
                     trusted("fetch", ref(f"commit:{EXT_REPO}@{SHA}"))):
            with self.subTest(ping=ping), self.assertRaises(ValueError):
                forge.check_ping(ping, self.RECORD, client)

    def test_forge_must_match_the_record(self) -> None:
        client, _ = self.make({}, api=GHE_API)
        with self.assertRaises(ValueError):
            forge.check_ping(trusted("fetch", COMMIT_REF), self.RECORD, client)

    def test_for_record_builds_the_client(self) -> None:
        opener = FakeOpener({compare_url("main"): [status("identical")]})
        client = forge.Forge.for_record(self.RECORD, forge.Credential("gh", TOKEN), opener=opener)
        self.assertEqual(client.api, API)
        client.check(COMMIT_REF, ("main",))
        self.assertEqual(opener.requests[0].unredirected_hdrs["Authorization"], f"Bearer {TOKEN}")


class TestRepoAnswerCache(CacheCase):
    """Plan 00161 D66: each `pingbus` command is a new process, so the repository answer
    (`GET /repos/O/R`) is kept in the cache file, for REACH_TTL_S like reachability."""

    REPO_URL = url(f"/repos/{ORG_REPO}")
    ORG_REF = ref(f"commit:{ORG_REPO}@{SHA}")
    OTHER_SHA = "fedcba9876543210fedcba9876543210fedcba98"
    OTHER_REF = ref(f"commit:{ORG_REPO}@{OTHER_SHA}")
    RECORD = TestCheckPing.RECORD

    def routes(self, meta: FakeResponse | None = None) -> dict[str, list[object]]:
        return {
            self.REPO_URL: [meta or repo_meta(ORG_REPO)],
            url(f"/repos/{ORG_REPO}/compare/trunk...{SHA}?per_page=1"): [status("behind")],
            url(f"/repos/{ORG_REPO}/compare/trunk...{self.OTHER_SHA}?per_page=1"): [status("behind")],
        }

    def ping(self, client: forge.Forge, the_ref: protocol.Ref) -> None:
        forge.check_ping(trusted("review", the_ref), self.RECORD, client)

    def test_a_second_process_pinging_the_same_repo_asks_for_the_repo_once_in_all(self) -> None:
        """Calls per ping, one new process each: 2 for a new commit without the file's help
        (the repository, then the compare), 1 with it (the compare only), 0 for a repeat."""
        calls = []
        for cache in (None, None):
            client, opener = self.make(self.routes(), cache=cache)
            self.ping(client, self.OTHER_REF)
            calls.append(len(opener.urls))
        self.assertEqual(calls, [2, 2])
        calls = []
        for the_ref in (self.ORG_REF, self.OTHER_REF, self.OTHER_REF):
            client, opener = self.make(self.routes(), cache=self.cache())
            self.ping(client, the_ref)
            calls.append(len(opener.urls))
        self.assertEqual(calls, [2, 1, 0])

    def test_the_answer_expires_after_the_hour(self) -> None:
        client, _ = self.make(self.routes(), cache=self.cache())
        self.ping(client, self.ORG_REF)
        self.now += forge.REACH_TTL_S - 1
        client, opener = self.make(self.routes(), cache=self.cache())
        self.ping(client, self.OTHER_REF)
        self.assertNotIn(self.REPO_URL, opener.urls)
        self.now += 2
        client, opener = self.make(self.routes(), cache=self.cache())
        self.ping(client, self.OTHER_REF)
        self.assertIn(self.REPO_URL, opener.urls)

    def test_a_kept_fork_answer_is_still_refused_and_a_refusal_is_not_kept(self) -> None:
        client, _ = self.make(self.routes(repo_meta(ORG_REPO, fork=True)), cache=self.cache())
        with self.assertRaises(forge.ForgeError) as caught:
            self.ping(client, self.ORG_REF)
        self.assertEqual(caught.exception.code, "provenance")
        # The answer was read, so it is kept; the verdict is made from it every time.
        client, opener = self.make({}, cache=self.cache())
        with self.assertRaises(forge.ForgeError) as caught:
            self.ping(client, self.ORG_REF)
        self.assertEqual((caught.exception.code, opener.urls), ("provenance", []))

    def test_a_missing_repository_is_not_kept(self) -> None:
        client, _ = self.make({self.REPO_URL: [http_error(self.REPO_URL, 404)]}, cache=self.cache())
        with self.assertRaises(forge.ForgeError):
            self.ping(client, self.ORG_REF)
        client, opener = self.make(self.routes(), cache=self.cache())
        self.ping(client, self.ORG_REF)
        self.assertIn(self.REPO_URL, opener.urls)

    def test_the_file_keeps_the_answer_and_still_loads_without_one(self) -> None:
        client, _ = self.make(self.routes(), cache=self.cache())
        self.ping(client, self.ORG_REF)
        data = json.loads(self.path.read_text())
        self.assertEqual(data["v"], forge.CACHE_VERSION)
        kept = data["repos"][f"{API}|repo|{ORG_REPO}"]
        self.assertEqual(kept, {"exp": int(T0) + forge.REACH_TTL_S, "owner": "Example-Org", "private": False,
                                "fork": False, "default_branch": "trunk"})
        self.path.write_text(json.dumps({"v": forge.CACHE_VERSION, "entries": {}}))
        self.cache()

    def test_expired_answers_are_pruned(self) -> None:
        client, _ = self.make(self.routes(), cache=self.cache())
        self.ping(client, self.ORG_REF)
        self.now += forge.REACH_TTL_S + 1
        client, _ = self.make(self.routes(), cache=self.cache())
        self.ping(client, self.ORG_REF)
        self.assertEqual(len(json.loads(self.path.read_text())["repos"]), 1)

    def test_a_malformed_repos_section_is_refused(self) -> None:
        good = {"exp": 1, "owner": "o", "private": False, "fork": False, "default_branch": "main"}
        for label, repos in (("not an object", []), ("entry not an object", {"k": 1}),
                             ("missing field", {"k": {"exp": 1}}),
                             ("extra field", {"k": dict(good, extra=1)}),
                             ("bad exp", {"k": dict(good, exp=-1)}),
                             ("exp bool", {"k": dict(good, exp=True)}),
                             ("owner not str", {"k": dict(good, owner=3)}),
                             ("fork not bool", {"k": dict(good, fork="no")})):
            with self.subTest(case=label):
                self.path.write_text(json.dumps({"v": forge.CACHE_VERSION, "entries": {}, "repos": repos}))
                with self.assertRaises(config.ConfigError):
                    self.cache()


if __name__ == "__main__":
    unittest.main()

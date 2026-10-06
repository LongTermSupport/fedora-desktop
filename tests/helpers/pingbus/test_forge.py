"""Unit tests for helpers/pingbus/forge.py: the forge check and provenance (spec §6).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_forge

Every HTTP exchange goes through an injected opener (`FakeOpener`), so nothing here
touches the network; the clock and the sleep are injected too.
"""

from __future__ import annotations

import email.message
import http.client
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest
import urllib.error
import urllib.request

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import config, forge, protocol

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

    def make(self, routes: dict[str, list[object]], *, token: str | None = None,
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
            expected = "provenance" if code == "provenance" else "unresolved"
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

    def test_no_token_no_authorization_header(self) -> None:
        client, opener = self.make({compare_url("main"): [status("identical")]})
        client.check(COMMIT_REF, BRANCHES)
        request = opener.requests[0]
        self.assertFalse(request.has_header("Authorization"))
        self.assertNotIn("Authorization", request.unredirected_hdrs)

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
            forge.Forge("http://api.github.com", opener=FakeOpener({}))

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
        reset = str(int(T0) + forge.RATE_WAIT_MAX_S + 1)
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
        wait = str(forge.RATE_WAIT_MAX_S + 1)
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
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = pathlib.Path(tmp.name)
        self.file = self.dir / "forge-token"
        self.file.write_text("file-token")
        self.file.chmod(0o600)

    def resolve(self, environ: dict[str, str], api: str = API) -> forge.Credential:
        return forge.resolve_credential(environ, api)

    def test_precedence(self) -> None:
        full = {"PINGBUS_FORGE_TOKEN_FILE": str(self.file), "PINGBUS_FORGE_TOKEN": "env-token",
                "GH_TOKEN": "gh-token", "GITHUB_TOKEN": "github-token"}
        expected = [
            ("PINGBUS_FORGE_TOKEN_FILE", "file-token"),
            ("PINGBUS_FORGE_TOKEN", "env-token"),
            ("GH_TOKEN", "gh-token"),
            ("GITHUB_TOKEN", "github-token"),
            ("none", None),
        ]
        environ = dict(full)
        for source, token in expected:
            self.assertEqual(self.resolve(environ), forge.Credential(source, token))
            if environ:
                environ.pop(next(iter(environ)))

    def test_empty_values_are_unset(self) -> None:
        environ = {"PINGBUS_FORGE_TOKEN_FILE": "", "PINGBUS_FORGE_TOKEN": "", "GH_TOKEN": "",
                   "GITHUB_TOKEN": "github-token"}
        self.assertEqual(self.resolve(environ).source, "GITHUB_TOKEN")

    def test_gh_tokens_only_for_exactly_github(self) -> None:
        environ = {"GH_TOKEN": "gh-token", "GITHUB_TOKEN": "github-token"}
        for api in (GHE_API, "https://api.github.com/", "https://api.github.com.example.com"):
            self.assertEqual(self.resolve(environ, api), forge.Credential("none", None), api)
        self.assertEqual(self.resolve({"PINGBUS_FORGE_TOKEN": "t0123"}, GHE_API).token, "t0123")

    def test_token_file_mode_must_be_0600_or_stricter(self) -> None:
        self.file.chmod(0o400)
        self.assertEqual(self.resolve({"PINGBUS_FORGE_TOKEN_FILE": str(self.file)}).token, "file-token")
        for mode in (0o640, 0o644, 0o604):
            self.file.chmod(mode)
            with self.assertRaises(config.ConfigError, msg=oct(mode)):
                self.resolve({"PINGBUS_FORGE_TOKEN_FILE": str(self.file)})

    def test_token_file_refusals(self) -> None:
        link = self.dir / "link"
        link.symlink_to(self.file)
        cases = {
            "missing": str(self.dir / "absent"),
            "relative": "forge-token",
            "symlink": str(link),
            "directory": str(self.dir),
        }
        for name, value in cases.items():
            with self.assertRaises(config.ConfigError, msg=name) as caught:
                self.resolve({"PINGBUS_FORGE_TOKEN_FILE": value})
            self.assertNotIn("file-token", str(caught.exception))

    def test_token_file_owner_is_checked(self) -> None:
        with self.assertRaises(config.ConfigError):
            forge.resolve_credential({"PINGBUS_FORGE_TOKEN_FILE": str(self.file)}, API, uid=os.getuid() + 1)

    def test_token_shape_is_checked_and_never_quoted(self) -> None:
        bad = "has space\n"
        self.file.write_text(bad)
        with self.assertRaises(config.ConfigError) as caught:
            self.resolve({"PINGBUS_FORGE_TOKEN_FILE": str(self.file)})
        self.assertNotIn("has space", str(caught.exception))
        with self.assertRaises(config.ConfigError) as caught:
            self.resolve({"PINGBUS_FORGE_TOKEN": bad})
        self.assertNotIn("has space", str(caught.exception))

    def test_credential_repr_hides_the_token(self) -> None:
        credential = self.resolve({"PINGBUS_FORGE_TOKEN": TOKEN})
        self.assertNotIn(TOKEN, repr(credential))
        self.assertNotIn(TOKEN, str(credential))


class TestCheckPing(ForgeCase):
    RECORD = protocol.TeamRecord(
        team="team-a", humans=frozenset(), roles={},
        repos={REPO: ("main", "release")}, path_prefixes=("CLAUDE/Plan/",), forge_api=API,
    )

    def test_no_ref_asks_nothing(self) -> None:
        client, opener = self.make({})
        forge.check_ping(protocol.Ping("halt", ("@x:y",), None, None), self.RECORD, client)
        self.assertEqual(opener.urls, [])

    def test_ref_checked_against_the_record_branches(self) -> None:
        client, opener = self.make({
            compare_url("main"): [status("diverged")],
            compare_url("release"): [status("behind")],
        })
        forge.check_ping(protocol.Ping("fetch", ("@x:y",), COMMIT_REF, None), self.RECORD, client)
        self.assertEqual(opener.urls, [compare_url("main"), compare_url("release")])

    def test_repo_outside_the_allowlist_is_a_programming_error(self) -> None:
        client, _ = self.make({})
        stray = ref(f"commit:other/repo@{SHA}")
        with self.assertRaises(ValueError):
            forge.check_ping(protocol.Ping("fetch", ("@x:y",), stray, None), self.RECORD, client)

    def test_forge_must_match_the_record(self) -> None:
        client, _ = self.make({}, api=GHE_API)
        with self.assertRaises(ValueError):
            forge.check_ping(protocol.Ping("fetch", ("@x:y",), COMMIT_REF, None), self.RECORD, client)

    def test_for_record_builds_the_client(self) -> None:
        opener = FakeOpener({compare_url("main"): [status("identical")]})
        client = forge.Forge.for_record(self.RECORD, forge.Credential("none", None), opener=opener)
        self.assertEqual(client.api, API)


if __name__ == "__main__":
    unittest.main()

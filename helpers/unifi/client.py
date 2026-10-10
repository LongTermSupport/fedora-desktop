"""Thin client for the UniFi Network controller's API (classic /api paths).

The password is passed in by the caller (the executor reads it from the environment),
so it is never on a command line. Errors raise UnifiApiError naming the HTTP status and
the controller's message, never the request body.
"""

import http.cookiejar
import json
import ssl
import urllib.error
import urllib.parse
import urllib.request

_LOCAL_HOSTS = frozenset({"localhost", "127.0.0.1", "::1"})


class UnifiApiError(RuntimeError):
    """The controller refused a request or answered with an error."""


def ssl_context_for(url):
    """Verify certificates, except for the controller on this machine.

    The self-hosted controller serves a self-signed certificate on localhost; anything
    reached over the network is verified as usual.
    """
    context = ssl.create_default_context()
    if urllib.parse.urlsplit(url).hostname in _LOCAL_HOSTS:
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
    return context


class UnifiClient:
    def __init__(self, base_url, username, password, site="default"):
        self._base = base_url.rstrip("/")
        self._username = username
        self._password = password
        self._site = site
        self._opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
            urllib.request.HTTPSHandler(context=ssl_context_for(base_url)),
        )

    def _call(self, method, url, body=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(
            url, data=data, method=method, headers={"Content-Type": "application/json"})
        try:
            with self._opener.open(request) as response:
                reply = json.load(response)
        except urllib.error.HTTPError as error:
            detail = error.read().decode(errors="replace")[:300]
            raise UnifiApiError(f"{method} {url}: HTTP {error.code}: {detail}") from None
        meta = reply.get("meta", {})
        if meta.get("rc") != "ok":
            raise UnifiApiError(f"{method} {url}: {meta.get('msg', meta)}")
        return reply.get("data", [])

    def login(self):
        self._call("POST", f"{self._base}/api/login",
                   {"username": self._username, "password": self._password})

    def get(self, path):
        return self._call("GET", f"{self._base}/api/s/{self._site}/{path}")

    def put(self, path, body):
        return self._call("PUT", f"{self._base}/api/s/{self._site}/{path}", body)

    def post(self, path, body):
        return self._call("POST", f"{self._base}/api/s/{self._site}/{path}", body)

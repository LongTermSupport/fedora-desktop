"""Tests for helpers.unifi.client: the thin UniFi controller API client."""

import io
import json
import ssl
import sys
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from helpers.unifi.client import UnifiApiError, UnifiClient, ssl_context_for


class FakeResponse(io.BytesIO):
    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()


def ok(data):
    return FakeResponse(json.dumps({"meta": {"rc": "ok"}, "data": data}).encode())


class ClientTest(unittest.TestCase):
    def make(self, responses):
        client = UnifiClient("https://localhost:8443", "unifi-admin", "pw")
        opener = mock.Mock()
        opener.open.side_effect = responses
        client._opener = opener
        return client, opener

    def request_of(self, opener, index):
        return opener.open.call_args_list[index].args[0]

    def test_login_posts_the_credentials_as_json_body(self):
        client, opener = self.make([ok([])])
        client.login()
        request = self.request_of(opener, 0)
        self.assertEqual(request.full_url, "https://localhost:8443/api/login")
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(json.loads(request.data), {"username": "unifi-admin", "password": "pw"})

    def test_get_returns_the_data_list_for_the_site_path(self):
        client, opener = self.make([ok([{"name": "AP"}])])
        self.assertEqual(client.get("stat/device"), [{"name": "AP"}])
        self.assertEqual(self.request_of(opener, 0).full_url,
                         "https://localhost:8443/api/s/default/stat/device")

    def test_put_sends_json_with_the_put_method(self):
        client, opener = self.make([ok([])])
        client.put("rest/device/1", {"radio_table": []})
        request = self.request_of(opener, 0)
        self.assertEqual(request.get_method(), "PUT")
        self.assertEqual(request.full_url, "https://localhost:8443/api/s/default/rest/device/1")
        self.assertEqual(json.loads(request.data), {"radio_table": []})

    def test_post_sends_json_to_the_site_path(self):
        client, opener = self.make([ok([])])
        client.post("cmd/devmgr", {"cmd": "adopt", "mac": "aa"})
        request = self.request_of(opener, 0)
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(request.full_url, "https://localhost:8443/api/s/default/cmd/devmgr")
        self.assertEqual(json.loads(request.data), {"cmd": "adopt", "mac": "aa"})

    def test_an_error_rc_raises_with_the_controller_message(self):
        reply = FakeResponse(json.dumps({"meta": {"rc": "error", "msg": "api.err.Invalid"}}).encode())
        client, _ = self.make([reply])
        with self.assertRaisesRegex(UnifiApiError, "api.err.Invalid"):
            client.get("stat/device")

    def test_an_http_error_raises_without_the_request_body(self):
        error = urllib.error.HTTPError("u", 400, "Bad Request", {}, io.BytesIO(b'{"meta":{"rc":"error","msg":"api.err.Invalid"}}'))
        client, _ = self.make([error])
        with self.assertRaises(UnifiApiError) as caught:
            client.login()
        self.assertIn("HTTP 400", str(caught.exception))
        self.assertNotIn("pw", str(caught.exception))


class SslContextTest(unittest.TestCase):
    def test_localhost_skips_verification_for_the_self_signed_certificate(self):
        for url in ("https://localhost:8443", "https://127.0.0.1:8443"):
            with self.subTest(url=url):
                context = ssl_context_for(url)
                self.assertEqual(context.verify_mode, ssl.CERT_NONE)
                self.assertFalse(context.check_hostname)

    def test_any_other_host_is_verified(self):
        context = ssl_context_for("https://controller.example.com:8443")
        self.assertEqual(context.verify_mode, ssl.CERT_REQUIRED)
        self.assertTrue(context.check_hostname)


if __name__ == "__main__":
    unittest.main()

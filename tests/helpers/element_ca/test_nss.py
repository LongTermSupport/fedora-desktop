"""Tests for helpers/element_ca/nss.py: the team CA in the stores Element Desktop reads.

Run from the repo root:

    python3 -m unittest tests.helpers.element_ca.test_nss

SPEC-cross-network X34 (Plan 00246): the Element play places the team CA and its last task
makes a TLS handshake from inside Element's sandbox, failing when the CA is not trusted. These
tests run the helper as the play runs it, one process per verb, against a real TLS server on
loopback whose leaf the test's own CA signed, and real NSS databases in a scratch HOME. They
need libnss3, libnspr4, libnssckbi and the openssl command, and fail (never skip) without them.

What they do not prove: that Element's Flatpak build reads these stores. That is the host
check the play's last task is.
"""

from __future__ import annotations

import os
import pathlib
import socket
import ssl
import stat
import subprocess
import sys
import tempfile
import threading
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.element_ca import nss

HELPER = pathlib.Path(nss.__file__).resolve()


def _openssl(*args: str, cwd: pathlib.Path) -> None:
    subprocess.run(["openssl", *args], cwd=cwd, check=True, capture_output=True)


def _make_ca(d: pathlib.Path, name: str) -> None:
    _openssl("req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
             "-keyout", f"{name}.key", "-out", f"{name}.crt", "-days", "30", "-subj", f"/CN={name}",
             "-addext", "basicConstraints=critical,CA:TRUE,pathlen:0",
             "-addext", "keyUsage=critical,keyCertSign", cwd=d)


def _make_leaf(d: pathlib.Path, ca: str) -> None:
    (d / "leaf.ext").write_text("subjectAltName=IP:127.0.0.1\nextendedKeyUsage=serverAuth\n"
                                "basicConstraints=CA:FALSE\nkeyUsage=critical,digitalSignature\n")
    _openssl("req", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
             "-keyout", "leaf.key", "-out", "leaf.csr", "-subj", "/CN=homeserver", cwd=d)
    _openssl("x509", "-req", "-in", "leaf.csr", "-CA", f"{ca}.crt", "-CAkey", f"{ca}.key",
             "-set_serial", "2", "-days", "30", "-extfile", "leaf.ext", "-out", "leaf.crt", cwd=d)


class _Server:
    """A TLS 1.3 server on 127.0.0.1 presenting the leaf: each connection is a handshake."""

    def __init__(self, d: pathlib.Path) -> None:
        self.ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        self.ctx.minimum_version = ssl.TLSVersion.TLSv1_3
        self.ctx.load_cert_chain(d / "leaf.crt", d / "leaf.key")
        self.sock = socket.create_server(("127.0.0.1", 0))
        self.sock.settimeout(0.2)
        self.port = self.sock.getsockname()[1]
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self._serve, daemon=True)
        self.thread.start()

    def _serve(self) -> None:
        while not self.stop.is_set():
            try:
                conn, _ = self.sock.accept()
            except TimeoutError:
                continue
            with conn:
                conn.settimeout(5)
                try:
                    with self.ctx.wrap_socket(conn, server_side=True):
                        pass
                except (ssl.SSLError, OSError):
                    # A client that refuses the certificate ends the handshake; that is the
                    # case under test, not a server fault.
                    continue

    def close(self) -> None:
        self.stop.set()
        self.thread.join()
        self.sock.close()


class ElementCATest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls._tmp = tempfile.TemporaryDirectory()
        cls.certs = pathlib.Path(cls._tmp.name)
        _make_ca(cls.certs, "team-ca")
        _make_ca(cls.certs, "other-ca")
        _make_leaf(cls.certs, "team-ca")
        cls.server = _Server(cls.certs)

    @classmethod
    def tearDownClass(cls) -> None:
        cls.server.close()
        cls._tmp.cleanup()

    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.home = pathlib.Path(tmp.name) / "home"
        self.config = self.home / ".var/app/im.riot.Riot/config"
        self.config.mkdir(parents=True)
        self.store = self.home / ".pki/nssdb"
        self.marker = self.config / "teamchat-ca" / "web.pem"
        self.url = f"https://127.0.0.1:{self.server.port}"

    def run_helper(self, *args: str, stdin: bytes) -> subprocess.CompletedProcess[str]:
        env = {"PATH": os.environ.get("PATH", ""), "HOME": str(self.home), "XDG_CONFIG_HOME": str(self.config)}
        return subprocess.run([sys.executable, "-I", str(HELPER), *args], input=stdin.decode(), env=env,
                              capture_output=True, text=True, check=False, timeout=60)

    def pem(self, name: str) -> bytes:
        return (self.certs / f"{name}.crt").read_bytes()

    def place(self, name: str) -> subprocess.CompletedProcess[str]:
        return self.run_helper("place", "--nssdb", str(self.store), "--nickname", f"teamchat-{name}",
                               stdin=self.pem(name))

    def check(self, url: str | None = None) -> subprocess.CompletedProcess[str]:
        return self.run_helper("check", "--team", "web", "--url", url or self.url, stdin=self.pem("team-ca"))

    def assert_untrusted(self, r: subprocess.CompletedProcess[str]) -> None:
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertEqual(r.stdout, "")
        self.assertIn(str(self.store), r.stderr)
        self.assertFalse(self.marker.exists(), "no record of a CA Element does not trust")

    # X34: a sandbox with no NSS database at all does not trust the team CA.
    def test_no_store_is_not_trusted(self) -> None:
        self.assert_untrusted(self.check())

    # X34: a database that trusts another CA does not trust the team's, and a record a past
    # run left is taken away, so teamchat-admin element stops treating the CA as placed.
    def test_a_store_without_the_team_ca_is_not_trusted_and_its_old_record_goes(self) -> None:
        self.assertEqual(self.place("other-ca").returncode, 0)
        self.marker.parent.mkdir(mode=0o700)
        self.marker.write_bytes(self.pem("team-ca"))
        r = self.check()
        self.assert_untrusted(r)
        self.assertIn("SEC_ERROR_UNKNOWN_ISSUER", r.stderr)

    # X34: placed, the team CA is trusted; the record holds that CA, 0600; both verbs change
    # nothing the second time.
    def test_the_placed_team_ca_is_trusted_and_recorded_once(self) -> None:
        first = self.place("team-ca")
        self.assertEqual((first.returncode, first.stdout), (0, f"CHANGED\t{self.store}\n"), first.stderr)
        again = self.place("team-ca")
        self.assertEqual((again.returncode, again.stdout), (0, ""), again.stderr)
        self.assertEqual(stat.S_IMODE(self.store.stat().st_mode), 0o700)
        r = self.check()
        self.assertEqual((r.returncode, r.stdout), (0, f"CHANGED\t{self.marker}\n"), r.stderr)
        self.assertEqual(ssl.PEM_cert_to_DER_cert(self.marker.read_text()),
                         ssl.PEM_cert_to_DER_cert(self.pem("team-ca").decode()))
        self.assertEqual(stat.S_IMODE(self.marker.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(self.marker.parent.stat().st_mode), 0o700)
        r = self.check()
        self.assertEqual((r.returncode, r.stdout), (0, ""), r.stderr)

    # X16 as Element sees it: the CA is trusted but the certificate is not for the name asked.
    def test_a_certificate_for_another_name_is_not_trusted(self) -> None:
        self.assertEqual(self.place("team-ca").returncode, 0)
        r = self.check(f"https://localhost:{self.server.port}")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("localhost", r.stderr)
        self.assertFalse(self.marker.exists())

    # Nothing listening is not trusted either: the handshake did not succeed.
    def test_no_answer_is_not_trusted(self) -> None:
        with socket.create_server(("127.0.0.1", 0)) as s:
            port = s.getsockname()[1]
        r = self.check(f"https://127.0.0.1:{port}")
        self.assertEqual(r.returncode, 1, r.stderr)
        self.assertIn("handshake", r.stderr)

    # A bad input is exit 2, before anything is reached.
    def test_bad_inputs_are_refused(self) -> None:
        for args, stdin in [
            (["check", "--team", "web", "--url", self.url], b"not a certificate\n"),
            (["check", "--team", "web", "--url", f"http://127.0.0.1:{self.server.port}"], self.pem("team-ca")),
            (["check", "--team", "../web", "--url", self.url], self.pem("team-ca")),
            (["place", "--nssdb", "relative/nssdb", "--nickname", "teamchat-web"], self.pem("team-ca")),
        ]:
            with self.subTest(args=args):
                r = self.run_helper(*args, stdin=stdin)
                self.assertEqual(r.returncode, 2, r.stderr)
                self.assertEqual(r.stdout, "")


class AddressTest(unittest.TestCase):
    def test_the_address_is_the_url_host_and_port(self) -> None:
        self.assertEqual(nss.address("https://192.0.2.10:8448"), ("192.0.2.10", 8448))
        self.assertEqual(nss.address("https://[2001:db8::1]:8448"), ("2001:db8::1", 8448))
        self.assertEqual(nss.address("https://chat.example.org"), ("chat.example.org", 443))
        for bad in ["http://192.0.2.10:8448", "https://", "https://user@host", "https://host/path"]:
            with self.subTest(bad=bad), self.assertRaises(nss.Refused):
                nss.address(bad)


if __name__ == "__main__":
    unittest.main()

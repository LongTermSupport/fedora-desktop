"""The team CA in the stores Element Desktop reads, placed and proven (SPEC-cross-network X34).

Element Desktop is Electron, so Chromium verifies its certificates, and on Linux Chromium reads
trust from NSS: the user database `$HOME/.pki/nssdb` and the builtin roots module it loads,
`libnssckbi.so` (in the Flatpak runtime, p11-kit's trust module, which flatpak connects to the
host's trust). Two verbs, one process each, because NSS initialises once per process:

    place --nssdb DIR --nickname NAME   < the CA certificate, PEM
        On the host, as the desktop user: the CA into the NSS database DIR (made 0700 when
        missing), trusted to issue server certificates ("C,,"), as `certutil -A` does. Prints
        CHANGED<TAB>DIR when it changed anything.

    check --team TEAM --url URL         < the CA certificate, PEM
        Inside Element's sandbox (`flatpak run --command=python3 im.riot.Riot`): a TLS handshake
        to URL's host and port, then the certificate the server presented verified by NSS over
        the stores above, for a TLS server and for URL's host. On success the CA is recorded at
        `$XDG_CONFIG_HOME/teamchat-ca/TEAM.pem` (0600, in a 0700 directory; CHANGED<TAB>path when
        it changed), which is what `teamchat-admin element check|apply` read as placed. On
        failure that record is removed and the cause, naming the store, goes to stderr.

Exit status: 0 done, 1 the CA is not trusted (or the handshake failed), 2 a bad input or an
environment this cannot work in. The check uses NSS's own verifier over the stores Chromium
reads, not Chromium's verifier, so it proves where the trust is, not every rule Chromium adds.
Self-contained and stdlib only: the sandbox runs it as a file, with no repository on its path.
"""

from __future__ import annotations

import argparse
import ctypes
import os
import pathlib
import re
import socket
import ssl
import sys
import tempfile
import urllib.parse
from typing import TextIO

MAX_PEM = 64 << 10
HANDSHAKE_TIMEOUT_S = 15
TEAM_RE = re.compile(r"^[a-z][a-z0-9-]{0,30}$")
NICKNAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
BUILTIN_ROOTS = b'name="Root Certs" library="libnssckbi.so"'
CERT_USAGE_SSL_SERVER = 1
CERTDB_TRUSTED_CA = 1 << 4
SEC_SUCCESS = 0


class Refused(Exception):
    """A bad input or an environment the helper cannot work in: exit 2."""


class Untrusted(Exception):
    """The CA is not trusted for the team's address: exit 1."""


class _SECItem(ctypes.Structure):
    _fields_ = [("type", ctypes.c_int), ("data", ctypes.POINTER(ctypes.c_ubyte)), ("len", ctypes.c_uint)]


class _CERTCertTrust(ctypes.Structure):
    _fields_ = [("sslFlags", ctypes.c_uint), ("emailFlags", ctypes.c_uint), ("objectSigningFlags", ctypes.c_uint)]


class _SECMODModule(ctypes.Structure):
    # The leading members of NSS's SECMODModule, up to `loaded`.
    _fields_ = [("arena", ctypes.c_void_p), ("internal", ctypes.c_int), ("loaded", ctypes.c_int)]


class _NSS:
    """The few NSS calls used, typed. Loading the libraries is the first thing that can fail."""

    def __init__(self) -> None:
        try:
            self.nspr = ctypes.CDLL("libnspr4.so")
            self.lib = ctypes.CDLL("libnss3.so")
        except OSError as exc:
            raise Refused(f"NSS is not loadable here ({exc})") from None
        p, i = ctypes.c_void_p, ctypes.c_int
        item, trust = ctypes.POINTER(_SECItem), ctypes.POINTER(_CERTCertTrust)
        for name, res, args in [
            ("NSS_Init", i, [ctypes.c_char_p]),
            ("NSS_InitReadWrite", i, [ctypes.c_char_p]),
            ("NSS_NoDB_Init", i, [ctypes.c_char_p]),
            ("NSS_Shutdown", i, []),
            ("SECMOD_LoadUserModule", ctypes.POINTER(_SECMODModule), [ctypes.c_char_p, p, i]),
            ("CERT_GetDefaultCertDB", p, []),
            ("CERT_NewTempCertificate", p, [p, item, ctypes.c_char_p, i, i]),
            ("CERT_FindCertByDERCert", p, [p, item]),
            ("CERT_DestroyCertificate", None, [p]),
            ("CERT_VerifyCertNow", i, [p, p, i, i, p]),
            ("CERT_VerifyCertName", i, [p, ctypes.c_char_p]),
            ("CERT_GetCertTrust", i, [p, trust]),
            ("CERT_DecodeTrustString", i, [trust, ctypes.c_char_p]),
            ("CERT_ChangeCertTrust", i, [p, p, trust]),
            ("PK11_GetInternalKeySlot", p, []),
            ("PK11_FreeSlot", None, [p]),
            ("PK11_NeedUserInit", i, [p]),
            ("PK11_InitPin", i, [p, ctypes.c_char_p, ctypes.c_char_p]),
            ("PK11_Authenticate", i, [p, i, p]),
            ("PK11_ImportCert", i, [p, p, ctypes.c_ulong, ctypes.c_char_p, i]),
        ]:
            fn = getattr(self.lib, name)
            fn.restype, fn.argtypes = res, args
        self.nspr.PR_GetError.restype, self.nspr.PR_GetError.argtypes = i, []
        self.nspr.PR_ErrorToName.restype, self.nspr.PR_ErrorToName.argtypes = ctypes.c_char_p, [i]
        self._keep: list[object] = []

    def error(self) -> str:
        """The last NSS error, by name when NSS has one for it."""
        code = self.nspr.PR_GetError()
        name = self.nspr.PR_ErrorToName(code)
        return name.decode() if name else f"NSS error {code}"

    def item(self, der: bytes) -> _SECItem:
        buf = (ctypes.c_ubyte * len(der)).from_buffer_copy(der)
        self._keep.append(buf)
        return _SECItem(0, ctypes.cast(buf, ctypes.POINTER(ctypes.c_ubyte)), len(der))


def address(url: str) -> tuple[str, int]:
    """The host and port a member reaches for an https url with nothing else in it."""
    parts = urllib.parse.urlsplit(url)
    try:
        port = parts.port or 443
    except ValueError:
        raise Refused(f"the url {url!r} has no valid port") from None
    if (parts.scheme != "https" or not parts.hostname or "@" in parts.netloc
            or parts.path not in ("", "/") or parts.query or parts.fragment):
        raise Refused(f"the url {url!r} is not https://<host>[:port]")
    return parts.hostname, port


def read_ca(stream: TextIO) -> bytes:
    """The CA certificate on stdin, one PEM block, as DER."""
    data = stream.read(MAX_PEM + 1)
    if len(data) > MAX_PEM:
        raise Refused(f"the CA on stdin is over {MAX_PEM} bytes")
    try:
        return ssl.PEM_cert_to_DER_cert(data)
    except ValueError:
        raise Refused("stdin is not one PEM certificate") from None


def handshake(host: str, port: int) -> bytes:
    """The certificate the server presents, from a TLS 1.3 handshake that verifies nothing."""
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_3
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    try:
        with socket.create_connection((host, port), timeout=HANDSHAKE_TIMEOUT_S) as raw, \
                ctx.wrap_socket(raw, server_hostname=host) as tls:
            der = tls.getpeercert(binary_form=True)
    except (OSError, ssl.SSLError) as exc:
        raise Untrusted(f"the TLS handshake with {host} port {port} failed: {exc}") from None
    if not der:
        raise Untrusted(f"{host} port {port} presented no certificate")
    return der


def check(nss: _NSS, team: str, url: str, ca_der: bytes) -> str | None:
    """Verify url's certificate over the stores Chromium reads; the record path when written."""
    host, port = address(url)
    home, config = os.environ.get("HOME", ""), os.environ.get("XDG_CONFIG_HOME", "")
    if not os.path.isabs(home) or not os.path.isabs(config):
        raise Refused("HOME and XDG_CONFIG_HOME must be absolute paths (run inside Element's sandbox)")
    store = pathlib.Path(home) / ".pki/nssdb"
    record = pathlib.Path(config) / "teamchat-ca" / f"{team}.pem"
    try:
        verify(nss, store, host, handshake(host, port))
    except Untrusted:
        record.unlink(missing_ok=True)
        raise
    return write_record(record, ssl.DER_cert_to_PEM_cert(ca_der).encode())


def verify(nss: _NSS, store: pathlib.Path, host: str, leaf_der: bytes) -> None:
    """NSS over the user database (none, when Chromium has made none yet) and the builtin roots."""
    if (store / "cert9.db").is_file():
        ok = nss.lib.NSS_Init(b"sql:" + str(store).encode()) == SEC_SUCCESS
    else:
        ok = nss.lib.NSS_NoDB_Init(None) == SEC_SUCCESS
    if not ok:
        raise Refused(f"NSS could not open {store}: {nss.error()}")
    module = nss.lib.SECMOD_LoadUserModule(BUILTIN_ROOTS, None, 0)
    if not module or not module.contents.loaded:
        raise Refused(f"NSS could not load the builtin roots module libnssckbi.so: {nss.error()}")
    db = nss.lib.CERT_GetDefaultCertDB()
    leaf = nss.lib.CERT_NewTempCertificate(db, ctypes.byref(nss.item(leaf_der)), None, 0, 1)
    if not leaf:
        raise Refused(f"the certificate {host} presented is not one NSS reads: {nss.error()}")
    if nss.lib.CERT_VerifyCertNow(db, leaf, 1, CERT_USAGE_SSL_SERVER, None) != SEC_SUCCESS:
        raise Untrusted(f"the certificate {host} presented is not trusted by the NSS store {store} "
                        f"with the builtin roots: {nss.error()}")
    if nss.lib.CERT_VerifyCertName(leaf, host.encode()) != SEC_SUCCESS:
        raise Untrusted(f"the certificate {host} presented is not for {host} (NSS store {store}): {nss.error()}")
    # Read-only and the process ends next, so NSS is not shut down: the module and the
    # certificate hold references that would make NSS_Shutdown report the store busy.


def write_record(record: pathlib.Path, pem: bytes) -> str | None:
    """pem at record, 0600 in a 0700 directory, only on change; the path when written."""
    record.parent.mkdir(mode=0o700, exist_ok=True)
    os.chmod(record.parent, 0o700)
    if not record.is_symlink() and record.is_file() and record.read_bytes() == pem \
            and (record.stat().st_mode & 0o777) == 0o600:
        return None
    fd, tmp = tempfile.mkstemp(dir=record.parent, prefix=".record-")
    try:
        with os.fdopen(fd, "wb") as f:
            f.write(pem)
        os.chmod(tmp, 0o600)
        os.replace(tmp, record)
    except BaseException:
        pathlib.Path(tmp).unlink(missing_ok=True)
        raise
    return str(record)


def place(nss: _NSS, store: pathlib.Path, nickname: str, ca_der: bytes) -> bool:
    """The CA into store, trusted to issue server certificates; whether anything changed."""
    store.mkdir(mode=0o700, parents=True, exist_ok=True)
    if nss.lib.NSS_InitReadWrite(b"sql:" + str(store).encode()) != SEC_SUCCESS:
        raise Refused(f"NSS could not open {store} to write: {nss.error()}")
    db = nss.lib.CERT_GetDefaultCertDB()
    item = ctypes.byref(nss.item(ca_der))
    trust = _CERTCertTrust()
    cert = nss.lib.CERT_FindCertByDERCert(db, item)
    changed = not (cert and nss.lib.CERT_GetCertTrust(cert, ctypes.byref(trust)) == SEC_SUCCESS
                   and trust.sslFlags & CERTDB_TRUSTED_CA)
    if changed:
        slot = nss.lib.PK11_GetInternalKeySlot()
        try:
            login(nss, slot, store)
            if not cert:
                cert = import_cert(nss, db, slot, item, nickname, store)
            if nss.lib.CERT_DecodeTrustString(ctypes.byref(trust), b"C,,") != SEC_SUCCESS \
                    or nss.lib.CERT_ChangeCertTrust(db, cert, ctypes.byref(trust)) != SEC_SUCCESS:
                raise Refused(f"NSS could not trust the CA in {store}: {nss.error()}")
        finally:
            nss.lib.PK11_FreeSlot(slot)
    nss.lib.CERT_DestroyCertificate(cert)
    if nss.lib.NSS_Shutdown() != SEC_SUCCESS:
        raise Refused(f"NSS could not close {store}: {nss.error()}")
    return changed


def login(nss: _NSS, slot: int, store: pathlib.Path) -> None:
    """The internal slot made writable: a new database gets the empty password Chromium's has,
    and one with a password set is refused (the play has no password to give)."""
    if nss.lib.PK11_NeedUserInit(slot) and nss.lib.PK11_InitPin(slot, None, b"") != SEC_SUCCESS:
        raise Refused(f"NSS could not initialise {store}: {nss.error()}")
    if nss.lib.PK11_Authenticate(slot, 1, None) != SEC_SUCCESS:
        raise Refused(f"{store} has a password set, so the CA cannot be added to it: {nss.error()}")


def import_cert(nss: _NSS, db: int, slot: int, item: object, nickname: str, store: pathlib.Path) -> int:
    """The CA added to store's internal slot under nickname; the certificate handle."""
    cert = nss.lib.CERT_NewTempCertificate(db, item, nickname.encode(), 0, 1)
    if not cert:
        raise Refused(f"the CA is not a certificate NSS reads: {nss.error()}")
    if nss.lib.PK11_ImportCert(slot, cert, 0, nickname.encode(), 0) != SEC_SUCCESS:
        raise Refused(f"NSS could not add the CA to {store}: {nss.error()}")
    return cert


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="nss.py", description=__doc__.splitlines()[0])
    verbs = parser.add_subparsers(dest="verb", required=True)
    p = verbs.add_parser("place")
    p.add_argument("--nssdb", required=True)
    p.add_argument("--nickname", required=True)
    c = verbs.add_parser("check")
    c.add_argument("--team", required=True)
    c.add_argument("--url", required=True)
    args = parser.parse_args(argv)
    try:
        ca_der = read_ca(sys.stdin)
        if args.verb == "place":
            store = pathlib.Path(args.nssdb)
            if not store.is_absolute() or NICKNAME_RE.fullmatch(args.nickname) is None:
                raise Refused("--nssdb must be an absolute path and --nickname a plain name")
            if place(_NSS(), store, args.nickname, ca_der):
                print(f"CHANGED\t{store}")
            return 0
        if TEAM_RE.fullmatch(args.team) is None:
            raise Refused("--team is not a team id")
        address(args.url)
        written = check(_NSS(), args.team, args.url, ca_der)
        if written:
            print(f"CHANGED\t{written}")
        return 0
    except Untrusted as exc:
        print(f"element-ca: {exc}", file=sys.stderr)
        return 1
    except Refused as exc:
        print(f"element-ca: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

"""One activation of the container-to-host bridge (Plan 00110, DESIGN.md §6.4).

The thin executor the path unit starts. It drains every request in the spool
in one activation, answers each one — `accepted` before dispatch, `rejected`
with the code of the check that refused it — and dispatches an accepted
request as a `systemd-run --user --scope` that outlives this oneshot. It never
exits non-zero for bad INPUT (that would let a sandbox loop wedge the unit);
it exits non-zero only when the spool itself cannot be trusted, and then it
writes nothing into it.

    python3 -m helpers.vmtest.bridge_watcher --checkout DIR --slug SLUG \\
        --config-dir ~/.config/vmtest-bridge/<slug> --state-dir ~/.local/state/vmtest-bridge/<slug> \\
        --allowlist ~/.local/share/vmtest/scenarios.allowlist [--dispatcher /usr/bin/systemd-run]

Validation order, per request (§6.4):
   1. pin the spool root and every directory (spool.py)     -> else REFUSE: audit log, exit 2
   2. filename grammar                                       -> else quarantine + rejected response
   3. read the body ONCE
   4. hardcoded deny list, 5. filename verb == body verb, 6. verb set, all in parse_request
   7. argument in the DEPLOYED enumeration
   8. watcher-side rate limit, from the off-mount audit log  -> rejected "rate-limited", exit 0
   9. policy MODE_<verb> == auto (missing/unknown/unreadable -> deny)
  10. single-flight lock
  11. renameat requests/ -> processing/
  12. write the signed accepted response BEFORE dispatch
  13. argv from the buffer: a hardcoded list, no shell
  14. dispatch
"""

from __future__ import annotations

import argparse
import os
import pathlib
import re
import subprocess
import sys
import time

from helpers.vmtest import spool, verdict

RATE_LIMIT_WINDOW_SECONDS = 60
RATE_LIMIT_MAX = 10
REFRESH_BASE_ARGUMENTS = frozenset({"server", "desktop", "all"})
POLICY_LINE_RE = re.compile(r"^MODE_([a-z-]+)=(auto|deny)$")


class Refusal(RuntimeError):
    """The spool or the host-side configuration cannot be trusted; nothing is written into the spool."""


class Audit:
    """The off-mount audit log: the verdict of record and the rate limiter's memory."""

    def __init__(self, path: pathlib.Path) -> None:
        self.path = path

    def record(self, now: int, event: str, request: str, detail: str = "") -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(f"{now} {event} {request} {detail}".rstrip() + "\n")

    def answered_since(self, since: int) -> int:
        if not self.path.exists():
            return 0
        count = 0
        for line in self.path.read_text(encoding="utf-8").splitlines():
            fields = line.split(" ", 2)
            if len(fields) >= 2 and fields[0].isdigit() and int(fields[0]) >= since and fields[1] in ("accepted", "rejected"):
                count += 1
        return count


def read_policy(config_dir: pathlib.Path) -> dict[str, str]:
    """`MODE_<verb>=auto|deny` lines; anything unreadable or malformed contributes nothing (= deny)."""
    path = config_dir / "policy"
    modes: dict[str, str] = {}
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return modes
    for line in text.splitlines():
        match = POLICY_LINE_RE.match(line.strip())
        if match:
            modes[match.group(1)] = match.group(2)
    return modes


def read_key(config_dir: pathlib.Path) -> bytes:
    path = config_dir / "response.key"
    try:
        key = path.read_bytes()
    except OSError as exc:
        raise Refusal(f"no signing key at {path}: {exc}") from exc
    if len(key) < verdict.MIN_KEY_BYTES:
        raise Refusal(f"signing key at {path} is shorter than {verdict.MIN_KEY_BYTES} bytes")
    return key


def read_allowlist(path: pathlib.Path) -> list[str] | None:
    try:
        return [line.strip() for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
    except OSError:
        return None


def enumeration_check(request: spool.Request, allowlist: list[str] | None) -> None:
    if request.verb == "run-scenario":
        if allowlist is None:
            raise spool.RequestRejected(
                "allowlist-stale",
                "no deployed scenarios.allowlist on the host; re-run play-vm-test-lab.yml on the host",
            )
        if request.argument not in allowlist:
            raise spool.RequestRejected(
                "unknown-argument",
                f"{request.argument!r} is not in the deployed allowlist; if the repo declares it, "
                "re-run play-vm-test-lab.yml on the host",
            )
    elif request.verb == "refresh-base" and request.argument not in REFRESH_BASE_ARGUMENTS:
        raise spool.RequestRejected(
            "unknown-argument", f"refresh-base takes one of {sorted(REFRESH_BASE_ARGUMENTS)}"
        )


def build_argv(request: spool.Request, slug: str, checkout: str) -> list[str]:
    """A hardcoded array from the buffer's fields; never a shell, never a path from the request."""
    unit = f"vmtest-bridge-run-{slug}-{request.nonce}"
    argv = [
        "--user",
        "--scope",
        "--quiet",
        "--collect",
        "--unit",
        unit,
        sys.executable,
        "-m",
        "helpers.vmtest.bridge_run",
        "--checkout",
        checkout,
        "--slug",
        slug,
        "--request",
        request.name,
        "--verb",
        request.verb,
    ]
    if request.argument is not None:
        argv += ["--argument", request.argument]
    return argv


class Activation:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.now = args.now if args.now is not None else int(time.time())
        self.audit = Audit(pathlib.Path(args.state_dir) / "service.log")
        self.key = read_key(pathlib.Path(args.config_dir))
        self.policy = read_policy(pathlib.Path(args.config_dir))
        self.allowlist = read_allowlist(pathlib.Path(args.allowlist))
        self.in_flight = pathlib.Path(args.state_dir) / "in-flight"

    def respond(self, responses_fd: int, request_name: str, document: dict, nonce: str) -> None:
        spool.write_file(responses_fd, f"{request_name}.response.json", verdict.sign(document, self.key, nonce=nonce).encode())

    def reject(self, fds: dict, name: str, code: str, reason: str) -> None:
        nonce_match = spool.REQUEST_NAME_RE.match(name)
        nonce = nonce_match.group(3) if nonce_match else "0" * 16
        self.respond(fds["responses"], name, verdict.rejected(name, code=code, reason=reason, now=self.now), nonce)
        spool.quarantine(fds["requests"], name, fds["quarantine"])
        self.audit.record(self.now, "rejected", name, code)

    def handle(self, fds: dict, name: str) -> bool:
        """Answer one request; return True if it was dispatched."""
        try:
            body = spool.read_request(fds["requests"], name)
        except spool.SpoolMalformed as exc:
            self.reject(fds, name, "bad-body", str(exc))
            return False
        try:
            request = spool.parse_request(name, body)
            enumeration_check(request, self.allowlist)
            since = self.now - RATE_LIMIT_WINDOW_SECONDS
            if self.audit.answered_since(since) >= RATE_LIMIT_MAX:
                raise spool.RequestRejected(
                    "rate-limited", f"more than {RATE_LIMIT_MAX} requests answered in {RATE_LIMIT_WINDOW_SECONDS}s"
                )
            if self.policy.get(request.verb) != "auto":
                raise spool.RequestRejected(
                    "policy-deny", f"MODE_{request.verb} is not 'auto' in the host policy file (missing means deny)"
                )
            if self.in_flight.exists():
                raise spool.RequestRejected(
                    "in-flight", f"a run is in flight ({self.in_flight.read_text(encoding='utf-8').strip()}); one at a time"
                )
        except spool.RequestRejected as exc:
            self.reject(fds, name, exc.code, exc.reason)
            return False

        run_id = f"{request.timestamp}-{request.argument or request.verb}"
        spool.claim(fds["requests"], name, fds["processing"])
        self.in_flight.write_text(f"{run_id}\n", encoding="utf-8")
        self.respond(fds["responses"], name, verdict.accepted(request, run_id=run_id, now=self.now), request.nonce)
        self.audit.record(self.now, "accepted", name, run_id)
        argv = [self.args.dispatcher, *build_argv(request, self.args.slug, self.args.checkout)]
        subprocess.run(argv, check=True)
        self.audit.record(self.now, "dispatched", name, argv[6] if len(argv) > 6 else "")
        return True

    def run(self) -> int:
        root_fd = spool.open_root(self.args.checkout)
        fds = {"root": root_fd}
        try:
            for name in ("requests", "processing", "responses", "quarantine"):
                fds[name] = spool.open_subdir(root_fd, name)
            listing = spool.list_requests(fds["requests"])
            for name in listing.malformed:
                try:
                    self.reject(fds, name, "bad-filename", "request file name does not match the grammar")
                except spool.SpoolRefusal as exc:
                    self.audit.record(self.now, "refused", name, str(exc))
                    raise
            for name in listing.valid:
                self.handle(fds, name)
        finally:
            for fd in fds.values():
                os.close(fd)
        if self.args.debounce_seconds > 0:
            time.sleep(self.args.debounce_seconds)
        return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--checkout", required=True, help="absolute path of the checkout whose spool this is")
    parser.add_argument("--slug", required=True, help="systemd-escaped checkout path; names the units and the state")
    parser.add_argument("--config-dir", required=True, help="holds policy and response.key; host-only, off the mount")
    parser.add_argument("--state-dir", required=True, help="holds service.log and the in-flight lock; off the mount")
    parser.add_argument("--allowlist", required=True, help="the DEPLOYED scenarios.allowlist")
    parser.add_argument("--dispatcher", default="/usr/bin/systemd-run", help="what to hand the hardcoded argv to")
    parser.add_argument("--now", type=int, default=None)
    parser.add_argument("--debounce-seconds", type=float, default=2.0, help="sleep after draining, so a write loop is absorbed")
    args = parser.parse_args(argv)

    audit = Audit(pathlib.Path(args.state_dir) / "service.log")
    try:
        return Activation(args).run()
    except (Refusal, spool.SpoolRefusal) as exc:
        audit.record(int(time.time()), "refused", "-", f"{exc}")
        print(f"REFUSED: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())

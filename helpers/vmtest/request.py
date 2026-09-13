"""The container-side requester and reader of the bridge (Plan 00110, §6.5, §6.6).

    python3 -m helpers.vmtest.request [--checkout DIR] VERB [ARGUMENT]

Runs INSIDE the sandbox, against the spool it shares with the host:

1. read `diagnostics/bridge-heartbeat.json` FIRST — a stale or absent heartbeat
   means the bridge is not running, a failed unit means it is wedged; each is
   its own exit code with the remedy, never a timeout and never a `fail`
2. write the request atomically: `tmp/<nonce>` then rename into `requests/`
3. poll for `responses/<request>.response.json` and classify it:
   no response by --accept-timeout  -> unknown (the watcher never answered)
   rejected                         -> the refusing check's code
   accepted / running               -> keep waiting while its heartbeat is fresh;
                                       a stale one means the host process died
   finished                         -> pass | fail | error (with the stage)

Exit 0 ONLY on `state == finished and verdict == pass`. Every other outcome is
a distinct non-zero code (the EXIT_* constants) with a distinct reason.

This reader does NOT verify the response signature and does not pretend to:
the key is host-only by construction. It says so, prints the `vmtest verify`
line a human runs on the host, and the off-mount audit log that is the
verdict of record. The one marker line on stdout is:

    VMTEST-REQUEST <request> state=<state> verdict=<verdict|-> stage=<stage|-> run_id=<id|->
"""

from __future__ import annotations

import argparse
import datetime
import json
import os
import pathlib
import secrets
import sys
import time
from dataclasses import dataclass

from helpers.vmtest import spool, verdict

EXIT_PASS = 0
EXIT_FAIL = 1
EXIT_ERROR = 2
EXIT_REJECTED = 3
EXIT_UNKNOWN = 4
EXIT_BRIDGE_DOWN = 5
EXIT_BRIDGE_WEDGED = 6
EXIT_DIED = 7
EXIT_MALFORMED = 8
EXIT_USAGE = 64

BRIDGE_HEARTBEAT_MAX_AGE = 180  # the timer writes every minute
RUN_HEARTBEAT_MAX_AGE = 300  # the run scope refreshes every minute
HEARTBEAT_FILE = "bridge-heartbeat.json"


class Usage(ValueError):
    """A request that could never be valid; nothing is written."""


@dataclass(frozen=True)
class Outcome:
    kind: str  # pass | fail | error | rejected | waiting | died | malformed
    exit_code: int | None
    reason: str


def fresh_nonce() -> str:
    return secrets.token_hex(8)


def build_request(verb: str, argument: str | None, *, now: int, nonce: str) -> tuple[str, bytes]:
    """The file name and body of a request, validated by the same rules the host applies."""
    timestamp = datetime.datetime.fromtimestamp(now, tz=datetime.UTC).strftime("%Y%m%dT%H%M%SZ")
    name = f"{timestamp}-{verb}-{nonce}.json"
    body = json.dumps({"verb": verb, "argument": argument, "nonce": nonce}, sort_keys=True).encode("utf-8")
    try:
        spool.parse_request(name, body)
    except spool.RequestRejected as exc:
        raise Usage(f"{exc.code}: {exc.reason}") from exc
    return name, body


def _age(document: dict, field: str, now: int) -> int | None:
    stamp = verdict._parse_iso(document.get(field))
    return None if stamp is None else now - stamp


def classify(response: dict, *, now: int, heartbeat_max_age: int) -> Outcome:
    state = response.get("state")
    if state == verdict.STATE_REJECTED:
        failure = response.get("failure") or {}
        return Outcome("rejected", EXIT_REJECTED, f"rejected before dispatch: {failure.get('reason', 'no reason recorded')}")
    if state in (verdict.STATE_ACCEPTED, verdict.STATE_RUNNING):
        age = _age(response, "heartbeat_at", now)
        if age is None:
            return Outcome("malformed", EXIT_MALFORMED, f"response in state {state} has no readable heartbeat_at")
        if age > heartbeat_max_age:
            return Outcome("died", EXIT_DIED, f"response is {state} but its heartbeat is {age}s old (limit {heartbeat_max_age}s): the host run process died")
        return Outcome("waiting", None, f"{state}; heartbeat {age}s ago")
    if state == verdict.STATE_FINISHED:
        result = response.get("verdict")
        if result not in verdict.VERDICTS or not isinstance(response.get("run_id"), str) or not isinstance(response.get("checks"), dict):
            return Outcome("malformed", EXIT_MALFORMED, f"finished response is malformed (verdict={result!r})")
        if result == "pass":
            return Outcome("pass", EXIT_PASS, "finished: pass")
        failure = response.get("failure") or {}
        stage = failure.get("stage", "-")
        reason = failure.get("reason", "no reason recorded")
        if result == "fail":
            return Outcome("fail", EXIT_FAIL, f"finished: fail at {stage}: {reason}")
        return Outcome("error", EXIT_ERROR, f"finished: error at {stage}: {reason}")
    return Outcome("malformed", EXIT_MALFORMED, f"response has unknown state {state!r}")


def read_json(path: pathlib.Path) -> dict | None:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return None
    except (OSError, json.JSONDecodeError):
        return {}
    return document if isinstance(document, dict) else {}


def marker(name: str, state: str, response: dict | None) -> str:
    response = response or {}
    failure = response.get("failure") or {}
    return (
        f"VMTEST-REQUEST {name} state={state} verdict={response.get('verdict') or '-'} "
        f"stage={failure.get('stage') or '-'} run_id={response.get('run_id') or '-'}"
    )


def say(text: str) -> None:
    print(text, file=sys.stderr)


def provenance(heartbeat: dict | None, response: dict | None) -> None:
    """What this reader cannot vouch for, and where a human can (§6.6 rule 11)."""
    say("signature: present, not verifiable from inside the sandbox (the key is host-only)")
    run_id = (response or {}).get("run_id")
    if run_id:
        say(f"verify on the host: vmtest verify {run_id}")
    audit = (heartbeat or {}).get("audit_log") or "~/.local/state/vmtest-bridge/<slug>/service.log"
    say(f"verdict of record (host-only audit log): {audit}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("verb", help=f"one of {', '.join(sorted(spool.VERBS))}; judged by the host's own rules, not argparse")
    parser.add_argument("argument", nargs="?", default=None)
    parser.add_argument("--checkout", default=os.getcwd(), help="the checkout whose spool to use (default: cwd)")
    parser.add_argument("--timeout", type=float, default=7200.0, help="seconds to wait for a finished response")
    parser.add_argument("--accept-timeout", type=float, default=60.0, help="seconds to wait for ANY response")
    parser.add_argument("--poll-seconds", type=float, default=5.0)
    parser.add_argument("--heartbeat-max-age", type=int, default=BRIDGE_HEARTBEAT_MAX_AGE)
    parser.add_argument("--run-heartbeat-max-age", type=int, default=RUN_HEARTBEAT_MAX_AGE)
    args = parser.parse_args(argv)

    spool_dir = pathlib.Path(args.checkout) / "untracked" / "vmtest-bridge"
    now = int(time.time())

    # 1. the heartbeat, before anything is written
    heartbeat = read_json(spool_dir / "diagnostics" / HEARTBEAT_FILE)
    assessment = verdict.assess_heartbeat(heartbeat, now=now, max_age=args.heartbeat_max_age)
    if assessment.state == "wedged":
        say(f"bridge wedged: {assessment.reason}")
        print(marker("-", "bridge-wedged", None))
        return EXIT_BRIDGE_WEDGED
    if assessment.state != "ok":
        say(f"bridge {assessment.state}: {assessment.reason}")
        print(marker("-", f"bridge-{assessment.state}", None))
        return EXIT_BRIDGE_DOWN

    # 2. the request, validated by the host's own rules before it exists
    try:
        name, body = build_request(args.verb, args.argument, now=now, nonce=fresh_nonce())
    except Usage as exc:
        say(f"usage: {exc}")
        return EXIT_USAGE
    staging = spool_dir / "tmp" / name
    staging.write_bytes(body)
    os.rename(staging, spool_dir / "requests" / name)
    say(f"request written: {name}")

    # 3. the response
    response_path = spool_dir / "responses" / f"{name}.response.json"
    started = time.monotonic()
    last_reported = ""
    response: dict | None = None
    while True:
        elapsed = time.monotonic() - started
        response = read_json(response_path)
        if response is None:
            if elapsed > args.accept_timeout:
                say(f"unknown: no response after {int(elapsed)}s; the watcher never answered (is the path unit active on the host?)")
                provenance(heartbeat, None)
                print(marker(name, "unknown", None))
                return EXIT_UNKNOWN
        else:
            outcome = classify(response, now=int(time.time()), heartbeat_max_age=args.run_heartbeat_max_age)
            if outcome.kind != "waiting":
                say(outcome.reason)
                checks = response.get("checks") or {}
                if outcome.kind in ("pass", "fail", "error"):
                    say(f"checks: planned={checks.get('planned')} total={checks.get('total')} passed={checks.get('passed')} failed={checks.get('failed')} skipped={checks.get('skipped')}")
                    transcript = (response.get("evidence") or {}).get("transcript")
                    if transcript:
                        say(f"transcript: {transcript}")
                provenance(heartbeat, response)
                print(marker(name, response.get("state", "malformed"), response))
                return outcome.exit_code if outcome.exit_code is not None else EXIT_MALFORMED
            if outcome.reason != last_reported:
                say(outcome.reason)
                last_reported = outcome.reason
        if elapsed > args.timeout:
            say(f"timeout: no finished response after {int(elapsed)}s; the run may still be in flight on the host")
            provenance(heartbeat, response)
            print(marker(name, (response or {}).get("state", "unknown"), response))
            return EXIT_UNKNOWN
        time.sleep(args.poll_seconds)


if __name__ == "__main__":
    sys.exit(main())

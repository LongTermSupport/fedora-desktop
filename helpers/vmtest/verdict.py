"""The response state machine, HMAC signing and the heartbeat (Plan 00110, §6.5, §6.6).

Pure. The watcher writes `accepted` BEFORE dispatch and `rejected` for every
refusal, the run scope moves `accepted` → `running` (refreshing the heartbeat)
→ `finished`, and the heartbeat timer writes what `heartbeat()` returns. The
container-side reader uses `assess_heartbeat` to tell "busy" from "the bridge
is dead" before it writes a request.

Rules carried in code rather than prose:

- `verdict` is null until `state == "finished"`; a naive reader gets a falsy
  value (§6.6 rule 01).
- transitions only move forward; a terminal document cannot be re-entered.
- every response is HMAC-signed with a host-only key, and the request nonce is
  INSIDE the signed payload, so a previous run's response cannot be replayed
  (§6.6 rule 11). Verification needs the key and therefore happens on the host.
"""

from __future__ import annotations

import datetime
import hashlib
import hmac
import json
from dataclasses import dataclass

from helpers.vmtest import spool

SCHEMA = 1
SIGNATURE_ALG = "hmac-sha256"
MIN_KEY_BYTES = 32

STATE_ACCEPTED = "accepted"
STATE_RUNNING = "running"
STATE_FINISHED = "finished"
STATE_REJECTED = "rejected"
VERDICTS = frozenset({"pass", "fail", "error"})
STAGES = frozenset(
    {"freshness", "allowlist", "base", "clone", "boot", "ssh", "provision", "assert", "collect", "aborted"}
)

# Every rejection the watcher can issue (§6.4 steps 2-10) is an allowlist-stage
# refusal: the request never reached the lab.
REJECTION_CODES = frozenset(
    {
        "bad-filename",
        "denylisted-verb",
        "unknown-verb",
        "verb-mismatch",
        "nonce-mismatch",
        "bad-argument",
        "bad-body",
        "unknown-argument",
        "allowlist-stale",
        "policy-deny",
        "rate-limited",
        "in-flight",
    }
)

HEARTBEAT_STATES = ("ok", "stale", "wedged", "absent")


class SignatureError(ValueError):
    """The document is not signed by this key, or is not a signed document at all."""


@dataclass(frozen=True)
class HeartbeatAssessment:
    state: str
    reason: str


def _iso(epoch: int) -> str:
    return datetime.datetime.fromtimestamp(epoch, tz=datetime.UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def _parse_iso(text: object) -> int | None:
    if not isinstance(text, str):
        return None
    try:
        return int(datetime.datetime.strptime(text, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=datetime.UTC).timestamp())
    except ValueError:
        return None


def _canonical(document: dict) -> bytes:
    return json.dumps(document, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")


# ── signing ───────────────────────────────────────────────────────────────────────────────


def sign(document: dict, key: bytes, *, nonce: str) -> str:
    """Return the document as JSON text with a `signature` block covering body + nonce."""
    if len(key) < MIN_KEY_BYTES:
        raise SignatureError(f"signing key must be at least {MIN_KEY_BYTES} bytes")
    body = {k: v for k, v in document.items() if k != "signature"}
    payload = _canonical({"body": body, "nonce": nonce})
    value = hmac.new(key, payload, hashlib.sha256).hexdigest()
    signed = dict(body)
    signed["signature"] = {"alg": SIGNATURE_ALG, "nonce": nonce, "value": value}
    return json.dumps(signed, indent=2, sort_keys=True) + "\n"


def verify(text: str, key: bytes) -> dict:
    """Return the document if its signature checks out under `key`; raise otherwise."""
    try:
        document = json.loads(text)
    except json.JSONDecodeError as exc:
        raise SignatureError(f"not JSON: {exc}") from exc
    if not isinstance(document, dict):
        raise SignatureError("a signed response is a JSON object")
    signature = document.get("signature")
    if (
        not isinstance(signature, dict)
        or signature.get("alg") != SIGNATURE_ALG
        or not isinstance(signature.get("nonce"), str)
        or not isinstance(signature.get("value"), str)
    ):
        raise SignatureError("missing or malformed signature block")
    body = {k: v for k, v in document.items() if k != "signature"}
    payload = _canonical({"body": body, "nonce": signature["nonce"]})
    expected = hmac.new(key, payload, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(expected, signature["value"]):
        raise SignatureError("signature does not verify under this key")
    return document


# ── the state machine ─────────────────────────────────────────────────────────────────────


def _empty_checks(planned: int | None) -> dict:
    return {"planned": planned, "total": None, "passed": None, "failed": None, "skipped": None}


def accepted(request: spool.Request, *, run_id: str | None, now: int, planned: int | None = None) -> dict:
    """The stub written BEFORE dispatch (§6.4 step 12): silence is never an outcome."""
    return {
        "schema": SCHEMA,
        "request": request.name,
        "verb": request.verb,
        "argument": request.argument,
        "state": STATE_ACCEPTED,
        "verdict": None,
        "run_id": run_id,
        "accepted_at": _iso(now),
        "started_at": None,
        "heartbeat_at": _iso(now),
        "finished_at": None,
        "checks": _empty_checks(planned),
        "failure": None,
    }


def rejected(request_name: str, *, code: str, reason: str, now: int) -> dict:
    """A terminal response for a refused request; the code names which check refused it."""
    if code not in REJECTION_CODES:
        raise ValueError(f"unknown rejection code {code!r}")
    return {
        "schema": SCHEMA,
        "request": request_name,
        "verb": None,
        "argument": None,
        "state": STATE_REJECTED,
        "verdict": None,
        "run_id": None,
        "accepted_at": None,
        "started_at": None,
        "heartbeat_at": _iso(now),
        "finished_at": _iso(now),
        "checks": _empty_checks(None),
        "failure": {"stage": "allowlist", "reason": f"{code}: {reason}"},
    }


def _require_open(document: dict, transition: str) -> None:
    if document.get("state") not in (STATE_ACCEPTED, STATE_RUNNING):
        raise ValueError(f"cannot {transition} a response in state {document.get('state')!r}; it is terminal")


def running(document: dict, *, now: int) -> dict:
    """Mark the run started (first call) and refresh the heartbeat (every call)."""
    _require_open(document, "run")
    updated = dict(document)
    updated["state"] = STATE_RUNNING
    if updated.get("started_at") is None:
        updated["started_at"] = _iso(now)
    updated["heartbeat_at"] = _iso(now)
    return updated


def finished(document: dict, judged: dict) -> dict:
    """Merge judge_run's finished response into the stub; identities must agree."""
    _require_open(document, "finish")
    if judged.get("state") != STATE_FINISHED:
        raise ValueError("the judged response is not in state finished")
    if judged.get("verdict") not in VERDICTS:
        raise ValueError(f"judged verdict {judged.get('verdict')!r} is not one of {sorted(VERDICTS)}")
    if judged.get("run_id") != document.get("run_id"):
        raise ValueError("the judged response is for a different run")
    if judged.get("argument") != document.get("argument") or judged.get("verb") != document.get("verb"):
        raise ValueError("the judged response is for a different request")
    final = dict(document)
    final.update({k: v for k, v in judged.items() if k not in ("schema", "request", "accepted_at")})
    final["state"] = STATE_FINISHED
    final["heartbeat_at"] = final.get("finished_at") or document.get("heartbeat_at")
    return final


def errored(document: dict, *, now: int, stage: str, reason: str) -> dict:
    """The harness could not complete: a finished `error` naming where."""
    _require_open(document, "error")
    if stage not in STAGES:
        raise ValueError(f"unknown failure stage {stage!r}")
    final = dict(document)
    final.update(
        {
            "state": STATE_FINISHED,
            "verdict": "error",
            "finished_at": _iso(now),
            "heartbeat_at": _iso(now),
            "failure": {"stage": stage, "reason": reason},
        }
    )
    return final


def aborted(document: dict, *, now: int, reason: str) -> dict:
    return errored(document, now=now, stage="aborted", reason=reason)


# ── the heartbeat (§6.5) ──────────────────────────────────────────────────────────────────


def remedy_for(slug: str) -> str:
    return f"systemctl --user reset-failed vmtest-bridge@{slug}.path vmtest-bridge@{slug}.service"


def audit_log_for(slug: str) -> str:
    """The verdict of record (§6.6 rule 11), off the mount; the sandbox can only be told where it is."""
    return f"~/.local/state/vmtest-bridge/{slug}/service.log"


def heartbeat(*, now: int, path_unit: dict, service_unit: dict, in_flight: str | None, slug: str) -> dict:
    return {
        "schema": SCHEMA,
        "written_at": _iso(now),
        "path_unit": {"active_state": path_unit["active_state"], "result": path_unit["result"]},
        "service_unit": {"active_state": service_unit["active_state"], "result": service_unit["result"]},
        "in_flight": in_flight,
        "slug": slug,
        "audit_log": audit_log_for(slug),
        "remedy": remedy_for(slug),
    }


def assess_heartbeat(document: dict | None, *, now: int, max_age: int) -> HeartbeatAssessment:
    """ok | stale | wedged | absent — never a timeout, never a fail (§6.5)."""
    if not isinstance(document, dict):
        return HeartbeatAssessment("absent", "no heartbeat document; the bridge units are not installed or never ran")
    written = _parse_iso(document.get("written_at"))
    path_unit = document.get("path_unit")
    service_unit = document.get("service_unit")
    if written is None or not isinstance(path_unit, dict) or not isinstance(service_unit, dict):
        return HeartbeatAssessment("absent", "heartbeat document is malformed; treating the bridge as not installed")
    age = now - written
    if age < 0 or age > max_age:
        return HeartbeatAssessment(
            "stale",
            f"heartbeat written {age}s ago (limit {max_age}s); the bridge daemon is not running",
        )
    for label, unit in (("path unit", path_unit), ("service unit", service_unit)):
        if unit.get("active_state") == "failed":
            return HeartbeatAssessment(
                "wedged",
                f"the bridge {label} is failed ({unit.get('result')}); a human must run: {document.get('remedy')}",
            )
    return HeartbeatAssessment("ok", "bridge alive")

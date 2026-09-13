"""The run-as-probe refresh outcome and base certification (Plan 00110, §4.4a, T6.2).

No refresh boot of its own: every scenario run performs the product's own
`play-AB-dnf-upgrade.yml` transaction on its overlay, and the transcript says
whether it changed anything and which updates revision the GUEST saw. Judged
against the revision the probe read from the canonical host:

    guest_seen < probe_seen          -> incomplete   the guest's mirror lagged; nothing is proven (B7)
    caught up, nothing changed       -> current      the base is checked and current at guest_seen
    caught up, packages changed      -> stale        the base needs `vmtest refresh-base`
    any input missing                -> unknown

`current` is the zero-boot win: the run itself certifies the base at the
guest-seen revision, and `certify` advances `base.json` accordingly — only on
a passing run, only forwards, never touching the identity or disk facts. A
`stale` base is rebuilt by `vmtest refresh-base` (a fast base is re-imported
from the published image; that IS its refresh, minutes not hours). The
overlay of a provisioned run is never flattened into a base: a base is a
fresh install plus updates, and a provisioned system is not.

    python3 -m helpers.vmtest.refresh certify --response FILE --base-json FILE [--now EPOCH]
        VMTEST-BASE-CERTIFIED <name> revision=<r> mirror=<m>
        VMTEST-BASE-NOT-CERTIFIED <name> reason=<why>
    exit 0 either way (not certifying is a result); 2 on unusable inputs.
"""

from __future__ import annotations

import argparse
import dataclasses
import json
import pathlib
import sys
import time
from dataclasses import dataclass

from helpers.vmtest import basejson

STATE_CURRENT = "current"
STATE_STALE = "stale"
STATE_INCOMPLETE = "incomplete"
STATE_UNKNOWN = "unknown"


@dataclass(frozen=True)
class Outcome:
    state: str
    reason: str


def after_run(*, upgrade_changed: bool | None, guest_seen: int | None, probe_seen: int | None) -> Outcome:
    if upgrade_changed is None or guest_seen is None or probe_seen is None:
        return Outcome(STATE_UNKNOWN, "the transcript did not carry the upgrade result, the guest-seen revision, or the probe revision")
    if guest_seen < probe_seen:
        return Outcome(
            STATE_INCOMPLETE,
            f"the guest's mirror was behind the probe ({guest_seen} < {probe_seen}); the base was not checked against the current revision",
        )
    if upgrade_changed:
        return Outcome(STATE_STALE, f"the upgrade changed packages at revision {guest_seen}; the base needs `vmtest refresh-base`")
    return Outcome(STATE_CURRENT, f"the upgrade changed nothing at revision {guest_seen}, which is at or past the probe's {probe_seen}")


def certify(response: dict, record: basejson.BaseRecord, *, now: int) -> tuple[basejson.BaseRecord | None, str]:
    """The advanced record, or None and the reason it was left alone."""
    if response.get("state") != "finished" or response.get("verdict") != "pass":
        return None, f"run verdict is {response.get('verdict')!r}, not pass"
    evidence = response.get("evidence") or {}
    after = (evidence.get("refresh") or {})
    if after.get("state") != STATE_CURRENT:
        return None, f"refresh outcome is {after.get('state')!r}, not current"
    revision = after.get("guest_seen_revision")
    if not isinstance(revision, int):
        return None, "no guest-seen revision in the response"
    if revision < record.last_upgraded_revision:
        return None, f"guest-seen revision {revision} is backwards from the record's {record.last_upgraded_revision}"
    mirror = (evidence.get("guest") or {}).get("updates_mirror") or record.last_upgraded_mirror
    advanced = dataclasses.replace(
        record,
        last_upgraded_revision=revision,
        last_upgraded_at=now,
        last_upgraded_mirror=mirror,
        refresh_state="complete",
    )
    return advanced, ""


def cmd_certify(args: argparse.Namespace) -> int:
    try:
        response = json.loads(pathlib.Path(args.response).read_text(encoding="utf-8"))
        record = basejson.parse_record(pathlib.Path(args.base_json).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError, basejson.BaseRecordError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2
    if not isinstance(response, dict):
        print("ERROR: the response is not a JSON object", file=sys.stderr)
        return 2
    now = args.now if args.now is not None else int(time.time())
    advanced, reason = certify(response, record, now=now)
    if advanced is None:
        print(f"VMTEST-BASE-NOT-CERTIFIED {record.name} reason={reason}")
        return 0
    target = pathlib.Path(args.base_json)
    staging = target.with_name(target.name + ".new")
    staging.write_text(basejson.render_record(advanced), encoding="utf-8")
    staging.replace(target)
    print(f"VMTEST-BASE-CERTIFIED {advanced.name} revision={advanced.last_upgraded_revision} mirror={advanced.last_upgraded_mirror}")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    cert = sub.add_parser("certify", help="advance base.json to the guest-seen revision after a passing, current run")
    cert.add_argument("--response", required=True)
    cert.add_argument("--base-json", required=True)
    cert.add_argument("--now", type=int, default=None)
    cert.set_defaults(func=cmd_certify)
    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())

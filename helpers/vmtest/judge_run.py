"""Turn a finished run into its response document (thin executor).

`vmtest run` captured the transcript and knows the facts the guest cannot: the
run id, the base record, the commit it asked for, the freshness gate's verdict
and the timings. This joins them with `helpers.vmtest.transcript`'s judgement
into the §6.6 response — schema 1, state `finished`, three-valued verdict, a
`failure.stage` on every non-pass, and evidence that binds the base's kind and
name so a fast-path pass can never read as a fresh-install pass. The bridge
fields (request, verb, signature) are Phase 4's; they are absent here, not
faked.

    python3 -m helpers.vmtest.judge_run --transcript FILE --transcript-path REL \\
        --base-json FILE --scenario ID --planned N --max-skipped N --run-id ID \\
        --commit SHA --branch NAME --freshness-decision D --freshness-degraded true|false \\
        [--freshness-divergences a,b] [--divergences c,d] [--override TEXT ...] \\
        --started-at EPOCH --finished-at EPOCH > response.json

Exit 0 on `pass`, 1 on `fail` or `error` (the response is printed either way),
2 when the inputs are unusable (nothing is printed).
"""

from __future__ import annotations

import argparse
import datetime
import hashlib
import json
import pathlib
import sys

from helpers.vmtest import basejson, scenarios, transcript

SCHEMA = 1


def _iso(epoch: int) -> str:
    return datetime.datetime.fromtimestamp(epoch, tz=datetime.UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def _csv(text: str | None) -> list[str]:
    return [item for item in (text or "").split(",") if item]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--transcript", type=pathlib.Path, required=True)
    parser.add_argument("--transcript-path", required=True, help="the transcript's path as the response should cite it")
    parser.add_argument("--base-json", type=pathlib.Path, required=True)
    parser.add_argument("--scenario", required=True)
    parser.add_argument("--planned", type=int, required=True)
    parser.add_argument("--max-skipped", type=int, required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--branch", required=True)
    parser.add_argument("--freshness-decision", required=True)
    parser.add_argument("--freshness-degraded", choices=("true", "false"), required=True)
    parser.add_argument("--freshness-divergences", default="")
    parser.add_argument("--divergences", default="", help="the scenario's own divergences, comma-separated")
    parser.add_argument("--override", action="append", default=[], help="a host-CLI override, recorded verbatim")
    parser.add_argument("--started-at", type=int, required=True)
    parser.add_argument("--finished-at", type=int, required=True)
    args = parser.parse_args(argv)

    try:
        raw = args.transcript.read_bytes()
        parsed = transcript.parse(raw.decode("utf-8", errors="replace"))
    except OSError as exc:
        print(f"ERROR: transcript: {exc}", file=sys.stderr)
        return 2
    except transcript.TranscriptError as exc:
        print(f"ERROR: transcript: {exc}", file=sys.stderr)
        return 2
    try:
        record = basejson.parse_record(args.base_json.read_text(encoding="utf-8"))
    except (OSError, basejson.BaseRecordError) as exc:
        print(f"ERROR: base.json: {exc}", file=sys.stderr)
        return 2

    judgement = transcript.judge(parsed, planned=args.planned, max_skipped=args.max_skipped)
    divergences = sorted(set(_csv(args.freshness_divergences)) | set(_csv(args.divergences)))
    guest_keys = ("boot_id", "machine_id", "os_release", "kernel", "repo_commit", "default_target", "updates_revision", "updates_mirror")

    response = {
        "schema": SCHEMA,
        "verb": "run-scenario",
        "argument": args.scenario,
        "state": "finished",
        "verdict": judgement.verdict,
        "run_id": args.run_id,
        "started_at": _iso(args.started_at),
        "finished_at": _iso(args.finished_at),
        "checks": judgement.checks,
        "checks_skipped": list(judgement.skipped_names),
        "evidence": {
            "transcript": args.transcript_path,
            "transcript_sha256": hashlib.sha256(raw).hexdigest(),
            "base": {
                "profile": record.profile,
                "kind": record.kind,
                "name": record.name,
                "built_from": [a["name"] for a in record.artefacts],
                "compose_label": record.compose_label,
                "compose_id": record.compose_id,
                "base_sha256": record.base_sha256,
                "installed_at": _iso(record.installed_at),
                "last_upgraded_at": _iso(record.last_upgraded_at),
                "last_upgraded_revision": record.last_upgraded_revision,
                "last_upgraded_mirror": record.last_upgraded_mirror,
                "freshness": args.freshness_decision,
                "freshness_degraded": args.freshness_degraded == "true",
                "refresh_state": record.refresh_state,
            },
            "guest": {key: parsed.evidence.get(key) or None for key in guest_keys},
            "repo": {"commit": args.commit, "branch": args.branch},
            "playbook_recap": parsed.recaps[-1] if parsed.recaps else None,
            "run_bash_exit": parsed.run_bash_exit,
            "divergences": divergences,
            "overrides": list(args.override),
        },
        "failure": None
        if judgement.verdict == scenarios.VERDICT_PASS
        else {"stage": judgement.stage, "reason": judgement.reason},
    }
    json.dump(response, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")
    return 0 if judgement.verdict == scenarios.VERDICT_PASS else 1


if __name__ == "__main__":
    sys.exit(main())

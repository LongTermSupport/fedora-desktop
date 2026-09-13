"""Assemble a base.json record from the base builder's facts (thin executor).

The bash builder gathers the facts — what it downloaded, what the guest saw,
what it flattened — and hands them here as argv. The record is built and
validated by `helpers.vmtest.basejson` and printed on stdout for the caller to
write into place; nothing on stdout but the record, diagnostics on stderr.

    python3 -m helpers.vmtest.write_base_record --fedora-version 44 --profile server \\
        --kind fast --name server-fast-44 --compose-id Fedora-44-20260422.1 \\
        --compose-label 44-1.7 --artefact NAME=SHA256 [--artefact ...] \\
        [--treeinfo-checksums FILE.json] --recipe-digest SHA256 \\
        --installed-at EPOCH --last-upgraded-at EPOCH --last-upgraded-revision INT \\
        --last-upgraded-mirror URL --refresh-state complete|incomplete|degraded \\
        --base-sha256 SHA256 --base-size BYTES --base-mtime EPOCH > base.json
"""

from __future__ import annotations

import argparse
import json
import pathlib
import sys

from helpers.vmtest import basejson


def _artefact(text: str) -> dict[str, str]:
    name, separator, digest = text.partition("=")
    if not separator or not name or not digest:
        raise argparse.ArgumentTypeError(f"expected NAME=SHA256, got {text!r}")
    return {"name": name, "sha256": digest}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--fedora-version", type=int, required=True)
    parser.add_argument("--profile", required=True)
    parser.add_argument("--kind", required=True)
    parser.add_argument("--name", required=True)
    parser.add_argument("--compose-id", required=True)
    parser.add_argument("--compose-label", required=True)
    parser.add_argument("--artefact", type=_artefact, action="append", required=True, metavar="NAME=SHA256")
    parser.add_argument(
        "--treeinfo-checksums",
        type=pathlib.Path,
        help="JSON object of tree-relative path -> sha256; required for a full base, forbidden for a fast one",
    )
    parser.add_argument("--recipe-digest", required=True)
    parser.add_argument("--installed-at", type=int, required=True)
    parser.add_argument("--last-upgraded-at", type=int, required=True)
    parser.add_argument("--last-upgraded-revision", type=int, required=True)
    parser.add_argument("--last-upgraded-mirror", required=True)
    parser.add_argument("--refresh-state", required=True)
    parser.add_argument("--base-sha256", required=True)
    parser.add_argument("--base-size", type=int, required=True)
    parser.add_argument("--base-mtime", type=int, required=True)
    args = parser.parse_args(argv)

    treeinfo_checksums = None
    if args.treeinfo_checksums is not None:
        try:
            treeinfo_checksums = json.loads(args.treeinfo_checksums.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            print(f"ERROR: --treeinfo-checksums {args.treeinfo_checksums}: {exc}", file=sys.stderr)
            return 2

    try:
        record = basejson.build_record(
            fedora_version=args.fedora_version,
            profile=args.profile,
            kind=args.kind,
            name=args.name,
            compose_id=args.compose_id,
            compose_label=args.compose_label,
            artefacts=tuple(args.artefact),
            treeinfo_checksums=treeinfo_checksums,
            recipe_digest=args.recipe_digest,
            installed_at=args.installed_at,
            last_upgraded_at=args.last_upgraded_at,
            last_upgraded_revision=args.last_upgraded_revision,
            last_upgraded_mirror=args.last_upgraded_mirror,
            refresh_state=args.refresh_state,
            base_sha256=args.base_sha256,
            base_size=args.base_size,
            base_mtime=args.base_mtime,
        )
    except basejson.BaseRecordError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    sys.stdout.write(basejson.render_record(record))
    return 0


if __name__ == "__main__":
    sys.exit(main())

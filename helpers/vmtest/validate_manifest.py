"""Validate the JSON form of the scenario manifest (thin executor).

Reads the manifest as JSON on stdin — the shape Ansible renders from
`vars/vm-test-scenarios.yml`, or what `scripts/qa-vmtest-manifest.bash` pipes in
after converting the tracked YAML — and validates it with
`helpers.vmtest.scenarios`. Stdout carries marker lines only; every diagnostic
goes to stderr.

    python3 -m helpers.vmtest.validate_manifest --fedora-version 44 < manifest.json
    python3 -m helpers.vmtest.validate_manifest --fedora-version 44 --allowlist < manifest.json

Markers:
    VMTEST-MANIFEST-OK scenarios=N runnable=N bases=N
    VMTEST-MANIFEST-INVALID
With `--allowlist`, stdout is the allowlist itself (one id per line), which is
the payload Ansible writes to the host's `scenarios.allowlist`.
"""

from __future__ import annotations

import argparse
import sys

from helpers.vmtest import scenarios


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument(
        "--fedora-version",
        type=int,
        required=True,
        help="the branch's Fedora version (vars/fedora-version.yml); base names carry it",
    )
    parser.add_argument(
        "--allowlist",
        action="store_true",
        help="print the deployed allowlist (runnable scenario ids, one per line) instead of the OK marker",
    )
    args = parser.parse_args(argv)

    try:
        manifest = scenarios.load_manifest(sys.stdin.read(), args.fedora_version)
        if args.allowlist:
            sys.stdout.write(scenarios.allowlist_text(manifest))
            return 0
    except scenarios.ManifestError as exc:
        print("VMTEST-MANIFEST-INVALID")
        print(f"ERROR: {exc}", file=sys.stderr)
        return 1

    runnable = sum(1 for scenario in manifest.scenarios.values() if scenario.runnable)
    print(
        f"VMTEST-MANIFEST-OK scenarios={len(manifest.scenarios)} "
        f"runnable={runnable} bases={len(manifest.bases)}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())

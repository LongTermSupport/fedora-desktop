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
    VMTEST-BASE key=K name=N kind=K profile=P tree=T|- vcpus=N ram_mib=N   (with --base KEY)
    VMTEST-SCENARIO id=I base=K base_name=N profile=P planned=N|- max_skipped=N runnable=true|false
With `--allowlist`, stdout is the allowlist itself (one id per line), which is
the payload Ansible writes to the host's `scenarios.allowlist`. With
`--base KEY` or `--scenario ID`, stdout is that entry's facts as one marker
line, which is how the `vmtest` CLI reads the manifest without parsing JSON in
bash.
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
    parser.add_argument(
        "--base",
        metavar="KEY",
        help="print one VMTEST-BASE marker with this base's facts instead of the OK marker",
    )
    parser.add_argument(
        "--scenario",
        metavar="ID",
        help="print one VMTEST-SCENARIO marker with this scenario's facts instead of the OK marker",
    )
    args = parser.parse_args(argv)

    try:
        manifest = scenarios.load_manifest(sys.stdin.read(), args.fedora_version)
        if args.scenario is not None:
            scenario = manifest.scenarios.get(args.scenario)
            if scenario is None:
                raise scenarios.ManifestError(
                    f"no scenario {args.scenario!r}; the manifest declares {', '.join(sorted(manifest.scenarios))}"
                )
            print(
                f"VMTEST-SCENARIO id={scenario.id} base={scenario.base.key} base_name={scenario.base.name} "
                f"profile={scenario.profile} planned={scenario.planned if scenario.planned is not None else '-'} "
                f"max_skipped={scenario.max_skipped} runnable={'true' if scenario.runnable else 'false'}"
            )
            return 0
        if args.allowlist:
            sys.stdout.write(scenarios.allowlist_text(manifest))
            return 0
        if args.base is not None:
            base = manifest.bases.get(args.base)
            if base is None:
                raise scenarios.ManifestError(
                    f"no base {args.base!r}; the manifest declares {', '.join(sorted(manifest.bases))}"
                )
            print(
                f"VMTEST-BASE key={base.key} name={base.name} kind={base.kind} "
                f"profile={base.profile} tree={base.tree or '-'} vcpus={base.vcpus} ram_mib={base.ram_mib}"
            )
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

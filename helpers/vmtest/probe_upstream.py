"""Read the Fedora upstream freshness signals and print them as marker lines.

The thin executor for `helpers.vmtest.upstream` (Plan 00110, DESIGN.md §4.1,
§9 T1.4). It fetches, the pure module parses, and this prints one stable
`VMTEST-FRESHNESS-*` line per fact on stdout. Diagnostics go to stderr.

    python3 -m helpers.vmtest.probe_upstream --fedora-version 44 --manifest manifest.json
    python3 -m helpers.vmtest.probe_upstream ... --fixture-dir DIR   # offline: DIR/<host>/<path>

Every signal is reported on its own. A signal that cannot be read or parsed
prints `VMTEST-FRESHNESS-UNREADABLE <signal> <url> <error>` and the probe
carries on to the next, because §4.4 gives "identity unreadable" and "revision
unreadable" different verdicts, and a probe that stopped at the first failure
could not tell them apart. The exit status is non-zero if anything was
unreadable; the probe never substitutes a value for a signal it could not read.

Markers:
    VMTEST-FRESHNESS-COMPOSE-ID <label>
    VMTEST-FRESHNESS-REVISION <int>                      updates repo <revision>
    VMTEST-FRESHNESS-BODHI F<v> <state>
    VMTEST-FRESHNESS-TREE <tree> build_timestamp=<int>
    VMTEST-FRESHNESS-TREE-CHECKSUM <tree> <path> <sha256>
    VMTEST-FRESHNESS-ARTEFACT <base-name> <filename> <sha256> label=<compose-label>
    VMTEST-FRESHNESS-UNREADABLE <signal> <url> <error>
    VMTEST-FRESHNESS-DONE unreadable=<n>
"""

from __future__ import annotations

import argparse
import pathlib
import sys
import urllib.error
import urllib.request
from collections.abc import Callable
from urllib.parse import urlsplit

from helpers.vmtest import scenarios, upstream

ARCH = "x86_64"
CANONICAL = "https://dl.fedoraproject.org/pub/fedora/linux"
RELEASES_JSON_URL = "https://fedoraproject.org/releases.json"
BODHI_RELEASES_URL = "https://bodhi.fedoraproject.org/releases/?rows_per_page=100"
TIMEOUT_SECONDS = 30
USER_AGENT = "fedora-desktop vmtest freshness probe"


def compose_id_url(fedora_version: int) -> str:
    return f"{CANONICAL}/releases/{fedora_version}/COMPOSE_ID"


def treeinfo_url(fedora_version: int, tree: str) -> str:
    return f"{CANONICAL}/releases/{fedora_version}/{tree}/{ARCH}/os/.treeinfo"


def updates_repomd_url(fedora_version: int) -> str:
    return f"{CANONICAL}/updates/{fedora_version}/Everything/{ARCH}/repodata/repomd.xml"


def fixture_path(fixture_dir: pathlib.Path, url: str) -> pathlib.Path:
    """`<dir>/<host>/<path>`; a trailing slash (Bodhi's `/releases/`) becomes `index`."""
    parts = urlsplit(url)
    path = parts.path
    if path.endswith("/"):
        path += "index"
    return fixture_dir / parts.netloc / path.lstrip("/")


def fetch_http(url: str) -> str:
    # Every URL is one of the https constants above with a version or a
    # validated tree name substituted in; nothing here comes from a request.
    if not url.startswith("https://"):
        raise ValueError(f"refusing a non-https upstream URL: {url}")
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:
        return response.read().decode("utf-8")


def make_fixture_fetcher(fixture_dir: pathlib.Path) -> Callable[[str], str]:
    def fetch(url: str) -> str:
        path = fixture_path(fixture_dir, url)
        print(f"fixture: {url} -> {path}", file=sys.stderr)
        return path.read_text(encoding="utf-8")

    return fetch


class Probe:
    def __init__(self, fetch: Callable[[str], str]) -> None:
        self._fetch = fetch
        self.unreadable = 0

    def unreadable_signal(self, signal: str, url: str, error: str) -> None:
        self.unreadable += 1
        flat = " ".join(error.split())
        print(f"VMTEST-FRESHNESS-UNREADABLE {signal} {url} {flat}")
        print(f"ERROR: {signal}: {url}: {flat}", file=sys.stderr)

    def read(self, signal: str, url: str, parse: Callable[[str], object]) -> object | None:
        """Fetch and parse one signal; on any failure print UNREADABLE and return None."""
        try:
            return parse(self._fetch(url))
        except (OSError, urllib.error.URLError, UnicodeDecodeError, upstream.UpstreamParseError) as exc:
            self.unreadable_signal(signal, url, str(exc))
            return None


def run(fedora_version: int, manifest: scenarios.Manifest, fetch: Callable[[str], str]) -> int:
    probe = Probe(fetch)

    compose_id = probe.read("compose-id", compose_id_url(fedora_version), upstream.parse_compose_id)
    if compose_id is not None:
        print(f"VMTEST-FRESHNESS-COMPOSE-ID {compose_id}")

    revision = probe.read("revision", updates_repomd_url(fedora_version), upstream.parse_repomd_revision)
    if revision is not None:
        print(f"VMTEST-FRESHNESS-REVISION {revision}")

    states = probe.read("bodhi", BODHI_RELEASES_URL, upstream.parse_bodhi_releases)
    if states is not None:
        try:
            state = upstream.bodhi_state_for(states, fedora_version)
        except upstream.UpstreamParseError as exc:
            probe.unreadable_signal("bodhi", BODHI_RELEASES_URL, str(exc))
        else:
            print(f"VMTEST-FRESHNESS-BODHI F{fedora_version} {state}")

    # One fetch per distinct tree, however many bases share it.
    trees = sorted({base.tree for base in manifest.bases.values() if base.tree is not None})
    for tree in trees:
        treeinfo = probe.read(f"tree:{tree}", treeinfo_url(fedora_version, tree), upstream.parse_treeinfo)
        if treeinfo is None:
            continue
        print(f"VMTEST-FRESHNESS-TREE {tree} build_timestamp={treeinfo.build_timestamp}")
        for path, digest in sorted(treeinfo.checksums.items()):
            print(f"VMTEST-FRESHNESS-TREE-CHECKSUM {tree} {path} {digest}")

    index = probe.read("artefacts", RELEASES_JSON_URL, upstream.parse_releases_json)
    if index is not None:
        for base in sorted(manifest.bases.values(), key=lambda b: b.name):
            for selector in base.artefacts:
                try:
                    artefact = upstream.select_artefact(index, fedora_version, ARCH, selector)
                    label = upstream.compose_label_from_link(artefact.link)
                except upstream.UpstreamParseError as exc:
                    probe.unreadable_signal(f"artefact {base.name}", RELEASES_JSON_URL, str(exc))
                    continue
                print(
                    f"VMTEST-FRESHNESS-ARTEFACT {base.name} {artefact.filename} "
                    f"{artefact.sha256} label={label}"
                )

    print(f"VMTEST-FRESHNESS-DONE unreadable={probe.unreadable}")
    return 1 if probe.unreadable else 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--fedora-version", type=int, required=True, help="the branch's Fedora version")
    parser.add_argument(
        "--manifest",
        type=pathlib.Path,
        required=True,
        help="the scenario manifest in its JSON form (bases decide which trees and artefacts to read)",
    )
    parser.add_argument(
        "--fixture-dir",
        type=pathlib.Path,
        help="read every URL from DIR/<host>/<path> instead of the network (offline tests)",
    )
    args = parser.parse_args(argv)

    try:
        manifest = scenarios.load_manifest(args.manifest.read_text(encoding="utf-8"), args.fedora_version)
    except (OSError, scenarios.ManifestError) as exc:
        print(f"ERROR: manifest {args.manifest}: {exc}", file=sys.stderr)
        return 2

    fetch = make_fixture_fetcher(args.fixture_dir) if args.fixture_dir else fetch_http
    return run(args.fedora_version, manifest, fetch)


if __name__ == "__main__":
    sys.exit(main())

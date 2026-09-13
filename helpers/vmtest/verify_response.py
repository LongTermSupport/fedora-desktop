"""HOST-side verification of a bridge response signature (Plan 00110, §6.6 rule 11).

    python3 -m helpers.vmtest.verify_response --checkout DIR --key FILE --run-id ID

The container-side reader cannot verify a signature — the key is host-only by
construction — so this is where a human settles whether a response was written
by the host or by the sandbox. Finds the response in the spool for the run id,
recomputes the HMAC under the host key, and checks that the nonce inside the
signed payload is the nonce in the request's own name (a replay under another
request name fails here).

    VMTEST-VERIFY <run-id> signature=ok|bad request=<name> verdict=<verdict|->

Exit 0 when the signature verifies, 1 when it does not, 2 when there is no
response for that run id.
"""

from __future__ import annotations

import argparse
import json
import pathlib
import sys

from helpers.vmtest import spool, verdict


def find_response(responses_dir: pathlib.Path, run_id: str) -> tuple[str, str] | None:
    """The (request name, response text) whose document names this run id, if any."""
    for path in sorted(responses_dir.glob("*.response.json")):
        text = path.read_text(encoding="utf-8")
        try:
            document = json.loads(text)
        except json.JSONDecodeError:
            continue
        if isinstance(document, dict) and document.get("run_id") == run_id:
            return path.name[: -len(".response.json")], text
    return None


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--checkout", required=True, help="the checkout whose spool holds the response")
    parser.add_argument("--key", required=True, help="the host-only response.key")
    parser.add_argument("--run-id", required=True)
    args = parser.parse_args(argv)

    found = find_response(pathlib.Path(args.checkout) / "untracked" / "vmtest-bridge" / "responses", args.run_id)
    if found is None:
        print(f"ERROR: no response for run id {args.run_id!r} in the spool", file=sys.stderr)
        return 2
    name, text = found
    key = pathlib.Path(args.key).read_bytes()
    match = spool.REQUEST_NAME_RE.match(name)
    document = json.loads(text)
    verdict_name = document.get("verdict") or "-"
    try:
        signed = verdict.verify(text, key)
    except verdict.SignatureError as exc:
        print(f"signature does not verify under the host key: {exc}", file=sys.stderr)
        print(f"VMTEST-VERIFY {args.run_id} signature=bad request={name} verdict={verdict_name}")
        return 1
    if match is None or signed["signature"]["nonce"] != match.group(3):
        print("signed nonce is not the request's nonce: this response was signed for a different request (replay)", file=sys.stderr)
        print(f"VMTEST-VERIFY {args.run_id} signature=bad request={name} verdict={verdict_name}")
        return 1
    print(f"VMTEST-VERIFY {args.run_id} signature=ok request={name} verdict={verdict_name}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

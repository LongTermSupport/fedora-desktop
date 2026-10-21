"""Read membership files with pingbus's own readers, for strict_test.go.

Usage: python3 -I read_membership.py <checkout> <kind>:<dir>...

<kind> is desired (read_desired of <dir>/membership.json) or applied (read_applied of
<dir>/applied.json). One line per file, in order: "ok", or "refused" when the reader raises
MembershipError. Any other exception is not caught, so a reader that crashes fails the test.
"""

import pathlib
import sys


def main() -> None:
    checkout, files = sys.argv[1], sys.argv[2:]
    sys.path.insert(0, checkout)
    from helpers.pingbus import membership

    readers = {"desired": membership.read_desired, "applied": membership.read_applied}
    for item in files:
        kind, _, directory = item.partition(":")
        try:
            readers[kind](pathlib.Path(directory))
        except membership.MembershipError:
            print("refused")
        else:
            print("ok")


main()

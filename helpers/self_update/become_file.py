"""Is the root-only become password file one the cycle can use (Plan 00137 Task 6.2, route G).

    python3 -B -m helpers.self_update.become_file /etc/fedora-desktop/self-update.become

Prints one JSON line, {"state": S, "reason": R}, and exits 0 for every state:

- `present`: a regular file the cycle's own `read_private(..., secret=True)` accepts (owned
  by root, mode 0600, no symlink) holding one non-empty line, the password run.bash reads;
- `missing`: no file at the path;
- `unusable`: anything else, with the reason in words that never repeat the content.

play-self-update.yml runs it as root before it changes anything. When the file is present,
`self_update_become_password` is not needed in host_vars, so an install can delete the
vaulted value from the user's checkout and the cycle keeps working from the root-only file.
Exit 2 is a usage error, the one failure.
"""

from __future__ import annotations

import json
import os
import sys
from typing import TextIO

from helpers.self_update.cycle import ConfigError, read_private


def inspect(path: str) -> tuple[str, str]:
    """The file's state and, unless present, why it cannot be used."""
    if not os.path.lexists(path):
        return "missing", f"there is no become password file at {path}"
    try:
        read_private(path, "become password file", secret=True)
    except ConfigError as error:
        return "unusable", str(error)
    except UnicodeDecodeError:
        return "unusable", f"the become password file {path} is not UTF-8 text"
    # Read again as bytes: text mode turns a CRLF into "\n", and run.bash would keep the "\r".
    with open(path, "rb") as handle:
        data = handle.read()
    line = data[:-1] if data.endswith(b"\n") else data
    if not line:
        return "unusable", f"the become password file {path} is empty"
    if b"\n" in line or b"\r" in line:
        return "unusable", f"the become password file {path} holds more than one line"
    return "present", ""


def main(argv: list[str] | None = None, *, stdout: TextIO = sys.stdout, stderr: TextIO = sys.stderr) -> int:
    args = sys.argv[1:] if argv is None else argv
    if len(args) != 1 or not os.path.isabs(args[0]):
        print("usage: python3 -m helpers.self_update.become_file <absolute path>", file=stderr)
        return 2
    state, reason = inspect(args[0])
    print(json.dumps({"state": state, "reason": reason}), file=stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())

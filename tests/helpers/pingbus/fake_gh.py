"""A stand-in `gh` executable for the pingbus tests: the forge credential's one source (§6).

pingbus runs `gh auth token --hostname github.com` from the PATH of the environment it is
given, so a test puts this stand-in's directory first on that PATH and no test ever reads
the real login of whoever runs the suite. Anything but exactly that command line exits 2.
"""

from __future__ import annotations

import os
import pathlib

#: A token shaped like a GitHub one that is not one.
FAKE_GH_TOKEN = "gho_notarealtoken0123456789abcdefghijkl"

_ANSWERS = {
    "token": f"printf '%s\\n' '{FAKE_GH_TOKEN}'\nexit 0",
    "logged-out": "echo 'no oauth token found for github.com' >&2\nexit 1",
    "broken": "echo 'failed to read configuration: permission denied' >&2\nexit 1",
}


def fake_gh_dir(parent: pathlib.Path, answer: str = "token") -> str:
    """A new directory under `parent` holding an executable `gh` that answers `answer`
    (`token`, `logged-out` or `broken`); returns the directory, for the front of PATH."""
    directory = parent / f"fake-gh-{answer}"
    directory.mkdir()
    script = directory / "gh"
    script.write_text(
        "#!/bin/sh\n"
        'if [ "$*" != "auth token --hostname github.com" ]; then\n'
        '    echo "fake gh: unexpected arguments: $*" >&2\n'
        "    exit 2\n"
        "fi\n"
        f"{_ANSWERS[answer]}\n",
        encoding="utf-8",
    )
    script.chmod(0o755)
    return str(directory)


def path_with(directory: str, path: str | None = None) -> str:
    """`directory` in front of `path` (default: this process's PATH)."""
    rest = os.environ.get("PATH", "/usr/bin:/bin") if path is None else path
    return f"{directory}{os.pathsep}{rest}"

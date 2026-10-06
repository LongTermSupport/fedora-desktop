"""Build the `pingbus` and `agent-bus` zipapps, reproducibly (Plan 00161 DESIGN D9).

    python3 -m helpers.pingbus.bundle --source <repo> --out <file.pyz> {pingbus,agent-bus}

Both archives carry every `*.py` file under `helpers/pingbus/` and `helpers/agent_bus/`
(found by walking the trees, so a module a later unit adds is included with no edit
here) except this bundler itself; they differ only in `__main__.py`. A symlinked module
is refused, since its bytes would come from outside the tree. The archive's `helpers` packages get empty
`__init__.py` files, making them regular packages: no `helpers` directory elsewhere on
the path can contribute modules to them.

Reproducible: entries are sorted, stored uncompressed (no dependence on the zlib build),
and carry a fixed timestamp and mode, so the installer and the ccy play produce the same
bytes from the same source. The output is written atomically with mode 0755 and left
alone when it already holds the same bytes and mode.

stdout carries one marker line, `BUNDLE-CHANGED <path>` or `BUNDLE-UNCHANGED <path>`;
errors go to stderr and exit 1.
"""

from __future__ import annotations

import argparse
import io
import os
import pathlib
import stat
import sys
import tempfile
import zipfile
from collections.abc import Sequence
from typing import TextIO

#: App name -> the module whose `main()` (returning an exit code) the archive runs.
APPS = {
    "pingbus": "helpers.pingbus.cli",
    "agent-bus": "helpers.agent_bus.cli",
}
PACKAGES = ("helpers/pingbus", "helpers/agent_bus")
#: Build-time only: no app imports it.
EXCLUDE = frozenset({"helpers/pingbus/bundle.py"})
SHEBANG = b"#!/usr/bin/env python3\n"
ZIP_EPOCH = (1980, 1, 1, 0, 0, 0)
ENTRY_MODE = stat.S_IFREG | 0o644
ARCHIVE_MODE = 0o755
MARK_CHANGED = "BUNDLE-CHANGED"
MARK_UNCHANGED = "BUNDLE-UNCHANGED"


class BundleError(Exception):
    """The archive cannot be built from this source tree."""


def _main_module(entry: str) -> bytes:
    return f"import sys\n\nfrom {entry} import main\n\nsys.exit(main())\n".encode()


def collect(source: pathlib.Path) -> dict[str, bytes]:
    """Archive name -> contents for every module of the bus packages under `source`."""
    files: dict[str, bytes] = {"helpers/__init__.py": b""}
    for package in PACKAGES:
        root = source / package
        if not root.is_dir():
            raise BundleError(f"{package} is not a directory under {source}")
        files[f"{package}/__init__.py"] = b""
        for path in root.rglob("*.py"):
            relative = path.relative_to(source)
            name = relative.as_posix()
            if "__pycache__" in relative.parts or name in EXCLUDE:
                continue
            if path.is_symlink():
                raise BundleError(f"{name} is a symlink; bundle only regular files in the tree")
            if not path.is_file():
                continue
            if name in files:
                raise BundleError(f"{name} would replace the archive's generated package marker")
            files[name] = path.read_bytes()
    return files


def build(app: str, source: pathlib.Path) -> bytes:
    """The archive's bytes: a deterministic function of the module files' contents."""
    entry = APPS.get(app)
    if entry is None:
        raise BundleError(f"unknown app {app!r} (one of: {', '.join(APPS)})")
    files = collect(source)
    entry_file = entry.replace(".", "/") + ".py"
    if entry_file not in files:
        raise BundleError(f"{entry_file}, the {app} entry point, is missing under {source}")
    files["__main__.py"] = _main_module(entry)
    buffer = io.BytesIO()
    buffer.write(SHEBANG)
    with zipfile.ZipFile(buffer, "w", compression=zipfile.ZIP_STORED) as archive:
        for name in sorted(files):
            info = zipfile.ZipInfo(name, date_time=ZIP_EPOCH)
            info.external_attr = ENTRY_MODE << 16
            info.create_system = 3
            archive.writestr(info, files[name])
    return buffer.getvalue()


def _is_current(target: pathlib.Path, data: bytes) -> bool:
    if not target.is_file() or target.is_symlink():
        return False
    return stat.S_IMODE(target.stat().st_mode) == ARCHIVE_MODE and target.read_bytes() == data


def install(target: pathlib.Path, data: bytes) -> bool:
    """Place `data` at `target` (mode 0755) atomically. False when it was already there."""
    if not target.parent.is_dir():
        raise BundleError(f"output directory {target.parent} does not exist")
    if _is_current(target, data):
        return False
    fd, temp_name = tempfile.mkstemp(prefix=f".{target.name}.", dir=target.parent)
    temp = pathlib.Path(temp_name)
    try:
        with os.fdopen(fd, "wb") as handle:
            handle.write(data)
            handle.flush()
            os.fchmod(handle.fileno(), ARCHIVE_MODE)
            os.fsync(handle.fileno())
        os.replace(temp, target)
    finally:
        if temp.exists():
            temp.unlink()
    return True


def main(argv: Sequence[str] | None = None, *, stdout: TextIO | None = None,
         stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    parser = argparse.ArgumentParser(prog="python3 -m helpers.pingbus.bundle",
                                     description="Build a bus zipapp reproducibly.")
    parser.add_argument("--source", required=True, type=pathlib.Path,
                        help="the repository root holding helpers/")
    parser.add_argument("--out", required=True, type=pathlib.Path, help="the .pyz to write")
    parser.add_argument("app", choices=sorted(APPS))
    args = parser.parse_args(argv)
    try:
        changed = install(args.out, build(args.app, args.source))
    except (BundleError, OSError) as exc:
        err.write(f"bundle: {exc}\n")
        return 1
    out.write(f"{MARK_CHANGED if changed else MARK_UNCHANGED} {args.out}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())

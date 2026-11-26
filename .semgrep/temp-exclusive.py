"""Fixture for .semgrep/temp-exclusive.yml, Python (the gate's self-test).

Each "ruleid" line marks the line below it as the start of a finding; each "ok" line marks the
line below it as clean.
"""

import os
import tempfile


def lifecycle_record(path, text):
    # The Plan 00265 R36 instance, as it was: ccy_lifecycle.py.
    tmp = path.with_name(path.name + f".tmp.{os.getpid()}")
    # ruleid: temp-not-exclusive-python
    tmp.write_text(text, encoding="utf-8")
    os.replace(tmp, path)


def other_shapes(path, data, directory):
    tmp = f"{path}.tmp"
    # ruleid: temp-not-exclusive-python
    with open(tmp, "w", encoding="utf-8") as handle:
        handle.write(data)
    os.rename(tmp, path)

    staging = directory / f".{path.name}.{os.getpid()}"
    # ruleid: temp-not-exclusive-python
    fd = os.open(staging, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.close(fd)
    staging.replace(path)

    other = path.with_suffix(".new")
    # ruleid: temp-not-exclusive-python
    with other.open("wb") as handle:
        handle.write(data)
    try:
        other.rename(path)
    finally:
        pass


def fixed(path, data):
    fd, name = tempfile.mkstemp(dir=path.parent, prefix=f".{path.name}.")
    # ok: temp-not-exclusive-python
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write(data)
    os.replace(name, path)

    tmp = path.with_name(path.name + ".tmp")
    # ok: temp-not-exclusive-python
    with open(tmp, "x", encoding="utf-8") as handle:
        handle.write(data)
    os.replace(tmp, path)

    # ok: temp-not-exclusive-python
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.close(fd)
    os.replace(tmp, path)


def written_in_place(path, data):
    # ok: temp-not-exclusive-python
    path.write_text(data, encoding="utf-8")

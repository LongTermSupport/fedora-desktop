# Fixture for .semgrep/walk-open.yml. `ruleid:` marks a line that must fire, `ok:` a line that
# must stay silent.

import glob
import io
import os
import stat
from pathlib import Path


def _read(path):
    with open(path, encoding="utf-8") as handle:
        return handle.read()


def play_text(repo_root, prefixes):
    # The originating instance in helpers/play_ledger/group_vars.py: walked names gathered into a
    # set and read through a helper of the module's own.
    paths = set()
    for prefix in prefixes:
        for dirpath, _dirs, files in os.walk(os.path.join(repo_root, prefix)):
            paths.update(os.path.relpath(os.path.join(dirpath, name), repo_root) for name in files)
    # ruleid: walk-opens-non-regular-file
    texts = (_read(os.path.join(repo_root, path)) for path in sorted(paths))
    return list(texts)


def walked_and_opened(top):
    out = []
    for dirpath, _dirs, files in os.walk(top):
        for name in files:
            # ruleid: walk-opens-non-regular-file
            with open(os.path.join(dirpath, name), encoding="utf-8") as handle:
                out.append(handle.read())
    return out


def child_of_a_walked_directory(top):
    for name in os.listdir(top):
        # ruleid: walk-opens-non-regular-file
        with io.open(os.path.join(top, name, "stat"), encoding="utf-8") as handle:
            yield handle.read()


def raw_descriptor(top):
    with os.scandir(top) as entries:
        for entry in entries:
            # ruleid: walk-opens-non-regular-file
            yield os.open(entry.path, os.O_RDONLY)


def globbed(top):
    for name in glob.glob(os.path.join(top, "*.yml")):
        # ruleid: walk-opens-non-regular-file
        with open(name, encoding="utf-8") as handle:
            yield handle.read()


def iglobbed(top):
    found = []
    for name in glob.iglob(os.path.join(top, "**"), recursive=True):
        found.append(name)
    # ruleid: walk-opens-non-regular-file
    return [_read(name) for name in found]


def path_walks(top):
    root = Path(top)
    # ruleid: walk-opens-non-regular-file
    texts = [p.read_text(encoding="utf-8") for p in root.rglob("*.yml")]
    # ruleid: walk-opens-non-regular-file
    blobs = [p.read_bytes() for p in root.glob("*.bin")]
    for child in root.iterdir():
        # ruleid: walk-opens-non-regular-file
        with (child / "cmdline").open("rb") as handle:
            blobs.append(handle.read())
    return texts, blobs


def suffix_is_no_check(top):
    # A named pipe can be called anything.yml.
    for dirpath, _dirs, files in os.walk(top):
        for name in files:
            if name.endswith(".yml"):
                # ruleid: walk-opens-non-regular-file
                yield _read(os.path.join(dirpath, name))


def existence_is_no_check(top):
    for name in os.listdir(top):
        path = os.path.join(top, name)
        if os.path.exists(path):
            # ruleid: walk-opens-non-regular-file
            yield _read(path)


def guarded_by_isfile(top):
    out = []
    for dirpath, _dirs, files in os.walk(top):
        for name in files:
            path = os.path.join(dirpath, name)
            if not os.path.isfile(path):
                continue
            # ok: walk-opens-non-regular-file
            with open(path, encoding="utf-8") as handle:
                out.append(handle.read())
    return out


def guarded_in_a_comprehension(top):
    paths = set()
    for dirpath, _dirs, files in os.walk(top):
        paths.update(p for p in (os.path.join(dirpath, n) for n in files) if os.path.isfile(p))
    # ok: walk-opens-non-regular-file
    return [_read(path) for path in sorted(paths)]


def guarded_by_is_file(top):
    # ok: walk-opens-non-regular-file
    return [p.read_text(encoding="utf-8") for p in Path(top).rglob("*.yml") if p.is_file()]


def guarded_dir_entry(top):
    with os.scandir(top) as entries:
        for entry in entries:
            if entry.is_file():
                # ok: walk-opens-non-regular-file
                with open(entry.path, encoding="utf-8") as handle:
                    yield handle.read()


def guarded_by_s_isreg(top):
    for name in os.listdir(top):
        path = os.path.join(top, name)
        if stat.S_ISREG(os.stat(path).st_mode):
            # ok: walk-opens-non-regular-file
            yield _read(path)


def guarded_in_a_compound_condition(top):
    for name in sorted(os.listdir(top)):
        path = os.path.join(top, name)
        if not name.endswith(".go") or not os.path.isfile(path):
            continue
        # ok: walk-opens-non-regular-file
        with open(path, encoding="utf-8") as handle:
            yield handle.read()


def guarded_path_in_a_compound_condition(top):
    for path in sorted(Path(top).iterdir()):
        if path.is_symlink() or not path.is_file():
            continue
        # ok: walk-opens-non-regular-file
        yield path.read_text(encoding="utf-8")


def a_symlink_test_is_no_check(top):
    for path in sorted(Path(top).iterdir()):
        if path.is_symlink():
            continue
        # ruleid: walk-opens-non-regular-file
        yield path.read_text(encoding="utf-8")


def _reads_key(node):
    return node


def a_name_that_only_starts_with_read(top):
    # ok: walk-opens-non-regular-file
    return [_reads_key(name) for name in os.listdir(top)]


def listed_only(top):
    # ok: walk-opens-non-regular-file
    return sorted(os.listdir(top))


def not_walked(path):
    # ok: walk-opens-non-regular-file
    return _read(path)

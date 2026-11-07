# Fixture for .semgrep/walk-open.yml. `ruleid:` marks a line that must fire, `ok:` a line that
# must stay silent.

import glob
import io
import os
import stat
import zipfile
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


def a_check_that_guards_nothing(top):
    # The check runs, but the open is not in its branch.
    for name in os.listdir(top):
        path = os.path.join(top, name)
        if os.path.isfile(path):
            print("regular")
        # ruleid: walk-opens-non-regular-file
        yield _read(path)


def a_bare_check_statement(top):
    for name in os.listdir(top):
        path = os.path.join(top, name)
        os.path.isfile(path)
        # ruleid: walk-opens-non-regular-file
        with open(path, encoding="utf-8") as handle:
            yield handle.read()


def a_check_joined_by_or(top):
    for path in sorted(Path(top).iterdir()):
        if path.is_file() or True:
            # ruleid: walk-opens-non-regular-file
            yield path.read_text(encoding="utf-8")


def skip_and_warn(top):
    # The failed check only warns, then falls through to the open.
    for path in sorted(Path(top).iterdir()):
        if not path.is_file():
            print(f"not a regular file: {path}")
        # ruleid: walk-opens-non-regular-file
        yield path.read_text(encoding="utf-8")


def a_negated_check_joined_by_and(top):
    for path in sorted(Path(top).iterdir()):
        if not path.is_file() and path.suffix == ".yml":
            continue
        # ruleid: walk-opens-non-regular-file
        yield path.read_text(encoding="utf-8")


def a_check_on_another_name(top, other):
    for path in sorted(Path(top).iterdir()):
        if Path(other).is_file():
            # ruleid: walk-opens-non-regular-file
            yield path.read_text(encoding="utf-8")


def the_else_branch_is_not_guarded(top):
    for path in sorted(Path(top).iterdir()):
        if path.is_file():
            pass
        else:
            # ruleid: walk-opens-non-regular-file
            yield path.read_text(encoding="utf-8")


def guarded_branch_with_more_terms(top):
    for path in sorted(Path(top).iterdir()):
        if path.suffix == ".yml" and path.is_file():
            # ok: walk-opens-non-regular-file
            yield path.read_text(encoding="utf-8")


def refused_with_raise(top):
    for path in sorted(Path(top).iterdir()):
        if not path.is_file():
            raise ValueError(f"{path} is not a regular file")
        # ok: walk-opens-non-regular-file
        yield path.read_text(encoding="utf-8")


def left_with_return(top):
    for path in sorted(Path(top).iterdir()):
        if not path.is_file():
            return
        # ok: walk-opens-non-regular-file
        yield path.read_text(encoding="utf-8")


def guarded_in_an_elif(top):
    for path in sorted(Path(top).iterdir()):
        if path.suffix == ".skip":
            continue
        elif path.is_file():
            # ok: walk-opens-non-regular-file
            yield path.read_text(encoding="utf-8")


def zipped(top):
    for path in sorted(Path(top).glob("*.zip")):
        # ruleid: walk-opens-non-regular-file
        with zipfile.ZipFile(path) as archive:
            yield archive.namelist()


def guarded_by_short_circuit(top):
    for name in glob.glob(os.path.join(top, "*/idVendor")):
        # ok: walk-opens-non-regular-file
        if not os.path.isfile(name) or _read(name).strip() != "17e9":
            continue
        product = os.path.join(os.path.dirname(name), "idProduct")
        # ok: walk-opens-non-regular-file
        if os.path.isfile(product) and _read(product).strip() == "4307":
            yield product


def a_short_circuit_the_wrong_way(top):
    for name in os.listdir(top):
        # ruleid: walk-opens-non-regular-file
        if os.path.isfile(name) or _read(name):
            yield name


def guarded_by_a_negated_conjunction(top):
    for name in os.listdir(top):
        status = os.path.join(top, name, "status")
        modes = os.path.join(top, name, "modes")
        if not (os.path.isfile(status) and os.path.isfile(modes)):
            continue
        # ok: walk-opens-non-regular-file
        yield _read(status), _read(modes)


def refused_by_assert(top):
    for path in sorted(Path(top).glob("*.js")):
        assert path.is_file(), f"{path} is not a regular file"
        # ok: walk-opens-non-regular-file
        yield path.read_text()


class Refused:
    def assertTrue(self, condition, message=None):
        if not condition:
            raise AssertionError(message)

    def refused_by_assert_true(self, top):
        for path in sorted(Path(top).glob("*.js")):
            self.assertTrue(path.is_file(), f"{path.name} is not a regular file")
            # ok: walk-opens-non-regular-file
            yield path.read_text()

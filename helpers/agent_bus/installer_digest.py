"""The installer's source digest: the identity of agent-bus-install now that it is a Go binary.

The digest is a hash of the source the binary is built from, not of the binary: the play
computes it with this module and bakes it in (`go build -ldflags "-X main.SourceDigest=<hex>"`),
`agent-bus-install team` stamps it, and `declare.declared_software` recomputes it from the
checkout. It needs no Go toolchain, and it moves exactly when a rebuild would produce a
different program, so "the checkout moved on, the play has not run" shows as out of date.

The definition, all paths relative to `<checkout>/src-go` and written with `/`:

1. The build closure is `cmd/agent-bus-install` and every package of the module it imports,
   directly or not (imports of other modules and of the standard library are not followed:
   the module has no dependencies). A package is a directory; its imports are read from every
   `.go` file in it except `*_test.go`, build constraints ignored, so the closure is the
   largest the tags allow. The Go test src-go/internal/businstall/digest_agree_test.go proves
   it equals the closure `go list -deps` reports for the build the play does.
2. The files are `go.mod` and every regular file directly in a closure directory, `*_test.go`
   excepted (`usage.txt`, which the binary embeds, counts; `testdata` does not).
3. Sorted by path (bytewise), the digest is the sha256 of the concatenation, per file, of
   `<path>NUL<sha256 hex of the file's bytes>LF`.

Run as `python3 -m helpers.agent_bus.installer_digest --source <checkout>`: it prints the
digest and nothing else.
"""

from __future__ import annotations

import argparse
import hashlib
import pathlib
import re
import sys
from collections.abc import Sequence
from typing import TextIO

MODULE_DIR = "src-go"
COMMAND_DIR = "cmd/agent-bus-install"
EXIT_OK, EXIT_FAILURE = 0, 1

_MODULE_LINE = re.compile(r"^module[ \t]+(\S+)[ \t]*$", re.MULTILINE)
_COMMENT = re.compile(r"//[^\n]*|/\*.*?\*/", re.DOTALL)
_IMPORT_BLOCK = re.compile(r"^import[ \t]*\((.*?)\)", re.MULTILINE | re.DOTALL)
_IMPORT_LINE = re.compile(r"^import[ \t]+(?:[\w.]+[ \t]+)?\"([^\"\n]+)\"", re.MULTILINE)
_BLOCK_SPEC = re.compile(r"(?:^|[ \t;])(?:[\w.]+[ \t]+)?\"([^\"\n]+)\"", re.MULTILINE)


class DigestError(Exception):
    """The digest cannot be computed; the message names why."""


def _module_path(module_dir: pathlib.Path) -> str:
    try:
        text = (module_dir / "go.mod").read_text(encoding="utf-8")
    except OSError as exc:
        raise DigestError(f"cannot read {module_dir / 'go.mod'}: {exc.strerror}") from None
    found = _MODULE_LINE.search(text)
    if found is None:
        raise DigestError(f"{module_dir / 'go.mod'} names no module")
    return found.group(1)


def _sources(directory: pathlib.Path) -> list[pathlib.Path]:
    return sorted(path for path in directory.glob("*.go") if path.is_file() and not path.name.endswith("_test.go"))


def _imports(path: pathlib.Path) -> set[str]:
    # The strings of an import are never inside a comment, and a comment may hold a quoted path.
    text = _COMMENT.sub("", path.read_text(encoding="utf-8"))
    found = set(_IMPORT_LINE.findall(text))
    for block in _IMPORT_BLOCK.findall(text):
        found.update(_BLOCK_SPEC.findall(block))
    return found


def closure(source: pathlib.Path) -> list[str]:
    """The package directories the installer binary is built from, sorted."""
    module_dir = source / MODULE_DIR
    prefix = _module_path(module_dir) + "/"
    seen: set[str] = set()
    todo = [COMMAND_DIR]
    while todo:
        package = todo.pop()
        if package in seen:
            continue
        seen.add(package)
        files = _sources(module_dir / package)
        if not files:
            raise DigestError(f"package {package} has no Go files (imported, or the command)")
        for path in files:
            todo.extend(name[len(prefix):] for name in _imports(path) if name.startswith(prefix))
    return sorted(seen)


def files(source: pathlib.Path) -> list[str]:
    """Every file the digest covers, sorted, relative to src-go."""
    module_dir = source / MODULE_DIR
    names = ["go.mod"]
    for package in closure(source):
        names.extend(f"{package}/{path.name}" for path in sorted((module_dir / package).iterdir())
                     if path.is_file() and not path.name.endswith("_test.go"))
    return sorted(names)


def source_digest(source: pathlib.Path) -> str:
    """The sha256 (hex) the installer binary built from this checkout carries."""
    module_dir = source / MODULE_DIR
    listing = hashlib.sha256()
    for name in files(source):
        listing.update(f"{name}\0{hashlib.sha256((module_dir / name).read_bytes()).hexdigest()}\n".encode())
    return listing.hexdigest()


def main(argv: Sequence[str] | None = None, *, stdout: TextIO | None = None,
         stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    parser = argparse.ArgumentParser(prog="installer_digest", description=__doc__.split("\n\n")[0])
    parser.add_argument("--source", required=True, help="the fedora-desktop checkout")
    args = parser.parse_args(argv)
    try:
        digest = source_digest(pathlib.Path(args.source))
    except DigestError as exc:
        err.write(f"installer_digest: {exc}\n")
        return EXIT_FAILURE
    out.write(f"{digest}\n")
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())

"""Render a VM kickstart template's `@NAME@` placeholders (Plan 00110, T3b.1 / T5.1).

    python3 -m helpers.vmtest.render_kickstart --template FILE --set NAME=VALUE [--set ...] > ks.cfg

Total in both directions: a placeholder with no value and a value with no
placeholder are both refusals (exit 2, nothing on stdout), so a typo cannot
produce an installer that boots with `@SSH_PUBKEY@` as its authorised key. A
value may not contain a newline (a second kickstart command) or a placeholder
shape of its own.
"""

from __future__ import annotations

import argparse
import pathlib
import re
import sys

PLACEHOLDER_RE = re.compile(r"@([A-Z][A-Z0-9_]*)@")


class RenderError(ValueError):
    """The template and the values do not describe one complete kickstart."""


def render(template: str, values: dict[str, str]) -> str:
    wanted = set(PLACEHOLDER_RE.findall(template))
    given = set(values)
    if wanted - given:
        raise RenderError(f"no value for placeholder(s): {', '.join(sorted(wanted - given))}")
    if given - wanted:
        raise RenderError(f"value(s) with no placeholder in the template: {', '.join(sorted(given - wanted))}")
    for name, value in values.items():
        if "\n" in value or "\r" in value:
            raise RenderError(f"value for {name} contains a newline")
        if PLACEHOLDER_RE.search(value):
            raise RenderError(f"value for {name} contains a placeholder shape")
    return PLACEHOLDER_RE.sub(lambda match: values[match.group(1)], template)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--template", required=True, type=pathlib.Path)
    parser.add_argument("--set", action="append", default=[], metavar="NAME=VALUE")
    parser.add_argument(
        "--set-stdin", action="store_true",
        help="also read NAME=VALUE lines from stdin — for values that must not appear in argv (a passphrase)",
    )
    args = parser.parse_args(argv)
    pairs = list(args.set)
    if args.set_stdin:
        pairs.extend(line.rstrip("\n") for line in sys.stdin if line.strip())
    values: dict[str, str] = {}
    for item in pairs:
        name, separator, value = item.partition("=")
        if not separator or not PLACEHOLDER_RE.fullmatch(f"@{name}@"):
            print(f"ERROR: expected NAME=VALUE with an upper-case NAME, got {item!r}", file=sys.stderr)
            return 2
        values[name] = value
    try:
        rendered = render(args.template.read_text(encoding="utf-8"), values)
    except (OSError, RenderError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2
    sys.stdout.write(rendered)
    return 0


if __name__ == "__main__":
    sys.exit(main())

# Fixture for .semgrep/stdin-wait.yml. `ruleid:` marks a line that must fire, `ok:` a line that
# must stay silent.

import json
import sys

from helpers.stdin_text import stdin_text


def side_text(stdin, cap):
    # The originating instance in helpers/pingbus/cli.py: `thread --close` read here and waited.
    # ruleid: stdin-read-unbounded
    raw = stdin.read(cap + 2)
    return raw


def from_args(args):
    # ruleid: stdin-read-unbounded
    return args.stdin.read(4096)


def whole_stdin():
    # ruleid: stdin-read-unbounded
    return sys.stdin.read()


def bytes_of_stdin():
    # ruleid: stdin-read-unbounded
    return sys.stdin.buffer.read()


def one_line():
    # ruleid: stdin-read-unbounded
    return sys.stdin.readline()


def handed_one_line(stdin):
    # ruleid: stdin-read-unbounded
    return stdin.readline().removesuffix("\n")


def all_lines():
    # ruleid: stdin-read-unbounded
    return sys.stdin.readlines()


def json_of_stdin():
    # ruleid: stdin-read-unbounded
    return json.load(sys.stdin)


def iterated():
    out = []
    # ruleid: stdin-read-unbounded
    for line in sys.stdin:
        out.append(line)
    return out


def generator_of_lines(pairs):
    # helpers/vmtest/render_kickstart.py's shape: a generator over stdin's lines.
    # ruleid: stdin-read-unbounded
    pairs.extend(line.rstrip("\n") for line in sys.stdin if line.strip())


def list_of_lines():
    # ruleid: stdin-read-unbounded
    return [line for line in sys.stdin]


def handed_stream_iterated(stdin):
    out = []
    # ruleid: stdin-read-unbounded
    for line in stdin:
        out.append(line)
    return out


def through_the_reader(stdin):
    # ok: stdin-read-unbounded
    return stdin_text.read(stdin, prog="tool")


def bounded_for_a_verb():
    # ok: stdin-read-unbounded
    return stdin_text.read(sys.stdin.buffer, prog="tool", give_up_s=60.0)


def a_file_is_not_stdin(path):
    with open(path, encoding="utf-8") as handle:
        # ok: stdin-read-unbounded
        return handle.read()


def a_subprocess_stdin_is_written_not_read(proc):
    # ok: stdin-read-unbounded
    proc.stdin.write(b"x")

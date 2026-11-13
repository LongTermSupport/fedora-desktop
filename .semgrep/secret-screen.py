"""Fixture for .semgrep/secret-screen.yml, Python (the gate's self-test).

Each "ruleid" line marks the line below it as the start of a finding; each "ok" line marks the
line below it as clean.
"""

import getpass
import sys

from helpers import secret_screen


def human(io, admin, team, transport, args):
    # The issue #261 instance, as it was: agent-bus human password.
    # ruleid: secret-on-normal-screen-python
    io.lines([admin.human_password(team, transport, args.name)])


def other_shapes(token, gh_password, api_key, access_token, stream):
    # ruleid: secret-on-normal-screen-python
    print(token)
    # ruleid: secret-on-normal-screen-python
    print("the password:", gh_password)
    # ruleid: secret-on-normal-screen-python
    sys.stdout.write(api_key)
    # ruleid: secret-on-normal-screen-python
    stream.write(gh_password.encode())
    # ruleid: secret-on-normal-screen-python
    print(access_token(stream))
    # ruleid: secret-on-normal-screen-python
    return input("Password: ")


def fixed(token, token_name, stream):
    # ok: secret-on-normal-screen-python
    secret_screen.show(token, stream)
    # ok: secret-on-normal-screen-python
    print("token saved as", token_name)
    # ok: secret-on-normal-screen-python
    print("no password on stdin; nothing was run", file=sys.stderr)
    # ok: secret-on-normal-screen-python
    name = input("Name: ")
    # ok: secret-on-normal-screen-python
    return name, getpass.getpass("Password: ")

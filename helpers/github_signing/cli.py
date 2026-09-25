#!/usr/bin/env python3
"""Register commit-signing keys on GitHub, and prove git picks the right one.

ACCOUNTS is github_accounts as JSON, {"alias": "login", ...}. Each account's key is
SSH_DIR/github_<alias>_signing.

    python3 -m helpers.github_signing.cli register --title TITLE \\
            --ssh-dir SSH_DIR --accounts ACCOUNTS --machine-key KEY --email EMAIL
        Adds each account's key as a signing key on that account, and the machine key on
        the account with EMAIL (the global user_email) as a verified email. Each account
        is read and written with its own token, so gh's active account is never switched.
        Every key is checked (non-empty, 0600, no passphrase, its .pub matches) and every
        account read before anything is added, so a refusal from those checks adds
        nothing. Prints SIGNING-ADDED or SIGNING-PRESENT <login> <key name> per key.

    python3 -m helpers.github_signing.cli check-selection \\
            --ssh-dir SSH_DIR --accounts ACCOUNTS --fallback KEY
        In a scratch repository, git signs with each account's key for a
        github.com-<alias> remote, in scp and ssh:// form, and with the fallback for a
        plain github.com one.
        Prints SELECTION-OK.

Run from the repository root, so the package imports. Diagnostics go to stderr; stdout is
only the answer. Exit 1 on any refusal.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import stat
import subprocess
import sys
import tempfile

from helpers.github_signing import signing

HOST = "github.com"


class Refusal(Exception):
    """A reason to stop before anything more is changed."""


def _run(argv: list[str], token: str | None = None) -> subprocess.CompletedProcess[str]:
    env = {**os.environ, "GH_TOKEN": token} if token else None
    return subprocess.run(argv, capture_output=True, text=True, check=False, env=env)


def _why(done: subprocess.CompletedProcess[str]) -> str:
    return " ".join((done.stderr or done.stdout).split()) or f"exit {done.returncode}"


def _check_key(private: pathlib.Path) -> str:
    """The key's blob, once it is a 0600 file with no passphrase and a matching .pub."""
    public = pathlib.Path(f"{private}.pub")
    try:
        mode = private.stat()
    except OSError as exc:
        raise Refusal(f"{private}: {exc.strerror}") from exc
    if not stat.S_ISREG(mode.st_mode) or stat.S_IMODE(mode.st_mode) != 0o600:
        raise Refusal(f"{private} must be a regular file at mode 0600")
    if mode.st_size == 0:
        raise Refusal(
            f"{private} is empty, most likely a write that was cut short. Remove it and "
            f"{public}, and re-run the play to generate a new pair"
        )
    try:
        recorded = signing.key_blob(public.read_text(encoding="utf-8"))
    except OSError as exc:
        raise Refusal(f"{public}: {exc.strerror}") from exc
    except ValueError as exc:
        raise Refusal(f"{public}: {exc}") from exc
    derived = _run(["ssh-keygen", "-y", "-P", "", "-f", str(private)])
    if derived.returncode != 0:
        raise Refusal(
            f"{private} does not load without a passphrase ({_why(derived)}); a signing key "
            "must not have one, or every commit an agent makes would fail"
        )
    if signing.key_blob(derived.stdout) != recorded:
        raise Refusal(f"{public} does not match {private}")
    return recorded


def _token(login: str) -> str:
    done = _run(["gh", "auth", "token", "--hostname", HOST, "--user", login])
    if done.returncode != 0 or not done.stdout.strip():
        raise Refusal(
            f"gh holds no token for {login} ({_why(done)}); run scripts/gh-account-setup.bash --setup-all"
        )
    return done.stdout.strip()


def _listed(login: str, token: str, endpoint: str) -> set[str]:
    done = _run(["gh", "api", "--paginate", endpoint, "--jq", ".[].key"], token)
    if done.returncode != 0:
        raise Refusal(f"could not read {endpoint} for {login}: {_why(done)}")
    try:
        return signing.blobs(done.stdout)
    except ValueError as exc:
        raise Refusal(f"{endpoint} for {login}: {exc}") from exc


def _verified_emails(login: str, token: str) -> set[str]:
    done = _run(
        [
            "gh",
            "api",
            "--paginate",
            "user/emails",
            "--jq",
            ".[] | select(.verified) | .email",
        ],
        token,
    )
    if done.returncode != 0:
        raise Refusal(f"could not read the verified emails of {login}: {_why(done)}")
    return {line.strip() for line in done.stdout.splitlines() if line.strip()}


def _accounts(args: argparse.Namespace) -> list[tuple[str, str, str]]:
    """(alias, login, private key path) for each account in --accounts."""
    try:
        accounts = signing.parse_accounts(args.accounts)
    except ValueError as exc:
        raise Refusal(f"--accounts: {exc}") from exc
    return [
        (
            alias,
            login,
            str(pathlib.Path(args.ssh_dir) / signing.account_key_name(alias)),
        )
        for alias, login in accounts.items()
    ]


def _register(args: argparse.Namespace) -> None:
    accounts = [(login, key) for _, login, key in _accounts(args)]
    machine = pathlib.Path(args.machine_key)

    keys = {
        path: _check_key(pathlib.Path(path))
        for path in {str(machine), *(k for _, k in accounts)}
    }
    logins = list(dict.fromkeys(login for login, _ in accounts))
    tokens = {login: _token(login) for login in logins}
    verified = {login: _verified_emails(login, tokens[login]) for login in logins}
    held = {
        login: _listed(login, tokens[login], "user/ssh_signing_keys")
        for login in logins
    }
    try:
        owner = signing.owner_of_email(args.email, verified)
    except ValueError as exc:
        raise Refusal(
            f"the machine signing key has no account to go on: {exc}. Commits signed with "
            f"it carry user_email, and GitHub marks them Verified only on the account that "
            f"has it as a verified email. Verify {args.email} on the account it belongs "
            f"to and add that account to github_accounts, or set user_email to an address "
            f"one of them has verified"
        ) from exc

    wanted = [
        signing.Wanted(login, pathlib.Path(key).name, keys[key])
        for login, key in accounts
    ]
    wanted.append(signing.Wanted(owner, machine.name, keys[str(machine)]))
    missing = signing.missing_registrations(wanted, held)
    paths = {pathlib.Path(key).name: key for key in keys}
    for want in wanted:
        if want not in missing:
            print(f"SIGNING-PRESENT {want.login} {want.name}")
            continue
        done = _run(
            [
                "gh",
                "ssh-key",
                "add",
                f"{paths[want.name]}.pub",
                "--type",
                "signing",
                "--title",
                f"{args.title} {want.name}",
            ],
            tokens[want.login],
        )
        if done.returncode != 0:
            raise Refusal(
                f"could not add {want.name} as a signing key on {want.login}: {_why(done)}"
            )
        print(f"SIGNING-ADDED {want.login} {want.name}")


def _signing_key_for(repo: pathlib.Path, url: str) -> str:
    for argv in (
        ["git", "-C", str(repo), "remote", "set-url", "origin", url],
        ["git", "-C", str(repo), "config", "--get", "user.signingkey"],
    ):
        done = _run(argv)
        if done.returncode != 0:
            raise Refusal(f"{' '.join(argv[3:])} failed: {_why(done)}")
    return done.stdout.strip()


def _check_selection(args: argparse.Namespace) -> None:
    # Both URL forms a github.com-<alias> remote can take (clone-<alias>, remote-<alias>).
    expected = [
        (url, key)
        for alias, _, key in _accounts(args)
        for url in (
            f"git@{HOST}-{alias}:example/example.git",
            f"ssh://git@{HOST}-{alias}/example/example.git",
        )
    ]
    expected.append((f"git@{HOST}:example/example.git", args.fallback))
    wrong = []
    with tempfile.TemporaryDirectory() as scratch:
        repo = pathlib.Path(scratch)
        for argv in (
            ["git", "-C", scratch, "init", "-q"],
            ["git", "-C", scratch, "remote", "add", "origin", expected[-1][0]],
        ):
            done = _run(argv)
            if done.returncode != 0:
                raise Refusal(f"could not make a scratch repository: {_why(done)}")
        for url, key in expected:
            got = _signing_key_for(repo, url)
            if got != key:
                wrong.append(
                    f"a repo with remote {url} signs with {got or 'no key'}, not {key}"
                )
    if wrong:
        raise Refusal(
            "git does not pick the expected signing key:\n  " + "\n  ".join(wrong)
        )
    print("SELECTION-OK")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python3 -m helpers.github_signing.cli")
    sub = parser.add_subparsers(dest="command", required=True)
    register = sub.add_parser("register")
    register.add_argument("--title", required=True)
    register.add_argument("--machine-key", required=True)
    register.add_argument("--email", required=True)
    select = sub.add_parser("check-selection")
    select.add_argument("--fallback", required=True)
    for cmd in (register, select):
        cmd.add_argument("--ssh-dir", required=True)
        cmd.add_argument("--accounts", required=True)
    args = parser.parse_args(argv)

    try:
        if args.command == "register":
            _register(args)
        else:
            _check_selection(args)
    except Refusal as exc:
        print(f"github_signing: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Install/upgrade pyenv-managed CPython versions for play-python.yml.

Thin side-effecting wrapper around the `pyenv` binary. All version-selection
logic lives in resolver.py and is unit-tested; this module only:

  1. refreshes pyenv's definitions (`pyenv update`),
  2. reads the installable + installed version lists,
  3. asks the resolver what to install and what is stale, and
  4. runs `pyenv install -s` for each missing target, emitting markers the
     playbook keys its changed_when on.

It calls the pyenv binary directly via PYENV_ROOT (no `source ~/.bash_profile`),
so it sidesteps both the BASHRCSOURCED nounset trap and Ansible 2.19's
not-bash-aware shell-block parser.

Invoked by the playbook as a module from the repo root:

    python3 -m helpers.pyenv.install_pyenv_versions --minor-count 4
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys

from helpers.pyenv import resolver


def _pyenv_binary(pyenv_root: str) -> str:
    candidate = os.path.join(pyenv_root, "bin", "pyenv")
    if not os.path.exists(candidate):
        sys.exit(
            f"ERROR: pyenv not found at {candidate} — run the pyenv installer "
            "task first (creates ~/.pyenv)."
        )
    return candidate


def _pyenv_env(pyenv_root: str) -> dict[str, str]:
    env = dict(os.environ)
    env["PYENV_ROOT"] = pyenv_root
    env["PATH"] = os.path.join(pyenv_root, "bin") + os.pathsep + env.get("PATH", "")
    return env


def _refresh_definitions(pyenv_root: str, env: dict[str, str]) -> None:
    """Refresh python-build definitions by force-aligning the pyenv repo to upstream.

    `pyenv update` is deliberately avoided: it walks every plugin and its
    `git merge origin` step fails on modern git / detached-HEAD checkouts with
    "merge: origin - not something we can merge", aborting the whole run.

    pyenv is a vendored upstream repo that should mirror origin exactly, so we
    `fetch` then `reset --hard` the main repo (which bundles python-build):

    - reset cannot hit the "unmergeable" wall — it works on a detached HEAD,
      a diverged branch, or a master/main rename that a merge cannot.
    - reset only rewrites *tracked* core files (incl. the definitions); it leaves
      untracked data — built `versions/`, `shims/`, and separately-cloned plugins
      — untouched. That is why this is preferred over nuke-and-reclone: same
      pristine-upstream result, without discarding compiled Pythons.

    If `~/.pyenv` exists but is not a git checkout, that is a broken install the
    installer task owns — fail loudly rather than silently re-cloning.
    """
    if not os.path.isdir(os.path.join(pyenv_root, ".git")):
        sys.exit(
            f"ERROR: {pyenv_root} is not a git checkout — remove it and re-run the "
            "pyenv installer task (the 'Pyenv Python Version Management' play step)."
        )
    git = ["git", "-C", pyenv_root]
    subprocess.run([*git, "fetch", "--quiet", "origin"], check=True, text=True, env=env)
    subprocess.run([*git, "reset", "--hard", "FETCH_HEAD"], check=True, text=True, env=env)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--minor-count",
        type=int,
        default=4,
        help="Number of newest CPython minor lines to install (default: 4).",
    )
    parser.add_argument(
        "--pyenv-root",
        default=os.environ.get("PYENV_ROOT", os.path.expanduser("~/.pyenv")),
        help="pyenv installation root (default: $PYENV_ROOT or ~/.pyenv).",
    )
    args = parser.parse_args(argv)

    pyenv = _pyenv_binary(args.pyenv_root)
    env = _pyenv_env(args.pyenv_root)

    # Refresh definitions so newly released minors/patches are known. Fail fast:
    # check=True turns any git error into a non-zero exit the playbook will see.
    _refresh_definitions(args.pyenv_root, env)

    listing = subprocess.run(
        [pyenv, "install", "--list"], check=True, text=True, capture_output=True, env=env
    ).stdout
    installed = subprocess.run(
        [pyenv, "versions", "--bare"], check=True, text=True, capture_output=True, env=env
    ).stdout

    plan = resolver.build_install_plan(listing, installed, args.minor_count)

    for minor in plan.stale_kept:
        print(
            f"PYENV-WARN: keeping installed Python {minor} "
            f"(older than the newest {args.minor_count} minors, not auto-removed)"
        )

    versions_dir = os.path.join(args.pyenv_root, "versions")
    changed = 0
    for version in plan.to_install:
        if os.path.isdir(os.path.join(versions_dir, version)):
            continue  # already built; -s would no-op but skip the call entirely
        subprocess.run([pyenv, "install", "-s", version], check=True, text=True, env=env)
        print(f"PYENV-CHANGED: installed {version}")
        changed += 1

    print(f"PYENV-DONE changed={changed}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

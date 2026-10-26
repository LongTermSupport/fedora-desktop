# Fixture for .semgrep/hook-handlers.yml. `ruleid:` marks a line that must fire, `ok:` a line
# that must stay silent.

import os
from pathlib import Path

# hook-path-resolved-against-cwd judges only a module that imports the hooks daemon
from claude_code_hooks_daemon.core import Handler


def system_path_judged_from_cwd(file_path, blocked):
    # The originating instance in system_paths.py: a relative path judged where the daemon runs.
    # ruleid: hook-path-resolved-against-cwd
    return os.path.realpath(file_path).startswith(blocked)


def other_cwd_resolutions(file_path, env_root):
    # ruleid: hook-path-resolved-against-cwd
    absolute = os.path.abspath(file_path)
    # ruleid: hook-path-resolved-against-cwd
    here = os.getcwd()
    # ruleid: hook-path-resolved-against-cwd
    root = Path(env_root).resolve()
    # ruleid: hook-path-resolved-against-cwd
    return absolute, here, root, Path.cwd()


def hollow_guard(file_path, other):
    if not os.path.isabs(file_path):
        pass
    # ruleid: hook-path-resolved-against-cwd
    resolved = os.path.realpath(file_path)
    if not os.path.isabs(other):
        raise ValueError(other)
    # ruleid: hook-path-resolved-against-cwd
    return resolved, os.getcwd()


def chained_resolutions(file_path):
    # ruleid: hook-path-resolved-against-cwd
    home = Path(file_path).expanduser().resolve()
    # ruleid: hook-path-resolved-against-cwd
    parent = Path(file_path).parent.absolute()
    # ok: hook-path-resolved-against-cwd
    here = Path(__file__).parent.resolve()
    return home, parent, here


def not_a_path(future):
    # ok: hook-path-resolved-against-cwd
    return future.resolve()


def anchored(file_path, project_root):
    if not os.path.isabs(file_path):
        file_path = os.path.join(project_root, file_path)
    # ok: hook-path-resolved-against-cwd
    return os.path.realpath(file_path)


def env_root_required_absolute(env_root):
    if not os.path.isabs(env_root):
        raise RuntimeError(f"CLAUDE_PROJECT_DIR={env_root!r} is not absolute")
    # ok: hook-path-resolved-against-cwd
    return Path(env_root).resolve()


class FixtureHandler(Handler):
    # ok: hook-path-resolved-against-cwd
    HERE = Path(__file__).resolve().parents[1]

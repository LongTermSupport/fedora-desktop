"""HiddenGitMutationHandler judges the commands a line runs, not the words it holds (issue #294).

`bash -c "$(cat f)"` and `eval "$(< f)"` name a script file by a regex over the command line.
Run over the whole text, that regex also fires on an echo, a grep pattern or a commit message
that merely quotes the form. Each command here quotes a real uncommitted script holding a git
mutation and runs nothing; the same forms outside the quotes stay denied.
"""

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parent.parent))

from hidden_git_mutation import HiddenGitMutationHandler

MUTATING_SCRIPT = "#!/usr/bin/env bash\ngit push origin main\n"


@pytest.fixture
def handler() -> HiddenGitMutationHandler:
    return HiddenGitMutationHandler()


@pytest.fixture
def cwd(tmp_path: Path) -> Path:
    """A directory holding an uncommitted script with a git mutation (outside any repository)."""
    script = tmp_path / "rescue.bash"
    script.write_text(MUTATING_SCRIPT)
    script.chmod(0o755)
    return tmp_path


def _bash(command: str, cwd: Path) -> dict:
    return {"tool_name": "Bash", "tool_input": {"command": command}, "cwd": str(cwd)}


@pytest.mark.parametrize(
    "command",
    [
        "echo 'bash -c \"$(cat rescue.bash)\"'",
        "echo 'eval \"$(< rescue.bash)\"'",
        "printf '%s\\n' 'sh -c \"$(cat rescue.bash)\"'",
        "grep 'bash -c \"$(cat rescue.bash)\"' notes.md",
        "git commit -m 'run bash -c \"$(cat rescue.bash)\" by hand'",
        "gh issue comment 1 --body 'eval \"$(< rescue.bash)\"'",
    ],
)
def test_text_that_quotes_a_substituted_script_runs_nothing(handler, cwd, command):
    assert handler.matches(_bash(command, cwd)) is False


@pytest.mark.parametrize(
    "command",
    [
        'bash -c "$(cat rescue.bash)"',
        'eval "$(< rescue.bash)"',
        'true && bash -c "$(cat rescue.bash)"',
        'echo start; eval "$(< rescue.bash)"',
        'sudo bash -c "$(cat rescue.bash)"',
    ],
)
def test_a_substituted_script_that_runs_is_still_denied(handler, cwd, command):
    assert handler.matches(_bash(command, cwd)) is True

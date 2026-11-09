"""Tests for HiddenGitMutationHandler: an untracked script holding git mutations is not run.

The red proof is RESCUE_FIXTURE, an anonymised copy of the scratch script that ran
`git add -A`, `git commit` and `git push -u` across every worktree (Plan 00250 TRIAGE cause 1).
Each test builds its own repository under tmp_path, so the fixture is untracked there exactly
as the original was, and committing it there is the green half.
"""

import os
import subprocess
import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parent.parent))

from hidden_git_mutation import RULE_ID, HiddenGitMutationHandler

# The originating instance, anonymised: worktree names and the list file are placeholders.
RESCUE_FIXTURE = """#!/usr/bin/env bash
set -uo pipefail
while IFS='|' read -r w b nond total rec; do
  case "$w" in *worktree-A*|*worktree-B*) continue;; esac
  [ -n "${rec#recent=}" ] && { echo "SKIP-RECENT|$w"; continue; }
  nd=${nond#nonD=}; t=${total#total=}
  if [ "$((t-nd))" -gt 50 ]; then git -C "$w" add --ignore-removal . || { echo "REFUSED-ADD|$b"; continue; }
  else git -C "$w" add -A || { echo "REFUSED-ADD|$b"; continue; }; fi
  if git -C "$w" diff --cached --quiet; then echo "NOSTAGED|$b"; continue; fi
  if out=$(git -C "$w" commit -F wip-msg.txt 2>&1); then
    if pout=$(git -C "$w" push -u origin "$b" 2>&1); then echo "OK|$b"; else echo "REFUSED-PUSH|$b"; fi
  else echo "REFUSED-COMMIT|$b"; fi
done < wt-classify.txt
echo DONE
"""

READ_ONLY_SCRIPT = """#!/usr/bin/env bash
set -euo pipefail
git status --short
git log --oneline -5
git stash list
git branch --list
git worktree list
git diff --cached --quiet
"""

_GIT_ENV = {
    "GIT_AUTHOR_NAME": "Test",
    "GIT_AUTHOR_EMAIL": "test@example.com",
    "GIT_COMMITTER_NAME": "Test",
    "GIT_COMMITTER_EMAIL": "test@example.com",
}


def _git(repo: Path, *args: str) -> None:
    subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        env={**os.environ, **_GIT_ENV},
    )


@pytest.fixture
def repo(tmp_path: Path) -> Path:
    """An empty git repository with one commit."""
    root = tmp_path / "repo"
    root.mkdir()
    _git(root, "init", "-q")
    (root / "README").write_text("x\n")
    _git(root, "add", "README")
    _git(root, "commit", "-q", "-m", "init")
    return root


@pytest.fixture
def handler() -> HiddenGitMutationHandler:
    return HiddenGitMutationHandler()


def _write(path: Path, text: str) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)
    path.chmod(0o755)
    return path


def _bash(command: str, cwd: Path) -> dict:
    return {"tool_name": "Bash", "tool_input": {"command": command}, "cwd": str(cwd)}


class TestRedOnTheOriginatingInstance:
    def test_rescue_copy_untracked_is_denied(self, handler, repo):
        _write(repo / "untracked/scratch/rescue.bash", RESCUE_FIXTURE)
        hook_input = _bash("bash untracked/scratch/rescue.bash --help", repo)
        assert handler.matches(hook_input) is True
        result = handler.handle(hook_input)
        assert result.decision == "deny"
        assert f"BLOCKED [{RULE_ID}]" in result.reason
        assert "untracked/scratch/rescue.bash" in result.reason
        assert "cannot see" in result.reason
        assert 'git -C "$w" add -A' in result.reason
        assert 'git -C "$w" push -u' in result.reason
        assert "directly" in result.reason
        assert "tracked tool" in result.reason

    def test_rescue_copy_outside_any_repository_is_denied(self, handler, tmp_path):
        _write(tmp_path / "loose/rescue.bash", RESCUE_FIXTURE)
        assert handler.matches(_bash("bash loose/rescue.bash", tmp_path)) is True


class TestGreenOnATrackedTool:
    def test_tracked_script_with_git_verbs_is_allowed(self, handler, repo):
        _write(repo / "scripts/release.bash", RESCUE_FIXTURE)
        _git(repo, "add", "scripts/release.bash")
        _git(repo, "commit", "-q", "-m", "tool")
        assert handler.matches(_bash("bash scripts/release.bash", repo)) is False
        assert handler.matches(_bash("./scripts/release.bash", repo)) is False

    def test_this_projects_own_tracked_tool_is_allowed(self, handler):
        project = Path(__file__).resolve().parents[5]
        tool = project / "scripts/release.bash"
        assert "git push" in tool.read_text()
        assert handler.matches(_bash("bash scripts/release.bash --help", project)) is False


class TestEverySpellingOfRunningAScript:
    @pytest.mark.parametrize(
        "command",
        [
            "bash rescue.bash",
            "sh rescue.bash",
            "./rescue.bash",
            "source rescue.bash",
            ". rescue.bash",
            "bash -x ./rescue.bash arg",
            "bash -o pipefail rescue.bash",
            "bash -- rescue.bash",
            "/usr/bin/bash rescue.bash",
            "env bash rescue.bash",
            "FOO=1 bash rescue.bash",
            "timeout 60 bash rescue.bash 2>&1 | cat",
            "nohup ./rescue.bash &",
            "true && bash rescue.bash",
            "bash -c 'source ./rescue.bash'",
            "cd sub && cd .. && bash rescue.bash",
            "bash < rescue.bash",
            "bash <rescue.bash",
            "cat rescue.bash | bash",
            "cat rescue.bash | sh -s",
            'bash -c "$(cat rescue.bash)"',
            'eval "$(< rescue.bash)"',
            "timeout -s KILL 5 bash rescue.bash",
            "timeout --signal=KILL 5 bash rescue.bash",
            "nice -n 10 bash rescue.bash",
            "sudo -u nobody bash rescue.bash",
            "env -u FOO bash rescue.bash",
            "stdbuf -oL bash rescue.bash",
            "flock /tmp/lock bash rescue.bash",
            "(cd sub && bash ../rescue.bash)",
            "cd -P sub && bash ../rescue.bash",
            "{ bash rescue.bash; }",
        ],
    )
    def test_spelling_is_denied(self, handler, repo, command):
        _write(repo / "rescue.bash", RESCUE_FIXTURE)
        (repo / "sub").mkdir()
        assert handler.matches(_bash(command, repo)) is True

    def test_cd_into_the_script_directory_is_followed(self, handler, repo):
        _write(repo / "untracked/scratch/rescue.bash", RESCUE_FIXTURE)
        assert handler.matches(_bash("cd untracked/scratch && ./rescue.bash", repo)) is True

    def test_absolute_path_is_denied(self, handler, repo, tmp_path):
        script = _write(repo / "rescue.bash", RESCUE_FIXTURE)
        assert handler.matches(_bash(f"bash {script}", tmp_path)) is True

    def test_syntax_check_only_is_allowed(self, handler, repo):
        _write(repo / "rescue.bash", RESCUE_FIXTURE)
        assert handler.matches(_bash("bash -n rescue.bash", repo)) is False

    def test_a_script_run_by_the_script_is_judged(self, handler, repo):
        _write(repo / "inner.bash", RESCUE_FIXTURE)
        _write(repo / "outer.bash", "#!/usr/bin/env bash\nbash ./inner.bash\n")
        assert handler.matches(_bash("bash outer.bash", repo)) is True

    def test_a_staged_but_uncommitted_script_is_denied(self, handler, repo):
        _write(repo / "rescue.bash", RESCUE_FIXTURE)
        _git(repo, "add", "rescue.bash")
        assert handler.matches(_bash("bash rescue.bash", repo)) is True

    @pytest.mark.parametrize(
        "command",
        [
            "printf 'git push origin x\\n' > f.bash && bash f.bash",
            "cat > f.bash <<'EOF'\ngit add -A\ngit commit -m x\nEOF\nbash f.bash",
        ],
    )
    def test_a_script_written_by_the_same_command_is_left_to_the_command_guards(self, handler, repo, command):
        """Its text is on the command line, and the daemon's command view reads it as commands."""
        from claude_code_hooks_daemon.utils.command_position import command_position_segments

        assert handler.matches(_bash(command, repo)) is False
        assert any("git" in segment for segment in command_position_segments(command))


class TestEveryMutationVerb:
    @pytest.mark.parametrize(
        "line",
        [
            "git add -A",
            "git commit -m x",
            "git push -u origin b",
            "git reset HEAD~1",
            "git branch -d old",
            "git branch -D old",
            "git branch --delete old",
            "git worktree add ../w b",
            "git worktree remove ../w",
            "git checkout main",
            "git switch main",
            "git restore file",
            "git rm --cached file",
            "git stash",
            "git stash push -m x",
            "git merge --no-ff b",
            "git rebase main",
            "git update-ref -d refs/heads/b",
            "git clean -fd",
            'git -C "$w" add -A',
            "git -c core.hooksPath=/dev/null commit -m x",
            "git cherry-pick abc123",
            "git revert abc123",
            "git pull --ff-only",
            "git tag -d v1",
            "git tag v1",
            "git branch new-work",
            "git branch -f main abc123",
            "git branch -M old new",
            "git worktree prune",
            "git am patch.mbox",
            "git apply --index p.diff",
            "git mv a b",
            "git reflog expire --expire=now --all",
            "git gc --prune=now",
            "git filter-branch --tree-filter x HEAD",
            "/usr/bin/git push origin b",
            '"git" push origin b',
            'if out=$(git -C "$w" commit -F m 2>&1); then echo ok; fi',
            'git -C "$w" \\\n  push -u origin "$b"',
            "G=git\n$G push origin b",
            'GIT=/usr/bin/git\n"${GIT}" -C x commit -m y',
            'g() { git -C "$w" "$@"; }\ng push origin b',
        ],
    )
    def test_verb_is_denied(self, handler, repo, line):
        _write(repo / "s.bash", f"#!/usr/bin/env bash\n{line}\n")
        assert handler.matches(_bash("bash s.bash", repo)) is True

    @pytest.mark.parametrize(
        "line",
        [
            'echo "then run: git push origin --delete old"',
            "printf '%s\\n' 'git commit -m x'",
            "git push --dry-run origin x",
            "git tag -l",
            "git branch -a",
            "git branch --show-current",
            "git fetch origin",
            "git worktree list",
        ],
    )
    def test_hazard_free_line_is_allowed(self, handler, repo, line):
        _write(repo / "s.bash", f"#!/usr/bin/env bash\n{line}\n")
        assert handler.matches(_bash("bash s.bash", repo)) is False


class TestPythonWrappingGit:
    @pytest.mark.parametrize(
        "line",
        [
            'subprocess.run(["git", "add", "-A"], check=True)',
            'subprocess.run(["git", "-C", repo, "push", "-u", "origin", b], check=True)',
            "subprocess.run(['git', 'worktree', 'remove', path], check=True)",
            'subprocess.run(["git", "branch", "-D", b], check=True)',
            'subprocess.run(["git", "--no-pager", "push"], check=True)',
            'subprocess.run(\n    [\n        "git",\n        "commit",\n        "-m",\n        "x",\n    ],\n    check=True,\n)',
            'run_shell("git push origin b")',
        ],
    )
    def test_python_subprocess_git_mutation_is_denied(self, handler, repo, line):
        _write(repo / "p.py", f"import subprocess\n{line}\n")
        assert handler.matches(_bash("python3 p.py", repo)) is True
        assert handler.matches(_bash("python p.py --dry-run", repo)) is True

    def test_python_git_helper_function_is_denied(self, handler, repo):
        _write(
            repo / "p.py",
            "import subprocess\n\n\ndef git(cwd, *args):\n"
            '    subprocess.run(["git", *args], cwd=cwd, check=True)\n\n\n'
            'git(".", "init", "-q")\ngit(".", "add", ".gitignore")\n',
        )
        assert handler.matches(_bash("python3 p.py", repo)) is True

    def test_python_git_helper_with_only_reads_is_allowed(self, handler, repo):
        _write(
            repo / "p.py",
            "import subprocess\n\n\ndef git(*args):\n"
            '    return subprocess.run(["git", *args], check=True)\n\n\ngit("status")\ngit("log", "-1")\n',
        )
        assert handler.matches(_bash("python3 p.py", repo)) is False

    def test_python_read_only_git_is_allowed(self, handler, repo):
        _write(repo / "p.py", 'import subprocess\nsubprocess.run(["git", "stash", "list"], check=True)\n'
               'subprocess.run(["git", "worktree", "list"], check=True)\n')
        assert handler.matches(_bash("python3 p.py", repo)) is False

    def test_python_module_and_inline_are_not_files(self, handler, repo):
        assert handler.matches(_bash("python3 -m pytest", repo)) is False
        assert handler.matches(_bash("python3 -c 'print(1)'", repo)) is False


class TestWhatDoesNotCarryTheHazard:
    def test_read_only_git_script_is_allowed(self, handler, repo):
        _write(repo / "s.bash", READ_ONLY_SCRIPT)
        assert handler.matches(_bash("bash s.bash", repo)) is False

    def test_git_verb_only_in_a_comment_is_allowed(self, handler, repo):
        _write(repo / "s.bash", "#!/usr/bin/env bash\n# then git push it by hand\necho hi\n")
        assert handler.matches(_bash("bash s.bash", repo)) is False

    def test_script_without_git_is_allowed(self, handler, repo):
        _write(repo / "s.bash", "#!/usr/bin/env bash\necho hi\n")
        assert handler.matches(_bash("bash s.bash", repo)) is False

    def test_inline_git_is_left_to_the_command_guards(self, handler, repo):
        assert handler.matches(_bash("git add README && git commit -m x", repo)) is False
        assert handler.matches(_bash("bash -c 'git add -A'", repo)) is False

    def test_mention_of_a_script_in_data_is_allowed(self, handler, repo):
        _write(repo / "rescue.bash", RESCUE_FIXTURE)
        assert handler.matches(_bash("echo bash rescue.bash", repo)) is False
        assert handler.matches(_bash("cat rescue.bash", repo)) is False

    def test_missing_script_is_allowed(self, handler, repo):
        assert handler.matches(_bash("bash nowhere.bash", repo)) is False

    def test_other_tools_are_ignored(self, handler, repo):
        _write(repo / "rescue.bash", RESCUE_FIXTURE)
        hook_input = {"tool_name": "Write", "tool_input": {"file_path": str(repo / "rescue.bash")}}
        assert handler.matches(hook_input) is False


class TestHandlerContract:
    def test_rule_is_declared(self, handler):
        assert [rule.rule_id for rule in handler.get_rules()] == [RULE_ID]

    def test_acceptance_tests_cover_deny_and_allow(self, handler):
        decisions = [test.expected_decision for test in handler.get_acceptance_tests()]
        assert decisions == ["deny", "allow"]

    def test_is_terminal_safety_guard(self, handler):
        assert handler.terminal is True

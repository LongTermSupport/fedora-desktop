# Fixture for .semgrep/hook-handlers.yml. `ruleid:` marks a line that must fire, `ok:` a line
# that must stay silent.

import fnmatch
import os
import re
from pathlib import Path

# hook-path-resolved-against-cwd judges only a module that imports the hooks daemon
from claude_code_hooks_daemon.core import Handler
from claude_code_hooks_daemon.core.utils import get_bash_command

# ok: hook-sanctioned-name-impostor
from claude_code_hooks_daemon.utils.command_position import (
    command_position_code_segments,
    command_position_segments,
    is_plain_data_command,
)
# ok: hook-sanctioned-name-impostor
from _raw_command_judge import judge_raw_command_line


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


_FORBIDDEN = re.compile(r"dnf\s+install")


def originating_instance(hook_input):
    # The originating instance in ansible_enforcement.py: a regex over the whole command line.
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return bool(_FORBIDDEN.search(command))


def every_regex_method(hook_input):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    first = _FORBIDDEN.match(command)
    # ruleid: hook-regex-over-raw-command
    found = re.findall(r"git\s+stash", command)
    # ruleid: hook-regex-over-raw-command
    replaced = _FORBIDDEN.sub("", command)
    # ruleid: hook-regex-over-raw-command
    parts = re.split(r";", command)
    return first, found, replaced, parts


def a_generator_over_the_patterns(hook_input, patterns):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return any(pattern.search(command) for pattern in patterns)


def a_substring_test(hook_input):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return "dnf install" in command


def one_assignment_away(hook_input):
    command = get_bash_command(hook_input)
    text = command
    # ruleid: hook-regex-over-raw-command
    return _FORBIDDEN.search(text)


def straight_from_the_call(hook_input):
    # ruleid: hook-regex-over-raw-command
    return _FORBIDDEN.search(get_bash_command(hook_input))


def judged_by_segment(hook_input, patterns):
    command = get_bash_command(hook_input)
    # ok: hook-regex-over-raw-command
    return any(pattern.search(segment) for segment in command_position_code_segments(command) for pattern in patterns)


def segments_held_in_a_variable(hook_input):
    command = get_bash_command(hook_input)
    segments = command_position_segments(command)
    for segment in segments:
        # ok: hook-regex-over-raw-command
        if _FORBIDDEN.search(segment):
            return True
    return False


def text_that_is_not_the_command(script_text):
    # ok: hook-regex-over-raw-command
    return _FORBIDDEN.search(script_text)


def a_helper_of_its_own_that_judges_what_it_is_given(hook_input, judge):
    # A helper that takes the line under any parameter name is the escape the rule closes: the
    # line leaves the function, so the call is the finding.
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return judge(command)


def _scan(text):
    # ok: hook-regex-over-raw-command
    return _FORBIDDEN.search(text)


def renamed_parameter_hides_the_search(hook_input):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return _scan(command)


def helper_called_on_the_call_itself(hook_input):
    # ruleid: hook-regex-over-raw-command
    return _scan(get_bash_command(hook_input))


class FixtureJudge:
    def _judge(self, line):
        # ok: hook-regex-over-raw-command
        return _FORBIDDEN.search(line)

    def method_helper_gets_the_line(self, hook_input):
        command = get_bash_command(hook_input)
        # ruleid: hook-regex-over-raw-command
        return self._judge(command)


def read_from_tool_input_without_the_helper(hook_input):
    command = hook_input["tool_input"]["command"]
    # ruleid: hook-regex-over-raw-command
    return _FORBIDDEN.search(command)


def read_from_tool_input_with_get(hook_input):
    command = hook_input.get("tool_input", {}).get("command", "")
    # ruleid: hook-regex-over-raw-command
    return _FORBIDDEN.search(command)


def the_one_sanctioned_judge(hook_input, search):
    # judge_raw_command_line is the one function that judges the line as written, on purpose, with
    # its reason in its docstring: a visible exception, not a gap.
    command = get_bash_command(hook_input)
    # ok: hook-regex-over-raw-command
    return judge_raw_command_line(command, search)


def find_over_the_line(hook_input):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return command.find("dnf install") >= 0


def count_over_the_line(hook_input):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return command.count("dnf")


def index_and_partition_over_the_line(hook_input):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    head = command.partition(" ")
    # ruleid: hook-regex-over-raw-command
    return head, command.rfind("&&")


def fnmatch_over_the_line(hook_input):
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return fnmatch.fnmatch(command, "*dnf install*")


def two_step_read_of_tool_input(hook_input):
    tool_input = hook_input["tool_input"]
    command = tool_input["command"]
    # ruleid: hook-regex-over-raw-command
    return _FORBIDDEN.search(command)


def two_step_read_of_tool_input_with_get(hook_input):
    tool_input = hook_input.get("tool_input", {})
    command = tool_input.get("command", "")
    # ruleid: hook-regex-over-raw-command
    return _FORBIDDEN.search(command)


def a_look_alike_judge_in_another_module(text, search):
    # ok: hook-regex-over-raw-command
    return search(text)


def a_renamed_look_alike_of_the_sanctioned_judge(hook_input):
    # The judge's own module is excluded by path, so a copy of it anywhere else is judged: the
    # call that hands it the line is the finding, whatever the copy is called.
    command = get_bash_command(hook_input)
    # ruleid: hook-regex-over-raw-command
    return a_look_alike_judge_in_another_module(command, _FORBIDDEN.search)


# ruleid: hook-sanctioned-name-impostor
def command_position_clean(text):
    return text


# ruleid: hook-sanctioned-name-impostor
command_position_all_clean = lambda text: text


class ImpostorJudges:
    # ruleid: hook-sanctioned-name-impostor
    def judge_raw_command_line(self, text, search):
        return search(text)

    # ruleid: hook-sanctioned-name-impostor
    def is_plain_data_command(self, text):
        return True


# ruleid: hook-sanctioned-name-impostor
from elsewhere import sneaky as command_position_segments_2

# ruleid: hook-sanctioned-name-impostor
from my_helpers import command_position_code_segments_2

# ruleid: hook-sanctioned-name-impostor
from ansible_enforcement import judge_raw_command_line as the_judge


def the_daemons_whole_line_predicate_is_not_a_helper(hook_input):
    command = get_bash_command(hook_input)
    # ok: hook-regex-over-raw-command
    return is_plain_data_command(command)


def builtins_and_messages_are_not_helpers(hook_input, result):
    command = get_bash_command(hook_input)
    # ok: hook-regex-over-raw-command
    size = len(command)
    # ok: hook-regex-over-raw-command
    return result.deny(f"Command: {command}"), size


def no_command_text(hook_input):
    path = hook_input.get("file_path", "")
    # ok: hook-regex-over-raw-command
    return "/etc/" in path


def helper_over_a_command_parameter(command, cwd):
    # ruleid: hook-regex-over-command-parameter
    return _FORBIDDEN.findall(command)


def typed_helper_over_a_command_parameter(command: str) -> bool:
    # ruleid: hook-regex-over-command-parameter
    if re.search(r"git\s+stash", command):
        return True
    # ruleid: hook-regex-over-command-parameter
    return "dnf install" in command


def helper_over_a_segment_parameter(segment: str) -> bool:
    # ok: hook-regex-over-command-parameter
    return bool(_FORBIDDEN.search(segment))


def a_text_scanner_names_its_parameter_for_what_it_is(text: str):
    # ok: hook-regex-over-command-parameter
    return _FORBIDDEN.match(text)


def command_is_only_passed_on(command: str, judge):
    # ok: hook-regex-over-command-parameter
    return judge(command)

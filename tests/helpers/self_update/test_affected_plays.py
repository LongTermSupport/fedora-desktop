"""Tests for helpers.self_update.affected_plays — which plays a set of changed paths touches.

A play's own file is not all it deploys: a change to the ccy lib or launcher changes what
`play-claude-yolo.yml` puts on the host (Plan 00137 research §2). This maps changed paths
to the plays that deploy them, then splits the result by the unattended allowlist (D2).

A reference the mapper cannot follow is REPORTED, never dropped: a missed dependency is a
play that silently never re-runs, which is exactly the defect this plan exists to remove.
"""

from __future__ import annotations

import io
import json
import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import affected_plays as ap

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
CLAUDE_PLAY = "playbooks/imports/play-claude-yolo.yml"
ROOT = "{{ root_dir }}"
CONFIG_ROOT = "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
FREEZE_PLAYS = (
    "playbooks/imports/optional/common/play-podfreeze.yml",
    "playbooks/imports/optional/common/play-lxcfreeze.yml",
)


def _go_list_deps(command: str) -> list[str]:
    """The repo-relative directories of every non-standard package the command at the
    repo-relative `command` (`src-go/cmd/<name>`) builds from, as the Go toolchain lists them
    (vendored ones included), with the build's GOFLAGS."""
    module, package = command.split("/", 1)
    env = {**os.environ, "GOFLAGS": "-mod=vendor", "GOTOOLCHAIN": "local"}
    result = subprocess.run(
        ["go", "list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}", "./" + package],
        cwd=os.path.join(REPO_ROOT, module),
        env=env,
        capture_output=True,
        text=True,
        check=True,
    )
    return [os.path.relpath(line, REPO_ROOT) for line in result.stdout.splitlines() if line]


def _write(root: str, rel: str, text: str) -> None:
    path = os.path.join(root, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(textwrap.dedent(text))


class Fixture:
    """A throwaway repo shaped like this one: playbooks/imports/, files/, tasks/, helpers/."""

    def __init__(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = self._tmp.name

    def play(self, rel: str, body: str) -> str:
        _write(self.root, rel, body)
        return rel

    def file(self, rel: str, text: str = "x\n") -> str:
        _write(self.root, rel, text)
        return rel

    def allow(self, plays: list[str]) -> None:
        _write(self.root, ap.ALLOWLIST_PATH, json.dumps(plays))

    def close(self) -> None:
        self._tmp.cleanup()


class FixtureCase(unittest.TestCase):
    def setUp(self) -> None:
        self.fx = Fixture()
        self.addCleanup(self.fx.close)


class TestInputs(FixtureCase):
    def test_a_literal_root_dir_path_is_an_exact_input(self) -> None:
        self.fx.file("files/bin/tool")
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/bin/tool"
        """)
        inputs = ap.play_inputs(self.fx.root, play)
        self.assertIn("files/bin/tool", inputs.exact)
        self.assertEqual(inputs.unresolved, [])

    def test_the_config_file_lookup_is_the_same_root(self) -> None:
        self.fx.file("vars/list.yml")
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              vars_files:
                - "{CONFIG_ROOT}/vars/list.yml"
        """)
        self.assertIn("vars/list.yml", ap.play_inputs(self.fx.root, play).exact)

    def test_a_directory_input_covers_everything_under_it(self) -> None:
        self.fx.file("files/plugins/one.json")
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/plugins/"
        """)
        self.assertTrue(ap.affects(ap.play_inputs(self.fx.root, play), "files/plugins/new/two.json"))

    def test_an_existing_directory_without_a_slash_is_still_a_directory(self) -> None:
        self.fx.file("extensions/ext@x/extension.js")
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              vars:
                extension_src: "{ROOT}/extensions/ext@x"
        """)
        self.assertTrue(ap.affects(ap.play_inputs(self.fx.root, play), "extensions/ext@x/extension.js"))

    def test_a_templated_tail_maps_to_its_literal_prefix(self) -> None:
        """Conservative: every file under the prefix counts, because the loop that fills
        the template in is not something a regex can evaluate."""
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/home/.local/bin/{{{{ item }}}}"
        """)
        inputs = ap.play_inputs(self.fx.root, play)
        self.assertTrue(ap.affects(inputs, "files/home/.local/bin/anything"))
        self.assertFalse(ap.affects(inputs, "files/home/.local/share/other"))

    def test_a_full_line_comment_is_not_an_input(self) -> None:
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              # formerly deployed {ROOT}/files/old-thing
              tasks: []
        """)
        self.assertNotIn("files/old-thing", ap.play_inputs(self.fx.root, play).exact)

    def test_included_task_files_are_followed_transitively(self) -> None:
        self.fx.file("files/lib/shared-lib.bash")
        self.fx.file("tasks/deploy-shared-lib.yml", f"""
            - ansible.builtin.copy:
                src: "{ROOT}/files/lib/shared-lib.bash"
        """)
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.include_tasks: "{ROOT}/tasks/deploy-shared-lib.yml"
        """)
        inputs = ap.play_inputs(self.fx.root, play)
        self.assertIn("tasks/deploy-shared-lib.yml", inputs.exact)
        self.assertIn("files/lib/shared-lib.bash", inputs.exact)

    def test_import_playbook_is_relative_to_the_importing_file(self) -> None:
        self.fx.file("files/x")
        self.fx.play("playbooks/imports/play-b.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/x"
        """)
        play = self.fx.play("playbooks/imports/play-a.yml", """
            - import_playbook: play-b.yml
        """)
        inputs = ap.play_inputs(self.fx.root, play)
        self.assertIn("playbooks/imports/play-b.yml", inputs.exact)
        self.assertIn("files/x", inputs.exact)

    def test_a_helper_module_brings_its_whole_package_and_its_imports(self) -> None:
        self.fx.file("helpers/alpha/cli.py", "from helpers.beta import core\n")
        self.fx.file("helpers/beta/core.py", "X = 1\n")
        play = self.fx.play("playbooks/imports/play-a.yml", """
            - hosts: desktop
              tasks:
                - ansible.builtin.command:
                    argv:
                      - python3
                      - -m
                      - helpers.alpha.cli
        """)
        inputs = ap.play_inputs(self.fx.root, play)
        self.assertTrue(ap.affects(inputs, "helpers/alpha/cli.py"))
        self.assertTrue(ap.affects(inputs, "helpers/beta/core.py"))

    def test_a_dotted_word_that_is_not_a_helper_package_is_ignored(self) -> None:
        play = self.fx.play("playbooks/imports/play-a.yml", """
            - hosts: desktop
              # see helpers.yml and helpers.bash
              tasks:
                - ansible.builtin.debug:
                    msg: "helpers.github is not a package"
        """)
        self.assertEqual(ap.play_inputs(self.fx.root, play).prefixes, set())

    def test_the_play_file_itself_is_an_input(self) -> None:
        play = self.fx.play("playbooks/imports/play-a.yml", "- hosts: desktop\n")
        self.assertTrue(ap.affects(ap.play_inputs(self.fx.root, play), play))


class TestUnresolved(FixtureCase):
    def _unresolved(self, body: str) -> list[str]:
        play = self.fx.play("playbooks/imports/play-a.yml", body)
        return ap.play_inputs(self.fx.root, play).unresolved

    def test_a_src_from_an_unknown_variable_is_reported(self) -> None:
        found = self._unresolved("""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{{ mystery }}/thing"
        """)
        self.assertEqual(len(found), 1)
        self.assertIn("{{ mystery }}/thing", found[0])

    def test_a_src_from_a_root_dir_variable_is_not_reported(self) -> None:
        self.assertEqual(self._unresolved(f"""
            - hosts: desktop
              vars:
                extension_src: "{ROOT}/extensions/ext"
              tasks:
                - ansible.builtin.copy:
                    src: "{{{{ extension_src }}}}/{{{{ item }}}}"
        """), [])

    def test_a_src_from_a_url_variable_is_not_reported(self) -> None:
        self.assertEqual(self._unresolved("""
            - hosts: desktop
              vars:
                toolUrl: "https://example.com/{{ version }}/tool.tar.gz"
              tasks:
                - ansible.builtin.unarchive:
                    src: "{{ toolUrl }}"
        """), [])

    def test_host_paths_urls_loop_items_and_registered_results_are_not_reported(self) -> None:
        self.assertEqual(self._unresolved("""
            - hosts: desktop
              tasks:
                - ansible.builtin.tempfile:
                    state: directory
                  register: download_dir
                - ansible.builtin.copy:
                    src: /usr/local/bin/thing
                    remote_src: true
                - ansible.builtin.copy:
                    src: "/home/{{ user_login }}/.bashrc"
                - ansible.builtin.unarchive:
                    src: "https://example.com/{{ version }}.tar.xz"
                - ansible.builtin.copy:
                    src: "{{ item }}"
                - ansible.builtin.copy:
                    src: "{{ item.src }}"
                - ansible.builtin.copy:
                    src: "{{ download_dir.path }}/x.tar.gz"
        """), [])

    def test_a_relative_src_that_does_not_exist_is_reported(self) -> None:
        self.assertEqual(len(self._unresolved("""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: somewhere/missing.conf
        """)), 1)

    def test_an_unresolved_reference_in_an_included_file_names_that_file(self) -> None:
        self.fx.file("tasks/inc.yml", """
            - ansible.builtin.copy:
                src: "{{ mystery }}"
        """)
        found = self._unresolved(f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.include_tasks: "{ROOT}/tasks/inc.yml"
        """)
        self.assertEqual(len(found), 1)
        self.assertIn("tasks/inc.yml", found[0])


class TestJoinedReferences(FixtureCase):
    """`root_dir ~ '/x'` and `root_dir + '/x'` are checkout references too (Plan 00192 3.6)."""

    def _inputs(self, line: str) -> ap.Inputs:
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              vars:
                value: >-
                  {line}
        """)
        return ap.play_inputs(self.fx.root, play)

    def test_a_joined_file_is_an_exact_input(self) -> None:
        self.fx.file(".qa-versions")
        inputs = self._inputs("{{ lookup('ansible.builtin.file', root_dir ~ '/.qa-versions') }}")
        self.assertIn(".qa-versions", inputs.exact)
        self.assertEqual(inputs.unresolved, [])

    def test_a_joined_glob_is_its_literal_prefix(self) -> None:
        inputs = self._inputs("{{ lookup('fileglob', root_dir ~ '/files/share/lab/*.bash') }}")
        self.assertTrue(ap.affects(inputs, "files/share/lab/new.bash"))
        self.assertFalse(ap.affects(inputs, "files/share/other.bash"))

    def test_a_joined_prefix_with_a_templated_tail_is_a_prefix(self) -> None:
        inputs = self._inputs("{{ lookup('file', root_dir + '/files/conf/' + item.item) }}")
        self.assertTrue(ap.affects(inputs, "files/conf/anything"))

    def test_a_join_with_no_literal_path_is_reported(self) -> None:
        inputs = self._inputs("{{ root_dir ~ '/' ~ some_path }}")
        self.assertEqual(len(inputs.unresolved), 1)
        self.assertIn("play-a.yml:5:", inputs.unresolved[0])


GO_MOD = "module example.com/m\n\ngo 1.26.0\n"
BUILD_TASK = """
    - ansible.builtin.command:
        argv: [go, build, -o, "/tmp/{{ go_command_name }}", "./cmd/{{ go_command_name }}"]
        chdir: "{{ root_dir ~ '/src-go' }}"
"""


class GoFixtureCase(FixtureCase):
    """A src-go module with one command, `tool`, and the build task file."""

    def setUp(self) -> None:
        super().setUp()
        fx = self.fx
        fx.file("src-go/go.mod", GO_MOD)
        fx.file("src-go/go.sum", "")
        fx.file("src-go/vendor/modules.txt", "# github.com/x/y v1.0.0\n")
        fx.file("src-go/cmd/tool/main.go", """
            package main

            import (
            \t"fmt"
            \tui "example.com/m/internal/ui"
            \t_ "example.com/m/internal/blank"
            \t// "example.com/m/internal/commented"
            \t"github.com/x/y"
            )

            func main() { fmt.Println(ui.X, y.Y) }
        """)
        fx.file("src-go/cmd/tool/main_test.go", 'package main\n\nimport "example.com/m/internal/testonly"\n')
        fx.file("src-go/internal/ui/ui.go", """
            package ui

            import "example.com/m/internal/term"

            //go:embed assets/*
            var X = term.T
        """)
        fx.file("src-go/internal/ui/assets/help.txt")
        fx.file("src-go/internal/ui/sub/sub.go", "package sub\n")
        fx.file("src-go/internal/term/term.go", "package term\n\nconst T = 1\n")
        fx.file("src-go/internal/blank/blank.go", "package blank\n")
        fx.file("src-go/internal/commented/c.go", "package commented\n")
        fx.file("src-go/internal/testonly/t.go", "package testonly\n")
        fx.file("src-go/vendor/github.com/x/y/y.go", 'package y\n\nimport . "github.com/x/z"\n\nconst Y = Z\n')
        fx.file("src-go/vendor/github.com/x/z/z.go", "package z\n\nconst Z = 1\n")
        fx.file("tasks/build-go-command.yml", BUILD_TASK)

    def _play(self, go_vars: str, extra_vars: str = "") -> ap.Inputs:
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              vars:
                root_dir: "{CONFIG_ROOT}"
            {textwrap.indent(textwrap.dedent(extra_vars), "    ")}
              tasks:
                - ansible.builtin.import_tasks: "{ROOT}/tasks/build-go-command.yml"
                  vars:
            {textwrap.indent(textwrap.dedent(go_vars), "          ")}
        """)
        return ap.play_inputs(self.fx.root, play)


class TestGoBuilds(GoFixtureCase):
    """A play that builds a Go command counts every package the command imports, within the
    module and from its vendor tree, so a shared package change marks every such play stale."""

    def test_every_package_the_command_imports_is_an_input(self) -> None:
        inputs = self._play("go_command_name: tool\n")
        self.assertEqual(inputs.unresolved, [])
        for path in (
            "src-go/cmd/tool/main.go",
            "src-go/internal/ui/ui.go",
            "src-go/internal/ui/new_file.go",
            "src-go/internal/term/term.go",
            "src-go/internal/blank/blank.go",
            "src-go/vendor/github.com/x/y/y.go",
            "src-go/vendor/github.com/x/z/z.go",
            "src-go/go.mod",
            "src-go/go.sum",
            "src-go/vendor/modules.txt",
        ):
            self.assertTrue(ap.affects(inputs, path), path)
        self.assertIn("src-go", inputs.go_modules)

    def test_what_the_command_does_not_build_is_not_an_input(self) -> None:
        inputs = self._play("go_command_name: tool\n")
        for path in (
            "src-go/internal/commented/c.go",
            "src-go/internal/testonly/t.go",
            "src-go/internal/ui/ui_test.go",
            "src-go/internal/ui/sub/sub.go",
            "src-go/cmd/tool/main_test.go",
            "src-go/qa/tool.go",
        ):
            self.assertFalse(ap.affects(inputs, path), path)

    def test_an_embedded_directory_is_an_input(self) -> None:
        inputs = self._play("go_command_name: tool\n")
        self.assertTrue(ap.affects(inputs, "src-go/internal/ui/assets/help.txt"))
        self.assertTrue(ap.affects(inputs, "src-go/internal/ui/assets/new/more.txt"))

    def test_a_templated_command_name_resolves_through_the_play_vars(self) -> None:
        """The freeze plays' shape: the task file names `{{ freeze_tool }}`, the play sets it."""
        inputs = self._play('go_command_name: "{{ tool_name }}"\n', "tool_name: tool\n")
        self.assertEqual(inputs.unresolved, [])
        self.assertTrue(ap.affects(inputs, "src-go/internal/term/term.go"))

    def test_a_templated_command_name_nobody_sets_is_reported(self) -> None:
        inputs = self._play('go_command_name: "{{ tool_name }}"\n')
        self.assertEqual(len(inputs.unresolved), 1)
        self.assertIn("tool_name", inputs.unresolved[0])

    def test_a_command_the_module_does_not_have_is_reported(self) -> None:
        inputs = self._play("go_command_name: missing\n")
        self.assertEqual(len(inputs.unresolved), 1)
        self.assertIn("src-go/cmd/missing", inputs.unresolved[0])

    def test_a_pinned_external_module_brings_no_src_go_input(self) -> None:
        inputs = self._play(
            "go_command_name: viewer\n"
            'go_command_module: "github.com/o/viewer/cmd/viewer@{{ viewer_version }}"\n'
        )
        self.assertEqual(inputs.unresolved, [])
        self.assertFalse(ap.affects(inputs, "src-go/internal/ui/ui.go"))
        self.assertFalse(ap.affects(inputs, "src-go/go.mod"))

    def test_a_module_built_with_no_command_named_is_reported(self) -> None:
        inputs = self._play("other_var: 1\n")
        self.assertEqual(len(inputs.unresolved), 1)
        self.assertIn("go_command_name", inputs.unresolved[0])


class TestGoChanges(GoFixtureCase):
    """Plan 00192 Task 3.8: a changed Go file is named as its package and the commands built
    from it, so a play stale from Go code alone gives a reason a person can read."""

    def _two_commands(self) -> ap.Inputs:
        """tool imports ui and term; other imports term only."""
        self.fx.file("src-go/cmd/other/main.go", 'package main\n\nimport "example.com/m/internal/term"\n')
        play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              vars:
                root_dir: "{CONFIG_ROOT}"
              tasks:
                - ansible.builtin.import_tasks: "{ROOT}/tasks/build-go-command.yml"
                  vars:
                    go_command_name: tool
                - ansible.builtin.import_tasks: "{ROOT}/tasks/build-go-command.yml"
                  vars:
                    go_command_name: other
        """)
        return ap.play_inputs(self.fx.root, play)

    def test_each_package_knows_the_commands_built_from_it(self) -> None:
        inputs = self._two_commands()
        self.assertEqual(inputs.unresolved, [])
        self.assertEqual(inputs.go_builders["src-go/internal/term"], {"tool", "other"})
        self.assertEqual(inputs.go_builders["src-go/internal/ui"], {"tool"})
        self.assertEqual(inputs.go_builders["src-go/cmd/other"], {"other"})
        self.assertEqual(inputs.go_builders["src-go/vendor/github.com/x/z"], {"tool"})

    def test_changed_files_are_grouped_by_package_in_the_order_seen(self) -> None:
        inputs = self._two_commands()
        changes = ap.go_changes(inputs, [
            "playbooks/imports/play-a.yml",
            "src-go/internal/term/term.go",
            "src-go/internal/ui/ui.go",
            "src-go/internal/ui/new_file.go",
            "src-go/internal/ui/ui_test.go",
            "src-go/internal/commented/c.go",
        ])
        self.assertEqual(list(changes), ["src-go/internal/term", "src-go/internal/ui"])
        self.assertEqual(changes["src-go/internal/term"], {"tool", "other"})
        self.assertEqual(changes["src-go/internal/ui"], {"tool"})

    def test_no_go_change_is_an_empty_answer(self) -> None:
        inputs = self._play("go_command_name: tool\n")
        self.assertEqual(ap.go_changes(inputs, ["src-go/go.mod", "playbooks/imports/play-a.yml"]), {})


class TestAllowlist(FixtureCase):
    def test_a_list_of_existing_plays_loads(self) -> None:
        play = self.fx.play("playbooks/imports/play-a.yml", "- hosts: desktop\n")
        self.fx.allow([play])
        self.assertEqual(ap.load_allowlist(self.fx.root), [play])

    def test_a_malformed_allowlist_is_refused(self) -> None:
        for bad in ('{"plays": []}', "not json", '["playbooks/imports/play-a.yml", 3]', "[]x"):
            with self.subTest(bad=bad):
                _write(self.fx.root, ap.ALLOWLIST_PATH, bad)
                with self.assertRaises(ValueError):
                    ap.load_allowlist(self.fx.root)

    def test_a_play_that_does_not_exist_is_refused(self) -> None:
        self.fx.allow(["playbooks/imports/play-gone.yml"])
        with self.assertRaises(ValueError):
            ap.load_allowlist(self.fx.root)

    def test_a_path_outside_playbooks_imports_is_refused(self) -> None:
        self.fx.file("scripts/evil.yml")
        for bad in ("scripts/evil.yml", "playbooks/imports/../../scripts/evil.yml", "/etc/passwd"):
            with self.subTest(bad=bad):
                self.fx.allow([bad])
                with self.assertRaises(ValueError):
                    ap.load_allowlist(self.fx.root)

    def test_a_duplicate_entry_is_refused(self) -> None:
        play = self.fx.play("playbooks/imports/play-a.yml", "- hosts: desktop\n")
        self.fx.allow([play, play])
        with self.assertRaises(ValueError):
            ap.load_allowlist(self.fx.root)


class TestDecide(FixtureCase):
    def setUp(self) -> None:
        super().setUp()
        self.fx.file("files/lib/lib.bash")
        self.fx.file("files/other/tool")
        self.allowed = self.fx.play("playbooks/imports/play-allowed.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/lib/lib.bash"
        """)
        self.other = self.fx.play("playbooks/imports/play-other.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/other/tool"
        """)
        self.fx.allow([self.allowed])

    def test_an_allowlisted_affected_play_is_run(self) -> None:
        report = ap.decide(self.fx.root, ["files/lib/lib.bash"])
        self.assertEqual(report.run, [self.allowed])
        self.assertEqual(report.skipped, [])

    def test_an_affected_play_not_on_the_list_is_skipped_by_name(self) -> None:
        report = ap.decide(self.fx.root, ["files/other/tool"])
        self.assertEqual(report.run, [])
        self.assertEqual(report.skipped, [self.other])

    def test_a_change_that_touches_nothing_deployed_affects_nothing(self) -> None:
        report = ap.decide(self.fx.root, ["docs/readme.md"])
        self.assertEqual((report.run, report.skipped), ([], []))

    def test_unresolved_references_of_an_allowlisted_play_are_always_reported(self) -> None:
        """Even when nothing changed: an allowlisted play is one we would run unattended, so
        a dependency we cannot see is a run we might silently miss."""
        self.fx.play("playbooks/imports/play-allowed.yml", """
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{{ mystery }}"
        """)
        report = ap.decide(self.fx.root, ["docs/readme.md"])
        self.assertEqual([play for play, _ in report.unresolved], [self.allowed])

    def test_plays_outside_imports_are_not_candidates(self) -> None:
        self.fx.play("playbooks/playbook-main.yml", "- import_playbook: imports/play-other.yml\n")
        self.fx.play("playbooks/dev/play-dev.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/other/tool"
        """)
        self.assertEqual(ap.decide(self.fx.root, ["files/other/tool"]).skipped, [self.other])

    def test_nested_optional_plays_are_candidates(self) -> None:
        nested = self.fx.play("playbooks/imports/optional/common/play-n.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/other/tool"
        """)
        self.assertIn(nested, ap.decide(self.fx.root, ["files/other/tool"]).skipped)


class TestCli(FixtureCase):
    def _main(self, *argv: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = ap.main(["--repo-root", self.fx.root, *argv], stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def setUp(self) -> None:
        super().setUp()
        self.fx.file("files/lib/lib.bash")
        self.play = self.fx.play("playbooks/imports/play-a.yml", f"""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{ROOT}/files/lib/lib.bash"
        """)
        self.fx.allow([self.play])

    def test_markers_on_stdout_and_exit_zero(self) -> None:
        code, out, _ = self._main("--changed", "files/lib/lib.bash")
        self.assertEqual(code, ap.EXIT_OK)
        self.assertEqual(out, f"RUN {self.play}\n")

    def test_nothing_changed_prints_nothing(self) -> None:
        code, out, _ = self._main("--changed", "docs/x.md")
        self.assertEqual((code, out), (ap.EXIT_OK, ""))

    def test_an_unresolved_allowlisted_play_is_its_own_exit_status(self) -> None:
        self.fx.play("playbooks/imports/play-a.yml", """
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{{ mystery }}"
        """)
        code, out, _ = self._main("--changed", "docs/x.md")
        self.assertEqual(code, ap.EXIT_UNRESOLVED)
        self.assertTrue(out.startswith(f"UNRESOLVED {self.play} "))

    def test_a_bad_allowlist_is_an_error_not_an_empty_answer(self) -> None:
        _write(self.fx.root, ap.ALLOWLIST_PATH, "nope")
        code, out, err = self._main("--changed", "files/lib/lib.bash")
        self.assertEqual((code, out), (ap.EXIT_ERROR, ""))
        self.assertIn("allowlist", err)

    def test_old_and_new_shas_come_from_git(self) -> None:
        # The machine's own git config stays out: a global `commit.gpgsign` would make these
        # fixture commits depend on the host's signing key. Config passed through the
        # environment outranks GIT_CONFIG_GLOBAL, so it goes too.
        env = {k: v for k, v in os.environ.items()
               if k not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
        env |= {"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}

        def git(*args: str) -> None:
            subprocess.run(["git", "-C", self.fx.root, *args], check=True, capture_output=True,
                           env=env)

        git("init", "-q")
        git("-c", "user.email=t@example.com", "-c", "user.name=t", "add", "-A")
        git("-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "a")
        old = subprocess.run(["git", "-C", self.fx.root, "rev-parse", "HEAD"], check=True,
                             capture_output=True, text=True, env=env).stdout.strip()
        self.fx.file("files/lib/lib.bash", "changed\n")
        git("-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qam", "b")
        code, out, _ = self._main("--old", old, "--new", "HEAD")
        self.assertEqual((code, out), (ap.EXIT_OK, f"RUN {self.play}\n"))

    def test_an_unknown_sha_is_an_error(self) -> None:
        subprocess.run(["git", "-C", self.fx.root, "init", "-q"], check=True, capture_output=True)
        code, _, err = self._main("--old", "0" * 40, "--new", "HEAD")
        self.assertEqual(code, ap.EXIT_ERROR)
        self.assertTrue(err)


class TestRealRepo(unittest.TestCase):
    """The mapper against this checkout: the mandatory cases from Plan 00137 Task 1.3."""

    def test_a_ccy_lib_change_runs_the_claude_play(self) -> None:
        report = ap.decide(REPO_ROOT, ["files/var/local/claude-yolo/lib/tmux-session.bash"])
        self.assertIn(CLAUDE_PLAY, report.run)

    def test_a_cc_wrapper_change_runs_the_claude_play(self) -> None:
        self.assertIn(CLAUDE_PLAY, ap.decide(REPO_ROOT, ["files/var/local/claude-code/cc"]).run)

    def test_a_docs_only_change_runs_and_skips_nothing(self) -> None:
        report = ap.decide(REPO_ROOT, ["docs/ccy.md", "CLAUDE/Plan/README.md"])
        self.assertEqual((report.run, report.skipped), ([], []))

    def test_the_seeded_allowlist_is_the_claude_play(self) -> None:
        self.assertEqual(ap.load_allowlist(REPO_ROOT), [CLAUDE_PLAY])

    def test_the_claude_play_has_no_unresolved_reference(self) -> None:
        self.assertEqual(ap.play_inputs(REPO_ROOT, CLAUDE_PLAY).unresolved, [])

    def test_a_shared_go_package_change_runs_both_freeze_plays(self) -> None:
        """Plan 00192 Task 3.6: src-go/internal/ui changed and neither freeze play ran."""
        report = ap.decide(REPO_ROOT, ["src-go/internal/ui/ui.go"])
        changed = report.run + report.skipped
        for play in FREEZE_PLAYS:
            self.assertIn(play, changed)

    def test_a_shared_go_package_change_names_each_freeze_command(self) -> None:
        """Plan 00192 Task 3.8: the reason names the package and the command it is built into."""
        for play, command in zip(FREEZE_PLAYS, ("podfreeze", "lxcfreeze"), strict=True):
            changes = ap.go_changes(ap.play_inputs(REPO_ROOT, play), ["src-go/internal/ui/ui.go"])
            self.assertEqual(changes, {"src-go/internal/ui": {command}}, play)

    def test_every_go_command_counts_the_packages_go_list_names(self) -> None:
        """The oracle is the toolchain: `go list -deps` of each command a play builds."""
        for play in ap.candidate_plays(REPO_ROOT):
            inputs = ap.play_inputs(REPO_ROOT, play)
            for command in sorted(inputs.go_commands):
                for package in _go_list_deps(command):
                    self.assertIn(package, inputs.go_packages, f"{play}: {command} imports {package}")

    def test_every_play_building_go_names_its_commands(self) -> None:
        builders = [
            play for play in ap.candidate_plays(REPO_ROOT)
            if "tasks/build-go-command.yml" in ap.play_inputs(REPO_ROOT, play).exact
        ]
        self.assertGreater(len(builders), 10)
        for play in builders:
            inputs = ap.play_inputs(REPO_ROOT, play)
            self.assertIn("src-go", inputs.go_modules, play)
            self.assertFalse([u for u in inputs.unresolved if "go_command" in u], play)

    def test_every_import_play_parses(self) -> None:
        """Parsing never crashes on a real play. The per-play unresolved counts are what
        Plan 00137's report records; they are printed so a drift is visible in the log."""
        plays = ap.candidate_plays(REPO_ROOT)
        self.assertGreater(len(plays), 30)
        counts = {play: len(ap.play_inputs(REPO_ROOT, play).unresolved) for play in plays}
        noisy = {play: n for play, n in counts.items() if n}
        sys.stderr.write(f"\n  unresolved per play: {noisy or 'none'}\n")


if __name__ == "__main__":
    unittest.main()

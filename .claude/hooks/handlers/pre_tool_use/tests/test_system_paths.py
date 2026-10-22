"""Comprehensive tests for SystemPathsHandler - Infrastructure-as-Code enforcement."""

import os
import sys
from pathlib import Path

import pytest

# Add handler directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent))

import system_paths
from system_paths import SystemPathsHandler


class TestSystemPathsHandler:
    """Test suite for SystemPathsHandler - blocks direct editing of deployed system files."""

    @pytest.fixture
    def handler(self):
        """Create handler instance."""
        return SystemPathsHandler()

    # Initialization Tests
    def test_init_sets_correct_name(self, handler):
        """Handler name should be 'prevent-system-file-edits'."""
        assert handler.name == "prevent-system-file-edits"

    def test_init_sets_correct_priority(self, handler):
        """Handler priority should be 8 (safety handler, runs early)."""
        assert handler.priority == 8

    def test_init_sets_correct_terminal_flag(self, handler):
        """Handler should be terminal (blocks and returns immediately)."""
        assert handler.terminal is True

    # matches() - Write Tool - Positive Cases (should block)
    def test_matches_write_to_etc_config(self, handler):
        """Should match Write to /etc/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/etc/systemd/system/my-service.service",
                "content": "[Service]\nExecStart=/usr/bin/my-app",
            },
        }
        assert handler.matches(hook_input) is True

    def test_matches_write_to_var_directory(self, handler):
        """Should match Write to /var/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/var/local/my-script/config.sh",
                "content": "#!/bin/bash\necho 'test'",
            },
        }
        assert handler.matches(hook_input) is True

    def test_matches_write_to_usr_directory(self, handler):
        """Should match Write to /usr/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/usr/local/bin/my-tool",
                "content": "#!/bin/bash\necho 'tool'",
            },
        }
        assert handler.matches(hook_input) is True

    def test_matches_write_to_opt_directory(self, handler):
        """Should match Write to /opt/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/opt/myapp/config.json",
                "content": '{"setting": "value"}',
            },
        }
        assert handler.matches(hook_input) is True

    def test_matches_write_to_root_home(self, handler):
        """Should match Write to /root/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/root/.bashrc",
                "content": "export PATH=/usr/local/bin:$PATH",
            },
        }
        assert handler.matches(hook_input) is True

    def test_matches_write_to_user_home(self, handler):
        """Should match Write to /home/ directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/home/u/.config/myapp/config.yml",
                "content": "setting: value",
            },
        }
        assert handler.matches(hook_input) is True

    # matches() - Edit Tool - Positive Cases
    def test_matches_edit_etc_file(self, handler):
        """Should match Edit to /etc/ file."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/etc/fstab",
                "old_string": "# old mount",
                "new_string": "# new mount",
            },
        }
        assert handler.matches(hook_input) is True

    def test_matches_edit_var_file(self, handler):
        """Should match Edit to /var/ file."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/var/www/html/index.html",
                "old_string": "<h1>Old</h1>",
                "new_string": "<h1>New</h1>",
            },
        }
        assert handler.matches(hook_input) is True

    # matches() - Negative Cases (should NOT block)
    def test_matches_write_to_workspace_returns_false(self, handler):
        """Should NOT match Write to /workspace/ (project directory)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/files/etc/systemd/system/my-service.service",
                "content": "[Service]\nExecStart=/usr/bin/my-app",
            },
        }
        assert handler.matches(hook_input) is False

    def test_matches_write_to_workspace_files_subdir_returns_false(self, handler):
        """Should NOT match Write to /workspace/files/* (source files)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/files/var/local/my-script.sh",
                "content": "#!/bin/bash\necho 'test'",
            },
        }
        assert handler.matches(hook_input) is False

    def test_matches_write_relative_path_returns_false(self, handler):
        """Should NOT match Write with relative path (assume workspace)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "files/etc/config.conf", "content": "setting=value"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_write_to_root_claude_projects_returns_false(self, handler):
        """Should NOT match Write under /root/.claude/projects/ (agent runtime memory)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/root/.claude/projects/-workspace/memory/feedback_x.md",
                "content": "---\nname: x\n---\nbody",
            },
        }
        assert handler.matches(hook_input) is False

    def test_matches_write_to_home_claude_projects_returns_false(self, handler):
        """Should NOT match Write under /home/<user>/.claude/projects/ (agent runtime when on host)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/home/u/.claude/projects/-Projects-foo/memory/MEMORY.md",
                "content": "- [x](x.md)",
            },
        }
        assert handler.matches(hook_input) is False

    # The exemptions are judged on the path the write would reach, not its spelling.
    @pytest.mark.parametrize(
        "file_path",
        [
            "/root/.claude/projects/../../../etc/passwd",
            "/home/u/.claude/projects/x/../../../../../etc/passwd",
            "/home/u/work/.claude/projects/-x/memory/MEMORY.md",
            "//etc/hosts",
            "/./etc/hosts",
            "/tmp/../etc/hosts",
        ],
    )
    def test_matches_a_system_path_however_it_is_spelt(self, handler, file_path):
        """`..`, `.`, a doubled leading slash and another project's .claude are not exempt."""
        assert handler.matches({"tool_name": "Write", "tool_input": {"file_path": file_path}}) is True

    def test_matches_a_climb_out_of_the_project_directory(self, handler):
        """The project prefix followed by `..` out of it is not the project."""
        project = system_paths._resolve_project_root()
        climb = project + "../" * project.count("/") + "etc/hosts"
        assert handler.matches({"tool_name": "Edit", "tool_input": {"file_path": climb}}) is True

    def test_resolves_under_follows_a_symlinked_root(self, tmp_path):
        """The exempt directory may be a link (CCY's /root/.claude); a file under it is exempt."""
        target = tmp_path / "ccy" / "projects"
        (target / "-p" / "memory").mkdir(parents=True)
        link = tmp_path / "projects"
        link.symlink_to(target)
        assert system_paths._resolves_under(f"{link}/-p/memory/MEMORY.md", str(link)) is True

    def test_resolves_under_refuses_a_link_that_leaves_the_root(self, tmp_path):
        """A link planted inside the exempt directory that points out of it is not exempt."""
        root = tmp_path / "projects"
        outside = tmp_path / "etc"
        root.mkdir()
        outside.mkdir()
        (root / "escape").symlink_to(outside)
        assert system_paths._resolves_under(f"{root}/escape/passwd", str(root)) is False

    def test_resolves_under_resolves_dotdot_after_a_link_as_the_kernel_does(self, tmp_path):
        """`link/../x` with link -> /etc/ssh lands in /etc, though it normalises to root/x."""
        root = tmp_path / "projects"
        sub = tmp_path / "etc" / "ssh"
        root.mkdir()
        sub.mkdir(parents=True)
        (root / "link").symlink_to(sub)
        assert system_paths._resolves_under(f"{root}/link/../passwd", str(root)) is False
        assert system_paths._resolves_under(f"{root}/plain/../passwd", str(root)) is True

    # The block is judged on where the write lands too: symlinks are followed.
    @staticmethod
    def _write(file_path):
        return {"tool_name": "Write", "tool_input": {"file_path": file_path}}

    @pytest.fixture
    def project(self, tmp_path, monkeypatch):
        """A stand-in project root, so links can be planted in it without touching the checkout."""
        root = Path(os.path.realpath(tmp_path)) / "project"
        (root / "sub").mkdir(parents=True)
        monkeypatch.setattr(system_paths, "_resolve_project_root", lambda: f"{root}/")
        monkeypatch.delenv("CLAUDE_PROJECT_DIR", raising=False)
        return root

    def test_matches_a_link_outside_the_project_into_a_system_dir(self, handler, tmp_path):
        """A constructed /bin -> usr/bin: the path as written is nowhere blocked, its target is."""
        (tmp_path / "bin").symlink_to("/usr/bin")
        assert handler.matches(self._write(f"{tmp_path}/bin/foo")) is True

    @pytest.mark.parametrize("file_path", ["/bin/foo", "/lib/x", "/sbin/x"])
    def test_matches_the_merged_usr_top_level_links(self, handler, file_path):
        """On a merged-/usr system /bin, /lib and /sbin are links into /usr."""
        top = "/" + file_path.split("/")[1]
        if not os.path.islink(top):
            pytest.skip(f"{top} is not a symlink on this machine; the constructed-link test covers it")
        assert os.path.realpath(top).startswith("/usr/")
        assert handler.matches(self._write(file_path)) is True

    def test_matches_proc_self_root(self, handler):
        """/proc/self/root/etc/hosts writes /etc/hosts."""
        assert handler.matches(self._write("/proc/self/root/etc/hosts")) is True

    def test_matches_a_project_link_into_a_system_dir(self, handler, project):
        """<project>/link/x with link -> /usr/bin is a write to /usr/bin, not to the project."""
        (project / "link").symlink_to("/usr/bin")
        assert handler.matches(self._write(f"{project}/link/x")) is True

    def test_matches_dotdot_after_a_project_link(self, handler, project):
        """<project>/link/../x folds to <project>/x but lands in /usr/x."""
        (project / "link").symlink_to("/usr/bin")
        assert handler.matches(self._write(f"{project}/link/../x")) is True

    def test_allows_the_project_with_a_link_that_stays_in_it(self, handler, project):
        """A link inside the project to the project, and a file not yet created, stay allowed."""
        (project / "inner").symlink_to(project / "sub")
        assert handler.matches(self._write(f"{project}/sub/new-dir/new-file")) is False
        assert handler.matches(self._write(f"{project}/inner/x")) is False

    def test_a_project_reached_through_a_symlinked_parent_is_the_project(self, project, tmp_path, monkeypatch):
        """CLAUDE_PROJECT_DIR spelt through a link is the project, when it resolves to the root."""
        alias = tmp_path / "alias"
        alias.symlink_to(project)
        assert system_paths._is_project_path(f"{alias}/x") is False
        monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(alias))
        assert system_paths._is_project_path(f"{alias}/x") is True
        assert system_paths._is_project_path(f"{alias}/../alias/x") is True

    def test_a_project_dir_variable_that_is_not_the_project_exempts_nothing(self, project, tmp_path, monkeypatch):
        """CLAUDE_PROJECT_DIR naming somewhere else is not taken as a spelling of the project."""
        other = tmp_path / "other"
        other.mkdir()
        monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(other))
        assert system_paths._is_project_path(f"{other}/x") is False

    @pytest.mark.parametrize("relative", ["x.md", "untracked/scratch/x", ".claude/worktrees/w/files/etc/x"])
    def test_allows_the_real_project_tree(self, handler, relative):
        """The checkout's own files, untracked/ and worktrees under it stay allowed."""
        root = system_paths._resolve_project_root()
        assert handler.matches(self._write(root + relative)) is False

    def test_matches_read_tool_returns_false(self, handler):
        """Should NOT match Read tool (reading system files is allowed)."""
        hook_input = {"tool_name": "Read", "tool_input": {"file_path": "/etc/fstab"}}
        assert handler.matches(hook_input) is False

    def test_matches_bash_tool_returns_false(self, handler):
        """Should NOT match Bash tool (Ansible handles deployment)."""
        hook_input = {"tool_name": "Bash", "tool_input": {"command": "sudo cp file /etc/"}}
        assert handler.matches(hook_input) is False

    def test_matches_glob_tool_returns_false(self, handler):
        """Should NOT match Glob tool (searching is allowed)."""
        hook_input = {"tool_name": "Glob", "tool_input": {"pattern": "/etc/**/*.conf"}}
        assert handler.matches(hook_input) is False

    def test_matches_write_empty_file_path_returns_false(self, handler):
        """Should NOT match Write with empty file_path."""
        hook_input = {"tool_name": "Write", "tool_input": {"file_path": "", "content": "test"}}
        assert handler.matches(hook_input) is False

    def test_matches_write_none_file_path_returns_false(self, handler):
        """Should NOT match Write with None file_path."""
        hook_input = {"tool_name": "Write", "tool_input": {"file_path": None, "content": "test"}}
        assert handler.matches(hook_input) is False

    def test_matches_write_missing_file_path_returns_false(self, handler):
        """Should NOT match Write without file_path key."""
        hook_input = {"tool_name": "Write", "tool_input": {"content": "test"}}
        assert handler.matches(hook_input) is False

    def test_matches_missing_tool_input_returns_false(self, handler):
        """Should NOT match when tool_input is missing."""
        hook_input = {"tool_name": "Write"}
        assert handler.matches(hook_input) is False

    # Edge Cases
    def test_matches_etc_in_middle_of_path_returns_false(self, handler):
        """Should NOT match /etc/ appearing in middle of non-system path."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/projects/etc/config.txt",  # "etc" but not system /etc/
                "content": "test",
            },
        }
        assert handler.matches(hook_input) is False

    def test_matches_system_path_with_trailing_slash(self, handler):
        """Should match system path even with double slashes."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/etc//systemd/system/service", "content": "test"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_usr_share_returns_true(self, handler):
        """Should match /usr/share/ (part of /usr/ system path)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/usr/share/applications/myapp.desktop", "content": "[Desktop Entry]"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_var_log_returns_true(self, handler):
        """Should match /var/log/ (part of /var/ system path)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/var/log/myapp.log", "content": "log entry"},
        }
        assert handler.matches(hook_input) is True

    # handle() Tests - Decision and Reason
    def test_handle_returns_deny_decision(self, handler):
        """handle() should return deny for blocked system paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/etc/config.conf", "content": "setting=value"},
        }
        result = handler.handle(hook_input)
        assert result.decision == "deny"

    def test_handle_reason_contains_blocked_indicator(self, handler):
        """handle() reason should clearly indicate operation is blocked."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/etc/config.conf", "content": "setting=value"},
        }
        result = handler.handle(hook_input)
        assert "BLOCKED" in result.reason

    def test_handle_reason_shows_target_file_path(self, handler):
        """handle() reason should show the blocked file path."""
        target_path = "/etc/systemd/system/my-service.service"
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": target_path, "content": "[Service]"},
        }
        result = handler.handle(hook_input)
        assert target_path in result.reason

    def test_handle_reason_suggests_project_equivalent(self, handler):
        """handle() reason should suggest correct project path."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/etc/config.conf", "content": "test"},
        }
        result = handler.handle(hook_input)
        assert "files/etc/" in result.reason

    def test_handle_reason_suggests_ansible_deployment(self, handler):
        """handle() reason should mention Ansible deployment."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/var/local/script.sh", "content": "#!/bin/bash"},
        }
        result = handler.handle(hook_input)
        assert "ansible-playbook" in result.reason.lower()

    def test_handle_reason_explains_infrastructure_as_code(self, handler):
        """handle() reason should explain IaC principle."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/etc/fstab",
                "old_string": "old",
                "new_string": "new",
            },
        }
        result = handler.handle(hook_input)
        assert "version control" in result.reason.lower() or "infrastructure" in result.reason.lower()

    def test_handle_reason_references_claude_md(self, handler):
        """handle() reason should reference CLAUDE.md documentation."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/usr/local/bin/script", "content": "#!/bin/bash"},
        }
        result = handler.handle(hook_input)
        assert "CLAUDE.md" in result.reason

    # handle() Tests - Project Path Mapping
    def test_handle_maps_etc_to_files_etc(self, handler):
        """handle() should suggest files/etc/ for /etc/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/etc/myconfig.conf", "content": "test"},
        }
        result = handler.handle(hook_input)
        assert "files/etc/" in result.reason

    def test_handle_maps_var_local_to_files_var_local(self, handler):
        """handle() should suggest files/var/local/ for /var/local/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/var/local/bin/script.sh", "content": "#!/bin/bash"},
        }
        result = handler.handle(hook_input)
        assert "files/var/local/" in result.reason

    def test_handle_maps_deployed_bash_launcher_to_its_source(self, handler):
        """The play installs files/var/local/claude-yolo/claude-yolo as claude-yolo.bash."""
        result = handler.handle(
            {"tool_name": "Edit", "tool_input": {"file_path": "/var/local/claude-yolo/claude-yolo.bash"}}
        )
        assert "Edit the project file in: files/var/local/claude-yolo/claude-yolo\n" in result.reason

    def test_handle_maps_deployed_go_front_to_its_source(self, handler):
        """/var/local/claude-yolo/claude-yolo is the Go front, built from src-go."""
        result = handler.handle(
            {"tool_name": "Write", "tool_input": {"file_path": "/var/local/claude-yolo/claude-yolo"}}
        )
        assert "Edit the project file in: src-go/cmd/claude-yolo" in result.reason
        assert "Edit the project file in: files/var/local/claude-yolo/claude-yolo\n" not in result.reason

    def test_handle_maps_usr_local_to_files_usr_local(self, handler):
        """handle() should suggest files/usr/local/ for /usr/local/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/usr/local/bin/mytool", "content": "#!/bin/bash"},
        }
        result = handler.handle(hook_input)
        assert "files/usr/local/" in result.reason

    def test_handle_suggests_ansible_for_home_directories(self, handler):
        """handle() should suggest Ansible for /home/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/home/u/.bashrc", "content": "export PATH=/usr/bin"},
        }
        result = handler.handle(hook_input)
        assert "Ansible" in result.reason or "playbook" in result.reason.lower()

    def test_handle_suggests_ansible_for_root_directory(self, handler):
        """handle() should suggest Ansible for /root/ paths."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/root/.vimrc", "content": "set number"},
        }
        result = handler.handle(hook_input)
        assert "Ansible" in result.reason or "playbook" in result.reason.lower()

    # Integration Tests
    def test_allows_editing_project_mirror_of_system_files(self, handler):
        """Should allow editing project files that mirror system structure."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/files/etc/systemd/system/my.service",
                "content": "[Service]\nExecStart=/usr/bin/myapp",
            },
        }
        assert handler.matches(hook_input) is False

    def test_blocks_direct_system_edits_even_with_good_intentions(self, handler):
        """Should block direct system edits regardless of description."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/etc/important.conf", "content": "# Quick fix"},
            "description": "Just a quick config fix, will update Ansible later",
        }
        assert handler.matches(hook_input) is True
        result = handler.handle(hook_input)
        assert result.decision == "deny"

    def test_comprehensive_workflow_enforcement(self, handler):
        """Should enforce complete IaC workflow: edit files/, deploy via Ansible."""
        # 1. Block direct system edit
        bad_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/var/local/claude-yolo/claude-yolo", "content": "#!/bin/bash\necho new"},
        }
        assert handler.matches(bad_input) is True

        # 2. Allow editing project source
        good_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/files/var/local/claude-yolo/claude-yolo",
                "content": "#!/bin/bash\necho new",
            },
        }
        assert handler.matches(good_input) is False

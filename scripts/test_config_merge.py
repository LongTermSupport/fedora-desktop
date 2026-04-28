"""Tests for config_merge — YAML block splitting, preview, and merge logic.

All test data uses obvious placeholder values. No real secrets.
"""

from config_merge import merge_blocks, preview_value, split_blocks


# ─── split_blocks ────────────────────────────────────────────────────────────


class TestSplitBlocks:
    """Top-level YAML key block splitting."""

    def test_simple_key_value_pairs(self):
        content = 'user_login: "alice"\nuser_name: "Alice Smith"\n'
        blocks = split_blocks(content)
        assert len(blocks) == 2
        assert blocks[0][0] == "user_login"
        assert blocks[1][0] == "user_name"

    def test_multiline_dict_value(self):
        content = (
            'github_accounts:\n'
            '  personal: "alice"\n'
            '  work: "alice-corp"\n'
            'user_email: "alice@example.com"\n'
        )
        blocks = split_blocks(content)
        assert len(blocks) == 2
        assert blocks[0][0] == "github_accounts"
        assert '  personal: "alice"' in blocks[0][1]
        assert '  work: "alice-corp"' in blocks[0][1]
        assert blocks[1][0] == "user_email"

    def test_vault_encrypted_value(self):
        content = (
            'api_key: !vault |\n'
            '  $ANSIBLE_VAULT;1.2;AES256;localhost\n'
            '  66386439653162636163623333\n'
            'user_login: "alice"\n'
        )
        blocks = split_blocks(content)
        assert len(blocks) == 2
        assert blocks[0][0] == "api_key"
        assert "!vault" in blocks[0][1]
        assert blocks[1][0] == "user_login"

    def test_comment_attaches_to_next_key(self):
        """Comments at column 0 between keys belong to the NEXT key, not previous."""
        content = (
            'user_email: "alice@example.com"\n'
            '# GitHub CLI accounts\n'
            'github_accounts:\n'
            '  personal: "alice"\n'
        )
        blocks = split_blocks(content)
        assert len(blocks) == 2

        # user_email block should NOT contain the comment
        assert "# GitHub CLI accounts" not in blocks[0][1]

        # github_accounts block SHOULD contain the comment
        assert "# GitHub CLI accounts" in blocks[1][1]

    def test_multiple_comments_attach_to_next_key(self):
        content = (
            'user_login: "alice"\n'
            '# First comment\n'
            '# Second comment\n'
            'user_email: "alice@example.com"\n'
        )
        blocks = split_blocks(content)
        assert len(blocks) == 2
        assert "# First comment" not in blocks[0][1]
        assert "# First comment" in blocks[1][1]
        assert "# Second comment" in blocks[1][1]

    def test_inline_comment_stays_with_key(self):
        """Comments on the same line as a value are part of that key's block."""
        content = (
            'user_login: "alice"  # the username\n'
            'user_email: "alice@example.com"\n'
        )
        blocks = split_blocks(content)
        assert len(blocks) == 2
        assert "# the username" in blocks[0][1]

    def test_indented_comment_stays_with_key(self):
        """Indented comments (inside a dict value) stay with the current key."""
        content = (
            'github_accounts:\n'
            '  # personal account\n'
            '  personal: "alice"\n'
            'user_login: "alice"\n'
        )
        blocks = split_blocks(content)
        assert len(blocks) == 2
        assert "# personal account" in blocks[0][1]

    def test_yaml_document_marker_ignored(self):
        content = '---\nuser_login: "alice"\n'
        blocks = split_blocks(content)
        assert len(blocks) == 1
        assert blocks[0][0] == "user_login"

    def test_empty_content(self):
        blocks = split_blocks("")
        assert blocks == []

    def test_preserves_block_text_exactly(self):
        """Block text should be preserved character-for-character."""
        content = 'user_login: "alice"\nuser_name: "Alice Smith"\n'
        blocks = split_blocks(content)
        assert blocks[0][1] == 'user_login: "alice"\n'
        assert blocks[1][1] == 'user_name: "Alice Smith"\n'

    def test_comment_at_start_of_file(self):
        content = (
            '# File header comment\n'
            'user_login: "alice"\n'
        )
        blocks = split_blocks(content)
        assert len(blocks) == 1
        assert blocks[0][0] == "user_login"
        assert "# File header comment" in blocks[0][1]

    def test_key_with_colon_in_value(self):
        """Key detection should split on first colon only."""
        content = 'url: "https://example.com"\nname: "test"\n'
        blocks = split_blocks(content)
        assert len(blocks) == 2
        assert blocks[0][0] == "url"
        assert "https://example.com" in blocks[0][1]

    def test_blank_lines_between_keys(self):
        content = 'user_login: "alice"\n\nuser_email: "alice@example.com"\n'
        blocks = split_blocks(content)
        assert len(blocks) == 2
        # Blank line should be attached somewhere, not lost
        combined = blocks[0][1] + blocks[1][1]
        assert "\n\n" in combined or len(combined.splitlines()) >= 3


# ─── preview_value ───────────────────────────────────────────────────────────


class TestPreviewValue:
    """Preview generation for config key values."""

    def test_simple_string_value(self):
        text = 'user_login: "alice"\n'
        result = preview_value(text)
        assert '"alice"' in result

    def test_vault_encrypted(self):
        text = (
            'api_key: !vault |\n'
            '  $ANSIBLE_VAULT;1.2;AES256;localhost\n'
            '  66386439653162636163623333\n'
        )
        result = preview_value(text)
        assert result == "[vault-encrypted]"

    def test_dict_value_shows_children(self):
        text = (
            'github_accounts:\n'
            '  personal: "alice"\n'
            '  work: "alice-corp"\n'
        )
        result = preview_value(text)
        assert "personal" in result
        assert "alice" in result

    def test_long_value_truncated(self):
        text = 'description: "This is a very long value that should be truncated because it exceeds the preview limit"\n'
        result = preview_value(text)
        assert len(result) < 55
        assert result.endswith("...")

    def test_simple_value_with_trailing_comment_shows_value_not_comment(self):
        """When a key: value line is followed by a comment, preview shows the value."""
        text = (
            'user_email: "alice@example.com"\n'
            '# GitHub CLI accounts\n'
        )
        result = preview_value(text)
        assert "alice@example.com" in result
        assert "GitHub CLI" not in result


# ─── merge_blocks ────────────────────────────────────────────────────────────


class TestMergeBlocks:
    """Interactive merge of local and remote config blocks."""

    def test_identical_configs_all_unchanged(self):
        local = [("user_login", 'user_login: "alice"\n')]
        remote = [("user_login", 'user_login: "alice"\n')]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "l")
        assert len(merged) == 1
        assert stats["unchanged"] == 1

    def test_remote_new_key_added(self):
        local = [("user_login", 'user_login: "alice"\n')]
        remote = [
            ("user_login", 'user_login: "alice"\n'),
            ("api_key", 'api_key: !vault |\n  encrypted\n'),
        ]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "a")
        assert len(merged) == 2
        assert stats["added"] == 1

    def test_remote_new_key_skipped(self):
        local = [("user_login", 'user_login: "alice"\n')]
        remote = [
            ("user_login", 'user_login: "alice"\n'),
            ("api_key", 'api_key: !vault |\n  encrypted\n'),
        ]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "s")
        assert len(merged) == 1
        assert stats["added"] == 0

    def test_changed_key_choose_remote(self):
        local = [("user_email", 'user_email: "old@example.com"\n')]
        remote = [("user_email", 'user_email: "new@example.com"\n')]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "r")
        assert "new@example.com" in merged[0][1]
        assert stats["updated"] == 1

    def test_changed_key_choose_local(self):
        local = [("user_email", 'user_email: "old@example.com"\n')]
        remote = [("user_email", 'user_email: "new@example.com"\n')]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "l")
        assert "old@example.com" in merged[0][1]
        assert stats["kept_local"] == 1

    def test_local_only_key_always_kept(self):
        local = [
            ("user_login", 'user_login: "alice"\n'),
            ("custom_key", 'custom_key: "local-only"\n'),
        ]
        remote = [("user_login", 'user_login: "alice"\n')]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "l")
        assert len(merged) == 2
        keys = [k for k, _ in merged]
        assert "custom_key" in keys
        assert stats["kept_local"] == 1

    def test_preserves_local_key_order(self):
        local = [
            ("z_key", 'z_key: "z"\n'),
            ("a_key", 'a_key: "a"\n'),
        ]
        remote = [
            ("a_key", 'a_key: "a"\n'),
            ("z_key", 'z_key: "z"\n'),
        ]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "l")
        assert merged[0][0] == "z_key"
        assert merged[1][0] == "a_key"

    def test_new_remote_keys_appended_after_local(self):
        local = [("user_login", 'user_login: "alice"\n')]
        remote = [
            ("user_login", 'user_login: "alice"\n'),
            ("new_key", 'new_key: "value"\n'),
        ]
        merged, stats = merge_blocks(local, remote, chooser=lambda *_: "a")
        assert merged[0][0] == "user_login"
        assert merged[1][0] == "new_key"

    def test_mixed_scenario(self):
        """Full scenario: unchanged, changed, local-only, and new remote keys."""
        local = [
            ("user_login", 'user_login: "alice"\n'),
            ("user_email", 'user_email: "old@example.com"\n'),
            ("local_only", 'local_only: "mine"\n'),
        ]
        remote = [
            ("user_login", 'user_login: "alice"\n'),
            ("user_email", 'user_email: "new@example.com"\n'),
            ("api_key", 'api_key: "secret"\n'),
        ]

        def chooser(action, key):
            if action == "changed":
                return "r"  # take remote for changed keys
            return "a"  # add new keys

        merged, stats = merge_blocks(local, remote, chooser=chooser)
        assert stats["unchanged"] == 1
        assert stats["updated"] == 1
        assert stats["kept_local"] == 1
        assert stats["added"] == 1
        keys = [k for k, _ in merged]
        assert keys == ["user_login", "user_email", "local_only", "api_key"]


# ─── write_blocks ────────────────────────────────────────────────────────────


class TestWriteBlocks:
    """File output from block lists."""

    def test_writes_blocks_to_file(self, tmp_path):
        from config_merge import write_blocks

        outfile = str(tmp_path / "out.yml")
        blocks = [
            ("user_login", 'user_login: "alice"\n'),
            ("user_email", 'user_email: "alice@example.com"\n'),
        ]
        write_blocks(blocks, outfile)
        with open(outfile) as f:
            content = f.read()
        assert 'user_login: "alice"' in content
        assert 'user_email: "alice@example.com"' in content

    def test_adds_trailing_newline_if_missing(self, tmp_path):
        from config_merge import write_blocks

        outfile = str(tmp_path / "out.yml")
        blocks = [("key", "key: value")]  # no trailing newline
        write_blocks(blocks, outfile)
        with open(outfile) as f:
            content = f.read()
        assert content.endswith("\n")

    def test_roundtrip_split_then_write(self, tmp_path):
        from config_merge import write_blocks

        original = (
            'user_login: "alice"\n'
            '# A comment\n'
            'github_accounts:\n'
            '  personal: "alice"\n'
        )
        blocks = split_blocks(original)
        outfile = str(tmp_path / "out.yml")
        write_blocks(blocks, outfile)
        with open(outfile) as f:
            result = f.read()
        # All original content should be present
        assert 'user_login: "alice"' in result
        assert "# A comment" in result
        assert '  personal: "alice"' in result


# ─── parse_exclusion_input ───────────────────────────────────────────────────


class TestParseExclusionInput:
    """Parsing of user exclusion number input."""

    def test_empty_string(self):
        from config_merge import parse_exclusion_input

        assert parse_exclusion_input("", 5) == set()

    def test_single_number(self):
        from config_merge import parse_exclusion_input

        assert parse_exclusion_input("3", 5) == {3}

    def test_multiple_space_separated(self):
        from config_merge import parse_exclusion_input

        assert parse_exclusion_input("1 3 5", 5) == {1, 3, 5}

    def test_comma_separated(self):
        from config_merge import parse_exclusion_input

        assert parse_exclusion_input("1,3,5", 5) == {1, 3, 5}

    def test_mixed_separators(self):
        from config_merge import parse_exclusion_input

        assert parse_exclusion_input("1, 3 5", 5) == {1, 3, 5}

    def test_out_of_range_ignored(self):
        from config_merge import parse_exclusion_input

        assert parse_exclusion_input("0 3 99", 5) == {3}

    def test_non_numeric_ignored(self):
        from config_merge import parse_exclusion_input

        assert parse_exclusion_input("1 abc 3", 5) == {1, 3}

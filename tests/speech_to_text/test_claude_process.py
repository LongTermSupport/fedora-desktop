"""wsi-claude-process fails loudly, and runs as the token Settings name (Plan 00148).

Ctrl+Insert pasted raw transcripts for a whole session: claude answered "Not logged in"
(cc parks the desktop login while a named-token session runs), and the script printed
its input as if it were Claude's output and exited 0, so every caller reported success.

The real script is run from a temporary bin directory, beside a stub wsi-setting (the
claude-token value) and a stub claude (which records the token it was given), with
HOME pointing at a temporary tree holding the token files.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest

# STT_TEST_BIN points at another copy of the scripts, e.g. an older revision, for a
# control run that must fail (as in stt_stubs.py)
SCRIPT = pathlib.Path(os.environ.get(
    "STT_TEST_BIN", pathlib.Path(__file__).resolve().parents[2] / "files/home/.local/bin")) / "wsi-claude-process"
TOKEN = "sk-ant-oat01-" + "x" * 95

STUB_CLAUDE = """#!/bin/bash
# Records the token it ran with, then answers as $STUB_CLAUDE_MODE says
printf '%s' "${CLAUDE_CODE_OAUTH_TOKEN:-}" > "$HOME/claude-saw-token"
cat > /dev/null
case "$STUB_CLAUDE_MODE" in
    ok) echo "Rewritten by Claude." ;;
    logged-out) echo "Not logged in · Please run /login"; exit 1 ;;
    empty) ;;
esac
"""


class ClaudeProcessTest(unittest.TestCase):

    def setUp(self):
        self.home = pathlib.Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.home)
        self.bin = self.home / "bin"
        self.bin.mkdir()
        shutil.copy(SCRIPT, self.bin / "wsi-claude-process")
        (self.bin / "wsi-claude-process").chmod(0o755)
        (self.bin / "claude").write_text(STUB_CLAUDE)
        (self.bin / "claude").chmod(0o755)
        self.token_dir = self.home / ".claude-tokens/ccy/tokens"
        self.token_dir.mkdir(parents=True)
        prompts = self.home / ".config/speech-to-text"
        prompts.mkdir(parents=True)
        (prompts / "claude-prompt-corporate.txt").write_text("Rewrite: {TRANSCRIPTION}")
        (self.home / ".local/share/speech-to-text").mkdir(parents=True)

    def setting(self, value):
        reader = self.bin / "wsi-setting"
        reader.write_text(f"#!/bin/bash\n[[ $1 == claude-token ]] || exit 3\nprintf '%s\\n' '{value}'\n")
        reader.chmod(0o755)

    def run_script(self, mode="ok"):
        env = {"HOME": str(self.home), "PATH": f"{self.bin}:/usr/bin:/bin",
               "STUB_CLAUDE_MODE": mode}
        return subprocess.run([str(self.bin / "wsi-claude-process"), "raw words"],
                              capture_output=True, text=True, env=env, timeout=30, check=False)

    def saw_token(self):
        return (self.home / "claude-saw-token").read_text()

    def test_a_logged_out_claude_fails_with_its_reason_and_prints_nothing(self):
        self.setting("")
        result = self.run_script("logged-out")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "", "the raw text must never stand in for Claude's")
        self.assertIn("Not logged in", result.stderr)

    def test_an_empty_answer_fails(self):
        self.setting("")
        result = self.run_script("empty")
        self.assertEqual((result.returncode, result.stdout), (1, ""))
        self.assertIn("empty", result.stderr)

    def test_the_desktop_login_is_used_when_no_token_is_named(self):
        self.setting("")
        result = self.run_script()
        self.assertEqual((result.returncode, result.stdout.strip()), (0, "Rewritten by Claude."))
        self.assertEqual(self.saw_token(), "")

    def test_the_newest_unexpired_file_of_the_named_token_is_used(self):
        (self.token_dir / "work.2099-01-01.token").write_text(TOKEN)
        (self.token_dir / "work.2000-01-01.token").write_text("sk-ant-oat01-expired" + "y" * 90)
        (self.token_dir / "other.2099-06-01.token").write_text("sk-ant-oat01-other" + "z" * 90)
        self.setting("work")
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.saw_token(), TOKEN)

    def test_a_named_token_with_only_expired_files_fails_before_claude_runs(self):
        (self.token_dir / "work.2000-01-01.token").write_text(TOKEN)
        self.setting("work")
        result = self.run_script()
        self.assertEqual((result.returncode, result.stdout), (1, ""))
        self.assertIn("no unexpired token named 'work'", result.stderr)
        self.assertFalse((self.home / "claude-saw-token").exists())

    def test_a_file_that_is_not_a_token_is_refused(self):
        (self.token_dir / "work.2099-01-01.token").write_text("not a token")
        self.setting("work")
        result = self.run_script()
        self.assertEqual(result.returncode, 1)
        self.assertIn("not a Claude Code OAuth token", result.stderr)


if __name__ == "__main__":
    unittest.main()

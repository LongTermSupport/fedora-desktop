"""wsi-claude-process fails loudly, and runs as the token wsi-claude-token names (Plans 00148, 00208).

Ctrl+Insert pasted raw transcripts for a whole session: claude answered "Not logged in"
(cc parks the desktop login while a named-token session runs), and the script printed
its input as if it were Claude's output and exited 0, so every caller reported success.

The real script is run from a temporary bin directory, beside a stub wsi-claude-token
(which chooses the account; its own tests are src-go/internal/wsitoken's) and a stub
claude (which records the token it was given), with HOME pointing at a temporary tree.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import os
import pathlib
import shutil
import subprocess
import tempfile
import time
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
    slow) sleep 10; echo "Too late." ;;
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

    def chooser(self, token_file="", fail="", seconds=0):
        """The stub wsi-claude-token: takes `seconds`, then prints token_file, or fails
        saying `fail`."""
        stub = self.bin / "wsi-claude-token"
        if fail:
            stub.write_text(f"#!/bin/bash\necho 'wsi-claude-token: {fail}' >&2\nexit 1\n")
        else:
            stub.write_text(f"#!/bin/bash\nsleep {seconds}\necho 'using the stub account' >&2\n"
                            f"printf '%s' '{token_file}'\n")
        stub.chmod(0o755)

    def run_script(self, mode="ok", budget=None):
        env = {"HOME": str(self.home), "PATH": f"{self.bin}:/usr/bin:/bin",
               "STUB_CLAUDE_MODE": mode}
        if budget is not None:
            env["WSI_CLAUDE_TIMEOUT_SECONDS"] = str(budget)
        return subprocess.run([str(self.bin / "wsi-claude-process"), "raw words"],
                              capture_output=True, text=True, env=env, timeout=30, check=False)

    def saw_token(self):
        return (self.home / "claude-saw-token").read_text()

    def token_file(self):
        path = self.token_dir / "work.2099-01-01.token"
        path.write_text(TOKEN + "\n")
        return path

    def test_a_logged_out_claude_fails_with_its_reason_and_prints_nothing(self):
        self.chooser()
        result = self.run_script("logged-out")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "", "the raw text must never stand in for Claude's")
        self.assertIn("Not logged in", result.stderr)
        self.assertIn("as the desktop login", result.stderr)

    def test_an_empty_answer_fails(self):
        self.chooser()
        result = self.run_script("empty")
        self.assertEqual((result.returncode, result.stdout), (1, ""))
        self.assertIn("empty", result.stderr)

    def test_the_desktop_login_is_used_when_the_chooser_names_no_file(self):
        self.chooser()
        result = self.run_script()
        self.assertEqual((result.returncode, result.stdout.strip()), (0, "Rewritten by Claude."))
        self.assertEqual(self.saw_token(), "")

    def test_the_token_in_the_chosen_file_is_used(self):
        self.chooser(str(self.token_file()))
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.saw_token(), TOKEN)
        self.assertNotIn(TOKEN, result.stdout + result.stderr)

    def test_a_failed_run_names_the_token_it_ran_as(self):
        self.chooser(str(self.token_file()))
        result = self.run_script("logged-out")
        self.assertEqual(result.returncode, 1)
        self.assertIn("claude failed as the token 'work' (exit 1)", result.stderr)
        self.assertNotIn(TOKEN, result.stdout + result.stderr)

    def test_a_chooser_that_fails_stops_the_run_before_claude_with_its_reason(self):
        self.chooser(fail="no Claude token has allowance left: work: 5-hour 3%, 7-day 100%")
        result = self.run_script()
        self.assertEqual((result.returncode, result.stdout), (1, ""))
        self.assertEqual(result.stderr.strip(), "wsi-claude-process: no Claude token has allowance "
                         "left: work: 5-hour 3%, 7-day 100%")
        self.assertFalse((self.home / "claude-saw-token").exists())

    def test_claude_gets_only_the_time_choosing_the_token_left(self):
        # The callers stop waiting at 180 s; the script's own limit (170 s) covers the
        # chooser too, so a slow usage read cannot push claude past the callers' limit and
        # turn the real reason into the caller's generic timeout.
        self.chooser(seconds=2)
        started = time.monotonic()
        result = self.run_script("slow", budget=4)
        took = time.monotonic() - started
        self.assertEqual((result.returncode, result.stdout), (1, ""))
        self.assertRegex(result.stderr, r"claude failed as the desktop login \(exit 124\): "
                         r"no answer within [12] s of the 4 s this run has")
        self.assertLess(took, 4.5, "claude was given the whole limit after the chooser had used half")

    def test_a_chooser_that_used_up_the_limit_leaves_claude_unrun(self):
        self.chooser(seconds=3)
        result = self.run_script(budget=2)
        self.assertEqual((result.returncode, result.stdout), (1, ""))
        self.assertRegex(result.stderr, r"choosing the token took [3-9] s of the 2 s this run has; "
                         r"claude was not run")
        self.assertFalse((self.home / "claude-saw-token").exists())


if __name__ == "__main__":
    unittest.main()

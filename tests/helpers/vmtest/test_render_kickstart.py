"""Tests for helpers/vmtest/render_kickstart.py — placeholder substitution for the VM kickstarts (T3b.1).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_render_kickstart

A kickstart template carries `@NAME@` placeholders for the facts only the build
knows (the lab SSH public key, the install tree URL). Rendering is total: every
placeholder must be given a value and every given value must be used, so a
typo in either direction is a refusal rather than an installer that boots with
`@SSH_PUBKEY@` as the authorised key.
"""

from __future__ import annotations

import pathlib
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.vmtest import render_kickstart

TEMPLATE = "url --url=@TREE_URL@\nsshkey --username=fedora \"@SSH_PUBKEY@\"\n# version @FEDORA_VERSION@\n"


class TestRender(unittest.TestCase):
    def test_every_placeholder_is_replaced(self):
        out = render_kickstart.render(TEMPLATE, {"TREE_URL": "https://example.com/os/", "SSH_PUBKEY": "ssh-ed25519 AAAA lab", "FEDORA_VERSION": "44"})
        self.assertEqual(out, "url --url=https://example.com/os/\nsshkey --username=fedora \"ssh-ed25519 AAAA lab\"\n# version 44\n")

    def test_a_placeholder_without_a_value_is_refused(self):
        with self.assertRaises(render_kickstart.RenderError) as ctx:
            render_kickstart.render(TEMPLATE, {"TREE_URL": "x", "SSH_PUBKEY": "y"})
        self.assertIn("FEDORA_VERSION", str(ctx.exception))

    def test_a_value_without_a_placeholder_is_refused(self):
        with self.assertRaises(render_kickstart.RenderError) as ctx:
            render_kickstart.render(TEMPLATE, {"TREE_URL": "x", "SSH_PUBKEY": "y", "FEDORA_VERSION": "44", "EXTRA": "z"})
        self.assertIn("EXTRA", str(ctx.exception))

    def test_a_value_containing_a_placeholder_shape_is_refused(self):
        with self.assertRaises(render_kickstart.RenderError):
            render_kickstart.render(TEMPLATE, {"TREE_URL": "@SSH_PUBKEY@", "SSH_PUBKEY": "y", "FEDORA_VERSION": "44"})

    def test_a_value_with_a_newline_is_refused(self):
        # A newline would smuggle a second kickstart command into the file.
        with self.assertRaises(render_kickstart.RenderError):
            render_kickstart.render(TEMPLATE, {"TREE_URL": "x\nrootpw --plaintext pwned", "SSH_PUBKEY": "y", "FEDORA_VERSION": "44"})

    def test_the_real_server_template_renders_with_its_three_facts(self):
        template = (REPO_ROOT / "fedora-install" / "ks-vm-server.cfg").read_text(encoding="utf-8")
        out = render_kickstart.render(template, {"TREE_URL": "https://example.com/os/", "SSH_PUBKEY": "ssh-ed25519 AAAA lab", "FEDORA_VERSION": "44"})
        self.assertNotIn("@", out.replace("@^", ""))  # only the `@^environment` group syntax remains
        self.assertIn("sshkey --username=fedora", out)
        self.assertIn("url --url=https://example.com/os/", out)
        self.assertIn("poweroff", out)


class TestExecutor(unittest.TestCase):
    def test_renders_to_stdout_and_exits_two_on_a_refusal(self):
        with tempfile.TemporaryDirectory() as tmp:
            template = pathlib.Path(tmp) / "t.cfg"
            template.write_text(TEMPLATE)
            ok = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.render_kickstart", "--template", str(template),
                 "--set", "TREE_URL=https://example.com/os/", "--set", "SSH_PUBKEY=ssh-ed25519 AAAA lab", "--set", "FEDORA_VERSION=44"],
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(ok.returncode, 0, ok.stderr)
            self.assertIn("url --url=https://example.com/os/", ok.stdout)
            bad = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.render_kickstart", "--template", str(template), "--set", "TREE_URL=x"],
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(bad.returncode, 2)
            self.assertEqual(bad.stdout, "")

    def test_values_on_stdin_never_appear_in_argv(self):
        # A passphrase handed as --set would be readable in /proc/<pid>/cmdline by any
        # local user; --set-stdin reads NAME=VALUE lines from stdin instead, one per line.
        with tempfile.TemporaryDirectory() as tmp:
            template = pathlib.Path(tmp) / "t.cfg"
            template.write_text("url --url=@TREE_URL@\nrootpw --iscrypted @PW@\n")
            ok = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.render_kickstart", "--template", str(template),
                 "--set", "TREE_URL=https://example.com/os/", "--set-stdin"],
                input="PW=$6$salt$hash=with=equals\n",
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(ok.returncode, 0, ok.stderr)
            self.assertIn("rootpw --iscrypted $6$salt$hash=with=equals\n", ok.stdout)
            blank = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.render_kickstart", "--template", str(template),
                 "--set", "TREE_URL=x", "--set-stdin"],
                input="PW=a\n\nnot-a-pair\n",
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(blank.returncode, 2)
            self.assertEqual(blank.stdout, "")
            self.assertIn("not-a-pair", blank.stderr)


if __name__ == "__main__":
    unittest.main()

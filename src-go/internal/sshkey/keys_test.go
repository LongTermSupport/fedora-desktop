package sshkey

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestE5APublicKeyIsRefusedWithItsPrivateKeyNamed(t *testing.T) {
	w := newWorld(t)
	w.key("id_a")
	pub := w.file("id_a.pub", "ssh-ed25519 AAAAC3Nz test\n")
	wantCode(t, w.run(SSH, "-i", pub, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: ~/.ssh/id_a.pub is a public key; give its private key ~/.ssh/id_a.")
	if len(w.ran()) != 0 {
		t.Errorf("ran %v", w.ran())
	}
}

func TestE5WhatIsNotAReadablePrivateKeyIsRefused(t *testing.T) {
	w := newWorld(t)
	notKey := w.file("known_hosts", "host.example.com ssh-ed25519 AAAA\n")
	for _, path := range []string{notKey, filepath.Join(w.sshDir(), "missing"), w.sshDir()} {
		w.stderr.Reset()
		wantCode(t, w.run(CopyID, "--auth-key", path, "host.example.com"), term.ExitFailure)
		w.stderrHas("ssh-copy-id-with-key: " + term.AbbrevHome(path, w.home) + " is not a readable private key: ")
	}
	if len(w.ran()) != 0 {
		t.Errorf("ran %v", w.ran())
	}
}

func TestE6OnlyPrivateKeysAreListedByName(t *testing.T) {
	w := newWorld(t)
	w.key("id_b")
	w.file("id_b.pub", "ssh-ed25519 AAAA b\n")
	w.file("config", "Host *\n")
	w.file("environment", "X=1\n")
	w.file("known_hosts", "")
	w.file("legacy_pem", privateKeyText("RSA "))
	w.key("id_a")
	if err := os.Mkdir(filepath.Join(w.sshDir(), "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(w.sshDir(), "id_a"), filepath.Join(w.sshDir(), "id_link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent", filepath.Join(w.sshDir(), "dangling")); err != nil {
		t.Fatal(err)
	}
	got, err := privateKeys(w.sshDir())
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(got))
	for _, k := range got {
		names = append(names, filepath.Base(k))
	}
	if want := []string{"id_a", "id_b", "id_link", "legacy_pem"}; !slices.Equal(names, want) {
		t.Errorf("keys %v, want %v", names, want)
	}
}

func TestE6TheChosenKeyIsUsed(t *testing.T) {
	w := newWorld(t)
	w.key("id_a")
	b := w.key("id_b")
	w.probeSays()
	w.tty = "2\n"
	wantCode(t, w.run(SSH, "host.example.com"), term.ExitOK)
	got := w.execd()
	if got.Name != "ssh" || got.Args[0] != "-i" || got.Args[1] != b {
		t.Errorf("exec %s", got.Line())
	}
	w.stderrHas("ssh-with-key › Login key")
	w.stderrHas("2 private keys in ~/.ssh")
	w.stderrHas("  1) id_a\n")
	w.stderrHas("  q) Cancel\n")
}

func TestE6SSHCopyIDSaysWhichKeyItAsksFor(t *testing.T) {
	w := newWorld(t)
	w.key("id_a")
	w.probeSays()
	w.tty = "1\n"
	wantCode(t, w.run(CopyID, "-i", "new.pub", "host.example.com"), term.ExitOK)
	w.stderrHas("the key to log in with, not the key to install")
}

func TestE7ABadAnswerIsAskedAgainThenGivenUp(t *testing.T) {
	w := newWorld(t)
	w.key("id_a")
	w.tty = "0\nx\n7\n"
	wantCode(t, w.run(SSH, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-with-key: no key chosen after 3 tries; nothing was run.")
	if strings.Count(w.stderr.String(), "Choose one of") != 3 {
		t.Errorf("stderr:\n%s", w.stderr.String())
	}
	w.noExec()
}

func TestE7ABadAnswerThenAGoodOne(t *testing.T) {
	w := newWorld(t)
	a := w.key("id_a")
	w.probeSays()
	w.tty = "9\n1\n"
	wantCode(t, w.run(SSH, "host.example.com"), term.ExitOK)
	if got := w.execd(); got.Args[1] != a {
		t.Errorf("exec %s", got.Line())
	}
}

func TestE7CancelRunsNothing(t *testing.T) {
	for _, typed := range []string{"q\n", ""} {
		w := newWorld(t)
		w.key("id_a")
		w.tty = typed
		wantCode(t, w.run(SSH, "host.example.com"), term.ExitCancelled)
		w.stderrHas("Cancelled; nothing was run.")
		if len(w.ran()) != 0 {
			t.Errorf("typed %q: ran %v", typed, w.ran())
		}
	}
}

func TestE8NoTerminalToChooseOn(t *testing.T) {
	w := newWorld(t)
	w.key("id_a")
	w.noTTY = true
	wantCode(t, w.run(SCP, "f", "host.example.com:/tmp/"), term.ExitUsage)
	w.stderrHas("scp-with-key: no key given and no terminal to choose one on; give it with -i KEY.")
}

func TestE9NoPrivateKeys(t *testing.T) {
	w := newWorld(t)
	w.file("id_a.pub", "ssh-ed25519 AAAA a\n")
	wantCode(t, w.run(CopyID, "host.example.com"), term.ExitFailure)
	w.stderrHas("ssh-copy-id-with-key: no private keys in ~/.ssh; give one with --auth-key KEY.")
	if err := os.RemoveAll(w.sshDir()); err != nil {
		t.Fatal(err)
	}
	w.stderr.Reset()
	wantCode(t, w.run(SSH, "host.example.com"), term.ExitFailure)
	w.stderrHas("no private keys in ~/.ssh")
}

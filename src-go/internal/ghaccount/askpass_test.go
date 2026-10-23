package ghaccount

import (
	"bytes"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func askpassEnv(mode, pass string) func(string) string {
	return func(k string) string {
		switch k {
		case AskpassModeVar:
			return mode
		case askpassSecretVar:
			return pass
		}
		return ""
	}
}

func TestAskpassAnswersOnlyThePassphrasePrompts(t *testing.T) {
	cases := []struct {
		mode, prompt string
		answer       bool
	}{
		{"keygen", `Enter passphrase for "/h/.ssh/github_w" (empty for no passphrase): `, true},
		{"keygen", "Enter passphrase (empty for no passphrase): ", true},
		{"keygen", "Enter same passphrase again: ", true},
		{"keygen", "Overwrite (y/n)? ", false},
		{"ssh", "Enter passphrase for key '/h/.ssh/github_w': ", true},
		{"ssh", "Are you sure you want to continue connecting (yes/no/[fingerprint])? ", false},
		{"ssh", "git@github.com's password: ", false},
		{"ssh", "Enter same passphrase again: ", false},
		{"other", "Enter passphrase for key 'x': ", false},
	}
	for _, c := range cases {
		var out bytes.Buffer
		code := Askpass(askpassEnv(c.mode, "pw 1"), []string{c.prompt}, &out)
		if c.answer {
			if code != term.ExitOK || out.String() != "pw 1\n" {
				t.Errorf("%s %q: %d %q", c.mode, c.prompt, code, out.String())
			}
			continue
		}
		if code != term.ExitFailure || out.Len() != 0 {
			t.Errorf("%s %q answered: %d %q", c.mode, c.prompt, code, out.String())
		}
	}
}

func TestAskpassWithNoPassphraseOrPromptAnswersNothing(t *testing.T) {
	var out bytes.Buffer
	if code := Askpass(askpassEnv("ssh", ""), []string{"Enter passphrase for key 'x': "}, &out); code != term.ExitFailure || out.Len() != 0 {
		t.Errorf("no passphrase: %d %q", code, out.String())
	}
	if code := Askpass(askpassEnv("ssh", "pw"), nil, &out); code != term.ExitFailure || out.Len() != 0 {
		t.Errorf("no prompt: %d %q", code, out.String())
	}
}

func TestAskpassWriteFailureIsAFailure(t *testing.T) {
	if code := Askpass(askpassEnv("ssh", "pw"), []string{"Enter passphrase for key 'x': "}, failingWriter{}); code != term.ExitFailure {
		t.Errorf("exit %d", code)
	}
}

func TestIsAskpass(t *testing.T) {
	if IsAskpass(askpassEnv("", "")) || !IsAskpass(askpassEnv("ssh", "")) {
		t.Error("IsAskpass reads the mode variable")
	}
}

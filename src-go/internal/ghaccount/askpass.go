package ghaccount

import (
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	// AskpassModeVar is the variable that, set in the environment of ssh-keygen and ssh, makes
	// this binary their askpass program (E21): "keygen" or "ssh".
	AskpassModeVar = "GH_ACCOUNT_SETUP_ASKPASS"
	// askpassSecretVar is the passphrase, handed to the same child as an execx.Secret.
	askpassSecretVar = "GH_ACCOUNT_SETUP_PASSPHRASE"

	askpassKeygen = "keygen"
	askpassSSH    = "ssh"
)

// IsAskpass reports whether this process was started as an askpass program.
func IsAskpass(getenv func(string) string) bool { return getenv(AskpassModeVar) != "" }

// Askpass answers one prompt of ssh-keygen or ssh with the passphrase, and only a passphrase
// prompt of the program it was set up for (E21): a host key question or a password prompt is
// refused, so the passphrase can only ever go where it was meant to.
func Askpass(getenv func(string) string, args []string, stdout io.Writer) term.ExitCode {
	pass := getenv(askpassSecretVar)
	if pass == "" || len(args) != 1 {
		return term.ExitFailure
	}
	prompt := args[0]
	var ok bool
	switch getenv(AskpassModeVar) {
	case askpassKeygen:
		ok = strings.Contains(prompt, "passphrase")
	case askpassSSH:
		ok = strings.HasPrefix(prompt, "Enter passphrase for")
	}
	if !ok {
		return term.ExitFailure
	}
	if _, err := io.WriteString(stdout, pass+"\n"); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}

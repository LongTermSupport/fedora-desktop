package badexit

import (
	"fmt"
	"os"
	"syscall"

	"fix/internal/term"
)

// translated hands the code through the owner's translation.
func translated(cmd *CommandError) term.ExitCode {
	return term.FromChild(cmd.Code)
}

// said prints the child's code and exits with one of the tool's own.
func said(cmd *CommandError) term.ExitCode {
	fmt.Fprintf(os.Stderr, "%s exited %d\n", cmd.Tool, cmd.Code)
	return term.ExitFailure
}

// signalled is the shell's own convention for the tool's own signal, not a child's code.
func signalled(sig syscall.Signal) term.ExitCode {
	return term.ExitCode(128 + int(sig))
}

// own is a code this tool computed itself.
type result struct{ status int }

func own(r result) term.ExitCode {
	return term.ExitCode(r.status)
}

// constant is a code by number.
func constant() term.ExitCode { return term.ExitCode(78) }

// compared reads the code only to decide.
func compared(cmd *CommandError) term.ExitCode {
	if cmd.Code == 78 {
		return term.ExitConfig
	}
	return term.ExitFailure
}

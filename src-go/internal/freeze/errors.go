package freeze

import (
	"context"
	"errors"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitCode turns the error a run ended with into its exit status, printing the "<tool>: ..."
// line a fatal error owes on stderr. ErrSomeFailed prints nothing (each failure and the count
// were already reported); a cancellation prints nothing (the prompt already ended its line).
// Usage mistakes exit 1, not term.ExitUsage: the cut-over keeps every observable status.
func ExitCode(stderr io.Writer, tool string, err error) term.ExitCode {
	switch {
	case err == nil:
		return term.ExitOK
	case errors.Is(err, ErrSomeFailed):
		return term.ExitFailure
	case errors.Is(err, context.Canceled):
		return term.ExitCancelled
	}
	t := &term.Term{Err: stderr}
	t.Printf("%s: %s\n", tool, err.Error())
	return term.ExitFailure
}
